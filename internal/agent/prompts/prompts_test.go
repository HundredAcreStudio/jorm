package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBuiltinRequirementsReview(t *testing.T) {
	result, err := Resolve("builtin:requirements-review", "/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error resolving builtin prompt: %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result for builtin:requirements-review")
	}

	if !strings.Contains(strings.ToLower(result), "acceptance criterion") &&
		!strings.Contains(strings.ToLower(result), "acceptance criteria") {
		t.Errorf("expected prompt to mention acceptance criteria, got:\n%s", result)
	}
}

func TestResolveBuiltinRequirementsReview_LocalOverride(t *testing.T) {
	tmpDir := t.TempDir()

	promptDir := filepath.Join(tmpDir, ".jorm", "prompts")
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatal(err)
	}

	customText := "Custom local requirements review prompt for testing"
	if err := os.WriteFile(filepath.Join(promptDir, "requirements-review.md"), []byte(customText), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Resolve("builtin:requirements-review", tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != customText {
		t.Errorf("expected local override text %q, got %q", customText, result)
	}
}

func TestResolveNonBuiltin(t *testing.T) {
	prompt := "Just a plain prompt string"
	result, err := Resolve(prompt, "/any/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != prompt {
		t.Errorf("expected %q, got %q", prompt, result)
	}
}

func TestResolveBuiltinNotFound(t *testing.T) {
	_, err := Resolve("builtin:nonexistent-prompt-that-does-not-exist", "/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent builtin prompt")
	}
}
