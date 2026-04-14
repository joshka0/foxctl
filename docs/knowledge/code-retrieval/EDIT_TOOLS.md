# Edit Tools

## Overview

foxctl provides two edit tools for modifying code files:

| Tool                         | Use Case                | Complexity |
| ---------------------------- | ----------------------- | ---------- |
| `edit.apply_patch`           | Simple text replacement | Low        |
| `edit.apply_structured_diff` | Multi-hunk diffs        | High       |

## edit.apply_patch

Simple old_text → new_text replacement. Best for single-location edits.

### Input Schema

```json
{
	"path": "string (required)",
	"old_text": "string (required)",
	"new_text": "string (required)"
}
```

### Example

```json
{
	"path": "auth/login.go",
	"old_text": "func Login(ctx context.Context) error {",
	"new_text": "func Login(ctx context.Context, opts *LoginOptions) error {"
}
```

### When to Use

- Single function signature change
- Adding/removing a few lines
- Simple text replacement
- Quick fixes

### Limitations

- Only one replacement per call
- `old_text` must match exactly (including whitespace)
- Not suitable for multi-location changes

---

## edit.apply_structured_diff

Apply structured diffs from the `code/diff` skill. Best for complex refactors.

### Input Schema

```json
{
	"path": "string (required)",
	"diff_json": {
		"hunks": [
			{
				"old_start": "integer",
				"old_lines": "integer",
				"new_start": "integer",
				"new_lines": "integer",
				"lines": ["string array with +/-/space prefixes"]
			}
		]
	},
	"dry_run": "boolean (optional)"
}
```

### Example

```json
{
	"path": "auth/login.go",
	"diff_json": {
		"hunks": [
			{
				"old_start": 10,
				"old_lines": 3,
				"new_start": 10,
				"new_lines": 5,
				"lines": [
					" func Login(ctx context.Context) error {",
					"-    return validateCredentials(ctx)",
					"+    if err := validateCredentials(ctx); err != nil {",
					"+        return fmt.Errorf(\"validation failed: %w\", err)",
					"+    }",
					"+    return nil",
					" }"
				]
			}
		]
	}
}
```

### Hunk Line Prefixes

| Prefix     | Meaning                  |
| ---------- | ------------------------ |
| `` (space) | Context line (unchanged) |
| `-`        | Removed line             |
| `+`        | Added line               |

### When to Use

- Multi-hunk changes across a file
- Complex refactors with many insertions/deletions
- Applying diffs from `code/diff` skill
- Changes that span multiple locations

### Features

- **Dry run mode**: Set `dry_run: true` to validate without writing
- **Context verification**: Validates context lines match before applying
- **Reverse order application**: Hunks applied bottom-up to preserve line
  numbers

---

## Tool Selection Decision Tree

```
Is it a simple, single-location edit?
├─ YES → edit.apply_patch
└─ NO
   └─ Is it a multi-hunk or complex change?
      ├─ YES → edit.apply_structured_diff
      └─ NO → edit.apply_patch (multiple calls)
```

## Common Patterns

### Add Import Statement

```json
// Use apply_patch - simple insertion
{
	"path": "main.go",
	"old_text": "import (\n\t\"fmt\"",
	"new_text": "import (\n\t\"context\"\n\t\"fmt\""
}
```

### Refactor Function Signature + Body

```json
// Use apply_structured_diff - multiple hunks
{
	"path": "handler.go",
	"diff_json": {
		"hunks": [
			{
				"old_start": 5,
				"lines": ["-func Handle()", "+func Handle(ctx context.Context)"]
			},
			{ "old_start": 20, "lines": ["-    doWork()", "+    doWork(ctx)"] }
		]
	}
}
```

## Error Handling

Both tools return errors for:

- File not found
- Path escapes workspace
- Text/context mismatch
- Invalid diff format

Check `result.IsError` and parse error messages for details.
