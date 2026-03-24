package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/neokn/agenticsystem/internal/core/application/orchestrator"
	"github.com/neokn/agenticsystem/internal/core/domain"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

// mockPlanner returns plans in order; when exhausted, repeats the last plan.
type mockPlanner struct {
	plans []*domain.PlanOutput
	idx   int
	calls int
}

func (m *mockPlanner) Plan(
	_ context.Context,
	_, _ string,
	_, _ []string,
) (*domain.PlanOutput, error) {
	m.calls++
	if m.idx >= len(m.plans) {
		return m.plans[len(m.plans)-1], nil
	}
	p := m.plans[m.idx]
	m.idx++
	return p, nil
}

// errPlanner always returns an error.
type errPlanner struct{ err error }

func (e *errPlanner) Plan(_ context.Context, _, _ string, _, _ []string) (*domain.PlanOutput, error) {
	return nil, e.err
}

// invalidPlanner returns a PlanOutput that fails Validate().
type invalidPlanner struct{}

func (p *invalidPlanner) Plan(_ context.Context, _, _ string, _, _ []string) (*domain.PlanOutput, error) {
	// intent is empty — Validate() will reject this.
	return &domain.PlanOutput{
		Intent:     "",
		MaxRetries: 1,
		Plan:       domain.PlanNode{Type: domain.PlanTypeStep, Role: "worker", OutputKey: "out"},
	}, nil
}

// mockEvaluator returns EvalOutputs in order; repeats the last when exhausted.
type mockEvaluator struct {
	evals []*domain.EvalOutput
	idx   int
	calls int
}

func (m *mockEvaluator) Evaluate(
	_ context.Context,
	_ string,
	_ map[string]any,
) (*domain.EvalOutput, error) {
	m.calls++
	if m.idx >= len(m.evals) {
		return m.evals[len(m.evals)-1], nil
	}
	e := m.evals[m.idx]
	m.idx++
	return e, nil
}

// mockResponder captures the call and returns a fixed response string.
type mockResponder struct {
	called   bool
	response string
}

func (m *mockResponder) Respond(
	_ context.Context,
	_ string,
	_ map[string]any,
) (string, error) {
	m.called = true
	return m.response, nil
}

// mockExecutor captures the call and returns a fixed results map.
type mockExecutor struct {
	called  bool
	results map[string]any
	err     error
}

func (m *mockExecutor) Execute(
	_ context.Context,
	_ *domain.AgentNodeConfig,
) (map[string]any, error) {
	m.called = true
	return m.results, m.err
}

// ---------------------------------------------------------------------------
// Helper factories
// ---------------------------------------------------------------------------

// directPlan builds a valid direct PlanOutput.
func directPlan(response string) *domain.PlanOutput {
	return &domain.PlanOutput{
		Intent:     "answer directly",
		MaxRetries: 0,
		Plan: domain.PlanNode{
			Type:     domain.PlanTypeDirect,
			Response: response,
		},
	}
}

// stepPlan builds a valid step-based PlanOutput.
func stepPlan(maxRetries int) *domain.PlanOutput {
	return &domain.PlanOutput{
		Intent:     "do something",
		MaxRetries: maxRetries,
		Plan: domain.PlanNode{
			Type:      domain.PlanTypeStep,
			Role:      "worker",
			OutputKey: "result",
		},
	}
}

// nilLoader is a TemplateLoader that never finds templates.
func nilLoader(_ string, _ string) (string, bool) { return "", false }

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestOrchestrator_DirectPlan verifies that when the Planner returns a direct
// plan the Orchestrator returns immediately with IsDirect=true and does NOT
// call Execute, Evaluate, or Respond.
func TestOrchestrator_DirectPlan(t *testing.T) {
	// Arrange
	planner := &mockPlanner{plans: []*domain.PlanOutput{directPlan("Hello, world!")}}
	executor := &mockExecutor{}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{{Satisfied: true}}}
	responder := &mockResponder{response: "should not be called"}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: nilLoader,
		SystemMaxRetry: 5,
	})

	// Act
	result, err := orch.Run(context.Background(), "hi")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsDirect {
		t.Error("expected IsDirect=true")
	}
	if result.Response != "Hello, world!" {
		t.Errorf("expected response %q, got %q", "Hello, world!", result.Response)
	}
	if executor.called {
		t.Error("Executor should NOT have been called for a direct plan")
	}
	if evaluator.calls > 0 {
		t.Error("Evaluator should NOT have been called for a direct plan")
	}
	if responder.called {
		t.Error("Responder should NOT have been called for a direct plan")
	}
}

// TestOrchestrator_SuccessfulExecution verifies the happy path:
// Plan → Execute → Evaluate(satisfied=true) → Respond → result.
func TestOrchestrator_SuccessfulExecution(t *testing.T) {
	// Arrange
	planner := &mockPlanner{plans: []*domain.PlanOutput{stepPlan(3)}}
	executor := &mockExecutor{results: map[string]any{"result": "done"}}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{{Satisfied: true}}}
	responder := &mockResponder{response: "Task completed successfully."}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: nilLoader,
		SystemMaxRetry: 5,
	})

	// Act
	result, err := orch.Run(context.Background(), "do something")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsDirect {
		t.Error("expected IsDirect=false")
	}
	if result.Response != "Task completed successfully." {
		t.Errorf("unexpected response: %q", result.Response)
	}
	if !executor.called {
		t.Error("Executor should have been called")
	}
	if evaluator.calls != 1 {
		t.Errorf("expected 1 Evaluate call, got %d", evaluator.calls)
	}
	if result.Retries != 0 {
		t.Errorf("expected 0 retries, got %d", result.Retries)
	}
	if result.Intent != "do something" {
		t.Errorf("expected intent %q, got %q", "do something", result.Intent)
	}
}

// TestOrchestrator_RetryOnce verifies that when the first Evaluate returns
// unsatisfied, the loop retries and then succeeds on the second Evaluate.
func TestOrchestrator_RetryOnce(t *testing.T) {
	// Arrange
	planner := &mockPlanner{plans: []*domain.PlanOutput{stepPlan(3)}}
	executor := &mockExecutor{results: map[string]any{"result": "partial"}}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{
		{Satisfied: false, Feedback: "needs more detail"},
		{Satisfied: true},
	}}
	responder := &mockResponder{response: "Final answer."}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: nilLoader,
		SystemMaxRetry: 5,
	})

	// Act
	result, err := orch.Run(context.Background(), "do something")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Retries != 1 {
		t.Errorf("expected 1 retry, got %d", result.Retries)
	}
	if evaluator.calls != 2 {
		t.Errorf("expected 2 Evaluate calls, got %d", evaluator.calls)
	}
	if planner.calls != 2 {
		t.Errorf("expected 2 Plan calls (initial + 1 retry), got %d", planner.calls)
	}
	if !responder.called {
		t.Error("Responder should have been called after successful retry")
	}
}

// TestOrchestrator_MaxRetriesExceeded verifies that when plan.MaxRetries=1 and
// both evals are unsatisfied, the Orchestrator still calls Respond (forced by limit).
func TestOrchestrator_MaxRetriesExceeded(t *testing.T) {
	// Arrange
	planner := &mockPlanner{plans: []*domain.PlanOutput{stepPlan(1)}}
	executor := &mockExecutor{results: map[string]any{"result": "incomplete"}}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{
		{Satisfied: false, Feedback: "not done"},
		{Satisfied: false, Feedback: "still not done"},
	}}
	responder := &mockResponder{response: "Best effort result."}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: nilLoader,
		SystemMaxRetry: 5,
	})

	// Act
	result, err := orch.Run(context.Background(), "do something hard")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !responder.called {
		t.Error("Responder should have been called when max retries exceeded")
	}
	// plan.MaxRetries=1 means 1 retry allowed → 2 total evaluations
	if evaluator.calls != 2 {
		t.Errorf("expected 2 Evaluate calls, got %d", evaluator.calls)
	}
	if result.Retries != 1 {
		t.Errorf("expected 1 retry recorded, got %d", result.Retries)
	}
}

// TestOrchestrator_SystemHardLimit verifies that when plan says max_retries=10
// but system limit is 2, only 2 retries are allowed.
func TestOrchestrator_SystemHardLimit(t *testing.T) {
	// Arrange — all evals unsatisfied so retries continue until capped
	planner := &mockPlanner{plans: []*domain.PlanOutput{stepPlan(10)}}
	executor := &mockExecutor{results: map[string]any{"result": "nope"}}
	// Provide 5 unsatisfied evals — only 3 should be consumed (initial + 2 retries)
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{
		{Satisfied: false, Feedback: "f1"},
		{Satisfied: false, Feedback: "f2"},
		{Satisfied: false, Feedback: "f3"},
		{Satisfied: false, Feedback: "f4"},
		{Satisfied: false, Feedback: "f5"},
	}}
	responder := &mockResponder{response: "Capped result."}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: nilLoader,
		SystemMaxRetry: 2,
	})

	// Act
	result, err := orch.Run(context.Background(), "unlimited request")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Retries != 2 {
		t.Errorf("expected 2 retries (system limit), got %d", result.Retries)
	}
	// initial eval + 2 retry evals = 3 total
	if evaluator.calls != 3 {
		t.Errorf("expected 3 Evaluate calls, got %d", evaluator.calls)
	}
	if !responder.called {
		t.Error("Responder should have been called after system hard limit")
	}
}

// TestOrchestrator_PlanValidationError verifies that an invalid PlanOutput
// from the Planner propagates as an error.
func TestOrchestrator_PlanValidationError(t *testing.T) {
	// Arrange
	planner := &invalidPlanner{}
	executor := &mockExecutor{}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{{Satisfied: true}}}
	responder := &mockResponder{}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: nilLoader,
		SystemMaxRetry: 5,
	})

	// Act
	_, err := orch.Run(context.Background(), "anything")

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid plan, got nil")
	}
	if executor.called {
		t.Error("Executor should NOT have been called after plan validation failure")
	}
}

// TestOrchestrator_ExecuteError verifies that an error from the Executor
// is propagated immediately.
func TestOrchestrator_ExecuteError(t *testing.T) {
	// Arrange
	execErr := errors.New("execution failed: agent crashed")
	planner := &mockPlanner{plans: []*domain.PlanOutput{stepPlan(3)}}
	executor := &mockExecutor{err: execErr}
	evaluator := &mockEvaluator{evals: []*domain.EvalOutput{{Satisfied: true}}}
	responder := &mockResponder{}

	orch := orchestrator.New(orchestrator.Config{
		Planner:        planner,
		Evaluator:      evaluator,
		Responder:      responder,
		Executor:       executor,
		TemplateLoader: nilLoader,
		SystemMaxRetry: 5,
	})

	// Act
	_, err := orch.Run(context.Background(), "do something")

	// Assert
	if err == nil {
		t.Fatal("expected error from Executor, got nil")
	}
	if !errors.Is(err, execErr) {
		t.Errorf("expected wrapped execErr, got: %v", err)
	}
	if evaluator.calls > 0 {
		t.Error("Evaluator should NOT have been called after Executor error")
	}
	if responder.called {
		t.Error("Responder should NOT have been called after Executor error")
	}
}
