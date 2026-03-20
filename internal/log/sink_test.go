package log

import (
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/jorm/internal/agent"
)

// mockSink records calls to verify delegation.
type mockSink struct {
	phases            []string
	issueLoaded       []string
	attempts          [][2]int
	claudeOutputs     []string
	validatorStarts   [][2]string
	validatorDones    []agent.ValidatorResult
	agentStateChanges [][3]string
	messagesPublished [][2]string
	costs             []float64
	classifications   []string
	loopDoneErrs      []error
	totalAgents       []int
	agentsSpawned     []string
	triggersFired     []string
	tasksCompleted    []string
	tasksFailed       []string
	tokenUsages       []string
	roundStarts       []int
	roundCompletes    [][3]int
	retryRoundStarts  []int
	systemEvents      []string
	clusterCompletes  [][2]string
}

func (m *mockSink) Phase(name string)             { m.phases = append(m.phases, name) }
func (m *mockSink) IssueLoaded(title, url string) { m.issueLoaded = append(m.issueLoaded, title) }
func (m *mockSink) Attempt(current, max int) {
	m.attempts = append(m.attempts, [2]int{current, max})
}
func (m *mockSink) ClaudeOutput(text string)       { m.claudeOutputs = append(m.claudeOutputs, text) }
func (m *mockSink) ValidatorStart(id, name string) { m.validatorStarts = append(m.validatorStarts, [2]string{id, name}) }
func (m *mockSink) ValidatorDone(result agent.ValidatorResult) {
	m.validatorDones = append(m.validatorDones, result)
}
func (m *mockSink) AgentStateChange(id, name, state string) {
	m.agentStateChanges = append(m.agentStateChanges, [3]string{id, name, state})
}
func (m *mockSink) MessagePublished(topic, sender string) {
	m.messagesPublished = append(m.messagesPublished, [2]string{topic, sender})
}
func (m *mockSink) Cost(totalCost float64)    { m.costs = append(m.costs, totalCost) }
func (m *mockSink) Classification(cls string) { m.classifications = append(m.classifications, cls) }
func (m *mockSink) LoopDone(err error)        { m.loopDoneErrs = append(m.loopDoneErrs, err) }
func (m *mockSink) UpdateTotalAgents(count int) {
	m.totalAgents = append(m.totalAgents, count)
}
func (m *mockSink) AgentSpawned(id, name string, triggers []string) {
	m.agentsSpawned = append(m.agentsSpawned, name)
}
func (m *mockSink) AgentTriggerFired(id, topic string, taskNum int, model string) {
	m.triggersFired = append(m.triggersFired, topic)
}
func (m *mockSink) AgentTaskCompleted(id string, taskNum int) {
	m.tasksCompleted = append(m.tasksCompleted, id)
}
func (m *mockSink) AgentTaskFailed(id string, taskNum int, err error) {
	m.tasksFailed = append(m.tasksFailed, id)
}
func (m *mockSink) AgentTokenUsage(id, name string, input, output int) {
	m.tokenUsages = append(m.tokenUsages, name)
}
func (m *mockSink) ValidationRoundStart(round int) { m.roundStarts = append(m.roundStarts, round) }
func (m *mockSink) ValidationRoundComplete(round, approved, rejected int) {
	m.roundCompletes = append(m.roundCompletes, [3]int{round, approved, rejected})
}
func (m *mockSink) RetryRoundStart(round int) { m.retryRoundStarts = append(m.retryRoundStarts, round) }
func (m *mockSink) SystemEvent(text string)   { m.systemEvents = append(m.systemEvents, text) }
func (m *mockSink) ClusterComplete(runID, reason string) {
	m.clusterCompletes = append(m.clusterCompletes, [2]string{runID, reason})
}

// newTempLogger creates a Logger backed by a temp file for tests.
func newTempLogger(t *testing.T) *Logger {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "jorm-test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	handler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
	return &Logger{logger: slog.New(handler), file: f}
}

// TestLogSink verifies LogSink delegates all calls to the inner sink.
func TestLogSink(t *testing.T) {
	logger := newTempLogger(t)
	inner := &mockSink{}
	ls := NewLogSink(inner, logger)

	ls.Phase("testing")
	ls.IssueLoaded("Fix bug", "https://example.com")
	ls.Attempt(1, 3)
	ls.ClaudeOutput("hello")
	ls.ValidatorStart("v1", "Build")
	ls.ValidatorDone(agent.ValidatorResult{Name: "Build", Passed: true, OnFail: "reject"})
	ls.ValidatorDone(agent.ValidatorResult{Name: "Lint", Passed: false, OnFail: "warn", Output: "error"})
	ls.AgentStateChange("id1", "Worker", "running")
	ls.MessagePublished("PLAN_READY", "Planner")
	ls.Cost(0.05)
	ls.Classification("simple/bug")
	ls.LoopDone(nil)
	ls.LoopDone(errors.New("oops"))
	ls.UpdateTotalAgents(3)
	ls.AgentSpawned("id1", "Worker", []string{"PLAN_READY"})
	ls.AgentTriggerFired("id1", "PLAN_READY", 1, "sonnet")
	ls.AgentTaskCompleted("id1", 1)
	ls.AgentTaskFailed("id2", 2, errors.New("fail"))
	ls.AgentTokenUsage("id1", "Worker", 100, 200)
	ls.ValidationRoundStart(1)
	ls.ValidationRoundComplete(1, 2, 1)
	ls.RetryRoundStart(2)
	ls.SystemEvent("some event")
	ls.ClusterComplete("run-1", "accepted")

	if len(inner.phases) != 1 || inner.phases[0] != "testing" {
		t.Errorf("Phase not delegated: %v", inner.phases)
	}
	if len(inner.issueLoaded) != 1 || inner.issueLoaded[0] != "Fix bug" {
		t.Errorf("IssueLoaded not delegated: %v", inner.issueLoaded)
	}
	if len(inner.attempts) != 1 || inner.attempts[0] != [2]int{1, 3} {
		t.Errorf("Attempt not delegated: %v", inner.attempts)
	}
	if len(inner.claudeOutputs) != 1 || inner.claudeOutputs[0] != "hello" {
		t.Errorf("ClaudeOutput not delegated: %v", inner.claudeOutputs)
	}
	if len(inner.validatorStarts) != 1 {
		t.Errorf("ValidatorStart not delegated: %v", inner.validatorStarts)
	}
	if len(inner.validatorDones) != 2 {
		t.Errorf("ValidatorDone not delegated: %v", inner.validatorDones)
	}
	if len(inner.agentStateChanges) != 1 {
		t.Errorf("AgentStateChange not delegated")
	}
	if len(inner.messagesPublished) != 1 {
		t.Errorf("MessagePublished not delegated")
	}
	if len(inner.costs) != 1 {
		t.Errorf("Cost not delegated")
	}
	if len(inner.classifications) != 1 || inner.classifications[0] != "simple/bug" {
		t.Errorf("Classification not delegated")
	}
	if len(inner.loopDoneErrs) != 2 {
		t.Errorf("LoopDone not delegated: %v", inner.loopDoneErrs)
	}
	if len(inner.totalAgents) != 1 || inner.totalAgents[0] != 3 {
		t.Errorf("UpdateTotalAgents not delegated")
	}
	if len(inner.agentsSpawned) != 1 || inner.agentsSpawned[0] != "Worker" {
		t.Errorf("AgentSpawned not delegated")
	}
	if len(inner.triggersFired) != 1 || inner.triggersFired[0] != "PLAN_READY" {
		t.Errorf("AgentTriggerFired not delegated")
	}
	if len(inner.tasksCompleted) != 1 {
		t.Errorf("AgentTaskCompleted not delegated")
	}
	if len(inner.tasksFailed) != 1 {
		t.Errorf("AgentTaskFailed not delegated")
	}
	if len(inner.tokenUsages) != 1 {
		t.Errorf("AgentTokenUsage not delegated")
	}
	if len(inner.roundStarts) != 1 || inner.roundStarts[0] != 1 {
		t.Errorf("ValidationRoundStart not delegated")
	}
	if len(inner.roundCompletes) != 1 {
		t.Errorf("ValidationRoundComplete not delegated")
	}
	if len(inner.retryRoundStarts) != 1 || inner.retryRoundStarts[0] != 2 {
		t.Errorf("RetryRoundStart not delegated")
	}
	if len(inner.systemEvents) != 1 || inner.systemEvents[0] != "some event" {
		t.Errorf("SystemEvent not delegated")
	}
	if len(inner.clusterCompletes) != 1 {
		t.Errorf("ClusterComplete not delegated")
	}
}

// TestLogSinkDelegates verifies delegation for key agent lifecycle methods.
func TestLogSinkDelegates(t *testing.T) {
	logger := newTempLogger(t)
	inner := &mockSink{}
	ls := NewLogSink(inner, logger)

	ls.AgentSpawned("a1", "Planner", []string{"START"})
	ls.ClusterComplete("run-42", "accepted")

	if len(inner.agentsSpawned) != 1 || inner.agentsSpawned[0] != "Planner" {
		t.Errorf("AgentSpawned not delegated: %v", inner.agentsSpawned)
	}
	if len(inner.clusterCompletes) != 1 || inner.clusterCompletes[0][0] != "run-42" {
		t.Errorf("ClusterComplete not delegated correctly: %v", inner.clusterCompletes)
	}
}

// TestLogSinkClaudeOutputTruncated verifies inner receives original (untruncated) text.
func TestLogSinkClaudeOutputTruncated(t *testing.T) {
	logger := newTempLogger(t)
	inner := &mockSink{}
	ls := NewLogSink(inner, logger)

	long := string(make([]byte, 1000))
	ls.ClaudeOutput(long)

	if len(inner.claudeOutputs) != 1 || len(inner.claudeOutputs[0]) != 1000 {
		t.Errorf("ClaudeOutput inner should receive full text, got len=%d", len(inner.claudeOutputs[0]))
	}
}
