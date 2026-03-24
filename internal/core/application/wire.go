// Package application assembles the shared application core used by all entrypoints
// (CLI, Web UI, Telegram, etc.). Each entrypoint only provides the I/O layer;
// the orchestrator, plugins, tools, and session service are wired identically.
//
// Orchestrator-only mode: the App drives conversations through the 4-phase
// Orchestrator loop (Plan → Execute → Evaluate → Respond).
package application

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/genai"

	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/adk/tool/mcptoolset"

	"github.com/neokn/agenticsystem/internal/core/application/agenttree"
	"github.com/neokn/agenticsystem/internal/core/application/orchestrator"
	"github.com/neokn/agenticsystem/internal/core/domain"
	"github.com/neokn/agenticsystem/internal/infra/config/agentdef"
	"github.com/neokn/agenticsystem/internal/infra/config/mcpconfig"
	"github.com/neokn/agenticsystem/internal/infra/executor"
	"github.com/neokn/agenticsystem/internal/infra/llm"
	"github.com/neokn/agenticsystem/internal/infra/observe/memory"
	"github.com/neokn/agenticsystem/internal/infra/tooling/shell"
)

// MemoryMetrics is a re-export of memory.MemoryMetrics so that callers of
// application do not need to import internal/infra/observe/memory directly.
type MemoryMetrics = memory.MemoryMetrics

// ModelProfile is a re-export of memory.ModelProfile so that callers of
// application do not need to import internal/infra/observe/memory directly.
type ModelProfile = memory.ModelProfile

// App holds the assembled application core. Entrypoints use the Orchestrator
// to drive conversations through their own I/O layer.
type App struct {
	Orchestrator   *orchestrator.Orchestrator
	SessionService session.Service
	MemoryPlugin   *memory.MemoryPlugin
	AppName        string
}

// Config holds the parameters that vary between entrypoints.
type Config struct {
	// AgentDir is the base directory for agent definitions and prompts (typically ".").
	AgentDir string

	// AppName is the ADK application name (e.g. "telegram_bot_app").
	AppName string

	// SessionService is the session backend. Entrypoints choose between
	// InMemoryService, JSONLService, etc.
	SessionService session.Service

	// ModelID is the default LLM model identifier (e.g. "gemini-2.5-flash").
	// Used for Planner, Evaluator, Responder, and the agent tree executor.
	ModelID string

	// SystemMaxRetry is the Orchestrator hard upper limit on retry cycles.
	// Defaults to 3 when zero.
	SystemMaxRetry int
}

// New assembles the full application core wired around the Orchestrator.
func New(ctx context.Context, apiKey string, cfg Config) (*App, error) {
	// Apply defaults.
	if cfg.ModelID == "" {
		cfg.ModelID = "gemini-2.5-flash"
	}
	maxRetry := cfg.SystemMaxRetry
	if maxRetry == 0 {
		maxRetry = 3
	}

	// --- genai client (for memory compression worker) ---
	genaiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("appwire: creating genai client: %w", err)
	}

	// --- Model profile ---
	reg, err := memory.NewRegistry()
	if err != nil {
		return nil, fmt.Errorf("appwire: creating registry: %w", err)
	}
	profile, err := reg.GetProfile(cfg.ModelID)
	if err != nil {
		return nil, fmt.Errorf("appwire: getting profile: %w", err)
	}
	profile.CompressModelID = "gemini-3.1-flash-lite-preview"

	// --- Memory plugin ---
	worker := memory.NewGenaiWorker(genaiClient)
	strategy := memory.NewGenerational(memory.GenerationalConfig{}, worker, profile)
	memPlugin, err := memory.NewMemoryPlugin(genaiClient, strategy, profile, 0)
	if err != nil {
		return nil, fmt.Errorf("appwire: creating memory plugin: %w", err)
	}
	memPl, err := memPlugin.BuildPlugin()
	if err != nil {
		return nil, fmt.Errorf("appwire: building memory plugin: %w", err)
	}
	_ = memPl // memory plugin built; retained in App for metrics access

	// --- Shell tool ---
	shellTool, err := functiontool.New(functiontool.Config{
		Name:                "shell_exec",
		Description:         "Execute a shell command and return stdout and exit code.",
		RequireConfirmation: false,
	}, shell.ToolHandlerFunc)
	if err != nil {
		return nil, fmt.Errorf("appwire: creating shell tool: %w", err)
	}

	// --- MCP toolsets ---
	toolsetRegistry := make(map[string]tool.Toolset)
	mcpCfg, err := mcpconfig.Load(cfg.AgentDir, "root")
	if err != nil {
		slog.Debug("appwire: no MCP config for root agent", "err", err)
	}
	if mcpCfg != nil {
		for _, srv := range mcpCfg.Servers {
			env := append(os.Environ(), envMapToSlice(srv.Env)...)
			cmd := exec.Command(srv.Command, srv.Args...)
			cmd.Env = env
			ts, err := mcptoolset.New(mcptoolset.Config{
				Transport: &mcp.CommandTransport{Command: cmd},
			})
			if err != nil {
				return nil, fmt.Errorf("appwire: mcp server %q: %w", srv.Name, err)
			}
			toolsetRegistry[srv.Name] = ts
		}
	}

	// --- Load prompts ---
	planPrompt, err := loadPrompt(cfg.AgentDir, "plan.prompt")
	if err != nil {
		return nil, fmt.Errorf("appwire: %w", err)
	}
	evalPrompt, err := loadPrompt(cfg.AgentDir, "evaluate.prompt")
	if err != nil {
		return nil, fmt.Errorf("appwire: %w", err)
	}
	respondPrompt, err := loadPrompt(cfg.AgentDir, "respond.prompt")
	if err != nil {
		return nil, fmt.Errorf("appwire: %w", err)
	}

	// --- Build Gemini adapters ---
	planner := &llm.GeminiPlanner{
		Client:       genaiClient,
		Model:        cfg.ModelID,
		SystemPrompt: planPrompt,
	}
	evaluator := &llm.GeminiEvaluator{
		Client:       genaiClient,
		Model:        cfg.ModelID,
		SystemPrompt: evalPrompt,
	}
	responder := &llm.GeminiResponder{
		Client:       genaiClient,
		Model:        cfg.ModelID,
		SystemPrompt: respondPrompt,
	}

	// --- Build ADKExecutor with builder dependencies ---
	builderDeps := agenttree.Deps{
		ModelFactory: func(modelID string) (model.LLM, error) {
			return gemini.NewModel(ctx, modelID, &genai.ClientConfig{APIKey: apiKey})
		},
		PromptLoader: func(baseDir, agentName string) (string, error) {
			def, err := agentdef.Load(baseDir, agentName)
			if err != nil {
				return "", err
			}
			return def.Instruction, nil
		},
		ToolRegistry: map[string]tool.Tool{
			"shell_exec": shellTool,
		},
		ToolsetRegistry: toolsetRegistry,
		BaseDir:         cfg.AgentDir,
	}

	exec := &executor.ADKExecutor{
		BuilderDeps:    builderDeps,
		SessionService: cfg.SessionService,
		AppName:        cfg.AppName,
		Defaults: domain.AgentDefaults{
			Model: cfg.ModelID,
		},
	}

	// --- Scan available roles ---
	availableRoles := scanAvailableRoles(cfg.AgentDir)

	// --- Template loader for the Orchestrator converter ---
	templateLoader := orchestrator.TemplateLoader(func(baseDir, role string) (string, bool) {
		def, err := agentdef.Load(baseDir, role)
		if err != nil {
			return "", false
		}
		return def.Instruction, true
	})

	// --- Build Orchestrator ---
	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       exec,
		TemplateLoader: templateLoader,
		AvailableTools: []string{"shell_exec"},
		AvailableRoles: availableRoles,
		SystemMaxRetry: maxRetry,
	})

	return &App{
		Orchestrator:   orch,
		SessionService: cfg.SessionService,
		MemoryPlugin:   memPlugin,
		AppName:        cfg.AppName,
	}, nil
}

// loadPrompt reads a prompt file from <baseDir>/prompts/<name> and returns its content.
func loadPrompt(baseDir, name string) (string, error) {
	path := filepath.Join(baseDir, "prompts", name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("loading prompt %s: %w", name, err)
	}
	return string(data), nil
}

// scanAvailableRoles reads the agents/ directory under baseDir and returns the
// names of subdirectories that contain an agent.prompt file.
func scanAvailableRoles(baseDir string) []string {
	agentsDir := filepath.Join(baseDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil
	}
	var roles []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		promptPath := filepath.Join(agentsDir, e.Name(), "agent.prompt")
		if _, err := os.Stat(promptPath); err == nil {
			roles = append(roles, e.Name())
		}
	}
	return roles
}

// envMapToSlice converts a map[string]string into a slice of "KEY=value" strings
// suitable for appending to exec.Cmd.Env. Entries with an empty key are skipped.
func envMapToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		if k == "" {
			continue
		}
		result = append(result, k+"="+v)
	}
	return result
}
