#!/usr/bin/env python3
"""Print a compact summary for a foxctl refactor scout envelope."""

from __future__ import annotations

import json
import sys
from typing import Any


def _as_dict(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _as_list(value: Any) -> list[Any]:
    return value if isinstance(value, list) else []


def main() -> int:
    raw = sys.stdin.read()
    try:
        envelope = json.loads(raw)
    except json.JSONDecodeError:
        print(raw)
        return 0

    if envelope.get("status") != "ok":
        print(json.dumps(envelope, indent=2, sort_keys=True))
        return 0

    data = _as_dict(envelope.get("data"))
    summary = _as_dict(data.get("summary"))
    presentation = _as_dict(data.get("presentation"))
    overview = _as_dict(presentation.get("overview"))

    finding_count = summary.get("finding_count", 0)
    returned_findings = summary.get("returned_findings", 0)
    scanned_files = summary.get("scanned_files", 0)
    scanned_symbols = summary.get("scanned_symbols", 0)
    index_mode = data.get("index_mode", "unknown")
    view = data.get("view", "unknown")

    print(
        "Scout: "
        f"{finding_count} findings, {returned_findings} returned, "
        f"{scanned_files} files, {scanned_symbols} symbols, "
        f"index={index_mode}, view={view}"
    )

    severities = _as_dict(summary.get("severity_counts"))
    if severities:
        parts = [f"{name}={count}" for name, count in sorted(severities.items())]
        print("Severity: " + ", ".join(parts))

    top_files = _as_list(overview.get("top_files"))[:5]
    if top_files:
        print("Top files:")
        for item in top_files:
            row = _as_dict(item)
            file_path = row.get("file", "")
            count = row.get("count", 0)
            score = row.get("max_score", 0)
            symbol = row.get("top_symbol", "")
            print(f"  - {file_path}: {count} findings, max={score}, top={symbol}")

    findings = _as_list(data.get("findings"))[:8]
    if findings:
        print("Returned findings:")
        for item in findings:
            row = _as_dict(item)
            file_path = row.get("file", "")
            line = row.get("line", 0)
            symbol = row.get("symbol", "")
            rule = row.get("rule_id", "")
            score = row.get("score", 0)
            title = row.get("title", "")
            location = f"{file_path}:{line}" if line else file_path
            owner = f" {symbol}" if symbol else ""
            print(f"  - {location}{owner} [{rule} score={score}] {title}")

    snapshot = data.get("snapshot_id")
    evidence = data.get("evidence_artifact")
    if snapshot or evidence:
        print(f"Artifacts: snapshot={snapshot or 'none'} evidence={evidence or 'none'}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
