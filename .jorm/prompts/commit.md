# Commit

Create a git commit for all changes following Conventional Commits format. This runs headlessly — do not ask for confirmation, just execute.

## Procedure

1. Stage all changes: `git add -A`

2. Gather context:
   - `git diff --staged` to see what will be committed
   - `git log --oneline -5` for recent commit style

3. Analyze the staged diff and create the commit:

```bash
git commit -m "$(cat <<'EOF'
<type>(<scope>): <short description>

- <what was changed and why>
- <key implementation details>
- <additional bullets as needed, 2-5 total>

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

## Commit Message Rules

**Type** (required): feat, fix, docs, style, refactor, perf, test, build, ci, chore

**Scope** (preferred): lowercase, hyphen-separated module/area name. Omit for broad changes.

**Short description**: imperative mood, lowercase, no period, under 70 chars for the full first line

**Breaking changes**: add `!` after type/scope (e.g., `feat(api)!: remove v1 endpoints`)

**Bullets**: explain "what" and "why", 2-5 substantive points

**Co-Authored-By**: always include

## Rules

- Do NOT ask for confirmation — just create the commit
- Do NOT use `--amend`
- If pre-commit hooks fail, fix the issue and create a NEW commit
- If there are no changes, exit successfully

## Examples

```
feat(reports): add CSV export for user reports

- Add CsvExporter utility class with streaming support
- Create /api/reports/export endpoint with pagination
- Handle large datasets without loading all into memory

Co-Authored-By: Claude <noreply@anthropic.com>
```

```
fix(worker): resolve memory leak in background worker

- Add connection pooling with proper cleanup on shutdown
- Implement timeout handling for long-running queries
- Reduce memory usage by ~60% under load

Co-Authored-By: Claude <noreply@anthropic.com>
```
