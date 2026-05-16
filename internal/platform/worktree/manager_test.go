package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helperInitGitRepo creates a temp git repo with an initial commit.
// Returns the repo path and a cleanup function.
func helperInitGitRepo(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "myrepo")

	require.NoError(t, os.MkdirAll(repoPath, 0o755))

	// git init with an explicit primary branch so tests don't depend on the
	// runner's global init.defaultBranch setting.
	require.NoError(t, runGit(repoPath, "init", "-b", "main"))
	// configure user for commits
	require.NoError(t, runGit(repoPath, "config", "user.email", "test@test.com"))
	require.NoError(t, runGit(repoPath, "config", "user.name", "Test"))

	// Create initial commit
	readmePath := filepath.Join(repoPath, "README.md")
	require.NoError(t, os.WriteFile(readmePath, []byte("# Test Repo\n"), 0o644))
	require.NoError(t, runGit(repoPath, "add", "."))
	require.NoError(t, runGit(repoPath, "commit", "-m", "initial commit"))

	return repoPath, func() {}
}

// runGit executes a git command in the given directory.
func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// helperGetHEAD returns the HEAD commit SHA for a repo.
func helperGetHEAD(t *testing.T, repoPath string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

// helperBranchExists checks if a branch exists in the repo.
func helperBranchExists(t *testing.T, repoPath, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "branch", "--list", branch)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out)) != ""
}

func TestCreate_NewBranch(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(
		context.Background(), repoPath, "feat/x",
		WithNewBranch(true),
	)
	require.NoError(t, err)

	assert.NotEmpty(t, result.Path)
	assert.Equal(t, "feat/x", result.Branch)
	assert.NotEmpty(t, result.Commit)
	assert.False(t, result.CreatedAt.IsZero())

	// Verify directory exists
	info, err := os.Stat(result.Path)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify .git file points to main repo
	gitFile := filepath.Join(result.Path, ".git")
	data, err := os.ReadFile(gitFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), repoPath)

	// Verify branch via git
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = result.Path
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "feat/x", strings.TrimSpace(string(out)))

	// Clean up worktree
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestCreate_ExistingBranch(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	// Pre-create a branch
	require.NoError(t, runGit(repoPath, "branch", "feat/existing"))

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/existing")
	require.NoError(t, err)

	assert.Equal(t, "feat/existing", result.Branch)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestCreate_FromRef(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	// Create a tag
	require.NoError(t, runGit(repoPath, "tag", "v1.0"))
	tagCommit := helperGetHEAD(t, repoPath)

	// Make another commit so HEAD != v1.0
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "extra.txt"), []byte("extra\n"), 0o644))
	require.NoError(t, runGit(repoPath, "add", "."))
	require.NoError(t, runGit(repoPath, "commit", "-m", "second commit"))

	mgr := NewManager()
	result, err := mgr.Create(
		context.Background(), repoPath, "feat/from-tag",
		WithNewBranch(true),
		WithRef("v1.0"),
	)
	require.NoError(t, err)

	assert.Equal(t, tagCommit, result.Commit)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestCreate_CustomBaseDir(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	baseDir := t.TempDir()
	// Resolve symlinks for consistent path comparison
	resolvedBaseDir, err := filepath.EvalSymlinks(baseDir)
	require.NoError(t, err)

	mgr := NewManager()
	result, err := mgr.Create(
		context.Background(), repoPath, "feat/custom",
		WithNewBranch(true),
		WithBaseDir(baseDir),
	)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(result.Path, resolvedBaseDir),
		"worktree path %q should be under base dir %q", result.Path, resolvedBaseDir)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestCreate_DefaultBaseDir(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(
		context.Background(), repoPath, "feat/default-basedir",
		WithNewBranch(true),
	)
	require.NoError(t, err)

	// Worktree should NOT be a subdirectory of the repo
	assert.False(t, strings.HasPrefix(result.Path, repoPath+string(os.PathSeparator)),
		"worktree path %q should not be under repo %q", result.Path, repoPath)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestCreate_NonRepoDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := NewManager()
	_, err := mgr.Create(context.Background(), tmpDir, "feat/fail", WithNewBranch(true))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}

func TestCreate_DuplicateBranch(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	// Create first worktree with new branch
	result, err := mgr.Create(
		context.Background(), repoPath, "feat/dup",
		WithNewBranch(true),
	)
	require.NoError(t, err)

	// Try creating again with same branch name + NewBranch
	_, err = mgr.Create(
		context.Background(), repoPath, "feat/dup",
		WithNewBranch(true),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestCreate_ContextCancellation(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	mgr := NewManager()
	_, err := mgr.Create(ctx, repoPath, "feat/cancelled", WithNewBranch(true))
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestCreate_PathValidation(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	_, err := mgr.Create(
		context.Background(), repoPath, "../../../etc/passwd",
		WithNewBranch(true),
	)
	require.Error(t, err)
	// Should reject path traversal
	assert.Contains(t, err.Error(), "invalid")
}

// --- List Tests ---

func TestList_AllWorktrees(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()

	// Create two worktrees
	r1, err := mgr.Create(context.Background(), repoPath, "feat/a", WithNewBranch(true))
	require.NoError(t, err)
	r2, err := mgr.Create(context.Background(), repoPath, "feat/b", WithNewBranch(true))
	require.NoError(t, err)

	entries, err := mgr.List(context.Background(), repoPath)
	require.NoError(t, err)

	// Should have 3: main + 2 worktrees
	assert.Len(t, entries, 3)

	// Find the main entry
	var mainEntry *WorktreeEntry
	evalRepoPath, _ := filepath.EvalSymlinks(repoPath)
	for i := range entries {
		evalEntry, _ := filepath.EvalSymlinks(entries[i].Path)
		if entries[i].Path == repoPath || evalEntry == evalRepoPath || entries[i].Branch == "main" || entries[i].Branch == "master" {
			mainEntry = &entries[i]
			break
		}
	}
	require.NotNil(t, mainEntry, "main checkout should be listed, entries: %v", entries)
	assert.Equal(t, "main", mainEntry.Branch)

	// Verify our worktrees are in the list
	// Use EvalSymlinks for consistent path comparison
	evalR1, _ := filepath.EvalSymlinks(r1.Path)
	evalR2, _ := filepath.EvalSymlinks(r2.Path)
	found1, found2 := false, false
	for _, e := range entries {
		evalE, _ := filepath.EvalSymlinks(e.Path)
		if evalE == evalR1 || e.Path == r1.Path {
			found1 = true
		}
		if evalE == evalR2 || e.Path == r2.Path {
			found2 = true
		}
	}
	assert.True(t, found1, "worktree r1 path %q should be in list", r1.Path)
	assert.True(t, found2, "worktree r2 path %q should be in list", r2.Path)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, r1.Path, WithForce(true), WithDeleteBranch(true))
	_ = mgr.Remove(context.Background(), repoPath, r2.Path, WithForce(true), WithDeleteBranch(true))
}

func TestList_ContextMenuCancellation(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr := NewManager()
	_, err := mgr.List(ctx, repoPath)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// --- Status Tests ---

func TestStatus_OK(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/status-ok", WithNewBranch(true))
	require.NoError(t, err)

	entry, err := mgr.Status(context.Background(), repoPath, result.Path)
	require.NoError(t, err)
	assert.Equal(t, StatusOK, entry.Status)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestStatus_Locked(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/locked", WithNewBranch(true))
	require.NoError(t, err)

	// Lock the worktree
	require.NoError(t, runGit(repoPath, "worktree", "lock", result.Path))

	entry, err := mgr.Status(context.Background(), repoPath, result.Path)
	require.NoError(t, err)
	assert.Equal(t, StatusLocked, entry.Status)

	// Clean up
	_ = runGit(repoPath, "worktree", "unlock", result.Path)
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestStatus_Prunable(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "prunable-test", WithNewBranch(true))
	require.NoError(t, err)

	// Delete the worktree directory externally
	require.NoError(t, os.RemoveAll(result.Path))

	entry, err := mgr.Status(context.Background(), repoPath, result.Path)
	require.NoError(t, err)
	assert.Equal(t, StatusPrunable, entry.Status)

	// Prune to clean up
	_ = mgr.Prune(context.Background(), repoPath)
}

// --- Remove Tests ---

func TestRemove_CleanWorktree(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/remove", WithNewBranch(true))
	require.NoError(t, err)

	wtPath := result.Path
	require.NoError(t, mgr.Remove(context.Background(), repoPath, wtPath))

	// Verify directory is gone
	_, err = os.Stat(wtPath)
	assert.True(t, os.IsNotExist(err), "worktree directory should be removed")

	// Verify it's no longer listed
	entries, err := mgr.List(context.Background(), repoPath)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, wtPath, e.Path)
	}
}

func TestRemove_DirtyWithoutForce(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/dirty", WithNewBranch(true))
	require.NoError(t, err)

	// Make dirty: modify a tracked file
	require.NoError(t, os.WriteFile(filepath.Join(result.Path, "README.md"), []byte("dirty!\n"), 0o644))

	err = mgr.Remove(context.Background(), repoPath, result.Path)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "dirty") || strings.Contains(err.Error(), "force"),
		"error should mention dirty or force: %v", err)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestRemove_DirtyWithForce(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/force-remove", WithNewBranch(true))
	require.NoError(t, err)

	// Make dirty
	require.NoError(t, os.WriteFile(filepath.Join(result.Path, "README.md"), []byte("dirty!\n"), 0o644))

	// Force remove should succeed
	require.NoError(t, mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true)))

	_, err = os.Stat(result.Path)
	assert.True(t, os.IsNotExist(err))
}

func TestRemove_WithDeleteBranch(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/del-branch", WithNewBranch(true))
	require.NoError(t, err)

	// Verify branch exists
	assert.True(t, helperBranchExists(t, repoPath, "feat/del-branch"))

	require.NoError(t, mgr.Remove(context.Background(), repoPath, result.Path, WithDeleteBranch(true)))

	// Branch should be gone
	assert.False(t, helperBranchExists(t, repoPath, "feat/del-branch"))
}

func TestRemove_PreservesBranchByDefault(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/keep-branch", WithNewBranch(true))
	require.NoError(t, err)

	require.NoError(t, mgr.Remove(context.Background(), repoPath, result.Path))

	// Branch should still exist
	assert.True(t, helperBranchExists(t, repoPath, "feat/keep-branch"))
}

func TestRemove_OrphanedAdminData(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/orphan", WithNewBranch(true))
	require.NoError(t, err)

	wtPath := result.Path
	// Delete directory externally
	require.NoError(t, os.RemoveAll(wtPath))

	// Remove should still succeed (clean up stale git data)
	require.NoError(t, mgr.Remove(context.Background(), repoPath, wtPath))

	// Verify it's no longer listed
	entries, err := mgr.List(context.Background(), repoPath)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, wtPath, e.Path)
	}
}

func TestRemove_RejectsMainCheckout(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	err := mgr.Remove(context.Background(), repoPath, repoPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "main checkout")

	// Verify repo is still intact
	assert.True(t, helperBranchExists(t, repoPath, "main"))
}

func TestRemove_ContextMenuCancellation(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/cancel-rm", WithNewBranch(true))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = mgr.Remove(ctx, repoPath, result.Path)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

// --- Prune Tests ---

func TestPrune_RemovesStaleEntries(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	r1, err := mgr.Create(context.Background(), repoPath, "feat/keep", WithNewBranch(true))
	require.NoError(t, err)
	r2, err := mgr.Create(context.Background(), repoPath, "feat/prune-me", WithNewBranch(true))
	require.NoError(t, err)

	// Delete one worktree directory externally
	require.NoError(t, os.RemoveAll(r2.Path))

	// Before prune: should have 3 entries (main + 2 worktrees, one prunable)
	entries, err := mgr.List(context.Background(), repoPath)
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	// Prune
	require.NoError(t, mgr.Prune(context.Background(), repoPath))

	// After prune: should have 2 entries (main + 1 worktree)
	entries, err = mgr.List(context.Background(), repoPath)
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	// Clean up remaining worktree
	_ = mgr.Remove(context.Background(), repoPath, r1.Path, WithForce(true), WithDeleteBranch(true))
}

func TestPrune_ContextMenuCancellation(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr := NewManager()
	err := mgr.Prune(ctx, repoPath)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// --- Concurrent Tests ---

func TestCreate_ConcurrentDifferentBranches(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()

	type createResult struct {
		result WorktreeResult
		err    error
	}

	results := make(chan createResult, 2)

	go func() {
		r, err := mgr.Create(context.Background(), repoPath, "feat/concurrent-a", WithNewBranch(true))
		results <- createResult{result: r, err: err}
	}()
	go func() {
		r, err := mgr.Create(context.Background(), repoPath, "feat/concurrent-b", WithNewBranch(true))
		results <- createResult{result: r, err: err}
	}()

	r1 := <-results
	r2 := <-results

	require.NoError(t, r1.err)
	require.NoError(t, r2.err)
	assert.NotEqual(t, r1.result.Path, r2.result.Path)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, r1.result.Path, WithForce(true), WithDeleteBranch(true))
	_ = mgr.Remove(context.Background(), repoPath, r2.result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestCreate_NonRepoReturnsStructuredError(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager()
	_, err := mgr.Create(context.Background(), tmpDir, "feat/fail", WithNewBranch(true))
	require.Error(t, err)
	// Error should be structured, not raw git stderr
	assert.NotContains(t, err.Error(), "fatal:")
}

func TestResolveBaseDir(t *testing.T) {
	tests := []struct {
		name         string
		repoPath     string
		customBase   string
		expectedFunc func(t *testing.T, repoPath, result string)
	}{
		{
			name:       "custom base dir",
			repoPath:   "/some/repo",
			customBase: "/tmp/worktrees",
			expectedFunc: func(t *testing.T, repoPath, result string) {
				assert.Equal(t, "/tmp/worktrees", result)
			},
		},
		{
			name:       "default: sibling pattern",
			repoPath:   "/some/repo",
			customBase: "",
			expectedFunc: func(t *testing.T, repoPath, result string) {
				// Should be sibling-like, not a subdirectory of repo
				assert.False(t, strings.HasPrefix(result, "/some/repo/"))
				assert.Contains(t, result, "repo")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := resolveBaseDir(tc.repoPath, tc.customBase)
			tc.expectedFunc(t, tc.repoPath, result)
		})
	}
}

// TestCreate_ValidatesBranchName tests that Create rejects invalid branch names
func TestCreate_ValidatesBranchName(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	_, err := mgr.Create(context.Background(), repoPath, "...", WithNewBranch(true))
	require.Error(t, err)
}

// TestCreate_SanitizesBranchName tests that Create auto-sanitizes branch names
func TestCreate_SanitizesBranchName(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/my cool feature~", WithNewBranch(true))
	require.NoError(t, err)
	assert.Equal(t, "feat/my-cool-feature", result.Branch)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

// Ensure createdAt is approximately now
func TestCreate_CreatedAtIsRecent(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	before := time.Now().Add(-5 * time.Second)
	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/time-test", WithNewBranch(true))
	require.NoError(t, err)
	after := time.Now().Add(5 * time.Second)

	assert.True(t, result.CreatedAt.After(before), "created_at should be after test start")
	assert.True(t, result.CreatedAt.Before(after), "created_at should be before test end")

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

// TestRemove_DeleteBranchThatDoesNotExist tests that delete-branch succeeds even if branch already gone
func TestRemove_DeleteBranchAlreadyGone(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/gone-branch", WithNewBranch(true))
	require.NoError(t, err)

	// Delete the branch manually first
	_ = runGit(repoPath, "branch", "-D", "feat/gone-branch")

	// Remove with delete-branch should still succeed
	require.NoError(t, mgr.Remove(context.Background(), repoPath, result.Path, WithDeleteBranch(true)))
}
