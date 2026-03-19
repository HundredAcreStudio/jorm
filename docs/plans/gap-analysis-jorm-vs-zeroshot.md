# jorm vs zeroshot Gap Analysis

*Date: 2026-03-19*

## Current State

jorm has ~70% of zeroshot's core architecture. The conductor, message bus, agent lifecycle, workflow templates, and retry loop are all working. The successful dogfood run (issue #39 → PR #42) proved the end-to-end pipeline works.

The quality gap is in the **prompts and validator suite**, not the engine. The biggest functional gap is **--pr/--ship wiring** and the **hooks system**.

## What jorm HAS (implemented and working)

| Feature | Notes |
|---|---|
| Conductor classification (LLM via haiku) | TRIVIAL/SIMPLE/STANDARD/CRITICAL × INQUIRY/TASK/DEBUG |
| 4 workflow templates | single-worker, worker-validator, full-workflow, debug |
| Message bus (SQLite pub/sub) | Publish, Subscribe, Query, FindLast with WAL mode |
| 3 agent execution modes | claude, shell, passthrough |
| Planner with numbered ACs | AC1-AC15 with verification commands (#43) |
| Requirements validator | Checks ACs with evidence, JSON output (#44) |
| Code review validator | VERDICT: ACCEPT/REJECT auto-injected |
| Security validator | In strict profile |
| Shell validators | build, test, vet, lint — run real commands |
| Commit action validator | Conventional commits, Closes #N |
| Worker retry on rejection | Findings injected into context |
| Completion detector | Waits for all validators, resets on rejection |
| 5 issue providers | GitHub, Linear, Jira, File, String |
| In-place mode | Default, --worktree opt-in |
| Scrolling TUI | Agent-prefixed output, round separators, no alt-screen |
| 11 CLI commands | run, resume, list, status, logs, stop, clean, inspect, config, version, init |
| --pr / --ship / --worktree flags | With implication chain (ship→pr→worktree) |
| Structured logging | slog JSON to ~/.jorm/logs/ |
| 42 tests | orchestrator, context, prompts, config, ui |
| Stop signal | File-based polling in orchestrator |
| Version command | Build-time ldflags injection |

## Remaining Gaps

### Critical (affects output quality)

| Gap | Impact | Issue |
|---|---|---|
| Tester validator prompt | No dedicated test runner with structured results | #45 |
| Code review WHAT/HOW/WHY format | Rejections less actionable for worker retries | #46 |
| Criteria flow to validators | Planner ACs in Data field not always populated | #48 |
| Comprehensive log files | Logger only captures a few slog calls, not full lifecycle | #49 |

### Major (feature gaps)

| Gap | Impact | Issue |
|---|---|---|
| Footer not rendering | ANSI scroll region code exists but doesn't display | #37 (partial) |
| --pr / --ship not wired | Flags exist but PR creation logic incomplete | Needs issue |
| Hooks not invoked | hooks.go exists but removed from on_complete config | Needs fix |
| Accept-only validators | Commit validator path may be broken post-refactor | Needs verification |
| Docker isolation | No container support | #23 |
| Guidance injection | Can't send instructions to running agents | #21 |
| Dynamic agent management | Can't add/remove agents mid-run | #24 |
| MCP server | Can't monitor runs from Claude Code | #50 |
| init command | Stubbed, no LLM config generation | Needs implementation |
| Security validator prompt | Needs severity levels, OWASP checks | #47 |

### Moderate (nice to have)

| Gap | Impact | Issue |
|---|---|---|
| Exponential backoff | No delay between retries | #4 |
| Stuck detection | Hung agents not detected | #5 |
| Cost tracking to TUI | Cost accumulated but not always displayed | #16 |
| Multi-cluster parallel | One issue at a time | #26 |
| Daemon mode (-d flag) | No background execution | — |
| Resume via ledger replay | Resume restarts from scratch | #20 |
| Template overrides in config | Users can't customize workflow templates | #18 |
| Git-pusher agent | PR creation as consensus-gated agent | #28 |
| JSON schema validation | No structured output enforcement | #30 |
| Context strategies with token budgets | Flat context, no intelligent selection | #25 |

## Priority Roadmap

### Phase 1: Close the quality gap (prompts)
1. #46 — Code review WHAT/HOW/WHY format
2. #45 — Tester validator prompt
3. #47 — Security validator with severity levels
4. #48 — Criteria flow from planner to validators

### Phase 2: Complete the pipeline
5. Wire --pr/--ship to actually create PRs after commit
6. Fix hooks invocation (or replace with flag-gated logic)
7. Verify accept-only validators (commit) work end-to-end
8. #49 — Comprehensive log files (LogSink wrapper)

### Phase 3: Polish the UX
9. Fix footer rendering in TUI
10. #50 — MCP server for monitoring
11. init command for LLM-assisted config generation

### Phase 4: Advanced features
12. #21 — Guidance injection
13. #23 — Docker isolation
14. #20 — Resume via ledger replay
15. #4 — Exponential backoff

## Key Insight

The engine is there. The gap is in what we're telling the agents to do (prompts), not what they're capable of (code). Zeroshot's advantage comes from:
1. Specialized validator prompts that produce structured, actionable output
2. A planner that produces detailed acceptance criteria validators can check mechanically
3. 4 specialized validators (requirements, code, security, tester) vs jorm's current 3 effective ones
4. Error format (WHAT/HOW/WHY) that gives the worker clear fix instructions on rejection
