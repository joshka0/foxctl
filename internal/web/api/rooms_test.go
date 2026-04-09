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
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/coordination"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
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

func TestRoomDetailHandler_ArchiveAndRestore(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	detailHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

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
	action, _ := body["action_required"].(map[string]any)
	if got := int(action["pending_replies"].(float64)); got != 1 {
		t.Fatalf("pending_replies=%d want 1", got)
	}
	if got := int(action["participants_with_pending"].(float64)); got != 1 {
		t.Fatalf("participants_with_pending=%d want 1", got)
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
		ManagedBy:               "agentctl.room.loop/test",
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
	if got := strings.TrimSpace(loop["managed_by"].(string)); got != "agentctl.room.loop/test" {
		t.Fatalf("managed_by=%q want agentctl.room.loop/test", got)
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
