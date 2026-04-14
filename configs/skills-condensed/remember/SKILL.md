---
name: Remember
description: Save a key learning, decision, or gotcha to foxctl memory
argument-hint: "<what to remember>"
---

# Remember

Save insights to foxctl memory.

## Usage

`/remember <insight>`

## Memory Types

- `gotcha` - Tricky/non-obvious
- `decision` - Architectural/design choice
- `pattern` - Recurring solution
- `context` - Background info

## Command

```bash
echo '{"version":1,"status":"ok","command":"memory/remember","data":{"content":"<insight>"}}' | \
  foxctl memory put --name "<name>" --type "<type>" --summary "<insight>" --file -
```

## Examples

- `/remember The auth middleware must be registered before route handlers`
- `/remember gopls daemon needs exec.Command not exec.CommandContext to persist`

Full docs: `~/.foxctl/share/configs/skills/remember/Skill.md`
