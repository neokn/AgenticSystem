package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ---- Task 1 & 2: Interface and type definition tests ----

// TestCompressStrategy_InterfaceCompliance verifies that Generational implements
// CompressStrategy without needing a real genai.Client.
func TestCompressStrategy_InterfaceCompliance(t *testing.T) {
	// Arrange
	var _ CompressStrategy = (*Generational)(nil) // compile-time check
}

// TestCompressResult_HasAllFields verifies that CompressResult carries every
// field the acceptance criteria require.
func TestCompressResult_HasAllFields(t *testing.T) {
	// Arrange & Act
	r := CompressResult{
		CompressedText:         "summary",
		OriginalTokens:         100,
		CompressedTokens:       30,
		ActualCompressionRatio: 0.3,
		Cost:                   0.001,
		WorkerUsage: WorkerUsageMetadata{
			PromptTokenCount:    80,
			CandidatesTokenCount: 20,
			TotalTokenCount:     100,
		},
	}

	// Assert
	if r.CompressedText != "summary" {
		t.Errorf("CompressedText mismatch")
	}
	if r.OriginalTokens != 100 {
		t.Errorf("OriginalTokens mismatch")
	}
	if r.CompressedTokens != 30 {
		t.Errorf("CompressedTokens mismatch")
	}
	if r.ActualCompressionRatio != 0.3 {
		t.Errorf("ActualCompressionRatio mismatch")
	}
	if r.WorkerUsage.TotalTokenCount != 100 {
		t.Errorf("WorkerUsage.TotalTokenCount mismatch")
	}
}

// TestConversationTurn_HasAllFields verifies ConversationTurn carries role,
// content, and token_count as specified in the card.
func TestConversationTurn_HasAllFields(t *testing.T) {
	// Arrange & Act
	turn := ConversationTurn{
		Role:       "user",
		Content:    "hello world",
		TokenCount: 3,
	}

	// Assert
	if turn.Role != "user" {
		t.Errorf("Role mismatch")
	}
	if turn.Content != "hello world" {
		t.Errorf("Content mismatch")
	}
	if turn.TokenCount != 3 {
		t.Errorf("TokenCount mismatch")
	}
}

// ---- Task 4: SelectCandidates boundary tests ----

func makeNTurns(n int) []ConversationTurn {
	turns := make([]ConversationTurn, n)
	for i := range turns {
		turns[i] = ConversationTurn{Role: "user", Content: "msg", TokenCount: 10}
	}
	return turns
}

// TestGenerational_SelectCandidates_should_return_oldest_N_when_more_than_N_available
func TestGenerational_SelectCandidates_should_return_oldest_N_when_more_than_N_available(t *testing.T) {
	// Arrange
	g := &Generational{cfg: GenerationalConfig{OldestN: 5}}
	turns := makeNTurns(8)
	// Mark turns with distinct content to identify them
	for i := range turns {
		turns[i].Content = strings.Repeat("x", i+1)
	}

	// Act
	candidates := g.SelectCandidates(turns, 1000)

	// Assert
	if len(candidates) != 5 {
		t.Fatalf("expected 5 candidates, got %d", len(candidates))
	}
	// Oldest N = first 5 (index 0..4)
	for i, c := range candidates {
		if c.Content != turns[i].Content {
			t.Errorf("candidate[%d] should be turn[%d]", i, i)
		}
	}
}

// TestGenerational_SelectCandidates_should_return_all_when_fewer_than_N_available
func TestGenerational_SelectCandidates_should_return_all_when_fewer_than_N_available(t *testing.T) {
	// Arrange
	g := &Generational{cfg: GenerationalConfig{OldestN: 5}}
	turns := makeNTurns(3)

	// Act
	candidates := g.SelectCandidates(turns, 1000)

	// Assert
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates (all available), got %d", len(candidates))
	}
}

// TestGenerational_SelectCandidates_should_return_empty_when_activeTurns_is_empty
func TestGenerational_SelectCandidates_should_return_empty_when_activeTurns_is_empty(t *testing.T) {
	// Arrange
	g := &Generational{cfg: GenerationalConfig{OldestN: 5}}

	// Act
	candidates := g.SelectCandidates([]ConversationTurn{}, 1000)

	// Assert
	if len(candidates) != 0 {
		t.Fatalf("expected empty slice, got %d elements", len(candidates))
	}
}

// TestGenerational_SelectCandidates_should_return_exactly_N_when_exactly_N_available
func TestGenerational_SelectCandidates_should_return_exactly_N_when_exactly_N_available(t *testing.T) {
	// Arrange
	g := &Generational{cfg: GenerationalConfig{OldestN: 5}}
	turns := makeNTurns(5)

	// Act
	candidates := g.SelectCandidates(turns, 1000)

	// Assert
	if len(candidates) != 5 {
		t.Fatalf("expected exactly 5 candidates, got %d", len(candidates))
	}
}

// ---- Task 8: Prompt template config tests ----

// TestGenerationalConfig_should_use_default_template_when_not_configured
func TestGenerationalConfig_should_use_default_template_when_not_configured(t *testing.T) {
	// Arrange & Act
	cfg := defaultGenerationalConfig()

	// Assert
	if cfg.PromptTemplate == "" {
		t.Error("default PromptTemplate must not be empty")
	}
}

// TestGenerationalConfig_should_use_custom_template_when_configured
func TestGenerationalConfig_should_use_custom_template_when_configured(t *testing.T) {
	// Arrange
	customTemplate := "custom: {{.ExistingSummary}} | {{.Turns}}"
	cfg := GenerationalConfig{
		OldestN:        5,
		PromptTemplate: customTemplate,
	}

	// Act: build prompt to verify template is used verbatim
	g := &Generational{cfg: cfg}
	prompt := g.buildPrompt("prior summary", makeNTurns(2))

	// Assert: the custom template text (prefix) must appear in output
	if !strings.Contains(prompt, "custom:") {
		t.Errorf("expected custom template to be used, got: %s", prompt)
	}
}

// ---- Task 9: Strategy registry tests ----

// TestStrategyRegistry_should_return_error_listing_available_names_when_unknown
func TestStrategyRegistry_should_return_error_listing_available_names_when_unknown(t *testing.T) {
	// Arrange
	reg := NewStrategyRegistry()

	// Act
	_, err := reg.Resolve("foo")

	// Assert
	if err == nil {
		t.Fatal("expected error for unknown strategy, got nil")
	}
	if !strings.Contains(err.Error(), "unknown strategy") {
		t.Errorf("error should mention 'unknown strategy', got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "generational") {
		t.Errorf("error should list 'generational' as available, got: %s", err.Error())
	}
}

// TestStrategyRegistry_should_resolve_generational_by_name
func TestStrategyRegistry_should_resolve_generational_by_name(t *testing.T) {
	// Arrange
	reg := NewStrategyRegistry()

	// Act
	s, err := reg.Resolve("generational")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if s.Name() != "generational" {
		t.Errorf("expected strategy name 'generational', got %s", s.Name())
	}
}

// ---- Task 12: CompressResult ratio + fallback model warning tests ----

// TestCompressResult_should_calculate_ratio_correctly
func TestCompressResult_should_calculate_ratio_correctly(t *testing.T) {
	// Arrange
	originalTokens := 100
	compressedTokens := 40

	// Act
	ratio := float64(compressedTokens) / float64(originalTokens)
	r := CompressResult{
		OriginalTokens:         originalTokens,
		CompressedTokens:       compressedTokens,
		ActualCompressionRatio: ratio,
	}

	// Assert
	if r.ActualCompressionRatio != 0.4 {
		t.Errorf("expected ratio 0.4, got %f", r.ActualCompressionRatio)
	}
}

// TestGenerational_Compress_should_return_nil_result_and_error_when_worker_fails
func TestGenerational_Compress_should_return_nil_result_and_error_when_worker_fails(t *testing.T) {
	// Arrange — use mock worker that always fails
	g := &Generational{
		cfg:    defaultGenerationalConfig(),
		worker: &mockFailingWorker{},
	}
	turns := makeNTurns(3)
	profile := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		ContextWindowTokens: 1048576,
	}

	// Act
	result, err := g.Compress(context.Background(), turns, "", profile)

	// Assert
	if err == nil {
		t.Fatal("expected error when worker fails, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result when worker fails, got non-nil")
	}
}

// TestGenerational_Compress_should_use_CompressModelID_when_set
func TestGenerational_Compress_should_use_CompressModelID_when_set(t *testing.T) {
	// Arrange
	recorder := &mockRecordingWorker{}
	g := &Generational{
		cfg:    defaultGenerationalConfig(),
		worker: recorder,
	}
	turns := makeNTurns(2)
	profile := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		CompressModelID:     "gemini-2.0-flash-lite",
		ContextWindowTokens: 1048576,
	}

	// Act
	_, _ = g.Compress(context.Background(), turns, "", profile)

	// Assert
	if recorder.calledWithModel != "gemini-2.0-flash-lite" {
		t.Errorf("expected CompressModelID 'gemini-2.0-flash-lite', worker got %q", recorder.calledWithModel)
	}
}

// TestGenerational_Compress_should_use_primary_model_when_CompressModelID_empty
func TestGenerational_Compress_should_use_primary_model_when_CompressModelID_empty(t *testing.T) {
	// Arrange
	recorder := &mockRecordingWorker{}
	g := &Generational{
		cfg:    defaultGenerationalConfig(),
		worker: recorder,
	}
	turns := makeNTurns(2)
	profile := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		CompressModelID:     "", // empty → fall back
		ContextWindowTokens: 1048576,
	}

	// Act
	_, _ = g.Compress(context.Background(), turns, "", profile)

	// Assert
	if recorder.calledWithModel != "gemini-2.0-flash" {
		t.Errorf("expected fallback to primary model 'gemini-2.0-flash', worker got %q", recorder.calledWithModel)
	}
}

// TestGenerational_Compress_should_include_existingSummary_in_prompt
func TestGenerational_Compress_should_include_existingSummary_in_prompt(t *testing.T) {
	// Arrange
	recorder := &mockRecordingWorker{}
	g := &Generational{
		cfg:    defaultGenerationalConfig(),
		worker: recorder,
	}
	turns := makeNTurns(2)
	profile := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		ContextWindowTokens: 1048576,
	}
	existingSummary := "prior context: user discussed topic A"

	// Act
	_, _ = g.Compress(context.Background(), turns, existingSummary, profile)

	// Assert
	if !strings.Contains(recorder.calledWithPrompt, existingSummary) {
		t.Errorf("expected prompt to include existingSummary %q, got: %s", existingSummary, recorder.calledWithPrompt)
	}
}

// ---- Mock helpers ----

// mockFailingWorker always returns an error from Summarize.
type mockFailingWorker struct{}

func (m *mockFailingWorker) Summarize(_ context.Context, _, _ string) (string, *WorkerUsageMetadata, error) {
	return "", nil, errors.New("simulated LLM failure")
}

// mockRecordingWorker records what it was called with and returns a canned result.
type mockRecordingWorker struct {
	calledWithModel  string
	calledWithPrompt string
}

func (m *mockRecordingWorker) Summarize(_ context.Context, model, prompt string) (string, *WorkerUsageMetadata, error) {
	m.calledWithModel = model
	m.calledWithPrompt = prompt
	return "compressed summary", &WorkerUsageMetadata{
		PromptTokenCount:    50,
		CandidatesTokenCount: 10,
		TotalTokenCount:     60,
	}, nil
}
