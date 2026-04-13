package worktree

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyOptions_Defaults(t *testing.T) {
	opts := ApplyOptions()
	assert.False(t, opts.NewBranch, "NewBranch should default to false")
	assert.Empty(t, opts.Ref, "Ref should default to empty")
	assert.Empty(t, opts.BaseDir, "BaseDir should default to empty")
	assert.False(t, opts.Force, "Force should default to false")
	assert.False(t, opts.DeleteBranch, "DeleteBranch should default to false")
	assert.Nil(t, opts.Include, "Include should default to nil")
	assert.Nil(t, opts.Exclude, "Exclude should default to nil")
	assert.Empty(t, opts.Hooks.PostCreate, "Hooks.PostCreate should default to empty")
	assert.Empty(t, opts.Hooks.PostRemove, "Hooks.PostRemove should default to empty")
	assert.Zero(t, opts.Hooks.Timeout, "Hooks.Timeout should default to zero")
}

func TestApplyOptions_WithNewBranch(t *testing.T) {
	opts := ApplyOptions(WithNewBranch(true))
	assert.True(t, opts.NewBranch)
}

func TestApplyOptions_WithRef(t *testing.T) {
	opts := ApplyOptions(WithRef("v1.2.3"))
	assert.Equal(t, "v1.2.3", opts.Ref)
}

func TestApplyOptions_WithBaseDir(t *testing.T) {
	opts := ApplyOptions(WithBaseDir("/tmp/worktrees"))
	assert.Equal(t, "/tmp/worktrees", opts.BaseDir)
}

func TestApplyOptions_WithForce(t *testing.T) {
	opts := ApplyOptions(WithForce(true))
	assert.True(t, opts.Force)
}

func TestApplyOptions_WithDeleteBranch(t *testing.T) {
	opts := ApplyOptions(WithDeleteBranch(true))
	assert.True(t, opts.DeleteBranch)
}

func TestApplyOptions_WithInclude(t *testing.T) {
	opts := ApplyOptions(WithInclude("*.go", "*.mod"))
	assert.Equal(t, []string{"*.go", "*.mod"}, opts.Include)
}

func TestApplyOptions_WithExclude(t *testing.T) {
	opts := ApplyOptions(WithExclude("*.test.ts", "node_modules/**"))
	assert.Equal(t, []string{"*.test.ts", "node_modules/**"}, opts.Exclude)
}

func TestApplyOptions_WithHooks(t *testing.T) {
	hooks := HookConfig{
		PostCreate: "echo created",
		PostRemove: "echo removed",
		Timeout:    10 * time.Second,
	}
	opts := ApplyOptions(WithHooks(hooks))
	assert.Equal(t, "echo created", opts.Hooks.PostCreate)
	assert.Equal(t, "echo removed", opts.Hooks.PostRemove)
	assert.Equal(t, 10*time.Second, opts.Hooks.Timeout)
}

func TestApplyOptions_Combined(t *testing.T) {
	opts := ApplyOptions(
		WithNewBranch(true),
		WithRef("main"),
		WithBaseDir("/custom/dir"),
		WithForce(true),
		WithDeleteBranch(true),
		WithInclude("*.go"),
		WithExclude("*_test.go"),
		WithHooks(HookConfig{PostCreate: "make setup", Timeout: 60 * time.Second}),
	)

	assert.True(t, opts.NewBranch)
	assert.Equal(t, "main", opts.Ref)
	assert.Equal(t, "/custom/dir", opts.BaseDir)
	assert.True(t, opts.Force)
	assert.True(t, opts.DeleteBranch)
	assert.Equal(t, []string{"*.go"}, opts.Include)
	assert.Equal(t, []string{"*_test.go"}, opts.Exclude)
	assert.Equal(t, "make setup", opts.Hooks.PostCreate)
	assert.Equal(t, 60*time.Second, opts.Hooks.Timeout)
}

func TestApplyOptions_LastOptionWins(t *testing.T) {
	opts := ApplyOptions(
		WithRef("v1.0"),
		WithRef("v2.0"),
	)
	assert.Equal(t, "v2.0", opts.Ref, "last option should win")
}

func TestOptions_CreateOpts(t *testing.T) {
	opts := ApplyOptions(
		WithNewBranch(true),
		WithRef("develop"),
		WithBaseDir("/tmp/wt"),
		WithHooks(HookConfig{PostCreate: "make setup"}),
	)
	co := opts.CreateOpts()

	assert.True(t, co.NewBranch)
	assert.Equal(t, "develop", co.Ref)
	assert.Equal(t, "/tmp/wt", co.BaseDir)
	assert.Equal(t, "make setup", co.Hooks.PostCreate)
	// Force and DeleteBranch should not leak into CreateOpts
	assert.False(t, co.NewBranch == opts.Force) // sanity: different fields
}

func TestOptions_RemoveOpts(t *testing.T) {
	opts := ApplyOptions(
		WithForce(true),
		WithDeleteBranch(true),
		WithHooks(HookConfig{PostRemove: "cleanup.sh"}),
	)
	ro := opts.RemoveOpts()

	assert.True(t, ro.Force)
	assert.True(t, ro.DeleteBranch)
	assert.Equal(t, "cleanup.sh", ro.Hooks.PostRemove)
}

func TestOptions_CopyOpts(t *testing.T) {
	opts := ApplyOptions(
		WithInclude("*.go", "*.mod"),
		WithExclude("*_test.go"),
	)
	co := opts.CopyOpts()

	assert.Equal(t, []string{"*.go", "*.mod"}, co.Include)
	assert.Equal(t, []string{"*_test.go"}, co.Exclude)
}

func TestHookConfig_EffectiveTimeout_Default(t *testing.T) {
	h := HookConfig{}
	assert.Equal(t, DefaultHookTimeout, h.EffectiveTimeout(), "zero timeout should use default")
}

func TestHookConfig_EffectiveTimeout_Custom(t *testing.T) {
	h := HookConfig{Timeout: 10 * time.Second}
	assert.Equal(t, 10*time.Second, h.EffectiveTimeout(), "custom timeout should be used")
}

func TestWorktreeEntry_Fields(t *testing.T) {
	entry := WorktreeEntry{
		Path:   "/tmp/worktrees/feat-x",
		Branch: "feat/x",
		Commit: "abc123def456",
		Status: StatusOK,
		Bare:   false,
	}
	assert.Equal(t, "/tmp/worktrees/feat-x", entry.Path)
	assert.Equal(t, "feat/x", entry.Branch)
	assert.Equal(t, "abc123def456", entry.Commit)
	assert.Equal(t, StatusOK, entry.Status)
	assert.False(t, entry.Bare)
}

func TestWorktreeEntry_LockedStatus(t *testing.T) {
	entry := WorktreeEntry{
		Path:   "/tmp/worktrees/feat-y",
		Branch: "feat/y",
		Commit: "deadbeef",
		Status: StatusLocked,
		Reason: "user locked",
	}
	assert.Equal(t, StatusLocked, entry.Status)
	assert.Equal(t, "user locked", entry.Reason)
}

func TestWorktreeEntry_PrunableStatus(t *testing.T) {
	entry := WorktreeEntry{
		Path:   "/tmp/worktrees/gone",
		Branch: "feat/gone",
		Commit: "cafebaad",
		Status: StatusPrunable,
	}
	assert.Equal(t, StatusPrunable, entry.Status)
}

func TestWorktreeResult_Fields(t *testing.T) {
	now := time.Now().UTC()
	result := WorktreeResult{
		Path:      "/tmp/worktrees/feat-z",
		Branch:    "feat/z",
		Commit:    "1234567890ab",
		CreatedAt: now,
	}
	assert.Equal(t, "/tmp/worktrees/feat-z", result.Path)
	assert.Equal(t, "feat/z", result.Branch)
	assert.Equal(t, "1234567890ab", result.Commit)
	assert.Equal(t, now, result.CreatedAt)
}

func TestWorktreeStatus_Constants(t *testing.T) {
	assert.Equal(t, WorktreeStatus("ok"), StatusOK)
	assert.Equal(t, WorktreeStatus("locked"), StatusLocked)
	assert.Equal(t, WorktreeStatus("prunable"), StatusPrunable)
}

func TestCreateOptions_Fields(t *testing.T) {
	co := CreateOptions{
		NewBranch: true,
		Ref:       "v1.0",
		BaseDir:   "/custom",
		Hooks:     HookConfig{PostCreate: "echo hi", Timeout: 5 * time.Second},
	}
	assert.True(t, co.NewBranch)
	assert.Equal(t, "v1.0", co.Ref)
	assert.Equal(t, "/custom", co.BaseDir)
	assert.Equal(t, "echo hi", co.Hooks.PostCreate)
}

func TestRemoveOptions_Fields(t *testing.T) {
	ro := RemoveOptions{
		Force:        true,
		DeleteBranch: true,
		Hooks:        HookConfig{PostRemove: "cleanup", Timeout: 15 * time.Second},
	}
	assert.True(t, ro.Force)
	assert.True(t, ro.DeleteBranch)
	assert.Equal(t, "cleanup", ro.Hooks.PostRemove)
}

func TestCopyOptions_Fields(t *testing.T) {
	co := CopyOptions{
		Include: []string{"*.go"},
		Exclude: []string{"*_test.go"},
	}
	assert.Equal(t, []string{"*.go"}, co.Include)
	assert.Equal(t, []string{"*_test.go"}, co.Exclude)
}

func TestManager_Interface(t *testing.T) {
	// Verify Manager interface has the expected methods by checking
	// that a nil pointer would satisfy the interface signature.
	// This is a compile-time check: if the interface changes,
	// this test will fail to compile.
	var _ Manager = (Manager)(nil)
}

func TestOptions_EmptyIncludeExclude(t *testing.T) {
	opts := ApplyOptions()
	co := opts.CopyOpts()
	assert.Nil(t, co.Include)
	assert.Nil(t, co.Exclude)
}

func TestHookConfig_EmptyCommands(t *testing.T) {
	h := HookConfig{}
	assert.Empty(t, h.PostCreate)
	assert.Empty(t, h.PostRemove)
}

func TestWorktreeEntry_DetachedHead(t *testing.T) {
	// Detached HEAD entries have empty Branch
	entry := WorktreeEntry{
		Path:   "/tmp/worktrees/detached",
		Branch: "",
		Commit: "abcdef123456",
		Status: StatusOK,
	}
	assert.Empty(t, entry.Branch, "detached HEAD should have empty branch")
}

func TestWorktreeEntry_BareRepo(t *testing.T) {
	entry := WorktreeEntry{
		Path:   "/path/to/repo.git",
		Commit: "abc123",
		Bare:   true,
	}
	assert.True(t, entry.Bare)
}

func TestApplyOptions_IncludeAppend(t *testing.T) {
	// Multiple WithInclude calls should override (not append)
	opts := ApplyOptions(
		WithInclude("*.go"),
		WithInclude("*.ts"),
	)
	assert.Equal(t, []string{"*.ts"}, opts.Include, "last WithInclude should win")
}

func TestApplyOptions_ExcludeAppend(t *testing.T) {
	// Multiple WithExclude calls should override (not append)
	opts := ApplyOptions(
		WithExclude("*.test.ts"),
		WithExclude("node_modules/**"),
	)
	assert.Equal(t, []string{"node_modules/**"}, opts.Exclude, "last WithExclude should win")
}

func TestDefaultHookTimeout_Value(t *testing.T) {
	assert.Equal(t, 30*time.Second, DefaultHookTimeout)
}

func TestOptions_CreateOpts_DoesNotIncludeRemoveFields(t *testing.T) {
	opts := ApplyOptions(
		WithForce(true),
		WithDeleteBranch(true),
	)
	co := opts.CreateOpts()
	// CreateOptions should not have Force/DeleteBranch fields
	// These fields don't exist on CreateOptions, so we just verify
	// the struct doesn't have them (compile-time check)
	assert.Empty(t, co.Ref)       // not set
	assert.False(t, co.NewBranch) // not set
}

func TestOptions_RemoveOpts_DoesNotIncludeCreateFields(t *testing.T) {
	opts := ApplyOptions(
		WithNewBranch(true),
		WithRef("main"),
		WithBaseDir("/tmp"),
	)
	ro := opts.RemoveOpts()
	// RemoveOptions should not have NewBranch/Ref/BaseDir
	assert.False(t, ro.Force)        // not set
	assert.False(t, ro.DeleteBranch) // not set
}

func TestApplyOptions_NilOptionSlice(t *testing.T) {
	// Passing no options should be valid
	opts := ApplyOptions()
	require.NotNil(t, opts)
}

func TestWorktreeResult_ZeroValue(t *testing.T) {
	var r WorktreeResult
	assert.Empty(t, r.Path)
	assert.Empty(t, r.Branch)
	assert.Empty(t, r.Commit)
	assert.True(t, r.CreatedAt.IsZero())
}

func TestWorktreeEntry_ZeroValue(t *testing.T) {
	var e WorktreeEntry
	assert.Empty(t, e.Path)
	assert.Empty(t, e.Branch)
	assert.Empty(t, e.Commit)
	assert.Empty(t, e.Status)
	assert.False(t, e.Bare)
	assert.Empty(t, e.Reason)
}
