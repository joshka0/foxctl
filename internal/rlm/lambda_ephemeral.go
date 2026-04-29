package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/runtime/engine"
	"github.com/joshka0/foxctl/internal/tooling/skillrun/ephemeral"
)

type lambdaEphemeralDraft struct {
	Source string         `json:"source"`
	Input  map[string]any `json:"input,omitempty"`
}

func (r LambdaRunner) runEphemeralHelper(ctx context.Context, task Task, cfg LambdaConfig) (Result, error) {
	var lastErr string
	var attempts []map[string]any
	for attempt := 1; attempt <= cfg.EphemeralSkillAttempts; attempt++ {
		draft, raw, err := r.draftEphemeralSkill(ctx, cfg.LLM, task.Prompt, lastErr)
		if err != nil {
			lastErr = err.Error()
			attempts = append(attempts, map[string]any{
				"attempt": attempt,
				"ok":      false,
				"stage":   "draft",
				"error":   lastErr,
				"raw":     raw,
			})
			continue
		}
		runner, err := ephemeral.NewGoSkillRunner(ephemeral.GoSkillSpec{
			Name:   "helper_solve_shortcut",
			Source: draft.Source,
		})
		if err != nil {
			lastErr = err.Error()
			attempts = append(attempts, map[string]any{
				"attempt": attempt,
				"ok":      false,
				"stage":   "validate",
				"error":   lastErr,
			})
			continue
		}
		input := draft.Input
		if input == nil {
			input = map[string]any{"prompt": task.Prompt}
		}
		helperResult, err := runner.Run(ctx, input)
		if err != nil {
			lastErr = err.Error()
			attempts = append(attempts, map[string]any{
				"attempt": attempt,
				"ok":      false,
				"stage":   "run",
				"error":   lastErr,
			})
			continue
		}
		answer, answerSanitization, ok := lambdaEphemeralAnswer(helperResult.Output, cfg.ExtractSolutionLine)
		if !ok {
			lastErr = "helper output did not include a usable answer"
			attempts = append(attempts, map[string]any{
				"attempt": attempt,
				"ok":      false,
				"stage":   "finalize",
				"error":   lastErr,
				"output":  helperResult.Output,
			})
			continue
		}
		attempts = append(attempts, map[string]any{
			"attempt": attempt,
			"ok":      true,
			"stage":   "done",
		})
		metadata := map[string]any{
			"helper_solve_attempts": attempts,
			"helper_solve_output":   helperResult.Output,
			"helper_solve_input":    input,
			"helper_solve_runner":   helperResult.Metadata,
		}
		if answerSanitization.Changed {
			metadata["output_sanitization"] = answerSanitization
		}
		return Result{
			Answer:     answer,
			Iterations: attempt,
			Subcalls:   0,
			Metadata:   metadata,
		}, nil
	}
	return Result{}, fmt.Errorf("helper solve shortcut failed after %d attempts: %s", cfg.EphemeralSkillAttempts, lastErr)
}

func (r LambdaRunner) draftEphemeralSkill(ctx context.Context, cfg LLMConfig, taskPrompt, feedback string) (lambdaEphemeralDraft, string, error) {
	llmCfg := lambdaLLMChatConfig(cfg)
	llmCfg.MaxIterations = 1

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return lambdaEphemeralDraft{}, "", fmt.Errorf("lambda ephemeral draft: init LLM: %w", err)
	}
	prompt := buildHelperSolveDraftPrompt(taskPrompt, feedback)
	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: HelperSolveDraftSystemPrompt(),
		Messages:     []engine.Message{engine.NewUserMessage(prompt)},
	})
	if err != nil {
		return lambdaEphemeralDraft{}, "", fmt.Errorf("lambda ephemeral draft: LLM call: %w", err)
	}
	raw := strings.TrimSpace(output.AssistantText)
	var draft lambdaEphemeralDraft
	if err := decodeHelperSolveDraft(raw, &draft); err != nil {
		return lambdaEphemeralDraft{}, raw, fmt.Errorf("decode draft JSON: %w", err)
	}
	if strings.TrimSpace(draft.Source) == "" {
		return lambdaEphemeralDraft{}, raw, fmt.Errorf("draft source is empty")
	}
	return draft, raw, nil
}

// HelperSolveDraftSystemPrompt returns the system instruction used to
// synthesize short-lived Go helpers for helper-solve shortcut mode.
func HelperSolveDraftSystemPrompt() string {
	return "You synthesize short-lived Go helpers. Return only one JSON object."
}

// HelperSolveDraftPromptTemplate returns the optimizer-facing prompt
// template used by helper-solve shortcut mode. Placeholders are illustrative; the
// runtime fills them with the task and any validation/runtime feedback.
func HelperSolveDraftPromptTemplate() string {
	var b strings.Builder
	b.WriteString("Task:\n")
	b.WriteString("{{task_prompt}}\n\n")
	b.WriteString("Write a short-lived Go helper for this exact task.\n")
	b.WriteString("Return only JSON with this shape: {\"source\":\"func Solve(input map[string]any) map[string]any { ... }\", \"input\":{...}}.\n")
	b.WriteString("The Go source must define Solve(input map[string]any) map[string]any.\n")
	b.WriteString("The Solve output should include an answer field beginning with \"solution =\".\n")
	b.WriteString("Use only allowed imports if needed: encoding/json, fmt, math, sort, strconv, strings.\n")
	b.WriteString("\nIf previous validation/runtime error is present:\n")
	b.WriteString("{{feedback}}\n")
	b.WriteString("Repair the helper.\n")
	return b.String()
}

func buildHelperSolveDraftPrompt(taskPrompt, feedback string) string {
	var b strings.Builder
	b.WriteString("Task:\n")
	b.WriteString(strings.TrimSpace(taskPrompt))
	b.WriteString("\n\nWrite a short-lived Go helper for this exact task.\n")
	b.WriteString("Return only JSON with this shape: {\"source\":\"func Solve(input map[string]any) map[string]any { ... }\", \"input\":{...}}.\n")
	b.WriteString("The Go source must define Solve(input map[string]any) map[string]any.\n")
	b.WriteString("The Solve output should include an answer field beginning with \"solution =\".\n")
	b.WriteString("Use only allowed imports if needed: encoding/json, fmt, math, sort, strconv, strings.\n")
	if strings.TrimSpace(feedback) != "" {
		b.WriteString("\nPrevious validation/runtime error:\n")
		b.WriteString(strings.TrimSpace(feedback))
		b.WriteString("\nRepair the helper.\n")
	}
	return b.String()
}

func decodeHelperSolveDraft(text string, draft *lambdaEphemeralDraft) error {
	for _, candidate := range helperSolveJSONCandidates(text) {
		decoder := json.NewDecoder(strings.NewReader(candidate))
		decoder.UseNumber()
		var parsed lambdaEphemeralDraft
		if err := decoder.Decode(&parsed); err != nil {
			continue
		}
		if strings.TrimSpace(parsed.Source) == "" {
			continue
		}
		*draft = parsed
		return nil
	}
	return fmt.Errorf("no valid draft JSON object found")
}

func helperSolveJSONCandidates(text string) []string {
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
		if ch != '{' {
			continue
		}
		out = append(out, text[i:])
	}
	return uniqueHelperSolveCandidates(out)
}

func uniqueHelperSolveCandidates(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func lambdaEphemeralAnswer(output map[string]any, extractSolution bool) (string, OutputSanitization, bool) {
	for _, key := range []string{"answer", "solution"} {
		value, ok := output[key].(string)
		if !ok {
			continue
		}
		value, sanitization := SanitizeOutputText(value)
		if extractSolution {
			if line, ok := ExtractSolutionLine(value); ok {
				return line, sanitization, true
			}
		}
		if value != "" {
			return value, sanitization, true
		}
	}
	return "", OutputSanitization{}, false
}
