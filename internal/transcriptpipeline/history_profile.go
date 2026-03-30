package transcriptpipeline

import (
	"fmt"
	"strings"
)

type HistoryQuestionID string

const (
	HistoryQuestionObjective         HistoryQuestionID = "objective"
	HistoryQuestionActiveDirections  HistoryQuestionID = "active_directions"
	HistoryQuestionAcceptedLearnings HistoryQuestionID = "accepted_learnings"
	HistoryQuestionOpenQuestions     HistoryQuestionID = "open_questions"
	HistoryQuestionMisunderstandings HistoryQuestionID = "misunderstandings"
	HistoryQuestionGotchas           HistoryQuestionID = "gotchas"
	HistoryQuestionRegressions       HistoryQuestionID = "regressions"
	HistoryQuestionRecurringMistakes HistoryQuestionID = "recurring_mistakes"
	HistoryQuestionSurprises         HistoryQuestionID = "surprises"
	HistoryQuestionEpisodicHistory   HistoryQuestionID = "episodic_history"
	HistoryQuestionNextStep          HistoryQuestionID = "next_step"
)

type HistoryQuestion struct {
	ID          HistoryQuestionID `json:"id"`
	Prompt      string            `json:"prompt"`
	Description string            `json:"description,omitempty"`
}

// HistoryProfile defines the stable questions we want transcript-derived history to answer.
type HistoryProfile struct {
	ProfileID string            `json:"profile_id"`
	Questions []HistoryQuestion `json:"questions"`
}

// HistoryAnswer is one answer slot for a history-profile question.
type HistoryAnswer struct {
	QuestionID HistoryQuestionID `json:"question_id"`
	Answer     string            `json:"answer"`
	Label      string            `json:"label,omitempty"`
	Confidence float64           `json:"confidence"`
	Evidence   []string          `json:"evidence,omitempty"`
}

// HistoryPack is a compact handoff surface built from history answers.
// It is meant to be directly useful to downstream agents and easy for humans to scan.
type HistoryPack struct {
	Overview          string   `json:"overview,omitempty"`
	CurrentObjective  string   `json:"current_objective,omitempty"`
	ObjectiveLabel    string   `json:"objective_label,omitempty"`
	ContinueWith      []string `json:"continue_with,omitempty"`
	AcceptedLearnings []string `json:"accepted_learnings,omitempty"`
	WatchOutFor       []string `json:"watch_out_for,omitempty"`
	Regressions       []string `json:"regressions,omitempty"`
	RecurringMistakes []string `json:"recurring_mistakes,omitempty"`
	RecentSurprises   []string `json:"recent_surprises,omitempty"`
	OpenQuestions     []string `json:"open_questions,omitempty"`
	RecentEpisode     string   `json:"recent_episode,omitempty"`
	NextStep          string   `json:"next_step,omitempty"`
	AgentBrief        string   `json:"agent_brief,omitempty"`
	HumanBrief        []string `json:"human_brief,omitempty"`
}

func DefaultHistoryProfile() *HistoryProfile {
	return &HistoryProfile{
		ProfileID: "default_history_v1",
		Questions: []HistoryQuestion{
			{ID: HistoryQuestionObjective, Prompt: "What is the current objective?", Description: "Current goal or mission the session is pushing toward."},
			{ID: HistoryQuestionActiveDirections, Prompt: "What directions are currently active?", Description: "Live directions or workstreams an agent should continue."},
			{ID: HistoryQuestionAcceptedLearnings, Prompt: "What reusable learnings were accepted?", Description: "Rules, decisions, or learnings worth carrying into later work."},
			{ID: HistoryQuestionOpenQuestions, Prompt: "What remains open?", Description: "Outstanding questions or unresolved asks."},
			{ID: HistoryQuestionMisunderstandings, Prompt: "What was misunderstood?", Description: "Corrections or misreadings that should not be repeated."},
			{ID: HistoryQuestionGotchas, Prompt: "What gotchas matter?", Description: "Pain points, traps, or operational failure modes."},
			{ID: HistoryQuestionRegressions, Prompt: "What regressed or failed recently?", Description: "Recent failures, breakages, or regressions that future work should avoid."},
			{ID: HistoryQuestionRecurringMistakes, Prompt: "What continual mistakes are repeating?", Description: "Repeated negative patterns or mistakes that should be avoided later."},
			{ID: HistoryQuestionSurprises, Prompt: "What surprised us?", Description: "Unexpected findings or cross-checks worth remembering."},
			{ID: HistoryQuestionEpisodicHistory, Prompt: "What notable episode should we remember?", Description: "Short history of the most recent turning point."},
			{ID: HistoryQuestionNextStep, Prompt: "What should the next agent do?", Description: "Best immediate continuation from the current state."},
		},
	}
}

func BuildHistoryAnswers(profile *HistoryProfile, objective *SessionObjective, brief *InsightBrief, notable []NotableInsight, insights []DecisionInsight) []HistoryAnswer {
	if profile == nil {
		return nil
	}
	answers := make([]HistoryAnswer, 0, len(profile.Questions))
	if objectiveAnswer := objectiveAnswer(objective); objectiveAnswer != "" {
		answers = append(answers, HistoryAnswer{
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
			answers = append(answers, HistoryAnswer{
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
			answers = append(answers, HistoryAnswer{
				QuestionID: HistoryQuestionAcceptedLearnings,
				Answer:     strings.Join(learningAnswers, " | "),
				Confidence: 0.8,
			})
		}
		if len(brief.OpenQuestions) > 0 {
			answers = append(answers, HistoryAnswer{
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
		answers = append(answers, HistoryAnswer{
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
			answers = append(answers, HistoryAnswer{
				QuestionID: HistoryQuestionRegressions,
				Answer:     strings.Join(fallback, " | "),
				Confidence: 0.72,
			})
		}
	}
	if !hasHistoryAnswer(answers, HistoryQuestionRecurringMistakes) {
		if fallback := fallbackRecurringMistakeInsights(insights, notable, 3); len(fallback) > 0 {
			answers = append(answers, HistoryAnswer{
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
			answers = append(answers, HistoryAnswer{
				QuestionID: HistoryQuestionSurprises,
				Answer:     strings.Join(fallback, " | "),
				Confidence: 0.66,
			})
		}
	}
	if brief != nil && len(brief.Risks) > 0 && !hasHistoryAnswer(answers, HistoryQuestionGotchas) {
		answers = append(answers, HistoryAnswer{
			QuestionID: HistoryQuestionGotchas,
			Answer:     strings.Join(brief.Risks, " | "),
			Confidence: 0.68,
		})
	}

	if nextStep := nextStepAnswer(brief, notable); nextStep != "" {
		answers = append(answers, HistoryAnswer{
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

func hasHistoryAnswer(in []HistoryAnswer, id HistoryQuestionID) bool {
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

func BuildHistoryPack(answers []HistoryAnswer) *HistoryPack {
	if len(answers) == 0 {
		return nil
	}
	pack := &HistoryPack{}
	for _, item := range answers {
		switch item.QuestionID {
		case HistoryQuestionObjective:
			pack.CurrentObjective = item.Answer
			pack.ObjectiveLabel = normalizeObjectiveLabel(item.Label, item.Answer)
		case HistoryQuestionActiveDirections:
			pack.ContinueWith = appendUniqueStrings(pack.ContinueWith, splitAnswerItems(item.Answer), 4)
		case HistoryQuestionAcceptedLearnings:
			pack.AcceptedLearnings = appendUniqueStrings(pack.AcceptedLearnings, splitAnswerItems(item.Answer), 4)
		case HistoryQuestionOpenQuestions:
			pack.OpenQuestions = appendUniqueStrings(pack.OpenQuestions, splitAnswerItems(item.Answer), 4)
		case HistoryQuestionMisunderstandings, HistoryQuestionGotchas:
			pack.WatchOutFor = appendUniqueStrings(pack.WatchOutFor, splitAnswerItems(item.Answer), 5)
		case HistoryQuestionRegressions:
			pack.Regressions = appendUniqueStrings(pack.Regressions, splitAnswerItems(item.Answer), 4)
		case HistoryQuestionRecurringMistakes:
			pack.RecurringMistakes = appendUniqueStrings(pack.RecurringMistakes, splitAnswerItems(item.Answer), 4)
		case HistoryQuestionSurprises:
			pack.RecentSurprises = appendUniqueStrings(pack.RecentSurprises, splitAnswerItems(item.Answer), 3)
		case HistoryQuestionEpisodicHistory:
			if pack.RecentEpisode == "" {
				items := splitAnswerItems(item.Answer)
				if len(items) > 0 {
					pack.RecentEpisode = items[0]
				}
			}
		case HistoryQuestionNextStep:
			if pack.NextStep == "" {
				pack.NextStep = summarizeInsightText(item.Answer)
			}
		}
	}
	pack.ContinueWith = dedupeHistoryPackDirections(pack.CurrentObjective, pack.ContinueWith, 4)
	if strings.EqualFold(normalizeHistoryPackText(pack.CurrentObjective), normalizeHistoryPackText(pack.NextStep)) {
		pack.NextStep = ""
	}
	pack.Overview = buildHistoryOverview(pack)
	pack.AgentBrief = buildAgentBrief(pack)
	pack.HumanBrief = buildHumanBrief(pack)
	if pack.Overview == "" &&
		pack.CurrentObjective == "" &&
		pack.ObjectiveLabel == "" &&
		len(pack.ContinueWith) == 0 &&
		len(pack.AcceptedLearnings) == 0 &&
		len(pack.WatchOutFor) == 0 &&
		len(pack.Regressions) == 0 &&
		len(pack.RecurringMistakes) == 0 &&
		len(pack.RecentSurprises) == 0 &&
		len(pack.OpenQuestions) == 0 &&
		pack.RecentEpisode == "" &&
		pack.NextStep == "" {
		return nil
	}
	return pack
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

func splitAnswerItems(answer string) []string {
	if strings.TrimSpace(answer) == "" {
		return nil
	}
	parts := strings.Split(answer, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := summarizeInsightText(part)
		if item == "" {
			continue
		}
		out = appendUniqueString(out, item, 8)
	}
	return out
}

func appendUniqueStrings(dst, items []string, limit int) []string {
	for _, item := range items {
		dst = appendUniqueString(dst, item, limit)
	}
	return dst
}

func buildHistoryOverview(pack *HistoryPack) string {
	objectiveText := preferredObjectiveText(pack)
	switch {
	case objectiveText != "" && len(pack.ContinueWith) > 0:
		return truncateInline("Objective: "+objectiveText+" Continue with: "+pack.ContinueWith[0], 240)
	case objectiveText != "":
		return truncateInline("Objective: "+objectiveText, 240)
	case len(pack.ContinueWith) > 0:
		return truncateInline("Continue with: "+pack.ContinueWith[0], 240)
	case pack.NextStep != "":
		return truncateInline("Next: "+pack.NextStep, 240)
	default:
		return ""
	}
}

func buildAgentBrief(pack *HistoryPack) string {
	lines := make([]string, 0, 6)
	if objectiveText := preferredObjectiveText(pack); objectiveText != "" {
		lines = append(lines, "Objective: "+objectiveText)
	}
	if len(pack.ContinueWith) > 0 {
		lines = append(lines, "Continue with: "+strings.Join(pack.ContinueWith, " | "))
	}
	if len(pack.AcceptedLearnings) > 0 {
		lines = append(lines, "Learned: "+strings.Join(pack.AcceptedLearnings, " | "))
	}
	if len(pack.WatchOutFor) > 0 {
		lines = append(lines, "Watch out for: "+strings.Join(pack.WatchOutFor, " | "))
	}
	if len(pack.Regressions) > 0 {
		lines = append(lines, "Regressions: "+strings.Join(pack.Regressions, " | "))
	}
	if len(pack.RecurringMistakes) > 0 {
		lines = append(lines, "Recurring mistakes: "+strings.Join(pack.RecurringMistakes, " | "))
	}
	if len(pack.OpenQuestions) > 0 {
		lines = append(lines, "Open: "+strings.Join(pack.OpenQuestions, " | "))
	}
	if pack.NextStep != "" {
		lines = append(lines, "Next: "+pack.NextStep)
	}
	return strings.Join(lines, "\n")
}

func buildHumanBrief(pack *HistoryPack) []string {
	out := make([]string, 0, 6)
	if objectiveText := preferredObjectiveText(pack); objectiveText != "" {
		out = append(out, "Objective: "+objectiveText)
	}
	if len(pack.ContinueWith) > 0 {
		out = append(out, "In progress: "+strings.Join(pack.ContinueWith, " | "))
	}
	if len(pack.AcceptedLearnings) > 0 {
		out = append(out, "Reusable learnings: "+strings.Join(pack.AcceptedLearnings, " | "))
	}
	if len(pack.WatchOutFor) > 0 {
		out = append(out, "Watch-outs: "+strings.Join(pack.WatchOutFor, " | "))
	}
	if len(pack.Regressions) > 0 {
		out = append(out, "Regressions: "+strings.Join(pack.Regressions, " | "))
	}
	if len(pack.RecurringMistakes) > 0 {
		out = append(out, "Recurring mistakes: "+strings.Join(pack.RecurringMistakes, " | "))
	}
	if len(pack.RecentSurprises) > 0 {
		out = append(out, "Surprises: "+strings.Join(pack.RecentSurprises, " | "))
	}
	if pack.RecentEpisode != "" {
		out = append(out, "Recent episode: "+pack.RecentEpisode)
	}
	if len(pack.OpenQuestions) > 0 {
		out = append(out, "Open questions: "+strings.Join(pack.OpenQuestions, " | "))
	}
	if pack.NextStep != "" {
		out = append(out, "Next step: "+pack.NextStep)
	}
	return out
}

func dedupeHistoryPackDirections(objective string, directions []string, limit int) []string {
	if len(directions) == 0 {
		return nil
	}
	objectiveNorm := normalizeHistoryPackText(objective)
	out := make([]string, 0, len(directions))
	for _, item := range directions {
		item = summarizeInsightText(item)
		if item == "" {
			continue
		}
		itemNorm := normalizeHistoryPackText(item)
		if objectiveNorm != "" && (itemNorm == objectiveNorm || strings.Contains(objectiveNorm, itemNorm) || strings.Contains(itemNorm, objectiveNorm)) {
			continue
		}
		out = appendUniqueString(out, item, limit)
	}
	return out
}

func normalizeHistoryPackText(text string) string {
	text = strings.TrimSpace(strings.ToLower(text))
	text = strings.TrimRight(text, ".!?")
	return strings.Join(strings.Fields(text), " ")
}

func preferredObjectiveText(pack *HistoryPack) string {
	if pack == nil {
		return ""
	}
	full := strings.TrimSpace(pack.CurrentObjective)
	label := strings.TrimSpace(pack.ObjectiveLabel)
	if full == "" {
		return label
	}
	if label == "" {
		return full
	}
	if normalizeHistoryPackText(full) == normalizeHistoryPackText(label) {
		return full
	}
	if len(label)+12 < len(full) {
		return label
	}
	return full
}
