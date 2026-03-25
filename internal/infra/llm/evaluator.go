package llm

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// GeminiEvaluator implements orchestrator.Evaluator using the Gemini generative
// AI API with structured JSON output constrained by EvalSchema.
type GeminiEvaluator struct {
	// Client is the Gemini API client. Must not be nil.
	Client *genai.Client

	// Model is the Gemini model identifier (e.g. "gemini-2.5-flash").
	Model string

	// SystemPrompt is the raw system prompt text loaded from
	// prompts/evaluate.prompt.
	SystemPrompt string
}

// Evaluate calls the Gemini API to assess whether the execution results satisfy
// the user's original request.
//
// The user content is composed of the original prompt followed by a formatted
// summary of the results map (key=value pairs). The response is parsed into a
// *domain.EvalOutput.
func (e *GeminiEvaluator) Evaluate(
	ctx context.Context,
	userPrompt string,
	results map[string]any,
) (*domain.EvalOutput, error) {
	// Build user content: original prompt + formatted results.
	userText := userPrompt
	if formatted := formatResults(results); formatted != "" {
		userText += "\n\nExecution results:\n" + formatted
	}

	contents := []*genai.Content{
		genai.NewContentFromText(userText, genai.RoleUser),
	}

	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: e.SystemPrompt}}},
		ResponseMIMEType:  "application/json",
		ResponseSchema:    EvalSchema(),
	}

	resp, err := e.Client.Models.GenerateContent(ctx, e.Model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("llm: GeminiEvaluator.Evaluate: %w", err)
	}

	text := resp.Text()
	out, err := parseEvalOutput(text)
	if err != nil {
		return nil, fmt.Errorf("llm: GeminiEvaluator.Evaluate: parse response: %w", err)
	}
	return out, nil
}
