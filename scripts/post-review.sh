#!/usr/bin/env bash
#
# post-review.sh — Run jorm's review prompts against a completed jorm run
#
# Feeds the generated diff to each reviewer (pr-review, security-review, tester-review)
# and reports whether they find issues the pipeline should have caught.
#
# Usage:
#   ./scripts/post-review.sh /path/to/jorm-output-repo
#   ./scripts/post-review.sh /path/to/jorm-output-repo --json
#
# Requires: claude CLI, git

set -euo pipefail

REPO_DIR="${1:?Usage: post-review.sh <repo-dir> [--json]}"
JSON_MODE="${2:-}"
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PROMPTS_DIR="$SCRIPT_DIR/internal/agent/prompts"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log() { [[ "$JSON_MODE" == "--json" ]] || echo -e "$@"; }

# Get the diff from the most recent commit
DIFF=$(cd "$REPO_DIR" && git diff HEAD~1 2>/dev/null || echo "")
if [[ -z "$DIFF" ]]; then
    log "${RED}No diff found in $REPO_DIR (no commits or single commit)${NC}"
    exit 1
fi

DIFF_LINES=$(echo "$DIFF" | wc -l | tr -d ' ')
log "${CYAN}Post-review of $REPO_DIR ($DIFF_LINES diff lines)${NC}"
log ""

FOUND_ISSUES=0
RESULT_PR=""
RESULT_SECURITY=""
RESULT_TESTER=""

run_reviewer() {
    local reviewer="$1"
    local label="$2"
    local prompt_file="$PROMPTS_DIR/$reviewer.md"

    if [[ ! -f "$prompt_file" ]]; then
        log "${YELLOW}SKIP: $label — prompt not found at $prompt_file${NC}"
        echo "SKIP"
        return
    fi

    local prompt
    prompt=$(cat "$prompt_file")

    local full_prompt="$prompt

## Diff to review

\`\`\`diff
$DIFF
\`\`\`

End your response with exactly \"VERDICT: ACCEPT\" or \"VERDICT: REJECT\" followed by a brief reason."

    log "${CYAN}Running $label...${NC}"

    local review_output
    review_output=$(cd "$REPO_DIR" && echo "$full_prompt" | claude --print --output-format text --model sonnet 2>/dev/null || echo "ERROR: claude failed")

    if echo "$review_output" | grep -q "VERDICT: ACCEPT"; then
        log "  ${GREEN}✓ $label: ACCEPT${NC}"

        local notes
        notes=$(echo "$review_output" | grep -o '"notes":\s*\[.*\]' | head -1 || echo "")
        if [[ -n "$notes" && "$notes" != *'[]'* ]]; then
            log "    ${YELLOW}Notes: $notes${NC}"
        fi
        echo "ACCEPT"
    elif echo "$review_output" | grep -q "VERDICT: REJECT"; then
        FOUND_ISSUES=$((FOUND_ISSUES + 1))
        log "  ${RED}✗ $label: REJECT${NC}"

        local output_file="/tmp/jorm-post-review-$reviewer.txt"
        echo "$review_output" > "$output_file"
        log "    Full output: $output_file"
        echo "REJECT"
    else
        log "  ${YELLOW}? $label: No clear verdict${NC}"
        local output_file="/tmp/jorm-post-review-$reviewer.txt"
        echo "$review_output" > "$output_file"
        log "    Full output: $output_file"
        echo "UNKNOWN"
    fi
    log ""
}

RESULT_PR=$(run_reviewer "pr-review" "PR Review")
RESULT_SECURITY=$(run_reviewer "security-review" "Security Review")
RESULT_TESTER=$(run_reviewer "tester-review" "Tester Review")

# Summary
log "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
log "${CYAN}Post-Review Summary${NC}"
log "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

print_status() {
    local label="$1" status="$2"
    case "$status" in
        ACCEPT) log "  ${GREEN}✓ $label${NC}" ;;
        REJECT) log "  ${RED}✗ $label${NC}" ;;
        SKIP)   log "  ${YELLOW}○ $label (skipped)${NC}" ;;
        *)      log "  ${YELLOW}? $label (unknown)${NC}" ;;
    esac
}

print_status "PR Review" "$RESULT_PR"
print_status "Security Review" "$RESULT_SECURITY"
print_status "Tester Review" "$RESULT_TESTER"
log ""

if [[ $FOUND_ISSUES -gt 0 ]]; then
    log "${RED}$FOUND_ISSUES reviewer(s) found issues the pipeline missed.${NC}"
    EXIT_CODE=1
else
    log "${GREEN}All reviewers accepted. Pipeline output meets review standards.${NC}"
    EXIT_CODE=0
fi

# JSON output mode
if [[ "$JSON_MODE" == "--json" ]]; then
    cat <<JSONEOF
{
  "repo": "$REPO_DIR",
  "diff_lines": $DIFF_LINES,
  "found_issues": $FOUND_ISSUES,
  "results": {
    "pr-review": "$RESULT_PR",
    "security-review": "$RESULT_SECURITY",
    "tester-review": "$RESULT_TESTER"
  }
}
JSONEOF
fi

exit $EXIT_CODE
