// Package domain — plan types for dynamic orchestration.
//
// These types represent the structured output from the Root LLM plan phase
// and the execution plan tree. They are pure domain types with no external
// dependencies (stdlib only).
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

// Validate checks the PlanOutput for structural correctness.
// Returns nil if valid, or a *ValidationError describing the first problem.
func (p *PlanOutput) Validate() error {
	if p.Intent == "" {
		return &ValidationError{Field: "intent", Reason: "must not be empty"}
	}
	return validatePlanNode(&p.Plan, "plan", true)
}

// validatePlanNode recursively validates a plan node.
// atRoot is true only for the top-level plan node — direct is only valid there.
func validatePlanNode(n *PlanNode, path string, atRoot bool) error {
	switch n.Type {
	case PlanTypeDirect:
		if !atRoot {
			return &ValidationError{
				Field:  path + ".type",
				Reason: "direct node is only valid at the root level",
			}
		}
		if n.Response == "" {
			return &ValidationError{
				Field:  path + ".response",
				Reason: "must not be empty for direct node",
			}
		}

	case PlanTypeSequential, PlanTypeParallel:
		if len(n.Steps) == 0 {
			return &ValidationError{
				Field:  path + ".steps",
				Reason: "must not be empty for " + string(n.Type) + " node",
			}
		}
		for i := range n.Steps {
			subPath := path + ".steps[" + itoa(i) + "]"
			if err := validatePlanNode(&n.Steps[i], subPath, false); err != nil {
				return err
			}
		}

	case PlanTypeLoop:
		if n.MaxIterations == 0 {
			return &ValidationError{
				Field:  path + ".max_iterations",
				Reason: "must be greater than 0 for loop node",
			}
		}
		if len(n.Steps) == 0 {
			return &ValidationError{
				Field:  path + ".steps",
				Reason: "must not be empty for loop node",
			}
		}
		for i := range n.Steps {
			subPath := path + ".steps[" + itoa(i) + "]"
			if err := validatePlanNode(&n.Steps[i], subPath, false); err != nil {
				return err
			}
		}

	case PlanTypeStep:
		if n.Role == "" {
			return &ValidationError{
				Field:  path + ".role",
				Reason: "must not be empty for step node",
			}
		}
		if n.OutputKey == "" {
			return &ValidationError{
				Field:  path + ".output_key",
				Reason: "must not be empty for step node",
			}
		}

	default:
		return &ValidationError{
			Field:  path + ".type",
			Reason: "unsupported plan node type: " + string(n.Type),
		}
	}
	return nil
}
