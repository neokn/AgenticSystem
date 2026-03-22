// Package main implements the Telegram Bot MVP entrypoint for AgenticSystem.
//
// It is a Driving Adapter in the Infrastructure layer: it polls Telegram for
// incoming messages via Long Polling, translates each message into an ADK
// runner.Run() call, and sends the reply back to the chat.
//
// Each message creates a fresh in-memory session — no persistence across
// messages (MVP simplification, as noted in ADR-0005).
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

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
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

// checkBotToken returns an error if TELEGRAM_BOT_TOKEN is not set.
// Acceptance criterion: process must fail-fast with a human-readable message.
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
// It mirrors the wiring in cmd/agent/main.go exactly (ADR-0005).
func runBot(ctx context.Context) error {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	apiKey := os.Getenv("GOOGLE_API_KEY")

	// --- Step 0: Load agent definition ---
	def, err := agentdef.Load(".", "demo_agent")
	if err != nil {
		return fmt.Errorf("failed to load agent definition: %w", err)
	}

	// --- Step 1: genai client (for compress worker) ---
	genaiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("failed to create genai client: %w", err)
	}

	// --- Step 2: Model profile ---
	reg, err := memory.NewRegistry()
	if err != nil {
		return fmt.Errorf("failed to create model registry: %w", err)
	}
	profile, err := reg.GetProfile(def.ModelID)
	if err != nil {
		return fmt.Errorf("failed to get model profile: %w", err)
	}
	profile.CompressModelID = "gemini-3.1-flash-lite-preview"

	// --- Step 3: Compress strategy ---
	worker := memory.NewGenaiWorker(genaiClient)
	strategy := memory.NewGenerational(memory.GenerationalConfig{}, worker, profile)

	// --- Step 4: Memory plugin ---
	memPlugin, err := memory.NewMemoryPlugin(genaiClient, strategy, profile, 0)
	if err != nil {
		return fmt.Errorf("failed to create memory plugin: %w", err)
	}

	// --- Step 5: Build ADK plugin ---
	pl, err := memPlugin.BuildPlugin()
	if err != nil {
		return fmt.Errorf("failed to build ADK plugin: %w", err)
	}

	// --- Step 6: Gemini model ---
	// gemini.NewModel requires a *genai.ClientConfig, not an existing *genai.Client.
	// The genaiClient created above is used exclusively for the compress worker.
	llmModel, err := gemini.NewModel(ctx, profile.ModelID, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("failed to create Gemini model: %w", err)
	}

	// --- Step 6b: LLM agent ---
	a, err := llmagent.New(llmagent.Config{
		Name:        def.Name,
		Model:       llmModel,
		Instruction: def.Instruction,
		Description: "Telegram Bot MVP — demo agent via Telegram.",
	})
	if err != nil {
		return fmt.Errorf("failed to create LLM agent: %w", err)
	}

	// --- Step 7: Session service and Runner ---
	sessionSvc := session.InMemoryService()
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
		return fmt.Errorf("failed to create runner: %w", err)
	}

	// --- Step 8: Telegram bot setup ---
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return fmt.Errorf("failed to create Telegram bot: %w", err)
	}

	slog.Info("telegram-bot: authorized", "username", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	slog.Info("telegram-bot: starting long polling loop")

	// --- Long Polling loop ---
	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Each message gets a fresh session (MVP: no persistence across messages).
		userID := strconv.FormatInt(update.Message.From.ID, 10)
		sessResp, err := sessionSvc.Create(ctx, &session.CreateRequest{
			AppName: appName,
			UserID:  userID,
		})
		if err != nil {
			slog.Error("telegram-bot: failed to create session", "error", err)
			continue
		}
		sess := sessResp.Session

		userMsg := genai.NewContentFromText(update.Message.Text, genai.RoleUser)
		reply := ""

		for event, err := range r.Run(ctx, userID, sess.ID(), userMsg, agent.RunConfig{}) {
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

		msg := tgbotapi.NewMessage(update.Message.Chat.ID, reply)
		if _, err := bot.Send(msg); err != nil {
			slog.Error("telegram-bot: failed to send reply", "error", err)
		}
	}

	return nil
}

func main() {
	ctx := context.Background()

	// Load .env file if present; ignore error when file does not exist.
	_ = godotenv.Load()

	// Fail fast if required tokens are missing.
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
