package prompts

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed *.md
var builtinFS embed.FS

// Resolve returns the prompt text. If the prompt starts with "builtin:",
// it first checks .jorm/prompts/<name>.md in the repo directory,
// then falls back to the embedded default.
// Otherwise returns the prompt string as-is.
func Resolve(prompt, repoDir string) (string, error) {
	if !strings.HasPrefix(prompt, "builtin:") {
		return prompt, nil
	}

	name := strings.TrimPrefix(prompt, "builtin:")
	filename := name + ".md"

	// Check .jorm/prompts/ in the repo first
	localPath := filepath.Join(repoDir, ".jorm", "prompts", filename)
	if data, err := os.ReadFile(localPath); err == nil {
		return string(data), nil
	}

	// Fall back to embedded default
	data, err := builtinFS.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("prompt %q not found (checked %s and builtins): %w", name, localPath, err)
	}

	return string(data), nil
}
