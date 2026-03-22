# ADR-0004: Remove MemoryLayout Segment Partitioning

**Status:** Accepted
**Date:** 2026-03-22
**Supersedes:** ADR-0002 (MemoryLayout as Immutable Value Object)

## Context

ADR-0002 introduced `MemoryLayout` as an immutable value object that partitions the
context window into four named segments: PINNED (15%), SUMMARY (25%), ACTIVE (50%),
BUFFER (10%). The design drew an analogy to OS memory segmentation.

After implementing the fork-based compression model and SubSession tracking, an
audit revealed that `MemoryLayout` was **computed but never consumed**:

| Segment | Intended Use | Actual Use |
|---------|-------------|------------|
| PINNED | Cap system prompt tokens | Never read — PINNED protection is structural (ADK separates `SystemInstruction` from `Contents`) |
| SUMMARY | Cap compressed summary tokens | Never read — summary size is determined by the LLM, not by a token budget |
| ACTIVE | Cap recent conversation turns | Never read — no turn-eviction logic references this value |
| BUFFER | Ensure space for model output | Only segment with a runtime effect (auto-raise to `MaxOutputTokens`) |

The fork-based compression (introduced alongside SubSession) further decoupled
compression from segment budgets: the fork inherits the agent's `SystemInstruction`
as a structurally isolated field, making PINNED protection implicit rather than
budget-based.

## Problem

Keeping `MemoryLayout` introduces:

1. **Dead code** — four ratio fields, a config file section, a constructor with
   proportional-deduction logic, five accessor methods, and ~300 lines of tests,
   none of which affect runtime behavior.
2. **False confidence** — the existence of segment ratios implies they are enforced,
   when in fact compression ignores them entirely.
3. **Configuration burden** — callers must supply a `LayoutConfig` and call
   `NewLayout` for values that are never read.

## Decision

Remove `MemoryLayout`, `LayoutConfig`, `NewLayout`, and `DefaultLayoutConfig`
entirely. The BUFFER >= `MaxOutputTokens` invariant, the only segment rule with
a runtime effect, is already enforced by the compression threshold and OOM handler
logic (which operate on the full `ContextWindowTokens` from `ModelProfile`).

PINNED content (system prompt, tool schema) is protected by **structural isolation**:
ADK places it in `req.Config.SystemInstruction`, separate from `req.Contents`.
The fork-based compression model reinforces this — the `ForkRequest` carries
`SystemInstruction` as a distinct field that is never included in compression
candidates.

## Consequences

### Positive

- ~300 lines of dead code and tests removed.
- `NewMemoryPlugin` signature simplified — no longer requires a `MemoryLayout` parameter.
- `configs/default.json` no longer carries unused `layout` ratios.
- No false promises — if segment budgets are not enforced, they should not exist.

### Negative

- If future work requires segment-aware compression (e.g. capping summary tokens),
  the segment model will need to be reintroduced. This ADR documents the rationale
  so that decision can be made with full context.

### Migration

- ADR-0002 is superseded; its immutability pattern was sound but the data it
  protected turned out to be unused.
- `MemoryPlugin` constructor changed from
  `NewMemoryPlugin(client, layout, strategy, profile, threshold)` to
  `NewMemoryPlugin(client, strategy, profile, threshold)`.
