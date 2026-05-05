#!/usr/bin/env bash
# build-guard.sh - Project-specific build safety checks
#
# Blocks dangerous go build patterns that cause issues in this project:
#   - go build ./skills/ → Without -o, creates binaries at repo root
#
# This is project-specific. For general tool redirection, see strict-mode-enforce.sh

set -euo pipefail

INPUT=$(cat)
tool_name=$(echo "$INPUT" | jq -r '.tool_name // ""')

[[ "$tool_name" != "Bash" ]] && { echo '{}'; exit 0; }

command=$(echo "$INPUT" | jq -r '.tool_input.command // ""')
[[ -z "$command" || "$command" == "null" ]] && { echo '{}'; exit 0; }

# Only check commands starting with go build
if ! echo "$command" | grep -qE '^(CGO_ENABLED=[01]\s+)?go\s+build'; then
  echo '{}'
  exit 0
fi

# BLOCK: go build ./skills/ without -o
if echo "$command" | grep -qE '^(CGO_ENABLED=[01]\s+)?go\s+build\s+\./skills/' && ! echo "$command" | grep -qE '\s-o\s'; then
  skill_name=$(echo "$command" | grep -oE 'skills/[^/\s]+' | head -1 | sed 's|skills/||')
  jq -n --arg skill "$skill_name" '{
    decision: "block",
    reason: ("**[Build Guard] BLOCKED: go build ./skills/ without -o**\n\nCreates binary at repo root instead of ~/.foxctl/skills/\n\n**Use instead:**\n- `make skill SKILL=" + $skill + "` — Builds & installs correctly\n- `make skills-build` — Build all skills to dist/skills/")
  }'
  exit 0
fi

echo '{}'
