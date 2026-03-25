package orchestrator

import (
	"context"
	"fmt"
	"os/exec"
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
	// ExecutionMode controls how the agent executes: "claude" (default), "shell", or "passthrough".
	ExecutionMode string
	// Command is the shell command to execute (only used when ExecutionMode=="shell").
	Command string
	// ContextBuilder is called to assemble the prompt context from the bus.
	// If nil, the raw prompt is used as-is.
	ContextBuilder func(b *bus.Bus, clusterID string) (string, error)
	// ResultProcessor extracts structured data from the agent's output
	// to include in the OnComplete message's Data field.
	ResultProcessor func(result *agent.ClaudeResult) map[string]any
	// TriggerProcessor processes trigger messages directly without executing Claude or shell.
	// Used with ExecutionMode "passthrough". Returns (data, shouldPublish).
	TriggerProcessor func(msg bus.Message) (map[string]any, bool)
	// ReviewMode when true appends the VERDICT instruction to the prompt automatically.
	// Used for claude review validators so custom prompts don't need to include it.
	ReviewMode bool
}

// AgentResult holds the outcome of a single agent execution cycle.
type AgentResult struct {
	Output   string
	Approved bool           // For validators: whether the review passed
	Cost     float64
	Data     map[string]any // Full ResultProcessor output
}

// Agent is a running instance of an AgentConfig.
type Agent struct {
	Config    AgentConfig
	State     AgentState
	Iteration int

	bus         *bus.Bus
	sink        events.Sink
	clusterID   string
	workDir     string
	repoDir     string
	env         []string
	totalCost   float64
	lastTrigger bus.Message // set by Run() before ExecuteOnce() to pass trigger context
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

		// Check if context was cancelled between trigger and execution
		if ctx.Err() != nil {
			return nil
		}

		// Store trigger message so ExecuteOnce can access it
		a.lastTrigger = msg

		result, err := a.ExecuteOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}

		// Publish OnComplete messages based on execution mode
		switch a.Config.ExecutionMode {
		case "passthrough":
			if a.Config.TriggerProcessor != nil {
				data, shouldPublish := a.Config.TriggerProcessor(msg)
				if shouldPublish {
					for _, action := range a.Config.OnComplete {
						a.bus.Publish(bus.Message{
							ClusterID: a.clusterID,
							Topic:     action.Topic,
							Sender:    a.Config.ID,
							Content:   msg.Content,
							Data:      data,
						})
					}
				}
			}
		case "shell":
			for _, action := range a.Config.OnComplete {
				a.bus.Publish(bus.Message{
					ClusterID: a.clusterID,
					Topic:     action.Topic,
					Sender:    a.Config.ID,
					Content:   result.Output,
					Data: map[string]any{
						"approved":     result.Approved,
						"validator_id": a.Config.ID,
						"agent_id":     a.Config.ID,
						"iteration":    a.Iteration,
					},
				})
			}
		default: // claude
			data := map[string]any{
				"agent_id":  a.Config.ID,
				"iteration": a.Iteration,
			}
			for k, v := range result.Data {
				data[k] = v
			}
			for _, action := range a.Config.OnComplete {
				a.bus.Publish(bus.Message{
					ClusterID: a.clusterID,
					Topic:     action.Topic,
					Sender:    a.Config.ID,
					Content:   result.Output,
					Data:      data,
				})
			}
		}
	}
}

// ExecuteOnce runs the agent's execution cycle exactly once (no trigger loop).
// Used by StageOrchestrator to run agents synchronously.
// Run() sets a.lastTrigger before calling this so trigger context is available.
func (a *Agent) ExecuteOnce(ctx context.Context) (*AgentResult, error) {
	a.setState(StateExecuting)
	a.Iteration++

	// Passthrough mode is driven by trigger messages; no synchronous execution body.
	if a.Config.ExecutionMode == "passthrough" {
		return &AgentResult{}, nil
	}

	a.sink.AgentTriggerFired(a.Config.ID, a.lastTrigger.Topic, a.Iteration, a.Config.Model)

	// Shell mode: execute command directly, check exit code.
	if a.Config.ExecutionMode == "shell" {
		if a.Config.Role == "validator" {
			a.sink.ValidatorStart(a.Config.ID, a.Config.Name)
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", a.Config.Command)
		cmd.Dir = a.workDir
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		approved := err == nil

		a.sink.ClaudeOutput(fmt.Sprintf("[%s] $ %s", a.Config.Name, a.Config.Command))
		if len(out) > 0 {
			a.sink.ClaudeOutput(fmt.Sprintf("[%s] %s", a.Config.Name, string(out)))
		}
		if approved {
			a.sink.ClaudeOutput(fmt.Sprintf("[%s] ✓ passed", a.Config.Name))
		} else {
			a.sink.ClaudeOutput(fmt.Sprintf("[%s] ✗ failed: %v", a.Config.Name, err))
		}

		if a.Config.Role == "validator" {
			a.sink.ValidatorDone(agent.ValidatorResult{
				ValidatorID: a.Config.ID,
				Name:        a.Config.Name,
				Passed:      approved,
				OnFail:      "reject",
				Output:      string(out),
			})
		}

		return &AgentResult{Output: string(out), Approved: approved}, nil
	}

	// Claude mode (default): build prompt and run Claude.
	a.setState(StateBuildingContext)
	prompt, err := a.buildPrompt()
	if err != nil {
		a.sink.ClaudeOutput(fmt.Sprintf("[%s] prompt error: %v", a.Config.Name, err))
		return nil, fmt.Errorf("building prompt: %w", err)
	}

	if a.lastTrigger.Content != "" {
		prompt = prompt + "\n\n## Context from " + a.lastTrigger.Topic + "\n\n" + a.lastTrigger.Content
	}

	// For review-mode validators, append the verdict instruction.
	if a.Config.ReviewMode {
		prompt = prompt + "\n\nEnd your response with exactly \"VERDICT: ACCEPT\" or \"VERDICT: REJECT\" followed by a brief reason."
	}

	a.setState(StateExecuting)

	if a.Config.Role == "validator" {
		a.sink.ValidatorStart(a.Config.ID, a.Config.Name)
	}

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
		// If the context was cancelled (e.g. cluster completed while we were
		// running), this is a graceful shutdown — not a failure.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		a.sink.AgentTaskFailed(a.Config.ID, a.Iteration, err)
		a.sink.ClaudeOutput(fmt.Sprintf("[%s] error: %v", a.Config.Name, err))
		if a.Config.Role == "validator" {
			a.sink.ValidatorDone(agent.ValidatorResult{
				ValidatorID: a.Config.ID,
				Name:        a.Config.Name,
				Passed:      false,
				OnFail:      "reject",
				Output:      fmt.Sprintf("error: %v", err),
			})
		}
		return nil, err
	}

	a.sink.AgentTaskCompleted(a.Config.ID, a.Iteration)

	// Accumulate cost
	if result.Cost > 0 {
		a.totalCost += result.Cost
	}

	// Process result data and determine approved status
	var data map[string]any
	approved := false
	if a.Config.ResultProcessor != nil {
		data = a.Config.ResultProcessor(result)
		if v, ok := data["approved"].(bool); ok {
			approved = v
		}
	}

	if a.Config.Role == "validator" {
		a.sink.ValidatorDone(agent.ValidatorResult{
			ValidatorID: a.Config.ID,
			Name:        a.Config.Name,
			Passed:      approved,
			OnFail:      "reject",
			Output:      result.Text,
		})
	}

	return &AgentResult{Output: result.Text, Approved: approved, Cost: result.Cost, Data: data}, nil
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
