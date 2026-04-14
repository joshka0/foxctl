#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VAULT_PATH="${AGENTCTL_VAULT_PATH:-$HOME/.foxctl/templates/obsidian-vault}"
WORKSPACE="${PRAZE_WORKSPACE:-${1:-}}"

if [[ -z "${WORKSPACE}" ]]; then
  echo "usage: PRAZE_WORKSPACE=/path/to/praze $0 [extra foxctl flags]" >&2
  exit 1
fi

if [[ "${1:-}" == "${WORKSPACE}" ]]; then
  shift
fi

cd "$ROOT"
exec env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 go run -tags=libsqlite3 ./cmd/foxctl \
  eval code-search-ensemble \
  --workspace "$WORKSPACE" \
  --vault-path "$VAULT_PATH" \
  --eval-dataset-file "$ROOT/testdata/evals/code-search-ensemble/praze-infra-smoke.jsonl" \
  --policy-file "$ROOT/testdata/evals/code-search-ensemble/praze-infra-policy.yaml" \
  --tool-profile repo-grounded \
  --include-aca \
  "$@"
