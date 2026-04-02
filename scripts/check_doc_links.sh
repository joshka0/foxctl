#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT_DIR"

TMP_ERRORS="$(mktemp)"
trap 'rm -f "$TMP_ERRORS"' EXIT

collect_files() {
  find docs -type f -name '*.md' | sort
  for f in AGENTS.md README.md CONTRIBUTING.md CLAUDE_INTEGRATION_GUIDE.md; do
    if [[ -f "$f" ]]; then
      echo "$f"
    fi
  done
}

while IFS= read -r file; do
  tmp_stripped="$(mktemp)"

  # Keep line numbers stable while removing fenced code blocks.
  awk '
    BEGIN { in_fence = 0 }
    {
      if ($0 ~ /^```/ || $0 ~ /^~~~/) {
        in_fence = !in_fence
        print ""
        next
      }
      if (in_fence) {
        print ""
        next
      }
      print
    }
  ' "$file" > "$tmp_stripped"

  while IFS=: read -r line match; do
    target="$(printf '%s' "$match" | sed -E 's/^\[[^]]+\]\(([^)]+)\)$/\1/')"
    base="${target%%#*}"
    base="${base%%\?*}"

    case "$base" in
      ""|http://*|https://*|mailto:*|\#*|file://*|vscode://*|cci:*|data:*)
        continue
        ;;
    esac

    # Skip malformed pseudo-links that are usually from prose/code signatures.
    if [[ "$base" =~ [[:space:]] ]]; then
      continue
    fi
    if [[ "$base" != */* && "$base" != .* && "$base" != *.* ]]; then
      continue
    fi

    if [[ "$base" == /* ]]; then
      candidate=".$base"
      if [[ ! -e "$candidate" ]]; then
        printf '%s:%s -> %s\n' "$file" "$line" "$base" >> "$TMP_ERRORS"
      fi
      continue
    fi

    file_dir="$(dirname "$file")"
    candidate="$ROOT_DIR/$file_dir/$base"

    if [[ ! -e "$candidate" ]]; then
      printf '%s:%s -> %s\n' "$file" "$line" "$base" >> "$TMP_ERRORS"
    fi
  done < <(grep -nEo '\[[^][]+\]\([^)]+\)' "$tmp_stripped" || true)

  rm -f "$tmp_stripped"
done < <(collect_files)

if [[ -s "$TMP_ERRORS" ]]; then
  echo "Broken local Markdown links found:"
  sort -u "$TMP_ERRORS"
  exit 1
fi

echo "Docs link check passed."
