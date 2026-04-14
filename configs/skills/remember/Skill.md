# Remember

Save a key learning, decision, or gotcha to foxctl memory for future reference.

## Usage

```
/remember <what to remember>
```

## Examples

- `/remember The auth middleware must be registered before route handlers`
- `/remember gopls daemon needs exec.Command not exec.CommandContext to persist`
- `/remember Always validate workspace_id for task operations`

## Instructions

When the user runs this command, save the memory to foxctl:

1. Extract the key insight from `$ARGUMENTS`
2. Determine the memory type:
   - `gotcha` - Something that was tricky or non-obvious
   - `decision` - An architectural or design choice
   - `pattern` - A recurring solution or approach
   - `context` - Important background information

3. Create a descriptive name (kebab-case, max 50 chars)

4. Run the foxctl command:
```bash
echo '{"version":1,"status":"ok","command":"memory/remember","data":{"content":"<the insight>"}}' | \
  foxctl memory put \
    --name "<name>" \
    --type "<type>" \
    --summary "<the insight>" \
    --file -
```

5. Confirm to the user what was saved

## Arguments

$ARGUMENTS
