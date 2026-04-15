#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VAULT_PATH="${FOXCTL_VAULT_PATH:-$HOME/.foxctl/templates/obsidian-vault}"
WORKSPACE="${CODE_SEARCH_WORKSPACE:-$ROOT}"
DATASET="${CODE_SEARCH_DATASET:-}"
POLICY="${CODE_SEARCH_POLICY:-}"

if [[ -z "$DATASET" ]]; then
  echo "CODE_SEARCH_DATASET is required" >&2
  exit 1
fi

ARGS=(
  eval code-search-ensemble
  --workspace "$WORKSPACE"
  --vault-path "$VAULT_PATH"
  --eval-dataset-file "$DATASET"
  --tool-profile repo-grounded
  --include-aca
)

if [[ -n "$POLICY" ]]; then
  ARGS+=(--policy-file "$POLICY")
fi

cd "$ROOT"
exec env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 go run -tags=libsqlite3 ./cmd/foxctl "${ARGS[@]}" "$@"
