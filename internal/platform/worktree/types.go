// Package worktree provides types and interfaces for managing git worktrees.
//
// This package defines the domain types and Manager interface used to create,
// list, remove, and manage git worktrees. All types are pure — no IO, no
// external dependencies beyond stdlib. Implementation lives in adapter packages.
//
// The Manager interface follows the plan/apply pattern from AGENTS.md:
// operations are pure function signatures that the shell (adapter) implements
// with real git commands.
package worktree

import (
	"context"
	"time"
)

// WorktreeStatus represents the health state of a worktree entry.
type WorktreeStatus string

const (
	// StatusOK indicates a healthy, accessible worktree.
	StatusOK WorktreeStatus = "ok"
	// StatusLocked indicates the worktree is locked by git.
	StatusLocked WorktreeStatus = "locked"
	// StatusPrunable indicates the worktree directory is missing but admin data remains.
	StatusPrunable WorktreeStatus = "prunable"
)

// WorktreeEntry describes a single worktree as returned by List.
// It maps to the output of `git worktree list --porcelain`.
type WorktreeEntry struct {
	// Path is the absolute path to the worktree directory.
	Path string `json:"path"`
	// Branch is the checked-out branch name (empty for detached HEAD).
	Branch string `json:"branch,omitempty"`
	// Commit is the full HEAD commit SHA.
	Commit string `json:"commit"`
	// Status is the detected health state of the worktree.
	Status WorktreeStatus `json:"status"`
	// Bare is true if this entry represents a bare repository.
	Bare bool `json:"bare,omitempty"`
	// Reason contains additional context for non-ok statuses (e.g., lock reason).
	Reason string `json:"reason,omitempty"`
}

// WorktreeResult is returned by Create after a successful worktree creation.
type WorktreeResult struct {
	// Path is the absolute path to the newly created worktree directory.
	Path string `json:"path"`
	// Branch is the branch name checked out in the new worktree.
	Branch string `json:"branch"`
	// Commit is the HEAD commit SHA after checkout.
	Commit string `json:"commit"`
	// CreatedAt is the time the worktree was created.
	CreatedAt time.Time `json:"created_at"`
}

// HookConfig defines lifecycle hooks for worktree operations.
// Hooks receive environment variables: WORKTREE_PATH, WORKTREE_BRANCH,
// WORKTREE_REPO_PATH, WORKTREE_COMMIT.
type HookConfig struct {
	// PostCreate is a command to run after worktree creation.
	// The worktree is already on disk when this runs.
	PostCreate string `json:"post_create,omitempty"`
	// PostRemove is a command to run after worktree removal.
	// The worktree directory is already gone when this runs.
	PostRemove string `json:"post_remove,omitempty"`
	// Timeout is the maximum time to wait for a hook to complete.
	// Defaults to 30 seconds if zero.
	Timeout time.Duration `json:"timeout,omitempty"`
}

// DefaultHookTimeout is the default timeout for lifecycle hooks.
const DefaultHookTimeout = 30 * time.Second

// EffectiveTimeout returns the configured timeout or the default.
func (h HookConfig) EffectiveTimeout() time.Duration {
	if h.Timeout > 0 {
		return h.Timeout
	}
	return DefaultHookTimeout
}

// CreateOptions holds parameters for worktree creation.
// Use WithXxx option functions to set fields.
type CreateOptions struct {
	// NewBranch indicates whether to create a new branch (git worktree add -b).
	NewBranch bool
	// Ref is the commit, tag, or branch to base the worktree on.
	Ref string
	// BaseDir is the directory under which the worktree is created.
	// When empty, a sensible default is used (sibling of the repo).
	BaseDir string
	// Hooks to run after creation.
	Hooks HookConfig
}

// RemoveOptions holds parameters for worktree removal.
// Use WithXxx option functions to set fields.
type RemoveOptions struct {
	// Force removes the worktree even if it has uncommitted changes.
	Force bool
	// DeleteBranch also deletes the associated branch after removal.
	DeleteBranch bool
	// Hooks to run after removal.
	Hooks HookConfig
}

// CopyOptions holds parameters for file copying into a worktree.
// Use WithXxx option functions to set fields.
type CopyOptions struct {
	// Include glob patterns for files to copy (e.g., "*.go", "*.mod").
	// When empty, all files are copied.
	Include []string
	// Exclude glob patterns for files to skip (e.g., "*.test.ts", "node_modules/**").
	Exclude []string
	// Hooks to run after copy (future use).
	Hooks HookConfig
}

// Option is a function that configures operation options.
// This follows the functional options pattern used throughout foxctl.
type Option func(*Options)

// Options is the unified option bag for all worktree operations.
// Individual operation types (CreateOptions, RemoveOptions, CopyOptions)
// are populated from this via the option functions.
type Options struct {
	// NewBranch indicates whether to create a new branch.
	NewBranch bool
	// Ref is the commit, tag, or branch to base the worktree on.
	Ref string
	// BaseDir is the directory under which the worktree is created.
	BaseDir string
	// Force forces operations that would normally fail (e.g., dirty removal).
	Force bool
	// DeleteBranch also deletes the branch after worktree removal.
	DeleteBranch bool
	// Include glob patterns for file copy operations.
	Include []string
	// Exclude glob patterns for file copy operations.
	Exclude []string
	// Hooks configures lifecycle hooks.
	Hooks HookConfig
}

// WithNewBranch sets whether to create a new branch.
func WithNewBranch(v bool) Option {
	return func(o *Options) { o.NewBranch = v }
}

// WithRef sets the base ref (commit, tag, or branch) for the worktree.
func WithRef(ref string) Option {
	return func(o *Options) { o.Ref = ref }
}

// WithBaseDir sets the directory under which the worktree is created.
func WithBaseDir(dir string) Option {
	return func(o *Options) { o.BaseDir = dir }
}

// WithForce forces operations that would normally be rejected.
func WithForce(v bool) Option {
	return func(o *Options) { o.Force = v }
}

// WithDeleteBranch sets whether to delete the branch after removal.
func WithDeleteBranch(v bool) Option {
	return func(o *Options) { o.DeleteBranch = v }
}

// WithInclude sets glob patterns for files to include in copy operations.
func WithInclude(patterns ...string) Option {
	return func(o *Options) { o.Include = patterns }
}

// WithExclude sets glob patterns for files to exclude from copy operations.
func WithExclude(patterns ...string) Option {
	return func(o *Options) { o.Exclude = patterns }
}

// WithHooks sets lifecycle hooks for the operation.
func WithHooks(hooks HookConfig) Option {
	return func(o *Options) { o.Hooks = hooks }
}

// ApplyOptions applies the given option functions to an Options struct
// and returns it. This is a pure function — no side effects.
func ApplyOptions(opts ...Option) Options {
	var o Options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// CreateOpts converts the unified Options to CreateOptions.
// This bridges the functional options pattern to the typed operation struct.
func (o Options) CreateOpts() CreateOptions {
	return CreateOptions{
		NewBranch: o.NewBranch,
		Ref:       o.Ref,
		BaseDir:   o.BaseDir,
		Hooks:     o.Hooks,
	}
}

// RemoveOpts converts the unified Options to RemoveOptions.
func (o Options) RemoveOpts() RemoveOptions {
	return RemoveOptions{
		Force:        o.Force,
		DeleteBranch: o.DeleteBranch,
		Hooks:        o.Hooks,
	}
}

// CopyOpts converts the unified Options to CopyOptions.
func (o Options) CopyOpts() CopyOptions {
	return CopyOptions{
		Include: o.Include,
		Exclude: o.Exclude,
		Hooks:   o.Hooks,
	}
}

// Manager defines the interface for git worktree operations.
// Implementations must be safe for concurrent use.
//
// All methods accept context.Context for cancellation and timeout support.
// Errors should be structured (not raw git stderr) with actionable messages.
type Manager interface {
	// Create creates a new git worktree for the given branch name.
	//
	// Options:
	//   - WithNewBranch(true): create a new branch (git worktree add -b <name>)
	//   - WithRef(ref): base the worktree on a specific commit/tag/branch
	//   - WithBaseDir(dir): create the worktree under this directory
	//   - WithHooks(config): run post-create hook
	//
	// Returns WorktreeResult with the path, branch, commit, and creation time.
	// Returns error if:
	//   - repo is not a git repository
	//   - branch already exists with WithNewBranch(true)
	//   - ref does not exist
	//   - worktree path already exists
	//   - context is cancelled
	Create(ctx context.Context, repo string, branch string, opts ...Option) (WorktreeResult, error)

	// List returns all worktrees for the given repository.
	//
	// Parses `git worktree list --porcelain` output and returns entries
	// with path, branch, commit, and status metadata.
	//
	// Returns error if:
	//   - repo is not a git repository
	//   - context is cancelled
	List(ctx context.Context, repo string) ([]WorktreeEntry, error)

	// Remove removes a worktree at the given path.
	//
	// Options:
	//   - WithForce(true): remove even with uncommitted changes
	//   - WithDeleteBranch(true): also delete the associated branch
	//   - WithHooks(config): run post-remove hook
	//
	// Returns error if:
	//   - path is not a valid worktree
	//   - path is the main checkout (rejected)
	//   - worktree has uncommitted changes without WithForce
	//   - context is cancelled
	Remove(ctx context.Context, repo string, path string, opts ...Option) error

	// Resolve finds a worktree by branch name or partial match.
	//
	// Query rules:
	//   - "1" returns the main checkout entry
	//   - Full branch name returns exact match
	//   - Partial name uses suffix matching (e.g., "abc-123" matches "feat/abc-123")
	//   - Ambiguous matches return error listing candidates
	//
	// Returns error if:
	//   - no worktree matches the query
	//   - multiple worktrees match (ambiguous)
	//   - repo is not a git repository
	//   - context is cancelled
	Resolve(ctx context.Context, repo string, query string) (WorktreeEntry, error)

	// Status returns the health status of a specific worktree.
	//
	// Returns WorktreeEntry with the Status field populated:
	//   - "ok": healthy, accessible worktree
	//   - "locked": worktree is locked by git
	//   - "prunable": directory deleted but admin data remains
	//
	// Returns error if:
	//   - path is not a known worktree
	//   - repo is not a git repository
	//   - context is cancelled
	Status(ctx context.Context, repo string, path string) (WorktreeEntry, error)

	// Prune removes stale worktree administrative data for deleted directories.
	//
	// Runs `git worktree prune` to clean up entries where the worktree
	// directory has been removed externally.
	//
	// Returns error if:
	//   - repo is not a git repository
	//   - context is cancelled
	Prune(ctx context.Context, repo string) error

	// CopyFiles copies files from src to dst with optional include/exclude patterns.
	//
	// Options:
	//   - WithInclude(patterns...): only copy files matching these globs
	//   - WithExclude(patterns...): skip files matching these globs
	//
	// Preserves directory structure. Handles dotfiles (e.g., .env*).
	//
	// Returns error if:
	//   - src does not exist
	//   - dst cannot be created
	//   - context is cancelled
	CopyFiles(ctx context.Context, src string, dst string, opts ...Option) error
}
