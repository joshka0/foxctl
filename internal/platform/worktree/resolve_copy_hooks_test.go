package worktree

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Resolve Tests (VAL-WT-021 through VAL-WT-024)
// ============================================================================

func TestResolve_ByBranchName(t *testing.T) {
	// VAL-WT-021: Resolve by branch name returns correct path
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/x", WithNewBranch(true))
	require.NoError(t, err)

	entry, err := mgr.Resolve(context.Background(), repoPath, "feat/x")
	require.NoError(t, err)

	assert.Equal(t, result.Path, entry.Path)
	assert.Equal(t, "feat/x", entry.Branch)
	assert.NotEmpty(t, entry.Commit)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestResolve_ByPartialBranchName(t *testing.T) {
	// VAL-WT-022: Resolve by partial branch name uses suffix matching
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/abc-123", WithNewBranch(true))
	require.NoError(t, err)

	// Suffix match: "abc-123" matches "feat/abc-123"
	entry, err := mgr.Resolve(context.Background(), repoPath, "abc-123")
	require.NoError(t, err)

	assert.Equal(t, result.Path, entry.Path)
	assert.Equal(t, "feat/abc-123", entry.Branch)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestResolve_SpecialOneReturnsMain(t *testing.T) {
	// VAL-WT-023: Resolve "1" returns main repo
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	entry, err := mgr.Resolve(context.Background(), repoPath, "1")
	require.NoError(t, err)

	// Should return the main checkout
	evalRepoPath, _ := filepath.EvalSymlinks(repoPath)
	evalEntryPath, _ := filepath.EvalSymlinks(entry.Path)
	assert.True(t, entry.Path == repoPath || evalEntryPath == evalRepoPath,
		"expected main repo path %q but got %q", repoPath, entry.Path)
}

func TestResolve_AmbiguousPartialReturnsError(t *testing.T) {
	// VAL-WT-024: Ambiguous partial match returns error listing candidates
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	r1, err := mgr.Create(context.Background(), repoPath, "feat/test", WithNewBranch(true))
	require.NoError(t, err)
	r2, err := mgr.Create(context.Background(), repoPath, "fix/test", WithNewBranch(true))
	require.NoError(t, err)

	// Both "feat/test" and "fix/test" have suffix "test"
	_, err = mgr.Resolve(context.Background(), repoPath, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
	assert.Contains(t, err.Error(), "feat/test")
	assert.Contains(t, err.Error(), "fix/test")

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, r1.Path, WithForce(true), WithDeleteBranch(true))
	_ = mgr.Remove(context.Background(), repoPath, r2.Path, WithForce(true), WithDeleteBranch(true))
}

func TestResolve_NoMatchReturnsError(t *testing.T) {
	repoPath, _ := helperInitGitRepo(t)
	// No need to defer cleanup; t.TempDir() is auto-cleaned

	mgr := NewManager()
	_, err := mgr.Resolve(context.Background(), repoPath, "nonexistent-branch")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no worktree matches")
}

func TestResolve_ByPath(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/path-test", WithNewBranch(true))
	require.NoError(t, err)

	// Resolve by exact path
	entry, err := mgr.Resolve(context.Background(), repoPath, result.Path)
	require.NoError(t, err)
	assert.Equal(t, result.Path, entry.Path)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestResolve_ContextMenuCancellation(t *testing.T) {
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr := NewManager()
	_, err := mgr.Resolve(ctx, repoPath, "anything")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// ============================================================================
// CopyFiles Tests (VAL-WT-025 through VAL-WT-029)
// ============================================================================

func TestCopyFiles_IncludePatterns(t *testing.T) {
	// VAL-WT-025: CopyFiles with include patterns

	mgr := NewManager()

	// Create source files of different types
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module test\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "readme.md"), []byte("# Test\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "main_test.go"), []byte("package main\n"), 0o644))

	dstDir := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	err := mgr.CopyFiles(context.Background(), srcDir, dstDir,
		WithInclude("*.go", "*.mod"),
	)
	require.NoError(t, err)

	// Only Go and mod files should be copied
	assert.FileExists(t, filepath.Join(dstDir, "main.go"))
	assert.FileExists(t, filepath.Join(dstDir, "go.mod"))
	assert.FileExists(t, filepath.Join(dstDir, "main_test.go"))
	assert.NoFileExists(t, filepath.Join(dstDir, "readme.md"))
}

func TestCopyFiles_ExcludePatterns(t *testing.T) {
	// VAL-WT-026: CopyFiles with exclude patterns
	srcDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "node_modules", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "app.ts"), []byte("app\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "app.test.ts"), []byte("test\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "node_modules", "pkg", "index.js"), []byte("js\n"), 0o644))

	dstDir := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	mgr := NewManager()
	err := mgr.CopyFiles(context.Background(), srcDir, dstDir,
		WithExclude("*.test.ts", "node_modules/**"),
	)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dstDir, "app.ts"))
	assert.NoFileExists(t, filepath.Join(dstDir, "app.test.ts"))
	assert.NoDirExists(t, filepath.Join(dstDir, "node_modules"))
}

func TestCopyFiles_CombinedIncludeExclude(t *testing.T) {
	// VAL-WT-027: Include *.go but exclude *_test.go
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "main_test.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "readme.md"), []byte("# Test\n"), 0o644))

	dstDir := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	mgr := NewManager()
	err := mgr.CopyFiles(context.Background(), srcDir, dstDir,
		WithInclude("*.go"),
		WithExclude("*_test.go"),
	)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dstDir, "main.go"))
	assert.NoFileExists(t, filepath.Join(dstDir, "main_test.go"))
	assert.NoFileExists(t, filepath.Join(dstDir, "readme.md"))
}

func TestCopyFiles_Dotfiles(t *testing.T) {
	// VAL-WT-028: Copy .env dotfiles correctly
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".env"), []byte("KEY=val\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".env.local"), []byte("LOCAL=1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".env.production"), []byte("PROD=1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "config.yaml"), []byte("key: val\n"), 0o644))

	dstDir := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	mgr := NewManager()
	err := mgr.CopyFiles(context.Background(), srcDir, dstDir,
		WithInclude(".env*"),
	)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dstDir, ".env"))
	assert.FileExists(t, filepath.Join(dstDir, ".env.local"))
	assert.FileExists(t, filepath.Join(dstDir, ".env.production"))
	assert.NoFileExists(t, filepath.Join(dstDir, "config.yaml"))
}

func TestCopyFiles_PreservesNestedDirectoryStructure(t *testing.T) {
	// VAL-WT-029: Nested dirs are preserved
	srcDir := t.TempDir()
	nestedPath := filepath.Join(srcDir, "a", "b", "c")
	require.NoError(t, os.MkdirAll(nestedPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nestedPath, "file.txt"), []byte("nested\n"), 0o644))
	// Also create a file at a shallower level
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a", "root.txt"), []byte("root\n"), 0o644))

	dstDir := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	mgr := NewManager()
	err := mgr.CopyFiles(context.Background(), srcDir, dstDir)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dstDir, "a", "b", "c", "file.txt"))
	assert.FileExists(t, filepath.Join(dstDir, "a", "root.txt"))

	// Verify content
	data, err := os.ReadFile(filepath.Join(dstDir, "a", "b", "c", "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "nested\n", string(data))
}

func TestCopyFiles_EmptySrcReturnsError(t *testing.T) {
	// Empty/nonexistent src should return error
	mgr := NewManager()
	dstDir := filepath.Join(t.TempDir(), "dst")
	err := mgr.CopyFiles(context.Background(), "/nonexistent/path", dstDir)
	require.Error(t, err)
}

func TestCopyFiles_Symlinks(t *testing.T) {
	// Symlinks should be copied (as symlinks or as regular files)
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not reliable on Windows")
	}

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "real.txt"), []byte("real\n"), 0o644))
	require.NoError(t, os.Symlink("real.txt", filepath.Join(srcDir, "link.txt")))

	dstDir := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	mgr := NewManager()
	err := mgr.CopyFiles(context.Background(), srcDir, dstDir)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dstDir, "real.txt"))
	// The symlink should be resolved/copied so the content is accessible
	linkPath := filepath.Join(dstDir, "link.txt")
	assert.FileExists(t, linkPath)
}

func TestCopyFiles_NoPatternsCopiesAll(t *testing.T) {
	// No include/exclude → copy everything
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file2.go"), []byte("2\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sub", "file3.md"), []byte("3\n"), 0o644))

	dstDir := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	mgr := NewManager()
	err := mgr.CopyFiles(context.Background(), srcDir, dstDir)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dstDir, "file1.txt"))
	assert.FileExists(t, filepath.Join(dstDir, "file2.go"))
	assert.FileExists(t, filepath.Join(dstDir, "sub", "file3.md"))
}

func TestCopyFiles_ContextMenuCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr := NewManager()
	err := mgr.CopyFiles(ctx, "/tmp/any", "/tmp/any-dst")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// ============================================================================
// Hooks Tests (VAL-WT-030 through VAL-WT-033)
// ============================================================================

func TestHooks_PostCreateEnvVars(t *testing.T) {
	// VAL-WT-030: PostCreate hook receives correct env vars
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	// Create a hook script that writes env vars to a file
	hookOutputFile := filepath.Join(t.TempDir(), "hook_output.txt")
	hookScript := filepath.Join(t.TempDir(), "post_create.sh")
	hookContent := "#!/bin/sh\nenv | grep WORKTREE_ > " + hookOutputFile + "\n"
	require.NoError(t, os.WriteFile(hookScript, []byte(hookContent), 0o755))

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/hook-test",
		WithNewBranch(true),
		WithHooks(HookConfig{
			PostCreate: hookScript,
			Timeout:    10 * time.Second,
		}),
	)
	require.NoError(t, err)

	// Verify the hook ran and wrote env vars
	data, err := os.ReadFile(hookOutputFile)
	require.NoError(t, err, "hook output file should exist")

	hookOutput := string(data)
	assert.Contains(t, hookOutput, "WORKTREE_PATH=")
	assert.Contains(t, hookOutput, "WORKTREE_BRANCH=feat/hook-test")
	assert.Contains(t, hookOutput, "WORKTREE_REPO_PATH=")
	assert.Contains(t, hookOutput, "WORKTREE_COMMIT=")

	// Verify the worktree still exists
	assert.DirExists(t, result.Path)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestHooks_PostCreateFailureDoesNotRollback(t *testing.T) {
	// VAL-WT-031: Hook failure does not roll back worktree creation
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/hook-fail",
		WithNewBranch(true),
		WithHooks(HookConfig{
			PostCreate: "sh -c 'exit 1'",
			Timeout:    5 * time.Second,
		}),
	)
	// Should return a wrapped error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook")

	// But worktree should still exist on disk
	assert.DirExists(t, result.Path)
	assert.NotEmpty(t, result.Path)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestHooks_PostRemoveExecutesAfterRemoval(t *testing.T) {
	// VAL-WT-032: PostRemove hook runs after directory is gone
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	// Create a hook script that checks if path does NOT exist
	hookOutputFile := filepath.Join(t.TempDir(), "hook_output.txt")
	hookScript := filepath.Join(t.TempDir(), "post_remove.sh")
	hookContent := "#!/bin/sh\necho \"WORKTREE_PATH=$WORKTREE_PATH\" > " + hookOutputFile + "\n"
	hookContent += "if [ -d \"$WORKTREE_PATH\" ]; then echo 'STILL_EXISTS' >> " + hookOutputFile + "; else echo 'REMOVED' >> " + hookOutputFile + "; fi\n"
	require.NoError(t, os.WriteFile(hookScript, []byte(hookContent), 0o755))

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/hook-rm",
		WithNewBranch(true),
	)
	require.NoError(t, err)

	wtPath := result.Path

	// Remove with post-remove hook
	err = mgr.Remove(context.Background(), repoPath, wtPath,
		WithForce(true),
		WithDeleteBranch(true),
		WithHooks(HookConfig{
			PostRemove: hookScript,
			Timeout:    10 * time.Second,
		}),
	)
	require.NoError(t, err)

	// Verify the hook ran
	data, err := os.ReadFile(hookOutputFile)
	require.NoError(t, err, "hook output file should exist")

	hookOutput := string(data)
	assert.Contains(t, hookOutput, "WORKTREE_PATH="+wtPath)
	assert.Contains(t, hookOutput, "REMOVED")
	assert.NotContains(t, hookOutput, "STILL_EXISTS")
}

func TestHooks_TimeoutEnforcement(t *testing.T) {
	// VAL-WT-033: Hook exceeding timeout is killed
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	start := time.Now()
	result, err := mgr.Create(context.Background(), repoPath, "feat/hook-timeout",
		WithNewBranch(true),
		WithHooks(HookConfig{
			PostCreate: "sleep 60",
			Timeout:    1 * time.Second,
		}),
	)
	elapsed := time.Since(start)

	// Should error due to timeout
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook")

	// Should complete within reasonable time (timeout + overhead)
	assert.Less(t, elapsed, 5*time.Second, "hook should be killed within timeout + overhead")

	// Worktree should still exist (operation succeeded, hook failed)
	assert.DirExists(t, result.Path)

	// Clean up
	_ = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
}

func TestHooks_NoHookConfiguredIsNoop(t *testing.T) {
	// When no hooks are configured, Create/Remove should work normally
	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/no-hook",
		WithNewBranch(true),
		// No hooks configured
	)
	require.NoError(t, err)
	assert.DirExists(t, result.Path)

	err = mgr.Remove(context.Background(), repoPath, result.Path, WithForce(true), WithDeleteBranch(true))
	require.NoError(t, err)
	assert.NoDirExists(t, result.Path)
}

func TestHooks_PostRemoveFailureDoesNotRollback(t *testing.T) {
	// Hook failure on remove should not prevent cleanup from being considered done
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/hook-rm-fail",
		WithNewBranch(true),
	)
	require.NoError(t, err)

	wtPath := result.Path

	// Remove with failing hook
	err = mgr.Remove(context.Background(), repoPath, wtPath,
		WithForce(true),
		WithDeleteBranch(true),
		WithHooks(HookConfig{
			PostRemove: "sh -c 'exit 1'",
			Timeout:    5 * time.Second,
		}),
	)
	// Should return a wrapped error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook")

	// But directory should be gone
	assert.NoDirExists(t, wtPath)
}

func TestHooks_DefaultTimeout(t *testing.T) {
	// Verify the default timeout is 30s
	h := HookConfig{}
	assert.Equal(t, DefaultHookTimeout, h.EffectiveTimeout())
	assert.Equal(t, 30*time.Second, h.EffectiveTimeout())
}

// ============================================================================
// RunHooks unit tests (pure function tests)
// ============================================================================

func TestRunHook_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	ctx := context.Background()
	err := RunHook(ctx, "echo hello", map[string]string{
		"TEST_VAR": "test_value",
	}, 5*time.Second)
	require.NoError(t, err)
}

func TestRunHook_Failure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	ctx := context.Background()
	err := RunHook(ctx, "sh -c 'exit 42'", map[string]string{
		"TEST_VAR": "test_value",
	}, 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook")
}

func TestRunHook_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	ctx := context.Background()
	start := time.Now()
	err := RunHook(ctx, "sleep 60", map[string]string{}, 1*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook")
	assert.Less(t, elapsed, 3*time.Second, "should be killed well before 3s")
}

func TestRunHook_EnvVarsInjected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	outFile := filepath.Join(t.TempDir(), "env_output.txt")
	script := "env > " + outFile

	ctx := context.Background()
	err := RunHook(ctx, script, map[string]string{
		"MY_VAR_1": "value1",
		"MY_VAR_2": "value2",
	}, 5*time.Second)
	require.NoError(t, err)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)

	output := string(data)
	assert.Contains(t, output, "MY_VAR_1=value1")
	assert.Contains(t, output, "MY_VAR_2=value2")
}

func TestRunHook_EmptyCommandIsNoop(t *testing.T) {
	ctx := context.Background()
	err := RunHook(ctx, "", nil, 5*time.Second)
	require.NoError(t, err)
}

func TestRunHook_ContextCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RunHook(ctx, "echo hello", nil, 5*time.Second)
	require.Error(t, err)
}

// TestHooks_Integration tests the full lifecycle with hooks
func TestHooks_Integration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not supported on Windows")
	}

	repoPath, cleanup := helperInitGitRepo(t)
	defer cleanup()

	hookDir := t.TempDir()
	postCreateLog := filepath.Join(hookDir, "post_create.log")
	postRemoveLog := filepath.Join(hookDir, "post_remove.log")

	postCreateScript := "#!/bin/sh\necho \"branch=$WORKTREE_BRANCH path=$WORKTREE_PATH\" > " + postCreateLog + "\n"
	postRemoveScript := "#!/bin/sh\necho \"removed=$WORKTREE_PATH\" > " + postRemoveLog + "\n"

	require.NoError(t, os.WriteFile(filepath.Join(hookDir, "post_create.sh"), []byte(postCreateScript), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hookDir, "post_remove.sh"), []byte(postRemoveScript), 0o755))

	mgr := NewManager()
	result, err := mgr.Create(context.Background(), repoPath, "feat/integration",
		WithNewBranch(true),
		WithHooks(HookConfig{
			PostCreate: filepath.Join(hookDir, "post_create.sh"),
			PostRemove: filepath.Join(hookDir, "post_remove.sh"),
			Timeout:    10 * time.Second,
		}),
	)
	require.NoError(t, err)

	// Verify post-create hook ran
	data, err := os.ReadFile(postCreateLog)
	require.NoError(t, err)
	assert.Contains(t, string(data), "branch=feat/integration")
	assert.Contains(t, string(data), "path="+result.Path)

	// Remove with hooks
	err = mgr.Remove(context.Background(), repoPath, result.Path,
		WithForce(true),
		WithDeleteBranch(true),
		WithHooks(HookConfig{
			PostRemove: filepath.Join(hookDir, "post_remove.sh"),
			Timeout:    10 * time.Second,
		}),
	)
	require.NoError(t, err)

	// Verify post-remove hook ran
	data, err = os.ReadFile(postRemoveLog)
	require.NoError(t, err)
	assert.Contains(t, string(data), "removed="+result.Path)
}
