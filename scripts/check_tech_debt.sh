#!/usr/bin/env bash
# check_tech_debt.sh — Scan for TODO/FIXME/HACK markers and report.
# Fails if count exceeds threshold (default: 200).
# Usage: ./scripts/check_tech_debt.sh [max_count]
set -euo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT_DIR"

MAX_COUNT="${1:-200}"

echo "Scanning for TODO/FIXME/HACK markers..."
echo ""

count=0
while IFS= read -r -d '' file; do
	# Skip lock files and binary-ish files
	case "$file" in
		*.lock|*.sum|*.db|*.wasm|*.so|*.dylib) continue ;;
	esac

	matches=$(grep -nE '\b(TODO|FIXME|HACK)\b' "$file" 2>/dev/null || true)
	if [ -n "$matches" ]; then
		echo "--- $file ---"
		echo "$matches"
		echo ""
		found=$(echo "$matches" | wc -l | tr -d ' ')
		count=$((count + found))
	fi
done < <(git ls-files -z -- \
	'*.go' '*.ts' '*.tsx' '*.js' '*.jsx' '*.py' '*.md' \
	':!vendor/**' \
	':!node_modules/**' \
	':!dist/**' \
	':!.cache/**' \
	':!.gocache/**' \
	':!.gomodcache/**' \
	':!.mutation-reports/**' \
	':!.foxctl/**')

echo "=============================="
echo "Total TODO/FIXME/HACK markers: $count"
echo "Threshold: $MAX_COUNT"
echo "=============================="

if [ "$count" -gt "$MAX_COUNT" ]; then
	echo ""
	echo "FAIL: Tech debt markers ($count) exceed threshold ($MAX_COUNT)."
	echo "Consider resolving some TODOs or bumping the threshold in CI."
	exit 1
fi

echo "OK: Tech debt markers within threshold."
