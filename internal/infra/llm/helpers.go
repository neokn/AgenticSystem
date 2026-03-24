package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// buildPlanSystemInstruction replaces the template placeholders
// {{AVAILABLE_TOOLS}} and {{AVAILABLE_ROLES}} with comma-separated lists of
// the provided tools and roles slices.
func buildPlanSystemInstruction(template string, tools, roles []string) string {
	toolsStr := strings.Join(tools, ", ")
	rolesStr := strings.Join(roles, ", ")

	result := strings.ReplaceAll(template, "{{AVAILABLE_TOOLS}}", toolsStr)
	result = strings.ReplaceAll(result, "{{AVAILABLE_ROLES}}", rolesStr)
	return result
}

// parsePlanOutput decodes a JSON string into a *domain.PlanOutput.
// Returns an error if the JSON is invalid or empty.
func parsePlanOutput(jsonStr string) (*domain.PlanOutput, error) {
	if jsonStr == "" {
		return nil, fmt.Errorf("llm: parsePlanOutput: empty response")
	}

	var out domain.PlanOutput
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return nil, fmt.Errorf("llm: parsePlanOutput: %w", err)
	}
	return &out, nil
}

// parseEvalOutput decodes a JSON string into a *domain.EvalOutput.
// Returns an error if the JSON is invalid or empty.
func parseEvalOutput(jsonStr string) (*domain.EvalOutput, error) {
	if jsonStr == "" {
		return nil, fmt.Errorf("llm: parseEvalOutput: empty response")
	}

	var out domain.EvalOutput
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return nil, fmt.Errorf("llm: parseEvalOutput: %w", err)
	}
	return &out, nil
}

// formatResults converts a map[string]any into a newline-separated list of
// "key=value" pairs suitable for inclusion in an LLM prompt.
//
// For scalar values (string, int, float, bool), the Go default string
// representation is used. For complex values (structs, maps, slices), the
// value is JSON-encoded.
func formatResults(results map[string]any) string {
	if len(results) == 0 {
		return ""
	}

	// Sort keys for deterministic output — helps with testability.
	keys := make([]string, 0, len(results))
	for k := range results {
		keys = append(keys, k)
	}
	sortStrings(keys)

	var sb strings.Builder
	for i, k := range keys {
		v := results[k]
		var valStr string

		switch tv := v.(type) {
		case string:
			valStr = tv
		case int:
			valStr = fmt.Sprintf("%d", tv)
		case int64:
			valStr = fmt.Sprintf("%d", tv)
		case float64:
			valStr = fmt.Sprintf("%g", tv)
		case bool:
			if tv {
				valStr = "true"
			} else {
				valStr = "false"
			}
		default:
			b, err := json.Marshal(v)
			if err != nil {
				valStr = fmt.Sprintf("%v", v)
			} else {
				valStr = string(b)
			}
		}

		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(valStr)
		if i < len(keys)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// sortStrings is a simple insertion sort to avoid importing "sort" for a small
// list. For very large maps, the standard library sort would be preferred.
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		key := ss[i]
		j := i - 1
		for j >= 0 && ss[j] > key {
			ss[j+1] = ss[j]
			j--
		}
		ss[j+1] = key
	}
}
