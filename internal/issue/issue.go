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
// tokenEnv is an optional env var name override for the auth token.
func NewProvider(providerType, tokenEnv string) (Provider, error) {
	switch providerType {
	case "github":
		return NewGitHubProvider(tokenEnv)
	case "linear":
		return NewLinearProvider()
	default:
		return NewGitHubProvider(tokenEnv)
	}
}
