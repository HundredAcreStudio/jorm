package mcp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// RunRow represents a row from the runs table.
type RunRow struct {
	ID        string
	IssueID   string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MessageRow represents a row from the messages table.
type MessageRow struct {
	ID        string
	ClusterID string
	Topic     string
	Sender    string
	Timestamp time.Time
	Content   string
	Data      map[string]any
}

// readOnlyDSN builds a DSN that opens the database in read-only mode.
func readOnlyDSN(dbPath string) string {
	return fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL", dbPath)
}

// openReadOnlyDB opens a read-only connection to the SQLite database.
func openReadOnlyDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", readOnlyDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	return db, nil
}

// QueryRuns returns all runs ordered by most recent first.
func QueryRuns(db *sql.DB) ([]RunRow, error) {
	rows, err := db.Query(`SELECT id, issue_id, status, created_at, updated_at FROM runs ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("querying runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []RunRow
	for rows.Next() {
		var r RunRow
		if err := rows.Scan(&r.ID, &r.IssueID, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning run: %w", err)
		}
		result = append(result, r)
	}
	if result == nil {
		result = []RunRow{}
	}
	return result, rows.Err()
}

// QueryRun returns a single run by ID.
func QueryRun(db *sql.DB, runID string) (*RunRow, error) {
	var r RunRow
	err := db.QueryRow(`SELECT id, issue_id, status, created_at, updated_at FROM runs WHERE id = ?`, runID).
		Scan(&r.ID, &r.IssueID, &r.Status, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("querying run %s: %w", runID, err)
	}
	return &r, nil
}

// QueryMessages returns messages for a run, optionally filtered by topic, sender, and limit.
func QueryMessages(db *sql.DB, runID, topic, sender string, limit int) ([]MessageRow, error) {
	query := `SELECT id, cluster_id, topic, sender, timestamp, content, data FROM messages WHERE cluster_id = ?`
	args := []any{runID}

	if topic != "" {
		query += " AND topic = ?"
		args = append(args, topic)
	}
	if sender != "" {
		query += " AND sender = ?"
		args = append(args, sender)
	}

	query += " ORDER BY timestamp ASC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var msgs []MessageRow
	for rows.Next() {
		var m MessageRow
		var dataJSON string
		if err := rows.Scan(&m.ID, &m.ClusterID, &m.Topic, &m.Sender, &m.Timestamp, &m.Content, &dataJSON); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		m.Data = make(map[string]any)
		_ = json.Unmarshal([]byte(dataJSON), &m.Data)
		msgs = append(msgs, m)
	}
	if msgs == nil {
		msgs = []MessageRow{}
	}
	return msgs, rows.Err()
}

// QueryLastMessage returns the most recent message for a given run and topic.
func QueryLastMessage(db *sql.DB, runID, topic string) (*MessageRow, error) {
	var m MessageRow
	var dataJSON string
	err := db.QueryRow(
		`SELECT id, cluster_id, topic, sender, timestamp, content, data FROM messages WHERE cluster_id = ? AND topic = ? ORDER BY timestamp DESC LIMIT 1`,
		runID, topic,
	).Scan(&m.ID, &m.ClusterID, &m.Topic, &m.Sender, &m.Timestamp, &m.Content, &dataJSON)
	if err != nil {
		return nil, fmt.Errorf("querying last message for %s/%s: %w", runID, topic, err)
	}
	m.Data = make(map[string]any)
	_ = json.Unmarshal([]byte(dataJSON), &m.Data)
	return &m, nil
}
