package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// LayoutConfig holds the ratio of context window tokens allocated to each
// memory segment. All four ratios must be positive and must sum to exactly 1.0.
// Load defaults from configs/default.json via DefaultLayoutConfig.
type LayoutConfig struct {
	PinnedRatio  float64 `json:"pinned"`
	SummaryRatio float64 `json:"summary"`
	ActiveRatio  float64 `json:"active"`
	BufferRatio  float64 `json:"buffer"`
}

// defaultConfigJSON holds the path to the default config file relative to
// this source file. We use runtime.Caller to locate the source tree so the
// function works regardless of the working directory during tests.
//
// DefaultLayoutConfig reads the layout ratios from configs/default.json in the
// project root (two directories above internal/memory/).
func DefaultLayoutConfig() (LayoutConfig, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return LayoutConfig{}, fmt.Errorf("DefaultLayoutConfig: could not determine source file location")
	}
	// internal/memory/layout.go → ../../configs/default.json
	configPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "configs", "default.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return LayoutConfig{}, fmt.Errorf("DefaultLayoutConfig: reading config file: %w", err)
	}

	var raw struct {
		Memory struct {
			Layout LayoutConfig `json:"layout"`
		} `json:"memory"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return LayoutConfig{}, fmt.Errorf("DefaultLayoutConfig: parsing config file: %w", err)
	}

	return raw.Memory.Layout, nil
}

// MinSegmentTokens is the minimum number of tokens any non-BUFFER segment must
// receive after BUFFER is raised to max_output_tokens. A value of 1 ensures no
// segment is left empty.
const MinSegmentTokens = 1

// ratioEpsilon is the tolerance for floating-point ratio sum comparisons.
const ratioEpsilon = 1e-9

// NewLayout constructs an immutable MemoryLayout from a ModelProfile and a
// LayoutConfig. It validates all ratios, computes segment token limits using
// floor arithmetic, auto-raises BUFFER to max_output_tokens when necessary,
// and checks that every non-BUFFER segment receives at least MinSegmentTokens.
//
// Errors follow ADR-0001: plain fmt.Errorf with descriptive context.
func NewLayout(profile ModelProfile, cfg LayoutConfig) (MemoryLayout, error) {
	// --- Ratio validation ---
	if cfg.PinnedRatio <= 0 {
		return MemoryLayout{}, fmt.Errorf("NewLayout: all ratios must be > 0; PinnedRatio is %g", cfg.PinnedRatio)
	}
	if cfg.SummaryRatio <= 0 {
		return MemoryLayout{}, fmt.Errorf("NewLayout: all ratios must be > 0; SummaryRatio is %g", cfg.SummaryRatio)
	}
	if cfg.ActiveRatio <= 0 {
		return MemoryLayout{}, fmt.Errorf("NewLayout: all ratios must be > 0; ActiveRatio is %g", cfg.ActiveRatio)
	}
	if cfg.BufferRatio <= 0 {
		return MemoryLayout{}, fmt.Errorf("NewLayout: all ratios must be > 0; BufferRatio is %g", cfg.BufferRatio)
	}

	sum := cfg.PinnedRatio + cfg.SummaryRatio + cfg.ActiveRatio + cfg.BufferRatio
	if sum < 1.0-ratioEpsilon || sum > 1.0+ratioEpsilon {
		return MemoryLayout{}, fmt.Errorf("NewLayout: ratio sum must equal 1.0, got %.10f", sum)
	}

	total := profile.ContextWindowTokens

	// --- Base segment calculation: floor each segment ---
	pinned  := int(float64(total) * cfg.PinnedRatio)
	summary := int(float64(total) * cfg.SummaryRatio)
	active  := int(float64(total) * cfg.ActiveRatio)
	buffer  := int(float64(total) * cfg.BufferRatio)

	// Assign rounding remainder to active so sum == total exactly.
	remainder := total - (pinned + summary + active + buffer)
	active += remainder

	return MemoryLayout{
		pinned:  pinned,
		summary: summary,
		active:  active,
		buffer:  buffer,
	}, nil
}

// MemoryLayout is an immutable value object that partitions a model's context
// window into four named segments: PINNED, SUMMARY, ACTIVE, and BUFFER.
//
// Construct with NewLayout — never use a zero-value MemoryLayout.
// Pass by value; the unexported fields prevent mutation from outside this
// package, enforcing the invariant that all four limits sum to the total
// context window size.
type MemoryLayout struct {
	pinned  int
	summary int
	active  int
	buffer  int
}

// Pinned returns the token limit for the PINNED segment.
func (l MemoryLayout) Pinned() int { return l.pinned }

// Summary returns the token limit for the SUMMARY segment.
func (l MemoryLayout) Summary() int { return l.summary }

// Active returns the token limit for the ACTIVE segment.
func (l MemoryLayout) Active() int { return l.active }

// Buffer returns the token limit for the BUFFER segment.
func (l MemoryLayout) Buffer() int { return l.buffer }

// Total returns the sum of all four segment token limits. This always equals
// the context_window_tokens value used to construct the layout.
func (l MemoryLayout) Total() int {
	return l.pinned + l.summary + l.active + l.buffer
}
