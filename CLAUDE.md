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

jorm is an autonomous dev loop harness: fetch issue → run Claude Code headlessly → validate → retry → post-accept hooks.

- **`cmd/jorm/main.go`** — Cobra CLI with `run`, `resume`, `list` subcommands
- **`internal/loop/loop.go`** — Top-level orchestrator: config → provider → worktree → cluster → hooks → state
- **`internal/cluster/cluster.go`** — Core retry loop: builds worker prompt → calls Claude → gets diff → fans out validators (parallel via goroutines+channels, then sequential with short-circuit) → retries with injected findings
- **`internal/agent/agent.go`** — Runs `claude --print --output-format stream-json`, parses streaming JSON for result text and cost. `resolveModel()` maps aliases (sonnet/opus/haiku) to full model IDs
- **`internal/agent/validator.go`** — `Validator` interface with `ShellValidator` (exit code) and `ClaudeValidator` (blind review, looks for `VERDICT: ACCEPT`). `ValidatorResult.IsBlocker()` checks on_fail=="reject"
- **`internal/config/config.go`** — YAML config loader with defaults (max_attempts=5, model=sonnet, profile=default). `ValidatorsForProfile()` resolves profile → validator configs
- **`internal/git/worktree.go`** — Creates/cleans git worktrees on `jorm/issue-<id>` branches, provides `Diff()` and `HasChanges()`
- **`internal/issue/`** — `Provider` interface with `GitHubProvider` (go-github) and `LinearProvider` (GraphQL, generic `linearGraphQL[T]` helper)
- **`internal/store/store.go`** — SQLite persistence at `~/.jorm/jorm.db` for `RunState` (id, issue, branch, attempt, status, findings)
- **`internal/hooks/hooks.go`** — Runs shell commands in worktree dir for on_complete/on_failure lifecycle events

## Key Patterns

- **Blind validation**: Claude validators get a fresh context with only the diff — they never see worker history
- **Validator fan-out**: parallel validators run via goroutines + buffered channel + WaitGroup; sequential validators short-circuit on blocking reject
- **Prompt injection on retry**: rejected findings are appended under "## Previous attempt was rejected. Fix these issues:" in the next worker prompt
- **Worktree lifecycle**: cleanup deferred only if no changes were produced (keeps worktree if commits exist)

## Code Style

- Follow standard Go conventions (gofmt, go vet)
- Error handling: return errors, wrap with `fmt.Errorf("context: %w", err)`
- Naming: MixedCaps/mixedCaps per Go conventions
