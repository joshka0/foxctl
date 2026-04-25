package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/runtime/engine"
	"github.com/joshka0/foxctl/internal/tooling/skillrun/ephemeral"
)

const EphemeralHelperSolveToolName = "ephemeral_helper_solve"

const (
	HelperLanguageGo     = "go"
	HelperLanguagePython = "python"
)

const (
	helperFactoryDefaultMaxSourceLines = 80
	helperFactoryDefaultMaxSourceChars = 4000
)

// HelperFactoryConfig configures an attempt-scoped helper factory. The factory
// keeps the parent model contract small: call one tool, while the runtime drafts,
// validates, retries, and runs the short-lived helper.
type HelperFactoryConfig struct {
	LLM                 rlm.LLMConfig
	TaskPrompt          string
	Attempts            int
	ExtractSolutionLine bool
	PresetName          string
	PresetSource        string
	PresetInput         map[string]any
	Language            string
	MaxSourceLines      int
	MaxSourceChars      int
	AnswerVerifier      HelperAnswerVerifier
	Search              HelperSearchConfig
}

type HelperSearchConfig struct {
	BeamWidth int
}

type HelperFactoryTools struct {
	Config HelperFactoryConfig
}

type HelperAnswerVerifier func(answer string, input map[string]any) (HelperVerifierDiagnostic, bool)

type HelperVerifierDiagnostic struct {
	Pass          bool           `json:"pass"`
	Score         float64        `json:"score,omitempty"`
	FailureKind   string         `json:"failure_kind,omitempty"`
	FailedAtStep  int            `json:"failed_at_step,omitempty"`
	FailedAction  []int          `json:"failed_action,omitempty"`
	StateBefore   any            `json:"state_before,omitempty"`
	ObservedFinal any            `json:"observed_final,omitempty"`
	ExpectedFinal any            `json:"expected_final,omitempty"`
	Message       string         `json:"message,omitempty"`
	RepairHint    string         `json:"repair_hint,omitempty"`
	Progress      map[string]any `json:"progress,omitempty"`
	Extra         map[string]any `json:"extra,omitempty"`
}

type helperFactoryDraft struct {
	Source    string         `json:"source"`
	SourceB64 string         `json:"source_b64,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
}

type helperFactoryTaskContext struct {
	Prompt       string
	DefaultInput map[string]any
	Compacted    bool
}

type helperFactoryRepairState struct {
	Stage    string
	Error    string
	Source   string
	Raw      string
	Input    map[string]any
	Output   map[string]any
	Verifier map[string]any
	Language string
}

var _ engine.ToolExecutor = (*HelperFactoryTools)(nil)

func (h *HelperFactoryTools) List() []engine.ToolDef {
	return []engine.ToolDef{{
		Name:        EphemeralHelperSolveToolName,
		Description: "Synthesize, validate, and run a short-lived helper for the current task. This helper shortcut is separate from rlm_query recursion. Returns a compact answer and helper trace.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string","description":"Optional task prompt. Omit to use the current RLM task prompt."},"instructions":{"type":"string","description":"Optional helper guidance such as answer format or domain constraints."},"input":{"type":"object","description":"Optional JSON input passed to Solve. Defaults to {\"prompt\": prompt} or the drafter's proposed input."},"max_attempts":{"type":"integer","minimum":1,"description":"Optional per-call attempt cap. It can only reduce the runtime configured attempt budget."}},"additionalProperties":false}`),
	}}
}

func (h *HelperFactoryTools) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if strings.TrimSpace(name) != EphemeralHelperSolveToolName {
		return "", fmt.Errorf("unknown helper factory tool %q", name)
	}
	var input struct {
		Prompt       string         `json:"prompt,omitempty"`
		Instructions string         `json:"instructions,omitempty"`
		Input        map[string]any `json:"input,omitempty"`
		MaxAttempts  int            `json:"max_attempts,omitempty"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("decode %s args: %w", EphemeralHelperSolveToolName, err)
		}
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(h.Config.TaskPrompt)
	}
	if prompt == "" {
		return marshalHelperFactoryOutput(map[string]any{
			"ok":    false,
			"error": "helper factory requires a task prompt",
		})
	}
	return h.solve(ctx, prompt, strings.TrimSpace(input.Instructions), input.Input, input.MaxAttempts)
}

func (h *HelperFactoryTools) AutoExecuteArgs() json.RawMessage {
	if h == nil {
		return json.RawMessage(`{}`)
	}
	taskCtx := helperFactoryTaskContextForPrompt(strings.TrimSpace(h.Config.TaskPrompt))
	if len(taskCtx.DefaultInput) == 0 {
		return json.RawMessage(`{}`)
	}
	body, err := json.Marshal(map[string]any{
		"input": cloneMapAny(taskCtx.DefaultInput),
	})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return body
}

func (h *HelperFactoryTools) solve(ctx context.Context, prompt, instructions string, explicitInput map[string]any, maxAttempts int) (string, error) {
	attemptLimit := h.attemptLimit(maxAttempts)
	var lastFeedback string
	taskCtx := helperFactoryTaskContextForPrompt(prompt)
	var repairState *helperFactoryRepairState
	attempts := make([]map[string]any, 0, attemptLimit)
	candidateBeam := make([]map[string]any, 0, attemptLimit)
	var bestVerifierCandidate map[string]any
	for attempt := 1; attempt <= attemptLimit; attempt++ {
		draft, raw, err := h.draftForAttempt(ctx, attempt, taskCtx, instructions, lastFeedback, repairState)
		if err != nil {
			lastFeedback = helperFactoryRepairFeedback("draft", err.Error(), "", raw, nil, nil)
			repairState = nil
			if strings.TrimSpace(raw) != "" {
				repairState = &helperFactoryRepairState{
					Stage:    "draft",
					Error:    err.Error(),
					Raw:      raw,
					Input:    cloneMapAny(taskCtx.DefaultInput),
					Language: h.helperLanguage(attempt),
				}
			}
			attempts = append(attempts, map[string]any{
				"attempt": attempt,
				"ok":      false,
				"stage":   "draft",
				"error":   err.Error(),
				"raw":     raw,
			})
			continue
		}
		helperLanguage := h.helperLanguage(attempt)
		if helperFactorySourceNeedsTaskRedraft(helperLanguage, draft.Source) {
			errText := fmt.Sprintf("helper source is structurally incomplete for %s", helperLanguage)
			lastFeedback = helperFactoryRepairFeedback("draft", errText, draft.Source, raw, draft.Input, nil)
			repairState = nil
			attempts = append(attempts, map[string]any{
				"attempt":  attempt,
				"ok":       false,
				"stage":    "draft",
				"error":    errText,
				"source":   draft.Source,
				"raw":      raw,
				"language": helperLanguage,
			})
			continue
		}
		runner, err := h.newHelperRunner(ctx, helperLanguage, draft.Source)
		if err != nil {
			lastFeedback = helperFactoryRepairFeedback("validate", err.Error(), draft.Source, raw, draft.Input, nil)
			repairState = &helperFactoryRepairState{
				Stage:    "validate",
				Error:    err.Error(),
				Source:   draft.Source,
				Raw:      raw,
				Input:    cloneMapAny(draft.Input),
				Language: helperLanguage,
			}
			attempts = append(attempts, map[string]any{
				"attempt":  attempt,
				"ok":       false,
				"stage":    "validate",
				"error":    err.Error(),
				"source":   draft.Source,
				"language": helperLanguage,
			})
			continue
		}
		helperInput := helperFactoryEffectiveInput(taskCtx.DefaultInput, draft.Input, explicitInput, prompt)
		result, err := runner.Run(ctx, helperInput)
		if err != nil {
			lastFeedback = helperFactoryRepairFeedback("run", err.Error(), draft.Source, raw, helperInput, nil)
			repairState = &helperFactoryRepairState{
				Stage:    "run",
				Error:    err.Error(),
				Source:   draft.Source,
				Raw:      raw,
				Input:    cloneMapAny(helperInput),
				Language: helperLanguage,
			}
			attempts = append(attempts, map[string]any{
				"attempt":  attempt,
				"ok":       false,
				"stage":    "run",
				"error":    err.Error(),
				"source":   draft.Source,
				"input":    helperInput,
				"language": helperLanguage,
			})
			continue
		}
		answer, ok := helperFactoryAnswer(result.Output, h.Config.ExtractSolutionLine)
		if !ok {
			errText := "helper output did not include a usable answer"
			lastFeedback = helperFactoryRepairFeedback("finalize", errText, draft.Source, raw, helperInput, result.Output)
			repairState = &helperFactoryRepairState{
				Stage:    "finalize",
				Error:    errText,
				Source:   draft.Source,
				Raw:      raw,
				Input:    cloneMapAny(helperInput),
				Output:   cloneMapAny(result.Output),
				Language: helperLanguage,
			}
			attempts = append(attempts, map[string]any{
				"attempt":  attempt,
				"ok":       false,
				"stage":    "finalize",
				"error":    errText,
				"source":   draft.Source,
				"input":    helperInput,
				"output":   result.Output,
				"language": helperLanguage,
			})
			continue
		}
		if h.Config.AnswerVerifier != nil {
			diag, applicable := h.Config.AnswerVerifier(answer, helperInput)
			if applicable && !diag.Pass {
				diagMap := helperVerifierDiagnosticMap(diag)
				errText := helperVerifierDiagnosticError(diag)
				candidateBeam = append(candidateBeam, map[string]any{
					"attempt":    attempt,
					"answer":     compactHelperFactoryString(answer),
					"diagnostic": diagMap,
				})
				bestVerifierCandidate = bestHelperFactoryVerifierCandidate(bestVerifierCandidate, candidateBeam[len(candidateBeam)-1])
				lastFeedback = helperFactoryRepairFeedbackWithVerifier("verify", errText, draft.Source, raw, helperInput, result.Output, helperFactoryVerifierFeedbackMap(diagMap, bestVerifierCandidate, h.Config.Search.BeamWidth))
				repairState = &helperFactoryRepairState{
					Stage:    "verify",
					Error:    errText,
					Source:   draft.Source,
					Raw:      raw,
					Input:    cloneMapAny(helperInput),
					Output:   cloneMapAny(result.Output),
					Verifier: helperFactoryVerifierFeedbackMap(diagMap, bestVerifierCandidate, h.Config.Search.BeamWidth),
					Language: helperLanguage,
				}
				attempts = append(attempts, map[string]any{
					"attempt":             attempt,
					"ok":                  false,
					"stage":               "verify",
					"error":               errText,
					"source":              draft.Source,
					"input":               helperInput,
					"output":              result.Output,
					"verifier_diagnostic": diagMap,
					"language":            helperLanguage,
				})
				continue
			}
		}
		attempts = append(attempts, map[string]any{
			"attempt":  attempt,
			"ok":       true,
			"stage":    "done",
			"source":   draft.Source,
			"preset":   strings.TrimSpace(h.Config.PresetName),
			"language": helperLanguage,
		})
		out := map[string]any{
			"ok":             true,
			"answer":         answer,
			"output_summary": compactHelperFactoryMap(result.Output),
			"input_summary":  compactHelperFactoryMap(helperInput),
			"runner":         result.Metadata,
			"provider":       strings.TrimSpace(h.Config.LLM.Provider),
			"model":          strings.TrimSpace(h.Config.LLM.Model),
			"preset":         strings.TrimSpace(h.Config.PresetName),
			"attempts":       compactHelperFactoryAttempts(attempts),
		}
		if len(candidateBeam) > 0 {
			out["candidate_beam"] = compactHelperFactoryCandidateBeam(candidateBeam)
		}
		return marshalHelperFactoryOutput(out)
	}
	out := map[string]any{
		"ok":       false,
		"error":    fmt.Sprintf("helper factory failed after %d attempts: %s", attemptLimit, helperFactoryFirstFeedbackLine(lastFeedback)),
		"provider": strings.TrimSpace(h.Config.LLM.Provider),
		"model":    strings.TrimSpace(h.Config.LLM.Model),
		"preset":   strings.TrimSpace(h.Config.PresetName),
		"attempts": compactHelperFactoryAttempts(attempts),
	}
	if len(candidateBeam) > 0 {
		out["candidate_beam"] = compactHelperFactoryCandidateBeam(candidateBeam)
	}
	return marshalHelperFactoryOutput(out)
}

func (h *HelperFactoryTools) attemptLimit(maxAttempts int) int {
	attemptLimit := h.Config.Attempts
	if attemptLimit <= 0 {
		attemptLimit = 3
	}
	if maxAttempts > 0 && maxAttempts < attemptLimit {
		return maxAttempts
	}
	return attemptLimit
}

func (h *HelperFactoryTools) draftForAttempt(ctx context.Context, attempt int, taskCtx helperFactoryTaskContext, instructions, feedback string, repairState *helperFactoryRepairState) (helperFactoryDraft, string, error) {
	if strings.TrimSpace(h.Config.PresetSource) != "" && attempt == 1 {
		if err := h.validateSourceBudget(h.Config.PresetSource); err != nil {
			return helperFactoryDraft{}, "", err
		}
		return helperFactoryDraft{
			Source: h.Config.PresetSource,
			Input:  cloneMapAny(h.Config.PresetInput),
		}, "", nil
	}
	if repairState != nil {
		return h.repair(ctx, taskCtx, repairState)
	}
	return h.draft(ctx, taskCtx, instructions, feedback)
}

func (h *HelperFactoryTools) draft(ctx context.Context, taskCtx helperFactoryTaskContext, instructions, feedback string) (helperFactoryDraft, string, error) {
	language := h.helperLanguage(0)
	llmCfg := helperFactoryLLMChatConfig(h.Config.LLM)
	llmCfg.MaxIterations = 1
	llmCfg.ToolChoice = nil
	llmCfg.ResponseFormat = helperFactoryDraftResponseFormat()
	llmCfg.UseReasoningContentAsText = true

	output, err := runHelperFactoryLLM(ctx, llmCfg, buildHelperFactoryDraftPrompt(taskCtx.Prompt, instructions, feedback, language, h.sourceBudget()))
	if err != nil {
		return helperFactoryDraft{}, "", fmt.Errorf("helper factory draft: LLM call: %w", err)
	}
	raw := strings.TrimSpace(output.AssistantText)
	if raw == "" && output.StopReason != "" && output.StopReason != engine.StopReasonEndTurn {
		detail := strings.TrimSpace(output.Error)
		if detail == "" {
			detail = string(output.StopReason)
		}
		return helperFactoryDraft{}, raw, fmt.Errorf("helper factory draft stopped with %s: %s", output.StopReason, detail)
	}
	var draft helperFactoryDraft
	if err := decodeHelperFactoryDraft(raw, &draft); err != nil {
		return helperFactoryDraft{}, raw, fmt.Errorf("decode draft JSON: %w", err)
	}
	if strings.TrimSpace(draft.Source) == "" {
		return helperFactoryDraft{}, raw, fmt.Errorf("draft source is empty")
	}
	if err := h.validateSourceBudget(draft.Source); err != nil {
		return helperFactoryDraft{}, raw, err
	}
	if len(draft.Input) == 0 && len(taskCtx.DefaultInput) > 0 {
		draft.Input = cloneMapAny(taskCtx.DefaultInput)
	}
	return draft, raw, nil
}

func (h *HelperFactoryTools) repair(ctx context.Context, taskCtx helperFactoryTaskContext, repairState *helperFactoryRepairState) (helperFactoryDraft, string, error) {
	language := strings.ToLower(strings.TrimSpace(repairState.Language))
	if language == "" {
		language = h.helperLanguage(0)
	}
	llmCfg := helperFactoryLLMChatConfig(h.Config.LLM)
	llmCfg.MaxIterations = 1
	llmCfg.ToolChoice = nil
	llmCfg.ResponseFormat = helperFactoryDraftResponseFormat()
	llmCfg.UseReasoningContentAsText = true

	output, err := runHelperFactoryLLM(ctx, llmCfg, buildHelperFactorySourceRepairPrompt(language, repairState, h.sourceBudget()))
	if err != nil {
		return helperFactoryDraft{}, "", fmt.Errorf("helper factory repair: LLM call: %w", err)
	}
	raw := strings.TrimSpace(output.AssistantText)
	if raw == "" && output.StopReason != "" && output.StopReason != engine.StopReasonEndTurn {
		detail := strings.TrimSpace(output.Error)
		if detail == "" {
			detail = string(output.StopReason)
		}
		return helperFactoryDraft{}, raw, fmt.Errorf("helper factory repair stopped with %s: %s", output.StopReason, detail)
	}
	var draft helperFactoryDraft
	if err := decodeHelperFactoryDraft(raw, &draft); err != nil {
		return helperFactoryDraft{}, raw, fmt.Errorf("decode repair JSON: %w", err)
	}
	if strings.TrimSpace(draft.Source) == "" {
		return helperFactoryDraft{}, raw, fmt.Errorf("repair source is empty")
	}
	if err := h.validateSourceBudget(draft.Source); err != nil {
		return helperFactoryDraft{}, raw, err
	}
	if len(draft.Input) == 0 && len(repairState.Input) > 0 {
		draft.Input = cloneMapAny(repairState.Input)
	}
	if len(draft.Input) == 0 && len(taskCtx.DefaultInput) > 0 {
		draft.Input = cloneMapAny(taskCtx.DefaultInput)
	}
	return draft, raw, nil
}

type helperFactorySourceBudget struct {
	MaxLines int
	MaxChars int
}

func (h *HelperFactoryTools) sourceBudget() helperFactorySourceBudget {
	maxLines := h.Config.MaxSourceLines
	if maxLines <= 0 {
		maxLines = helperFactoryDefaultMaxSourceLines
	}
	maxChars := h.Config.MaxSourceChars
	if maxChars <= 0 {
		maxChars = helperFactoryDefaultMaxSourceChars
	}
	return helperFactorySourceBudget{MaxLines: maxLines, MaxChars: maxChars}
}

func (h *HelperFactoryTools) validateSourceBudget(source string) error {
	budget := h.sourceBudget()
	source = strings.TrimSpace(source)
	if budget.MaxChars > 0 && len(source) > budget.MaxChars {
		return fmt.Errorf("helper source exceeds max chars: chars=%d max=%d", len(source), budget.MaxChars)
	}
	if budget.MaxLines > 0 {
		lines := 0
		for _, line := range strings.Split(source, "\n") {
			if strings.TrimSpace(line) != "" {
				lines++
			}
		}
		if lines > budget.MaxLines {
			return fmt.Errorf("helper source exceeds max lines: lines=%d max=%d", lines, budget.MaxLines)
		}
	}
	return nil
}

type helperSkillRunner interface {
	Run(context.Context, map[string]any) (ephemeral.GoSkillResult, error)
}

func (h *HelperFactoryTools) helperLanguage(attempt int) string {
	if strings.TrimSpace(h.Config.PresetSource) != "" && attempt == 1 {
		return HelperLanguageGo
	}
	switch strings.ToLower(strings.TrimSpace(h.Config.Language)) {
	case HelperLanguagePython:
		return HelperLanguagePython
	default:
		return HelperLanguageGo
	}
}

func (h *HelperFactoryTools) newHelperRunner(ctx context.Context, language, source string) (helperSkillRunner, error) {
	switch language {
	case HelperLanguagePython:
		return ephemeral.NewPythonSkillRunner(ctx, ephemeral.PythonSkillSpec{
			Name:   "ephemeral_helper_solver",
			Source: source,
		})
	default:
		return ephemeral.NewGoSkillRunner(ephemeral.GoSkillSpec{
			Name:   "ephemeral_helper_solver",
			Source: source,
		})
	}
}

func helperFactoryDraftResponseFormat() json.RawMessage {
	return json.RawMessage(`{"type":"json_schema","json_schema":{"name":"ephemeral_helper_draft","schema":{"type":"object","properties":{"source_b64":{"type":"string"},"source":{"type":"string"},"input":{"type":"object","additionalProperties":true}},"additionalProperties":false,"anyOf":[{"required":["source_b64"]},{"required":["source"]}]},"strict":true}}`)
}

func runHelperFactoryLLM(ctx context.Context, cfg engine.LLMChatConfig, prompt string) (engine.EngineOutput, error) {
	output, err := runHelperFactoryLLMOnce(ctx, cfg, prompt)
	if len(cfg.ResponseFormat) == 0 || (err == nil && !helperFactoryShouldRetryWithoutResponseFormat(output)) {
		return output, err
	}
	plainCfg := cfg
	plainCfg.ResponseFormat = nil
	plainOutput, plainErr := runHelperFactoryLLMOnce(ctx, plainCfg, prompt)
	if plainErr == nil {
		return plainOutput, nil
	}
	if err == nil {
		detail := strings.TrimSpace(output.Error)
		if detail == "" {
			detail = string(output.StopReason)
		}
		return output, fmt.Errorf("strict response_format stopped with %s: %s; retry without response_format: %v", output.StopReason, detail, plainErr)
	}
	return output, fmt.Errorf("%w; retry without response_format: %v", err, plainErr)
}

func helperFactoryShouldRetryWithoutResponseFormat(output engine.EngineOutput) bool {
	return strings.TrimSpace(output.AssistantText) == "" &&
		output.StopReason != "" &&
		output.StopReason != engine.StopReasonEndTurn
}

func runHelperFactoryLLMOnce(ctx context.Context, cfg engine.LLMChatConfig, prompt string) (engine.EngineOutput, error) {
	llm, err := engine.NewLLMChatEngine(cfg)
	if err != nil {
		return engine.EngineOutput{}, fmt.Errorf("init LLM: %w", err)
	}
	return llm.Run(ctx, engine.EngineInput{
		SystemPrompt: helperFactorySystemPrompt(),
		Messages: []engine.Message{engine.NewUserMessage(
			prompt,
		)},
	})
}

func helperFactorySourceNeedsTaskRedraft(language, source string) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(language)) {
	case HelperLanguagePython:
		return !helperFactoryPythonSourceIsSubstantive(source)
	default:
		return !helperFactoryGoSourceIsSubstantive(source)
	}
}

func helperFactoryPythonSourceIsSubstantive(source string) bool {
	source = strings.TrimSpace(source)
	lower := strings.ToLower(source)
	idx := strings.Index(lower, "def solve")
	if idx < 0 {
		return false
	}
	bodyStart := strings.Index(source[idx:], ":")
	if bodyStart < 0 {
		return false
	}
	body := strings.TrimSpace(source[idx+bodyStart+1:])
	return strings.Contains(body, "return")
}

func helperFactoryGoSourceIsSubstantive(source string) bool {
	source = strings.TrimSpace(source)
	return strings.Contains(source, "func ") &&
		strings.Contains(source, "{") &&
		strings.Contains(source, "}") &&
		strings.Contains(source, "return")
}

func helperFactoryLLMChatConfig(cfg rlm.LLMConfig) engine.LLMChatConfig {
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
	if cfg.MaxIterations > 0 {
		llmCfg.MaxIterations = cfg.MaxIterations
	}
	llmCfg.ExtraBody = cloneMapAny(cfg.ExtraBody)
	return llmCfg
}

func helperFactorySystemPrompt() string {
	return "You synthesize short-lived deterministic helper programs. Return only one JSON object. Prefer source_b64 containing base64-encoded compilable/runnable source code. The decoded source must be code, not prose, not placeholders, not escaped pseudo-code, not string-concatenation that describes code, and not another JSON object."
}

func buildHelperFactoryDraftPrompt(taskPrompt, instructions, feedback, language string, budget helperFactorySourceBudget) string {
	var b strings.Builder
	b.WriteString("Task:\n")
	b.WriteString(strings.TrimSpace(taskPrompt))
	maxLines := firstPositiveInt(budget.MaxLines, helperFactoryDefaultMaxSourceLines)
	maxChars := firstPositiveInt(budget.MaxChars, helperFactoryDefaultMaxSourceChars)
	if language == HelperLanguagePython {
		b.WriteString("\n\nWrite a short-lived Python helper for this exact task.\n")
		b.WriteString("Return only JSON with this shape: {\"source_b64\":\"<base64 utf-8 Python source>\", \"input\":{}}.\n")
		b.WriteString("Use source_b64 instead of source so newlines and quotes cannot break JSON. If you cannot base64 encode, source is accepted as a fallback.\n")
		b.WriteString("The Python source must define solve(input) or Solve(input), where input is a dict.\n")
		fmt.Fprintf(&b, "Keep source short and task-scoped: at most %d non-empty lines and %d characters, no full proof transcript, no unrelated subproblems, no long comments.\n", maxLines, maxChars)
		b.WriteString("The decoded source must be actual Python source text. Do not include markdown fences, shell commands, JSON inside source, or placeholder code.\n")
		b.WriteString("Use only standard algorithm imports if needed: bisect, collections, copy, functools, heapq, itertools, json, math, operator, statistics.\n")
		b.WriteString("Do not use ellipses, placeholders, escaped braces, or backslashes before Python punctuation. Source must parse as Python after JSON decoding.\n")
		b.WriteString("Valid decoded source example: def solve(input):\\n    return {\"ok\": True, \"answer\": \"solution = 42\"}\n")
	} else {
		b.WriteString("\n\nWrite a short-lived Go helper for this exact task.\n")
		b.WriteString("Return only JSON with this shape: {\"source_b64\":\"<base64 utf-8 Go source>\", \"input\":{}}.\n")
		b.WriteString("Use source_b64 instead of source so newlines and quotes cannot break JSON. If you cannot base64 encode, source is accepted as a fallback.\n")
		b.WriteString("The Go source must define Solve(input map[string]any) map[string]any.\n")
		fmt.Fprintf(&b, "Keep source short and task-scoped: at most %d non-empty lines and %d characters, no full proof transcript, no unrelated subproblems, no long comments.\n", maxLines, maxChars)
		b.WriteString("The decoded source must be the actual Go source text. Do not wrap source in package main. Do not put imports inside strings. Do not build the source with + operators. Do not include markdown fences.\n")
		b.WriteString("Do not use ellipses, placeholders, escaped braces, or backslashes before Go punctuation. Source must parse as Go after JSON decoding.\n")
		b.WriteString("Valid decoded source example: func Solve(input map[string]any) map[string]any { return map[string]any{\"ok\": true, \"answer\": \"solution = 42\"} }\n")
	}
	b.WriteString("The Solve output should include an answer or solution field. Prefer the official answer format and use \"solution = ...\" when the task expects that shape.\n")
	b.WriteString("Keep the helper deterministic and bounded; use parsing, simulation, search, or verification rather than prose.\n")
	b.WriteString("For large state-transition tasks, do not use exhaustive BFS/DFS over complete states. Derive a constructive candidate from the state structure, then verify it deterministically before returning.\n")
	if strings.TrimSpace(instructions) != "" {
		b.WriteString("\nAdditional helper instructions:\n")
		b.WriteString(strings.TrimSpace(instructions))
		b.WriteString("\n")
	}
	if strings.TrimSpace(feedback) != "" {
		b.WriteString("\nPrevious failed attempt feedback:\n")
		b.WriteString(strings.TrimSpace(feedback))
		b.WriteString("\nRepair by returning a complete replacement JSON object. Do not repeat the same invalid source.\n")
	}
	return b.String()
}

func buildHelperFactorySourceRepairPrompt(language string, repair *helperFactoryRepairState, budget helperFactorySourceBudget) string {
	var b strings.Builder
	maxLines := firstPositiveInt(budget.MaxLines, helperFactoryDefaultMaxSourceLines)
	maxChars := firstPositiveInt(budget.MaxChars, helperFactoryDefaultMaxSourceChars)
	b.WriteString("Repair a failed helper source file. Do not solve the original task. Do not mention the task. Return only one JSON object with a complete replacement source_b64 field")
	b.WriteString(" and optional input field.\n")
	fmt.Fprintf(&b, "Replacement source must be at most %d non-empty lines and %d characters.\n", maxLines, maxChars)
	b.WriteString("\nLanguage: ")
	if language == HelperLanguagePython {
		b.WriteString("python\n")
		b.WriteString("Contract: define solve(input) or Solve(input), return a dict containing ok and answer/solution.\n")
		b.WriteString("Use clear multi-line Python. Do not compress compound for/if/while blocks after semicolons. Do not include JSON fragments, markdown, shell commands, prose, or placeholders in source.\n")
		b.WriteString("Allowed imports: bisect, collections, copy, functools, heapq, itertools, json, math, operator, statistics.\n")
		b.WriteString("Return shape example: {\"source_b64\":\"ZGVmIHNvbHZlKGlucHV0KTpcbiAgICByZXR1cm4ge1wib2tcIjogVHJ1ZSwgXCJhbnN3ZXJcIjogXCJzb2x1dGlvbiA9IDQyXCJ9\"}\n")
	} else {
		b.WriteString("go\n")
		b.WriteString("Contract: define Solve(input map[string]any) map[string]any, return a map containing ok and answer/solution.\n")
		b.WriteString("Use complete Go source without package main. Do not include JSON fragments, markdown, prose, or placeholders in source.\n")
		b.WriteString("Allowed imports: encoding/json, fmt, math, sort, strconv, strings.\n")
		b.WriteString("Return shape example: {\"source_b64\":\"ZnVuYyBTb2x2ZShpbnB1dCBtYXBbc3RyaW5nXWFueSkgbWFwW3N0cmluZ11hbnkgeyByZXR1cm4gbWFwW3N0cmluZ11hbnl7XCJva1wiOiB0cnVlLCBcImFuc3dlclwiOiBcInNvbHV0aW9uID0gNDJcIn0gfQ==\"}\n")
	}
	b.WriteString("\nFailed stage: ")
	b.WriteString(strings.TrimSpace(repair.Stage))
	b.WriteString("\nError:\n")
	b.WriteString(compactHelperFactoryLongText(repair.Error, 1600))
	b.WriteString("\n\nRepair policy:\n")
	b.WriteString("- Prefer a minimal patch to the previous source. Do not discard a mostly useful algorithm because one index, variable name, budget, or return-shape error failed.\n")
	b.WriteString("- If the previous source exceeded the source budget, keep the algorithm and remove comments, unused helpers, and redundant checks before changing the approach.\n")
	b.WriteString("- If the previous source raised a runtime exception, fix that concrete exception first and preserve the intended input shape.\n")
	b.WriteString("- If the previous source returned ok:false, either produce a real checked answer or return no usable answer; do not hide failure behind an answer string.\n")
	if strings.TrimSpace(repair.Source) != "" {
		b.WriteString("\n\nInvalid source to repair:\n")
		b.WriteString(compactHelperFactoryLongText(repair.Source, 5000))
	}
	if strings.TrimSpace(repair.Raw) != "" {
		b.WriteString("\n\nMalformed draft/output to fix:\n")
		b.WriteString(compactHelperFactoryLongText(repair.Raw, 5000))
	}
	if len(repair.Input) > 0 {
		b.WriteString("\n\nInput summary, for type expectations only:\n")
		b.WriteString(helperFactoryJSONSummary(repair.Input))
	}
	if len(repair.Output) > 0 {
		b.WriteString("\n\nPrevious output summary:\n")
		b.WriteString(helperFactoryJSONSummary(repair.Output))
	}
	if len(repair.Verifier) > 0 {
		b.WriteString("\n\nVerifier counterexample:\n")
		b.WriteString(helperFactoryJSONSummary(repair.Verifier))
		b.WriteString("\nUse this counterexample as a failing test. The replacement helper must avoid this exact failure before returning an answer.\n")
	}
	b.WriteString("\n\nReturn a complete replacement source_b64. Interpret malformed draft text generously: if source_b64 visibly contains raw source instead of base64, treat it as the source to repair. Preserve the algorithmic intent where visible, but fix JSON, syntax, indentation, imports, return shape, source budget, and runtime errors.")
	return b.String()
}

func helperFactoryTaskContextForPrompt(prompt string) helperFactoryTaskContext {
	prompt = strings.TrimSpace(prompt)
	defaultCtx := helperFactoryTaskContext{Prompt: prompt}
	instance, ok := helperFactoryExtractInstanceFields(prompt)
	if !ok {
		return defaultCtx
	}
	var b strings.Builder
	b.WriteString("Compacted visible task. Boilerplate and worked examples were removed; do not assume hidden data.\n")
	if desc := helperFactoryTaskDescription(prompt); desc != "" {
		b.WriteString("\nProblem rules:\n")
		b.WriteString(desc)
		b.WriteString("\n")
	}
	b.WriteString("\nRuntime helper input:\n")
	b.WriteString("The runtime will pass Solve a JSON-decoded map containing the fields below if your response omits input.\n")
	b.WriteString("In Go, JSON arrays arrive as []any and JSON numbers arrive as float64; convert them before arithmetic.\n")
	b.WriteString(helperFactoryInputSchemaSummary(instance))
	if exact := helperFactoryExactInputJSON(instance, 6000); exact != "" {
		b.WriteString("\nCanonical structured input JSON:\n")
		b.WriteString(exact)
		b.WriteString("\n")
	}
	if objective := helperFactoryObjectiveSummary(prompt); objective != "" {
		b.WriteString("\nObjective and answer format:\n")
		b.WriteString(objective)
		b.WriteString("\n")
	}
	return helperFactoryTaskContext{
		Prompt:       strings.TrimSpace(b.String()),
		DefaultInput: instance,
		Compacted:    true,
	}
}

func helperFactoryEffectiveInput(defaultInput, draftInput, explicitInput map[string]any, prompt string) map[string]any {
	if len(explicitInput) > 0 {
		return cloneMapAny(explicitInput)
	}
	if len(defaultInput) > 0 {
		if len(draftInput) > 0 {
			return mergeHelperFactoryInput(defaultInput, draftInput)
		}
		return cloneMapAny(defaultInput)
	}
	if len(draftInput) > 0 {
		return cloneMapAny(draftInput)
	}
	return map[string]any{"prompt": prompt}
}

func mergeHelperFactoryInput(base, overlay map[string]any) map[string]any {
	out := cloneMapAny(base)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range overlay {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out[key] = cloneAny(value)
	}
	return out
}

func helperFactoryExtractInstanceFields(prompt string) (map[string]any, bool) {
	start := helperFactoryIndexFold(prompt, "Puzzle instance:")
	if start < 0 {
		return nil, false
	}
	section := prompt[start+len("Puzzle instance:"):]
	out := map[string]any{}
	offset := 0
	for offset < len(section) {
		lineEnd := strings.IndexByte(section[offset:], '\n')
		if lineEnd < 0 {
			lineEnd = len(section) - offset
		}
		line := strings.TrimSpace(section[offset : offset+lineEnd])
		nextOffset := offset + lineEnd + 1
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			offset = nextOffset
			continue
		}
		label := strings.TrimSpace(line[:colon])
		key := helperFactoryNormalizeInputKey(label)
		if key == "" {
			offset = nextOffset
			continue
		}
		valueStartInLine := colon + 1
		valueText := strings.TrimSpace(line[valueStartInLine:])
		absoluteValueStart := offset + strings.Index(section[offset:offset+lineEnd], line) + valueStartInLine
		for absoluteValueStart < len(section) && (section[absoluteValueStart] == ' ' || section[absoluteValueStart] == '\t') {
			absoluteValueStart++
		}
		if absoluteValueStart < len(section) && section[absoluteValueStart] == '[' {
			if end := helperFactoryMatchingBracketEnd(section[absoluteValueStart:]); end >= 0 {
				raw := section[absoluteValueStart : absoluteValueStart+end+1]
				var parsed any
				if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
					out[key] = parsed
					offset = absoluteValueStart + end + 1
					continue
				}
			}
		}
		if valueText != "" {
			if parsed, ok := helperFactoryParseScalar(valueText); ok {
				out[key] = parsed
			}
		}
		offset = nextOffset
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func helperFactoryTaskDescription(prompt string) string {
	start := helperFactoryIndexFold(prompt, "Puzzle description:")
	if start < 0 {
		return ""
	}
	start += len("Puzzle description:")
	end := len(prompt)
	for _, marker := range []string{"\n\nExample:", "\n\nPuzzle instance:"} {
		if idx := helperFactoryIndexFold(prompt[start:], marker); idx >= 0 && start+idx < end {
			end = start + idx
		}
	}
	return compactHelperFactoryLongText(prompt[start:end], 2600)
}

func helperFactoryObjectiveSummary(prompt string) string {
	var lines []string
	for _, line := range strings.Split(prompt, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(lower, "find ") ||
			strings.HasPrefix(lower, "format your solution") ||
			strings.Contains(lower, "solution =") ||
			strings.Contains(lower, "mod ") ||
			strings.Contains(lower, "modulo") {
			lines = append(lines, trimmed)
		}
	}
	return compactHelperFactoryLongText(strings.Join(lines, "\n"), 1200)
}

func helperFactoryInputSchemaSummary(input map[string]any) string {
	keys := sortedHelperFactoryMapKeys(input)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString("- ")
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(helperFactoryValueSummary(input[key], 0))
		b.WriteString("\n")
	}
	return b.String()
}

func helperFactoryValueSummary(value any, depth int) string {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return "array len=0"
		}
		if depth >= 2 {
			return fmt.Sprintf("array len=%d", len(typed))
		}
		samples := make([]string, 0, 3)
		for i := 0; i < len(typed) && i < 3; i++ {
			samples = append(samples, helperFactoryValueSummary(typed[i], depth+1))
		}
		return fmt.Sprintf("array len=%d sample=[%s]", len(typed), strings.Join(samples, "; "))
	case map[string]any:
		return fmt.Sprintf("object keys=%v", sortedHelperFactoryMapKeys(typed))
	case json.Number:
		return "number"
	case float64, float32, int, int64, int32, uint, uint64, uint32:
		return fmt.Sprintf("number example=%v", typed)
	case string:
		return fmt.Sprintf("string len=%d sample=%q", len(typed), compactHelperFactoryLongText(typed, 80))
	case bool:
		return fmt.Sprintf("bool example=%v", typed)
	default:
		return fmt.Sprintf("%T", value)
	}
}

func helperFactoryParseScalar(value string) (any, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err == nil {
		return parsed, true
	}
	if strings.EqualFold(value, "true") {
		return true, true
	}
	if strings.EqualFold(value, "false") {
		return false, true
	}
	return value, true
}

func helperFactoryNormalizeInputKey(label string) string {
	label = strings.TrimSpace(strings.ToLower(label))
	var b strings.Builder
	lastUnderscore := false
	for _, ch := range label {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func helperFactoryIndexFold(text, marker string) int {
	return strings.Index(strings.ToLower(text), strings.ToLower(marker))
}

func helperFactoryMatchingBracketEnd(text string) int {
	depth := 0
	inString := false
	escaped := false
	for idx, ch := range text {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return idx
			}
		}
	}
	return -1
}

func compactHelperFactoryLongText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "...[truncated]"
}

func helperFactoryRepairFeedback(stage, errText, source, raw string, input, output map[string]any) string {
	return helperFactoryRepairFeedbackWithVerifier(stage, errText, source, raw, input, output, nil)
}

func helperFactoryRepairFeedbackWithVerifier(stage, errText, source, raw string, input, output, verifier map[string]any) string {
	var b strings.Builder
	b.WriteString("stage: ")
	b.WriteString(strings.TrimSpace(stage))
	b.WriteString("\nerror: ")
	b.WriteString(strings.TrimSpace(errText))
	if strings.TrimSpace(source) != "" {
		b.WriteString("\nprevious_source_excerpt:\n")
		b.WriteString(compactHelperFactoryString(source))
	}
	if strings.TrimSpace(raw) != "" {
		b.WriteString("\nprevious_raw_response_excerpt:\n")
		b.WriteString(compactHelperFactoryString(raw))
	}
	if len(input) > 0 {
		b.WriteString("\ninput_summary: ")
		b.WriteString(helperFactoryJSONSummary(input))
	}
	if len(output) > 0 {
		b.WriteString("\noutput_summary: ")
		b.WriteString(helperFactoryJSONSummary(output))
	}
	if len(verifier) > 0 {
		b.WriteString("\nverifier_counterexample: ")
		b.WriteString(helperFactoryJSONSummary(verifier))
	}
	return b.String()
}

func helperFactoryFirstFeedbackLine(feedback string) string {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return "unknown helper factory error"
	}
	for _, line := range strings.Split(feedback, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "error:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "error:"))
		}
	}
	return compactHelperFactoryString(feedback)
}

func helperVerifierDiagnosticMap(diag HelperVerifierDiagnostic) map[string]any {
	body, err := json.Marshal(diag)
	if err != nil {
		return map[string]any{"pass": diag.Pass, "message": diag.Message}
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return map[string]any{"pass": diag.Pass, "message": diag.Message}
	}
	for key, value := range out {
		if value == nil || value == "" {
			delete(out, key)
		}
	}
	if diag.FailedAtStep < 0 {
		delete(out, "failed_at_step")
	}
	return out
}

func helperVerifierDiagnosticError(diag HelperVerifierDiagnostic) string {
	parts := []string{"helper answer failed verifier"}
	if strings.TrimSpace(diag.FailureKind) != "" {
		parts = append(parts, "kind="+strings.TrimSpace(diag.FailureKind))
	}
	if diag.FailedAtStep >= 0 {
		parts = append(parts, fmt.Sprintf("step=%d", diag.FailedAtStep))
	}
	if strings.TrimSpace(diag.Message) != "" {
		parts = append(parts, strings.TrimSpace(diag.Message))
	}
	return strings.Join(parts, ": ")
}

func helperFactoryJSONSummary(value map[string]any) string {
	body, err := json.Marshal(compactHelperFactoryMap(value))
	if err != nil {
		return err.Error()
	}
	return string(body)
}

func helperFactoryExactInputJSON(value map[string]any, maxChars int) string {
	if len(value) == 0 {
		return ""
	}
	body, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	if maxChars > 0 && len(body) > maxChars {
		return ""
	}
	return string(body)
}

func decodeHelperFactoryDraft(text string, draft *helperFactoryDraft) error {
	for _, candidate := range helperFactoryJSONCandidates(text) {
		decoder := json.NewDecoder(strings.NewReader(candidate))
		decoder.UseNumber()
		var parsed helperFactoryDraft
		if err := decoder.Decode(&parsed); err != nil {
			continue
		}
		if err := decodeHelperFactorySource(&parsed); err != nil {
			continue
		}
		if strings.TrimSpace(parsed.Source) == "" {
			continue
		}
		*draft = parsed
		return nil
	}
	if parsed, ok := decodeMalformedHelperFactoryDraft(text); ok {
		*draft = parsed
		return nil
	}
	return fmt.Errorf("no valid draft JSON object found")
}

func decodeHelperFactorySource(draft *helperFactoryDraft) error {
	if draft == nil {
		return fmt.Errorf("nil draft")
	}
	if strings.TrimSpace(draft.SourceB64) == "" {
		draft.Source = strings.TrimSpace(draft.Source)
		return nil
	}
	body, err := decodeHelperFactorySourceB64(draft.SourceB64)
	if err != nil {
		if helperFactoryLooksLikeSource(draft.SourceB64) {
			if strings.TrimSpace(draft.Source) == "" {
				draft.Source = draft.SourceB64
			}
			draft.Source = strings.TrimSpace(draft.Source)
			return nil
		}
		return fmt.Errorf("decode source_b64: %w", err)
	}
	if !helperFactoryLooksLikeSource(string(body)) && helperFactoryLooksLikeSource(draft.SourceB64) {
		if strings.TrimSpace(draft.Source) == "" {
			draft.Source = draft.SourceB64
		}
		draft.Source = strings.TrimSpace(draft.Source)
		return nil
	}
	if strings.TrimSpace(draft.Source) == "" {
		draft.Source = string(body)
	}
	draft.Source = strings.TrimSpace(draft.Source)
	return nil
}

func decodeMalformedHelperFactoryDraft(text string) (helperFactoryDraft, bool) {
	if rawB64, ok := extractMalformedHelperFactoryField(text, "source_b64"); ok {
		body, err := decodeHelperFactorySourceB64(rawB64)
		if err == nil && helperFactoryLooksLikeSource(string(body)) {
			return helperFactoryDraft{Source: strings.TrimSpace(string(body))}, true
		}
	}
	return helperFactoryDraft{}, false
}

func extractMalformedHelperFactoryField(text, field string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.TrimSpace(field) == "" {
		return "", false
	}
	key := `"` + field + `"`
	keyIdx := strings.Index(text, key)
	if keyIdx < 0 {
		return "", false
	}
	rest := text[keyIdx+len(key):]
	colon := strings.IndexByte(rest, ':')
	if colon < 0 {
		return "", false
	}
	rest = strings.TrimLeft(rest[colon+1:], " \t\r\n")
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	var b strings.Builder
	escaped := false
	for _, ch := range rest[1:] {
		if escaped {
			switch ch {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteRune(ch)
			}
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			break
		}
		b.WriteRune(ch)
	}
	value := strings.TrimSpace(b.String())
	if value == "" {
		return "", false
	}
	return value, true
}

func decodeHelperFactorySourceB64(value string) ([]byte, error) {
	cleaned := helperFactoryCleanSourceB64(value)
	if cleaned == "" {
		return nil, fmt.Errorf("empty source_b64")
	}
	if remainder := len(cleaned) % 4; remainder != 0 {
		cleaned += strings.Repeat("=", 4-remainder)
	}
	return base64.StdEncoding.DecodeString(cleaned)
}

func helperFactoryCleanSourceB64(value string) string {
	var b strings.Builder
	for _, ch := range strings.TrimSpace(value) {
		switch {
		case ch >= 'A' && ch <= 'Z':
			b.WriteRune(ch)
		case ch >= 'a' && ch <= 'z':
			b.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch == '+' || ch == '/' || ch == '=':
			b.WriteRune(ch)
		case ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t':
			continue
		default:
			return b.String()
		}
	}
	return b.String()
}

func helperFactoryLooksLikeSource(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	return strings.Contains(lower, "def solve") ||
		strings.Contains(lower, "def solve(") ||
		strings.Contains(value, "func Solve") ||
		strings.Contains(lower, "return ") ||
		strings.Contains(lower, "import ")
}

func helperFactoryJSONCandidates(text string) []string {
	text = strings.TrimSpace(text)
	var out []string
	if strings.HasPrefix(text, "```") {
		trimmed := strings.TrimPrefix(text, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		out = append(out, strings.TrimSpace(trimmed))
	}
	out = append(out, text)
	for i, ch := range text {
		if ch == '{' {
			out = append(out, text[i:])
		}
	}
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(out))
	for _, value := range out {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func helperFactoryAnswer(output map[string]any, extractSolution bool) (string, bool) {
	if okValue, exists := output["ok"]; exists {
		if ok, isBool := okValue.(bool); isBool && !ok {
			return "", false
		}
	}
	for _, key := range []string{"answer", "solution"} {
		value, ok := output[key]
		if !ok {
			continue
		}
		if answer, ok := helperFactoryAnswerValue(key, value, extractSolution); ok {
			return answer, true
		}
	}
	return "", false
}

func helperFactoryAnswerValue(key string, value any, extractSolution bool) (string, bool) {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return "", false
		}
		if extractSolution {
			if line, ok := rlm.ExtractSolutionLine(text); ok {
				return line, true
			}
			if strings.EqualFold(key, "solution") {
				return "solution = " + text, true
			}
			return "", false
		}
		return text, true
	case map[string]any:
		if okValue, exists := typed["ok"]; exists {
			if ok, isBool := okValue.(bool); isBool && !ok {
				return "", false
			}
		}
		for _, nestedKey := range []string{"answer", "solution"} {
			nestedValue, ok := typed[nestedKey]
			if !ok {
				continue
			}
			if answer, ok := helperFactoryAnswerValue(nestedKey, nestedValue, extractSolution); ok {
				return answer, true
			}
		}
		return "", false
	case []any:
		if !strings.EqualFold(key, "solution") && !(extractSolution && strings.EqualFold(key, "answer")) {
			return "", false
		}
		body, err := json.Marshal(typed)
		if err != nil || len(body) == 0 {
			return "", false
		}
		return "solution = " + string(body), true
	default:
		if !strings.EqualFold(key, "solution") && !(extractSolution && strings.EqualFold(key, "answer")) {
			return "", false
		}
		body, err := json.Marshal(typed)
		if err != nil || len(body) == 0 || string(body) == "null" {
			return "", false
		}
		return "solution = " + string(body), true
	}
}

func helperFactoryTraceFromToolResults(results []engine.ToolResult) (map[string]any, bool) {
	for i := len(results) - 1; i >= 0; i-- {
		result := results[i]
		if result.IsError || strings.TrimSpace(result.Content) == "" {
			continue
		}
		var payload map[string]any
		decoder := json.NewDecoder(strings.NewReader(result.Content))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			continue
		}
		if _, ok := payload["attempts"]; !ok {
			if _, ok := payload["output"]; !ok {
				continue
			}
		}
		return payload, true
	}
	return nil, false
}

func compactHelperFactoryAttempts(attempts []map[string]any) []map[string]any {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(attempts))
	for _, attempt := range attempts {
		item := map[string]any{}
		for _, key := range []string{"attempt", "ok", "stage", "error", "preset"} {
			if value, ok := attempt[key]; ok {
				item[key] = value
			}
		}
		if source, ok := attempt["source"].(string); ok && strings.TrimSpace(source) != "" {
			item["source_hash"] = hashHelperFactoryString(source)
			item["source_chars"] = len(source)
			if attempt["ok"] != true {
				item["source_excerpt"] = compactHelperFactoryString(source)
			}
		}
		if raw, ok := attempt["raw"].(string); ok && strings.TrimSpace(raw) != "" {
			item["raw_hash"] = hashHelperFactoryString(raw)
			item["raw_chars"] = len(raw)
			item["raw_excerpt"] = compactHelperFactoryString(raw)
		}
		if input, ok := attempt["input"].(map[string]any); ok {
			item["input_summary"] = compactHelperFactoryMap(input)
		}
		if output, ok := attempt["output"].(map[string]any); ok {
			item["output_summary"] = compactHelperFactoryMap(output)
		}
		out = append(out, item)
	}
	return out
}

func compactHelperFactoryCandidateBeam(candidates []map[string]any) []map[string]any {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		item := map[string]any{}
		if value, ok := candidate["attempt"]; ok {
			item["attempt"] = value
		}
		if answer, ok := candidate["answer"].(string); ok && strings.TrimSpace(answer) != "" {
			item["answer"] = compactHelperFactoryString(answer)
		}
		if diagnostic, ok := candidate["diagnostic"].(map[string]any); ok {
			item["diagnostic_summary"] = compactHelperFactoryMap(diagnostic)
		}
		out = append(out, item)
	}
	return out
}

func bestHelperFactoryVerifierCandidate(current, next map[string]any) map[string]any {
	if len(next) == 0 {
		return current
	}
	if len(current) == 0 || helperFactoryCandidateScore(next) > helperFactoryCandidateScore(current) {
		return cloneMapAny(next)
	}
	return current
}

func helperFactoryCandidateScore(candidate map[string]any) float64 {
	diagnostic, _ := candidate["diagnostic"].(map[string]any)
	if len(diagnostic) == 0 {
		return 0
	}
	switch typed := diagnostic["score"].(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		score, _ := typed.Float64()
		return score
	default:
		return 0
	}
}

func helperFactoryVerifierFeedbackMap(current, best map[string]any, beamWidth int) map[string]any {
	out := map[string]any{"current": current}
	if len(best) > 0 {
		out["best_candidate"] = compactHelperFactoryCandidateBeam([]map[string]any{best})[0]
	}
	if beamWidth > 1 {
		out["search_policy"] = map[string]any{
			"kind":        "verifier_guided_candidate_repair",
			"beam_width":  beamWidth,
			"instruction": "repair the highest-scoring failed candidate first; preserve useful valid prefixes and fix the verifier counterexample",
		}
	}
	return out
}

func compactHelperFactoryMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	out := map[string]any{
		"keys":       sortedHelperFactoryMapKeys(value),
		"json_hash":  hashHelperFactoryString(string(body)),
		"json_chars": len(body),
	}
	for _, key := range []string{"answer", "solution", "error", "ok"} {
		if raw, ok := value[key]; ok {
			switch typed := raw.(type) {
			case string:
				out[key] = compactHelperFactoryString(typed)
			case bool:
				out[key] = typed
			default:
				out[key] = typed
			}
		}
	}
	for _, key := range []string{"moves", "final"} {
		if raw, ok := value[key]; ok {
			if count := helperFactorySequenceLen(raw); count >= 0 {
				out[key+"_count"] = count
			}
		}
	}
	return out
}

func sortedHelperFactoryMapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func helperFactorySequenceLen(value any) int {
	switch typed := value.(type) {
	case []any:
		return len(typed)
	case [][]int:
		return len(typed)
	case []map[string]any:
		return len(typed)
	default:
		return -1
	}
}

func compactHelperFactoryString(value string) string {
	value = strings.TrimSpace(value)
	const limit = 500
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...[truncated]"
}

func hashHelperFactoryString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func marshalHelperFactoryOutput(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
