package log

import (
	"strings"

	"github.com/jorm/internal/agent"
	"github.com/jorm/internal/events"
)

// LogSink wraps another Sink and logs every event to the structured log file.
type LogSink struct {
	inner  events.Sink
	logger *Logger
}

// NewLogSink creates a LogSink that delegates to inner and logs via logger.
func NewLogSink(inner events.Sink, logger *Logger) *LogSink {
	return &LogSink{inner: inner, logger: logger}
}

func (l *LogSink) Phase(name string) {
	l.logger.Info("phase", "name", name)
	l.inner.Phase(name)
}

func (l *LogSink) IssueLoaded(title, url string) {
	l.logger.Info("issue.loaded", "title", title, "url", url)
	l.inner.IssueLoaded(title, url)
}

func (l *LogSink) Attempt(current, max int) {
	l.logger.Info("attempt", "current", current, "max", max)
	l.inner.Attempt(current, max)
}

func (l *LogSink) ClaudeOutput(text string) {
	truncated := text
	if len(truncated) > 500 {
		truncated = truncated[:500]
	}
	l.logger.Debug("claude.output", "text", truncated)
	l.inner.ClaudeOutput(text)
}

func (l *LogSink) ValidatorStart(id, name string) {
	l.logger.Info("validator.started", "id", id, "validator", name)
	l.inner.ValidatorStart(id, name)
}

func (l *LogSink) ValidatorDone(result agent.ValidatorResult) {
	if result.Passed {
		l.logger.Info("validator.passed", "validator", result.Name, "id", result.ValidatorID, "on_fail", result.OnFail)
	} else {
		output := result.Output
		if len(output) > 500 {
			output = output[:500]
		}
		l.logger.Warn("validator.rejected", "validator", result.Name, "id", result.ValidatorID, "on_fail", result.OnFail, "output", output)
	}
	l.inner.ValidatorDone(result)
}

func (l *LogSink) AgentStateChange(agentID, agentName, state string) {
	l.logger.Info("agent.state_change", "agent_id", agentID, "agent", agentName, "state", state)
	l.inner.AgentStateChange(agentID, agentName, state)
}

func (l *LogSink) MessagePublished(topic, sender string) {
	l.logger.Info("bus.message", "topic", topic, "sender", sender)
	l.inner.MessagePublished(topic, sender)
}

func (l *LogSink) Cost(totalCost float64) {
	l.logger.Info("cost", "total_cost", totalCost)
	l.inner.Cost(totalCost)
}

func (l *LogSink) Classification(classification string) {
	l.logger.Info("classification", "result", classification)
	l.inner.Classification(classification)
}

func (l *LogSink) LoopDone(err error) {
	if err != nil {
		l.logger.Error("loop.done", "error", err)
	} else {
		l.logger.Info("loop.done")
	}
	l.inner.LoopDone(err)
}

func (l *LogSink) UpdateTotalAgents(count int) {
	l.logger.Info("agents.total", "count", count)
	l.inner.UpdateTotalAgents(count)
}

func (l *LogSink) AgentSpawned(id, name string, triggers []string) {
	l.logger.Info("agent.spawned", "agent_id", id, "agent", name, "triggers", strings.Join(triggers, ","))
	l.inner.AgentSpawned(id, name, triggers)
}

func (l *LogSink) AgentTriggerFired(id, topic string, taskNum int, model string) {
	l.logger.Info("agent.trigger_fired", "agent_id", id, "topic", topic, "task", taskNum, "model", model)
	l.inner.AgentTriggerFired(id, topic, taskNum, model)
}

func (l *LogSink) AgentTaskCompleted(id string, taskNum int) {
	l.logger.Info("agent.completed", "agent_id", id, "task", taskNum)
	l.inner.AgentTaskCompleted(id, taskNum)
}

func (l *LogSink) AgentTaskFailed(id string, taskNum int, err error) {
	l.logger.Warn("agent.failed", "agent_id", id, "task", taskNum, "error", err)
	l.inner.AgentTaskFailed(id, taskNum, err)
}

func (l *LogSink) AgentTokenUsage(id, name string, input, output int) {
	l.logger.Info("agent.tokens", "agent_id", id, "agent", name, "tokens_in", input, "tokens_out", output)
	l.inner.AgentTokenUsage(id, name, input, output)
}

func (l *LogSink) ValidationRoundStart(round int) {
	l.logger.Info("round.start", "round", round)
	l.inner.ValidationRoundStart(round)
}

func (l *LogSink) ValidationRoundComplete(round, approved, rejected int) {
	l.logger.Info("round.complete", "round", round, "approved", approved, "rejected", rejected)
	l.inner.ValidationRoundComplete(round, approved, rejected)
}

func (l *LogSink) RetryRoundStart(round int) {
	l.logger.Info("retry.start", "round", round)
	l.inner.RetryRoundStart(round)
}

func (l *LogSink) SystemEvent(text string) {
	l.logger.Info("system", "text", text)
	l.inner.SystemEvent(text)
}

func (l *LogSink) ClusterComplete(runID, reason string) {
	l.logger.Info("cluster.complete", "run_id", runID, "reason", reason)
	l.inner.ClusterComplete(runID, reason)
}
