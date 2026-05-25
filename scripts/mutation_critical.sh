#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'USAGE'
Usage: scripts/mutation_critical.sh [--list|--dry-run|--run]

Runs an opt-in mutation gate for critical Go packages.

Modes:
  --list     Write the report skeleton and print the package commands without running them.
  --dry-run  Ask the mutation runner to discover covered mutants without executing mutated tests.
  --run      Execute mutation testing and fail if the runner or thresholds fail.

Environment:
  GO                         Go binary to use when the mutation runner is not on PATH.
  MUTATION_PACKAGES          Space-separated package paths to check.
  MUTATION_REPORT_DIR        Directory for summary, stdout logs, JSON output, and temporary files.
  MUTATION_WORKERS           Number of runner workers. Default: 2.
  MUTATION_TIMEOUT           Per-package timeout. Default: 20m.
  MUTATION_THRESHOLD_EFFICACY Run-mode efficacy threshold. Default: 70.
  MUTATION_THRESHOLD_MCOVER   Run-mode mutant-coverage threshold. Default: 20.
  MUTATION_TAGS              Optional comma-separated Go build tags.
  MUTATION_COVERPKG          Optional comma-separated coverpkg patterns.
USAGE
}

mode=run
while [[ $# -gt 0 ]]; do
	case "$1" in
		--list)
			mode=list
			;;
		--dry-run)
			mode=dry-run
			;;
		--run)
			mode=run
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

go_bin="${GO:-}"
if [[ -z "$go_bin" ]]; then
	if command -v go >/dev/null 2>&1; then
		go_bin="$(command -v go)"
	elif [[ -x /usr/local/go/bin/go ]]; then
		go_bin=/usr/local/go/bin/go
	else
		go_bin=go
	fi
fi

mutation_packages="${MUTATION_PACKAGES:-./internal/domain/policy ./internal/runtime/flow ./internal/storage/jobs ./internal/storage/jobs/persist ./internal/runtime/jobs/workers}"
report_dir="${MUTATION_REPORT_DIR:-.mutation-reports}"
workers="${MUTATION_WORKERS:-2}"
timeout_duration="${MUTATION_TIMEOUT:-20m}"
threshold_efficacy="${MUTATION_THRESHOLD_EFFICACY:-70}"
threshold_mcover="${MUTATION_THRESHOLD_MCOVER:-20}"
runner_pkg="github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0"
go_flags="${GOFLAGS:-}"
if [[ "$go_flags" != *"-buildvcs="* ]]; then
	go_flags="${go_flags:+$go_flags }-buildvcs=false"
fi

mkdir -p "$report_dir"
export TMPDIR="${TMPDIR:-$repo_root/$report_dir/tmp}"
mkdir -p "$TMPDIR"

if [[ "$mode" != "list" ]]; then
	if ! git diff --quiet || ! git diff --cached --quiet; then
		echo "mutation runs require a clean tracked worktree; commit or stash changes first" >&2
		exit 2
	fi
fi

timeout_bin=""
if command -v timeout >/dev/null 2>&1; then
	timeout_bin="$(command -v timeout)"
elif command -v gtimeout >/dev/null 2>&1; then
	timeout_bin="$(command -v gtimeout)"
elif [[ "$mode" != "list" ]]; then
	echo "GNU timeout is required for mutation runs. Install coreutils or put timeout/gtimeout on PATH." >&2
	exit 127
fi

summary="$report_dir/summary.md"
{
	printf '# Critical Mutation Report\n\n'
	printf -- '- Mode: `%s`\n' "$mode"
	printf -- '- Started: `%s`\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	printf -- '- Report directory: `%s`\n' "$report_dir"
	printf -- '- Temp directory: `%s`\n\n' "$TMPDIR"
	printf '## Targets\n\n'
} > "$summary"

if command -v gremlins >/dev/null 2>&1; then
	runner=(gremlins)
else
	if ! "$go_bin" version >/dev/null 2>&1; then
		echo "Go is required to run mutation testing when gremlins is not on PATH: $go_bin" >&2
		exit 127
	fi
	runner=("$go_bin" run "$runner_pkg")
fi

failures=0
for pkg in $mutation_packages; do
	safe="${pkg#./}"
	safe="${safe//\//_}"
	safe="${safe//[^[:alnum:]_.-]/_}"
	json_out="$report_dir/${safe}.json"
	stdout_out="$report_dir/${safe}.log"

	args=(unleash "$pkg" --workers "$workers" --output "$json_out")
	if [[ "$mode" == "dry-run" ]]; then
		args+=(--dry-run)
	fi
	if [[ "$mode" == "run" ]]; then
		args+=(--threshold-efficacy "$threshold_efficacy" --threshold-mcover "$threshold_mcover")
	fi
	if [[ -n "${MUTATION_TAGS:-}" ]]; then
		args+=(--tags "$MUTATION_TAGS")
	fi
	if [[ -n "${MUTATION_COVERPKG:-}" ]]; then
		args+=(--coverpkg "$MUTATION_COVERPKG")
	fi

	printf -- '- `%s`\n' "$pkg" >> "$summary"

	if [[ "$mode" == "list" ]]; then
		printf 'would run: TMPDIR=%q timeout %q' "$TMPDIR" "$timeout_duration"
		printf ' %q' "${runner[@]}" "${args[@]}"
		printf '\n'
		continue
	fi

	printf 'Running mutation %s for %s\n' "$mode" "$pkg"
	if "$timeout_bin" "$timeout_duration" env GOFLAGS="$go_flags" TMPDIR="$TMPDIR" "${runner[@]}" "${args[@]}" >"$stdout_out" 2>&1; then
		printf '  - status: passed\n' >> "$summary"
	else
		status=$?
		failures=$((failures + 1))
		printf '  - status: failed (%d)\n' "$status" >> "$summary"
		printf '  - log: `%s`\n' "$stdout_out" >> "$summary"
		printf 'Mutation %s failed for %s; see %s\n' "$mode" "$pkg" "$stdout_out" >&2
	fi
	if ! git diff --quiet || ! git diff --cached --quiet; then
		printf 'mutation runner left tracked worktree changes after %s; aborting\n' "$pkg" >&2
		git diff --stat >&2 || true
		exit 1
	fi
done

{
	printf '\n## Notes\n\n'
	if [[ "$mode" == "list" ]]; then
		printf 'List mode does not execute mutation analysis. Use `make mutation-critical-dry-run` to discover covered mutants, or `make mutation-critical` for the full opt-in gate.\n'
	else
		printf 'Review surviving mutants in the per-package JSON and log files before raising thresholds.\n'
	fi
} >> "$summary"

printf 'Wrote %s\n' "$summary"
if [[ "$failures" -gt 0 ]]; then
	exit 1
fi
