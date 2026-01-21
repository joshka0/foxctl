package api

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

// CompanionChatHandler returns a handler for POST /api/companion/chat.
// This is the main chat endpoint for the companion mobile app.
func CompanionChatHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Parse request
		var req companion.ChatRequest
		if err := readJSON(r, &req); err != nil {
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

		// Open companion memory database for memory context injection
		dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
		memoryDB, err := sqliteutil.OpenDB(r.Context(), dbPath, nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = memoryDB.Close() }()

		// Create service with memory enabled
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		})

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
// Sets a context variable for a conversation.
func CompanionContextSetHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Parse request
		var req companion.ContextSetRequest
		if err := readJSON(r, &req); err != nil {
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
		})

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

		// Create service
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger: log,
		})

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
		})

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
		})

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
// Lists all conversations.
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
		memoryDB, err := sqliteutil.OpenDB(r.Context(), dbPath, nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = memoryDB.Close() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		})

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

// CompanionMemoryStatsHandler returns a handler for GET /api/companion/memory/:id/stats.
// Gets memory stats for a conversation.
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
		memoryDB, err := sqliteutil.OpenDB(r.Context(), dbPath, nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = memoryDB.Close() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		})

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
// Gets formatted memory context for a conversation.
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
		memoryDB, err := sqliteutil.OpenDB(r.Context(), dbPath, nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = memoryDB.Close() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		})

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
// Clears all memory for a conversation.
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
		memoryDB, err := sqliteutil.OpenDB(r.Context(), dbPath, nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to open companion memory database")
			httpError(w, http.StatusInternalServerError, "failed to open companion memory database")
			return
		}
		defer func() { _ = memoryDB.Close() }()

		// Create service with memory DB
		svc := companion.NewService(store, companion.ServiceConfig{
			Logger:   log,
			MemoryDB: memoryDB,
		})

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
