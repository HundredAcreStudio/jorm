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
	InPlace     bool
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

	// Enable WAL mode for concurrent reads from multiple agent goroutines
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
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
			findings TEXT NOT NULL DEFAULT '',
			in_place INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		return fmt.Errorf("migrating runs table: %w", err)
	}

	_, err = s.db.Exec(`
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
		return fmt.Errorf("migrating messages table: %w", err)
	}

	// Index for efficient queries by cluster + topic
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_cluster_topic ON messages(cluster_id, topic, timestamp)`)

	// Add in_place column for existing databases
	s.db.Exec(`ALTER TABLE runs ADD COLUMN in_place INTEGER NOT NULL DEFAULT 0`)

	return nil
}

// DB exposes the underlying database connection for the message bus.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Save upserts a run state.
func (s *Store) Save(r *RunState) error {
	r.UpdatedAt = time.Now()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = r.UpdatedAt
	}

	inPlace := 0
	if r.InPlace {
		inPlace = 1
	}

	_, err := s.db.Exec(`
		INSERT INTO runs (id, issue_id, branch, worktree_dir, attempt, status, created_at, updated_at, findings, in_place)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			attempt = excluded.attempt,
			status = excluded.status,
			updated_at = excluded.updated_at,
			findings = excluded.findings
	`, r.ID, r.IssueID, r.Branch, r.WorktreeDir, r.Attempt, r.Status, r.CreatedAt, r.UpdatedAt, r.Findings, inPlace)
	if err != nil {
		return fmt.Errorf("saving run: %w", err)
	}
	return nil
}

// Load retrieves a run by ID.
func (s *Store) Load(id string) (*RunState, error) {
	r := &RunState{}
	var inPlace int
	err := s.db.QueryRow(`SELECT id, issue_id, branch, worktree_dir, attempt, status, created_at, updated_at, findings, in_place FROM runs WHERE id = ?`, id).
		Scan(&r.ID, &r.IssueID, &r.Branch, &r.WorktreeDir, &r.Attempt, &r.Status, &r.CreatedAt, &r.UpdatedAt, &r.Findings, &inPlace)
	if err != nil {
		return nil, fmt.Errorf("loading run %s: %w", id, err)
	}
	r.InPlace = inPlace != 0
	return r, nil
}

// LoadByIssue retrieves the most recent run for an issue.
func (s *Store) LoadByIssue(issueID string) (*RunState, error) {
	r := &RunState{}
	var inPlace int
	err := s.db.QueryRow(`SELECT id, issue_id, branch, worktree_dir, attempt, status, created_at, updated_at, findings, in_place FROM runs WHERE issue_id = ? ORDER BY updated_at DESC LIMIT 1`, issueID).
		Scan(&r.ID, &r.IssueID, &r.Branch, &r.WorktreeDir, &r.Attempt, &r.Status, &r.CreatedAt, &r.UpdatedAt, &r.Findings, &inPlace)
	if err != nil {
		return nil, fmt.Errorf("loading run for issue %s: %w", issueID, err)
	}
	r.InPlace = inPlace != 0
	return r, nil
}

// List returns all runs ordered by most recent first.
func (s *Store) List() ([]*RunState, error) {
	rows, err := s.db.Query(`SELECT id, issue_id, branch, worktree_dir, attempt, status, created_at, updated_at, findings, in_place FROM runs ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	defer rows.Close()

	var runs []*RunState
	for rows.Next() {
		r := &RunState{}
		var inPlace int
		if err := rows.Scan(&r.ID, &r.IssueID, &r.Branch, &r.WorktreeDir, &r.Attempt, &r.Status, &r.CreatedAt, &r.UpdatedAt, &r.Findings, &inPlace); err != nil {
			return nil, fmt.Errorf("scanning run: %w", err)
		}
		r.InPlace = inPlace != 0
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// Delete removes a run and its associated messages by ID within a single transaction.
func (s *Store) Delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction for delete: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM runs WHERE id = ?`, id); err != nil {
		tx.Rollback()
		return fmt.Errorf("deleting run %s: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE cluster_id = ?`, id); err != nil {
		tx.Rollback()
		return fmt.Errorf("deleting messages for run %s: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing delete for run %s: %w", id, err)
	}
	return nil
}

// CountRunsForIssue returns the number of existing runs for a given issue ID.
func (s *Store) CountRunsForIssue(issueID string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE issue_id = ?`, issueID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting runs for issue %s: %w", issueID, err)
	}
	return count, nil
}

// QueryMessages retrieves messages for a cluster, optionally filtered by topic.
func (s *Store) QueryMessages(clusterID string, topic string) ([]Message, error) {
	var rows *sql.Rows
	var err error

	if topic != "" {
		rows, err = s.db.Query(`SELECT id, cluster_id, topic, sender, timestamp, content, data FROM messages WHERE cluster_id = ? AND topic = ? ORDER BY timestamp ASC`, clusterID, topic)
	} else {
		rows, err = s.db.Query(`SELECT id, cluster_id, topic, sender, timestamp, content, data FROM messages WHERE cluster_id = ? ORDER BY timestamp ASC`, clusterID)
	}
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var msg Message
		var dataJSON string
		if err := rows.Scan(&msg.ID, &msg.ClusterID, &msg.Topic, &msg.Sender, &msg.Timestamp, &msg.Content, &dataJSON); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		msgs = append(msgs, msg)
	}
	return msgs, rows.Err()
}

// Message represents a stored bus message.
type Message struct {
	ID        string
	ClusterID string
	Topic     string
	Sender    string
	Timestamp time.Time
	Content   string
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
