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
NODE_ID=${NODE_ID:-}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --lbug-bin) LBUG_BIN=$2; shift 2 ;;
    --db-path) DB_PATH=$2; shift 2 ;;
    --home) LADYBUG_HOME=$2; shift 2 ;;
    --node-id) NODE_ID=$2; shift 2 ;;
    -h|--help)
      cat <<'EOF'
Usage: ladybug_smart_context.sh --db-path PATH --node-id ID [--lbug-bin PATH] [--home DIR]
EOF
      exit 0
      ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[ -n "$DB_PATH" ] || fail "DB_PATH is required"
command -v "$LBUG_BIN" >/dev/null 2>&1 || fail "lbug binary not found: $LBUG_BIN"
validate_id "NODE_ID" "$NODE_ID"

if [ -n "$LADYBUG_HOME" ]; then
  mkdir -p "$LADYBUG_HOME"
  export HOME="$LADYBUG_HOME"
fi

cat <<EOF | "$LBUG_BIN" "$DB_PATH" -r -b -s -m json
MATCH (n:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "self" AS section, n.id AS node_id, n.kind AS kind, n.pkg AS pkg, n.file AS file, n.name AS name, n.signature AS signature;
MATCH (container:GraphNode)-[r:Contains]->(n:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "contains_in" AS section, "CONTAINS" AS edge_type, container.id AS src_id, n.id AS dst_id, r.source AS source, r.meta_json AS meta_json;
MATCH (n:GraphNode)-[r:Contains]->(child:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "contains_children" AS section, "CONTAINS" AS edge_type, n.id AS src_id, child.id AS dst_id, r.source AS source, r.meta_json AS meta_json;
MATCH (n:GraphNode)-[r:Calls]->(callee:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "callees" AS section, "CALLS" AS edge_type, n.id AS src_id, callee.id AS dst_id, r.source AS source, r.meta_json AS meta_json;
MATCH (caller:GraphNode)-[r:Calls]->(n:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "callers" AS section, "CALLS" AS edge_type, caller.id AS src_id, n.id AS dst_id, r.source AS source, r.meta_json AS meta_json;
MATCH (n:GraphNode)-[r:DescribedBy]->(concept:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "docs_concepts" AS section, "DESCRIBED_BY" AS edge_type, n.id AS src_id, concept.id AS dst_id, r.source AS source, r.meta_json AS meta_json;
MATCH (n:GraphNode)-[r:CoChangesWith]->(neighbor:GraphNode)
WHERE n.id = "${NODE_ID}"
RETURN "co_changes" AS section, "CO_CHANGES_WITH" AS edge_type, n.id AS src_id, neighbor.id AS dst_id, r.source AS source, r.meta_json AS meta_json;
EOF
