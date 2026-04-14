package transcriptpipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	historypkg "github.com/joshka0/foxctl/internal/context/transcriptpipeline/history"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

func TestBuildHistoryRecords_AnchorsDerivedTexts(t *testing.T) {
	t.Parallel()

	profile := historypkg.DefaultHistoryProfile()
	records := BuildHistoryRecords(profile, HistoryRecordContext{
		ConversationID: "conv-1",
		GroupID:        "group-1",
		SessionIDs:     []string{"sess-a", "sess-b"},
	}, []DecisionInsight{
		{
			Kind:                 InsightKindDirection,
			Summary:              "Close the release gate verdict.",
			Status:               InsightStatusActive,
			Confidence:           0.73,
			SourceBasis:          "user",
			EvidenceFrameIndices: []int{2, 3},
			Tags:                 []string{"follow_up_needed"},
		},
	}, []NotableInsight{
		{
			Kind:         NotableInsightMisunderstanding,
			Headline:     "We initially misunderstood the release gating request.",
			WhyItMatters: "The next agent should not repeat the same framing error.",
			StartFrame:   4,
			EndFrame:     5,
			Resolution:   "corrected",
			Reaction:     "corrected",
		},
	}, []historypkg.HistoryAnswer{
		{
			QuestionID: HistoryQuestionOpenQuestions,
			Answer:     "Do we still need manual QA signoff?",
			Confidence: 0.7,
			Evidence:   []string{"frames:4-5"},
		},
	})

	if len(records) != 3 {
		t.Fatalf("records=%d want 3", len(records))
	}

	var foundInsight, foundNotable, foundAnswer bool
	for _, item := range records {
		if item.RecordID == "" || item.NormalizedHash == "" {
			t.Fatalf("record missing ids: %+v", item)
		}
		if item.ConversationID != "conv-1" || item.GroupID != "group-1" {
			t.Fatalf("record context mismatch: %+v", item)
		}
		switch item.Kind {
		case HistoryRecordKindInsight:
			foundInsight = true
			if item.FrameStart == nil || item.FrameEnd == nil || *item.FrameStart != 2 || *item.FrameEnd != 3 {
				t.Fatalf("insight frame bounds=%v..%v want 2..3", item.FrameStart, item.FrameEnd)
			}
			if item.RetrievalText == "" || item.InsightKind != string(InsightKindDirection) {
				t.Fatalf("insight record=%+v", item)
			}
		case HistoryRecordKindNotable:
			foundNotable = true
			if item.NotableKind != string(NotableInsightMisunderstanding) {
				t.Fatalf("notable kind=%q want %q", item.NotableKind, NotableInsightMisunderstanding)
			}
			if item.FrameStart == nil || item.FrameEnd == nil || *item.FrameStart != 4 || *item.FrameEnd != 5 {
				t.Fatalf("notable frame bounds=%v..%v want 4..5", item.FrameStart, item.FrameEnd)
			}
		case HistoryRecordKindAnswer:
			foundAnswer = true
			if item.HistoryQuestionID != HistoryQuestionOpenQuestions {
				t.Fatalf("question_id=%q want %q", item.HistoryQuestionID, HistoryQuestionOpenQuestions)
			}
			if item.RetrievalText == "" || item.RetrievalText == item.Summary {
				t.Fatalf("answer retrieval text=%q want prompt+answer", item.RetrievalText)
			}
			if item.FrameStart == nil || item.FrameEnd == nil || *item.FrameStart != 4 || *item.FrameEnd != 5 {
				t.Fatalf("answer frame bounds=%v..%v want 4..5", item.FrameStart, item.FrameEnd)
			}
		}
	}

	if !foundInsight || !foundNotable || !foundAnswer {
		t.Fatalf("records=%+v missing expected kinds", records)
	}
}

func TestBuildHistoryRecords_PreservesQuestionPromptInRetrievalText(t *testing.T) {
	t.Parallel()

	profile := historypkg.DefaultHistoryProfile()
	records := BuildHistoryRecords(profile, HistoryRecordContext{ConversationID: "conv-2"}, nil, nil, []historypkg.HistoryAnswer{
		{
			QuestionID: HistoryQuestionNextStep,
			Answer:     "Continue the second-pass consolidator direction.",
			Confidence: 0.72,
		},
	})
	if len(records) != 1 {
		t.Fatalf("records=%d want 1", len(records))
	}
	if records[0].Kind != HistoryRecordKindAnswer {
		t.Fatalf("kind=%q want %q", records[0].Kind, HistoryRecordKindAnswer)
	}
	if got := records[0].RetrievalText; got == "" || !strings.HasPrefix(got, "Question:") {
		t.Fatalf("retrieval_text=%q want question-prefixed text", got)
	}
}

func TestBuildHistoryRecords_PreservesAnswerLabel(t *testing.T) {
	t.Parallel()

	records := BuildHistoryRecords(historypkg.DefaultHistoryProfile(), HistoryRecordContext{ConversationID: "conv-label"}, nil, nil, []historypkg.HistoryAnswer{
		{
			QuestionID: HistoryQuestionObjective,
			Answer:     "Complete and verify all planned backend integration tasks while updating the plan.",
			Label:      "complete backend tasks",
			Confidence: 0.8,
		},
	})
	if len(records) != 1 {
		t.Fatalf("records=%d want 1", len(records))
	}
	if records[0].AnswerLabel != "complete backend tasks" {
		t.Fatalf("answer_label=%q", records[0].AnswerLabel)
	}
}

func TestPersistHistoryRecords_SavesRetrievalUnits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("memory.Open() error = %v", err)
	}
	defer store.Close()

	records := BuildHistoryRecords(historypkg.DefaultHistoryProfile(), HistoryRecordContext{
		ConversationID: "conv-persist",
		SessionIDs:     []string{"sess-1"},
	}, []DecisionInsight{{
		Kind:                 InsightKindDirection,
		Summary:              "Close the release gate verdict.",
		Status:               InsightStatusActive,
		Confidence:           0.75,
		EvidenceFrameIndices: []int{2, 3},
		SourceBasis:          "user",
	}}, nil, []historypkg.HistoryAnswer{{
		QuestionID: HistoryQuestionNextStep,
		Answer:     "Close the release gate verdict.",
		Confidence: 0.72,
		Evidence:   []string{"frames:2-3"},
	}})

	got, err := historypkg.PersistHistoryRecords(ctx, store, "/tmp/ws", "sess-1", "sess-1", records, nil)
	if err != nil {
		t.Fatalf("PersistHistoryRecords() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("persisted=%d want 2", len(got))
	}

	entry, err := store.Get(ctx, got[0].Name, "/tmp/ws")
	if err != nil {
		t.Fatalf("Get(%q) error = %v", got[0].Name, err)
	}
	if !strings.HasPrefix(entry.Name, historypkg.TranscriptHistoryPrefix("sess-1")) {
		t.Fatalf("entry name=%q want prefix %q", entry.Name, historypkg.TranscriptHistoryPrefix("sess-1"))
	}
	var payload map[string]any
	if err := json.Unmarshal(entry.Result, &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if payload["retrieval_text"] == "" {
		t.Fatalf("payload missing retrieval_text: %v", payload)
	}
	if payload["record_kind"] == "" {
		t.Fatalf("payload missing record_kind: %v", payload)
	}
}

func TestReconcileHistoryRecordPrefix_RemovesStaleEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("memory.Open() error = %v", err)
	}
	defer store.Close()

	workspace := "/tmp/ws"
	prefix := historypkg.TranscriptHistoryPrefix("sess-reconcile")
	for _, item := range []struct {
		name string
		typ  string
	}{
		{name: prefix + "insight:keep", typ: "history_insight"},
		{name: prefix + "answer:drop", typ: "history_answer"},
		{name: "transcript-history:other:notable:stay", typ: "history_notable"},
	} {
		if _, err := store.Save(ctx, memory.NamedEntry{
			Name:      item.name,
			Type:      item.typ,
			Workspace: workspace,
			Summary:   item.name,
			Result:    []byte(`{}`),
		}); err != nil {
			t.Fatalf("Save(%q) error = %v", item.name, err)
		}
	}

	removed, err := historypkg.ReconcileHistoryRecordPrefix(ctx, store, workspace, prefix, []historypkg.PersistedHistoryRecord{{
		Name:    prefix + "insight:keep",
		Type:    "history_insight",
		Kind:    historypkg.HistoryRecordKindInsight,
		Summary: "keep",
	}})
	if err != nil {
		t.Fatalf("ReconcileHistoryRecordPrefix() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != prefix+"answer:drop" {
		t.Fatalf("removed=%v want [%q]", removed, prefix+"answer:drop")
	}
	if _, err := store.Get(ctx, prefix+"answer:drop", workspace); err == nil {
		t.Fatal("expected dropped history entry to be deleted")
	}
	if _, err := store.Get(ctx, prefix+"insight:keep", workspace); err != nil {
		t.Fatalf("expected keep entry to remain: %v", err)
	}
	if _, err := store.Get(ctx, "transcript-history:other:notable:stay", workspace); err != nil {
		t.Fatalf("expected unrelated entry to remain: %v", err)
	}
}
