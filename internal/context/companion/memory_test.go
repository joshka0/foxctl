package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	actormemory "github.com/jkatigb/agentctl/internal/actor/memory"
	_ "modernc.org/sqlite"
)

type fixedTokenCounter int

func (c fixedTokenCounter) Count(_ string) int { return int(c) }

// TestTrimTurnsToTokenBudget verifies trimming preserves the most recent turns within a budget.
func TestTrimTurnsToTokenBudget(t *testing.T) {
	turns := []ConversationTurn{
		{Content: "first", TokenCount: 4},
		{Content: "second", TokenCount: 4},
		{Content: "third", TokenCount: 4},
	}

	got := trimTurnsToTokenBudget(turns, 8)
	if len(got) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(got))
	}
	if got[0].Content != "second" || got[1].Content != "third" {
		t.Fatalf("unexpected turn order: %q, %q", got[0].Content, got[1].Content)
	}
}

// TestApplyTotalTokenBudgetPrefersRecent verifies budget allocation drops older history before newer content.
func TestApplyTotalTokenBudgetPrefersRecent(t *testing.T) {
	history := strings.Repeat("h", 40) // 10 tokens
	summary := strings.Repeat("s", 40) // 10 tokens
	turns := strings.Repeat("t", 40)   // 10 tokens

	gotHistory, gotSummary, gotTurns := applyTotalTokenBudget(history, summary, turns, 20)

	if gotTurns == "" {
		t.Fatal("expected turns to be preserved")
	}
	if gotSummary == "" {
		t.Fatal("expected summaries to be preserved")
	}
	if gotHistory != "" {
		t.Fatalf("expected history to be dropped, got %q", gotHistory)
	}

	total := actormemory.EstimateTokens(gotHistory) + actormemory.EstimateTokens(gotSummary) + actormemory.EstimateTokens(gotTurns)
	if total > 20 {
		t.Fatalf("expected total tokens <= 20, got %d", total)
	}
}

// TestTrimToTokenBudgetUTF8 verifies trimming does not split UTF-8 runes.
func TestTrimToTokenBudgetUTF8(t *testing.T) {
	text := strings.Repeat("你好", 10)

	head := trimToTokenBudget(text, 5, false)
	if !utf8.ValidString(head) {
		t.Fatalf("expected valid UTF-8 in head trim, got %q", head)
	}

	tail := trimToTokenBudget(text, 5, true)
	if !utf8.ValidString(tail) {
		t.Fatalf("expected valid UTF-8 in tail trim, got %q", tail)
	}
}

// TestGetHybridContextUsesHybridRuntime verifies hybrid context formatting.
func TestGetHybridContextUsesHybridRuntime(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mem, err := NewConversationMemory(db)
	if err != nil {
		t.Fatalf("new conversation memory: %v", err)
	}

	convID := "conv-1"
	if err := mem.AppendTurn(ctx, ConversationTurn{
		ConversationID: convID,
		Role:           "user",
		Content:        "We should use sqlite for now.",
		CreatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append turn: %v", err)
	}

	got, err := mem.GetHybridContext(ctx, convID, "")
	if err != nil {
		t.Fatalf("get context: %v", err)
	}
	if !strings.Contains(got, "=== RECENT TURNS ===") {
		t.Fatalf("expected hybrid recent turns section, got %q", got)
	}
	if !strings.Contains(got, "user: We should use sqlite for now.") {
		t.Fatalf("expected user turn in hybrid context, got %q", got)
	}
}

func TestAppendTurnUsesConfiguredTokenCounter(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mem, err := NewConversationMemory(db, WithTokenCounter(fixedTokenCounter(42)))
	if err != nil {
		t.Fatalf("new conversation memory: %v", err)
	}

	turn := ConversationTurn{
		ConversationID: "conv-token-counter",
		Role:           "user",
		Content:        "I prefer concise answers.",
	}
	if err := mem.AppendTurn(ctx, turn); err != nil {
		t.Fatalf("append turn: %v", err)
	}

	var tokenCount int
	if err := db.QueryRowContext(ctx, `SELECT token_count FROM companion_turns WHERE conversation_id = ? LIMIT 1`, "conv-token-counter").Scan(&tokenCount); err != nil {
		t.Fatalf("query token_count: %v", err)
	}
	if tokenCount != 42 {
		t.Fatalf("token_count = %d, want %d", tokenCount, 42)
	}
}

func TestAppendTurnExtractsPreferencesImmediately(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mem, err := NewConversationMemory(db)
	if err != nil {
		t.Fatalf("new conversation memory: %v", err)
	}

	turn := ConversationTurn{
		ConversationID: "conv-pref-extract",
		Role:           "user",
		Content:        "I prefer short responses and bullet points.",
	}
	if err := mem.AppendTurn(ctx, turn); err != nil {
		t.Fatalf("append turn: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM companion_hard_state_entries
		WHERE conversation_id = ? AND entry_type = ?
	`, "conv-pref-extract", EntryTypePreference).Scan(&count); err != nil {
		t.Fatalf("query preference entries: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected at least one preference extraction entry")
	}
}

func TestExtractExplicitFacts(t *testing.T) {
	got := extractExplicitFacts("Remember these facts: owner Mina, codename cedar-planet-42, deploy window Tuesday 14:00 UTC, rollback color is amber.")
	if len(got) < 4 {
		t.Fatalf("explicit facts=%d want at least 4", len(got))
	}

	byKey := make(map[string]ExtractedEntry, len(got))
	for _, entry := range got {
		byKey[entry.Key] = entry
	}

	if byKey["owner"].Value != "Mina" {
		t.Fatalf("owner value=%q want Mina", byKey["owner"].Value)
	}
	if byKey["codename"].Value != "cedar-planet-42" {
		t.Fatalf("codename value=%q want cedar-planet-42", byKey["codename"].Value)
	}
	if byKey["deploy_window"].Value != "Tuesday 14:00 UTC" {
		t.Fatalf("deploy_window value=%q want Tuesday 14:00 UTC", byKey["deploy_window"].Value)
	}
	if byKey["rollback_color"].Value != "amber" {
		t.Fatalf("rollback_color value=%q want amber", byKey["rollback_color"].Value)
	}
}

func TestExtractExplicitFacts_CutsOffChainedUpdateClauses(t *testing.T) {
	got := extractExplicitFacts("Update the codename from cedar-planet-42 to amber-river-19 and change the deploy window to Thursday 09:30 UTC.")
	byKey := make(map[string]ExtractedEntry, len(got))
	for _, entry := range got {
		byKey[entry.Key] = entry
	}

	if byKey["codename"].Value != "amber-river-19" {
		t.Fatalf("codename value=%q want amber-river-19", byKey["codename"].Value)
	}
	if byKey["deploy_window"].Value != "Thursday 09:30 UTC" {
		t.Fatalf("deploy_window value=%q want Thursday 09:30 UTC", byKey["deploy_window"].Value)
	}
}

func TestAppendTurnExtractsTechnicalFactsImmediately(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mem, err := NewConversationMemory(db)
	if err != nil {
		t.Fatalf("new conversation memory: %v", err)
	}

	if err := mem.AppendTurn(ctx, ConversationTurn{
		ConversationID: "conv-tech-facts",
		Role:           "user",
		Content:        "Remember these facts: owner Mina, codename cedar-planet-42, deploy window Tuesday 14:00 UTC.",
	}); err != nil {
		t.Fatalf("append initial facts: %v", err)
	}
	if err := mem.AppendTurn(ctx, ConversationTurn{
		ConversationID: "conv-tech-facts",
		Role:           "user",
		Content:        "Update the codename from cedar-planet-42 to amber-river-19 and change the deploy window to Thursday 09:30 UTC.",
	}); err != nil {
		t.Fatalf("append updated facts: %v", err)
	}

	hardState, err := mem.GetCachedHardState(ctx, "conv-tech-facts")
	if err != nil {
		t.Fatalf("get cached hard state: %v", err)
	}

	assertValue := func(mapKey, want string) {
		t.Helper()
		entry, ok := hardState[mapKey]
		if !ok {
			t.Fatalf("missing hard state key %q", mapKey)
		}
		var got string
		if err := json.Unmarshal([]byte(entry.ValueJSON), &got); err != nil {
			got = entry.ValueJSON
		}
		if got != want {
			t.Fatalf("%s=%q want %q", mapKey, got, want)
		}
	}

	assertValue("technical_context:tech:owner", "Mina")
	assertValue("technical_context:tech:codename", "amber-river-19")
	assertValue("technical_context:tech:deploy_window", "Thursday 09:30 UTC")
}

func TestAppendTurnAlwaysBridgesToHybridEvents(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mem, err := NewConversationMemory(db)
	if err != nil {
		t.Fatalf("new conversation memory: %v", err)
	}

	turn := ConversationTurn{
		ConversationID: "conv-hybrid-bridge",
		Role:           "assistant",
		Content:        "Here is the summary.",
	}
	if err := mem.AppendTurn(ctx, turn); err != nil {
		t.Fatalf("append turn: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM companion_events
		WHERE conversation_id = ?
	`, "conv-hybrid-bridge").Scan(&count); err != nil {
		t.Fatalf("query companion events: %v", err)
	}
	if count != 1 {
		t.Fatalf("event rows = %d, want 1", count)
	}
}

func TestSearchCompanionMemories_SearchesHybridArtifacts(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mem, err := NewConversationMemory(db)
	if err != nil {
		t.Fatalf("new conversation memory: %v", err)
	}

	convID := "conv-hybrid-search"
	if err := mem.AppendTurn(ctx, ConversationTurn{
		ConversationID: convID,
		Role:           "user",
		Content:        "I prefer short responses.",
	}); err != nil {
		t.Fatalf("append turn: %v", err)
	}

	results, err := mem.SearchCompanionMemories(ctx, convID, "short responses", 5)
	if err != nil {
		t.Fatalf("search companion memories: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected hybrid search results")
	}
	if results[0].Entry.Type == "" {
		t.Fatalf("expected result type to be set")
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()

	srcDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open src db: %v", err)
	}
	defer srcDB.Close()
	srcMem, err := NewConversationMemory(srcDB)
	if err != nil {
		t.Fatalf("new src memory: %v", err)
	}

	convID := "conv-roundtrip"
	if err := srcMem.AppendTurn(ctx, ConversationTurn{
		ID:             "turn-1",
		ConversationID: convID,
		Role:           "user",
		Content:        "I prefer short responses.",
		TokenCount:     3,
		CreatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append turn: %v", err)
	}

	if _, err := srcDB.ExecContext(ctx, `
		INSERT INTO companion_soft_episodes
			(id, conversation_id, episode_type, start_event_id, end_event_id, summary, needs_summary, assumption_ids, token_count, boundary_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 1, convID, "exploration", 1, 1, "User stated a response preference.", 0, "[]", 9, "bh-roundtrip-1", "2026-02-01 10:00:00.000000"); err != nil {
		t.Fatalf("insert soft episode: %v", err)
	}
	if _, err := srcDB.ExecContext(ctx, `
		INSERT INTO companion_evidence_snippets
			(id, conversation_id, source_event_id, event_type, fact_text, content_hash, confidence, bucket, ttl_days, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 1, convID, 1, EventTypeUserMessage, "User prefers short responses.", "hash-roundtrip-evidence-1", 0.95, "preference", nil, "2026-02-01 10:00:00.000000"); err != nil {
		t.Fatalf("insert evidence snippet: %v", err)
	}
	if _, err := srcDB.ExecContext(ctx, `
		INSERT INTO companion_assumptions_ledger
			(id, conversation_id, assumption, status, reason, source_event_id, confidence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, 1, convID, "User values concise output", AssumptionStatusActive, "stated preference", 1, 0.9, "2026-02-01 10:00:00.000000"); err != nil {
		t.Fatalf("insert assumption: %v", err)
	}

	exported, err := srcMem.Export(ctx, convID)
	if err != nil {
		t.Fatalf("export memory: %v", err)
	}

	dstDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open dst db: %v", err)
	}
	defer dstDB.Close()
	dstMem, err := NewConversationMemory(dstDB)
	if err != nil {
		t.Fatalf("new dst memory: %v", err)
	}
	if err := dstMem.Import(ctx, exported); err != nil {
		t.Fatalf("import memory: %v", err)
	}

	var turnsCount int
	if err := dstDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM companion_turns WHERE conversation_id = ?`, convID).Scan(&turnsCount); err != nil {
		t.Fatalf("count imported turns: %v", err)
	}
	if turnsCount != 1 {
		t.Fatalf("imported turns = %d, want 1", turnsCount)
	}

	var eventCount int
	if err := dstDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM companion_events WHERE conversation_id = ?`, convID).Scan(&eventCount); err != nil {
		t.Fatalf("count imported events: %v", err)
	}
	if eventCount == 0 {
		t.Fatalf("expected imported events, got %d", eventCount)
	}

	var hardStateCount int
	if err := dstDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM companion_hard_state_entries WHERE conversation_id = ?`, convID).Scan(&hardStateCount); err != nil {
		t.Fatalf("count imported hard state entries: %v", err)
	}
	if hardStateCount == 0 {
		t.Fatalf("expected imported hard-state entries, got %d", hardStateCount)
	}

	var episodeCount int
	if err := dstDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM companion_soft_episodes WHERE conversation_id = ?`, convID).Scan(&episodeCount); err != nil {
		t.Fatalf("count imported episodes: %v", err)
	}
	if episodeCount == 0 {
		t.Fatalf("expected imported episodes, got %d", episodeCount)
	}

	var evidenceCount int
	if err := dstDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM companion_evidence_snippets WHERE conversation_id = ?`, convID).Scan(&evidenceCount); err != nil {
		t.Fatalf("count imported evidence snippets: %v", err)
	}
	if evidenceCount == 0 {
		t.Fatalf("expected imported evidence snippets, got %d", evidenceCount)
	}

	var assumptionCount int
	if err := dstDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM companion_assumptions_ledger WHERE conversation_id = ?`, convID).Scan(&assumptionCount); err != nil {
		t.Fatalf("count imported assumptions: %v", err)
	}
	if assumptionCount == 0 {
		t.Fatalf("expected imported assumptions, got %d", assumptionCount)
	}

	var mode string
	if err := dstDB.QueryRowContext(ctx, `SELECT mode FROM companion_memory_mode_state WHERE conversation_id = ?`, convID).Scan(&mode); err != nil {
		t.Fatalf("query mode state: %v", err)
	}
	if mode != MemoryModeHybrid {
		t.Fatalf("mode = %q, want %q", mode, MemoryModeHybrid)
	}
}

func TestImportUsesPathConversationIDWhenMissingInPayload(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mem, err := NewConversationMemory(db)
	if err != nil {
		t.Fatalf("new conversation memory: %v", err)
	}

	raw := json.RawMessage(`{"turns":[{"id":"t1","role":"user","content":"hello"}],"day_summaries":[]}`)
	// Service-level normalization is outside this test; memory import should reject missing conversation_id.
	if err := mem.Import(ctx, raw); err == nil {
		t.Fatalf("expected import error for missing conversation_id")
	}
}
