package jormpath

import (
	"os"
	"path/filepath"
)

// StoreDir returns the global jorm data directory for the SQLite database.
// If JORM_HOME is set, it is used directly. Otherwise defaults to ~/.jorm.
func StoreDir() (string, error) {
	if dir := os.Getenv("JORM_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".jorm"), nil
}

// ProjectDir returns the project-local .jorm directory (cwd/.jorm).
// Used for run logs that belong alongside the project code.
func ProjectDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, ".jorm"), nil
}
