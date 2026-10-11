#!/bin/bash
set -euo pipefail

cd /Users/joshka/repos/worktrees/feat-rlm-general-solver
set -a
source /Users/joshka/repos/personal/foxctl/.env
set +a

echo "=== Starting V7 probes ==="
date

for spec in \
  "logic:/tmp/foxctl-longcot-probe-logic-easy-1.jsonl" \
  "cs:/tmp/foxctl-longcot-probe-cs-easy-1.jsonl" \
  "chemistry:/tmp/foxctl-longcot-probe-chemistry-easy-1.jsonl" \
  "chess:/tmp/foxctl-longcot-probe-chess-easy-1.jsonl" \
  "math:/tmp/foxctl-longcot-probe-math-easy-1.jsonl"
do
  DOMAIN="${spec%%:*}"
  DATASET="${spec#*:}"
  echo ""
  echo "=== PROBE: $DOMAIN ==="
  date
  bin/foxctl eval longcot \
    --dataset "$DATASET" \
    --longcot-repo /Users/joshka/repos/githubs/LongCoT \
    --condition rlm_braid_single \
    --provider openrouter \
    --model qwen/qwen3.6-flash \
    --timeout 12m \
    --max-tokens 8192 \
    --max-iterations 64 \
    --sandbox python \
    --helper-language python \
    --helper-attempts 5 \
    --save \
    --output-dir ".foxctl/longcot-autoresearch/general-solver-probe-v7-${DOMAIN}-easy-1" \
    --verify \
    --format markdown \
    --general-helper \
    --blocksworld-helper=false \
    --rlm-final-from-verified-handoff || echo "=== PROBE $DOMAIN FAILED ==="
done

echo ""
echo "=== All V7 probes complete ==="
date
