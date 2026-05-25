package dreamer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/context/transcriptpipeline"
	"github.com/joshka0/foxctl/internal/context/transcriptpipeline/history"
	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
)

func TestPlanSingleInsightDreamNoteUsesPersistedHistoryWithoutRawTranscript(t *testing.T) {
	started := time.Date(2026, 5, 24, 9, 0, 0, 0, time.UTC)
	result := transcriptpipeline.SingleRunResult{
		Parsed: sourceimport.ParsedSession{
			SessionID:     "sess-1",
			WorkspacePath: "/repo/foxctl",
		},
		ConversationID: "conv-1",
		InsightBrief: &transcriptpipeline.InsightBrief{
			Overview: "The service should convert transcript history into reviewable memory drafts.",
		},
		Insights: []transcriptpipeline.DecisionInsight{{
			Kind:       transcriptpipeline.InsightKindDirection,
			Status:     transcriptpipeline.InsightStatusActive,
			Summary:    "Run transcript dreaming outside the curator maintenance loop.",
			Confidence: 0.86,
			Tags:       []string{"dream_worker"},
		}},
		HistoryRecords: []history.HistoryRecord{{
			RecordID:          "record-1",
			Kind:              history.HistoryRecordKindInsight,
			ConversationID:    "conv-1",
			SessionIDs:        []string{"sess-1"},
			SourceStartedAt:   started,
			Summary:           "Run transcript dreaming outside the curator maintenance loop.",
			RetrievalText:     "RAW TRANSCRIPT TURN THAT MUST NOT APPEAR",
			NormalizedHash:    "sha256:record1",
			EvidenceRefs:      []string{"file:/tmp/source.jsonl"},
			Confidence:        0.86,
			InsightKind:       "direction",
			InsightStatus:     "active",
			HistoryQuestionID: "test",
		}},
	}
	persisted := []history.PersistedHistoryRecord{{
		Name:     "transcript-history:sess-1:insight:record1",
		RecordID: "record-1",
		Kind:     history.HistoryRecordKindInsight,
		Summary:  "Run transcript dreaming outside the curator maintenance loop.",
	}}

	note, err := PlanSingleInsightDreamNote(result, persisted, time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC), Source{
		Provider:    "codex",
		SessionID:   "sess-1",
		Fingerprint: "sha256:source",
	})
	if err != nil {
		t.Fatalf("PlanSingleInsightDreamNote() error = %v", err)
	}
	if note.SourceLane != "transcript_dream" {
		t.Fatalf("SourceLane=%q want transcript_dream", note.SourceLane)
	}
	if !strings.Contains(note.Content, `source_lane: "transcript_dream"`) {
		t.Fatalf("missing source_lane frontmatter:\n%s", note.Content)
	}
	if !strings.Contains(note.Content, "`record-1`") {
		t.Fatalf("missing source ref:\n%s", note.Content)
	}
	if strings.Contains(note.Content, "RAW TRANSCRIPT TURN") {
		t.Fatalf("note leaked raw transcript retrieval text:\n%s", note.Content)
	}
}

func TestWriteDreamNoteUsesDeterministicOverwrite(t *testing.T) {
	writer := &recordingDreamNoteWriter{}
	note := mustPlanDreamNote(t)

	if err := writeDreamNote(context.Background(), writer, note); err != nil {
		t.Fatalf("writeDreamNote() error = %v", err)
	}
	if writer.notePath != note.DraftPath || writer.content != note.Content || !writer.overwrite {
		t.Fatalf("writer=%+v want overwrite write to draft path", writer)
	}
}

func TestIndexDreamNotePassesPlannedNote(t *testing.T) {
	note := mustPlanDreamNote(t)
	indexer := &recordingDreamNoteIndexer{}

	if err := indexDreamNote(context.Background(), indexer, note); err != nil {
		t.Fatalf("indexDreamNote() error = %v", err)
	}
	if indexer.note.DraftPath != note.DraftPath || indexer.note.Content != note.Content {
		t.Fatalf("indexed note=%+v want planned note=%+v", indexer.note, note)
	}
}

func TestIndexDreamNoteReturnsIndexerError(t *testing.T) {
	want := errors.New("index unavailable")
	indexer := &recordingDreamNoteIndexer{err: want}

	err := indexDreamNote(context.Background(), indexer, mustPlanDreamNote(t))
	if !errors.Is(err, want) {
		t.Fatalf("indexDreamNote() error=%v want %v", err, want)
	}
}

func TestBlurSingleInsightDreamNoteInputAddsValidatedAgentBlur(t *testing.T) {
	input := contextplane.TranscriptDreamNoteInput{
		Project:          "foxctl",
		WorkspaceID:      "foxctl",
		Provider:         "codex",
		SessionID:        "sess-1",
		SourceDigest:     "sha256:source",
		Title:            "You own exactly one file",
		DistilledSummary: "You own exactly one file.",
		BlurredMechanisms: []contextplane.TranscriptDreamMechanism{{
			Name:    "direction",
			Summary: "You own exactly one file.",
		}},
		SourceRefs: []contextplane.TranscriptDreamSourceRef{{RecordID: "record-1", Summary: "You own exactly one file."}},
	}
	agent := &recordingDreamBlurAgent{output: contextplane.MemoryBlurAgentOutput{
		AbstractSchema: "single-owner boundaries reduce coordination ambiguity across parallel workers",
		MechanismTags:  []string{"bounded_ownership"},
		Confidence:     0.87,
		LeakageRisk:    0.01,
	}}

	got, ok, err := blurSingleInsightDreamNoteInput(context.Background(), agent, "pi", input)
	if err != nil {
		t.Fatalf("blurSingleInsightDreamNoteInput() error = %v", err)
	}
	if !ok || got.AgentBlur == nil {
		t.Fatalf("expected accepted agent blur: ok=%v input=%#v", ok, got.AgentBlur)
	}
	if agent.input.ID != "transcript-dream" || len(agent.input.SourceRefs) != 0 {
		t.Fatalf("agent prompt should use generic id and hide concrete source refs: %#v", agent.input)
	}
	note, err := contextplane.PlanTranscriptDreamNote(got)
	if err != nil {
		t.Fatalf("PlanTranscriptDreamNote() error = %v", err)
	}
	if !strings.Contains(note.Content, "## Agent Blurred Mechanism") || !strings.Contains(note.Content, "bounded_ownership") {
		t.Fatalf("blurred note content missing agent abstraction:\n%s", note.Content)
	}
}

func mustPlanDreamNote(t *testing.T) contextplane.TranscriptDreamNote {
	t.Helper()
	result := transcriptpipeline.SingleRunResult{
		Parsed:         sourceimport.ParsedSession{SessionID: "sess-1", WorkspacePath: "/repo/foxctl"},
		ConversationID: "conv-1",
		Insights: []transcriptpipeline.DecisionInsight{{
			Kind:       transcriptpipeline.InsightKindDirection,
			Status:     transcriptpipeline.InsightStatusActive,
			Summary:    "Persist deterministic dream note drafts.",
			Confidence: 0.8,
		}},
		HistoryRecords: []history.HistoryRecord{{
			RecordID:       "record-1",
			Kind:           history.HistoryRecordKindInsight,
			ConversationID: "conv-1",
			Summary:        "Persist this draft.",
		}},
	}
	persisted := []history.PersistedHistoryRecord{{
		Name:     "transcript-history:sess-1:insight:record1",
		RecordID: "record-1",
		Kind:     history.HistoryRecordKindInsight,
		Summary:  "Persist this draft.",
	}}
	note, err := PlanSingleInsightDreamNote(result, persisted, time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC), Source{})
	if err != nil {
		t.Fatalf("PlanSingleInsightDreamNote() error = %v", err)
	}
	return note
}

type recordingDreamNoteWriter struct {
	notePath  string
	content   string
	overwrite bool
}

func (w *recordingDreamNoteWriter) CreateNote(_ context.Context, notePath, content string, overwrite bool) error {
	w.notePath = notePath
	w.content = content
	w.overwrite = overwrite
	return nil
}

type recordingDreamNoteIndexer struct {
	note contextplane.TranscriptDreamNote
	err  error
}

func (i *recordingDreamNoteIndexer) IndexDreamNote(_ context.Context, note contextplane.TranscriptDreamNote) error {
	i.note = note
	return i.err
}

type recordingDreamBlurAgent struct {
	input  contextplane.MemoryBlurAgentPromptInput
	output contextplane.MemoryBlurAgentOutput
}

func (a *recordingDreamBlurAgent) BlurMemory(_ context.Context, input contextplane.MemoryBlurAgentPromptInput) (contextplane.MemoryBlurAgentOutput, string, error) {
	a.input = input
	return a.output, "", nil
}
