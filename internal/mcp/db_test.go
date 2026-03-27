package mcp

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// newTestDB creates an in-memory SQLite database with the runs and messages
// schema, mirroring what store.Store.migrate() does in production.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL,
			branch TEXT NOT NULL,
			worktree_dir TEXT NOT NULL DEFAULT '',
			attempt INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'running',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			findings TEXT NOT NULL DEFAULT '',
			in_place INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			cluster_id TEXT NOT NULL,
			topic TEXT NOT NULL,
			sender TEXT NOT NULL,
			timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			content TEXT NOT NULL DEFAULT '',
			data TEXT NOT NULL DEFAULT '{}'
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	return db
}

func insertRun(t *testing.T, db *sql.DB, id, issueID, status string, createdAt, updatedAt time.Time) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO runs (id, issue_id, branch, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, issueID, "jorm/issue-"+issueID, status,
		createdAt.UTC().Format(time.RFC3339),
		updatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insertRun: %v", err)
	}
}

func insertMessage(t *testing.T, db *sql.DB, id, clusterID, topic, sender, content string, ts time.Time, data string) {
	t.Helper()
	if data == "" {
		data = "{}"
	}
	_, err := db.Exec(
		`INSERT INTO messages (id, cluster_id, topic, sender, content, timestamp, data) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, clusterID, topic, sender, content, ts.UTC().Format(time.RFC3339), data,
	)
	if err != nil {
		t.Fatalf("insertMessage: %v", err)
	}
}

// AC11: QueryRuns returns all runs
func TestQueryRuns_ReturnsAllRuns(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertRun(t, db, "1-1", "1", "accepted", now, now)
	insertRun(t, db, "2-1", "2", "running", now, now)

	rows, err := QueryRuns(db)
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 runs, got %d", len(rows))
	}
}

// AC11: QueryRuns on empty DB returns empty slice, not error
func TestQueryRuns_EmptyDB(t *testing.T) {
	db := newTestDB(t)
	rows, err := QueryRuns(db)
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 runs, got %d", len(rows))
	}
}

// AC11: QueryRuns populates RunRow fields correctly
func TestQueryRuns_FieldsPopulated(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().Truncate(time.Second)
	insertRun(t, db, "42-1", "42", "accepted", now, now)

	rows, err := QueryRuns(db)
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 run, got %d", len(rows))
	}
	r := rows[0]
	if r.ID != "42-1" {
		t.Errorf("ID = %q, want %q", r.ID, "42-1")
	}
	if r.IssueID != "42" {
		t.Errorf("IssueID = %q, want %q", r.IssueID, "42")
	}
	if r.Status != "accepted" {
		t.Errorf("Status = %q, want %q", r.Status, "accepted")
	}
}

// AC11: QueryRun finds a run by ID
func TestQueryRun_FindsByID(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertRun(t, db, "99-1", "99", "running", now, now)
	insertRun(t, db, "99-2", "99", "accepted", now, now)

	row, err := QueryRun(db, "99-1")
	if err != nil {
		t.Fatalf("QueryRun: %v", err)
	}
	if row.ID != "99-1" {
		t.Errorf("ID = %q, want %q", row.ID, "99-1")
	}
	if row.Status != "running" {
		t.Errorf("Status = %q, want %q", row.Status, "running")
	}
}

// AC11: QueryRun returns error for missing run
func TestQueryRun_NotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := QueryRun(db, "nonexistent")
	if err == nil {
		t.Error("expected error for missing run, got nil")
	}
}

// AC11: QueryMessages returns messages for a run
func TestQueryMessages_ReturnsRunMessages(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertMessage(t, db, "m1", "run-1", "ISSUE_OPENED", "system", "issue body", now, "")
	insertMessage(t, db, "m2", "run-1", "PLAN_READY", "planner", "plan text", now, "")
	insertMessage(t, db, "m3", "run-2", "ISSUE_OPENED", "system", "other run", now, "")

	msgs, err := QueryMessages(db, "run-1", "", "", 0)
	if err != nil {
		t.Fatalf("QueryMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages for run-1, got %d", len(msgs))
	}
}

// AC11: QueryMessages filters by topic
func TestQueryMessages_FiltersByTopic(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertMessage(t, db, "m1", "run-1", "PLAN_READY", "planner", "plan", now, "")
	insertMessage(t, db, "m2", "run-1", "VALIDATION_RESULT", "validator", "result", now, "")

	msgs, err := QueryMessages(db, "run-1", "PLAN_READY", "", 0)
	if err != nil {
		t.Fatalf("QueryMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Topic != "PLAN_READY" {
		t.Errorf("expected 1 PLAN_READY message, got %d", len(msgs))
	}
}

// AC11: QueryMessages filters by sender
func TestQueryMessages_FiltersBySender(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertMessage(t, db, "m1", "run-1", "VALIDATION_RESULT", "validator-build", "ok", now, "")
	insertMessage(t, db, "m2", "run-1", "VALIDATION_RESULT", "validator-pr", "fail", now, "")

	msgs, err := QueryMessages(db, "run-1", "", "validator-build", 0)
	if err != nil {
		t.Fatalf("QueryMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Sender != "validator-build" {
		t.Errorf("expected 1 message from validator-build, got %d", len(msgs))
	}
}

// AC11, AC12: QueryMessages respects limit
func TestQueryMessages_RespectsLimit(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	for i := range 5 {
		insertMessage(t, db, fmt.Sprintf("m%d", i), "run-1", "STAGE_STARTED", "orchestrator", "stage", now, "")
	}

	msgs, err := QueryMessages(db, "run-1", "", "", 3)
	if err != nil {
		t.Fatalf("QueryMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages with limit=3, got %d", len(msgs))
	}
}

// AC11: QueryMessages returns all when limit is 0
func TestQueryMessages_ZeroLimitReturnsAll(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertMessage(t, db, "m1", "run-1", "T", "s", "a", now, "")
	insertMessage(t, db, "m2", "run-1", "T", "s", "b", now, "")
	insertMessage(t, db, "m3", "run-1", "T", "s", "c", now, "")

	msgs, err := QueryMessages(db, "run-1", "", "", 0)
	if err != nil {
		t.Fatalf("QueryMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages with limit=0, got %d", len(msgs))
	}
}

// AC11: QueryLastMessage returns most recent message for topic
func TestQueryLastMessage_ReturnsMostRecent(t *testing.T) {
	db := newTestDB(t)
	t1 := time.Now().Add(-2 * time.Second)
	t2 := time.Now().Add(-time.Second)
	t3 := time.Now()
	insertMessage(t, db, "m1", "run-1", "VALIDATION_RESULT", "v", "first", t1, "")
	insertMessage(t, db, "m2", "run-1", "VALIDATION_RESULT", "v", "second", t2, "")
	insertMessage(t, db, "m3", "run-1", "VALIDATION_RESULT", "v", "third", t3, "")

	msg, err := QueryLastMessage(db, "run-1", "VALIDATION_RESULT")
	if err != nil {
		t.Fatalf("QueryLastMessage: %v", err)
	}
	if msg.Content != "third" {
		t.Errorf("Content = %q, want %q", msg.Content, "third")
	}
}

// AC11: QueryLastMessage returns error when no matching message
func TestQueryLastMessage_NotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := QueryLastMessage(db, "run-1", "CLUSTER_COMPLETE")
	if err == nil {
		t.Error("expected error for missing message, got nil")
	}
}

// AC11: QueryLastMessage extracts cost from CLUSTER_COMPLETE data JSON
func TestQueryLastMessage_DataJSONRoundTrips(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	insertMessage(t, db, "m1", "run-1", "CLUSTER_COMPLETE", "completion", "done", now,
		`{"total_cost": 1.23, "all_validators_approved": true}`)

	msg, err := QueryLastMessage(db, "run-1", "CLUSTER_COMPLETE")
	if err != nil {
		t.Fatalf("QueryLastMessage: %v", err)
	}
	cost, ok := msg.Data["total_cost"].(float64)
	if !ok || cost != 1.23 {
		t.Errorf("Data[total_cost] = %v, want 1.23", msg.Data["total_cost"])
	}
}

// AC13: openReadOnlyDB uses read-only connection string
func TestOpenReadOnlyDB_DSNContainsReadOnly(t *testing.T) {
	// Verify that the package-level dsn or openReadOnlyDB function uses a read-only flag.
	// We test this indirectly: opening a read-only connection to :memory: should succeed
	// but any write should fail.
	//
	// Since :memory: with mode=ro is not practical (no data to read), we verify
	// the function exists and the DSN it builds contains a read-only marker.
	dsn := readOnlyDSN("/tmp/test.db")
	hasRO := strings.Contains(dsn, "mode=ro") ||
		strings.Contains(dsn, "_query_only=true") ||
		strings.Contains(dsn, "immutable=1")
	if !hasRO {
		t.Errorf("readOnlyDSN(%q) = %q, want DSN containing mode=ro, _query_only=true, or immutable=1", "/tmp/test.db", dsn)
	}
}
