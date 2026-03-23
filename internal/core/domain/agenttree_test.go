package domain_test

import (
	"testing"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

func TestAgentTreeConfig_Validate_should_accept_valid_config(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Root: domain.AgentNodeConfig{
			Name:        "root",
			Type:        domain.AgentTypeLLM,
			Description: "Root agent",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestAgentTreeConfig_Validate_should_reject_empty_version(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Root: domain.AgentNodeConfig{
			Name: "root",
			Type: domain.AgentTypeLLM,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty version")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "version" {
		t.Errorf("expected field 'version', got %q", ve.Field)
	}
}

func TestAgentTreeConfig_Validate_should_reject_empty_root_name(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Root: domain.AgentNodeConfig{
			Type: domain.AgentTypeLLM,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty root name")
	}
}

func TestAgentTreeConfig_Validate_should_reject_unknown_agent_type(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Root: domain.AgentNodeConfig{
			Name: "root",
			Type: "unknown",
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}

func TestAgentTreeConfig_Validate_should_reject_duplicate_agent_names(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Root: domain.AgentNodeConfig{
			Name: "root",
			Type: domain.AgentTypeSequential,
			SubAgents: []domain.AgentNodeConfig{
				{Name: "worker", Type: domain.AgentTypeLLM},
				{Name: "worker", Type: domain.AgentTypeLLM},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate names")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Reason != "duplicate agent name: worker" {
		t.Errorf("unexpected reason: %q", ve.Reason)
	}
}

func TestAgentTreeConfig_Validate_should_reject_sequential_without_subagents(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Root: domain.AgentNodeConfig{
			Name: "root",
			Type: domain.AgentTypeSequential,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for sequential without sub_agents")
	}
}

func TestAgentTreeConfig_Validate_should_accept_deeply_nested_tree(t *testing.T) {
	cfg := &domain.AgentTreeConfig{
		Version: "1",
		Root: domain.AgentNodeConfig{
			Name: "root",
			Type: domain.AgentTypeLLM,
			SubAgents: []domain.AgentNodeConfig{
				{
					Name: "workflow_1",
					Type: domain.AgentTypeSequential,
					SubAgents: []domain.AgentNodeConfig{
						{Name: "planner", Type: domain.AgentTypeLLM},
						{
							Name: "iter_loop",
							Type: domain.AgentTypeLoop,
							SubAgents: []domain.AgentNodeConfig{
								{Name: "worker", Type: domain.AgentTypeLLM},
								{Name: "evaluator", Type: domain.AgentTypeLLM},
							},
							MaxIterations: 5,
						},
						{Name: "reporter", Type: domain.AgentTypeLLM},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error for valid nested config, got %v", err)
	}
}

func TestDefaultStateKeys_should_return_canonical_keys(t *testing.T) {
	keys := domain.DefaultStateKeys()
	if keys.UserIntent != "user_intent" {
		t.Errorf("expected user_intent, got %q", keys.UserIntent)
	}
	if keys.Plan != "plan" {
		t.Errorf("expected plan, got %q", keys.Plan)
	}
	if keys.Draft != "draft" {
		t.Errorf("expected draft, got %q", keys.Draft)
	}
	if keys.Evaluation != "evaluation" {
		t.Errorf("expected evaluation, got %q", keys.Evaluation)
	}
	if keys.Summary != "summary" {
		t.Errorf("expected summary, got %q", keys.Summary)
	}
}
