package httpjson

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/envelope"
	"github.com/joshka0/foxctl/internal/atcp/kinds"
)

// TestEventsSSE_StreamsSessionOutput creates a session, submits text, then
// asserts a terminal.output SSE frame arrives with the typed bytes.
func TestEventsSSE_StreamsSessionOutput(t *testing.T) {
	ts, _ := newTestServer(t)

	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"cat"}})
	sess := decodeResponse[SessionResponse](t, create)
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/sessions/"+sess.ID, nil)
		resp, _ := http.DefaultClient.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events?target=session:"+sess.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body = %s", resp.StatusCode, raw)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}

	// Submit text in another goroutine so the SSE read below picks it up.
	submitDone := make(chan struct{})
	go func() {
		defer close(submitDone)
		time.Sleep(50 * time.Millisecond)
		_ = postJSON(t, ts, "/v1/terminal/submit", map[string]any{
			"session_id": sess.ID, "text": "pingz",
		})
	}()

	wantSub := "pingz"
	data, ok := readSSEDataUntil(t, resp.Body, wantSub, 3*time.Second)
	if !ok {
		t.Fatalf("SSE stream did not contain %q within deadline", wantSub)
	}
	if !strings.Contains(data, wantSub) {
		t.Fatalf("decoded data = %q, want to contain %q", data, wantSub)
	}
	<-submitDone
}

func TestEventsSSE_RejectsUnsupportedTarget(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/events?target=room:x")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestEventsSSE_RejectsMissingTarget(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestEventsSSE_UnknownSession(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/events?target=session:nope")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// readSSEDataUntil scans an SSE stream for an ATCP envelope whose decoded
// body.bytes_b64 contains the substring want. It also validates the envelope
// on the wire so a regression to ad-hoc frames fails this test.
func readSSEDataUntil(t *testing.T, r io.Reader, want string, timeout time.Duration) (string, bool) {
	t.Helper()
	done := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			env, err := envelope.DecodeStrict([]byte(payload))
			if err != nil {
				t.Errorf("expected canonical ATCP envelope on SSE, got invalid frame: %v (payload=%s)", err, payload)
				continue
			}
			if env.Kind != string(kinds.TerminalOutput) {
				continue
			}
			var body TerminalOutputBody
			if err := env.DecodeBody(&body); err != nil {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(body.BytesB64)
			if err != nil {
				continue
			}
			s := string(raw)
			if strings.Contains(s, want) {
				done <- result{s: s, ok: true}
				return
			}
		}
		done <- result{}
	}()
	select {
	case r := <-done:
		return r.s, r.ok
	case <-time.After(timeout):
		return "", false
	}
}

type result struct {
	s  string
	ok bool
}
