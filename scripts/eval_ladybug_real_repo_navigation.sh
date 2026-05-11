#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)

if [ -z "${LBUG_BIN:-}" ] && [ -x /private/tmp/ladybug-phase0/bin/lbug ]; then
  export LBUG_BIN=/private/tmp/ladybug-phase0/bin/lbug
fi

export FOXCTL_LADYBUG_EVAL=1

cd "$ROOT"
exec go test -count=1 ./internal/intelligence/indexing/repoindex \
  -run TestLadybugRealRepoNavigationEval \
  -timeout "${FOXCTL_LADYBUG_EVAL_TIMEOUT:-5m}" \
  -v
