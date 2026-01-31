// companion_memory_test demonstrates the conversation memory system with temporal decay.
// Shows L0 (vivid) → L1 (summarized) → L2 (distilled) progression.
//
// Run with: go run ./cmd/companion_memory_test/
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

// main demonstrates a runnable Companion conversation-memory temporal-decay demo.
// It sets up a temporary SQLite database and filesystem-backed memory store, injects
// sample yesterday and today turns, runs daily compression (L0 → L1) with a mock
// summarizer, optionally enables vector embeddings from configuration, prints memory
// statistics and contexts, exports the full memory state, and lists any stored
// semantic memories. The program uses temporary files and directories that are
// cleaned up on exit.
func main() {
	ctx := context.Background()

	// Load platform config (reads .env, includes VOYAGE_API_KEY)
	platformCfg, err := config.Load(ctx)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Create temp DB
	tmpDir, err := os.MkdirTemp("", "companion-memory-test-*")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "memory.db")
	db, closeFn, err := sqliteutil.OpenDBShared(ctx, dbPath, nil) // Migration handled by ConversationMemory
	if err != nil {
		fmt.Printf("Failed to create database: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = closeFn()
	}()

	// Create named memory store for semantic search integration
	casDir := filepath.Join(tmpDir, "cas")
	if err := os.MkdirAll(casDir, 0o755); err != nil {
		fmt.Printf("Failed to create CAS directory: %v\n", err)
		os.Exit(1)
	}
	memoryStore, err := memory.Open(ctx, tmpDir, casDir)
	if err != nil {
		fmt.Printf("Failed to create memory store: %v\n", err)
		os.Exit(1)
	}
	defer memoryStore.Close()

	// Create memory store with custom config for testing
	cfg := companion.DefaultMemoryConfig()
	cfg.VividWindowHours = 1 // 1 hour for testing
	cfg.VividMaxTurns = 5    // Keep last 5 turns vivid
	cfg.RecentWindowDays = 1 // 1 day for testing

	companionMemory, err := companion.NewConversationMemory(db,
		companion.WithMemoryConfig(cfg),
		companion.WithMemoryStore(memoryStore, "test-workspace"),
	)
	if err != nil {
		fmt.Printf("Failed to create memory: %v\n", err)
		os.Exit(1)
	}

	convID := "test-conversation"

	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║     Companion Memory Test - Temporal Decay Demo               ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")

	// Simulate a conversation over time
	fmt.Println("\n━━━ Simulating conversation over time ━━━")

	// Add some "old" turns (from yesterday) by manipulating timestamps
	yesterday := time.Now().AddDate(0, 0, -1)
	oldTurns := []struct {
		role    string
		content string
	}{
		{"user", "Hey! I've been thinking about learning guitar lately."},
		{"assistant", "That's exciting! Have you played any instruments before?"},
		{"user", "I played piano as a kid but stopped. Guitar seems more portable."},
		{"assistant", "Piano background will help with music theory. What kind of music do you want to play?"},
		{"user", "Mostly acoustic stuff - folk and indie. Maybe some fingerpicking."},
		{"assistant", "Great choices! Fingerpicking is beautiful. I'd recommend starting with simple patterns."},
	}

	for i, turn := range oldTurns {
		timestamp := yesterday.Add(time.Duration(i) * 10 * time.Minute)
		err := companionMemory.AppendTurn(ctx, companion.ConversationTurn{
			ConversationID: convID,
			Role:           turn.role,
			Content:        turn.content,
			CreatedAt:      timestamp,
		})
		if err != nil {
			fmt.Printf("Failed to append old turn: %v\n", err)
		}
	}
	fmt.Printf("✅ Added %d turns from yesterday\n", len(oldTurns))

	// Add "today's" turns
	todayTurns := []struct {
		role    string
		content string
	}{
		{"user", "I actually bought a guitar yesterday!"},
		{"assistant", "That's wonderful! What kind did you get?"},
		{"user", "A Yamaha FG800. The reviews said it's good for beginners."},
		{"assistant", "Excellent choice! The FG800 has a nice warm tone. Have you tried playing anything yet?"},
	}

	for _, turn := range todayTurns {
		err := companionMemory.AppendTurn(ctx, companion.ConversationTurn{
			ConversationID: convID,
			Role:           turn.role,
			Content:        turn.content,
			CreatedAt:      time.Now(),
		})
		if err != nil {
			fmt.Printf("Failed to append today's turn: %v\n", err)
		}
	}
	fmt.Printf("✅ Added %d turns from today\n", len(todayTurns))

	// Show stats
	stats, _ := companionMemory.GetStats(ctx, convID)
	fmt.Printf("\n📊 Memory Stats:\n")
	fmt.Printf("   Total turns: %d\n", stats.TotalTurns)
	fmt.Printf("   Day summaries: %d\n", stats.DaySummaries)
	fmt.Printf("   Has distilled history: %v\n", stats.HasDistilledHistory)

	// Get current context (L0 only - no compression yet)
	fmt.Println("\n━━━ Current Context (L0 - Vivid) ━━━")
	context1, _ := companionMemory.GetContext(ctx, convID)
	if context1 != "" {
		fmt.Println(context1)
	} else {
		fmt.Println("(no vivid context - old turns are outside window)")
	}

	// Now simulate running daily compression
	fmt.Println("\n━━━ Running Daily Compression (L0 → L1) ━━━")

	// For testing, we need a summarizer. Create a mock one.
	mockSummarizer := &mockSummarizer{}

	// Try to create an embedder for vector search (uses VOYAGE_API_KEY from config/.env)
	var embedder *semantic.Embedder
	embedder, err = semantic.NewEmbedderFromConfig(semantic.ScopeMemory, platformCfg)
	if err != nil {
		fmt.Printf("⚠️  Could not create embedder: %v\n", err)
		fmt.Println("   Summaries will be stored but NOT vector searchable")
		embedder = nil // Ensure nil for later checks
	} else {
		fmt.Println("✅ Embedder created - vector search will be enabled")
	}

	// Create memory with all options
	memOpts := []companion.MemoryOption{
		companion.WithMemoryConfig(cfg),
		companion.WithSummarizer(mockSummarizer),
		companion.WithMemoryStore(memoryStore, "test-workspace"), // Enable semantic search storage
	}
	if embedder != nil {
		memOpts = append(memOpts, companion.WithEmbedder(embedder))
	}
	memoryWithSummarizer, err := companion.NewConversationMemory(db, memOpts...)
	if err != nil {
		fmt.Printf("Failed to create memory with summarizer: %v\n", err)
		os.Exit(1)
	}

	if err := memoryWithSummarizer.RunDailyCompression(ctx, convID); err != nil {
		fmt.Printf("Daily compression error: %v\n", err)
	} else {
		fmt.Println("✅ Daily compression complete")
	}

	// Show updated stats
	stats, _ = memoryWithSummarizer.GetStats(ctx, convID)
	fmt.Printf("\n📊 Updated Stats:\n")
	fmt.Printf("   Total turns: %d\n", stats.TotalTurns)
	fmt.Printf("   Day summaries: %d\n", stats.DaySummaries)
	fmt.Printf("   Last summarized: %s\n", stats.LastSummarizedDate)

	// Get context with summaries
	fmt.Println("\n━━━ Context with Memory (L0 + L1) ━━━")
	context2, _ := memoryWithSummarizer.GetContext(ctx, convID)
	if context2 != "" {
		fmt.Println(context2)
	}

	// Export full memory state
	fmt.Println("\n━━━ Exported Memory State ━━━")
	export, _ := memoryWithSummarizer.Export(ctx, convID)
	fmt.Println(string(export))

	// Check what was stored in named_memory for semantic search
	fmt.Println("\n━━━ Semantic Search Storage ━━━")
	memories, _, err := memoryStore.ListFiltered(ctx, "test-workspace", storage.MemoryListFilter{
		Types: []string{"companion_summary", "companion_history"},
	}, 10, 0)
	if err != nil {
		fmt.Printf("Error listing memories: %v\n", err)
	} else if len(memories) == 0 {
		fmt.Println("No companion memories stored in named_memory table")
	} else {
		fmt.Printf("Found %d searchable companion memories:\n", len(memories))
		for _, m := range memories {
			fmt.Printf("  • %s [%s]\n", m.Name, m.Type)
			fmt.Printf("    Summary: %s\n", truncate(m.Summary, 80))
			fmt.Printf("    Session (ConversationID): %s\n", m.SessionID)
		}
		if embedder != nil {
			fmt.Println("\n📌 Vector embeddings were generated! These memories are now searchable via:")
			fmt.Println("   agentctl run code/semantic_search --input '{\"query\": \"hobbies\", \"scope\": [\"memories\"]}'")
			fmt.Println("   agentctl run code/semantic_search --input '{\"query\": \"guitar learning\", \"scope\": [\"memories\"]}'")
		} else {
			fmt.Println("\n⚠️  No embeddings generated. Set VOYAGE_API_KEY to enable vector search.")
			fmt.Println("   Without embeddings, only text search (not semantic/vector) will work.")
		}
	}

	fmt.Println("\n✨ Memory test complete!")
	fmt.Println("\nArchitecture summary:")
	fmt.Println("  • L0 (Vivid): Today's turns in full - natural conversation flow")
	fmt.Println("  • L1 (Recent): Yesterday summarized - \"we talked about guitar\"")
	fmt.Println("  • L2 (History): Older weeks distilled - relationship, preferences, key moments")
	fmt.Println("  • Semantic: Summaries stored in named_memory for vector search")
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// mockSummarizer provides simple summaries without LLM for testing
type mockSummarizer struct{}

func (m *mockSummarizer) SummarizeDay(ctx context.Context, turns []companion.ConversationTurn) (*companion.DaySummary, error) {
	// Count user turns for the summary
	userTurnCount := 0
	for _, t := range turns {
		if t.Role == "user" {
			userTurnCount++
		}
	}

	return &companion.DaySummary{
		Summary:    fmt.Sprintf("Discussed %d topics. User seemed interested in learning guitar and music.", userTurnCount),
		Topics:     []string{"guitar", "music", "learning"},
		Mood:       "enthusiastic and curious",
		KeyMoments: []string{"User mentioned wanting to learn fingerpicking"},
		TokenCount: 50,
	}, nil
}

func (m *mockSummarizer) DistillHistory(ctx context.Context, existing *companion.DistilledHistory, summaries []companion.DaySummary) (*companion.DistilledHistory, error) {
	history := &companion.DistilledHistory{
		RelationshipNote: "A friendly conversation about learning music and pursuing hobbies",
		RecurringTopics:  []string{"guitar", "music", "hobbies"},
		UserPreferences:  []string{"acoustic music", "folk and indie", "portable instruments"},
		SharedMemories:   []string{"Discussed their piano background", "Excited about new Yamaha guitar"},
		TokenCount:       100,
	}

	// Merge with existing if present
	if existing != nil && len(existing.RecurringTopics) > 0 {
		history.RecurringTopics = append(existing.RecurringTopics, history.RecurringTopics...)
	}

	return history, nil
}