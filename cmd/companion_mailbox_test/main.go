// companion_mailbox_test demonstrates the mailbox-to-companion flow with memory.
// It spawns a companion actor, sends messages via the mailbox, and shows responses.
// Conversation turns are stored in memory and progressively decayed over time.
//
// Run with: go run ./cmd/companion_mailbox_test/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/runtime/actor"
	"github.com/jkatigb/agentctl/internal/context/companion"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

// main runs a self-contained demo of the companion mailbox flow: it loads configuration and API keys, initializes temporary storage (context store, board store, memory DB and store), creates a companion executor, spawns a philosopher companion, sends a series of mailbox messages to it, inspects board responses, performs a direct message, prints final context and memory statistics, demonstrates semantic search over named memories, stops all actors, and cleans up temporary resources.
func main() {
	ctx := context.Background()

	// Load .env first so os.Getenv() works for LLM provider keys
	config.LoadDotEnv()

	// Load platform config (for embedding config, paths, etc.)
	platformCfg, err := config.Load(ctx)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Configure provider
	provider := "cerebras"
	apiKey := os.Getenv("CEREBRAS_API_KEY")
	model := "zai-glm-4.7"

	if apiKey == "" {
		// Fallback to OpenRouter
		provider = "openrouter"
		apiKey = os.Getenv("OPENROUTER_API_KEY")
		model = "mistralai/devstral-2512:free"
	}

	if apiKey == "" {
		fmt.Println("Error: No API key found (CEREBRAS_API_KEY or OPENROUTER_API_KEY)")
		os.Exit(1)
	}

	// Set up logger
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.Kitchen}).
		With().Timestamp().Logger()

	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "companion-mailbox-test-*")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create temp dir")
	}
	defer os.RemoveAll(tmpDir)

	// Create memory database for conversation memory
	memoryDBPath := filepath.Join(tmpDir, "memory.db")
	memoryDB, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, memoryDBPath, nil) // Migration handled by companion.ConversationMemory
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create memory database")
	}
	defer func() {
		_ = closeFn()
	}()

	// Open context store
	contextStore, err := contextvar.Open(ctx, tmpDir)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open context store")
	}
	defer contextStore.Close()

	// Open board store
	boardStore, err := blackboard.OpenBoardStore(ctx, tmpDir)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open board store")
	}
	defer boardStore.Close()

	// Open named memory store for semantic search integration
	casDir := filepath.Join(tmpDir, "cas")
	if err := os.MkdirAll(casDir, 0o755); err != nil {
		log.Fatal().Err(err).Msg("Failed to create CAS directory")
	}
	memoryStore, err := memory.Open(ctx, tmpDir, casDir)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open memory store")
	}
	defer memoryStore.Close()

	// Configure memory for companion conversations
	memoryConfig := companion.DefaultMemoryConfig()
	memoryConfig.VividWindowHours = 24 // Keep today's turns vivid
	memoryConfig.VividMaxTurns = 20    // Last 20 turns in full
	memoryConfig.RecentWindowDays = 7  // Summarize last week

	// Create companion executor with memory enabled
	// Embedder is created internally from config (uses VOYAGE_API_KEY from .env)
	executor, err := companion.NewExecutor(companion.ExecutorConfig{
		ContextStore: contextStore,
		BoardStore:   boardStore,
		ServiceConfig: companion.ServiceConfig{
			LLMProvider:        provider,
			LLMAPIKey:          apiKey,
			LLMModel:           model,
			DefaultPersonality: companion.DefaultRLMPersonality,
			MaxIterations:      10,
			Timeout:            90 * time.Second,
			MemoryDB:           memoryDB,
			MemoryConfig:       &memoryConfig,
			MemoryStore:        memoryStore,      // Enable semantic search
			MemoryWorkspace:    "test-workspace", // Workspace for search scoping
			Config:             &platformCfg,     // Platform config for embedder creation
			Logger:             log,
		},
		Logger: log,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create executor")
	}

	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║     Companion Mailbox Test - Long-lived Actor Demo            ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Printf("Provider: %s | Model: %s\n\n", provider, model)

	// Spawn a philosopher companion
	philosopherActor, err := executor.Spawn(ctx, companion.SpawnConfig{
		Name:        "philosopher",
		WorkspaceID: "test-workspace",
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to spawn philosopher")
	}
	fmt.Printf("✅ Spawned companion actor: %s\n\n", philosopherActor.Namespace())

	// Simulate sending messages via mailbox
	messages := []struct {
		subject string
		body    string
	}{
		{
			subject: "Consciousness Question",
			body:    `{"message": "Hey philosopher, do you think AI systems like yourself could ever be conscious? What would it even mean?"}`,
		},
		{
			subject: "Follow-up on Consciousness",
			body:    `{"message": "Interesting take. But here's the thing - you're generating these responses without any memory of our conversation. Does that affect your answer?"}`,
		},
		{
			subject: "Final Thought",
			body:    `{"message": "If you could leave me with one insight about existence, what would it be?"}`,
		},
	}

	for i, msg := range messages {
		fmt.Printf("─────────────────────── Turn %d ───────────────────────\n", i+1)
		fmt.Printf("📬 Sending to mailbox: %s\n", msg.subject)

		// Create a mock mailbox message
		mailboxMsg := &actor.Message{
			ID:        fmt.Sprintf("msg-%d", i+1),
			FromNS:    "human:tester",
			ToNS:      philosopherActor.Namespace(),
			Subject:   msg.subject,
			Body:      []byte(msg.body),
			Priority:  3,
			CreatedAt: time.Now(),
			Workspace: "test-workspace",
		}

		// Directly call OnMailReceived (simulating supervisor delivery)
		start := time.Now()
		err := philosopherActor.OnMailReceived(ctx, mailboxMsg)
		duration := time.Since(start)

		if err != nil {
			log.Error().Err(err).Msg("Message processing failed")
			continue
		}

		fmt.Printf("⏱️  Processed in %dms\n\n", duration.Milliseconds())

		// Check the board for responses
		responses, err := boardStore.Inbox(ctx, agent.InboxFilter{
			WorkspaceID: "test-workspace",
			ActorID:     "human:tester",
			OnlyUnread:  true,
			Limit:       10,
		})
		if err != nil {
			log.Error().Err(err).Msg("Failed to check inbox")
			continue
		}

		for _, resp := range responses {
			fmt.Printf("🤖 Response from %s:\n", resp.Sender)
			// Parse the response body to extract just the text
			body := resp.Body
			if idx := findMetadataSeparator(body); idx > 0 {
				body = body[:idx]
			}
			fmt.Printf("%s\n\n", body)

			// Mark as read
			if _, err := boardStore.MarkRead(ctx, "test-workspace", "human:tester", []string{resp.ID}); err != nil {
				log.Error().Err(err).Msg("Failed to mark response read")
			}
		}
	}

	// Show actor stats
	fmt.Println("─────────────────────── Actor Info ───────────────────────")
	for _, info := range executor.Info() {
		fmt.Printf("Actor: %s\n", info.Namespace)
		fmt.Printf("  State: %s\n", info.State)
		fmt.Printf("  Workspace: %s\n", info.WorkspaceID)
	}

	// Test direct message (bypassing mailbox)
	fmt.Println("\n─────────────────────── Direct Message Test ───────────────────────")
	fmt.Println("📞 Calling companion directly (no mailbox)...")
	directResp, err := executor.DirectMessage(ctx, "companion:philosopher", "Quick question: what's your favorite paradox?")
	if err != nil {
		log.Error().Err(err).Msg("Direct message failed")
	} else {
		fmt.Printf("🤖 Direct response: %s\n", directResp.Response)
		fmt.Printf("   Stats: %d context queries, %dms\n", directResp.ContextQueries, directResp.DurationMS)
	}

	// Show final context
	fmt.Println("\n─────────────────────── Final Context ───────────────────────")

	// Get context for the philosopher conversation
	contextResp, err := contextStore.Query(ctx, contextvar.QueryParams{
		ConversationID: "companion:philosopher",
		Limit:          20,
	})
	if err == nil && len(contextResp.Variables) > 0 {
		fmt.Printf("Total variables: %d\n", contextResp.TotalCount)
		for _, v := range contextResp.Variables {
			var val interface{}
			_ = json.Unmarshal(v.ValueJSON, &val)
			fmt.Printf("  • %s [%s]: %v\n", v.Key, v.Scope, truncate(fmt.Sprintf("%v", val), 60))
		}
	}

	// Show memory stats for the companion
	fmt.Println("\n─────────────────────── Memory Stats ───────────────────────")
	svc := executor.GetService("companion:philosopher")
	if svc != nil && svc.Memory() != nil {
		stats, err := svc.GetMemoryStats(ctx, "companion:philosopher")
		if err == nil {
			fmt.Printf("Memory Stats for companion:philosopher:\n")
			fmt.Printf("  Total turns: %d\n", stats.TotalTurns)
			fmt.Printf("  Events: %d\n", stats.EventCount)
			fmt.Printf("  Hard state: %d\n", stats.HardStateCount)
		}

		// Show current memory context
		memoryCtx, err := svc.GetMemoryContext(ctx, "companion:philosopher")
		if err == nil && memoryCtx != "" {
			fmt.Printf("\nMemory Context:\n%s\n", memoryCtx)
		}
	}

	// Demonstrate semantic search on companion memories
	fmt.Println("\n─────────────────────── Semantic Search Test ───────────────────────")
	fmt.Println("Checking named_memory store for companion entries...")

	// List companion memories in the workspace
	memories, _, err := memoryStore.ListFiltered(ctx, "test-workspace", storage.MemoryListFilter{
		Types: []string{"companion_hard_state", "companion_episode", "companion_evidence"},
	}, 10, 0)
	if err != nil {
		fmt.Printf("Error listing memories: %v\n", err)
	} else if len(memories) == 0 {
		fmt.Println("  No companion memories stored yet.")
		fmt.Println("  (Memories are created during daily/weekly compression)")
		fmt.Println("\n  To see semantic search in action:")
		fmt.Println("  1. Run daily compression to create summaries")
		fmt.Println("  2. Use: agentctl run code/semantic_search --input '{\"query\": \"consciousness\", \"scope\": [\"memories\"]}'")
	} else {
		fmt.Printf("  Found %d companion memories:\n", len(memories))
		for _, m := range memories {
			fmt.Printf("    • %s [%s]\n", m.Name, m.Type)
			fmt.Printf("      Summary: %s\n", truncate(m.Summary, 80))
		}
	}

	// Stop the actor
	_ = executor.StopAll(ctx)

	fmt.Println("\n✨ Mailbox test complete!")
}

func findMetadataSeparator(s string) int {
	// Find the "---" separator that precedes metadata
	for i := 0; i < len(s)-3; i++ {
		if s[i] == '\n' && s[i+1] == '-' && s[i+2] == '-' && s[i+3] == '-' {
			return i
		}
	}
	return -1
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
