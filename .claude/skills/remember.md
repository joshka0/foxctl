# /remember - Save a key learning, decision, or gotcha

Save important context to both foxctl memory AND CLAUDE.md for persistence.

## Arguments

- `$ARGUMENTS` - The content to remember (optional, can extract from conversation)

## Steps

1. **Extract the insight** from the user's message or `$ARGUMENTS`:
   - Identify the key learning, gotcha, decision, or tip
   - Determine the type: `gotcha`, `decision`, `pattern`, or `context`
   - Create a concise summary (1-2 sentences)

2. **Save to foxctl memory**:
   ```bash
   foxctl memory put \
     --name "<descriptive-name>" \
     --type "<type>" \
     --summary "<concise summary>" \
     --data '{"details": "<full context>"}'
   ```

3. **Append to CLAUDE.md** under Gotchas section:
   - Read current `.claude/CLAUDE.md`
   - Find or create the `## Gotchas` section
   - Add a concise bullet point under appropriate subsection
   - Format: `- **<topic>**: <insight>`

## Example

User: "remember that CGO builds need -tags=libsqlite3 to avoid duplicate symbols"

Actions:
1. Save to memory:
   ```bash
   foxctl memory put \
     --name "gotcha-cgo-sqlite-tags" \
     --type "gotcha" \
     --summary "CGO builds require -tags=libsqlite3 to avoid duplicate SQLite symbols" \
     --data '{"details": "Both go-libsql and go-sqlite3 embed SQLite, causing linker conflicts"}'
   ```

2. Append to `.claude/CLAUDE.md`:
   ```markdown
   - **CGO builds**: Always use `-tags=libsqlite3` to avoid duplicate SQLite symbols
   ```

## Notes

- Keep CLAUDE.md entries concise (single line preferred)
- Group related gotchas under subsections if patterns emerge
- Memory entries can have more detail in the `--data` field
