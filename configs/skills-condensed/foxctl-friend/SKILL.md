---
name: foxctl Friend
description: Enhanced friend command that uses foxctl for structured context before consulting Gemini
allowed-tools: Bash(foxctl:*), Bash(gemini:*), Read
argument-hint: "<query>"
---

# Gemini Second Opinion

Get Gemini's perspective using foxctl-gathered context.

## Workflow

1. Gather context via foxctl:
   - `code/complexity` - Hotspots
   - `code/symbols` - Structure
   - `code/smart_search` - Relevant snippets

2. Log query: `foxctl mailbox send gemini-queries --from claude-agent --type agent.ask`

3. Send to Gemini: `echo "$CONTEXT" | gemini -p "$ARGUMENTS"`

4. Log response: `foxctl mailbox send claude-queries --from gemini-agent --type agent.reply`

5. Present: foxctl findings + Gemini analysis + Claude synthesis

Full docs: `~/.foxctl/share/configs/skills/foxctl-friend/Skill.md`
