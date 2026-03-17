package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// GitHubProvider fetches issues from the GitHub API.
type GitHubProvider struct {
	repo  string // owner/repo
	token string // may be empty if using gh CLI fallback
}

func NewGitHubProvider(tokenEnv string) (*GitHubProvider, error) {
	repo := inferRepo()
	if repo == "" {
		return nil, fmt.Errorf("could not determine GitHub repository (set GITHUB_REPOSITORY or ensure a GitHub git remote exists)")
	}

	// Check custom env var first, then standard fallbacks
	var token string
	if tokenEnv != "" {
		token = os.Getenv(tokenEnv)
	}
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	}

	return &GitHubProvider{repo: repo, token: token}, nil
}

func (p *GitHubProvider) Fetch(ctx context.Context, id string) (*Issue, error) {
	if p.token != "" {
		iss, err := p.fetchWithAPI(ctx, id)
		if err == nil {
			return iss, nil
		}
		// If API fails, try gh CLI as fallback
	}

	return p.fetchWithGH(ctx, id)
}

// fetchWithAPI uses the GitHub REST API directly.
func (p *GitHubProvider) fetchWithAPI(ctx context.Context, id string) (*Issue, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%s", p.repo, id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &Issue{
		ID:    id,
		Title: result.Title,
		Body:  result.Body,
		URL:   result.HTMLURL,
	}, nil
}

// fetchWithGH uses the gh CLI as a fallback.
func (p *GitHubProvider) fetchWithGH(ctx context.Context, id string) (*Issue, error) {
	cmd := exec.CommandContext(ctx, "gh", "issue", "view", id, "--repo", p.repo, "--json", "number,title,body,url")
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		return nil, fmt.Errorf("gh issue view failed: %w\n%s", err, stderr)
	}

	var result struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parsing gh output: %w", err)
	}

	return &Issue{
		ID:    id,
		Title: result.Title,
		Body:  result.Body,
		URL:   result.URL,
	}, nil
}

// inferRepo determines owner/repo from GITHUB_REPOSITORY env var or git remote.
func inferRepo() string {
	if r := os.Getenv("GITHUB_REPOSITORY"); r != "" {
		return r
	}

	for _, remote := range []string{"origin", "upstream"} {
		out, err := exec.Command("git", "remote", "get-url", remote).Output()
		if err != nil {
			continue
		}
		if repo, err := parseGitHubRepo(strings.TrimSpace(string(out))); err == nil {
			return repo
		}
	}

	return ""
}

// parseGitHubRepo extracts owner/repo from a GitHub remote URL.
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
