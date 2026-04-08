#!/usr/bash
# check_large_files.sh — Fail if any tracked file exceeds the size threshold.
# Usage: ./scripts/check_large_files.sh [max_bytes]
# Default: 512000 (500 KB)
set -euo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT_DIR"

MAX_BYTES="${1:-512000}"
BASE_REF="${CHECK_LARGE_FILES_BASE_REF:-}"

# Skip patterns for known large/generated files
SKIP_EXT='\.db$|\.db-shm$|\.db-wal$|\.wasm$|\.so$|\.dylib$|\.png$|\.jpg$|\.jpeg$|\.gif$|\.ico$|\.woff2?$|\.ttf$|\.eot$|\.ndjson$|\.test$'

# Only check source files (Go, TS, JS, YAML, JSON, MD, etc.)
INCLUDE_EXT='\.(go|ts|tsx|js|jsx|py|yaml|yml|json|md|toml|mod|sum|sql|proto|graphql)$'

large_files=()

check_path_size() {
	local path="$1"
	local size="$2"
	if [ "$size" -le "$MAX_BYTES" ]; then
		return 0
	fi
	if ! echo "$path" | grep -qE "$INCLUDE_EXT"; then
		return 0
	fi
	large_files+=("$path ($size bytes)")
}

if [ -n "$BASE_REF" ] && git rev-parse --verify "$BASE_REF" >/dev/null 2>&1; then
	while IFS= read -r path; do
		[ -n "$path" ] || continue
		size="$(git cat-file -s "HEAD:$path" 2>/dev/null || true)"
		[ -n "$size" ] || continue
		check_path_size "$path" "$size"
	done < <(git diff --name-only --diff-filter=A "$BASE_REF...HEAD")
else
	# Use git ls-tree for speed (no working tree stat calls for untracked files)
	while IFS=$'\t' read -r mode_type size path; do
		check_path_size "$path" "$size"
	done < <(git ls-tree -r -l HEAD | awk '{print $1"\t"$4"\t"$5}')
fi

if [ ${#large_files[@]} -gt 0 ]; then
	echo "ERROR: Found files exceeding ${MAX_BYTES} byte threshold:"
	printf '  - %s\n' "${large_files[@]}"
	echo ""
	echo "Consider:"
	echo "  - Splitting large files into smaller modules"
	echo "  - Using .gitattributes + Git LFS for large binaries"
	echo "  - Excluding generated files from tracking"
	exit 1
fi

echo "OK: All tracked files under ${MAX_BYTES} byte threshold."
