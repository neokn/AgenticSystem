// Package agenttree loads the declarative agent tree configuration from YAML.
//
// Architecture: Infrastructure / Driven Adapter.
// Returns domain.AgentTreeConfig so the Application Layer receives domain types.
package agenttree

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// DefaultConfigFile is the conventional filename for the agent tree config.
const DefaultConfigFile = "agenttree.yaml"

// Load reads the agent tree configuration from baseDir/agenttree.yaml.
// Returns (nil, nil) when the file does not exist (optional-config contract).
// Returns a validated AgentTreeConfig or an error.
func Load(baseDir string) (*domain.AgentTreeConfig, error) {
	return LoadFile(filepath.Join(baseDir, DefaultConfigFile))
}

// LoadFile reads and parses the agent tree configuration from the given path.
// Returns (nil, nil) when the file does not exist.
func LoadFile(path string) (*domain.AgentTreeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("agenttree: reading %q: %w", path, err)
	}

	var cfg domain.AgentTreeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("agenttree: parsing %q: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("agenttree: validating %q: %w", path, err)
	}

	return &cfg, nil
}
