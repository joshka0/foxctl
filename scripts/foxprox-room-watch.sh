#!/usr/bin/env bash
set -euo pipefail

socket="${FOXPROX_SOCKET:-/tmp/foxprox-zellij-rlm.sock}"
room_id=""
interval="2"
limit="50"
seen_file=""
show_existing="false"

usage() {
  cat >&2 <<'EOF'
usage: foxprox-room-watch.sh --room ROOM_ID [--socket PATH] [--interval SECONDS] [--limit N] [--show-existing]

Poll a foxprox room's message history and render new rows as a compact
operator timeline. This is intentionally a viewer-layer helper: the daemon
keeps emitting structured state, while this script decides how to display it.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --room)
      room_id="${2:-}"
      shift 2
      ;;
    --socket)
      socket="${2:-}"
      shift 2
      ;;
    --interval)
      interval="${2:-}"
      shift 2
      ;;
    --limit)
      limit="${2:-}"
      shift 2
      ;;
    --seen-file)
      seen_file="${2:-}"
      shift 2
      ;;
    --show-existing)
      show_existing="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ -z "$room_id" ]]; then
  usage
  exit 2
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi

if [[ -z "$seen_file" ]]; then
  seen_file="${TMPDIR:-/tmp}/foxprox-room-watch-${room_id}.seen"
fi

touch "$seen_file"

endpoint="http://localhost/v1/rooms/${room_id}/messages?limit=${limit}"

render_messages='
  def excerpt:
    gsub("[\r\n\t]+"; " ")
    | if length > 160 then .[0:157] + "..." else . end;

  .messages[]
  | [
      .id,
      .sent_at,
      (.source // "unknown"),
      (.text // "" | excerpt),
      (.delivered // 0 | tostring),
      (.failed // 0 | tostring),
      ((.members // []) | map(.agent_id + "=" + (if .delivered then "delivered" else "failed" end)) | join(","))
    ]
  | @tsv
'

echo "[room-watch] room=${room_id} socket=${socket} interval=${interval}s limit=${limit}"
if [[ "$show_existing" != "true" ]]; then
  curl -sS --unix-socket "$socket" "$endpoint" \
    | jq -r '.messages[]?.id' >> "$seen_file" || true
  sort -u "$seen_file" -o "$seen_file"
  echo "[room-watch] existing messages marked seen; waiting for new traffic"
fi

while true; do
  raw="$(curl -sS --unix-socket "$socket" "$endpoint" 2>&1)" || {
    printf '[room-watch] fetch failed: %s\n' "$raw" >&2
    sleep "$interval"
    continue
  }

  while IFS=$'\t' read -r id sent_at source text delivered failed members; do
    [[ -z "${id:-}" ]] && continue
    if grep -Fxq "$id" "$seen_file"; then
      continue
    fi
    printf '%s\n' "$id" >> "$seen_file"
    if [[ -n "${members:-}" ]]; then
      printf '[room <- %s] %s\n[room -> members] message=%s delivered=%s failed=%s members=%s at=%s\n' \
        "$source" "$text" "$id" "$delivered" "$failed" "$members" "$sent_at"
    else
      printf '[room <- %s] %s\n[room -> members] message=%s delivered=%s failed=%s at=%s\n' \
        "$source" "$text" "$id" "$delivered" "$failed" "$sent_at"
    fi
  done < <(printf '%s' "$raw" | jq -r "$render_messages")

  sort -u "$seen_file" -o "$seen_file"
  sleep "$interval"
done
