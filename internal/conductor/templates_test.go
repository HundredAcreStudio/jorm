package conductor

import (
	"testing"

	"github.com/jorm/internal/agent"
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
