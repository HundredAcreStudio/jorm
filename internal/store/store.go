package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// RunState represents the persisted state of a jorm run.
type RunState struct {
	ID          string
	IssueID     string
	Branch      string
	WorktreeDir string
	Attempt     int
	Status      string // "running", "accepted", "rejected", "failed"
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Findings    string
}

// Store manages persistent run state in SQLite.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database at ~/.jorm/jorm.db.
func New() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home dir: %w", err)
	}

	dir := filepath.Join(home, ".jorm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating store dir: %w", err)
	}

	dbPath := filepath.Join(dir, "jorm.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL,
			branch TEXT NOT NULL,
			worktree_dir TEXT NOT NULL,
			attempt INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'running',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			findings TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		return fmt.Errorf("migrating database: %w", err)
	}
	return nil
}

// Save upserts a run state.
func (s *Store) Save(r *RunState) error {
	r.UpdatedAt = time.Now()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = r.UpdatedAt
	}

	_, err := s.db.Exec(`
		INSERT INTO runs (id, issue_id, branch, worktree_dir, attempt, status, created_at, updated_at, findings)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			attempt = excluded.attempt,
			status = excluded.status,
			updated_at = excluded.updated_at,
			findings = excluded.findings
	`, r.ID, r.IssueID, r.Branch, r.WorktreeDir, r.Attempt, r.Status, r.CreatedAt, r.UpdatedAt, r.Findings)
	if err != nil {
		return fmt.Errorf("saving run: %w", err)
	}
	return nil
}

// Load retrieves a run by ID.
func (s *Store) Load(id string) (*RunState, error) {
	r := &RunState{}
	err := s.db.QueryRow(`SELECT id, issue_id, branch, worktree_dir, attempt, status, created_at, updated_at, findings FROM runs WHERE id = ?`, id).
		Scan(&r.ID, &r.IssueID, &r.Branch, &r.WorktreeDir, &r.Attempt, &r.Status, &r.CreatedAt, &r.UpdatedAt, &r.Findings)
	if err != nil {
		return nil, fmt.Errorf("loading run %s: %w", id, err)
	}
	return r, nil
}

// LoadByIssue retrieves the most recent run for an issue.
func (s *Store) LoadByIssue(issueID string) (*RunState, error) {
	r := &RunState{}
	err := s.db.QueryRow(`SELECT id, issue_id, branch, worktree_dir, attempt, status, created_at, updated_at, findings FROM runs WHERE issue_id = ? ORDER BY updated_at DESC LIMIT 1`, issueID).
		Scan(&r.ID, &r.IssueID, &r.Branch, &r.WorktreeDir, &r.Attempt, &r.Status, &r.CreatedAt, &r.UpdatedAt, &r.Findings)
	if err != nil {
		return nil, fmt.Errorf("loading run for issue %s: %w", issueID, err)
	}
	return r, nil
}

// List returns all runs ordered by most recent first.
func (s *Store) List() ([]*RunState, error) {
	rows, err := s.db.Query(`SELECT id, issue_id, branch, worktree_dir, attempt, status, created_at, updated_at, findings FROM runs ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	defer rows.Close()

	var runs []*RunState
	for rows.Next() {
		r := &RunState{}
		if err := rows.Scan(&r.ID, &r.IssueID, &r.Branch, &r.WorktreeDir, &r.Attempt, &r.Status, &r.CreatedAt, &r.UpdatedAt, &r.Findings); err != nil {
			return nil, fmt.Errorf("scanning run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
