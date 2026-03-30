#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VAULT_PATH="${AGENTCTL_VAULT_PATH:-$HOME/.agentctl/templates/obsidian-vault}"
WORKSPACE="${RETRIEVAL_WORKSPACE:-$ROOT}"
SUITE="${RETRIEVAL_SUITE:-}"
LIMIT="${RETRIEVAL_LIMIT:-}"
FORMAT="${RETRIEVAL_FORMAT:-}"
POLICY_FILE="${RETRIEVAL_POLICY_FILE:-}"

if [[ -z "$SUITE" && -z "$POLICY_FILE" ]]; then
  echo "RETRIEVAL_SUITE or RETRIEVAL_POLICY_FILE is required" >&2
  exit 1
fi

ARGS=(
  eval retrieval
  --workspace "$WORKSPACE"
  --vault-path "$VAULT_PATH"
)

if [[ -n "$SUITE" ]]; then
  ARGS+=(--suite "$SUITE")
fi

if [[ -n "$LIMIT" ]]; then
  ARGS+=(--limit "$LIMIT")
fi

if [[ -n "$FORMAT" ]]; then
  ARGS+=(--format "$FORMAT")
fi

if [[ -n "$POLICY_FILE" ]]; then
  ARGS+=(--policy-file "$POLICY_FILE")
fi

if [[ -n "${RETRIEVAL_MODES:-}" ]]; then
  for mode in ${RETRIEVAL_MODES}; do
    ARGS+=(--mode "$mode")
  done
fi

cd "$ROOT"
exec env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 go run -tags=libsqlite3 ./cmd/agentctl "${ARGS[@]}" "$@"
