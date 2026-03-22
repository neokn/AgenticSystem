package agentdef

import (
	"log/slog"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/genai"
)

// InstructionOverridePlugin creates a plugin that replaces the SystemInstruction
// in every LLM request with the given instruction text. This runs as a
// BeforeModelCallback, after ADK's internal identity injection, effectively
// overriding the auto-injected "You are an agent. Your internal name is ..."
// with the agent's own prompt from agent.prompt.
//
// If instruction is empty, the SystemInstruction is cleared entirely.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func InstructionOverridePlugin(instruction string) (*plugin.Plugin, error) {
	return plugin.New(plugin.Config{
		Name: "instruction_override",
		BeforeModelCallback: func(_ agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
			// Log what ADK injected before we override.
			if req.Config != nil && req.Config.SystemInstruction != nil {
				for _, p := range req.Config.SystemInstruction.Parts {
					if p.Text != "" {
						slog.Debug("instruction_override: before override",
							"chars", len(p.Text),
							"preview", truncate(p.Text, 200),
						)
					}
				}
			}

			if req.Config == nil {
				req.Config = &genai.GenerateContentConfig{}
			}
			if instruction == "" {
				req.Config.SystemInstruction = nil
			} else {
				req.Config.SystemInstruction = &genai.Content{
				Parts: []*genai.Part{{Text: instruction}},
			}
			}
			return nil, nil
		},
	})
}
