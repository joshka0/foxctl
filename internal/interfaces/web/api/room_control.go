package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/coordination"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
)

type roomStatusParticipantResponse struct {
	ActorID              string                   `json:"actor_id"`
	Role                 string                   `json:"role,omitempty"`
	LastActiveAt         *time.Time               `json:"last_active_at,omitempty"`
	Status               string                   `json:"status"`
	AssignedTaskCount    int                      `json:"assigned_task_count"`
	OwnedTaskCount       int                      `json:"owned_task_count"`
	ActionableInboxCount int                      `json:"actionable_inbox_count"`
	LatestActionable     *roomStatusEntryResponse `json:"latest_actionable,omitempty"`
}

type roomTaskPulseSummaryResponse struct {
	Pending           int `json:"pending"`
	AssignedUnclaimed int `json:"assigned_unclaimed"`
	InProgress        int `json:"in_progress"`
	Blocked           int `json:"blocked"`
	Stale             int `json:"stale"`
	Completed         int `json:"completed"`
}

type roomStatusBacklogResponse struct {
	ParticipantsWithPending int                       `json:"participants_with_pending"`
	PendingAcks             int                       `json:"pending_acks"`
	PendingReplies          int                       `json:"pending_replies"`
	LatestByParticipant     []roomStatusEntryResponse `json:"latest_by_participant,omitempty"`
}

type roomStatusEntryResponse struct {
	ID        string                   `json:"id"`
	Sender    string                   `json:"sender"`
	Recipient string                   `json:"recipient"`
	Subject   string                   `json:"subject"`
	Priority  int                      `json:"priority"`
	Status    agent.BoardMessageStatus `json:"status"`
	CreatedAt time.Time                `json:"created_at"`
	Category  string                   `json:"category"`
	Flags     []string                 `json:"flags,omitempty"`
	Preview   string                   `json:"preview,omitempty"`
}

type roomStatusActionRequiredResponse struct {
	ParticipantsWithPending int                       `json:"participants_with_pending"`
	PendingAcks             int                       `json:"pending_acks"`
	PendingReplies          int                       `json:"pending_replies"`
	AssignedUnclaimed       int                       `json:"assigned_unclaimed"`
	BlockedTasks            int                       `json:"blocked_tasks"`
	StaleTasks              int                       `json:"stale_tasks"`
	Filter                  []string                  `json:"filter,omitempty"`
	TopEntries              []roomStatusEntryResponse `json:"top_entries,omitempty"`
	TopTasks                []roomStatusTaskResponse  `json:"top_tasks,omitempty"`
}

type roomStatusTaskResponse struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	AssignedActorID string     `json:"assigned_actor_id,omitempty"`
	OwnerActorID    string     `json:"owner_actor_id,omitempty"`
	BlockedReason   string     `json:"blocked_reason,omitempty"`
	HeartbeatAt     *time.Time `json:"heartbeat_at,omitempty"`
	Signals         []string   `json:"signals,omitempty"`
}

type roomInboxEntryResponse struct {
	ID        string                   `json:"id"`
	Sender    string                   `json:"sender"`
	Recipient string                   `json:"recipient"`
	Subject   string                   `json:"subject"`
	Priority  int                      `json:"priority"`
	Status    agent.BoardMessageStatus `json:"status"`
	CreatedAt time.Time                `json:"created_at"`
	Category  string                   `json:"category"`
	Flags     []string                 `json:"flags,omitempty"`
	Preview   string                   `json:"preview,omitempty"`
}

type roomLoopResponse struct {
	Enabled                      bool                           `json:"enabled"`
	ManagedBy                    string                         `json:"managed_by"`
	LastTickAt                   *time.Time                     `json:"last_tick_at,omitempty"`
	DeliveryLeaseName            string                         `json:"delivery_lease_name,omitempty"`
	DeliveryOwnerID              string                         `json:"delivery_owner_id,omitempty"`
	DeliveryCursorMessageID      string                         `json:"delivery_cursor_message_id,omitempty"`
	DeliveryCursorAt             *time.Time                     `json:"delivery_cursor_at,omitempty"`
	PulseInterval                string                         `json:"pulse_interval"`
	TaskFollowupInterval         string                         `json:"task_followup_interval"`
	ReplyStaleAfter              string                         `json:"reply_stale_after"`
	TaskStaleAfter               string                         `json:"task_stale_after"`
	MinPulseFloor                string                         `json:"min_pulse_floor"`
	InterruptAttemptLimit        int                            `json:"interrupt_attempt_limit"`
	ReminderBackoffCap           int                            `json:"reminder_backoff_cap"`
	CoordinatorPulseEnabled      bool                           `json:"coordinator_pulse_enabled"`
	CoordinatorEscalationEnabled bool                           `json:"coordinator_escalation_enabled"`
	LastDeliveryTrace            *roomLoopDeliveryTraceResponse `json:"last_delivery_trace,omitempty"`
}

type roomLoopDeliveryTraceResponse struct {
	WorkspaceID             string     `json:"workspace_id,omitempty"`
	RoomID                  string     `json:"room_id,omitempty"`
	MessageID               string     `json:"message_id,omitempty"`
	TaskID                  string     `json:"task_id,omitempty"`
	Recipient               string     `json:"recipient,omitempty"`
	DeliveryLeaseName       string     `json:"delivery_lease_name,omitempty"`
	DeliveryOwnerID         string     `json:"delivery_owner_id,omitempty"`
	RelayBackend            string     `json:"relay_backend,omitempty"`
	ChosenActorID           string     `json:"chosen_actor_id,omitempty"`
	ChosenMuxBackend        string     `json:"chosen_mux_backend,omitempty"`
	ChosenMuxSession        string     `json:"chosen_mux_session,omitempty"`
	ChosenMuxPaneID         string     `json:"chosen_mux_pane_id,omitempty"`
	ChosenTransportEndpoint string     `json:"chosen_transport_endpoint,omitempty"`
	ChosenTransportKind     string     `json:"chosen_transport_kind,omitempty"`
	ChosenSubmitMode        string     `json:"chosen_submit_mode,omitempty"`
	FallbackAttempted       bool       `json:"fallback_attempted,omitempty"`
	DeliveredCount          int        `json:"delivered_count,omitempty"`
	FailedCount             int        `json:"failed_count,omitempty"`
	DeliveredTo             []string   `json:"delivered_to,omitempty"`
	FailedMembers           []string   `json:"failed_members,omitempty"`
	Outcome                 string     `json:"outcome,omitempty"`
	CursorBeforeMessageID   string     `json:"cursor_before_message_id,omitempty"`
	CursorAfterMessageID    string     `json:"cursor_after_message_id,omitempty"`
	CursorAdvanced          bool       `json:"cursor_advanced,omitempty"`
	ObservedAt              *time.Time `json:"observed_at,omitempty"`
}

type roomLoopPatchRequest struct {
	WorkspaceID                  string  `json:"workspace_id"`
	ActorID                      string  `json:"actor_id"`
	Enabled                      *bool   `json:"enabled,omitempty"`
	PulseInterval                *string `json:"pulse_interval,omitempty"`
	TaskFollowupInterval         *string `json:"task_followup_interval,omitempty"`
	ReplyStaleAfter              *string `json:"reply_stale_after,omitempty"`
	TaskStaleAfter               *string `json:"task_stale_after,omitempty"`
	MinPulseFloor                *string `json:"min_pulse_floor,omitempty"`
	InterruptAttemptLimit        *int    `json:"interrupt_attempt_limit,omitempty"`
	ReminderBackoffCap           *int    `json:"reminder_backoff_cap,omitempty"`
	CoordinatorPulseEnabled      *bool   `json:"coordinator_pulse_enabled,omitempty"`
	CoordinatorEscalationEnabled *bool   `json:"coordinator_escalation_enabled,omitempty"`
}

type roomCoordinatorSetRequest struct {
	WorkspaceID string `json:"workspace_id"`
	ActorID     string `json:"actor_id"`
	TargetID    string `json:"target_id"`
	Note        string `json:"note,omitempty"`
}

type roomMessageActionRequest struct {
	WorkspaceID string   `json:"workspace_id"`
	ActorID     string   `json:"actor_id"`
	Mode        string   `json:"mode,omitempty"`
	MessageIDs  []string `json:"message_ids,omitempty"`
	Only        []string `json:"only,omitempty"`
	All         bool     `json:"all,omitempty"`
}

type roomTaskActionRequest struct {
	WorkspaceID string `json:"workspace_id"`
	ActorID     string `json:"actor_id"`
	Recipient   string `json:"recipient,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Gotchas     string `json:"gotchas,omitempty"`
}

type roomTaskCreateRequest struct {
	WorkspaceID string   `json:"workspace_id"`
	ActorID     string   `json:"actor_id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	ScopePath   string   `json:"scope_path,omitempty"`
	ParentID    string   `json:"parent_id,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	MilestoneID string   `json:"milestone_id,omitempty"`
}

const (
	apiRoomTaskScanLimit         = 1000
	apiRoomDefaultLimit          = 200
	apiRoomDefaultStaleAfter     = 5 * time.Minute
	apiRoomPulseInterval         = 30 * time.Minute
	apiRoomReplyStaleAfter       = 2 * time.Hour
	apiRoomTaskStaleAfter        = 4 * time.Hour
	apiRoomMinimumPulseFloor     = 24 * time.Hour
	apiRoomInterruptAttemptLimit = 2
	apiRoomReminderBackoffCap    = 8
	apiRoomLoopManager           = "server-default"
)

func handleRoomStatusGet(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	workspaceID := roomWorkspaceID(r)
	if workspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id required")
		return
	}
	limit, verbose, filters, err := parseRoomStatusParams(r)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()
	summary, messages, err := apiLoadRoomState(r.Context(), store, workspaceID, roomID, "", limit)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room status state")
		httpError(w, http.StatusInternalServerError, "failed to load room status")
		return
	}
	taskStore, err := taskstore.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open task store")
		httpError(w, http.StatusInternalServerError, "failed to open task store")
		return
	}
	defer taskStore.Close()
	tasks, err := apiListRoomTasks(r.Context(), taskStore, ws.CanonicalID(workspaceID), messages, "")
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to list room tasks")
		httpError(w, http.StatusInternalServerError, "failed to load room tasks")
		return
	}
	loopStore, err := coordination.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open coordination store")
		httpError(w, http.StatusInternalServerError, "failed to open coordination store")
		return
	}
	defer loopStore.Close()
	loop, err := apiLoadRoomLoop(r.Context(), loopStore, workspaceID, roomID)
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room loop")
		httpError(w, http.StatusInternalServerError, "failed to load room loop")
		return
	}
	staleAfter := loop.TaskStaleAfter
	now := time.Now().UTC()
	taskPulse := apiBuildRoomTaskPulseSummary(tasks, now, staleAfter)
	backlog := apiBuildRoomStatusBacklog(summary, messages)
	writeJSON(w, http.StatusOK, map[string]any{
		"room":            convertRoomSummary(summary),
		"participants":    apiBuildRoomStatusParticipants(summary, messages, tasks, staleAfter),
		"task_pulse":      taskPulse,
		"backlog":         backlog,
		"action_required": apiBuildRoomStatusActionRequired(summary, messages, tasks, backlog, taskPulse, filters, staleAfter, now, verbose),
		"loop":            apiConvertRoomLoop(loop),
	})
}

func handleRoomInboxGet(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	workspaceID := roomWorkspaceID(r)
	if workspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id required")
		return
	}
	actorID := strings.TrimSpace(r.URL.Query().Get("actor_id"))
	if actorID == "" {
		httpError(w, http.StatusBadRequest, "actor_id required")
		return
	}
	filter := strings.TrimSpace(r.URL.Query().Get("only"))
	if filter == "" {
		filter = strings.TrimSpace(r.URL.Query().Get("filter"))
	}
	includeBroadcasts := parseBool(r.URL.Query().Get("include_broadcasts"))
	compact := true
	q := r.URL.Query()
	if parseBool(q.Get("full_room")) {
		compact = false
	} else if raw := strings.TrimSpace(q.Get("compact")); raw != "" {
		compact = parseBool(raw)
	}
	limit := apiRoomDefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > apiRoomTaskScanLimit {
			httpError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", apiRoomTaskScanLimit))
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
	summary, messages, err := apiLoadRoomState(r.Context(), store, workspaceID, roomID, actorID, limit)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room inbox state")
		httpError(w, http.StatusInternalServerError, "failed to load room inbox")
		return
	}
	entries := apiBuildRoomInboxEntries(actorID, messages, filter, includeBroadcasts)
	roomPayload := any(convertRoomSummary(summary))
	if compact {
		roomPayload = agent.CompactRoomSummaryForInbox(summary)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room":    roomPayload,
		"actor":   actorID,
		"filter":  apiNormalizeRoomInboxFilter(filter),
		"count":   len(entries),
		"entries": entries,
	})
}

func handleRoomTasksGet(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	workspaceID := roomWorkspaceID(r)
	if workspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id required")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := apiRoomTaskScanLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > apiRoomTaskScanLimit {
			httpError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", apiRoomTaskScanLimit))
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
	summary, messages, err := apiLoadRoomState(r.Context(), store, workspaceID, roomID, "", limit)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room task state")
		httpError(w, http.StatusInternalServerError, "failed to load room tasks")
		return
	}
	taskStore, err := taskstore.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open task store")
		httpError(w, http.StatusInternalServerError, "failed to open task store")
		return
	}
	defer taskStore.Close()
	tasks, err := apiListRoomTasks(r.Context(), taskStore, ws.CanonicalID(workspaceID), messages, status)
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to list room tasks")
		httpError(w, http.StatusInternalServerError, "failed to load room tasks")
		return
	}
	resp := make([]roomTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		resp = append(resp, convertRoomTask(task))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room":  convertRoomSummary(summary),
		"tasks": resp,
		"count": len(resp),
	})
}

func handleRoomTasksPost(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	var req roomTaskCreateRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.normalize()
	if err := req.validate(); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	boardStore, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer boardStore.Close()

	summary, messages, err := apiLoadRoomState(r.Context(), boardStore, req.WorkspaceID, roomID, req.ActorID, apiRoomTaskScanLimit)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room before task create")
		httpError(w, http.StatusInternalServerError, "failed to load room")
		return
	}
	if !apiRoomHasParticipant(summary, req.ActorID) && !apiRoomIsLocalSuperuser(req.ActorID) {
		httpError(w, http.StatusForbidden, "only room participants can create room tasks")
		return
	}
	recipient, err := apiRoomTaskEventRecipient(summary.Members)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	epicID, milestoneID, err := apiResolveRoomTaskMilestoneSelection(messages, req.MilestoneID)
	if err != nil {
		statusCode := http.StatusBadRequest
		if errors.Is(err, errAPIRoomTaskMilestoneNotFound) {
			statusCode = http.StatusNotFound
		}
		httpError(w, statusCode, err.Error())
		return
	}

	taskStore, err := taskstore.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open task store")
		httpError(w, http.StatusInternalServerError, "failed to open task store")
		return
	}
	defer taskStore.Close()

	task, err := taskStore.Add(r.Context(), taskstore.Task{
		WorkspaceID: ws.CanonicalID(req.WorkspaceID),
		Title:       req.Title,
		Description: req.Description,
		ScopePath:   req.ScopePath,
		ParentID:    req.ParentID,
		DependsOn:   append([]string(nil), req.DependsOn...),
		Status:      taskstore.StatusPending,
		EpicID:      epicID,
		MilestoneID: milestoneID,
	})
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to create room task")
		httpError(w, http.StatusInternalServerError, "failed to create room task")
		return
	}

	msg := &agent.BoardMessage{
		WorkspaceID: req.WorkspaceID,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:      req.ActorID,
		Recipient:   recipient,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Priority:    agent.DefaultPriority,
		Subject:     fmt.Sprintf("Task added: %s", task.Title),
		Body:        apiFormatRoomTaskAddedBody(task),
	}
	if err := boardStore.SendMessage(r.Context(), msg); err != nil {
		log.Error().Err(err).Str("room_id", roomID).Str("task_id", task.ID).Msg("failed to write room task message")
		httpError(w, http.StatusInternalServerError, "failed to create room task")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"room_id": roomID,
		"task":    convertRoomTask(task),
		"message": msg,
		"actor":   req.ActorID,
	})
}

func handleRoomLoopGet(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	workspaceID := roomWorkspaceID(r)
	if workspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id required")
		return
	}
	actorID := strings.TrimSpace(r.URL.Query().Get("actor_id"))
	if actorID == "" {
		httpError(w, http.StatusBadRequest, "actor_id required")
		return
	}
	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()
	summary, err := store.GetRoom(r.Context(), workspaceID, roomID, actorID)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room for loop get")
		httpError(w, http.StatusInternalServerError, "failed to load room")
		return
	}
	if !apiRoomHasParticipant(summary, actorID) && !apiRoomIsLocalSuperuser(actorID) {
		httpError(w, http.StatusForbidden, "only current room members can inspect loop state")
		return
	}
	loopStore, err := coordination.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open coordination store")
		httpError(w, http.StatusInternalServerError, "failed to open coordination store")
		return
	}
	defer loopStore.Close()
	loop, err := apiLoadRoomLoop(r.Context(), loopStore, workspaceID, roomID)
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room loop")
		httpError(w, http.StatusInternalServerError, "failed to load room loop")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id": roomID,
		"loop":    apiConvertRoomLoop(loop),
	})
}

func handleRoomLoopPatch(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	var req roomLoopPatchRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	if req.WorkspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.ActorID == "" {
		httpError(w, http.StatusBadRequest, "actor_id is required")
		return
	}

	boardStore, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer boardStore.Close()
	summary, err := boardStore.GetRoom(r.Context(), req.WorkspaceID, roomID, req.ActorID)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room for loop patch")
		httpError(w, http.StatusInternalServerError, "failed to update room loop")
		return
	}
	if !apiRoomActorHasCoordinatorAccess(summary.Members, req.ActorID) {
		httpError(w, http.StatusForbidden, "only room coordinators can update loop policy")
		return
	}

	loopStore, err := coordination.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open coordination store")
		httpError(w, http.StatusInternalServerError, "failed to open coordination store")
		return
	}
	defer loopStore.Close()
	current, err := apiLoadRoomLoop(r.Context(), loopStore, req.WorkspaceID, roomID)
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room loop for patch")
		httpError(w, http.StatusInternalServerError, "failed to update room loop")
		return
	}
	updated, err := apiApplyRoomLoopPatch(current, req)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated.ManagedBy = strings.TrimSpace(current.ManagedBy)
	if updated.ManagedBy == "" {
		updated.ManagedBy = apiRoomLoopManager
	}
	persisted, err := loopStore.UpsertRoomLoop(r.Context(), updated)
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to persist room loop")
		httpError(w, http.StatusInternalServerError, "failed to update room loop")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id": roomID,
		"loop":    apiConvertRoomLoop(persisted),
	})
}

func handleRoomCoordinatorSet(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	var req roomCoordinatorSetRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.TargetID = strings.TrimSpace(req.TargetID)
	if req.WorkspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.ActorID == "" {
		httpError(w, http.StatusBadRequest, "actor_id is required")
		return
	}
	if req.TargetID == "" {
		httpError(w, http.StatusBadRequest, "target_id is required")
		return
	}
	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()
	summary, err := store.GetRoom(r.Context(), req.WorkspaceID, roomID, req.ActorID)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room for coordinator set")
		httpError(w, http.StatusInternalServerError, "failed to update coordinator")
		return
	}
	currentCoordinator := apiRoomCoordinatorActorID(summary.Members)
	if currentCoordinator != "" && !apiSameRoomParticipant(currentCoordinator, req.ActorID) && !apiRoomIsLocalSuperuser(req.ActorID) {
		httpError(w, http.StatusForbidden, "only the current coordinator can reassign coordinator role")
		return
	}
	if !apiRoomHasParticipant(summary, req.TargetID) {
		httpError(w, http.StatusNotFound, "target participant is not a room member")
		return
	}
	updatedMembers := make([]agent.RoomMember, 0, len(summary.Members))
	for _, member := range summary.Members {
		if apiSameRoomParticipant(member.ActorID, req.TargetID) {
			member.Role = "coordinator"
		} else if strings.EqualFold(strings.TrimSpace(member.Role), "coordinator") {
			member.Role = ""
		}
		updatedMembers = append(updatedMembers, member)
	}
	if _, err := store.ReplaceRoomMembers(r.Context(), req.WorkspaceID, roomID, updatedMembers); err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to replace room members for coordinator set")
		httpError(w, http.StatusInternalServerError, "failed to update coordinator")
		return
	}
	leadMsg := &agent.BoardMessage{
		WorkspaceID: req.WorkspaceID,
		Stream:      agent.RoomStreamName(roomID),
		Sender:      req.ActorID,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindLeadChange,
		Priority:    agent.DefaultPriority,
		Subject:     "Coordinator handoff",
		Body:        fmt.Sprintf("lead_change\nPrevious coordinator: %s\nNew coordinator: %s\nActor: %s\nNote: %s", apiFallbackRoomValue(currentCoordinator), req.TargetID, req.ActorID, strings.TrimSpace(req.Note)),
	}
	if err := store.SendMessage(r.Context(), leadMsg); err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to write coordinator handoff message")
		httpError(w, http.StatusInternalServerError, "failed to update coordinator")
		return
	}
	updated, err := store.GetRoom(r.Context(), req.WorkspaceID, roomID, "")
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to reload room after coordinator set")
		httpError(w, http.StatusInternalServerError, "failed to update coordinator")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room":                 convertRoomSummary(updated),
		"previous_coordinator": currentCoordinator,
		"coordinator":          req.TargetID,
		"actor":                req.ActorID,
		"event_kind":           "lead_change",
	})
}

func handleRoomMessageAction(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID, messageID, action string) {
	var req roomMessageActionRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	if req.WorkspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.ActorID == "" {
		httpError(w, http.StatusBadRequest, "actor_id is required")
		return
	}
	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()
	summary, err := store.GetRoom(r.Context(), req.WorkspaceID, roomID, req.ActorID)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room for message action")
		httpError(w, http.StatusInternalServerError, "failed to update room message")
		return
	}
	if strings.EqualFold(action, "resolve") && !apiRoomActorHasCoordinatorAccess(summary.Members, req.ActorID) {
		httpError(w, http.StatusForbidden, "room resolve requires coordinator role")
		return
	}
	messageIDs := []string{strings.TrimSpace(messageID)}
	var (
		updated int
		status  agent.BoardMessageStatus
	)
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "ack":
		updated, err = store.AckMessages(r.Context(), req.WorkspaceID, req.ActorID, messageIDs)
		status = agent.BoardMessageStatusAcked
	case "resolve":
		mode := strings.TrimSpace(strings.ToLower(req.Mode))
		switch mode {
		case "", "acked", "ack":
			expanded, expandErr := apiExpandRoomResolveMessageIDs(r.Context(), store, req.WorkspaceID, roomID, messageIDs)
			if expandErr != nil {
				httpError(w, http.StatusInternalServerError, "failed to resolve reminder chain")
				return
			}
			messageIDs = expanded
			updated, err = store.AckMessages(r.Context(), req.WorkspaceID, req.ActorID, messageIDs)
			status = agent.BoardMessageStatusAcked
		case "read":
			expanded, expandErr := apiExpandRoomResolveMessageIDs(r.Context(), store, req.WorkspaceID, roomID, messageIDs)
			if expandErr != nil {
				httpError(w, http.StatusInternalServerError, "failed to resolve reminder chain")
				return
			}
			messageIDs = expanded
			updated, err = store.MarkRead(r.Context(), req.WorkspaceID, req.ActorID, messageIDs)
			status = agent.BoardMessageStatusRead
		default:
			httpError(w, http.StatusBadRequest, "mode must be acked or read")
			return
		}
	default:
		httpError(w, http.StatusMethodNotAllowed, "unsupported room message action")
		return
	}
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Str("message_id", messageID).Msg("failed to update room message")
		httpError(w, http.StatusInternalServerError, "failed to update room message")
		return
	}
	if updated == 0 {
		httpError(w, http.StatusNotFound, "no room messages were updated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id":         roomID,
		"message_ids":     messageIDs,
		"updated":         updated,
		"resolved_status": status,
		"actor":           req.ActorID,
	})
}

func handleRoomMessagesResolveBulk(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	var req roomMessageActionRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	if req.WorkspaceID == "" {
		httpError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.ActorID == "" {
		httpError(w, http.StatusBadRequest, "actor_id is required")
		return
	}
	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()
	summary, err := store.GetRoom(r.Context(), req.WorkspaceID, roomID, req.ActorID)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room for bulk resolve")
		httpError(w, http.StatusInternalServerError, "failed to resolve room messages")
		return
	}
	if !apiRoomActorHasCoordinatorAccess(summary.Members, req.ActorID) {
		httpError(w, http.StatusForbidden, "room resolve requires coordinator role")
		return
	}
	ids, err := apiResolveRoomMessageIDsForResolve(r.Context(), store, req.WorkspaceID, summary, req.All, req.Only, req.MessageIDs)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids, err = apiExpandRoomResolveMessageIDs(r.Context(), store, req.WorkspaceID, roomID, ids)
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to expand resolve ids")
		httpError(w, http.StatusInternalServerError, "failed to resolve room messages")
		return
	}
	mode := strings.TrimSpace(strings.ToLower(req.Mode))
	var (
		updated int
		status  agent.BoardMessageStatus
	)
	switch mode {
	case "", "acked", "ack":
		updated, err = store.AckMessages(r.Context(), req.WorkspaceID, req.ActorID, ids)
		status = agent.BoardMessageStatusAcked
	case "read":
		updated, err = store.MarkRead(r.Context(), req.WorkspaceID, req.ActorID, ids)
		status = agent.BoardMessageStatusRead
	default:
		httpError(w, http.StatusBadRequest, "mode must be acked or read")
		return
	}
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to bulk resolve room messages")
		httpError(w, http.StatusInternalServerError, "failed to resolve room messages")
		return
	}
	if updated == 0 {
		httpError(w, http.StatusNotFound, "no room messages were resolved")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id":         roomID,
		"message_ids":     ids,
		"updated":         updated,
		"resolved_status": status,
		"actor":           req.ActorID,
	})
}

func handleRoomTaskAction(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID, taskID, action string) {
	var req roomTaskActionRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.normalize()
	if err := req.validate(); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	boardStore, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer boardStore.Close()
	summary, err := boardStore.GetRoom(r.Context(), req.WorkspaceID, roomID, req.ActorID)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Msg("failed to load room for task action")
		httpError(w, http.StatusInternalServerError, "failed to update room task")
		return
	}

	taskStore, err := taskstore.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open task store")
		httpError(w, http.StatusInternalServerError, "failed to open task store")
		return
	}
	defer taskStore.Close()

	task, err := taskStore.Get(r.Context(), strings.TrimSpace(taskID))
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if task.WorkspaceID != ws.CanonicalID(req.WorkspaceID) {
		httpError(w, http.StatusBadRequest, "task does not belong to this workspace")
		return
	}

	now := time.Now().UTC()
	action = strings.ToLower(strings.TrimSpace(action))
	if err := applyRoomTaskAction(&task, summary, req, action, now); err != nil {
		statusCode := http.StatusBadRequest
		switch {
		case errors.Is(err, errRoomTaskActionForbidden):
			statusCode = http.StatusForbidden
		case errors.Is(err, errRoomTaskActionUnsupported):
			statusCode = http.StatusMethodNotAllowed
		}
		httpError(w, statusCode, err.Error())
		return
	}

	task, err = taskStore.Update(r.Context(), task)
	if err != nil {
		log.Error().Err(err).Str("room_id", roomID).Str("task_id", taskID).Msg("failed to update room task")
		httpError(w, http.StatusInternalServerError, "failed to update room task")
		return
	}

	subject, body := apiFormatRoomTaskActionMessage(strings.ToLower(strings.TrimSpace(action)), task, req.ActorID, req.Recipient, req.Reason)
	msg := &agent.BoardMessage{
		WorkspaceID: req.WorkspaceID,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:      req.ActorID,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Priority:    agent.DefaultPriority,
		Subject:     subject,
		Body:        body,
	}
	if err := boardStore.SendMessage(r.Context(), msg); err != nil {
		log.Error().Err(err).Str("room_id", roomID).Str("task_id", taskID).Msg("failed to write room task message")
		httpError(w, http.StatusInternalServerError, "failed to update room task")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"room_id": roomID,
		"task":    convertRoomTask(task),
		"action":  strings.ToLower(strings.TrimSpace(action)),
		"actor":   req.ActorID,
	})
}

var (
	errRoomTaskActionForbidden   = errors.New("room task action forbidden")
	errRoomTaskActionUnsupported = errors.New("room task action unsupported")
)

func (req *roomTaskActionRequest) normalize() {
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Recipient = strings.TrimSpace(req.Recipient)
	req.Reason = strings.TrimSpace(req.Reason)
	req.Notes = strings.TrimSpace(req.Notes)
	req.Gotchas = strings.TrimSpace(req.Gotchas)
}

func (req roomTaskActionRequest) validate() error {
	if req.WorkspaceID == "" {
		return fmt.Errorf("workspace_id is required")
	}
	if req.ActorID == "" {
		return fmt.Errorf("actor_id is required")
	}
	return nil
}

func applyRoomTaskAction(task *taskstore.Task, summary agent.RoomSummary, req roomTaskActionRequest, action string, now time.Time) error {
	switch action {
	case "claim":
		return applyRoomTaskClaim(task, req, now)
	case "touch":
		return applyRoomTaskTouch(task, req, now)
	case "block":
		return applyRoomTaskBlock(task, req, now)
	case "unblock":
		return applyRoomTaskUnblock(task, req, now)
	case "complete":
		return applyRoomTaskComplete(task, req, now)
	case "abandon":
		return applyRoomTaskAbandon(task, req)
	case "assign", "reassign", "reclaim":
		return applyCoordinatorRoomTaskAction(task, summary, req, action, now)
	default:
		return fmt.Errorf("%w: unsupported room task action", errRoomTaskActionUnsupported)
	}
}

func applyRoomTaskClaim(task *taskstore.Task, req roomTaskActionRequest, now time.Time) error {
	if assigned := strings.TrimSpace(task.AssignedActorID); assigned != "" && !apiSameRoomParticipant(assigned, req.ActorID) {
		return fmt.Errorf("%w: task is assigned to another participant", errRoomTaskActionForbidden)
	}
	if strings.TrimSpace(task.OwnerActorID) != "" && !apiSameRoomParticipant(task.OwnerActorID, req.ActorID) && task.Status != taskstore.StatusPending && task.Status != taskstore.StatusCanceled {
		return fmt.Errorf("%w: task is already claimed by another participant", errRoomTaskActionForbidden)
	}
	task.Status = taskstore.StatusInProgress
	if strings.TrimSpace(task.AssignedActorID) == "" {
		task.AssignedActorID = req.ActorID
		task.AssignedAt = &now
	}
	task.OwnerActorID = req.ActorID
	task.ClaimedAt = &now
	task.HeartbeatAt = &now
	task.BlockedReason = ""
	task.BlockedAt = nil
	return nil
}

func applyRoomTaskTouch(task *taskstore.Task, req roomTaskActionRequest, now time.Time) error {
	if !apiSameRoomParticipant(task.OwnerActorID, req.ActorID) {
		return fmt.Errorf("%w: only the current owner can refresh this task heartbeat", errRoomTaskActionForbidden)
	}
	if task.Status != taskstore.StatusInProgress && task.Status != taskstore.StatusBlocked {
		return fmt.Errorf("only in-progress or blocked tasks can be refreshed")
	}
	task.HeartbeatAt = &now
	return nil
}

func applyRoomTaskBlock(task *taskstore.Task, req roomTaskActionRequest, now time.Time) error {
	if req.Reason == "" {
		return fmt.Errorf("reason is required")
	}
	if !apiSameRoomParticipant(task.OwnerActorID, req.ActorID) {
		return fmt.Errorf("%w: only the current owner can block this task", errRoomTaskActionForbidden)
	}
	task.Status = taskstore.StatusBlocked
	task.BlockedReason = req.Reason
	task.BlockedAt = &now
	task.HeartbeatAt = &now
	return nil
}

func applyRoomTaskUnblock(task *taskstore.Task, req roomTaskActionRequest, now time.Time) error {
	if !apiSameRoomParticipant(task.OwnerActorID, req.ActorID) {
		return fmt.Errorf("%w: only the current owner can unblock this task", errRoomTaskActionForbidden)
	}
	task.Status = taskstore.StatusInProgress
	task.BlockedReason = ""
	task.BlockedAt = nil
	task.HeartbeatAt = &now
	return nil
}

func applyRoomTaskComplete(task *taskstore.Task, req roomTaskActionRequest, now time.Time) error {
	if !apiSameRoomParticipant(task.OwnerActorID, req.ActorID) {
		return fmt.Errorf("%w: only the current owner can complete this task", errRoomTaskActionForbidden)
	}
	if task.Status == taskstore.StatusBlocked {
		return fmt.Errorf("blocked tasks must be unblocked before completion")
	}
	task.Status = taskstore.StatusCompleted
	task.CompletedAt = &now
	task.OwnerActorID = ""
	task.HeartbeatAt = &now
	task.BlockedReason = ""
	task.BlockedAt = nil
	task.Notes = req.Notes
	task.Gotchas = req.Gotchas
	return nil
}

func applyRoomTaskAbandon(task *taskstore.Task, req roomTaskActionRequest) error {
	if strings.TrimSpace(task.OwnerActorID) != "" && !apiSameRoomParticipant(task.OwnerActorID, req.ActorID) {
		return fmt.Errorf("%w: only the current owner can abandon this task", errRoomTaskActionForbidden)
	}
	resetRoomTaskOwnership(task)
	task.Status = taskstore.StatusPending
	if req.Reason != "" {
		task.Notes = req.Reason
	}
	return nil
}

func applyCoordinatorRoomTaskAction(task *taskstore.Task, summary agent.RoomSummary, req roomTaskActionRequest, action string, now time.Time) error {
	if !apiRoomActorHasCoordinatorAccess(summary.Members, req.ActorID) {
		return fmt.Errorf("%w: only room coordinators can perform this action", errRoomTaskActionForbidden)
	}
	switch action {
	case "assign":
		return assignRoomTask(task, summary, req, now)
	case "reassign":
		return reassignRoomTask(task, summary, req, now)
	case "reclaim":
		return reclaimRoomTask(task, req)
	default:
		return fmt.Errorf("%w: unsupported room task action", errRoomTaskActionUnsupported)
	}
}

func assignRoomTask(task *taskstore.Task, summary agent.RoomSummary, req roomTaskActionRequest, now time.Time) error {
	if err := validateRoomTaskRecipient(summary, req.Recipient); err != nil {
		return err
	}
	task.AssignedActorID = req.Recipient
	task.AssignedAt = &now
	if req.Notes != "" {
		task.Notes = req.Notes
	}
	return nil
}

func reassignRoomTask(task *taskstore.Task, summary agent.RoomSummary, req roomTaskActionRequest, now time.Time) error {
	if err := validateRoomTaskRecipient(summary, req.Recipient); err != nil {
		return err
	}
	task.Status = taskstore.StatusPending
	task.AssignedActorID = req.Recipient
	task.AssignedAt = &now
	task.OwnerActorID = ""
	task.ClaimedAt = nil
	task.HeartbeatAt = nil
	task.BlockedReason = ""
	task.BlockedAt = nil
	if req.Reason != "" {
		task.Notes = req.Reason
	}
	return nil
}

func reclaimRoomTask(task *taskstore.Task, req roomTaskActionRequest) error {
	if req.Reason == "" {
		return fmt.Errorf("reason is required")
	}
	task.Status = taskstore.StatusPending
	resetRoomTaskOwnership(task)
	task.Notes = req.Reason
	return nil
}

func validateRoomTaskRecipient(summary agent.RoomSummary, recipient string) error {
	if recipient == "" {
		return fmt.Errorf("recipient is required")
	}
	if !apiRoomHasParticipant(summary, recipient) {
		return fmt.Errorf("assignee is not a room participant")
	}
	return nil
}

func resetRoomTaskOwnership(task *taskstore.Task) {
	task.AssignedActorID = ""
	task.AssignedAt = nil
	task.OwnerActorID = ""
	task.ClaimedAt = nil
	task.HeartbeatAt = nil
	task.BlockedReason = ""
	task.BlockedAt = nil
}

func parseRoomStatusParams(r *http.Request) (limit int, verbose bool, filters map[string]struct{}, err error) {
	limit = apiRoomDefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n <= 0 || n > apiRoomTaskScanLimit {
			return 0, false, nil, fmt.Errorf("limit must be between 1 and %d", apiRoomTaskScanLimit)
		}
		limit = n
	}
	verbose = parseBool(r.URL.Query().Get("verbose"))
	onlyValues := r.URL.Query()["only"]
	if len(onlyValues) == 0 {
		if raw := strings.TrimSpace(r.URL.Query().Get("filter")); raw != "" {
			onlyValues = []string{raw}
		}
	}
	filters, err = apiNormalizeRoomStatusFilters(onlyValues)
	return limit, verbose, filters, err
}

func apiLoadRoomState(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, actorID string, limit int) (agent.RoomSummary, []agent.BoardMessage, error) {
	summary, err := store.GetRoom(ctx, workspaceID, strings.TrimSpace(roomID), actorID)
	if err != nil {
		return agent.RoomSummary{}, nil, err
	}
	if limit <= 0 {
		limit = apiRoomDefaultLimit
	}
	messages, err := store.ListRoomMessages(ctx, workspaceID, strings.TrimSpace(roomID), limit)
	if err != nil {
		return agent.RoomSummary{}, nil, err
	}
	return summary, messages, nil
}

func apiListRoomTasks(ctx context.Context, store taskstore.Store, workspaceID string, messages []agent.BoardMessage, status string) ([]taskstore.Task, error) {
	taskIDs := apiCollectRoomTaskIDs(messages)
	if len(taskIDs) == 0 {
		return []taskstore.Task{}, nil
	}
	allTasks, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(taskIDs))
	for _, id := range taskIDs {
		wanted[id] = struct{}{}
	}
	filtered := make([]taskstore.Task, 0, len(taskIDs))
	for _, task := range allTasks {
		if _, ok := wanted[task.ID]; !ok {
			continue
		}
		if status != "" && task.Status != status {
			continue
		}
		filtered = append(filtered, task)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	return filtered, nil
}

func apiCollectRoomTaskIDs(messages []agent.BoardMessage) []string {
	seen := make(map[string]struct{}, len(messages))
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		id := strings.TrimSpace(msg.TaskID)
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

var errAPIRoomTaskMilestoneNotFound = errors.New("room task milestone not found")

func (req *roomTaskCreateRequest) normalize() {
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	req.ScopePath = strings.TrimSpace(req.ScopePath)
	req.ParentID = strings.TrimSpace(req.ParentID)
	req.MilestoneID = strings.TrimSpace(req.MilestoneID)
	if len(req.DependsOn) == 0 {
		return
	}
	out := make([]string, 0, len(req.DependsOn))
	for _, dep := range req.DependsOn {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		out = append(out, dep)
	}
	req.DependsOn = out
}

func (req roomTaskCreateRequest) validate() error {
	if req.WorkspaceID == "" {
		return fmt.Errorf("workspace_id is required")
	}
	if req.ActorID == "" {
		return fmt.Errorf("actor_id is required")
	}
	if req.Title == "" {
		return fmt.Errorf("title is required")
	}
	return nil
}

func apiRoomTaskEventRecipient(members []agent.RoomMember) (string, error) {
	for _, member := range members {
		if strings.EqualFold(strings.TrimSpace(member.Role), "coordinator") {
			if id := strings.TrimSpace(member.ActorID); id != "" {
				return id, nil
			}
		}
	}
	for _, member := range members {
		if strings.EqualFold(strings.TrimSpace(member.Role), "lead") {
			if id := strings.TrimSpace(member.ActorID); id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("room has no coordinator or lead")
}

type apiRoomMilestoneMeta struct {
	EpicID   string
	LaneKind string
}

func apiParseRoomMilestoneBody(body string) apiRoomMilestoneMeta {
	meta := apiRoomMilestoneMeta{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "EpicID":
			meta.EpicID = strings.TrimSpace(value)
		case "LaneKind":
			meta.LaneKind = strings.TrimSpace(value)
		}
	}
	return meta
}

func apiResolveRoomTaskMilestoneSelection(messages []agent.BoardMessage, selectedMilestoneID string) (string, string, error) {
	selectedMilestoneID = strings.TrimSpace(selectedMilestoneID)
	epics := make(map[string]time.Time)
	closedEpics := make(map[string]struct{})
	summarizedMilestones := make(map[string]struct{})
	type milestoneInfo struct {
		ID        string
		EpicID    string
		LaneKind  string
		CreatedAt time.Time
	}
	milestones := make(map[string]milestoneInfo)

	for _, msg := range messages {
		switch msg.Kind {
		case agent.BoardMessageKindEpic:
			if id := strings.TrimSpace(msg.ID); id != "" {
				epics[id] = msg.CreatedAt
			}
		case agent.BoardMessageKindEpicClose:
			if epicID := strings.TrimSpace(msg.RelatedMessageID); epicID != "" {
				closedEpics[epicID] = struct{}{}
			}
		case agent.BoardMessageKindMilestone:
			id := strings.TrimSpace(msg.ID)
			if id == "" {
				continue
			}
			meta := apiParseRoomMilestoneBody(msg.Body)
			milestones[id] = milestoneInfo{
				ID:        id,
				EpicID:    firstNonEmpty(strings.TrimSpace(meta.EpicID), strings.TrimSpace(msg.RelatedMessageID)),
				LaneKind:  strings.TrimSpace(meta.LaneKind),
				CreatedAt: msg.CreatedAt,
			}
		case agent.BoardMessageKindMilestoneSummary:
			if milestoneID := strings.TrimSpace(msg.RelatedMessageID); milestoneID != "" {
				summarizedMilestones[milestoneID] = struct{}{}
			}
		}
	}

	if selectedMilestoneID == "" {
		type epicCandidate struct {
			ID        string
			CreatedAt time.Time
		}
		candidates := make([]epicCandidate, 0, len(epics))
		for id, createdAt := range epics {
			if _, closed := closedEpics[id]; closed {
				continue
			}
			candidates = append(candidates, epicCandidate{ID: id, CreatedAt: createdAt})
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
		})
		for _, epic := range candidates {
			for _, milestone := range milestones {
				if milestone.EpicID == epic.ID && milestone.LaneKind == "chores" {
					return epic.ID, milestone.ID, nil
				}
			}
		}
		return "", "", nil
	}

	milestone, ok := milestones[selectedMilestoneID]
	if !ok {
		return "", "", fmt.Errorf("%w: %s", errAPIRoomTaskMilestoneNotFound, selectedMilestoneID)
	}
	if _, summarized := summarizedMilestones[selectedMilestoneID]; summarized {
		return "", "", fmt.Errorf("milestone %q is already summarized", selectedMilestoneID)
	}
	if milestone.EpicID == "" {
		return "", "", fmt.Errorf("milestone %q is missing epic linkage", selectedMilestoneID)
	}
	if _, closed := closedEpics[milestone.EpicID]; closed {
		return "", "", fmt.Errorf("milestone %q belongs to a closed epic", selectedMilestoneID)
	}
	return milestone.EpicID, milestone.ID, nil
}

func apiFormatRoomTaskAddedBody(task taskstore.Task) string {
	lines := []string{
		fmt.Sprintf("Task ID: %s", task.ID),
		fmt.Sprintf("Status: %s", task.Status),
	}
	if strings.TrimSpace(task.EpicID) != "" {
		lines = append(lines, fmt.Sprintf("Epic ID: %s", strings.TrimSpace(task.EpicID)))
	}
	if strings.TrimSpace(task.MilestoneID) != "" {
		lines = append(lines, fmt.Sprintf("Milestone ID: %s", strings.TrimSpace(task.MilestoneID)))
	}
	if strings.TrimSpace(task.Description) != "" {
		lines = append(lines, strings.TrimSpace(task.Description))
	}
	return strings.Join(lines, "\n")
}

func convertRoomTask(task taskstore.Task) roomTaskResponse {
	return roomTaskResponse{
		ID:              task.ID,
		WorkspaceID:     task.WorkspaceID,
		EpicID:          task.EpicID,
		MilestoneID:     task.MilestoneID,
		Title:           task.Title,
		Description:     task.Description,
		ScopePath:       task.ScopePath,
		ParentID:        task.ParentID,
		Children:        append([]string(nil), task.Children...),
		DependsOn:       append([]string(nil), task.DependsOn...),
		Status:          task.Status,
		CreatedAt:       task.CreatedAt,
		CompletedAt:     task.CompletedAt,
		AssignedActorID: task.AssignedActorID,
		AssignedAt:      task.AssignedAt,
		OwnerActorID:    task.OwnerActorID,
		ClaimedAt:       task.ClaimedAt,
		HeartbeatAt:     task.HeartbeatAt,
		BlockedReason:   task.BlockedReason,
		BlockedAt:       task.BlockedAt,
		Notes:           task.Notes,
		Gotchas:         task.Gotchas,
	}
}

type roomTaskResponse struct {
	ID              string     `json:"id"`
	WorkspaceID     string     `json:"workspace_id"`
	EpicID          string     `json:"epic_id,omitempty"`
	MilestoneID     string     `json:"milestone_id,omitempty"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	ScopePath       string     `json:"scope_path,omitempty"`
	ParentID        string     `json:"parent_id,omitempty"`
	Children        []string   `json:"children,omitempty"`
	DependsOn       []string   `json:"depends_on,omitempty"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	AssignedActorID string     `json:"assigned_actor_id,omitempty"`
	AssignedAt      *time.Time `json:"assigned_at,omitempty"`
	OwnerActorID    string     `json:"owner_actor_id,omitempty"`
	ClaimedAt       *time.Time `json:"claimed_at,omitempty"`
	HeartbeatAt     *time.Time `json:"heartbeat_at,omitempty"`
	BlockedReason   string     `json:"blocked_reason,omitempty"`
	BlockedAt       *time.Time `json:"blocked_at,omitempty"`
	Notes           string     `json:"notes,omitempty"`
	Gotchas         string     `json:"gotchas,omitempty"`
}

func defaultRoomLoopState(workspaceID, roomID string) coordination.RoomLoop {
	return coordination.RoomLoop{
		WorkspaceID:                  strings.TrimSpace(workspaceID),
		RoomID:                       strings.TrimSpace(roomID),
		Enabled:                      true,
		ManagedBy:                    apiRoomLoopManager,
		PulseInterval:                apiRoomPulseInterval,
		TaskFollowupInterval:         0,
		ReplyStaleAfter:              apiRoomReplyStaleAfter,
		TaskStaleAfter:               apiRoomTaskStaleAfter,
		MinPulseFloor:                apiRoomMinimumPulseFloor,
		InterruptAttemptLimit:        apiRoomInterruptAttemptLimit,
		ReminderBackoffCap:           apiRoomReminderBackoffCap,
		CoordinatorPulseEnabled:      true,
		CoordinatorEscalationEnabled: true,
	}
}

func apiLoadRoomLoop(ctx context.Context, store *coordination.Store, workspaceID, roomID string) (coordination.RoomLoop, error) {
	loop, err := store.GetRoomLoop(ctx, workspaceID, roomID)
	if err != nil {
		return coordination.RoomLoop{}, err
	}
	if loop == nil {
		def := defaultRoomLoopState(workspaceID, roomID)
		return def, nil
	}
	if loop.MinPulseFloor <= 0 {
		loop.MinPulseFloor = apiRoomMinimumPulseFloor
	}
	if loop.InterruptAttemptLimit <= 0 {
		loop.InterruptAttemptLimit = apiRoomInterruptAttemptLimit
	}
	if loop.ReminderBackoffCap <= 0 {
		loop.ReminderBackoffCap = apiRoomReminderBackoffCap
	}
	if loop.PulseInterval <= 0 {
		loop.PulseInterval = apiRoomPulseInterval
	}
	if loop.ReplyStaleAfter <= 0 {
		loop.ReplyStaleAfter = apiRoomReplyStaleAfter
	}
	if loop.TaskStaleAfter <= 0 {
		loop.TaskStaleAfter = apiRoomTaskStaleAfter
	}
	if strings.TrimSpace(loop.ManagedBy) == "" {
		loop.ManagedBy = apiRoomLoopManager
	}
	return *loop, nil
}

func apiConvertRoomLoop(loop coordination.RoomLoop) roomLoopResponse {
	return roomLoopResponse{
		Enabled:                      loop.Enabled,
		ManagedBy:                    loop.ManagedBy,
		LastTickAt:                   loop.LastTickAt,
		DeliveryLeaseName:            loop.DeliveryLeaseName,
		DeliveryOwnerID:              loop.DeliveryOwnerID,
		DeliveryCursorMessageID:      loop.DeliveryCursorMessageID,
		DeliveryCursorAt:             loop.DeliveryCursorAt,
		PulseInterval:                loop.PulseInterval.String(),
		TaskFollowupInterval:         loop.TaskFollowupInterval.String(),
		ReplyStaleAfter:              loop.ReplyStaleAfter.String(),
		TaskStaleAfter:               loop.TaskStaleAfter.String(),
		MinPulseFloor:                loop.MinPulseFloor.String(),
		InterruptAttemptLimit:        loop.InterruptAttemptLimit,
		ReminderBackoffCap:           loop.ReminderBackoffCap,
		CoordinatorPulseEnabled:      loop.CoordinatorPulseEnabled,
		CoordinatorEscalationEnabled: loop.CoordinatorEscalationEnabled,
		LastDeliveryTrace:            apiConvertRoomLoopDeliveryTrace(loop.LastDeliveryTrace),
	}
}

func apiConvertRoomLoopDeliveryTrace(trace *coordination.RoomLoopDeliveryTrace) *roomLoopDeliveryTraceResponse {
	if trace == nil {
		return nil
	}
	resp := &roomLoopDeliveryTraceResponse{
		WorkspaceID:             strings.TrimSpace(trace.WorkspaceID),
		RoomID:                  strings.TrimSpace(trace.RoomID),
		MessageID:               strings.TrimSpace(trace.MessageID),
		TaskID:                  strings.TrimSpace(trace.TaskID),
		Recipient:               strings.TrimSpace(trace.Recipient),
		DeliveryLeaseName:       strings.TrimSpace(trace.DeliveryLeaseName),
		DeliveryOwnerID:         strings.TrimSpace(trace.DeliveryOwnerID),
		RelayBackend:            strings.TrimSpace(trace.RelayBackend),
		ChosenActorID:           strings.TrimSpace(trace.ChosenActorID),
		ChosenMuxBackend:        strings.TrimSpace(trace.ChosenMuxBackend),
		ChosenMuxSession:        strings.TrimSpace(trace.ChosenMuxSession),
		ChosenMuxPaneID:         strings.TrimSpace(trace.ChosenMuxPaneID),
		ChosenTransportEndpoint: strings.TrimSpace(trace.ChosenTransportEndpoint),
		ChosenTransportKind:     strings.TrimSpace(trace.ChosenTransportKind),
		ChosenSubmitMode:        strings.TrimSpace(trace.ChosenSubmitMode),
		FallbackAttempted:       trace.FallbackAttempted,
		DeliveredCount:          trace.DeliveredCount,
		FailedCount:             trace.FailedCount,
		DeliveredTo:             append([]string(nil), trace.DeliveredTo...),
		FailedMembers:           append([]string(nil), trace.FailedMembers...),
		Outcome:                 strings.TrimSpace(trace.Outcome),
		CursorBeforeMessageID:   strings.TrimSpace(trace.CursorBeforeMessageID),
		CursorAfterMessageID:    strings.TrimSpace(trace.CursorAfterMessageID),
		CursorAdvanced:          trace.CursorAdvanced,
	}
	if !trace.ObservedAt.IsZero() {
		ts := trace.ObservedAt.UTC()
		resp.ObservedAt = &ts
	}
	return resp
}

func apiApplyRoomLoopPatch(current coordination.RoomLoop, req roomLoopPatchRequest) (coordination.RoomLoop, error) {
	updated := current
	if req.Enabled != nil {
		updated.Enabled = *req.Enabled
	}
	if req.CoordinatorPulseEnabled != nil {
		updated.CoordinatorPulseEnabled = *req.CoordinatorPulseEnabled
	}
	if req.CoordinatorEscalationEnabled != nil {
		updated.CoordinatorEscalationEnabled = *req.CoordinatorEscalationEnabled
	}
	if req.PulseInterval != nil {
		d, err := apiParsePositiveDuration(*req.PulseInterval, "pulse_interval")
		if err != nil {
			return coordination.RoomLoop{}, err
		}
		updated.PulseInterval = d
	}
	if req.TaskFollowupInterval != nil {
		d, err := apiParseNonNegativeDuration(*req.TaskFollowupInterval, "task_followup_interval")
		if err != nil {
			return coordination.RoomLoop{}, err
		}
		updated.TaskFollowupInterval = d
	}
	if req.ReplyStaleAfter != nil {
		d, err := apiParsePositiveDuration(*req.ReplyStaleAfter, "reply_stale_after")
		if err != nil {
			return coordination.RoomLoop{}, err
		}
		updated.ReplyStaleAfter = d
	}
	if req.TaskStaleAfter != nil {
		d, err := apiParsePositiveDuration(*req.TaskStaleAfter, "task_stale_after")
		if err != nil {
			return coordination.RoomLoop{}, err
		}
		updated.TaskStaleAfter = d
	}
	if req.MinPulseFloor != nil {
		d, err := apiParsePositiveDuration(*req.MinPulseFloor, "min_pulse_floor")
		if err != nil {
			return coordination.RoomLoop{}, err
		}
		updated.MinPulseFloor = d
	}
	if updated.MinPulseFloor <= 0 {
		updated.MinPulseFloor = apiRoomMinimumPulseFloor
	}
	if req.InterruptAttemptLimit != nil {
		if *req.InterruptAttemptLimit <= 0 {
			return coordination.RoomLoop{}, fmt.Errorf("interrupt_attempt_limit must be positive")
		}
		updated.InterruptAttemptLimit = *req.InterruptAttemptLimit
	}
	if req.ReminderBackoffCap != nil {
		if *req.ReminderBackoffCap <= 0 {
			return coordination.RoomLoop{}, fmt.Errorf("reminder_backoff_cap must be positive")
		}
		updated.ReminderBackoffCap = *req.ReminderBackoffCap
	}
	if updated.InterruptAttemptLimit <= 0 {
		updated.InterruptAttemptLimit = apiRoomInterruptAttemptLimit
	}
	if updated.ReminderBackoffCap <= 0 {
		updated.ReminderBackoffCap = apiRoomReminderBackoffCap
	}
	if !updated.Enabled && !updated.CoordinatorPulseEnabled {
		return coordination.RoomLoop{}, fmt.Errorf("disable everything forever is not allowed")
	}
	return updated, nil
}

func apiParsePositiveDuration(raw string, field string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("%s is required", field)
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid %s duration", field)
	}
	return d, nil
}

func apiParseNonNegativeDuration(raw string, field string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("%s is required", field)
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid %s duration", field)
	}
	return d, nil
}

func apiRoomCoordinatorActorID(members []agent.RoomMember) string {
	for _, member := range members {
		if strings.EqualFold(strings.TrimSpace(member.Role), "coordinator") {
			return strings.TrimSpace(member.ActorID)
		}
	}
	return ""
}

func apiRoomIsLocalSuperuser(actorID string) bool {
	switch strings.TrimSpace(strings.ToLower(actorID)) {
	case "dev-local-user", "local-dev-user":
		return true
	default:
		return false
	}
}

func apiRoomActorHasCoordinatorAccess(members []agent.RoomMember, actorID string) bool {
	return apiRoomIsLocalSuperuser(actorID) || apiRoomMemberHasRole(members, actorID, "coordinator")
}

func apiRoomMemberHasRole(members []agent.RoomMember, actorID, role string) bool {
	role = strings.TrimSpace(strings.ToLower(role))
	for _, member := range members {
		if apiSameRoomParticipant(member.ActorID, actorID) && strings.EqualFold(strings.TrimSpace(member.Role), role) {
			return true
		}
	}
	return false
}

func apiRoomHasParticipant(room agent.RoomSummary, actorID string) bool {
	for _, participant := range room.Participants {
		if apiSameRoomParticipant(participant, actorID) {
			return true
		}
	}
	for _, member := range room.Members {
		if apiSameRoomParticipant(member.ActorID, actorID) {
			return true
		}
	}
	return false
}

func apiFallbackRoomValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(none)"
	}
	return value
}

func apiSameRoomParticipant(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if strings.EqualFold(a, b) {
		return true
	}
	return strings.EqualFold(strings.TrimPrefix(a, "@"), strings.TrimPrefix(b, "@"))
}

func apiBuildRoomTaskPulseSummary(tasks []taskstore.Task, now time.Time, staleAfter time.Duration) roomTaskPulseSummaryResponse {
	var pulse roomTaskPulseSummaryResponse
	for _, task := range tasks {
		switch task.Status {
		case taskstore.StatusPending:
			pulse.Pending++
			if strings.TrimSpace(task.AssignedActorID) != "" {
				pulse.AssignedUnclaimed++
			}
		case taskstore.StatusInProgress:
			pulse.InProgress++
		case taskstore.StatusBlocked:
			pulse.Blocked++
		case taskstore.StatusCompleted:
			pulse.Completed++
		}
		if apiTaskIsStale(task, now, staleAfter) {
			pulse.Stale++
		}
	}
	return pulse
}

func apiBuildRoomStatusParticipants(room agent.RoomSummary, messages []agent.BoardMessage, tasks []taskstore.Task, staleAfter time.Duration) []roomStatusParticipantResponse {
	latestBySender := apiLatestRoomSenderActivity(messages)
	participantSet := map[string]struct{}{}
	for _, member := range room.Members {
		if id := strings.TrimSpace(member.ActorID); id != "" {
			participantSet[id] = struct{}{}
		}
	}
	for _, participant := range room.Participants {
		if id := strings.TrimSpace(participant); id != "" && !strings.HasPrefix(id, "actor:system:room:") {
			participantSet[id] = struct{}{}
		}
	}
	now := time.Now().UTC()
	participants := make([]roomStatusParticipantResponse, 0, len(participantSet))
	for actorID := range participantSet {
		p := roomStatusParticipantResponse{
			ActorID: actorID,
			Role:    apiRoomMemberRole(room.Members, actorID),
			Status:  "idle",
		}
		if ts, ok := latestBySender[actorID]; ok {
			tsCopy := ts
			p.LastActiveAt = &tsCopy
			if staleAfter > 0 && now.Sub(ts) > staleAfter {
				p.Status = "stale"
			} else {
				p.Status = "active"
			}
		}
		for _, task := range tasks {
			if apiSameRoomParticipant(task.AssignedActorID, actorID) {
				p.AssignedTaskCount++
			}
			if apiSameRoomParticipant(task.OwnerActorID, actorID) {
				p.OwnedTaskCount++
			}
		}
		entries := apiBuildRoomStatusEntries(actorID, messages)
		p.ActionableInboxCount = len(entries)
		if len(entries) > 0 {
			entry := apiRoomStatusEntryFromInbox(entries[0])
			p.LatestActionable = &entry
		}
		participants = append(participants, p)
	}
	sort.SliceStable(participants, func(i, j int) bool { return participants[i].ActorID < participants[j].ActorID })
	return participants
}

func apiBuildRoomStatusBacklog(room agent.RoomSummary, messages []agent.BoardMessage) roomStatusBacklogResponse {
	backlog := roomStatusBacklogResponse{}
	for _, participant := range room.Participants {
		if strings.HasPrefix(strings.TrimSpace(participant), "actor:system:room:") {
			continue
		}
		entries := apiBuildRoomStatusEntries(participant, messages)
		if len(entries) == 0 {
			continue
		}
		backlog.ParticipantsWithPending++
		backlog.LatestByParticipant = append(backlog.LatestByParticipant, apiRoomStatusEntryFromInbox(entries[0]))
		for _, entry := range entries {
			for _, flag := range entry.Flags {
				switch flag {
				case "ACK-REQUIRED":
					backlog.PendingAcks++
				case "REPLY-EXPECTED":
					backlog.PendingReplies++
				}
			}
		}
	}
	sort.SliceStable(backlog.LatestByParticipant, func(i, j int) bool {
		if !backlog.LatestByParticipant[i].CreatedAt.Equal(backlog.LatestByParticipant[j].CreatedAt) {
			return backlog.LatestByParticipant[i].CreatedAt.After(backlog.LatestByParticipant[j].CreatedAt)
		}
		return backlog.LatestByParticipant[i].Recipient < backlog.LatestByParticipant[j].Recipient
	})
	return backlog
}

func apiBuildRoomStatusActionRequired(room agent.RoomSummary, messages []agent.BoardMessage, tasks []taskstore.Task, backlog roomStatusBacklogResponse, taskPulse roomTaskPulseSummaryResponse, filters map[string]struct{}, staleAfter time.Duration, now time.Time, _ bool) roomStatusActionRequiredResponse {
	return roomStatusActionRequiredResponse{
		ParticipantsWithPending: apiRoomStatusFilteredCount(filters, "ack", "reply", backlog.ParticipantsWithPending),
		PendingAcks:             apiRoomStatusFilteredCount(filters, "ack", "", backlog.PendingAcks),
		PendingReplies:          apiRoomStatusFilteredCount(filters, "reply", "", backlog.PendingReplies),
		AssignedUnclaimed:       apiRoomStatusFilteredCount(filters, "assigned", "", taskPulse.AssignedUnclaimed),
		BlockedTasks:            apiRoomStatusFilteredCount(filters, "blocked", "", taskPulse.Blocked),
		StaleTasks:              apiRoomStatusFilteredCount(filters, "stale", "", taskPulse.Stale),
		Filter:                  apiSortedRoomStatusFilters(filters),
		TopEntries:              apiFilterRoomStatusEntries(backlog.LatestByParticipant, filters),
		TopTasks:                apiBuildRoomStatusTaskEntries(tasks, filters, now, staleAfter),
	}
}

func apiBuildRoomStatusTaskEntries(tasks []taskstore.Task, filters map[string]struct{}, now time.Time, staleAfter time.Duration) []roomStatusTaskResponse {
	out := make([]roomStatusTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		signals := apiRoomStatusTaskSignals(task, now, staleAfter)
		if len(signals) == 0 {
			continue
		}
		filtered := apiFilterRoomStatusTaskSignals(signals, filters)
		if len(filtered) == 0 {
			continue
		}
		out = append(out, roomStatusTaskResponse{
			ID:              task.ID,
			Title:           task.Title,
			Status:          task.Status,
			AssignedActorID: task.AssignedActorID,
			OwnerActorID:    task.OwnerActorID,
			BlockedReason:   task.BlockedReason,
			HeartbeatAt:     task.HeartbeatAt,
			Signals:         filtered,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := apiRoomStatusTaskPriority(out[i].Signals)
		right := apiRoomStatusTaskPriority(out[j].Signals)
		if left != right {
			return left < right
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func apiBuildRoomInboxEntries(actorID string, messages []agent.BoardMessage, filter string, includeBroadcasts bool) []roomInboxEntryResponse {
	normalized := apiNormalizeRoomInboxFilter(filter)
	latestBySender := apiLatestRoomSenderActivity(messages)
	entries := make([]roomInboxEntryResponse, 0, len(messages))
	for _, msg := range messages {
		entry, ok := apiRoomInboxEntryForActor(actorID, msg, includeBroadcasts, latestBySender)
		if !ok {
			continue
		}
		if normalized != "all" && entry.Category != normalized {
			continue
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Priority != entries[j].Priority {
			return entries[i].Priority < entries[j].Priority
		}
		if !entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].CreatedAt.Before(entries[j].CreatedAt)
		}
		return entries[i].ID < entries[j].ID
	})
	return entries
}

func apiRoomInboxEntryForActor(actorID string, msg agent.BoardMessage, includeBroadcasts bool, latestBySender map[string]time.Time) (roomInboxEntryResponse, bool) {
	recipient := apiNormalizeRoomRecipient(msg.Recipient)
	isDirect := apiSameRoomParticipant(recipient, actorID)
	isBroadcast := recipient == agent.BroadcastRecipient
	if !isDirect && !isBroadcast {
		return roomInboxEntryResponse{}, false
	}
	if msg.Status == agent.BoardMessageStatusAcked {
		return roomInboxEntryResponse{}, false
	}
	if msg.ReplyExpected && !apiMessageStillAwaitsReply(msg, latestBySender) {
		return roomInboxEntryResponse{}, false
	}
	flags := make([]string, 0, 2)
	if msg.AckRequired && msg.Status != agent.BoardMessageStatusAcked {
		flags = append(flags, "ACK-REQUIRED")
	}
	if msg.ReplyExpected && msg.Status != agent.BoardMessageStatusAcked {
		flags = append(flags, "REPLY-EXPECTED")
	}
	category := "direct"
	if msg.AckRequired && msg.Status != agent.BoardMessageStatusAcked {
		category = "ack-required"
	} else if msg.ReplyExpected && msg.Status != agent.BoardMessageStatusAcked {
		category = "reply-expected"
	} else if isBroadcast {
		category = "broadcast"
	}
	if isBroadcast && !includeBroadcasts && category == "broadcast" {
		return roomInboxEntryResponse{}, false
	}
	return roomInboxEntryResponse{
		ID:        msg.ID,
		Sender:    msg.Sender,
		Recipient: recipient,
		Subject:   msg.Subject,
		Priority:  msg.Priority,
		Status:    msg.Status,
		CreatedAt: msg.CreatedAt,
		Category:  category,
		Flags:     flags,
		Preview:   apiSummarizeRoomPreview(msg.Body),
	}, true
}

func apiLatestRoomSenderActivity(messages []agent.BoardMessage) map[string]time.Time {
	latest := make(map[string]time.Time, len(messages))
	for _, msg := range messages {
		sender := strings.TrimSpace(msg.Sender)
		if sender == "" {
			continue
		}
		if ts, ok := latest[sender]; !ok || msg.CreatedAt.After(ts) {
			latest[sender] = msg.CreatedAt
		}
	}
	return latest
}

func apiMessageStillAwaitsReply(msg agent.BoardMessage, latestBySender map[string]time.Time) bool {
	if !msg.ReplyExpected {
		return false
	}
	recipient := apiNormalizeRoomRecipient(msg.Recipient)
	if recipient == agent.BroadcastRecipient {
		return false
	}
	latestReply, ok := latestBySender[recipient]
	if !ok {
		return true
	}
	return latestReply.Before(msg.CreatedAt)
}

func apiNormalizeRoomInboxFilter(filter string) string {
	switch strings.TrimSpace(strings.ToLower(filter)) {
	case "ack-required", "reply-expected", "direct", "broadcast":
		return strings.TrimSpace(strings.ToLower(filter))
	default:
		return "all"
	}
}

func apiBuildRoomStatusEntries(actorID string, messages []agent.BoardMessage) []roomInboxEntryResponse {
	entries := apiBuildRoomInboxEntries(actorID, messages, "all", false)
	if len(entries) == 0 {
		return nil
	}
	latestByChain := make(map[string]roomInboxEntryResponse, len(entries))
	for _, entry := range entries {
		key := apiRoomMessageChainKey(entry.ID, messages)
		current, ok := latestByChain[key]
		if !ok || apiRoomStatusEntryMoreRecent(entry, current) {
			latestByChain[key] = entry
		}
	}
	out := make([]roomInboxEntryResponse, 0, len(latestByChain))
	for _, entry := range latestByChain {
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func apiRoomMessageChainKey(entryID string, messages []agent.BoardMessage) string {
	for _, msg := range messages {
		if msg.ID != entryID {
			continue
		}
		if strings.TrimSpace(msg.RelatedMessageID) != "" {
			return strings.TrimSpace(msg.RelatedMessageID)
		}
		return strings.TrimSpace(msg.ID)
	}
	return entryID
}

func apiRoomStatusEntryMoreRecent(left, right roomInboxEntryResponse) bool {
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.After(right.CreatedAt)
	}
	if left.Priority != right.Priority {
		return left.Priority < right.Priority
	}
	return left.ID < right.ID
}

func apiRoomStatusEntryFromInbox(entry roomInboxEntryResponse) roomStatusEntryResponse {
	return roomStatusEntryResponse{
		ID:        entry.ID,
		Sender:    entry.Sender,
		Recipient: entry.Recipient,
		Subject:   entry.Subject,
		Priority:  entry.Priority,
		Status:    entry.Status,
		CreatedAt: entry.CreatedAt,
		Category:  entry.Category,
		Flags:     append([]string(nil), entry.Flags...),
		Preview:   entry.Preview,
	}
}

func apiNormalizeRoomStatusFilters(values []string) (map[string]struct{}, error) {
	allowed := map[string]struct{}{"all": {}, "ack": {}, "reply": {}, "assigned": {}, "blocked": {}, "stale": {}}
	filters := make(map[string]struct{})
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			value := strings.TrimSpace(strings.ToLower(part))
			if value == "" {
				continue
			}
			if _, ok := allowed[value]; !ok {
				return nil, fmt.Errorf("unsupported room status filter %q", value)
			}
			if value == "all" {
				return map[string]struct{}{"all": {}}, nil
			}
			filters[value] = struct{}{}
		}
	}
	if len(filters) == 0 {
		return map[string]struct{}{"all": {}}, nil
	}
	return filters, nil
}

func apiRoomStatusIncludesAll(filters map[string]struct{}) bool {
	_, ok := filters["all"]
	return ok
}

func apiSortedRoomStatusFilters(filters map[string]struct{}) []string {
	out := make([]string, 0, len(filters))
	for k := range filters {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func apiRoomStatusFilteredCount(filters map[string]struct{}, primary, secondary string, value int) int {
	if apiRoomStatusIncludesAll(filters) {
		return value
	}
	if primary != "" {
		if _, ok := filters[primary]; ok {
			return value
		}
	}
	if secondary != "" {
		if _, ok := filters[secondary]; ok {
			return value
		}
	}
	return 0
}

func apiFilterRoomStatusEntries(entries []roomStatusEntryResponse, filters map[string]struct{}) []roomStatusEntryResponse {
	if apiRoomStatusIncludesAll(filters) {
		return append([]roomStatusEntryResponse(nil), entries...)
	}
	out := make([]roomStatusEntryResponse, 0, len(entries))
	for _, entry := range entries {
		if apiRoomStatusEntryMatchesFilters(entry, filters) {
			out = append(out, entry)
		}
	}
	return out
}

func apiRoomStatusEntryMatchesFilters(entry roomStatusEntryResponse, filters map[string]struct{}) bool {
	if apiRoomStatusIncludesAll(filters) {
		return true
	}
	for _, flag := range entry.Flags {
		switch flag {
		case "ACK-REQUIRED":
			if _, ok := filters["ack"]; ok {
				return true
			}
		case "REPLY-EXPECTED":
			if _, ok := filters["reply"]; ok {
				return true
			}
		}
	}
	return false
}

func apiRoomStatusTaskSignals(task taskstore.Task, now time.Time, staleAfter time.Duration) []string {
	signals := make([]string, 0, 3)
	if task.Status == taskstore.StatusPending && strings.TrimSpace(task.AssignedActorID) != "" {
		signals = append(signals, "assigned")
	}
	if task.Status == taskstore.StatusBlocked {
		signals = append(signals, "blocked")
	}
	if apiTaskIsStale(task, now, staleAfter) {
		signals = append(signals, "stale")
	}
	return signals
}

func apiFilterRoomStatusTaskSignals(signals []string, filters map[string]struct{}) []string {
	if apiRoomStatusIncludesAll(filters) {
		return append([]string(nil), signals...)
	}
	out := make([]string, 0, len(signals))
	for _, signal := range signals {
		if _, ok := filters[signal]; ok {
			out = append(out, signal)
		}
	}
	return out
}

func apiRoomStatusTaskPriority(signals []string) int {
	for _, signal := range signals {
		if signal == "stale" {
			return 0
		}
	}
	for _, signal := range signals {
		if signal == "blocked" {
			return 1
		}
	}
	return 2
}

func apiTaskIsStale(task taskstore.Task, now time.Time, staleAfter time.Duration) bool {
	if staleAfter <= 0 || strings.TrimSpace(task.OwnerActorID) == "" {
		return false
	}
	if task.Status != taskstore.StatusInProgress && task.Status != taskstore.StatusBlocked {
		return false
	}
	reference := task.CreatedAt
	if task.HeartbeatAt != nil {
		reference = *task.HeartbeatAt
	} else if task.ClaimedAt != nil {
		reference = *task.ClaimedAt
	}
	return now.Sub(reference) > staleAfter
}

func apiRoomMemberRole(members []agent.RoomMember, actorID string) string {
	for _, member := range members {
		if apiSameRoomParticipant(member.ActorID, actorID) {
			return strings.TrimSpace(member.Role)
		}
	}
	return ""
}

func apiSummarizeRoomPreview(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= 140 {
		return body
	}
	return body[:140] + "..."
}

func apiNormalizeRoomRecipient(recipient string) string {
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return agent.BroadcastRecipient
	}
	return recipient
}

func apiResolveRoomMessageIDsForResolve(ctx context.Context, store blackboard.BoardStore, workspaceID string, summary agent.RoomSummary, resolveAll bool, only []string, messageIDs []string) ([]string, error) {
	trimmedIDs := make([]string, 0, len(messageIDs))
	for _, id := range messageIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		trimmedIDs = append(trimmedIDs, id)
	}
	if resolveAll {
		messages, err := store.ListRoomMessages(ctx, workspaceID, summary.ID, apiRoomTaskScanLimit)
		if err != nil {
			return nil, err
		}
		filters, err := apiNormalizeRoomResolveFilters(only)
		if err != nil {
			return nil, err
		}
		for _, participant := range summary.Participants {
			if strings.HasPrefix(strings.TrimSpace(participant), "actor:system:room:") {
				continue
			}
			for _, entry := range apiBuildRoomStatusEntries(participant, messages) {
				if apiRoomResolveEntryMatches(entry, filters) {
					trimmedIDs = append(trimmedIDs, entry.ID)
				}
			}
		}
	}
	if len(trimmedIDs) == 0 {
		return nil, fmt.Errorf("at least one matching room message is required")
	}
	return trimmedIDs, nil
}

func apiNormalizeRoomResolveFilters(values []string) (map[string]struct{}, error) {
	allowed := map[string]struct{}{"all": {}, "ack": {}, "reply": {}, "direct": {}}
	filters := make(map[string]struct{})
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			value := strings.TrimSpace(strings.ToLower(part))
			if value == "" {
				continue
			}
			if _, ok := allowed[value]; !ok {
				return nil, fmt.Errorf("unsupported room resolve filter %q", value)
			}
			if value == "all" {
				return map[string]struct{}{"all": {}}, nil
			}
			filters[value] = struct{}{}
		}
	}
	if len(filters) == 0 {
		return map[string]struct{}{"all": {}}, nil
	}
	return filters, nil
}

func apiRoomResolveEntryMatches(entry roomInboxEntryResponse, filters map[string]struct{}) bool {
	if apiRoomStatusIncludesAll(filters) {
		return true
	}
	if _, ok := filters["direct"]; ok {
		return true
	}
	for _, flag := range entry.Flags {
		switch flag {
		case "ACK-REQUIRED":
			if _, ok := filters["ack"]; ok {
				return true
			}
		case "REPLY-EXPECTED":
			if _, ok := filters["reply"]; ok {
				return true
			}
		}
	}
	return false
}

func apiExpandRoomResolveMessageIDs(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID string, messageIDs []string) ([]string, error) {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, apiRoomTaskScanLimit)
	if err != nil {
		return nil, fmt.Errorf("list room messages: %w", err)
	}
	byID := make(map[string]agent.BoardMessage, len(messages))
	for _, msg := range messages {
		byID[msg.ID] = msg
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(messageIDs))
	for _, id := range messageIDs {
		msg, ok := byID[id]
		if !ok {
			if _, exists := seen[id]; !exists {
				seen[id] = struct{}{}
				out = append(out, id)
			}
			continue
		}
		chain := strings.TrimSpace(msg.RelatedMessageID)
		if chain == "" {
			chain = strings.TrimSpace(msg.ID)
		}
		for _, candidate := range messages {
			candidateChain := strings.TrimSpace(candidate.RelatedMessageID)
			if candidateChain == "" {
				candidateChain = strings.TrimSpace(candidate.ID)
			}
			if candidateChain != chain {
				continue
			}
			if _, exists := seen[candidate.ID]; exists {
				continue
			}
			seen[candidate.ID] = struct{}{}
			out = append(out, candidate.ID)
		}
	}
	return out, nil
}

func apiFormatRoomTaskActionMessage(action string, task taskstore.Task, actorID, recipient, reason string) (string, string) {
	subject := fmt.Sprintf("Task %s: %s", action, task.Title)
	lines := []string{
		fmt.Sprintf("Task ID: %s", task.ID),
		fmt.Sprintf("Status: %s", task.Status),
	}
	switch action {
	case "claim", "touch", "block", "unblock", "complete", "abandon":
		if task.OwnerActorID != "" {
			lines = append(lines, fmt.Sprintf("Owner: %s", task.OwnerActorID))
		}
	case "assign", "reassign":
		lines = append(lines, fmt.Sprintf("Assigned to: %s", recipient))
	case "reclaim":
		lines = append(lines, fmt.Sprintf("Reclaimed by: %s", actorID))
	}
	if task.BlockedReason != "" {
		lines = append(lines, "Blocked reason: "+task.BlockedReason)
	}
	if reason != "" {
		lines = append(lines, "Reason: "+reason)
	}
	return subject, strings.Join(lines, "\n")
}
