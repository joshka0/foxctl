#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'ladybug_trace_path: %s\n' "$*" >&2
  exit 1
}

validate_id() {
  local name=$1
  local value=$2
  [ -n "$value" ] || fail "$name is required"
  case "$value" in
    *\"*|*$'\n'*|*$'\r'*)
      fail "$name contains unsupported characters"
      ;;
  esac
}

LBUG_BIN=${LBUG_BIN:-lbug}
DB_PATH=${DB_PATH:-}
LADYBUG_HOME=${LADYBUG_HOME:-}
PROJECTION_META=${PROJECTION_META:-}
REPO_ROOT=${REPO_ROOT:-}
ALLOW_STALE=${LADYBUG_ALLOW_STALE:-}
SRC_ID=${SRC_ID:-}
DST_ID=${DST_ID:-}
MAX_DEPTH=${MAX_DEPTH:-5}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --lbug-bin) LBUG_BIN=$2; shift 2 ;;
    --db-path) DB_PATH=$2; shift 2 ;;
    --home) LADYBUG_HOME=$2; shift 2 ;;
    --projection-meta) PROJECTION_META=$2; shift 2 ;;
    --repo-root) REPO_ROOT=$2; shift 2 ;;
    --allow-stale) ALLOW_STALE=1; shift ;;
    --src-id) SRC_ID=$2; shift 2 ;;
    --dst-id) DST_ID=$2; shift 2 ;;
    --max-depth) MAX_DEPTH=$2; shift 2 ;;
    -h|--help)
      cat <<'EOF'
Usage: ladybug_trace_path.sh --db-path PATH --src-id ID --dst-id ID [--max-depth N] [--lbug-bin PATH] [--home DIR] [--projection-meta PATH] [--repo-root DIR] [--allow-stale]
EOF
      exit 0
      ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[ -n "$DB_PATH" ] || fail "DB_PATH is required"
command -v "$LBUG_BIN" >/dev/null 2>&1 || fail "lbug binary not found: $LBUG_BIN"
validate_id "SRC_ID" "$SRC_ID"
validate_id "DST_ID" "$DST_ID"
case "$MAX_DEPTH" in
  ''|*[!0-9]*) fail "MAX_DEPTH must be a positive integer" ;;
esac
[ "$MAX_DEPTH" -gt 0 ] || fail "MAX_DEPTH must be greater than zero"

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=scripts/ladybug_freshness.sh
. "${SCRIPT_DIR}/ladybug_freshness.sh"
ladybug_check_projection_freshness "$DB_PATH" "$PROJECTION_META" "$REPO_ROOT" "$ALLOW_STALE" "ladybug_trace_path"

if [ -n "$LADYBUG_HOME" ]; then
  mkdir -p "$LADYBUG_HOME"
  export HOME="$LADYBUG_HOME"
fi

cat <<EOF | "$LBUG_BIN" "$DB_PATH" -r -b -s -m json
MATCH p = (a:GraphNode)-[e:Contains|:Imports|:UsesSymbol|:RefersTo|:Calls|:Implements|:Embeds|:Tests|:DescribedBy|:VerifiedBy|:Enforces|:CoChangesWith* SHORTEST 1..${MAX_DEPTH}]->(b:GraphNode)
WHERE a.id = "${SRC_ID}" AND b.id = "${DST_ID}"
RETURN length(e) AS path_len,
  properties(nodes(p), 'id') AS node_ids,
  properties(nodes(p), 'kind') AS node_kinds,
  properties(nodes(p), 'name') AS node_names,
  properties(nodes(p), 'file') AS node_files,
  properties(nodes(p), 'span_start') AS node_span_starts,
  properties(nodes(p), 'span_end') AS node_span_ends;
EOF
