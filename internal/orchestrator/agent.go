package orchestrator

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

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

// OnCompleteAction defines a message to publish when an agent finishes.
type OnCompleteAction struct {
	Topic string
}

// AgentConfig defines an agent's behavior.
type AgentConfig struct {
	ID            string
	Name          string
	Role          string // "planner", "worker", "validator", "completion"
	Prompt        string // supports "builtin:" prefix
	Model         string
	MaxIterations int
	OnComplete    []OnCompleteAction
	// ExecutionMode controls how the agent executes: "claude" (default) or "shell".
	ExecutionMode string
	// Command is the shell command to execute (only used when ExecutionMode=="shell").
	Command string
	// ContextBuilder is called to assemble the prompt context from the bus.
	// If nil, the raw prompt is used as-is.
	ContextBuilder func(b *bus.Bus, clusterID string) (string, error)
	// ResultProcessor extracts structured data from the agent's output
	// to include in the OnComplete message's Data field.
	ResultProcessor func(result *agent.ClaudeResult) map[string]any
	// ReviewMode when true appends the VERDICT instruction to the prompt automatically.
	// Used for claude review validators so custom prompts don't need to include it.
	ReviewMode bool
	// Timeout is the per-agent execution deadline. If >0, a child context with this
	// timeout is used for shell execution.
	Timeout time.Duration
	// OnFail propagates the validator's on_fail policy ("reject", "warn", "ignore").
	OnFail string
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

// ExecuteOnce runs the agent's execution cycle exactly once.
// Used by StageOrchestrator to run agents synchronously.
func (a *Agent) ExecuteOnce(ctx context.Context) (*AgentResult, error) {
	a.setState(StateExecuting)
	a.Iteration++

	a.sink.AgentTriggerFired(a.Config.ID, a.lastTrigger.Topic, a.Iteration, a.Config.Model)

	// Shell mode: execute command directly, check exit code.
	if a.Config.ExecutionMode == "shell" {
		if a.Config.Role == "validator" {
			a.sink.ValidatorStart(a.Config.ID, a.Config.Name)
		}

		execCtx := ctx
		if a.Config.Timeout > 0 {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithTimeout(ctx, a.Config.Timeout)
			defer cancel()
		}

		cmd := exec.CommandContext(execCtx, "sh", "-c", a.Config.Command)
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

		onFail := a.Config.OnFail
		if onFail == "" {
			onFail = "reject"
		}
		if a.Config.Role == "validator" {
			a.sink.ValidatorDone(agent.ValidatorResult{
				ValidatorID: a.Config.ID,
				Name:        a.Config.Name,
				Passed:      approved,
				OnFail:      onFail,
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
			errOnFail := a.Config.OnFail
			if errOnFail == "" {
				errOnFail = "reject"
			}
			a.sink.ValidatorDone(agent.ValidatorResult{
				ValidatorID: a.Config.ID,
				Name:        a.Config.Name,
				Passed:      false,
				OnFail:      errOnFail,
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
	} else if a.Config.ReviewMode {
		// ReviewMode agents without a ResultProcessor: check VERDICT in output
		approved = strings.Contains(result.Text, "VERDICT: ACCEPT")
	}

	if a.Config.Role == "validator" {
		claudeOnFail := a.Config.OnFail
		if claudeOnFail == "" {
			claudeOnFail = "reject"
		}
		a.sink.ValidatorDone(agent.ValidatorResult{
			ValidatorID: a.Config.ID,
			Name:        a.Config.Name,
			Passed:      approved,
			OnFail:      claudeOnFail,
			Output:      result.Text,
		})
	}

	return &AgentResult{Output: result.Text, Approved: approved, Cost: result.Cost, Data: data}, nil
}

// PublishOnComplete publishes OnComplete messages to the bus with the agent's result data.
func (a *Agent) PublishOnComplete(result *AgentResult) {
	data := map[string]any{
		"agent_id":  a.Config.ID,
		"iteration": a.Iteration,
	}
	for k, v := range result.Data {
		data[k] = v
	}
	for _, action := range a.Config.OnComplete {
		_ = a.bus.Publish(bus.Message{
			ClusterID: a.clusterID,
			Topic:     action.Topic,
			Sender:    a.Config.ID,
			Content:   result.Output,
			Data:      data,
		})
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
