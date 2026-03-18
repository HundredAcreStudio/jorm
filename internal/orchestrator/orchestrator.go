package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jorm/internal/agent"
	"github.com/jorm/internal/bus"
	"github.com/jorm/internal/config"
	"github.com/jorm/internal/events"
	gitpkg "github.com/jorm/internal/git"
	"github.com/jorm/internal/issue"
)

// Orchestrator manages the multi-agent workflow lifecycle.
type Orchestrator struct {
	bus      *bus.Bus
	sink     events.Sink
	cfg      *config.Config
	worktree *gitpkg.Worktree
	env      []string
}

// New creates an orchestrator.
func New(b *bus.Bus, cfg *config.Config, worktree *gitpkg.Worktree, sink events.Sink, env []string) *Orchestrator {
	return &Orchestrator{
		bus:      b,
		cfg:      cfg,
		worktree: worktree,
		sink:     sink,
		env:      env,
	}
}

// checkStopSignal returns true if the stop signal file exists for the given run ID.
func checkStopSignal(runID string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	signalPath := filepath.Join(home, ".jorm", fmt.Sprintf("stop-%s", runID))
	_, err = os.Stat(signalPath)
	return err == nil
}

// removeStopSignal removes the stop signal file for the given run ID.
func removeStopSignal(runID string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	os.Remove(filepath.Join(home, ".jorm", fmt.Sprintf("stop-%s", runID)))
}

// Run executes the multi-agent workflow for the given issue and agent template.
func (o *Orchestrator) Run(ctx context.Context, iss *issue.Issue, clusterID string, agentConfigs []AgentConfig) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Poll for stop signal file
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if checkStopSignal(clusterID) {
					o.sink.Phase("Stop signal detected, cancelling...")
					removeStopSignal(clusterID)
					cancel()
					return
				}
			}
		}
	}()

	// Add validators from the config profile as validator agents
	agentConfigs, err := o.injectValidators(agentConfigs)
	if err != nil {
		return fmt.Errorf("injecting validators: %w", err)
	}

	// Create agent instances
	agents := make([]*Agent, 0, len(agentConfigs))
	for _, cfg := range agentConfigs {
		a := NewAgent(cfg, o.bus, o.sink, clusterID, o.worktree.Dir, o.worktree.RepoDir, o.env)
		agents = append(agents, a)
	}

	// Subscribe to CLUSTER_COMPLETE before starting agents
	completeCh := o.bus.Subscribe(bus.TopicClusterComplete)
	defer o.bus.Unsubscribe(bus.TopicClusterComplete, completeCh)

	// Start all agents as goroutines
	var wg sync.WaitGroup
	agentErrors := make(chan error, len(agents))

	for _, a := range agents {
		wg.Add(1)
		go func(a *Agent) {
			defer wg.Done()
			if err := a.Run(ctx); err != nil && ctx.Err() == nil {
				agentErrors <- fmt.Errorf("agent %s: %w", a.Config.Name, err)
			}
		}(a)
	}

	// Publish ISSUE_OPENED to kick off the workflow
	o.sink.Phase("Starting workflow...")
	o.bus.Publish(bus.Message{
		ClusterID: clusterID,
		Topic:     bus.TopicIssueOpened,
		Sender:    "orchestrator",
		Content:   fmt.Sprintf("# %s\n\n%s", iss.Title, iss.Body),
		Data: map[string]any{
			"issue_id":    iss.ID,
			"issue_title": iss.Title,
			"issue_url":   iss.URL,
		},
	})
	o.sink.MessagePublished(bus.TopicIssueOpened, "orchestrator")

	// Wait for CLUSTER_COMPLETE or error
	for {
		select {
		case msg := <-completeCh:
			approved, _ := msg.Data["approved"].(bool)
			if approved {
				o.sink.Phase("Workflow completed successfully!")
				cancel()
				wg.Wait()
				return nil
			}
			// approved:false should not reach here after completion detector fix,
			// but defensively just continue waiting.

		case err := <-agentErrors:
			cancel()
			wg.Wait()
			return err

		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		}
	}
}

// injectValidators adds validator agents from the config profile into the agent list.
// Validators trigger on IMPLEMENTATION_READY and publish VALIDATION_RESULT.
// Also replaces any existing completion agent with one that waits for ALL validators.
func (o *Orchestrator) injectValidators(configs []AgentConfig) ([]AgentConfig, error) {
	profile := o.cfg.Profile
	validators, err := o.cfg.ValidatorsForProfile(profile)
	if err != nil {
		return nil, err
	}

	// Remove any pre-existing completion agent (templates include one, but we inject our own).
	filtered := configs[:0]
	for _, c := range configs {
		if c.Role != "completion" {
			filtered = append(filtered, c)
		}
	}
	configs = filtered

	validatorCount := 0
	for _, v := range validators {
		if v.RunOn == "accept_only" {
			continue
		}

		id := "validator-" + v.ID

		switch v.Type {
		case "shell":
			configs = append(configs, AgentConfig{
				ID:            id,
				Name:          v.Name,
				Role:          "validator",
				ExecutionMode: "shell",
				Command:       v.Command,
				Triggers: []Trigger{
					{Topic: bus.TopicImplementationReady, Predicate: "always"},
				},
				MaxIterations: 0,
				OnComplete:    []OnCompleteAction{{Topic: bus.TopicValidationResult}},
			})
		case "claude":
			vCopy := v // capture loop variable
			var resultProcessor func(result *agent.ClaudeResult) map[string]any
			if vCopy.Mode == "action" {
				// Action mode: passes if Claude exits cleanly (no error).
				resultProcessor = func(result *agent.ClaudeResult) map[string]any {
					return map[string]any{
						"approved":     true,
						"validator_id": id,
					}
				}
			} else {
				// Review mode (default): check for VERDICT: ACCEPT.
				resultProcessor = func(result *agent.ClaudeResult) map[string]any {
					approved := false
					if result != nil {
						approved = contains(result.Text, "VERDICT: ACCEPT")
					}
					return map[string]any{
						"approved":     approved,
						"validator_id": id,
					}
				}
			}
			configs = append(configs, AgentConfig{
				ID:            id,
				Name:          vCopy.Name,
				Role:          "validator",
				ExecutionMode: "claude",
				ReviewMode:    vCopy.Mode != "action",
				Triggers: []Trigger{
					{Topic: bus.TopicImplementationReady, Predicate: "always"},
				},
				Prompt:          vCopy.Prompt,
				Model:           "sonnet",
				MaxIterations:   0,
				OnComplete:      []OnCompleteAction{{Topic: bus.TopicValidationResult}},
				ContextBuilder:  BuildValidatorContext,
				ResultProcessor: resultProcessor,
			})
		default:
			return nil, fmt.Errorf("unknown validator type %q for %q", v.Type, v.ID)
		}

		validatorCount++
	}

	// If no validators were injected, add a simple pass-through completion agent.
	if validatorCount == 0 {
		configs = append(configs, AgentConfig{
			ID:            "completion",
			Name:          "Completion",
			Role:          "completion",
			ExecutionMode: "passthrough",
			Triggers:      []Trigger{{Topic: bus.TopicImplementationReady, Predicate: "always"}},
			MaxIterations: 1,
			OnComplete:    []OnCompleteAction{{Topic: bus.TopicClusterComplete}},
			TriggerProcessor: func(msg bus.Message) (map[string]any, bool) {
				return map[string]any{"approved": true}, true
			},
		})
		return configs, nil
	}

	// Add completion agent that waits for ALL validators before publishing CLUSTER_COMPLETE.
	expectedCount := validatorCount
	approvedSet := &sync.Map{}

	configs = append(configs, AgentConfig{
		ID:            "completion",
		Name:          "Completion",
		Role:          "completion",
		ExecutionMode: "passthrough",
		Triggers:      []Trigger{{Topic: bus.TopicValidationResult, Predicate: "always"}},
		MaxIterations: 0,
		OnComplete:    []OnCompleteAction{{Topic: bus.TopicClusterComplete}},
		TriggerProcessor: func(msg bus.Message) (map[string]any, bool) {
			approved, _ := msg.Data["approved"].(bool)
			validatorID, _ := msg.Data["validator_id"].(string)

			if !approved {
				// Reset approved set for next validation round.
				// Don't publish CLUSTER_COMPLETE — let the worker's
				// VALIDATION_RESULT:rejected trigger handle retry.
				approvedSet = &sync.Map{}
				return nil, false
			}

			approvedSet.Store(validatorID, true)
			count := 0
			approvedSet.Range(func(_, _ any) bool { count++; return true })

			if count >= expectedCount {
				return map[string]any{"approved": true}, true
			}
			return nil, false // not all validators have approved yet
		},
	})

	return configs, nil
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
