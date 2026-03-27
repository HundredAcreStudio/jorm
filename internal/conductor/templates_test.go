package conductor

import (
	"testing"
	"time"

	"github.com/jorm/internal/agent"
	"github.com/jorm/internal/config"
	"github.com/jorm/internal/orchestrator"
)

func TestExtractSection_Found(t *testing.T) {
	text := `Some preamble

### Acceptance Criteria
AC1: Build passes
AC2: Tests pass

### Plan
Step 1: Do the thing
`
	got := extractSection(text, "Acceptance Criteria")
	want := "AC1: Build passes\nAC2: Tests pass"
	if got != want {
		t.Errorf("extractSection(Acceptance Criteria) = %q, want %q", got, want)
	}
}

func TestExtractSection_NotFound(t *testing.T) {
	text := "### Plan\nStep 1: Do the thing\n"
	got := extractSection(text, "Acceptance Criteria")
	if got != "" {
		t.Errorf("extractSection for missing heading = %q, want empty", got)
	}
}

func TestExtractSection_AtEndOfText(t *testing.T) {
	text := `### Plan
Step 1: Do the thing

### Acceptance Criteria
AC1: Build passes
AC2: Tests pass`
	got := extractSection(text, "Acceptance Criteria")
	want := "AC1: Build passes\nAC2: Tests pass"
	if got != want {
		t.Errorf("extractSection(at end) = %q, want %q", got, want)
	}
}

// TestBuildStagedTemplate covers AC4–AC7 and the plan's test cases:
// no profile, shell-reject, shell-warn, claude reviewers, explicit model, mixed profile.
func TestBuildStagedTemplate(t *testing.T) {
	t.Run("no profile falls back to builtin full-workflow", func(t *testing.T) {
		cfg := &config.Config{
			Model:    "sonnet",
			Profile:  "default",
			Profiles: map[string][]string{}, // "default" absent
		}
		tmpl, err := BuildStagedTemplate(cfg, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Builtin full-workflow has Planning + Test Writing + 3 review stages + Cleanup = 6 stages
		if len(tmpl.Stages) != 6 {
			t.Errorf("expected 6 builtin stages, got %d", len(tmpl.Stages))
		}
		if tmpl.TesterConfig.Command == "" {
			t.Error("expected non-empty tester command from builtin fallback")
		}
	})

	t.Run("shell reject validator populates tester command and timeout", func(t *testing.T) {
		const cmd = "CGO_ENABLED=1 go test -count=1 ./..."
		timeout := 5 * time.Minute
		cfg := &config.Config{
			Model: "sonnet",
			Validators: []config.ValidatorConfig{
				{ID: "tests", Type: "shell", Command: cmd, OnFail: "reject", RunOn: "always", Timeout: timeout},
			},
			Profiles: map[string][]string{
				"default": {"tests"},
			},
		}
		tmpl, err := BuildStagedTemplate(cfg, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tmpl.TesterConfig.Command != cmd {
			t.Errorf("TesterConfig.Command = %q, want %q", tmpl.TesterConfig.Command, cmd)
		}
		if tmpl.TesterConfig.Timeout != timeout {
			t.Errorf("TesterConfig.Timeout = %v, want %v", tmpl.TesterConfig.Timeout, timeout)
		}
		if tmpl.TesterConfig.OnFail != "reject" {
			t.Errorf("TesterConfig.OnFail = %q, want %q", tmpl.TesterConfig.OnFail, "reject")
		}
	})

	t.Run("shell warn validator produces StageKindAgent stage", func(t *testing.T) {
		cfg := &config.Config{
			Model: "sonnet",
			Validators: []config.ValidatorConfig{
				{ID: "vet", Type: "shell", Command: "go vet ./...", OnFail: "warn", RunOn: "always"},
			},
			Profiles: map[string][]string{
				"default": {"vet"},
			},
		}
		tmpl, err := BuildStagedTemplate(cfg, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Planning + Test Writing are always prepended; vet adds one StageKindAgent
		var warnStages []orchestrator.Stage
		for _, s := range tmpl.Stages {
			if s.Kind == orchestrator.StageKindAgent && s.AgentConfig != nil &&
				s.AgentConfig.ExecutionMode == "shell" {
				warnStages = append(warnStages, s)
			}
		}
		if len(warnStages) != 1 {
			t.Fatalf("expected 1 shell warn stage, got %d", len(warnStages))
		}
		if warnStages[0].AgentConfig.Command != "go vet ./..." {
			t.Errorf("warn stage command = %q", warnStages[0].AgentConfig.Command)
		}
		if warnStages[0].AgentConfig.OnFail != "warn" {
			t.Errorf("warn stage OnFail = %q, want %q", warnStages[0].AgentConfig.OnFail, "warn")
		}
	})

	t.Run("claude validators become StageKindReview stages in profile order", func(t *testing.T) {
		cfg := &config.Config{
			Model: "sonnet",
			Validators: []config.ValidatorConfig{
				{ID: "pr-review", Type: "claude", Prompt: "builtin:pr-review", OnFail: "reject", RunOn: "always"},
				{ID: "security", Type: "claude", Prompt: "builtin:security-review", OnFail: "reject", RunOn: "always"},
			},
			Profiles: map[string][]string{
				"default": {"pr-review", "security"},
			},
		}
		tmpl, err := BuildStagedTemplate(cfg, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var reviewStages []orchestrator.Stage
		for _, s := range tmpl.Stages {
			if s.Kind == orchestrator.StageKindReview {
				reviewStages = append(reviewStages, s)
			}
		}
		if len(reviewStages) != 2 {
			t.Fatalf("expected 2 review stages, got %d", len(reviewStages))
		}
		if reviewStages[0].ReviewerConfig.Prompt != "builtin:pr-review" {
			t.Errorf("first review stage prompt = %q, want %q", reviewStages[0].ReviewerConfig.Prompt, "builtin:pr-review")
		}
		if reviewStages[1].ReviewerConfig.Prompt != "builtin:security-review" {
			t.Errorf("second review stage prompt = %q, want %q", reviewStages[1].ReviewerConfig.Prompt, "builtin:security-review")
		}
	})

	t.Run("claude validator with explicit model uses that model", func(t *testing.T) {
		cfg := &config.Config{
			Model: "sonnet",
			Validators: []config.ValidatorConfig{
				{
					ID:     "pr-review",
					Type:   "claude",
					Prompt: "builtin:pr-review",
					OnFail: "reject",
					RunOn:  "always",
					Model:  "opus", // AC5: Model field on ValidatorConfig
				},
			},
			Profiles: map[string][]string{
				"default": {"pr-review"},
			},
		}
		tmpl, err := BuildStagedTemplate(cfg, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var reviewStages []orchestrator.Stage
		for _, s := range tmpl.Stages {
			if s.Kind == orchestrator.StageKindReview {
				reviewStages = append(reviewStages, s)
			}
		}
		if len(reviewStages) != 1 {
			t.Fatalf("expected 1 review stage, got %d", len(reviewStages))
		}
		if reviewStages[0].ReviewerConfig.Model != "opus" {
			t.Errorf("reviewer model = %q, want %q", reviewStages[0].ReviewerConfig.Model, "opus")
		}
	})

	t.Run("claude validator without model falls back to cfg.Model", func(t *testing.T) {
		cfg := &config.Config{
			Model: "opus",
			Validators: []config.ValidatorConfig{
				{ID: "pr-review", Type: "claude", Prompt: "builtin:pr-review", OnFail: "reject", RunOn: "always"},
			},
			Profiles: map[string][]string{
				"default": {"pr-review"},
			},
		}
		tmpl, err := BuildStagedTemplate(cfg, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, s := range tmpl.Stages {
			if s.Kind == orchestrator.StageKindReview {
				if s.ReviewerConfig.Model != "opus" {
					t.Errorf("reviewer model = %q, want %q (cfg.Model fallback)", s.ReviewerConfig.Model, "opus")
				}
			}
		}
	})

	t.Run("mixed profile: warn stage appears before review stages", func(t *testing.T) {
		cfg := &config.Config{
			Model: "sonnet",
			Validators: []config.ValidatorConfig{
				{ID: "vet", Type: "shell", Command: "go vet ./...", OnFail: "warn", RunOn: "always"},
				{ID: "pr-review", Type: "claude", Prompt: "builtin:pr-review", OnFail: "reject", RunOn: "always"},
			},
			Profiles: map[string][]string{
				"default": {"vet", "pr-review"},
			},
		}
		tmpl, err := BuildStagedTemplate(cfg, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Find indices of the warn shell stage and review stage
		warnIdx, reviewIdx := -1, -1
		for i, s := range tmpl.Stages {
			if s.Kind == orchestrator.StageKindAgent && s.AgentConfig != nil && s.AgentConfig.ExecutionMode == "shell" {
				warnIdx = i
			}
			if s.Kind == orchestrator.StageKindReview {
				reviewIdx = i
			}
		}
		if warnIdx == -1 {
			t.Fatal("warn shell stage not found")
		}
		if reviewIdx == -1 {
			t.Fatal("review stage not found")
		}
		if warnIdx > reviewIdx {
			t.Errorf("warn stage (idx %d) should come before review stage (idx %d)", warnIdx, reviewIdx)
		}
	})

	t.Run("accept_only validators are excluded from stages", func(t *testing.T) {
		cfg := &config.Config{
			Model: "sonnet",
			Validators: []config.ValidatorConfig{
				{ID: "commit", Type: "shell", Command: "git commit -a", OnFail: "reject", RunOn: "accept_only"},
				{ID: "tests", Type: "shell", Command: "go test ./...", OnFail: "reject", RunOn: "always"},
			},
			Profiles: map[string][]string{
				"default": {"commit", "tests"},
			},
		}
		tmpl, err := BuildStagedTemplate(cfg, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tmpl.TesterConfig.Command != "go test ./..." {
			t.Errorf("TesterConfig.Command = %q, expected 'go test ./...' (commit excluded)", tmpl.TesterConfig.Command)
		}
	})

	t.Run("stage order matches profile validator ordering for reviewers", func(t *testing.T) {
		cfg := &config.Config{
			Model: "sonnet",
			Validators: []config.ValidatorConfig{
				{ID: "security", Type: "claude", Prompt: "builtin:security-review", OnFail: "reject", RunOn: "always"},
				{ID: "pr-review", Type: "claude", Prompt: "builtin:pr-review", OnFail: "reject", RunOn: "always"},
				{ID: "tester", Type: "claude", Prompt: "builtin:tester-review", OnFail: "reject", RunOn: "always"},
			},
			Profiles: map[string][]string{
				// tester first, then security, then pr-review
				"default": {"tester", "security", "pr-review"},
			},
		}
		tmpl, err := BuildStagedTemplate(cfg, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var prompts []string
		for _, s := range tmpl.Stages {
			if s.Kind == orchestrator.StageKindReview {
				prompts = append(prompts, s.ReviewerConfig.Prompt)
			}
		}
		want := []string{"builtin:tester-review", "builtin:security-review", "builtin:pr-review"}
		if len(prompts) != len(want) {
			t.Fatalf("review stage count = %d, want %d", len(prompts), len(want))
		}
		for i, p := range prompts {
			if p != want[i] {
				t.Errorf("review stage[%d] prompt = %q, want %q", i, p, want[i])
			}
		}
	})

	t.Run("reviewer on_fail warn is propagated to reviewer config", func(t *testing.T) {
		cfg := &config.Config{
			Model: "sonnet",
			Validators: []config.ValidatorConfig{
				{ID: "pr-review", Type: "claude", Prompt: "builtin:pr-review", OnFail: "warn", RunOn: "always"},
			},
			Profiles: map[string][]string{
				"default": {"pr-review"},
			},
		}
		tmpl, err := BuildStagedTemplate(cfg, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, s := range tmpl.Stages {
			if s.Kind == orchestrator.StageKindReview {
				if s.ReviewerConfig.OnFail != "warn" {
					t.Errorf("reviewer OnFail = %q, want %q", s.ReviewerConfig.OnFail, "warn")
				}
			}
		}
	})
}

func TestPlannerResultProcessor(t *testing.T) {
	cfg := plannerAgent()
	if cfg.ResultProcessor == nil {
		t.Fatal("plannerAgent() should have a non-nil ResultProcessor")
	}

	t.Run("extracts criteria and plan", func(t *testing.T) {
		result := &agent.ClaudeResult{
			Text: `Some preamble text

### Acceptance Criteria
AC1: Build passes
AC2: Tests pass

### Plan
1. Do step one
2. Do step two
`,
		}
		data := cfg.ResultProcessor(result)
		criteria, ok := data["acceptance_criteria"].(string)
		if !ok || criteria == "" {
			t.Fatal("expected non-empty acceptance_criteria in data")
		}
		if criteria != "AC1: Build passes\nAC2: Tests pass" {
			t.Errorf("acceptance_criteria = %q", criteria)
		}
		plan, ok := data["plan"].(string)
		if !ok || plan == "" {
			t.Fatal("expected non-empty plan in data")
		}
		if plan != "1. Do step one\n2. Do step two" {
			t.Errorf("plan = %q", plan)
		}
	})

	t.Run("missing sections returns empty strings", func(t *testing.T) {
		result := &agent.ClaudeResult{
			Text: "Just some text with no markdown sections",
		}
		data := cfg.ResultProcessor(result)
		criteria, _ := data["acceptance_criteria"].(string)
		if criteria != "" {
			t.Errorf("expected empty acceptance_criteria, got %q", criteria)
		}
		plan, _ := data["plan"].(string)
		if plan != "" {
			t.Errorf("expected empty plan, got %q", plan)
		}
	})

	t.Run("nil result returns nil", func(t *testing.T) {
		data := cfg.ResultProcessor(nil)
		if data != nil {
			t.Errorf("expected nil data for nil result, got %v", data)
		}
	})
}
