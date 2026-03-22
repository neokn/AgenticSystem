# ADR-0001: Error Type Conventions for internal/memory Package

**Status:** Accepted
**Date:** 2026-03-22
**Authors:** Jensen (J-Team Architect)

## Context

The `internal/memory` package implements a Context Window Memory Manager across six
stories (S0–S6). Each story introduces error conditions that callers (primarily
`MemoryPlugin` and test code) will need to handle or detect. Go gives three main
options: sentinel errors, custom error types, and plain `fmt.Errorf` strings.

Establishing a convention now — before any story is implemented — avoids inconsistency
across the package and costly refactors once callers are written.

## Problem

Which error strategy should the `internal/memory` package use, and where does each
strategy apply?

## Options Considered

### Option A: All plain `fmt.Errorf` strings

- **Pros:** Zero boilerplate, easy to write.
- **Cons:** Callers cannot detect specific conditions with `errors.Is()` — they would
  have to match on error message strings, which is fragile and untestable.
- **Effort:** Low

### Option B: Custom error types for every error

- **Pros:** Full type information; callers can inspect fields (e.g. which model ID was
  not found).
- **Cons:** High boilerplate for errors that callers never need to distinguish
  programmatically (e.g. a validation message like "ModelID is empty").
- **Effort:** High

### Option C: Sentinel errors for detectable conditions, fmt.Errorf for the rest

- **Pros:** Pragmatic. Sentinel errors only where callers need `errors.Is()`.
  Plain `fmt.Errorf` with field context for validation failures that are programming
  mistakes (fail-fast at init time, never inspected at runtime).
- **Cons:** Requires per-error judgment; slightly inconsistent surface.
- **Effort:** Low–Medium

## Decision

We will use **Option C**.

Specifically:

| Condition | Error strategy | Rationale |
|-----------|---------------|-----------|
| Unknown model ID in `GetProfile` | Sentinel `var ErrModelNotFound = errors.New("model not found")` | Callers may want to branch on this at runtime (e.g. fall back to a default). |
| OOM state signalled from `MemoryPlugin` | Named type `OOMEvent` (struct with context fields) | Callers must distinguish this from ordinary errors and may need to inspect the payload. |
| Validation failures in `NewRegistry` / `NewMemoryLayout` | `fmt.Errorf("NewRegistry: ModelID is required for profile at index %d", i)` | These are programming mistakes caught at startup; no runtime branching needed. |
| All other domain errors (threshold, strategy errors) | `fmt.Errorf` with descriptive message | Same rationale — fail fast, no programmatic detection needed. |

## Consequences

### Positive
- Callers can use `errors.Is(err, memory.ErrModelNotFound)` for clean conditional logic.
- Validation errors carry field-level context in the message without type overhead.
- `OOMEvent` carries structured payload (token counts, suggested action) useful for
  telemetry and user-facing messaging.

### Negative
- Two different error styles coexist in the package — developers must consult this ADR
  to understand when to use each.

### Risks
- If future stories introduce new "detectable" conditions, they must be reviewed against
  this convention and added to the table above. Risk is low — the package is small and
  the maintainer group is the J-Team.
