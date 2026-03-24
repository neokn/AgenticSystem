package llm

import (
	"context"
	"fmt"

	"google.golang.org/genai"
)

// GeminiResponder implements orchestrator.Responder using the Gemini generative
// AI API. Unlike the Planner and Evaluator, the Responder uses free-form text
// output (no ResponseSchema) so the model can produce a natural, readable
// response.
type GeminiResponder struct {
	// Client is the Gemini API client. Must not be nil.
	Client *genai.Client

	// Model is the Gemini model identifier (e.g. "gemini-2.5-flash").
	Model string

	// SystemPrompt is the raw system prompt text for the response phase.
	SystemPrompt string
}

// Respond calls the Gemini API to produce a user-facing response string
// summarising the execution results in relation to the original prompt.
//
// The user content is composed of the original prompt followed by a formatted
// summary of the results map (key=value pairs). The raw text from the model
// response is returned directly.
func (r *GeminiResponder) Respond(
	ctx context.Context,
	userPrompt string,
	results map[string]any,
) (string, error) {
	// Build user content: original prompt + formatted results.
	userText := userPrompt
	if formatted := formatResults(results); formatted != "" {
		userText += "\n\nExecution results:\n" + formatted
	}

	contents := []*genai.Content{
		genai.NewContentFromText(userText, genai.RoleUser),
	}

	config := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(r.SystemPrompt, genai.RoleUser),
		// No ResponseMIMEType or ResponseSchema — free-form text output.
	}

	resp, err := r.Client.Models.GenerateContent(ctx, r.Model, contents, config)
	if err != nil {
		return "", fmt.Errorf("llm: GeminiResponder.Respond: %w", err)
	}

	return resp.Text(), nil
}
