# Plan: Conductor/Orchestrator Pattern for jorm

## Context

jorm currently runs a single-worker loop: fetch issue → run Claude → validate → retry. This works but doesn't leverage multi-agent collaboration. Zeroshot uses a conductor pattern where a classifier routes issues to workflow templates, a planner creates acceptance criteria, workers implement, and validators check against those criteria — all communicating through a message bus.

This plan adds that pattern to jorm while keeping the existing simple mode as the default.

## Design Decisions

- **Conductor**: LLM-based classification using Claude haiku from the start
- **Planner**: Outputs step-by-step plan + acceptance criteria that validators check against
- **Triggers**: Named predicate strings (`always`, `approved`, `rejected`) — no expression engine
- **Backward compat**: `conductor.enabled: false` (default) preserves current behavior

## Implementation Phases

### Phase 1: Message Bus (`internal/bus/`)

Add SQLite-backed pub/sub for agent communication.

**New file: `internal/bus/bus.go`**

```go
type Message struct {
    ID        string
    ClusterID string
    Topic     string  // ISSUE_OPENED, PLAN_READY, IMPLEMENTATION_READY, VALIDATION_RESULT, CLUSTER_COMPLETE
    Sender    string  // agent ID
    Timestamp time.Time
    Content   string  // free-form text
    Data      map[string]any // structured data (JSON in DB)
}

type Bus struct {
    db          *sql.DB
    mu          sync.Mutex
    subscribers map[string][]chan Message
}
```

Methods: `Publish()`, `Subscribe(topic)`, `Unsubscribe()`, `Query(clusterID, QueryOpts)`, `FindLast(clusterID, topic)`

**Modify: `internal/store/store.go`**
- Add `messages` table to `migrate()`
- Add `DB() *sql.DB` accessor so bus can share the connection
- Enable WAL mode for concurrent reads

Schema:
```sql
CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    cluster_id TEXT NOT NULL,
    topic TEXT NOT NULL,
    sender TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    content TEXT NOT NULL DEFAULT '',
    data TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_messages_cluster_topic ON messages(cluster_id, topic, timestamp);
```

### Phase 2: Agent Abstraction (`internal/orchestrator/`)

**New file: `internal/orchestrator/agent.go`**

```go
type AgentState string // "idle", "evaluating", "building_context", "executing"

type Trigger struct {
    Topic     string
    Predicate string // "always", "approved", "rejected"
}

type AgentConfig struct {
    ID            string
    Name          string
    Role          string         // "conductor", "planner", "worker", "validator", "completion"
    Triggers      []Trigger
    Prompt        string         // supports "builtin:" prefix
    Model         string
    MaxIterations int
    OnComplete    []OnCompleteAction
}

type OnCompleteAction struct {
    Topic string
    // Data is populated from the agent's result at runtime
}
```

Agent lifecycle (each runs as a goroutine):
1. Subscribe to trigger topics on the bus
2. Wait for message → evaluate predicate
3. Build context by querying bus for relevant messages
4. Execute (RunClaude or run validator)
5. Publish OnComplete messages
6. Loop or exit if maxIterations reached

Predicate evaluation — simple switch:
- `"always"` → true
- `"approved"` → `msg.Data["approved"] == true`
- `"rejected"` → `msg.Data["approved"] == false`

**New file: `internal/orchestrator/context.go`**

Context builders that query the bus to assemble agent prompts:
- `BuildPlannerContext(bus, clusterID)` → issue content
- `BuildWorkerContext(bus, clusterID)` → issue + plan + any rejection feedback
- `BuildValidatorContext(bus, clusterID)` → diff + acceptance criteria from plan
- `BuildCompletionContext(bus, clusterID)` → all validation results

### Phase 3: Conductor (`internal/conductor/`)

**New file: `internal/conductor/conductor.go`**

LLM-based classification using Claude haiku. Runs synchronously before agents start.

```go
type Classification struct {
    Complexity string // TRIVIAL, SIMPLE, STANDARD, CRITICAL
    Type       string // INQUIRY, TASK, DEBUG
    Reasoning  string
}
```

Classification prompt asks Claude to output structured JSON with complexity, type, and reasoning. Uses `agent.RunClaude()` with haiku model.

Maps classification → workflow template:
- TRIVIAL → `single-worker` (worker only, no planner, no validators)
- SIMPLE → `worker-validator` (worker + validators, no planner)
- STANDARD → `full-workflow` (planner + worker + validators)
- CRITICAL → `full-workflow` with extra validators
- DEBUG → `debug-workflow` (specialized debug prompt)

**New file: `internal/conductor/templates.go`**

Built-in workflow templates as Go structs. Each template is a list of `AgentConfig`. Can be overridden in `.jorm/config.yaml`.

### Phase 4: Orchestrator (`internal/orchestrator/orchestrator.go`)

Wires everything together:

```go
type Orchestrator struct {
    conductor *conductor.Conductor
    bus       *bus.Bus
    store     *store.Store
    worktree  *git.Worktree
    sink      events.Sink
    cfg       *config.Config
}
```

`Run(ctx, issue)`:
1. Conductor classifies the issue
2. Load workflow template (built-in + config overrides)
3. Create agent goroutines from template
4. Publish `ISSUE_OPENED` to kick off the workflow
5. Block on `CLUSTER_COMPLETE` from bus
6. Cancel all agents, return result

**Modify: `internal/loop/loop.go`**
- After issue fetch: if `cfg.Conductor.Enabled`, call `orchestrator.Run()` instead of `cluster.Run()`
- Existing cluster path remains as default

### Phase 5: Built-in Agents

**New file: `internal/orchestrator/agents/planner.go`**
- Trigger: `ISSUE_OPENED:always`
- Reads issue content from bus
- Calls Claude with `builtin:planner` prompt
- Outputs: plan steps, acceptance criteria, estimated files affected
- Publishes `PLAN_READY` with plan + criteria in Data

**New file: `internal/orchestrator/agents/worker.go`**
- Triggers: `PLAN_READY:always`, `VALIDATION_RESULT:rejected`
- Builds prompt from issue + plan + any rejection findings (via context builder)
- Calls `agent.RunClaude()` with full tools (same as today)
- Publishes `IMPLEMENTATION_READY` with diff summary

**New file: `internal/orchestrator/agents/validator.go`**
- Trigger: `IMPLEMENTATION_READY:always`
- Wraps existing `agent.Validator` implementations
- Reads diff + acceptance criteria from bus
- Runs validation, publishes `VALIDATION_RESULT` with approved/rejected + findings

**New file: `internal/orchestrator/agents/completion.go`**
- Trigger: `VALIDATION_RESULT:approved`
- Checks all validators have reported
- Publishes `CLUSTER_COMPLETE`

**New prompts:**
- `.jorm/prompts/planner.md` — plan creation with acceptance criteria
- `.jorm/prompts/classify.md` — conductor classification prompt
- `.jorm/prompts/worker.md` — worker implementation prompt (extracted from current hardcoded prompt in cluster.go)

### Phase 6: Config Changes

**Modify: `internal/config/config.go`**

Add to Config:
```go
type ConductorConfig struct {
    Enabled       bool   `yaml:"enabled"`
    ClassifyModel string `yaml:"classify_model"` // default: "haiku"
}
```

Config example:
```yaml
conductor:
  enabled: true
  classify_model: haiku
```

Templates and agent configs use sensible defaults. Advanced users can override templates in config YAML.

### Phase 7: Events & TUI

**Modify: `internal/events/events.go`**

Add to Sink interface:
```go
AgentStateChange(agentID, agentName, state string)
MessagePublished(topic, sender string)
```

**Modify: `internal/tui/model.go`**
- Add `agents []agentState` to model (id, name, role, state)
- Render agent status panel showing each agent's current state
- Prefix Claude output lines with agent name: `[worker] → Bash: go test`

## Key Files to Create
- `internal/bus/bus.go` — message bus
- `internal/conductor/conductor.go` — LLM classifier
- `internal/conductor/templates.go` — workflow templates
- `internal/orchestrator/orchestrator.go` — main orchestration loop
- `internal/orchestrator/agent.go` — agent abstraction + lifecycle
- `internal/orchestrator/context.go` — context builders
- `internal/orchestrator/agents/planner.go`
- `internal/orchestrator/agents/worker.go`
- `internal/orchestrator/agents/validator.go`
- `internal/orchestrator/agents/completion.go`
- `.jorm/prompts/planner.md`
- `.jorm/prompts/classify.md`
- `.jorm/prompts/worker.md`

## Key Files to Modify
- `internal/store/store.go` — messages table, DB() accessor, WAL mode
- `internal/config/config.go` — ConductorConfig
- `internal/loop/loop.go` — branch to orchestrator when conductor enabled
- `internal/events/events.go` — agent-aware events
- `internal/tui/model.go` + `events.go` + `sink.go` — multi-agent display

## Verification

1. `go build ./...` — compiles cleanly
2. `conductor.enabled: false` (default) — existing `jorm run` behavior unchanged
3. `conductor.enabled: true` with a GitHub issue:
   - Conductor classifies the issue (check haiku API call)
   - Correct template loaded (check agent count matches classification)
   - Planner produces plan + acceptance criteria
   - Worker implements based on plan
   - Validators check against acceptance criteria
   - On rejection: worker retries with findings
   - On acceptance: commit + push + PR hooks run
   - `CLUSTER_COMPLETE` published and run marked as accepted in SQLite
4. `jorm list` shows the run with correct status
5. TUI shows per-agent status during the run
6. Message ledger in SQLite contains full audit trail
