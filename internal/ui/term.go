package ui

import (
	"os"

	"golang.org/x/sys/unix"
)

// termSize returns the current terminal width and height.
// Falls back to 80x24 if detection fails.
func termSize() (width, height int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}
