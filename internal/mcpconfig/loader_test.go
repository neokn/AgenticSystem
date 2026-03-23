// Package mcpconfig_test tests the MCP config loader.
package mcpconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neokn/agenticsystem/internal/mcpconfig"
)

// writeFile writes content to baseDir/agents/<agentDir>/mcp.json, creating
// intermediate directories as needed. Returns the baseDir.
func writeMCPFile(t *testing.T, agentDir, content string) string {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "agents", agentDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}
	return base
}

// Test_Load_should_return_single_server_config_when_valid_file_has_one_entry verifies
// that Load parses a well-formed mcp.json with one server entry correctly.
func Test_Load_should_return_single_server_config_when_valid_file_has_one_entry(t *testing.T) {
	// Arrange
	base := writeMCPFile(t, "demo_agent", `{
		"servers": [
			{"name": "echo-server", "command": "node", "args": ["echo-server.js"]}
		]
	}`)

	// Act
	cfg, err := mcpconfig.Load(base, "demo_agent")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil MCPConfig, got nil")
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.Servers))
	}
	s := cfg.Servers[0]
	if s.Name != "echo-server" {
		t.Errorf("expected name %q, got %q", "echo-server", s.Name)
	}
	if s.Command != "node" {
		t.Errorf("expected command %q, got %q", "node", s.Command)
	}
	if len(s.Args) != 1 || s.Args[0] != "echo-server.js" {
		t.Errorf("expected args [echo-server.js], got %v", s.Args)
	}
}

// Test_Load_should_return_env_map_when_server_entry_has_env_field verifies
// that env key-value pairs are parsed into the Env map.
func Test_Load_should_return_env_map_when_server_entry_has_env_field(t *testing.T) {
	// Arrange
	base := writeMCPFile(t, "demo_agent", `{
		"servers": [
			{"name": "srv", "command": "bin", "env": {"MY_KEY": "my_val", "ANOTHER": "val2"}}
		]
	}`)

	// Act
	cfg, err := mcpconfig.Load(base, "demo_agent")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].Env["MY_KEY"] != "my_val" {
		t.Errorf("expected MY_KEY=my_val, got %q", cfg.Servers[0].Env["MY_KEY"])
	}
	if cfg.Servers[0].Env["ANOTHER"] != "val2" {
		t.Errorf("expected ANOTHER=val2, got %q", cfg.Servers[0].Env["ANOTHER"])
	}
}

// Test_Load_should_return_nil_nil_when_mcp_json_is_absent verifies the
// optional-config contract: missing file returns (nil, nil).
func Test_Load_should_return_nil_nil_when_mcp_json_is_absent(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentDir := filepath.Join(base, "agents", "demo_agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// No mcp.json file written.

	// Act
	cfg, err := mcpconfig.Load(base, "demo_agent")

	// Assert
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil MCPConfig for missing file, got %+v", cfg)
	}
}

// Test_Load_should_return_parse_error_when_json_is_malformed verifies that
// malformed JSON yields an error containing "mcpconfig" and "parse".
func Test_Load_should_return_parse_error_when_json_is_malformed(t *testing.T) {
	// Arrange
	base := writeMCPFile(t, "demo_agent", `{not valid json`)

	// Act
	_, err := mcpconfig.Load(base, "demo_agent")

	// Assert
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "mcpconfig") {
		t.Errorf("expected error to contain %q, got %q", "mcpconfig", err.Error())
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected error to contain %q, got %q", "parse", err.Error())
	}
}

// Test_Load_should_return_validation_error_naming_server_when_command_is_empty
// verifies that an empty Command field yields an error naming the offending server.
func Test_Load_should_return_validation_error_naming_server_when_command_is_empty(t *testing.T) {
	// Arrange
	base := writeMCPFile(t, "demo_agent", `{
		"servers": [
			{"name": "bad-server", "command": ""}
		]
	}`)

	// Act
	_, err := mcpconfig.Load(base, "demo_agent")

	// Assert
	if err == nil {
		t.Fatal("expected validation error for empty command, got nil")
	}
	if !strings.Contains(err.Error(), "bad-server") {
		t.Errorf("expected error to name server %q, got %q", "bad-server", err.Error())
	}
	if !strings.Contains(err.Error(), "command") {
		t.Errorf("expected error to mention %q field, got %q", "command", err.Error())
	}
}

// Test_Load_should_return_two_servers_when_file_has_two_entries verifies
// that ordering is preserved for multiple server entries.
func Test_Load_should_return_two_servers_when_file_has_two_entries(t *testing.T) {
	// Arrange
	base := writeMCPFile(t, "demo_agent", `{
		"servers": [
			{"name": "first", "command": "cmd1"},
			{"name": "second", "command": "cmd2"}
		]
	}`)

	// Act
	cfg, err := mcpconfig.Load(base, "demo_agent")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "first" || cfg.Servers[1].Name != "second" {
		t.Errorf("unexpected server order: %v, %v", cfg.Servers[0].Name, cfg.Servers[1].Name)
	}
}
