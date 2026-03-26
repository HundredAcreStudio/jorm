//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	calibrationRepo = "git@github.com:HundredAcreStudio/jorm-calibration.git"
)

// jormBinary returns the path to the jorm binary.
// Build with: CGO_ENABLED=1 go build -o bin/jorm ./cmd/jorm
func jormBinary(t *testing.T) string {
	t.Helper()
	// Check for JORM_BINARY env var first
	if bin := os.Getenv("JORM_BINARY"); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}
	// Fall back to repo-relative path
	root := repoRoot(t)
	bin := filepath.Join(root, "bin", "jorm")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("jorm binary not found at %s — run: CGO_ENABLED=1 go build -o bin/jorm ./cmd/jorm", bin)
	}
	return bin
}

// repoRoot returns the jorm repo root directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("finding repo root: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// cloneCalibrationRepo clones the calibration repo into a unique temp dir.
// Returns the path to the clone.
func cloneCalibrationRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "calibration")

	cmd := exec.Command("git", "clone", "--depth=1", calibrationRepo, cloneDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cloning calibration repo: %s: %v", string(out), err)
	}
	return cloneDir
}

// RunResult holds the outcome of a jorm run.
type RunResult struct {
	ExitCode   int
	Output     string
	WorkDir    string
	IssueID    string
	LogFile    string
	Stages     []StageEvent
	Compiles   bool
	TestsPass  bool
	FilesAdded []string
}

// StageEvent is a parsed stage lifecycle event from the log.
type StageEvent struct {
	Index int
	Name  string
	Event string // "started" or "completed"
}

// runJorm executes jorm against an issue in the given work directory.
func runJorm(t *testing.T, workDir string, issueID string) *RunResult {
	t.Helper()
	bin := jormBinary(t)

	cmd := exec.Command(bin, "run", issueID)
	cmd.Dir = workDir
	// Inherit env for API keys, tokens, etc.
	cmd.Env = append(os.Environ(),
		"JORM_BINARY="+bin,
	)

	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("running jorm: %v", err)
		}
	}

	result := &RunResult{
		ExitCode: exitCode,
		Output:   string(out),
		WorkDir:  workDir,
		IssueID:  issueID,
	}

	// Find log file
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".jorm", "logs")
	entries, _ := os.ReadDir(logDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), issueID+"-") {
			result.LogFile = filepath.Join(logDir, e.Name())
		}
	}

	// Parse stages from log
	if result.LogFile != "" {
		result.Stages = parseStages(t, result.LogFile)
	}

	// Check compilation
	result.Compiles = checkCompiles(workDir)

	// Check tests
	result.TestsPass = checkTests(workDir)

	// Get added files
	result.FilesAdded = getAddedFiles(t, workDir)

	return result
}

// parseStages extracts stage events from the log file.
func parseStages(t *testing.T, logFile string) []StageEvent {
	t.Helper()
	data, err := os.ReadFile(logFile)
	if err != nil {
		return nil
	}

	var events []StageEvent
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "stage.started") || strings.Contains(line, "stage.completed") {
			event := StageEvent{}
			if strings.Contains(line, "stage.started") {
				event.Event = "started"
			} else {
				event.Event = "completed"
			}
			// Extract stage name from JSON — simple parsing
			if idx := strings.Index(line, `"stage":"`); idx >= 0 {
				rest := line[idx+9:]
				if end := strings.Index(rest, `"`); end >= 0 {
					event.Name = rest[:end]
				}
			}
			if idx := strings.Index(line, `"stage_index":`); idx >= 0 {
				rest := line[idx+14:]
				fmt.Sscanf(rest, "%d", &event.Index)
			}
			events = append(events, event)
		}
	}
	return events
}

// checkCompiles runs go build in the work dir and returns true if it succeeds.
func checkCompiles(workDir string) bool {
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = workDir
	return cmd.Run() == nil
}

// checkTests runs go test in the work dir and returns true if all pass.
func checkTests(workDir string) bool {
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = workDir
	return cmd.Run() == nil
}

// getAddedFiles returns files added in the most recent commit.
func getAddedFiles(t *testing.T, workDir string) []string {
	t.Helper()
	cmd := exec.Command("git", "diff", "HEAD~1", "--name-only", "--diff-filter=A")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// commitMessage returns the most recent commit message.
func commitMessage(t *testing.T, workDir string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "-1", "--format=%B")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("getting commit message: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// stageNames returns just the completed stage names in order.
func stageNames(stages []StageEvent) []string {
	var names []string
	for _, s := range stages {
		if s.Event == "completed" {
			names = append(names, s.Name)
		}
	}
	return names
}

// hasFile checks if a file exists in the work dir.
func hasFile(workDir, path string) bool {
	_, err := os.Stat(filepath.Join(workDir, path))
	return err == nil
}
