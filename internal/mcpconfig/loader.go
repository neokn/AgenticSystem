// Package mcpconfig loads MCP server configuration from mcp.json files.
// It is a pure infrastructure package: only stdlib imports (encoding/json,
// fmt, os, path/filepath). No ADK or MCP SDK dependencies.
package mcpconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServerConfig holds the configuration for a single MCP server.
type ServerConfig struct {
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

// MCPConfig holds the parsed contents of an mcp.json file.
type MCPConfig struct {
	// Servers is the ordered list of MCP server entries.
	Servers []ServerConfig `json:"servers"`
}

// Load reads agents/<agentDir>/mcp.json relative to baseDir and returns the
// parsed configuration. It returns (nil, nil) when the file is absent —
// the optional-config contract. baseDir is typically the project root.
//
// Parse errors wrap with "mcpconfig" and "parse" in their message.
// Validation errors name the offending server and the invalid field.
func Load(baseDir, agentDir string) (*MCPConfig, error) {
	path := filepath.Join(baseDir, "agents", agentDir, "mcp.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("mcpconfig: reading %q: %w", path, err)
	}

	var cfg MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("mcpconfig: parse %q: %w", path, err)
	}

	for _, s := range cfg.Servers {
		if s.Command == "" {
			return nil, fmt.Errorf("mcpconfig: server %q: command must not be empty", s.Name)
		}
	}

	return &cfg, nil
}
