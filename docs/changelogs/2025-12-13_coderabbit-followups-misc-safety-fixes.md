# CodeRabbit follow-ups – Misc safety fixes

## Summary

- Made diff application stricter by rejecting unknown hunk line prefixes in
  `internal/agent/tools/edit_tools.go` so malformed diffs fail fast.
- Updated `skills/code_swe_grep/testdata/config.go` fixture to follow
  guidelines:
  - Context-aware `Load(ctx)` and `Validate(ctx)`.
  - No ignored parse errors for `PORT`.
  - Error type includes canonical code and actionable hint.
- Updated cancellation assertion in `skills/code_swe_grep/main_test.go` to use
  `errors.Is` for wrapped errors.
