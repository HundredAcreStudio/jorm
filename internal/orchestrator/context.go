package orchestrator

import (
	"fmt"
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

// BuildCompletionContext assembles context for the completion detector:
// all validation results.
func BuildCompletionContext(b *bus.Bus, clusterID string) (string, error) {
	results, err := b.Query(clusterID, bus.QueryOpts{
		Topics: []string{bus.TopicValidationResult},
	})
	if err != nil {
		return "", err
	}

	var lines []string
	for _, r := range results {
		approved, _ := r.Data["approved"].(bool)
		status := "REJECTED"
		if approved {
			status = "APPROVED"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", r.Sender, status))
	}

	return "## Validation Results\n\n" + strings.Join(lines, "\n"), nil
}
