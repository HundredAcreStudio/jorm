package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/jorm/internal/config"
)

// Runner executes hook commands in the worktree directory.
type Runner struct {
	cfg     config.HooksConfig
	workDir string
}

// NewRunner creates a hook runner for the given worktree directory.
func NewRunner(cfg config.HooksConfig, workDir string) *Runner {
	return &Runner{cfg: cfg, workDir: workDir}
}

// OnComplete runs all on_complete hooks sequentially.
func (r *Runner) OnComplete(ctx context.Context) error {
	for _, cmd := range r.cfg.OnComplete {
		if err := r.run(ctx, cmd); err != nil {
			return fmt.Errorf("on_complete hook %q: %w", cmd, err)
		}
	}
	return nil
}

// OnFailure runs all on_failure hooks sequentially.
func (r *Runner) OnFailure(ctx context.Context) error {
	for _, cmd := range r.cfg.OnFailure {
		if err := r.run(ctx, cmd); err != nil {
			return fmt.Errorf("on_failure hook %q: %w", cmd, err)
		}
	}
	return nil
}

func (r *Runner) run(ctx context.Context, command string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = r.workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}
