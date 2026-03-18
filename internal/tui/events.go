package tui

import "github.com/jorm/internal/agent"

// PhaseMsg signals a phase transition in the loop.
type PhaseMsg struct {
	Name string
}

// IssueLoadedMsg carries the fetched issue info.
type IssueLoadedMsg struct {
	Title string
	URL   string
}

// AttemptMsg signals a new worker attempt.
type AttemptMsg struct {
	Current int
	Max     int
}

// ClaudeOutputMsg carries a line of Claude output.
type ClaudeOutputMsg struct {
	Text string
}

// ValidatorStartMsg signals a validator has started running.
type ValidatorStartMsg struct {
	ID   string
	Name string
}

// ValidatorDoneMsg signals a validator has completed.
type ValidatorDoneMsg struct {
	agent.ValidatorResult
}

// AgentStateChangeMsg signals an agent's state has changed.
type AgentStateChangeMsg struct {
	ID    string
	Name  string
	State string
}

// MessagePublishedMsg signals a message was published on the bus.
type MessagePublishedMsg struct {
	Topic  string
	Sender string
}

// CostMsg updates the total cost display.
type CostMsg struct {
	TotalCost float64
}

// ClassificationMsg carries the conductor classification.
type ClassificationMsg struct {
	Classification string
}

// LoopDoneMsg signals the entire loop has finished.
type LoopDoneMsg struct {
	Err error
}
