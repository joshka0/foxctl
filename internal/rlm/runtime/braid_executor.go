package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/rlm/runtime/generalsolver"
	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// braidNodeHelperBudget tracks cumulative helper wall-clock time and attempt
// count per braid node so that repair attempts get bounded sub-timeouts derived
// from remaining budget instead of inheriting an almost-expired context.
type braidNodeHelperBudget struct {
	CumulativeDuration time.Duration
	Attempts           int
}

const (
	// braidNodeHelperDefaultBudget is the default wall-clock budget allocated to
	// helper attempts for a single node when no explicit timeout is configured.
	braidNodeHelperDefaultBudget = 12 * time.Minute

	// braidNodeHelperMinSubTimeout is the minimum sub-timeout for a repair
	// attempt. If the remaining budget falls below this floor, the attempt is
	// skipped with a deadline_exhausted verdict.
	braidNodeHelperMinSubTimeout = 10 * time.Second

	// braidRepairFeedbackCap limits repair feedback text to avoid pushing the
	// next attempt's input over the token budget.
	braidRepairFeedbackCap = 1800

	// braidDeadlineExhaustedPrefix marks feedback produced when a repair attempt
	// is skipped because cumulative helper duration consumed the node budget.
	braidDeadlineExhaustedPrefix = "deadline_exhausted"
)

// braidHelperStage enumerates the stages of a helper attempt lifecycle.
type braidHelperStage string

const (
	braidHelperStageDraft    braidHelperStage = "draft"
	braidHelperStageParse    braidHelperStage = "parse"
	braidHelperStageValidate braidHelperStage = "validate"
	braidHelperStageRun      braidHelperStage = "run"
	braidHelperStageVerify   braidHelperStage = "verify"
)

// helperBudgetByNode tracks per-node helper budgets during graph execution.
type helperBudgetByNode map[string]*braidNodeHelperBudget

func (m helperBudgetByNode) get(nodeID string) *braidNodeHelperBudget {
	budget, ok := m[nodeID]
	if !ok {
		budget = &braidNodeHelperBudget{}
		m[nodeID] = budget
	}
	return budget
}

type braidNodeExecutionRecord struct {
	Summary       string
	Artifact      braidNodeArtifact
	Source        string
	Certification *RuntimeCertification
}

type RuntimeCertification struct {
	NodeID          string           `json:"node_id"`
	Pass            bool             `json:"pass"`
	VerifierID      string           `json:"verifier_id"`
	VerifierKind    string           `json:"verifier_kind"`
	ScaffoldClass   string           `json:"scaffold_class,omitempty"`
	ScaffoldID      string           `json:"scaffold_id,omitempty"`
	CandidateDigest string           `json:"candidate_digest,omitempty"`
	InputDigest     string           `json:"input_digest,omitempty"`
	Failure         *NodeRepairCause `json:"failure,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
}

const (
	runtimeVerifierKindScaffold       = "runtime_scaffold"
	runtimeVerifierKindForward        = "runtime_forward"
	runtimeVerifierKindReducer        = "runtime_reducer"
	runtimeVerifierKindExpectedAnswer = "runtime_expected_answer"
)

type NodeRepairCause struct {
	FailureKind   string         `json:"failure_kind"`
	FailedNode    string         `json:"failed_node,omitempty"`
	FailedStep    int            `json:"failed_step,omitempty"`
	FirstFailure  string         `json:"first_failure"`
	Observed      any            `json:"observed,omitempty"`
	Expected      any            `json:"expected,omitempty"`
	Candidate     string         `json:"candidate,omitempty"`
	InputKeys     []string       `json:"input_keys,omitempty"`
	ExpectedInput string         `json:"expected_input,omitempty"`
	RepairHint    string         `json:"repair_hint,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// remainingSubTimeout returns the bounded sub-timeout for the next attempt. If
// the remaining budget is below the minimum floor, it returns 0 to signal that
// the attempt should be skipped.
func (b *braidNodeHelperBudget) remainingSubTimeout(totalBudget time.Duration) time.Duration {
	remaining := totalBudget - b.CumulativeDuration
	if remaining < braidNodeHelperMinSubTimeout {
		return 0
	}
	return remaining
}

// formatDeadlineExhausted produces a structured feedback string for a skipped
// repair attempt.
func formatDeadlineExhausted(nodeID string, budget *braidNodeHelperBudget) string {
	return fmt.Sprintf("%s: node=%s cumulative_ms=%d attempts=%d",
		braidDeadlineExhaustedPrefix, nodeID,
		budget.CumulativeDuration.Milliseconds(),
		budget.Attempts,
	)
}

// capBraidRepairFeedback truncates repair feedback to the budget cap.
func capBraidRepairFeedback(feedback string) string {
	if len(feedback) <= braidRepairFeedbackCap {
		return feedback
	}
	if braidRepairFeedbackCap < 32 {
		return feedback[:braidRepairFeedbackCap]
	}
	return feedback[:braidRepairFeedbackCap-15] + "...[truncated]"
}

func executePhaseBraidGraph(
	ctx context.Context,
	phaseName string,
	phase REPLRunnerPhase,
	toolExec *replToolExecutor,
	graph *BraidGraph,
	rootPrompt string,
	maxSubcalls int,
	output *engine.EngineOutput,
) error {
	if !phase.AutoExecuteGraphNodes || phase.Final || output == nil {
		return nil
	}
	if graph == nil {
		return fmt.Errorf("rlm repl runner phase %q requires braid graph state before auto execution", phaseName)
	}
	if toolExec == nil || (!toolExec.allowAsyncRLMTools() && !toolExec.allowRLMQueryTool()) {
		return fmt.Errorf("rlm repl runner phase %q cannot auto-execute braid graph without %s", phaseName, RLMQueryToolName)
	}

	if toolExec.recorder != nil {
		toolExec.recorder.RecordBraidEvent(BraidEvent{
			Phase:     phaseName,
			Status:    "accepted",
			FinalNode: graph.FinalNode,
			NodeCount: len(graph.Nodes),
		})
	}

	summaries := map[string]string{}
	executionRecords := map[string]braidNodeExecutionRecord{}
	executed := map[string]struct{}{}
	repairFeedbackByNode := map[string]string{}
	helperBudgets := make(helperBudgetByNode, len(graph.Nodes))
	// Apply structural splits to the graph before execution so that
	// readyBraidNodes operates on the decomposed graph, not the original.
	beforeRuntimeSplits := cloneBraidGraph(*graph)
	applyBraidGraphSplits(graph, toolExec, phaseName)
	if toolExec != nil {
		recordBraidGraphRewriteIfChanged(toolExec.recorder, phase, "graph_runtime_split", beforeRuntimeSplits, *graph)
	}
	if err := validateBraidGraphAfterRuntimeRewrite(phase, *graph); err != nil {
		return fmt.Errorf("rlm repl runner phase %q: rewritten braid graph invalid: %w", phaseName, err)
	}
	nodesByID := make(map[string]BraidNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodesByID[node.ID] = node
	}
	solverState := seedSolverStateFromBraidGraph(graph)
	repairAttempts := 0
	submitted := 0
	wave := 0
graphLoop:
	for len(executed) < len(graph.Nodes) {
		nodesByID = make(map[string]BraidNode, len(graph.Nodes))
		for _, node := range graph.Nodes {
			nodesByID[node.ID] = node
		}
		if rewired := applyAdaptiveRouterSummaryDependencies(graph, summaries, executed); len(rewired) > 0 {
			for _, nodeID := range rewired {
				if node, ok := braidGraphNodeByID(*graph, nodeID); ok {
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "router_dependencies_applied", strings.Join(node.DependsOn, ", "))
				}
			}
			if err := validateBraidGraphAfterRuntimeRewrite(phase, *graph); err != nil {
				return fmt.Errorf("rlm repl runner phase %q: adaptive braid graph invalid: %w", phaseName, err)
			}
			nodesByID = make(map[string]BraidNode, len(graph.Nodes))
			for _, node := range graph.Nodes {
				nodesByID[node.ID] = node
			}
		}
		ready := readyBraidNodes(*graph, executed, summaries)
		if len(ready) == 0 {
			return fmt.Errorf("rlm repl runner phase %q: braid graph stalled before final_node %q", phaseName, graph.FinalNode)
		}
		wave++

		if !toolExec.allowAsyncRLMTools() {
			for _, node := range ready {
				recordBraidNodeEvent(toolExec, phaseName, wave, node, "ready", "")
				deps := dependencySummarySubset(node, summaries)
				if reason := braidRuntimeMergeBlockReason(node, deps); reason != "" {
					err := fmt.Errorf("rlm repl runner phase %q: braid node %q merge blocked: %s", phaseName, node.ID, reason)
					recordSolverFailure(solverState, node.ID, "merge", err.Error())
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "merge_blocked", reason)
					return err
				}
				if summary, cert, ok := runBraidRuntimeNodeShortcut(node, deps, nodesByID, executionRecords); ok {
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "runtime_shortcut", summary)
					summaries[node.ID] = summary
					recordBraidNodeExecution(executionRecords, node.ID, summary, "runtime", cert)
					executed[node.ID] = struct{}{}
					commitSolverArtifact(solverState, node.ID, summary)
					continue
				}
				if summary, handled, err := runBraidNodeHelperFirst(ctx, phaseName, node, rootPrompt, deps, repairFeedbackByNode[node.ID], phase.DisableHelperFirstFallback, toolExec, output, graph, helperBudgets.get(node.ID)); handled {
					if err != nil {
						recordSolverFailure(solverState, node.ID, "helper_first", err.Error())
						if prepareBraidRepair(phase, graph, node, summary, summaries, executed, repairFeedbackByNode, &repairAttempts) {
							recordBraidNodeEvent(toolExec, phaseName, wave, node, "repairing", err.Error())
							continue graphLoop
						}
						recordBraidNodeEvent(toolExec, phaseName, wave, node, "rejected", err.Error())
						return err
					}
					summaries[node.ID] = summary
					recordBraidNodeExecution(executionRecords, node.ID, summary, "helper", certificationFromBraidSummary(node, summary))
					executed[node.ID] = struct{}{}
					commitSolverArtifact(solverState, node.ID, summary)
					continue
				}
				if maxSubcalls > 0 && submitted+1 > maxSubcalls {
					return fmt.Errorf("rlm repl runner phase %q: braid graph needs %d subcalls, exceeds max %d", phaseName, submitted+1, maxSubcalls)
				}
				summary, err := runDirectBraidNode(ctx, phaseName, node, rootPrompt, deps, capBraidRepairFeedback(repairFeedbackByNode[node.ID]), toolExec, output)
				if err != nil {
					return err
				}
				recordBraidNodeEvent(toolExec, phaseName, wave, node, "completed", summary)
				if normalized, ok := normalizeBraidAdaptiveTargetSummary(node, summary); ok {
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "normalized", normalized)
					summary = normalized
				}
				concreteVerifyFailure := false
				if normalized, ok := normalizeBraidVerificationFailureSummary(node, summary); ok {
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "verification_failed", normalized)
					summary = normalized
					concreteVerifyFailure = true
				}
				summaries[node.ID] = summary
				recordBraidNodeExecution(executionRecords, node.ID, summary, "child", nil)
				executed[node.ID] = struct{}{}
				if !concreteVerifyFailure {
					if recovered, ok := recoverBraidNodeWithHelper(ctx, phaseName, node, rootPrompt, dependencySummarySubset(node, summaries), repairFeedbackByNode[node.ID], summary, toolExec, output); ok {
						recordBraidNodeEvent(toolExec, phaseName, wave, node, "helper_recovered", recovered)
						summaries[node.ID] = recovered
						summary = recovered
						recordBraidNodeExecution(executionRecords, node.ID, summary, "helper_recovered", certificationFromBraidSummary(node, summary))
					}
				}
				submitted++
				if err := validateBraidNodeExecutionRecordInGraph(phaseName, node, executionRecords[node.ID], graph.FinalNode, graph); err != nil {
					if !concreteVerifyFailure {
						if recovered, ok := recoverBraidNodeWithHelper(ctx, phaseName, node, rootPrompt, dependencySummarySubset(node, summaries), repairFeedbackByNode[node.ID], summary, toolExec, output); ok {
							recordBraidNodeEvent(toolExec, phaseName, wave, node, "helper_recovered", recovered)
							summaries[node.ID] = recovered
							recordBraidNodeExecution(executionRecords, node.ID, recovered, "helper_recovered", certificationFromBraidSummary(node, recovered))
							if recoveredErr := validateBraidNodeExecutionRecordInGraph(phaseName, node, executionRecords[node.ID], graph.FinalNode, graph); recoveredErr == nil {
								commitSolverArtifact(solverState, node.ID, recovered)
								continue
							} else {
								err = recoveredErr
							}
						}
					}
					recordSolverFailure(solverState, node.ID, "validate", err.Error())
					if prepareBraidRepair(phase, graph, node, summary, summaries, executed, repairFeedbackByNode, &repairAttempts) {
						recordBraidNodeEvent(toolExec, phaseName, wave, node, "repairing", err.Error())
						continue graphLoop
					}
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "rejected", err.Error())
					return err
				}
				commitSolverArtifact(solverState, node.ID, summary)
			}
			continue
		}

		waveChildren := make([]int, 0, len(ready))
		waveNodeIDs := make([]string, 0, len(ready))
		for _, node := range ready {
			recordBraidNodeEvent(toolExec, phaseName, wave, node, "ready", "")
			deps := dependencySummarySubset(node, summaries)
			if reason := braidRuntimeMergeBlockReason(node, deps); reason != "" {
				err := fmt.Errorf("rlm repl runner phase %q: braid node %q merge blocked: %s", phaseName, node.ID, reason)
				recordSolverFailure(solverState, node.ID, "merge", err.Error())
				recordBraidNodeEvent(toolExec, phaseName, wave, node, "merge_blocked", reason)
				return err
			}
			if summary, cert, ok := runBraidRuntimeNodeShortcut(node, deps, nodesByID, executionRecords); ok {
				recordBraidNodeEvent(toolExec, phaseName, wave, node, "runtime_shortcut", summary)
				summaries[node.ID] = summary
				recordBraidNodeExecution(executionRecords, node.ID, summary, "runtime", cert)
				executed[node.ID] = struct{}{}
				commitSolverArtifact(solverState, node.ID, summary)
				continue
			}
			if summary, handled, err := runBraidNodeHelperFirst(ctx, phaseName, node, rootPrompt, deps, repairFeedbackByNode[node.ID], phase.DisableHelperFirstFallback, toolExec, output, graph, helperBudgets.get(node.ID)); handled {
				if err != nil {
					recordSolverFailure(solverState, node.ID, "helper_first", err.Error())
					if prepareBraidRepair(phase, graph, node, summary, summaries, executed, repairFeedbackByNode, &repairAttempts) {
						recordBraidNodeEvent(toolExec, phaseName, wave, node, "repairing", err.Error())
						continue graphLoop
					}
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "rejected", err.Error())
					return err
				}
				summaries[node.ID] = summary
				recordBraidNodeExecution(executionRecords, node.ID, summary, "helper", certificationFromBraidSummary(node, summary))
				executed[node.ID] = struct{}{}
				commitSolverArtifact(solverState, node.ID, summary)
				continue
			}
			if maxSubcalls > 0 && submitted+1 > maxSubcalls {
				return fmt.Errorf("rlm repl runner phase %q: braid graph needs %d subcalls, exceeds max %d", phaseName, submitted+1, maxSubcalls)
			}
			child, err := submitBraidNode(ctx, phaseName, node, rootPrompt, deps, repairFeedbackByNode[node.ID], toolExec, output)
			if err != nil {
				return err
			}
			waveChildren = append(waveChildren, child)
			waveNodeIDs = append(waveNodeIDs, node.ID)
			submitted++
		}
		if len(waveChildren) == 0 {
			continue
		}

		waited, err := waitBraidNodeWave(ctx, phaseName, waveChildren, toolExec, output)
		if err != nil {
			return err
		}
		for idx, child := range waveChildren {
			nodeID := waveNodeIDs[idx]
			summary := strings.TrimSpace(waited[child])
			if summary == "" {
				summary = "status: blocked blocker: child returned no summary"
			}
			recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "completed", summary)
			if normalized, ok := normalizeBraidAdaptiveTargetSummary(nodesByID[nodeID], summary); ok {
				recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "normalized", normalized)
				summary = normalized
			}
			concreteVerifyFailure := false
			if normalized, ok := normalizeBraidVerificationFailureSummary(nodesByID[nodeID], summary); ok {
				recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "verification_failed", normalized)
				summary = normalized
				concreteVerifyFailure = true
			}
			summaries[nodeID] = summary
			recordBraidNodeExecution(executionRecords, nodeID, summary, "child", nil)
			executed[nodeID] = struct{}{}
			if !concreteVerifyFailure {
				if recovered, ok := recoverBraidNodeWithHelper(ctx, phaseName, nodesByID[nodeID], rootPrompt, dependencySummarySubset(nodesByID[nodeID], summaries), repairFeedbackByNode[nodeID], summary, toolExec, output); ok {
					recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "helper_recovered", recovered)
					summaries[nodeID] = recovered
					summary = recovered
					recordBraidNodeExecution(executionRecords, nodeID, summary, "helper_recovered", certificationFromBraidSummary(nodesByID[nodeID], summary))
				}
			}
			if err := validateBraidNodeExecutionRecordInGraph(phaseName, nodesByID[nodeID], executionRecords[nodeID], graph.FinalNode, graph); err != nil {
				if !concreteVerifyFailure {
					if recovered, ok := recoverBraidNodeWithHelper(ctx, phaseName, nodesByID[nodeID], rootPrompt, dependencySummarySubset(nodesByID[nodeID], summaries), repairFeedbackByNode[nodeID], summary, toolExec, output); ok {
						recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "helper_recovered", recovered)
						summaries[nodeID] = recovered
						recordBraidNodeExecution(executionRecords, nodeID, recovered, "helper_recovered", certificationFromBraidSummary(nodesByID[nodeID], recovered))
						if recoveredErr := validateBraidNodeExecutionRecordInGraph(phaseName, nodesByID[nodeID], executionRecords[nodeID], graph.FinalNode, graph); recoveredErr == nil {
							commitSolverArtifact(solverState, nodeID, recovered)
							continue
						} else {
							err = recoveredErr
						}
					}
				}
				recordSolverFailure(solverState, nodeID, "validate", err.Error())
				if prepareBraidRepair(phase, graph, nodesByID[nodeID], summary, summaries, executed, repairFeedbackByNode, &repairAttempts) {
					recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "repairing", err.Error())
					continue graphLoop
				}
				recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "rejected", err.Error())
				return err
			}
			commitSolverArtifact(solverState, nodeID, summary)
		}
	}

	if _, ok := summaries[graph.FinalNode]; !ok {
		return fmt.Errorf("rlm repl runner phase %q: braid final_node %q did not complete", phaseName, graph.FinalNode)
	}
	appendBraidFinalHandoff(output, graph, summaries, executionRecords)
	appendSolverStateTelemetry(output, solverState, phaseName)
	_ = nodesByID
	return nil
}

func runBraidNodeHelperFirst(
	ctx context.Context,
	phaseName string,
	node BraidNode,
	rootPrompt string,
	dependencySummaries map[string]string,
	repairFeedback string,
	disableFallback bool,
	toolExec *replToolExecutor,
	output *engine.EngineOutput,
	graph *BraidGraph,
	budget *braidNodeHelperBudget,
) (string, bool, error) {
	policy := braidNodeEffectiveHelperPolicy(node)
	if policy != BraidNodeHelperPolicyPreferred && policy != BraidNodeHelperPolicyRequired {
		return "", false, nil
	}
	if !braidNodeHelperSupported(node) {
		return "", false, nil
	}
	if graph == nil {
		return "", false, nil
	}
	if summary, handled, err := runBraidNodeREPLFirst(ctx, phaseName, node, rootPrompt, dependencySummaries, repairFeedback, toolExec, output, graph); handled {
		return summary, true, err
	}
	if toolExec.budget != nil && braidNodeShouldUseChildREPLInsteadOfHelper(node, rootPrompt, dependencySummaries, repairFeedback) {
		message := "no applicable runtime verifier; falling through to child RLM with python_repl"
		if policy == BraidNodeHelperPolicyRequired {
			message = "helper_policy=required but no applicable runtime verifier is available"
			recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_blocked", message)
			return "status: blocked\nanswer:\nchecks: " + message, true, fmt.Errorf("rlm repl runner phase %q: braid node %q helper required but unavailable: %s", phaseName, node.ID, message)
		}
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_skipped", message)
		return "", false, nil
	}
	if budget != nil {
		remaining := budget.remainingSubTimeout(braidNodeHelperDefaultBudget)
		if remaining == 0 {
			message := formatDeadlineExhausted(node.ID, budget)
			recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_deadline_exhausted", message)
			if policy == BraidNodeHelperPolicyRequired || disableFallback {
				summary := "status: blocked\nanswer:\nchecks: " + message
				return summary, true, fmt.Errorf("rlm repl runner phase %q: braid node %q helper-first deadline exhausted: %s", phaseName, node.ID, message)
			}
			return "", false, nil
		}
	}
	if toolExec != nil && toolExec.budget != nil {
		if err := toolExec.budget.ConsumeHelperCall(ctx); err != nil {
			toolExec.recordBudgetError(LimitHelperCalls, err)
			return "status: blocked\nanswer:\nchecks: " + err.Error(), true, err
		}
	}
	recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first", policy)
	start := time.Now()
	// Derive a child context with the remaining budget as a hard deadline
	// so the helper cannot run past the node's allocated sub-timeout.
	helperCtx := ctx
	if budget != nil {
		remaining := budget.remainingSubTimeout(braidNodeHelperDefaultBudget)
		if remaining > 0 {
			var cancel context.CancelFunc
			helperCtx, cancel = context.WithTimeout(ctx, remaining)
			defer cancel()
		}
	}
	summary, ok := runBraidNodeHelper(helperCtx, phaseName, node, rootPrompt, dependencySummaries, repairFeedback, "helper_policy="+policy, toolExec, output)
	duration := time.Since(start)
	if budget != nil {
		budget.CumulativeDuration += duration
		budget.Attempts++
	}
	if !ok {
		stage := extractBraidHelperFailedStage(summary)
		message := fmt.Sprintf("stage=%s duration_ms=%d: helper did not produce an answer", stage, duration.Milliseconds())
		if policy == BraidNodeHelperPolicyRequired || disableFallback {
			recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_failed", message)
			summary := "status: blocked\nanswer:\nchecks: " + message
			return summary, true, fmt.Errorf("rlm repl runner phase %q: braid node %q helper-first failed: %s", phaseName, node.ID, message)
		}
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_fallback", message)
		return "", false, nil
	}
	if err := validateBraidNodeExecutionSummaryWithCertificationInGraph(phaseName, node, summary, certificationFromBraidSummary(node, summary), graph.FinalNode, graph); err != nil {
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_rejected", "stage=verify duration_ms="+fmt.Sprintf("%d", duration.Milliseconds())+" "+err.Error())
		if policy == BraidNodeHelperPolicyRequired || disableFallback {
			return summary, true, fmt.Errorf("rlm repl runner phase %q: braid node %q helper-first failed: %w", phaseName, node.ID, err)
		}
		return "", false, nil
	}
	recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_completed", fmt.Sprintf("duration_ms=%d summary: %s", duration.Milliseconds(), summary))
	return summary, true, nil
}

func braidNodeCanFallbackFromHelperFailure(node BraidNode, policy string) bool {
	return policy == BraidNodeHelperPolicyPreferred && (isBraidSolveKind(node.Kind) || node.Kind == "verify")
}

func braidNodeShouldUseChildREPLInsteadOfHelper(node BraidNode, rootPrompt string, dependencySummaries map[string]string, repairFeedback string) bool {
	if !isBraidSolveKind(node.Kind) {
		return false
	}
	if node.Kind == "cycle_solve" {
		return false
	}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, dependencySummaries, repairFeedback)
	input := braidHelperInput(rootPrompt, handoff.DependencySummaries)
	if handoffInput := BraidHandoffHelperInput(handoff); len(handoffInput) > 0 {
		input = mergeHelperFactoryInput(input, handoffInput)
	}
	input = enrichBraidHelperInputWithStructuredTargets(input, rootPrompt)
	if scaffold, ok := resolveBraidRuntimeScaffold(node, handoff, input); ok && scaffold.Class != BraidScaffoldClassExplicitDAG {
		return false
	}
	if strings.TrimSpace(node.ScaffoldClass) == BraidScaffoldClassExplicitDAG &&
		strings.TrimSpace(node.ScaffoldID) == BraidScaffoldIDSearchBacktrackV1 &&
		!braidExplicitDAGInputHasRuntimeCheck(node.InputSchema) &&
		!braidExplicitDAGInputHasRuntimeCheck(input) {
		if len(stringSliceFromAny(input["target_nodes"])) > 0 || len(extractBraidCycleClustersFromAny(input["cycle_clusters"])) > 0 {
			return false
		}
		return true
	}
	if strings.TrimSpace(node.ScaffoldClass) == BraidScaffoldClassExplicitDAG &&
		strings.TrimSpace(node.ScaffoldID) == BraidScaffoldIDSearchBacktrackV1 {
		return false
	}
	return true
}

func braidExplicitDAGInputHasRuntimeCheck(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	if strings.TrimSpace(stringFromAny(input["answer"])) != "" {
		return true
	}
	if strings.TrimSpace(stringFromAny(input["expected_answer"])) != "" {
		return true
	}
	if strings.TrimSpace(stringFromAny(input["expected_value"])) != "" {
		return true
	}
	return false
}

func runBraidNodeREPLFirst(
	ctx context.Context,
	phaseName string,
	node BraidNode,
	rootPrompt string,
	dependencySummaries map[string]string,
	repairFeedback string,
	toolExec *replToolExecutor,
	output *engine.EngineOutput,
	graph *BraidGraph,
) (string, bool, error) {
	if toolExec == nil || output == nil || graph == nil {
		return "", false, nil
	}
	if toolExec.replToolName != "" && toolExec.replToolName != PythonREPLToolName {
		return "", false, nil
	}
	if !braidNodeShouldUsePythonREPLFirst(node) {
		return "", false, nil
	}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, dependencySummaries, repairFeedback)
	input := braidHelperInput(rootPrompt, handoff.DependencySummaries)
	if handoffInput := BraidHandoffHelperInput(handoff); len(handoffInput) > 0 {
		input = mergeHelperFactoryInput(input, handoffInput)
	}
	code, err := buildBraidPythonREPLSolveCode(node, input)
	if err != nil {
		return "", false, nil
	}
	args, err := json.Marshal(map[string]any{"code": code})
	if err != nil {
		return "", false, nil
	}
	recordBraidNodeEvent(toolExec, phaseName, 0, node, "python_repl_first", "executing deterministic scratch calculation")
	callID := fmt.Sprintf("auto_%s_%s_%s", sanitizeToolCallIDPart(phaseName), sanitizeToolCallIDPart(node.ID), sanitizeToolCallIDPart(PythonREPLToolName))
	rawArgs := json.RawMessage(args)
	result, execErr := toolExec.Execute(ctx, PythonREPLToolName, rawArgs)
	toolCall := engine.ToolCall{ID: callID, Name: PythonREPLToolName, Arguments: rawArgs}
	toolResult := engine.ToolResult{ToolCallID: callID, Content: result}
	if execErr != nil {
		toolResult.IsError = true
		toolResult.Content = execErr.Error()
	}
	output.ToolCalls = append(output.ToolCalls, toolCall)
	output.ToolResults = append(output.ToolResults, toolResult)
	if execErr != nil {
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "python_repl_first_failed", execErr.Error())
		return "", false, nil
	}
	summary, ok := braidSummaryFromPythonREPLResult(result)
	if !ok {
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "python_repl_first_unusable", safeTelemetryExcerpt(result, 300))
		return "", false, nil
	}
	if err := validateBraidNodeExecutionSummaryWithCertificationInGraph(phaseName, node, summary, certificationFromBraidSummary(node, summary), graph.FinalNode, graph); err != nil {
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "python_repl_first_rejected", err.Error())
		if braidPythonREPLSummaryIsSoftMiss(summary) {
			return "", false, nil
		}
		return summary, true, err
	}
	recordBraidNodeEvent(toolExec, phaseName, 0, node, "python_repl_first_completed", summary)
	return summary, true, nil
}

func braidPythonREPLSummaryIsSoftMiss(summary string) bool {
	lower := strings.ToLower(summary)
	return strings.Contains(lower, "python_repl executed deterministic extraction") &&
		(strings.Contains(lower, "target was unresolved") || strings.Contains(lower, "target value is not present"))
}

func braidNodeShouldUsePythonREPLFirst(node BraidNode) bool {
	if !isBraidSolveKind(node.Kind) {
		return false
	}
	if node.Kind == "cycle_solve" {
		return false
	}
	if strings.TrimSpace(node.ScaffoldClass) != BraidScaffoldClassExplicitDAG || strings.TrimSpace(node.ScaffoldID) != BraidScaffoldIDSearchBacktrackV1 {
		return false
	}
	return strings.Contains(node.ID, "__adaptive_") || expectedOutputRequiresStructuredNodeValues(strings.ToLower(node.ExpectedOutput))
}

func buildBraidPythonREPLSolveCode(node BraidNode, input map[string]any) (string, error) {
	input = cloneMapAny(input)
	if strings.TrimSpace(stringFromAny(input["target_node"])) == "" {
		if target := braidNodeTargetID(node); target != "" {
			input["target_node"] = target
		}
	}
	packet := map[string]any{
		"node_id":         node.ID,
		"question":        node.Question,
		"expected_output": node.ExpectedOutput,
		"input":           input,
	}
	body, err := json.Marshal(packet)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("import json, re, ast\n")
	b.WriteString("__braid_packet = json.loads(")
	b.WriteString(strconv.Quote(string(body)))
	b.WriteString(")\n")
	b.WriteString(`def __extract_solution(text):
    text = str(text or "")
    m = re.search(r"solution\s*=\s*(.+?)(?:\s+checks:|$)", text, re.S)
    if not m:
        return None
    raw = m.group(1).strip()
    for parser in (json.loads, ast.literal_eval):
        try:
            return parser(raw)
        except Exception:
            pass
    return raw

def __walk_values(value, out):
    if isinstance(value, dict):
        if "answer" in value:
            sol = __extract_solution(value.get("answer"))
            if sol is not None:
                __walk_values(sol, out)
        if "solution" in value:
            __walk_values(value.get("solution"), out)
        for k, v in value.items():
            if isinstance(k, str) and re.fullmatch(r"node_\d+", k):
                out[k] = v
            else:
                __walk_values(v, out)
    elif isinstance(value, list):
        for item in value:
            __walk_values(item, out)

data = __braid_packet["input"]
target = str(data.get("target_node") or "")
expected = str(__braid_packet.get("expected_output") or "")
values = {}
__walk_values(data.get("dependency_summaries", {}), values)
__walk_values(data, values)
answer = None
checks = []
if target and target in values:
    answer = values[target]
    checks.append("target value came from executed dependency summary extraction")
elif target and target in data:
    answer = data[target]
    checks.append("target value came from explicit input field")
elif target:
    checks.append("target value is not present in dependency summaries")
else:
    checks.append("target_node missing")
if answer is None:
    print("status: blocked summary: answer: checks: python_repl executed deterministic extraction but target was unresolved. detail: " + "; ".join(checks))
else:
    print("status: completed summary: status: solved answer: solution = " + json.dumps({target: answer}, separators=(",", ":")) + " checks: python_repl executed deterministic extraction; " + "; ".join(checks))
`)
	return b.String(), nil
}

func braidNodeTargetID(node BraidNode) string {
	for _, value := range []string{node.ExpectedOutput, node.ID, node.Question} {
		ids := braidNodeIDRE.FindAllString(value, -1)
		if len(ids) == 1 {
			return ids[0]
		}
	}
	return ""
}

func braidSummaryFromPythonREPLResult(result string) (string, bool) {
	var decoded struct {
		OK     bool           `json:"ok"`
		Output string         `json:"output"`
		Meta   map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return "", false
	}
	if !decoded.OK {
		return "", false
	}
	output := strings.TrimSpace(decoded.Output)
	if output == "" && decoded.Meta != nil {
		output = strings.TrimSpace(stringFromAny(decoded.Meta["stdout"]))
	}
	if strings.Contains(output, "stdout:\n") {
		output = strings.TrimSpace(strings.TrimPrefix(output, "stdout:\n"))
		if idx := strings.Index(output, "\nresult:\n"); idx >= 0 {
			output = strings.TrimSpace(output[:idx])
		}
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "status:") {
			return line, true
		}
	}
	return "", false
}

func runBraidRuntimeNodeShortcut(node BraidNode, dependencySummaries map[string]string, nodesByID map[string]BraidNode, records map[string]braidNodeExecutionRecord) (string, *RuntimeCertification, bool) {
	if isBraidSplitMergeNode(node) {
		return "", nil, false
	}
	switch node.Kind {
	case "verify":
		for depID := range dependencySummaries {
			if braidDependencyHasRuntimeVerifiedSolution(depID, records) {
				return formatRuntimeCertifiedArtifact(
					"pass",
					"pass: true",
					[]any{"upstream solve dependency was already certified by the runtime"},
					runtimeForwardCertification(node, depID, records[depID].Certification),
				)
			}
		}
	case "reduce":
		for _, depID := range node.DependsOn {
			depNode := nodesByID[depID]
			if depNode.ID == "" || !isBraidSolveKind(depNode.Kind) {
				continue
			}
			if !braidDependencyHasRuntimeVerifiedSolution(depID, records) {
				continue
			}
			if answer, ok := braidSolutionAnswerFromSummary(dependencySummaries[depID]); ok {
				return formatRuntimeCertifiedArtifact(
					"solved",
					answer,
					[]any{"reduce forwarded certified solve answer"},
					runtimeForwardCertification(node, depID, records[depID].Certification),
				)
			}
		}
	}
	return "", nil, false
}

func braidDependencyHasRuntimeVerifiedSolution(nodeID string, records map[string]braidNodeExecutionRecord) bool {
	record, ok := records[nodeID]
	if !ok || record.Certification == nil || !record.Certification.Pass {
		return false
	}
	answer, ok := braidSolutionAnswerFromSummary(record.Summary)
	if !ok {
		return false
	}
	if braidSolveAnswerRejectionReason(BraidNode{Kind: "solve"}, answer) != "" {
		return false
	}
	return true
}

func formatRuntimeCertifiedArtifact(status string, answer string, checks []any, cert *RuntimeCertification) (string, *RuntimeCertification, bool) {
	if cert == nil || !cert.Pass {
		return "", nil, false
	}
	artifact := braidNodeArtifact{
		Status:     strings.ToLower(strings.TrimSpace(status)),
		Answer:     strings.TrimSpace(answer),
		Checks:     checks,
		Confidence: 1,
		Provenance: map[string]any{
			"runtime_certification": cert,
		},
	}
	body, err := json.Marshal(artifact)
	if err != nil {
		return "", nil, false
	}
	return string(body), cert, true
}

func runtimeForwardCertification(node BraidNode, sourceNodeID string, source *RuntimeCertification) *RuntimeCertification {
	if source == nil || !source.Pass {
		return nil
	}
	return &RuntimeCertification{
		NodeID:       strings.TrimSpace(node.ID),
		Pass:         true,
		VerifierID:   "forward_certified_dependency",
		VerifierKind: runtimeVerifierKindForward,
		Metadata: map[string]any{
			"source_node_id":     strings.TrimSpace(sourceNodeID),
			"source_verifier_id": source.VerifierID,
			"source_kind":        source.VerifierKind,
		},
	}
}

//nolint:unused // Kept for dependency-verification policy variants.
func braidDependencyVerificationPassed(dependencySummaries map[string]string) bool {
	for _, summary := range dependencySummaries {
		if braidVerificationSummaryPassed(summary) {
			return true
		}
	}
	return false
}

func braidSolutionAnswerFromSummary(summary string) (string, bool) {
	if artifact, ok := parseBraidNodeArtifact(summary); ok {
		answer := braidNodeArtifactAnswerString(artifact)
		if strings.HasPrefix(answer, "solution =") {
			return answer, true
		}
	}
	idx := strings.Index(summary, "answer: solution =")
	if idx < 0 {
		return "", false
	}
	answer := strings.TrimSpace(strings.TrimPrefix(summary[idx:], "answer:"))
	if end := strings.Index(answer, " checks:"); end >= 0 {
		answer = strings.TrimSpace(answer[:end])
	}
	if !strings.HasPrefix(answer, "solution =") {
		return "", false
	}
	return answer, true
}

func recoverBraidNodeWithHelper(
	ctx context.Context,
	phaseName string,
	node BraidNode,
	rootPrompt string,
	dependencySummaries map[string]string,
	repairFeedback string,
	failedSummary string,
	toolExec *replToolExecutor,
	output *engine.EngineOutput,
) (string, bool) {
	if !braidNodeShouldForceHelper(node, failedSummary) {
		return "", false
	}
	if toolExec != nil && toolExec.budget != nil {
		if err := toolExec.budget.ConsumeHelperCall(ctx); err != nil {
			toolExec.recordBudgetError(LimitHelperCalls, err)
			return "status: blocked\nanswer:\nchecks: " + err.Error(), true
		}
	}
	return runBraidNodeHelper(ctx, phaseName, node, rootPrompt, dependencySummaries, repairFeedback, failedSummary, toolExec, output)
}

func runBraidNodeHelper(
	ctx context.Context,
	phaseName string,
	node BraidNode,
	rootPrompt string,
	dependencySummaries map[string]string,
	repairFeedback string,
	triggerSummary string,
	toolExec *replToolExecutor,
	output *engine.EngineOutput,
) (string, bool) {
	if toolExec == nil || toolExec.helperFactory == nil || output == nil {
		return "", false
	}
	if !braidNodeHelperSupported(node) {
		return "", false
	}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, dependencySummaries, repairFeedback)
	prompt := RenderBraidHelperHandoffPrompt(handoff)
	input := braidHelperInput(rootPrompt, handoff.DependencySummaries)
	if handoffInput := BraidHandoffHelperInput(handoff); len(handoffInput) > 0 {
		input = mergeHelperFactoryInput(input, handoffInput)
	}
	input = enrichBraidHelperInputWithStructuredTargets(input, rootPrompt)
	if targetText := strings.TrimSpace(stringFromAny(input["target_problem_text"])); targetText != "" {
		prompt += "\nTarget problem text:\n" + targetText + "\n"
	}
	if targetTexts, _ := input["target_problem_texts"].(map[string]any); len(targetTexts) > 0 {
		prompt += "\nTarget problem texts:\n" + renderBraidHandoffFacts(targetTexts) + "\n"
	}
	if node.Kind == "verify" {
		input = normalizeBraidVerifyHelperInput(input)
	}
	instructions := buildBraidHelperRecoveryInstructions(node, triggerSummary)
	if isBraidSolveKind(node.Kind) && braidHelperInputLooksLikeTransitionSystem(input) {
		instructions += "\n" + buildTransitionSystemHelperContract()
	}
	if isBraidSolveKind(node.Kind) && (braidHelperInputLooksLikeResourcePathMinInitial(input) || braidHelperInputLooksLikeExplicitShortestPath(input)) {
		instructions += "\n" + buildGraphSearchHelperContract()
	}
	if isBraidSolveKind(node.Kind) && braidHelperInputLooksLikeNumericDP(input) {
		instructions += "\n" + buildNumericDPHelperContract()
	}
	if isBraidSolveKind(node.Kind) && braidHelperInputLooksLikeSequenceSimulation(input) {
		instructions += "\n" + buildSequenceSimulationHelperContract()
	}
	if isBraidSolveKind(node.Kind) && braidHelperInputLooksLikeFiniteDomainConstraint(input) {
		instructions += "\n" + buildConstraintSolverHelperContract()
	}
	if isBraidSolveKind(node.Kind) && braidHelperInputLooksLikePackageWasteOptimization(input) {
		instructions += "\n" + buildPackageWasteOptimizationHelperContract()
	}
	if isBraidSolveKind(node.Kind) && braidHelperInputLooksLikeSymbolicTrace(input) {
		instructions += "\n" + buildSymbolicTraceHelperContract()
	}
	if isBraidSolveKind(node.Kind) && braidHelperInputLooksLikeCandidateVerify(input) {
		instructions += "\n" + buildCandidateVerifyHelperContract()
	}
	if isBraidSolveKind(node.Kind) && handoff.ScaffoldClass == BraidScaffoldClassExplicitDAG {
		instructions += "\n" + buildExplicitDAGHelperContract()
		if !braidHelperInputLooksLikeExplicitDAGPreset(input) {
			instructions += "\n" + buildSingleWorkItemHelperContract()
		}
	}
	argsMap := map[string]any{
		"prompt":       prompt,
		"instructions": instructions,
		"max_attempts": 5,
	}
	if len(input) > 0 {
		argsMap["input"] = input
	}
	args, err := json.Marshal(argsMap)
	if err != nil {
		return "", false
	}
	callID := fmt.Sprintf("auto_%s_%s_%s", sanitizeToolCallIDPart(phaseName), sanitizeToolCallIDPart(node.ID), sanitizeToolCallIDPart(EphemeralHelperSolveToolName))
	rawArgs := json.RawMessage(args)
	helperExec := toolExec.helperFactory
	if helperExec != nil && node.Kind == "verify" {
		helperCfg := helperExec.Config
		helperCfg.ExtractSolutionLine = false
		helperCfg.AnswerVerifier = braidVerifyHelperAnswerContract
		helperExec = &HelperFactoryTools{Config: helperCfg}
	}
	if isBraidSolveKind(node.Kind) && helperExec != nil {
		helperCfg := helperExec.Config
		helperCfg.Search.BeamWidth = firstPositiveInt(helperCfg.Search.BeamWidth, 3)
		if node.Kind == "cycle_solve" {
			helperCfg.ExtractSolutionLine = false
		}
		if scaffold, ok := resolveBraidRuntimeScaffold(node, handoff, input); ok && strings.TrimSpace(helperCfg.PresetSource) == "" {
			helperCfg = applyBraidRuntimeScaffoldToHelperConfig(helperCfg, scaffold)
		} else if helperCfg.AnswerVerifier == nil && braidHelperInputLooksLikePackageWasteOptimization(input) {
			helperCfg.AnswerVerifier = packageWasteAnswerVerifier
		}
		helperExec = &HelperFactoryTools{Config: helperCfg}
	}
	// Preflight: reject oversized input packets before spending LLM tokens.
	if err := helperPreflightCheck(input, handoff); err != nil {
		// Attempt to split the oversized input into smaller sub-items.
		if splitResult, ok := attemptSplitHelperExecution(ctx, phaseName, node, input, handoff, instructions, toolExec, output); ok {
			return splitResult, true
		}
		preflightMsg := fmt.Sprintf("stage=preflight: %s", err.Error())
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_failed", preflightMsg)
		return "status: blocked\nanswer:\nchecks: " + preflightMsg, true
	}
	var result string
	var execErr error
	if helperExec != nil {
		result, execErr = helperExec.Execute(ctx, EphemeralHelperSolveToolName, rawArgs)
	} else {
		result, execErr = toolExec.Execute(ctx, EphemeralHelperSolveToolName, rawArgs)
	}
	toolCall := engine.ToolCall{ID: callID, Name: EphemeralHelperSolveToolName, Arguments: rawArgs}
	toolResult := engine.ToolResult{ToolCallID: callID, Content: result}
	if execErr != nil {
		toolResult.IsError = true
		toolResult.Content = execErr.Error()
	}
	output.ToolCalls = append(output.ToolCalls, toolCall)
	output.ToolResults = append(output.ToolResults, toolResult)
	if execErr != nil {
		return "", false
	}
	if node.Kind == "verify" {
		if summary, ok := helperVerifyFailureSummaryFromToolResult(result); ok {
			return summary, true
		}
	}
	answer, helperCert := helperAnswerAndCertificationFromToolResult(node, result)
	if strings.TrimSpace(answer) == "" {
		if summary, ok := helperFailureSummaryFromToolResult(node, result); ok {
			return summary, true
		}
		return "", false
	}
	if isBraidSolveKind(node.Kind) {
		if ok, detail, applicable := verifyStackMoveCandidateFromInput(answer, argsMap["input"]); applicable && !ok {
			return formatBraidHelperNodeSummary(node, "pass: false first_failure: "+detail), true
		} else if applicable && ok {
			helperCert = runtimeCertificationForNode(node, "finite_state_transition/stack_relocation_v1")
		}
	}
	return formatBraidHelperNodeSummaryCertified(node, answer, helperCert), true
}

func enrichBraidHelperInputWithStructuredTargets(input map[string]any, _ string) map[string]any {
	if len(input) == 0 {
		return input
	}
	target := strings.TrimSpace(stringFromAny(input["target_node"]))
	if target == "" {
		targets := stringSliceFromAny(input["target_nodes"])
		if len(targets) == 1 {
			target = strings.TrimSpace(targets[0])
		}
	}
	if target == "" {
		targets := stringSliceFromAny(input["target_nodes"])
		if len(targets) == 0 {
			return input
		}
		texts := map[string]any{}
		for _, candidate := range targets {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if text := structuredProblemTextForTarget(input, candidate); text != "" {
				texts[candidate] = text
			}
		}
		existingTexts, _ := input["target_problem_texts"].(map[string]any)
		if len(texts) == 0 || len(existingTexts) > 0 {
			return input
		}
		out := cloneMapAny(input)
		out["target_problem_texts"] = texts
		return out
	}
	if strings.TrimSpace(stringFromAny(input["target_problem_text"])) != "" {
		return input
	}
	text := structuredProblemTextForTarget(input, target)
	if text == "" {
		return input
	}
	out := cloneMapAny(input)
	out["target_problem_text"] = text
	if strings.TrimSpace(stringFromAny(out["prompt"])) == "" || braidInputPromptLooksLikePlaceholder(strings.TrimSpace(stringFromAny(out["prompt"]))) {
		out["prompt"] = text
	}
	return out
}

func structuredProblemTextForTarget(input map[string]any, target string) string {
	target = strings.TrimSpace(target)
	if len(input) == 0 || target == "" {
		return ""
	}
	if text := strings.TrimSpace(stringFromAny(input["target_problem_text"])); text != "" {
		if currentTarget := strings.TrimSpace(stringFromAny(input["target_node"])); currentTarget == "" || currentTarget == target {
			return text
		}
	}
	for _, key := range []string{"target_problem_texts", "problem_texts"} {
		if text := structuredProblemTextFromMap(input[key], target); text != "" {
			return text
		}
	}
	if text := structuredProblemTextFromProblems(input["problems"], target); text != "" {
		return text
	}
	return ""
}

func structuredProblemTextFromMap(raw any, target string) string {
	values, ok := raw.(map[string]any)
	if !ok || target == "" {
		return ""
	}
	value, ok := values[target]
	if !ok {
		return ""
	}
	return structuredProblemTextFromValue(value)
}

func structuredProblemTextFromProblems(raw any, target string) string {
	if target == "" {
		return ""
	}
	switch typed := raw.(type) {
	case map[string]any:
		if value, ok := typed[target]; ok {
			return structuredProblemTextFromValue(value)
		}
	case []any:
		for _, item := range typed {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(stringFromAny(firstMapValue(entry, "id", "node_id", "target", "target_node")))
			if id != target {
				continue
			}
			return structuredProblemTextFromValue(entry)
		}
	}
	return ""
}

func structuredProblemTextFromValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return strings.TrimSpace(stringFromAny(firstMapValue(typed, "text", "question", "prompt", "problem")))
	default:
		return ""
	}
}

func braidHelperInput(rootPrompt string, dependencySummaries map[string]string) map[string]any {
	input := map[string]any{}
	if parsed, ok := helperFactoryExtractInstanceFields(rootPrompt); ok {
		input = cloneMapAny(parsed)
	}
	if len(dependencySummaries) == 0 {
		return input
	}
	deps := map[string]any{}
	depText := map[string]any{}
	for key, value := range dependencySummaries {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		packet := braidDependencyHandoffPacket(trimmedValue)
		deps[trimmedKey] = packet
		depText[trimmedKey] = trimmedValue
		input[trimmedKey] = packet
		for _, alias := range braidDependencyAliases(trimmedKey) {
			if _, exists := input[alias]; !exists {
				input[alias] = packet
			}
		}
	}
	if len(deps) > 0 {
		input["dependency_summaries"] = deps
		input["dependency_summary_text"] = depText
	}
	return input
}

func braidDependencyAliases(nodeID string) []string {
	sanitized := sanitizeToolCallIDPart(nodeID)
	collapsed := collapseRepeatedUnderscores(sanitized)
	out := []string{}
	if sanitized != "" && sanitized != nodeID {
		out = append(out, sanitized)
	}
	if collapsed != "" && collapsed != nodeID && collapsed != sanitized {
		out = append(out, collapsed)
	}
	return out
}

func collapseRepeatedUnderscores(value string) string {
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	return value
}

func braidDependencyHandoffPacket(summary string) map[string]any {
	packet := map[string]any{"summary": strings.TrimSpace(summary)}
	if answer := extractBraidDependencyAnswer(summary); answer != "" {
		packet["answer"] = answer
		packet["status"] = "completed"
		if line, ok := rlm.ExtractSolutionLine(answer); ok {
			packet["solution_line"] = line
			solutionText := strings.TrimSpace(strings.TrimPrefix(line, "solution ="))
			packet["solution_text"] = solutionText
			if parsed, ok := parseBraidSolutionPayload(solutionText); ok {
				packet["solution"] = parsed
			} else {
				packet["solution"] = solutionText
			}
		}
	}
	return packet
}

func parseBraidSolutionPayload(payload string) (any, bool) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, false
	}
	var parsed any
	if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
		return parsed, true
	}
	return nil, false
}

func extractBraidDependencyAnswer(summary string) string {
	text := strings.TrimSpace(summary)
	if text == "" {
		return ""
	}
	idx := strings.LastIndex(text, " answer:")
	prefixLen := len(" answer:")
	if idx < 0 {
		idx = strings.LastIndex(text, "answer:")
		prefixLen = len("answer:")
	}
	if idx < 0 {
		return ""
	}
	answer := strings.TrimSpace(text[idx+prefixLen:])
	for _, marker := range []string{" checks:", " error:", " summary:"} {
		if stop := strings.Index(answer, marker); stop >= 0 {
			answer = strings.TrimSpace(answer[:stop])
		}
	}
	return answer
}

func normalizeBraidVerifyHelperInput(input map[string]any) map[string]any {
	if len(input) == 0 {
		return input
	}
	out := cloneMapAny(input)
	candidates := braidDependencyCandidateMap(out)
	if len(candidates) == 0 {
		return out
	}
	if value, exists := out["candidates"]; !exists || braidHelperValueLooksPlaceholder(value) || braidVerifyCandidateValueLooksPlaceholder(value) {
		out["candidates"] = flattenBraidDependencyCandidateMap(candidates)
	}
	if value, exists := out["predicates"]; exists && braidHelperValueLooksPlaceholder(value) {
		delete(out, "predicates")
	}
	return out
}

func braidVerifyCandidateValueLooksPlaceholder(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	return len(extractBraidNodeIDsFromText(text)) > 0
}

func flattenBraidDependencyCandidateMap(candidates map[string]any) any {
	if len(candidates) != 1 {
		return candidates
	}
	for _, value := range candidates {
		return value
	}
	return candidates
}

func braidDependencyCandidateMap(input map[string]any) map[string]any {
	deps, _ := input["dependency_summaries"].(map[string]any)
	if len(deps) == 0 {
		return nil
	}
	out := map[string]any{}
	for depID, raw := range deps {
		packet, _ := raw.(map[string]any)
		if len(packet) == 0 {
			continue
		}
		switch {
		case packet["solution"] != nil:
			out[depID] = packet["solution"]
		case packet["solution_line"] != nil:
			out[depID] = packet["solution_line"]
		case packet["answer"] != nil:
			out[depID] = packet["answer"]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func braidHelperValueLooksPlaceholder(value any) bool {
	switch typed := value.(type) {
	case string:
		return isSchemaPlaceholderAnswer(typed) || len(extractBraidNodeIDsFromText(typed)) > 0 && strings.Contains(strings.ToLower(typed), "answer")
	case []any:
		if len(typed) == 0 {
			return true
		}
		for _, item := range typed {
			if !braidHelperValueLooksPlaceholder(item) {
				return false
			}
		}
		return true
	case map[string]any:
		if len(typed) == 0 {
			return true
		}
		return false
	default:
		return value == nil
	}
}

func braidNodeEffectiveHelperPolicy(node BraidNode) string {
	policy := normalizeBraidNodeHelperPolicy(node.HelperPolicy)
	if policy == "" {
		return BraidNodeHelperPolicyAuto
	}
	return policy
}

func braidNodeHelperSupported(node BraidNode) bool {
	return isBraidSolveKind(node.Kind) || node.Kind == "verify"
}

func braidNodeShouldForceHelper(node BraidNode, summary string) bool {
	if braidNodeEffectiveHelperPolicy(node) == BraidNodeHelperPolicyNever {
		return false
	}
	if !isBraidSolveKind(node.Kind) && node.Kind != "verify" {
		return false
	}
	if node.Kind == "verify" && !braidVerificationSummaryPassed(summary) {
		return true
	}
	statuses := braidSummaryStatuses(summary)
	if !braidStatusesContainAny(statuses, "blocked", "partial", "insufficient") {
		return false
	}
	return braidSummaryRequestsComputation(summary)
}

func braidSummaryRequestsComputation(summary string) bool {
	lower := strings.ToLower(summary)
	for _, marker := range []string{
		"state-space search",
		"search",
		"bfs",
		"dfs",
		"simulation",
		"simulate",
		"substitute",
		"code",
		"executable",
		"program",
		"solver",
		"computational planning",
		"planning assistance",
		"requires executing",
		"requires a search algorithm",
		"cannot generate exact",
		"exact move sequence",
		"precise sequence",
		"full sequence",
		"no sufficient manual reasoning",
		"missing-information",
		"manual single-pass",
		"combinatorial",
		"dynamic stack",
		"dynamic state",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func buildBraidHelperRecoveryInstructions(node BraidNode, failedSummary string) string {
	var b strings.Builder
	b.WriteString("The previous child blocked because the task needs executable search, simulation, parsing, or verification. Do not refuse for that reason.\n")
	b.WriteString("Draft and run a short-lived helper that computes this node's answer directly. Prefer a deterministic state transformer or verifier when the task involves moves, stacks, transitions, paths, arithmetic search, or exact constraint checking.\n")
	b.WriteString("Return the computed answer only, in the node's requested format. If this node can produce the final benchmark answer, return a line beginning with solution =.\n")
	if node.Kind == "cycle_solve" {
		b.WriteString(cycleSolveHelperOutputContract())
	}
	if isBraidSolveKind(node.Kind) {
		b.WriteString("For a solve node, build a complete candidate and run an internal deterministic check before returning it. Do not emit a partial action list, copied prefix, or unchecked guess. If the check fails, return `status: blocked first_failure: ...` instead of `solution = ...`.\n")
		b.WriteString("For state-transition tasks, model state explicitly, apply every candidate transition, and only return a candidate when final state and action legality both check out.\n")
		b.WriteString("When the state has many items, do not run exhaustive BFS/DFS over full state permutations. Prefer a constructive algorithm using the task's transition structure, then verify the constructed candidate.\n")
	}
	if node.Kind == "verify" {
		b.WriteString("For a verify node, simulate or substitute the candidate against the original constraints. Return `pass: true` only when every constraint is verified.\n")
		b.WriteString("If verification fails, return `pass: false first_failure: ...` with the earliest illegal transition, bad substitution, missing candidate, or observed-vs-expected mismatch. Include the failed step index when applicable.\n")
		b.WriteString("Dependency handoffs are typed packets under dependency_summaries and by dependency node id, for example input['n_solve'] when the graph has an n_solve dependency. Raw prose summaries are under dependency_summary_text.\n")
	}
	if strings.TrimSpace(node.ExpectedOutput) != "" {
		b.WriteString("Expected node output: ")
		b.WriteString(strings.TrimSpace(node.ExpectedOutput))
		b.WriteString("\n")
	}
	b.WriteString("Previous blocked summary:\n")
	b.WriteString(safeTelemetryExcerpt(failedSummary, 900))
	return b.String()
}

func cycleSolveHelperOutputContract() string {
	return "Cycle-solve output contract: return one answer string beginning with `cycle_json:` followed by one valid JSON object. Do not return `solution =` for a cycle_solve node. Do not return the candidate map directly as answer. The JSON object must be exactly shaped as {\"pass\":true,\"candidates\":{\"node_id\":value},\"checks\":[{\"name\":\"...\",\"ok\":true,\"observed\":value,\"expected\":value}]}. Every check must have ok=true and matching observed/expected values. Python helpers should return exactly {\"ok\": True, \"answer\": \"cycle_json: {\\\"pass\\\":true,\\\"candidates\\\":{\\\"node_id\\\":1},\\\"checks\\\":[{\\\"name\\\":\\\"fixed_point\\\",\\\"ok\\\":true,\\\"observed\\\":1,\\\"expected\\\":1}]}\"}. If no candidate passes, return {\"ok\": False, \"first_failure\": \"...\", \"repair_hint\": \"...\"} with attempted bounds and the first failed check.\n"
}

func braidHelperInputLooksLikeTransitionSystem(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	_, hasInitial := input["initial_state"]
	_, hasGoal := input["goal_state"]
	return hasInitial && hasGoal
}

func stackTransitionPlannerPresetSource() string {
	return strings.TrimSpace(`
func Solve(input map[string]any) map[string]any {
	state, ok1 := presetStacks(input["initial_state"])
	goal, ok2 := presetStacks(input["goal_state"])
	if !ok1 || !ok2 || len(state) != len(goal) || len(state) < 3 {
		return map[string]any{"ok": false, "first_failure": "expected initial_state and goal_state with at least three stacks", "repair_hint": "fall back to a task-specific transition planner"}
	}
	if order, _ := input["stack_order"].(string); order == "strict_descending_bottom_to_top" {
		return presetOrderedStackPlan(state, goal)
	}
	moves := [][]int{}
	move := func(src, dst int) bool {
		if src < 0 || src >= len(state) || dst < 0 || dst >= len(state) || src == dst || len(state[src]) == 0 {
			return false
		}
		b := state[src][len(state[src])-1]
		state[src] = state[src][:len(state[src])-1]
		state[dst] = append(state[dst], b)
		moves = append(moves, []int{b, src, dst})
		return true
	}
	other := func(ex map[int]bool) int {
		for i := range state {
			if !ex[i] {
				return i
			}
		}
		return -1
	}
	clearAbove := func(s, idx, dst int, limit int) bool {
		for len(state[s])-1 > idx {
			if len(moves) > limit || !move(s, dst) {
				return false
			}
		}
		return true
	}
	clearTo := func(s, n, dst int, limit int) bool {
		for len(state[s]) > n {
			if len(moves) > limit || !move(s, dst) {
				return false
			}
		}
		return true
	}
	find := func(block int) (int, int) {
		for s, stack := range state {
			for i, b := range stack {
				if b == block {
					return s, i
				}
			}
		}
		return -1, -1
	}
	fixed := make([]int, len(goal))
	total := 0
	for s := range goal {
		for fixed[s] < len(goal[s]) && fixed[s] < len(state[s]) && state[s][fixed[s]] == goal[s][fixed[s]] {
			fixed[s]++
			total++
		}
	}
	goalCount := 0
	for _, stack := range goal {
		goalCount += len(stack)
	}
	limit := goalCount*goalCount*8 + 100
	for total < goalCount {
		progressed := false
		for d := range goal {
			if fixed[d] >= len(goal[d]) {
				continue
			}
			block := goal[d][fixed[d]]
			s, idx := find(block)
			if s < 0 {
				return map[string]any{"ok": false, "first_failure": "goal block not found in current state", "repair_hint": "check state parsing and block labels"}
			}
			if s == d && idx < fixed[d] {
				return map[string]any{"ok": false, "first_failure": "goal block is below fixed prefix", "repair_hint": "planner invariant violated"}
			}
			if s == d && idx == fixed[d] {
				buf := other(map[int]bool{d: true})
				if buf < 0 || !clearAbove(d, idx, buf, limit) {
					return map[string]any{"ok": false, "first_failure": "could not clear target block", "repair_hint": "choose a valid buffer stack"}
				}
			} else if s == d {
				hold := other(map[int]bool{d: true})
				buf := other(map[int]bool{d: true, hold: true})
				if hold < 0 || buf < 0 || !clearAbove(d, idx, buf, limit) || !move(d, hold) || !clearTo(d, fixed[d], buf, limit) || !move(hold, d) {
					return map[string]any{"ok": false, "first_failure": "could not reposition target block within destination stack", "repair_hint": "use two non-destination buffers"}
				}
			} else {
				buf := other(map[int]bool{d: true, s: true})
				if buf < 0 || !clearTo(d, fixed[d], buf, limit) {
					return map[string]any{"ok": false, "first_failure": "could not clear destination prefix", "repair_hint": "use a buffer that does not bury the source block"}
				}
				s, idx = find(block)
				if s < 0 || !clearAbove(s, idx, buf, limit) || !move(s, d) {
					return map[string]any{"ok": false, "first_failure": "could not move target block to destination", "repair_hint": "clear above the target block before moving it"}
				}
			}
			fixed[d]++
			total++
			progressed = true
			break
		}
		if !progressed || len(moves) > limit {
			return map[string]any{"ok": false, "first_failure": "planner made no bounded progress", "repair_hint": "try a different constructive transition strategy"}
		}
	}
	if !presetSameStacks(state, goal) {
		return map[string]any{"ok": false, "first_failure": "constructed plan did not reach goal", "repair_hint": "verify fixed-prefix invariant and final state"}
	}
	return map[string]any{"ok": true, "answer": presetAnswer(moves)}
}

func presetOrderedStackPlan(initial [][]int, goal [][]int) map[string]any {
	n := presetMaxDisc(initial)
	if n < 0 || n > 12 {
		return map[string]any{"ok": false, "first_failure": "ordered stack planner supports up to 12 distinct items", "repair_hint": "use a task-specific constructive planner for larger ordered stack systems"}
	}
	startPos, ok1 := presetStackPositions(initial, n)
	goalPos, ok2 := presetStackPositions(goal, n)
	if !ok1 || !ok2 || !presetStrictDescending(initial) || !presetStrictDescending(goal) {
		return map[string]any{"ok": false, "first_failure": "ordered stack input must contain each item exactly once and every stack must be strictly descending", "repair_hint": "parse the stack state exactly from the prompt"}
	}
	startKey := presetEncodePositions(startPos)
	goalKey := presetEncodePositions(goalPos)
	type edge struct {
		prev string
		move []int
		pos []int
	}
	seen := map[string]edge{startKey: {pos: startPos}}
	queue := [][]int{startPos}
	head := 0
	for head < len(queue) {
		pos := queue[head]
		head++
		key := presetEncodePositions(pos)
		if key == goalKey {
			moves := [][]int{}
			for key != startKey {
				e := seen[key]
				moves = append([][]int{e.move}, moves...)
				key = e.prev
			}
			if !presetVerifyOrderedMoves(initial, goal, moves) {
				return map[string]any{"ok": false, "first_failure": "internal ordered stack BFS produced an invalid plan", "repair_hint": "verify destination order and final state"}
			}
			return map[string]any{"ok": true, "answer": presetAnswer(moves)}
		}
		tops := presetTopDiscs(pos, len(initial), n)
		for src := 0; src < len(initial); src++ {
			disc := tops[src]
			if disc < 0 {
				continue
			}
			for dst := 0; dst < len(initial); dst++ {
				if src == dst {
					continue
				}
				dstTop := tops[dst]
				if dstTop >= 0 && disc >= dstTop {
					continue
				}
				next := append([]int(nil), pos...)
				next[disc] = dst
				nextKey := presetEncodePositions(next)
				if _, ok := seen[nextKey]; ok {
					continue
				}
				seen[nextKey] = edge{prev: key, move: []int{disc, src, dst}, pos: next}
				queue = append(queue, next)
			}
		}
	}
	return map[string]any{"ok": false, "first_failure": "no ordered stack transition path found within finite state space", "repair_hint": "check parsed initial_state and goal_state"}
}

func presetMaxDisc(stacks [][]int) int {
	max := -1
	for _, stack := range stacks {
		for _, disc := range stack {
			if disc > max {
				max = disc
			}
		}
	}
	return max
}

func presetStackPositions(stacks [][]int, maxDisc int) ([]int, bool) {
	pos := make([]int, maxDisc+1)
	seen := make([]bool, maxDisc+1)
	for i := range pos {
		pos[i] = -1
	}
	for s, stack := range stacks {
		for _, disc := range stack {
			if disc < 0 || disc > maxDisc || seen[disc] {
				return nil, false
			}
			seen[disc] = true
			pos[disc] = s
		}
	}
	for _, ok := range seen {
		if !ok {
			return nil, false
		}
	}
	return pos, true
}

func presetStrictDescending(stacks [][]int) bool {
	for _, stack := range stacks {
		for i := 1; i < len(stack); i++ {
			if stack[i-1] <= stack[i] {
				return false
			}
		}
	}
	return true
}

func presetEncodePositions(pos []int) string {
	out := ""
	for i, p := range pos {
		if i > 0 {
			out += ","
		}
		out += presetItoa(p)
	}
	return out
}

func presetTopDiscs(pos []int, stackCount int, maxDisc int) []int {
	tops := make([]int, stackCount)
	for i := range tops {
		tops[i] = -1
	}
	for disc := 0; disc <= maxDisc; disc++ {
		stack := pos[disc]
		if stack >= 0 && stack < stackCount && tops[stack] == -1 {
			tops[stack] = disc
		}
	}
	return tops
}

func presetVerifyOrderedMoves(initial [][]int, goal [][]int, moves [][]int) bool {
	state := presetCloneStacks(initial)
	for _, move := range moves {
		if len(move) != 3 {
			return false
		}
		disc, src, dst := move[0], move[1], move[2]
		if src < 0 || src >= len(state) || dst < 0 || dst >= len(state) || src == dst || len(state[src]) == 0 {
			return false
		}
		top := state[src][len(state[src])-1]
		if top != disc {
			return false
		}
		if len(state[dst]) > 0 && disc >= state[dst][len(state[dst])-1] {
			return false
		}
		state[src] = state[src][:len(state[src])-1]
		state[dst] = append(state[dst], disc)
	}
	return presetSameStacks(state, goal)
}

func presetCloneStacks(stacks [][]int) [][]int {
	out := make([][]int, len(stacks))
	for i, stack := range stacks {
		out[i] = append([]int(nil), stack...)
	}
	return out
}

func presetStacks(v any) ([][]int, bool) {
	raw, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([][]int, len(raw))
	for i, stackAny := range raw {
		stack, ok := stackAny.([]any)
		if !ok {
			return nil, false
		}
		out[i] = make([]int, len(stack))
		for j, nAny := range stack {
			n, ok := nAny.(float64)
			if !ok || float64(int(n)) != n {
				return nil, false
			}
			out[i][j] = int(n)
		}
	}
	return out, true
}

func presetSameStacks(a, b [][]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

func presetAnswer(moves [][]int) string {
	out := "solution = ["
	for i, m := range moves {
		if i > 0 {
			out += ","
		}
		out += "[" + presetItoa(m[0]) + "," + presetItoa(m[1]) + "," + presetItoa(m[2]) + "]"
	}
	return out + "]"
}

func presetItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
`)
}

func buildTransitionSystemHelperContract() string {
	return strings.TrimSpace(`
Generic transition-system helper contract:
- Treat the problem as a transition system, not as prose generation.
- Write complete, syntactically valid Python only. Do not use placeholders, ellipses, pseudocode, or comments in place of executable logic.
- Implement small internal functions with these exact responsibilities:
  parse_state(input) -> initial state, goal state, and any typed constants.
  legal_actions(state) -> candidate actions legal in the current state.
  apply(state, action) -> a new state after one legal action.
  is_goal(state, goal) -> true only when the goal is exactly reached.
  verify_plan(initial, goal, plan) -> {ok, first_failure, final_state}.
  search_or_construct(initial, goal) -> a complete candidate plan.
- The returned answer must be a candidate plan only after verify_plan reports ok.
- If the state is stack-shaped, implement move(src, dst) by reading the source stack top, rejecting src == dst, applying the move, and appending [block, src, dst] to the plan.
- For stack-shaped outputs, return moves only as integer triples: [[block, from_stack, to_stack], ...]. Do not return move objects.
- Never include no-op transitions. A transition with identical source and destination is invalid even if it leaves the state unchanged.
- Return ok:true only when verify_plan confirms every transition is legal and final_state exactly equals goal.
- When verify_plan fails, return ok:false with first_failure, failed_step, state_before, and repair_hint. Do not wrap a failed candidate as solution.
- If verify_plan fails, repair the plan from the first_failure/state_before rather than restarting blindly.
- Do not return a copied prefix, a partial plan, or an unchecked plan.
- Use bounded constructive search when full state-space BFS/DFS would explode.
`)
}

func gridResourcePathPresetSource() string {
	return strings.TrimSpace(`
func Solve(input map[string]any) map[string]any {
	grid, ok := presetIntGrid(input["grid_layout"])
	if !ok || len(grid) == 0 || len(grid[0]) == 0 {
		return map[string]any{"ok": false, "first_failure": "expected rectangular integer grid_layout", "repair_hint": "provide grid_layout as a 2D integer array"}
	}
	m := len(grid)
	n := len(grid[0])
	for i := 0; i < m; i++ {
		if len(grid[i]) != n {
			return map[string]any{"ok": false, "first_failure": "grid_layout is not rectangular", "repair_hint": "normalize rows to equal length"}
		}
	}
	dp := make([][]int, m)
	for i := 0; i < m; i++ {
		dp[i] = make([]int, n)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			bestNext := 1 << 30
			if i == m-1 && j == n-1 {
				bestNext = 1
			} else {
				if i+1 < m && dp[i+1][j] < bestNext {
					bestNext = dp[i+1][j]
				}
				if j+1 < n && dp[i][j+1] < bestNext {
					bestNext = dp[i][j+1]
				}
			}
			need := bestNext - grid[i][j]
			if need < 1 {
				need = 1
			}
			dp[i][j] = need
		}
	}
	return map[string]any{"ok": true, "answer": "solution = " + presetItoa(dp[0][0])}
}

func presetIntGrid(v any) ([][]int, bool) {
	raw, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([][]int, len(raw))
	for i, rowAny := range raw {
		row, ok := rowAny.([]any)
		if !ok {
			return nil, false
		}
		out[i] = make([]int, len(row))
		for j, nAny := range row {
			n, ok := presetInt(nAny)
			if !ok {
				return nil, false
			}
			out[i][j] = n
		}
	}
	return out, true
}

func presetInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if float64(int(n)) != n {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}

func presetItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
`)
}

func numericDPTablePresetSource() string {
	return strings.TrimSpace(`
func Solve(input map[string]any) map[string]any {
	p, ok := presetNumericDPProblem(input)
	if !ok {
		return map[string]any{"ok": false, "first_failure": "expected typed numeric_dp recurrence table input", "repair_hint": "provide objective, dp_dimensions, target, base_cases, and transitions"}
	}
	answer, ok := presetEvaluateNumericDP(p)
	if !ok {
		return map[string]any{"ok": false, "first_failure": "recurrence table is not feasible for the generic preset", "repair_hint": "use acyclic predecessor offsets and reachable target state"}
	}
	return map[string]any{"ok": true, "answer": "solution = " + presetItoa(answer)}
}

type presetNumericDPTransition struct {
	Offset     []int
	Weight     int
	Multiplier int
}

type presetNumericDPProblemT struct {
	Objective   string
	Dimensions  []int
	Target      []int
	BaseCases   map[string]int
	Transitions []presetNumericDPTransition
	Modulo      int
}

func presetNumericDPProblem(input map[string]any) (presetNumericDPProblemT, bool) {
	objective, ok := input["objective"].(string)
	if !ok || (objective != "min" && objective != "max" && objective != "count") {
		return presetNumericDPProblemT{}, false
	}
	dimensions, ok := presetIntVector(input["dp_dimensions"])
	if !ok || len(dimensions) == 0 {
		return presetNumericDPProblemT{}, false
	}
	for _, n := range dimensions {
		if n <= 0 {
			return presetNumericDPProblemT{}, false
		}
	}
	target, ok := presetIntVector(input["target"])
	if !ok || len(target) != len(dimensions) || !presetIndexInBounds(target, dimensions) {
		return presetNumericDPProblemT{}, false
	}
	baseCases, ok := presetBaseCases(input["base_cases"], dimensions)
	if !ok || len(baseCases) == 0 {
		return presetNumericDPProblemT{}, false
	}
	transitions, ok := presetTransitions(input["transitions"], len(dimensions))
	if !ok || len(transitions) == 0 {
		return presetNumericDPProblemT{}, false
	}
	modulo := 0
	if rawModulo, exists := input["modulo"]; exists {
		n, ok := presetInt(rawModulo)
		if !ok || n <= 0 {
			return presetNumericDPProblemT{}, false
		}
		modulo = n
	}
	return presetNumericDPProblemT{Objective: objective, Dimensions: dimensions, Target: target, BaseCases: baseCases, Transitions: transitions, Modulo: modulo}, true
}

func presetEvaluateNumericDP(p presetNumericDPProblemT) (int, bool) {
	values := map[string]int{}
	var walk func([]int, int) bool
	walk = func(idx []int, axis int) bool {
		if axis == len(p.Dimensions) {
			key := presetIndexKey(idx)
			if value, ok := p.BaseCases[key]; ok {
				values[key] = presetApplyModulo(value, p.Modulo)
				return true
			}
			have := false
			best := 0
			sum := 0
			for _, tr := range p.Transitions {
				pred := make([]int, len(idx))
				for i := range idx {
					pred[i] = idx[i] + tr.Offset[i]
				}
				if !presetIndexInBounds(pred, p.Dimensions) {
					continue
				}
				if !presetIndexBefore(pred, idx) {
					return false
				}
				prev, ok := values[presetIndexKey(pred)]
				if !ok {
					continue
				}
				switch p.Objective {
				case "min":
					candidate := prev + tr.Weight
					if !have || candidate < best {
						best = candidate
					}
				case "max":
					candidate := prev + tr.Weight
					if !have || candidate > best {
						best = candidate
					}
				case "count":
					sum += prev * tr.Multiplier
					sum = presetApplyModulo(sum, p.Modulo)
				}
				have = true
			}
			if p.Objective == "count" {
				values[key] = presetApplyModulo(sum, p.Modulo)
				return true
			}
			if !have {
				return false
			}
			values[key] = presetApplyModulo(best, p.Modulo)
			return true
		}
		for i := 0; i < p.Dimensions[axis]; i++ {
			idx[axis] = i
			if !walk(idx, axis+1) {
				return false
			}
		}
		return true
	}
	if !walk(make([]int, len(p.Dimensions)), 0) {
		return 0, false
	}
	answer, ok := values[presetIndexKey(p.Target)]
	return answer, ok
}

func presetBaseCases(value any, dimensions []int) (map[string]int, bool) {
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := map[string]int{}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		idx, ok := presetIntVector(m["index"])
		if !ok || len(idx) != len(dimensions) || !presetIndexInBounds(idx, dimensions) {
			return nil, false
		}
		value, ok := presetInt(m["value"])
		if !ok {
			return nil, false
		}
		out[presetIndexKey(idx)] = value
	}
	return out, true
}

func presetTransitions(value any, rank int) ([]presetNumericDPTransition, bool) {
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := []presetNumericDPTransition{}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		offset, ok := presetIntVector(m["offset"])
		if !ok || len(offset) != rank {
			return nil, false
		}
		weight := 0
		if rawWeight, exists := m["weight"]; exists {
			n, ok := presetInt(rawWeight)
			if !ok {
				return nil, false
			}
			weight = n
		}
		multiplier := 1
		if rawMultiplier, exists := m["multiplier"]; exists {
			n, ok := presetInt(rawMultiplier)
			if !ok {
				return nil, false
			}
			multiplier = n
		}
		out = append(out, presetNumericDPTransition{Offset: offset, Weight: weight, Multiplier: multiplier})
	}
	return out, true
}

func presetIntVector(value any) ([]int, bool) {
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]int, len(raw))
	for i, item := range raw {
		n, ok := presetInt(item)
		if !ok {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

func presetIndexInBounds(idx, dimensions []int) bool {
	if len(idx) != len(dimensions) {
		return false
	}
	for i := range idx {
		if idx[i] < 0 || idx[i] >= dimensions[i] {
			return false
		}
	}
	return true
}

func presetIndexBefore(a, b []int) bool {
	for i := range a {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

func presetIndexKey(idx []int) string {
	out := ""
	for i, n := range idx {
		if i > 0 {
			out += ","
		}
		out += presetItoa(n)
	}
	return out
}

func presetApplyModulo(value, modulo int) int {
	if modulo <= 0 {
		return value
	}
	value = value % modulo
	if value < 0 {
		value += modulo
	}
	return value
}

func presetInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if float64(int(n)) != n {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}

func presetItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
`)
}

func explicitShortestPathPresetSource() string {
	return strings.TrimSpace(`
func Solve(input map[string]any) map[string]any {
	nodes, nodeSet, ok := presetNodes(input["nodes"])
	if !ok || len(nodes) == 0 {
		return map[string]any{"ok": false, "first_failure": "expected non-empty nodes array", "repair_hint": "provide explicit nodes as strings or integer ids"}
	}
	start, okStart := presetNodeID(input["start_node"])
	goal, okGoal := presetNodeID(input["goal_node"])
	if !okStart || !okGoal || !nodeSet[start] || !nodeSet[goal] {
		return map[string]any{"ok": false, "first_failure": "start_node or goal_node is missing from nodes", "repair_hint": "use only node ids declared in nodes"}
	}
	rawEdges, ok := input["edges"].([]any)
	if !ok {
		return map[string]any{"ok": false, "first_failure": "expected edges array", "repair_hint": "provide edges as [[from, to], ...]"}
	}
	directed := true
	if rawDirected, ok := input["directed"].(bool); ok {
		directed = rawDirected
	}
	adj := map[string][]string{}
	for _, rawEdge := range rawEdges {
		from, to, ok := presetEdge(rawEdge)
		if !ok || !nodeSet[from] || !nodeSet[to] {
			return map[string]any{"ok": false, "first_failure": "edge endpoint is not declared in nodes", "repair_hint": "validate every directed edge against nodes before searching"}
		}
		adj[from] = append(adj[from], to)
		if !directed {
			adj[to] = append(adj[to], from)
		}
	}
	answer := -1
	if start == goal {
		answer = 0
	} else {
		dist := map[string]int{start: 0}
		queue := []string{start}
		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			for _, next := range adj[node] {
				if _, seen := dist[next]; seen {
					continue
				}
				dist[next] = dist[node] + 1
				if next == goal {
					answer = dist[next]
					queue = nil
					break
				}
				queue = append(queue, next)
			}
		}
	}
	return map[string]any{"ok": true, "answer": "solution = " + presetItoa(answer)}
}

func presetNodes(v any) ([]string, map[string]bool, bool) {
	raw, ok := v.([]any)
	if !ok {
		return nil, nil, false
	}
	nodes := []string{}
	seen := map[string]bool{}
	for _, rawNode := range raw {
		node, ok := presetNodeID(rawNode)
		if !ok {
			return nil, nil, false
		}
		if seen[node] {
			continue
		}
		seen[node] = true
		nodes = append(nodes, node)
	}
	return nodes, seen, true
}

func presetEdge(v any) (string, string, bool) {
	if raw, ok := v.([]any); ok {
		if len(raw) < 2 {
			return "", "", false
		}
		from, okFrom := presetNodeID(raw[0])
		to, okTo := presetNodeID(raw[1])
		return from, to, okFrom && okTo
	}
	raw, ok := v.(map[string]any)
	if !ok {
		return "", "", false
	}
	from, okFrom := presetNodeID(raw["from"])
	to, okTo := presetNodeID(raw["to"])
	return from, to, okFrom && okTo
}

func presetNodeID(v any) (string, bool) {
	switch typed := v.(type) {
	case string:
		return typed, typed != ""
	case float64:
		n := int(typed)
		if float64(n) != typed {
			return "", false
		}
		return presetItoa(n), true
	case int:
		return presetItoa(typed), true
	default:
		return "", false
	}
}

func presetItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
`)
}

func buildGraphSearchHelperContract() string {
	return strings.TrimSpace(`
Generic graph-search helper contract:
- Treat the typed input as a graph problem, not prose generation.
- Identify nodes, directed edges, start state, goal state, transition rules, and objective.
- Use a deterministic graph algorithm or dynamic-programming recurrence when the graph is acyclic or monotone.
- For grid DAG resource paths, let dp[node] be the minimum resource needed on entering node so some allowed suffix path reaches the goal while resource stays positive.
- For explicit unweighted graphs, run BFS from start_node over edges and return the exact shortest path length, or -1 if unreachable.
- Return ok:true only with a checked answer in the requested format.
- If the typed graph objective is not represented by the provided fields, return ok:false with first_failure and repair_hint rather than guessing.
`)
}

func buildNumericDPHelperContract() string {
	return strings.TrimSpace(`
Generic numeric-DP helper contract:
- Treat the typed input as a finite recurrence/table dynamic program, not prose generation.
- Use only explicit typed fields: objective, dp_dimensions, target, base_cases, transitions, and optional modulo.
- Supported objectives are min, max, and count.
- Table indexes are zero-based integer tuples. Base cases set exact table values.
- Each transition is a predecessor offset relative to the current index. It must point to an earlier table cell under lexicographic table order.
- For min/max, candidate = predecessor_value + weight and the objective selects min or max.
- For count, contribution = predecessor_value * multiplier and contributions are summed, with optional modulo.
- Return ok:true only with one checked answer in the requested format: solution = <integer>.
- If the typed DP contract is not represented by the provided fields, return ok:false with first_failure and repair_hint rather than guessing from prose.
`)
}

func finiteDomainConstraintPresetSource() string {
	return strings.TrimSpace(`
func Solve(input map[string]any) map[string]any {
	vars, ok := presetFiniteDomainVars(input["variables"])
	if !ok || len(vars) == 0 {
		return map[string]any{"ok": false, "first_failure": "expected variables with finite integer min/max domains", "repair_hint": "provide variables as [{name,min,max}, ...]"}
	}
	constraints, ok := input["constraints"].([]any)
	if !ok || len(constraints) == 0 {
		return map[string]any{"ok": false, "first_failure": "expected non-empty typed constraints", "repair_hint": "provide constraints as expression objects"}
	}
	known := presetFiniteDomainKnown(input["known_values"])
	assign := map[string]float64{}
	var found map[string]float64
	var search func(int) bool
	search = func(idx int) bool {
		if idx == len(vars) {
			for _, raw := range constraints {
				ok, valid := presetFiniteDomainConstraintOK(raw, assign, known)
				if !valid || !ok {
					return false
				}
			}
			found = map[string]float64{}
			for _, v := range vars {
				found[v.Name] = assign[v.Name]
			}
			return true
		}
		v := vars[idx]
		for n := v.Min; n <= v.Max; n++ {
			assign[v.Name] = float64(n)
			if search(idx + 1) {
				return true
			}
		}
		delete(assign, v.Name)
		return false
	}
	if !search(0) {
		return map[string]any{"ok": false, "first_failure": "no assignment satisfies the finite-domain constraints", "repair_hint": "check bounds and typed constraint expressions"}
	}
	return map[string]any{"ok": true, "answer": "solution = " + presetFiniteDomainAnswer(vars, found)}
}

type presetFiniteDomainVar struct {
	Name string
	Min int
	Max int
}

func presetFiniteDomainVars(raw any) ([]presetFiniteDomainVar, bool) {
	items, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	out := []presetFiniteDomainVar{}
	seen := map[string]bool{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		name, ok := m["name"].(string)
		if !ok || name == "" || seen[name] {
			return nil, false
		}
		min, okMin := presetFiniteDomainInt(m["min"])
		max, okMax := presetFiniteDomainInt(m["max"])
		if !okMin || !okMax || min > max {
			return nil, false
		}
		seen[name] = true
		out = append(out, presetFiniteDomainVar{Name: name, Min: min, Max: max})
	}
	return out, true
}

func presetFiniteDomainKnown(raw any) map[string]float64 {
	out := map[string]float64{}
	m, ok := raw.(map[string]any)
	if !ok {
		return out
	}
	for k, v := range m {
		if n, ok := presetFiniteDomainNumber(v); ok {
			out[k] = n
		}
	}
	return out
}

func presetFiniteDomainConstraintOK(raw any, assign map[string]float64, known map[string]float64) (bool, bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return false, false
	}
	op, ok := m["op"].(string)
	if !ok {
		return false, false
	}
	left, okLeft := presetFiniteDomainExpr(m["left"], assign, known)
	right, okRight := presetFiniteDomainExpr(m["right"], assign, known)
	if !okLeft || !okRight {
		return false, false
	}
	switch op {
	case "eq":
		return left == right, true
	case "ne":
		return left != right, true
	case "lt":
		return left < right, true
	case "lte":
		return left <= right, true
	case "gt":
		return left > right, true
	case "gte":
		return left >= right, true
	default:
		return false, false
	}
}

func presetFiniteDomainExpr(raw any, assign map[string]float64, known map[string]float64) (float64, bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return 0, false
	}
	if v, exists := m["const"]; exists {
		return presetFiniteDomainNumber(v)
	}
	if name, ok := m["var"].(string); ok && name != "" {
		v, exists := assign[name]
		return v, exists
	}
	if name, ok := m["known"].(string); ok && name != "" {
		v, exists := known[name]
		return v, exists
	}
	if op, ok := m["op"].(string); ok && op != "" {
		args, ok := m["args"].([]any)
		if !ok || len(args) == 0 {
			return 0, false
		}
		values := []float64{}
		for _, arg := range args {
			v, ok := presetFiniteDomainExpr(arg, assign, known)
			if !ok {
				return 0, false
			}
			values = append(values, v)
		}
		return presetFiniteDomainApplyOp(op, values)
	}
	if fn, ok := m["func"].(string); ok && fn != "" {
		args, ok := m["args"].([]any)
		if !ok || len(args) == 0 {
			return 0, false
		}
		values := []float64{}
		for _, arg := range args {
			v, ok := presetFiniteDomainExpr(arg, assign, known)
			if !ok {
				return 0, false
			}
			values = append(values, v)
		}
		return presetFiniteDomainApplyFunc(fn, values)
	}
	return 0, false
}

func presetFiniteDomainApplyOp(op string, values []float64) (float64, bool) {
	switch op {
	case "add":
		total := 0.0
		for _, v := range values {
			total += v
		}
		return total, true
	case "sub":
		total := values[0]
		for _, v := range values[1:] {
			total -= v
		}
		return total, true
	case "mul":
		total := 1.0
		for _, v := range values {
			total *= v
		}
		return total, true
	case "div":
		if len(values) != 2 || values[1] == 0 {
			return 0, false
		}
		return values[0] / values[1], true
	case "mod":
		if len(values) != 2 || int(values[1]) == 0 {
			return 0, false
		}
		return float64(int(values[0]) % int(values[1])), true
	case "neg":
		if len(values) != 1 {
			return 0, false
		}
		return -values[0], true
	case "min":
		best := values[0]
		for _, v := range values[1:] {
			if v < best {
				best = v
			}
		}
		return best, true
	case "max":
		best := values[0]
		for _, v := range values[1:] {
			if v > best {
				best = v
			}
		}
		return best, true
	default:
		return 0, false
	}
}

func presetFiniteDomainApplyFunc(fn string, values []float64) (float64, bool) {
	switch fn {
	case "abs":
		if len(values) != 1 {
			return 0, false
		}
		if values[0] < 0 {
			return -values[0], true
		}
		return values[0], true
	case "gcd":
		if len(values) != 2 {
			return 0, false
		}
		return float64(presetFiniteDomainGCD(int(values[0]), int(values[1]))), true
	default:
		return 0, false
	}
}

func presetFiniteDomainAnswer(vars []presetFiniteDomainVar, assign map[string]float64) string {
	out := "{"
	for i, v := range vars {
		if i > 0 {
			out += ","
		}
		out += string(byte(34)) + v.Name + string(byte(34)) + ":" + presetItoa(int(assign[v.Name]))
	}
	return out + "}"
}

func presetFiniteDomainInt(v any) (int, bool) {
	n, ok := presetFiniteDomainNumber(v)
	if !ok || float64(int(n)) != n {
		return 0, false
	}
	return int(n), true
}

func presetFiniteDomainNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

func presetFiniteDomainGCD(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func presetItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
`)
}

func buildConstraintSolverHelperContract() string {
	return strings.TrimSpace(`
Generic constraint-solver helper contract:
- Treat the typed input as a finite-domain constraint problem, not prose generation.
- Use only explicit fields: variables, known_values, constraints, claims, and requested_outputs.
- Variables are finite integer domains with min and max bounds.
- Supported constraint comparisons are eq, ne, lt, lte, gt, and gte over expression objects.
- Search or propagate deterministically over the declared domains and verify every constraint before returning.
- Return ok:true only with one checked answer in the requested format: solution = {"variable": integer, ...}.
- If the typed constraint contract is not represented by the provided fields, return ok:false with first_failure and repair_hint rather than guessing from prose.
`)
}

func buildPackageWasteOptimizationHelperContract() string {
	return strings.TrimSpace(`
Generic resource-allocation helper contract:
- Treat packages and suppliers as an explicit finite optimization instance.
- Each supplier's box sizes have infinite supply; the same box size may be reused for many packages.
- For each supplier, assign every package to the smallest available box size >= package size.
- A supplier is invalid only when its largest available box is smaller than at least one package.
- Minimize total waste across valid suppliers, then return the minimum modulo 1000000007.
- Return ok:true only with one checked answer in the requested format: solution = <integer>.
`)
}

func buildSymbolicTraceHelperContract() string {
	return strings.TrimSpace(`
Generic symbolic-trace helper contract:
- Treat the typed input as an ordered symbolic trace, not prose generation.
- Use only explicit fields: program, queries, trace_kind, events, state, rules, and checks.
- Parse the program/events into an ordered sequence of operations.
- Maintain an environment mapping variables to types/values, updating step by step.
- Apply each operation in order, checking invariants after every step.
- Answer each query from the queries field by looking up the final or intermediate state.
- Return ok:true only with all query answers in the requested JSON format.
- If verification fails, return ok:false with first_failure, failed_step, observed, expected, and repair_hint.
`)
}

func buildCandidateVerifyHelperContract() string {
	return strings.TrimSpace(`
Generic candidate-verify helper contract:
- Treat the typed input as a candidate enumeration and verification problem, not prose generation.
- Use only explicit fields: candidates, predicates, selection_rule, and output_schema.
- candidates is a list of items to evaluate (SMILES strings, moves, tuples, etc.).
- predicates is a list of named property checks to apply to each candidate.
- selection_rule describes how to pick the answer: "best", "all_matching", "nth", "count_matching", etc.
- Evaluate every predicate for every candidate. Do not skip or short-circuit.
- If a required library is unavailable (e.g., rdkit, python-chess), return ok:false with a structured failure naming the missing library — do not time out attempting to install it.
- Return ok:true only with the answer in the requested output_schema format.
- If verification fails, return ok:false with first_failure, failed_candidate, observed, expected, and repair_hint.
`)
}

func buildSingleWorkItemHelperContract() string {
	return strings.TrimSpace(`
Generic single-work-item helper contract:
- Treat work_item_question and expected_output as the primary local task.
- Use root_task only for definitions and constraints that the local task explicitly references.
- Do not parse, invent, or solve node_0/node_1 dependency graphs unless nodes, dependencies, or problems are explicitly present in input.
- Return one concrete checked answer as solution = <value>, or return ok:false with first_failure and repair_hint.
- Never return UNKNOWN, UNSOLVED, placeholders, scaffold metadata, or an answer_format template as the answer.
- Verify the candidate by direct substitution, simulation, or a small independent check before returning ok:true.
`)
}

func buildExplicitDAGHelperContract() string {
	return strings.TrimSpace(`
Generic explicit-DAG helper contract:
- Treat problems, dependencies, dependency_summaries, work_item_question, and root_task as a dependency graph.
- If problems is only a list of labels, recover the actual subproblem text from root_task or dependency_summaries before solving.
- Solve leaf prerequisites first, substitute concrete values into dependent nodes, and keep a small checked table of node answers.
- Return the concrete values requested by expected_output; if multiple node values are requested, return solution = {"node_id": value, ...}.
- Do not return a single scalar unless expected_output asks for exactly one scalar.
- Never mark a node solved with UNKNOWN, UNSOLVED, null, or a placeholder.
`)
}

const (
	helperPreflightMaxInputChars = 50000
)

// helperPreflightCheck validates the helper input packet before spending LLM
// tokens. Returns an error if the packet is too large or structurally invalid.
func helperPreflightCheck(input map[string]any, handoff BraidNodeHandoff) error {
	if len(input) == 0 {
		return nil
	}
	// Estimate serialized size.
	estimated, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("input serialization estimate failed: %w", err)
	}
	if len(estimated) > helperPreflightMaxInputChars {
		return fmt.Errorf("helper input packet too large: %d bytes (max %d); split the work item into smaller sub-items", len(estimated), helperPreflightMaxInputChars)
	}
	// Check for known library requirements in the handoff scaffold.
	switch handoff.ScaffoldClass {
	case BraidScaffoldClassSymbolicTrace:
		// Symbolic trace helpers should work without external libraries.
	case BraidScaffoldClassFiniteStateTransition:
		// State transition helpers use pure Python.
	default:
		// No preflight library checks for other scaffolds yet.
	}
	return nil
}

// attemptSplitHelperExecution tries to decompose an oversized helper input
// into smaller sub-items and execute them sequentially, then merge the results.
// Returns the merged summary and true if the split succeeded, or ("", false)
// if the input cannot be meaningfully split.
func attemptSplitHelperExecution(
	ctx context.Context,
	phaseName string,
	node BraidNode,
	input map[string]any,
	handoff BraidNodeHandoff,
	instructions string,
	toolExec *replToolExecutor,
	output *engine.EngineOutput,
) (string, bool) {
	if toolExec == nil || toolExec.helperFactory == nil {
		return "", false
	}
	// Build a synthetic work item to analyze for splitting.
	splitItem := generalsolver.WorkItem{
		ID:        node.ID,
		Goal:      node.Question,
		Archetype: generalsolver.ArchetypeMixed,
		Payload:   input,
	}
	plan := generalsolver.AnalyzeForSplit(splitItem)
	if plan.Strategy == generalsolver.SplitStrategyNone {
		return "", false
	}

	// Extract query-able chunks from the input.
	chunks := generalsolver.ExtractQueryableChunks(input)
	if len(chunks) < 2 {
		return "", false
	}
	if len(chunks) > generalsolver.SplitMaxSubItems {
		chunks = chunks[:generalsolver.SplitMaxSubItems]
	}

	recordBraidNodeEvent(toolExec, phaseName, 0, node, "split", fmt.Sprintf("splitting into %d sub-items: %s", len(chunks), plan.Reason))

	var subAnswers []string
	var failures []string
	for i, chunk := range chunks {
		// Build a sub-input with just this chunk.
		subInput := map[string]any{
			"split_role":   "solve",
			"chunk_index":  i,
			"total_chunks": len(chunks),
			"parent_id":    node.ID,
		}
		// Merge chunk data into sub-input.
		for k, v := range chunk {
			subInput[k] = v
		}
		// Keep non-chunk fields from original input (e.g., environment).
		for k, v := range input {
			switch k {
			case "queries", "sub_problems", "subproblems", "bindings", "events", "constraints":
				// Skip the array being split.
			default:
				if _, exists := subInput[k]; !exists {
					subInput[k] = v
				}
			}
		}

		// Check size.
		if estimated, err := json.Marshal(subInput); err != nil || len(estimated) > helperPreflightMaxInputChars {
			recordBraidNodeEvent(toolExec, phaseName, 0, node, "split_chunk_oversized", fmt.Sprintf("chunk %d still too large (%d bytes)", i, len(estimated)))
			failures = append(failures, fmt.Sprintf("chunk_%d oversized", i))
			continue
		}

		subArgsMap := map[string]any{
			"prompt":       RenderBraidHelperHandoffPrompt(handoff),
			"instructions": instructions,
			"max_attempts": 3,
			"input":        subInput,
		}
		subArgs, err := json.Marshal(subArgsMap)
		if err != nil {
			failures = append(failures, fmt.Sprintf("chunk_%d marshal_error", i))
			continue
		}

		callID := fmt.Sprintf("auto_%s_%s_split_%02d_%s", sanitizeToolCallIDPart(phaseName), sanitizeToolCallIDPart(node.ID), i, sanitizeToolCallIDPart(EphemeralHelperSolveToolName))
		rawArgs := json.RawMessage(subArgs)
		if toolExec != nil && toolExec.budget != nil {
			if err := toolExec.budget.ConsumeHelperCall(ctx); err != nil {
				toolExec.recordBudgetError(LimitHelperCalls, err)
				failures = append(failures, fmt.Sprintf("chunk_%d helper_budget_exceeded: %s", i, err.Error()))
				continue
			}
		}
		result, execErr := toolExec.helperFactory.Execute(ctx, EphemeralHelperSolveToolName, rawArgs)
		toolCall := engine.ToolCall{ID: callID, Name: EphemeralHelperSolveToolName, Arguments: rawArgs}
		toolResult := engine.ToolResult{ToolCallID: callID, Content: result}
		if execErr != nil {
			toolResult.IsError = true
			toolResult.Content = execErr.Error()
		}
		output.ToolCalls = append(output.ToolCalls, toolCall)
		output.ToolResults = append(output.ToolResults, toolResult)
		if execErr != nil {
			failures = append(failures, fmt.Sprintf("chunk_%d helper_error: %s", i, execErr.Error()))
			continue
		}

		answer := helperAnswerFromToolResult(result)
		if strings.TrimSpace(answer) == "" {
			failures = append(failures, fmt.Sprintf("chunk_%d no_answer", i))
			continue
		}
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "split_chunk_completed", fmt.Sprintf("chunk %d/%d: %s", i+1, len(chunks), safeTelemetryExcerpt(answer, 100)))
		subAnswers = append(subAnswers, answer)
	}
	if len(failures) > 0 {
		return fmt.Sprintf("status: blocked summary: split execution failed for %d/%d chunks answer: checks: %s", len(failures), len(chunks), strings.Join(failures, "; ")), true
	}

	// Merge sub-answers into a combined result.
	merged := fmt.Sprintf("status: completed summary: split-solved %d chunks answer: %s checks: ephemeral_helper_solve ran split execution over %d sub-items.",
		len(subAnswers), strings.Join(subAnswers, "; "), len(chunks))
	return merged, true
}

func formatBraidHelperNodeSummary(node BraidNode, answer string) string {
	return formatBraidHelperNodeSummaryVerified(node, answer, false)
}

func formatBraidHelperNodeSummaryVerified(node BraidNode, answer string, verified bool) string {
	var cert *RuntimeCertification
	if verified {
		cert = runtimeCertificationForNode(node, strings.TrimSpace(node.ScaffoldClass)+"/"+strings.TrimSpace(node.ScaffoldID))
	}
	return formatBraidHelperNodeSummaryCertified(node, answer, cert)
}

func formatBraidHelperNodeSummaryCertified(node BraidNode, answer string, cert *RuntimeCertification) string {
	answer = strings.TrimSpace(answer)
	verified := cert != nil && cert.Pass
	if node.Kind == "verify" {
		if !verified {
			if braidPassFalseRE.MatchString(answer) {
				return "status: blocked summary: answer: " + answer + " checks: ephemeral_helper_solve reported a concrete verification failure."
			}
			return "status: blocked summary: answer: pass: false checks: helper verification is non-authoritative without a runtime verifier. detail: " + safeTelemetryExcerpt(answer, 600)
		}
		if pass, ok := braidVerifyAnswerJSONPass(answer); ok {
			if pass {
				return formatBraidCertifiedArtifactString("pass", answer, []any{"ephemeral_helper_solve simulated original constraints"}, cert)
			}
			return "status: blocked summary: answer: " + answer + " checks: ephemeral_helper_solve simulated original constraints and found a concrete failure."
		}
		if braidPassFalseRE.MatchString(answer) {
			return "status: blocked summary: answer: " + answer + " checks: ephemeral_helper_solve simulated original constraints and found a concrete failure."
		}
		if braidPassTrueRE.MatchString(answer) || braidVerificationSummaryPassed(answer) {
			return formatBraidCertifiedArtifactString("pass", answer, []any{"ephemeral_helper_solve simulated original constraints"}, cert)
		}
		return "status: blocked summary: answer: pass: false checks: ephemeral_helper_solve simulated original constraints but did not produce a passing verification. detail: " + safeTelemetryExcerpt(answer, 600)
	}
	if isBraidSolveKind(node.Kind) && braidPassFalseRE.MatchString(answer) {
		return "status: blocked summary: answer: " + answer + " checks: ephemeral_helper_solve self-verified candidate and found a concrete failure."
	}
	if node.Kind == "cycle_solve" {
		if _, err := extractCycleSolveJSON(answer); err != nil {
			return "status: blocked summary: answer: " + safeTelemetryExcerpt(answer, 600) + " checks: ephemeral_helper_solve produced an unusable cycle candidate. detail: " + err.Error()
		}
	}
	if isBraidSolveKind(node.Kind) {
		statuses := braidSummaryStatuses(answer)
		if braidStatusesContainAny(statuses, "blocked", "failed", "failure", "error") {
			return "status: blocked summary: answer: " + safeTelemetryExcerpt(answer, 600) + " checks: ephemeral_helper_solve did not produce a verified candidate."
		}
		if !verified && braidSolveNodeRequiresRuntimeVerification(node) {
			return "status: blocked summary: answer: " + safeTelemetryExcerpt(answer, 600) + " checks: ephemeral_helper_solve produced an unverified explicit dependency candidate. detail: runtime verification is required before this solve node can feed downstream nodes."
		}
		if reason := braidSolveAnswerRejectionReason(node, answer); reason != "" {
			return "status: blocked summary: answer: " + safeTelemetryExcerpt(answer, 600) + " checks: ephemeral_helper_solve produced an unusable candidate. detail: " + reason
		}
	}
	if verified {
		return formatBraidCertifiedArtifactString("solved", answer, []any{"runtime scaffold verifier passed"}, cert)
	}
	return "status: completed summary: status: solved answer: " + answer + " checks: ephemeral_helper_solve produced and ran an executable helper for this node."
}

func formatBraidCertifiedArtifactString(status string, answer string, checks []any, cert *RuntimeCertification) string {
	artifact := braidNodeArtifact{
		Status:     strings.ToLower(strings.TrimSpace(status)),
		Answer:     strings.TrimSpace(answer),
		Checks:     checks,
		Confidence: 1,
		Provenance: map[string]any{
			"runtime_certification": cert,
		},
	}
	if body, err := json.Marshal(artifact); err == nil {
		return string(body)
	}
	return "status: completed summary: status: " + status + " answer: " + answer + " checks: runtime certification passed."
}

func braidVerifyAnswerJSONPass(answer string) (bool, bool) {
	payload := strings.TrimSpace(answer)
	if strings.HasPrefix(strings.ToLower(payload), "solution") {
		_, after, ok := strings.Cut(payload, "=")
		if !ok {
			return false, false
		}
		payload = strings.TrimSpace(after)
	}
	if idx := strings.Index(payload, "{"); idx >= 0 {
		payload = payload[idx:]
	}
	var decoded any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return false, false
	}
	return braidVerifyJSONPass(decoded)
}

func braidVerifyJSONPass(value any) (bool, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if raw, ok := typed["pass"]; ok {
			if b, ok := raw.(bool); ok {
				return b, true
			}
		}
		if raw, ok := typed["verified"]; ok {
			if b, ok := raw.(bool); ok {
				return b, true
			}
		}
		for _, child := range typed {
			if pass, ok := braidVerifyJSONPass(child); ok {
				return pass, true
			}
		}
	case []any:
		for _, child := range typed {
			if pass, ok := braidVerifyJSONPass(child); ok {
				return pass, true
			}
		}
	}
	return false, false
}

func normalizeBraidVerificationFailureSummary(node BraidNode, summary string) (string, bool) {
	if node.Kind != "verify" || braidVerificationSummaryPassed(summary) {
		return "", false
	}
	if !braidVerificationSummaryHasConcreteFailure(summary) {
		return "", false
	}
	detail := safeTelemetryExcerpt(strings.TrimSpace(summary), 900)
	if detail == "" {
		detail = "verification failed"
	}
	return "status: blocked summary: answer: pass: false first_failure: " + detail + " checks: verifier found a concrete failed constraint; repair the upstream candidate instead of retrying verifier helper.", true
}

func braidVerificationSummaryHasConcreteFailure(summary string) bool {
	lower := strings.ToLower(strings.TrimSpace(summary))
	if lower == "" {
		return false
	}
	if braidPassFalseRE.MatchString(summary) {
		return true
	}
	for _, marker := range []string{
		"first_failure",
		"first failure",
		"failed constraint",
		"failing constraint",
		"constraint failure",
		"critical constraint",
		"constraint violation",
		"does not satisfy",
		"not satisfy",
		"mismatch",
		"not equal",
		"!=",
		"≠",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func braidSolveNodeRequiresRuntimeVerification(node BraidNode) bool {
	if !isBraidSolveKind(node.Kind) {
		return false
	}
	if strings.TrimSpace(node.ScaffoldClass) != BraidScaffoldClassExplicitDAG || strings.TrimSpace(node.ScaffoldID) != BraidScaffoldIDSearchBacktrackV1 {
		return false
	}
	if strings.Contains(node.ID, "__adaptive_") || strings.HasSuffix(node.ID, "__adaptive_merge") {
		if len(stringSliceFromAny(node.InputSchema["target_node"])) == 1 || len(stringSliceFromAny(node.InputSchema["target_nodes"])) == 1 {
			return false
		}
		return true
	}
	return false
}

func braidSolveAnswerRejectionReason(node BraidNode, answer string) string {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return "empty answer"
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lower, "<answer>") || strings.Contains(lower, "\\u003canswer\\u003e") || strings.Contains(lower, "u003canswer"):
		return "answer contains placeholder answer markers"
	case strings.Contains(lower, `"root_task"`) || strings.Contains(lower, `"work_item_question"`) || strings.Contains(lower, `"answer_format"`):
		return "answer appears to echo the helper input packet"
	case strings.Contains(lower, `"scaffold_class"`) || strings.Contains(lower, `"scaffold_id"`) || strings.Contains(lower, `"task_type"`):
		return "answer appears to echo scaffold metadata"
	case strings.Contains(lower, "official task text begins"):
		return "answer includes runtime prompt text"
	case strings.Contains(strings.ToUpper(trimmed), "UNSOLVED"):
		return "answer contains unresolved values"
	case strings.Contains(strings.ToUpper(trimmed), "UNKNOWN"):
		return "answer contains unknown values"
	}
	expected := strings.ToLower(strings.TrimSpace(node.ExpectedOutput))
	if expected != "" && expectedOutputRequiresStructuredNodeValues(expected) {
		hasNodeValue := strings.Contains(lower, "node_") || strings.Contains(trimmed, "[") || strings.Contains(trimmed, "{")
		if !hasNodeValue {
			return "answer does not contain structured node values required by expected_output"
		}
	}
	if isMultiVariableSolveNode(node) && !strings.Contains(trimmed, "{") && !strings.Contains(trimmed, "[") {
		return "answer does not contain structured values required by multi-variable input_schema"
	}
	if expected != "" && (strings.Contains(expected, "final answer") || strings.Contains(expected, "final answers")) {
		if len([]rune(trimmed)) < 16 {
			return "answer too short for requested final values"
		}
	}
	if strings.HasPrefix(lower, "solution = ") {
		payload := strings.TrimSpace(trimmed[len("solution = "):])
		if len([]rune(payload)) == 1 && strings.ContainsAny(payload, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			return "single-letter solution is not a structured candidate"
		}
	}
	return ""
}

func expectedOutputRequiresStructuredNodeValues(expected string) bool {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return false
	}
	nodeIDs := extractBraidNodeIDsFromText(expected)
	if len(nodeIDs) > 1 {
		return true
	}
	return strings.Contains(expected, "answers for nodes") ||
		strings.Contains(expected, "answer for nodes") ||
		(strings.Contains(expected, "nodes ") && strings.Contains(expected, "through"))
}

func isMultiVariableSolveNode(node BraidNode) bool {
	if !isBraidSolveKind(node.Kind) || len(node.InputSchema) == 0 {
		return false
	}
	vars := stringSliceFromAny(node.InputSchema["variables"])
	if len(vars) > 1 {
		return true
	}
	targets := stringSliceFromAny(node.InputSchema["target_nodes"])
	return len(targets) > 1
}

func helperAnswerFromToolResult(result string) string {
	answer, _ := helperAnswerAndCertificationFromToolResult(BraidNode{}, result)
	return answer
}

//nolint:unused // Kept for older helper result consumers during LongCoT runtime migration.
func helperAnswerAndVerifiedFromToolResult(result string) (string, bool) {
	answer, cert := helperAnswerAndCertificationFromToolResult(BraidNode{}, result)
	return answer, cert != nil && cert.Pass
}

func helperAnswerAndCertificationFromToolResult(node BraidNode, result string) (string, *RuntimeCertification) {
	var decoded struct {
		OK           bool           `json:"ok"`
		Answer       string         `json:"answer"`
		Error        string         `json:"error"`
		Verification map[string]any `json:"verification"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err == nil {
		if !decoded.OK || strings.TrimSpace(decoded.Answer) == "" {
			return "", nil
		}
		cert := certificationFromHelperVerification(node, decoded.Verification)
		return strings.TrimSpace(decoded.Answer), cert
	}
	return strings.TrimSpace(result), nil
}

func certificationFromHelperVerification(node BraidNode, verification map[string]any) *RuntimeCertification {
	if len(verification) == 0 {
		return nil
	}
	pass, _ := verification["pass"].(bool)
	verifierID := strings.TrimSpace(stringFromAny(verification["verifier_id"]))
	verifierKind := strings.TrimSpace(stringFromAny(verification["verifier_kind"]))
	if !pass || verifierID == "" || verifierKind != runtimeVerifierKindScaffold {
		return nil
	}
	return &RuntimeCertification{
		NodeID:          strings.TrimSpace(node.ID),
		Pass:            true,
		VerifierID:      verifierID,
		VerifierKind:    verifierKind,
		ScaffoldClass:   strings.TrimSpace(node.ScaffoldClass),
		ScaffoldID:      strings.TrimSpace(node.ScaffoldID),
		CandidateDigest: strings.TrimSpace(stringFromAny(verification["candidate_digest"])),
		InputDigest:     strings.TrimSpace(stringFromAny(verification["input_digest"])),
	}
}

func helperFailureSummaryFromToolResult(node BraidNode, result string) (string, bool) {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return "", false
	}
	if ok, _ := decoded["ok"].(bool); ok {
		return "", false
	}
	detail := strings.TrimSpace(stringFromAny(decoded["error"]))
	if detail == "" {
		detail = "helper returned ok:false without a usable answer"
	}
	if preset := strings.TrimSpace(stringFromAny(decoded["preset"])); preset != "" {
		detail += "; preset=" + preset
	}
	if attempts, ok := decoded["attempts"].([]any); ok && len(attempts) > 0 {
		detail += "; attempts=" + compactHelperFailureAttempts(attempts)
	}
	counterexample := helperCounterexampleSummaryFromToolResult(decoded)
	if node.Kind == "verify" {
		summary := "status: blocked summary: answer: pass: false checks: ephemeral_helper_solve failed before producing a passing verification. detail: " + safeTelemetryExcerpt(detail, 900)
		if counterexample != "" {
			summary += " counterexample: " + counterexample
		}
		return summary, true
	}
	summary := "status: blocked summary: answer: checks: ephemeral_helper_solve failed before producing a usable candidate. detail: " + safeTelemetryExcerpt(detail, 900)
	if counterexample != "" {
		summary += " counterexample: " + counterexample
	}
	return summary, true
}

func helperVerifyFailureSummaryFromToolResult(result string) (string, bool) {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return "", false
	}
	output, _ := decoded["output_summary"].(map[string]any)
	if len(output) == 0 {
		return "", false
	}
	pass, hasPass := output["pass"].(bool)
	if !hasPass || pass {
		return "", false
	}
	detailParts := []string{"pass: false"}
	for _, key := range []string{"first_failure", "failed_step", "failed_at_step", "failed_node", "message", "repair_hint"} {
		if value := strings.TrimSpace(stringFromAny(output[key])); value != "" {
			detailParts = append(detailParts, key+": "+value)
		}
	}
	for _, key := range []string{"observed", "expected"} {
		if value, ok := output[key+"_summary"]; ok {
			detailParts = append(detailParts, key+": "+safeTelemetryExcerpt(fmt.Sprintf("%v", value), 180))
		}
	}
	if answer := strings.TrimSpace(stringFromAny(output["answer"])); answer != "" {
		detailParts = append(detailParts, "candidate: "+safeTelemetryExcerpt(answer, 220))
	}
	detail := strings.Join(detailParts, "; ")
	return "status: blocked summary: answer: " + detail + " checks: ephemeral_helper_solve simulated original constraints and found a concrete failure.", true
}

func braidVerifyHelperAnswerContract(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return HelperVerifierDiagnostic{
			Pass:         false,
			FailureKind:  "verify_contract",
			FirstFailure: "empty verifier answer",
			Expected:     "pass: true or pass: false with diagnostic fields",
			RepairHint:   "return explicit pass true/false; do not return a solution line from a verify helper",
		}, true
	}
	if diag, ok := braidVerifyAnswerJSONContract(trimmed); ok {
		return diag, true
	}
	if braidPassTrueRE.MatchString(trimmed) || braidPassFalseRE.MatchString(trimmed) {
		return HelperVerifierDiagnostic{Pass: true}, true
	}
	return HelperVerifierDiagnostic{
		Pass:         false,
		FailureKind:  "verify_contract",
		FirstFailure: "verifier answer did not contain explicit pass true/false",
		Observed:     safeTelemetryExcerpt(trimmed, 300),
		Expected:     "pass: true, or pass: false first_failure: <concrete mismatch>",
		RepairHint:   "simulate/substitute the candidate and return explicit pass true/false; do not return solution = ... from a verify helper",
	}, true
}

func braidVerifyAnswerJSONContract(answer string) (HelperVerifierDiagnostic, bool) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return HelperVerifierDiagnostic{}, false
	}
	if idx := strings.Index(answer, "{"); idx >= 0 {
		answer = answer[idx:]
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(answer), &decoded); err != nil {
		return HelperVerifierDiagnostic{}, false
	}
	pass, ok := decoded["pass"].(bool)
	if !ok {
		return HelperVerifierDiagnostic{}, false
	}
	if !pass {
		return HelperVerifierDiagnostic{Pass: true}, true
	}
	if braidVerifyJSONHasNonEmptyEvidence(decoded) {
		return HelperVerifierDiagnostic{Pass: true}, true
	}
	return HelperVerifierDiagnostic{
		Pass:         false,
		FailureKind:  "verify_contract",
		FirstFailure: "verifier pass:true JSON included no concrete checks, evidence, or candidate values",
		Observed:     safeTelemetryExcerpt(answer, 300),
		Expected:     "pass:true JSON must include non-empty checks, evidence, observations, or candidate values",
		RepairHint:   "simulate or substitute the candidate and include at least one concrete check/evidence item; otherwise return pass:false with first_failure",
	}, true
}

func braidVerifyJSONHasNonEmptyEvidence(decoded map[string]any) bool {
	for _, key := range []string{"checks", "evidence", "observations", "candidate", "candidates", "verified_constraints"} {
		value, ok := decoded[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case []any:
			if len(typed) > 0 {
				return true
			}
		case map[string]any:
			if len(typed) > 0 {
				return true
			}
		case string:
			if strings.TrimSpace(typed) != "" {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func helperCounterexampleSummaryFromToolResult(decoded map[string]any) string {
	counterexample := helperCounterexampleFromToolResult(decoded)
	if len(counterexample) == 0 {
		return ""
	}
	body, err := json.Marshal(counterexample)
	if err != nil || len(body) == 0 {
		return ""
	}
	return safeTelemetryExcerpt(string(body), 700)
}

func helperCounterexampleFromToolResult(decoded map[string]any) map[string]any {
	if len(decoded) == 0 {
		return nil
	}
	if repairHarness, ok := decoded["repair_harness"].(map[string]any); ok {
		if latest, ok := repairHarness["latest_counterexample"].(map[string]any); ok && len(latest) > 0 {
			return cloneMapAny(latest)
		}
	}
	if feedback, ok := decoded["finalize_feedback"].(map[string]any); ok {
		if counterexample, ok := feedback["counterexample"].(map[string]any); ok && len(counterexample) > 0 {
			return cloneMapAny(counterexample)
		}
	}
	if attempts, ok := decoded["attempts"].([]any); ok {
		for i := len(attempts) - 1; i >= 0; i-- {
			attempt, ok := attempts[i].(map[string]any)
			if !ok {
				continue
			}
			if diag, ok := attempt["verifier_diagnostic"].(map[string]any); ok {
				if counterexample := helperFactoryCounterexamplePacket(diag); len(counterexample) > 0 {
					return counterexample
				}
			}
			if feedback, ok := attempt["finalize_feedback"].(map[string]any); ok {
				if counterexample, ok := feedback["counterexample"].(map[string]any); ok && len(counterexample) > 0 {
					return cloneMapAny(counterexample)
				}
			}
		}
	}
	return nil
}

func compactHelperFailureAttempts(attempts []any) string {
	parts := make([]string, 0, len(attempts))
	for _, raw := range attempts {
		attempt, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		stage := strings.TrimSpace(stringFromAny(attempt["stage"]))
		errText := strings.TrimSpace(stringFromAny(attempt["error"]))
		if stage == "" && errText == "" {
			continue
		}
		if errText != "" {
			parts = append(parts, stage+":"+safeTelemetryExcerpt(errText, 120))
		} else {
			parts = append(parts, stage)
		}
	}
	if len(parts) == 0 {
		return "unavailable"
	}
	return strings.Join(parts, " | ")
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("%v", value)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func verifyStackMoveCandidateFromInput(answer string, rawInput any) (bool, string, bool) {
	input, ok := rawInput.(map[string]any)
	if !ok {
		return false, "", false
	}
	diag, applicable := stackMoveAnswerVerifier(answer, input)
	if !applicable {
		return false, "", false
	}
	return diag.Pass, diag.Message, true
}

func gridResourcePathAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	base := HelperVerifierDiagnostic{Pass: false, FailedAtStep: -1, FailureKind: "graph_resource_path"}
	grid, ok := intGridFromAny(input["grid_layout"])
	if !ok || len(grid) == 0 || len(grid[0]) == 0 {
		return HelperVerifierDiagnostic{}, false
	}
	want := minInitialResourceForGrid(grid)
	got, ok := intSolutionFromAnswer(answer)
	if !ok {
		base.FailureKind = "parse"
		base.Message = "candidate does not contain a parseable integer solution"
		base.RepairHint = "return answer exactly as solution = <integer>"
		return base, true
	}
	if got != want {
		base.FailureKind = "objective_mismatch"
		base.Message = fmt.Sprintf("candidate %d does not equal minimum initial resource %d", got, want)
		base.ObservedFinal = got
		base.ExpectedFinal = want
		base.RepairHint = "compute the reverse dynamic-programming recurrence over the directed grid graph"
		return base, true
	}
	return HelperVerifierDiagnostic{Pass: true, Score: 1, FailureKind: "graph_resource_path", FailedAtStep: -1}, true
}

func explicitShortestPathAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	base := HelperVerifierDiagnostic{Pass: false, FailedAtStep: -1, FailureKind: "explicit_shortest_path"}
	graph, ok := explicitGraphFromInput(input)
	if !ok {
		return HelperVerifierDiagnostic{}, false
	}
	want := shortestPathLength(graph)
	got, ok := intSolutionFromAnswer(answer)
	if !ok {
		base.FailureKind = "parse"
		base.Message = "candidate does not contain a parseable integer solution"
		base.RepairHint = "return answer exactly as solution = <integer>"
		return base, true
	}
	if got != want {
		base.FailureKind = "objective_mismatch"
		base.Message = fmt.Sprintf("candidate %d does not equal shortest path length %d", got, want)
		base.ObservedFinal = got
		base.ExpectedFinal = want
		base.RepairHint = "run BFS over the explicit directed graph using only typed edges"
		base.Extra = map[string]any{
			"start_node": graph.Start,
			"goal_node":  graph.Goal,
		}
		return base, true
	}
	return HelperVerifierDiagnostic{Pass: true, Score: 1, FailureKind: "explicit_shortest_path", FailedAtStep: -1}, true
}

func numericDPAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	base := HelperVerifierDiagnostic{Pass: false, FailedAtStep: -1, FailureKind: "numeric_dp"}
	problem, ok := numericDPProblemFromInput(input)
	if !ok {
		return HelperVerifierDiagnostic{}, false
	}
	want, ok := evaluateNumericDP(problem)
	if !ok {
		base.FailureKind = "unsupported_recurrence"
		base.Message = "numeric DP input is outside the deterministic recurrence-table preset"
		base.RepairHint = "use explicit acyclic predecessor offsets and reachable table states"
		return base, true
	}
	got, ok := intSolutionFromAnswer(answer)
	if !ok {
		base.FailureKind = "parse"
		base.Message = "candidate does not contain a parseable integer solution"
		base.RepairHint = "return answer exactly as solution = <integer>"
		return base, true
	}
	if got != want {
		base.FailureKind = "objective_mismatch"
		base.Message = fmt.Sprintf("candidate %d does not equal recurrence-table value %d", got, want)
		base.ObservedFinal = got
		base.ExpectedFinal = want
		base.RepairHint = "fill the DP table from base cases using the declared objective and predecessor offsets"
		return base, true
	}
	return HelperVerifierDiagnostic{Pass: true, Score: 1, FailureKind: "numeric_dp", FailedAtStep: -1}, true
}

type numericDPTransition struct {
	Offset     []int
	Weight     int
	Multiplier int
}

type numericDPProblem struct {
	Objective   string
	Dimensions  []int
	Target      []int
	BaseCases   map[string]int
	Transitions []numericDPTransition
	Modulo      int
}

func numericDPProblemFromInput(input map[string]any) (numericDPProblem, bool) {
	if len(input) == 0 {
		return numericDPProblem{}, false
	}
	objective, ok := input["objective"].(string)
	if !ok {
		return numericDPProblem{}, false
	}
	objective = strings.TrimSpace(objective)
	switch objective {
	case "min", "max", "count":
	default:
		return numericDPProblem{}, false
	}
	dimensions, ok := intVectorFromAny(input["dp_dimensions"])
	if !ok || len(dimensions) == 0 {
		return numericDPProblem{}, false
	}
	for _, n := range dimensions {
		if n <= 0 {
			return numericDPProblem{}, false
		}
	}
	target, ok := intVectorFromAny(input["target"])
	if !ok || len(target) != len(dimensions) || !numericDPIndexInBounds(target, dimensions) {
		return numericDPProblem{}, false
	}
	baseCases, ok := numericDPBaseCasesFromAny(input["base_cases"], dimensions)
	if !ok || len(baseCases) == 0 {
		return numericDPProblem{}, false
	}
	transitions, ok := numericDPTransitionsFromAny(input["transitions"], len(dimensions))
	if !ok || len(transitions) == 0 {
		return numericDPProblem{}, false
	}
	modulo := 0
	if rawModulo, exists := input["modulo"]; exists {
		n, ok := intFromJSONNumberLike(rawModulo)
		if !ok || n <= 0 {
			return numericDPProblem{}, false
		}
		modulo = n
	}
	return numericDPProblem{
		Objective:   objective,
		Dimensions:  dimensions,
		Target:      target,
		BaseCases:   baseCases,
		Transitions: transitions,
		Modulo:      modulo,
	}, true
}

func evaluateNumericDP(problem numericDPProblem) (int, bool) {
	values := map[string]int{}
	var walk func(idx []int, axis int) bool
	walk = func(idx []int, axis int) bool {
		if axis == len(problem.Dimensions) {
			key := numericDPIndexKey(idx)
			if value, ok := problem.BaseCases[key]; ok {
				values[key] = applyNumericDPModulo(value, problem.Modulo)
				return true
			}
			haveCandidate := false
			best := 0
			sum := 0
			for _, transition := range problem.Transitions {
				predecessor := make([]int, len(idx))
				for i := range idx {
					predecessor[i] = idx[i] + transition.Offset[i]
				}
				if !numericDPIndexInBounds(predecessor, problem.Dimensions) {
					continue
				}
				if !numericDPIndexBefore(predecessor, idx) {
					return false
				}
				prev, ok := values[numericDPIndexKey(predecessor)]
				if !ok {
					continue
				}
				switch problem.Objective {
				case "min":
					candidate := prev + transition.Weight
					if !haveCandidate || candidate < best {
						best = candidate
					}
				case "max":
					candidate := prev + transition.Weight
					if !haveCandidate || candidate > best {
						best = candidate
					}
				case "count":
					sum += prev * transition.Multiplier
					sum = applyNumericDPModulo(sum, problem.Modulo)
				}
				haveCandidate = true
			}
			if problem.Objective == "count" {
				values[key] = applyNumericDPModulo(sum, problem.Modulo)
				return true
			}
			if !haveCandidate {
				return false
			}
			values[key] = applyNumericDPModulo(best, problem.Modulo)
			return true
		}
		for i := 0; i < problem.Dimensions[axis]; i++ {
			idx[axis] = i
			if !walk(idx, axis+1) {
				return false
			}
		}
		return true
	}
	if !walk(make([]int, len(problem.Dimensions)), 0) {
		return 0, false
	}
	answer, ok := values[numericDPIndexKey(problem.Target)]
	return answer, ok
}

func numericDPBaseCasesFromAny(value any, dimensions []int) (map[string]int, bool) {
	rawCases, ok := value.([]any)
	if !ok {
		return nil, false
	}
	baseCases := map[string]int{}
	for _, rawCase := range rawCases {
		item, ok := rawCase.(map[string]any)
		if !ok {
			return nil, false
		}
		index, ok := intVectorFromAny(item["index"])
		if !ok || len(index) != len(dimensions) || !numericDPIndexInBounds(index, dimensions) {
			return nil, false
		}
		n, ok := intFromJSONNumberLike(item["value"])
		if !ok {
			return nil, false
		}
		baseCases[numericDPIndexKey(index)] = n
	}
	return baseCases, true
}

func numericDPTransitionsFromAny(value any, rank int) ([]numericDPTransition, bool) {
	rawTransitions, ok := value.([]any)
	if !ok {
		return nil, false
	}
	transitions := make([]numericDPTransition, 0, len(rawTransitions))
	for _, rawTransition := range rawTransitions {
		item, ok := rawTransition.(map[string]any)
		if !ok {
			return nil, false
		}
		offset, ok := intVectorFromAny(item["offset"])
		if !ok || len(offset) != rank {
			return nil, false
		}
		weight := 0
		if rawWeight, exists := item["weight"]; exists {
			n, ok := intFromJSONNumberLike(rawWeight)
			if !ok {
				return nil, false
			}
			weight = n
		}
		multiplier := 1
		if rawMultiplier, exists := item["multiplier"]; exists {
			n, ok := intFromJSONNumberLike(rawMultiplier)
			if !ok {
				return nil, false
			}
			multiplier = n
		}
		transitions = append(transitions, numericDPTransition{
			Offset:     offset,
			Weight:     weight,
			Multiplier: multiplier,
		})
	}
	return transitions, true
}

func intVectorFromAny(value any) ([]int, bool) {
	rawItems, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]int, len(rawItems))
	for i, rawItem := range rawItems {
		n, ok := intFromJSONNumberLike(rawItem)
		if !ok {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

func numericDPIndexInBounds(index, dimensions []int) bool {
	if len(index) != len(dimensions) {
		return false
	}
	for i := range index {
		if index[i] < 0 || index[i] >= dimensions[i] {
			return false
		}
	}
	return true
}

func numericDPIndexBefore(a, b []int) bool {
	for i := range a {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

func numericDPIndexKey(index []int) string {
	parts := make([]string, len(index))
	for i, n := range index {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func applyNumericDPModulo(value, modulo int) int {
	if modulo <= 0 {
		return value
	}
	value %= modulo
	if value < 0 {
		value += modulo
	}
	return value
}

func finiteDomainAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	base := HelperVerifierDiagnostic{Pass: false, FailedAtStep: -1, FailureKind: "finite_domain_constraint"}
	witness, ok := finiteDomainWitnessFromInput(input)
	if !ok {
		return HelperVerifierDiagnostic{}, false
	}
	assignment, ok := finiteDomainAssignmentFromAnswer(answer, witness)
	if !ok {
		base.FailureKind = "parse"
		base.Message = "candidate does not contain a parseable finite-domain assignment"
		base.RepairHint = `return answer exactly as solution = {"variable": integer, ...}`
		return base, true
	}
	for _, variable := range witness.Variables {
		value, exists := assignment[variable.Name]
		if !exists {
			base.FailureKind = "missing_variable"
			base.Message = fmt.Sprintf("candidate does not assign variable %q", variable.Name)
			base.ExpectedFinal = variable.Name
			base.RepairHint = "return every declared variable needed to evaluate the constraints"
			return base, true
		}
		if value != float64(int(value)) || int(value) < variable.Min || int(value) > variable.Max {
			base.FailureKind = "domain_violation"
			base.Message = fmt.Sprintf("candidate assigns %s=%v outside [%d,%d]", variable.Name, value, variable.Min, variable.Max)
			base.ObservedFinal = normalizeCycleNumber(value)
			base.ExpectedFinal = []int{variable.Min, variable.Max}
			base.RepairHint = "choose an integer value inside the declared finite domain"
			return base, true
		}
	}
	checks, pass := evaluateCycleConstraints(witness, assignment)
	if !pass {
		base.FailureKind = "constraint_mismatch"
		base.Message = "candidate assignment does not satisfy all typed constraints"
		base.RepairHint = "search the declared finite domains and evaluate every constraint before returning"
		for idx, check := range checks {
			if !check.OK {
				base.FailedAtStep = idx
				base.ObservedFinal = check.Observed
				base.ExpectedFinal = check.Expected
				if check.Error != "" {
					base.Message = check.Error
				} else if check.Name != "" {
					base.Message = fmt.Sprintf("constraint %q failed", check.Name)
				}
				break
			}
		}
		base.Extra = map[string]any{"checks": checks}
		return base, true
	}
	return HelperVerifierDiagnostic{Pass: true, Score: 1, FailureKind: "finite_domain_constraint", FailedAtStep: -1}, true
}

func braidHelperInputLooksLikePackageWasteOptimization(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	_, ok := packageWasteProblemFromInput(input)
	return ok
}

type packageWasteProblem struct {
	Packages  []int
	Suppliers [][]int
}

func packageWasteProblemFromInput(input map[string]any) (packageWasteProblem, bool) {
	packages, ok := intVectorFromAny(input["packages"])
	if !ok || len(packages) == 0 {
		return packageWasteProblem{}, false
	}
	rawSuppliers, ok := input["suppliers"].([]any)
	if !ok || len(rawSuppliers) == 0 {
		return packageWasteProblem{}, false
	}
	suppliers := make([][]int, 0, len(rawSuppliers))
	for _, rawSupplier := range rawSuppliers {
		boxes, ok := intVectorFromAny(rawSupplier)
		if !ok || len(boxes) == 0 {
			return packageWasteProblem{}, false
		}
		suppliers = append(suppliers, boxes)
	}
	return packageWasteProblem{Packages: packages, Suppliers: suppliers}, true
}

func packageWasteAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	base := HelperVerifierDiagnostic{Pass: false, FailedAtStep: -1, FailureKind: "resource_allocation_min_waste"}
	problem, ok := packageWasteProblemFromInput(input)
	if !ok {
		return HelperVerifierDiagnostic{}, false
	}
	observed, ok := integerSolutionFromAnswer(answer)
	if !ok {
		base.FirstFailure = "answer did not contain solution = <integer>"
		base.RepairHint = "return answer exactly as solution = <integer>"
		return base, true
	}
	expected := packageWasteMinimum(problem)
	if observed != expected {
		base.FirstFailure = "computed minimum waste does not match deterministic allocation check"
		base.Observed = observed
		base.Expected = expected
		base.RepairHint = "supplier box sizes have infinite supply; reuse the chosen supplier's smallest fitting box size for every package"
		return base, true
	}
	return HelperVerifierDiagnostic{Pass: true, Score: 1, FailureKind: "resource_allocation_min_waste", FailedAtStep: -1, Observed: observed, Expected: expected}, true
}

func packageWasteMinimum(problem packageWasteProblem) int {
	const mod = 1000000007
	packages := append([]int(nil), problem.Packages...)
	sort.Ints(packages)
	totalPackageSize := 0
	for _, size := range packages {
		totalPackageSize += size
	}
	best := -1
	for _, supplier := range problem.Suppliers {
		boxes := append([]int(nil), supplier...)
		sort.Ints(boxes)
		if len(boxes) == 0 || boxes[len(boxes)-1] < packages[len(packages)-1] {
			continue
		}
		totalBoxSize := 0
		for _, pkg := range packages {
			idx := sort.SearchInts(boxes, pkg)
			if idx >= len(boxes) {
				totalBoxSize = -1
				break
			}
			totalBoxSize += boxes[idx]
		}
		if totalBoxSize < 0 {
			continue
		}
		waste := totalBoxSize - totalPackageSize
		if best < 0 || waste < best {
			best = waste
		}
	}
	if best < 0 {
		return -1
	}
	return best % mod
}

func integerSolutionFromAnswer(answer string) (int, bool) {
	answer = strings.TrimSpace(answer)
	idx := strings.Index(answer, "solution =")
	if idx < 0 {
		return 0, false
	}
	value := strings.TrimSpace(strings.TrimPrefix(answer[idx:], "solution ="))
	if end := strings.IndexAny(value, "\n\r\t ,.;)]}"); end > 0 {
		value = strings.TrimSpace(value[:end])
	}
	n, err := strconv.Atoi(value)
	return n, err == nil
}

func finiteDomainWitnessFromInput(input map[string]any) (CycleWitness, bool) {
	if len(input) == 0 {
		return CycleWitness{}, false
	}
	normalized := cloneMapAny(input)
	if _, ok := normalized["version"]; !ok {
		normalized["version"] = float64(cycleWitnessVersionV1)
	}
	if _, ok := normalized["checker_kind"]; !ok {
		normalized["checker_kind"] = cycleWitnessCheckerBounded
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return CycleWitness{}, false
	}
	var witness CycleWitness
	if err := json.Unmarshal(raw, &witness); err != nil {
		return CycleWitness{}, false
	}
	if err := ValidateCycleWitness(witness); err != nil {
		return CycleWitness{}, false
	}
	return witness, true
}

func finiteDomainAssignmentFromAnswer(answer string, witness CycleWitness) (map[string]float64, bool) {
	raw := strings.TrimSpace(answer)
	if idx := strings.Index(raw, "="); idx >= 0 && strings.Contains(strings.ToLower(raw[:idx]), "solution") {
		raw = strings.TrimSpace(raw[idx+1:])
	}
	raw = strings.Trim(raw, "` \t\r\n")
	if raw == "" {
		return nil, false
	}
	if strings.HasPrefix(raw, "{") {
		var decoded map[string]any
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return nil, false
		}
		out := make(map[string]float64, len(decoded))
		for key, value := range decoded {
			n, ok := finiteDomainNumberFromAny(value)
			if !ok {
				return nil, false
			}
			out[key] = n
		}
		return out, true
	}
	if len(witness.Variables) == 1 {
		n, ok := parseLeadingInt(raw)
		if !ok {
			return nil, false
		}
		return map[string]float64{witness.Variables[0].Name: float64(n)}, true
	}
	return nil, false
}

func finiteDomainNumberFromAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case json.Number:
		value, err := typed.Float64()
		return value, err == nil
	default:
		return 0, false
	}
}

func minInitialResourceForGrid(grid [][]int) int {
	m := len(grid)
	n := len(grid[0])
	dp := make([][]int, m)
	for i := range dp {
		dp[i] = make([]int, n)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			bestNext := int(^uint(0) >> 2)
			if i == m-1 && j == n-1 {
				bestNext = 1
			} else {
				if i+1 < m && dp[i+1][j] < bestNext {
					bestNext = dp[i+1][j]
				}
				if j+1 < n && dp[i][j+1] < bestNext {
					bestNext = dp[i][j+1]
				}
			}
			need := bestNext - grid[i][j]
			if need < 1 {
				need = 1
			}
			dp[i][j] = need
		}
	}
	return dp[0][0]
}

func intGridFromAny(value any) ([][]int, bool) {
	rawRows, ok := value.([]any)
	if !ok {
		return nil, false
	}
	grid := make([][]int, len(rawRows))
	width := -1
	for i, rawRow := range rawRows {
		rawCells, ok := rawRow.([]any)
		if !ok {
			return nil, false
		}
		if width < 0 {
			width = len(rawCells)
		}
		if len(rawCells) != width {
			return nil, false
		}
		grid[i] = make([]int, len(rawCells))
		for j, rawCell := range rawCells {
			n, ok := intFromJSONNumberLike(rawCell)
			if !ok {
				return nil, false
			}
			grid[i][j] = n
		}
	}
	return grid, true
}

type explicitGraphInput struct {
	Nodes    []string
	NodeSet  map[string]struct{}
	Edges    [][2]string
	Start    string
	Goal     string
	Directed bool
}

func explicitGraphFromInput(input map[string]any) (explicitGraphInput, bool) {
	rawNodes, ok := input["nodes"].([]any)
	if !ok || len(rawNodes) == 0 {
		return explicitGraphInput{}, false
	}
	graph := explicitGraphInput{
		Nodes:    make([]string, 0, len(rawNodes)),
		NodeSet:  make(map[string]struct{}, len(rawNodes)),
		Directed: true,
	}
	for _, rawNode := range rawNodes {
		node, ok := explicitGraphNodeID(rawNode)
		if !ok {
			return explicitGraphInput{}, false
		}
		if _, exists := graph.NodeSet[node]; exists {
			continue
		}
		graph.NodeSet[node] = struct{}{}
		graph.Nodes = append(graph.Nodes, node)
	}
	start, okStart := explicitGraphNodeID(input["start_node"])
	goal, okGoal := explicitGraphNodeID(input["goal_node"])
	if !okStart || !okGoal {
		return explicitGraphInput{}, false
	}
	if _, ok := graph.NodeSet[start]; !ok {
		return explicitGraphInput{}, false
	}
	if _, ok := graph.NodeSet[goal]; !ok {
		return explicitGraphInput{}, false
	}
	graph.Start = start
	graph.Goal = goal
	if directed, ok := input["directed"].(bool); ok {
		graph.Directed = directed
	}
	rawEdges, ok := input["edges"].([]any)
	if !ok {
		return explicitGraphInput{}, false
	}
	graph.Edges = make([][2]string, 0, len(rawEdges))
	for _, rawEdge := range rawEdges {
		edge, ok := explicitGraphEdge(rawEdge)
		if !ok {
			return explicitGraphInput{}, false
		}
		if _, ok := graph.NodeSet[edge[0]]; !ok {
			return explicitGraphInput{}, false
		}
		if _, ok := graph.NodeSet[edge[1]]; !ok {
			return explicitGraphInput{}, false
		}
		graph.Edges = append(graph.Edges, edge)
	}
	return graph, true
}

func explicitGraphNodeID(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		node := strings.TrimSpace(typed)
		return node, node != ""
	case json.Number:
		node := strings.TrimSpace(typed.String())
		return node, node != ""
	case float64:
		n := int(typed)
		if typed != float64(n) {
			return "", false
		}
		return fmt.Sprintf("%d", n), true
	case int:
		return fmt.Sprintf("%d", typed), true
	case int64:
		return fmt.Sprintf("%d", typed), true
	default:
		return "", false
	}
}

func explicitGraphEdge(value any) ([2]string, bool) {
	if rawEdge, ok := value.([]any); ok {
		if len(rawEdge) < 2 {
			return [2]string{}, false
		}
		from, okFrom := explicitGraphNodeID(rawEdge[0])
		to, okTo := explicitGraphNodeID(rawEdge[1])
		if !okFrom || !okTo {
			return [2]string{}, false
		}
		return [2]string{from, to}, true
	}
	rawMap, ok := value.(map[string]any)
	if !ok {
		return [2]string{}, false
	}
	from, okFrom := explicitGraphNodeID(firstMapValue(rawMap, "from", "source"))
	to, okTo := explicitGraphNodeID(firstMapValue(rawMap, "to", "target"))
	if !okFrom || !okTo {
		return [2]string{}, false
	}
	return [2]string{from, to}, true
}

func shortestPathLength(graph explicitGraphInput) int {
	if graph.Start == graph.Goal {
		return 0
	}
	adj := make(map[string][]string, len(graph.Nodes))
	for _, edge := range graph.Edges {
		adj[edge[0]] = append(adj[edge[0]], edge[1])
		if !graph.Directed {
			adj[edge[1]] = append(adj[edge[1]], edge[0])
		}
	}
	dist := map[string]int{graph.Start: 0}
	queue := []string{graph.Start}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for _, next := range adj[node] {
			if _, seen := dist[next]; seen {
				continue
			}
			dist[next] = dist[node] + 1
			if next == graph.Goal {
				return dist[next]
			}
			queue = append(queue, next)
		}
	}
	return -1
}

func intSolutionFromAnswer(answer string) (int, bool) {
	raw := strings.TrimSpace(answer)
	if idx := strings.Index(raw, "="); idx >= 0 && strings.Contains(strings.ToLower(raw[:idx]), "solution") {
		raw = strings.TrimSpace(raw[idx+1:])
	}
	raw = strings.Trim(raw, "` \t\r\n")
	return parseLeadingInt(raw)
}

func parseLeadingInt(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	sign := 1
	pos := 0
	switch raw[0] {
	case '-':
		sign = -1
		pos = 1
	case '+':
		pos = 1
	}
	if pos >= len(raw) || raw[pos] < '0' || raw[pos] > '9' {
		return 0, false
	}
	value := 0
	for pos < len(raw) && raw[pos] >= '0' && raw[pos] <= '9' {
		value = value*10 + int(raw[pos]-'0')
		pos++
	}
	return sign * value, true
}

func stackMoveAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	base := HelperVerifierDiagnostic{Pass: false, FailedAtStep: -1, FailureKind: "stack_move"}
	if len(input) == 0 {
		return HelperVerifierDiagnostic{}, false
	}
	initial, okInitial := stackStateFromAny(input["initial_state"])
	goal, okGoal := stackStateFromAny(input["goal_state"])
	if !okInitial || !okGoal {
		return HelperVerifierDiagnostic{}, false
	}
	moves, okMoves := stackMovesFromAnswer(answer)
	if !okMoves {
		base.FailureKind = "parse"
		base.Score = 0
		base.Message = "candidate does not contain a parseable solution move list"
		base.RepairHint = "return answer exactly as solution = [[block, from_stack, to_stack], ...] with JSON-compatible integers"
		return base, true
	}
	state := cloneIntStacks(initial)
	for idx, move := range moves {
		block, from, to := move[0], move[1], move[2]
		stateBefore := cloneIntStacks(state)
		validPrefix := stackMovePrefixForDiagnostic(moves[:idx])
		base.Score = stackMoveVerifierStepScore(idx, len(moves))
		base.ValidPrefix = validPrefix
		base.Progress = map[string]any{
			"valid_prefix_moves": idx,
			"candidate_moves":    len(moves),
		}
		if from < 0 || from >= len(state) {
			base.FailureKind = "source_out_of_range"
			base.FailedAtStep = idx
			base.FailedAction = move[:]
			base.StateBefore = stateBefore
			base.Message = fmt.Sprintf("move %d source stack %d out of range", idx, from)
			base.RepairHint = "choose a source stack index that exists"
			return base, true
		}
		if to < 0 || to >= len(state) {
			base.FailureKind = "destination_out_of_range"
			base.FailedAtStep = idx
			base.FailedAction = move[:]
			base.StateBefore = stateBefore
			base.Message = fmt.Sprintf("move %d destination stack %d out of range", idx, to)
			base.RepairHint = "choose a destination stack index that exists"
			return base, true
		}
		if from == to {
			base.FailureKind = "same_stack"
			base.FailedAtStep = idx
			base.FailedAction = move[:]
			base.StateBefore = stateBefore
			base.Message = fmt.Sprintf("move %d moves block %d from stack %d to the same stack", idx, block, from)
			base.RepairHint = "remove no-op same-stack moves and construct only state-changing moves"
			return base, true
		}
		if len(state[from]) == 0 {
			base.FailureKind = "empty_source"
			base.FailedAtStep = idx
			base.FailedAction = move[:]
			base.StateBefore = stateBefore
			base.Message = fmt.Sprintf("move %d source stack %d is empty", idx, from)
			base.RepairHint = "only move from non-empty stacks"
			return base, true
		}
		top := state[from][len(state[from])-1]
		if top != block {
			base.FailureKind = "precondition_not_top"
			base.FailedAtStep = idx
			base.FailedAction = move[:]
			base.StateBefore = stateBefore
			base.Message = fmt.Sprintf("move %d tries to move block %d, but stack %d top is %d", idx, block, from, top)
			base.RepairHint = "clear the requested block before moving it, or move the current top block first"
			return base, true
		}
		if stackMoveRequiresStrictDescending(input) && len(state[to]) > 0 && block >= state[to][len(state[to])-1] {
			base.FailureKind = "destination_order_violation"
			base.FailedAtStep = idx
			base.FailedAction = move[:]
			base.StateBefore = stateBefore
			base.Message = fmt.Sprintf("move %d tries to place item %d on destination top %d, violating strict descending stack order", idx, block, state[to][len(state[to])-1])
			base.RepairHint = "only place an item on an empty stack or on a larger-index destination top"
			return base, true
		}
		state[from] = state[from][:len(state[from])-1]
		state[to] = append(state[to], block)
	}
	if !reflect.DeepEqual(state, goal) {
		base.FailureKind = "goal_mismatch"
		base.Score = stackMoveVerifierGoalMismatchScore(state, goal)
		base.ValidPrefix = stackMovePrefixForDiagnostic(moves)
		base.Progress = map[string]any{
			"valid_prefix_moves": len(moves),
			"candidate_moves":    len(moves),
			"state_similarity":   stackStateSimilarity(state, goal),
		}
		base.ObservedFinal = state
		base.ExpectedFinal = goal
		base.Message = fmt.Sprintf("final state %v does not match goal %v", state, goal)
		base.RepairHint = "continue constructing moves until every stack exactly matches the goal order"
		return base, true
	}
	return HelperVerifierDiagnostic{Pass: true, Score: 1, FailureKind: "stack_move", FailedAtStep: -1}, true
}

func stackMoveRequiresStrictDescending(input map[string]any) bool {
	order, _ := input["stack_order"].(string)
	return strings.TrimSpace(order) == "strict_descending_bottom_to_top"
}

func stackMovePrefixForDiagnostic(moves [][3]int) [][]int {
	if len(moves) == 0 {
		return nil
	}
	const maxPrefix = 120
	start := 0
	if len(moves) > maxPrefix {
		start = len(moves) - maxPrefix
	}
	out := make([][]int, 0, len(moves)-start)
	for _, move := range moves[start:] {
		out = append(out, []int{move[0], move[1], move[2]})
	}
	return out
}

func stackMoveVerifierStepScore(validPrefix, totalMoves int) float64 {
	if totalMoves <= 0 {
		return 0
	}
	score := float64(validPrefix) / float64(totalMoves)
	if score < 0 {
		return 0
	}
	if score > 0.8 {
		return 0.8
	}
	return score
}

func stackMoveVerifierGoalMismatchScore(observed, expected [][]int) float64 {
	return 0.8 + 0.19*stackStateSimilarity(observed, expected)
}

func stackStateSimilarity(observed, expected [][]int) float64 {
	total := 0
	matched := 0
	for i := 0; i < len(expected); i++ {
		total += len(expected[i])
		if i >= len(observed) {
			continue
		}
		limit := len(expected[i])
		if len(observed[i]) < limit {
			limit = len(observed[i])
		}
		for j := 0; j < limit; j++ {
			if observed[i][j] == expected[i][j] {
				matched++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(matched) / float64(total)
}

func stackStateFromAny(value any) ([][]int, bool) {
	rawStacks, ok := value.([]any)
	if !ok {
		return nil, false
	}
	stacks := make([][]int, len(rawStacks))
	for i, rawStack := range rawStacks {
		items, ok := rawStack.([]any)
		if !ok {
			return nil, false
		}
		stacks[i] = make([]int, len(items))
		for j, item := range items {
			n, ok := intFromJSONNumberLike(item)
			if !ok {
				return nil, false
			}
			stacks[i][j] = n
		}
	}
	return stacks, true
}

func stackMovesFromAnswer(answer string) ([][3]int, bool) {
	raw := strings.TrimSpace(answer)
	if idx := strings.Index(raw, "="); idx >= 0 && strings.Contains(strings.ToLower(raw[:idx]), "solution") {
		raw = strings.TrimSpace(raw[idx+1:])
	}
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end < start {
		return nil, false
	}
	raw = raw[start : end+1]
	var decoded []any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, false
	}
	moves := make([][3]int, 0, len(decoded))
	for _, item := range decoded {
		move, ok := stackMoveFromAny(item)
		if !ok {
			return nil, false
		}
		moves = append(moves, move)
	}
	return moves, true
}

func stackMoveFromAny(item any) ([3]int, bool) {
	rawMove, ok := item.([]any)
	if ok {
		if len(rawMove) != 3 {
			return [3]int{}, false
		}
		var move [3]int
		for idx, value := range rawMove {
			n, ok := intFromJSONNumberLike(value)
			if !ok {
				return [3]int{}, false
			}
			move[idx] = n
		}
		return move, true
	}
	rawMap, ok := item.(map[string]any)
	if !ok {
		return [3]int{}, false
	}
	block, okBlock := intFromJSONNumberLike(firstMapValue(rawMap, "block", "move", "item", "value"))
	from, okFrom := intFromJSONNumberLike(firstMapValue(rawMap, "from", "from_stack", "src", "source"))
	to, okTo := intFromJSONNumberLike(firstMapValue(rawMap, "to", "to_stack", "dst", "destination"))
	if !okBlock || !okFrom || !okTo {
		return [3]int{}, false
	}
	return [3]int{block, from, to}, true
}

func firstMapValue(value map[string]any, keys ...string) any {
	for _, key := range keys {
		if raw, ok := value[key]; ok {
			return raw
		}
	}
	return nil
}

func intFromJSONNumberLike(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		n := int(typed)
		return n, typed == float64(n)
	case int:
		return typed, true
	case json.Number:
		n, err := typed.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

func cloneIntStacks(in [][]int) [][]int {
	out := make([][]int, len(in))
	for idx := range in {
		out[idx] = append([]int(nil), in[idx]...)
	}
	return out
}

func prepareBraidRepair(
	phase REPLRunnerPhase,
	graph *BraidGraph,
	failedNode BraidNode,
	failedSummary string,
	summaries map[string]string,
	executed map[string]struct{},
	repairFeedbackByNode map[string]string,
	repairAttempts *int,
) bool {
	if graph == nil || phase.BraidRepairAttempts <= 0 || repairAttempts == nil || *repairAttempts >= phase.BraidRepairAttempts {
		return false
	}
	if !isBraidSolveKind(failedNode.Kind) && failedNode.Kind != "verify" && failedNode.Kind != "reduce" {
		return false
	}
	statuses := braidSummaryStatuses(failedSummary)
	if !braidStatusesContainAny(statuses, "blocked", "partial") || braidStatusesContainAny(statuses, "failed", "failure", "error") {
		return false
	}
	if braidFailureIsHelperContractFailure(failedSummary) {
		return false
	}
	solveIDs := braidRepairSolveNodeIDs(*graph, failedNode)
	if len(solveIDs) == 0 {
		return false
	}
	if braidFailureSignalsUnresolvedWork(failedSummary) {
		for _, solveID := range solveIDs {
			node, ok := braidGraphNodeByID(*graph, solveID)
			if !ok {
				continue
			}
			plan, ok := buildAdaptiveBraidSplitPlan(node, failedSummary)
			if !ok {
				continue
			}
			if applyAdaptiveBraidSplitPlan(graph, plan) {
				*repairAttempts++
				for nodeID := range braidNodeClosureFrom(*graph, plan.MergeNodeID) {
					delete(summaries, nodeID)
					delete(executed, nodeID)
					delete(repairFeedbackByNode, nodeID)
				}
				delete(summaries, solveID)
				delete(executed, solveID)
				delete(repairFeedbackByNode, solveID)
				return true
			}
		}
	}
	*repairAttempts++
	affected := map[string]struct{}{}
	for _, solveID := range solveIDs {
		for nodeID := range braidNodeClosureFrom(*graph, solveID) {
			affected[nodeID] = struct{}{}
		}
	}
	for nodeID := range affected {
		delete(summaries, nodeID)
		delete(executed, nodeID)
		delete(repairFeedbackByNode, nodeID)
	}
	for _, solveID := range solveIDs {
		repairFeedbackByNode[solveID] = capBraidRepairFeedback(buildBraidRepairFeedback(failedNode, failedSummary))
	}
	return true
}

func braidFailureIsHelperContractFailure(summary string) bool {
	lower := strings.ToLower(summary)
	if !strings.Contains(lower, "ephemeral_helper_solve") && !strings.Contains(lower, "helper factory") {
		return false
	}
	for _, marker := range []string{
		"decode draft json",
		"decode repair json",
		"no valid draft json object",
		"source is structurally incomplete",
		"python skill validation/run failed",
		"syntaxerror",
		"indentationerror",
		"source accepted by helper validator",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

type braidAdaptiveSplitPlan struct {
	ParentNodeID string
	RouterNodeID string
	MergeNodeID  string
	Reason       string
	Targets      []braidAdaptiveSplitTarget
}

type braidAdaptiveSplitTarget struct {
	ID             string
	Kind           string
	Question       string
	DependsOn      []string
	ExpectedOutput string
	InputSchema    map[string]any
}

const maxAdaptiveBraidSplitTargets = 12

func braidFailureSignalsUnresolvedWork(summary string) bool {
	lower := strings.ToLower(summary)
	upper := strings.ToUpper(summary)
	if strings.Contains(upper, "UNSOLVED") {
		return true
	}
	if strings.Contains(lower, "answer contains unresolved values") {
		return true
	}
	if strings.Contains(lower, "does not contain structured node values") {
		return true
	}
	if strings.Contains(lower, "failure_kind") && strings.Contains(lower, "search_backtrack") && strings.Contains(lower, "unresolved") {
		return true
	}
	if strings.Contains(lower, "helper output did not include a usable answer") && strings.Contains(lower, "candidate_frontier") {
		return true
	}
	return false
}

func buildAdaptiveBraidSplitPlan(node BraidNode, failedSummary string) (braidAdaptiveSplitPlan, bool) {
	if !isBraidSolveKind(node.Kind) {
		return braidAdaptiveSplitPlan{}, false
	}
	targets := adaptiveBraidSplitTargetIDs(node)
	cycleClusters := adaptiveBraidCycleClusters(node)
	requestedTargets := adaptiveBraidRequestedTargetIDs(node)
	if len(targets) < 2 && len(cycleClusters) == 0 {
		return braidAdaptiveSplitPlan{}, false
	}
	if len(targets) > maxAdaptiveBraidSplitTargets {
		targets = targets[:maxAdaptiveBraidSplitTargets]
	}
	mergeID := node.ID + "__adaptive_merge"
	routerID := node.ID + "__adaptive_router"
	plan := braidAdaptiveSplitPlan{
		ParentNodeID: node.ID,
		RouterNodeID: routerID,
		MergeNodeID:  mergeID,
		Reason:       safeTelemetryExcerpt(failedSummary, 300),
		Targets:      make([]braidAdaptiveSplitTarget, 0, len(targets)+len(cycleClusters)),
	}
	priorID := ""
	clusterMembers := map[string]bool{}
	for _, cluster := range cycleClusters {
		for _, id := range cluster {
			clusterMembers[id] = true
		}
	}
	targetIndex := 0
	for _, cluster := range cycleClusters {
		if len(cluster) < 2 {
			continue
		}
		if len(requestedTargets) > 0 && !braidNodeIDListsIntersect(cluster, requestedTargets) {
			continue
		}
		solveID := fmt.Sprintf("%s__adaptive_%02d_cycle_%s", node.ID, targetIndex, sanitizeToolCallIDPart(strings.Join(cluster, "_")))
		deps := []string{routerID}
		if priorID != "" {
			deps = append(deps, priorID)
		}
		input := cloneMapAny(node.InputSchema)
		if input == nil {
			input = map[string]any{}
		}
		input["target_nodes"] = stringSliceToAny(cluster)
		input["cycle_clusters"] = []any{stringSliceToAny(cluster)}
		input["adaptive_parent"] = node.ID
		input["router_node"] = routerID
		input["expected_output"] = fmt.Sprintf("solution = %s", braidNodeAnswerObjectTemplate(cluster))
		delete(input, "target_node")
		delete(input, "solve_targets")
		plan.Targets = append(plan.Targets, braidAdaptiveSplitTarget{
			ID:             solveID,
			Kind:           "cycle_solve",
			Question:       fmt.Sprintf("Solve mutually dependent target cluster %s for parent %s as one fixed-point or constraint-search problem. Use the router dependency packet and dependency summaries; return all cluster values and checks. Parent task: %s", strings.Join(cluster, ", "), node.ID, strings.TrimSpace(node.Question)),
			DependsOn:      deps,
			ExpectedOutput: fmt.Sprintf("solution = %s", braidNodeAnswerObjectTemplate(cluster)),
			InputSchema:    input,
		})
		priorID = solveID
		targetIndex++
	}
	for _, targetID := range targets {
		if clusterMembers[targetID] {
			continue
		}
		solveID := fmt.Sprintf("%s__adaptive_%02d_%s", node.ID, targetIndex, sanitizeToolCallIDPart(targetID))
		deps := []string{routerID}
		if priorID != "" {
			deps = append(deps, priorID)
		}
		input := cloneMapAny(node.InputSchema)
		if input == nil {
			input = map[string]any{}
		}
		input["target_node"] = targetID
		input["target_nodes"] = []any{targetID}
		input["adaptive_parent"] = node.ID
		input["router_node"] = routerID
		input["expected_output"] = fmt.Sprintf("solution = {\"%s\": <answer>}", targetID)
		delete(input, "solve_targets")
		plan.Targets = append(plan.Targets, braidAdaptiveSplitTarget{
			ID:             solveID,
			Kind:           node.Kind,
			Question:       fmt.Sprintf("Solve only %s for parent %s. Use the router dependency packet and dependency summaries as already solved inputs; return this target's candidate and checks. Parent task: %s", targetID, node.ID, strings.TrimSpace(node.Question)),
			DependsOn:      deps,
			ExpectedOutput: fmt.Sprintf("solution = <answer for %s>", targetID),
			InputSchema:    input,
		})
		priorID = solveID
		targetIndex++
	}
	if len(cycleClusters) > 0 {
		outsideRequested := make([]string, 0, len(requestedTargets))
		for _, targetID := range requestedTargets {
			if !clusterMembers[targetID] {
				outsideRequested = append(outsideRequested, targetID)
			}
		}
		if len(outsideRequested) > 0 {
			solveID := fmt.Sprintf("%s__adaptive_%02d_requested_outputs", node.ID, targetIndex)
			deps := []string{routerID}
			if priorID != "" {
				deps = append(deps, priorID)
			}
			input := cloneMapAny(node.InputSchema)
			if input == nil {
				input = map[string]any{}
			}
			input["target_nodes"] = stringSliceToAny(outsideRequested)
			input["adaptive_parent"] = node.ID
			input["router_node"] = routerID
			input["expected_output"] = strings.TrimSpace(node.ExpectedOutput)
			delete(input, "target_node")
			delete(input, "solve_targets")
			plan.Targets = append(plan.Targets, braidAdaptiveSplitTarget{
				ID:             solveID,
				Kind:           node.Kind,
				Question:       fmt.Sprintf("Solve requested outputs %s for parent %s using prior cycle-cluster results and the router dependency packet. Parent task: %s", strings.Join(outsideRequested, ", "), node.ID, strings.TrimSpace(node.Question)),
				DependsOn:      deps,
				ExpectedOutput: node.ExpectedOutput,
				InputSchema:    input,
			})
		}
	}
	return plan, len(plan.Targets) > 0
}

func adaptiveBraidSplitTargetIDs(node BraidNode) []string {
	input := node.InputSchema
	if len(input) == 0 {
		return extractBraidNodeIDsFromText(node.Question)
	}
	for _, key := range []string{"solve_targets", "nodes_to_solve", "intermediate_dependencies"} {
		if ids := extractBraidNodeIDsFromAny(input[key]); len(ids) > 1 {
			return ids
		}
		if ids := stringSliceFromAny(input[key]); len(ids) > 0 {
			return dedupeNonEmptyStrings(ids)
		}
	}
	if rawTarget, ok := input["target_node"]; ok {
		if id := strings.TrimSpace(fmt.Sprintf("%v", rawTarget)); id != "" {
			return []string{id}
		}
	}
	for _, key := range []string{"problems", "dependencies"} {
		if ids := extractBraidNodeIDsFromAny(input[key]); len(ids) > 1 {
			return ids
		}
	}
	return nil
}

func adaptiveBraidRequestedTargetIDs(node BraidNode) []string {
	input := node.InputSchema
	if len(input) > 0 {
		for _, key := range []string{"target_nodes", "requested_outputs"} {
			if ids := extractBraidNodeIDsFromAny(input[key]); len(ids) > 0 {
				return ids
			}
			if ids := stringSliceFromAny(input[key]); len(ids) > 0 {
				return dedupeNonEmptyStrings(ids)
			}
		}
	}
	if ids := extractBraidNodeIDsFromText(node.ExpectedOutput); len(ids) > 0 {
		return ids
	}
	return nil
}

func adaptiveBraidCycleClusters(node BraidNode) [][]string {
	input := node.InputSchema
	if len(input) == 0 {
		return nil
	}
	return extractBraidCycleClustersFromAny(input["cycle_clusters"])
}

func extractBraidCycleClustersFromAny(value any) [][]string {
	switch typed := value.(type) {
	case []any:
		out := make([][]string, 0, len(typed))
		for _, item := range typed {
			ids := extractBraidCycleClusterIDs(item)
			if len(ids) > 1 {
				out = append(out, ids)
			}
		}
		return out
	case []string:
		ids := dedupeNonEmptyStrings(typed)
		if len(ids) > 1 {
			return [][]string{ids}
		}
	case map[string]any:
		ids := extractBraidCycleClusterIDs(typed)
		if len(ids) > 1 {
			return [][]string{ids}
		}
	}
	return nil
}

func extractBraidCycleClusterIDs(value any) []string {
	switch typed := value.(type) {
	case []string:
		return dedupeNonEmptyStrings(typed)
	case []any:
		ids := make([]string, 0, len(typed))
		for _, item := range typed {
			if id := strings.TrimSpace(fmt.Sprintf("%v", item)); id != "" {
				ids = append(ids, id)
			}
		}
		return dedupeNonEmptyStrings(ids)
	case map[string]any:
		for _, key := range []string{"nodes", "target_nodes", "ids"} {
			if ids := extractBraidCycleClusterIDs(typed[key]); len(ids) > 1 {
				return ids
			}
		}
	}
	return nil
}

func braidNodeIDListsIntersect(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, id := range a {
		seen[id] = true
	}
	for _, id := range b {
		if seen[id] {
			return true
		}
	}
	return false
}

func stringSliceToAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func braidNodeAnswerObjectTemplate(ids []string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%q: <answer>", id))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

//nolint:unused // Kept for router/pre-split diagnostics while adaptive splitting is feature-gated.
func adaptiveBraidSplitDeclaredTargetIDs(node BraidNode) []string {
	input := node.InputSchema
	if len(input) == 0 {
		return nil
	}
	for _, key := range []string{"solve_targets", "nodes_to_solve"} {
		if ids := extractBraidNodeIDsFromAny(input[key]); len(ids) > 1 {
			return ids
		}
		if ids := stringSliceFromAny(input[key]); len(ids) > 1 {
			return dedupeNonEmptyStrings(ids)
		}
	}
	return nil
}

var braidNodeIDRangeRE = regexp.MustCompile(`\bnode_(\d+)\s*(?:to|-|through)\s*node_(\d+)\b`)
var braidNodeIDRE = regexp.MustCompile(`\bnode_\d+\b`)
var braidBareNodeListRE = regexp.MustCompile(`\bnodes?\s+((?:\d+\s*(?:,|and|through|to|-)?\s*){2,})`)
var braidBareNIDRE = regexp.MustCompile(`\bn(\d+)\b`)
var braidIntegerRE = regexp.MustCompile(`\d+`)

func extractBraidNodeIDsFromAny(value any) []string {
	switch typed := value.(type) {
	case string:
		return extractBraidNodeIDsFromText(typed)
	case []string:
		return extractBraidNodeIDsFromText(strings.Join(typed, "\n"))
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		return extractBraidNodeIDsFromText(strings.Join(parts, "\n"))
	default:
		if value == nil {
			return nil
		}
		return extractBraidNodeIDsFromText(fmt.Sprintf("%v", value))
	}
}

func extractBraidNodeIDsFromText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	out := []string{}
	for _, match := range braidNodeIDRangeRE.FindAllStringSubmatch(text, -1) {
		if len(match) != 3 {
			continue
		}
		start, errStart := strconv.Atoi(match[1])
		end, errEnd := strconv.Atoi(match[2])
		if errStart != nil || errEnd != nil {
			continue
		}
		if start <= end {
			for i := start; i <= end && len(out) < maxAdaptiveBraidSplitTargets; i++ {
				out = append(out, fmt.Sprintf("node_%d", i))
			}
		} else {
			for i := start; i >= end && len(out) < maxAdaptiveBraidSplitTargets; i-- {
				out = append(out, fmt.Sprintf("node_%d", i))
			}
		}
	}
	out = append(out, braidNodeIDRE.FindAllString(text, -1)...)
	for _, match := range braidBareNodeListRE.FindAllStringSubmatch(strings.ToLower(text), -1) {
		if len(match) != 2 {
			continue
		}
		for _, number := range braidIntegerRE.FindAllString(match[1], -1) {
			out = append(out, "node_"+number)
		}
	}
	for _, match := range braidBareNIDRE.FindAllStringSubmatch(strings.ToLower(text), -1) {
		if len(match) == 2 {
			out = append(out, "node_"+match[1])
		}
	}
	return dedupeNonEmptyStrings(out)
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		var decoded []string
		if strings.HasPrefix(trimmed, "[") && json.Unmarshal([]byte(trimmed), &decoded) == nil {
			return decoded
		}
		return []string{trimmed}
	default:
		return nil
	}
}

func dedupeNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func applyAdaptiveBraidSplitPlan(graph *BraidGraph, plan braidAdaptiveSplitPlan) bool {
	if graph == nil || strings.TrimSpace(plan.ParentNodeID) == "" || len(plan.Targets) == 0 {
		return false
	}
	parent, ok := braidGraphNodeByID(*graph, plan.ParentNodeID)
	if !ok {
		return false
	}
	newNodes := make([]BraidNode, 0, len(plan.Targets)+1)
	routerID := strings.TrimSpace(plan.RouterNodeID)
	if routerID == "" {
		routerID = plan.ParentNodeID + "__adaptive_router"
	}
	newNodes = append(newNodes, BraidNode{
		ID:              routerID,
		Kind:            "extract",
		Question:        fmt.Sprintf("Create a compact routing packet for parent %s. Include target ids, target-local inputs, dependency edges among targets, dependency edges to already solved parent dependencies, and the exact final output format. Do not solve target values.", plan.ParentNodeID),
		DependsOn:       append([]string(nil), parent.DependsOn...),
		ExpectedOutput:  "routing packet with target ids, dependencies, local inputs, and answer format; no final numeric answers",
		MaxSummaryChars: 2400,
		HelperPolicy:    BraidNodeHelperPolicyNever,
	})
	solveIDs := make([]string, 0, len(plan.Targets))
	for _, target := range plan.Targets {
		if strings.TrimSpace(target.ID) == "" {
			return false
		}
		kind := strings.TrimSpace(target.Kind)
		if kind == "" {
			kind = parent.Kind
		}
		solveIDs = append(solveIDs, target.ID)
		newNodes = append(newNodes, BraidNode{
			ID:              target.ID,
			Kind:            kind,
			Question:        target.Question,
			DependsOn:       append([]string(nil), target.DependsOn...),
			ExpectedOutput:  target.ExpectedOutput,
			MaxSummaryChars: parent.MaxSummaryChars,
			HelperPolicy:    parent.HelperPolicy,
			Archetype:       BraidScaffoldClassExplicitDAG,
			ScaffoldClass:   BraidScaffoldClassExplicitDAG,
			ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
			InputSchema:     target.InputSchema,
		})
	}
	mergeID := strings.TrimSpace(plan.MergeNodeID)
	if mergeID == "" {
		mergeID = plan.ParentNodeID + "__adaptive_merge"
	}
	newNodes = append(newNodes, BraidNode{
		ID:              mergeID,
		Kind:            "reduce",
		Question:        fmt.Sprintf("Merge adaptive split results for parent %s into the parent node's expected output.", plan.ParentNodeID),
		DependsOn:       solveIDs,
		ExpectedOutput:  parent.ExpectedOutput,
		MaxSummaryChars: parent.MaxSummaryChars,
		HelperPolicy:    BraidNodeHelperPolicyNever,
	})

	filtered := make([]BraidNode, 0, len(graph.Nodes)-1+len(newNodes))
	for _, node := range graph.Nodes {
		if node.ID == plan.ParentNodeID {
			continue
		}
		for i, depID := range node.DependsOn {
			if depID == plan.ParentNodeID {
				node.DependsOn[i] = mergeID
			}
		}
		filtered = append(filtered, node)
	}
	filtered = append(filtered, newNodes...)
	graph.Nodes = filtered
	if graph.FinalNode == plan.ParentNodeID {
		graph.FinalNode = mergeID
	}
	return true
}

func applyAdaptiveRouterSummaryDependencies(graph *BraidGraph, summaries map[string]string, executed map[string]struct{}) []string {
	if graph == nil || len(graph.Nodes) == 0 || len(summaries) == 0 {
		return nil
	}
	rewired := []string{}
	for _, router := range graph.Nodes {
		if !strings.HasSuffix(router.ID, "__adaptive_router") {
			continue
		}
		routerSummary := strings.TrimSpace(summaries[router.ID])
		if routerSummary == "" {
			continue
		}
		packet, ok := extractBraidRouterPacketFromSummary(routerSummary)
		if !ok || len(packet.DependencyEdges) == 0 {
			continue
		}
		parentPrefix := strings.TrimSuffix(router.ID, "__adaptive_router")
		targetNodeByTargetID := map[string]string{}
		templateNode := BraidNode{}
		for _, node := range graph.Nodes {
			if !strings.HasPrefix(node.ID, parentPrefix+"__adaptive_") || node.ID == router.ID || strings.HasSuffix(node.ID, "__adaptive_merge") {
				continue
			}
			if templateNode.ID == "" && isBraidSolveKind(node.Kind) {
				templateNode = node
			}
			targetID := braidAdaptiveNodeSingleTargetID(node)
			if targetID == "" {
				continue
			}
			targetNodeByTargetID[targetID] = node.ID
		}
		for _, targetID := range packet.TargetIDs {
			if len(targetNodeByTargetID) >= maxAdaptiveBraidSplitTargets {
				break
			}
			if _, exists := targetNodeByTargetID[targetID]; exists {
				continue
			}
			nodeID := fmt.Sprintf("%s__adaptive_extra_%02d_%s", parentPrefix, len(targetNodeByTargetID), sanitizeToolCallIDPart(targetID))
			input := cloneMapAny(templateNode.InputSchema)
			if input == nil {
				input = map[string]any{}
			}
			input["target_node"] = targetID
			input["target_nodes"] = []any{targetID}
			input["adaptive_parent"] = parentPrefix
			input["router_node"] = router.ID
			input["expected_output"] = fmt.Sprintf("solution = {\"%s\": <answer>}", targetID)
			delete(input, "solve_targets")
			newNode := BraidNode{
				ID:              nodeID,
				Kind:            firstNonEmptyString(templateNode.Kind, "solve"),
				Question:        fmt.Sprintf("Solve only %s for parent %s. Use the router dependency packet and dependency summaries as already solved inputs; return this target's candidate and checks.", targetID, parentPrefix),
				DependsOn:       []string{router.ID},
				ExpectedOutput:  fmt.Sprintf("solution = <answer for %s>", targetID),
				MaxSummaryChars: templateNode.MaxSummaryChars,
				HelperPolicy:    firstNonEmptyString(templateNode.HelperPolicy, BraidNodeHelperPolicyPreferred),
				Archetype:       BraidScaffoldClassExplicitDAG,
				ScaffoldClass:   BraidScaffoldClassExplicitDAG,
				ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
				InputSchema:     input,
			}
			graph.Nodes = append(graph.Nodes, newNode)
			targetNodeByTargetID[targetID] = nodeID
			rewired = append(rewired, nodeID)
			mergeID := parentPrefix + "__adaptive_merge"
			for i := range graph.Nodes {
				if graph.Nodes[i].ID == mergeID {
					graph.Nodes[i].DependsOn = dedupeNonEmptyStrings(append(graph.Nodes[i].DependsOn, nodeID))
					break
				}
			}
		}
		if len(targetNodeByTargetID) == 0 {
			continue
		}
		incoming := map[string][]string{}
		for _, edge := range packet.DependencyEdges {
			fromNodeID, fromOK := targetNodeByTargetID[edge.From]
			toNodeID, toOK := targetNodeByTargetID[edge.To]
			if !fromOK || !toOK || fromNodeID == toNodeID {
				continue
			}
			incoming[toNodeID] = append(incoming[toNodeID], fromNodeID)
		}
		if collapsed := collapseAdaptiveRouterCycles(graph, parentPrefix, router.ID, targetNodeByTargetID, incoming, executed, templateNode); len(collapsed) > 0 {
			rewired = append(rewired, collapsed...)
			continue
		}
		for i := range graph.Nodes {
			node := &graph.Nodes[i]
			targetID := braidAdaptiveNodeSingleTargetID(*node)
			if targetID == "" {
				continue
			}
			mappedNodeID, ok := targetNodeByTargetID[targetID]
			if !ok || mappedNodeID != node.ID {
				continue
			}
			if _, done := executed[node.ID]; done {
				continue
			}
			newDeps := dedupeNonEmptyStrings(append([]string{router.ID}, incoming[node.ID]...))
			if len(newDeps) == 0 || reflect.DeepEqual(node.DependsOn, newDeps) {
				continue
			}
			node.DependsOn = newDeps
			rewired = append(rewired, node.ID)
		}
	}
	return dedupeNonEmptyStrings(rewired)
}

func collapseAdaptiveRouterCycles(graph *BraidGraph, parentPrefix, routerID string, targetNodeByTargetID map[string]string, incoming map[string][]string, executed map[string]struct{}, templateNode BraidNode) []string {
	if graph == nil || len(targetNodeByTargetID) == 0 || len(incoming) == 0 {
		return nil
	}
	nodeToTarget := map[string]string{}
	for targetID, nodeID := range targetNodeByTargetID {
		nodeToTarget[nodeID] = targetID
	}
	graphEdges := map[string][]string{}
	for toNodeID, fromNodeIDs := range incoming {
		if _, ok := nodeToTarget[toNodeID]; !ok {
			continue
		}
		for _, fromNodeID := range fromNodeIDs {
			if _, ok := nodeToTarget[fromNodeID]; ok {
				graphEdges[fromNodeID] = append(graphEdges[fromNodeID], toNodeID)
			}
		}
	}
	components := braidStronglyConnectedComponents(graphEdges)
	rewired := []string{}
	for _, component := range components {
		if len(component) < 2 || len(component) > maxControllerCycleClusterSize {
			continue
		}
		skip := false
		for _, nodeID := range component {
			if _, done := executed[nodeID]; done {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		targets := make([]string, 0, len(component))
		member := map[string]struct{}{}
		for _, nodeID := range component {
			member[nodeID] = struct{}{}
			if targetID := nodeToTarget[nodeID]; targetID != "" {
				targets = append(targets, targetID)
			}
		}
		sort.Strings(targets)
		if len(targets) < 2 {
			continue
		}
		cycleID := fmt.Sprintf("%s__adaptive_cycle_%s", parentPrefix, sanitizeToolCallIDPart(strings.Join(targets, "_")))
		if _, exists := braidGraphNodeByID(*graph, cycleID); exists {
			continue
		}
		cycleDeps := []string{routerID}
		for _, nodeID := range component {
			for _, depID := range incoming[nodeID] {
				if _, isMember := member[depID]; isMember {
					continue
				}
				if depID != "" {
					cycleDeps = append(cycleDeps, depID)
				}
			}
		}
		input := cloneMapAny(templateNode.InputSchema)
		if input == nil {
			input = map[string]any{}
		}
		input["target_nodes"] = stringSliceToAny(targets)
		input["cycle_clusters"] = []any{stringSliceToAny(targets)}
		input["adaptive_parent"] = parentPrefix
		input["router_node"] = routerID
		input["expected_output"] = fmt.Sprintf("solution = %s", braidNodeAnswerObjectTemplate(targets))
		delete(input, "target_node")
		delete(input, "solve_targets")
		cycleNode := BraidNode{
			ID:              cycleID,
			Kind:            "cycle_solve",
			Question:        fmt.Sprintf("Solve mutually dependent adaptive target cluster %s for parent %s as one fixed-point or constraint-search problem. Use the router dependency packet and dependency summaries; return all cluster values and checks.", strings.Join(targets, ", "), parentPrefix),
			DependsOn:       dedupeNonEmptyStrings(cycleDeps),
			ExpectedOutput:  fmt.Sprintf("solution = %s", braidNodeAnswerObjectTemplate(targets)),
			MaxSummaryChars: firstPositiveInt(templateNode.MaxSummaryChars, minCycleSolveSummaryChars),
			HelperPolicy:    firstNonEmptyString(templateNode.HelperPolicy, BraidNodeHelperPolicyPreferred),
			Archetype:       BraidScaffoldClassExplicitDAG,
			ScaffoldClass:   BraidScaffoldClassExplicitDAG,
			ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
			InputSchema:     input,
		}
		filtered := make([]BraidNode, 0, len(graph.Nodes)+1-len(component))
		for _, node := range graph.Nodes {
			if _, isMember := member[node.ID]; isMember {
				continue
			}
			for i, depID := range node.DependsOn {
				if _, isMember := member[depID]; isMember {
					node.DependsOn[i] = cycleID
				}
			}
			if _, isAdaptiveTarget := nodeToTarget[node.ID]; isAdaptiveTarget {
				derivedDeps := []string{routerID}
				for _, depID := range incoming[node.ID] {
					if _, isMember := member[depID]; isMember {
						derivedDeps = append(derivedDeps, cycleID)
					} else if depID != "" {
						derivedDeps = append(derivedDeps, depID)
					}
				}
				node.DependsOn = derivedDeps
			}
			node.DependsOn = dedupeNonEmptyStrings(node.DependsOn)
			filtered = append(filtered, node)
		}
		filtered = append(filtered, cycleNode)
		graph.Nodes = filtered
		rewired = append(rewired, cycleID)
	}
	return dedupeNonEmptyStrings(rewired)
}

func braidStronglyConnectedComponents(edges map[string][]string) [][]string {
	nodes := map[string]struct{}{}
	for from, tos := range edges {
		nodes[from] = struct{}{}
		for _, to := range tos {
			nodes[to] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(nodes))
	for node := range nodes {
		ordered = append(ordered, node)
	}
	sort.Strings(ordered)
	index := 0
	stack := []string{}
	onStack := map[string]bool{}
	indices := map[string]int{}
	lowlink := map[string]int{}
	out := [][]string{}
	var visit func(string)
	visit = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			if _, ok := indices[w]; !ok {
				visit(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] && indices[w] < lowlink[v] {
				lowlink[v] = indices[w]
			}
		}
		if lowlink[v] != indices[v] {
			return
		}
		component := []string{}
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			component = append(component, w)
			if w == v {
				break
			}
		}
		sort.Strings(component)
		out = append(out, component)
	}
	for _, node := range ordered {
		if _, ok := indices[node]; !ok {
			visit(node)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], ",") < strings.Join(out[j], ",")
	})
	return out
}

type braidDependencyEdge struct {
	From string
	To   string
}

type braidRouterPacket struct {
	TargetIDs       []string
	DependencyEdges []braidDependencyEdge
}

func extractBraidRouterPacketFromSummary(summary string) (braidRouterPacket, bool) {
	value, ok := extractFirstJSONObjectFromText(summary)
	if !ok {
		return braidRouterPacket{}, false
	}
	packet := braidRouterPacket{
		TargetIDs:       extractBraidNodeIDsFromAny(firstMapValue(value, "target_ids", "targets", "target_nodes")),
		DependencyEdges: extractBraidDependencyEdgesFromAny(firstMapValue(value, "dependency_edges", "edges", "dependencies")),
	}
	if len(packet.TargetIDs) == 0 {
		packet.TargetIDs = extractBraidNodeIDsFromAny(firstMapValue(value, "nodes", "work_items"))
	}
	packet.TargetIDs = dedupeNonEmptyStrings(packet.TargetIDs)
	packet.DependencyEdges = dedupeBraidDependencyEdges(packet.DependencyEdges)
	if len(packet.TargetIDs) == 0 && len(packet.DependencyEdges) == 0 {
		return braidRouterPacket{}, false
	}
	return packet, true
}

func extractFirstJSONObjectFromText(text string) (map[string]any, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	start := strings.Index(text, "{")
	if start < 0 {
		return nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(text[start:]))
	decoder.UseNumber()
	var decoded map[string]any
	if err := decoder.Decode(&decoded); err != nil || len(decoded) == 0 {
		return nil, false
	}
	return decoded, true
}

func extractBraidDependencyEdgesFromAny(value any) []braidDependencyEdge {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 2 {
			to := normalizeBraidNodeRef(stringFromAny(typed[0]))
			from := normalizeBraidNodeRef(stringFromAny(typed[1]))
			if from != "" && to != "" && from != to {
				return []braidDependencyEdge{{From: from, To: to}}
			}
		}
		out := make([]braidDependencyEdge, 0, len(typed))
		for _, item := range typed {
			out = append(out, extractBraidDependencyEdgesFromAny(item)...)
		}
		return out
	case []string:
		if len(typed) == 2 {
			to := normalizeBraidNodeRef(typed[0])
			from := normalizeBraidNodeRef(typed[1])
			if from != "" && to != "" && from != to {
				return []braidDependencyEdge{{From: from, To: to}}
			}
		}
		out := make([]braidDependencyEdge, 0, len(typed))
		for _, item := range typed {
			out = append(out, extractBraidDependencyEdgesFromAny(item)...)
		}
		return out
	case map[string]any:
		if from := normalizeBraidNodeRef(stringFromAny(firstMapValue(typed, "from", "source", "dependency", "dep"))); from != "" {
			if to := normalizeBraidNodeRef(stringFromAny(firstMapValue(typed, "to", "target", "dependent", "node"))); to != "" {
				return []braidDependencyEdge{{From: from, To: to}}
			}
		}
		out := []braidDependencyEdge{}
		for to, rawDeps := range typed {
			toID := normalizeBraidNodeRef(to)
			if toID == "" {
				continue
			}
			for _, fromID := range extractBraidNodeIDsFromAny(rawDeps) {
				fromID = normalizeBraidNodeRef(fromID)
				if fromID != "" && fromID != toID {
					out = append(out, braidDependencyEdge{From: fromID, To: toID})
				}
			}
		}
		return out
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var decoded any
			if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
				return extractBraidDependencyEdgesFromAny(decoded)
			}
		}
		return nil
	default:
		return nil
	}
}

func dedupeBraidDependencyEdges(edges []braidDependencyEdge) []braidDependencyEdge {
	seen := map[string]struct{}{}
	out := make([]braidDependencyEdge, 0, len(edges))
	for _, edge := range edges {
		from := normalizeBraidNodeRef(edge.From)
		to := normalizeBraidNodeRef(edge.To)
		if from == "" || to == "" || from == to {
			continue
		}
		key := from + "->" + to
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, braidDependencyEdge{From: from, To: to})
	}
	return out
}

func normalizeBraidNodeRef(ref string) string {
	ref = strings.TrimSpace(strings.ToLower(ref))
	if strings.HasPrefix(ref, "node_") {
		if number := strings.TrimPrefix(ref, "node_"); number != "" {
			if _, err := strconv.Atoi(number); err != nil {
				return ""
			}
			return "node_" + number
		}
		return ""
	}
	if strings.HasPrefix(ref, "n") {
		number := strings.TrimPrefix(ref, "n")
		if number == "" {
			return ""
		}
		if _, err := strconv.Atoi(number); err != nil {
			return ""
		}
		return "node_" + number
	}
	return ""
}

func braidAdaptiveNodeSingleTargetID(node BraidNode) string {
	if len(node.InputSchema) == 0 {
		return ""
	}
	if raw := strings.TrimSpace(fmt.Sprintf("%v", node.InputSchema["target_node"])); raw != "" && raw != "<nil>" {
		return raw
	}
	ids := extractBraidNodeIDsFromAny(node.InputSchema["target_nodes"])
	if len(ids) == 1 {
		return ids[0]
	}
	return ""
}

func braidGraphNodeByID(graph BraidGraph, nodeID string) (BraidNode, bool) {
	for _, node := range graph.Nodes {
		if node.ID == nodeID {
			return node, true
		}
	}
	return BraidNode{}, false
}

// extractBraidHelperFailedStage attempts to identify the failure stage from a
// helper output summary. Returns "run" as the default when no stage marker is
// found.
func extractBraidHelperFailedStage(summary string) braidHelperStage {
	lower := strings.ToLower(summary)
	for _, stage := range []braidHelperStage{
		braidHelperStageDraft,
		braidHelperStageParse,
		braidHelperStageValidate,
		braidHelperStageRun,
		braidHelperStageVerify,
	} {
		if strings.Contains(lower, "stage="+string(stage)) {
			return stage
		}
		if strings.Contains(lower, string(stage)+" error") ||
			strings.Contains(lower, string(stage)+" failed") ||
			strings.Contains(lower, string(stage)+" timed out") {
			return stage
		}
	}
	if strings.Contains(lower, "syntax") || strings.Contains(lower, "parse") {
		return braidHelperStageParse
	}
	if strings.Contains(lower, "verify") || strings.Contains(lower, "verifier") {
		return braidHelperStageVerify
	}
	if strings.Contains(lower, "compil") || strings.Contains(lower, "validation") {
		return braidHelperStageValidate
	}
	return braidHelperStageRun
}

func buildBraidRepairFeedback(failedNode BraidNode, failedSummary string) string {
	var b strings.Builder
	switch failedNode.Kind {
	case "solve", "cycle_solve":
		b.WriteString("Previous solve-like node returned blocked. Revise this node and produce candidate values or a concrete mathematical check before returning.\n")
		b.WriteString("Do not treat circular-looking or mutual mutual dependencies as runtime blockers. Treat them as simultaneous constraints, fixed-point equations, or a small candidate search.\n")
	default:
		fmt.Fprintf(&b, "Previous %s node found a concrete failed constraint. Revise this solve node's candidate values to address the failure before returning.\n", failedNode.Kind)
	}
	b.WriteString("Failed summary:\n")
	b.WriteString(safeTelemetryExcerpt(failedSummary, 900))
	return b.String()
}

func braidRepairSolveNodeIDs(graph BraidGraph, failedNode BraidNode) []string {
	byID := make(map[string]BraidNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		byID[node.ID] = node
	}
	seen := map[string]struct{}{}
	var out []string
	var visit func(string)
	visit = func(nodeID string) {
		node, ok := byID[nodeID]
		if !ok {
			return
		}
		if isBraidSolveKind(node.Kind) {
			if _, exists := seen[node.ID]; !exists {
				seen[node.ID] = struct{}{}
				out = append(out, node.ID)
			}
			return
		}
		for _, depID := range node.DependsOn {
			visit(depID)
		}
	}
	visit(failedNode.ID)
	return out
}

func braidNodeClosureFrom(graph BraidGraph, startID string) map[string]struct{} {
	out := map[string]struct{}{startID: {}}
	changed := true
	for changed {
		changed = false
		for _, node := range graph.Nodes {
			if _, exists := out[node.ID]; exists {
				continue
			}
			for _, depID := range node.DependsOn {
				if _, dependsOnAffected := out[depID]; dependsOnAffected {
					out[node.ID] = struct{}{}
					changed = true
					break
				}
			}
		}
	}
	return out
}

var (
	braidSummaryStatusRE = regexp.MustCompile(`(?i)(?:^|\s)status\s*:\s*([a-z][a-z0-9_-]*)`)
	braidPassTrueRE      = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])"?pass"?\s*[:=]\s*true(?:[^a-z0-9_]|$)`)
	braidPassFalseRE     = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])"?pass"?\s*[:=]\s*false(?:[^a-z0-9_]|$)`)
)

type braidNodeArtifact struct {
	Status          string           `json:"status"`
	Answer          any              `json:"answer,omitempty"`
	Checks          []any            `json:"checks,omitempty"`
	Counterexamples []map[string]any `json:"counterexamples,omitempty"`
	Confidence      float64          `json:"confidence,omitempty"`
	Pass            *bool            `json:"pass,omitempty"`
	Provenance      map[string]any   `json:"provenance,omitempty"`
}

func parseBraidNodeArtifact(summary string) (braidNodeArtifact, bool) {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return braidNodeArtifact{}, false
	}
	var artifact braidNodeArtifact
	if err := json.Unmarshal([]byte(trimmed), &artifact); err != nil {
		return braidNodeArtifact{}, false
	}
	if strings.TrimSpace(artifact.Status) == "" {
		return braidNodeArtifact{}, false
	}
	artifact.Status = strings.ToLower(strings.TrimSpace(artifact.Status))
	return artifact, true
}

func braidNodeArtifactAnswerString(artifact braidNodeArtifact) string {
	switch typed := artifact.Answer.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err == nil {
			return strings.TrimSpace(string(encoded))
		}
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func braidNodeArtifactChecksText(artifact braidNodeArtifact) string {
	if len(artifact.Checks) == 0 {
		return ""
	}
	parts := make([]string, 0, len(artifact.Checks))
	for _, check := range artifact.Checks {
		switch typed := check.(type) {
		case string:
			parts = append(parts, typed)
		default:
			parts = append(parts, fmt.Sprint(typed))
		}
	}
	return strings.Join(parts, "\n")
}

func validateBraidNodeExecutionSummary(phaseName string, node BraidNode, summary string, finalNodeID string) error {
	return validateBraidNodeExecutionSummaryInGraph(phaseName, node, summary, finalNodeID, nil)
}

func validateBraidNodeExecutionSummaryInGraph(phaseName string, node BraidNode, summary string, finalNodeID string, graph *BraidGraph) error {
	return validateBraidNodeExecutionSummaryWithCertificationInGraph(phaseName, node, summary, nil, finalNodeID, graph)
}

func validateBraidNodeExecutionRecordInGraph(phaseName string, node BraidNode, record braidNodeExecutionRecord, finalNodeID string, graph *BraidGraph) error {
	return validateBraidNodeExecutionSummaryWithCertificationInGraph(phaseName, node, record.Summary, record.Certification, finalNodeID, graph)
}

func validateBraidNodeExecutionSummaryWithCertificationInGraph(phaseName string, node BraidNode, summary string, cert *RuntimeCertification, finalNodeID string, graph *BraidGraph) error {
	statuses := braidSummaryStatuses(summary)
	if len(statuses) == 0 {
		return fmt.Errorf("rlm repl runner phase %q: braid node %q returned no status field", phaseName, node.ID)
	}
	if braidStatusesContainAny(statuses, "failed", "failure", "blocked", "error") {
		return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) did not complete: %s", phaseName, node.ID, node.Kind, safeTelemetryExcerpt(summary, 300))
	}
	if braidStatusesContainAny(statuses, "partial") {
		if braidNodePartialCanFeedDownstream(node, finalNodeID, graph) {
			return nil
		}
		return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) returned partial status: %s", phaseName, node.ID, node.Kind, safeTelemetryExcerpt(summary, 300))
	}
	if node.Kind == "cycle_solve" {
		if braidPassFalseRE.MatchString(summary) {
			return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) reported pass=false with solved status: %s", phaseName, node.ID, node.Kind, safeTelemetryExcerpt(summary, 300))
		}
		if braidStatusesContainAny(statuses, "solved", "ok", "completed", "pass", "passed") {
			if err := validateCycleSolveSummaryJSON(summary); err != nil {
				return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) invalid cycle_json: %w", phaseName, node.ID, node.Kind, err)
			}
		}
	}
	solutionLineSource := summary
	if artifact, ok := parseBraidNodeArtifact(summary); ok {
		solutionLineSource = braidNodeArtifactAnswerString(artifact)
	}
	if isBraidSolveKind(node.Kind) && braidSolveNodeRequiresTargetSolutionLine(node) && !strings.Contains(strings.ToLower(solutionLineSource), "solution =") {
		return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) did not provide a target solution line: %s", phaseName, node.ID, node.Kind, safeTelemetryExcerpt(summary, 300))
	}
	if node.ID == finalNodeID || node.Kind == "extract" || node.Kind == "verify" || node.Kind == "reduce" {
		if !braidStatusesContainAny(statuses, "solved", "ok", "completed", "pass", "passed") {
			return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) returned unsupported status %q", phaseName, node.ID, node.Kind, strings.Join(statuses, ","))
		}
	}
	if node.Kind == "verify" {
		if !braidVerificationSummaryPassed(summary) {
			return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) did not report verification pass: %s", phaseName, node.ID, node.Kind, safeTelemetryExcerpt(summary, 300))
		}
		// A text-only pass is accepted as an advisory node completion so legacy
		// graph execution can continue, but it does not create RuntimeCertification
		// and therefore cannot drive runtime shortcuts or verified final handoff.
		_ = cert
	}
	return nil
}

func braidSolveNodeRequiresTargetSolutionLine(node BraidNode) bool {
	if !isBraidSolveKind(node.Kind) {
		return false
	}
	return strings.Contains(node.ID, "__adaptive_") && !strings.HasSuffix(node.ID, "__adaptive_merge")
}

func normalizeBraidAdaptiveTargetSummary(node BraidNode, summary string) (string, bool) {
	if !braidSolveNodeRequiresTargetSolutionLine(node) || strings.Contains(strings.ToLower(summary), "solution =") {
		return "", false
	}
	target := braidNodeTargetID(node)
	if target == "" {
		return "", false
	}
	answer, ok := braidConcreteAnswerFromSummary(summary)
	if !ok {
		return "", false
	}
	payload := map[string]any{target: parseBraidScalarAnswer(answer)}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return "status: completed summary: status: solved answer: solution = " + string(body) + " checks: normalized adaptive target answer from child summary.", true
}

func braidConcreteAnswerFromSummary(summary string) (string, bool) {
	re := regexp.MustCompile(`(?is)\banswer\s*:\s*(.+?)(?:\s+checks\s*:|$)`)
	match := re.FindStringSubmatch(summary)
	if len(match) < 2 {
		return "", false
	}
	answer := strings.TrimSpace(match[1])
	answer = strings.Trim(answer, "`")
	if answer == "" {
		return "", false
	}
	lower := strings.ToLower(answer)
	blockers := []string{
		"missing",
		"cannot solve",
		"can't solve",
		"blocked",
		"unknown",
		"not available",
		"insufficient",
		"unable to",
		"need ",
		"needs ",
		"requires ",
		"placeholder",
	}
	for _, blocker := range blockers {
		if strings.Contains(lower, blocker) {
			return "", false
		}
	}
	if len([]rune(answer)) > 1200 {
		return "", false
	}
	return answer, true
}

func parseBraidScalarAnswer(answer string) any {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded
	}
	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return f
	}
	return trimmed
}

func braidVerificationSummaryPassed(summary string) bool {
	if artifact, ok := parseBraidNodeArtifact(summary); ok {
		if artifact.Pass != nil && *artifact.Pass {
			return true
		}
		switch artifact.Status {
		case "pass", "passed", "ok":
			return true
		}
		checks := strings.ToLower(braidNodeArtifactChecksText(artifact))
		for _, marker := range []string{
			"verification: pass",
			"verification pass",
			"verified: pass",
			"verdict: pass",
			"checks: verified",
			"verified",
			"all moves valid",
			"final state matches",
			"matches the goal state",
		} {
			if strings.Contains(checks, marker) {
				return true
			}
		}
		return false
	}
	statuses := braidSummaryStatuses(summary)
	if braidStatusesContainAny(statuses, "pass", "passed") {
		return true
	}
	if braidPassTrueRE.MatchString(summary) {
		return true
	}
	lower := strings.ToLower(summary)
	for _, marker := range []string{
		"verification: pass",
		"verification pass",
		"verified: pass",
		"verdict: pass",
		"checks: verified",
		"all moves valid",
		"final state matches",
		"matches the goal state",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func braidNodePartialCanFeedDownstream(node BraidNode, finalNodeID string, graph *BraidGraph) bool {
	if graph == nil || node.ID == finalNodeID {
		return false
	}
	if node.Kind != "solve" && node.Kind != "extract" {
		return false
	}
	for _, candidate := range graph.Nodes {
		if candidate.ID == node.ID || candidate.Kind == "extract" {
			continue
		}
		if braidNodeDependsOn(*graph, candidate.ID, node.ID) {
			return true
		}
	}
	return false
}

//nolint:unused // Kept for generated split graph cleanup variants.
func isGeneratedBraidSplitParseNode(node BraidNode) bool {
	return node.Kind == "extract" && strings.HasSuffix(node.ID, "__parse")
}

func braidNodeDependsOn(graph BraidGraph, nodeID string, dependencyID string) bool {
	seen := map[string]struct{}{}
	var visit func(string) bool
	visit = func(id string) bool {
		if id == dependencyID {
			return true
		}
		if _, ok := seen[id]; ok {
			return false
		}
		seen[id] = struct{}{}
		for _, node := range graph.Nodes {
			if node.ID != id {
				continue
			}
			for _, dep := range node.DependsOn {
				if visit(dep) {
					return true
				}
			}
			return false
		}
		return false
	}
	return visit(nodeID)
}

func validateCycleSolveSummaryJSON(summary string) error {
	raw, err := extractCycleSolveJSON(summary)
	if err != nil {
		return err
	}
	var payload struct {
		Pass       *bool            `json:"pass"`
		Candidates map[string]any   `json:"candidates"`
		Checks     []map[string]any `json:"checks"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if payload.Pass == nil {
		return fmt.Errorf("pass is required")
	}
	if !*payload.Pass {
		return fmt.Errorf("pass must be true for solved cycle_solve")
	}
	if len(payload.Candidates) == 0 {
		return fmt.Errorf("candidates is required")
	}
	if len(payload.Checks) == 0 {
		return fmt.Errorf("checks is required")
	}
	for idx, check := range payload.Checks {
		if value, ok := check["ok"].(bool); !ok || !value {
			return fmt.Errorf("checks[%d].ok must be true", idx)
		}
		observed, hasObserved := check["observed"]
		expected, hasExpected := check["expected"]
		if !hasObserved || !hasExpected {
			return fmt.Errorf("checks[%d] must include observed and expected", idx)
		}
		if !cycleJSONValuesEqual(observed, expected) {
			return fmt.Errorf("checks[%d] observed=%v does not match expected=%v", idx, observed, expected)
		}
	}
	return nil
}

func extractCycleSolveJSON(summary string) (string, error) {
	if artifact, ok := parseBraidNodeArtifact(summary); ok {
		summary = braidNodeArtifactAnswerString(artifact)
	}
	const marker = "cycle_json"
	lower := strings.ToLower(summary)
	markerIdx := strings.Index(lower, marker)
	if markerIdx < 0 {
		return "", fmt.Errorf("missing cycle_json object")
	}
	afterMarker := summary[markerIdx+len(marker):]
	colonIdx := strings.Index(afterMarker, ":")
	if colonIdx < 0 {
		return "", fmt.Errorf("missing cycle_json object")
	}
	rest := afterMarker[colonIdx+1:]
	startRel := strings.Index(rest, "{")
	if startRel < 0 {
		return "", fmt.Errorf("missing cycle_json object")
	}
	start := markerIdx + len(marker) + colonIdx + 1 + startRel
	depth := 0
	inString := false
	escaped := false
	for idx := start; idx < len(summary); idx++ {
		ch := summary[idx]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return summary[start : idx+1], nil
			}
		}
	}
	return "", fmt.Errorf("unterminated cycle_json object")
}

func cycleJSONValuesEqual(observed any, expected any) bool {
	if reflect.DeepEqual(observed, expected) {
		return true
	}
	observedFloat, observedOK := observed.(float64)
	expectedFloat, expectedOK := expected.(float64)
	if observedOK && expectedOK {
		return observedFloat == expectedFloat
	}
	return strings.TrimSpace(fmt.Sprint(observed)) == strings.TrimSpace(fmt.Sprint(expected))
}

func braidSummaryStatuses(summary string) []string {
	if artifact, ok := parseBraidNodeArtifact(summary); ok {
		statuses := []string{artifact.Status}
		if artifact.Pass != nil && *artifact.Pass {
			statuses = append(statuses, "pass")
		}
		return statuses
	}
	matches := braidSummaryStatusRE.FindAllStringSubmatch(summary, -1)
	statuses := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		status := strings.ToLower(strings.Trim(match[1], " \t\r\n,.;:"))
		if status != "" {
			statuses = append(statuses, status)
		}
	}
	return statuses
}

func braidStatusesContainAny(statuses []string, targets ...string) bool {
	targetSet := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		targetSet[strings.ToLower(strings.TrimSpace(target))] = struct{}{}
	}
	for _, status := range statuses {
		if _, ok := targetSet[status]; ok {
			return true
		}
	}
	return false
}

func recordBraidNodeEvent(toolExec *replToolExecutor, phaseName string, wave int, node BraidNode, status string, message string) {
	if toolExec == nil || toolExec.recorder == nil {
		return
	}
	toolExec.recorder.RecordBraidEvent(BraidEvent{
		Phase:   phaseName,
		Status:  strings.TrimSpace(status),
		Wave:    wave,
		NodeID:  node.ID,
		Kind:    node.Kind,
		Message: safeTelemetryExcerpt(message, 500),
	})
}

func readyBraidNodes(graph BraidGraph, executed map[string]struct{}, summaries map[string]string) []BraidNode {
	out := make([]BraidNode, 0)
	for _, node := range graph.Nodes {
		if _, ok := executed[node.ID]; ok {
			continue
		}
		ready := true
		for _, depID := range node.DependsOn {
			if _, ok := summaries[depID]; !ok {
				ready = false
				break
			}
		}
		if ready {
			out = append(out, node)
		}
	}
	return out
}

func dependencySummarySubset(node BraidNode, summaries map[string]string) map[string]string {
	if len(node.DependsOn) == 0 || len(summaries) == 0 {
		return nil
	}
	out := make(map[string]string, len(node.DependsOn))
	for _, depID := range node.DependsOn {
		if summary := strings.TrimSpace(summaries[depID]); summary != "" {
			out[depID] = summary
		}
	}
	return out
}

func braidRuntimeMergeBlockReason(node BraidNode, dependencySummaries map[string]string) string {
	if strings.TrimSpace(node.Kind) != "reduce" || len(node.InputSchema) == 0 {
		return ""
	}
	block, _ := node.InputSchema["block_on_missing_artifact"].(bool)
	if !block {
		return ""
	}
	solveIDs := stringSliceFromAny(node.InputSchema["solve_ids"])
	if len(solveIDs) == 0 {
		solveIDs = append([]string(nil), node.DependsOn...)
	}
	allowed := map[string]struct{}{"solved": {}, "pass": {}, "passed": {}, "ok": {}, "completed": {}}
	if required := stringSliceFromAny(node.InputSchema["required_artifact_status"]); len(required) > 0 {
		allowed = map[string]struct{}{}
		for _, status := range required {
			allowed[strings.ToLower(strings.TrimSpace(status))] = struct{}{}
		}
	}
	for _, depID := range solveIDs {
		summary := strings.TrimSpace(dependencySummaries[depID])
		if summary == "" {
			return fmt.Sprintf("missing required split artifact %q", depID)
		}
		statuses := braidSummaryStatuses(summary)
		if braidStatusesContainAny(statuses, "blocked", "failed", "failure", "error", "partial") {
			return fmt.Sprintf("required split artifact %q is not solved/pass: %s", depID, strings.Join(statuses, ","))
		}
		ok := false
		for _, status := range statuses {
			if _, exists := allowed[strings.ToLower(strings.TrimSpace(status))]; exists {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Sprintf("required split artifact %q has unsupported status: %s", depID, strings.Join(statuses, ","))
		}
	}
	return ""
}

func recordBraidNodeExecution(records map[string]braidNodeExecutionRecord, nodeID, summary, source string, cert *RuntimeCertification) {
	if records == nil || strings.TrimSpace(nodeID) == "" {
		return
	}
	if cert != nil {
		copy := *cert
		copy.NodeID = strings.TrimSpace(nodeID)
		cert = &copy
	}
	record := braidNodeExecutionRecord{
		Summary:       strings.TrimSpace(summary),
		Source:        strings.TrimSpace(source),
		Certification: cert,
	}
	if artifact, ok := parseBraidNodeArtifact(summary); ok {
		record.Artifact = artifact
	}
	records[nodeID] = record
}

func runtimeCertificationForNode(node BraidNode, verifierID string) *RuntimeCertification {
	verifierID = strings.TrimSpace(verifierID)
	if verifierID == "" {
		return nil
	}
	return &RuntimeCertification{
		NodeID:        strings.TrimSpace(node.ID),
		Pass:          true,
		VerifierID:    verifierID,
		VerifierKind:  runtimeVerifierKindScaffold,
		ScaffoldClass: strings.TrimSpace(node.ScaffoldClass),
		ScaffoldID:    strings.TrimSpace(node.ScaffoldID),
	}
}

func certificationFromBraidSummary(node BraidNode, summary string) *RuntimeCertification {
	artifact, ok := parseBraidNodeArtifact(summary)
	if !ok || artifact.Provenance == nil {
		return nil
	}
	raw, ok := artifact.Provenance["runtime_certification"]
	if !ok {
		return nil
	}
	certMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	pass, _ := certMap["pass"].(bool)
	verifierID := strings.TrimSpace(stringFromAny(certMap["verifier_id"]))
	verifierKind := strings.TrimSpace(stringFromAny(certMap["verifier_kind"]))
	if !pass || verifierID == "" || verifierKind != runtimeVerifierKindScaffold {
		return nil
	}
	scaffoldClass := strings.TrimSpace(stringFromAny(certMap["scaffold_class"]))
	if scaffoldClass == "" {
		scaffoldClass = strings.TrimSpace(node.ScaffoldClass)
	}
	scaffoldID := strings.TrimSpace(stringFromAny(certMap["scaffold_id"]))
	if scaffoldID == "" {
		scaffoldID = strings.TrimSpace(node.ScaffoldID)
	}
	cert := &RuntimeCertification{
		NodeID:          strings.TrimSpace(node.ID),
		Pass:            true,
		VerifierID:      verifierID,
		VerifierKind:    verifierKind,
		ScaffoldClass:   scaffoldClass,
		ScaffoldID:      scaffoldID,
		CandidateDigest: strings.TrimSpace(stringFromAny(certMap["candidate_digest"])),
		InputDigest:     strings.TrimSpace(stringFromAny(certMap["input_digest"])),
	}
	return cert
}

func submitBraidNode(
	ctx context.Context,
	phaseName string,
	node BraidNode,
	rootPrompt string,
	dependencySummaries map[string]string,
	repairFeedback string,
	toolExec *replToolExecutor,
	output *engine.EngineOutput,
) (int, error) {
	handoff := BuildBraidNodeHandoff(node, rootPrompt, dependencySummaries, repairFeedback)
	argsMap := map[string]any{
		"prompt": RenderBraidNodeHandoffPrompt(handoff),
		"metadata": map[string]any{
			"braid_node_id":    node.ID,
			"braid_node_kind":  node.Kind,
			"braid_depends_on": append([]string(nil), node.DependsOn...),
			"handoff":          handoff,
		},
	}
	if maxSummaryChars := EffectiveBraidNodeSummaryChars(node); maxSummaryChars > 0 {
		argsMap["max_summary_chars"] = maxSummaryChars
	}
	args, err := json.Marshal(argsMap)
	if err != nil {
		return 0, fmt.Errorf("rlm repl runner phase %q: marshal %s args: %w", phaseName, RLMQueryToolName, err)
	}

	callID := fmt.Sprintf("auto_%s_%s_%s", sanitizeToolCallIDPart(phaseName), sanitizeToolCallIDPart(node.ID), sanitizeToolCallIDPart(RLMQueryToolName))
	rawArgs := json.RawMessage(args)
	result, execErr := toolExec.Execute(ctx, RLMQueryToolName, rawArgs)
	toolCall := engine.ToolCall{ID: callID, Name: RLMQueryToolName, Arguments: rawArgs}
	toolResult := engine.ToolResult{ToolCallID: callID, Content: result}
	if execErr != nil {
		toolResult.IsError = true
		toolResult.Content = execErr.Error()
	}
	output.ToolCalls = append(output.ToolCalls, toolCall)
	output.ToolResults = append(output.ToolResults, toolResult)
	if execErr != nil {
		return 0, execErr
	}

	var decoded rlmQueryToolOutput
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return 0, fmt.Errorf("rlm repl runner phase %q: decode %s result: %w", phaseName, RLMQueryToolName, err)
	}
	if decoded.Child <= 0 {
		return 0, fmt.Errorf("rlm repl runner phase %q: %s returned invalid child ordinal %d", phaseName, RLMQueryToolName, decoded.Child)
	}
	return decoded.Child, nil
}

func runDirectBraidNode(
	ctx context.Context,
	phaseName string,
	node BraidNode,
	rootPrompt string,
	dependencySummaries map[string]string,
	repairFeedback string,
	toolExec *replToolExecutor,
	output *engine.EngineOutput,
) (string, error) {
	handoff := BuildBraidNodeHandoff(node, rootPrompt, dependencySummaries, repairFeedback)
	argsMap := map[string]any{
		"prompt": RenderBraidNodeHandoffPrompt(handoff),
		"metadata": map[string]any{
			"braid_node_id":    node.ID,
			"braid_node_kind":  node.Kind,
			"braid_depends_on": append([]string(nil), node.DependsOn...),
			"handoff":          handoff,
		},
	}
	if maxSummaryChars := EffectiveBraidNodeSummaryChars(node); maxSummaryChars > 0 {
		argsMap["max_summary_chars"] = maxSummaryChars
	}
	args, err := json.Marshal(argsMap)
	if err != nil {
		return "", fmt.Errorf("rlm repl runner phase %q: marshal %s args: %w", phaseName, RLMQueryToolName, err)
	}

	callID := fmt.Sprintf("auto_%s_%s_%s", sanitizeToolCallIDPart(phaseName), sanitizeToolCallIDPart(node.ID), sanitizeToolCallIDPart(RLMQueryToolName))
	rawArgs := json.RawMessage(args)
	result, execErr := toolExec.Execute(ctx, RLMQueryToolName, rawArgs)
	toolCall := engine.ToolCall{ID: callID, Name: RLMQueryToolName, Arguments: rawArgs}
	toolResult := engine.ToolResult{ToolCallID: callID, Content: result}
	if execErr != nil {
		toolResult.IsError = true
		toolResult.Content = execErr.Error()
	}
	output.ToolCalls = append(output.ToolCalls, toolCall)
	output.ToolResults = append(output.ToolResults, toolResult)
	if execErr != nil {
		return "", execErr
	}

	var decoded struct {
		Answer string `json:"answer"`
	}
	_ = json.Unmarshal([]byte(result), &decoded)
	if strings.TrimSpace(decoded.Answer) != "" {
		return "status: completed summary: " + strings.TrimSpace(decoded.Answer), nil
	}
	if strings.TrimSpace(result) != "" {
		return "status: completed summary: " + strings.TrimSpace(result), nil
	}
	return "status: blocked blocker: child returned no summary", nil
}

func waitBraidNodeWave(ctx context.Context, phaseName string, children []int, toolExec *replToolExecutor, output *engine.EngineOutput) (map[int]string, error) {
	argsMap := map[string]any{
		"children":     append([]int(nil), children...),
		"min_complete": len(children),
	}
	args, err := json.Marshal(argsMap)
	if err != nil {
		return nil, fmt.Errorf("rlm repl runner phase %q: marshal %s args: %w", phaseName, RLMWaitToolName, err)
	}

	callID := fmt.Sprintf("auto_%s_%s", sanitizeToolCallIDPart(phaseName), sanitizeToolCallIDPart(RLMWaitToolName))
	rawArgs := json.RawMessage(args)
	result, execErr := toolExec.Execute(ctx, RLMWaitToolName, rawArgs)
	toolCall := engine.ToolCall{ID: callID, Name: RLMWaitToolName, Arguments: rawArgs}
	toolResult := engine.ToolResult{ToolCallID: callID, Content: result}
	if execErr != nil {
		toolResult.IsError = true
		toolResult.Content = execErr.Error()
	}
	output.ToolCalls = append(output.ToolCalls, toolCall)
	output.ToolResults = append(output.ToolResults, toolResult)
	if execErr != nil {
		return nil, execErr
	}

	var decoded rlmWaitToolOutput
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return nil, fmt.Errorf("rlm repl runner phase %q: decode %s result: %w", phaseName, RLMWaitToolName, err)
	}
	if len(decoded.Pending) > 0 {
		return nil, fmt.Errorf("rlm repl runner phase %q: braid wave returned %d pending children", phaseName, len(decoded.Pending))
	}
	out := map[int]string{}
	for _, item := range decoded.Completed {
		out[item.Child] = formatBraidNodeSummary(item)
	}
	for _, item := range decoded.Failed {
		out[item.Child] = formatBraidNodeSummary(item)
	}
	return out, nil
}

func formatBraidNodeSummary(item rlmNodeSummary) string {
	parts := []string{fmt.Sprintf("status: %s", item.Status)}
	if strings.TrimSpace(item.Summary) != "" {
		parts = append(parts, "summary: "+strings.TrimSpace(item.Summary))
	}
	if strings.TrimSpace(item.ErrorMessage) != "" {
		parts = append(parts, "error: "+strings.TrimSpace(item.ErrorMessage))
	}
	return strings.Join(parts, " ")
}

func validateBraidGraphAfterRuntimeRewrite(phase REPLRunnerPhase, graph BraidGraph) error {
	if err := ValidateBraidGraph(graph, phase.MaxGraphNodes); err != nil {
		return err
	}
	if strings.TrimSpace(phase.BraidGraphPolicy) != "" {
		if err := ValidateBraidGraphPolicy(graph, phase.BraidGraphPolicy); err != nil {
			return err
		}
	}
	if phase.RequireScaffoldContract {
		if err := ValidateBraidGraphScaffoldContract(graph); err != nil {
			return err
		}
	}
	return nil
}

// seedSolverStateFromBraidGraph creates a generalsolver.SolverState seeded
// from the braid graph nodes using the bridge adapter.
func seedSolverStateFromBraidGraph(graph *BraidGraph) *generalsolver.SolverState {
	if graph == nil || len(graph.Nodes) == 0 {
		return generalsolver.NewSolverState()
	}
	state := generalsolver.NewSolverState()
	nodeLikes := make([]generalsolver.BraidNodeLike, len(graph.Nodes))
	for i, node := range graph.Nodes {
		nodeLikes[i] = generalsolver.BraidNodeLike{
			ID:              node.ID,
			Kind:            node.Kind,
			Question:        node.Question,
			DependsOn:       node.DependsOn,
			MaxSummaryChars: node.MaxSummaryChars,
			HelperPolicy:    node.HelperPolicy,
			Archetype:       node.Archetype,
			ScaffoldClass:   node.ScaffoldClass,
			ScaffoldID:      node.ScaffoldID,
			InputSchema:     node.InputSchema,
		}
	}
	if err := generalsolver.BraidToWorkItems(state, nodeLikes, graph.FinalNode); err != nil {
		return generalsolver.NewSolverState()
	}
	// Proactively split large work items during seeding so the solver state
	// has decomposed sub-items before execution begins.
	applyGraphLevelSplits(state, graph)
	return state
}

// applyBraidGraphSplits rewrites the BraidGraph by decomposing nodes whose
// helper input would exceed structural thresholds (bindings, queries, events,
// constraints counts). This is the execution-path integration: the rewritten
// graph is what readyBraidNodes actually iterates over, so splits produce real
// sub-nodes with concrete chunk payloads.
//
// For each oversized node, the original is removed and replaced with:
//   - <id>__parse: extracts/normalizes the raw input
//   - <id>__solve_01..<id>__solve_NN: independent or sequential sub-problems
//   - <id>__merge: combines sub-results into the original node's output
//
// Downstream nodes that depended on the original are rewired to depend on
// the merge node.
func applyBraidGraphSplits(graph *BraidGraph, toolExec *replToolExecutor, phaseName string) {
	if graph == nil || len(graph.Nodes) == 0 {
		return
	}

	// Collect nodes that need splitting.
	var toSplit []string
	for _, node := range graph.Nodes {
		if node.Kind != "solve" {
			continue
		}
		policy := braidNodeEffectiveHelperPolicy(node)
		if policy != BraidNodeHelperPolicyPreferred && policy != BraidNodeHelperPolicyRequired {
			continue
		}
		if braidNodeHasRegisteredSplitPolicy(node) && shouldStructuralSplitFromInput(node) {
			toSplit = append(toSplit, node.ID)
			continue
		}
	}

	if len(toSplit) == 0 {
		return
	}

	// Build a lookup map for rewiring.
	nodesByID := make(map[string]*BraidNode, len(graph.Nodes))
	for i := range graph.Nodes {
		nodesByID[graph.Nodes[i].ID] = &graph.Nodes[i]
	}

	var newNodes []BraidNode
	removeIDs := map[string]bool{}

	for _, nodeID := range toSplit {
		node, ok := nodesByID[nodeID]
		if !ok {
			continue
		}
		splitPayloads, splitMode := braidSplitPayloadsForNode(*node)
		chunkCount := len(splitPayloads)
		if chunkCount < 2 {
			continue
		}

		parseID := nodeID + "__parse"
		mergeID := nodeID + "__merge"
		solveIDs := make([]string, chunkCount)
		for i := range solveIDs {
			solveIDs[i] = fmt.Sprintf("%s__solve_%02d", nodeID, i)
		}

		// Parse node: inherits original deps.
		newNodes = append(newNodes, BraidNode{
			ID:              parseID,
			Kind:            "extract",
			Question:        fmt.Sprintf("Parse and normalize input for sub-problem decomposition (parent: %s)", nodeID),
			DependsOn:       append([]string(nil), node.DependsOn...),
			HelperPolicy:    BraidNodeHelperPolicyNever,
			MaxSummaryChars: 4000,
		})

		// Solve nodes: each handles one concrete chunk.
		for i, solveID := range solveIDs {
			chunkGoal := fmt.Sprintf("Solve sub-problem %d/%d (parent: %s)", i+1, chunkCount, nodeID)
			deps := []string{parseID}
			if splitMode == "sequential" && i > 0 {
				deps = append(deps, solveIDs[i-1])
			}
			newNodes = append(newNodes, BraidNode{
				ID:              solveID,
				Kind:            "solve",
				Question:        chunkGoal,
				DependsOn:       deps,
				HelperPolicy:    node.HelperPolicy,
				Archetype:       node.Archetype,
				ScaffoldClass:   node.ScaffoldClass,
				ScaffoldID:      node.ScaffoldID,
				InputSchema:     splitPayloads[i],
				MaxSummaryChars: node.MaxSummaryChars,
			})
		}

		// Merge node: combines sub-results.
		newNodes = append(newNodes, BraidNode{
			ID:        mergeID,
			Kind:      "reduce",
			Question:  fmt.Sprintf("Merge %d sub-problem results into final answer (parent: %s)", chunkCount, nodeID),
			DependsOn: solveIDs,
			InputSchema: map[string]any{
				"split_role":                "merge",
				"split_mode":                splitMode,
				"parent_id":                 nodeID,
				"solve_ids":                 solveIDs,
				"required_artifact_status":  []any{"solved", "pass"},
				"block_on_missing_artifact": true,
			},
			ExpectedOutput:  node.ExpectedOutput,
			MaxSummaryChars: node.MaxSummaryChars,
		})

		// Rewire downstream nodes to depend on merge instead of original.
		for i := range graph.Nodes {
			if removeIDs[graph.Nodes[i].ID] {
				continue
			}
			for j, depID := range graph.Nodes[i].DependsOn {
				if depID == nodeID {
					graph.Nodes[i].DependsOn[j] = mergeID
				}
			}
		}

		// If this node was the final node, update final node to merge.
		if graph.FinalNode == nodeID {
			graph.FinalNode = mergeID
		}

		removeIDs[nodeID] = true
		if toolExec != nil && toolExec.recorder != nil {
			toolExec.recorder.RecordBraidEvent(BraidEvent{
				Phase:   phaseName,
				NodeID:  nodeID,
				Status:  "graph_split",
				Message: fmt.Sprintf("split into %d %s solve + parse + merge nodes (archetype=%s)", chunkCount, splitMode, node.Archetype),
			})
		}
	}

	// Rebuild graph.Nodes: remove originals, add new sub-nodes.
	filtered := make([]BraidNode, 0, len(graph.Nodes)-len(removeIDs)+len(newNodes))
	for _, node := range graph.Nodes {
		if !removeIDs[node.ID] {
			filtered = append(filtered, node)
		}
	}
	filtered = append(filtered, newNodes...)
	graph.Nodes = filtered
}

func braidSplitPayloadsForNode(node BraidNode) ([]map[string]any, string) {
	contract, ok := braidScaffoldContractFor(node.ScaffoldClass, node.ScaffoldID)
	if !ok || contract.SplitPolicy == nil {
		return nil, ""
	}
	policy := contract.SplitPolicy
	baseInput := cloneMapAny(node.InputSchema)
	chunks := braidSplitChunksForPolicy(baseInput, policy)
	if len(chunks) > generalsolver.SplitMaxSubItems {
		chunks = chunks[:generalsolver.SplitMaxSubItems]
	}
	if len(chunks) < 2 {
		return nil, strings.TrimSpace(policy.Mode)
	}
	splitMode := strings.TrimSpace(policy.Mode)
	if splitMode == "" {
		splitMode = "independent"
	}

	payloads := make([]map[string]any, len(chunks))
	parentSchema := braidSplitParentSchema(baseInput)
	sourceRef := baseInput["source_ref"]
	for i, chunk := range chunks {
		payload := map[string]any{
			"split_role":   "solve",
			"split_mode":   splitMode,
			"parent_id":    node.ID,
			"chunk_index":  i,
			"total_chunks": len(chunks),
			"chunk":        cloneMapAny(chunk),
			"parent_schema": map[string]any{
				"fields": parentSchema,
			},
			"carry_state": splitMode == "sequential",
		}
		if sourceRef != nil {
			payload["source_ref"] = sourceRef
		}
		for k, v := range chunk {
			if _, exists := payload[k]; !exists {
				payload[k] = v
			}
		}
		for _, key := range policy.PreserveKeys {
			if v, ok := baseInput[key]; ok {
				payload[key] = v
			}
		}
		payloads[i] = payload
	}
	return payloads, splitMode
}

func braidSplitChunksForPolicy(input map[string]any, policy *BraidSplitPolicy) []map[string]any {
	if len(input) == 0 || policy == nil {
		return nil
	}
	var chunks []map[string]any
	for _, key := range policy.ChunkKeys {
		items, ok := input[key].([]any)
		if !ok || len(items) < 2 {
			continue
		}
		for idx, item := range items {
			chunks = append(chunks, map[string]any{
				"chunk_label": strings.TrimSpace(key),
				"chunk_index": idx,
				"item":        item,
			})
			if policy.MaxChunks > 0 && len(chunks) >= policy.MaxChunks {
				return chunks
			}
		}
	}
	return chunks
}

func braidSplitParentSchema(input map[string]any) map[string]any {
	parent := cloneMapAny(input)
	for _, key := range []string{"queries", "sub_problems", "subproblems", "bindings", "events", "constraints"} {
		delete(parent, key)
	}
	return parent
}

//nolint:unused // Kept for feature-gated router pre-split experiments.
func applyBraidRouterSplits(graph *BraidGraph, toolExec *replToolExecutor, phaseName string) {
	if graph == nil || len(graph.Nodes) == 0 {
		return
	}
	for {
		applied := false
		for _, node := range append([]BraidNode(nil), graph.Nodes...) {
			if !shouldBraidRouterSplitNode(node) {
				continue
			}
			plan, ok := buildAdaptiveBraidSplitPlan(node, "router pre-split broad solve node before helper execution")
			if !ok {
				continue
			}
			if applyAdaptiveBraidSplitPlan(graph, plan) {
				applied = true
				if toolExec != nil && toolExec.recorder != nil {
					toolExec.recorder.RecordBraidEvent(BraidEvent{
						Phase:   phaseName,
						NodeID:  node.ID,
						Status:  "router_split",
						Message: fmt.Sprintf("split broad solve node into %d target nodes", len(plan.Targets)),
					})
				}
				break
			}
		}
		if !applied {
			return
		}
	}
}

//nolint:unused // Kept with applyBraidRouterSplits.
func shouldBraidRouterSplitNode(node BraidNode) bool {
	if node.Kind != "solve" {
		return false
	}
	policy := braidNodeEffectiveHelperPolicy(node)
	if policy != BraidNodeHelperPolicyPreferred && policy != BraidNodeHelperPolicyRequired {
		return false
	}
	if strings.Contains(node.ID, "__adaptive_") || strings.HasSuffix(node.ID, "__adaptive_merge") {
		return false
	}
	if len(adaptiveBraidCycleClusters(node)) > 0 {
		return true
	}
	targets := adaptiveBraidSplitDeclaredTargetIDs(node)
	return len(targets) >= 2
}

func braidNodeHasRegisteredSplitPolicy(node BraidNode) bool {
	contract, ok := braidScaffoldContractFor(node.ScaffoldClass, node.ScaffoldID)
	return ok && contract.SplitPolicy != nil
}

func shouldStructuralSplitFromInput(node BraidNode) bool {
	if !braidNodeHasRegisteredSplitPolicy(node) || len(node.InputSchema) == 0 {
		return false
	}
	syntheticItem := generalsolver.WorkItem{
		ID:      node.ID,
		Payload: cloneMapAny(node.InputSchema),
	}
	plan := generalsolver.AnalyzeForSplit(syntheticItem)
	return plan.Strategy != generalsolver.SplitStrategyNone
}

// splitChunkCountForNode determines how many solve sub-items to create.
//
//nolint:unused // Kept for helper-factory split sizing variants.
func splitChunkCountForNode(node BraidNode) int {
	// Try structural analysis first.
	if parsed, ok := helperFactoryExtractInstanceFields(node.Question); ok && len(parsed) > 0 {
		syntheticItem := generalsolver.WorkItem{
			ID:      node.ID,
			Payload: parsed,
		}
		plan := generalsolver.AnalyzeForSplit(syntheticItem)
		if plan.ChunkCount > 0 {
			return plan.ChunkCount
		}
	}
	// Archetype-based default: 4 chunks for compute-heavy archetypes.
	archetype := strings.ToLower(strings.TrimSpace(node.Archetype))
	switch archetype {
	case "symbolic_trace":
		return 4
	default:
		return 2
	}
}

// applyGraphLevelSplits examines each work item in the solver state and splits
// items whose payloads exceed the splitting threshold. Split items are replaced
// with parse → solve₁…solveₙ → merge sub-items.
func applyGraphLevelSplits(state *generalsolver.SolverState, graph *BraidGraph) {
	if state == nil {
		return
	}
	// Collect IDs that need splitting (cannot iterate and mutate simultaneously).
	var toSplit []string
	for id, item := range state.Items {
		if item.Status != generalsolver.StatusReady && item.Status != generalsolver.StatusPending {
			continue
		}
		plan := generalsolver.AnalyzeForSplit(item)
		if plan.Strategy != generalsolver.SplitStrategyNone {
			toSplit = append(toSplit, id)
		}
	}
	for _, id := range toSplit {
		// Item may have been removed by a prior split's rewireDependents.
		if _, exists := state.Items[id]; !exists {
			continue
		}
		if _, err := generalsolver.SplitWorkItem(state, id); err != nil {
			// Log but don't block — the original item stays unsplit.
			_ = err
		}
	}
}

// commitSolverArtifact records a successful helper result as a WorkArtifact
// in the solver state. Failures are silently ignored so the solver state
// shadow never blocks the primary execution path.
func commitSolverArtifact(state *generalsolver.SolverState, nodeID string, summary string) {
	if state == nil {
		return
	}
	item, exists := state.Items[nodeID]
	if !exists {
		return
	}
	if item.Status == generalsolver.StatusSolved {
		return
	}
	artifact := generalsolver.BraidSummaryToArtifact(nodeID, summary)
	item.Status = generalsolver.StatusSolving
	state.Items[nodeID] = item
	_ = generalsolver.CommitArtifact(state, nodeID, artifact)
}

// recordSolverFailure records a failed helper attempt in the solver state.
func recordSolverFailure(state *generalsolver.SolverState, nodeID string, stage string, errText string) {
	if state == nil {
		return
	}
	item, exists := state.Items[nodeID]
	if !exists {
		return
	}
	if item.Status == generalsolver.StatusSolved {
		return
	}
	item.Status = generalsolver.StatusSolving
	state.Items[nodeID] = item
	feedback := map[string]any{
		"stage": stage,
	}
	if errText != "" {
		feedback["error"] = truncateForSolverFeedback(errText, 500)
	}
	_ = generalsolver.RecordFailure(state, nodeID, stage+": "+truncateForSolverFeedback(errText, 200), feedback)
}

func truncateForSolverFeedback(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen < 15 {
		return s[:maxLen]
	}
	return s[:maxLen-12] + "...[trunc]"
}

// appendSolverStateTelemetry adds the solver state summary to the output
// telemetry as a structured shadow alongside the existing summaries map.
func appendSolverStateTelemetry(output *engine.EngineOutput, state *generalsolver.SolverState, phaseName string) {
	if output == nil || state == nil {
		return
	}
	// Compact failures first so the summary reflects post-compaction state.
	digest := generalsolver.CompactFailureDigest(state)
	summary := generalsolver.SummarizeState(state)
	telemetry := map[string]any{
		"phase":         phaseName,
		"total_items":   summary.TotalItems,
		"solved_count":  len(summary.SolvedIDs),
		"blocked_count": len(summary.BlockedIDs),
		"ready_count":   summary.ReadyCount,
		"artifacts":     summary.Artifacts,
		"failure_count": summary.FailureCount,
		"digest_count":  summary.DigestCount,
	}
	if len(summary.SolvedIDs) > 0 {
		telemetry["solved_ids"] = summary.SolvedIDs
	}
	if len(summary.BlockedIDs) > 0 {
		telemetry["blocked_ids"] = summary.BlockedIDs
	}
	if digest != "" {
		telemetry["failure_digest"] = digest
	}
	if len(summary.ByStatus) > 0 {
		statusMap := make(map[string]int, len(summary.ByStatus))
		for k, v := range summary.ByStatus {
			statusMap[string(k)] = v
		}
		telemetry["by_status"] = statusMap
	}
	if len(summary.ByArchetype) > 0 {
		archMap := make(map[string]int, len(summary.ByArchetype))
		for k, v := range summary.ByArchetype {
			archMap[string(k)] = v
		}
		telemetry["by_archetype"] = archMap
	}
	body, err := json.Marshal(telemetry)
	if err != nil {
		return
	}
	output.ToolCalls = append(output.ToolCalls, engine.ToolCall{
		ID:   "solver_state_" + sanitizeToolCallIDPart(phaseName),
		Name: "solver_state_telemetry",
	})
	output.ToolResults = append(output.ToolResults, engine.ToolResult{
		ToolCallID: "solver_state_" + sanitizeToolCallIDPart(phaseName),
		Content:    string(body),
	})
}
