# Dynamic Orchestration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace static YAML-based agent tree with Root LLM dynamic orchestration — Root outputs structured Plan JSON, system builds and executes ADK agent tree, Root supervises via outer loop.

**Architecture:** Orchestrator (Go) drives a 4-phase loop: Plan (Root LLM → structured JSON) → Execute (dynamic ADK agent tree) → Evaluate (Root LLM → satisfied?) → Respond (Root LLM → natural language). PlanConverter bridges Plan JSON → existing AgentNodeConfig → Builder.

**Tech Stack:** Go, Google ADK v1.0.0, Gemini structured output (`response_schema`), existing agenttree.Builder

**Spec:** `docs/superpowers/specs/2026-03-25-dynamic-orchestration-design.md`

---

## File Structure

### New Files

| File | Responsibility |
|------|----------------|
| `internal/core/domain/plan.go` | PlanNode domain types + validation (stdlib only) |
| `internal/core/domain/plan_test.go` | PlanNode validation tests |
| `internal/core/application/orchestrator/converter.go` | PlanNode → AgentNodeConfig conversion + exit-checker injection |
| `internal/core/application/orchestrator/converter_test.go` | Converter unit tests |
| `internal/core/application/orchestrator/exitchecker.go` | Loop exit-checker custom agent (agent.New) |
| `internal/core/application/orchestrator/exitchecker_test.go` | Exit-checker unit tests |
| `internal/core/application/orchestrator/orchestrator.go` | Orchestrator: 4-phase loop driver |
| `internal/core/application/orchestrator/orchestrator_test.go` | Orchestrator unit tests (mocked LLM) |
| `internal/infra/llm/schema.go` | Gemini response_schema definitions for Plan + Evaluate |
| `internal/infra/llm/schema_test.go` | Schema structure tests |
| `prompts/plan.prompt` | Plan phase system prompt |
| `prompts/evaluate.prompt` | Evaluate phase system prompt |
| `prompts/respond.prompt` | Respond phase system prompt |

### Modified Files

| File | Change |
|------|--------|
| `internal/core/application/wire.go` | Remove legacy + YAML modes, wire Orchestrator only |
| `internal/core/application/wire_test.go` | Update tests for Orchestrator mode |
| `cmd/agent/main.go` | Call `orchestrator.Run()` instead of `runner.Run()` |
| `cmd/telegram/main.go` | Call `orchestrator.Run()` instead of `runner.Run()` |
| `cmd/web/main.go` | Call `orchestrator.Run()` instead of `runner.Run()` |

### Removed Files

| File | Reason |
|------|--------|
| `agenttree.yaml` | Static config replaced by dynamic plan |
| `internal/infra/config/agenttree/loader.go` | YAML loader no longer needed |
| `internal/infra/config/agenttree/loader_test.go` | Tests for removed loader |
| `agents/root/agent.prompt` | Root is now Orchestrator LLM calls |
| `agents/planner/agent.prompt` | Replaced by `prompts/plan.prompt` |
| `agents/executor/agent.prompt` | Dynamically generated |
| `agents/reporter/agent.prompt` | Replaced by `prompts/respond.prompt` |
| `agents/worker/agent.prompt` | Dynamically generated |
| `agents/evaluator/agent.prompt` | Replaced by `prompts/evaluate.prompt` |

---

## Task 1: PlanNode Domain Types

**Files:**
- Create: `internal/core/domain/plan.go`
- Create: `internal/core/domain/plan_test.go`

- [ ] **Step 1: Write failing tests for PlanNode validation**

```go
// internal/core/domain/plan_test.go
package domain

import "testing"

func TestPlanOutput_Validate_ValidDirect(t *testing.T) {
	p := PlanOutput{
		Intent:     "test",
		MaxRetries: 0,
		Plan:       PlanNode{Type: PlanTypeDirect, Response: "hello"},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlanOutput_Validate_EmptyIntent(t *testing.T) {
	p := PlanOutput{Plan: PlanNode{Type: PlanTypeDirect, Response: "hi"}}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for empty intent")
	}
}

func TestPlanOutput_Validate_DirectMissingResponse(t *testing.T) {
	p := PlanOutput{Intent: "test", Plan: PlanNode{Type: PlanTypeDirect}}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for direct without response")
	}
}

func TestPlanNode_Validate_SequentialEmpty(t *testing.T) {
	p := PlanOutput{
		Intent: "test",
		Plan:   PlanNode{Type: PlanTypeSequential},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for sequential without steps")
	}
}

func TestPlanNode_Validate_LoopNoMaxIterations(t *testing.T) {
	p := PlanOutput{
		Intent: "test",
		Plan: PlanNode{
			Type:  PlanTypeLoop,
			Steps: []PlanNode{{Type: PlanTypeStep, Role: "a", OutputKey: "x"}},
		},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for loop without max_iterations")
	}
}

func TestPlanNode_Validate_StepMissingRole(t *testing.T) {
	p := PlanOutput{
		Intent: "test",
		Plan: PlanNode{
			Type: PlanTypeSequential,
			Steps: []PlanNode{
				{Type: PlanTypeStep, OutputKey: "x"},
			},
		},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for step without role")
	}
}

func TestPlanNode_Validate_StepMissingOutputKey(t *testing.T) {
	p := PlanOutput{
		Intent: "test",
		Plan: PlanNode{
			Type: PlanTypeSequential,
			Steps: []PlanNode{
				{Type: PlanTypeStep, Role: "writer"},
			},
		},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for step without output_key")
	}
}

func TestPlanNode_Validate_DirectNestedForbidden(t *testing.T) {
	p := PlanOutput{
		Intent: "test",
		Plan: PlanNode{
			Type: PlanTypeSequential,
			Steps: []PlanNode{
				{Type: PlanTypeDirect, Response: "nested direct"},
			},
		},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for nested direct node")
	}
}

func TestPlanNode_Validate_NestedComposite(t *testing.T) {
	p := PlanOutput{
		Intent:     "complex task",
		MaxRetries: 2,
		Plan: PlanNode{
			Type: PlanTypeSequential,
			Steps: []PlanNode{
				{Type: PlanTypeStep, Role: "planner", OutputKey: "plan"},
				{
					Type:          PlanTypeLoop,
					MaxIterations: 3,
					ExitCondition: &ExitCondition{OutputKey: "eval", Pattern: "APPROVED"},
					Steps: []PlanNode{
						{Type: PlanTypeStep, Role: "coder", OutputKey: "draft"},
						{Type: PlanTypeStep, Role: "tester", OutputKey: "eval"},
					},
				},
				{
					Type: PlanTypeParallel,
					Steps: []PlanNode{
						{Type: PlanTypeStep, Role: "doc", OutputKey: "docs"},
						{Type: PlanTypeStep, Role: "log", OutputKey: "changelog"},
					},
				},
			},
		},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/domain/ -run TestPlan -v`
Expected: compilation error (types not defined)

- [ ] **Step 3: Implement PlanNode types and validation**

```go
// internal/core/domain/plan.go
package domain

// PlanNodeType enumerates the plan node types.
type PlanNodeType string

const (
	PlanTypeDirect     PlanNodeType = "direct"
	PlanTypeSequential PlanNodeType = "sequential"
	PlanTypeLoop       PlanNodeType = "loop"
	PlanTypeParallel   PlanNodeType = "parallel"
	PlanTypeStep       PlanNodeType = "step"
)

// PlanOutput is the top-level structured output from the Root LLM plan phase.
type PlanOutput struct {
	Intent     string   `json:"intent"`
	MaxRetries int      `json:"max_retries"`
	Plan       PlanNode `json:"plan"`
}

// PlanNode is a recursive node in the execution plan tree.
type PlanNode struct {
	Type PlanNodeType `json:"type"`

	// direct only
	Response string `json:"response,omitempty"`

	// sequential / loop / parallel
	Steps []PlanNode `json:"steps,omitempty"`

	// loop only
	MaxIterations uint           `json:"max_iterations,omitempty"`
	ExitCondition *ExitCondition `json:"exit_condition,omitempty"`

	// step only
	Role        string   `json:"role,omitempty"`
	Instruction string   `json:"instruction,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	OutputKey   string   `json:"output_key,omitempty"`
}

// ExitCondition defines early termination for a loop node.
type ExitCondition struct {
	OutputKey string `json:"output_key"`
	Pattern   string `json:"pattern"`
}

// EvalOutput is the structured output from the Root LLM evaluate phase.
type EvalOutput struct {
	Satisfied bool   `json:"satisfied"`
	Feedback  string `json:"feedback"`
}

// Validate checks PlanOutput for structural correctness.
func (p *PlanOutput) Validate() error {
	if p.Intent == "" {
		return &ValidationError{Field: "intent", Reason: "must not be empty"}
	}
	return validatePlanNode(&p.Plan, "plan", true)
}

func validatePlanNode(n *PlanNode, path string, isRoot bool) error {
	switch n.Type {
	case PlanTypeDirect:
		if !isRoot {
			return &ValidationError{Field: path, Reason: "direct is only valid at plan root level"}
		}
		if n.Response == "" {
			return &ValidationError{Field: path + ".response", Reason: "must not be empty for direct"}
		}
	case PlanTypeSequential, PlanTypeParallel:
		if len(n.Steps) == 0 {
			return &ValidationError{Field: path + ".steps", Reason: string(n.Type) + " must have steps"}
		}
		for i := range n.Steps {
			if err := validatePlanNode(&n.Steps[i], path+".steps["+itoa(i)+"]", false); err != nil {
				return err
			}
		}
	case PlanTypeLoop:
		if n.MaxIterations == 0 {
			return &ValidationError{Field: path + ".max_iterations", Reason: "must be > 0 for loop"}
		}
		if len(n.Steps) == 0 {
			return &ValidationError{Field: path + ".steps", Reason: "loop must have steps"}
		}
		for i := range n.Steps {
			if err := validatePlanNode(&n.Steps[i], path+".steps["+itoa(i)+"]", false); err != nil {
				return err
			}
		}
	case PlanTypeStep:
		if n.Role == "" {
			return &ValidationError{Field: path + ".role", Reason: "must not be empty"}
		}
		if n.OutputKey == "" {
			return &ValidationError{Field: path + ".output_key", Reason: "must not be empty"}
		}
	default:
		return &ValidationError{Field: path + ".type", Reason: "unsupported type: " + string(n.Type)}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/domain/ -run TestPlan -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/domain/plan.go internal/core/domain/plan_test.go
git commit -m "feat(domain): add PlanNode types and validation for dynamic orchestration"
```

---

## Task 2: PlanConverter — PlanNode to AgentNodeConfig

**Files:**
- Create: `internal/core/application/orchestrator/converter.go`
- Create: `internal/core/application/orchestrator/converter_test.go`

- [ ] **Step 1: Write failing tests for PlanConverter**

```go
// internal/core/application/orchestrator/converter_test.go
package orchestrator

import (
	"testing"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

func TestConvert_Step(t *testing.T) {
	plan := domain.PlanNode{
		Type: domain.PlanTypeStep, Role: "writer", Instruction: "write it",
		Tools: []string{"shell_exec"}, OutputKey: "draft",
	}
	cfg, err := Convert(&plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Type != domain.AgentTypeLLM {
		t.Errorf("expected llm, got %s", cfg.Type)
	}
	if cfg.Name != "writer_0" {
		t.Errorf("expected writer_0, got %s", cfg.Name)
	}
	if cfg.Instruction != "write it" {
		t.Errorf("instruction mismatch")
	}
	if cfg.OutputKey != "draft" {
		t.Errorf("output_key mismatch")
	}
}

func TestConvert_Sequential(t *testing.T) {
	plan := domain.PlanNode{
		Type: domain.PlanTypeSequential,
		Steps: []domain.PlanNode{
			{Type: domain.PlanTypeStep, Role: "a", OutputKey: "x"},
			{Type: domain.PlanTypeStep, Role: "b", OutputKey: "y"},
		},
	}
	cfg, err := Convert(&plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Type != domain.AgentTypeSequential {
		t.Errorf("expected sequential, got %s", cfg.Type)
	}
	if len(cfg.SubAgents) != 2 {
		t.Fatalf("expected 2 sub-agents, got %d", len(cfg.SubAgents))
	}
}

func TestConvert_LoopWithExitCondition(t *testing.T) {
	plan := domain.PlanNode{
		Type: domain.PlanTypeLoop, MaxIterations: 3,
		ExitCondition: &domain.ExitCondition{OutputKey: "eval", Pattern: "PASS"},
		Steps: []domain.PlanNode{
			{Type: domain.PlanTypeStep, Role: "worker", OutputKey: "draft"},
			{Type: domain.PlanTypeStep, Role: "checker", OutputKey: "eval"},
		},
	}
	cfg, err := Convert(&plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Type != domain.AgentTypeLoop {
		t.Errorf("expected loop, got %s", cfg.Type)
	}
	if cfg.MaxIterations != 3 {
		t.Errorf("expected max_iterations 3, got %d", cfg.MaxIterations)
	}
	// Loop body should be wrapped in a sequential with an extra exit-checker
	if len(cfg.SubAgents) != 1 {
		t.Fatalf("expected 1 sub-agent (sequential wrapper), got %d", len(cfg.SubAgents))
	}
	seq := cfg.SubAgents[0]
	if seq.Type != domain.AgentTypeSequential {
		t.Errorf("expected sequential wrapper, got %s", seq.Type)
	}
	// 2 original steps + 1 exit-checker
	if len(seq.SubAgents) != 3 {
		t.Errorf("expected 3 sub-agents (2 steps + exit-checker), got %d", len(seq.SubAgents))
	}
}

func TestConvert_LoopWithoutExitCondition(t *testing.T) {
	plan := domain.PlanNode{
		Type: domain.PlanTypeLoop, MaxIterations: 5,
		Steps: []domain.PlanNode{
			{Type: domain.PlanTypeStep, Role: "worker", OutputKey: "draft"},
		},
	}
	cfg, err := Convert(&plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	// No exit-checker injected, just the steps wrapped in sequential
	seq := cfg.SubAgents[0]
	if len(seq.SubAgents) != 1 {
		t.Errorf("expected 1 sub-agent (no exit-checker), got %d", len(seq.SubAgents))
	}
}

func TestConvert_Parallel(t *testing.T) {
	plan := domain.PlanNode{
		Type: domain.PlanTypeParallel,
		Steps: []domain.PlanNode{
			{Type: domain.PlanTypeStep, Role: "a", OutputKey: "x"},
			{Type: domain.PlanTypeStep, Role: "b", OutputKey: "y"},
		},
	}
	cfg, err := Convert(&plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Type != domain.AgentTypeParallel {
		t.Errorf("expected parallel, got %s", cfg.Type)
	}
	if len(cfg.SubAgents) != 2 {
		t.Fatalf("expected 2 sub-agents, got %d", len(cfg.SubAgents))
	}
}

func TestConvert_NameUniqueness(t *testing.T) {
	plan := domain.PlanNode{
		Type: domain.PlanTypeSequential,
		Steps: []domain.PlanNode{
			{Type: domain.PlanTypeStep, Role: "coder", OutputKey: "x"},
			{Type: domain.PlanTypeStep, Role: "coder", OutputKey: "y"},
		},
	}
	cfg, err := Convert(&plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	name0 := cfg.SubAgents[0].Name
	name1 := cfg.SubAgents[1].Name
	if name0 == name1 {
		t.Errorf("names should be unique, both are %q", name0)
	}
}

func TestConvert_InstructionFromTemplate(t *testing.T) {
	loader := func(baseDir, role string) (string, bool) {
		if role == "writer" {
			return "You are a professional writer.", true
		}
		return "", false
	}
	plan := domain.PlanNode{
		Type: domain.PlanTypeStep, Role: "writer",
		Instruction: "Write about Go.", OutputKey: "doc",
	}
	cfg, err := Convert(&plan, loader)
	if err != nil {
		t.Fatal(err)
	}
	// Template exists: template + instruction appended
	want := "You are a professional writer.\n\nWrite about Go."
	if cfg.Instruction != want {
		t.Errorf("expected %q, got %q", want, cfg.Instruction)
	}
}

func TestConvert_InstructionNoTemplate(t *testing.T) {
	loader := func(baseDir, role string) (string, bool) {
		return "", false
	}
	plan := domain.PlanNode{
		Type: domain.PlanTypeStep, Role: "researcher",
		Instruction: "Search for X.", OutputKey: "result",
	}
	cfg, err := Convert(&plan, loader)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Instruction != "Search for X." {
		t.Errorf("expected raw instruction, got %q", cfg.Instruction)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/application/orchestrator/ -run TestConvert -v`
Expected: compilation error

- [ ] **Step 3: Implement PlanConverter**

```go
// internal/core/application/orchestrator/converter.go
package orchestrator

import (
	"fmt"
	"strconv"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// TemplateLoader checks if a role has a prompt template file.
// Returns (instruction, found). baseDir is unused in this signature
// but kept for compatibility with agentdef.Load patterns.
type TemplateLoader func(baseDir, role string) (instruction string, found bool)

// Convert transforms a PlanNode tree into an AgentNodeConfig tree
// suitable for the existing agenttree.Builder.
// counter tracks depth-first index for unique name generation.
func Convert(node *domain.PlanNode, loader TemplateLoader) (*domain.AgentNodeConfig, error) {
	counter := 0
	return convertNode(node, loader, &counter)
}

func convertNode(node *domain.PlanNode, loader TemplateLoader, counter *int) (*domain.AgentNodeConfig, error) {
	idx := *counter
	*counter++

	switch node.Type {
	case domain.PlanTypeStep:
		return convertStep(node, loader, idx)
	case domain.PlanTypeSequential:
		return convertComposite(node, domain.AgentTypeSequential, "seq_"+strconv.Itoa(idx), loader, counter)
	case domain.PlanTypeLoop:
		return convertLoop(node, loader, idx, counter)
	case domain.PlanTypeParallel:
		return convertComposite(node, domain.AgentTypeParallel, "par_"+strconv.Itoa(idx), loader, counter)
	default:
		return nil, fmt.Errorf("converter: unsupported plan node type %q", node.Type)
	}
}

func convertStep(node *domain.PlanNode, loader TemplateLoader, idx int) (*domain.AgentNodeConfig, error) {
	instruction := node.Instruction

	// Try loading template for this role
	if loader != nil {
		if tmpl, found := loader("", node.Role); found {
			if instruction != "" {
				instruction = tmpl + "\n\n" + instruction
			} else {
				instruction = tmpl
			}
		}
	}

	return &domain.AgentNodeConfig{
		Name:        node.Role + "_" + strconv.Itoa(idx),
		Type:        domain.AgentTypeLLM,
		Instruction: instruction,
		Tools:       node.Tools,
		OutputKey:   node.OutputKey,
	}, nil
}

func convertComposite(node *domain.PlanNode, agentType domain.AgentType, name string, loader TemplateLoader, counter *int) (*domain.AgentNodeConfig, error) {
	subs := make([]domain.AgentNodeConfig, 0, len(node.Steps))
	for i := range node.Steps {
		child, err := convertNode(&node.Steps[i], loader, counter)
		if err != nil {
			return nil, err
		}
		subs = append(subs, *child)
	}
	return &domain.AgentNodeConfig{
		Name:      name,
		Type:      agentType,
		SubAgents: subs,
	}, nil
}

func convertLoop(node *domain.PlanNode, loader TemplateLoader, idx int, counter *int) (*domain.AgentNodeConfig, error) {
	// Build the step children
	subs := make([]domain.AgentNodeConfig, 0, len(node.Steps)+1)
	for i := range node.Steps {
		child, err := convertNode(&node.Steps[i], loader, counter)
		if err != nil {
			return nil, err
		}
		subs = append(subs, *child)
	}

	// Inject exit-checker if exit_condition is set
	if node.ExitCondition != nil {
		checkerIdx := *counter
		*counter++
		subs = append(subs, domain.AgentNodeConfig{
			Name: "exit_checker_" + strconv.Itoa(checkerIdx),
			Type: domain.AgentTypeLLM, // placeholder — Orchestrator replaces with custom agent at build time
			// Special marker fields for the exit-checker
			Instruction: "__EXIT_CHECKER__",
			OutputKey:   node.ExitCondition.OutputKey + "|" + node.ExitCondition.Pattern,
		})
	}

	// Wrap steps in a sequential body (LoopAgent iterates over its sub-agents)
	seqIdx := *counter
	*counter++
	seqBody := domain.AgentNodeConfig{
		Name:      "loop_body_" + strconv.Itoa(seqIdx),
		Type:      domain.AgentTypeSequential,
		SubAgents: subs,
	}

	return &domain.AgentNodeConfig{
		Name:          "loop_" + strconv.Itoa(idx),
		Type:          domain.AgentTypeLoop,
		MaxIterations: node.MaxIterations,
		SubAgents:     []domain.AgentNodeConfig{seqBody},
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/application/orchestrator/ -run TestConvert -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/application/orchestrator/converter.go internal/core/application/orchestrator/converter_test.go
git commit -m "feat(orchestrator): add PlanConverter — PlanNode to AgentNodeConfig"
```

---

## Task 3: Exit-Checker Custom Agent

**Files:**
- Create: `internal/core/application/orchestrator/exitchecker.go`
- Create: `internal/core/application/orchestrator/exitchecker_test.go`

- [ ] **Step 1: Write failing tests for exit-checker**

```go
// internal/core/application/orchestrator/exitchecker_test.go
package orchestrator

import "testing"

func TestNewExitChecker_Escalates(t *testing.T) {
	checker := NewExitChecker("test_checker", ExitCheckConfig{
		OutputKey: "evaluation",
		Pattern:   "APPROVED",
	})
	if checker == nil {
		t.Fatal("expected non-nil agent")
	}
	// Agent name should match
	// Functional test requires ADK session mock — verify construction only here
}

func TestExitCheckShouldEscalate_Match(t *testing.T) {
	ok := exitCheckShouldEscalate("The result is APPROVED and ready.", "APPROVED")
	if !ok {
		t.Error("expected match")
	}
}

func TestExitCheckShouldEscalate_NoMatch(t *testing.T) {
	ok := exitCheckShouldEscalate("Still needs work.", "APPROVED")
	if ok {
		t.Error("expected no match")
	}
}

func TestExitCheckShouldEscalate_EmptyState(t *testing.T) {
	ok := exitCheckShouldEscalate("", "APPROVED")
	if ok {
		t.Error("expected no match on empty state")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/application/orchestrator/ -run TestExit -v`
Expected: compilation error

- [ ] **Step 3: Implement exit-checker**

```go
// internal/core/application/orchestrator/exitchecker.go
package orchestrator

import (
	"iter"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

// ExitCheckConfig defines when a loop should terminate early.
type ExitCheckConfig struct {
	OutputKey string
	Pattern   string
}

// NewExitChecker creates a custom agent that checks session state
// and escalates to terminate the parent LoopAgent when the condition is met.
func NewExitChecker(name string, cfg ExitCheckConfig) agent.Agent {
	a, _ := agent.New(agent.Config{
		Name:        name,
		Description: "Checks loop exit condition and escalates if met.",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				state := ctx.Session().State
				val, _ := state[cfg.OutputKey].(string)
				if exitCheckShouldEscalate(val, cfg.Pattern) {
					yield(&session.Event{
						Actions: session.EventActions{Escalate: true},
					}, nil)
				}
				// No yield = continue loop iteration
			}
		},
	})
	return a
}

// exitCheckShouldEscalate returns true if val contains pattern.
func exitCheckShouldEscalate(val, pattern string) bool {
	if val == "" || pattern == "" {
		return false
	}
	return strings.Contains(val, pattern)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/application/orchestrator/ -run TestExit -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/application/orchestrator/exitchecker.go internal/core/application/orchestrator/exitchecker_test.go
git commit -m "feat(orchestrator): add exit-checker custom agent for loop termination"
```

---

## Task 4: Gemini Response Schemas

**Files:**
- Create: `internal/infra/llm/schema.go`
- Create: `internal/infra/llm/schema_test.go`

- [ ] **Step 1: Write failing test for schema construction**

```go
// internal/infra/llm/schema_test.go
package llm

import "testing"

func TestPlanSchema_NotNil(t *testing.T) {
	s := PlanSchema()
	if s == nil {
		t.Fatal("PlanSchema returned nil")
	}
}

func TestEvalSchema_NotNil(t *testing.T) {
	s := EvalSchema()
	if s == nil {
		t.Fatal("EvalSchema returned nil")
	}
}

func TestEvalSchema_HasSatisfiedField(t *testing.T) {
	s := EvalSchema()
	if s.Properties == nil {
		t.Fatal("expected properties")
	}
	if _, ok := s.Properties["satisfied"]; !ok {
		t.Error("missing 'satisfied' property")
	}
	if _, ok := s.Properties["feedback"]; !ok {
		t.Error("missing 'feedback' property")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/infra/llm/ -v`
Expected: compilation error

- [ ] **Step 3: Implement schemas**

Build the `genai.Schema` objects matching the PlanNode JSON schema. Use Gemini's structured output format. The plan schema is recursive — use `$ref` or inline the structure to the depth Gemini supports (typically 5 levels is sufficient).

Reference: `google.golang.org/genai` package's `Schema` type.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/infra/llm/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/infra/llm/schema.go internal/infra/llm/schema_test.go
git commit -m "feat(infra): add Gemini response schemas for plan and evaluate phases"
```

---

## Task 5: Orchestrator Core

**Files:**
- Create: `internal/core/application/orchestrator/orchestrator.go`
- Create: `internal/core/application/orchestrator/orchestrator_test.go`

- [ ] **Step 1: Write failing tests for Orchestrator**

Test the 4-phase loop with a mock LLM. Key scenarios:
1. Direct plan → immediate return (skip execute/evaluate/respond)
2. Successful plan → execute → evaluate (satisfied) → respond
3. Evaluate not satisfied → retry → second plan → execute → satisfied → respond
4. Max retries exceeded → respond with best-effort
5. System hard limit exceeded → respond with best-effort

Use interface injection for the LLM calls so tests don't need real Gemini.

```go
// internal/core/application/orchestrator/orchestrator_test.go
package orchestrator

import (
	"context"
	"testing"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

func TestOrchestrator_DirectPlan(t *testing.T) {
	o := &Orchestrator{
		Planner: &mockPlanner{output: &domain.PlanOutput{
			Intent: "simple", Plan: domain.PlanNode{Type: domain.PlanTypeDirect, Response: "hello"},
		}},
		SystemMaxRetry: 3,
	}
	result, err := o.Run(context.Background(), "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != "hello" {
		t.Errorf("expected 'hello', got %q", result.Response)
	}
	if result.IsDirect != true {
		t.Error("expected IsDirect=true")
	}
}

// Additional test stubs for satisfied, retry, max_retries scenarios...
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/application/orchestrator/ -run TestOrchestrator -v`
Expected: compilation error

- [ ] **Step 3: Define Orchestrator interfaces**

Define the `Planner`, `Evaluator`, `Responder`, and `Executor` interfaces that the Orchestrator depends on:

```go
// internal/core/application/orchestrator/orchestrator.go
package orchestrator

import (
	"context"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

type Planner interface {
	Plan(ctx context.Context, userPrompt string, feedback string, availableTools []string, availableRoles []string) (*domain.PlanOutput, error)
}

type Evaluator interface {
	Evaluate(ctx context.Context, userPrompt string, results map[string]any) (*domain.EvalOutput, error)
}

type Responder interface {
	Respond(ctx context.Context, userPrompt string, results map[string]any) (string, error)
}

type Executor interface {
	Execute(ctx context.Context, agentTreeCfg *domain.AgentNodeConfig) (map[string]any, error)
}
```

- [ ] **Step 4: Implement Orchestrator.Run()**

The core loop: plan → (direct? return) → execute → evaluate → (satisfied? respond : retry).

```go
type Orchestrator struct {
	Planner        Planner
	Evaluator      Evaluator
	Responder      Responder
	Executor       Executor
	Converter      *ConverterConfig // holds TemplateLoader, available tools/roles
	SystemMaxRetry int
}

type Result struct {
	Response string
	IsDirect bool
	Intent   string
	Retries  int
}

func (o *Orchestrator) Run(ctx context.Context, userPrompt string, sessionOpts any) (*Result, error) {
	var feedback string
	retries := 0

	for {
		// Phase 1: Plan
		plan, err := o.Planner.Plan(ctx, userPrompt, feedback, o.Converter.AvailableTools, o.Converter.AvailableRoles)
		if err != nil {
			return nil, err
		}
		if err := plan.Validate(); err != nil {
			return nil, err
		}

		// Fast path
		if plan.Plan.Type == domain.PlanTypeDirect {
			return &Result{Response: plan.Plan.Response, IsDirect: true, Intent: plan.Intent}, nil
		}

		maxRetry := min(plan.MaxRetries, o.SystemMaxRetry)

		// Phase 2: Execute
		agentCfg, err := Convert(&plan.Plan, o.Converter.TemplateLoader)
		if err != nil {
			return nil, err
		}
		results, err := o.Executor.Execute(ctx, agentCfg)
		if err != nil {
			return nil, err
		}

		// Phase 3: Evaluate
		eval, err := o.Evaluator.Evaluate(ctx, userPrompt, results)
		if err != nil {
			return nil, err
		}

		if eval.Satisfied || retries >= maxRetry {
			// Phase 4: Respond
			response, err := o.Responder.Respond(ctx, userPrompt, results)
			if err != nil {
				return nil, err
			}
			return &Result{Response: response, Intent: plan.Intent, Retries: retries}, nil
		}

		feedback = eval.Feedback
		retries++
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 5: Implement mock types and complete tests**

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/core/application/orchestrator/ -v`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/core/application/orchestrator/orchestrator.go internal/core/application/orchestrator/orchestrator_test.go
git commit -m "feat(orchestrator): implement 4-phase orchestration loop"
```

---

## Task 6: Orchestrator Prompts

**Files:**
- Create: `prompts/plan.prompt`
- Create: `prompts/evaluate.prompt`
- Create: `prompts/respond.prompt`

- [ ] **Step 1: Write plan.prompt**

System prompt for the plan phase. Includes placeholders `{{AVAILABLE_TOOLS}}` and `{{AVAILABLE_ROLES}}` that the Orchestrator injects at runtime.

- [ ] **Step 2: Write evaluate.prompt**

System prompt for the evaluate phase.

- [ ] **Step 3: Write respond.prompt**

System prompt for the respond phase.

- [ ] **Step 4: Commit**

```bash
git add prompts/
git commit -m "feat: add orchestrator phase prompts (plan, evaluate, respond)"
```

---

## Task 7: Gemini LLM Adapter — Planner, Evaluator, Responder

**Files:**
- Create: `internal/infra/llm/planner.go`
- Create: `internal/infra/llm/evaluator.go`
- Create: `internal/infra/llm/responder.go`
- Create: `internal/infra/llm/planner_test.go`

These implement the `Planner`, `Evaluator`, `Responder` interfaces using Gemini API with `response_schema`.

- [ ] **Step 1: Implement GeminiPlanner**

Uses `genai.Client.GenerateContent()` with `PlanSchema()` as `response_schema`. Parses JSON output into `domain.PlanOutput`.

- [ ] **Step 2: Implement GeminiEvaluator**

Uses `genai.Client.GenerateContent()` with `EvalSchema()` as `response_schema`. Parses JSON into `domain.EvalOutput`.

- [ ] **Step 3: Implement GeminiResponder**

Uses `genai.Client.GenerateContent()` without schema. Returns free-form text.

- [ ] **Step 4: Write unit tests (mock genai client or test JSON parsing)**

- [ ] **Step 5: Run tests**

Run: `go test ./internal/infra/llm/ -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/infra/llm/
git commit -m "feat(infra): add Gemini LLM adapters for plan/evaluate/respond"
```

---

## Task 8: ADK Executor Adapter

**Files:**
- Create: `internal/infra/executor/executor.go`
- Create: `internal/infra/executor/executor_test.go`

Implements the `Executor` interface: takes `AgentNodeConfig`, builds ADK agent tree via existing Builder, creates Runner, runs it, collects session state.

- [ ] **Step 1: Implement ADKExecutor**

```go
// internal/infra/executor/executor.go
package executor

// ADKExecutor implements orchestrator.Executor using the existing agenttree.Builder.
type ADKExecutor struct {
	BuilderDeps    agenttree.Deps
	SessionService session.Service
	Plugins        []*plugin.Plugin
	AppName        string
	Defaults       domain.AgentDefaults
}

func (e *ADKExecutor) Execute(ctx context.Context, agentCfg *domain.AgentNodeConfig) (map[string]any, error) {
	// Wrap in AgentTreeConfig for the Builder
	treeCfg := &domain.AgentTreeConfig{
		Version:  "1",
		Defaults: e.Defaults,
		Root:     *agentCfg,
	}

	rootAgent, err := agenttree.Build(treeCfg, e.BuilderDeps)
	if err != nil {
		return nil, err
	}

	// Create per-invocation runner
	r, err := runner.New(runner.Config{
		AppName:        e.AppName,
		Agent:          rootAgent,
		SessionService: e.SessionService,
		PluginConfig:   runner.PluginConfig{Plugins: e.Plugins},
	})
	if err != nil {
		return nil, err
	}

	// Run and collect results from session state
	// ... (iterate events, return session state map)
}
```

- [ ] **Step 2: Write unit tests (mock Builder deps)**

- [ ] **Step 3: Run tests**

Run: `go test ./internal/infra/executor/ -v`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add internal/infra/executor/
git commit -m "feat(infra): add ADK executor adapter for dynamic agent tree execution"
```

---

## Task 9: Rewrite wire.go

**Files:**
- Modify: `internal/core/application/wire.go`
- Modify: `internal/core/application/wire_test.go`

- [ ] **Step 1: Rewrite wire.go — Orchestrator mode only**

Remove `newFromTree()` and `newLegacy()`. Replace `App` struct with `OrchestratorApp` that holds an `*orchestrator.Orchestrator`.

`New()` wires: genai client → model profile → memory plugin → shell tool → MCP toolsets → GeminiPlanner/Evaluator/Responder → ADKExecutor → Orchestrator.

- [ ] **Step 2: Update App struct**

Change entrypoint-facing API from `app.Runner.Run()` to `app.Orchestrator.Run()`.

- [ ] **Step 3: Update wire_test.go**

- [ ] **Step 4: Run tests**

Run: `go test ./internal/core/application/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/application/wire.go internal/core/application/wire_test.go
git commit -m "refactor(wire): replace legacy+YAML modes with Orchestrator-only wiring"
```

---

## Task 10: Update Entrypoints

**Files:**
- Modify: `cmd/agent/main.go`
- Modify: `cmd/telegram/main.go`
- Modify: `cmd/web/main.go`

- [ ] **Step 1: Update cmd/agent/main.go**

Replace `app.Runner.Run()` calls with `app.Orchestrator.Run()`. Adjust event collection to use `orchestrator.Result`.

- [ ] **Step 2: Update cmd/telegram/main.go**

Same pattern — `orchestrator.Run()` returns `Result.Response` which is sent as the Telegram reply.

- [ ] **Step 3: Update cmd/web/main.go**

Same pattern.

- [ ] **Step 4: Run all tests**

Run: `go test ./... -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/
git commit -m "refactor(cmd): update all entrypoints to use Orchestrator"
```

---

## Task 11: Cleanup — Remove Old Files

**Files:**
- Delete: `agenttree.yaml`
- Delete: `internal/infra/config/agenttree/loader.go`
- Delete: `internal/infra/config/agenttree/loader_test.go`
- Delete: `agents/root/agent.prompt`
- Delete: `agents/planner/agent.prompt`
- Delete: `agents/executor/agent.prompt`
- Delete: `agents/reporter/agent.prompt`
- Delete: `agents/worker/agent.prompt`
- Delete: `agents/evaluator/agent.prompt`

- [ ] **Step 1: Delete old files**

```bash
rm agenttree.yaml
rm -r internal/infra/config/agenttree/
rm -r agents/root/ agents/planner/ agents/executor/ agents/reporter/ agents/worker/ agents/evaluator/
```

- [ ] **Step 2: Remove unused imports from wire.go**

Remove `agentreeloader` import and any references to the deleted packages.

- [ ] **Step 3: Run all tests**

Run: `go test ./...`
Expected: all PASS (no references to deleted code)

- [ ] **Step 4: Run go vet**

Run: `go vet ./...`
Expected: clean

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: remove static agent tree config, YAML loader, and static agent prompts"
```

---

## Task 12: End-to-End Smoke Test

**Files:**
- Create: `internal/core/application/orchestrator/integration_test.go`

- [ ] **Step 1: Write integration test**

Uses mock LLM that returns canned plan/eval/respond outputs. Verifies the full Orchestrator flow: plan → convert → build → execute → evaluate → respond.

Tests:
1. Direct path (simple question)
2. Sequential plan with 2 steps
3. Loop plan with exit condition
4. Retry scenario (first eval unsatisfied, second satisfied)

- [ ] **Step 2: Run integration tests**

Run: `go test ./internal/core/application/orchestrator/ -run TestIntegration -v`
Expected: all PASS

- [ ] **Step 3: Run full test suite**

Run: `go test ./... && go vet ./...`
Expected: all clean

- [ ] **Step 4: Commit**

```bash
git add internal/core/application/orchestrator/integration_test.go
git commit -m "test: add end-to-end integration tests for dynamic orchestration"
```

---

## Task 13: Update Documentation

**Files:**
- Modify: `docs/architecture-overview.md`
- Modify: `docs/end-to-end-flow.md`
- Delete: `docs/agent-tree-config-guide.md` (YAML config no longer exists)
- Modify: `docs/workflow-patterns-guide.md`

- [ ] **Step 1: Update architecture-overview.md**

Replace static agent tree description with Orchestrator 4-phase architecture.

- [ ] **Step 2: Update end-to-end-flow.md**

Update Telegram flow examples to show Orchestrator phases.

- [ ] **Step 3: Delete agent-tree-config-guide.md**

The YAML config guide is obsolete.

- [ ] **Step 4: Update workflow-patterns-guide.md**

Reframe workflow patterns as dynamic plan structures (JSON) rather than static YAML.

- [ ] **Step 5: Commit**

```bash
git add docs/
git commit -m "docs: update architecture docs for dynamic orchestration"
```
