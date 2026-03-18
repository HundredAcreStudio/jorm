package orchestrator

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/jorm/internal/agent"
	"github.com/jorm/internal/agent/prompts"
	"github.com/jorm/internal/bus"
	"github.com/jorm/internal/events"
)

// AgentState represents where an agent is in its lifecycle.
type AgentState string

const (
	StateIdle            AgentState = "idle"
	StateEvaluating      AgentState = "evaluating"
	StateBuildingContext  AgentState = "building_context"
	StateExecuting       AgentState = "executing"
)

// Trigger defines when an agent should activate.
type Trigger struct {
	Topic     string
	Predicate string // "always", "approved", "rejected"
}

// OnCompleteAction defines a message to publish when an agent finishes.
type OnCompleteAction struct {
	Topic string
}

// AgentConfig defines an agent's behavior.
type AgentConfig struct {
	ID            string
	Name          string
	Role          string // "planner", "worker", "validator", "completion"
	Triggers      []Trigger
	Prompt        string // supports "builtin:" prefix
	Model         string
	MaxIterations int
	OnComplete    []OnCompleteAction
	// ContextBuilder is called to assemble the prompt context from the bus.
	// If nil, the raw prompt is used as-is.
	ContextBuilder func(b *bus.Bus, clusterID string) (string, error)
	// ResultProcessor extracts structured data from the agent's output
	// to include in the OnComplete message's Data field.
	ResultProcessor func(result *agent.ClaudeResult) map[string]any
}

// Agent is a running instance of an AgentConfig.
type Agent struct {
	Config    AgentConfig
	State     AgentState
	Iteration int

	bus       *bus.Bus
	sink      events.Sink
	clusterID string
	workDir   string
	repoDir   string
	env       []string
	totalCost float64
}

// TotalCost returns the accumulated cost from all Claude invocations by this agent.
func (a *Agent) TotalCost() float64 {
	return a.totalCost
}

// NewAgent creates a new agent instance.
func NewAgent(cfg AgentConfig, b *bus.Bus, sink events.Sink, clusterID, workDir, repoDir string, env []string) *Agent {
	return &Agent{
		Config:    cfg,
		State:     StateIdle,
		bus:       b,
		sink:      sink,
		clusterID: clusterID,
		workDir:   workDir,
		repoDir:   repoDir,
		env:       env,
	}
}

// Run starts the agent's trigger-driven lifecycle loop.
// It blocks until the context is cancelled or maxIterations is reached.
func (a *Agent) Run(ctx context.Context) error {
	// Subscribe to all trigger topics
	channels := make(map[string]<-chan bus.Message)
	for _, t := range a.Config.Triggers {
		ch := a.bus.Subscribe(t.Topic)
		channels[t.Topic] = ch
		defer a.bus.Unsubscribe(t.Topic, ch)
	}

	for {
		a.setState(StateIdle)

		// Wait for a matching trigger
		msg, err := a.waitForTrigger(ctx, channels)
		if err != nil {
			return err // context cancelled
		}

		a.setState(StateEvaluating)

		// Check if we've exceeded max iterations
		if a.Config.MaxIterations > 0 && a.Iteration >= a.Config.MaxIterations {
			return nil
		}

		// Build context
		a.setState(StateBuildingContext)
		prompt, err := a.buildPrompt()
		if err != nil {
			a.sink.ClaudeOutput(fmt.Sprintf("[%s] prompt error: %v", a.Config.Name, err))
			continue
		}

		// Add trigger message context
		if msg.Content != "" {
			prompt = prompt + "\n\n## Context from " + msg.Topic + "\n\n" + msg.Content
		}

		// Execute
		a.setState(StateExecuting)
		a.Iteration++

		result, err := agent.RunClaude(ctx, agent.RunOptions{
			Prompt:  prompt,
			WorkDir: a.workDir,
			Model:   a.Config.Model,
			Env:     a.env,
			OnOutput: func(text string) {
				a.sink.ClaudeOutput(fmt.Sprintf("[%s] %s", a.Config.Name, text))
			},
		})
		if err != nil {
			a.sink.ClaudeOutput(fmt.Sprintf("[%s] error: %v", a.Config.Name, err))
			continue
		}

		// Accumulate cost
		if result.Cost > 0 {
			a.totalCost += result.Cost
		}

		// Publish OnComplete messages
		for _, action := range a.Config.OnComplete {
			data := make(map[string]any)
			if a.Config.ResultProcessor != nil {
				data = a.Config.ResultProcessor(result)
			}
			data["agent_id"] = a.Config.ID
			data["iteration"] = a.Iteration

			a.bus.Publish(bus.Message{
				ClusterID: a.clusterID,
				Topic:     action.Topic,
				Sender:    a.Config.ID,
				Content:   result.Text,
				Data:      data,
			})
		}
	}
}

// waitForTrigger blocks until a message matches one of the agent's triggers.
// Uses reflect.Select for proper blocking across multiple channels.
func (a *Agent) waitForTrigger(ctx context.Context, channels map[string]<-chan bus.Message) (bus.Message, error) {
	// Build reflect.SelectCase slice: first case is ctx.Done(), rest are trigger channels
	selectCases := make([]reflect.SelectCase, 0, len(a.Config.Triggers)+1)
	triggerMap := make([]Trigger, 0, len(a.Config.Triggers))

	// Case 0: context cancellation
	selectCases = append(selectCases, reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	})

	// Remaining cases: trigger channels
	for _, t := range a.Config.Triggers {
		ch, ok := channels[t.Topic]
		if !ok {
			continue
		}
		selectCases = append(selectCases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ch),
		})
		triggerMap = append(triggerMap, t)
	}

	for {
		chosen, value, ok := reflect.Select(selectCases)

		// Case 0: context cancelled
		if chosen == 0 {
			return bus.Message{}, ctx.Err()
		}

		// Channel closed
		if !ok {
			return bus.Message{}, fmt.Errorf("trigger channel closed for agent %s", a.Config.Name)
		}

		msg := value.Interface().(bus.Message)
		trigger := triggerMap[chosen-1]

		if evaluatePredicate(trigger.Predicate, msg) {
			return msg, nil
		}
		// Predicate didn't match, loop back and wait again
	}
}

// evaluatePredicate checks if a message matches a named predicate.
func evaluatePredicate(predicate string, msg bus.Message) bool {
	switch strings.ToLower(predicate) {
	case "always", "":
		return true
	case "approved":
		v, ok := msg.Data["approved"]
		if !ok {
			return false
		}
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return val == "true"
		}
		return false
	case "rejected":
		v, ok := msg.Data["approved"]
		if !ok {
			return false
		}
		switch val := v.(type) {
		case bool:
			return !val
		case string:
			return val == "false"
		}
		return false
	default:
		return true
	}
}

func (a *Agent) setState(state AgentState) {
	a.State = state
	a.sink.AgentStateChange(a.Config.ID, a.Config.Name, string(state))
}

func (a *Agent) buildPrompt() (string, error) {
	// Resolve the prompt template
	promptText, err := prompts.Resolve(a.Config.Prompt, a.repoDir)
	if err != nil {
		return "", err
	}

	// If there's a context builder, append bus context
	if a.Config.ContextBuilder != nil {
		busContext, err := a.Config.ContextBuilder(a.bus, a.clusterID)
		if err != nil {
			return "", fmt.Errorf("building context: %w", err)
		}
		if busContext != "" {
			promptText = promptText + "\n\n" + busContext
		}
	}

	return promptText, nil
}
