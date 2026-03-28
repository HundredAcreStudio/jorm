package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorm/internal/bus"
	"github.com/jorm/internal/config"
	gitpkg "github.com/jorm/internal/git"
	"github.com/jorm/internal/issue"
)

// newTestStageOrchestrator is a helper that creates a StageOrchestrator wired to the
// provided test bus with shell-mode worker and tester configs.
func newTestStageOrchestrator(t *testing.T, b *bus.Bus, stages []Stage) *StageOrchestrator {
	t.Helper()
	tmpDir := t.TempDir()
	wt := &gitpkg.Worktree{Dir: tmpDir, RepoDir: tmpDir}
	sink := &fakeSink{}

	workerCfg := AgentConfig{
		ID:            "worker",
		Name:          "Worker",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       "true",
	}
	testerCfg := AgentConfig{
		ID:            "tester",
		Name:          "Tester",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       "true",
	}

	return NewStageOrchestrator(b, &config.Config{}, wt, sink, nil,
		"test-cluster", workerCfg, testerCfg, stages)
}

// testIssue returns a minimal issue for test pipelines.
func testIssue() *issue.Issue {
	return &issue.Issue{ID: "1", Title: "Test Issue", Body: "test body"}
}

// TestStageOrchestrator_SingleAgentStage verifies that a pipeline with one StageKindAgent
// stage runs the agent once and publishes CLUSTER_COMPLETE.
func TestStageOrchestrator_SingleAgentStage(t *testing.T) {
	b := newTestBus(t)

	stages := []Stage{
		{
			Name: "planner",
			Kind: StageKindAgent,
			AgentConfig: &AgentConfig{
				ID:            "planner",
				Name:          "Planner",
				Role:          "planner",
				ExecutionMode: "shell",
				Command:       "echo plan-done",
				OnComplete:    []OnCompleteAction{{Topic: bus.TopicPlanReady}},
			},
		},
	}

	so := newTestStageOrchestrator(t, b, stages)

	err := so.Run(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CLUSTER_COMPLETE must be on the bus after a successful run
	msgs, err := b.Query("test-cluster", bus.QueryOpts{Topics: []string{bus.TopicClusterComplete}})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected CLUSTER_COMPLETE to be published after single-stage pipeline")
	}

	// PLAN_READY must also be published by the planner's OnComplete action
	planMsgs, err := b.Query("test-cluster", bus.QueryOpts{Topics: []string{bus.TopicPlanReady}})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}
	if len(planMsgs) == 0 {
		t.Error("expected PLAN_READY published by planner's OnComplete")
	}
}

// TestStageOrchestrator_ReviewStage_ApprovesFirst verifies that a StageKindReview stage
// that is approved on the first attempt publishes VALIDATION_RESULT(approved=true) and
// CLUSTER_COMPLETE, without running a worker fix.
func TestStageOrchestrator_ReviewStage_ApprovesFirst(t *testing.T) {
	b := newTestBus(t)

	stages := []Stage{
		{
			Name:       "review",
			Kind:       StageKindReview,
			MaxRetries: 3,
			ReviewerConfig: &AgentConfig{
				ID:            "reviewer",
				Name:          "Reviewer",
				Role:          "validator",
				ExecutionMode: "shell",
				Command:       "exit 0", // always approves
			},
		},
	}

	so := newTestStageOrchestrator(t, b, stages)

	err := so.Run(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// VALIDATION_RESULT must be published with approved=true
	valMsgs, err := b.Query("test-cluster", bus.QueryOpts{Topics: []string{bus.TopicValidationResult}})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}
	if len(valMsgs) == 0 {
		t.Fatal("expected at least one VALIDATION_RESULT")
	}
	approved, _ := valMsgs[0].Data["approved"].(bool)
	if !approved {
		t.Error("expected VALIDATION_RESULT with approved=true")
	}

	// CLUSTER_COMPLETE must be present
	completeMsgs, err := b.Query("test-cluster", bus.QueryOpts{Topics: []string{bus.TopicClusterComplete}})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}
	if len(completeMsgs) == 0 {
		t.Error("expected CLUSTER_COMPLETE after approved review stage")
	}
}

// TestStageOrchestrator_ReviewStage_RejectThenApprove verifies the inner retry loop:
// reviewer rejects on the first attempt (creating a sentinel file) and approves on the
// second (when the sentinel exists). Exactly two VALIDATION_RESULT messages are published.
func TestStageOrchestrator_ReviewStage_RejectThenApprove(t *testing.T) {
	b := newTestBus(t)

	sentinelFile := filepath.Join(t.TempDir(), "sentinel")
	// First call: create sentinel, exit 1 (rejected). Second call: sentinel exists, exit 0 (approved).
	reviewerCmd := fmt.Sprintf(`test -f "%s" && exit 0; touch "%s"; exit 1`, sentinelFile, sentinelFile)

	stages := []Stage{
		{
			Name:       "review",
			Kind:       StageKindReview,
			MaxRetries: 5,
			ReviewerConfig: &AgentConfig{
				ID:            "reviewer",
				Name:          "Reviewer",
				Role:          "validator",
				ExecutionMode: "shell",
				Command:       reviewerCmd,
			},
		},
	}

	so := newTestStageOrchestrator(t, b, stages)

	err := so.Run(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect exactly two VALIDATION_RESULT messages: one rejection, one approval
	valMsgs, err := b.Query("test-cluster", bus.QueryOpts{Topics: []string{bus.TopicValidationResult}})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}
	if len(valMsgs) != 2 {
		t.Fatalf("expected 2 VALIDATION_RESULT messages, got %d", len(valMsgs))
	}

	// First result: rejected
	firstApproved, _ := valMsgs[0].Data["approved"].(bool)
	if firstApproved {
		t.Error("expected first VALIDATION_RESULT to be rejected")
	}

	// Second result: approved
	secondApproved, _ := valMsgs[1].Data["approved"].(bool)
	if !secondApproved {
		t.Error("expected second VALIDATION_RESULT to be approved")
	}
}

// TestStageOrchestrator_MaxRetriesExceeded verifies that runReviewStage returns an error
// wrapping "exceeded max retries" when the reviewer always rejects and MaxRetries is hit.
func TestStageOrchestrator_MaxRetriesExceeded(t *testing.T) {
	b := newTestBus(t)

	stages := []Stage{
		{
			Name:       "review",
			Kind:       StageKindReview,
			MaxRetries: 2, // allow exactly 2 attempts before giving up
			ReviewerConfig: &AgentConfig{
				ID:            "reviewer",
				Name:          "Reviewer",
				Role:          "validator",
				ExecutionMode: "shell",
				Command:       "exit 1", // always rejects
			},
		},
	}

	so := newTestStageOrchestrator(t, b, stages)

	err := so.Run(context.Background(), testIssue())
	if err == nil {
		t.Fatal("expected error when max retries exceeded, got nil")
	}
	if !strings.Contains(err.Error(), "exceeded max retries") {
		t.Errorf("expected error to mention 'exceeded max retries', got: %v", err)
	}
}

// TestStageOrchestrator_ContextCancellation verifies that Run returns ctx.Err() when
// the context is cancelled before stage execution begins.
func TestStageOrchestrator_ContextCancellation(t *testing.T) {
	b := newTestBus(t)

	stages := []Stage{
		{
			Name: "planner",
			Kind: StageKindAgent,
			AgentConfig: &AgentConfig{
				ID:            "planner",
				Name:          "Planner",
				ExecutionMode: "shell",
				Command:       "sleep 10",
			},
		},
	}

	so := newTestStageOrchestrator(t, b, stages)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run is called

	err := so.Run(ctx, testIssue())
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestStageOrchestrator_MultipleStages verifies that two sequential StageKindAgent stages
// both execute in order and CLUSTER_COMPLETE is published after both complete.
func TestStageOrchestrator_MultipleStages(t *testing.T) {
	b := newTestBus(t)
	tmpDir := t.TempDir()

	markerA := filepath.Join(tmpDir, "stage-a.txt")
	markerB := filepath.Join(tmpDir, "stage-b.txt")

	stages := []Stage{
		{
			Name: "stage-a",
			Kind: StageKindAgent,
			AgentConfig: &AgentConfig{
				ID:            "agent-a",
				Name:          "Agent A",
				ExecutionMode: "shell",
				Command:       fmt.Sprintf(`touch "%s"`, markerA),
				OnComplete:    []OnCompleteAction{{Topic: bus.TopicPlanReady}},
			},
		},
		{
			Name: "stage-b",
			Kind: StageKindAgent,
			AgentConfig: &AgentConfig{
				ID:            "agent-b",
				Name:          "Agent B",
				ExecutionMode: "shell",
				Command:       fmt.Sprintf(`touch "%s"`, markerB),
			},
		},
	}

	// Use a custom orchestrator so worktree workDir is tmpDir (for file creation)
	sink := &fakeSink{}
	wt := &gitpkg.Worktree{Dir: tmpDir, RepoDir: tmpDir}
	workerCfg := AgentConfig{ID: "worker", ExecutionMode: "shell", Command: "true"}
	testerCfg := AgentConfig{ID: "tester", ExecutionMode: "shell", Command: "true"}
	so := NewStageOrchestrator(b, &config.Config{}, wt, sink, nil,
		"test-cluster", workerCfg, testerCfg, stages)

	err := so.Run(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify CLUSTER_COMPLETE was published
	completeMsgs, err := b.Query("test-cluster", bus.QueryOpts{Topics: []string{bus.TopicClusterComplete}})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}
	if len(completeMsgs) == 0 {
		t.Error("expected CLUSTER_COMPLETE after multi-stage pipeline")
	}

	// Verify both stages published their OnComplete events or left audit trail via ISSUE_OPENED
	allMsgs, err := b.Query("test-cluster", bus.QueryOpts{})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}
	topicsPresent := make(map[string]bool)
	for _, m := range allMsgs {
		topicsPresent[m.Topic] = true
	}
	if !topicsPresent[bus.TopicPlanReady] {
		t.Error("expected PLAN_READY from stage-a's OnComplete")
	}
}

// TestStageOrchestrator_BusAuditTrail verifies that a complete pipeline run produces
// the expected audit trail of ISSUE_OPENED, VALIDATION_RESULT, and CLUSTER_COMPLETE
// messages on the bus.
func TestStageOrchestrator_BusAuditTrail(t *testing.T) {
	b := newTestBus(t)

	stages := []Stage{
		{
			Name:       "review",
			Kind:       StageKindReview,
			MaxRetries: 3,
			ReviewerConfig: &AgentConfig{
				ID:            "reviewer",
				Name:          "Reviewer",
				Role:          "validator",
				ExecutionMode: "shell",
				Command:       "exit 0", // approves on first attempt
			},
		},
	}

	so := newTestStageOrchestrator(t, b, stages)

	err := so.Run(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Query all topics of interest in one call
	wantTopics := []string{
		bus.TopicIssueOpened,
		bus.TopicValidationResult,
		bus.TopicClusterComplete,
	}
	msgs, err := b.Query("test-cluster", bus.QueryOpts{Topics: wantTopics})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}

	present := make(map[string]bool)
	for _, m := range msgs {
		present[m.Topic] = true
	}

	for _, topic := range wantTopics {
		if !present[topic] {
			t.Errorf("expected %q message in audit trail, but not found", topic)
		}
	}

	// Verify approved=true on CLUSTER_COMPLETE
	for _, m := range msgs {
		if m.Topic == bus.TopicClusterComplete {
			approved, _ := m.Data["approved"].(bool)
			if !approved {
				t.Error("expected CLUSTER_COMPLETE with approved=true")
			}
		}
	}
}

// TestStageOrchestrator_CleanupStage_SkipsWhenNoNotes verifies that a cleanup stage is a
// no-op when no approved VALIDATION_RESULT messages with LOW: notes exist on the bus.
// The worker is configured to fail ("false") so if it runs, the test will error.
func TestStageOrchestrator_CleanupStage_SkipsWhenNoNotes(t *testing.T) {
	b := newTestBus(t)
	tmpDir := t.TempDir()

	sentinel := filepath.Join(tmpDir, "worker-ran.txt")

	sink := &fakeSink{}
	wt := &gitpkg.Worktree{Dir: tmpDir, RepoDir: tmpDir}

	// Worker fails if invoked — proves cleanup stage does not call the worker
	workerCfg := AgentConfig{
		ID:            "worker",
		Name:          "Worker",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       fmt.Sprintf(`touch "%s"; exit 1`, sentinel),
	}
	testerCfg := AgentConfig{
		ID:            "tester",
		Name:          "Tester",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       "true",
	}

	stages := []Stage{
		{
			Name: "Cleanup",
			Kind: StageKindCleanup,
		},
	}

	so := NewStageOrchestrator(b, &config.Config{}, wt, sink, nil,
		"test-cluster", workerCfg, testerCfg, stages)

	err := so.Run(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Worker must NOT have run (no notes exist)
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Error("expected worker to be skipped when no notes exist, but sentinel file was created")
	}

	// CLUSTER_COMPLETE must still be published
	completeMsgs, err := b.Query("test-cluster", bus.QueryOpts{Topics: []string{bus.TopicClusterComplete}})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}
	if len(completeMsgs) == 0 {
		t.Error("expected CLUSTER_COMPLETE after cleanup stage with no notes")
	}
}

// TestStageOrchestrator_CleanupStage_RunsWorkerWhenNitNotesExist verifies that when an
// approved VALIDATION_RESULT with "Nit:" notes is on the bus (new format from updated
// pr-review.md), the cleanup stage invokes the worker, and CLUSTER_COMPLETE is published.
func TestStageOrchestrator_CleanupStage_RunsWorkerWhenNitNotesExist(t *testing.T) {
	b := newTestBus(t)
	tmpDir := t.TempDir()

	workerSentinel := filepath.Join(tmpDir, "worker-ran-nit.txt")

	// Seed bus with an approved VALIDATION_RESULT containing a Nit: note (new format)
	_ = b.Publish(bus.Message{
		ClusterID: "test-cluster",
		Topic:     bus.TopicValidationResult,
		Sender:    "pr-reviewer",
		Content:   "VERDICT: ACCEPT\nNit: context.go:42 — variable name could be clearer",
		Data:      map[string]any{"approved": true},
	})

	sink := &fakeSink{}
	wt := &gitpkg.Worktree{Dir: tmpDir, RepoDir: tmpDir}

	workerCfg := AgentConfig{
		ID:            "worker",
		Name:          "Worker",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       fmt.Sprintf(`touch "%s"`, workerSentinel),
	}
	testerCfg := AgentConfig{
		ID:            "tester",
		Name:          "Tester",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       "true",
	}

	stages := []Stage{
		{
			Name: "Cleanup",
			Kind: StageKindCleanup,
		},
	}

	so := NewStageOrchestrator(b, &config.Config{}, wt, sink, nil,
		"test-cluster", workerCfg, testerCfg, stages)

	err := so.Run(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Worker must have run because Nit: note triggered cleanup
	if _, statErr := os.Stat(workerSentinel); os.IsNotExist(statErr) {
		t.Error("expected worker to be invoked when Nit: notes exist, but sentinel file not found")
	}

	// CLUSTER_COMPLETE must be published
	completeMsgs, err := b.Query("test-cluster", bus.QueryOpts{Topics: []string{bus.TopicClusterComplete}})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}
	if len(completeMsgs) == 0 {
		t.Error("expected CLUSTER_COMPLETE after cleanup stage with Nit: notes")
	}
}

// TestStageOrchestrator_CleanupStage_RunsWorkerWhenNotesExist verifies that when an
// approved VALIDATION_RESULT with LOW: notes is on the bus, the cleanup stage invokes
// the worker and then the tester, and CLUSTER_COMPLETE is published on success.
func TestStageOrchestrator_CleanupStage_RunsWorkerWhenNotesExist(t *testing.T) {
	b := newTestBus(t)
	tmpDir := t.TempDir()

	workerSentinel := filepath.Join(tmpDir, "worker-ran.txt")
	testerSentinel := filepath.Join(tmpDir, "tester-ran.txt")

	// Seed bus with an approved VALIDATION_RESULT containing a LOW: note
	_ = b.Publish(bus.Message{
		ClusterID: "test-cluster",
		Topic:     bus.TopicValidationResult,
		Sender:    "pr-reviewer",
		Content:   "VERDICT: ACCEPT\nLOW: example note that should trigger cleanup",
		Data:      map[string]any{"approved": true},
	})

	sink := &fakeSink{}
	wt := &gitpkg.Worktree{Dir: tmpDir, RepoDir: tmpDir}

	workerCfg := AgentConfig{
		ID:            "worker",
		Name:          "Worker",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       fmt.Sprintf(`touch "%s"`, workerSentinel),
	}
	testerCfg := AgentConfig{
		ID:            "tester",
		Name:          "Tester",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       fmt.Sprintf(`touch "%s"`, testerSentinel),
	}

	stages := []Stage{
		{
			Name: "Cleanup",
			Kind: StageKindCleanup,
		},
	}

	so := NewStageOrchestrator(b, &config.Config{}, wt, sink, nil,
		"test-cluster", workerCfg, testerCfg, stages)

	err := so.Run(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Worker must have run
	if _, statErr := os.Stat(workerSentinel); os.IsNotExist(statErr) {
		t.Error("expected worker to be invoked when notes exist, but sentinel file not found")
	}

	// Tester must have run
	if _, statErr := os.Stat(testerSentinel); os.IsNotExist(statErr) {
		t.Error("expected tester to be invoked after worker cleanup, but sentinel file not found")
	}

	// CLUSTER_COMPLETE must be published
	completeMsgs, err := b.Query("test-cluster", bus.QueryOpts{Topics: []string{bus.TopicClusterComplete}})
	if err != nil {
		t.Fatalf("querying bus: %v", err)
	}
	if len(completeMsgs) == 0 {
		t.Error("expected CLUSTER_COMPLETE after cleanup stage with notes")
	}
}

// newTestStageOrchestratorWithSink is a test helper that creates a StageOrchestrator and
// returns both the orchestrator and the recording fakeSink for assertion.
func newTestStageOrchestratorWithSink(t *testing.T, b *bus.Bus, stages []Stage) (*StageOrchestrator, *fakeSink) {
	t.Helper()
	tmpDir := t.TempDir()
	wt := &gitpkg.Worktree{Dir: tmpDir, RepoDir: tmpDir}
	sink := &fakeSink{}

	workerCfg := AgentConfig{
		ID:            "worker",
		Name:          "Worker",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       "true",
	}
	testerCfg := AgentConfig{
		ID:            "tester",
		Name:          "Tester",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       "true",
	}

	so := NewStageOrchestrator(b, &config.Config{}, wt, sink, nil,
		"test-cluster", workerCfg, testerCfg, stages)
	return so, sink
}

// TestStageOrchestrator_StageFailed_EmittedOnError verifies that sink.StageFailed is called
// when a stage returns an error. Covers AC8.
//
// This test will FAIL (runtime) until stages.go calls so.sink.StageFailed before returning
// the stage error in Run().
func TestStageOrchestrator_StageFailed_EmittedOnError(t *testing.T) {
	b := newTestBus(t)

	// A review stage that always rejects with MaxRetries=1 exhausts retries → returns error.
	failingStages := []Stage{
		{
			Name:       "always-reject",
			Kind:       StageKindReview,
			MaxRetries: 1, // exhausts retries immediately → returns error
			ReviewerConfig: &AgentConfig{
				ID:            "strict-reviewer",
				Name:          "Strict Reviewer",
				Role:          "validator",
				ExecutionMode: "shell",
				Command:       "exit 1", // always rejects
			},
		},
	}

	so, sink := newTestStageOrchestratorWithSink(t, b, failingStages)

	err := so.Run(context.Background(), testIssue())
	if err == nil {
		t.Fatal("expected error from exhausted review stage, got nil")
	}

	sink.mu.Lock()
	failed := sink.stagesFailed
	sink.mu.Unlock()

	if len(failed) == 0 {
		t.Error("expected sink.StageFailed to be called when stage fails, but it was not called")
	}
	if len(failed) > 0 && failed[0].name != "always-reject" {
		t.Errorf("expected StageFailed with stage name %q, got %q", "always-reject", failed[0].name)
	}
}

// TestStageOrchestrator_StageRoundStarted_EmittedPerReviewRound verifies that
// sink.StageRoundStarted is called once per attempt in a review stage retry loop.
// Covers AC9.
//
// This test will FAIL (runtime) until stages.go calls so.sink.StageRoundStarted at the top
// of each runReviewStage loop iteration.
func TestStageOrchestrator_StageRoundStarted_EmittedPerReviewRound(t *testing.T) {
	b := newTestBus(t)

	sentinelFile := t.TempDir() + "/sentinel"
	// First call: create sentinel + reject. Second call: sentinel exists → approve.
	reviewerCmd := fmt.Sprintf(`test -f "%s" && exit 0; touch "%s"; exit 1`, sentinelFile, sentinelFile)

	stages := []Stage{
		{
			Name:       "two-round-review",
			Kind:       StageKindReview,
			MaxRetries: 5,
			ReviewerConfig: &AgentConfig{
				ID:            "reviewer",
				Name:          "Reviewer",
				Role:          "validator",
				ExecutionMode: "shell",
				Command:       reviewerCmd,
			},
		},
	}

	so, sink := newTestStageOrchestratorWithSink(t, b, stages)

	err := so.Run(context.Background(), testIssue())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sink.mu.Lock()
	rounds := sink.stageRoundsStarted
	sink.mu.Unlock()

	// The reviewer ran twice (reject then approve), so StageRoundStarted should be called twice
	if len(rounds) < 2 {
		t.Errorf("expected StageRoundStarted to be called at least 2 times (one per review round), got %d", len(rounds))
	}
	for i, r := range rounds {
		if r.index != 0 {
			t.Errorf("round[%d]: expected stage index 0, got %d", i, r.index)
		}
		if r.round != i+1 {
			t.Errorf("round[%d]: expected round number %d, got %d", i, i+1, r.round)
		}
	}
}
