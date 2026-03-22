package memory

import (
	"errors"
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

// Task 2: Registry type, NewRegistry, GetProfile

func TestNewRegistry_ReturnsNonNilRegistry_WhenNoCustomProfiles(t *testing.T) {
	// Arrange / Act
	reg, err := NewRegistry()

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestGetProfile_ReturnsProfile_WhenCustomProfileRegistered(t *testing.T) {
	// Arrange — use a custom profile so test is independent of built-in data
	custom := ModelProfile{
		ModelID:             "custom-model",
		Provider:            "google",
		ContextWindowTokens: 100000,
		MaxOutputTokens:     4096,
	}
	reg, _ := NewRegistry(custom)

	// Act
	profile, err := reg.GetProfile("custom-model")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if profile.ModelID != "custom-model" {
		t.Errorf("expected ModelID custom-model, got %s", profile.ModelID)
	}
}

func TestGetProfile_ReturnsErrModelNotFound_WhenModelIDUnknown(t *testing.T) {
	// Arrange
	reg, _ := NewRegistry()

	// Act
	_, err := reg.GetProfile("gpt-4o")

	// Assert
	if err == nil {
		t.Fatal("expected error for unknown model, got nil")
	}
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("expected ErrModelNotFound, got %v", err)
	}
}

// Task 3: Built-in Gemini profiles

func TestGetProfile_ReturnsBuiltin_GeminiFlash(t *testing.T) {
	// Arrange
	reg, _ := NewRegistry()

	// Act
	p, err := reg.GetProfile("gemini-2.0-flash")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.ModelID != "gemini-2.0-flash" {
		t.Errorf("ModelID: expected gemini-2.0-flash, got %s", p.ModelID)
	}
	if p.Provider != "google" {
		t.Errorf("Provider: expected google, got %s", p.Provider)
	}
	if p.ContextWindowTokens != 1048576 {
		t.Errorf("ContextWindowTokens: expected 1048576, got %d", p.ContextWindowTokens)
	}
	if p.MaxOutputTokens != 8192 {
		t.Errorf("MaxOutputTokens: expected 8192, got %d", p.MaxOutputTokens)
	}
}

func TestGetProfile_ReturnsBuiltin_GeminiFlashLite(t *testing.T) {
	reg, _ := NewRegistry()
	p, err := reg.GetProfile("gemini-2.0-flash-lite")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.ContextWindowTokens != 1048576 {
		t.Errorf("ContextWindowTokens: expected 1048576, got %d", p.ContextWindowTokens)
	}
	if p.MaxOutputTokens != 8192 {
		t.Errorf("MaxOutputTokens: expected 8192, got %d", p.MaxOutputTokens)
	}
}

func TestGetProfile_ReturnsBuiltin_Gemini25Pro(t *testing.T) {
	reg, _ := NewRegistry()
	p, err := reg.GetProfile("gemini-2.5-pro")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.ContextWindowTokens != 1048576 {
		t.Errorf("ContextWindowTokens: expected 1048576, got %d", p.ContextWindowTokens)
	}
	if p.MaxOutputTokens != 65536 {
		t.Errorf("MaxOutputTokens: expected 65536, got %d", p.MaxOutputTokens)
	}
}

func TestGetProfile_ReturnsBuiltin_Gemini25Flash(t *testing.T) {
	reg, _ := NewRegistry()
	p, err := reg.GetProfile("gemini-2.5-flash")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.ContextWindowTokens != 1048576 {
		t.Errorf("ContextWindowTokens: expected 1048576, got %d", p.ContextWindowTokens)
	}
	if p.MaxOutputTokens != 65536 {
		t.Errorf("MaxOutputTokens: expected 65536, got %d", p.MaxOutputTokens)
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
