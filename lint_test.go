package jorm_test

import (
	"os/exec"
	"testing"
)

// TestLint runs golangci-lint and fails if any issues are reported.
// This test enforces that all lint violations (currently 39 errcheck issues)
// are resolved. It will FAIL before the fix and PASS once all errcheck
// violations are addressed.
func TestLint(t *testing.T) {
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		t.Skip("golangci-lint not found in PATH; skipping lint test")
	}

	cmd := exec.Command("golangci-lint", "run", "./...")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("golangci-lint reported issues:\n%s", out)
	}
}
