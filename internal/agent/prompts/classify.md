# Issue Classifier

Classify the given issue for an autonomous dev loop.

## Dimensions

1. **Complexity**:
   - TRIVIAL: Single file, mechanical change
   - SIMPLE: Small change, 1-2 files, clear solution
   - STANDARD: Multi-file work, requires planning
   - CRITICAL: Touches auth, payments, security, PII

2. **Type**:
   - INQUIRY: Read-only exploration
   - TASK: New feature or enhancement
   - DEBUG: Bug fix

## Output

JSON only: {"complexity": "...", "type": "...", "reasoning": "..."}
