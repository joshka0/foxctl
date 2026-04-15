#!/usr/bin/env bash
# overseer-send.sh - Send a message to the overseer mailbox
#
# This is a thin wrapper around foxctl-mail for backwards compatibility.
# Prefer using foxctl-mail directly:
#
#   foxctl-mail "Subject" "Body"
#   foxctl-mail -p 1 "URGENT" "Stop and review this"
#   foxctl-mail --ack "Review needed" "Check the API changes"
#
# Usage:
#   overseer-send.sh "Subject line" "Message body"
#   overseer-send.sh -p 1 "Urgent" "Please stop and address this first"
#
# Options:
#   -p, --priority NUM    Priority 1-5 (1=highest, default=3)
#   -a, --ack             Require acknowledgment
#   -h, --help            Show help

set -euo pipefail

# Find foxctl-mail binary (check common locations)
find_mail_binary() {
  # Check if in PATH
  if command -v foxctl-mail &>/dev/null; then
    echo "foxctl-mail"
    return
  fi

  # Check repo bin directory
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_bin="$script_dir/../bin/foxctl-mail"
  if [[ -x "$repo_bin" ]]; then
    echo "$repo_bin"
    return
  fi

  # Check FOXCTL_HOME
  if [[ -n "${FOXCTL_HOME:-}" && -x "$FOXCTL_HOME/bin/foxctl-mail" ]]; then
    echo "$FOXCTL_HOME/bin/foxctl-mail"
    return
  fi

  # Check default location
  if [[ -x "$HOME/.foxctl/bin/foxctl-mail" ]]; then
    echo "$HOME/.foxctl/bin/foxctl-mail"
    return
  fi

  return 1
}

MAIL_BIN="$(find_mail_binary 2>/dev/null || true)"

if [[ -n "${MAIL_BIN:-}" ]]; then
  args=()
  while [[ $# -gt 0 ]]; do
    case $1 in
      -a)
        args+=("--ack")
        shift
        ;;
      *)
        args+=("$1")
        shift
        ;;
    esac
  done

  exec "$MAIL_BIN" "${args[@]}"
fi

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
if ! command -v "$FOXCTL_BIN" &>/dev/null; then
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_bin="$script_dir/../bin/foxctl"
  if [[ -x "$repo_bin" ]]; then
    FOXCTL_BIN="$repo_bin"
  else
    echo "Error: foxctl not found" >&2
    exit 1
  fi
fi

if ! command -v jq &>/dev/null; then
  echo "Error: jq not found" >&2
  exit 1
fi

priority=3
ack_required=false
positionals=()

while [[ $# -gt 0 ]]; do
  case $1 in
    -p|--priority)
      priority="${2:-3}"
      shift 2
      ;;
    -a|--ack)
      ack_required=true
      shift
      ;;
    -h|--help)
      echo "Usage: overseer-send.sh [-p NUM] [-a] <subject> <body>" >&2
      exit 0
      ;;
    *)
      positionals+=("$1")
      shift
      ;;
  esac
done

if [[ ${#positionals[@]} -lt 2 ]]; then
  echo "Error: subject and body are required" >&2
  exit 1
fi

subject="${positionals[0]}"
body="${positionals[1]}"

workspace_id="${OPENCODE_PROJECT_DIR:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"

payload=$(jq -nc \
  --arg ws "$workspace_id" \
  --arg sender "human" \
  --arg recipient "overseer" \
  --arg subject "$subject" \
  --arg body "$body" \
  --argjson priority "$priority" \
  --argjson ack_required "$ack_required" \
  '{
    operation: "send",
    workspace_id: $ws,
    send: {
      sender: $sender,
      recipient: $recipient,
      subject: $subject,
      body: $body,
      priority: $priority,
      ack_required: $ack_required
    }
  }'
)

exec "$FOXCTL_BIN" run mailbox/manage --input "$payload"
