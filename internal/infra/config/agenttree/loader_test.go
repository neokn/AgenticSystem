package agenttree_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/neokn/agenticsystem/internal/core/domain"
	"github.com/neokn/agenticsystem/internal/infra/config/agenttree"
)

func TestLoad_should_return_nil_when_file_missing(t *testing.T) {
	cfg, err := agenttree.Load(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil for missing file")
	}
}

func TestLoad_should_parse_valid_yaml(t *testing.T) {
	dir := t.TempDir()
	yaml := `
version: "1"
defaults:
  model: "test-model"
root:
  name: root
  type: llm
  description: "Root agent"
  sub_agents:
    - name: worker
      type: llm
      description: "Worker"
      output_key: draft
`
	if err := os.WriteFile(filepath.Join(dir, "agenttree.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := agenttree.Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Version != "1" {
		t.Errorf("expected version '1', got %q", cfg.Version)
	}
	if cfg.Root.Name != "root" {
		t.Errorf("expected root name 'root', got %q", cfg.Root.Name)
	}
	if cfg.Root.Type != domain.AgentTypeLLM {
		t.Errorf("expected root type 'llm', got %q", cfg.Root.Type)
	}
	if len(cfg.Root.SubAgents) != 1 {
		t.Fatalf("expected 1 sub-agent, got %d", len(cfg.Root.SubAgents))
	}
	if cfg.Root.SubAgents[0].OutputKey != "draft" {
		t.Errorf("expected output_key 'draft', got %q", cfg.Root.SubAgents[0].OutputKey)
	}
}

func TestLoad_should_reject_invalid_yaml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agenttree.yaml"), []byte("{{invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := agenttree.Load(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoad_should_reject_invalid_config(t *testing.T) {
	dir := t.TempDir()
	yaml := `
version: ""
root:
  name: root
  type: llm
`
	if err := os.WriteFile(filepath.Join(dir, "agenttree.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := agenttree.Load(dir)
	if err == nil {
		t.Fatal("expected error for invalid config (empty version)")
	}
}

func TestLoad_should_parse_complex_nested_tree(t *testing.T) {
	dir := t.TempDir()
	yaml := `
version: "1"
defaults:
  model: "gemini-3-flash"
root:
  name: root
  type: llm
  description: "Root"
  sub_agents:
    - name: per_workflow
      type: sequential
      description: "Plan-Execute-Report"
      sub_agents:
        - name: planner
          type: llm
          output_key: plan
        - name: loop
          type: loop
          max_iterations: 3
          sub_agents:
            - name: worker
              type: llm
              output_key: draft
            - name: evaluator
              type: llm
              output_key: evaluation
        - name: reporter
          type: llm
          output_key: summary
`
	if err := os.WriteFile(filepath.Join(dir, "agenttree.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := agenttree.Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tree structure
	if len(cfg.Root.SubAgents) != 1 {
		t.Fatalf("expected 1 top-level sub-agent, got %d", len(cfg.Root.SubAgents))
	}
	per := cfg.Root.SubAgents[0]
	if per.Type != domain.AgentTypeSequential {
		t.Errorf("expected sequential, got %q", per.Type)
	}
	if len(per.SubAgents) != 3 {
		t.Fatalf("expected 3 PER children, got %d", len(per.SubAgents))
	}
	loop := per.SubAgents[1]
	if loop.Type != domain.AgentTypeLoop {
		t.Errorf("expected loop, got %q", loop.Type)
	}
	if loop.MaxIterations != 3 {
		t.Errorf("expected max_iterations 3, got %d", loop.MaxIterations)
	}
}
