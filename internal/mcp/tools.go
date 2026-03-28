package mcp

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// listRuns returns a JSON object with all runs including elapsed time and cost.
func listRuns(db *sql.DB) (string, error) {
	runs, err := QueryRuns(db)
	if err != nil {
		return "", err
	}

	type runJSON struct {
		ID      string  `json:"id"`
		Issue   string  `json:"issue"`
		Status  string  `json:"status"`
		Elapsed string  `json:"elapsed"`
		Cost    float64 `json:"cost"`
	}

	var out []runJSON
	for _, r := range runs {
		elapsed := r.UpdatedAt.Sub(r.CreatedAt).Truncate(time.Second).String()
		var cost float64
		msg, err := QueryLastMessage(db, r.ID, "CLUSTER_COMPLETE")
		if err == nil {
			if c, ok := msg.Data["total_cost"].(float64); ok {
				cost = c
			}
		}
		out = append(out, runJSON{
			ID:      r.ID,
			Issue:   r.IssueID,
			Status:  r.Status,
			Elapsed: elapsed,
			Cost:    cost,
		})
	}

	data, err := json.Marshal(map[string]any{"runs": out})
	if err != nil {
		return "", fmt.Errorf("marshaling runs: %w", err)
	}
	return string(data), nil
}

// getStatus returns a JSON object with run status and recent agent activity.
func getStatus(db *sql.DB, runID string) (string, error) {
	run, err := QueryRun(db, runID)
	if err != nil {
		return "", err
	}

	// Query recent stage/validation messages to infer agent states
	msgs, err := QueryMessages(db, runID, "", "", 0)
	if err != nil {
		return "", err
	}

	type agentState struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}

	var agents []agentState
	var round int
	for _, m := range msgs {
		switch m.Topic {
		case "STAGE_STARTED":
			round++
		case "VALIDATION_RESULT":
			agents = append(agents, agentState{ID: m.Sender, State: "completed"})
		}
	}

	var cost float64
	msg, err := QueryLastMessage(db, runID, "CLUSTER_COMPLETE")
	if err == nil {
		if c, ok := msg.Data["total_cost"].(float64); ok {
			cost = c
		}
	}

	result := map[string]any{
		"run_id": run.ID,
		"status": run.Status,
		"round":  round,
		"agents": agents,
		"cost":   cost,
	}

	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshaling status: %w", err)
	}
	return string(data), nil
}

// getLogs reads log lines from the run's log file, optionally filtering by since and limit.
func getLogs(logDir, runID string, since time.Time, limit int) (string, error) {
	// Reject run IDs containing path separators or traversal sequences to prevent
	// reading arbitrary files outside the log directory.
	if strings.ContainsAny(runID, "/\\") || strings.Contains(runID, "..") {
		return "", fmt.Errorf("invalid run_id")
	}

	path := filepath.Join(logDir, runID+".log")

	// Belt-and-suspenders: verify resolved path is within logDir.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid run_id")
	}
	absLogDir, err := filepath.Abs(logDir)
	if err != nil {
		return "", fmt.Errorf("invalid log directory")
	}
	if !strings.HasPrefix(absPath, absLogDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid run_id")
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening log file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if !since.IsZero() {
			// Try to parse the time from JSON log line
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err == nil {
				if ts, ok := entry["time"].(string); ok {
					if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
						if t.Before(since) {
							continue
						}
					}
				}
			}
		}

		lines = append(lines, line)

		if limit > 0 && len(lines) >= limit {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading log file: %w", err)
	}
	return strings.Join(lines, "\n"), nil
}

// getMessages returns messages for a run as a JSON array.
func getMessages(db *sql.DB, runID, topic, sender string, limit int) (string, error) {
	msgs, err := QueryMessages(db, runID, topic, sender, limit)
	if err != nil {
		return "", err
	}

	type msgJSON struct {
		ID        string         `json:"id"`
		Topic     string         `json:"topic"`
		Sender    string         `json:"sender"`
		Timestamp string         `json:"timestamp"`
		Content   string         `json:"content"`
		Data      map[string]any `json:"data,omitempty"`
	}

	var out []msgJSON
	for _, m := range msgs {
		out = append(out, msgJSON{
			ID:        m.ID,
			Topic:     m.Topic,
			Sender:    m.Sender,
			Timestamp: m.Timestamp.Format(time.RFC3339),
			Content:   m.Content,
			Data:      m.Data,
		})
	}
	if out == nil {
		out = []msgJSON{}
	}

	data, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("marshaling messages: %w", err)
	}
	return string(data), nil
}

// inspect returns a plain text timeline of all messages for a run.
func inspect(db *sql.DB, runID string) (string, error) {
	msgs, err := QueryMessages(db, runID, "", "", 0)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for _, m := range msgs {
		content := m.Content
		if len(content) > 60 {
			content = content[:57] + "..."
		}
		// Replace newlines in content excerpt
		content = strings.ReplaceAll(content, "\n", " ")
		fmt.Fprintf(&b, "%s  %-16s  %-26s  %s\n",
			m.Timestamp.Format("15:04:05"),
			m.Sender,
			m.Topic,
			content,
		)
	}
	return b.String(), nil
}
