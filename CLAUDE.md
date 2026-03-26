# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development

```bash
CGO_ENABLED=1 go build ./...              # Build all packages (CGO required for sqlite3)
CGO_ENABLED=1 go build -o jorm ./cmd/jorm # Build the CLI binary
go test ./...                              # Run all tests
go test ./internal/config                  # Run tests for a specific package
go test -run TestName ./internal/...       # Run a single test by name
go vet ./...                               # Static analysis
```

CGO_ENABLED=1 is required because `mattn/go-sqlite3` is a CGO library. Ensure `gcc` is available.

## Architecture

jorm is an autonomous dev loop harness: fetch issue → classify → run staged pipeline → validate → retry → commit → post-accept hooks.

### Entry Points
- **`cmd/jorm/main.go`** — Cobra CLI with `run`, `resume`, `list`, `status`, `logs`, `stop`, `clean`, `inspect`, `config`, `init` subcommands

### Core Pipeline (Staged)
- **`internal/loop/loop.go`** — Top-level orchestrator: config → provider → worktree → conductor → stage orchestrator → hooks → state
- **`internal/conductor/conductor.go`** — Classifies issues by complexity/type (TRIVIAL/SIMPLE/STANDARD/CRITICAL × INQUIRY/TASK/DEBUG) using Claude
- **`internal/conductor/templates.go`** — `BuildStagedTemplate()` builds pipeline from config validators. `BuiltinStagedTemplates()` provides fallback defaults. Agent factories: `plannerAgent()`, `workerAgent()`, `testWriterAgent()`, review agents
- **`internal/orchestrator/stages.go`** — `StageOrchestrator` drives stages sequentially via imperative `for` loop. `runAgentStage()` for one-shot agents, `runReviewStage()` for reviewer → worker → tester retry loops
- **`internal/orchestrator/agent.go`** — `Agent.ExecuteOnce()` runs a single agent cycle (shell or Claude mode). `AgentConfig` defines behavior. `AgentResult` carries output, approval status, cost, and structured data
- **`internal/orchestrator/stage.go`** — `Stage` and `StageKind` types (`StageKindAgent` for one-shot, `StageKindReview` for retry loops)
- **`internal/orchestrator/context.go`** — Context builders that query the bus to assemble prompts: `BuildPlannerContext`, `BuildWorkerContext`, `BuildTestWriterContext`, `BuildValidatorContext`, `BuildStageScopedWorkerContext`

### Infrastructure
- **`internal/bus/bus.go`** — SQLite-backed message bus (data plane). Persists messages for context builders and audit trail. In-memory pub/sub for subscribers
- **`internal/store/store.go`** — SQLite persistence at `~/.jorm/jorm.db` for `RunState` (id, issue, branch, status). Also hosts the messages table used by the bus
- **`internal/agent/agent.go`** — Runs `claude --print --output-format stream-json`, parses streaming JSON for result text and cost. `resolveModel()` maps aliases (sonnet/opus/haiku) to full model IDs
- **`internal/agent/validator.go`** — `Validator` interface with `ShellValidator` (exit code), `ClaudeReviewValidator` (blind review, looks for `VERDICT: ACCEPT`), and `ClaudeActionValidator` (full tool access, e.g. commit). Used for post-accept validators
- **`internal/config/config.go`** — YAML config loader with defaults (model=sonnet, profile=default). `ValidatorsForProfile()` resolves profile → validator configs
- **`internal/git/worktree.go`** — Creates/cleans git worktrees on `jorm/issue-<id>` branches, provides `Diff()` and `HasChanges()`
- **`internal/issue/`** — `Provider` interface with `GitHubProvider`, `LinearProvider`, `JiraProvider`, `FileProvider`
- **`internal/hooks/hooks.go`** — Runs shell commands in worktree dir for on_complete/on_failure lifecycle events
- **`internal/log/logger.go`** — Structured logger using `log/slog` with file output to `~/.jorm/logs/<run-id>.log`
- **`internal/log/sink.go`** — `LogSink` decorator that wraps another `Sink` and logs all events to the structured log file

### UI
- **`internal/events/events.go`** — `Sink` interface (22 methods) for pipeline observability. `PrintSink` as no-op default
- **`internal/ui/`** — ANSI scroll-region UI with persistent footer showing agent status, run progress, cost
- **`internal/tui/`** — Bubble Tea TUI (alternative UI, lifecycle events are no-ops currently)

## Key Patterns

- **Staged pipeline**: Sequential stages driven by `StageOrchestrator.Run()`. Agent stages run once, review stages loop: reviewer → worker fix → tester → re-review
- **Stage-scoped feedback**: `BuildStageScopedWorkerContext` filters rejection feedback by `stage_index` so workers only see the current stage's reviewer feedback
- **Bus as data plane**: The bus persists messages to SQLite for context builders and audit, but does NOT drive control flow (that's the imperative `for` loop)
- **Blind validation**: Claude validators get a fresh context with only the diff — they never see worker history
- **ReviewMode VERDICT parsing**: Claude reviewers with `ReviewMode: true` and no `ResultProcessor` parse `VERDICT: ACCEPT/REJECT` from output
- **Config-driven templates**: `BuildStagedTemplate()` reads validators from config profiles. Shell reject → tester, shell warn → agent stage, claude review → review stage
- **Worktree lifecycle**: cleanup deferred only if no changes were produced (keeps worktree if commits exist)
- **In-place mode**: Default runs in current directory without creating a git worktree; `--worktree` creates isolated worktree
- **Flag implications**: `--ship` implies `--pr` implies `--worktree`

## Code Style

- Follow standard Go conventions (gofmt, go vet)
- Error handling: return errors, wrap with `fmt.Errorf("context: %w", err)`
- Naming: MixedCaps/mixedCaps per Go conventions
