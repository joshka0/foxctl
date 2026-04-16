package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
	"github.com/joshka0/foxctl/internal/tooling/evolve/store"
	"github.com/joshka0/foxctl/internal/tooling/shellreduce"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

const (
	evolveRunCommandTimeout  = 10 * time.Minute
	evolveRunOutputLimitByte = 64 * 1024
)

var evolveScoreTokenPattern = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)

type evolveInheritedGate struct {
	Gate         model.Gate
	SourceNodeID string
}

type evolveCommandExecution struct {
	Stdout    string
	Stderr    string
	Combined  string
	ExitCode  int
	Truncated bool
	Err       error
}

type evolveGateExecutionSummary struct {
	GateName     string
	SourceNodeID string
	Passed       bool
	ReturnCode   *int
	LogArtifact  string
	Error        string
}

func newEvolveRunCommand() *cobra.Command {
	var (
		workspacePath string
		runID         string
		nodeID        string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run benchmark and inherited gates for an evolve node",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeERuntime), fmt.Sprintf("configuration not loaded: %v", err))
			}

			st, err := store.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open evolve store: %v", err))
			}
			defer func() { _ = st.Close() }()

			run, resolvedWorkspace, err := resolveEvolveRunForExecution(ctx, st, workspacePath, runID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeEARG), err.Error())
			}

			node, err := resolveEvolveNodeForExecution(ctx, st, run, nodeID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeENotFound), err.Error())
				}
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeEARG), err.Error())
			}

			executionPath, err := resolveEvolveNodeExecutionPath(run, node)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeEARG), fmt.Sprintf("resolve execution path: %v", err))
			}

			allNodes, err := st.NodesByRun(ctx, run.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load nodes: %v", err))
			}
			allGates, err := st.GatesByRun(ctx, run.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load gates: %v", err))
			}
			inheritedGates, err := inheritEvolveGates(node, allNodes, allGates)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeERuntime), fmt.Sprintf("collect inherited gates: %v", err))
			}

			attempts, err := st.AttemptsByNode(ctx, node.ID)
			if err != nil {
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeERuntime), fmt.Sprintf("load attempts: %v", err))
			}

			now := time.Now().UTC()
			attemptNo := nextEvolveAttemptNo(node, attempts)
			attempt := model.Attempt{
				ID:        ulid.Make().String(),
				NodeID:    node.ID,
				AttemptNo: attemptNo,
				Status:    model.AttemptStatusActive,
				StartedAt: now,
			}
			if err := st.SaveAttempt(ctx, attempt); err != nil {
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist active attempt: %v", err))
			}

			benchmarkExec := executeEvolveCommand(ctx, executionPath, run.BenchmarkCommand)
			benchmarkArtifact, stdoutArtifact, stderrArtifact, artifactErr := persistEvolveExecutionArtifacts(
				ctx,
				cfg.Paths.CAS,
				benchmarkExec,
				[]string{"evolve", "run", "benchmark", "node:" + node.ID},
			)
			if artifactErr != nil {
				benchmarkExec.Err = firstEvolveError(benchmarkExec.Err, fmt.Errorf("persist benchmark artifacts: %w", artifactErr))
			}

			score, parseErr := parseEvolveScore(benchmarkExec.Combined)
			if benchmarkExec.Err != nil {
				parseErr = nil
			}

			gateSummaries := make([]evolveGateExecutionSummary, 0, len(inheritedGates))
			gatePassed := 0
			gateFailed := 0
			if benchmarkExec.Err == nil && parseErr == nil {
				for _, inherited := range inheritedGates {
					gateExec := executeEvolveCommand(ctx, executionPath, inherited.Gate.Command)
					logArtifact, persistErr := persistEvolveSingleArtifact(
						ctx,
						cfg.Paths.CAS,
						gateExec.Combined,
						"text/plain; charset=utf-8",
						[]string{"evolve", "run", "gate", "node:" + node.ID, "gate:" + inherited.Gate.Name},
					)
					if persistErr != nil {
						gateExec.Err = firstEvolveError(gateExec.Err, fmt.Errorf("persist gate log artifact for %s: %w", inherited.Gate.Name, persistErr))
					}

					passed := gateExec.Err == nil
					if passed {
						gatePassed++
					} else {
						gateFailed++
					}
					returnCode := evolveReturnCode(gateExec.ExitCode)
					result := model.GateResult{
						AttemptID:    attempt.ID,
						GateName:     inherited.Gate.Name,
						SourceNodeID: inherited.SourceNodeID,
						Passed:       passed,
						ReturnCode:   returnCode,
						LogArtifact:  logArtifact,
					}
					if err := st.SaveGateResult(ctx, result); err != nil {
						return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist gate result %s: %v", inherited.Gate.Name, err))
					}

					summary := evolveGateExecutionSummary{
						GateName:     inherited.Gate.Name,
						SourceNodeID: inherited.SourceNodeID,
						Passed:       passed,
						ReturnCode:   returnCode,
						LogArtifact:  logArtifact,
					}
					if gateExec.Err != nil {
						summary.Error = gateExec.Err.Error()
					}
					gateSummaries = append(gateSummaries, summary)
				}
			}

			attemptScore := (*float64)(nil)
			if benchmarkExec.Err == nil && parseErr == nil {
				attemptScore = &score
			}
			attempt.Status = decideEvolveAttemptStatus(benchmarkExec.Err, parseErr, gateFailed)
			attempt.Score = attemptScore
			attempt.BenchmarkArtifact = benchmarkArtifact
			attempt.TraceArtifact = stdoutArtifact
			attempt.DiffArtifact = stderrArtifact
			attempt.Error = evolveAttemptErrorMessage(benchmarkExec.Err, parseErr, gateFailed)
			attempt.FinishedAt = time.Now().UTC()
			if err := st.SaveAttempt(ctx, attempt); err != nil {
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist finalized attempt: %v", err))
			}

			node.CurrentAttempt = attemptNo
			node.EvaluatedAttempts++
			node.EvalEpoch++
			node.UpdatedAt = attempt.FinishedAt
			node.Status = decideEvolveNodeStatus(attempt.Status)
			if attemptScore != nil {
				node.Score = attemptScore
			}
			if err := st.SaveNode(ctx, node); err != nil {
				return writeErrorEnvelope(cmd, "evolve/run", string(protocol.ErrorCodeERuntime), fmt.Sprintf("persist node after attempt: %v", err))
			}

			data := map[string]any{
				"workspace_path":   resolvedWorkspace,
				"run_id":           run.ID,
				"node_id":          node.ID,
				"attempt_id":       attempt.ID,
				"status":           node.Status,
				"attempt_status":   attempt.Status,
				"score":            attempt.Score,
				"metric":           run.Metric,
				"execution_path":   executionPath,
				"output_truncated": benchmarkExec.Truncated,
				"artifacts": map[string]any{
					"benchmark": benchmarkArtifact,
					"stdout":    stdoutArtifact,
					"stderr":    stderrArtifact,
				},
				"gate_result_summary": map[string]any{
					"total":     len(inheritedGates),
					"evaluated": len(gateSummaries),
					"passed":    gatePassed,
					"failed":    gateFailed,
					"results":   gateSummaries,
				},
			}

			if attempt.Status == model.AttemptStatusCompleted {
				return writeOK(cmd, "evolve/run", data, "run", profilesCoreAgent)
			}

			return writeEvolveRunErrorEnvelope(
				cmd,
				decideEvolveFailureCode(benchmarkExec.Err, parseErr, gateFailed),
				evolveAttemptErrorMessage(benchmarkExec.Err, parseErr, gateFailed),
				data,
			)
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (defaults to detected workspace)")
	cmd.Flags().StringVar(&runID, "run", "", "Run id (defaults to active run for workspace)")
	cmd.Flags().StringVar(&nodeID, "node", "", "Node id to execute (defaults to first runnable node)")
	return cmd
}

func resolveEvolveRunForExecution(ctx context.Context, st store.Store, workspacePath, runID string) (model.Run, string, error) {
	runID = strings.TrimSpace(runID)
	if runID != "" {
		run, err := st.Run(ctx, runID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return model.Run{}, "", fmt.Errorf("run not found: %s: %w", runID, err)
			}
			return model.Run{}, "", fmt.Errorf("load run: %w", err)
		}
		if strings.TrimSpace(workspacePath) == "" {
			return run, run.WorkspacePath, nil
		}
		resolvedWorkspace, err := resolveEvolveWorkspacePath(workspacePath)
		if err != nil {
			return model.Run{}, "", fmt.Errorf("resolve workspace: %w", err)
		}
		if resolvedWorkspace != run.WorkspacePath {
			return model.Run{}, "", fmt.Errorf("run %s belongs to workspace %s (got %s)", runID, run.WorkspacePath, resolvedWorkspace)
		}
		return run, resolvedWorkspace, nil
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
		return model.Run{}, "", fmt.Errorf("no active evolve run for workspace: %s: %w", resolvedWorkspace, store.ErrNotFound)
	}
	return run, resolvedWorkspace, nil
}

func resolveEvolveNodeForExecution(ctx context.Context, st store.Store, run model.Run, nodeID string) (model.Node, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID != "" {
		node, err := st.Node(ctx, nodeID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return model.Node{}, fmt.Errorf("node not found: %s: %w", nodeID, err)
			}
			return model.Node{}, fmt.Errorf("load node: %w", err)
		}
		if node.RunID != run.ID {
			return model.Node{}, fmt.Errorf("node %s does not belong to run %s", nodeID, run.ID)
		}
		return node, nil
	}

	frontier, err := st.FrontierNodes(ctx, run.ID)
	if err != nil {
		return model.Node{}, fmt.Errorf("load frontier nodes: %w", err)
	}
	if len(frontier) > 0 {
		return frontier[0], nil
	}

	nodes, err := st.NodesByRun(ctx, run.ID)
	if err != nil {
		return model.Node{}, fmt.Errorf("load run nodes: %w", err)
	}
	for _, candidate := range nodes {
		if candidate.Status == model.NodeStatusRoot {
			return candidate, nil
		}
	}
	if len(nodes) > 0 {
		return nodes[0], nil
	}
	return model.Node{}, fmt.Errorf("no runnable node found for run %s: %w", run.ID, store.ErrNotFound)
}

func resolveEvolveNodeExecutionPath(run model.Run, node model.Node) (string, error) {
	raw := strings.TrimSpace(node.WorktreePath)
	if raw == "" {
		if node.Status == model.NodeStatusRoot || strings.TrimSpace(node.ParentID) == "" {
			raw = strings.TrimSpace(run.WorkspacePath)
		}
	}
	if raw == "" {
		return "", fmt.Errorf("node %s has no execution path", node.ID)
	}
	abs, err := filepath.Abs(raw)
	if err == nil {
		raw = abs
	}
	return filepath.Clean(raw), nil
}

func nextEvolveAttemptNo(node model.Node, attempts []model.Attempt) int {
	next := node.CurrentAttempt + 1
	if next < 1 {
		next = 1
	}
	if len(attempts) == 0 {
		return next
	}
	last := attempts[len(attempts)-1].AttemptNo + 1
	if last > next {
		return last
	}
	return next
}

func inheritEvolveGates(node model.Node, nodes []model.Node, gates []model.Gate) ([]evolveInheritedGate, error) {
	nodeByID := make(map[string]model.Node, len(nodes))
	for _, item := range nodes {
		nodeByID[item.ID] = item
	}
	if _, ok := nodeByID[node.ID]; !ok {
		nodeByID[node.ID] = node
	}
	ancestry, err := evolveNodeAncestry(node.ID, nodeByID)
	if err != nil {
		return nil, err
	}

	gatesByNode := make(map[string][]model.Gate)
	for _, gate := range gates {
		gatesByNode[gate.NodeID] = append(gatesByNode[gate.NodeID], gate)
	}

	ordered := make([]evolveInheritedGate, 0, len(gates))
	indexByName := make(map[string]int)
	for _, ancestor := range ancestry {
		for _, gate := range gatesByNode[ancestor.ID] {
			resolved := evolveInheritedGate{Gate: gate, SourceNodeID: ancestor.ID}
			if idx, exists := indexByName[gate.Name]; exists {
				ordered[idx] = resolved
				continue
			}
			indexByName[gate.Name] = len(ordered)
			ordered = append(ordered, resolved)
		}
	}
	return ordered, nil
}

func evolveNodeAncestry(nodeID string, nodesByID map[string]model.Node) ([]model.Node, error) {
	current, ok := nodesByID[nodeID]
	if !ok {
		return nil, fmt.Errorf("node %s missing from run graph", nodeID)
	}

	chain := []model.Node{current}
	seen := map[string]struct{}{current.ID: {}}
	for strings.TrimSpace(current.ParentID) != "" {
		parent, ok := nodesByID[current.ParentID]
		if !ok {
			return nil, fmt.Errorf("node %s parent %s missing from run graph", current.ID, current.ParentID)
		}
		if _, visited := seen[parent.ID]; visited {
			return nil, fmt.Errorf("cycle detected in run graph at node %s", parent.ID)
		}
		seen[parent.ID] = struct{}{}
		chain = append(chain, parent)
		current = parent
	}

	reverseEvolveNodes(chain)
	return chain, nil
}

func reverseEvolveNodes(nodes []model.Node) {
	for left, right := 0, len(nodes)-1; left < right; left, right = left+1, right-1 {
		nodes[left], nodes[right] = nodes[right], nodes[left]
	}
}

func executeEvolveCommand(ctx context.Context, cwd, commandText string) evolveCommandExecution {
	commandText = strings.TrimSpace(commandText)
	if commandText == "" {
		return evolveCommandExecution{Err: fmt.Errorf("command is empty")}
	}

	argv, err := shellreduce.SplitCommand(commandText)
	if err != nil {
		return evolveCommandExecution{Err: fmt.Errorf("parse command: %w", err)}
	}
	if len(argv) == 0 {
		return evolveCommandExecution{Err: fmt.Errorf("command has no executable")}
	}

	runCtx, cancel := context.WithTimeout(ctx, evolveRunCommandTimeout)
	defer cancel()

	command := exec.CommandContext(runCtx, argv[0], argv[1:]...) //nolint:gosec // explicit benchmark/gate command from evolve run config
	command.Dir = cwd

	stdoutBuffer := newEvolveBoundedBuffer(evolveRunOutputLimitByte)
	stderrBuffer := newEvolveBoundedBuffer(evolveRunOutputLimitByte)
	command.Stdout = stdoutBuffer
	command.Stderr = stderrBuffer

	runErr := command.Run()
	exitCode := -1
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		runErr = fmt.Errorf("command timed out after %s", evolveRunCommandTimeout)
	} else if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			runErr = fmt.Errorf("command failed with exit code %d", exitCode)
		}
	}

	stdoutText := stdoutBuffer.String()
	stderrText := stderrBuffer.String()
	combinedText := evolveJoinOutputs(stdoutText, stderrText)
	if stdoutBuffer.Truncated() || stderrBuffer.Truncated() {
		combinedText = evolveAppendTruncationNotice(combinedText)
	}

	return evolveCommandExecution{
		Stdout:    stdoutText,
		Stderr:    stderrText,
		Combined:  combinedText,
		ExitCode:  exitCode,
		Truncated: stdoutBuffer.Truncated() || stderrBuffer.Truncated(),
		Err:       runErr,
	}
}

func parseEvolveScore(output string) (float64, error) {
	matches := evolveScoreTokenPattern.FindAllString(output, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("benchmark output did not contain a numeric score")
	}
	last := matches[len(matches)-1]
	score, err := strconvParseFloat(last)
	if err != nil {
		return 0, fmt.Errorf("parse score token %q: %w", last, err)
	}
	return score, nil
}

func persistEvolveExecutionArtifacts(
	ctx context.Context,
	casRoot string,
	execution evolveCommandExecution,
	tags []string,
) (benchmarkDigest string, stdoutDigest string, stderrDigest string, err error) {
	benchmarkDigest, err = persistEvolveSingleArtifact(ctx, casRoot, execution.Combined, "text/plain; charset=utf-8", append([]string{}, tags...))
	if err != nil {
		return "", "", "", err
	}
	stdoutDigest, err = persistEvolveSingleArtifact(ctx, casRoot, execution.Stdout, "text/plain; charset=utf-8", append([]string{}, tags...))
	if err != nil {
		return "", "", "", err
	}
	stderrDigest, err = persistEvolveSingleArtifact(ctx, casRoot, execution.Stderr, "text/plain; charset=utf-8", append([]string{}, tags...))
	if err != nil {
		return "", "", "", err
	}
	return benchmarkDigest, stdoutDigest, stderrDigest, nil
}

func persistEvolveSingleArtifact(ctx context.Context, casRoot, content, contentType string, tags []string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", nil
	}
	casStore, err := cas.NewStore(casRoot)
	if err != nil {
		return "", fmt.Errorf("open cas store: %w", err)
	}
	defer casStore.Close()

	obj, err := casStore.Put(ctx, strings.NewReader(content), contentType, tags)
	if err != nil {
		return "", fmt.Errorf("write cas artifact: %w", err)
	}
	return obj.Digest, nil
}

func decideEvolveAttemptStatus(benchmarkErr, parseErr error, gateFailed int) model.AttemptStatus {
	if benchmarkErr != nil || parseErr != nil || gateFailed > 0 {
		return model.AttemptStatusFailed
	}
	return model.AttemptStatusCompleted
}

func decideEvolveNodeStatus(attemptStatus model.AttemptStatus) model.NodeStatus {
	if attemptStatus == model.AttemptStatusCompleted {
		return model.NodeStatusEvaluated
	}
	return model.NodeStatusFailed
}

func decideEvolveFailureCode(benchmarkErr, parseErr error, gateFailed int) protocol.ErrorCode {
	if parseErr != nil {
		return protocol.ErrorCodeEParse
	}
	if benchmarkErr != nil || gateFailed > 0 {
		return protocol.ErrorCodeERuntime
	}
	return protocol.ErrorCodeERuntime
}

func evolveAttemptErrorMessage(benchmarkErr, parseErr error, gateFailed int) string {
	if benchmarkErr != nil {
		return benchmarkErr.Error()
	}
	if parseErr != nil {
		return parseErr.Error()
	}
	if gateFailed > 0 {
		return fmt.Sprintf("%d gate(s) failed", gateFailed)
	}
	return ""
}

func evolveReturnCode(exitCode int) *int {
	if exitCode < 0 {
		return nil
	}
	code := exitCode
	return &code
}

func writeEvolveRunErrorEnvelope(cmd *cobra.Command, code protocol.ErrorCode, message string, data map[string]any) error {
	env := envelope.Error("evolve/run", string(code), message, data)
	if err := envelope.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s", message)
}

func firstEvolveError(current error, candidate error) error {
	if current != nil {
		return current
	}
	return candidate
}

func evolveJoinOutputs(stdoutText, stderrText string) string {
	switch {
	case stdoutText == "" && stderrText == "":
		return ""
	case stdoutText == "":
		return stderrText
	case stderrText == "":
		return stdoutText
	default:
		return stdoutText + "\n" + stderrText
	}
}

func evolveAppendTruncationNotice(content string) string {
	notice := "... (truncated)"
	if strings.TrimSpace(content) == "" {
		return notice
	}
	if strings.HasSuffix(content, "\n") {
		return content + notice
	}
	return content + "\n" + notice
}

type evolveBoundedBuffer struct {
	maxBytes  int
	buffer    bytes.Buffer
	truncated bool
}

func newEvolveBoundedBuffer(maxBytes int) *evolveBoundedBuffer {
	return &evolveBoundedBuffer{maxBytes: maxBytes}
}

func (b *evolveBoundedBuffer) Write(p []byte) (int, error) {
	if b.maxBytes <= 0 {
		b.truncated = true
		return len(p), nil
	}
	remaining := b.maxBytes - b.buffer.Len()
	switch {
	case remaining <= 0:
		b.truncated = true
		return len(p), nil
	case len(p) <= remaining:
		_, _ = b.buffer.Write(p)
		return len(p), nil
	default:
		_, _ = b.buffer.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
}

func (b *evolveBoundedBuffer) String() string {
	return b.buffer.String()
}

func (b *evolveBoundedBuffer) Truncated() bool {
	return b.truncated
}

func strconvParseFloat(value string) (float64, error) {
	return strconv.ParseFloat(value, 64)
}
