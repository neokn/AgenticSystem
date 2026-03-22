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
