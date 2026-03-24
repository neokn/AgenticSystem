package orchestrator

import (
	"context"
	"fmt"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// ---------------------------------------------------------------------------
// Ports (interfaces) — the Orchestrator depends only on these abstractions.
// Concrete implementations live in the infrastructure layer and are injected
// at wire-up time.
// ---------------------------------------------------------------------------

// Planner generates an execution plan from a user prompt.
type Planner interface {
	Plan(ctx context.Context, userPrompt, feedback string, availableTools, availableRoles []string) (*domain.PlanOutput, error)
}

// Evaluator assesses execution results against the original request.
type Evaluator interface {
	Evaluate(ctx context.Context, userPrompt string, results map[string]any) (*domain.EvalOutput, error)
}

// Responder formats execution results into a user-friendly response.
type Responder interface {
	Respond(ctx context.Context, userPrompt string, results map[string]any) (string, error)
}

// Executor builds and runs an ADK agent tree from an AgentNodeConfig.
type Executor interface {
	Execute(ctx context.Context, cfg *domain.AgentNodeConfig) (map[string]any, error)
}

// ---------------------------------------------------------------------------
// Config & Orchestrator
// ---------------------------------------------------------------------------

// Config holds all dependencies and parameters for the Orchestrator.
type Config struct {
	Planner        Planner
	Evaluator      Evaluator
	Responder      Responder
	Executor       Executor
	TemplateLoader TemplateLoader // from converter.go
	AvailableTools []string
	AvailableRoles []string
	SystemMaxRetry int // hard upper limit on retry count (e.g. 5)
}

// Orchestrator drives the 4-phase orchestration loop:
//
//	Phase 1: Plan    → Root LLM produces a structured PlanOutput
//	Phase 2: Execute → ADK agent tree runs and produces results
//	Phase 3: Evaluate → Root LLM checks if results are satisfactory
//	Phase 4: Respond → Root LLM formats a user-facing response
//
// An outer retry loop re-runs phases 1–3 when Phase 3 returns satisfied=false,
// up to min(plan.MaxRetries, Config.SystemMaxRetry) retries.
type Orchestrator struct {
	cfg Config
}

// New creates a new Orchestrator with the given configuration.
func New(cfg Config) *Orchestrator {
	return &Orchestrator{cfg: cfg}
}

// Result carries the outcome of a completed orchestration run.
type Result struct {
	// Response is the final user-facing response text.
	Response string

	// IsDirect is true when the Planner chose a direct response (no execution).
	IsDirect bool

	// Intent is the intent string from the last PlanOutput.
	Intent string

	// Retries is the number of retry cycles performed (0 = first attempt succeeded).
	Retries int
}

// Run executes the 4-phase orchestration loop for the given user prompt.
//
// Returns (*Result, nil) on success, or (nil, error) if any phase fails fatally.
func (o *Orchestrator) Run(ctx context.Context, userPrompt string) (*Result, error) {
	feedback := ""
	retries := 0

	for {
		// ------------------------------------------------------------------
		// Phase 1: Plan
		// ------------------------------------------------------------------
		plan, err := o.cfg.Planner.Plan(
			ctx,
			userPrompt,
			feedback,
			o.cfg.AvailableTools,
			o.cfg.AvailableRoles,
		)
		if err != nil {
			return nil, fmt.Errorf("orchestrator: Plan phase failed: %w", err)
		}
		if err := plan.Validate(); err != nil {
			return nil, fmt.Errorf("orchestrator: invalid plan: %w", err)
		}

		// ------------------------------------------------------------------
		// Direct short-circuit — no execution needed
		// ------------------------------------------------------------------
		if plan.Plan.Type == domain.PlanTypeDirect {
			return &Result{
				Response: plan.Plan.Response,
				IsDirect: true,
				Intent:   plan.Intent,
				Retries:  retries,
			}, nil
		}

		// ------------------------------------------------------------------
		// Phase 2: Execute
		// ------------------------------------------------------------------
		agentCfg, err := Convert(&plan.Plan, o.cfg.TemplateLoader)
		if err != nil {
			return nil, fmt.Errorf("orchestrator: Convert phase failed: %w", err)
		}

		results, err := o.cfg.Executor.Execute(ctx, agentCfg)
		if err != nil {
			return nil, fmt.Errorf("orchestrator: Execute phase failed: %w", err)
		}

		// ------------------------------------------------------------------
		// Phase 3: Evaluate
		// ------------------------------------------------------------------
		// Determine effective retry limit: the lesser of plan and system limits.
		maxRetry := o.cfg.SystemMaxRetry
		if plan.MaxRetries < maxRetry {
			maxRetry = plan.MaxRetries
		}

		eval, err := o.cfg.Evaluator.Evaluate(ctx, userPrompt, results)
		if err != nil {
			return nil, fmt.Errorf("orchestrator: Evaluate phase failed: %w", err)
		}

		if eval.Satisfied || retries >= maxRetry {
			// ------------------------------------------------------------------
			// Phase 4: Respond
			// ------------------------------------------------------------------
			response, err := o.cfg.Responder.Respond(ctx, userPrompt, results)
			if err != nil {
				return nil, fmt.Errorf("orchestrator: Respond phase failed: %w", err)
			}
			return &Result{
				Response: response,
				IsDirect: false,
				Intent:   plan.Intent,
				Retries:  retries,
			}, nil
		}

		// Not satisfied and retries remain — loop back with feedback.
		feedback = eval.Feedback
		retries++
	}
}
