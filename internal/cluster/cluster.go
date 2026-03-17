package cluster

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jorm/internal/agent"
	"github.com/jorm/internal/config"
	gitpkg "github.com/jorm/internal/git"
	"github.com/jorm/internal/issue"
)

// Cluster orchestrates the worker -> validate -> retry loop.
type Cluster struct {
	cfg        *config.Config
	profile    string
	worktree   *gitpkg.Worktree
	parallel   []agent.Validator
	sequential []agent.Validator
	acceptOnly []agent.Validator
}

// New builds a Cluster, splitting validators into parallel, sequential, and accept-only groups.
func New(cfg *config.Config, profile string, worktree *gitpkg.Worktree) (*Cluster, error) {
	validators, err := cfg.ValidatorsForProfile(profile)
	if err != nil {
		return nil, err
	}

	built, err := agent.BuildValidators(validators)
	if err != nil {
		return nil, err
	}

	c := &Cluster{
		cfg:      cfg,
		profile:  profile,
		worktree: worktree,
	}

	for _, v := range built {
		vcfg := v.Cfg()
		switch {
		case vcfg.RunOn == "accept_only":
			c.acceptOnly = append(c.acceptOnly, v)
		case vcfg.Parallel:
			c.parallel = append(c.parallel, v)
		default:
			c.sequential = append(c.sequential, v)
		}
	}

	return c, nil
}

// Run executes the main worker -> validate -> retry loop.
func (c *Cluster) Run(ctx context.Context, iss *issue.Issue) error {
	var findings string

	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		prompt := c.buildWorkerPrompt(iss, findings)

		_, err := agent.RunClaude(ctx, prompt, c.worktree.Dir, c.cfg.Model)
		if err != nil {
			return fmt.Errorf("worker attempt %d: %w", attempt, err)
		}

		diff, err := c.worktree.Diff()
		if err != nil {
			return fmt.Errorf("getting diff: %w", err)
		}

		if diff == "" {
			findings = "No changes were produced. You must modify files to implement the issue."
			continue
		}

		results := c.runValidators(ctx, diff)

		var rejected bool
		var findingsBuf strings.Builder
		for _, r := range results {
			if !r.Passed {
				findingsBuf.WriteString(fmt.Sprintf("### %s (%s)\n%s\n\n", r.Name, r.OnFail, r.Output))
				if r.IsBlocker() {
					rejected = true
				}
			}
		}

		if !rejected {
			return nil
		}

		findings = findingsBuf.String()
	}

	return fmt.Errorf("exhausted %d attempts without acceptance", c.cfg.MaxAttempts)
}

// RunAcceptOnlyValidators runs validators that only execute after the main loop succeeds.
func (c *Cluster) RunAcceptOnlyValidators(ctx context.Context) error {
	diff, err := c.worktree.Diff()
	if err != nil {
		return fmt.Errorf("getting diff for accept-only validators: %w", err)
	}

	for _, v := range c.acceptOnly {
		result := v.Validate(ctx, diff, c.worktree.Dir)
		if result.IsBlocker() {
			return fmt.Errorf("accept-only validator %q failed: %s", result.Name, result.Output)
		}
	}
	return nil
}

// runValidators fans out parallel validators via goroutines, then runs sequential ones in order.
func (c *Cluster) runValidators(ctx context.Context, diff string) []agent.ValidatorResult {
	results := make([]agent.ValidatorResult, 0, len(c.parallel)+len(c.sequential))

	// Fan out parallel validators
	if len(c.parallel) > 0 {
		ch := make(chan agent.ValidatorResult, len(c.parallel))
		var wg sync.WaitGroup

		for _, v := range c.parallel {
			wg.Add(1)
			go func(v agent.Validator) {
				defer wg.Done()
				ch <- v.Validate(ctx, diff, c.worktree.Dir)
			}(v)
		}

		wg.Wait()
		close(ch)

		for r := range ch {
			results = append(results, r)
		}
	}

	// Run sequential validators; short-circuit on blocking reject
	for _, v := range c.sequential {
		r := v.Validate(ctx, diff, c.worktree.Dir)
		results = append(results, r)
		if r.IsBlocker() {
			break
		}
	}

	return results
}

func (c *Cluster) buildWorkerPrompt(iss *issue.Issue, previousFindings string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", iss.Title))
	b.WriteString(fmt.Sprintf("Issue: %s\n\n", iss.URL))
	b.WriteString(iss.Body)

	if previousFindings != "" {
		b.WriteString("\n\n## Previous attempt was rejected. Fix these issues:\n\n")
		b.WriteString(previousFindings)
	}

	return b.String()
}
