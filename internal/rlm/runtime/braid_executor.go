package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

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
				if summary, handled, err := runBraidNodeHelperFirst(ctx, phaseName, node, rootPrompt, dependencySummarySubset(node, summaries), repairFeedbackByNode[node.ID], toolExec, output, graph); handled {
					if err != nil {
						if prepareBraidRepair(phase, graph, node, summary, summaries, executed, repairFeedbackByNode, &repairAttempts) {
							recordBraidNodeEvent(toolExec, phaseName, wave, node, "repairing", err.Error())
							continue graphLoop
						}
						recordBraidNodeEvent(toolExec, phaseName, wave, node, "rejected", err.Error())
						return err
					}
					summaries[node.ID] = summary
					executed[node.ID] = struct{}{}
					continue
				}
				if maxSubcalls > 0 && submitted+1 > maxSubcalls {
					return fmt.Errorf("rlm repl runner phase %q: braid graph needs %d subcalls, exceeds max %d", phaseName, submitted+1, maxSubcalls)
				}
				summary, err := runDirectBraidNode(ctx, phaseName, node, rootPrompt, dependencySummarySubset(node, summaries), repairFeedbackByNode[node.ID], toolExec, output)
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
							continue
						} else {
							err = recoveredErr
						}
					}
					if prepareBraidRepair(phase, graph, node, summary, summaries, executed, repairFeedbackByNode, &repairAttempts) {
						recordBraidNodeEvent(toolExec, phaseName, wave, node, "repairing", err.Error())
						continue graphLoop
					}
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "rejected", err.Error())
					return err
				}
			}
			continue
		}

		waveChildren := make([]int, 0, len(ready))
		waveNodeIDs := make([]string, 0, len(ready))
		for _, node := range ready {
			recordBraidNodeEvent(toolExec, phaseName, wave, node, "ready", "")
			deps := dependencySummarySubset(node, summaries)
			if summary, handled, err := runBraidNodeHelperFirst(ctx, phaseName, node, rootPrompt, deps, repairFeedbackByNode[node.ID], toolExec, output, graph); handled {
				if err != nil {
					if prepareBraidRepair(phase, graph, node, summary, summaries, executed, repairFeedbackByNode, &repairAttempts) {
						recordBraidNodeEvent(toolExec, phaseName, wave, node, "repairing", err.Error())
						continue graphLoop
					}
					recordBraidNodeEvent(toolExec, phaseName, wave, node, "rejected", err.Error())
					return err
				}
				summaries[node.ID] = summary
				executed[node.ID] = struct{}{}
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
						continue
					} else {
						err = recoveredErr
					}
				}
				if prepareBraidRepair(phase, graph, nodesByID[nodeID], summary, summaries, executed, repairFeedbackByNode, &repairAttempts) {
					recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "repairing", err.Error())
					continue graphLoop
				}
				recordBraidNodeEvent(toolExec, phaseName, wave, nodesByID[nodeID], "rejected", err.Error())
				return err
			}
		}
	}

	if _, ok := summaries[graph.FinalNode]; !ok {
		return fmt.Errorf("rlm repl runner phase %q: braid final_node %q did not complete", phaseName, graph.FinalNode)
	}
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
	toolExec *replToolExecutor,
	output *engine.EngineOutput,
	graph *BraidGraph,
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
	recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first", policy)
	summary, ok := runBraidNodeHelper(ctx, phaseName, node, rootPrompt, dependencySummaries, repairFeedback, "helper_policy="+policy, toolExec, output)
	if !ok {
		if policy == BraidNodeHelperPolicyRequired {
			return "", true, fmt.Errorf("rlm repl runner phase %q: braid node %q requires helper execution but helper did not produce an answer", phaseName, node.ID)
		}
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_fallback", "helper did not produce an answer")
		return "", false, nil
	}
	if err := validateBraidNodeExecutionSummaryInGraph(phaseName, node, summary, graph.FinalNode, graph); err != nil {
		recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_rejected", err.Error())
		return summary, true, err
	}
	recordBraidNodeEvent(toolExec, phaseName, 0, node, "helper_first_completed", summary)
	return summary, true, nil
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
	prompt := RenderBraidNodeChildPromptWithFeedback(node, rootPrompt, dependencySummaries, repairFeedback)
	instructions := buildBraidHelperRecoveryInstructions(node, triggerSummary)
	argsMap := map[string]any{
		"prompt":       prompt,
		"instructions": instructions,
		"max_attempts": 5,
	}
	if input := braidHelperInput(rootPrompt, dependencySummaries); len(input) > 0 {
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
		helperCfg.AnswerVerifier = stackMoveAnswerVerifier
		helperCfg.Search.BeamWidth = firstPositiveInt(helperCfg.Search.BeamWidth, 3)
		helperExec = &HelperFactoryTools{Config: helperCfg}
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
	answer := helperAnswerFromToolResult(result)
	if strings.TrimSpace(answer) == "" {
		return "", false
	}
	if isBraidSolveKind(node.Kind) {
		if ok, detail, applicable := verifyStackMoveCandidateFromInput(answer, argsMap["input"]); applicable && !ok {
			return formatBraidHelperNodeSummary(node, "pass: false first_failure: "+detail), true
		}
	}
	return formatBraidHelperNodeSummary(node, answer), true
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

func formatBraidHelperNodeSummary(node BraidNode, answer string) string {
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
	return "status: completed summary: status: solved answer: " + answer + " checks: ephemeral_helper_solve produced and ran an executable helper for this node."
}

func helperAnswerFromToolResult(result string) string {
	var decoded struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err == nil {
		if decoded.OK && strings.TrimSpace(decoded.Answer) != "" {
			return strings.TrimSpace(decoded.Answer)
		}
		return ""
	}
	return strings.TrimSpace(result)
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
		rawMove, ok := item.([]any)
		if !ok || len(rawMove) != 3 {
			return nil, false
		}
		var move [3]int
		for idx, value := range rawMove {
			n, ok := intFromJSONNumberLike(value)
			if !ok {
				return nil, false
			}
			move[idx] = n
		}
		moves = append(moves, move)
	}
	return moves, true
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
		repairFeedbackByNode[solveID] = buildBraidRepairFeedback(failedNode, failedSummary)
	}
	return true
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
	argsMap := map[string]any{
		"prompt": RenderBraidNodeChildPromptWithFeedback(node, rootPrompt, dependencySummaries, repairFeedback),
		"metadata": map[string]any{
			"braid_node_id":    node.ID,
			"braid_node_kind":  node.Kind,
			"braid_depends_on": append([]string(nil), node.DependsOn...),
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
	argsMap := map[string]any{
		"prompt": RenderBraidNodeChildPromptWithFeedback(node, rootPrompt, dependencySummaries, repairFeedback),
		"metadata": map[string]any{
			"braid_node_id":    node.ID,
			"braid_node_kind":  node.Kind,
			"braid_depends_on": append([]string(nil), node.DependsOn...),
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
