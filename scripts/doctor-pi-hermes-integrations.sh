#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

PI_SOURCE="$ROOT_DIR/integrations/pi/foxctl.ts"
HERMES_SOURCE="$ROOT_DIR/integrations/hermes"

PI_TARGET="${PI_EXTENSION_PATH:-$HOME/.pi/extensions/foxctl.ts}"
HERMES_TARGET="${HERMES_FOXCTL_PLUGIN_PATH:-$HOME/.hermes/plugins/foxctl}"
FOXCTL_URL="${FOXCTL_URL:-http://localhost:8090}"

APPLY=0
FORCE=0
CHECK_DAEMON=0
STATUS=0

usage() {
  cat <<'EOF'
Usage: scripts/doctor-pi-hermes-integrations.sh [--apply] [--force] [--check-daemon]

Checks that the local Pi and Hermes foxctl integrations point at this checkout
and contain the expected foxctl tools plus automatic memory draft hooks.

Options:
  --apply         Relink Pi and Hermes integration targets to this checkout.
  --force         With --apply, move a non-symlink target directory aside.
  --check-daemon  Also check FOXCTL_URL health if curl is available.
  -h, --help      Show this help.

Environment:
  PI_EXTENSION_PATH          Default: ~/.pi/extensions/foxctl.ts
  HERMES_FOXCTL_PLUGIN_PATH  Default: ~/.hermes/plugins/foxctl
  FOXCTL_URL                 Default: http://localhost:8090
EOF
}

while (($#)); do
  case "$1" in
    --apply|--ensure)
      APPLY=1
      ;;
    --force)
      FORCE=1
      ;;
    --check-daemon)
      CHECK_DAEMON=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

note() {
  printf '%-5s %s\n' "$1" "$2"
}

ok() {
  note "ok" "$1"
}

warn() {
  note "warn" "$1"
}

fail() {
  note "fail" "$1"
  STATUS=1
}

real_path() {
  local path="$1"
  if command -v realpath >/dev/null 2>&1; then
    realpath -m "$path"
    return
  fi
  python3 -c 'import os, sys; print(os.path.realpath(sys.argv[1]))' "$path"
}

existing_real_path() {
  local path="$1"
  if [[ -e "$path" || -L "$path" ]]; then
    real_path "$path"
  else
    printf ''
  fi
}

ensure_symlink() {
  local source="$1"
  local target="$2"
  local label="$3"

  mkdir -p "$(dirname "$target")"

  if [[ -L "$target" || -f "$target" ]]; then
    rm -f "$target"
  elif [[ -e "$target" ]]; then
    if ((FORCE)); then
      local backup="${target}.bak.$(date -u +%Y%m%dT%H%M%SZ)"
      mv "$target" "$backup"
      warn "$label existing directory moved to $backup"
    else
      fail "$label target exists and is not a symlink/file: $target (use --force to move it aside)"
      return
    fi
  fi

  ln -s "$source" "$target"
  ok "$label linked: $target -> $source"
}

check_source() {
  local source="$1"
  local label="$2"

  if [[ -e "$source" ]]; then
    ok "$label source exists: $source"
  else
    fail "$label source missing: $source"
  fi
}

check_link() {
  local source="$1"
  local target="$2"
  local label="$3"
  local resolved_source
  local resolved_target

  resolved_source="$(existing_real_path "$source")"
  resolved_target="$(existing_real_path "$target")"

  if [[ -z "$resolved_target" ]]; then
    fail "$label target missing: $target"
    return
  fi

  if [[ "$resolved_target" == "$resolved_source" ]]; then
    ok "$label target points at this checkout: $target"
  else
    fail "$label target points elsewhere: $target -> $resolved_target"
    warn "$label expected: $resolved_source"
  fi
}

grep_file_marker() {
  local file="$1"
  local marker="$2"
  local label="$3"

  if [[ ! -f "$file" ]]; then
    fail "$label target is not a file: $file"
    return
  fi

  if grep -Fq -- "$marker" "$file"; then
    ok "$label has marker: $marker"
  else
    fail "$label missing marker: $marker"
  fi
}

grep_tree_marker() {
  local dir="$1"
  local marker="$2"
  local label="$3"

  if [[ ! -d "$dir" ]]; then
    fail "$label target is not a directory: $dir"
    return
  fi

  if grep -R -Fq --exclude-dir='__pycache__' -- "$marker" "$dir"; then
    ok "$label has marker: $marker"
  else
    fail "$label missing marker: $marker"
  fi
}

check_daemon() {
  if ((CHECK_DAEMON == 0)); then
    return
  fi
  if ! command -v curl >/dev/null 2>&1; then
    warn "curl not installed; skipping foxctl daemon check"
    return
  fi
  if curl -fsS "$FOXCTL_URL/api/health" >/dev/null 2>&1; then
    ok "foxctl daemon reachable: $FOXCTL_URL"
  else
    fail "foxctl daemon health check failed: $FOXCTL_URL/api/health"
  fi
}

check_pi_markers() {
  local target="$1"
  local markers=(
    "foxctl_context_memory_drafts"
    "foxctl-memory-drafts-auto"
    "session_start"
    "before_agent_start"
    "foxctl_tool_run"
    "foxctl_context_show"
    "foxctl_vault_index_build"
    "foxctl_memory_curator"
  )

  for marker in "${markers[@]}"; do
    grep_file_marker "$target" "$marker" "pi"
  done
}

check_hermes_markers() {
  local target="$1"
  local markers=(
    "foxctl_context_memory_drafts"
    "memory_drafts_auto"
    "on_session_start"
    "on_session_end"
    "foxctl_flow_build_pipeline"
    "foxctl_context_curator"
    "foxctl_vault_index_build"
    "foxctl_memory_curator"
  )

  for marker in "${markers[@]}"; do
    grep_tree_marker "$target" "$marker" "hermes"
  done
}

echo "foxctl integration doctor"
echo "repo: $ROOT_DIR"
echo "pi target: $PI_TARGET"
echo "hermes target: $HERMES_TARGET"
echo

check_source "$PI_SOURCE" "pi"
check_source "$HERMES_SOURCE" "hermes"

if ((APPLY)); then
  echo
  ensure_symlink "$PI_SOURCE" "$PI_TARGET" "pi"
  ensure_symlink "$HERMES_SOURCE" "$HERMES_TARGET" "hermes"
fi

echo
check_link "$PI_SOURCE" "$PI_TARGET" "pi"
check_link "$HERMES_SOURCE" "$HERMES_TARGET" "hermes"

echo
check_pi_markers "$PI_TARGET"
check_hermes_markers "$HERMES_TARGET"
check_daemon

echo
if ((STATUS == 0)); then
  ok "Pi and Hermes foxctl integrations are installed from this checkout"
else
  fail "Pi and Hermes foxctl integration doctor found issues"
fi

exit "$STATUS"
