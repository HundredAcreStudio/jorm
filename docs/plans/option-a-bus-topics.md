# Option A: New Bus Topics Per Stage

Replaces the current flat fan-out validation model with a sequential staged pipeline. Each stage has its own bus topic pair, enabling the orchestrator to advance through stages one at a time, with worker retry loops scoped to each stage's feedback.

## Pipeline

```
Stage 0: Planner → Test Writer
Stage 1: [Worker → Tester]*
Stage 2: Test Reviewer → [Worker → Tester]*
Stage 3: PR Reviewer → [Worker → Tester]*
Stage 4: Security Reviewer → [Worker → Tester]*
```

---

## 1. New Bus Topics

**File: `internal/bus/bus.go`**

```go
const (
    // Existing (keep as-is)
    TopicIssueOpened       = "ISSUE_OPENED"
    TopicPlanReady         = "PLAN_READY"
    TopicClusterComplete   = "CLUSTER_COMPLETE"
    TopicClusterOperations = "CLUSTER_OPERATIONS"

    // Keep name, new semantics
    TopicImplementationReady = "IMPLEMENTATION_READY"

    // New
    TopicTestsReady  = "TESTS_READY"   // test writer finished
    TopicTestResult  = "TEST_RESULT"   // tester pass/fail
    TopicStageResult = "STAGE_RESULT"  // reviewer verdict (carries stage_index)
    TopicStageAdvance = "STAGE_ADVANCE" // stage controller → trigger next reviewer
)
```

**Message flow:**

```
ISSUE_OPENED
  → Planner → PLAN_READY
    → Test Writer → TESTS_READY
      → Worker → IMPLEMENTATION_READY
        → Tester → TEST_RESULT
          if rejected → Worker retries (stage 1 inner loop)
          if approved → Stage Controller → STAGE_ADVANCE {stage: 0}
            → Test Reviewer → STAGE_RESULT
              if rejected → Worker → Tester → ... (inner loop)
              if approved → Stage Controller → STAGE_ADVANCE {stage: 1}
                → PR Reviewer → STAGE_RESULT
                  ... same pattern ...
                    → Security Reviewer → STAGE_RESULT
                      if approved → Stage Controller → CLUSTER_COMPLETE
```

---

## 2. Template Changes

**File: `internal/conductor/templates.go`**

Add `"staged-workflow"` template. Existing templates stay for backward compat.

```go
"staged-workflow": {
    plannerAgent(),
    testWriterAgent(),
    stagedWorkerAgent("sonnet", 5),
    testerAgent(),
    // Reviewers injected dynamically from config via injectStagedValidators()
    // Stage controller also injected there
},
```

New agent factories:

```go
func testWriterAgent() orchestrator.AgentConfig {
    return orchestrator.AgentConfig{
        ID:             "test-writer",
        Name:           "Test Writer",
        Role:           "test-writer",
        Triggers:       []orchestrator.Trigger{{Topic: bus.TopicPlanReady, Predicate: "always"}},
        Prompt:         "builtin:test-writer",
        Model:          "sonnet",
        MaxIterations:  1,
        OnComplete:     []orchestrator.OnCompleteAction{{Topic: bus.TopicTestsReady}},
        ContextBuilder: orchestrator.BuildTestWriterContext,
    }
}

func stagedWorkerAgent(model string, maxIter int) orchestrator.AgentConfig {
    return orchestrator.AgentConfig{
        ID:   "worker",
        Name: "Worker",
        Role: "worker",
        Triggers: []orchestrator.Trigger{
            {Topic: bus.TopicTestsReady, Predicate: "always"},
            {Topic: bus.TopicStageResult, Predicate: "rejected"},
            {Topic: bus.TopicTestResult, Predicate: "rejected"},
        },
        Prompt:         "builtin:worker",
        Model:          model,
        MaxIterations:  maxIter,
        OnComplete:     []orchestrator.OnCompleteAction{{Topic: bus.TopicImplementationReady}},
        ContextBuilder: orchestrator.BuildStagedWorkerContext,
    }
}

func testerAgent() orchestrator.AgentConfig {
    return orchestrator.AgentConfig{
        ID:            "tester",
        Name:          "Tester",
        Role:          "tester",
        ExecutionMode: "shell",
        Command:       "go test ./...", // replaced by config value in injectStagedValidators
        Triggers:      []orchestrator.Trigger{{Topic: bus.TopicImplementationReady, Predicate: "always"}},
        MaxIterations: 0,
        OnComplete:    []orchestrator.OnCompleteAction{{Topic: bus.TopicTestResult}},
    }
}
```

---

## 3. Stage Controller (Passthrough Agent)

**New file: `internal/orchestrator/stages.go`**

A `StageController` is injected as a passthrough agent. It tracks the current stage and routes messages.

```go
type Stage struct {
    Index       int
    ValidatorID string
    Name        string
}

type StageController struct {
    stages       []Stage
    currentStage int
    mu           sync.Mutex
}
```

The controller listens on `{TopicTestResult:approved, TopicStageResult:approved}` and publishes either `TopicStageAdvance` (to trigger the next reviewer) or `TopicClusterComplete` (when all done).

Uses the closure-captures-bus pattern (same as existing completion agent in `injectValidators`):

```go
func (sc *StageController) Process(msg bus.Message, b *bus.Bus, clusterID string) (map[string]any, bool) {
    sc.mu.Lock()
    defer sc.mu.Unlock()

    if msg.Topic == bus.TopicTestResult {
        // Tests passed. Trigger current stage's reviewer.
        if sc.currentStage >= len(sc.stages) {
            return map[string]any{"approved": true}, true // → CLUSTER_COMPLETE via OnComplete
        }
        b.Publish(bus.Message{
            ClusterID: clusterID,
            Topic:     bus.TopicStageAdvance,
            Sender:    "stage-controller",
            Data: map[string]any{
                "stage_index":      sc.currentStage,
                "trigger_reviewer": sc.stages[sc.currentStage].ValidatorID,
            },
        })
        return nil, false
    }

    if msg.Topic == bus.TopicStageResult {
        sc.currentStage++
        if sc.currentStage >= len(sc.stages) {
            return map[string]any{"approved": true}, true // → CLUSTER_COMPLETE
        }
        b.Publish(bus.Message{
            ClusterID: clusterID,
            Topic:     bus.TopicStageAdvance,
            Sender:    "stage-controller",
            Data: map[string]any{
                "stage_index":      sc.currentStage,
                "trigger_reviewer": sc.stages[sc.currentStage].ValidatorID,
            },
        })
        return nil, false
    }
    return nil, false
}
```

OnComplete for the agent: `[]OnCompleteAction{{Topic: bus.TopicClusterComplete}}`.

---

## 4. Reviewer Triggering — "self" Predicate

Each reviewer triggers on `TopicStageAdvance` but only when `trigger_reviewer` matches its own ID.

**File: `internal/orchestrator/agent.go`**

Update `evaluatePredicate` to accept `agentID` and add `"self"` case:

```go
func evaluatePredicate(predicate string, msg bus.Message, agentID string) bool {
    switch strings.ToLower(predicate) {
    // ... existing cases ...
    case "self":
        id, _ := msg.Data["trigger_reviewer"].(string)
        return id == agentID
    }
}
```

Each reviewer agent config:

```go
AgentConfig{
    ID:         "reviewer-pr-review",
    Triggers:   []Trigger{{Topic: bus.TopicStageAdvance, Predicate: "self"}},
    OnComplete: []OnCompleteAction{{Topic: bus.TopicStageResult}},
    // ...
}
```

---

## 5. Context Builders

**File: `internal/orchestrator/context.go`**

### BuildTestWriterContext

Issue + plan. Used by test writer to write tests before implementation.

```go
func BuildTestWriterContext(b *bus.Bus, clusterID string) (string, error) {
    // Query ISSUE_OPENED + PLAN_READY, join as sections
}
```

### BuildStagedWorkerContext

Issue + plan + **most recent rejection only** (stage-scoped). Unlike `BuildWorkerContext` which queries ALL `VALIDATION_RESULT` messages, this finds only the most recent `STAGE_RESULT` or `TEST_RESULT` rejection.

```go
func BuildStagedWorkerContext(b *bus.Bus, clusterID string) (string, error) {
    // Issue + Plan (same as BuildWorkerContext)
    // + most recent rejection from STAGE_RESULT or TEST_RESULT (whichever is newer)
    // Scoped to current stage via timestamp ordering
}
```

---

## 6. Config Changes

**File: `internal/config/config.go`**

Add fields to `ValidatorConfig`:

```go
Stage    int  `yaml:"stage"`     // stage order (0=tester, 1+=reviewers)
IsTester bool `yaml:"is_tester"` // marks this as the test runner
```

Example `.jorm.yaml`:

```yaml
validators:
  - id: tests
    name: Tests
    type: shell
    command: "go test ./..."
    is_tester: true

  - id: test-review
    name: Test Reviewer
    type: claude
    prompt: "builtin:tester-review"
    stage: 2

  - id: pr-review
    name: PR Reviewer
    type: claude
    prompt: "builtin:pr-review"
    stage: 3

  - id: security-review
    name: Security Reviewer
    type: claude
    prompt: "builtin:security-review"
    stage: 4

profiles:
  default: [tests, test-review, pr-review, security-review]
```

---

## 7. Events/Sink Changes

**File: `internal/events/events.go`**

Add stage events:

```go
StageAdvance(stageIndex int, reviewerName string)
StageReviewStart(stageIndex int, reviewerName string)
StageReviewResult(stageIndex int, reviewerName string, approved bool)
```

---

## 8. Modified Files Summary

| File | Change |
|------|--------|
| `internal/bus/bus.go` | Add 4 new topic constants |
| `internal/config/config.go` | Add `Stage`, `IsTester` fields to `ValidatorConfig` |
| `internal/orchestrator/agent.go` | Add `agentID` param to `evaluatePredicate`, add `"self"` case |
| `internal/orchestrator/context.go` | Add `BuildTestWriterContext`, `BuildStagedWorkerContext` |
| `internal/orchestrator/orchestrator.go` | Add `injectStagedValidators` method, detect staged template in `Run` |
| `internal/conductor/templates.go` | Add `"staged-workflow"` template with new agent factories |
| `internal/events/events.go` | Add 3 stage event methods to `Sink` interface |

**New files:**

| File | Purpose |
|------|---------|
| `internal/orchestrator/stages.go` | `StageController` struct and `Process` method |
| `internal/agent/prompts/test-writer.md` | Built-in prompt for test writer agent |

---

## 9. Potential Challenges

**Worker trigger disambiguation**: Worker listens on both `TopicStageResult:rejected` and `TopicTestResult:rejected`. If a reviewer rejects, worker fixes, then tester fails, worker could see two triggers quickly. Handled naturally — worker processes triggers sequentially.

**Stage reset on rejection**: When a reviewer rejects, `currentStage` does NOT change. When tests pass again (`TopicTestResult:approved`), the controller re-triggers the same reviewer.

**MaxIterations across stages**: Worker's `MaxIterations` applies globally across all stages. May need per-stage limits in future.

**Breaking change**: `evaluatePredicate` signature adds `agentID` parameter. Internal-only, single call site.

---

## 10. Implementation Sequence

1. `bus.go` — Add new topic constants
2. `config.go` — Add `Stage` and `IsTester` fields
3. `agent.go` — Update `evaluatePredicate` for `"self"` predicate
4. `context.go` — Add new context builders
5. `stages.go` (new) — Implement `StageController`
6. `orchestrator.go` — Add `injectStagedValidators`
7. `templates.go` — Add `"staged-workflow"` template
8. `events.go` — Add stage events
9. `test-writer.md` (new) — Write prompt
10. Tests — Unit tests for `StageController.Process`, `BuildStagedWorkerContext`, `evaluatePredicate`
