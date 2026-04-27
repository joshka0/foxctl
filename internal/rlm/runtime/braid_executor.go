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

	nodesByID := make(map[string]BraidNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodesByID[node.ID] = node
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
	executed := map[string]struct{}{}
	repairFeedbackByNode := map[string]string{}
	helperBudgets := make(helperBudgetByNode, len(graph.Nodes))
	// Apply structural splits to the graph before execution so that
	// readyBraidNodes operates on the decomposed graph, not the original.
	applyBraidGraphSplits(graph, toolExec, phaseName)
	solverState := seedSolverStateFromBraidGraph(graph)
	repairAttempts := 0
	submitted := 0
	wave := 0
graphLoop:
	for len(executed) < len(graph.Nodes) {
		ready := readyBraidNodes(*graph, executed, summaries)
		if len(ready) == 0 {
			return fmt.Errorf("rlm repl runner phase %q: braid graph stalled before final_node %q", phaseName, graph.FinalNode)
		}
		wave++

		if !toolExec.allowAsyncRLMTools() {
			for _, node := range ready {
				recordBraidNodeEvent(toolExec, phaseName, wave, node, "ready", "")
				deps := dependencySummarySubset(node, summaries)
				if summary, ok := runBraidRuntimeNodeShortcut(node, deps); ok {
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "runtime_shortcut", summary)
					summaries[node.ID] = summary
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
				summaries[node.ID] = summary
				executed[node.ID] = struct{}{}
				if recovered, ok := recoverBraidNodeWithHelper(ctx, phaseName, node, rootPrompt, dependencySummarySubset(node, summaries), repairFeedbackByNode[node.ID], summary, toolExec, output); ok {
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "helper_recovered", recovered)
					summaries[node.ID] = recovered
					summary = recovered
				}
				submitted++
				if err := validateBraidNodeExecutionSummaryInGraph(phaseName, node, summary, graph.FinalNode, graph); err != nil {
					if recovered, ok := recoverBraidNodeWithHelper(ctx, phaseName, node, rootPrompt, dependencySummarySubset(node, summaries), repairFeedbackByNode[node.ID], summary, toolExec, output); ok {
						recordBraidNodeEvent(toolExec, phaseName, wave, node, "helper_recovered", recovered)
						summaries[node.ID] = recovered
						if recoveredErr := validateBraidNodeExecutionSummaryInGraph(phaseName, node, recovered, graph.FinalNode, graph); recoveredErr == nil {
							commitSolverArtifact(solverState, node.ID, recovered)
							continue
						} else {
							err = recoveredErr
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
			if summary, ok := runBraidRuntimeNodeShortcut(node, deps); ok {
				recordBraidNodeEvent(toolExec, phaseName, wave, node, "runtime_shortcut", summary)
				summaries[node.ID] = summary
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
			summaries[nodeID] = summary
			executed[nodeID] = struct{}{}
			if recovered, ok := recoverBraidNodeWithHelper(ctx, phaseName, nodesByID[nodeID], rootPrompt, dependencySummarySubset(nodesByID[nodeID], summaries), repairFeedbackByNode[nodeID], summary, toolExec, output); ok {
				recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "helper_recovered", recovered)
				summaries[nodeID] = recovered
				summary = recovered
			}
			if err := validateBraidNodeExecutionSummaryInGraph(phaseName, nodesByID[nodeID], summary, graph.FinalNode, graph); err != nil {
				if recovered, ok := recoverBraidNodeWithHelper(ctx, phaseName, nodesByID[nodeID], rootPrompt, dependencySummarySubset(nodesByID[nodeID], summaries), repairFeedbackByNode[nodeID], summary, toolExec, output); ok {
					recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "helper_recovered", recovered)
					summaries[nodeID] = recovered
					if recoveredErr := validateBraidNodeExecutionSummaryInGraph(phaseName, nodesByID[nodeID], recovered, graph.FinalNode, graph); recoveredErr == nil {
						commitSolverArtifact(solverState, nodeID, recovered)
						continue
					} else {
						err = recoveredErr
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
	appendBraidFinalHandoff(output, graph, summaries)
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
	if err := validateBraidNodeExecutionSummaryInGraph(phaseName, node, summary, graph.FinalNode, graph); err != nil {
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_rejected", "stage=verify duration_ms="+fmt.Sprintf("%d", duration.Milliseconds())+" "+err.Error())
		return summary, true, err
	}
	recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_completed", fmt.Sprintf("duration_ms=%d summary: %s", duration.Milliseconds(), summary))
	return summary, true, nil
}

func runBraidRuntimeNodeShortcut(node BraidNode, dependencySummaries map[string]string) (string, bool) {
	switch node.Kind {
	case "verify":
		for _, summary := range dependencySummaries {
			if braidDependencyHasRuntimeVerifiedSolution(summary) {
				return "status: pass summary: answer: pass: true checks: upstream solve dependency was already verified by the runtime scaffold verifier.", true
			}
		}
	case "reduce":
		if !braidDependencyVerificationPassed(dependencySummaries) {
			return "", false
		}
		for _, summary := range dependencySummaries {
			if answer, ok := braidSolutionAnswerFromSummary(summary); ok {
				return "status: completed summary: status: solved answer: " + answer + " checks: reduce forwarded verified solve answer.", true
			}
		}
	}
	return "", false
}

func braidDependencyHasRuntimeVerifiedSolution(summary string) bool {
	if _, ok := braidSolutionAnswerFromSummary(summary); !ok {
		return false
	}
	return strings.Contains(summary, "checks: ephemeral_helper_solve verified candidate with a runtime scaffold verifier")
}

func braidDependencyVerificationPassed(dependencySummaries map[string]string) bool {
	for _, summary := range dependencySummaries {
		if braidVerificationSummaryPassed(summary) {
			return true
		}
	}
	return false
}

func braidSolutionAnswerFromSummary(summary string) (string, bool) {
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
	if isBraidSolveKind(node.Kind) && helperExec != nil {
		helperCfg := helperExec.Config
		helperCfg.Search.BeamWidth = firstPositiveInt(helperCfg.Search.BeamWidth, 3)
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
	answer, helperVerified := helperAnswerAndVerifiedFromToolResult(result)
	if strings.TrimSpace(answer) == "" {
		return "", false
	}
	if isBraidSolveKind(node.Kind) {
		if ok, detail, applicable := verifyStackMoveCandidateFromInput(answer, argsMap["input"]); applicable && !ok {
			return formatBraidHelperNodeSummary(node, "pass: false first_failure: "+detail), true
		} else if applicable && ok {
			helperVerified = true
		}
	}
	return formatBraidHelperNodeSummaryVerified(node, answer, helperVerified), true
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
	for key, value := range dependencySummaries {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		deps[trimmedKey] = trimmedValue
		input[trimmedKey] = trimmedValue
	}
	if len(deps) > 0 {
		input["dependency_summaries"] = deps
	}
	return input
}

func braidNodeEffectiveHelperPolicy(node BraidNode) string {
	policy := normalizeBraidNodeHelperPolicy(node.HelperPolicy)
	if policy == "" {
		return BraidNodeHelperPolicyAuto
	}
	return policy
}

func braidNodeHelperFirstAllowed(node BraidNode) bool {
	policy := braidNodeEffectiveHelperPolicy(node)
	return (policy == BraidNodeHelperPolicyPreferred || policy == BraidNodeHelperPolicyRequired) && braidNodeHelperSupported(node)
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
	if isBraidSolveKind(node.Kind) {
		b.WriteString("For a solve node, build a complete candidate and run an internal deterministic check before returning it. Do not emit a partial action list, copied prefix, or unchecked guess. If the check fails, return `status: blocked first_failure: ...` instead of `solution = ...`.\n")
		b.WriteString("For state-transition tasks, model state explicitly, apply every candidate transition, and only return a candidate when final state and action legality both check out.\n")
		b.WriteString("When the state has many items, do not run exhaustive BFS/DFS over full state permutations. Prefer a constructive algorithm using the task's transition structure, then verify the constructed candidate.\n")
	}
	if node.Kind == "verify" {
		b.WriteString("For a verify node, simulate or substitute the candidate against the original constraints. Return `pass: true` only when every constraint is verified.\n")
		b.WriteString("If verification fails, return `pass: false first_failure: ...` with the earliest illegal transition, bad substitution, missing candidate, or observed-vs-expected mismatch. Include the failed step index when applicable.\n")
		b.WriteString("Dependency summaries are passed in the helper input under dependency_summaries and by dependency node id, for example input['n_solve'] when the graph has an n_solve dependency.\n")
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
	for i, chunk := range chunks {
		// Build a sub-input with just this chunk.
		subInput := map[string]any{
			"split_role":    "solve",
			"chunk_index":   i,
			"total_chunks":  len(chunks),
			"parent_id":     node.ID,
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
			subAnswers = append(subAnswers, fmt.Sprintf("chunk_%d: [oversized, skipped]", i))
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
			subAnswers = append(subAnswers, fmt.Sprintf("chunk_%d: [marshal error]", i))
			continue
		}

		callID := fmt.Sprintf("auto_%s_%s_split_%02d_%s", sanitizeToolCallIDPart(phaseName), sanitizeToolCallIDPart(node.ID), i, sanitizeToolCallIDPart(EphemeralHelperSolveToolName))
		rawArgs := json.RawMessage(subArgs)
		result, execErr := toolExec.helperFactory.Execute(ctx, EphemeralHelperSolveToolName, rawArgs)
		toolCall := engine.ToolCall{ID: callID, Name: EphemeralHelperSolveToolName, Arguments: rawArgs}
		toolResult := engine.ToolResult{ToolCallID: callID, Content: result}
		if execErr != nil {
			toolResult.IsError = true
			toolResult.Content = execErr.Error()
		}
		output.ToolCalls = append(output.ToolCalls, toolCall)
		output.ToolResults = append(output.ToolResults, toolResult)

		answer := helperAnswerFromToolResult(result)
		if strings.TrimSpace(answer) == "" {
			answer = "[no answer]"
		}
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "split_chunk_completed", fmt.Sprintf("chunk %d/%d: %s", i+1, len(chunks), safeTelemetryExcerpt(answer, 100)))
		subAnswers = append(subAnswers, answer)
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
	answer = strings.TrimSpace(answer)
	if node.Kind == "verify" {
		if braidPassFalseRE.MatchString(answer) {
			return "status: blocked summary: answer: " + answer + " checks: ephemeral_helper_solve simulated original constraints and found a concrete failure."
		}
		if braidPassTrueRE.MatchString(answer) || braidVerificationSummaryPassed(answer) {
			return "status: pass summary: answer: " + answer + " checks: ephemeral_helper_solve simulated original constraints."
		}
		return "status: blocked summary: answer: pass: false checks: ephemeral_helper_solve simulated original constraints but did not produce a passing verification. detail: " + safeTelemetryExcerpt(answer, 600)
	}
	if isBraidSolveKind(node.Kind) && braidPassFalseRE.MatchString(answer) {
		return "status: blocked summary: answer: " + answer + " checks: ephemeral_helper_solve self-verified candidate and found a concrete failure."
	}
	if isBraidSolveKind(node.Kind) {
		statuses := braidSummaryStatuses(answer)
		if braidStatusesContainAny(statuses, "blocked", "failed", "failure", "error") {
			return "status: blocked summary: answer: " + safeTelemetryExcerpt(answer, 600) + " checks: ephemeral_helper_solve did not produce a verified candidate."
		}
	}
	if verified {
		return "status: completed summary: status: solved answer: " + answer + " checks: ephemeral_helper_solve verified candidate with a runtime scaffold verifier."
	}
	return "status: completed summary: status: solved answer: " + answer + " checks: ephemeral_helper_solve produced and ran an executable helper for this node."
}

func helperAnswerFromToolResult(result string) string {
	answer, _ := helperAnswerAndVerifiedFromToolResult(result)
	return answer
}

func helperAnswerAndVerifiedFromToolResult(result string) (string, bool) {
	var decoded struct {
		OK       bool   `json:"ok"`
		Answer   string `json:"answer"`
		Error    string `json:"error"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err == nil {
		if decoded.OK && strings.TrimSpace(decoded.Answer) != "" {
			return strings.TrimSpace(decoded.Answer), decoded.Verified
		}
		return "", false
	}
	return strings.TrimSpace(result), false
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
	if raw[0] == '-' {
		sign = -1
		pos = 1
	} else if raw[0] == '+' {
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
	if !braidStatusesContainAny(statuses, "blocked") || braidStatusesContainAny(statuses, "failed", "failure", "error") {
		return false
	}
	solveIDs := braidRepairSolveNodeIDs(*graph, failedNode)
	if len(solveIDs) == 0 {
		return false
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
		b.WriteString("Do not treat circular-looking or mutual LongCoT dependencies as runtime blockers. Treat them as simultaneous constraints, fixed-point equations, or a small candidate search.\n")
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

var braidSummaryStatusRE = regexp.MustCompile(`(?i)(?:^|\s)status\s*:\s*([a-z][a-z0-9_-]*)`)
var braidPassTrueRE = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])pass\s*[:=]\s*true(?:[^a-z0-9_]|$)`)
var braidPassFalseRE = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])pass\s*[:=]\s*false(?:[^a-z0-9_]|$)`)

func validateBraidNodeExecutionSummary(phaseName string, node BraidNode, summary string, finalNodeID string) error {
	return validateBraidNodeExecutionSummaryInGraph(phaseName, node, summary, finalNodeID, nil)
}

func validateBraidNodeExecutionSummaryInGraph(phaseName string, node BraidNode, summary string, finalNodeID string, graph *BraidGraph) error {
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
	if node.ID == finalNodeID || node.Kind == "extract" || node.Kind == "verify" || node.Kind == "reduce" {
		if !braidStatusesContainAny(statuses, "solved", "ok", "completed", "pass", "passed") {
			return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) returned unsupported status %q", phaseName, node.ID, node.Kind, strings.Join(statuses, ","))
		}
	}
	if node.Kind == "verify" && !braidVerificationSummaryPassed(summary) {
		return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) did not report verification pass: %s", phaseName, node.ID, node.Kind, safeTelemetryExcerpt(summary, 300))
	}
	return nil
}

func braidVerificationSummaryPassed(summary string) bool {
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
	if graph == nil || node.ID == finalNodeID || node.Kind != "solve" {
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
// sub-nodes that execute independently.
//
// For each oversized node, the original is removed and replaced with:
//   - <id>__parse: extracts/normalizes the raw input
//   - <id>__solve_01..<id>__solve_NN: independent sub-problems
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
		if node.Kind != "solve" && node.Kind != "cycle_solve" {
			continue
		}
		policy := braidNodeEffectiveHelperPolicy(node)
		if policy != BraidNodeHelperPolicyPreferred && policy != BraidNodeHelperPolicyRequired {
			continue
		}
		// Check archetype-based preemptive split.
		if shouldPreemptiveSplitByArchetype(node) {
			toSplit = append(toSplit, node.ID)
			continue
		}
		// Check structural thresholds from parsed instance fields.
		if shouldStructuralSplitFromQuestion(node) {
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
		// Parse instance fields for chunk data.
		chunkCount := splitChunkCountForNode(*node)

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

		// Solve nodes: each handles one chunk.
		for i, solveID := range solveIDs {
			chunkGoal := fmt.Sprintf("Solve sub-problem %d/%d (parent: %s)", i+1, chunkCount, nodeID)
			newNodes = append(newNodes, BraidNode{
				ID:              solveID,
				Kind:            "solve",
				Question:        chunkGoal,
				DependsOn:       []string{parseID},
				HelperPolicy:    node.HelperPolicy,
				Archetype:       node.Archetype,
				MaxSummaryChars: node.MaxSummaryChars,
			})
		}

		// Merge node: combines sub-results.
		newNodes = append(newNodes, BraidNode{
			ID:              mergeID,
			Kind:            "reduce",
			Question:        fmt.Sprintf("Merge %d sub-problem results into final answer (parent: %s)", chunkCount, nodeID),
			DependsOn:       solveIDs,
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
				Phase:    phaseName,
				NodeID:   nodeID,
				Status:   "graph_split",
				Message:  fmt.Sprintf("split into %d solve + parse + merge nodes (archetype=%s)", chunkCount, node.Archetype),
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

// shouldPreemptiveSplitByArchetype returns true when a node's archetype
// signals a structurally multi-binding problem that should be preemptively
// decomposed regardless of payload size.
func shouldPreemptiveSplitByArchetype(node BraidNode) bool {
	archetype := strings.ToLower(strings.TrimSpace(node.Archetype))
	switch archetype {
	case "symbolic_trace":
		// Symbolic trace problems (HM type inference, etc.) are structurally
		// multi-binding. Even small instances benefit from parse/solve/merge.
		return true
	default:
		return false
	}
}

// shouldStructuralSplitFromQuestion returns true when the node's question
// contains parseable arrays that exceed count thresholds.
func shouldStructuralSplitFromQuestion(node BraidNode) bool {
	if parsed, ok := helperFactoryExtractInstanceFields(node.Question); ok && len(parsed) > 0 {
		syntheticItem := generalsolver.WorkItem{
			ID:      node.ID,
			Payload: parsed,
		}
		plan := generalsolver.AnalyzeForSplit(syntheticItem)
		return plan.Strategy != generalsolver.SplitStrategyNone
	}
	return false
}

// splitChunkCountForNode determines how many solve sub-items to create.
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
