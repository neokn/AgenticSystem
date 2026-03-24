// Package llm provides Gemini response schema definitions for structured output.
package llm

import "google.golang.org/genai"

// PlanSchema returns the genai.Schema for the plan phase response.
//
// The schema matches domain.PlanOutput:
//   - intent     (string, required)
//   - max_retries (integer, required)
//   - plan        (PlanNode, required)
//
// PlanNode is defined inline to 3 levels of nesting (sufficient for realistic
// plans). The root plan node accepts types: direct, sequential, loop, parallel.
// Child nodes in steps[] accept: step, sequential, loop, parallel.
func PlanSchema() *genai.Schema {
	// child3 is the leaf-level step schema (no further nesting).
	child3 := planNodeSchema(false, nil)

	// child2 can reference child3 in its steps[].
	child2 := planNodeSchema(false, child3)

	// child1 can reference child2 in its steps[].
	child1 := planNodeSchema(false, child2)

	// root plan node: accepts direct/sequential/loop/parallel, steps reference child1.
	root := planNodeSchema(true, child1)

	return &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Structured plan output from the Root LLM plan phase.",
		Properties: map[string]*genai.Schema{
			"intent": {
				Type:        genai.TypeString,
				Description: "The intent of the plan — what the user wants to achieve.",
			},
			"max_retries": {
				Type:        genai.TypeInteger,
				Description: "Maximum number of retries if evaluation fails.",
			},
			"plan": root,
		},
		Required: []string{"intent", "max_retries", "plan"},
	}
}

// EvalSchema returns the genai.Schema for the evaluate phase response.
//
// The schema matches domain.EvalOutput:
//   - satisfied (boolean, required)
//   - feedback  (string, required)
func EvalSchema() *genai.Schema {
	return &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Structured evaluation output from the Root LLM evaluate phase.",
		Properties: map[string]*genai.Schema{
			"satisfied": {
				Type:        genai.TypeBoolean,
				Description: "Whether the agent's response satisfies the user's intent.",
			},
			"feedback": {
				Type:        genai.TypeString,
				Description: "Feedback explaining the evaluation result.",
			},
		},
		Required: []string{"satisfied", "feedback"},
	}
}

// planNodeSchema builds the genai.Schema for a single PlanNode.
//
// atRoot controls the type enum:
//   - atRoot=true:  ["direct", "sequential", "loop", "parallel"] — direct is
//     valid only at the root level.
//   - atRoot=false: ["step", "sequential", "loop", "parallel"] — direct is
//     not valid inside a steps[] array.
//
// stepsItemSchema is the schema for items inside steps[]. Pass nil for the
// deepest nesting level (leaf nodes cannot have further children).
func planNodeSchema(atRoot bool, stepsItemSchema *genai.Schema) *genai.Schema {
	var typeEnum []string
	if atRoot {
		typeEnum = []string{"direct", "sequential", "loop", "parallel"}
	} else {
		typeEnum = []string{"step", "sequential", "loop", "parallel"}
	}

	properties := map[string]*genai.Schema{
		"type": {
			Type:        genai.TypeString,
			Description: "The type of plan node.",
			Enum:        typeEnum,
		},
		// direct only
		"response": {
			Type:        genai.TypeString,
			Description: "The direct response text (used when type=direct).",
		},
		// step only
		"role": {
			Type:        genai.TypeString,
			Description: "The agent role to invoke (used when type=step).",
		},
		"instruction": {
			Type:        genai.TypeString,
			Description: "The instruction for the agent (used when type=step).",
		},
		"tools": {
			Type:        genai.TypeArray,
			Description: "Tool names available to the agent (used when type=step).",
			Items: &genai.Schema{
				Type: genai.TypeString,
			},
		},
		"output_key": {
			Type:        genai.TypeString,
			Description: "Key under which the agent's output is stored (used when type=step).",
		},
		// loop only
		"max_iterations": {
			Type:        genai.TypeInteger,
			Description: "Maximum number of loop iterations (used when type=loop).",
		},
		"exit_condition": exitConditionSchema(),
	}

	// Add steps[] only when a child schema is provided (non-leaf levels).
	if stepsItemSchema != nil {
		properties["steps"] = &genai.Schema{
			Type:        genai.TypeArray,
			Description: "Child nodes (used when type=sequential, loop, or parallel).",
			Items:        stepsItemSchema,
		}
	}

	return &genai.Schema{
		Type:       genai.TypeObject,
		Properties: properties,
		Required:   []string{"type"},
	}
}

// exitConditionSchema returns the schema for ExitCondition (loop only).
func exitConditionSchema() *genai.Schema {
	return &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Optional early-exit condition for a loop node.",
		Properties: map[string]*genai.Schema{
			"output_key": {
				Type:        genai.TypeString,
				Description: "The output key to inspect for the exit pattern.",
			},
			"pattern": {
				Type:        genai.TypeString,
				Description: "Regex pattern; loop exits when a match is found.",
			},
		},
		Required: []string{"output_key", "pattern"},
	}
}
