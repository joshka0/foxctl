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

MAIL_BIN=$(find_mail_binary) || {
  echo "Error: agentctl-mail not found. Build it with: make build" >&2
  exit 1
}

# Pass through all arguments to agentctl-mail
# Map --ack to proper flag format if needed
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
