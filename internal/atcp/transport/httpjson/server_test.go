package httpjson

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
)

func newTestServer(t *testing.T) (*httptest.Server, *broker.Broker) {
	t.Helper()
	// Tests exercise terminal endpoints without orchestrating a lease every
	// time; AllowUnleasedInputForTests: true matches the production invariant
	// for the happy paths. Lease-specific tests still hit the real policy.
	b := broker.MustNew(broker.Options{AllowUnleasedInputForTests: true})
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
	resp := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{
		Cmd:            []string{"sleep", "30"},
		SubmitKey:      "LineFeed",
		EnableRawBytes: true,
		Readiness:      &ReadinessProfileDTO{ScreenRegex: "PROMPT>$", ThresholdBPS: 12, DebounceMS: 34},
	})
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
	if got.SubmitKey != "LineFeed" {
		t.Errorf("SubmitKey = %q, want LineFeed", got.SubmitKey)
	}
	if !got.EnableRawBytes {
		t.Error("EnableRawBytes = false, want true")
	}
	if got.Readiness.ScreenRegex != "PROMPT>$" || got.Readiness.ThresholdBPS != 12 || got.Readiness.DebounceMS != 34 {
		t.Errorf("Readiness = %+v", got.Readiness)
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

func TestGetSessionReadiness(t *testing.T) {
	ts, _ := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	sess := decodeResponse[SessionResponse](t, create)

	resp, err := http.Get(ts.URL + "/v1/sessions/" + sess.ID + "/readiness?debounce_ms=0")
	if err != nil {
		t.Fatalf("get readiness: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	got := decodeResponse[ReadinessResponse](t, resp)
	if got.SessionID != sess.ID {
		t.Fatalf("SessionID = %q, want %q", got.SessionID, sess.ID)
	}
	if !got.Idle {
		t.Fatalf("Idle = false, want true: %+v", got)
	}
	if got.OutputRateBPS < 0 {
		t.Fatalf("OutputRateBPS = %f, want non-negative", got.OutputRateBPS)
	}
}

func TestGetSessionActivityReportsCursorDelta(t *testing.T) {
	ts, b := newTestServer(t)
	sessionID := createCatSession(t, ts, b)

	resp, err := http.Get(ts.URL + "/v1/sessions/" + sessionID + "/activity")
	if err != nil {
		t.Fatalf("get activity: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	base := decodeResponse[ActivityResponse](t, resp)
	if base.Working || base.OutputChanged {
		t.Fatalf("baseline activity = %+v, want no change", base)
	}

	_ = postJSON(t, ts, "/v1/terminal/submit", map[string]any{
		"session_id": sessionID,
		"text":       "activity_ping",
	})
	sess, _ := b.Sessions().Get(sessionID)
	assertSessionSeesText(t, sess, "activity_ping", 2*time.Second)

	resp, err = http.Get(ts.URL + "/v1/sessions/" + sessionID + "/activity?since_seq=" +
		strconv.FormatUint(base.CurrentSeq, 10) + "&since_output_bytes_total=" +
		strconv.FormatInt(base.OutputBytesTotal, 10))
	if err != nil {
		t.Fatalf("get activity delta: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	got := decodeResponse[ActivityResponse](t, resp)
	if !got.Working || !got.OutputChanged || got.OutputBytesDelta <= 0 || got.SeqDelta == 0 {
		t.Fatalf("activity delta = %+v, want output change", got)
	}
}

func TestGetSessionReadiness_WithScreenRegex(t *testing.T) {
	ts, b := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sh", "-c", "printf 'PROMPT>'; sleep 30"}})
	sess := decodeResponse[SessionResponse](t, create)
	liveSess, err := b.Sessions().Get(sess.ID)
	if err != nil {
		t.Fatalf("session lookup: %v", err)
	}
	assertHTTPOutputContains(t, liveSess, "PROMPT>", 2*time.Second)

	resp, err := http.Get(ts.URL + "/v1/sessions/" + sess.ID + "/readiness?debounce_ms=0&screen_regex=PROMPT%3E")
	if err != nil {
		t.Fatalf("get readiness: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	got := decodeResponse[ReadinessResponse](t, resp)
	if !got.Idle || !got.ScreenMatch || got.ScreenRegex != "PROMPT>" || !strings.Contains(got.ScreenLine, "PROMPT>") {
		t.Fatalf("readiness = %+v, want idle screen match", got)
	}

	resp, err = http.Get(ts.URL + "/v1/sessions/" + sess.ID + "/readiness?debounce_ms=0&screen_regex=NOPE")
	if err != nil {
		t.Fatalf("get readiness no-match: %v", err)
	}
	got = decodeResponse[ReadinessResponse](t, resp)
	if got.Idle || got.ScreenMatch {
		t.Fatalf("readiness no-match = %+v, want not idle/no screen match", got)
	}
}

func TestGetSessionReadiness_UsesProfileRegex(t *testing.T) {
	ts, b := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{
		Cmd:       []string{"sh", "-c", "printf 'PROFILE>'; sleep 30"},
		Readiness: &ReadinessProfileDTO{ScreenRegex: "PROFILE>", DebounceMS: 1},
	})
	sess := decodeResponse[SessionResponse](t, create)
	liveSess, err := b.Sessions().Get(sess.ID)
	if err != nil {
		t.Fatalf("session lookup: %v", err)
	}
	assertHTTPOutputContains(t, liveSess, "PROFILE>", 2*time.Second)

	resp, err := http.Get(ts.URL + "/v1/sessions/" + sess.ID + "/readiness?debounce_ms=0")
	if err != nil {
		t.Fatalf("get readiness: %v", err)
	}
	got := decodeResponse[ReadinessResponse](t, resp)
	if !got.Idle || !got.ScreenMatch || got.ScreenRegex != "PROFILE>" {
		t.Fatalf("readiness = %+v, want profile screen match", got)
	}
}

func TestGetSessionReadiness_RejectsInvalidQuery(t *testing.T) {
	ts, _ := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	sess := decodeResponse[SessionResponse](t, create)

	resp, err := http.Get(ts.URL + "/v1/sessions/" + sess.ID + "/readiness?threshold_bps=-1")
	if err != nil {
		t.Fatalf("get readiness: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetSessionScreen(t *testing.T) {
	ts, b := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sh", "-c", "printf screen; sleep 30"}})
	sess := decodeResponse[SessionResponse](t, create)
	t.Cleanup(func() { _ = b.DeleteSession(sess.ID) })

	liveSess, _ := b.Sessions().Get(sess.ID)
	assertHTTPOutputContains(t, liveSess, "screen", 2*time.Second)

	resp, err := http.Get(ts.URL + "/v1/sessions/" + sess.ID + "/screen")
	if err != nil {
		t.Fatalf("get screen: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body = %s", resp.StatusCode, raw)
	}
	got := decodeResponse[struct {
		Lines []string `json:"lines"`
	}](t, resp)
	for _, line := range got.Lines {
		if strings.Contains(line, "screen") {
			return
		}
	}
	t.Fatalf("screen snapshot did not contain typed text: %+v", got.Lines)
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
	t.Cleanup(func() { _ = b.DeleteSession(sess.ID) })

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

// TestCreateSession_RejectsTrailingJSON verifies decodeJSON consumes exactly
// one top-level JSON value, so `{..}{..}` fails with 400 rather than silently
// accepting the first object and discarding the rest.
func TestCreateSession_RejectsTrailingJSON(t *testing.T) {
	ts, _ := newTestServer(t)
	body := strings.NewReader(`{"cmd":["sleep","30"]}{"cmd":["cat"]}`)
	resp, err := http.Post(ts.URL+"/v1/sessions", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d (want 400), body = %s", resp.StatusCode, raw)
	}
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

func TestTerminalWriteBytes_RequiresOptIn(t *testing.T) {
	ts, _ := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	sess := decodeResponse[SessionResponse](t, create)

	resp := postJSON(t, ts, "/v1/terminal/write_bytes", map[string]any{
		"session_id": sess.ID,
		"bytes":      []byte("raw\n"),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTerminalWriteBytes_OptInWritesToPTY(t *testing.T) {
	ts, b := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{
		Cmd:            []string{"cat"},
		EnableRawBytes: true,
	})
	sess := decodeResponse[SessionResponse](t, create)
	t.Cleanup(func() { _ = b.DeleteSession(sess.ID) })

	resp := postJSON(t, ts, "/v1/terminal/write_bytes", map[string]any{
		"session_id": sess.ID,
		"bytes":      []byte("raw\n"),
	})
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	got := decodeResponse[TerminalResponse](t, resp)
	if got.Written == 0 {
		t.Fatal("Written = 0")
	}

	liveSess, _ := b.Sessions().Get(sess.ID)
	assertHTTPOutputContains(t, liveSess, "raw", 2*time.Second)
	_, _ = liveSess.Write([]byte{0x04})
	<-liveSess.Done()
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

// TestTerminalSubmit_RejectsOversizedBody locks in the MaxBytesReader cap
// so a future refactor that removes it reappears as a loud test failure
// instead of a silent DOS window. The cap is TerminalRequestMaxBytes; we
// send cap+1 bytes of padding inside the text field.
func TestTerminalSubmit_RejectsOversizedBody(t *testing.T) {
	ts, _ := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"sleep", "30"}})
	sess := decodeResponse[SessionResponse](t, create)

	// Build a body that exceeds the cap. The text field dominates, so
	// total body size = overhead + cap+1. MaxBytesReader triggers before
	// the JSON decoder touches anything.
	padding := strings.Repeat("A", TerminalRequestMaxBytes+1)
	resp := postJSON(t, ts, "/v1/terminal/submit", map[string]any{
		"session_id": sess.ID, "text": padding,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func assertHTTPOutputContains(t *testing.T, s *session.Session, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var buf bytes.Buffer
		for _, c := range s.Log().Since(0, 0) {
			buf.Write(c.Bytes)
		}
		if strings.Contains(buf.String(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session output did not contain %q", want)
}
