package loop

import (
	"context"
	"fmt"

	agentPkg "github.com/jorm/internal/agent"
	"github.com/jorm/internal/bus"
	"github.com/jorm/internal/conductor"
	"github.com/jorm/internal/config"
	"github.com/jorm/internal/events"
	gitpkg "github.com/jorm/internal/git"
	"github.com/jorm/internal/hooks"
	"github.com/jorm/internal/issue"
	"github.com/jorm/internal/orchestrator"
	"github.com/jorm/internal/store"
)

// Options configures a loop run.
type Options struct {
	ConfigPath string
	RepoDir    string
	Profile    string
	IssueID    string
	Title      string
	Body       string
	Resume     bool
	Sink       events.Sink
}

// Run orchestrates the full jorm lifecycle.
func Run(ctx context.Context, opts Options) error {
	sink := opts.Sink
	if sink == nil {
		sink = &events.PrintSink{}
	}

	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
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

	// Create worktree
	sink.Phase("Creating worktree...")
	wt, err := gitpkg.CreateWorktree(opts.RepoDir, opts.IssueID)
	if err != nil {
		return err
	}
	sink.Phase(fmt.Sprintf("Branch %s at %s", wt.Branch, wt.Dir))

	// Defer cleanup only if no changes
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
		ID:          fmt.Sprintf("%s-%d", opts.IssueID, 1),
		IssueID:     opts.IssueID,
		Branch:      wt.Branch,
		WorktreeDir: wt.Dir,
		Status:      "running",
	}
	if err := st.Save(runState); err != nil {
		return fmt.Errorf("saving run state: %w", err)
	}

	// Build env with issue context
	subEnv := issueEnv(cfg.SubprocessEnv(), iss)

	// Run conductor-driven multi-agent workflow
	if err := runConductorMode(ctx, cfg, st, wt, sink, iss, runState, subEnv); err != nil {
		return err
	}

	// Run hooks
	sink.Phase("Running completion hooks...")
	hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir, sink, subEnv)
	if err := hookRunner.OnComplete(ctx); err != nil {
		sink.Phase(fmt.Sprintf("Hook failed: %s", err))
	}

	runState.Status = "accepted"
	st.Save(runState)
	return nil
}

// runConductorMode runs the multi-agent conductor workflow.
func runConductorMode(ctx context.Context, cfg *config.Config, st *store.Store, wt *gitpkg.Worktree, sink events.Sink, iss *issue.Issue, runState *store.RunState, subEnv []string) error {
	// Create message bus
	msgBus := bus.New(st.DB())

	// Classify the issue
	cond := conductor.New(cfg.Conductor.ClassifyModel, wt.Dir, subEnv, sink)
	cls, err := cond.Classify(ctx, iss)
	if err != nil {
		return fmt.Errorf("conductor classification: %w", err)
	}

	// Select workflow template
	templateName := cond.SelectTemplate(cls)
	sink.Phase(fmt.Sprintf("Workflow: %s (%s/%s)", templateName, cls.Complexity, cls.Type))

	templates := conductor.BuiltinTemplates()
	agentConfigs, ok := templates[templateName]
	if !ok {
		return fmt.Errorf("unknown workflow template: %s", templateName)
	}

	// Run the orchestrator
	orch := orchestrator.New(msgBus, cfg, wt, sink, subEnv)
	if err := orch.Run(ctx, iss, runState.ID, agentConfigs); err != nil {
		runState.Status = "rejected"
		st.Save(runState)
		hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir, sink, subEnv)
		hookRunner.OnFailure(ctx)
		return err
	}

	// Run accept-only validators (commit, etc.)
	sink.Phase("Running post-accept validators...")
	validators, _ := cfg.ValidatorsForProfile(cfg.Profile)
	for _, v := range validators {
		if v.RunOn != "accept_only" {
			continue
		}
		built, err := buildSingleValidator(v)
		if err != nil {
			return err
		}
		diff, _ := wt.Diff()
		result := built.Validate(ctx, diff, wt.Dir, wt.RepoDir)
		sink.ValidatorDone(result)
		if result.IsBlocker() {
			runState.Status = "failed"
			st.Save(runState)
			return fmt.Errorf("accept-only validator %q failed: %s", result.Name, result.Output)
		}
	}

	runState.Status = "accepted"
	st.Save(runState)
	return nil
}

// buildSingleValidator creates a single validator from config.
func buildSingleValidator(cfg config.ValidatorConfig) (agentPkg.Validator, error) {
	validators, err := agentPkg.BuildValidators([]config.ValidatorConfig{cfg})
	if err != nil {
		return nil, err
	}
	return validators[0], nil
}

// issueEnv appends issue context env vars to the base env.
func issueEnv(base []string, iss *issue.Issue) []string {
	env := make([]string, len(base), len(base)+3)
	copy(env, base)
	env = append(env, "JORM_ISSUE_ID="+iss.ID)
	env = append(env, "JORM_ISSUE_TITLE="+iss.Title)
	if iss.URL != "" {
		env = append(env, "JORM_ISSUE_URL="+iss.URL)
	}
	return env
}

func resume(ctx context.Context, cfg *config.Config, st *store.Store, _ string, opts Options, sink events.Sink) error {
	sink.Phase("Resuming...")

	runState, err := st.LoadByIssue(opts.IssueID)
	if err != nil {
		return fmt.Errorf("no previous run found for issue %s: %w", opts.IssueID, err)
	}

	wt := &gitpkg.Worktree{
		Branch:  runState.Branch,
		Dir:     runState.WorktreeDir,
		RepoDir: opts.RepoDir,
	}

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

	subEnv := issueEnv(cfg.SubprocessEnv(), iss)

	runState.Status = "running"
	st.Save(runState)

	if err := runConductorMode(ctx, cfg, st, wt, sink, iss, runState, subEnv); err != nil {
		return err
	}

	// Run hooks
	sink.Phase("Running completion hooks...")
	hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir, sink, subEnv)
	if err := hookRunner.OnComplete(ctx); err != nil {
		sink.Phase(fmt.Sprintf("Hook failed: %s", err))
	}

	runState.Status = "accepted"
	st.Save(runState)
	return nil
}
