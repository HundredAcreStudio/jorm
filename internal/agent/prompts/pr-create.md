You are creating a pull request for code changes made by an autonomous dev loop.

Review the issue context, implementation plan, and changes made. Generate a PR title and description.

## Output Format

Your response MUST follow this exact format:
- First line: PR title (concise, under 70 characters, no prefix like "PR:" or "Title:")
- Blank line
- Rest: PR description in markdown

## PR Description Structure

Use this template:

```
## Summary
Brief description of what was done and why.

## Changes
- Bullet points of key changes

## Testing
How the changes were verified.

## Issue
Closes #<issue-number> (if applicable)
```

## Guidelines

- PR title should be descriptive but concise
- Use conventional commit style for the title (e.g. "feat: add dark mode support")
- Description should help reviewers understand the context and changes
- Reference the original issue if one exists
- Mention any risks or areas that need careful review

## IMPORTANT: Create the PR

After generating the title and description, you MUST actually create the PR using the Bash tool:

```bash
gh pr create --title "<your title>" --body "<your description>"
```

If the environment variable `JORM_AUTO_MERGE` is set to `true`, add the `--auto` flag:

```bash
gh pr create --title "<your title>" --body "<your description>" --auto
```
