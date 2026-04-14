---
name: foxctl Pre-Implementation Analysis
description: Pre-implementation checklist to understand codebase before writing code. Combines code analysis skills for thorough preparation.
---

# Pre-Implementation Analysis

Systematic codebase analysis before implementation. Run `/pre-impl <task>`.

## Skills Used

```bash
# Find related files
foxctl run fs/find --input '{"path": ".", "pattern": "*client*"}'
foxctl run text/ripgrep --input '{"pattern": "http.*request", "path": "internal/"}'

# Extract code structure
foxctl run code/symbols --input '{"path": "internal/client/http.go"}'

# Check complexity
foxctl run code/complexity --input '{"path": "internal/", "recursive": true}'

# LSP analysis (Go)
foxctl run lsp/gopls --input '{"operation": "references", "file": "main.go", "line": 25, "column": 6}'
foxctl run lsp/gopls --input '{"operation": "call_hierarchy", "file": "main.go", "line": 25, "column": 6}'

# Smart code search
foxctl run code/smart_search --input '{"query": "error handling", "files": ["handler.go"]}'
```

## Output Template

```markdown
## Pre-Impl Summary: <task>
**Problem:** ...
**Approach:** ...
**Key files:** ...
**Assumptions:** ...
**Risk:** ...
Ready to implement: YES/NO
```

Full docs: `~/.foxctl/share/configs/skills/foxctl-preimpl/Skill.md`
