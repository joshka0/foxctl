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

	return WorktreeResult{
		Path:      resolvedPath,
		Branch:    cleanBranch,
		Commit:    commit,
		CreatedAt: time.Now().UTC(),
	}, nil
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
func (m *GitManager) CopyFiles(ctx context.Context, src string, dst string, opts ...Option) error {
	// This will be implemented in a later feature (worktree-resolve-copy-hooks)
	return fmt.Errorf("CopyFiles: not yet implemented")
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
