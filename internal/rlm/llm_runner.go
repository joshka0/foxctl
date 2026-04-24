package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// LLMToolExecutor adapts the RLM tool surface to the engine.ToolExecutor contract.
type LLMToolExecutor struct {
	adapter ToolExecutor
	tools   []Tool
}

func NewLLMToolExecutor(adapter ToolExecutor, tools []Tool) *LLMToolExecutor {
	return &LLMToolExecutor{adapter: adapter, tools: tools}
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
}

// LLMRunner uses the existing engine.LLMChatEngine as an experimental read-only RLM backend.
type LLMRunner struct {
	Config LLMConfig
	Tools  ToolExecutor
}

func (r LLMRunner) Run(ctx context.Context, task Task, env Environment) (Result, error) {
	if err := ValidateTask(task); err != nil {
		return Result{}, err
	}
	if err := ValidateEnvironment(env); err != nil {
		return Result{}, err
	}
	if r.Tools == nil {
		return Result{}, fmt.Errorf("rlm llm runner requires tool adapter")
	}
	spec, err := ResolveRunSpec(ResolveRunSpecInput{
		Prompt:               task.Prompt,
		RequestedRoute:       r.Config.RouteProfile,
		RequestedPlanMode:    r.Config.PlanMode,
		RequestedToolProfile: string(ToolProfileDefault),
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
	answer := strings.TrimSpace(output.AssistantText)
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
	evidence := collectEvidenceRefs(env)
	retrievedPaths := collectRetrievedPaths(output.ToolResults, task.WorkspaceRoot, answer)
	parentUsage := summarizeParentToolUsage(output.Iterations, "code_search_ensemble")
	metadata := map[string]any{
		"stop_reason":            output.StopReason,
		"provider":               llmCfg.Provider,
		"model":                  llmCfg.Model,
		"tool_calls":             len(output.ToolCalls),
		"tool_names":             toolCallNames(output.ToolCalls),
		"llm_error":              output.Error,
		"require_tool_use":       pass.RequireToolUse,
		"retrieved_paths":        retrievedPaths,
		"parent_input_tokens":    output.Tokens.InputTokens,
		"parent_output_tokens":   output.Tokens.OutputTokens,
		"parent_total_tokens":    output.Tokens.TotalTokens,
		"parent_iteration_count": len(output.Iterations),
		"parent_tool_usage":      parentUsage,
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
	b.WriteString("For repo questions, start with code_search_ensemble. Use semantic_search_code or smart_search_code only as follow-up discovery lanes when the ensemble leaves uncertainty. Use search_repo only as a shallow fallback, then load_file for exact verification, and ripgrep_code for literal patterns.\n")
	b.WriteString("Cite exact relative repo file paths you inspected. Do not cite .foxctl or .claude runtime files as repository evidence.\n")
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
		phaseResult, err := r.runSinglePass(ctx, task, phaseEnv, runPassConfig{
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
			requiredTools := filterToolsByNames(phaseEnv.Tools, phase.RequireOneOf)
			if len(requiredTools) == 0 {
				return Result{}, fmt.Errorf("rlm llm runner: %s phase did not use any required tools", phase.Name)
			}
			retryResult, retryErr := r.runSinglePass(ctx, task, phaseEnv, runPassConfig{
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
		if summary := strings.TrimSpace(phaseResult.Answer); summary != "" {
			phaseNotes = append(phaseNotes, phase.Name+": "+truncateRLMText(summary, 320))
			if len(phaseNotes) > 3 {
				phaseNotes = phaseNotes[len(phaseNotes)-3:]
			}
		}
		phaseMeta = append(phaseMeta, map[string]any{
			"name":                 phase.Name,
			"tool_names":           toolNames,
			"retrieved_paths":      append([]string(nil), phaseResult.RetrievedPaths...),
			"answer":               phaseResult.Answer,
			"parent_input_tokens":  intFromAny(phaseResult.Metadata["parent_input_tokens"]),
			"parent_output_tokens": intFromAny(phaseResult.Metadata["parent_output_tokens"]),
			"parent_total_tokens":  intFromAny(phaseResult.Metadata["parent_total_tokens"]),
			"parent_tool_usage":    phaseResult.Metadata["parent_tool_usage"],
		})
	}

	allPaths = rerankCandidatePaths(task.Prompt, allPaths)
	finalEnv := enrichEnvironmentWithPaths(baseStageEnv, allPaths)
	finalPrompt := buildSynthesisPrompt(task.Prompt, allPaths, phaseNotes)
	finalResult, err := r.runSinglePass(ctx, task, finalEnv, runPassConfig{
		Prompt:         finalPrompt,
		Tools:          nil,
		SystemPrompt:   buildSynthesisSystemPrompt(BuildLLMSystemPrompt(finalEnv, task), plan, allPaths, phaseNotes),
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
	finalResult.Metadata["phase_count"] = len(plan.Phases)
	finalResult.Metadata["phases"] = phaseMeta
	finalResult.Metadata["tool_calls"] = totalToolCalls
	finalResult.Metadata["retrieved_paths"] = finalResult.RetrievedPaths
	finalResult.Metadata["parent_input_tokens_total"] = sumIntMetadata(phaseMeta, "parent_input_tokens")
	finalResult.Metadata["parent_output_tokens_total"] = sumIntMetadata(phaseMeta, "parent_output_tokens")
	finalResult.Metadata["parent_total_tokens_total"] = sumIntMetadata(phaseMeta, "parent_total_tokens")
	finalResult.Metadata["parent_code_search_ensemble_prompt_delta_total"] = sumNestedIntMetadata(phaseMeta, "parent_tool_usage", "target_tool_prompt_delta_total")
	finalResult.Metadata["parent_code_search_ensemble_invocations_total"] = sumNestedIntMetadata(phaseMeta, "parent_tool_usage", "target_tool_invocations")
	finalResult.Metadata["parent_code_search_ensemble_result_token_estimate_total"] = sumNestedIntMetadata(phaseMeta, "parent_tool_usage", "target_tool_result_token_estimate_total")
	return finalResult, nil
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
	if hints := queryGuidanceHints(query, phase.Name, candidatePaths); len(hints) > 0 {
		b.WriteString("\nGeneral heuristics:\n")
		for _, hint := range hints {
			b.WriteString("- ")
			b.WriteString(hint)
			b.WriteString("\n")
		}
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
		b.WriteString("\nYou must open at least one candidate path with load_file or read_note before you finish.")
	}
	if len(candidatePaths) > 0 {
		b.WriteString("\nFocus on these candidate paths if they seem relevant: ")
		b.WriteString(strings.Join(shortenRefs(candidatePaths, 8), ", "))
	}
	if hints := queryGuidanceHints(query, phase.Name, candidatePaths); len(hints) > 0 {
		b.WriteString("\nQuery guidance:\n")
		for _, hint := range hints {
			b.WriteString("- ")
			b.WriteString(hint)
			b.WriteString("\n")
		}
	}
	if len(phaseNotes) > 0 {
		b.WriteString("\nUse prior findings, but verify them with this phase's tools.")
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

func buildSynthesisPrompt(query string, candidatePaths, phaseNotes []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(query))
	b.WriteString("\n\nWrite the final answer using only the verified evidence from the staged phases.")
	if len(candidatePaths) > 0 {
		b.WriteString("\nCandidate paths: ")
		b.WriteString(strings.Join(shortenRefs(candidatePaths, 10), ", "))
	}
	return b.String()
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

var answerPathPattern = regexp.MustCompile(`(?m)(?:^|[\s` + "`" + `("'“])([A-Za-z0-9._/-]+\.(?:go|md|yaml|yml|tf|sh|json|sql|toml|txt))(?:$|[\s` + "`" + `)"'”:,])`)

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
		return "Re-open or literal-check the single strongest inspected path and confirm it really matches the query."
	default:
		return ""
	}
}

func queryGuidanceHints(query, phaseName string, candidatePaths []string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	hints := make([]string, 0, 4)
	if strings.Contains(q, "package") {
		hints = append(hints, "Prefer directory or package matches over basename-only coincidences.")
	}
	if strings.Contains(q, "semantic") && strings.Contains(q, "index") {
		hints = append(hints, "Prefer paths under internal/intelligence/indexing/semantic or closely related indexing directories.")
		hints = append(hints, "Avoid generic repository or events files unless they explicitly mention semantic indexing.")
	}
	if containsAny(q, "web", "api", "handler", "transport") {
		hints = append(hints, "Prefer handler, server, transport, bridge, http, or api paths over unrelated tool clients.")
		hints = append(hints, "Avoid obsidian/tooling paths unless the query explicitly mentions them.")
	}
	if phaseName == "inspection" && len(candidatePaths) > 0 {
		hints = append(hints, "Choose the candidate whose directory and filename most directly match the query terms.")
	}
	return hints
}

func rerankCandidatePaths(query string, paths []string) []string {
	type scoredPath struct {
		Path  string
		Score int
	}
	normalized := dedupePathsCaseInsensitive(paths)
	if len(normalized) <= 1 {
		return normalized
	}
	scored := make([]scoredPath, 0, len(normalized))
	for _, path := range normalized {
		scored = append(scored, scoredPath{
			Path:  path,
			Score: scoreCandidatePath(query, path),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			if pathDepth(scored[i].Path) == pathDepth(scored[j].Path) {
				return scored[i].Path < scored[j].Path
			}
			return pathDepth(scored[i].Path) > pathDepth(scored[j].Path)
		}
		return scored[i].Score > scored[j].Score
	})
	out := make([]string, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.Path)
	}
	return out
}

func scoreCandidatePath(query, path string) int {
	q := strings.ToLower(strings.TrimSpace(query))
	p := strings.ToLower(strings.TrimSpace(filepath.ToSlash(path)))
	if p == "" {
		return -1000
	}
	score := 0
	tokens := tokenizeQueryTerms(q)
	for _, token := range tokens {
		if token == "" {
			continue
		}
		switch {
		case strings.Contains("/"+p+"/", "/"+token+"/"):
			score += 8
		case strings.Contains(p, token):
			score += 4
		}
	}
	if strings.Contains(q, "package") {
		if strings.Count(p, "/") >= 2 {
			score += 2
		}
		if !strings.Contains(p, "/") {
			score -= 4
		}
	}
	if containsAny(q, "semantic", "index") {
		if strings.Contains(p, "indexing/semantic") {
			score += 12
		}
		if strings.Contains(p, "repository") || strings.Contains(p, "events/") || strings.Contains(p, "builder") {
			score -= 6
		}
	}
	if containsAny(q, "web", "api", "handler", "transport") {
		if containsAny(p, "web/", "/web", "api", "handler", "http", "transport", "server", "bridge") {
			score += 8
		}
		if containsAny(p, "obsidian", "godot", "gui-agent") {
			score -= 8
		}
	}
	if containsAny(q, "storage", "memory") {
		if containsAny(p, "storage/memory", "/memory/", "memoryflow", "stores.go", "store.go") {
			score += 10
		}
	}
	if containsAny(q, "skill", "output", "envelope", "cas") {
		if containsAny(p, "skillout", "cas", "envelope", "skillmain") {
			score += 8
		}
	}
	if containsAny(q, "platform", "config") {
		if containsAny(p, "config", "platform") {
			score += 8
		}
	}
	if containsAny(q, "cmd", "runtime") {
		if containsAny(p, "cmd/", "/cmd", "runtime") {
			score += 8
		}
	}
	score += minInt(pathDepth(p), 6)
	return score
}

func tokenizeQueryTerms(query string) []string {
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make([]string, 0, len(parts)+8)
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if len(part) < 3 {
			continue
		}
		switch part {
		case "handlers":
			part = "handler"
		case "settings":
			part = "config"
		}
		out = append(out, part)
	}
	if containsAny(query, "web", "api", "handler", "transport") {
		out = append(out, "web", "api", "handler", "transport", "http", "server")
	}
	if containsAny(query, "semantic", "index") {
		out = append(out, "semantic", "index", "indexing", "provider", "indexer")
	}
	if containsAny(query, "storage", "memory") {
		out = append(out, "storage", "memory", "store")
	}
	if containsAny(query, "skill", "output", "envelope", "cas") {
		out = append(out, "skill", "output", "envelope", "cas")
	}
	if containsAny(query, "platform", "config") {
		out = append(out, "platform", "config")
	}
	return uniqueStringsRLM(out)
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
