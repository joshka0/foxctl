---
description: Pre-implementation analysis checklist - understand before you build
argument-hint: What you want to implement (e.g., "add user authentication", "refactor error handling")
---

# Pre-Implementation Checklist for: $ARGUMENTS

Before writing any code, complete this analysis. Do NOT skip steps.

## Phase 1: Understand the Context

### 1.1 Find Related Code

Search for existing code related to this task:

```bash
# Find files by name patterns
foxctl run fs/find --input '{"path": ".", "pattern": "*relevant*"}'

# Search for related symbols/patterns
foxctl run text/ripgrep --input '{"pattern": "related_term", "path": "."}'
```

**Output required:**
- [ ] List 3-5 most relevant existing files
- [ ] For each file: one-sentence summary of what it does

### 1.2 Read the Code Philosophy

For each relevant file identified:

```bash
# Extract symbols to understand structure
foxctl run code/symbols --input '{"path": "path/to/file.go"}'

# Check complexity
foxctl run code/complexity --input '{"path": "path/to/file.go"}'
```

**Output required:**
- [ ] What patterns does this codebase use? (naming, error handling, structure)
- [ ] What abstractions exist that I should reuse?
- [ ] What should I NOT do based on existing code?

### 1.3 Semantic Analysis (Language-specific)

Use LSP for deeper understanding. Choose based on project type:

**Go projects:**
```bash
# Find all references to a function/type
foxctl run lsp/gopls --input '{"operation": "references", "file": "path/to/file.go", "line": 20, "column": 6}'

# Understand call hierarchy (who calls this? what does it call?)
foxctl run lsp/gopls --input '{"operation": "call_hierarchy", "file": "path/to/file.go", "line": 20, "column": 6}'

# Search for related types/functions across workspace
foxctl run lsp/gopls --input '{"operation": "workspace_symbol", "query": "Handler"}'
```

**Python projects:**
```bash
# Find all references
foxctl run lsp/pylsp --input '{"operation": "references", "file": "src/main.py", "line": 10, "column": 5}'

# Get documentation for a symbol
foxctl run lsp/pylsp --input '{"operation": "hover", "file": "src/main.py", "line": 10, "column": 5}'

# Search workspace
foxctl run lsp/pylsp --input '{"operation": "workspace_symbol", "query": "Handler"}'
```

**TypeScript/JavaScript projects:**
```bash
# Find all references
foxctl run lsp/tsserver --input '{"operation": "references", "file": "src/index.ts", "line": 10, "column": 5}'

# Search workspace
foxctl run lsp/tsserver --input '{"operation": "workspace_symbol", "query": "Handler"}'
```

**Output required:**
- [ ] How is the code I'm modifying currently used? (from references)
- [ ] What's the call chain? (from call_hierarchy, Go only)
- [ ] Are there similar patterns elsewhere I should follow?

## Phase 2: Challenge Assumptions

### 2.1 Question the Request

Answer these before proceeding:

- [ ] **What problem does this actually solve?** (not the stated request, the underlying need)
- [ ] **Is this the simplest solution?** List 2 alternatives you considered and rejected
- [ ] **What assumptions am I making?** List them explicitly
- [ ] **What could go wrong?** Top 3 failure modes

### 2.2 Scope Check

- [ ] **What files will I modify?** List them
- [ ] **What files will I create?** (Should be minimal - prefer editing existing)
- [ ] **What's OUT of scope?** Be explicit about what you won't do

## Phase 3: Propose Solution

### 3.1 Implementation Approach

Write a 3-5 sentence description of your approach. Include:
- Where the main logic will live
- What existing patterns you'll follow
- What you'll reuse vs create

### 3.2 Checklist for Implementation

Create a numbered checklist of discrete steps:

1. [ ] Step 1...
2. [ ] Step 2...
3. [ ] ...

### 3.3 Test Strategy

- [ ] How will you verify this works?
- [ ] What edge cases need testing?

---

## Output Format

After completing this analysis, output a summary block:

```markdown
## Pre-Impl Summary: [task name]

**Problem:** [one sentence]
**Approach:** [one sentence]
**Key files:** [comma-separated list]
**Assumptions:** [bullet list]
**Risk:** [highest risk item]
**Steps:** [count] steps identified

Ready to implement: YES/NO
```

---

**STOP** if you cannot fill out this checklist. Ask clarifying questions instead of guessing.
