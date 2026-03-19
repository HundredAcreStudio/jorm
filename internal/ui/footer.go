package ui

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// AgentRow represents one active agent in the footer.
type AgentRow struct {
	ID        string
	Name      string
	CPU       float64
	RAMMB     float64
	Iteration int
}

// Footer manages the persistent footer using ANSI scroll regions.
type Footer struct {
	mu           sync.Mutex
	runID        string
	startTime    time.Time
	activeAgents map[string]*AgentRow
	totalAgents  int
	totalCost    float64
	width        int
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
		status:       "running",
	}
}

// SetTotalAgents updates the total agent count.
func (f *Footer) SetTotalAgents(count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.totalAgents = count
}

// SetScrollRegion writes the ANSI escape to set the scroll region.
// totalRows is the terminal height, footerHeight is how many lines the footer needs.
func (f *Footer) SetScrollRegion(totalRows, footerHeight int) string {
	scrollEnd := totalRows - footerHeight
	if scrollEnd < 1 {
		scrollEnd = 1
	}
	return fmt.Sprintf("\033[1;%dr", scrollEnd)
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

// Height returns the number of lines the footer will occupy.
func (f *Footer) Height() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.activeAgents) == 0 {
		return 2 // top border + summary bar
	}
	return len(f.activeAgents) + 2 // top border + agent rows + summary bar
}

// Render produces the footer string with box-drawing characters.
func (f *Footer) Render() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.activeAgents) == 0 {
		return ""
	}

	w := f.width
	if w < 40 {
		w = 40
	}

	var b strings.Builder

	// Top border: ┌─ runID ───┐
	header := fmt.Sprintf("─ %s ", f.runID)
	remaining := w - 2 - len(header) // 2 for ┌ and ┐
	if remaining < 0 {
		remaining = 0
	}
	b.WriteString("┌")
	b.WriteString(header)
	b.WriteString(strings.Repeat("─", remaining))
	b.WriteString("┐\n")

	// Sort agents by name for stable output
	ids := make([]string, 0, len(f.activeAgents))
	for id := range f.activeAgents {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var totalCPU, totalRAM float64
	for _, id := range ids {
		row := f.activeAgents[id]
		totalCPU += row.CPU
		totalRAM += row.RAMMB

		name := row.Name
		if len(name) > 16 {
			name = name[:16]
		}
		line := fmt.Sprintf("  ● %-16s CPU: %5.1f%%  RAM: %6.1fMB  #%d",
			name, row.CPU, row.RAMMB, row.Iteration)
		// Pad to width
		pad := w - 2 - len(line) // 2 for │ and │
		if pad < 0 {
			pad = 0
		}
		b.WriteString("│")
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString("│\n")
	}

	// Summary bar: └─ status │ elapsed │ active/total │ $cost │ Σ CPU RAM ─┘
	elapsed := time.Since(f.startTime).Truncate(time.Second)
	active := len(f.activeAgents)
	summary := fmt.Sprintf(" %s │ %s │ %d/%d active │ $%.2f │ Σ CPU:%.1f%% RAM:%.1fMB",
		f.status, elapsed, active, f.totalAgents, f.totalCost, totalCPU, totalRAM)
	remaining = w - 2 - len(summary) // 2 for └ and ┘
	if remaining < 0 {
		remaining = 0
	}
	b.WriteString("└")
	b.WriteString(summary)
	b.WriteString(strings.Repeat("─", remaining))
	b.WriteString("┘")

	return b.String()
}

// Clear resets the scroll region and clears the footer area.
func (f *Footer) Clear() string {
	// Reset scroll region to full terminal
	// Move cursor to bottom and clear
	return "\033[r\033[999;1H\033[J"
}
