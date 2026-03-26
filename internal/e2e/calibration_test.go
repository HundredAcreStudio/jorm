//go:build e2e

package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// These tests clone the calibration repo and run jorm against real GitHub issues.
// They require: jorm binary built, Claude API access, GitHub token, network access.
//
// Run with:
//   CGO_ENABLED=1 go test -tags e2e -timeout 30m -v ./internal/e2e/
//
// Run a single calibration issue:
//   CGO_ENABLED=1 go test -tags e2e -timeout 10m -v -run TestCalibration_Issue1 ./internal/e2e/

func TestCalibration_Issue1_StringReverse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	workDir := cloneCalibrationRepo(t)
	result := runJorm(t, workDir, "1")

	// Basic success
	if result.ExitCode != 0 {
		t.Fatalf("jorm exited with code %d:\n%s", result.ExitCode, result.Output)
	}

	// Must compile
	if !result.Compiles {
		t.Error("FAIL: code does not compile after jorm run")
	}

	// Must pass tests
	if !result.TestsPass {
		t.Error("FAIL: tests do not pass after jorm run")
	}

	// Must have created the expected files
	if !hasFile(workDir, "internal/utils/strings.go") {
		t.Error("FAIL: internal/utils/strings.go not created")
	}
	if !hasFile(workDir, "internal/utils/strings_test.go") {
		t.Error("FAIL: internal/utils/strings_test.go not created")
	}

	// Commit message should reference the issue
	msg := commitMessage(t, workDir)
	if !strings.Contains(msg, "Closes #1") {
		t.Errorf("FAIL: commit message missing 'Closes #1': %s", msg)
	}

	// Stages should have completed
	completed := stageNames(result.Stages)
	if len(completed) < 3 {
		t.Errorf("expected at least 3 completed stages, got %d: %v", len(completed), completed)
	}

	t.Logf("Stages completed: %v", completed)
	t.Logf("Files added: %v", result.FilesAdded)
	t.Logf("Commit: %s", strings.Split(msg, "\n")[0])
}

func TestCalibration_Issue2_HealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	workDir := cloneCalibrationRepo(t)
	result := runJorm(t, workDir, "2")

	if result.ExitCode != 0 {
		t.Fatalf("jorm exited with code %d:\n%s", result.ExitCode, result.Output)
	}

	if !result.Compiles {
		t.Error("FAIL: code does not compile")
	}
	if !result.TestsPass {
		t.Error("FAIL: tests do not pass")
	}

	// Health handler should exist
	if !hasFile(workDir, "internal/handler/health.go") {
		t.Error("FAIL: internal/handler/health.go not created")
	}

	// Health tests should exist
	if !hasFile(workDir, "internal/handler/health_test.go") {
		t.Error("FAIL: internal/handler/health_test.go not created")
	}

	msg := commitMessage(t, workDir)
	if !strings.Contains(msg, "Closes #2") {
		t.Errorf("FAIL: commit message missing 'Closes #2': %s", msg)
	}

	completed := stageNames(result.Stages)
	t.Logf("Stages completed: %v", completed)
	t.Logf("Files added: %v", result.FilesAdded)
	t.Logf("Commit: %s", strings.Split(msg, "\n")[0])
}

func TestCalibration_Issue3_LoggingMiddleware(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	workDir := cloneCalibrationRepo(t)
	result := runJorm(t, workDir, "3")

	if result.ExitCode != 0 {
		t.Fatalf("jorm exited with code %d:\n%s", result.ExitCode, result.Output)
	}

	if !result.Compiles {
		t.Error("FAIL: code does not compile")
	}
	if !result.TestsPass {
		t.Error("FAIL: tests do not pass")
	}

	// Middleware file should exist
	if !hasFile(workDir, "internal/middleware/logging.go") {
		t.Error("FAIL: internal/middleware/logging.go not created")
	}

	// Middleware tests should exist
	if !hasFile(workDir, "internal/middleware/logging_test.go") {
		t.Error("FAIL: internal/middleware/logging_test.go not created")
	}

	msg := commitMessage(t, workDir)
	if !strings.Contains(msg, "Closes #3") {
		t.Errorf("FAIL: commit message missing 'Closes #3': %s", msg)
	}

	completed := stageNames(result.Stages)
	t.Logf("Stages completed: %v", completed)
	t.Logf("Files added: %v", result.FilesAdded)
	t.Logf("Commit: %s", strings.Split(msg, "\n")[0])
}

func TestCalibration_Issue4_CacheRaceCondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	workDir := cloneCalibrationRepo(t)
	result := runJorm(t, workDir, "4")

	if result.ExitCode != 0 {
		t.Fatalf("jorm exited with code %d:\n%s", result.ExitCode, result.Output)
	}

	if !result.Compiles {
		t.Error("FAIL: code does not compile")
	}
	if !result.TestsPass {
		t.Error("FAIL: tests do not pass")
	}

	// Verify the race detector passes (the key requirement for this issue)
	if !checkTestsWithRace(workDir, "./internal/cache/...") {
		t.Error("FAIL: go test -race ./internal/cache/... fails — race condition not fixed")
	}

	msg := commitMessage(t, workDir)
	if !strings.Contains(msg, "Closes #4") {
		t.Errorf("FAIL: commit message missing 'Closes #4': %s", msg)
	}

	completed := stageNames(result.Stages)
	t.Logf("Stages completed: %v", completed)
	t.Logf("Commit: %s", strings.Split(msg, "\n")[0])
}

func TestCalibration_Issue5_UpdateUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	workDir := cloneCalibrationRepo(t)
	result := runJorm(t, workDir, "5")

	if result.ExitCode != 0 {
		t.Fatalf("jorm exited with code %d:\n%s", result.ExitCode, result.Output)
	}

	if !result.Compiles {
		t.Error("FAIL: code does not compile")
	}
	if !result.TestsPass {
		t.Error("FAIL: tests do not pass")
	}

	msg := commitMessage(t, workDir)
	if !strings.Contains(msg, "Closes #5") {
		t.Errorf("FAIL: commit message missing 'Closes #5': %s", msg)
	}

	completed := stageNames(result.Stages)
	t.Logf("Stages completed: %v", completed)
	t.Logf("Files added: %v", result.FilesAdded)
	t.Logf("Commit: %s", strings.Split(msg, "\n")[0])
}

// checkTestsWithRace runs go test with the race detector for a specific package.
func checkTestsWithRace(workDir, pkg string) bool {
	cmd := exec.Command("go", "test", "-race", pkg)
	cmd.Dir = workDir
	return cmd.Run() == nil
}
