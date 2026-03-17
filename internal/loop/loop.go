package loop

import (
	"context"
	"fmt"

	"github.com/fatih/color"

	"github.com/jorm/internal/cluster"
	"github.com/jorm/internal/config"
	gitpkg "github.com/jorm/internal/git"
	"github.com/jorm/internal/hooks"
	"github.com/jorm/internal/issue"
	"github.com/jorm/internal/store"
)

var (
	bold   = color.New(color.Bold).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
	cyan   = color.New(color.FgCyan).SprintFunc()
)

// Options configures a loop run.
type Options struct {
	ConfigPath string
	RepoDir    string
	Profile    string
	IssueID    string
	Resume     bool
}

// Run orchestrates the full jorm lifecycle.
func Run(ctx context.Context, opts Options) error {
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
		return resume(ctx, cfg, st, profile, opts)
	}

	// Fetch issue
	provider, err := issue.NewProvider(cfg.IssueProvider.Type)
	if err != nil {
		return fmt.Errorf("creating issue provider: %w", err)
	}

	fmt.Printf("%s Fetching issue %s...\n", cyan("→"), bold(opts.IssueID))
	iss, err := provider.Fetch(ctx, opts.IssueID)
	if err != nil {
		return fmt.Errorf("fetching issue: %w", err)
	}
	fmt.Printf("%s %s\n", green("✓"), iss.Title)

	// Create worktree
	fmt.Printf("%s Creating worktree...\n", cyan("→"))
	wt, err := gitpkg.CreateWorktree(opts.RepoDir, opts.IssueID)
	if err != nil {
		return err
	}
	fmt.Printf("%s Branch %s at %s\n", green("✓"), bold(wt.Branch), wt.Dir)

	// Defer cleanup only if no changes
	defer func() {
		hasChanges, _ := wt.HasChanges()
		if !hasChanges {
			fmt.Printf("%s Cleaning up worktree (no changes)...\n", yellow("→"))
			wt.Cleanup()
		} else {
			fmt.Printf("%s Worktree kept at %s\n", cyan("ℹ"), wt.Dir)
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
	fmt.Printf("%s Starting dev loop (profile: %s, max attempts: %d)...\n", cyan("→"), bold(profile), cfg.MaxAttempts)
	cl, err := cluster.New(cfg, profile, wt)
	if err != nil {
		return err
	}

	if err := cl.Run(ctx, iss); err != nil {
		runState.Status = "rejected"
		st.Save(runState)
		hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir)
		hookRunner.OnFailure(ctx)
		fmt.Printf("%s %s\n", red("✗"), err)
		return err
	}

	// Run accept-only validators
	fmt.Printf("%s Running post-accept validators...\n", cyan("→"))
	if err := cl.RunAcceptOnlyValidators(ctx); err != nil {
		runState.Status = "failed"
		st.Save(runState)
		fmt.Printf("%s %s\n", red("✗"), err)
		return err
	}

	// Run hooks
	fmt.Printf("%s Running completion hooks...\n", cyan("→"))
	hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir)
	if err := hookRunner.OnComplete(ctx); err != nil {
		fmt.Printf("%s Hook failed: %s\n", yellow("⚠"), err)
	}

	runState.Status = "accepted"
	st.Save(runState)
	fmt.Printf("%s Issue %s completed successfully!\n", green("✓"), bold(opts.IssueID))
	return nil
}

func resume(ctx context.Context, cfg *config.Config, st *store.Store, profile string, opts Options) error {
	fmt.Printf("%s Resuming issue %s...\n", cyan("→"), bold(opts.IssueID))

	runState, err := st.LoadByIssue(opts.IssueID)
	if err != nil {
		return fmt.Errorf("no previous run found for issue %s: %w", opts.IssueID, err)
	}

	wt := &gitpkg.Worktree{
		Branch:  runState.Branch,
		Dir:     runState.WorktreeDir,
		RepoDir: opts.RepoDir,
	}

	provider, err := issue.NewProvider(cfg.IssueProvider.Type)
	if err != nil {
		return fmt.Errorf("creating issue provider: %w", err)
	}

	iss, err := provider.Fetch(ctx, opts.IssueID)
	if err != nil {
		return fmt.Errorf("fetching issue: %w", err)
	}

	runState.Status = "running"
	st.Save(runState)

	cl, err := cluster.New(cfg, profile, wt)
	if err != nil {
		return err
	}

	if err := cl.Run(ctx, iss); err != nil {
		runState.Status = "rejected"
		st.Save(runState)
		fmt.Printf("%s %s\n", red("✗"), err)
		return err
	}

	fmt.Printf("%s Running post-accept validators...\n", cyan("→"))
	if err := cl.RunAcceptOnlyValidators(ctx); err != nil {
		runState.Status = "failed"
		st.Save(runState)
		return err
	}

	fmt.Printf("%s Running completion hooks...\n", cyan("→"))
	hookRunner := hooks.NewRunner(cfg.Hooks, wt.Dir)
	if err := hookRunner.OnComplete(ctx); err != nil {
		fmt.Printf("%s Hook failed: %s\n", yellow("⚠"), err)
	}

	runState.Status = "accepted"
	st.Save(runState)
	fmt.Printf("%s Issue %s resumed and completed!\n", green("✓"), bold(opts.IssueID))
	return nil
}
