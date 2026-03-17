package hooks

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/jorm/internal/config"
	"github.com/jorm/internal/events"
)

// Runner executes hook commands in the worktree directory.
type Runner struct {
	cfg     config.HooksConfig
	workDir string
	sink    events.Sink
	env     []string
}

// NewRunner creates a hook runner for the given worktree directory.
func NewRunner(cfg config.HooksConfig, workDir string, sink events.Sink, env []string) *Runner {
	return &Runner{cfg: cfg, workDir: workDir, sink: sink, env: env}
}

// OnComplete runs all on_complete hooks sequentially.
func (r *Runner) OnComplete(ctx context.Context) error {
	for _, cmd := range r.cfg.OnComplete {
		r.sink.Phase(fmt.Sprintf("Running hook: %s", cmd))
		output, err := r.run(ctx, cmd)
		if output != "" {
			r.sink.ClaudeOutput(fmt.Sprintf("[hook: %s]\n%s", cmd, output))
		}
		if err != nil {
			errMsg := fmt.Sprintf("Hook %q failed: %v\nOutput: %s", cmd, err, output)
			r.sink.Phase(errMsg)
			r.sink.ClaudeOutput(errMsg)
			return fmt.Errorf("%s", errMsg)
		}
		r.sink.Phase(fmt.Sprintf("✓ %s", cmd))
	}
	return nil
}

// OnFailure runs all on_failure hooks sequentially.
func (r *Runner) OnFailure(ctx context.Context) error {
	for _, cmd := range r.cfg.OnFailure {
		r.sink.Phase(fmt.Sprintf("Running hook: %s", cmd))
		output, err := r.run(ctx, cmd)
		if output != "" {
			r.sink.ClaudeOutput(output)
		}
		if err != nil {
			return fmt.Errorf("on_failure hook %q: %w", cmd, err)
		}
	}
	return nil
}

func (r *Runner) run(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = r.workDir
	cmd.Env = r.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}
