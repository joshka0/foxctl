#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STAMP="${BENCH_STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${BENCH_OUT_DIR:-/private/tmp/foxctl-benchmarks}"
PRICE="${INPUT_TOKEN_PRICE_PER_MILLION_USD:-1}"
BENCH_COUNT="${BENCH_COUNT:-3}"
BENCH_TIME="${BENCH_TIME:-1s}"
RUN_GATHER_CONTEXT="${RUN_GATHER_CONTEXT:-0}"
GATHER_DATASET="${GATHER_DATASET:-testdata/evals/gather-context/foxctl-repo-grounded.jsonl}"
GATHER_CASE_LIMIT="${GATHER_CASE_LIMIT:-2}"
FOXCTL_BIN="${FOXCTL_BIN:-$ROOT/bin/foxctl}"

mkdir -p "$OUT_DIR"

shell_json="$OUT_DIR/orientation-shell-$STAMP.json"
shell_envelope="$OUT_DIR/orientation-shell-$STAMP.envelope.json"
shell_bench="$OUT_DIR/orientation-shellreduce-$STAMP.txt"
tool_json="$OUT_DIR/orientation-tools-$STAMP.json"
gather_json="$OUT_DIR/orientation-gather-context-$STAMP.json"
gather_envelope="$OUT_DIR/orientation-gather-context-$STAMP.envelope.json"
summary_md="$OUT_DIR/orientation-summary-$STAMP.md"
tool_tmp_dir="$OUT_DIR/orientation-tools-$STAMP.tmp"

rm -f "$shell_json" "$shell_envelope" "$shell_bench" "$tool_json" "$gather_json" "$gather_envelope" "$summary_md"
rm -rf "$tool_tmp_dir"
mkdir -p "$tool_tmp_dir"

orientation_commands=(
  "ls -la internal"
  "grep -rn 'func ' internal/tooling/shellreduce"
  "sed -n '1,120p' cmd/foxctl/cmd/shell.go"
  "git status --short"
  "git diff --stat"
  "git log --stat -5"
)

if [[ -x "$FOXCTL_BIN" ]]; then
  foxctl_cmd=("$FOXCTL_BIN")
else
  foxctl_cmd=(go run ./cmd/foxctl)
fi

now_ms() {
  perl -MTime::HiRes=time -e 'printf "%.0f\n", time() * 1000'
}

run_tool_case() {
  local case_id="$1"
  local tool="$2"
  local mode="$3"
  local command_label="$4"
  local max_entries="$5"
  local max_matches="$6"
  local max_blocks="$7"
  local out_file="$8"
  local err_file="$9"
  shift 9

  local start_ms end_ms duration_ms status bytes stderr_bytes tokens
  start_ms="$(now_ms)"
  if (
    cd "$ROOT"
    "$@"
  ) >"$out_file" 2>"$err_file"; then
    status=0
  else
    status=$?
  fi
  end_ms="$(now_ms)"
  duration_ms=$((end_ms - start_ms))
  bytes="$(wc -c <"$out_file" | tr -d ' ')"
  stderr_bytes="$(wc -c <"$err_file" | tr -d ' ')"
  tokens=$(((bytes + 3) / 4))

  jq -n \
    --arg case_id "$case_id" \
    --arg tool "$tool" \
    --arg mode "$mode" \
    --arg command "$command_label" \
    --argjson status "$status" \
    --argjson duration_ms "$duration_ms" \
    --argjson bytes "$bytes" \
    --argjson stderr_bytes "$stderr_bytes" \
    --argjson tokens "$tokens" \
    --argjson price "$PRICE" \
    --arg max_entries "$max_entries" \
    --arg max_matches "$max_matches" \
    --arg max_blocks "$max_blocks" \
    '{
      case_id: $case_id,
      tool: $tool,
      mode: $mode,
      command: $command,
      status: $status,
      duration_ms: $duration_ms,
      bytes: $bytes,
      stderr_bytes: $stderr_bytes,
      estimated_tokens: $tokens,
      estimated_input_cost_usd: (($tokens * $price) / 1000000),
      budget: {
        token_estimator: "ceil(stdout_bytes/4)",
        input_token_price_per_million_usd: $price,
        max_entries: (if $max_entries == "" then null else ($max_entries | tonumber) end),
        max_matches: (if $max_matches == "" then null else ($max_matches | tonumber) end),
        max_blocks: (if $max_blocks == "" then null else ($max_blocks | tonumber) end)
      }
    }' >>"$tool_tmp_dir/cases.jsonl"
}

run_tool_case \
  "fs_ls_internal" "fs/ls" "native" "ls -la internal" "" "" "" \
  "$tool_tmp_dir/fs-ls-native.out" "$tool_tmp_dir/fs-ls-native.err" \
  ls -la internal
run_tool_case \
  "fs_ls_internal" "fs/ls" "foxctl" "foxctl run fs/ls --ephemeral --input '{\"path\":\"internal\",\"max_entries\":200}'" "200" "" "" \
  "$tool_tmp_dir/fs-ls-foxctl.out" "$tool_tmp_dir/fs-ls-foxctl.err" \
  "${foxctl_cmd[@]}" run fs/ls --ephemeral --input '{"path":"internal","max_entries":200}'
run_tool_case \
  "text_ripgrep_functions" "text/ripgrep" "native" "rg -n --max-count 20 'func ' internal/tooling/shellreduce" "" "20" "" \
  "$tool_tmp_dir/text-ripgrep-native.out" "$tool_tmp_dir/text-ripgrep-native.err" \
  rg -n --max-count 20 "func " internal/tooling/shellreduce
run_tool_case \
  "text_ripgrep_functions" "text/ripgrep" "foxctl" "foxctl run text/ripgrep --ephemeral --input '{\"pattern\":\"func \",\"path\":\"internal/tooling/shellreduce\",\"max_matches\":20}'" "" "20" "" \
  "$tool_tmp_dir/text-ripgrep-foxctl.out" "$tool_tmp_dir/text-ripgrep-foxctl.err" \
  "${foxctl_cmd[@]}" run text/ripgrep --ephemeral --input '{"pattern":"func ","path":"internal/tooling/shellreduce","max_matches":20}'
run_tool_case \
  "code_context_grep_functions" "code/context_grep" "native" "rg -n --max-count 20 'func ' internal/tooling/shellreduce" "" "20" "" \
  "$tool_tmp_dir/context-grep-native.out" "$tool_tmp_dir/context-grep-native.err" \
  rg -n --max-count 20 "func " internal/tooling/shellreduce
run_tool_case \
  "code_context_grep_functions" "code/context_grep" "foxctl" "foxctl run code/context_grep --ephemeral --input '{\"pattern\":\"func \",\"path\":\"internal/tooling/shellreduce\",\"max_matches\":20,\"max_blocks\":20,\"inline_mode\":\"preview\"}'" "" "20" "20" \
  "$tool_tmp_dir/context-grep-foxctl.out" "$tool_tmp_dir/context-grep-foxctl.err" \
  "${foxctl_cmd[@]}" run code/context_grep --ephemeral --input '{"pattern":"func ","path":"internal/tooling/shellreduce","max_matches":20,"max_blocks":20,"inline_mode":"preview"}'

jq -s \
  --arg generated "$STAMP" \
  --arg workspace "$ROOT" \
  --argjson price "$PRICE" \
  '{
    generated: $generated,
    workspace: $workspace,
    budget: {
      token_estimator: "ceil(stdout_bytes/4)",
      input_token_price_per_million_usd: $price
    },
    summary: {
      case_count: length,
      native_duration_ms: (map(select(.mode == "native").duration_ms) | add // 0),
      foxctl_duration_ms: (map(select(.mode == "foxctl").duration_ms) | add // 0),
      native_tokens: (map(select(.mode == "native").estimated_tokens) | add // 0),
      foxctl_tokens: (map(select(.mode == "foxctl").estimated_tokens) | add // 0),
      native_cost_usd: (map(select(.mode == "native").estimated_input_cost_usd) | add // 0),
      foxctl_cost_usd: (map(select(.mode == "foxctl").estimated_input_cost_usd) | add // 0),
      failed_cases: (map(select(.status != 0)) | length)
    },
    cases: .
  }' "$tool_tmp_dir/cases.jsonl" >"$tool_json"

shell_args=(
  shell report
  --workspace "$ROOT"
  --save-file "$shell_json"
  --input-token-price-per-million-usd "$PRICE"
)
for command in "${orientation_commands[@]}"; do
  shell_args+=(--command "$command")
done

(
  cd "$ROOT"
  go run ./cmd/foxctl "${shell_args[@]}"
) >"$shell_envelope"

(
  cd "$ROOT"
  BENCH_PATTERN=BenchmarkShellReduce \
    BENCH_COUNT="$BENCH_COUNT" \
    BENCH_TIME="$BENCH_TIME" \
    BENCH_OUT="$shell_bench" \
    ./scripts/run-go-benchmarks.sh ./internal/tooling/shellreduce
) >/dev/null

if [[ "$RUN_GATHER_CONTEXT" == "1" ]]; then
  (
    cd "$ROOT"
    go run ./cmd/foxctl eval gather-context \
      --workspace "$ROOT" \
      --eval-dataset-file "$GATHER_DATASET" \
      --case-limit "$GATHER_CASE_LIMIT" \
      --lane code \
      --tool-profile gather-context \
      --limit 8 \
      --max-context-chars 6000 \
      --report-file "$gather_json"
  ) >"$gather_envelope"
fi

shell_summary="$(jq -r '
  .summary
  | "- Shell orientation cases: \(.case_count)\n" +
    "- Raw tokens: \(.total_raw_tokens)\n" +
    "- Reduced tokens: \(.total_reduced_tokens)\n" +
    "- Token savings: \(.total_tokens_saved_percent | tonumber | .*10 | round/10)%\n" +
    "- Raw bytes: \(.total_raw_bytes)\n" +
    "- Reduced bytes: \(.total_reduced_bytes)\n" +
    "- Byte savings: \(.total_bytes_saved_percent | tonumber | .*10 | round/10)%\n" +
    "- Estimated input-cost savings: $\(.total_estimated_input_cost_saved_usd)\n" +
    "- Cold raw duration: \(.total_raw_duration_ms)ms\n" +
    "- Cold reduced duration: \(.total_reduced_duration_ms)ms"
' "$shell_json")"

shell_rows="$(jq -r '
  .cases[]
  | "| `" + (.operation // "") + "` | `" + ((.command // "") | gsub("\\|"; "\\|")) + "` | " +
    ((.raw_tokens // 0) | tostring) + " | " +
    ((.reduced_tokens // 0) | tostring) + " | " +
    (((.tokens_saved_percent // 0) | tonumber | .*10 | round/10) | tostring) + "% | " +
    ((.raw_duration_ms // 0) | tostring) + " | " +
    ((.reduced_duration_ms // 0) | tostring) + " | " +
    ((.duration_saved_ms // 0) | tostring) + " |"
' "$shell_json")"

hot_rows="$(awk '
  /^BenchmarkShellReduceRouteTypicalCommands/ {
    printf "- route run %d: %s ns/op, %s B/op, %s allocs/op\n", ++route, $3, $5, $7
  }
  /^BenchmarkShellReduceSummarizeGitAndGrep/ {
    printf "- summary run %d: %s ns/op, %s B/op, %s allocs/op\n", ++summary, $3, $5, $7
  }
' "$shell_bench")"

tool_summary="$(jq -r '
  .summary
  | "- Tool skill cases: \(.case_count)\n" +
    "- Native cold duration: \(.native_duration_ms)ms\n" +
    "- Foxctl cold duration: \(.foxctl_duration_ms)ms\n" +
    "- Native output tokens: \(.native_tokens)\n" +
    "- Foxctl output tokens: \(.foxctl_tokens)\n" +
    "- Native estimated input cost: $\(.native_cost_usd)\n" +
    "- Foxctl estimated input cost: $\(.foxctl_cost_usd)\n" +
    "- Failed cases: \(.failed_cases)"
' "$tool_json")"

tool_rows="$(jq -r '
  .cases[]
  | "| `" + .tool + "` | " + .mode + " | `" + (.command | gsub("\\|"; "\\|")) + "` | " +
    (.duration_ms | tostring) + " | " +
    (.bytes | tostring) + " | " +
    (.estimated_tokens | tostring) + " | " +
    ((.budget.max_entries // "-") | tostring) + " | " +
    ((.budget.max_matches // "-") | tostring) + " | " +
    ((.budget.max_blocks // "-") | tostring) + " | " +
    (.status | tostring) + " |"
' "$tool_json")"

gather_section="Not run. Set RUN_GATHER_CONTEXT=1 to include the offline gather_context eval."
gather_artifacts="- gather_context report JSON: not generated
- gather_context envelope JSON: not generated"
if [[ -f "$gather_json" ]]; then
  gather_artifacts="- gather_context report JSON: \`$gather_json\`
- gather_context envelope JSON: \`$gather_envelope\`"
  gather_section="$(jq -r '
    .summary
    | "- gather_context cases: \(.count)\n" +
      "- Pass rate: \(.pass_rate | tonumber | .*1000 | round/10)%\n" +
      "- Mean path recall: \(.mean_path_recall | tonumber | .*1000 | round/10)%\n" +
      "- Mean fact recall: \(.mean_fact_recall | tonumber | .*1000 | round/10)%\n" +
      "- Mean duration: \(.mean_duration_ms | tonumber | round)ms\n" +
      "- Mean emitted context chars: \(.mean_emitted_context_chars | tonumber | round)"
  ' "$gather_json")"
fi

cat >"$summary_md" <<EOF
# Foxctl Orientation Benchmark

Generated: $STAMP

Workspace: \`$ROOT\`

## Artifacts

- Shell report JSON: \`$shell_json\`
- Shell envelope JSON: \`$shell_envelope\`
- Hot shell reducer benchmark: \`$shell_bench\`
- Tool skill cold-run JSON: \`$tool_json\`
$gather_artifacts

## Tool Skill Cold Runs

$tool_summary

| Tool | Mode | Command | Duration ms | Bytes | Tokens | Max Entries | Max Matches | Max Blocks | Status |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
$tool_rows

Interpretation: these rows compare native shell output against cold foxctl skill
invocations with explicit budget controls. They should be used for "actual
tool" context and cost evidence, not as hot-path runtime latency claims.

## Shell Output Reduction

$shell_summary

| Operation | Command | Raw Tokens | Reduced Tokens | Token Savings | Raw ms | Reduced ms | Delta ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
$shell_rows

Interpretation: use these numbers for context-size and input-cost claims. Do not
claim cold structured-shell invocation is faster than native shell unless the
per-row duration proves it. The hot reducer benchmark below measures the
in-process routing and summarization cost separately from CLI/skill startup.

## Hot Reducer Cost

$hot_rows

## gather_context Offline Eval

$gather_section

## Native Subagent Baseline

Native explorer/subagent comparisons should be recorded through
\`foxctl eval gather-context --agent-baseline-results <jsonl>\` or
\`foxctl eval agents --external-results <jsonl>\` so duration, path recall,
tokens, and cost live beside the foxctl harness numbers. See
\`docs/general/code-search-evals.md#native-subagent-baselines\` for the current
transcript-token accounting contract.
EOF

printf 'orientation_summary=%s\n' "$summary_md"
printf 'shell_report=%s\n' "$shell_json"
printf 'shell_benchmark=%s\n' "$shell_bench"
printf 'tool_report=%s\n' "$tool_json"
if [[ -f "$gather_json" ]]; then
  printf 'gather_context_report=%s\n' "$gather_json"
fi
