#!/usr/bin/env bash
# overseer-send.sh - Send a message to the overseer mailbox
#
# This is a thin wrapper around agentctl-mail for backwards compatibility.
# Prefer using agentctl-mail directly:
#
#   agentctl-mail "Subject" "Body"
#   agentctl-mail -p 1 "URGENT" "Stop and review this"
#   agentctl-mail --ack "Review needed" "Check the API changes"
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

# Find agentctl-mail binary (check common locations)
find_mail_binary() {
  # Check if in PATH
  if command -v agentctl-mail &>/dev/null; then
    echo "agentctl-mail"
    return
  fi

  # Check repo bin directory
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_bin="$script_dir/../bin/agentctl-mail"
  if [[ -x "$repo_bin" ]]; then
    echo "$repo_bin"
    return
  fi

  # Check AGENTCTL_HOME
  if [[ -n "${AGENTCTL_HOME:-}" && -x "$AGENTCTL_HOME/bin/agentctl-mail" ]]; then
    echo "$AGENTCTL_HOME/bin/agentctl-mail"
    return
  fi

  # Check default location
  if [[ -x "$HOME/.agentctl/bin/agentctl-mail" ]]; then
    echo "$HOME/.agentctl/bin/agentctl-mail"
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

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
if ! command -v "$AGENTCTL_BIN" &>/dev/null; then
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_bin="$script_dir/../bin/agentctl"
  if [[ -x "$repo_bin" ]]; then
    AGENTCTL_BIN="$repo_bin"
  else
    echo "Error: agentctl not found" >&2
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

exec "$AGENTCTL_BIN" run mailbox/manage --input "$payload"
