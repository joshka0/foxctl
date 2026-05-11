#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'ladybug_smart_context: %s\n' "$*" >&2
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
NODE_ID=${NODE_ID:-}
LIMIT=${LIMIT:-50}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --lbug-bin) LBUG_BIN=$2; shift 2 ;;
    --db-path) DB_PATH=$2; shift 2 ;;
    --home) LADYBUG_HOME=$2; shift 2 ;;
    --projection-meta) PROJECTION_META=$2; shift 2 ;;
    --repo-root) REPO_ROOT=$2; shift 2 ;;
    --allow-stale) ALLOW_STALE=1; shift ;;
    --node-id) NODE_ID=$2; shift 2 ;;
    --limit) LIMIT=$2; shift 2 ;;
    -h|--help)
      cat <<'EOF'
Usage: ladybug_smart_context.sh --db-path PATH --node-id ID [--limit N] [--lbug-bin PATH] [--home DIR] [--projection-meta PATH] [--repo-root DIR] [--allow-stale]
EOF
      exit 0
      ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[ -n "$DB_PATH" ] || fail "DB_PATH is required"
command -v "$LBUG_BIN" >/dev/null 2>&1 || fail "lbug binary not found: $LBUG_BIN"
validate_id "NODE_ID" "$NODE_ID"
case "$LIMIT" in
  ''|*[!0-9]*) fail "LIMIT must be a positive integer" ;;
esac
[ "$LIMIT" -gt 0 ] || fail "LIMIT must be greater than zero"

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=scripts/ladybug_freshness.sh
. "${SCRIPT_DIR}/ladybug_freshness.sh"
ladybug_check_projection_freshness "$DB_PATH" "$PROJECTION_META" "$REPO_ROOT" "$ALLOW_STALE" "ladybug_smart_context"

if [ -n "$LADYBUG_HOME" ]; then
  mkdir -p "$LADYBUG_HOME"
  export HOME="$LADYBUG_HOME"
fi

cat <<EOF | "$LBUG_BIN" "$DB_PATH" -r -b -s -m json
MATCH (n:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "self" AS section, n.id AS node_id, n.kind AS kind, n.pkg AS pkg, n.file AS file, n.name AS name, n.signature AS signature, n.span_start AS span_start, n.span_end AS span_end, n.doc AS doc, n.summary AS summary;
MATCH (container:GraphNode)-[r:Contains]->(n:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "contains_in" AS section, "CONTAINS" AS edge_type, container.id AS src_id, container.name AS src_name, container.file AS src_file, container.span_start AS src_span_start, n.id AS dst_id, n.name AS dst_name, n.file AS dst_file, n.span_start AS dst_span_start, r.source AS source, r.meta_json AS meta_json
LIMIT ${LIMIT};
MATCH (n:GraphNode)-[r:Contains]->(child:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "contains_children" AS section, "CONTAINS" AS edge_type, n.id AS src_id, n.name AS src_name, n.file AS src_file, n.span_start AS src_span_start, child.id AS dst_id, child.name AS dst_name, child.file AS dst_file, child.span_start AS dst_span_start, r.source AS source, r.meta_json AS meta_json
LIMIT ${LIMIT};
MATCH (n:GraphNode)-[r:Calls]->(callee:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "callees" AS section, "CALLS" AS edge_type, n.id AS src_id, n.name AS src_name, n.file AS src_file, n.span_start AS src_span_start, callee.id AS dst_id, callee.name AS dst_name, callee.file AS dst_file, callee.span_start AS dst_span_start, r.source AS source, r.meta_json AS meta_json
LIMIT ${LIMIT};
MATCH (caller:GraphNode)-[r:Calls]->(n:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "callers" AS section, "CALLS" AS edge_type, caller.id AS src_id, caller.name AS src_name, caller.file AS src_file, caller.span_start AS src_span_start, n.id AS dst_id, n.name AS dst_name, n.file AS dst_file, n.span_start AS dst_span_start, r.source AS source, r.meta_json AS meta_json
LIMIT ${LIMIT};
MATCH (n:GraphNode)-[r:DescribedBy]->(concept:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "docs_concepts" AS section, "DESCRIBED_BY" AS edge_type, n.id AS src_id, n.name AS src_name, n.file AS src_file, n.span_start AS src_span_start, concept.id AS dst_id, concept.name AS dst_name, concept.file AS dst_file, concept.span_start AS dst_span_start, r.source AS source, r.meta_json AS meta_json
LIMIT ${LIMIT};
MATCH (n:GraphNode)-[r:CoChangesWith]->(neighbor:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "co_changes" AS section, "CO_CHANGES_WITH" AS edge_type, n.id AS src_id, n.name AS src_name, n.file AS src_file, n.span_start AS src_span_start, neighbor.id AS dst_id, neighbor.name AS dst_name, neighbor.file AS dst_file, neighbor.span_start AS dst_span_start, r.source AS source, r.meta_json AS meta_json
LIMIT ${LIMIT};
EOF
