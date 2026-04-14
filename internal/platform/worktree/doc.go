// Package worktree provides types and interfaces for managing git worktrees.
//
// The worktree package defines the domain layer for git worktree operations.
// It follows the functional core / imperative shell pattern:
//
//   - Core types (WorktreeEntry, WorktreeResult, etc.) are pure data with no IO.
//   - The Manager interface defines the contract for operations.
//   - Adapter packages provide the concrete implementation using git commands.
//
// Usage:
//
//	// Types are used with the Manager interface
//	var mgr worktree.Manager = adapter.NewManager(runner)
//	result, err := mgr.Create(ctx, repoPath, "feat/my-feature",
//	    worktree.WithNewBranch(true),
//	    worktree.WithRef("main"),
//	    worktree.WithBaseDir("/tmp/worktrees"),
//	)
//
// Option functions configure operations following the functional options pattern:
//
//	worktree.WithNewBranch(true)   // Create a new branch
//	worktree.WithRef("v1.0")      // Base on specific ref
//	worktree.WithBaseDir(dir)     // Custom worktree directory
//	worktree.WithForce(true)      // Force dirty operations
//	worktree.WithDeleteBranch(true) // Delete branch on remove
//	worktree.WithInclude("*.go")  // Include patterns for copy
//	worktree.WithExclude("*.ts")  // Exclude patterns for copy
//	worktree.WithHooks(config)    // Lifecycle hooks
package worktree
