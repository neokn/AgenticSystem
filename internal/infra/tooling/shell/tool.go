// Package shell provides a shell command execution tool for the LLM agent.
// It implements the handler function wired into ADK via functiontool.New.
//
// Architecture: Infrastructure / Driven Adapter.
// The Handler function wraps os/exec and exposes it through the contract
// implied by ADK's FunctionTool mechanism. The core handler accepts a plain
// context.Context for testability; the ADK adapter (ToolHandlerFunc) wraps it
// with the tool.Context signature expected by functiontool.New.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"google.golang.org/adk/tool"
)

// MaxOutputBytes is the maximum number of output bytes returned from a command.
// Output exceeding this limit is truncated and the truncation marker is appended.
const MaxOutputBytes = 8192

// truncationMarker is appended after truncated output.
const truncationMarker = " [output truncated]"

// defaultTimeout is the execution timeout applied to every shell command.
const defaultTimeout = 30 * time.Second

// ShellInput is the input structure for the shell execution tool.
// The LLM provides a shell command string to execute.
type ShellInput struct {
	Command string `json:"command"`
}

// ShellOutput is the output structure returned by the shell execution tool.
type ShellOutput struct {
	Stdout   string `json:"stdout"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// Handler executes the shell command specified in input and returns the combined
// output along with the exit code. It enforces a 30-second timeout derived from
// the parent context and truncates output at 8192 bytes.
//
// This function accepts context.Context for testability. The ADK adapter
// ToolHandlerFunc wraps it with the tool.Context signature.
func Handler(ctx context.Context, input ShellInput) (ShellOutput, error) {
	// Derive a child context with the execution timeout.
	execCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", input.Command)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()

	stdout := buf.String()

	// Apply output truncation.
	if len(stdout) > MaxOutputBytes {
		stdout = stdout[:MaxOutputBytes] + truncationMarker
	}

	exitCode := 0
	errMsg := ""

	if runErr != nil {
		// Distinguish exit errors (non-zero exit code) from execution errors
		// (e.g., timeout, binary not found).
		var exitErr *exec.ExitError
		if ok := isExitError(runErr, &exitErr); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Context deadline exceeded or other execution error.
			errMsg = fmt.Sprintf("exec error: %v", runErr)
			exitCode = -1
		}
	}

	return ShellOutput{
		Stdout:   stdout,
		ExitCode: exitCode,
		Error:    errMsg,
	}, nil
}

// isExitError checks whether err is an *exec.ExitError and stores it.
func isExitError(err error, out **exec.ExitError) bool {
	var e *exec.ExitError
	if errors.As(err, &e) {
		*out = e
		return true
	}
	return false
}

// ToolHandlerFunc wraps Handler with the tool.Context signature required by
// functiontool.New[ShellInput, ShellOutput].
func ToolHandlerFunc(ctx tool.Context, input ShellInput) (ShellOutput, error) {
	return Handler(ctx, input)
}
