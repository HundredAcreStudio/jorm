package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jorm/internal/agent/prompts"
	"github.com/jorm/internal/config"
)

// ValidatorResult holds the outcome of a single validator run.
type ValidatorResult struct {
	ValidatorID string
	Name        string
	Passed      bool
	OnFail      string
	Output      string
}

// IsBlocker returns true if this failed result should block acceptance.
func (r ValidatorResult) IsBlocker() bool {
	return !r.Passed && r.OnFail == "reject"
}

// Validator is the interface all validators implement.
type Validator interface {
	Validate(ctx context.Context, diff, workDir, repoDir string) ValidatorResult
	Cfg() config.ValidatorConfig
}

// ShellValidator runs a shell command; exit 0 means accept.
type ShellValidator struct {
	Config config.ValidatorConfig
}

func (v *ShellValidator) Cfg() config.ValidatorConfig { return v.Config }

func (v *ShellValidator) Validate(ctx context.Context, diff, workDir, repoDir string) ValidatorResult {
	if v.Config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, v.Config.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", v.Config.Command)
	cmd.Dir = workDir

	out, err := cmd.CombinedOutput()
	passed := err == nil

	return ValidatorResult{
		ValidatorID: v.Config.ID,
		Name:        v.Config.Name,
		Passed:      passed,
		OnFail:      v.Config.OnFail,
		Output:      string(out),
	}
}

// ClaudeReviewValidator runs a Claude review with fresh context (blind validation).
// Injects the diff into the prompt and looks for VERDICT: ACCEPT.
type ClaudeReviewValidator struct {
	Config config.ValidatorConfig
}

func (v *ClaudeReviewValidator) Cfg() config.ValidatorConfig { return v.Config }

func (v *ClaudeReviewValidator) Validate(ctx context.Context, diff, workDir, repoDir string) ValidatorResult {
	if v.Config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, v.Config.Timeout)
		defer cancel()
	}

	promptText, err := prompts.Resolve(v.Config.Prompt, repoDir)
	if err != nil {
		return ValidatorResult{
			ValidatorID: v.Config.ID,
			Name:        v.Config.Name,
			Passed:      false,
			OnFail:      v.Config.OnFail,
			Output:      fmt.Sprintf("prompt error: %v", err),
		}
	}

	prompt := fmt.Sprintf("%s\n\n## Diff to review\n\n```diff\n%s\n```\n\nEnd your response with exactly \"VERDICT: ACCEPT\" or \"VERDICT: REJECT\" followed by a brief reason.", promptText, diff)

	result, err := RunClaude(ctx, RunOptions{
		Prompt:  prompt,
		WorkDir: workDir,
		Model:   "sonnet",
	})
	if err != nil {
		return ValidatorResult{
			ValidatorID: v.Config.ID,
			Name:        v.Config.Name,
			Passed:      false,
			OnFail:      v.Config.OnFail,
			Output:      fmt.Sprintf("claude error: %v", err),
		}
	}

	passed := strings.Contains(result.Text, "VERDICT: ACCEPT")

	return ValidatorResult{
		ValidatorID: v.Config.ID,
		Name:        v.Config.Name,
		Passed:      passed,
		OnFail:      v.Config.OnFail,
		Output:      result.Text,
	}
}

// ClaudeActionValidator runs Claude with full tool access in the worktree.
// Passes if Claude exits cleanly (no VERDICT needed).
type ClaudeActionValidator struct {
	Config config.ValidatorConfig
	Env    []string // environment for subprocess; nil uses os.Environ()
}

func (v *ClaudeActionValidator) Cfg() config.ValidatorConfig { return v.Config }

func (v *ClaudeActionValidator) Validate(ctx context.Context, diff, workDir, repoDir string) ValidatorResult {
	if v.Config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, v.Config.Timeout)
		defer cancel()
	}

	promptText, err := prompts.Resolve(v.Config.Prompt, repoDir)
	if err != nil {
		return ValidatorResult{
			ValidatorID: v.Config.ID,
			Name:        v.Config.Name,
			Passed:      false,
			OnFail:      v.Config.OnFail,
			Output:      fmt.Sprintf("prompt error: %v", err),
		}
	}

	// Inject closes reference from env if available
	for _, e := range v.Env {
		if len(e) > 17 && e[:17] == "JORM_CLOSES_REF=" {
			ref := e[17:]
			if ref != "" {
				promptText = promptText + "\n\nIMPORTANT: Include \"" + ref + "\" on its own line in the commit message, before the Co-Authored-By line."
			}
			break
		}
	}

	result, err := RunClaude(ctx, RunOptions{
		Prompt:  promptText,
		WorkDir: workDir,
		Model:   "sonnet",
		Env:     v.Env,
	})
	if err != nil {
		return ValidatorResult{
			ValidatorID: v.Config.ID,
			Name:        v.Config.Name,
			Passed:      false,
			OnFail:      v.Config.OnFail,
			Output:      fmt.Sprintf("claude error: %v", err),
		}
	}

	return ValidatorResult{
		ValidatorID: v.Config.ID,
		Name:        v.Config.Name,
		Passed:      true,
		OnFail:      v.Config.OnFail,
		Output:      result.Text,
	}
}

// BuildValidators constructs validators from a config slice.
func BuildValidators(configs []config.ValidatorConfig) ([]Validator, error) {
	validators := make([]Validator, 0, len(configs))
	for _, cfg := range configs {
		switch cfg.Type {
		case "shell":
			validators = append(validators, &ShellValidator{Config: cfg})
		case "claude":
			if cfg.Mode == "action" {
				validators = append(validators, &ClaudeActionValidator{Config: cfg})
			} else {
				validators = append(validators, &ClaudeReviewValidator{Config: cfg})
			}
		default:
			return nil, fmt.Errorf("unknown validator type %q for %q", cfg.Type, cfg.ID)
		}
	}
	return validators, nil
}
