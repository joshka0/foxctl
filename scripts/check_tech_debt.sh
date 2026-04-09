#!/usr/bin/env bash
# check_tech_debt.sh — Scan for TODO/FIXME/HACK markers and report.
# Fails if count exceeds threshold (default: 200).
# Usage: ./scripts/check_tech_debt.sh [max_count]
set -euo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT_DIR"

MAX_COUNT="${1:-200}"

# Skip vendored, generated, and dependency directories
SKIP_DIRS='node_modules|vendor|\.git|dist|\.gocache|\.gomodcache|bun\.lock'

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
		relpath="${file#$ROOT_DIR/}"
		echo "--- $relpath ---"
		echo "$matches"
		echo ""
		found=$(echo "$matches" | wc -l | tr -d ' ')
		count=$((count + found))
	fi
done < <(find . -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.jsx' -o -name '*.py' -o -name '*.md' \) \
	-not -path "*/vendor/*" \
	-not -path "*/node_modules/*" \
	-not -path "*/.git/*" \
	-not -path "*/dist/*" \
	-not -path "*/.cache/*" \
	-not -path "*/.gocache/*" \
	-not -path "*/.gomodcache/*" \
	-print0 2>/dev/null)

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
