#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'ladybug_rebuild_projection: %s\n' "$*" >&2
  exit 1
}

require_file() {
  local path=$1
  [ -f "$path" ] || fail "required file not found: $path"
}

copy_rel_if_present() {
  local file_name=$1
  local table_name=$2
  local csv_path="${EXPORT_DIR_ABS}/${file_name}"
  if [ ! -f "$csv_path" ]; then
    printf 'ladybug_rebuild_projection: skip missing %s\n' "$file_name" >&2
    return
  fi
  printf "COPY %s FROM \"%s\" (HEADER=true, DELIM=\",\", QUOTE='\"', ESCAPE='\"', AUTO_DETECT=false);\n" "$table_name" "$csv_path" >>"$IMPORT_SCRIPT"
}

allow_edge_file() {
  ALLOWED_EDGE_FILES="${ALLOWED_EDGE_FILES}${ALLOWED_EDGE_FILES:+ }$1"
}

validate_edge_files() {
  local path
  for path in "${EXPORT_DIR_ABS}"/edges_*.csv; do
    [ -e "$path" ] || return 0
    local file_name
    file_name=$(basename "$path")
    case " ${ALLOWED_EDGE_FILES} " in
      *" ${file_name} "*) ;;
      *) fail "unsupported edge CSV: ${file_name}" ;;
    esac
  done
}

LBUG_BIN=${LBUG_BIN:-lbug}
EXPORT_DIR=${EXPORT_DIR:-}
DB_PATH=${DB_PATH:-}
LADYBUG_HOME=${LADYBUG_HOME:-}
ALLOWED_EDGE_FILES=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --lbug-bin)
      [ "$#" -ge 2 ] || fail "--lbug-bin requires a value"
      LBUG_BIN=$2
      shift 2
      ;;
    --export-dir)
      [ "$#" -ge 2 ] || fail "--export-dir requires a value"
      EXPORT_DIR=$2
      shift 2
      ;;
    --db-path)
      [ "$#" -ge 2 ] || fail "--db-path requires a value"
      DB_PATH=$2
      shift 2
      ;;
    --home)
      [ "$#" -ge 2 ] || fail "--home requires a value"
      LADYBUG_HOME=$2
      shift 2
      ;;
    -h|--help)
      cat <<'EOF'
Usage: ladybug_rebuild_projection.sh --export-dir DIR --db-path PATH [--lbug-bin PATH] [--home DIR]

Rebuilds a disposable LadybugDB projection from a Phase 1 export directory.
Environment fallbacks: EXPORT_DIR, DB_PATH, LBUG_BIN, LADYBUG_HOME.
EOF
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[ -n "$EXPORT_DIR" ] || fail "EXPORT_DIR is required"
[ -n "$DB_PATH" ] || fail "DB_PATH is required"
command -v "$LBUG_BIN" >/dev/null 2>&1 || fail "lbug binary not found: $LBUG_BIN"

EXPORT_DIR_ABS=$(cd "$EXPORT_DIR" 2>/dev/null && pwd -P) || fail "export directory not found: $EXPORT_DIR"
require_file "${EXPORT_DIR_ABS}/meta.json"
require_file "${EXPORT_DIR_ABS}/nodes_graph.csv"
EXPECTED_NODE_COUNT=$(($(wc -l <"${EXPORT_DIR_ABS}/nodes_graph.csv") - 1))

case "$DB_PATH" in
  ""|"/"|".")
    fail "refusing unsafe DB_PATH: $DB_PATH"
    ;;
esac

DB_DIR=$(dirname "$DB_PATH")
mkdir -p "$DB_DIR"

LADYBUG_HOME=${LADYBUG_HOME:-"${DB_DIR}/.ladybug-home"}
mkdir -p "$LADYBUG_HOME"

IMPORT_SCRIPT=$(mktemp "${TMPDIR:-/tmp}/ladybug-import.XXXXXX.cypher")
trap 'rm -f "$IMPORT_SCRIPT"' EXIT

cat >"$IMPORT_SCRIPT" <<EOF
:mode json
CREATE NODE TABLE GraphNode(
  id STRING PRIMARY KEY,
  kind STRING,
  repo_key STRING,
  repo_root_identity STRING,
  pkg STRING,
  file STRING,
  name STRING,
  signature STRING,
  span_start STRING,
  span_end STRING,
  exported STRING,
  doc STRING,
  summary STRING,
  hash STRING,
  updated_at STRING,
  meta_json STRING
);
CREATE REL TABLE Contains(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE Imports(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE UsesSymbol(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE RefersTo(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE Calls(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE Implements(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE Embeds(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE Tests(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE HasKeyword(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE HasOutputField(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE TouchesResource(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE EmitsEvent(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE DocRelated(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE DocFlow(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE Enforces(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE ProtectsAgainst(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE VerifiedBy(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE DescribedBy(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE DecidedBy(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE ImplementsProtocol(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE ParticipatesIn(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE DeclaresAnchorTarget(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE BeaconFor(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE CoChangesWith(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
COPY GraphNode FROM "${EXPORT_DIR_ABS}/nodes_graph.csv" (HEADER=true, DELIM=",", QUOTE='"', ESCAPE='"', AUTO_DETECT=false);
EOF

allow_edge_file "edges_contains.csv"
allow_edge_file "edges_imports.csv"
allow_edge_file "edges_uses_symbol.csv"
allow_edge_file "edges_refers_to.csv"
allow_edge_file "edges_calls.csv"
allow_edge_file "edges_implements.csv"
allow_edge_file "edges_embeds.csv"
allow_edge_file "edges_tests.csv"
allow_edge_file "edges_has_keyword.csv"
allow_edge_file "edges_has_output_field.csv"
allow_edge_file "edges_touches_resource.csv"
allow_edge_file "edges_emits_event.csv"
allow_edge_file "edges_doc_related.csv"
allow_edge_file "edges_doc_flow.csv"
allow_edge_file "edges_enforces.csv"
allow_edge_file "edges_protects_against.csv"
allow_edge_file "edges_verified_by.csv"
allow_edge_file "edges_described_by.csv"
allow_edge_file "edges_decided_by.csv"
allow_edge_file "edges_implements_protocol.csv"
allow_edge_file "edges_participates_in.csv"
allow_edge_file "edges_declares_anchor_target.csv"
allow_edge_file "edges_beacon_for.csv"
allow_edge_file "edges_co_changes_with.csv"

validate_edge_files

copy_rel_if_present "edges_contains.csv" "Contains"
copy_rel_if_present "edges_imports.csv" "Imports"
copy_rel_if_present "edges_uses_symbol.csv" "UsesSymbol"
copy_rel_if_present "edges_refers_to.csv" "RefersTo"
copy_rel_if_present "edges_calls.csv" "Calls"
copy_rel_if_present "edges_implements.csv" "Implements"
copy_rel_if_present "edges_embeds.csv" "Embeds"
copy_rel_if_present "edges_tests.csv" "Tests"
copy_rel_if_present "edges_has_keyword.csv" "HasKeyword"
copy_rel_if_present "edges_has_output_field.csv" "HasOutputField"
copy_rel_if_present "edges_touches_resource.csv" "TouchesResource"
copy_rel_if_present "edges_emits_event.csv" "EmitsEvent"
copy_rel_if_present "edges_doc_related.csv" "DocRelated"
copy_rel_if_present "edges_doc_flow.csv" "DocFlow"
copy_rel_if_present "edges_enforces.csv" "Enforces"
copy_rel_if_present "edges_protects_against.csv" "ProtectsAgainst"
copy_rel_if_present "edges_verified_by.csv" "VerifiedBy"
copy_rel_if_present "edges_described_by.csv" "DescribedBy"
copy_rel_if_present "edges_decided_by.csv" "DecidedBy"
copy_rel_if_present "edges_implements_protocol.csv" "ImplementsProtocol"
copy_rel_if_present "edges_participates_in.csv" "ParticipatesIn"
copy_rel_if_present "edges_declares_anchor_target.csv" "DeclaresAnchorTarget"
copy_rel_if_present "edges_beacon_for.csv" "BeaconFor"
copy_rel_if_present "edges_co_changes_with.csv" "CoChangesWith"

cat >>"$IMPORT_SCRIPT" <<'EOF'
MATCH (n:GraphNode) RETURN count(n) AS node_count;
EOF

rm -rf "$DB_PATH"
IMPORT_OUTPUT=$(HOME="$LADYBUG_HOME" "$LBUG_BIN" "$DB_PATH" -b -s <"$IMPORT_SCRIPT")
printf '%s\n' "$IMPORT_OUTPUT"
case "$IMPORT_OUTPUT" in
  *"\"node_count\":${EXPECTED_NODE_COUNT}"*) ;;
  *) fail "imported node count did not match nodes_graph.csv row count ${EXPECTED_NODE_COUNT}" ;;
esac
