package companion

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	actormemory "github.com/jkatigb/agentctl/internal/actor/memory"
	_ "modernc.org/sqlite"
)

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

// TestGetContextIncludesHistoryWithoutRelationshipNote verifies history renders without requiring a relationship note.
func TestGetContextIncludesHistoryWithoutRelationshipNote(t *testing.T) {
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
	_, err = db.ExecContext(ctx, `
		INSERT INTO companion_history
			(id, conversation_id, relationship_note, recurring_topics, user_preferences, shared_memories, token_count, last_distilled_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "hist-1", convID, "", joinList([]string{"hiking", "travel"}), "", "", 0, time.Now())
	if err != nil {
		t.Fatalf("insert history: %v", err)
	}

	got, err := mem.GetContext(ctx, convID)
	if err != nil {
		t.Fatalf("get context: %v", err)
	}
	if !strings.Contains(got, "## Our History") {
		t.Fatalf("expected history section, got %q", got)
	}
	if !strings.Contains(got, "We often discuss: hiking, travel") {
		t.Fatalf("expected recurring topics in history, got %q", got)
	}
}
