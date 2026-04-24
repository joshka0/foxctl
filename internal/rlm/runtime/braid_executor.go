package runtime

import (
	"context"
	"encoding/json"
	"fmt"
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
		if maxSubcalls > 0 && submitted+len(ready) > maxSubcalls {
			return fmt.Errorf("rlm repl runner phase %q: braid graph needs %d subcalls, exceeds max %d", phaseName, submitted+len(ready), maxSubcalls)
		}

		if !toolExec.allowAsyncRLMTools() {
			for _, node := range ready {
				recordBraidNodeEvent(toolExec, phaseName, wave, node, "ready", "")
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
				submitted++
				if err := validateBraidNodeExecutionSummary(phaseName, node, summary, graph.FinalNode); err != nil {
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
			child, err := submitBraidNode(ctx, phaseName, node, rootPrompt, deps, repairFeedbackByNode[node.ID], toolExec, output)
			if err != nil {
				return err
			}
			waveChildren = append(waveChildren, child)
			waveNodeIDs = append(waveNodeIDs, node.ID)
			submitted++
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
			if err := validateBraidNodeExecutionSummary(phaseName, nodesByID[nodeID], summary, graph.FinalNode); err != nil {
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
	if failedNode.Kind != "solve" && failedNode.Kind != "verify" && failedNode.Kind != "reduce" {
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
	case "solve":
		b.WriteString("Previous solve node returned blocked. Revise this solve node and produce candidate values or a concrete mathematical check before returning.\n")
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
		if node.Kind == "solve" {
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

func validateBraidNodeExecutionSummary(phaseName string, node BraidNode, summary string, finalNodeID string) error {
	statuses := braidSummaryStatuses(summary)
	if len(statuses) == 0 {
		return fmt.Errorf("rlm repl runner phase %q: braid node %q returned no status field", phaseName, node.ID)
	}
	if braidStatusesContainAny(statuses, "failed", "failure", "blocked", "error") {
		return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) did not complete: %s", phaseName, node.ID, node.Kind, safeTelemetryExcerpt(summary, 300))
	}
	if node.ID == finalNodeID || node.Kind == "extract" || node.Kind == "verify" || node.Kind == "reduce" {
		if braidStatusesContainAny(statuses, "partial") {
			return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) returned partial status: %s", phaseName, node.ID, node.Kind, safeTelemetryExcerpt(summary, 300))
		}
		if !braidStatusesContainAny(statuses, "solved", "ok", "completed", "pass", "passed") {
			return fmt.Errorf("rlm repl runner phase %q: braid node %q (%s) returned unsupported status %q", phaseName, node.ID, node.Kind, strings.Join(statuses, ","))
		}
	}
	return nil
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
	if node.MaxSummaryChars > 0 {
		argsMap["max_summary_chars"] = node.MaxSummaryChars
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
	if node.MaxSummaryChars > 0 {
		argsMap["max_summary_chars"] = node.MaxSummaryChars
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
