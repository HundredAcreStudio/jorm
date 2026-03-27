package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jorm/internal/jormpath"
	"github.com/jorm/internal/mcp"
)

func main() {
	storeDir, err := jormpath.StoreDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	projDir, err := jormpath.ProjectDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	dbPath := filepath.Join(storeDir, "jorm.db")
	logDir := filepath.Join(projDir, "logs")
	if err := mcp.NewServer(dbPath, logDir).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
