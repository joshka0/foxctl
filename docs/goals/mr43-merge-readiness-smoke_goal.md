# Goal: MR 43 Merge Readiness Smoke

## Goal

Prove that merge request 43 is safe to merge by rerunning the high-value build,
tool, storage, room relay, frontend, and integration smoke checks from the
current worktree. Produce a concise evidence report with pass/fail status,
unavailable conditional checks, residual risks, and a final merge-readiness
decision.

This is a verification goal, not a refactor goal. Do not change product code
unless a real blocker is found and the fix is narrow, reviewed against the
affected call graph, and fully reverified.

## Context

- Worktree: `/home/dev/repos/foxctl-code-quality-cleanup`
- Branch: `feat/code-quality-cleanup`
- Merge request: GitLab MR `!43`
- Current verified branch head from prior smoke pass:
  `038c8300cedfa278b6c01e7e8129900dbd06188d`
- Built binary from prior smoke pass:
  - Path: `./bin/foxctl`
  - Version: `038c8300`
  - SHA-256:
    `f396eb0821b105ba2f25fcc8f0fc62cd765d24e49671006a021027914cec5c43`
- Prior MR state:
  - Open, not draft
  - `detailed_merge_status: mergeable`
  - Head pipeline `2547557012` was `success`
  - Blocking discussions resolved
  - Approvals left: `0`
- Prior diff size: `366` files changed, `12313` insertions,
  `10486` deletions.
- Repo guidance:
  - `AGENTS.md`
  - `CONTEXT.md`
  - `docs/glossary.md`
  - `docs/architecture/package-topology.md`
- Related audit docs:
  - `docs/goals/code-quality-cleanup-results.md`
  - `docs/goals/code-quality-cleanup-audit.md`
  - `docs/goals/code-quality-cleanup-outstanding_goal.md`

The areas with highest residual risk are:

- Room transport and delivery-binding hard cut.
- Existing persisted room/storage rows after removing legacy API mirrors.
- GUI room/control/log surfaces that consume the canonical DTOs.
- CoVe and RLM model-backed paths, which previously reported clean structured
  unavailable errors when no provider or local model server was configured.
- Pi and Hermes integrations, where type/unit checks passed but live service
  checks depend on local credentials or service availability.

## Constraints

- Work only from `/home/dev/repos/foxctl-code-quality-cleanup`.
- Do not rebase, force-push, rewrite branch history, or push without explicit
  instruction.
- Do not mutate live or production foxctl storage. Copy existing storage to an
  isolated persistent path before testing decode or migration behavior.
- Use persistent temp/storage paths because `/tmp` may be near capacity:

```bash
export TMPDIR=/var/tmp/foxctl-codex/tmp
export GOTMPDIR=/var/tmp/foxctl-codex/tmp
export GOCACHE=/home/dev/.cache/go-build
export FOXCTL_STORAGE_ROOT=/var/tmp/foxctl-codex/storage-smoke
export FOXCTL_PATHS_CAS=/var/tmp/foxctl-codex/cas-smoke
export FOXCTL_OBS_DIR=/var/tmp/foxctl-codex/obs-smoke
mkdir -p "$TMPDIR" "$FOXCTL_STORAGE_ROOT" "$FOXCTL_PATHS_CAS" "$FOXCTL_OBS_DIR"
```

- Do not add dependencies.
- Do not expose secrets, tokens, API keys, or local private data in logs,
  commits, or the report.
- If a check requires missing credentials, provider configuration, local model
  server, tmux/zellij/herdr process, or a running frontend service, mark it
  unavailable with the exact reason. Do not fake a pass.
- If code changes are necessary, keep them in a separate commit-sized slice,
  inspect affected callers with repo search and `./bin/foxctl run code/dag_grep`
  where useful, then rerun all affected verification and the MR pipeline.
- Prefer exact, built-binary commands: `./bin/foxctl ...`.

## Milestones

### 1. Confirm Branch And MR State

Done when:

- `git status --short` is recorded.
- Current branch and commit SHA are recorded.
- MR `!43` metadata is checked with `glab` or the GitLab API.
- MR is still open, not draft, mergeable, and green, or any drift is clearly
  reported.

Suggested commands:

```bash
git status --short
git branch --show-current
git rev-parse HEAD
git diff --stat gitlab/main...HEAD
glab mr view 43 --json title,state,draft,mergeStatus,headPipeline,blockingDiscussionsResolved,approvals
```

### 2. Rebuild The Binary

Done when:

- `make build` passes from this worktree.
- `./bin/foxctl version` succeeds.
- Binary path, version, size, and SHA-256 are recorded.

Suggested commands:

```bash
make build
./bin/foxctl version
ls -lh ./bin/foxctl
sha256sum ./bin/foxctl
```

### 3. Rerun Core Tool Smoke Checks

Done when all required smoke checks pass, or any failure is captured with the
exact command, stderr summary, and likely owner.

Required checks:

```bash
./bin/foxctl index repo build --workspace . --go --typescript --elixir
./bin/foxctl index repo search --workspace . --query RoomDeliveryBinding --limit 5
./bin/foxctl run code/dag_grep --input '{"query":"RoomMember DeliveryBinding relay","workspace":".","render":"tree","edge_sets":["structural"],"depth":2,"budget":80,"k":5}'
./bin/foxctl run repo/index_dag_grep --input '{"query":"RoomDeliveryBinding NormalizeRoomDeliveryBinding DefaultRoomDeliverySubmitMode","workspace":".","render":"tree","edge_sets":["structural"],"depth":2,"budget":80,"k":5}'
./bin/foxctl run fs/ls --input '{"path":"."}'
./bin/foxctl run text/grep --input '{"pattern":"RoomDeliveryBinding","path":"internal","limit":10}'
./bin/foxctl run code/symbols --input '{"path":"internal/domain/agent"}'
./bin/foxctl run git/status --input '{}'
./bin/foxctl run code/context_grep --input '{"pattern":"RoomDeliveryBinding","path":"."}'
./bin/foxctl run code/smart_search --input '{"query":"room delivery binding transport canonical contract","limit":5}'
./bin/foxctl run code/imports --input '{"path":"internal/domain/agent"}'
./bin/foxctl run code/stats --input '{"path":"."}'
```

Also run `code/security` and record the result, but treat known DES substring
matches in words like `description` or `deserializes` as tool-noise unless a
real insecure crypto use is found.

### 4. Rerun Focused Test And Type Gates

Done when these pass:

```bash
go test ./cmd/foxctl/cmd ./internal/domain/agent ./internal/storage/blackboard ./internal/interfaces/web/api -run 'TestRoomMemberJSONUsesDeliveryBindingTransportOnly|TestNormalizeRoomMemberUsesExplicitDeliveryBinding|TestBoardStore_UpdateRoomMemberBinding|TestRoomMemberBinding|TestRoomControl' -count=1
bun run unused:frontend
bun run --cwd integrations/pi check
python3 -m unittest integrations.hermes.test_client
bun run --cwd packages/foxterm typecheck
```

If time allows, also run broader gates and record the result:

```bash
make test
make lint
make check
```

### 5. Existing-Data Storage Smoke

Done when:

- A real existing foxctl storage root is identified, or the report states that
  none was available.
- The storage root is copied to a persistent isolated path such as
  `/var/tmp/foxctl-codex/mr43-storage-copy`.
- No command mutates the live original.
- Read/list/status commands against the copy work.
- Existing room/member rows, if present, decode to the canonical
  `delivery_binding` shape.
- JSON/API output does not publish legacy top-level room transport mirror
  fields such as `backend`, `session`, `pane_id`, `transport_backend`,
  `transport_session_id`, or `transport_pane_id`.

Use read-only commands first. Only run isolated writes against the copied store
after confirming the command targets the copy.

### 6. Live Room Relay Smoke

Done when:

- A disposable room/member is created in isolated storage.
- A real available delivery target is used if present: tmux, zellij, herdr, or
  a local pane/socket equivalent.
- The member is bound through the canonical `delivery_binding` path.
- A message or relay operation reaches the target, or the exact missing runtime
  dependency is recorded.
- Room status/member output remains canonical and does not emit legacy mirror
  fields.

If no live pane/socket runtime is available, record this milestone as
unavailable rather than substituting a deterministic fake.

### 7. Frontend Manual Smoke

Done when:

- If straightforward, the GUI/dev server is started from this worktree.
- The following surfaces are opened or otherwise exercised:
  - Rooms
  - RoomControlCenter
  - Orchestration board
  - Logs or flow view
- Broken imports, blank screens, console errors, and obvious layout regressions
  are recorded.
- If the frontend cannot be started locally, record the blocker and rely on the
  completed frontend build/type gates as partial evidence.

### 8. Conditional Live Integration Checks

Done when each conditional path is either proven with a real run or explicitly
marked unavailable.

Checks:

- CoVe real verification: run only if an LLM provider and API key are
  configured. Otherwise record the structured unavailable error.
- RLM LLM executor: run only if a provider/API key is configured.
- RLM REPL executor: run only if the expected local model endpoint, such as
  `127.0.0.1:1234`, is running.
- Pi live call: run only if Pi credentials/service configuration is available.
- Hermes live call: run only if the Hermes service or required credentials are
  available.

The prior acceptable unavailable patterns were:

- CoVe: `no LLM provider configured`
- RLM LLM: structured auth/provider error
- RLM REPL: connection refused to the local model server

### 9. External Or High-Risk Diff Review

Done when:

- If `coderabbit` is installed, run a prompt-only review of the committed diff
  against `gitlab/main` or `main` and save the output to a persistent report
  path.
- If `coderabbit` is unavailable, perform a manual high-risk diff review and
  record the findings.
- The review focuses on accidental active-path deletion, room transport hard-cut
  regressions, DTO/API contract drift, hidden fallback removal, and storage
  compatibility.
- Actionable findings are either fixed and reverified or listed as residual
  risk.

## Verification Report

Create or update:

```text
docs/goals/mr43-merge-readiness-report.md
```

The report must include:

- Branch, commit, MR status, and pipeline status.
- Binary build details.
- Commands run with pass/fail/unavailable status.
- Evidence snippets for high-risk checks.
- Existing-data storage smoke result.
- Live room relay smoke result.
- Frontend manual smoke result.
- Conditional live integration status and exact unavailable reasons.
- External/high-risk review findings.
- Final decision:
  - `merge-ready`
  - `merge-ready-with-known-gaps`
  - `needs-changes`
- Final self-review:
  - What would fail code review?
  - What residual risks remain?
  - Confidence score from 0 to 100.

Run this after editing the report:

```bash
make check-doc-links
git diff --check
```

## Done When

- All required milestones pass.
- Every conditional milestone is either passed with a real check or marked
  unavailable with a concrete reason.
- The report exists and includes the final decision.
- No live storage, production data, or secrets were mutated or exposed.
- If code changed, affected tests and the MR pipeline were rerun and recorded.

## Stop Conditions

- Stop after 3 failed attempts at the same failing command and summarize the
  blocker.
- Stop before changing schemas, public API routes, dependencies, protocol
  envelopes, storage format, or branch history.
- Stop if an existing-data smoke would require mutating live storage. Copy it
  first or ask for direction.
- Stop if a live integration requires unavailable credentials or services.
  Record it as unavailable instead of inventing a substitute.
- Stop if verification uncovers a product decision rather than a mechanical
  fix. Summarize the decision needed and wait for direction.

## Start Command

```text
/goal docs/goals/mr43-merge-readiness-smoke_goal.md
```
