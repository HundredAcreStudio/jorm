package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type validatorState struct {
	id     string
	name   string
	status string // "pending", "running", "pass", "fail"
}

type agentInfo struct {
	id        string
	name      string
	state     string
	iteration int
}

type model struct {
	// Header
	issueTitle  string
	attempt     int
	maxAttempts int
	profile     string
	modelName   string

	// Phase
	phase   string
	spinner spinner.Model

	// Output
	viewport    viewport.Model
	outputLines []string

	// Validators
	validators []validatorState

	// Agents
	agents []agentInfo

	// Terminal size
	width  int
	height int

	// State
	done     bool
	finalErr error

	// Summary (collected for post-TUI display)
	phases      []string
	classification string
	totalCost   float64
}

func newModel(profile, modelName string, validatorNames []string) model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	vp := viewport.New(80, 20)

	vals := make([]validatorState, len(validatorNames))
	for i, name := range validatorNames {
		vals[i] = validatorState{id: name, name: name, status: "pending"}
	}

	return model{
		profile:   profile,
		modelName: modelName,
		spinner:   s,
		viewport:  vp,
		validators: vals,
		phase:     "Initializing...",
	}
}

func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.done = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerHeight := 4 // header + phase + validator bar + borders
		vpHeight := m.height - headerHeight
		if vpHeight < 3 {
			vpHeight = 3
		}
		m.viewport.Width = m.width
		m.viewport.Height = vpHeight

	case PhaseMsg:
		m.phase = msg.Name
		m.phases = append(m.phases, msg.Name)

	case IssueLoadedMsg:
		m.issueTitle = msg.Title

	case AttemptMsg:
		m.attempt = msg.Current
		m.maxAttempts = msg.Max

	case ClaudeOutputMsg:
		m.outputLines = append(m.outputLines, msg.Text)
		m.viewport.SetContent(strings.Join(m.outputLines, "\n"))
		m.viewport.GotoBottom()

	case ValidatorStartMsg:
		for i := range m.validators {
			if m.validators[i].name == msg.Name || m.validators[i].id == msg.ID {
				m.validators[i].status = "running"
				break
			}
		}

	case ValidatorDoneMsg:
		for i := range m.validators {
			if m.validators[i].name == msg.Name || m.validators[i].id == msg.ValidatorID {
				if msg.Passed {
					m.validators[i].status = "pass"
				} else {
					m.validators[i].status = "fail"
				}
				break
			}
		}

	case CostMsg:
		m.totalCost = msg.TotalCost

	case ClassificationMsg:
		m.classification = msg.Classification

	case AgentStateChangeMsg:
		found := false
		for i := range m.agents {
			if m.agents[i].id == msg.ID {
				m.agents[i].state = msg.State
				found = true
				break
			}
		}
		if !found {
			m.agents = append(m.agents, agentInfo{
				id:    msg.ID,
				name:  msg.Name,
				state: msg.State,
			})
		}

	case LoopDoneMsg:
		m.done = true
		m.finalErr = msg.Err
		if msg.Err != nil {
			m.phase = fmt.Sprintf("Failed: %s", msg.Err)
		} else {
			m.phase = "Done!"
		}
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Forward to viewport for scrolling
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	var b strings.Builder

	// Header
	title := m.issueTitle
	if title == "" {
		title = "..."
	}
	if len(title) > m.width-30 && m.width > 30 {
		title = title[:m.width-33] + "..."
	}
	attemptStr := ""
	if m.maxAttempts > 0 {
		attemptStr = fmt.Sprintf("  Attempt %d/%d", m.attempt, m.maxAttempts)
	}
	b.WriteString(fmt.Sprintf("  %s%s\n", title, attemptStr))
	b.WriteString(fmt.Sprintf("  profile: %s  model: %s\n", m.profile, m.modelName))

	// Phase with spinner
	if !m.done {
		b.WriteString(fmt.Sprintf("  %s %s\n", m.spinner.View(), m.phase))
	} else {
		if m.finalErr != nil {
			b.WriteString(fmt.Sprintf("  ✗ %s\n", m.phase))
		} else {
			b.WriteString(fmt.Sprintf("  ✓ %s\n", m.phase))
		}
	}

	// Agent status panel
	if len(m.agents) > 0 {
		b.WriteString("  Agents: ")
		for i, a := range m.agents {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(fmt.Sprintf("%s[%s]", a.name, a.state))
		}
		b.WriteString("\n")
	}

	// Viewport
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Validator bar
	var vals []string
	for _, v := range m.validators {
		var icon string
		switch v.status {
		case "pending":
			icon = "○"
		case "running":
			icon = m.spinner.View()
		case "pass":
			icon = "✓"
		case "fail":
			icon = "✗"
		}
		vals = append(vals, fmt.Sprintf("%s %s", v.name, icon))
	}
	b.WriteString("  " + strings.Join(vals, "  "))

	return b.String()
}
