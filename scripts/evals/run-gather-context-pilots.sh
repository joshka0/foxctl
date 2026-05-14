#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${OUT:-/tmp/gather-context-pilots/$(date -u +%Y%m%dT%H%M%SZ)}"
FOXCTL_BIN="${FOXCTL_BIN:-$OUT/foxctl}"
PRAZE_REPO="${PRAZE_REPO:-$HOME/repos/personal/praze}"
HEARTWOOD_REPO="${HEARTWOOD_REPO:-$HOME/repos/personal/heartwood}"
DEARDAY_REPO="${DEARDAY_REPO:-$HOME/repos/personal/dearday}"
OVERCHARGE_REPO="${OVERCHARGE_REPO:-$HOME/repos/personal/overcharge}"
FRESH_CLONE="${FRESH_CLONE:-0}"
CLONE_ROOT="${CLONE_ROOT:-/tmp/gather-context-pilot-repos/$(date -u +%Y%m%dT%H%M%SZ)}"
# RUN_NATIVE is the legacy name for this provider-backed eval-agent lane.
# Prefer RUN_PROVIDER_AGENT for new invocations.
RUN_NATIVE="${RUN_NATIVE:-1}"
RUN_PROVIDER_AGENT="${RUN_PROVIDER_AGENT:-$RUN_NATIVE}"
RUN_RLM="${RUN_RLM:-0}"
REBUILD_INDEX="${REBUILD_INDEX:-0}"
RUN_REPOS="${RUN_REPOS:-praze,heartwood,dearday}"
CASE_LIMIT="${CASE_LIMIT:-0}"
CASE_ID_GREP="${CASE_ID_GREP:-}"
# NATIVE_TARGET is kept as a compatibility alias; prefer PROVIDER_AGENT_TARGET.
NATIVE_TARGET="${NATIVE_TARGET:-openai:gpt-5.4-mini}"
PROVIDER_AGENT_TARGET="${PROVIDER_AGENT_TARGET:-$NATIVE_TARGET}"
RLM_TARGET="${RLM_TARGET:-openai:gpt-5.4-mini}"
TIMEOUT="${TIMEOUT:-90s}"
NATIVE_TIMEOUT="${NATIVE_TIMEOUT:-180s}"
NATIVE_MAX_ITERATIONS="${NATIVE_MAX_ITERATIONS:-8}"
RLM_MAX_ITERATIONS="${RLM_MAX_ITERATIONS:-5}"
LIMIT="${LIMIT:-8}"
MAX_CONTEXT_CHARS="${MAX_CONTEXT_CHARS:-9000}"
PASS_THRESHOLD="${PASS_THRESHOLD:-0.8}"

mkdir -p "$OUT"
if [[ "$FRESH_CLONE" == "1" ]]; then
  mkdir -p "$CLONE_ROOT"
fi

echo "Building foxctl -> $FOXCTL_BIN"
(
  cd "$ROOT"
  env -u GOROOT -u GOBIN -u GOTOOLDIR \
    GOCACHE="${GOCACHE:-/tmp/foxctl-go-build-cache}" \
    CGO_ENABLED="${CGO_ENABLED:-0}" \
    go build -o "$FOXCTL_BIN" ./cmd/foxctl
)

write_repo_state() {
  local repo="$1"
  local dest="$2"
  {
    echo "repo=$repo"
    if git -C "$repo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
      echo "head=$(git -C "$repo" rev-parse HEAD)"
      echo "branch=$(git -C "$repo" rev-parse --abbrev-ref HEAD)"
      echo
      echo "status:"
      git -C "$repo" status --short
    else
      echo "not a git worktree"
    fi
  } > "$dest"
}

write_repo_state_json() {
  local repo="$1"
  local dest="$2"
  node - "$repo" "$dest" <<'NODE'
const fs = require("fs");
const cp = require("child_process");
const repo = process.argv[2];
const dest = process.argv[3];

function git(args) {
  try {
    return cp.execFileSync("git", ["-C", repo, ...args], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    return "";
  }
}

const inside = git(["rev-parse", "--is-inside-work-tree"]) === "true";
const state = {
  path: repo,
  is_git_worktree: inside,
};

if (inside) {
  const remotes = git(["remote", "-v"])
    .split(/\n/)
    .filter(Boolean)
    .map((line) => line.replace(/\s+/g, " "));
  const status = git(["status", "--short"]);
  state.head = git(["rev-parse", "HEAD"]);
  state.branch = git(["rev-parse", "--abbrev-ref", "HEAD"]);
  state.remote_origin_url = git(["remote", "get-url", "origin"]);
  state.remotes = remotes;
  state.dirty = status.length > 0;
  state.status_short = status ? status.split(/\n/) : [];
}

fs.writeFileSync(dest, JSON.stringify(state, null, 2) + "\n");
NODE
}

repo_source_url() {
  local repo="$1"
  local name="$2"
  local env_name
  env_name="$(printf '%s_REPO_URL' "${name^^}")"
  local explicit="${!env_name:-}"
  if [[ -n "$explicit" ]]; then
    echo "$explicit"
    return 0
  fi
  if git -C "$repo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git -C "$repo" remote get-url origin 2>/dev/null || echo "$repo"
    return 0
  fi
  echo "$repo"
}

prepare_repo() {
  local name="$1"
  local repo="$2"
  if [[ "$FRESH_CLONE" != "1" ]]; then
    echo "$repo"
    return 0
  fi

  local source
  source="$(repo_source_url "$repo" "$name")"
  local dest="$CLONE_ROOT/$name"
  if [[ -e "$dest" ]]; then
    echo "fresh clone destination already exists: $dest" >&2
    return 1
  fi
  echo "Fresh-cloning $name from $source -> $dest" >&2
  git clone --no-local "$source" "$dest" >&2
  echo "$dest"
}

attach_repo_state() {
  local report="$1"
  local repo_state_json="$2"
  node - "$report" "$repo_state_json" <<'NODE'
const fs = require("fs");
const reportPath = process.argv[2];
const statePath = process.argv[3];
const report = JSON.parse(fs.readFileSync(reportPath, "utf8"));
report.repo_state = JSON.parse(fs.readFileSync(statePath, "utf8"));
fs.writeFileSync(reportPath, JSON.stringify(report, null, 2) + "\n");
NODE
}

maybe_rebuild_index() {
	local repo="$1"
	local name="$2"
  if [[ "$REBUILD_INDEX" != "1" ]]; then
    return 0
  fi
	echo "Rebuilding repo index for $name"
	case "$name" in
	praze)
		"$FOXCTL_BIN" index repo build --workspace "$repo" --go=false --typescript --elixir || true
		;;
	heartwood)
		"$FOXCTL_BIN" index repo build --workspace "$repo" --go=false --typescript --elixir=false || true
		;;
	dearday)
		"$FOXCTL_BIN" index repo build --workspace "$repo" --go=false --typescript=false --python --elixir=false || true
		;;
	overcharge)
		"$FOXCTL_BIN" index repo build --workspace "$repo" --go=false --typescript=false --python=false --rust --csharp --elixir=false --include-tests || true
		;;
	*)
		"$FOXCTL_BIN" index repo build --workspace "$repo" --go=false --typescript --elixir=false || true
		;;
	esac
}

results_to_jsonl() {
  local report="$1"
  local jsonl="$2"
  node - "$report" "$jsonl" <<'NODE'
const fs = require("fs");
const reportPath = process.argv[2];
const outPath = process.argv[3];
const report = JSON.parse(fs.readFileSync(reportPath, "utf8"));
const results = Array.isArray(report.results) ? report.results : [];
fs.writeFileSync(outPath, results.map((row) => JSON.stringify(row)).join("\n") + (results.length ? "\n" : ""));
NODE
}

filtered_dataset() {
  local name="$1"
  local dataset="$2"
  local dir="$3"
  local out="$dir/eval-cases.jsonl"
  node - "$dataset" "$out" "$CASE_LIMIT" "$CASE_ID_GREP" <<'NODE'
const fs = require("fs");
const input = process.argv[2];
const output = process.argv[3];
const limit = Number(process.argv[4] || "0");
const grep = process.argv[5] || "";
let rows = fs.readFileSync(input, "utf8").split(/\n/).filter(Boolean).map((line) => JSON.parse(line));
if (grep) {
  const re = new RegExp(grep);
  rows = rows.filter((row) => re.test(row.id || ""));
}
if (limit > 0) {
  rows = rows.slice(0, limit);
}
if (rows.length === 0) {
  throw new Error(`no eval rows selected for ${input}`);
}
fs.writeFileSync(output, rows.map((row) => JSON.stringify(row)).join("\n") + "\n");
NODE
  echo "$out"
}

run_repo() {
  local name="$1"
  local configured_repo="$2"
  local dataset="$3"
  local dir="$OUT/$name"
  mkdir -p "$dir"
  local repo
  repo="$(prepare_repo "$name" "$configured_repo")"
  local selected_dataset
  selected_dataset="$(filtered_dataset "$name" "$dataset" "$dir")"

  echo "== $name =="
  write_repo_state "$repo" "$dir/repo-state.txt"
  write_repo_state_json "$repo" "$dir/repo-state.json"
  maybe_rebuild_index "$repo" "$name"

  echo "Running deterministic gather_context for $name"
  "$FOXCTL_BIN" eval gather-context \
    --workspace "$repo" \
    --eval-dataset-file "$selected_dataset" \
    --tool-profile gather-context \
    --lane code \
    --limit "$LIMIT" \
    --max-context-chars "$MAX_CONTEXT_CHARS" \
    --timeout "$TIMEOUT" \
    --pass-threshold "$PASS_THRESHOLD" \
    --report-file "$dir/gather-context.json" \
    > "$dir/gather-context.out.json"
  attach_repo_state "$dir/gather-context.json" "$dir/repo-state.json"

  if [[ "$RUN_PROVIDER_AGENT" == "1" ]]; then
    echo "Running provider-backed foxctl eval-agent baseline for $name ($PROVIDER_AGENT_TARGET)"
    "$FOXCTL_BIN" eval agents \
      --workspace "$repo" \
      --eval-dataset-file "$selected_dataset" \
      --role researcher \
      --target "$PROVIDER_AGENT_TARGET" \
      --timeout "$NATIVE_TIMEOUT" \
      --max-iterations "$NATIVE_MAX_ITERATIONS" \
      --pass-threshold "$PASS_THRESHOLD" \
      --report-file "$dir/provider-agent.json" \
      > "$dir/provider-agent.out.json"
    attach_repo_state "$dir/provider-agent.json" "$dir/repo-state.json"
    results_to_jsonl "$dir/provider-agent.json" "$dir/provider-agent-results.jsonl"

    echo "Running gather_context comparison import for $name"
    "$FOXCTL_BIN" eval gather-context \
      --workspace "$repo" \
      --eval-dataset-file "$selected_dataset" \
      --tool-profile gather-context \
      --lane code \
      --limit "$LIMIT" \
      --max-context-chars "$MAX_CONTEXT_CHARS" \
      --timeout "$TIMEOUT" \
      --pass-threshold "$PASS_THRESHOLD" \
      --agent-baseline-results "$dir/provider-agent-results.jsonl" \
      --report-file "$dir/head-to-head.json" \
      > "$dir/head-to-head.out.json"
    attach_repo_state "$dir/head-to-head.json" "$dir/repo-state.json"
  fi

  if [[ "$RUN_RLM" == "1" ]]; then
    echo "Running gather_context + RLM search-agent comparison for $name ($RLM_TARGET)"
    "$FOXCTL_BIN" eval gather-context \
      --workspace "$repo" \
      --eval-dataset-file "$selected_dataset" \
      --tool-profile gather-context \
      --lane code \
      --limit "$LIMIT" \
      --max-context-chars "$MAX_CONTEXT_CHARS" \
      --timeout "$NATIVE_TIMEOUT" \
      --pass-threshold "$PASS_THRESHOLD" \
      --rlm-agent-target "$RLM_TARGET" \
      --rlm-agent-plan-mode rerank \
      --rlm-agent-max-iterations "$RLM_MAX_ITERATIONS" \
      --report-file "$dir/rlm-rerank.json" \
      > "$dir/rlm-rerank.out.json"
    attach_repo_state "$dir/rlm-rerank.json" "$dir/repo-state.json"
  fi
}

if [[ ",$RUN_REPOS," == *",praze,"* ]]; then
  run_repo "praze" "$PRAZE_REPO" "$ROOT/testdata/evals/gather-context/pilots/praze.jsonl"
fi
if [[ ",$RUN_REPOS," == *",heartwood,"* ]]; then
	run_repo "heartwood" "$HEARTWOOD_REPO" "$ROOT/testdata/evals/gather-context/pilots/heartwood.jsonl"
fi
if [[ ",$RUN_REPOS," == *",dearday,"* ]]; then
	run_repo "dearday" "$DEARDAY_REPO" "$ROOT/testdata/evals/gather-context/pilots/dearday.jsonl"
fi
if [[ ",$RUN_REPOS," == *",overcharge,"* ]]; then
	run_repo "overcharge" "$OVERCHARGE_REPO" "$ROOT/testdata/evals/gather-context/pilots/overcharge.jsonl"
fi

echo "Pilot reports written under $OUT"
