# jorm

Autonomous dev loop harness that takes a GitHub or Linear issue, runs Claude Code headlessly to implement it, fans out parallel validators via Go goroutines and channels, then runs post-accept hooks like `claude commit` and `gh pr create`.

## How it works

1. **Fetch issue** from GitHub or Linear
2. **Create git worktree** with an isolated branch
3. **Run Claude Code** headlessly to implement the issue
4. **Validate** the changes with parallel and sequential validators (shell commands + blind Claude reviews)
5. **Retry** if validators reject, injecting findings into the next prompt
6. **Post-accept hooks** run after validation passes (commit, create PR, etc.)

## Quick start

```bash
# Build
go build -o jorm ./cmd/jorm

# Configure
cp .dev-loop.yaml your-repo/.dev-loop.yaml
# Edit validators, profiles, and hooks for your project

# Run
export GITHUB_TOKEN=...
export GITHUB_REPOSITORY=owner/repo
export ANTHROPIC_API_KEY=...

jorm run 42          # Run for GitHub issue #42
jorm resume 42       # Resume a failed run
jorm list            # List all runs
```

## Docker

```bash
docker compose run jorm run 42
```

## Configuration

See `.dev-loop.yaml` for a fully commented example config.

### Validator types

- **shell**: Runs a shell command. Exit 0 = accept.
- **claude**: Runs a blind Claude review with the diff. Looks for `VERDICT: ACCEPT` in output.

### Validator options

| Field      | Values                              | Default  |
|------------|-------------------------------------|----------|
| `on_fail`  | `reject`, `warn`, `ignore`          | `reject` |
| `run_on`   | `always`, `accept_only`, `reject_only` | `always` |
| `parallel` | `true`, `false`                     | `false`  |
| `timeout`  | duration string (e.g., `120s`)      | none     |

### Profiles

Named sets of validator IDs. Use `--profile` to select one at runtime.

### Hooks

Shell commands that run after the loop completes or fails. Execute in the worktree directory with inherited environment.

## CLI

```
jorm run <issue-id>      Run the dev loop for an issue
jorm resume <issue-id>   Resume a previous run
jorm list                List all runs

Flags:
  --config   Path to config file (default: .dev-loop.yaml)
  --repo     Path to git repository (default: .)
  --profile  Validator profile to use
```

## Architecture

```
cmd/jorm/main.go        CLI entry point (Cobra)
internal/
  loop/loop.go          Top-level orchestrator
  cluster/cluster.go    Worker → validate → retry loop
  agent/agent.go        Claude CLI runner
  agent/validator.go    Validator interface + shell/claude implementations
  config/config.go      YAML config loader
  git/worktree.go       Git worktree management
  issue/                Issue providers (GitHub, Linear)
  hooks/hooks.go        Post-accept/failure hook runner
  store/store.go        SQLite state persistence
```
