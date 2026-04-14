#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export CODE_SEARCH_WORKSPACE="${CODE_SEARCH_WORKSPACE:-$ROOT}"
export CODE_SEARCH_DATASET="$ROOT/testdata/evals/code-search-ensemble/foxctl-trace-symbol.jsonl"
export CODE_SEARCH_POLICY="$ROOT/testdata/evals/code-search-ensemble/foxctl-trace-symbol-policy.yaml"

exec bash "$ROOT/scripts/eval_code_search_suite.sh" "$@"
