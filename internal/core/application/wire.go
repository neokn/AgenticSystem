// Package application assembles the shared application core used by all entrypoints
// (CLI, Web UI, Telegram, etc.). Each entrypoint only provides the I/O layer;
// the agent, plugins, tools, and runner are wired identically.
//
// Supports two modes:
//   - Legacy single-agent mode: uses AgentName to load a single LlmAgent (backward compatible)
//   - Agent tree mode: loads agenttree.yaml and recursively builds the full agent hierarchy
package application

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/adk/tool/mcptoolset"

	"github.com/neokn/agenticsystem/internal/core/application/agenttree"
	"github.com/neokn/agenticsystem/internal/core/domain"
	"github.com/neokn/agenticsystem/internal/infra/config/agentdef"
	agentreeloader "github.com/neokn/agenticsystem/internal/infra/config/agenttree"
	"github.com/neokn/agenticsystem/internal/infra/config/mcpconfig"
	"github.com/neokn/agenticsystem/internal/infra/observe/debug"
	"github.com/neokn/agenticsystem/internal/infra/observe/memory"
	"github.com/neokn/agenticsystem/internal/infra/tooling/shell"
)

// MemoryMetrics is a re-export of memory.MemoryMetrics so that callers of
// application do not need to import internal/infra/observe/memory directly.
type MemoryMetrics = memory.MemoryMetrics

// ModelProfile is a re-export of memory.ModelProfile so that callers of
// application do not need to import internal/infra/observe/memory directly.
type ModelProfile = memory.ModelProfile

// App holds the assembled application core. Entrypoints use the Runner and
// SessionService to drive conversations through their own I/O layer.
type App struct {
	Runner         *runner.Runner
	SessionService session.Service
	Agent          agent.Agent
	MemoryPlugin   *memory.MemoryPlugin
	PluginConfig   runner.PluginConfig
	AppName        string
}

// Config holds the parameters that vary between entrypoints.
type Config struct {
	// AgentDir is the base directory for agent definitions (typically ".").
	AgentDir string

	// AgentName is the directory name under agents/ (e.g. "demo_agent").
	// Used in legacy single-agent mode. Ignored when agenttree.yaml exists.
	AgentName string

	// AppName is the ADK application name (e.g. "telegram_bot_app").
	AppName string

	// SessionService is the session backend. Entrypoints choose between
	// InMemoryService, JSONLService, etc.
	SessionService session.Service
}

// New assembles the full application core. It checks for agenttree.yaml first;
// if present, it builds the full agent tree. Otherwise, it falls back to the
// legacy single-agent mode for backward compatibility.
func New(ctx context.Context, apiKey string, cfg Config) (*App, error) {
	// --- Check for agent tree config ---
	treeCfg, err := agentreeloader.Load(cfg.AgentDir)
	if err != nil {
		return nil, fmt.Errorf("appwire: loading agent tree config: %w", err)
	}

	if treeCfg != nil {
		slog.Info("appwire: agent tree config found, building multi-agent tree")
		return newFromTree(ctx, apiKey, cfg, treeCfg)
	}

	// --- Legacy single-agent mode ---
	slog.Info("appwire: no agent tree config, using legacy single-agent mode",
		"agent", cfg.AgentName)
	return newLegacy(ctx, apiKey, cfg)
}

// newFromTree builds the application using the declarative agent tree config.
func newFromTree(ctx context.Context, apiKey string, cfg Config, treeCfg *domain.AgentTreeConfig) (*App, error) {
	// --- genai client ---
	genaiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("appwire: creating genai client: %w", err)
	}

	// --- Model profile (use default model from tree config) ---
	reg, err := memory.NewRegistry()
	if err != nil {
		return nil, fmt.Errorf("appwire: creating registry: %w", err)
	}
	profile, err := reg.GetProfile(treeCfg.Defaults.Model)
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

	// --- Shell tool ---
	shellTool, err := functiontool.New(functiontool.Config{
		Name:                "shell_exec",
		Description:         "Execute a shell command and return stdout and exit code.",
		RequireConfirmation: false,
	}, shell.ToolHandlerFunc)
	if err != nil {
		return nil, fmt.Errorf("appwire: creating shell tool: %w", err)
	}

	// --- Build MCP toolset registry ---
	toolsetRegistry := make(map[string]tool.Toolset)
	mcpCfg, err := mcpconfig.Load(cfg.AgentDir, treeCfg.Root.Name)
	if err != nil {
		// Try loading from root agent's mcp.json; if not found, try loading from base dir
		slog.Debug("appwire: no MCP config for root agent, trying legacy path")
	}
	if mcpCfg == nil && cfg.AgentName != "" {
		mcpCfg, err = mcpconfig.Load(cfg.AgentDir, cfg.AgentName)
		if err != nil {
			slog.Debug("appwire: no MCP config for legacy agent either")
		}
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

	// --- Build agent tree ---
	deps := agenttree.Deps{
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

	rootAgent, err := agenttree.Build(treeCfg, deps)
	if err != nil {
		return nil, fmt.Errorf("appwire: building agent tree: %w", err)
	}

	// --- Debug plugin ---
	debugPl, err := debug.New()
	if err != nil {
		return nil, fmt.Errorf("appwire: creating debug plugin: %w", err)
	}

	// --- Runner ---
	pluginCfg := runner.PluginConfig{
		Plugins: []*plugin.Plugin{memPl, debugPl},
	}
	r, err := runner.New(runner.Config{
		AppName:        cfg.AppName,
		Agent:          rootAgent,
		SessionService: cfg.SessionService,
		PluginConfig:   pluginCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("appwire: creating runner: %w", err)
	}

	return &App{
		Runner:         r,
		SessionService: cfg.SessionService,
		Agent:          rootAgent,
		MemoryPlugin:   memPlugin,
		PluginConfig:   pluginCfg,
		AppName:        cfg.AppName,
	}, nil
}

// newLegacy builds the application using the original single-agent mode.
// This preserves full backward compatibility with existing entrypoints.
func newLegacy(ctx context.Context, apiKey string, cfg Config) (*App, error) {
	// --- Load agent definition ---
	loader := &agentdef.Loader{}
	def, err := loader.Load(cfg.AgentDir, cfg.AgentName)
	if err != nil {
		return nil, fmt.Errorf("appwire: loading agent definition: %w", err)
	}

	// --- Load MCP server config (optional: absent file returns nil, nil) ---
	mcpCfg, err := mcpconfig.Load(cfg.AgentDir, cfg.AgentName)
	if err != nil {
		return nil, fmt.Errorf("appwire: %w", err)
	}

	// --- Build MCP toolsets ---
	var toolsets []tool.Toolset
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
			toolsets = append(toolsets, ts)
		}
	}

	// --- genai client (for compress worker) ---
	genaiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("appwire: creating genai client: %w", err)
	}

	// --- Model profile ---
	reg, err := memory.NewRegistry()
	if err != nil {
		return nil, fmt.Errorf("appwire: creating registry: %w", err)
	}
	profile, err := reg.GetProfile(def.ModelID)
	if err != nil {
		return nil, fmt.Errorf("appwire: getting profile: %w", err)
	}
	profile.CompressModelID = "gemini-3.1-flash-lite-preview"

	// --- Compress strategy + memory plugin ---
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

	// --- LLM model ---
	llmModel, err := gemini.NewModel(ctx, profile.ModelID, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("appwire: creating Gemini model: %w", err)
	}

	// --- Shell tool ---
	shellTool, err := functiontool.New(functiontool.Config{
		Name:                "shell_exec",
		Description:         "Execute a shell command and return stdout and exit code.",
		RequireConfirmation: false,
	}, shell.ToolHandlerFunc)
	if err != nil {
		return nil, fmt.Errorf("appwire: creating shell tool: %w", err)
	}

	// --- LLM agent ---
	a, err := llmagent.New(llmagent.Config{
		Name:        def.Name,
		Model:       llmModel,
		Instruction: def.Instruction,
		Description: "",
		Tools:       []tool.Tool{shellTool},
		Toolsets:    toolsets,
	})
	if err != nil {
		return nil, fmt.Errorf("appwire: creating LLM agent: %w", err)
	}

	// --- Debug plugin: dump the final LLM request (no truncation) ---
	debugPl, err := debug.New()
	if err != nil {
		return nil, fmt.Errorf("appwire: creating debug plugin: %w", err)
	}

	// --- Runner ---
	pluginCfg := runner.PluginConfig{
		Plugins: []*plugin.Plugin{memPl, debugPl},
	}
	r, err := runner.New(runner.Config{
		AppName:        cfg.AppName,
		Agent:          a,
		SessionService: cfg.SessionService,
		PluginConfig:   pluginCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("appwire: creating runner: %w", err)
	}

	return &App{
		Runner:         r,
		SessionService: cfg.SessionService,
		Agent:          a,
		MemoryPlugin:   memPlugin,
		PluginConfig:   pluginCfg,
		AppName:        cfg.AppName,
	}, nil
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
