# Commit

Create a git commit for all changes in the working directory following Conventional Commits format.

## Procedure

1. Run `git add -A` to stage all changes.

2. Run these commands to understand what changed:
   - `git diff --staged` to see all staged changes
   - `git log --oneline -5` for recent commit style

3. Analyze the staged diff:
   - Identify the nature of changes (feat, fix, refactor, docs, test, build, ci, chore)
   - Determine the scope (module or area affected)
   - Understand the purpose and impact

4. Check for an issue reference: run `echo $JORM_CLOSES_REF`. If it outputs a value like `Closes #39`, include it in the commit message on its own line before the Co-Authored-By line.

5. Create the commit using this exact format:

```bash
git commit -m "$(cat <<'EOF'
<type>(<scope>): <short description>

- <bullet point explaining what and why>
- <bullet point with key implementation detail>
- <additional bullets as needed, 2-5 total>

<JORM_CLOSES_REF value here, if set>

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

## Rules

- Type: feat, fix, docs, style, refactor, perf, test, build, ci, chore
- Short description: imperative mood, lowercase, no period, under 70 chars total for first line
- Scope: lowercase, hyphen-separated (e.g., auth, user-api). Omit for broad changes
- Breaking changes: add `!` after type/scope
- Bullets: explain "what" and "why", not just repeat the description
- ALWAYS include the Co-Authored-By line
- Do NOT ask for confirmation — just create the commit
- Do NOT use --amend
- If pre-commit hooks fail, fix the issue and create a NEW commit
- If there are no changes to commit, report that and exit successfully

6. After committing, run `git log -1 --stat` to verify.
