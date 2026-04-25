#!/usr/bin/env python3
"""Run one fixed-budget LongCoT RLM experiment and append a TSV ledger row."""

from __future__ import annotations

import argparse
import json
import os
import pathlib
import subprocess
import sys
import time
from datetime import datetime, timezone


LEDGER_COLUMNS = [
    "timestamp",
    "commit",
    "variant",
    "condition",
    "provider",
    "model",
    "domain",
    "difficulty",
    "limit",
    "correct",
    "verified",
    "attempts",
    "status",
    "duration_s",
    "cost_usd",
    "result_json",
    "notes",
]


VARIANTS = {
    "baseline_braid_helper": {
        "condition": "rlm_braid_single",
        "sandbox": "python",
        "general_helper": True,
        "helper_language": "python",
        "blocksworld_helper": False,
        "max_tokens": 8192,
        "max_iterations": 64,
        "timeout": "20m",
    },
    "baseline_braid_no_helper": {
        "condition": "rlm_braid_single",
        "sandbox": "python",
        "general_helper": False,
        "helper_language": "python",
        "blocksworld_helper": False,
        "max_tokens": 8192,
        "max_iterations": 64,
        "timeout": "20m",
    },
}


def repo_root() -> pathlib.Path:
    return pathlib.Path(__file__).resolve().parents[1]


def load_env_file(path: pathlib.Path, env: dict[str, str]) -> None:
    if not path.exists():
        raise SystemExit(f"env file not found: {path}")
    for raw in path.read_text().splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        if key and key not in env:
            env[key] = value


def run(cmd: list[str], cwd: pathlib.Path, env: dict[str, str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        cmd,
        cwd=str(cwd),
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        check=False,
    )


def build_binary(root: pathlib.Path, env: dict[str, str]) -> None:
    binary = root / "bin" / "foxctl"
    binary.parent.mkdir(parents=True, exist_ok=True)
    build_env = dict(env)
    build_env.setdefault("CGO_ENABLED", "0")
    build_env.setdefault("GOCACHE", "/tmp/foxctl-go-build-cache")
    proc = run(["go", "build", "-o", str(binary), "./cmd/foxctl"], root, build_env)
    if proc.returncode != 0:
        sys.stdout.write(proc.stdout)
        raise SystemExit(f"go build failed with exit code {proc.returncode}")


def latest_result_json(output_dir: pathlib.Path) -> pathlib.Path | None:
    candidates = sorted(output_dir.glob("longcot/*/result.json"), key=lambda p: p.stat().st_mtime)
    return candidates[-1] if candidates else None


def summarize_result(path: pathlib.Path | None, returncode: int) -> dict[str, object]:
    if path is None:
        return {
            "correct": 0,
            "verified": 0,
            "attempts": 0,
            "status": f"missing_result_exit_{returncode}",
            "cost_usd": 0.0,
            "notes": "no result.json emitted",
        }
    data = json.loads(path.read_text())
    attempts = data.get("attempts") or []
    correct = 0
    verified = 0
    cost = 0.0
    statuses: list[str] = []
    errors: list[str] = []
    for attempt in attempts:
        if attempt.get("correct") or attempt.get("verifier_status") == "correct":
            correct += 1
        if attempt.get("verifier_status") or attempt.get("correct") or attempt.get("wrong_formatting"):
            verified += 1
        usage = attempt.get("usage") or {}
        cost += float(usage.get("total_cost_usd") or 0.0)
        status = str(attempt.get("status") or "")
        verifier_status = str(attempt.get("verifier_status") or "")
        if status or verifier_status:
            statuses.append("/".join(part for part in [status, verifier_status] if part))
        if attempt.get("error"):
            errors.append(str(attempt["error"]).replace("\t", " ").replace("\n", " ")[:240])
    status = "ok" if returncode == 0 else f"exit_{returncode}"
    if statuses:
        status += ":" + ",".join(statuses[:4])
    notes = "; ".join(errors[:2])
    return {
        "correct": correct,
        "verified": verified,
        "attempts": len(attempts),
        "status": status,
        "cost_usd": cost,
        "notes": notes,
    }


def append_ledger(path: pathlib.Path, row: dict[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    needs_header = not path.exists()
    with path.open("a", encoding="utf-8") as f:
        if needs_header:
            f.write("\t".join(LEDGER_COLUMNS) + "\n")
        f.write("\t".join(str(row.get(col, "")).replace("\t", " ").replace("\n", " ") for col in LEDGER_COLUMNS) + "\n")


def git_commit(root: pathlib.Path) -> str:
    proc = run(["git", "rev-parse", "--short=8", "HEAD"], root, os.environ.copy())
    if proc.returncode != 0:
        return "unknown"
    return proc.stdout.strip()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--variant", choices=sorted(VARIANTS), default="baseline_braid_helper")
    parser.add_argument("--env-file", default="")
    parser.add_argument("--longcot-repo", default="/Users/joshka/repos/githubs/LongCoT")
    parser.add_argument("--provider", default=os.environ.get("LONGCOT_PROVIDER", "openrouter"))
    parser.add_argument("--model", default=os.environ.get("LONGCOT_MODEL", "google/gemini-3.1-flash-lite-preview"))
    parser.add_argument("--domain", default="logic")
    parser.add_argument("--difficulty", default="easy")
    parser.add_argument("--limit", type=int, default=1)
    parser.add_argument("--output-root", default=".foxctl/longcot-autoresearch")
    parser.add_argument("--ledger", default=".foxctl/longcot-autoresearch/results.tsv")
    parser.add_argument("--no-build", action="store_true")
    args = parser.parse_args()

    root = repo_root()
    env = os.environ.copy()
    if args.env_file:
        load_env_file(pathlib.Path(args.env_file).expanduser(), env)
    env.setdefault("GOCACHE", "/tmp/foxctl-go-build-cache")
    env.setdefault("CGO_ENABLED", "0")

    if not args.no_build:
        build_binary(root, env)

    variant = VARIANTS[args.variant]
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    output_dir = root / args.output_root / f"{stamp}-{args.variant}"
    output_dir.mkdir(parents=True, exist_ok=True)
    log_path = output_dir / "run.log"

    cmd = [
        str(root / "bin" / "foxctl"),
        "eval",
        "longcot",
        "--longcot-repo",
        args.longcot_repo,
        "--domain",
        args.domain,
        "--difficulty",
        args.difficulty,
        "--limit",
        str(args.limit),
        "--condition",
        str(variant["condition"]),
        "--provider",
        args.provider,
        "--model",
        args.model,
        "--timeout",
        str(variant["timeout"]),
        "--max-tokens",
        str(variant["max_tokens"]),
        "--max-iterations",
        str(variant["max_iterations"]),
        "--sandbox",
        str(variant["sandbox"]),
        "--helper-language",
        str(variant["helper_language"]),
        "--save",
        "--output-dir",
        str(output_dir),
        "--verify",
        "--format",
        "markdown",
    ]
    if variant["general_helper"]:
        cmd.append("--general-helper")
    if not variant["blocksworld_helper"]:
        cmd.append("--blocksworld-helper=false")

    start = time.time()
    proc = run(cmd, root, env)
    duration = time.time() - start
    log_path.write_text(proc.stdout, encoding="utf-8")

    result_path = latest_result_json(output_dir)
    summary = summarize_result(result_path, proc.returncode)
    row = {
        "timestamp": stamp,
        "commit": git_commit(root),
        "variant": args.variant,
        "condition": variant["condition"],
        "provider": args.provider,
        "model": args.model,
        "domain": args.domain,
        "difficulty": args.difficulty,
        "limit": args.limit,
        "correct": summary["correct"],
        "verified": summary["verified"],
        "attempts": summary["attempts"],
        "status": summary["status"],
        "duration_s": f"{duration:.1f}",
        "cost_usd": f"{float(summary['cost_usd']):.6f}",
        "result_json": str(result_path) if result_path else "",
        "notes": summary["notes"],
    }
    append_ledger(root / args.ledger, row)
    print("\t".join(str(row[col]) for col in LEDGER_COLUMNS))
    print(f"log: {log_path}")
    return proc.returncode


if __name__ == "__main__":
    raise SystemExit(main())
