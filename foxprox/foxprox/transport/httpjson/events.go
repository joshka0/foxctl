package httpjson

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/joshka/foxprox/foxprox/broker"
	"github.com/joshka/foxprox/foxprox/broker/session"
	"github.com/joshka/foxprox/foxprox/broker/vtscreen"
	"github.com/joshka/foxprox/foxprox/envelope"
	"github.com/joshka/foxprox/foxprox/kinds"
)

// eventMaxBytes caps the size of any single SSE payload so a single huge
// output chunk cannot wedge a slow reader with an unbounded write.
const eventMaxBytes = 64 * 1024

// events implements `GET /v1/events?target=session:<id>&since=<seq>` as a
// Server-Sent Events stream.
//
// In this milestone the only supported target scheme is session:. Later
// milestones extend the handler to fan in lease/room/reminder events via a
// broker-wide event log.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	if target == "" {
		writeError(w, http.StatusBadRequest, "target query parameter is required")
		return
	}
	if roomID, ok := parseRoomTarget(target); ok {
		s.roomEvents(w, r, roomID)
		return
	}
	sessionID, ok := parseSessionTarget(target)
	if !ok {
		writeError(w, http.StatusBadRequest, "target must be session:<id> or room:<id>")
		return
	}
	s.sessionEvents(w, r, sessionID)
}

func (s *Server) sessionEvents(w http.ResponseWriter, r *http.Request, sessionID string) {
	sess, err := s.broker.Sessions().Get(sessionID)
	if err != nil {
		notFoundOrInternal(w, broker.ErrSessionNotFound)
		return
	}
	opts, ok := parseSessionEventOptions(w, r)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported on this transport")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	replay, ch, sub, cancel := sess.Log().SubscribeFrom(ctx, opts.since)
	defer cancel()

	enc := newSSEEncoder(w, flusher)
	state := newSessionEventState(sessionID, sess, enc, opts)
	if err := state.writeInitial(replay, sub.Dropped()); err != nil {
		return
	}
	readyTick, stopReady := optionalTicker(opts.includeReady, 100*time.Millisecond)
	defer stopReady()
	activityTick, stopActivity := optionalTicker(opts.includeActivity, opts.activityInterval)
	defer stopActivity()

	for {
		select {
		case <-ctx.Done():
			return
		case chunk, open := <-ch:
			if !open {
				return
			}
			if err := state.writeChunk(chunk, sub.Dropped()); err != nil {
				return
			}
		case <-readyTick:
			if err := state.writeReadyIfChanged(sess.Snapshot().LastSeq); err != nil {
				return
			}
		case <-activityTick:
			if err := state.writeActivity(); err != nil {
				return
			}
		}
	}
}

type sessionEventOptions struct {
	since            uint64
	includeScreen    bool
	includeReady     bool
	includeActivity  bool
	activityInterval time.Duration
}

func parseSessionEventOptions(w http.ResponseWriter, r *http.Request) (sessionEventOptions, bool) {
	since, ok := parseSinceParam(w, r)
	if !ok {
		return sessionEventOptions{}, false
	}
	screen, ok := parseBoolQuery(w, r, "screen")
	if !ok {
		return sessionEventOptions{}, false
	}
	ready, ok := parseBoolQuery(w, r, "ready")
	if !ok {
		return sessionEventOptions{}, false
	}
	activity, ok := parseBoolQuery(w, r, "activity")
	if !ok {
		return sessionEventOptions{}, false
	}
	opts := sessionEventOptions{
		since:            since,
		includeScreen:    screen,
		includeReady:     ready,
		includeActivity:  activity,
		activityInterval: time.Second,
	}
	if !opts.includeActivity {
		return opts, true
	}
	intervalMS, ok := parseIntQuery(w, r, "activity_interval_ms", 1000)
	if !ok {
		return opts, false
	}
	if intervalMS <= 0 {
		writeError(w, http.StatusBadRequest, "activity_interval_ms must be > 0")
		return opts, false
	}
	opts.activityInterval = time.Duration(intervalMS) * time.Millisecond
	return opts, true
}

type sessionEventState struct {
	sessionID          string
	sess               *session.Session
	enc                *sseEncoder
	opts               sessionEventOptions
	readyState         bool
	activitySinceSeq   uint64
	activitySinceBytes int64
}

func newSessionEventState(sessionID string, sess *session.Session, enc *sseEncoder, opts sessionEventOptions) *sessionEventState {
	return &sessionEventState{
		sessionID: sessionID,
		sess:      sess,
		enc:       enc,
		opts:      opts,
	}
}

func (st *sessionEventState) writeInitial(replay []session.Chunk, dropped uint64) error {
	// Drain replay first to guarantee client sees history in order before any
	// live chunks. SubscribeFrom captured both under the log lock so no gap
	// exists between the last replay seq and the first live seq.
	for _, chunk := range replay {
		if err := st.enc.writeChunk(st.sessionID, chunk, dropped); err != nil {
			return err
		}
	}
	if st.opts.includeScreen {
		seq := st.sess.Snapshot().LastSeq
		if err := st.enc.writeScreenSnapshot(st.sessionID, seq, st.sess.Screen().Snapshot(), dropped); err != nil {
			return err
		}
	}
	if st.opts.includeReady {
		seq := st.sess.Snapshot().LastSeq
		ready := st.sess.ProfileReadiness()
		st.readyState = ready.Idle
		if err := st.enc.writeTerminalReady(st.sessionID, seq, ready); err != nil {
			return err
		}
	}
	if st.opts.includeActivity {
		snap := st.sess.Snapshot()
		st.activitySinceSeq = snap.LastSeq
		st.activitySinceBytes = snap.OutputBytesTotal
		body := activityResponseFromSnapshot(st.sessionID, snap, st.activitySinceSeq, st.activitySinceBytes, time.Now().UTC())
		if err := st.enc.writeTerminalActivity(st.sessionID, snap.LastSeq, body); err != nil {
			return err
		}
	}
	return nil
}

func (st *sessionEventState) writeChunk(chunk session.Chunk, dropped uint64) error {
	if err := st.enc.writeChunk(st.sessionID, chunk, dropped); err != nil {
		return err
	}
	if st.opts.includeScreen {
		if err := st.enc.writeScreenSnapshot(st.sessionID, chunk.Seq, st.sess.Screen().Snapshot(), dropped); err != nil {
			return err
		}
	}
	if st.opts.includeReady {
		return st.writeReadyIfChanged(chunk.Seq)
	}
	return nil
}

func (st *sessionEventState) writeReadyIfChanged(seq uint64) error {
	ready := st.sess.ProfileReadiness()
	if ready.Idle == st.readyState {
		return nil
	}
	st.readyState = ready.Idle
	return st.enc.writeTerminalReady(st.sessionID, seq, ready)
}

func (st *sessionEventState) writeActivity() error {
	snap := st.sess.Snapshot()
	body := activityResponseFromSnapshot(st.sessionID, snap, st.activitySinceSeq, st.activitySinceBytes, time.Now().UTC())
	if err := st.enc.writeTerminalActivity(st.sessionID, snap.LastSeq, body); err != nil {
		return err
	}
	st.activitySinceSeq = snap.LastSeq
	st.activitySinceBytes = snap.OutputBytesTotal
	return nil
}

func optionalTicker(enabled bool, interval time.Duration) (<-chan time.Time, func()) {
	if !enabled {
		return nil, func() {}
	}
	ticker := time.NewTicker(interval)
	return ticker.C, ticker.Stop
}

type roomEventChunk struct {
	roomID    string
	agentID   string
	sessionID string
	chunk     session.Chunk
	dropped   uint64
}

func (s *Server) roomEvents(w http.ResponseWriter, r *http.Request, roomID string) {
	if _, err := s.broker.Rooms().GetRoom(roomID); err != nil {
		writeRoomError(w, err)
		return
	}
	since, ok := parseSinceParam(w, r)
	if !ok {
		return
	}
	if since != 0 {
		writeError(w, http.StatusBadRequest, "room event streams do not accept scalar since cursors")
		return
	}
	members, err := s.broker.Rooms().ActiveMembers(roomID)
	if err != nil {
		writeRoomError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported on this transport")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	enc := newSSEEncoder(w, flusher)
	events := make(chan roomEventChunk, 256)
	var cancels []func()
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	for _, member := range members {
		sess, err := s.broker.Sessions().Get(member.SessionID)
		if err != nil {
			continue
		}
		replay, ch, sub, cancel := sess.Log().SubscribeFrom(ctx, since)
		cancels = append(cancels, cancel)
		for _, chunk := range replay {
			if err := enc.writeRoomChunk(roomID, member.AgentID, member.SessionID, chunk, sub.Dropped()); err != nil {
				return
			}
		}
		go func(memberID, sessionID string, ch <-chan session.Chunk, sub *session.Subscription) {
			for {
				select {
				case <-ctx.Done():
					return
				case chunk, open := <-ch:
					if !open {
						return
					}
					ev := roomEventChunk{
						roomID:    roomID,
						agentID:   memberID,
						sessionID: sessionID,
						chunk:     chunk,
						dropped:   sub.Dropped(),
					}
					select {
					case events <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
		}(member.AgentID, member.SessionID, ch, sub)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			if err := enc.writeRoomChunk(ev.roomID, ev.agentID, ev.sessionID, ev.chunk, ev.dropped); err != nil {
				return
			}
		}
	}
}

// sseEncoder writes Server-Sent Events frames with optional id/event/data
// fields. Each chunk is base64-encoded so non-printable PTY bytes survive
// transit without clobbering the SSE field separators.
type sseEncoder struct {
	w http.ResponseWriter
	f http.Flusher
}

func newSSEEncoder(w http.ResponseWriter, f http.Flusher) *sseEncoder {
	return &sseEncoder{w: w, f: f}
}

// TerminalOutputBody is the canonical body for a `terminal.output` Foxprox event
// carried over SSE. Exported so clients in Go can decode it from the
// envelope's raw body without redefining the shape.
type TerminalOutputBody struct {
	SessionID   string `json:"session_id"`
	RoomID      string `json:"room_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	BytesB64    string `json:"bytes_b64"`
	Dropped     uint64 `json:"dropped,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	OriginalLen int    `json:"original_len,omitempty"`
}

// TerminalScreenSnapshotBody is the canonical body for
// `terminal.screen.snapshot` events carried over SSE.
type TerminalScreenSnapshotBody struct {
	SessionID string            `json:"session_id"`
	RoomID    string            `json:"room_id,omitempty"`
	AgentID   string            `json:"agent_id,omitempty"`
	Screen    vtscreen.Snapshot `json:"screen"`
	Dropped   uint64            `json:"dropped,omitempty"`
}

// TerminalReadyBody is the canonical body for `terminal.ready` events.
type TerminalReadyBody struct {
	SessionID     string  `json:"session_id"`
	RoomID        string  `json:"room_id,omitempty"`
	AgentID       string  `json:"agent_id,omitempty"`
	Ready         bool    `json:"ready"`
	Reason        string  `json:"reason"`
	IdleForMS     int64   `json:"idle_for_ms"`
	OutputRateBPS float64 `json:"output_rate_bps"`
	ScreenMatch   bool    `json:"screen_match,omitempty"`
	ScreenRegex   string  `json:"screen_regex,omitempty"`
	ScreenLine    string  `json:"screen_line,omitempty"`
	AltScreen     bool    `json:"alt_screen,omitempty"`
}

// TerminalActivityBody is the canonical body for `terminal.activity` events.
type TerminalActivityBody = ActivityResponse

// writeChunk emits a canonical Foxprox envelope framed as an SSE "terminal.output"
// event. The envelope target is `session:<id>`, seq is the session's output
// log seq, and body is TerminalOutputBody. Non-printable PTY bytes survive
// transit via base64 in the body.
func (e *sseEncoder) writeChunk(sessionID string, c session.Chunk, dropped uint64) error {
	body := TerminalOutputBody{
		SessionID:   sessionID,
		BytesB64:    base64.StdEncoding.EncodeToString(limitBytes(c.Bytes, eventMaxBytes)),
		Dropped:     dropped,
		Truncated:   len(c.Bytes) > eventMaxBytes,
		OriginalLen: originalLenIfTruncated(c.Bytes, eventMaxBytes),
	}
	return e.writeEnvelope(kinds.TerminalOutput, "session:"+sessionID, c.Seq, c.Timestamp.UTC(), body)
}

func (e *sseEncoder) writeRoomChunk(roomID, agentID, sessionID string, c session.Chunk, dropped uint64) error {
	body := TerminalOutputBody{
		SessionID:   sessionID,
		RoomID:      roomID,
		AgentID:     agentID,
		BytesB64:    base64.StdEncoding.EncodeToString(limitBytes(c.Bytes, eventMaxBytes)),
		Dropped:     dropped,
		Truncated:   len(c.Bytes) > eventMaxBytes,
		OriginalLen: originalLenIfTruncated(c.Bytes, eventMaxBytes),
	}
	return e.writeEnvelope(kinds.TerminalOutput, "room:"+roomID, c.Seq, c.Timestamp.UTC(), body)
}

func originalLenIfTruncated(b []byte, n int) int {
	if len(b) <= n {
		return 0
	}
	return len(b)
}

func (e *sseEncoder) writeEnvelope(kind kinds.Kind, target string, seq uint64, ts time.Time, body any) error {
	env, err := envelope.New(string(kind), target, body)
	if err != nil {
		return err
	}
	if !ts.IsZero() {
		env.Timestamp = ts.UTC()
	}
	env.Seq = seq
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.w, "id: %d\nevent: %s\ndata: %s\n\n", seq, kind, raw); err != nil {
		return err
	}
	e.f.Flush()
	return nil
}

func (e *sseEncoder) writeScreenSnapshot(sessionID string, seq uint64, snap vtscreen.Snapshot, dropped uint64) error {
	body := TerminalScreenSnapshotBody{
		SessionID: sessionID,
		Screen:    snap,
		Dropped:   dropped,
	}
	env, err := envelope.New(string(kinds.TerminalScreenSnapshot), "session:"+sessionID, body)
	if err != nil {
		return err
	}
	env.Seq = seq
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.w, "id: %d\nevent: %s\ndata: %s\n\n", seq, kinds.TerminalScreenSnapshot, raw); err != nil {
		return err
	}
	e.f.Flush()
	return nil
}

func (e *sseEncoder) writeTerminalReady(sessionID string, seq uint64, ready session.PromptReadiness) error {
	body := TerminalReadyBody{
		SessionID:     sessionID,
		Ready:         ready.Idle,
		Reason:        terminalReadyReason(ready),
		IdleForMS:     int64(ready.IdleFor / time.Millisecond),
		OutputRateBPS: ready.OutputStats.RateBPS,
		ScreenMatch:   ready.ScreenMatch,
		ScreenRegex:   ready.ScreenRegex,
		ScreenLine:    ready.ScreenLine,
		AltScreen:     ready.AltScreen,
	}
	env, err := envelope.New(string(kinds.TerminalReady), "session:"+sessionID, body)
	if err != nil {
		return err
	}
	env.Seq = seq
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.w, "id: %d\nevent: %s\ndata: %s\n\n", seq, kinds.TerminalReady, raw); err != nil {
		return err
	}
	e.f.Flush()
	return nil
}

func (e *sseEncoder) writeTerminalActivity(sessionID string, seq uint64, body TerminalActivityBody) error {
	env, err := envelope.New(string(kinds.TerminalActivity), "session:"+sessionID, body)
	if err != nil {
		return err
	}
	env.Seq = seq
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.w, "id: %d\nevent: %s\ndata: %s\n\n", seq, kinds.TerminalActivity, raw); err != nil {
		return err
	}
	e.f.Flush()
	return nil
}

func terminalReadyReason(ready session.PromptReadiness) string {
	if ready.ScreenRegex != "" && !ready.ScreenMatch {
		return "screen_regex_no_match"
	}
	if ready.AltScreen {
		return "alt_screen"
	}
	if !ready.Idle {
		return "output_busy"
	}
	return "ready"
}

// limitBytes returns a copy truncated to n bytes. Used to bound a single SSE
// frame without stalling the subscriber.
func limitBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	out := make([]byte, n)
	copy(out, b)
	return out
}

// parseSessionTarget accepts a literal "session:<id>" string and returns the
// id. It is intentionally minimal — the addressing package is the canonical
// parser once the event bus gains more schemes.
func parseSessionTarget(s string) (string, bool) {
	const prefix = "session:"
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	id := strings.TrimSpace(s[len(prefix):])
	if id == "" {
		return "", false
	}
	return id, true
}

func parseRoomTarget(s string) (string, bool) {
	const prefix = "room:"
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	id := strings.TrimSpace(s[len(prefix):])
	if id == "" {
		return "", false
	}
	return id, true
}

func parseBoolQuery(w http.ResponseWriter, r *http.Request, name string) (bool, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return false, true
	}
	ok, err := strconv.ParseBool(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, name+" must be a boolean")
		return false, false
	}
	return ok, true
}
