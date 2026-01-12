---
allowed-tools: Bash(agentctl:*), Bash(gemini:*), Read
description: Enhanced friend command that uses agentctl for structured context before consulting Gemini
argument-hint: "<query>"
---

## Purpose
Get a second opinion from Gemini on code/architecture questions, using agentctl skills to provide structured context.

## Workflow (agent-only)

1. **Gather structured context using agentctl skills:**
   - `agentctl run code/complexity` - Get complexity hotspots
   - `agentctl run code/symbols` - Extract code structure
   - `agentctl run code/smart_search` - Find relevant code snippets

2. **Format context for Gemini:**
   - Create a structured summary from agentctl output
   - Include relevant file snippets using Read tool if needed

3. **Log the query to mailbox (for traceability):**
   ```bash
   agentctl mailbox send gemini-queries \
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
   agentctl mailbox send claude-queries \
     --from gemini-agent \
     --type agent.reply \
     --payload '{"response": "..."}'
   ```

6. **Present combined analysis:**
   - Show agentctl's structured findings
   - Show Gemini's analysis
   - Provide Claude's synthesis

## Example Context Gathering

```bash
# Get complexity analysis
result=$(agentctl run code/complexity --input '{"path": "internal/agent", "threshold": 10}' 2>&1)
complexity=$(echo "$result" | grep '{"version"' | jq -r '.data.results[] | "- \(.function): complexity \(.cyclomatic_complexity)"')

# Get code symbols
symbols=$(agentctl run code/symbols --input '{"path": "internal/agent", "lang": "go"}' 2>&1)
```

## Output Format

### agentctl Analysis
<!-- Structured findings from agentctl skills -->

### Gemini's Perspective
<details>
<summary>Raw Gemini Output</summary>

<!-- Gemini response -->

</details>

### Synthesis
<!-- Claude's combined analysis -->

---
$ARGUMENTS
