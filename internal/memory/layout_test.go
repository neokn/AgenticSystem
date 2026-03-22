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
