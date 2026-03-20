package ui

import (
	"fmt"
	"sort"
	"strings"
)

// Formatter produces formatted output strings for the terminal UI.
type Formatter struct{}

// FormatAgentLine left-pads the agent name to 16 chars and adds " | " separator.
func (f *Formatter) FormatAgentLine(name, text string) string {
	padded := fmt.Sprintf("%-16s", name)
	coloredName := colorAgent.Sprint(padded)
	dimPipe := colorDim.Sprint("|")
	return fmt.Sprintf("%s%s %s", coloredName, dimPipe, text)
}

// FormatSeparator produces a centered label in a single-line border (────).
func (f *Formatter) FormatSeparator(label string, width int) string {
	if width < len(label)+4 {
		width = len(label) + 4
	}
	total := width - len(label) - 2 // 2 for spaces around label
	left := total / 2
	right := total - left
	line := strings.Repeat("─", left) + " " + label + " " + strings.Repeat("─", right)
	return colorSeparator.Sprint(line)
}

// FormatDoubleSeparator produces a centered label in a double-line border (════).
func (f *Formatter) FormatDoubleSeparator(label string, width int) string {
	if width < len(label)+4 {
		width = len(label) + 4
	}
	total := width - len(label) - 2
	left := total / 2
	right := total - left
	line := strings.Repeat("═", left) + " " + label + " " + strings.Repeat("═", right)
	return colorSeparator.Sprint(line)
}

// FormatSuccess returns a green success marker with text.
func (f *Formatter) FormatSuccess(text string) string {
	return colorSuccess.Sprint(text)
}

// FormatFailure returns a red failure marker with text.
func (f *Formatter) FormatFailure(text string) string {
	return colorFailure.Sprint(text)
}

// FormatTimestamp returns a dimmed timestamp.
func (f *Formatter) FormatTimestamp(ts string) string {
	return colorDim.Sprint(ts)
}

// FormatRoundSummary produces a compact round result summary line.
func (f *Formatter) FormatRoundSummary(round int, results map[string]bool) string {
	approved := 0
	total := len(results)
	for _, v := range results {
		if v {
			approved++
		}
	}

	header := fmt.Sprintf("Round %d Results: %d/%d approved", round, approved, total)
	line := f.FormatSeparator(header, 72)

	// Per-validator summary
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)

	var parts []string
	var rejected []string
	for _, name := range names {
		if results[name] {
			parts = append(parts, colorSuccess.Sprintf("✓ %s", name))
		} else {
			parts = append(parts, colorFailure.Sprintf("✗ %s", name))
			rejected = append(rejected, name)
		}
	}

	summary := "  " + strings.Join(parts, "  ")
	if len(rejected) > 0 {
		summary += colorWarning.Sprintf("\n  → Retrying: %s found errors", strings.Join(rejected, ", "))
	}

	return line + "\n" + summary
}
