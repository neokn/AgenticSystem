// Package main launches the AgenticSystem with ADK's built-in Web UI.
//
// Usage:
//
//	go run ./cmd/web/main.go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"

	"github.com/neokn/agenticsystem/internal/app/appwire"
	"github.com/neokn/agenticsystem/internal/infra/sessionstore"
)

func run() error {
	_ = godotenv.Load()

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GOOGLE_API_KEY is not set")
	}

	ctx := context.Background()

	app, err := appwire.New(ctx, apiKey, appwire.Config{
		AgentDir:       ".",
		AgentName:      "demo_agent",
		AppName:        "web_app",
		SessionService: sessionstore.NewJSONLService("data/sessions"),
	})
	if err != nil {
		return fmt.Errorf("assembling app: %w", err)
	}

	// Default args: web --port 9090 api webui --api_server_address http://localhost:9090/api
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
		SessionService: app.SessionService,
		AgentLoader:    agent.NewSingleLoader(app.Agent),
		PluginConfig:   app.PluginConfig,
	}, args)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
