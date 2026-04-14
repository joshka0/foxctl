#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export RETRIEVAL_WORKSPACE="${RETRIEVAL_WORKSPACE:-$ROOT}"
export RETRIEVAL_POLICY_FILE="${RETRIEVAL_POLICY_FILE:-$ROOT/testdata/evals/retrieval/foxctl-cochange-policy.yaml}"

exec bash "$ROOT/scripts/eval_retrieval_suite.sh" "$@"
