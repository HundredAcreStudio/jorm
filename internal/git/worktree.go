package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree represents a git worktree created for an issue.
type Worktree struct {
	Branch  string
	Dir     string
	RepoDir string
}

// CreateWorktree creates a new git worktree and branch for the given issue.
func CreateWorktree(repoDir, issueID string) (*Worktree, error) {
	branch := fmt.Sprintf("jorm/issue-%s", issueID)
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("jorm-%s", issueID))

	cmd := exec.Command("git", "worktree", "add", "-b", branch, dir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("creating worktree: %s: %w", string(out), err)
	}

	return &Worktree{
		Branch:  branch,
		Dir:     dir,
		RepoDir: repoDir,
	}, nil
}

// Diff returns the diff between the base branch and HEAD.
func (w *Worktree) Diff() (string, error) {
	base, err := w.baseBranch()
	if err != nil {
		return "", err
	}

	cmd := exec.Command("git", "diff", base+"...HEAD")
	cmd.Dir = w.Dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting diff: %w", err)
	}
	return string(out), nil
}

// HasChanges returns true if there are uncommitted changes or commits ahead of base.
func (w *Worktree) HasChanges() (bool, error) {
	// Check for uncommitted changes
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = w.Dir
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("checking status: %w", err)
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return true, nil
	}

	// Check for commits ahead of base
	base, err := w.baseBranch()
	if err != nil {
		return false, err
	}
	cmd = exec.Command("git", "rev-list", "--count", base+"..HEAD")
	cmd.Dir = w.Dir
	out, err = cmd.Output()
	if err != nil {
		return false, fmt.Errorf("checking commits ahead: %w", err)
	}
	return strings.TrimSpace(string(out)) != "0", nil
}

// Cleanup removes the worktree and its branch.
func (w *Worktree) Cleanup() error {
	cmd := exec.Command("git", "worktree", "remove", w.Dir, "--force")
	cmd.Dir = w.RepoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("removing worktree: %s: %w", string(out), err)
	}

	cmd = exec.Command("git", "branch", "-D", w.Branch)
	cmd.Dir = w.RepoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("deleting branch: %s: %w", string(out), err)
	}

	return nil
}

// baseBranch detects whether the repo uses main or master.
func (w *Worktree) baseBranch() (string, error) {
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = w.RepoDir
	out, err := cmd.Output()
	if err != nil {
		// Fallback: check if main exists
		check := exec.Command("git", "rev-parse", "--verify", "main")
		check.Dir = w.RepoDir
		if check.Run() == nil {
			return "main", nil
		}
		return "master", nil
	}
	ref := strings.TrimSpace(string(out))
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1], nil
}
