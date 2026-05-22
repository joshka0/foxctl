package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/interfaces/web/sse"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/agents"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/coordination"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
)

type testRoomEventPublisher struct {
	mu            sync.Mutex
	types         []string
	events        []roomMessageEvent
	invalidations []roomInvalidationEvent
}

func activateAPIRoomLoop(t *testing.T, cfg config.Config, workspaceID, roomID string) {
	t.Helper()
	store, err := coordination.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open coordination store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	leaseName := fmt.Sprintf("room-loop:%s:%s:delivery", workspaceID, roomID)
	acquired, err := store.TryAcquireLease(context.Background(), leaseName, "owner-a", time.Minute)
	if err != nil {
		t.Fatalf("TryAcquireLease: %v", err)
	}
	if !acquired {
		t.Fatal("expected lease acquisition")
	}
	_, err = store.UpsertRoomLoop(context.Background(), coordination.RoomLoop{
		WorkspaceID:             workspaceID,
		RoomID:                  roomID,
		Enabled:                 true,
		ManagedBy:               "foxctl.room.loop/test",
		LastTickAt:              &now,
		DeliveryLeaseName:       leaseName,
		DeliveryOwnerID:         "owner-a",
		PulseInterval:           45 * time.Minute,
		ReplyStaleAfter:         90 * time.Minute,
		TaskStaleAfter:          6 * time.Hour,
		MinPulseFloor:           24 * time.Hour,
		CoordinatorPulseEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert room loop: %v", err)
	}
}

func TestMain(m *testing.M) {
	SetRoomSendLiveRelayHookForTests(func(ctx context.Context, workspaceID, roomID, messageID string) ([]RoomLiveRelayResult, error) {
		return nil, nil
	})
	os.Exit(m.Run())
}

func (p *testRoomEventPublisher) Publish(eventType string, data any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.types = append(p.types, eventType)
	if evt, ok := data.(roomMessageEvent); ok {
		p.events = append(p.events, evt)
	}
	if evt, ok := data.(roomInvalidationEvent); ok {
		p.invalidations = append(p.invalidations, evt)
	}
}

func readSSEEvent(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	reader := bufio.NewReader(body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read sse line: %v", err)
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("decode sse event: %v", err)
		}
		return event
	}
}

func TestRoomsListHandler_ReturnsDerivedRoomSummaries(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	store, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	msgs := []agent.BoardMessage{
		{
			WorkspaceID: "ws1",
			TaskID:      "task-1",
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      "actor:agent:a",
			Recipient:   "actor:agent:viewer",
			Subject:     "alpha-1",
			Body:        "first",
			Status:      agent.BoardMessageStatusUnread,
			CreatedAt:   now.Add(-1 * time.Minute),
		},
		{
			WorkspaceID: "ws1",
			TaskID:      "task-2",
			Stream:      agent.RoomStreamName("beta"),
			Sender:      "actor:agent:b",
			Recipient:   agent.BroadcastRecipient,
			Subject:     "beta-1",
			Body:        "second",
			Status:      agent.BoardMessageStatusUnread,
			CreatedAt:   now,
		},
	}
	for i := range msgs {
		if err := store.SendMessage(context.Background(), &msgs[i]); err != nil {
			t.Fatalf("send message[%d]: %v", i, err)
		}
	}

	h := RoomsListHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/rooms?workspace_id=ws1&actor_id=actor:agent:viewer&limit=10", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	rawRooms, ok := body["rooms"].([]any)
	if !ok {
		t.Fatalf("rooms type=%T want []any", body["rooms"])
	}
	if len(rawRooms) != 2 {
		t.Fatalf("rooms=%d want 2", len(rawRooms))
	}
	first, _ := rawRooms[0].(map[string]any)
	if got := strings.TrimSpace(first["id"].(string)); got != "beta" {
		t.Fatalf("first room=%q want beta", got)
	}
}

func TestRoomDetailHandler_GetAndPostMessages(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)
	originalRelayHook := roomSendLiveRelayHook
	roomSendLiveRelayHook = func(ctx context.Context, workspaceID, roomID, messageID string) ([]RoomLiveRelayResult, error) {
		return nil, nil
	}
	defer func() {
		roomSendLiveRelayHook = originalRelayHook
	}()

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"description":"Primary coordination",
		"members":[
			{"actor_id":"actor:agent:a","role":"coordinator"},
			{"actor_id":"actor:agent:b","role":"member"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"actor:agent:a",
		"body":"hello room",
		"task_id":"task-1"
	}`))
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	postBody := decodeResponseBody(t, postRR)
	if got := strings.TrimSpace(postBody["room_id"].(string)); got != "alpha" {
		t.Fatalf("room_id=%q want alpha", got)
	}
	if got := strings.TrimSpace(postBody["stream"].(string)); got != agent.RoomStreamName("alpha") {
		t.Fatalf("stream=%q want %s", got, agent.RoomStreamName("alpha"))
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha?workspace_id=ws1", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	getBody := decodeResponseBody(t, getRR)
	room, ok := getBody["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T want map[string]any", getBody["room"])
	}
	if got := strings.TrimSpace(room["id"].(string)); got != "alpha" {
		t.Fatalf("room.id=%q want alpha", got)
	}
	if got := strings.TrimSpace(room["title"].(string)); got != "Alpha Room" {
		t.Fatalf("room.title=%q want Alpha Room", got)
	}
	if got := strings.TrimSpace(room["description"].(string)); got != "Primary coordination" {
		t.Fatalf("room.description=%q want Primary coordination", got)
	}
	rawMembers, ok := room["members"].([]any)
	if !ok {
		t.Fatalf("room.members type=%T want []any", room["members"])
	}
	if len(rawMembers) != 2 {
		t.Fatalf("room.members=%d want 2", len(rawMembers))
	}

	msgReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/messages?workspace_id=ws1&limit=10", nil)
	msgRR := httptest.NewRecorder()
	h.ServeHTTP(msgRR, msgReq)
	if msgRR.Code != http.StatusOK {
		t.Fatalf("messages status=%d body=%s", msgRR.Code, msgRR.Body.String())
	}
	msgBody := decodeResponseBody(t, msgRR)
	rawMessages, ok := msgBody["messages"].([]any)
	if !ok {
		t.Fatalf("messages type=%T want []any", msgBody["messages"])
	}
	if len(rawMessages) != 1 {
		t.Fatalf("messages=%d want 1", len(rawMessages))
	}
	msg, _ := rawMessages[0].(map[string]any)
	if got := strings.TrimSpace(msg["body"].(string)); got != "hello room" {
		t.Fatalf("message.body=%q want hello room", got)
	}

	memberPatchReq := httptest.NewRequest(http.MethodPatch, "/api/rooms/alpha/members?workspace_id=ws1", strings.NewReader(`{
		"actor_id":"actor:agent:a",
		"members":[{"actor_id":"actor:agent:c","role":"owner","delivery_binding":{"mux_backend":"tmux","mux_session":"room-alpha","mux_pane_id":"%9","transport_endpoint":"/tmp/foxctl-pane/actor-c.sock","transport_kind":"pane_socket","submit_mode":"composer_ctrl_enter"}}]
	}`))
	memberPatchRR := httptest.NewRecorder()
	h.ServeHTTP(memberPatchRR, memberPatchReq)
	if memberPatchRR.Code != http.StatusOK {
		t.Fatalf("member patch status=%d body=%s", memberPatchRR.Code, memberPatchRR.Body.String())
	}
	memberPatchBody := decodeResponseBody(t, memberPatchRR)
	updatedRoom, ok := memberPatchBody["room"].(map[string]any)
	if !ok {
		t.Fatalf("patched room type=%T want map[string]any", memberPatchBody["room"])
	}
	patchedMembers, ok := updatedRoom["members"].([]any)
	if !ok || len(patchedMembers) != 1 {
		t.Fatalf("patched room members=%T %#v want one member", updatedRoom["members"], updatedRoom["members"])
	}
	patchedMember, ok := patchedMembers[0].(map[string]any)
	if !ok {
		t.Fatalf("patched member type=%T want map[string]any", patchedMembers[0])
	}
	binding, ok := patchedMember["delivery_binding"].(map[string]any)
	if !ok {
		t.Fatalf("delivery_binding type=%T want map[string]any", patchedMember["delivery_binding"])
	}
	if got := strings.TrimSpace(fmt.Sprint(binding["mux_session"])); got != "room-alpha" {
		t.Fatalf("delivery_binding.mux_session=%q want room-alpha", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(binding["submit_mode"])); got != "composer_ctrl_enter" {
		t.Fatalf("delivery_binding.submit_mode=%q want composer_ctrl_enter", got)
	}
	updatedMembers, ok := updatedRoom["members"].([]any)
	if !ok {
		t.Fatalf("patched room members type=%T want []any", updatedRoom["members"])
	}
	if len(updatedMembers) != 1 {
		t.Fatalf("patched room members=%d want 1", len(updatedMembers))
	}
}

type roomAgileTestResponse[T any] struct {
	Action    string `json:"action"`
	RoomID    string `json:"room_id"`
	Workspace string `json:"workspace"`
	Result    struct {
		Version int                        `json:"version"`
		Status  string                     `json:"status"`
		Command string                     `json:"command"`
		Data    T                          `json:"data"`
		Meta    RoomAgileMeta              `json:"meta"`
		Error   map[string]json.RawMessage `json:"error"`
	} `json:"result"`
}

func decodeRoomAgileTestResponse[T any](t *testing.T, rr *httptest.ResponseRecorder, action string) roomAgileTestResponse[T] {
	t.Helper()
	var body roomAgileTestResponse[T]
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode room-agile response: %v body=%s", err, rr.Body.String())
	}
	if body.Action != action {
		t.Fatalf("action=%q want %q", body.Action, action)
	}
	if body.RoomID != "alpha" {
		t.Fatalf("room_id=%q want alpha", body.RoomID)
	}
	if body.Workspace != "ws1" {
		t.Fatalf("workspace=%q want ws1", body.Workspace)
	}
	if body.Result.Version != 1 {
		t.Fatalf("result.version=%d want 1", body.Result.Version)
	}
	if body.Result.Status != "ok" {
		t.Fatalf("result.status=%q want ok", body.Result.Status)
	}
	wantCommand := "foxctl.room.agile." + strings.ReplaceAll(action, "_", ".")
	if body.Result.Command != wantCommand {
		t.Fatalf("result.command=%q want %q", body.Result.Command, wantCommand)
	}
	if body.Result.Meta.Source != "web:/api/rooms/{room_id}/agile" {
		t.Fatalf("result.meta.source=%q want web:/api/rooms/{room_id}/agile", body.Result.Meta.Source)
	}
	if len(body.Result.Error) != 0 {
		t.Fatalf("result.error=%v want empty object", body.Result.Error)
	}
	return body
}

func TestRoomDetailHandler_RoomAgileReadEndpoint(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[{"actor_id":"human-a","role":"coordinator"}]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	store, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer store.Close()

	epic := agent.BoardMessage{
		WorkspaceID: "ws1",
		Stream:      agent.RoomStreamName("alpha"),
		Sender:      "human-a",
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindEpic,
		Subject:     "Epic: Pi integration",
		Body:        "Goal: Make Pi understand room-agile state",
	}
	if err := store.SendMessage(context.Background(), &epic); err != nil {
		t.Fatalf("send epic: %v", err)
	}
	finalize := agent.BoardMessage{
		WorkspaceID:      "ws1",
		Stream:           agent.RoomStreamName("alpha"),
		Sender:           "human-a",
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindEpicFinalize,
		RelatedMessageID: epic.ID,
		Subject:          "Epic Finalized: Pi integration",
		Body:             "Summary: ready for milestones",
	}
	if err := store.SendMessage(context.Background(), &finalize); err != nil {
		t.Fatalf("send finalize: %v", err)
	}
	milestone := agent.BoardMessage{
		WorkspaceID:      "ws1",
		Stream:           agent.RoomStreamName("alpha"),
		Sender:           "human-a",
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindMilestone,
		RelatedMessageID: epic.ID,
		Subject:          "Milestone: Backend API",
		Body:             "EpicID: " + epic.ID + "\nObjective: expose read model",
	}
	if err := store.SendMessage(context.Background(), &milestone); err != nil {
		t.Fatalf("send milestone: %v", err)
	}
	story := agent.BoardMessage{
		WorkspaceID:      "ws1",
		Stream:           agent.RoomStreamName("alpha"),
		Sender:           "human-a",
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStory,
		RelatedMessageID: milestone.ID,
		Subject:          "Story: Add endpoint",
		Body:             "Expose agile reads",
	}
	if err := store.SendMessage(context.Background(), &story); err != nil {
		t.Fatalf("send story: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"epic_next",
		"epic_id":"%s"
	}`, epic.ID)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeRoomAgileTestResponse[roomAgileEpicPlanResult](t, rr, "epic_next")
	data := body.Result.Data
	if got := data.Status.MilestoneCount; got != 1 {
		t.Fatalf("milestone_count=%d want 1", got)
	}
	if got := data.Status.StoryCount; got != 1 {
		t.Fatalf("story_count=%d want 1", got)
	}
	if len(data.Next) != 1 {
		t.Fatalf("next actions=%d want 1", len(data.Next))
	}
	first := data.Next[0]
	if got := first.Action; got != "start_story" {
		t.Fatalf("next action=%v want start_story", got)
	}
}

func TestRoomDetailHandler_RoomAgileHealthEndpoint(t *testing.T) {
	h, _, epicID, _, _ := setupRoomAgileTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"epic_health",
		"epic_id":"%s"
	}`, epicID)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("epic_health status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeRoomAgileTestResponse[roomAgileEpicPlanResult](t, rr, "epic_health")
	data := body.Result.Data
	if data.Health == nil {
		t.Fatalf("health missing")
	}
	if got := data.Health.Status; got != "healthy" {
		t.Fatalf("health.status=%q want healthy", got)
	}
	if got := len(data.Health.Warnings); got != 0 {
		t.Fatalf("health.warnings=%d want 0", got)
	}
	if got := len(data.Next); got != 0 {
		t.Fatalf("next actions=%d want 0 for epic_health", got)
	}
	if got := data.Status.ValidatedStories; got != 0 {
		t.Fatalf("validated_stories=%d want 0", got)
	}
}

func TestRoomDetailHandler_PostMessagesRequiresActiveLoop(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"actor:agent:a","role":"coordinator"},
			{"actor_id":"actor:agent:b","role":"member"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"actor:agent:a",
		"body":"hello room"
	}`))
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusConflict {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	if !strings.Contains(postRR.Body.String(), "room loop is not active") {
		t.Fatalf("post body=%s want active loop error", postRR.Body.String())
	}

	store, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer store.Close()
	messages, err := store.ListRoomMessages(context.Background(), "ws1", "alpha", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("messages=%d want 0 after failed send", len(messages))
	}
}

func TestRoomDetailHandler_GetEventsStreamsScopedRoomMessages(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	hub := sse.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	srv := httptest.NewServer(RoomDetailHandler(cfg, zerolog.Nop(), hub))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/rooms/alpha/events?workspace_id=ws1")
	if err != nil {
		t.Fatalf("get room events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	connected := readSSEEvent(t, resp.Body)
	if got := strings.TrimSpace(fmt.Sprint(connected["type"])); got != "connected" {
		t.Fatalf("connected type=%q want connected", got)
	}

	publishRoomMessageEvent(hub, roomMessageEvent{
		WorkspaceID: "ws1",
		RoomID:      "beta",
		Stream:      agent.RoomStreamName("beta"),
		MessageID:   "msg-beta",
		Phase:       "sent",
	})
	publishRoomMessageEvent(hub, roomMessageEvent{
		WorkspaceID: "ws1",
		RoomID:      "alpha",
		Stream:      agent.RoomStreamName("alpha"),
		MessageID:   "msg-alpha",
		Phase:       "sent",
	})

	event := readSSEEvent(t, resp.Body)
	if got := strings.TrimSpace(fmt.Sprint(event["type"])); got != "room.message" {
		t.Fatalf("event type=%q want room.message", got)
	}
	data, ok := event["data"].(map[string]any)
	if !ok {
		t.Fatalf("event data type=%T want map[string]any", event["data"])
	}
	if got := strings.TrimSpace(fmt.Sprint(data["room_id"])); got != "alpha" {
		t.Fatalf("room_id=%q want alpha", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(data["message_id"])); got != "msg-alpha" {
		t.Fatalf("message_id=%q want msg-alpha", got)
	}
}

func TestRoomDetailHandler_PostMessageQueuesForRoomLoopDelivery(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"claude-a","role":"participant"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"dev-local-user",
		"recipient":"claude-a",
		"body":"hello from gui",
		"kind":"instruction",
		"interrupt":true
	}`))
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	postBody := decodeResponseBody(t, postRR)
	if got := strings.TrimSpace(fmt.Sprint(postBody["status"])); got != "queued" {
		t.Fatalf("status=%q want queued", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(postBody["delivery_owner"])); got != "room_loop" {
		t.Fatalf("delivery_owner=%q want room_loop", got)
	}
	if got := fmt.Sprint(postBody["delivery_pending"]); got != "true" {
		t.Fatalf("delivery_pending=%q want true", got)
	}
	if rawRelay, ok := postBody["live_relay"]; ok {
		if relay, ok := rawRelay.([]any); !ok || len(relay) != 0 {
			t.Fatalf("live_relay=%T %#v want empty or omitted when send path no longer relays", rawRelay, rawRelay)
		}
	}
}

func TestRoomDetailHandler_PostMessageWithoutRelayHookStillQueuesForRoomLoop(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	originalRelayHook := roomSendLiveRelayHook
	roomSendLiveRelayHook = nil
	defer func() {
		roomSendLiveRelayHook = originalRelayHook
	}()

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"claude-a","role":"participant"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"dev-local-user",
		"recipient":"claude-a",
		"body":"hello from api without immediate relay",
		"kind":"instruction"
	}`))
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	postBody := decodeResponseBody(t, postRR)
	if got := strings.TrimSpace(fmt.Sprint(postBody["status"])); got != "queued" {
		t.Fatalf("status=%q want queued", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(postBody["delivery_owner"])); got != "room_loop" {
		t.Fatalf("delivery_owner=%q want room_loop", got)
	}
	if rawRelay, ok := postBody["live_relay"]; ok {
		if relay, ok := rawRelay.([]any); !ok || len(relay) != 0 {
			t.Fatalf("live_relay=%T %#v want empty or omitted when no hook is installed", rawRelay, rawRelay)
		}
	}
}

func TestRoomDetailHandler_AddListAndCancelReminder(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"claude-a","role":"participant"},
			{"actor_id":"coordinator-a","role":"coordinator"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	addReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/reminders", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"dev-local-user",
		"recipient":"claude-a",
		"subject":"Status update requested",
		"body":"Please reply with current status.",
		"every":"15m",
		"max_iterations":3,
		"reply_expected":true,
		"allow_passive":true
	}`))
	addRR := httptest.NewRecorder()
	h.ServeHTTP(addRR, addReq)
	if addRR.Code != http.StatusCreated {
		t.Fatalf("add reminder status=%d body=%s", addRR.Code, addRR.Body.String())
	}
	addBody := decodeResponseBody(t, addRR)
	reminder, ok := addBody["reminder"].(map[string]any)
	if !ok {
		t.Fatalf("reminder type=%T want map[string]any", addBody["reminder"])
	}
	reminderID := strings.TrimSpace(fmt.Sprint(reminder["id"]))
	if reminderID == "" {
		t.Fatal("reminder id should be set")
	}
	if got := strings.TrimSpace(fmt.Sprint(addBody["delivery_owner"])); got != "room_loop" {
		t.Fatalf("delivery_owner=%q want room_loop", got)
	}
	if got := fmt.Sprint(addBody["delivery_pending"]); got != "true" {
		t.Fatalf("delivery_pending=%q want true", got)
	}
	if rawRelay, ok := addBody["live_relay"]; ok {
		if relay, ok := rawRelay.([]any); !ok || len(relay) != 0 {
			t.Fatalf("live_relay=%T %#v want empty or omitted for room-loop-owned reminder delivery", rawRelay, rawRelay)
		}
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/reminders?workspace_id=ws1", nil)
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list reminders status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	listBody := decodeResponseBody(t, listRR)
	reminders, ok := listBody["reminders"].([]any)
	if !ok || len(reminders) != 1 {
		t.Fatalf("reminders=%T %#v want one reminder", listBody["reminders"], listBody["reminders"])
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/reminders/"+reminderID+"/cancel?workspace_id=ws1", strings.NewReader(`{
		"actor":"dev-local-user"
	}`))
	cancelRR := httptest.NewRecorder()
	h.ServeHTTP(cancelRR, cancelReq)
	if cancelRR.Code != http.StatusOK {
		t.Fatalf("cancel reminder status=%d body=%s", cancelRR.Code, cancelRR.Body.String())
	}
	cancelBody := decodeResponseBody(t, cancelRR)
	cancelled, ok := cancelBody["reminder"].(map[string]any)
	if !ok {
		t.Fatalf("cancelled reminder type=%T want map[string]any", cancelBody["reminder"])
	}
	if active := cancelled["active"]; active != false {
		t.Fatalf("cancelled active=%v want false", active)
	}
}

func TestRoomDetailHandler_AddReminderDedupesEquivalentActiveReminder(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"claude-a","role":"participant"},
			{"actor_id":"coordinator-a","role":"coordinator"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	body := `{
		"workspace_id":"ws1",
		"sender":"dev-local-user",
		"recipient":"claude-a",
		"subject":"Status update requested",
		"body":"Please reply with current status.",
		"task_id":"task-1",
		"every":"15m",
		"max_iterations":3,
		"reply_expected":true
	}`
	addReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/reminders", strings.NewReader(body))
	addRR := httptest.NewRecorder()
	h.ServeHTTP(addRR, addReq)
	if addRR.Code != http.StatusCreated {
		t.Fatalf("first add status=%d body=%s", addRR.Code, addRR.Body.String())
	}
	firstBody := decodeResponseBody(t, addRR)
	firstReminder := firstBody["reminder"].(map[string]any)
	firstID := strings.TrimSpace(fmt.Sprint(firstReminder["id"]))

	addReq = httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/reminders", strings.NewReader(body))
	addRR = httptest.NewRecorder()
	h.ServeHTTP(addRR, addReq)
	if addRR.Code != http.StatusOK {
		t.Fatalf("second add status=%d body=%s", addRR.Code, addRR.Body.String())
	}
	secondBody := decodeResponseBody(t, addRR)
	if got := fmt.Sprint(secondBody["deduped"]); got != "true" {
		t.Fatalf("deduped=%q want true", got)
	}
	secondReminder := secondBody["reminder"].(map[string]any)
	secondID := strings.TrimSpace(fmt.Sprint(secondReminder["id"]))
	if secondID != firstID {
		t.Fatalf("second reminder id=%q want %q", secondID, firstID)
	}

	store, err := coordination.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open coordination store: %v", err)
	}
	defer store.Close()
	reminders, err := store.ListRoomReminders(context.Background(), "ws1", "alpha", false)
	if err != nil {
		t.Fatalf("ListRoomReminders: %v", err)
	}
	if len(reminders) != 1 {
		t.Fatalf("len(reminders)=%d want 1", len(reminders))
	}
}

func TestRoomDetailHandler_AddPassiveReminder(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"claude-a","role":"participant"},
			{"actor_id":"coordinator-a","role":"coordinator"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	addReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/reminders", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"dev-local-user",
		"recipient":"claude-a",
		"subject":"Topology hard-cut cadence pulse",
		"body":"Keep the loop moving.",
		"every":"15m",
		"max_iterations":3,
		"passive":true,
		"allow_passive":true
	}`))
	addRR := httptest.NewRecorder()
	h.ServeHTTP(addRR, addReq)
	if addRR.Code != http.StatusCreated {
		t.Fatalf("add passive reminder status=%d body=%s", addRR.Code, addRR.Body.String())
	}
	addBody := decodeResponseBody(t, addRR)
	reminder, ok := addBody["reminder"].(map[string]any)
	if !ok {
		t.Fatalf("reminder type=%T want map[string]any", addBody["reminder"])
	}
	if got := reminder["passive"]; got != true {
		t.Fatalf("passive=%v want true", got)
	}
	if got := reminder["reply_expected"]; got != false {
		t.Fatalf("reply_expected=%v want false", got)
	}
	message, ok := addBody["message"].(map[string]any)
	if !ok {
		t.Fatalf("message type=%T want map[string]any", addBody["message"])
	}
	if got, ok := message["reply_expected"]; ok && got != false {
		t.Fatalf("message.reply_expected=%v want false or omitted", got)
	}
	if got, ok := message["ack_required"]; ok && got != false {
		t.Fatalf("message.ack_required=%v want false or omitted", got)
	}
}

func TestRoomDetailHandler_PutMemberBinding(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"coordinator-a","role":"coordinator"},
			{"actor_id":"droid-a","role":"participant"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/rooms/alpha/members/droid-a/binding?workspace_id=ws1", strings.NewReader(`{
		"actor_id":"droid-a",
		"backend":"tmux",
		"session":"146",
		"pane_id":"%159",
		"transport_endpoint":"/tmp/droid-a.sock",
		"transport_kind":"pane_socket",
		"delivery_binding":{
			"mux_backend":"tmux",
			"mux_session":"146",
			"mux_pane_id":"%159",
			"transport_endpoint":"/tmp/droid-a.sock",
			"transport_kind":"pane_socket",
			"submit_mode":"composer_ctrl_enter",
			"health":"ready",
			"fallback_policy":"allow_legacy_mux"
		}
	}`))
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, putReq)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	body := decodeResponseBody(t, putRR)
	member, ok := body["member"].(map[string]any)
	if !ok {
		t.Fatalf("member type=%T want map[string]any", body["member"])
	}
	for _, key := range []string{"backend", "session", "pane_id", "transport_endpoint", "transport_kind"} {
		if _, ok := member[key]; ok {
			t.Fatalf("member contains legacy transport field %q: %#v", key, member)
		}
	}
	binding, ok := member["delivery_binding"].(map[string]any)
	if !ok {
		t.Fatalf("delivery_binding type=%T want map[string]any", member["delivery_binding"])
	}
	if got := strings.TrimSpace(fmt.Sprint(binding["submit_mode"])); got != "composer_ctrl_enter" {
		t.Fatalf("delivery_binding.submit_mode=%q want composer_ctrl_enter", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(binding["health"])); got != "ready" {
		t.Fatalf("delivery_binding.health=%q want ready", got)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha?workspace_id=ws1", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	getBody := decodeResponseBody(t, getRR)
	room, ok := getBody["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T want map[string]any", getBody["room"])
	}
	members, ok := room["members"].([]any)
	if !ok {
		t.Fatalf("room.members type=%T want []any", room["members"])
	}
	var detailMember map[string]any
	for _, raw := range members {
		candidate, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(candidate["actor_id"])) == "droid-a" {
			detailMember = candidate
			break
		}
	}
	if detailMember == nil {
		t.Fatalf("room.members=%#v want droid-a", members)
	}
	for _, key := range []string{"backend", "session", "pane_id", "transport_endpoint", "transport_kind"} {
		if _, ok := detailMember[key]; ok {
			t.Fatalf("detail member contains legacy transport field %q: %#v", key, detailMember)
		}
	}
	if _, ok := detailMember["delivery_binding"].(map[string]any); !ok {
		t.Fatalf("detail member delivery_binding type=%T want map[string]any", detailMember["delivery_binding"])
	}
}

func TestRoomDetailHandler_MemberTransportRouteRemoved(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"coordinator-a","role":"coordinator"},
			{"actor_id":"droid-a","role":"participant"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/rooms/alpha/members/droid-a/transport?workspace_id=ws1", strings.NewReader(`{
		"actor_id":"coordinator-a",
		"transport_endpoint":"/tmp/droid-a.sock",
		"transport_kind":"pane_socket"
	}`))
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, putReq)
	if putRR.Code != http.StatusNotFound {
		t.Fatalf("legacy transport route status=%d body=%s", putRR.Code, putRR.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha?workspace_id=ws1", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	getBody := decodeResponseBody(t, getRR)
	room := getBody["room"].(map[string]any)
	members := room["members"].([]any)
	for _, raw := range members {
		member := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(member["actor_id"])) != "droid-a" {
			continue
		}
		binding, _ := member["delivery_binding"].(map[string]any)
		if rawEndpoint, ok := binding["transport_endpoint"]; ok && strings.TrimSpace(fmt.Sprint(rawEndpoint)) != "" {
			t.Fatalf("legacy transport route unexpectedly updated delivery_binding: %#v", member)
		}
		for _, key := range []string{"backend", "session", "pane_id", "transport_endpoint", "transport_kind"} {
			if _, ok := member[key]; ok {
				t.Fatalf("member contains legacy transport field %q after removed route: %#v", key, member)
			}
		}
		return
	}
	t.Fatalf("room.members=%#v want droid-a", members)
}

func TestRoomDetailHandler_MembersPatchRequiresCoordinator(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"coordinator-a","role":"coordinator"},
			{"actor_id":"member-a","role":"participant"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/rooms/alpha/members?workspace_id=ws1", strings.NewReader(`{
		"actor_id":"member-a",
		"members":[{"actor_id":"member-a","role":"participant"}]
	}`))
	patchRR := httptest.NewRecorder()
	h.ServeHTTP(patchRR, patchReq)
	if patchRR.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", patchRR.Code, patchRR.Body.String())
	}
}

func TestRoomDetailHandler_PatchRequiresCoordinator(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"coordinator-a","role":"coordinator"},
			{"actor_id":"member-a","role":"participant"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/rooms/alpha?workspace_id=ws1", strings.NewReader(`{
		"actor_id":"member-a",
		"title":"New Title"
	}`))
	patchRR := httptest.NewRecorder()
	h.ServeHTTP(patchRR, patchReq)
	if patchRR.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", patchRR.Code, patchRR.Body.String())
	}
}

func TestRoomDetailHandler_PutMemberBindingRejectsSelfRoleChange(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[
			{"actor_id":"coordinator-a","role":"coordinator"},
			{"actor_id":"droid-a","role":"participant"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/rooms/alpha/members/droid-a/binding?workspace_id=ws1", strings.NewReader(`{
		"actor_id":"droid-a",
		"role":"coordinator",
		"transport_endpoint":"/tmp/droid-a.sock",
		"transport_kind":"pane_socket"
	}`))
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, putReq)
	if putRR.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", putRR.Code, putRR.Body.String())
	}
}

func TestRoomDetailHandler_ArchiveAndRestore(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	detailHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)
	originalRelayHook := roomSendLiveRelayHook
	roomSendLiveRelayHook = func(ctx context.Context, workspaceID, roomID, messageID string) ([]RoomLiveRelayResult, error) {
		return nil, nil
	}
	t.Cleanup(func() {
		roomSendLiveRelayHook = originalRelayHook
	})

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"archive-room",
		"title":"Archive Room"
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "archive-room")

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/archive-room/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"actor:agent:a",
		"body":"preserve timeline"
	}`))
	postRR := httptest.NewRecorder()
	detailHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}

	archiveReq := httptest.NewRequest(http.MethodPost, "/api/rooms/archive-room/archive?workspace_id=ws1", nil)
	archiveRR := httptest.NewRecorder()
	detailHandler.ServeHTTP(archiveRR, archiveReq)
	if archiveRR.Code != http.StatusOK {
		t.Fatalf("archive status=%d body=%s", archiveRR.Code, archiveRR.Body.String())
	}

	activeListReq := httptest.NewRequest(http.MethodGet, "/api/rooms?workspace_id=ws1", nil)
	activeListRR := httptest.NewRecorder()
	listHandler.ServeHTTP(activeListRR, activeListReq)
	if activeListRR.Code != http.StatusOK {
		t.Fatalf("active list status=%d body=%s", activeListRR.Code, activeListRR.Body.String())
	}
	activeListBody := decodeResponseBody(t, activeListRR)
	if got := int(activeListBody["count"].(float64)); got != 0 {
		t.Fatalf("active count=%d want 0", got)
	}

	archivedListReq := httptest.NewRequest(http.MethodGet, "/api/rooms?workspace_id=ws1&archived_only=true", nil)
	archivedListRR := httptest.NewRecorder()
	listHandler.ServeHTTP(archivedListRR, archivedListReq)
	if archivedListRR.Code != http.StatusOK {
		t.Fatalf("archived list status=%d body=%s", archivedListRR.Code, archivedListRR.Body.String())
	}
	archivedListBody := decodeResponseBody(t, archivedListRR)
	if got := int(archivedListBody["count"].(float64)); got != 1 {
		t.Fatalf("archived count=%d want 1", got)
	}
	archivedRooms, _ := archivedListBody["rooms"].([]any)
	firstArchived, _ := archivedRooms[0].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(firstArchived["id"])); got != "archive-room" {
		t.Fatalf("archived room id=%q want archive-room", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(firstArchived["archived_at"])); got == "" {
		t.Fatal("archived_at should be populated for archived room")
	}

	restoreReq := httptest.NewRequest(http.MethodPost, "/api/rooms/archive-room/restore?workspace_id=ws1", nil)
	restoreRR := httptest.NewRecorder()
	detailHandler.ServeHTTP(restoreRR, restoreReq)
	if restoreRR.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", restoreRR.Code, restoreRR.Body.String())
	}

	activeAfterRestoreReq := httptest.NewRequest(http.MethodGet, "/api/rooms?workspace_id=ws1", nil)
	activeAfterRestoreRR := httptest.NewRecorder()
	listHandler.ServeHTTP(activeAfterRestoreRR, activeAfterRestoreReq)
	if activeAfterRestoreRR.Code != http.StatusOK {
		t.Fatalf("active after restore status=%d body=%s", activeAfterRestoreRR.Code, activeAfterRestoreRR.Body.String())
	}
	activeAfterRestoreBody := decodeResponseBody(t, activeAfterRestoreRR)
	if got := int(activeAfterRestoreBody["count"].(float64)); got != 1 {
		t.Fatalf("active count after restore=%d want 1", got)
	}
	activeRooms, _ := activeAfterRestoreBody["rooms"].([]any)
	firstActive, _ := activeRooms[0].(map[string]any)
	if raw, ok := firstActive["archived_at"]; ok && raw != nil && strings.TrimSpace(fmt.Sprint(raw)) != "" {
		t.Fatalf("archived_at after restore=%q want empty/omitted", strings.TrimSpace(fmt.Sprint(raw)))
	}
}

func TestRoomDetailHandler_PostMessageRejectsBroadcastReplyExpected(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	h := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"actor:agent:a",
		"body":"please review",
		"reply_expected":true
	}`))
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", postRR.Code, postRR.Body.String())
	}
}

func TestRoomDetailHandler_PostMessageDispatchesAgentReplies(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	pub := &testRoomEventPublisher{}
	h := RoomDetailHandler(cfg, zerolog.Nop(), pub)
	originalRelayHook := roomSendLiveRelayHook
	roomSendLiveRelayHook = func(ctx context.Context, workspaceID, roomID, messageID string) ([]RoomLiveRelayResult, error) {
		return nil, nil
	}
	t.Cleanup(func() {
		roomSendLiveRelayHook = originalRelayHook
	})

	agentStore, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := agentStore.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	if err := agentStore.Create(context.Background(), agent.Agent{
		ID:          "agent-room-worker-1",
		Namespace:   "ws1",
		Name:        "Room Worker",
		Role:        "coder",
		SkillsAllow: []string{},
		Policy:      agent.Policy{},
		ShareBB:     "scoped",
		State:       agent.StateRunning,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"agent-room",
		"title":"Agent Room",
		"members":[
			{"actor_id":"agent-room-worker-1","role":"worker"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "agent-room")

	done := make(chan struct{}, 1)
	origRunner := runRoomAgentReply
	runRoomAgentReply = func(_ context.Context, cfg config.Config, _ zerolog.Logger, target agent.Agent, req roomAgentDispatchRequest, events roomEventPublisher) error {
		publishRoomMessageEvent(events, roomMessageEvent{
			WorkspaceID:   req.WorkspaceID,
			RoomID:        req.RoomID,
			Stream:        req.Stream,
			CorrelationID: "corr-room-1",
			Sender:        target.ID,
			Subject:       fmt.Sprintf("Reply from %s", target.ID),
			AgentID:       target.ID,
			Phase:         "agent_delta",
			ContentDelta:  "working...",
		})
		store, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
		if err != nil {
			return err
		}
		defer store.Close()

		reply := &agent.BoardMessage{
			WorkspaceID: req.WorkspaceID,
			TaskID:      req.TaskID,
			Stream:      req.Stream,
			Sender:      target.ID,
			Recipient:   agent.BroadcastRecipient,
			Subject:     fmt.Sprintf("Reply from %s", target.ID),
			Body:        "worker reply",
			Kind:        agent.BoardMessageKindInfo,
			Priority:    agent.DefaultPriority,
			Status:      agent.BoardMessageStatusUnread,
			CreatedAt:   time.Now().UTC(),
		}
		if err := store.SendMessage(context.Background(), reply); err != nil {
			return err
		}
		publishRoomMessageEvent(events, roomMessageEvent{
			WorkspaceID:   req.WorkspaceID,
			RoomID:        req.RoomID,
			Stream:        req.Stream,
			MessageID:     reply.ID,
			CorrelationID: "corr-room-1",
			Sender:        reply.Sender,
			Recipient:     reply.Recipient,
			Subject:       reply.Subject,
			AgentID:       target.ID,
			Content:       reply.Body,
			Phase:         "agent_completed",
		})
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	}
	t.Cleanup(func() { runRoomAgentReply = origRunner })

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/agent-room/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"human:gui",
		"body":"coordinate this change",
		"dispatch_agents":true
	}`))
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}

	postBody := decodeResponseBody(t, postRR)
	if got := strings.TrimSpace(fmt.Sprint(postBody["dispatched"])); got != "1" {
		t.Fatalf("dispatched=%q want 1", got)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for room agent reply")
	}

	msgReq := httptest.NewRequest(http.MethodGet, "/api/rooms/agent-room/messages?workspace_id=ws1&limit=10", nil)
	msgRR := httptest.NewRecorder()
	h.ServeHTTP(msgRR, msgReq)
	if msgRR.Code != http.StatusOK {
		t.Fatalf("messages status=%d body=%s", msgRR.Code, msgRR.Body.String())
	}
	msgBody := decodeResponseBody(t, msgRR)
	rawMessages, ok := msgBody["messages"].([]any)
	if !ok {
		t.Fatalf("messages type=%T want []any", msgBody["messages"])
	}
	if len(rawMessages) != 2 {
		t.Fatalf("messages=%d want 2", len(rawMessages))
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.types) < 2 {
		t.Fatalf("published types=%v want at least room.message events", pub.types)
	}
	if pub.events[0].Phase != "sent" {
		t.Fatalf("first event phase=%q want sent", pub.events[0].Phase)
	}
	foundReply := false
	foundDelta := false
	for _, event := range pub.events {
		if event.Phase == "agent_delta" && event.ContentDelta == "working..." {
			foundDelta = true
		}
		if event.Phase == "agent_completed" && event.AgentID == "agent-room-worker-1" {
			foundReply = true
		}
	}
	if !foundDelta {
		t.Fatalf("published events=%+v want agent_delta", pub.events)
	}
	if !foundReply {
		t.Fatalf("published events=%+v want agent_completed", pub.events)
	}
}

func TestRoomDetailHandler_PostMessageMarksLinkedBoardCardDone(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)
	seedHandler := OrchestrationSeedCardsHandler(cfg, zerolog.Nop())
	cardHandler := OrchestrationBoardCardGetHandler(cfg, zerolog.Nop())
	originalRelayHook := roomSendLiveRelayHook
	roomSendLiveRelayHook = func(ctx context.Context, workspaceID, roomID, messageID string) ([]RoomLiveRelayResult, error) {
		return nil, nil
	}
	defer func() {
		roomSendLiveRelayHook = originalRelayHook
	}()

	seedReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/seed-cards", strings.NewReader(`{
		"request_id":"req-room-board-seed-001",
		"workspace_id":"ws1",
		"cards":[{"issue_id":"issue-room-1","issue_identifier":"ROOM-1","title":"Room bridge card"}]
	}`))
	seedRR := httptest.NewRecorder()
	seedHandler.ServeHTTP(seedRR, seedReq)
	if seedRR.Code != http.StatusOK {
		t.Fatalf("seed status=%d body=%s", seedRR.Code, seedRR.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"bridge-room",
		"title":"Bridge Room"
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create room status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "bridge-room")

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/bridge-room/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"human:gui",
		"task_id":"issue-room-1",
		"body":"ROOM-BOARD-DONE issue-room-1: completed in room"
	}`))
	postRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}

	cardReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-card-get?workspace_id=ws1&issue_id=issue-room-1", nil)
	cardRR := httptest.NewRecorder()
	cardHandler.ServeHTTP(cardRR, cardReq)
	if cardRR.Code != http.StatusOK {
		t.Fatalf("card status=%d body=%s", cardRR.Code, cardRR.Body.String())
	}
	cardBody := decodeResponseBody(t, cardRR)
	data, _ := cardBody["data"].(map[string]any)
	card, _ := data["card"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(card["tracker_state"])); got != "Done" {
		t.Fatalf("tracker_state=%q want Done", got)
	}
}

func TestResolveRoomBoardAction(t *testing.T) {
	action, ok := resolveRoomBoardAction("issue-task-1", map[string]any{
		"issue_id":     "issue-ctx-1",
		"board_action": "mark-done",
	}, "plain body")
	if !ok {
		t.Fatal("expected board action from explicit context")
	}
	if action.IssueID != "issue-ctx-1" {
		t.Fatalf("issue_id=%q want issue-ctx-1", action.IssueID)
	}
	if action.Action != orchestrationActionMarkDone {
		t.Fatalf("action=%q want %q", action.Action, orchestrationActionMarkDone)
	}

	action, ok = resolveRoomBoardAction("issue-task-2", nil, "ROOM-BOARD-RELEASE issue-task-2: released by room")
	if !ok {
		t.Fatal("expected board action from message marker")
	}
	if action.IssueID != "issue-task-2" {
		t.Fatalf("issue_id=%q want issue-task-2", action.IssueID)
	}
	if action.Action != orchestrationActionRelease {
		t.Fatalf("action=%q want %q", action.Action, orchestrationActionRelease)
	}
}

func TestRoomDetailHandler_PostMessageUsesPersistedDispatchPolicy(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	pub := &testRoomEventPublisher{}
	h := RoomDetailHandler(cfg, zerolog.Nop(), pub)

	agentStore, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := agentStore.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	for _, a := range []agent.Agent{
		{
			ID:          "agent-room-lead",
			Namespace:   "ws1",
			Name:        "Lead",
			Role:        "overseer",
			SkillsAllow: []string{},
			Policy:      agent.Policy{},
			ShareBB:     "scoped",
			State:       agent.StateRunning,
			CreatedAt:   time.Now().UTC(),
		},
		{
			ID:          "agent-room-child",
			Namespace:   "ws1",
			Name:        "Child",
			Role:        "coder",
			SkillsAllow: []string{},
			Policy:      agent.Policy{},
			ShareBB:     "scoped",
			State:       agent.StateRunning,
			CreatedAt:   time.Now().UTC(),
		},
	} {
		if err := agentStore.Create(context.Background(), a); err != nil {
			t.Fatalf("create agent %s: %v", a.ID, err)
		}
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"agent-room-defaults",
		"title":"Agent Room Defaults",
		"dispatch_policy":"lead_only",
		"members":[
			{"actor_id":"agent-room-lead","role":"lead"},
			{"actor_id":"agent-room-child","role":"worker"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "agent-room-defaults")

	targets := make(chan string, 2)
	origRunner := runRoomAgentReply
	runRoomAgentReply = func(_ context.Context, _ config.Config, _ zerolog.Logger, target agent.Agent, _ roomAgentDispatchRequest, _ roomEventPublisher) error {
		targets <- target.ID
		return nil
	}
	t.Cleanup(func() { runRoomAgentReply = origRunner })

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/agent-room-defaults/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"human:gui",
		"body":"coordinate this change",
		"dispatch_agents":true
	}`))
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}

	postBody := decodeResponseBody(t, postRR)
	if got := strings.TrimSpace(fmt.Sprint(postBody["dispatched"])); got != "1" {
		t.Fatalf("dispatched=%q want 1", got)
	}

	select {
	case targetID := <-targets:
		if targetID != "agent-room-lead" {
			t.Fatalf("target=%q want agent-room-lead", targetID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for persisted dispatch target")
	}

	select {
	case targetID := <-targets:
		t.Fatalf("unexpected extra target %q", targetID)
	default:
	}
}

func TestRoomDetailHandler_GetStatusReturnsCoordinatorSummary(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"human-a",
		"recipient":"gemini-a",
		"body":"Please review this",
		"reply_expected":true
	}`))
	postRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/status?workspace_id=ws1&only=reply", nil)
	statusRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(statusRR, statusReq)
	if statusRR.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", statusRR.Code, statusRR.Body.String())
	}
	body := decodeResponseBody(t, statusRR)
	action, _ := body["actionable_backlog"].(map[string]any)
	if got := int(action["pending_replies"].(float64)); got != 1 {
		t.Fatalf("pending_replies=%d want 1", got)
	}
	if got := int(action["participants_with_pending"].(float64)); got != 1 {
		t.Fatalf("participants_with_pending=%d want 1", got)
	}
}

func TestRoomDetailHandler_GetStatusReturnsPersistedLoopState(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	loopStore, err := coordination.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open coordination store: %v", err)
	}
	defer loopStore.Close()
	lastTick := time.Now().UTC().Add(-2 * time.Minute)
	cursorAt := lastTick.Add(time.Minute)
	observedAt := lastTick.Add(90 * time.Second)
	_, err = loopStore.UpsertRoomLoop(context.Background(), coordination.RoomLoop{
		WorkspaceID:             "ws1",
		RoomID:                  "alpha",
		Enabled:                 false,
		ManagedBy:               "foxctl.room.loop/test",
		LastTickAt:              &lastTick,
		DeliveryLeaseName:       "room-loop:ws1:alpha:delivery",
		DeliveryOwnerID:         "owner-a",
		DeliveryCursorMessageID: "01TESTCURSOR",
		DeliveryCursorAt:        &cursorAt,
		LastDeliveryTrace: &coordination.RoomLoopDeliveryTrace{
			WorkspaceID:             "ws1",
			RoomID:                  "alpha",
			MessageID:               "msg-9",
			Recipient:               "gemini-a",
			DeliveryLeaseName:       "room-loop:ws1:alpha:delivery",
			DeliveryOwnerID:         "owner-a",
			RelayBackend:            "auto",
			ChosenActorID:           "gemini-a",
			ChosenMuxBackend:        "tmux",
			ChosenMuxSession:        "room-alpha",
			ChosenMuxPaneID:         "%9",
			ChosenTransportEndpoint: "/tmp/gemini-a.sock",
			ChosenTransportKind:     "pane_socket",
			ChosenSubmitMode:        "composer_ctrl_enter",
			FallbackAttempted:       true,
			DeliveredCount:          1,
			Outcome:                 "delivered",
			CursorBeforeMessageID:   "msg-8",
			CursorAfterMessageID:    "msg-9",
			CursorAdvanced:          true,
			ObservedAt:              observedAt,
		},
		PulseInterval:                45 * time.Minute,
		ReplyStaleAfter:              90 * time.Minute,
		TaskStaleAfter:               6 * time.Hour,
		MinPulseFloor:                24 * time.Hour,
		InterruptAttemptLimit:        3,
		ReminderBackoffCap:           5,
		CoordinatorPulseEnabled:      false,
		CoordinatorEscalationEnabled: false,
	})
	if err != nil {
		t.Fatalf("upsert room loop: %v", err)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/status?workspace_id=ws1", nil)
	statusRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(statusRR, statusReq)
	if statusRR.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", statusRR.Code, statusRR.Body.String())
	}
	body := decodeResponseBody(t, statusRR)
	loop := body["loop"].(map[string]any)
	if got := loop["enabled"].(bool); got {
		t.Fatalf("enabled=%v want false", got)
	}
	if got := strings.TrimSpace(loop["managed_by"].(string)); got != "foxctl.room.loop/test" {
		t.Fatalf("managed_by=%q want foxctl.room.loop/test", got)
	}
	if got := strings.TrimSpace(loop["pulse_interval"].(string)); got != "45m0s" {
		t.Fatalf("pulse_interval=%q want 45m0s", got)
	}
	if got := strings.TrimSpace(loop["delivery_owner_id"].(string)); got != "owner-a" {
		t.Fatalf("delivery_owner_id=%q want owner-a", got)
	}
	if got := strings.TrimSpace(loop["delivery_cursor_message_id"].(string)); got != "01TESTCURSOR" {
		t.Fatalf("delivery_cursor_message_id=%q want 01TESTCURSOR", got)
	}
	trace, ok := loop["last_delivery_trace"].(map[string]any)
	if !ok {
		t.Fatalf("last_delivery_trace type=%T want map[string]any", loop["last_delivery_trace"])
	}
	if got := strings.TrimSpace(fmt.Sprint(trace["message_id"])); got != "msg-9" {
		t.Fatalf("last_delivery_trace.message_id=%q want msg-9", got)
	}
	if got := fmt.Sprint(trace["fallback_attempted"]); got != "true" {
		t.Fatalf("last_delivery_trace.fallback_attempted=%q want true", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(trace["chosen_transport_kind"])); got != "pane_socket" {
		t.Fatalf("last_delivery_trace.chosen_transport_kind=%q want pane_socket", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(trace["cursor_after_message_id"])); got != "msg-9" {
		t.Fatalf("last_delivery_trace.cursor_after_message_id=%q want msg-9", got)
	}
	if got := loop["coordinator_pulse_enabled"].(bool); got {
		t.Fatalf("coordinator_pulse_enabled=%v want false", got)
	}
	if _, ok := loop["last_tick_at"].(string); !ok {
		t.Fatalf("last_tick_at type=%T want string", loop["last_tick_at"])
	}
}

func TestRequireActiveRoomLoopAPIRequiresDeliveryOwner(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	store, err := coordination.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open coordination store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	_, err = store.UpsertRoomLoop(context.Background(), coordination.RoomLoop{
		WorkspaceID:             "ws1",
		RoomID:                  "alpha",
		Enabled:                 true,
		ManagedBy:               "foxctl.room.loop/test",
		LastTickAt:              &now,
		PulseInterval:           45 * time.Minute,
		ReplyStaleAfter:         90 * time.Minute,
		TaskStaleAfter:          6 * time.Hour,
		MinPulseFloor:           24 * time.Hour,
		CoordinatorPulseEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert room loop: %v", err)
	}
	err = requireActiveRoomLoopAPI(context.Background(), store, "ws1", "alpha", now)
	if err == nil || !strings.Contains(err.Error(), "no active delivery owner") {
		t.Fatalf("requireActiveRoomLoopAPI err=%v want missing owner error", err)
	}

	leaseName := "room-loop:ws1:alpha:delivery"
	acquired, err := store.TryAcquireLease(context.Background(), leaseName, "owner-a", time.Minute)
	if err != nil {
		t.Fatalf("TryAcquireLease: %v", err)
	}
	if !acquired {
		t.Fatal("expected lease acquisition")
	}
	_, err = store.UpsertRoomLoop(context.Background(), coordination.RoomLoop{
		WorkspaceID:             "ws1",
		RoomID:                  "alpha",
		Enabled:                 true,
		ManagedBy:               "foxctl.room.loop/test",
		LastTickAt:              &now,
		DeliveryLeaseName:       leaseName,
		DeliveryOwnerID:         "owner-a",
		PulseInterval:           45 * time.Minute,
		ReplyStaleAfter:         90 * time.Minute,
		TaskStaleAfter:          6 * time.Hour,
		MinPulseFloor:           24 * time.Hour,
		CoordinatorPulseEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert room loop with owner: %v", err)
	}
	if err := requireActiveRoomLoopAPI(context.Background(), store, "ws1", "alpha", now); err != nil {
		t.Fatalf("requireActiveRoomLoopAPI with owner: %v", err)
	}
}

func TestRoomDetailHandler_GetInboxReturnsActorScopedEntries(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"human-a",
		"recipient":"gemini-a",
		"body":"Please ack this",
		"ack_required":true
	}`))
	postRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}

	inboxReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/inbox?workspace_id=ws1&actor_id=gemini-a", nil)
	inboxRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(inboxRR, inboxReq)
	if inboxRR.Code != http.StatusOK {
		t.Fatalf("inbox status=%d body=%s", inboxRR.Code, inboxRR.Body.String())
	}
	body := decodeResponseBody(t, inboxRR)
	if got := int(body["count"].(float64)); got != 1 {
		t.Fatalf("count=%d want 1", got)
	}
	room, ok := body["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T", body["room"])
	}
	for _, key := range []string{"task_ids", "members", "participants"} {
		if _, has := room[key]; has {
			t.Fatalf("default inbox response should omit %q", key)
		}
	}
	entries, _ := body["entries"].([]any)
	entry := entries[0].(map[string]any)
	if got := strings.TrimSpace(entry["recipient"].(string)); got != "gemini-a" {
		t.Fatalf("recipient=%q want gemini-a", got)
	}

	inboxFullReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/inbox?workspace_id=ws1&actor_id=gemini-a&full_room=1", nil)
	inboxFullRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(inboxFullRR, inboxFullReq)
	if inboxFullRR.Code != http.StatusOK {
		t.Fatalf("inbox full status=%d body=%s", inboxFullRR.Code, inboxFullRR.Body.String())
	}
	fullBody := decodeResponseBody(t, inboxFullRR)
	fullRoom, ok := fullBody["room"].(map[string]any)
	if !ok {
		t.Fatalf("full room type=%T", fullBody["room"])
	}
	if _, has := fullRoom["members"]; !has {
		t.Fatalf("full_room=1 should include members")
	}
}

func TestRoomDetailHandler_CoordinatorSetTransfersRole(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	setReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/coordinator", strings.NewReader(`{
		"workspace_id":"ws1",
		"actor_id":"human-a",
		"target_id":"gemini-a",
		"note":"handoff"
	}`))
	setRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(setRR, setReq)
	if setRR.Code != http.StatusOK {
		t.Fatalf("set status=%d body=%s", setRR.Code, setRR.Body.String())
	}
	body := decodeResponseBody(t, setRR)
	if got := strings.TrimSpace(body["coordinator"].(string)); got != "gemini-a" {
		t.Fatalf("coordinator=%q want gemini-a", got)
	}
	room, _ := body["room"].(map[string]any)
	members, _ := room["members"].([]any)
	found := false
	for _, raw := range members {
		member := raw.(map[string]any)
		if member["actor_id"] == "gemini-a" && member["role"] == "coordinator" {
			found = true
		}
	}
	if !found {
		t.Fatalf("members=%v want gemini-a coordinator", members)
	}

	msgReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/messages?workspace_id=ws1&limit=10", nil)
	msgRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(msgRR, msgReq)
	if msgRR.Code != http.StatusOK {
		t.Fatalf("messages status=%d body=%s", msgRR.Code, msgRR.Body.String())
	}
	msgBody := decodeResponseBody(t, msgRR)
	messages, _ := msgBody["messages"].([]any)
	if got := strings.TrimSpace(messages[0].(map[string]any)["kind"].(string)); got != "lead_change" {
		t.Fatalf("message kind=%q want lead_change", got)
	}
}

func TestRoomDetailHandler_GetLoopReturnsPersistedState(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	loopStore, err := coordination.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open coordination store: %v", err)
	}
	defer loopStore.Close()
	lastTick := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
	_, err = loopStore.UpsertRoomLoop(context.Background(), coordination.RoomLoop{
		WorkspaceID:             "ws1",
		RoomID:                  "alpha",
		Enabled:                 true,
		ManagedBy:               "foxctl.room.loop/test",
		LastTickAt:              &lastTick,
		PulseInterval:           45 * time.Minute,
		ReplyStaleAfter:         90 * time.Minute,
		TaskStaleAfter:          6 * time.Hour,
		MinPulseFloor:           24 * time.Hour,
		CoordinatorPulseEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert room loop: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/loop?workspace_id=ws1&actor_id=gemini-a", nil)
	rr := httptest.NewRecorder()
	roomHandler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	loop := body["loop"].(map[string]any)
	if got := strings.TrimSpace(loop["managed_by"].(string)); got != "foxctl.room.loop/test" {
		t.Fatalf("managed_by=%q want foxctl.room.loop/test", got)
	}
	if got := strings.TrimSpace(loop["pulse_interval"].(string)); got != "45m0s" {
		t.Fatalf("pulse_interval=%q want 45m0s", got)
	}
	if got := strings.TrimSpace(loop["reply_stale_after"].(string)); got != "1h30m0s" {
		t.Fatalf("reply_stale_after=%q want 1h30m0s", got)
	}
	if got := strings.TrimSpace(loop["task_stale_after"].(string)); got != "6h0m0s" {
		t.Fatalf("task_stale_after=%q want 6h0m0s", got)
	}
	if got := strings.TrimSpace(loop["min_pulse_floor"].(string)); got != "24h0m0s" {
		t.Fatalf("min_pulse_floor=%q want 24h0m0s", got)
	}
	if _, ok := loop["last_tick_at"].(string); !ok {
		t.Fatalf("last_tick_at type=%T want string", loop["last_tick_at"])
	}
}

func TestRoomDetailHandler_PatchLoopRequiresCoordinator(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/rooms/alpha/loop", strings.NewReader(`{
		"workspace_id":"ws1",
		"actor_id":"gemini-a",
		"pulse_interval":"15m"
	}`))
	rr := httptest.NewRecorder()
	roomHandler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomDetailHandler_PatchLoopAllowsLocalDevSuperuser(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/rooms/alpha/loop", strings.NewReader(`{
		"workspace_id":"ws1",
		"actor_id":"dev-local-user",
		"pulse_interval":"15m"
	}`))
	rr := httptest.NewRecorder()
	roomHandler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomDetailHandler_PatchLoopPersistsPolicy(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/rooms/alpha/loop", strings.NewReader(`{
		"workspace_id":"ws1",
		"actor_id":"human-a",
		"enabled": true,
		"pulse_interval":"15m",
		"reply_stale_after":"20m",
		"task_stale_after":"45m",
		"coordinator_pulse_enabled": false
	}`))
	patchRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(patchRR, patchReq)
	if patchRR.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patchRR.Code, patchRR.Body.String())
	}
	body := decodeResponseBody(t, patchRR)
	loop := body["loop"].(map[string]any)
	if got := strings.TrimSpace(loop["pulse_interval"].(string)); got != "15m0s" {
		t.Fatalf("pulse_interval=%q want 15m0s", got)
	}
	if got := strings.TrimSpace(loop["reply_stale_after"].(string)); got != "20m0s" {
		t.Fatalf("reply_stale_after=%q want 20m0s", got)
	}
	if got := strings.TrimSpace(loop["task_stale_after"].(string)); got != "45m0s" {
		t.Fatalf("task_stale_after=%q want 45m0s", got)
	}
	if got := loop["coordinator_pulse_enabled"].(bool); got {
		t.Fatalf("coordinator_pulse_enabled=%v want false", got)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/loop?workspace_id=ws1&actor_id=human-a", nil)
	getRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	getBody := decodeResponseBody(t, getRR)
	gotLoop := getBody["loop"].(map[string]any)
	if got := strings.TrimSpace(gotLoop["pulse_interval"].(string)); got != "15m0s" {
		t.Fatalf("persisted pulse_interval=%q want 15m0s", got)
	}
}

func TestRoomDetailHandler_PatchLoopPublishesRoomLoopUpdatedEvent(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	pub := &testRoomEventPublisher{}
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), pub)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/rooms/alpha/loop", strings.NewReader(`{
		"workspace_id":"ws1",
		"actor_id":"human-a",
		"pulse_interval":"15m"
	}`))
	patchRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(patchRR, patchReq)
	if patchRR.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patchRR.Code, patchRR.Body.String())
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	found := false
	for _, typ := range pub.types {
		if typ != "room.loop.updated" {
			continue
		}
		found = true
	}
	if !found {
		t.Fatalf("published types=%v want room.loop.updated", pub.types)
	}
	if len(pub.invalidations) == 0 {
		t.Fatal("expected at least one invalidation payload")
	}
	last := pub.invalidations[len(pub.invalidations)-1]
	if got := strings.TrimSpace(last.Mutation); got != "loop" {
		t.Fatalf("mutation=%q want loop", got)
	}
	if got := strings.TrimSpace(last.Action); got != "patch" {
		t.Fatalf("action=%q want patch", got)
	}
}

func TestRoomDetailHandler_TaskActionPublishesRoomTaskUpdatedEvent(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	pub := &testRoomEventPublisher{}
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), pub)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	taskStore, err := taskstore.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open task store: %v", err)
	}
	defer taskStore.Close()
	task, err := taskStore.Add(context.Background(), taskstore.Task{
		WorkspaceID: ws.CanonicalID("ws1"),
		Title:       "Backend task",
		Status:      taskstore.StatusPending,
	})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}

	claimReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/tasks/"+task.ID+"/claim", strings.NewReader(`{
		"workspace_id":"ws1",
		"actor_id":"gemini-a"
	}`))
	claimRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(claimRR, claimReq)
	if claimRR.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimRR.Code, claimRR.Body.String())
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	found := false
	for _, typ := range pub.types {
		if typ == "room.task.updated" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("published types=%v want room.task.updated", pub.types)
	}
	if len(pub.invalidations) == 0 {
		t.Fatal("expected invalidation payloads")
	}
	last := pub.invalidations[len(pub.invalidations)-1]
	if got := strings.TrimSpace(last.Mutation); got != "task" {
		t.Fatalf("mutation=%q want task", got)
	}
	if got := strings.TrimSpace(last.TaskID); got != task.ID {
		t.Fatalf("task_id=%q want %q", got, task.ID)
	}
	if got := strings.TrimSpace(last.Action); got != "claim" {
		t.Fatalf("action=%q want claim", got)
	}
}

func TestRoomDetailHandler_MessageAckUpdatesStatus(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(`{
		"workspace_id":"ws1",
		"sender":"human-a",
		"recipient":"gemini-a",
		"body":"Please ack this",
		"ack_required":true
	}`))
	postRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	postBody := decodeResponseBody(t, postRR)
	msgID := strings.TrimSpace(postBody["id"].(string))

	ackReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages/"+msgID+"/ack", strings.NewReader(`{
		"workspace_id":"ws1",
		"actor_id":"gemini-a"
	}`))
	ackRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(ackRR, ackReq)
	if ackRR.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", ackRR.Code, ackRR.Body.String())
	}

	msgReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/messages?workspace_id=ws1&limit=10", nil)
	msgRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(msgRR, msgReq)
	if msgRR.Code != http.StatusOK {
		t.Fatalf("messages status=%d body=%s", msgRR.Code, msgRR.Body.String())
	}
	msgBody := decodeResponseBody(t, msgRR)
	messages, _ := msgBody["messages"].([]any)
	msg := messages[0].(map[string]any)
	if got := strings.TrimSpace(msg["status"].(string)); got != "acked" {
		t.Fatalf("message status=%q want acked", got)
	}
}

func TestRoomDetailHandler_GetControlSnapshotIncludesLoopHealthAndLinkedCards(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)
	seedHandler := OrchestrationSeedCardsHandler(cfg, zerolog.Nop())

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{
				"actor_id":"gemini-a",
				"role":"reviewer",
				"backend":"tmux",
				"session":"146",
				"pane_id":"%159",
				"transport_endpoint":"/tmp/gemini-a.sock",
				"transport_kind":"pane_socket",
				"delivery_binding":{
					"mux_backend":"tmux",
					"mux_session":"146",
					"mux_pane_id":"%159",
					"transport_endpoint":"/tmp/gemini-a.sock",
					"transport_kind":"pane_socket",
					"submit_mode":"composer_ctrl_enter",
					"health":"ready"
				}
			}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	taskStore, err := taskstore.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open task store: %v", err)
	}
	defer taskStore.Close()
	task, err := taskStore.Add(context.Background(), taskstore.Task{
		WorkspaceID: ws.CanonicalID("ws1"),
		Title:       "Linked task",
		Status:      taskstore.StatusPending,
	})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}

	seedReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/seed-cards", strings.NewReader(fmt.Sprintf(`{
		"request_id":"req-room-snapshot-seed-001",
		"workspace_id":"ws1",
		"cards":[{"issue_id":"%s","issue_identifier":"ROOM-LINK-1","title":"Linked room card"}]
	}`, task.ID)))
	seedRR := httptest.NewRecorder()
	seedHandler.ServeHTTP(seedRR, seedReq)
	if seedRR.Code != http.StatusOK {
		t.Fatalf("seed status=%d body=%s", seedRR.Code, seedRR.Body.String())
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(fmt.Sprintf(`{
		"workspace_id":"ws1",
		"sender":"human-a",
		"recipient":"gemini-a",
		"task_id":"%s",
		"body":"Please review this",
		"reply_expected":true
	}`, task.ID)))
	postRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}

	snapshotReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/control-snapshot?workspace_id=ws1&actor_id=gemini-a", nil)
	snapshotRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(snapshotRR, snapshotReq)
	if snapshotRR.Code != http.StatusOK {
		t.Fatalf("snapshot status=%d body=%s", snapshotRR.Code, snapshotRR.Body.String())
	}
	body := decodeResponseBody(t, snapshotRR)

	loopHealth, ok := body["loop_health"].(map[string]any)
	if !ok {
		t.Fatalf("loop_health type=%T want map[string]any", body["loop_health"])
	}
	if got := strings.TrimSpace(fmt.Sprint(loopHealth["status"])); got != "active" {
		t.Fatalf("loop_health.status=%q want active", got)
	}

	participants, ok := body["participants"].([]any)
	if !ok {
		t.Fatalf("participants type=%T want []any", body["participants"])
	}
	var member map[string]any
	for _, raw := range participants {
		participant, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(participant["actor_id"])) == "gemini-a" {
			member = participant
			break
		}
	}
	if member == nil {
		t.Fatalf("participants=%v want gemini-a", participants)
	}
	if got := strings.TrimSpace(fmt.Sprint(member["transport_status"])); got != "ready" {
		t.Fatalf("transport_status=%q want ready", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(member["runtime_binding_status"])); got != "ready" {
		t.Fatalf("runtime_binding_status=%q want ready", got)
	}
	for _, key := range []string{"backend", "session", "pane_id", "transport_endpoint", "transport_kind", "delivery_binding"} {
		if _, ok := member[key]; ok {
			t.Fatalf("participant contains legacy transport field %q: %#v", key, member)
		}
	}
	transport, ok := member["transport"].(map[string]any)
	if !ok {
		t.Fatalf("transport type=%T want map[string]any", member["transport"])
	}
	if got := strings.TrimSpace(fmt.Sprint(transport["transport_endpoint"])); got != "/tmp/gemini-a.sock" {
		t.Fatalf("transport.transport_endpoint=%q want /tmp/gemini-a.sock", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(transport["mux_backend"])); got != "tmux" {
		t.Fatalf("transport.mux_backend=%q want tmux", got)
	}

	if got := strings.TrimSpace(fmt.Sprint(body["task_card_link"])); got != "issue_id_equals_task_id" {
		t.Fatalf("task_card_link=%q want issue_id_equals_task_id", got)
	}
	rawCards, ok := body["linked_orchestration_cards"].([]any)
	if !ok || len(rawCards) == 0 {
		t.Fatalf("linked_orchestration_cards=%T %#v want at least one card", body["linked_orchestration_cards"], body["linked_orchestration_cards"])
	}
	card, _ := rawCards[0].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(card["issue_id"])); got != task.ID {
		t.Fatalf("issue_id=%q want %q", got, task.ID)
	}
	if got := strings.TrimSpace(fmt.Sprint(card["linked_task_id"])); got != task.ID {
		t.Fatalf("linked_task_id=%q want %q", got, task.ID)
	}
}

func TestRoomDetailHandler_GetTasksReturnsRoomLinkedTasks(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha"
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	taskStore, err := taskstore.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open task store: %v", err)
	}
	defer taskStore.Close()
	task, err := taskStore.Add(context.Background(), taskstore.Task{
		WorkspaceID: ws.CanonicalID("ws1"),
		Title:       "Backend task",
		Status:      taskstore.StatusPending,
	})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(fmt.Sprintf(`{
		"workspace_id":"ws1",
		"sender":"human-a",
		"task_id":"%s",
		"body":"task linked"
	}`, task.ID)))
	postRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}

	tasksReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/tasks?workspace_id=ws1", nil)
	tasksRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(tasksRR, tasksReq)
	if tasksRR.Code != http.StatusOK {
		t.Fatalf("tasks status=%d body=%s", tasksRR.Code, tasksRR.Body.String())
	}
	body := decodeResponseBody(t, tasksRR)
	if got := int(body["count"].(float64)); got != 1 {
		t.Fatalf("count=%d want 1", got)
	}
	tasks, _ := body["tasks"].([]any)
	first := tasks[0].(map[string]any)
	if got := strings.TrimSpace(first["id"].(string)); got != task.ID {
		t.Fatalf("task id=%q want %q", got, task.ID)
	}
}

func TestRoomDetailHandler_TaskClaimActionUpdatesTask(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, "ws1", "alpha")

	taskStore, err := taskstore.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open task store: %v", err)
	}
	defer taskStore.Close()
	task, err := taskStore.Add(context.Background(), taskstore.Task{
		WorkspaceID: ws.CanonicalID("ws1"),
		Title:       "Backend task",
		Status:      taskstore.StatusPending,
	})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/messages", strings.NewReader(fmt.Sprintf(`{
		"workspace_id":"ws1",
		"sender":"human-a",
		"task_id":"%s",
		"body":"task linked"
	}`, task.ID)))
	postRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}

	claimReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/tasks/"+task.ID+"/claim", strings.NewReader(`{
		"workspace_id":"ws1",
		"actor_id":"gemini-a"
	}`))
	claimRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(claimRR, claimReq)
	if claimRR.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimRR.Code, claimRR.Body.String())
	}
	body := decodeResponseBody(t, claimRR)
	taskMap := body["task"].(map[string]any)
	if got := strings.TrimSpace(taskMap["status"].(string)); got != taskstore.StatusInProgress {
		t.Fatalf("status=%q want %q", got, taskstore.StatusInProgress)
	}
	if got := strings.TrimSpace(taskMap["owner_actor_id"].(string)); got != "gemini-a" {
		t.Fatalf("owner_actor_id=%q want gemini-a", got)
	}
}

func TestRoomDetailHandler_PostTaskUsesExplicitMilestoneSelection(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"},
			{"actor_id":"gemini-a","role":"reviewer"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	boardStore, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer boardStore.Close()

	epic := &agent.BoardMessage{
		WorkspaceID: "ws1",
		Stream:      agent.RoomStreamName("alpha"),
		Sender:      "human-a",
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindEpic,
		Subject:     "Epic: Runtime hardening",
		Body:        "Goal: ship room runtime hardening",
		CreatedAt:   time.Now().UTC().Add(-3 * time.Minute),
	}
	if err := boardStore.SendMessage(context.Background(), epic); err != nil {
		t.Fatalf("send epic: %v", err)
	}
	finalize := &agent.BoardMessage{
		WorkspaceID:      "ws1",
		RelatedMessageID: epic.ID,
		Stream:           agent.RoomStreamName("alpha"),
		Sender:           "human-a",
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindEpicFinalize,
		Subject:          "Epic Finalized: Runtime hardening",
		Body:             "Summary: ready for milestones",
		CreatedAt:        time.Now().UTC().Add(-2 * time.Minute),
	}
	if err := boardStore.SendMessage(context.Background(), finalize); err != nil {
		t.Fatalf("send epic finalize: %v", err)
	}
	milestone := &agent.BoardMessage{
		WorkspaceID:      "ws1",
		RelatedMessageID: epic.ID,
		Stream:           agent.RoomStreamName("alpha"),
		Sender:           "human-a",
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindMilestone,
		Subject:          "Milestone: Foundation",
		Body:             fmt.Sprintf("EpicID: %s\nObjective: ship the first slice", epic.ID),
		CreatedAt:        time.Now().UTC().Add(-1 * time.Minute),
	}
	if err := boardStore.SendMessage(context.Background(), milestone); err != nil {
		t.Fatalf("send milestone: %v", err)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/tasks", strings.NewReader(fmt.Sprintf(`{
		"workspace_id":"ws1",
		"actor_id":"human-a",
		"title":"Milestone task",
		"description":"Explicit lane selection",
		"milestone_id":"%s"
	}`, milestone.ID)))
	postRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	body := decodeResponseBody(t, postRR)
	task, ok := body["task"].(map[string]any)
	if !ok {
		t.Fatalf("task type=%T want map[string]any", body["task"])
	}
	if got := strings.TrimSpace(fmt.Sprint(task["epic_id"])); got != epic.ID {
		t.Fatalf("epic_id=%q want %q", got, epic.ID)
	}
	if got := strings.TrimSpace(fmt.Sprint(task["milestone_id"])); got != milestone.ID {
		t.Fatalf("milestone_id=%q want %q", got, milestone.ID)
	}
	message, ok := body["message"].(map[string]any)
	if !ok {
		t.Fatalf("message type=%T want map[string]any", body["message"])
	}
	if got := strings.TrimSpace(fmt.Sprint(message["recipient"])); got != "human-a" {
		t.Fatalf("recipient=%q want human-a", got)
	}

	tasksReq := httptest.NewRequest(http.MethodGet, "/api/rooms/alpha/tasks?workspace_id=ws1", nil)
	tasksRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(tasksRR, tasksReq)
	if tasksRR.Code != http.StatusOK {
		t.Fatalf("tasks status=%d body=%s", tasksRR.Code, tasksRR.Body.String())
	}
	tasksBody := decodeResponseBody(t, tasksRR)
	tasksList, _ := tasksBody["tasks"].([]any)
	if len(tasksList) != 1 {
		t.Fatalf("tasks=%d want 1", len(tasksList))
	}
	first := tasksList[0].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(first["milestone_id"])); got != milestone.ID {
		t.Fatalf("tasks milestone_id=%q want %q", got, milestone.ID)
	}
}

func TestRoomDetailHandler_PostTaskRejectsUnknownMilestoneSelection(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	roomHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha",
		"members":[
			{"actor_id":"human-a","role":"coordinator"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/tasks", strings.NewReader(`{
		"workspace_id":"ws1",
		"actor_id":"human-a",
		"title":"Bad lane",
		"milestone_id":"mile-missing"
	}`))
	postRR := httptest.NewRecorder()
	roomHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusNotFound {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	if !strings.Contains(postRR.Body.String(), "room task milestone not found") {
		t.Fatalf("body=%s want missing milestone error", postRR.Body.String())
	}
}

func TestRoomWorkspaceIDCanonicalizesPathSelectors(t *testing.T) {
	root := t.TempDir()
	req := httptest.NewRequest(http.MethodGet, "/api/rooms?workspace_id="+root+"/.", nil)

	if got := roomWorkspaceID(req); got != root {
		t.Fatalf("roomWorkspaceID(path)=%q want %q", got, root)
	}
}

func TestRoomWorkspaceIDPreservesOpaqueIDs(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/rooms?workspace_id=ws1", nil)

	if got := roomWorkspaceID(req); got != "ws1" {
		t.Fatalf("roomWorkspaceID(id)=%q want ws1", got)
	}
}

// setupRoomAgileTest is a test helper that creates a room, epic, finalized epic,
// milestone, and story. It returns the handler, config, and the IDs of the
// created entities.
func setupRoomAgileTest(t *testing.T) (handler http.Handler, cfg config.Config, epicID string, milestoneID string, storyID string) {
	t.Helper()
	cfg = orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	handler = RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"members":[{"actor_id":"human-a","role":"coordinator"}]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	store, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer store.Close()

	epic := agent.BoardMessage{
		WorkspaceID: "ws1",
		Stream:      agent.RoomStreamName("alpha"),
		Sender:      "human-a",
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindEpic,
		Subject:     "Epic: Test",
		Body:        "Goal: test",
	}
	if err := store.SendMessage(context.Background(), &epic); err != nil {
		t.Fatalf("send epic: %v", err)
	}
	final := agent.BoardMessage{
		WorkspaceID:      "ws1",
		Stream:           agent.RoomStreamName("alpha"),
		Sender:           "human-a",
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindEpicFinalize,
		RelatedMessageID: epic.ID,
		Subject:          "Epic Finalized: Test",
		Body:             "Summary: ready",
	}
	if err := store.SendMessage(context.Background(), &final); err != nil {
		t.Fatalf("send finalize: %v", err)
	}
	mile := agent.BoardMessage{
		WorkspaceID:      "ws1",
		Stream:           agent.RoomStreamName("alpha"),
		Sender:           "human-a",
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindMilestone,
		RelatedMessageID: epic.ID,
		Subject:          "Milestone: Backend",
		Body:             "EpicID: " + epic.ID + "\nObjective: test",
	}
	if err := store.SendMessage(context.Background(), &mile); err != nil {
		t.Fatalf("send milestone: %v", err)
	}
	story := agent.BoardMessage{
		WorkspaceID:      "ws1",
		Stream:           agent.RoomStreamName("alpha"),
		Sender:           "human-a",
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStory,
		RelatedMessageID: mile.ID,
		Subject:          "Story: Add endpoint",
		Body:             "Expose reads",
	}
	if err := store.SendMessage(context.Background(), &story); err != nil {
		t.Fatalf("send story: %v", err)
	}

	return handler, cfg, epic.ID, mile.ID, story.ID
}

func TestRoomDetailHandler_RoomAgileStoryState(t *testing.T) {
	h, _, _, _, storyID := setupRoomAgileTest(t)

	// Test: transition to in_progress
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_state",
		"story_id":"%s",
		"state":"in_progress",
		"actor":"human-a"
	}`, storyID)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("story_state in_progress status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeRoomAgileTestResponse[roomAgileStoryStateResult](t, rr, "story_state")
	data := body.Result.Data
	if got := data.Message.Kind; got != "story_state" {
		t.Fatalf("message.kind=%v want story_state", got)
	}
	if got := data.Story.Status; got != "in_progress" {
		t.Fatalf("story.status=%v want in_progress", got)
	}
	if got := data.Story.Previous; got != "accepted" {
		t.Fatalf("story.previous=%v want accepted", got)
	}

	// Test: transition to in_review
	req2 := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_state",
		"story_id":"%s",
		"state":"in_review",
		"actor":"human-a"
	}`, storyID)))
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("story_state in_review status=%d body=%s", rr2.Code, rr2.Body.String())
	}
	body2 := decodeRoomAgileTestResponse[roomAgileStoryStateResult](t, rr2, "story_state")
	story2 := body2.Result.Data.Story
	if got := story2.Status; got != "in_review" {
		t.Fatalf("story.status=%v want in_review", got)
	}
	if got := story2.Previous; got != "in_progress" {
		t.Fatalf("story.previous=%v want in_progress", got)
	}

	// Test: invalid state returns 400
	reqBad := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_state",
		"story_id":"%s",
		"state":"unknown"
	}`, storyID)))
	rrBad := httptest.NewRecorder()
	h.ServeHTTP(rrBad, reqBad)
	if rrBad.Code != http.StatusBadRequest {
		t.Fatalf("invalid state status=%d body=%s", rrBad.Code, rrBad.Body.String())
	}

	// Test: missing story_id returns 400
	reqNoID := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(`{
		"workspace":"ws1",
		"action":"story_state",
		"state":"in_progress"
	}`))
	rrNoID := httptest.NewRecorder()
	h.ServeHTTP(rrNoID, reqNoID)
	if rrNoID.Code != http.StatusBadRequest {
		t.Fatalf("missing story_id status=%d body=%s", rrNoID.Code, rrNoID.Body.String())
	}

	// Test: nonexistent story_id returns 404
	reqNoStory := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(`{
		"workspace":"ws1",
		"action":"story_state",
		"story_id":"nonexistent",
		"state":"in_progress"
	}`))
	rrNoStory := httptest.NewRecorder()
	h.ServeHTTP(rrNoStory, reqNoStory)
	if rrNoStory.Code != http.StatusNotFound {
		t.Fatalf("nonexistent story status=%d body=%s", rrNoStory.Code, rrNoStory.Body.String())
	}
}

func TestRoomDetailHandler_RoomAgileStoryValidate(t *testing.T) {
	h, _, _, _, storyID := setupRoomAgileTest(t)

	// Move story to in_review first.
	stateReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_state",
		"story_id":"%s",
		"state":"in_review",
		"actor":"human-a"
	}`, storyID)))
	stateRR := httptest.NewRecorder()
	h.ServeHTTP(stateRR, stateReq)
	if stateRR.Code != http.StatusOK {
		t.Fatalf("story_state status=%d body=%s", stateRR.Code, stateRR.Body.String())
	}

	// Test: validate with full fields
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_validate",
		"story_id":"%s",
		"verdict":"pass",
		"validator_type":"human",
		"command":"make test",
		"artifact":"sha256:abc",
		"notes":"All tests pass",
		"actor":"human-a"
	}`, storyID)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("story_validate status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeRoomAgileTestResponse[roomAgileStoryValidationResult](t, rr, "story_validate")
	data := body.Result.Data
	if got := data.Message.Kind; got != "story_validation" {
		t.Fatalf("message.kind=%v want story_validation", got)
	}
	if got := data.Message.Subject; got != "Story Validation: pass" {
		t.Fatalf("message.subject=%v want Story Validation: pass", got)
	}
	msgBody := data.Message.Body
	if !strings.Contains(msgBody, "Verdict: pass") {
		t.Fatalf("message.body=%q want Verdict: pass", msgBody)
	}
	if !strings.Contains(msgBody, "ValidatorType: human") {
		t.Fatalf("message.body=%q want ValidatorType: human", msgBody)
	}
	if !strings.Contains(msgBody, "Command: make test") {
		t.Fatalf("message.body=%q want Command: make test", msgBody)
	}
	if !strings.Contains(msgBody, "Artifact: sha256:abc") {
		t.Fatalf("message.body=%q want Artifact: sha256:abc", msgBody)
	}
	if !strings.Contains(msgBody, "Notes: All tests pass") {
		t.Fatalf("message.body=%q want Notes: All tests pass", msgBody)
	}
	if got := data.StoryID; got != storyID {
		t.Fatalf("story_id=%v want %s", got, storyID)
	}

	// Verify the validation is visible via story_show
	showReq := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_show",
		"story_id":"%s"
	}`, storyID)))
	showRR := httptest.NewRecorder()
	h.ServeHTTP(showRR, showReq)
	if showRR.Code != http.StatusOK {
		t.Fatalf("story_show status=%d body=%s", showRR.Code, showRR.Body.String())
	}
	showBody := decodeRoomAgileTestResponse[roomAgileStoryResult](t, showRR, "story_show")
	showStory := showBody.Result.Data.Story
	if showStory == nil {
		t.Fatalf("story missing")
	}
	if got := showStory.Status; got != "in_review" {
		t.Fatalf("story.status after validate=%v want in_review (validation does not change state)", got)
	}
	if got := showStory.ValidationCount; got != 1 {
		t.Fatalf("story.validation_count=%d want 1", got)
	}
	if got := len(showStory.Validations); got != 1 {
		t.Fatalf("story.validations=%d want 1", got)
	}

	// Test: missing verdict returns 400
	reqNoVerdict := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_validate",
		"story_id":"%s",
		"validator_type":"human"
	}`, storyID)))
	rrNoVerdict := httptest.NewRecorder()
	h.ServeHTTP(rrNoVerdict, reqNoVerdict)
	if rrNoVerdict.Code != http.StatusBadRequest {
		t.Fatalf("missing verdict status=%d body=%s", rrNoVerdict.Code, rrNoVerdict.Body.String())
	}

	// Test: missing validator_type returns 400
	reqNoVal := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_validate",
		"story_id":"%s",
		"verdict":"pass"
	}`, storyID)))
	rrNoVal := httptest.NewRecorder()
	h.ServeHTTP(rrNoVal, reqNoVal)
	if rrNoVal.Code != http.StatusBadRequest {
		t.Fatalf("missing validator_type status=%d body=%s", rrNoVal.Code, rrNoVal.Body.String())
	}
}

func TestRoomDetailHandler_RoomAgileStoryPropose(t *testing.T) {
	h, _, _, milestoneID, _ := setupRoomAgileTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_propose",
		"milestone_id":"%s",
		"title":"New story",
		"goal":"Do the thing",
		"notes":"Optional context",
		"actor":"human-a"
	}`, milestoneID)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("story_propose status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeRoomAgileTestResponse[roomAgileStoryProposeResult](t, rr, "story_propose")
	message := body.Result.Data.Message
	if got := message.Kind; got != "story_proposal" {
		t.Fatalf("message.kind=%v want story_proposal", got)
	}
	if got := message.Subject; got != "Story Proposal: New story" {
		t.Fatalf("message.subject=%v want Story Proposal: New story", got)
	}
	if got := message.RelatedMessageID; got != milestoneID {
		t.Fatalf("message.related_message_id=%v want %s", got, milestoneID)
	}
	msgBody := message.Body
	if !strings.Contains(msgBody, "Title: New story") {
		t.Fatalf("message.body=%q want Title: New story", msgBody)
	}
	if !strings.Contains(msgBody, "Goal: Do the thing") {
		t.Fatalf("message.body=%q want Goal: Do the thing", msgBody)
	}
	if !strings.Contains(msgBody, "Notes: Optional context") {
		t.Fatalf("message.body=%q want Notes: Optional context", msgBody)
	}

	// Test: missing milestone_id returns 400
	reqNoMS := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(`{
		"workspace":"ws1",
		"action":"story_propose",
		"title":"Missing milestone"
	}`))
	rrNoMS := httptest.NewRecorder()
	h.ServeHTTP(rrNoMS, reqNoMS)
	if rrNoMS.Code != http.StatusBadRequest {
		t.Fatalf("missing milestone_id status=%d body=%s", rrNoMS.Code, rrNoMS.Body.String())
	}

	// Test: missing title returns 400
	reqNoTitle := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_propose",
		"milestone_id":"%s"
	}`, milestoneID)))
	rrNoTitle := httptest.NewRecorder()
	h.ServeHTTP(rrNoTitle, reqNoTitle)
	if rrNoTitle.Code != http.StatusBadRequest {
		t.Fatalf("missing title status=%d body=%s", rrNoTitle.Code, rrNoTitle.Body.String())
	}
}

func TestRoomDetailHandler_RoomAgileStoryAccept(t *testing.T) {
	h, cfg, _, milestoneID, _ := setupRoomAgileTest(t)

	// Create a proposal via direct store.SendMessage
	store, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer store.Close()

	proposal := &agent.BoardMessage{
		WorkspaceID:      "ws1",
		Stream:           agent.RoomStreamName("alpha"),
		Sender:           "human-a",
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStoryProposal,
		RelatedMessageID: milestoneID,
		Subject:          "Story Proposal: Proposed feature",
		Body:             "Title: Proposed feature\nGoal: Implement it",
		Status:           agent.BoardMessageStatusUnread,
		Priority:         agent.DefaultPriority,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.SendMessage(context.Background(), proposal); err != nil {
		t.Fatalf("send proposal: %v", err)
	}

	// Accept the proposal
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(fmt.Sprintf(`{
		"workspace":"ws1",
		"action":"story_accept",
		"proposal_id":"%s",
		"actor":"human-a"
	}`, proposal.ID)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("story_accept status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeRoomAgileTestResponse[roomAgileStoryAcceptResult](t, rr, "story_accept")
	data := body.Result.Data
	message := data.Message
	if got := message.Kind; got != "story" {
		t.Fatalf("message.kind=%v want story", got)
	}
	if got := message.Subject; got != "Story: Proposed feature" {
		t.Fatalf("message.subject=%v want Story: Proposed feature", got)
	}
	// The accepted story should point to the milestone (from proposal.related_message_id)
	if got := message.RelatedMessageID; got != milestoneID {
		t.Fatalf("message.related_message_id=%v want %s (milestone)", got, milestoneID)
	}
	if got := data.ProposalID; got != proposal.ID {
		t.Fatalf("proposal_id=%v want %s", got, proposal.ID)
	}

	// Test: missing proposal_id returns 400
	reqNoID := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(`{
		"workspace":"ws1",
		"action":"story_accept"
	}`))
	rrNoID := httptest.NewRecorder()
	h.ServeHTTP(rrNoID, reqNoID)
	if rrNoID.Code != http.StatusBadRequest {
		t.Fatalf("missing proposal_id status=%d body=%s", rrNoID.Code, rrNoID.Body.String())
	}

	// Test: nonexistent proposal_id returns 404
	reqNoProposal := httptest.NewRequest(http.MethodPost, "/api/rooms/alpha/agile", strings.NewReader(`{
		"workspace":"ws1",
		"action":"story_accept",
		"proposal_id":"nonexistent"
	}`))
	rrNoProposal := httptest.NewRecorder()
	h.ServeHTTP(rrNoProposal, reqNoProposal)
	if rrNoProposal.Code != http.StatusNotFound {
		t.Fatalf("nonexistent proposal status=%d body=%s", rrNoProposal.Code, rrNoProposal.Body.String())
	}
}
