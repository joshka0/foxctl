package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/rlm/optdata"
	rlmruntime "github.com/joshka0/foxctl/internal/rlm/runtime"
)

type rlmOptimizerTraceInput struct {
	Executor        string
	Mode            string
	ToolProfile     string
	RouteProfile    string
	PlanMode        string
	SandboxKind     string
	EphemeralSkills bool
	ExtractSolution bool
	LLMProvider     string
	LLMModel        string
	LLMBaseURL      string
	InputPrompt     string
	Task            rlm.Task
	Environment     rlm.Environment
	Result          rlm.Result
	RunErr          error
	RecordedAt      time.Time
}

type rlmOptimizerTraceModel struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
}

func resolveRLMTraceOutputPath(optTraceOut, trajectoryOut string) (string, error) {
	optTraceOut = strings.TrimSpace(optTraceOut)
	trajectoryOut = strings.TrimSpace(trajectoryOut)
	switch {
	case optTraceOut == "" && trajectoryOut == "":
		return "", nil
	case optTraceOut != "" && trajectoryOut != "":
		optAbs, err := filepath.Abs(optTraceOut)
		if err != nil {
			return "", fmt.Errorf("resolve --opt-trace-out path: %w", err)
		}
		trajectoryAbs, err := filepath.Abs(trajectoryOut)
		if err != nil {
			return "", fmt.Errorf("resolve --trajectory-out path: %w", err)
		}
		if optAbs != trajectoryAbs {
			return "", fmt.Errorf("--opt-trace-out and --trajectory-out must match when both are set")
		}
		return optAbs, nil
	case optTraceOut != "":
		path, err := filepath.Abs(optTraceOut)
		if err != nil {
			return "", fmt.Errorf("resolve --opt-trace-out path: %w", err)
		}
		return path, nil
	default:
		path, err := filepath.Abs(trajectoryOut)
		if err != nil {
			return "", fmt.Errorf("resolve --trajectory-out path: %w", err)
		}
		return path, nil
	}
}

func buildRLMOptimizerTraceRecord(input rlmOptimizerTraceInput) optdata.TrajectoryRecord {
	recordedAt := input.RecordedAt
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}
	model := buildRLMOptimizerTraceModel(input)
	success := input.RunErr == nil
	errorMessage := ""
	if input.RunErr != nil {
		errorMessage = strings.TrimSpace(input.RunErr.Error())
	}
	feedbackItems := buildRLMTraceFeedback(input.Result.Metadata)
	if errorMessage != "" {
		feedbackItems = append(feedbackItems, rlmTraceFeedbackItem{
			Component: optdata.ComponentRuntimeError,
			Stage:     "runtime",
			Outcome:   "error",
			Message:   "runtime_error: " + truncateRLMTraceText(errorMessage, 320),
		})
	}

	return optdata.NewRecordBuilder(optdata.WithBuilderNow(func() time.Time { return recordedAt })).Build(optdata.BuildInput{
		RecordID: firstNonEmpty(input.Result.TrajectoryID, stringFromRLMTraceMetadata(input.Result.Metadata, "run_id"), input.Task.RunID),
		Prompt: buildRLMTracePromptComponents(
			input.Mode,
			input.InputPrompt,
			input.Task,
			input.Environment,
			input.RouteProfile,
			input.PlanMode,
			input.SandboxKind,
			input.EphemeralSkills,
		),
		Execution: optdata.ExecutionMetadata{
			Runtime:          "rlm",
			Mode:             strings.TrimSpace(input.Mode),
			Model:            model.Model,
			OutputText:       truncateRLMTraceText(strings.TrimSpace(input.Result.Answer), 24_000),
			RunID:            firstNonEmpty(stringFromRLMTraceMetadata(input.Result.Metadata, "run_id"), input.Task.RunID),
			AgentID:          firstNonEmpty(stringFromRLMTraceMetadata(input.Result.Metadata, "agent_id"), input.Task.AgentID),
			Success:          success,
			ErrorMessage:     errorMessage,
			PromptTokens:     firstNonZeroRLMTraceInt(intFromRLMTraceMetadata(input.Result.Metadata, "parent_input_tokens"), intFromRLMTraceMetadata(input.Result.Metadata, "parent_input_tokens_total")),
			CompletionTokens: firstNonZeroRLMTraceInt(intFromRLMTraceMetadata(input.Result.Metadata, "parent_output_tokens"), intFromRLMTraceMetadata(input.Result.Metadata, "parent_output_tokens_total")),
			TotalTokens:      firstNonZeroRLMTraceInt(intFromRLMTraceMetadata(input.Result.Metadata, "parent_total_tokens"), intFromRLMTraceMetadata(input.Result.Metadata, "parent_total_tokens_total")),
			EvidenceRefs:     append([]string(nil), input.Result.EvidenceRefs...),
			RetrievedPaths:   append([]string(nil), input.Result.RetrievedPaths...),
		},
		Metrics:  buildRLMTraceMetrics(input, success),
		Feedback: buildRLMTraceFeedbackRecords(feedbackItems),
		Labels: map[string]string{
			"executor":         strings.TrimSpace(input.Executor),
			"mode":             strings.TrimSpace(input.Mode),
			"tool_profile":     strings.TrimSpace(input.ToolProfile),
			"route_profile":    strings.TrimSpace(input.RouteProfile),
			"plan_mode":        strings.TrimSpace(input.PlanMode),
			"sandbox_kind":     strings.TrimSpace(input.SandboxKind),
			"ephemeral_skills": boolRLMTraceLabel(input.EphemeralSkills),
			"extract_solution": boolRLMTraceLabel(input.ExtractSolution),
			"provider":         model.Provider,
			"base_url":         model.BaseURL,
		},
	})
}

func appendRLMOptimizerTraceRecord(path string, record optdata.TrajectoryRecord) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return optdata.AppendTrajectoryRecordFile(path, record)
}

func buildRLMOptimizerTraceModel(input rlmOptimizerTraceInput) rlmOptimizerTraceModel {
	if strings.EqualFold(strings.TrimSpace(input.Mode), "inspect") {
		return rlmOptimizerTraceModel{}
	}
	return rlmOptimizerTraceModel{
		Provider: strings.TrimSpace(firstNonEmpty(input.LLMProvider, os.Getenv("FOXCTL_RLM_LLM_PROVIDER"), "lmstudio")),
		Model:    strings.TrimSpace(firstNonEmpty(input.LLMModel, os.Getenv("FOXCTL_RLM_LLM_MODEL"), os.Getenv("LMSTUDIO_MODEL"))),
		BaseURL:  strings.TrimSpace(firstNonEmpty(input.LLMBaseURL, os.Getenv("FOXCTL_RLM_LLM_BASE_URL"), os.Getenv("LMSTUDIO_BASE_URL"))),
	}
}

func buildRLMTracePromptComponents(
	mode string,
	inputPrompt string,
	task rlm.Task,
	env rlm.Environment,
	routeProfile string,
	planMode string,
	sandboxKind string,
	ephemeralSkills bool,
) optdata.PromptComponents {
	prompt := optdata.PromptComponents{
		Objective: "Optimize foxctl RLM prompt and tool policy for correctness, valid tool use, concise fan-in, and safe structured output.",
		User:      strings.TrimSpace(inputPrompt),
		ContextBlocks: []optdata.PromptContextBlock{
			{Name: "effective_task_prompt", Source: optdata.ComponentTaskPrompt, Content: strings.TrimSpace(task.Prompt)},
		},
		ToolDefinitions: rlmTraceToolDefinitions(env.Tools),
	}
	if role := strings.TrimSpace(task.Role); role != "" {
		prompt.ContextBlocks = append(prompt.ContextBlocks, optdata.PromptContextBlock{Name: "scout_role", Source: "rlm.task.role", Content: role})
	}
	if strings.TrimSpace(routeProfile) != "" {
		prompt.ContextBlocks = append(prompt.ContextBlocks, optdata.PromptContextBlock{Name: "route_profile", Source: "rlm.route_profile", Content: strings.TrimSpace(routeProfile)})
	}
	if strings.TrimSpace(planMode) != "" {
		prompt.ContextBlocks = append(prompt.ContextBlocks, optdata.PromptContextBlock{Name: "plan_mode", Source: "rlm.plan_mode", Content: strings.TrimSpace(planMode)})
	}
	mode = strings.TrimSpace(mode)
	if mode == "llm" {
		spec, err := rlm.ResolveRunSpec(rlm.ResolveRunSpecInput{
			Prompt:               task.Prompt,
			RequestedRoute:       rlm.RouteProfile(routeProfile),
			RequestedPlanMode:    rlm.PlanMode(planMode),
			RequestedToolProfile: string(rlm.ToolProfileDefault),
			AvailableTools:       env.Tools,
		})
		if err == nil {
			prompt.System = rlm.BuildLLMSystemPrompt(env, task)
			prompt.ContextBlocks = append(
				prompt.ContextBlocks,
				optdata.PromptContextBlock{Name: "effective_route_profile", Source: "rlm.run_spec.route_profile", Content: string(spec.RouteProfile)},
				optdata.PromptContextBlock{Name: "effective_plan_mode", Source: "rlm.run_spec.plan_mode", Content: string(spec.PlanMode)},
				optdata.PromptContextBlock{Name: "effective_tool_policy", Source: "rlm.run_spec.tool_policy", Content: jsonStringRLMTrace(spec.ToolPolicy)},
				optdata.PromptContextBlock{Name: "rlm_plan", Source: "rlm.run_spec.plan", Content: jsonStringRLMTrace(spec.Plan)},
			)
		} else {
			plan := rlm.BuildPlan(task.Prompt, rlm.NormalizeRouteProfile(routeProfile), rlm.NormalizePlanMode(planMode))
			prompt.System = rlm.BuildLLMSystemPrompt(env, task)
			prompt.ContextBlocks = append(prompt.ContextBlocks, optdata.PromptContextBlock{Name: "rlm_plan", Source: "rlm.BuildPlan", Content: jsonStringRLMTrace(plan)})
		}
		if rlm.NormalizePlanMode(planMode) == rlm.PlanModeLambda && ephemeralSkills {
			prompt.System = rlm.HelperSolveDraftSystemPrompt()
			prompt.ContextBlocks = append(
				prompt.ContextBlocks,
				optdata.PromptContextBlock{Name: "helper_solve_system_prompt", Source: optdata.ComponentHelperSolveSystem, Content: rlm.HelperSolveDraftSystemPrompt()},
				optdata.PromptContextBlock{Name: "helper_solve_draft_prompt_template", Source: optdata.ComponentHelperSolveDraft, Content: rlm.HelperSolveDraftPromptTemplate()},
			)
		}
	}
	if mode == "repl" {
		kind := rlmruntime.NormalizeSandboxKind(rlmruntime.SandboxKind(sandboxKind))
		helperEnabled := true
		recursionEnabled := task.MaxDepth > 0 && task.MaxSubcalls > 0
		prompt.System = buildRLMREPLCLISystemPromptForPolicy(kind, helperEnabled, recursionEnabled)
		prompt.ContextBlocks = append(
			prompt.ContextBlocks,
			optdata.PromptContextBlock{Name: "repl_system_prompt", Source: optdata.ComponentREPLSystemPrompt, Content: buildRLMREPLCLISystemPromptForPolicy(kind, helperEnabled, recursionEnabled)},
			optdata.PromptContextBlock{Name: "repl_initial_state", Source: "rlm.repl.initial_state", Content: jsonStringRLMTrace(map[string]any{"official_prompt": strings.TrimSpace(task.Prompt)})},
		)
	}
	return prompt
}

type rlmTraceFeedbackItem struct {
	Component string
	Stage     string
	Outcome   string
	Message   string
}

func buildRLMTraceFeedback(metadata map[string]any) []rlmTraceFeedbackItem {
	if len(metadata) == 0 {
		return nil
	}
	out := make([]rlmTraceFeedbackItem, 0, 8)
	appendFeedback := func(component, prefix, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		out = append(out, rlmTraceFeedbackItem{
			Component: component,
			Stage:     "runtime",
			Outcome:   "diagnostic",
			Message:   prefix + ": " + truncateRLMTraceText(value, 320),
		})
	}

	appendFeedback(optdata.ComponentRuntimeError, "llm_error", fmt.Sprint(metadata["llm_error"]))
	if outputSanitization := rlmTraceMap(metadata["output_sanitization"]); len(outputSanitization) > 0 {
		appendFeedback(optdata.ComponentOutputSanitization, "output_sanitization", fmt.Sprint(outputSanitization["raw_text"]))
	}
	if extraction := rlmTraceMap(metadata["solution_extraction"]); len(extraction) > 0 {
		appendFeedback(optdata.ComponentSolutionExtraction, "solution_extraction", fmt.Sprint(extraction["raw_text"]))
	}
	if finalization := rlmTraceMap(metadata["ephemeral_skill_finalization"]); len(finalization) > 0 {
		appendFeedback(optdata.ComponentEphemeralFinalization, "ephemeral_skill_finalization", fmt.Sprint(finalization["raw_text"]))
	}

	for _, attempt := range rlmTraceMaps(metadata["helper_solve_attempts"]) {
		if value, ok := attempt["ok"].(bool); ok && value {
			continue
		}
		stage := strings.TrimSpace(fmt.Sprint(attempt["stage"]))
		errorText := strings.TrimSpace(fmt.Sprint(attempt["error"]))
		if errorText == "" {
			errorText = strings.TrimSpace(fmt.Sprint(attempt["raw"]))
		}
		if errorText == "" {
			errorText = strings.TrimSpace(fmt.Sprint(attempt["output"]))
		}
		if stage != "" {
			appendFeedback(optdata.ComponentHelperSolveRuntime, "helper_solve_"+stage, errorText)
			continue
		}
		appendFeedback(optdata.ComponentHelperSolveRuntime, "helper_solve", errorText)
	}

	for _, event := range rlmTraceMaps(metadata["trajectory_events"]) {
		runtimeErr := rlmTraceMap(event["error"])
		if len(runtimeErr) == 0 {
			continue
		}
		appendFeedback(optdata.ComponentTrajectoryRuntime, "trajectory_error", fmt.Sprint(runtimeErr["message"]))
	}

	return uniqueRLMTraceFeedbackItems(out)
}

func rlmTraceToolDefinitions(tools []rlm.Tool) []optdata.PromptToolDefinition {
	out := make([]optdata.PromptToolDefinition, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		out = append(out, optdata.PromptToolDefinition{
			Name:            name,
			Description:     strings.TrimSpace(tool.Description),
			InputSchemaJSON: strings.TrimSpace(string(tool.Parameters)),
		})
	}
	return out
}

func buildRLMTraceMetrics(input rlmOptimizerTraceInput, success bool) []optdata.MetricFeedback {
	passed := success
	metrics := []optdata.MetricFeedback{
		{Name: "success", Value: boolRLMTraceMetric(success), Goal: optdata.MetricGoalMaximize, Passed: &passed, Source: "runtime"},
		{Name: "iterations", Value: float64(input.Result.Iterations), Goal: optdata.MetricGoalMinimize, Source: "runtime"},
		{Name: "subcalls", Value: float64(input.Result.Subcalls), Goal: optdata.MetricGoalMinimize, Source: "runtime"},
		{Name: "tool_calls", Value: float64(intFromRLMTraceMetadata(input.Result.Metadata, "tool_calls")), Goal: optdata.MetricGoalMinimize, Source: "metadata"},
		{Name: "total_tokens", Value: float64(firstNonZeroRLMTraceInt(intFromRLMTraceMetadata(input.Result.Metadata, "parent_total_tokens"), intFromRLMTraceMetadata(input.Result.Metadata, "parent_total_tokens_total"))), Goal: optdata.MetricGoalMinimize, Source: "metadata"},
	}
	if attempts := len(rlmTraceMaps(input.Result.Metadata["helper_solve_attempts"])); attempts > 0 {
		metrics = append(metrics, optdata.MetricFeedback{
			Name:   "helper_solve_attempts",
			Value:  float64(attempts),
			Goal:   optdata.MetricGoalMinimize,
			Source: "metadata",
			Notes:  "Retries indicate prompt/schema discipline issues.",
		})
	}
	return metrics
}

func buildRLMTraceFeedbackRecords(values []rlmTraceFeedbackItem) []optdata.PromptFeedback {
	values = uniqueRLMTraceFeedbackItems(values)
	out := make([]optdata.PromptFeedback, 0, len(values))
	for _, value := range values {
		component := strings.TrimSpace(value.Component)
		if component == "" {
			component = optdata.ComponentRuntimeOutput
		}
		stage := strings.TrimSpace(value.Stage)
		if stage == "" {
			stage = "runtime"
		}
		outcome := strings.TrimSpace(value.Outcome)
		if outcome == "" {
			outcome = "diagnostic"
		}
		out = append(out, optdata.PromptFeedback{
			Component: component,
			Stage:     stage,
			Outcome:   outcome,
			Message:   strings.TrimSpace(value.Message),
		})
	}
	return out
}

func rlmTraceMap(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return typed
	default:
		body, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			return nil
		}
		return out
	}
}

func rlmTraceMaps(value any) []map[string]any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped := rlmTraceMap(item); len(mapped) > 0 {
				out = append(out, mapped)
			}
		}
		return out
	default:
		body, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var out []map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			return nil
		}
		return out
	}
}

func uniqueRLMTraceFeedbackItems(values []rlmTraceFeedbackItem) []rlmTraceFeedbackItem {
	seen := map[string]struct{}{}
	out := make([]rlmTraceFeedbackItem, 0, len(values))
	for _, value := range values {
		message := strings.TrimSpace(value.Message)
		if message == "" {
			continue
		}
		component := strings.TrimSpace(value.Component)
		key := component + "\x00" + message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		value.Component = component
		value.Message = message
		out = append(out, value)
	}
	return out
}

func truncateRLMTraceText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func intFromRLMTraceMetadata(metadata map[string]any, key string) int {
	if len(metadata) == 0 {
		return 0
	}
	switch typed := metadata[key].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		value, _ := typed.Int64()
		return int(value)
	default:
		return 0
	}
}

func stringFromRLMTraceMetadata(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func firstNonZeroRLMTraceInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func boolRLMTraceMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func boolRLMTraceLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func jsonStringRLMTrace(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(body)
}
