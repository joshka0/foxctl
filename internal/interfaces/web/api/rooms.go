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

	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/orchestration/roomruntime"
	"github.com/joshka0/foxctl/internal/storage/agents"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/coordination"
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
	ArchivedAt       string               `json:"archived_at,omitempty"`
}

type RoomMemberResponse struct {
	ActorID           string                       `json:"actor_id"`
	Role              string                       `json:"role,omitempty"`
	Backend           string                       `json:"backend,omitempty"`
	Session           string                       `json:"session,omitempty"`
	PaneID            string                       `json:"pane_id,omitempty"`
	Unbound           bool                         `json:"unbound,omitempty"`
	JoinedAt          string                       `json:"joined_at,omitempty"`
	TransportEndpoint string                       `json:"transport_endpoint,omitempty"`
	TransportKind     string                       `json:"transport_kind,omitempty"`
	DeliveryBinding   *RoomDeliveryBindingResponse `json:"delivery_binding,omitempty"`
}

type RoomDeliveryBindingResponse struct {
	MuxBackend        string `json:"mux_backend,omitempty"`
	MuxSession        string `json:"mux_session,omitempty"`
	MuxPaneID         string `json:"mux_pane_id,omitempty"`
	TransportEndpoint string `json:"transport_endpoint,omitempty"`
	TransportKind     string `json:"transport_kind,omitempty"`
	SubmitMode        string `json:"submit_mode,omitempty"`
	Health            string `json:"health,omitempty"`
	FallbackPolicy    string `json:"fallback_policy,omitempty"`
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
	ActorID          string   `json:"actor_id,omitempty"`
	Title            *string  `json:"title,omitempty"`
	Description      *string  `json:"description,omitempty"`
	DispatchPolicy   *string  `json:"dispatch_policy,omitempty"`
	DispatchAgentIDs []string `json:"dispatch_agent_ids,omitempty"`
}

type RoomMemberRequest struct {
	ActorID           string                      `json:"actor_id"`
	Role              string                      `json:"role,omitempty"`
	Backend           string                      `json:"backend,omitempty"`
	Session           string                      `json:"session,omitempty"`
	PaneID            string                      `json:"pane_id,omitempty"`
	Unbound           bool                        `json:"unbound,omitempty"`
	TransportEndpoint string                      `json:"transport_endpoint,omitempty"`
	TransportKind     string                      `json:"transport_kind,omitempty"`
	DeliveryBinding   *RoomDeliveryBindingRequest `json:"delivery_binding,omitempty"`
}

type RoomDeliveryBindingRequest struct {
	MuxBackend        string `json:"mux_backend,omitempty"`
	MuxSession        string `json:"mux_session,omitempty"`
	MuxPaneID         string `json:"mux_pane_id,omitempty"`
	TransportEndpoint string `json:"transport_endpoint,omitempty"`
	TransportKind     string `json:"transport_kind,omitempty"`
	SubmitMode        string `json:"submit_mode,omitempty"`
	Health            string `json:"health,omitempty"`
	FallbackPolicy    string `json:"fallback_policy,omitempty"`
}

// RoomMessageSendRequest sends one message into a room-scoped stream.
type RoomMessageSendRequest struct {
	WorkspaceID      string         `json:"workspace_id"`
	Sender           string         `json:"sender"`
	Recipient        string         `json:"recipient,omitempty"`
	RelatedMessageID string         `json:"related_message_id,omitempty"`
	Subject          string         `json:"subject,omitempty"`
	Body             string         `json:"body"`
	Kind             string         `json:"kind,omitempty"`
	Priority         int            `json:"priority,omitempty"`
	AckRequired      bool           `json:"ack_required,omitempty"`
	ReplyExpected    bool           `json:"reply_expected,omitempty"`
	Interrupt        bool           `json:"interrupt,omitempty"`
	TaskID           string         `json:"task_id,omitempty"`
	DispatchAgents   bool           `json:"dispatch_agents,omitempty"`
	DispatchAgentIDs []string       `json:"dispatch_agent_ids,omitempty"`
	Context          map[string]any `json:"context,omitempty"`
}

// RoomMessageSendResponse reports the created room message.
type RoomMessageSendResponse struct {
	ID              string                `json:"id"`
	RoomID          string                `json:"room_id"`
	Stream          string                `json:"stream"`
	Status          string                `json:"status"`
	Message         string                `json:"message,omitempty"`
	Dispatched      int                   `json:"dispatched,omitempty"`
	Skipped         int                   `json:"skipped,omitempty"`
	DeliveryOwner   string                `json:"delivery_owner,omitempty"`
	DeliveryPending bool                  `json:"delivery_pending,omitempty"`
	LiveRelay       []RoomLiveRelayResult `json:"live_relay,omitempty"`
}

type RoomReminderRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	Sender        string `json:"sender"`
	Recipient     string `json:"recipient"`
	Subject       string `json:"subject,omitempty"`
	Body          string `json:"body"`
	TaskID        string `json:"task_id,omitempty"`
	StoryID       string `json:"story_id,omitempty"`
	MilestoneID   string `json:"milestone_id,omitempty"`
	Every         string `json:"every"`
	MaxIterations int    `json:"max_iterations,omitempty"`
	AckRequired   bool   `json:"ack_required,omitempty"`
	ReplyExpected bool   `json:"reply_expected,omitempty"`
	Interrupt     bool   `json:"interrupt,omitempty"`
	Passive       bool   `json:"passive,omitempty"`
	AllowPassive  bool   `json:"allow_passive,omitempty"`
}

type RoomReminderResponse struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspace_id"`
	RoomID        string `json:"room_id"`
	RootMessageID string `json:"root_message_id"`
	TaskID        string `json:"task_id,omitempty"`
	StoryID       string `json:"story_id,omitempty"`
	MilestoneID   string `json:"milestone_id,omitempty"`
	Sender        string `json:"sender"`
	Recipient     string `json:"recipient"`
	Subject       string `json:"subject"`
	Body          string `json:"body"`
	AckRequired   bool   `json:"ack_required"`
	ReplyExpected bool   `json:"reply_expected"`
	Interrupt     bool   `json:"interrupt"`
	Passive       bool   `json:"passive"`
	Interval      string `json:"interval"`
	MaxIterations int    `json:"max_iterations"`
	SentCount     int    `json:"sent_count"`
	Active        bool   `json:"active"`
	LastSentAt    string `json:"last_sent_at,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

type RoomLiveRelayResult struct {
	Backend        string   `json:"backend"`
	DeliveredCount int      `json:"delivered_count,omitempty"`
	FailedCount    int      `json:"failed_count,omitempty"`
	DeliveredTo    []string `json:"delivered_to,omitempty"`
	FailedMembers  []string `json:"failed_members,omitempty"`
	SkippedMembers []string `json:"skipped_members,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type roomEventPublisher interface {
	Publish(eventType string, data any)
}

type roomTopicEventPublisher interface {
	roomEventPublisher
	PublishTopic(topic, eventType string, data any)
}

type roomEventStreamer interface {
	roomEventPublisher
	TopicHandler(topics ...string) http.HandlerFunc
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

type roomInvalidationEvent struct {
	WorkspaceID string `json:"workspace_id"`
	RoomID      string `json:"room_id"`
	Stream      string `json:"stream,omitempty"`
	Mutation    string `json:"mutation,omitempty"`
	Action      string `json:"action,omitempty"`
	ActorID     string `json:"actor_id,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	ReminderID  string `json:"reminder_id,omitempty"`
	MemberID    string `json:"member_id,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
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

var roomSendLiveRelayHook func(context.Context, string, string, string) ([]RoomLiveRelayResult, error)

func RoomSendLiveRelayHookForTests() func(context.Context, string, string, string) ([]RoomLiveRelayResult, error) {
	return roomSendLiveRelayHook
}

func SetRoomSendLiveRelayHookForTests(hook func(context.Context, string, string, string) ([]RoomLiveRelayResult, error)) {
	roomSendLiveRelayHook = hook
}

var runRoomAgentReply = defaultRoomAgentReply

const apiRoomLoopHeartbeatGrace = 15 * time.Second

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

		archivedOnly := parseBool(r.URL.Query().Get("archived_only"))
		rooms, err := store.ListRooms(r.Context(), workspaceID, actorID, limit, archivedOnly)
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
	req.ActorID = strings.TrimSpace(req.ActorID)

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
	if req.ActorID == "" {
		httpError(w, http.StatusBadRequest, "actor_id required")
		return
	}
	if !apiRoomActorHasCoordinatorAccess(current.Members, req.ActorID) {
		httpError(w, http.StatusForbidden, "room patch requires coordinator role")
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
		ActorID string              `json:"actor_id"`
		Members []RoomMemberRequest `json:"members"`
	}
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.ActorID = strings.TrimSpace(req.ActorID)

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

	room, err := store.GetRoom(r.Context(), workspaceID, roomID, req.ActorID)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room before members patch")
		httpError(w, http.StatusInternalServerError, "failed to update room members")
		return
	}
	if req.ActorID == "" {
		httpError(w, http.StatusBadRequest, "actor_id required")
		return
	}
	if !apiRoomActorHasCoordinatorAccess(room.Members, req.ActorID) {
		httpError(w, http.StatusForbidden, "room members patch requires coordinator role")
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
		roomID, parts, ok := roomRequestParts(r)
		if !ok {
			httpError(w, http.StatusBadRequest, "room id required")
			return
		}
		if len(parts) >= 2 && handleRoomSubresourceRoute(w, r, cfg, log, events, roomID, parts[1:]) {
			return
		}
		handleRoomRootRoute(w, r, cfg, log, roomID)
	}
}

func roomRequestParts(r *http.Request) (string, []string, bool) {
	path := strings.TrimPrefix(r.URL.Path, "/api/rooms/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return "", nil, false
	}
	roomID := strings.TrimSpace(parts[0])
	if roomID == "" {
		return "", nil, false
	}
	return roomID, parts, true
}

func handleRoomSubresourceRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, events roomEventPublisher, roomID string, parts []string) bool {
	switch parts[0] {
	case "events":
		handleRoomGetOnly(w, r, func() { handleRoomEventsGet(w, r, roomID, events) })
	case "control-snapshot":
		handleRoomGetOnly(w, r, func() { handleRoomControlSnapshotGet(w, r, cfg, log, roomID) })
	case "status":
		handleRoomGetOnly(w, r, func() { handleRoomStatusGet(w, r, cfg, log, roomID) })
	case "inbox":
		handleRoomGetOnly(w, r, func() { handleRoomInboxGet(w, r, cfg, log, roomID) })
	case "tasks":
		handleRoomTasksRoute(w, r, cfg, log, events, roomID, parts)
	case "loop":
		handleRoomLoopRoute(w, r, cfg, log, events, roomID)
	case "coordinator":
		handleRoomPostOnly(w, r, func() { handleRoomCoordinatorSet(w, r, cfg, log, roomID) })
	case "messages":
		handleRoomMessagesRoute(w, r, cfg, log, events, roomID, parts)
	case "reminders":
		handleRoomRemindersRoute(w, r, cfg, log, roomID, parts)
	case "archive":
		handleRoomPostOnly(w, r, func() { handleRoomArchive(w, r, cfg, log, roomID) })
	case "restore":
		handleRoomPostOnly(w, r, func() { handleRoomRestore(w, r, cfg, log, roomID) })
	case "members":
		// /members/{actor_id}/transport or /members/{actor_id}/binding
		if len(parts) >= 3 && strings.TrimSpace(parts[2]) == "transport" {
			actorID := strings.TrimSpace(parts[1])
			handleRoomPutOnly(w, r, func() { handleRoomMemberTransportPut(w, r, cfg, log, roomID, actorID) })
		} else if len(parts) >= 3 && strings.TrimSpace(parts[2]) == "binding" {
			actorID := strings.TrimSpace(parts[1])
			handleRoomPutOnly(w, r, func() { handleRoomMemberBindingPut(w, r, cfg, log, roomID, actorID) })
		} else {
			handleRoomMembersRoute(w, r, cfg, log, roomID)
		}
	default:
		return false
	}
	return true
}

func handleRoomEventsGet(w http.ResponseWriter, r *http.Request, roomID string, events roomEventPublisher) {
	workspaceID := roomWorkspaceID(r)
	if workspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	streamer, ok := events.(roomEventStreamer)
	if !ok {
		httpError(w, http.StatusServiceUnavailable, "room event stream unavailable")
		return
	}
	streamer.TopicHandler(roomEventTopic(workspaceID, roomID)).ServeHTTP(w, r)
}

func handleRoomTasksRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, events roomEventPublisher, roomID string, parts []string) {
	if len(parts) >= 3 {
		handleRoomPostOnly(w, r, func() {
			handleRoomTaskAction(w, r, cfg, log, events, roomID, strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]))
		})
		return
	}
	switch r.Method {
	case http.MethodGet:
		handleRoomTasksGet(w, r, cfg, log, roomID)
	case http.MethodPost:
		handleRoomTasksPost(w, r, cfg, log, roomID)
	default:
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleRoomLoopRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, events roomEventPublisher, roomID string) {
	switch r.Method {
	case http.MethodGet:
		handleRoomLoopGet(w, r, cfg, log, roomID)
	case http.MethodPatch:
		handleRoomLoopPatch(w, r, cfg, log, events, roomID)
	default:
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleRoomMessagesRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, events roomEventPublisher, roomID string, parts []string) {
	if len(parts) >= 2 && parts[1] == "resolve" {
		handleRoomPostOnly(w, r, func() { handleRoomMessagesResolveBulk(w, r, cfg, log, roomID) })
		return
	}
	if len(parts) >= 3 {
		handleRoomPostOnly(w, r, func() {
			handleRoomMessageAction(w, r, cfg, log, roomID, strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]))
		})
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
}

func handleRoomMembersRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	switch r.Method {
	case http.MethodGet:
		handleRoomGet(w, r, cfg, log, roomID)
	case http.MethodPatch:
		handleRoomMembersPatch(w, r, cfg, log, roomID)
	default:
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleRoomRemindersRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string, parts []string) {
	if len(parts) >= 3 && strings.TrimSpace(parts[2]) == "cancel" {
		handleRoomPostOnly(w, r, func() {
			handleRoomReminderCancel(w, r, cfg, log, roomID, strings.TrimSpace(parts[1]))
		})
		return
	}
	switch r.Method {
	case http.MethodGet:
		handleRoomRemindersGet(w, r, cfg, log, roomID)
	case http.MethodPost:
		handleRoomReminderAdd(w, r, cfg, log, roomID)
	default:
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleRoomRootRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	switch r.Method {
	case http.MethodGet:
		handleRoomGet(w, r, cfg, log, roomID)
	case http.MethodPatch:
		handleRoomPatch(w, r, cfg, log, roomID)
	case http.MethodDelete:
		handleRoomDelete(w, r, cfg, log, roomID)
	default:
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleRoomGetOnly(w http.ResponseWriter, r *http.Request, next func()) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	next()
}

func handleRoomPostOnly(w http.ResponseWriter, r *http.Request, next func()) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	next()
}

func handleRoomPutOnly(w http.ResponseWriter, r *http.Request, next func()) {
	if r.Method != http.MethodPut {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	next()
}

// handleRoomMemberTransportPut handles PUT /api/rooms/{id}/members/{actor_id}/transport.
// It updates transport_endpoint and transport_kind for a single existing room member
// without disturbing other members. Returns 404 if the actor is not a current member.
func handleRoomMemberTransportPut(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID, actorID string) {
	roomID = strings.TrimSpace(roomID)
	actorID = strings.TrimSpace(actorID)
	if roomID == "" || actorID == "" {
		httpError(w, http.StatusBadRequest, "room_id and actor_id required")
		return
	}

	var req struct {
		ActorID           string `json:"actor_id"`
		TransportEndpoint string `json:"transport_endpoint"`
		TransportKind     string `json:"transport_kind"`
	}
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.ActorID = strings.TrimSpace(req.ActorID)

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
	room, err := store.GetRoom(r.Context(), workspaceID, roomID, req.ActorID)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room before transport update")
		httpError(w, http.StatusInternalServerError, "failed to update member transport")
		return
	}
	if req.ActorID == "" {
		httpError(w, http.StatusBadRequest, "actor_id required")
		return
	}
	if !apiSameRoomParticipant(req.ActorID, actorID) && !apiRoomActorHasCoordinatorAccess(room.Members, req.ActorID) {
		httpError(w, http.StatusForbidden, "member transport update requires self or coordinator role")
		return
	}

	if err := store.UpdateRoomMemberTransport(r.Context(), workspaceID, roomID, actorID, req.TransportEndpoint, req.TransportKind); err != nil {
		if errors.Is(err, blackboard.ErrRoomMemberNotFound) {
			httpError(w, http.StatusNotFound, fmt.Sprintf("actor %q is not a member of room %q", actorID, roomID))
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Str("actor_id", actorID).Msg("failed to update member transport")
		httpError(w, http.StatusInternalServerError, "failed to update member transport")
		return
	}

	updated, err := store.GetRoom(r.Context(), workspaceID, roomID, "")
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to reload room after transport update")
		httpError(w, http.StatusInternalServerError, "failed to reload room")
		return
	}

	// Return the updated member record.
	var member *RoomMemberResponse
	for _, m := range convertRoomMembers(updated.Members) {
		m := m
		if m.ActorID == actorID {
			member = &m
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"member": member,
	})
}

func handleRoomMemberBindingPut(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID, actorID string) {
	roomID = strings.TrimSpace(roomID)
	actorID = strings.TrimSpace(actorID)
	if roomID == "" || actorID == "" {
		httpError(w, http.StatusBadRequest, "room_id and actor_id required")
		return
	}

	var req struct {
		ActorID           string                      `json:"actor_id"`
		Role              string                      `json:"role"`
		Backend           string                      `json:"backend"`
		Session           string                      `json:"session"`
		PaneID            string                      `json:"pane_id"`
		Unbound           bool                        `json:"unbound"`
		TransportEndpoint string                      `json:"transport_endpoint"`
		TransportKind     string                      `json:"transport_kind"`
		DeliveryBinding   *RoomDeliveryBindingRequest `json:"delivery_binding"`
	}
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.ActorID = strings.TrimSpace(req.ActorID)

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
	room, err := store.GetRoom(r.Context(), workspaceID, roomID, req.ActorID)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room before binding update")
		httpError(w, http.StatusInternalServerError, "failed to update member binding")
		return
	}
	if req.ActorID == "" {
		httpError(w, http.StatusBadRequest, "actor_id required")
		return
	}
	coordinatorAccess := apiRoomActorHasCoordinatorAccess(room.Members, req.ActorID)
	if !apiSameRoomParticipant(req.ActorID, actorID) && !coordinatorAccess {
		httpError(w, http.StatusForbidden, "member binding update requires self or coordinator role")
		return
	}
	if strings.TrimSpace(req.Role) != "" && !coordinatorAccess {
		httpError(w, http.StatusForbidden, "only the coordinator can change room member roles")
		return
	}

	member := agent.RoomMember{
		ActorID:           actorID,
		Backend:           req.Backend,
		Session:           req.Session,
		PaneID:            req.PaneID,
		Unbound:           req.Unbound,
		TransportEndpoint: req.TransportEndpoint,
		TransportKind:     req.TransportKind,
		DeliveryBinding:   toAgentRoomDeliveryBinding(req.DeliveryBinding),
	}
	if err := store.UpdateRoomMemberBinding(r.Context(), workspaceID, roomID, member); err != nil {
		if errors.Is(err, blackboard.ErrRoomMemberNotFound) {
			httpError(w, http.StatusNotFound, fmt.Sprintf("actor %q is not a member of room %q", actorID, roomID))
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Str("actor_id", actorID).Msg("failed to update member binding")
		httpError(w, http.StatusInternalServerError, "failed to update member binding")
		return
	}

	updated, err := store.GetRoom(r.Context(), workspaceID, roomID, "")
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to reload room after binding update")
		httpError(w, http.StatusInternalServerError, "failed to reload room")
		return
	}

	var memberResp *RoomMemberResponse
	for _, m := range convertRoomMembers(updated.Members) {
		m := m
		if m.ActorID == actorID {
			memberResp = &m
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"member": memberResp,
	})
}

func handleRoomRemindersGet(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	workspaceID := roomWorkspaceID(r)
	if workspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id required")
		return
	}
	includeInactive := parseBool(r.URL.Query().Get("all"))

	store, err := coordination.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open coordination store")
		httpError(w, http.StatusInternalServerError, "failed to open coordination store")
		return
	}
	defer store.Close()

	reminders, err := store.ListRoomReminders(r.Context(), workspaceID, strings.TrimSpace(roomID), includeInactive)
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to list room reminders")
		httpError(w, http.StatusInternalServerError, "failed to list room reminders")
		return
	}
	resp := make([]RoomReminderResponse, 0, len(reminders))
	for _, reminder := range reminders {
		resp = append(resp, convertRoomReminder(reminder))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id":   roomID,
		"count":     len(resp),
		"reminders": resp,
	})
}

func handleRoomReminderAdd(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	var req RoomReminderRequest
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
	req.StoryID = strings.TrimSpace(req.StoryID)
	req.MilestoneID = strings.TrimSpace(req.MilestoneID)
	req.Every = strings.TrimSpace(req.Every)
	if req.WorkspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id required")
		return
	}
	if req.Sender == "" {
		httpError(w, http.StatusBadRequest, "sender required")
		return
	}
	if req.Recipient == "" {
		httpError(w, http.StatusBadRequest, "recipient required")
		return
	}
	if req.Body == "" {
		httpError(w, http.StatusBadRequest, "body required")
		return
	}
	if req.Passive {
		if req.AckRequired || req.ReplyExpected {
			httpError(w, http.StatusBadRequest, "passive reminders cannot require ack_required or reply_expected")
			return
		}
	} else if !req.AckRequired && !req.ReplyExpected {
		httpError(w, http.StatusBadRequest, "reminders require ack_required or reply_expected")
		return
	}
	every, err := time.ParseDuration(req.Every)
	if err != nil || every <= 0 {
		httpError(w, http.StatusBadRequest, "every must be a positive duration")
		return
	}
	if req.MaxIterations <= 0 {
		req.MaxIterations = 3
	}

	boardStore, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer boardStore.Close()

	room, err := boardStore.GetRoom(r.Context(), req.WorkspaceID, strings.TrimSpace(roomID), "")
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room before reminder add")
		httpError(w, http.StatusInternalServerError, "failed to load room")
		return
	}

	recipient, err := apiResolveReminderRecipient(room, req.Recipient)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Subject == "" {
		req.Subject = deriveAPIRoomSubject(req.Body)
	}

	coordStore, err := coordination.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open coordination store")
		httpError(w, http.StatusInternalServerError, "failed to open coordination store")
		return
	}
	defer coordStore.Close()
	if !req.AllowPassive {
		if err := requireActiveRoomLoopAPI(r.Context(), coordStore, req.WorkspaceID, strings.TrimSpace(roomID), time.Now().UTC()); err != nil {
			httpError(w, http.StatusConflict, err.Error())
			return
		}
	}
	candidate := coordination.RoomReminder{
		WorkspaceID:   req.WorkspaceID,
		RoomID:        strings.TrimSpace(roomID),
		TaskID:        req.TaskID,
		StoryID:       req.StoryID,
		MilestoneID:   req.MilestoneID,
		Sender:        req.Sender,
		Recipient:     recipient,
		Subject:       req.Subject,
		Body:          req.Body,
		AckRequired:   req.AckRequired,
		ReplyExpected: req.ReplyExpected,
		Interrupt:     req.Interrupt,
		Passive:       req.Passive,
		Interval:      every,
		MaxIterations: req.MaxIterations,
		Active:        true,
	}
	reminders, err := coordStore.ListRoomReminders(r.Context(), req.WorkspaceID, strings.TrimSpace(roomID), false)
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to list reminders")
		httpError(w, http.StatusInternalServerError, "failed to persist reminder")
		return
	}
	if existing := apiFindEquivalentActiveReminder(reminders, candidate); existing != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"room_id":          roomID,
			"reminder":         existing,
			"recipient":        recipient,
			"delivery_owner":   "room_loop",
			"delivery_pending": false,
			"deduped":          true,
		})
		return
	}
	root := &agent.BoardMessage{
		WorkspaceID:   req.WorkspaceID,
		Stream:        agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:        req.Sender,
		Recipient:     recipient,
		Kind:          agent.BoardMessageKindInstruction,
		Priority:      agent.DefaultPriority,
		AckRequired:   req.AckRequired,
		ReplyExpected: req.ReplyExpected,
		Interrupt:     req.Interrupt,
		Subject:       req.Subject,
		Body:          req.Body,
		TaskID:        req.TaskID,
	}
	if err := boardStore.SendMessage(r.Context(), root); err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to persist reminder root message")
		httpError(w, http.StatusInternalServerError, "failed to persist reminder root message")
		return
	}
	candidate.ID = root.ID
	candidate.RootMessageID = root.ID
	candidate.LastSentAt = &root.CreatedAt
	reminder, err := coordStore.UpsertRoomReminder(r.Context(), candidate)
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to upsert reminder")
		httpError(w, http.StatusInternalServerError, "failed to persist reminder")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"room_id":          roomID,
		"message":          root,
		"reminder":         convertRoomReminder(reminder),
		"delivery_owner":   "room_loop",
		"delivery_pending": true,
	})
}

func handleRoomReminderCancel(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID, reminderID string) {
	workspaceID := roomWorkspaceID(r)
	if workspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id required")
		return
	}
	var req struct {
		Actor string `json:"actor,omitempty"`
	}
	if r.Body != nil {
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}
	req.Actor = strings.TrimSpace(req.Actor)

	boardStore, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer boardStore.Close()
	room, err := boardStore.GetRoom(r.Context(), workspaceID, strings.TrimSpace(roomID), "")
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room before reminder cancel")
		httpError(w, http.StatusInternalServerError, "failed to load room")
		return
	}

	coordStore, err := coordination.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open coordination store")
		httpError(w, http.StatusInternalServerError, "failed to open coordination store")
		return
	}
	defer coordStore.Close()
	reminder, err := coordStore.GetRoomReminder(r.Context(), workspaceID, strings.TrimSpace(reminderID))
	if err != nil {
		log.Error().Err(err).Str("reminder_id", reminderID).Msg("failed to load reminder")
		httpError(w, http.StatusInternalServerError, "failed to load reminder")
		return
	}
	if reminder == nil || strings.TrimSpace(reminder.RoomID) != strings.TrimSpace(roomID) {
		httpError(w, http.StatusNotFound, fmt.Sprintf("reminder %q not found", reminderID))
		return
	}
	if req.Actor != "" && req.Actor != reminder.Sender && !apiRoomMemberHasRole(room.Members, req.Actor, "coordinator") {
		httpError(w, http.StatusForbidden, "only the reminder sender or room coordinator can cancel this reminder")
		return
	}
	reminder.Active = false
	updated, err := coordStore.UpsertRoomReminder(r.Context(), *reminder)
	if err != nil {
		log.Error().Err(err).Str("reminder_id", reminderID).Msg("failed to cancel reminder")
		httpError(w, http.StatusInternalServerError, "failed to cancel reminder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id":   roomID,
		"cancelled": true,
		"reminder":  convertRoomReminder(updated),
	})
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

func handleRoomDelete(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
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

	if err := store.DeleteRoom(r.Context(), workspaceID, roomID); err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to delete room")
		httpError(w, http.StatusInternalServerError, "failed to delete room")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "deleted",
		"room_id":      roomID,
		"workspace_id": workspaceID,
	})
}

func handleRoomArchive(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
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
	if err := store.ArchiveRoom(r.Context(), workspaceID, roomID); err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to archive room")
		httpError(w, http.StatusInternalServerError, "failed to archive room")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "archived",
		"room_id":      roomID,
		"workspace_id": workspaceID,
	})
}

func handleRoomRestore(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
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
	if err := store.RestoreRoom(r.Context(), workspaceID, roomID); err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to restore room")
		httpError(w, http.StatusInternalServerError, "failed to restore room")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "restored",
		"room_id":      roomID,
		"workspace_id": workspaceID,
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
	req.RelatedMessageID = strings.TrimSpace(req.RelatedMessageID)
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
	if req.Interrupt && req.Recipient == agent.BroadcastRecipient {
		httpError(w, http.StatusBadRequest, "interrupt requires a direct recipient")
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
	coordStore, err := coordination.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open coordination store")
		httpError(w, http.StatusInternalServerError, "failed to open coordination store")
		return
	}
	defer coordStore.Close()
	if err := requireActiveRoomLoopAPI(r.Context(), coordStore, req.WorkspaceID, strings.TrimSpace(roomID), time.Now().UTC()); err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}

	result, err := roomruntime.SendMessage(r.Context(), store, roomruntime.SendMessageInput{
		WorkspaceID:      req.WorkspaceID,
		RoomID:           roomID,
		RoomTitle:        req.Subject,
		Sender:           req.Sender,
		Recipient:        req.Recipient,
		RelatedMessageID: req.RelatedMessageID,
		Subject:          req.Subject,
		Body:             req.Body,
		TaskID:           req.TaskID,
		Kind:             kind,
		Priority:         priority,
		AckRequired:      req.AckRequired,
		ReplyExpected:    req.ReplyExpected,
		Interrupt:        req.Interrupt,
		EnsureRoom:       true,
	})
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to send room message")
		httpError(w, http.StatusInternalServerError, "failed to send room message")
		return
	}
	msg := result.Message
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
					ID:              msg.ID,
					RoomID:          roomID,
					Stream:          msg.Stream,
					Status:          "queued",
					Message:         "Room message queued for room loop delivery",
					Dispatched:      0,
					Skipped:         0,
					DeliveryOwner:   "room_loop",
					DeliveryPending: true,
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
		ID:              msg.ID,
		RoomID:          roomID,
		Stream:          msg.Stream,
		Status:          "queued",
		Message:         roomSendStatusMessage(boardUpdated),
		Dispatched:      dispatched,
		Skipped:         skipped,
		DeliveryOwner:   "room_loop",
		DeliveryPending: true,
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
		return "Room message queued for room loop delivery; orchestration card updated"
	}
	return "Room message queued for room loop delivery"
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
	if topicPublisher, ok := events.(roomTopicEventPublisher); ok {
		topicPublisher.PublishTopic(roomEventTopic(event.WorkspaceID, event.RoomID), "room.message", event)
		return
	}
	events.Publish("room.message", event)
}

func publishRoomInvalidationEvent(events roomEventPublisher, eventType string, event roomInvalidationEvent) {
	if events == nil {
		return
	}
	if topicPublisher, ok := events.(roomTopicEventPublisher); ok {
		topicPublisher.PublishTopic(roomEventTopic(event.WorkspaceID, event.RoomID), eventType, event)
		return
	}
	events.Publish(eventType, event)
}

func roomEventTopic(workspaceID, roomID string) string {
	return fmt.Sprintf("room-event:%s:%s", ws.CanonicalWorkspaceKey(workspaceID), strings.TrimSpace(roomID))
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
	if room.ArchivedAt != nil && !room.ArchivedAt.IsZero() {
		resp.ArchivedAt = room.ArchivedAt.Format(time.RFC3339)
	}
	return resp
}

func apiFindEquivalentActiveReminder(reminders []coordination.RoomReminder, candidate coordination.RoomReminder) *coordination.RoomReminder {
	for i := range reminders {
		reminder := reminders[i]
		if !reminder.Active {
			continue
		}
		if !apiSameReminderContract(reminder, candidate) {
			continue
		}
		dup := reminder
		return &dup
	}
	return nil
}

func apiSameReminderContract(a, b coordination.RoomReminder) bool {
	return ws.CanonicalID(strings.TrimSpace(a.WorkspaceID)) == ws.CanonicalID(strings.TrimSpace(b.WorkspaceID)) &&
		strings.TrimSpace(a.RoomID) == strings.TrimSpace(b.RoomID) &&
		strings.TrimSpace(a.Recipient) == strings.TrimSpace(b.Recipient) &&
		strings.TrimSpace(a.Subject) == strings.TrimSpace(b.Subject) &&
		strings.TrimSpace(a.Body) == strings.TrimSpace(b.Body) &&
		strings.TrimSpace(a.TaskID) == strings.TrimSpace(b.TaskID) &&
		strings.TrimSpace(a.StoryID) == strings.TrimSpace(b.StoryID) &&
		strings.TrimSpace(a.MilestoneID) == strings.TrimSpace(b.MilestoneID) &&
		a.AckRequired == b.AckRequired &&
		a.ReplyExpected == b.ReplyExpected &&
		a.Passive == b.Passive &&
		a.Interrupt == b.Interrupt
}

func normalizeBoardMessageKind(raw string) (agent.BoardMessageKind, error) {
	kind := agent.BoardMessageKind(strings.TrimSpace(raw))
	if kind == "" {
		return agent.BoardMessageKindInfo, nil
	}
	switch kind {
	case agent.BoardMessageKindInstruction, agent.BoardMessageKindInfo,
		agent.BoardMessageKindAlert, agent.BoardMessageKindReviewRequest,
		agent.BoardMessageKindTaskUpdate, agent.BoardMessageKindLeadChange,
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
		return kind, nil
	default:
		return "", errors.New("invalid kind: must be one of instruction, info, alert, review_request, task_update, lead_change, coordinator_pulse, plan_session, plan_proposal, plan_question, plan_decision, plan_review, plan_close, interview_session, interview_question, interview_answer, interview_verify, epic, epic_question, epic_answer, epic_finalize, epic_update, epic_checkpoint, milestone_proposal, milestone, story, story_proposal, story_state, story_update, story_validation, acceptance_criteria, milestone_review, milestone_summary, delivery_log, guidance_update")
	}
}

func convertRoomMembers(members []agent.RoomMember) []RoomMemberResponse {
	out := make([]RoomMemberResponse, 0, len(members))
	for _, member := range members {
		resp := RoomMemberResponse{
			ActorID:           member.ActorID,
			Role:              member.Role,
			Backend:           member.Backend,
			Session:           member.Session,
			PaneID:            member.PaneID,
			Unbound:           member.Unbound,
			TransportEndpoint: member.TransportEndpoint,
			TransportKind:     member.TransportKind,
			DeliveryBinding:   convertRoomDeliveryBinding(member.DeliveryBinding),
		}
		if !member.JoinedAt.IsZero() {
			resp.JoinedAt = member.JoinedAt.Format(time.RFC3339)
		}
		out = append(out, resp)
	}
	return out
}

func convertRoomDeliveryBinding(binding *agent.RoomDeliveryBinding) *RoomDeliveryBindingResponse {
	if binding == nil {
		return nil
	}
	return &RoomDeliveryBindingResponse{
		MuxBackend:        binding.MuxBackend,
		MuxSession:        binding.MuxSession,
		MuxPaneID:         binding.MuxPaneID,
		TransportEndpoint: binding.TransportEndpoint,
		TransportKind:     binding.TransportKind,
		SubmitMode:        binding.SubmitMode,
		Health:            binding.Health,
		FallbackPolicy:    binding.FallbackPolicy,
	}
}

func toRoomMembers(members []RoomMemberRequest) []agent.RoomMember {
	out := make([]agent.RoomMember, 0, len(members))
	for _, member := range members {
		actorID := strings.TrimSpace(member.ActorID)
		if actorID == "" {
			continue
		}
		out = append(out, agent.RoomMember{
			ActorID:           actorID,
			Role:              strings.TrimSpace(member.Role),
			Backend:           strings.ToLower(strings.TrimSpace(member.Backend)),
			Session:           strings.TrimSpace(member.Session),
			PaneID:            strings.TrimSpace(member.PaneID),
			Unbound:           member.Unbound,
			TransportEndpoint: strings.TrimSpace(member.TransportEndpoint),
			TransportKind:     strings.ToLower(strings.TrimSpace(member.TransportKind)),
			DeliveryBinding:   toAgentRoomDeliveryBinding(member.DeliveryBinding),
		})
	}
	return out
}

func toAgentRoomDeliveryBinding(binding *RoomDeliveryBindingRequest) *agent.RoomDeliveryBinding {
	if binding == nil {
		return nil
	}
	return &agent.RoomDeliveryBinding{
		MuxBackend:        strings.ToLower(strings.TrimSpace(binding.MuxBackend)),
		MuxSession:        strings.TrimSpace(binding.MuxSession),
		MuxPaneID:         strings.TrimSpace(binding.MuxPaneID),
		TransportEndpoint: strings.TrimSpace(binding.TransportEndpoint),
		TransportKind:     strings.ToLower(strings.TrimSpace(binding.TransportKind)),
		SubmitMode:        strings.TrimSpace(binding.SubmitMode),
		Health:            strings.TrimSpace(binding.Health),
		FallbackPolicy:    strings.TrimSpace(binding.FallbackPolicy),
	}
}

func convertRoomReminder(reminder coordination.RoomReminder) RoomReminderResponse {
	resp := RoomReminderResponse{
		ID:            strings.TrimSpace(reminder.ID),
		WorkspaceID:   strings.TrimSpace(reminder.WorkspaceID),
		RoomID:        strings.TrimSpace(reminder.RoomID),
		RootMessageID: strings.TrimSpace(reminder.RootMessageID),
		TaskID:        strings.TrimSpace(reminder.TaskID),
		StoryID:       strings.TrimSpace(reminder.StoryID),
		MilestoneID:   strings.TrimSpace(reminder.MilestoneID),
		Sender:        strings.TrimSpace(reminder.Sender),
		Recipient:     strings.TrimSpace(reminder.Recipient),
		Subject:       strings.TrimSpace(reminder.Subject),
		Body:          strings.TrimSpace(reminder.Body),
		AckRequired:   reminder.AckRequired,
		ReplyExpected: reminder.ReplyExpected,
		Interrupt:     reminder.Interrupt,
		Passive:       reminder.Passive,
		Interval:      reminder.Interval.String(),
		MaxIterations: reminder.MaxIterations,
		SentCount:     reminder.SentCount,
		Active:        reminder.Active,
	}
	if reminder.LastSentAt != nil && !reminder.LastSentAt.IsZero() {
		resp.LastSentAt = reminder.LastSentAt.UTC().Format(time.RFC3339)
	}
	if !reminder.CreatedAt.IsZero() {
		resp.CreatedAt = reminder.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !reminder.UpdatedAt.IsZero() {
		resp.UpdatedAt = reminder.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return resp
}

func apiResolveReminderRecipient(room agent.RoomSummary, recipient string) (string, error) {
	recipient = strings.TrimSpace(recipient)
	switch recipient {
	case "":
		return "", fmt.Errorf("recipient required")
	case agent.BroadcastRecipient:
		return "", fmt.Errorf("broadcast reminders are not supported")
	case "@coordinator":
		for _, member := range room.Members {
			if strings.EqualFold(strings.TrimSpace(member.Role), "coordinator") {
				return strings.TrimSpace(member.ActorID), nil
			}
		}
		return "", fmt.Errorf("room has no coordinator to target")
	default:
		for _, member := range room.Members {
			if strings.TrimSpace(member.ActorID) == recipient {
				return recipient, nil
			}
		}
		return "", fmt.Errorf("recipient %q is not a room member", recipient)
	}
}

func requireActiveRoomLoopAPI(ctx context.Context, coordStore *coordination.Store, workspaceID, roomID string, now time.Time) error {
	loop, err := coordStore.GetRoomLoop(ctx, workspaceID, roomID)
	if err != nil {
		return err
	}
	if loop == nil || !loop.Enabled {
		return fmt.Errorf("room loop is not active for %q", roomID)
	}
	if loop.LastTickAt == nil || loop.LastTickAt.IsZero() {
		return fmt.Errorf("room loop for %q has no recorded heartbeat", roomID)
	}
	if now.Sub(loop.LastTickAt.UTC()) > apiRoomLoopHeartbeatGrace {
		return fmt.Errorf("room loop heartbeat for %q is stale (last tick %s)", roomID, loop.LastTickAt.UTC().Format(time.RFC3339))
	}
	if strings.TrimSpace(loop.DeliveryLeaseName) == "" || strings.TrimSpace(loop.DeliveryOwnerID) == "" {
		return fmt.Errorf("room loop for %q has no active delivery owner", roomID)
	}
	lease, err := coordStore.GetLease(ctx, loop.DeliveryLeaseName)
	if err != nil {
		return err
	}
	if lease == nil || strings.TrimSpace(lease.OwnerID) != strings.TrimSpace(loop.DeliveryOwnerID) || now.After(lease.ExpiresAt.UTC()) {
		return fmt.Errorf("room loop delivery owner for %q is not active", roomID)
	}
	return nil
}

func deriveAPIRoomSubject(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "room message"
	}
	first := body
	if idx := strings.IndexByte(first, '\n'); idx >= 0 {
		first = first[:idx]
	}
	first = strings.Join(strings.Fields(first), " ")
	if len(first) > 80 {
		first = first[:77] + "..."
	}
	return first
}

func roomWorkspaceID(r *http.Request) string {
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(r.URL.Query().Get("workspace"))
	}
	return ws.CanonicalWorkspaceKey(workspaceID)
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
