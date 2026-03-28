package bus

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		cluster_id TEXT,
		topic TEXT,
		sender TEXT,
		timestamp DATETIME,
		content TEXT,
		data TEXT
	)`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPublish_AssignsIDAndTimestamp(t *testing.T) {
	b := New(newTestDB(t))
	err := b.Publish(Message{
		ClusterID: "c1",
		Topic:     "TEST",
		Sender:    "test",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	msgs, err := b.Query("c1", QueryOpts{Topics: []string{"TEST"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ID == "" {
		t.Error("expected auto-assigned ID")
	}
	if msgs[0].Content != "hello" {
		t.Errorf("Content = %q, want %q", msgs[0].Content, "hello")
	}
}

func TestQuery_FiltersByTopic(t *testing.T) {
	b := New(newTestDB(t))
	_ = b.Publish(Message{ClusterID: "c1", Topic: "A", Sender: "s", Content: "a"})
	_ = b.Publish(Message{ClusterID: "c1", Topic: "B", Sender: "s", Content: "b"})

	msgs, err := b.Query("c1", QueryOpts{Topics: []string{"A"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Topic != "A" {
		t.Errorf("expected 1 message with topic A, got %d", len(msgs))
	}
}

func TestQuery_FiltersBySender(t *testing.T) {
	b := New(newTestDB(t))
	_ = b.Publish(Message{ClusterID: "c1", Topic: "T", Sender: "alice", Content: "a"})
	_ = b.Publish(Message{ClusterID: "c1", Topic: "T", Sender: "bob", Content: "b"})

	msgs, err := b.Query("c1", QueryOpts{Sender: "alice"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Sender != "alice" {
		t.Errorf("expected 1 message from alice, got %d", len(msgs))
	}
}

func TestQuery_Limit(t *testing.T) {
	b := New(newTestDB(t))
	_ = b.Publish(Message{ClusterID: "c1", Topic: "T", Sender: "s", Content: "1"})
	_ = b.Publish(Message{ClusterID: "c1", Topic: "T", Sender: "s", Content: "2"})
	_ = b.Publish(Message{ClusterID: "c1", Topic: "T", Sender: "s", Content: "3"})

	msgs, err := b.Query("c1", QueryOpts{Limit: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages with limit, got %d", len(msgs))
	}
}

func TestQuery_IsolatesByCluster(t *testing.T) {
	b := New(newTestDB(t))
	_ = b.Publish(Message{ClusterID: "c1", Topic: "T", Sender: "s", Content: "c1"})
	_ = b.Publish(Message{ClusterID: "c2", Topic: "T", Sender: "s", Content: "c2"})

	msgs, err := b.Query("c1", QueryOpts{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "c1" {
		t.Errorf("expected only c1's message")
	}
}

func TestFindLast_ReturnsMostRecent(t *testing.T) {
	b := New(newTestDB(t))
	_ = b.Publish(Message{ClusterID: "c1", Topic: "T", Sender: "s", Content: "first", Timestamp: time.Now().Add(-time.Second)})
	_ = b.Publish(Message{ClusterID: "c1", Topic: "T", Sender: "s", Content: "second", Timestamp: time.Now()})

	msg, err := b.FindLast("c1", "T")
	if err != nil {
		t.Fatalf("FindLast: %v", err)
	}
	if msg.Content != "second" {
		t.Errorf("expected most recent message, got %q", msg.Content)
	}
}

func TestFindLast_ReturnsErrorWhenEmpty(t *testing.T) {
	b := New(newTestDB(t))
	_, err := b.FindLast("c1", "NONEXISTENT")
	if err == nil {
		t.Error("expected error for empty result")
	}
}

func TestPublish_DataRoundTrips(t *testing.T) {
	b := New(newTestDB(t))
	_ = b.Publish(Message{
		ClusterID: "c1",
		Topic:     "T",
		Sender:    "s",
		Data:      map[string]any{"approved": true, "count": float64(42)},
	})

	msgs, _ := b.Query("c1", QueryOpts{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	approved, ok := msgs[0].Data["approved"].(bool)
	if !ok || !approved {
		t.Errorf("Data[approved] = %v, want true", msgs[0].Data["approved"])
	}
}

func TestSubscribe_ReceivesPublishedMessages(t *testing.T) {
	b := New(newTestDB(t))
	ch := b.Subscribe("T")

	_ = b.Publish(Message{ClusterID: "c1", Topic: "T", Sender: "s", Content: "hello"})

	select {
	case msg := <-ch:
		if msg.Content != "hello" {
			t.Errorf("Content = %q, want %q", msg.Content, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscribed message")
	}
}

func TestUnsubscribe_StopsDelivery(t *testing.T) {
	b := New(newTestDB(t))
	ch := b.Subscribe("T")
	b.Unsubscribe("T", ch)

	_ = b.Publish(Message{ClusterID: "c1", Topic: "T", Sender: "s", Content: "hello"})

	select {
	case <-ch:
		t.Fatal("should not receive after unsubscribe")
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}
