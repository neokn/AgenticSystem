// Package app contains the Application Layer ports that reference framework
// types. These ports live here — not in internal/domain — because they depend
// on google.golang.org/adk/tool types that the pure domain layer must not
// import.
//
// See docs/adr/0009-explicit-architecture-layer-structure.md for the full
// rationale and dependency direction rules.
package app

import (
	"github.com/neokn/agenticsystem/internal/domain"
	"google.golang.org/adk/tool"
)

// ToolProvider is the port for constructing the tool set for an agent from an
// MCP configuration. The Application Layer uses this interface during assembly;
// the Infrastructure Layer implements it (see internal/infra/mcpconfig or a
// dedicated mcp adapter package).
//
// Tools returns the flat tool list and the toolset list derived from cfg.
// Returns nil slices (not an error) when cfg is nil or cfg.Servers is empty.
// Returns an error if any MCP server subprocess fails to start.
type ToolProvider interface {
	Tools(cfg *domain.MCPConfig) ([]tool.Tool, []tool.Toolset, error)
}
