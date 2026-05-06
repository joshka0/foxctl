# Semantic Commenting Cleanup Notes

These notes cover the semantic-comment lint findings after commit `36b88c84`
(`refactor: convert old Index: comments to structured semantic anchors`).

The structured `Index:` blocks generally follow the intended discoverability
lane. The cleanup needed is in the `[[...]]` semantic anchor lane.

## Current Lint Summary

Command:

```bash
./bin/foxctl index anchors lint --workspace . \
  | jq -r '.data.files[].occurrences[]?.findings[]?.Reason' \
  | sort | uniq -c | sort -nr
```

Current result:

```text
547 unsupported_owner
 27 unknown_scope
 10 secret_like
  6 session_like
  5 unbound_owner
  1 malformed_target
```

Fix order:

1. Fix the 44 parser-level lint errors: `unknown_scope`, `secret_like`,
   `session_like`, and `malformed_target`.
2. Re-run anchor lint.
3. Rebuild repoindex with semantic anchors enabled.
4. Re-check owner binding warnings (`unsupported_owner`, `unbound_owner`).

## Cleanup Rules

Use only supported semantic anchor types:

- `invariant`
- `risk`
- `protocol`
- `domain`
- `decision`
- `test-contract`
- `doc`
- `test`

Do not use `lifecycle:` or `context:` as anchor types. These should become
valid `domain:` or `protocol:` anchors.

Avoid slugs that trip the safety filter:

- `api_key`
- `secret`
- `token`
- `task-` when the slug contains the substring `sk-` across `task-...`
- `session-...`, `conversation-...`, or `thread-...` with a long suffix

Use lowercase slugs only. CamelCase is invalid.

## Unknown Scope Findings

### Replace `[[lifecycle:component]]`

`lifecycle` is not a valid semantic anchor type. Replace each instance with a
specific valid `domain:` or `protocol:` anchor. Do not create a generic
`domain:component` anchor unless the owner is genuinely about component
lifecycle as a domain concept.

Suggested pattern:

```text
[[lifecycle:component]]
```

becomes one of:

```text
[[domain:<component-name>]]
[[protocol:<component-lifecycle-name>]]
```

Files:

- `internal/context/companion/actor.go:60`
- `internal/context/companion/daemon.go:65`
- `internal/context/companion/daemon.go:101`
- `internal/context/companion/daemon.go:126`
- `internal/context/companion/executor.go:53`
- `internal/context/companion/executor.go:105`
- `internal/context/companion/executor.go:215`
- `internal/context/companion/memory.go:136`
- `internal/context/companion/service.go:227`
- `internal/interfaces/web/server.go:59`
- `internal/interfaces/web/sse/hub.go:87`
- `internal/runtime/observability/builder.go:48`
- `internal/runtime/observability/emitter.go:128`
- `internal/runtime/observability/persistence.go:112`
- `internal/runtime/observability/persistence.go:285`
- `internal/runtime/observability/persistence.go:310`
- `internal/runtime/observability/persistence.go:337`
- `internal/runtime/observability/persistence.go:483`
- `internal/runtime/observability/persistence.go:516`

Examples:

- `internal/context/companion/actor.go:60`: use
  `[[domain:companion-actor-lifecycle]]` or keep only
  `[[protocol:companion-actor-creation]]`.
- `internal/runtime/observability/persistence.go:285`: use
  `[[protocol:observability-background-sync-start]]` or keep
  `[[domain:observability-background-sync]]`.
- `internal/interfaces/web/sse/hub.go:87`: use
  `[[domain:sse-event-distribution]]` only, or
  `[[protocol:sse-hub-run-loop]]` if the owner is specifically the loop.

### Replace `[[context:memory]]`

`context` is not a valid semantic anchor type. These should become more
specific `domain:` anchors or be removed when the paired anchor already carries
the retrieval concept.

Suggested pattern:

```text
[[context:memory]]
```

becomes one of:

```text
[[domain:memory-context]]
[[domain:conversation-memory]]
[[domain:transcript-memory]]
```

Files:

- `internal/context/companion/memory.go:2488`
- `internal/context/companion/service.go:1956`
- `internal/context/companion/service.go:2930`
- `internal/context/companion/service.go:3469`
- `internal/context/contextplane/taskhistory/integration.go:104`
- `internal/context/transcriptpipeline/consolidate.go:102`
- `internal/context/transcriptpipeline/consolidate.go:180`
- `internal/context/transcriptpipeline/consolidate.go:241`

Examples:

- `buildChatMessages`: prefer `[[domain:conversation-memory-context]]`.
- `buildSystemPrompt`: prefer `[[domain:hybrid-memory-context]]`.
- transcript consolidation functions: prefer `[[domain:transcript-memory]]`
  or remove `[[context:memory]]` if the paired anchor is already precise.

## Secret-Like Findings

These are not necessarily leaked secrets. They are unsafe anchor slugs because
the parser intentionally blocks secret-like substrings.

### Rename `secret` and `api_key` slugs

Files:

- `skills/code_security/main.go:368`
  - Current: `[[invariant:secret-redaction]]`
  - Suggested: `[[invariant:sensitive-data-redaction]]`
- `skills/trajectory_export/main.go:251`
  - Current: `[[invariant:secret_redaction]]`
  - Suggested: `[[invariant:sensitive-data-redaction]]`
- `skills/web_search/main.go:76`
  - Current: `[[risk:api_key_missing]]`
  - Suggested: `[[risk:missing-provider-credential]]`

### Rename `task-...` slugs

The `task-` prefix contains `sk-` across the word boundary, which matches the
secret-token safety pattern. Use `work-item`, `todo`, `tasking`, or a more
specific domain word instead.

Files:

- `internal/intelligence/analysis/overseer/scorer.go:84`
  - Current: `[[protocol:task-recommendation-scoring]]`
  - Suggested: `[[protocol:work-item-recommendation-scoring]]`
- `internal/intelligence/analysis/tasksgraph/graph.go:56`
  - Current: `[[protocol:task-graph-analysis]]`
  - Suggested: `[[protocol:work-item-graph-analysis]]`
- `skills/embedding_tasks/main.go:91`
  - Current: `[[domain:task-embedding-generation]]`
  - Suggested: `[[domain:work-item-embedding-generation]]`
- `skills/embedding_tasks/main.go:92`
  - Current: `[[protocol:task-memory-embedding-storage]]`
  - Suggested: `[[protocol:work-item-memory-embedding-storage]]`
- `skills/hooks_task_guard/main.go:49`
  - Current: `[[invariant:active-task-for-writes]]`
  - Suggested: `[[invariant:active-work-item-for-writes]]`
- `skills/hooks_task_guard/main.go:50`
  - Current: `[[domain:task-centric-model]]`
  - Suggested: `[[domain:work-item-centric-model]]`
- `skills/plan_sync/main.go:94`
  - Current: `[[domain:plan-to-task-sync]]`
  - Suggested: `[[domain:plan-to-work-item-sync]]`

## Session-Like Findings

These slugs are concept names, but the safety rule blocks session-like IDs by
matching `session`, `conversation`, or `thread` plus a long suffix. Rename the
anchors to avoid those prefixes while preserving meaning.

Files:

- `internal/context/companion/service.go:620`
  - Current: `[[domain:conversation-orchestration]]`
  - Suggested: `[[domain:dialogue-orchestration]]`
- `internal/interfaces/web/consolews/hub.go:211`
  - Current: `[[domain:console-session-transport]]`
  - Suggested: `[[domain:console-pty-transport]]`
- `skills/hooks_session_end/main.go:65`
  - Current: `[[domain:session-lifecycle]]`
  - Suggested: `[[domain:turn-log-lifecycle]]` or
    `[[domain:agent-run-lifecycle]]`
- `skills/optimize_from_feedback/main.go:92`
  - Current: `[[protocol:session-feedback-schema]]`
  - Suggested: `[[protocol:run-feedback-schema]]`
- `skills/session_recall/main.go:182`
  - Current: `[[domain:semantic-session-retrieval]]`
  - Suggested: `[[domain:semantic-run-retrieval]]`
- `skills/session_timeline/main.go:150`
  - Current: `[[domain:session-timeline-retrieval]]`
  - Suggested: `[[domain:run-timeline-retrieval]]`

## Malformed Target Finding

File:

- `internal/intelligence/indexing/filesummary/worker.go:280`
  - Current: `[[invariant:stopCh-signal-respected]]`
  - Suggested: `[[invariant:stop-channel-signal-respected]]`

Reason: semantic anchor slugs must be lowercase.

## Owner Binding Warnings

The 547 `unsupported_owner` findings and 5 `unbound_owner` findings should be
handled after parser lint is clean.

These are not all comment wording bugs. They mean repoindex did not emit a
semantic edge because it could not bind the anchor occurrence to a supported
owner node.

Top files by `unsupported_owner` count:

- `internal/runtime/observability/persistence.go`: 14
- `internal/runtime/daemon/service.go`: 12
- `internal/runtime/engine/rlm_tools.go`: 9
- `internal/context/companion/service.go`: 9
- `internal/runtime/observability/sampler.go`: 8
- `internal/intelligence/indexing/embeddingtext/digest.go`: 8
- `internal/runtime/terminal/tmuxbridge/client.go`: 7
- `internal/runtime/observability/emitter.go`: 7
- `cmd/foxctl/cmd/index.go`: 7

After parser cleanup, run:

```bash
./bin/foxctl index anchors lint --workspace .
./bin/foxctl index repo build --workspace . --semantic-anchors --include-tests
./bin/foxctl index anchors lint --workspace .
```

If owner warnings persist:

1. Check whether anchors are adjacent to a supported owner: function, method,
   type, const, or var.
2. Move file-scope anchors to a real package/file owner if repoindex supports
   one, otherwise remove weak file-scope anchors.
3. Avoid anchors on repeated leaf helpers and bulk skill `run` wrappers unless
   the wrapper is the actual behavior boundary.
4. For recurring `func run` skill entrypoints, either make repoindex owner
   binding support them consistently or reduce anchors to high-value skills
   only.

## Validation Commands

Parser and repoindex tests:

```bash
GOWORK=off go test -count=1 \
  ./internal/intelligence/indexing/semanticanchors \
  ./internal/intelligence/indexing/repoindex

GOWORK=off go test -count=1 ./cmd/foxctl/cmd \
  -run TestIndexRepoSemanticAnchorsE2EIndexCommentsCoexist
```

Repo-wide lint:

```bash
./bin/foxctl index anchors lint --workspace .
```

Expected cleanup target:

- `unknown_scope`: 0
- `secret_like`: 0
- `session_like`: 0
- `malformed_target`: 0

Only after those are zero should the owner-binding warnings be evaluated.
