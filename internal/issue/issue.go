package issue

import (
	"context"
	"fmt"
)

// Issue represents a work item from any provider.
type Issue struct {
	ID    string
	Title string
	Body  string
	URL   string
}

// Provider fetches issues from an external system.
type Provider interface {
	Fetch(ctx context.Context, id string) (*Issue, error)
}

// NewProvider creates a provider based on the type string.
func NewProvider(providerType, token string) (Provider, error) {
	switch providerType {
	case "github":
		return NewGitHubProvider(token)
	case "linear":
		return NewLinearProvider()
	case "jira":
		return NewJiraProvider(token)
	case "file":
		return nil, fmt.Errorf("file provider requires a path; use NewFileProvider directly")
	case "string":
		return nil, fmt.Errorf("string provider requires a prompt; use NewStringProvider directly")
	default:
		return NewGitHubProvider(token)
	}
}
