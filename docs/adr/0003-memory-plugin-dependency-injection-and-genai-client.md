# ADR-0003: MemoryPlugin Dependency Injection and genai.Client Access

**Status:** Accepted
**Date:** 2026-03-22
**Authors:** Jensen (J-Team Architect)

## Context

`MemoryPlugin` (card 40, `internal/memory/plugin.go`) implements two ADK callbacks:

- `BeforeModelCallback` — two-layer token estimation and compression trigger
- `AfterModelCallback` — stores `resp.UsageMetadata.TotalTokenCount`

The `BeforeModelCallback` needs to call the `countTokens` API (`genai.Client.Models.CountTokens`) when the offline estimate crosses the 80% threshold. Jules's question was: does the ADK v0.6 `plugin.Plugin` interface or `agent.CallbackContext` give the plugin access to the `genai.Client` already held by the main Runner? Or must the plugin accept a client reference in its constructor?

## Investigation

Inspecting `google.golang.org/adk@v0.5.1` (the closest available version to v0.6 in the module cache):

**`agent.CallbackContext`** (`agent/context.go`):
```go
type CallbackContext interface {
    ReadonlyContext
    Artifacts() Artifacts
    State() session.State
}
```
`ReadonlyContext` exposes: `UserContent`, `InvocationID`, `AgentName`, `ReadonlyState`, `UserID`, `AppName`, `SessionID`, `Branch`. No `genai.Client` accessor anywhere in the interface or its embedded types.

**`llmagent.BeforeModelCallback`** signature (`agent/llmagent/llmagent.go`):
```go
type BeforeModelCallback func(ctx agent.CallbackContext, llmRequest *model.LLMRequest) (*model.LLMResponse, error)
```
`model.LLMRequest` contains `Model string`, `Contents []*genai.Content`, `Config *genai.GenerateContentConfig`, `Tools map[string]any`. No client reference.

**`model.gemini.geminiModel`** (`model/gemini/gemini.go`):
The `geminiModel` struct holds a private `client *genai.Client` field. There is no public accessor and the `model.LLM` interface exposes only `Name()` and `GenerateContent()`. The client is inaccessible to plugin callbacks.

**`plugin.Plugin`** (`plugin/plugin.go`):
The `Plugin` struct is a callback-container pattern — it wraps function references and has no mechanism to receive or expose infrastructure dependencies like `genai.Client`.

**Conclusion:** The ADK plugin system intentionally decouples plugins from infrastructure clients. `CallbackContext`, `LLMRequest`, and `plugin.Plugin` provide no path to the `genai.Client`. The plugin **must** accept a `*genai.Client` in its constructor.

## Decision

`MemoryPlugin` will accept a `*genai.Client` as a constructor parameter and store it in the struct. The same `*genai.Client` that the caller creates for the main `gemini.NewModel(...)` call is passed to `NewMemoryPlugin(...)`.

```go
type MemoryPlugin struct {
    client    *genai.Client   // for countTokens API — injected at construction
    layout    MemoryLayout
    strategy  CompressStrategy
    profile   ModelProfile
    metrics   *MemoryMetrics

    mu                 sync.Mutex
    lastTotalTokens    int
    summaries          []string
    compressedUpToIndex int
    threshold          float64
}

func NewMemoryPlugin(
    client    *genai.Client,
    layout    MemoryLayout,
    strategy  CompressStrategy,
    profile   ModelProfile,
    threshold float64,
) (*MemoryPlugin, error) { ... }
```

The caller (typically `cmd/agent/main.go`) creates the `*genai.Client` once and passes it to both `gemini.NewModel` and `NewMemoryPlugin`. This is the standard Go DI pattern — explicit, testable, no global state.

## Options Considered

### Option A: Pass genai.Client via constructor (chosen)
- **Pros:** Explicit, testable (inject a mock client in unit tests), no hidden globals.
- **Cons:** Caller must manage one more dependency reference.
- **Effort:** Minimal — one extra parameter.

### Option B: Plugin creates its own genai.Client internally
- **Pros:** Self-contained.
- **Cons:** Requires `genai.ClientConfig` (API keys/credentials) to be passed in, which is worse DX. Creates a second client connection for the same API key. Harder to test.
- **Effort:** Same, but messier.

### Option C: Retrieve client through a custom interface injected at plugin registration
- **Pros:** More indirection — plugin could work with a swappable token counter.
- **Cons:** Overengineered for MVP. The spec explicitly targets Gemini only. The "port fits the core" principle applies — define a `TokenCounter` port only when a second implementation is needed.
- **Effort:** Higher, speculative generality.

## Consequences

### Positive
- `NewMemoryPlugin` constructor clearly documents all dependencies — no hidden state.
- Unit tests inject a nil or stub client, bypassing the network for the offline-estimate path.
- The token-counter path (`countTokens` API) can be integration-tested separately.
- One `genai.Client` connection for the whole process.

### Negative
- The `cmd/agent/main.go` wiring is slightly more verbose (pass client to two places).
- If a future story adds a non-Gemini provider, a `TokenCounter` port will need to be extracted (trivial refactor at that time — the decision is reversible).

### Testing Guidance
Since `*genai.Client` is a concrete struct (not an interface), unit tests that do not need to exercise the `countTokens` path can pass `nil`. `BeforeModelCallback` must guard against a nil client and short-circuit to "pass through" when `lastTotalTokens + offline_estimate < threshold` (so the nil client is never reached on the happy fast-path). For tests that exercise the API call path, use `genai.NewClient` with a test transport or an integration test tag.
