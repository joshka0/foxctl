package httpjson

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker"
)

func newTestServer(t *testing.T) (*httptest.Server, *broker.Broker) {
	t.Helper()
	b := broker.New(broker.Options{})
	s := NewServer(b)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() {
		ts.Close()
		b.Stop()
	})
	return ts, b
}

func postJSON(t *testing.T, ts *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(ts.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func decodeResponse[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = resp.Body.Close()
	return out
}

func TestHealth(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestCreateSession_201(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(body))
	}
	got := decodeResponse[SessionResponse](t, resp)
	if got.ID == "" {
		t.Error("ID empty")
	}
	if got.Status != "running" {
		t.Errorf("Status = %q, want running", got.Status)
	}
}

func TestCreateSession_400WhenCmdMissing(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestGetSession_404(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/sessions/nope")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestListSessions(t *testing.T) {
	ts, _ := newTestServer(t)
	_ = postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	_ = postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	resp, err := http.Get(ts.URL + "/v1/sessions")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body := decodeResponse[struct {
		Sessions []SessionResponse `json:"sessions"`
	}](t, resp)
	if len(body.Sessions) != 2 {
		t.Fatalf("len = %d, want 2", len(body.Sessions))
	}
}

func TestDeleteSession_204(t *testing.T) {
	ts, _ := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	sess := decodeResponse[SessionResponse](t, create)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/sessions/"+sess.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestTerminalSubmit_WritesToPTY(t *testing.T) {
	ts, b := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"cat"}})
	sess := decodeResponse[SessionResponse](t, create)

	submitBody := map[string]any{
		"session_id": sess.ID,
		"text":       "hello",
	}
	resp := postJSON(t, ts, "/v1/terminal/submit", submitBody)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	got := decodeResponse[TerminalResponse](t, resp)
	if got.Written == 0 {
		t.Error("Written = 0")
	}

	liveSess, _ := b.Sessions().Get(sess.ID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var buf bytes.Buffer
		for _, c := range liveSess.Log().Since(0, 0) {
			buf.Write(c.Bytes)
		}
		if strings.Contains(buf.String(), "hello") {
			_, _ = liveSess.Write([]byte{0x04})
			<-liveSess.Done()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	liveSess.Close()
	<-liveSess.Done()
	t.Fatal("submitted text never appeared in output")
}

func TestTerminalSubmit_BadSession(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := postJSON(t, ts, "/v1/terminal/submit", map[string]any{"session_id": "nope", "text": "x"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestTerminalText_EmptyReturns400(t *testing.T) {
	ts, _ := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	sess := decodeResponse[SessionResponse](t, create)

	resp := postJSON(t, ts, "/v1/terminal/text", map[string]any{"session_id": sess.ID, "text": ""})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestLeaseAcquireAndRelease(t *testing.T) {
	ts, _ := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	sess := decodeResponse[SessionResponse](t, create)

	acq := postJSON(t, ts, "/v1/leases/acquire", LeaseAcquireRequest{
		SessionID: sess.ID, Owner: "test", TTLMS: 2000,
	})
	if acq.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(acq.Body)
		t.Fatalf("acquire status = %d body = %s", acq.StatusCode, raw)
	}
	l := decodeResponse[LeaseResponse](t, acq)
	if l.ID == "" || l.Scope != "terminal.input" {
		t.Errorf("unexpected lease payload: %+v", l)
	}

	// Second acquire without preempt => 409.
	conflict := postJSON(t, ts, "/v1/leases/acquire", LeaseAcquireRequest{
		SessionID: sess.ID, Owner: "other", TTLMS: 2000,
	})
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("second acquire status = %d, want 409", conflict.StatusCode)
	}
	_ = conflict.Body.Close()

	// Release.
	rel := postJSON(t, ts, "/v1/leases/release", LeaseReleaseRequest{LeaseID: l.ID})
	if rel.StatusCode != http.StatusNoContent {
		t.Fatalf("release status = %d", rel.StatusCode)
	}

	// Release unknown => 404.
	rel2 := postJSON(t, ts, "/v1/leases/release", LeaseReleaseRequest{LeaseID: "nope"})
	if rel2.StatusCode != http.StatusNotFound {
		t.Fatalf("release unknown status = %d, want 404", rel2.StatusCode)
	}
}

func TestLeaseAcquire_InvalidTTL(t *testing.T) {
	ts, _ := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	sess := decodeResponse[SessionResponse](t, create)
	resp := postJSON(t, ts, "/v1/leases/acquire", LeaseAcquireRequest{
		SessionID: sess.ID, Owner: "test", TTLMS: 0,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTerminalSubmit_LeaseMismatchReturns409(t *testing.T) {
	ts, _ := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	sess := decodeResponse[SessionResponse](t, create)

	_ = postJSON(t, ts, "/v1/leases/acquire", LeaseAcquireRequest{
		SessionID: sess.ID, Owner: "test", TTLMS: 5000,
	})
	// Try to submit without the lease id.
	resp := postJSON(t, ts, "/v1/terminal/submit", map[string]any{
		"session_id": sess.ID, "text": "x",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}
