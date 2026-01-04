#!/bin/bash
# Benchmark ALL Claude Code hooks to identify slow hooks
# Usage: ./scripts/benchmark-hooks.sh [iterations] [category]
# Categories: all, pre, post, stop, session, compact, prompt

set -eo pipefail

ITERATIONS=${1:-3}
CATEGORY=${2:-all}
HOOKS_DIR="${CLAUDE_PROJECT_DIR:-$(pwd)}/configs/hooks"
RESULTS_FILE="/tmp/hook-benchmark-$(date +%Y%m%d-%H%M%S).txt"

# Sample inputs for different hook types
PRETOOL_INPUT='{"tool_input":{"file_path":"README.md","pattern":"*.go","command":"ls"}}'
POSTTOOL_INPUT='{"tool_input":{"file_path":"README.md"},"tool_output":{"content":"file content"}}'
SESSION_INPUT='{"session_id":"test-session","type":"startup"}'
STOP_INPUT='{"reason":"user_interrupt"}'
COMPACT_INPUT='{"type":"auto"}'
PROMPT_INPUT='{"prompt":"test prompt"}'

# Hook categories with their hooks and sample inputs
declare -a PRE_HOOKS=(
    "overseer-inbox"
    "session-identity"
    "file-memory-recall"
    "task-guard"
    "security-scanner"
    "smart-read"
    "bash-advisor"
    "semantic-search"
    "smart-grep"
    "smart-find"
    "todo-advisor"
)

declare -a POST_HOOKS=(
    "overseer-inbox-post"
    "read-context-suggestions"
    "todo-sync"
    "memory-prompt"
    "lsp-diagnostics"
    "test-feedback"
    "complexity-warning"
    "impact-analysis"
    "live-index"
    "memory-capture"
    "memory-embed"
    "task-file-link"
)

declare -a STOP_HOOKS=(
    "plan-sync"
    "embedding-flush"
    "session-capture"
    "graph-cleanup"
    "graph-pagerank"
)

declare -a SESSION_HOOKS=(
    "session-identity"
    "session-restore"
)

declare -a COMPACT_HOOKS=(
    "session-save"
)

declare -a PROMPT_HOOKS=(
    "memory-detector"
)

# Benchmark a single hook
benchmark_hook() {
    local hook_name=$1
    local sample_input=$2
    local hook_file="$HOOKS_DIR/${hook_name}.sh"

    if [[ ! -x "$hook_file" ]]; then
        printf "%-28s %8s %8s %8s %8s\n" "$hook_name" "-" "-" "-" "MISSING"
        return
    fi

    local times=()
    local status="OK"

    for ((i=1; i<=ITERATIONS; i++)); do
        local start end elapsed
        start=$(gdate +%s.%N 2>/dev/null || python3 -c 'import time; print(time.time())')

        # Run hook with sample input, capture exit code
        # Use printf to safely handle special characters in JSON input
        if timeout 15s bash -c 'printf "%s" "$1" | "$2"' -- "$sample_input" "$hook_file" >/dev/null 2>&1; then
            end=$(gdate +%s.%N 2>/dev/null || python3 -c 'import time; print(time.time())')
            elapsed=$(echo "$end - $start" | bc)
            times+=("$elapsed")
        else
            status="FAIL"
            break
        fi
    done

    if [[ ${#times[@]} -gt 0 ]]; then
        # Calculate min, max, avg
        local min max sum avg
        min=$(printf '%s\n' "${times[@]}" | sort -n | head -1)
        max=$(printf '%s\n' "${times[@]}" | sort -n | tail -1)
        sum=0
        for t in "${times[@]}"; do
            sum=$(echo "$sum + $t" | bc)
        done
        avg=$(echo "scale=3; $sum / ${#times[@]}" | bc)

        # Flag slow hooks
        if (( $(echo "$avg > 1.0" | bc -l) )); then
            status="CRITICAL"
        elif (( $(echo "$avg > 0.5" | bc -l) )); then
            status="SLOW"
        fi

        printf "%-28s %7.3fs %7.3fs %7.3fs %8s\n" "$hook_name" "$min" "$max" "$avg" "$status"
    else
        printf "%-28s %8s %8s %8s %8s\n" "$hook_name" "-" "-" "-" "$status"
    fi
}

# Run benchmarks for a category
run_category() {
    local category_name=$1
    local sample_input=$2
    shift 2
    local hooks=("$@")

    echo ""
    echo "=== $category_name ==="
    printf "%-28s %8s %8s %8s %8s\n" "HOOK" "MIN" "MAX" "AVG" "STATUS"
    printf "%-28s %8s %8s %8s %8s\n" "----" "---" "---" "---" "------"

    for hook_name in "${hooks[@]}"; do
        benchmark_hook "$hook_name" "$sample_input"
    done
}

echo "Hook Benchmark - $ITERATIONS iterations each"
echo "Category: $CATEGORY"
echo "============================================="

{
    if [[ "$CATEGORY" == "all" || "$CATEGORY" == "pre" ]]; then
        run_category "PreToolUse Hooks" "$PRETOOL_INPUT" "${PRE_HOOKS[@]}"
    fi

    if [[ "$CATEGORY" == "all" || "$CATEGORY" == "post" ]]; then
        run_category "PostToolUse Hooks" "$POSTTOOL_INPUT" "${POST_HOOKS[@]}"
    fi

    if [[ "$CATEGORY" == "all" || "$CATEGORY" == "stop" ]]; then
        run_category "Stop Hooks" "$STOP_INPUT" "${STOP_HOOKS[@]}"
    fi

    if [[ "$CATEGORY" == "all" || "$CATEGORY" == "session" ]]; then
        run_category "SessionStart Hooks" "$SESSION_INPUT" "${SESSION_HOOKS[@]}"
    fi

    if [[ "$CATEGORY" == "all" || "$CATEGORY" == "compact" ]]; then
        run_category "PreCompact Hooks" "$COMPACT_INPUT" "${COMPACT_HOOKS[@]}"
    fi

    if [[ "$CATEGORY" == "all" || "$CATEGORY" == "prompt" ]]; then
        run_category "UserPromptSubmit Hooks" "$PROMPT_INPUT" "${PROMPT_HOOKS[@]}"
    fi
} | tee "$RESULTS_FILE"

echo ""
echo "Results saved to: $RESULTS_FILE"
echo ""
echo "Legend:"
echo "  OK       - < 500ms (acceptable)"
echo "  SLOW     - 500ms-1s (consider optimization)"
echo "  CRITICAL - > 1s (needs immediate attention)"
echo "  FAIL     - Hook errored or timed out"
echo "  MISSING  - Hook script not found"
echo ""
echo "PreToolUse Rules:"
echo "  - Can only approve/reject tool execution"
echo "  - Context only shown to Claude on error/rejection"
echo "  - Must be fast - blocks tool execution"
echo ""
echo "PostToolUse Rules:"
echo "  - Can provide context to Claude after tool runs"
echo "  - Non-blocking - runs after tool completes"
echo "  - Better place for slow operations"
