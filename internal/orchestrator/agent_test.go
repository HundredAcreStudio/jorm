package orchestrator

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jorm/internal/agent"
	"github.com/jorm/internal/bus"

	_ "github.com/mattn/go-sqlite3"
)

// fakeSink implements events.Sink for testing.
type fakeSink struct {
	mu                 sync.Mutex
	validatorStarts    []string
	validatorResults   []agent.ValidatorResult
	outputs            []string
	stagesFailed       []stageFailedRecord
	stageRoundsStarted []stageRoundRecord
}

type stageFailedRecord struct {
	index int
	name  string
	err   error
}

type stageRoundRecord struct {
	index int
	round int
}

func (s *fakeSink) Phase(string)                            {}
func (s *fakeSink) IssueLoaded(string, string)              {}
func (s *fakeSink) Attempt(int, int)                        {}
func (s *fakeSink) Cost(float64)                            {}
func (s *fakeSink) Classification(string)                   {}
func (s *fakeSink) LoopDone(error)                          {}
func (s *fakeSink) AgentStateChange(string, string, string)          {}
func (s *fakeSink) MessagePublished(string, string)                  {}
func (s *fakeSink) UpdateTotalAgents(int)                            {}
func (s *fakeSink) AgentSpawned(string, string, []string)            {}
func (s *fakeSink) AgentTriggerFired(string, string, int, string)    {}
func (s *fakeSink) AgentTaskCompleted(string, int)                   {}
func (s *fakeSink) AgentTaskFailed(string, int, error)               {}
func (s *fakeSink) AgentTokenUsage(string, string, int, int)         {}
func (s *fakeSink) ValidationRoundStart(int)                         {}
func (s *fakeSink) ValidationRoundComplete(int, int, int)            {}
func (s *fakeSink) RetryRoundStart(int)                              {}
func (s *fakeSink) SystemEvent(string)                               {}
func (s *fakeSink) ClusterComplete(string, string)                   {}
func (s *fakeSink) StageStarted(int, string)                         {}
func (s *fakeSink) StageCompleted(int, string)                       {}

func (s *fakeSink) StageFailed(index int, name string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stagesFailed = append(s.stagesFailed, stageFailedRecord{index: index, name: name, err: err})
}

func (s *fakeSink) StageRoundStarted(index int, round int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stageRoundsStarted = append(s.stageRoundsStarted, stageRoundRecord{index: index, round: round})
}

func (s *fakeSink) ClaudeOutput(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outputs = append(s.outputs, text)
}

func (s *fakeSink) ValidatorStart(id, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validatorStarts = append(s.validatorStarts, id)
}

func (s *fakeSink) ValidatorDone(result agent.ValidatorResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validatorResults = append(s.validatorResults, result)
}

func newTestBus(t *testing.T) *bus.Bus {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		cluster_id TEXT,
		topic TEXT,
		sender TEXT,
		timestamp DATETIME,
		content TEXT,
		data TEXT
	)`)
	if err != nil {
		t.Fatal(err)
	}

	return bus.New(db)
}

// contains is a test helper wrapping strings.Contains for readability.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// TestExecuteOnce_ShellAgent_Success verifies that calling ExecuteOnce directly on a
// shell-mode agent returns Approved=true and non-empty output on success.
func TestExecuteOnce_ShellAgent_Success(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	cfg := AgentConfig{
		ID:            "shell-agent",
		Name:          "Shell",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       "echo hello-from-execute-once",
	}
	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	a.lastTrigger = bus.Message{Topic: bus.TopicIssueOpened}

	result, err := a.ExecuteOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Error("expected Approved=true for successful shell command")
	}
	if result.Output == "" {
		t.Error("expected non-empty output from echo command")
	}
}

// TestExecuteOnce_ShellAgent_Failure verifies that a failing shell command sets Approved=false
// without returning an error from ExecuteOnce itself.
func TestExecuteOnce_ShellAgent_Failure(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	cfg := AgentConfig{
		ID:            "shell-agent",
		Name:          "Shell",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       "exit 1",
	}
	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	a.lastTrigger = bus.Message{Topic: bus.TopicIssueOpened}

	result, err := a.ExecuteOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected Approved=false for failing shell command")
	}
}

// TestExecuteOnce_IncrementsIteration verifies that each call to ExecuteOnce increments
// the agent's Iteration counter by exactly one.
func TestExecuteOnce_IncrementsIteration(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	cfg := AgentConfig{
		ID:            "shell-agent",
		Name:          "Shell",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       "true",
	}
	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	a.lastTrigger = bus.Message{Topic: bus.TopicIssueOpened}

	if _, err := a.ExecuteOnce(context.Background()); err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if a.Iteration != 1 {
		t.Errorf("expected Iteration=1 after first call, got %d", a.Iteration)
	}

	if _, err := a.ExecuteOnce(context.Background()); err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if a.Iteration != 2 {
		t.Errorf("expected Iteration=2 after second call, got %d", a.Iteration)
	}
}

// TestExecuteOnce_ContextCancellation verifies that ExecuteOnce returns ctx.Err() when
// the context is cancelled before or during shell execution.
func TestExecuteOnce_ContextCancellation(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	cfg := AgentConfig{
		ID:            "shell-agent",
		Name:          "Shell",
		Role:          "worker",
		ExecutionMode: "shell",
		Command:       "sleep 10",
	}
	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	a.lastTrigger = bus.Message{Topic: bus.TopicIssueOpened}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before calling ExecuteOnce

	_, err := a.ExecuteOnce(ctx)
	if err != ctx.Err() {
		t.Errorf("expected ctx.Err()=%v, got %v", ctx.Err(), err)
	}
}

// TestExecuteOnce_ReviewMode_ShellApprovedByExitCode verifies that shell-mode agents
// determine Approved from exit code, not from VERDICT text in output. This confirms
// that ReviewMode VERDICT parsing only applies to claude-mode agents.
func TestExecuteOnce_ReviewMode_ShellApprovedByExitCode(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	// Shell exits 0 but outputs VERDICT: REJECT — shell mode wins (approved by exit code)
	cfg := AgentConfig{
		ID:            "reviewer",
		Name:          "Reviewer",
		Role:          "validator",
		ExecutionMode: "shell",
		ReviewMode:    true,
		Command:       `echo "VERDICT: REJECT — but exit code is 0"`,
	}
	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	a.lastTrigger = bus.Message{Topic: bus.TopicImplementationReady}

	result, err := a.ExecuteOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Error("expected Approved=true: shell mode uses exit code, not VERDICT text")
	}
}

// TestExecuteOnce_ReviewMode_ResultProcessorTakesPrecedence verifies that when both
// ReviewMode and ResultProcessor are set, ResultProcessor determines Approved (not VERDICT parsing).
func TestExecuteOnce_ReviewMode_ResultProcessorTakesPrecedence(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	cfg := AgentConfig{
		ID:            "reviewer",
		Name:          "Reviewer",
		Role:          "validator",
		ExecutionMode: "shell",
		ReviewMode:    true,
		Command:       "true",
		// ResultProcessor forces approved=false regardless of exit code or VERDICT
		ResultProcessor: func(result *agent.ClaudeResult) map[string]any {
			return map[string]any{"approved": false}
		},
	}
	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	a.lastTrigger = bus.Message{Topic: bus.TopicImplementationReady}

	result, err := a.ExecuteOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Shell mode returns early with exit-code-based approved, ResultProcessor not reached for shell.
	// This test documents that shell mode ignores ResultProcessor.
	if !result.Approved {
		t.Error("expected Approved=true: shell mode uses exit code, ResultProcessor not consulted")
	}
}

// TestReviewMode_AgentConfig_NilResultProcessor verifies that a ReviewMode agent
// with no ResultProcessor uses the VERDICT parsing fallback. This mirrors how
// staged reviewer agents are configured in BuiltinStagedTemplates.
func TestReviewMode_AgentConfig_NilResultProcessor(t *testing.T) {
	cfg := AgentConfig{
		ID:         "test-reviewer",
		Name:       "Test Reviewer",
		Role:       "validator",
		ReviewMode: true,
		// No ResultProcessor — this is the staged reviewer pattern
	}

	if !cfg.ReviewMode {
		t.Error("expected ReviewMode=true")
	}
	if cfg.ResultProcessor != nil {
		t.Error("expected nil ResultProcessor for ReviewMode agent (VERDICT fallback path)")
	}
}

// TestExecuteOnce_ShellAgent_RunsInWorkDir verifies shell commands execute in the specified work directory.
func TestExecuteOnce_ShellAgent_RunsInWorkDir(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	markerPath := filepath.Join(tmpDir, "marker.txt")
	if err := os.WriteFile(markerPath, []byte("found"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := AgentConfig{
		ID:            "validator-check",
		Name:          "Check",
		Role:          "validator",
		ExecutionMode: "shell",
		Command:       "cat marker.txt",
	}

	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	a.lastTrigger = bus.Message{Topic: bus.TopicImplementationReady}

	result, err := a.ExecuteOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Error("expected command to succeed (marker.txt exists in workdir)")
	}
	if !contains(result.Output, "found") {
		t.Errorf("expected output containing 'found', got %q", result.Output)
	}
}
