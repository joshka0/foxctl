package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
)

// RoomResponse is the room-centric read model exposed over HTTP.
type RoomResponse struct {
	ID               string               `json:"id"`
	WorkspaceID      string               `json:"workspace_id"`
	Stream           string               `json:"stream"`
	Title            string               `json:"title"`
	Description      string               `json:"description,omitempty"`
	DispatchPolicy   string               `json:"dispatch_policy,omitempty"`
	DispatchAgentIDs []string             `json:"dispatch_agent_ids,omitempty"`
	CreatedAt        string               `json:"created_at,omitempty"`
	UpdatedAt        string               `json:"updated_at,omitempty"`
	LatestSubject    string               `json:"latest_subject,omitempty"`
	LatestPreview    string               `json:"latest_preview,omitempty"`
	LatestSender     string               `json:"latest_sender,omitempty"`
	LatestMessageAt  string               `json:"latest_message_at,omitempty"`
	MessageCount     int                  `json:"message_count"`
	UnreadCount      int                  `json:"unread_count"`
	Participants     []string             `json:"participants,omitempty"`
	TaskIDs          []string             `json:"task_ids,omitempty"`
	Members          []RoomMemberResponse `json:"members,omitempty"`
}

type RoomMemberResponse struct {
	ActorID  string `json:"actor_id"`
	Role     string `json:"role,omitempty"`
	Backend  string `json:"backend,omitempty"`
	Session  string `json:"session,omitempty"`
	PaneID   string `json:"pane_id,omitempty"`
	Unbound  bool   `json:"unbound,omitempty"`
	JoinedAt string `json:"joined_at,omitempty"`
}

type RoomCreateRequest struct {
	WorkspaceID      string              `json:"workspace_id"`
	ID               string              `json:"id,omitempty"`
	Title            string              `json:"title"`
	Description      string              `json:"description,omitempty"`
	DispatchPolicy   string              `json:"dispatch_policy,omitempty"`
	DispatchAgentIDs []string            `json:"dispatch_agent_ids,omitempty"`
	Members          []RoomMemberRequest `json:"members,omitempty"`
}

type RoomPatchRequest struct {
	Title            *string  `json:"title,omitempty"`
	Description      *string  `json:"description,omitempty"`
	DispatchPolicy   *string  `json:"dispatch_policy,omitempty"`
	DispatchAgentIDs []string `json:"dispatch_agent_ids,omitempty"`
}

type RoomMemberRequest struct {
	ActorID string `json:"actor_id"`
	Role    string `json:"role,omitempty"`
	Backend string `json:"backend,omitempty"`
	Session string `json:"session,omitempty"`
	PaneID  string `json:"pane_id,omitempty"`
	Unbound bool   `json:"unbound,omitempty"`
}

// RoomMessageSendRequest sends one message into a room-scoped stream.
type RoomMessageSendRequest struct {
	WorkspaceID      string         `json:"workspace_id"`
	Sender           string         `json:"sender"`
	Recipient        string         `json:"recipient,omitempty"`
	Subject          string         `json:"subject,omitempty"`
	Body             string         `json:"body"`
	Kind             string         `json:"kind,omitempty"`
	Priority         int            `json:"priority,omitempty"`
	AckRequired      bool           `json:"ack_required,omitempty"`
	ReplyExpected    bool           `json:"reply_expected,omitempty"`
	TaskID           string         `json:"task_id,omitempty"`
	DispatchAgents   bool           `json:"dispatch_agents,omitempty"`
	DispatchAgentIDs []string       `json:"dispatch_agent_ids,omitempty"`
	Context          map[string]any `json:"context,omitempty"`
}

// RoomMessageSendResponse reports the created room message.
type RoomMessageSendResponse struct {
	ID         string `json:"id"`
	RoomID     string `json:"room_id"`
	Stream     string `json:"stream"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	Dispatched int    `json:"dispatched,omitempty"`
	Skipped    int    `json:"skipped,omitempty"`
}

type roomEventPublisher interface {
	Publish(eventType string, data any)
}

type roomMessageEvent struct {
	WorkspaceID   string `json:"workspace_id"`
	RoomID        string `json:"room_id"`
	Stream        string `json:"stream"`
	MessageID     string `json:"message_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	Sender        string `json:"sender,omitempty"`
	Recipient     string `json:"recipient,omitempty"`
	Subject       string `json:"subject,omitempty"`
	Phase         string `json:"phase,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	Content       string `json:"content,omitempty"`
	ContentDelta  string `json:"content_delta,omitempty"`
	ToolName      string `json:"tool_name,omitempty"`
	ToolCallID    string `json:"tool_call_id,omitempty"`
	ToolOutput    string `json:"tool_output,omitempty"`
	IsError       bool   `json:"is_error,omitempty"`
	Dispatched    int    `json:"dispatched,omitempty"`
	Skipped       int    `json:"skipped,omitempty"`
	Error         string `json:"error,omitempty"`
}

type roomAgentDispatchRequest struct {
	WorkspaceID string
	RoomID      string
	Stream      string
	Sender      string
	Subject     string
	Body        string
	TaskID      string
	Context     map[string]any
}

type roomBoardAction struct {
	IssueID string
	Action  string
}

var runRoomAgentReply = defaultRoomAgentReply

func normalizeRoomDispatchPolicy(raw string) string {
	switch strings.TrimSpace(raw) {
	case "children_only", "lead_only", "selected":
		return strings.TrimSpace(raw)
	default:
		return "all_subtree"
	}
}

func normalizeRoomDispatchAgentIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// RoomsListHandler serves GET /api/rooms.
func RoomsListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handleRoomCreate(w, r, cfg, log)
			return
		}
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
		if workspaceID == "" {
			workspaceID = strings.TrimSpace(r.URL.Query().Get("workspace"))
		}
		if workspaceID == "" {
			httpError(w, http.StatusBadRequest, "workspace_id required")
			return
		}
		actorID := strings.TrimSpace(r.URL.Query().Get("actor_id"))
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 || n > 500 {
				httpError(w, http.StatusBadRequest, "limit must be between 1 and 500")
				return
			}
			limit = n
		}

		store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open board store")
			httpError(w, http.StatusInternalServerError, "failed to open board store")
			return
		}
		defer store.Close()

		rooms, err := store.ListRooms(r.Context(), workspaceID, actorID, limit)
		if err != nil {
			log.Error().Err(err).Msg("failed to list rooms")
			httpError(w, http.StatusInternalServerError, "failed to list rooms")
			return
		}

		resp := make([]RoomResponse, 0, len(rooms))
		for _, room := range rooms {
			resp = append(resp, convertRoomSummary(room))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"rooms": resp,
			"count": len(resp),
		})
	}
}

func handleRoomCreate(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	var req RoomCreateRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ID = strings.TrimSpace(req.ID)
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	if req.WorkspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.ID == "" {
		req.ID = slugRoomID(req.Title)
	}
	if req.ID == "" {
		httpError(w, http.StatusBadRequest, "room id or title is required")
		return
	}
	if req.Title == "" {
		req.Title = req.ID
	}

	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()

	room, err := store.UpsertRoom(r.Context(), agent.Room{
		ID:               req.ID,
		WorkspaceID:      req.WorkspaceID,
		Stream:           agent.RoomStreamName(req.ID),
		Title:            req.Title,
		Description:      req.Description,
		DispatchPolicy:   normalizeRoomDispatchPolicy(req.DispatchPolicy),
		DispatchAgentIDs: normalizeRoomDispatchAgentIDs(req.DispatchAgentIDs),
		Members:          toRoomMembers(req.Members),
	})
	if err != nil {
		log.Error().Err(err).Str("room_id", req.ID).Msg("failed to create room")
		httpError(w, http.StatusInternalServerError, "failed to create room")
		return
	}

	summary, err := store.GetRoom(r.Context(), room.WorkspaceID, room.ID, "")
	if err != nil {
		log.Error().Err(err).Str("room_id", room.ID).Msg("failed to reload room after create")
		httpError(w, http.StatusInternalServerError, "failed to create room")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"room": convertRoomSummary(summary),
	})
}

func handleRoomPatch(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	var req RoomPatchRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	workspaceID := roomWorkspaceID(r)
	if workspaceID == "" {
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

	current, err := store.GetRoom(r.Context(), workspaceID, roomID, "")
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room for patch")
		httpError(w, http.StatusInternalServerError, "failed to update room")
		return
	}

	title := current.Title
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
		if title == "" {
			title = roomID
		}
	}
	description := current.Description
	if req.Description != nil {
		description = strings.TrimSpace(*req.Description)
	}
	dispatchPolicy := normalizeRoomDispatchPolicy(current.DispatchPolicy)
	if req.DispatchPolicy != nil {
		dispatchPolicy = normalizeRoomDispatchPolicy(*req.DispatchPolicy)
	}
	dispatchAgentIDs := append([]string(nil), current.DispatchAgentIDs...)
	if req.DispatchAgentIDs != nil {
		dispatchAgentIDs = normalizeRoomDispatchAgentIDs(req.DispatchAgentIDs)
	}

	if _, err := store.UpsertRoom(r.Context(), agent.Room{
		ID:               roomID,
		WorkspaceID:      workspaceID,
		Stream:           current.Stream,
		Title:            title,
		Description:      description,
		DispatchPolicy:   dispatchPolicy,
		DispatchAgentIDs: dispatchAgentIDs,
	}); err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to patch room")
		httpError(w, http.StatusInternalServerError, "failed to update room")
		return
	}

	updated, err := store.GetRoom(r.Context(), workspaceID, roomID, "")
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to reload room after patch")
		httpError(w, http.StatusInternalServerError, "failed to update room")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"room": convertRoomSummary(updated),
	})
}

func handleRoomMembersPatch(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	var req struct {
		Members []RoomMemberRequest `json:"members"`
	}
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	workspaceID := roomWorkspaceID(r)
	if workspaceID == "" {
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

	if _, err := store.EnsureRoom(r.Context(), workspaceID, roomID, roomID); err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to ensure room before members patch")
		httpError(w, http.StatusInternalServerError, "failed to update room members")
		return
	}
	if _, err := store.ReplaceRoomMembers(r.Context(), workspaceID, roomID, toRoomMembers(req.Members)); err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to replace room members")
		httpError(w, http.StatusInternalServerError, "failed to update room members")
		return
	}

	updated, err := store.GetRoom(r.Context(), workspaceID, roomID, "")
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to reload room after members patch")
		httpError(w, http.StatusInternalServerError, "failed to update room members")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"room": convertRoomSummary(updated),
	})
}

// RoomDetailHandler serves /api/rooms/{id} and room control subroutes.
func RoomDetailHandler(cfg config.Config, log zerolog.Logger, events roomEventPublisher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/rooms/")
		parts := strings.Split(path, "/")
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			httpError(w, http.StatusBadRequest, "room id required")
			return
		}
		roomID := strings.TrimSpace(parts[0])

		if len(parts) >= 2 && parts[1] == "status" {
			if r.Method != http.MethodGet {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			handleRoomStatusGet(w, r, cfg, log, roomID)
			return
		}

		if len(parts) >= 2 && parts[1] == "inbox" {
			if r.Method != http.MethodGet {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			handleRoomInboxGet(w, r, cfg, log, roomID)
			return
		}

		if len(parts) >= 2 && parts[1] == "tasks" {
			if len(parts) >= 4 {
				switch r.Method {
				case http.MethodPost:
					handleRoomTaskAction(w, r, cfg, log, roomID, strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3]))
				default:
					httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				}
				return
			}
			if r.Method != http.MethodGet {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			handleRoomTasksGet(w, r, cfg, log, roomID)
			return
		}

		if len(parts) >= 2 && parts[1] == "loop" {
			if r.Method != http.MethodGet {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			handleRoomLoopGet(w, r, cfg, log, roomID)
			return
		}

		if len(parts) >= 2 && parts[1] == "coordinator" {
			if r.Method != http.MethodPost {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			handleRoomCoordinatorSet(w, r, cfg, log, roomID)
			return
		}

		if len(parts) >= 2 && parts[1] == "messages" {
			if len(parts) >= 3 && parts[2] == "resolve" {
				if r.Method != http.MethodPost {
					httpError(w, http.StatusMethodNotAllowed, "method not allowed")
					return
				}
				handleRoomMessagesResolveBulk(w, r, cfg, log, roomID)
				return
			}
			if len(parts) >= 4 {
				switch r.Method {
				case http.MethodPost:
					handleRoomMessageAction(w, r, cfg, log, roomID, strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3]))
				default:
					httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				}
				return
			}
			switch r.Method {
			case http.MethodGet:
				handleRoomMessagesGet(w, r, cfg, log, roomID)
			case http.MethodPost:
				handleRoomMessagesPost(w, r, cfg, log, events, roomID)
			default:
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
			return
		}

		if len(parts) >= 2 && parts[1] == "members" {
			switch r.Method {
			case http.MethodGet:
				handleRoomGet(w, r, cfg, log, roomID)
			case http.MethodPatch:
				handleRoomMembersPatch(w, r, cfg, log, roomID)
			default:
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleRoomGet(w, r, cfg, log, roomID)
		case http.MethodPatch:
			handleRoomPatch(w, r, cfg, log, roomID)
		default:
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func handleRoomGet(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(r.URL.Query().Get("workspace"))
	}
	if workspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id required")
		return
	}
	actorID := strings.TrimSpace(r.URL.Query().Get("actor_id"))

	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()

	room, err := store.GetRoom(r.Context(), workspaceID, roomID, actorID)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to get room")
		httpError(w, http.StatusInternalServerError, "failed to get room")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"room": convertRoomSummary(room),
	})
}

func handleRoomMessagesGet(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(r.URL.Query().Get("workspace"))
	}
	if workspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id required")
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 1000 {
			httpError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = n
	}

	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()

	messages, err := store.ListRoomMessages(r.Context(), workspaceID, roomID, limit)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to list room messages")
		httpError(w, http.StatusInternalServerError, "failed to list room messages")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"room_id":  roomID,
		"stream":   agent.RoomStreamName(roomID),
		"messages": convertBoardMessages(messages),
		"count":    len(messages),
	})
}

func handleRoomMessagesPost(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, events roomEventPublisher, roomID string) {
	var req RoomMessageSendRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.Sender = strings.TrimSpace(req.Sender)
	req.Recipient = strings.TrimSpace(req.Recipient)
	req.Subject = strings.TrimSpace(req.Subject)
	req.Body = strings.TrimSpace(req.Body)
	req.TaskID = strings.TrimSpace(req.TaskID)
	if req.WorkspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.Sender == "" {
		httpError(w, http.StatusBadRequest, "sender is required")
		return
	}
	if req.Body == "" {
		httpError(w, http.StatusBadRequest, "body is required")
		return
	}

	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()

	kind, err := normalizeBoardMessageKind(req.Kind)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	priority := req.Priority
	if priority == 0 {
		priority = agent.DefaultPriority
	} else if priority < 1 || priority > 5 {
		httpError(w, http.StatusBadRequest, "invalid priority: must be between 1 (highest) and 5 (lowest)")
		return
	}

	if req.Recipient == "" {
		req.Recipient = agent.BroadcastRecipient
	}
	if req.ReplyExpected && req.Recipient == agent.BroadcastRecipient {
		httpError(w, http.StatusBadRequest, "reply_expected requires a direct recipient")
		return
	}
	if req.Subject == "" {
		req.Subject = agent.RoomStreamName(roomID)
	}
	if _, err := store.EnsureRoom(r.Context(), req.WorkspaceID, roomID, req.Subject); err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to ensure room before message send")
		httpError(w, http.StatusInternalServerError, "failed to send room message")
		return
	}

	msg := &agent.BoardMessage{
		ID:            uuid.New().String(),
		WorkspaceID:   req.WorkspaceID,
		TaskID:        req.TaskID,
		Stream:        agent.RoomStreamName(roomID),
		Sender:        req.Sender,
		Recipient:     req.Recipient,
		Subject:       req.Subject,
		Body:          req.Body,
		Kind:          kind,
		Priority:      priority,
		Status:        agent.BoardMessageStatusUnread,
		AckRequired:   req.AckRequired,
		ReplyExpected: req.ReplyExpected,
		CreatedAt:     time.Now(),
	}
	if err := store.SendMessage(r.Context(), msg); err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to send room message")
		httpError(w, http.StatusInternalServerError, "failed to send room message")
		return
	}
	publishRoomMessageEvent(events, roomMessageEvent{
		WorkspaceID: req.WorkspaceID,
		RoomID:      roomID,
		Stream:      msg.Stream,
		MessageID:   msg.ID,
		Sender:      msg.Sender,
		Recipient:   msg.Recipient,
		Subject:     msg.Subject,
		Phase:       "sent",
	})

	boardUpdated := false
	if applied, err := maybeApplyRoomBoardAction(r.Context(), cfg, log, req.WorkspaceID, req.TaskID, req.Context, req.Body); err != nil {
		log.Warn().Err(err).Str("room_id", roomID).Msg("failed to apply room board action")
	} else {
		boardUpdated = applied
	}

	dispatched := 0
	skipped := 0
	if req.DispatchAgents {
		room, err := store.GetRoom(r.Context(), req.WorkspaceID, roomID, "")
		if err != nil {
			log.Warn().Err(err).Str("room_id", roomID).Msg("failed to load room members for agent dispatch")
		} else {
			targetIDs := normalizeRoomDispatchAgentIDs(req.DispatchAgentIDs)
			explicitTargets := len(targetIDs) > 0
			if len(targetIDs) == 0 {
				var explicit bool
				targetIDs, explicit = roomDefaultDispatchAgentIDs(room)
				explicitTargets = explicit
			}
			if explicitTargets && len(targetIDs) == 0 {
				writeJSON(w, http.StatusCreated, RoomMessageSendResponse{
					ID:         msg.ID,
					RoomID:     roomID,
					Stream:     msg.Stream,
					Status:     "sent",
					Message:    "Room message sent successfully",
					Dispatched: 0,
					Skipped:    0,
				})
				return
			}
			dispatched, skipped = queueRoomAgentReplies(cfg, log, events, room, roomAgentDispatchRequest{
				WorkspaceID: req.WorkspaceID,
				RoomID:      roomID,
				Stream:      msg.Stream,
				Sender:      req.Sender,
				Subject:     msg.Subject,
				Body:        req.Body,
				TaskID:      req.TaskID,
				Context:     req.Context,
			}, targetIDs)
		}
	}

	writeJSON(w, http.StatusCreated, RoomMessageSendResponse{
		ID:         msg.ID,
		RoomID:     roomID,
		Stream:     msg.Stream,
		Status:     "sent",
		Message:    roomSendStatusMessage(boardUpdated),
		Dispatched: dispatched,
		Skipped:    skipped,
	})
}

func queueRoomAgentReplies(cfg config.Config, log zerolog.Logger, events roomEventPublisher, room agent.RoomSummary, req roomAgentDispatchRequest, requestedIDs []string) (int, int) {
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		log.Warn().Err(err).Str("room_id", room.ID).Msg("failed to open agents store for room dispatch")
		return 0, len(uniqueRoomAgentIDs(room, requestedIDs))
	}
	defer func() {
		_ = store.Close()
	}()

	targetIDs := uniqueRoomAgentIDs(room, requestedIDs)
	dispatched := 0
	skipped := 0
	for _, targetID := range targetIDs {
		if targetID == "" || targetID == strings.TrimSpace(req.Sender) {
			skipped++
			continue
		}
		target, err := store.Get(context.Background(), targetID)
		if err != nil {
			skipped++
			publishRoomMessageEvent(events, roomMessageEvent{
				WorkspaceID: req.WorkspaceID,
				RoomID:      req.RoomID,
				Stream:      req.Stream,
				AgentID:     targetID,
				Phase:       "agent_error",
				Error:       "agent not found",
			})
			continue
		}
		if target.State != agent.StateRunning {
			skipped++
			publishRoomMessageEvent(events, roomMessageEvent{
				WorkspaceID: req.WorkspaceID,
				RoomID:      req.RoomID,
				Stream:      req.Stream,
				AgentID:     target.ID,
				Phase:       "agent_error",
				Error:       "agent is not running",
			})
			continue
		}
		dispatched++
		go func(target agent.Agent) {
			if err := runRoomAgentReply(context.Background(), cfg, log, target, req, events); err != nil {
				log.Warn().Err(err).Str("room_id", req.RoomID).Str("agent_id", target.ID).Msg("room agent reply failed")
			}
		}(target)
	}
	return dispatched, skipped
}

func uniqueRoomAgentIDs(room agent.RoomSummary, requestedIDs []string) []string {
	candidates := requestedIDs
	if len(candidates) == 0 {
		candidates = make([]string, 0, len(room.Members))
		for _, member := range room.Members {
			candidates = append(candidates, member.ActorID)
		}
	}
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func roomDefaultDispatchAgentIDs(room agent.RoomSummary) ([]string, bool) {
	switch normalizeRoomDispatchPolicy(room.DispatchPolicy) {
	case "selected":
		return normalizeRoomDispatchAgentIDs(room.DispatchAgentIDs), true
	case "lead_only":
		if targets := normalizeRoomDispatchAgentIDs(room.DispatchAgentIDs); len(targets) > 0 {
			return []string{targets[0]}, true
		}
		if leadID := roomLeadDispatchAgentID(room); leadID != "" {
			return []string{leadID}, true
		}
		return nil, true
	case "children_only":
		if targets := normalizeRoomDispatchAgentIDs(room.DispatchAgentIDs); len(targets) > 0 {
			return targets, true
		}
		leadID := roomLeadDispatchAgentID(room)
		out := make([]string, 0, len(room.Members))
		for _, member := range room.Members {
			if strings.TrimSpace(member.ActorID) == "" || strings.TrimSpace(member.ActorID) == leadID {
				continue
			}
			out = append(out, member.ActorID)
		}
		return normalizeRoomDispatchAgentIDs(out), true
	default:
		return nil, false
	}
}

func roomLeadDispatchAgentID(room agent.RoomSummary) string {
	for _, member := range room.Members {
		role := strings.ToLower(strings.TrimSpace(member.Role))
		if role == "lead" || role == "owner" || role == "overseer" || role == "companion" {
			return strings.TrimSpace(member.ActorID)
		}
	}
	if len(room.Members) == 0 {
		return ""
	}
	return strings.TrimSpace(room.Members[0].ActorID)
}

func defaultRoomAgentReply(_ context.Context, cfg config.Config, log zerolog.Logger, target agent.Agent, req roomAgentDispatchRequest, events roomEventPublisher) error {
	timeout := 30 * time.Minute
	if target.Policy.Timeout != "" {
		if d, err := time.ParseDuration(target.Policy.Timeout); err == nil && d > 0 {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	correlationID := uuid.New().String()
	subject := fmt.Sprintf("Reply from %s", roomAgentLabel(target))

	publishRoomMessageEvent(events, roomMessageEvent{
		WorkspaceID:   req.WorkspaceID,
		RoomID:        req.RoomID,
		Stream:        req.Stream,
		CorrelationID: correlationID,
		AgentID:       target.ID,
		Sender:        target.ID,
		Subject:       subject,
		Phase:         "agent_started",
	})

	svc, cleanup, err := buildAgentCompanionService(ctx, cfg, log, target)
	if err != nil {
		publishRoomMessageEvent(events, roomMessageEvent{
			WorkspaceID: req.WorkspaceID,
			RoomID:      req.RoomID,
			Stream:      req.Stream,
			AgentID:     target.ID,
			Phase:       "agent_error",
			Error:       err.Error(),
		})
		return fmt.Errorf("initialize companion service: %w", err)
	}
	defer cleanup()

	resp, err := svc.ChatStreaming(ctx, companion.ChatRequest{
		ConversationID: resolveAgentConversationID(target, ""),
		Message:        buildRoomAgentPrompt(req),
		Context:        buildRoomAgentContext(req, target),
	}, companion.ChatStreamCallbacks{
		OnDelta: func(delta companion.ChatStreamDelta) {
			if strings.TrimSpace(delta.ContentDelta) == "" {
				return
			}
			publishRoomMessageEvent(events, roomMessageEvent{
				WorkspaceID:   req.WorkspaceID,
				RoomID:        req.RoomID,
				Stream:        req.Stream,
				CorrelationID: correlationID,
				AgentID:       target.ID,
				Sender:        target.ID,
				Subject:       subject,
				Phase:         "agent_delta",
				ContentDelta:  delta.ContentDelta,
			})
		},
		OnToolCall: func(call companion.ChatToolCallEvent) {
			publishRoomMessageEvent(events, roomMessageEvent{
				WorkspaceID:   req.WorkspaceID,
				RoomID:        req.RoomID,
				Stream:        req.Stream,
				CorrelationID: correlationID,
				AgentID:       target.ID,
				Sender:        target.ID,
				Subject:       subject,
				Phase:         "agent_tool_call",
				ToolCallID:    call.ID,
				ToolName:      call.Name,
			})
		},
		OnToolResult: func(result companion.ChatToolResultEvent) {
			publishRoomMessageEvent(events, roomMessageEvent{
				WorkspaceID:   req.WorkspaceID,
				RoomID:        req.RoomID,
				Stream:        req.Stream,
				CorrelationID: correlationID,
				AgentID:       target.ID,
				Sender:        target.ID,
				Subject:       subject,
				Phase:         "agent_tool_result",
				ToolCallID:    result.ToolCallID,
				ToolName:      result.Name,
				ToolOutput:    truncateAgentChatPayload(result.Content, 1024),
				IsError:       result.IsError,
			})
		},
	})
	if err != nil {
		publishRoomMessageEvent(events, roomMessageEvent{
			WorkspaceID:   req.WorkspaceID,
			RoomID:        req.RoomID,
			Stream:        req.Stream,
			CorrelationID: correlationID,
			AgentID:       target.ID,
			Sender:        target.ID,
			Subject:       subject,
			Phase:         "agent_error",
			Error:         err.Error(),
		})
		return fmt.Errorf("room agent chat: %w", err)
	}

	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		publishRoomMessageEvent(events, roomMessageEvent{
			WorkspaceID: req.WorkspaceID,
			RoomID:      req.RoomID,
			Stream:      req.Stream,
			AgentID:     target.ID,
			Phase:       "agent_error",
			Error:       err.Error(),
		})
		return fmt.Errorf("open board store: %w", err)
	}
	defer func() {
		_ = store.Close()
	}()

	reply := &agent.BoardMessage{
		ID:          uuid.New().String(),
		WorkspaceID: req.WorkspaceID,
		TaskID:      req.TaskID,
		Stream:      req.Stream,
		Sender:      target.ID,
		Recipient:   agent.BroadcastRecipient,
		Subject:     subject,
		Body:        strings.TrimSpace(resp.Response),
		Kind:        agent.BoardMessageKindInfo,
		Priority:    agent.DefaultPriority,
		Status:      agent.BoardMessageStatusUnread,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.SendMessage(ctx, reply); err != nil {
		publishRoomMessageEvent(events, roomMessageEvent{
			WorkspaceID: req.WorkspaceID,
			RoomID:      req.RoomID,
			Stream:      req.Stream,
			AgentID:     target.ID,
			Phase:       "agent_error",
			Error:       err.Error(),
		})
		return fmt.Errorf("store room reply: %w", err)
	}

	publishRoomMessageEvent(events, roomMessageEvent{
		WorkspaceID:   req.WorkspaceID,
		RoomID:        req.RoomID,
		Stream:        req.Stream,
		MessageID:     reply.ID,
		CorrelationID: correlationID,
		Sender:        reply.Sender,
		Recipient:     reply.Recipient,
		Subject:       reply.Subject,
		AgentID:       target.ID,
		Content:       reply.Body,
		Phase:         "agent_completed",
	})
	if _, err := maybeApplyRoomBoardAction(ctx, cfg, log, req.WorkspaceID, req.TaskID, req.Context, reply.Body); err != nil {
		log.Warn().Err(err).Str("room_id", req.RoomID).Str("agent_id", target.ID).Msg("failed to apply room board action from agent reply")
	}
	return nil
}

func buildRoomAgentPrompt(req roomAgentDispatchRequest) string {
	var b strings.Builder
	b.WriteString("You are replying inside a shared control room.\n")
	issueID := chooseNonEmpty(stringValueFromAnyMap(req.Context, "issue_id"), req.TaskID)
	if req.RoomID != "" {
		fmt.Fprintf(&b, "Room: %s\n", req.RoomID)
	}
	if issueID != "" {
		fmt.Fprintf(&b, "Issue: %s\n", issueID)
	}
	if req.Sender != "" {
		fmt.Fprintf(&b, "Sender: %s\n", req.Sender)
	}
	if strings.TrimSpace(req.Subject) != "" {
		fmt.Fprintf(&b, "Subject: %s\n", strings.TrimSpace(req.Subject))
	}
	b.WriteString("\nLatest room message:\n")
	b.WriteString(strings.TrimSpace(req.Body))
	b.WriteString("\n\nReply directly to the room with a concise coordination update.")
	if truthyAnyMapValue(req.Context, "final_conclusion") && issueID != "" {
		fmt.Fprintf(&b, "\nIf you conclude the issue is complete, start your reply with exactly: ROOM-BOARD-DONE %s:", issueID)
	}
	return b.String()
}

func buildRoomAgentContext(req roomAgentDispatchRequest, target agent.Agent) map[string]any {
	ctx := map[string]any{
		"room_dispatch": true,
		"workspace_id":  req.WorkspaceID,
		"room_id":       req.RoomID,
		"room_sender":   req.Sender,
		"room_subject":  req.Subject,
		"agent_id":      target.ID,
		"agent_role":    target.Role,
	}
	if req.TaskID != "" {
		ctx["task_id"] = req.TaskID
	}
	for key, value := range req.Context {
		if _, exists := ctx[key]; exists {
			continue
		}
		ctx[key] = value
	}
	return ctx
}

func roomAgentLabel(target agent.Agent) string {
	if name := strings.TrimSpace(target.Name); name != "" {
		return name
	}
	return target.ID
}

func maybeApplyRoomBoardAction(ctx context.Context, cfg config.Config, log zerolog.Logger, workspaceID, taskID string, payloadCtx map[string]any, body string) (bool, error) {
	action, ok := resolveRoomBoardAction(taskID, payloadCtx, body)
	if !ok {
		return false, nil
	}

	_, err := applyOrchestrationCardAction(ctx, cfg, log, orchestrationCardActionRequest{
		RequestID:   uuid.New().String(),
		WorkspaceID: strings.TrimSpace(workspaceID),
		IssueID:     action.IssueID,
		Action:      action.Action,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

func resolveRoomBoardAction(taskID string, payloadCtx map[string]any, body string) (roomBoardAction, bool) {
	ctxIssueID := strings.TrimSpace(stringValueFromAnyMap(payloadCtx, "issue_id"))
	issueID := chooseNonEmpty(ctxIssueID, strings.TrimSpace(taskID))
	action := normalizeOrchestrationCardAction(stringValueFromAnyMap(payloadCtx, "board_action"))
	if issueID != "" && action != "" {
		return roomBoardAction{IssueID: issueID, Action: action}, true
	}

	body = strings.TrimSpace(body)
	if body == "" {
		return roomBoardAction{}, false
	}

	prefixes := []struct {
		Prefix string
		Action string
	}{
		{Prefix: "ROOM-BOARD-DONE", Action: orchestrationActionMarkDone},
		{Prefix: "ROOM-BOARD-RETRY", Action: orchestrationActionRetryNow},
		{Prefix: "ROOM-BOARD-RELEASE", Action: orchestrationActionRelease},
	}
	upperBody := strings.ToUpper(body)
	for _, candidate := range prefixes {
		if !strings.HasPrefix(upperBody, candidate.Prefix) {
			continue
		}
		remainder := strings.TrimSpace(body[len(candidate.Prefix):])
		remainder = strings.TrimLeft(remainder, ":")
		remainder = strings.TrimSpace(remainder)
		markerIssueID := remainder
		if idx := strings.IndexAny(markerIssueID, " \n\t:"); idx >= 0 {
			markerIssueID = markerIssueID[:idx]
		}
		markerIssueID = strings.TrimSpace(markerIssueID)
		issueID = chooseNonEmpty(markerIssueID, issueID)
		if issueID == "" {
			return roomBoardAction{}, false
		}
		return roomBoardAction{IssueID: issueID, Action: candidate.Action}, true
	}

	return roomBoardAction{}, false
}

func stringValueFromAnyMap(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func roomSendStatusMessage(boardUpdated bool) string {
	if boardUpdated {
		return "Room message sent successfully; orchestration card updated"
	}
	return "Room message sent successfully"
}

func truthyAnyMapValue(values map[string]any, key string) bool {
	if len(values) == 0 {
		return false
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return false
	}
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(value))
		return trimmed == "true" || trimmed == "1" || trimmed == "yes"
	case float64:
		return value != 0
	case int:
		return value != 0
	default:
		return strings.TrimSpace(fmt.Sprint(value)) != ""
	}
}

func publishRoomMessageEvent(events roomEventPublisher, event roomMessageEvent) {
	if events == nil {
		return
	}
	events.Publish("room.message", event)
}

func convertRoomSummary(room agent.RoomSummary) RoomResponse {
	resp := RoomResponse{
		ID:               room.ID,
		WorkspaceID:      room.WorkspaceID,
		Stream:           room.Stream,
		Title:            room.Title,
		Description:      room.Description,
		DispatchPolicy:   normalizeRoomDispatchPolicy(room.DispatchPolicy),
		DispatchAgentIDs: normalizeRoomDispatchAgentIDs(room.DispatchAgentIDs),
		LatestSubject:    room.LatestSubject,
		LatestPreview:    room.LatestPreview,
		LatestSender:     room.LatestSender,
		MessageCount:     room.MessageCount,
		UnreadCount:      room.UnreadCount,
		Participants:     room.Participants,
		TaskIDs:          room.TaskIDs,
		Members:          convertRoomMembers(room.Members),
	}
	if !room.CreatedAt.IsZero() {
		resp.CreatedAt = room.CreatedAt.Format(time.RFC3339)
	}
	if !room.UpdatedAt.IsZero() {
		resp.UpdatedAt = room.UpdatedAt.Format(time.RFC3339)
	}
	if !room.LatestMessageAt.IsZero() {
		resp.LatestMessageAt = room.LatestMessageAt.Format(time.RFC3339)
	}
	return resp
}

func normalizeBoardMessageKind(raw string) (agent.BoardMessageKind, error) {
	kind := agent.BoardMessageKind(strings.TrimSpace(raw))
	if kind == "" {
		return agent.BoardMessageKindInfo, nil
	}
	switch kind {
	case agent.BoardMessageKindInstruction, agent.BoardMessageKindInfo,
		agent.BoardMessageKindAlert, agent.BoardMessageKindReviewRequest,
		agent.BoardMessageKindTaskUpdate, agent.BoardMessageKindLeadChange:
		return kind, nil
	default:
		return "", errors.New("invalid kind: must be one of instruction, info, alert, review_request, task_update, lead_change")
	}
}

func convertRoomMembers(members []agent.RoomMember) []RoomMemberResponse {
	out := make([]RoomMemberResponse, 0, len(members))
	for _, member := range members {
		resp := RoomMemberResponse{
			ActorID: member.ActorID,
			Role:    member.Role,
			Backend: member.Backend,
			Session: member.Session,
			PaneID:  member.PaneID,
			Unbound: member.Unbound,
		}
		if !member.JoinedAt.IsZero() {
			resp.JoinedAt = member.JoinedAt.Format(time.RFC3339)
		}
		out = append(out, resp)
	}
	return out
}

func toRoomMembers(members []RoomMemberRequest) []agent.RoomMember {
	out := make([]agent.RoomMember, 0, len(members))
	for _, member := range members {
		actorID := strings.TrimSpace(member.ActorID)
		if actorID == "" {
			continue
		}
		out = append(out, agent.RoomMember{
			ActorID: actorID,
			Role:    strings.TrimSpace(member.Role),
			Backend: strings.ToLower(strings.TrimSpace(member.Backend)),
			Session: strings.TrimSpace(member.Session),
			PaneID:  strings.TrimSpace(member.PaneID),
			Unbound: member.Unbound,
		})
	}
	return out
}

func roomWorkspaceID(r *http.Request) string {
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(r.URL.Query().Get("workspace"))
	}
	return workspaceID
}

func slugRoomID(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if b.Len() == 0 || lastDash {
				continue
			}
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}
