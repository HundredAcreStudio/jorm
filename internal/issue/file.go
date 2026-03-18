package issue

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileProvider reads a markdown file and returns it as an Issue.
type FileProvider struct {
	path string
}

// NewFileProvider creates a provider that reads an issue from a file.
func NewFileProvider(path string) *FileProvider {
	return &FileProvider{path: path}
}

// Fetch reads the file and returns an Issue.
// The ID is the base filename, the Title is the first # heading, and the Body is the full content.
func (p *FileProvider) Fetch(_ context.Context, _ string) (*Issue, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return nil, fmt.Errorf("reading issue file %s: %w", p.path, err)
	}

	body := string(data)
	title := extractFirstHeading(body)
	if title == "" {
		title = filepath.Base(p.path)
	}

	id := strings.TrimSuffix(filepath.Base(p.path), filepath.Ext(p.path))

	return &Issue{
		ID:    id,
		Title: title,
		Body:  body,
	}, nil
}

// extractFirstHeading returns the text of the first markdown # heading.
func extractFirstHeading(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}
