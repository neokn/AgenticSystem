package agenttree_test

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"

	"github.com/neokn/agenticsystem/internal/core/application/agenttree"
	"github.com/neokn/agenticsystem/internal/core/domain"
)

// stubModel implements model.LLM for testing.
type stubModel struct{}

func (m *stubModel) Name() string { return "stub-model" }
func (m *stubModel) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {}
}

// stubModelFactory returns a factory that always returns a stubModel.
func stubModelFactory() func(string) (model.LLM, error) {
	return func(_ string) (model.LLM, error) {
		return &stubModel{}, nil
	}
}

// stubPromptLoader returns a loader that always returns a fixed instruction.
func stubPromptLoader(instruction string) func(string, string) (string, error) {
	return func(_, _ string) (string, error) {
		return instruction, nil
	}
}

func baseDeps() agenttree.Deps {
	return agenttree.Deps{
		ModelFactory:    stubModelFactory(),
		PromptLoader:   stubPromptLoader("You are a test agent."),
		ToolRegistry:    map[string]tool.Tool{},
		ToolsetRegistry: map[string]tool.Toolset{},
		BaseDir:         ".",
	}
}

func TestBuild_should_return_error_for_nil_config(t *testing.T) {
	_, err := agenttree.Build(nil, baseDeps())
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestBuild_should_build_single_llm_agent(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Defaults: domain.AgentDefaults{
			Model: "test-model",
		},
		Root: domain.AgentNodeConfig{
			Name:        "root",
			Type:        domain.AgentTypeLLM,
			Description: "Root agent",
		},
	}

	a, err := agenttree.Build(cfg, baseDeps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name() != "root" {
		t.Errorf("expected name 'root', got %q", a.Name())
	}
	if a.Description() != "Root agent" {
		t.Errorf("expected description 'Root agent', got %q", a.Description())
	}
}

func TestBuild_should_build_sequential_agent_with_children(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Defaults: domain.AgentDefaults{
			Model: "test-model",
		},
		Root: domain.AgentNodeConfig{
			Name: "workflow",
			Type: domain.AgentTypeSequential,
			SubAgents: []domain.AgentNodeConfig{
				{Name: "step1", Type: domain.AgentTypeLLM, Description: "Step 1"},
				{Name: "step2", Type: domain.AgentTypeLLM, Description: "Step 2"},
			},
		},
	}

	a, err := agenttree.Build(cfg, baseDeps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name() != "workflow" {
		t.Errorf("expected name 'workflow', got %q", a.Name())
	}
	subs := a.SubAgents()
	if len(subs) != 2 {
		t.Fatalf("expected 2 sub-agents, got %d", len(subs))
	}
	if subs[0].Name() != "step1" {
		t.Errorf("expected sub-agent 'step1', got %q", subs[0].Name())
	}
	if subs[1].Name() != "step2" {
		t.Errorf("expected sub-agent 'step2', got %q", subs[1].Name())
	}
}

func TestBuild_should_build_loop_agent(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Defaults: domain.AgentDefaults{
			Model: "test-model",
		},
		Root: domain.AgentNodeConfig{
			Name:          "iter",
			Type:          domain.AgentTypeLoop,
			MaxIterations: 5,
			SubAgents: []domain.AgentNodeConfig{
				{Name: "worker", Type: domain.AgentTypeLLM},
				{Name: "evaluator", Type: domain.AgentTypeLLM},
			},
		},
	}

	a, err := agenttree.Build(cfg, baseDeps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name() != "iter" {
		t.Errorf("expected name 'iter', got %q", a.Name())
	}
	if len(a.SubAgents()) != 2 {
		t.Errorf("expected 2 sub-agents, got %d", len(a.SubAgents()))
	}
}

func TestBuild_should_build_nested_workflow_tree(t *testing.T) {
	// Root (LLM) -> PER Workflow (Sequential) -> [Planner (LLM), Loop[Worker, Evaluator], Reporter (LLM)]
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Defaults: domain.AgentDefaults{
			Model: "test-model",
		},
		Root: domain.AgentNodeConfig{
			Name:        "root",
			Type:        domain.AgentTypeLLM,
			Description: "Root agent",
			SubAgents: []domain.AgentNodeConfig{
				{
					Name: "plan_execute_report",
					Type: domain.AgentTypeSequential,
					SubAgents: []domain.AgentNodeConfig{
						{Name: "planner", Type: domain.AgentTypeLLM, OutputKey: "plan"},
						{
							Name:          "refinement_loop",
							Type:          domain.AgentTypeLoop,
							MaxIterations: 3,
							SubAgents: []domain.AgentNodeConfig{
								{Name: "worker", Type: domain.AgentTypeLLM, OutputKey: "draft"},
								{Name: "evaluator", Type: domain.AgentTypeLLM, OutputKey: "evaluation"},
							},
						},
						{Name: "reporter", Type: domain.AgentTypeLLM, OutputKey: "summary"},
					},
				},
			},
		},
	}

	a, err := agenttree.Build(cfg, baseDeps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the full tree structure
	if a.Name() != "root" {
		t.Errorf("root name: expected 'root', got %q", a.Name())
	}
	if len(a.SubAgents()) != 1 {
		t.Fatalf("root sub-agents: expected 1, got %d", len(a.SubAgents()))
	}

	per := a.SubAgents()[0]
	if per.Name() != "plan_execute_report" {
		t.Errorf("PER name: expected 'plan_execute_report', got %q", per.Name())
	}
	if len(per.SubAgents()) != 3 {
		t.Fatalf("PER sub-agents: expected 3, got %d", len(per.SubAgents()))
	}

	loop := per.SubAgents()[1]
	if loop.Name() != "refinement_loop" {
		t.Errorf("loop name: expected 'refinement_loop', got %q", loop.Name())
	}
	if len(loop.SubAgents()) != 2 {
		t.Errorf("loop sub-agents: expected 2, got %d", len(loop.SubAgents()))
	}
}

func TestBuild_should_use_node_model_over_default(t *testing.T) {
	modelRequested := ""
	deps := baseDeps()
	deps.ModelFactory = func(id string) (model.LLM, error) {
		modelRequested = id
		return &stubModel{}, nil
	}

	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Defaults: domain.AgentDefaults{
			Model: "default-model",
		},
		Root: domain.AgentNodeConfig{
			Name:  "root",
			Type:  domain.AgentTypeLLM,
			Model: "override-model",
		},
	}

	_, err := agenttree.Build(cfg, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if modelRequested != "override-model" {
		t.Errorf("expected model 'override-model', got %q", modelRequested)
	}
}

func TestBuild_should_fail_when_tool_not_in_registry(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Defaults: domain.AgentDefaults{
			Model: "test-model",
		},
		Root: domain.AgentNodeConfig{
			Name:  "root",
			Type:  domain.AgentTypeLLM,
			Tools: []string{"nonexistent_tool"},
		},
	}

	_, err := agenttree.Build(cfg, baseDeps())
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
}
