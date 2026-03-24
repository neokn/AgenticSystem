package llm

import "github.com/neokn/agenticsystem/internal/core/application/orchestrator"

// Compile-time interface compliance checks.
// These blank assignments fail to compile if the struct no longer satisfies
// the port interface, giving immediate feedback during development.
var (
	_ orchestrator.Planner   = (*GeminiPlanner)(nil)
	_ orchestrator.Evaluator = (*GeminiEvaluator)(nil)
	_ orchestrator.Responder = (*GeminiResponder)(nil)
)
