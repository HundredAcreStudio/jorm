package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jorm/internal/agent"
)

// ProgramSink sends events to a bubbletea Program.
type ProgramSink struct {
	P *tea.Program
}

func (s *ProgramSink) Phase(name string)              { s.P.Send(PhaseMsg{Name: name}) }
func (s *ProgramSink) IssueLoaded(title, url string)  { s.P.Send(IssueLoadedMsg{Title: title, URL: url}) }
func (s *ProgramSink) Attempt(current, max int)       { s.P.Send(AttemptMsg{Current: current, Max: max}) }
func (s *ProgramSink) ClaudeOutput(text string)       { s.P.Send(ClaudeOutputMsg{Text: text}) }
func (s *ProgramSink) ValidatorStart(id, name string) { s.P.Send(ValidatorStartMsg{ID: id, Name: name}) }
func (s *ProgramSink) ValidatorDone(result agent.ValidatorResult) {
	s.P.Send(ValidatorDoneMsg{ValidatorResult: result})
}
func (s *ProgramSink) AgentStateChange(agentID, agentName, state string) {
	s.P.Send(AgentStateChangeMsg{ID: agentID, Name: agentName, State: state})
}
func (s *ProgramSink) MessagePublished(topic, sender string) {
	s.P.Send(MessagePublishedMsg{Topic: topic, Sender: sender})
}
func (s *ProgramSink) Cost(totalCost float64) {
	s.P.Send(CostMsg{TotalCost: totalCost})
}
func (s *ProgramSink) Classification(classification string) {
	s.P.Send(ClassificationMsg{Classification: classification})
}
func (s *ProgramSink) LoopDone(err error) { s.P.Send(LoopDoneMsg{Err: err}) }

// No-op implementations for lifecycle events on ProgramSink.
func (s *ProgramSink) UpdateTotalAgents(count int)                                  {}
func (s *ProgramSink) AgentSpawned(id, name string, triggers []string)              {}
func (s *ProgramSink) AgentTriggerFired(id, topic string, taskNum int, model string) {}
func (s *ProgramSink) AgentTaskCompleted(id string, taskNum int)                     {}
func (s *ProgramSink) AgentTaskFailed(id string, taskNum int, err error)             {}
func (s *ProgramSink) AgentTokenUsage(id, name string, input, output int)            {}
func (s *ProgramSink) ValidationRoundStart(round int)                                {}
func (s *ProgramSink) ValidationRoundComplete(round, approved, rejected int)          {}
func (s *ProgramSink) RetryRoundStart(round int)                                     {}
func (s *ProgramSink) SystemEvent(text string)                                       {}
func (s *ProgramSink) ClusterComplete(runID, reason string)                          {}
func (s *ProgramSink) StageStarted(stageIndex int, stageName string)                 {}
func (s *ProgramSink) StageCompleted(stageIndex int, stageName string)               {}
