// Package domain contains the application's core domain model: data types and
// port interfaces that define what the Application Layer needs from the outside
// world. This package must never import infrastructure packages.
//
// Dependency rule (see docs/adr/0009-explicit-architecture-layer-structure.md):
//
//	internal/domain MUST NOT import:
//	  - internal/infra/*
//	  - internal/app/*
//	  - google.golang.org/adk
//	  - google.golang.org/genai
//	  - github.com/google/dotprompt
//	  - github.com/modelcontextprotocol/go-sdk
//
// Only stdlib imports are permitted here.
package domain

// AgentDefinition is the canonical data transfer object for a loaded agent.
// It is the return type of AgentLoader and the authoritative definition of what
// an agent configuration contains from the Application Layer's perspective.
type AgentDefinition struct {
	// Name is the agent directory name (e.g. "demo_agent").
	Name string

	// Instruction is the system instruction text for the LLM.
	Instruction string

	// ModelID is the model identifier from the agent definition frontmatter
	// (e.g. "gemini-2.0-flash"). Empty if not specified.
	ModelID string
}

// AgentLoader is the port for loading agent definitions from the agent store.
// The Application Layer uses this interface; the Infrastructure Layer implements
// it (see internal/infra/agentdef).
//
// Load reads the agent definition identified by (baseDir, name) and returns an
// AgentDefinition. baseDir is typically the project root; name is the
// subdirectory under agents/ (e.g. "demo_agent").
//
// Returns an error if the agent definition cannot be found or parsed.
type AgentLoader interface {
	Load(baseDir, name string) (*AgentDefinition, error)
}

// MCPServerConfig describes a single MCP server subprocess.
// This is the domain representation of one MCP server entry — isomorphic to
// internal/infra/mcpconfig.ServerConfig but owned by the domain layer so that
// port signatures do not import infrastructure types.
type MCPServerConfig struct {
	// Name is the logical name of the server (used in error messages).
	Name string

	// Command is the executable to launch as the MCP server subprocess.
	Command string

	// Args are the command-line arguments passed to Command.
	Args []string

	// Env contains additional environment variables to merge into the
	// subprocess environment. These override inherited process variables.
	Env map[string]string
}

// MCPConfig is the domain representation of an MCP server configuration file.
// The Application Layer passes this to the ToolProvider port (defined in
// internal/app/ports.go); the Infrastructure adapter (internal/infra/mcpconfig)
// translates its own type to this type before returning it to the app layer.
type MCPConfig struct {
	// Servers is the ordered list of MCP server entries.
	Servers []MCPServerConfig
}
