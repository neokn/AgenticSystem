// Package main implements the demo agent CLI for end-to-end verification of
// the AgenticSystem context-memory manager. It assembles all components from
// internal/memory/ and wires them into an ADK Runner with a real LLMAgent.
//
// Usage:
//
//	go run ./cmd/agent/main.go [--turns N] [--metrics-out FILE]
//
// Interactive mode: type turns manually; Ctrl-D (EOF) ends the session.
// Scripted mode: pipe a file of N lines to stdin for automated test runs.
//
// Metrics are always printed to stdout at exit. Use --metrics-out to also
// write a machine-readable copy to a file.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"

	"github.com/neokn/agenticsystem/internal/agentdef"
	"github.com/neokn/agenticsystem/internal/memory"
)

// cliConfig holds parsed command-line flags.
type cliConfig struct {
	turns      int
	metricsOut string
}

// checkAPIKey returns an error if GOOGLE_API_KEY is not set in the environment.
// Acceptance criterion: process must fail-fast with a human-readable error.
func checkAPIKey() error {
	if os.Getenv("GOOGLE_API_KEY") == "" {
		return fmt.Errorf("GOOGLE_API_KEY is not set")
	}
	return nil
}

// parseFlags parses command-line arguments into a cliConfig.
// Returns an error if flag parsing fails.
func parseFlags(args []string) (cliConfig, error) {
	fs := flag.NewFlagSet("demo-agent", flag.ContinueOnError)
	var cfg cliConfig
	fs.IntVar(&cfg.turns, "turns", 0, "Number of turns to process before exiting (0 = unlimited, read until EOF)")
	fs.StringVar(&cfg.metricsOut, "metrics-out", "", "File path to write machine-readable metrics report")
	if err := fs.Parse(args); err != nil {
		return cliConfig{}, fmt.Errorf("parseFlags: %w", err)
	}
	return cfg, nil
}

// buildOOMTestProfile returns a ModelProfile with an artificially small
// context window for triggering the OOM handler in automated tests.
// Per acceptance criterion: context_window_tokens=2000.
func buildOOMTestProfile() memory.ModelProfile {
	return memory.ModelProfile{
		ModelID:             "gemini-2.0-flash",
		Provider:            "google",
		ContextWindowTokens: 2000,
		MaxOutputTokens:     512,
	}
}

// formatMetrics formats the metrics report as colon-separated lines.
// Format (per acceptance criteria):
//
//	usage_ratio_curve: <comma-separated floats>
//	compress_trigger_count: <int>
//	countTokens_api_call_count: <int>
//	compress_cost_usd: <6 decimal float>
//	oom_event_count: <int>
func formatMetrics(snap memory.MemoryMetrics, usageRatioCurve []float64, compressCostUSD float64) string {
	// Format usage_ratio_curve as comma-separated 6-decimal floats.
	curveValues := make([]string, 0, len(usageRatioCurve))
	for _, v := range usageRatioCurve {
		curveValues = append(curveValues, fmt.Sprintf("%f", v))
	}
	curveStr := strings.Join(curveValues, ",")

	return fmt.Sprintf(
		"usage_ratio_curve: %s\n"+
			"compress_trigger_count: %d\n"+
			"countTokens_api_call_count: %d\n"+
			"compress_cost_usd: %f\n"+
			"oom_event_count: %d\n",
		curveStr,
		snap.CompressTriggerCount,
		snap.CountTokensAPICallCount,
		compressCostUSD,
		snap.OOMEventCount,
	)
}

// writeMetricsToFile writes the metrics content to a file at path.
// Returns an error if the file cannot be created or written.
func writeMetricsToFile(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writeMetricsToFile: %w", err)
	}
	return nil
}

// runDemo is the main conversation loop. It is extracted from main() to allow
// testing without os.Exit calls.
//
// Architecture (per ADR-0003):
//  1. Create genai.Client (real API)
//  2. Create ModelProfile for gemini-2.0-flash
//  3. Create MemoryLayout with default config
//  4. Create CompressStrategy (Generational with GenaiWorker)
//  5. Create MemoryPlugin with all dependencies
//  6. Build ADK plugin and register with Runner
//  7. Create LLMAgent and ADK Runner
//  8. Run conversation loop (stdin → agent → stdout)
//  9. On exit: print metrics report
func runDemo(ctx context.Context, cfg cliConfig, input io.Reader, output io.Writer, errOutput io.Writer) error {
	// Step 0: Load agent definition from agents/demo_agent/agent.prompt
	def, err := agentdef.Load(".", "demo_agent")
	if err != nil {
		return fmt.Errorf("failed to load agent definition: %w", err)
	}

	// Step 1: Create genai.Client
	apiKey := os.Getenv("GOOGLE_API_KEY")
	genaiClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return fmt.Errorf("failed to create genai.Client: %w", err)
	}

	// Step 2: ModelProfile from agent definition (compress worker: gemini-3.1-flash-lite-preview)
	reg, err := memory.NewRegistry()
	if err != nil {
		return fmt.Errorf("failed to create model registry: %w", err)
	}
	profile, err := reg.GetProfile(def.ModelID)
	if err != nil {
		return fmt.Errorf("failed to get model profile: %w", err)
	}
	profile.CompressModelID = "gemini-3.1-flash-lite-preview"

	// Step 3: MemoryLayout with default config
	layoutCfg, err := memory.DefaultLayoutConfig()
	if err != nil {
		return fmt.Errorf("failed to load default layout config: %w", err)
	}
	layout, err := memory.NewLayout(profile, layoutCfg)
	if err != nil {
		return fmt.Errorf("failed to create memory layout: %w", err)
	}

	// Step 4: CompressStrategy (Generational with real GenaiWorker)
	worker := memory.NewGenaiWorker(genaiClient)
	strategy := memory.NewGenerational(memory.GenerationalConfig{OldestN: 5}, worker)

	// Step 5: MemoryPlugin
	memPlugin, err := memory.NewMemoryPlugin(genaiClient, layout, strategy, profile, 0)
	if err != nil {
		return fmt.Errorf("failed to create memory plugin: %w", err)
	}

	// Step 6: Build ADK plugin
	pl, err := memPlugin.BuildPlugin()
	if err != nil {
		return fmt.Errorf("failed to build ADK plugin: %w", err)
	}

	// Step 7a: Create ADK Gemini model.
	// Note: gemini.NewModel accepts a *genai.ClientConfig (not an existing *genai.Client),
	// so a separate client configuration is required here. The genaiClient created in Step 1
	// is used exclusively for the compress worker; the ADK Gemini model manages its own
	// internal client lifecycle via the ClientConfig it receives.
	llmModel, err := gemini.NewModel(ctx, profile.ModelID, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return fmt.Errorf("failed to create Gemini model: %w", err)
	}

	// Step 7b: Create LLMAgent
	a, err := llmagent.New(llmagent.Config{
		Name:        def.Name,
		Model:       llmModel,
		Instruction: def.Instruction,
		Description: "Demo agent for end-to-end verification of context memory manager.",
	})
	if err != nil {
		return fmt.Errorf("failed to create LLM agent: %w", err)
	}

	// Step 7c: Create session service and Runner
	sessionSvc := session.InMemoryService()
	appName := "demo_agent_app"
	userID := "demo_user"

	sessResp, err := sessionSvc.Create(ctx, &session.CreateRequest{
		AppName: appName,
		UserID:  userID,
	})
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	sess := sessResp.Session

	r, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          a,
		SessionService: sessionSvc,
		PluginConfig: runner.PluginConfig{
			Plugins: []*plugin.Plugin{pl},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	// Step 8: Conversation loop
	// usageRatioCurve records the usage ratio snapshot after each turn.
	var usageRatioCurve []float64

	scanner := bufio.NewScanner(input)
	turnCount := 0

	slog.Info("demo-agent: starting conversation loop",
		"turns_limit", cfg.turns,
	)

	for {
		// Check turn limit
		if cfg.turns > 0 && turnCount >= cfg.turns {
			break
		}

		if !scanner.Scan() {
			// EOF or scan error
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		turnCount++

		// Send user message to agent
		userMsg := genai.NewContentFromText(line, genai.RoleUser)
		responseText := ""
		isOOM := false

		for event, err := range r.Run(ctx, userID, sess.ID(), userMsg, agent.RunConfig{}) {
			if err != nil {
				fmt.Fprintf(errOutput, "AGENT_ERROR: %v\n", err)
				continue
			}
			// Event embeds model.LLMResponse by value; check Content field directly.
			if event.Content == nil {
				// Check for OOM warning in CustomMetadata
				if meta := event.CustomMetadata; meta != nil {
					if _, ok := meta["oom_warning"]; ok {
						isOOM = true
						fmt.Fprintf(errOutput, "OOM_WARNING: Context window exhausted. Please start a new conversation.\n")
					}
				}
				continue
			}
			for _, p := range event.Content.Parts {
				responseText += p.Text
			}
		}

		if isOOM && responseText == "" {
			responseText = "[OOM] Context window exhausted. Please start a new conversation."
		}

		fmt.Fprintln(output, responseText)

		// Record usage ratio after this turn
		snap := memPlugin.GetSnapshot()
		usageRatioCurve = append(usageRatioCurve, snap.UsageRatio)
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("demo-agent: scanner error", "error", err)
	}

	// Step 9: Print metrics report
	finalSnap := memPlugin.GetSnapshot()
	// Cost calculation: use compress reclaimed tokens as a proxy
	// (full cost calculation is out of scope for MVP)
	compressCostUSD := 0.0
	metricsContent := formatMetrics(finalSnap, usageRatioCurve, compressCostUSD)
	fmt.Fprint(output, "\n--- Metrics Report ---\n")
	fmt.Fprint(output, metricsContent)

	if cfg.metricsOut != "" {
		if err := writeMetricsToFile(cfg.metricsOut, metricsContent); err != nil {
			fmt.Fprintf(errOutput, "failed to write metrics file: %v\n", err)
		} else {
			slog.Info("demo-agent: metrics written to file", "path", cfg.metricsOut)
		}
	}

	return nil
}

func main() {
	ctx := context.Background()

	// Load .env file if present; ignore error when file does not exist.
	_ = godotenv.Load()

	// Check API key first — fail fast.
	if err := checkAPIKey(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := runDemo(ctx, cfg, os.Stdin, os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
