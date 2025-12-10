# code.swe_grep

## Overview

Extract high-signal code snippets from candidate files based on a natural
language question. Use this after `code.symbol_search` to get detailed code
context before making edits.

## Input Schema

```json
{
	"workspace_id": "string (required)",
	"question": "string (required)",
	"candidate_files": [
		{
			"path": "string (required)",
			"symbol_id": "string (optional)",
			"priority": "number (optional, 0-1)"
		}
	]
}
```

## Parameters

| Parameter         | Required | Description                                           |
| ----------------- | -------- | ----------------------------------------------------- |
| `workspace_id`    | Yes      | Workspace identifier                                  |
| `question`        | Yes      | Natural language question to guide snippet extraction |
| `candidate_files` | Yes      | Array of files to search (from symbol_search output)  |

### Candidate File Object

| Field       | Required | Description                                      |
| ----------- | -------- | ------------------------------------------------ |
| `path`      | Yes      | Relative path to the file                        |
| `symbol_id` | No       | Specific symbol to focus on (from symbol_search) |
| `priority`  | No       | Ranking hint (0-1, higher = more important)      |

## Output Format

```json
{
	"count": 2,
	"snippets": [
		{
			"file": "auth/login.go",
			"symbol_id": "Login",
			"start_line": 10,
			"end_line": 45,
			"preview": "func Login(ctx context.Context, creds Credentials) (*Token, error) {\n    // Validate credentials\n    ..."
		}
	]
}
```

## Usage Examples

### From Symbol Search Results

```json
{
	"workspace_id": "my-project",
	"question": "How does the login function validate credentials?",
	"candidate_files": [
		{ "path": "auth/login.go", "symbol_id": "Login", "priority": 0.95 },
		{
			"path": "auth/validate.go",
			"symbol_id": "ValidateCredentials",
			"priority": 0.8
		}
	]
}
```

### Without Symbol IDs

```json
{
	"workspace_id": "my-project",
	"question": "Error handling patterns",
	"candidate_files": [
		{ "path": "pkg/errors/handler.go" },
		{ "path": "internal/api/middleware.go" }
	]
}
```

## Workflow: Symbol Search → SWE Grep

```text
1. code.symbol_search → get candidate files with symbol IDs
2. code.swe_grep → extract relevant snippets from candidates
3. fs.read_file → read full file if needed for broader context
4. edit.apply_patch → make the change
```

## When to Use

- **After symbol search** - Convert candidates to actual code snippets
- **Before editing** - Understand the code context
- **Multiple files** - Extract relevant parts from several files at once
- **Focused extraction** - Get just the relevant snippets, not entire files

## When NOT to Use

- No candidates yet → run `code.symbol_search` first
- Need entire file → use `fs.read_file`
- Simple pattern search → use `code.search` (ripgrep)
- Very large files → may hit token limits

## Best Practices

1. **Provide symbol_id when available** - Much more precise extraction
2. **Use priority scores** - Pass through scores from symbol_search
3. **Limit candidates** - 5-10 files is usually sufficient
4. **Specific questions** - Better questions yield better snippets
