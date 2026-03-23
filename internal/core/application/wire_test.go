// Package application white-box tests for internal helpers.
package application

import (
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
