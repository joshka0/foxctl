---
name: agentctl Friend
description: Enhanced friend command that uses agentctl for structured context before consulting Gemini
allowed-tools: Bash(agentctl:*), Bash(gemini:*), Read
argument-hint: "<query>"
---

# Gemini Second Opinion

Get Gemini's perspective using agentctl-gathered context.

## Workflow

1. Gather context via agentctl:
   - `code/complexity` - Hotspots
   - `code/symbols` - Structure
   - `code/swe_grep` - Relevant snippets

2. Log query: `agentctl mailbox send gemini-queries --from claude-agent --type agent.ask`

3. Send to Gemini: `echo "$CONTEXT" | gemini -p "$ARGUMENTS"`

4. Log response: `agentctl mailbox send claude-queries --from gemini-agent --type agent.reply`

5. Present: agentctl findings + Gemini analysis + Claude synthesis

Full docs: `~/.agentctl/share/configs/skills/agentctl-friend/Skill.md`
