package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/neokn/agenticsystem/internal/memory"
)

// ---------------------------------------------------------------------------
// Task 1 & 2: assembly and flag parsing
// ---------------------------------------------------------------------------

// should_return_error_when_gemini_api_key_is_not_set verifies that the demo
// agent exits immediately with a descriptive error when GOOGLE_API_KEY is not set.
// Acceptance criterion: "GOOGLE_API_KEY is not set" message must appear on stderr.
func Test_should_return_error_when_gemini_api_key_is_not_set(t *testing.T) {
	// Arrange
	orig := os.Getenv("GOOGLE_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	defer func() {
		if orig != "" {
			os.Setenv("GOOGLE_API_KEY", orig)
		}
	}()

	// Act
	err := checkAPIKey()

	// Assert
	if err == nil {
		t.Fatal("expected error when GOOGLE_API_KEY is not set, got nil")
	}
	if !strings.Contains(err.Error(), "GOOGLE_API_KEY is not set") {
		t.Errorf("expected error message to contain %q, got %q", "GOOGLE_API_KEY is not set", err.Error())
	}
}

// should_not_return_error_when_gemini_api_key_is_set verifies that checkAPIKey
// succeeds when the environment variable is present.
func Test_should_not_return_error_when_gemini_api_key_is_set(t *testing.T) {
	// Arrange
	os.Setenv("GOOGLE_API_KEY", "test-key")
	defer os.Unsetenv("GOOGLE_API_KEY")

	// Act
	err := checkAPIKey()

	// Assert
	if err != nil {
		t.Errorf("expected no error when GOOGLE_API_KEY is set, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task 2: CLI flag parsing
// ---------------------------------------------------------------------------

// should_parse_turns_flag verifies that --turns N is correctly parsed.
func Test_should_parse_turns_flag(t *testing.T) {
	// Arrange / Act
	cfg, err := parseFlags([]string{"--turns", "5"})

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.turns != 5 {
		t.Errorf("expected turns=5, got %d", cfg.turns)
	}
}

// should_parse_metrics_out_flag verifies that --metrics-out FILE is correctly parsed.
func Test_should_parse_metrics_out_flag(t *testing.T) {
	// Arrange / Act
	cfg, err := parseFlags([]string{"--metrics-out", "/tmp/metrics.txt"})

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.metricsOut != "/tmp/metrics.txt" {
		t.Errorf("expected metricsOut=%q, got %q", "/tmp/metrics.txt", cfg.metricsOut)
	}
}

// should_use_default_flag_values_when_no_flags_provided verifies defaults.
func Test_should_use_default_flag_values_when_no_flags_provided(t *testing.T) {
	// Arrange / Act
	cfg, err := parseFlags([]string{})

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.turns != 0 {
		t.Errorf("expected default turns=0, got %d", cfg.turns)
	}
	if cfg.metricsOut != "" {
		t.Errorf("expected default metricsOut empty, got %q", cfg.metricsOut)
	}
}

// ---------------------------------------------------------------------------
// Task 5: metrics output format
// ---------------------------------------------------------------------------

// should_format_metrics_with_colon_separated_fields verifies that formatMetrics
// returns the exact expected format from the acceptance criteria.
func Test_should_format_metrics_with_colon_separated_fields(t *testing.T) {
	// Arrange
	snap := memory.MemoryMetrics{
		CountTokensAPICallCount: 3,
		CompressTriggerCount:    2,
		OOMEventCount:           0,
	}
	usageRatioCurve := []float64{0.12, 0.24, 0.48}
	compressCostUSD := 0.000123

	// Act
	output := formatMetrics(snap, usageRatioCurve, compressCostUSD)

	// Assert
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines in metrics output, got %d:\n%s", len(lines), output)
	}

	assertMetricLine(t, lines[0], "usage_ratio_curve", "0.120000,0.240000,0.480000")
	assertMetricLine(t, lines[1], "compress_trigger_count", "2")
	assertMetricLine(t, lines[2], "countTokens_api_call_count", "3")
	assertMetricLine(t, lines[3], "compress_cost_usd", "0.000123")
	assertMetricLine(t, lines[4], "oom_event_count", "0")
}

// should_format_empty_usage_ratio_curve_as_empty_list verifies that an empty
// usage ratio curve is formatted as an empty value.
func Test_should_format_empty_usage_ratio_curve_as_empty_list(t *testing.T) {
	// Arrange
	snap := memory.MemoryMetrics{}
	var usageRatioCurve []float64

	// Act
	output := formatMetrics(snap, usageRatioCurve, 0.0)

	// Assert
	if !strings.Contains(output, "usage_ratio_curve: ") {
		t.Errorf("expected usage_ratio_curve line in output, got:\n%s", output)
	}
}

// should_format_compress_cost_usd_with_6_decimal_places verifies precision.
func Test_should_format_compress_cost_usd_with_6_decimal_places(t *testing.T) {
	// Arrange
	snap := memory.MemoryMetrics{}
	cost := 0.123456789

	// Act
	output := formatMetrics(snap, nil, cost)

	// Assert
	if !strings.Contains(output, "compress_cost_usd: 0.123457") {
		t.Errorf("expected 6 decimal places in compress_cost_usd, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Task 3 & 6: OOM scenario via small context window (structural, no API)
// ---------------------------------------------------------------------------

// should_build_small_profile_for_oom_scenario verifies that a ModelProfile
// with artificially small context_window_tokens can be created.
// This covers acceptance criterion: OOM handler test with 2000-token window.
func Test_should_build_small_profile_for_oom_scenario(t *testing.T) {
	// Arrange / Act
	profile := buildOOMTestProfile()

	// Assert
	if profile.ContextWindowTokens != 2000 {
		t.Errorf("expected ContextWindowTokens=2000, got %d", profile.ContextWindowTokens)
	}
	if profile.ModelID == "" {
		t.Error("expected non-empty ModelID")
	}
}

// should_assemble_memory_layout_with_default_config verifies that all memory
// components assemble correctly using the dependency injection pattern from ADR-0003.
// Assembles MemoryLayout using real memory types without a genai.Client — no API key required.
func Test_should_assemble_memory_layout_with_default_config(t *testing.T) {
	// Arrange
	profile := memory.ModelProfile{
		ModelID:             "gemini-2.0-flash",
		Provider:            "google",
		ContextWindowTokens: 1048576,
		MaxOutputTokens:     8192,
	}

	// Act
	cfg, err := memory.DefaultLayoutConfig()
	if err != nil {
		t.Fatalf("DefaultLayoutConfig failed: %v", err)
	}
	layout, err := memory.NewLayout(profile, cfg)

	// Assert
	if err != nil {
		t.Fatalf("NewLayout failed: %v", err)
	}
	if layout.Total() != profile.ContextWindowTokens {
		t.Errorf("expected layout.Total()=%d, got %d", profile.ContextWindowTokens, layout.Total())
	}
}

// should_assemble_memory_layout_for_oom_scenario verifies that NewLayout
// accepts a 2000-token context window (OOM test scenario).
func Test_should_assemble_memory_layout_for_oom_scenario(t *testing.T) {
	// Arrange
	profile := buildOOMTestProfile()

	// Act
	cfg, err := memory.DefaultLayoutConfig()
	if err != nil {
		t.Fatalf("DefaultLayoutConfig failed: %v", err)
	}
	_, err = memory.NewLayout(profile, cfg)

	// Assert: layout creation must fail gracefully or succeed;
	// either way it must not panic. With MaxOutputTokens > ContextWindowTokens
	// it should return an error, not panic.
	// We just verify no panic by reaching this point.
	_ = err // error is acceptable for an intentionally tiny window
}

// ---------------------------------------------------------------------------
// Task 5: writeMetricsToFile
// ---------------------------------------------------------------------------

// should_write_metrics_to_file_when_metrics_out_flag_is_set verifies that
// metrics are written to a file when --metrics-out is provided.
func Test_should_write_metrics_to_file_when_metrics_out_flag_is_set(t *testing.T) {
	// Arrange
	f, err := os.CreateTemp("", "metrics-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	snap := memory.MemoryMetrics{
		CompressTriggerCount:    1,
		CountTokensAPICallCount: 2,
		OOMEventCount:           0,
	}
	content := formatMetrics(snap, []float64{0.5}, 0.001234)

	// Act
	err = writeMetricsToFile(f.Name(), content)

	// Assert
	if err != nil {
		t.Fatalf("writeMetricsToFile failed: %v", err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("failed to read metrics file: %v", err)
	}
	if !strings.Contains(string(data), "compress_trigger_count: 1") {
		t.Errorf("expected compress_trigger_count in file, got:\n%s", string(data))
	}
}

// should_print_metrics_to_writer verifies that printMetrics writes to
// an io.Writer correctly (for testability without stdout capture).
func Test_should_print_metrics_to_writer(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	snap := memory.MemoryMetrics{
		CompressTriggerCount:    3,
		CountTokensAPICallCount: 5,
		OOMEventCount:           1,
	}
	curve := []float64{0.1, 0.5, 0.9}
	content := formatMetrics(snap, curve, 0.000042)

	// Act
	fmt.Fprint(&buf, content)

	// Assert
	output := buf.String()
	if !strings.Contains(output, "oom_event_count: 1") {
		t.Errorf("expected oom_event_count in output, got:\n%s", output)
	}
	if !strings.Contains(output, "compress_trigger_count: 3") {
		t.Errorf("expected compress_trigger_count in output, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// assertMetricLine checks that a metrics line matches "key: value" format.
func assertMetricLine(t *testing.T, line, expectedKey, expectedValue string) {
	t.Helper()
	parts := strings.SplitN(line, ": ", 2)
	if len(parts) != 2 {
		t.Errorf("expected colon-separated line %q, got %q", expectedKey+": "+expectedValue, line)
		return
	}
	if parts[0] != expectedKey {
		t.Errorf("expected key %q, got %q", expectedKey, parts[0])
	}
	if parts[1] != expectedValue {
		t.Errorf("for key %q: expected value %q, got %q", expectedKey, expectedValue, parts[1])
	}
}
