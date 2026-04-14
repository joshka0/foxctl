package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/conversationsettings"
)

// CompanionConversationSettingsHandler handles GET/PATCH for /api/companion/conversations/:id/settings.
func CompanionConversationSettingsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract conversation_id from path: /api/companion/conversations/:id/settings
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/api/companion/conversations/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] != "settings" {
			httpError(w, http.StatusBadRequest, "invalid path, expected /api/companion/conversations/:id/settings")
			return
		}
		conversationID := parts[0]

		store, err := conversationsettings.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open conversation settings store")
			httpError(w, http.StatusInternalServerError, "failed to open conversation settings store")
			return
		}
		defer store.Close()

		switch r.Method {
		case http.MethodGet:
			settings, err := store.Get(r.Context(), conversationID)
			if err != nil {
				if errors.Is(err, conversationsettings.ErrNotFound) {
					settings = conversationsettings.Settings{ConversationID: conversationID}
				} else {
					log.Error().Err(err).Str("conversation_id", conversationID).Msg("failed to get conversation settings")
					httpError(w, http.StatusInternalServerError, "failed to get conversation settings")
					return
				}
			}

			writeJSON(w, http.StatusOK, envelope.OK("conversation.settings.get", map[string]any{
				"conversation_id": conversationID,
				"settings":        settings,
			}))

		case http.MethodPatch:
			var patch conversationsettings.Patch
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}

			settings, err := store.Patch(r.Context(), conversationID, patch)
			if err != nil {
				if errors.Is(err, conversationsettings.ErrInvalid) {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
				log.Error().Err(err).Str("conversation_id", conversationID).Msg("failed to patch conversation settings")
				httpError(w, http.StatusInternalServerError, "failed to patch conversation settings")
				return
			}

			writeJSON(w, http.StatusOK, envelope.OK("conversation.settings.patch", map[string]any{
				"conversation_id": conversationID,
				"settings":        settings,
			}))

		case http.MethodDelete:
			if err := store.Delete(r.Context(), conversationID); err != nil {
				log.Error().Err(err).Str("conversation_id", conversationID).Msg("failed to delete conversation settings")
				httpError(w, http.StatusInternalServerError, "failed to delete conversation settings")
				return
			}
			writeJSON(w, http.StatusOK, envelope.OK("conversation.settings.delete", map[string]any{
				"message": "settings deleted",
			}))

		default:
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}
