package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
)

type HistoryRecordKind string

const (
	HistoryRecordKindInsight HistoryRecordKind = "insight"
	HistoryRecordKindNotable HistoryRecordKind = "notable_insight"
	HistoryRecordKindAnswer  HistoryRecordKind = "history_answer"
)

// HistoryRecord is the normalized retrieval unit for transcript-derived history.
// The intended embedding text is RetrievalText; EvidenceRefs and frame bounds preserve provenance.
type HistoryRecord struct {
	RecordID          string            `json:"record_id"`
	Kind              HistoryRecordKind `json:"kind"`
	ConversationID    string            `json:"conversation_id,omitempty"`
	GroupID           string            `json:"group_id,omitempty"`
	SessionIDs        []string          `json:"session_ids,omitempty"`
	SourceStartedAt   time.Time         `json:"source_started_at,omitempty"`
	Summary           string            `json:"summary"`
	RetrievalText     string            `json:"retrieval_text"`
	Confidence        float64           `json:"confidence"`
	InsightKind       string            `json:"insight_kind,omitempty"`
	InsightStatus     string            `json:"insight_status,omitempty"`
	NotableKind       string            `json:"notable_kind,omitempty"`
	HistoryQuestionID HistoryQuestionID `json:"history_question_id,omitempty"`
	AnswerLabel       string            `json:"answer_label,omitempty"`
	SourceBasis       string            `json:"source_basis,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
	FrameStart        *int              `json:"frame_start,omitempty"`
	FrameEnd          *int              `json:"frame_end,omitempty"`
	EvidenceRefs      []string          `json:"evidence_refs,omitempty"`
	NormalizedHash    string            `json:"normalized_hash"`
}

type HistoryRecordContext struct {
	ConversationID  string
	GroupID         string
	SessionIDs      []string
	SourceStartedAt time.Time
}

type HistoryRecordEmbedFunc func(context.Context, string) ([]float32, error)

type PersistedHistoryRecord struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	RecordID   string            `json:"record_id"`
	Kind       HistoryRecordKind `json:"kind"`
	Summary    string            `json:"summary"`
	Embedded   bool              `json:"embedded"`
	FrameStart *int              `json:"frame_start,omitempty"`
	FrameEnd   *int              `json:"frame_end,omitempty"`
}

func PersistHistoryRecords(ctx context.Context, store storage.MemoryStore, workspace, ownerID, sessionID string, records []HistoryRecord, embed HistoryRecordEmbedFunc) ([]PersistedHistoryRecord, error) {
	workspace = strings.TrimSpace(workspace)
	ownerID = strings.TrimSpace(ownerID)
	if workspace == "" {
		return nil, fmt.Errorf("transcriptpipeline/history: persist history records: workspace is required")
	}
	if ownerID == "" {
		return nil, fmt.Errorf("transcriptpipeline/history: persist history records: owner id is required")
	}
	if len(records) == 0 {
		return nil, nil
	}
	out := make([]PersistedHistoryRecord, 0, len(records))
	workspacePath := ws.Normalize(workspace)
	workspaceFamilyPath := ws.FamilyPath(workspacePath)
	for _, item := range records {
		entryType, ok := historyRecordMemoryType(item.Kind)
		if !ok {
			continue
		}
		summary := summarizeRecordSummary(firstNonEmpty(item.Summary, item.RetrievalText))
		if summary == "" {
			continue
		}
		name := HistoryRecordMemoryName(ownerID, item)
		result, err := json.Marshal(map[string]any{
			"source":                "sessions/history-records",
			"record_id":             item.RecordID,
			"record_kind":           item.Kind,
			"conversation_id":       item.ConversationID,
			"group_id":              item.GroupID,
			"session_ids":           item.SessionIDs,
			"source_started_at":     formatOptionalRecordTime(item.SourceStartedAt),
			"workspace_path":        workspacePath,
			"workspace_family_path": workspaceFamilyPath,
			"summary":               item.Summary,
			"retrieval_text":        item.RetrievalText,
			"confidence":            item.Confidence,
			"insight_kind":          item.InsightKind,
			"insight_status":        item.InsightStatus,
			"notable_kind":          item.NotableKind,
			"history_question_id":   item.HistoryQuestionID,
			"answer_label":          item.AnswerLabel,
			"source_basis":          item.SourceBasis,
			"tags":                  item.Tags,
			"frame_start":           item.FrameStart,
			"frame_end":             item.FrameEnd,
			"evidence_refs":         item.EvidenceRefs,
			"normalized_hash":       item.NormalizedHash,
		})
		if err != nil {
			return nil, fmt.Errorf("transcriptpipeline/history: persist history record marshal: %w", err)
		}
		if _, err := store.Save(ctx, storage.NamedEntry{
			Name:      name,
			Type:      entryType,
			Workspace: workspace,
			Summary:   summary,
			Result:    result,
			SessionID: strings.TrimSpace(sessionID),
		}); err != nil {
			return nil, fmt.Errorf("transcriptpipeline/history: persist history record save: %w", err)
		}
		embedded := false
		if embed != nil && strings.TrimSpace(item.RetrievalText) != "" {
			if vec, err := embed(ctx, item.RetrievalText); err == nil && len(vec) > 0 {
				if err := store.UpdateEmbedding(ctx, name, workspace, vec); err == nil {
					embedded = true
				}
			}
		}
		out = append(out, PersistedHistoryRecord{
			Name:       name,
			Type:       entryType,
			RecordID:   item.RecordID,
			Kind:       item.Kind,
			Summary:    summary,
			Embedded:   embedded,
			FrameStart: item.FrameStart,
			FrameEnd:   item.FrameEnd,
		})
	}
	return out, nil
}

func ReconcileHistoryRecordPrefix(ctx context.Context, store storage.MemoryStore, workspace, prefix string, keep []PersistedHistoryRecord) ([]string, error) {
	workspace = strings.TrimSpace(workspace)
	prefix = strings.TrimSpace(prefix)
	if workspace == "" || prefix == "" {
		return nil, nil
	}

	keepSet := make(map[string]struct{}, len(keep))
	for _, item := range keep {
		keepSet[item.Name] = struct{}{}
	}

	var removed []string
	offset := 0
	for {
		entries, total, err := store.ListFiltered(ctx, workspace, storage.MemoryListFilter{Types: historyRecordMemoryTypes()}, 200, offset)
		if err != nil {
			return nil, fmt.Errorf("transcriptpipeline/history: reconcile history records list: %w", err)
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name, prefix) {
				continue
			}
			if _, ok := keepSet[entry.Name]; ok {
				continue
			}
			if err := store.Delete(ctx, entry.Name, workspace); err != nil {
				return nil, fmt.Errorf("transcriptpipeline/history: reconcile history records delete %s: %w", entry.Name, err)
			}
			removed = append(removed, entry.Name)
		}
		offset += len(entries)
		if offset >= total || len(entries) == 0 {
			break
		}
	}
	sort.Strings(removed)
	return removed, nil
}

func TranscriptHistoryPrefix(ownerID string) string {
	return fmt.Sprintf("transcript-history:%s:", strings.TrimSpace(ownerID))
}

func HistoryRecordMemoryName(ownerID string, record HistoryRecord) string {
	hash := strings.TrimPrefix(strings.TrimSpace(record.NormalizedHash), "sha256:")
	if hash == "" {
		hash = strings.TrimPrefix(historyRecordHash(record.Kind, string(record.HistoryQuestionID), firstNonEmpty(record.RetrievalText, record.Summary)), "sha256:")
	}
	return TranscriptHistoryPrefix(ownerID) + fmt.Sprintf("%s:%s", historyRecordTypeSuffix(record.Kind), hash)
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

func formatOptionalRecordTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func historyRecordHash(kind HistoryRecordKind, subtype string, summary string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(summary)), " "))
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(kind)) + "|" + strings.TrimSpace(subtype) + "|" + normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func summarizeRecordSummary(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return ""
	}
	if len(text) <= 200 {
		return text
	}
	return text[:199] + "…"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
