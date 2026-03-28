package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jorm/internal/agent"
	jormerrors "github.com/jorm/internal/errors"
)

// stageRecord tracks timing and status for a single pipeline stage.
type stageRecord struct {
	name      string
	startTime time.Time
	endTime   time.Time
	rounds    int
	failed    bool
	err       error
}

// UI implements events.Sink with zeroshot-style scrolling log + persistent ANSI footer.
type UI struct {
	mu          sync.Mutex
	w           io.Writer
	formatter   *Formatter
	footer      *Footer
	metrics     *ProcessMetrics
	round       int
	runID       string
	startTime   time.Time
	termWidth       int
	termHeight      int
	lastFooterLines int
	totalAgents     int
	totalCost   float64
	cancel      context.CancelFunc

	stageRecords []stageRecord
	issueTitle   string
	issueURL     string
}

// New creates a new UI. It writes to os.Stdout.
func New(runID string, totalAgents int) *UI {
	width, height := termSize()
	f := NewFooter(runID, totalAgents, width)
	f.SetTermSize(width, height)

	ctx, cancel := context.WithCancel(context.Background())

	footerLines := f.Lines()
	u := &UI{
		w:               os.Stdout,
		formatter:       &Formatter{},
		footer:          f,
		metrics:         NewProcessMetrics(),
		runID:           runID,
		startTime:       time.Now(),
		termWidth:       width,
		termHeight:      height,
		lastFooterLines: footerLines,
		totalAgents:     totalAgents,
		cancel:          cancel,
	}

	// Set scroll region — reserves space at the bottom for the footer.
	_, _ = fmt.Fprint(u.w, InitScrollRegion(height, footerLines))
	u.paintFooter()
	u.startFooterLoop(ctx)

	return u
}

// startFooterLoop repaints the footer every 500ms to update elapsed time, etc.
func (u *UI) startFooterLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w, h := termSize()
				u.footer.SetTermSize(w, h)
				u.mu.Lock()
				u.termWidth = w
				u.termHeight = h
				u.mu.Unlock()
				u.paintFooter()
			}
		}
	}()
}

// paintFooter writes the footer to the reserved area. Safe to call from any goroutine.
func (u *UI) paintFooter() {
	footerLines := u.footer.Lines()

	u.mu.Lock()
	if footerLines != u.lastFooterLines {
		_, _ = fmt.Fprint(u.w, InitScrollRegion(u.termHeight, footerLines))
		u.lastFooterLines = footerLines
	}
	u.mu.Unlock()

	paint := u.footer.Paint()
	if paint != "" {
		u.mu.Lock()
		_, _ = fmt.Fprint(u.w, paint)
		u.mu.Unlock()
	}
}

// Metrics returns the process metrics for PID registration.
func (u *UI) Metrics() *ProcessMetrics {
	return u.metrics
}

func (u *UI) printLine(line string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, _ = fmt.Fprintln(u.w, line)
}

func (u *UI) printAgentLine(name, text string) {
	u.printLine(u.formatter.FormatAgentLine(name, text))
}

func (u *UI) timestamp() string {
	return u.formatter.FormatTimestamp(time.Now().Format("15:04:05"))
}

// --- events.Sink implementation ---

func (u *UI) UpdateTotalAgents(count int) {
	u.mu.Lock()
	u.totalAgents = count
	u.mu.Unlock()
	u.footer.SetTotalAgents(count)
	u.paintFooter()
}

func (u *UI) Phase(name string) {
	u.printAgentLine("system", colorBold.Sprint(name))
}

func (u *UI) IssueLoaded(title, url string) {
	u.mu.Lock()
	u.issueTitle = title
	u.issueURL = url
	u.mu.Unlock()

	u.printLine(u.formatter.FormatSeparator("", u.termWidth))
	u.printAgentLine("system", fmt.Sprintf("%s 📋 %s", u.timestamp(), colorBold.Sprint("NEW TASK")))
	u.printAgentLine("system", colorBold.Sprintf("# %s", title))
	if url != "" {
		u.printAgentLine("system", colorDim.Sprint(url))
	}
	u.printLine(u.formatter.FormatSeparator("", u.termWidth))
}

func (u *UI) Attempt(current, max int) {
	if max > 0 {
		u.printAgentLine("system", colorBold.Sprintf("Attempt %d/%d", current, max))
	} else {
		u.printAgentLine("system", colorBold.Sprintf("Attempt %d", current))
	}
}

func (u *UI) ClaudeOutput(text string) {
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
	u.printAgentLine(name, fmt.Sprintf("%s → %s", colorWarning.Sprint("⚡"), colorDim.Sprintf("task %s (validating)", id)))
}

func (u *UI) ValidatorDone(result agent.ValidatorResult) {
	if result.Passed {
		u.printAgentLine(result.Name, fmt.Sprintf("%s %s", u.timestamp(), colorSuccess.Sprint("✓ APPROVED")))
		if result.Output != "" {
			lines := strings.SplitN(result.Output, "\n", 2)
			summary := lines[0]
			if len(summary) > 120 {
				summary = summary[:120] + "..."
			}
			u.printAgentLine(result.Name, colorDim.Sprint(summary))
		}
	} else {
		u.printAgentLine(result.Name, fmt.Sprintf("%s %s", u.timestamp(), colorFailure.Sprint("✗ REJECTED")))
		if result.Output != "" {
			lines := strings.SplitN(result.Output, "\n", 4)
			for _, line := range lines {
				if line = strings.TrimSpace(line); line != "" {
					trimmed := line
					if len(trimmed) > 120 {
						trimmed = trimmed[:120] + "..."
					}
					u.printAgentLine(result.Name, colorFailure.Sprint(trimmed))
				}
			}
		}
	}
}

func (u *UI) AgentStateChange(agentID, agentName, state string) {
	switch state {
	case "executing":
		u.footer.AddAgent(agentID, agentName, 0)
		u.paintFooter()
	case "idle":
		u.footer.RemoveAgent(agentID)
		u.paintFooter()
	}
}

func (u *UI) MessagePublished(topic, sender string) {
	u.printAgentLine(sender, fmt.Sprintf("%s %s", colorWarning.Sprint("⚡"), topic))
}

func (u *UI) Cost(totalCost float64) {
	u.mu.Lock()
	u.totalCost = totalCost
	u.mu.Unlock()
	u.footer.UpdateCost(totalCost)
}

func (u *UI) Classification(classification string) {
	u.printAgentLine("system", fmt.Sprintf("Classification: %s", colorBold.Sprint(classification)))
}

func (u *UI) LoopDone(err error) {
	if u.cancel != nil {
		u.cancel()
	}

	u.mu.Lock()
	termHeight := u.termHeight
	footerLines := u.lastFooterLines
	_, _ = fmt.Fprint(u.w, u.footer.Clear(termHeight, footerLines))
	records := make([]stageRecord, len(u.stageRecords))
	copy(records, u.stageRecords)
	issueTitle := u.issueTitle
	issueURL := u.issueURL
	u.mu.Unlock()

	elapsed := time.Since(u.startTime).Truncate(time.Second)

	if err != nil {
		// Find the failed stage name
		failedStage := ""
		for _, r := range records {
			if r.failed {
				failedStage = r.name
				break
			}
		}

		if failedStage != "" {
			u.printAgentLine(u.runID, fmt.Sprintf("%s %s", u.timestamp(), colorFailure.Sprintf("❌ FAILED at stage %q", failedStage)))
		} else {
			u.printAgentLine(u.runID, fmt.Sprintf("%s %s", u.timestamp(), colorFailure.Sprint("❌ FAILED")))
		}
	} else {
		u.printAgentLine(u.runID, fmt.Sprintf("%s %s", u.timestamp(), colorSuccess.Sprint("🎉 COMPLETED")))
	}

	u.printLine("")
	u.printLine(u.formatter.FormatDoubleSeparator("", u.termWidth))
	u.printLine("")

	// Print stage summary if we have any
	if len(records) > 0 {
		u.printLine("Stage summary:")
		for _, r := range records {
			duration := ""
			if !r.endTime.IsZero() {
				duration = r.endTime.Sub(r.startTime).Truncate(time.Second).String()
			}
			roundInfo := ""
			if r.rounds > 1 {
				roundInfo = fmt.Sprintf(", %d rounds", r.rounds)
			}
			if r.failed {
				ce := jormerrors.ClassifyError(r.err)
				detail := ""
				if ce != nil {
					detail = ce.Msg
				}
				if detail != "" {
					u.printLine(fmt.Sprintf("  %s %-25s — %s", colorFailure.Sprint("✗"), r.name, detail))
				} else {
					u.printLine(fmt.Sprintf("  %s %-25s", colorFailure.Sprint("✗"), r.name))
				}
			} else if !r.endTime.IsZero() {
				u.printLine(fmt.Sprintf("  %s %-25s (%s%s)", colorSuccess.Sprint("✓"), r.name, duration, roundInfo))
			} else {
				u.printLine(fmt.Sprintf("  %s %-25s", colorDim.Sprint("○"), r.name))
			}
		}
		u.printLine("")
	}

	// Print metadata
	if issueTitle != "" {
		if issueURL != "" {
			u.printLine(fmt.Sprintf("Issue:  %s (%s)", issueTitle, issueURL))
		} else {
			u.printLine(fmt.Sprintf("Issue:  %s", issueTitle))
		}
	}
	u.printLine(fmt.Sprintf("Run:    %s", u.runID))
	u.printLine(fmt.Sprintf("Time:   %s", elapsed))
	if u.totalCost > 0 {
		u.printLine(fmt.Sprintf("Cost:   $%.2f", u.totalCost))
	}

	if err != nil {
		ce := jormerrors.ClassifyError(err)
		if ce != nil {
			u.printLine(fmt.Sprintf("Error:  %s", ce.Msg))
			if ce.Hint != "" {
				u.printLine(fmt.Sprintf("Tip:    %s", ce.Hint))
			}
		} else {
			u.printLine(fmt.Sprintf("Error:  %s", err))
		}
	}
	u.printLine("")
}

// --- Lifecycle event methods ---

func (u *UI) AgentSpawned(id, name string, triggers []string) {
	u.printAgentLine(name, fmt.Sprintf("%s started %s",
		colorSuccess.Sprint("▶"),
		colorDim.Sprintf("(listening for: %s)", strings.Join(triggers, ", "))))
}

func (u *UI) AgentTriggerFired(id, topic string, taskNum int, model string) {
	u.printAgentLine(id, fmt.Sprintf("%s %s → task #%d %s",
		colorWarning.Sprint("⚡"),
		topic, taskNum,
		colorDim.Sprintf("(%s)", model)))
	u.footer.AddAgent(id, id, taskNum)
	u.paintFooter()
}

func (u *UI) AgentTaskCompleted(id string, taskNum int) {
	u.printAgentLine(id, colorSuccess.Sprintf("✓ task #%d completed", taskNum))
	u.footer.RemoveAgent(id)
	u.paintFooter()
}

func (u *UI) AgentTaskFailed(id string, taskNum int, err error) {
	u.printAgentLine(id, colorFailure.Sprintf("✗ task #%d failed: %v", taskNum, err))
	u.footer.RemoveAgent(id)
	u.paintFooter()
}

func (u *UI) AgentTokenUsage(id, name string, input, output int) {
	u.printAgentLine(name, fmt.Sprintf("%s %s",
		u.timestamp(),
		colorDim.Sprintf("tokens: %d in + %d out", input, output)))
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
	var statusColor = colorSuccess
	if rejected > 0 {
		statusColor = colorWarning
	}
	header := statusColor.Sprintf("Round %d Results: %d/%d approved", round, approved, total)
	u.printLine("")
	u.printLine(u.formatter.FormatSeparator(header, u.termWidth))
}

func (u *UI) RetryRoundStart(round int) {
	u.mu.Lock()
	u.round = round
	u.mu.Unlock()
	u.printLine("")
	u.printLine(u.formatter.FormatSeparator(colorWarning.Sprintf("Retry Round %d", round), u.termWidth))
	u.printLine("")
}

func (u *UI) SystemEvent(text string) {
	u.printLine(u.formatter.FormatSeparator("", u.termWidth))
	u.printAgentLine("system", colorBold.Sprint(text))
	u.printLine(u.formatter.FormatSeparator("", u.termWidth))
}

func (u *UI) ClusterComplete(runID, reason string) {
	u.printLine("")
	u.printLine(u.formatter.FormatDoubleSeparator("", u.termWidth))
	u.printLine("")
	u.printAgentLine(runID, fmt.Sprintf("%s %s", u.timestamp(), colorSuccess.Sprint("🎉 CLUSTER COMPLETE")))
	u.printAgentLine(runID, colorDim.Sprint(reason))
	u.printLine("")
	u.printLine(u.formatter.FormatDoubleSeparator("", u.termWidth))
	u.printLine("")
}

func (u *UI) StageStarted(stageIndex int, stageName string) {
	u.mu.Lock()
	u.stageRecords = append(u.stageRecords, stageRecord{
		name:      stageName,
		startTime: time.Now(),
	})
	u.mu.Unlock()
}

func (u *UI) StageCompleted(stageIndex int, stageName string) {
	u.mu.Lock()
	if stageIndex < len(u.stageRecords) {
		u.stageRecords[stageIndex].endTime = time.Now()
	}
	u.mu.Unlock()
}

func (u *UI) StageFailed(stageIndex int, stageName string, err error) {
	u.mu.Lock()
	if stageIndex < len(u.stageRecords) {
		u.stageRecords[stageIndex].endTime = time.Now()
		u.stageRecords[stageIndex].failed = true
		u.stageRecords[stageIndex].err = err
	}
	u.mu.Unlock()
}

func (u *UI) StageRoundStarted(stageIndex int, round int) {
	u.mu.Lock()
	if stageIndex < len(u.stageRecords) {
		u.stageRecords[stageIndex].rounds = round
	}
	u.mu.Unlock()
}
