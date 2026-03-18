package issue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// JiraProvider fetches issues from the Jira REST API.
type JiraProvider struct {
	baseURL string
	email   string
	token   string
	client  *http.Client
}

// NewJiraProvider creates a Jira issue provider.
// It reads JIRA_BASE_URL and JIRA_EMAIL from the environment.
// Uses Basic auth with base64(email:token) for Atlassian Cloud.
func NewJiraProvider(token string) (*JiraProvider, error) {
	baseURL := os.Getenv("JIRA_BASE_URL")
	if baseURL == "" {
		return nil, fmt.Errorf("JIRA_BASE_URL environment variable is required")
	}
	email := os.Getenv("JIRA_EMAIL")
	if email == "" {
		return nil, fmt.Errorf("JIRA_EMAIL environment variable is required")
	}
	if token == "" {
		return nil, fmt.Errorf("jira API token is required (set via providers.jira.token_var or providers.jira.token in config)")
	}
	return &JiraProvider{
		baseURL: baseURL,
		email:   email,
		token:   token,
		client:  &http.Client{},
	}, nil
}

// jiraIssueResponse is the relevant subset of the Jira issue API response.
type jiraIssueResponse struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string `json:"summary"`
		Description string `json:"description"`
	} `json:"fields"`
	Self string `json:"self"`
}

// Fetch retrieves a Jira issue by key (e.g. "PROJ-123").
func (p *JiraProvider) Fetch(ctx context.Context, id string) (*Issue, error) {
	url := fmt.Sprintf("%s/rest/api/3/issue/%s", p.baseURL, id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating jira request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(p.email+":"+p.token)))
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching jira issue %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jira API returned %d: %s", resp.StatusCode, string(body))
	}

	var jiraIssue jiraIssueResponse
	if err := json.NewDecoder(resp.Body).Decode(&jiraIssue); err != nil {
		return nil, fmt.Errorf("decoding jira response: %w", err)
	}

	issueURL := fmt.Sprintf("%s/browse/%s", p.baseURL, jiraIssue.Key)

	return &Issue{
		ID:    jiraIssue.Key,
		Title: jiraIssue.Fields.Summary,
		Body:  jiraIssue.Fields.Description,
		URL:   issueURL,
	}, nil
}
