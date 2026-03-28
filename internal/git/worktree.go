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
	inPlace bool // true when operating in-place (no git worktree created)
}

// CreateWorktree creates a new git worktree and branch for the given issue.
func CreateWorktree(repoDir, issueID string) (*Worktree, error) {
	branch := fmt.Sprintf("jorm/issue-%s", issueID)
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("jorm-%s", issueID))

	// Clean up stale worktree/branch from previous runs
	_ = exec.Command("git", "worktree", "remove", dir, "--force").Run()
	_ = exec.Command("git", "branch", "-D", branch).Run()

	// Remove leftover temp dir if it exists
	_ = os.RemoveAll(dir)

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

// Diff returns the full diff of all changes (committed + uncommitted) relative to the base branch.
func (w *Worktree) Diff() (string, error) {
	if w.inPlace {
		return w.diffInPlace()
	}

	base, err := w.baseBranch()
	if err != nil {
		return "", err
	}

	// Single unified diff: base against working tree (committed + uncommitted in one pass).
	cmd := exec.Command("git", "diff", base)
	cmd.Dir = w.Dir
	diff, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting diff: %w", err)
	}

	// Also check for untracked files
	cmd = exec.Command("git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = w.Dir
	untracked, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("listing untracked files: %w", err)
	}

	var combined strings.Builder
	combined.Write(diff)

	// For untracked files, generate a diff-like output
	for _, f := range strings.Split(strings.TrimSpace(string(untracked)), "\n") {
		if f == "" {
			continue
		}
		cmd = exec.Command("git", "diff", "--no-index", "/dev/null", f)
		cmd.Dir = w.Dir
		out, _ := cmd.Output() // exit code 1 is expected for new files
		if len(out) > 0 {
			combined.Write(out)
		}
	}

	return combined.String(), nil
}

// diffInPlace returns the diff against HEAD for in-place worktrees.
func (w *Worktree) diffInPlace() (string, error) {
	// Staged + unstaged changes against HEAD
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = w.Dir
	uncommitted, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting uncommitted diff: %w", err)
	}

	// Untracked files
	cmd = exec.Command("git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = w.Dir
	untracked, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("listing untracked files: %w", err)
	}

	var combined strings.Builder
	combined.Write(uncommitted)

	for _, f := range strings.Split(strings.TrimSpace(string(untracked)), "\n") {
		if f == "" {
			continue
		}
		cmd = exec.Command("git", "diff", "--no-index", "/dev/null", f)
		cmd.Dir = w.Dir
		out, _ := cmd.Output()
		if len(out) > 0 {
			combined.Write(out)
		}
	}

	return combined.String(), nil
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

	if w.inPlace {
		return false, nil
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

// Cleanup removes the worktree and its branch. No-op for in-place worktrees.
func (w *Worktree) Cleanup() error {
	if w.inPlace {
		return nil
	}

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

// InPlaceWorktree returns a Worktree that operates in the current directory
// without creating a git worktree. Diff compares against HEAD, HasChanges
// checks the working tree, and Cleanup is a no-op.
func InPlaceWorktree(repoDir string) (*Worktree, error) {
	// Get current branch name
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting current branch: %w", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		branch = "HEAD"
	}

	return &Worktree{
		Branch:  branch,
		Dir:     repoDir,
		RepoDir: repoDir,
		inPlace: true,
	}, nil
}

// ReconstructWorktree creates a Worktree from persisted state (for resume).
func ReconstructWorktree(branch, dir, repoDir string, inPlace bool) *Worktree {
	return &Worktree{
		Branch:  branch,
		Dir:     dir,
		RepoDir: repoDir,
		inPlace: inPlace,
	}
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
