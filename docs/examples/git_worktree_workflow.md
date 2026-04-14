# Git Worktree Workflow Example

This example demonstrates using the `git/worktree` skill to manage multiple branches simultaneously.

## Scenario

You're working on a project and need to:
1. Continue development on a feature branch
2. Quickly fix a bug on main
3. Review a colleague's PR

Instead of stashing changes and switching branches, use git worktrees!

## Workflow

### 1. Check current worktrees

```bash
echo '{"operation":"list"}' | foxctl run git/worktree
```

Output shows your main worktree:
```json
{
  "worktree_count": 1,
  "worktrees": [
    {
      "path": "/home/user/myproject",
      "branch": "refs/heads/feature-x",
      "commit": "abc123"
    }
  ]
}
```

### 2. Create a worktree for hotfix

```bash
echo '{
  "operation": "add",
  "path": "/tmp/myproject-hotfix",
  "branch": "hotfix-123",
  "new_branch": true
}' | foxctl run git/worktree
```

Now you have two working directories:
- `/home/user/myproject` - your feature work
- `/tmp/myproject-hotfix` - hotfix branch

### 3. Work on the hotfix

```bash
cd /tmp/myproject-hotfix
# Make your fixes
git add .
git commit -m "fix: resolve critical bug"
git push origin hotfix-123
```

### 4. Create another worktree for PR review

```bash
echo '{
  "operation": "add",
  "path": "/tmp/myproject-pr-review",
  "branch": "colleague-feature"
}' | foxctl run git/worktree
```

### 5. Review the changes

```bash
cd /tmp/myproject-pr-review
# Review code, run tests, etc.
```

### 6. Clean up when done

```bash
# Remove the hotfix worktree
echo '{
  "operation": "remove",
  "path": "/tmp/myproject-hotfix"
}' | foxctl run git/worktree

# Remove the PR review worktree
echo '{
  "operation": "remove",
  "path": "/tmp/myproject-pr-review"
}' | foxctl run git/worktree

# Prune any stale references
echo '{"operation":"prune"}' | foxctl run git/worktree
```

### 7. Continue your feature work

```bash
cd /home/user/myproject
# Back to your feature branch, exactly as you left it!
```

## Benefits

1. **No context switching**: Each branch has its own directory
2. **No stashing required**: Changes remain in place
3. **Parallel work**: Build/test multiple branches simultaneously
4. **Clean workflow**: Easy to switch between tasks

## Advanced: Using with Jobs

For long-running operations, use foxctl jobs (note: CLI flag syntax for jobs is planned but not yet implemented):

```bash
# Current workaround: use synchronous run command
echo '{
  "operation": "add",
  "path": "/tmp/big-refactor",
  "branch": "refactor-v2",
  "new_branch": true
}' | foxctl run git/worktree

# Async jobs support is planned for future releases
# Future syntax (not yet implemented):
# JOB_ID=$(foxctl jobs submit git/worktree \
#   --operation add \
#   --path /tmp/big-refactor \
#   --branch refactor-v2 \
#   --new_branch true | jq -r '.data.job_id')
```

## Integration with Other Skills

Combine with other foxctl skills:

```bash
# 1. List worktrees
foxctl run git/worktree --operation list --remember worktrees

# 2. For each worktree, check for TODOs
foxctl memory get worktrees | jq -r '.worktrees[].path' | while read path; do
  echo "Checking $path for TODOs..."
  foxctl run text/grep --pattern "TODO" --path "$path"
done

# 3. List files in each worktree
foxctl memory get worktrees | jq -r '.worktrees[].path' | while read path; do
  echo "Files in $path:"
  foxctl run fs/ls --path "$path"
done
```

## Tips

1. **Use /tmp for temporary worktrees**: They're automatically cleaned on reboot
2. **Name worktrees by purpose**: e.g., `project-hotfix`, `project-review`
3. **Clean up regularly**: Use `prune` to remove stale entries
4. **One worktree per task**: Keeps context clean and focused

## Common Use Cases

### Parallel CI Testing
```bash
# Create worktrees for different test configurations
for config in unit integration e2e; do
  foxctl run git/worktree \
    --operation add \
    --path "/tmp/test-$config" \
    --branch "test-$config"
done

# Run tests in parallel (separate terminals/jobs)
# Then clean up
```

### Code Review Workflow
```bash
# Fetch latest PR branches
git fetch origin

# Create worktree for each PR to review
for pr in 123 124 125; do
  foxctl run git/worktree \
    --operation add \
    --path "/tmp/pr-$pr" \
    --branch "origin/pr-$pr"
done
```

### Multi-version Support
```bash
# Maintain multiple release branches
foxctl run git/worktree \
  --operation add \
  --path "/opt/app-v1" \
  --branch "release-v1"

foxctl run git/worktree \
  --operation add \
  --path "/opt/app-v2" \
  --branch "release-v2"
```
