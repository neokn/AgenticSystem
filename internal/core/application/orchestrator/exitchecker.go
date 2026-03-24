package orchestrator

import (
	"iter"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

// ExitCheckConfig defines when a loop should terminate early.
type ExitCheckConfig struct {
	OutputKey string
	Pattern   string
}

// NewExitChecker creates a custom agent that checks session state and emits an
// event with Actions.Escalate = true to terminate the parent LoopAgent when
// the exit condition is met.
//
// ADK's LoopAgent only terminates early when a sub-agent emits an escalation
// event. BeforeAgentCallback cannot trigger escalation (ADK v1.0.0 limitation),
// so this agent must be the last sub-agent in the loop body.
func NewExitChecker(name string, cfg ExitCheckConfig) agent.Agent {
	a, _ := agent.New(agent.Config{
		Name:        name,
		Description: "Checks loop exit condition and escalates if met.",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				state := ctx.Session().State()
				val, _ := state.Get(cfg.OutputKey)
				valStr, _ := val.(string)
				if exitCheckShouldEscalate(valStr, cfg.Pattern) {
					yield(&session.Event{
						Actions: session.EventActions{Escalate: true},
					}, nil)
				}
				// No yield = continue loop iteration
			}
		},
	})
	return a
}

// exitCheckShouldEscalate returns true if val contains pattern (case-sensitive).
// Both val and pattern must be non-empty for a match to be possible.
func exitCheckShouldEscalate(val, pattern string) bool {
	if val == "" || pattern == "" {
		return false
	}
	return strings.Contains(val, pattern)
}
