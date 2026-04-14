#!/usr/bin/env bash
# skill-advisor.sh - Suggest foxctl skills based on user prompt patterns
#
# UserPromptSubmit hook that detects intent and suggests relevant skills:
#
# CI/PR PATTERNS:
# - "pr comments", "review comments", "what did reviewers say" → ci/prcomments
# - "ci status", "build status", "checks failed" → ci/github_checks
# - "merge conflicts", "can we merge" → ci/prcomments
#
# SEARCH/INVESTIGATE PATTERNS:
# - "search for", "find", "look for", "where is" → code/semantic_search
# - "investigate", "dig into", "explore", "understand" → code/smart_search
# - "query", "semantic search" → code/semantic_search
#
# CODE ANALYSIS PATTERNS:
# - "complexity", "how complex", "cyclomatic" → code/complexity
# - "symbols", "functions", "types", "extract" → code/symbols
# - "imports", "dependencies", "what uses" → code/imports
# - "security", "vulnerabilities", "secrets" → code/security
# - "diff", "compare files", "what changed" → code/diff
#
# GIT PATTERNS:
# - "git status", "uncommitted", "staged" → git/status
# - "recent commits", "blame", "who changed", "hotspots" → code/git
#
# TESTING PATTERNS:
# - "run tests", "test coverage", "failing tests" → test/run
#
# LSP PATTERNS:
# - "definition", "references", "call hierarchy" → lsp/gopls
#
# SESSION PATTERNS:
# - "past sessions", "session history", "what did we discuss" → session/recall

set -euo pipefail

# Read hook input
payload="$(cat)"

# Extract user prompt (lowercase for matching)
prompt=$(printf '%s' "$payload" | jq -r '.prompt // ""' | tr '[:upper:]' '[:lower:]')

# Skip if prompt is too short
if [[ ${#prompt} -lt 5 ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

hint=""

# CI/PR patterns
if [[ "$prompt" =~ (pr\ comment|review\ comment|reviewer|what\ did.*say|feedback\ on\ pr|pr\ feedback) ]]; then
  hint="**Skill hint:** Check PR comments:
\`\`\`bash
foxctl run ci/prcomments --input '{\"pr\": <number>}'
\`\`\`"
elif [[ "$prompt" =~ (ci\ status|build\ status|check.*fail|pipeline|workflow.*fail|github.*action) ]]; then
  hint="**Skill hint:** Check CI status:
\`\`\`bash
foxctl run ci/checks --input '{\"pr\": <number>}'
\`\`\`"
elif [[ "$prompt" =~ (merge\ conflict|can.*merge|mergeable|ready\ to\ merge) ]]; then
  hint="**Skill hint:** Check merge status:
\`\`\`bash
foxctl run ci/prcomments --input '{\"pr\": <number>, \"include_merge_status\": true}'
\`\`\`"

# Search/investigate patterns
elif [[ "$prompt" =~ (semantic.*search|search.*semantic|vector.*search) ]]; then
  hint="**Skill hint:** Semantic code search:
\`\`\`bash
foxctl run code/semantic_search --input '{\"query\": \"<your query>\"}'
\`\`\`"
elif [[ "$prompt" =~ (investigate|dig\ into|explore.*code|understand.*how|figure\ out) ]]; then
  hint="**Skill hint:** Code investigation:
\`\`\`bash
# Smart grep with context
foxctl run code/smart_search --input '{\"query\": \"<pattern>\", \"path\": \".\"}'
# Or semantic search
foxctl run code/semantic_search --input '{\"query\": \"<question>\"}'
\`\`\`"
elif [[ "$prompt" =~ (search\ for|find.*code|look\ for|where\ is|locate) ]] && [[ ! "$prompt" =~ (file|directory) ]]; then
  hint="**Skill hint:** Code search options:
\`\`\`bash
# Semantic (understands meaning)
foxctl run code/semantic_search --input '{\"query\": \"...\"}'
# Pattern-based (ripgrep)
foxctl run text/ripgrep --input '{\"pattern\": \"...\", \"path\": \".\"}'
\`\`\`"

# Code analysis patterns
elif [[ "$prompt" =~ (complexity|cyclomatic|cognitive|how\ complex|nesting) ]]; then
  hint="**Skill hint:** Analyze code complexity:
\`\`\`bash
foxctl run code/complexity --input '{\"path\": \"<file_or_dir>\"}'
\`\`\`"
elif [[ "$prompt" =~ (extract.*symbol|list.*function|what.*types|symbol.*index) ]]; then
  hint="**Skill hint:** Extract code symbols:
\`\`\`bash
foxctl run code/symbols --input '{\"path\": \"<file>\", \"language\": \"go\"}'
\`\`\`"
elif [[ "$prompt" =~ (import|dependenc|what.*uses|used\ by|cycle.*detect) ]]; then
  hint="**Skill hint:** Analyze imports/dependencies:
\`\`\`bash
foxctl run code/imports --input '{\"path\": \"<file>\", \"operation\": \"graph\"}'
\`\`\`"
elif [[ "$prompt" =~ (security|vulnerab|secret|injection|xss|owasp) ]]; then
  hint="**Skill hint:** Security scan:
\`\`\`bash
foxctl run code/security --input '{\"path\": \"<file_or_dir>\"}'
\`\`\`"
elif [[ "$prompt" =~ (diff|compare.*file|what.*changed.*between|side.*by.*side) ]]; then
  hint="**Skill hint:** Compare files/diffs:
\`\`\`bash
foxctl run code/diff --input '{\"file_a\": \"...\", \"file_b\": \"...\"}'
\`\`\`"

# Git patterns
elif [[ "$prompt" =~ (git\ status|uncommitted|staged|unstaged|working.*tree) ]]; then
  hint="**Skill hint:** Git status:
\`\`\`bash
foxctl run git/status --input '{}'
\`\`\`"
elif [[ "$prompt" =~ (recent.*commit|blame|who.*changed|hotspot|churn|co-change) ]]; then
  hint="**Skill hint:** Git analysis:
\`\`\`bash
foxctl run code/git --input '{\"path\": \".\", \"operation\": \"hotspots\"}'
# or: blame, recent_changes, co_changed
\`\`\`"

# Testing patterns
elif [[ "$prompt" =~ (run.*test|test.*coverage|failing.*test|test.*fail) ]]; then
  hint="**Skill hint:** Run tests:
\`\`\`bash
foxctl run test/run --input '{\"path\": \"./...\", \"coverage\": true}'
\`\`\`"

# LSP patterns
elif [[ "$prompt" =~ (go\ to\ definition|find.*reference|call.*hierarch|implementation) ]]; then
  hint="**Skill hint:** LSP operations:
\`\`\`bash
foxctl run lsp/gopls --input '{\"operation\": \"references\", \"file\": \"...\", \"line\": N, \"column\": N}'
\`\`\`"

# Session patterns
elif [[ "$prompt" =~ (past.*session|session.*history|what.*discuss|previous.*conversation) ]]; then
  hint="**Skill hint:** Search past sessions:
\`\`\`bash
foxctl run session/recall --input '{\"query\": \"<topic>\"}'
\`\`\`"

# Task patterns (beyond what todo-advisor handles)
elif [[ "$prompt" =~ (task.*graph|pagerank|critical.*path|task.*priorit) ]]; then
  hint="**Skill hint:** Task graph analysis:
\`\`\`bash
foxctl todo insights                    # Graph insights
foxctl todo recommend                   # AI-prioritized tasks
foxctl todo list --ranked -f table      # PageRank sorted
\`\`\`"

# Codemap patterns
elif [[ "$prompt" =~ (codemap|code.*map|trace.*code|map.*relationship) ]]; then
  hint="**Skill hint:** Generate codemap:
\`\`\`bash
foxctl run codemap/generate --input '{\"entry_point\": \"<file:function>\"}'
foxctl run codemap/check --input '{\"codemap_id\": \"...\"}'  # Check staleness
\`\`\`"

fi

# Return hint if found
if [[ -n "$hint" ]]; then
  jq -nc --arg hint "$hint" '{
    decision: "approve",
    context: $hint
  }'
else
  echo '{"decision":"approve"}'
fi
