package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
)

type testRoomEventPublisher struct {
	mu     sync.Mutex
	types  []string
	events []roomMessageEvent
}

func (p *testRoomEventPublisher) Publish(eventType string, data any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.types = append(p.types, eventType)
	if evt, ok := data.(roomMessageEvent); ok {
		p.events = append(p.events, evt)
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

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"ws1",
		"id":"alpha",
		"title":"Alpha Room",
		"description":"Primary coordination",
		"members":[
			{"actor_id":"actor:agent:a","role":"owner"},
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
		"members":[{"actor_id":"actor:agent:c","role":"owner"}]
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
	updatedMembers, ok := updatedRoom["members"].([]any)
	if !ok {
		t.Fatalf("patched room members type=%T want []any", updatedRoom["members"])
	}
	if len(updatedMembers) != 1 {
		t.Fatalf("patched room members=%d want 1", len(updatedMembers))
	}
}

func TestRoomDetailHandler_PostMessageDispatchesAgentReplies(t *testing.T) {
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
