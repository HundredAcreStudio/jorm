package issue

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/google/go-github/v58/github"
	"golang.org/x/oauth2"
)

// GitHubProvider fetches issues from the GitHub API.
type GitHubProvider struct {
	client *github.Client
	owner  string
	repo   string
}

func NewGitHubProvider() (*GitHubProvider, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN or GITHUB_PERSONAL_ACCESS_TOKEN not set")
	}

	ghRepo := os.Getenv("GITHUB_REPOSITORY")
	if ghRepo == "" {
		var err error
		ghRepo, err = inferRepoFromGit()
		if err != nil {
			return nil, fmt.Errorf("GITHUB_REPOSITORY not set and could not infer from git remote: %w", err)
		}
	}

	parts := strings.SplitN(ghRepo, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("could not parse owner/repo from %q", ghRepo)
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)

	return &GitHubProvider{
		client: github.NewClient(tc),
		owner:  parts[0],
		repo:   parts[1],
	}, nil
}

func (p *GitHubProvider) Fetch(ctx context.Context, id string) (*Issue, error) {
	num, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("invalid GitHub issue number %q: %w", id, err)
	}

	ghIssue, _, err := p.client.Issues.Get(ctx, p.owner, p.repo, num)
	if err != nil {
		return nil, fmt.Errorf("fetching GitHub issue #%d: %w", num, err)
	}

	return &Issue{
		ID:    id,
		Title: ghIssue.GetTitle(),
		Body:  ghIssue.GetBody(),
		URL:   ghIssue.GetHTMLURL(),
	}, nil
}

// inferRepoFromGit parses owner/repo from the git remote URL.
func inferRepoFromGit() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		// Try "upstream" if "origin" doesn't exist
		out, err = exec.Command("git", "remote", "get-url", "upstream").Output()
		if err != nil {
			return "", fmt.Errorf("no origin or upstream remote found")
		}
	}

	return parseGitHubRepo(strings.TrimSpace(string(out)))
}

// parseGitHubRepo extracts owner/repo from a GitHub remote URL.
// Supports SSH (git@github.com:owner/repo.git) and HTTPS (https://github.com/owner/repo.git).
func parseGitHubRepo(remote string) (string, error) {
	remote = strings.TrimSuffix(remote, ".git")

	// SSH: git@github.com:owner/repo
	if strings.HasPrefix(remote, "git@") {
		parts := strings.SplitN(remote, ":", 2)
		if len(parts) == 2 {
			return parts[1], nil
		}
	}

	// HTTPS: https://github.com/owner/repo
	if strings.Contains(remote, "github.com/") {
		idx := strings.Index(remote, "github.com/")
		return remote[idx+len("github.com/"):], nil
	}

	return "", fmt.Errorf("could not parse GitHub repo from remote %q", remote)
}
