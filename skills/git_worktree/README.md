# git/worktree Skill

A skill for managing git worktrees through foxctl.

## Overview

This skill provides a wrapper around `git worktree` commands, allowing you to manage multiple working trees attached to the same repository. This is useful for working on multiple branches simultaneously without having to switch between them.

## Operations

### list

List all worktrees associated with a repository.

**Input:**
```json
{
  "operation": "list",
  "repo_path": "/path/to/repo"
}
```

**Output:**
```json
{
  "version": 1,
  "status": "ok",
  "command": "git/worktree",
  "data": {
    "operation": "list",
    "worktree_count": 2,
    "worktrees": [
      {
        "path": "/home/user/foxctl",
        "branch": "refs/heads/main",
        "commit": "abc123...",
        "bare": false
      },
      {
        "path": "/tmp/feature-branch",
        "branch": "refs/heads/feature",
        "commit": "def456...",
        "bare": false
      }
    ]
  },
  "meta": {
    "ts": "2025-11-17T18:47:43Z",
    "source": "run",
    "runner": "exec"
  }
}
```

### add

Create a new worktree.

**Input:**
```json
{
  "operation": "add",
  "repo_path": "/path/to/repo",
  "path": "/path/to/new/worktree",
  "branch": "feature-branch",
  "new_branch": true
}
```

**Parameters:**
- `path` (required): Where to create the new worktree
- `branch` (optional): Branch name to checkout or create
- `new_branch` (optional): If true, creates a new branch with `-b`

**Output:**
```json
{
  "version": 1,
  "status": "ok",
  "command": "git/worktree",
  "data": {
    "operation": "add",
    "path": "/tmp/feature-branch",
    "branch": "feature-branch",
    "message": "Worktree added at /tmp/feature-branch"
  }
}
```

### remove

Remove a worktree.

**Input:**
```json
{
  "operation": "remove",
  "repo_path": "/path/to/repo",
  "path": "/path/to/worktree",
  "force": false
}
```

**Parameters:**
- `path` (required): Path to the worktree to remove
- `force` (optional): If true, forces removal even if the worktree is dirty

**Output:**
```json
{
  "version": 1,
  "status": "ok",
  "command": "git/worktree",
  "data": {
    "operation": "remove",
    "path": "/tmp/feature-branch",
    "message": "Worktree removed from /tmp/feature-branch"
  }
}
```

### prune

Prune stale worktree administrative files.

**Input:**
```json
{
  "operation": "prune",
  "repo_path": "/path/to/repo"
}
```

**Output:**
```json
{
  "version": 1,
  "status": "ok",
  "command": "git/worktree",
  "data": {
    "operation": "prune",
    "message": "No stale worktrees to prune"
  }
}
```

## Usage Examples

### Using the skill directly

```bash
# List worktrees
echo '{"operation":"list","repo_path":"."}' | ./dist/skills/git_worktree/bin

# Add a new worktree with a new branch
echo '{
  "operation":"add",
  "repo_path":".",
  "path":"/tmp/my-feature",
  "branch":"my-feature",
  "new_branch":true
}' | ./dist/skills/git_worktree/bin

# Remove a worktree
echo '{
  "operation":"remove",
  "repo_path":".",
  "path":"/tmp/my-feature"
}' | ./dist/skills/git_worktree/bin

# Prune stale worktrees
echo '{"operation":"prune","repo_path":"."}' | ./dist/skills/git_worktree/bin
```

### Using with foxctl (when integrated)

Use `foxctl skills run` for direct parameter flags. Use `foxctl run` with
`--input` when you want job-tracked execution.

```bash
# List worktrees
foxctl skills run git/worktree --operation list --repo-path .

# Add a worktree
foxctl skills run git/worktree \
  --operation add \
  --path /tmp/my-feature \
  --branch my-feature \
  --new-branch true

# Remove a worktree
foxctl skills run git/worktree \
  --operation remove \
  --path /tmp/my-feature

# Prune worktrees
foxctl skills run git/worktree --operation prune
```

## Security

This skill:
- Respects workspace confinement through PathValidator
- Does not require network access (`network: none`)
- Can only access paths within the workspace
- Cannot execute arbitrary commands (only `git worktree`)

## Requirements

- Git must be installed and available in PATH
- Must be run within a git repository
- All paths are validated against the workspace

## Notes

- All worktree paths must be within the allowed workspace
- The skill uses workspace path validation to prevent directory traversal
- Follows the foxctl envelope protocol (Version 1)
- Pure skill: false (has side effects on filesystem and git state)
