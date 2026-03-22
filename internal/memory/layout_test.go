package memory

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Task 1 — MemoryLayout struct, unexported fields, accessor methods
// ---------------------------------------------------------------------------

func TestMemoryLayout_should_return_segment_values_via_accessors(t *testing.T) {
	// Arrange: construct directly via struct literal (within package — fields accessible)
	l := MemoryLayout{pinned: 100, summary: 200, active: 300, buffer: 400}

	// Act + Assert
	if got := l.Pinned(); got != 100 {
		t.Errorf("Pinned() = %d, want 100", got)
	}
	if got := l.Summary(); got != 200 {
		t.Errorf("Summary() = %d, want 200", got)
	}
	if got := l.Active(); got != 300 {
		t.Errorf("Active() = %d, want 300", got)
	}
	if got := l.Buffer(); got != 400 {
		t.Errorf("Buffer() = %d, want 400", got)
	}
}

// ---------------------------------------------------------------------------
// Task 2 — LayoutConfig struct
// ---------------------------------------------------------------------------

func TestLayoutConfig_should_hold_per_segment_ratios(t *testing.T) {
	// Arrange + Act
	cfg := LayoutConfig{
		PinnedRatio:  0.15,
		SummaryRatio: 0.25,
		ActiveRatio:  0.50,
		BufferRatio:  0.10,
	}

	// Assert: fields accessible and hold correct values
	if cfg.PinnedRatio != 0.15 {
		t.Errorf("PinnedRatio = %v, want 0.15", cfg.PinnedRatio)
	}
	if cfg.SummaryRatio != 0.25 {
		t.Errorf("SummaryRatio = %v, want 0.25", cfg.SummaryRatio)
	}
	if cfg.ActiveRatio != 0.50 {
		t.Errorf("ActiveRatio = %v, want 0.50", cfg.ActiveRatio)
	}
	if cfg.BufferRatio != 0.10 {
		t.Errorf("BufferRatio = %v, want 0.10", cfg.BufferRatio)
	}
}

// ---------------------------------------------------------------------------
// Task 3 — DefaultLayoutConfig loads from configs/default.json
// ---------------------------------------------------------------------------

func TestDefaultLayoutConfig_should_load_ratios_from_default_json(t *testing.T) {
	// Act
	cfg, err := DefaultLayoutConfig()

	// Assert
	if err != nil {
		t.Fatalf("DefaultLayoutConfig() unexpected error: %v", err)
	}
	if cfg.PinnedRatio <= 0 {
		t.Errorf("PinnedRatio = %v, want > 0", cfg.PinnedRatio)
	}
	if cfg.SummaryRatio <= 0 {
		t.Errorf("SummaryRatio = %v, want > 0", cfg.SummaryRatio)
	}
	if cfg.ActiveRatio <= 0 {
		t.Errorf("ActiveRatio = %v, want > 0", cfg.ActiveRatio)
	}
	if cfg.BufferRatio <= 0 {
		t.Errorf("BufferRatio = %v, want > 0", cfg.BufferRatio)
	}
}

func TestDefaultLayoutConfig_should_sum_to_exactly_one(t *testing.T) {
	// Act
	cfg, err := DefaultLayoutConfig()
	if err != nil {
		t.Fatalf("DefaultLayoutConfig() unexpected error: %v", err)
	}

	// Assert
	sum := cfg.PinnedRatio + cfg.SummaryRatio + cfg.ActiveRatio + cfg.BufferRatio
	if sum < 0.9999 || sum > 1.0001 {
		t.Errorf("ratio sum = %v, want ~1.0", sum)
	}
}

// ---------------------------------------------------------------------------
// Task 4 — NewLayout ratio validation
// ---------------------------------------------------------------------------

func TestNewLayout_should_error_when_ratio_sum_is_not_100_percent(t *testing.T) {
	// Arrange: ratios sum to 1.05 (not 1.0)
	profile := ModelProfile{
		ContextWindowTokens: 1_048_576,
		MaxOutputTokens:     8_192,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.20,
		SummaryRatio: 0.25,
		ActiveRatio:  0.50,
		BufferRatio:  0.10, // sum = 1.05
	}

	// Act
	_, err := NewLayout(profile, cfg)

	// Assert
	if err == nil {
		t.Fatal("NewLayout() expected error for ratio sum != 1.0, got nil")
	}
}

func TestNewLayout_should_error_when_any_ratio_is_zero(t *testing.T) {
	// Arrange: PINNED ratio is zero
	profile := ModelProfile{
		ContextWindowTokens: 1_048_576,
		MaxOutputTokens:     8_192,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.00,
		SummaryRatio: 0.35,
		ActiveRatio:  0.55,
		BufferRatio:  0.10, // sum = 1.0
	}

	// Act
	_, err := NewLayout(profile, cfg)

	// Assert
	if err == nil {
		t.Fatal("NewLayout() expected error for zero ratio, got nil")
	}
}

func TestNewLayout_should_error_when_any_ratio_is_negative(t *testing.T) {
	// Arrange: SUMMARY ratio is negative
	profile := ModelProfile{
		ContextWindowTokens: 1_048_576,
		MaxOutputTokens:     8_192,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.20,
		SummaryRatio: -0.05,
		ActiveRatio:  0.75,
		BufferRatio:  0.10, // sum = 1.0
	}

	// Act
	_, err := NewLayout(profile, cfg)

	// Assert
	if err == nil {
		t.Fatal("NewLayout() expected error for negative ratio, got nil")
	}
}

// ---------------------------------------------------------------------------
// Task 5 — Base segment calculation (AC#1 happy path, AC#4 rounding invariant)
// ---------------------------------------------------------------------------

func TestNewLayout_should_compute_floor_segments_and_assign_remainder_to_active(t *testing.T) {
	// Arrange: gemini-2.0-flash, 15/25/50/10 ratios, 1,048,576 tokens
	profile := ModelProfile{
		ContextWindowTokens: 1_048_576,
		MaxOutputTokens:     8_192,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.15,
		SummaryRatio: 0.25,
		ActiveRatio:  0.50,
		BufferRatio:  0.10,
	}

	// Act
	layout, err := NewLayout(profile, cfg)

	// Assert
	if err != nil {
		t.Fatalf("NewLayout() unexpected error: %v", err)
	}

	// Each segment = floor(1_048_576 * ratio), remainder goes to active
	// pinned  = floor(1_048_576 * 0.15) = 157_286
	// summary = floor(1_048_576 * 0.25) = 262_144
	// buffer  = floor(1_048_576 * 0.10) = 104_857  (>= max_output_tokens=8192 — no raise needed)
	// active  = floor(1_048_576 * 0.50) = 524_288
	// sum of floors = 157_286 + 262_144 + 524_288 + 104_857 = 1_048_575
	// remainder = 1_048_576 - 1_048_575 = 1 → added to active
	// active final = 524_288 + 1 = 524_289
	wantPinned  := 157_286
	wantSummary := 262_144
	wantBuffer  := 104_857
	wantActive  := 524_289

	if layout.Pinned() != wantPinned {
		t.Errorf("Pinned() = %d, want %d", layout.Pinned(), wantPinned)
	}
	if layout.Summary() != wantSummary {
		t.Errorf("Summary() = %d, want %d", layout.Summary(), wantSummary)
	}
	if layout.Buffer() != wantBuffer {
		t.Errorf("Buffer() = %d, want %d", layout.Buffer(), wantBuffer)
	}
	if layout.Active() != wantActive {
		t.Errorf("Active() = %d, want %d", layout.Active(), wantActive)
	}
	if layout.Total() != profile.ContextWindowTokens {
		t.Errorf("Total() = %d, want %d", layout.Total(), profile.ContextWindowTokens)
	}
}

func TestNewLayout_should_sum_exactly_to_context_window_with_prime_like_total(t *testing.T) {
	// Arrange: context=1000, max_output=50 — non-integer splits
	profile := ModelProfile{
		ContextWindowTokens: 1_000,
		MaxOutputTokens:     50,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.15,
		SummaryRatio: 0.25,
		ActiveRatio:  0.50,
		BufferRatio:  0.10,
	}

	// Act
	layout, err := NewLayout(profile, cfg)

	// Assert
	if err != nil {
		t.Fatalf("NewLayout() unexpected error: %v", err)
	}
	if layout.Total() != 1_000 {
		t.Errorf("Total() = %d, want 1000 (rounding invariant)", layout.Total())
	}
}

func TestMemoryLayout_Total_should_sum_all_four_segments(t *testing.T) {
	// Arrange
	l := MemoryLayout{pinned: 10, summary: 20, active: 30, buffer: 40}

	// Act
	got := l.Total()

	// Assert
	if got != 100 {
		t.Errorf("Total() = %d, want 100", got)
	}
}
