# ADR-0008: MCP JSON Config Loader and Toolset Wiring

**Status:** Accepted
**Date:** 2026-03-23
**Authors:** Jensen (J-Team Architect)

## Context

AgenticSystem needs to support MCP (Model Context Protocol) servers as tool
providers without hardcoding them in Go source. The ADK module (v0.6.0) ships
`google.golang.org/adk/tool/mcptoolset` which handles the full MCP-to-ADK tool
conversion internally. The `mcp.CommandTransport` in the MCP go-sdk takes an
`*exec.Cmd`, launching an MCP server subprocess over stdio.

The pattern established by ADR-0006 (shelltool) separates pure logic packages
(`internal/shelltool`) from the wiring site (`internal/appwire/wire.go`). The
same separation applies here.

## Problem

How should mcp.json config loading be structured, and where should the
`mcptoolset.Toolset` construction live?

## Options Considered

### Option A: internal/mcpconfig + wiring in appwire (mirror shelltool pattern)

A new `internal/mcpconfig` package owns the mcp.json schema (Go structs),
file reading, JSON parsing, and validation. `internal/appwire/wire.go` imports
`mcpconfig`, calls `mcpconfig.Load`, then constructs `exec.Cmd`,
`mcp.CommandTransport`, and `mcptoolset.New` per server entry.

- **Pros:** Config loading is testable in isolation with zero ADK/MCP SDK
  dependency; appwire remains the sole wiring site for all ADK/MCP references;
  consistent with the established shelltool pattern; independently deployable test
  surface.
- **Cons:** Two packages instead of one (acceptable, same as shelltool).
- **Effort:** Low

### Option B: Inline config loading and toolset construction in appwire

Define the JSON schema structs and `os.ReadFile` / `json.Unmarshal` directly
inside `wire.go` alongside the toolset construction.

- **Pros:** One file to change; no new package.
- **Cons:** Config loading logic is not independently testable; mixing
  infrastructure concerns (file I/O, parsing) with wiring logic; violates the
  separation demonstrated by agentdef and shelltool.
- **Effort:** Low (but creates debt)

### Option C: Full adapter package with mcptoolset construction inside

Move both config loading and toolset construction into `internal/mcpconfig`,
importing the MCP SDK and ADK mcptoolset package there.

- **Pros:** Single package for everything MCP-related.
- **Cons:** Breaks the Ports-fit-Core principle — config parsing should not
  depend on `mcptoolset`; makes `mcpconfig` untestable without the MCP
  subprocess; conflates two distinct concerns (schema/parsing vs. ADK wiring).
- **Effort:** Medium

## Decision

We will use **Option A**: a new `internal/mcpconfig` package for pure config
loading, with `mcptoolset` construction remaining in `internal/appwire/wire.go`.

### Architectural Classification

**internal/mcpconfig** — Infrastructure / Driven Adapter (config side)
- Layer: Infrastructure
- Component: Tool Infrastructure (same bounded context as shelltool)
- Imports: `encoding/json`, `fmt`, `os`, `path/filepath` only. No ADK, no MCP SDK.
- Responsibility: own the mcp.json schema (`MCPConfig`, `ServerConfig`),
  file reading, JSON parsing, and validation (non-empty `Command` field).
- Returns `nil, nil` when the file is absent — the "optional config" contract.
- Error messages wrap with the package name "mcpconfig" and the operation
  ("parse") to satisfy acceptance criteria on error message shape.

**internal/appwire/wire.go** — Application Layer (wiring/orchestration)
- Layer: Application Layer
- Responsibility: call `mcpconfig.Load`, iterate `ServerConfig` entries,
  construct `exec.Cmd` (merging `os.Environ()` with per-server `Env` map),
  wrap in `mcp.CommandTransport`, call `mcptoolset.New`, collect into
  `[]tool.Toolset`, pass to `llmagent.Config.Toolsets`.
- The existing shell tool remains in `llmagent.Config.Tools` (unchanged).
- All ADK and MCP SDK imports stay at this wiring site.

### No New Design Pattern Required

The implementation is a simple constructor call sequence — a loop over config
entries producing toolsets. A Factory or Builder pattern would add indirection
with no benefit: there is only one product type (`mcptoolset.Toolset`), one
construction path, and no variation axis. A plain `for range` loop is the
correct solution.

### Port/Adapter Boundary

There is no explicit Port interface required here. The `mcpconfig.Load` function
acts as a thin infrastructure function returning a plain value type
(`*MCPConfig`). The ADK `tool.Toolset` interface is the port — it is defined by
the ADK framework (our infrastructure layer), and `mcptoolset.Toolset` is the
adapter that implements it. This matches how `functiontool.New` wraps shelltool.

## Consequences

### Positive

- `internal/mcpconfig` is unit-testable with stdlib only — no subprocess, no
  MCP server, no ADK dependency.
- mcp.json is optional: missing file returns `(nil, nil)`; appwire continues
  normally with zero MCP toolsets.
- Shell tool and memory plugin are unaffected by this change.
- The pattern is immediately recognisable to anyone who has read ADR-0006.
- Integration test in `internal/appwire` can use `exec.Command` with a minimal
  Go MCP server binary to verify end-to-end construction.

### Negative

- Subprocess spawning for MCP servers happens at `appwire.New` call time. If an
  MCP server binary is missing, the error surfaces at startup, not at first tool
  call. This is acceptable and consistent with fail-fast startup semantics.

### Risks

- Env var inheritance: `os.Environ()` is called at wiring time. If the process
  environment changes between startup and tool invocation, the subprocess sees
  the startup snapshot. Acceptable for MVP; documented here for future readers.
- MCP server subprocess lifecycle: `mcp.CommandTransport` owns the subprocess.
  Cleanup on agent shutdown depends on the ADK runner's teardown behaviour.
  No explicit `cmd.Process.Kill` is needed in this story; accepted for MVP.
