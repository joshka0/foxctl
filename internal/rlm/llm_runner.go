package rlm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// LLMToolExecutor adapts the RLM tool surface to the engine.ToolExecutor contract.
type LLMToolExecutor struct {
	adapter ToolExecutor
	tools   []Tool
}

func NewLLMToolExecutor(adapter ToolExecutor, tools []Tool) *LLMToolExecutor {
	return &LLMToolExecutor{adapter: newAllowlistedToolExecutor(adapter, tools), tools: tools}
}

func (e *LLMToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if e == nil || e.adapter == nil {
		return "", fmt.Errorf("rlm llm tool executor is not configured")
	}
	result, err := e.adapter.Execute(ctx, name, args)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (e *LLMToolExecutor) List() []engine.ToolDef {
	if e == nil {
		return nil
	}
	defs := make([]engine.ToolDef, 0, len(e.tools))
	for _, tool := range e.tools {
		schema := tool.Parameters
		if len(schema) == 0 {
			schema, _ = json.Marshal(map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			})
		}
		defs = append(defs, engine.ToolDef{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  schema,
		})
	}
	return defs
}

// LLMConfig configures the model-backed experimental RLM runner.
type LLMConfig struct {
	Provider       string
	APIKey         string
	BaseURL        string
	AuthMode       string
	AuthHeader     string
	AuthPrefix     string
	Model          string
	Timeout        time.Duration
	MaxTokens      int
	Temperature    float64
	MaxIterations  int
	RequireToolUse bool
	QwenNoThink    bool
	ExtraBody      map[string]any
	RouteProfile   RouteProfile
	PlanMode       PlanMode
	ToolProfile    string
}

// LLMRunner uses the existing engine.LLMChatEngine as an experimental read-only RLM backend.
type LLMRunner struct {
	Config        LLMConfig
	Tools         ToolExecutor
	FeedbackStore contextengine.RetrievalFeedbackEffectStore // optional; nil = no feedback emission
}

func (r LLMRunner) Run(ctx context.Context, task Task, env Environment) (Result, error) {
	if err := ValidateRunRequest(task, env); err != nil {
		return Result{}, err
	}
	if r.Tools == nil {
		return Result{}, fmt.Errorf("rlm llm runner requires tool adapter")
	}
	spec, err := ResolveRunSpec(ResolveRunSpecInput{
		Prompt:               task.Prompt,
		RequestedRoute:       r.Config.RouteProfile,
		RequestedPlanMode:    r.Config.PlanMode,
		RequestedToolProfile: r.Config.ToolProfile,
		AvailableTools:       env.Tools,
	})
	if err != nil {
		return Result{}, err
	}
	effectiveEnv := env
	effectiveEnv.Tools = append([]Tool(nil), spec.ToolPolicy.Tools...)
	if spec.PlanMode == PlanModeStaged && len(spec.Plan.Phases) > 0 {
		return r.runStaged(ctx, task, effectiveEnv, spec)
	}
	return r.runSinglePass(ctx, task, effectiveEnv, runPassConfig{
		Prompt:         task.Prompt,
		Tools:          effectiveEnv.Tools,
		SystemPrompt:   BuildLLMSystemPrompt(effectiveEnv, task),
		RequireToolUse: r.Config.RequireToolUse,
		MaxIterations:  task.MaxIterations,
		Metadata: map[string]any{
			"effective_route_profile": spec.RouteProfile,
			"effective_plan_mode":     spec.PlanMode,
			"tool_policy_profile":     spec.ToolPolicy.Profile,
			"allowed_tools":           append([]string(nil), spec.ToolPolicy.AllowedTools...),
		},
	})
}

type runPassConfig struct {
	Prompt         string
	Tools          []Tool
	SystemPrompt   string
	RequireToolUse bool
	MaxIterations  int
	Metadata       map[string]any
}

func (r LLMRunner) runSinglePass(ctx context.Context, task Task, env Environment, pass runPassConfig) (Result, error) {
	if pass.RequireToolUse && len(pass.Tools) == 0 {
		return Result{}, fmt.Errorf("rlm llm runner: require-tool-use is enabled but no tools are available")
	}
	llmCfg := engine.DefaultLLMChatConfig()
	if strings.TrimSpace(r.Config.Provider) != "" {
		llmCfg.Provider = strings.TrimSpace(r.Config.Provider)
	}
	if strings.TrimSpace(r.Config.APIKey) != "" {
		llmCfg.APIKey = strings.TrimSpace(r.Config.APIKey)
	}
	if strings.TrimSpace(r.Config.BaseURL) != "" {
		llmCfg.BaseURL = strings.TrimSpace(r.Config.BaseURL)
	}
	if strings.TrimSpace(r.Config.AuthMode) != "" {
		llmCfg.AuthMode = strings.TrimSpace(r.Config.AuthMode)
	}
	if strings.TrimSpace(r.Config.AuthHeader) != "" {
		llmCfg.AuthHeader = strings.TrimSpace(r.Config.AuthHeader)
	}
	if r.Config.AuthPrefix != "" {
		llmCfg.AuthPrefix = r.Config.AuthPrefix
	}
	if strings.TrimSpace(r.Config.Model) != "" {
		llmCfg.Model = strings.TrimSpace(r.Config.Model)
	}
	if r.Config.Timeout > 0 {
		llmCfg.Timeout = r.Config.Timeout
	}
	if r.Config.MaxTokens > 0 {
		llmCfg.MaxTokens = r.Config.MaxTokens
	}
	if r.Config.Temperature != 0 {
		llmCfg.Temperature = r.Config.Temperature
	}
	llmCfg.ExtraBody = cloneStringAnyMap(r.Config.ExtraBody)
	if pass.MaxIterations > 0 {
		llmCfg.MaxIterations = pass.MaxIterations
	} else if r.Config.MaxIterations > 0 {
		llmCfg.MaxIterations = r.Config.MaxIterations
	} else if task.MaxIterations > 0 {
		llmCfg.MaxIterations = task.MaxIterations
	}
	if pass.RequireToolUse {
		llmCfg.ToolChoice = json.RawMessage(`"required"`)
	}

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return Result{}, err
	}
	toolExec := NewLLMToolExecutor(r.Tools, pass.Tools)
	llm.SetToolRunner(engine.NewToolRunner(toolExec, nil, engine.ToolRunnerConfig{
		Workspace:   task.WorkspaceRoot,
		WorkspaceID: task.WorkspaceID,
	}))

	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: pass.SystemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(pass.Prompt)},
		Tools:        toolExec.List(),
		Workspace:    task.WorkspaceRoot,
		MaxTokens:    llmCfg.MaxTokens,
		Temperature:  llmCfg.Temperature,
	})
	if err != nil {
		return Result{}, err
	}
	if output.StopReason == engine.StopReasonError {
		return Result{}, fmt.Errorf("rlm llm runner: %s", strings.TrimSpace(output.Error))
	}
	if pass.RequireToolUse && len(output.ToolCalls) == 0 {
		return Result{}, fmt.Errorf("rlm llm runner: model answered without using tools")
	}
	answer, sanitization := SanitizeOutputText(output.AssistantText)
	if answer == "" {
		if output.StopReason != "" && output.StopReason != engine.StopReasonEndTurn {
			detail := strings.TrimSpace(output.Error)
			if detail != "" {
				return Result{}, fmt.Errorf("rlm llm runner: %s before assistant response: %s", output.StopReason, detail)
			}
			return Result{}, fmt.Errorf("rlm llm runner: %s before assistant response", output.StopReason)
		}
		return Result{}, fmt.Errorf("rlm llm runner: empty assistant response")
	}
	surfacedEvidence := collectSurfacedToolEvidenceRefs(output.ToolCalls, output.ToolResults)
	answerUsedEvidence := collectAnswerUsedEvidenceRefs(answer, surfacedEvidence)
	acceptedLedgerEvidence := collectAcceptedLedgerEvidenceRows(output.ToolCalls, output.ToolResults)
	evidence := collectEvidenceRefs(env)
	retrievedPaths := collectRetrievedPaths(output.ToolResults, task.WorkspaceRoot, answer)
	gatherSurface := collectGatherContextSurfaceMetadata(output.ToolCalls, output.ToolResults, task.WorkspaceRoot)
	parentUsage := summarizeParentToolUsage(output.Iterations, "retrieve_code")
	metadata := map[string]any{
		"stop_reason":                 output.StopReason,
		"provider":                    llmCfg.Provider,
		"model":                       llmCfg.Model,
		"tool_calls":                  len(output.ToolCalls),
		"tool_names":                  toolCallNames(output.ToolCalls),
		"llm_error":                   output.Error,
		"require_tool_use":            pass.RequireToolUse,
		"tool_surfaced_evidence_refs": surfacedEvidence,
		"answer_used_evidence_refs":   answerUsedEvidence,
		"accepted_ledger_evidence":    acceptedLedgerEvidence,
		"retrieved_paths":             retrievedPaths,
		"parent_input_tokens":         output.Tokens.InputTokens,
		"parent_output_tokens":        output.Tokens.OutputTokens,
		"parent_total_tokens":         output.Tokens.TotalTokens,
		"parent_iteration_count":      len(output.Iterations),
		"parent_tool_usage":           parentUsage,
	}
	for key, value := range gatherSurface {
		metadata[key] = value
	}
	if sanitization.Changed {
		metadata["output_sanitization"] = sanitization
	}
	for key, value := range pass.Metadata {
		metadata[key] = value
	}
	return Result{
		Answer:         answer,
		EvidenceRefs:   evidence,
		RetrievedPaths: retrievedPaths,
		Iterations:     1,
		Subcalls:       len(output.ToolCalls),
		Metadata:       metadata,
	}, nil
}

func BuildLLMSystemPrompt(env Environment, task Task) string {
	var b strings.Builder
	b.WriteString("You are an experimental read-only recursive reasoning runtime.\n")
	b.WriteString("Use tools to inspect external state before answering. Do not invent unavailable evidence.\n")
	b.WriteString("Prefer repo, scene, vault, and artifact handles already present in the environment.\n")
	b.WriteString("If the prompt is about the current workspace, inspect with at least one tool before writing the final synthesis.\n")
	if guidance := toolSurfaceGuidance(env.Tools); guidance != "" {
		b.WriteString(guidance)
		b.WriteString("\n")
	}
	b.WriteString("Cite exact relative repo file paths you inspected. Do not cite .foxctl or .claude runtime files as repository evidence.\n")
	b.WriteString("When relying on a surfaced non-path evidence handle such as memory_claim:<id>, cite the exact ref string.\n")
	b.WriteString("Return a concise synthesis with supporting evidence.\n")
	if len(env.Tools) > 0 {
		b.WriteString("\nAllowed read-only tools:\n")
		for _, tool := range env.Tools {
			b.WriteString("- ")
			b.WriteString(tool.Name)
			if sig := toolSignature(tool.Parameters); sig != "" {
				b.WriteString(sig)
			}
			if desc := strings.TrimSpace(tool.Description); desc != "" {
				b.WriteString(": ")
				b.WriteString(desc)
			}
			b.WriteString("\n")
		}
	}
	if objective := stringField(env.TopOfMind, "objective"); objective != "" {
		b.WriteString("\nCurrent objective: " + objective + "\n")
	}
	if phase := stringField(env.TopOfMind, "phase"); phase != "" {
		b.WriteString("Current phase: " + phase + "\n")
	}
	if summary := stringField(env.LatestHandoff, "summary"); summary != "" {
		b.WriteString("Latest handoff: " + summary + "\n")
	}
	if len(env.RepoHandles) > 0 {
		b.WriteString("Repo handles: " + strings.Join(shortenRefs(env.RepoHandles, 5), ", ") + "\n")
	}
	if len(env.VaultHandles) > 0 {
		b.WriteString("Vault handles: " + strings.Join(shortenRefs(env.VaultHandles, 5), ", ") + "\n")
	}
	if len(env.SceneHandles) > 0 {
		b.WriteString("Scene handles: " + strings.Join(shortenRefs(env.SceneHandles, 5), ", ") + "\n")
	}
	if len(env.ArtifactHandles) > 0 {
		b.WriteString("Artifact handles: " + strings.Join(shortenRefs(env.ArtifactHandles, 5), ", ") + "\n")
	}
	if task.MaxDepth > 0 {
		b.WriteString(fmt.Sprintf("Max recursive depth available: %d\n", task.MaxDepth))
	}
	return b.String()
}

func (r LLMRunner) runStaged(ctx context.Context, task Task, env Environment, spec RunSpec) (Result, error) {
	plan := spec.Plan
	baseStageEnv := routeStageEnvironment(env, spec.RouteProfile)
	allPaths := make([]string, 0, 16)
	allEvidence := make([]string, 0, 16)
	allSurfacedEvidence := make([]string, 0, 16)
	allAcceptedLedgerEvidence := make([]string, 0, 16)
	phaseNotes := make([]string, 0, len(plan.Phases))
	phaseMeta := make([]map[string]any, 0, len(plan.Phases))
	totalToolCalls := 0

	for _, phase := range plan.Phases {
		rankedPaths := rerankCandidatePaths(task.Prompt, allPaths)
		phaseEnv := stagedPhaseEnvironment(baseStageEnv, rankedPaths, phase.Name == "discovery")
		phaseTools := filterToolsByNames(phaseEnv.Tools, phase.AllowedTools)
		if len(phaseTools) == 0 && phase.RequireToolUse {
			return Result{}, fmt.Errorf("rlm llm runner: %s phase requires tool use but no tools are available", phase.Name)
		}
		promptEnv := phaseEnv
		promptEnv.Tools = append([]Tool(nil), phaseTools...)
		phasePrompt := buildPhasePrompt(task.Prompt, phase, rankedPaths, phaseNotes)
		phaseSystemPrompt := buildPhaseSystemPrompt(BuildLLMSystemPrompt(promptEnv, task), task.Prompt, phase, rankedPaths, phaseNotes)
		phaseResult, err := r.runSinglePass(ctx, task, baseStageEnv, runPassConfig{
			Prompt:         phasePrompt,
			Tools:          phaseTools,
			SystemPrompt:   phaseSystemPrompt,
			RequireToolUse: phase.RequireToolUse,
			MaxIterations:  phase.MaxIterations,
			Metadata: map[string]any{
				"effective_route_profile": spec.RouteProfile,
				"effective_plan_mode":     spec.PlanMode,
				"tool_policy_profile":     spec.ToolPolicy.Profile,
				"allowed_tools":           append([]string(nil), spec.ToolPolicy.AllowedTools...),
				"phase_name":              phase.Name,
			},
		})
		if err != nil {
			return Result{}, fmt.Errorf("rlm llm runner: %s phase failed: %w", phase.Name, err)
		}
		toolNames := stringsFromAnySlice(phaseResult.Metadata["tool_names"])
		if len(phase.RequireOneOf) > 0 && !containsAnyToolName(toolNames, phase.RequireOneOf) {
			if containsString(phase.RequireOneOf, "evidence_ledger") {
				if ledgerResult, ok := r.tryDeterministicEvidenceLedgerPhase(ctx, task, phase, phaseResult, allSurfacedEvidence); ok {
					phaseResult = ledgerResult
					toolNames = stringsFromAnySlice(phaseResult.Metadata["tool_names"])
				}
			}
		}
		if containsString(phase.RequireOneOf, "evidence_ledger") &&
			containsString(toolNames, "evidence_ledger") &&
			len(stringsFromAnySlice(phaseResult.Metadata["accepted_ledger_evidence"])) == 0 {
			if ledgerResult, ok := r.tryDeterministicEvidenceLedgerPhase(ctx, task, phase, phaseResult, allSurfacedEvidence); ok &&
				len(stringsFromAnySlice(ledgerResult.Metadata["accepted_ledger_evidence"])) > 0 {
				phaseResult = ledgerResult
				toolNames = stringsFromAnySlice(phaseResult.Metadata["tool_names"])
			}
		}
		if len(phase.RequireOneOf) > 0 && !containsAnyToolName(toolNames, phase.RequireOneOf) {
			requiredTools := filterToolsByNames(phaseEnv.Tools, phase.RequireOneOf)
			if len(requiredTools) == 0 {
				return Result{}, fmt.Errorf("rlm llm runner: %s phase did not use any required tools", phase.Name)
			}
			retryResult, retryErr := r.runSinglePass(ctx, task, baseStageEnv, runPassConfig{
				Prompt:         phasePrompt + "\n\nRetry this phase now. Only the required tools are available.",
				Tools:          requiredTools,
				SystemPrompt:   phaseSystemPrompt + "\nRequired tool retry: only the required tools are available in this retry.",
				RequireToolUse: phase.RequireToolUse,
				MaxIterations:  maxInt(1, phase.MaxIterations-1),
				Metadata: map[string]any{
					"effective_route_profile": spec.RouteProfile,
					"effective_plan_mode":     spec.PlanMode,
					"tool_policy_profile":     spec.ToolPolicy.Profile,
					"allowed_tools":           append([]string(nil), spec.ToolPolicy.AllowedTools...),
					"phase_name":              phase.Name,
				},
			})
			if retryErr != nil {
				return Result{}, fmt.Errorf("rlm llm runner: %s phase failed: %w", phase.Name, retryErr)
			}
			phaseResult = retryResult
			toolNames = stringsFromAnySlice(phaseResult.Metadata["tool_names"])
			if len(phase.RequireOneOf) > 0 && !containsAnyToolName(toolNames, phase.RequireOneOf) {
				return Result{}, fmt.Errorf("rlm llm runner: %s phase did not use any required tools", phase.Name)
			}
		}
		totalToolCalls += intFromAny(phaseResult.Metadata["tool_calls"])
		allPaths = rerankCandidatePaths(task.Prompt, append(allPaths, phaseResult.RetrievedPaths...))
		allPaths = shortenRefs(allPaths, 8)
		allEvidence = uniqueStringsRLM(append(allEvidence, phaseResult.EvidenceRefs...))
		phaseSurfacedEvidence := stringsFromAnySlice(phaseResult.Metadata["tool_surfaced_evidence_refs"])
		allSurfacedEvidence = uniqueStringsRLM(append(allSurfacedEvidence, phaseSurfacedEvidence...))
		phaseAcceptedLedgerEvidence := stringsFromAnySlice(phaseResult.Metadata["accepted_ledger_evidence"])
		allAcceptedLedgerEvidence = uniqueStringsRLM(append(allAcceptedLedgerEvidence, phaseAcceptedLedgerEvidence...))
		if summary := strings.TrimSpace(phaseResult.Answer); summary != "" {
			phaseNotes = append(phaseNotes, phase.Name+": "+truncateRLMText(summary, 320))
			if len(phaseNotes) > 3 {
				phaseNotes = phaseNotes[len(phaseNotes)-3:]
			}
		}
		phaseMeta = append(phaseMeta, map[string]any{
			"name":                        phase.Name,
			"tool_names":                  toolNames,
			"tool_surfaced_evidence_refs": append([]string(nil), phaseSurfacedEvidence...),
			"answer_used_evidence_refs":   stringsFromAnySlice(phaseResult.Metadata["answer_used_evidence_refs"]),
			"accepted_ledger_evidence":    append([]string(nil), phaseAcceptedLedgerEvidence...),
			"retrieved_paths":             append([]string(nil), phaseResult.RetrievedPaths...),
			"answer":                      phaseResult.Answer,
			"parent_input_tokens":         intFromAny(phaseResult.Metadata["parent_input_tokens"]),
			"parent_output_tokens":        intFromAny(phaseResult.Metadata["parent_output_tokens"]),
			"parent_total_tokens":         intFromAny(phaseResult.Metadata["parent_total_tokens"]),
			"parent_tool_usage":           phaseResult.Metadata["parent_tool_usage"],
		})
	}

	allPaths = rerankCandidatePaths(task.Prompt, allPaths)

	// REQUIRED_DATA fallback (Slice 4): when evidence was surfaced but the
	// ledger accepted nothing, run one bounded re-query with target nouns
	// extracted from the task prompt. This is Quarq's REQUIRED_DATA protocol
	// adapted to foxctl's staged runner. The re-query uses the gather_context
	// tool at a lower (deep) threshold. Capped to one fallback.
	fallbackFired := false
	if len(allAcceptedLedgerEvidence) == 0 && len(allSurfacedEvidence) > 0 && r.Tools != nil {
		fallbackRefs := r.tryRetrievalFallback(ctx, task, baseStageEnv)
		if len(fallbackRefs) > 0 {
			fallbackFired = true
			allSurfacedEvidence = uniqueStringsRLM(append(allSurfacedEvidence, fallbackRefs...))
			allEvidence = uniqueStringsRLM(append(allEvidence, fallbackRefs...))
		}
	}

	finalPrompt := buildSynthesisPrompt(task.Prompt, allPaths, allSurfacedEvidence, allAcceptedLedgerEvidence, phaseNotes)
	finalResult, err := r.runSinglePass(ctx, task, baseStageEnv, runPassConfig{
		Prompt:         finalPrompt,
		Tools:          nil,
		SystemPrompt:   buildSynthesisSystemPrompt(BuildLLMSystemPrompt(baseStageEnv, task), plan, allPaths, phaseNotes),
		RequireToolUse: false,
		MaxIterations:  1,
		Metadata: map[string]any{
			"effective_route_profile": spec.RouteProfile,
			"effective_plan_mode":     spec.PlanMode,
			"tool_policy_profile":     spec.ToolPolicy.Profile,
			"allowed_tools":           append([]string(nil), spec.ToolPolicy.AllowedTools...),
		},
	})
	if err != nil && len(phaseNotes) > 0 {
		finalResult = Result{Answer: phaseNotes[len(phaseNotes)-1]}
	} else if err != nil {
		return Result{}, err
	}
	finalResult.EvidenceRefs = uniqueStringsRLM(append(finalResult.EvidenceRefs, allEvidence...))
	finalResult.RetrievedPaths = uniqueStringsRLM(append(finalResult.RetrievedPaths, allPaths...))
	finalResult.Subcalls = totalToolCalls
	finalResult.Iterations = len(plan.Phases) + 1
	if finalResult.Metadata == nil {
		finalResult.Metadata = map[string]any{}
	}
	finalResult.Metadata["plan_mode"] = spec.PlanMode
	finalResult.Metadata["route_profile"] = spec.RouteProfile
	finalResult.Metadata["tool_policy_profile"] = spec.ToolPolicy.Profile
	finalResult.Metadata["allowed_tools"] = append([]string(nil), spec.ToolPolicy.AllowedTools...)
	finalSurfacedEvidence := uniqueStringsRLM(append(stringsFromAnySlice(finalResult.Metadata["tool_surfaced_evidence_refs"]), allSurfacedEvidence...))
	sort.Strings(finalSurfacedEvidence)
	finalResult.Metadata["tool_surfaced_evidence_refs"] = finalSurfacedEvidence
	finalResult.Metadata["answer_used_evidence_refs"] = collectAnswerUsedEvidenceRefs(finalResult.Answer, finalSurfacedEvidence)
	finalResult.Metadata["accepted_ledger_evidence"] = append([]string(nil), allAcceptedLedgerEvidence...)
	finalResult.Metadata["phase_count"] = len(plan.Phases)
	finalResult.Metadata["phases"] = phaseMeta
	finalResult.Metadata["tool_calls"] = totalToolCalls
	finalResult.Metadata["retrieved_paths"] = finalResult.RetrievedPaths
	finalResult.Metadata["parent_input_tokens_total"] = sumIntMetadata(phaseMeta, "parent_input_tokens")
	finalResult.Metadata["parent_output_tokens_total"] = sumIntMetadata(phaseMeta, "parent_output_tokens")
	finalResult.Metadata["parent_total_tokens_total"] = sumIntMetadata(phaseMeta, "parent_total_tokens")
	finalResult.Metadata["parent_retrieve_code_prompt_delta_total"] = sumNestedIntMetadata(phaseMeta, "parent_tool_usage", "target_tool_prompt_delta_total")
	finalResult.Metadata["parent_retrieve_code_invocations_total"] = sumNestedIntMetadata(phaseMeta, "parent_tool_usage", "target_tool_invocations")
	finalResult.Metadata["parent_retrieve_code_result_token_estimate_total"] = sumNestedIntMetadata(phaseMeta, "parent_tool_usage", "target_tool_result_token_estimate_total")
	// Best-effort: emit AnswerAccepted feedback from accepted ledger evidence so
	// candidate claims promoted by Slice 1a can fire. Errors are logged and
	// swallowed so a store hiccup never breaks the answer path.
	emitAnswerAcceptedFeedback(ctx, r.FeedbackStore, task, finalResult.Metadata["accepted_ledger_evidence"])
	finalResult.Metadata["retrieval_fallback_fired"] = fallbackFired
	return finalResult, nil
}

func (r LLMRunner) tryDeterministicEvidenceLedgerPhase(ctx context.Context, task Task, phase Phase, phaseResult Result, priorSurfacedEvidence []string) (Result, bool) {
	refs := deterministicEvidenceLedgerRefs(priorSurfacedEvidence, stringsFromAnySlice(phaseResult.Metadata["tool_surfaced_evidence_refs"]))
	if len(refs) == 0 {
		return Result{}, false
	}
	args := map[string]any{
		"query":              evidenceLedgerQueryFromTaskPrompt(task.Prompt),
		"refs":               refs,
		"max_refs":           len(refs),
		"max_tokens_per_ref": 1200,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return Result{}, false
	}
	out, err := newAllowlistedToolExecutor(r.Tools, []Tool{{Name: "evidence_ledger", ReadOnly: true}}).Execute(ctx, "evidence_ledger", argsJSON)
	if err != nil {
		return Result{}, false
	}
	content, err := json.Marshal(out)
	if err != nil {
		return Result{}, false
	}
	callID := "deterministic-" + phase.Name + "-evidence-ledger"
	calls := []engine.ToolCall{{
		ID:        callID,
		Name:      "evidence_ledger",
		Arguments: argsJSON,
	}}
	results := []engine.ToolResult{{
		ToolCallID: callID,
		Content:    string(content),
	}}
	surfacedEvidence := collectSurfacedToolEvidenceRefs(calls, results)
	acceptedLedgerEvidence := collectAcceptedLedgerEvidenceRows(calls, results)
	answer := deterministicEvidenceLedgerPhaseAnswer(out)
	return Result{
		Answer:         answer,
		RetrievedPaths: collectRetrievedPaths(results, task.WorkspaceRoot, answer),
		Subcalls:       1,
		Iterations:     1,
		Metadata: map[string]any{
			"stop_reason":                  engine.StopReasonEndTurn,
			"tool_calls":                   1,
			"tool_names":                   []string{"evidence_ledger"},
			"require_tool_use":             true,
			"tool_surfaced_evidence_refs":  surfacedEvidence,
			"answer_used_evidence_refs":    collectAnswerUsedEvidenceRefs(answer, surfacedEvidence),
			"accepted_ledger_evidence":     acceptedLedgerEvidence,
			"retrieved_paths":              collectRetrievedPaths(results, task.WorkspaceRoot, answer),
			"parent_input_tokens":          0,
			"parent_output_tokens":         0,
			"parent_total_tokens":          0,
			"parent_iteration_count":       0,
			"parent_tool_usage":            map[string]any{},
			"deterministic_tool_fallback":  "evidence_ledger",
			"deterministic_fallback_refs":  refs,
			"deterministic_fallback_phase": phase.Name,
		},
	}, true
}

func deterministicEvidenceLedgerRefs(groups ...[]string) []string {
	out := []string{}
	for _, group := range groups {
		for _, ref := range group {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			out = uniqueStringsRLM(append(out, ref))
			if len(out) >= 12 {
				return out
			}
		}
	}
	return out
}

func evidenceLedgerQueryFromTaskPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	lower := strings.ToLower(prompt)
	marker := "question:"
	if idx := strings.LastIndex(lower, marker); idx >= 0 {
		question := strings.TrimSpace(prompt[idx+len(marker):])
		if question != "" {
			return question
		}
	}
	return prompt
}

func deterministicEvidenceLedgerPhaseAnswer(out map[string]any) string {
	accepted := stringsFromAny(out["accepted_refs"])
	rejected := stringsFromAny(out["rejected_refs"])
	missing := stringsFromAny(nestedMapValue(out, []string{"answer_outline", "missing_slots"}))
	var b strings.Builder
	b.WriteString("Evidence ledger built.")
	if boolFromRLMAny(out["needs_fallback"]) {
		b.WriteString(" The ledger is not ready; do not answer from rejected refs without accepted rows.")
	}
	if len(accepted) > 0 {
		b.WriteString(" Accepted refs: ")
		b.WriteString(strings.Join(shortenRefs(accepted, 8), ", "))
		b.WriteString(".")
	}
	if len(rejected) > 0 {
		b.WriteString(" Rejected refs: ")
		b.WriteString(strings.Join(shortenRefs(rejected, 8), ", "))
		b.WriteString(".")
	}
	if len(missing) > 0 {
		b.WriteString(" Missing slots: ")
		b.WriteString(strings.Join(shortenRefs(missing, 8), ", "))
		b.WriteString(".")
	}
	if len(accepted) == 0 && len(rejected) == 0 && len(missing) == 0 {
		b.WriteString(" No accepted or rejected refs were returned.")
	}
	return b.String()
}

func buildPhaseSystemPrompt(base, query string, phase Phase, candidatePaths, phaseNotes []string) string {
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\nCurrent staged phase: ")
	b.WriteString(phase.Name)
	b.WriteString("\nPhase objective: ")
	b.WriteString(phase.Objective)
	b.WriteString("\nAllowed tools in this phase only: ")
	b.WriteString(strings.Join(phase.AllowedTools, ", "))
	b.WriteString("\nDo not answer the overall user question until this phase objective is complete.")
	if len(phase.RequireOneOf) > 0 {
		b.WriteString("\nBefore you finish this phase, you must use at least one of: ")
		b.WriteString(strings.Join(phase.RequireOneOf, ", "))
	}
	if guidance := phaseGuidance(phase.Name); guidance != "" {
		b.WriteString("\nPhase guidance: ")
		b.WriteString(guidance)
	}
	if len(candidatePaths) > 0 {
		b.WriteString("\nCandidate paths from prior phases: ")
		b.WriteString(strings.Join(shortenRefs(candidatePaths, 8), ", "))
	}
	if len(phaseNotes) > 0 {
		b.WriteString("\nPrior phase notes:\n")
		for _, note := range phaseNotes {
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(note))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func buildPhasePrompt(query string, phase Phase, candidatePaths, phaseNotes []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(query))
	b.WriteString("\n\nPhase: ")
	b.WriteString(phase.Name)
	b.WriteString("\nObjective: ")
	b.WriteString(phase.Objective)
	if guidance := phaseGuidance(phase.Name); guidance != "" {
		b.WriteString("\nExecution rule: ")
		b.WriteString(guidance)
	}
	if phase.Name == "inspection" || phase.Name == "verification" {
		if containsString(phase.RequireOneOf, "evidence_ledger") {
			b.WriteString("\nYou must build an evidence_ledger from candidate refs before you finish. If it reports needs_fallback=true, run one targeted gather fallback when a gather tool is available, then rebuild or update the ledger before finalizing this phase.")
		} else if containsString(phase.AllowedTools, "evidence_ledger") {
			b.WriteString("\nPrefer evidence_ledger for count, list, duration, location, or exact personal-memory answers; otherwise aggregate or load at least one candidate evidence reference before you finish.")
		} else if containsString(phase.AllowedTools, "aggregate_evidence_refs") {
			b.WriteString("\nYou must aggregate or load at least one candidate evidence reference before you finish.")
		} else {
			b.WriteString("\nYou must load at least one candidate evidence reference with load_evidence_ref before you finish.")
		}
	}
	if len(candidatePaths) > 0 {
		b.WriteString("\nFocus on these candidate paths if they seem relevant: ")
		b.WriteString(strings.Join(shortenRefs(candidatePaths, 8), ", "))
	}
	if len(phaseNotes) > 0 {
		b.WriteString("\nUse prior findings, but verify them with this phase's tools.")
	}
	return b.String()
}

func toolSurfaceGuidance(tools []Tool) string {
	present := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name != "" {
			present[name] = struct{}{}
		}
	}
	has := func(name string) bool {
		_, ok := present[name]
		return ok
	}

	var guidance []string
	if has("gather_memory_context") {
		guidance = append(guidance, "Use gather_memory_context for explicit long-term memory recall; it applies memory-lane defaults and bounded coverage repair before returning evidence_digest, answer_seed, and path_set refs.")
	}
	if has("plan_context_query") && has("gather_context") {
		guidance = append(guidance, "For ambiguous recall, code, task, or mixed-context questions, call plan_context_query before gather_context and copy its required_evidence, coverage_requirements, lanes, and fallback probes into the gather flow.")
	}
	if has("gather_context") {
		text := gatherContextModelGuidance(false)
		if has("load_evidence_ref") {
			text = gatherContextModelGuidance(true)
		}
		guidance = append(guidance, text)
	}
	if has("aggregate_evidence_refs") {
		guidance = append(guidance, "Use aggregate_evidence_refs after gather surfaces when several refs may jointly answer; pass the smallest candidate ref set from evidence_digest.load_refs, path_set, or load_queue, then synthesize from answer_outline.supported_claims and slots before loading more refs.")
	}
	if has("evidence_ledger") {
		guidance = append(guidance, "Use evidence_ledger before final answers that require exact evidence binding, especially long-term memory questions with counts, lists, durations, locations, state updates, or possible near-miss evidence. Treat accepted_rows as the only facts you may answer from, rejected_rows as banned direct answers, and fallback_queries as the next gather probes when needs_fallback=true.")
	}
	if has("expand_context_graph") {
		guidance = append(guidance, "When gather_context reports graph.recommended_next_tool=\"expand_context_graph\" or trust.next_action=\"expand_context_graph\", call expand_context_graph with graph.root_refs before making dependency, integration, change-impact, or subsystem completeness claims.")
	}
	if has("gather_test_context") || has("gather_docs_context") {
		var surfaces []string
		if has("gather_memory_context") {
			surfaces = append(surfaces, "gather_memory_context for explicit long-term memory recall")
		}
		if has("gather_test_context") {
			surfaces = append(surfaces, "gather_test_context for explicit test/spec/fixture/mocking questions")
		}
		if has("gather_docs_context") {
			surfaces = append(surfaces, "gather_docs_context for explicit docs/design/architecture/readme questions")
		}
		guidance = append(guidance, "Use specialized gather surfaces instead of broadening gather_context when source intent is explicit: "+strings.Join(surfaces, "; ")+".")
	}
	if has("retrieve_code") {
		text := "Use retrieve_code only for narrow raw code lookup when gather_context is unavailable or too broad."
		if has("load_evidence_ref") {
			text = "Use retrieve_code only for narrow raw code lookup when gather_context is unavailable or too broad, and use load_evidence_ref for exact evidence verification."
		}
		guidance = append(guidance, text)
	}
	if has("retrieve_memory") || has("retrieve_context") || has("retrieve_task") {
		var lanes []string
		for _, name := range []string{"retrieve_memory", "retrieve_context", "retrieve_task"} {
			if has(name) {
				lanes = append(lanes, name)
			}
		}
		text := "Use " + strings.Join(lanes, ", ") + " only for lane-specific debugging or follow-up retrieval."
		if has("load_evidence_ref") {
			text = "Use " + strings.Join(lanes, ", ") + " only for lane-specific debugging or follow-up retrieval, and verify specific evidence with load_evidence_ref."
		}
		guidance = append(guidance, text)
	}
	if has("retrieve_mixed") {
		guidance = append(guidance, "Use retrieve_mixed only when you need the raw fused EvidencePack instead of a reduced ContextBundle.")
	}
	if has("load_evidence_ref") && len(guidance) == 0 {
		guidance = append(guidance, "Use load_evidence_ref to inspect and verify referenced evidence.")
	}

	return strings.Join(guidance, "\n")
}

func gatherContextModelGuidance(hasLoadEvidenceRef bool) string {
	var b strings.Builder
	b.WriteString("For production code, memory, task, or mixed context questions, start with gather_context using response_mode=\"answer_surface\". It is production-code biased: tests and docs are separate explicit surfaces when available.")
	b.WriteString(" Read evidence_digest first for compact supported claims, covered/missing slots, and load_refs before deciding whether more evidence is needed.")
	b.WriteString(" Deterministic gather trust policy: for file_locate and symbol/definition lookup, if answerable=true, certificate.status is not failed, certificate.required_evidence_ok is not false, answer_seed has paths or categories, and gaps/conflicts are empty, copy answer_seed.paths, answer_seed.categories, and answer_seed.facts directly as the final answer seed. Do not spend extra tool/model turns re-ranking those paths.")
	b.WriteString(" For execution_trace, change_impact, architecture_map, subsystem_map, and integration_surface tasks, first inspect the graph/trust metadata; when graph expansion is recommended, call expand_context_graph before claiming dependency or subsystem completeness.")
	b.WriteString(" Fall back to verification or broader retrieval for package-owner/package-anchor questions without categories, broad synthesis beyond the returned map, stale/conflicting evidence, empty answer_seed, required evidence misses, graph gaps, or obvious wrong-scope paths.")
	if hasLoadEvidenceRef {
		b.WriteString(" Use load_evidence_ref only to verify a specific load_ref from path_set.must or load_queue; loading one ref must not narrow the final answer to only that file.")
	}
	return b.String()
}

func buildSynthesisSystemPrompt(base string, plan Plan, candidatePaths, phaseNotes []string) string {
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\nYou are now in the synthesis phase of a staged ")
	b.WriteString(string(plan.RouteProfile))
	b.WriteString(" plan.")
	b.WriteString("\nDo not call tools. Synthesize only from the candidate paths and phase notes already collected.")
	b.WriteString("\nCite exact relative paths and be explicit when evidence is weak.")
	if len(candidatePaths) > 0 {
		b.WriteString("\nFinal candidate paths: ")
		b.WriteString(strings.Join(shortenRefs(candidatePaths, 10), ", "))
	}
	if len(phaseNotes) > 0 {
		b.WriteString("\nPhase notes:\n")
		for _, note := range phaseNotes {
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(note))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func buildSynthesisPrompt(query string, candidatePaths, evidenceRefs, acceptedLedgerEvidence, phaseNotes []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(query))
	b.WriteString("\n\nWrite the final answer using only the verified evidence from the staged phases.")
	// Enumeration guidance for count/list questions: the model must enumerate
	// each evidence piece before stating the count. This prevents undercounting
	// when multiple evidence pieces each carry one item.
	if synthesisQueryIsEnumeration(query) {
		b.WriteString("\n\nThis question asks for a count or list. Before answering, examine each evidence piece separately. For each piece of evidence, state whether it mentions an item relevant to the question. Cast a wide net: include items you need to pick up, return, exchange, retrieve from someone else, or that are otherwise not currently available to you. State the total count only after listing all items from all evidence pieces.")
	}
	// Temporal reasoning guidance: for duration/date-arithmetic questions,
	// extract dates deterministically from evidence and inject them so the
	// model only does arithmetic, not date parsing.
	if at := classifySynthesisAnswerType(query); at == "temporal" || at == "duration" {
		dates := collectSynthesisDates(acceptedLedgerEvidence)
		if len(dates) > 0 {
			b.WriteString("\n\nThis question requires temporal reasoning. The following dates were extracted from the evidence:\n")
			for _, d := range dates {
				b.WriteString("- ")
				b.WriteString(d)
				b.WriteString("\n")
			}
			b.WriteString("Compute the interval between the relevant dates. Express the answer as a specific number of days.")
		} else {
			b.WriteString("\n\nThis question requires temporal reasoning. Extract any dates or time references from the evidence. If you find two dates, compute the interval between them (in days, weeks, or months as appropriate). Express the answer as a specific number.")
		}
	}
	if len(candidatePaths) > 0 {
		b.WriteString("\nCandidate paths: ")
		b.WriteString(strings.Join(shortenRefs(candidatePaths, 10), ", "))
	}
	if len(evidenceRefs) > 0 {
		b.WriteString("\nDiagnostic surfaced evidence refs, not standalone factual support: ")
		b.WriteString(strings.Join(shortenRefs(evidenceRefs, 12), ", "))
	}
	if len(acceptedLedgerEvidence) > 0 {
		b.WriteString("\nAccepted ledger evidence:\n")
		for _, row := range acceptedLedgerEvidence {
			b.WriteString("- ")
			b.WriteString(truncateRLMText(row, 320))
			b.WriteString("\n")
		}
	} else if len(evidenceRefs) > 0 {
		b.WriteString("\nNo accepted ledger evidence was collected. Do not answer from surfaced or rejected evidence; state that verified evidence is insufficient.")
	}
	// Cross-session inference guidance: when evidence spans multiple sessions
	// and the answer requires combining facts (e.g. "age 32" from one session
	// + "living here 5 years" from another = moved at age 27), explicitly
	// instruct the model to state each fact and compute the answer.
	if len(acceptedLedgerEvidence) >= 2 || len(evidenceRefs) >= 2 {
		b.WriteString("\n\nCross-session reasoning: if the answer requires combining facts from multiple evidence sources, state each fact separately, then show how they combine to produce the answer (e.g. arithmetic, comparison, or aggregation).")
	}
	return b.String()
}

// synthesisQueryIsEnumeration detects whether a question asks for a count or
// list of items, requiring enumeration before answering. This is a prompt
// enrichment signal, not a routing/classification heuristic — the answer type
// classification lives in context_query_plan.go.
func synthesisQueryIsEnumeration(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	// Check for count-style questions.
	for _, marker := range []string{"how many", "how much", "number of", "count of", "total number"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	// Check for list-style questions.
	for _, marker := range []string{"list all", "what items", "which items", "what are all"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	return false
}

// classifySynthesisAnswerType detects the answer type for prompt enrichment.
// This is a lightweight local classifier to avoid an import cycle with
// rlm/env. It is used only for synthesis prompt enrichment, not for routing
// or evidence-ledger classification.
func classifySynthesisAnswerType(query string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return "fact"
	}
	if strings.Contains(q, "how many days") || strings.Contains(q, "days between") ||
		strings.Contains(q, "how long ago") || strings.Contains(q, "days passed") ||
		strings.Contains(q, "how long is") || strings.Contains(q, "how long was") ||
		strings.Contains(q, "duration of") || strings.Contains(q, "how much time") {
		return "duration"
	}
	if strings.Contains(q, "when did") || strings.Contains(q, "what date") ||
		strings.Contains(q, "what time") || strings.Contains(q, "which year") ||
		strings.Contains(q, "what month") || strings.Contains(q, "what year") ||
		strings.Contains(q, "last time") || strings.Contains(q, "first time") {
		return "temporal"
	}
	return "fact"
}

// synthesisDatePattern matches common date formats in conversational evidence:
//   - "October 15, 2024" / "Oct 15, 2024" / "October 15 2024"
//   - "15 October 2024" / "15 Oct 2024"
//   - "2024-10-15" / "10/15/2024" / "15/10/2024"
//   - "October 15th" / "Oct 15th"
var synthesisDatePattern = regexp.MustCompile(`(?i)\b(?:` +
	`(?:January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\.?\s+\d{1,2}(?:st|nd|rd|th)?,?\s*\d{4}` + // "October 15, 2024"
	`|\d{1,2}\s+(?:January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\.?\s+\d{4}` + // "15 October 2024"
	`|\d{4}-\d{1,2}-\d{1,2}` + // "2024-10-15"
	`|\d{1,2}/\d{1,2}/\d{4}` + // "10/15/2024"
	`|(?:January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\.?\s+\d{1,2}(?:st|nd|rd|th)?(?:,?\s*\d{4})?` + // "October 15th" (no year)
	`)\b`)

// collectSynthesisDates extracts date strings from accepted ledger evidence
// text. Returns deduplicated dates in order of first appearance. Used to
// inject dates into the synthesis prompt so the model does arithmetic, not
// date parsing.
func collectSynthesisDates(rows []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(rows)*2)
	for _, row := range rows {
		for _, match := range synthesisDatePattern.FindAllString(row, -1) {
			match = strings.TrimSpace(match)
			if match == "" {
				continue
			}
			key := strings.ToLower(match)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, match)
		}
	}
	return out
}

func toolSignature(schema json.RawMessage) string {
	if len(schema) == 0 {
		return ""
	}
	var raw struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(schema, &raw); err != nil || len(raw.Properties) == 0 {
		return ""
	}
	required := make(map[string]struct{}, len(raw.Required))
	for _, name := range raw.Required {
		required[name] = struct{}{}
	}
	names := make([]string, 0, len(raw.Properties))
	for name := range raw.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		suffix := "?"
		if _, ok := required[name]; ok {
			suffix = ""
		}
		parts = append(parts, name+suffix)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func collectEvidenceRefs(env Environment) []string {
	out := make([]string, 0, len(env.RepoHandles)+len(env.VaultHandles)+len(env.SceneHandles)+len(env.ArtifactHandles))
	out = append(out, env.RepoHandles...)
	out = append(out, env.VaultHandles...)
	out = append(out, env.SceneHandles...)
	out = append(out, env.ArtifactHandles...)
	return uniqueStringsRLM(out)
}

const answerPathExtensionPattern = `go|md|yaml|yml|tf|sh|json|sql|toml|txt|ts|tsx|js|jsx|mjs|cjs|py|rs|gd|ex|exs|html|css|scss|swift|kt|java|rb|php|vue|svelte|proto|graphql|gql|xml|csv|ini|env|dockerfile`

var answerPathPattern = regexp.MustCompile(`(?m)(?:^|[\s` + "`" + `("'“])([A-Za-z0-9._/-]+\.(?:` + answerPathExtensionPattern + `))(?:$|[\s` + "`" + `)"'”:,])`)

func collectRetrievedPaths(results []engine.ToolResult, workspaceRoot, answer string) []string {
	out := make([]string, 0, len(results))
	for _, result := range results {
		var payload any
		if err := json.Unmarshal([]byte(strings.TrimSpace(result.Content)), &payload); err != nil {
			continue
		}
		collectPathsRecursive(payload, workspaceRoot, &out)
	}
	for _, match := range answerPathPattern.FindAllStringSubmatch(answer, -1) {
		if len(match) < 2 {
			continue
		}
		if normalized := normalizeRetrievedPath(match[1], workspaceRoot); normalized != "" {
			out = append(out, normalized)
		}
	}
	return uniqueStringsRLM(out)
}

func collectGatherContextSurfaceMetadata(calls []engine.ToolCall, results []engine.ToolResult, workspaceRoot string) map[string]any {
	callNames := make(map[string]string, len(calls))
	for _, call := range calls {
		callNames[call.ID] = call.Name
	}
	selectedPaths := make([]string, 0)
	answerSeedPaths := make([]string, 0)
	pathSetMust := make([]string, 0)
	certificateStatuses := make([]string, 0)
	for _, result := range results {
		if result.IsError || callNames[result.ToolCallID] != "gather_context" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(result.Content)), &payload); err != nil {
			continue
		}
		selectedPaths = append(selectedPaths, pathsFromSelectedPathItems(payload["selected_paths"], workspaceRoot)...)
		answerSeedPaths = append(answerSeedPaths, pathsFromNestedPathList(payload, []string{"answer_seed", "paths"}, workspaceRoot)...)
		pathSetMust = append(pathSetMust, pathsFromNestedPathItems(payload, []string{"path_set", "must"}, workspaceRoot)...)
		if status := stringFromNestedMap(payload, []string{"certificate", "status"}); status != "" {
			certificateStatuses = append(certificateStatuses, status)
		}
	}
	out := map[string]any{}
	if len(selectedPaths) > 0 {
		out["gather_context_selected_paths"] = uniqueStringsRLM(selectedPaths)
	}
	if len(answerSeedPaths) > 0 {
		out["gather_context_answer_seed_paths"] = uniqueStringsRLM(answerSeedPaths)
	}
	if len(pathSetMust) > 0 {
		out["gather_context_path_set_must"] = uniqueStringsRLM(pathSetMust)
	}
	if len(certificateStatuses) > 0 {
		out["gather_context_certificate_statuses"] = uniqueStringsRLM(certificateStatuses)
	}
	return out
}

func pathsFromNestedPathList(payload map[string]any, keys []string, workspaceRoot string) []string {
	value := nestedMapValue(payload, keys)
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if normalized := normalizeRetrievedPath(fmt.Sprint(item), workspaceRoot); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func pathsFromNestedPathItems(payload map[string]any, keys []string, workspaceRoot string) []string {
	return pathsFromSelectedPathItems(nestedMapValue(payload, keys), workspaceRoot)
}

func pathsFromSelectedPathItems(value any, workspaceRoot string) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if normalized := normalizeRetrievedPath(fmt.Sprint(m["path"]), workspaceRoot); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func stringFromNestedMap(payload map[string]any, keys []string) string {
	value := nestedMapValue(payload, keys)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func nestedMapValue(payload map[string]any, keys []string) any {
	var current any = payload
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[key]
	}
	return current
}

func summarizeParentToolUsage(iterations []engine.IterationUsage, toolName string) map[string]any {
	toolName = strings.TrimSpace(toolName)
	out := map[string]any{
		"target_tool":                             toolName,
		"target_tool_invocations":                 0,
		"target_tool_prompt_delta_total":          0,
		"target_tool_result_token_estimate_total": 0,
		"target_tool_invocation_details":          []map[string]any{},
	}
	if toolName == "" || len(iterations) == 0 {
		return out
	}
	details := make([]map[string]any, 0, 4)
	invocations := 0
	promptDeltaTotal := 0
	resultEstimateTotal := 0
	for i, iter := range iterations {
		if !containsString(iter.ToolNames, toolName) {
			continue
		}
		invocations++
		resultEstimateTotal += iter.ToolResultTokenEstimate
		detail := map[string]any{
			"iteration":                  iter.Iteration,
			"prompt_tokens_before":       iter.PromptTokens,
			"completion_tokens":          iter.CompletionTokens,
			"tool_result_token_estimate": iter.ToolResultTokenEstimate,
			"tool_calls":                 iter.ToolCalls,
			"tool_names":                 append([]string(nil), iter.ToolNames...),
			"mixed_tool_iteration":       iter.ToolCalls > 1,
		}
		if i+1 < len(iterations) {
			delta := iterations[i+1].PromptTokens - iter.PromptTokens
			detail["prompt_tokens_after"] = iterations[i+1].PromptTokens
			detail["prompt_token_delta"] = delta
			promptDeltaTotal += delta
		}
		details = append(details, detail)
	}
	out["target_tool_invocations"] = invocations
	out["target_tool_prompt_delta_total"] = promptDeltaTotal
	out["target_tool_result_token_estimate_total"] = resultEstimateTotal
	out["target_tool_invocation_details"] = details
	return out
}

func collectPathsRecursive(value any, workspaceRoot string, out *[]string) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if (key == "path" || key == "full_path") && child != nil {
				if normalized := normalizeRetrievedPath(fmt.Sprint(child), workspaceRoot); normalized != "" {
					*out = append(*out, normalized)
				}
			}
			collectPathsRecursive(child, workspaceRoot, out)
		}
	case []any:
		for _, child := range v {
			collectPathsRecursive(child, workspaceRoot, out)
		}
	}
}

func normalizeRetrievedPath(value, workspaceRoot string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "path:"))
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		if strings.TrimSpace(workspaceRoot) == "" {
			return ""
		}
		rel, err := filepath.Rel(workspaceRoot, value)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ""
		}
		value = rel
	}
	value = filepath.ToSlash(value)
	switch {
	case strings.HasPrefix(value, ".foxctl/"):
		return ""
	case strings.HasPrefix(value, ".claude/"):
		return ""
	default:
		return value
	}
}

func toolCallNames(calls []engine.ToolCall) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, strings.TrimSpace(call.Name))
	}
	return uniqueStringsRLM(names)
}

func enrichEnvironmentWithPaths(env Environment, paths []string) Environment {
	out := env
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		switch {
		case strings.HasPrefix(path, "notes/"), strings.HasPrefix(path, "00-home/"), strings.HasPrefix(path, "atlas/"):
			out.VaultHandles = uniqueStringsRLM(append(out.VaultHandles, "note:"+path))
		default:
			out.RepoHandles = uniqueStringsRLM(append(out.RepoHandles, "path:"+path))
		}
	}
	return out
}

func stagedPhaseEnvironment(env Environment, paths []string, discovery bool) Environment {
	if discovery {
		return env
	}
	out := Environment{
		TopOfMind:       env.TopOfMind,
		LatestHandoff:   env.LatestHandoff,
		ActiveThreadIDs: nil,
		SceneHandles:    nil,
		ArtifactHandles: nil,
		RepoHandles:     nil,
		VaultHandles:    nil,
		Tools:           append([]Tool(nil), env.Tools...),
	}
	return enrichEnvironmentWithPaths(out, shortenRefs(paths, 6))
}

func routeStageEnvironment(env Environment, route RouteProfile) Environment {
	switch route {
	case RouteProfileCodeRetrieval:
		return Environment{
			RepoHandles:  append([]string(nil), env.RepoHandles...),
			VaultHandles: append([]string(nil), env.VaultHandles...),
			Tools:        append([]Tool(nil), env.Tools...),
		}
	default:
		return env
	}
}

func containsAnyToolName(got, want []string) bool {
	allow := make(map[string]struct{}, len(want))
	for _, name := range want {
		allow[strings.TrimSpace(name)] = struct{}{}
	}
	for _, name := range got {
		if _, ok := allow[strings.TrimSpace(name)]; ok {
			return true
		}
	}
	return false
}

func stringsFromAnySlice(value any) []string {
	items, ok := value.([]string)
	if ok {
		return items
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func boolFromRLMAny(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func sumIntMetadata(items []map[string]any, key string) int {
	total := 0
	for _, item := range items {
		total += intFromAny(item[key])
	}
	return total
}

func sumNestedIntMetadata(items []map[string]any, parentKey, childKey string) int {
	total := 0
	for _, item := range items {
		parent, ok := item[parentKey].(map[string]any)
		if !ok {
			continue
		}
		total += intFromAny(parent[childKey])
	}
	return total
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func truncateRLMText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func phaseGuidance(phaseName string) string {
	switch phaseName {
	case "discovery":
		return "End discovery with 2-5 plausible relative paths. If the first search is weak or off-domain, run a second discovery tool before finishing."
	case "inspection":
		return "Do not do another broad search if candidate paths already exist. Open the 1-2 strongest candidates directly."
	case "verification":
		return "Build an accept/reject ledger or literal-check the strongest evidence, then confirm accepted rows really match the query before final synthesis."
	case "recall":
		return "Retrieve the most relevant memory and context entries first. Use ledger-ready refs from evidence_digest or path_set for verification."
	case "audit":
		return "Cross-check evidence from multiple lanes. Look for consistency across sources."
	default:
		return ""
	}
}

// rerankCandidatePaths deduplicates and sorts paths by depth.
// Keyword-based scoring has been removed; paths are ordered by directory depth
// (deeper paths first) as a proxy for specificity.
func rerankCandidatePaths(_ string, paths []string) []string {
	normalized := dedupePathsCaseInsensitive(paths)
	if len(normalized) <= 1 {
		return normalized
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		di, dj := pathDepth(normalized[i]), pathDepth(normalized[j])
		if di == dj {
			return normalized[i] < normalized[j]
		}
		return di > dj
	})
	return normalized
}

func dedupePathsCaseInsensitive(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(filepath.ToSlash(path))
		if path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	return out
}

func pathDepth(path string) int {
	path = strings.Trim(path, "/")
	if path == "" {
		return 0
	}
	return strings.Count(path, "/")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func cloneStringAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// emitAnswerAcceptedFeedback records AnswerAccepted feedback for accepted
// ledger evidence refs, enabling candidate claim promotion via the Slice 1a
// feedback loop. It is best-effort: any error is swallowed so the answer path
// never breaks on a store hiccup. No-op when store is nil or no accepted
// evidence exists.
func emitAnswerAcceptedFeedback(ctx context.Context, store contextengine.RetrievalFeedbackEffectStore, task Task, acceptedEvidence any) {
	if store == nil {
		return
	}
	refs := parseAcceptedLedgerEvidenceRefs(stringsFromAnySlice(acceptedEvidence))
	if len(refs) == 0 {
		return
	}
	answer := strings.TrimSpace(task.Prompt)
	if answer == "" {
		return
	}
	episodeID := deterministicAnswerEpisodeID(task, stringsFromAnySlice(acceptedEvidence))
	now := time.Now().UTC().Truncate(time.Millisecond)
	feedback := contextengine.RetrievalFeedback{
		ID:          "rlm-answer-accepted-" + episodeID,
		WorkspaceID: task.WorkspaceID,
		EpisodeID:   "episode-answer-" + episodeID,
		Kind:        contextengine.RetrievalFeedbackKindAnswerAccepted,
		Query:       answer,
		UsedRefs:    refs,
		CreatedAt:   now,
	}
	_, _ = contextengine.RecordRetrievalFeedbackWithEffects(ctx, store, feedback)
}

// parseAcceptedLedgerEvidenceRefs converts accepted ledger evidence strings
// into typed EvidenceRefs. Strings without a type:value colon separator are
// skipped.
func parseAcceptedLedgerEvidenceRefs(values []string) []contextengine.EvidenceRef {
	out := make([]contextengine.EvidenceRef, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		// Strip trailing descriptions: "named_memory:foo: some text" -> "named_memory:foo"
		if idx := strings.Index(value, ": "); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 {
			continue
		}
		refType := strings.TrimSpace(parts[0])
		refValue := strings.TrimSpace(parts[1])
		if refType == "" || refValue == "" {
			continue
		}
		out = append(out, contextengine.EvidenceRef{
			Type: contextengine.RefType(refType),
			Ref:  refValue,
		})
	}
	return out
}

func deterministicAnswerEpisodeID(task Task, acceptedRefs []string) string {
	h := sha256.New()
	if task.RunID != "" {
		h.Write([]byte(task.RunID))
	} else {
		h.Write([]byte(task.Prompt))
	}
	sorted := append([]string(nil), acceptedRefs...)
	sort.Strings(sorted)
	for _, ref := range sorted {
		h.Write([]byte(ref))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// tryRetrievalFallback implements the REQUIRED_DATA protocol: when the evidence
// ledger has no accepted rows, extract key nouns from the task prompt and run
// one bounded gather_memory_context call to widen the recall net. Returns the
// surfaced evidence refs from the fallback, or nil if the fallback did not fire
// or produced nothing new.
func (r LLMRunner) tryRetrievalFallback(ctx context.Context, task Task, env Environment) []string {
	probeQueries := extractFallbackProbeQueries(task.Prompt)
	if len(probeQueries) == 0 {
		return nil
	}
	// Use the gather_memory_context tool if available, otherwise retrieve_memory.
	toolName := "gather_memory_context"
	if !containsToolName(env.Tools, toolName) {
		toolName = "retrieve_memory"
		if !containsToolName(env.Tools, toolName) {
			return nil
		}
	}
	var newRefs []string
	for _, query := range probeQueries {
		args := map[string]any{
			"query": query,
			"limit": 10,
		}
		argsJSON, err := json.Marshal(args)
		if err != nil {
			continue
		}
		out, err := newAllowlistedToolExecutor(r.Tools, []Tool{{Name: toolName, ReadOnly: true}}).Execute(ctx, toolName, argsJSON)
		if err != nil {
			continue
		}
		newRefs = append(newRefs, extractEvidenceRefsFromToolOutput(out)...)
	}
	return uniqueStringsRLM(newRefs)
}

// extractFallbackProbeQueries derives re-query probes from the task prompt by
// splitting on question markers and extracting noun-heavy phrases. Capped at 3
// probes to bound cost.
func extractFallbackProbeQueries(prompt string) []string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil
	}
	// Extract the trailing Question: text if present (staged prompt format).
	if idx := strings.LastIndex(strings.ToLower(prompt), "question:"); idx >= 0 {
		prompt = strings.TrimSpace(prompt[idx+len("question:"):])
	}
	// Split on common question delimiters and take the last clause.
	question := strings.TrimSpace(prompt)
	if idx := strings.LastIndexByte(question, '?'); idx >= 0 {
		question = strings.TrimSpace(question[:idx])
	}
	// Remove common question prefixes.
	for _, prefix := range []string{"what ", "where ", "when ", "how ", "which ", "do ", "did ", "does ", "is ", "are ", "can ", "could ", "have ", "has "} {
		question = strings.TrimPrefix(strings.ToLower(question), prefix)
	}
	if len(question) < 3 {
		return nil
	}
	// Build probes: the full question + key noun phrases (words > 3 chars).
	probes := []string{question}
	words := strings.Fields(question)
	keyWords := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?()[]{}\"'")
		if len(w) > 3 {
			keyWords = append(keyWords, w)
		}
	}
	if len(keyWords) > 0 && len(keyWords) <= 6 {
		probe := strings.Join(keyWords, " ")
		if probe != question {
			probes = append(probes, probe)
		}
	}
	if len(probes) > 3 {
		probes = probes[:3]
	}
	return probes
}

// extractEvidenceRefsFromToolOutput pulls evidence ref strings from a tool
// execution result map.
func extractEvidenceRefsFromToolOutput(out any) []string {
	m, ok := out.(map[string]any)
	if !ok {
		return nil
	}
	var refs []string
	// Check common output field names.
	for _, key := range []string{"evidence_refs", "refs", "surfaced_refs", "nodes"} {
		if values, ok := m[key].([]any); ok {
			for _, v := range values {
				switch val := v.(type) {
				case string:
					refs = append(refs, val)
				case map[string]any:
					if ref, ok := val["ref"].(string); ok && ref != "" {
						refs = append(refs, ref)
					}
					if path, ok := val["path"].(string); ok && path != "" {
						refs = append(refs, path)
					}
				}
			}
		}
	}
	return refs
}

func containsToolName(tools []Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}
