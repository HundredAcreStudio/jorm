package conductor

import (
	"strings"

	"github.com/jorm/internal/agent"
	"github.com/jorm/internal/bus"
	"github.com/jorm/internal/orchestrator"
)

// BuiltinTemplates returns the default workflow templates.
// Note: completion agents are NOT included here — the orchestrator's
// injectValidators() adds the completion agent with proper multi-validator tracking.
func BuiltinTemplates() map[string][]orchestrator.AgentConfig {
	return map[string][]orchestrator.AgentConfig{
		"single-worker": {
			workerAgent("sonnet", 3, []orchestrator.Trigger{
				{Topic: bus.TopicIssueOpened, Predicate: "always"},
			}),
		},

		"worker-validator": {
			workerAgent("sonnet", 5, []orchestrator.Trigger{
				{Topic: bus.TopicIssueOpened, Predicate: "always"},
				{Topic: bus.TopicValidationResult, Predicate: "rejected"},
			}),
		},

		"full-workflow": {
			plannerAgent(),
			workerAgent("sonnet", 5, []orchestrator.Trigger{
				{Topic: bus.TopicPlanReady, Predicate: "always"},
				{Topic: bus.TopicValidationResult, Predicate: "rejected"},
			}),
		},

		"debug-workflow": {
			debugWorkerAgent(),
		},
	}
}

func plannerAgent() orchestrator.AgentConfig {
	return orchestrator.AgentConfig{
		ID:            "planner",
		Name:          "Planner",
		Role:          "planner",
		Triggers:      []orchestrator.Trigger{{Topic: bus.TopicIssueOpened, Predicate: "always"}},
		Prompt:        "builtin:planner",
		Model:         "sonnet",
		MaxIterations: 1,
		OnComplete:    []orchestrator.OnCompleteAction{{Topic: bus.TopicPlanReady}},
		ContextBuilder: orchestrator.BuildPlannerContext,
		ResultProcessor: func(result *agent.ClaudeResult) map[string]any {
			if result == nil {
				return nil
			}
			return map[string]any{
				"acceptance_criteria": extractSection(result.Text, "Acceptance Criteria"),
				"plan":               extractSection(result.Text, "Plan"),
			}
		},
	}
}

// extractSection extracts the content between a "### <heading>" marker
// and the next "###" marker (or end of text). Returns empty string if not found.
func extractSection(text, heading string) string {
	marker := "### " + heading
	idx := strings.Index(text, marker)
	if idx == -1 {
		return ""
	}
	start := idx + len(marker)
	rest := text[start:]
	// Find the next ### heading
	end := strings.Index(rest, "\n###")
	if end == -1 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

func workerAgent(model string, maxIter int, triggers []orchestrator.Trigger) orchestrator.AgentConfig {
	return orchestrator.AgentConfig{
		ID:            "worker",
		Name:          "Worker",
		Role:          "worker",
		Triggers:      triggers,
		Prompt:        "builtin:worker",
		Model:         model,
		MaxIterations: maxIter,
		OnComplete:    []orchestrator.OnCompleteAction{{Topic: bus.TopicImplementationReady}},
		ContextBuilder: orchestrator.BuildWorkerContext,
	}
}

func debugWorkerAgent() orchestrator.AgentConfig {
	cfg := workerAgent("sonnet", 5, []orchestrator.Trigger{
		{Topic: bus.TopicIssueOpened, Predicate: "always"},
		{Topic: bus.TopicValidationResult, Predicate: "rejected"},
	})
	cfg.ID = "debugger"
	cfg.Name = "Debugger"
	cfg.Prompt = "builtin:debug-worker"
	return cfg
}

func testWriterAgent() orchestrator.AgentConfig {
	return orchestrator.AgentConfig{
		ID:            "test-writer",
		Name:          "Test Writer",
		Role:          "worker",
		Triggers:      []orchestrator.Trigger{{Topic: bus.TopicPlanReady, Predicate: "always"}},
		Prompt:        "builtin:test-writer",
		Model:         "sonnet",
		MaxIterations: 1,
		OnComplete:    []orchestrator.OnCompleteAction{{Topic: bus.TopicTestsReady}},
		ContextBuilder: orchestrator.BuildTestWriterContext,
	}
}

func testerAgentConfig() orchestrator.AgentConfig {
	return orchestrator.AgentConfig{
		ID:            "tester",
		Name:          "Tester",
		Role:          "validator",
		ExecutionMode: "shell",
		Command:       "CGO_ENABLED=1 go test ./...",
	}
}

func testReviewAgent() orchestrator.AgentConfig {
	return orchestrator.AgentConfig{
		ID:         "test-reviewer",
		Name:       "Test Reviewer",
		Role:       "validator",
		Prompt:     "builtin:tester-review",
		Model:      "sonnet",
		ReviewMode: true,
	}
}

func prReviewAgent() orchestrator.AgentConfig {
	return orchestrator.AgentConfig{
		ID:         "pr-reviewer",
		Name:       "PR Reviewer",
		Role:       "validator",
		Prompt:     "builtin:pr-review",
		Model:      "sonnet",
		ReviewMode: true,
	}
}

func securityReviewAgent() orchestrator.AgentConfig {
	return orchestrator.AgentConfig{
		ID:         "security-reviewer",
		Name:       "Security Reviewer",
		Role:       "validator",
		Prompt:     "builtin:security-review",
		Model:      "sonnet",
		ReviewMode: true,
	}
}

func ptr[T any](v T) *T {
	return &v
}

// StagedTemplate defines the full pipeline structure for the stage orchestrator.
type StagedTemplate struct {
	WorkerConfig orchestrator.AgentConfig
	TesterConfig orchestrator.AgentConfig
	Stages       []orchestrator.Stage
}

// BuiltinStagedTemplates returns staged workflow templates for the StageOrchestrator.
func BuiltinStagedTemplates() map[string]StagedTemplate {
	return map[string]StagedTemplate{
		"full-workflow": {
			WorkerConfig: workerAgent("sonnet", 5, nil),
			TesterConfig: testerAgentConfig(),
			Stages: []orchestrator.Stage{
				{Name: "Planning", Kind: orchestrator.StageKindAgent, AgentConfig: ptr(plannerAgent())},
				{Name: "Test Writing", Kind: orchestrator.StageKindAgent, AgentConfig: ptr(testWriterAgent())},
				{Name: "Test Review", Kind: orchestrator.StageKindReview, ReviewerConfig: ptr(testReviewAgent()), MaxRetries: 3},
				{Name: "PR Review", Kind: orchestrator.StageKindReview, ReviewerConfig: ptr(prReviewAgent()), MaxRetries: 3},
				{Name: "Security Review", Kind: orchestrator.StageKindReview, ReviewerConfig: ptr(securityReviewAgent()), MaxRetries: 3},
			},
		},
	}
}

