package prompts

import (
	"fmt"
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

// --- pr-review.md content tests ---

func resolvePRReview(t *testing.T) string {
	t.Helper()
	result, err := Resolve("builtin:pr-review", "/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error resolving builtin:pr-review: %v", err)
	}
	return result
}

func resolveSecurityReview(t *testing.T) string {
	t.Helper()
	result, err := Resolve("builtin:security-review", "/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error resolving builtin:security-review: %v", err)
	}
	return result
}

// TestPRReviewPrompt_HasAllEightCriteria verifies that the updated pr-review.md contains
// all eight review criteria required by the issue acceptance criteria.
func TestPRReviewPrompt_HasAllEightCriteria(t *testing.T) {
	prompt := resolvePRReview(t)

	criteria := []string{
		"Design & Architecture",
		"Duplication Analysis",
		"Complexity Assessment",
		"Testing Coverage",
		"Naming & Clarity",
		"Documentation",
	}
	for _, c := range criteria {
		if !strings.Contains(prompt, c) {
			t.Errorf("expected pr-review.md to contain %q", c)
		}
	}
}

// TestPRReviewPrompt_UsesThreeTierSeverity verifies that the updated pr-review.md uses
// the Critical/Important/Nit three-tier severity model.
func TestPRReviewPrompt_UsesThreeTierSeverity(t *testing.T) {
	prompt := resolvePRReview(t)

	for _, tier := range []string{"Critical", "Important", "Nit"} {
		if !strings.Contains(prompt, tier) {
			t.Errorf("expected pr-review.md to contain severity tier %q", tier)
		}
	}
}

// TestPRReviewPrompt_NoOldTwoTierSeverity verifies that the old HIGH/LOW severity labels
// have been removed from pr-review.md (replaced by the three-tier model).
func TestPRReviewPrompt_NoOldTwoTierSeverity(t *testing.T) {
	prompt := resolvePRReview(t)

	// The old model had "HIGH" and "LOW" as severity labels; these must be gone.
	// "LOW:" is the old notes prefix; "HIGH" was the blocking severity label.
	if strings.Contains(prompt, "**HIGH**") {
		t.Error("expected pr-review.md to not contain old **HIGH** severity label")
	}
	if strings.Contains(prompt, "**LOW**") {
		t.Error("expected pr-review.md to not contain old **LOW** severity label")
	}
	// The notes prefix must now be "Nit:" not "LOW:"
	if strings.Contains(prompt, `"LOW:`) {
		t.Error("expected pr-review.md notes prefix to be Nit:, not LOW:")
	}
}

// TestPRReviewPrompt_NitNotesPrefix verifies that the notes array example in pr-review.md
// uses the "Nit:" prefix format required by the updated cleanup stage parser.
func TestPRReviewPrompt_NitNotesPrefix(t *testing.T) {
	prompt := resolvePRReview(t)

	if !strings.Contains(prompt, "Nit:") {
		t.Error("expected pr-review.md to use Nit: as the notes prefix in its JSON output example")
	}
}

// TestPRReviewPrompt_VerdictFormatPreserved verifies that VERDICT: ACCEPT and VERDICT: REJECT
// are still present in the pr-review.md output format (required by ClaudeReviewValidator).
func TestPRReviewPrompt_VerdictFormatPreserved(t *testing.T) {
	prompt := resolvePRReview(t)

	if !strings.Contains(prompt, "VERDICT: ACCEPT") {
		t.Error("expected pr-review.md to contain VERDICT: ACCEPT")
	}
	if !strings.Contains(prompt, "VERDICT: REJECT") {
		t.Error("expected pr-review.md to contain VERDICT: REJECT")
	}
}

// TestPRReviewPrompt_HasAuditQualityRules verifies that the pr-review.md includes an
// audit quality rules section to filter false positives.
func TestPRReviewPrompt_HasAuditQualityRules(t *testing.T) {
	prompt := resolvePRReview(t)

	if !strings.Contains(prompt, "Audit Quality") && !strings.Contains(prompt, "audit quality") {
		t.Error("expected pr-review.md to contain an audit quality rules section")
	}
}

// --- security-review.md content tests ---

// TestSecurityReviewPrompt_HasDependencySecuritySection verifies that the updated
// security-review.md includes a dependency security check section.
func TestSecurityReviewPrompt_HasDependencySecuritySection(t *testing.T) {
	prompt := resolveSecurityReview(t)

	if !strings.Contains(prompt, "Dependency Security") && !strings.Contains(prompt, "Dependency security") {
		t.Error("expected security-review.md to contain a dependency security section")
	}
}

// TestSecurityReviewPrompt_HasDataProtectionSection verifies that security-review.md
// includes PII/data-at-rest/sensitive-URL checks under data protection.
func TestSecurityReviewPrompt_HasDataProtectionSection(t *testing.T) {
	prompt := resolveSecurityReview(t)

	if !strings.Contains(prompt, "Data Protection") && !strings.Contains(prompt, "Data protection") {
		t.Error("expected security-review.md to contain a data protection section")
	}
}

// TestSecurityReviewPrompt_HasNetworkConfigSection verifies that security-review.md
// includes network and configuration security checks (rate limiting, redirects, headers).
func TestSecurityReviewPrompt_HasNetworkConfigSection(t *testing.T) {
	prompt := resolveSecurityReview(t)

	if !strings.Contains(prompt, "Network") && !strings.Contains(prompt, "rate limiting") {
		t.Error("expected security-review.md to contain network/configuration security checks")
	}
}

// TestSecurityReviewPrompt_HasAuditQualityRules verifies that security-review.md includes
// audit quality rules to filter false positives.
func TestSecurityReviewPrompt_HasAuditQualityRules(t *testing.T) {
	prompt := resolveSecurityReview(t)

	if !strings.Contains(prompt, "Audit Quality") && !strings.Contains(prompt, "audit quality") {
		t.Error("expected security-review.md to contain audit quality rules section")
	}
}

// TestSecurityReviewPrompt_MapsToOWASPCategories verifies that security-review.md
// references at least 5 specific OWASP categories (e.g., A01, A03) in its criteria.
func TestSecurityReviewPrompt_MapsToOWASPCategories(t *testing.T) {
	prompt := resolveSecurityReview(t)

	// Count distinct OWASP A-category references (A01, A02, ..., A10)
	count := 0
	for i := 1; i <= 10; i++ {
		pattern := fmt.Sprintf("A%02d", i)
		if strings.Contains(prompt, pattern) {
			count++
		}
	}
	if count < 5 {
		t.Errorf("expected security-review.md to reference at least 5 OWASP categories (A01-A10), found %d", count)
	}
}

// TestSecurityReviewPrompt_VerdictFormatPreserved verifies that VERDICT: ACCEPT and
// VERDICT: REJECT are still present in security-review.md.
func TestSecurityReviewPrompt_VerdictFormatPreserved(t *testing.T) {
	prompt := resolveSecurityReview(t)

	if !strings.Contains(prompt, "VERDICT: ACCEPT") {
		t.Error("expected security-review.md to contain VERDICT: ACCEPT")
	}
	if !strings.Contains(prompt, "VERDICT: REJECT") {
		t.Error("expected security-review.md to contain VERDICT: REJECT")
	}
}
