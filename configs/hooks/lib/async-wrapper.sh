#!/usr/bin/env bash
# async-wrapper.sh - Run a hook script in the background without blocking
#
# Usage in other hooks:
#   source "${BASH_SOURCE%/*}/lib/async-wrapper.sh"
#   run_async "$0.impl" "$@"
#
# Or wrap a command:
#   run_async_cmd agentctl run graph/manage --input "$input"
#
# The wrapper:
#   1. Immediately returns {} to unblock Claude
#   2. Spawns the actual work in background
#   3. Logs to ~/.agentctl/logs/hooks/ for debugging

ASYNC_LOG_DIR="${HOME}/.agentctl/logs/hooks"
mkdir -p "$ASYNC_LOG_DIR" 2>/dev/null || true

# Run a script file in background
run_async() {
    local script="$1"
    shift
    local log_file
    log_file="$ASYNC_LOG_DIR/$(basename "$script" .sh)-$(date +%Y%m%d-%H%M%S).log"

    # Spawn in background, detached from terminal
    nohup bash "$script" "$@" > "$log_file" 2>&1 &
    disown

    # Return immediately
    echo '{}'
}

# Run a command in background
run_async_cmd() {
    local log_file
    log_file="$ASYNC_LOG_DIR/async-$(date +%Y%m%d-%H%M%S).log"

    # Spawn in background
    nohup "$@" > "$log_file" 2>&1 &
    disown

    # Return immediately
    echo '{}'
}

# Run a command in background with stdin piped
run_async_with_input() {
    local input="$1"
    shift
    local log_file
    log_file="$ASYNC_LOG_DIR/async-$(date +%Y%m%d-%H%M%S).log"

    # Spawn in background with input
    echo "$input" | nohup "$@" > "$log_file" 2>&1 &
    disown

    # Return immediately
    echo '{}'
}
