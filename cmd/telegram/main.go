// Package main implements the Telegram Bot entrypoint for AgenticSystem.
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

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"

	"github.com/neokn/agenticsystem/internal/core/application"
	"github.com/neokn/agenticsystem/internal/infra/persistence/jsonl"
)

func checkBotToken() error {
	if os.Getenv("TELEGRAM_BOT_TOKEN") == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is not set")
	}
	return nil
}

func checkAPIKey() error {
	if os.Getenv("GOOGLE_API_KEY") == "" {
		return fmt.Errorf("GOOGLE_API_KEY is not set")
	}
	return nil
}

func runBot(ctx context.Context) error {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	apiKey := os.Getenv("GOOGLE_API_KEY")

	app, err := application.New(ctx, apiKey, application.Config{
		AgentDir:       ".",
		AppName:        "telegram_bot_app",
		SessionService: jsonl.NewJSONLService("data/sessions"),
	})
	if err != nil {
		return fmt.Errorf("assembling app: %w", err)
	}

	handler := func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil || update.Message.Text == "" || update.Message.From == nil {
			return
		}

		chatID := update.Message.Chat.ID

		result, err := app.Orchestrator.Run(ctx, update.Message.Text)
		reply := ""
		if err != nil {
			slog.Error("telegram-bot: orchestrator error", "error", err)
			reply = "(internal error)"
		} else {
			reply = result.Response
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
	b.Start(ctx)
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
