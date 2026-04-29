package optdata

import (
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/agent/optimization"
)

// PreferenceBuildOptions controls conversion from RLM traces to prompt
// preference examples.
type PreferenceBuildOptions struct {
	AgentRole       string
	Mode            string
	TargetComponent string
	TargetProfile   string
}

// BuildPromptPreferenceExamples converts RLM trajectory rows into ranked prompt
// preference examples for foxctl's prompt optimizer.
func BuildPromptPreferenceExamples(records []TrajectoryRecord, options PreferenceBuildOptions) []optimization.PromptPreferenceExample {
	grouped := map[string][]TrajectoryRecord{}
	for _, record := range records {
		if !recordIsUsablePreferenceCandidate(record, options.TargetComponent) {
			continue
		}
		key := preferenceInputKey(record)
		grouped[key] = append(grouped[key], record)
	}

	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	examples := make([]optimization.PromptPreferenceExample, 0, len(keys))
	for _, key := range keys {
		candidates := grouped[key]
		if len(candidates) == 0 {
			continue
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			left := scoreRecordForPreference(candidates[i])
			right := scoreRecordForPreference(candidates[j])
			if left == right {
				return promptTextForComponent(candidates[i], options.TargetComponent) < promptTextForComponent(candidates[j], options.TargetComponent)
			}
			return left > right
		})
		chosen := candidates[0]
		rejected := chooseRejectedCandidate(candidates)
		if rejected.RecordType == "" {
			rejected = syntheticRejectedRecord(chosen, options.TargetComponent)
		}

		examples = append(examples, optimization.PromptPreferenceExample{
			RecordType: "rlm_prompt_preference",
			Input: optimization.PromptPreferenceInput{
				Question:       strings.TrimSpace(chosen.Prompt.User),
				Context:        compactRecordContext(chosen),
				TargetResponse: targetResponseForRecord(chosen),
				EvalCaseID:     strings.TrimSpace(chosen.RecordID),
				Category:       firstNonEmptyString(chosen.Labels["plan_mode"], chosen.Labels["mode"], "rlm"),
			},
			Chosen:   preferenceCandidate(chosen, options, "chosen"),
			Rejected: preferenceCandidate(rejected, options, "rejected"),
			Metadata: optimization.PromptPreferenceExampleMeta{
				RunID:       strings.TrimSpace(chosen.Execution.RunID),
				Provider:    strings.TrimSpace(chosen.Labels["provider"]),
				BaseURL:     strings.TrimSpace(chosen.Labels["base_url"]),
				Granularity: "rlm_trace",
				EvalCaseID:  strings.TrimSpace(chosen.RecordID),
				Category:    firstNonEmptyString(chosen.Labels["plan_mode"], chosen.Labels["mode"], "rlm"),
				Scoring: map[string]any{
					"chosen_score":   scoreRecordForPreference(chosen),
					"rejected_score": scoreRecordForPreference(rejected),
					"component":      normalizeComponent(options.TargetComponent),
				},
			},
		})
	}
	return examples
}

func recordIsUsablePreferenceCandidate(record TrajectoryRecord, component string) bool {
	if record.RecordType == "" {
		return false
	}
	return strings.TrimSpace(promptTextForComponent(record, component)) != ""
}

func preferenceInputKey(record TrajectoryRecord) string {
	question := strings.TrimSpace(record.Prompt.User)
	if question == "" {
		question = strings.TrimSpace(record.Prompt.Objective)
	}
	return question
}

func chooseRejectedCandidate(records []TrajectoryRecord) TrajectoryRecord {
	if len(records) < 2 {
		return TrajectoryRecord{}
	}
	return records[len(records)-1]
}

func syntheticRejectedRecord(chosen TrajectoryRecord, component string) TrajectoryRecord {
	rejected := chosen
	rejected.RecordID = strings.TrimSpace(chosen.RecordID) + ":baseline"
	rejected.Execution.Success = false
	rejected.Execution.OutputText = ""
	rejected.Metrics = []MetricFeedback{{Name: "success", Value: 0, Goal: MetricGoalMaximize, Source: "synthetic"}}
	rejected.Feedback = []PromptFeedback{{
		Component: normalizeComponent(component),
		Stage:     "synthetic",
		Outcome:   "baseline",
		Message:   "Baseline lacks the runtime-observed successful prompt/tool discipline from the chosen RLM trace.",
	}}
	return rejected
}

func preferenceCandidate(record TrajectoryRecord, options PreferenceBuildOptions, suffix string) optimization.PromptPreferenceCandidate {
	score := scoreRecordForPreference(record)
	passed := record.Execution.Success
	return optimization.PromptPreferenceCandidate{
		VariantID:  firstNonEmptyString(strings.TrimSpace(record.RecordID), strings.TrimSpace(record.Execution.RunID), suffix),
		AgentRole:  strings.TrimSpace(options.AgentRole),
		Mode:       firstNonEmptyString(strings.TrimSpace(options.Mode), strings.TrimSpace(record.Labels["mode"]), "rlm"),
		Prompt:     promptTextForComponent(record, options.TargetComponent),
		MeanScore:  score,
		WorstScore: score,
		PassCount:  boolCount(passed),
		OutputsByModel: []optimization.PromptPreferenceModel{{
			Model:  strings.TrimSpace(record.Execution.Model),
			Output: targetResponseForRecord(record),
			Error:  strings.TrimSpace(record.Execution.ErrorMessage),
			Score:  score,
			Passed: passed,
		}},
	}
}

func scoreRecordForPreference(record TrajectoryRecord) float64 {
	score := 0.0
	if record.Execution.Success {
		score += 1.0
	}
	for _, metric := range record.Metrics {
		name := strings.ToLower(strings.TrimSpace(metric.Name))
		switch name {
		case "success", "correct", "correctness", "accuracy":
			score += clampPreferenceScore(metric.Value)
		case "tool_calls", "iterations", "subcalls", "total_tokens", "helper_solve_attempts":
			score -= minFloat(metric.Value, 20) * 0.01
		}
	}
	if record.Execution.ErrorMessage != "" {
		score -= 0.5
	}
	return score
}

func promptTextForComponent(record TrajectoryRecord, component string) string {
	component = normalizeComponent(component)
	switch component {
	case ComponentREPLSystemPrompt, "repl_system_prompt", "system":
		if strings.TrimSpace(record.Prompt.System) != "" {
			return strings.TrimSpace(record.Prompt.System)
		}
	case ComponentHelperSolveSystem, "helper_solve_system":
		if text := contextBlockContent(record, ComponentHelperSolveSystem); text != "" {
			return text
		}
	case ComponentHelperSolveDraft, "helper_solve_draft":
		if text := contextBlockContent(record, ComponentHelperSolveDraft); text != "" {
			return text
		}
	case ComponentTaskPrompt, "task", "user":
		if strings.TrimSpace(record.Prompt.User) != "" {
			return strings.TrimSpace(record.Prompt.User)
		}
	}
	if strings.TrimSpace(record.Prompt.System) != "" {
		return strings.TrimSpace(record.Prompt.System)
	}
	return strings.TrimSpace(record.Prompt.User)
}

func contextBlockContent(record TrajectoryRecord, source string) string {
	for _, block := range record.Prompt.ContextBlocks {
		if strings.EqualFold(strings.TrimSpace(block.Source), strings.TrimSpace(source)) {
			return strings.TrimSpace(block.Content)
		}
	}
	return ""
}

func compactRecordContext(record TrajectoryRecord) string {
	var parts []string
	for _, block := range record.Prompt.ContextBlocks {
		if strings.TrimSpace(block.Content) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", strings.TrimSpace(block.Name), truncatePreferenceText(block.Content, 300)))
		if len(parts) >= 6 {
			break
		}
	}
	for _, feedback := range record.Feedback {
		if strings.TrimSpace(feedback.Message) == "" {
			continue
		}
		parts = append(parts, "feedback: "+truncatePreferenceText(feedback.Message, 300))
		if len(parts) >= 10 {
			break
		}
	}
	return strings.Join(parts, "\n")
}

func targetResponseForRecord(record TrajectoryRecord) string {
	if output := strings.TrimSpace(record.Execution.OutputText); output != "" {
		return output
	}
	if strings.TrimSpace(record.Execution.ErrorMessage) != "" {
		return record.Execution.ErrorMessage
	}
	for _, feedback := range record.Feedback {
		if strings.EqualFold(strings.TrimSpace(feedback.Component), ComponentRuntimeOutput) && strings.TrimSpace(feedback.Message) != "" {
			return strings.TrimSpace(feedback.Message)
		}
	}
	for _, feedback := range record.Feedback {
		if strings.TrimSpace(feedback.Message) != "" {
			return strings.TrimSpace(feedback.Message)
		}
	}
	return "no_observed_output"
}

func normalizeComponent(component string) string {
	component = strings.ToLower(strings.TrimSpace(component))
	component = strings.ReplaceAll(component, "-", "_")
	return component
}

func clampPreferenceScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func truncatePreferenceText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
