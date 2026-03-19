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
	return fmt.Sprintf("%s| %s", padded, text)
}

// FormatSeparator produces a centered label in a single-line border (────).
func (f *Formatter) FormatSeparator(label string, width int) string {
	if width < len(label)+4 {
		width = len(label) + 4
	}
	total := width - len(label) - 2 // 2 for spaces around label
	left := total / 2
	right := total - left
	return strings.Repeat("─", left) + " " + label + " " + strings.Repeat("─", right)
}

// FormatDoubleSeparator produces a centered label in a double-line border (════).
func (f *Formatter) FormatDoubleSeparator(label string, width int) string {
	if width < len(label)+4 {
		width = len(label) + 4
	}
	total := width - len(label) - 2
	left := total / 2
	right := total - left
	return strings.Repeat("═", left) + " " + label + " " + strings.Repeat("═", right)
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
			parts = append(parts, fmt.Sprintf("✓ %s", name))
		} else {
			parts = append(parts, fmt.Sprintf("✗ %s", name))
			rejected = append(rejected, name)
		}
	}

	summary := "  " + strings.Join(parts, "  ")
	if len(rejected) > 0 {
		summary += fmt.Sprintf("\n  → Retrying: %s found errors", strings.Join(rejected, ", "))
	}

	return line + "\n" + summary
}
