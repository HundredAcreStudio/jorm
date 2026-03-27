package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jorm/internal/mcp"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	dbPath := filepath.Join(home, ".jorm", "jorm.db")
	logDir := filepath.Join(home, ".jorm", "logs")
	if err := mcp.NewServer(dbPath, logDir).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
