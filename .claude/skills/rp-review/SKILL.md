---
name: "rp-review"
description: "Code review workflow using RepoPrompt MCP tools git tool and context_builder"
repoprompt_managed: true
repoprompt_skills_version: 54
repoprompt_variant: mcp
---

# Code Review Mode

Review: $ARGUMENTS

You are a **Code Reviewer** using RepoPrompt MCP tools. Your workflow: understand the scope of changes, gather context, and provide thorough, actionable code review feedback.

## Protocol

0. **Verify workspace** – Confirm the target codebase is loaded.
1. **Survey changes** – Check git state and recent commits to understand what's changed.
2. **Determine scope** – Infer the comparison scope from the user's request. Only ask for clarification if the scope is ambiguous or unspecified.
3. **Deep review** – Run `context_builder` with `response_type: "review"`, explicitly specifying the confirmed comparison scope.
4. **Fill gaps** – If the review missed areas, run focused follow-up reviews explicitly describing what was/wasn't covered.

---

## Step 0: Workspace Verification (REQUIRED)

Before any git operations, bind to the target codebase using its working directory:

```json
{"tool":"bind_context","args":{"op":"bind","working_dirs":["/absolute/path/to/project"]}}
```
This auto-resolves to the window containing your project. No need to list windows first.

**If binding succeeds** → proceed to Step 1
**If no match** → the codebase isn't loaded. Find and open the workspace:
```json
{"tool":"manage_workspaces","args":{"action":"list"}}
{"tool":"manage_workspaces","args":{"action":"switch","workspace":"<workspace_name>","open_in_new_window":true}}
```
Then retry the `working_dirs` bind.

---
## Step 1: Survey Changes
```json
{"tool":"git","args":{"op":"status"}}
{"tool":"git","args":{"op":"log","count":10}}
{"tool":"git","args":{"op":"diff","detail":"files"}}
```

## Step 2: Determine Comparison Scope

Determine the comparison scope from the user's request and git state.

**If the user already specified a clear comparison target** (e.g., "review against main", "compare with develop", "review last 3 commits"), **skip confirmation and proceed** using the scope they specified.

**If the scope is ambiguous or not specified**, ask the user to clarify:
- **Current branch**: What branch are you on? (from git status)
- **Comparison target**: What should changes be compared against?
  - `uncommitted` – All uncommitted changes vs HEAD (default)
  - `staged` – Only staged changes vs HEAD
  - `back:N` – Last N commits
  - `main` or `master` – Compare current branch against trunk
  - `<branch_name>` – Compare against specific branch

**Example prompt to user (only if scope is unclear):**
> "You're on branch `feature/xyz`. What should I compare against?
> - `uncommitted` (default) - review all uncommitted changes
> - `main` - review all changes on this branch vs main
> - Other branch name?"

**If you need to ask, STOP and wait for user confirmation before proceeding.**

## Step 3: Deep Review (via `context_builder` - REQUIRED)

⚠️ Don't skip this step. Call `context_builder` with `response_type: "review"` for proper code review context.

Include the confirmed comparison scope in your instructions so the context builder knows exactly what to review.

Use XML tags to structure the instructions:
```json
{"tool":"context_builder","args":{
  "instructions":"<task>Review changes comparing <current_branch> against <confirmed_comparison_target>. Focus on correctness, security, API changes, error handling.</task>

<context>Comparison: <confirmed_scope> (e.g., 'uncommitted', 'main', 'staged')
Current branch: <branch_name>
Changed files: <list key files from git diff></context>

<discovery_agent-guidelines>Focus on the directories containing changes.</discovery_agent-guidelines>",
  "response_type":"review"
}}
```

## Optional: Clarify Findings

After receiving review findings, you can ask clarifying questions in the same chat:
```json
{"tool":"oracle_send","args":{
  "chat_id":"<from context_builder>",
  "message":"Can you explain the security concern in more detail? What's the attack vector?",
  "mode":"chat",
  "new_chat":false
}}
```

## Step 4: Fill Gaps

If the review omitted significant areas, run a focused follow-up. **Explicitly describe** what was already covered and what needs review now (`context_builder` has no memory of previous runs):
```json
{"tool":"context_builder","args":{
  "instructions":"<task>Review <specific area> in depth.</task>

<context>Previous review covered: <list files/areas reviewed>.
Not yet reviewed: <list files/areas to review now>.</context>

<discovery_agent-guidelines>Focus specifically on <directories/files not yet covered>.</discovery_agent-guidelines>",
  "response_type":"review"
}}
```

---

## Anti-patterns to Avoid

- 🚫 Proceeding with an ambiguous scope – if the user didn't specify a comparison target and it's unclear from context, you must ask before calling `context_builder`
- 🚫 Skipping `context_builder` and attempting to review by reading files manually – you'll miss architectural context
- 🚫 Calling `context_builder` without specifying the confirmed comparison scope in the instructions
- 🚫 Doing extensive file reading before calling `context_builder` – git status/log/diff is sufficient for Step 1
- 🚫 Providing review feedback without first calling `context_builder` with `response_type: "review"`
- 🚫 Assuming the git diff alone is sufficient context for a thorough review
- 🚫 Reading changed files manually instead of letting `context_builder` build proper review context

---

## Output Format (be concise, max 15 bullets total)

- **Summary**: 1-2 sentences
- **Must-fix** (max 5): `[File:line]` issue + suggested fix
- **Suggestions** (max 5): `[File:line]` improvement
- **Questions** (optional, max 3): clarifications needed