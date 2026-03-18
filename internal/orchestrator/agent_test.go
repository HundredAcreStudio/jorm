package orchestrator

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jorm/internal/agent"
	"github.com/jorm/internal/bus"
	"github.com/jorm/internal/config"

	_ "github.com/mattn/go-sqlite3"
)

// fakeSink implements events.Sink for testing.
type fakeSink struct {
	mu               sync.Mutex
	validatorStarts  []string
	validatorResults []agent.ValidatorResult
	outputs          []string
}

func (s *fakeSink) Phase(string)                            {}
func (s *fakeSink) IssueLoaded(string, string)              {}
func (s *fakeSink) Attempt(int, int)                        {}
func (s *fakeSink) Cost(float64)                            {}
func (s *fakeSink) Classification(string)                   {}
func (s *fakeSink) LoopDone(error)                          {}
func (s *fakeSink) AgentStateChange(string, string, string) {}
func (s *fakeSink) MessagePublished(string, string)         {}

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
	t.Cleanup(func() { db.Close() })

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

// TestShellAgent_ExecutesCommand verifies that shell execution mode runs the actual
// command via exec.Command instead of delegating to RunClaude.
// This is the core regression test for the root cause: previously ALL validators
// were run through Claude regardless of type.
func TestShellAgent_ExecutesCommand(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	cfg := AgentConfig{
		ID:            "validator-build",
		Name:          "Build",
		Role:          "validator",
		ExecutionMode: "shell",
		Command:       "echo hello-from-shell",
		Triggers:      []Trigger{{Topic: bus.TopicImplementationReady, Predicate: "always"}},
		MaxIterations: 1,
		OnComplete:    []OnCompleteAction{{Topic: bus.TopicValidationResult}},
	}

	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)

	resultCh := b.Subscribe(bus.TopicValidationResult)
	defer b.Unsubscribe(bus.TopicValidationResult, resultCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	b.Publish(bus.Message{
		ClusterID: "test-cluster",
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Content:   "implementation ready",
		Data:      map[string]any{},
	})

	select {
	case msg := <-resultCh:
		approved, ok := msg.Data["approved"].(bool)
		if !ok || !approved {
			t.Errorf("expected approved=true, got %v", msg.Data["approved"])
		}
		if msg.Content == "" {
			t.Error("expected non-empty content from shell output")
		}
		if msg.Data["validator_id"] != "validator-build" {
			t.Errorf("expected validator_id=validator-build, got %v", msg.Data["validator_id"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for VALIDATION_RESULT")
	}

	cancel()
	<-done

	sink.mu.Lock()
	defer sink.mu.Unlock()

	if len(sink.validatorStarts) == 0 {
		t.Error("expected ValidatorStart to be called for shell validator")
	}
	if len(sink.validatorResults) == 0 {
		t.Fatal("expected ValidatorDone to be called for shell validator")
	}
	if !sink.validatorResults[0].Passed {
		t.Error("expected validator result to be Passed=true")
	}
}

// TestShellAgent_FailingCommand verifies that a failing shell command sets approved=false.
func TestShellAgent_FailingCommand(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	cfg := AgentConfig{
		ID:            "validator-test",
		Name:          "Tests",
		Role:          "validator",
		ExecutionMode: "shell",
		Command:       "exit 1",
		Triggers:      []Trigger{{Topic: bus.TopicImplementationReady, Predicate: "always"}},
		MaxIterations: 1,
		OnComplete:    []OnCompleteAction{{Topic: bus.TopicValidationResult}},
	}

	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	resultCh := b.Subscribe(bus.TopicValidationResult)
	defer b.Unsubscribe(bus.TopicValidationResult, resultCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	b.Publish(bus.Message{
		ClusterID: "test-cluster",
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Data:      map[string]any{},
	})

	select {
	case msg := <-resultCh:
		approved, _ := msg.Data["approved"].(bool)
		if approved {
			t.Error("expected approved=false for failing command")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for VALIDATION_RESULT")
	}

	cancel()
	<-done

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.validatorResults) == 0 || sink.validatorResults[0].Passed {
		t.Error("expected ValidatorDone with Passed=false")
	}
}

// TestPassthroughAgent_CompletionDetector verifies that the passthrough execution mode
// processes trigger messages without running Claude, and the completion TriggerProcessor
// correctly waits for all validators before publishing CLUSTER_COMPLETE.
func TestPassthroughAgent_CompletionDetector(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	expectedCount := 2
	approvedSet := &sync.Map{}

	cfg := AgentConfig{
		ID:            "completion",
		Name:          "Completion",
		Role:          "completion",
		ExecutionMode: "passthrough",
		Triggers:      []Trigger{{Topic: bus.TopicValidationResult, Predicate: "always"}},
		MaxIterations: 0,
		OnComplete:    []OnCompleteAction{{Topic: bus.TopicClusterComplete}},
		TriggerProcessor: func(msg bus.Message) (map[string]any, bool) {
			approved, _ := msg.Data["approved"].(bool)
			validatorID, _ := msg.Data["validator_id"].(string)
			if !approved {
				approvedSet = &sync.Map{}
				return nil, false
			}
			approvedSet.Store(validatorID, true)
			count := 0
			approvedSet.Range(func(_, _ any) bool { count++; return true })
			if count >= expectedCount {
				return map[string]any{"approved": true}, true
			}
			return nil, false
		},
	}

	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	completeCh := b.Subscribe(bus.TopicClusterComplete)
	defer b.Unsubscribe(bus.TopicClusterComplete, completeCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)

	// First validator approves — should NOT trigger CLUSTER_COMPLETE yet.
	b.Publish(bus.Message{
		ClusterID: "test-cluster",
		Topic:     bus.TopicValidationResult,
		Sender:    "validator-build",
		Data:      map[string]any{"approved": true, "validator_id": "validator-build"},
	})

	select {
	case <-completeCh:
		t.Fatal("CLUSTER_COMPLETE should NOT fire after only 1 of 2 validators approve")
	case <-time.After(200 * time.Millisecond):
		// expected
	}

	// Second validator approves — now CLUSTER_COMPLETE should fire.
	b.Publish(bus.Message{
		ClusterID: "test-cluster",
		Topic:     bus.TopicValidationResult,
		Sender:    "validator-tests",
		Data:      map[string]any{"approved": true, "validator_id": "validator-tests"},
	})

	select {
	case msg := <-completeCh:
		approved, _ := msg.Data["approved"].(bool)
		if !approved {
			t.Error("expected CLUSTER_COMPLETE with approved=true")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for CLUSTER_COMPLETE")
	}

	cancel()
	<-done
}

// TestPassthroughAgent_RejectionDoesNotPublishComplete verifies that a validator rejection
// does NOT publish CLUSTER_COMPLETE, and that subsequent approvals from all validators
// DO publish CLUSTER_COMPLETE with approved=true. This proves the retry loop is not
// short-circuited by rejections.
func TestPassthroughAgent_RejectionDoesNotPublishComplete(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	expectedCount := 2
	approvedSet := &sync.Map{}

	cfg := AgentConfig{
		ID:            "completion",
		Name:          "Completion",
		Role:          "completion",
		ExecutionMode: "passthrough",
		Triggers:      []Trigger{{Topic: bus.TopicValidationResult, Predicate: "always"}},
		MaxIterations: 0,
		OnComplete:    []OnCompleteAction{{Topic: bus.TopicClusterComplete}},
		TriggerProcessor: func(msg bus.Message) (map[string]any, bool) {
			approved, _ := msg.Data["approved"].(bool)
			validatorID, _ := msg.Data["validator_id"].(string)
			if !approved {
				approvedSet = &sync.Map{}
				return nil, false
			}
			approvedSet.Store(validatorID, true)
			count := 0
			approvedSet.Range(func(_, _ any) bool { count++; return true })
			if count >= expectedCount {
				return map[string]any{"approved": true}, true
			}
			return nil, false
		},
	}

	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	completeCh := b.Subscribe(bus.TopicClusterComplete)
	defer b.Unsubscribe(bus.TopicClusterComplete, completeCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)

	// Send a rejection — CLUSTER_COMPLETE must NOT be published.
	b.Publish(bus.Message{
		ClusterID: "test-cluster",
		Topic:     bus.TopicValidationResult,
		Sender:    "validator-build",
		Data:      map[string]any{"approved": false, "validator_id": "validator-build"},
	})

	select {
	case <-completeCh:
		t.Fatal("CLUSTER_COMPLETE must NOT be published on rejection")
	case <-time.After(200 * time.Millisecond):
		// expected: no CLUSTER_COMPLETE on rejection
	}

	// Now simulate a new validation round where both validators approve.
	b.Publish(bus.Message{
		ClusterID: "test-cluster",
		Topic:     bus.TopicValidationResult,
		Sender:    "validator-build",
		Data:      map[string]any{"approved": true, "validator_id": "validator-build"},
	})

	select {
	case <-completeCh:
		t.Fatal("CLUSTER_COMPLETE should NOT fire after only 1 of 2 validators approve")
	case <-time.After(200 * time.Millisecond):
		// expected
	}

	b.Publish(bus.Message{
		ClusterID: "test-cluster",
		Topic:     bus.TopicValidationResult,
		Sender:    "validator-tests",
		Data:      map[string]any{"approved": true, "validator_id": "validator-tests"},
	})

	select {
	case msg := <-completeCh:
		approved, _ := msg.Data["approved"].(bool)
		if !approved {
			t.Error("expected CLUSTER_COMPLETE with approved=true after all validators approve")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for CLUSTER_COMPLETE after all validators approved")
	}

	cancel()
	<-done
}

// TestInjectValidators_ShellType verifies that injectValidators() creates shell-mode
// agent configs for type:"shell" validators instead of Claude agents.
func TestInjectValidators_ShellType(t *testing.T) {
	cfg := &config.Config{
		Profile: "default",
		Validators: []config.ValidatorConfig{
			{ID: "build", Name: "Build", Type: "shell", Command: "go build ./...", OnFail: "reject", RunOn: "always"},
			{ID: "test", Name: "Tests", Type: "shell", Command: "go test ./...", OnFail: "reject", RunOn: "always"},
		},
		Profiles: map[string][]string{
			"default": {"build", "test"},
		},
	}

	o := &Orchestrator{cfg: cfg}

	initial := []AgentConfig{
		{ID: "worker", Name: "Worker", Role: "worker"},
		{ID: "completion", Name: "Completion", Role: "completion"}, // should be removed and replaced
	}

	result, err := o.injectValidators(initial)
	if err != nil {
		t.Fatal(err)
	}

	completionCount := 0
	shellCount := 0
	workerCount := 0
	for _, c := range result {
		switch {
		case c.Role == "completion":
			completionCount++
		case c.ExecutionMode == "shell":
			shellCount++
			if c.Command == "" {
				t.Errorf("shell agent %s has empty Command", c.ID)
			}
		case c.Role == "worker":
			workerCount++
		}
		if c.Role == "validator" && c.ExecutionMode != "shell" && c.ExecutionMode != "claude" {
			t.Errorf("validator %s has unexpected ExecutionMode %q", c.ID, c.ExecutionMode)
		}
	}

	if shellCount != 2 {
		t.Errorf("expected 2 shell validators, got %d", shellCount)
	}
	if completionCount != 1 {
		t.Errorf("expected exactly 1 completion agent, got %d", completionCount)
	}
	if workerCount != 1 {
		t.Errorf("expected 1 worker, got %d", workerCount)
	}

	for _, c := range result {
		if c.Role == "completion" {
			if c.ExecutionMode != "passthrough" {
				t.Errorf("completion agent should use passthrough mode, got %q", c.ExecutionMode)
			}
			if c.TriggerProcessor == nil {
				t.Error("completion agent should have a TriggerProcessor")
			}
		}
	}
}

// TestInjectValidators_ClaudeType verifies claude validators get ExecutionMode="claude".
func TestInjectValidators_ClaudeType(t *testing.T) {
	cfg := &config.Config{
		Profile: "default",
		Validators: []config.ValidatorConfig{
			{ID: "review", Name: "Review", Type: "claude", Mode: "review", Prompt: "Review the code", OnFail: "reject", RunOn: "always"},
			{ID: "action", Name: "Action", Type: "claude", Mode: "action", Prompt: "Fix issues", OnFail: "reject", RunOn: "always"},
		},
		Profiles: map[string][]string{
			"default": {"review", "action"},
		},
	}

	o := &Orchestrator{cfg: cfg}
	result, err := o.injectValidators(nil)
	if err != nil {
		t.Fatal(err)
	}

	claudeCount := 0
	for _, c := range result {
		if c.ExecutionMode == "claude" {
			claudeCount++
			if c.ResultProcessor == nil {
				t.Errorf("claude validator %s should have ResultProcessor", c.ID)
			}
		}
	}

	if claudeCount != 2 {
		t.Errorf("expected 2 claude validators, got %d", claudeCount)
	}
}

// TestInjectValidators_AcceptOnlySkipped verifies accept_only validators are excluded.
func TestInjectValidators_AcceptOnlySkipped(t *testing.T) {
	cfg := &config.Config{
		Profile: "default",
		Validators: []config.ValidatorConfig{
			{ID: "build", Name: "Build", Type: "shell", Command: "go build", OnFail: "reject", RunOn: "always"},
			{ID: "deploy", Name: "Deploy", Type: "shell", Command: "deploy.sh", OnFail: "reject", RunOn: "accept_only"},
		},
		Profiles: map[string][]string{
			"default": {"build", "deploy"},
		},
	}

	o := &Orchestrator{cfg: cfg}
	result, err := o.injectValidators(nil)
	if err != nil {
		t.Fatal(err)
	}

	validatorCount := 0
	for _, c := range result {
		if c.Role == "validator" {
			validatorCount++
		}
	}
	if validatorCount != 1 {
		t.Errorf("expected 1 validator (accept_only should be skipped), got %d", validatorCount)
	}
}

// TestShellAgent_RunsInWorkDir verifies shell commands execute in the specified work directory.
func TestShellAgent_RunsInWorkDir(t *testing.T) {
	b := newTestBus(t)
	sink := &fakeSink{}
	tmpDir := t.TempDir()

	markerPath := filepath.Join(tmpDir, "marker.txt")
	os.WriteFile(markerPath, []byte("found"), 0644)

	cfg := AgentConfig{
		ID:            "validator-check",
		Name:          "Check",
		Role:          "validator",
		ExecutionMode: "shell",
		Command:       "cat marker.txt",
		Triggers:      []Trigger{{Topic: bus.TopicImplementationReady, Predicate: "always"}},
		MaxIterations: 1,
		OnComplete:    []OnCompleteAction{{Topic: bus.TopicValidationResult}},
	}

	a := NewAgent(cfg, b, sink, "test-cluster", tmpDir, tmpDir, nil)
	resultCh := b.Subscribe(bus.TopicValidationResult)
	defer b.Unsubscribe(bus.TopicValidationResult, resultCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	b.Publish(bus.Message{
		ClusterID: "test-cluster",
		Topic:     bus.TopicImplementationReady,
		Sender:    "worker",
		Data:      map[string]any{},
	})

	select {
	case msg := <-resultCh:
		approved, _ := msg.Data["approved"].(bool)
		if !approved {
			t.Error("expected command to succeed (marker.txt exists in workdir)")
		}
		if msg.Content != "found" {
			t.Errorf("expected content 'found', got %q", msg.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}

	cancel()
	<-done
}
