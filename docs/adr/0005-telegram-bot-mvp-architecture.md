# ADR-0005: Telegram Bot MVP — New Driving Adapter in cmd/telegram

**Status:** Accepted
**Date:** 2026-03-22
**Authors:** Jensen (J-Team Architect)

## Context

AgenticSystem already has two delivery mechanisms: a CLI (`cmd/agent/main.go`) and a
Web UI (`cmd/web/main.go`). Both are thin driving adapters — they wire the
`internal/memory` plugin, `internal/agentdef` loader, and Google ADK components,
then hand off to the respective transport (stdin/stdout or HTTP).

The project needs a third delivery mechanism: a Telegram Bot that lets users
converse with the ADK-backed agent via the Telegram messaging platform.

The MVP constraints are intentionally minimal:
- Long Polling only (no public HTTPS endpoint required)
- No session persistence — each Telegram message creates a new ADK session
- MemoryPlugin is wired in but will rarely trigger compression in single-turn sessions
- No graceful shutdown for MVP

## Problem

Where does the Telegram transport code live, and how does it relate to the
existing Application Core (agent wiring, memory plugin)?

## Options Considered

### Option A: New cmd/telegram/main.go driving adapter (thin, mirrors cmd/agent pattern)

The Telegram Bot is a new top-level entrypoint. It reuses the exact same
application assembly pattern as `cmd/agent/main.go` — load agent def, wire memory
plugin, create ADK runner — then starts a Long Polling loop instead of reading
from stdin.

- **Pros:** Consistent with existing pattern; no new internal packages needed for
  MVP; Telegram library is an implementation detail contained in one file;
  zero changes to Application Core.
- **Cons:** Some wiring code is duplicated across cmd/ entrypoints (acceptable
  until a shared factory is warranted).
- **Effort:** Low

### Option B: Shared internal/appwire package for agent assembly

Extract the common ADK wiring (model profile, memory plugin, runner creation)
into a new `internal/appwire` package shared by all cmd/ entrypoints.

- **Pros:** Eliminates duplication across cmd/ entrypoints.
- **Cons:** Premature generalization — three entrypoints does not yet justify a
  shared factory; adds complexity before the duplication is painful; scope creep
  for MVP.
- **Effort:** Medium

### Option C: Add Telegram handling inside cmd/web/main.go

Run Long Polling in a goroutine alongside the existing Web UI launcher.

- **Pros:** Single binary.
- **Cons:** Violates Single Responsibility; conflates two completely different
  delivery mechanisms in one entrypoint; harder to deploy independently.
- **Effort:** Low (but creates architectural debt)

## Decision

We will use **Option A**: a new `cmd/telegram/main.go` driving adapter that
mirrors the established `cmd/agent` pattern.

This is a pure Driving Adapter decision. The Telegram Bot is a UI/transport
concern — it belongs at the outermost layer (Infrastructure / Driving Adapter),
calling inward into the ADK Application Core. The Application Core (memory plugin,
agent definition, runner) is unchanged.

The correct choice for a third Telegram message handler library is
`github.com/go-telegram-bot-api/telegram-bot-api/v5` — it is the most widely
adopted Go Telegram Bot library, has stable Long Polling support, and requires
minimal setup. No abstraction layer over it is needed at MVP scale.

Once three or more cmd/ entrypoints share identical wiring code, introduce an
`internal/appwire` package (Option B) as a follow-up, not now.

## Consequences

### Positive
- Application Core (internal/memory, internal/agentdef, ADK wiring) is untouched.
- Telegram library is an isolated infrastructure detail; swapping it later
  requires changing only `cmd/telegram/main.go`.
- MVP is delivered with minimal scope.
- Each cmd/ entrypoint is independently deployable.

### Negative
- ADK runner assembly code is repeated across cmd/agent, cmd/web, and cmd/telegram.
  This will become a maintenance concern if a fourth entrypoint is added, or if
  the assembly logic grows more complex. Accepted trade-off for MVP.
- No graceful shutdown — Long Polling goroutine does not handle SIGTERM. Acceptable
  for MVP; add in a follow-up card.
