package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
max_attempts: 3
model: opus
profile: ci
validators:
  - id: lint
    name: Lint
    type: shell
    command: "golint ./..."
    on_fail: warn
    run_on: always
profiles:
  ci:
    - lint
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.Model != "opus" {
		t.Errorf("Model = %q, want %q", cfg.Model, "opus")
	}
	if cfg.Profile != "ci" {
		t.Errorf("Profile = %q, want %q", cfg.Profile, "ci")
	}
	if len(cfg.Validators) != 1 {
		t.Fatalf("len(Validators) = %d, want 1", len(cfg.Validators))
	}
	if cfg.Validators[0].ID != "lint" {
		t.Errorf("Validators[0].ID = %q, want %q", cfg.Validators[0].ID, "lint")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("Load() expected error for missing file, got nil")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "bad.yaml", `
max_attempts: [invalid
  broken: yaml: {{
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() expected error for invalid YAML, got nil")
	}
}

func TestApplyDefaultsSetsEmptyFields(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "empty.yaml", `{}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Model != "sonnet" {
		t.Errorf("Model = %q, want %q", cfg.Model, "sonnet")
	}
	if cfg.Profile != "default" {
		t.Errorf("Profile = %q, want %q", cfg.Profile, "default")
	}
	if cfg.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", cfg.MaxAttempts)
	}
}

func TestApplyDefaultsValidatorFields(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
validators:
  - id: test
    name: Test
    type: shell
    command: "go test ./..."
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cfg.Validators) != 1 {
		t.Fatalf("len(Validators) = %d, want 1", len(cfg.Validators))
	}
	v := cfg.Validators[0]
	if v.OnFail != "reject" {
		t.Errorf("OnFail = %q, want %q", v.OnFail, "reject")
	}
	if v.RunOn != "always" {
		t.Errorf("RunOn = %q, want %q", v.RunOn, "always")
	}
}

func TestApplyDefaultsDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
model: opus
profile: ci
validators:
  - id: v1
    name: V1
    type: shell
    command: "echo ok"
    on_fail: warn
    run_on: accept_only
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Model != "opus" {
		t.Errorf("Model = %q, want %q", cfg.Model, "opus")
	}
	if cfg.Profile != "ci" {
		t.Errorf("Profile = %q, want %q", cfg.Profile, "ci")
	}
	v := cfg.Validators[0]
	if v.OnFail != "warn" {
		t.Errorf("OnFail = %q, want %q", v.OnFail, "warn")
	}
	if v.RunOn != "accept_only" {
		t.Errorf("RunOn = %q, want %q", v.RunOn, "accept_only")
	}
}

func TestValidatorsForProfileKnown(t *testing.T) {
	cfg := &Config{
		Validators: []ValidatorConfig{
			{ID: "lint", Name: "Lint", Type: "shell", Command: "golint ./..."},
			{ID: "test", Name: "Test", Type: "shell", Command: "go test ./..."},
		},
		Profiles: map[string][]string{
			"default": {"lint", "test"},
		},
	}

	vals, err := cfg.ValidatorsForProfile("default")
	if err != nil {
		t.Fatalf("ValidatorsForProfile() error: %v", err)
	}
	if len(vals) != 2 {
		t.Fatalf("len(vals) = %d, want 2", len(vals))
	}
	if vals[0].ID != "lint" {
		t.Errorf("vals[0].ID = %q, want %q", vals[0].ID, "lint")
	}
	if vals[1].ID != "test" {
		t.Errorf("vals[1].ID = %q, want %q", vals[1].ID, "test")
	}
}

func TestValidatorsForProfileUnknown(t *testing.T) {
	cfg := &Config{
		Profiles: map[string][]string{
			"default": {"lint"},
		},
	}

	_, err := cfg.ValidatorsForProfile("nonexistent")
	if err == nil {
		t.Fatal("ValidatorsForProfile() expected error for unknown profile, got nil")
	}
}

func TestValidatorsForProfileMissingValidator(t *testing.T) {
	cfg := &Config{
		Validators: []ValidatorConfig{
			{ID: "lint", Name: "Lint", Type: "shell"},
		},
		Profiles: map[string][]string{
			"default": {"lint", "missing-validator"},
		},
	}

	_, err := cfg.ValidatorsForProfile("default")
	if err == nil {
		t.Fatal("ValidatorsForProfile() expected error for missing validator ID, got nil")
	}
}
