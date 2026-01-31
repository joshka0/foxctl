#!/usr/bin/env bash
# security-scanner.sh - Claude Code PreToolUse hook for security scanning
# Scans the file being edited for security vulnerabilities and injects warnings.
# Non-blocking (advisory only) - always approves but may inject context.
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary (default: agentctl)
#   AGENTCTL_SECURITY_THRESHOLD - Minimum severity: low, medium, high, critical (default: medium)

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
SEVERITY_THRESHOLD="${AGENTCTL_SECURITY_THRESHOLD:-medium}"

# Read hook input from stdin
INPUT=$(cat)

# Extract file path from tool_input
file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

# Skip if no file path
if [[ -z "$file_path" || "$file_path" == "null" ]]; then
  echo '{}'
  exit 0
fi

# Skip non-code files
case "$file_path" in
  *.md|*.txt|*.json|*.yaml|*.yml|*.toml|*.lock|*.sum|*.mod)
    echo '{}'
    exit 0
    ;;
esac

# Run security scan on the specific file
result=$("$AGENTCTL_BIN" run --daemon code/security --input "{\"path\":\"$file_path\",\"severity_threshold\":\"$SEVERITY_THRESHOLD\",\"max_results\":5}" 2>/dev/null) || {
  # On error, fail open
  echo '{}'
  exit 0
}

# Extract vulnerability count and details
vuln_count=$(echo "$result" | jq -r '.data.vulnerability_count // 0')

if [[ "$vuln_count" -eq 0 ]]; then
  echo '{}'
  exit 0
fi

# Build context message with top vulnerabilities
context="## Security Warning

Found **$vuln_count** potential security issue(s) in this file:

"

# Add each vulnerability
vulns=$(echo "$result" | jq -r '.data.vulnerabilities // []')
context+=$(echo "$vulns" | jq -r '
  .[:3] |
  to_entries |
  map("- **\(.value.severity | ascii_upcase)**: \(.value.issue) (line \(.value.line))\n  - \(.value.recommendation)") |
  join("\n")
')

context+="

*Review before committing. Use \`agentctl run code/security\` for full scan.*"

# Return approve with context (non-blocking warning)
jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
