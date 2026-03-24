package llm

import (
	"encoding/json"
	"testing"

	"github.com/neokn/agenticsystem/internal/core/domain"
)

// ---------------------------------------------------------------------------
// buildPlanSystemInstruction
// ---------------------------------------------------------------------------

// TestBuildPlanSystemInstruction_ReplacesAvailableTools verifies that the
// {{AVAILABLE_TOOLS}} placeholder is replaced with the joined tools string.
func TestBuildPlanSystemInstruction_ReplacesAvailableTools(t *testing.T) {
	t.Parallel()

	// Arrange
	template := "Tools: {{AVAILABLE_TOOLS}} and roles: {{AVAILABLE_ROLES}}"
	tools := []string{"search", "calculator"}
	roles := []string{"analyst"}

	// Act
	result := buildPlanSystemInstruction(template, tools, roles)

	// Assert
	if result == template {
		t.Error("buildPlanSystemInstruction() should have replaced placeholders")
	}
	const wantToolsStr = "search, calculator"
	if !contains(result, wantToolsStr) {
		t.Errorf("buildPlanSystemInstruction() result %q does not contain tools %q", result, wantToolsStr)
	}
}

// TestBuildPlanSystemInstruction_ReplacesAvailableRoles verifies that the
// {{AVAILABLE_ROLES}} placeholder is replaced with the joined roles string.
func TestBuildPlanSystemInstruction_ReplacesAvailableRoles(t *testing.T) {
	t.Parallel()

	// Arrange
	template := "Roles: {{AVAILABLE_ROLES}}"
	tools := []string{}
	roles := []string{"planner", "executor", "reviewer"}

	// Act
	result := buildPlanSystemInstruction(template, tools, roles)

	// Assert
	const wantRolesStr = "planner, executor, reviewer"
	if !contains(result, wantRolesStr) {
		t.Errorf("buildPlanSystemInstruction() result %q does not contain roles %q", result, wantRolesStr)
	}
}

// TestBuildPlanSystemInstruction_EmptyLists verifies that empty tool and role
// slices produce empty replacement strings (not panics or placeholder text).
func TestBuildPlanSystemInstruction_EmptyLists(t *testing.T) {
	t.Parallel()

	// Arrange
	template := "{{AVAILABLE_TOOLS}}|{{AVAILABLE_ROLES}}"
	tools := []string{}
	roles := []string{}

	// Act
	result := buildPlanSystemInstruction(template, tools, roles)

	// Assert
	const want = "|"
	if result != want {
		t.Errorf("buildPlanSystemInstruction() = %q, want %q", result, want)
	}
}

// ---------------------------------------------------------------------------
// parsePlanOutput
// ---------------------------------------------------------------------------

// TestParsePlanOutput_ValidJSON verifies that valid JSON is correctly parsed
// into a *domain.PlanOutput.
func TestParsePlanOutput_ValidJSON(t *testing.T) {
	t.Parallel()

	// Arrange
	jsonStr := `{
		"intent": "search the web",
		"max_retries": 1,
		"plan": {
			"type": "direct",
			"response": "Here is your answer."
		}
	}`

	// Act
	out, err := parsePlanOutput(jsonStr)

	// Assert
	if err != nil {
		t.Fatalf("parsePlanOutput() error = %v, want nil", err)
	}
	if out == nil {
		t.Fatal("parsePlanOutput() returned nil, want non-nil")
	}
	if out.Intent != "search the web" {
		t.Errorf("parsePlanOutput() Intent = %q, want %q", out.Intent, "search the web")
	}
	if out.MaxRetries != 1 {
		t.Errorf("parsePlanOutput() MaxRetries = %d, want 1", out.MaxRetries)
	}
	if out.Plan.Type != domain.PlanTypeDirect {
		t.Errorf("parsePlanOutput() Plan.Type = %q, want %q", out.Plan.Type, domain.PlanTypeDirect)
	}
	if out.Plan.Response != "Here is your answer." {
		t.Errorf("parsePlanOutput() Plan.Response = %q, want %q", out.Plan.Response, "Here is your answer.")
	}
}

// TestParsePlanOutput_InvalidJSON verifies that malformed JSON returns an error.
func TestParsePlanOutput_InvalidJSON(t *testing.T) {
	t.Parallel()

	// Arrange
	badJSON := `{not valid json`

	// Act
	out, err := parsePlanOutput(badJSON)

	// Assert
	if err == nil {
		t.Error("parsePlanOutput() error = nil, want non-nil for invalid JSON")
	}
	if out != nil {
		t.Errorf("parsePlanOutput() = %v, want nil on error", out)
	}
}

// TestParsePlanOutput_EmptyString verifies that an empty string returns an error.
func TestParsePlanOutput_EmptyString(t *testing.T) {
	t.Parallel()

	// Act
	out, err := parsePlanOutput("")

	// Assert
	if err == nil {
		t.Error("parsePlanOutput() error = nil, want non-nil for empty string")
	}
	if out != nil {
		t.Errorf("parsePlanOutput() = %v, want nil on error", out)
	}
}

// ---------------------------------------------------------------------------
// parseEvalOutput
// ---------------------------------------------------------------------------

// TestParseEvalOutput_ValidJSON verifies that valid JSON is correctly parsed
// into a *domain.EvalOutput.
func TestParseEvalOutput_ValidJSON(t *testing.T) {
	t.Parallel()

	// Arrange
	jsonStr := `{"satisfied": true, "feedback": "looks good"}`

	// Act
	out, err := parseEvalOutput(jsonStr)

	// Assert
	if err != nil {
		t.Fatalf("parseEvalOutput() error = %v, want nil", err)
	}
	if out == nil {
		t.Fatal("parseEvalOutput() returned nil, want non-nil")
	}
	if !out.Satisfied {
		t.Errorf("parseEvalOutput() Satisfied = false, want true")
	}
	if out.Feedback != "looks good" {
		t.Errorf("parseEvalOutput() Feedback = %q, want %q", out.Feedback, "looks good")
	}
}

// TestParseEvalOutput_InvalidJSON verifies that malformed JSON returns an error.
func TestParseEvalOutput_InvalidJSON(t *testing.T) {
	t.Parallel()

	// Arrange
	badJSON := `{bad`

	// Act
	out, err := parseEvalOutput(badJSON)

	// Assert
	if err == nil {
		t.Error("parseEvalOutput() error = nil, want non-nil for invalid JSON")
	}
	if out != nil {
		t.Errorf("parseEvalOutput() = %v, want nil on error", out)
	}
}

// ---------------------------------------------------------------------------
// formatResults
// ---------------------------------------------------------------------------

// TestFormatResults_SingleKey verifies that a single key-value pair is formatted
// correctly.
func TestFormatResults_SingleKey(t *testing.T) {
	t.Parallel()

	// Arrange
	results := map[string]any{
		"answer": "42",
	}

	// Act
	result := formatResults(results)

	// Assert
	const want = "answer=42"
	if result != want {
		t.Errorf("formatResults() = %q, want %q", result, want)
	}
}

// TestFormatResults_MultipleKeys verifies that multiple key-value pairs are
// all present in the output (order not guaranteed).
func TestFormatResults_MultipleKeys(t *testing.T) {
	t.Parallel()

	// Arrange
	results := map[string]any{
		"step1": "result one",
		"step2": 99,
	}

	// Act
	result := formatResults(results)

	// Assert
	if !contains(result, "step1=result one") {
		t.Errorf("formatResults() = %q, missing step1=result one", result)
	}
	if !contains(result, "step2=99") {
		t.Errorf("formatResults() = %q, missing step2=99", result)
	}
}

// TestFormatResults_EmptyMap verifies that an empty map returns an empty string.
func TestFormatResults_EmptyMap(t *testing.T) {
	t.Parallel()

	// Arrange
	results := map[string]any{}

	// Act
	result := formatResults(results)

	// Assert
	if result != "" {
		t.Errorf("formatResults() = %q, want empty string", result)
	}
}

// TestFormatResults_NilMap verifies that a nil map returns an empty string.
func TestFormatResults_NilMap(t *testing.T) {
	t.Parallel()

	// Act
	result := formatResults(nil)

	// Assert
	if result != "" {
		t.Errorf("formatResults() = %q, want empty string for nil map", result)
	}
}

// TestFormatResults_StructValue verifies that a struct value is JSON-encoded.
func TestFormatResults_StructValue(t *testing.T) {
	t.Parallel()

	// Arrange
	type inner struct {
		X int `json:"x"`
	}
	v := inner{X: 7}
	results := map[string]any{"data": v}

	// Act
	result := formatResults(results)

	// Assert
	encoded, _ := json.Marshal(v)
	want := "data=" + string(encoded)
	if result != want {
		t.Errorf("formatResults() = %q, want %q", result, want)
	}
}

// ---------------------------------------------------------------------------
// helper
// ---------------------------------------------------------------------------

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexSubstr(s, sub) >= 0)
}

func indexSubstr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
