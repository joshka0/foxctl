package httpjson

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
)

// TestRooms_HappyPath exercises the full HTTP surface: create a room, join
// two sessions, list members, fan out a message, both PTYs receive it, then
// leave cleanly. This is the primary proof that the daemon's wire contract
// is wired end-to-end.
func TestRooms_HappyPath(t *testing.T) {
	ts, b := newTestServer(t)

	// Two live sessions.
	s1 := createCatSession(t, ts, b)
	s2 := createCatSession(t, ts, b)

	// Create the room.
	var room RoomResponse
	postAndDecode(t, ts, "/v1/rooms", CreateRoomRequest{Workspace: "ws", Title: "wire"}, http.StatusCreated, &room)
	if room.ID == "" {
		t.Fatal("room id missing")
	}

	// Join both sessions.
	var m1 MemberResponse
	postAndDecode(t, ts, "/v1/rooms/"+room.ID+"/join", JoinRoomRequest{
		AgentID: "alice", SessionID: s1, CanMutate: true,
	}, http.StatusCreated, &m1)
	postAndDecode(t, ts, "/v1/rooms/"+room.ID+"/join", JoinRoomRequest{
		AgentID: "bob", SessionID: s2, CanMutate: true,
	}, http.StatusCreated, nil)

	// Verify the listing.
	resp, err := http.Get(ts.URL + "/v1/rooms/" + room.ID + "/members")
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("members status = %d", resp.StatusCode)
	}
	var ml struct {
		Members []MemberResponse `json:"members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ml); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(ml.Members) != 2 {
		t.Fatalf("want 2 members, got %d", len(ml.Members))
	}

	// Fan out.
	var sent SendMessageResponse
	postAndDecode(t, ts, "/v1/messages", SendMessageRequest{
		RoomID: room.ID, Source: "wire-test", CorrelationID: "corr-wire", Text: "WIRE_HELLO",
	}, http.StatusOK, &sent)
	if sent.Delivered != 2 || sent.Failed != 0 {
		t.Fatalf("delivered=%d failed=%d want 2/0", sent.Delivered, sent.Failed)
	}
	if sent.Receipt.RoomID != room.ID || sent.Receipt.MessageID != sent.MessageID ||
		sent.Receipt.Source != "wire-test" || sent.Receipt.CorrelationID != "corr-wire" ||
		sent.Receipt.ReplyPrefix != "@room:"+room.ID+" " {
		t.Fatalf("receipt = %+v, message_id=%s", sent.Receipt, sent.MessageID)
	}
	resp, err = http.Get(ts.URL + "/v1/rooms/" + room.ID + "/messages")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("messages status = %d body = %s", resp.StatusCode, raw)
	}
	var history struct {
		Messages []MessageRecordResponse `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(history.Messages) != 1 || history.Messages[0].ID != sent.MessageID ||
		history.Messages[0].Text != "WIRE_HELLO" || len(history.Messages[0].Members) != 2 {
		t.Fatalf("messages = %+v, want sent message", history.Messages)
	}

	// Assert both cat sessions echoed the text.
	sess1, _ := b.Sessions().Get(s1)
	sess2, _ := b.Sessions().Get(s2)
	assertSessionSeesText(t, sess1, "[ATCP receipt]", 2*time.Second)
	assertSessionSeesText(t, sess1, "<AT>room:"+room.ID+" ", 2*time.Second)
	assertSessionSeesText(t, sess1, "WIRE_HELLO", 2*time.Second)
	assertSessionSeesText(t, sess2, "WIRE_HELLO", 2*time.Second)

	// Leave.
	postAndDecode(t, ts, "/v1/rooms/"+room.ID+"/leave", LeaveRoomRequest{AgentID: "alice"}, http.StatusOK, nil)
}

// TestRooms_CreateRequiresWorkspace locks the 400 mapping for the workspace
// invariant. Prevents silent success if the broker grows lenient.
func TestRooms_CreateRequiresWorkspace(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := postJSON(t, ts, "/v1/rooms", CreateRoomRequest{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestRooms_JoinUnknownSessionReturns404 verifies the broker's
// ErrSessionNotFound is surfaced rather than swallowed.
func TestRooms_JoinUnknownSessionReturns404(t *testing.T) {
	ts, _ := newTestServer(t)
	var room RoomResponse
	postAndDecode(t, ts, "/v1/rooms", CreateRoomRequest{Workspace: "ws"}, http.StatusCreated, &room)
	resp := postJSON(t, ts, "/v1/rooms/"+room.ID+"/join", JoinRoomRequest{
		AgentID: "a", SessionID: "missing",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestRooms_DoubleJoinSameAgentConflicts asserts 409 on the
// agent-already-in-room invariant (plan §5a.7 unique (room_id, agent_id)
// active).
func TestRooms_DoubleJoinSameAgentConflicts(t *testing.T) {
	ts, b := newTestServer(t)
	s1 := createCatSession(t, ts, b)
	s2 := createCatSession(t, ts, b)

	var room RoomResponse
	postAndDecode(t, ts, "/v1/rooms", CreateRoomRequest{Workspace: "ws"}, http.StatusCreated, &room)
	postAndDecode(t, ts, "/v1/rooms/"+room.ID+"/join", JoinRoomRequest{AgentID: "a", SessionID: s1}, http.StatusCreated, nil)
	resp := postJSON(t, ts, "/v1/rooms/"+room.ID+"/join", JoinRoomRequest{AgentID: "a", SessionID: s2})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

// TestMessages_EmptyRoomReturns409 proves "no active members" is semantically
// a precondition failure, not a caller-input 400.
func TestMessages_EmptyRoomReturns409(t *testing.T) {
	ts, _ := newTestServer(t)
	var room RoomResponse
	postAndDecode(t, ts, "/v1/rooms", CreateRoomRequest{Workspace: "ws"}, http.StatusCreated, &room)
	resp := postJSON(t, ts, "/v1/messages", SendMessageRequest{RoomID: room.ID, Text: "x"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

func TestMessages_AwaitsPerMemberActivityAndReady(t *testing.T) {
	ts, _ := newTestServer(t)
	create := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{
		Cmd:       []string{"cat"},
		Readiness: &ReadinessProfileDTO{ThresholdBPS: 1_000_000, DebounceMS: 1},
	})
	sess := decodeResponse[SessionResponse](t, create)

	var room RoomResponse
	postAndDecode(t, ts, "/v1/rooms", CreateRoomRequest{Workspace: "ws"}, http.StatusCreated, &room)
	postAndDecode(t, ts, "/v1/rooms/"+room.ID+"/join", JoinRoomRequest{
		AgentID: "worker", SessionID: sess.ID, CanMutate: true,
	}, http.StatusCreated, nil)

	visible := false
	var sent SendMessageResponse
	postAndDecode(t, ts, "/v1/messages", SendMessageRequest{
		RoomID:          room.ID,
		Source:          "wire-test",
		Text:            "AWAIT_ACTIVITY",
		ReceiptVisible:  &visible,
		AwaitActivityMS: 1000,
		AwaitReadyMS:    1000,
	}, http.StatusOK, &sent)
	if len(sent.Members) != 1 {
		t.Fatalf("members = %d, want 1", len(sent.Members))
	}
	activity := sent.Members[0].Activity
	if activity == nil {
		t.Fatal("activity missing")
	}
	if !activity.OutputChanged || activity.OutputBytesDelta <= 0 || activity.SeqDelta == 0 {
		t.Fatalf("activity = %+v, want output change", activity)
	}
	if !activity.Completed || !activity.Ready || activity.AwaitReadyTimedOut {
		t.Fatalf("activity = %+v, want completed ready", activity)
	}
}

// --- helpers local to rooms_test.go ---

// createCatSession posts a `cat` session and returns its id. Kept here rather
// than promoted to server_test helpers because the room tests are the only
// file that needs a multi-session fixture.
func createCatSession(t *testing.T, ts *httptest.Server, b *broker.Broker) string {
	t.Helper()
	resp := postJSON(t, ts, "/v1/sessions", CreateSessionRequest{Cmd: []string{"cat"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /v1/sessions: status=%d body=%s", resp.StatusCode, body)
	}
	var out SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/sessions/"+out.ID, nil)
		_, _ = http.DefaultClient.Do(req)
	})
	_ = b
	return out.ID
}

// postAndDecode posts body as JSON, asserts the status, and optionally
// decodes the response into out. out may be nil when the caller only wants
// the side effect.
func postAndDecode(t *testing.T, ts *httptest.Server, path string, body any, wantStatus int, out any) {
	t.Helper()
	resp := postJSON(t, ts, path, body)
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: status=%d want=%d body=%s", path, resp.StatusCode, wantStatus, raw)
	}
	if out == nil {
		return
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// assertSessionSeesText polls the session's output log until want is present
// or timeout elapses. Works with any broker session — not coupled to rooms.
func assertSessionSeesText(t *testing.T, s *session.Session, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var b strings.Builder
		for _, c := range s.Log().Since(0, 0) {
			b.Write(c.Bytes)
		}
		if strings.Contains(b.String(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("text %q never appeared in session output", want)
}
