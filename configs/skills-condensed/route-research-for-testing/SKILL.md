---
name: Route Research for Testing
description: Map edited routes & launch tests
argument-hint: "[/extra/path ...]"
allowed-tools: Bash(cat:*), Bash(awk:*), Bash(grep:*), Bash(sort:*), Bash(xargs:*), Bash(sed:*)
model: sonnet
---

# Route Testing

Map changed routes and launch tests.

## Workflow

1. **Find changed routes**:
   ```bash
   cat "$CLAUDE_PROJECT_DIR/.claude/tsc-cache"/*/edited-files.log | awk -F: '{print $2}' | grep '/routes/' | sort -u
   ```

2. **Combine** with `$ARGUMENTS`, dedupe, resolve prefixes from `src/app.ts`

3. **Output JSON** per route: path, method, request/response shapes, valid/invalid payloads

4. **Launch tester**: Call Task tool with `auth-route-tester` sub-agent

Full docs: `~/.foxctl/share/configs/skills/route-research-for-testing/Skill.md`
