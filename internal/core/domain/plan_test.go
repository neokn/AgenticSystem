package domain_test

import (
	"testing"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// TestPlanOutput_Validate_should_accept_valid_direct_plan verifies that a
// PlanOutput with a direct node (response provided) passes validation.
func TestPlanOutput_Validate_should_accept_valid_direct_plan(t *testing.T) {
	p := &domain.PlanOutput{
		Intent:     "answer the user",
		MaxRetries: 0,
		Plan: domain.PlanNode{
			Type:     domain.PlanTypeDirect,
			Response: "Here is the answer.",
		},
	}

	if err := p.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// TestPlanOutput_Validate_should_reject_empty_intent verifies that an empty
// intent field is caught by validation.
func TestPlanOutput_Validate_should_reject_empty_intent(t *testing.T) {
	p := &domain.PlanOutput{
		Intent: "",
		Plan: domain.PlanNode{
			Type:     domain.PlanTypeDirect,
			Response: "some response",
		},
	}

	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for empty intent")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "intent" {
		t.Errorf("expected field 'intent', got %q", ve.Field)
	}
}

// TestPlanOutput_Validate_should_reject_direct_missing_response verifies that
// a direct node with no response is invalid.
func TestPlanOutput_Validate_should_reject_direct_missing_response(t *testing.T) {
	p := &domain.PlanOutput{
		Intent: "answer the user",
		Plan: domain.PlanNode{
			Type:     domain.PlanTypeDirect,
			Response: "",
		},
	}

	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for direct node missing response")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "plan.response" {
		t.Errorf("expected field 'plan.response', got %q", ve.Field)
	}
}

// TestPlanOutput_Validate_should_reject_sequential_with_empty_steps verifies
// that a sequential node with no steps is invalid.
func TestPlanOutput_Validate_should_reject_sequential_with_empty_steps(t *testing.T) {
	p := &domain.PlanOutput{
		Intent: "do several things",
		Plan: domain.PlanNode{
			Type:  domain.PlanTypeSequential,
			Steps: nil,
		},
	}

	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for sequential node with empty steps")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "plan.steps" {
		t.Errorf("expected field 'plan.steps', got %q", ve.Field)
	}
}

// TestPlanOutput_Validate_should_reject_loop_without_max_iterations verifies
// that a loop node with max_iterations == 0 is invalid.
func TestPlanOutput_Validate_should_reject_loop_without_max_iterations(t *testing.T) {
	p := &domain.PlanOutput{
		Intent: "loop until done",
		Plan: domain.PlanNode{
			Type:          domain.PlanTypeLoop,
			MaxIterations: 0,
			Steps: []domain.PlanNode{
				{
					Type:        domain.PlanTypeStep,
					Role:        "worker",
					Instruction: "do work",
					OutputKey:   "result",
				},
			},
		},
	}

	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for loop node with max_iterations == 0")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "plan.max_iterations" {
		t.Errorf("expected field 'plan.max_iterations', got %q", ve.Field)
	}
}

// TestPlanOutput_Validate_should_reject_step_missing_role verifies that a
// step node with no role is invalid.
func TestPlanOutput_Validate_should_reject_step_missing_role(t *testing.T) {
	p := &domain.PlanOutput{
		Intent: "run a step",
		Plan: domain.PlanNode{
			Type:        domain.PlanTypeSequential,
			Steps: []domain.PlanNode{
				{
					Type:        domain.PlanTypeStep,
					Role:        "",
					Instruction: "do something",
					OutputKey:   "result",
				},
			},
		},
	}

	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for step missing role")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "plan.steps[0].role" {
		t.Errorf("expected field 'plan.steps[0].role', got %q", ve.Field)
	}
}

// TestPlanOutput_Validate_should_reject_step_missing_output_key verifies that
// a step node with no output_key is invalid.
func TestPlanOutput_Validate_should_reject_step_missing_output_key(t *testing.T) {
	p := &domain.PlanOutput{
		Intent: "run a step",
		Plan: domain.PlanNode{
			Type:  domain.PlanTypeSequential,
			Steps: []domain.PlanNode{
				{
					Type:        domain.PlanTypeStep,
					Role:        "worker",
					Instruction: "do something",
					OutputKey:   "",
				},
			},
		},
	}

	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for step missing output_key")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "plan.steps[0].output_key" {
		t.Errorf("expected field 'plan.steps[0].output_key', got %q", ve.Field)
	}
}

// TestPlanOutput_Validate_should_reject_nested_direct verifies that a direct
// node is forbidden when nested (only valid at the root level).
func TestPlanOutput_Validate_should_reject_nested_direct(t *testing.T) {
	p := &domain.PlanOutput{
		Intent: "do things",
		Plan: domain.PlanNode{
			Type: domain.PlanTypeSequential,
			Steps: []domain.PlanNode{
				{
					Type:     domain.PlanTypeDirect,
					Response: "short-circuit",
				},
			},
		},
	}

	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for nested direct node")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "plan.steps[0].type" {
		t.Errorf("expected field 'plan.steps[0].type', got %q", ve.Field)
	}
}

// TestPlanOutput_Validate_should_accept_complex_nested_composite verifies
// that a deeply nested sequential + loop + parallel tree is valid.
func TestPlanOutput_Validate_should_accept_complex_nested_composite(t *testing.T) {
	p := &domain.PlanOutput{
		Intent:     "perform a complex multi-phase task",
		MaxRetries: 2,
		Plan: domain.PlanNode{
			Type: domain.PlanTypeSequential,
			Steps: []domain.PlanNode{
				{
					Type:      domain.PlanTypeParallel,
					Steps: []domain.PlanNode{
						{
							Type:        domain.PlanTypeStep,
							Role:        "researcher",
							Instruction: "gather data",
							OutputKey:   "research_data",
						},
						{
							Type:        domain.PlanTypeStep,
							Role:        "analyst",
							Instruction: "assess context",
							OutputKey:   "analysis",
						},
					},
				},
				{
					Type:          domain.PlanTypeLoop,
					MaxIterations: 3,
					ExitCondition: &domain.ExitCondition{
						OutputKey: "quality_check",
						Pattern:   "PASS",
					},
					Steps: []domain.PlanNode{
						{
							Type:        domain.PlanTypeStep,
							Role:        "writer",
							Instruction: "produce draft",
							OutputKey:   "draft",
						},
						{
							Type:        domain.PlanTypeStep,
							Role:        "reviewer",
							Instruction: "review draft",
							OutputKey:   "quality_check",
						},
					},
				},
				{
					Type:        domain.PlanTypeStep,
					Role:        "reporter",
					Instruction: "compile final answer",
					OutputKey:   "final_answer",
					Tools:       []string{"search", "calculate"},
				},
			},
		},
	}

	if err := p.Validate(); err != nil {
		t.Errorf("expected no error for valid complex plan, got %v", err)
	}
}

// TestPlanOutput_Validate_should_reject_loop_with_empty_steps verifies that
// a loop node with valid max_iterations but empty steps is invalid.
func TestPlanOutput_Validate_should_reject_loop_with_empty_steps(t *testing.T) {
	p := &domain.PlanOutput{
		Intent: "test",
		Plan: domain.PlanNode{
			Type:          domain.PlanTypeLoop,
			MaxIterations: 3,
		},
	}

	if err := p.Validate(); err == nil {
		t.Fatal("expected error for loop without steps")
	}
}

// TestPlanOutput_Validate_should_reject_unknown_type verifies that a plan
// node with an unknown or empty type is invalid.
func TestPlanOutput_Validate_should_reject_unknown_type(t *testing.T) {
	p := &domain.PlanOutput{
		Intent: "test",
		Plan: domain.PlanNode{
			Type: "bogus",
		},
	}

	if err := p.Validate(); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

// TestPlanOutput_Validate_should_reject_parallel_without_steps verifies that
// a parallel node with no steps is invalid.
func TestPlanOutput_Validate_should_reject_parallel_without_steps(t *testing.T) {
	p := &domain.PlanOutput{
		Intent: "test",
		Plan: domain.PlanNode{
			Type: domain.PlanTypeParallel,
		},
	}

	if err := p.Validate(); err == nil {
		t.Fatal("expected error for parallel without steps")
	}
}
