# Code Search Evals

Stable entrypoints for the direct `code_search_ensemble` suites live in three places:

- checked-in datasets under `testdata/evals/code-search-ensemble/`
- checked-in route-alert policies in the same folder
- wrapper scripts under `scripts/`

The wrappers use:

- `AGENTCTL_STORAGE_ROOT` if set
- `AGENTCTL_VAULT_PATH` if set, otherwise `~/.foxctl/templates/obsidian-vault`

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
