package llm

import (
	"testing"

	"google.golang.org/genai"
)

// TestPlanSchema_NotNil verifies that PlanSchema returns a non-nil schema.
func TestPlanSchema_NotNil(t *testing.T) {
	schema := PlanSchema()
	if schema == nil {
		t.Fatal("PlanSchema() returned nil")
	}
}

// TestPlanSchema_HasRequiredFields verifies the top-level schema has
// intent, max_retries, and plan as required properties.
func TestPlanSchema_HasRequiredFields(t *testing.T) {
	schema := PlanSchema()

	if schema.Type != genai.TypeObject {
		t.Errorf("PlanSchema() type = %v, want TypeObject", schema.Type)
	}

	requiredFields := []string{"intent", "max_retries", "plan"}
	for _, field := range requiredFields {
		if _, ok := schema.Properties[field]; !ok {
			t.Errorf("PlanSchema() missing property %q", field)
		}
	}

	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}
	for _, field := range requiredFields {
		if !requiredSet[field] {
			t.Errorf("PlanSchema() field %q not listed in Required", field)
		}
	}
}

// TestPlanSchema_PlanTypeEnum verifies the plan.type enum contains the
// correct values for the root node: direct, sequential, loop, parallel.
func TestPlanSchema_PlanTypeEnum(t *testing.T) {
	schema := PlanSchema()

	planProp, ok := schema.Properties["plan"]
	if !ok {
		t.Fatal("PlanSchema() missing 'plan' property")
	}

	typeProp, ok := planProp.Properties["type"]
	if !ok {
		t.Fatal("PlanSchema() plan.type property not found")
	}

	wantEnum := map[string]bool{
		"direct":     true,
		"sequential": true,
		"loop":       true,
		"parallel":   true,
	}

	if len(typeProp.Enum) == 0 {
		t.Fatal("PlanSchema() plan.type has no enum values")
	}

	for _, v := range typeProp.Enum {
		if !wantEnum[v] {
			t.Errorf("PlanSchema() plan.type enum contains unexpected value %q", v)
		}
		delete(wantEnum, v)
	}

	for missing := range wantEnum {
		t.Errorf("PlanSchema() plan.type enum missing value %q", missing)
	}
}

// TestEvalSchema_NotNil verifies that EvalSchema returns a non-nil schema.
func TestEvalSchema_NotNil(t *testing.T) {
	schema := EvalSchema()
	if schema == nil {
		t.Fatal("EvalSchema() returned nil")
	}
}

// TestEvalSchema_HasFields verifies that EvalSchema has satisfied (boolean)
// and feedback (string) as required properties.
func TestEvalSchema_HasFields(t *testing.T) {
	schema := EvalSchema()

	if schema.Type != genai.TypeObject {
		t.Errorf("EvalSchema() type = %v, want TypeObject", schema.Type)
	}

	satisfiedProp, ok := schema.Properties["satisfied"]
	if !ok {
		t.Fatal("EvalSchema() missing 'satisfied' property")
	}
	if satisfiedProp.Type != genai.TypeBoolean {
		t.Errorf("EvalSchema() satisfied.type = %v, want TypeBoolean", satisfiedProp.Type)
	}

	feedbackProp, ok := schema.Properties["feedback"]
	if !ok {
		t.Fatal("EvalSchema() missing 'feedback' property")
	}
	if feedbackProp.Type != genai.TypeString {
		t.Errorf("EvalSchema() feedback.type = %v, want TypeString", feedbackProp.Type)
	}

	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}
	for _, field := range []string{"satisfied", "feedback"} {
		if !requiredSet[field] {
			t.Errorf("EvalSchema() field %q not listed in Required", field)
		}
	}
}
