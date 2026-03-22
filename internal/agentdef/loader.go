// Package agentdef loads agent definitions from agents/<name>/agent.prompt files.
// Each agent is a directory under agents/ containing a dotprompt file that defines
// the system instruction and optional frontmatter (model, input schema, etc.).
package agentdef

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dp "github.com/google/dotprompt/go/dotprompt"
)

// Definition holds the parsed contents of an agent.prompt file.
type Definition struct {
	// Name is the agent directory name (e.g. "demo_agent").
	Name string

	// Instruction is the prompt body text (below the frontmatter).
	Instruction string

	// ModelID is the model from the frontmatter (e.g. "gemini-3-flash-preview").
	// Empty if not specified in the prompt file.
	ModelID string
}

// Load reads agents/<name>/agent.prompt relative to baseDir and returns a
// Definition. baseDir is typically the project root.
func Load(baseDir, name string) (*Definition, error) {
	promptPath := filepath.Join(baseDir, "agents", name, "agent.prompt")
	data, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("agentdef.Load: reading %q: %w", promptPath, err)
	}

	parsed, err := dp.ParseDocument(string(data))
	if err != nil {
		return nil, fmt.Errorf("agentdef.Load: parsing %q: %w", promptPath, err)
	}

	def := &Definition{
		Name:        name,
		Instruction: strings.TrimSpace(parsed.Template),
	}

	// Extract model ID from frontmatter if present.
	// dotprompt uses "googleai/<model>" format; strip the provider prefix.
	if parsed.Model != "" {
		def.ModelID = parsed.Model
		if _, after, ok := strings.Cut(def.ModelID, "/"); ok {
			def.ModelID = after
		}
	}

	return def, nil
}
