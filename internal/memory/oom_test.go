package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// ---------------------------------------------------------------------------
// Helpers for OOM tests
// ---------------------------------------------------------------------------

// newOOMTestPlugin creates a MemoryPlugin with a tiny context window (1000 tokens)
// configured for OOM handler tests.
// primaryThreshold: fraction at which primary compression fires (e.g. 0.80).
// emergencyThreshold: fraction at which OOM handler fires (e.g. 0.90).
func newOOMTestPlugin(
	t *testing.T,
	tc tokenCounter,
	ac apiTokenCounter,
	strategy CompressStrategy,
	primaryThreshold float64,
	emergencyThreshold float64,
) *MemoryPlugin {
	t.Helper()
	profile := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		ContextWindowTokens: 1_000,
		MaxOutputTokens:     10,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.10,
		SummaryRatio: 0.15,
		ActiveRatio:  0.65,
		BufferRatio:  0.10,
	}
	layout, err := NewLayout(profile, cfg)
	if err != nil {
		t.Fatalf("newOOMTestPlugin: NewLayout: %v", err)
	}
	if strategy == nil {
		strategy = &stubCompressStrategy{
			result: &CompressResult{CompressedText: "summary", OriginalTokens: 100, CompressedTokens: 20},
		}
	}
	p, err := newMemoryPluginWithDeps(tc, ac, layout, strategy, profile, primaryThreshold)
	if err != nil {
		t.Fatalf("newOOMTestPlugin: newMemoryPluginWithDeps: %v", err)
	}
	p.emergencyThreshold = emergencyThreshold
	return p
}

// makeMultiTurnRequest returns a request with multiple turns ending with a user message.
func makeMultiTurnRequest() *model.LLMRequest {
	return &model.LLMRequest{
		Model: "gemini-2.0-flash",
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "turn1"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "reply1"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "turn2"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "reply2"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "new message"}}},
		},
	}
}

// extractOOMWarning extracts the OOMWarningEvent from a LLMResponse's CustomMetadata.
// Returns the event and true if found, else zero value and false.
func extractOOMWarning(resp *model.LLMResponse) (OOMWarningEvent, bool) {
	if resp == nil || resp.CustomMetadata == nil {
		return OOMWarningEvent{}, false
	}
	val, ok := resp.CustomMetadata["oom_warning"]
	if !ok {
		return OOMWarningEvent{}, false
	}
	event, ok := val.(OOMWarningEvent)
	return event, ok
}

// ---------------------------------------------------------------------------
// Task 1: OOMWarningEvent struct
// ---------------------------------------------------------------------------

func TestOOMWarningEvent_HasRequiredFields(t *testing.T) {
	// Arrange & Act
	event := OOMWarningEvent{
		UsageRatio:     0.95,
		Recommendation: "start a new conversation",
		Reason:         "secondary compression ineffective",
	}

	// Assert
	if event.UsageRatio != 0.95 {
		t.Errorf("expected UsageRatio=0.95, got %v", event.UsageRatio)
	}
	if event.Recommendation != "start a new conversation" {
		t.Errorf("expected Recommendation='start a new conversation', got %q", event.Recommendation)
	}
	if event.Reason != "secondary compression ineffective" {
		t.Errorf("expected Reason='secondary compression ineffective', got %q", event.Reason)
	}
}

// ---------------------------------------------------------------------------
// Task 4: Emergency threshold is configurable via emergencyThreshold field
// ---------------------------------------------------------------------------

func TestOOMPlugin_DefaultEmergencyThreshold_Is90Percent(t *testing.T) {
	// Arrange
	tc := &stubTokenCounter{count: 10}
	ac := &stubAPICounter{count: 10}
	p := newTestPlugin(t, tc, ac, nil, 0.80)

	// Assert: default emergency threshold is 0.90
	if p.emergencyThreshold != defaultEmergencyThreshold {
		t.Errorf("expected emergencyThreshold=%v, got %v", defaultEmergencyThreshold, p.emergencyThreshold)
	}
}

// ---------------------------------------------------------------------------
// Task 2+3: Secondary compression + OOMWarning return
// ---------------------------------------------------------------------------

// AC1: After normal compression, if precise_total >= 90% and SUMMARY is non-empty,
// secondary compression (summary of summary) fires and precise_total drops below 90%.
func TestOOMHandler_TriggersSecondaryCompression_WhenAboveEmergencyThreshold(t *testing.T) {
	// Arrange:
	// contextWindow=1000, primaryThreshold=80%→800, emergencyThreshold=90%→900
	// First API call: 920 (>800) → triggers primary compression
	// Second API call: 920 (>=900) → OOM handler triggers secondary compression
	// Third API call: 850 (<900) → secondary compression succeeded, no OOMWarning
	ac := &seqAPICounter{responses: []int32{920, 920, 850}}
	tc := &stubTokenCounter{count: 50}

	strategy := &seqCompressStrategy{
		results: []*CompressResult{
			{CompressedText: "primary summary", OriginalTokens: 100, CompressedTokens: 50},
			{CompressedText: "shorter secondary summary", OriginalTokens: 50, CompressedTokens: 10, ActualCompressionRatio: 0.20},
		},
	}

	p := newOOMTestPlugin(t, tc, ac, strategy, 0.80, 0.90)
	// Seed existing summary so SUMMARY is non-empty.
	p.mu.Lock()
	p.summaries = []string{"existing summary from prior cycle"}
	p.lastTotalTokens = 800
	p.mu.Unlock()

	req := makeMultiTurnRequest()

	// Act
	result, err := p.runBeforeModel(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return nil (not OOMWarning) because secondary compression brought it below 90%.
	if result != nil {
		t.Errorf("expected nil LLMResponse (secondary compression succeeded), got non-nil")
	}
	// req.Contents must have been updated — non-empty after secondary compression.
	if len(req.Contents) == 0 {
		t.Fatalf("expected req.Contents to be non-empty after secondary compression, got empty")
	}
}

// AC2: After secondary compression, if precise_total still >= 90%,
// BeforeModelCallback returns non-nil LLMResponse with OOMWarningEvent in CustomMetadata.
func TestOOMHandler_ReturnsOOMWarning_WhenSecondaryCompressionInsufficientAfterPrimary(t *testing.T) {
	// Arrange:
	// All API responses above 90% threshold → OOMWarning is returned.
	ac := &seqAPICounter{responses: []int32{920, 950, 940}}
	tc := &stubTokenCounter{count: 50}

	strategy := &seqCompressStrategy{
		results: []*CompressResult{
			{CompressedText: "primary summary", OriginalTokens: 100, CompressedTokens: 80},
			{CompressedText: "secondary summary", OriginalTokens: 80, CompressedTokens: 70, ActualCompressionRatio: 0.875},
		},
	}

	p := newOOMTestPlugin(t, tc, ac, strategy, 0.80, 0.90)
	p.mu.Lock()
	p.summaries = []string{"existing summary"}
	p.lastTotalTokens = 800
	p.mu.Unlock()

	req := makeMultiTurnRequest()

	// Act
	result, err := p.runBeforeModel(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil LLMResponse containing OOMWarning, got nil")
	}
	event, ok := extractOOMWarning(result)
	if !ok {
		t.Fatalf("expected OOMWarningEvent in CustomMetadata[oom_warning], not found")
	}
	if event.UsageRatio <= 0 {
		t.Errorf("expected UsageRatio > 0, got %v", event.UsageRatio)
	}
	if event.Recommendation != "start a new conversation" {
		t.Errorf("expected Recommendation='start a new conversation', got %q", event.Recommendation)
	}
}

// AC3: emergency_threshold_pct is configurable (e.g. 85%).
func TestOOMHandler_UsesCustomEmergencyThreshold_WhenConfigured(t *testing.T) {
	// Arrange: emergencyThreshold=85% → 850 tokens for 1000-token window.
	// After primary compression, precise_total is 870 (>=85%) → OOM fires.
	// After secondary compression, still 870 (>=85%) → OOMWarning returned.
	ac := &seqAPICounter{responses: []int32{870, 870, 870}}
	tc := &stubTokenCounter{count: 50}

	strategy := &seqCompressStrategy{
		results: []*CompressResult{
			{CompressedText: "primary summary", OriginalTokens: 100, CompressedTokens: 80},
			{CompressedText: "secondary summary", OriginalTokens: 80, CompressedTokens: 75, ActualCompressionRatio: 0.9375},
		},
	}

	p := newOOMTestPlugin(t, tc, ac, strategy, 0.80, 0.85)
	p.mu.Lock()
	p.summaries = []string{"existing summary"}
	p.lastTotalTokens = 800
	p.mu.Unlock()

	req := makeMultiTurnRequest()

	// Act
	result, err := p.runBeforeModel(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With 85% custom threshold, OOMWarning should be returned.
	if result == nil {
		t.Fatal("expected non-nil LLMResponse (OOMWarning), because custom 85% threshold was crossed")
	}
	_, ok := extractOOMWarning(result)
	if !ok {
		t.Fatalf("expected OOMWarningEvent in response CustomMetadata, not found")
	}
}

// AC4: When SUMMARY segment is empty, skip secondary compression and return OOMWarning.
func TestOOMHandler_SkipsSecondaryCompression_WhenSummaryIsEmpty(t *testing.T) {
	// Arrange: Call handleOOM directly with empty p.summaries so the empty-SUMMARY
	// guard is exercised. Using runBeforeModel would populate p.summaries via primary
	// compression before handleOOM fires, making the guard unreachable.
	tc := &stubTokenCounter{count: 50}
	ac := &stubAPICounter{count: 0} // no API calls expected in this path
	strategy := &seqCompressStrategy{
		results: []*CompressResult{}, // no compress calls expected
	}

	p := newOOMTestPlugin(t, tc, ac, strategy, 0.80, 0.90)
	// Explicitly leave p.summaries empty — this is the scenario under test.

	req := makeMultiTurnRequest()

	// preciseTotalBeforeSecondary = 940 (above 90% of 1000-token window = 900)
	preciseTotalAboveThreshold := 940

	// Act — call handleOOM directly; must not panic.
	result, err := p.handleOOM(context.Background(), req, preciseTotalAboveThreshold)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error (must not panic or propagate error for empty SUMMARY): %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil LLMResponse (OOMWarning) when SUMMARY is empty, got nil")
	}
	event, ok := extractOOMWarning(result)
	if !ok {
		t.Fatalf("expected OOMWarningEvent in CustomMetadata, not found")
	}
	if event.Recommendation != "start a new conversation" {
		t.Errorf("expected Recommendation='start a new conversation', got %q", event.Recommendation)
	}
	// Verify the correct code path was taken — Reason must mention empty SUMMARY.
	const wantReasonSubstr = "SUMMARY segment empty"
	if !strings.Contains(event.Reason, wantReasonSubstr) {
		t.Errorf("expected event.Reason to contain %q (empty-SUMMARY guard), got %q", wantReasonSubstr, event.Reason)
	}
}

// AC5: When secondary compression yields < 5% token reduction, treat as
// maximally compressed and go directly to OOMWarning.
func TestOOMHandler_ReturnsOOMWarning_WhenSecondaryCompressionBelowMinReduction(t *testing.T) {
	// Arrange: secondary compression yields only 3% reduction (OriginalTokens=100, CompressedTokens=97).
	ac := &seqAPICounter{responses: []int32{920, 950}}
	tc := &stubTokenCounter{count: 50}

	strategy := &seqCompressStrategy{
		results: []*CompressResult{
			{CompressedText: "primary summary", OriginalTokens: 100, CompressedTokens: 50},
			{
				CompressedText:         "almost same summary",
				OriginalTokens:         100,
				CompressedTokens:       97,
				ActualCompressionRatio: 0.97, // 3% reduction — below 5% minimum
			},
		},
	}

	p := newOOMTestPlugin(t, tc, ac, strategy, 0.80, 0.90)
	p.mu.Lock()
	p.summaries = []string{"existing summary"}
	p.lastTotalTokens = 800
	p.mu.Unlock()

	req := makeMultiTurnRequest()

	// Act
	result, err := p.runBeforeModel(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil LLMResponse (OOMWarning) when secondary compression < 5% reduction")
	}
	event, ok := extractOOMWarning(result)
	if !ok {
		t.Fatalf("expected OOMWarningEvent, not found in CustomMetadata")
	}
	if event.Reason == "" {
		t.Error("expected non-empty Reason in OOMWarningEvent")
	}
}

// AC6: When secondary compression returns an error, fall back to OOMWarning
// (do not silently ignore), and increment oom_event_count.
func TestOOMHandler_ReturnsOOMWarning_WhenSecondaryCompressionFails(t *testing.T) {
	// Arrange: secondary compress call returns an error.
	ac := &seqAPICounter{responses: []int32{920, 950}}
	tc := &stubTokenCounter{count: 50}

	strategy := &seqCompressStrategy{
		results: []*CompressResult{
			{CompressedText: "primary summary", OriginalTokens: 100, CompressedTokens: 50},
		},
		secondaryErr: errors.New("compress worker unavailable"),
	}

	p := newOOMTestPlugin(t, tc, ac, strategy, 0.80, 0.90)
	p.mu.Lock()
	p.summaries = []string{"existing summary"}
	p.lastTotalTokens = 800
	p.mu.Unlock()

	req := makeMultiTurnRequest()

	// Act
	result, err := p.runBeforeModel(context.Background(), req)

	// Assert: must NOT propagate the compress error as a Go error.
	if err != nil {
		t.Fatalf("expected no Go error (compress error must fallback to OOMWarning), got: %v", err)
	}
	// Must return OOMWarning.
	if result == nil {
		t.Fatal("expected non-nil LLMResponse (OOMWarning after compress error), got nil")
	}
	event, ok := extractOOMWarning(result)
	if !ok {
		t.Fatalf("expected OOMWarningEvent in CustomMetadata, not found")
	}
	if event.Reason == "" {
		t.Error("expected non-empty Reason (should include error info)")
	}

	// oom_event_count must be incremented.
	snap := p.GetSnapshot()
	if snap.OOMEventCount != 1 {
		t.Errorf("expected OOMEventCount=1, got %d", snap.OOMEventCount)
	}
}

// Task 5: OOMWarning path always increments oom_event_count.
func TestOOMHandler_IncrementsOOMEventCount_WhenOOMWarningReturned(t *testing.T) {
	// Arrange: secondary compression is effective but precise_total remains above 90%.
	ac := &seqAPICounter{responses: []int32{920, 950, 940}}
	tc := &stubTokenCounter{count: 50}

	strategy := &seqCompressStrategy{
		results: []*CompressResult{
			{CompressedText: "primary summary", OriginalTokens: 100, CompressedTokens: 50},
			{CompressedText: "secondary summary", OriginalTokens: 80, CompressedTokens: 50, ActualCompressionRatio: 0.625},
		},
	}

	p := newOOMTestPlugin(t, tc, ac, strategy, 0.80, 0.90)
	p.mu.Lock()
	p.summaries = []string{"existing summary"}
	p.lastTotalTokens = 800
	p.mu.Unlock()

	req := makeMultiTurnRequest()

	// Act
	result, err := p.runBeforeModel(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected OOMWarning, got nil")
	}

	snap := p.GetSnapshot()
	if snap.OOMEventCount != 1 {
		t.Errorf("expected OOMEventCount=1, got %d", snap.OOMEventCount)
	}
}

// ---------------------------------------------------------------------------
// Stub helpers for OOM tests
// ---------------------------------------------------------------------------

// seqAPICounter returns pre-configured token counts in sequence for each API call.
type seqAPICounter struct {
	responses []int32
	mu        sync.Mutex
	callIdx   int
}

func (c *seqAPICounter) CountTokensAPI(_ context.Context, _ string, _ []*genai.Content) (int32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.responses) == 0 {
		return 0, nil
	}
	if c.callIdx >= len(c.responses) {
		return c.responses[len(c.responses)-1], nil
	}
	resp := c.responses[c.callIdx]
	c.callIdx++
	return resp, nil
}

// seqCompressStrategy returns results in sequence: results[0] for first Compress call,
// results[1] for second. If secondaryErr is set, the second call returns that error.
type seqCompressStrategy struct {
	results      []*CompressResult
	secondaryErr error
	mu           sync.Mutex
	callCount    int
}

func (s *seqCompressStrategy) Name() string { return "seq-stub" }

func (s *seqCompressStrategy) SelectCandidates(activeTurns []ConversationTurn, _ int) []ConversationTurn {
	if len(activeTurns) == 0 {
		return []ConversationTurn{}
	}
	return activeTurns
}

func (s *seqCompressStrategy) Compress(_ context.Context, _ []ConversationTurn, _ string, _ ModelProfile) (*CompressResult, error) {
	s.mu.Lock()
	s.callCount++
	idx := s.callCount - 1
	s.mu.Unlock()

	// Second call (idx >= 1) uses secondaryErr if set.
	if idx >= 1 && s.secondaryErr != nil {
		return nil, s.secondaryErr
	}

	if idx < len(s.results) {
		return s.results[idx], nil
	}
	// Default fallback.
	return &CompressResult{CompressedText: "default summary", OriginalTokens: 100, CompressedTokens: 50}, nil
}
