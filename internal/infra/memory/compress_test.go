package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/genai"
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
			PromptTokenCount:     80,
			CandidatesTokenCount: 20,
			TotalTokenCount:      100,
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
		TokenCount: 2,
	}

	// Assert
	if turn.Role != "user" {
		t.Errorf("Role mismatch")
	}
	if turn.Content != "hello world" {
		t.Errorf("Content mismatch")
	}
	if turn.TokenCount != 2 {
		t.Errorf("TokenCount mismatch")
	}
}

// ---- Task 3: SelectCandidates tests ----

func makeNTurns(n int) []ConversationTurn {
	turns := make([]ConversationTurn, n)
	for i := range turns {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		turns[i] = ConversationTurn{Role: role, Content: "msg", TokenCount: 10}
	}
	return turns
}

func TestGenerational_SelectCandidates_should_compress_all_except_recent_M(t *testing.T) {
	// TurnsToKeep=3, 10 turns → compress oldest 7, keep recent 3
	g := &Generational{cfg: GenerationalConfig{TurnsToKeep: 3}}
	turns := makeNTurns(10)

	candidates := g.SelectCandidates(turns, 1000)

	if len(candidates) != 7 {
		t.Fatalf("expected 7 candidates (10 - 3 kept), got %d", len(candidates))
	}
	if candidates[0].Content != turns[0].Content {
		t.Errorf("expected first candidate to be the oldest turn")
	}
}

func TestGenerational_SelectCandidates_should_return_empty_when_all_fit_in_keep(t *testing.T) {
	// TurnsToKeep=5, only 2 turns → nothing to compress
	g := &Generational{cfg: GenerationalConfig{TurnsToKeep: 5}}
	turns := makeNTurns(2)

	candidates := g.SelectCandidates(turns, 1000)

	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates (all fit in keep window), got %d", len(candidates))
	}
}

func TestGenerational_SelectCandidates_should_return_empty_when_activeTurns_is_empty(t *testing.T) {
	g := &Generational{cfg: GenerationalConfig{TurnsToKeep: 5}}

	candidates := g.SelectCandidates([]ConversationTurn{}, 1000)

	if len(candidates) != 0 {
		t.Fatalf("expected empty slice, got %d elements", len(candidates))
	}
}

func TestGenerational_SelectCandidates_should_return_empty_when_turns_equal_keep(t *testing.T) {
	// TurnsToKeep=5, exactly 5 turns → nothing to compress
	g := &Generational{cfg: GenerationalConfig{TurnsToKeep: 5}}
	turns := makeNTurns(5)

	candidates := g.SelectCandidates(turns, 1000)

	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates (turns == keep window), got %d", len(candidates))
	}
}

// ---- Fork-based Compress tests ----

func makeForkRequest(nTurns int) *ForkRequest {
	history := make([]*genai.Content, nTurns)
	for i := range history {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		history[i] = &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: "msg"}},
		}
	}
	return &ForkRequest{
		SystemInstruction: &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: "You are a helpful assistant."}},
		},
		History: history,
	}
}

func TestGenerational_buildForkContents_should_include_system_and_instruction(t *testing.T) {
	g := NewGenerational(GenerationalConfig{TurnsToKeep: 3}, nil, ModelProfile{})
	fork := makeForkRequest(2)

	contents := g.buildForkContents(fork)

	// Should be: system + 2 history turns + summarize instruction = 4
	if len(contents) != 4 {
		t.Fatalf("expected 4 contents, got %d", len(contents))
	}
	// First should be system
	if contents[0].Parts[0].Text != "You are a helpful assistant." {
		t.Errorf("expected system instruction first")
	}
	// Last should be summarize instruction
	last := contents[len(contents)-1]
	if last.Role != "user" {
		t.Errorf("expected last content to be user role, got %q", last.Role)
	}
	if !strings.Contains(last.Parts[0].Text, "handover") {
		t.Errorf("expected handover instruction, got %q", last.Parts[0].Text)
	}
}

func TestGenerational_buildForkContents_should_work_without_system_instruction(t *testing.T) {
	g := NewGenerational(GenerationalConfig{TurnsToKeep: 3}, nil, ModelProfile{})
	fork := &ForkRequest{
		History: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hello"}}},
		},
	}

	contents := g.buildForkContents(fork)

	// Should be: 1 history turn + summarize instruction = 2
	if len(contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(contents))
	}
}

func TestGenerational_Compress_should_return_error_when_worker_is_nil(t *testing.T) {
	g := &Generational{
		cfg:    GenerationalConfig{TurnsToKeep: 5, SummarizeInstruction: defaultSummarizeInstruction},
		worker: nil,
	}
	profile := ModelProfile{ModelID: "gemini-2.0-flash", ContextWindowTokens: 1048576}

	result, err := g.Compress(context.Background(), makeForkRequest(2), profile)

	if err == nil {
		t.Fatal("expected error when worker is nil, got nil")
	}
	if !strings.Contains(err.Error(), "worker is nil") {
		t.Errorf("expected 'worker is nil' in error, got: %s", err.Error())
	}
	if result != nil {
		t.Errorf("expected nil result")
	}
}

func TestGenerational_Compress_should_return_error_when_fork_has_no_history(t *testing.T) {
	g := NewGenerational(GenerationalConfig{TurnsToKeep: 5}, &mockRecordingWorker{}, ModelProfile{})
	profile := ModelProfile{ModelID: "gemini-2.0-flash", ContextWindowTokens: 1048576}

	result, err := g.Compress(context.Background(), &ForkRequest{}, profile)

	if err == nil {
		t.Fatal("expected error for empty fork history, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result")
	}
}

func TestGenerational_Compress_should_return_nil_result_and_error_when_worker_fails(t *testing.T) {
	g := NewGenerational(GenerationalConfig{TurnsToKeep: 3}, &mockFailingWorker{}, ModelProfile{})
	profile := ModelProfile{ModelID: "gemini-2.0-flash", ContextWindowTokens: 1048576}

	result, err := g.Compress(context.Background(), makeForkRequest(3), profile)

	if err == nil {
		t.Fatal("expected error when worker fails, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result when worker fails")
	}
}

func TestGenerational_Compress_should_use_CompressModelID_when_set(t *testing.T) {
	recorder := &mockRecordingWorker{}
	g := NewGenerational(GenerationalConfig{TurnsToKeep: 3}, recorder, ModelProfile{})
	profile := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		CompressModelID:     "gemini-2.0-flash-lite",
		ContextWindowTokens: 1048576,
	}

	_, _ = g.Compress(context.Background(), makeForkRequest(2), profile)

	if recorder.calledWithModel != "gemini-2.0-flash-lite" {
		t.Errorf("expected CompressModelID 'gemini-2.0-flash-lite', worker got %q", recorder.calledWithModel)
	}
}

func TestGenerational_Compress_should_use_primary_model_when_CompressModelID_empty(t *testing.T) {
	recorder := &mockRecordingWorker{}
	g := NewGenerational(GenerationalConfig{TurnsToKeep: 3}, recorder, ModelProfile{})
	profile := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		CompressModelID:     "",
		ContextWindowTokens: 1048576,
	}

	_, _ = g.Compress(context.Background(), makeForkRequest(2), profile)

	if recorder.calledWithModel != "gemini-2.0-flash" {
		t.Errorf("expected fallback to 'gemini-2.0-flash', worker got %q", recorder.calledWithModel)
	}
}

func TestGenerational_Compress_should_pass_structured_contents_to_worker(t *testing.T) {
	recorder := &mockRecordingWorker{}
	g := NewGenerational(GenerationalConfig{TurnsToKeep: 3}, recorder, ModelProfile{})
	profile := ModelProfile{ModelID: "gemini-2.0-flash", ContextWindowTokens: 1048576}

	_, _ = g.Compress(context.Background(), makeForkRequest(2), profile)

	// Worker should receive: system + 2 history + summarize instruction = 4 contents
	if len(recorder.calledWithContents) != 4 {
		t.Errorf("expected 4 contents passed to worker, got %d", len(recorder.calledWithContents))
	}
}

func TestCompressResult_should_calculate_ratio_from_usage_metadata(t *testing.T) {
	worker := &mockRatioWorker{promptTokenCount: 100, candidatesTokenCount: 40}
	g := NewGenerational(GenerationalConfig{TurnsToKeep: 5}, worker, ModelProfile{})
	profile := ModelProfile{ModelID: "gemini-2.0-flash", ContextWindowTokens: 1048576}

	result, err := g.Compress(context.Background(), makeForkRequest(4), profile)

	if err != nil {
		t.Fatalf("Compress() unexpected error: %v", err)
	}
	wantRatio := float64(40) / float64(100)
	if result.ActualCompressionRatio != wantRatio {
		t.Errorf("ActualCompressionRatio = %f, want %f", result.ActualCompressionRatio, wantRatio)
	}
}

// ---- Task 9: Strategy registry tests ----

func TestStrategyRegistry_should_return_error_listing_available_names_when_unknown(t *testing.T) {
	reg := NewStrategyRegistry()

	_, err := reg.Resolve("foo")

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

func TestStrategyRegistry_should_resolve_generational_by_name(t *testing.T) {
	reg := NewStrategyRegistry()

	s, err := reg.Resolve("generational")

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if s.Name() != "generational" {
		t.Errorf("expected strategy name 'generational', got %s", s.Name())
	}
}

// ---- Mock helpers ----

// mockFailingWorker always returns an error from Summarize.
type mockFailingWorker struct{}

func (m *mockFailingWorker) Summarize(_ context.Context, _ string, _ []*genai.Content) (string, *WorkerUsageMetadata, error) {
	return "", nil, errors.New("simulated LLM failure")
}

// mockRecordingWorker records what it was called with and returns a canned result.
type mockRecordingWorker struct {
	calledWithModel    string
	calledWithContents []*genai.Content
}

func (m *mockRecordingWorker) Summarize(_ context.Context, model string, contents []*genai.Content) (string, *WorkerUsageMetadata, error) {
	m.calledWithModel = model
	m.calledWithContents = contents
	return "compressed summary", &WorkerUsageMetadata{
		PromptTokenCount:     50,
		CandidatesTokenCount: 10,
		TotalTokenCount:      60,
	}, nil
}

// mockRatioWorker returns fixed token counts for ratio verification.
type mockRatioWorker struct {
	promptTokenCount     int32
	candidatesTokenCount int32
}

func (m *mockRatioWorker) Summarize(_ context.Context, _ string, _ []*genai.Content) (string, *WorkerUsageMetadata, error) {
	return "summary", &WorkerUsageMetadata{
		PromptTokenCount:     m.promptTokenCount,
		CandidatesTokenCount: m.candidatesTokenCount,
		TotalTokenCount:      m.promptTokenCount + m.candidatesTokenCount,
	}, nil
}
