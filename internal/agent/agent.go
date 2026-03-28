package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	jormerrors "github.com/jorm/internal/errors"
)

// ClaudeResult holds the output from a Claude CLI invocation.
type ClaudeResult struct {
	Text string
	Cost float64
}

// RunOptions configures a RunClaude invocation.
type RunOptions struct {
	Prompt    string
	WorkDir   string
	Model     string
	Env       []string          // environment for subprocess; nil uses os.Environ()
	OnOutput  func(text string) // called for each meaningful output line; nil-safe
	OnStarted func(pid int)     // called after process starts with its PID; nil-safe
}

// streamLine is the top-level JSON object from claude --output-format stream-json.
type streamLine struct {
	Type    string          `json:"type"`
	Message *streamMessage  `json:"message"`
	Result  json.RawMessage `json:"result"`
	CostUSD float64        `json:"cost_usd"`
}

// streamMessage is the nested message object within assistant lines.
type streamMessage struct {
	Role       string         `json:"role"`
	Content    []contentBlock `json:"content"`
	StopReason *string        `json:"stop_reason"`
}

// contentBlock represents one block in the content array.
type contentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
}

// RunClaude runs the Claude CLI headlessly and returns the final assistant text and cost.
func RunClaude(ctx context.Context, opts RunOptions) (*ClaudeResult, error) {
	resolved := resolveModel(opts.Model)

	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--allowedTools", "Bash,Read,Write,Edit,MultiEdit,Glob,Grep",
		"--max-turns", "30",
		"--model", resolved,
		"-p", "-",
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = opts.WorkDir
	if opts.Env != nil {
		cmd.Env = opts.Env
	}

	// Pass prompt via stdin to avoid ARG_MAX limits on macOS (~1MB).
	// Review prompts can exceed this when they include full file contents.
	cmd.Stdin = strings.NewReader(opts.Prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting claude: %w", err)
	}

	if opts.OnStarted != nil {
		opts.OnStarted(cmd.Process.Pid)
	}

	var text strings.Builder
	var totalCost float64

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		var sl streamLine
		if err := json.Unmarshal(line, &sl); err != nil {
			if opts.OnOutput != nil {
				opts.OnOutput(string(line))
			}
			continue
		}

		// Stream output to callback
		if opts.OnOutput != nil {
			for _, display := range formatStreamLine(sl) {
				opts.OnOutput(display)
			}
		}

		// Accumulate final result text
		switch sl.Type {
		case "assistant":
			if sl.Message != nil {
				for _, block := range sl.Message.Content {
					if block.Type == "text" {
						text.WriteString(block.Text)
					}
				}
			}
		case "result":
			// Result may contain the final text
			var resultObj struct {
				Result string  `json:"result"`
				CostUSD float64 `json:"cost_usd"`
			}
			if json.Unmarshal(line, &resultObj) == nil {
				if resultObj.Result != "" {
					text.Reset()
					text.WriteString(resultObj.Result)
				}
				if resultObj.CostUSD > 0 {
					totalCost = resultObj.CostUSD
				}
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		raw := fmt.Errorf("claude exited with error: %w\nstderr: %s", err, stderr.String())
		return nil, jormerrors.ClassifyError(raw)
	}

	return &ClaudeResult{
		Text: text.String(),
		Cost: totalCost,
	}, nil
}

// formatStreamLine produces human-readable lines from a stream-json line.
func formatStreamLine(sl streamLine) []string {
	switch sl.Type {
	case "system":
		return []string{"[system] initializing..."}

	case "assistant":
		if sl.Message == nil {
			return nil
		}
		var lines []string
		for _, block := range sl.Message.Content {
			switch block.Type {
			case "thinking":
				if block.Thinking != "" {
					t := block.Thinking
					if len(t) > 200 {
						t = t[:200] + "..."
					}
					lines = append(lines, fmt.Sprintf("💭 %s", t))
				}
			case "text":
				if block.Text != "" {
					t := block.Text
					if len(t) > 200 {
						t = t[:200] + "..."
					}
					lines = append(lines, t)
				}
			case "tool_use":
				lines = append(lines, formatToolUse(block))
			}
		}
		return lines

	case "tool_result", "user":
		// tool results / user turns — skip (they're verbose)
		return nil

	case "rate_limit_event":
		return []string{"⏳ rate limited, waiting..."}

	case "result":
		if sl.CostUSD > 0 {
			return []string{fmt.Sprintf("✓ Done (cost: $%.4f)", sl.CostUSD)}
		}
		return []string{"✓ Done"}
	}

	return nil
}

// formatToolUse produces a readable summary of a tool call.
func formatToolUse(block contentBlock) string {
	var input map[string]any
	if json.Unmarshal(block.Input, &input) != nil {
		return fmt.Sprintf("→ %s", block.Name)
	}

	// Show the most relevant input field per tool
	switch block.Name {
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			if len(cmd) > 100 {
				cmd = cmd[:100] + "..."
			}
			return fmt.Sprintf("→ Bash: %s", cmd)
		}
	case "Read":
		if path, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("→ Read: %s", path)
		}
	case "Write":
		if path, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("→ Write: %s", path)
		}
	case "Edit", "MultiEdit":
		if path, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("→ Edit: %s", path)
		}
	case "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			return fmt.Sprintf("→ Glob: %s", pattern)
		}
	case "Grep":
		if pattern, ok := input["pattern"].(string); ok {
			return fmt.Sprintf("→ Grep: %s", pattern)
		}
	}

	return fmt.Sprintf("→ %s", block.Name)
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
