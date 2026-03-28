package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"github.com/jorm/internal/config"
	"github.com/jorm/internal/loop"
)

var (
	rBold  = color.New(color.Bold).SprintFunc()
	rGreen = color.New(color.FgGreen).SprintFunc()
	rRed   = color.New(color.FgRed).SprintFunc()
	rCyan  = color.New(color.FgCyan).SprintFunc()
)

// Run starts the bubbletea TUI and runs the loop in a background goroutine.
func Run(ctx context.Context, opts loop.Options) error {
	// Load config to get validator names for the UI
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}

	profile := cfg.Profile
	if opts.Profile != "" {
		profile = opts.Profile
	}

	validators, err := cfg.ValidatorsForProfile(profile)
	if err != nil {
		return err
	}

	var validatorNames []string
	for _, v := range validators {
		validatorNames = append(validatorNames, v.Name)
	}

	m := newModel(profile, cfg.Model, validatorNames)
	p := tea.NewProgram(m, tea.WithAltScreen())

	sink := &ProgramSink{P: p}
	opts.Sink = sink

	go func() {
		// loop.Run defers sink.LoopDone before any fallible work,
		// so ProgramSink.LoopDone (→ LoopDoneMsg) fires on all paths.
		_ = loop.Run(ctx, opts)
	}()

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	fm := finalModel.(model)

	// Print summary after alt-screen clears
	printSummary(fm)

	return fm.finalErr
}

func printSummary(m model) {
	fmt.Println()
	if m.issueTitle != "" {
		fmt.Printf("  %s %s\n", rBold("Issue:"), m.issueTitle)
	}

	// Classification
	if m.classification != "" {
		fmt.Printf("  %s %s\n", rBold("Classification:"), m.classification)
	}

	if m.attempt > 0 {
		fmt.Printf("  %s %d  %s %s  %s %s\n",
			rBold("Attempts:"), m.attempt,
			rBold("Profile:"), m.profile,
			rBold("Model:"), m.modelName)
	}

	// Agent summary
	if len(m.agents) > 0 {
		fmt.Printf("  %s %d agents\n", rBold("Agents:"), len(m.agents))
		for _, a := range m.agents {
			fmt.Printf("    %-15s  state=%s\n", a.name, a.state)
		}
	}

	// Validator results
	if len(m.validators) > 0 {
		fmt.Printf("  %s ", rBold("Validators:"))
		for _, v := range m.validators {
			switch v.status {
			case "pass":
				fmt.Printf("%s %s  ", rGreen("✓"), v.name)
			case "fail":
				fmt.Printf("%s %s  ", rRed("✗"), v.name)
			default:
				fmt.Printf("○ %s  ", v.name)
			}
		}
		fmt.Println()
	}

	// Cost
	if m.totalCost > 0 {
		fmt.Printf("  %s $%.4f\n", rBold("Total Cost:"), m.totalCost)
	}

	// Key phases (worktree location, hooks, results)
	for _, phase := range m.phases {
		if containsAny(phase, "Worktree kept", "Branch ", "Hook", "failed", "Failed", "push", "pr create", "✓", "Workflow") {
			fmt.Printf("  %s %s\n", rCyan("→"), phase)
		}
	}

	// Final status
	fmt.Println()
	if m.finalErr != nil {
		fmt.Printf("  %s %s\n", rRed("✗ Failed:"), m.finalErr)
	} else {
		fmt.Printf("  %s\n", rGreen("✓ Completed successfully!"))
	}
	fmt.Println()
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
