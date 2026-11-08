// Package main implements a Telegram Bot backed by an ADK Go agent.
//
// Usage:
//
//	# Telegram bot mode
//	TELEGRAM_BOT_TOKEN=<token> GOOGLE_GENAI_API_KEY=<key> go run ./cmd/telegram
//
//	# ADK Dev UI mode
//	GOOGLE_GENAI_API_KEY=<key> go run ./cmd/telegram web --port 9090
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/google/dotprompt/go/dotprompt"
	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/adk/tool/mcptoolset"
	"google.golang.org/genai"
)

// loadPrompt loads and parses a .prompt file via dotprompt DirStore.
// Returns the model name from frontmatter and the rendered system instruction.
func loadPrompt(dir, name string) (modelName, instruction string, err error) {
	store, err := dotprompt.NewDirStore(dir)
	if err != nil {
		return "", "", fmt.Errorf("creating prompt store: %w", err)
	}

	promptData, err := store.Load(name, dotprompt.LoadPromptOptions{})
	if err != nil {
		return "", "", fmt.Errorf("loading prompt %q: %w", name, err)
	}

	dp := dotprompt.NewDotprompt(nil)
	rendered, err := dp.Render(promptData.Source, &dotprompt.DataArgument{}, nil)
	if err != nil {
		return "", "", fmt.Errorf("rendering prompt: %w", err)
	}

	modelName = rendered.Model

	// Collect system messages as the instruction.
	var parts []string
	for _, msg := range rendered.Messages {
		if msg.Role == dotprompt.RoleSystem {
			for _, p := range msg.Content {
				if tp, ok := p.(*dotprompt.TextPart); ok {
					parts = append(parts, tp.Text)
				}
			}
		}
	}
	instruction = strings.Join(parts, "\n")

	return modelName, instruction, nil
}

// buildAgent constructs the ADK agent with all tools configured.
func buildAgent(ctx context.Context) (agent.Agent, error) {
	apiKey := os.Getenv("GOOGLE_GENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GOOGLE_GENAI_API_KEY is not set")
	}

	backend := genai.BackendGeminiAPI
	if strings.EqualFold(os.Getenv("GOOGLE_GENAI_USE_VERTEXAI"), "true") {
		backend = genai.BackendVertexAI
	}

	// --- Load system prompt via dotprompt ---
	modelName, instruction, err := loadPrompt("agents/goost", "agent")
	if err != nil {
		return nil, err
	}
	if i := strings.Index(modelName, "/"); i >= 0 {
		modelName = modelName[i+1:]
	}
	if modelName == "" {
		modelName = "gemini-2.5-flash"
	}

	// --- Custom tools ---
	type timeArgs struct {
		Timezone string `json:"timezone,omitempty" jsonschema:"IANA timezone such as Asia/Taipei. Defaults to Asia/Taipei if omitted."`
	}
	type timeResult struct {
		Now      string `json:"now"`
		Timezone string `json:"timezone"`
	}
	currentTimeTool, err := functiontool.New(
		functiontool.Config{
			Name:        "get_current_time",
			Description: "Returns the current date and time.",
		},
		func(ctx tool.Context, args timeArgs) (timeResult, error) {
			tz := args.Timezone
			if tz == "" {
				tz = "Asia/Taipei"
			}
			loc, err := time.LoadLocation(tz)
			if err != nil {
				return timeResult{}, fmt.Errorf("invalid timezone %q: %w", tz, err)
			}
			now := time.Now().In(loc)
			return timeResult{
				Now:      now.Format(time.RFC3339),
				Timezone: tz,
			}, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating current_time tool: %w", err)
	}

	// --- MCP: Chrome DevTools ---
	cdpTools, err := mcptoolset.New(mcptoolset.Config{
		Transport: &mcp.CommandTransport{
			Command: exec.Command("npx", "-y", "chrome-devtools-mcp@latest"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating chrome devtools mcp toolset: %w", err)
	}

	// --- Create Gemini model ---
	model, err := gemini.NewModel(ctx, modelName, &genai.ClientConfig{
		Backend: backend,
		APIKey:  apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("creating gemini model: %w", err)
	}

	// --- Assemble agent ---
	a, err := llmagent.New(llmagent.Config{
		Name:        "Goost",
		Model:       model,
		Description: "A helpful Telegram bot assistant.",
		Instruction: instruction,
		Tools:       []tool.Tool{currentTimeTool},
		Toolsets:    []tool.Toolset{cdpTools},
	})
	if err != nil {
		return nil, fmt.Errorf("creating agent: %w", err)
	}

	slog.Info("agent ready", "model", modelName)
	return a, nil
}

// extractReplyText walks runner events and collects text parts from
// non-user authors, returning the concatenated reply.
func extractReplyText(events func(func(*session.Event, error) bool)) (string, error) {
	var reply strings.Builder
	for event, err := range events {
		if err != nil {
			return "", err
		}
		if event.Author == "user" || event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part.Text != "" {
				reply.WriteString(part.Text)
			}
		}
	}
	return reply.String(), nil
}

// runTelegram starts the Telegram bot handler.
func runTelegram(ctx context.Context, a agent.Agent) error {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is not set")
	}

	r, err := runner.New(runner.Config{
		AppName:           "telegram-bot",
		Agent:             a,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		return fmt.Errorf("creating runner: %w", err)
	}

	handler := func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil || update.Message.Text == "" || update.Message.From == nil {
			return
		}

		chatID := update.Message.Chat.ID
		userID := strconv.FormatInt(update.Message.From.ID, 10)

		userMsg := genai.NewContentFromText(update.Message.Text, genai.RoleUser)

		reply, err := extractReplyText(r.Run(ctx, userID, userID, userMsg, agent.RunConfig{}))
		if err != nil {
			slog.Error("runner error", "error", err)
			reply = "(internal error)"
		}
		if reply == "" {
			reply = "(no response)"
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

	slog.Info("telegram bot started")
	b.Start(ctx)
	return nil
}

func run() error {
	_ = godotenv.Load()

	ctx := context.Background()

	a, err := buildAgent(ctx)
	if err != nil {
		return err
	}

	// If subcommand is "web" or "console", delegate to ADK launcher.
	// Otherwise, start the Telegram bot.
	if len(os.Args) > 1 {
		l := full.NewLauncher()
		return l.Execute(ctx, &launcher.Config{
			AgentLoader:    agent.NewSingleLoader(a),
			SessionService: session.InMemoryService(),
		}, os.Args[1:])
	}

	return runTelegram(ctx, a)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
