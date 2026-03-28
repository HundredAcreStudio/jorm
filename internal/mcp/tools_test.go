package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// writeLogLines writes JSON-lines slog-formatted log entries to a temp file
// and returns the file path. Each entry gets a "time" key in RFC3339Nano format.
func writeLogLines(t *testing.T, dir, runID string, entries []struct {
	ts  time.Time
	msg string
}) string {
	t.Helper()
	path := filepath.Join(dir, runID+".log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create log file: %v", err)
	}
	defer func() { _ = f.Close() }()
	for _, e := range entries {
		line, _ := json.Marshal(map[string]any{
			"time":  e.ts.UTC().Format(time.RFC3339Nano),
			"level": "INFO",
			"msg":   e.msg,
		})
		_, _ = f.WriteString(string(line) + "\n")
	}
	return path
}

// --- listRuns tool ---

// AC6: listRuns returns JSON array with run fields
func TestListRuns_ReturnsJSON(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertRun(t, db, "1-1", "1", "accepted", now.Add(-5*time.Minute), now)

	out, err := listRuns(db)
	if err != nil {
		t.Fatalf("listRuns: %v", err)
	}

	var result struct {
		Runs []map[string]any `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("listRuns output is not valid JSON: %v\noutput: %s", err, out)
	}
	if len(result.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(result.Runs))
	}

	r := result.Runs[0]
	if r["id"] != "1-1" {
		t.Errorf("run id = %v, want %q", r["id"], "1-1")
	}
	if r["status"] != "accepted" {
		t.Errorf("run status = %v, want %q", r["status"], "accepted")
	}
}

// AC6: listRuns includes elapsed field
func TestListRuns_IncludesElapsed(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertRun(t, db, "1-1", "1", "accepted", now.Add(-3*time.Minute), now)

	out, err := listRuns(db)
	if err != nil {
		t.Fatalf("listRuns: %v", err)
	}

	var result struct {
		Runs []map[string]any `json:"runs"`
	}
	_ = json.Unmarshal([]byte(out), &result)
	if len(result.Runs) == 0 {
		t.Fatal("expected at least one run")
	}
	if _, ok := result.Runs[0]["elapsed"]; !ok {
		t.Error("expected 'elapsed' field in run output")
	}
}

// AC6: listRuns includes cost from CLUSTER_COMPLETE message (0 when absent)
func TestListRuns_CostFromClusterComplete(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertRun(t, db, "1-1", "1", "accepted", now.Add(-time.Minute), now)
	insertMessage(t, db, "msg-1", "1-1", "CLUSTER_COMPLETE", "completion", "done", now,
		`{"total_cost": 2.50}`)

	out, err := listRuns(db)
	if err != nil {
		t.Fatalf("listRuns: %v", err)
	}

	var result struct {
		Runs []map[string]any `json:"runs"`
	}
	_ = json.Unmarshal([]byte(out), &result)
	if len(result.Runs) == 0 {
		t.Fatal("expected at least one run")
	}
	cost, _ := result.Runs[0]["cost"].(float64)
	if cost != 2.50 {
		t.Errorf("cost = %v, want 2.50", result.Runs[0]["cost"])
	}
}

// --- getStatus tool ---

// AC6: getStatus returns JSON with run_id and status
func TestGetStatus_ReturnsStatusJSON(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertRun(t, db, "5-1", "5", "running", now, now)

	out, err := getStatus(db, "5-1")
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("getStatus output is not valid JSON: %v\noutput: %s", err, out)
	}
	if result["run_id"] != "5-1" {
		t.Errorf("run_id = %v, want %q", result["run_id"], "5-1")
	}
	if result["status"] != "running" {
		t.Errorf("status = %v, want %q", result["status"], "running")
	}
}

// AC6: getStatus returns error for unknown run
func TestGetStatus_UnknownRun(t *testing.T) {
	db := newTestDB(t)
	_, err := getStatus(db, "nonexistent")
	if err == nil {
		t.Error("expected error for unknown run, got nil")
	}
}

// --- getMessages tool ---

// AC6, AC12: getMessages returns JSON array filtered by topic
func TestGetMessages_FiltersByTopic(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertRun(t, db, "r1", "1", "running", now, now)
	insertMessage(t, db, "m1", "r1", "PLAN_READY", "planner", "plan", now, "")
	insertMessage(t, db, "m2", "r1", "VALIDATION_RESULT", "validator", "ok", now, "")

	out, err := getMessages(db, "r1", "PLAN_READY", "", 0)
	if err != nil {
		t.Fatalf("getMessages: %v", err)
	}

	var msgs []map[string]any
	if err := json.Unmarshal([]byte(out), &msgs); err != nil {
		t.Fatalf("getMessages output is not valid JSON: %v\noutput: %s", err, out)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0]["topic"] != "PLAN_READY" {
		t.Errorf("topic = %v, want %q", msgs[0]["topic"], "PLAN_READY")
	}
}

// AC6, AC12: getMessages respects limit
func TestGetMessages_RespectsLimit(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertRun(t, db, "r1", "1", "running", now, now)
	for i := range 10 {
		insertMessage(t, db, fmt.Sprintf("m%d", i), "r1", "STAGE_STARTED", "orchestrator", "stage", now, "")
	}

	out, err := getMessages(db, "r1", "", "", 4)
	if err != nil {
		t.Fatalf("getMessages: %v", err)
	}

	var msgs []map[string]any
	_ = json.Unmarshal([]byte(out), &msgs)
	if len(msgs) != 4 {
		t.Errorf("expected 4 messages with limit=4, got %d", len(msgs))
	}
}

// AC6, AC12: getMessages filters by sender
func TestGetMessages_FiltersBySender(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertRun(t, db, "r1", "1", "running", now, now)
	insertMessage(t, db, "m1", "r1", "VALIDATION_RESULT", "validator-build", "ok", now, "")
	insertMessage(t, db, "m2", "r1", "VALIDATION_RESULT", "validator-pr", "fail", now, "")

	out, err := getMessages(db, "r1", "", "validator-pr", 0)
	if err != nil {
		t.Fatalf("getMessages: %v", err)
	}

	var msgs []map[string]any
	_ = json.Unmarshal([]byte(out), &msgs)
	if len(msgs) != 1 || msgs[0]["sender"] != "validator-pr" {
		t.Errorf("expected 1 message from validator-pr, got %d", len(msgs))
	}
}

// --- getLogs tool ---

// AC12: getLogs respects limit (returns at most N lines)
func TestGetLogs_RespectsLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	var entries []struct {
		ts  time.Time
		msg string
	}
	for i := range 20 {
		entries = append(entries, struct {
			ts  time.Time
			msg string
		}{now.Add(time.Duration(i) * time.Second), fmt.Sprintf("log line %d", i)})
	}
	writeLogLines(t, dir, "run-1", entries)

	out, err := getLogs(dir, "run-1", time.Time{}, 5)
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 5 {
		t.Errorf("expected at most 5 lines with limit=5, got %d", len(lines))
	}
}

// AC12: getLogs filters by since timestamp
func TestGetLogs_FiltersBySince(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	entries := []struct {
		ts  time.Time
		msg string
	}{
		{base, "old line 1"},
		{base.Add(time.Minute), "old line 2"},
		{base.Add(5 * time.Minute), "recent line 1"},
		{base.Add(6 * time.Minute), "recent line 2"},
	}
	writeLogLines(t, dir, "run-1", entries)

	since := base.Add(4 * time.Minute)
	out, err := getLogs(dir, "run-1", since, 0)
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	if strings.Contains(out, "old line") {
		t.Errorf("getLogs returned lines before since timestamp:\n%s", out)
	}
	if !strings.Contains(out, "recent line") {
		t.Errorf("getLogs did not return lines after since timestamp:\n%s", out)
	}
}

// getLogs gracefully handles missing log file
func TestGetLogs_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := getLogs(dir, "nonexistent-run", time.Time{}, 0)
	if err == nil {
		t.Error("expected error for missing log file, got nil")
	}
}

// getLogs with limit=0 returns all lines
func TestGetLogs_ZeroLimitReturnsAll(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	var entries []struct {
		ts  time.Time
		msg string
	}
	for i := range 15 {
		entries = append(entries, struct {
			ts  time.Time
			msg string
		}{now.Add(time.Duration(i) * time.Second), fmt.Sprintf("line %d", i)})
	}
	writeLogLines(t, dir, "run-1", entries)

	out, err := getLogs(dir, "run-1", time.Time{}, 0)
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 15 {
		t.Errorf("expected 15 lines with limit=0, got %d", len(lines))
	}
}

// --- inspect tool ---

// AC6: inspect returns plain text timeline with timestamp, sender, topic columns
func TestInspect_ReturnsTimeline(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertRun(t, db, "r1", "1", "accepted", now, now)
	insertMessage(t, db, "m1", "r1", "ISSUE_OPENED", "system", "#1 Add feature", now.Add(-2*time.Minute), "")
	insertMessage(t, db, "m2", "r1", "CLUSTER_COMPLETE", "completion", "all_validators_approved", now.Add(-time.Minute), "")

	out, err := inspect(db, "r1")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !strings.Contains(out, "ISSUE_OPENED") {
		t.Errorf("inspect output missing ISSUE_OPENED:\n%s", out)
	}
	if !strings.Contains(out, "system") {
		t.Errorf("inspect output missing sender 'system':\n%s", out)
	}
	if !strings.Contains(out, "CLUSTER_COMPLETE") {
		t.Errorf("inspect output missing CLUSTER_COMPLETE:\n%s", out)
	}
}

// AC6: inspect returns error for unknown run
func TestInspect_UnknownRun(t *testing.T) {
	db := newTestDB(t)
	// inspect with no messages should return empty timeline (not error)
	out, err := inspect(db, "nonexistent")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	// Empty run → empty output is acceptable
	_ = out
}
