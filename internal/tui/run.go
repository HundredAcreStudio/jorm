package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jorm/internal/config"
	"github.com/jorm/internal/loop"
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
		err := loop.Run(ctx, opts)
		p.Send(LoopDoneMsg{Err: err})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	fm := finalModel.(model)
	return fm.finalErr
}
