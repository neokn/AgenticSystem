# ADR-0009: Explicit Architecture — Four-Layer Structure and Port Contracts

**Status:** Accepted
**Date:** 2026-03-23
**Authors:** Jensen (Architecture)

---

## Context

The project currently has six packages sitting flat in `internal/`:

| Package | Current path | Primary deps |
|---|---|---|
| `agentdef` | `internal/agentdef` | `github.com/google/dotprompt/go` |
| `appwire` | `internal/appwire` | `google.golang.org/adk`, `google.golang.org/genai`, MCP SDK, all other internal pkgs |
| `mcpconfig` | `internal/mcpconfig` | stdlib only |
| `memory` | `internal/memory` | `google.golang.org/adk`, `google.golang.org/genai` |
| `sessionstore` | `internal/sessionstore` | `google.golang.org/adk/session`, `google.golang.org/genai` |
| `shelltool` | `internal/shelltool` | `google.golang.org/adk/tool` |

Entrypoints under `cmd/` (agent CLI, telegram bot, web launcher) each import `internal/appwire` and — in the cases of telegram and web — also import `internal/sessionstore` directly.

The flat layout has no enforced layer boundaries. Any package can import any other. As the system grows toward multi-agent phases (Phase 1: Root + SubAgents, Phase 2: Workflow agents), this will produce import cycles and make component extraction impossible.

**Additional observation:** `internal/appwire/wire.go` currently contains a 70-line debug plugin defined inline as a literal `plugin.Config` with a `BeforeModelCallback`. This conflates assembly logic with an infrastructure concern (debug telemetry), making `appwire` harder to test and harder to replace.

**Additional observation:** `internal/agentdef/override.go` creates an ADK plugin (`InstructionOverridePlugin`). This means agentdef currently spans two responsibilities: loading agent definitions (infrastructure) and producing ADK plugin objects (infrastructure adapter, consumed by assembly). Both responsibilities belong in `infra/` — no split is needed.

---

## Problem

We need to define and enforce layer boundaries before any new components are added, so that:

1. The domain model (agent definitions, port contracts) cannot accidentally import infrastructure.
2. Assembly (`appwire`) can be tested without starting real external processes.
3. Each future bounded context (memory management, session persistence, tool provisioning, multi-agent orchestration) is placed in the correct layer from the start.
4. Dependency direction is machine-checkable in CI.

---

## Options Considered

### Option A: Keep flat internal/ layout, add naming conventions only

- **Pros:** Zero migration cost. Developers just follow a naming rule.
- **Cons:** Not enforceable. Rules without tools get ignored. Import cycles will appear as the system grows.
- **Effort:** Low

### Option B: Single-level grouping (internal/core, internal/infra)

- **Pros:** Simple two-bucket split. Clear infra vs non-infra.
- **Cons:** Loses the distinction between domain (pure business rules) and application (use-case orchestration). The Application Layer is the most important boundary — it is where ports are consumed and where future multi-agent workflow logic lives.
- **Effort:** Low

### Option C: Four-layer Explicit Architecture (Domain / Application / Infrastructure / Interface)

- **Pros:** Matches the Explicit Architecture / Hexagonal / Clean Architecture model. Enforces the dependency direction rule. Ports and adapters are structurally visible. Application Layer is explicitly separated, giving a clear home for multi-agent orchestration logic in Phases 1 and 2.
- **Cons:** Requires a one-time package migration (card 01b). Four layers feel heavyweight for a six-package codebase today — but the project roadmap makes this necessary before new components are added.
- **Effort:** Medium (one migration sprint, no behaviour changes)

---

## Decision

We will use **Option C — Four-layer Explicit Architecture**.

The four layers and their dependency rule:

```
cmd/ (Interface)
  └─ depends on ──► internal/app/  (Application Layer)
                        └─ depends on ──► internal/domain/  (Domain Layer — innermost)
                                              ▲
                                    implements ports
                        internal/infra/  (Infrastructure)
                              └─ depends on ──► internal/domain/  (inward only)
```

**Dependency direction rule (testable constraint):**
- `internal/domain/` MUST NOT import any path containing `internal/infra/`, `internal/app/`, `google.golang.org/adk`, `google.golang.org/genai`, or any MCP SDK package.
- `internal/app/` MAY import `internal/domain/`, `google.golang.org/adk/runner`, and `google.golang.org/adk/session` (framework orchestration interfaces), but MUST NOT import `internal/infra/` directly except through injected Port values.
- `internal/infra/` MAY import `internal/domain/` (to implement ports) and all external dependencies.
- `cmd/` MAY import `internal/app/appwire` and `internal/infra/sessionstore` (for session service construction), but MUST NOT import any other `internal/infra/` package directly.

---

## Target Directory Structure

```
internal/
  domain/
    ports.go           — AgentLoader and ToolProvider port interfaces
  app/
    appwire/           — assembly: wires domain ports, infra adapters, and ADK runner
  infra/
    agentdef/          — dotprompt loader; InstructionOverridePlugin adapter
    mcpconfig/         — stdlib-only config loader (file system = infrastructure)
    memory/            — token counting, compression strategy, MemoryPlugin
    sessionstore/      — JSONL-backed session.Service adapter
    shelltool/         — shell exec functiontool adapter
    debugplugin/       — debug LLM request dump plugin (extracted from appwire)
cmd/
  agent/               — CLI entrypoint (thin I/O layer)
  telegram/            — Telegram bot entrypoint
  web/                 — Web UI launcher entrypoint
```

---

## Current Package Dependency Map

```
cmd/agent
  → internal/appwire
  → internal/memory         (for MemoryMetrics — to be removed after migration)
  → google.golang.org/adk/session
  → google.golang.org/genai

cmd/telegram
  → internal/appwire
  → internal/sessionstore   (direct infra import — violates future rule; acceptable pre-migration)
  → google.golang.org/adk/session
  → google.golang.org/genai

cmd/web
  → internal/appwire
  → internal/sessionstore   (direct infra import — acceptable pre-migration)

internal/appwire
  → internal/agentdef
  → internal/mcpconfig
  → internal/memory
  → internal/shelltool
  → google.golang.org/adk  (agent, llmagent, model, plugin, runner, session, tool, functiontool, mcptoolset)
  → google.golang.org/genai
  → github.com/modelcontextprotocol/go-sdk/mcp

internal/agentdef
  → github.com/google/dotprompt/go
  → google.golang.org/adk   (agent, model, plugin, genai)

internal/mcpconfig
  → stdlib only

internal/memory
  → google.golang.org/adk   (agent, model, plugin)
  → google.golang.org/genai (including tokenizer)

internal/sessionstore
  → google.golang.org/adk/session
  → google.golang.org/genai

internal/shelltool
  → google.golang.org/adk/tool
```

**No cycles exist today.** The flat layout does not have import cycles — migration can proceed safely.

---

## Port Interface Definitions

Two port interfaces are needed. A third candidate (MemoryService) is deferred.

### AgentLoader

Abstracts dotprompt loading away from the Application Layer. The Application Layer knows only that it can load an agent definition by base directory and name; it does not know about dotprompt, the `agents/` directory convention, or frontmatter parsing.

```go
// AgentLoader loads an agent definition from the configured agent store.
type AgentLoader interface {
    Load(baseDir, name string) (*AgentDefinition, error)
}

// AgentDefinition is the data transfer object returned by AgentLoader.
type AgentDefinition struct {
    Name        string
    Instruction string
    ModelID     string
}
```

Note: `AgentDefinition` mirrors `agentdef.Definition` exactly. After migration, `agentdef.Definition` will embed or alias this type; the domain type is the authoritative definition.

### ToolProvider

Abstracts MCP subprocess launch away from the Application Layer. The Application Layer knows only that it can ask for tools from a config; it does not know about exec.Command, mcp.CommandTransport, or environment variable merging.

**Placement note:** `ToolProvider` references `google.golang.org/adk/tool` types in its return signature. Because `internal/domain/` must not import ADK packages, `ToolProvider` is defined in `internal/app/ports.go` (the Application Layer), not in `internal/domain/`. Its input type (`*domain.MCPConfig`) is still owned by the domain layer.

```go
// In internal/app/ports.go:
// ToolProvider constructs the tool set for an agent from an MCP configuration.
// Returns nil slices (not an error) when cfg is nil or has no servers.
type ToolProvider interface {
    Tools(cfg *domain.MCPConfig) ([]tool.Tool, []tool.Toolset, error)
}
```

The domain layer owns the config types (`domain.MCPConfig`, `domain.MCPServerConfig`). The infra adapter that implements `ToolProvider` imports `internal/infra/mcpconfig` and `mcptoolset` — both are infrastructure concerns hidden behind this port.

### SessionStore (deferred as port)

`google.golang.org/adk/session.Service` is already a Go interface. The Application Layer can use it directly without a wrapping port. No new `SessionStore` port interface is needed — `session.Service` IS the port.

### MemoryService (deferred)

The memory plugin is currently a concrete type used directly in `appwire`. Whether to introduce a `MemoryService` port will be decided in card 05 (multi-agent memory architecture). For now, `memory.MemoryPlugin` remains a concrete dependency of `appwire`. This is acceptable because `memory` will move to `internal/infra/memory/` and `appwire` is free to import infra packages during the assembly phase.

---

## Multi-Agent Readiness Assessment

### Phase 1: Root Agent + SubAgents

The four-layer structure supports Phase 1 naturally:
- Each sub-agent definition is loaded via `AgentLoader` — same port, different `name` argument.
- The Application Layer (`appwire`) assembles multiple agents and passes them as children to the root agent. ADK's `llmagent` supports sub-agent routing via the `SubAgents` field.
- No new ports or layers needed for Phase 1.

### Phase 2: Workflow Agents

Phase 2 will introduce orchestration logic (sequential/parallel agent pipelines) that belongs in the Application Layer, not in infrastructure. The `internal/app/` directory provides the correct home for this logic:
- A new `internal/app/workflow/` package can define workflow orchestration types.
- Workflow agents will consume the same `AgentLoader` and `ToolProvider` ports.
- No new infrastructure packages need to be added; the ports remain stable.

---

## Consequences

### Positive

- Dependency direction is enforced structurally (enforced by `go build` if imports are correct; verifiable by grep).
- `internal/domain/` becomes a clean, zero-external-dependency package — trivial to test, trivial to reason about.
- The debug plugin extraction makes `appwire` shorter and removes one side-effectful concern from assembly.
- Multi-agent orchestration (Phases 1 and 2) has a clear home in `internal/app/`.
- Future bounded contexts (e.g. a dedicated `memory` component, a `toolregistry` component) can be added as new packages under the correct layer without touching existing packages.

### Negative

- One migration sprint required (card 01b) to move packages and update ~20 import paths.
- `cmd/telegram` and `cmd/web` currently import `internal/sessionstore` directly. After migration, this becomes `internal/infra/sessionstore` — a minor path update, not a structural violation, since `cmd/` is the outermost layer and is permitted to construct infrastructure dependencies to inject into `appwire.Config`.

### Risks

- **Import path churn:** All existing import paths under `internal/` change. This is a one-time cost but requires coordinated `git mv` + sed across the repo. Mitigation: card 01b is a single PR; no concurrent feature work touches `internal/`.
- **ADK framework coupling:** `internal/app/appwire` will still import `google.golang.org/adk` packages (`runner`, `session`, `llmagent`). This is intentional — the Application Layer orchestrates the ADK framework. It is NOT a layer violation; it is an architectural choice to treat the ADK runner as an application-layer framework dependency.

---

## Testable Constraint (for CI)

After card 01b lands, the following grep MUST return zero matches:

```bash
# No file in internal/domain/ may import external packages
grep -r "google.golang.org\|github.com/google/dotprompt\|github.com/modelcontextprotocol" \
     internal/domain/ && exit 1 || exit 0
```

This can be added as a `go vet`-style check or a simple CI step.
