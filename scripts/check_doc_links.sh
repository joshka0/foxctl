#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT_DIR"

python3 - <<'PY'
import re
import sys
from pathlib import Path

ROOT = Path.cwd()
LINK_RE = re.compile(r"\[([^][]+)\]\(([^)]+)\)")
SKIP_PREFIXES = ("http://", "https://", "mailto:", "#", "file://", "vscode://", "cci:", "data:")
ROOT_FILES = ("AGENTS.md", "README.md", "CONTRIBUTING.md", "CLAUDE_INTEGRATION_GUIDE.md")


def collect_files() -> list[Path]:
    files = sorted((ROOT / "docs").rglob("*.md"))
    for name in ROOT_FILES:
        path = ROOT / name
        if path.exists():
            files.append(path)
    return files


def stripped_lines(path: Path) -> list[str]:
    lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
    out: list[str] = []
    in_fence = False
    for line in lines:
        if line.startswith("```") or line.startswith("~~~"):
            in_fence = not in_fence
            out.append("")
            continue
        if in_fence:
            out.append("")
            continue
        out.append(line)
    return out


def should_skip(base: str) -> bool:
    if base == "":
        return True
    if base.startswith(SKIP_PREFIXES):
        return True
    if any(ch.isspace() for ch in base):
        return True
    if "/" not in base and not base.startswith(".") and "." not in base:
        return True
    return False


def resolve_candidate(file_path: Path, base: str) -> Path:
    if base.startswith("/"):
        return ROOT / base.lstrip("/")
    return ROOT / file_path.parent.relative_to(ROOT) / base


errors: set[str] = set()

for path in collect_files():
    for lineno, line in enumerate(stripped_lines(path), start=1):
        for match in LINK_RE.finditer(line):
            target = match.group(2)
            base = target.split("#", 1)[0].split("?", 1)[0]
            if should_skip(base):
                continue
            candidate = resolve_candidate(path, base)
            if not candidate.exists():
                errors.add(f"{path.relative_to(ROOT)}:{lineno} -> {base}")

if errors:
    print("Broken local Markdown links found:")
    for item in sorted(errors):
        print(item)
    sys.exit(1)

print("Docs link check passed.")
PY
