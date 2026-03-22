# ADR-0002: MemoryLayout as an Immutable Value Object with Unexported Fields

**Status:** Superseded by ADR-0004
**Date:** 2026-03-22
**Authors:** Jensen (J-Team Architect)

## Context

`MemoryLayout` partitions the LLM context window into four named segments
(PINNED, SUMMARY, ACTIVE, BUFFER). It is computed once from a `ModelProfile`
and a `LayoutConfig`, and its four token limits drive every subsequent
token-budget decision in `MemoryPlugin`.

Two design questions arose during architecture review of S1:

1. Should `MemoryLayout` be a Value Object in the Domain Layer, or should it
   carry identity and live in the Application Layer?
2. Should its segment limits be exported struct fields or accessed via methods?

## Problem

Choosing the wrong object model here has knock-on effects: a mutable layout
could be silently corrupted by any caller holding a reference; exported fields
allow mutation from outside the package, breaking the invariant that limits
always sum to `context_window_tokens`.

## Options Considered

### Option A: Mutable struct with exported fields

- **Pros:** Minimal boilerplate; field access is direct.
- **Cons:** Any caller can write `layout.Active = 0`, silently breaking the
  sum invariant. No Go compiler enforcement.
- **Effort:** Low

### Option B: Immutable struct with exported fields (copy-by-value defense)

- **Pros:** Passing by value means callers get a copy; mutations do not
  propagate back to the owner.
- **Cons:** If the struct is ever passed by pointer (e.g. stored in a plugin
  struct), the defense evaporates. The invariant is not enforced by the type
  system.
- **Effort:** Low

### Option C: Immutable struct with unexported fields and accessor methods

- **Pros:** The compiler enforces immutability — no code outside
  `internal/memory` can write to the fields. Invariants (sum == N, all > 0)
  are guaranteed to hold after construction. Accessor methods form a clean,
  intentional API surface.
- **Cons:** Four small accessor methods to write. Slightly more verbose at
  call sites (`layout.Active()` vs `layout.Active`).
- **Effort:** Low

## Decision

We will use **Option C**: unexported fields, accessor methods.

`MemoryLayout` is a Domain Layer Value Object. It has no identity — two
layouts computed from the same inputs are equal in every meaningful sense.
It must be immutable after `NewLayout` returns so that the token-budget
invariants it encodes cannot be violated by any caller.

Concretely:

```go
// internal/memory/layout.go

type MemoryLayout struct {
    pinned  int
    summary int
    active  int
    buffer  int
}

func (l MemoryLayout) Pinned() int  { return l.pinned }
func (l MemoryLayout) Summary() int { return l.summary }
func (l MemoryLayout) Active() int  { return l.active }
func (l MemoryLayout) Buffer() int  { return l.buffer }
func (l MemoryLayout) Total() int   { return l.pinned + l.summary + l.active + l.buffer }
```

`NewLayout` returns `(MemoryLayout, error)` — a value, not a pointer. Callers
receive a copy. Because fields are unexported, there is no way to mutate the
copy or the original.

## Consequences

### Positive

- Invariants (sum == context_window_tokens, all segments >= MinSegmentTokens)
  are structurally guaranteed — no defensive checks needed in callers.
- The API surface is explicit: only the four segment limits and `Total()` are
  visible outside the package.
- Value semantics make the struct safe to embed in other structs or pass across
  goroutine boundaries without synchronization.

### Negative

- Call sites use method syntax (`layout.Active()`) rather than field syntax.
  This is idiomatic Go for encapsulated value objects; no practical downside.

### Risks

- None identified. The struct is small and its usage is confined to
  `internal/memory`.
