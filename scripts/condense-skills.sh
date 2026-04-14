#!/usr/bin/env bash
# Condense skill documentation for lower token usage
# Full docs go to docs/skills/, condensed Skill.md stays in ~/.claude/skills/

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCS_DIR="$ROOT_DIR/docs/skills"
USER_SKILLS="${HOME}/.claude/skills"

mkdir -p "$DOCS_DIR"

# Function to extract frontmatter description
get_description() {
    grep -A1 "^description:" "$1" | tail -1 | sed 's/description: //'
}

echo "=== Processing skills ==="

# Process each skill
for skill_dir in "$USER_SKILLS"/foxctl-*/; do
    skill_name=$(basename "$skill_dir")
    skill_file="$skill_dir/Skill.md"

    if [ ! -f "$skill_file" ]; then
        echo "Skipping $skill_name (no Skill.md)"
        continue
    fi

    echo "Processing: $skill_name"

    # Extract name from frontmatter
    name=$(grep "^name:" "$skill_file" | sed 's/name: //')
    desc=$(grep "^description:" "$skill_file" | sed 's/description: //')

    # Copy full content (minus frontmatter) to docs/skills/
    doc_name=$(echo "$skill_name" | sed 's/foxctl-//')
    tail -n +6 "$skill_file" > "$DOCS_DIR/${doc_name}.md"

    echo "  -> Created docs/skills/${doc_name}.md"
done

echo ""
echo "=== Full docs created in docs/skills/ ==="
ls -la "$DOCS_DIR"
