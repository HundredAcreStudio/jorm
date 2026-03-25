package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MaxAttempts   int                        `yaml:"max_attempts"`
	Model         string                     `yaml:"model"`
	Profile       string                     `yaml:"profile"`
	Validators    []ValidatorConfig          `yaml:"validators"`
	Profiles      map[string][]string        `yaml:"profiles"`
	Cluster       ClusterConfig              `yaml:"cluster"`
	Conductor     ConductorConfig            `yaml:"conductor"`
	IssueProvider string                     `yaml:"issue_provider"` // "github", "linear", "jira"
	Providers     map[string]ProviderConfig  `yaml:"providers"`
	Hooks         HooksConfig                `yaml:"hooks"`
	Env           map[string]string          `yaml:"env"` // extra env vars injected into all subprocesses
}

type ConductorConfig struct {
	Enabled       bool   `yaml:"enabled"`
	ClassifyModel string `yaml:"classify_model"` // default: "haiku"
	Staged        bool   `yaml:"staged"`          // use stage orchestrator
}

type ProviderConfig struct {
	TokenVar string `yaml:"token_var"` // env var name containing the token
	Token    string `yaml:"token"`     // hardcoded token (token_var takes precedence)
}

// ResolveToken returns the token for a provider, checking token_var first, then token.
func (p ProviderConfig) ResolveToken() string {
	if p.TokenVar != "" {
		if t := os.Getenv(p.TokenVar); t != "" {
			return t
		}
	}
	return p.Token
}

type ValidatorConfig struct {
	ID       string        `yaml:"id"`
	Name     string        `yaml:"name"`
	Type     string        `yaml:"type"`     // "shell" or "claude"
	Mode     string        `yaml:"mode"`     // "review" (default) or "action"
	Command  string        `yaml:"command"`  // for shell validators
	Prompt   string        `yaml:"prompt"`   // for claude validators (supports "builtin:commit" etc.)
	OnFail   string        `yaml:"on_fail"`  // "reject", "warn", "ignore"
	RunOn    string        `yaml:"run_on"`   // "always", "accept_only", "reject_only"
	Parallel bool          `yaml:"parallel"`
	Timeout  time.Duration `yaml:"timeout"`
}

type ClusterConfig struct {
	MaxParallel int `yaml:"max_parallel"`
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
	// MaxAttempts 0 means unlimited (YAML unmarshals missing int as 0).
	if cfg.Model == "" {
		cfg.Model = "sonnet"
	}
	if cfg.Profile == "" {
		cfg.Profile = "default"
	}
	if cfg.IssueProvider == "" {
		cfg.IssueProvider = "github"
	}
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]ProviderConfig)
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

// ProviderToken resolves the auth token for the given provider name.
func (c *Config) ProviderToken(name string) string {
	if p, ok := c.Providers[name]; ok {
		return p.ResolveToken()
	}
	return ""
}

// SubprocessEnv returns os.Environ() with config-resolved env vars appended.
// Provider tokens are injected so tools like gh CLI pick them up automatically.
func (c *Config) SubprocessEnv() []string {
	env := os.Environ()

	// Inject explicit env vars from config
	for k, v := range c.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Inject provider tokens into well-known env vars
	if token := c.ProviderToken("github"); token != "" {
		env = append(env, "GITHUB_TOKEN="+token)
		env = append(env, "GH_TOKEN="+token)
	}
	if token := c.ProviderToken("linear"); token != "" {
		env = append(env, "LINEAR_API_KEY="+token)
	}
	if token := c.ProviderToken("jira"); token != "" {
		env = append(env, "JIRA_API_TOKEN="+token)
	}

	return env
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
