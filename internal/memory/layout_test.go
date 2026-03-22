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

// ---------------------------------------------------------------------------
// Task 6 — BUFFER auto-raise with proportional deduction (AC#2)
// ---------------------------------------------------------------------------

func TestNewLayout_should_raise_buffer_to_max_output_tokens_when_floor_is_less(t *testing.T) {
	// Arrange: context=200,000, max_output=65,536
	// floor(200,000 * 0.10) = 20,000 < 65,536 → auto-raise
	profile := ModelProfile{
		ContextWindowTokens: 200_000,
		MaxOutputTokens:     65_536,
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

	// BUFFER must equal max_output_tokens exactly
	if layout.Buffer() != 65_536 {
		t.Errorf("Buffer() = %d, want 65536 (raised to max_output_tokens)", layout.Buffer())
	}

	// Total must still equal context_window_tokens
	if layout.Total() != 200_000 {
		t.Errorf("Total() = %d, want 200000 (sum invariant)", layout.Total())
	}

	// All segments must be positive
	if layout.Pinned() <= 0 {
		t.Errorf("Pinned() = %d, must be > 0 after proportional deduction", layout.Pinned())
	}
	if layout.Summary() <= 0 {
		t.Errorf("Summary() = %d, must be > 0 after proportional deduction", layout.Summary())
	}
	if layout.Active() <= 0 {
		t.Errorf("Active() = %d, must be > 0 after proportional deduction", layout.Active())
	}
}

// ---------------------------------------------------------------------------
// Task 7 — Minimum-capacity guard (AC#3 extremely small context window)
// ---------------------------------------------------------------------------

func TestNewLayout_should_error_when_remaining_capacity_after_buffer_raise_is_insufficient(t *testing.T) {
	// Arrange: context=10,000, max_output=8,500
	// floor(10,000 * 0.10) = 1,000 < 8,500 → raise buffer to 8,500
	// remaining = 10,000 - 8,500 = 1,500 tokens for 3 segments
	// 1,500 / 3 = 500 each — each >= MinSegmentTokens (1) → this should succeed
	// To trigger error: context=5,000, max_output=4,998
	// remaining = 5,000 - 4,998 = 2 tokens for 3 segments → error
	profile := ModelProfile{
		ContextWindowTokens: 5_000,
		MaxOutputTokens:     4_998,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.15,
		SummaryRatio: 0.25,
		ActiveRatio:  0.50,
		BufferRatio:  0.10,
	}

	// Act
	_, err := NewLayout(profile, cfg)

	// Assert
	if err == nil {
		t.Fatal("NewLayout() expected error when remaining capacity is insufficient, got nil")
	}
}

func TestNewLayout_should_succeed_when_remaining_capacity_is_just_enough(t *testing.T) {
	// Arrange: context=10,000, max_output=8,500
	// remaining = 1,500 — three segments get 500 each → all >= MinSegmentTokens
	profile := ModelProfile{
		ContextWindowTokens: 10_000,
		MaxOutputTokens:     8_500,
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
	if layout.Buffer() != 8_500 {
		t.Errorf("Buffer() = %d, want 8500", layout.Buffer())
	}
	if layout.Total() != 10_000 {
		t.Errorf("Total() = %d, want 10000", layout.Total())
	}
}

// ---------------------------------------------------------------------------
// Task 8 — Complete coverage for all 6 acceptance criteria
// ---------------------------------------------------------------------------

// AC#2 precision: verify sum invariant after auto-raise on 200k context
func TestNewLayout_should_sum_exactly_after_buffer_auto_raise_200k(t *testing.T) {
	// AC#2: context=200,000, max_output=65,536
	profile := ModelProfile{
		ContextWindowTokens: 200_000,
		MaxOutputTokens:     65_536,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.15,
		SummaryRatio: 0.25,
		ActiveRatio:  0.50,
		BufferRatio:  0.10,
	}

	layout, err := NewLayout(profile, cfg)
	if err != nil {
		t.Fatalf("NewLayout() unexpected error: %v", err)
	}
	if layout.Total() != 200_000 {
		t.Errorf("Total() = %d, want 200000 (sum invariant after auto-raise)", layout.Total())
	}
	if layout.Buffer() != 65_536 {
		t.Errorf("Buffer() = %d, want 65536 (max_output_tokens)", layout.Buffer())
	}
}

// AC#4 explicit: gemini-2.5-pro with BUFFER auto-raise still sums correctly
func TestNewLayout_should_sum_exactly_with_gemini_25_pro_profile(t *testing.T) {
	// gemini-2.5-pro: 1,048,576 context, 65,536 max_output
	profile := ModelProfile{
		ContextWindowTokens: 1_048_576,
		MaxOutputTokens:     65_536,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.15,
		SummaryRatio: 0.25,
		ActiveRatio:  0.50,
		BufferRatio:  0.10,
	}

	layout, err := NewLayout(profile, cfg)
	if err != nil {
		t.Fatalf("NewLayout() unexpected error: %v", err)
	}
	// buffer = floor(1,048,576 * 0.10) = 104,857 which is >= 65,536 → no raise
	if layout.Total() != 1_048_576 {
		t.Errorf("Total() = %d, want 1048576", layout.Total())
	}
	if layout.Buffer() != 104_857 {
		t.Errorf("Buffer() = %d, want 104857 (no raise needed)", layout.Buffer())
	}
}

// ---------------------------------------------------------------------------
// Task 9 — otherTotal==0 guard (AC: error when all non-buffer segments are zero)
// ---------------------------------------------------------------------------

func TestNewLayout_should_error_when_otherTotal_is_zero_after_floor_calculation(t *testing.T) {
	// Arrange: context window so small that all three non-buffer segments floor to zero
	// AND buffer must be raised (buffer floor < max_output_tokens) to reach the guard.
	//
	// With total=10 and ratios pinned=0.003, summary=0.003, active=0.004, buffer=0.990:
	//   pinned  = floor(10 * 0.003) = floor(0.03) = 0
	//   summary = floor(10 * 0.003) = floor(0.03) = 0
	//   active  = floor(10 * 0.004) = floor(0.04) = 0
	//   buffer  = floor(10 * 0.990) = floor(9.90) = 9
	//
	// buffer(9) < max_output_tokens(10) → auto-raise triggered.
	// otherTotal = 0+0+0 = 0 → the new guard returns an error.
	//
	// (The minimum-capacity guard also fires, but the otherTotal guard fires first.)
	profile := ModelProfile{
		ContextWindowTokens: 10,
		MaxOutputTokens:     10,
	}
	cfg := LayoutConfig{
		PinnedRatio:  0.003,
		SummaryRatio: 0.003,
		ActiveRatio:  0.004,
		BufferRatio:  0.990,
	}

	// Act
	_, err := NewLayout(profile, cfg)

	// Assert: must return an error (either otherTotal==0 guard or minimum-capacity guard)
	if err == nil {
		t.Fatal("NewLayout() expected error when otherTotal is zero, got nil")
	}
	errMsg := err.Error()
	if len(errMsg) == 0 {
		t.Error("expected descriptive error message")
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
