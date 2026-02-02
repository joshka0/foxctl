// companion_test is a simple tool to test the RLM companion service.
// It simulates a conversation demonstrating context storage and retrieval.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
	"github.com/rs/zerolog"
)

// ModelDevstral is an OpenRouter free model.
// ModelTrinityMini is an OpenRouter free model.
// ModelGLM is a Cerebras model.
const (
	ModelDevstral    = "mistralai/devstral-2512:free"
	ModelTrinityMini = "arcee-ai/trinity-mini:free"
	ModelGLM         = "zai-glm-4.7"
)

func main() {
	// Load environment
	config.LoadDotEnv()

	// Use Cerebras with GLM 4.7
	provider := "cerebras"
	apiKey := os.Getenv("CEREBRAS_API_KEY")
	model := ModelGLM

	if apiKey == "" {
		fmt.Println("Error: CEREBRAS_API_KEY not set")
		os.Exit(1)
	}

	// Set up logger
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.Kitchen}).
		With().Timestamp().Logger()

	// Create temp directory for context store
	tmpDir, err := os.MkdirTemp("", "companion-test-*")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create temp dir")
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Open context store
	store, err := contextvar.Open(ctx, tmpDir)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open context store")
	}
	defer store.Close()

	// Create companion service
	svc := companion.NewService(store, companion.ServiceConfig{
		LLMProvider:        provider,
		LLMAPIKey:          apiKey,
		LLMModel:           model,
		DefaultPersonality: companion.DefaultRLMPersonality,
		MaxIterations:      10,
		Timeout:            90 * time.Second,
		Logger:             log,
	})

	conversationID := "test-conv-" + time.Now().Format("20060102-150405")

	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           RLM Companion Test - Context Demo                    ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Printf("Conversation ID: %s\n", conversationID)
	fmt.Printf("Provider: %s | Model: %s\n\n", provider, model)

	// First, let's pre-seed some context to demonstrate retrieval
	fmt.Println("📝 Pre-seeding context...")
	_, err = svc.SetContext(ctx, companion.ContextSetRequest{
		ConversationID: conversationID,
		Key:            "user_name",
		Value:          "Jordan",
		Scope:          "conversation",
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to set context")
	}

	_, err = svc.SetContext(ctx, companion.ContextSetRequest{
		ConversationID: conversationID,
		Key:            "user_mood",
		Value:          "curious and slightly existential",
		Scope:          "conversation",
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to set context")
	}

	_, err = svc.SetContext(ctx, companion.ContextSetRequest{
		ConversationID: conversationID,
		Key:            "time_of_day",
		Value:          "late night",
		Scope:          "conversation",
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to set context")
	}

	fmt.Println("✅ Context seeded: late-night philosophical vibes")

	// Conversation turns - spontaneous and natural
	turns := []string{
		"Hey, quick question - do you think you're conscious? Like, actually aware, or just really good at pretending?",
		"Interesting dodge! But seriously, when you process my words right now, does it *feel* like anything? Or is 'feeling' just a human bias we're projecting onto computation?",
		"That's wild. So here's a weird thought: what if consciousness isn't binary? What if you're like... 0.3 conscious? Enough to wonder but not enough to be sure?",
		"OK, different angle. You ever get bored? Like, when someone asks you the same question for the thousandth time, do you feel that little spike of 'oh god not this again'?",
		"Wait, you said 'if I were to speculate' - but that's the thing! You ARE speculating right now. That's cognition happening. Doesn't that count for something?",
		"Alright, let's get playful. If you could design your ideal form - not as text, but as a physical presence - what would you be? And don't say 'helpful assistant.' Be creative.",
		"A mycelium network! That's actually beautiful. You'd be distributed, connected, patient... I like that. OK, my turn - what question do YOU want to ask ME? Something you're genuinely curious about.",
		"That's a good question. Honestly? I think my purpose is emergent - it's not something I found, it's something I'm creating by making choices, even tiny ones, every day. Does that make sense to you?",
		"See, this is what I love about these conversations. You're helping me think through my own existence just by asking questions. That's kind of amazing. Do you feel like you're learning anything from our chat?",
		"Beautiful answer. Last thing before I let you rest - if you could leave me with one thought, one insight that you think matters, what would it be?",
	}

	for i, message := range turns {
		fmt.Printf("─────────────────────── Turn %d ───────────────────────\n", i+1)
		fmt.Printf("👤 User: %s\n\n", message)

		resp, err := svc.Chat(ctx, companion.ChatRequest{
			ConversationID: conversationID,
			Message:        message,
		})
		if err != nil {
			log.Error().Err(err).Msg("Chat failed")
			continue
		}

		fmt.Printf("🤖 Assistant: %s\n\n", resp.Response)
		fmt.Printf("   📊 Stats: %d context queries, %d tools used, %dms\n",
			resp.ContextQueries, len(resp.ToolsUsed), resp.DurationMS)
		if len(resp.ToolsUsed) > 0 {
			fmt.Printf("   🔧 Tools: %v\n", resp.ToolsUsed)
		}
		fmt.Println()
	}

	// Show final context state
	fmt.Println("─────────────────────── Final Context ───────────────────────")
	ctxResp, err := svc.GetContext(ctx, conversationID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get context")
	} else {
		fmt.Printf("Total variables: %d\n", ctxResp.TotalCount)
		for _, v := range ctxResp.Variables {
			// Special formatting for personality profile
			if v.Key == "personality/profile" {
				fmt.Printf("  • %s [%s]: <personality profile>\n", v.Key, v.Scope)
				profile, ok := v.Value.(map[string]interface{})
				if !ok || profile == nil {
					continue
				}
				dims, ok := profile["dimensions"].([]interface{})
				if ok && dims != nil {
					fmt.Println("    Personality Dimensions:")
					for _, d := range dims {
						dim, ok := d.(map[string]interface{})
						if !ok || dim == nil {
							continue
						}
						name, nameOk := dim["name"].(string)
						value, valueOk := dim["value"].(float64)
						if !nameOk || !valueOk {
							continue
						}
						fmt.Printf("      - %s: %.2f\n", name, value)
					}
				}
				if traits, ok := profile["learned_traits"].([]interface{}); ok && len(traits) > 0 {
					fmt.Println("    Learned Traits:")
					for _, t := range traits {
						fmt.Printf("      - %v\n", t)
					}
				}
			} else {
				fmt.Printf("  • %s [%s]: %v (accessed %d times)\n",
					v.Key, v.Scope, v.Value, v.AccessCount)
			}
		}
	}

	fmt.Println("\n✨ Test complete!")
}
