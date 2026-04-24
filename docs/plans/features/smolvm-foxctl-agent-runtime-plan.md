# smolvm Foxctl Agent Runtime Plan

## Status

Draft implementation plan.

## Goal

Package a Linux foxctl agent runtime into a smolvm `.smolmachine` so one
top-level agent/RLM run executes inside one hardware-isolated VM. Recursive
`rlm_query` calls stay inside that same guest runtime rather than creating
nested VMs.

This gives foxctl a stronger isolation boundary for model-driven execution
while keeping the host in control of lifecycle, mounting, allowlists, and
artifact collection.

## Target Runtime Shape

```text
host foxctl
  └─ smolvm pack run / machine run
       ├─ repo mounted read-only at /mnt/repo
       ├─ output mounted writable at /mnt/out
       ├─ optional foxctl home/cache mounted at /mnt/.foxctl
       └─ guest foxctl agent runtime
            ├─ parent RLM
            ├─ go_repl/python_repl inside guest
            └─ rlm_query children inside the same guest
```

Isolation unit:

```text
one top-level foxctl sandboxed agent/run = one smolvm
```

Recursive calls are logical RLM subcalls, not new OS isolation domains.
The async recursive scheduler and `rlm_query`/`rlm_wait` fan-out behavior are
planned in
[rlm-recursive-fanout-runtime-plan.md](rlm-recursive-fanout-runtime-plan.md).

## Run/Agent IDs and Output Namespace Contract

Output is always mounted at one shared guest root:

```text
/mnt/out
```

Smoke note from smolvm `0.5.19`: `/workspace` is managed by smolvm and did
not behave as a reliable host bind-mount target in local tests. Host-provided
repo/output mounts should use `/mnt/repo` and `/mnt/out`; `/workspace` can
remain smolvm's own persistent workspace when needed.

Each top-level run gets one readable run namespace and each agent gets one
readable agent namespace under that run:

```text
/mnt/out/runs/<run_id>/
  manifest.json
  events.jsonl
  blackboard.jsonl
  agents/
    <agent_id>/
      trajectory.jsonl
      artifacts/
      scratch/
```

Identifier rules for path segments:

- `run_id` and `agent_id` are deterministic, readable, and filesystem-safe.
- normalize to lowercase `[a-z0-9_-]`; replace other spans with `-`; trim edge
  separators.
- reject empty results after normalization.
- disambiguate collisions with deterministic numeric suffixes (`-2`, `-3`, ...).

Examples:

- run: `Run 2026-04-21 Main` -> `run-2026-04-21-main`
- agent: `Researcher/Child#1` -> `researcher-child-1`
- second colliding agent: `Researcher Child 1` -> `researcher-child-1-2`

Visibility model inside one guest VM:

- `blackboard.jsonl`, run `manifest.json`, run `events.jsonl`, and each
  agent's `artifacts/` and `trajectory.jsonl` are publish/read surfaces.
- `scratch/` is private working state for that agent namespace and must not be
  treated as a cross-agent contract.
- parent/child sharing is done via structured published summaries/artifacts and
  blackboard records, not by reading another agent's scratch tree.

## Why smolvm

Local inspection found `smolvm 0.5.19` installed. Its CLI already supports the
needed first-slice primitives:

- `smolvm pack create --image ... --output ...`
- `smolvm pack create --from-vm ... --output ...`
- `smolvm pack run --sidecar ...`
- `smolvm machine run --image ...`
- `smolvm machine create/start/cp/exec/stop`
- directory mounts with `-v HOST:GUEST[:ro]`
- network off by default
- constrained egress on `machine run` with `--allow-host`, `--allow-cidr`,
  and `--outbound-localhost-only`
- resource caps with `--cpus`, `--mem`, and `--timeout`

Important CLI limitation in smolvm 0.5.19: `pack run` exposes `--net`, but not
the restricted-egress flags. Restricted egress for packed agent runs therefore
needs either a smolvm feature update, a `machine run`/named-machine path that
supports the filters, or a host-side proxy that is the only reachable endpoint.

The local checkout at `~/repos/githubs/smolvm` is useful as reference material,
but its git index currently appears dirty/stale with many tracked files reported
as deleted and reappearing as untracked. Treat it as read-only unless that repo
is repaired separately.

## Non-Goals

- Do not nest smolvm inside smolvm for `rlm_query`.
- Do not mount the host repo writable by default.
- Do not give the guest broad access to the host foxctl stores.
- Do not make the smolvm path the only agent runtime yet.
- Do not solve remote/cloud sandbox deployment in this slice.

## Security Model

Default mounts:

```text
/mnt/repo      read-only repo/worktree snapshot or mount
/mnt/out       writable namespaced run output root (`runs/<run_id>/...`)
/mnt/.foxctl   optional writable guest foxctl home/cache
```

Default network:

```text
network disabled unless an LLM endpoint is explicitly configured
```

Local LMStudio/OpenAI-compatible access should use the smallest possible
egress policy. With `machine run`, that can be:

```bash
--allow-cidr 127.0.0.0/8
```

Local smoke testing showed `--outbound-localhost-only` and
`--allow-host localhost` can fail during VM readiness in smolvm `0.5.19`, while
`--allow-cidr 127.0.0.0/8` successfully reaches host LMStudio at
`127.0.0.1:1234`. Likewise, bare `--net` wrote an unusable guest resolver
(`127.0.0.1`) locally, while `--allow-cidr 0.0.0.0/0` behaved as the broad
egress equivalent and fixed image-pull DNS.

With `pack run` on smolvm 0.5.19, the host planner must not emit that flag
because the command does not support it. In that mode, enabling live LLM access
is a degraded network posture unless it is paired with a host-side proxy or a
future smolvm build that adds restricted egress to packed runs.

Named machine staging has the same DNS constraint as `machine run`: locally,
`machine start` only pulled Alpine reliably when the machine was created with
`--allow-cidr 0.0.0.0/0`. Use that broad rule only while constructing the
package image; the agent run plan should still keep runtime network disabled
unless an LLM endpoint is requested.

If guest localhost is not the host LMStudio endpoint, add a host-side proxy or
gateway discovery layer and allow only that endpoint.

OpenAI/cloud access, when explicitly requested, should use host allowlists:

```bash
--net --allow-host api.openai.com
```

Skill access is enforced twice:

1. Package only selected skills into the `.smolmachine`.
2. Pass the same normalized allowlist into `foxctl agent spawn --skills-allow`.

## Package Placement

Follow `docs/architecture/package-topology.md`.

Runtime-owned sandbox orchestration belongs under:

```text
internal/runtime/sandbox/
internal/runtime/sandbox/smolvm/
```

CLI wiring should reactivate the existing disabled command surface:

```text
cmd/foxctl/cmd/sandbox.go
cmd/foxctl/cmd/sandbox_smolvm.go
```

The current `foxctl sandbox` command is disabled with an OpenSandbox message.
Replace that disabled root with a real `sandbox smolvm` subtree while preserving
error-envelope behavior for unsupported providers.

## CLI Surface

### Probe local LLM reachability

```bash
foxctl sandbox smolvm probe-lmstudio \
  --base-url http://127.0.0.1:1234/v1 \
  --image alpine:3.20
```

Responsibilities:

- run a tiny guest command using smolvm
- test `$base_url/models`
- report which endpoint was tried
- recommend proxy/gateway fallback if unreachable

### Package foxctl agent runtime

```bash
foxctl sandbox smolvm package-plan \
  --output artifacts/smolvm/foxctl-agent \
  --image alpine:3.20 \
  --platform linux/arm64 \
  --no-sign
```

Current `package-plan` responsibilities:

- plan a deterministic `smolvm pack create` argv
- validate one package source (`--image` or `--from-vm`)
- return expected packed executable and sidecar paths

Current `foxctl-package --dry-run=false` responsibilities:

- build Linux foxctl binary (`GOOS=linux GOARCH=arm64 CGO_ENABLED=0`)
- create host staging/output directories
- create/start a named smolvm staging machine with the DNS workaround
- copy the foxctl binary to `/usr/local/bin/foxctl`
- chmod and verify the guest foxctl binary
- stop the staging machine
- call `smolvm pack create --from-vm`
- verify the packed stub with `foxctl-agent run -- /usr/local/bin/foxctl --help`
- optionally delete the staging machine with `--cleanup-machine`
- return paths to:
  - packed executable
  - sidecar `.smolmachine`

Future `package` responsibilities:

- build allowlisted skill binaries for Linux
- stage a minimal runtime filesystem beyond the single foxctl binary
- generate a Smolfile/package manifest when skills are included

Manual smoke verified this staging path on macOS:

```bash
foxctl sandbox smolvm foxctl-package \
  --dry-run=false \
  --output "$out/foxctl-agent" \
  --machine-name "$machine" \
  --no-sign \
  --cleanup-machine
```

### Run a sandboxed agent

```bash
foxctl sandbox smolvm run-agent \
  --sidecar artifacts/smolvm/foxctl-agent.smolmachine \
  --repo . \
  --repo-mode readonly \
  --out /tmp/foxctl-agent-out \
  --role researcher \
  --prompt "Investigate RLM runtime shape" \
  --skills-allow fs_read,fs_tree,code_context_grep,code_symbols,repo_index_search \
  --llm-provider lmstudio \
  --llm-base-url http://127.0.0.1:1234/v1 \
  --llm-model liquid/lfm2.5-1.2b \
  --local-llm-only
```

Host command should translate this into `smolvm pack run` with mounts and env:

```bash
smolvm pack run \
  --sidecar foxctl-agent.smolmachine \
  --net \
  -v "$repo:/mnt/repo:ro" \
  -v "$out:/mnt/out" \
  -w /mnt/repo \
  -e FOXCTL_HOME=/mnt/.foxctl \
  -e FOXCTL_OBS_DIR=/mnt/out/observability \
  -e FOXCTL_LLM_PROVIDER=lmstudio \
  -e FOXCTL_LLM_BASE_URL=http://127.0.0.1:1234/v1 \
  -e FOXCTL_LLM_API_KEY=lm-studio \
  -e FOXCTL_LLM_MODEL="$model" \
  -- \
  foxctl agent spawn \
    --role researcher \
    --prompt "$prompt" \
    --workspace /mnt/repo \
    --slug "$slug" \
    --skills-allow "$allowlist" \
    --llm-provider lmstudio \
    --llm-base-url http://127.0.0.1:1234/v1 \
    --llm-model "$model"
```

For a real LLM smoke, `run-agent` also supports `--ask-question`. In that
mode the guest command spawns the agent with a stable slug, starts
`foxctl agent run <slug>` in the background, sends
`foxctl agent ask <slug> --wait`, writes `spawn.json` and `agent-run.log` under
`/mnt/out/runs/<run_id>/`, then stops the guest daemon process.

The `--net` line above is intentionally explicit: for `pack run` it is broad
egress in smolvm 0.5.19. The command planner should include a warning when
`--local-llm-only` requests restricted egress but the selected backend is
`pack run`.

## Guest Runtime Layout

Recommended staged filesystem before packing:

```text
/opt/foxctl/bin/foxctl
/opt/foxctl/skills/<skill>/skill.yaml
/opt/foxctl/skills/<skill>/bin
/usr/local/bin/foxctl-agent-entrypoint
```

Guest runtime paths:

```text
/mnt/repo       mounted repo
/mnt/out        artifacts and telemetry
/mnt/.foxctl    guest foxctl home/cache/storage
```

Entrypoint behavior:

```sh
#!/bin/sh
set -eu

export PATH="/opt/foxctl/bin:$PATH"
export FOXCTL_HOME="${FOXCTL_HOME:-/mnt/.foxctl}"
export FOXCTL_PATHS_SKILLS="${FOXCTL_PATHS_SKILLS:-/opt/foxctl/skills}"
export FOXCTL_STORAGE_ROOT="${FOXCTL_STORAGE_ROOT:-$FOXCTL_HOME/storage}"
export FOXCTL_PATHS_CAS="${FOXCTL_PATHS_CAS:-$FOXCTL_HOME/cas}"
export FOXCTL_OBS_DIR="${FOXCTL_OBS_DIR:-/mnt/out/observability}"

exec "$@"
```

## Skill Packaging

Inputs:

```text
--skills-allow fs_read,fs_tree,code_context_grep,...
```

Normalize the allowlist with the same runtime tool-name rules used by agent
spawn. Then resolve each allowed skill to a source directory under `skills/`.

Build rules:

- Go skills:
  ```bash
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o <stage>/skills/<name>/bin ./skills/<src>
  ```
- WASI skills:
  copy `module.wasm` and manifest unchanged.
- Non-portable skills:
  fail closed unless explicitly marked as host-only or excluded.

Manifest output:

```json
{
  "platform": "linux/arm64",
  "skills": [
    {
      "requested": "fs_read",
      "normalized": "fs/read",
      "source": "skills/fs_read",
      "staged": "/opt/foxctl/skills/fs_read",
      "kind": "go"
    }
  ]
}
```

## RLM Behavior Inside The VM

In the full-agent-in-smolvm path:

- `rlm_query` runs child RLMs inside the guest foxctl process.
- No additional smolvm instances are launched for recursive subcalls.
- Parent and child RLMs share the same VM boundary.
- REPL sessions may be separate per child to avoid accidental state mutation.
- Recursion is controlled by existing depth/subcall/token budgets.

## Host/Guest LLM Policy

For full guest agent runtime, the guest needs LLM access.

Preferred local dev policy:

```text
--outbound-localhost-only + FOXCTL_LLM_BASE_URL=<local OpenAI-compatible endpoint>
```

Open questions to resolve in `probe-lmstudio`:

- Does guest `127.0.0.1` reach host LMStudio?
- Does smolvm expose a stable host gateway address/name?
- Do we need a host-side loopback proxy?

Fallback:

- host starts a tiny proxy bound to the smolvm-reachable interface
- guest gets only that endpoint
- proxy forwards to host LMStudio

## Artifacts

Every sandboxed run writes under a run namespace rooted at:

```text
/mnt/out/runs/<run_id>/
```

Required run-level files:

```text
manifest.json        run metadata (ids, role, prompt hash, policy flags)
events.jsonl         ordered run-level events
blackboard.jsonl     structured cross-agent published records
agents/              per-agent namespaces
```

Required per-agent files:

```text
agents/<agent_id>/trajectory.jsonl
agents/<agent_id>/artifacts/
agents/<agent_id>/scratch/
```

The host summarizes these paths and never directly applies guest changes to the
host repo. Treat sandbox output as proposals.

## Implementation Slices

### Slice 1: smolvm adapter package

Owner: worker A

Files:

```text
internal/runtime/sandbox/smolvm/
```

Deliver:

- command builder for `smolvm`
- version/probe helpers
- `RunPack`, `RunMachine`, and `ProbeEndpoint`
- deterministic args tests
- no real VM required in unit tests

### Slice 2: packaging planner

Owner: worker B

Files:

```text
internal/runtime/sandbox/smolvm/package.go
internal/runtime/sandbox/smolvm/skills.go
internal/runtime/sandbox/smolvm/output_layout.go
internal/runtime/sandbox/smolvm/output_layout_test.go
```

Deliver:

- package plan type
- skill allowlist normalization/resolution
- stage tree builder
- Linux build command generation
- Smolfile renderer
- manifest renderer
- output layout planner (run/agent readable IDs + path contract)
- dry-run tests

### Slice 3: CLI command surface

Owner: worker C

Files:

```text
cmd/foxctl/cmd/sandbox.go
cmd/foxctl/cmd/sandbox_smolvm.go
```

Deliver:

- `foxctl sandbox smolvm probe-lmstudio`
- `foxctl sandbox smolvm package`
- `foxctl sandbox smolvm run-agent`
- JSON envelope outputs
- dry-run mode for package/run-agent
- tests for flags and envelope shape

### Slice 4: guest entrypoint and smoke fixture

Owner: worker D

Files:

```text
configs/sandbox/smolvm/foxctl-agent-entrypoint.sh
configs/sandbox/smolvm/Smolfile.template
testdata/sandbox/smolvm/
```

Deliver:

- entrypoint script
- default Smolfile template
- minimal skill allowlist fixture
- smoke docs

### Slice 5: live smoke and hardening

Owner: coordinator/reviewer

Deliver:

- run `probe-lmstudio`
- package minimal image with one or two read-only skills
- run guest `foxctl version`
- run guest `foxctl agent spawn --dry-run`
- if local LLM works, run one small live agent prompt
- verify host repo is mounted read-only
- verify telemetry lands in `/mnt/out`

Smoke status:

- `run-agent --agent-dry-run` passed with the packaged foxctl binary and
  `/mnt/repo` + `/mnt/out` mounts.
- `run-agent --ask-question` reached LMStudio from inside the packed VM and
  returned an `agent.reply`; the daemon log recorded prompt/completion tokens.
- Researcher role currently attempts repo tools in the guest and exposed two
  runtime integration issues: `repo_index_search` reported `sql: database is
  closed`, and `repo_index_dag_grep` was not registered under the expected tool
  name. Companion role completed a no-tool LLM reply.

## Acceptance Criteria

- `foxctl sandbox smolvm package --dry-run` produces a deterministic package
  plan.
- `foxctl sandbox smolvm run-agent --dry-run` emits the exact `smolvm pack run`
  command it would execute.
- Packaged runtime includes only allowlisted skills.
- Runtime agent command still receives `--skills-allow`.
- Repo mount defaults to read-only.
- Network defaults to disabled unless LLM access is explicitly requested.
- Local LLM mode does not enable broad internet egress.
- `rlm_query` recursion does not launch nested smolvm instances.
- run output is namespaced under `/mnt/out/runs/<run_id>/agents/<agent_id>`.
- readable run/agent IDs are deterministic and collision-safe.
- cross-agent sharing uses blackboard/published artifacts; scratch is not shared.
- Unit tests do not require smolvm/hypervisor availability.
- Live smoke is optional and skipped when `smolvm` is missing.

## Risks

| Risk | Mitigation |
| --- | --- |
| Linux guest cannot use macOS-built skills | Cross-compile selected Go skills for `linux/arm64`; fail closed for non-portable skills |
| Guest cannot reach host LMStudio via localhost | Add `probe-lmstudio`; support host proxy/gateway fallback |
| Writable repo mount corrupts host checkout | Default to `:ro`; require explicit `--repo-mode writable` |
| Too-large image from shipping all skills | Package allowlist only |
| Guest gets broad store access | Use guest-local `FOXCTL_HOME`; only mount explicit cache/home when requested |
| smolvm local checkout has stale git state | Use installed `smolvm` CLI; do not depend on checkout state |

## Next Step

Start with slices 1 and 2 in parallel:

- Worker A builds the command adapter and probe primitives.
- Worker B builds the packaging planner and skill staging logic.

The coordinator should keep slice 3 until both package APIs stabilize, then wire
the CLI around the reviewed internal API.
