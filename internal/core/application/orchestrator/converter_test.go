package orchestrator_test

import (
	"strings"
	"testing"

	"github.com/neokn/agenticsystem/internal/core/application/orchestrator"
	"github.com/neokn/agenticsystem/internal/core/domain"
)

// noopLoader is a TemplateLoader that never finds a template.
func noopLoader(_, _ string) (string, bool) { return "", false }

// fixedLoader always returns a fixed template for any role.
func fixedLoader(tmpl string) orchestrator.TemplateLoader {
	return func(_, _ string) (string, bool) { return tmpl, true }
}

// roleLoader returns a template only for the specified role.
func roleLoader(role, tmpl string) orchestrator.TemplateLoader {
	return func(_, r string) (string, bool) {
		if r == role {
			return tmpl, true
		}
		return "", false
	}
}

// --- TestConvert_Step ---

func TestConvert_Step_should_produce_llm_agent_with_correct_fields(t *testing.T) {
	node := &domain.PlanNode{
		Type:        domain.PlanTypeStep,
		Role:        "coder",
		Instruction: "write the code",
		Tools:       []string{"bash", "read_file"},
		OutputKey:   "code_output",
	}

	got, err := orchestrator.Convert(node, noopLoader)

	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got.Type != domain.AgentTypeLLM {
		t.Errorf("Type = %q, want %q", got.Type, domain.AgentTypeLLM)
	}
	if got.Name == "" {
		t.Error("Name must not be empty")
	}
	if !strings.HasPrefix(got.Name, "coder_") {
		t.Errorf("Name = %q, want prefix %q", got.Name, "coder_")
	}
	if got.Instruction != "write the code" {
		t.Errorf("Instruction = %q, want %q", got.Instruction, "write the code")
	}
	if got.OutputKey != "code_output" {
		t.Errorf("OutputKey = %q, want %q", got.OutputKey, "code_output")
	}
	if len(got.Tools) != 2 || got.Tools[0] != "bash" || got.Tools[1] != "read_file" {
		t.Errorf("Tools = %v, want [bash read_file]", got.Tools)
	}
}

// --- TestConvert_Sequential ---

func TestConvert_Sequential_should_produce_sequential_agent_with_sub_agents(t *testing.T) {
	node := &domain.PlanNode{
		Type: domain.PlanTypeSequential,
		Steps: []domain.PlanNode{
			{Type: domain.PlanTypeStep, Role: "coder", Instruction: "write", OutputKey: "code"},
			{Type: domain.PlanTypeStep, Role: "reviewer", Instruction: "review", OutputKey: "review"},
		},
	}

	got, err := orchestrator.Convert(node, noopLoader)

	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got.Type != domain.AgentTypeSequential {
		t.Errorf("Type = %q, want %q", got.Type, domain.AgentTypeSequential)
	}
	if !strings.HasPrefix(got.Name, "seq_") {
		t.Errorf("Name = %q, want prefix %q", got.Name, "seq_")
	}
	if len(got.SubAgents) != 2 {
		t.Fatalf("SubAgents len = %d, want 2", len(got.SubAgents))
	}
	if got.SubAgents[0].Type != domain.AgentTypeLLM {
		t.Errorf("SubAgents[0].Type = %q, want %q", got.SubAgents[0].Type, domain.AgentTypeLLM)
	}
	if got.SubAgents[1].Type != domain.AgentTypeLLM {
		t.Errorf("SubAgents[1].Type = %q, want %q", got.SubAgents[1].Type, domain.AgentTypeLLM)
	}
}

// --- TestConvert_LoopWithExitCondition ---

func TestConvert_LoopWithExitCondition_should_inject_exit_checker_at_end_of_body(t *testing.T) {
	node := &domain.PlanNode{
		Type:          domain.PlanTypeLoop,
		MaxIterations: 5,
		Steps: []domain.PlanNode{
			{Type: domain.PlanTypeStep, Role: "worker", Instruction: "do work", OutputKey: "work_output"},
		},
		ExitCondition: &domain.ExitCondition{
			OutputKey: "work_output",
			Pattern:   "DONE",
		},
	}

	got, err := orchestrator.Convert(node, noopLoader)

	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got.Type != domain.AgentTypeLoop {
		t.Errorf("Type = %q, want %q", got.Type, domain.AgentTypeLoop)
	}
	if !strings.HasPrefix(got.Name, "loop_") {
		t.Errorf("Name = %q, want prefix %q", got.Name, "loop_")
	}
	if got.MaxIterations != 5 {
		t.Errorf("MaxIterations = %d, want 5", got.MaxIterations)
	}

	// Loop body should be wrapped in a sequential agent
	if len(got.SubAgents) != 1 {
		t.Fatalf("SubAgents len = %d, want 1 (body sequential wrapper)", len(got.SubAgents))
	}
	body := got.SubAgents[0]
	if body.Type != domain.AgentTypeSequential {
		t.Errorf("body.Type = %q, want %q", body.Type, domain.AgentTypeSequential)
	}

	// Body should contain original steps + exit checker
	if len(body.SubAgents) != 2 {
		t.Fatalf("body.SubAgents len = %d, want 2 (1 step + exit_checker)", len(body.SubAgents))
	}

	exitChecker := body.SubAgents[len(body.SubAgents)-1]
	if !strings.HasPrefix(exitChecker.Name, "exit_checker_") {
		t.Errorf("exit_checker name = %q, want prefix %q", exitChecker.Name, "exit_checker_")
	}
	if exitChecker.Type != domain.AgentTypeLLM {
		t.Errorf("exit_checker.Type = %q, want %q", exitChecker.Type, domain.AgentTypeLLM)
	}
	if exitChecker.Instruction != "__EXIT_CHECKER__" {
		t.Errorf("exit_checker.Instruction = %q, want %q", exitChecker.Instruction, "__EXIT_CHECKER__")
	}
	if exitChecker.OutputKey != "work_output|DONE" {
		t.Errorf("exit_checker.OutputKey = %q, want %q", exitChecker.OutputKey, "work_output|DONE")
	}
}

// --- TestConvert_LoopWithoutExitCondition ---

func TestConvert_LoopWithoutExitCondition_should_not_inject_exit_checker(t *testing.T) {
	node := &domain.PlanNode{
		Type:          domain.PlanTypeLoop,
		MaxIterations: 3,
		Steps: []domain.PlanNode{
			{Type: domain.PlanTypeStep, Role: "worker", Instruction: "do work", OutputKey: "work_output"},
		},
	}

	got, err := orchestrator.Convert(node, noopLoader)

	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got.Type != domain.AgentTypeLoop {
		t.Errorf("Type = %q, want %q", got.Type, domain.AgentTypeLoop)
	}

	// Loop body should be wrapped in a sequential agent
	if len(got.SubAgents) != 1 {
		t.Fatalf("SubAgents len = %d, want 1 (body sequential wrapper)", len(got.SubAgents))
	}
	body := got.SubAgents[0]
	if body.Type != domain.AgentTypeSequential {
		t.Errorf("body.Type = %q, want %q", body.Type, domain.AgentTypeSequential)
	}

	// Body contains only the original step — no exit checker
	if len(body.SubAgents) != 1 {
		t.Fatalf("body.SubAgents len = %d, want 1 (no exit checker)", len(body.SubAgents))
	}
	for _, sub := range body.SubAgents {
		if strings.HasPrefix(sub.Name, "exit_checker_") {
			t.Errorf("unexpected exit_checker sub-agent: %q", sub.Name)
		}
	}
}

// --- TestConvert_Parallel ---

func TestConvert_Parallel_should_produce_parallel_agent_with_sub_agents(t *testing.T) {
	node := &domain.PlanNode{
		Type: domain.PlanTypeParallel,
		Steps: []domain.PlanNode{
			{Type: domain.PlanTypeStep, Role: "fetcher", Instruction: "fetch data", OutputKey: "data_a"},
			{Type: domain.PlanTypeStep, Role: "parser", Instruction: "parse data", OutputKey: "data_b"},
		},
	}

	got, err := orchestrator.Convert(node, noopLoader)

	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got.Type != domain.AgentTypeParallel {
		t.Errorf("Type = %q, want %q", got.Type, domain.AgentTypeParallel)
	}
	if !strings.HasPrefix(got.Name, "par_") {
		t.Errorf("Name = %q, want prefix %q", got.Name, "par_")
	}
	if len(got.SubAgents) != 2 {
		t.Fatalf("SubAgents len = %d, want 2", len(got.SubAgents))
	}
}

// --- TestConvert_NameUniqueness ---

func TestConvert_NameUniqueness_should_generate_different_names_for_same_role(t *testing.T) {
	node := &domain.PlanNode{
		Type: domain.PlanTypeSequential,
		Steps: []domain.PlanNode{
			{Type: domain.PlanTypeStep, Role: "coder", Instruction: "first pass", OutputKey: "out1"},
			{Type: domain.PlanTypeStep, Role: "coder", Instruction: "second pass", OutputKey: "out2"},
		},
	}

	got, err := orchestrator.Convert(node, noopLoader)

	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if len(got.SubAgents) != 2 {
		t.Fatalf("SubAgents len = %d, want 2", len(got.SubAgents))
	}
	name0 := got.SubAgents[0].Name
	name1 := got.SubAgents[1].Name
	if name0 == name1 {
		t.Errorf("both sub-agents have the same name %q — names must be unique", name0)
	}
	if !strings.HasPrefix(name0, "coder_") {
		t.Errorf("SubAgents[0].Name = %q, want prefix %q", name0, "coder_")
	}
	if !strings.HasPrefix(name1, "coder_") {
		t.Errorf("SubAgents[1].Name = %q, want prefix %q", name1, "coder_")
	}
}

// --- TestConvert_InstructionFromTemplate ---

func TestConvert_InstructionFromTemplate_should_use_template_and_append_instruction(t *testing.T) {
	const tmpl = "You are an expert coder."
	const instr = "Focus on performance."
	node := &domain.PlanNode{
		Type:        domain.PlanTypeStep,
		Role:        "coder",
		Instruction: instr,
		OutputKey:   "code_output",
	}

	got, err := orchestrator.Convert(node, roleLoader("coder", tmpl))

	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	want := tmpl + "\n\n" + instr
	if got.Instruction != want {
		t.Errorf("Instruction = %q, want %q", got.Instruction, want)
	}
}

// --- TestConvert_InstructionNoTemplate ---

func TestConvert_InstructionNoTemplate_should_use_instruction_directly(t *testing.T) {
	const instr = "Do exactly what the user says."
	node := &domain.PlanNode{
		Type:        domain.PlanTypeStep,
		Role:        "assistant",
		Instruction: instr,
		OutputKey:   "result",
	}

	got, err := orchestrator.Convert(node, noopLoader)

	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got.Instruction != instr {
		t.Errorf("Instruction = %q, want %q", got.Instruction, instr)
	}
}

// --- TestConvert_UnknownType ---

func TestConvert_UnknownType_should_return_error(t *testing.T) {
	node := &domain.PlanNode{
		Type: domain.PlanNodeType("unknown"),
	}

	_, err := orchestrator.Convert(node, noopLoader)

	if err == nil {
		t.Error("Convert() expected error for unknown node type, got nil")
	}
}

// --- TestConvert_NilNode ---

func TestConvert_NilNode_should_return_error(t *testing.T) {
	_, err := orchestrator.Convert(nil, noopLoader)
	if err == nil {
		t.Error("Convert() expected error for nil node, got nil")
	}
}

// --- fixedLoader helper used in template test ---
var _ = fixedLoader // suppress unused warning if only roleLoader is used
