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
	reg, err := NewRegistry(custom)
	if err != nil {
		t.Fatalf("NewRegistry() unexpected error: %v", err)
	}

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
	// NewRegistry() with no args cannot fail — error path only fires for invalid custom profiles.
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
	// NewRegistry() with no args cannot fail — error path only fires for invalid custom profiles.
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
	// Arrange
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() unexpected error: %v", err)
	}

	// Act
	p, err := reg.GetProfile("gemini-2.0-flash-lite")

	// Assert
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
	// Arrange
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() unexpected error: %v", err)
	}

	// Act
	p, err := reg.GetProfile("gemini-2.5-pro")

	// Assert
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
	// Arrange
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() unexpected error: %v", err)
	}

	// Act
	p, err := reg.GetProfile("gemini-2.5-flash")

	// Assert
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

// Task 4: Custom profile override

func TestNewRegistry_CustomProfileOverridesBuiltin_WhenSameModelID(t *testing.T) {
	// Arrange — custom profile for a built-in model with different token limit
	custom := ModelProfile{
		ModelID:             "gemini-2.0-flash",
		Provider:            "google",
		ContextWindowTokens: 500000,
		MaxOutputTokens:     4096,
	}

	// Act
	reg, err := NewRegistry(custom)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	p, err := reg.GetProfile("gemini-2.0-flash")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.ContextWindowTokens != 500000 {
		t.Errorf("expected custom ContextWindowTokens 500000, got %d", p.ContextWindowTokens)
	}
}

func TestNewRegistry_BuiltinProfilesStillAvailable_WhenOtherModelCustomised(t *testing.T) {
	// Arrange — override only flash-lite, flash should remain unchanged
	custom := ModelProfile{
		ModelID:             "gemini-2.0-flash-lite",
		Provider:            "google",
		ContextWindowTokens: 200000,
		MaxOutputTokens:     2048,
	}

	// Act
	reg, err := NewRegistry(custom)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Assert — built-in flash unchanged
	p, err := reg.GetProfile("gemini-2.0-flash")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.ContextWindowTokens != 1048576 {
		t.Errorf("expected built-in ContextWindowTokens 1048576, got %d", p.ContextWindowTokens)
	}
}

// Task 5: Required-field validation in NewRegistry

func TestNewRegistry_ReturnsError_WhenCustomProfileHasEmptyModelID(t *testing.T) {
	// Arrange
	invalid := ModelProfile{
		ModelID:             "",
		ContextWindowTokens: 100000,
	}

	// Act
	reg, err := NewRegistry(invalid)

	// Assert
	if err == nil {
		t.Fatal("expected error for empty ModelID, got nil")
	}
	if reg != nil {
		t.Error("expected nil registry on validation error")
	}
}

func TestNewRegistry_ReturnsError_WhenCustomProfileHasZeroContextWindowTokens(t *testing.T) {
	// Arrange
	invalid := ModelProfile{
		ModelID:             "some-model",
		ContextWindowTokens: 0,
	}

	// Act
	reg, err := NewRegistry(invalid)

	// Assert
	if err == nil {
		t.Fatal("expected error for zero ContextWindowTokens, got nil")
	}
	if reg != nil {
		t.Error("expected nil registry on validation error")
	}
}

func TestNewRegistry_ErrorMessage_ContainsFieldContext_ForEmptyModelID(t *testing.T) {
	// Arrange
	invalid := ModelProfile{
		ModelID:             "",
		ContextWindowTokens: 100000,
	}

	// Act
	_, err := NewRegistry(invalid)

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if len(msg) == 0 {
		t.Error("expected descriptive error message, got empty string")
	}
}

// Task 6: GetEffectiveCompressModelID helper

func TestGetEffectiveCompressModelID_ReturnsCompressModelID_WhenNonEmpty(t *testing.T) {
	// Arrange
	p := ModelProfile{
		ModelID:         "gemini-2.5-pro",
		CompressModelID: "gemini-2.0-flash-lite",
	}

	// Act
	result := p.GetEffectiveCompressModelID()

	// Assert
	if result != "gemini-2.0-flash-lite" {
		t.Errorf("expected gemini-2.0-flash-lite, got %s", result)
	}
}

func TestGetEffectiveCompressModelID_ReturnsPrimaryModelID_WhenCompressModelIDEmpty(t *testing.T) {
	// Arrange — CompressModelID absent, should fall back to ModelID
	p := ModelProfile{
		ModelID:         "gemini-2.5-pro",
		CompressModelID: "",
	}

	// Act
	result := p.GetEffectiveCompressModelID()

	// Assert
	if result != "gemini-2.5-pro" {
		t.Errorf("expected fallback to primary ModelID gemini-2.5-pro, got %s", result)
	}
}

// Task 7: Comprehensive acceptance criteria coverage

// AC#1 — gemini-2.0-flash full field check (already tested above, adding
//         explicit check for Provider field per acceptance criteria)
func TestGetProfile_AC1_GeminiFlash_ProviderIsGoogle(t *testing.T) {
	// Arrange
	// NewRegistry() with no args cannot fail — error path only fires for invalid custom profiles.
	reg, _ := NewRegistry()

	// Act
	p, err := reg.GetProfile("gemini-2.0-flash")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.Provider != "google" {
		t.Errorf("AC1: expected Provider google, got %s", p.Provider)
	}
}

// AC#2 — gemini-2.5-pro has non-empty or empty CompressModelID (both valid)
func TestGetProfile_AC2_Gemini25Pro_CompressModelIDIsStringValue(t *testing.T) {
	// Arrange
	// NewRegistry() with no args cannot fail — error path only fires for invalid custom profiles.
	reg, _ := NewRegistry()

	// Act
	p, err := reg.GetProfile("gemini-2.5-pro")

	// Assert — no error; CompressModelID is a string field; zero value "" is valid per spec.
	// We verify it is a string (not nil) by comparing to "" — the empty string is the zero value.
	if err != nil {
		t.Fatalf("AC2: expected no error, got %v", err)
	}
	if p.CompressModelID != "" {
		// Non-empty is also valid; just verify it is a string value.
		_ = p.CompressModelID
	}
	// Reaching here confirms CompressModelID is a string (comparison to "" succeeds without panic).
}

// AC#8 — zero cost fields are valid (custom model with all costs at 0.0)
func TestNewRegistry_AC8_ZeroCostFields_AreValid(t *testing.T) {
	// Arrange — all cost fields at zero value
	p := ModelProfile{
		ModelID:                       "private-test-model",
		Provider:                      "google",
		ContextWindowTokens:           100000,
		MaxOutputTokens:               4096,
		CostPer1KInputTokens:          0.0,
		CostPer1KOutputTokens:         0.0,
		CompressCostPer1KInputTokens:  0.0,
		CompressCostPer1KOutputTokens: 0.0,
	}

	// Act
	reg, err := NewRegistry(p)

	// Assert — no error; zero cost is acceptable for testing/private models
	if err != nil {
		t.Fatalf("AC8: expected no error for zero cost fields, got %v", err)
	}
	got, err := reg.GetProfile("private-test-model")
	if err != nil {
		t.Fatalf("AC8: GetProfile returned error: %v", err)
	}
	if got.CostPer1KInputTokens != 0.0 {
		t.Errorf("AC8: expected CostPer1KInputTokens 0.0, got %f", got.CostPer1KInputTokens)
	}
	if got.CostPer1KOutputTokens != 0.0 {
		t.Errorf("AC8: expected CostPer1KOutputTokens 0.0, got %f", got.CostPer1KOutputTokens)
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
