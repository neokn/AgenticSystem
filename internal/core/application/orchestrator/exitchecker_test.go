package orchestrator

import (
	"testing"
)

// TestNewExitChecker_ReturnsNonNilAgent verifies that the constructor produces
// a valid, non-nil agent.Agent.
func TestNewExitChecker_ReturnsNonNilAgent(t *testing.T) {
	// Arrange
	cfg := ExitCheckConfig{
		OutputKey: "review_result",
		Pattern:   "APPROVED",
	}

	// Act
	a := NewExitChecker("exit_checker_0", cfg)

	// Assert
	if a == nil {
		t.Fatal("expected non-nil agent, got nil")
	}
	if a.Name() != "exit_checker_0" {
		t.Errorf("expected name %q, got %q", "exit_checker_0", a.Name())
	}
}

// TestExitCheckShouldEscalate_Match verifies that a value containing the
// pattern returns true.
func TestExitCheckShouldEscalate_Match(t *testing.T) {
	// Arrange
	val := "The result is APPROVED"
	pattern := "APPROVED"

	// Act
	result := exitCheckShouldEscalate(val, pattern)

	// Assert
	if !result {
		t.Errorf("expected true when val %q contains pattern %q", val, pattern)
	}
}

// TestExitCheckShouldEscalate_NoMatch verifies that a value not containing
// the pattern returns false.
func TestExitCheckShouldEscalate_NoMatch(t *testing.T) {
	// Arrange
	val := "Still needs work."
	pattern := "APPROVED"

	// Act
	result := exitCheckShouldEscalate(val, pattern)

	// Assert
	if result {
		t.Errorf("expected false when val %q does not contain pattern %q", val, pattern)
	}
}

// TestExitCheckShouldEscalate_EmptyState verifies that an empty value returns false.
func TestExitCheckShouldEscalate_EmptyState(t *testing.T) {
	// Arrange
	val := ""
	pattern := "APPROVED"

	// Act
	result := exitCheckShouldEscalate(val, pattern)

	// Assert
	if result {
		t.Errorf("expected false for empty val with pattern %q", pattern)
	}
}

// TestExitCheckShouldEscalate_EmptyPattern verifies that an empty pattern returns false.
func TestExitCheckShouldEscalate_EmptyPattern(t *testing.T) {
	// Arrange
	val := "The result is APPROVED"
	pattern := ""

	// Act
	result := exitCheckShouldEscalate(val, pattern)

	// Assert
	if result {
		t.Errorf("expected false for val %q with empty pattern", val)
	}
}
