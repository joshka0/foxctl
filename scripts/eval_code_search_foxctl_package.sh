#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VAULT_PATH="${AGENTCTL_VAULT_PATH:-$HOME/.foxctl/templates/obsidian-vault}"

cd "$ROOT"
exec env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 go run -tags=libsqlite3 ./cmd/foxctl \
  eval code-search-ensemble \
  --workspace "$ROOT" \
  --vault-path "$VAULT_PATH" \
  --eval-dataset-file "$ROOT/testdata/evals/code-search-ensemble/foxctl-package.jsonl" \
  --policy-file "$ROOT/testdata/evals/code-search-ensemble/foxctl-package-policy.yaml" \
  --tool-profile repo-grounded \
  --include-aca \
  "$@"
