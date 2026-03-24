package main

import (
	"fmt"
	"time"

	"github.com/jorm/internal/agent"
	"github.com/jorm/internal/ui"
	"github.com/spf13/cobra"
)

func newDemoCmd() *cobra.Command {
	var speed float64

	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Run a fake event sequence to preview the TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDemo(speed)
		},
	}
	cmd.Flags().Float64Var(&speed, "speed", 1.0, "playback speed multiplier (2.0 = twice as fast)")
	return cmd
}

func runDemo(speed float64) error {
	if speed <= 0 {
		speed = 1.0
	}
	pause := func(d time.Duration) {
		time.Sleep(time.Duration(float64(d) / speed))
	}

	runID := "demo-abc123"
	totalAgents := 5
	sink := ui.New(runID, totalAgents)

	// --- Issue loaded ---
	pause(300 * time.Millisecond)
	sink.Phase("loading issue")
	pause(500 * time.Millisecond)
	sink.IssueLoaded("Add retry logic to webhook delivery", "https://github.com/acme/api/issues/42")
	sink.Classification("feature/medium")
	pause(400 * time.Millisecond)

	// --- Agents spawn ---
	sink.SystemEvent("Spawning agent cluster")
	pause(200 * time.Millisecond)

	agents := []struct {
		id, name string
		triggers []string
	}{
		{"planner", "planner", []string{"ISSUE_OPENED"}},
		{"worker", "worker", []string{"PLAN_READY"}},
		{"code-review", "code-review", []string{"DIFF_READY"}},
		{"security", "security", []string{"DIFF_READY"}},
		{"tester", "tester", []string{"DIFF_READY"}},
	}
	for _, a := range agents {
		sink.AgentSpawned(a.id, a.name, a.triggers)
		pause(150 * time.Millisecond)
	}

	// --- Planner runs ---
	pause(500 * time.Millisecond)
	sink.AgentTriggerFired("planner", "ISSUE_OPENED", 1, "sonnet")
	pause(200 * time.Millisecond)
	sink.ClaudeOutput("[planner] Analyzing issue requirements...")
	pause(800 * time.Millisecond)
	sink.ClaudeOutput("[planner] Identified 3 files to modify: webhook.go, retry.go, webhook_test.go")
	pause(600 * time.Millisecond)
	sink.AgentTokenUsage("planner", "planner", 2450, 890)
	sink.Cost(0.03)
	sink.MessagePublished("PLAN_READY", "planner")
	sink.AgentTaskCompleted("planner", 1)
	pause(300 * time.Millisecond)

	// --- Worker runs ---
	sink.Attempt(1, 3)
	sink.AgentTriggerFired("worker", "PLAN_READY", 1, "sonnet")
	pause(300 * time.Millisecond)
	sink.ClaudeOutput("[worker] Creating retry mechanism with exponential backoff...")
	pause(1200 * time.Millisecond)
	sink.ClaudeOutput("[worker] Writing tests for retry logic...")
	pause(800 * time.Millisecond)
	sink.ClaudeOutput("[worker] Running go test ./internal/webhook/...")
	pause(600 * time.Millisecond)
	sink.ClaudeOutput("[worker] All 12 tests passing")
	pause(400 * time.Millisecond)
	sink.AgentTokenUsage("worker", "worker", 8200, 3100)
	sink.Cost(0.15)
	sink.MessagePublished("DIFF_READY", "worker")
	sink.AgentTaskCompleted("worker", 1)
	pause(300 * time.Millisecond)

	// --- Validation round 1 ---
	sink.ValidationRoundStart(1)
	pause(200 * time.Millisecond)

	// Validators fire in parallel
	sink.AgentTriggerFired("code-review", "DIFF_READY", 1, "sonnet")
	sink.AgentTriggerFired("security", "DIFF_READY", 1, "sonnet")
	sink.AgentTriggerFired("tester", "DIFF_READY", 1, "sonnet")
	pause(200 * time.Millisecond)

	sink.ClaudeOutput("[code-review] Reviewing diff for code quality...")
	sink.ClaudeOutput("[security] Scanning for OWASP vulnerabilities...")
	sink.ClaudeOutput("[tester] Checking test coverage and edge cases...")
	pause(1500 * time.Millisecond)

	// Security passes
	sink.ValidatorDone(agent.ValidatorResult{
		ValidatorID: "security",
		Name:        "security",
		Passed:      true,
		OnFail:      "reject",
		Output:      "No security issues found. Retry logic uses safe exponential backoff.",
	})
	sink.AgentTaskCompleted("security", 1)
	sink.Cost(0.22)
	pause(500 * time.Millisecond)

	// Tester passes
	sink.ValidatorDone(agent.ValidatorResult{
		ValidatorID: "tester",
		Name:        "tester",
		Passed:      true,
		OnFail:      "reject",
		Output:      "Test coverage adequate. 12 tests cover happy path and error cases.",
	})
	sink.AgentTaskCompleted("tester", 1)
	sink.Cost(0.28)
	pause(500 * time.Millisecond)

	// Code review rejects
	sink.ValidatorDone(agent.ValidatorResult{
		ValidatorID: "code-review",
		Name:        "code-review",
		Passed:      false,
		OnFail:      "reject",
		Output:      "WHAT: Missing context.Context propagation in retry loop\nHOW: RetryWebhook() spawns goroutines without passing parent context\nWHY: Leaked goroutines on server shutdown; violates cancellation contract",
	})
	sink.AgentTaskCompleted("code-review", 1)
	sink.Cost(0.35)
	pause(300 * time.Millisecond)

	sink.ValidationRoundComplete(1, 2, 1)

	// --- Retry round ---
	pause(500 * time.Millisecond)
	sink.RetryRoundStart(2)
	sink.Attempt(2, 3)
	sink.AgentTriggerFired("worker", "DIFF_READY", 2, "sonnet")
	pause(300 * time.Millisecond)
	sink.ClaudeOutput("[worker] Fixing context propagation in retry loop...")
	pause(1000 * time.Millisecond)
	sink.ClaudeOutput("[worker] Added context.WithCancel to RetryWebhook goroutines")
	pause(600 * time.Millisecond)
	sink.ClaudeOutput("[worker] Tests still passing after fix")
	pause(400 * time.Millisecond)
	sink.AgentTokenUsage("worker", "worker", 4100, 1200)
	sink.Cost(0.42)
	sink.MessagePublished("DIFF_READY", "worker")
	sink.AgentTaskCompleted("worker", 2)
	pause(300 * time.Millisecond)

	// --- Validation round 2 ---
	sink.ValidationRoundStart(2)
	pause(200 * time.Millisecond)

	sink.AgentTriggerFired("code-review", "DIFF_READY", 2, "sonnet")
	sink.AgentTriggerFired("security", "DIFF_READY", 2, "sonnet")
	sink.AgentTriggerFired("tester", "DIFF_READY", 2, "sonnet")
	pause(200 * time.Millisecond)

	sink.ClaudeOutput("[code-review] Re-reviewing with fixes applied...")
	sink.ClaudeOutput("[security] Re-scanning updated diff...")
	sink.ClaudeOutput("[tester] Verifying test coverage on updated code...")
	pause(1200 * time.Millisecond)

	sink.ValidatorDone(agent.ValidatorResult{
		ValidatorID: "code-review",
		Name:        "code-review",
		Passed:      true,
		OnFail:      "reject",
		Output:      "Context propagation fixed. Code looks clean.",
	})
	sink.AgentTaskCompleted("code-review", 2)
	sink.Cost(0.48)
	pause(400 * time.Millisecond)

	sink.ValidatorDone(agent.ValidatorResult{
		ValidatorID: "security",
		Name:        "security",
		Passed:      true,
		OnFail:      "reject",
		Output:      "No issues found.",
	})
	sink.AgentTaskCompleted("security", 2)
	pause(300 * time.Millisecond)

	sink.ValidatorDone(agent.ValidatorResult{
		ValidatorID: "tester",
		Name:        "tester",
		Passed:      true,
		OnFail:      "reject",
		Output:      "All tests pass. Coverage at 94%.",
	})
	sink.AgentTaskCompleted("tester", 2)
	sink.Cost(0.55)
	pause(200 * time.Millisecond)

	sink.ValidationRoundComplete(2, 3, 0)
	pause(500 * time.Millisecond)

	// --- Complete ---
	sink.ClusterComplete(runID, "all validators approved on round 2")
	pause(300 * time.Millisecond)
	sink.LoopDone(nil)

	fmt.Println()
	return nil
}
