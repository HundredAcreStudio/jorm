package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ClaudeResult holds the output from a Claude CLI invocation.
type ClaudeResult struct {
	Text string
	Cost float64
}

// streamMessage represents a single JSON message from claude --output-format stream-json.
type streamMessage struct {
	Type    string  `json:"type"`
	Content string  `json:"content"`
	Role    string  `json:"role"`
	CostUSD float64 `json:"cost_usd"`
}

// RunClaude runs the Claude CLI headlessly and returns the final assistant text and cost.
func RunClaude(ctx context.Context, prompt, workDir, model string) (*ClaudeResult, error) {
	resolved := resolveModel(model)

	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--allowedTools", "Bash,Read,Write,Edit,MultiEdit,Glob,Grep",
		"--max-turns", "30",
		"--model", resolved,
		"-p", prompt,
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = workDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting claude: %w", err)
	}

	var text strings.Builder
	var totalCost float64

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var msg streamMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "assistant":
			if msg.Content != "" {
				text.WriteString(msg.Content)
			}
		case "result":
			if msg.Content != "" {
				text.Reset()
				text.WriteString(msg.Content)
			}
			totalCost = msg.CostUSD
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("claude exited with error: %w", err)
	}

	return &ClaudeResult{
		Text: text.String(),
		Cost: totalCost,
	}, nil
}

func resolveModel(alias string) string {
	switch strings.ToLower(alias) {
	case "sonnet":
		return "claude-sonnet-4-6"
	case "opus":
		return "claude-opus-4-6"
	case "haiku":
		return "claude-haiku-4-5-20251001"
	default:
		return alias
	}
}
