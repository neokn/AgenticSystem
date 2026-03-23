// Package agenttree recursively constructs an ADK agent tree from a declarative
// AgentTreeConfig. This is the Application Layer orchestrator that combines
// domain config types with ADK framework types to build the full agent hierarchy.
//
// Architecture: Application Layer.
// Depends on: domain (config types), ADK (agent construction), infra (via injected interfaces).
package agenttree

import (
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
	"google.golang.org/adk/agent/workflowagents/parallelagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// Deps holds the external dependencies injected into the builder.
// This allows the Application Layer to remain decoupled from infrastructure.
type Deps struct {
	// ModelFactory creates an LLM model instance from a model ID.
	ModelFactory func(modelID string) (model.LLM, error)

	// PromptLoader loads an agent's system instruction from its prompt file.
	// (baseDir, agentName) -> instruction text.
	PromptLoader func(baseDir, agentName string) (string, error)

	// ToolRegistry maps tool names to tool.Tool instances.
	// Key is the tool name as declared in the config YAML.
	ToolRegistry map[string]tool.Tool

	// ToolsetRegistry maps MCP server names to tool.Toolset instances.
	// Key is the MCP server name as declared in the config YAML.
	ToolsetRegistry map[string]tool.Toolset

	// BaseDir is the project root directory for resolving prompt files.
	BaseDir string
}

// Build recursively constructs the ADK agent tree from the given config.
// Returns the root agent.Agent, ready to be passed to an ADK runner.
func Build(cfg *domain.AgentTreeConfig, deps Deps) (agent.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("agenttree.Build: config is nil")
	}
	return buildNode(&cfg.Root, cfg.Defaults, deps)
}

// buildNode recursively builds a single agent node and all its children.
func buildNode(node *domain.AgentNodeConfig, defaults domain.AgentDefaults, deps Deps) (agent.Agent, error) {
	// First, recursively build all sub-agents.
	subAgents := make([]agent.Agent, 0, len(node.SubAgents))
	for i := range node.SubAgents {
		child, err := buildNode(&node.SubAgents[i], defaults, deps)
		if err != nil {
			return nil, fmt.Errorf("building sub-agent %q: %w", node.SubAgents[i].Name, err)
		}
		subAgents = append(subAgents, child)
	}

	switch node.Type {
	case domain.AgentTypeLLM:
		return buildLLMAgent(node, defaults, deps, subAgents)
	case domain.AgentTypeSequential:
		return buildSequentialAgent(node, subAgents)
	case domain.AgentTypeLoop:
		return buildLoopAgent(node, subAgents)
	case domain.AgentTypeParallel:
		return buildParallelAgent(node, subAgents)
	default:
		return nil, fmt.Errorf("agenttree: unsupported agent type %q for %q", node.Type, node.Name)
	}
}

// buildLLMAgent constructs an ADK LlmAgent with model, instruction, tools, and toolsets.
func buildLLMAgent(node *domain.AgentNodeConfig, defaults domain.AgentDefaults, deps Deps, subAgents []agent.Agent) (agent.Agent, error) {
	// Resolve model: node.Model > defaults.Model
	modelID := node.Model
	if modelID == "" {
		modelID = defaults.Model
	}
	if modelID == "" {
		return nil, fmt.Errorf("agenttree: agent %q: no model specified and no default model set", node.Name)
	}

	llmModel, err := deps.ModelFactory(modelID)
	if err != nil {
		return nil, fmt.Errorf("agenttree: agent %q: creating model %q: %w", node.Name, modelID, err)
	}

	// Resolve instruction: prompt_file > instruction inline
	instruction := node.Instruction
	if node.PromptFile != "" || instruction == "" {
		// Try loading from prompt file (convention: agents/<name>/agent.prompt)
		agentName := node.Name
		if node.PromptFile != "" {
			agentName = node.PromptFile
		}
		if deps.PromptLoader != nil {
			loaded, err := deps.PromptLoader(deps.BaseDir, agentName)
			if err != nil {
				// If prompt_file was explicitly set, this is an error.
				// If it was just a convention attempt, fall back to inline instruction.
				if node.PromptFile != "" {
					return nil, fmt.Errorf("agenttree: agent %q: loading prompt %q: %w", node.Name, node.PromptFile, err)
				}
				// Convention miss is acceptable if inline instruction exists.
				if instruction == "" {
					return nil, fmt.Errorf("agenttree: agent %q: no instruction found (no prompt file and no inline instruction)", node.Name)
				}
			} else {
				instruction = loaded
			}
		}
	}

	// Resolve tools
	var tools []tool.Tool
	for _, toolName := range node.Tools {
		t, ok := deps.ToolRegistry[toolName]
		if !ok {
			return nil, fmt.Errorf("agenttree: agent %q: tool %q not found in registry", node.Name, toolName)
		}
		tools = append(tools, t)
	}

	// Resolve MCP toolsets
	var toolsets []tool.Toolset
	for _, serverName := range node.MCPServers {
		ts, ok := deps.ToolsetRegistry[serverName]
		if !ok {
			return nil, fmt.Errorf("agenttree: agent %q: MCP server %q not found in registry", node.Name, serverName)
		}
		toolsets = append(toolsets, ts)
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        node.Name,
		Description: node.Description,
		Model:       llmModel,
		Instruction: instruction,
		SubAgents:   subAgents,
		Tools:       tools,
		Toolsets:    toolsets,
		OutputKey:   node.OutputKey,
	})
	if err != nil {
		return nil, fmt.Errorf("agenttree: creating LLM agent %q: %w", node.Name, err)
	}
	return a, nil
}

// buildSequentialAgent constructs an ADK SequentialAgent.
func buildSequentialAgent(node *domain.AgentNodeConfig, subAgents []agent.Agent) (agent.Agent, error) {
	a, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        node.Name,
			Description: node.Description,
			SubAgents:   subAgents,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("agenttree: creating sequential agent %q: %w", node.Name, err)
	}
	return a, nil
}

// buildLoopAgent constructs an ADK LoopAgent.
func buildLoopAgent(node *domain.AgentNodeConfig, subAgents []agent.Agent) (agent.Agent, error) {
	a, err := loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:        node.Name,
			Description: node.Description,
			SubAgents:   subAgents,
		},
		MaxIterations: node.MaxIterations,
	})
	if err != nil {
		return nil, fmt.Errorf("agenttree: creating loop agent %q: %w", node.Name, err)
	}
	return a, nil
}

// buildParallelAgent constructs an ADK ParallelAgent.
func buildParallelAgent(node *domain.AgentNodeConfig, subAgents []agent.Agent) (agent.Agent, error) {
	a, err := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{
			Name:        node.Name,
			Description: node.Description,
			SubAgents:   subAgents,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("agenttree: creating parallel agent %q: %w", node.Name, err)
	}
	return a, nil
}
