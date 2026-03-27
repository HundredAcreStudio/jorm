package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jorm/internal/bus"
)

// BuildPlannerContext assembles context for the planner agent: issue content.
func BuildPlannerContext(b *bus.Bus, clusterID string) (string, error) {
	msgs, err := b.Query(clusterID, bus.QueryOpts{
		Topics: []string{bus.TopicIssueOpened},
		Limit:  1,
	})
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("no ISSUE_OPENED message found")
	}

	return msgs[0].Content, nil
}

// BuildWorkerContext assembles context for the worker agent:
// issue + plan + any rejection feedback.
func BuildWorkerContext(b *bus.Bus, clusterID string) (string, error) {
	var sections []string

	// Issue
	issueMsgs, err := b.Query(clusterID, bus.QueryOpts{
		Topics: []string{bus.TopicIssueOpened},
		Limit:  1,
	})
	if err != nil {
		return "", err
	}
	if len(issueMsgs) > 0 {
		sections = append(sections, "## Issue\n\n"+issueMsgs[0].Content)
	}

	// Plan (if available)
	planMsg, err := b.FindLast(clusterID, bus.TopicPlanReady)
	if err == nil && planMsg != nil {
		sections = append(sections, "## Implementation Plan\n\n"+planMsg.Content)
	}

	// Previous rejection findings (if any)
	rejections, err := b.Query(clusterID, bus.QueryOpts{
		Topics: []string{bus.TopicValidationResult},
	})
	if err == nil {
		var findings []string
		for _, r := range rejections {
			approved, _ := r.Data["approved"].(bool)
			if !approved && r.Content != "" {
				findings = append(findings, fmt.Sprintf("### Validator: %s\n%s", r.Sender, r.Content))
			}
		}
		if len(findings) > 0 {
			sections = append(sections, "## Previous attempt was rejected. Fix these issues:\n\n"+strings.Join(findings, "\n\n"))
		}
	}

	return strings.Join(sections, "\n\n"), nil
}

// parseDiffFilePaths extracts the current-version file paths from unified diff output
// by parsing "diff --git a/... b/..." lines and taking the b/ path.
// Skips /dev/null entries (deleted files).
func parseDiffFilePaths(diff string) []string {
	var paths []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "diff --git ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		bPath := parts[len(parts)-1]
		if strings.HasPrefix(bPath, "b/") {
			bPath = bPath[2:]
		}
		if bPath == "/dev/null" || bPath == "dev/null" {
			continue
		}
		if !seen[bPath] {
			seen[bPath] = true
			paths = append(paths, bPath)
		}
	}
	return paths
}

// readFileIfExists reads a file at an absolute path, returning "" on error.
// Caps output at 100 KB and appends a truncation notice for large files.
func readFileIfExists(path string) string {
	const maxBytes = 100 * 1024
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > maxBytes {
		return string(data[:maxBytes]) + "\n[...truncated at 100KB]"
	}
	return string(data)
}

// BuildRichValidatorContext assembles enriched context for reviewer agents:
// acceptance criteria, diff, full content of changed files, related test files,
// CLAUDE.md, and go.mod.
func BuildRichValidatorContext(b *bus.Bus, clusterID, workDir string) (string, error) {
	var sections []string

	// Acceptance criteria from plan
	planMsg, err := b.FindLast(clusterID, bus.TopicPlanReady)
	if err == nil && planMsg != nil {
		if criteria, ok := planMsg.Data["acceptance_criteria"].(string); ok && criteria != "" {
			sections = append(sections, "## Acceptance Criteria (from planner)\n\n"+criteria)
		}
	}

	// Latest implementation diff
	diff := ""
	implMsg, err := b.FindLast(clusterID, bus.TopicImplementationReady)
	if err == nil && implMsg != nil {
		diff = implMsg.Content
		sections = append(sections, "## Implementation\n\n"+diff)
	}

	// Changed files (full content)
	changedPaths := parseDiffFilePaths(diff)
	if len(changedPaths) > 0 {
		var filesSections []string
		for _, p := range changedPaths {
			content := readFileIfExists(filepath.Join(workDir, p))
			if content == "" {
				continue
			}
			filesSections = append(filesSections, fmt.Sprintf("### %s\n\n```\n%s\n```", p, content))
		}
		// Auto-include test files for changed .go files
		for _, p := range changedPaths {
			if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
				testPath := strings.TrimSuffix(p, ".go") + "_test.go"
				content := readFileIfExists(filepath.Join(workDir, testPath))
				if content != "" {
					filesSections = append(filesSections, fmt.Sprintf("### %s\n\n```\n%s\n```", testPath, content))
				}
			}
		}
		if len(filesSections) > 0 {
			sections = append(sections, "## Changed Files (full content)\n\n"+strings.Join(filesSections, "\n\n"))
		}
	}

	// Project conventions
	if content := readFileIfExists(filepath.Join(workDir, "CLAUDE.md")); content != "" {
		sections = append(sections, "## Project Conventions (CLAUDE.md)\n\n"+content)
	}

	// Dependency manifest
	if content := readFileIfExists(filepath.Join(workDir, "go.mod")); content != "" {
		sections = append(sections, "## Dependency Manifest (go.mod)\n\n"+content)
	}

	return strings.Join(sections, "\n\n"), nil
}

// BuildValidatorContext assembles context for validator agents:
// the diff + acceptance criteria from the plan.
func BuildValidatorContext(b *bus.Bus, clusterID string) (string, error) {
	var sections []string

	// Acceptance criteria from plan
	planMsg, err := b.FindLast(clusterID, bus.TopicPlanReady)
	if err == nil && planMsg != nil {
		if criteria, ok := planMsg.Data["acceptance_criteria"].(string); ok && criteria != "" {
			sections = append(sections, "## Acceptance Criteria (from planner)\n\n"+criteria)
		}
	}

	// Latest implementation diff
	implMsg, err := b.FindLast(clusterID, bus.TopicImplementationReady)
	if err == nil && implMsg != nil {
		sections = append(sections, "## Implementation\n\n"+implMsg.Content)
	}

	return strings.Join(sections, "\n\n"), nil
}

// BuildTestWriterContext assembles context for the test-writer agent:
// issue + plan (no validation feedback — tests are written before implementation).
func BuildTestWriterContext(b *bus.Bus, clusterID string) (string, error) {
	var sections []string

	issueMsgs, err := b.Query(clusterID, bus.QueryOpts{
		Topics: []string{bus.TopicIssueOpened},
		Limit:  1,
	})
	if err != nil {
		return "", err
	}
	if len(issueMsgs) > 0 {
		sections = append(sections, "## Issue\n\n"+issueMsgs[0].Content)
	}

	planMsg, err := b.FindLast(clusterID, bus.TopicPlanReady)
	if err == nil && planMsg != nil {
		sections = append(sections, "## Plan\n\n"+planMsg.Content)
	}

	return strings.Join(sections, "\n\n"), nil
}

// CollectReviewerNotes queries all approved VALIDATION_RESULT messages and extracts
// notes containing "Nit:" (primary) or "LOW:" (legacy) — from both bare lines and
// JSON "notes" arrays. Returns a deduplicated slice.
func CollectReviewerNotes(b *bus.Bus, clusterID string) ([]string, error) {
	msgs, err := b.Query(clusterID, bus.QueryOpts{
		Topics: []string{bus.TopicValidationResult},
	})
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var notes []string

	addNote := func(note, sender string) {
		note = strings.TrimSpace(note)
		if note != "" && !seen[note] {
			seen[note] = true
			notes = append(notes, fmt.Sprintf("%s  (from %s)", note, sender))
		}
	}

	for _, m := range msgs {
		approved, _ := m.Data["approved"].(bool)
		if !approved {
			continue
		}

		// Strategy 1: Parse JSON "notes" array from the content.
		// Reviewers output: {"approved": true, "errors": [], "notes": ["Nit: ...", ...]}
		if idx := strings.Index(m.Content, `"notes"`); idx >= 0 {
			// Find the array start
			rest := m.Content[idx:]
			if arrStart := strings.Index(rest, "["); arrStart >= 0 {
				arrRest := rest[arrStart:]
				if arrEnd := strings.Index(arrRest, "]"); arrEnd >= 0 {
					arrStr := arrRest[:arrEnd+1]
					var jsonNotes []string
					if json.Unmarshal([]byte(arrStr), &jsonNotes) == nil {
						for _, n := range jsonNotes {
							if strings.Contains(n, "Nit:") || strings.Contains(n, "LOW:") {
								addNote(n, m.Sender)
							}
						}
					}
				}
			}
		}

		// Strategy 2: Scan bare lines for "Nit:" or "LOW:" prefix (fallback for non-JSON output).
		for _, line := range strings.Split(m.Content, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "Nit:") || strings.HasPrefix(trimmed, "LOW:") {
				addNote(trimmed, m.Sender)
			}
		}
	}
	return notes, nil
}

// BuildCleanupWorkerContext assembles context for the cleanup worker:
// issue + plan + collected LOW notes from all approved reviewers.
// Returns ("", nil) if no notes exist — caller should skip the stage.
func BuildCleanupWorkerContext(b *bus.Bus, clusterID string) (string, error) {
	notes, err := CollectReviewerNotes(b, clusterID)
	if err != nil {
		return "", err
	}
	return BuildCleanupWorkerContextFromNotes(b, clusterID, notes)
}

// BuildCleanupWorkerContextFromNotes assembles cleanup context from pre-collected notes.
// Avoids re-querying the bus when the caller already has the notes.
func BuildCleanupWorkerContextFromNotes(b *bus.Bus, clusterID string, notes []string) (string, error) {
	if len(notes) == 0 {
		return "", nil
	}

	var sections []string

	// Issue
	issueMsgs, err := b.Query(clusterID, bus.QueryOpts{
		Topics: []string{bus.TopicIssueOpened},
		Limit:  1,
	})
	if err != nil {
		return "", err
	}
	if len(issueMsgs) > 0 {
		sections = append(sections, "## Issue\n\n"+issueMsgs[0].Content)
	}

	// Plan (if available)
	planMsg, err := b.FindLast(clusterID, bus.TopicPlanReady)
	if err == nil && planMsg != nil {
		sections = append(sections, "## Implementation Plan\n\n"+planMsg.Content)
	}

	// Cleanup task
	var noteLines []string
	for _, n := range notes {
		noteLines = append(noteLines, "- "+n)
	}
	sections = append(sections, "## Cleanup Task: Address Review Notes\n\nThe following low-severity notes were flagged by reviewers. Address each one:\n\n"+strings.Join(noteLines, "\n"))

	return strings.Join(sections, "\n\n"), nil
}

// BuildStageScopedWorkerContext assembles worker context with rejection feedback
// scoped to the current stage only (identified by stageIndex in Data["stage_index"]).
// Feedback from prior stages (already addressed and accepted) is excluded.
func BuildStageScopedWorkerContext(b *bus.Bus, clusterID string, stageIndex int, stageName string) (string, error) {
	var sections []string

	issueMsgs, err := b.Query(clusterID, bus.QueryOpts{
		Topics: []string{bus.TopicIssueOpened},
		Limit:  1,
	})
	if err != nil {
		return "", err
	}
	if len(issueMsgs) > 0 {
		sections = append(sections, "## Issue\n\n"+issueMsgs[0].Content)
	}

	planMsg, err := b.FindLast(clusterID, bus.TopicPlanReady)
	if err == nil && planMsg != nil {
		sections = append(sections, "## Implementation Plan\n\n"+planMsg.Content)
	}

	rejections, err := b.Query(clusterID, bus.QueryOpts{
		Topics: []string{bus.TopicValidationResult},
	})
	if err == nil {
		var findings []string
		for _, r := range rejections {
			approved, _ := r.Data["approved"].(bool)
			if approved || r.Content == "" {
				continue
			}
			idx, ok := r.Data["stage_index"].(int)
			if !ok {
				// Try float64 (JSON numbers unmarshal as float64)
				if f, ok2 := r.Data["stage_index"].(float64); ok2 {
					idx = int(f)
					ok = true
				}
			}
			if ok && idx != stageIndex {
				continue
			}
			findings = append(findings, fmt.Sprintf("### Validator: %s\n%s", r.Sender, r.Content))
		}
		if len(findings) > 0 {
			header := "## Previous attempt was rejected. Fix these issues:\n\n"
			sections = append(sections, header+strings.Join(findings, "\n\n"))
		}
	}

	return strings.Join(sections, "\n\n"), nil
}

