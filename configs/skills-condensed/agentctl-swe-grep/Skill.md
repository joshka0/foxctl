---
name: agentctl SWE Grep
description: Extract high-signal code snippets from files based on natural language questions. Smart code retrieval for AI agents.
---

# SWE Grep - Smart Code Extraction

Extract relevant code snippets from candidate files based on natural language questions.

## Usage

```bash
agentctl run code/swe_grep --input '{
  "workspace_id": "my-project",
  "question": "How does user authentication work?",
  "candidates": [
    {"path": "internal/auth/login.go", "priority": 0.95},
    {"path": "internal/auth/session.go"}
  ]
}'
```

## Parameters

| Param | Type | Description |
|-------|------|-------------|
| `workspace_id` | string | Workspace identifier (required) |
| `question` | string | Natural language question (required) |
| `candidates` | array | Files to process: `{path, symbol_id?, priority?}` |
| `limits` | object | `{max_files, max_snippets, max_bytes_per_file}` |

## Output

```json
{
  "data": {
    "summary": {"files_considered": 3, "snippets_emitted": 5},
    "snippets_inline": [
      {"path": "auth/login.go", "start_line": 42, "end_line": 67, "content": "..."}
    ]
  }
}
```

Large results stored in CAS: `agentctl cas get sha256:...`

Full docs: `~/repos/personal/agentctl/configs/skills/agentctl-swe-grep/Skill.md`
