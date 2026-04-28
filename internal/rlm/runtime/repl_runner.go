package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/rlm/repl"
	"github.com/joshka0/foxctl/internal/runtime/engine"
)

const (
	PythonREPLToolName         = "python_repl"
	GoREPLToolName             = "go_repl"
	RLMQueryToolName           = "rlm_query"
	replRootNodeID             = "root"
	requiredSubcallMaxAttempts = 3
	pendingSubcallMaxAttempts  = 3
)

// RecursionPolicy controls whether rlm_query/rlm_wait/rlm_result are optional.
type RecursionPolicy string

const (
	RecursionPolicyOptional RecursionPolicy = "optional"
	RecursionPolicyRequired RecursionPolicy = "required"
	RecursionPolicyDisabled RecursionPolicy = "disabled"
)

// SandboxKind identifies the scratch execution backend used by REPLRunner.
type SandboxKind string

const (
	SandboxKindPython SandboxKind = "python"
	SandboxKindYaegi  SandboxKind = "yaegi"
)

// SandboxConfig configures the scratch sandbox. Python remains the default.
type SandboxConfig struct {
	Kind   SandboxKind
	Python repl.Options
	Yaegi  repl.YaegiOptions
}

// RLMQueryRunFunc executes one recursive child call for the optional rlm_query tool.
type RLMQueryRunFunc func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error)

// TelemetrySink receives structured RLM runtime events. Implementations must be
// best-effort and should not fail the run.
type TelemetrySink interface {
	EmitRLMEvent(ctx context.Context, event Event)
}

// REPLRunnerConfig configures a paper-style RLM parent loop with a persistent
// sandbox and optional bounded recursive subcalls.
type REPLRunnerConfig struct {
	LLM                               rlm.LLMConfig
	Budget                            BudgetConfig
	Sandbox                           SandboxConfig
	Python                            repl.Options
	SystemPrompt                      string
	InitialState                      map[string]any
	Phases                            []REPLRunnerPhase
	DefaultREPLCode                   string
	DefaultRLMQueryPrompt             string
	RecursionPolicy                   RecursionPolicy
	REPLToolResultMaxChars            int
	ChildSummaryMaxChars              int
	ChildSummaryNormalizeBeforeSubmit bool
	ChildSummaryRewriteOverLimit      bool
	ChildSummaryRewriteMaxIterations  int
	RejectFailedSubcalls              bool
	RLMQueryFactory                   func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc
	AsyncRecursion                    bool
	AsyncScheduler                    SchedulerConfig
	RequiredSubcallRules              []RequiredSubcallRule
	ExtraToolExecutor                 engine.ToolExecutor
	EphemeralSkills                   bool
	HelperFactory                     *HelperFactoryConfig
	ExtractSolutionLine               bool
	FinalSolutionLineRequired         bool
	FinalAnswerFromVerifiedHandoff    bool
	FinalAnswerRepairMaxAttempts      int
	ToolErrorRepairMaxAttempts        int
	Telemetry                         TelemetrySink
}

// REPLRunnerPhase constrains one model turn to a specific tool surface. Phases
// let callers make protocol order deterministic while keeping one sandbox and
// one recursive scheduler for the whole RLM attempt.
type REPLRunnerPhase struct {
	Name                       string
	Prompt                     string
	OutputKind                 string
	MaxGraphNodes              int
	BraidGraphPolicy           string
	BraidRepairAttempts        int
	FinalOutputKind            string
	ResponseFormat             json.RawMessage
	MaxTokens                  int
	Tools                      []string
	RequiredTools              []string
	MaxIterations              int
	Final                      bool
	AutoExecuteRequiredTool    bool
	AutoExecuteGraphNodes      bool
	DisableHelperFirstFallback bool
	AutoExecuteToolCalls       []REPLRunnerPhaseAutoToolCall
	RequireToolResultOK        bool
	RequireToolOutput          bool
	ContinueOnREPLCodeError    bool
	RequiredREPLCodeSubstrings []string
	MaxREPLCodeLines           int
	MaxREPLCodeCommentLines    int
	IncludePriorAssistantText  bool
	FilterOverlongREPLCode     bool
	RequireScaffoldContract    bool
	FilterREPLCodeMaxTokens    int
	FilterOverlongOutput       bool
	FilterOutputMaxTokens      int
}

type REPLRunnerPhaseAutoToolCall struct {
	Tool string
	Args json.RawMessage
}

type replRunnerRunState struct {
	braidGraph *BraidGraph
}

const braidFinalHandoffSource = "rlm.braid.final_handoff"

// REPLRunner exposes a prompt-bound persistent scratch REPL to a parent model.
type REPLRunner struct {
	Config         REPLRunnerConfig
	SandboxFactory func() rlm.Sandbox
}

var _ rlm.Runner = (*REPLRunner)(nil)

type sandboxWorkDirProvider interface {
	WorkDir() string
}

// Run executes a bounded REPL-backed RLM attempt.
func (r *REPLRunner) Run(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
	if err := rlm.ValidateTask(task); err != nil {
		return rlm.Result{}, err
	}
	if err := rlm.ValidateEnvironment(env); err != nil {
		return rlm.Result{}, err
	}
	identity := PlanIdentity(task)
	task = WithTaskIdentity(task, identity)

	budgetCfg := r.Config.Budget
	if budgetCfg.MaxIterations <= 0 {
		budgetCfg.MaxIterations = firstPositive(task.MaxIterations, r.Config.LLM.MaxIterations)
	}
	if budgetCfg.MaxSubcalls <= 0 && task.MaxSubcalls > 0 {
		budgetCfg.MaxSubcalls = task.MaxSubcalls
	}
	if budgetCfg.MaxChildren <= 0 && budgetCfg.MaxSubcalls > 0 {
		budgetCfg.MaxChildren = budgetCfg.MaxSubcalls
	}
	if budgetCfg.MaxTotalNodes <= 0 && budgetCfg.MaxChildren > 0 {
		budgetCfg.MaxTotalNodes = budgetCfg.MaxChildren
	}
	if budgetCfg.MaxDepth <= 0 && task.MaxDepth > 0 {
		budgetCfg.MaxDepth = task.MaxDepth
	}
	if budgetCfg.MaxDuration <= 0 && r.Config.LLM.Timeout > 0 {
		budgetCfg.MaxDuration = r.Config.LLM.Timeout
	}
	budget, err := NewBudget(budgetCfg)
	if err != nil {
		return rlm.Result{}, err
	}

	telemetry := telemetryWithIdentity(r.Config.Telemetry, identity, task.WorkspaceID)
	recorder := NewRecorder(WithRecorderHook(func(event Event) {
		if telemetry != nil {
			telemetry.EmitRLMEvent(ctx, event)
		}
	}))
	sandboxCfg := normalizeSandboxConfig(r.Config.Sandbox, r.Config.Python)
	sandbox := r.newSandbox(sandboxCfg)
	initState := map[string]any{"prompt": task.Prompt}
	for key, value := range r.Config.InitialState {
		if strings.TrimSpace(key) == "" || key == "prompt" {
			continue
		}
		initState[key] = value
	}
	if err := sandbox.Init(ctx, initState); err != nil {
		recorder.RecordError(RuntimeErrorEvent{Code: "sandbox_init", Message: err.Error()})
		return rlm.Result{}, fmt.Errorf("rlm repl runner: init sandbox: %w", err)
	}
	sandboxWorkDir := sandboxWorkDir(sandbox)
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sandbox.Close(closeCtx)
	}()

	llmCfg := buildREPLLLMConfig(r.Config.LLM, task)
	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		recorder.RecordError(RuntimeErrorEvent{Code: "llm_config", Message: err.Error()})
		return rlm.Result{}, err
	}
	helperFactory := r.helperFactoryForTask(task)
	extraToolExecutor := r.extraToolExecutor(task)
	recursionPolicy := NormalizeRecursionPolicy(r.Config.RecursionPolicy)
	toolExec := &replToolExecutor{
		sandbox:            sandbox,
		budget:             budget,
		recorder:           recorder,
		identity:           identity,
		parentTask:         task,
		parentEnv:          env,
		rlmQuery:           r.newRLMQueryFunc(task, env),
		subcallsEnabled:    recursionPolicy != RecursionPolicyDisabled && budgetCfg.MaxSubcalls > 0 && budgetCfg.MaxDepth > 0,
		recursionPolicy:    recursionPolicy,
		replToolName:       sandboxToolName(sandboxCfg.Kind),
		sandboxKind:        sandboxCfg.Kind,
		defaultREPLCode:    strings.TrimSpace(r.Config.DefaultREPLCode),
		defaultQueryPrompt: strings.TrimSpace(r.Config.DefaultRLMQueryPrompt),
		extraToolExecutor:  extraToolExecutor,
		helperFactory:      helperFactory,
		toolResultMaxChars: r.Config.REPLToolResultMaxChars,
	}
	var asyncScheduler *Scheduler
	if r.Config.AsyncRecursion && toolExec.subcallsEnabled {
		asyncTools, scheduler, store, err := r.newAsyncRLMTools(ctx, toolExec, identity)
		if err != nil {
			recorder.RecordError(RuntimeErrorEvent{Code: "async_subcalls", Message: err.Error()})
			return rlm.Result{}, err
		}
		toolExec.asyncRLMTools = asyncTools
		toolExec.asyncNodeStore = store
		toolExec.asyncRunID = identity.RunID
		toolExec.asyncRootNodeID = replRootNodeID
		asyncScheduler = scheduler
	}
	if recursionPolicy == RecursionPolicyRequired && !toolExec.allowAsyncRLMTools() && !toolExec.allowRLMQueryTool() {
		err := fmt.Errorf("rlm repl runner: recursion policy requires rlm_query surface, but recursion tools are unavailable")
		recorder.RecordError(RuntimeErrorEvent{Code: "required_recursion_unavailable", Message: err.Error()})
		return rlm.Result{}, err
	}
	if asyncScheduler != nil {
		defer func() { _ = asyncScheduler.Close() }()
	}
	llm.SetToolRunner(engine.NewToolRunner(toolExec, nil, engine.ToolRunnerConfig{
		Workspace:   task.WorkspaceRoot,
		WorkspaceID: task.WorkspaceID,
	}))

	systemPrompt := strings.TrimSpace(r.Config.SystemPrompt)
	if systemPrompt == "" {
		helperEnabled := helperFactory != nil || extraToolNameAllowed(extraToolExecutor, EphemeralHelperSolveToolName)
		if toolExec.allowAsyncRLMTools() {
			systemPrompt = buildSandboxSystemPrompt(sandboxCfg.Kind, true, true, helperEnabled, recursionPolicy)
		} else if toolExec.allowRLMQueryTool() {
			systemPrompt = buildSandboxSystemPrompt(sandboxCfg.Kind, true, false, helperEnabled, recursionPolicy)
		} else {
			systemPrompt = buildSandboxSystemPrompt(sandboxCfg.Kind, false, false, helperEnabled, recursionPolicy)
		}
	}
	if r.Config.LLM.QwenNoThink {
		systemPrompt = qwenNoThinkREPLSystemPrompt(systemPrompt)
	}
	output, pendingRetryCount, err := r.runREPLLoop(ctx, llm, llmCfg, systemPrompt, task, toolExec, recorder)
	if err != nil {
		if len(output.ToolCalls) > 0 || len(output.ToolResults) > 0 || len(output.Iterations) > 0 {
			return partialREPLResultFromOutput(output, err, llmCfg, sandboxCfg, sandboxWorkDir, toolExec, budget, recorder, identity, r.Config.Phases), err
		}
		return rlm.Result{}, err
	}
	if r.Config.LLM.RequireToolUse && len(output.ToolCalls) == 0 {
		err := fmt.Errorf("rlm repl runner: model answered without using %s", toolExec.replToolName)
		recorder.RecordError(RuntimeErrorEvent{Code: "missing_repl_call", Message: err.Error()})
		return rlm.Result{}, err
	}
	if err := toolExec.requiredRecursionFailure(ctx); err != nil {
		recorder.RecordError(RuntimeErrorEvent{Code: "required_recursion", Message: err.Error()})
		return rlm.Result{}, err
	}
	for i := 0; i < replBudgetedIterationCount(output.Iterations); i++ {
		if err := budget.ConsumeIteration(ctx); err != nil {
			recorder.RecordBudgetEvent(BudgetEvent{Limit: LimitIterations, Message: err.Error()})
			return rlm.Result{}, err
		}
	}
	if err := budget.ConsumeParentTokens(ctx, output.Tokens.TotalTokens); err != nil {
		recorder.RecordBudgetEvent(BudgetEvent{Limit: LimitParentTokens, Message: err.Error()})
		return rlm.Result{}, err
	}

	answer := strings.TrimSpace(output.AssistantText)
	sanitizedAnswer, sanitization := rlm.SanitizeOutputText(answer)
	answer = sanitizedAnswer
	var finalizedFromEphemeral bool
	var ephemeralFinalRaw string
	if r.Config.ExtractSolutionLine {
		if finalAnswer, raw, ok := finalAnswerFromEphemeralSkillRun(output.ToolResults); ok {
			answer = finalAnswer
			ephemeralFinalRaw = raw
			finalizedFromEphemeral = true
		}
	}
	var solutionExtracted bool
	var solutionFound bool
	var solutionRawText string
	if (r.Config.ExtractSolutionLine || r.Config.FinalSolutionLineRequired) && !finalizedFromEphemeral {
		if line, ok := rlm.ExtractSolutionLine(answer); ok {
			solutionFound = true
			if line != answer {
				solutionRawText = answer
				solutionExtracted = true
			}
			answer = line
		}
	}
	if r.Config.FinalSolutionLineRequired && !finalizedFromEphemeral && !solutionFound {
		err := fmt.Errorf("rlm repl runner: final answer did not contain a solution = line")
		recorder.RecordError(RuntimeErrorEvent{
			Code:             "missing_solution_line",
			Message:          err.Error(),
			RawChars:         len(output.AssistantText),
			SanitizedChars:   len(answer),
			RawExcerpt:       safeTelemetryExcerpt(output.AssistantText, 600),
			SanitizedExcerpt: safeTelemetryExcerpt(answer, 600),
		})
		return rlm.Result{}, err
	}
	if answer == "" {
		err := fmt.Errorf("rlm repl runner: empty assistant response")
		recorder.RecordError(RuntimeErrorEvent{
			Code:             "empty_response",
			Message:          err.Error(),
			RawChars:         len(output.AssistantText),
			SanitizedChars:   len(answer),
			RawExcerpt:       safeTelemetryExcerpt(output.AssistantText, 600),
			SanitizedExcerpt: safeTelemetryExcerpt(answer, 600),
			Artifacts:        append([]string(nil), sanitization.Artifacts...),
		})
		return rlm.Result{}, err
	}
	recorder.RecordFinalAnswer(FinalAnswerEvent{Text: answer, Tokens: output.Tokens.OutputTokens})

	toolNames := toolCallNames(output.ToolCalls)
	subcallSummary := toolExec.subcallSummaryContext(ctx)
	runnerName := "rlm_repl_no_recursion"
	if toolExec.allowAsyncRLMTools() {
		runnerName = "rlm_repl_with_async_recursion"
	} else if toolExec.allowRLMQueryTool() {
		runnerName = "rlm_repl_with_recursion"
	}
	metadata := map[string]any{
		"runner":                  runnerName,
		"stop_reason":             output.StopReason,
		"provider":                llmCfg.Provider,
		"model":                   llmCfg.Model,
		"tool_calls":              len(output.ToolCalls),
		"tool_names":              toolNames,
		"repl_calls":              countToolName(toolNames, toolExec.replToolName),
		"require_tool_use":        r.Config.LLM.RequireToolUse,
		"sandbox_kind":            string(sandboxCfg.Kind),
		"sandbox_work_dir":        sandboxWorkDir,
		"repl_tool_name":          toolExec.replToolName,
		"parent_input_tokens":     output.Tokens.InputTokens,
		"parent_output_tokens":    output.Tokens.OutputTokens,
		"parent_total_tokens":     output.Tokens.TotalTokens,
		"parent_iteration_count":  len(output.Iterations),
		"parent_pending_retries":  pendingRetryCount,
		"parent_tool_usage":       summarizeToolUsage(output.Iterations),
		"budget_snapshot":         budget.Snapshot(),
		"trajectory_events":       recorder.Events(),
		"python_repl_tool_name":   PythonREPLToolName,
		"go_repl_tool_name":       GoREPLToolName,
		"rlm_query_tool_name":     RLMQueryToolName,
		"rlm_wait_tool_name":      RLMWaitToolName,
		"rlm_result_tool_name":    RLMResultToolName,
		"async_recursion":         toolExec.allowAsyncRLMTools(),
		"recursion_policy":        string(recursionPolicy),
		"recursive_subcalls_used": subcallSummary.Calls,
		"child_input_tokens":      subcallSummary.InputTokens,
		"child_output_tokens":     subcallSummary.OutputTokens,
		"child_total_tokens":      subcallSummary.TotalTokens,
		"run_id":                  identity.RunID,
		"agent_id":                identity.AgentID,
		"parent_agent_id":         identity.ParentAgentID,
		"output_root":             identity.OutputRoot,
		"output_namespace":        identity.OutputNamespace,
	}
	if toolExec.allowAsyncRLMTools() {
		if trace, traceErr := buildRecursiveTrace(ctx, toolExec.asyncNodeStore, toolExec.asyncRunID, toolExec.asyncRootNodeID); traceErr == nil && trace != nil {
			metadata["recursive_trace"] = trace
		}
	}
	if phases := replPhaseNames(r.Config.Phases); len(phases) > 0 {
		metadata["staged_phases"] = phases
	}
	if sanitization.Changed {
		metadata["output_sanitization"] = map[string]any{
			"changed":   true,
			"artifacts": append([]string(nil), sanitization.Artifacts...),
			"raw_text":  sanitization.RawText,
		}
	}
	if solutionExtracted {
		metadata["solution_extraction"] = map[string]any{
			"changed":  true,
			"raw_text": solutionRawText,
		}
	}
	if finalizedFromEphemeral {
		metadata["ephemeral_skill_finalization"] = map[string]any{
			"changed":  true,
			"raw_text": ephemeralFinalRaw,
			"source":   EphemeralSkillRunToolName,
		}
	}
	if helperTrace, ok := helperFactoryTraceFromToolResults(output.ToolResults); ok {
		metadata["ephemeral_helper"] = helperTrace
	}
	return rlm.Result{
		Answer:     answer,
		Iterations: len(output.Iterations),
		Subcalls:   subcallSummary.Calls,
		Metadata:   metadata,
	}, nil
}

func partialREPLResultFromOutput(
	output engine.EngineOutput,
	runErr error,
	llmCfg engine.LLMChatConfig,
	sandboxCfg SandboxConfig,
	sandboxWorkDir string,
	toolExec *replToolExecutor,
	budget *Budget,
	recorder *Recorder,
	identity IdentityPlan,
	phases []REPLRunnerPhase,
) rlm.Result {
	toolNames := toolCallNames(output.ToolCalls)
	subcallSummary := toolExec.subcallSummaryContext(context.Background())
	metadata := map[string]any{
		"runner":                  "rlm_repl_partial",
		"error":                   strings.TrimSpace(runErr.Error()),
		"stop_reason":             output.StopReason,
		"provider":                llmCfg.Provider,
		"model":                   llmCfg.Model,
		"tool_calls":              len(output.ToolCalls),
		"tool_names":              toolNames,
		"repl_calls":              countToolName(toolNames, toolExec.replToolName),
		"require_tool_use":        true,
		"sandbox_kind":            string(sandboxCfg.Kind),
		"sandbox_work_dir":        sandboxWorkDir,
		"repl_tool_name":          toolExec.replToolName,
		"parent_input_tokens":     output.Tokens.InputTokens,
		"parent_output_tokens":    output.Tokens.OutputTokens,
		"parent_total_tokens":     output.Tokens.TotalTokens,
		"parent_iteration_count":  len(output.Iterations),
		"parent_tool_usage":       summarizeToolUsage(output.Iterations),
		"budget_snapshot":         budget.Snapshot(),
		"trajectory_events":       recorder.Events(),
		"python_repl_tool_name":   PythonREPLToolName,
		"go_repl_tool_name":       GoREPLToolName,
		"rlm_query_tool_name":     RLMQueryToolName,
		"rlm_wait_tool_name":      RLMWaitToolName,
		"rlm_result_tool_name":    RLMResultToolName,
		"async_recursion":         toolExec.allowAsyncRLMTools(),
		"recursion_policy":        string(toolExec.recursionPolicy),
		"recursive_subcalls_used": subcallSummary.Calls,
		"child_input_tokens":      subcallSummary.InputTokens,
		"child_output_tokens":     subcallSummary.OutputTokens,
		"child_total_tokens":      subcallSummary.TotalTokens,
		"run_id":                  identity.RunID,
		"agent_id":                identity.AgentID,
		"parent_agent_id":         identity.ParentAgentID,
		"output_root":             identity.OutputRoot,
		"output_namespace":        identity.OutputNamespace,
	}
	if names := replPhaseNames(phases); len(names) > 0 {
		metadata["staged_phases"] = names
	}
	if helperTrace, ok := helperFactoryTraceFromToolResults(output.ToolResults); ok {
		metadata["ephemeral_helper"] = helperTrace
	}
	answer := strings.TrimSpace(output.AssistantText)
	answer, _ = rlm.SanitizeOutputText(answer)
	return rlm.Result{
		Answer:     answer,
		Iterations: len(output.Iterations),
		Subcalls:   subcallSummary.Calls,
		Metadata:   metadata,
	}
}

func sandboxWorkDir(sandbox rlm.Sandbox) string {
	provider, ok := sandbox.(sandboxWorkDirProvider)
	if !ok || provider == nil {
		return ""
	}
	return strings.TrimSpace(provider.WorkDir())
}

func (r *REPLRunner) extraToolExecutor(task rlm.Task) engine.ToolExecutor {
	var executors []engine.ToolExecutor
	if r.Config.ExtraToolExecutor != nil {
		executors = append(executors, r.Config.ExtraToolExecutor)
	}
	if r.Config.HelperFactory != nil {
		executors = append(executors, r.helperFactoryForTask(task))
	} else if r.Config.EphemeralSkills {
		executors = append(executors, &EphemeralSkillTools{})
	}
	switch len(executors) {
	case 0:
		return nil
	case 1:
		return executors[0]
	default:
		return MultiToolExecutor(executors)
	}
}

func (r *REPLRunner) helperFactoryForTask(task rlm.Task) *HelperFactoryTools {
	if r == nil || r.Config.HelperFactory == nil {
		return nil
	}
	helperCfg := *r.Config.HelperFactory
	if strings.TrimSpace(helperCfg.TaskPrompt) == "" {
		helperCfg.TaskPrompt = task.Prompt
	}
	if strings.TrimSpace(helperCfg.LLM.Provider) == "" &&
		strings.TrimSpace(helperCfg.LLM.Model) == "" &&
		strings.TrimSpace(helperCfg.LLM.BaseURL) == "" {
		helperCfg.LLM = r.Config.LLM
	}
	return &HelperFactoryTools{Config: helperCfg}
}

func (r *REPLRunner) runREPLLoop(
	ctx context.Context,
	llm *engine.LLMChatEngine,
	llmCfg engine.LLMChatConfig,
	systemPrompt string,
	task rlm.Task,
	toolExec *replToolExecutor,
	recorder *Recorder,
) (engine.EngineOutput, int, error) {
	if len(r.Config.Phases) > 0 {
		return r.runStagedREPLLoop(ctx, llm, llmCfg, systemPrompt, task, toolExec, recorder)
	}
	return r.runDefaultREPLLoop(ctx, llm, llmCfg, systemPrompt, task, toolExec, recorder)
}

func (r *REPLRunner) runDefaultREPLLoop(
	ctx context.Context,
	llm *engine.LLMChatEngine,
	llmCfg engine.LLMChatConfig,
	systemPrompt string,
	task rlm.Task,
	toolExec *replToolExecutor,
	recorder *Recorder,
) (engine.EngineOutput, int, error) {
	var output engine.EngineOutput
	messages := []engine.Message{engine.NewUserMessage(task.Prompt)}
	attempts := 1
	if toolExec.allowAsyncRLMTools() {
		attempts = pendingSubcallMaxAttempts
	}
	pendingRetryCount := 0
	availableTools := toolExec.List()
	for attempt := 1; attempt <= attempts; attempt++ {
		attemptOutput, err := llm.Run(ctx, engine.EngineInput{
			SystemPrompt: systemPrompt,
			Messages:     messages,
			Tools:        availableTools,
			Workspace:    task.WorkspaceRoot,
			MaxTokens:    llmCfg.MaxTokens,
			Temperature:  llmCfg.Temperature,
		})
		output = mergeEngineOutputs(output, attemptOutput)
		recordParentLLMIterations(recorder, llmCfg.Model, attemptOutput, len(output.Iterations)-len(attemptOutput.Iterations))
		if err := validateREPLAttemptOutput(attemptOutput, err, llmCfg.MaxTokens); err != nil {
			if repairOutput, repaired, repairErr := r.repairToolErrorIfNeeded(ctx, llm, llmCfg, systemPrompt, task, output, err); repaired {
				recordParentLLMIterations(recorder, llmCfg.Model, repairOutput, len(output.Iterations))
				output = mergeEngineOutputs(output, repairOutput)
				if repairErr == nil {
					return output, pendingRetryCount, nil
				}
				err = repairErr
			}
			recorder.RecordError(parentLLMRuntimeError("parent_llm", err, attemptOutput))
			return output, pendingRetryCount, err
		}
		if err := toolExec.requiredSubcallFailure(ctx); err != nil {
			recorder.RecordError(RuntimeErrorEvent{Code: "required_subcalls", Message: err.Error()})
			return output, pendingRetryCount, err
		}
		if err := toolExec.unfinishedSubcallFailure(ctx); err != nil {
			if attempt == attempts {
				recorder.RecordError(RuntimeErrorEvent{Code: "pending_subcalls", Message: err.Error()})
				return output, pendingRetryCount, err
			}
			pendingRetryCount++
			recorder.RecordError(RuntimeErrorEvent{Code: "pending_subcalls_retry", Message: err.Error()})
			messages = []engine.Message{engine.NewUserMessage(pendingSubcallRetryPrompt(task.Prompt, err))}
			availableTools = toolExec.pendingSubcallCorrectionTools()
			continue
		}
		if err := toolExec.staleSubcallWaitFailure(ctx); err != nil {
			if attempt == attempts {
				recorder.RecordError(RuntimeErrorEvent{Code: "stale_subcall_wait", Message: err.Error()})
				return output, pendingRetryCount, err
			}
			pendingRetryCount++
			recorder.RecordError(RuntimeErrorEvent{Code: "stale_subcall_wait_retry", Message: err.Error()})
			messages = []engine.Message{engine.NewUserMessage(pendingSubcallRetryPrompt(task.Prompt, err))}
			availableTools = toolExec.pendingSubcallCorrectionTools()
			continue
		}
		break
	}
	return output, pendingRetryCount, nil
}

func (r *REPLRunner) runStagedREPLLoop(
	ctx context.Context,
	llm *engine.LLMChatEngine,
	llmCfg engine.LLMChatConfig,
	systemPrompt string,
	task rlm.Task,
	toolExec *replToolExecutor,
	recorder *Recorder,
) (engine.EngineOutput, int, error) {
	var output engine.EngineOutput
	state := replRunnerRunState{}
	allTools := toolExec.List()
	for idx, phase := range r.Config.Phases {
		phaseName := strings.TrimSpace(phase.Name)
		if phaseName == "" {
			phaseName = fmt.Sprintf("phase-%d", idx+1)
		}
		phaseTools := filterPhaseTools(phase, filterREPLToolDefs(allTools, phase.Tools))
		if strings.TrimSpace(phase.OutputKind) == REPLPhaseOutputKindREPLCode {
			phaseTools = nil
		}
		if phase.Final {
			phaseTools = nil
		}
		if phase.Final && r.Config.FinalAnswerFromVerifiedHandoff {
			if answer, ok := verifiedAnswerFromLatestBraidFinalHandoff(output); ok {
				attemptOutput := engine.EngineOutput{
					AssistantText: answer,
					StopReason:    engine.StopReasonEndTurn,
					InjectedContexts: []engine.InjectedContext{{
						ToolCallID: "braid-final-forward",
						Source:     "rlm.braid.final_answer_forwarded",
						Content:    answer,
					}},
				}
				output = mergeEngineOutputs(output, attemptOutput)
				continue
			}
		}
		if phase.AutoExecuteRequiredTool {
			attemptOutput := engine.EngineOutput{}
			if err := autoExecutePhaseRequiredTool(ctx, phaseName, phase, toolExec, &attemptOutput); err != nil {
				recorder.RecordError(RuntimeErrorEvent{Code: "phase_" + phaseName, Message: err.Error()})
				output = mergeEngineOutputs(output, attemptOutput)
				return output, 0, err
			}
			output = mergeEngineOutputs(output, attemptOutput)
			if err := validateREPLPhaseOutput(phase, attemptOutput); err != nil {
				recorder.RecordError(RuntimeErrorEvent{Code: "phase_" + phaseName, Message: err.Error()})
				return output, 0, err
			}
			continue
		}
		if phase.AutoExecuteGraphNodes && strings.TrimSpace(phase.OutputKind) != REPLPhaseOutputKindBraidGraph {
			attemptOutput := engine.EngineOutput{}
			if err := executePhaseBraidGraph(
				ctx,
				phaseName,
				phase,
				toolExec,
				state.braidGraph,
				task.Prompt,
				replBraidGraphAutoFanoutCap(task, toolExec),
				&attemptOutput,
			); err != nil {
				recorder.RecordError(RuntimeErrorEvent{Code: "phase_" + phaseName, Message: err.Error()})
				output = mergeEngineOutputs(output, attemptOutput)
				return output, 0, err
			}
			output = mergeEngineOutputs(output, attemptOutput)
			if err := validateREPLPhaseOutput(phase, attemptOutput); err != nil {
				recorder.RecordError(RuntimeErrorEvent{Code: "phase_" + phaseName, Message: err.Error()})
				return output, 0, err
			}
			continue
		}
		phaseLLM := llm
		forcedToolChoice := forcedToolChoiceForPhase(phase, phaseTools)
		phaseMaxTokens := firstPositiveInt(phase.MaxTokens, llmCfg.MaxTokens)
		if phase.MaxIterations > 0 || phase.MaxTokens > 0 || len(forcedToolChoice) > 0 || len(phase.ResponseFormat) > 0 {
			phaseLLMCfg := llmCfg
			phaseLLMCfg.MaxIterations = phase.MaxIterations
			phaseLLMCfg.MaxTokens = phaseMaxTokens
			if len(phase.ResponseFormat) > 0 {
				phaseLLMCfg.ResponseFormat = normalizeREPLPhaseResponseFormat(phaseLLMCfg.Provider, phase.ResponseFormat)
			}
			if phase.AutoExecuteRequiredTool {
				phaseLLMCfg.ToolChoice = nil
				phaseLLMCfg.ParseReasoningToolCalls = false
			}
			if len(forcedToolChoice) > 0 {
				phaseLLMCfg.ToolChoice = forcedToolChoice
			}
			var phaseErr error
			phaseLLM, phaseErr = engine.NewLLMChatEngine(phaseLLMCfg)
			if phaseErr != nil {
				recorder.RecordError(RuntimeErrorEvent{Code: "phase_" + phaseName, Message: phaseErr.Error()})
				return engine.EngineOutput{}, 0, phaseErr
			}
			phaseLLM.SetToolRunner(engine.NewToolRunner(toolExec, nil, engine.ToolRunnerConfig{
				Workspace:   task.WorkspaceRoot,
				WorkspaceID: task.WorkspaceID,
			}))
		}
		attemptOutput, err := runREPLLLMWithTransientRetry(ctx, phaseLLM, engine.EngineInput{
			SystemPrompt: systemPrompt,
			Messages: []engine.Message{engine.NewUserMessage(buildREPLPhasePrompt(
				task.Prompt,
				phase,
				output,
				state,
			))},
			Tools:       phaseTools,
			Workspace:   task.WorkspaceRoot,
			MaxTokens:   phaseMaxTokens,
			Temperature: llmCfg.Temperature,
		})
		if phase.FilterOverlongOutput && strings.TrimSpace(phase.OutputKind) == REPLPhaseOutputKindCyclePacket && attemptOutput.StopReason == engine.StopReasonMaxTokens && strings.TrimSpace(attemptOutput.AssistantText) != "" {
			filterOutput, filterErr := runCyclePacketFilter(ctx, llmCfg, systemPrompt, task, phase, output, attemptOutput.AssistantText)
			attemptOutput = mergeEngineOutputs(attemptOutput, filterOutput)
			if filterErr != nil {
				err = filterErr
			} else {
				attemptOutput.AssistantText = filterOutput.AssistantText
				attemptOutput.StopReason = filterOutput.StopReason
			}
		}
		validationErr := validateREPLAttemptOutputForPhase(phase, attemptOutput, err, phaseMaxTokens)
		skipREPLCodeExecution := false
		if validationErr != nil &&
			strings.TrimSpace(phase.OutputKind) == REPLPhaseOutputKindREPLCode &&
			phase.ContinueOnREPLCodeError {
			appendREPLCodeFailureContext(phaseName, phase, &attemptOutput, validationErr)
			validationErr = nil
			skipREPLCodeExecution = true
		}
		if phase.Final {
			repairOutput, repaired, repairErr := r.repairFinalSolutionLineIfNeeded(ctx, phaseLLM, llmCfg, systemPrompt, task, phase, output, attemptOutput, validationErr != nil)
			if repaired {
				attemptOutput = mergeEngineOutputs(attemptOutput, repairOutput)
			}
			if repairErr != nil {
				if validationErr == nil {
					validationErr = repairErr
				}
			} else if repaired {
				validationErr = nil
			}
			if validationErr == nil || shouldRepairStructuredFinalAfterAttemptError(phase, attemptOutput) {
				repairOutput, repaired, repairErr := r.repairStructuredFinalIfNeeded(ctx, phaseLLM, llmCfg, systemPrompt, task, phase, output, attemptOutput)
				if repaired {
					attemptOutput = mergeEngineOutputs(attemptOutput, repairOutput)
				}
				if repairErr != nil {
					validationErr = repairErr
				} else if repaired {
					validationErr = nil
				}
			}
		}
		if validationErr != nil {
			recorder.RecordError(parentLLMRuntimeError("phase_"+phaseName, validationErr, attemptOutput))
			output = mergeEngineOutputs(output, attemptOutput)
			recordParentLLMIterations(recorder, llmCfg.Model, attemptOutput, len(output.Iterations)-len(attemptOutput.Iterations))
			return output, 0, validationErr
		}
		if strings.TrimSpace(phase.OutputKind) == REPLPhaseOutputKindREPLCode && !skipREPLCodeExecution {
			if err := r.executeAndRepairPhaseREPLCode(ctx, phaseLLM, llmCfg, systemPrompt, task, phaseName, phase, output, toolExec, &attemptOutput, phaseMaxTokens); err != nil {
				recorder.RecordError(RuntimeErrorEvent{Code: "phase_" + phaseName, Message: err.Error()})
				output = mergeEngineOutputs(output, attemptOutput)
				recordParentLLMIterations(recorder, llmCfg.Model, attemptOutput, len(output.Iterations)-len(attemptOutput.Iterations))
				return output, 0, err
			}
		}
		if strings.TrimSpace(phase.OutputKind) == REPLPhaseOutputKindCycleWitness {
			if err := autoCheckPhaseCycleWitness(phaseName, &attemptOutput); err != nil {
				recorder.RecordError(RuntimeErrorEvent{Code: "phase_" + phaseName, Message: err.Error()})
				output = mergeEngineOutputs(output, attemptOutput)
				recordParentLLMIterations(recorder, llmCfg.Model, attemptOutput, len(output.Iterations)-len(attemptOutput.Iterations))
				return output, 0, err
			}
		}
		if strings.TrimSpace(phase.OutputKind) == REPLPhaseOutputKindBraidGraph {
			phaseCap := replBraidGraphValidationCap(phase, toolExec)
			graph, graphRepairOutput, graphErr := r.parseAndRepairBraidGraphPhaseOutput(
				ctx,
				phaseLLM,
				llmCfg,
				systemPrompt,
				task,
				phase,
				output,
				attemptOutput,
				phaseCap,
			)
			if len(graphRepairOutput.Iterations) > 0 || len(graphRepairOutput.ToolCalls) > 0 || len(graphRepairOutput.ToolResults) > 0 {
				attemptOutput = mergeEngineOutputs(attemptOutput, graphRepairOutput)
			}
			if graphErr != nil {
				recorder.RecordError(parentLLMRuntimeError("phase_"+phaseName, graphErr, attemptOutput))
				output = mergeEngineOutputs(output, attemptOutput)
				recordParentLLMIterations(recorder, llmCfg.Model, attemptOutput, len(output.Iterations)-len(attemptOutput.Iterations))
				return output, 0, graphErr
			}
			graphCopy := graph
			state.braidGraph = &graphCopy
			if phase.AutoExecuteGraphNodes {
				if err := executePhaseBraidGraph(
					ctx,
					phaseName,
					phase,
					toolExec,
					state.braidGraph,
					task.Prompt,
					replBraidGraphAutoFanoutCap(task, toolExec),
					&attemptOutput,
				); err != nil {
					recorder.RecordError(RuntimeErrorEvent{Code: "phase_" + phaseName, Message: err.Error()})
					output = mergeEngineOutputs(output, attemptOutput)
					recordParentLLMIterations(recorder, llmCfg.Model, attemptOutput, len(output.Iterations)-len(attemptOutput.Iterations))
					return output, 0, err
				}
			}
		}
		recordParentLLMIterations(recorder, llmCfg.Model, attemptOutput, len(output.Iterations))
		if err := autoExecutePhaseRequiredTool(ctx, phaseName, phase, toolExec, &attemptOutput); err != nil {
			recorder.RecordError(RuntimeErrorEvent{Code: "phase_" + phaseName, Message: err.Error()})
			output = mergeEngineOutputs(output, attemptOutput)
			return output, 0, err
		}
		output = mergeEngineOutputs(output, attemptOutput)
		if err := validateREPLPhaseOutput(phase, attemptOutput); err != nil {
			recorder.RecordError(RuntimeErrorEvent{Code: "phase_" + phaseName, Message: err.Error()})
			return output, 0, err
		}
	}
	if err := toolExec.requiredSubcallFailure(ctx); err != nil {
		recorder.RecordError(RuntimeErrorEvent{Code: "required_subcalls", Message: err.Error()})
		return output, 0, err
	}
	if err := toolExec.unfinishedSubcallFailure(ctx); err != nil {
		recorder.RecordError(RuntimeErrorEvent{Code: "pending_subcalls", Message: err.Error()})
		return output, 0, err
	}
	if err := toolExec.staleSubcallWaitFailure(ctx); err != nil {
		recorder.RecordError(RuntimeErrorEvent{Code: "stale_subcall_wait", Message: err.Error()})
		return output, 0, err
	}
	if r.Config.RejectFailedSubcalls {
		if err := toolExec.failedSubcallFailure(ctx); err != nil {
			recorder.RecordError(RuntimeErrorEvent{Code: "failed_subcalls", Message: err.Error()})
			return output, 0, err
		}
	}
	return output, 0, nil
}

func (r *REPLRunner) repairToolErrorIfNeeded(
	ctx context.Context,
	llm *engine.LLMChatEngine,
	llmCfg engine.LLMChatConfig,
	systemPrompt string,
	task rlm.Task,
	prior engine.EngineOutput,
	validationErr error,
) (engine.EngineOutput, bool, error) {
	if r == nil || r.Config.ToolErrorRepairMaxAttempts <= 0 || validationErr == nil {
		return engine.EngineOutput{}, false, nil
	}
	var merged engine.EngineOutput
	errText := validationErr.Error()
	for attempt := 1; attempt <= r.Config.ToolErrorRepairMaxAttempts; attempt++ {
		repairOutput, err := llm.Run(ctx, engine.EngineInput{
			SystemPrompt: systemPrompt,
			Messages: []engine.Message{engine.NewUserMessage(buildToolErrorRepairPrompt(
				task.Prompt,
				prior,
				errText,
				attempt,
				r.Config.ToolErrorRepairMaxAttempts,
			))},
			Tools:       nil,
			Workspace:   task.WorkspaceRoot,
			MaxTokens:   llmCfg.MaxTokens,
			Temperature: llmCfg.Temperature,
		})
		merged = mergeEngineOutputs(merged, repairOutput)
		if err := validateREPLAttemptOutput(repairOutput, err, llmCfg.MaxTokens); err != nil {
			errText = err.Error()
			continue
		}
		if strings.TrimSpace(repairOutput.AssistantText) != "" {
			return merged, true, nil
		}
		errText = "repair attempt returned an empty answer"
	}
	return merged, true, fmt.Errorf("rlm repl runner: tool-error repair failed after %d attempt(s): %s", r.Config.ToolErrorRepairMaxAttempts, errText)
}

func (r *REPLRunner) repairFinalSolutionLineIfNeeded(
	ctx context.Context,
	llm *engine.LLMChatEngine,
	llmCfg engine.LLMChatConfig,
	systemPrompt string,
	task rlm.Task,
	phase REPLRunnerPhase,
	prior engine.EngineOutput,
	finalOutput engine.EngineOutput,
	force bool,
) (engine.EngineOutput, bool, error) {
	if r == nil || !r.Config.FinalSolutionLineRequired {
		return engine.EngineOutput{}, false, nil
	}
	if !force {
		if _, ok := rlm.ExtractSolutionLine(finalOutput.AssistantText); ok {
			return engine.EngineOutput{}, false, nil
		}
	}
	if force && strings.TrimSpace(finalOutput.AssistantText) == "" {
		return engine.EngineOutput{}, false, nil
	}
	attempts := r.Config.FinalAnswerRepairMaxAttempts
	if attempts <= 0 {
		return engine.EngineOutput{}, false, fmt.Errorf("rlm repl runner: final answer did not contain a solution = line")
	}
	var merged engine.EngineOutput
	invalid := finalOutput.AssistantText
	for attempt := 1; attempt <= attempts; attempt++ {
		repairOutput, err := llm.Run(ctx, engine.EngineInput{
			SystemPrompt: systemPrompt,
			Messages: []engine.Message{engine.NewUserMessage(buildFinalSolutionLineRepairPrompt(
				task.Prompt,
				phase,
				prior,
				invalid,
				attempt,
				attempts,
			))},
			Tools:       nil,
			Workspace:   task.WorkspaceRoot,
			MaxTokens:   llmCfg.MaxTokens,
			Temperature: llmCfg.Temperature,
		})
		merged = mergeEngineOutputs(merged, repairOutput)
		if err := validateREPLAttemptOutput(repairOutput, err, llmCfg.MaxTokens); err != nil {
			return merged, true, err
		}
		if _, ok := rlm.ExtractSolutionLine(repairOutput.AssistantText); ok {
			return merged, true, nil
		}
		invalid = repairOutput.AssistantText
	}
	return merged, true, fmt.Errorf("rlm repl runner: final answer repair did not produce a solution = line")
}

func (r *REPLRunner) repairStructuredFinalIfNeeded(
	ctx context.Context,
	llm *engine.LLMChatEngine,
	llmCfg engine.LLMChatConfig,
	systemPrompt string,
	task rlm.Task,
	phase REPLRunnerPhase,
	prior engine.EngineOutput,
	finalOutput engine.EngineOutput,
) (engine.EngineOutput, bool, error) {
	if r == nil || strings.TrimSpace(phase.FinalOutputKind) == "" {
		return engine.EngineOutput{}, false, nil
	}
	validationErr := validateFinalOutputKind(phase, finalOutput.AssistantText)
	if validationErr == nil {
		if finalOutput.StopReason != engine.StopReasonMaxTokens {
			return engine.EngineOutput{}, false, nil
		}
		validationErr = fmt.Errorf("structured final exceeded max tokens and must be compacted")
	}
	attempts := r.Config.FinalAnswerRepairMaxAttempts
	if attempts <= 0 {
		return engine.EngineOutput{}, false, validationErr
	}
	var merged engine.EngineOutput
	invalid := finalOutput.AssistantText
	for attempt := 1; attempt <= attempts; attempt++ {
		repairOutput, err := llm.Run(ctx, engine.EngineInput{
			SystemPrompt: systemPrompt,
			Messages: []engine.Message{engine.NewUserMessage(buildStructuredFinalRepairPrompt(
				task.Prompt,
				phase,
				prior,
				invalid,
				validationErr,
				attempt,
				attempts,
			))},
			Tools:       nil,
			Workspace:   task.WorkspaceRoot,
			MaxTokens:   structuredFinalRepairMaxTokens(llmCfg.MaxTokens),
			Temperature: llmCfg.Temperature,
		})
		merged = mergeEngineOutputs(merged, repairOutput)
		if err := validateStructuredFinalRepairAttempt(repairOutput, err); err != nil {
			validationErr = err
			invalid = repairOutput.AssistantText
			continue
		}
		if err := validateFinalOutputKind(phase, repairOutput.AssistantText); err == nil {
			return merged, true, nil
		} else {
			validationErr = err
			invalid = repairOutput.AssistantText
		}
	}
	return merged, true, fmt.Errorf("rlm repl runner: final output repair failed for %s: %w", strings.TrimSpace(phase.FinalOutputKind), validationErr)
}

func (r *REPLRunner) executeAndRepairPhaseREPLCode(
	ctx context.Context,
	llm *engine.LLMChatEngine,
	llmCfg engine.LLMChatConfig,
	systemPrompt string,
	task rlm.Task,
	phaseName string,
	phase REPLRunnerPhase,
	prior engine.EngineOutput,
	toolExec *replToolExecutor,
	output *engine.EngineOutput,
	maxTokens int,
) error {
	err := autoExecutePhaseREPLCode(ctx, phaseName, phase, toolExec, output)
	if err == nil {
		return nil
	}
	if phase.FilterOverlongREPLCode && output != nil && output.StopReason == engine.StopReasonMaxTokens && strings.TrimSpace(output.AssistantText) != "" {
		filterMaxTokens := replCodeFilterMaxTokens(phase, maxTokens)
		filterLLMCfg := llmCfg
		filterLLMCfg.MaxTokens = filterMaxTokens
		filterLLMCfg.MaxIterations = 1
		filterLLMCfg.ToolChoice = nil
		filterLLMCfg.ParseReasoningToolCalls = false
		filterLLM, filterEngineErr := engine.NewLLMChatEngine(filterLLMCfg)
		if filterEngineErr != nil {
			return filterEngineErr
		}
		filterOutput, filterErr := filterLLM.Run(ctx, engine.EngineInput{
			SystemPrompt: systemPrompt,
			Messages: []engine.Message{engine.NewUserMessage(buildREPLCodeFilterPrompt(
				task.Prompt,
				phase,
				prior,
				output.AssistantText,
			))},
			Tools:       nil,
			Workspace:   task.WorkspaceRoot,
			MaxTokens:   filterMaxTokens,
			Temperature: llmCfg.Temperature,
		})
		*output = mergeEngineOutputs(*output, filterOutput)
		if validationErr := validateREPLAttemptOutputForPhase(phase, filterOutput, filterErr, filterMaxTokens); validationErr != nil {
			err = fmt.Errorf("rlm repl runner phase %q filter overlong repl_code output: %w", phaseName, validationErr)
		} else if execErr := autoExecutePhaseREPLCode(ctx, phaseName, phase, toolExec, &filterOutput); execErr != nil {
			*output = mergeEngineOutputs(*output, filterOutput)
			err = execErr
		} else {
			*output = mergeEngineOutputs(*output, filterOutput)
			return nil
		}
	}
	if r == nil || r.Config.ToolErrorRepairMaxAttempts <= 0 || output == nil {
		if phase.ContinueOnREPLCodeError {
			appendREPLCodeFailureContext(phaseName, phase, output, err)
			return nil
		}
		return err
	}
	errText := err.Error()
	invalidCode := output.AssistantText
	for attempt := 1; attempt <= r.Config.ToolErrorRepairMaxAttempts; attempt++ {
		repairOutput, runErr := llm.Run(ctx, engine.EngineInput{
			SystemPrompt: systemPrompt,
			Messages: []engine.Message{engine.NewUserMessage(buildREPLCodeRepairPrompt(
				task.Prompt,
				phase,
				prior,
				invalidCode,
				errText,
				attempt,
				r.Config.ToolErrorRepairMaxAttempts,
			))},
			Tools:       nil,
			Workspace:   task.WorkspaceRoot,
			MaxTokens:   maxTokens,
			Temperature: llmCfg.Temperature,
		})
		*output = mergeEngineOutputs(*output, repairOutput)
		if validationErr := validateREPLAttemptOutputForPhase(phase, repairOutput, runErr, maxTokens); validationErr != nil {
			errText = validationErr.Error()
			invalidCode = repairOutput.AssistantText
			continue
		}
		if execErr := autoExecutePhaseREPLCode(ctx, phaseName, phase, toolExec, &repairOutput); execErr != nil {
			*output = mergeEngineOutputs(*output, repairOutput)
			errText = execErr.Error()
			invalidCode = repairOutput.AssistantText
			continue
		}
		*output = mergeEngineOutputs(*output, repairOutput)
		return nil
	}
	finalErr := fmt.Errorf("rlm repl runner: repl_code repair failed after %d attempt(s): %s", r.Config.ToolErrorRepairMaxAttempts, errText)
	if phase.ContinueOnREPLCodeError {
		appendREPLCodeFailureContext(phaseName, phase, output, finalErr)
		return nil
	}
	return finalErr
}

func (r *REPLRunner) parseAndRepairBraidGraphPhaseOutput(
	ctx context.Context,
	llm *engine.LLMChatEngine,
	llmCfg engine.LLMChatConfig,
	systemPrompt string,
	task rlm.Task,
	phase REPLRunnerPhase,
	prior engine.EngineOutput,
	phaseOutput engine.EngineOutput,
	maxNodes int,
) (BraidGraph, engine.EngineOutput, error) {
	graph, err := ParseBraidGraphText(phaseOutput.AssistantText)
	if err == nil {
		graph = NormalizeBraidGraphForPolicy(graph, phase.BraidGraphPolicy, maxNodes)
		if validateErr := validateBraidGraphForPhase(phase, graph, maxNodes); validateErr == nil {
			return graph, engine.EngineOutput{}, nil
		} else {
			err = validateErr
		}
	}
	initialErr := err

	repairOutput, runErr := runREPLLLMWithTransientRetry(ctx, llm, engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages: []engine.Message{engine.NewUserMessage(buildBraidGraphRepairPrompt(
			task.Prompt,
			phase,
			prior,
			phaseOutput.AssistantText,
			err,
			maxNodes,
		))},
		Tools:       nil,
		Workspace:   task.WorkspaceRoot,
		MaxTokens:   llmCfg.MaxTokens,
		Temperature: llmCfg.Temperature,
	})
	if validationErr := validateREPLAttemptOutput(repairOutput, runErr, llmCfg.MaxTokens); validationErr != nil {
		return BraidGraph{}, repairOutput, validationErr
	}

	repaired, parseErr := ParseBraidGraphText(repairOutput.AssistantText)
	if parseErr != nil {
		return BraidGraph{}, repairOutput, fmt.Errorf("rlm repl runner: braid_graph repair parse failed (initial error: %v): %w", initialErr, parseErr)
	}
	repaired = NormalizeBraidGraphForPolicy(repaired, phase.BraidGraphPolicy, maxNodes)
	if validateErr := validateBraidGraphForPhase(phase, repaired, maxNodes); validateErr != nil {
		return BraidGraph{}, repairOutput, fmt.Errorf("rlm repl runner: braid_graph repair validation failed (initial error: %v): %w", initialErr, validateErr)
	}
	return repaired, repairOutput, nil
}

func validateBraidGraphForPhase(phase REPLRunnerPhase, graph BraidGraph, maxNodes int) error {
	if err := ValidateBraidGraph(graph, maxNodes); err != nil {
		return err
	}
	if err := ValidateBraidGraphPolicy(graph, phase.BraidGraphPolicy); err != nil {
		return err
	}
	if phase.RequireScaffoldContract {
		if err := ValidateBraidGraphScaffoldContract(graph); err != nil {
			return err
		}
	}
	return nil
}

func runREPLLLMWithTransientRetry(ctx context.Context, llm *engine.LLMChatEngine, input engine.EngineInput) (engine.EngineOutput, error) {
	output, err := llm.Run(ctx, input)
	if !isRetryableREPLLLMError(err) {
		return output, err
	}
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return output, ctx.Err()
	case <-timer.C:
	}
	retryOutput, retryErr := llm.Run(ctx, input)
	return mergeEngineOutputs(output, retryOutput), retryErr
}

func normalizeREPLPhaseResponseFormat(provider string, rf json.RawMessage) json.RawMessage {
	if len(rf) == 0 {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(provider), "lmstudio") {
		var body struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rf, &body); err == nil && strings.EqualFold(body.Type, "json_object") {
			return json.RawMessage(`{"type":"text"}`)
		}
	}
	return rf
}

func isRetryableREPLLLMError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{
		"tls: bad record mac",
		"unexpected eof",
		"connection reset",
		"connection refused",
		"server disconnected",
		"http2: client connection lost",
		"stream error",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func buildBraidGraphRepairPrompt(originalPrompt string, phase REPLRunnerPhase, prior engine.EngineOutput, invalidOutput string, validationErr error, maxNodes int) string {
	var b strings.Builder
	b.WriteString(buildREPLPhasePrompt(originalPrompt, phase, prior, replRunnerRunState{}))
	b.WriteString("\n\nThe previous phase response was invalid for output_kind=braid_graph.\n")
	if validationErr != nil {
		b.WriteString("Error:\n")
		b.WriteString(strings.TrimSpace(validationErr.Error()))
		b.WriteString("\n")
	}
	if maxNodes > 0 {
		fmt.Fprintf(&b, "Node cap: %d\n", maxNodes)
	}
	b.WriteString("\nReturn JSON only. No markdown fences and no prose.\n")
	b.WriteString("Use kind extract|solve|cycle_solve|verify|reduce. Keep each question under 220 characters and expected_output under 120 characters.\n")
	if strings.TrimSpace(phase.BraidGraphPolicy) == BraidGraphPolicyLongCoTController {
		b.WriteString("LongCoT controller policy is mandatory: include extract, at least one solve-like node, verify, and reduce; keep the graph acyclic and shorten invalid fields instead of deleting required node kinds.\n")
		b.WriteString("Use one primary solve node by default. Add another solve-like node only for a real alternate candidate, concrete repair, or true dependency cluster. Do not split state-transition, planning, simulation, or BlocksWorld-style tasks into vague prose segments.\n")
		b.WriteString("cycle_solve is optional. Use it only for a true strongly connected/fixed-point constraint cluster.\n")
		b.WriteString("The verify node question or expected_output must explicitly say it checks original constraints by substituting candidate values into the original problem placeholders.\n")
	}
	b.WriteString("Schema:\n")
	b.WriteString(`{"version":1,"nodes":[{"id":"n1","kind":"extract|solve|cycle_solve|verify|reduce","question":"...","depends_on":["n0"],"expected_output":"...","max_summary_chars":256,"helper_policy":"auto|preferred|required|never","archetype":"symbolic_trace|candidate_verify|state_transition|explicit_dag|graph_search|numeric_dp|sequence_simulation|constraint_solver|mixed","scaffold_class":"symbolic_trace|candidate_verify|state_transition|explicit_dag|graph_search|numeric_dp|sequence_simulation|constraint_solver","scaffold_id":"type_inference_v1|property_check_v1|state_replay_v1|search_backtrack_v1|generic_v1","input_schema":{"key":"value"}}],"final_node":"n1"}`)
	b.WriteString("\n")
	// If the error is a scaffold contract violation, add explicit repair instructions.
	if mse, ok := IsMissingScaffoldContract(validationErr); ok {
		fmt.Fprintf(&b, "\nScaffold contract violation on node %q: missing %v.\n", mse.NodeID, mse.Missing)
		b.WriteString("Every solve and verify node MUST include archetype, scaffold_class, scaffold_id, and input_schema.\n")
		b.WriteString("If you cannot choose a specific scaffold, use:\n")
		b.WriteString(`  "archetype": "mixed", "scaffold_class": "explicit_dag", "scaffold_id": "generic_v1", "input_schema": {"prompt": "..."}` + "\n")
		b.WriteString("Do NOT omit these fields. Repair this graph JSON only — do not solve the task.\n")
	}
	b.WriteString("\nReturn JSON only. No markdown fences and no prose.\n")
	b.WriteString(safeTelemetryExcerpt(invalidOutput, 3000))
	return b.String()
}

func buildToolErrorRepairPrompt(taskPrompt string, prior engine.EngineOutput, errText string, attempt, attempts int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "A previous tool call failed or produced invalid tool arguments during attempt %d of %d.\n", attempt, attempts)
	b.WriteString("Do not call any tools now. Return a compact child answer or blocker from the information already available.\n")
	b.WriteString("Use this format:\n")
	b.WriteString("status: ok|partial|blocked\n")
	b.WriteString("answer: <best compact answer or empty>\n")
	b.WriteString("checks: <one compact check or blocker>\n\n")
	b.WriteString("Tool/runtime error:\n")
	b.WriteString(strings.TrimSpace(errText))
	b.WriteString("\n\nOriginal task:\n")
	b.WriteString(strings.TrimSpace(taskPrompt))
	if summary := summarizeREPLPhaseToolResults(prior); summary != "" {
		b.WriteString("\n\nPrior tool results:\n")
		b.WriteString(summary)
	}
	return b.String()
}

func recordParentLLMIterations(recorder *Recorder, model string, output engine.EngineOutput, offset int) {
	if recorder == nil {
		return
	}
	for idx, iteration := range output.Iterations {
		recorder.RecordParentLLMCall(ParentLLMCallEvent{
			CallID:           fmt.Sprintf("parent-%d", offset+idx+1),
			Model:            model,
			PromptTokens:     iteration.PromptTokens,
			CompletionTokens: iteration.CompletionTokens,
			FinishReason:     iteration.FinishReason,
			ToolCalls:        iteration.ToolCalls,
			ToolNames:        append([]string(nil), iteration.ToolNames...),
		})
	}
}

func filterPhaseTools(phase REPLRunnerPhase, tools []engine.ToolDef) []engine.ToolDef {
	if !phase.AutoExecuteGraphNodes {
		return tools
	}
	out := make([]engine.ToolDef, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == RLMQueryToolName {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func replBraidGraphValidationCap(phase REPLRunnerPhase, toolExec *replToolExecutor) int {
	if phase.MaxGraphNodes > 0 {
		return phase.MaxGraphNodes
	}
	if toolExec != nil && toolExec.budget != nil && toolExec.budget.cfg.MaxTotalNodes > 0 {
		return toolExec.budget.cfg.MaxTotalNodes
	}
	return 64
}

func replBraidGraphAutoFanoutCap(task rlm.Task, toolExec *replToolExecutor) int {
	maxSubcalls := task.MaxSubcalls
	if toolExec != nil && toolExec.budget != nil && toolExec.budget.cfg.MaxSubcalls > 0 {
		maxSubcalls = toolExec.budget.cfg.MaxSubcalls
	}
	return maxSubcalls
}

func autoExecutePhaseRequiredTool(ctx context.Context, phaseName string, phase REPLRunnerPhase, toolExec *replToolExecutor, output *engine.EngineOutput) error {
	if !phase.AutoExecuteRequiredTool || phase.Final || output == nil {
		return nil
	}
	calls := phase.AutoExecuteToolCalls
	if len(calls) == 0 {
		if len(phase.RequiredTools) != 1 {
			return nil
		}
		required := strings.TrimSpace(phase.RequiredTools[0])
		if required == "" {
			return nil
		}
		calls = []REPLRunnerPhaseAutoToolCall{{Tool: required}}
	}
	if autoPhaseAlreadySatisfied(output, calls) {
		return nil
	}
	for idx, call := range calls {
		name := strings.TrimSpace(call.Tool)
		if name == "" {
			return fmt.Errorf("rlm repl runner phase %q has empty auto tool call", phaseName)
		}
		if !extraToolNameAllowed(toolExec, name) && name != toolExec.replToolName && name != RLMQueryToolName && name != RLMWaitToolName && name != RLMResultToolName {
			return fmt.Errorf("rlm repl runner phase %q cannot auto-execute unavailable tool %q", phaseName, name)
		}
		callID := fmt.Sprintf("auto_%s_%02d_%s", sanitizeToolCallIDPart(phaseName), idx+1, sanitizeToolCallIDPart(name))
		args := call.Args
		if len(args) == 0 {
			args = autoExecutePhaseToolArgs(toolExec, name)
		}
		result, err := toolExec.Execute(ctx, name, args)
		toolCall := engine.ToolCall{ID: callID, Name: name, Arguments: args}
		toolResult := engine.ToolResult{ToolCallID: callID, Content: result}
		if err != nil {
			toolResult.IsError = true
			toolResult.Content = err.Error()
		}
		output.ToolCalls = append(output.ToolCalls, toolCall)
		output.ToolResults = append(output.ToolResults, toolResult)
		if err != nil {
			return err
		}
	}
	return nil
}

func autoExecutePhaseREPLCode(ctx context.Context, phaseName string, phase REPLRunnerPhase, toolExec *replToolExecutor, output *engine.EngineOutput) error {
	if output == nil {
		return nil
	}
	if toolExec == nil {
		return fmt.Errorf("rlm repl runner phase %q cannot execute repl_code without tool executor", phaseName)
	}
	toolName := replCodePhaseToolName(phase, toolExec)
	if toolName == "" {
		return fmt.Errorf("rlm repl runner phase %q has no REPL tool for repl_code output", phaseName)
	}
	code, err := parseREPLCodePhaseText(output.AssistantText)
	if err != nil {
		return fmt.Errorf("rlm repl runner phase %q invalid repl_code output: %w", phaseName, err)
	}
	if err := validateREPLCodePhaseContract(phase, code); err != nil {
		return fmt.Errorf("rlm repl runner phase %q invalid repl_code output: %w", phaseName, err)
	}
	args, err := json.Marshal(map[string]any{"code": code})
	if err != nil {
		return err
	}
	callID := fmt.Sprintf("auto_%s_%s", sanitizeToolCallIDPart(phaseName), sanitizeToolCallIDPart(toolName))
	result, execErr := toolExec.Execute(ctx, toolName, args)
	toolCall := engine.ToolCall{ID: callID, Name: toolName, Arguments: args}
	toolResult := engine.ToolResult{ToolCallID: callID, Content: result}
	if execErr != nil {
		toolResult.IsError = true
		toolResult.Content = execErr.Error()
	}
	output.ToolCalls = append(output.ToolCalls, toolCall)
	output.ToolResults = append(output.ToolResults, toolResult)
	if execErr != nil && (phase.RequireToolResultOK || phase.RequireToolOutput) {
		return execErr
	}
	if execErr == nil && phase.RequireToolOutput {
		if replOutputText(result) == "" {
			return fmt.Errorf("rlm repl runner phase %q %s produced empty output", phaseName, toolName)
		}
	}
	return nil
}

func autoCheckPhaseCycleWitness(phaseName string, output *engine.EngineOutput) error {
	if output == nil {
		return nil
	}
	result, err := CheckCycleWitnessText(output.AssistantText)
	callID := fmt.Sprintf("auto_%s_cycle_witness_check", sanitizeToolCallIDPart(phaseName))
	toolCall := engine.ToolCall{ID: callID, Name: "cycle_witness_check"}
	toolResult := engine.ToolResult{ToolCallID: callID}
	if err != nil {
		toolResult.IsError = true
		toolResult.Content = err.Error()
		output.ToolCalls = append(output.ToolCalls, toolCall)
		output.ToolResults = append(output.ToolResults, toolResult)
		return fmt.Errorf("rlm repl runner phase %q invalid cycle_witness output: %w", phaseName, err)
	}
	line, err := CycleWitnessResultJSONLine(result)
	if err != nil {
		toolResult.IsError = true
		toolResult.Content = err.Error()
		output.ToolCalls = append(output.ToolCalls, toolCall)
		output.ToolResults = append(output.ToolResults, toolResult)
		return err
	}
	toolResult.Content = line
	output.ToolCalls = append(output.ToolCalls, toolCall)
	output.ToolResults = append(output.ToolResults, toolResult)
	return nil
}

func validateREPLCodePhaseContract(phase REPLRunnerPhase, code string) error {
	if disallowed := disallowedREPLCodeImport(code); disallowed != "" {
		return fmt.Errorf("disallowed third-party import %q", disallowed)
	}
	if phase.MaxREPLCodeLines > 0 || phase.MaxREPLCodeCommentLines > 0 {
		lines, comments := countREPLCodeLines(code)
		if phase.MaxREPLCodeLines > 0 && lines > phase.MaxREPLCodeLines {
			return fmt.Errorf("too many code lines: got %d, max %d", lines, phase.MaxREPLCodeLines)
		}
		if phase.MaxREPLCodeCommentLines > 0 && comments > phase.MaxREPLCodeCommentLines {
			return fmt.Errorf("too many comment lines: got %d, max %d", comments, phase.MaxREPLCodeCommentLines)
		}
	}
	for _, required := range phase.RequiredREPLCodeSubstrings {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		if !strings.Contains(code, required) {
			return fmt.Errorf("missing required code substring %q", required)
		}
	}
	return nil
}

func countREPLCodeLines(code string) (int, int) {
	lines := 0
	comments := 0
	for _, rawLine := range strings.Split(code, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lines++
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			comments++
		}
	}
	return lines, comments
}

func disallowedREPLCodeImport(code string) string {
	disallowed := map[string]struct{}{
		"networkx": {},
		"numpy":    {},
		"pandas":   {},
		"scipy":    {},
		"sympy":    {},
	}
	for _, rawLine := range strings.Split(code, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "import ") {
			for _, field := range strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "import ")), ",") {
				name := strings.TrimSpace(field)
				if idx := strings.IndexAny(name, " \t."); idx >= 0 {
					name = name[:idx]
				}
				if _, denied := disallowed[name]; denied {
					return name
				}
			}
			continue
		}
		if strings.HasPrefix(line, "from ") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "from "))
			name := rest
			if idx := strings.IndexAny(name, " \t."); idx >= 0 {
				name = name[:idx]
			}
			if _, denied := disallowed[name]; denied {
				return name
			}
		}
	}
	return ""
}

func appendREPLCodeFailureContext(phaseName string, phase REPLRunnerPhase, output *engine.EngineOutput, err error) {
	if output == nil || err == nil {
		return
	}
	callID := fmt.Sprintf("auto_%s_repl_code_failure", sanitizeToolCallIDPart(phaseName))
	content := map[string]any{
		"ok":       false,
		"phase":    nonEmptyPhaseName(phase.Name),
		"kind":     REPLPhaseOutputKindREPLCode,
		"error":    err.Error(),
		"fallback": "scratch computation unavailable; use this as context and return a compact blocker or partial answer",
	}
	raw, marshalErr := json.Marshal(content)
	if marshalErr != nil {
		raw = []byte(fmt.Sprintf(`{"ok":false,"phase":%q,"kind":%q,"error":%q}`, nonEmptyPhaseName(phase.Name), REPLPhaseOutputKindREPLCode, err.Error()))
	}
	output.ToolCalls = append(output.ToolCalls, engine.ToolCall{
		ID:   callID,
		Name: "repl_code_failure",
	})
	output.ToolResults = append(output.ToolResults, engine.ToolResult{
		ToolCallID: callID,
		Content:    string(raw),
		IsError:    true,
	})
}

func shouldRepairStructuredFinalAfterAttemptError(phase REPLRunnerPhase, output engine.EngineOutput) bool {
	return strings.TrimSpace(phase.FinalOutputKind) != "" &&
		strings.TrimSpace(output.AssistantText) != "" &&
		output.StopReason == engine.StopReasonMaxTokens
}

func replCodeFilterMaxTokens(phase REPLRunnerPhase, phaseMaxTokens int) int {
	if phase.FilterREPLCodeMaxTokens > 0 {
		return phase.FilterREPLCodeMaxTokens
	}
	if phaseMaxTokens > 0 && phaseMaxTokens < 768 {
		return phaseMaxTokens
	}
	return 768
}

func phaseOutputFilterMaxTokens(phase REPLRunnerPhase, fallback int) int {
	if phase.FilterOutputMaxTokens > 0 {
		return phase.FilterOutputMaxTokens
	}
	if fallback > 0 && fallback < 768 {
		return fallback
	}
	return 768
}

func replCodePhaseToolName(phase REPLRunnerPhase, toolExec *replToolExecutor) string {
	for _, name := range phase.Tools {
		name = strings.TrimSpace(name)
		if name == PythonREPLToolName || name == GoREPLToolName {
			return name
		}
	}
	if toolExec != nil {
		return strings.TrimSpace(toolExec.replToolName)
	}
	return ""
}

func parseREPLCodePhaseText(text string) (string, error) {
	code := strings.TrimSpace(text)
	if code == "" {
		return "", fmt.Errorf("empty code")
	}
	if strings.HasPrefix(code, "```") {
		if unfenced := extractSingleFencedCodeBlock(code); unfenced != "" {
			code = unfenced
		} else {
			return "", fmt.Errorf("invalid markdown fence")
		}
	} else if strings.Contains(code, "```") {
		return "", fmt.Errorf("mixed prose and markdown fences are not allowed")
	}
	if looksLikePseudoToolCall(strings.ToLower(code)) {
		return "", fmt.Errorf("tool-call syntax is not allowed")
	}
	if !hasExecutableCodeLine(code) {
		return "", fmt.Errorf("code must contain at least one executable non-comment line")
	}
	return code, nil
}

func hasExecutableCodeLine(code string) bool {
	for _, line := range strings.Split(code, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "*/") {
			continue
		}
		return true
	}
	return false
}

func replOutputText(result string) string {
	var decoded struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return strings.TrimSpace(result)
	}
	return strings.TrimSpace(decoded.Output)
}

func extractSingleFencedCodeBlock(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return ""
	}
	rest := strings.TrimPrefix(text, "```")
	if idx := strings.IndexByte(rest, '\n'); idx >= 0 {
		rest = rest[idx+1:]
	} else {
		return ""
	}
	if idx := strings.LastIndex(rest, "```"); idx >= 0 {
		rest = rest[:idx]
	}
	return strings.TrimSpace(rest)
}

func autoPhaseAlreadySatisfied(output *engine.EngineOutput, calls []REPLRunnerPhaseAutoToolCall) bool {
	if output == nil || len(calls) == 0 {
		return true
	}
	names := toolCallNames(output.ToolCalls)
	counts := map[string]int{}
	for _, name := range names {
		counts[name]++
	}
	required := map[string]int{}
	for _, call := range calls {
		name := strings.TrimSpace(call.Tool)
		if name != "" {
			required[name]++
		}
	}
	for name, want := range required {
		if counts[name] < want {
			return false
		}
	}
	return true
}

func autoExecutePhaseToolArgs(toolExec *replToolExecutor, required string) json.RawMessage {
	if toolExec == nil {
		return json.RawMessage(`{}`)
	}
	if required == EphemeralHelperSolveToolName && toolExec.helperFactory != nil {
		return toolExec.helperFactory.AutoExecuteArgs()
	}
	if required == RLMQueryToolName {
		return json.RawMessage(`{"max_iterations":2}`)
	}
	return json.RawMessage(`{}`)
}

func sanitizeToolCallIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "tool"
	}
	var b strings.Builder
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			b.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			b.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func forcedToolChoiceForPhase(phase REPLRunnerPhase, tools []engine.ToolDef) json.RawMessage {
	// Provider-level forced tool choice is not portable across OpenAI-compatible
	// backends. Phase enforcement is deterministic in validateREPLPhaseOutput:
	// each phase exposes only its allowed tools and rejects missing required
	// tool calls after the model response.
	return nil
}

func (r *REPLRunner) newSandbox(cfg SandboxConfig) rlm.Sandbox {
	if r != nil && r.SandboxFactory != nil {
		return r.SandboxFactory()
	}
	switch cfg.Kind {
	case SandboxKindYaegi:
		return repl.NewYaegiSession(cfg.Yaegi)
	default:
		return repl.NewPythonSession(cfg.Python)
	}
}

func (r *REPLRunner) newRLMQueryFunc(task rlm.Task, env rlm.Environment) RLMQueryRunFunc {
	if r == nil || r.Config.RLMQueryFactory == nil {
		return nil
	}
	return r.Config.RLMQueryFactory(task, env)
}

func (r *REPLRunner) newAsyncRLMTools(ctx context.Context, toolExec *replToolExecutor, identity IdentityPlan) (*RLMToolsExecutor, *Scheduler, NodeStore, error) {
	if toolExec == nil || toolExec.rlmQuery == nil {
		return nil, nil, nil, fmt.Errorf("rlm repl runner: async recursion requires rlm_query factory")
	}

	store := NewMemoryNodeStore()
	if _, err := store.CreateRun(ctx, Run{
		ID:         identity.RunID,
		RootNodeID: replRootNodeID,
		Status:     NodeStatusQueued,
		Metadata: map[string]any{
			"agent_id":         identity.AgentID,
			"output_namespace": identity.OutputNamespace,
		},
	}); err != nil {
		return nil, nil, nil, fmt.Errorf("rlm repl runner: create async run: %w", err)
	}
	if _, err := store.CreateNode(ctx, Node{
		RunID:  identity.RunID,
		ID:     replRootNodeID,
		Depth:  0,
		Status: NodeStatusQueued,
		Prompt: toolExec.parentTask.Prompt,
		Metadata: map[string]any{
			"agent_id":         identity.AgentID,
			"output_namespace": identity.OutputNamespace,
		},
	}); err != nil {
		return nil, nil, nil, fmt.Errorf("rlm repl runner: create async root node: %w", err)
	}

	schedulerCfg := r.Config.AsyncScheduler
	schedulerCfg.Store = store
	schedulerCfg.Budget = toolExec.budget
	schedulerCfg.BudgetConfig = nil
	schedulerCfg.Backend = r.newAsyncNodeBackend(toolExec, identity)
	schedulerCfg.Recorder = toolExec.recorder
	schedulerCfg.RunID = identity.RunID
	schedulerCfg.RootNodeID = replRootNodeID
	if strings.TrimSpace(schedulerCfg.OutputRoot) == "" {
		schedulerCfg.OutputRoot = identity.OutputRoot
	}
	if schedulerCfg.RootContext == nil {
		schedulerCfg.RootContext = ctx
	}

	scheduler, err := NewScheduler(schedulerCfg)
	if err != nil {
		return nil, nil, nil, err
	}

	asyncTools, err := NewRLMToolsExecutor(RLMToolsConfig{
		Scheduler:            scheduler,
		Store:                store,
		RunID:                identity.RunID,
		ParentNodeID:         replRootNodeID,
		DefaultQueryPrompt:   r.Config.DefaultRLMQueryPrompt,
		SummaryMaxChars:      r.Config.ChildSummaryMaxChars,
		RequiredSubcallRules: r.Config.RequiredSubcallRules,
	})
	if err != nil {
		_ = scheduler.Close()
		return nil, nil, nil, err
	}
	return asyncTools, scheduler, store, nil
}

func (r *REPLRunner) newAsyncNodeBackend(toolExec *replToolExecutor, identity IdentityPlan) NodeBackend {
	return NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
		childTask := buildAsyncChildTask(toolExec.parentTask, identity, node, input)

		var childResult rlm.Result
		var validationErr error
		usedAttempts := 0
		attempts := 1
		if input.RequiredSubcalls > 0 {
			attempts = requiredSubcallMaxAttempts
		}
		for attempt := 1; attempt <= attempts; attempt++ {
			usedAttempts = attempt
			if attempt > 1 {
				childTask.Prompt = requiredSubcallRetryPrompt(input.Prompt, input.RequiredSubcalls, validationErr)
			}
			var err error
			childResult, err = toolExec.rlmQuery(ctx, childTask, toolExec.parentEnv)
			if err != nil {
				return NodeResult{}, err
			}
			validationErr = validateRequiredSubcalls(childResult, input.RequiredSubcalls)
			if validationErr == nil {
				break
			}
		}
		if validationErr != nil {
			return NodeResult{}, validationErr
		}

		metadata := cloneMapAny(childResult.Metadata)
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["required_subcalls"] = input.RequiredSubcalls
		metadata["required_subcall_attempts"] = usedAttempts
		metadata["iterations"] = childResult.Iterations
		metadata["subcalls"] = childResult.Subcalls
		inputTokens, outputTokens, totalTokens := extractChildTokenTotals(childResult.Metadata)
		metadata["child_input_tokens"] = inputTokens
		metadata["child_output_tokens"] = outputTokens
		metadata["child_total_tokens"] = totalTokens
		metadata["run_id"] = childTask.RunID
		metadata["agent_id"] = childTask.AgentID
		metadata["parent_agent_id"] = childTask.ParentAgentID
		metadata["output_namespace"] = childTask.OutputNamespace
		if trace, ok := childResult.Metadata["recursive_trace"].(*RecursiveTrace); ok && trace != nil {
			metadata["recursive_trace"] = trace
		}

		rawSummary := strings.TrimSpace(childResult.Answer)
		summaryLimit := firstPositiveInt(input.SummaryMaxChars, r.Config.ChildSummaryMaxChars)
		summary, summaryTruncated, summaryMeta := r.compactChildSummary(ctx, toolExec, childTask, rawSummary, summaryLimit)
		if summary == "" {
			summary = "child completed"
		}
		metadata["summary_chars"] = runeLen(summary)
		metadata["summary_truncated"] = summaryTruncated
		for key, value := range summaryMeta {
			metadata[key] = value
		}
		if rewriteInput := intFromAny(summaryMeta["summary_rewrite_input_tokens"]); rewriteInput > 0 {
			metadata["child_input_tokens"] = inputTokens + rewriteInput
		}
		if rewriteOutput := intFromAny(summaryMeta["summary_rewrite_output_tokens"]); rewriteOutput > 0 {
			metadata["child_output_tokens"] = outputTokens + rewriteOutput
		}
		if rewriteTotal := intFromAny(summaryMeta["summary_rewrite_total_tokens"]); rewriteTotal > 0 {
			metadata["child_total_tokens"] = totalTokens + rewriteTotal
		}
		if summaryTruncated {
			metadata["raw_summary_chars"] = runeLen(rawSummary)
		}

		return NodeResult{
			Status:   NodeStatusCompleted,
			Summary:  summary,
			Answer:   childResult.Answer,
			Metadata: metadata,
		}, nil
	})
}

func (r *REPLRunner) compactChildSummary(ctx context.Context, toolExec *replToolExecutor, childTask rlm.Task, rawSummary string, maxChars int) (string, bool, map[string]any) {
	summary, truncated := compactRLMSummaryText(rawSummary, maxChars)
	meta := map[string]any{
		"summary_compaction_method": "deterministic",
	}
	if maxChars > 0 {
		meta["summary_max_chars"] = maxChars
	}
	shouldRewrite := false
	rewriteReason := ""
	if r.Config.ChildSummaryNormalizeBeforeSubmit {
		shouldRewrite = true
		rewriteReason = "presubmit"
	} else if truncated && r.Config.ChildSummaryRewriteOverLimit {
		shouldRewrite = true
		rewriteReason = "over_limit"
	}
	if !shouldRewrite || toolExec == nil || toolExec.rlmQuery == nil || strings.TrimSpace(rawSummary) == "" {
		return summary, truncated, meta
	}

	meta["summary_rewrite_attempted"] = true
	meta["summary_rewrite_reason"] = rewriteReason
	meta["raw_summary_chars"] = runeLen(rawSummary)
	summaryTask := childTask
	summaryTask.Prompt = buildChildSummaryRewritePrompt(childTask.Prompt, rawSummary, maxChars)
	summaryTask.RunID = strings.TrimSpace(childTask.RunID) + "-summary"
	summaryTask.ParentAgentID = strings.TrimSpace(childTask.AgentID)
	summaryTask.AgentID = strings.Trim(strings.TrimSpace(childTask.AgentID)+"/summary", "/")
	summaryTask.MaxDepth = 0
	summaryTask.MaxSubcalls = 0
	summaryTask.MaxIterations = firstPositiveInt(r.Config.ChildSummaryRewriteMaxIterations, 2)

	rewriteResult, err := toolExec.rlmQuery(ctx, summaryTask, toolExec.parentEnv)
	if err != nil {
		meta["summary_rewrite_error"] = err.Error()
		return summary, truncated, meta
	}
	rewriteInput, rewriteOutput, rewriteTotal := extractChildTokenTotals(rewriteResult.Metadata)
	if rewriteInput > 0 {
		meta["summary_rewrite_input_tokens"] = rewriteInput
	}
	if rewriteOutput > 0 {
		meta["summary_rewrite_output_tokens"] = rewriteOutput
	}
	if rewriteTotal > 0 {
		meta["summary_rewrite_total_tokens"] = rewriteTotal
	}

	rewritten, rewriteTruncated := compactRLMSummaryText(rewriteResult.Answer, maxChars)
	if strings.TrimSpace(rewritten) == "" {
		meta["summary_rewrite_error"] = "empty rewrite"
		return summary, truncated, meta
	}
	if validateChildSummaryFinalText(rawSummary) == nil {
		if err := validateChildSummaryFinalText(rewritten); err != nil {
			meta["summary_rewrite_error"] = "invalid child summary rewrite: " + err.Error()
			return summary, truncated, meta
		}
	}
	meta["summary_compaction_method"] = "rewrite"
	meta["summary_rewrite_used"] = true
	meta["summary_rewrite_raw_chars"] = runeLen(strings.TrimSpace(rewriteResult.Answer))
	return rewritten, rewriteTruncated, meta
}

func buildChildSummaryRewritePrompt(childPrompt, rawSummary string, maxChars int) string {
	var b strings.Builder
	b.WriteString("Normalize this child RLM answer before it is submitted to the parent.\n")
	if maxChars > 0 {
		fmt.Fprintf(&b, "Return the densest possible parent-facing summary under %d characters.\n", maxChars)
	} else {
		b.WriteString("Return the densest possible parent-facing summary.\n")
	}
	b.WriteString("Align the summary to the child task, not to incidental scratch work.\n")
	b.WriteString("Use compact lines only. Prefer these fields when useful: status, answer, values, checks, blockers.\n")
	b.WriteString("Preserve exact final answer strings such as `solution = ...`. Drop scratch work, derivations, tool logs, and narrative prose.\n\n")
	if childPrompt = strings.TrimSpace(childPrompt); childPrompt != "" {
		childPrompt, _ = compactRLMSummaryText(childPrompt, 2000)
		b.WriteString("Child task:\n")
		b.WriteString(childPrompt)
		b.WriteString("\n\n")
	}
	b.WriteString("Child answer:\n")
	b.WriteString(strings.TrimSpace(rawSummary))
	return b.String()
}

func buildAsyncChildTask(parentTask rlm.Task, identity IdentityPlan, node Node, input NodeInput) rlm.Task {
	childLeaf := sanitizeSegment(strings.ReplaceAll(node.ID, ".", "-"), "rlm-node")
	childAgentID := sanitizeAgentPath(strings.TrimSpace(identity.AgentID+"/"+childLeaf), defaultAgentID)
	maxDepth := maxInt(parentTask.MaxDepth-node.Depth, 0)
	maxSubcalls := maxInt(parentTask.MaxSubcalls-node.Depth, 0)
	return rlm.Task{
		Prompt:          buildChildRuntimePrompt(parentTask.Prompt, input.Prompt, maxDepth, maxSubcalls, input.RequiredSubcalls),
		Role:            parentTask.Role,
		RunID:           identity.RunID,
		AgentID:         childAgentID,
		ParentAgentID:   identity.AgentID,
		OutputRoot:      identity.OutputRoot,
		OutputNamespace: buildOutputNamespace(identity.RunID, childAgentID),
		WorkspaceID:     parentTask.WorkspaceID,
		WorkspaceRoot:   parentTask.WorkspaceRoot,
		MaxDepth:        maxDepth,
		MaxIterations:   firstPositive(input.MaxIterations, parentTask.MaxIterations),
		MaxSubcalls:     maxSubcalls,
	}
}

func buildChildRuntimePrompt(parentPrompt, childPrompt string, remainingDepth, remainingSubcalls, requiredSubcalls int) string {
	parentPrompt = strings.TrimSpace(parentPrompt)
	childPrompt = strings.TrimSpace(childPrompt)
	var b strings.Builder
	b.WriteString("RLM child runtime context:\n")
	b.WriteString("- You are a child solve created by rlm_query. Solve the child task below and return the child answer or compact child summary.\n")
	b.WriteString("- The original parent task is included for grounding. If the child task introduces unrelated equations, variables, objects, or goals not present in the parent task, ignore that drift and solve the parent-grounded task instead.\n")
	b.WriteString("- python_repl/go_repl are scratch REPL tools only.\n")
	b.WriteString("- rlm_query, rlm_wait, and rlm_result are separate model tools when they are listed as available tools. They are not Python or Go functions. Never call rlm_query, rlm_wait, or rlm_result inside REPL code.\n")
	if requiredSubcalls > 0 {
		fmt.Fprintf(&b, "- Remaining recursive depth: %d. Remaining recursive subcall budget: %d.\n", remainingDepth, remainingSubcalls)
		fmt.Fprintf(&b, "- Runtime requirement: this child must make at least %d recursive rlm_query call(s), then call rlm_wait({}), before finalizing.\n", requiredSubcalls)
	} else if remainingDepth <= 0 || remainingSubcalls <= 0 {
		b.WriteString("- Leaf-child mode: solve directly with the child task and scratch REPL if useful. Do not discuss recursion, depth, budget, subagents, or runtime availability in the final child answer.\n")
	} else {
		fmt.Fprintf(&b, "- Remaining recursive depth: %d. Remaining recursive subcall budget: %d.\n", remainingDepth, remainingSubcalls)
		b.WriteString("- Recursive child calls are optional. If decomposition is not needed, solve directly; do not claim the runtime is unavailable.\n")
	}
	b.WriteString("- Preserve the requested answer format from the child task.\n")
	b.WriteString("- Final child responses must be compact: for math or puzzle tasks, prefer one line such as `solution = ...`; otherwise use at most 120 words.\n")
	b.WriteString("- Do not return a dependency graph, full proof, scratch transcript, or tool log unless the child task explicitly asks for it.\n")
	b.WriteString("- If scratch REPL calls fail twice, stop using the REPL and return the best compact answer or one short blocker line.\n\n")
	if parentPrompt != "" {
		b.WriteString("Original parent task for grounding:\n")
		b.WriteString(parentPrompt)
		b.WriteString("\n\n")
	}
	b.WriteString("Child task:\n")
	b.WriteString(childPrompt)
	return b.String()
}

func validateRequiredSubcalls(result rlm.Result, required int) error {
	if required <= 0 {
		return nil
	}
	used := result.Subcalls
	if fromMetadata := intFromAny(result.Metadata["recursive_subcalls_used"]); fromMetadata > used {
		used = fromMetadata
	}
	if used >= required {
		return nil
	}
	return RequiredSubcallsError{Required: required, Used: used}
}

func requiredSubcallRetryPrompt(originalPrompt string, required int, previous error) string {
	var b strings.Builder
	b.WriteString("Runtime correction: your previous child answer was rejected because it flattened the task.\n")
	if previous != nil {
		b.WriteString("Failure: ")
		b.WriteString(previous.Error())
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "This child must make at least %d recursive rlm_query call(s), then call rlm_wait({}), before returning its final child answer.\n", required)
	b.WriteString("Do not answer directly. Do not invent child IDs. Use rlm_query for the required grandchild work, then rlm_wait({}), then synthesize.\n\n")
	b.WriteString("Original child task:\n")
	b.WriteString(strings.TrimSpace(originalPrompt))
	return b.String()
}

func pendingSubcallRetryPrompt(originalPrompt string, previous error) string {
	var b strings.Builder
	b.WriteString("Runtime correction: your previous parent answer was rejected because submitted child solves were still running.\n")
	if previous != nil {
		b.WriteString("Failure: ")
		b.WriteString(previous.Error())
		b.WriteString("\n")
	}
	b.WriteString("Existing child submissions are still tracked by this tool session. Do not answer yet.\n")
	b.WriteString("Call rlm_wait as the separate tool named rlm_wait with empty JSON arguments: {}. Do not call rlm_wait from inside python_repl or go_repl. Do not invent or pass child IDs.\n")
	b.WriteString("After rlm_wait reports terminal child summaries, synthesize the final answer and follow the original task's requested answer format exactly.\n\n")
	b.WriteString("Original task:\n")
	b.WriteString(strings.TrimSpace(originalPrompt))
	b.WriteString("\n")
	return b.String()
}

func mergeEngineOutputs(base, next engine.EngineOutput) engine.EngineOutput {
	if base.StopReason == "" {
		base.StopReason = next.StopReason
	}
	if strings.TrimSpace(next.AssistantText) != "" {
		base.AssistantText = next.AssistantText
	}
	if next.StopReason != "" {
		base.StopReason = next.StopReason
	}
	if strings.TrimSpace(next.Error) != "" {
		base.Error = next.Error
	}
	base.ToolCalls = append(base.ToolCalls, next.ToolCalls...)
	base.ToolResults = append(base.ToolResults, next.ToolResults...)
	base.Iterations = append(base.Iterations, next.Iterations...)
	base.InjectedContexts = append(base.InjectedContexts, next.InjectedContexts...)
	base.Tokens.Add(next.Tokens.InputTokens, next.Tokens.OutputTokens)
	return base
}

func validateREPLAttemptOutput(output engine.EngineOutput, err error, maxTokens int) error {
	if err != nil {
		return err
	}
	if output.StopReason == engine.StopReasonError {
		return fmt.Errorf("rlm repl runner: %s", strings.TrimSpace(output.Error))
	}
	if output.StopReason == engine.StopReasonMaxTokens {
		return fmt.Errorf("rlm repl runner: model hit max token stop before producing a valid final answer")
	}
	if maxTokens > 0 {
		for _, iteration := range output.Iterations {
			if iteration.CompletionTokens > maxTokens {
				if replOverrunHasUsableText(output, maxTokens) {
					continue
				}
				return fmt.Errorf("rlm repl runner: model completion exceeded configured max tokens: completion_tokens=%d max_tokens=%d", iteration.CompletionTokens, maxTokens)
			}
		}
	}
	for _, toolResult := range output.ToolResults {
		if toolResult.IsError {
			return fmt.Errorf("rlm repl runner: %s", strings.TrimSpace(toolResult.Content))
		}
	}
	return nil
}

func validateREPLAttemptOutputForPhase(phase REPLRunnerPhase, output engine.EngineOutput, err error, maxTokens int) error {
	if strings.TrimSpace(phase.OutputKind) == REPLPhaseOutputKindBraidGraph {
		if err != nil {
			return err
		}
		if output.StopReason == engine.StopReasonError {
			return fmt.Errorf("rlm repl runner: %s", strings.TrimSpace(output.Error))
		}
		if output.StopReason == engine.StopReasonMaxTokens && strings.TrimSpace(output.AssistantText) != "" {
			return nil
		}
		return validateREPLAttemptOutput(output, err, maxTokens)
	}
	if strings.TrimSpace(phase.OutputKind) != REPLPhaseOutputKindREPLCode {
		return validateREPLAttemptOutput(output, err, maxTokens)
	}
	if err != nil {
		return err
	}
	if output.StopReason == engine.StopReasonError {
		return fmt.Errorf("rlm repl runner: %s", strings.TrimSpace(output.Error))
	}
	if strings.TrimSpace(output.AssistantText) == "" {
		return fmt.Errorf("rlm repl runner: repl_code phase returned empty code")
	}
	return nil
}

func replOverrunHasUsableText(output engine.EngineOutput, maxTokens int) bool {
	answer := strings.TrimSpace(output.AssistantText)
	if answer == "" {
		return false
	}
	limit := maxTokens * 4
	if limit < 2048 {
		limit = 2048
	}
	if limit > 8192 {
		limit = 8192
	}
	return len(answer) <= limit
}

func replBudgetedIterationCount(iterations []engine.IterationUsage) int {
	count := 0
	for _, iteration := range iterations {
		if isREPLFinalizeFinishReason(iteration.FinishReason) {
			continue
		}
		count++
	}
	return count
}

func isREPLFinalizeFinishReason(finishReason string) bool {
	switch strings.TrimSpace(finishReason) {
	case "max_iterations_finalize", "empty_response_finalize":
		return true
	default:
		return false
	}
}

func parentLLMRuntimeError(code string, err error, output engine.EngineOutput) RuntimeErrorEvent {
	event := RuntimeErrorEvent{Code: code, Message: err.Error()}
	if strings.TrimSpace(output.AssistantText) != "" {
		event.RawChars = len(output.AssistantText)
		event.RawExcerpt = safeTelemetryExcerpt(output.AssistantText, 600)
		sanitized, info := rlm.SanitizeOutputText(output.AssistantText)
		event.SanitizedChars = len(sanitized)
		event.SanitizedExcerpt = safeTelemetryExcerpt(sanitized, 600)
		event.Artifacts = append([]string(nil), info.Artifacts...)
	}
	return event
}

func safeTelemetryExcerpt(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"))
	if text == "" || limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "...[truncated]"
}

func validateREPLPhaseOutput(phase REPLRunnerPhase, output engine.EngineOutput) error {
	switch strings.TrimSpace(phase.OutputKind) {
	case REPLPhaseOutputKindCyclePacket:
		if err := validateCyclePacketText(output.AssistantText); err != nil {
			return fmt.Errorf("rlm repl runner phase %q invalid cycle_packet output: %w", strings.TrimSpace(phase.Name), err)
		}
	case REPLPhaseOutputKindCycleWitness:
		if _, err := CheckCycleWitnessText(output.AssistantText); err != nil {
			return fmt.Errorf("rlm repl runner phase %q invalid cycle_witness output: %w", strings.TrimSpace(phase.Name), err)
		}
	}
	for _, required := range phase.RequiredTools {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		if countToolName(toolCallNames(output.ToolCalls), required) == 0 {
			name := strings.TrimSpace(phase.Name)
			if name == "" {
				name = "unnamed"
			}
			return fmt.Errorf("rlm repl runner phase %q required tool %q was not called", name, required)
		}
	}
	if phase.RequireToolResultOK {
		if err := validateRequiredPhaseToolResultsOK(phase, output); err != nil {
			return err
		}
	}
	if phase.Final {
		if err := validateFinalOutputKind(phase, output.AssistantText); err != nil {
			return err
		}
	}
	return nil
}

func validateCyclePacketText(text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fmt.Errorf("empty cycle packet")
	}
	if len(trimmed) > 2400 {
		return fmt.Errorf("cycle packet length %d exceeds max 2400", len(trimmed))
	}
	if strings.HasPrefix(trimmed, "```") || strings.Contains(trimmed, "```") {
		return fmt.Errorf("cycle packet must be raw JSON, not markdown")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var packet map[string]any
	if err := decoder.Decode(&packet); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("parse JSON: %w", err)
	}
	if len(packet) == 0 {
		return fmt.Errorf("cycle packet object is empty")
	}
	if _, ok := packet["unknowns"]; !ok {
		return fmt.Errorf("cycle packet missing unknowns")
	}
	if _, ok := packet["constraints"]; !ok {
		return fmt.Errorf("cycle packet missing constraints")
	}
	if _, ok := packet["candidate_bounds"]; !ok {
		return fmt.Errorf("cycle packet missing candidate_bounds")
	}
	return nil
}

func validateFinalOutputKind(phase REPLRunnerPhase, text string) error {
	switch strings.TrimSpace(phase.FinalOutputKind) {
	case "":
		return nil
	case "child_summary":
		return validateChildSummaryFinalText(text)
	default:
		return fmt.Errorf("rlm repl runner phase %q has unsupported final_output_kind %q", strings.TrimSpace(phase.Name), phase.FinalOutputKind)
	}
}

func validateChildSummaryFinalText(text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fmt.Errorf("child summary final response is empty")
	}
	lower := strings.ToLower(trimmed)
	if looksLikePseudoToolCall(lower) {
		return fmt.Errorf("child summary final response looks like a tool call or code instead of structured lines")
	}
	if forbidden := firstForbiddenChildSummaryRuntimeToken(lower); forbidden != "" {
		return fmt.Errorf("child summary final response mentions forbidden runtime protocol detail %q", forbidden)
	}
	lines := strings.Split(trimmed, "\n")
	fields := map[string]string{}
	for _, line := range lines {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "status", "answer", "checks":
			fields[key] = value
		}
	}
	status := strings.ToLower(strings.TrimSpace(fields["status"]))
	switch status {
	case "solved", "partial", "blocked":
	default:
		return fmt.Errorf("child summary final response must include status: solved|partial|blocked")
	}
	if status == "blocked" {
		if forbidden := firstForbiddenChildSummaryBlockerToken(lower); forbidden != "" {
			return fmt.Errorf("child summary final response treats dependency-cycle constraint as blocked via %q", forbidden)
		}
	}
	if _, ok := fields["answer"]; !ok {
		return fmt.Errorf("child summary final response must include answer line")
	}
	if strings.TrimSpace(fields["checks"]) == "" {
		return fmt.Errorf("child summary final response must include non-empty checks line")
	}
	if len(trimmed) > 1200 {
		return fmt.Errorf("child summary final response too long: chars=%d max=1200", len(trimmed))
	}
	return nil
}

func looksLikePseudoToolCall(lower string) bool {
	for _, marker := range []string{
		"python_repl(",
		"go_repl(",
		"rlm_query(",
		"rlm_wait(",
		"rlm_result(",
		"<|tool_call",
		"```",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func firstForbiddenChildSummaryRuntimeToken(lower string) string {
	for _, token := range []string{
		"rlm_query",
		"rlm_wait",
		"rlm_result",
		"subagent",
		"sub-agent",
		"remaining depth",
		"depth 0",
		"depth=0",
		"recursive depth",
		"recursion depth",
		"runtime depth",
		"runtime constraint",
		"runtime constraints",
		"subcall budget",
		"recursion budget",
		"no recursion budget",
		"tool availability",
		"unavailable tools",
	} {
		if strings.Contains(lower, token) {
			return token
		}
	}
	return ""
}

func firstForbiddenChildSummaryBlockerToken(lower string) string {
	for _, token := range []string{
		"circular dependency",
		"circular dependencies",
		"dependency cycle",
		"cyclic dependency",
		"cycle prevents",
		"prevents unique solution",
		"requires external resolution",
		"external resolution",
		"not supported",
		"unsupported",
		"single-pass logic",
		"single pass",
	} {
		if strings.Contains(lower, token) {
			return token
		}
	}
	return ""
}

func validateStructuredFinalRepairAttempt(output engine.EngineOutput, err error) error {
	if err != nil {
		return err
	}
	if output.StopReason == engine.StopReasonError {
		return fmt.Errorf("rlm repl runner: %s", strings.TrimSpace(output.Error))
	}
	if strings.TrimSpace(output.AssistantText) == "" {
		return fmt.Errorf("structured final repair returned empty response")
	}
	return nil
}

func structuredFinalRepairMaxTokens(maxTokens int) int {
	if maxTokens <= 0 || maxTokens > 512 {
		return 512
	}
	return maxTokens
}

func validateRequiredPhaseToolResultsOK(phase REPLRunnerPhase, output engine.EngineOutput) error {
	callNames := make(map[string]string, len(output.ToolCalls))
	for _, call := range output.ToolCalls {
		callNames[call.ID] = strings.TrimSpace(call.Name)
	}
	for _, required := range phase.RequiredTools {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		var sawResult bool
		for _, result := range output.ToolResults {
			if callNames[result.ToolCallID] != required {
				continue
			}
			sawResult = true
			if result.IsError {
				return fmt.Errorf("rlm repl runner phase %q required tool %q failed: %s", nonEmptyPhaseName(phase.Name), required, strings.TrimSpace(result.Content))
			}
			ok, hasOK := jsonObjectBoolField(result.Content, "ok")
			if !hasOK {
				return fmt.Errorf("rlm repl runner phase %q required tool %q result missing ok field", nonEmptyPhaseName(phase.Name), required)
			}
			if !ok {
				return fmt.Errorf("rlm repl runner phase %q required tool %q returned ok=false: %s", nonEmptyPhaseName(phase.Name), required, strings.TrimSpace(result.Content))
			}
		}
		if !sawResult {
			return fmt.Errorf("rlm repl runner phase %q required tool %q did not return a result", nonEmptyPhaseName(phase.Name), required)
		}
	}
	return nil
}

func jsonObjectBoolField(content, key string) (bool, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return false, false
	}
	value, ok := payload[key]
	if !ok {
		return false, false
	}
	boolValue, ok := value.(bool)
	return boolValue, ok
}

func nonEmptyPhaseName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unnamed"
	}
	return name
}

func filterREPLToolDefs(tools []engine.ToolDef, allowed []string) []engine.ToolDef {
	allowedSet := map[string]struct{}{}
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		allowedSet[name] = struct{}{}
	}
	if len(allowedSet) == 0 {
		return nil
	}
	out := make([]engine.ToolDef, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allowedSet[tool.Name]; ok {
			out = append(out, tool)
		}
	}
	return out
}

func buildREPLPhasePrompt(originalPrompt string, phase REPLRunnerPhase, prior engine.EngineOutput, state replRunnerRunState) string {
	var b strings.Builder
	if name := strings.TrimSpace(phase.Name); name != "" {
		fmt.Fprintf(&b, "RLM runtime phase: %s\n", name)
	}
	if kind := strings.TrimSpace(phase.OutputKind); kind != "" {
		fmt.Fprintf(&b, "Phase output kind: %s\n", kind)
	}
	if prompt := strings.TrimSpace(phase.Prompt); prompt != "" {
		b.WriteString(prompt)
		b.WriteString("\n\n")
	}
	if phase.Final {
		if handoff := latestBraidFinalHandoff(prior); handoff != "" {
			if summary := summarizeBraidGraphForPrompt(state.braidGraph); summary != "" {
				b.WriteString("Current braid graph:\n")
				b.WriteString(summary)
				b.WriteString("\n\n")
			}
			b.WriteString("Verified braid final handoff:\n")
			b.WriteString(handoff)
			b.WriteString("\n\n")
			b.WriteString("Final synthesis contract:\n")
			b.WriteString("- Use only the verified handoff above.\n")
			b.WriteString("- Return the final answer only, in the requested format.\n")
			b.WriteString("- Do not call tools, write code, restate the prompt, include scratch work, or mention runtime internals.\n")
			return strings.TrimSpace(b.String())
		}
	}
	if summary := summarizeBraidGraphForPrompt(state.braidGraph); summary != "" {
		b.WriteString("Current braid graph:\n")
		b.WriteString(summary)
		b.WriteString("\n\n")
	}
	if summary := summarizeREPLPhaseToolResults(prior); summary != "" {
		b.WriteString("Prior phase tool outputs:\n")
		b.WriteString(summary)
		b.WriteString("\n\n")
	}
	if phase.IncludePriorAssistantText {
		if text := strings.TrimSpace(prior.AssistantText); text != "" {
			b.WriteString("Prior phase assistant output:\n")
			b.WriteString(safeTelemetryExcerpt(text, 2400))
			b.WriteString("\n\n")
		}
	}
	b.WriteString("Original task:\n")
	b.WriteString(strings.TrimSpace(originalPrompt))
	return b.String()
}

func appendBraidFinalHandoff(output *engine.EngineOutput, graph *BraidGraph, summaries map[string]string) {
	if output == nil || graph == nil || strings.TrimSpace(graph.FinalNode) == "" {
		return
	}
	finalSummary := strings.TrimSpace(summaries[graph.FinalNode])
	if finalSummary == "" {
		return
	}
	content := renderBraidFinalHandoff(*graph, finalSummary)
	if strings.TrimSpace(content) == "" {
		return
	}
	output.InjectedContexts = append(output.InjectedContexts, engine.InjectedContext{
		ToolCallID: "braid-final-" + strings.TrimSpace(graph.FinalNode),
		Source:     braidFinalHandoffSource,
		Content:    content,
	})
}

func latestBraidFinalHandoff(output engine.EngineOutput) string {
	for i := len(output.InjectedContexts) - 1; i >= 0; i-- {
		ctx := output.InjectedContexts[i]
		if strings.TrimSpace(ctx.Source) != braidFinalHandoffSource {
			continue
		}
		if content := strings.TrimSpace(ctx.Content); content != "" {
			return content
		}
	}
	return ""
}

func verifiedAnswerFromLatestBraidFinalHandoff(output engine.EngineOutput) (string, bool) {
	return verifiedAnswerFromBraidFinalHandoff(latestBraidFinalHandoff(output))
}

func verifiedAnswerFromBraidFinalHandoff(handoff string) (string, bool) {
	handoff = strings.TrimSpace(handoff)
	if handoff == "" {
		return "", false
	}
	var payload struct {
		VerifiedAnswer string `json:"verified_answer"`
	}
	if err := json.Unmarshal([]byte(handoff), &payload); err == nil {
		answer := strings.TrimSpace(payload.VerifiedAnswer)
		if strings.HasPrefix(answer, "solution =") {
			return answer, true
		}
	}
	const marker = `"verified_answer":`
	idx := strings.Index(handoff, marker)
	if idx < 0 {
		return "", false
	}
	raw := strings.TrimSpace(handoff[idx+len(marker):])
	if strings.HasPrefix(raw, `"`) {
		var answer string
		if err := json.Unmarshal([]byte(raw), &answer); err == nil {
			answer = strings.TrimSpace(answer)
			if strings.HasPrefix(answer, "solution =") {
				return answer, true
			}
		}
	}
	return "", false
}

func renderBraidFinalHandoff(graph BraidGraph, finalSummary string) string {
	finalSummary = compactBraidFinalHandoffText(finalSummary, 1200)
	answer, hasAnswer := braidSolutionAnswerFromSummary(finalSummary)
	payload := map[string]any{
		"final_node":    strings.TrimSpace(graph.FinalNode),
		"final_summary": finalSummary,
	}
	if hasAnswer {
		payload["verified_answer"] = answer
		payload["required_output"] = "return exactly this answer line"
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		if hasAnswer {
			return "verified_answer: " + answer + "\nfinal_summary: " + finalSummary
		}
		return "final_summary: " + finalSummary
	}
	return string(body)
}

func compactBraidFinalHandoffText(value string, limit int) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 || len([]rune(compact)) <= limit {
		return compact
	}
	runes := []rune(compact)
	if limit <= 16 {
		return string(runes[:limit])
	}
	return string(runes[:limit-15]) + " ...[truncated]"
}

func buildFinalSolutionLineRepairPrompt(originalPrompt string, phase REPLRunnerPhase, prior engine.EngineOutput, invalid string, attempt, maxAttempts int) string {
	var b strings.Builder
	b.WriteString(buildREPLPhasePrompt(originalPrompt, phase, prior, replRunnerRunState{}))
	b.WriteString("\n\nFinal-answer repair required.\n")
	if maxAttempts > 1 {
		fmt.Fprintf(&b, "Repair attempt: %d of %d.\n", attempt, maxAttempts)
	}
	b.WriteString("The previous final response was invalid because it did not contain a line beginning with `solution =`.\n")
	b.WriteString("Previous final response:\n")
	b.WriteString(safeTelemetryExcerpt(invalid, 1200))
	b.WriteString("\n\nReturn exactly one line beginning with `solution =`. Do not output a tool name such as go_repl, python_repl, rlm_query, rlm_wait, or rlm_result. Do not include prose, caveats, scratch work, dependency graphs, or markdown.")
	return b.String()
}

func buildStructuredFinalRepairPrompt(originalPrompt string, phase REPLRunnerPhase, prior engine.EngineOutput, invalid string, validationErr error, attempt, maxAttempts int) string {
	var b strings.Builder
	b.WriteString(buildREPLPhasePrompt(originalPrompt, phase, prior, replRunnerRunState{}))
	b.WriteString("\n\nStructured final repair required.\n")
	if maxAttempts > 1 {
		fmt.Fprintf(&b, "Repair attempt: %d of %d.\n", attempt, maxAttempts)
	}
	if validationErr != nil {
		b.WriteString("Validation error:\n")
		b.WriteString(strings.TrimSpace(validationErr.Error()))
		b.WriteString("\n\n")
	}
	b.WriteString("Previous final response:\n")
	b.WriteString(safeTelemetryExcerpt(invalid, 1200))
	b.WriteString("\n\nReturn compact structured lines only. Do not call tools. Do not output tool names or code.\n")
	switch strings.TrimSpace(phase.FinalOutputKind) {
	case "child_summary":
		b.WriteString("Required format:\n")
		b.WriteString("status: solved|partial|blocked\n")
		b.WriteString("answer: <answer or empty>\n")
		b.WriteString("checks: <one compact check or blocker>\n")
		b.WriteString("Do not mention runtime depth, recursion budget, subagents, rlm_query, rlm_wait, or rlm_result. If blocked, name only the mathematical or information blocker.\n")
		b.WriteString("Do not mark circular-looking dependencies as blocked. Treat them as simultaneous constraints or a fixed-point problem; report the attempted values/checks instead.\n")
		b.WriteString("A response containing circular dependency, dependency cycle, or external resolution as a blocker will be rejected again.\n")
	default:
		b.WriteString("Return only the required final format for this phase.\n")
	}
	return b.String()
}

func buildREPLCodeRepairPrompt(originalPrompt string, phase REPLRunnerPhase, prior engine.EngineOutput, invalidCode, errText string, attempt, maxAttempts int) string {
	var b strings.Builder
	b.WriteString(buildREPLPhasePrompt(originalPrompt, phase, prior, replRunnerRunState{}))
	b.WriteString("\n\nREPL code repair required.\n")
	if maxAttempts > 1 {
		fmt.Fprintf(&b, "Repair attempt: %d of %d.\n", attempt, maxAttempts)
	}
	if errText = strings.TrimSpace(errText); errText != "" {
		b.WriteString("Runtime error:\n")
		b.WriteString(errText)
		b.WriteString("\n\n")
	}
	b.WriteString("Previous REPL code response:\n")
	b.WriteString(safeTelemetryExcerpt(invalidCode, 1600))
	b.WriteString("\n\nReturn raw executable code only. Do not use markdown fences, prose, JSON, or tool-call syntax.\n")
	b.WriteString("The first non-empty line must be executable code, not a comment.\n")
	b.WriteString("Do not explain, derive, or narrate in comments. Use at most one short comment if absolutely necessary.\n")
	b.WriteString("The code must print a non-empty compact result in its first five executable lines.\n")
	if len(phase.RequiredREPLCodeSubstrings) > 0 {
		b.WriteString("The code must include these exact substrings: ")
		b.WriteString(strings.Join(phase.RequiredREPLCodeSubstrings, ", "))
		b.WriteString(".\n")
	}
	b.WriteString("Use only built-in language features and standard library imports. Do not import sympy, numpy, scipy, pandas, networkx, or any third-party package.\n")
	b.WriteString("If the full solution is unclear, print a compact pass=false/blocker witness instead of writing a long derivation.\n")
	b.WriteString("Use the same REPL language selected for this phase.")
	return b.String()
}

func buildREPLCodeFilterPrompt(originalPrompt string, phase REPLRunnerPhase, prior engine.EngineOutput, overlongCode string) string {
	var b strings.Builder
	b.WriteString(buildREPLPhasePrompt(originalPrompt, phase, prior, replRunnerRunState{}))
	b.WriteString("\n\nREPL code filter required.\n")
	b.WriteString("The previous response exceeded the scratch code budget. Treat it as exploration notes, not as final code.\n")
	b.WriteString("Extract the smallest executable witness program that preserves the explored candidates, constraints, and checks.\n")
	b.WriteString("Return raw executable code only. Do not add prose, markdown fences, JSON wrappers, or tool-call syntax.\n")
	b.WriteString("The first non-empty line must be executable code, not a comment. Keep comments rare and short.\n")
	if phase.MaxREPLCodeLines > 0 {
		fmt.Fprintf(&b, "The filtered code must be at most %d non-empty lines.\n", phase.MaxREPLCodeLines)
	}
	if phase.MaxREPLCodeCommentLines > 0 {
		fmt.Fprintf(&b, "The filtered code must contain at most %d comment lines.\n", phase.MaxREPLCodeCommentLines)
	}
	if len(phase.RequiredREPLCodeSubstrings) > 0 {
		b.WriteString("The filtered code must include these exact substrings: ")
		b.WriteString(strings.Join(phase.RequiredREPLCodeSubstrings, ", "))
		b.WriteString(".\n")
	}
	b.WriteString("If the exploration did not find a satisfying candidate, preserve that result as executable code that prints a compact pass=false witness with observed and expected fields.\n")
	b.WriteString("Overlong exploration response:\n")
	b.WriteString(safeTelemetryExcerpt(overlongCode, 5000))
	return b.String()
}

func runCyclePacketFilter(ctx context.Context, llmCfg engine.LLMChatConfig, systemPrompt string, task rlm.Task, phase REPLRunnerPhase, prior engine.EngineOutput, overlongOutput string) (engine.EngineOutput, error) {
	filterMaxTokens := phaseOutputFilterMaxTokens(phase, phase.MaxTokens)
	filterCfg := llmCfg
	filterCfg.MaxTokens = filterMaxTokens
	filterCfg.MaxIterations = 1
	filterCfg.ToolChoice = nil
	filterCfg.ParseReasoningToolCalls = false
	filterLLM, err := engine.NewLLMChatEngine(filterCfg)
	if err != nil {
		return engine.EngineOutput{}, err
	}
	filterOutput, runErr := filterLLM.Run(ctx, engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages: []engine.Message{engine.NewUserMessage(buildCyclePacketFilterPrompt(
			task.Prompt,
			phase,
			prior,
			overlongOutput,
		))},
		Tools:       nil,
		Workspace:   task.WorkspaceRoot,
		MaxTokens:   filterMaxTokens,
		Temperature: llmCfg.Temperature,
	})
	if validationErr := validateREPLAttemptOutput(filterOutput, runErr, filterMaxTokens); validationErr != nil {
		return filterOutput, validationErr
	}
	if err := validateCyclePacketText(filterOutput.AssistantText); err != nil {
		return filterOutput, err
	}
	return filterOutput, nil
}

func buildCyclePacketFilterPrompt(originalPrompt string, phase REPLRunnerPhase, prior engine.EngineOutput, overlongOutput string) string {
	var b strings.Builder
	b.WriteString(buildREPLPhasePrompt(originalPrompt, phase, prior, replRunnerRunState{}))
	b.WriteString("\n\nCycle packet filter required.\n")
	b.WriteString("The previous cycle_packet response exceeded the JSON budget. Treat it as exploration notes.\n")
	b.WriteString("Return one compact raw JSON object only. Do not include markdown, prose, code, or explanations.\n")
	b.WriteString("Required keys: unknowns, known_values, constraints, candidate_bounds, requested_outputs, blockers.\n")
	b.WriteString("Preserve finite bounds and concrete constraints. If bounds are missing, put a short explanation string in candidate_bounds and blockers.\n")
	b.WriteString("Overlong cycle_packet response:\n")
	b.WriteString(safeTelemetryExcerpt(overlongOutput, 5000))
	return b.String()
}

func summarizeBraidGraphForPrompt(graph *BraidGraph) string {
	if graph == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- version: %d\n", graph.Version)
	fmt.Fprintf(&b, "- final_node: %s\n", graph.FinalNode)
	for _, node := range graph.Nodes {
		line := fmt.Sprintf("- node %s [%s]", node.ID, node.Kind)
		if len(node.DependsOn) > 0 {
			line += " deps=" + strings.Join(node.DependsOn, ",")
		}
		if strings.TrimSpace(node.Question) != "" {
			line += " q=" + safeTelemetryExcerpt(node.Question, 160)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func summarizeREPLPhaseToolResults(output engine.EngineOutput) string {
	if len(output.ToolResults) == 0 {
		return ""
	}
	callNames := make(map[string]string, len(output.ToolCalls))
	for _, call := range output.ToolCalls {
		callNames[call.ID] = call.Name
	}
	var b strings.Builder
	for _, result := range output.ToolResults {
		name := strings.TrimSpace(callNames[result.ToolCallID])
		if name == "" {
			name = "tool"
		}
		content := strings.TrimSpace(result.Content)
		if len(content) > 2000 {
			content = content[:2000] + "\n...[truncated]"
		}
		fmt.Fprintf(&b, "- %s: %s\n", name, content)
	}
	return strings.TrimSpace(b.String())
}

func replPhaseNames(phases []REPLRunnerPhase) []string {
	if len(phases) == 0 {
		return nil
	}
	out := make([]string, 0, len(phases))
	for idx, phase := range phases {
		name := strings.TrimSpace(phase.Name)
		if name == "" {
			name = fmt.Sprintf("phase-%d", idx+1)
		}
		out = append(out, name)
	}
	return out
}

func buildREPLLLMConfig(cfg rlm.LLMConfig, task rlm.Task) engine.LLMChatConfig {
	llmCfg := engine.DefaultLLMChatConfig()
	if strings.TrimSpace(cfg.Provider) != "" {
		llmCfg.Provider = strings.TrimSpace(cfg.Provider)
	}
	if strings.TrimSpace(cfg.APIKey) != "" {
		llmCfg.APIKey = strings.TrimSpace(cfg.APIKey)
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		llmCfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	}
	if strings.TrimSpace(cfg.AuthMode) != "" {
		llmCfg.AuthMode = strings.TrimSpace(cfg.AuthMode)
	}
	if strings.TrimSpace(cfg.AuthHeader) != "" {
		llmCfg.AuthHeader = strings.TrimSpace(cfg.AuthHeader)
	}
	if cfg.AuthPrefix != "" {
		llmCfg.AuthPrefix = cfg.AuthPrefix
	}
	if strings.TrimSpace(cfg.Model) != "" {
		llmCfg.Model = strings.TrimSpace(cfg.Model)
	}
	if cfg.Timeout > 0 {
		llmCfg.Timeout = cfg.Timeout
	}
	if cfg.MaxTokens > 0 {
		llmCfg.MaxTokens = cfg.MaxTokens
	}
	if cfg.Temperature != 0 {
		llmCfg.Temperature = cfg.Temperature
	}
	if cfg.RequireToolUse {
		llmCfg.ParseReasoningToolCalls = true
	}
	llmCfg.ExtraBody = cloneMetadataMap(cfg.ExtraBody)
	if task.MaxIterations > 0 {
		llmCfg.MaxIterations = task.MaxIterations
	} else if cfg.MaxIterations > 0 {
		llmCfg.MaxIterations = cfg.MaxIterations
	}
	return llmCfg
}

func qwenNoThinkREPLSystemPrompt(systemPrompt string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	prefix := strings.TrimSpace(strings.Join([]string{
		"/no_think",
		"Qwen runtime profile: non-thinking mode.",
		"Do not emit hidden reasoning, scratch transcripts, dependency graphs, or proof prose.",
		"Keep non-tool text short. In tool phases, call the requested tool instead of explaining the plan.",
		"When calling a tool, emit only one compact tool call with valid JSON arguments. Do not put prose, markdown, or unfinished code in function.arguments.",
		"For python_repl/go_repl, keep code snippets short; split long computations into separate tool calls instead of sending a large program.",
	}, "\n"))
	if systemPrompt == "" {
		return prefix
	}
	if strings.HasPrefix(systemPrompt, "/no_think") {
		return systemPrompt
	}
	return prefix + "\n\n" + systemPrompt
}

// BuildREPLSystemPrompt returns the default no-subcall paper-style RLM prompt.
func BuildREPLSystemPrompt() string {
	return buildSandboxSystemPrompt(SandboxKindPython, false, false, false, RecursionPolicyOptional)
}

func buildSandboxSystemPrompt(kind SandboxKind, subcalls bool, asyncSubcalls bool, helperSolve bool, recursionPolicy RecursionPolicy) string {
	toolName := sandboxToolName(kind)
	language := sandboxLanguage(kind)
	scratchpad := sandboxScratchpadName(kind)
	recursionPolicy = NormalizeRecursionPolicy(recursionPolicy)

	var b strings.Builder
	b.WriteString("You are running a REPL-backed recursive language model runtime.\n\n")
	fmt.Fprintf(&b, "The full task prompt is available in the persistent %s REPL as the variable prompt.\n", language)
	fmt.Fprintf(&b, "Use %s for scratch computation, parsing, simulation, state tracking, and verification.\n", toolName)
	if NormalizeSandboxKind(kind) == SandboxKindYaegi {
		b.WriteString("Go REPL contract: send Go snippets for a persistent Yaegi session. Do not include package declarations. Prefer small statements and expressions. Use built-in println(...) to inspect values when possible.\n")
		b.WriteString("If an import is needed, send the import as its own go_repl call first, then send statements in a later go_repl call. Do not combine an import and executable statements in the same snippet.\n")
		b.WriteString("Do not write long prose inside go_repl. Use go_repl for computation only, then answer concisely in the final assistant message.\n")
		b.WriteString("If two go_repl snippets fail, stop using go_repl for that branch and return a compact answer or blocker. Do not write a dependency graph or report unless explicitly requested.\n")
	}
	if helperSolve {
		b.WriteString("Use ephemeral_helper_solve when a short-lived helper improves parsing, simulation, or verification.\n")
		b.WriteString("ephemeral_helper_solve is a helper shortcut, not a recursive rlm_query execution path.\n")
	}
	if subcalls && asyncSubcalls {
		b.WriteString("Use rlm_query to schedule bounded recursive child solves.\n")
		b.WriteString("When the task requires a child to recurse, the runtime may enforce that shape and reject flattened child answers.\n")
		if recursionPolicy == RecursionPolicyRequired {
			b.WriteString("Runtime policy: recursive decomposition is required in this run; submit at least one rlm_query child before finalizing.\n")
		} else {
			b.WriteString("Do not use rlm_query for simple one-step tasks; solve those directly after REPL inspection. Recursive decomposition is optional unless the task or runtime explicitly requires it.\n")
		}
		b.WriteString("Use rlm_wait with empty arguments ({}) to collect child results submitted in this tool session; the runtime tracks child IDs for you.\n")
		b.WriteString("Use rlm_result only if you need to re-read a child by its small child number.\n")
		b.WriteString("rlm_query, rlm_wait, and rlm_result are separate model tools, not functions inside the REPL. Never call them in Python or Go code.\n")
	} else if subcalls {
		if recursionPolicy == RecursionPolicyRequired {
			b.WriteString("Runtime policy: recursive decomposition is required in this run; submit at least one rlm_query child before finalizing.\n")
		} else {
			b.WriteString("Use rlm_query when a bounded recursive child solve is helpful before finalizing.\n")
		}
		b.WriteString("rlm_query is a separate model tool, not a function inside the REPL. Never call it in Python or Go code.\n")
	}
	b.WriteString("Do not access the network, files, hidden datasets, official verifiers, answer keys, or external tools.\n")
	b.WriteString("If the task prompt says not to use tools or code, that restriction applies to\n")
	if subcalls && asyncSubcalls {
		fmt.Fprintf(&b, "leaderboard-visible solving aids; this internal %s scratchpad and bounded rlm_query/rlm_wait recursion are still allowed in this condition.\n", scratchpad)
	} else if subcalls {
		fmt.Fprintf(&b, "leaderboard-visible solving aids; this internal %s scratchpad and bounded rlm_query recursion are still allowed in this condition.\n", scratchpad)
	} else {
		fmt.Fprintf(&b, "leaderboard-visible solving aids; this internal %s scratchpad is still allowed in this condition.\n", scratchpad)
	}
	if !subcalls {
		b.WriteString("No recursive child-query tool is available at this depth. This is expected, not an environment failure; solve directly and do not ask the user to run anything locally.\n")
	}
	if helperSolve && subcalls && asyncSubcalls {
		b.WriteString("Recommended order: compute locally, use ephemeral_helper_solve when useful, query child with rlm_query, wait with rlm_wait({}), then synthesize.\n")
	}
	b.WriteString("Before finalizing stateful or algorithmic answers, use the REPL to simulate or check the candidate answer.\n")
	b.WriteString("Return the final answer in the exact format requested by the user prompt.")
	return strings.TrimSpace(b.String())
}

func sandboxToolName(kind SandboxKind) string {
	switch NormalizeSandboxKind(kind) {
	case SandboxKindYaegi:
		return GoREPLToolName
	default:
		return PythonREPLToolName
	}
}

func sandboxLanguage(kind SandboxKind) string {
	switch NormalizeSandboxKind(kind) {
	case SandboxKindYaegi:
		return "Go"
	default:
		return "Python"
	}
}

func sandboxScratchpadName(kind SandboxKind) string {
	switch NormalizeSandboxKind(kind) {
	case SandboxKindYaegi:
		return "go_repl"
	default:
		return "python_repl"
	}
}

// NormalizeSandboxKind canonicalizes sandbox kinds and applies the default.
func NormalizeSandboxKind(kind SandboxKind) SandboxKind {
	switch strings.ToLower(strings.TrimSpace(string(kind))) {
	case "", string(SandboxKindPython):
		return SandboxKindPython
	case string(SandboxKindYaegi), "go", "golang":
		return SandboxKindYaegi
	default:
		return SandboxKind(strings.ToLower(strings.TrimSpace(string(kind))))
	}
}

// IsSupportedSandboxKind reports whether the kind is implemented.
func IsSupportedSandboxKind(kind SandboxKind) bool {
	switch NormalizeSandboxKind(kind) {
	case SandboxKindPython, SandboxKindYaegi:
		return true
	default:
		return false
	}
}

func normalizeSandboxConfig(cfg SandboxConfig, legacyPython repl.Options) SandboxConfig {
	cfg.Kind = NormalizeSandboxKind(cfg.Kind)
	if cfg.Python == (repl.Options{}) {
		cfg.Python = legacyPython
	}
	return cfg
}

type subcallSummary struct {
	Calls        int
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type replToolExecutor struct {
	sandbox            rlm.Sandbox
	budget             *Budget
	recorder           *Recorder
	identity           IdentityPlan
	parentTask         rlm.Task
	parentEnv          rlm.Environment
	rlmQuery           RLMQueryRunFunc
	asyncRLMTools      *RLMToolsExecutor
	asyncNodeStore     NodeStore
	asyncRunID         string
	asyncRootNodeID    string
	subcallsEnabled    bool
	recursionPolicy    RecursionPolicy
	currentDepth       int
	summary            subcallSummary
	nextID             int
	nextSubcallID      int
	replToolName       string
	sandboxKind        SandboxKind
	defaultREPLCode    string
	defaultQueryPrompt string
	extraToolExecutor  engine.ToolExecutor
	helperFactory      *HelperFactoryTools
	toolResultMaxChars int
}

func (e *replToolExecutor) List() []engine.ToolDef {
	toolName := e.replToolName
	if strings.TrimSpace(toolName) == "" {
		toolName = PythonREPLToolName
	}
	language := sandboxLanguage(e.sandboxKind)
	tools := []engine.ToolDef{{
		Name:        toolName,
		Description: fmt.Sprintf("Execute %s code in a persistent prompt-bound scratch REPL. The variable prompt contains the task prompt.", language),
		Parameters:  json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"code":{"type":"string","description":"%s code to execute in the persistent REPL."}},"required":["code"],"additionalProperties":false}`, language)),
	}}
	if e.allowAsyncRLMTools() {
		tools = append(tools, e.asyncRLMTools.List()...)
	} else if e.allowRLMQueryTool() {
		tools = append(tools, engine.ToolDef{
			Name:        RLMQueryToolName,
			Description: "Run a bounded recursive child RLM query and return its answer and metadata.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string","description":"Child query prompt."},"max_iterations":{"type":"integer","minimum":1,"description":"Optional child iteration cap override."}},"required":["prompt"],"additionalProperties":false}`),
		})
	}
	if e.extraToolExecutor != nil {
		tools = append(tools, e.extraToolExecutor.List()...)
	}
	return tools
}

func (e *replToolExecutor) pendingSubcallCorrectionTools() []engine.ToolDef {
	if !e.allowAsyncRLMTools() {
		return nil
	}
	defs := e.asyncRLMTools.List()
	out := make([]engine.ToolDef, 0, len(defs))
	for _, def := range defs {
		switch def.Name {
		case RLMWaitToolName, RLMResultToolName:
			out = append(out, def)
		}
	}
	return out
}

func (e *replToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	toolName := e.replToolName
	if strings.TrimSpace(toolName) == "" {
		toolName = PythonREPLToolName
	}
	switch name {
	case toolName:
		return e.executeScratchREPL(ctx, args)
	case RLMQueryToolName:
		if e.allowAsyncRLMTools() {
			return e.executeAsyncRLMTool(ctx, name, args)
		}
		if !e.allowRLMQueryTool() {
			return "", fmt.Errorf("unknown RLM REPL tool %q", name)
		}
		return e.executeRLMQuery(ctx, args)
	case RLMWaitToolName, RLMResultToolName:
		if !e.allowAsyncRLMTools() {
			return "", fmt.Errorf("unknown RLM REPL tool %q", name)
		}
		return e.executeAsyncRLMTool(ctx, name, args)
	default:
		if e.extraToolExecutor != nil && extraToolNameAllowed(e.extraToolExecutor, name) {
			return e.extraToolExecutor.Execute(ctx, name, args)
		}
		return "", fmt.Errorf("unknown RLM REPL tool %q", name)
	}
}

func extraToolNameAllowed(executor engine.ToolExecutor, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || executor == nil {
		return false
	}
	for _, tool := range executor.List() {
		if strings.TrimSpace(tool.Name) == name {
			return true
		}
	}
	return false
}

func (e *replToolExecutor) executeScratchREPL(ctx context.Context, args json.RawMessage) (string, error) {
	var input struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("decode %s args: %w", e.replToolName, err)
	}
	if strings.TrimSpace(input.Code) == "" {
		input.Code = e.defaultREPLCode
	}
	if strings.TrimSpace(input.Code) == "" {
		return "", fmt.Errorf("%s requires non-empty code", e.replToolName)
	}
	if err := e.budget.ConsumeREPLCall(ctx); err != nil {
		e.recorder.RecordBudgetEvent(BudgetEvent{Limit: LimitREPLCalls, Message: err.Error()})
		return "", err
	}

	e.nextID++
	callID := fmt.Sprintf("repl-%d", e.nextID)
	e.recorder.RecordREPLCall(REPLCallEvent{CallID: callID, Input: input.Code})
	result, err := e.sandbox.Execute(ctx, input.Code)
	if err != nil {
		e.recorder.RecordREPLResult(REPLResultEvent{CallID: callID, Success: false, Output: err.Error()})
		return "", err
	}
	ok := boolFromAny(result.Metadata["ok"])
	if result.Metadata == nil {
		ok = true
	}
	output := result.Output
	metadata := cloneMetadataMap(result.Metadata)
	maxOutputChars := e.effectiveToolResultMaxChars()
	cappedOutput, outputTruncated, originalOutputChars := truncateREPLToolOutput(output, maxOutputChars)
	if outputTruncated {
		output = cappedOutput
		metadata["output_truncated"] = true
		metadata["output_original_chars"] = originalOutputChars
		metadata["output_max_chars"] = maxOutputChars
	}
	e.recorder.RecordREPLResult(REPLResultEvent{
		CallID:     callID,
		Output:     result.Output,
		Success:    ok,
		DurationMS: result.DurationMS,
	})
	body, err := json.Marshal(map[string]any{
		"ok":          ok,
		"output":      output,
		"duration_ms": result.DurationMS,
		"metadata":    metadata,
	})
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (e *replToolExecutor) effectiveToolResultMaxChars() int {
	if e == nil || e.toolResultMaxChars <= 0 {
		return 0
	}
	return e.toolResultMaxChars
}

func cloneMetadataMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in)+3)
	for key, value := range in {
		out[key] = value
	}
	return out
}

func truncateREPLToolOutput(output string, maxChars int) (string, bool, int) {
	originalChars := len(output)
	if maxChars <= 0 || originalChars <= maxChars {
		return output, false, originalChars
	}
	marker := fmt.Sprintf("\n...[truncated by rlm runtime: original_chars=%d max_chars=%d]", originalChars, maxChars)
	keep := maxChars - len(marker)
	if keep <= 0 {
		if len(marker) <= maxChars {
			return marker, true, originalChars
		}
		return marker[:maxChars], true, originalChars
	}
	return output[:keep] + marker, true, originalChars
}

func (e *replToolExecutor) executeRLMQuery(ctx context.Context, args json.RawMessage) (_ string, err error) {
	var input struct {
		Prompt        string `json:"prompt"`
		MaxIterations int    `json:"max_iterations,omitempty"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("decode rlm_query args: %w", err)
	}
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Prompt == "" {
		input.Prompt = e.defaultQueryPrompt
	}
	if input.Prompt == "" {
		return "", fmt.Errorf("rlm_query requires non-empty prompt")
	}

	childDepth := e.currentDepth + 1
	if err := e.budget.CheckDepth(childDepth); err != nil {
		e.recordBudgetError(LimitDepth, err)
		return "", err
	}
	lease, err := e.budget.ReserveSubcall(ctx)
	if err != nil {
		e.recordBudgetError(LimitSubcalls, err)
		return "", err
	}
	defer func() {
		if releaseErr := lease.Release(); releaseErr != nil {
			e.recordBudgetError(LimitSubcalls, releaseErr)
			if err == nil {
				err = releaseErr
			}
		}
	}()

	e.nextID++
	callID := fmt.Sprintf("subcall-%d", e.nextID)
	e.nextSubcallID++
	childIdentity := ChildIdentity(e.identity, e.nextSubcallID)
	e.recorder.RecordSubcallStart(SubcallEvent{
		CallID:          callID,
		Name:            RLMQueryToolName,
		Depth:           childDepth,
		AgentID:         childIdentity.AgentID,
		ParentAgentID:   childIdentity.ParentAgentID,
		OutputNamespace: childIdentity.OutputNamespace,
	})
	defer e.recorder.RecordSubcallEnd(SubcallEvent{
		CallID:          callID,
		Name:            RLMQueryToolName,
		Depth:           childDepth,
		AgentID:         childIdentity.AgentID,
		ParentAgentID:   childIdentity.ParentAgentID,
		OutputNamespace: childIdentity.OutputNamespace,
	})

	childTask := rlm.Task{
		Prompt:          buildChildRuntimePrompt(e.parentTask.Prompt, input.Prompt, maxInt(e.parentTask.MaxDepth-1, 0), maxInt(e.parentTask.MaxSubcalls-1, 0), 0),
		Role:            e.parentTask.Role,
		RunID:           childIdentity.RunID,
		AgentID:         childIdentity.AgentID,
		ParentAgentID:   childIdentity.ParentAgentID,
		OutputRoot:      childIdentity.OutputRoot,
		OutputNamespace: childIdentity.OutputNamespace,
		WorkspaceID:     e.parentTask.WorkspaceID,
		WorkspaceRoot:   e.parentTask.WorkspaceRoot,
		MaxDepth:        maxInt(e.parentTask.MaxDepth-1, 0),
		MaxIterations:   firstPositive(input.MaxIterations, e.parentTask.MaxIterations),
		MaxSubcalls:     maxInt(e.parentTask.MaxSubcalls-1, 0),
	}
	childResult, err := e.rlmQuery(ctx, childTask, e.parentEnv)
	if err != nil {
		return "", err
	}

	childInput, childOutput, childTotal := extractChildTokenTotals(childResult.Metadata)
	if childTotal <= 0 {
		childTotal = childInput + childOutput
	}
	if childTotal > 0 {
		if err := e.budget.ConsumeChildTokens(ctx, childTotal); err != nil {
			e.recordBudgetError(LimitChildTokens, err)
			return "", err
		}
	}

	e.summary.Calls++
	e.summary.InputTokens += childInput
	e.summary.OutputTokens += childOutput
	e.summary.TotalTokens += childTotal

	body, err := json.Marshal(map[string]any{
		"answer":     childResult.Answer,
		"metadata":   childResult.Metadata,
		"iterations": childResult.Iterations,
		"subcalls":   childResult.Subcalls,
	})
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (e *replToolExecutor) executeAsyncRLMTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	raw, err := e.asyncRLMTools.Execute(ctx, name, args)
	if err != nil {
		return "", err
	}
	if name == RLMQueryToolName {
		e.summary.Calls++
	}
	return raw, nil
}

func (e *replToolExecutor) allowRLMQueryTool() bool {
	return e != nil && e.subcallsEnabled && e.rlmQuery != nil && !e.allowAsyncRLMTools()
}

func (e *replToolExecutor) allowAsyncRLMTools() bool {
	return e != nil && e.subcallsEnabled && e.asyncRLMTools != nil
}

func (e *replToolExecutor) subcallSummary() subcallSummary {
	return e.subcallSummaryContext(context.Background())
}

func (e *replToolExecutor) subcallSummaryContext(ctx context.Context) subcallSummary {
	if e == nil {
		return subcallSummary{}
	}
	if !e.allowAsyncRLMTools() || e.asyncNodeStore == nil {
		return e.summary
	}

	runID := strings.TrimSpace(e.asyncRunID)
	rootNodeID := strings.TrimSpace(e.asyncRootNodeID)
	if runID == "" || rootNodeID == "" {
		return e.summary
	}

	children, err := e.asyncNodeStore.ListChildren(ctx, runID, rootNodeID)
	if err != nil {
		return e.summary
	}
	summary := subcallSummary{Calls: len(children)}
	for _, child := range children {
		if child.Result == nil {
			continue
		}
		input, output, total := extractChildTokenTotals(child.Result.Metadata)
		if total <= 0 {
			total = input + output
		}
		summary.InputTokens += input
		summary.OutputTokens += output
		summary.TotalTokens += total
	}
	if summary.Calls == 0 {
		return e.summary
	}
	if summary.InputTokens == 0 && summary.OutputTokens == 0 && summary.TotalTokens == 0 {
		summary.InputTokens = e.summary.InputTokens
		summary.OutputTokens = e.summary.OutputTokens
		summary.TotalTokens = e.summary.TotalTokens
	}
	return summary
}

func (e *replToolExecutor) requiredSubcallFailure(ctx context.Context) error {
	if e == nil || e.asyncNodeStore == nil {
		return nil
	}
	runID := strings.TrimSpace(e.asyncRunID)
	if runID == "" {
		runID = e.identity.RunID
	}
	if runID == "" {
		return nil
	}
	nodes, err := e.asyncNodeStore.ListNodes(ctx, runID)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Result == nil || node.Result.ErrorCode != "required_subcalls" {
			continue
		}
		message := strings.TrimSpace(node.Result.ErrorMessage)
		if message == "" {
			message = "required subcalls not satisfied"
		}
		return fmt.Errorf("%w: %s", ErrRequiredSubcallsNotSatisfied, message)
	}
	return nil
}

func (e *replToolExecutor) unfinishedSubcallFailure(ctx context.Context) error {
	if e == nil || e.asyncNodeStore == nil {
		return nil
	}
	runID := strings.TrimSpace(e.asyncRunID)
	if runID == "" {
		runID = e.identity.RunID
	}
	if runID == "" {
		return nil
	}
	nodes, err := e.asyncNodeStore.ListNodes(ctx, runID)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.ID == replRootNodeID || node.Status.IsTerminal() {
			continue
		}
		return fmt.Errorf("rlm runtime: pending subcalls remain: node %s status %s; call rlm_wait({}) until all submitted children are terminal", node.ID, node.Status)
	}
	return nil
}

func (e *replToolExecutor) staleSubcallWaitFailure(ctx context.Context) error {
	if e == nil || e.asyncNodeStore == nil || e.recorder == nil {
		return nil
	}
	runID := strings.TrimSpace(e.asyncRunID)
	if runID == "" {
		runID = e.identity.RunID
	}
	if runID == "" {
		return nil
	}
	rootNodeID := strings.TrimSpace(e.asyncRootNodeID)
	if rootNodeID == "" {
		rootNodeID = replRootNodeID
	}
	children, err := e.asyncNodeStore.ListChildren(ctx, runID, rootNodeID)
	if err != nil {
		return err
	}
	if len(children) == 0 {
		return nil
	}
	for _, child := range children {
		if !child.Status.IsTerminal() {
			return nil
		}
	}
	collected := map[string]bool{}
	sawWait := false
	for _, event := range e.recorder.Events() {
		if event.Type != EventTypeNodeWaitCompleted || event.Wait == nil {
			continue
		}
		if event.Wait.RunID != runID || event.Wait.ParentNodeID != rootNodeID {
			continue
		}
		sawWait = true
		if event.Wait.Pending > 0 {
			continue
		}
		terminal := event.Wait.Completed + event.Wait.Failed
		if terminal < len(event.Wait.ChildIDs) {
			continue
		}
		for _, childID := range event.Wait.ChildIDs {
			if childID != "" {
				collected[childID] = true
			}
		}
	}
	for _, child := range children {
		if collected[child.ID] {
			continue
		}
		if !sawWait {
			return fmt.Errorf("rlm runtime: submitted child results were never collected; call rlm_wait({}) before finalizing")
		}
		return fmt.Errorf("rlm runtime: submitted child results changed after the last rlm_wait; call rlm_wait({}) again before finalizing")
	}
	return nil
}

func (e *replToolExecutor) failedSubcallFailure(ctx context.Context) error {
	if e == nil || e.asyncNodeStore == nil {
		return nil
	}
	runID := strings.TrimSpace(e.asyncRunID)
	if runID == "" {
		runID = e.identity.RunID
	}
	if runID == "" {
		return nil
	}
	nodes, err := e.asyncNodeStore.ListNodes(ctx, runID)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.ID == replRootNodeID || node.Status != NodeStatusFailed {
			continue
		}
		message := "child failed"
		if node.Result != nil {
			message = strings.TrimSpace(node.Result.ErrorMessage)
			if message == "" {
				message = strings.TrimSpace(node.Result.Summary)
			}
		}
		if message == "" {
			message = "child failed"
		}
		return fmt.Errorf("rlm runtime: failed subcall %s: %s", node.ID, message)
	}
	return nil
}

func (e *replToolExecutor) recordBudgetError(fallback BudgetLimit, err error) {
	limit := fallback
	var exceeded LimitExceededError
	if errors.As(err, &exceeded) && exceeded.Limit != "" {
		limit = exceeded.Limit
	}
	e.recorder.RecordBudgetEvent(BudgetEvent{Limit: limit, Message: err.Error()})
}

func (e *replToolExecutor) requiredRecursionFailure(ctx context.Context) error {
	if e == nil || NormalizeRecursionPolicy(e.recursionPolicy) != RecursionPolicyRequired {
		return nil
	}
	if e.subcallSummaryContext(ctx).Calls > 0 {
		return nil
	}
	return fmt.Errorf("rlm runtime: recursion_policy=required but no rlm_query calls were submitted")
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func boolFromAny(value any) bool {
	switch raw := value.(type) {
	case bool:
		return raw
	case string:
		return strings.EqualFold(strings.TrimSpace(raw), "true")
	default:
		return false
	}
}

func toolCallNames(calls []engine.ToolCall) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		if name := strings.TrimSpace(call.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func countToolName(names []string, target string) int {
	count := 0
	for _, name := range names {
		if name == target {
			count++
		}
	}
	return count
}

// NormalizeRecursionPolicy canonicalizes recursion-policy values.
func NormalizeRecursionPolicy(policy RecursionPolicy) RecursionPolicy {
	switch strings.ToLower(strings.TrimSpace(string(policy))) {
	case "", string(RecursionPolicyOptional):
		return RecursionPolicyOptional
	case string(RecursionPolicyRequired):
		return RecursionPolicyRequired
	case string(RecursionPolicyDisabled):
		return RecursionPolicyDisabled
	default:
		return RecursionPolicyOptional
	}
}

func summarizeToolUsage(iterations []engine.IterationUsage) map[string]any {
	totalCalls := 0
	byName := map[string]int{}
	for _, iteration := range iterations {
		totalCalls += iteration.ToolCalls
		for _, name := range iteration.ToolNames {
			byName[name]++
		}
	}
	return map[string]any{
		"total_calls": totalCalls,
		"by_name":     byName,
	}
}

func extractChildTokenTotals(metadata map[string]any) (input int, output int, total int) {
	if len(metadata) == 0 {
		return 0, 0, 0
	}
	input = firstPositive(
		intFromAny(metadata["child_input_tokens"]),
		intFromAny(metadata["parent_input_tokens"]),
	)
	output = firstPositive(
		intFromAny(metadata["child_output_tokens"]),
		intFromAny(metadata["parent_output_tokens"]),
	)
	total = firstPositive(
		intFromAny(metadata["child_total_tokens"]),
		intFromAny(metadata["parent_total_tokens"]),
	)
	return input, output, total
}

func intFromAny(value any) int {
	switch raw := value.(type) {
	case int:
		return raw
	case int8:
		return int(raw)
	case int16:
		return int(raw)
	case int32:
		return int(raw)
	case int64:
		return int(raw)
	case uint:
		return int(raw)
	case uint8:
		return int(raw)
	case uint16:
		return int(raw)
	case uint32:
		return int(raw)
	case uint64:
		return int(raw)
	case float32:
		return int(raw)
	case float64:
		return int(raw)
	case json.Number:
		i, _ := raw.Int64()
		return int(i)
	default:
		return 0
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func telemetryWithIdentity(sink TelemetrySink, identity IdentityPlan, workspaceID string) TelemetrySink {
	switch typed := sink.(type) {
	case ObservabilityTelemetrySink:
		if strings.TrimSpace(typed.SessionID) == "" {
			typed.SessionID = identity.RunID
		}
		if strings.TrimSpace(typed.AgentID) == "" {
			typed.AgentID = identity.AgentID
		}
		if strings.TrimSpace(typed.WorkspaceID) == "" {
			typed.WorkspaceID = strings.TrimSpace(workspaceID)
		}
		return typed
	case *ObservabilityTelemetrySink:
		if typed == nil {
			return nil
		}
		cloned := *typed
		if strings.TrimSpace(cloned.SessionID) == "" {
			cloned.SessionID = identity.RunID
		}
		if strings.TrimSpace(cloned.AgentID) == "" {
			cloned.AgentID = identity.AgentID
		}
		if strings.TrimSpace(cloned.WorkspaceID) == "" {
			cloned.WorkspaceID = strings.TrimSpace(workspaceID)
		}
		return &cloned
	default:
		return sink
	}
}
