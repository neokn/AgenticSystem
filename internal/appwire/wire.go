// Package appwire assembles the shared application core used by all entrypoints
// (CLI, Web UI, Telegram, etc.). Each entrypoint only provides the I/O layer;
// the agent, plugins, tools, and runner are wired identically.
package appwire

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/adk/tool/mcptoolset"

	"github.com/neokn/agenticsystem/internal/agentdef"
	"github.com/neokn/agenticsystem/internal/mcpconfig"
	"github.com/neokn/agenticsystem/internal/memory"
	"github.com/neokn/agenticsystem/internal/shelltool"
)

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
	AgentName string

	// AppName is the ADK application name (e.g. "telegram_bot_app").
	AppName string

	// SessionService is the session backend. Entrypoints choose between
	// InMemoryService, JSONLService, etc.
	SessionService session.Service
}

// New assembles the full application core: agent definition, genai client,
// model profile, memory plugin, instruction override, shell tool, LLM agent,
// and runner. Returns an App ready for the entrypoint to drive.
func New(ctx context.Context, apiKey string, cfg Config) (*App, error) {
	// --- Load agent definition ---
	def, err := agentdef.Load(cfg.AgentDir, cfg.AgentName)
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
	}, shelltool.ToolHandlerFunc)
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
	debugPl, err := plugin.New(plugin.Config{
		Name: "debug_request_dump",
		BeforeModelCallback: func(_ agent.CallbackContext, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
			fmt.Fprintln(os.Stderr, "========== DEBUG: LLM REQUEST START ==========")

			// SystemInstruction
			if req.Config != nil && req.Config.SystemInstruction != nil {
				fmt.Fprintf(os.Stderr, "--- SystemInstruction (role=%s, %d parts) ---\n",
					req.Config.SystemInstruction.Role, len(req.Config.SystemInstruction.Parts))
				for i, p := range req.Config.SystemInstruction.Parts {
					if p.Text != "" {
						fmt.Fprintf(os.Stderr, "  [SI part %d] TEXT (%d chars):\n%s\n", i, len(p.Text), p.Text)
					}
					if p.FunctionCall != nil {
						fmt.Fprintf(os.Stderr, "  [SI part %d] FUNCTION_CALL: %s\n", i, p.FunctionCall.Name)
					}
					if p.FunctionResponse != nil {
						fmt.Fprintf(os.Stderr, "  [SI part %d] FUNCTION_RESPONSE: %s\n", i, p.FunctionResponse.Name)
					}
					if p.InlineData != nil {
						fmt.Fprintf(os.Stderr, "  [SI part %d] INLINE_DATA: mime=%s, %d bytes\n", i, p.InlineData.MIMEType, len(p.InlineData.Data))
					}
				}
			} else {
				fmt.Fprintln(os.Stderr, "--- SystemInstruction: nil ---")
			}

			// Contents
			fmt.Fprintf(os.Stderr, "--- Contents (%d entries) ---\n", len(req.Contents))
			for i, c := range req.Contents {
				role := c.Role
				if role == "" {
					role = "(empty)"
				}
				fmt.Fprintf(os.Stderr, "  [%d] role=%s, %d parts\n", i, role, len(c.Parts))
				for j, p := range c.Parts {
					if p.Text != "" {
						fmt.Fprintf(os.Stderr, "    [%d.%d] TEXT (%d chars):\n%s\n", i, j, len(p.Text), p.Text)
					}
					if p.FunctionCall != nil {
						args := ""
						if p.FunctionCall.Args != nil {
							argsBytes, _ := json.Marshal(p.FunctionCall.Args)
							args = string(argsBytes)
						}
						fmt.Fprintf(os.Stderr, "    [%d.%d] FUNCTION_CALL: %s args=%s\n", i, j, p.FunctionCall.Name, args)
					}
					if p.FunctionResponse != nil {
						resp := ""
						if p.FunctionResponse.Response != nil {
							respBytes, _ := json.Marshal(p.FunctionResponse.Response)
							resp = string(respBytes)
						}
						fmt.Fprintf(os.Stderr, "    [%d.%d] FUNCTION_RESPONSE: %s response=%s\n", i, j, p.FunctionResponse.Name, resp)
					}
					if p.InlineData != nil {
						fmt.Fprintf(os.Stderr, "    [%d.%d] INLINE_DATA: mime=%s, %d bytes\n", i, j, p.InlineData.MIMEType, len(p.InlineData.Data))
					}
					if len(p.ThoughtSignature) > 0 {
						fmt.Fprintf(os.Stderr, "    [%d.%d] THOUGHT_SIGNATURE: %d bytes\n", i, j, len(p.ThoughtSignature))
					}
				}
			}

			// Model
			fmt.Fprintf(os.Stderr, "--- Model: %s ---\n", req.Model)

			fmt.Fprintln(os.Stderr, "========== DEBUG: LLM REQUEST END ==========")
			return nil, nil
		},
	})
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
