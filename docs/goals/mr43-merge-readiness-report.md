# MR 43 Merge Readiness Report

Status: `gap-fixes-verified-with-remaining-architecture-debt`
Generated: 2026-05-23
Worktree: `/home/dev/repos/foxctl-code-quality-cleanup`
Branch: `feat/code-quality-cleanup`
Commit: `038c8300cedfa278b6c01e7e8129900dbd06188d`
Merge request: `https://gitlab.com/joshka0/foxctl/-/merge_requests/43`

## Decision

MR 43 had previously been merge-ready with known gaps. The follow-up gap-fix
slice has now closed the stale installed-plugin checks, the `pi-extension`
participant semantics gap, the missing browser-render smoke, and the no-auth
local model configuration gap for CoVe/RLM.

The remaining items are architecture follow-ups rather than known behavioral
blockers:

- Real CoVe/RLM model answers still require an available provider or local
  endpoint. The branch now has explicit local LMStudio no-auth support and
  tests, but this machine still has no model endpoint listening.
- `packages/data/src/types.ts` and `cmd/foxctl/cmd/room.go` remain large
  Modules that should be decomposed in follow-up slices.

## Gap Fixes Applied

Applied after the initial readiness report:

- Repointed installed Pi and Hermes integrations to this worktree with
  `scripts/doctor-pi-hermes-integrations.sh --apply`.
- Verified Pi/Hermes from this checkout:
  - `bun run --cwd integrations/pi check`
  - real Pi `memory-blur` call returned `{"shape":"mr43-gap-fix","status":"ok"}`
  - `python3 -m unittest integrations.hermes.test_client`
  - Hermes health via `http://localhost:8091/api/health`
- Made `pi-extension` delivery a first-class viewer/inbox capability:
  - `transport_kind:"pi-extension"`
  - `delivery_capability:"viewer_inbox"`
  - not push-relayable and skipped by room relay
- Added focused Go and GUI tests for Pi-extension participant status,
  API transport status, relay skipping, and GUI transport-kind rendering.
- Added local no-auth model support for CoVe and RLM:
  - CoVe accepts explicit or env-signaled `lmstudio` without a fake API key.
  - RLM CLI config sets `auth_mode:"none"` for local LMStudio when no key is
    configured.
- Added `bun run smoke:gui-browser`, a dependency-free Chrome DevTools smoke
  that starts an isolated foxctl backend, starts Vite with that backend target,
  navigates `/`, `/#rooms`, and `/#orchestration`, verifies expected headings,
  and fails on browser console/runtime errors.

## Branch And MR State

| Check | Result |
| --- | --- |
| `git status --short` | Only goal/report docs were untracked during this verification pass. |
| Branch | `feat/code-quality-cleanup` |
| Commit | `038c8300cedfa278b6c01e7e8129900dbd06188d` |
| Diff size against `gitlab/main` | `366 files changed, 12313 insertions(+), 10486 deletions(-)` |
| MR state | Open, not draft |
| Merge status | `mergeable`, `can_be_merged`, no conflicts |
| Blocking discussions | Resolved |
| Approvals | Required `0`, left `0` |
| Head pipeline | `2547557012`, `success`, finished `2026-05-23T02:48:23.322Z` |

`glab` was not installed, so MR state was checked through the GitLab API. No
tokens or credentials are included in this report.

## Binary Build

| Check | Result |
| --- | --- |
| `make build` | Passed |
| `./bin/foxctl version` | Passed |
| Binary path | `./bin/foxctl` |
| Binary version/commit | `038c8300` |
| Go version | `go1.26.3` |
| Build date | `2026-05-23T08:26:05Z` |
| Size | `142M` |
| SHA-256 | `bcccbfe28c5d41bb13c412592d25129f2b313a04d7cda27249e8932cafc3c53d` |

Repoindex subprocesses needed `PATH=/usr/local/go/bin:$PATH`; otherwise the Go
indexer could not find `go`.

## Tool Smoke

| Command area | Result | Evidence |
| --- | --- | --- |
| `index repo build --go --typescript --elixir` | Passed | `525` packages, `1876` files, `33459` symbols, `35087` nodes, `156358` edges |
| `index repo search RoomDeliveryBinding` | Passed | Found Go domain, CLI merge/update, storage, and TypeScript DTO anchors |
| `code/dag_grep` | Passed | Explanation graph connected `RoomMember`, `NormalizeRoomMember`, `mergeRoomDeliveryBinding`, and `RoomDeliveryBinding` |
| `repo/index_dag_grep` | Passed | Explanation graph connected canonical binding, submit mode constants, and health constants |
| `fs/ls` | Passed | Basic skill execution OK |
| `text/grep` | Passed | Found room delivery-binding references |
| `code/symbols` | Passed | Symbol extraction OK |
| `git/status` | Passed | Structured git status OK |
| `code/context_grep` | Passed | Context grep OK |
| `code/smart_search` | Passed | Command OK, but relevance was noisy for this query |
| `code/imports` | Passed | Import analysis OK |
| `code/stats` | Passed | Repository stats OK |
| `code/security` | Non-blocking noisy pass | Returned high-risk static findings, but top examples were broad pattern matches such as `exec.CommandContext(... "git" ...)` and documentation text. No confirmed blocker was identified in this pass. |

## Tests And Type Gates

| Gate | Result |
| --- | --- |
| Focused Go tests for room member JSON, normalization, storage update, binding, and room control | Passed |
| `bun run --cwd integrations/pi check` | Passed |
| `python3 -m unittest integrations.hermes.test_client` | Passed, `4` tests |
| `bun run --cwd packages/foxterm typecheck` | Passed |
| `bun run unused:frontend` | Passed, including frontend check, lint, dead export scan, GUI build, and GUI tests |
| Clean `make test` | Passed |
| Gap-fix touched Go packages | Passed: `go test ./internal/domain/agent ./internal/interfaces/web/api ./internal/intelligence/verification ./skills/cove_verify ./internal/rlm ./cmd/foxctl/cmd -count=1` |
| Gap-fix browser smoke | Passed: `bun run smoke:gui-browser` rendered `/`, `/#rooms`, and `/#orchestration` in headless Chrome with expected headings |
| Gap-fix binary build | Passed: `make build` |
| Gap-fix docs/whitespace | Passed: `make check-doc-links` and `git diff --check` |

The first broad `make test` run failed only when global smoke-test storage/CAS
overrides were exported. Focused reruns of the failing tests passed without the
`FOXCTL_STORAGE_ROOT` and `FOXCTL_PATHS_CAS` overrides, and a clean full
`make test` then passed. Use persistent `TMPDIR`, `GOTMPDIR`, and `GOCACHE` for
test stability, but do not globally override foxctl storage/CAS for repository
test runs.

`make lint` and `make check` were not rerun locally in full. The MR head
pipeline is green, and the local high-value gates above passed.

## Existing-Data Storage Smoke

A live storage root was found at `/home/dev/.foxctl/storage` and copied to:

```text
/var/tmp/foxctl-codex/mr43-storage-copy-20260523T0829Z
```

The copy was about `602M`. The original storage was not mutated.

Read/list/status commands against the copied store passed:

- `./bin/foxctl room list --workspace /home/dev/repos/foxctl --limit 10`
- `./bin/foxctl room show alpha --workspace /home/dev/repos/foxctl --limit 5`
- `./bin/foxctl room status alpha --workspace /home/dev/repos/foxctl`

The copied `board.db` had `room_members` data encoded in storage columns such
as `transport_endpoint`, `transport_kind`, `delivery_submit_mode`,
`delivery_health`, and `delivery_fallback_policy`. The CLI decoded the existing
row to canonical API output:

```json
"delivery_binding":{"transport_endpoint":"p_21","transport_kind":"pi-extension","submit_mode":"newline","health":"unknown"}
```

Greps against `room show` output found no legacy top-level transport mirror
fields such as `backend`, `session`, `pane_id`, `transport_backend`,
`transport_session_id`, `transport_pane_id`, `fallback_policy`, `legacy_mux`,
or `legacy_bound`.

Follow-up status: `pi-extension` rows now decode to a first-class
viewer/inbox capability. They remain intentionally non-triggerable by the push
relay path because there is no mux/pane or pane-socket endpoint to push into.

## Live Room Relay Smoke

A disposable Herdr workspace was created because `tmux` was unavailable and no
active `zellij` session existed. The smoke used isolated foxctl storage at:

```text
/var/tmp/foxctl-codex/live-room-storage
```

The disposable member was bound through the canonical shape:

```json
"delivery_binding":{
  "mux_backend":"herdr",
  "mux_pane_id":"w6527802262c4f2-1",
  "transport_endpoint":"herdr::w6527802262c4f2-1",
  "transport_kind":"mux_pane",
  "submit_mode":"newline",
  "health":"unknown"
}
```

`room loop` delivered token `MR43_SMOKE_1779525365` to the Herdr pane:

```json
"relay":{"backend":"herdr","delivered_count":1,"failed_count":0,"delivered_to":["w6527802262c4f2-1"]}
```

The token was visible in `herdr pane read`, and disposable `room show` output
remained canonical with no legacy mirror fields. The disposable Herdr workspace
was closed after the smoke.

## Frontend Smoke

`bun run unused:frontend` passed, including:

- `check:frontend`
- frontend lint
- frontend dead export scan
- GUI Vite build
- GUI tests, `3` passing
- oxlint, `0` warnings/errors

The GUI preview server also started from this worktree:

```bash
bun run --cwd packages/gui-agent preview --host 127.0.0.1 --port 41743
```

Route responses returned HTTP `200` for:

- `/`
- `/rooms`
- `/orchestration`

Follow-up status: `bun run smoke:gui-browser` now covers a real browser render
pass. It starts an isolated foxctl backend, starts Vite with that backend as
`FOXCTL_GUI_API_TARGET`, opens Chrome headlessly through the DevTools protocol,
and verifies `/`, `/#rooms`, and `/#orchestration` render the expected
headings without app console/runtime errors.

## Conditional Integrations

| Integration | Result | Evidence |
| --- | --- | --- |
| CoVe real verification | Extended gate, no endpoint in this environment | No provider/local endpoint is configured; local `lmstudio` no-auth resolution is now covered by tests and a direct closed-port smoke reaches the request path without API-key validation failure. |
| RLM inspect executor | Passed | Returned an inspect-mode answer in one iteration |
| RLM LLM executor | Extended gate, no endpoint in this environment | Current failure is local endpoint connection refusal when no model server is listening; CLI config now uses `auth_mode:"none"` for local LMStudio without a key. |
| RLM REPL executor | Unavailable | Local endpoint `127.0.0.1:1234` refused connection |
| Pi direct SDK call | Passed | `integrations/pi memory-blur` returned `{"shape":"mr43-gap-fix","status":"ok"}` using local Pi agent auth |
| Pi installed extension doctor | Passed | `scripts/doctor-pi-hermes-integrations.sh --apply` points `/home/dev/.pi/extensions/foxctl.ts` at this worktree and validates expected markers. |
| Hermes unit client | Passed | `python3 -m unittest integrations.hermes.test_client` passed |
| Hermes live health via Python client | Passed | `http://localhost:8091/api/health` returned JSON `ok: true` |
| Hermes installed plugin doctor | Passed | `scripts/doctor-pi-hermes-integrations.sh --apply` points `/home/dev/.hermes/plugins/foxctl` at this worktree and validates expected markers. |

## High-Risk Review

No active-path deletion blocker was found for the canonical room
delivery-binding hard cut. The strongest evidence is the combination of:

- Repo graph paths linking Go domain types, CLI merge/update paths, storage, and
  TypeScript DTOs.
- Focused canonical JSON/storage tests passing.
- Existing-data decode from a copied live store.
- A real Herdr-backed room relay using canonical `delivery_binding`.
- Frontend DTO/type gates and GUI build/tests passing.

The main architecture residue is not a merge blocker for this verification
goal, but it should remain on the cleanup backlog:

- `packages/data/src/types.ts` is about `1495` lines and remains a large shared
  Interface module.
- `cmd/foxctl/cmd/room.go` remains very large and should be decomposed by
  command responsibility over future slices.
- Real model-backed CoVe/RLM answers remain an extended gate that depends on an
  available provider or local endpoint.

## Final Self-Review

What would fail code review:

- The branch still contains large Modules that are difficult to scan, especially
  `cmd/foxctl/cmd/room.go` and `packages/data/src/types.ts`.
- `code/security` currently has a high false-positive rate and should not be
  treated as a strict merge gate without triage.

Residual risks:

- LLM-backed CoVe/RLM answer quality is not proven without a provider/local
  endpoint; only provider resolution and unavailable/local-endpoint behavior are
  proven here.

Confidence: `92/100`.

Final decision: `gap-fixes-verified-with-remaining-architecture-debt`.
