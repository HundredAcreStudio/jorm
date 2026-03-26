package conductor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jorm/internal/agent"
	"github.com/jorm/internal/events"
	"github.com/jorm/internal/issue"
)

// Complexity levels for issue classification.
const (
	Trivial  = "TRIVIAL"
	Simple   = "SIMPLE"
	Standard = "STANDARD"
	Critical = "CRITICAL"
)

// Task types for issue classification.
const (
	Inquiry = "INQUIRY"
	Task    = "TASK"
	Debug   = "DEBUG"
)

// Classification is the result of the conductor's analysis.
type Classification struct {
	Complexity string `json:"complexity"`
	Type       string `json:"type"`
	Reasoning  string `json:"reasoning"`
}

// Conductor classifies issues and selects workflow templates.
type Conductor struct {
	model   string
	workDir string
	env     []string
	sink    events.Sink
}

// New creates a conductor with the given model for classification.
func New(model, workDir string, env []string, sink events.Sink) *Conductor {
	if model == "" {
		model = "haiku"
	}
	return &Conductor{
		model:   model,
		workDir: workDir,
		env:     env,
		sink:    sink,
	}
}

// Classify analyzes an issue and returns a classification.
func (c *Conductor) Classify(ctx context.Context, iss *issue.Issue) (*Classification, error) {
	c.sink.Phase("Classifying issue...")

	prompt := fmt.Sprintf(`Classify this issue for an autonomous dev loop.

## Issue
Title: %s
Body:
%s

## Instructions
Analyze the issue and classify it along two dimensions:

1. **Complexity**: How complex is this to implement?
   - TRIVIAL: Single file, mechanical change (rename, config tweak)
   - SIMPLE: Small change, 1-2 files, clear solution
   - STANDARD: Multi-file work, requires planning and testing
   - CRITICAL: Touches auth, payments, security, PII, or core infrastructure

2. **Type**: What kind of work is this?
   - INQUIRY: Read-only exploration or investigation
   - TASK: Implement a new feature or enhancement
   - DEBUG: Fix a bug or broken behavior

Respond with ONLY a JSON object, no other text:
{"complexity": "...", "type": "...", "reasoning": "one sentence explaining your classification"}`, iss.Title, iss.Body)

	result, err := agent.RunClaude(ctx, agent.RunOptions{
		Prompt:  prompt,
		WorkDir: c.workDir,
		Model:   c.model,
		Env:     c.env,
	})
	if err != nil {
		return nil, fmt.Errorf("classification failed: %w", err)
	}

	// Parse the JSON response
	cls := &Classification{}
	text := strings.TrimSpace(result.Text)

	// Try to extract JSON from the response (may be wrapped in markdown)
	if idx := strings.Index(text, "{"); idx >= 0 {
		if end := strings.LastIndex(text, "}"); end >= idx {
			text = text[idx : end+1]
		}
	}

	if err := json.Unmarshal([]byte(text), cls); err != nil {
		// Default to STANDARD/TASK if parsing fails
		c.sink.ClaudeOutput(fmt.Sprintf("[conductor] classification parse error, defaulting to STANDARD/TASK: %v", err))
		return &Classification{
			Complexity: Standard,
			Type:       Task,
			Reasoning:  "Classification failed, using default",
		}, nil
	}

	// Normalize
	cls.Complexity = strings.ToUpper(cls.Complexity)
	cls.Type = strings.ToUpper(cls.Type)

	c.sink.ClaudeOutput(fmt.Sprintf("[conductor] %s/%s — %s", cls.Complexity, cls.Type, cls.Reasoning))
	return cls, nil
}

