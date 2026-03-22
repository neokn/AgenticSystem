// Package main implements the Telegram Bot MVP entrypoint for AgenticSystem.
//
// It is a Driving Adapter in the Infrastructure layer: it polls Telegram for
// incoming messages via Long Polling, translates each message into an ADK
// runner.Run() call, and sends the reply back to the chat.
//
// Each message creates a fresh JSONL-backed session stored under data/sessions/.
// Sessions are persisted across process restarts (ADR-0007).
//
// Usage:
//
//	TELEGRAM_BOT_TOKEN=<token> GOOGLE_API_KEY=<key> go run ./cmd/telegram/main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/neokn/agenticsystem/internal/agentdef"
	"github.com/neokn/agenticsystem/internal/memory"
	"github.com/neokn/agenticsystem/internal/sessionstore"
	"github.com/neokn/agenticsystem/internal/shelltool"
)

// checkBotToken returns an error if TELEGRAM_BOT_TOKEN is not set.
func checkBotToken() error {
	if os.Getenv("TELEGRAM_BOT_TOKEN") == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is not set")
	}
	return nil
}

// checkAPIKey returns an error if GOOGLE_API_KEY is not set.
func checkAPIKey() error {
	if os.Getenv("GOOGLE_API_KEY") == "" {
		return fmt.Errorf("GOOGLE_API_KEY is not set")
	}
	return nil
}

// runBot assembles all components and starts the Long Polling loop.
func runBot(ctx context.Context) error {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	apiKey := os.Getenv("GOOGLE_API_KEY")

	// --- Load agent definition ---
	def, err := agentdef.Load(".", "demo_agent")
	if err != nil {
		return fmt.Errorf("loading agent definition: %w", err)
	}

	// --- genai client (for compress worker) ---
	genaiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("creating genai client: %w", err)
	}

	// --- Model profile ---
	reg, err := memory.NewRegistry()
	if err != nil {
		return fmt.Errorf("creating registry: %w", err)
	}
	profile, err := reg.GetProfile(def.ModelID)
	if err != nil {
		return fmt.Errorf("getting profile: %w", err)
	}
	profile.CompressModelID = "gemini-3.1-flash-lite-preview"

	// --- Compress strategy + memory plugin ---
	worker := memory.NewGenaiWorker(genaiClient)
	strategy := memory.NewGenerational(memory.GenerationalConfig{}, worker, profile)

	memPlugin, err := memory.NewMemoryPlugin(genaiClient, strategy, profile, 0)
	if err != nil {
		return fmt.Errorf("creating memory plugin: %w", err)
	}
	pl, err := memPlugin.BuildPlugin()
	if err != nil {
		return fmt.Errorf("building ADK plugin: %w", err)
	}

	// --- LLM agent with shell tool ---
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
		Description: "Telegram Bot — demo agent via Telegram.",
		Tools:       []tool.Tool{shellTool},
	})
	if err != nil {
		return fmt.Errorf("creating LLM agent: %w", err)
	}

	// --- Session service + Runner ---
	sessionSvc := sessionstore.NewJSONLService("data/sessions")
	appName := "telegram_bot_app"

	r, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          a,
		SessionService: sessionSvc,
		PluginConfig: runner.PluginConfig{
			Plugins: []*plugin.Plugin{pl},
		},
	})
	if err != nil {
		return fmt.Errorf("creating runner: %w", err)
	}

	// --- Telegram bot ---
	handler := func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil || update.Message.Text == "" || update.Message.From == nil {
			return
		}

		chatID := update.Message.Chat.ID
		userID := strconv.FormatInt(update.Message.From.ID, 10)

		// Each message gets a fresh JSONL-backed session (persistence via ADR-0007).
		// Session reuse per chat ID is deferred to card 06.
		sessResp, err := sessionSvc.Create(ctx, &session.CreateRequest{
			AppName: appName,
			UserID:  userID,
		})
		if err != nil {
			slog.Error("telegram-bot: failed to create session", "error", err)
			return
		}

		userMsg := genai.NewContentFromText(update.Message.Text, genai.RoleUser)
		reply := ""

		for event, err := range r.Run(ctx, userID, sessResp.Session.ID(), userMsg, agent.RunConfig{}) {
			if err != nil {
				slog.Error("telegram-bot: agent error", "error", err)
				continue
			}
			if event.Content == nil {
				continue
			}
			for _, p := range event.Content.Parts {
				reply += p.Text
			}
		}

		if reply == "" {
			reply = "(no response)"
		}

		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   reply,
		}); err != nil {
			slog.Error("telegram-bot: failed to send reply", "error", err)
		}
	}

	b, err := bot.New(botToken, bot.WithDefaultHandler(handler))
	if err != nil {
		return fmt.Errorf("creating telegram bot: %w", err)
	}

	slog.Info("telegram-bot: starting long polling")
	b.Start(ctx) // blocks until ctx is cancelled
	return nil
}

func main() {
	ctx := context.Background()

	_ = godotenv.Load()

	if err := checkBotToken(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if err := checkAPIKey(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if err := runBot(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
