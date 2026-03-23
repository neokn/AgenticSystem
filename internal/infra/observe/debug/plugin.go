// Package debug provides an ADK plugin that dumps the full LLM request to
// stderr before each model call. It is an infrastructure concern (debug telemetry)
// extracted from the assembly layer so the application package contains only assembly logic.
//
// Architecture: Infrastructure / Driven Adapter.
package debug

import (
	"encoding/json"
	"fmt"
	"os"

	"google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
)

// New creates and returns a *plugin.Plugin that dumps the full LLM request to
// stderr. The plugin has no closures over external state and can be created
// at any point during assembly.
func New() (*plugin.Plugin, error) {
	return plugin.New(plugin.Config{
		Name:                "debug_request_dump",
		BeforeModelCallback: beforeModelCallback,
	})
}

// beforeModelCallback implements the BeforeModelCallback for the debug plugin.
func beforeModelCallback(_ agent.CallbackContext, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
	fmt.Fprintln(os.Stderr, "========== DEBUG: LLM REQUEST START ==========")

	// SystemInstruction
	if req.Config != nil && req.Config.SystemInstruction != nil {
		fmt.Fprintf(os.Stderr, "--- SystemInstruction (role=%s, %d parts) ---\n",
			req.Config.SystemInstruction.Role, len(req.Config.SystemInstruction.Parts))
		for i, p := range req.Config.SystemInstruction.Parts {
			if p.Text != "" {
				fmt.Fprintf(os.Stderr, "  [SI part %d] TEXT (%d chars):\n%s\n", i, len(p.Text), p.Text)
			}
			if p.FunctionCall != nil {
				fmt.Fprintf(os.Stderr, "  [SI part %d] FUNCTION_CALL: %s\n", i, p.FunctionCall.Name)
			}
			if p.FunctionResponse != nil {
				fmt.Fprintf(os.Stderr, "  [SI part %d] FUNCTION_RESPONSE: %s\n", i, p.FunctionResponse.Name)
			}
			if p.InlineData != nil {
				fmt.Fprintf(os.Stderr, "  [SI part %d] INLINE_DATA: mime=%s, %d bytes\n", i, p.InlineData.MIMEType, len(p.InlineData.Data))
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, "--- SystemInstruction: nil ---")
	}

	// Contents
	fmt.Fprintf(os.Stderr, "--- Contents (%d entries) ---\n", len(req.Contents))
	for i, c := range req.Contents {
		role := c.Role
		if role == "" {
			role = "(empty)"
		}
		fmt.Fprintf(os.Stderr, "  [%d] role=%s, %d parts\n", i, role, len(c.Parts))
		for j, p := range c.Parts {
			if p.Text != "" {
				fmt.Fprintf(os.Stderr, "    [%d.%d] TEXT (%d chars):\n%s\n", i, j, len(p.Text), p.Text)
			}
			if p.FunctionCall != nil {
				args := ""
				if p.FunctionCall.Args != nil {
					argsBytes, _ := json.Marshal(p.FunctionCall.Args)
					args = string(argsBytes)
				}
				fmt.Fprintf(os.Stderr, "    [%d.%d] FUNCTION_CALL: %s args=%s\n", i, j, p.FunctionCall.Name, args)
			}
			if p.FunctionResponse != nil {
				resp := ""
				if p.FunctionResponse.Response != nil {
					respBytes, _ := json.Marshal(p.FunctionResponse.Response)
					resp = string(respBytes)
				}
				fmt.Fprintf(os.Stderr, "    [%d.%d] FUNCTION_RESPONSE: %s response=%s\n", i, j, p.FunctionResponse.Name, resp)
			}
			if p.InlineData != nil {
				fmt.Fprintf(os.Stderr, "    [%d.%d] INLINE_DATA: mime=%s, %d bytes\n", i, j, p.InlineData.MIMEType, len(p.InlineData.Data))
			}
			if len(p.ThoughtSignature) > 0 {
				fmt.Fprintf(os.Stderr, "    [%d.%d] THOUGHT_SIGNATURE: %d bytes\n", i, j, len(p.ThoughtSignature))
			}
		}
	}

	// Model
	fmt.Fprintf(os.Stderr, "--- Model: %s ---\n", req.Model)

	fmt.Fprintln(os.Stderr, "========== DEBUG: LLM REQUEST END ==========")
	return nil, nil
}
