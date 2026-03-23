// Package mcpconfig_test contains integration tests for MCP toolset construction
// using the go-sdk in-memory transport pattern.
package mcpconfig_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/tool/mcptoolset"

	"github.com/neokn/agenticsystem/internal/mcpconfig"
)

// weatherInput is the input type for the test MCP tool.
type weatherInput struct {
	City string `json:"city"`
}

// weatherOutput is the output type for the test MCP tool.
type weatherOutput struct {
	Summary string `json:"summary"`
}

func weatherHandler(_ context.Context, _ *mcp.CallToolRequest, input weatherInput) (*mcp.CallToolResult, weatherOutput, error) {
	return nil, weatherOutput{Summary: "sunny in " + input.City}, nil
}

// Test_mcptoolset_construction_should_succeed_when_in_memory_server_is_running
// verifies that mcptoolset.New succeeds with an in-memory MCP transport,
// confirming the ADK toolset construction path works end-to-end.
func Test_mcptoolset_construction_should_succeed_when_in_memory_server_is_running(t *testing.T) {
	// Arrange: set up an in-memory MCP server with one tool.
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "v1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "get_weather", Description: "returns weather"}, weatherHandler)
	if _, err := server.Connect(t.Context(), serverTransport, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}

	// Act: build a toolset using the client transport (mirrors what appwire does
	// after reading mcpconfig — substituting CommandTransport with the in-memory
	// transport to avoid subprocess requirements in CI).
	ts, err := mcptoolset.New(mcptoolset.Config{
		Transport: clientTransport,
	})

	// Assert
	if err != nil {
		t.Fatalf("mcptoolset.New: %v", err)
	}
	if ts == nil {
		t.Fatal("expected non-nil toolset")
	}
}

// Test_mcpconfig_Load_fields_should_match_values_used_by_appwire_for_exec_Cmd
// verifies that mcpconfig.Load round-trips config fields correctly — these are
// the exact values appwire uses to build exec.Cmd and CommandTransport.
func Test_mcpconfig_Load_fields_should_match_values_used_by_appwire_for_exec_Cmd(t *testing.T) {
	// Arrange
	base := writeMCPFile(t, "demo_agent", `{
		"servers": [
			{
				"name": "test-server",
				"command": "node",
				"args": ["server.js"],
				"env": {"DEBUG": "1"}
			}
		]
	}`)

	// Act
	cfg, err := mcpconfig.Load(base, "demo_agent")

	// Assert
	if err != nil {
		t.Fatalf("mcpconfig.Load: %v", err)
	}
	if cfg == nil || len(cfg.Servers) != 1 {
		t.Fatalf("expected 1 server, got cfg=%v", cfg)
	}
	srv := cfg.Servers[0]
	if srv.Name != "test-server" {
		t.Errorf("expected name test-server, got %q", srv.Name)
	}
	if srv.Command != "node" {
		t.Errorf("expected command node, got %q", srv.Command)
	}
	if len(srv.Args) != 1 || srv.Args[0] != "server.js" {
		t.Errorf("expected args [server.js], got %v", srv.Args)
	}
	if srv.Env["DEBUG"] != "1" {
		t.Errorf("expected DEBUG=1 in env, got %q", srv.Env["DEBUG"])
	}
}
