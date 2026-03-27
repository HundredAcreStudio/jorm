package ui

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxFooterReserve is the number of terminal rows reserved for the footer.
// Set once at startup, never changed. Enough for border + ~6 agents + status bar.
const maxFooterReserve = 8

// AgentRow represents one active agent in the footer.
type AgentRow struct {
	ID        string
	Name      string
	CPU       float64
	RAMMB     float64
	Iteration int
}

// Footer manages the persistent footer pinned to the bottom of the terminal.
type Footer struct {
	mu           sync.Mutex
	runID        string
	startTime    time.Time
	activeAgents map[string]*AgentRow
	totalAgents  int
	totalCost    float64
	width        int
	height       int // terminal rows
	status       string
}

// NewFooter creates a new Footer.
func NewFooter(runID string, totalAgents, width int) *Footer {
	return &Footer{
		runID:        runID,
		startTime:    time.Now(),
		activeAgents: make(map[string]*AgentRow),
		totalAgents:  totalAgents,
		width:        width,
		height:       24,
		status:       "running",
	}
}

// SetTotalAgents updates the total agent count.
func (f *Footer) SetTotalAgents(count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.totalAgents = count
}

// SetTermSize updates the terminal dimensions.
func (f *Footer) SetTermSize(width, height int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.width = width
	f.height = height
}

// AddAgent adds an agent row to the footer.
func (f *Footer) AddAgent(id, name string, iteration int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activeAgents[id] = &AgentRow{
		ID:        id,
		Name:      name,
		Iteration: iteration,
	}
}

// RemoveAgent removes an agent row from the footer.
func (f *Footer) RemoveAgent(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.activeAgents, id)
}

// UpdateMetrics updates CPU and RAM for an agent.
func (f *Footer) UpdateMetrics(id string, cpu, ramMB float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row, ok := f.activeAgents[id]; ok {
		row.CPU = cpu
		row.RAMMB = ramMB
	}
}

// UpdateCost sets the total cost.
func (f *Footer) UpdateCost(cost float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.totalCost = cost
}

// SetStatus updates the footer status text.
func (f *Footer) SetStatus(status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

// Lines returns the number of lines the footer content will occupy.
func (f *Footer) Lines() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.linesLocked()
}

func (f *Footer) linesLocked() int {
	if len(f.activeAgents) == 0 {
		return 1 // just the status bar
	}
	n := len(f.activeAgents) + 2 // top border + agent rows + status bar
	if n > maxFooterReserve {
		n = maxFooterReserve
	}
	return n
}

// InitScrollRegion returns the escape sequence to set a fixed scroll region,
// reserving maxFooterReserve rows at the bottom. Call once at startup.
func InitScrollRegion(termHeight, footerLines int) string {
	scrollEnd := termHeight - footerLines
	if scrollEnd < 1 {
		scrollEnd = 1
	}
	// Set scroll region to rows 1..scrollEnd, position cursor at bottom of region
	return fmt.Sprintf("\033[1;%dr\033[%d;1H", scrollEnd, scrollEnd)
}

// Paint renders the footer into the reserved area at the bottom of the terminal.
// Uses absolute cursor positioning — does not modify the scroll region.
// The entire output is a single string for atomic writing.
func (f *Footer) Paint() string {
	f.mu.Lock()
	lines := f.renderLines()
	height := f.height
	f.mu.Unlock()

	if len(lines) == 0 {
		return ""
	}

	// Footer content is bottom-aligned within the reserved area.
	reserveStart := height - len(lines) + 1
	if reserveStart < 1 {
		reserveStart = 1
	}
	contentStart := reserveStart

	var b strings.Builder
	b.Grow(512)
	b.WriteString("\0337") // save cursor

	// Clear the entire reserved area, then draw content lines bottom-aligned
	for row := reserveStart; row <= height; row++ {
		fmt.Fprintf(&b, "\033[%d;1H\033[K", row)
	}
	for i, line := range lines {
		row := contentStart + i
		fmt.Fprintf(&b, "\033[%d;1H%s", row, line)
	}

	b.WriteString("\0338") // restore cursor
	return b.String()
}

// Render produces the footer as a single string (public, for testing).
func (f *Footer) Render() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.renderLines(), "\n")
}

// renderLines returns each footer line as a separate string (no trailing newlines).
func (f *Footer) renderLines() []string {
	w := f.width
	if w < 40 {
		w = 40
	}

	elapsed := time.Since(f.startTime).Truncate(time.Second)

	// No active agents: single compact status bar
	if len(f.activeAgents) == 0 {
		return []string{f.renderStatusBar(elapsed, 0, 0)}
	}

	var lines []string

	// Top border
	top := colorBorder.Sprint("┌") +
		colorBorder.Sprint(strings.Repeat("─", w-2)) +
		colorBorder.Sprint("┐")
	lines = append(lines, top)

	// Sort agents by name for stable output
	ids := make([]string, 0, len(f.activeAgents))
	for id := range f.activeAgents {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Cap agent rows to fit in reserve
	maxAgentRows := maxFooterReserve - 2 // minus border and status bar
	if len(ids) > maxAgentRows {
		ids = ids[:maxAgentRows]
	}

	var totalCPU, totalRAM float64
	for _, id := range ids {
		row := f.activeAgents[id]
		totalCPU += row.CPU
		totalRAM += row.RAMMB

		name := row.Name
		if len(name) > 20 {
			name = name[:20]
		}

		var b strings.Builder
		b.WriteString(colorBorder.Sprint("│"))
		b.WriteString(" ")
		b.WriteString(colorFooterActive.Sprint("●"))
		b.WriteString(" ")
		b.WriteString(colorAgent.Sprintf("%-20s", name))

		if row.CPU > 0 || row.RAMMB > 0 {
			b.WriteString(colorDim.Sprintf("  cpu %5.1f%%  mem %5.0fMB", row.CPU, row.RAMMB))
		}
		if row.Iteration > 0 {
			b.WriteString(colorDim.Sprintf("  #%d", row.Iteration))
		}
		lines = append(lines, b.String())
	}

	// Bottom status bar
	lines = append(lines, f.renderStatusBar(elapsed, totalCPU, totalRAM))

	return lines
}

func (f *Footer) renderStatusBar(elapsed time.Duration, totalCPU, totalRAM float64) string {
	var b strings.Builder

	active := len(f.activeAgents)

	// Build segments
	statusIcon := colorFooterStatus.Sprint("●")
	if f.status == "failed" {
		statusIcon = colorFailure.Sprint("●")
	}

	segments := []string{
		fmt.Sprintf(" %s %s %s",
			statusIcon,
			colorFooterValue.Sprint(f.runID),
			colorFooterStatus.Sprint(f.status)),
		colorFooterValue.Sprint(elapsed),
		fmt.Sprintf("%s %s",
			colorFooterActive.Sprintf("%d/%d", active, f.totalAgents),
			colorFooterLabel.Sprint("agents")),
		colorFooterCost.Sprintf("$%.2f", f.totalCost),
	}

	if totalCPU > 0 {
		segments = append(segments,
			colorDim.Sprintf("Σ cpu:%.0f%% mem:%.0fMB", totalCPU, totalRAM))
	}

	sep := colorFooterLabel.Sprint(" │ ")
	content := strings.Join(segments, sep) + " "

	if active > 0 {
		b.WriteString(colorBorder.Sprint("└"))
	} else {
		b.WriteString(colorBorder.Sprint("─"))
	}
	b.WriteString(content)
	b.WriteString(colorBorder.Sprint("─"))

	return b.String()
}

// Clear resets the scroll region and clears the footer area.
func (f *Footer) Clear(termHeight, footerLines int) string {
	cursorRow := termHeight - footerLines
	if cursorRow < 1 {
		cursorRow = 1
	}
	return fmt.Sprintf("\033[r\033[%d;1H\033[J", cursorRow)
}
