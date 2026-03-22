package memory

import (
	"context"
	"fmt"
	"strings"

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

// ForkRequest represents a forked conversation snapshot for compression.
// Like a process fork, it captures the current subsession's memory image
// (system prompt + conversation history) and appends a summarization instruction.
//
// For generation 0→1: the fork contains the original conversation turns.
// For generation N→N+1: the fork contains the subsession view (prior summary +
// recent turns since last compression), NOT the full session history.
type ForkRequest struct {
	// SystemInstruction is the agent's system prompt (from the parent process).
	SystemInstruction *genai.Content

	// History is the conversation turns to summarize. For the first fork this
	// is the original turns; for subsequent forks this is the subsession view
	// (summary + recent active turns).
	History []*genai.Content
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

	// Compress forks the current subsession and asks the compress worker to
	// summarize it. The ForkRequest carries the system instruction and the
	// conversation history of the subsession being compressed.
	// Returns (nil, error) on any worker failure — never a partial result.
	Compress(ctx context.Context, fork *ForkRequest, profile ModelProfile) (*CompressResult, error)
}

// ---- Generational strategy ----

// GenerationalConfig holds configuration for the Generational strategy.
type GenerationalConfig struct {
	// TurnsToKeep is the number of most recent turns to preserve uncompressed.
	// All older turns are selected as compression candidates.
	// When zero, it is computed as ContextWindowTokens / MaxOutputTokens from
	// the ModelProfile, giving larger context windows more preserved turns.
	TurnsToKeep int

	// SummarizeInstruction is the user-facing instruction appended to the forked
	// conversation to request a summary. Defaults to defaultSummarizeInstruction.
	SummarizeInstruction string
}

// defaultSummarizeInstruction is the handover instruction appended as a user
// message to the forked conversation. It frames compression as a shift handover:
// the current subsession is ending, and the summary must equip the next
// subsession to continue seamlessly.
//
// Design rationale (Prompt Architect four-pillar framework):
//   - Persona: senior project manager performing shift handover
//   - Task: produce a structured Context Handover Document
//   - Context: the next AI instance has zero prior knowledge
//   - Format: 8 mandatory sections, skip empty sections, no filler
const defaultSummarizeInstruction = `You are a senior project manager performing a shift handover. Your task is to review this entire conversation and produce a Context Handover Document.

Purpose: a brand-new AI instance with zero knowledge of this conversation must be able to continue seamlessly using only this document, without the user needing to re-explain anything.

Write the document using exactly these sections. Skip any section that has no content — do not include empty placeholders.

## Task Charter
- Core objective: what is the user ultimately trying to achieve?
- In scope: what is included in this task
- Out of scope: what has been explicitly excluded
- Success criteria: how do we know the task is done?

## Stakeholder Profile
- Expertise and domain knowledge level
- Communication preferences (language, style, level of detail)
- Important personal context that affects how to assist them

## Current Status
- Overall progress (e.g. phase 2/5, or percentage)
- Last completed milestone
- What is currently blocked or in progress

## Deliverables Log
List all outputs produced, with status and key content summary:
- [done] item: summary...
- [in progress] item: current state...

## Decision Log
Record every significant decision AND the reasoning behind it — the reasoning is the most important part:
- Chose X over Y because...
- Abandoned Z because...

## Open Issues
Unresolved items that need continued attention:
- Issue: ... | Status: awaiting user input / needs more info / in progress

## Constraints & Lessons Learned
This is the most critical section. Record what went wrong and what must not be repeated:
- Failed approach: tried X, it did not work because...
- Hard constraint: ...
- User explicitly emphasized: ...

## Next Actions
What the next shift should do immediately upon taking over:
1. First action: ...
2. Awaiting user input on: ... (if any)

Rules: be concise. Omit pleasantries, reasoning chains, and redundant information. Every sentence must earn its place.`

// compressWorker is the interface satisfied by the real genai worker and by
// test mocks. It is kept package-private so callers outside internal/memory
// never depend on it directly.
type compressWorker interface {
	Summarize(ctx context.Context, model string, contents []*genai.Content) (string, *WorkerUsageMetadata, error)
}

// Generational is the MVP CompressStrategy: select the oldest N turns and
// compress them using an isolated compress worker (LLM call via genai).
type Generational struct {
	cfg    GenerationalConfig
	worker compressWorker // injectable for testing; set by NewGenerational
}

// NewGenerational constructs a Generational strategy using a real genai-backed worker.
// If cfg.TurnsToKeep is zero, it is computed from the profile as
// ContextWindowTokens / MaxOutputTokens (capped at a minimum of 2).
// If cfg.SummarizeInstruction is empty the default instruction is used.
func NewGenerational(cfg GenerationalConfig, worker compressWorker, profile ModelProfile) *Generational {
	if cfg.TurnsToKeep <= 0 && profile.MaxOutputTokens > 0 {
		cfg.TurnsToKeep = profile.ContextWindowTokens / profile.MaxOutputTokens
	}
	if cfg.TurnsToKeep < 2 {
		cfg.TurnsToKeep = 2
	}
	if cfg.SummarizeInstruction == "" {
		cfg.SummarizeInstruction = defaultSummarizeInstruction
	}
	return &Generational{cfg: cfg, worker: worker}
}

// Name returns "generational".
func (g *Generational) Name() string { return "generational" }

// SelectCandidates keeps the most recent TurnsToKeep turns and returns all
// older turns as compression candidates. Never panics on empty or short slices.
func (g *Generational) SelectCandidates(activeTurns []ConversationTurn, _ int) []ConversationTurn {
	toCompress := len(activeTurns) - g.cfg.TurnsToKeep
	if toCompress <= 0 {
		return []ConversationTurn{}
	}
	// Allocate a new backing array and copy the oldest turns into it.
	// Mutations to the returned slice do not affect activeTurns.
	result := make([]ConversationTurn, toCompress)
	copy(result, activeTurns[:toCompress])
	return result
}

// buildForkContents assembles the forked conversation: system instruction +
// subsession history + summarize instruction as the final user turn.
// This is the "child process image" that the compress worker will execute.
func (g *Generational) buildForkContents(fork *ForkRequest) []*genai.Content {
	var contents []*genai.Content

	// System instruction (from the parent process).
	if fork.SystemInstruction != nil {
		contents = append(contents, fork.SystemInstruction)
	}

	// Subsession history — the conversation turns being compressed.
	contents = append(contents, fork.History...)

	// Summarize instruction — the "exec" in the forked process.
	contents = append(contents, &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: g.cfg.SummarizeInstruction}},
	})

	return contents
}

// Compress forks the current subsession and asks the compress worker to
// summarize it. The fork carries the system instruction and conversation
// history, preserving the full multi-turn structure instead of flattening
// to plain text.
//
// The effective model is determined by profile.GetEffectiveCompressModelID().
// Returns (nil, error) if the worker fails — never a partial CompressResult.
func (g *Generational) Compress(ctx context.Context, fork *ForkRequest, profile ModelProfile) (*CompressResult, error) {
	if g.worker == nil {
		return nil, fmt.Errorf("compress: worker is nil — inject a compressWorker via NewGenerational before calling Compress")
	}
	if fork == nil || len(fork.History) == 0 {
		return nil, fmt.Errorf("compress: fork has no history to compress")
	}

	modelID := profile.GetEffectiveCompressModelID()
	contents := g.buildForkContents(fork)

	summaryText, usage, err := g.worker.Summarize(ctx, modelID, contents)
	if err != nil {
		return nil, fmt.Errorf("compress worker failed: %w", err)
	}

	// Estimate compressed token count from the response metadata.
	compressedTokens := 0
	if usage != nil {
		compressedTokens = int(usage.CandidatesTokenCount)
	}

	// OriginalTokens is estimated from prompt token count (includes system +
	// history + instruction). Not exact per-candidate, but directionally correct.
	originalTokens := 0
	if usage != nil {
		originalTokens = int(usage.PromptTokenCount)
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
		return NewGenerational(GenerationalConfig{}, nil, ModelProfile{})
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

// Summarize sends the forked conversation contents to the model and returns the
// generated summary text along with token-usage figures. The contents include
// the system instruction, conversation history, and summarize instruction as
// structured multi-turn content — preserving the full conversation shape.
func (w *GenaiWorker) Summarize(ctx context.Context, modelName string, contents []*genai.Content) (string, *WorkerUsageMetadata, error) {
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
