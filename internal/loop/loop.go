package loop

import (
	"context"
	"fmt"

	"github.com/jorm/internal/cluster"
	"github.com/jorm/internal/config"
	"github.com/jorm/internal/events"
	gitpkg "github.com/jorm/internal/git"
	"github.com/jorm/internal/hooks"
	"github.com/jorm/internal/issue"
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
		provider, err := issue.NewProvider(cfg.IssueProvider.Type, cfg.IssueProvider.TokenEnv)
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

	// Run cluster
	cl, err := cluster.New(cfg, profile, wt, sink)
	if err != nil {
		return err
	}

	if err := cl.Run(ctx, iss); err != nil {
		runState.Status = "rejected"
		st.Save(runState)
		hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir)
		hookRunner.OnFailure(ctx)
		return err
	}

	// Run accept-only validators
	sink.Phase("Running post-accept validators...")
	if err := cl.RunAcceptOnlyValidators(ctx); err != nil {
		runState.Status = "failed"
		st.Save(runState)
		return err
	}

	// Run hooks
	sink.Phase("Running completion hooks...")
	hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir)
	if err := hookRunner.OnComplete(ctx); err != nil {
		sink.Phase(fmt.Sprintf("Hook failed: %s", err))
	}

	runState.Status = "accepted"
	st.Save(runState)
	return nil
}

func resume(ctx context.Context, cfg *config.Config, st *store.Store, profile string, opts Options, sink events.Sink) error {
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
		provider, err := issue.NewProvider(cfg.IssueProvider.Type, cfg.IssueProvider.TokenEnv)
		if err != nil {
			return fmt.Errorf("creating issue provider: %w", err)
		}
		iss, err = provider.Fetch(ctx, opts.IssueID)
		if err != nil {
			return fmt.Errorf("fetching issue: %w", err)
		}
	}
	sink.IssueLoaded(iss.Title, iss.URL)

	runState.Status = "running"
	st.Save(runState)

	cl, err := cluster.New(cfg, profile, wt, sink)
	if err != nil {
		return err
	}

	if err := cl.Run(ctx, iss); err != nil {
		runState.Status = "rejected"
		st.Save(runState)
		return err
	}

	sink.Phase("Running post-accept validators...")
	if err := cl.RunAcceptOnlyValidators(ctx); err != nil {
		runState.Status = "failed"
		st.Save(runState)
		return err
	}

	sink.Phase("Running completion hooks...")
	hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir)
	if err := hookRunner.OnComplete(ctx); err != nil {
		sink.Phase(fmt.Sprintf("Hook failed: %s", err))
	}

	runState.Status = "accepted"
	st.Save(runState)
	return nil
}
