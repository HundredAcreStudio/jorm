package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MaxAttempts   int                 `yaml:"max_attempts"`
	Model         string              `yaml:"model"`
	Profile       string              `yaml:"profile"`
	Validators    []ValidatorConfig   `yaml:"validators"`
	Profiles      map[string][]string `yaml:"profiles"`
	Cluster       ClusterConfig       `yaml:"cluster"`
	IssueProvider IssueProviderConfig `yaml:"issue_provider"`
	Hooks         HooksConfig         `yaml:"hooks"`
}

type ValidatorConfig struct {
	ID       string        `yaml:"id"`
	Name     string        `yaml:"name"`
	Type     string        `yaml:"type"`     // "shell" or "claude"
	Command  string        `yaml:"command"`  // for shell validators
	Prompt   string        `yaml:"prompt"`   // for claude validators
	OnFail   string        `yaml:"on_fail"`  // "reject", "warn", "ignore"
	RunOn    string        `yaml:"run_on"`   // "always", "accept_only", "reject_only"
	Parallel bool          `yaml:"parallel"`
	Timeout  time.Duration `yaml:"timeout"`
}

type ClusterConfig struct {
	MaxParallel int `yaml:"max_parallel"`
}

type IssueProviderConfig struct {
	Type string `yaml:"type"` // "github" or "linear"
}

type HooksConfig struct {
	OnComplete []string `yaml:"on_complete"`
	OnFailure  []string `yaml:"on_failure"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	applyDefaults(cfg)
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.Model == "" {
		cfg.Model = "sonnet"
	}
	if cfg.Profile == "" {
		cfg.Profile = "default"
	}
	for i := range cfg.Validators {
		if cfg.Validators[i].OnFail == "" {
			cfg.Validators[i].OnFail = "reject"
		}
		if cfg.Validators[i].RunOn == "" {
			cfg.Validators[i].RunOn = "always"
		}
	}
}

func (c *Config) ValidatorsForProfile(profile string) ([]ValidatorConfig, error) {
	ids, ok := c.Profiles[profile]
	if !ok {
		return nil, fmt.Errorf("profile %q not found", profile)
	}

	lookup := make(map[string]ValidatorConfig, len(c.Validators))
	for _, v := range c.Validators {
		lookup[v.ID] = v
	}

	var result []ValidatorConfig
	for _, id := range ids {
		v, ok := lookup[id]
		if !ok {
			return nil, fmt.Errorf("validator %q referenced in profile %q not found", id, profile)
		}
		result = append(result, v)
	}
	return result, nil
}
