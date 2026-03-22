# ADR-0006: Shell Tool MVP — Register via ADK FunctionTool as Driven Adapter

**Status:** Accepted
**Date:** 2026-03-22
**Authors:** Jensen (J-Team Architect)

## Context

AgenticSystem's LLMAgent is constructed with `llmagent.New(llmagent.Config{...})`.
The `Config.Tools []tool.Tool` field accepts any value implementing the
`google.golang.org/adk/tool.Tool` interface. The ADK module (v0.6.0) ships
`google.golang.org/adk/tool/functiontool` — a generic constructor that wraps any
Go function in the `tool.Tool` interface and auto-infers a JSON Schema from the
handler's input struct type.

The MVP requirement is minimal: let the LLM call `exec.CommandContext` to run a
shell command, with a 30-second timeout and 8192-byte output truncation. No
command whitelist and no human approval gate are in scope.

## Problem

Where does the shell execution logic live, and how is it exposed to the LLM via
ADK?

## Options Considered

### Option A: internal/shelltool package + functiontool.New at wiring site

A new `internal/shelltool` package owns the pure execution logic (args struct,
handler func, truncation, timeout). The calling site (wherever the LLMAgent is
built) calls `functiontool.New` and injects the result into `llmagent.Config.Tools`.

- **Pros:** Execution logic is testable in isolation (pure Go, no ADK dependency);
  follows Ports-fit-Core principle — the handler signature is defined by what the
  Core needs, not by ADK internals; consistent with existing `internal/` pattern.
- **Cons:** Two files (package + wiring site) instead of one. Acceptable.
- **Effort:** Low

### Option B: Inline the handler directly in cmd/ entrypoint

Define the input struct and handler closure inside `cmd/agent/main.go` (or a new
`cmd/shellagent/`), call `functiontool.New` there, and wire directly.

- **Pros:** One file to change.
- **Cons:** Logic is not independently testable; mixes infrastructure wiring with
  business logic; cannot be reused by a future cmd/telegram or cmd/web entrypoint
  without duplication.
- **Effort:** Low (but creates debt immediately)

### Option C: Implement the full tool.Tool interface manually

Satisfy `tool.Tool`, `Declaration()`, and `Run()` manually without
`functiontool.New`.

- **Pros:** Full control over FunctionDeclaration shape.
- **Cons:** Significant boilerplate with zero benefit for an MVP; `functiontool`
  auto-infers JSON Schema correctly from struct tags; no reason to bypass it.
- **Effort:** High

## Decision

We will use **Option A**: a new `internal/shelltool` package that owns the handler
function, with `functiontool.New` called at the wiring site where the LLMAgent is
assembled.

Architectural classification:
- `internal/shelltool` is an **Infrastructure / Driven Adapter** — it adapts the
  OS process API (`os/exec`) to the port implied by the LLM's tool-call contract.
- The package lives in `internal/` because it is an infrastructure concern, not a
  domain concern.
- No new ADK dependency enters `internal/shelltool` — the handler signature is
  `func(tool.Context, ShellInput) (ShellOutput, error)` where `tool.Context` is the
  ADK context interface, which is acceptable since ADK is already the infrastructure
  framework for this project.
- `functiontool.New` is called at the LLMAgent construction site, mirroring how
  the existing `cmd/agent/main.go` wires the memory plugin.

The `functiontool.New` generic constructor infers the JSON Schema from struct field
names and JSON tags. The LLM sees a single parameter `command string` and receives
a response with `stdout string`, `exit_code int`, and optionally `error string`.

## Consequences

### Positive

- `internal/shelltool` is independently unit-testable with no agent/LLM dependency.
- Handler function is reusable across any future cmd/ entrypoint that wants shell
  capability (cmd/telegram, cmd/web).
- No change to Application Core (agent definition, memory plugin, runner).
- `RequireConfirmation: false` is explicit in the `functiontool.Config` — the MVP
  has no approval gate, and the field documents this clearly for future readers.

### Negative

- `os/exec` and `context.WithTimeout` are called in a package under `internal/`,
  which means any cmd/ entrypoint that imports it gains OS-level process execution
  capability. Acceptable for an agentic system; document the risk.

### Risks

- Shell injection: the LLM constructs the command string; there is no sanitization.
  This is an accepted MVP risk — documented here, to be addressed in a follow-up
  security card.
- Hanging processes beyond timeout: `exec.CommandContext` sends SIGKILL on timeout
  via `os/exec` default behaviour (Go 1.20+), so orphan processes are prevented.
