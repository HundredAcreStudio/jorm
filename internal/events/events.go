package events

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/jorm/internal/agent"
)

// Sink is the interface for sending UI events from business logic.
type Sink interface {
	Phase(name string)
	IssueLoaded(title, url string)
	Attempt(current, max int)
	ClaudeOutput(text string)
	ValidatorStart(id, name string)
	ValidatorDone(result agent.ValidatorResult)
	AgentStateChange(agentID, agentName, state string)
	MessagePublished(topic, sender string)
	Cost(totalCost float64)
	Classification(classification string)
	LoopDone(err error)
}

// PrintSink writes events to stdout (non-TUI mode).
type PrintSink struct{}

var (
	pBold = color.New(color.Bold).SprintFunc()
	pGreen = color.New(color.FgGreen).SprintFunc()
	pRed   = color.New(color.FgRed).SprintFunc()
	pCyan  = color.New(color.FgCyan).SprintFunc()
)

func (s *PrintSink) Phase(name string) {
	fmt.Printf("%s %s\n", pCyan("→"), name)
}

func (s *PrintSink) IssueLoaded(title, url string) {
	fmt.Printf("%s %s\n", pGreen("✓"), title)
}

func (s *PrintSink) Attempt(current, max int) {
	fmt.Printf("%s Attempt %d/%d\n", pCyan("→"), current, max)
}

func (s *PrintSink) ClaudeOutput(text string) {
	fmt.Println(text)
}

func (s *PrintSink) ValidatorStart(id, name string) {
	fmt.Printf("%s Running validator: %s\n", pCyan("→"), pBold(name))
}

func (s *PrintSink) ValidatorDone(result agent.ValidatorResult) {
	if result.Passed {
		fmt.Printf("%s %s passed\n", pGreen("✓"), result.Name)
	} else {
		fmt.Printf("%s %s failed (%s)\n", pRed("✗"), result.Name, result.OnFail)
	}
}

func (s *PrintSink) AgentStateChange(agentID, agentName, state string) {
	fmt.Printf("%s [%s] %s\n", pCyan("⚙"), agentName, state)
}

func (s *PrintSink) MessagePublished(topic, sender string) {
	fmt.Printf("%s %s → %s\n", pCyan("◆"), sender, topic)
}

func (s *PrintSink) Cost(totalCost float64) {
	fmt.Printf("%s Total cost: $%.4f\n", pCyan("→"), totalCost)
}

func (s *PrintSink) Classification(classification string) {
	fmt.Printf("%s Classification: %s\n", pCyan("→"), classification)
}

func (s *PrintSink) LoopDone(err error) {
	if err != nil {
		fmt.Printf("%s %s\n", pRed("✗"), err)
	} else {
		fmt.Printf("%s Done!\n", pGreen("✓"))
	}
}
