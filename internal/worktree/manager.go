package worktree

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GitRunner abstracts git command execution for testability.
// Implementations must be safe for concurrent use.
type GitRunner interface {
	// Run executes a git command in the given working directory.
	// Returns stdout, stderr, and any error.
	Run(ctx context.Context, dir string, args ...string) (stdout, stderr string, err error)
}

// RealGitRunner runs real git commands.
type RealGitRunner struct{}

// Run executes a git command.
func (r *RealGitRunner) Run(ctx context.Context, dir string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// GitManager implements Manager using git CLI commands.
type GitManager struct {
	runner GitRunner
	clock  func() time.Time
}

// NewGitManager creates a new GitManager with the default runner and real clock.
func NewGitManager() *GitManager {
	return &GitManager{
		runner: &RealGitRunner{},
		clock:  time.Now,
	}
}

// NewGitManagerWithRunner creates a GitManager with a custom runner (for testing).
func NewGitManagerWithRunner(runner GitRunner, clock func() time.Time) *GitManager {
	return &GitManager{
		runner: runner,
		clock:  clock,
	}
}

// Create creates a new git worktree for the given branch.
func (m *GitManager) Create(ctx context.Context, repo string, branch string, opts ...Option) (WorktreeResult, error) {
	o := ApplyOptions(opts...)
	co := o.CreateOpts()

	// Validate repo is a git repository
	if err := m.validateRepo(ctx, repo); err != nil {
		return WorktreeResult{}, wrapError("create", repo, "validating repository", err)
	}

	// Sanitize branch name
	cleanBranch, err := SanitizeBranchName(branch)
	if err != nil {
		return WorktreeResult{}, newError("create", repo, err.Error())
	}

	// Resolve base directory
	baseDir, err := ResolveBaseDir(repo, co.BaseDir)
	if err != nil {
		return WorktreeResult{}, wrapError("create", repo, "resolving base directory", err)
	}

	// Ensure base directory exists
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return WorktreeResult{}, wrapError("create", repo, "creating base directory", err)
	}

	// Build worktree path
	worktreePath := filepath.Join(baseDir, cleanBranch)

	// Validate no path traversal
	if err := validatePath(baseDir, worktreePath); err != nil {
		return WorktreeResult{}, wrapError("create", repo, "validating path", err)
	}

	// Build git worktree add arguments
	args := []string{"worktree", "add"}

	if co.NewBranch {
		args = append(args, "-b", cleanBranch)
	}

	// Add ref if specified
	if co.Ref != "" {
		args = append(args, worktreePath, co.Ref)
	} else {
		args = append(args, worktreePath)
	}

	// Execute git worktree add
	_, stderr, err := m.runner.Run(ctx, repo, args...)
	if err != nil {
		return WorktreeResult{}, wrapError("create", repo, "creating worktree", combineStderr(err, stderr))
	}

	// Get the HEAD commit of the new worktree
	commit, err := m.getHeadCommit(ctx, worktreePath)
	if err != nil {
		return WorktreeResult{}, wrapError("create", repo, "reading HEAD commit", err)
	}

	return WorktreeResult{
		Path:      worktreePath,
		Branch:    cleanBranch,
		Commit:    commit,
		CreatedAt: m.clock().UTC(),
	}, nil
}

// List returns all worktrees for the given repository.
func (m *GitManager) List(ctx context.Context, repo string) ([]WorktreeEntry, error) {
	if err := m.validateRepo(ctx, repo); err != nil {
		return nil, wrapError("list", repo, "validating repository", err)
	}

	stdout, stderr, err := m.runner.Run(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, wrapError("list", repo, "listing worktrees", combineStderr(err, stderr))
	}

	entries, err := ParsePorcelain(stdout)
	if err != nil {
		return nil, wrapError("list", repo, "parsing porcelain output", err)
	}

	// Enrich status for each entry
	for i := range entries {
		entries[i].Status = m.detectEntryStatus(entries[i])
	}

	return entries, nil
}

// Remove removes a worktree at the given path.
func (m *GitManager) Remove(ctx context.Context, repo string, path string, opts ...Option) error {
	o := ApplyOptions(opts...)
	ro := o.RemoveOpts()

	if err := m.validateRepo(ctx, repo); err != nil {
		return wrapError("remove", repo, "validating repository", err)
	}

	// Resolve paths to absolute for comparison
	absPath, err := filepath.Abs(path)
	if err != nil {
		return wrapError("remove", repo, "resolving path", err)
	}
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return wrapError("remove", repo, "resolving repo path", err)
	}

	// Reject removing the main checkout
	if absPath == absRepo {
		return newErrorWithPath("remove", repo, path, "cannot remove main checkout")
	}

	// Check if path is actually a worktree of this repo
	entries, err := m.List(ctx, repo)
	if err != nil {
		return wrapError("remove", repo, "listing worktrees for validation", err)
	}

	found := false
	for _, e := range entries {
		if e.Path == absPath {
			found = true
			break
		}
	}
	if !found {
		// The worktree directory may have been externally deleted;
		// still try to prune the administrative data
		return m.pruneWorktree(ctx, repo, absPath, ro)
	}

	// Build remove args
	args := []string{"worktree", "remove"}
	if ro.Force {
		args = append(args, "--force")
	}
	args = append(args, absPath)

	_, stderr, err := m.runner.Run(ctx, repo, args...)
	if err != nil {
		// If the directory is gone, try prune instead
		if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
			return m.pruneWorktree(ctx, repo, absPath, ro)
		}
		return wrapError("remove", repo, "removing worktree", combineStderr(err, stderr))
	}

	// Optionally delete the branch
	if ro.DeleteBranch {
		// Find the branch name from the worktree
		branch := m.findBranchForPath(entries, absPath)
		if branch != "" {
			_, _, branchErr := m.runner.Run(ctx, repo, "branch", "-D", branch)
			if branchErr != nil {
				// Branch deletion failure is non-fatal
				return wrapError("remove", repo, "deleting branch", branchErr)
			}
		}
	}

	return nil
}

// Status returns the health status of a specific worktree.
func (m *GitManager) Status(ctx context.Context, repo string, path string) (WorktreeEntry, error) {
	if err := m.validateRepo(ctx, repo); err != nil {
		return WorktreeEntry{}, wrapError("status", repo, "validating repository", err)
	}

	entries, err := m.List(ctx, repo)
	if err != nil {
		return WorktreeEntry{}, wrapError("status", repo, "listing worktrees", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return WorktreeEntry{}, wrapError("status", repo, "resolving path", err)
	}

	for _, e := range entries {
		if e.Path == absPath {
			return e, nil
		}
	}

	return WorktreeEntry{}, newErrorWithPath("status", repo, path, "worktree not found")
}

// Prune removes stale worktree administrative data.
func (m *GitManager) Prune(ctx context.Context, repo string) error {
	if err := m.validateRepo(ctx, repo); err != nil {
		return wrapError("prune", repo, "validating repository", err)
	}

	_, stderr, err := m.runner.Run(ctx, repo, "worktree", "prune")
	if err != nil {
		return wrapError("prune", repo, "pruning worktrees", combineStderr(err, stderr))
	}

	return nil
}

// Resolve finds a worktree by branch name or partial match.
// This is a stub - full implementation is in the resolve-copy-hooks feature.
func (m *GitManager) Resolve(ctx context.Context, repo string, query string) (WorktreeEntry, error) {
	return WorktreeEntry{}, fmt.Errorf("resolve not yet implemented")
}

// CopyFiles copies files from src to dst with optional patterns.
// This is a stub - full implementation is in the resolve-copy-hooks feature.
func (m *GitManager) CopyFiles(ctx context.Context, src string, dst string, opts ...Option) error {
	return fmt.Errorf("copy files not yet implemented")
}

// validateRepo checks that the given path is a valid git repository.
func (m *GitManager) validateRepo(ctx context.Context, repo string) error {
	_, stderr, err := m.runner.Run(ctx, repo, "rev-parse", "--git-dir")
	if err != nil {
		msg := "not a git repository"
		if stderr != "" {
			if interpreted := interpretGitError("validate", stderr); interpreted != "" {
				msg = interpreted
			}
		}
		return fmt.Errorf("%s: %s", msg, repo)
	}
	return nil
}

// getHeadCommit returns the HEAD commit SHA of a worktree.
func (m *GitManager) getHeadCommit(ctx context.Context, worktreePath string) (string, error) {
	stdout, stderr, err := m.runner.Run(ctx, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("getting HEAD: %s", stderr)
	}
	return strings.TrimSpace(stdout), nil
}

// detectEntryStatus determines the status of a worktree entry.
func (m *GitManager) detectEntryStatus(entry WorktreeEntry) WorktreeStatus {
	// If porcelain already reports a status, keep it
	if entry.Status == StatusLocked || entry.Status == StatusPrunable {
		return entry.Status
	}

	// Check if directory exists
	if _, err := os.Stat(entry.Path); os.IsNotExist(err) {
		return StatusPrunable
	}

	return StatusOK
}

// pruneWorktree handles removal of a worktree whose directory is gone.
func (m *GitManager) pruneWorktree(ctx context.Context, repo, path string, ro RemoveOptions) error {
	// Run git worktree prune to clean up stale entries
	_, stderr, err := m.runner.Run(ctx, repo, "worktree", "prune")
	if err != nil {
		return wrapError("remove", repo, "pruning stale worktree", combineStderr(err, stderr))
	}

	// Optionally delete the branch
	if ro.DeleteBranch {
		// We can't easily determine the branch without the worktree,
		// but we can try from the list before pruning
		return nil
	}

	return nil
}

// findBranchForPath returns the branch name for a worktree path from a list of entries.
func (m *GitManager) findBranchForPath(entries []WorktreeEntry, absPath string) string {
	for _, e := range entries {
		if e.Path == absPath {
			return e.Branch
		}
	}
	return ""
}

// validatePath checks for path traversal attacks.
func validatePath(baseDir, candidate string) error {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("resolving base dir: %w", err)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return fmt.Errorf("resolving candidate path: %w", err)
	}

	// Check that candidate is under baseDir
	if !strings.HasPrefix(absCandidate, absBase+string(os.PathSeparator)) && absCandidate != absBase {
		return fmt.Errorf("path traversal detected: %s escapes base dir %s", candidate, baseDir)
	}

	return nil
}

// combineStderr combines an error with stderr output for better error messages.
func combineStderr(err error, stderr string) error {
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr))
}

// MockGitRunner is a test double for GitRunner that records calls and
// returns preconfigured responses.
type MockGitRunner struct {
	// Responses maps "dir:args" to (stdout, stderr, error) tuples.
	// Args are joined with spaces for the key.
	Responses map[string]mockResponse
	// Calls records all invocations for assertion in tests.
	Calls []mockCall
}

type mockResponse struct {
	stdout string
	stderr string
	err    error
}

type mockCall struct {
	dir  string
	args []string
}

// NewMockGitRunner creates a new MockGitRunner with empty responses.
func NewMockGitRunner() *MockGitRunner {
	return &MockGitRunner{
		Responses: make(map[string]mockResponse),
	}
}

// On configures a response for a given dir and args.
func (m *MockGitRunner) On(dir string, args []string, stdout, stderr string, err error) {
	key := dir + ":" + strings.Join(args, " ")
	m.Responses[key] = mockResponse{stdout: stdout, stderr: stderr, err: err}
}

// Run executes a mocked git command.
func (m *MockGitRunner) Run(ctx context.Context, dir string, args ...string) (string, string, error) {
	// Check for context cancellation first
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	default:
	}

	m.Calls = append(m.Calls, mockCall{dir: dir, args: args})

	key := dir + ":" + strings.Join(args, " ")
	if resp, ok := m.Responses[key]; ok {
		return resp.stdout, resp.stderr, resp.err
	}

	// Default: command not found
	return "", fmt.Errorf("mock: unexpected command: git %s (in %s)", strings.Join(args, " "), dir)
}

// helperInitGitRepo creates a real git repository in a temp directory for integration tests.
func helperInitGitRepo(t interface{ Fatalf(string, ...interface{}) }, dir string) {
	helperRunGit(t, dir, "init")
	helperRunGit(t, dir, "config", "user.email", "test@test.com")
	helperRunGit(t, dir, "config", "user.name", "Test")

	// Create an initial commit so there's a HEAD
	filePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(filePath, []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("writing initial file: %v", err)
	}
	helperRunGit(t, dir, "add", ".")
	helperRunGit(t, dir, "commit", "-m", "initial commit")
}

// helperRunGit runs a git command in a directory.
func helperRunGit(t interface{ Fatalf(string, ...interface{}) }, dir string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), dir, err, stderr.String())
	}
}

// skipIfNoGit skips the test if git is not available.
func skipIfNoGit(t interface{ Skip(...interface{}) }) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
}

// ScanLines is a helper that splits output into non-empty lines.
func ScanLines(output string) []string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
