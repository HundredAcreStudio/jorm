package mcp

import (
	"testing"
)

// AC5: NewServer constructor exists and returns a non-nil *Server
func TestNewServer_NotNil(t *testing.T) {
	s := NewServer("/tmp/test.db", "/tmp/logs")
	if s == nil {
		t.Error("NewServer returned nil")
	}
}

// AC5: NewServer stores the dbPath and logDir
func TestNewServer_StoresConfig(t *testing.T) {
	s := NewServer("/custom/path/jorm.db", "/custom/logs")
	if s.dbPath != "/custom/path/jorm.db" {
		t.Errorf("dbPath = %q, want %q", s.dbPath, "/custom/path/jorm.db")
	}
	if s.logDir != "/custom/logs" {
		t.Errorf("logDir = %q, want %q", s.logDir, "/custom/logs")
	}
}
