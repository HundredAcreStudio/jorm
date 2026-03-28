package bus

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Standard topic constants for the agent workflow.
const (
	TopicIssueOpened         = "ISSUE_OPENED"
	TopicPlanReady           = "PLAN_READY"
	TopicImplementationReady = "IMPLEMENTATION_READY"
	TopicValidationResult    = "VALIDATION_RESULT"
	TopicClusterComplete     = "CLUSTER_COMPLETE"
	TopicStageStarted        = "STAGE_STARTED"
	TopicStageCompleted      = "STAGE_COMPLETED"
	TopicTestsReady          = "TESTS_READY"
)

// Message represents a single event on the bus.
type Message struct {
	ID        string
	ClusterID string
	Topic     string
	Sender    string
	Timestamp time.Time
	Content   string         // free-form text
	Data      map[string]any // structured data
}

// QueryOpts filters messages when querying the bus.
type QueryOpts struct {
	Topics []string
	Sender string
	Since  time.Time
	Limit  int
}

// Bus provides SQLite-backed pub/sub for agent communication.
type Bus struct {
	db          *sql.DB
	mu          sync.Mutex
	subscribers map[string][]chan Message
}

// New creates a new Bus using the provided database connection.
func New(db *sql.DB) *Bus {
	return &Bus{
		db:          db,
		subscribers: make(map[string][]chan Message),
	}
}

// Publish persists a message to SQLite and fans out to in-memory subscribers.
func (b *Bus) Publish(msg Message) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	if msg.Data == nil {
		msg.Data = make(map[string]any)
	}

	dataJSON, err := json.Marshal(msg.Data)
	if err != nil {
		return fmt.Errorf("marshaling message data: %w", err)
	}

	_, err = b.db.Exec(`
		INSERT INTO messages (id, cluster_id, topic, sender, timestamp, content, data)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, msg.ID, msg.ClusterID, msg.Topic, msg.Sender, msg.Timestamp, msg.Content, string(dataJSON))
	if err != nil {
		return fmt.Errorf("persisting message: %w", err)
	}

	// Fan out to in-memory subscribers
	b.mu.Lock()
	subs := b.subscribers[msg.Topic]
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- msg:
		default:
			// Drop if subscriber is slow — don't block the publisher
		}
	}

	return nil
}

// Subscribe returns a channel that receives messages for the given topic.
// The channel is buffered (16) to absorb bursts.
func (b *Bus) Subscribe(topic string) <-chan Message {
	ch := make(chan Message, 16)
	b.mu.Lock()
	b.subscribers[topic] = append(b.subscribers[topic], ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel from a topic's subscriber list.
func (b *Bus) Unsubscribe(topic string, ch <-chan Message) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[topic]
	for i, s := range subs {
		if s == ch {
			b.subscribers[topic] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

// Query retrieves messages matching the given options, ordered by timestamp.
func (b *Bus) Query(clusterID string, opts QueryOpts) ([]Message, error) {
	query := `SELECT id, cluster_id, topic, sender, timestamp, content, data FROM messages WHERE cluster_id = ?`
	args := []any{clusterID}

	if len(opts.Topics) > 0 {
		placeholders := ""
		for i, t := range opts.Topics {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, t)
		}
		query += fmt.Sprintf(" AND topic IN (%s)", placeholders)
	}

	if opts.Sender != "" {
		query += " AND sender = ?"
		args = append(args, opts.Sender)
	}

	if !opts.Since.IsZero() {
		query += " AND timestamp > ?"
		args = append(args, opts.Since)
	}

	query += " ORDER BY timestamp ASC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := b.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanMessages(rows)
}

// FindLast returns the most recent message for a topic in a cluster.
func (b *Bus) FindLast(clusterID, topic string) (*Message, error) {
	row := b.db.QueryRow(`
		SELECT id, cluster_id, topic, sender, timestamp, content, data
		FROM messages
		WHERE cluster_id = ? AND topic = ?
		ORDER BY timestamp DESC LIMIT 1
	`, clusterID, topic)

	msg, err := scanMessage(row)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

func scanMessages(rows *sql.Rows) ([]Message, error) {
	var msgs []Message
	for rows.Next() {
		var msg Message
		var dataJSON string
		if err := rows.Scan(&msg.ID, &msg.ClusterID, &msg.Topic, &msg.Sender, &msg.Timestamp, &msg.Content, &dataJSON); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		msg.Data = make(map[string]any)
		_ = json.Unmarshal([]byte(dataJSON), &msg.Data)
		msgs = append(msgs, msg)
	}
	return msgs, rows.Err()
}

func scanMessage(row *sql.Row) (*Message, error) {
	var msg Message
	var dataJSON string
	if err := row.Scan(&msg.ID, &msg.ClusterID, &msg.Topic, &msg.Sender, &msg.Timestamp, &msg.Content, &dataJSON); err != nil {
		return nil, fmt.Errorf("scanning message: %w", err)
	}
	msg.Data = make(map[string]any)
	_ = json.Unmarshal([]byte(dataJSON), &msg.Data)
	return &msg, nil
}
