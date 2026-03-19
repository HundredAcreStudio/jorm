package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormatterAgentLine(t *testing.T) {
	f := &Formatter{}

	line := f.FormatAgentLine("planner", "▶ started")
	if !strings.HasPrefix(line, "planner") {
		t.Errorf("expected line to start with agent name, got %q", line)
	}
	if !strings.Contains(line, "| ") {
		t.Errorf("expected line to contain separator '| ', got %q", line)
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
	if !strings.Contains(line, "| checking") {
		t.Errorf("expected separator and text, got %q", line)
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
	if !strings.Contains(rendered, "┐") {
		t.Errorf("expected top-right corner in footer, got %q", rendered)
	}
	if !strings.Contains(rendered, "└") {
		t.Errorf("expected bottom-left corner in footer, got %q", rendered)
	}
	if !strings.Contains(rendered, "┘") {
		t.Errorf("expected bottom-right corner in footer, got %q", rendered)
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
	if f.Height() != 3 { // top + 1 agent + summary
		t.Errorf("expected height 3, got %d", f.Height())
	}

	f.AddAgent("a2", "validator", 1)
	if f.Height() != 4 {
		t.Errorf("expected height 4, got %d", f.Height())
	}

	f.RemoveAgent("a1")
	if f.Height() != 3 {
		t.Errorf("expected height 3 after remove, got %d", f.Height())
	}

	f.RemoveAgent("a2")
	if f.Height() != 2 { // empty: top + summary
		t.Errorf("expected height 2 when empty, got %d", f.Height())
	}
}

func TestFooterEmpty(t *testing.T) {
	f := NewFooter("test-run", 0, 72)
	rendered := f.Render()
	if rendered != "" {
		t.Errorf("expected empty string for footer with no agents, got %q", rendered)
	}
}

func TestFooterClear(t *testing.T) {
	f := NewFooter("test-run", 0, 72)
	clear := f.Clear()
	if !strings.Contains(clear, "\033[r") {
		t.Errorf("expected scroll region reset in clear, got %q", clear)
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
	if !strings.Contains(rendered, "256.0") {
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
	if !strings.Contains(output, "▶ started") {
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
	if !strings.Contains(output, "Cluster test-run completed in") {
		t.Errorf("expected completion message, got %q", output)
	}
	if !strings.Contains(output, "5 agents") {
		t.Errorf("expected agent count, got %q", output)
	}
	if !strings.Contains(output, "3 rounds") {
		t.Errorf("expected round count, got %q", output)
	}
}
