// Package main implements a Telegram Bot backed by a single Gemini agent.
//
// Usage:
//
//	TELEGRAM_BOT_TOKEN=<token> GOOGLE_API_KEY=<key> go run ./cmd/telegram
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
	"google.golang.org/genai"
)

// chatSession holds a per-user Gemini chat session.
type chatSession struct {
	chat *genai.Chat
}

// sessionStore manages per-user chat sessions (in-memory).
type sessionStore struct {
	mu       sync.RWMutex
	sessions map[int64]*chatSession
	client   *genai.Client
	model    string
	system   string
}

func newSessionStore(client *genai.Client, model, system string) *sessionStore {
	return &sessionStore{
		sessions: make(map[int64]*chatSession),
		client:   client,
		model:    model,
		system:   system,
	}
}

func (s *sessionStore) get(userID int64) *chatSession {
	s.mu.RLock()
	sess, ok := s.sessions[userID]
	s.mu.RUnlock()
	if ok {
		return sess
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if sess, ok := s.sessions[userID]; ok {
		return sess
	}

	chat, _ := s.client.Chats.Create(context.Background(), s.model, &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: s.system}},
		},
	}, nil)

	sess = &chatSession{chat: chat}
	s.sessions[userID] = sess
	return sess
}

func loadSystemPrompt(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading agent prompt: %w", err)
	}

	// Strip YAML frontmatter (--- ... ---) if present.
	content := string(data)
	if strings.HasPrefix(content, "---") {
		if idx := strings.Index(content[3:], "---"); idx >= 0 {
			content = strings.TrimSpace(content[3+idx+3:])
		}
	}

	// Strip dotprompt role markers like {{role "system"}}.
	content = strings.ReplaceAll(content, `{{role "system"}}`, "")
	return strings.TrimSpace(content), nil
}

func run() error {
	_ = godotenv.Load()

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is not set")
	}
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GOOGLE_API_KEY is not set")
	}

	ctx := context.Background()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("creating genai client: %w", err)
	}

	system, err := loadSystemPrompt("agents/demo_agent/agent.prompt")
	if err != nil {
		return fmt.Errorf("loading system prompt: %w", err)
	}

	store := newSessionStore(client, "gemini-2.5-flash", system)

	handler := func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil || update.Message.Text == "" || update.Message.From == nil {
			return
		}

		chatID := update.Message.Chat.ID
		userID := update.Message.From.ID

		sess := store.get(userID)
		resp, err := sess.chat.SendMessage(ctx, genai.Part{Text: update.Message.Text})
		reply := ""
		if err != nil {
			slog.Error("gemini error", "error", err)
			reply = "(internal error)"
		} else {
			reply = resp.Text()
			if reply == "" {
				reply = "(no response)"
			}
		}

		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   reply,
		}); err != nil {
			slog.Error("failed to send reply", "error", err)
		}
	}

	b, err := bot.New(botToken, bot.WithDefaultHandler(handler))
	if err != nil {
		return fmt.Errorf("creating telegram bot: %w", err)
	}

	slog.Info("bot started", "model", "gemini-2.5-flash")
	b.Start(ctx)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
