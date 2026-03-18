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
	select {
	case msg := <-completeCh:
		approved, _ := msg.Data["approved"].(bool)
		if !approved {
			cancel()
			wg.Wait()
			return fmt.Errorf("workflow completed with rejection")
		}
		o.sink.Phase("Workflow completed successfully!")
		cancel()
		wg.Wait()
		return nil

	case err := <-agentErrors:
		cancel()
		wg.Wait()
		return err

	case <-ctx.Done():
		wg.Wait()
		return ctx.Err()
	}
}

// injectValidators adds validator agents from the config profile into the agent list.
// Validators trigger on IMPLEMENTATION_READY and publish VALIDATION_RESULT.
func (o *Orchestrator) injectValidators(configs []AgentConfig) ([]AgentConfig, error) {
	profile := o.cfg.Profile
	validators, err := o.cfg.ValidatorsForProfile(profile)
	if err != nil {
		return nil, err
	}

	for _, v := range validators {
		if v.RunOn == "accept_only" {
			continue // handled separately after the workflow
		}

		configs = append(configs, AgentConfig{
			ID:   "validator-" + v.ID,
			Name: v.Name,
			Role: "validator",
			Triggers: []Trigger{
				{Topic: bus.TopicImplementationReady, Predicate: "always"},
			},
			Prompt:        v.Prompt,
			Model:         "sonnet",
			MaxIterations: 0, // unlimited — runs each time IMPLEMENTATION_READY fires
			OnComplete:    []OnCompleteAction{{Topic: bus.TopicValidationResult}},
			ContextBuilder: BuildValidatorContext,
			ResultProcessor: func(result *agent.ClaudeResult) map[string]any {
				// For now, simple pass/fail based on VERDICT
				approved := false
				if result != nil {
					approved = contains(result.Text, "VERDICT: ACCEPT")
				}
				return map[string]any{
					"approved":     approved,
					"validator_id": v.ID,
				}
			},
		})
	}

	return configs, nil
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
