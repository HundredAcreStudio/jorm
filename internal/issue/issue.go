package issue

import "context"

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
	default:
		return NewGitHubProvider(token)
	}
}
