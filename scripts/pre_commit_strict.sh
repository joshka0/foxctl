#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$repo_root"

mapfile -t formatted_go_files < <(git ls-files \
    'cmd/*.go' 'cmd/**/*.go' \
    'internal/*.go' 'internal/**/*.go' \
    'skills/*.go' 'skills/**/*.go')

echo "Running foxctl static-analysis gate..."

cache_root="${FOXCTL_STATIC_ANALYSIS_CACHE_DIR:-${TMPDIR:-/tmp}/foxctl-static-analysis}"
export GOCACHE="${GOCACHE:-$cache_root/go-build}"
export GOLANGCI_LINT_CACHE="${GOLANGCI_LINT_CACHE:-$cache_root/golangci-lint}"
mkdir -p "$GOCACHE" "$GOLANGCI_LINT_CACHE"

before_fmt="$(mktemp)"
after_fmt="$(mktemp)"
trap 'rm -f "$before_fmt" "$after_fmt"' EXIT

if ((${#formatted_go_files[@]} > 0)); then
    git -c core.filemode=false diff -- "${formatted_go_files[@]}" >"$before_fmt"
fi

make fmt

if ((${#formatted_go_files[@]} > 0)); then
    git -c core.filemode=false diff -- "${formatted_go_files[@]}" >"$after_fmt"
    if ! cmp -s "$before_fmt" "$after_fmt"; then
        cat "$after_fmt"
        echo "Formatting changed Go files. Run make fmt locally, review the diff, and commit the formatted result."
        exit 1
    fi
fi

GOGC=50 GOMEMLIMIT=1800MiB make lint \
    GOLANGCI_TIMEOUT="${GOLANGCI_TIMEOUT:-30m}" \
    GOLANGCI_FLAGS="${GOLANGCI_FLAGS:---concurrency=1}" \
    LINT_TARGETS="${LINT_TARGETS:-./cmd/... ./internal/... ./plugins/... ./scripts/... ./tests/...}"

make repo-hygiene
