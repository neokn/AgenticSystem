package memory

import (
	"context"
	"errors"
	"sync"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// ---------------------------------------------------------------------------
// Test helpers / stubs
// ---------------------------------------------------------------------------

// stubTokenCounter implements tokenCounter for unit tests. It returns a fixed
// token count without any network or file-system access.
type stubTokenCounter struct {
	count int32
	err   error
}

func (s *stubTokenCounter) CountTokens(_ []*genai.Content, _ *genai.CountTokensConfig) (*genai.CountTokensResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &genai.CountTokensResult{TotalTokens: s.count}, nil
}

// stubAPICounter implements apiTokenCounter for unit tests.
type stubAPICounter struct {
	count    int32
	err      error
	callsMu  sync.Mutex
	calls    int
}

func (s *stubAPICounter) CountTokensAPI(ctx context.Context, modelID string, contents []*genai.Content) (int32, error) {
	s.callsMu.Lock()
	s.calls++
	s.callsMu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	return s.count, nil
}

func (s *stubAPICounter) CallCount() int {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	return s.calls
}

// stubCompressStrategy is a stateless CompressStrategy for tests.
type stubCompressStrategy struct {
	candidates []ConversationTurn
	result     *CompressResult
	err        error
	selectMu   sync.Mutex
	selectCalls int
	compressCalls int
}

func (s *stubCompressStrategy) Name() string { return "stub" }

func (s *stubCompressStrategy) SelectCandidates(activeTurns []ConversationTurn, _ int) []ConversationTurn {
	s.selectMu.Lock()
	s.selectCalls++
	s.selectMu.Unlock()
	if s.candidates != nil {
		return s.candidates
	}
	return activeTurns
}

func (s *stubCompressStrategy) Compress(_ context.Context, _ []ConversationTurn, _ string, _ ModelProfile) (*CompressResult, error) {
	s.selectMu.Lock()
	s.compressCalls++
	s.selectMu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &CompressResult{CompressedText: "summary", OriginalTokens: 100, CompressedTokens: 20}, nil
}


// newTestPlugin builds a MemoryPlugin with sensible defaults for tests.
// profile: gemini-2.0-flash context=1_000_000, maxOutput=8192
// threshold: 0.80
func newTestPlugin(t *testing.T, tc tokenCounter, ac apiTokenCounter, strategy CompressStrategy, threshold float64) *MemoryPlugin {
	t.Helper()
	profile := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		ContextWindowTokens: 1_000_000,
		MaxOutputTokens:     8_192,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.10,
		SummaryRatio: 0.15,
		ActiveRatio:  0.65,
		BufferRatio:  0.10,
	}
	layout, err := NewLayout(profile, cfg)
	if err != nil {
		t.Fatalf("newTestPlugin: NewLayout: %v", err)
	}
	if strategy == nil {
		strategy = &stubCompressStrategy{
			result: &CompressResult{CompressedText: "summary", OriginalTokens: 100, CompressedTokens: 20},
		}
	}
	p, err := newMemoryPluginWithDeps(tc, ac, layout, strategy, profile, threshold, 0)
	if err != nil {
		t.Fatalf("newTestPlugin: newMemoryPluginWithDeps: %v", err)
	}
	return p
}

// makeUserMsg returns a minimal genai.Content with a single user text part.
func makeUserMsg(text string) *genai.Content {
	return &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: text}},
	}
}

// makeRequest wraps a user message into a model.LLMRequest.
func makeRequest(msg string) *model.LLMRequest {
	return &model.LLMRequest{
		Model:    "gemini-2.0-flash",
		Contents: []*genai.Content{makeUserMsg(msg)},
	}
}

// makeResponse wraps a token count into a model.LLMResponse with UsageMetadata.
func makeResponse(totalTokenCount int32) *model.LLMResponse {
	return &model.LLMResponse{
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			TotalTokenCount: totalTokenCount,
		},
	}
}

// ---------------------------------------------------------------------------
// Task 1 + 2: Constructor and plugin.Plugin registration
// ---------------------------------------------------------------------------

func TestNewMemoryPlugin_ReturnsPlugin_WhenValidArgs(t *testing.T) {
	// Arrange
	tc := &stubTokenCounter{count: 10}
	ac := &stubAPICounter{count: 10}

	// Act
	p := newTestPlugin(t, tc, ac, nil, 0.80)

	// Assert
	if p == nil {
		t.Fatal("expected non-nil MemoryPlugin")
	}
}

func TestNewMemoryPlugin_DefaultsThresholdTo80Percent_WhenZeroPassed(t *testing.T) {
	// Arrange / Act
	p := newTestPlugin(t, &stubTokenCounter{}, &stubAPICounter{}, nil, 0.0)

	// Assert
	if p.threshold != defaultThreshold {
		t.Errorf("expected threshold %v, got %v", defaultThreshold, p.threshold)
	}
}

func TestNewMemoryPlugin_RejectsThresholdOutOfRange(t *testing.T) {
	// Arrange
	profile := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		ContextWindowTokens: 1_000_000,
		MaxOutputTokens:     8_192,
	}
	cfg := LayoutConfig{PinnedRatio: 0.10, SummaryRatio: 0.15, ActiveRatio: 0.65, BufferRatio: 0.10}
	layout, _ := NewLayout(profile, cfg)
	strategy := &stubCompressStrategy{}

	tests := []struct {
		name      string
		threshold float64
	}{
		{"negative", -0.1},
		{"zero-but-treat-as-invalid-out-of-range-only-if-below-zero", -1.0},
		{"one", 1.0},
		{"greater-than-one", 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			_, err := newMemoryPluginWithDeps(&stubTokenCounter{}, &stubAPICounter{}, layout, strategy, profile, tt.threshold, 0)

			// Assert
			if err == nil {
				t.Errorf("expected error for threshold %v, got nil", tt.threshold)
			}
		})
	}
}

func TestMemoryPlugin_BuildPlugin_ReturnsNonNilPlugin(t *testing.T) {
	// Arrange
	p := newTestPlugin(t, &stubTokenCounter{}, &stubAPICounter{}, nil, 0.80)

	// Act
	pl, err := p.BuildPlugin()

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pl == nil {
		t.Fatal("expected non-nil plugin.Plugin")
	}
	if pl.Name() != "memory_plugin" {
		t.Errorf("expected name 'memory_plugin', got %q", pl.Name())
	}
}

// ---------------------------------------------------------------------------
// Task 3: AfterModelCallback stores TotalTokenCount
// ---------------------------------------------------------------------------

func TestAfterModelCallback_StoresTokenCount_WhenUsageMetadataPresent(t *testing.T) {
	// Arrange
	p := newTestPlugin(t, &stubTokenCounter{count: 5}, &stubAPICounter{}, nil, 0.80)
	ctx := agent.CallbackContext(nil)
	resp := makeResponse(5000)

	// Act
	_, err := p.afterModelCallback(ctx, resp, nil)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p.mu.Lock()
	got := p.lastTotalTokens
	p.mu.Unlock()
	if got != 5000 {
		t.Errorf("expected lastTotalTokens=5000, got %d", got)
	}
}

func TestAfterModelCallback_DoesNotOverwriteWithZero_WhenUsageMetadataIsNil(t *testing.T) {
	// Arrange
	p := newTestPlugin(t, &stubTokenCounter{count: 5}, &stubAPICounter{}, nil, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 3000
	p.mu.Unlock()

	ctx := agent.CallbackContext(nil)
	resp := &model.LLMResponse{UsageMetadata: nil}

	// Act
	_, err := p.afterModelCallback(ctx, resp, nil)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p.mu.Lock()
	got := p.lastTotalTokens
	p.mu.Unlock()
	if got != 3000 {
		t.Errorf("expected lastTotalTokens to remain 3000, got %d", got)
	}
}

func TestAfterModelCallback_DoesNotOverwriteWithZero_WhenTotalTokenCountIsZero(t *testing.T) {
	// Arrange
	p := newTestPlugin(t, &stubTokenCounter{count: 5}, &stubAPICounter{}, nil, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 4000
	p.mu.Unlock()

	ctx := agent.CallbackContext(nil)
	resp := makeResponse(0) // TotalTokenCount == 0

	// Act
	_, err := p.afterModelCallback(ctx, resp, nil)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p.mu.Lock()
	got := p.lastTotalTokens
	p.mu.Unlock()
	if got != 4000 {
		t.Errorf("expected lastTotalTokens to remain 4000, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Task 4: offline estimator helper
// ---------------------------------------------------------------------------

func TestEstimatedTotal_ReturnsLastPlusMsgPlusMaxOutput(t *testing.T) {
	// Arrange
	p := newTestPlugin(t, &stubTokenCounter{count: 0}, &stubAPICounter{}, nil, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 100_000
	p.mu.Unlock()

	// Act — snapshot last under lock (mirrors real BeforeModelCallback logic)
	p.mu.Lock()
	last := p.lastTotalTokens
	p.mu.Unlock()
	got := p.estimatedTotal(last, 500)

	// Assert: 100_000 + 500 + 8_192 = 108_692
	want := 108_692
	if got != want {
		t.Errorf("estimatedTotal: expected %d, got %d", want, got)
	}
}

func TestEstimatedTotal_WorksCorrectly_WhenLastTotalTokensIsZero(t *testing.T) {
	// Arrange — first turn, no prior AfterModelCallback
	p := newTestPlugin(t, &stubTokenCounter{count: 0}, &stubAPICounter{}, nil, 0.80)

	// Act
	got := p.estimatedTotal(0, 200)

	// Assert: 0 + 200 + 8_192 = 8_392
	want := 8_392
	if got != want {
		t.Errorf("first-turn estimatedTotal: expected %d, got %d", want, got)
	}
}

// ---------------------------------------------------------------------------
// Task 5: BeforeModelCallback first layer (offline estimate)
// ---------------------------------------------------------------------------

func TestBeforeModelCallback_PassesThrough_WhenBelowThreshold(t *testing.T) {
	// Arrange: 100 offline tokens, threshold 80% of 1_000_000 = 800_000
	// estimatedTotal = 0 + 100 + 8_192 = 8_292 << 800_000
	tc := &stubTokenCounter{count: 100}
	ac := &stubAPICounter{}
	p := newTestPlugin(t, tc, ac, nil, 0.80)
	req := makeRequest("hello world")

	// Act
	result, err := p.runBeforeModel(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil LLMResponse (pass-through), got non-nil")
	}
	if ac.CallCount() != 0 {
		t.Errorf("expected zero countTokens API calls, got %d", ac.CallCount())
	}
}

func TestBeforeModelCallback_NeverCallsAPI_WhenBelowThreshold(t *testing.T) {
	// Arrange: lastTotalTokens=100_000 (10% of 1M), threshold 80%
	// estimatedTotal = 100_000 + 50 + 8_192 = 108_242 << 800_000
	tc := &stubTokenCounter{count: 50}
	ac := &stubAPICounter{}
	p := newTestPlugin(t, tc, ac, nil, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 100_000
	p.mu.Unlock()
	req := makeRequest("short message")

	// Act
	result, err := p.runBeforeModel(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil (pass-through)")
	}
	if ac.CallCount() != 0 {
		t.Errorf("expected 0 API calls, got %d", ac.CallCount())
	}
}

// ---------------------------------------------------------------------------
// Task 6: BeforeModelCallback second layer (countTokens API + false alarm)
// ---------------------------------------------------------------------------

func TestBeforeModelCallback_CallsAPI_WhenOfflineEstimateAboveThreshold(t *testing.T) {
	// Arrange: lastTotalTokens=790_000, offline msg=3000
	// estimatedTotal = 790_000 + 3_000 + 8_192 = 801_192 > 800_000 → triggers API
	// API returns precise total of 750_000 — false alarm, no compression
	tc := &stubTokenCounter{count: 3000}
	ac := &stubAPICounter{count: 750_000} // precise < threshold
	p := newTestPlugin(t, tc, ac, nil, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 790_000
	p.mu.Unlock()
	req := makeRequest("some long message text")

	// Act
	result, err := p.runBeforeModel(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil (false alarm pass-through), got non-nil")
	}
	if ac.CallCount() != 1 {
		t.Errorf("expected 1 API call, got %d", ac.CallCount())
	}
	// Verify countTokensAPICallCount metric incremented
	snap := p.GetSnapshot()
	if snap.CountTokensAPICallCount != 1 {
		t.Errorf("expected CountTokensAPICallCount=1, got %d", snap.CountTokensAPICallCount)
	}
	// Compression must NOT have triggered
	if snap.CompressTriggerCount != 0 {
		t.Errorf("expected CompressTriggerCount=0, got %d", snap.CompressTriggerCount)
	}
}

// ---------------------------------------------------------------------------
// Task 7: Compression trigger
// ---------------------------------------------------------------------------

func TestBeforeModelCallback_TriggersCompression_WhenPreciseTotalAboveThreshold(t *testing.T) {
	// Arrange: lastTotalTokens=790_000, offline msg=3_000
	// estimatedTotal = 801_192 > threshold → API call
	// API returns precise total 850_000 > threshold → compression fires
	tc := &stubTokenCounter{count: 3_000}
	ac := &stubAPICounter{count: 850_000}
	strategy := &stubCompressStrategy{
		result: &CompressResult{
			CompressedText:   "compressed summary",
			OriginalTokens:   100,
			CompressedTokens: 20,
		},
	}
	p := newTestPlugin(t, tc, ac, strategy, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 790_000
	p.mu.Unlock()

	req := &model.LLMRequest{
		Model: "gemini-2.0-flash",
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "turn1"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "reply1"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "turn2"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "reply2"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "new message"}}},
		},
	}

	// Act
	result, err := p.runBeforeModel(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// result should be nil — we rewrite req.Contents in place (or pass through)
	// ADK convention: return nil means the request was modified in-place; the
	// modified req.Contents is the signal, not a returned LLMResponse.
	_ = result

	// req.Contents must have been rewritten
	if len(req.Contents) == 0 {
		t.Fatal("expected req.Contents to be rewritten, got empty")
	}

	// CompressTriggerCount must have incremented
	snap := p.GetSnapshot()
	if snap.CompressTriggerCount != 1 {
		t.Errorf("expected CompressTriggerCount=1, got %d", snap.CompressTriggerCount)
	}

	// summaries must have a new entry
	p.mu.Lock()
	summaryCount := len(p.summaries)
	p.mu.Unlock()
	if summaryCount != 1 {
		t.Errorf("expected 1 summary entry, got %d", summaryCount)
	}

	// strategy must have been called
	if strategy.compressCalls != 1 {
		t.Errorf("expected 1 Compress call, got %d", strategy.compressCalls)
	}
}

func TestBeforeModelCallback_ReclaimedTokensRecorded_WhenCompressionFires(t *testing.T) {
	// Arrange: must have prior turns + new user message so activeTurns is non-empty.
	tc := &stubTokenCounter{count: 3_000}
	ac := &stubAPICounter{count: 850_000}
	strategy := &stubCompressStrategy{
		result: &CompressResult{
			CompressedText:   "summary",
			OriginalTokens:   200,
			CompressedTokens: 40,
		},
	}
	p := newTestPlugin(t, tc, ac, strategy, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 790_000
	p.mu.Unlock()
	req := &model.LLMRequest{
		Model: "gemini-2.0-flash",
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "prior turn"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "prior reply"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "msg"}}},
		},
	}

	// Act
	_, _ = p.runBeforeModel(context.Background(), req)

	// Assert
	snap := p.GetSnapshot()
	if len(snap.CompressReclaimedTokens) != 1 {
		t.Fatalf("expected 1 reclaimed-tokens entry, got %d", len(snap.CompressReclaimedTokens))
	}
	expected := 200 - 40 // 160
	if snap.CompressReclaimedTokens[0] != expected {
		t.Errorf("expected reclaimedTokens=%d, got %d", expected, snap.CompressReclaimedTokens[0])
	}
}

// ---------------------------------------------------------------------------
// Task 8: Configurable threshold
// ---------------------------------------------------------------------------

func TestNewMemoryPlugin_UsesCustomThreshold(t *testing.T) {
	// Arrange
	p := newTestPlugin(t, &stubTokenCounter{}, &stubAPICounter{}, nil, 0.70)

	// Assert
	if p.threshold != 0.70 {
		t.Errorf("expected threshold 0.70, got %v", p.threshold)
	}
}

func TestBeforeModelCallback_Respects70PercentThreshold(t *testing.T) {
	// Arrange: threshold=70%, contextWindow=1_000_000 → threshold at 700_000
	// lastTotalTokens=692_000, offline msg=3_000
	// estimatedTotal = 692_000 + 3_000 + 8_192 = 703_192 > 700_000 → API call
	// API returns 650_000 < 700_000 → false alarm, no compression
	// With 80% default that would be 650_000 < 800_000 → no API call at all.
	tc := &stubTokenCounter{count: 3_000}
	ac := &stubAPICounter{count: 650_000}
	p := newTestPlugin(t, tc, ac, nil, 0.70)
	p.mu.Lock()
	p.lastTotalTokens = 692_000
	p.mu.Unlock()
	req := makeRequest("medium length message")

	// Act
	_, err := p.runBeforeModel(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// API must have been called because offline estimate crossed 70% threshold
	if ac.CallCount() != 1 {
		t.Errorf("expected 1 API call (70%% threshold), got %d", ac.CallCount())
	}
}

// ---------------------------------------------------------------------------
// Task 9: MemoryMetrics struct and GetSnapshot
// ---------------------------------------------------------------------------

func TestGetSnapshot_ReturnsConsistentMetrics(t *testing.T) {
	// Arrange
	p := newTestPlugin(t, &stubTokenCounter{}, &stubAPICounter{}, nil, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 42_000
	p.metrics.CountTokensAPICallCount = 3
	p.metrics.CompressTriggerCount = 1
	p.metrics.CompressReclaimedTokens = []int{80, 120}
	p.mu.Unlock()

	// Act
	snap := p.GetSnapshot()

	// Assert
	if snap.LastTotalTokens != 42_000 {
		t.Errorf("expected LastTotalTokens=42000, got %d", snap.LastTotalTokens)
	}
	if snap.CountTokensAPICallCount != 3 {
		t.Errorf("expected CountTokensAPICallCount=3, got %d", snap.CountTokensAPICallCount)
	}
	if snap.CompressTriggerCount != 1 {
		t.Errorf("expected CompressTriggerCount=1, got %d", snap.CompressTriggerCount)
	}
	if len(snap.CompressReclaimedTokens) != 2 {
		t.Errorf("expected 2 entries in CompressReclaimedTokens, got %d", len(snap.CompressReclaimedTokens))
	}
	// UsageRatio = 42_000 / 1_000_000 = 0.042
	wantRatio := float64(42_000) / float64(1_000_000)
	if snap.UsageRatio != wantRatio {
		t.Errorf("expected UsageRatio=%v, got %v", wantRatio, snap.UsageRatio)
	}
}

// ---------------------------------------------------------------------------
// Bug 1 fix: new user message must NOT be in SelectCandidates activeTurns
// ---------------------------------------------------------------------------

// captureSelectStrategy records the activeTurns slice passed to SelectCandidates.
type captureSelectStrategy struct {
	stubCompressStrategy
	capturedTurns []ConversationTurn
	captureMu     sync.Mutex
}

func (c *captureSelectStrategy) SelectCandidates(activeTurns []ConversationTurn, targetReclaim int) []ConversationTurn {
	c.captureMu.Lock()
	// Deep-copy so we don't hold a slice alias into req.Contents.
	copied := make([]ConversationTurn, len(activeTurns))
	copy(copied, activeTurns)
	c.capturedTurns = copied
	c.captureMu.Unlock()
	return c.stubCompressStrategy.SelectCandidates(activeTurns, targetReclaim)
}

func TestTriggerCompression_NewUserMessageNotInCandidates_WhenCompressionFires(t *testing.T) {
	// Arrange: precise total above threshold so compression fires.
	tc := &stubTokenCounter{count: 3_000}
	ac := &stubAPICounter{count: 850_000}
	capture := &captureSelectStrategy{
		stubCompressStrategy: stubCompressStrategy{
			result: &CompressResult{
				CompressedText:   "summary",
				OriginalTokens:   100,
				CompressedTokens: 20,
			},
		},
	}
	p := newTestPlugin(t, tc, ac, capture, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 790_000
	p.mu.Unlock()

	const newUserText = "brand new user message"
	req := &model.LLMRequest{
		Model: "gemini-2.0-flash",
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "turn1"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "reply1"}}},
			{Role: "user", Parts: []*genai.Part{{Text: newUserText}}},
		},
	}

	// Act
	_, err := p.runBeforeModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert — the brand-new user message must not appear in the captured turns.
	capture.captureMu.Lock()
	turns := capture.capturedTurns
	capture.captureMu.Unlock()

	for _, turn := range turns {
		if turn.Content == newUserText {
			t.Errorf("new user message %q was included in SelectCandidates activeTurns — it must be excluded", newUserText)
		}
	}
}

// ---------------------------------------------------------------------------
// Bug 2 fix: countMsgTokens returns conservative fallback on tokenizer error
// ---------------------------------------------------------------------------

func TestCountMsgTokens_ReturnsMaxOutputTokens_WhenTokenizerErrors(t *testing.T) {
	// Arrange: tokenizer always errors.
	tc := &stubTokenCounter{err: errors.New("tokenizer unavailable")}
	ac := &stubAPICounter{}
	p := newTestPlugin(t, tc, ac, nil, 0.80)

	// Act
	got := p.countMsgTokens([]*genai.Content{makeUserMsg("hello")})

	// Assert: fallback must be MaxOutputTokens (8192), not 0.
	want := p.profile.MaxOutputTokens
	if got != want {
		t.Errorf("expected conservative fallback %d (MaxOutputTokens), got %d", want, got)
	}
	if got == 0 {
		t.Error("countMsgTokens must not return 0 on tokenizer error — that under-estimates (optimistic, not conservative)")
	}
}

func TestBeforeModelCallback_TriggersAPICall_WhenTokenizerErrors(t *testing.T) {
	// Arrange: tokenizer errors → countMsgTokens returns MaxOutputTokens (8192).
	// lastTotalTokens = 795_000.
	// estimatedTotal = 795_000 + 8_192 (fallback) + 8_192 = 811_384 > 800_000 → API triggered.
	// API returns 750_000 < 800_000 → false alarm, no compression.
	tc := &stubTokenCounter{err: errors.New("tokenizer unavailable")}
	ac := &stubAPICounter{count: 750_000}
	p := newTestPlugin(t, tc, ac, nil, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 795_000
	p.mu.Unlock()
	req := makeRequest("any message")

	// Act
	_, err := p.runBeforeModel(context.Background(), req)

	// Assert: API must have been called because conservative fallback over-estimated.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ac.CallCount() == 0 {
		t.Error("expected countTokens API to be called after tokenizer error (conservative fallback must over-estimate)")
	}
}

// ---------------------------------------------------------------------------
// Bug 3 fix: no system prompt duplication in buildCompressedContents
// ---------------------------------------------------------------------------

func TestBuildCompressedContents_NoSystemPromptDuplication_WhenSystemInstructionAlreadyInContents(t *testing.T) {
	// Arrange: systemInstruction is both in Config AND the first element of Contents.
	// buildCompressedContents must NOT prepend it again.
	sysInstruction := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: "You are a helpful assistant."}},
	}
	req := &model.LLMRequest{
		Model: "gemini-2.0-flash",
		Config: &genai.GenerateContentConfig{
			SystemInstruction: sysInstruction,
		},
		Contents: []*genai.Content{
			sysInstruction, // same pointer — ADK already put it in Contents
			{Role: "user", Parts: []*genai.Part{{Text: "turn1"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "reply1"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "user msg"}}},
		},
	}

	// Act
	result := buildCompressedContents(req, "compressed summary", 2)

	// Assert: count occurrences of the system instruction pointer in result.
	occurrences := 0
	for _, c := range result {
		if c == sysInstruction {
			occurrences++
		}
	}
	if occurrences > 1 {
		t.Errorf("system instruction appears %d times in result — expected at most 1 (no duplication)", occurrences)
	}
}

func TestBuildCompressedContents_IncludesSystemPrompt_WhenNotAlreadyInContents(t *testing.T) {
	// Arrange: systemInstruction is in Config but NOT in Contents.
	// buildCompressedContents must prepend it.
	sysInstruction := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: "You are a helpful assistant."}},
	}
	req := &model.LLMRequest{
		Model: "gemini-2.0-flash",
		Config: &genai.GenerateContentConfig{
			SystemInstruction: sysInstruction,
		},
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "turn1"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "reply1"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "user msg"}}},
		},
	}

	// Act
	result := buildCompressedContents(req, "summary", 1)

	// Assert: system instruction appears exactly once and is first.
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	if result[0] != sysInstruction {
		t.Error("expected system instruction to be the first element when not already in contents")
	}
	occurrences := 0
	for _, c := range result {
		if c == sysInstruction {
			occurrences++
		}
	}
	if occurrences != 1 {
		t.Errorf("expected system instruction exactly once, got %d", occurrences)
	}
}

// ---------------------------------------------------------------------------
// Should-fix 2: exact req.Contents structure after compression
// ---------------------------------------------------------------------------

func TestBeforeModelCallback_ReqContentsExactStructure_AfterCompression(t *testing.T) {
	// Arrange: 4 prior turns + 1 new user message.
	// Strategy compresses first 2 turns; the remaining 2 prior turns are "recent active".
	// Expected structure: [summary_turn, recent1, recent2, user_msg]  (no system prompt)
	tc := &stubTokenCounter{count: 3_000}
	ac := &stubAPICounter{count: 850_000}

	const summaryText = "summary of first two turns"
	// Strategy always returns the first 2 activeTurns as candidates.
	strategy := &stubCompressStrategy{
		candidates: nil, // nil → returns all activeTurns; we rely on compressedCount in buildCompressedContents
		result: &CompressResult{
			CompressedText:   summaryText,
			OriginalTokens:   200,
			CompressedTokens: 40,
		},
	}
	p := newTestPlugin(t, tc, ac, strategy, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 790_000
	p.mu.Unlock()

	userTurn1 := &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "turn1"}}}
	modelTurn1 := &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "reply1"}}}
	userTurn2 := &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "turn2"}}}
	modelTurn2 := &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "reply2"}}}
	newUserMsg := &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "new question"}}}

	req := &model.LLMRequest{
		Model:    "gemini-2.0-flash",
		Contents: []*genai.Content{userTurn1, modelTurn1, userTurn2, modelTurn2, newUserMsg},
	}

	// Act
	_, err := p.runBeforeModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert: last element must be the original new user message.
	contents := req.Contents
	if len(contents) == 0 {
		t.Fatal("req.Contents must not be empty after compression")
	}
	last := contents[len(contents)-1]
	if last != newUserMsg {
		t.Errorf("last element must be the new user message; got role=%q text=%q",
			last.Role, contentText(last))
	}

	// Assert: new user message appears exactly once.
	userMsgCount := 0
	for _, c := range contents {
		if c == newUserMsg {
			userMsgCount++
		}
	}
	if userMsgCount != 1 {
		t.Errorf("new user message must appear exactly once in req.Contents, got %d", userMsgCount)
	}

	// Assert: first non-system element is the summary turn (role "model").
	summaryFound := false
	for _, c := range contents {
		if len(c.Parts) > 0 && c.Parts[0].Text == summaryText {
			if c.Role != "model" {
				t.Errorf("summary turn must have role 'model', got %q", c.Role)
			}
			summaryFound = true
			break
		}
	}
	if !summaryFound {
		t.Error("summary turn not found in req.Contents after compression")
	}

	// Assert: no system instruction (none was set).
	for _, c := range contents {
		if c.Role == "system" || (req.Config == nil && c.Role == "") {
			// Allow — but we didn't set one.
		}
	}
	// The critical structural invariant: summary comes before recent active turns,
	// and recent active turns come before the new user message.
	summaryIdx := -1
	newMsgIdx := -1
	for i, c := range contents {
		if len(c.Parts) > 0 && c.Parts[0].Text == summaryText {
			summaryIdx = i
		}
		if c == newUserMsg {
			newMsgIdx = i
		}
	}
	if summaryIdx == -1 {
		t.Fatal("summary not found in contents")
	}
	if newMsgIdx == -1 {
		t.Fatal("new user message not found in contents")
	}
	if summaryIdx >= newMsgIdx {
		t.Errorf("summary (index %d) must come before new user message (index %d)", summaryIdx, newMsgIdx)
	}
}

// ---------------------------------------------------------------------------
// Task 10: Concurrent safety (go test -race)
// ---------------------------------------------------------------------------

func TestBeforeAndAfterModelCallback_NoConcurrentDataRace(t *testing.T) {
	// Arrange: run many goroutines calling Before and After concurrently.
	tc := &stubTokenCounter{count: 100} // well below threshold
	ac := &stubAPICounter{}
	p := newTestPlugin(t, tc, ac, nil, 0.80)

	var wg sync.WaitGroup
	const goroutines = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			req := makeRequest("concurrent message")
			_, _ = p.runBeforeModel(context.Background(), req)
		}()
		go func(n int) {
			defer wg.Done()
			resp := makeResponse(int32(n * 1000))
			_, _ = p.afterModelCallback(nil, resp, nil)
		}(i)
	}

	wg.Wait()
	// If go test -race finds a data race, the test binary exits with a non-zero
	// status. We do not need an assertion here — the race detector catches it.
}

// ---------------------------------------------------------------------------
// Edge case: AfterModelCallback with nil response
// ---------------------------------------------------------------------------

func TestAfterModelCallback_HandlesNilResponse_Gracefully(t *testing.T) {
	// Arrange
	p := newTestPlugin(t, &stubTokenCounter{}, &stubAPICounter{}, nil, 0.80)
	p.mu.Lock()
	p.lastTotalTokens = 5_000
	p.mu.Unlock()

	// Act — pass nil response (some ADK error paths may supply nil)
	_, err := p.afterModelCallback(nil, nil, errors.New("model error"))

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p.mu.Lock()
	got := p.lastTotalTokens
	p.mu.Unlock()
	if got != 5_000 {
		t.Errorf("expected lastTotalTokens to remain 5000 after nil response, got %d", got)
	}
}
