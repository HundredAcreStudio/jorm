package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
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

// --- CollectReviewerNotes tests ---

// TestCollectReviewerNotes_NoApprovedMessages verifies that an empty bus returns an empty slice.
func TestCollectReviewerNotes_NoApprovedMessages(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	notes, err := CollectReviewerNotes(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("expected empty notes slice, got %v", notes)
	}
}

// TestCollectReviewerNotes_ApprovedWithNotes verifies that LOW: lines are extracted from
// approved VALIDATION_RESULT messages.
func TestCollectReviewerNotes_ApprovedWithNotes(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "pr-reviewer",
		Content:   "VERDICT: ACCEPT\nLOW: json.Unmarshal error silently discarded\nLOW: Consider adding a timeout",
		Data:      map[string]any{"approved": true},
	})

	notes, err := CollectReviewerNotes(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notes) == 0 {
		t.Fatal("expected notes to be extracted from approved VALIDATION_RESULT")
	}

	var foundUnmarshal, foundTimeout bool
	for _, n := range notes {
		if contains(n, "json.Unmarshal error silently discarded") {
			foundUnmarshal = true
		}
		if contains(n, "Consider adding a timeout") {
			foundTimeout = true
		}
	}
	if !foundUnmarshal {
		t.Error("expected note about json.Unmarshal to be extracted")
	}
	if !foundTimeout {
		t.Error("expected note about timeout to be extracted")
	}
}

// TestCollectReviewerNotes_JSONEmbeddedNotes verifies that LOW: notes inside a JSON
// "notes" array are extracted — this is the actual format reviewers produce.
func TestCollectReviewerNotes_JSONEmbeddedNotes(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	// This is the actual format from pr-review.md output
	content := `I've reviewed the diff.

` + "```json\n" + `{
  "approved": true,
  "errors": [],
  "notes": [
    "LOW: db.go:108 — json.Unmarshal error is silently discarded",
    "LOW: tools.go:43 — var out stays nil producing null instead of empty array"
  ]
}
` + "```\n\nVERDICT: ACCEPT"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "pr-reviewer",
		Content:   content,
		Data:      map[string]any{"approved": true},
	})

	notes, err := CollectReviewerNotes(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notes) == 0 {
		t.Fatal("expected notes from JSON-embedded format")
	}

	var foundUnmarshal, foundNull bool
	for _, n := range notes {
		if contains(n, "json.Unmarshal error") {
			foundUnmarshal = true
		}
		if contains(n, "null instead of empty array") {
			foundNull = true
		}
	}
	if !foundUnmarshal {
		t.Errorf("expected json.Unmarshal note, got: %v", notes)
	}
	if !foundNull {
		t.Errorf("expected null/empty array note, got: %v", notes)
	}
}

// TestCollectReviewerNotes_RejectedMessagesIgnored verifies that LOW: lines from rejected
// VALIDATION_RESULT messages are not collected.
func TestCollectReviewerNotes_RejectedMessagesIgnored(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "pr-reviewer",
		Content:   "VERDICT: REJECT\nLOW: This is from a rejection and should be ignored",
		Data:      map[string]any{"approved": false},
	})

	notes, err := CollectReviewerNotes(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("expected no notes from rejected validation results, got %v", notes)
	}
}

// TestCollectReviewerNotes_MixedApprovalStatuses verifies that only notes from approved
// messages are collected when both approved and rejected messages exist.
func TestCollectReviewerNotes_MixedApprovalStatuses(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "pr-reviewer",
		Content:   "VERDICT: ACCEPT\nLOW: approved-note",
		Data:      map[string]any{"approved": true},
	})
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "security-reviewer",
		Content:   "VERDICT: REJECT\nLOW: rejected-note",
		Data:      map[string]any{"approved": false},
	})

	notes, err := CollectReviewerNotes(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, n := range notes {
		if contains(n, "rejected-note") {
			t.Error("expected rejected-note to be excluded from collected notes")
		}
	}

	var foundApproved bool
	for _, n := range notes {
		if contains(n, "approved-note") {
			foundApproved = true
		}
	}
	if !foundApproved {
		t.Error("expected approved-note to be included in collected notes")
	}
}

// TestCollectReviewerNotes_DeduplicatesNotes verifies that duplicate LOW: lines across
// multiple approved messages appear only once in the result.
func TestCollectReviewerNotes_DeduplicatesNotes(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "pr-reviewer",
		Content:   "VERDICT: ACCEPT\nLOW: duplicate note",
		Data:      map[string]any{"approved": true},
	})
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "security-reviewer",
		Content:   "VERDICT: ACCEPT\nLOW: duplicate note",
		Data:      map[string]any{"approved": true},
	})

	notes, err := CollectReviewerNotes(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, n := range notes {
		if contains(n, "duplicate note") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected duplicate note to appear exactly once, got %d occurrences", count)
	}
}

// --- BuildCleanupWorkerContext tests ---

// TestBuildCleanupWorkerContext_NoNotes verifies that an empty string is returned when
// no approved VALIDATION_RESULT messages exist.
func TestBuildCleanupWorkerContext_NoNotes(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	result, err := BuildCleanupWorkerContext(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string when no notes exist, got %q", result)
	}
}

// TestBuildCleanupWorkerContext_WithNotes verifies that the prompt contains all collected
// notes and the cleanup task header.
func TestBuildCleanupWorkerContext_WithNotes(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "pr-reviewer",
		Content:   "VERDICT: ACCEPT\nLOW: json.Unmarshal error silently discarded",
		Data:      map[string]any{"approved": true},
	})
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "security-reviewer",
		Content:   "VERDICT: ACCEPT\nLOW: Consider rate limiting",
		Data:      map[string]any{"approved": true},
	})

	result, err := BuildCleanupWorkerContext(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty cleanup context when notes exist")
	}

	if !contains(result, "Cleanup") {
		t.Error("expected cleanup task header in context")
	}
	if !contains(result, "json.Unmarshal error silently discarded") {
		t.Error("expected first note in cleanup context")
	}
	if !contains(result, "Consider rate limiting") {
		t.Error("expected second note in cleanup context")
	}
}

// TestBuildCleanupWorkerContext_IncludesIssueAndPlan verifies that the cleanup context
// includes issue and plan sections alongside the notes.
func TestBuildCleanupWorkerContext_IncludesIssueAndPlan(t *testing.T) {
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
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicValidationResult,
		Sender:    "pr-reviewer",
		Content:   "VERDICT: ACCEPT\nLOW: Add missing error check",
		Data:      map[string]any{"approved": true},
	})

	result, err := BuildCleanupWorkerContext(b, clusterID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "Fix the login bug") {
		t.Error("expected issue content in cleanup context")
	}
	if !contains(result, "Step 1: do this") {
		t.Error("expected plan content in cleanup context")
	}
	if !contains(result, "Add missing error check") {
		t.Error("expected note in cleanup context")
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

// --- BuildRichValidatorContext tests ---

// TestBuildRichValidatorContext_IncludesChangedFileContent verifies that the full content
// of a file referenced in the diff is included in the output.
func TestBuildRichValidatorContext_IncludesChangedFileContent(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"
	workDir := t.TempDir()

	fileContent := "package main\n\nfunc Hello() string { return \"hello\" }\n"
	if err := os.WriteFile(filepath.Join(workDir, "hello.go"), []byte(fileContent), 0644); err != nil {
		t.Fatal(err)
	}

	diff := "diff --git a/hello.go b/hello.go\n--- a/hello.go\n+++ b/hello.go\n@@ -1 +1 @@\n+package main\n"
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Content:   diff,
		Data:      map[string]any{},
	})

	result, err := BuildRichValidatorContext(b, clusterID, workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "## Changed Files (full content)") {
		t.Error("expected '## Changed Files (full content)' section")
	}
	if !contains(result, fileContent) {
		t.Error("expected full file content to be included in output")
	}
}

// TestBuildRichValidatorContext_IncludesCLAUDEMD verifies that CLAUDE.md from the workDir
// is included under "Project Conventions".
func TestBuildRichValidatorContext_IncludesCLAUDEMD(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"
	workDir := t.TempDir()

	claudeMDContent := "# Project Conventions\n\nUse gofmt. Write tests.\n"
	if err := os.WriteFile(filepath.Join(workDir, "CLAUDE.md"), []byte(claudeMDContent), 0644); err != nil {
		t.Fatal(err)
	}

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Content:   "diff --git a/foo.go b/foo.go\n",
		Data:      map[string]any{},
	})

	result, err := BuildRichValidatorContext(b, clusterID, workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "## Project Conventions (CLAUDE.md)") {
		t.Error("expected '## Project Conventions (CLAUDE.md)' section")
	}
	if !contains(result, claudeMDContent) {
		t.Error("expected CLAUDE.md content in output")
	}
}

// TestBuildRichValidatorContext_IncludesGoMod verifies that go.mod from the workDir
// is included under "Dependency Manifest".
func TestBuildRichValidatorContext_IncludesGoMod(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"
	workDir := t.TempDir()

	goModContent := "module github.com/example/project\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Content:   "diff --git a/foo.go b/foo.go\n",
		Data:      map[string]any{},
	})

	result, err := BuildRichValidatorContext(b, clusterID, workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "## Dependency Manifest (go.mod)") {
		t.Error("expected '## Dependency Manifest (go.mod)' section")
	}
	if !contains(result, goModContent) {
		t.Error("expected go.mod content in output")
	}
}

// TestBuildRichValidatorContext_IncludesTestFile verifies that when a .go file is referenced
// in the diff, the corresponding _test.go file is also included if it exists.
func TestBuildRichValidatorContext_IncludesTestFile(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"
	workDir := t.TempDir()

	srcContent := "package foo\n\nfunc Add(a, b int) int { return a + b }\n"
	testContent := "package foo\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(workDir, "foo.go"), []byte(srcContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "foo_test.go"), []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	diff := "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n+package foo\n"
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Content:   diff,
		Data:      map[string]any{},
	})

	result, err := BuildRichValidatorContext(b, clusterID, workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, srcContent) {
		t.Error("expected source file content in output")
	}
	if !contains(result, testContent) {
		t.Error("expected test file content to be auto-included in output")
	}
}

// TestBuildRichValidatorContext_SkipsDeletedFiles verifies that when the diff references
// a file that doesn't exist in workDir, the function returns without error.
func TestBuildRichValidatorContext_SkipsDeletedFiles(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"
	workDir := t.TempDir()

	diff := "diff --git a/deleted.go b/deleted.go\n--- a/deleted.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-package old\n"
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Content:   diff,
		Data:      map[string]any{},
	})

	result, err := BuildRichValidatorContext(b, clusterID, workDir)
	if err != nil {
		t.Fatalf("expected no error for deleted/missing file, got: %v", err)
	}

	// Result may be empty or contain the diff section, but must not panic
	_ = result
}

// TestBuildRichValidatorContext_SkipsMissingCLAUDEMD verifies that when CLAUDE.md is absent,
// the "Project Conventions" section is not included.
func TestBuildRichValidatorContext_SkipsMissingCLAUDEMD(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"
	workDir := t.TempDir()

	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Content:   "diff --git a/foo.go b/foo.go\n",
		Data:      map[string]any{},
	})

	result, err := BuildRichValidatorContext(b, clusterID, workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if contains(result, "## Project Conventions (CLAUDE.md)") {
		t.Error("expected no 'Project Conventions' section when CLAUDE.md is absent")
	}
}

// TestBuildRichValidatorContext_TruncatesLargeFiles verifies that files larger than 100KB
// are truncated with a "[...truncated" notice.
func TestBuildRichValidatorContext_TruncatesLargeFiles(t *testing.T) {
	b := newTestBus(t)
	clusterID := "test-cluster"
	workDir := t.TempDir()

	// Write a 200KB file (well over the 100KB cap)
	largeContent := strings.Repeat("x", 200*1024)
	if err := os.WriteFile(filepath.Join(workDir, "large.go"), []byte(largeContent), 0644); err != nil {
		t.Fatal(err)
	}

	diff := "diff --git a/large.go b/large.go\n--- a/large.go\n+++ b/large.go\n@@ -1 +1 @@\n+x\n"
	b.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Content:   diff,
		Data:      map[string]any{},
	})

	result, err := BuildRichValidatorContext(b, clusterID, workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "[...truncated") {
		t.Error("expected '[...truncated' notice for file exceeding 100KB cap")
	}
}
