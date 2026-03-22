// Package shelltool_test tests the shell tool handler.
package shelltool_test

import (
	"context"
	"strings"
	"testing"

	"github.com/neokn/agenticsystem/internal/shelltool"
)

// Test that Handler returns stdout and exit code 0 for a successful command.
func Test_Handler_should_return_stdout_and_zero_exit_code_when_command_succeeds(t *testing.T) {
	// Arrange
	ctx := context.Background()
	input := shelltool.ShellInput{Command: "echo hello"}

	// Act
	output, err := shelltool.Handler(ctx, input)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", output.ExitCode)
	}
	if !strings.Contains(output.Stdout, "hello") {
		t.Errorf("expected stdout to contain 'hello', got %q", output.Stdout)
	}
}

// Test that Handler returns non-zero exit code for a failing command.
func Test_Handler_should_return_nonzero_exit_code_when_command_fails(t *testing.T) {
	// Arrange
	ctx := context.Background()
	input := shelltool.ShellInput{Command: "exit 1"}

	// Act
	output, err := shelltool.Handler(ctx, input)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.ExitCode == 0 {
		t.Error("expected non-zero exit code, got 0")
	}
}

// Test that Handler truncates output exceeding 8192 bytes.
func Test_Handler_should_truncate_output_when_it_exceeds_8192_bytes(t *testing.T) {
	// Arrange
	ctx := context.Background()
	// Generate more than 8192 bytes of output using base64-encoded random bytes.
	input := shelltool.ShellInput{Command: "head -c 10000 /dev/urandom | base64"}

	// Act
	output, err := shelltool.Handler(ctx, input)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(output.Stdout) > 8192+len(" [output truncated]") {
		t.Errorf("expected truncated output, got %d bytes", len(output.Stdout))
	}
	if !strings.HasSuffix(output.Stdout, " [output truncated]") {
		t.Errorf("expected output to end with ' [output truncated]', got suffix %q",
			output.Stdout[max(0, len(output.Stdout)-30):])
	}
}

// Test that Handler respects context cancellation.
func Test_Handler_should_set_error_field_when_context_is_cancelled(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately to simulate timeout
	input := shelltool.ShellInput{Command: "sleep 10"}

	// Act
	output, err := shelltool.Handler(ctx, input)

	// Assert — either an error is returned or the Error field is populated.
	if err == nil && output.Error == "" {
		t.Error("expected error or Error field when context is cancelled")
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
