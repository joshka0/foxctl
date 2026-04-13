package transcriptpipeline

import (
	"fmt"
	"strings"

	historypkg "github.com/jkatigb/agentctl/internal/context/transcriptpipeline/history"
)

type HistoryQuestionID = historypkg.HistoryQuestionID

const (
	HistoryQuestionObjective         = historypkg.HistoryQuestionObjective
	HistoryQuestionActiveDirections  = historypkg.HistoryQuestionActiveDirections
	HistoryQuestionAcceptedLearnings = historypkg.HistoryQuestionAcceptedLearnings
	HistoryQuestionOpenQuestions     = historypkg.HistoryQuestionOpenQuestions
	HistoryQuestionMisunderstandings = historypkg.HistoryQuestionMisunderstandings
	HistoryQuestionGotchas           = historypkg.HistoryQuestionGotchas
	HistoryQuestionRegressions       = historypkg.HistoryQuestionRegressions
	HistoryQuestionRecurringMistakes = historypkg.HistoryQuestionRecurringMistakes
	HistoryQuestionSurprises         = historypkg.HistoryQuestionSurprises
	HistoryQuestionEpisodicHistory   = historypkg.HistoryQuestionEpisodicHistory
	HistoryQuestionNextStep          = historypkg.HistoryQuestionNextStep
)

func BuildHistoryAnswers(profile *historypkg.HistoryProfile, objective *SessionObjective, brief *InsightBrief, notable []NotableInsight, insights []DecisionInsight) []historypkg.HistoryAnswer {
	if profile == nil {
		return nil
	}
	answers := make([]historypkg.HistoryAnswer, 0, len(profile.Questions))
	if objectiveAnswer := objectiveAnswer(objective); objectiveAnswer != "" {
		answers = append(answers, historypkg.HistoryAnswer{
			QuestionID: HistoryQuestionObjective,
			Answer:     objectiveAnswer,
			Label:      normalizeObjectiveLabel(objective.Label, objective.Objective),
			Confidence: objectiveConfidence(objective),
			Evidence:   objectiveEvidence(objective),
		})
	}
	if brief != nil {
		activeDirections := primaryActiveDirections(insights, 4)
		if len(activeDirections) == 0 {
			activeDirections = brief.ActiveDirections
		}
		if len(activeDirections) > 0 {
			answers = append(answers, historypkg.HistoryAnswer{
				QuestionID: HistoryQuestionActiveDirections,
				Answer:     strings.Join(activeDirections, " | "),
				Confidence: 0.74,
			})
		}
		learningAnswers := brief.LatestLearnings
		if len(learningAnswers) == 0 {
			learningAnswers = fallbackAcceptedLearningInsights(notable, insights, 3)
		}
		if len(learningAnswers) > 0 {
			answers = append(answers, historypkg.HistoryAnswer{
				QuestionID: HistoryQuestionAcceptedLearnings,
				Answer:     strings.Join(learningAnswers, " | "),
				Confidence: 0.8,
			})
		}
		if len(brief.OpenQuestions) > 0 {
			answers = append(answers, historypkg.HistoryAnswer{
				QuestionID: HistoryQuestionOpenQuestions,
				Answer:     strings.Join(brief.OpenQuestions, " | "),
				Confidence: 0.7,
			})
		}
	}
	appendNotableAnswer := func(questionID HistoryQuestionID, kind NotableInsightKind, confidence float64) {
		items := filterNotableInsights(notable, kind)
		if len(items) == 0 {
			return
		}
		answers = append(answers, historypkg.HistoryAnswer{
			QuestionID: questionID,
			Answer:     joinNotableHeadlines(items, 3),
			Confidence: confidence,
			Evidence:   notableEvidence(items, 3),
		})
	}
	appendNotableAnswer(HistoryQuestionMisunderstandings, NotableInsightMisunderstanding, 0.82)
	appendNotableAnswer(HistoryQuestionGotchas, NotableInsightGotcha, 0.78)
	if !hasHistoryAnswer(answers, HistoryQuestionRegressions) {
		if fallback := fallbackRegressionInsights(insights, notable, 3); len(fallback) > 0 {
			answers = append(answers, historypkg.HistoryAnswer{
				QuestionID: HistoryQuestionRegressions,
				Answer:     strings.Join(fallback, " | "),
				Confidence: 0.72,
			})
		}
	}
	if !hasHistoryAnswer(answers, HistoryQuestionRecurringMistakes) {
		if fallback := fallbackRecurringMistakeInsights(insights, notable, 3); len(fallback) > 0 {
			answers = append(answers, historypkg.HistoryAnswer{
				QuestionID: HistoryQuestionRecurringMistakes,
				Answer:     strings.Join(fallback, " | "),
				Confidence: 0.7,
			})
		}
	}
	appendNotableAnswer(HistoryQuestionSurprises, NotableInsightSurprise, 0.8)
	appendNotableAnswer(HistoryQuestionEpisodicHistory, NotableInsightEpisodic, 0.66)
	if !hasHistoryAnswer(answers, HistoryQuestionSurprises) {
		if fallback := fallbackSurpriseInsights(insights, 3); len(fallback) > 0 {
			answers = append(answers, historypkg.HistoryAnswer{
				QuestionID: HistoryQuestionSurprises,
				Answer:     strings.Join(fallback, " | "),
				Confidence: 0.66,
			})
		}
	}
	if brief != nil && len(brief.Risks) > 0 && !hasHistoryAnswer(answers, HistoryQuestionGotchas) {
		answers = append(answers, historypkg.HistoryAnswer{
			QuestionID: HistoryQuestionGotchas,
			Answer:     strings.Join(brief.Risks, " | "),
			Confidence: 0.68,
		})
	}

	if nextStep := nextStepAnswer(brief, notable); nextStep != "" {
		answers = append(answers, historypkg.HistoryAnswer{
			QuestionID: HistoryQuestionNextStep,
			Answer:     nextStep,
			Confidence: 0.72,
		})
	}
	return answers
}

func fallbackAcceptedLearningInsights(notable []NotableInsight, in []DecisionInsight, limit int) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, limit)
	for _, item := range in {
		compacted := compactSummaryText(item.Summary, 160)
		if isLearningSignal(item) && (item.Status == InsightStatusAccepted || item.Status == InsightStatusSupported) {
			out = appendUniqueString(out, compacted, limit)
			continue
		}
		if item.SourceBasis == "user_approved" &&
			(item.Status == InsightStatusAccepted || item.Status == InsightStatusSupported) &&
			looksReusableLearningText(compacted) {
			out = appendUniqueString(out, compacted, limit)
			continue
		}
		if item.SourceBasis == "user_approved" &&
			(item.Kind == InsightKindDirection || item.Kind == InsightKindContext) &&
			looksReusableLearningText(compacted) &&
			hasMatchingLearningNotable(notable, compacted) {
			out = appendUniqueString(out, compacted, limit)
		}
	}
	return out
}

func hasMatchingLearningNotable(notable []NotableInsight, summary string) bool {
	if len(notable) == 0 {
		return false
	}
	summaryNorm := normalizeHistoryPackText(summary)
	for _, item := range notable {
		if item.Kind != NotableInsightProceduralLearning && item.Kind != NotableInsightSurprise {
			continue
		}
		headlineNorm := normalizeHistoryPackText(item.Headline)
		if headlineNorm == "" {
			continue
		}
		if summaryNorm == headlineNorm || strings.Contains(summaryNorm, headlineNorm) || strings.Contains(headlineNorm, summaryNorm) {
			return true
		}
	}
	return false
}

func fallbackSurpriseInsights(in []DecisionInsight, limit int) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, limit)
	for _, item := range in {
		if item.SourceBasis == "sidecar_consensus" || hasInsightTag(item, "consensus") {
			out = appendUniqueString(out, item.Summary, limit)
			continue
		}
		if item.Kind == InsightKindRisk && hasInsightTag(item, "tool-receipt") {
			out = appendUniqueString(out, item.Summary, limit)
		}
	}
	return out
}

func fallbackRegressionInsights(insights []DecisionInsight, notable []NotableInsight, limit int) []string {
	out := make([]string, 0, limit)
	for _, item := range insights {
		if item.Kind == InsightKindRisk && hasInsightTag(item, "tool-receipt") {
			out = appendUniqueString(out, item.Summary, limit)
		}
	}
	if len(out) == 0 {
		for _, item := range notable {
			if item.Kind == NotableInsightGotcha {
				out = appendUniqueString(out, item.Headline, limit)
			}
		}
	}
	return out
}

func fallbackRecurringMistakeInsights(insights []DecisionInsight, notable []NotableInsight, limit int) []string {
	candidates := make([]string, 0, limit)
	for _, item := range notable {
		switch item.Kind {
		case NotableInsightMisunderstanding, NotableInsightGotcha:
			candidates = appendUniqueString(candidates, item.Headline, limit)
		}
	}
	for _, item := range insights {
		if item.Kind == InsightKindRisk && hasInsightTag(item, "tool-receipt") {
			candidates = appendUniqueString(candidates, item.Summary, limit)
		}
	}
	if len(candidates) < 2 {
		return nil
	}
	return candidates
}

func hasHistoryAnswer(in []historypkg.HistoryAnswer, id HistoryQuestionID) bool {
	for _, item := range in {
		if item.QuestionID == id && strings.TrimSpace(item.Answer) != "" {
			return true
		}
	}
	return false
}

func primaryActiveDirections(in []DecisionInsight, limit int) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, limit)
	for _, item := range in {
		if item.Kind != InsightKindDirection {
			continue
		}
		if item.Status != InsightStatusActive && item.Status != InsightStatusOpen {
			continue
		}
		if item.SourceBasis != "user" {
			continue
		}
		out = appendUniqueString(out, item.Summary, limit)
	}
	return out
}

func objectiveAnswer(objective *SessionObjective) string {
	if objective == nil {
		return ""
	}
	out := InsightFromObjective(objective)
	if len(out) == 0 {
		return ""
	}
	return out[0].Summary
}

func objectiveConfidence(objective *SessionObjective) float64 {
	if objective == nil {
		return 0
	}
	return clampConfidence(objective.Confidence)
}

func objectiveEvidence(objective *SessionObjective) []string {
	if objective == nil || len(objective.Evidence) == 0 {
		return nil
	}
	out := make([]string, 0, minInt(2, len(objective.Evidence)))
	for _, item := range objective.Evidence {
		item = summarizeInsightText(item)
		if item == "" {
			continue
		}
		out = append(out, item)
		if len(out) >= 2 {
			break
		}
	}
	return out
}

func filterNotableInsights(in []NotableInsight, kind NotableInsightKind) []NotableInsight {
	if len(in) == 0 {
		return nil
	}
	out := make([]NotableInsight, 0, len(in))
	for _, item := range in {
		if item.Kind != kind {
			continue
		}
		out = append(out, item)
	}
	return out
}

func joinNotableHeadlines(in []NotableInsight, limit int) string {
	if len(in) == 0 {
		return ""
	}
	parts := make([]string, 0, minInt(limit, len(in)))
	for _, item := range in {
		parts = appendUniqueString(parts, item.Headline, limit)
	}
	return strings.Join(parts, " | ")
}

func notableEvidence(in []NotableInsight, limit int) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, minInt(limit, len(in)))
	for _, item := range in {
		out = appendUniqueString(out, fmt.Sprintf("frames:%d-%d", item.StartFrame, item.EndFrame), limit)
	}
	return out
}

func nextStepAnswer(brief *InsightBrief, notable []NotableInsight) string {
	if brief != nil && len(brief.ActiveDirections) > 0 {
		return brief.ActiveDirections[0]
	}
	for _, item := range notable {
		switch item.Kind {
		case NotableInsightMisunderstanding, NotableInsightGotcha:
			if item.Headline != "" {
				return item.Headline
			}
		}
	}
	if brief != nil && len(brief.OpenQuestions) > 0 {
		return brief.OpenQuestions[0]
	}
	return ""
}

func normalizeHistoryPackText(text string) string {
	text = strings.TrimSpace(strings.ToLower(text))
	text = strings.TrimRight(text, ".!?")
	return strings.Join(strings.Fields(text), " ")
}
