// Package memory implements the Context Window Memory Manager for AgenticSystem.
// It provides model profile lookup, memory layout calculation, compression
// triggering, and observability for Gemini-based ADK agents.
package memory

import (
	"errors"
	"fmt"
	"log/slog"
)

// ErrModelNotFound is returned by GetProfile when the requested model ID is not
// in the registry. Callers may use errors.Is(err, memory.ErrModelNotFound) to
// branch on this condition at runtime.
var ErrModelNotFound = errors.New("model not found")

// ModelProfile is an immutable value object describing a model's hardware
// capabilities and associated cost parameters. Pass by value — never by pointer.
// Two profiles with identical fields are considered equivalent.
type ModelProfile struct {
	ModelID             string
	Provider            string // "google" for all Gemini models
	ContextWindowTokens int
	MaxOutputTokens     int

	// Cost per 1,000 input/output tokens for the primary model.
	CostPer1KInputTokens  float64
	CostPer1KOutputTokens float64

	// Optional: independent compress-worker model configuration.
	// When CompressModelID is empty, callers should fall back to ModelID.
	CompressModelID               string
	CompressCostPer1KInputTokens  float64
	CompressCostPer1KOutputTokens float64
}

// Registry is a lookup table of ModelProfile values, initialized once at startup.
// Built-in profiles are merged with caller-supplied custom profiles; custom profiles
// take precedence over built-ins with the same ModelID.
// Construct with NewRegistry — never use a zero-value Registry.
type Registry struct {
	profiles map[string]ModelProfile
}

// NewRegistry constructs a Registry pre-loaded with the four built-in Gemini
// profiles. Any customProfiles provided override built-ins with the same ModelID.
// Returns an error if any custom profile fails required-field validation.
func NewRegistry(customProfiles ...ModelProfile) (*Registry, error) {
	// Validate all custom profiles before touching any state.
	for i, p := range customProfiles {
		if p.ModelID == "" {
			return nil, fmt.Errorf("NewRegistry: ModelID is required for profile at index %d", i)
		}
		if p.ContextWindowTokens == 0 {
			return nil, fmt.Errorf("NewRegistry: ContextWindowTokens is required for profile %q at index %d", p.ModelID, i)
		}
	}

	m := make(map[string]ModelProfile, len(builtinProfiles)+len(customProfiles))
	for _, p := range builtinProfiles {
		m[p.ModelID] = p
	}
	for _, p := range customProfiles {
		m[p.ModelID] = p
	}

	return &Registry{profiles: m}, nil
}

// builtinProfiles is the package-level list of built-in Gemini model profiles.
// Declared as a composite literal (not init()) to keep data visible in tests
// and to avoid init() ordering surprises.
var builtinProfiles = []ModelProfile{
	{
		ModelID:             "gemini-2.0-flash",
		Provider:            "google",
		ContextWindowTokens: 1048576,
		MaxOutputTokens:     8192,
	},
	{
		ModelID:             "gemini-2.0-flash-lite",
		Provider:            "google",
		ContextWindowTokens: 1048576,
		MaxOutputTokens:     8192,
	},
	{
		ModelID:             "gemini-2.5-pro",
		Provider:            "google",
		ContextWindowTokens: 1048576,
		MaxOutputTokens:     65536,
	},
	{
		ModelID:             "gemini-2.5-flash",
		Provider:            "google",
		ContextWindowTokens: 1048576,
		MaxOutputTokens:     65536,
	},
	{
		ModelID:             "gemini-3-flash-preview",
		Provider:            "google",
		ContextWindowTokens: 1048576,
		MaxOutputTokens:     65536,
	},
	{
		ModelID:             "gemini-3.1-flash-lite-preview",
		Provider:            "google",
		ContextWindowTokens: 1048576,
		MaxOutputTokens:     65536,
	},
}

// GetEffectiveCompressModelID returns the model ID to use for the compress worker.
// If CompressModelID is set, it is returned unchanged.
// If CompressModelID is empty, the primary ModelID is returned and a warning is
// emitted via slog so operators can see the fallback in logs.
// This helper carries no error return — the fallback is valid behaviour, not a fault.
func (p ModelProfile) GetEffectiveCompressModelID() string {
	if p.CompressModelID != "" {
		return p.CompressModelID
	}
	slog.Warn("CompressModelID not set; falling back to primary model",
		"primaryModelID", p.ModelID,
	)
	return p.ModelID
}

// GetProfile returns the ModelProfile for the given model ID.
// Returns ErrModelNotFound if the model ID is not in the registry.
func (r *Registry) GetProfile(modelID string) (ModelProfile, error) {
	p, ok := r.profiles[modelID]
	if !ok {
		return ModelProfile{}, fmt.Errorf("%w: %q", ErrModelNotFound, modelID)
	}
	return p, nil
}
