//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	calibrationRepo = "git@github.com:HundredAcreStudio/jorm-calibration.git"
)

// JormBinary returns the path to the jorm binary.
func JormBinary() string {
	if bin := os.Getenv("JORM_BINARY"); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}
	root := repoRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "bin", "jorm")
}

// CloneCalibrationRepo clones the calibration repo into a temp dir and returns the path.
func CloneCalibrationRepo(dir string) (string, error) {
	cloneDir := filepath.Join(dir, "calibration")
	cmd := exec.Command("git", "clone", "--depth=1", calibrationRepo, cloneDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cloning: %s: %w", string(out), err)
	}
	return cloneDir, nil
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

const (
	// stallTimeout is how long RunJorm tolerates silence on the message bus
	// before concluding the run is hung and killing it.
	stallTimeout = 3 * time.Minute

	// pollInterval is how often we check the message bus for new activity.
	pollInterval = 5 * time.Second
)

// RunJorm executes jorm against an issue in the given work directory.
// It monitors the SQLite message bus for activity and kills the process
// if no new messages appear within stallTimeout.
func RunJorm(workDir string, issueID string) (*RunResult, error) {
	bin := JormBinary()
	if bin == "" {
		return nil, fmt.Errorf("jorm binary not found — run: CGO_ENABLED=1 go build -o bin/jorm ./cmd/jorm")
	}

	cmd := exec.Command(bin, "run", issueID)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "JORM_BINARY="+bin)

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting jorm: %w", err)
	}

	// Wait for process in background
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	// Monitor message bus; closes stalled channel if no activity for stallTimeout
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stalledCh := make(chan struct{}, 1)
	go watchBusActivity(ctx, issueID, stalledCh)

	var err error
	select {
	case err = <-waitErr:
		cancel()
	case <-stalledCh:
		cmd.Process.Kill()
		<-waitErr
		return nil, fmt.Errorf("jorm stalled: no message bus activity for %v", stallTimeout)
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("running jorm: %w", err)
		}
	}

	result := &RunResult{
		ExitCode: exitCode,
		Output:   outBuf.String(),
		WorkDir:  workDir,
		IssueID:  issueID,
	}

	// Find log file (most recent for this issue)
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".jorm", "logs")
	entries, _ := os.ReadDir(logDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), issueID+"-") {
			result.LogFile = filepath.Join(logDir, e.Name())
		}
	}

	if result.LogFile != "" {
		result.Stages = parseStages(result.LogFile)
	}

	result.Compiles = Compiles(workDir)
	result.TestsPass = TestsPass(workDir)
	result.FilesAdded = AddedFiles(workDir)

	return result, nil
}

// watchBusActivity polls the jorm SQLite DB for message bus activity.
// It sends on stalled if no new messages appear for stallTimeout.
// It returns silently if ctx is cancelled (process finished normally).
func watchBusActivity(ctx context.Context, issueID string, stalled chan<- struct{}) {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".jorm", "jorm.db")

	db, err := sql.Open("sqlite3", dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return // can't monitor — don't interfere with the run
	}
	defer db.Close()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var runID string
	var msgCount int
	lastActivity := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// Find the run ID if we haven't yet
		if runID == "" {
			_ = db.QueryRowContext(ctx,
				"SELECT id FROM runs WHERE issue_id = ? ORDER BY created_at DESC LIMIT 1",
				issueID,
			).Scan(&runID)
			if runID != "" {
				lastActivity = time.Now()
			}
			continue
		}

		// Count messages — any increase means progress
		var count int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM messages WHERE cluster_id = ?",
			runID,
		).Scan(&count); err == nil && count > msgCount {
			msgCount = count
			lastActivity = time.Now()
		}

		if time.Since(lastActivity) > stallTimeout {
			stalled <- struct{}{}
			return
		}
	}
}

// parseStages extracts stage events from the log file.
func parseStages(logFile string) []StageEvent {
	data, err := os.ReadFile(logFile)
	if err != nil {
		return nil
	}

	var events []StageEvent
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "stage.started") && !strings.Contains(line, "stage.completed") {
			continue
		}
		event := StageEvent{}
		if strings.Contains(line, "stage.started") {
			event.Event = "started"
		} else {
			event.Event = "completed"
		}
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
	return events
}

// Compiles returns true if `go build ./...` succeeds in the directory.
func Compiles(workDir string) bool {
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = workDir
	return cmd.Run() == nil
}

// TestsPass returns true if `go test ./...` succeeds in the directory.
func TestsPass(workDir string) bool {
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = workDir
	return cmd.Run() == nil
}

// TestsPassWithRace returns true if `go test -race` succeeds for a package.
func TestsPassWithRace(workDir, pkg string) bool {
	cmd := exec.Command("go", "test", "-race", pkg)
	cmd.Dir = workDir
	return cmd.Run() == nil
}

// AddedFiles returns files added in the most recent commit.
func AddedFiles(workDir string) []string {
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

// CommitMessage returns the most recent commit message.
func CommitMessage(workDir string) string {
	cmd := exec.Command("git", "log", "-1", "--format=%B")
	cmd.Dir = workDir
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

// CompletedStageNames returns just the completed stage names in order.
func CompletedStageNames(stages []StageEvent) []string {
	var names []string
	for _, s := range stages {
		if s.Event == "completed" {
			names = append(names, s.Name)
		}
	}
	return names
}

// HasFile checks if a file exists relative to the work dir.
func HasFile(workDir, path string) bool {
	_, err := os.Stat(filepath.Join(workDir, path))
	return err == nil
}

// PostReviewResult holds the outcome of running review prompts against a completed run.
type PostReviewResult struct {
	ExitCode int
	Output   string
	PRReview string // "ACCEPT", "REJECT", "UNKNOWN", "SKIP"
	Security string
	Tester   string
}

// RunPostReview executes the post-review script against a completed jorm run.
// Returns nil if the script is not found (non-fatal).
func RunPostReview(workDir string) (*PostReviewResult, error) {
	root := repoRoot()
	script := filepath.Join(root, "scripts", "post-review.sh")
	if _, err := os.Stat(script); err != nil {
		return nil, nil // script not found, skip
	}

	cmd := exec.Command(script, workDir, "--json")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	result := &PostReviewResult{
		ExitCode: exitCode,
		Output:   string(out),
	}

	// Parse JSON output for individual results
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "pr-review") {
			result.PRReview = extractVerdict(line)
		} else if strings.Contains(line, "security-review") {
			result.Security = extractVerdict(line)
		} else if strings.Contains(line, "tester-review") {
			result.Tester = extractVerdict(line)
		}
	}

	return result, nil
}

func extractVerdict(line string) string {
	for _, v := range []string{"ACCEPT", "REJECT", "SKIP", "UNKNOWN"} {
		if strings.Contains(line, v) {
			return v
		}
	}
	return "UNKNOWN"
}

func repoRoot() string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
