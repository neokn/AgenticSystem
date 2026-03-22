package memory

import (
	"testing"
)

// Task 1: ModelProfile struct definition tests

func TestModelProfile_HasAllRequiredFields(t *testing.T) {
	// Arrange
	p := ModelProfile{
		ModelID:                      "gemini-2.0-flash",
		Provider:                     "google",
		ContextWindowTokens:          1048576,
		MaxOutputTokens:              8192,
		CostPer1KInputTokens:         0.0,
		CostPer1KOutputTokens:        0.0,
		CompressModelID:              "",
		CompressCostPer1KInputTokens: 0.0,
		CompressCostPer1KOutputTokens: 0.0,
	}

	// Act & Assert
	if p.ModelID != "gemini-2.0-flash" {
		t.Errorf("expected ModelID gemini-2.0-flash, got %s", p.ModelID)
	}
	if p.Provider != "google" {
		t.Errorf("expected Provider google, got %s", p.Provider)
	}
	if p.ContextWindowTokens != 1048576 {
		t.Errorf("expected ContextWindowTokens 1048576, got %d", p.ContextWindowTokens)
	}
	if p.MaxOutputTokens != 8192 {
		t.Errorf("expected MaxOutputTokens 8192, got %d", p.MaxOutputTokens)
	}
}

func TestModelProfile_PassedByValue_IsImmutable(t *testing.T) {
	// Arrange — value object: modifying a copy does not affect original
	original := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		Provider:            "google",
		ContextWindowTokens: 1048576,
		MaxOutputTokens:     8192,
	}

	// Act — copy and mutate
	copy := original
	copy.ModelID = "mutated"

	// Assert — original unchanged
	if original.ModelID != "gemini-2.0-flash" {
		t.Errorf("original ModelID was mutated: got %s", original.ModelID)
	}
}
