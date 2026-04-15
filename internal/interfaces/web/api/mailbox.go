package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
)

// MailboxMessageResponse represents a mailbox message in API responses.
type MailboxMessageResponse struct {
	ID               string `json:"id"`
	RelatedMessageID string `json:"related_message_id,omitempty"`
	Sender           string `json:"sender"`
	Recipient        string `json:"recipient"`
	Subject          string `json:"subject"`
	Body             string `json:"body"`
	Kind             string `json:"kind"`
	Priority         int    `json:"priority"`
	Status           string `json:"status"`
	AckRequired      bool   `json:"ack_required,omitempty"`
	ReplyExpected    bool   `json:"reply_expected,omitempty"`
	Interrupt        bool   `json:"interrupt,omitempty"`
	CreatedAt        string `json:"created_at"`
	TaskID           string `json:"task_id,omitempty"`
	Stream           string `json:"stream,omitempty"`
}

// MailboxSendRequest is the request body for sending a mailbox message.
type MailboxSendRequest struct {
	WorkspaceID      string `json:"workspace_id"`
	RelatedMessageID string `json:"related_message_id,omitempty"`
	Sender           string `json:"sender"`
	Recipient        string `json:"recipient"`
	Subject          string `json:"subject"`
	Body             string `json:"body"`
	Kind             string `json:"kind,omitempty"`
	Priority         int    `json:"priority,omitempty"`
	AckRequired      bool   `json:"ack_required,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	Stream           string `json:"stream,omitempty"`
}

// MailboxSendResponse is the response for sending a mailbox message.
type MailboxSendResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// MailboxStatusUpdateRequest updates the read/ack lifecycle for existing board messages.
type MailboxStatusUpdateRequest struct {
	WorkspaceID string   `json:"workspace_id"`
	ActorID     string   `json:"actor_id,omitempty"`
	Action      string   `json:"action"`
	MessageIDs  []string `json:"message_ids"`
}

// MailboxStatusUpdateResponse reports how many messages changed state.
type MailboxStatusUpdateResponse struct {
	Action  string `json:"action"`
	Updated int    `json:"updated"`
	Status  string `json:"status"`
}

// MailboxListHandler returns an HTTP handler for the /api/mailbox endpoint.
//
// It handles GET and POST requests. POST requests are forwarded to handleMailboxSend to create and send a mailbox message.
// GET requests return a list of mailbox messages and accept the following query parameters:
// - limit: integer 1–500 (defaults to 50).
// - actor_id or actor: actor identifier; if omitted and `all` is not true, defaults to the broadcast recipient.
// - all: boolean flag to bypass actor restriction.
// - task_id: optional task identifier to filter room/task-local messages.
// - stream: optional stream identifier (for example `room:<id>`) to filter room timelines.
// - only_unread: boolean flag to filter only unread messages.
// - only_unsurfaced: boolean flag to filter only unsurfaced messages.
// - workspace_id or workspace: required workspace identifier.
//
// PATCH requests update message lifecycle state and require workspace_id, action, and at least one message_id.
// Supported PATCH actions are `read`, `surfaced`, and `ack`.
//
// For a successful GET the handler returns a JSON object with a "messages" array. For unsupported HTTP methods it responds with 405 Method Not Allowed.
func MailboxListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handleMailboxSend(w, r, cfg, log)
			return
		}
		if r.Method == http.MethodPatch {
			handleMailboxStatusUpdate(w, r, cfg, log)
			return
		}
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
		taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
		stream := strings.TrimSpace(r.URL.Query().Get("stream"))

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
			TaskID:         taskID,
			Stream:         stream,
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
			ID:               m.ID,
			RelatedMessageID: m.RelatedMessageID,
			Sender:           m.Sender,
			Recipient:        m.Recipient,
			Subject:          m.Subject,
			Body:             m.Body,
			Kind:             string(m.Kind),
			Priority:         m.Priority,
			Status:           string(m.Status),
			AckRequired:      m.AckRequired,
			ReplyExpected:    m.ReplyExpected,
			Interrupt:        m.Interrupt,
			CreatedAt:        m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			TaskID:           m.TaskID,
			Stream:           m.Stream,
		})
	}
	return resp
}

// parseBool reports whether raw represents a truthy value.
// It treats the trimmed, case-insensitive strings "1", "true", "yes", "y", and "on" as true; any other value yields false.
func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// handleMailboxSend parses a MailboxSendRequest from the HTTP request, validates required fields,
// constructs a BoardMessage with defaults (kind defaults to "info", priority defaults to 3) and
// persists it to the board store, then responds with a 201 Created containing the new message ID.
//
// Validation behavior:
// - Returns 400 if workspace_id, sender, or recipient are missing.
// - Returns 400 if kind is not one of "instruction", "info", "alert", or "review_request".
// - Returns 400 if priority is not between 1 (highest) and 5 (lowest).
//
// Store errors result in a 500 Internal Server Error response.
func handleMailboxSend(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	// Parse request body (readJSON limits body size to prevent DOS)
	var req MailboxSendRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.WorkspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.Sender == "" {
		httpError(w, http.StatusBadRequest, "sender is required")
		return
	}
	if req.Recipient == "" {
		httpError(w, http.StatusBadRequest, "recipient is required")
		return
	}

	// Open board store
	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()

	// Validate and default kind to "info"
	kind := agent.BoardMessageKind(req.Kind)
	if kind == "" {
		kind = agent.BoardMessageKindInfo
	} else {
		// Validate enum value
		switch kind {
		case agent.BoardMessageKindInstruction, agent.BoardMessageKindInfo,
			agent.BoardMessageKindAlert, agent.BoardMessageKindReviewRequest,
			agent.BoardMessageKindCoordinatorPulse,
			agent.BoardMessageKindPlanSession, agent.BoardMessageKindPlanProposal,
			agent.BoardMessageKindPlanQuestion, agent.BoardMessageKindPlanDecision,
			agent.BoardMessageKindPlanReview, agent.BoardMessageKindPlanClose,
			agent.BoardMessageKindInterviewSession, agent.BoardMessageKindInterviewQuestion,
			agent.BoardMessageKindInterviewAnswer, agent.BoardMessageKindInterviewVerify,
			agent.BoardMessageKindEpic, agent.BoardMessageKindEpicQuestion,
			agent.BoardMessageKindEpicAnswer, agent.BoardMessageKindEpicFinalize, agent.BoardMessageKindEpicUpdate, agent.BoardMessageKindEpicClose, agent.BoardMessageKindEpicCheckpoint,
			agent.BoardMessageKindMilestoneProposal,
			agent.BoardMessageKindMilestone,
			agent.BoardMessageKindMilestoneContract,
			agent.BoardMessageKindStory, agent.BoardMessageKindStoryProposal, agent.BoardMessageKindStoryState, agent.BoardMessageKindStoryUpdate, agent.BoardMessageKindStoryValidation,
			agent.BoardMessageKindAcceptanceCriteria,
			agent.BoardMessageKindMilestoneReview, agent.BoardMessageKindMilestoneSummary,
			agent.BoardMessageKindDeliveryLog, agent.BoardMessageKindGuidanceUpdate:
			// valid
		default:
			httpError(w, http.StatusBadRequest, "invalid kind: must be one of instruction, info, alert, review_request, coordinator_pulse, plan_session, plan_proposal, plan_question, plan_decision, plan_review, plan_close, interview_session, interview_question, interview_answer, interview_verify, epic, epic_question, epic_answer, epic_finalize, epic_update, epic_checkpoint, milestone_proposal, milestone, story, story_proposal, story_state, story_update, story_validation, acceptance_criteria, milestone_review, milestone_summary, delivery_log, guidance_update")
			return
		}
	}

	// Validate and default priority (1=highest, 5=lowest)
	priority := req.Priority
	if priority == 0 {
		priority = 3 // default to medium priority
	} else if priority < 1 || priority > 5 {
		httpError(w, http.StatusBadRequest, "invalid priority: must be between 1 (highest) and 5 (lowest)")
		return
	}

	// Create the message
	msg := &agent.BoardMessage{
		ID:               uuid.New().String(),
		WorkspaceID:      req.WorkspaceID,
		RelatedMessageID: strings.TrimSpace(req.RelatedMessageID),
		Sender:           req.Sender,
		Recipient:        req.Recipient,
		Subject:          req.Subject,
		Body:             req.Body,
		Kind:             kind,
		Priority:         priority,
		Status:           agent.BoardMessageStatusUnread,
		AckRequired:      req.AckRequired,
		CreatedAt:        time.Now(),
		TaskID:           req.TaskID,
		Stream:           req.Stream,
	}

	// Send the message
	if err := store.SendMessage(r.Context(), msg); err != nil {
		log.Error().Err(err).Msg("failed to send message")
		httpError(w, http.StatusInternalServerError, "failed to send message")
		return
	}

	writeJSON(w, http.StatusCreated, MailboxSendResponse{
		ID:      msg.ID,
		Status:  "sent",
		Message: "Message sent successfully",
	})
}

func handleMailboxStatusUpdate(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	var req MailboxStatusUpdateRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Action = normalizeMailboxStatusAction(req.Action)
	if req.WorkspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.Action == "" {
		httpError(w, http.StatusBadRequest, "action must be one of read, surfaced, ack")
		return
	}

	messageIDs := make([]string, 0, len(req.MessageIDs))
	for _, id := range req.MessageIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			messageIDs = append(messageIDs, trimmed)
		}
	}
	if len(messageIDs) == 0 {
		httpError(w, http.StatusBadRequest, "message_ids must contain at least one id")
		return
	}

	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()

	var updated int
	switch req.Action {
	case "read":
		updated, err = store.MarkRead(r.Context(), req.WorkspaceID, req.ActorID, messageIDs)
	case "surfaced":
		updated, err = store.MarkSurfaced(r.Context(), req.WorkspaceID, req.ActorID, messageIDs)
	case "ack":
		updated, err = store.AckMessages(r.Context(), req.WorkspaceID, req.ActorID, messageIDs)
	default:
		httpError(w, http.StatusBadRequest, "action must be one of read, surfaced, ack")
		return
	}
	if err != nil {
		log.Error().Err(err).Str("action", req.Action).Msg("failed to update mailbox messages")
		httpError(w, http.StatusInternalServerError, "failed to update mailbox messages")
		return
	}

	writeJSON(w, http.StatusOK, MailboxStatusUpdateResponse{
		Action:  req.Action,
		Updated: updated,
		Status:  "ok",
	})
}

func normalizeMailboxStatusAction(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "read":
		return "read"
	case "surfaced":
		return "surfaced"
	case "ack", "acked":
		return "ack"
	default:
		return ""
	}
}
