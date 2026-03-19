package ui

// Event types consumed by the UI renderer.
// These are plain data structs — no behavior.

// AgentSpawnedEvent is emitted when an agent starts up.
type AgentSpawnedEvent struct {
	ID       string
	Name     string
	Triggers []string
}

// AgentTriggerFiredEvent is emitted when an agent's trigger fires.
type AgentTriggerFiredEvent struct {
	ID      string
	Topic   string
	TaskNum int
	Model   string
}

// AgentTaskEvent is emitted for sub-events during agent execution.
type AgentTaskEventData struct {
	ID    string
	Event string
}

// AgentTaskCompletedEvent is emitted when an agent task finishes successfully.
type AgentTaskCompletedEvent struct {
	ID      string
	TaskNum int
}

// AgentTaskFailedEvent is emitted when an agent task fails.
type AgentTaskFailedEvent struct {
	ID      string
	TaskNum int
	Err     error
}

// AgentTokenUsageEvent is emitted with token usage stats after an agent completes.
type AgentTokenUsageEvent struct {
	ID           string
	Name         string
	InputTokens  int
	OutputTokens int
}

// ValidationRoundStartEvent is emitted when a validation round begins.
type ValidationRoundStartEvent struct {
	Round int
}

// ValidationRoundCompleteEvent is emitted when all validators in a round finish.
type ValidationRoundCompleteEvent struct {
	Round    int
	Approved int
	Rejected int
}

// RetryRoundStartEvent is emitted when a retry round begins.
type RetryRoundStartEvent struct {
	Round int
}

// SystemEventData is emitted for system-level messages.
type SystemEventData struct {
	Text string
}

// ClusterCompleteEvent is emitted when the cluster finishes.
type ClusterCompleteEvent struct {
	RunID  string
	Reason string
}
