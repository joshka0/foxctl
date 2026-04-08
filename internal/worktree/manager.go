package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GitManager implements the Manager interface using git CLI commands.
// It is safe for concurrent use.
type GitManager struct{}

// NewManager creates a new worktree Manager backed by git CLI commands.
func NewManager() *GitManager {
	return &GitManager{}
}

// Compile-time check that GitManager implements Manager.
var _ Manager = (*GitManager)(nil)

// Create creates a new git worktree for the given branch.
func (m *GitManager) Create(ctx context.Context, repo string, branch string, opts ...Option) (WorktreeResult, error) {
	options := ApplyOptions(opts...)

	// Validate context
	if err := ctx.Err(); err != nil {
		return WorktreeResult{}, err
	}

	// Validate repo is a git repository
	if err := validateGitRepo(repo); err != nil {
		return WorktreeResult{}, fmt.Errorf("create worktree: %w", err)
	}

	// Check for path traversal in branch name before sanitization
	if strings.Contains(branch, "..") {
		return WorktreeResult{}, fmt.Errorf("create worktree: invalid branch name: path traversal detected")
	}

	// Sanitize and validate branch name
	cleanBranch, err := SanitizeBranchName(branch)
	if err != nil {
		return WorktreeResult{}, fmt.Errorf("create worktree: %w", err)
	}

	// Resolve base directory
	baseDir := resolveBaseDir(repo, options.BaseDir)
	worktreePath := filepath.Join(baseDir, cleanBranch)

	// Check if this is a new branch or existing
	isNewBranch := options.NewBranch

	// Build git worktree add command
	args := []string{"worktree", "add"}
	if isNewBranch {
		args = append(args, "-b", cleanBranch)
		if options.Ref != "" {
			args = append(args, worktreePath, options.Ref)
		} else {
			args = append(args, worktreePath)
		}
	} else {
		args = append(args, worktreePath, cleanBranch)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check for context cancellation
		if ctx.Err() != nil {
			return WorktreeResult{}, ctx.Err()
		}
		return WorktreeResult{}, structuredGitError("create", output, err)
	}

	// Get HEAD commit of the new worktree
	commit, err := getHeadCommit(worktreePath)
	if err != nil {
		// Rollback: remove the worktree we just created
		_, _ = m.runGitNoOutput(repo, "worktree", "remove", "--force", worktreePath)
		return WorktreeResult{}, fmt.Errorf("create worktree: failed to get HEAD: %w", err)
	}

	// Resolve symlinks so returned path matches what git reports
	// (macOS: /var → /private/var)
	resolvedPath := worktreePath
	if resolved, err := filepath.EvalSymlinks(worktreePath); err == nil {
		resolvedPath = resolved
	}

	result := WorktreeResult{
		Path:      resolvedPath,
		Branch:    cleanBranch,
		Commit:    commit,
		CreatedAt: time.Now().UTC(),
	}

	// Run post-create hook (failure does not roll back the worktree)
	if options.Hooks.PostCreate != "" {
		hookEnv := map[string]string{
			"WORKTREE_PATH":      result.Path,
			"WORKTREE_BRANCH":    result.Branch,
			"WORKTREE_REPO_PATH": repo,
			"WORKTREE_COMMIT":    result.Commit,
		}
		if hookErr := RunHook(ctx, options.Hooks.PostCreate, hookEnv, options.Hooks.EffectiveTimeout()); hookErr != nil {
			return result, fmt.Errorf("create worktree succeeded but post-create hook failed: %w", hookErr)
		}
	}

	return result, nil
}

// List returns all worktrees for the given repository.
func (m *GitManager) List(ctx context.Context, repo string) ([]WorktreeEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := validateGitRepo(repo); err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}

	output, err := m.runGitOutput(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, structuredGitError("list", []byte(output), err)
	}

	entries, err := ParsePorcelain(output)
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}

	return entries, nil
}

// Remove removes a worktree at the given path.
func (m *GitManager) Remove(ctx context.Context, repo string, path string, opts ...Option) error {
	options := ApplyOptions(opts...)

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := validateGitRepo(repo); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}

	// Reject removal of main checkout
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return fmt.Errorf("remove worktree: resolving repo path: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("remove worktree: resolving worktree path: %w", err)
	}

	if absRepo == absPath {
		return fmt.Errorf("remove worktree: cannot remove main checkout")
	}

	// Before removing, look up the branch name if we need to delete it
	var branchToDelete string
	if options.DeleteBranch {
		entries, listErr := m.List(ctx, repo)
		if listErr == nil {
			absPath2, _ := filepath.Abs(path)
			evalPath, _ := filepath.EvalSymlinks(absPath2)
			for _, e := range entries {
				absEntry, _ := filepath.Abs(e.Path)
				evalEntry, _ := filepath.EvalSymlinks(absEntry)
				if absEntry == absPath2 || evalEntry == evalPath {
					branchToDelete = e.Branch
					break
				}
			}
		}
	}

	args := []string{"worktree", "remove"}
	if options.Force {
		args = append(args, "--force")
	}
	args = append(args, path)

	_, err = m.runGitNoOutput(repo, args...)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return structuredGitError("remove", nil, err)
	}

	// Optionally delete the branch
	if options.DeleteBranch && branchToDelete != "" {
		// Ignore errors if branch is already gone
		_, _ = m.runGitNoOutput(repo, "branch", "-D", branchToDelete)
	}

	// Run post-remove hook (failure does not roll back the removal)
	if options.Hooks.PostRemove != "" {
		hookEnv := map[string]string{
			"WORKTREE_PATH":      path,
			"WORKTREE_REPO_PATH": repo,
		}
		if branchToDelete != "" {
			hookEnv["WORKTREE_BRANCH"] = branchToDelete
		}
		if hookErr := RunHook(ctx, options.Hooks.PostRemove, hookEnv, options.Hooks.EffectiveTimeout()); hookErr != nil {
			return fmt.Errorf("remove worktree succeeded but post-remove hook failed: %w", hookErr)
		}
	}

	return nil
}

// Resolve finds a worktree by branch name or partial match.
func (m *GitManager) Resolve(ctx context.Context, repo string, query string) (WorktreeEntry, error) {
	if err := ctx.Err(); err != nil {
		return WorktreeEntry{}, err
	}

	entries, err := m.List(ctx, repo)
	if err != nil {
		return WorktreeEntry{}, err
	}

	// Special case: "1" returns main checkout
	if query == "1" {
		for _, e := range entries {
			if e.Path == repo || e.Branch == "main" || e.Branch == "master" {
				return e, nil
			}
		}
		return WorktreeEntry{}, fmt.Errorf("resolve: main checkout not found")
	}

	// Exact match by branch name
	for _, e := range entries {
		if e.Branch == query {
			return e, nil
		}
	}

	// Exact match by path
	for _, e := range entries {
		if e.Path == query {
			return e, nil
		}
	}

	// Suffix match: query "abc-123" matches "feat/abc-123"
	var candidates []WorktreeEntry
	for _, e := range entries {
		if strings.HasSuffix(e.Branch, "/"+query) || e.Branch == query {
			candidates = append(candidates, e)
		}
	}

	switch len(candidates) {
	case 0:
		return WorktreeEntry{}, fmt.Errorf("resolve: no worktree matches %q", query)
	case 1:
		return candidates[0], nil
	default:
		names := make([]string, len(candidates))
		for i, c := range candidates {
			names[i] = c.Branch
		}
		return WorktreeEntry{}, fmt.Errorf("resolve: ambiguous query %q matches: %s", query, strings.Join(names, ", "))
	}
}

// Status returns the health status of a specific worktree.
func (m *GitManager) Status(ctx context.Context, repo string, path string) (WorktreeEntry, error) {
	if err := ctx.Err(); err != nil {
		return WorktreeEntry{}, err
	}

	entries, err := m.List(ctx, repo)
	if err != nil {
		return WorktreeEntry{}, err
	}

	// Resolve both paths for consistent comparison (handles symlinks on macOS)
	absPath, _ := filepath.Abs(path)
	evalPath, _ := filepath.EvalSymlinks(absPath)

	for _, e := range entries {
		absEntry, _ := filepath.Abs(e.Path)
		evalEntry, _ := filepath.EvalSymlinks(absEntry)
		if absEntry == absPath || evalEntry == evalPath || evalEntry == absPath || absEntry == evalPath {
			return e, nil
		}
	}

	return WorktreeEntry{}, fmt.Errorf("status: worktree not found at %q", path)
}

// Prune removes stale worktree administrative data.
func (m *GitManager) Prune(ctx context.Context, repo string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := validateGitRepo(repo); err != nil {
		return fmt.Errorf("prune worktrees: %w", err)
	}

	_, err := m.runGitNoOutput(repo, "worktree", "prune")
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return structuredGitError("prune", nil, err)
	}

	return nil
}

// CopyFiles copies files from src to dst with optional include/exclude patterns.
// It preserves directory structure and handles dotfiles (e.g., .env*).
func (m *GitManager) CopyFiles(ctx context.Context, src string, dst string, opts ...Option) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	options := ApplyOptions(opts...)

	// Validate source exists
	srcInfo, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("copy files: source %q does not exist", src)
		}
		return fmt.Errorf("copy files: %w", err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("copy files: source %q is not a directory", src)
	}

	// Create destination if needed
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("copy files: creating destination: %w", err)
	}

	return copyDir(ctx, src, dst, options.Include, options.Exclude)
}

// copyDir recursively copies files from src to dst, applying include/exclude patterns.
func copyDir(ctx context.Context, src, dst string, include, exclude []string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check context cancellation
		if err := ctx.Err(); err != nil {
			return err
		}

		// Compute relative path
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}

		// Skip the root directory itself
		if rel == "." {
			return nil
		}

		dstPath := filepath.Join(dst, rel)

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			// Check if the symlink target should be copied
			if !shouldCopyFile(rel, include, exclude) {
				return nil
			}
			target, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("reading symlink: %w", err)
			}
			return os.Symlink(target, dstPath)
		}

		if info.IsDir() {
			// Check if the directory is excluded by a ** pattern
			if isDirExcluded(rel, exclude) {
				return filepath.SkipDir
			}
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Regular file: check include/exclude patterns
		if !shouldCopyFile(rel, include, exclude) {
			return nil
		}

		return copyFile(path, dstPath, info.Mode())
	})
}

// isDirExcluded checks if a directory path should be skipped entirely
// based on exclude patterns like "node_modules/**".
func isDirExcluded(rel string, exclude []string) bool {
	for _, pattern := range exclude {
		// Handle dir/** patterns
		if strings.HasSuffix(pattern, string(os.PathSeparator)+"**") {
			dirPart := strings.TrimSuffix(pattern, string(os.PathSeparator)+"**")
			if rel == dirPart || strings.HasPrefix(rel, dirPart+string(os.PathSeparator)) {
				return true
			}
		}
		// Handle **/dir patterns (any depth match)
		if strings.HasPrefix(pattern, "**"+string(os.PathSeparator)) {
			dirPart := strings.TrimPrefix(pattern, "**"+string(os.PathSeparator))
			if filepath.Base(rel) == dirPart {
				return true
			}
		}
	}
	return false
}

// shouldCopyFile determines if a file should be copied based on include/exclude patterns.
// rel is the relative path from the source root.
func shouldCopyFile(rel string, include, exclude []string) bool {
	// Base name for pattern matching (for top-level patterns like "*.go")
	baseName := filepath.Base(rel)

	// If exclude patterns match, skip
	for _, pattern := range exclude {
		// Match against both base name and relative path
		if matched, _ := filepath.Match(pattern, baseName); matched {
			return false
		}
		// Also try matching against the full relative path (for patterns like "node_modules/**")
		if matched, _ := filepath.Match(pattern, rel); matched {
			return false
		}
		// Handle dir/** patterns: check if any parent path component matches
		// e.g., "node_modules/**" should exclude "node_modules/pkg/index.js"
		dirPattern := filepath.Dir(pattern)
		if dirPattern != "." && strings.HasPrefix(rel, dirPattern+string(os.PathSeparator)) {
			return false
		}
		// Also match the directory name directly (e.g., "node_modules/**" should match any path under node_modules/)
		patternDir := strings.TrimSuffix(pattern, string(os.PathSeparator)+"**")
		if patternDir != pattern {
			// This is a dir/** pattern
			if strings.HasPrefix(rel, patternDir+string(os.PathSeparator)) || rel == patternDir {
				return false
			}
		}
	}

	// If no include patterns, include everything (that wasn't excluded)
	if len(include) == 0 {
		return true
	}

	// If include patterns are set, file must match at least one
	for _, pattern := range include {
		if matched, _ := filepath.Match(pattern, baseName); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, rel); matched {
			return true
		}
		// Handle dotfile patterns like ".env*"
		if strings.Contains(pattern, "*") {
			if matched, _ := filepath.Match(pattern, baseName); matched {
				return true
			}
		}
	}

	return false
}

// copyFile copies a single file from src to dst preserving permissions.
func copyFile(src, dst string, mode os.FileMode) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating parent directory: %w", err)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	if err := os.WriteFile(dst, data, mode); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

// --- Helper functions ---

// validateGitRepo checks that the given path is a git repository.
func validateGitRepo(path string) error {
	gitDir := filepath.Join(path, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%q is not a git repository", path)
		}
		return fmt.Errorf("checking git repo: %w", err)
	}
	// .git can be a directory or a file (worktree pointer)
	_ = info
	return nil
}

// resolveBaseDir determines the base directory for worktree creation.
// If customBase is set, it's used. Otherwise a sibling directory pattern is used.
func resolveBaseDir(repoPath, customBase string) string {
	if customBase != "" {
		return customBase
	}
	// Default: <repo-dir>-worktrees sibling
	base := filepath.Dir(repoPath)
	repoName := filepath.Base(repoPath)
	return filepath.Join(base, repoName+"-worktrees")
}

// getHeadCommit returns the HEAD commit SHA for a worktree/repo path.
func getHeadCommit(path string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = path
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// runGitOutput runs a git command and returns its stdout as a string.
func (m *GitManager) runGitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

// runGitNoOutput runs a git command and returns an error if it fails.
func (m *GitManager) runGitNoOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// structuredGitError converts raw git errors into structured errors.
func structuredGitError(operation string, output []byte, err error) error {
	raw := ""
	if output != nil {
		raw = strings.TrimSpace(string(output))
	}

	// Remove "fatal:" prefix and clean up
	msg := raw
	msg = strings.TrimPrefix(msg, "fatal: ")
	msg = strings.TrimSpace(msg)

	// Map known error patterns to structured messages
	switch {
	case strings.Contains(msg, "not a git repository"):
		return fmt.Errorf("%s: not a git repository", operation)
	case strings.Contains(msg, "already exists"):
		return fmt.Errorf("%s: branch already exists", operation)
	case strings.Contains(msg, "dirty") || strings.Contains(msg, "contains modified"):
		return fmt.Errorf("%s: worktree is dirty, use WithForce(true) to remove", operation)
	case strings.Contains(msg, "cannot be used"):
		return fmt.Errorf("%s: %s", operation, msg)
	default:
		if msg != "" {
			return fmt.Errorf("%s: %s", operation, msg)
		}
		return fmt.Errorf("%s: %w", operation, err)
	}
}
