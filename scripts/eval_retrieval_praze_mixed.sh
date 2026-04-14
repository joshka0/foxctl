#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKSPACE="${PRAZE_WORKSPACE:-${1:-}}"

if [[ -z "${WORKSPACE}" ]]; then
  echo "usage: PRAZE_WORKSPACE=/path/to/praze $0 [extra foxctl flags]" >&2
  exit 1
fi

if [[ "${1:-}" == "${WORKSPACE}" ]]; then
  shift
fi

export RETRIEVAL_WORKSPACE="$WORKSPACE"
export RETRIEVAL_POLICY_FILE="${RETRIEVAL_POLICY_FILE:-$ROOT/testdata/evals/retrieval/praze-mixed-policy.yaml}"

exec bash "$ROOT/scripts/eval_retrieval_suite.sh" "$@"
