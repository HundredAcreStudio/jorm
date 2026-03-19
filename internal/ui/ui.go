package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jorm/internal/agent"
)

// UI implements events.Sink with zeroshot-style scrolling log + ANSI footer.
type UI struct {
	mu          sync.Mutex
	w           io.Writer
	formatter   *Formatter
	footer      *Footer
	metrics     *ProcessMetrics
	round       int
	runID       string
	startTime   time.Time
	termWidth   int
	totalAgents int
	totalCost   float64
}

// New creates a new UI. It writes to os.Stdout.
func New(runID string, totalAgents int) *UI {
	width := 80
	// Try to detect terminal width; fall back to 80
	// Using a simple approach that works without x/term
	return &UI{
		w:           os.Stdout,
		formatter:   &Formatter{},
		footer:      NewFooter(runID, totalAgents, width),
		metrics:     NewProcessMetrics(),
		runID:       runID,
		startTime:   time.Now(),
		termWidth:   width,
		totalAgents: totalAgents,
	}
}

// Metrics returns the process metrics for PID registration.
func (u *UI) Metrics() *ProcessMetrics {
	return u.metrics
}

func (u *UI) printLine(line string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprintln(u.w, line)
}

func (u *UI) printAgentLine(name, text string) {
	u.printLine(u.formatter.FormatAgentLine(name, text))
}

func (u *UI) timestamp() string {
	return time.Now().Format("15:04:05")
}

// --- events.Sink implementation ---

func (u *UI) UpdateTotalAgents(count int) {
	u.mu.Lock()
	u.totalAgents = count
	u.mu.Unlock()
	u.footer.SetTotalAgents(count)
}

func (u *UI) Phase(name string) {
	u.printAgentLine("system", name)
}

func (u *UI) IssueLoaded(title, url string) {
	u.printLine(u.formatter.FormatSeparator("", u.termWidth))
	u.printAgentLine("system", fmt.Sprintf("%s 📋 NEW TASK", u.timestamp()))
	u.printAgentLine("system", fmt.Sprintf("# %s", title))
	if url != "" {
		u.printAgentLine("system", url)
	}
	u.printLine(u.formatter.FormatSeparator("", u.termWidth))
}

func (u *UI) Attempt(current, max int) {
	if max > 0 {
		u.printAgentLine("system", fmt.Sprintf("Attempt %d/%d", current, max))
	} else {
		u.printAgentLine("system", fmt.Sprintf("Attempt %d", current))
	}
}

func (u *UI) ClaudeOutput(text string) {
	// Extract agent name prefix if present: [agentName] text
	if strings.HasPrefix(text, "[") {
		if idx := strings.Index(text, "] "); idx > 0 {
			name := text[1:idx]
			rest := text[idx+2:]
			u.printAgentLine(name, rest)
			return
		}
	}
	u.printAgentLine("system", text)
}

func (u *UI) ValidatorStart(id, name string) {
	u.printAgentLine(name, fmt.Sprintf("⚡ %s → task (validating)", id))
}

func (u *UI) ValidatorDone(result agent.ValidatorResult) {
	if result.Passed {
		u.printAgentLine(result.Name, fmt.Sprintf("%s ✓ APPROVED", u.timestamp()))
		if result.Output != "" {
			// Show first line of output
			lines := strings.SplitN(result.Output, "\n", 2)
			summary := lines[0]
			if len(summary) > 120 {
				summary = summary[:120] + "..."
			}
			u.printAgentLine(result.Name, summary)
		}
	} else {
		u.printAgentLine(result.Name, fmt.Sprintf("%s ✗ REJECTED", u.timestamp()))
		if result.Output != "" {
			lines := strings.SplitN(result.Output, "\n", 4)
			for _, line := range lines {
				if line = strings.TrimSpace(line); line != "" {
					trimmed := line
					if len(trimmed) > 120 {
						trimmed = trimmed[:120] + "..."
					}
					u.printAgentLine(result.Name, trimmed)
				}
			}
		}
	}
}

func (u *UI) AgentStateChange(agentID, agentName, state string) {
	// Only log significant state changes
	switch state {
	case "executing":
		u.footer.AddAgent(agentID, agentName, 0)
	case "idle":
		u.footer.RemoveAgent(agentID)
	}
}

func (u *UI) MessagePublished(topic, sender string) {
	u.printAgentLine(sender, fmt.Sprintf("⚡ %s", topic))
}

func (u *UI) Cost(totalCost float64) {
	u.mu.Lock()
	u.totalCost = totalCost
	u.mu.Unlock()
	u.footer.UpdateCost(totalCost)
}

func (u *UI) Classification(classification string) {
	u.printAgentLine("system", fmt.Sprintf("Classification: %s", classification))
}

func (u *UI) LoopDone(err error) {
	// Clear footer
	u.mu.Lock()
	fmt.Fprint(u.w, u.footer.Clear())
	u.mu.Unlock()

	if err != nil {
		u.printLine(fmt.Sprintf("✗ Cluster %s failed: %s", u.runID, err))
	} else {
		elapsed := time.Since(u.startTime).Truncate(time.Second)
		u.printLine(fmt.Sprintf("✓ Cluster %s completed in %s │ %d agents │ %d rounds │ $%.2f",
			u.runID, elapsed, u.totalAgents, u.round, u.totalCost))
	}
}

// --- New lifecycle event methods ---

func (u *UI) AgentSpawned(id, name string, triggers []string) {
	u.printAgentLine(name, fmt.Sprintf("▶ started (listening for: %s)", strings.Join(triggers, ", ")))
}

func (u *UI) AgentTriggerFired(id, topic string, taskNum int, model string) {
	// Look up name from footer or use id
	u.printAgentLine(id, fmt.Sprintf("⚡ %s → task #%d (%s)", topic, taskNum, model))
	u.footer.AddAgent(id, id, taskNum)
}

func (u *UI) AgentTaskCompleted(id string, taskNum int) {
	u.printAgentLine(id, fmt.Sprintf("✓ task #%d completed", taskNum))
	u.footer.RemoveAgent(id)
}

func (u *UI) AgentTaskFailed(id string, taskNum int, err error) {
	u.printAgentLine(id, fmt.Sprintf("✗ task #%d failed: %v", taskNum, err))
	u.footer.RemoveAgent(id)
}

func (u *UI) AgentTokenUsage(id, name string, input, output int) {
	u.printAgentLine(name, fmt.Sprintf("%s TOKEN_USAGE", u.timestamp()))
	u.printAgentLine(name, fmt.Sprintf("%s used %d input + %d output tokens", name, input, output))
}

func (u *UI) ValidationRoundStart(round int) {
	u.mu.Lock()
	u.round = round
	u.mu.Unlock()
	u.printLine("")
	u.printLine(u.formatter.FormatSeparator(fmt.Sprintf("Validation Round %d", round), u.termWidth))
	u.printLine("")
}

func (u *UI) ValidationRoundComplete(round, approved, rejected int) {
	total := approved + rejected
	header := fmt.Sprintf("Round %d Results: %d/%d approved", round, approved, total)
	u.printLine("")
	u.printLine(u.formatter.FormatSeparator(header, u.termWidth))
}

func (u *UI) RetryRoundStart(round int) {
	u.mu.Lock()
	u.round = round
	u.mu.Unlock()
	u.printLine("")
	u.printLine(u.formatter.FormatSeparator(fmt.Sprintf("Retry Round %d", round), u.termWidth))
	u.printLine("")
}

func (u *UI) SystemEvent(text string) {
	u.printLine(u.formatter.FormatSeparator("", u.termWidth))
	u.printAgentLine("system", text)
	u.printLine(u.formatter.FormatSeparator("", u.termWidth))
}

func (u *UI) ClusterComplete(runID, reason string) {
	u.printLine("")
	u.printLine(u.formatter.FormatDoubleSeparator("", u.termWidth))
	u.printLine("")
	u.printAgentLine(runID, fmt.Sprintf("%s 🎉 CLUSTER COMPLETE", u.timestamp()))
	u.printAgentLine(runID, reason)
	u.printLine("")
	u.printLine(u.formatter.FormatDoubleSeparator("", u.termWidth))
	u.printLine("")
}
