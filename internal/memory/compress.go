package memory

import (
	"context"
	"fmt"
	"strings"

	dp "github.com/google/dotprompt/go/dotprompt"
	"google.golang.org/genai"
)

// ConversationTurn represents a single turn in a conversation. It maps from
// an ADK Event and carries the role, textual content, and an estimated or
// exact token count for that turn.
type ConversationTurn struct {
	Role       string // "user" | "model"
	Content    string
	TokenCount int
}

// WorkerUsageMetadata mirrors the token-count fields from genai's
// GenerateContentResponseUsageMetadata so callers do not need to import genai
// in domain-layer code.
type WorkerUsageMetadata struct {
	PromptTokenCount     int32
	CandidatesTokenCount int32
	TotalTokenCount      int32
}

// CompressResult holds the outcome of a single compression run.
type CompressResult struct {
	CompressedText         string
	OriginalTokens         int
	CompressedTokens       int
	ActualCompressionRatio float64
	Cost                   float64
	WorkerUsage            WorkerUsageMetadata
}

// CompressStrategy is the pluggable compression policy interface.
// Implementations must be stateless — no mutable state may be held between calls.
// All I/O is injected via parameters; implementations must not open network
// connections or files directly.
type CompressStrategy interface {
	// Name returns the strategy identifier used in configuration and error messages.
	Name() string

	// SelectCandidates picks the turns to compress from activeTurns.
	// It must never panic on empty or short slices.
	SelectCandidates(activeTurns []ConversationTurn, targetReclaimTokens int) []ConversationTurn

	// Compress compresses candidates using an isolated worker.
	// existingSummary from a prior cycle must be woven into the prompt so context
	// is not lost across multiple compression cycles.
	// Returns (nil, error) on any worker failure — never a partial result.
	Compress(ctx context.Context, candidates []ConversationTurn, existingSummary string, profile ModelProfile) (*CompressResult, error)
}

// ---- Generational strategy ----

// GenerationalConfig holds configuration for the Generational strategy.
type GenerationalConfig struct {
	// OldestN is the number of oldest turns to select per compression cycle.
	// Defaults to 5.
	OldestN int

	// PromptStore is a dotprompt source loader used to load and render the
	// summarize prompt. In production, pass a *dp.DirStore pointing at the
	// prompts/ directory. In tests, pass an in-memory implementation so no
	// filesystem access is needed.
	PromptStore dp.PromptStore
}

// defaultGenerationalConfig returns a GenerationalConfig with safe defaults.
// The PromptStore is nil — callers must inject a real store before use, or
// use NewGenerational which wires up the production DirStore.
func defaultGenerationalConfig() GenerationalConfig {
	return GenerationalConfig{
		OldestN: 5,
	}
}

// compressWorker is the interface satisfied by the real genai worker and by
// test mocks. It is kept package-private so callers outside internal/memory
// never depend on it directly.
type compressWorker interface {
	Summarize(ctx context.Context, model, prompt string) (string, *WorkerUsageMetadata, error)
}

// Generational is the MVP CompressStrategy: select the oldest N turns and
// compress them using an isolated compress worker (LLM call via genai).
type Generational struct {
	cfg    GenerationalConfig
	worker compressWorker // injectable for testing; set by NewGenerational
}

// NewGenerational constructs a Generational strategy using a real genai-backed worker.
// If cfg.OldestN is zero the default of 5 is applied. If cfg.PromptStore is nil,
// a DirStore pointed at the prompts/ directory relative to the current working
// directory is used.
func NewGenerational(cfg GenerationalConfig, worker compressWorker) *Generational {
	if cfg.OldestN <= 0 {
		cfg.OldestN = 5
	}
	if cfg.PromptStore == nil {
		store, err := dp.NewDirStore("prompts")
		if err != nil {
			// DirStore creation only fails on absolute path resolution; fall back
			// gracefully — buildPrompt will return an error at render time.
			store = nil
		}
		cfg.PromptStore = store
	}
	return &Generational{cfg: cfg, worker: worker}
}

// Name returns "generational".
func (g *Generational) Name() string { return "generational" }

// SelectCandidates returns the min(OldestN, len(activeTurns)) oldest turns in
// original order. Never panics on empty or short slices.
func (g *Generational) SelectCandidates(activeTurns []ConversationTurn, _ int) []ConversationTurn {
	n := g.cfg.OldestN
	if n > len(activeTurns) {
		n = len(activeTurns)
	}
	if n == 0 {
		return []ConversationTurn{}
	}
	// Allocate a new backing array and copy the first n turns into it.
	// Mutations to the returned slice do not affect activeTurns.
	result := make([]ConversationTurn, n)
	copy(result, activeTurns[:n])
	return result
}

// formatTurns renders conversation turns as a plain text block for use as the
// "turns" input variable to the dotprompt template.
func formatTurns(turns []ConversationTurn) string {
	var sb strings.Builder
	for _, t := range turns {
		sb.WriteString("[")
		sb.WriteString(t.Role)
		sb.WriteString("]: ")
		sb.WriteString(t.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildPrompt loads the summarize prompt from the PromptStore, renders it with
// the given existingSummary and turns, and returns the rendered text.
// Returns (string, error) — never falls back silently on load or render failure.
func (g *Generational) buildPrompt(existingSummary string, turns []ConversationTurn) (string, error) {
	if g.cfg.PromptStore == nil {
		return "", fmt.Errorf("buildPrompt: PromptStore is nil — no prompt source available")
	}

	promptData, err := g.cfg.PromptStore.Load("summarize", dp.LoadPromptOptions{})
	if err != nil {
		return "", fmt.Errorf("buildPrompt: failed to load summarize prompt: %w", err)
	}

	engine := dp.NewDotprompt(nil)
	promptFunc, err := engine.Compile(promptData.Source, nil)
	if err != nil {
		return "", fmt.Errorf("buildPrompt: failed to compile prompt template: %w", err)
	}

	formattedTurns := formatTurns(turns)
	if formattedTurns == "" {
		return "", fmt.Errorf("buildPrompt: turns must not be empty")
	}

	input := map[string]any{
		"turns": formattedTurns,
	}
	if existingSummary != "" {
		input["existingSummary"] = existingSummary
	}

	rendered, err := promptFunc(&dp.DataArgument{Input: input}, nil)
	if err != nil {
		return "", fmt.Errorf("buildPrompt: failed to render prompt: %w", err)
	}

	// Extract text from rendered messages. dotprompt renders the template body
	// as a single user-role message with one or more TextPart content items.
	var sb strings.Builder
	for _, msg := range rendered.Messages {
		for _, part := range msg.Content {
			if tp, ok := part.(*dp.TextPart); ok {
				sb.WriteString(tp.Text)
			}
		}
	}
	return sb.String(), nil
}

// Compress invokes the compress worker to summarize candidates.
// The effective model is determined by profile.GetEffectiveCompressModelID().
// Returns (nil, error) if buildPrompt or the worker fails — never a partial CompressResult.
func (g *Generational) Compress(ctx context.Context, candidates []ConversationTurn, existingSummary string, profile ModelProfile) (*CompressResult, error) {
	if g.worker == nil {
		return nil, fmt.Errorf("compress: worker is nil — inject a compressWorker via NewGenerational before calling Compress")
	}

	modelID := profile.GetEffectiveCompressModelID()
	prompt, err := g.buildPrompt(existingSummary, candidates)
	if err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}

	// Count original tokens from candidate turns.
	originalTokens := 0
	for _, t := range candidates {
		originalTokens += t.TokenCount
	}

	summaryText, usage, err := g.worker.Summarize(ctx, modelID, prompt)
	if err != nil {
		return nil, fmt.Errorf("compress worker failed: %w", err)
	}

	// Estimate compressed token count from the response metadata.
	compressedTokens := 0
	if usage != nil {
		compressedTokens = int(usage.CandidatesTokenCount)
	}

	var ratio float64
	if originalTokens > 0 {
		ratio = float64(compressedTokens) / float64(originalTokens)
	}

	result := &CompressResult{
		CompressedText:         summaryText,
		OriginalTokens:         originalTokens,
		CompressedTokens:       compressedTokens,
		ActualCompressionRatio: ratio,
		Cost:                   0, // Cost calculation is out of scope for MVP
	}
	if usage != nil {
		result.WorkerUsage = *usage
	}

	return result, nil
}

// ---- Strategy registry ----

// StrategyFactory is a constructor function that returns a new CompressStrategy.
type StrategyFactory func() CompressStrategy

// StrategyRegistry maps strategy name strings to factory functions.
// Unknown names produce a descriptive error listing all registered names.
type StrategyRegistry struct {
	factories map[string]StrategyFactory
}

// NewStrategyRegistry constructs a registry pre-loaded with the built-in
// "generational" strategy (default GenerationalConfig, nil worker — callers
// must set a real worker before using in production via NewGenerational).
func NewStrategyRegistry() *StrategyRegistry {
	r := &StrategyRegistry{
		factories: make(map[string]StrategyFactory),
	}
	r.Register("generational", func() CompressStrategy {
		return NewGenerational(defaultGenerationalConfig(), nil)
	})
	return r
}

// Register adds or replaces a factory under the given name.
func (r *StrategyRegistry) Register(name string, factory StrategyFactory) {
	r.factories[name] = factory
}

// Resolve returns the CompressStrategy for name, or an error listing all
// available strategy names if the name is not registered.
func (r *StrategyRegistry) Resolve(name string) (CompressStrategy, error) {
	factory, ok := r.factories[name]
	if !ok {
		names := make([]string, 0, len(r.factories))
		for k := range r.factories {
			names = append(names, k)
		}
		return nil, fmt.Errorf("unknown strategy: %s; available: %s", name, strings.Join(names, ", "))
	}
	return factory(), nil
}

// ---- Real genai-backed worker ----

// GenaiWorker is the production compressWorker that calls the Gemini API via
// google.golang.org/genai. It creates a fresh, isolated API call per Summarize
// invocation — there is no shared session state, so it never pollutes the main
// agent's context window.
type GenaiWorker struct {
	client *genai.Client
}

// NewGenaiWorker wraps a *genai.Client for use as the compress worker.
func NewGenaiWorker(client *genai.Client) *GenaiWorker {
	return &GenaiWorker{client: client}
}

// Summarize sends prompt to the model identified by modelName and returns the
// generated text along with token-usage figures. Returns (_, nil, error) on
// any API or response-parsing failure; the caller (Generational.Compress) is
// responsible for propagating the error as (nil, error).
func (w *GenaiWorker) Summarize(ctx context.Context, modelName, prompt string) (string, *WorkerUsageMetadata, error) {
	contents := []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{Text: prompt},
			},
		},
	}

	resp, err := w.client.Models.GenerateContent(ctx, modelName, contents, nil)
	if err != nil {
		return "", nil, fmt.Errorf("genai GenerateContent failed for model %q: %w", modelName, err)
	}

	text := resp.Text()

	var usage WorkerUsageMetadata
	if resp.UsageMetadata != nil {
		usage = WorkerUsageMetadata{
			PromptTokenCount:     resp.UsageMetadata.PromptTokenCount,
			CandidatesTokenCount: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokenCount:      resp.UsageMetadata.TotalTokenCount,
		}
	}

	return text, &usage, nil
}
