# code.symbol_search

## Overview

Search the code symbol index for functions, methods, and classes by natural
language query. Returns ranked candidates with file paths and symbol metadata.

## Input Schema

```json
{
	"workspace_id": "string (required)",
	"question": "string (required)",
	"mode": "search | callers | callees (optional, default: search)",
	"symbol_hint": "string (optional)",
	"max_results": "integer (optional, default: 20)"
}
```

## Parameters

| Parameter      | Required | Description                                                               |
| -------------- | -------- | ------------------------------------------------------------------------- |
| `workspace_id` | Yes      | Workspace identifier for index lookup                                     |
| `question`     | Yes      | Natural language question describing what you're looking for              |
| `mode`         | No       | `search` (find symbols), `callers` (who calls), `callees` (what it calls) |
| `symbol_hint`  | No       | Symbol name hint to narrow search                                         |
| `max_results`  | No       | Maximum results to return (default: 20)                                   |

## Output Format

```json
{
	"count": 3,
	"candidates": [
		{
			"file": "auth/login.go",
			"symbol_id": "pkg/auth/login.go:Login",
			"name": "Login",
			"kind": "function",
			"score": 0.95
		}
	]
}
```

## Usage Examples

### Basic Search

```json
{
	"workspace_id": "my-project",
	"question": "How does user authentication work?"
}
```

### With Symbol Hint

```json
{
	"workspace_id": "my-project",
	"question": "JWT token validation",
	"symbol_hint": "ValidateToken"
}
```

### Find Callers

```json
{
	"workspace_id": "my-project",
	"question": "Who calls the Login function?",
	"mode": "callers",
	"symbol_hint": "Login"
}
```

## When to Use

- **Starting point** for any code understanding task
- Finding functions/methods by intent, not exact name
- Discovering related code across the codebase
- Building candidate list for `code.swe_grep`

## When NOT to Use

- Known exact pattern → use `code.search` (ripgrep)
- Need full file content → use `fs.read_file`
- Index not available → falls back to stub response

## Best Practices

1. **Be specific in questions** - "How does JWT validation work?" is better than
   "authentication"
2. **Use symbol_hint when known** - Narrows search significantly
3. **Check the score** - Lower scores may be less relevant
4. **Follow up with swe_grep** - Get actual code snippets from candidates
