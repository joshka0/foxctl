#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'ladybug_blast_radius: %s\n' "$*" >&2
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
MAX_DEPTH=${MAX_DEPTH:-3}
LIMIT=${LIMIT:-50}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --lbug-bin) LBUG_BIN=$2; shift 2 ;;
    --db-path) DB_PATH=$2; shift 2 ;;
    --home) LADYBUG_HOME=$2; shift 2 ;;
    --node-id) NODE_ID=$2; shift 2 ;;
    --max-depth) MAX_DEPTH=$2; shift 2 ;;
    --limit) LIMIT=$2; shift 2 ;;
    -h|--help)
      cat <<'EOF'
Usage: ladybug_blast_radius.sh --db-path PATH --node-id ID [--max-depth N] [--limit N] [--lbug-bin PATH] [--home DIR]
EOF
      exit 0
      ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[ -n "$DB_PATH" ] || fail "DB_PATH is required"
command -v "$LBUG_BIN" >/dev/null 2>&1 || fail "lbug binary not found: $LBUG_BIN"
validate_id "NODE_ID" "$NODE_ID"
case "$MAX_DEPTH" in
  ''|*[!0-9]*) fail "MAX_DEPTH must be a positive integer" ;;
esac
[ "$MAX_DEPTH" -gt 0 ] || fail "MAX_DEPTH must be greater than zero"
case "$LIMIT" in
  ''|*[!0-9]*) fail "LIMIT must be a positive integer" ;;
esac
[ "$LIMIT" -gt 0 ] || fail "LIMIT must be greater than zero"

if [ -n "$LADYBUG_HOME" ]; then
  mkdir -p "$LADYBUG_HOME"
  export HOME="$LADYBUG_HOME"
fi

cat <<EOF | "$LBUG_BIN" "$DB_PATH" -r -b -s -m json
MATCH p = (start:GraphNode)-[e:Contains|:Calls|:Tests|:DescribedBy|:CoChangesWith*1..${MAX_DEPTH}]->(candidate:GraphNode)
WHERE start.id = "${NODE_ID}"
RETURN "forward_reason" AS section,
  length(e) AS path_len,
  properties(nodes(p), 'id') AS node_ids,
  properties(nodes(p), 'kind') AS node_kinds,
  properties(nodes(p), 'name') AS node_names,
  properties(nodes(p), 'file') AS node_files,
  properties(nodes(p), 'span_start') AS node_span_starts,
  properties(nodes(p), 'span_end') AS node_span_ends
LIMIT ${LIMIT};
MATCH (candidate:GraphNode)-[r:Calls]->(start:GraphNode)
WHERE start.id = "${NODE_ID}"
RETURN "incoming_call" AS section, "CALLS" AS edge_type, candidate.id AS src_id, candidate.name AS src_name, candidate.file AS src_file, candidate.span_start AS src_span_start, start.id AS dst_id, start.name AS dst_name, start.file AS dst_file, start.span_start AS dst_span_start, r.source AS source, r.meta_json AS meta_json
LIMIT ${LIMIT};
MATCH (start:GraphNode)-[r:CoChangesWith]->(candidate:GraphNode)
WHERE start.id = "${NODE_ID}"
RETURN "co_change" AS section, "CO_CHANGES_WITH" AS edge_type, start.id AS src_id, start.name AS src_name, start.file AS src_file, start.span_start AS src_span_start, candidate.id AS dst_id, candidate.name AS dst_name, candidate.file AS dst_file, candidate.span_start AS dst_span_start, r.source AS source, r.meta_json AS meta_json
LIMIT ${LIMIT};
EOF
