package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// LambdaRunner executes deterministic recursive decomposition over the tool surface.
//
// Unlike LLMRunner (which uses an LLM tool loop for every iteration), LambdaRunner:
//  1. Classifies the task (1 LLM call)
//  2. Computes an optimal decomposition plan (0 LLM calls, pure math)
//  3. Executes Phi = Split -> Map(Phi) -> Reduce deterministically
//  4. Only calls the LLM at leaf nodes (evidence judgment) and for composition
//
// Guarantees:
//   - Termination: depth is analytically bounded
//   - Bounded cost: cost estimate computed before execution
//   - No LLM-generated control code: structure is deterministic Go
type LambdaRunner struct {
	Config LambdaConfig
	Tools  ToolExecutor
}

// Run implements Runner.
func (r LambdaRunner) Run(ctx context.Context, task Task, env Environment) (Result, error) {
	if err := ValidateTask(task); err != nil {
		return Result{}, err
	}
	if err := ValidateEnvironment(env); err != nil {
		return Result{}, err
	}
	if r.Tools == nil {
		return Result{}, fmt.Errorf("rlm lambda runner requires tool adapter")
	}
	r.Tools = newAllowlistedToolExecutor(r.Tools, env.Tools)

	cfg := r.Config.Defaults()
	if cfg.EphemeralSkills {
		result, err := r.runEphemeralHelper(ctx, task, cfg)
		if err != nil {
			return Result{}, err
		}
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		result.Metadata["plan_mode"] = string(PlanModeLambda)
		result.Metadata["lambda_mode"] = "ephemeral_helper"
		return result, nil
	}

	// Phase 1: Classify task (1 LLM call).
	taskType, taskTypeSource := explicitLambdaTaskType(task)
	var classifyUsage engine.TokenUsage
	var classifyErr error
	if taskTypeSource == "" {
		taskType, classifyUsage, classifyErr = classifyTaskWithUsage(ctx, cfg.LLM, task.Prompt)
		taskTypeSource = "classifier"
		if classifyErr != nil {
			taskType = TaskTypeGeneral
		}
	}

	// Phase 2: Estimate problem size and compute plan (0 LLM calls).
	n := estimateProblemSize(task, env)
	plan := PlanLambda(taskType, n, cfg)
	plan, planCaps := capLambdaPlanForTask(plan, task, cfg)

	// Phase 3: Execute Phi.
	result, err := r.executePhi(ctx, task, env, plan)
	if err != nil {
		return Result{}, err
	}

	// Attach execution metadata.
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["lambda_task_type"] = string(taskType)
	result.Metadata["lambda_task_type_source"] = taskTypeSource
	result.Metadata["lambda_k_star"] = plan.KStar
	result.Metadata["lambda_tau_star"] = plan.TauStar
	result.Metadata["lambda_depth"] = plan.Depth
	result.Metadata["lambda_cost_estimate"] = plan.CostEstimate
	result.Metadata["lambda_n"] = plan.N
	result.Metadata["lambda_compose_op"] = string(plan.ComposeOp)
	result.Metadata["plan_mode"] = string(PlanModeLambda)
	result.Metadata["lambda_classify_input_tokens"] = classifyUsage.InputTokens
	result.Metadata["lambda_classify_output_tokens"] = classifyUsage.OutputTokens
	addParentTokenUsage(result.Metadata, classifyUsage)
	for key, value := range planCaps {
		result.Metadata[key] = value
	}
	if classifyErr != nil {
		result.Metadata["lambda_classify_error"] = classifyErr.Error()
	}
	return result, nil
}

// executePhi runs the recursive combinator chain.
//
//	Phi(P):
//	  if |P| <= tau*: return leaf(P)       // LLM judges evidence
//	  chunks = split(P, k*)
//	  partials = [Phi(chunk) for chunk in chunks]
//	  return reduce(composeOp, partials)    // deterministic or 1 LLM call
func (r LambdaRunner) executePhi(ctx context.Context, task Task, env Environment, plan LambdaPlan) (Result, error) {
	if plan.Depth == 0 || plan.KStar <= 1 {
		return r.leaf(ctx, task, env, plan)
	}

	// Split: produce k* subproblems by running parallel search strategies.
	chunks := r.split(ctx, task, env, plan)

	// Map: recurse into each chunk. Store results by split index so concurrent
	// completion order cannot affect reduce input order.
	var mu sync.Mutex
	partialsByIndex := make([]Result, len(chunks))
	partialOK := make([]bool, len(chunks))
	childErrorCount := 0
	var wg sync.WaitGroup
	for i, chunk := range chunks {
		wg.Add(1)
		go func(index int, childTask Task) {
			defer wg.Done()
			childPlan := LambdaPlan{
				TaskType:  plan.TaskType,
				ComposeOp: plan.ComposeOp,
				KStar:     plan.KStar,
				TauStar:   plan.TauStar,
				Depth:     plan.Depth - 1,
			}
			childResult, err := r.executePhi(ctx, childTask, env, childPlan)
			if err != nil {
				mu.Lock()
				childErrorCount++
				mu.Unlock()
				return // graceful degradation
			}
			mu.Lock()
			partialsByIndex[index] = childResult
			partialOK[index] = true
			mu.Unlock()
		}(i, chunk)
	}
	wg.Wait()

	partials := make([]Result, 0, len(chunks))
	for i := range partialsByIndex {
		if partialOK[i] {
			partials = append(partials, partialsByIndex[i])
		}
	}

	if len(partials) == 0 {
		return Result{Answer: "No evidence found after recursive decomposition.", Metadata: map[string]any{
			"lambda_no_partials":       true,
			"lambda_child_error_count": childErrorCount,
		}}, nil
	}

	// Reduce: compose partial results.
	result, err := r.reduce(ctx, task, plan, partials)
	if err != nil {
		return Result{}, err
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	if childErrorCount > 0 {
		result.Metadata["lambda_child_error_count"] = childErrorCount
	}
	return result, nil
}

// leaf runs one bounded search + LLM judgment cycle.
//
// This is the ONLY place the LLM is used for evidence reasoning:
//  1. Call the primary search tool deterministically
//  2. Load top candidates deterministically
//  3. Ask the LLM: "Given this evidence, answer the question" (1 call)
func (r LambdaRunner) leaf(ctx context.Context, task Task, env Environment, plan LambdaPlan) (Result, error) {
	cfg := r.Config.Defaults()
	searchTool := SearchToolForTask(plan.TaskType)

	// Step 1: Run search deterministically (no LLM).
	searchResult, err := r.Tools.Execute(ctx, searchTool, jsonArgs(lambdaSearchArgs(task, plan)))
	if err != nil {
		return Result{}, fmt.Errorf("lambda leaf search: %w", err)
	}
	gatherSurface := lambdaGatherContextSurfaceMetadata(searchResult, task.WorkspaceRoot)

	// Extract candidate refs from the composite retrieval result.
	candidateRefs := extractCandidateEvidenceRefs(searchResult)
	candidates := evidenceRefsToPaths(candidateRefs, task.WorkspaceRoot)
	if len(candidates) == 0 {
		candidates = extractCandidatePaths(searchResult, task.WorkspaceRoot)
		candidateRefs = pathsToEvidenceRefs(candidates)
	}
	evidenceRefs := extractEvidenceRefs(searchResult)
	evidenceRefs = uniqueStringsRLM(append(evidenceRefs, candidateRefs...))
	if answer, ok := lambdaAnswerFromAnswerSurface(searchResult, candidates); ok {
		result := Result{
			Answer:         answer,
			EvidenceRefs:   evidenceRefs,
			RetrievedPaths: candidates,
			Iterations:     1,
			Subcalls:       1,
			Metadata: map[string]any{
				"lambda_leaf":              true,
				"lambda_answer_surface":    true,
				"search_tool":              searchTool,
				"candidates_found":         len(candidates),
				"candidate_paths":          candidates,
				"answer_paths":             candidates,
				"retrieved_path_source":    "answer_surface_paths",
				"files_loaded":             0,
				"parent_input_tokens":      0,
				"parent_output_tokens":     0,
				"parent_total_tokens":      0,
				"lambda_judge_skipped":     true,
				"lambda_judge_skip_reason": "answer_surface",
			},
		}
		for key, value := range gatherSurface {
			result.Metadata[key] = value
		}
		return result, nil
	}

	// Step 2: Load top candidates deterministically (no LLM).
	var loadedSnippets []string
	for i, ref := range candidateRefs {
		if i >= plan.TauStar {
			break
		}
		loadResult, err := r.Tools.Execute(ctx, "load_evidence_ref", jsonArgs(map[string]any{
			"ref": ref,
		}))
		if err != nil {
			continue
		}
		if snippet := extractTextFromToolResult(loadResult, 2000); snippet != "" {
			loadedSnippets = append(loadedSnippets, fmt.Sprintf("Evidence: %s\n%s", ref, snippet))
		}
	}

	// Step 3: Ask the LLM to judge the evidence (1 call).
	var evidenceBlock string
	if len(loadedSnippets) > 0 {
		evidenceBlock = strings.Join(loadedSnippets, "\n---\n")
	} else {
		evidenceBlock = formatMapAsText(searchResult)
	}

	answer, answerSanitization, judgeUsage, err := r.judgeEvidence(ctx, cfg.LLM, task.Prompt, evidenceBlock)
	if err != nil {
		// Fallback: return the search result summary without LLM judgment.
		result := Result{
			Answer:         formatMapAsText(searchResult),
			EvidenceRefs:   evidenceRefs,
			RetrievedPaths: candidates,
			Iterations:     1,
			Subcalls:       1,
			Metadata: map[string]any{
				"lambda_leaf":           true,
				"lambda_judge_error":    err.Error(),
				"search_tool":           searchTool,
				"candidates_found":      len(candidates),
				"candidate_paths":       candidates,
				"files_loaded":          len(loadedSnippets),
				"retrieved_path_source": "candidate_paths_fallback",
			},
		}
		setParentTokenUsage(result.Metadata, judgeUsage)
		for key, value := range gatherSurface {
			result.Metadata[key] = value
		}
		return result, nil
	}

	retrievedPaths, answerPaths, pathSource := selectLambdaRetrievedPaths(answer, candidates, task.WorkspaceRoot)

	result := Result{
		Answer:         answer,
		EvidenceRefs:   evidenceRefs,
		RetrievedPaths: retrievedPaths,
		Iterations:     1,
		Subcalls:       1 + len(loadedSnippets),
		Metadata: map[string]any{
			"lambda_leaf":           true,
			"search_tool":           searchTool,
			"candidates_found":      len(candidates),
			"candidate_paths":       candidates,
			"answer_paths":          answerPaths,
			"retrieved_path_source": pathSource,
			"files_loaded":          len(loadedSnippets),
		},
	}
	setParentTokenUsage(result.Metadata, judgeUsage)
	if answerSanitization.Changed {
		result.Metadata["output_sanitization"] = answerSanitization
	}
	for key, value := range gatherSurface {
		result.Metadata[key] = value
	}
	return result, nil
}

// split produces k* subproblems by running parallel searches with varied query formulations.
// This is deterministic Go code -- no LLM involved.
func (r LambdaRunner) split(ctx context.Context, task Task, _ Environment, plan LambdaPlan) []Task {
	variants := queryVariants(task.Prompt, plan.KStar)
	tasks := make([]Task, 0, len(variants))
	for _, variant := range variants {
		tasks = append(tasks, Task{
			Prompt:        variant,
			WorkspaceID:   task.WorkspaceID,
			WorkspaceRoot: task.WorkspaceRoot,
			MaxDepth:      task.MaxDepth,
			MaxIterations: 1,
			MaxSubcalls:   1,
			Metadata:      copyTaskMetadata(task.Metadata),
		})
	}
	return tasks
}

// reduce merges partial results using the composition operator.
func (r LambdaRunner) reduce(ctx context.Context, task Task, plan LambdaPlan, partials []Result) (Result, error) {
	cfg := r.Config.Defaults()

	var answer string
	var evidence []string
	var paths []string
	var candidatePaths []string
	var gatherSelectedPaths []string
	var gatherAnswerSeedPaths []string
	var gatherPathSetMust []string
	var gatherCertificateStatuses []string
	var totalIterations int
	var totalSubcalls int
	var inputTokens int
	var outputTokens int
	var totalTokens int
	var answerSanitization OutputSanitization

	for _, p := range partials {
		evidence = append(evidence, p.EvidenceRefs...)
		paths = append(paths, p.RetrievedPaths...)
		candidatePaths = append(candidatePaths, stringSliceFromAny(p.Metadata["candidate_paths"])...)
		gatherSelectedPaths = append(gatherSelectedPaths, stringSliceFromAny(p.Metadata["gather_context_selected_paths"])...)
		gatherAnswerSeedPaths = append(gatherAnswerSeedPaths, stringSliceFromAny(p.Metadata["gather_context_answer_seed_paths"])...)
		gatherPathSetMust = append(gatherPathSetMust, stringSliceFromAny(p.Metadata["gather_context_path_set_must"])...)
		gatherCertificateStatuses = append(gatherCertificateStatuses, stringSliceFromAny(p.Metadata["gather_context_certificate_statuses"])...)
		totalIterations += p.Iterations
		totalSubcalls += p.Subcalls
		inputTokens += intFromAny(p.Metadata["parent_input_tokens"])
		outputTokens += intFromAny(p.Metadata["parent_output_tokens"])
		totalTokens += intFromAny(p.Metadata["parent_total_tokens"])
	}
	evidence = uniqueStringsRLM(evidence)
	paths = uniqueStringsRLM(paths)
	candidatePaths = uniqueStringsRLM(candidatePaths)
	if len(candidatePaths) == 0 {
		candidatePaths = paths
	}

	switch plan.ComposeOp {
	case ComposeUnion:
		answer = composeUnion(partials)

	case ComposeIntersection:
		answer, _ = composeIntersection(partials, paths)

	case ComposeChronological:
		answer = composeChronological(partials)

	case ComposeRerank:
		// 1 LLM call to re-rank all collected evidence.
		synthesized, sanitization, usage, err := r.judgeEvidence(ctx, cfg.LLM, task.Prompt,
			formatPartialsAsEvidence(partials))
		if err != nil {
			answer = composeUnion(partials)
		} else {
			answer = synthesized
			answerSanitization = sanitization
			inputTokens += usage.InputTokens
			outputTokens += usage.OutputTokens
			totalTokens += usage.TotalTokens
		}

	case ComposeSynthesize:
		// 1 LLM call to synthesize partial answers.
		synthesized, sanitization, usage, err := r.synthesizePartials(ctx, cfg.LLM, task.Prompt, partials)
		if err != nil {
			answer = composeUnion(partials)
		} else {
			answer = synthesized
			answerSanitization = sanitization
			inputTokens += usage.InputTokens
			outputTokens += usage.OutputTokens
			totalTokens += usage.TotalTokens
		}

	default:
		answer = composeUnion(partials)
	}
	finalPaths, answerPaths, pathSource := selectLambdaRetrievedPaths(answer, candidatePaths, task.WorkspaceRoot)

	result := Result{
		Answer:         answer,
		EvidenceRefs:   evidence,
		RetrievedPaths: finalPaths,
		Iterations:     totalIterations,
		Subcalls:       totalSubcalls,
		Metadata: map[string]any{
			"lambda_reduce":         true,
			"compose_op":            string(plan.ComposeOp),
			"partial_count":         len(partials),
			"total_iterations":      totalIterations,
			"candidate_path_count":  len(candidatePaths),
			"answer_paths":          answerPaths,
			"retrieved_path_source": pathSource,
			"parent_input_tokens":   inputTokens,
			"parent_output_tokens":  outputTokens,
			"parent_total_tokens":   totalTokens,
		},
	}
	if answerSanitization.Changed {
		result.Metadata["output_sanitization"] = answerSanitization
	}
	if gatherSelectedPaths = uniqueStringsRLM(gatherSelectedPaths); len(gatherSelectedPaths) > 0 {
		result.Metadata["gather_context_selected_paths"] = gatherSelectedPaths
	}
	if gatherAnswerSeedPaths = uniqueStringsRLM(gatherAnswerSeedPaths); len(gatherAnswerSeedPaths) > 0 {
		result.Metadata["gather_context_answer_seed_paths"] = gatherAnswerSeedPaths
	}
	if gatherPathSetMust = uniqueStringsRLM(gatherPathSetMust); len(gatherPathSetMust) > 0 {
		result.Metadata["gather_context_path_set_must"] = gatherPathSetMust
	}
	if gatherCertificateStatuses = uniqueStringsRLM(gatherCertificateStatuses); len(gatherCertificateStatuses) > 0 {
		result.Metadata["gather_context_certificate_statuses"] = gatherCertificateStatuses
	}
	return result, nil
}

// judgeEvidence asks the LLM to answer a question given collected evidence (1 call).
func (r LambdaRunner) judgeEvidence(ctx context.Context, cfg LLMConfig, query, evidence string) (string, OutputSanitization, engine.TokenUsage, error) {
	llmCfg := lambdaLLMChatConfig(cfg)
	llmCfg.MaxIterations = 1

	prompt := fmt.Sprintf(`Based on the following evidence, answer the question concisely.
Cite specific file paths when evidence supports your answer.
If the evidence is insufficient, say so explicitly.

Question: %s

Evidence:
%s

Answer:`, query, truncateRLMText(evidence, 8000))
	systemPrompt := "You are an evidence-based reasoning assistant. Answer only from the provided evidence. Be concise."
	estimatedUsage := estimateLambdaTokenUsage(systemPrompt+"\n"+prompt, "")

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return "", OutputSanitization{}, estimatedUsage, fmt.Errorf("lambda judge: init LLM: %w", err)
	}

	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(prompt)},
	})
	if err != nil {
		return "", OutputSanitization{}, fillMissingLambdaUsage(output.Tokens, estimatedUsage, output.AssistantText), fmt.Errorf("lambda judge: LLM call: %w", err)
	}
	answer, sanitization := SanitizeOutputText(output.AssistantText)
	usage := fillMissingLambdaUsage(output.Tokens, estimatedUsage, answer)
	if answer == "" {
		return "", sanitization, usage, fmt.Errorf("lambda judge: empty assistant response after sanitization")
	}
	return answer, sanitization, usage, nil
}

// synthesizePartials asks the LLM to merge multiple partial answers (1 call).
func (r LambdaRunner) synthesizePartials(ctx context.Context, cfg LLMConfig, query string, partials []Result) (string, OutputSanitization, engine.TokenUsage, error) {
	llmCfg := lambdaLLMChatConfig(cfg)
	llmCfg.MaxIterations = 1

	var partialTexts []string
	for i, p := range partials {
		partialTexts = append(partialTexts, fmt.Sprintf("Partial %d:\n%s", i+1, truncateRLMText(p.Answer, 2000)))
	}

	prompt := fmt.Sprintf(`Synthesize these partial findings into one complete, accurate answer.

Question: %s

%s

Synthesized answer:`, query, strings.Join(partialTexts, "\n\n"))
	systemPrompt := "Synthesize multiple partial findings into one coherent answer. Preserve all key facts."
	estimatedUsage := estimateLambdaTokenUsage(systemPrompt+"\n"+prompt, "")

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return "", OutputSanitization{}, estimatedUsage, fmt.Errorf("lambda synthesize: init LLM: %w", err)
	}

	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(prompt)},
	})
	if err != nil {
		return "", OutputSanitization{}, fillMissingLambdaUsage(output.Tokens, estimatedUsage, output.AssistantText), fmt.Errorf("lambda synthesize: LLM call: %w", err)
	}
	answer, sanitization := SanitizeOutputText(output.AssistantText)
	usage := fillMissingLambdaUsage(output.Tokens, estimatedUsage, answer)
	if answer == "" {
		return "", sanitization, usage, fmt.Errorf("lambda synthesize: empty assistant response after sanitization")
	}
	return answer, sanitization, usage, nil
}

// --- Deterministic composition helpers ---

func composeUnion(partials []Result) string {
	var parts []string
	for _, p := range partials {
		if strings.TrimSpace(p.Answer) != "" {
			parts = append(parts, p.Answer)
		}
	}
	return strings.Join(parts, "\n\n")
}

func composeIntersection(partials []Result, paths []string) (string, []string) {
	// Keep only paths that appear in 2+ partials.
	pathCount := make(map[string]int)
	for _, p := range partials {
		seen := make(map[string]struct{})
		for _, path := range p.RetrievedPaths {
			if _, ok := seen[path]; !ok {
				pathCount[path]++
				seen[path] = struct{}{}
			}
		}
	}
	var shared []string
	for path, count := range pathCount {
		if count >= 2 {
			shared = append(shared, path)
		}
	}
	sort.Strings(shared)

	var parts []string
	if len(shared) > 0 {
		parts = append(parts, "Cross-confirmed paths: "+strings.Join(shared, ", "))
	}
	for _, p := range partials {
		if strings.TrimSpace(p.Answer) != "" {
			parts = append(parts, p.Answer)
		}
	}
	return strings.Join(parts, "\n\n"), shared
}

func composeChronological(partials []Result) string {
	// Sort partials by any timestamp metadata, then concatenate.
	sorted := make([]Result, len(partials))
	copy(sorted, partials)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, stringFromAny(sorted[i].Metadata["timestamp"]))
		tj, _ := time.Parse(time.RFC3339, stringFromAny(sorted[j].Metadata["timestamp"]))
		return ti.Before(tj)
	})
	var parts []string
	for _, p := range sorted {
		if strings.TrimSpace(p.Answer) != "" {
			parts = append(parts, p.Answer)
		}
	}
	return strings.Join(parts, "\n\n")
}

// --- Helpers ---

func formatPartialsAsEvidence(partials []Result) string {
	var parts []string
	for i, p := range partials {
		var pathsStr string
		if len(p.RetrievedPaths) > 0 {
			pathsStr = fmt.Sprintf("\nRetrieved paths: %s", strings.Join(p.RetrievedPaths, ", "))
		}
		parts = append(parts, fmt.Sprintf("--- Finding %d ---%s\n%s",
			i+1, pathsStr, truncateRLMText(p.Answer, 3000)))
	}
	return strings.Join(parts, "\n\n")
}

func extractCandidatePaths(result map[string]any, workspaceRoot string) []string {
	var paths []string
	collectPathsRecursive(result, workspaceRoot, &paths)
	if len(paths) == 0 {
		var decoded any
		if body, err := json.Marshal(result); err == nil && json.Unmarshal(body, &decoded) == nil {
			collectPathsRecursive(decoded, workspaceRoot, &paths)
		}
	}
	return uniqueStringsRLM(paths)
}

func extractCandidateEvidenceRefs(result map[string]any) []string {
	var refs []string
	collectEvidenceRefsRecursive(result, &refs)
	return uniqueStringsRLM(refs)
}

func collectEvidenceRefsRecursive(value any, out *[]string) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if (key == "ref" || key == "load_ref") && child != nil {
				if ref := strings.TrimSpace(fmt.Sprint(child)); ref != "" {
					*out = append(*out, ref)
				}
			}
			if key == "load_refs" && child != nil {
				for _, ref := range stringsFromAny(child) {
					if ref = strings.TrimSpace(ref); ref != "" {
						*out = append(*out, ref)
					}
				}
			}
			collectEvidenceRefsRecursive(child, out)
		}
	case []any:
		for _, child := range v {
			collectEvidenceRefsRecursive(child, out)
		}
	}
}

func evidenceRefsToPaths(refs []string, workspaceRoot string) []string {
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		if normalized := normalizeRetrievedPath(ref, workspaceRoot); normalized != "" {
			paths = append(paths, normalized)
		}
	}
	return uniqueStringsRLM(paths)
}

func pathsToEvidenceRefs(paths []string) []string {
	refs := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" {
			refs = append(refs, "path:"+path)
		}
	}
	return refs
}

func extractEvidenceRefs(result map[string]any) []string {
	if refs, ok := result["evidence_refs"].([]string); ok {
		return refs
	}
	if refs, ok := result["evidence_refs"].([]any); ok {
		var out []string
		for _, r := range refs {
			if s, ok := r.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func lambdaSearchArgs(task Task, plan LambdaPlan) map[string]any {
	if payload, ok := task.Metadata["gather_context_payload"].(map[string]any); ok && len(payload) > 0 {
		args := copyMapAny(payload)
		if strings.TrimSpace(fmt.Sprint(args["query"])) == "" {
			args["query"] = task.Prompt
		}
		if _, ok := args["limit"]; !ok && plan.TauStar > 0 {
			args["limit"] = plan.TauStar
		}
		if _, ok := args["response_mode"]; !ok {
			args["response_mode"] = "answer_surface"
		}
		if _, ok := args["budget"]; !ok && plan.TauStar > 0 {
			args["budget"] = map[string]any{"max_candidates": plan.TauStar}
		}
		return args
	}
	args := map[string]any{
		"query":         task.Prompt,
		"response_mode": "answer_surface",
		"limit":         plan.TauStar,
		"budget":        map[string]any{"max_candidates": plan.TauStar},
	}
	if taskType := lambdaGatherTaskType(plan.TaskType); taskType != "" {
		args["task_type"] = taskType
	}
	return args
}

func explicitLambdaTaskType(task Task) (TaskType, string) {
	payload, ok := task.Metadata["gather_context_payload"].(map[string]any)
	if !ok {
		return "", ""
	}
	switch strings.TrimSpace(fmt.Sprint(payload["task_type"])) {
	case "file_locate", "symbol_inspect", "registration_trace":
		return TaskTypeCodeLocate, "gather_context_payload"
	case "execution_trace", "change_impact":
		return TaskTypeCodeUnderstand, "gather_context_payload"
	case "memory_recall":
		return TaskTypeMemoryRecall, "gather_context_payload"
	case "evidence_audit":
		return TaskTypeEvidenceAudit, "gather_context_payload"
	default:
		return "", ""
	}
}

func lambdaGatherTaskType(taskType TaskType) string {
	switch taskType {
	case TaskTypeCodeLocate:
		return "file_locate"
	case TaskTypeCodeUnderstand:
		return "execution_trace"
	case TaskTypeMemoryRecall:
		return "memory_recall"
	case TaskTypeEvidenceAudit:
		return "evidence_audit"
	default:
		return ""
	}
}

func lambdaGatherContextSurfaceMetadata(payload map[string]any, workspaceRoot string) map[string]any {
	if payload == nil {
		return nil
	}
	out := map[string]any{}
	if selectedPaths := pathsFromSelectedPathItems(payload["selected_paths"], workspaceRoot); len(selectedPaths) > 0 {
		out["gather_context_selected_paths"] = uniqueStringsRLM(selectedPaths)
	}
	if answerSeedPaths := pathsFromNestedPathList(payload, []string{"answer_seed", "paths"}, workspaceRoot); len(answerSeedPaths) > 0 {
		out["gather_context_answer_seed_paths"] = uniqueStringsRLM(answerSeedPaths)
	}
	if pathSetMust := pathsFromNestedPathItems(payload, []string{"path_set", "must"}, workspaceRoot); len(pathSetMust) > 0 {
		out["gather_context_path_set_must"] = uniqueStringsRLM(pathSetMust)
	}
	if status := stringFromNestedMap(payload, []string{"certificate", "status"}); status != "" {
		out["gather_context_certificate_statuses"] = []string{status}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func lambdaAnswerFromAnswerSurface(payload map[string]any, candidates []string) (string, bool) {
	if strings.TrimSpace(fmt.Sprint(payload["schema_version"])) != "context_answer_surface/v2" {
		return "", false
	}
	if !boolValue(payload["answerable"]) {
		return "", false
	}
	if status := strings.TrimSpace(stringFromNestedMap(payload, []string{"certificate", "status"})); strings.EqualFold(status, "failed") {
		return "", false
	}
	if requiredOK, ok := optionalBoolValue(nestedMapValue(payload, []string{"certificate", "required_evidence_ok"})); ok && !requiredOK {
		return "", false
	}
	paths := pathsFromNestedPathList(payload, []string{"answer_seed", "paths"}, "")
	if len(paths) == 0 {
		paths = candidates
	}
	if len(paths) == 0 {
		return "", false
	}
	facts := stringsFromAny(nestedMapValue(payload, []string{"answer_seed", "facts"}))
	out := map[string]any{
		"summary":   firstNonEmptyLambda(stringFromAny(payload["summary"]), stringFromNestedMap(payload, []string{"answer_seed", "summary"}), "Lambda retrieval returned deterministic repo paths from gather_context."),
		"paths":     paths,
		"symbols":   []string{},
		"facts":     facts,
		"rationale": "Copied from gather_context answer_surface/v2 answer_seed paths.",
	}
	body, err := json.Marshal(out)
	if err != nil {
		return "", false
	}
	return string(body), true
}

func boolValue(value any) bool {
	got, ok := optionalBoolValue(value)
	return ok && got
}

func optionalBoolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "1":
			return true, true
		case "false", "no", "0":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func firstNonEmptyLambda(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func copyTaskMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if m, ok := value.(map[string]any); ok {
			out[key] = copyMapAny(m)
			continue
		}
		out[key] = value
	}
	return out
}

func copyMapAny(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func setParentTokenUsage(metadata map[string]any, usage engine.TokenUsage) {
	if metadata == nil {
		return
	}
	metadata["parent_input_tokens"] = usage.InputTokens
	metadata["parent_output_tokens"] = usage.OutputTokens
	metadata["parent_total_tokens"] = usage.TotalTokens
}

func addParentTokenUsage(metadata map[string]any, usage engine.TokenUsage) {
	if metadata == nil {
		return
	}
	metadata["parent_input_tokens"] = intFromAny(metadata["parent_input_tokens"]) + usage.InputTokens
	metadata["parent_output_tokens"] = intFromAny(metadata["parent_output_tokens"]) + usage.OutputTokens
	total := intFromAny(metadata["parent_total_tokens"]) + usage.TotalTokens
	if total == 0 {
		total = intFromAny(metadata["parent_input_tokens"]) + intFromAny(metadata["parent_output_tokens"])
	}
	metadata["parent_total_tokens"] = total
}

func estimateLambdaTokenUsage(inputText string, outputText string) engine.TokenUsage {
	usage := engine.TokenUsage{
		InputTokens:  estimateLambdaTokens(inputText),
		OutputTokens: estimateLambdaTokens(outputText),
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	return usage
}

func fillMissingLambdaUsage(actual engine.TokenUsage, estimated engine.TokenUsage, outputText string) engine.TokenUsage {
	if actual.InputTokens == 0 {
		actual.InputTokens = estimated.InputTokens
	}
	if actual.OutputTokens == 0 {
		actual.OutputTokens = estimateLambdaTokens(outputText)
	}
	if actual.TotalTokens == 0 {
		actual.TotalTokens = actual.InputTokens + actual.OutputTokens
	}
	return actual
}

func estimateLambdaTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	return maxInt(1, (len(text)+3)/4)
}

func extractTextFromToolResult(result map[string]any, maxLen int) string {
	// Try common field names from the tool result shape.
	for _, key := range []string{"content", "text", "snippet", "body", "output"} {
		if v, ok := result[key]; ok {
			if s, ok := v.(string); ok {
				return truncateRLMText(s, maxLen)
			}
		}
	}
	// Fallback: serialize the whole result.
	return truncateRLMText(formatMapAsText(result), maxLen)
}

func formatMapAsText(m map[string]any) string {
	if m == nil {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Sprintf("%v", m)
	}
	return string(b)
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// extractAnswerPaths pulls file paths from the LLM answer text using backtick-wrapped
// paths (e.g. `internal/rlm/runner.go`) and code-fenced paths, plus bare paths on
// lines starting with list markers.
var answerPathRE = regexp.MustCompile(`(?:^|[\s"` + "`" + `(]|\*)([A-Za-z0-9_./-]+\.(?:` + answerPathExtensionPattern + `))(?:[` + "`" + `\s)"',:;]|$)`)

func extractAnswerPaths(answer, workspaceRoot string) []string {
	matches := answerPathRE.FindAllStringSubmatch(answer, -1)
	var paths []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		p := normalizeRetrievedPath(m[1], workspaceRoot)
		if p != "" {
			paths = append(paths, p)
		}
	}
	return uniqueStringsRLM(paths)
}

func selectLambdaRetrievedPaths(answer string, candidatePaths []string, workspaceRoot string) ([]string, []string, string) {
	answerPaths := extractAnswerPaths(answer, workspaceRoot)
	if len(answerPaths) == 0 {
		return uniqueStringsRLM(candidatePaths), nil, "candidate_paths_fallback"
	}
	return answerPaths, answerPaths, "answer_paths"
}

func stringSliceFromAny(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
