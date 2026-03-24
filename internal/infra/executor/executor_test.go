package executor_test

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/adk/session"

	"github.com/neokn/agenticsystem/internal/core/application/agenttree"
	"github.com/neokn/agenticsystem/internal/core/domain"
	"github.com/neokn/agenticsystem/internal/infra/executor"
)

// ---------------------------------------------------------------------------
// Compile-time interface check
// ---------------------------------------------------------------------------

// Verify ADKExecutor satisfies the orchestrator.Executor interface shape.
type executorIface interface {
	Execute(ctx context.Context, cfg *domain.AgentNodeConfig) (map[string]any, error)
}

var _ executorIface = (*executor.ADKExecutor)(nil)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// errorModelFactory returns a Deps whose ModelFactory always errors.
// This exercises the "builder error" path without requiring a real LLM.
func errorModelDeps() agenttree.Deps {
	return agenttree.Deps{
		ModelFactory: func(_ string) (model.LLM, error) {
			return nil, errors.New("stub: no model available")
		},
	}
}

// ---------------------------------------------------------------------------
// TestADKExecutor_NilConfig — nil cfg must return an error.
// ---------------------------------------------------------------------------

func TestADKExecutor_NilConfig(t *testing.T) {
	t.Parallel()

	// Arrange
	e := &executor.ADKExecutor{
		BuilderDeps:    errorModelDeps(),
		SessionService: session.InMemoryService(),
		AppName:        "test-app",
	}

	// Act
	_, err := e.Execute(context.Background(), nil)

	// Assert
	if err == nil {
		t.Fatal("Execute(nil) should return an error, but got nil")
	}
}

// ---------------------------------------------------------------------------
// TestADKExecutor_BuilderError — when agenttree.Build returns an error,
// Execute must propagate it.
// ---------------------------------------------------------------------------

func TestADKExecutor_BuilderError(t *testing.T) {
	t.Parallel()

	// Arrange — ModelFactory always fails, so Build will error on any LLM node.
	e := &executor.ADKExecutor{
		BuilderDeps:    errorModelDeps(),
		SessionService: session.InMemoryService(),
		AppName:        "test-app",
		Defaults: domain.AgentDefaults{
			Model: "some-model",
		},
	}

	cfg := &domain.AgentNodeConfig{
		Name:        "root",
		Type:        domain.AgentTypeLLM,
		Instruction: "you are helpful",
	}

	// Act
	_, err := e.Execute(context.Background(), cfg)

	// Assert
	if err == nil {
		t.Fatal("Execute with a broken ModelFactory should return an error, but got nil")
	}
}
