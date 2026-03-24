# Option B: Stage Orchestrator Layer

Introduces a **Stage** abstraction as a first-class concept layered on top of the existing bus and agent system. Instead of encoding sequential review stages through bus topic wiring, a new `StageOrchestrator` drives an ordered list of stages, each containing its own inner retry loop. The bus remains for message persistence and context queries but is no longer the control-flow mechanism for stage sequencing.

## Pipeline

```
Stage 0: Planner → Test Writer
Stage 1: [Worker → Tester]*
Stage 2: Test Reviewer → [Worker → Tester]*
Stage 3: PR Reviewer → [Worker → Tester]*
Stage 4: Security Reviewer → [Worker → Tester]*
```

---

## 1. Stage Data Structure

**New file: `internal/orchestrator/stage.go`**

```go
type StageKind string

const (
    StageKindAgent  StageKind = "agent"  // Run a Claude agent once (planner, test-writer)
    StageKindReview StageKind = "review" // Reviewer → [Worker → Tester]* loop
)

type Stage struct {
    Name           string
    Kind           StageKind
    AgentConfig    *AgentConfig  // For StageKindAgent: the agent to run once
    ReviewerConfig *AgentConfig  // For StageKindReview: the Claude reviewer
    MaxRetries     int           // Max worker→tester→reviewer cycles (0 = unlimited)
}
```

Worker and tester configs are **shared** across all review stages, defined once at the `StageOrchestrator` level. One worker, one tester, multiple reviewers in sequence.

---

## 2. Stage Orchestrator

**New file: `internal/orchestrator/stages.go`**

```go
type StageOrchestrator struct {
    bus       *bus.Bus
    sink      events.Sink
    cfg       *config.Config
    env       []string
    clusterID string
    workDir   string
    repoDir   string

    workerConfig AgentConfig
    testerConfig AgentConfig
    stages       []Stage
}

func (so *StageOrchestrator) Run(ctx context.Context, iss *issue.Issue) error {
    // Publish ISSUE_OPENED to seed the bus
    so.bus.Publish(bus.Message{
        ClusterID: so.clusterID,
        Topic:     bus.TopicIssueOpened,
        Sender:    "stage-orchestrator",
        Content:   fmt.Sprintf("# %s\n\n%s", iss.Title, iss.Body),
        Data:      map[string]any{"issue_id": iss.ID, "issue_title": iss.Title},
    })

    for i, stage := range so.stages {
        so.sink.Phase(fmt.Sprintf("Stage %d: %s", i, stage.Name))

        var err error
        switch stage.Kind {
        case StageKindAgent:
            err = so.runAgentStage(ctx, stage)
        case StageKindReview:
            err = so.runReviewStage(ctx, i, stage)
        }
        if err != nil {
            return fmt.Errorf("stage %d (%s): %w", i, stage.Name, err)
        }
    }

    // All stages passed
    so.bus.Publish(bus.Message{
        ClusterID: so.clusterID,
        Topic:     bus.TopicClusterComplete,
        Sender:    "stage-orchestrator",
        Data:      map[string]any{"approved": true},
    })
    so.sink.ClusterComplete(so.clusterID, "all_stages_passed")
    return nil
}
```

---

## 3. Interaction with Existing Bus

The bus is **retained but demoted from control plane to data plane**.

**What the bus still does:**
- Persists all messages to SQLite for audit trail and context queries
- Context builders query the bus to assemble prompts
- `ISSUE_OPENED`, `PLAN_READY`, `IMPLEMENTATION_READY` topics remain for context builders

**What the bus no longer does (in staged mode):**
- No pub/sub-driven trigger loops. Control flow is an imperative `for` loop.
- No completion agent with `sync.Map` approval tracking.
- `VALIDATION_RESULT` becomes a data sink only — no agent subscribes to it.

**New bus topics** (purely for audit/observability):

```go
const (
    TopicStageStarted   = "STAGE_STARTED"
    TopicStageCompleted = "STAGE_COMPLETED"
    TopicTestsReady     = "TESTS_READY"
)
```

**Backward compatibility:** The existing `Orchestrator.Run` and its bus-driven approach remain intact. `StageOrchestrator` is a separate code path selected by config (`conductor.staged: true`).

---

## 4. Template Changes

**File: `internal/conductor/templates.go`**

New type and function alongside existing templates:

```go
type StagedTemplate struct {
    WorkerConfig AgentConfig
    TesterConfig AgentConfig
    Stages       []orchestrator.Stage
}

func BuiltinStagedTemplates() map[string]StagedTemplate {
    return map[string]StagedTemplate{
        "full-workflow": {
            WorkerConfig: workerAgent("sonnet", 5, nil),
            TesterConfig: testerAgentConfig(),
            Stages: []orchestrator.Stage{
                {
                    Name:        "Planning",
                    Kind:        orchestrator.StageKindAgent,
                    AgentConfig: ptr(plannerAgent()),
                },
                {
                    Name:        "Test Writing",
                    Kind:        orchestrator.StageKindAgent,
                    AgentConfig: ptr(testWriterAgent()),
                },
                {
                    Name:           "Test Review",
                    Kind:           orchestrator.StageKindReview,
                    ReviewerConfig: ptr(testReviewAgent()),
                    MaxRetries:     3,
                },
                {
                    Name:           "PR Review",
                    Kind:           orchestrator.StageKindReview,
                    ReviewerConfig: ptr(prReviewAgent()),
                    MaxRetries:     3,
                },
                {
                    Name:           "Security Review",
                    Kind:           orchestrator.StageKindReview,
                    ReviewerConfig: ptr(securityReviewAgent()),
                    MaxRetries:     3,
                },
            },
        },
    }
}

func testWriterAgent() orchestrator.AgentConfig {
    return orchestrator.AgentConfig{
        ID:             "test-writer",
        Name:           "Test Writer",
        Role:           "test-writer",
        Prompt:         "builtin:test-writer",
        Model:          "sonnet",
        MaxIterations:  1,
        OnComplete:     []orchestrator.OnCompleteAction{{Topic: bus.TopicTestsReady}},
        ContextBuilder: orchestrator.BuildTestWriterContext,
    }
}
```

---

## 5. Inner Retry Loop

The core of the design. Each review stage runs a synchronous retry loop:

```go
func (so *StageOrchestrator) runReviewStage(ctx context.Context, stageIdx int, stage Stage) error {
    for attempt := 0; stage.MaxRetries == 0 || attempt < stage.MaxRetries; attempt++ {
        // Step 1: Run reviewer against current diff
        reviewResult, err := so.runReviewer(ctx, stage)
        if err != nil {
            return fmt.Errorf("reviewer error: %w", err)
        }

        // Publish to bus for context builder
        so.bus.Publish(bus.Message{
            ClusterID: so.clusterID,
            Topic:     bus.TopicValidationResult,
            Sender:    stage.ReviewerConfig.ID,
            Content:   reviewResult.Output,
            Data: map[string]any{
                "approved":     reviewResult.Approved,
                "validator_id": stage.ReviewerConfig.ID,
                "stage_index":  stageIdx,
            },
        })

        // Step 2: If approved, advance
        if reviewResult.Approved {
            return nil
        }

        // Step 3: Rejected — worker fixes with scoped feedback
        if err := so.runWorkerFix(ctx, stageIdx, stage, reviewResult); err != nil {
            return fmt.Errorf("worker fix error: %w", err)
        }

        // Step 4: Run tester
        testerResult, err := so.runTester(ctx)
        if err != nil {
            return fmt.Errorf("tester error: %w", err)
        }

        if !testerResult.Approved {
            // Tests failed — worker fixes test failures before re-review
            so.bus.Publish(bus.Message{
                ClusterID: so.clusterID,
                Topic:     bus.TopicValidationResult,
                Sender:    "tester",
                Content:   testerResult.Output,
                Data:      map[string]any{"approved": false, "validator_id": "tester", "stage_index": stageIdx},
            })
            if err := so.runWorkerTestFix(ctx, testerResult); err != nil {
                return fmt.Errorf("worker test-fix error: %w", err)
            }
        }

        // Publish fresh IMPLEMENTATION_READY for reviewer's context builder
        diff, _ := so.worktree.Diff()
        so.bus.Publish(bus.Message{
            ClusterID: so.clusterID,
            Topic:     bus.TopicImplementationReady,
            Sender:    "worker",
            Content:   diff,
        })

        // Loop: same reviewer re-reviews
    }

    return fmt.Errorf("stage %q exceeded max retries (%d)", stage.Name, stage.MaxRetries)
}
```

Key properties:
- **Synchronous and imperative.** No goroutines, no channel multiplexing.
- Test failures within a review stage trigger an additional worker fix cycle before re-review.
- Each iteration publishes `IMPLEMENTATION_READY` with fresh diff for the reviewer.

---

## 6. Context Builder Changes

**File: `internal/orchestrator/context.go`**

### BuildTestWriterContext

```go
func BuildTestWriterContext(b *bus.Bus, clusterID string) (string, error) {
    // Issue + Plan sections. Used by test writer to write tests before implementation.
}
```

### BuildStageScopedWorkerContext

Filters `VALIDATION_RESULT` messages by `stage_index` so the worker only sees the current stage's reviewer feedback:

```go
func BuildStageScopedWorkerContext(b *bus.Bus, clusterID string, stageIndex int, stageName string) (string, error) {
    // Issue + Plan (same as BuildWorkerContext)
    // + rejections filtered by stage_index in Data
    // Previous stages' feedback (already addressed and accepted) is excluded
}
```

**Note:** Current `bus.Query` doesn't filter by Data fields. The builder queries all `VALIDATION_RESULT` messages and filters in-memory. Acceptable — tens of messages per run, not thousands.

---

## 7. Config Changes

**File: `internal/config/config.go`**

Minimal change — add `Staged` to conductor config:

```go
type ConductorConfig struct {
    Enabled       bool   `yaml:"enabled"`
    ClassifyModel string `yaml:"classify_model"`
    Staged        bool   `yaml:"staged"`  // NEW: use stage orchestrator
}
```

**No changes to ValidatorConfig needed.** Stages are derived from the profile's validator list order:
- Shell validators with `on_fail: reject` → shared tester
- Claude validators with `mode: review` → sequential reviewer stages (order = profile list order)

Example `.jorm.yaml`:

```yaml
conductor:
  enabled: true
  staged: true

validators:
  - id: tests
    name: Tests
    type: shell
    command: "go test ./..."
    on_fail: reject

  - id: test-review
    name: Test Reviewer
    type: claude
    prompt: "builtin:tester-review"

  - id: pr-review
    name: PR Reviewer
    type: claude
    prompt: "builtin:pr-review"

  - id: security-review
    name: Security Reviewer
    type: claude
    prompt: "builtin:security-review"

profiles:
  default: [tests, test-review, pr-review, security-review]
```

---

## 8. Completion Logic

Handled directly by `StageOrchestrator.Run` — no completion agent needed:

1. Each `runReviewStage` returns `nil` on approval, error on max retries or cancellation
2. When the `for` loop completes without error → publish `CLUSTER_COMPLETE`, return nil
3. If any stage errors → return error, caller handles failure hooks

The `sync.Map`-based completion agent is **not used** in staged mode. Sequential execution makes fan-in tracking unnecessary.

---

## 9. Agent.ExecuteOnce

**File: `internal/orchestrator/agent.go`**

Extract the execution core from `Agent.Run` into `ExecuteOnce` for synchronous single-execution:

```go
// ExecuteOnce runs the agent's execution cycle exactly once (no trigger loop).
// Used by StageOrchestrator to run agents synchronously.
func (a *Agent) ExecuteOnce(ctx context.Context) (*AgentResult, error) {
    a.setState(StateExecuting)
    a.Iteration++
    // Existing execution body: shell/claude/passthrough handling
    // Returns structured result instead of publishing to bus
}
```

Straightforward extract-method refactoring. `Run` calls `ExecuteOnce` internally in its trigger loop.

---

## 10. Migration Path

### New files

| File | Purpose |
|------|---------|
| `internal/orchestrator/stage.go` | `Stage`, `StageKind`, `StageResult` types |
| `internal/orchestrator/stages.go` | `StageOrchestrator` with `Run`, `runAgentStage`, `runReviewStage`, `runWorkerFix`, `runTester`, `runReviewer` |
| `internal/agent/prompts/test-writer.md` | Prompt for test-writer agent |

### Modified files

| File | Change |
|------|--------|
| `internal/orchestrator/agent.go` | Extract `ExecuteOnce` method from `Run` loop body |
| `internal/orchestrator/context.go` | Add `BuildStageScopedWorkerContext` and `BuildTestWriterContext` |
| `internal/conductor/templates.go` | Add `StagedTemplate` type, `BuiltinStagedTemplates()`, `testWriterAgent()` |
| `internal/bus/bus.go` | Add `TopicStageStarted`, `TopicStageCompleted`, `TopicTestsReady` constants |
| `internal/config/config.go` | Add `Staged bool` to `ConductorConfig` |
| `internal/loop/loop.go` | Branch on `cfg.Conductor.Staged` to use `StageOrchestrator` |
| `internal/events/events.go` | Add `StageStarted`, `StageCompleted` to `Sink` interface |

### Unchanged files

| File | Why |
|------|-----|
| `internal/orchestrator/orchestrator.go` | Existing `Orchestrator` remains for non-staged workflows |
| `internal/store/store.go` | No schema changes |
| `internal/agent/agent.go` | `RunClaude` called unchanged |

### Integration in loop.go

```go
func runConductorMode(ctx context.Context, ...) error {
    if cfg.Conductor.Staged {
        tmpl := conductor.BuiltinStagedTemplates()[templateName]
        so := orchestrator.NewStageOrchestrator(
            msgBus, cfg, wt, sink, subEnv,
            runState.ID, tmpl.WorkerConfig, tmpl.TesterConfig, tmpl.Stages,
        )
        return so.Run(ctx, iss)
    }
    // Existing non-staged path unchanged
}
```

---

## 11. Trade-offs vs Option A

| Dimension | Option A (Bus topics) | Option B (Stage orchestrator) |
|-----------|----------------------|-------------------------------|
| **Control flow** | Implicit — trace topic subscriptions to understand order | Explicit — `for` loop over stages, readable top to bottom |
| **Inner retry loop** | Complex topic wiring: reviewer → per-stage retry topic → worker → completion tracking | Simple `for` loop with `if approved { return nil }` |
| **Debugging** | Hard to diagnose why a stage didn't advance — inspect bus message history | Stack trace points to exact stage and retry iteration |
| **Bus complexity** | Adds ~4 new topics + per-stage completion tracking | Adds 3 topics (audit only) |
| **Concurrency** | All agents as goroutines, natural fit for current arch | Sequential within stages; simpler but no intra-stage parallelism |
| **Flexibility** | More flexible for future DAGs, conditional branches | Linear pipeline only |
| **Code reuse** | Reuses `Agent.Run` trigger loop entirely | Requires `ExecuteOnce` extraction |
| **Testability** | Hard to test bus-driven workflows (simulate pub/sub timing) | `runReviewStage` is a pure function, easy to test |
| **Message ordering bugs** | High risk — buffered channels can lose messages under load | None — imperative control flow |
| **Backward compat** | Must ensure new topics don't interfere with non-staged workflows | Clean separation — different code paths entirely |

**Option B is better for:** correctness, debuggability, testability, simplicity of retry loops, lower concurrency risk.

**Option B is worse for:** future non-linear workflows, intra-stage parallelism (not needed for current requirements).
