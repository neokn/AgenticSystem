// Package port contains the Application Layer port interfaces.
// Ports define the contracts between the core application and external adapters.
package port

import (
	"github.com/neokn/agenticsystem/internal/core/domain"
)

// AgentLoader is the port for loading agent definitions from the agent store.
// The Application Layer uses this interface; the Infrastructure Layer implements
// it (see internal/infra/config/agentdef).
//
// Load reads the agent definition identified by (baseDir, name) and returns an
// AgentDefinition. baseDir is typically the project root; name is the
// subdirectory under agents/ (e.g. "demo_agent").
//
// Returns an error if the agent definition cannot be found or parsed.
type AgentLoader interface {
	Load(baseDir, name string) (*domain.AgentDefinition, error)
}
