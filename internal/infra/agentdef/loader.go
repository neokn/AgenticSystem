// Package agentdef loads agent definitions from agents/<name>/agent.prompt files.
// Each agent is a directory under agents/ containing a dotprompt file that defines
// the system instruction and optional frontmatter (model, input schema, etc.).
//
// Architecture: Infrastructure / Driven Adapter.
// Implements domain.AgentLoader.
package agentdef

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dp "github.com/google/dotprompt/go/dotprompt"

	"github.com/neokn/agenticsystem/internal/domain"
)

// Definition holds the parsed contents of an agent.prompt file.
// Kept for internal use; the canonical domain type is domain.AgentDefinition.
type Definition struct {
	// Name is the agent directory name (e.g. "demo_agent").
	Name string

	// Instruction is the system instruction extracted from the rendered prompt.
	Instruction string

	// ModelID is the model from the frontmatter (e.g. "gemini-3-flash-preview").
	// Empty if not specified in the prompt file.
	ModelID string
}

// Loader implements domain.AgentLoader by reading dotprompt files.
type Loader struct{}

// Load reads agents/<name>/agent.prompt relative to baseDir and returns a
// domain.AgentDefinition. baseDir is typically the project root.
func (l *Loader) Load(baseDir, name string) (*domain.AgentDefinition, error) {
	def, err := Load(baseDir, name)
	if err != nil {
		return nil, err
	}
	return &domain.AgentDefinition{
		Name:        def.Name,
		Instruction: def.Instruction,
		ModelID:     def.ModelID,
	}, nil
}

// Load reads agents/<name>/agent.prompt relative to baseDir and returns a
// Definition. baseDir is typically the project root.
func Load(baseDir, name string) (*Definition, error) {
	promptPath := filepath.Join(baseDir, "agents", name, "agent.prompt")
	data, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("agentdef.Load: reading %q: %w", promptPath, err)
	}

	dotprompt := dp.NewDotprompt(nil)
	rendered, err := dotprompt.Render(string(data), &dp.DataArgument{}, nil)
	if err != nil {
		return nil, fmt.Errorf("agentdef.Load: rendering %q: %w", promptPath, err)
	}

	// Collect system messages as the instruction.
	var systemParts []string
	for _, msg := range rendered.Messages {
		if msg.Role == dp.RoleSystem {
			for _, part := range msg.Content {
				if tp, ok := part.(*dp.TextPart); ok {
					if text := strings.TrimSpace(tp.Text); text != "" {
						systemParts = append(systemParts, text)
					}
				}
			}
		}
	}

	def := &Definition{
		Name:        name,
		Instruction: strings.Join(systemParts, "\n"),
	}

	// Extract model ID from frontmatter if present.
	// dotprompt uses "googleai/<model>" format; strip the provider prefix.
	if rendered.Model != "" {
		def.ModelID = rendered.Model
		if _, after, ok := strings.Cut(def.ModelID, "/"); ok {
			def.ModelID = after
		}
	}

	return def, nil
}
