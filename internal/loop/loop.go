package loop

import (
	"context"
	"fmt"
	"log/slog"

	agentPkg "github.com/jorm/internal/agent"
	"github.com/jorm/internal/bus"
	"github.com/jorm/internal/conductor"
	"github.com/jorm/internal/config"
	"github.com/jorm/internal/events"
	gitpkg "github.com/jorm/internal/git"
	"github.com/jorm/internal/hooks"
	"github.com/jorm/internal/issue"
	jormlog "github.com/jorm/internal/log"
	"github.com/jorm/internal/orchestrator"
	"github.com/jorm/internal/store"
)

// Options configures a loop run.
type Options struct {
	ConfigPath  string
	RepoDir     string
	Profile     string
	IssueID     string
	Title       string
	Body        string
	Resume      bool
	Sink        events.Sink
	SinkFactory func(runID string, agentCount int) events.Sink // creates sink after runID is known
	Worktree    bool                                           // create git worktree (default: work in current dir)
	PR          bool                                           // create PR on completion (implies Worktree)
	Ship        bool                                           // PR + auto-merge (implies PR)
	Debug       bool                                           // enable debug logging
	Model       string                                         // model override
}

// Run orchestrates the full jorm lifecycle.
func Run(ctx context.Context, opts Options) error {
	// Flag implications: --ship implies --pr implies --worktree
	if opts.Ship {
		opts.PR = true
	}
	if opts.PR {
		opts.Worktree = true
	}

	sink := opts.Sink
	if sink == nil {
		sink = &events.PrintSink{}
	}

	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}

	// Apply model override from CLI
	if opts.Model != "" {
		cfg.Model = opts.Model
	}

	profile := cfg.Profile
	if opts.Profile != "" {
		profile = opts.Profile
	}

	st, err := store.New()
	if err != nil {
		return err
	}
	defer st.Close()

	// Generate sequential run ID
	runCount, err := st.CountRunsForIssue(opts.IssueID)
	if err != nil {
		return fmt.Errorf("counting runs for issue: %w", err)
	}
	runID := fmt.Sprintf("%s-%d", opts.IssueID, runCount+1)

	// If a SinkFactory is provided, create the sink now that we have the runID.
	if opts.SinkFactory != nil {
		sink = opts.SinkFactory(runID, 0) // agentCount not known yet, will be updated
	}

	// Create structured logger
	logger, err := jormlog.New(runID, opts.Debug)
	if err != nil {
		sink.Phase(fmt.Sprintf("Warning: could not create logger: %s", err))
	} else {
		defer logger.Close()
		slog.SetDefault(logger.SlogLogger())
		logger.Info("starting run", "issue_id", opts.IssueID, "worktree", opts.Worktree, "pr", opts.PR, "ship", opts.Ship)
		sink = jormlog.NewLogSink(sink, logger)
	}

	if opts.Resume {
		return resume(ctx, cfg, st, profile, opts, sink)
	}

	// Fetch issue
	var iss *issue.Issue
	if opts.Title != "" {
		iss = &issue.Issue{
			ID:    opts.IssueID,
			Title: opts.Title,
			Body:  opts.Body,
		}
		sink.IssueLoaded(iss.Title, "")
	} else {
		sink.Phase("Fetching issue...")
		provider, err := issue.NewProvider(cfg.IssueProvider, cfg.ProviderToken(cfg.IssueProvider))
		if err != nil {
			return fmt.Errorf("creating issue provider: %w", err)
		}
		iss, err = provider.Fetch(ctx, opts.IssueID)
		if err != nil {
			return fmt.Errorf("fetching issue: %w", err)
		}
		sink.IssueLoaded(iss.Title, iss.URL)
	}

	// Create worktree or use in-place
	var wt *gitpkg.Worktree
	if opts.Worktree {
		sink.Phase("Creating worktree...")
		wt, err = gitpkg.CreateWorktree(opts.RepoDir, opts.IssueID)
		if err != nil {
			return err
		}
		sink.Phase(fmt.Sprintf("Branch %s at %s", wt.Branch, wt.Dir))
	} else {
		sink.Phase("Working in current directory...")
		wt, err = gitpkg.InPlaceWorktree(opts.RepoDir)
		if err != nil {
			return err
		}
	}

	if logger != nil {
		logger.Info("worktree ready", "dir", wt.Dir, "branch", wt.Branch, "in_place", !opts.Worktree)
	}

	// Defer cleanup only if no changes (only for real worktrees)
	defer func() {
		hasChanges, _ := wt.HasChanges()
		if !hasChanges {
			sink.Phase("Cleaning up worktree (no changes)...")
			wt.Cleanup()
		} else {
			sink.Phase(fmt.Sprintf("Worktree kept at %s", wt.Dir))
		}
	}()

	// Persist initial state
	runState := &store.RunState{
		ID:          runID,
		IssueID:     opts.IssueID,
		Branch:      wt.Branch,
		WorktreeDir: wt.Dir,
		Status:      "running",
		InPlace:     !opts.Worktree,
	}
	if err := st.Save(runState); err != nil {
		return fmt.Errorf("saving run state: %w", err)
	}

	// Build env with issue context
	subEnv := issueEnv(cfg.SubprocessEnv(), iss, cfg.IssueProvider)

	// Run conductor-driven multi-agent workflow
	if err := runConductorMode(ctx, cfg, st, wt, sink, iss, runState, subEnv, opts); err != nil {
		return err
	}

	// Run hooks
	sink.Phase("Running completion hooks...")
	hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir, sink, subEnv)
	if err := hookRunner.OnComplete(ctx); err != nil {
		sink.Phase(fmt.Sprintf("Hook failed: %s", err))
	}

	runState.Status = "accepted"
	if err := st.Save(runState); err != nil {
		return fmt.Errorf("saving run state: %w", err)
	}
	return nil
}

// runConductorMode runs the multi-agent conductor workflow.
func runConductorMode(ctx context.Context, cfg *config.Config, st *store.Store, wt *gitpkg.Worktree, sink events.Sink, iss *issue.Issue, runState *store.RunState, subEnv []string, opts Options) error {
	// Create message bus
	msgBus := bus.New(st.DB())

	// Classify the issue
	cond := conductor.New(cfg.Conductor.ClassifyModel, wt.Dir, subEnv, sink, cfg.Conductor.Staged)
	cls, err := cond.Classify(ctx, iss)
	if err != nil {
		return fmt.Errorf("conductor classification: %w", err)
	}

	// Select workflow template
	templateName := cond.SelectTemplate(cls)
	sink.Classification(fmt.Sprintf("%s/%s", cls.Complexity, cls.Type))
	sink.Phase(fmt.Sprintf("Workflow: %s (%s/%s)", templateName, cls.Complexity, cls.Type))

	var runErr error
	useStaged := false
	if cfg.Conductor.Staged {
		stagedTmpl, err := conductor.BuildStagedTemplate(cfg, cfg.Profile)
		if err != nil {
			return fmt.Errorf("building staged template: %w", err)
		}
		useStaged = true
		so := orchestrator.NewStageOrchestrator(msgBus, cfg, wt, sink, subEnv, runState.ID, stagedTmpl.WorkerConfig, stagedTmpl.TesterConfig, stagedTmpl.Stages)
		runErr = so.Run(ctx, iss)
	}
	if !useStaged {
		templates := conductor.BuiltinTemplates(cfg.Model)
		agentConfigs, ok := templates[templateName]
		if !ok {
			return fmt.Errorf("unknown workflow template: %s", templateName)
		}
		orch := orchestrator.New(msgBus, cfg, wt, sink, subEnv)
		runErr = orch.Run(ctx, iss, runState.ID, agentConfigs)
	}
	if runErr != nil {
		runState.Status = "rejected"
		if saveErr := st.Save(runState); saveErr != nil {
			return fmt.Errorf("saving run state: %w (original: %w)", saveErr, runErr)
		}
		hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir, sink, subEnv)
		hookRunner.OnFailure(ctx)
		return runErr
	}

	// Run accept-only validators (commit, etc.)
	sink.Phase("Running post-accept validators...")
	validators, _ := cfg.ValidatorsForProfile(cfg.Profile)
	for _, v := range validators {
		if v.RunOn != "accept_only" {
			continue
		}
		built, err := buildSingleValidator(v, subEnv)
		if err != nil {
			return err
		}
		diff, _ := wt.Diff()
		result := built.Validate(ctx, diff, wt.Dir, wt.RepoDir)
		sink.ValidatorDone(result)
		if result.IsBlocker() {
			runState.Status = "failed"
			if saveErr := st.Save(runState); saveErr != nil {
				return fmt.Errorf("saving run state: %w", saveErr)
			}
			return fmt.Errorf("accept-only validator %q failed: %s", result.Name, result.Output)
		}
	}

	// Handle --pr/--ship: run pr-create action
	if opts.PR {
		sink.Phase("Creating PR...")
		prEnv := make([]string, len(subEnv), len(subEnv)+1)
		copy(prEnv, subEnv)
		if opts.Ship {
			prEnv = append(prEnv, "JORM_AUTO_MERGE=true")
		}

		diff, err := wt.Diff()
		if err != nil {
			return fmt.Errorf("getting diff for PR creation: %w", err)
		}
		prValidator := agentPkg.ClaudeActionValidator{
			Config: config.ValidatorConfig{
				ID:     "pr-create",
				Name:   "PR Creation",
				Type:   "claude",
				Mode:   "action",
				Prompt: "builtin:pr-create",
				OnFail: "warn",
			},
			Env: prEnv,
		}
		result := prValidator.Validate(ctx, diff, wt.Dir, wt.RepoDir)
		sink.ValidatorDone(result)
		if !result.Passed {
			sink.Phase(fmt.Sprintf("PR creation warning: %s", result.Output))
		}
	}

	runState.Status = "accepted"
	if err := st.Save(runState); err != nil {
		return fmt.Errorf("saving run state: %w", err)
	}
	return nil
}

// buildSingleValidator creates a single validator from config.
func buildSingleValidator(cfg config.ValidatorConfig, env []string) (agentPkg.Validator, error) {
	validators, err := agentPkg.BuildValidators([]config.ValidatorConfig{cfg})
	if err != nil {
		return nil, err
	}
	v := validators[0]
	// Inject env into action validators for JORM_CLOSES_REF etc.
	if av, ok := v.(*agentPkg.ClaudeActionValidator); ok {
		av.Env = env
	}
	return v, nil
}

// issueEnv appends issue context env vars to the base env.
func issueEnv(base []string, iss *issue.Issue, provider string) []string {
	env := make([]string, len(base), len(base)+5)
	copy(env, base)
	env = append(env, "JORM_ISSUE_ID="+iss.ID)
	env = append(env, "JORM_ISSUE_TITLE="+iss.Title)
	if iss.URL != "" {
		env = append(env, "JORM_ISSUE_URL="+iss.URL)
	}
	// Add closes reference for GitHub/Jira issues with numeric/key IDs
	if (provider == "github" || provider == "jira") && iss.ID != "" {
		env = append(env, "JORM_CLOSES_REF=Closes #"+iss.ID)
	}
	return env
}

func resume(ctx context.Context, cfg *config.Config, st *store.Store, _ string, opts Options, sink events.Sink) error {
	sink.Phase("Resuming...")

	runState, err := st.LoadByIssue(opts.IssueID)
	if err != nil {
		return fmt.Errorf("no previous run found for issue %s: %w", opts.IssueID, err)
	}

	wt := gitpkg.ReconstructWorktree(runState.Branch, runState.WorktreeDir, opts.RepoDir, runState.InPlace)

	var iss *issue.Issue
	if opts.Title != "" {
		iss = &issue.Issue{
			ID:    opts.IssueID,
			Title: opts.Title,
			Body:  opts.Body,
		}
	} else {
		provider, err := issue.NewProvider(cfg.IssueProvider, cfg.ProviderToken(cfg.IssueProvider))
		if err != nil {
			return fmt.Errorf("creating issue provider: %w", err)
		}
		iss, err = provider.Fetch(ctx, opts.IssueID)
		if err != nil {
			return fmt.Errorf("fetching issue: %w", err)
		}
	}
	sink.IssueLoaded(iss.Title, iss.URL)

	subEnv := issueEnv(cfg.SubprocessEnv(), iss, cfg.IssueProvider)

	runState.Status = "running"
	if err := st.Save(runState); err != nil {
		return fmt.Errorf("saving run state: %w", err)
	}

	if err := runConductorMode(ctx, cfg, st, wt, sink, iss, runState, subEnv, opts); err != nil {
		return err
	}

	// Run hooks
	sink.Phase("Running completion hooks...")
	hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir, sink, subEnv)
	if err := hookRunner.OnComplete(ctx); err != nil {
		sink.Phase(fmt.Sprintf("Hook failed: %s", err))
	}

	runState.Status = "accepted"
	if err := st.Save(runState); err != nil {
		return fmt.Errorf("saving run state: %w", err)
	}
	return nil
}
