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

	"github.com/joshka/foxprox/foxprox/envelope"
	"github.com/joshka/foxprox/foxprox/kinds"
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

func TestEventsSSE_StreamsScreenSnapshotsWhenRequested(t *testing.T) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events?target=session:"+sess.ID+"&screen=true", nil)
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

	submitDone := make(chan struct{})
	go func() {
		defer close(submitDone)
		time.Sleep(50 * time.Millisecond)
		_ = postJSON(t, ts, "/v1/terminal/submit", map[string]any{
			"session_id": sess.ID, "text": "screen_ping",
		})
	}()

	data, ok := readScreenSSEDataUntil(t, resp.Body, "screen_ping", 3*time.Second)
	if !ok {
		t.Fatal("SSE stream did not contain terminal.screen.snapshot with screen_ping within deadline")
	}
	if !strings.Contains(data, "screen_ping") {
		t.Fatalf("screen data = %q, want screen_ping", data)
	}
	<-submitDone
}

func TestEventsSSE_StreamsTerminalReadyWhenRequested(t *testing.T) {
	ts, _ := newTestServer(t)

	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{
		Cmd:       []string{"sh", "-c", "printf READY; sleep 30"},
		Readiness: &ReadinessProfileDTO{ScreenRegex: "READY", DebounceMS: 1},
	})
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events?target=session:"+sess.ID+"&ready=true", nil)
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

	data, ok := readReadySSEDataUntil(t, resp.Body, 3*time.Second)
	if !ok {
		t.Fatal("SSE stream did not contain ready terminal.ready within deadline")
	}
	if !strings.Contains(data, `"ready":true`) {
		t.Fatalf("ready data = %q, want ready true", data)
	}
}

func TestEventsSSE_StreamsTerminalActivityHeartbeats(t *testing.T) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events?target=session:"+sess.ID+"&activity=true&activity_interval_ms=50", nil)
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

	submitDone := make(chan struct{})
	go func() {
		defer close(submitDone)
		time.Sleep(100 * time.Millisecond)
		_ = postJSON(t, ts, "/v1/terminal/submit", map[string]any{
			"session_id": sess.ID, "text": "activity_sse",
		})
	}()

	data, ok := readActivitySSEDataUntil(t, resp.Body, 3*time.Second)
	if !ok {
		t.Fatal("SSE stream did not contain terminal.activity output change within deadline")
	}
	if !strings.Contains(data, `"output_changed":true`) {
		t.Fatalf("activity data = %q, want output_changed true", data)
	}
	<-submitDone
}

func TestEventsSSE_StreamsRoomOutput(t *testing.T) {
	ts, b := newTestServer(t)
	sessionID := createCatSession(t, ts, b)
	var room RoomResponse
	postAndDecode(t, ts, "/v1/rooms", CreateRoomRequest{Workspace: "ws"}, http.StatusCreated, &room)
	postAndDecode(t, ts, "/v1/rooms/"+room.ID+"/join", JoinRoomRequest{
		AgentID: "cat-agent", SessionID: sessionID, CanMutate: true,
	}, http.StatusCreated, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events?target=room:"+room.ID, nil)
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

	go func() {
		_ = postJSON(t, ts, "/v1/terminal/submit", map[string]any{
			"session_id": sessionID, "text": "room_sse",
		})
	}()

	data, ok := readRoomSSEDataUntil(t, resp.Body, room.ID, "cat-agent", "room_sse", 3*time.Second)
	if !ok {
		t.Fatal("room SSE stream did not contain room_sse within deadline")
	}
	if !strings.Contains(data, `"target":"room:`+room.ID+`"`) {
		t.Fatalf("room data = %q, want room target", data)
	}
}

func TestEventsSSE_RejectsUnsupportedTarget(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/events?target=agent:x")
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

// readSSEDataUntil scans an SSE stream for an Foxprox envelope whose decoded
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
				t.Errorf("expected canonical Foxprox envelope on SSE, got invalid frame: %v (payload=%s)", err, payload)
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

func readScreenSSEDataUntil(t *testing.T, r io.Reader, want string, timeout time.Duration) (string, bool) {
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
				t.Errorf("expected canonical Foxprox envelope on SSE, got invalid frame: %v (payload=%s)", err, payload)
				continue
			}
			if env.Kind != string(kinds.TerminalScreenSnapshot) {
				continue
			}
			var body TerminalScreenSnapshotBody
			if err := env.DecodeBody(&body); err != nil {
				t.Errorf("decode screen body: %v", err)
				continue
			}
			for _, line := range body.Screen.Lines {
				if strings.Contains(line, want) {
					done <- result{s: payload, ok: true}
					return
				}
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

func readReadySSEDataUntil(t *testing.T, r io.Reader, timeout time.Duration) (string, bool) {
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
				t.Errorf("expected canonical Foxprox envelope on SSE, got invalid frame: %v (payload=%s)", err, payload)
				continue
			}
			if env.Kind != string(kinds.TerminalReady) {
				continue
			}
			var body TerminalReadyBody
			if err := env.DecodeBody(&body); err != nil {
				t.Errorf("decode ready body: %v", err)
				continue
			}
			if body.Ready {
				done <- result{s: payload, ok: true}
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

func readActivitySSEDataUntil(t *testing.T, r io.Reader, timeout time.Duration) (string, bool) {
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
				t.Errorf("expected canonical Foxprox envelope on SSE, got invalid frame: %v (payload=%s)", err, payload)
				continue
			}
			if env.Kind != string(kinds.TerminalActivity) {
				continue
			}
			var body TerminalActivityBody
			if err := env.DecodeBody(&body); err != nil {
				t.Errorf("decode activity body: %v", err)
				continue
			}
			if body.OutputChanged {
				done <- result{s: payload, ok: true}
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

func readRoomSSEDataUntil(t *testing.T, r io.Reader, roomID, agentID, want string, timeout time.Duration) (string, bool) {
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
				t.Errorf("expected canonical Foxprox envelope on SSE, got invalid frame: %v (payload=%s)", err, payload)
				continue
			}
			if env.Kind != string(kinds.TerminalOutput) || env.Target != "room:"+roomID {
				continue
			}
			var body TerminalOutputBody
			if err := env.DecodeBody(&body); err != nil {
				t.Errorf("decode output body: %v", err)
				continue
			}
			if body.RoomID != roomID || body.AgentID != agentID {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(body.BytesB64)
			if err != nil {
				continue
			}
			if strings.Contains(string(raw), want) {
				done <- result{s: payload, ok: true}
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
