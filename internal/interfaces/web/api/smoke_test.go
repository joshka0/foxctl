package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

const (
	roomSmokeWorkspacePath          = "/tmp/foxctl-room-smoke-workspace"
	orchestrationSmokeWorkspacePath = "/tmp/foxctl-orchestration-smoke-workspace"
)

func TestRoomsHandlers_Smoke(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	listHandler := RoomsListHandler(cfg, zerolog.Nop())
	detailHandler := RoomDetailHandler(cfg, zerolog.Nop(), nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{
		"workspace_id":"`+roomSmokeWorkspacePath+`",
		"id":"smoke-room",
		"title":"Smoke Room",
		"description":"room smoke test",
		"members":[
			{"actor_id":"actor:agent:alpha","role":"researcher"},
			{"actor_id":"actor:agent:beta","role":"coder"}
		]
	}`))
	createRR := httptest.NewRecorder()
	listHandler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create room status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	activateAPIRoomLoop(t, cfg, roomSmokeWorkspacePath, "smoke-room")

	listReq := httptest.NewRequest(http.MethodGet, "/api/rooms?workspace_id="+roomSmokeWorkspacePath+"&limit=10", nil)
	listRR := httptest.NewRecorder()
	listHandler.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list rooms status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	listBody := decodeResponseBody(t, listRR)
	rawRooms, ok := listBody["rooms"].([]any)
	if !ok || len(rawRooms) != 1 {
		t.Fatalf("rooms payload=%v want 1 room", listBody["rooms"])
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/rooms/smoke-room/messages", strings.NewReader(`{
		"workspace_id":"`+roomSmokeWorkspacePath+`",
		"sender":"human:gui",
		"body":"smoke room message"
	}`))
	postRR := httptest.NewRecorder()
	detailHandler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post room message status=%d body=%s", postRR.Code, postRR.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/rooms/smoke-room?workspace_id="+roomSmokeWorkspacePath, nil)
	getRR := httptest.NewRecorder()
	detailHandler.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get room status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	getBody := decodeResponseBody(t, getRR)
	room, ok := getBody["room"].(map[string]any)
	if !ok {
		t.Fatalf("room payload type=%T want map[string]any", getBody["room"])
	}
	if got := strings.TrimSpace(room["id"].(string)); got != "smoke-room" {
		t.Fatalf("room.id=%q want smoke-room", got)
	}
	if got := int(room["message_count"].(float64)); got != 1 {
		t.Fatalf("room.message_count=%d want 1", got)
	}

	msgReq := httptest.NewRequest(http.MethodGet, "/api/rooms/smoke-room/messages?workspace_id="+roomSmokeWorkspacePath+"&limit=10", nil)
	msgRR := httptest.NewRecorder()
	detailHandler.ServeHTTP(msgRR, msgReq)
	if msgRR.Code != http.StatusOK {
		t.Fatalf("get room messages status=%d body=%s", msgRR.Code, msgRR.Body.String())
	}
	msgBody := decodeResponseBody(t, msgRR)
	rawMessages, ok := msgBody["messages"].([]any)
	if !ok || len(rawMessages) != 1 {
		t.Fatalf("room messages payload=%v want 1 message", msgBody["messages"])
	}
	message, _ := rawMessages[0].(map[string]any)
	if got := strings.TrimSpace(message["body"].(string)); got != "smoke room message" {
		t.Fatalf("room message body=%q want smoke room message", got)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/rooms/smoke-room?workspace_id="+roomSmokeWorkspacePath, nil)
	deleteRR := httptest.NewRecorder()
	detailHandler.ServeHTTP(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("delete room status=%d body=%s", deleteRR.Code, deleteRR.Body.String())
	}

	verifyReq := httptest.NewRequest(http.MethodGet, "/api/rooms/smoke-room?workspace_id="+roomSmokeWorkspacePath, nil)
	verifyRR := httptest.NewRecorder()
	detailHandler.ServeHTTP(verifyRR, verifyReq)
	if verifyRR.Code != http.StatusNotFound {
		t.Fatalf("verify deleted room status=%d want %d body=%s", verifyRR.Code, http.StatusNotFound, verifyRR.Body.String())
	}
}

func TestOrchestrationBoardHandlers_Smoke(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	seedHandler := OrchestrationSeedCardsHandler(cfg, zerolog.Nop())
	boardHandler := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	cardHandler := OrchestrationBoardCardGetHandler(cfg, zerolog.Nop())
	actionHandler := OrchestrationCardActionHandler(cfg, zerolog.Nop())

	seedReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/seed-cards", bytes.NewBufferString(`{
		"request_id":"req-smoke-seed-001",
		"workspace_id":"`+orchestrationSmokeWorkspacePath+`",
		"cards":[
			{"issue_id":"smoke-issue-1","issue_identifier":"SMOKE-1","title":"Smoke card one"},
			{"issue_id":"smoke-issue-2","issue_identifier":"SMOKE-2","title":"Smoke card two"}
		]
	}`))
	seedRR := httptest.NewRecorder()
	seedHandler.ServeHTTP(seedRR, seedReq)
	if seedRR.Code != http.StatusOK {
		t.Fatalf("seed cards status=%d body=%s", seedRR.Code, seedRR.Body.String())
	}

	boardReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id="+orchestrationSmokeWorkspacePath+"&limit=10", nil)
	boardRR := httptest.NewRecorder()
	boardHandler.ServeHTTP(boardRR, boardReq)
	if boardRR.Code != http.StatusOK {
		t.Fatalf("board get status=%d body=%s", boardRR.Code, boardRR.Body.String())
	}
	if got := boardCardCountFromEnvelope(decodeResponseBody(t, boardRR)); got < 2 {
		t.Fatalf("board card count=%d want >= 2", got)
	}

	cardReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-card-get?workspace_id="+orchestrationSmokeWorkspacePath+"&issue_id=smoke-issue-1", nil)
	cardRR := httptest.NewRecorder()
	cardHandler.ServeHTTP(cardRR, cardReq)
	if cardRR.Code != http.StatusOK {
		t.Fatalf("card get status=%d body=%s", cardRR.Code, cardRR.Body.String())
	}
	cardBody := decodeResponseBody(t, cardRR)
	cardData, _ := cardBody["data"].(map[string]any)
	card, _ := cardData["card"].(map[string]any)
	if got := strings.TrimSpace(card["issue_id"].(string)); got != "smoke-issue-1" {
		t.Fatalf("card.issue_id=%q want smoke-issue-1", got)
	}

	actionReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/card-action", bytes.NewBufferString(`{
		"request_id":"req-smoke-action-001",
		"workspace_id":"`+orchestrationSmokeWorkspacePath+`",
		"issue_id":"smoke-issue-1",
		"action":"mark-done"
	}`))
	actionRR := httptest.NewRecorder()
	actionHandler.ServeHTTP(actionRR, actionReq)
	if actionRR.Code != http.StatusOK {
		t.Fatalf("card action status=%d body=%s", actionRR.Code, actionRR.Body.String())
	}

	verifyReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-card-get?workspace_id="+orchestrationSmokeWorkspacePath+"&issue_id=smoke-issue-1", nil)
	verifyRR := httptest.NewRecorder()
	cardHandler.ServeHTTP(verifyRR, verifyReq)
	if verifyRR.Code != http.StatusOK {
		t.Fatalf("verify card status=%d body=%s", verifyRR.Code, verifyRR.Body.String())
	}
	verifyBody := decodeResponseBody(t, verifyRR)
	verifyData, _ := verifyBody["data"].(map[string]any)
	verifyCard, _ := verifyData["card"].(map[string]any)
	if got := strings.TrimSpace(verifyCard["tracker_state"].(string)); got != "Done" {
		t.Fatalf("tracker_state=%q want Done", got)
	}
}
