// Package main launches the AgenticSystem demo agent with ADK's built-in Web UI.
//
// Usage:
//
//	go run ./cmd/web/main.go web
//
// Then open http://localhost:8080 in a browser.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"

	"github.com/neokn/agenticsystem/internal/agentdef"
	"github.com/neokn/agenticsystem/internal/memory"
	"github.com/neokn/agenticsystem/internal/shelltool"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

func run() error {
	_ = godotenv.Load()

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GOOGLE_API_KEY is not set")
	}

	ctx := context.Background()

	// --- load agent definition from agents/demo_agent/agent.prompt ---
	def, err := agentdef.Load(".", "demo_agent")
	if err != nil {
		return fmt.Errorf("loading agent definition: %w", err)
	}

	// --- genai client (for compress worker) ---
	genaiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("creating genai client: %w", err)
	}

	// --- model profile ---
	reg, err := memory.NewRegistry()
	if err != nil {
		return fmt.Errorf("creating registry: %w", err)
	}
	profile, err := reg.GetProfile(def.ModelID)
	if err != nil {
		return fmt.Errorf("getting profile: %w", err)
	}
	profile.CompressModelID = "gemini-3.1-flash-lite-preview"

	// --- compress strategy ---
	worker := memory.NewGenaiWorker(genaiClient)
	strategy := memory.NewGenerational(memory.GenerationalConfig{}, worker, profile)

	// --- memory plugin ---
	memPlugin, err := memory.NewMemoryPlugin(genaiClient, strategy, profile, 0)
	if err != nil {
		return fmt.Errorf("creating memory plugin: %w", err)
	}
	pl, err := memPlugin.BuildPlugin()
	if err != nil {
		return fmt.Errorf("building ADK plugin: %w", err)
	}

	// --- LLM agent ---
	llmModel, err := gemini.NewModel(ctx, profile.ModelID, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("creating Gemini model: %w", err)
	}

	shellTool, err := functiontool.New(functiontool.Config{
		Name:                "shell_exec",
		Description:         "Execute a shell command and return stdout and exit code.",
		RequireConfirmation: false,
	}, shelltool.ToolHandlerFunc)
	if err != nil {
		return fmt.Errorf("creating shell tool: %w", err)
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        def.Name,
		Model:       llmModel,
		Instruction: def.Instruction,
		Description: "Demo agent with context memory management.",
		Tools:       []tool.Tool{shellTool},
	})
	if err != nil {
		return fmt.Errorf("creating LLM agent: %w", err)
	}

	// --- launcher ---
	// Default args: web --port 9090 api webui --api_server_address http://localhost:9090/api
	// Users can override by passing their own args.
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{
			"web", "--port", "9090",
			"api",
			"webui", "--api_server_address", "http://localhost:9090/api",
		}
	}

	l := full.NewLauncher()
	return l.Execute(ctx, &launcher.Config{
		SessionService: session.InMemoryService(),
		AgentLoader:    agent.NewSingleLoader(a),
		PluginConfig: runner.PluginConfig{
			Plugins: []*plugin.Plugin{pl},
		},
	}, args)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
