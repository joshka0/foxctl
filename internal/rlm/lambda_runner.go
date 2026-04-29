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
	taskType, classifyErr := classifyTask(ctx, cfg.LLM, task.Prompt)
	if classifyErr != nil {
		taskType = TaskTypeGeneral
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
	result.Metadata["lambda_k_star"] = plan.KStar
	result.Metadata["lambda_tau_star"] = plan.TauStar
	result.Metadata["lambda_depth"] = plan.Depth
	result.Metadata["lambda_cost_estimate"] = plan.CostEstimate
	result.Metadata["lambda_n"] = plan.N
	result.Metadata["lambda_compose_op"] = string(plan.ComposeOp)
	result.Metadata["plan_mode"] = string(PlanModeLambda)
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
	searchResult, err := r.Tools.Execute(ctx, searchTool, jsonArgs(map[string]any{
		"query":  task.Prompt,
		"limit":  plan.TauStar,
		"budget": map[string]any{"max_candidates": plan.TauStar},
	}))
	if err != nil {
		return Result{}, fmt.Errorf("lambda leaf search: %w", err)
	}

	// Extract candidate refs from the composite retrieval result.
	candidateRefs := extractCandidateEvidenceRefs(searchResult)
	candidates := evidenceRefsToPaths(candidateRefs, task.WorkspaceRoot)
	if len(candidates) == 0 {
		candidates = extractCandidatePaths(searchResult, task.WorkspaceRoot)
		candidateRefs = pathsToEvidenceRefs(candidates)
	}
	evidenceRefs := extractEvidenceRefs(searchResult)
	evidenceRefs = uniqueStringsRLM(append(evidenceRefs, candidateRefs...))

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

	answer, answerSanitization, err := r.judgeEvidence(ctx, cfg.LLM, task.Prompt, evidenceBlock)
	if err != nil {
		// Fallback: return the search result summary without LLM judgment.
		return Result{
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
		}, nil
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
	if answerSanitization.Changed {
		result.Metadata["output_sanitization"] = answerSanitization
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
	var totalIterations int
	var totalSubcalls int
	var answerSanitization OutputSanitization

	for _, p := range partials {
		evidence = append(evidence, p.EvidenceRefs...)
		paths = append(paths, p.RetrievedPaths...)
		candidatePaths = append(candidatePaths, stringSliceFromAny(p.Metadata["candidate_paths"])...)
		totalIterations += p.Iterations
		totalSubcalls += p.Subcalls
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
		synthesized, sanitization, err := r.judgeEvidence(ctx, cfg.LLM, task.Prompt,
			formatPartialsAsEvidence(partials))
		if err != nil {
			answer = composeUnion(partials)
		} else {
			answer = synthesized
			answerSanitization = sanitization
		}

	case ComposeSynthesize:
		// 1 LLM call to synthesize partial answers.
		synthesized, sanitization, err := r.synthesizePartials(ctx, cfg.LLM, task.Prompt, partials)
		if err != nil {
			answer = composeUnion(partials)
		} else {
			answer = synthesized
			answerSanitization = sanitization
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
		},
	}
	if answerSanitization.Changed {
		result.Metadata["output_sanitization"] = answerSanitization
	}
	return result, nil
}

// judgeEvidence asks the LLM to answer a question given collected evidence (1 call).
func (r LambdaRunner) judgeEvidence(ctx context.Context, cfg LLMConfig, query, evidence string) (string, OutputSanitization, error) {
	llmCfg := lambdaLLMChatConfig(cfg)
	llmCfg.MaxIterations = 1

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return "", OutputSanitization{}, fmt.Errorf("lambda judge: init LLM: %w", err)
	}

	prompt := fmt.Sprintf(`Based on the following evidence, answer the question concisely.
Cite specific file paths when evidence supports your answer.
If the evidence is insufficient, say so explicitly.

Question: %s

Evidence:
%s

Answer:`, query, truncateRLMText(evidence, 8000))

	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: "You are an evidence-based reasoning assistant. Answer only from the provided evidence. Be concise.",
		Messages:     []engine.Message{engine.NewUserMessage(prompt)},
	})
	if err != nil {
		return "", OutputSanitization{}, fmt.Errorf("lambda judge: LLM call: %w", err)
	}
	answer, sanitization := SanitizeOutputText(output.AssistantText)
	if answer == "" {
		return "", sanitization, fmt.Errorf("lambda judge: empty assistant response after sanitization")
	}
	return answer, sanitization, nil
}

// synthesizePartials asks the LLM to merge multiple partial answers (1 call).
func (r LambdaRunner) synthesizePartials(ctx context.Context, cfg LLMConfig, query string, partials []Result) (string, OutputSanitization, error) {
	llmCfg := lambdaLLMChatConfig(cfg)
	llmCfg.MaxIterations = 1

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return "", OutputSanitization{}, fmt.Errorf("lambda synthesize: init LLM: %w", err)
	}

	var partialTexts []string
	for i, p := range partials {
		partialTexts = append(partialTexts, fmt.Sprintf("Partial %d:\n%s", i+1, truncateRLMText(p.Answer, 2000)))
	}

	prompt := fmt.Sprintf(`Synthesize these partial findings into one complete, accurate answer.

Question: %s

%s

Synthesized answer:`, query, strings.Join(partialTexts, "\n\n"))

	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: "Synthesize multiple partial findings into one coherent answer. Preserve all key facts.",
		Messages:     []engine.Message{engine.NewUserMessage(prompt)},
	})
	if err != nil {
		return "", OutputSanitization{}, fmt.Errorf("lambda synthesize: LLM call: %w", err)
	}
	answer, sanitization := SanitizeOutputText(output.AssistantText)
	if answer == "" {
		return "", sanitization, fmt.Errorf("lambda synthesize: empty assistant response after sanitization")
	}
	return answer, sanitization, nil
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
			if key == "ref" && child != nil {
				if ref := strings.TrimSpace(fmt.Sprint(child)); ref != "" {
					*out = append(*out, ref)
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
