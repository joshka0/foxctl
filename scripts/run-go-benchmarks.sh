#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

COUNT="${BENCH_COUNT:-3}"
BENCHTIME="${BENCH_TIME:-1s}"
PATTERN="${BENCH_PATTERN:-.}"
GATE="${BENCH_GATE:-default}"
OUT="${BENCH_OUT:-/private/tmp/foxctl-benchmarks/$(date -u +%Y%m%dT%H%M%SZ).txt}"

if [[ "$COUNT" =~ ^[0-9]+$ ]] && (( COUNT < 1 )); then
  echo "BENCH_COUNT must be at least 1" >&2
  exit 2
fi

manifest_packages() {
  local envelope packages

  envelope="$(cd "$ROOT" && go run ./cmd/foxctl benchmark manifest packages --gate "$GATE")"
  if [[ "$envelope" != *'"status":"ok"'* ]]; then
    printf '%s\n' "$envelope" >&2
    return 1
  fi

  packages="$(sed -n 's/.*"packages":\[\([^]]*\)\].*/\1/p' <<<"$envelope")"
  if [[ -z "$packages" ]]; then
    echo "benchmark manifest returned no Go packages for gate $GATE" >&2
    return 1
  fi

  tr ',' '\n' <<<"$packages" | sed -n 's/^"\([^"]*\)"$/\1/p'
}

PACKAGES=("$@")
if (( ${#PACKAGES[@]} == 0 )); then
  mapfile -t PACKAGES < <(manifest_packages)
fi
if (( ${#PACKAGES[@]} == 0 )); then
  echo "no benchmark packages resolved" >&2
  exit 2
fi

mkdir -p "$(dirname "$OUT")"

{
  echo "# foxctl Go benchmarks"
  echo "# root: $ROOT"
  echo "# manifest: configs/benchmarks/foxctl.json"
  echo "# gate: $GATE"
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
