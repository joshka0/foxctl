# Hook Shell Triage Plan

Status: active plan

Owner: foxctl

Last updated: 2026-03-11

## Goal

Classify the remaining top-level shell hooks under `configs/hooks/` so we can
continue streamlining the hook layer without rewriting everything blindly.

The lifecycle hooks are already on the new path:

- `session-init.sh`
- `session-end.sh`
- `subagent-stop.sh`

Those are now thin shell wrappers over Go-native lifecycle entrypoints.

The remaining shell hooks should be handled by category.

## Categories

## 1. Thin lifecycle wrappers

Status: already migrated

These should remain tiny provider adapters only:

- `session-init.sh`
- `session-end.sh`
- `subagent-stop.sh`

## 2. Thin skill-wrapper candidates

Status: low-risk consolidation target

These scripts mostly:

1. read hook payload
2. optionally normalize a small amount of provider input
3. call a `hooks/*` skill
4. extract `data.hook_output`
5. translate it to provider output

Examples:

- `task-guard.sh`
- `test-feedback.sh`
- `knowledge-router.sh`
- `overseer-inbox.sh`
- `overseer-inbox-post.sh`
- `subagent-bash-guard.sh`

Recommendation:

- do not rewrite these into separate Go commands first
- first consolidate them behind a shared wrapper/adapter or route them through
  `hooks/dispatch` where practical

## 3. Shell-native utility hooks

Status: keep shell for now

These are mostly local UX helpers or simple command guards where shell is still
a reasonable fit:

- `bash-advisor.sh`
- `build-guard.sh`
- `todo-advisor.sh`
- `skill-advisor.sh`

These are usually:

- lightweight
- cheap to run
- close to the provider/tool surface
- not strongly stateful

Recommendation:

- keep them shell-based unless they start accumulating durable state, queue
  logic, or cross-hook orchestration

## 4. Stateful/orchestrating shell hooks

Status: strongest Go-migration candidates

These scripts still carry meaningful orchestration, state handling, background
execution, or DB/file mutations:

- `foxctl-mode.sh`
- `foxctl-mode-enforce.sh`

These are the scripts most likely to benefit from typed Go extraction because
they already behave like mini runtime components.

Already migrated to Go-native entrypoints:

- `context-updater-drain.sh`
- `session-restore-postcompact.sh`
- `task-file-link.sh`
- `todo-sync.sh`
- `todo-continuation.sh`
- `memory-lifecycle.sh`
- `memory-detector.sh`
- `memory-recall.sh`
- `code-analysis.sh`
- `graph-maintenance.sh`
- `live-index.sh`
- `lsp-diagnostics.sh`
- `embedding-flush.sh`
- `plan-sync.sh`

## 5. Slash-command / prompt-trigger helpers

Status: evaluate individually

These hooks sit between shell utility and orchestration:

- `anchor-detect.sh`
- `context-detect.sh`
- `counsel-detect.sh`
- `counsel-suggest.sh`

Recommendation:

- keep shell if they stay simple slash-command translators
- migrate if they grow restore/state/queue logic or need more structured tests

## Recommended Order

1. Consolidate thin skill-wrapper scripts
2. Migrate one stateful shell family end to end
3. Reevaluate shell-native utility hooks after the main orchestration paths are clean

## Best Next Slice

The best next migration family is:

- `todo-sync.sh`
- `todo-continuation.sh`
- `task-file-link.sh`

Reason:

- same domain
- shared task/session/workspace state
- repeated shell + sqlite + session-id logic
- high cross-cut risk

This family is a better architectural win than starting with advisory-only
scripts.

## Keep / Wrap / Migrate Summary

Keep shell:

- `bash-advisor.sh`
- `build-guard.sh`
- `todo-advisor.sh`
- `skill-advisor.sh`

Consolidate behind generic wrapper:

- `task-guard.sh`
- `test-feedback.sh`
- `knowledge-router.sh`
- `overseer-inbox.sh`
- `overseer-inbox-post.sh`
- `subagent-bash-guard.sh`

Migrate to Go next:

- `todo-sync.sh`
- `todo-continuation.sh`
- `task-file-link.sh`
- `context-updater-drain.sh`
- `session-restore-postcompact.sh`
- `memory-lifecycle.sh`

## Success Criteria

- fewer shell hooks with stateful logic
- less duplicated workspace/session-id resolution
- fewer direct sqlite/bash orchestration paths
- provider wrappers become predictable and easy to test
