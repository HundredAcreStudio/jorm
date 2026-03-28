package store

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSave_And_Load(t *testing.T) {
	s := newTestStore(t)
	r := &RunState{
		ID:          "run-1",
		IssueID:     "42",
		Branch:      "jorm/issue-42",
		WorktreeDir: "/tmp/jorm-42",
		Status:      "running",
	}

	if err := s.Save(r); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := s.Load("run-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.IssueID != "42" {
		t.Errorf("IssueID = %q, want %q", loaded.IssueID, "42")
	}
	if loaded.Status != "running" {
		t.Errorf("Status = %q, want %q", loaded.Status, "running")
	}
}

func TestSave_Upsert(t *testing.T) {
	s := newTestStore(t)
	r := &RunState{
		ID:      "run-1",
		IssueID: "42",
		Branch:  "b",
		Status:  "running",
	}
	_ = s.Save(r)

	r.Status = "accepted"
	_ = s.Save(r)

	loaded, _ := s.Load("run-1")
	if loaded.Status != "accepted" {
		t.Errorf("Status = %q after upsert, want %q", loaded.Status, "accepted")
	}
}

func TestLoadByIssue_ReturnsMostRecent(t *testing.T) {
	s := newTestStore(t)
	_ = s.Save(&RunState{ID: "42-1", IssueID: "42", Branch: "b", Status: "rejected"})
	_ = s.Save(&RunState{ID: "42-2", IssueID: "42", Branch: "b", Status: "running"})

	loaded, err := s.LoadByIssue("42")
	if err != nil {
		t.Fatalf("LoadByIssue: %v", err)
	}
	if loaded.ID != "42-2" {
		t.Errorf("ID = %q, want most recent %q", loaded.ID, "42-2")
	}
}

func TestLoad_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Load("nonexistent")
	if err == nil {
		t.Error("expected error for missing run")
	}
}

func TestDelete_RemovesRunAndMessages(t *testing.T) {
	s := newTestStore(t)
	_ = s.Save(&RunState{ID: "run-1", IssueID: "42", Branch: "b", Status: "running"})

	// Insert a message for this run
	_, _ = s.db.Exec(`INSERT INTO messages (id, cluster_id, topic, sender, content, data) VALUES ('m1', 'run-1', 'TEST', 'test', 'hello', '{}')`)

	if err := s.Delete("run-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := s.Load("run-1")
	if err == nil {
		t.Error("expected error after delete")
	}

	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE cluster_id = 'run-1'`).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 messages after delete, got %d", count)
	}
}

func TestList_ReturnsAllRuns(t *testing.T) {
	s := newTestStore(t)
	_ = s.Save(&RunState{ID: "1-1", IssueID: "1", Branch: "b", Status: "accepted"})
	_ = s.Save(&RunState{ID: "2-1", IssueID: "2", Branch: "b", Status: "running"})

	runs, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
}

func TestCountRunsForIssue(t *testing.T) {
	s := newTestStore(t)
	_ = s.Save(&RunState{ID: "42-1", IssueID: "42", Branch: "b", Status: "rejected"})
	_ = s.Save(&RunState{ID: "42-2", IssueID: "42", Branch: "b", Status: "running"})
	_ = s.Save(&RunState{ID: "99-1", IssueID: "99", Branch: "b", Status: "running"})

	count, err := s.CountRunsForIssue("42")
	if err != nil {
		t.Fatalf("CountRunsForIssue: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestSave_InPlace(t *testing.T) {
	s := newTestStore(t)
	_ = s.Save(&RunState{ID: "run-1", IssueID: "42", Branch: "main", Status: "running", InPlace: true})

	loaded, _ := s.Load("run-1")
	if !loaded.InPlace {
		t.Error("expected InPlace=true after round-trip")
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	s := newTestStore(t)
	// Second migration should not error
	if err := s.migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
