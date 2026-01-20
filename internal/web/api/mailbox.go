package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
)

// MailboxMessageResponse represents a mailbox message in API responses.
type MailboxMessageResponse struct {
	ID          string `json:"id"`
	Sender      string `json:"sender"`
	Recipient   string `json:"recipient"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	Kind        string `json:"kind"`
	Priority    int    `json:"priority"`
	Status      string `json:"status"`
	AckRequired bool   `json:"ack_required,omitempty"`
	CreatedAt   string `json:"created_at"`
	TaskID      string `json:"task_id,omitempty"`
	Stream      string `json:"stream,omitempty"`
}

// MailboxListHandler returns a handler for GET /api/mailbox.
func MailboxListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Parse query params
		limit := 50
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}

		actor := r.URL.Query().Get("actor_id")
		if actor == "" {
			actor = r.URL.Query().Get("actor")
		}
		all := parseBool(r.URL.Query().Get("all"))
		if actor == "" && !all {
			actor = agent.BroadcastRecipient
		}

		onlyUnread := parseBool(r.URL.Query().Get("only_unread"))
		onlyUnsurfaced := parseBool(r.URL.Query().Get("only_unsurfaced"))

		workspace := r.URL.Query().Get("workspace_id")
		if workspace == "" {
			workspace = r.URL.Query().Get("workspace")
		}
		if workspace == "" {
			httpError(w, http.StatusBadRequest, "workspace_id required")
			return
		}

		store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open board store")
			httpError(w, http.StatusInternalServerError, "failed to open board store")
			return
		}
		defer store.Close()

		msgList, err := store.Inbox(r.Context(), agent.InboxFilter{
			WorkspaceID:    workspace,
			ActorID:        actor,
			OnlyUnread:     onlyUnread,
			OnlyUnsurfaced: onlyUnsurfaced,
			Limit:          limit,
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to list mailbox messages")
			httpError(w, http.StatusInternalServerError, "failed to list mailbox messages")
			return
		}

		messages := convertBoardMessages(msgList)
		writeJSON(w, http.StatusOK, map[string]any{
			"messages": messages,
		})
	}
}

func convertBoardMessages(msgs []agent.BoardMessage) []MailboxMessageResponse {
	resp := make([]MailboxMessageResponse, 0, len(msgs))
	for _, m := range msgs {
		resp = append(resp, MailboxMessageResponse{
			ID:          m.ID,
			Sender:      m.Sender,
			Recipient:   m.Recipient,
			Subject:     m.Subject,
			Body:        m.Body,
			Kind:        string(m.Kind),
			Priority:    m.Priority,
			Status:      string(m.Status),
			AckRequired: m.AckRequired,
			CreatedAt:   m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			TaskID:      m.TaskID,
			Stream:      m.Stream,
		})
	}
	return resp
}

func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
