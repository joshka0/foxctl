package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/companion"
	agentdomain "github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
	"github.com/jkatigb/agentctl/internal/storage/conversationsettings"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
)

// CompanionProvidersHandler returns a handler for GET /api/companion/providers.
// It reports which LLM providers have API keys configured, the default provider, and Voyage availability.
func CompanionProvidersHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	type providerAvailability struct {
		ID        string `json:"id"`
		Available bool   `json:"available"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Check provider-specific keys directly. ResolveAPIKey falls back to the
		// generic APIKey which would make every provider look "available".
		// lmstudio is always available (no real API key needed).
		providers := []providerAvailability{
			{ID: "anthropic", Available: cfg.LLM.AnthropicAPIKey != ""},
			{ID: "openai", Available: cfg.LLM.OpenAIAPIKey != ""},
			{ID: "openrouter", Available: cfg.LLM.OpenRouterAPIKey != ""},
			{ID: "groq", Available: cfg.LLM.GroqAPIKey != ""},
			{ID: "gemini", Available: cfg.LLM.GeminiAPIKey != ""},
			{ID: "cerebras", Available: cfg.LLM.CerebrasAPIKey != ""},
			{ID: "lmstudio", Available: true},
			{ID: "bedrock", Available: cfg.LLM.BedrockRegion != ""},
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":               true,
			"providers":        providers,
			"default_provider": cfg.LLM.Provider,
			"voyage_available": cfg.Embedding.VoyageAPIKey != "",
		})
	}
}

// CompanionChatHandler returns an HTTP handler for POST /api/companion/chat that handles chat requests from the companion mobile app.
// The handler validates the request JSON (requiring conversation_id and message), opens the context store and companion memory DB,
// constructs a companion service with memory enabled, invokes the chat operation, and writes the chat response as JSON.
// On invalid input it responds 400, on method mismatch 405, and on internal failures 500.
func CompanionChatHandler(cfg config.Config, log zerolog.Logger, turnLock companion.Locker, skillRunner *SkillRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Parse request
		var req companion.ChatRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		// Validate
		if req.ConversationID == "" {
			httpError(w, http.StatusBadRequest, "conversation_id is required")
			return
		}
		if req.Message == "" {
			httpError(w, http.StatusBadRequest, "message is required")
			return
		}

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Load conversation settings (optional).
		settingsStore, err := conversationsettings.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open conversation settings store")
			httpError(w, http.StatusInternalServerError, "failed to open conversation settings store")
			return
		}
		defer settingsStore.Close()

		settings, err := settingsStore.Get(r.Context(), req.ConversationID)
		if err != nil && !errors.Is(err, conversationsettings.ErrNotFound) {
			log.Error().Err(err).Str("conversation_id", req.ConversationID).Msg("failed to load conversation settings")
			httpError(w, http.StatusInternalServerError, "failed to load conversation settings")
			return
		}

		// Open companion memory database for memory context injection.
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// If no per-turn override was provided, apply conversation defaults.
		if req.LLMProvider == "" && settings.LLMProvider != "" {
			req.LLMProvider = settings.LLMProvider
		}
		if req.LLMModel == "" && settings.LLMModel != "" {
			req.LLMModel = settings.LLMModel
		}
		if req.ExecMode == "" && settings.ExecMode != "" {
			req.ExecMode = agentdomain.ExecutionMode(settings.ExecMode)
		}
		if req.StoryGatherModel == "" && settings.StoryGatherModel != "" {
			req.StoryGatherModel = settings.StoryGatherModel
		}
		if req.StoryDialogueModel == "" && settings.StoryDialogueModel != "" {
			req.StoryDialogueModel = settings.StoryDialogueModel
		}

		// If still unset, fall back to linked agent defaults (conversation_id -> agent_id).
		// Precedence: per-turn overrides > conversation settings > linked agent > global config.
		var linkedAgentID string
		if rowErr := memoryDB.QueryRowContext(r.Context(), `
			SELECT agent_id FROM companion_conversation_titles WHERE conversation_id = ?
		`, req.ConversationID).Scan(&linkedAgentID); rowErr == nil {
			linkedAgentID = strings.TrimSpace(linkedAgentID)
		} else if !errors.Is(rowErr, sql.ErrNoRows) {
			log.Debug().Err(rowErr).Str("conversation_id", req.ConversationID).Msg("failed to query linked agent id")
		}

		var agentPrompt string
		if linkedAgentID != "" {
			agentStore, err := agents.Open(r.Context(), cfg.Storage.Root)
			if err != nil {
				log.Error().Err(err).Msg("failed to open agents store")
				httpError(w, http.StatusInternalServerError, "failed to open agents store")
				return
			}
			defer agentStore.Close()

			a, err := agentStore.Get(r.Context(), linkedAgentID)
			if err == nil {
				if req.LLMProvider == "" && strings.TrimSpace(a.LLMProvider) != "" {
					req.LLMProvider = strings.TrimSpace(a.LLMProvider)
				}
				if req.LLMModel == "" && strings.TrimSpace(a.LLMModel) != "" {
					req.LLMModel = strings.TrimSpace(a.LLMModel)
				}
				if req.ExecMode == "" && a.ExecMode != "" {
					req.ExecMode = a.ExecMode
				}
				agentPrompt = strings.TrimSpace(a.Prompt)
			}
		}

		// Resolve LLM credentials. The client may override provider/model for this request
		// (for example to select an OpenRouter model), but API keys are always resolved
		// server-side from config/env.
		llmProvider := strings.TrimSpace(req.LLMProvider)
		if llmProvider == "" {
			llmProvider = cfg.LLM.Provider
		}
		llmAPIKey := ""
		llmModel := strings.TrimSpace(req.LLMModel)
		if llmProvider != "" {
			llmAPIKey = cfg.LLM.ResolveAPIKey(llmProvider)
			if llmModel == "" {
				llmModel = cfg.LLM.ResolveModel(llmProvider)
			}
		}

		// Create service with memory enabled and LLM credentials.
		// The shared turnLock ensures per-conversation mutual exclusion
		// across all HTTP requests (not just within a single Service instance).
		presenceEnabled := settings.IsPresenceEnabled()
		svcCfg := companion.ServiceConfig{
			Logger:          log,
			MemoryDB:        memoryDB,
			LLMProvider:     llmProvider,
			LLMAPIKey:       llmAPIKey,
			LLMModel:        llmModel,
			ToolsAllow:      settings.ToolsAllow,
			UseHybridMemory: true,
		}
		if presenceEnabled {
			svcCfg.PresenceConfig = &companion.PresenceConfig{
				Enabled: true,
			}
			if skillRunner != nil {
				svcCfg.SkillRunner = &companionSkillRunnerAdapter{
					inner: skillRunner,
				}
			}
		}
		if agentPrompt != "" {
			svcCfg.DefaultPersonality = agentPrompt
		}
		svc := companion.NewService(store, svcCfg, turnLock)

		// Execute chat
		resp, err := svc.Chat(r.Context(), req)
		if err != nil {
			log.Error().Err(err).Msg("chat failed")
			httpError(w, http.StatusInternalServerError, "chat failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// CompanionContextSetHandler returns a handler for POST /api/companion/context.
// (for example when opening the context store or setting the context).
func CompanionContextSetHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Parse request
		var req companion.ContextSetRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		// Validate
		if req.ConversationID == "" {
			httpError(w, http.StatusBadRequest, "conversation_id is required")
			return
		}
		if req.Key == "" {
			httpError(w, http.StatusBadRequest, "key is required")
			return
		}

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Create service
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger: log,
		}, nil)

		// Set context
		resp, err := svc.SetContext(r.Context(), req)
		if err != nil {
			log.Error().Err(err).Msg("set context failed")
			httpError(w, http.StatusInternalServerError, "set context failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// CompanionContextGetHandler returns a handler for GET /api/companion/context/:conversation_id.
// Gets all context variables for a conversation.
func CompanionContextGetHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path
		// Expected path: /api/companion/context/{conversation_id}
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/context/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			httpError(w, http.StatusBadRequest, "conversation_id is required in path")
			return
		}
		conversationID := parts[0]

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Open companion memory database so context reads can include hybrid metadata.
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		svcCfg := companion.ServiceConfig{Logger: log}
		if memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil); err == nil {
			defer func() { _ = closeFn() }()
			svcCfg.MemoryDB = memoryDB
			svcCfg.UseHybridMemory = true
		} else {
			log.Debug().Err(err).Msg("failed to open companion memory database; returning legacy context")
		}

		// Create service
		svc := companion.NewService(store, svcCfg, nil)

		// Get context
		resp, err := svc.GetContext(r.Context(), conversationID)
		if err != nil {
			log.Error().Err(err).Msg("get context failed")
			httpError(w, http.StatusInternalServerError, "get context failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// CompanionContextDeleteHandler returns a handler for DELETE /api/companion/context/:conversation_id/:key.
// Deletes a context variable.
func CompanionContextDeleteHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id and key from path
		// Expected path: /api/companion/context/{conversation_id}/{key}
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/context/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			httpError(w, http.StatusBadRequest, "conversation_id and key are required in path")
			return
		}
		conversationID := parts[0]
		key := strings.Join(parts[1:], "/") // Key can contain slashes

		// Get scope from query param (defaults to conversation)
		scope := r.URL.Query().Get("scope")
		if scope == "" {
			scope = "conversation"
		}

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Create service
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger: log,
		}, nil)

		// Delete context
		err = svc.DeleteContext(r.Context(), conversationID, key, scope)
		if err != nil {
			if errors.Is(err, contextvar.ErrNotFound) {
				httpError(w, http.StatusNotFound, "context variable not found")
				return
			}
			log.Error().Err(err).Msg("delete context failed")
			httpError(w, http.StatusInternalServerError, "delete context failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, SuccessResponse{OK: true, Message: "deleted"})
	}
}

// CompanionContextClearHandler returns a handler for DELETE /api/companion/context/:conversation_id.
// Clears all context for a conversation.
func CompanionContextClearHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/context/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			httpError(w, http.StatusBadRequest, "conversation_id is required in path")
			return
		}

		// Only handle if this is exactly the conversation path (not /conversation_id/key)
		if len(parts) > 1 && parts[1] != "" {
			// This is a key-specific delete, let the other handler handle it
			httpError(w, http.StatusBadRequest, "use DELETE /api/companion/context/:id/:key for key deletion")
			return
		}

		conversationID := parts[0]

		// Check for clear=true query param to confirm
		if r.URL.Query().Get("clear") != "true" {
			httpError(w, http.StatusBadRequest, "add ?clear=true to confirm clearing all context")
			return
		}

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Create service
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger: log,
		}, nil)

		// Clear conversation
		count, err := svc.ClearConversation(r.Context(), conversationID)
		if err != nil {
			log.Error().Err(err).Msg("clear conversation failed")
			httpError(w, http.StatusInternalServerError, "clear conversation failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":            true,
			"deleted_count": count,
		})
	}
}

// CompanionConversationsHandler returns a handler for GET /api/companion/conversations.
// It responds with a JSON object containing a "conversations" field on success and maps failures to appropriate HTTP error responses.
func CompanionConversationsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Parse limit from query
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
				limit = parsed
			}
		}

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Open companion memory database
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		}, nil)

		// List conversations
		conversations, err := svc.ListConversations(r.Context(), limit)
		if err != nil {
			log.Error().Err(err).Msg("list conversations failed")
			httpError(w, http.StatusInternalServerError, "list conversations failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"conversations": conversations,
		})
	}
}

// CompanionConversationMessagesHandler returns a handler for GET /api/companion/conversations/:id/messages.
// CompanionConversationMessagesHandler returns an HTTP handler for GET /api/companion/conversations/:id/messages that retrieves messages for the specified conversation.
//
// The handler accepts an optional `limit` query parameter (default 100) to bound the number of messages returned and responds with a JSON object containing
// `conversation_id`, `messages`, and `count`. It validates the request path and method and uses the configured context store and companion memory DB.
func CompanionConversationMessagesHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path: /api/companion/conversations/:id/messages
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/conversations/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] != "messages" {
			httpError(w, http.StatusBadRequest, "invalid path, expected /api/companion/conversations/:id/messages")
			return
		}
		conversationID := parts[0]

		// Parse limit from query
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
				limit = parsed
			}
		}

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Open companion memory database
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		}, nil)

		// Get messages
		messages, err := svc.GetConversationMessages(r.Context(), conversationID, limit)
		if err != nil {
			log.Error().Err(err).Str("conversation_id", conversationID).Msg("get conversation messages failed")
			httpError(w, http.StatusInternalServerError, "get conversation messages failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"conversation_id": conversationID,
			"messages":        messages,
			"count":           len(messages),
		})
	}
}

// CompanionConversationCompressHandler returns a handler for POST /api/companion/conversations/:id/compress.
// The handler triggers on-demand L0→L1→L2 compression for the specified conversation by generating/updating
// day summaries (L1) and optionally running weekly distillation (L2).
func CompanionConversationCompressHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path: /api/companion/conversations/:id/compress
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/conversations/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] != "compress" {
			httpError(w, http.StatusBadRequest, "invalid path, expected /api/companion/conversations/:id/compress")
			return
		}
		conversationID := parts[0]

		var req struct {
			IncludeToday bool  `json:"include_today,omitempty"`
			MaxDays      int   `json:"max_days,omitempty"`
			Force        bool  `json:"force,omitempty"`
			Distill      *bool `json:"distill,omitempty"`

			// Optional: override which provider/model are used for summarization.
			LLMProvider string `json:"llm_provider,omitempty"`
			LLMModel    string `json:"llm_model,omitempty"`
		}
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		distill := true
		if req.Distill != nil {
			distill = *req.Distill
		}

		// Resolve LLM credentials. API keys are always resolved server-side from config/env.
		llmProvider := strings.TrimSpace(req.LLMProvider)
		if llmProvider == "" {
			llmProvider = cfg.LLM.Provider
		}
		llmAPIKey := ""
		llmModel := strings.TrimSpace(req.LLMModel)
		if llmProvider != "" {
			llmAPIKey = cfg.LLM.ResolveAPIKey(llmProvider)
			if llmModel == "" {
				llmModel = cfg.LLM.ResolveModel(llmProvider)
			}
		}

		// Open context store (required by Service constructor).
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Open companion memory database.
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// Create service with memory + summarizer credentials.
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:      log,
			MemoryDB:    memoryDB,
			LLMProvider: llmProvider,
			LLMAPIKey:   llmAPIKey,
			LLMModel:    llmModel,
		}, nil)
		if svc.Memory() == nil {
			httpError(w, http.StatusInternalServerError, "memory features not enabled")
			return
		}

		result, err := svc.Memory().CompressConversation(r.Context(), conversationID, companion.CompressionOptions{
			IncludeToday: req.IncludeToday,
			MaxDays:      req.MaxDays,
			Force:        req.Force,
			Distill:      distill,
		})
		if err != nil {
			if strings.Contains(err.Error(), "no summarizer configured") {
				httpError(w, http.StatusBadRequest, "summarization is not configured (missing LLM provider/api key)")
				return
			}
			log.Error().Err(err).Str("conversation_id", conversationID).Msg("compress conversation failed")
			httpError(w, http.StatusInternalServerError, "compress conversation failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, envelope.OK("companion.compress", result))
	}
}

// CompanionMessageDeleteHandler returns a handler for DELETE /api/companion/conversations/:id/messages/:msgId.
func CompanionMessageDeleteHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id and message_id from path:
		// /api/companion/conversations/:id/messages/:msgId
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/conversations/"), "/")
		if len(parts) < 3 || parts[0] == "" || parts[1] != "messages" || parts[2] == "" {
			httpError(w, http.StatusBadRequest, "invalid path, expected /api/companion/conversations/:id/messages/:msgId")
			return
		}
		conversationID := parts[0]
		messageID := parts[2]

		// Open companion memory database
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// Open context store (needed by Service constructor)
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		}, nil)

		// Delete message
		err = svc.DeleteMessage(r.Context(), conversationID, messageID)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				httpError(w, http.StatusNotFound, "message not found")
				return
			}
			log.Error().Err(err).
				Str("conversation_id", conversationID).
				Str("message_id", messageID).
				Msg("delete message failed")
			httpError(w, http.StatusInternalServerError, "delete message failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, SuccessResponse{OK: true, Message: "message deleted"})
	}
}

// CompanionConversationDeleteHandler returns a handler for DELETE /api/companion/conversations/:id.
// CompanionConversationDeleteHandler returns an HTTP handler that soft-deletes the conversation
// identified by the :id path segment when called with DELETE /api/companion/conversations/:id and
// writes a JSON success response.
func CompanionConversationDeleteHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path: /api/companion/conversations/:id
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/conversations/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			httpError(w, http.StatusBadRequest, "conversation_id is required in path")
			return
		}
		conversationID := parts[0]

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Open companion memory database
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		}, nil)

		// Soft delete conversation
		err = svc.SoftDeleteConversation(r.Context(), conversationID)
		if err != nil {
			log.Error().Err(err).Str("conversation_id", conversationID).Msg("soft delete conversation failed")
			httpError(w, http.StatusInternalServerError, "delete conversation failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, SuccessResponse{OK: true, Message: "conversation deleted"})
	}
}

// CompanionConversationRenameHandler returns a handler for PATCH /api/companion/conversations/:id.
// Accepts a JSON body with `title` (string) and optional `agent_id` (*string) to link/unlink
// the conversation to an agent. Supports many-to-one: multiple conversations can share one agent_id.
func CompanionConversationRenameHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path: /api/companion/conversations/:id
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/conversations/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			httpError(w, http.StatusBadRequest, "conversation_id is required in path")
			return
		}
		conversationID := parts[0]

		// Parse request body
		var req struct {
			Title   string  `json:"title"`
			AgentID *string `json:"agent_id,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Open companion memory database
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		}, nil)

		// Rename conversation
		err = svc.RenameConversation(r.Context(), conversationID, req.Title)
		if err != nil {
			log.Error().Err(err).Str("conversation_id", conversationID).Msg("rename conversation failed")
			httpError(w, http.StatusInternalServerError, "rename conversation failed: "+err.Error())
			return
		}

		// Link agent if provided
		if req.AgentID != nil {
			if linkErr := svc.LinkConversationAgent(r.Context(), conversationID, *req.AgentID); linkErr != nil {
				log.Error().Err(linkErr).Str("conversation_id", conversationID).Str("agent_id", *req.AgentID).Msg("link conversation agent failed (rename succeeded)")
				writeJSON(w, http.StatusOK, map[string]any{
					"ok":         true,
					"message":    "conversation renamed, but agent linking failed",
					"agent_id":   *req.AgentID,
					"link_error": linkErr.Error(),
				})
				return
			}
		}

		writeJSON(w, http.StatusOK, SuccessResponse{OK: true, Message: "conversation renamed"})
	}
}

// CompanionMemoryStatsHandler returns a handler for GET /api/companion/memory/:id/stats.
// CompanionMemoryStatsHandler returns an http.HandlerFunc that handles GET requests to /api/companion/memory/:id/stats.
// The handler extracts the conversation ID from the path, obtains memory statistics for that conversation, and writes the stats as JSON in the response.
func CompanionMemoryStatsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/memory/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] != "stats" {
			httpError(w, http.StatusBadRequest, "invalid path, expected /api/companion/memory/:id/stats")
			return
		}
		conversationID := parts[0]

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Open companion memory database
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		}, nil)

		// Get stats
		stats, err := svc.GetMemoryStats(r.Context(), conversationID)
		if err != nil {
			log.Error().Err(err).Msg("get memory stats failed")
			httpError(w, http.StatusInternalServerError, "get memory stats failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, stats)
	}
}

// CompanionMemoryContextHandler returns a handler for GET /api/companion/memory/:id/context.
// CompanionMemoryContextHandler returns an HTTP handler that serves the formatted memory context for a conversation.
//
// The handler expects a GET request to the path /api/companion/memory/:id/context where :id is the conversation ID.
// On success it responds with status 200 and a JSON body {"context": ...} containing the formatted memory context.
// It responds with 405 if the method is not GET, 400 for an invalid path, and 500 for internal errors opening stores or retrieving context.
func CompanionMemoryContextHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/memory/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] != "context" {
			httpError(w, http.StatusBadRequest, "invalid path, expected /api/companion/memory/:id/context")
			return
		}
		conversationID := parts[0]

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Open companion memory database
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		}, nil)

		// Get context
		context, err := svc.GetMemoryContext(r.Context(), conversationID)
		if err != nil {
			log.Error().Err(err).Msg("get memory context failed")
			httpError(w, http.StatusInternalServerError, "get memory context failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"context": context,
		})
	}
}

// CompanionMemoryClearHandler returns a handler for DELETE /api/companion/memory/:id.
// CompanionMemoryClearHandler returns an HTTP handler that clears all stored memory for a conversation identified by the conversation_id path segment (DELETE /api/companion/memory/:conversation_id).
// The handler requires the DELETE method, responds 405 for other methods, 400 if the conversation_id is missing, 200 with `{"ok": true}` on success, and 500 for internal errors encountered while accessing storage or the memory database.
func CompanionMemoryClearHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/memory/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			httpError(w, http.StatusBadRequest, "conversation_id is required in path")
			return
		}
		conversationID := parts[0]

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Open companion memory database
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		}, nil)

		// Clear memory
		err = svc.ClearMemory(r.Context(), conversationID)
		if err != nil {
			log.Error().Err(err).Msg("clear memory failed")
			httpError(w, http.StatusInternalServerError, "clear memory failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
		})
	}
}

// CompanionPersonalityHandler returns a handler for GET /api/companion/conversations/:id/personality.
// CompanionPersonalityHandler returns an HTTP handler that serves GET /api/companion/conversations/:id/personality and writes the conversation's personality profile and built system prompt as JSON.
func CompanionPersonalityHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path: /api/companion/conversations/:id/personality
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/conversations/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] != "personality" {
			httpError(w, http.StatusBadRequest, "invalid path, expected /api/companion/conversations/:id/personality")
			return
		}
		conversationID := parts[0]

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Open companion memory database
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, closeFn, err := dbutil.OpenStoreDB(r.Context(), cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = closeFn() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		}, nil)

		// Get personality info
		info, err := svc.GetPersonalityInfo(r.Context(), conversationID)
		if err != nil {
			log.Error().Err(err).Msg("get personality info failed")
			httpError(w, http.StatusInternalServerError, "get personality info failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, info)
	}
}

// CompanionPersonalityDimensionPatchHandler returns a handler for PATCH /api/companion/conversations/:id/personality/dimension.
// CompanionPersonalityDimensionPatchHandler returns an HTTP handler that updates a single personality dimension for a conversation.
// The handler expects PATCH requests to /api/companion/conversations/:id/personality/dimension with a JSON body containing
// `name` (string) and `value` (number); it validates the input, updates the personality dimension in the companion service,
// and responds with the updated `name` and `value` on success or an appropriate HTTP error status on failure.
func CompanionPersonalityDimensionPatchHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract conversation_id from path: /api/companion/conversations/:id/personality/dimension
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/conversations/"), "/")
		if len(parts) < 3 || parts[0] == "" || parts[1] != "personality" || parts[2] != "dimension" {
			httpError(w, http.StatusBadRequest, "invalid path, expected /api/companion/conversations/:id/personality/dimension")
			return
		}
		conversationID := parts[0]

		// Parse request body
		var req struct {
			Name  string  `json:"name"`
			Value float64 `json:"value"`
		}
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if req.Name == "" {
			httpError(w, http.StatusBadRequest, "dimension name is required")
			return
		}

		// Open context store
		store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open context store")
			httpError(w, http.StatusInternalServerError, "failed to open context store")
			return
		}
		defer store.Close()

		// Create companion service
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger: log,
		}, nil)

		// Update the dimension
		if err := svc.UpdatePersonalityDimension(r.Context(), conversationID, req.Name, req.Value); err != nil {
			log.Error().Err(err).Str("dimension", req.Name).Msg("update personality dimension failed")
			httpError(w, http.StatusInternalServerError, "update personality dimension failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"name":    req.Name,
			"value":   req.Value,
		})
	}
}

// CompanionCharactersListHandler returns a handler for GET /api/companion/characters/:conversation_id.
// CompanionCharactersListHandler returns an HTTP handler that lists characters for a conversation by invoking the "presence/character" skill.
// The handler accepts only GET requests, expects the conversation_id as the first path segment after /api/companion/characters/, returns 503 if the skill runner is not configured, and forwards the skill's output as JSON or an appropriate HTTP error on failure.
func CompanionCharactersListHandler(cfg config.Config, log zerolog.Logger, skillRunner *SkillRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if skillRunner == nil {
			httpError(w, http.StatusServiceUnavailable, "presence skills not configured")
			return
		}

		// Extract conversation_id from path
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/characters/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			httpError(w, http.StatusBadRequest, "conversation_id is required in path")
			return
		}
		conversationID := parts[0]

		// Run presence/character skill with list action
		input := map[string]any{
			"action":          "list",
			"conversation_id": conversationID,
		}
		result, err := skillRunner.Run(r.Context(), "presence/character", input)
		if err != nil {
			log.Error().Err(err).Msg("presence/character skill failed")
			httpError(w, http.StatusInternalServerError, "failed to list characters: "+err.Error())
			return
		}
		if !result.Success {
			httpError(w, http.StatusInternalServerError, "skill returned error: "+result.Error)
			return
		}

		writeJSON(w, http.StatusOK, result.Output)
	}
}

// CompanionCharacterCreateHandler returns a handler for POST /api/companion/characters.
// CompanionCharacterCreateHandler returns an HTTP handler that creates a new character for a conversation by invoking the "presence/character" skill.
// The handler accepts POST requests with JSON containing `conversation_id` and `name` (optional `avatar_digest`, `voice_id`, `base_mood`), responds with 201 and the skill output on success, and returns appropriate HTTP status codes for method not allowed, missing/invalid input, unconfigured skills, or skill execution errors.
func CompanionCharacterCreateHandler(cfg config.Config, log zerolog.Logger, skillRunner *SkillRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if skillRunner == nil {
			httpError(w, http.StatusServiceUnavailable, "presence skills not configured")
			return
		}

		// Parse request
		var req struct {
			ConversationID string `json:"conversation_id"`
			Name           string `json:"name"`
			AvatarDigest   string `json:"avatar_digest,omitempty"`
			VoiceID        string `json:"voice_id,omitempty"`
			BaseMood       string `json:"base_mood,omitempty"`
		}
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		if req.ConversationID == "" {
			httpError(w, http.StatusBadRequest, "conversation_id is required")
			return
		}
		if req.Name == "" {
			httpError(w, http.StatusBadRequest, "name is required")
			return
		}

		// Run presence/character skill with register action
		input := map[string]any{
			"action":          "register",
			"conversation_id": req.ConversationID,
			"name":            req.Name,
		}
		if req.AvatarDigest != "" {
			input["avatar_digest"] = req.AvatarDigest
		}
		if req.VoiceID != "" {
			input["voice_id"] = req.VoiceID
		}
		if req.BaseMood != "" {
			input["emotion"] = req.BaseMood
		}

		result, err := skillRunner.Run(r.Context(), "presence/character", input)
		if err != nil {
			log.Error().Err(err).Msg("presence/character skill failed")
			httpError(w, http.StatusInternalServerError, "failed to create character: "+err.Error())
			return
		}
		if !result.Success {
			httpError(w, http.StatusInternalServerError, "skill returned error: "+result.Error)
			return
		}

		writeJSON(w, http.StatusCreated, result.Output)
	}
}

// CompanionCharacterGetHandler returns a handler for GET /api/companion/characters/:conversation_id/:id.
// CompanionCharacterGetHandler returns an HTTP handler that serves GET
// /api/companion/characters/:conversation_id/:character_id and retrieves the
// specified character including its overlays by invoking the "presence/character" skill.
// It validates the request method and path, requires a non-nil SkillRunner (responding
// with 503 if absent), and writes the skill's output as a 200 JSON response or an
// appropriate HTTP error on failure.
func CompanionCharacterGetHandler(cfg config.Config, log zerolog.Logger, skillRunner *SkillRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if skillRunner == nil {
			httpError(w, http.StatusServiceUnavailable, "presence skills not configured")
			return
		}

		// Extract conversation_id and character_id from path
		// /api/companion/characters/:conversation_id/:character_id
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/characters/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			httpError(w, http.StatusBadRequest, "conversation_id and character_id are required in path")
			return
		}
		conversationID := parts[0]
		characterID := parts[1]

		// Run presence/character skill with get action
		// Include conversation_id for proper scoping (prevents cross-conversation access)
		input := map[string]any{
			"action":          "get",
			"conversation_id": conversationID,
			"character_id":    characterID,
		}
		result, err := skillRunner.Run(r.Context(), "presence/character", input)
		if err != nil {
			log.Error().Err(err).Msg("presence/character skill failed")
			httpError(w, http.StatusInternalServerError, "failed to get character: "+err.Error())
			return
		}
		if !result.Success {
			httpError(w, http.StatusInternalServerError, "skill returned error: "+result.Error)
			return
		}

		writeJSON(w, http.StatusOK, result.Output)
	}
}

// CompanionCharacterOverlayHandler returns a handler for POST /api/companion/characters/:conversation_id/:id/overlays.
// The handler maps method validation, request validation, missing skill runner, and skill execution failures to appropriate HTTP status codes.
func CompanionCharacterOverlayHandler(cfg config.Config, log zerolog.Logger, skillRunner *SkillRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if skillRunner == nil {
			httpError(w, http.StatusServiceUnavailable, "presence skills not configured")
			return
		}

		// Extract conversation_id and character_id from path
		// /api/companion/characters/:conversation_id/:character_id/overlays
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/characters/"), "/")
		if len(parts) < 3 || parts[0] == "" || parts[1] == "" || parts[2] != "overlays" {
			httpError(w, http.StatusBadRequest, "invalid path, expected /api/companion/characters/:conversation_id/:character_id/overlays")
			return
		}
		conversationID := parts[0]
		characterID := parts[1]

		// Parse request
		var req struct {
			Emotion       string `json:"emotion"`
			OverlayDigest string `json:"overlay_digest"`
		}
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		if req.Emotion == "" {
			httpError(w, http.StatusBadRequest, "emotion is required")
			return
		}
		if req.OverlayDigest == "" {
			httpError(w, http.StatusBadRequest, "overlay_digest is required")
			return
		}

		// Run presence/character skill with register_overlay action
		// Include conversation_id for proper scoping (prevents cross-conversation access)
		input := map[string]any{
			"action":          "register_overlay",
			"conversation_id": conversationID,
			"character_id":    characterID,
			"emotion":         req.Emotion,
			"overlay_digest":  req.OverlayDigest,
		}
		result, err := skillRunner.Run(r.Context(), "presence/character", input)
		if err != nil {
			log.Error().Err(err).Msg("presence/character skill failed")
			httpError(w, http.StatusInternalServerError, "failed to register overlay: "+err.Error())
			return
		}
		if !result.Success {
			httpError(w, http.StatusInternalServerError, "skill returned error: "+result.Error)
			return
		}

		writeJSON(w, http.StatusCreated, result.Output)
	}
}
