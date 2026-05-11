#!/usr/bin/env bash

ladybug_freshness_fail() {
  local label=${1:-ladybug_freshness}
  shift || true
  printf '%s: %s\n' "$label" "$*" >&2
  return 1
}

ladybug_freshness_truthy() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|y|Y) return 0 ;;
    *) return 1 ;;
  esac
}

ladybug_freshness_repo_root() {
  local helper_dir
  helper_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd -P) || helper_dir=""
  if [ -n "$helper_dir" ]; then
    git -C "${helper_dir}/.." rev-parse --show-toplevel 2>/dev/null && return 0
  fi
  git rev-parse --show-toplevel 2>/dev/null
}

ladybug_freshness_sha256() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum | awk '{print $1}'
    return
  fi
  return 1
}

ladybug_freshness_status_hash() {
  local repo_root=$1
  local status_file
  status_file=$(mktemp "${TMPDIR:-/tmp}/ladybug-git-status.XXXXXX") || return 1
  if ! git -C "$repo_root" status --porcelain=v1 -z --untracked-files=all >"$status_file"; then
    rm -f "$status_file"
    return 1
  fi
  if [ ! -s "$status_file" ]; then
    rm -f "$status_file"
    printf '\n'
    return 0
  fi
  perl -0777 -e '
    local $/;
    my $status = <STDIN>;
    $status =~ s/\A[ \t\r\n\f\x0b]+//;
    $status =~ s/[ \t\r\n\f\x0b]+\z//;
    if ($status eq "") {
      exit 0;
    }
    my @parts = split(/\0/, $status, -1);
    @parts = sort @parts;
    print join("\0", @parts);
  ' <"$status_file" | ladybug_freshness_sha256
  local rc=$?
  rm -f "$status_file"
  return "$rc"
}

ladybug_check_projection_freshness() {
  local db_path=${1:-}
  local projection_meta=${2:-}
  local repo_root=${3:-}
  local allow_stale=${4:-${LADYBUG_ALLOW_STALE:-}}
  local label=${5:-ladybug_freshness}

  if ladybug_freshness_truthy "$allow_stale"; then
    return 0
  fi

  [ -n "$db_path" ] || ladybug_freshness_fail "$label" "DB_PATH is required for freshness check" || return 1
  if [ -z "$projection_meta" ]; then
    projection_meta="${db_path}.meta.json"
  fi
  [ -f "$projection_meta" ] || ladybug_freshness_fail "$label" "projection metadata not found: ${projection_meta}; rebuild the projection or pass --allow-stale" || return 1

  if [ -z "$repo_root" ]; then
    repo_root=$(ladybug_freshness_repo_root || true)
  fi
  [ -n "$repo_root" ] || ladybug_freshness_fail "$label" "repo root not found; pass --repo-root or --allow-stale" || return 1

  command -v jq >/dev/null 2>&1 || ladybug_freshness_fail "$label" "jq is required for projection freshness checks" || return 1
  command -v perl >/dev/null 2>&1 || ladybug_freshness_fail "$label" "perl is required for dirty-status freshness checks" || return 1
  if ! command -v shasum >/dev/null 2>&1 && ! command -v sha256sum >/dev/null 2>&1; then
    ladybug_freshness_fail "$label" "shasum or sha256sum is required for dirty-status freshness checks" || return 1
  fi

  local index_head current_head index_dirty index_dirty_hash current_dirty_hash current_dirty reasons
  index_head=$(jq -r '.head_sha // ""' "$projection_meta")
  index_dirty=$(jq -r '.worktree_dirty // false' "$projection_meta")
  index_dirty_hash=$(jq -r '.dirty_status_hash // ""' "$projection_meta")
  current_head=$(git -C "$repo_root" rev-parse HEAD 2>/dev/null || true)
  current_dirty_hash=$(ladybug_freshness_status_hash "$repo_root") || ladybug_freshness_fail "$label" "could not compute current git dirty status" || return 1
  if [ -n "$current_dirty_hash" ]; then
    current_dirty=true
  else
    current_dirty=false
  fi

  reasons=""
  if [ -z "$index_head" ] || [ -z "$current_head" ]; then
    reasons="${reasons}${reasons:+,}head_unknown"
  elif [ "$index_head" != "$current_head" ]; then
    reasons="${reasons}${reasons:+,}head_mismatch"
  fi
  if [ "$index_dirty" != "$current_dirty" ]; then
    reasons="${reasons}${reasons:+,}dirty_state_changed"
  elif [ -n "$index_dirty_hash" ] && [ -n "$current_dirty_hash" ] && [ "$index_dirty_hash" != "$current_dirty_hash" ]; then
    reasons="${reasons}${reasons:+,}dirty_state_changed"
  fi

  if [ -n "$reasons" ]; then
    ladybug_freshness_fail "$label" "stale projection (${reasons}): index_head_sha=${index_head:-unknown} current_head_sha=${current_head:-unknown} index_dirty=${index_dirty} current_dirty=${current_dirty}; rebuild the projection or pass --allow-stale" || return 1
  fi
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  set -euo pipefail

  DB_PATH=${DB_PATH:-}
  PROJECTION_META=${PROJECTION_META:-}
  REPO_ROOT=${REPO_ROOT:-}
  ALLOW_STALE=${LADYBUG_ALLOW_STALE:-}

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --db-path) DB_PATH=$2; shift 2 ;;
      --projection-meta) PROJECTION_META=$2; shift 2 ;;
      --repo-root) REPO_ROOT=$2; shift 2 ;;
      --allow-stale) ALLOW_STALE=1; shift ;;
      -h|--help)
        cat <<'EOF'
Usage: ladybug_freshness.sh --db-path PATH [--projection-meta PATH] [--repo-root DIR] [--allow-stale]
EOF
        exit 0
        ;;
      *) ladybug_freshness_fail "ladybug_freshness" "unknown argument: $1"; exit 1 ;;
    esac
  done

  ladybug_check_projection_freshness "$DB_PATH" "$PROJECTION_META" "$REPO_ROOT" "$ALLOW_STALE" "ladybug_freshness"
fi
