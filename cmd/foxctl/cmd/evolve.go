package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/platform/worktree"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
	"github.com/joshka0/foxctl/internal/tooling/evolve/store"
	"github.com/joshka0/foxctl/internal/tooling/evolve/view"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newEvolveCommand())
}

func newEvolveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evolve",
		Short: "Run hypothesis-driven repository evolution workflows",
	}
	cmd.AddCommand(
		newEvolveInitCommand(),
		newEvolveNewCommand(),
		newEvolveStatusCommand(),
		newEvolveTreeCommand(),
	)
	return cmd
}

func newEvolveInitCommand() *cobra.Command {
	var (
		workspacePath string
		targetPath    string
		benchmarkCmd  string
		metric        string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize an evolve run and root node",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := openEvolveStore(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/init", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			if strings.TrimSpace(benchmarkCmd) == "" {
				return writeErrorEnvelope(cmd, "evolve/init", string(protocol.ErrorCodeEARG), "--benchmark is required")
			}

			metricValue := model.MetricDirection(strings.ToLower(strings.TrimSpace(metric)))
			if !metricValue.Valid() {
				return writeErrorEnvelope(cmd, "evolve/init", string(protocol.ErrorCodeEARG), "metric must be one of: max, min")
			}

			resolvedWorkspace, err := resolveEvolveWorkspacePath(workspacePath)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/init", string(protocol.ErrorCodeEARG), fmt.Sprintf("resolve workspace: %v", err))
			}
			resolvedTarget, err := resolveEvolveTargetPath(resolvedWorkspace, targetPath)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/init", string(protocol.ErrorCodeEARG), fmt.Sprintf("resolve target: %v", err))
			}

			now := time.Now().UTC()
			runID := ulid.Make().String()
			rootID := ulid.Make().String()

			run := model.Run{
				ID:               runID,
				WorkspacePath:    resolvedWorkspace,
				TargetPath:       resolvedTarget,
				BenchmarkCommand: benchmarkCmd,
				Metric:           metricValue,
				Status:           model.RunStatusActive,
				Active:           true,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := st.SaveRun(ctx, run); err != nil {
				return writeErrorEnvelope(cmd, "evolve/init", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist run: %v", err))
			}

			root := model.Node{
				ID:           rootID,
				RunID:        runID,
				Status:       model.NodeStatusRoot,
				WorktreePath: resolvedWorkspace,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := st.SaveNode(ctx, root); err != nil {
				return writeErrorEnvelope(cmd, "evolve/init", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist root node: %v", err))
			}

			return writeOK(cmd, "evolve/init", map[string]any{
				"run_id":            runID,
				"workspace_path":    resolvedWorkspace,
				"target_path":       resolvedTarget,
				"benchmark_command": benchmarkCmd,
				"metric":            metricValue,
				"status":            model.RunStatusActive,
				"root_node_id":      rootID,
				"active":            true,
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&targetPath, "target", ".", "Target path to evolve (absolute or workspace-relative)")
	cmd.Flags().StringVar(&benchmarkCmd, "benchmark", "", "Benchmark command used to score attempts")
	cmd.Flags().StringVar(&metric, "metric", string(model.MetricMax), "Benchmark metric direction (max|min)")
	return cmd
}

func newEvolveStatusCommand() *cobra.Command {
	var workspacePath string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show active evolve run status for a workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := openEvolveStore(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/status", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			resolvedWorkspace, err := resolveEvolveWorkspacePath(workspacePath)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/status", string(protocol.ErrorCodeEARG), fmt.Sprintf("resolve workspace: %v", err))
			}

			run, ok, err := st.ActiveRun(ctx, resolvedWorkspace)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/status", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load active run: %v", err))
			}
			if !ok {
				return writeOK(cmd, "evolve/status", map[string]any{
					"workspace_path": resolvedWorkspace,
					"active_run":     false,
				}, "run", profilesCoreAgent)
			}

			nodes, err := st.NodesByRun(ctx, run.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/status", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load nodes: %v", err))
			}
			frontier, err := st.FrontierNodes(ctx, run.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/status", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load frontier: %v", err))
			}
			summary := view.BuildStatusSummary(run, nodes, frontier)

			return writeOK(cmd, "evolve/status", map[string]any{
				"workspace_path": resolvedWorkspace,
				"active_run":     true,
				"summary":        summary,
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	return cmd
}

func newEvolveNewCommand() *cobra.Command {
	var (
		workspacePath string
		runID         string
		parentID      string
		hypothesis    string
	)

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a child experiment node and isolated worktree branch",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := openEvolveStore(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/new", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			parentID = strings.TrimSpace(parentID)
			if parentID == "" {
				return writeErrorEnvelope(cmd, "evolve/new", string(protocol.ErrorCodeEARG), "--parent is required")
			}

			run, resolvedWorkspace, err := resolveEvolveRunForNew(ctx, st, workspacePath, runID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/new", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/new", string(protocol.ErrorCodeEARG), err.Error())
			}

			parent, err := st.Node(ctx, parentID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/new", string(protocol.ErrorCodeENotFound), fmt.Sprintf("parent node not found: %s", parentID))
				}
				return writeErrorEnvelope(cmd, "evolve/new", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load parent node: %v", err))
			}
			if parent.RunID != run.ID {
				return writeErrorEnvelope(cmd, "evolve/new", string(protocol.ErrorCodeEARG), fmt.Sprintf("parent node %s does not belong to run %s", parentID, run.ID))
			}

			nodeID := ulid.Make().String()
			branchCandidate := evolveNodeBranchName(run.ID, nodeID)

			opts := []worktree.Option{worktree.WithNewBranch(true)}
			baseRef := strings.TrimSpace(parent.CommitSHA)
			if baseRef == "" {
				baseRef = strings.TrimSpace(parent.Branch)
			}
			if baseRef != "" {
				opts = append(opts, worktree.WithRef(baseRef))
			}

			mgr := worktree.NewManager()
			wt, err := mgr.Create(ctx, run.WorkspacePath, branchCandidate, opts...)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/new", string(protocol.ErrorCodeERuntime), fmt.Sprintf("create worktree: %v", err))
			}

			now := time.Now().UTC()
			child := model.Node{
				ID:           nodeID,
				RunID:        run.ID,
				ParentID:     parent.ID,
				Status:       model.NodeStatusPending,
				Hypothesis:   strings.TrimSpace(hypothesis),
				Branch:       wt.Branch,
				WorktreePath: wt.Path,
				CommitSHA:    wt.Commit,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := st.SaveNode(ctx, child); err != nil {
				removeErr := mgr.Remove(ctx, run.WorkspacePath, wt.Path, worktree.WithForce(true), worktree.WithDeleteBranch(true))
				if removeErr != nil {
					return writeErrorEnvelope(cmd, "evolve/new", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist child node: %v (rollback failed: %v)", err, removeErr))
				}
				return writeErrorEnvelope(cmd, "evolve/new", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist child node: %v", err))
			}

			return writeOK(cmd, "evolve/new", map[string]any{
				"workspace_path": resolvedWorkspace,
				"run_id":         run.ID,
				"parent_node_id": parent.ID,
				"node_id":        child.ID,
				"status":         child.Status,
				"hypothesis":     child.Hypothesis,
				"branch":         child.Branch,
				"worktree_path":  child.WorktreePath,
				"commit_sha":     child.CommitSHA,
				"base_ref":       baseRef,
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&runID, "run", "", "Run id (defaults to active run for workspace)")
	cmd.Flags().StringVar(&parentID, "parent", "", "Parent node id for the new experiment node")
	cmd.Flags().StringVar(&hypothesis, "hypothesis", "", "Hypothesis text attached to the child node")
	return cmd
}

func newEvolveTreeCommand() *cobra.Command {
	var (
		workspacePath string
		runID         string
	)

	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Render a deterministic run tree",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := openEvolveStore(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/tree", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			resolvedWorkspace, err := resolveEvolveWorkspacePath(workspacePath)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/tree", string(protocol.ErrorCodeEARG), fmt.Sprintf("resolve workspace: %v", err))
			}

			var run model.Run
			if strings.TrimSpace(runID) != "" {
				run, err = st.Run(ctx, strings.TrimSpace(runID))
				if err != nil {
					if errors.Is(err, store.ErrNotFound) {
						return writeErrorEnvelope(cmd, "evolve/tree", string(protocol.ErrorCodeENotFound), fmt.Sprintf("run not found: %s", strings.TrimSpace(runID)))
					}
					return writeErrorEnvelope(cmd, "evolve/tree", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load run: %v", err))
				}
			} else {
				activeRun, ok, activeErr := st.ActiveRun(ctx, resolvedWorkspace)
				if activeErr != nil {
					return writeErrorEnvelope(cmd, "evolve/tree", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load active run: %v", activeErr))
				}
				if !ok {
					return writeErrorEnvelope(cmd, "evolve/tree", string(protocol.ErrorCodeENotFound), "no active evolve run for workspace")
				}
				run = activeRun
			}

			nodes, err := st.NodesByRun(ctx, run.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/tree", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load nodes: %v", err))
			}
			frontier, err := st.FrontierNodes(ctx, run.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/tree", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load frontier: %v", err))
			}
			tree := view.BuildTreeView(run.ID, nodes, frontier)

			return writeOK(cmd, "evolve/tree", map[string]any{
				"workspace_path": resolvedWorkspace,
				"run_id":         run.ID,
				"tree":           tree,
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&runID, "run", "", "Run id (defaults to active run for workspace)")
	return cmd
}

func openEvolveStore(ctx context.Context) (*store.SQLStore, error) {
	cfg, err := commandConfig(ctx)
	if err != nil {
		return nil, err
	}
	return store.Open(ctx, cfg.Storage.Root)
}

func resolveEvolveWorkspacePath(override string) (string, error) {
	raw := strings.TrimSpace(override)
	if raw == "" {
		raw = workspace.Detect("")
	}
	if raw == "" {
		return "", fmt.Errorf("workspace path is empty")
	}
	abs, err := filepath.Abs(raw)
	if err == nil {
		raw = abs
	}
	return workspace.Normalize(raw), nil
}

func resolveEvolveTargetPath(workspacePath, targetPath string) (string, error) {
	workspacePath = filepath.Clean(strings.TrimSpace(workspacePath))
	if workspacePath == "" {
		return "", fmt.Errorf("workspace path is empty")
	}
	if absWorkspace, err := filepath.Abs(workspacePath); err == nil {
		workspacePath = filepath.Clean(absWorkspace)
	}

	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return "", fmt.Errorf("target path is empty")
	}
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(workspacePath, targetPath)
	}
	if absTarget, err := filepath.Abs(targetPath); err == nil {
		targetPath = absTarget
	}
	targetPath = filepath.Clean(targetPath)

	rel, err := filepath.Rel(workspacePath, targetPath)
	if err != nil {
		return "", fmt.Errorf("target path resolve failed: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target path escapes workspace: %s", targetPath)
	}
	return targetPath, nil
}

func resolveEvolveRunForNew(ctx context.Context, st store.Store, workspacePath, runID string) (model.Run, string, error) {
	runID = strings.TrimSpace(runID)
	if runID != "" {
		run, err := st.Run(ctx, runID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return model.Run{}, "", fmt.Errorf("run not found: %s: %w", runID, err)
			}
			return model.Run{}, "", fmt.Errorf("load run: %w", err)
		}
		if strings.TrimSpace(workspacePath) != "" {
			resolvedWorkspace, err := resolveEvolveWorkspacePath(workspacePath)
			if err != nil {
				return model.Run{}, "", fmt.Errorf("resolve workspace: %w", err)
			}
			if resolvedWorkspace != run.WorkspacePath {
				return model.Run{}, "", fmt.Errorf("run %s belongs to workspace %s (got %s)", runID, run.WorkspacePath, resolvedWorkspace)
			}
			return run, resolvedWorkspace, nil
		}
		return run, run.WorkspacePath, nil
	}

	resolvedWorkspace, err := resolveEvolveWorkspacePath(workspacePath)
	if err != nil {
		return model.Run{}, "", fmt.Errorf("resolve workspace: %w", err)
	}
	run, ok, err := st.ActiveRun(ctx, resolvedWorkspace)
	if err != nil {
		return model.Run{}, "", fmt.Errorf("load active run: %w", err)
	}
	if !ok {
		return model.Run{}, "", fmt.Errorf("no active evolve run for workspace: %s", resolvedWorkspace)
	}
	return run, resolvedWorkspace, nil
}

func evolveNodeBranchName(runID, nodeID string) string {
	return fmt.Sprintf("foxctl/evolve/%s/%s", strings.TrimSpace(runID), strings.TrimSpace(nodeID))
}
