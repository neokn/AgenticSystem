// Package orchestrator contains application-layer logic for dynamic orchestration.
//
// It is a pure domain-logic package: no I/O, no ADK imports, no database or
// file-system calls. External concerns (template loading) are injected via the
// TemplateLoader function type.
//
// Architecture: Application Layer.
// Depends on: domain (PlanNode, AgentNodeConfig).
package orchestrator

import (
	"fmt"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// TemplateLoader checks whether a role has a stored prompt template.
// baseDir is the project root; role is the agent role string from PlanNode.
// Returns (instruction, true) if a template is found, or ("", false) if not.
type TemplateLoader func(baseDir, role string) (instruction string, found bool)

// counter is a depth-first, monotonically increasing index generator.
// It guarantees globally unique names across the converted tree.
type counter struct{ n int }

func (c *counter) next() int {
	v := c.n
	c.n++
	return v
}

// Convert transforms a PlanNode tree into an AgentNodeConfig tree.
// loader is called for each step node to resolve template-based instructions.
// Returns an error if node is nil or contains an unsupported type.
func Convert(node *domain.PlanNode, loader TemplateLoader) (*domain.AgentNodeConfig, error) {
	if node == nil {
		return nil, fmt.Errorf("orchestrator.Convert: node is nil")
	}
	c := &counter{}
	return convertNode(node, loader, c, "")
}

// convertNode is the recursive implementation of Convert.
// baseDir is passed through to the TemplateLoader.
func convertNode(node *domain.PlanNode, loader TemplateLoader, c *counter, baseDir string) (*domain.AgentNodeConfig, error) {
	switch node.Type {
	case domain.PlanTypeStep:
		return convertStep(node, loader, c, baseDir)
	case domain.PlanTypeSequential:
		return convertSequential(node, loader, c, baseDir)
	case domain.PlanTypeLoop:
		return convertLoop(node, loader, c, baseDir)
	case domain.PlanTypeParallel:
		return convertParallel(node, loader, c, baseDir)
	default:
		return nil, fmt.Errorf("orchestrator.Convert: unsupported plan node type %q", node.Type)
	}
}

// convertStep maps a PlanTypeStep node to an AgentTypeLLM AgentNodeConfig.
// Name: "<role>_<index>"
// Instruction: template + "\n\n" + node.Instruction (if template found), else node.Instruction.
func convertStep(node *domain.PlanNode, loader TemplateLoader, c *counter, baseDir string) (*domain.AgentNodeConfig, error) {
	idx := c.next()
	name := node.Role + "_" + itoa(idx)

	instruction := node.Instruction
	if loader != nil {
		if tmpl, found := loader(baseDir, node.Role); found {
			instruction = tmpl + "\n\n" + node.Instruction
		}
	}

	return &domain.AgentNodeConfig{
		Name:        name,
		Type:        domain.AgentTypeLLM,
		Instruction: instruction,
		OutputKey:   node.OutputKey,
		Tools:       node.Tools,
	}, nil
}

// convertSequential maps a PlanTypeSequential node to an AgentTypeSequential AgentNodeConfig.
// Name: "seq_<index>"
func convertSequential(node *domain.PlanNode, loader TemplateLoader, c *counter, baseDir string) (*domain.AgentNodeConfig, error) {
	idx := c.next()
	name := "seq_" + itoa(idx)

	subAgents, err := convertSteps(node.Steps, loader, c, baseDir)
	if err != nil {
		return nil, err
	}

	return &domain.AgentNodeConfig{
		Name:      name,
		Type:      domain.AgentTypeSequential,
		SubAgents: subAgents,
	}, nil
}

// convertLoop maps a PlanTypeLoop node to an AgentTypeLoop AgentNodeConfig.
// Name: "loop_<index>"
// The loop body steps are wrapped in a single SequentialAgent sub-agent.
// If ExitCondition is set, an exit_checker sentinel node is appended to the body.
func convertLoop(node *domain.PlanNode, loader TemplateLoader, c *counter, baseDir string) (*domain.AgentNodeConfig, error) {
	loopIdx := c.next()
	loopName := "loop_" + itoa(loopIdx)

	// Convert body steps
	bodySubAgents, err := convertSteps(node.Steps, loader, c, baseDir)
	if err != nil {
		return nil, err
	}

	// Inject exit checker if ExitCondition is set
	if node.ExitCondition != nil {
		checkerIdx := c.next()
		exitChecker := domain.AgentNodeConfig{
			Name:        "exit_checker_" + itoa(checkerIdx),
			Type:        domain.AgentTypeLLM,
			Instruction: "__EXIT_CHECKER__",
			OutputKey:   node.ExitCondition.OutputKey + "|" + node.ExitCondition.Pattern,
		}
		bodySubAgents = append(bodySubAgents, exitChecker)
	}

	// Wrap body in a sequential agent so one iteration = run all steps in order
	seqIdx := c.next()
	bodyWrapper := domain.AgentNodeConfig{
		Name:      "seq_" + itoa(seqIdx),
		Type:      domain.AgentTypeSequential,
		SubAgents: bodySubAgents,
	}

	return &domain.AgentNodeConfig{
		Name:          loopName,
		Type:          domain.AgentTypeLoop,
		MaxIterations: node.MaxIterations,
		SubAgents:     []domain.AgentNodeConfig{bodyWrapper},
	}, nil
}

// convertParallel maps a PlanTypeParallel node to an AgentTypeParallel AgentNodeConfig.
// Name: "par_<index>"
func convertParallel(node *domain.PlanNode, loader TemplateLoader, c *counter, baseDir string) (*domain.AgentNodeConfig, error) {
	idx := c.next()
	name := "par_" + itoa(idx)

	subAgents, err := convertSteps(node.Steps, loader, c, baseDir)
	if err != nil {
		return nil, err
	}

	return &domain.AgentNodeConfig{
		Name:      name,
		Type:      domain.AgentTypeParallel,
		SubAgents: subAgents,
	}, nil
}

// convertSteps converts a slice of PlanNode steps into AgentNodeConfig sub-agents.
func convertSteps(steps []domain.PlanNode, loader TemplateLoader, c *counter, baseDir string) ([]domain.AgentNodeConfig, error) {
	subAgents := make([]domain.AgentNodeConfig, 0, len(steps))
	for i := range steps {
		child, err := convertNode(&steps[i], loader, c, baseDir)
		if err != nil {
			return nil, err
		}
		subAgents = append(subAgents, *child)
	}
	return subAgents, nil
}

// itoa converts a non-negative int to its decimal string representation
// without importing strconv, keeping this package free of unnecessary deps.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
