package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/genai"
	"google.golang.org/genai/tokenizer"
)

// defaultThreshold is the fraction of context_window_tokens at which the plugin
// triggers compression. Corresponds to 80% of the context window.
const defaultThreshold = 0.80

// tokenCounter is the interface for offline (local) token counting.
// It is satisfied by *tokenizer.LocalTokenizer and by test stubs.
type tokenCounter interface {
	CountTokens(contents []*genai.Content, config *genai.CountTokensConfig) (*genai.CountTokensResult, error)
}

// apiTokenCounter is the interface for the online countTokens API call.
// Extracted so unit tests can inject a stub without a real *genai.Client.
type apiTokenCounter interface {
	CountTokensAPI(ctx context.Context, modelID string, contents []*genai.Content) (int32, error)
}

// genaiAPICounter wraps *genai.Client to implement apiTokenCounter.
type genaiAPICounter struct {
	client *genai.Client
}

func (g *genaiAPICounter) CountTokensAPI(ctx context.Context, modelID string, contents []*genai.Content) (int32, error) {
	resp, err := g.client.Models.CountTokens(ctx, modelID, contents, nil)
	if err != nil {
		return 0, fmt.Errorf("countTokens API: %w", err)
	}
	return resp.TotalTokens, nil
}

// MemoryMetrics holds observability counters and gauges for a MemoryPlugin.
// GetSnapshot returns a copy; all fields are safe to read after the snapshot.
type MemoryMetrics struct {
	// LastTotalTokens is the most recently recorded TotalTokenCount from a model
	// response. Reflects the full conversation size as reported by the model API.
	LastTotalTokens int

	// UsageRatio is LastTotalTokens / context_window_tokens.
	UsageRatio float64

	// CountTokensAPICallCount counts how many times the precise countTokens API
	// was invoked (second layer of BeforeModelCallback).
	CountTokensAPICallCount int

	// CompressTriggerCount counts how many compression cycles have fired.
	CompressTriggerCount int

	// CompressReclaimedTokens records the tokens reclaimed per compression cycle
	// (OriginalTokens - CompressedTokens). One entry per cycle.
	CompressReclaimedTokens []int
}

// MemoryPlugin is the ADK plugin that tracks token usage and triggers compression
// when the context window approaches capacity. It implements a two-layer
// estimation strategy:
//
//  1. Offline (cheap): estimate from last known total + new message tokens + max output.
//  2. Online (precise): call the countTokens API when the offline estimate crosses
//     the threshold.
//
// Construct with NewMemoryPlugin — never use a zero-value MemoryPlugin.
// All mutable fields are protected by mu; do NOT hold mu during network calls.
type MemoryPlugin struct {
	// Immutable after construction.
	tc        tokenCounter
	ac        apiTokenCounter
	layout    MemoryLayout
	strategy  CompressStrategy
	profile   ModelProfile
	threshold float64

	// Mutable state — all reads and writes under mu.
	mu                  sync.Mutex
	lastTotalTokens     int
	summaries           []string
	compressedUpToIndex int
	metrics             MemoryMetrics
}

// NewMemoryPlugin constructs a MemoryPlugin using the real genai.Client for both
// the offline tokenizer and the countTokens API. Pass threshold=0 to use the
// default of 0.80 (80%).
//
// The same *genai.Client that is passed to gemini.NewModel should be passed here —
// one client, one connection. See ADR-0003.
func NewMemoryPlugin(
	client *genai.Client,
	layout MemoryLayout,
	strategy CompressStrategy,
	profile ModelProfile,
	threshold float64,
) (*MemoryPlugin, error) {
	if client == nil {
		return nil, fmt.Errorf("NewMemoryPlugin: client must not be nil")
	}
	// Build the local tokenizer from the model ID. This downloads the tokenizer
	// model on first use; subsequent calls are served from the in-process cache.
	tok, err := tokenizer.NewLocalTokenizer(profile.ModelID)
	if err != nil {
		return nil, fmt.Errorf("NewMemoryPlugin: creating local tokenizer for %q: %w", profile.ModelID, err)
	}
	return newMemoryPluginWithDeps(tok, &genaiAPICounter{client: client}, layout, strategy, profile, threshold)
}

// newMemoryPluginWithDeps is the internal constructor used by both NewMemoryPlugin
// and unit tests. It accepts interfaces for the tokenizer and API counter so
// that tests can inject stubs.
func newMemoryPluginWithDeps(
	tc tokenCounter,
	ac apiTokenCounter,
	layout MemoryLayout,
	strategy CompressStrategy,
	profile ModelProfile,
	threshold float64,
) (*MemoryPlugin, error) {
	if threshold == 0.0 {
		threshold = defaultThreshold
	}
	if threshold <= 0.0 || threshold >= 1.0 {
		return nil, fmt.Errorf("NewMemoryPlugin: threshold must be in (0, 1), got %v", threshold)
	}
	if strategy == nil {
		return nil, fmt.Errorf("NewMemoryPlugin: strategy must not be nil")
	}
	return &MemoryPlugin{
		tc:        tc,
		ac:        ac,
		layout:    layout,
		strategy:  strategy,
		profile:   profile,
		threshold: threshold,
	}, nil
}

// BuildPlugin creates and returns a plugin.Plugin configured with this
// MemoryPlugin's Before/AfterModel callbacks. Register the returned plugin
// with the ADK runner.
func (p *MemoryPlugin) BuildPlugin() *plugin.Plugin {
	pl, _ := plugin.New(plugin.Config{
		Name:                "memory_plugin",
		BeforeModelCallback: p.beforeModelCallback,
		AfterModelCallback:  p.afterModelCallback,
	})
	return pl
}

// GetSnapshot returns a point-in-time copy of the plugin's metrics. Safe to
// call from any goroutine.
func (p *MemoryPlugin) GetSnapshot() MemoryMetrics {
	p.mu.Lock()
	defer p.mu.Unlock()

	snap := p.metrics
	snap.LastTotalTokens = p.lastTotalTokens

	contextTokens := p.profile.ContextWindowTokens
	if contextTokens > 0 {
		snap.UsageRatio = float64(p.lastTotalTokens) / float64(contextTokens)
	}

	// Deep-copy the slice so callers cannot mutate internal state.
	if len(p.metrics.CompressReclaimedTokens) > 0 {
		snap.CompressReclaimedTokens = make([]int, len(p.metrics.CompressReclaimedTokens))
		copy(snap.CompressReclaimedTokens, p.metrics.CompressReclaimedTokens)
	}

	return snap
}

// ---------------------------------------------------------------------------
// AfterModelCallback
// ---------------------------------------------------------------------------

// afterModelCallback stores resp.UsageMetadata.TotalTokenCount as lastTotalTokens.
// Guard: only writes if the value is > 0 (preserves previous value on nil/zero metadata).
func (p *MemoryPlugin) afterModelCallback(_ agent.CallbackContext, resp *model.LLMResponse, _ error) (*model.LLMResponse, error) {
	if resp == nil || resp.UsageMetadata == nil {
		slog.Warn("memory_plugin: AfterModelCallback: nil or missing UsageMetadata; lastTotalTokens unchanged")
		return nil, nil
	}
	total := int(resp.UsageMetadata.TotalTokenCount)
	if total <= 0 {
		slog.Warn("memory_plugin: AfterModelCallback: TotalTokenCount is zero; lastTotalTokens unchanged",
			"reported_total", total,
		)
		return nil, nil
	}

	p.mu.Lock()
	p.lastTotalTokens = total
	p.mu.Unlock()

	return nil, nil
}

// ---------------------------------------------------------------------------
// estimatedTotal
// ---------------------------------------------------------------------------

// estimatedTotal computes the offline token estimate: last + msgTokens + maxOutput.
// It accepts lastTotal as a parameter (snapshot taken under mu by the caller) so
// that the mu is not held during any computation.
func (p *MemoryPlugin) estimatedTotal(lastTotal, msgTokens int) int {
	return lastTotal + msgTokens + p.profile.MaxOutputTokens
}

// ---------------------------------------------------------------------------
// BeforeModelCallback
// ---------------------------------------------------------------------------

// beforeModelCallback implements the two-layer token estimation and compression
// trigger. We use context.Background() for the network call because
// agent.CallbackContext does not expose a Go context.Context.
//
// Locking discipline (from ADR-0003 and architecture notes):
//  1. Lock mu → snapshot lastTotalTokens → unlock
//  2. Offline estimate (no lock held)
//  3. If above threshold: call countTokens API (no lock held — network I/O)
//  4. If compression needed: lock mu → update summaries/compressedUpToIndex → unlock
func (p *MemoryPlugin) beforeModelCallback(_ agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
	return p.runBeforeModel(context.Background(), req)
}

// runBeforeModel is the testable implementation of BeforeModelCallback that
// accepts an explicit context.Context so unit tests can pass context.Background().
func (p *MemoryPlugin) runBeforeModel(ctx context.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	if req == nil || len(req.Contents) == 0 {
		return nil, nil
	}

	// Extract the last user message for offline token counting.
	lastContent := req.Contents[len(req.Contents)-1]

	// --- Layer 1: offline estimate ---
	// Snapshot mutable state under lock, then release before any I/O.
	p.mu.Lock()
	lastTotal := p.lastTotalTokens
	p.mu.Unlock()

	// Count tokens for the new message using the local tokenizer.
	msgTokens := p.countMsgTokens([]*genai.Content{lastContent})

	estimated := p.estimatedTotal(lastTotal, msgTokens)
	thresholdTokens := int(p.threshold * float64(p.profile.ContextWindowTokens))

	if estimated < thresholdTokens {
		// Fast path: well below threshold, no API call needed.
		return nil, nil
	}

	// --- Layer 2: precise count via countTokens API ---
	// Do NOT hold mu across this network call.
	apiTotal, err := p.ac.CountTokensAPI(ctx, p.profile.ModelID, req.Contents)
	if err != nil {
		// Level 1 exception handling: propagate — the ADK runner will handle it.
		return nil, fmt.Errorf("memory_plugin: countTokens API: %w", err)
	}

	preciseTotal := int(apiTotal) + p.profile.MaxOutputTokens

	// Increment the API call counter under lock.
	p.mu.Lock()
	p.metrics.CountTokensAPICallCount++
	p.mu.Unlock()

	if preciseTotal < thresholdTokens {
		// False alarm: offline over-estimated, no compression needed.
		slog.Info("memory_plugin: countTokens false alarm",
			"estimated", estimated,
			"precise_total", preciseTotal,
			"threshold_tokens", thresholdTokens,
		)
		return nil, nil
	}

	// --- Compression trigger ---
	return p.triggerCompression(ctx, req)
}

// triggerCompression calls the CompressStrategy to select candidates and compress
// them, then rewrites req.Contents. All state updates are done under mu.
func (p *MemoryPlugin) triggerCompression(ctx context.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	// Build active turns from req.Contents (all except the last user message).
	activeTurns := contentsToTurns(req.Contents)
	targetReclaim := p.profile.MaxOutputTokens // conservative reclaim target

	// Read existingSummary without holding mu during the compress call.
	p.mu.Lock()
	var existingSummary string
	if len(p.summaries) > 0 {
		existingSummary = p.summaries[len(p.summaries)-1]
	}
	p.mu.Unlock()

	candidates := p.strategy.SelectCandidates(activeTurns, targetReclaim)
	if len(candidates) == 0 {
		// Nothing to compress (e.g. only one turn). Pass through.
		return nil, nil
	}

	result, err := p.strategy.Compress(ctx, candidates, existingSummary, p.profile)
	if err != nil {
		// Level 1: propagate — compression failure is non-fatal for the request
		// but worth surfacing so the caller can decide.
		return nil, fmt.Errorf("memory_plugin: compression failed: %w", err)
	}

	// Rewrite req.Contents: [system_prompt (if any) + summary turn + recent active turns + user_msg].
	req.Contents = buildCompressedContents(req, result.CompressedText, len(candidates))

	// Update plugin state under mu.
	reclaimedTokens := result.OriginalTokens - result.CompressedTokens
	p.mu.Lock()
	p.summaries = append(p.summaries, result.CompressedText)
	p.compressedUpToIndex += len(candidates)
	p.metrics.CompressTriggerCount++
	p.metrics.CompressReclaimedTokens = append(p.metrics.CompressReclaimedTokens, reclaimedTokens)
	p.mu.Unlock()

	slog.Info("memory_plugin: compression triggered",
		"strategy", p.strategy.Name(),
		"candidates", len(candidates),
		"reclaimed_tokens", reclaimedTokens,
		"summary_index", len(p.summaries),
	)

	return nil, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// countMsgTokens counts tokens for a slice of content using the offline tokenizer.
// Returns 0 on error (conservative: offline estimate may under-count, but the
// second layer API call will catch it).
func (p *MemoryPlugin) countMsgTokens(contents []*genai.Content) int {
	result, err := p.tc.CountTokens(contents, nil)
	if err != nil {
		slog.Warn("memory_plugin: offline CountTokens failed; using 0",
			"error", err,
		)
		return 0
	}
	return int(result.TotalTokens)
}

// contentsToTurns converts a slice of *genai.Content to []ConversationTurn.
// The last element (new user message) is included as the final turn.
func contentsToTurns(contents []*genai.Content) []ConversationTurn {
	turns := make([]ConversationTurn, 0, len(contents))
	for _, c := range contents {
		if c == nil {
			continue
		}
		text := contentText(c)
		turns = append(turns, ConversationTurn{
			Role:    c.Role,
			Content: text,
		})
	}
	return turns
}

// contentText extracts plain text from a *genai.Content by concatenating all
// text parts. Non-text parts are silently skipped.
func contentText(c *genai.Content) string {
	var s string
	for _, part := range c.Parts {
		if part != nil {
			s += part.Text
		}
	}
	return s
}

// buildCompressedContents rewrites req.Contents to:
//
//	[system_prompt (from Config if present) + summary content + recent active turns + user msg]
//
// compressedCount is the number of turns that were compressed; the remaining
// turns (after compressedCount, excluding the last user message) are "recent active".
func buildCompressedContents(req *model.LLMRequest, summaryText string, compressedCount int) []*genai.Content {
	contents := req.Contents

	// The last element is the new user message.
	userMsg := contents[len(contents)-1]
	prior := contents[:len(contents)-1]

	// Turns after the compressed candidates are "recent active".
	recentStart := compressedCount
	if recentStart > len(prior) {
		recentStart = len(prior)
	}
	recentActive := prior[recentStart:]

	var result []*genai.Content

	// Include system prompt if present in Config.
	if req.Config != nil && req.Config.SystemInstruction != nil {
		result = append(result, req.Config.SystemInstruction)
	}

	// Summary turn (model role, so it doesn't look like a user message).
	if summaryText != "" {
		result = append(result, &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{{Text: summaryText}},
		})
	}

	// Recent active turns.
	result = append(result, recentActive...)

	// New user message.
	result = append(result, userMsg)

	return result
}
