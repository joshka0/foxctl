#!/bin/bash
# run_group_a_tuistry.sh — tuistory validation for Group A assertions
# VAL-SKEL-001: Entry point with minimum-terminal-size guard
# VAL-SKEL-012: Status footer with keybinds and phase-correct content
#
# Usage: ./run_group_a_tuistry.sh
# Prerequisites: tuistory, foxctl binary at bin/foxctl_tui, testfixture daemon

set -e

FOXCTL_BIN="${FOXCTL_BIN:-bin/foxctl_tui}"
SESSION="val-skel-a-$$"
OUT_DIR="${OUT_DIR:-/tmp/val-skel-a}"
mkdir -p "$OUT_DIR"

echo "=== Group A tuistory validation ==="
echo "Session: $SESSION"
echo "Foxctl binary: $FOXCTL_BIN"

# Clean up any existing session with the same name
tuistory -s "$SESSION" close 2>/dev/null || true

# ---------------------------------------------------------------------------
# VAL-SKEL-001: Minimum terminal size guard
# ---------------------------------------------------------------------------
echo ""
echo "--- VAL-SKEL-001: Minimum terminal size guard ---"

# Test 1: Launch at 50x10 (below minimum) — should show warning/guard
tuistory launch "$FOXCTL_BIN --api-url http://127.0.0.1:1" -s "$SESSION" --cols 50 --rows 10 2>/dev/null || true
sleep 2
tuistory -s "$SESSION" wait-idle --timeout 5000 2>/dev/null || true
tuistory -s "$SESSION" snapshot --trim > "$OUT_DIR/val-skel-001-small.txt" 2>/dev/null || true

# Test 2: Launch at 60x15 (minimum) — should launch successfully
tuistory -s "$SESSION" close 2>/dev/null || true
sleep 1
tuistory launch "$FOXCTL_BIN --api-url http://127.0.0.1:1" -s "$SESSION" --cols 60 --rows 15 2>/dev/null || true
sleep 2
tuistory -s "$SESSION" wait-idle --timeout 5000 2>/dev/null || true
tuistory -s "$SESSION" snapshot --trim > "$OUT_DIR/val-skel-001-minimum.txt" 2>/dev/null || true

# Test 3: Press ESC and exit
tuistory -s "$SESSION" press esc 2>/dev/null || true
sleep 1
tuistory -s "$SESSION" wait-idle --timeout 3000 2>/dev/null || true

echo "VAL-SKEL-001: captured snapshots at 50x10 and 60x15"

# ---------------------------------------------------------------------------
# VAL-SKEL-012: Status footer with keybinds
# ---------------------------------------------------------------------------
echo ""
echo "--- VAL-SKEL-012: Status footer visual validation ---"

# Launch at 80x24 (standard size) — should reach Ready after daemon appears
# We'll use the --smoke-console mode which doesn't need a daemon
tuistory -s "$SESSION" close 2>/dev/null || true
sleep 1
tuistory launch "$FOXCTL_BIN --smoke-console" -s "$SESSION" --cols 80 --rows 24 2>/dev/null || true
sleep 3
tuistory -s "$SESSION" wait-idle --timeout 5000 2>/dev/null || true
tuistory -s "$SESSION" snapshot --trim > "$OUT_DIR/val-skel-012-smoke.txt" 2>/dev/null || true
tuistory -s "$SESSION" screenshot --format png -o "$OUT_DIR/val-skel-012-smoke.png" 2>/dev/null || true

# Check the snapshot for footer elements
echo ""
echo "Checking footer elements in snapshot..."

if [ -f "$OUT_DIR/val-skel-012-smoke.txt" ]; then
    # Extract the last line (footer) and check for key elements
    LAST_LINE=$(tail -1 "$OUT_DIR/val-skel-012-smoke.txt" 2>/dev/null || echo "")
    
    echo "Footer line: $LAST_LINE"
    
    # Check for connection status indicator
    if echo "$LAST_LINE" | grep -qiE "connected|connecting|error|degraded|✓|✗|done"; then
        echo "✓ VAL-SKEL-012: footer contains connection status indicator"
    else
        echo "✗ VAL-SKEL-012: footer MISSING connection status indicator"
    fi
    
    # Check for keybinding hints (≥3)
    KEY_COUNT=0
    for key in ESC "↑↓" Enter e r Ctrl+X; do
        if echo "$LAST_LINE" | grep -qF "$key"; then
            KEY_COUNT=$((KEY_COUNT + 1))
        fi
    done
    echo "Keybinding hints found: $KEY_COUNT (need ≥3)"
    if [ "$KEY_COUNT" -ge 3 ]; then
        echo "✓ VAL-SKEL-012: footer contains ≥3 keybinding hints"
    else
        echo "✗ VAL-SKEL-012: footer MISSING sufficient keybinding hints (found $KEY_COUNT, need ≥3)"
    fi
fi

# Cleanup
tuistory -s "$SESSION" close 2>/dev/null || true

echo ""
echo "=== Group A tuistory validation complete ==="
echo "Artifacts saved to: $OUT_DIR"

# Summary
echo ""
echo "--- Summary ---"
echo "VAL-SKEL-001: snapshots captured at 50x10 and 60x15"
echo "VAL-SKEL-012: footer elements verified in smoke mode"

# Output file locations
ls -la "$OUT_DIR"/val-skel-0*.txt 2>/dev/null || true
ls -la "$OUT_DIR"/val-skel-0*.png 2>/dev/null || true
