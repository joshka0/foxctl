# Code Search Evals

Stable entrypoints for the direct `code_search_ensemble` suites live in three places:

- checked-in datasets under `testdata/evals/code-search-ensemble/`
- checked-in route-alert policies in the same folder
- wrapper scripts under `scripts/`

The wrappers use:

- `FOXCTL_STORAGE_ROOT` if set
- `FOXCTL_VAULT_PATH` if set, otherwise `~/.foxctl/templates/obsidian-vault`

## Common Runs

Use these make targets:

- `make eval-code-search-foxctl-package`
- `make eval-code-search-foxctl-repo-grounded`
- `make eval-code-search-foxctl-change-impact`
- `make eval-code-search-foxctl-trace-symbol`
- `make eval-code-search-foxctl-bridge-esoteric`

For the checked-in `praze` infra smoke:

- `PRAZE_WORKSPACE=/path/to/praze make eval-code-search-praze-infra`

All of these targets call checked-in wrapper scripts which in turn call:

- `foxctl eval code-search-ensemble`
- `--eval-dataset-file <checked-in dataset>`
- `--policy-file <checked-in policy>`
- `--tool-profile repo-grounded`
- `--include-contextwiki`

## Datasets

- `testdata/evals/code-search-ensemble/foxctl-package.jsonl`
- `testdata/evals/code-search-ensemble/foxctl-repo-grounded.jsonl`
- `testdata/evals/code-search-ensemble/foxctl-change-impact.jsonl`
- `testdata/evals/code-search-ensemble/foxctl-trace-symbol.jsonl`
- `testdata/evals/code-search-ensemble/foxctl-bridge-esoteric.jsonl`
- `testdata/evals/code-search-ensemble/praze-infra-smoke.jsonl`

## Policies

- `testdata/evals/code-search-ensemble/foxctl-package-policy.yaml`
- `testdata/evals/code-search-ensemble/foxctl-repo-grounded-policy.yaml`
- `testdata/evals/code-search-ensemble/foxctl-change-impact-policy.yaml`
- `testdata/evals/code-search-ensemble/foxctl-trace-symbol-policy.yaml`
- `testdata/evals/code-search-ensemble/foxctl-bridge-esoteric-policy.yaml`
- `testdata/evals/code-search-ensemble/praze-infra-policy.yaml`

## Failure Mode

Route-family alerts are informational by default.

To make a suite fail in CI or local gating, set:

- `--fail-on-route-alerts`

You can also override thresholds directly:

- `--min-primary-anchor`
- `--max-secondary-anchor`
- `--min-package-repo-evidence`
- `--min-infra-declarative-companion`

If a checked-in policy file already sets those values, passing the flag again overrides the file.

For ContextWiki retrieval evals, see [retrieval-evals.md](retrieval-evals.md).

## `gather_context` Polyglot Fixture

Use the polyglot fixture before tuning repo-provider behavior against foxctl.
It is a tiny held-out workspace covering Go, TypeScript, Python, Elixir, and
repo documentation map tasks.

```bash
./bin/foxctl eval gather-context \
  --workspace testdata/fixtures/gather-context/polyglot-repo \
  --eval-dataset-file testdata/evals/gather-context/polyglot-fixture.jsonl \
  --case-limit 10 \
  --lane code \
  --tool-profile gather-context \
  --limit 10 \
  --max-context-chars 7000 \
  --report-file /tmp/foxctl-polyglot-gather.report.json
```

Track stage-specific failures separately:

- provider miss: expected path absent from raw evidence
- reduction miss: expected path present in raw evidence but absent from selected paths
- fact/certificate miss: path set is correct but the bundle is not certified or key facts are missing

`--case-limit` bounds dataset rows. `--limit` bounds gathered evidence sources
inside each case. Keep those separate when running quick smoke tests.

## Native Subagent Baselines

When comparing `gather_context` or Lambda RLM against a native Codex mini
explorer, record token usage from the subagent transcript rather than the
external JSONL scorer alone. If a rollout transcript exists, point the external
baseline row at it with `transcript_path`; the eval loader reads the latest
Codex `token_count` event and distributes shared transcript tokens across the
rows that reference it.

Some spawned subagents do not write a normal `~/.codex/sessions/...jsonl`
rollout. In that case, use the subagent `thread_id` to query Codex logs:

```bash
sqlite3 ~/.codex/logs_2.sqlite \
  "select feedback_log_body from logs where thread_id='<thread-id>' and feedback_log_body like '%post sampling token usage%' order by id;"
```

For the 2026-04-30 native mini explorer baseline on
`foxctl-trace-symbol.jsonl`, subagent `019de077-6141-7fb2-8299-9633ab5f3722`
reported this `response.completed` usage for the whole native explorer run:

```text
input_tokens=61854
cached_input_tokens=61312
output_tokens=4109
reasoning_output_tokens=2864
total_tokens=65963
estimated_token_count=61997
```

That was one Codex user turn / Responses websocket request, but it included the
subagent's full search-and-read exploration. Treat it as a batched-run total;
per-case token estimates are only comparable to one-case-per-agent runs if the
test harness distributes or records the shared transcript usage explicitly.

The matching deterministic foxctl run on the same dataset is:

```bash
make eval-code-search-foxctl-trace-symbol \
  ARGS='--report-file /private/tmp/foxctl-benchmarks/code-search-foxctl-trace-symbol.json'
```

The 2026-05-14 local artifact at
`/private/tmp/foxctl-benchmarks/code-search-foxctl-trace-symbol-20260514T001000Z.json`
reported `6` cases, mean path recall `1.00`, mean duration `11.75s`, mean loaded
token estimate `2,144`, mean emitted token estimate `703`, and mean parent
input-token savings estimate `1,441`. Use this only as a dataset-matched
deterministic-vs-native token-class comparison until a native transcript is
parsed into quality rows.

The offline orientation harness does not spawn native subagents:

```bash
RUN_GATHER_CONTEXT=0 make bench-orientation
```

It produces shell and cold tool budget artifacts suitable for homepage
context-size and latency claims. Native explorer rows still need to be imported
through `foxctl eval agents --external-results <jsonl>` or
`foxctl eval gather-context --agent-baseline-results <jsonl>` so path recall,
duration, token usage, and cost are compared under the same report contract.
Use `foxctl eval agents --case-limit N` for smoke runs; it bounds dataset rows
the same way `gather_context --case-limit` does.

The 2026-05-13 benchmark epic attempted a fresh one-case provider-backed
`openai:gpt-5.4-mini` row and saved the failed report at
`/private/tmp/foxctl-benchmarks/native-gpt54mini-case1-20260513T234500Z.json`.
It did not produce comparable measurements because engine creation failed with
missing bearer API-key configuration. Treat that artifact as an environment
blocker, not as a performance baseline. The imported head-to-head smoke
at
`/private/tmp/foxctl-benchmarks/gather-context-head-to-head-native-error-20260514T000500Z.json`
records the failed provider-backed row as `baseline_error_count: 1`.

An authenticated rerun through OpenRouter then exercised the foxctl eval-agent
runtime with a provider-backed researcher:

```bash
./bin/foxctl eval agents \
  --workspace . \
  --eval-dataset-file testdata/evals/gather-context/foxctl-repo-grounded.jsonl \
  --case-limit 1 \
  --role researcher \
  --target openrouter:openai/gpt-5.4-mini \
  --timeout 6m \
  --max-iterations 8 \
  --report-file /private/tmp/foxctl-benchmarks/native-openrouter-gpt54mini-case1.json
```

The cleaned local artifact at
`/private/tmp/foxctl-benchmarks/native-openrouter-gpt54mini-case1-fixed-20260513T233351Z.json`
completed without adapter errors, but failed the eval: pass rate `0.00`, mean
quality `0.14`, `0.00` path/fact recall, `89.5s`, `199,620` total tokens, and
`9` tool calls. The matching imported head-to-head report at
`/private/tmp/foxctl-benchmarks/gather-context-head-to-head-openrouter-native-fixed-after-reindex-priced-20260513T233351Z.json`
shows `gather_context` passing the same case with path recall `0.86`, fact
recall `1.00`, duration `10.52s`, `8.5x` duration speedup, and `$0.19962`
baseline normalized cost at `$1/Mtok`.

Do not cite that OpenRouter artifact as a local Codex native-subagent baseline.
It used the foxctl `eval agents` runtime path with an OpenRouter target. The
local Codex native-subagent baseline is the transcript-derived row above unless
a fresh `spawn_agent`/Codex transcript is imported with `runner:
codex-native-subagent` or `runner: spawn_agent`.

The 2026-05-14 clean local Codex native-subagent smoke used a `spawn_agent`
explorer named `Faraday` on the first `gather_context` case and imported the
result as `runner: codex-native-subagent`:

```bash
./bin/foxctl index repo build --workspace . --go --typescript --elixir

./bin/foxctl eval gather-context \
  --workspace . \
  --eval-dataset-file testdata/evals/gather-context/foxctl-repo-grounded.jsonl \
  --case-limit 1 \
  --tool-profile gather-context \
  --lane code \
  --limit 8 \
  --max-context-chars 12000 \
  --timeout 3m \
  --agent-baseline-results /private/tmp/foxctl-benchmarks/codex-native-subagent-case1-20260514T060039Z.jsonl \
  --report-file /private/tmp/foxctl-benchmarks/gather-context-head-to-head-codex-native-subagent-case1-fresh-index-20260514T061655Z.json
```

That report shows repoindex freshness `current` and `gather_context` passing in
`6.50s` with path recall `0.86`, fact recall `1.00`, emitted context `1,096`
chars, and `31.42x` duration speedup. The local Codex subagent took `204.3s`,
had path recall `1.00`, but scored fact recall `0.14`, so the eval marked it
failed. Token budget fields from the Codex transcript were `1,202,987` total
tokens, `1,194,936` input tokens, `1,049,728` cached input tokens, `8,051`
output tokens, and `3,658` reasoning output tokens.

Read that row critically: it is a dirty-worktree smoke, and the incremental
repoindex request fell back to a full graph rebuild because partial rebuilds for
global edges are not supported yet. The subagent answer named the right paths
but did not repeat enough exact required fact strings for the current fact
scorer. It is useful evidence for the local Codex import path, latency, and
budget shape, not a final quality claim.

Always check repoindex freshness before citing a gather-context/baseline
comparison:

```bash
./bin/foxctl index repo status --workspace .
```

If status reports stale metadata, a dirty-hash mismatch, or `nodes_total: 0`,
refresh the index before the gather side of the comparison:

```bash
./bin/foxctl index repo build --workspace . --go --typescript --elixir
```

The 2026-05-14 fresh-index rerun requested an incremental build. It detected a
small dirty-worktree delta, fell back to a full graph rebuild for global-edge
correctness, and then reported repoindex freshness `current`.

## `gather_context` Pilot Harness

Use the checked-in pilot wrapper for active cross-repo runs:

```bash
RUN_REPOS=praze,heartwood,dearday \
RUN_PROVIDER_AGENT=1 \
RUN_RLM=0 \
REBUILD_INDEX=0 \
scripts/evals/run-gather-context-pilots.sh
```

The default run uses existing local checkouts:

- `PRAZE_REPO`, defaulting to `~/repos/personal/praze`
- `HEARTWOOD_REPO`, defaulting to `~/repos/personal/heartwood`
- `DEARDAY_REPO`, defaulting to `~/repos/personal/dearday`
- `OVERCHARGE_REPO`, defaulting to `~/repos/personal/overcharge`

For reproducible pilot snapshots, opt into fresh clones under `/tmp`:

```bash
FRESH_CLONE=1 \
CLONE_ROOT=/tmp/gather-context-pilot-repos/$(date -u +%Y%m%dT%H%M%SZ) \
RUN_REPOS=praze,heartwood,dearday \
scripts/evals/run-gather-context-pilots.sh
```

Fresh clone mode is intentionally opt-in and non-destructive. The harness
creates a new clone directory and fails if a destination already exists. If a
repo needs a source other than the local checkout's `origin`, set
`PRAZE_REPO_URL`, `HEARTWOOD_REPO_URL`, `DEARDAY_REPO_URL`, or
`OVERCHARGE_REPO_URL`.

Each repo output directory includes `repo-state.txt` and `repo-state.json`.
Every generated JSON report also embeds the same object as top-level
`repo_state`, including the workspace path, Git worktree flag, HEAD SHA,
branch, origin URL, dirty flag, and short status. Use that field when comparing
pilot reports across machines or dates.

The active cross-repo pilot set should stay small and biased toward repos with
valid Git metadata and representative language coverage:

- `praze`: Elixir + TypeScript mixed app.
- `heartwood`: TypeScript app/backend.
- `dearday`: Python backend plus TypeScript/Expo frontend.

`turtlr-v2` is intentionally omitted from the active pilot set for now. The
available checkout shape includes nested `worktrees/` duplicates and stale local
Git metadata, so it is a poor signal for generic retrieval quality until the
harness has stronger path-exclusion enforcement.

`overcharge` is useful as a C#/Rust pilot. It has Rust server code plus C#
Godot client code. Build its repoindex with Rust, C#, and tests enabled:

```bash
foxctl index repo build --workspace "$OVERCHARGE_REPO" \
  --go=false --typescript=false --python=false --rust --csharp --include-tests
```

C# repoindex support is intentionally lightweight and tree-sitter based. It
extracts:

- `.cs` file discovery and declaration summaries.
- class/method/property symbol candidates.
- test companion closure for `*.Tests/*.cs`.
- Godot client structure signals such as `client/Scripts/*`.

Overcharge is now a reasonable pilot for path recall, but it should not be the
only C# quality gate until import/project references are indexed beyond
best-effort symbol and call edges.

Harness hardening should come before more scoring tweaks:

1. Keep each pilot repo on a valid Git object store or use `FRESH_CLONE=1`.
2. Record index metadata in every report.
3. Apply `excluded_paths` to provider retrieval, not only final scoring.
4. Report provider recall separately from reducer recall and final-answer recall.
5. Keep generated, dependency, nested worktree, and vendored paths suppressed by
   default unless explicitly requested by the case.
6. Keep deterministic `gather_context` runs separate from local Codex
   native-subagent baselines, then import transcript token usage for fair cost
   comparison.
