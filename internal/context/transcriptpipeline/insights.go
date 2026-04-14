package transcriptpipeline

import (
	"regexp"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/context/companion"
)

type InsightKind string

const (
	InsightKindDecision   InsightKind = "decision"
	InsightKindPreference InsightKind = "preference"
	InsightKindGoal       InsightKind = "goal"
	InsightKindDirection  InsightKind = "direction"
	InsightKindQuestion   InsightKind = "question"
	InsightKindRisk       InsightKind = "risk"
	InsightKindContext    InsightKind = "context"
)

type InsightStatus string

const (
	InsightStatusAccepted  InsightStatus = "accepted"
	InsightStatusOpen      InsightStatus = "open"
	InsightStatusActive    InsightStatus = "active"
	InsightStatusSupported InsightStatus = "supported"
)

// DecisionInsight is a compact, decision-oriented signal derived from transcript activity.
type DecisionInsight struct {
	Kind                 InsightKind   `json:"kind"`
	Summary              string        `json:"summary"`
	Status               InsightStatus `json:"status"`
	Confidence           float64       `json:"confidence"`
	SourceBasis          string        `json:"source_basis,omitempty"`
	EvidenceFrameIndices []int         `json:"evidence_frame_indices,omitempty"`
	Tags                 []string      `json:"tags,omitempty"`
}

// InsightBrief is a compact, scan-friendly view of decision support signals.
type InsightBrief struct {
	Overview         string   `json:"overview,omitempty"`
	CurrentGoals     []string `json:"current_goals,omitempty"`
	ActiveDirections []string `json:"active_directions,omitempty"`
	AcceptedItems    []string `json:"accepted_items,omitempty"`
	LatestLearnings  []string `json:"latest_accepted_learnings,omitempty"`
	OpenQuestions    []string `json:"open_questions,omitempty"`
	Risks            []string `json:"risks,omitempty"`
	Context          []string `json:"context,omitempty"`
}

// InsightTimelineEntry captures a notable window of transcript activity for timeline/history views.
type InsightTimelineEntry struct {
	StartFrame    int      `json:"start_frame"`
	EndFrame      int      `json:"end_frame"`
	Headline      string   `json:"headline"`
	PrimaryKind   string   `json:"primary_kind,omitempty"`
	Resolution    string   `json:"resolution,omitempty"`
	Reaction      string   `json:"reaction,omitempty"`
	ContextWindow []string `json:"context_window,omitempty"`
	Signals       []string `json:"signals,omitempty"`
}

type NotableInsightKind string

const (
	NotableInsightSurprise           NotableInsightKind = "surprise"
	NotableInsightMisunderstanding   NotableInsightKind = "misunderstanding"
	NotableInsightGotcha             NotableInsightKind = "gotcha"
	NotableInsightEpisodic           NotableInsightKind = "episodic"
	NotableInsightProceduralLearning NotableInsightKind = "procedural_learning"
)

// NotableInsight is a typed notable-memory window for agent/human history views.
type NotableInsight struct {
	Kind          NotableInsightKind `json:"kind"`
	Headline      string             `json:"headline"`
	WhyItMatters  string             `json:"why_it_matters,omitempty"`
	StartFrame    int                `json:"start_frame"`
	EndFrame      int                `json:"end_frame"`
	Resolution    string             `json:"resolution,omitempty"`
	Reaction      string             `json:"reaction,omitempty"`
	ContextWindow []string           `json:"context_window,omitempty"`
	Signals       []string           `json:"signals,omitempty"`
}

type frameSpan struct {
	start int
	end   int
}

type scoredInsight struct {
	DecisionInsight
	score int
}

// BuildDecisionInsights converts anchored derivations into compact decision-support insights.
func BuildDecisionInsights(derivations []companion.AnchoredMemoryDerivation, limit int) []DecisionInsight {
	if len(derivations) == 0 {
		return nil
	}
	collected := make([]scoredInsight, 0, len(derivations)*2)
	for _, derivation := range derivations {
		for _, candidate := range derivation.Candidates {
			insight, ok := insightFromCandidate(derivation, candidate)
			if !ok {
				continue
			}
			collected = append(collected, scoredInsight{
				DecisionInsight: insight,
				score:           scoreInsight(insight, candidate.Scope),
			})
		}
	}
	if len(collected) == 0 {
		return nil
	}
	merged := mergeDecisionInsights(collected)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].score != merged[j].score {
			return merged[i].score > merged[j].score
		}
		if insightKindPriority(merged[i].Kind) != insightKindPriority(merged[j].Kind) {
			return insightKindPriority(merged[i].Kind) > insightKindPriority(merged[j].Kind)
		}
		return merged[i].Summary < merged[j].Summary
	})
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	out := make([]DecisionInsight, 0, len(merged))
	for _, item := range merged {
		out = append(out, item.DecisionInsight)
	}
	return out
}

// FinalizeDecisionInsights merges, ranks, and caps a mixed insight set.
func FinalizeDecisionInsights(in []DecisionInsight, limit int) []DecisionInsight {
	merged := mergeDecisionInsights(asScoredInsights(in))
	if len(merged) == 0 {
		return nil
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].score != merged[j].score {
			return merged[i].score > merged[j].score
		}
		if insightKindPriority(merged[i].Kind) != insightKindPriority(merged[j].Kind) {
			return insightKindPriority(merged[i].Kind) > insightKindPriority(merged[j].Kind)
		}
		return merged[i].Summary < merged[j].Summary
	})
	if limit > 0 && len(merged) > limit {
		merged = diversifyInsights(merged, limit)
	}
	return flattenScoredInsights(merged)
}

// BuildInsightBrief groups insights into a compact decision-oriented summary.
func BuildInsightBrief(in []DecisionInsight) *InsightBrief {
	if len(in) == 0 {
		return nil
	}
	brief := &InsightBrief{}
	for _, item := range in {
		switch item.Kind {
		case InsightKindGoal:
			brief.CurrentGoals = appendUniqueString(brief.CurrentGoals, item.Summary, 3)
		case InsightKindDirection:
			if item.Status == InsightStatusAccepted {
				brief.AcceptedItems = appendUniqueString(brief.AcceptedItems, item.Summary, 4)
			} else if shouldSurfaceActiveDirection(item) {
				brief.ActiveDirections = appendUniqueString(brief.ActiveDirections, item.Summary, 4)
			}
		case InsightKindDecision, InsightKindPreference:
			brief.AcceptedItems = appendUniqueString(brief.AcceptedItems, item.Summary, 4)
		case InsightKindQuestion:
			brief.OpenQuestions = appendUniqueString(brief.OpenQuestions, item.Summary, 4)
		case InsightKindRisk:
			brief.Risks = appendUniqueString(brief.Risks, item.Summary, 3)
		case InsightKindContext:
			brief.Context = appendUniqueString(brief.Context, item.Summary, 3)
		}
	}
	brief.LatestLearnings = latestAcceptedLearnings(in, 3)
	brief.Overview = buildInsightOverview(brief)
	if brief.Overview == "" &&
		len(brief.CurrentGoals) == 0 &&
		len(brief.ActiveDirections) == 0 &&
		len(brief.AcceptedItems) == 0 &&
		len(brief.LatestLearnings) == 0 &&
		len(brief.OpenQuestions) == 0 &&
		len(brief.Risks) == 0 &&
		len(brief.Context) == 0 {
		return nil
	}
	return brief
}

// BuildInsightTimeline returns notable windows around insight-bearing or unstable frames.
func BuildInsightTimeline(derivations []companion.AnchoredMemoryDerivation, insights []DecisionInsight, limit int) []InsightTimelineEntry {
	if len(derivations) == 0 {
		return nil
	}
	spans := buildInterestingSpans(derivations, insights)
	if len(spans) == 0 {
		return nil
	}

	out := make([]InsightTimelineEntry, 0, len(spans))
	for _, item := range spans {
		start := maxInt(0, item.start-1)
		end := minInt(len(derivations)-1, item.end+1)
		signals := signalsForWindow(insights, item.start, item.end)
		headline := ""
		primaryKind := ""
		if len(signals) > 0 {
			headline = signals[0].Summary
			primaryKind = string(signals[0].Kind)
		} else {
			headline = deterministicFrameSynopsisLine(derivations[item.start])
		}
		contextWindow := make([]string, 0, end-start+1)
		for idx := start; idx <= end; idx++ {
			contextWindow = append(contextWindow, timelineContextLine(derivations[idx]))
		}
		signalSummaries := make([]string, 0, len(signals))
		for _, signal := range signals {
			signalSummaries = appendUniqueString(signalSummaries, signal.Summary, 4)
		}
		out = append(out, InsightTimelineEntry{
			StartFrame:    item.start,
			EndFrame:      item.end,
			Headline:      headline,
			PrimaryKind:   primaryKind,
			Resolution:    string(derivations[item.end].Resolution),
			Reaction:      string(derivations[item.end].Reaction.Outcome),
			ContextWindow: contextWindow,
			Signals:       signalSummaries,
		})
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// BuildNotableInsights labels larger windows with typed notable-memory categories.
func BuildNotableInsights(derivations []companion.AnchoredMemoryDerivation, insights []DecisionInsight, limit int) []NotableInsight {
	if len(derivations) == 0 {
		return nil
	}
	spans := buildInterestingSpans(derivations, insights)
	out := make([]NotableInsight, 0, len(spans))
	for _, span := range spans {
		start := maxInt(0, span.start-2)
		end := minInt(len(derivations)-1, span.end+2)
		signals := signalsForWindow(insights, span.start, span.end)
		kind, why := classifyNotableWindow(derivations[start:end+1], signals)
		if kind == "" {
			continue
		}
		headline := ""
		if len(signals) > 0 {
			headline = signals[0].Summary
		}
		if headline == "" {
			headline = timelineContextLine(derivations[span.start])
		}
		contextWindow := make([]string, 0, end-start+1)
		for idx := start; idx <= end; idx++ {
			contextWindow = append(contextWindow, timelineContextLine(derivations[idx]))
		}
		signalSummaries := make([]string, 0, len(signals))
		for _, signal := range signals {
			signalSummaries = appendUniqueString(signalSummaries, signal.Summary, 5)
		}
		out = append(out, NotableInsight{
			Kind:          kind,
			Headline:      headline,
			WhyItMatters:  why,
			StartFrame:    span.start,
			EndFrame:      span.end,
			Resolution:    string(derivations[span.end].Resolution),
			Reaction:      string(derivations[span.end].Reaction.Outcome),
			ContextWindow: contextWindow,
			Signals:       signalSummaries,
		})
	}
	out = append(out, globalNotableInsights(derivations, insights, limit)...)
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		if notablePriority(out[i].Kind) != notablePriority(out[j].Kind) {
			return notablePriority(out[i].Kind) > notablePriority(out[j].Kind)
		}
		return out[i].StartFrame < out[j].StartFrame
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// InsightsFromConsensusClaims turns grouped sidecar consensus into decision-support signals.
func InsightsFromConsensusClaims(claims []ConsensusClaim, limit int) []DecisionInsight {
	if len(claims) == 0 {
		return nil
	}
	out := make([]scoredInsight, 0, len(claims))
	for _, claim := range claims {
		if claim.SupportCount <= 0 {
			continue
		}
		confidence := 0.55 + (0.08 * float64(claim.SupportCount))
		if claim.MainlineEvidenceScore > 0 {
			confidence += 0.2 * claim.MainlineEvidenceScore
		}
		if confidence > 0.95 {
			confidence = 0.95
		}
		out = append(out, scoredInsight{
			DecisionInsight: DecisionInsight{
				Kind:        InsightKindDirection,
				Summary:     truncateInline(strings.TrimSpace(claim.Text), 200),
				Status:      InsightStatusSupported,
				Confidence:  confidence,
				SourceBasis: "sidecar_consensus",
				Tags:        []string{"consensus", "sidecar"},
			},
			score: 200 + (claim.SupportCount * 15),
		})
	}
	if len(out) == 0 {
		return nil
	}
	merged := mergeDecisionInsights(out)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].score != merged[j].score {
			return merged[i].score > merged[j].score
		}
		return merged[i].Summary < merged[j].Summary
	})
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	final := make([]DecisionInsight, 0, len(merged))
	for _, item := range merged {
		final = append(final, item.DecisionInsight)
	}
	return final
}

func insightFromCandidate(derivation companion.AnchoredMemoryDerivation, candidate companion.AnchoredMemoryCandidate) (DecisionInsight, bool) {
	kind, ok := insightKindFromCandidate(candidate)
	if !ok {
		return DecisionInsight{}, false
	}
	summary := compactInsightSummary(kind, summarizeInsightText(candidate.Text))
	if summary == "" {
		return DecisionInsight{}, false
	}

	status := insightStatusFromDerivation(derivation, kind)
	tags := []string{strings.TrimSpace(candidate.Type)}
	if source := strings.TrimSpace(candidate.Source); source != "" {
		tags = append(tags, source)
	}
	return DecisionInsight{
		Kind:                 kind,
		Summary:              summary,
		Status:               status,
		Confidence:           clampConfidence(candidate.Confidence),
		SourceBasis:          normalizeSourceBasis(candidate.Source),
		EvidenceFrameIndices: []int{derivation.FrameIndex},
		Tags:                 normalizeTagList(tags),
	}, true
}

func insightKindFromCandidate(candidate companion.AnchoredMemoryCandidate) (InsightKind, bool) {
	switch strings.TrimSpace(candidate.Type) {
	case companion.EntryTypeDecision:
		return InsightKindDecision, true
	case companion.EntryTypePreference:
		return InsightKindPreference, true
	case companion.EntryTypeGoal:
		return InsightKindGoal, true
	case companion.EntryTypePolicy:
		return InsightKindDirection, true
	case companion.EntryTypeTechnicalContext:
		if candidate.Source == "assistant_guidance" {
			return InsightKindDirection, true
		}
		return InsightKindContext, true
	case "follow_up_needed":
		if strings.TrimSpace(candidate.Source) == "tool_receipt" {
			return InsightKindRisk, true
		}
		if isActionRequestCandidate(candidate) {
			return InsightKindDirection, true
		}
		return InsightKindQuestion, true
	case "user_correction":
		if isActionRequestCandidate(candidate) {
			return InsightKindDirection, true
		}
		return InsightKindQuestion, true
	case companion.EntryTypeOpenQuestion:
		return InsightKindQuestion, true
	case "user_pain_point":
		return InsightKindRisk, true
	default:
		return "", false
	}
}

func insightStatusFromDerivation(derivation companion.AnchoredMemoryDerivation, kind InsightKind) InsightStatus {
	switch derivation.Resolution {
	case companion.InteractionResolutionResolved:
		if derivation.Reaction.Outcome == companion.ReactionOutcomeAccepted {
			return InsightStatusAccepted
		}
		return InsightStatusActive
	case companion.InteractionResolutionCorrected:
		if kind == InsightKindQuestion || kind == InsightKindRisk {
			return InsightStatusOpen
		}
		return InsightStatusActive
	case companion.InteractionResolutionUnresolved:
		if kind == InsightKindDirection || kind == InsightKindGoal {
			return InsightStatusActive
		}
		return InsightStatusOpen
	default:
		return InsightStatusActive
	}
}

func scoreInsight(insight DecisionInsight, scope companion.AnchoredMemoryCandidateScope) int {
	score := 0
	score += insightKindPriority(insight.Kind) * 40
	score += insightStatusPriority(insight.Status) * 25
	score += candidateScopeRank(scope) * 20
	score += int(insight.Confidence * 10)
	return score
}

func insightKindPriority(kind InsightKind) int {
	switch kind {
	case InsightKindDecision:
		return 6
	case InsightKindPreference:
		return 5
	case InsightKindGoal:
		return 4
	case InsightKindDirection:
		return 4
	case InsightKindQuestion:
		return 3
	case InsightKindRisk:
		return 3
	case InsightKindContext:
		return 2
	default:
		return 1
	}
}

func insightStatusPriority(status InsightStatus) int {
	switch status {
	case InsightStatusAccepted, InsightStatusSupported:
		return 4
	case InsightStatusOpen:
		return 3
	case InsightStatusActive:
		return 2
	default:
		return 1
	}
}

func mergeDecisionInsights(in []scoredInsight) []scoredInsight {
	if len(in) == 0 {
		return nil
	}
	out := make([]scoredInsight, 0, len(in))
	index := make(map[string]int, len(in))
	for _, item := range in {
		key := string(item.Kind) + "|" + strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(item.Summary)), " "))
		if idx, ok := index[key]; ok {
			merged := out[idx]
			if item.score > merged.score {
				merged.score = item.score
			}
			if item.Confidence > merged.Confidence {
				merged.Confidence = item.Confidence
			}
			if insightStatusPriority(item.Status) > insightStatusPriority(merged.Status) {
				merged.Status = item.Status
			}
			if sourceBasisRank(item.SourceBasis) > sourceBasisRank(merged.SourceBasis) {
				merged.SourceBasis = item.SourceBasis
			}
			merged.EvidenceFrameIndices = normalizeEvidenceFrames(append(merged.EvidenceFrameIndices, item.EvidenceFrameIndices...), 1000000)
			merged.Tags = normalizeTagList(append(merged.Tags, item.Tags...))
			out[idx] = merged
			continue
		}
		index[key] = len(out)
		item.Confidence = clampConfidence(item.Confidence)
		item.Tags = normalizeTagList(item.Tags)
		item.EvidenceFrameIndices = normalizeEvidenceFrames(item.EvidenceFrameIndices, 1000000)
		out = append(out, item)
	}
	return out
}

func clampConfidence(v float64) float64 {
	if v <= 0 {
		return 0.5
	}
	if v > 1 {
		return 1
	}
	return v
}

func InsightFromObjective(objective *SessionObjective) []DecisionInsight {
	if objective == nil {
		return nil
	}
	summary := summarizeInsightText(firstNonEmpty(strings.TrimSpace(objective.Objective), strings.TrimSpace(objective.Label)))
	if summary == "" || len(summary) < 8 || !containsAlphaNum(summary) {
		return nil
	}
	return []DecisionInsight{{
		Kind:        InsightKindGoal,
		Summary:     compactInsightSummary(InsightKindGoal, summary),
		Status:      InsightStatusActive,
		Confidence:  clampConfidence(objective.Confidence),
		SourceBasis: "objective",
		Tags:        []string{"objective"},
	}}
}

func asScoredInsights(in []DecisionInsight) []scoredInsight {
	if len(in) == 0 {
		return nil
	}
	out := make([]scoredInsight, 0, len(in))
	for _, item := range in {
		out = append(out, scoredInsight{
			DecisionInsight: item,
			score:           (insightKindPriority(item.Kind) * 40) + (insightStatusPriority(item.Status) * 25) + int(item.Confidence*10),
		})
	}
	return out
}

func flattenScoredInsights(in []scoredInsight) []DecisionInsight {
	if len(in) == 0 {
		return nil
	}
	out := make([]DecisionInsight, 0, len(in))
	for _, item := range in {
		out = append(out, item.DecisionInsight)
	}
	return out
}

func diversifyInsights(in []scoredInsight, limit int) []scoredInsight {
	if limit <= 0 || len(in) <= limit {
		return in
	}
	selected := make([]scoredInsight, 0, limit)
	seen := make(map[string]struct{}, limit)
	tryAdd := func(item scoredInsight) {
		if len(selected) >= limit {
			return
		}
		key := string(item.Kind) + "|" + strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(item.Summary)), " "))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		selected = append(selected, item)
	}
	for _, kind := range []InsightKind{InsightKindGoal, InsightKindDirection, InsightKindRisk, InsightKindQuestion} {
		for _, item := range in {
			if item.Kind == kind {
				tryAdd(item)
				break
			}
		}
	}
	for _, item := range in {
		tryAdd(item)
		if len(selected) >= limit {
			break
		}
	}
	return selected
}

var (
	environmentContextPattern = regexp.MustCompile(`(?s)<environment_context>.*?</environment_context>`)
	turnAbortedPattern        = regexp.MustCompile(`(?s)<turn_aborted>.*?</turn_aborted>`)
	imageTagPattern           = regexp.MustCompile(`(?is)</?image\b[^>]*>`)
	imageRefPattern           = regexp.MustCompile(`(?i)\[image #[0-9]+\]`)
)

func summarizeInsightText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = environmentContextPattern.ReplaceAllString(text, " ")
	text = turnAbortedPattern.ReplaceAllString(text, " ")
	text = imageTagPattern.ReplaceAllString(text, " ")
	text = imageRefPattern.ReplaceAllString(text, " ")
	if idx := strings.Index(text, "```"); idx >= 0 {
		text = text[:idx]
	}
	lines := strings.Split(text, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
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
	summary = summarizeToolReceiptText(summary)
	return truncateInline(compactSummaryText(strings.TrimSpace(summary), 200), 200)
}

func summarizeToolReceiptText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, prefix := range []string{"tool_result:", "tool_call:"} {
		if strings.HasPrefix(strings.ToLower(text), prefix) {
			text = strings.TrimSpace(text[len(prefix):])
		}
	}
	for _, prefix := range []string{"exec_command:", "apply_patch:", "write_stdin:"} {
		if strings.HasPrefix(strings.ToLower(text), prefix) {
			text = strings.TrimSpace(text[len(prefix):])
		}
	}
	if normalized := normalizeToolReceiptIssue(text); normalized != "" {
		return normalized
	}
	return text
}

func normalizeToolReceiptIssue(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	tool, rest := splitToolReceiptTool(text)
	lower := strings.ToLower(rest)
	switch {
	case strings.Contains(lower, "sandbox(denied") || strings.Contains(lower, "sandbox denied") || strings.Contains(lower, "sandboxdenied"):
		return formatToolIssue(tool, "sandbox denied")
	case strings.Contains(lower, "permission denied"):
		return formatToolIssue(tool, "permission denied")
	case strings.Contains(lower, "timed out") || strings.Contains(lower, "timeout"):
		return formatToolIssue(tool, "timed out")
	}
	if idx := strings.Index(lower, "process exited with code "); idx >= 0 {
		code := strings.TrimSpace(rest[idx+len("process exited with code "):])
		if end := strings.IndexAny(code, " ,.;)]}"); end >= 0 {
			code = code[:end]
		}
		if code != "" {
			return formatToolIssue(tool, "exited with code "+code)
		}
	}
	if strings.Contains(lower, "failed") {
		return formatToolIssue(tool, "failed")
	}
	return ""
}

func splitToolReceiptTool(text string) (string, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	parts := strings.SplitN(text, ":", 2)
	if len(parts) < 2 {
		return trimToolIssueSuffix(text), text
	}
	tool := trimToolIssueSuffix(parts[0])
	rest := strings.TrimSpace(parts[1])
	if next := strings.SplitN(rest, ":", 2); len(next) == 2 {
		prefix := strings.TrimSpace(next[0])
		lowerPrefix := strings.ToLower(prefix)
		if strings.EqualFold(prefix, tool) || strings.HasSuffix(lowerPrefix, " failed") {
			rest = strings.TrimSpace(next[1])
		}
	}
	return tool, rest
}

func trimToolIssueSuffix(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, ":")
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	if strings.HasSuffix(lower, " failed") {
		text = strings.TrimSpace(text[:len(text)-len("failed")])
	}
	return strings.TrimSpace(text)
}

func formatToolIssue(tool, issue string) string {
	issue = strings.TrimSpace(issue)
	if issue == "" {
		return ""
	}
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return issue
	}
	if strings.HasSuffix(strings.ToLower(tool), strings.ToLower(issue)) {
		return tool
	}
	return tool + " " + issue
}

func isActionRequestCandidate(candidate companion.AnchoredMemoryCandidate) bool {
	source := strings.TrimSpace(candidate.Source)
	if source != "user" && source != "followup_user" {
		return false
	}
	text := summarizeInsightText(candidate.Text)
	if text == "" {
		return false
	}
	return !strings.Contains(text, "?")
}

func containsAlphaNum(text string) bool {
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return true
		}
	}
	return false
}

func signalsForWindow(insights []DecisionInsight, startFrame, endFrame int) []DecisionInsight {
	if len(insights) == 0 {
		return nil
	}
	out := make([]DecisionInsight, 0, len(insights))
	for _, item := range insights {
		for _, idx := range item.EvidenceFrameIndices {
			if idx < startFrame || idx > endFrame {
				continue
			}
			out = append(out, item)
			break
		}
	}
	return out
}

func buildInterestingSpans(derivations []companion.AnchoredMemoryDerivation, insights []DecisionInsight) []frameSpan {
	interesting := make(map[int]struct{})
	for _, item := range insights {
		for _, frameIdx := range item.EvidenceFrameIndices {
			interesting[frameIdx] = struct{}{}
		}
	}
	for _, derivation := range derivations {
		if derivation.Resolution == companion.InteractionResolutionCorrected || derivation.Resolution == companion.InteractionResolutionUnresolved {
			interesting[derivation.FrameIndex] = struct{}{}
		}
	}
	if len(interesting) == 0 {
		return nil
	}

	indices := make([]int, 0, len(interesting))
	for idx := range interesting {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	spans := make([]frameSpan, 0, len(indices))
	cur := frameSpan{start: indices[0], end: indices[0]}
	for _, idx := range indices[1:] {
		if idx <= cur.end+1 {
			cur.end = idx
			continue
		}
		spans = append(spans, cur)
		cur = frameSpan{start: idx, end: idx}
	}
	spans = append(spans, cur)
	return spans
}

func classifyNotableWindow(window []companion.AnchoredMemoryDerivation, signals []DecisionInsight) (NotableInsightKind, string) {
	for _, item := range window {
		if item.Resolution == companion.InteractionResolutionCorrected {
			return NotableInsightMisunderstanding, "The user corrected the course or exposed that the previous framing was wrong."
		}
		for _, candidate := range item.Candidates {
			if strings.TrimSpace(candidate.Type) == "user_correction" {
				return NotableInsightMisunderstanding, "The user explicitly corrected the prior understanding."
			}
		}
	}
	for _, item := range window {
		if item.Reaction.Outcome == companion.ReactionOutcomeFrustrated || item.Reaction.Outcome == companion.ReactionOutcomeConfused {
			return NotableInsightGotcha, "The window captures friction, confusion, or a failure mode worth remembering."
		}
		for _, candidate := range item.Candidates {
			if strings.TrimSpace(candidate.Type) == "user_pain_point" {
				return NotableInsightGotcha, "The window captures a user pain point or operational gotcha."
			}
		}
	}
	for _, signal := range signals {
		if signal.SourceBasis == "sidecar_consensus" || hasInsightTag(signal, "consensus") {
			return NotableInsightSurprise, "Independent supporting evidence surfaced a notable direction beyond the immediate local turn."
		}
	}
	if hasAssistantGuidanceSurprise(signals) {
		return NotableInsightSurprise, "The assistant surfaced a materially different structural takeaway from the user's active ask."
	}
	for _, signal := range signals {
		if (signal.Status == InsightStatusAccepted || signal.Status == InsightStatusSupported) && isLearningSignal(signal) {
			return NotableInsightProceduralLearning, "The window records a direction or rule that looks reusable in later work."
		}
	}
	if isEpisodicPivotWindow(window, signals) {
		return NotableInsightEpisodic, "This window marks a concrete task turn or turning point in the session history."
	}
	return "", ""
}

func globalNotableInsights(derivations []companion.AnchoredMemoryDerivation, insights []DecisionInsight, limit int) []NotableInsight {
	if len(insights) == 0 || len(derivations) == 0 {
		return nil
	}
	lastFrame := derivations[len(derivations)-1].FrameIndex
	contextLine := timelineContextLine(derivations[len(derivations)-1])
	out := make([]NotableInsight, 0, 4)
	for _, signal := range insights {
		if len(signal.EvidenceFrameIndices) > 0 {
			continue
		}
		kind, why := notableKindFromSignal(signal)
		if kind == "" {
			continue
		}
		out = append(out, NotableInsight{
			Kind:         kind,
			Headline:     signal.Summary,
			WhyItMatters: why,
			StartFrame:   lastFrame,
			EndFrame:     lastFrame,
			Resolution:   string(derivations[len(derivations)-1].Resolution),
			Reaction:     string(derivations[len(derivations)-1].Reaction.Outcome),
			ContextWindow: func() []string {
				if contextLine == "" {
					return nil
				}
				return []string{contextLine}
			}(),
			Signals: []string{signal.Summary},
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func notableKindFromSignal(signal DecisionInsight) (NotableInsightKind, string) {
	if signal.SourceBasis == "sidecar_consensus" || hasInsightTag(signal, "consensus") {
		return NotableInsightSurprise, "Independent supporting evidence surfaced a notable direction beyond the immediate local turn."
	}
	if (signal.Status == InsightStatusAccepted || signal.Status == InsightStatusSupported) && isLearningSignal(signal) {
		return NotableInsightProceduralLearning, "This signal looks like a reusable direction or learned way of working."
	}
	return "", ""
}

func hasAssistantGuidanceSurprise(signals []DecisionInsight) bool {
	if len(signals) < 2 {
		return false
	}
	var userSignals []DecisionInsight
	var guidanceSignals []DecisionInsight
	for _, signal := range signals {
		if signal.Kind != InsightKindDirection && signal.Kind != InsightKindGoal {
			continue
		}
		switch signal.SourceBasis {
		case "user":
			userSignals = append(userSignals, signal)
		case "user_approved":
			if hasInsightTag(signal, "assistant-guidance") || hasInsightTag(signal, "technical-context") {
				guidanceSignals = append(guidanceSignals, signal)
			}
		}
	}
	if len(userSignals) == 0 || len(guidanceSignals) == 0 {
		return false
	}
	for _, userSignal := range userSignals {
		for _, guidanceSignal := range guidanceSignals {
			if materiallyDifferentInsight(userSignal.Summary, guidanceSignal.Summary) {
				return true
			}
		}
	}
	return false
}

func materiallyDifferentInsight(a, b string) bool {
	aTokens := normalizedInsightTokens(a)
	bTokens := normalizedInsightTokens(b)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return false
	}
	if len(bTokens) < 5 {
		return false
	}
	overlap := 0
	for token := range aTokens {
		if _, ok := bTokens[token]; ok {
			overlap++
		}
	}
	union := len(aTokens) + len(bTokens) - overlap
	if union <= 0 {
		return false
	}
	jaccard := float64(overlap) / float64(union)
	return jaccard <= 0.22
}

func normalizedInsightTokens(text string) map[string]struct{} {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	replacer := strings.NewReplacer(".", " ", ",", " ", ":", " ", ";", " ", "!", " ", "?", " ", "(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ", "\"", " ", "'", " ", "`", " ")
	text = replacer.Replace(text)
	parts := strings.Fields(text)
	out := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) < 4 {
			continue
		}
		out[part] = struct{}{}
	}
	return out
}

func isEpisodicPivotWindow(window []companion.AnchoredMemoryDerivation, signals []DecisionInsight) bool {
	if len(window) == 0 || len(signals) == 0 {
		return false
	}
	hasLiveState := false
	for _, signal := range signals {
		if signal.Status != InsightStatusActive && signal.Status != InsightStatusOpen {
			continue
		}
		switch signal.Kind {
		case InsightKindDirection, InsightKindQuestion, InsightKindGoal, InsightKindRisk:
		default:
			continue
		}
		switch signal.SourceBasis {
		case "user", "user_approved", "objective":
			hasLiveState = true
		default:
			continue
		}
	}
	if !hasLiveState {
		return false
	}
	for _, item := range window {
		switch item.Resolution {
		case companion.InteractionResolutionUnresolved, companion.InteractionResolutionContinues:
			return true
		}
	}
	return false
}

func hasInsightTag(item DecisionInsight, target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	for _, tag := range item.Tags {
		if strings.TrimSpace(strings.ToLower(tag)) == target {
			return true
		}
	}
	return false
}

func notablePriority(kind NotableInsightKind) int {
	switch kind {
	case NotableInsightMisunderstanding:
		return 5
	case NotableInsightGotcha:
		return 4
	case NotableInsightSurprise:
		return 3
	case NotableInsightProceduralLearning:
		return 2
	case NotableInsightEpisodic:
		return 1
	default:
		return 0
	}
}

func latestAcceptedLearnings(in []DecisionInsight, limit int) []string {
	type scoredLearning struct {
		summary string
		score   int
	}
	learners := make([]scoredLearning, 0, len(in))
	for _, item := range in {
		if !(item.Status == InsightStatusAccepted || item.Status == InsightStatusSupported) {
			continue
		}
		if !isLearningSignal(item) {
			continue
		}
		score := 0
		score += insightStatusPriority(item.Status) * 20
		score += insightKindPriority(item.Kind) * 10
		score += maxEvidenceFrame(item) * 2
		score += int(item.Confidence * 10)
		learners = append(learners, scoredLearning{summary: item.Summary, score: score})
	}
	sort.SliceStable(learners, func(i, j int) bool {
		if learners[i].score != learners[j].score {
			return learners[i].score > learners[j].score
		}
		return learners[i].summary < learners[j].summary
	})
	out := make([]string, 0, minInt(limit, len(learners)))
	for _, item := range learners {
		out = appendUniqueString(out, item.summary, limit)
	}
	return out
}

func isLearningSignal(signal DecisionInsight) bool {
	switch signal.Kind {
	case InsightKindDecision, InsightKindPreference:
		return true
	case InsightKindDirection:
		if signal.SourceBasis == "sidecar_consensus" || hasInsightTag(signal, "consensus") {
			return true
		}
		if hasInsightTag(signal, companion.EntryTypePolicy) || hasInsightTag(signal, companion.EntryTypeDecision) || hasInsightTag(signal, companion.EntryTypePreference) {
			return true
		}
		if signal.SourceBasis == "assistant_guidance" && (signal.Status == InsightStatusAccepted || signal.Status == InsightStatusSupported) && looksReusableLearningText(signal.Summary) {
			return true
		}
		return false
	default:
		return false
	}
}

func maxEvidenceFrame(signal DecisionInsight) int {
	if len(signal.EvidenceFrameIndices) == 0 {
		return 0
	}
	max := signal.EvidenceFrameIndices[0]
	for _, idx := range signal.EvidenceFrameIndices[1:] {
		if idx > max {
			max = idx
		}
	}
	return max
}

func appendUniqueString(in []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return in
	}
	for _, item := range in {
		if strings.EqualFold(item, value) {
			return in
		}
	}
	if limit > 0 && len(in) >= limit {
		return in
	}
	return append(in, value)
}

func buildInsightOverview(brief *InsightBrief) string {
	if brief == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if len(brief.CurrentGoals) > 0 {
		parts = append(parts, "Goal: "+brief.CurrentGoals[0])
	}
	if len(brief.ActiveDirections) > 0 {
		parts = append(parts, "Direction: "+brief.ActiveDirections[0])
	}
	if len(brief.OpenQuestions) > 0 {
		parts = append(parts, "Open: "+brief.OpenQuestions[0])
	} else if len(brief.Risks) > 0 {
		parts = append(parts, "Risk: "+brief.Risks[0])
	}
	return truncateInline(strings.Join(parts, " | "), 220)
}

func shouldSurfaceActiveDirection(item DecisionInsight) bool {
	if item.Kind != InsightKindDirection {
		return false
	}
	if hasInsightTag(item, "assistant-guidance") {
		return false
	}
	return !looksOperationalStatusUpdate(item.Summary)
}

func compactInsightSummary(kind InsightKind, summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	switch kind {
	case InsightKindGoal, InsightKindDirection, InsightKindQuestion:
		return compactSummaryText(summary, 160)
	default:
		return summary
	}
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
	if strings.HasPrefix(strings.ToLower(text), "in ") {
		if idx := strings.Index(text, ","); idx > 0 && idx <= 160 {
			lead := strings.TrimSpace(text[:idx])
			if containsPathLikeToken(lead) {
				suffix := strings.TrimSpace(text[idx+1:])
				if suffix != "" {
					text = suffix
				}
			}
		}
	}
	if idx := strings.Index(text, ","); idx > 0 && idx <= 180 {
		lead := strings.TrimSpace(text[:idx])
		if containsPathLikeToken(lead) {
			suffix := strings.TrimSpace(text[idx+1:])
			if suffix != "" {
				text = suffix
			}
		}
	}
	if idx := strings.Index(text, ":"); idx > 0 && idx <= 180 {
		prefix := strings.TrimSpace(text[:idx])
		suffix := strings.TrimSpace(text[idx+1:])
		if suffix != "" && len(strings.Fields(prefix)) >= 5 {
			text = prefix
		}
	}
	text = trimLeadingConnector(text)
	return truncateInline(strings.TrimSpace(text), max)
}

func containsPathLikeToken(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	return strings.ContainsAny(text, "/\\`")
}

func looksReusableLearningText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if looksOperationalStatusUpdate(text) {
		return false
	}
	if containsPathLikeToken(text) {
		return false
	}
	wordCount := len(strings.Fields(text))
	return wordCount >= 4 && wordCount <= 18
}

func looksOperationalStatusUpdate(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, prefix := range []string{
		"committed ",
		"committed as ",
		"updated ",
		"added ",
		"implemented ",
		"pushed ",
		"merged ",
		"i updated ",
		"i added ",
		"i implemented ",
		"i kept going ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func timelineContextLine(derivation companion.AnchoredMemoryDerivation) string {
	text := ""
	for _, candidate := range derivation.Candidates {
		if strings.TrimSpace(candidate.Type) == "tool_output_digest" {
			continue
		}
		text = summarizeInsightText(candidate.Text)
		if text != "" {
			break
		}
	}
	if text == "" {
		text = summarizeInteractionSummaryForTimeline(derivation.InteractionSummary)
	}
	if text == "" {
		return ""
	}
	prefix := "Continued"
	switch derivation.Resolution {
	case companion.InteractionResolutionResolved:
		prefix = "Resolved"
	case companion.InteractionResolutionCorrected:
		prefix = "Corrected"
	case companion.InteractionResolutionUnresolved:
		prefix = "Open"
	}
	return truncateInline(prefix+": "+text, 200)
}

func summarizeInteractionSummaryForTimeline(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	parts := strings.Split(summary, " | ")
	candidates := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch {
		case strings.HasPrefix(part, "followup:"):
			text := summarizeInsightText(strings.TrimSpace(strings.TrimPrefix(part, "followup:")))
			if text != "" {
				candidates = append(candidates, text)
			}
		case strings.HasPrefix(part, "user:"):
			text := summarizeInsightText(strings.TrimSpace(strings.TrimPrefix(part, "user:")))
			if text != "" {
				candidates = append(candidates, text)
			}
		case strings.HasPrefix(part, "assistant:"):
			text := summarizeInsightText(strings.TrimSpace(strings.TrimPrefix(part, "assistant:")))
			if text != "" {
				candidates = append(candidates, text)
			}
		default:
			text := summarizeInsightText(part)
			if text != "" {
				candidates = append(candidates, text)
			}
		}
	}
	if len(candidates) == 0 {
		return summarizeInsightText(summary)
	}
	return candidates[0]
}
