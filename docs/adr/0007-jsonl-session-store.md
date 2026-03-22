# ADR 0007 — JSONL File-Based Session Store

**Status:** Accepted
**Date:** 2026-03-22
**Deciders:** Jensen (Architecture)

---

## Context

All three entrypoints (`cmd/agent`, `cmd/web`, `cmd/telegram`) currently wire
`session.InMemoryService()` from `google.golang.org/adk@v0.6.0/session`.
Sessions are discarded when the process exits. This prevents:

- Resuming a conversation after a crash or redeploy
- Telegram per-chat continuity (card 06 depends on this)
- The GFS layered memory strategy (card 90)

The project needs a durable session backend that is a drop-in replacement for
`InMemoryService()` — i.e., it implements `session.Service` exactly — without
introducing a networked database dependency in the MVP.

---

## Decision

Implement `internal/sessionstore.JSONLService` — a `session.Service` backed by
one `.jsonl` file per session under `data/sessions/<session-id>.jsonl`.

Each line of the file is a JSON-serialised `session.Event` (the full struct,
not a simplified schema). On `AppendEvent` the line is appended with `O_APPEND`
and `os.O_WRONLY|os.O_CREATE|os.O_APPEND`. On `Get`, all lines are replayed in
order to reconstruct the event list; `StateDelta` fields drive state replay.

**Package location:** `internal/sessionstore/`
(distinct from `internal/memory/`, which handles context-window compression)

---

## Consequences

### Positive

- Zero new runtime dependencies (stdlib `os`, `encoding/json`, `log/slog`)
- Append-only writes — `O_APPEND` on POSIX is atomic for writes ≤ PIPE_BUF
  (~4 KB); a single serialised `Event` line is well within that limit for
  normal conversations, so no mutex is needed at the file layer for
  `AppendEvent`
- Human-readable and debuggable with `jq`
- Corrupt last-line tolerance: if `json.Unmarshal` fails on the final line
  (process crash mid-write), it is skipped and logged with `slog.Warn`
- Exact `session.Service` interface compliance — swap in one line per
  entrypoint

### Negative / Trade-offs

- No cross-process `List` atomicity (listing reads the directory, not an index)
- No automatic GC — stale session files accumulate; acceptable for MVP
- State reconstruction is O(n events) per `Get`; fine for conversational
  session lengths but would need an index for very long sessions
- Serialising `session.Event` (which embeds `model.LLMResponse`) requires
  verifying that all fields are JSON-compatible; the ADK database package does
  the same, so this is a solved problem

### Not decided here

- Cross-process session sharing (out of scope; single-process deployments only)
- Caching layer (explicitly deferred per MVP constraint)

---

## Alternatives Considered

| Option | Reason rejected |
|---|---|
| SQLite via GORM (ADK `session/database`) | Adds CGO/GORM dependency; heavier than needed for MVP |
| BoltDB / bbolt | Another binary dependency; overkill for append-only event log |
| One JSON file per session (read-modify-write) | Requires full file rewrite on every `AppendEvent`; not safe for concurrent writers |
| Single global JSONL (all sessions) | Complicates `Get` (scan entire file); harder to delete one session |

---

## Implementation Notes

### Concrete Session Type

`JSONLService` cannot return `*session.session` (unexported). Like the ADK
`session/database` package, it defines a local `jsonlSession` struct that
implements `session.Session` (ID, AppName, UserID, State, Events,
LastUpdateTime).

### State Replay on `Get`

`temp:` keys are stripped by `AppendEvent` before writing (matching ADK
contract). On replay, each line's `StateDelta` is split by
`sessionutils.ExtractStateDeltas` into `app:`, `user:`, and session-scoped
deltas. App and user deltas are accumulated per-service; session deltas are
merged into the session's state map. At `Get` return time,
`sessionutils.MergeStates` re-adds the `app:` and `user:` prefixes.

### Metadata Sidecar

A `<session-id>.meta.json` sidecar records `AppName`, `UserID`, `SessionID`,
and `CreatedAt`. This allows `List` to enumerate sessions by `AppName`/`UserID`
without replaying all event lines, and enables `Get` to validate the session
identity before reading the `.jsonl` file.

### Concurrency

`AppendEvent` opens the file in `O_APPEND` mode on each call (no persistent
file handle), so concurrent calls are safe at the OS level for
PIPE_BUF-bounded writes. `Create`, `Delete`, and `Get` are not
write-concurrent by design (single-process deployment assumption).

### `GetRequest` Filters

`NumRecentEvents` and `After` filters are applied in-memory after full file
replay, matching the behaviour of `InMemoryService`.
