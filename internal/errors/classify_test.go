package errors_test

import (
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	jormerrors "github.com/jorm/internal/errors"
)

// TestAllErrorKindsDefined verifies that all 5 specific error kind constants are defined,
// non-empty, and distinct. Covers AC7.
func TestAllErrorKindsDefined(t *testing.T) {
	kinds := []struct {
		name string
		kind jormerrors.ErrorKind
	}{
		{"ErrKindRateLimit", jormerrors.ErrKindRateLimit},
		{"ErrKindAuth", jormerrors.ErrKindAuth},
		{"ErrKindNetwork", jormerrors.ErrKindNetwork},
		{"ErrKindCrash", jormerrors.ErrKindCrash},
		{"ErrKindShell", jormerrors.ErrKindShell},
	}
	seen := map[jormerrors.ErrorKind]bool{}
	for _, tc := range kinds {
		if tc.kind == "" {
			t.Errorf("%s must be non-empty", tc.name)
		}
		if seen[tc.kind] {
			t.Errorf("duplicate error kind value %q for %s", tc.kind, tc.name)
		}
		seen[tc.kind] = true
	}
	if jormerrors.ErrKindUnknown == "" {
		t.Error("ErrKindUnknown must be non-empty")
	}
	// All 6 values (5 specific + Unknown) must be distinct
	if _, dup := seen[jormerrors.ErrKindUnknown]; dup {
		t.Errorf("ErrKindUnknown %q collides with another kind", jormerrors.ErrKindUnknown)
	}
}

// TestClassifyError_FunctionExists verifies ClassifyError is exported and returns non-nil.
// Covers AC6.
func TestClassifyError_FunctionExists(t *testing.T) {
	result := jormerrors.ClassifyError(fmt.Errorf("test error"))
	if result == nil {
		t.Fatal("ClassifyError must return non-nil *ClassifiedError")
	}
}

// TestClassifiedError_ImplementsError verifies *ClassifiedError satisfies the error interface.
func TestClassifiedError_ImplementsError(t *testing.T) {
	result := jormerrors.ClassifyError(fmt.Errorf("test"))
	var _ error = result // compile-time assertion
	if result.Error() == "" {
		t.Error("Error() must return non-empty string")
	}
}

// TestClassifyError_WrapsOriginal verifies errors.Is works with the wrapped original error.
func TestClassifyError_WrapsOriginal(t *testing.T) {
	sentinel := fmt.Errorf("sentinel error")
	result := jormerrors.ClassifyError(sentinel)
	if !stderrors.Is(result, sentinel) {
		t.Error("ClassifiedError must wrap the original so errors.Is(result, original) is true")
	}
}

// TestClassifyError_ClassifiedHasMsg verifies ClassifiedError.Msg is populated.
func TestClassifyError_ClassifiedHasMsg(t *testing.T) {
	result := jormerrors.ClassifyError(fmt.Errorf("rate limit exceeded"))
	if result.Msg == "" {
		t.Error("ClassifiedError.Msg must be non-empty")
	}
}

// TestClassifyError_RateLimit verifies "rate limit" in the error message → ErrKindRateLimit.
func TestClassifyError_RateLimit(t *testing.T) {
	tests := []string{
		"rate limit exceeded",
		"claude exited with error: exit status 1\nstderr: rate limit reached",
		"Error: rate limit",
		"You've hit your limit · resets 5pm (America/New_York)",
		"claude exited with error: exit status 1\nstderr: You've hit your limit · resets 5pm (America/New_York)",
		"resets 5pm (America/New_York)",
	}
	for _, msg := range tests {
		result := jormerrors.ClassifyError(fmt.Errorf("%s", msg))
		if result.Kind != jormerrors.ErrKindRateLimit {
			t.Errorf("msg %q: expected ErrKindRateLimit, got %q", msg, result.Kind)
		}
	}
}

// TestClassifyError_RateLimitHint verifies rate limit errors include a resume hint.
func TestClassifyError_RateLimitHint(t *testing.T) {
	result := jormerrors.ClassifyError(fmt.Errorf("rate limit exceeded"))
	if result.Hint == "" {
		t.Error("expected non-empty Hint for rate limit (resumable) error")
	}
	if !strings.Contains(strings.ToLower(result.Hint), "resume") {
		t.Errorf("Hint should mention 'resume', got %q", result.Hint)
	}
}

// TestClassifyError_Auth verifies authentication-related errors → ErrKindAuth.
func TestClassifyError_Auth(t *testing.T) {
	tests := []string{
		"authentication failed: invalid API key",
		"unauthorized: check your credentials",
	}
	for _, msg := range tests {
		result := jormerrors.ClassifyError(fmt.Errorf("%s", msg))
		if result.Kind != jormerrors.ErrKindAuth {
			t.Errorf("msg %q: expected ErrKindAuth, got %q", msg, result.Kind)
		}
	}
}

// TestClassifyError_Network verifies network-related errors → ErrKindNetwork.
func TestClassifyError_Network(t *testing.T) {
	tests := []string{
		"connection refused: dial tcp 127.0.0.1:443",
		"request timed out after 30s",
		"context deadline exceeded (timeout)",
	}
	for _, msg := range tests {
		result := jormerrors.ClassifyError(fmt.Errorf("%s", msg))
		if result.Kind != jormerrors.ErrKindNetwork {
			t.Errorf("msg %q: expected ErrKindNetwork, got %q", msg, result.Kind)
		}
	}
}

// TestClassifyError_ExitStatus verifies "exit status" errors → ErrKindCrash or ErrKindShell.
// The exact kind depends on context; both are valid for exit-status failures.
func TestClassifyError_ExitStatus(t *testing.T) {
	err := fmt.Errorf("claude exited with error: exit status 1\nstderr: some output")
	result := jormerrors.ClassifyError(err)
	if result.Kind != jormerrors.ErrKindCrash && result.Kind != jormerrors.ErrKindShell {
		t.Errorf("exit status error: expected ErrKindCrash or ErrKindShell, got %q", result.Kind)
	}
}

// TestClassifyError_Unknown verifies unrecognized errors → ErrKindUnknown.
func TestClassifyError_Unknown(t *testing.T) {
	result := jormerrors.ClassifyError(fmt.Errorf("some completely unexpected condition xyz"))
	if result.Kind != jormerrors.ErrKindUnknown {
		t.Errorf("expected ErrKindUnknown for unrecognized error, got %q", result.Kind)
	}
}

// TestClassifyError_NonResumableNoHint verifies auth/network/unknown errors have no resume hint.
func TestClassifyError_NonResumableNoHint(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"auth", fmt.Errorf("authentication failed")},
		{"network", fmt.Errorf("connection refused")},
		{"unknown", fmt.Errorf("something completely unexpected xyz")},
	}
	for _, tc := range tests {
		result := jormerrors.ClassifyError(tc.err)
		if result.Hint != "" {
			t.Errorf("%s (kind=%q): expected empty Hint for non-resumable error, got %q",
				tc.name, result.Kind, result.Hint)
		}
	}
}

// TestClassifyError_RateLimitPriority verifies rate limit detection takes priority over exit status.
func TestClassifyError_RateLimitPriority(t *testing.T) {
	err := fmt.Errorf("exit status 1: rate limit reached")
	result := jormerrors.ClassifyError(err)
	if result.Kind != jormerrors.ErrKindRateLimit {
		t.Errorf("expected ErrKindRateLimit to take priority over exit status, got %q", result.Kind)
	}
}

// TestClassifyError_AlreadyClassified verifies that ClassifyError unwraps an already-classified
// error from the chain rather than re-classifying from the string representation.
// This is critical: agent.go classifies at creation, then stages.go wraps 2-3 times via
// fmt.Errorf("%w"). Without errors.As, the re-classification loses Kind/Hint.
func TestClassifyError_AlreadyClassified(t *testing.T) {
	// Simulate the pipeline: agent.go returns a ClassifiedError (crash)
	original := &jormerrors.ClassifiedError{
		Kind:    jormerrors.ErrKindCrash,
		Msg:     "claude process crashed",
		Hint:    "run `jorm resume <run-id>` to retry from the failed stage",
		Wrapped: fmt.Errorf("claude exited with error: exit status 1"),
	}

	// stages.go wraps it 2-3 times
	wrapped1 := fmt.Errorf("reviewer: %w", original)
	wrapped2 := fmt.Errorf("stage %q: %w", "PR Review", wrapped1)

	// ui.go calls ClassifyError on the fully wrapped error
	result := jormerrors.ClassifyError(wrapped2)

	if result.Kind != jormerrors.ErrKindCrash {
		t.Errorf("expected ErrKindCrash, got %q (re-classification lost the original kind)", result.Kind)
	}
	if result.Hint == "" {
		t.Error("expected non-empty Hint (resume hint lost during re-classification)")
	}
	if result.Msg != "claude process crashed" {
		t.Errorf("expected original Msg, got %q", result.Msg)
	}
}

// TestClassifyError_AlreadyClassifiedRateLimit verifies wrapped rate limit errors preserve Hint.
func TestClassifyError_AlreadyClassifiedRateLimit(t *testing.T) {
	original := &jormerrors.ClassifiedError{
		Kind:    jormerrors.ErrKindRateLimit,
		Msg:     "claude rate limit reached",
		Hint:    "run `jorm resume <run-id>` to retry from the failed stage",
		Wrapped: fmt.Errorf("rate limit exceeded"),
	}
	wrapped := fmt.Errorf("stage %q: reviewer: %w", "PR Review", original)

	result := jormerrors.ClassifyError(wrapped)
	if result.Kind != jormerrors.ErrKindRateLimit {
		t.Errorf("expected ErrKindRateLimit, got %q", result.Kind)
	}
	if !strings.Contains(result.Hint, "resume") {
		t.Errorf("expected resume hint preserved, got %q", result.Hint)
	}
}

// TestClassifyError_CrashHint verifies crash errors (claude process exit) include a resume hint.
func TestClassifyError_CrashHint(t *testing.T) {
	// A crash (claude exit without rate limit) should be resumable
	result := jormerrors.ClassifyError(fmt.Errorf("claude exited with error: exit status 1"))
	if result.Kind == jormerrors.ErrKindCrash && result.Hint == "" {
		t.Errorf("ErrKindCrash should include a resume hint, got empty Hint")
	}
}
