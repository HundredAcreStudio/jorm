package issue

import (
	"context"
	"fmt"
	"os"
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
		return nil, fmt.Errorf("GITHUB_TOKEN not set")
	}

	ghRepo := os.Getenv("GITHUB_REPOSITORY")
	if ghRepo == "" {
		return nil, fmt.Errorf("GITHUB_REPOSITORY not set (expected owner/repo)")
	}

	parts := strings.SplitN(ghRepo, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("GITHUB_REPOSITORY must be in owner/repo format, got %q", ghRepo)
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
