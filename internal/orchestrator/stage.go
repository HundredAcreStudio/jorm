package orchestrator

// StageKind identifies what kind of work a stage performs.
type StageKind string

const (
	StageKindAgent  StageKind = "agent"  // Run a Claude agent once (planner, test-writer)
	StageKindReview StageKind = "review" // Reviewer → [Worker → Tester]* loop
)

// Stage describes a single unit of work within a StageOrchestrator pipeline.
type Stage struct {
	Name           string
	Kind           StageKind
	AgentConfig    *AgentConfig // For StageKindAgent: the agent to run once
	ReviewerConfig *AgentConfig // For StageKindReview: the Claude reviewer
	MaxRetries     int          // Max worker→tester→reviewer cycles (0 = unlimited)
}
