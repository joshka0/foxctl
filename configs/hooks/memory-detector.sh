#!/usr/bin/env bash
# memory-detector.sh - Detect memory save/recall/todo patterns
#
# UserPromptSubmit hook that looks for:
#
# SAVE patterns:
# - "remember this", "note that", "important:"
# - "gotcha:", "learned:", "decision:"
# - standalone "remember" (capture as memory)
# - "the trick is", "the key is", "turns out"
# - "watch out for", "be careful", "keep in mind"
# - "TIL", "today I learned", "pro tip", "tip:"
# - "for future reference"
# - "don't forget", "please don't", "please do not"
#
# TODO patterns:
# - "let's make sure we...", "let's not forget to..."
# - "make sure we...", "we need to...", "we should..."
# - "TODO:", "FIXME:", "HACK:"
# - "follow up on", "action item", "next step"
# - "before we..."
#
# RECALL patterns:
# - "how did we", "how did i", "where did we"
# - "what was the", "when did we"
# - "didn't we already", "like we did before"
# - "as we discussed", "previously", "earlier we"
# - "last time we", "remember when we"
#
# Suggests appropriate agentctl commands.

set -euo pipefail

# Read hook input
payload="$(cat)"

# Extract user prompt
prompt=$(printf '%s' "$payload" | jq -r '.prompt // ""' | tr '[:upper:]' '[:lower:]')

# Check for RECALL patterns first (more specific)
recall_hint=""
if [[ "$prompt" =~ (how\ did\ (we|i)|where\ did\ (we|i)|what\ was\ the|when\ did\ (we|i)|do\ you\ remember) ]] \
|| [[ "$prompt" =~ (didn.?t\ (we|i)\ already|like\ (we|i)\ did\ before|as\ we\ discussed) ]] \
|| [[ "$prompt" =~ (previously|earlier\ (we|i)|last\ time\ (we|i)|remember\ when\ (we|i)) ]] \
|| [[ "$prompt" =~ (similar\ to\ (before|what\ we)|we\ (already|once)\ (did|had|tried)) ]]; then
  recall_hint="**Recall hint:** Try these to find past context:
- \`agentctl memory get --query \"<keywords>\"\` - search memories
- \`agentctl run code/semantic_search --input '{\"query\": \"<question>\"}'\` - semantic code search
- \`agentctl run session/recall --input '{\"query\": \"<question>\"}'\` - search past sessions"
fi

# If recall pattern detected, return early with hint
if [[ -n "$recall_hint" ]]; then
  jq -nc --arg hint "$recall_hint" '{
    decision: "approve",
    context: $hint
  }'
  exit 0
fi

# Check for TODO patterns
todo_hint=""
if [[ "$prompt" =~ (let.?s\ make\ sure\ (we|to)|let.?s\ not\ forget\ to|make\ sure\ (we|to)|we\ need\ to\ make\ sure|don.?t\ forget\ to|we\ should\ make\ sure) ]] \
|| [[ "$prompt" =~ (we\ need\ to|we\ should|we\ must|we\ have\ to) ]] \
|| [[ "$prompt" =~ (todo:|fixme:|hack:|xxx:) ]] \
|| [[ "$prompt" =~ (follow\ up\ on|action\ item|next\ step) ]] \
|| [[ "$prompt" =~ (before\ we\ ) ]]; then
  todo_hint="**Todo hint:** Capture this as a task:
- \`bin/agentctl todo add --title \"<task>\" --description \"<details>\"\`
- Or use TodoWrite tool to track this"
fi

# If todo pattern detected, return with hint
if [[ -n "$todo_hint" ]]; then
  jq -nc --arg hint "$todo_hint" '{
    decision: "approve",
    context: $hint
  }'
  exit 0
fi

# Check for SAVE patterns
memory_type=""
if [[ "$prompt" =~ (remember|note|save).*(this|that) ]]; then
  memory_type="context"
elif [[ "$prompt" =~ (don.?t\ forget|do\ not\ forget|please\ don.?t|please\ do\ not) ]]; then
  memory_type="context"
elif [[ "$prompt" =~ ^(gotcha|learned|learning|tricky): ]]; then
  memory_type="gotcha"
elif [[ "$prompt" =~ ^(decision|decided|choosing): ]]; then
  memory_type="decision"
elif [[ "$prompt" =~ ^(important|note|remember): ]]; then
  memory_type="context"
elif [[ "$prompt" =~ ^(pattern|approach|solution): ]]; then
  memory_type="pattern"
elif [[ "$prompt" =~ (the\ trick\ is|the\ key\ is|turns\ out) ]]; then
  memory_type="gotcha"
elif [[ "$prompt" =~ (watch\ out\ for|be\ careful|keep\ in\ mind) ]]; then
  memory_type="gotcha"
elif [[ "$prompt" =~ (til:|today\ i\ learned|pro\ tip|tip:) ]]; then
  memory_type="gotcha"
elif [[ "$prompt" =~ (for\ future\ reference) ]]; then
  memory_type="context"
elif [[ "$prompt" =~ (^|[[:space:]])remember([[:space:]]|$) ]]; then
  # Standalone "remember" - prompt to capture as memory
  memory_type="context"
fi

# If memory phrase detected, add context hint for dual-save
if [[ -n "$memory_type" ]]; then
  jq -nc --arg type "$memory_type" '{
    decision: "approve",
    context: ("**Memory hint:** User wants to save a " + $type + ". Use `/remember` skill to:\n1. Store to agentctl memory\n2. Append to CLAUDE.md under Gotchas section")
  }'
else
  echo '{"decision":"approve"}'
fi
