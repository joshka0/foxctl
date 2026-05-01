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
- `--include-aca`

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

For ACA retrieval evals, see [retrieval-evals.md](retrieval-evals.md).

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
