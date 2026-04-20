package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
	"github.com/joshka0/foxctl/internal/tooling/evolve/store"
	"github.com/spf13/cobra"
)

const evolveDiffCommandTimeout = 2 * time.Minute

type evolveGitDiffResult struct {
	Diff      string
	Stderr    string
	ExitCode  int
	Truncated bool
	Err       error
}

func newEvolveFrontierCommand() *cobra.Command {
	var (
		workspacePath string
		runID         string
	)

	cmd := &cobra.Command{
		Use:   "frontier",
		Short: "List runnable nodes in deterministic order",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := openEvolveStore(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/frontier", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			run, resolvedWorkspace, err := resolveEvolveRunForExecution(ctx, st, workspacePath, runID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/frontier", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/frontier", string(protocol.ErrorCodeEARG), err.Error())
			}

			frontier, err := st.FrontierNodes(ctx, run.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/frontier", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load frontier nodes: %v", err))
			}

			nodes := make([]map[string]any, 0, len(frontier))
			for _, node := range frontier {
				nodes = append(nodes, evolveNodeSummaryData(node))
			}

			return writeOK(cmd, "evolve/frontier", map[string]any{
				"workspace_path": resolvedWorkspace,
				"run_id":         run.ID,
				"count":          len(nodes),
				"nodes":          nodes,
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&runID, "run", "", "Run id (defaults to active run for workspace)")
	return cmd
}

func newEvolveGetCommand() *cobra.Command {
	var (
		workspacePath string
		runID         string
		nodeID        string
		attemptLimit  int
	)

	cmd := &cobra.Command{
		Use:   "get [node-id]",
		Short: "Get one node with recent attempts and gate outcomes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := openEvolveStore(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/get", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			if attemptLimit < 0 {
				return writeErrorEnvelope(cmd, "evolve/get", string(protocol.ErrorCodeEARG), "--attempt-limit must be >= 0")
			}

			run, resolvedWorkspace, err := resolveEvolveRunForExecution(ctx, st, workspacePath, runID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/get", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/get", string(protocol.ErrorCodeEARG), err.Error())
			}

			resolvedNodeID, err := resolveEvolveNodeID(nodeID, args)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/get", string(protocol.ErrorCodeEARG), err.Error())
			}
			node, err := resolveEvolveNodeByID(ctx, st, run, resolvedNodeID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/get", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/get", string(protocol.ErrorCodeEARG), err.Error())
			}

			nodeGates, err := st.GatesByNode(ctx, node.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/get", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load node gates: %v", err))
			}
			attempts, err := st.AttemptsByNode(ctx, node.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/get", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load attempts: %v", err))
			}

			recentAttempts := evolveRecentAttempts(attempts, attemptLimit)
			attemptViews := make([]map[string]any, 0, len(recentAttempts))
			for _, attempt := range recentAttempts {
				gateResults, gateErr := st.GateResultsByAttempt(ctx, attempt.ID)
				if gateErr != nil {
					return writeErrorEnvelope(cmd, "evolve/get", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load gate results for attempt %s: %v", attempt.ID, gateErr))
				}
				attemptViews = append(attemptViews, evolveAttemptData(attempt, gateResults))
			}

			gateViews := make([]map[string]any, 0, len(nodeGates))
			for _, gate := range nodeGates {
				gateViews = append(gateViews, evolveGateData(gate))
			}

			return writeOK(cmd, "evolve/get", map[string]any{
				"workspace_path": resolvedWorkspace,
				"run_id":         run.ID,
				"node_id":        node.ID,
				"node":           evolveNodeSummaryData(node),
				"node_gates":     gateViews,
				"recent_attempts": map[string]any{
					"count":    len(attemptViews),
					"attempts": attemptViews,
				},
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&runID, "run", "", "Run id (defaults to active run for workspace)")
	cmd.Flags().StringVar(&nodeID, "node", "", "Node id (or provide as positional argument)")
	cmd.Flags().IntVar(&attemptLimit, "attempt-limit", 5, "Maximum number of recent attempts to include (0 disables attempts)")
	return cmd
}

func newEvolvePathCommand() *cobra.Command {
	var (
		workspacePath string
		runID         string
		nodeID        string
	)

	cmd := &cobra.Command{
		Use:   "path [node-id]",
		Short: "Resolve execution path for one node",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := openEvolveStore(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/path", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			run, resolvedWorkspace, err := resolveEvolveRunForExecution(ctx, st, workspacePath, runID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/path", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/path", string(protocol.ErrorCodeEARG), err.Error())
			}

			resolvedNodeID, err := resolveEvolveNodeID(nodeID, args)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/path", string(protocol.ErrorCodeEARG), err.Error())
			}
			node, err := resolveEvolveNodeByID(ctx, st, run, resolvedNodeID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/path", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/path", string(protocol.ErrorCodeEARG), err.Error())
			}

			executionPath, err := resolveEvolveNodeExecutionPath(run, node)
			if err != nil {
				if strings.Contains(err.Error(), "has no execution path") {
					return writeErrorEnvelope(cmd, "evolve/path", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/path", string(protocol.ErrorCodeEARG), fmt.Sprintf("resolve execution path: %v", err))
			}

			pathSource := "worktree"
			if node.Status == model.NodeStatusRoot || strings.TrimSpace(node.ParentID) == "" {
				pathSource = "workspace"
			}

			return writeOK(cmd, "evolve/path", map[string]any{
				"workspace_path": resolvedWorkspace,
				"run_id":         run.ID,
				"node_id":        node.ID,
				"execution_path": executionPath,
				"worktree_path":  strings.TrimSpace(node.WorktreePath),
				"path_source":    pathSource,
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&runID, "run", "", "Run id (defaults to active run for workspace)")
	cmd.Flags().StringVar(&nodeID, "node", "", "Node id (or provide as positional argument)")
	return cmd
}

func newEvolveDiffCommand() *cobra.Command {
	var (
		workspacePath string
		runID         string
		nodeID        string
		unified       int
	)

	cmd := &cobra.Command{
		Use:   "diff [node-id]",
		Short: "Show bounded git diff for one node relative to parent/base",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeERuntime), fmt.Sprintf("configuration not loaded: %v", err))
			}
			st, err := store.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			if unified < 0 || unified > 20 {
				return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeEARG), "--unified must be between 0 and 20")
			}

			run, resolvedWorkspace, err := resolveEvolveRunForExecution(ctx, st, workspacePath, runID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeEARG), err.Error())
			}

			resolvedNodeID, err := resolveEvolveNodeID(nodeID, args)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeEARG), err.Error())
			}
			node, err := resolveEvolveNodeByID(ctx, st, run, resolvedNodeID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeEARG), err.Error())
			}

			executionPath, err := resolveEvolveNodeExecutionPath(run, node)
			if err != nil {
				if strings.Contains(err.Error(), "has no execution path") {
					return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeEARG), fmt.Sprintf("resolve execution path: %v", err))
			}

			baseRef, parentNodeID, err := resolveEvolveDiffBase(ctx, st, node)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeEARG), err.Error())
			}

			diffResult := runEvolveGitDiff(ctx, executionPath, baseRef, unified)
			if diffResult.Err != nil {
				return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeERuntime), diffResult.Err.Error())
			}

			diffArtifact := ""
			if strings.TrimSpace(diffResult.Diff) != "" {
				diffArtifact, err = persistEvolveSingleArtifact(
					ctx,
					cfg.Paths.CAS,
					diffResult.Diff,
					"text/x-diff; charset=utf-8",
					[]string{"evolve", "diff", "node:" + node.ID},
				)
				if err != nil {
					return writeErrorEnvelope(cmd, "evolve/diff", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist diff artifact: %v", err))
				}
			}

			return writeOK(cmd, "evolve/diff", map[string]any{
				"workspace_path": resolvedWorkspace,
				"run_id":         run.ID,
				"node_id":        node.ID,
				"parent_node_id": parentNodeID,
				"execution_path": executionPath,
				"base_ref":       baseRef,
				"unified":        unified,
				"has_changes":    strings.TrimSpace(diffResult.Diff) != "",
				"diff_truncated": diffResult.Truncated,
				"stderr":         diffResult.Stderr,
				"diff":           diffResult.Diff,
				"artifact":       diffArtifact,
			}, "run", profilesCoreAgent)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&runID, "run", "", "Run id (defaults to active run for workspace)")
	cmd.Flags().StringVar(&nodeID, "node", "", "Node id (or provide as positional argument)")
	cmd.Flags().IntVar(&unified, "unified", 3, "Number of unified context lines in git diff output")
	return cmd
}

func resolveEvolveNodeID(flagValue string, args []string) (string, error) {
	resolved := strings.TrimSpace(flagValue)
	if len(args) > 0 {
		argValue := strings.TrimSpace(args[0])
		if argValue == "" {
			return "", fmt.Errorf("node id cannot be empty")
		}
		if resolved != "" && resolved != argValue {
			return "", fmt.Errorf("--node %s does not match positional node id %s", resolved, argValue)
		}
		resolved = argValue
	}
	if resolved == "" {
		return "", fmt.Errorf("node id is required (use --node or positional argument)")
	}
	return resolved, nil
}

func resolveEvolveNodeByID(ctx context.Context, st store.Store, run model.Run, nodeID string) (model.Node, error) {
	node, err := st.Node(ctx, nodeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return model.Node{}, fmt.Errorf("node not found: %s: %w", nodeID, err)
		}
		return model.Node{}, fmt.Errorf("load node: %w", err)
	}
	if node.RunID != run.ID {
		return model.Node{}, fmt.Errorf("node %s does not belong to run %s", node.ID, run.ID)
	}
	return node, nil
}

func resolveEvolveDiffBase(ctx context.Context, st store.Store, node model.Node) (baseRef string, parentNodeID string, err error) {
	parentNodeID = strings.TrimSpace(node.ParentID)
	if parentNodeID == "" {
		baseRef = strings.TrimSpace(node.CommitSHA)
		if baseRef == "" {
			baseRef = "HEAD"
		}
		return baseRef, "", nil
	}

	parent, err := st.Node(ctx, parentNodeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", parentNodeID, fmt.Errorf("parent node not found: %s: %w", parentNodeID, err)
		}
		return "", parentNodeID, fmt.Errorf("load parent node: %w", err)
	}
	if parent.RunID != node.RunID {
		return "", parentNodeID, fmt.Errorf("parent node %s does not belong to run %s", parent.ID, node.RunID)
	}

	baseRef = strings.TrimSpace(parent.CommitSHA)
	if baseRef == "" {
		baseRef = strings.TrimSpace(parent.Branch)
	}
	if baseRef == "" {
		baseRef = strings.TrimSpace(node.CommitSHA)
	}
	if baseRef == "" {
		baseRef = "HEAD"
	}
	return baseRef, parent.ID, nil
}

func runEvolveGitDiff(ctx context.Context, cwd, baseRef string, unified int) evolveGitDiffResult {
	runCtx, cancel := context.WithTimeout(ctx, evolveDiffCommandTimeout)
	defer cancel()

	args := []string{
		"-c", "core.pager=cat",
		"diff",
		"--no-ext-diff",
		"--minimal",
		"--unified=" + strconv.Itoa(unified),
	}
	baseRef = strings.TrimSpace(baseRef)
	if baseRef != "" {
		args = append(args, baseRef)
	}
	args = append(args, "--")

	cmd := exec.CommandContext(runCtx, "git", args...) //nolint:gosec // explicit argv with fixed command shape
	cmd.Dir = filepath.Clean(cwd)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")

	stdoutBuffer := newEvolveBoundedBuffer(evolveRunOutputLimitByte)
	stderrBuffer := newEvolveBoundedBuffer(evolveRunOutputLimitByte)
	cmd.Stdout = stdoutBuffer
	cmd.Stderr = stderrBuffer

	runErr := cmd.Run()
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		runErr = fmt.Errorf("git diff timed out after %s", evolveDiffCommandTimeout)
	} else if runErr != nil {
		runErr = fmt.Errorf("git diff failed with exit code %d", exitCode)
	}

	diffText := stdoutBuffer.String()
	stderrText := stderrBuffer.String()
	if stdoutBuffer.Truncated() {
		diffText = evolveAppendTruncationNotice(diffText)
	}
	if stderrBuffer.Truncated() {
		stderrText = evolveAppendTruncationNotice(stderrText)
	}

	return evolveGitDiffResult{
		Diff:      diffText,
		Stderr:    stderrText,
		ExitCode:  exitCode,
		Truncated: stdoutBuffer.Truncated(),
		Err:       runErr,
	}
}

func evolveNodeSummaryData(node model.Node) map[string]any {
	return map[string]any{
		"id":                 node.ID,
		"run_id":             node.RunID,
		"parent_id":          node.ParentID,
		"status":             node.Status,
		"hypothesis":         node.Hypothesis,
		"score":              node.Score,
		"eval_epoch":         node.EvalEpoch,
		"branch":             node.Branch,
		"worktree_path":      node.WorktreePath,
		"commit_sha":         node.CommitSHA,
		"pruned_reason":      node.PrunedReason,
		"current_attempt":    node.CurrentAttempt,
		"evaluated_attempts": node.EvaluatedAttempts,
		"created_at":         node.CreatedAt,
		"updated_at":         node.UpdatedAt,
	}
}

func evolveGateData(gate model.Gate) map[string]any {
	return map[string]any{
		"id":             gate.ID,
		"run_id":         gate.RunID,
		"node_id":        gate.NodeID,
		"name":           gate.Name,
		"command":        gate.Command,
		"defined_at":     gate.CreatedAt,
		"source_node_id": gate.NodeID,
	}
}

func evolveAttemptData(attempt model.Attempt, gateResults []model.GateResult) map[string]any {
	results := make([]map[string]any, 0, len(gateResults))
	for _, result := range gateResults {
		results = append(results, map[string]any{
			"attempt_id":     result.AttemptID,
			"gate_name":      result.GateName,
			"source_node_id": result.SourceNodeID,
			"passed":         result.Passed,
			"return_code":    result.ReturnCode,
			"log_artifact":   result.LogArtifact,
		})
	}

	return map[string]any{
		"id":                 attempt.ID,
		"attempt_no":         attempt.AttemptNo,
		"status":             attempt.Status,
		"score":              attempt.Score,
		"benchmark_artifact": attempt.BenchmarkArtifact,
		"trace_artifact":     attempt.TraceArtifact,
		"diff_artifact":      attempt.DiffArtifact,
		"error":              attempt.Error,
		"started_at":         attempt.StartedAt,
		"finished_at":        attempt.FinishedAt,
		"gate_results":       results,
	}
}

func evolveRecentAttempts(all []model.Attempt, limit int) []model.Attempt {
	if limit == 0 || len(all) == 0 {
		return nil
	}
	if limit < 0 || limit > len(all) {
		limit = len(all)
	}

	recent := make([]model.Attempt, 0, limit)
	for i := len(all) - 1; i >= 0 && len(recent) < limit; i-- {
		recent = append(recent, all[i])
	}
	return recent
}
