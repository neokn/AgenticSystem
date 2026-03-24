package orchestrator_test

import (
	"context"
	"strings"
	"testing"

	"github.com/neokn/agenticsystem/internal/core/application/orchestrator"
	"github.com/neokn/agenticsystem/internal/core/domain"
)

// ---------------------------------------------------------------------------
// verifyingExecutor — records received AgentNodeConfig for structural assertions
// ---------------------------------------------------------------------------

// verifyingExecutor records what it receives for assertion.
type verifyingExecutor struct {
	received []*domain.AgentNodeConfig
	results  map[string]any
}

func (e *verifyingExecutor) Execute(_ context.Context, cfg *domain.AgentNodeConfig) (map[string]any, error) {
	e.received = append(e.received, cfg)
	return e.results, nil
}

// ---------------------------------------------------------------------------
// Plan builder helpers for integration scenarios
// ---------------------------------------------------------------------------

// sequentialPlanWith2Steps returns a PlanOutput whose root plan is a
// sequential node containing two step sub-nodes.
func sequentialPlanWith2Steps() *domain.PlanOutput {
	return &domain.PlanOutput{
		Intent:     "run two steps in order",
		MaxRetries: 1,
		Plan: domain.PlanNode{
			Type: domain.PlanTypeSequential,
			Steps: []domain.PlanNode{
				{
					Type:        domain.PlanTypeStep,
					Role:        "researcher",
					Instruction: "gather information",
					OutputKey:   "research_output",
				},
				{
					Type:        domain.PlanTypeStep,
					Role:        "writer",
					Instruction: "write the report",
					OutputKey:   "report_output",
				},
			},
		},
	}
}

// loopPlanWithExitCondition returns a PlanOutput whose root plan is a loop
// node with one step and an exit condition.
func loopPlanWithExitCondition() *domain.PlanOutput {
	return &domain.PlanOutput{
		Intent:     "iterate until done",
		MaxRetries: 1,
		Plan: domain.PlanNode{
			Type:          domain.PlanTypeLoop,
			MaxIterations: 5,
			Steps: []domain.PlanNode{
				{
					Type:        domain.PlanTypeStep,
					Role:        "worker",
					Instruction: "do iterative work",
					OutputKey:   "work_output",
				},
			},
			ExitCondition: &domain.ExitCondition{
				OutputKey: "work_output",
				Pattern:   "DONE",
			},
		},
	}
}

// parallelPlanWith2Steps returns a PlanOutput whose root plan is a parallel
// node containing two independent step sub-nodes.
func parallelPlanWith2Steps() *domain.PlanOutput {
	return &domain.PlanOutput{
		Intent:     "run two steps concurrently",
		MaxRetries: 1,
		Plan: domain.PlanNode{
			Type: domain.PlanTypeParallel,
			Steps: []domain.PlanNode{
				{
					Type:        domain.PlanTypeStep,
					Role:        "fetcher",
					Instruction: "fetch data from source A",
					OutputKey:   "data_a",
				},
				{
					Type:        domain.PlanTypeStep,
					Role:        "analyzer",
					Instruction: "analyze data from source B",
					OutputKey:   "data_b",
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// TestIntegration_DirectPath
// ---------------------------------------------------------------------------

// TestIntegration_DirectPath verifies that when the Planner returns a direct
// plan the Orchestrator returns Result.IsDirect=true with the correct Response,
// and does not call Convert, Execute, Evaluate, or Respond.
func TestIntegration_DirectPath(t *testing.T) {
	// Arrange
	const expectedResponse = "The answer is 42."
	planner := &mockPlanner{plans: []*domain.PlanOutput{directPlan(expectedResponse)}}
	executor := &verifyingExecutor{results: map[string]any{}}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{{Satisfied: true}}}
	responder := &mockResponder{response: "should not be called"}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: noopLoader,
		SystemMaxRetry: 3,
	})

	// Act
	result, err := orch.Run(context.Background(), "what is the answer?")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsDirect {
		t.Error("expected Result.IsDirect=true for direct plan")
	}
	if result.Response != expectedResponse {
		t.Errorf("Result.Response = %q, want %q", result.Response, expectedResponse)
	}
	if len(executor.received) != 0 {
		t.Errorf("Executor should NOT have been called for a direct plan; got %d calls", len(executor.received))
	}
	if evaluator.calls != 0 {
		t.Errorf("Evaluator should NOT have been called for a direct plan; got %d calls", evaluator.calls)
	}
	if responder.called {
		t.Error("Responder should NOT have been called for a direct plan")
	}
}

// ---------------------------------------------------------------------------
// TestIntegration_SequentialPlan
// ---------------------------------------------------------------------------

// TestIntegration_SequentialPlan verifies that a sequential plan with 2 steps
// is fully converted before being passed to the Executor, resulting in an
// AgentTypeSequential node that wraps two AgentTypeLLM sub-agents.
// After the Executor returns, Evaluator is called (satisfied) and Responder
// formats the final response.
func TestIntegration_SequentialPlan(t *testing.T) {
	// Arrange
	const expectedResponse = "Report generated successfully."
	planner := &mockPlanner{plans: []*domain.PlanOutput{sequentialPlanWith2Steps()}}
	executor := &verifyingExecutor{results: map[string]any{"report_output": "final report"}}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{{Satisfied: true}}}
	responder := &mockResponder{response: expectedResponse}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: noopLoader,
		SystemMaxRetry: 3,
	})

	// Act
	result, err := orch.Run(context.Background(), "research and write a report")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsDirect {
		t.Error("expected IsDirect=false for sequential plan")
	}
	if result.Response != expectedResponse {
		t.Errorf("Result.Response = %q, want %q", result.Response, expectedResponse)
	}

	// Verify the Executor received exactly one correctly structured AgentNodeConfig
	if len(executor.received) != 1 {
		t.Fatalf("expected 1 Execute call, got %d", len(executor.received))
	}
	cfg := executor.received[0]

	// Root node must be sequential
	if cfg.Type != domain.AgentTypeSequential {
		t.Errorf("root AgentNodeConfig.Type = %q, want %q", cfg.Type, domain.AgentTypeSequential)
	}
	if !strings.HasPrefix(cfg.Name, "seq_") {
		t.Errorf("root AgentNodeConfig.Name = %q, want prefix %q", cfg.Name, "seq_")
	}

	// Must have exactly 2 LLM sub-agents
	if len(cfg.SubAgents) != 2 {
		t.Fatalf("SubAgents count = %d, want 2", len(cfg.SubAgents))
	}
	for i, sub := range cfg.SubAgents {
		if sub.Type != domain.AgentTypeLLM {
			t.Errorf("SubAgents[%d].Type = %q, want %q", i, sub.Type, domain.AgentTypeLLM)
		}
	}
	if !strings.HasPrefix(cfg.SubAgents[0].Name, "researcher_") {
		t.Errorf("SubAgents[0].Name = %q, want prefix %q", cfg.SubAgents[0].Name, "researcher_")
	}
	if !strings.HasPrefix(cfg.SubAgents[1].Name, "writer_") {
		t.Errorf("SubAgents[1].Name = %q, want prefix %q", cfg.SubAgents[1].Name, "writer_")
	}
	if cfg.SubAgents[0].OutputKey != "research_output" {
		t.Errorf("SubAgents[0].OutputKey = %q, want %q", cfg.SubAgents[0].OutputKey, "research_output")
	}
	if cfg.SubAgents[1].OutputKey != "report_output" {
		t.Errorf("SubAgents[1].OutputKey = %q, want %q", cfg.SubAgents[1].OutputKey, "report_output")
	}

	// Evaluator and Responder must each be called once
	if evaluator.calls != 1 {
		t.Errorf("Evaluator calls = %d, want 1", evaluator.calls)
	}
	if !responder.called {
		t.Error("Responder should have been called")
	}
}

// ---------------------------------------------------------------------------
// TestIntegration_LoopWithExitCondition
// ---------------------------------------------------------------------------

// TestIntegration_LoopWithExitCondition verifies that a loop plan with an
// exit_condition is converted to the correct structure:
//
//	loop_N
//	  └─ seq_M (body wrapper)
//	       ├─ worker_K
//	       └─ exit_checker_P
//
// The exit checker must have Instruction == "__EXIT_CHECKER__" and
// OutputKey == "<output_key>|<pattern>".
func TestIntegration_LoopWithExitCondition(t *testing.T) {
	// Arrange
	planner := &mockPlanner{plans: []*domain.PlanOutput{loopPlanWithExitCondition()}}
	executor := &verifyingExecutor{results: map[string]any{"work_output": "DONE"}}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{{Satisfied: true}}}
	responder := &mockResponder{response: "Loop completed."}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: noopLoader,
		SystemMaxRetry: 3,
	})

	// Act
	_, err := orch.Run(context.Background(), "iterate until done")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(executor.received) != 1 {
		t.Fatalf("expected 1 Execute call, got %d", len(executor.received))
	}
	cfg := executor.received[0]

	// Root must be a loop
	if cfg.Type != domain.AgentTypeLoop {
		t.Errorf("root.Type = %q, want %q", cfg.Type, domain.AgentTypeLoop)
	}
	if !strings.HasPrefix(cfg.Name, "loop_") {
		t.Errorf("root.Name = %q, want prefix %q", cfg.Name, "loop_")
	}
	if cfg.MaxIterations != 5 {
		t.Errorf("root.MaxIterations = %d, want 5", cfg.MaxIterations)
	}

	// Loop must have exactly one sub-agent (the sequential body wrapper)
	if len(cfg.SubAgents) != 1 {
		t.Fatalf("loop SubAgents count = %d, want 1 (body wrapper)", len(cfg.SubAgents))
	}
	body := cfg.SubAgents[0]
	if body.Type != domain.AgentTypeSequential {
		t.Errorf("body.Type = %q, want %q", body.Type, domain.AgentTypeSequential)
	}
	if !strings.HasPrefix(body.Name, "seq_") {
		t.Errorf("body.Name = %q, want prefix %q", body.Name, "seq_")
	}

	// Body must contain: [worker step, exit_checker]
	if len(body.SubAgents) != 2 {
		t.Fatalf("body.SubAgents count = %d, want 2 (step + exit_checker)", len(body.SubAgents))
	}
	workerNode := body.SubAgents[0]
	exitChecker := body.SubAgents[1]

	if workerNode.Type != domain.AgentTypeLLM {
		t.Errorf("workerNode.Type = %q, want %q", workerNode.Type, domain.AgentTypeLLM)
	}
	if !strings.HasPrefix(workerNode.Name, "worker_") {
		t.Errorf("workerNode.Name = %q, want prefix %q", workerNode.Name, "worker_")
	}

	if !strings.HasPrefix(exitChecker.Name, "exit_checker_") {
		t.Errorf("exitChecker.Name = %q, want prefix %q", exitChecker.Name, "exit_checker_")
	}
	if exitChecker.Type != domain.AgentTypeLLM {
		t.Errorf("exitChecker.Type = %q, want %q", exitChecker.Type, domain.AgentTypeLLM)
	}
	if exitChecker.Instruction != "__EXIT_CHECKER__" {
		t.Errorf("exitChecker.Instruction = %q, want %q", exitChecker.Instruction, "__EXIT_CHECKER__")
	}
	if exitChecker.OutputKey != "work_output|DONE" {
		t.Errorf("exitChecker.OutputKey = %q, want %q", exitChecker.OutputKey, "work_output|DONE")
	}
}

// ---------------------------------------------------------------------------
// TestIntegration_RetryFlow
// ---------------------------------------------------------------------------

// TestIntegration_RetryFlow verifies that when the first Evaluator call
// returns satisfied=false with feedback, the Orchestrator loops back to the
// Planner (passing the feedback), then a second evaluation returns satisfied,
// and Respond is called exactly once at the end.
func TestIntegration_RetryFlow(t *testing.T) {
	// Arrange: planner returns same sequential plan on both calls
	planner := &mockPlanner{
		plans: []*domain.PlanOutput{
			sequentialPlanWith2Steps(),
			sequentialPlanWith2Steps(),
		},
	}
	executor := &verifyingExecutor{results: map[string]any{"report_output": "improved report"}}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{
		{Satisfied: false, Feedback: "the report needs more detail"},
		{Satisfied: true},
	}}
	responder := &mockResponder{response: "Here is the improved report."}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: noopLoader,
		SystemMaxRetry: 5,
	})

	// Act
	result, err := orch.Run(context.Background(), "write a detailed report")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Retries != 1 {
		t.Errorf("Retries = %d, want 1", result.Retries)
	}
	// Planner must have been called twice (initial + 1 retry)
	if planner.calls != 2 {
		t.Errorf("Planner calls = %d, want 2", planner.calls)
	}
	// Evaluator must have been called twice
	if evaluator.calls != 2 {
		t.Errorf("Evaluator calls = %d, want 2", evaluator.calls)
	}
	// Executor must have been called twice (once per plan iteration)
	if len(executor.received) != 2 {
		t.Errorf("Executor calls = %d, want 2", len(executor.received))
	}
	// Both Execute calls should have received a sequential AgentNodeConfig
	for i, cfg := range executor.received {
		if cfg.Type != domain.AgentTypeSequential {
			t.Errorf("executor.received[%d].Type = %q, want %q", i, cfg.Type, domain.AgentTypeSequential)
		}
		if len(cfg.SubAgents) != 2 {
			t.Errorf("executor.received[%d].SubAgents count = %d, want 2", i, len(cfg.SubAgents))
		}
	}
	// Responder must be called exactly once (after second satisfied eval)
	if !responder.called {
		t.Error("Responder should have been called once after second evaluation")
	}
	if result.Response != "Here is the improved report." {
		t.Errorf("Result.Response = %q, want %q", result.Response, "Here is the improved report.")
	}
}

// ---------------------------------------------------------------------------
// TestIntegration_ParallelPlan
// ---------------------------------------------------------------------------

// TestIntegration_ParallelPlan verifies that a parallel plan is correctly
// converted and the Executor receives an AgentTypeParallel AgentNodeConfig
// with 2 LLM sub-agents.
func TestIntegration_ParallelPlan(t *testing.T) {
	// Arrange
	planner := &mockPlanner{plans: []*domain.PlanOutput{parallelPlanWith2Steps()}}
	executor := &verifyingExecutor{results: map[string]any{"data_a": "result A", "data_b": "result B"}}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{{Satisfied: true}}}
	responder := &mockResponder{response: "Parallel tasks completed."}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: noopLoader,
		SystemMaxRetry: 3,
	})

	// Act
	result, err := orch.Run(context.Background(), "run two tasks concurrently")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsDirect {
		t.Error("expected IsDirect=false for parallel plan")
	}
	if result.Response != "Parallel tasks completed." {
		t.Errorf("Result.Response = %q, want %q", result.Response, "Parallel tasks completed.")
	}

	if len(executor.received) != 1 {
		t.Fatalf("expected 1 Execute call, got %d", len(executor.received))
	}
	cfg := executor.received[0]

	// Root must be a parallel agent
	if cfg.Type != domain.AgentTypeParallel {
		t.Errorf("root.Type = %q, want %q", cfg.Type, domain.AgentTypeParallel)
	}
	if !strings.HasPrefix(cfg.Name, "par_") {
		t.Errorf("root.Name = %q, want prefix %q", cfg.Name, "par_")
	}

	// Must have exactly 2 LLM sub-agents
	if len(cfg.SubAgents) != 2 {
		t.Fatalf("SubAgents count = %d, want 2", len(cfg.SubAgents))
	}
	for i, sub := range cfg.SubAgents {
		if sub.Type != domain.AgentTypeLLM {
			t.Errorf("SubAgents[%d].Type = %q, want %q", i, sub.Type, domain.AgentTypeLLM)
		}
	}
	if !strings.HasPrefix(cfg.SubAgents[0].Name, "fetcher_") {
		t.Errorf("SubAgents[0].Name = %q, want prefix %q", cfg.SubAgents[0].Name, "fetcher_")
	}
	if !strings.HasPrefix(cfg.SubAgents[1].Name, "analyzer_") {
		t.Errorf("SubAgents[1].Name = %q, want prefix %q", cfg.SubAgents[1].Name, "analyzer_")
	}
	if cfg.SubAgents[0].OutputKey != "data_a" {
		t.Errorf("SubAgents[0].OutputKey = %q, want %q", cfg.SubAgents[0].OutputKey, "data_a")
	}
	if cfg.SubAgents[1].OutputKey != "data_b" {
		t.Errorf("SubAgents[1].OutputKey = %q, want %q", cfg.SubAgents[1].OutputKey, "data_b")
	}

	if evaluator.calls != 1 {
		t.Errorf("Evaluator calls = %d, want 1", evaluator.calls)
	}
	if !responder.called {
		t.Error("Responder should have been called")
	}
}
