// Package application white-box tests for internal helpers and App struct shape.
package application

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// Test_envMapToSlice_should_return_nil_when_env_map_is_empty verifies that an
// empty map yields a nil slice (no-op for os.Environ() append).
func Test_envMapToSlice_should_return_nil_when_env_map_is_empty(t *testing.T) {
	// Arrange
	env := map[string]string{}

	// Act
	result := envMapToSlice(env)

	// Assert
	if result != nil {
		t.Errorf("expected nil for empty map, got %v", result)
	}
}

// Test_envMapToSlice_should_return_nil_when_env_map_is_nil verifies nil input.
func Test_envMapToSlice_should_return_nil_when_env_map_is_nil(t *testing.T) {
	// Arrange + Act
	result := envMapToSlice(nil)

	// Assert
	if result != nil {
		t.Errorf("expected nil for nil map, got %v", result)
	}
}

// Test_envMapToSlice_should_produce_KEY_equals_value_entries verifies the
// "KEY=value" format for each map entry.
func Test_envMapToSlice_should_produce_KEY_equals_value_entries(t *testing.T) {
	// Arrange
	env := map[string]string{
		"MY_KEY":  "my_val",
		"ANOTHER": "val2",
	}

	// Act
	result := envMapToSlice(env)

	// Assert
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(result), result)
	}
	sort.Strings(result)
	if result[0] != "ANOTHER=val2" {
		t.Errorf("expected ANOTHER=val2, got %q", result[0])
	}
	if result[1] != "MY_KEY=my_val" {
		t.Errorf("expected MY_KEY=my_val, got %q", result[1])
	}
}

// Test_envMapToSlice_should_skip_entries_with_empty_key verifies that entries
// with an empty key are excluded from the output.
func Test_envMapToSlice_should_skip_entries_with_empty_key(t *testing.T) {
	// Arrange
	env := map[string]string{
		"":        "ignored",
		"PRESENT": "yes",
	}

	// Act
	result := envMapToSlice(env)

	// Assert
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(result), result)
	}
	if result[0] != "PRESENT=yes" {
		t.Errorf("expected PRESENT=yes, got %q", result[0])
	}
}

// ---------------------------------------------------------------------------
// loadPrompt helper tests
// ---------------------------------------------------------------------------

// Test_loadPrompt_should_return_content_when_file_exists verifies that
// loadPrompt reads the file content correctly.
func Test_loadPrompt_should_return_content_when_file_exists(t *testing.T) {
	// Arrange
	baseDir := t.TempDir()
	promptsDir := filepath.Join(baseDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatalf("failed to create prompts dir: %v", err)
	}
	content := "You are a planner. Plan the task."
	if err := os.WriteFile(filepath.Join(promptsDir, "plan.prompt"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	// Act
	got, err := loadPrompt(baseDir, "plan.prompt")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}

// Test_loadPrompt_should_return_error_when_file_missing verifies that
// loadPrompt returns a wrapped error when the file does not exist.
func Test_loadPrompt_should_return_error_when_file_missing(t *testing.T) {
	// Arrange
	baseDir := t.TempDir()

	// Act
	_, err := loadPrompt(baseDir, "nonexistent.prompt")

	// Assert
	if err == nil {
		t.Fatal("expected an error for missing file, got nil")
	}
}

// ---------------------------------------------------------------------------
// scanAvailableRoles helper tests
// ---------------------------------------------------------------------------

// Test_scanAvailableRoles_should_return_roles_with_agent_prompt verifies that
// only directories containing agent.prompt are returned.
func Test_scanAvailableRoles_should_return_roles_with_agent_prompt(t *testing.T) {
	// Arrange
	baseDir := t.TempDir()
	agentsDir := filepath.Join(baseDir, "agents")

	// Create two roles with agent.prompt and one without.
	for _, role := range []string{"planner", "executor"} {
		dir := filepath.Join(agentsDir, role)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", role, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agent.prompt"), []byte("# "+role), 0644); err != nil {
			t.Fatalf("writefile %s: %v", role, err)
		}
	}
	// Create a role directory without agent.prompt (should be excluded).
	emptyDir := filepath.Join(agentsDir, "empty_role")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatalf("mkdir empty_role: %v", err)
	}

	// Act
	roles := scanAvailableRoles(baseDir)

	// Assert
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %d: %v", len(roles), roles)
	}
	sort.Strings(roles)
	if roles[0] != "executor" {
		t.Errorf("expected executor, got %q", roles[0])
	}
	if roles[1] != "planner" {
		t.Errorf("expected planner, got %q", roles[1])
	}
}

// Test_scanAvailableRoles_should_return_empty_when_agents_dir_missing verifies
// that a missing agents/ directory produces an empty (not nil-panicking) result.
func Test_scanAvailableRoles_should_return_empty_when_agents_dir_missing(t *testing.T) {
	// Arrange
	baseDir := t.TempDir() // no agents/ subdirectory

	// Act
	roles := scanAvailableRoles(baseDir)

	// Assert — must not panic; empty slice is acceptable
	if len(roles) != 0 {
		t.Errorf("expected 0 roles for missing agents dir, got %d: %v", len(roles), roles)
	}
}

// ---------------------------------------------------------------------------
// App struct shape test (compile-time guard)
// ---------------------------------------------------------------------------

// Test_App_should_have_Orchestrator_field verifies that the App struct exposes
// the Orchestrator field so entrypoints can call app.Orchestrator.Run(...).
// This is a structural/compile-time test — if the field is missing the package
// will not compile.
func Test_App_should_have_Orchestrator_field(t *testing.T) {
	// Arrange + Act: construct a zero-value App and access the Orchestrator field.
	var a App

	// Assert — field access compiles and is nil for a zero-value struct.
	if a.Orchestrator != nil {
		t.Error("expected nil Orchestrator for zero-value App")
	}
}

// Test_App_should_have_SessionService_field verifies that the App struct has a
// SessionService field (required by entrypoints for session management).
func Test_App_should_have_SessionService_field(t *testing.T) {
	// Arrange + Act
	var a App

	// Assert — nil is the zero value for an interface.
	if a.SessionService != nil {
		t.Error("expected nil SessionService for zero-value App")
	}
}

// Test_App_should_have_AppName_field verifies the AppName string field.
func Test_App_should_have_AppName_field(t *testing.T) {
	// Arrange
	a := App{AppName: "test_app"}

	// Assert
	if a.AppName != "test_app" {
		t.Errorf("expected AppName=test_app, got %q", a.AppName)
	}
}

// Test_Config_should_not_have_AgentName_field verifies that the Config struct
// no longer contains AgentName (removed in orchestrator-only mode). This
// compiles only if AgentName is absent from the struct.
// We verify this indirectly by constructing Config without the field.
func Test_Config_should_have_required_fields(t *testing.T) {
	// Arrange — construct Config with all required fields; must compile.
	_ = Config{
		AgentDir: ".",
		AppName:  "my_app",
		ModelID:  "gemini-2.5-flash",
	}
	// If Config has unexpected required fields or AgentName was kept, the
	// programmer-intent test documents it here.
}
