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

	if !contains(result, "## Acceptance Criteria") {
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

	if contains(result, "## Acceptance Criteria") {
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

	if !contains(result, "## Acceptance Criteria") {
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
