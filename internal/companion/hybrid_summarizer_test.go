package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/engine"
	_ "modernc.org/sqlite"
)

var updateGolden = flag.Bool("update", false, "update golden files")

// newTestMemoryWithLLM creates a ConversationMemory backed by an in-memory SQLite DB
// with a canned LLM response. It seeds companion_events and companion_turns for
// the given conversation within the event range [startID, endID].
func newTestMemoryWithLLM(t *testing.T, llmResponse string, convID string, startID, endID int64) *ConversationMemory {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	runner := func(_ context.Context, _ engine.LLMChatConfig, _ engine.EngineInput) (engine.EngineOutput, error) {
		return engine.EngineOutput{
			AssistantText: llmResponse,
			StopReason:    engine.StopReasonEndTurn,
		}, nil
	}

	summarizer := NewLLMSummarizer(LLMSummarizerConfig{
		Provider: "test",
		APIKey:   "test-key",
		Model:    "test-model",
	})

	mem, err := NewConversationMemory(db,
		WithSummarizer(summarizer),
		WithEpisodeSummaryRunner(runner),
	)
	if err != nil {
		t.Fatalf("new conversation memory: %v", err)
	}

	// Seed turns and events for the test conversation.
	for id := startID; id <= endID; id++ {
		role := "user"
		evType := EventTypeUserMessage
		if id%2 == 0 {
			role = "assistant"
			evType = EventTypeAssistantMessage
		}
		turnID := mem.idGenerator()

		_, err := db.Exec(`INSERT INTO companion_turns (id, conversation_id, role, content, token_count, created_at)
			VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			turnID, convID, role, "test message "+role, 5)
		if err != nil {
			t.Fatalf("seed turn %d: %v", id, err)
		}

		_, err = db.Exec(`INSERT INTO companion_events (id, conversation_id, event_type, turn_id, created_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			id, convID, evType, turnID)
		if err != nil {
			t.Fatalf("seed event %d: %v", id, err)
		}
	}

	return mem
}

func TestSummarizeEpisodePlan_ValidJSON(t *testing.T) {
	llmResp := `{
		"summary": "User discussed Go preferences.",
		"hard_state_entries": [
			{"entry_type": "preference", "raw_text": "Likes Go", "value": "Go preferred", "confidence": 0.9, "source_event_id": 10}
		],
		"evidence_snippets": [
			{"source_event_id": 10, "event_type": "user_message", "fact_text": "I like Go.", "confidence": 0.85, "bucket": "default"}
		]
	}`

	mem := newTestMemoryWithLLM(t, llmResp, "conv-1", 10, 12)
	plan, err := mem.SummarizeEpisodePlan(context.Background(), "conv-1", 1, 10, 12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Summary != "User discussed Go preferences." {
		t.Errorf("summary = %q, want %q", plan.Summary, "User discussed Go preferences.")
	}
	if len(plan.HardStateEntries) != 1 {
		t.Fatalf("hard state entries = %d, want 1", len(plan.HardStateEntries))
	}
	if plan.HardStateEntries[0].Entry.EntryType != "preference" {
		t.Errorf("entry type = %q, want %q", plan.HardStateEntries[0].Entry.EntryType, "preference")
	}
	if plan.HardStateEntries[0].Entry.Value != "Go preferred" {
		t.Errorf("entry value = %q, want %q", plan.HardStateEntries[0].Entry.Value, "Go preferred")
	}
	if len(plan.EvidenceSnippets) != 1 {
		t.Fatalf("evidence snippets = %d, want 1", len(plan.EvidenceSnippets))
	}
	if plan.EvidenceSnippets[0].FactText != "I like Go." {
		t.Errorf("fact text = %q, want %q", plan.EvidenceSnippets[0].FactText, "I like Go.")
	}

	// Verify no DB writes occurred (plan only).
	var hardCount int
	if err := mem.db.QueryRow("SELECT COUNT(*) FROM companion_hard_state_entries WHERE conversation_id = 'conv-1'").Scan(&hardCount); err != nil {
		t.Fatalf("count hard state: %v", err)
	}
	if hardCount != 0 {
		t.Errorf("hard state rows = %d, want 0 (plan should not write)", hardCount)
	}

	var evidenceCount int
	if err := mem.db.QueryRow("SELECT COUNT(*) FROM companion_evidence_snippets WHERE conversation_id = 'conv-1'").Scan(&evidenceCount); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if evidenceCount != 0 {
		t.Errorf("evidence rows = %d, want 0 (plan should not write)", evidenceCount)
	}
}

func TestSummarizeEpisodePlan_MalformedJSON(t *testing.T) {
	mem := newTestMemoryWithLLM(t, "This is not JSON at all.", "conv-1", 10, 12)
	plan, err := mem.SummarizeEpisodePlan(context.Background(), "conv-1", 1, 10, 12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Summary != "This is not JSON at all." {
		t.Errorf("summary = %q, want raw response", plan.Summary)
	}
	if len(plan.HardStateEntries) != 0 {
		t.Errorf("hard state entries = %d, want 0", len(plan.HardStateEntries))
	}
	if len(plan.EvidenceSnippets) != 0 {
		t.Errorf("evidence snippets = %d, want 0", len(plan.EvidenceSnippets))
	}
}

func TestSummarizeEpisodePlan_EmptySummary(t *testing.T) {
	llmResp := `{"summary": "", "hard_state_entries": [], "evidence_snippets": []}`
	mem := newTestMemoryWithLLM(t, llmResp, "conv-1", 10, 12)
	plan, err := mem.SummarizeEpisodePlan(context.Background(), "conv-1", 1, 10, 12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty summary should fall back to raw response text.
	if plan.Summary != llmResp {
		t.Errorf("summary = %q, want raw response", plan.Summary)
	}
}

func TestSummarizeEpisodePlan_SourceEventIDClamping(t *testing.T) {
	llmResp := `{
		"summary": "Test clamping.",
		"hard_state_entries": [
			{"entry_type": "preference", "raw_text": "too low", "confidence": 0.5, "source_event_id": 1},
			{"entry_type": "preference", "raw_text": "too high", "confidence": 0.5, "source_event_id": 999},
			{"entry_type": "preference", "raw_text": "zero", "confidence": 0.5, "source_event_id": 0}
		],
		"evidence_snippets": []
	}`

	mem := newTestMemoryWithLLM(t, llmResp, "conv-1", 10, 12)
	plan, err := mem.SummarizeEpisodePlan(context.Background(), "conv-1", 1, 10, 12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.HardStateEntries) != 3 {
		t.Fatalf("entries = %d, want 3", len(plan.HardStateEntries))
	}
	// All out-of-range IDs should be clamped to endEventID (12).
	for i, item := range plan.HardStateEntries {
		if item.SourceEventID != 12 {
			t.Errorf("entry[%d] source_event_id = %d, want 12", i, item.SourceEventID)
		}
	}
}

func TestSummarizeEpisodePlan_EvidenceSkipRules(t *testing.T) {
	llmResp := `{
		"summary": "Test skip rules.",
		"hard_state_entries": [],
		"evidence_snippets": [
			{"source_event_id": 10, "fact_text": "valid", "confidence": 0.8, "bucket": "default"},
			{"source_event_id": 10, "fact_text": "", "confidence": 0.8, "bucket": "default"},
			{"source_event_id": 10, "fact_text": "   ", "confidence": 0.8, "bucket": "default"},
			{"source_event_id": 0, "fact_text": "invalid id", "confidence": 0.8, "bucket": "default"},
			{"source_event_id": -1, "fact_text": "negative id", "confidence": 0.8, "bucket": "default"},
			{"source_event_id": 999, "fact_text": "out of range", "confidence": 0.8, "bucket": "default"}
		]
	}`

	mem := newTestMemoryWithLLM(t, llmResp, "conv-1", 10, 12)
	plan, err := mem.SummarizeEpisodePlan(context.Background(), "conv-1", 1, 10, 12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the first snippet (valid) should survive.
	if len(plan.EvidenceSnippets) != 1 {
		t.Fatalf("evidence snippets = %d, want 1", len(plan.EvidenceSnippets))
	}
	if plan.EvidenceSnippets[0].FactText != "valid" {
		t.Errorf("fact text = %q, want %q", plan.EvidenceSnippets[0].FactText, "valid")
	}
}

func TestSummarizeEpisodePlan_ValidationErrors(t *testing.T) {
	mem := newTestMemoryWithLLM(t, `{}`, "conv-1", 10, 12)

	tests := []struct {
		name        string
		convID      string
		episodeID   int64
		startID     int64
		endID       int64
		wantErrFrag string
	}{
		{"empty conv id", "", 1, 10, 12, "conversation_id is required"},
		{"whitespace conv id", "   ", 1, 10, 12, "conversation_id is required"},
		{"zero episode id", "conv-1", 0, 10, 12, "episode_id must be positive"},
		{"negative episode id", "conv-1", -1, 10, 12, "episode_id must be positive"},
		{"zero start id", "conv-1", 1, 0, 12, "start_event_id must be positive"},
		{"zero end id", "conv-1", 1, 10, 0, "end_event_id must be positive"},
		{"end < start", "conv-1", 1, 12, 10, "end_event_id must be >= start_event_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mem.SummarizeEpisodePlan(context.Background(), tt.convID, tt.episodeID, tt.startID, tt.endID)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); got != tt.wantErrFrag {
				t.Errorf("error = %q, want %q", got, tt.wantErrFrag)
			}
		})
	}
}

func TestApplyEpisodeSummaryPlan_DryRun(t *testing.T) {
	mem := newTestMemoryWithLLM(t, `{}`, "conv-1", 10, 12)

	plan := &EpisodeSummaryPlan{
		ConversationID: "conv-1",
		EpisodeID:      1,
		StartEventID:   10,
		EndEventID:     12,
		Summary:        "test",
		HardStateEntries: []HardStateEntryPlanItem{
			{SourceEventID: 10, Entry: ExtractedEntry{EntryType: "preference", RawText: "test", Value: "test", Confidence: 0.9}},
		},
		EvidenceSnippets: []EvidenceSnippetPlanItem{
			{SourceEventID: 10, EventType: "user_message", FactText: "test fact", Confidence: 0.8, Bucket: "default"},
		},
	}

	if err := mem.ApplyEpisodeSummaryPlan(context.Background(), plan, true); err != nil {
		t.Fatalf("dry run failed: %v", err)
	}

	var hardCount int
	if err := mem.db.QueryRow("SELECT COUNT(*) FROM companion_hard_state_entries WHERE conversation_id = 'conv-1'").Scan(&hardCount); err != nil {
		t.Fatalf("count hard state: %v", err)
	}
	if hardCount != 0 {
		t.Errorf("hard state rows = %d, want 0 (dry run)", hardCount)
	}

	var evidenceCount int
	if err := mem.db.QueryRow("SELECT COUNT(*) FROM companion_evidence_snippets WHERE conversation_id = 'conv-1'").Scan(&evidenceCount); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if evidenceCount != 0 {
		t.Errorf("evidence rows = %d, want 0 (dry run)", evidenceCount)
	}
}

func TestApplyEpisodeSummaryPlan_NilPlan(t *testing.T) {
	mem := newTestMemoryWithLLM(t, `{}`, "conv-1", 10, 12)
	err := mem.ApplyEpisodeSummaryPlan(context.Background(), nil, false)
	if err == nil {
		t.Fatal("expected error for nil plan")
	}
}

func TestApplyEpisodeSummaryPlan_Normal(t *testing.T) {
	mem := newTestMemoryWithLLM(t, `{}`, "conv-1", 10, 12)

	// Use "decision" entry type because it doesn't require pattern matching
	// (normalizeEntryKey assigns a monotonic index).
	plan := &EpisodeSummaryPlan{
		ConversationID: "conv-1",
		EpisodeID:      1,
		StartEventID:   10,
		EndEventID:     12,
		Summary:        "test summary",
		HardStateEntries: []HardStateEntryPlanItem{
			{SourceEventID: 10, Entry: ExtractedEntry{EntryType: "decision", RawText: "Use PostgreSQL", Value: "PostgreSQL chosen", Confidence: 0.9}},
		},
		EvidenceSnippets: []EvidenceSnippetPlanItem{
			{SourceEventID: 10, EventType: "user_message", FactText: "I like Go for backends.", Confidence: 0.85, Bucket: "default"},
		},
	}

	if err := mem.ApplyEpisodeSummaryPlan(context.Background(), plan, false); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	var hardCount int
	if err := mem.db.QueryRow("SELECT COUNT(*) FROM companion_hard_state_entries WHERE conversation_id = 'conv-1'").Scan(&hardCount); err != nil {
		t.Fatalf("count hard state: %v", err)
	}
	if hardCount != 1 {
		t.Errorf("hard state rows = %d, want 1", hardCount)
	}

	var evidenceCount int
	if err := mem.db.QueryRow("SELECT COUNT(*) FROM companion_evidence_snippets WHERE conversation_id = 'conv-1'").Scan(&evidenceCount); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if evidenceCount != 1 {
		t.Errorf("evidence rows = %d, want 1", evidenceCount)
	}
}

func TestSummarizeEpisode_EndToEnd(t *testing.T) {
	llmResp := `{
		"summary": "End to end test summary.",
		"hard_state_entries": [
			{"entry_type": "decision", "raw_text": "Use PostgreSQL", "value": "PostgreSQL chosen", "confidence": 0.95, "source_event_id": 10}
		],
		"evidence_snippets": [
			{"source_event_id": 11, "event_type": "assistant_message", "fact_text": "PostgreSQL supports JSON.", "confidence": 0.9, "bucket": "default"}
		]
	}`

	mem := newTestMemoryWithLLM(t, llmResp, "conv-e2e", 10, 12)
	summary, tokenCount, err := mem.SummarizeEpisode(context.Background(), "conv-e2e", 1, 10, 12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary != "End to end test summary." {
		t.Errorf("summary = %q, want %q", summary, "End to end test summary.")
	}
	if tokenCount <= 0 {
		t.Errorf("token count = %d, want > 0", tokenCount)
	}

	// Verify DB writes happened via the Apply step.
	var hardCount int
	if err := mem.db.QueryRow("SELECT COUNT(*) FROM companion_hard_state_entries WHERE conversation_id = 'conv-e2e'").Scan(&hardCount); err != nil {
		t.Fatalf("count hard state: %v", err)
	}
	if hardCount != 1 {
		t.Errorf("hard state rows = %d, want 1", hardCount)
	}

	var evidenceCount int
	if err := mem.db.QueryRow("SELECT COUNT(*) FROM companion_evidence_snippets WHERE conversation_id = 'conv-e2e'").Scan(&evidenceCount); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if evidenceCount != 1 {
		t.Errorf("evidence rows = %d, want 1", evidenceCount)
	}
}

// TestSummarizeEpisodePlan_ValueFallback verifies that when Value is empty,
// it falls back to RawText.
func TestSummarizeEpisodePlan_ValueFallback(t *testing.T) {
	llmResp := `{
		"summary": "Value fallback test.",
		"hard_state_entries": [
			{"entry_type": "preference", "raw_text": "likes testing", "confidence": 0.8, "source_event_id": 10}
		],
		"evidence_snippets": []
	}`

	mem := newTestMemoryWithLLM(t, llmResp, "conv-1", 10, 12)
	plan, err := mem.SummarizeEpisodePlan(context.Background(), "conv-1", 1, 10, 12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.HardStateEntries) != 1 {
		t.Fatalf("entries = %d, want 1", len(plan.HardStateEntries))
	}
	if plan.HardStateEntries[0].Entry.Value != "likes testing" {
		t.Errorf("value = %q, want %q (fallback to raw_text)", plan.HardStateEntries[0].Entry.Value, "likes testing")
	}
}

// Golden tests: verify SummarizeEpisodePlan output against golden files.

func TestSummarizeEpisodePlan_Golden(t *testing.T) {
	cases := []struct {
		name       string
		llmFixture string
		goldenFile string
	}{
		{"valid_json", "valid_json_llm_response.txt", "valid_json.golden.json"},
		{"malformed_json", "malformed_json_llm_response.txt", "malformed_json.golden.json"},
		{"empty_entries", "empty_entries_llm_response.txt", "empty_entries.golden.json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			llmResp, err := os.ReadFile(filepath.Join("testdata", tc.llmFixture))
			if err != nil {
				t.Fatalf("read LLM fixture: %v", err)
			}

			mem := newTestMemoryWithLLM(t, string(llmResp), "conv-golden", 10, 12)
			plan, err := mem.SummarizeEpisodePlan(context.Background(), "conv-golden", 1, 10, 12)
			if err != nil {
				t.Fatalf("SummarizeEpisodePlan: %v", err)
			}

			got, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				t.Fatalf("marshal plan: %v", err)
			}
			got = append(got, '\n')

			goldenPath := filepath.Join("testdata", tc.goldenFile)
			if *updateGolden {
				if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
					t.Fatalf("write golden file: %v", err)
				}
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden file (run with -update to create): %v", err)
			}

			if string(got) != string(want) {
				t.Errorf("plan does not match golden file %s\ngot:\n%s\nwant:\n%s", tc.goldenFile, got, want)
			}
		})
	}
}
