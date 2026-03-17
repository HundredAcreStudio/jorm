---
name: commit
description: Create a git commit following Conventional Commits format
---

# Commit Command

Creates a git commit following the [Conventional Commits](https://www.conventionalcommits.org/) format.

## Usage

```bash
/commit
```

## Procedure

### 1. Gather Git Information

Run these commands in parallel:

```bash
git status
git diff --staged
git diff
git log --oneline -5
```

This provides:
- Current branch and staging status
- Staged changes (what will be committed)
- Unstaged changes (what won't be committed)
- Recent commit history (for message style consistency)

### 2. Stage Changes if Needed

If there are unstaged changes and no staged changes:
- Show the user what files have changed
- Ask which files to stage, or offer to stage all changes
- Use `git add <files>` to stage the selected files

If there are no changes at all:
- Inform the user and exit

### 3. Analyze the Changes

For all staged changes:
- Read the full diff to understand what changed
- Identify the nature of changes (new feature, bug fix, refactor, documentation, etc.)
- Note any files that were added, modified, or deleted
- Understand the purpose and impact of the changes
- Determine the appropriate scope (the area of the codebase affected)

### 4. Draft the Commit Message

Follow this EXACT format:

```
<type>(<scope>): <short description>

<Detailed summary with bullet points>
- What was changed
- Why it was changed
- Key implementation details
- Any breaking changes or important notes

Co-Authored-By: Claude <noreply@anthropic.com>
```

**Type must be one of:**
- `feat` - A new feature
- `fix` - A bug fix
- `docs` - Documentation only changes
- `style` - Changes that do not affect the meaning of the code (formatting, whitespace, etc.)
- `refactor` - A code change that neither fixes a bug nor adds a feature
- `perf` - A code change that improves performance
- `test` - Adding or correcting tests
- `build` - Changes that affect the build system or external dependencies
- `ci` - Changes to CI configuration files and scripts
- `chore` - Other changes that don't modify src or test files

**Scope rules:**
- The scope should be the module, component, or area of the codebase affected
- Use lowercase, hyphen-separated names (e.g., `auth`, `user-api`, `db-migrations`)
- Scope is optional but preferred when the change targets a specific area
- Omit scope for changes that span the entire project

**Short description rules:**
- Present tense, imperative mood (e.g., "add feature" not "added feature")
- Lowercase first letter
- No period at the end
- Concise (under 70 characters for the entire first line)

**Breaking changes:**
- Add `!` after the type/scope for breaking changes: `feat(api)!: remove v1 endpoints`
- Also note breaking changes in the detailed summary

**Detailed summary rules:**
- Use bullet points for clarity
- Explain the "what" and "why", not just repeat the short description
- Include key implementation details
- Note any breaking changes or important considerations
- Focus on 2-5 substantive bullets

### 5. Present the Commit Message

Show the user the drafted commit message in a clear format:

```
Proposed commit message:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
feat(auth): add JWT-based authentication to API

Implement JWT-based authentication for all API endpoints.
- Add authentication middleware to validate tokens
- Create token generation and validation utilities
- Update API documentation with authentication requirements
- Add tests for authentication flows

Co-Authored-By: Claude <noreply@anthropic.com>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Files to be committed:
  M src/api/middleware/auth.js
  M src/api/server.js
  A src/utils/jwt.js
  M tests/api/auth.test.js
  M docs/API.md
```

### 6. Confirm and Create Commit

Ask the user if they want to proceed with this commit message. Options:
- **Yes, commit** - Create the commit as-is
- **Edit message** - Let the user provide modifications
- **Cancel** - Abort the commit

If approved, create the commit using:

```bash
git commit -m "$(cat <<'EOF'
<commit message here>
EOF
)"
```

### 7. Verify and Report

After committing:
- Run `git log -1 --stat` to show the created commit
- Run `git status` to show the current state
- Provide a summary of what was committed

## Important Notes

### Handling Multiple Changes

If the staged changes span multiple unrelated concerns:
- Note this in your message to the user
- Suggest they might want to split into multiple commits
- If they proceed, create a message that covers all changes

### Pre-commit Hooks

If a pre-commit hook fails:
- Display the hook output to the user
- Explain what failed
- **CRITICAL**: Do NOT use `--amend` to retry. The commit did NOT happen.
- After the user fixes the issue, re-stage and create a NEW commit
- Never use `--no-verify` unless explicitly requested

### Empty Commits

Do not create empty commits. If there are no staged changes:
- Inform the user
- Offer to stage changes if there are unstaged changes
- Exit if there's nothing to commit

## Examples

### Example 1: New Feature

```
feat(reports): add CSV export for user reports

Implement CSV export functionality for user activity reports.
- Add CsvExporter utility class with streaming support
- Create /api/reports/export endpoint
- Add export button to reports UI
- Handle large datasets with pagination

Co-Authored-By: Claude <noreply@anthropic.com>
```

### Example 2: Bug Fix

```
fix(worker): resolve memory leak in background worker

Fix memory leak caused by unclosed database connections.
- Add connection pooling with proper cleanup
- Implement timeout handling for long-running queries
- Update worker lifecycle to close connections on shutdown
- Reduce memory usage by ~60% in testing

Co-Authored-By: Claude <noreply@anthropic.com>
```

### Example 3: Refactoring

```
refactor(auth): simplify authentication middleware

Simplify authentication logic and improve testability.
- Extract token validation into separate utility
- Remove duplicate error handling code
- Add comprehensive unit tests
- No functional changes to API behavior

Co-Authored-By: Claude <noreply@anthropic.com>
```

### Example 4: Documentation

```
docs(deployment): add deployment process documentation

Add comprehensive deployment documentation.
- Create step-by-step deployment runbook
- Document rollback procedures
- Add troubleshooting guide
- Include environment-specific configuration notes

Co-Authored-By: Claude <noreply@anthropic.com>
```

## Quality Standards

Before presenting a commit message, verify:

1. Commit type is correct and from the allowed list
2. Short description is imperative mood, present tense, lowercase
3. Scope accurately reflects the area of change (if provided)
4. Detailed summary has 2-5 substantive bullet points
5. Bullets explain "what" and "why", not just "what"
6. Co-Authored-By line is included
7. Message is formatted with proper spacing
8. All staged files are relevant to the commit message
9. Breaking changes are marked with `!` if applicable

## Error Handling

**No staged changes**
-> Offer to stage changes or inform user to stage manually

**Pre-commit hook fails**
-> Show error, explain issue, do NOT use --amend

**Commit message too vague**
-> Read more context (full files, related code) to write better bullets

**User rejects commit message**
-> Ask what should be changed, redraft, present again
