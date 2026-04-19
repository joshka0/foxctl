// Package httpjson implements the HTTP/1.1 JSON transport for the ATCP
// broker.
//
// The handlers deliberately map the intent surface one-to-one with Broker
// methods so transport concerns stay at the edge: decoding JSON, validating
// required fields, writing status + body. Business logic lives in the broker
// package.
package httpjson

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker"
	"github.com/joshka0/foxctl/internal/atcp/broker/lease"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
	"github.com/joshka0/foxctl/internal/atcp/intents"
)

// Server wraps a Broker with HTTP handlers. It exposes a net/http.Handler
// suitable for attaching to any listener (Unix socket, test server).
type Server struct {
	broker *broker.Broker
}

// NewServer constructs a Server bound to the supplied broker.
func NewServer(b *broker.Broker) *Server {
	return &Server{broker: b}
}

// Handler returns an http.Handler that multiplexes all /v1/* endpoints.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", s.createSession)
	mux.HandleFunc("GET /v1/sessions", s.listSessions)
	mux.HandleFunc("GET /v1/sessions/{id}", s.getSession)
	mux.HandleFunc("DELETE /v1/sessions/{id}", s.deleteSession)
	mux.HandleFunc("POST /v1/terminal/text", s.terminalText)
	mux.HandleFunc("POST /v1/terminal/key", s.terminalKey)
	mux.HandleFunc("POST /v1/terminal/submit", s.terminalSubmit)
	mux.HandleFunc("POST /v1/terminal/paste", s.terminalPaste)
	mux.HandleFunc("POST /v1/terminal/write_bytes", s.terminalWriteBytes)
	mux.HandleFunc("POST /v1/leases/acquire", s.leaseAcquire)
	mux.HandleFunc("POST /v1/leases/release", s.leaseRelease)
	mux.HandleFunc("GET /v1/leases", s.leaseList)
	mux.HandleFunc("GET /v1/events", s.events)
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("POST /v1/rooms", s.createRoom)
	mux.HandleFunc("GET /v1/rooms", s.listRooms)
	mux.HandleFunc("GET /v1/rooms/{id}", s.getRoom)
	mux.HandleFunc("POST /v1/rooms/{id}/join", s.joinRoom)
	mux.HandleFunc("POST /v1/rooms/{id}/leave", s.leaveRoom)
	mux.HandleFunc("GET /v1/rooms/{id}/members", s.roomMembers)
	mux.HandleFunc("POST /v1/messages", s.sendMessage)
	return mux
}

// --- wire types ---

// CreateSessionRequest mirrors session.Spec on the wire, with an optional
// log-options override.
type CreateSessionRequest struct {
	Cmd     []string `json:"cmd"`
	Cwd     string   `json:"cwd,omitempty"`
	Env     []string `json:"env,omitempty"`
	Rows    uint16   `json:"rows,omitempty"`
	Cols    uint16   `json:"cols,omitempty"`
	Adapter string   `json:"adapter,omitempty"`
	Log     *LogOpts `json:"log,omitempty"`
}

// LogOpts is the JSON form of session.OutputLogOptions.
type LogOpts struct {
	MaxChunks int `json:"max_chunks,omitempty"`
	MaxBytes  int `json:"max_bytes,omitempty"`
}

// SessionResponse mirrors session.Snapshot.
type SessionResponse struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"created_at"`
	ExitedAt  time.Time `json:"exited_at,omitempty"`
	ExitCode  int       `json:"exit_code,omitempty"`
	ExitError string    `json:"exit_error,omitempty"`
	LastSeq   uint64    `json:"last_seq"`
	Cmd       []string  `json:"cmd"`
	Cwd       string    `json:"cwd,omitempty"`
	Adapter   string    `json:"adapter,omitempty"`
}

// TerminalResponse is the common reply for terminal.* endpoints.
type TerminalResponse struct {
	SessionID string `json:"session_id"`
	Written   int    `json:"written"`
}

// LeaseAcquireRequest mirrors lease.AcquireRequest with durations expressed in
// milliseconds to keep the wire format trivially JSON-friendly.
type LeaseAcquireRequest struct {
	SessionID string `json:"session_id"`
	Scope     string `json:"scope,omitempty"`
	Owner     string `json:"owner"`
	TTLMS     int    `json:"ttl_ms"`
	Preempt   bool   `json:"preempt,omitempty"`
}

// LeaseReleaseRequest identifies a lease to release.
type LeaseReleaseRequest struct {
	LeaseID string `json:"lease_id"`
}

// LeaseResponse mirrors lease.Lease's public state.
type LeaseResponse struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	Scope      string    `json:"scope"`
	Owner      string    `json:"owner"`
	AcquiredAt time.Time `json:"acquired_at"`
	TTLMS      int       `json:"ttl_ms"`
	Reason     string    `json:"reason,omitempty"`
	ReleasedAt time.Time `json:"released_at,omitempty"`
}

// ErrorResponse is the canonical JSON error body.
type ErrorResponse struct {
	Error string `json:"error"`
}

// --- handlers ---

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Cmd) == 0 {
		writeError(w, http.StatusBadRequest, "cmd is required")
		return
	}
	spec := session.Spec{
		Cmd:     req.Cmd,
		Cwd:     req.Cwd,
		Env:     req.Env,
		Rows:    req.Rows,
		Cols:    req.Cols,
		Adapter: req.Adapter,
	}
	var logOpts session.OutputLogOptions
	if req.Log != nil {
		logOpts = session.OutputLogOptions{MaxChunks: req.Log.MaxChunks, MaxBytes: req.Log.MaxBytes}
	}
	snap, err := s.broker.CreateSession(spec, logOpts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toSessionResponse(snap))
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	snaps := s.broker.ListSessions()
	out := make([]SessionResponse, 0, len(snaps))
	for _, snap := range snaps {
		out = append(out, toSessionResponse(snap))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snap, err := s.broker.GetSession(id)
	if err != nil {
		notFoundOrInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSessionResponse(snap))
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.broker.DeleteSession(id); err != nil {
		notFoundOrInternal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) terminalText(w http.ResponseWriter, r *http.Request) {
	s.runTerminal(w, r, func(id string, intent any) (int, error) {
		t := *(intent.(*intents.TerminalText))
		return s.broker.SubmitText(id, t)
	}, func() any { return &intents.TerminalText{} })
}

func (s *Server) terminalKey(w http.ResponseWriter, r *http.Request) {
	s.runTerminal(w, r, func(id string, intent any) (int, error) {
		t := *(intent.(*intents.TerminalKey))
		return s.broker.SubmitKey(id, t)
	}, func() any { return &intents.TerminalKey{} })
}

func (s *Server) terminalSubmit(w http.ResponseWriter, r *http.Request) {
	s.runTerminal(w, r, func(id string, intent any) (int, error) {
		t := *(intent.(*intents.TerminalSubmit))
		return s.broker.Submit(id, t)
	}, func() any { return &intents.TerminalSubmit{} })
}

func (s *Server) terminalPaste(w http.ResponseWriter, r *http.Request) {
	s.runTerminal(w, r, func(id string, intent any) (int, error) {
		t := *(intent.(*intents.TerminalPaste))
		return s.broker.Paste(id, t)
	}, func() any { return &intents.TerminalPaste{} })
}

func (s *Server) terminalWriteBytes(w http.ResponseWriter, r *http.Request) {
	s.runTerminal(w, r, func(id string, intent any) (int, error) {
		t := *(intent.(*intents.TerminalWriteBytes))
		return s.broker.WriteBytes(id, t)
	}, func() any { return &intents.TerminalWriteBytes{} })
}

// runTerminal is the shared decode + dispatch path for every terminal
// endpoint. It expects a top-level session_id field plus the intent fields.
func (s *Server) runTerminal(w http.ResponseWriter, r *http.Request, dispatch func(string, any) (int, error), make func() any) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	var env struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if env.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	intent := make()
	if err := json.Unmarshal(raw, intent); err != nil {
		writeError(w, http.StatusBadRequest, "invalid intent: "+err.Error())
		return
	}
	n, err := dispatch(env.SessionID, intent)
	if err != nil {
		terminalErrorStatus(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TerminalResponse{SessionID: env.SessionID, Written: n})
}

func (s *Server) leaseAcquire(w http.ResponseWriter, r *http.Request) {
	var req LeaseAcquireRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.TTLMS <= 0 {
		writeError(w, http.StatusBadRequest, "ttl_ms must be > 0")
		return
	}
	scope := lease.Scope(req.Scope)
	if scope == "" {
		scope = lease.ScopeTerminalInput
	}
	l, err := s.broker.AcquireLease(lease.AcquireRequest{
		SessionID: req.SessionID,
		Scope:     scope,
		Owner:     req.Owner,
		TTL:       time.Duration(req.TTLMS) * time.Millisecond,
		Preempt:   req.Preempt,
	})
	if err != nil {
		leaseErrorStatus(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toLeaseResponse(l))
}

func (s *Server) leaseRelease(w http.ResponseWriter, r *http.Request) {
	var req LeaseReleaseRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.broker.ReleaseLease(req.LeaseID); err != nil {
		if errors.Is(err, lease.ErrUnknownLease) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) leaseList(w http.ResponseWriter, r *http.Request) {
	leases := s.broker.Leases().List()
	out := make([]LeaseResponse, 0, len(leases))
	for _, l := range leases {
		out = append(out, toLeaseResponse(l))
	}
	writeJSON(w, http.StatusOK, map[string]any{"leases": out})
}

// --- helpers ---

// decodeJSON decodes a single top-level JSON value from r.Body into dst. It
// rejects unknown fields (DisallowUnknownFields) and trailing tokens after
// the top-level value — bodies like `{...} {...}` or `{...}garbage` fail
// with 400 instead of silently dropping the extra data.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return false
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid json: unexpected trailing data")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

func notFoundOrInternal(w http.ResponseWriter, err error) {
	if errors.Is(err, broker.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func terminalErrorStatus(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, broker.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, broker.ErrLeaseRequired), errors.Is(err, broker.ErrLeaseMismatch):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, broker.ErrIntentInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, session.ErrNotRunning):
		writeError(w, http.StatusGone, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func leaseErrorStatus(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, broker.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, lease.ErrLeaseHeld):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, lease.ErrInvalidSession),
		errors.Is(err, lease.ErrInvalidScope),
		errors.Is(err, lease.ErrInvalidOwner),
		errors.Is(err, lease.ErrInvalidTTL):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func toSessionResponse(snap session.Snapshot) SessionResponse {
	return SessionResponse{
		ID:        snap.ID,
		Status:    snap.Status.String(),
		PID:       snap.PID,
		CreatedAt: snap.CreatedAt,
		ExitedAt:  snap.ExitedAt,
		ExitCode:  snap.ExitCode,
		ExitError: snap.ExitError,
		LastSeq:   snap.LastSeq,
		Cmd:       snap.Spec.Cmd,
		Cwd:       snap.Spec.Cwd,
		Adapter:   snap.Spec.Adapter,
	}
}

func toLeaseResponse(l *lease.Lease) LeaseResponse {
	resp := LeaseResponse{
		ID:         l.ID,
		SessionID:  l.SessionID,
		Scope:      string(l.Scope),
		Owner:      l.Owner,
		AcquiredAt: l.AcquiredAt,
		TTLMS:      int(l.TTL / time.Millisecond),
	}
	if r := l.Reason(); r != lease.ReasonActive {
		resp.Reason = r.String()
		resp.ReleasedAt = l.ReleasedAt()
	}
	return resp
}

// parseSinceParam reads ?since=<uint64> with a default of 0.
func parseSinceParam(r *http.Request) uint64 {
	s := strings.TrimSpace(r.URL.Query().Get("since"))
	if s == "" {
		return 0
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}
