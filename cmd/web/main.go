// Package main launches the AgenticSystem with a simple HTTP API.
//
// Usage:
//
//	go run ./cmd/web/main.go
//
// The server listens on :9090 and exposes a single POST endpoint:
//
//	POST /run
//	Body: plain text user prompt
//	Response: plain text orchestrator response
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/joho/godotenv"

	"github.com/neokn/agenticsystem/internal/core/application"
	"github.com/neokn/agenticsystem/internal/infra/persistence/jsonl"
)

func run() error {
	_ = godotenv.Load()

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GOOGLE_API_KEY is not set")
	}

	ctx := context.Background()

	app, err := application.New(ctx, apiKey, application.Config{
		AgentDir:       ".",
		AppName:        "web_app",
		SessionService: jsonl.NewJSONLService("data/sessions"),
	})
	if err != nil {
		return fmt.Errorf("assembling app: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /run", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		prompt := string(body)
		if prompt == "" {
			http.Error(w, "empty prompt", http.StatusBadRequest)
			return
		}

		result, err := app.Orchestrator.Run(r.Context(), prompt)
		if err != nil {
			slog.Error("web: orchestrator error", "error", err)
			http.Error(w, "orchestrator error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, result.Response)
	})

	addr := ":9090"
	slog.Info("web: starting server", "addr", addr)
	return http.ListenAndServe(addr, mux)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
