package issue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// LinearProvider fetches issues from the Linear GraphQL API.
type LinearProvider struct {
	apiKey string
	client *http.Client
}

func NewLinearProvider() (*LinearProvider, error) {
	key := os.Getenv("LINEAR_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("LINEAR_API_KEY not set")
	}
	return &LinearProvider{
		apiKey: key,
		client: http.DefaultClient,
	}, nil
}

func (p *LinearProvider) Fetch(ctx context.Context, id string) (*Issue, error) {
	query := `query($id: String!) {
		issue(id: $id) {
			identifier
			title
			description
			url
		}
	}`

	type issueData struct {
		Identifier  string `json:"identifier"`
		Title       string `json:"title"`
		Description string `json:"description"`
		URL         string `json:"url"`
	}
	type responseData struct {
		Issue issueData `json:"issue"`
	}

	result, err := linearGraphQL[responseData](ctx, p.client, p.apiKey, query, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}

	return &Issue{
		ID:    result.Issue.Identifier,
		Title: result.Issue.Title,
		Body:  result.Issue.Description,
		URL:   result.Issue.URL,
	}, nil
}

// linearGraphQL is a generic helper for Linear GraphQL API calls.
func linearGraphQL[T any](ctx context.Context, client *http.Client, apiKey, query string, variables map[string]any) (*T, error) {
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.linear.app/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linear API returned status %d", resp.StatusCode)
	}

	var gqlResp struct {
		Data   T      `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("linear API error: %s", gqlResp.Errors[0].Message)
	}

	return &gqlResp.Data, nil
}
