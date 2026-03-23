// Package port contains the Application Layer port interfaces.
// Ports define the contracts between the core application and external adapters.
package port

import (
	"github.com/neokn/agenticsystem/internal/core/domain"
	"google.golang.org/adk/tool"
)

// ToolProvider is the port for constructing the tool set for an agent from an
// MCP configuration. The Application Layer uses this interface during assembly;
// the Infrastructure Layer implements it (see internal/infra/config/mcpconfig or a
// dedicated mcp adapter package).
//
// Tools returns the flat tool list and the toolset list derived from cfg.
// Returns nil slices (not an error) when cfg is nil or cfg.Servers is empty.
// Returns an error if any MCP server subprocess fails to start.
type ToolProvider interface {
	Tools(cfg *domain.MCPConfig) ([]tool.Tool, []tool.Toolset, error)
}
