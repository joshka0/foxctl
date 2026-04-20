package httpjson

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker"
	"github.com/joshka0/foxctl/internal/atcp/broker/room"
	"github.com/joshka0/foxctl/internal/atcp/broker/router"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
	"github.com/joshka0/foxctl/internal/atcp/broker/storage"
)

// --- room wire types ---

// CreateRoomRequest is the JSON body for POST /v1/rooms.
type CreateRoomRequest struct {
	Workspace   string `json:"workspace"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// RoomResponse is the wire form of a room record.
type RoomResponse struct {
	ID          string    `json:"id"`
	Workspace   string    `json:"workspace"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ArchivedAt  time.Time `json:"archived_at,omitempty"`
}

// JoinRoomRequest is the JSON body for POST /v1/rooms/{id}/join. Uses the
// "bind existing session" variant — auto-spawn join composes a CreateSession
// call client-side for now.
type JoinRoomRequest struct {
	AgentID    string `json:"agent_id"`
	SessionID  string `json:"session_id"`
	InboxID    string `json:"inbox_id,omitempty"`
	Role       string `json:"role,omitempty"`
	CanMutate  bool   `json:"can_mutate,omitempty"`
	ImportHint string `json:"import_hint,omitempty"`
}

// LeaveRoomRequest is the JSON body for POST /v1/rooms/{id}/leave.
type LeaveRoomRequest struct {
	AgentID string `json:"agent_id"`
}

// MemberResponse is the wire form of a room member row.
type MemberResponse struct {
	RoomID     string    `json:"room_id"`
	AgentID    string    `json:"agent_id"`
	SessionID  string    `json:"session_id"`
	InboxID    string    `json:"inbox_id"`
	Role       string    `json:"role,omitempty"`
	RoleCustom string    `json:"role_custom,omitempty"`
	CanMutate  bool      `json:"can_mutate"`
	ImportHint string    `json:"import_hint,omitempty"`
	JoinedAt   time.Time `json:"joined_at"`
	LeftAt     time.Time `json:"left_at,omitempty"`
}

// SendMessageRequest is the JSON body for POST /v1/messages. For this slice
// the only supported delivery is "terminal" (the default) and the only
// supported body is plain text.
type SendMessageRequest struct {
	RoomID           string   `json:"room_id"`
	Source           string   `json:"source,omitempty"`
	CorrelationID    string   `json:"correlation_id,omitempty"`
	ReplyToMessageID string   `json:"reply_to_message_id,omitempty"`
	Text             string   `json:"text"`
	SubmitKey        string   `json:"submit_key,omitempty"`
	Delivery         string   `json:"delivery,omitempty"`
	SkipAgents       []string `json:"skip_agents,omitempty"`
	ReceiptVisible   *bool    `json:"receipt_visible,omitempty"`
	AwaitActivityMS  int64    `json:"await_activity_ms,omitempty"`
	AwaitReadyMS     int64    `json:"await_ready_ms,omitempty"`
	TerminalPolicy   string   `json:"terminal_policy,omitempty"`
	PolicyTimeoutMS  int64    `json:"policy_timeout_ms,omitempty"`
	InterruptKey     string   `json:"interrupt_key,omitempty"`
}

// SendMessageResponse is the aggregate outcome of a fan-out.
type SendMessageResponse struct {
	MessageID string                `json:"message_id"`
	Receipt   MessageReceiptDTO     `json:"receipt"`
	Delivered int                   `json:"delivered"`
	Failed    int                   `json:"failed"`
	Members   []MemberResultWireDTO `json:"members"`
}

// MessageReceiptDTO is the wire form of router.MessageReceipt.
type MessageReceiptDTO struct {
	MessageID        string `json:"message_id"`
	RoomID           string `json:"room_id"`
	Source           string `json:"source,omitempty"`
	CorrelationID    string `json:"correlation_id,omitempty"`
	ReplyToMessageID string `json:"reply_to_message_id,omitempty"`
	ReplyPrefix      string `json:"reply_prefix"`
}

// MessageRecordResponse is the wire form of one room message audit record.
type MessageRecordResponse struct {
	ID               string                    `json:"id"`
	RoomID           string                    `json:"room_id"`
	Source           string                    `json:"source,omitempty"`
	CorrelationID    string                    `json:"correlation_id,omitempty"`
	ReplyToMessageID string                    `json:"reply_to_message_id,omitempty"`
	Text             string                    `json:"text"`
	Delivery         string                    `json:"delivery,omitempty"`
	ReceiptVisible   bool                      `json:"receipt_visible"`
	SentAt           time.Time                 `json:"sent_at"`
	Delivered        int                       `json:"delivered"`
	Failed           int                       `json:"failed"`
	Members          []MessageDeliveryResponse `json:"members,omitempty"`
}

// MessageDeliveryResponse is the wire form of one member delivery result.
type MessageDeliveryResponse struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Delivered bool   `json:"delivered"`
	Err       string `json:"error,omitempty"`
}

// MemberResultWireDTO mirrors router.MemberResult on the wire. The Err field
// is stringified so clients see a stable JSON shape regardless of the
// underlying error type.
type MemberResultWireDTO struct {
	AgentID   string              `json:"agent_id"`
	SessionID string              `json:"session_id"`
	Delivered bool                `json:"delivered"`
	Activity  *MessageActivityDTO `json:"activity,omitempty"`
	Err       string              `json:"error,omitempty"`
}

// MessageActivityDTO reports opt-in per-recipient response timing for a
// message. "completed" means the recipient produced output after delivery and
// then returned to its configured readiness state before the await-ready
// timeout.
type MessageActivityDTO struct {
	OutputChanged            bool    `json:"output_changed"`
	Completed                bool    `json:"completed"`
	Ready                    bool    `json:"ready"`
	FirstOutputMS            int64   `json:"first_output_ms,omitempty"`
	CompletedMS              int64   `json:"completed_ms,omitempty"`
	AwaitActivityTimedOut    bool    `json:"await_activity_timed_out,omitempty"`
	AwaitReadyTimedOut       bool    `json:"await_ready_timed_out,omitempty"`
	BaselineSeq              uint64  `json:"baseline_seq"`
	CurrentSeq               uint64  `json:"current_seq"`
	SeqDelta                 uint64  `json:"seq_delta"`
	BaselineOutputBytesTotal int64   `json:"baseline_output_bytes_total"`
	OutputBytesTotal         int64   `json:"output_bytes_total"`
	OutputBytesDelta         int64   `json:"output_bytes_delta"`
	OutputRateBPS            float64 `json:"output_rate_bps"`
}

// --- handlers ---

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	var req CreateRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rm, err := s.broker.CreateRoom(r.Context(), room.CreateRoomRequest{
		Workspace:   req.Workspace,
		Title:       req.Title,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, room.ErrWorkspaceRequired) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toRoomResponse(rm))
}

func (s *Server) listRooms(w http.ResponseWriter, _ *http.Request) {
	rooms := s.broker.Rooms().ListRooms()
	out := make([]RoomResponse, 0, len(rooms))
	for _, rm := range rooms {
		out = append(out, toRoomResponse(rm))
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": out})
}

func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rm, err := s.broker.Rooms().GetRoom(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toRoomResponse(rm))
}

func (s *Server) joinRoom(w http.ResponseWriter, r *http.Request) {
	var req JoinRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	mem, err := s.broker.JoinRoom(r.Context(), room.JoinRequest{
		RoomID:     id,
		AgentID:    req.AgentID,
		SessionID:  req.SessionID,
		InboxID:    req.InboxID,
		Role:       room.Role(req.Role),
		CanMutate:  req.CanMutate,
		ImportHint: req.ImportHint,
	})
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMemberResponse(mem))
}

func (s *Server) leaveRoom(w http.ResponseWriter, r *http.Request) {
	var req LeaveRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	mem, err := s.broker.LeaveRoom(r.Context(), id, req.AgentID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMemberResponse(mem))
}

func (s *Server) roomMembers(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	members, err := s.broker.Rooms().Members(id)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	out := make([]MemberResponse, 0, len(members))
	for _, mem := range members {
		out = append(out, toMemberResponse(mem))
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

func (s *Server) roomMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	limit, ok := parseIntQuery(w, r, "limit", 100)
	if !ok {
		return
	}
	if limit < 0 {
		writeError(w, http.StatusBadRequest, "limit must be >= 0")
		return
	}
	msgs, err := s.broker.ListMessages(id, limit)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	out := make([]MessageRecordResponse, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, toMessageRecordResponse(msg))
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var req SendMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	baselines := s.messageActivityBaselines(req)
	sentAt := time.Now().UTC()
	res, err := s.broker.SendMessage(r.Context(), router.Message{
		RoomID:            req.RoomID,
		Source:            req.Source,
		CorrelationID:     req.CorrelationID,
		ReplyToMessageID:  req.ReplyToMessageID,
		Text:              req.Text,
		SubmitKey:         req.SubmitKey,
		Delivery:          router.DeliveryPolicy(req.Delivery),
		SkipAgents:        req.SkipAgents,
		ReceiptVisibility: receiptVisibility(req.ReceiptVisible),
		TerminalPolicy:    req.TerminalPolicy,
		PolicyTimeout:     time.Duration(req.PolicyTimeoutMS) * time.Millisecond,
		InterruptKey:      req.InterruptKey,
	})
	if err != nil {
		// Distinguish caller input errors (400) from room/delivery errors
		// that are semantically "no target" rather than server faults.
		switch {
		case errors.Is(err, router.ErrRoomIDRequired),
			errors.Is(err, router.ErrEmptyMessage),
			errors.Is(err, router.ErrUnsupportedDelivery):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, router.ErrNoActiveMembers):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, room.ErrRoomNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	trackActivity := req.AwaitActivityMS > 0 || req.AwaitReadyMS > 0
	out := SendMessageResponse{
		MessageID: res.MessageID,
		Receipt:   toMessageReceiptDTO(res.Receipt),
		Delivered: res.Delivered,
		Failed:    res.Failed,
		Members:   make([]MemberResultWireDTO, 0, len(res.Members)),
	}
	for _, mr := range res.Members {
		dto := MemberResultWireDTO{
			AgentID:   mr.AgentID,
			SessionID: mr.SessionID,
			Delivered: mr.Delivered,
		}
		if mr.Err != nil {
			dto.Err = mr.Err.Error()
		}
		if trackActivity && mr.Delivered {
			base := baselines[mr.SessionID]
			dto.Activity = s.awaitMessageActivity(r.Context(), mr.SessionID, base, sentAt, req.AwaitActivityMS, req.AwaitReadyMS)
		}
		out.Members = append(out.Members, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

type messageActivityBaseline struct {
	Seq              uint64
	OutputBytesTotal int64
}

func (s *Server) messageActivityBaselines(req SendMessageRequest) map[string]messageActivityBaseline {
	out := map[string]messageActivityBaseline{}
	members, err := s.broker.Rooms().ActiveMembers(req.RoomID)
	if err != nil {
		return out
	}
	skip := make(map[string]struct{}, len(req.SkipAgents))
	for _, agentID := range req.SkipAgents {
		skip[agentID] = struct{}{}
	}
	for _, mem := range members {
		if !mem.CanMutate {
			continue
		}
		if _, ok := skip[mem.AgentID]; ok {
			continue
		}
		sess, err := s.broker.Sessions().Get(mem.SessionID)
		if err != nil {
			continue
		}
		snap := sess.Snapshot()
		out[mem.SessionID] = messageActivityBaseline{
			Seq:              snap.LastSeq,
			OutputBytesTotal: snap.OutputBytesTotal,
		}
	}
	return out
}

func (s *Server) awaitMessageActivity(ctx context.Context, sessionID string, base messageActivityBaseline, sentAt time.Time, awaitActivityMS, awaitReadyMS int64) *MessageActivityDTO {
	sess, err := s.broker.Sessions().Get(sessionID)
	if err != nil {
		return nil
	}
	if awaitActivityMS <= 0 && awaitReadyMS > 0 {
		awaitActivityMS = awaitReadyMS
	}

	var snap session.Snapshot
	var changed bool
	var firstOutputAt time.Time
	if awaitActivityMS > 0 {
		snap, changed = waitForOutputChange(ctx, sess, base, time.Duration(awaitActivityMS)*time.Millisecond)
		if changed {
			firstOutputAt = time.Now().UTC()
		}
	} else {
		snap = sess.Snapshot()
		changed = snapshotChanged(snap, base)
		if changed {
			firstOutputAt = time.Now().UTC()
		}
	}
	dto := messageActivityDTOFromSnapshot(base, snap)
	dto.OutputChanged = changed
	if !changed && awaitActivityMS > 0 {
		dto.AwaitActivityTimedOut = true
	}
	if !firstOutputAt.IsZero() {
		dto.FirstOutputMS = int64(firstOutputAt.Sub(sentAt) / time.Millisecond)
	}

	ready := sess.ProfileReadiness()
	dto.Ready = ready.Idle
	if awaitReadyMS <= 0 || !changed {
		return &dto
	}
	ready, snap = waitForReady(ctx, sess, time.Duration(awaitReadyMS)*time.Millisecond)
	dto = messageActivityDTOFromSnapshot(base, snap)
	dto.OutputChanged = changed
	dto.FirstOutputMS = int64(firstOutputAt.Sub(sentAt) / time.Millisecond)
	dto.Ready = ready.Idle
	dto.Completed = ready.Idle
	if ready.Idle {
		dto.CompletedMS = int64(time.Since(sentAt) / time.Millisecond)
	} else {
		dto.AwaitReadyTimedOut = true
	}
	return &dto
}

func waitForOutputChange(ctx context.Context, sess *session.Session, base messageActivityBaseline, timeout time.Duration) (session.Snapshot, bool) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		snap := sess.Snapshot()
		if snapshotChanged(snap, base) {
			return snap, true
		}
		if time.Now().After(deadline) {
			return snap, false
		}
		select {
		case <-ctx.Done():
			return snap, false
		case <-ticker.C:
		}
	}
}

func waitForReady(ctx context.Context, sess *session.Session, timeout time.Duration) (session.PromptReadiness, session.Snapshot) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready := sess.ProfileReadiness()
		snap := sess.Snapshot()
		if ready.Idle || time.Now().After(deadline) {
			return ready, snap
		}
		select {
		case <-ctx.Done():
			return ready, snap
		case <-ticker.C:
		}
	}
}

func snapshotChanged(snap session.Snapshot, base messageActivityBaseline) bool {
	return snap.LastSeq > base.Seq || snap.OutputBytesTotal > base.OutputBytesTotal
}

func messageActivityDTOFromSnapshot(base messageActivityBaseline, snap session.Snapshot) MessageActivityDTO {
	seqDelta := uint64(0)
	if snap.LastSeq > base.Seq {
		seqDelta = snap.LastSeq - base.Seq
	}
	bytesDelta := snap.OutputBytesTotal - base.OutputBytesTotal
	if bytesDelta < 0 {
		bytesDelta = 0
	}
	return MessageActivityDTO{
		BaselineSeq:              base.Seq,
		CurrentSeq:               snap.LastSeq,
		SeqDelta:                 seqDelta,
		BaselineOutputBytesTotal: base.OutputBytesTotal,
		OutputBytesTotal:         snap.OutputBytesTotal,
		OutputBytesDelta:         bytesDelta,
		OutputRateBPS:            snap.OutputRateBPS,
	}
}

// --- helpers ---

// writeRoomError maps room + broker sentinels to canonical HTTP statuses so
// clients can disambiguate failure modes without parsing strings.
func writeRoomError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, room.ErrRoomNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, room.ErrMemberNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, broker.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, room.ErrRoomArchived),
		errors.Is(err, room.ErrSessionAlreadyBound),
		errors.Is(err, room.ErrAgentAlreadyInRoom):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, room.ErrSessionRequired),
		errors.Is(err, room.ErrAgentRequired),
		errors.Is(err, room.ErrWorkspaceRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func toRoomResponse(rm room.Room) RoomResponse {
	return RoomResponse{
		ID:          rm.ID,
		Workspace:   rm.Workspace,
		Title:       rm.Title,
		Description: rm.Description,
		CreatedAt:   rm.CreatedAt.UTC(),
		ArchivedAt:  rm.ArchivedAt.UTC(),
	}
}

func toMemberResponse(mem room.Member) MemberResponse {
	return MemberResponse{
		RoomID:     mem.RoomID,
		AgentID:    mem.AgentID,
		SessionID:  mem.SessionID,
		InboxID:    mem.InboxID,
		Role:       string(mem.Role),
		RoleCustom: mem.RoleCustom,
		CanMutate:  mem.CanMutate,
		ImportHint: mem.ImportHint,
		JoinedAt:   mem.JoinedAt.UTC(),
		LeftAt:     mem.LeftAt.UTC(),
	}
}

func receiptVisibility(visible *bool) router.ReceiptVisibility {
	if visible != nil && !*visible {
		return router.ReceiptHidden
	}
	return router.ReceiptVisible
}

func toMessageReceiptDTO(receipt router.MessageReceipt) MessageReceiptDTO {
	return MessageReceiptDTO{
		MessageID:        receipt.MessageID,
		RoomID:           receipt.RoomID,
		Source:           receipt.Source,
		CorrelationID:    receipt.CorrelationID,
		ReplyToMessageID: receipt.ReplyToMessageID,
		ReplyPrefix:      receipt.ReplyPrefix,
	}
}

func toMessageRecordResponse(rec storage.MessageRecord) MessageRecordResponse {
	out := MessageRecordResponse{
		ID:               rec.ID,
		RoomID:           rec.RoomID,
		Source:           rec.Source,
		CorrelationID:    rec.CorrelationID,
		ReplyToMessageID: rec.ReplyToMessageID,
		Text:             rec.Text,
		Delivery:         rec.Delivery,
		ReceiptVisible:   rec.ReceiptVisible,
		SentAt:           rec.SentAt.UTC(),
		Delivered:        rec.Delivered,
		Failed:           rec.Failed,
		Members:          make([]MessageDeliveryResponse, 0, len(rec.Members)),
	}
	for _, mem := range rec.Members {
		out.Members = append(out.Members, MessageDeliveryResponse{
			AgentID:   mem.AgentID,
			SessionID: mem.SessionID,
			Delivered: mem.Delivered,
			Err:       mem.ErrText,
		})
	}
	return out
}
