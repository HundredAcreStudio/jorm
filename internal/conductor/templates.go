package conductor

import (
	"github.com/jorm/internal/bus"
	"github.com/jorm/internal/orchestrator"
)

// BuiltinTemplates returns the default workflow templates.
func BuiltinTemplates() map[string][]orchestrator.AgentConfig {
	return map[string][]orchestrator.AgentConfig{
		"single-worker": {
			workerAgent("sonnet", 3, []orchestrator.Trigger{
				{Topic: bus.TopicIssueOpened, Predicate: "always"},
			}),
			completionAgent(),
		},

		"worker-validator": {
			workerAgent("sonnet", 5, []orchestrator.Trigger{
				{Topic: bus.TopicIssueOpened, Predicate: "always"},
				{Topic: bus.TopicValidationResult, Predicate: "rejected"},
			}),
			completionAgent(),
		},

		"full-workflow": {
			plannerAgent(),
			workerAgent("sonnet", 5, []orchestrator.Trigger{
				{Topic: bus.TopicPlanReady, Predicate: "always"},
				{Topic: bus.TopicValidationResult, Predicate: "rejected"},
			}),
			completionAgent(),
		},

		"debug-workflow": {
			debugWorkerAgent(),
			completionAgent(),
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
	}
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

func completionAgent() orchestrator.AgentConfig {
	return orchestrator.AgentConfig{
		ID:            "completion",
		Name:          "Completion",
		Role:          "completion",
		Triggers:      []orchestrator.Trigger{{Topic: bus.TopicValidationResult, Predicate: "approved"}},
		Model:         "",
		MaxIterations: 1,
		OnComplete:    []orchestrator.OnCompleteAction{{Topic: bus.TopicClusterComplete}},
	}
}
