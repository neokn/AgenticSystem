// Package mcpconfig loads MCP server configuration from mcp.json files.
// It is a pure infrastructure package: only stdlib imports (encoding/json,
// fmt, os, path/filepath). No ADK or MCP SDK dependencies.
//
// Architecture: Infrastructure / Driven Adapter.
// Returns domain.MCPConfig so the Application Layer receives domain types.
package mcpconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// serverConfig holds the configuration for a single MCP server (JSON form).
type serverConfig struct {
	// Name is the logical name of the MCP server (used in error messages).
	Name string `json:"name"`

	// Command is the executable to launch as the MCP server subprocess.
	Command string `json:"command"`

	// Args are the command-line arguments passed to Command.
	Args []string `json:"args,omitempty"`

	// Env contains additional environment variables to merge into the
	// subprocess environment. These override inherited process variables.
	Env map[string]string `json:"env,omitempty"`
}

// mcpConfigFile holds the parsed contents of an mcp.json file (JSON form).
type mcpConfigFile struct {
	// Servers is the ordered list of MCP server entries.
	Servers []serverConfig `json:"servers"`
}

// Load reads agents/<agentDir>/mcp.json relative to baseDir and returns the
// parsed configuration as a *domain.MCPConfig. It returns (nil, nil) when
// the file is absent — the optional-config contract. baseDir is typically
// the project root.
//
// Parse errors wrap with "mcpconfig" and "parse" in their message.
// Validation errors name the offending server and the invalid field.
func Load(baseDir, agentDir string) (*domain.MCPConfig, error) {
	path := filepath.Join(baseDir, "agents", agentDir, "mcp.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("mcpconfig: reading %q: %w", path, err)
	}

	var file mcpConfigFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("mcpconfig: parse %q: %w", path, err)
	}

	for _, s := range file.Servers {
		if s.Command == "" {
			return nil, fmt.Errorf("mcpconfig: server %q: command must not be empty", s.Name)
		}
	}

	servers := make([]domain.MCPServerConfig, 0, len(file.Servers))
	for _, s := range file.Servers {
		servers = append(servers, domain.MCPServerConfig{
			Name:    s.Name,
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
		})
	}

	return &domain.MCPConfig{Servers: servers}, nil
}
