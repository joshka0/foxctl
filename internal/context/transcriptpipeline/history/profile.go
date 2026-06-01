package history

import (
	"strings"
	"unicode/utf8"
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
				pack.NextStep = summarizeInlineText(item.Answer)
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

func splitAnswerItems(answer string) []string {
	if strings.TrimSpace(answer) == "" {
		return nil
	}
	parts := strings.Split(answer, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := summarizeInlineText(part)
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

func appendUniqueString(in []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return in
	}
	for _, item := range in {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return in
		}
	}
	if limit > 0 && len(in) >= limit {
		return in
	}
	return append(in, value)
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
		item = summarizeInlineText(item)
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

func summarizeInlineText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#-*`> ")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
	}
	if len(parts) == 0 {
		return ""
	}
	summary := parts[0]
	if len(summary) < 40 && len(parts) > 1 {
		summary = summary + " " + parts[1]
	}
	return compactSummaryText(strings.TrimSpace(summary), 200)
}

func compactSummaryText(text string, max int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, ":"); idx > 0 && idx <= 24 {
		prefix := strings.Fields(strings.TrimSpace(text[:idx]))
		if len(prefix) > 0 && len(prefix) <= 4 {
			suffix := strings.TrimSpace(text[idx+1:])
			if suffix != "" {
				text = suffix
			}
		}
	}
	text = trimLeadingConnector(text)
	return truncateInline(strings.TrimSpace(text), max)
}

func truncateInline(text string, max int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if max <= 0 {
		return text
	}
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func normalizeObjectiveLabel(label, fallback string) string {
	label = compactObjectiveLabelText(label)
	label = strings.Trim(label, "\"' ")
	if label != "" {
		return trimObjectiveLabelWords(label, 10)
	}
	fallback = compactObjectiveLabelText(fallback)
	fallback = strings.Trim(fallback, "\"' ")
	return trimObjectiveLabelWords(fallback, 10)
}

func compactObjectiveLabelText(text string) string {
	text = summarizeInlineText(strings.TrimSpace(text))
	text = compactSummaryText(text, 220)
	text = stripObjectiveLeadPhrase(text)
	text = stripObjectiveFillers(text)
	text = preferCompactObjectiveClause(text, 10)
	text = trimObjectiveTrailingPhrase(text)
	return strings.TrimSpace(truncateInline(text, 120))
}

func stripObjectiveLeadPhrase(text string) string {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	for _, prefix := range []string{
		"can we ",
		"could we ",
		"can you ",
		"could you ",
		"let's ",
		"lets ",
		"please ",
		"help me ",
		"i want to ",
		"we need to ",
		"we should ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(text[len(prefix):])
		}
	}
	return text
}

func stripObjectiveFillers(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	replacements := []struct {
		old string
		new string
	}{
		{" as well ", " "},
		{" try to ", " "},
		{"all the possible ", ""},
	}
	for _, item := range replacements {
		text = strings.ReplaceAll(text, item.old, item.new)
	}
	return strings.Join(strings.Fields(text), " ")
}

func preferCompactObjectiveClause(text string, limit int) string {
	text = strings.TrimSpace(strings.TrimRight(text, ".!?"))
	if text == "" {
		return ""
	}
	if len(strings.Fields(text)) <= limit {
		return text
	}
	parts := strings.Split(text, " and ")
	if len(parts) < 2 {
		return text
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	last = trimObjectiveTrailingPhrase(last)
	if words := len(strings.Fields(last)); words >= 3 && words <= limit {
		return last
	}
	return text
}

func trimObjectiveTrailingPhrase(text string) string {
	text = strings.TrimSpace(strings.TrimRight(text, ".!?"))
	lower := strings.ToLower(text)
	for _, suffix := range []string{" for that", " for this", " for it", " then"} {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimSpace(text[:len(text)-len(suffix)])
		}
	}
	return text
}

func trimObjectiveLabelWords(label string, limit int) string {
	label = strings.TrimSpace(strings.TrimRight(label, ".!?"))
	if label == "" || limit <= 0 {
		return label
	}
	words := strings.Fields(label)
	if len(words) <= limit {
		return label
	}
	return strings.Join(words[:limit], " ")
}

func trimLeadingConnector(text string) string {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	for _, prefix := range []string{"but ", "and ", "so ", "then "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(text[len(prefix):])
		}
	}
	return text
}
