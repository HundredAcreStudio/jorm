# Plan: jorm v2 — Full Reimplementation

## Context

jorm v1 was a prototype that validated the core concept: fetch issue → run Claude → validate → retry → commit → PR. Through building and dogfooding it, we discovered the right architecture by studying zeroshot's conductor/orchestrator pattern. Now we're starting over with a clean implementation that incorporates everything we learned.

This is a ground-up rewrite, keeping only the `.jorm/` config structure and prompt files as the interface contract.

## What We're Building

An autonomous dev loop CLI in Go that orchestrates multiple AI agents working together to implement GitHub issues, Jira tickets, markdown specs, or freeform prompts. Agents communicate through a message bus, with a conductor that classifies work and routes to the right workflow template.

## Core Workflow

```
Input (issue/file/text/prompt)
  → Conductor classifies (complexity × type via LLM)
  → Selects workflow template
  → Spawns agents (planner, worker, validators, completion)
  → Agents communicate via message bus:
      ISSUE_OPENED → Planner → PLAN_READY → Worker → IMPLEMENTATION_READY
      → Validators (parallel) → VALIDATION_RESULT → Completion/Retry
  → Accept-only actions (commit, PR creation)
  → Hooks (push, notifications)
```

## CLI Interface

```bash
# Core
jorm run <issue-id|file|prompt>   # Run the loop
jorm run 42                        # GitHub issue
jorm run 42 --pr                   # Create PR on completion
jorm run 42 --ship                 # PR + auto-merge (implies --pr)
jorm run 42 --worktree             # Isolate in git worktree
jorm run 42 -d                     # Detached/background mode
jorm run feature.md                # From markdown file
jorm run "Add dark mode"           # Freeform prompt

# Management
jorm status [run-id]               # Show run status
jorm logs <run-id> [-f]            # Stream/tail logs
jorm stop <run-id>                 # Graceful stop
jorm kill <run-id>                 # Force kill
jorm list                          # List all runs
jorm resume <run-id>               # Resume failed run
jorm clean [--all|<id>]            # Purge stale worktrees/state

# Config
jorm init                          # Generate .jorm/ config (LLM-assisted)
jorm config                        # Show resolved config
jorm config --workflow <name>      # Show workflow template
jorm config --validate             # Validate config

# Inspection
jorm inspect <run-id>              # View message bus ledger
jorm inspect <run-id> --timeline   # Compact event timeline
```

### Key Flags
- `--pr` — Create PR on completion (implies --worktree unless --docker)
- `--ship` — PR + auto-merge (implies --pr)
- `--worktree` — Git worktree isolation
- `--docker` — Docker container isolation (future)
- `-d, --detach` — Run in background
- `--profile <name>` — Validator profile
- `--config <path>` — Config file path
- `--model <model>` — Override worker model
- `--debug` — Verbose logging

## Project Structure

```
cmd/jorm/main.go                    # CLI entry point (Cobra)
internal/
  bus/bus.go                        # SQLite-backed message bus (pub/sub)
  conductor/
    conductor.go                    # LLM classification (complexity × type)
    templates.go                    # Built-in workflow templates
  orchestrator/
    orchestrator.go                 # Main lifecycle: classify → spawn → wait
    agent.go                        # Agent abstraction (goroutine lifecycle)
    context.go                      # Context builders (query bus → build prompt)
  agent/
    runner.go                       # Claude CLI invocation + stream-json parsing
    validator.go                    # Validator types (shell, claude/review, claude/action)
    prompts/                        # Embedded builtin prompts (go:embed)
      *.md
  config/
    config.go                       # YAML config types + loader
    init.go                         # LLM-assisted config generation
  events/
    events.go                       # Sink interface for UI decoupling
  git/
    worktree.go                     # Git worktree management
    diff.go                         # Diff utilities (committed + uncommitted + untracked)
  hooks/
    hooks.go                        # Post-accept/failure hook runner
  issue/
    issue.go                        # Provider interface
    github.go                       # GitHub (API + gh CLI fallback)
    jira.go                         # Jira REST API
    file.go                         # Markdown file loader
  store/
    store.go                        # SQLite persistence (runs + messages)
  tui/
    model.go                        # Bubbletea model (multi-agent dashboard)
    view.go                         # Rendering (agents, timeline, output)
    events.go                       # TUI message types
    sink.go                         # ProgramSink implementation
    run.go                          # TUI entry point + post-exit summary
  log/
    logger.go                       # Structured logger (levels, file output, contextual fields)
.jorm/
  config.yaml                       # Project config
  prompts/
    commit.md                       # Headless commit prompt
    pr-review.md                    # Code quality review
    security-review.md              # Security review
    planner.md                      # Plan + acceptance criteria
    worker.md                       # Implementation worker
    classify.md                     # Conductor classification
    pr-create.md                    # PR title + description generation
```

## Config Structure (.jorm/config.yaml)

```yaml
# Model defaults
model: sonnet                        # Default worker model
classify_model: haiku                # Conductor classification model

# Issue provider
issue_provider: github               # github, jira, file

# Provider auth
providers:
  github:
    token_var: MY_GITHUB_TOKEN
  jira:
    token_var: JIRA_API_TOKEN
    url: https://mycompany.atlassian.net

# Validators
validators:
  - id: build
    name: Build
    type: shell
    command: "go build ./..."
    on_fail: reject
    timeout: 60s

  - id: tests
    name: Tests
    type: shell
    command: "go test ./..."
    on_fail: reject
    timeout: 120s

  - id: pr-review
    name: PR Review
    type: claude
    mode: review
    prompt: "builtin:pr-review"
    on_fail: reject
    timeout: 300s

  - id: commit
    name: Commit
    type: claude
    mode: action
    prompt: "builtin:commit"
    on_fail: reject
    run_on: accept_only
    timeout: 120s

  - id: pr-create
    name: Create PR
    type: claude
    mode: action
    prompt: "builtin:pr-create"
    run_on: accept_only
    timeout: 120s

# Profiles
profiles:
  default: [build, tests, pr-review, commit]
  strict: [build, tests, lint, security, pr-review, commit]
  quick: [build, tests, commit]
  pr: [build, tests, pr-review, commit, pr-create]

# Hooks
hooks:
  on_complete:
    - "git push -u origin HEAD"
  on_failure: []

# Extra env vars for subprocesses
env:
  CGO_ENABLED: "1"
```

## Implementation Phases

### Phase 1: Foundation
- SQLite store with runs + messages tables (WAL mode)
- Message bus (Publish, Subscribe, Query, FindLast)
- Config loader with defaults
- Structured logger (debug/info/warn/error, file output)
- CLI skeleton (Cobra: run, list, config, init)

### Phase 2: Agent System
- Agent abstraction (config, triggers, predicates, lifecycle goroutine)
- Claude CLI runner (stream-json parsing, stderr capture, output callbacks)
- Context builders (planner, worker, validator, completion)
- Prompt resolver (.jorm/prompts/ → embedded builtins)

### Phase 3: Conductor + Orchestrator
- LLM classification (haiku)
- Built-in workflow templates (single-worker, worker-validator, full-workflow, debug)
- Orchestrator lifecycle (classify → spawn agents → publish ISSUE_OPENED → wait CLUSTER_COMPLETE)
- Validator injection from config profiles

### Phase 4: Validators + Actions
- Shell validator (exit code)
- Claude review validator (blind diff, VERDICT)
- Claude action validator (full tools, commit/PR)
- Accept-only validators (post-acceptance)

### Phase 5: Issue Providers + Input
- GitHub (net/http API + gh CLI fallback)
- Jira (REST API)
- Markdown file loader
- Freeform string input
- Input auto-detection (number → issue, .md → file, else → prompt)

### Phase 6: Git + Hooks
- Worktree management (create, diff, cleanup, stale detection)
- Diff capture (committed + uncommitted + untracked)
- Hook runner (shell commands in worktree, env injection)
- --pr / --ship flag handling

### Phase 7: TUI
- Bubbletea multi-agent dashboard
- Agent status panel (role, state, iteration)
- Scrollable output viewport (prefixed by agent name)
- Validator status bar
- Bus message timeline (toggleable)
- Post-exit summary (classification, agents, validators, hooks, cost)
- --no-tui plain text mode

### Phase 8: Init Command
- `jorm init` runs Claude to analyze the project:
  - Detect languages (go.mod, package.json, Cargo.toml, pyproject.toml, etc.)
  - Detect test commands, lint commands, build commands
  - Detect monorepo structure (multiple language roots)
  - Generate .jorm/config.yaml with appropriate validators
  - Generate .jorm/prompts/ with project-specific review prompts
- Monorepo support: detect sub-projects and generate per-project validators

### Phase 9: Management Commands
- `jorm status` — read run state + latest bus messages
- `jorm logs` — tail message ledger
- `jorm stop/kill` — signal file or DB flag
- `jorm clean` — purge worktrees + branches + state
- `jorm inspect` — message bus ledger viewer
- `jorm config` — resolved config + workflow visualization

### Phase 10: Polish
- Exponential backoff with rate-limit awareness
- Error sanitization before retry injection
- Cost tracking per agent
- Resume via message ledger replay
- Detached mode (-d flag)

## Key Design Decisions

1. **Message bus is the backbone** — all agent communication flows through SQLite-backed pub/sub. Enables audit trail, resume, and debugging.
2. **Agents are goroutines** — each agent runs as a goroutine with a trigger-driven lifecycle loop. Simple, Go-idiomatic.
3. **Named predicates** — trigger evaluation uses simple strings (always, approved, rejected), not an expression engine.
4. **Prompts are files** — .jorm/prompts/ overrides embedded builtins. Users customize behavior by editing markdown.
5. **Config drives everything** — validators, profiles, hooks, providers all in one YAML file. No code changes needed to customize.
6. **TUI is decoupled** — events.Sink interface means business logic doesn't know about the UI. PrintSink for plain text, ProgramSink for bubbletea.
7. **Worktrees are opt-in** — default runs in current directory. --worktree or --pr enables isolation.
8. **PR creation is an agent** — not a dumb hook. It has full context from the bus to generate good titles and descriptions.

## Verification

1. `go build ./...` compiles cleanly at each phase
2. `jorm init` generates valid config for a Go project
3. `jorm run "Add a hello world endpoint"` completes the full lifecycle
4. `jorm run 42` fetches a GitHub issue and implements it
5. `jorm run 42 --pr` creates a PR with intelligent title/description
6. `jorm config` shows the resolved workflow
7. `jorm inspect <id>` shows the message bus audit trail
8. TUI shows multi-agent progress without log spam
9. `--no-tui` mode produces clean, readable output
10. `--debug` surfaces detailed logs for troubleshooting
