// Package memory implements the Context Window Memory Manager for AgenticSystem.
// It provides model profile lookup, memory layout calculation, compression
// triggering, and observability for Gemini-based ADK agents.
package memory

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
	CompressModelID              string
	CompressCostPer1KInputTokens  float64
	CompressCostPer1KOutputTokens float64
}
