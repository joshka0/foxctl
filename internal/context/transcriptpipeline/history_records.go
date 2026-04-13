package transcriptpipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	historypkg "github.com/jkatigb/agentctl/internal/context/transcriptpipeline/history"
)

type HistoryRecordKind = historypkg.HistoryRecordKind

const (
	HistoryRecordKindInsight = historypkg.HistoryRecordKindInsight
	HistoryRecordKindNotable = historypkg.HistoryRecordKindNotable
	HistoryRecordKindAnswer  = historypkg.HistoryRecordKindAnswer
)

type HistoryRecord = historypkg.HistoryRecord

type HistoryRecordContext = historypkg.HistoryRecordContext

type HistoryRecordEmbedFunc = historypkg.HistoryRecordEmbedFunc

type PersistedHistoryRecord = historypkg.PersistedHistoryRecord

func BuildHistoryRecords(profile *historypkg.HistoryProfile, ctx HistoryRecordContext, insights []DecisionInsight, notable []NotableInsight, answers []historypkg.HistoryAnswer) []HistoryRecord {
	out := make([]HistoryRecord, 0, len(insights)+len(notable)+len(answers))
	out = append(out, historyRecordsFromInsights(ctx, insights)...)
	out = append(out, historyRecordsFromNotables(ctx, notable)...)
	out = append(out, historyRecordsFromAnswers(profile, ctx, answers)...)
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		if historyRecordPriority(out[i]) != historyRecordPriority(out[j]) {
			return historyRecordPriority(out[i]) > historyRecordPriority(out[j])
		}
		if historyRecordFrameStart(out[i]) != historyRecordFrameStart(out[j]) {
			return historyRecordFrameStart(out[i]) < historyRecordFrameStart(out[j])
		}
		return out[i].RecordID < out[j].RecordID
	})
	return out
}

func historyRecordMemoryType(kind HistoryRecordKind) (string, bool) {
	switch kind {
	case HistoryRecordKindInsight:
		return "history_insight", true
	case HistoryRecordKindNotable:
		return "history_notable", true
	case HistoryRecordKindAnswer:
		return "history_answer", true
	default:
		return "", false
	}
}

func historyRecordMemoryTypes() []string {
	return []string{"history_insight", "history_notable", "history_answer"}
}

func historyRecordTypeSuffix(kind HistoryRecordKind) string {
	switch kind {
	case HistoryRecordKindInsight:
		return "insight"
	case HistoryRecordKindNotable:
		return "notable"
	case HistoryRecordKindAnswer:
		return "answer"
	default:
		return "record"
	}
}

func historyRecordsFromInsights(ctx HistoryRecordContext, insights []DecisionInsight) []HistoryRecord {
	if len(insights) == 0 {
		return nil
	}
	out := make([]HistoryRecord, 0, len(insights))
	for _, item := range insights {
		summary := summarizeInsightText(item.Summary)
		if summary == "" {
			continue
		}
		start, end := frameBoundsFromIndices(item.EvidenceFrameIndices)
		record := newHistoryRecord(ctx, HistoryRecordKindInsight, summary, truncateInline(fmt.Sprintf("%s (%s): %s", titleLikeLabel(string(item.Kind)), strings.ToLower(string(item.Status)), summary), 320))
		record.Confidence = clampConfidence(item.Confidence)
		record.InsightKind = string(item.Kind)
		record.InsightStatus = string(item.Status)
		record.SourceBasis = strings.TrimSpace(item.SourceBasis)
		record.Tags = normalizeTagList(item.Tags)
		record.FrameStart = start
		record.FrameEnd = end
		record.EvidenceRefs = frameEvidenceRefs(item.EvidenceFrameIndices)
		record.NormalizedHash = historyRecordHash(record.Kind, string(item.Kind), summary)
		record.RecordID = string(record.Kind) + ":" + shortRecordHash(record.NormalizedHash)
		out = append(out, record)
	}
	return out
}

func historyRecordsFromNotables(ctx HistoryRecordContext, notable []NotableInsight) []HistoryRecord {
	if len(notable) == 0 {
		return nil
	}
	out := make([]HistoryRecord, 0, len(notable))
	for _, item := range notable {
		headline := summarizeInsightText(item.Headline)
		if headline == "" {
			continue
		}
		record := newHistoryRecord(ctx, HistoryRecordKindNotable, headline, truncateInline(buildNotableRetrievalText(item), 360))
		record.Confidence = notableConfidence(item.Kind)
		record.NotableKind = string(item.Kind)
		record.FrameStart = intPtr(item.StartFrame)
		record.FrameEnd = intPtr(item.EndFrame)
		record.EvidenceRefs = []string{fmt.Sprintf("frames:%d-%d", item.StartFrame, item.EndFrame)}
		record.Tags = notableTags(item)
		record.NormalizedHash = historyRecordHash(record.Kind, string(item.Kind), headline)
		record.RecordID = string(record.Kind) + ":" + shortRecordHash(record.NormalizedHash)
		out = append(out, record)
	}
	return out
}

func historyRecordsFromAnswers(profile *historypkg.HistoryProfile, ctx HistoryRecordContext, answers []historypkg.HistoryAnswer) []HistoryRecord {
	if len(answers) == 0 {
		return nil
	}
	out := make([]HistoryRecord, 0, len(answers))
	for _, item := range answers {
		answer := summarizeInsightText(item.Answer)
		if answer == "" {
			continue
		}
		prompt := historyQuestionPrompt(profile, item.QuestionID)
		retrieval := answer
		if prompt != "" {
			retrieval = truncateInline(fmt.Sprintf("Question: %s\nAnswer: %s", prompt, answer), 360)
		}
		record := newHistoryRecord(ctx, HistoryRecordKindAnswer, answer, retrieval)
		record.Confidence = clampConfidence(item.Confidence)
		record.HistoryQuestionID = item.QuestionID
		record.AnswerLabel = strings.TrimSpace(item.Label)
		record.EvidenceRefs = append([]string(nil), item.Evidence...)
		record.FrameStart, record.FrameEnd = frameBoundsFromEvidence(item.Evidence)
		record.NormalizedHash = historyRecordHash(record.Kind, string(item.QuestionID), answer)
		record.RecordID = string(record.Kind) + ":" + shortRecordHash(record.NormalizedHash)
		out = append(out, record)
	}
	return out
}

func newHistoryRecord(ctx HistoryRecordContext, kind HistoryRecordKind, summary, retrieval string) HistoryRecord {
	return HistoryRecord{
		Kind:            kind,
		ConversationID:  strings.TrimSpace(ctx.ConversationID),
		GroupID:         strings.TrimSpace(ctx.GroupID),
		SessionIDs:      append([]string(nil), ctx.SessionIDs...),
		SourceStartedAt: ctx.SourceStartedAt.UTC(),
		Summary:         summary,
		RetrievalText:   retrieval,
	}
}

func formatOptionalRecordTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func buildNotableRetrievalText(item NotableInsight) string {
	headline := summarizeInsightText(item.Headline)
	if headline == "" {
		headline = "Notable transcript event"
	}
	if why := summarizeInsightText(item.WhyItMatters); why != "" {
		return fmt.Sprintf("%s: %s Why it matters: %s", titleLikeLabel(string(item.Kind)), headline, why)
	}
	return fmt.Sprintf("%s: %s", titleLikeLabel(string(item.Kind)), headline)
}

func notableConfidence(kind NotableInsightKind) float64 {
	switch kind {
	case NotableInsightMisunderstanding:
		return 0.84
	case NotableInsightGotcha:
		return 0.8
	case NotableInsightSurprise:
		return 0.78
	case NotableInsightProceduralLearning:
		return 0.75
	case NotableInsightEpisodic:
		return 0.66
	default:
		return 0.7
	}
}

func notableTags(item NotableInsight) []string {
	tags := []string{string(item.Kind)}
	if item.Resolution != "" {
		tags = append(tags, strings.ToLower(strings.TrimSpace(item.Resolution)))
	}
	if item.Reaction != "" {
		tags = append(tags, strings.ToLower(strings.TrimSpace(item.Reaction)))
	}
	return normalizeTagList(tags)
}

func historyQuestionPrompt(profile *historypkg.HistoryProfile, id HistoryQuestionID) string {
	if profile == nil {
		return ""
	}
	for _, item := range profile.Questions {
		if item.ID == id {
			return strings.TrimSpace(item.Prompt)
		}
	}
	return ""
}

func frameEvidenceRefs(indices []int) []string {
	if len(indices) == 0 {
		return nil
	}
	frames := normalizeEvidenceFrames(indices, 1000000)
	if len(frames) == 0 {
		return nil
	}
	out := make([]string, 0, len(frames))
	start := frames[0]
	end := frames[0]
	flush := func() {
		out = append(out, fmt.Sprintf("frames:%d-%d", start, end))
	}
	for _, idx := range frames[1:] {
		if idx == end+1 {
			end = idx
			continue
		}
		flush()
		start = idx
		end = idx
	}
	flush()
	return out
}

func frameBoundsFromIndices(indices []int) (*int, *int) {
	frames := normalizeEvidenceFrames(indices, 1000000)
	if len(frames) == 0 {
		return nil, nil
	}
	return intPtr(frames[0]), intPtr(frames[len(frames)-1])
}

func frameBoundsFromEvidence(evidence []string) (*int, *int) {
	var frames []int
	for _, item := range evidence {
		item = strings.TrimSpace(item)
		if !strings.HasPrefix(item, "frames:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(item, "frames:"))
		parts := strings.SplitN(payload, "-", 2)
		if len(parts) != 2 {
			continue
		}
		start, errStart := strconv.Atoi(strings.TrimSpace(parts[0]))
		end, errEnd := strconv.Atoi(strings.TrimSpace(parts[1]))
		if errStart != nil || errEnd != nil {
			continue
		}
		if end < start {
			start, end = end, start
		}
		frames = append(frames, start, end)
	}
	return frameBoundsFromIndices(frames)
}

func historyRecordPriority(item HistoryRecord) int {
	switch item.Kind {
	case HistoryRecordKindAnswer:
		return 3
	case HistoryRecordKindNotable:
		return 2
	case HistoryRecordKindInsight:
		return 1
	default:
		return 0
	}
}

func historyRecordFrameStart(item HistoryRecord) int {
	if item.FrameStart == nil {
		return 1000000
	}
	return *item.FrameStart
}

func historyRecordHash(kind HistoryRecordKind, subtype string, summary string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(summary)), " "))
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(kind)) + "|" + strings.TrimSpace(subtype) + "|" + normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func shortRecordHash(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "sha256:")
	if len(value) > 16 {
		return value[:16]
	}
	return value
}

func titleLikeLabel(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	parts := strings.FieldsFunc(in, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	for idx, part := range parts {
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		parts[idx] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(parts, " ")
}

func intPtr(v int) *int {
	return &v
}
