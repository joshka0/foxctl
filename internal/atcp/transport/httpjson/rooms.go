package httpjson

import (
	"errors"
	"net/http"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker"
	"github.com/joshka0/foxctl/internal/atcp/broker/room"
	"github.com/joshka0/foxctl/internal/atcp/broker/router"
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
	RoomID     string   `json:"room_id"`
	Source     string   `json:"source,omitempty"`
	Text       string   `json:"text"`
	Delivery   string   `json:"delivery,omitempty"`
	SkipAgents []string `json:"skip_agents,omitempty"`
}

// SendMessageResponse is the aggregate outcome of a fan-out.
type SendMessageResponse struct {
	MessageID string                `json:"message_id"`
	Delivered int                   `json:"delivered"`
	Failed    int                   `json:"failed"`
	Members   []MemberResultWireDTO `json:"members"`
}

// MemberResultWireDTO mirrors router.MemberResult on the wire. The Err field
// is stringified so clients see a stable JSON shape regardless of the
// underlying error type.
type MemberResultWireDTO struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Delivered bool   `json:"delivered"`
	Err       string `json:"error,omitempty"`
}

// --- handlers ---

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	var req CreateRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rm, err := s.broker.CreateRoom(room.CreateRoomRequest{
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
	mem, err := s.broker.JoinRoom(room.JoinRequest{
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
	mem, err := s.broker.LeaveRoom(id, req.AgentID)
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

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var req SendMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := s.broker.SendMessage(router.Message{
		RoomID:     req.RoomID,
		Source:     req.Source,
		Text:       req.Text,
		Delivery:   router.DeliveryPolicy(req.Delivery),
		SkipAgents: req.SkipAgents,
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
	out := SendMessageResponse{
		MessageID: res.MessageID,
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
		out.Members = append(out.Members, dto)
	}
	writeJSON(w, http.StatusOK, out)
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
