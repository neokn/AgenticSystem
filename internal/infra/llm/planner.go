// Package llm provides Gemini-backed implementations of the orchestrator ports.
package llm

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// GeminiPlanner implements orchestrator.Planner using the Gemini generative AI
// API with structured JSON output constrained by PlanSchema.
type GeminiPlanner struct {
	// Client is the Gemini API client. Must not be nil.
	Client *genai.Client

	// Model is the Gemini model identifier (e.g. "gemini-2.5-flash").
	Model string

	// SystemPrompt is the raw system prompt template loaded from
	// prompts/plan.prompt. The {{AVAILABLE_TOOLS}} and {{AVAILABLE_ROLES}}
	// placeholders are replaced at call time.
	SystemPrompt string
}

// Plan calls the Gemini API to produce a structured execution plan.
//
// The system instruction is built by substituting the AVAILABLE_TOOLS and
// AVAILABLE_ROLES placeholders into the SystemPrompt template. When feedback
// is non-empty (a retry iteration), it is appended to the user content so the
// model can correct its previous plan.
func (p *GeminiPlanner) Plan(
	ctx context.Context,
	userPrompt, feedback string,
	availableTools, availableRoles []string,
) (*domain.PlanOutput, error) {
	// Build system instruction with template substitution.
	sysInstr := buildPlanSystemInstruction(p.SystemPrompt, availableTools, availableRoles)

	// Build user content.
	userText := userPrompt
	if feedback != "" {
		userText += "\n\nPrevious feedback (please incorporate):\n" + feedback
	}

	contents := []*genai.Content{
		genai.NewContentFromText(userText, genai.RoleUser),
	}

	config := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(sysInstr, genai.RoleUser),
		ResponseMIMEType:  "application/json",
		ResponseSchema:    PlanSchema(),
	}

	resp, err := p.Client.Models.GenerateContent(ctx, p.Model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("llm: GeminiPlanner.Plan: %w", err)
	}

	text := resp.Text()
	out, err := parsePlanOutput(text)
	if err != nil {
		return nil, fmt.Errorf("llm: GeminiPlanner.Plan: parse response: %w", err)
	}
	return out, nil
}
