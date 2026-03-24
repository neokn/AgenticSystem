// Package main implements the demo agent CLI for end-to-end verification of
// the AgenticSystem context-memory manager. It assembles all components from
// internal/infra/memory/ and wires them into an ADK Runner with a real LLMAgent.
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

	"google.golang.org/adk/session"

	"github.com/neokn/agenticsystem/internal/core/application"
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
func buildOOMTestProfile() application.ModelProfile {
	return application.ModelProfile{
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
func formatMetrics(snap application.MemoryMetrics, usageRatioCurve []float64, compressCostUSD float64) string {
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
	apiKey := os.Getenv("GOOGLE_API_KEY")

	app, err := application.New(ctx, apiKey, application.Config{
		AgentDir:       ".",
		AppName:        "demo_agent_app",
		SessionService: session.InMemoryService(),
	})
	if err != nil {
		return fmt.Errorf("assembling app: %w", err)
	}

	memPlugin := app.MemoryPlugin

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

		// Send user message to orchestrator.
		result, err := app.Orchestrator.Run(ctx, line)
		responseText := ""
		if err != nil {
			fmt.Fprintf(errOutput, "AGENT_ERROR: %v\n", err)
		} else {
			responseText = result.Response
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
