package issue

import (
	"context"
	"crypto/sha256"
	"fmt"
)

// StringProvider wraps a freeform prompt string as an Issue.
type StringProvider struct {
	prompt string
}

// NewStringProvider creates a provider that returns a static prompt as an issue.
func NewStringProvider(prompt string) *StringProvider {
	return &StringProvider{prompt: prompt}
}

// Fetch returns the prompt as an Issue.
func (p *StringProvider) Fetch(_ context.Context, _ string) (*Issue, error) {
	hash := sha256.Sum256([]byte(p.prompt))
	id := fmt.Sprintf("prompt-%x", hash[:8])

	return &Issue{
		ID:    id,
		Title: p.prompt,
		Body:  p.prompt,
	}, nil
}
