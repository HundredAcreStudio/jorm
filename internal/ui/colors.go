package ui

import "github.com/fatih/color"

// Color palette for terminal output.
var (
	// Agent names
	colorAgent = color.New(color.FgCyan)

	// Timestamps
	colorDim = color.New(color.Faint)

	// Status indicators
	colorSuccess = color.New(color.FgGreen, color.Bold)
	colorFailure = color.New(color.FgRed, color.Bold)
	colorWarning = color.New(color.FgYellow)

	// Structural elements
	colorSeparator = color.New(color.Faint)
	colorBorder    = color.New(color.Faint)

	// Phase/system messages
	colorBold = color.New(color.Bold)

	// Footer-specific
	colorFooterLabel  = color.New(color.Faint)
	colorFooterValue  = color.New(color.FgWhite, color.Bold)
	colorFooterStatus = color.New(color.FgGreen, color.Bold)
	colorFooterCost   = color.New(color.FgYellow)
	colorFooterActive = color.New(color.FgCyan, color.Bold)
)
