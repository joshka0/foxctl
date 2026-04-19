package httpjson

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/joshka0/foxctl/internal/atcp/broker"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
	"github.com/joshka0/foxctl/internal/atcp/envelope"
	"github.com/joshka0/foxctl/internal/atcp/kinds"
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
	sessionID, ok := parseSessionTarget(target)
	if !ok {
		writeError(w, http.StatusBadRequest, "only target=session:<id> is supported in this milestone")
		return
	}
	sess, err := s.broker.Sessions().Get(sessionID)
	if err != nil {
		notFoundOrInternal(w, broker.ErrSessionNotFound)
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
	since := parseSinceParam(r)
	replay, ch, sub, cancel := sess.Log().SubscribeFrom(ctx, since)
	defer cancel()

	enc := newSSEEncoder(w, flusher)
	// Drain replay first to guarantee client sees history in order before any
	// live chunks. SubscribeFrom captured both under the log lock so no gap
	// exists between the last replay seq and the first live seq.
	for _, chunk := range replay {
		if err := enc.writeChunk(sessionID, chunk, sub.Dropped()); err != nil {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, open := <-ch:
			if !open {
				return
			}
			if err := enc.writeChunk(sessionID, chunk, sub.Dropped()); err != nil {
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

// TerminalOutputBody is the canonical body for a `terminal.output` ATCP event
// carried over SSE. Exported so clients in Go can decode it from the
// envelope's raw body without redefining the shape.
type TerminalOutputBody struct {
	SessionID string `json:"session_id"`
	BytesB64  string `json:"bytes_b64"`
	Dropped   uint64 `json:"dropped,omitempty"`
}

// writeChunk emits a canonical ATCP envelope framed as an SSE "terminal.output"
// event. The envelope target is `session:<id>`, seq is the session's output
// log seq, and body is TerminalOutputBody. Non-printable PTY bytes survive
// transit via base64 in the body.
func (e *sseEncoder) writeChunk(sessionID string, c session.Chunk, dropped uint64) error {
	body := TerminalOutputBody{
		SessionID: sessionID,
		BytesB64:  base64.StdEncoding.EncodeToString(limitBytes(c.Bytes, eventMaxBytes)),
		Dropped:   dropped,
	}
	env, err := envelope.New(string(kinds.TerminalOutput), "session:"+sessionID, body)
	if err != nil {
		return err
	}
	env.Timestamp = c.Timestamp.UTC()
	env.Seq = c.Seq
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.w, "id: %d\nevent: %s\ndata: %s\n\n", c.Seq, kinds.TerminalOutput, raw); err != nil {
		return err
	}
	e.f.Flush()
	return nil
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
