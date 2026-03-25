package orchestrator

import (
	"context"
	"fmt"

	"github.com/jorm/internal/bus"
	"github.com/jorm/internal/config"
	"github.com/jorm/internal/events"
	gitpkg "github.com/jorm/internal/git"
	"github.com/jorm/internal/issue"
)

// StageOrchestrator drives an ordered list of stages imperatively.
// Each review stage contains its own inner retry loop.
type StageOrchestrator struct {
	bus       *bus.Bus
	sink      events.Sink
	cfg       *config.Config
	env       []string
	clusterID string
	workDir   string
	repoDir   string
	worktree  *gitpkg.Worktree

	workerConfig AgentConfig
	testerConfig AgentConfig
	stages       []Stage
}

// NewStageOrchestrator creates a new StageOrchestrator.
func NewStageOrchestrator(
	b *bus.Bus, cfg *config.Config, wt *gitpkg.Worktree, sink events.Sink, env []string,
	clusterID string, workerConfig AgentConfig, testerConfig AgentConfig, stages []Stage,
) *StageOrchestrator {
	return &StageOrchestrator{
		bus:          b,
		sink:         sink,
		cfg:          cfg,
		env:          env,
		clusterID:    clusterID,
		workDir:      wt.Dir,
		repoDir:      wt.RepoDir,
		worktree:     wt,
		workerConfig: workerConfig,
		testerConfig: testerConfig,
		stages:       stages,
	}
}

// Run drives stages sequentially, publishing bus events for the audit trail.
func (so *StageOrchestrator) Run(ctx context.Context, iss *issue.Issue) error {
	// 1. Publish ISSUE_OPENED to seed the bus
	so.bus.Publish(bus.Message{
		ClusterID: so.clusterID,
		Topic:     bus.TopicIssueOpened,
		Sender:    "stage_orchestrator",
		Content:   fmt.Sprintf("# %s\n\n%s", iss.Title, iss.Body),
		Data: map[string]any{
			"issue_id":    iss.ID,
			"issue_title": iss.Title,
			"issue_url":   iss.URL,
		},
	})
	so.sink.MessagePublished(bus.TopicIssueOpened, "stage_orchestrator")

	// 2. Drive stages sequentially
	for i, stage := range so.stages {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		so.sink.StageStarted(i, stage.Name)

		var err error
		switch stage.Kind {
		case StageKindAgent:
			err = so.runAgentStage(ctx, stage)
		case StageKindReview:
			err = so.runReviewStage(ctx, i, stage)
		default:
			err = fmt.Errorf("unknown stage kind %q", stage.Kind)
		}
		if err != nil {
			return fmt.Errorf("stage %q: %w", stage.Name, err)
		}

		so.sink.StageCompleted(i, stage.Name)
	}

	// 3. Publish CLUSTER_COMPLETE
	so.bus.Publish(bus.Message{
		ClusterID: so.clusterID,
		Topic:     bus.TopicClusterComplete,
		Sender:    "stage_orchestrator",
		Data:      map[string]any{"approved": true},
	})
	so.sink.ClusterComplete(so.clusterID, "all_stages_completed")

	return nil
}

// runAgentStage runs a single agent (planner, test-writer) synchronously via ExecuteOnce.
func (so *StageOrchestrator) runAgentStage(ctx context.Context, stage Stage) error {
	if stage.AgentConfig == nil {
		return fmt.Errorf("stage %q has kind=agent but no AgentConfig", stage.Name)
	}

	a := NewAgent(*stage.AgentConfig, so.bus, so.sink, so.clusterID, so.workDir, so.repoDir, so.env)

	// Seed lastTrigger from the most recent ISSUE_OPENED message
	if msg, err := so.bus.FindLast(so.clusterID, bus.TopicIssueOpened); err == nil && msg != nil {
		a.lastTrigger = *msg
	}

	result, err := a.ExecuteOnce(ctx)
	if err != nil {
		return fmt.Errorf("agent %q: %w", stage.AgentConfig.Name, err)
	}

	// Publish OnComplete messages (mirrors Agent.Run's "claude" branch)
	data := map[string]any{
		"agent_id":  stage.AgentConfig.ID,
		"iteration": a.Iteration,
	}
	for k, v := range result.Data {
		data[k] = v
	}
	for _, action := range stage.AgentConfig.OnComplete {
		so.bus.Publish(bus.Message{
			ClusterID: so.clusterID,
			Topic:     action.Topic,
			Sender:    stage.AgentConfig.ID,
			Content:   result.Output,
			Data:      data,
		})
		so.sink.MessagePublished(action.Topic, stage.AgentConfig.ID)
	}

	return nil
}

// runReviewStage implements the inner retry loop: reviewer → worker fix → tester cycle.
func (so *StageOrchestrator) runReviewStage(ctx context.Context, stageIdx int, stage Stage) error {
	for attempt := 0; stage.MaxRetries == 0 || attempt < stage.MaxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 1. Run reviewer
		reviewResult, err := so.runReviewer(ctx, stage)
		if err != nil {
			return fmt.Errorf("reviewer: %w", err)
		}

		// 2. Publish VALIDATION_RESULT
		so.bus.Publish(bus.Message{
			ClusterID: so.clusterID,
			Topic:     bus.TopicValidationResult,
			Sender:    stage.ReviewerConfig.ID,
			Content:   reviewResult.Output,
			Data: map[string]any{
				"approved":     reviewResult.Approved,
				"validator_id": stage.ReviewerConfig.ID,
				"stage_index":  stageIdx,
				"stage":        stage.Name,
			},
		})
		so.sink.MessagePublished(bus.TopicValidationResult, stage.ReviewerConfig.ID)

		// 3. Approved → done
		if reviewResult.Approved {
			return nil
		}

		// 3b. Warn-only reviewers: log rejection but skip the fix loop
		if stage.ReviewerConfig.OnFail == "warn" || stage.ReviewerConfig.OnFail == "ignore" {
			return nil
		}

		// 4. Run worker fix
		if err := so.runWorkerFix(ctx, stageIdx, stage, reviewResult); err != nil {
			return fmt.Errorf("worker fix: %w", err)
		}

		// 5. Run tester
		testerResult, err := so.runTester(ctx)
		if err != nil {
			return fmt.Errorf("tester: %w", err)
		}

		// 6. If tests fail, run worker test fix
		if !testerResult.Approved {
			if err := so.runWorkerTestFix(ctx, testerResult); err != nil {
				return fmt.Errorf("worker test fix: %w", err)
			}
		}

		// 7. Publish fresh IMPLEMENTATION_READY with updated diff
		diff := ""
		if d, err := so.worktree.Diff(); err == nil {
			diff = d
		}
		so.bus.Publish(bus.Message{
			ClusterID: so.clusterID,
			Topic:     bus.TopicImplementationReady,
			Sender:    "stage_orchestrator",
			Content:   diff,
			Data: map[string]any{
				"stage":   stage.Name,
				"attempt": attempt + 1,
			},
		})
		so.sink.MessagePublished(bus.TopicImplementationReady, "stage_orchestrator")
	}

	return fmt.Errorf("stage %q exceeded max retries (%d)", stage.Name, stage.MaxRetries)
}

// runReviewer creates a reviewer agent and runs it once.
func (so *StageOrchestrator) runReviewer(ctx context.Context, stage Stage) (*AgentResult, error) {
	if stage.ReviewerConfig == nil {
		return nil, fmt.Errorf("stage %q has kind=review but no ReviewerConfig", stage.Name)
	}

	a := NewAgent(*stage.ReviewerConfig, so.bus, so.sink, so.clusterID, so.workDir, so.repoDir, so.env)

	// Seed from most recent IMPLEMENTATION_READY for diff context
	if msg, err := so.bus.FindLast(so.clusterID, bus.TopicImplementationReady); err == nil && msg != nil {
		a.lastTrigger = *msg
	}

	return a.ExecuteOnce(ctx)
}

// runWorkerFix runs the worker agent to fix issues identified by the reviewer.
func (so *StageOrchestrator) runWorkerFix(ctx context.Context, stageIdx int, stage Stage, reviewResult *AgentResult) error {
	a := NewAgent(so.workerConfig, so.bus, so.sink, so.clusterID, so.workDir, so.repoDir, so.env)

	// Seed from most recent VALIDATION_RESULT (the rejection we just published)
	if msg, err := so.bus.FindLast(so.clusterID, bus.TopicValidationResult); err == nil && msg != nil {
		a.lastTrigger = *msg
	}

	_, err := a.ExecuteOnce(ctx)
	return err
}

// runTester runs the tester agent (shell execution) and publishes the result.
func (so *StageOrchestrator) runTester(ctx context.Context) (*AgentResult, error) {
	a := NewAgent(so.testerConfig, so.bus, so.sink, so.clusterID, so.workDir, so.repoDir, so.env)

	result, err := a.ExecuteOnce(ctx)
	if err != nil {
		return nil, err
	}

	// Publish TESTS_READY for audit trail
	so.bus.Publish(bus.Message{
		ClusterID: so.clusterID,
		Topic:     bus.TopicTestsReady,
		Sender:    "stage_orchestrator",
		Content:   result.Output,
		Data: map[string]any{
			"approved": result.Approved,
		},
	})
	so.sink.MessagePublished(bus.TopicTestsReady, "stage_orchestrator")

	return result, nil
}

// runWorkerTestFix runs the worker agent to fix failing tests.
func (so *StageOrchestrator) runWorkerTestFix(ctx context.Context, testerResult *AgentResult) error {
	a := NewAgent(so.workerConfig, so.bus, so.sink, so.clusterID, so.workDir, so.repoDir, so.env)

	// Seed from most recent TESTS_READY (failure output as trigger context)
	if msg, err := so.bus.FindLast(so.clusterID, bus.TopicTestsReady); err == nil && msg != nil {
		a.lastTrigger = *msg
	}

	_, err := a.ExecuteOnce(ctx)
	return err
}
