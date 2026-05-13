#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

DEFAULT_PACKAGES=(
  ./internal/domain/agent
  ./internal/intelligence/indexing/repoindex
  ./internal/platform/timeutil
  ./internal/runtime/execution/exec
  ./internal/storage/cas
  ./internal/storage/dbutil
)

COUNT="${BENCH_COUNT:-3}"
BENCHTIME="${BENCH_TIME:-1s}"
PATTERN="${BENCH_PATTERN:-.}"
OUT="${BENCH_OUT:-/private/tmp/foxctl-benchmarks/$(date -u +%Y%m%dT%H%M%SZ).txt}"

if [[ "$COUNT" =~ ^[0-9]+$ ]] && (( COUNT < 1 )); then
  echo "BENCH_COUNT must be at least 1" >&2
  exit 2
fi

PACKAGES=("$@")
if (( ${#PACKAGES[@]} == 0 )); then
  PACKAGES=("${DEFAULT_PACKAGES[@]}")
fi

mkdir -p "$(dirname "$OUT")"

{
  echo "# foxctl Go benchmarks"
  echo "# root: $ROOT"
  echo "# packages: ${PACKAGES[*]}"
  echo "# pattern: $PATTERN"
  echo "# benchtime: $BENCHTIME"
  echo "# count: $COUNT"
  echo
} | tee "$OUT"

cd "$ROOT"
go test \
  -run='^$' \
  -bench "$PATTERN" \
  -benchmem \
  -benchtime "$BENCHTIME" \
  -count "$COUNT" \
  "${PACKAGES[@]}" | tee -a "$OUT"

echo
echo "Benchmark output: $OUT"
