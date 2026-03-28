package ui

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
)

func init() {
	// Disable color output in tests for predictable assertions.
	color.NoColor = true
}

func TestMain(m *testing.M) {
	color.NoColor = true
	os.Exit(m.Run())
}

func TestFormatterAgentLine(t *testing.T) {
	f := &Formatter{}

	line := f.FormatAgentLine("planner", "▶ started")
	if !strings.HasPrefix(line, "planner") {
		t.Errorf("expected line to start with agent name, got %q", line)
	}
	if !strings.Contains(line, "|") {
		t.Errorf("expected line to contain separator '|', got %q", line)
	}
	if !strings.Contains(line, "▶ started") {
		t.Errorf("expected line to contain text, got %q", line)
	}

	// Check padding: "planner" is 7 chars, padded to 16
	parts := strings.SplitN(line, "|", 2)
	if len(parts[0]) != 16 {
		t.Errorf("expected name to be padded to 16 chars, got %d: %q", len(parts[0]), parts[0])
	}
}

func TestFormatterAgentLineLongName(t *testing.T) {
	f := &Formatter{}
	line := f.FormatAgentLine("validator-requirements", "checking")
	// Name is longer than 16, should still work
	if !strings.Contains(line, "checking") {
		t.Errorf("expected text, got %q", line)
	}
	if !strings.Contains(line, "|") {
		t.Errorf("expected separator, got %q", line)
	}
}

func TestFormatterSeparator(t *testing.T) {
	f := &Formatter{}

	sep := f.FormatSeparator("Validation Round 1", 72)
	if !strings.Contains(sep, "Validation Round 1") {
		t.Errorf("expected label in separator, got %q", sep)
	}
	if !strings.Contains(sep, "─") {
		t.Errorf("expected single-line chars, got %q", sep)
	}
}

func TestFormatterDoubleSeparator(t *testing.T) {
	f := &Formatter{}

	sep := f.FormatDoubleSeparator("CLUSTER COMPLETE", 72)
	if !strings.Contains(sep, "CLUSTER COMPLETE") {
		t.Errorf("expected label in separator, got %q", sep)
	}
	if !strings.Contains(sep, "═") {
		t.Errorf("expected double-line chars, got %q", sep)
	}
}

func TestFormatterSeparatorEmpty(t *testing.T) {
	f := &Formatter{}
	sep := f.FormatSeparator("", 40)
	// Should be all dashes with spaces around empty label
	if !strings.Contains(sep, "─") {
		t.Errorf("expected dashes in empty separator, got %q", sep)
	}
}

func TestFormatterRoundSummary(t *testing.T) {
	f := &Formatter{}
	results := map[string]bool{
		"requirements": true,
		"tester":       true,
		"security":     true,
		"code":         false,
	}
	summary := f.FormatRoundSummary(1, results)
	if !strings.Contains(summary, "3/4 approved") {
		t.Errorf("expected 3/4 approved in summary, got %q", summary)
	}
	if !strings.Contains(summary, "✓ requirements") {
		t.Errorf("expected checkmark for requirements, got %q", summary)
	}
	if !strings.Contains(summary, "✗ code") {
		t.Errorf("expected X for code, got %q", summary)
	}
	if !strings.Contains(summary, "Retrying") {
		t.Errorf("expected retry note, got %q", summary)
	}
}

func TestFooterRender(t *testing.T) {
	f := NewFooter("test-run-1", 4, 72)
	f.AddAgent("a1", "worker", 1)
	f.AddAgent("a2", "validator-code", 2)

	rendered := f.Render()
	if !strings.Contains(rendered, "┌") {
		t.Errorf("expected top-left corner in footer, got %q", rendered)
	}
	if !strings.Contains(rendered, "└") {
		t.Errorf("expected bottom-left corner in footer, got %q", rendered)
	}
	if !strings.Contains(rendered, "test-run-1") {
		t.Errorf("expected run ID in footer, got %q", rendered)
	}
	if !strings.Contains(rendered, "worker") {
		t.Errorf("expected agent name in footer, got %q", rendered)
	}
	if !strings.Contains(rendered, "●") {
		t.Errorf("expected active indicator in footer, got %q", rendered)
	}
	if !strings.Contains(rendered, "running") {
		t.Errorf("expected status in footer, got %q", rendered)
	}
}

func TestFooterAddRemoveAgent(t *testing.T) {
	f := NewFooter("test-run", 3, 72)

	f.AddAgent("a1", "worker", 1)
	if f.Lines() != 3 { // top + 1 agent + summary
		t.Errorf("expected height 3, got %d", f.Lines())
	}

	f.AddAgent("a2", "validator", 1)
	if f.Lines() != 4 {
		t.Errorf("expected height 4, got %d", f.Lines())
	}

	f.RemoveAgent("a1")
	if f.Lines() != 3 {
		t.Errorf("expected height 3 after remove, got %d", f.Lines())
	}

	f.RemoveAgent("a2")
	if f.Lines() != 1 { // empty: just the status bar
		t.Errorf("expected height 1 when empty, got %d", f.Lines())
	}
}

func TestFooterEmpty(t *testing.T) {
	f := NewFooter("test-run", 0, 72)
	rendered := f.Render()
	// Footer always renders a status bar, even with no agents
	if !strings.Contains(rendered, "test-run") {
		t.Errorf("expected run ID in minimal footer, got %q", rendered)
	}
	if !strings.Contains(rendered, "running") {
		t.Errorf("expected status in minimal footer, got %q", rendered)
	}
}

func TestFooterClear(t *testing.T) {
	f := NewFooter("test-run", 0, 72)
	// AC6: Clear now takes termHeight and footerLines.
	// AC11: cursor must land at scrollEnd = termHeight - footerLines (row 23 for 24-1).
	clear := f.Clear(24, 1)
	if !strings.Contains(clear, "\033[r") {
		t.Errorf("expected scroll region reset in clear, got %q", clear)
	}
	if !strings.Contains(clear, "\033[23;1H") {
		t.Errorf("expected cursor at row 23 (24-1) in clear, got %q", clear)
	}
	// AC13: hard-coded row 999 must be gone.
	if strings.Contains(clear, "\033[999;1H") {
		t.Errorf("expected no hard-coded row-999 cursor move in clear, got %q", clear)
	}
}

// TestFooterClearCursorPosition verifies Clear positions the cursor at termHeight-footerLines
// across several input combinations.
func TestFooterClearCursorPosition(t *testing.T) {
	tests := []struct {
		termHeight  int
		footerLines int
		wantRow     int
	}{
		{24, 1, 23},
		{24, 3, 21},
		{40, 5, 35},
	}
	f := NewFooter("test-run", 0, 72)
	for _, tt := range tests {
		clear := f.Clear(tt.termHeight, tt.footerLines)
		want := fmt.Sprintf("\033[%d;1H", tt.wantRow)
		if !strings.Contains(clear, want) {
			t.Errorf("Clear(%d, %d): want cursor escape %q in %q",
				tt.termHeight, tt.footerLines, want, clear)
		}
	}
}

// TestInitScrollRegionDynamicFooterLines verifies that InitScrollRegion uses the caller-supplied
// footerLines to compute the scroll-region end, rather than the fixed maxFooterReserve constant.
// AC5: function accepts two parameters; AC3/AC11: scroll end = termHeight - footerLines.
func TestInitScrollRegionDynamicFooterLines(t *testing.T) {
	tests := []struct {
		termHeight    int
		footerLines   int
		wantScrollEnd int
	}{
		{24, 1, 23}, // single status-bar: scroll region ends at row 23
		{24, 3, 21}, // 3 footer lines: scroll region ends at row 21
		{40, 5, 35},
		{24, 8, 16}, // maxFooterReserve-sized footer: same as old constant behavior
	}
	for _, tt := range tests {
		seq := InitScrollRegion(tt.termHeight, tt.footerLines)
		want := fmt.Sprintf("\033[1;%dr", tt.wantScrollEnd)
		if !strings.Contains(seq, want) {
			t.Errorf("InitScrollRegion(%d, %d) = %q, want scroll-region escape %q",
				tt.termHeight, tt.footerLines, seq, want)
		}
	}
}

// TestPaintReserveMatchesFooterLines verifies that Paint() clears only as many rows as the
// footer actually occupies (len(lines)), not the old fixed maxFooterReserve rows.
// AC7: maxFooterReserve must not control the reserved area in Paint().
func TestPaintReserveMatchesFooterLines(t *testing.T) {
	f := NewFooter("test-run", 0, 80)
	f.SetTermSize(80, 24)

	// With no active agents, Lines() == 1 (just the status bar).
	// Paint() should position into row 24, not into rows 17-23.
	paint := f.Paint()

	if !strings.Contains(paint, "\033[24;1H") {
		t.Errorf("Paint() with 1 footer line should clear/write row 24; got %q", paint)
	}

	// Row 17 = 24 - maxFooterReserve + 1: must not appear when footer is only 1 line.
	if strings.Contains(paint, "\033[17;1H") {
		t.Errorf("Paint() with 1 footer line must not clear row 17 (old maxFooterReserve behaviour); got %q", paint)
	}
}

// TestUIHasTermHeightAndLastFooterLines verifies that the UI struct exposes the two new fields
// required by the plan (AC8, AC9). This test will fail to compile until they are added.
func TestUIHasTermHeightAndLastFooterLines(t *testing.T) {
	u := &UI{
		w:               &bytes.Buffer{},
		formatter:       &Formatter{},
		footer:          NewFooter("test", 0, 80),
		metrics:         NewProcessMetrics(),
		runID:           "test",
		startTime:       time.Now(),
		termWidth:       80,
		termHeight:      24,
		lastFooterLines: 1,
	}
	if u.termHeight != 24 {
		t.Errorf("expected termHeight 24, got %d", u.termHeight)
	}
	if u.lastFooterLines != 1 {
		t.Errorf("expected lastFooterLines 1, got %d", u.lastFooterLines)
	}
}

// TestLoopDoneNoCursorRow999 verifies that LoopDone no longer emits the hard-coded row-999
// cursor escape. AC10/AC13: Clear must be called with actual height/footerLines.
func TestLoopDoneNoCursorRow999(t *testing.T) {
	var buf bytes.Buffer
	u := &UI{
		w:               &buf,
		formatter:       &Formatter{},
		footer:          NewFooter("test-run", 0, 80),
		metrics:         NewProcessMetrics(),
		runID:           "test-run",
		startTime:       time.Now(),
		termWidth:       80,
		termHeight:      24,
		lastFooterLines: 1,
		totalAgents:     0,
	}

	u.LoopDone(nil)
	output := buf.String()
	if strings.Contains(output, "\033[999;1H") {
		t.Errorf("LoopDone must not emit hard-coded row-999 cursor escape; got %q", output)
	}
}

func TestFooterUpdateMetrics(t *testing.T) {
	f := NewFooter("test-run", 2, 72)
	f.AddAgent("a1", "worker", 1)
	f.UpdateMetrics("a1", 5.5, 256.0)

	rendered := f.Render()
	if !strings.Contains(rendered, "5.5") {
		t.Errorf("expected CPU in render, got %q", rendered)
	}
	if !strings.Contains(rendered, "256") {
		t.Errorf("expected RAM in render, got %q", rendered)
	}
}

// Test that the UI writes to the provided writer
func TestUIWritesToWriter(t *testing.T) {
	var buf bytes.Buffer
	u := &UI{
		w:           &buf,
		formatter:   &Formatter{},
		footer:      NewFooter("test", 0, 80),
		metrics:     NewProcessMetrics(),
		runID:       "test",
		startTime:   time.Now(),
		termWidth:   80,
		totalAgents: 0,
	}

	u.Phase("test phase")
	if !strings.Contains(buf.String(), "test phase") {
		t.Errorf("expected output to contain 'test phase', got %q", buf.String())
	}
}

func TestUIAgentSpawned(t *testing.T) {
	var buf bytes.Buffer
	u := &UI{
		w:         &buf,
		formatter: &Formatter{},
		footer:    NewFooter("test", 2, 80),
		metrics:   NewProcessMetrics(),
		runID:     "test",
		startTime: time.Now(),
		termWidth: 80,
	}

	u.AgentSpawned("p1", "planner", []string{"ISSUE_OPENED"})
	output := buf.String()
	if !strings.Contains(output, "planner") {
		t.Errorf("expected planner in output, got %q", output)
	}
	if !strings.Contains(output, "started") {
		t.Errorf("expected started marker, got %q", output)
	}
	if !strings.Contains(output, "ISSUE_OPENED") {
		t.Errorf("expected trigger topic, got %q", output)
	}
}

func TestUIClusterComplete(t *testing.T) {
	var buf bytes.Buffer
	u := &UI{
		w:         &buf,
		formatter: &Formatter{},
		footer:    NewFooter("test-run", 0, 80),
		metrics:   NewProcessMetrics(),
		runID:     "test-run",
		startTime: time.Now(),
		termWidth: 80,
	}

	u.ClusterComplete("test-run", "all_validators_approved")
	output := buf.String()
	if !strings.Contains(output, "═") {
		t.Errorf("expected double separator, got %q", output)
	}
	if !strings.Contains(output, "CLUSTER COMPLETE") {
		t.Errorf("expected CLUSTER COMPLETE, got %q", output)
	}
	if !strings.Contains(output, "all_validators_approved") {
		t.Errorf("expected reason, got %q", output)
	}
}

func TestUILoopDoneSuccess(t *testing.T) {
	var buf bytes.Buffer
	u := &UI{
		w:           &buf,
		formatter:   &Formatter{},
		footer:      NewFooter("test-run", 5, 80),
		metrics:     NewProcessMetrics(),
		runID:       "test-run",
		startTime:   time.Now(),
		termWidth:   80,
		totalAgents: 5,
		round:       3,
		totalCost:   8.23,
	}

	u.LoopDone(nil)
	output := buf.String()
	if !strings.Contains(output, "COMPLETED") {
		t.Errorf("expected COMPLETED message, got %q", output)
	}
	if !strings.Contains(output, "Run:    test-run") {
		t.Errorf("expected run ID, got %q", output)
	}
	if !strings.Contains(output, "$8.23") {
		t.Errorf("expected cost, got %q", output)
	}
}
