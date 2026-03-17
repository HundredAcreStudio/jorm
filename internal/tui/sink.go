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
func (s *ProgramSink) LoopDone(err error) { s.P.Send(LoopDoneMsg{Err: err}) }
