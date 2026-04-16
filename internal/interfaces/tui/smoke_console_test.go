package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunConsoleSmokeSuccess(t *testing.T) {
	t.Parallel()

	const sessionID = "sess-smoke"

	var (
		mu         sync.Mutex
		hits       = map[string]int{}
		askBody    AskConsoleSessionRequest
		cancelBody CancelConsoleSessionRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		mu.Lock()
		hits[key]++
		mu.Unlock()

		switch key {
		case "GET /api/agents":
			if got := r.URL.Query().Get("limit"); got != "7" {
				t.Fatalf("agents limit query = %q, want %q", got, "7")
			}
			_, _ = w.Write([]byte(`{
				"agents": [{"id":"agent-1","name":"Worker One","role":"coder","state":"idle","workspace_root":"/tmp/ws"}],
				"total": 1
			}`))
		case "GET /api/console/sessions/" + sessionID:
			_, _ = w.Write([]byte(`{
				"session": {"id":"` + sessionID + `","workspace":"/tmp/ws","profile":"explorer","message_count":1},
				"messages": [{"role":"assistant","content":"seed","timestamp":1}],
				"inflight": null
			}`))
		case "GET /api/console/sessions/" + sessionID + "/events":
			if got := r.URL.Query().Get("format"); got != "payload" {
				t.Fatalf("events format query = %q, want %q", got, "payload")
			}
			if got := r.Header.Get("Accept"); got != "text/event-stream" {
				t.Fatalf("events accept = %q, want %q", got, "text/event-stream")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"type":"reply","correlation_id":"corr-1","content":"done"}` + "\n\n"))
		case "POST /api/console/sessions/" + sessionID + "/ask":
			var req AskConsoleSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode ask body: %v", err)
			}
			mu.Lock()
			askBody = req
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write([]byte(`{"ok":true,"correlation_id":"corr-1","message":"request queued"}`))
		case "POST /api/console/sessions/" + sessionID + "/cancel":
			var req CancelConsoleSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode cancel body: %v", err)
			}
			mu.Lock()
			cancelBody = req
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true,"message":"cancel requested"}`))
		default:
			t.Fatalf("unexpected route: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	summary, err := RunConsoleSmoke(context.Background(), SmokeConsoleOptions{
		Options: Options{
			APIBaseURL:       server.URL,
			ConsoleSessionID: sessionID,
			AgentLimit:       7,
		},
		Ask:     "   ping smoke   ",
		Cancel:  true,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunConsoleSmoke error: %v", err)
	}

	if summary.InitialTranscriptRows != 1 {
		t.Fatalf("InitialTranscriptRows = %d, want 1", summary.InitialTranscriptRows)
	}
	if summary.StreamEventsObserved != 1 {
		t.Fatalf("StreamEventsObserved = %d, want 1", summary.StreamEventsObserved)
	}
	if summary.StreamStatus != smokeStatusDone {
		t.Fatalf("StreamStatus = %q, want %q", summary.StreamStatus, smokeStatusDone)
	}
	if summary.AskAccepted != 1 || summary.AskErrors != 0 || summary.AskStatus != smokeStatusAccepted {
		t.Fatalf("ask summary = %+v, want accepted=1 errors=0 status=%q", summary, smokeStatusAccepted)
	}
	if summary.CancelAccepted != 1 || summary.CancelErrors != 0 || summary.CancelStatus != smokeStatusAccepted {
		t.Fatalf("cancel summary = %+v, want accepted=1 errors=0 status=%q", summary, smokeStatusAccepted)
	}
	if summary.TimedOut {
		t.Fatal("TimedOut = true, want false")
	}

	wantSummary := "smoke_console initial_transcript_rows=1 stream_events=1 stream_errors=0 stream_status=done ask_accepted=1 ask_errors=0 ask_status=accepted cancel_accepted=1 cancel_errors=0 cancel_status=accepted timed_out=false"
	if got := summary.String(); got != wantSummary {
		t.Fatalf("summary.String() = %q, want %q", got, wantSummary)
	}

	mu.Lock()
	if askBody.Content != "ping smoke" {
		mu.Unlock()
		t.Fatalf("ask content = %q, want %q", askBody.Content, "ping smoke")
	}
	if cancelBody.CorrelationID != "corr-1" {
		mu.Unlock()
		t.Fatalf("cancel correlation_id = %q, want %q", cancelBody.CorrelationID, "corr-1")
	}
	defer mu.Unlock()
	for _, route := range []string{
		"GET /api/agents",
		"GET /api/console/sessions/" + sessionID,
		"GET /api/console/sessions/" + sessionID + "/events",
		"POST /api/console/sessions/" + sessionID + "/ask",
		"POST /api/console/sessions/" + sessionID + "/cancel",
	} {
		if hits[route] != 1 {
			t.Fatalf("hits[%q] = %d, want 1", route, hits[route])
		}
	}
}

func TestRunConsoleSmokeRequiresAPIBaseURL(t *testing.T) {
	t.Parallel()

	_, err := RunConsoleSmoke(context.Background(), SmokeConsoleOptions{
		Options: Options{
			ConsoleSessionID: "sess-1",
		},
	})
	if err == nil {
		t.Fatal("RunConsoleSmoke error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--smoke-console requires --api-base-url") {
		t.Fatalf("error = %v, want missing --api-base-url detail", err)
	}
}

func TestRunConsoleSmokeRequiresConsoleSessionID(t *testing.T) {
	t.Parallel()

	_, err := RunConsoleSmoke(context.Background(), SmokeConsoleOptions{
		Options: Options{
			APIBaseURL: "http://127.0.0.1:7777",
		},
	})
	if err == nil {
		t.Fatal("RunConsoleSmoke error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--smoke-console requires --console-session-id") {
		t.Fatalf("error = %v, want missing --console-session-id detail", err)
	}
}

func TestRunConsoleSmokeTimeoutWithNoEvents(t *testing.T) {
	t.Parallel()

	const sessionID = "sess-timeout"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/agents":
			_, _ = w.Write([]byte(`{"agents":[],"total":0}`))
		case "GET /api/console/sessions/" + sessionID:
			_, _ = w.Write([]byte(`{
				"session":{"id":"` + sessionID + `","workspace":"/tmp/ws","profile":"explorer","message_count":0},
				"messages":[],
				"inflight":null
			}`))
		case "GET /api/console/sessions/" + sessionID + "/events":
			if got := r.URL.Query().Get("format"); got != "payload" {
				t.Fatalf("events format query = %q, want %q", got, "payload")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("response writer does not implement http.Flusher")
			}
			flusher.Flush()
			<-r.Context().Done()
		default:
			t.Fatalf("unexpected route: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	summary, err := RunConsoleSmoke(context.Background(), SmokeConsoleOptions{
		Options: Options{
			APIBaseURL:       server.URL,
			ConsoleSessionID: sessionID,
		},
		Timeout: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("RunConsoleSmoke error: %v", err)
	}

	if !summary.TimedOut {
		t.Fatal("TimedOut = false, want true")
	}
	if summary.StreamStatus != smokeStatusTimeout {
		t.Fatalf("StreamStatus = %q, want %q", summary.StreamStatus, smokeStatusTimeout)
	}
	if summary.StreamEventsObserved != 0 {
		t.Fatalf("StreamEventsObserved = %d, want 0", summary.StreamEventsObserved)
	}
	if summary.AskStatus != smokeStatusNotRequested {
		t.Fatalf("AskStatus = %q, want %q", summary.AskStatus, smokeStatusNotRequested)
	}
	if summary.CancelStatus != smokeStatusNotRequested {
		t.Fatalf("CancelStatus = %q, want %q", summary.CancelStatus, smokeStatusNotRequested)
	}

	wantSummary := "smoke_console initial_transcript_rows=1 stream_events=0 stream_errors=0 stream_status=timeout ask_accepted=0 ask_errors=0 ask_status=not_requested cancel_accepted=0 cancel_errors=0 cancel_status=not_requested timed_out=true"
	if got := summary.String(); got != wantSummary {
		t.Fatalf("summary.String() = %q, want %q", got, wantSummary)
	}
}
