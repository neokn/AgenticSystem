// Package domain — agent tree configuration types.
//
// These types represent the declarative agent tree configuration that describes
// the Root -> Workflow -> Agent hierarchy. They are pure domain types with no
// external dependencies (stdlib only).
package domain

// AgentType enumerates the supported ADK agent types in the tree config.
type AgentType string

const (
	AgentTypeLLM        AgentType = "llm"
	AgentTypeSequential AgentType = "sequential"
	AgentTypeLoop       AgentType = "loop"
	AgentTypeParallel   AgentType = "parallel"
)

// AgentTreeConfig is the top-level declarative configuration for the entire
// agent tree. It describes Root -> Workflows -> Agents as a tree structure
// that can be loaded from a YAML file and recursively built into ADK agents.
type AgentTreeConfig struct {
	// Version is the config schema version (e.g. "1").
	Version string `yaml:"version"`

	// Defaults provides shared default values inherited by all agents
	// unless explicitly overridden.
	Defaults AgentDefaults `yaml:"defaults"`

	// Root is the root agent node — the single entry point for all requests.
	Root AgentNodeConfig `yaml:"root"`
}

// AgentDefaults holds default values shared across the agent tree.
type AgentDefaults struct {
	// Model is the default LLM model ID (e.g. "gemini-3-flash-preview").
	Model string `yaml:"model"`
}

// AgentNodeConfig describes a single node in the agent tree.
// It is recursive: each node may contain sub-agents.
type AgentNodeConfig struct {
	// Name is the unique agent name within the tree. Must be non-empty.
	Name string `yaml:"name"`

	// Type is the agent type: "llm", "sequential", "loop", or "parallel".
	Type AgentType `yaml:"type"`

	// Description is a one-line description used by LLM for routing decisions.
	Description string `yaml:"description"`

	// Model overrides the default model for this LLM agent.
	// Only applicable when Type == "llm".
	Model string `yaml:"model,omitempty"`

	// PromptFile is the path to the agent's dotprompt file (relative to agents/ dir).
	// Only applicable when Type == "llm". If empty, uses agents/<name>/agent.prompt.
	PromptFile string `yaml:"prompt_file,omitempty"`

	// Instruction is an inline system instruction (alternative to prompt_file).
	// Only applicable when Type == "llm". prompt_file takes precedence.
	Instruction string `yaml:"instruction,omitempty"`

	// OutputKey is the session state key where the agent's output is stored.
	// Used by workflow agents to pass data between steps.
	OutputKey string `yaml:"output_key,omitempty"`

	// MaxIterations is the maximum loop count. Only applicable when Type == "loop".
	// 0 means unlimited (loop until escalate).
	MaxIterations uint `yaml:"max_iterations,omitempty"`

	// Tools lists the built-in tools to attach to this agent.
	// Only applicable when Type == "llm".
	Tools []string `yaml:"tools,omitempty"`

	// MCPServers lists MCP server names to attach to this agent.
	// These reference entries defined in the agent's mcp.json.
	// Only applicable when Type == "llm".
	MCPServers []string `yaml:"mcp_servers,omitempty"`

	// SubAgents are the child nodes in the tree.
	// For "sequential" and "loop": executed in listed order.
	// For "parallel": executed concurrently.
	// For "llm": available as transfer targets.
	SubAgents []AgentNodeConfig `yaml:"sub_agents,omitempty"`
}

// StateKeyConfig defines the well-known session state keys used for
// agent-to-agent communication through the session state.
type StateKeyConfig struct {
	// UserIntent captures the parsed user intent from Root.
	UserIntent string

	// Plan holds the structured plan from a Planner agent.
	Plan string

	// Artifacts holds intermediate work products from Executor agents.
	Artifacts string

	// Draft holds the current working draft in iterative workflows.
	Draft string

	// Evaluation holds the evaluator's assessment in iterative workflows.
	Evaluation string

	// Summary holds the final summary from a Reporter agent.
	Summary string
}

// DefaultStateKeys returns the canonical state key names.
func DefaultStateKeys() StateKeyConfig {
	return StateKeyConfig{
		UserIntent: "user_intent",
		Plan:       "plan",
		Artifacts:  "artifacts",
		Draft:      "draft",
		Evaluation: "evaluation",
		Summary:    "summary",
	}
}

// Validate checks the AgentTreeConfig for structural correctness.
// Returns nil if valid, or an error describing the first problem found.
func (c *AgentTreeConfig) Validate() error {
	if c.Version == "" {
		return &ValidationError{Field: "version", Reason: "must not be empty"}
	}
	if c.Root.Name == "" {
		return &ValidationError{Field: "root.name", Reason: "must not be empty"}
	}
	names := make(map[string]bool)
	return validateNode(&c.Root, "root", names)
}

func validateNode(n *AgentNodeConfig, path string, names map[string]bool) error {
	if n.Name == "" {
		return &ValidationError{Field: path + ".name", Reason: "must not be empty"}
	}
	if names[n.Name] {
		return &ValidationError{Field: path + ".name", Reason: "duplicate agent name: " + n.Name}
	}
	names[n.Name] = true

	switch n.Type {
	case AgentTypeLLM, AgentTypeSequential, AgentTypeLoop, AgentTypeParallel:
		// valid
	case "":
		return &ValidationError{Field: path + ".type", Reason: "must not be empty"}
	default:
		return &ValidationError{Field: path + ".type", Reason: "unsupported agent type: " + string(n.Type)}
	}

	if n.Type == AgentTypeLoop && n.MaxIterations == 0 && len(n.SubAgents) == 0 {
		return &ValidationError{Field: path, Reason: "loop agent must have sub_agents"}
	}

	if (n.Type == AgentTypeSequential || n.Type == AgentTypeParallel) && len(n.SubAgents) == 0 {
		return &ValidationError{Field: path, Reason: string(n.Type) + " agent must have sub_agents"}
	}

	for i, sub := range n.SubAgents {
		subPath := path + ".sub_agents[" + itoa(i) + "]"
		if err := validateNode(&sub, subPath, names); err != nil {
			return err
		}
	}
	return nil
}

// itoa converts an int to its string representation without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ValidationError describes a structural problem in the agent tree config.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return "agenttree: invalid " + e.Field + ": " + e.Reason
}
