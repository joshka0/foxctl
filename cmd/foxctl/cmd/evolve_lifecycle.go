package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/worktree"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
	"github.com/joshka0/foxctl/internal/tooling/evolve/store"
	"github.com/spf13/cobra"
)

func newEvolveDiscardCommand() *cobra.Command {
	var (
		workspacePath string
		runID         string
		nodeID        string
		reason        string
	)

	cmd := &cobra.Command{
		Use:   "discard [node-id]",
		Short: "Mark a node as discarded without pruning worktree resources",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := openEvolveStore(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/discard", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			run, resolvedWorkspace, err := resolveEvolveRunForExecution(ctx, st, workspacePath, runID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/discard", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/discard", string(protocol.ErrorCodeEARG), err.Error())
			}

			resolvedNodeID, err := resolveEvolveNodeID(nodeID, args)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/discard", string(protocol.ErrorCodeEARG), err.Error())
			}
			node, err := resolveEvolveNodeByID(ctx, st, run, resolvedNodeID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/discard", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/discard", string(protocol.ErrorCodeEARG), err.Error())
			}

			trimmedReason := strings.TrimSpace(reason)
			if trimmedReason != "" {
				node.PrunedReason = trimmedReason
			}
			node.Status = model.NodeStatusDiscarded
			node.UpdatedAt = time.Now().UTC()

			if err := st.SaveNode(ctx, node); err != nil {
				return writeErrorEnvelope(cmd, "evolve/discard", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist node: %v", err))
			}

			return writeOK(cmd, "evolve/discard", map[string]any{
				"workspace_path": resolvedWorkspace,
				"run_id":         run.ID,
				"node_id":        node.ID,
				"status":         node.Status,
				"pruned_reason":  node.PrunedReason,
				"branch":         node.Branch,
				"worktree_path":  node.WorktreePath,
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&runID, "run", "", "Run id (defaults to active run for workspace)")
	cmd.Flags().StringVar(&nodeID, "node", "", "Node id (or provide as positional argument)")
	cmd.Flags().StringVar(&reason, "reason", "", "Optional reason attached to this discard decision")
	return cmd
}

func newEvolvePruneCommand() *cobra.Command {
	var (
		workspacePath string
		runID         string
		nodeID        string
		reason        string
		force         bool
		deleteBranch  bool
	)

	cmd := &cobra.Command{
		Use:   "prune [node-id]",
		Short: "Remove a node worktree safely and mark the node as pruned",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := openEvolveStore(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/prune", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			run, resolvedWorkspace, err := resolveEvolveRunForExecution(ctx, st, workspacePath, runID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/prune", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/prune", string(protocol.ErrorCodeEARG), err.Error())
			}

			resolvedNodeID, err := resolveEvolveNodeID(nodeID, args)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/prune", string(protocol.ErrorCodeEARG), err.Error())
			}
			node, err := resolveEvolveNodeByID(ctx, st, run, resolvedNodeID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/prune", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/prune", string(protocol.ErrorCodeEARG), err.Error())
			}

			removalPath, err := resolveEvolvePrunePath(run, node)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/prune", string(protocol.ErrorCodeEARG), err.Error())
			}

			mgr := worktree.NewManager()
			entry, err := mgr.Status(ctx, run.WorkspacePath, removalPath)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/prune", string(protocol.ErrorCodeEARG), fmt.Sprintf("resolve worktree entry: %v", err))
			}

			storedBranch := strings.TrimSpace(node.Branch)
			actualBranch := strings.TrimSpace(entry.Branch)
			if storedBranch != "" && storedBranch != actualBranch {
				return writeErrorEnvelope(
					cmd,
					"evolve/prune",
					string(protocol.ErrorCodeEARG),
					fmt.Sprintf("node branch %s does not match worktree branch %s for path %s", storedBranch, actualBranch, removalPath),
				)
			}

			autoDeleteBranch := evolveIsFoxctlOwnedBranch(actualBranch)
			branchDeleted := (deleteBranch || autoDeleteBranch) && actualBranch != ""

			removeOpts := make([]worktree.Option, 0, 2)
			if force {
				removeOpts = append(removeOpts, worktree.WithForce(true))
			}
			if branchDeleted {
				removeOpts = append(removeOpts, worktree.WithDeleteBranch(true))
			}

			if err := mgr.Remove(ctx, run.WorkspacePath, removalPath, removeOpts...); err != nil {
				return writeErrorEnvelope(cmd, "evolve/prune", string(evolvePruneErrorCode(err)), fmt.Sprintf("remove worktree: %v", err))
			}

			trimmedReason := strings.TrimSpace(reason)
			if trimmedReason != "" {
				node.PrunedReason = trimmedReason
			}
			node.Status = model.NodeStatusPruned
			node.WorktreePath = ""
			if branchDeleted {
				node.Branch = ""
			}
			node.UpdatedAt = time.Now().UTC()
			if err := st.SaveNode(ctx, node); err != nil {
				return writeErrorEnvelope(cmd, "evolve/prune", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist pruned node: %v", err))
			}

			removedBranch := ""
			if branchDeleted {
				removedBranch = actualBranch
			}
			return writeOK(cmd, "evolve/prune", map[string]any{
				"workspace_path":        resolvedWorkspace,
				"run_id":                run.ID,
				"node_id":               node.ID,
				"status":                node.Status,
				"pruned_reason":         node.PrunedReason,
				"removed_worktree_path": removalPath,
				"removed_branch":        removedBranch,
				"branch_deleted":        branchDeleted,
				"delete_branch_auto":    autoDeleteBranch,
				"force":                 force,
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&runID, "run", "", "Run id (defaults to active run for workspace)")
	cmd.Flags().StringVar(&nodeID, "node", "", "Node id (or provide as positional argument)")
	cmd.Flags().StringVar(&reason, "reason", "", "Optional prune reason persisted on the node")
	cmd.Flags().BoolVar(&force, "force", false, "Force prune even if the worktree has uncommitted changes")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "Delete branch even when it is not foxctl-owned")
	return cmd
}

func newEvolveResetCommand() *cobra.Command {
	var (
		workspacePath string
		runID         string
		nodeID        string
	)

	cmd := &cobra.Command{
		Use:   "reset [node-id]",
		Short: "Reset node lifecycle fields for rerun while preserving attempt history",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := openEvolveStore(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/reset", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			run, resolvedWorkspace, err := resolveEvolveRunForExecution(ctx, st, workspacePath, runID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/reset", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/reset", string(protocol.ErrorCodeEARG), err.Error())
			}

			resolvedNodeID, err := resolveEvolveNodeID(nodeID, args)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/reset", string(protocol.ErrorCodeEARG), err.Error())
			}
			node, err := resolveEvolveNodeByID(ctx, st, run, resolvedNodeID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/reset", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/reset", string(protocol.ErrorCodeEARG), err.Error())
			}

			attempts, err := st.AttemptsByNode(ctx, node.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/reset", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load node attempts: %v", err))
			}

			if node.Status == model.NodeStatusRoot || strings.TrimSpace(node.ParentID) == "" {
				node.Status = model.NodeStatusRoot
			} else {
				node.Status = model.NodeStatusPending
			}
			node.Score = nil
			node.EvalEpoch = 0
			node.CurrentAttempt = 0
			node.EvaluatedAttempts = 0
			node.PrunedReason = ""
			node.UpdatedAt = time.Now().UTC()
			if err := st.SaveNode(ctx, node); err != nil {
				return writeErrorEnvelope(cmd, "evolve/reset", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist node reset: %v", err))
			}

			return writeOK(cmd, "evolve/reset", map[string]any{
				"workspace_path":     resolvedWorkspace,
				"run_id":             run.ID,
				"node_id":            node.ID,
				"status":             node.Status,
				"pruned_reason":      node.PrunedReason,
				"current_attempt":    node.CurrentAttempt,
				"evaluated_attempts": node.EvaluatedAttempts,
				"eval_epoch":         node.EvalEpoch,
				"attempts_preserved": len(attempts),
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&runID, "run", "", "Run id (defaults to active run for workspace)")
	cmd.Flags().StringVar(&nodeID, "node", "", "Node id (or provide as positional argument)")
	return cmd
}

func resolveEvolvePrunePath(run model.Run, node model.Node) (string, error) {
	if node.Status == model.NodeStatusRoot || strings.TrimSpace(node.ParentID) == "" {
		return "", fmt.Errorf("cannot prune root node %s", node.ID)
	}

	path := strings.TrimSpace(node.WorktreePath)
	if path == "" {
		return "", fmt.Errorf("node %s has no worktree path", node.ID)
	}

	if absPath, err := filepath.Abs(path); err == nil {
		path = absPath
	}
	path = filepath.Clean(path)

	workspacePath := strings.TrimSpace(run.WorkspacePath)
	if absWorkspace, err := filepath.Abs(workspacePath); err == nil {
		workspacePath = absWorkspace
	}
	workspacePath = filepath.Clean(workspacePath)
	if path == workspacePath {
		return "", fmt.Errorf("cannot prune workspace root path: %s", path)
	}
	return path, nil
}

func evolveIsFoxctlOwnedBranch(branch string) bool {
	branch = strings.TrimSpace(branch)
	return strings.HasPrefix(branch, "foxctl/evolve/")
}

func evolvePruneErrorCode(err error) protocol.ErrorCode {
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "main checkout"),
		strings.Contains(message, "dirty"),
		strings.Contains(message, "not a git repository"):
		return protocol.ErrorCodeEARG
	default:
		return protocol.ErrorCodeERuntime
	}
}
