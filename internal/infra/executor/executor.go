// Package executor provides an infrastructure adapter that satisfies the
// orchestrator.Executor port by wiring together the agenttree.Builder and
// the ADK runner.
//
// Architecture: Infrastructure Layer.
// Depends on: application/agenttree (builder), domain (config types), ADK (runner, session).
package executor

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"

	"github.com/neokn/agenticsystem/internal/core/application/agenttree"
	"github.com/neokn/agenticsystem/internal/core/domain"
)

// defaultUserID is used as the ADK user identifier for orchestrator-initiated sessions.
const defaultUserID = "orchestrator"

// ADKExecutor implements orchestrator.Executor using the existing
// agenttree.Builder and the ADK Runner.
//
// Each call to Execute:
//  1. Wraps the AgentNodeConfig in an AgentTreeConfig.
//  2. Builds the ADK agent tree via agenttree.Build.
//  3. Creates a per-invocation ADK runner.
//  4. Opens a fresh session.
//  5. Runs the agent tree, draining all events.
//  6. Returns the session state as map[string]any.
type ADKExecutor struct {
	// BuilderDeps is passed through to agenttree.Build.
	BuilderDeps agenttree.Deps

	// SessionService manages ADK sessions.
	SessionService session.Service

	// Plugins are forwarded to the runner's PluginConfig.
	Plugins []*plugin.Plugin

	// AppName is the ADK application name used for session namespacing.
	AppName string

	// Defaults supplies fallback model ID and other tree-wide defaults.
	Defaults domain.AgentDefaults
}

// Execute builds and runs an ADK agent tree for the given AgentNodeConfig.
// It returns the session state accumulated during the run as map[string]any.
//
// Error handling (Level 1 — Error Reporting):
// All errors are wrapped with context and propagated to the caller.
func (e *ADKExecutor) Execute(ctx context.Context, cfg *domain.AgentNodeConfig) (map[string]any, error) {
	// Guard: nil config is a caller error.
	if cfg == nil {
		return nil, fmt.Errorf("executor: cfg must not be nil")
	}

	// -----------------------------------------------------------------------
	// Step 1: Wrap cfg in AgentTreeConfig — the builder expects the full tree.
	// -----------------------------------------------------------------------
	treeCfg := &domain.AgentTreeConfig{
		Version:  "1",
		Defaults: e.Defaults,
		Root:     *cfg,
	}

	// -----------------------------------------------------------------------
	// Step 2: Build the ADK agent tree.
	// -----------------------------------------------------------------------
	rootAgent, err := agenttree.Build(treeCfg, e.BuilderDeps)
	if err != nil {
		return nil, fmt.Errorf("executor: building agent tree: %w", err)
	}

	// -----------------------------------------------------------------------
	// Step 3: Create a per-invocation runner.
	// -----------------------------------------------------------------------
	r, err := runner.New(runner.Config{
		AppName:        e.AppName,
		Agent:          rootAgent,
		SessionService: e.SessionService,
		PluginConfig:   runner.PluginConfig{Plugins: e.Plugins},
	})
	if err != nil {
		return nil, fmt.Errorf("executor: creating runner: %w", err)
	}

	// -----------------------------------------------------------------------
	// Step 4: Open a fresh session for this execution.
	// -----------------------------------------------------------------------
	createResp, err := e.SessionService.Create(ctx, &session.CreateRequest{
		AppName: e.AppName,
		UserID:  defaultUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("executor: creating session: %w", err)
	}
	sessID := createResp.Session.ID()

	// -----------------------------------------------------------------------
	// Step 5: Run the agent tree — drain all events until completion.
	//
	// The agent tree derives its work context from instruction + session state,
	// not from a user message, so we pass nil for the message content.
	// -----------------------------------------------------------------------
	for event, err := range r.Run(ctx, defaultUserID, sessID, nil, agent.RunConfig{}) {
		if err != nil {
			return nil, fmt.Errorf("executor: running agent: %w", err)
		}
		_ = event // consume events; state is written to the session service
	}

	// -----------------------------------------------------------------------
	// Step 6: Read the updated session to capture state written by agents.
	// -----------------------------------------------------------------------
	getResp, err := e.SessionService.Get(ctx, &session.GetRequest{
		AppName:   e.AppName,
		UserID:    defaultUserID,
		SessionID: sessID,
	})
	if err != nil {
		return nil, fmt.Errorf("executor: reading session: %w", err)
	}

	// -----------------------------------------------------------------------
	// Step 7: Collect session state into map[string]any.
	//
	// session.State.All() yields all key-value pairs via iter.Seq2.
	// We skip keys with the "temp:" prefix since they are discarded after
	// each invocation and are not meaningful output.
	// -----------------------------------------------------------------------
	results := make(map[string]any)
	for k, v := range getResp.Session.State().All() {
		if strings.HasPrefix(k, session.KeyPrefixTemp) {
			continue
		}
		results[k] = v
	}

	return results, nil
}
