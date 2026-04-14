---
allowed-tools: Bash(foxctl:*), Bash(gemini:*), Read
description: Enhanced friend command that uses foxctl for structured context before consulting Gemini
argument-hint: "<query>"
---

## Purpose
Get a second opinion from Gemini on code/architecture questions, using foxctl skills to provide structured context.

## Workflow (agent-only)

1. **Gather structured context using foxctl skills:**
   - `foxctl run code/complexity` - Get complexity hotspots
   - `foxctl run code/symbols` - Extract code structure
   - `foxctl run code/smart_search` - Find relevant code snippets

2. **Format context for Gemini:**
   - Create a structured summary from foxctl output
   - Include relevant file snippets using Read tool if needed

3. **Log the query to mailbox (for traceability):**
   ```bash
   foxctl mailbox send gemini-queries \
     --from claude-agent \
     --type agent.ask \
     --payload '{"query": "$ARGUMENTS", "context_summary": "..."}'
   ```

4. **Send to Gemini with pre-computed context:**
   ```bash
   echo "$STRUCTURED_CONTEXT" | gemini -p "$ARGUMENTS"
   ```

5. **Log Gemini's response to mailbox:**
   ```bash
   foxctl mailbox send claude-queries \
     --from gemini-agent \
     --type agent.reply \
     --payload '{"response": "..."}'
   ```

6. **Present combined analysis:**
   - Show foxctl's structured findings
   - Show Gemini's analysis
   - Provide Claude's synthesis

## Example Context Gathering

```bash
# Get complexity analysis
result=$(foxctl run code/complexity --input '{"path": "internal/agent", "threshold": 10}' 2>&1)
complexity=$(echo "$result" | grep '{"version"' | jq -r '.data.results[] | "- \(.function): complexity \(.cyclomatic_complexity)"')

# Get code symbols
symbols=$(foxctl run code/symbols --input '{"path": "internal/agent", "lang": "go"}' 2>&1)
```

## Output Format

### foxctl Analysis
<!-- Structured findings from foxctl skills -->

### Gemini's Perspective
<details>
<summary>Raw Gemini Output</summary>

<!-- Gemini response -->

</details>

### Synthesis
<!-- Claude's combined analysis -->

---
$ARGUMENTS
