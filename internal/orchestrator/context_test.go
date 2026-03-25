package orchestrator

import (
	"testing"

	"github.com/jorm/internal/bus"

	_ "github.com/mattn/go-sqlite3"
)

func TestBuildValidatorContext_IncludesAcceptanceCriteria(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	criteria := "AC1: Build passes\nAC2: Tests pass\nAC3: No lint errors"
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicPlanReady,
		Sender:    "planner",
		Content:   "Implementation plan content",
		Data: map[string]any{
			"acceptance_criteria": criteria,
		},
	})

	result, err := BuildValidatorContext(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "## Acceptance Criteria (from planner)") {
		t.Error("expected output to contain '## Acceptance Criteria' section")
	}
	if !contains(result, "AC1: Build passes") {
		t.Error("expected output to contain criteria text 'AC1: Build passes'")
	}
	if !contains(result, "AC3: No lint errors") {
		t.Error("expected output to contain criteria text 'AC3: No lint errors'")
	}
}

func TestBuildValidatorContext_NoPlanMessage(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	result, err := BuildValidatorContext(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With no messages at all, result should be empty
	if result != "" {
		t.Errorf("expected empty result with no messages, got %q", result)
	}
}

func TestBuildValidatorContext_PlanWithoutCriteria(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicPlanReady,
		Sender:    "planner",
		Content:   "Plan without acceptance criteria in Data",
		Data:      map[string]any{},
	})

	result, err := BuildValidatorContext(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if contains(result, "## Acceptance Criteria (from planner)") {
		t.Error("expected no Acceptance Criteria section when plan has no criteria in Data")
	}
}

func TestBuildValidatorContext_IncludesDiff(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	diffContent := "diff --git a/main.go b/main.go\n+added line"
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Content:   diffContent,
		Data:      map[string]any{},
	})

	result, err := BuildValidatorContext(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "## Implementation") {
		t.Error("expected output to contain '## Implementation' section")
	}
	if !contains(result, diffContent) {
		t.Error("expected output to contain the diff content")
	}
}

func TestBuildValidatorContext_CriteriaAndDiff(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	criteria := "AC1: Must compile"
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicPlanReady,
		Sender:    "planner",
		Content:   "plan",
		Data:      map[string]any{"acceptance_criteria": criteria},
	})
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Content:   "the diff",
		Data:      map[string]any{},
	})

	result, err := BuildValidatorContext(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "## Acceptance Criteria (from planner)") {
		t.Error("expected Acceptance Criteria section")
	}
	if !contains(result, "## Implementation") {
		t.Error("expected Implementation section")
	}
	if !contains(result, criteria) {
		t.Error("expected criteria text in output")
	}
	if !contains(result, "the diff") {
		t.Error("expected diff text in output")
	}
}

func TestBuildTestWriterContext_IssueAndPlan(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicIssueOpened,
		Sender:    "provider",
		Content:   "Fix the login bug",
		Data:      map[string]any{},
	})
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicPlanReady,
		Sender:    "planner",
		Content:   "Step 1: do this",
		Data:      map[string]any{},
	})

	result, err := BuildTestWriterContext(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "## Issue") {
		t.Error("expected '## Issue' section")
	}
	if !contains(result, "Fix the login bug") {
		t.Error("expected issue content")
	}
	if !contains(result, "## Plan") {
		t.Error("expected '## Plan' section")
	}
	if !contains(result, "Step 1: do this") {
		t.Error("expected plan content")
	}
}

func TestBuildTestWriterContext_NoPlan(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicIssueOpened,
		Sender:    "provider",
		Content:   "Some issue",
		Data:      map[string]any{},
	})

	result, err := BuildTestWriterContext(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "## Issue") {
		t.Error("expected '## Issue' section")
	}
	if contains(result, "## Plan") {
		t.Error("expected no '## Plan' section when plan not published")
	}
}

func TestBuildStageScopedWorkerContext_FiltersOtherStages(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicIssueOpened,
		Sender:    "provider",
		Content:   "Issue content",
		Data:      map[string]any{},
	})

	// Rejection from stage 0 (should be excluded when querying stage 1)
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "stage0-reviewer",
		Content:   "Stage 0 rejection",
		Data:      map[string]any{"approved": false, "stage_index": 0},
	})

	// Rejection from stage 1 (should be included)
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "stage1-reviewer",
		Content:   "Stage 1 rejection",
		Data:      map[string]any{"approved": false, "stage_index": 1},
	})

	result, err := BuildStageScopedWorkerContext(b, clusterID, 1, "impl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if contains(result, "Stage 0 rejection") {
		t.Error("expected stage 0 rejection to be excluded")
	}
	if !contains(result, "Stage 1 rejection") {
		t.Error("expected stage 1 rejection to be included")
	}
}

func TestBuildStageScopedWorkerContext_NoRejections(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicIssueOpened,
		Sender:    "provider",
		Content:   "Issue content",
		Data:      map[string]any{},
	})

	result, err := BuildStageScopedWorkerContext(b, clusterID, 0, "tests")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if contains(result, "## Previous attempt") {
		t.Error("expected no rejection section when no rejections exist")
	}
	if !contains(result, "## Issue") {
		t.Error("expected '## Issue' section")
	}
}

func TestBuildStageScopedWorkerContext_ExcludesApproved(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicIssueOpened,
		Sender:    "provider",
		Content:   "Issue",
		Data:      map[string]any{},
	})
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "reviewer",
		Content:   "Looks good",
		Data:      map[string]any{"approved": true, "stage_index": 0},
	})

	result, err := BuildStageScopedWorkerContext(b, clusterID, 0, "tests")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if contains(result, "## Previous attempt") {
		t.Error("expected no rejection section for approved validation")
	}
}

func TestBuildStageScopedWorkerContext_Float64StageIndex(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicIssueOpened,
		Sender:    "provider",
		Content:   "Issue",
		Data:      map[string]any{},
	})
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "reviewer",
		Content:   "Rejection via JSON",
		Data:      map[string]any{"approved": false, "stage_index": float64(1)},
	})

	result, err := BuildStageScopedWorkerContext(b, clusterID, 1, "review")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "Rejection via JSON") {
		t.Error("expected float64 stage_index to match")
	}
}
