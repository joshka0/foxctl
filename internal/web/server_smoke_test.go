package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/coordination"
	"github.com/jkatigb/agentctl/internal/web/api"
)

const (
	serverRoomSmokeWorkspacePath          = "/tmp/agentctl-server-room-smoke-workspace"
	serverOrchestrationSmokeWorkspacePath = "/tmp/agentctl-server-orchestration-smoke-workspace"
	serverRoomBoardSmokeWorkspacePath     = "/tmp/agentctl-server-room-board-smoke-workspace"
)

type smokeTestServer struct {
	*httptest.Server
	cfg config.Config
}

func activateServerRoomLoop(t *testing.T, cfg config.Config, workspaceID, roomID string) {
	t.Helper()
	store, err := coordination.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open coordination store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	leaseName := "room-loop:" + workspaceID + ":" + roomID + ":delivery"
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
		ManagedBy:               "agentctl.room.loop/test",
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

func TestServerHandler_RoomsSmoke(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	server := newSmokeTestServer(t)
	defer server.Close()

	createBody := `{
		"workspace_id":"` + serverRoomSmokeWorkspacePath + `",
		"id":"server-room",
		"title":"Server Room",
		"description":"server smoke room",
		"members":[
			{"actor_id":"actor:agent:a","role":"owner"}
		]
	}`
	createResp := mustJSONRequest(t, server, http.MethodPost, "/api/rooms", createBody)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create room status=%d body=%s", createResp.StatusCode, readBody(t, createResp))
	}
	activateServerRoomLoop(t, server.cfg, serverRoomSmokeWorkspacePath, "server-room")

	listResp := mustJSONRequest(t, server, http.MethodGet, "/api/rooms?workspace_id="+serverRoomSmokeWorkspacePath+"&limit=10", "")
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list rooms status=%d body=%s", listResp.StatusCode, readBody(t, listResp))
	}
	listBody := decodeJSONMap(t, listResp)
	rawRooms, ok := listBody["rooms"].([]any)
	if !ok || len(rawRooms) != 1 {
		t.Fatalf("rooms payload=%v want 1 room", listBody["rooms"])
	}

	postResp := mustJSONRequest(t, server, http.MethodPost, "/api/rooms/server-room/messages", `{
		"workspace_id":"`+serverRoomSmokeWorkspacePath+`",
		"sender":"human:gui",
		"body":"server smoke message"
	}`)
	if postResp.StatusCode != http.StatusCreated {
		t.Fatalf("post room message status=%d body=%s", postResp.StatusCode, readBody(t, postResp))
	}

	getResp := mustJSONRequest(t, server, http.MethodGet, "/api/rooms/server-room?workspace_id="+serverRoomSmokeWorkspacePath, "")
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get room status=%d body=%s", getResp.StatusCode, readBody(t, getResp))
	}
	getBody := decodeJSONMap(t, getResp)
	room, ok := getBody["room"].(map[string]any)
	if !ok {
		t.Fatalf("room payload type=%T want map[string]any", getBody["room"])
	}
	if got := strings.TrimSpace(room["id"].(string)); got != "server-room" {
		t.Fatalf("room.id=%q want server-room", got)
	}
	if got := int(room["message_count"].(float64)); got != 1 {
		t.Fatalf("room.message_count=%d want 1", got)
	}
}

func TestServerHandler_OrchestrationBoardSmoke(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	server := newSmokeTestServer(t)
	defer server.Close()

	seedResp := mustJSONRequest(t, server, http.MethodPost, "/api/orchestration/seed-cards", `{
		"request_id":"req-server-seed-001",
		"workspace_id":"`+serverOrchestrationSmokeWorkspacePath+`",
		"cards":[
			{"issue_id":"server-issue-1","issue_identifier":"SERVER-1","title":"Server smoke card one"},
			{"issue_id":"server-issue-2","issue_identifier":"SERVER-2","title":"Server smoke card two"}
		]
	}`)
	if seedResp.StatusCode != http.StatusOK {
		t.Fatalf("seed cards status=%d body=%s", seedResp.StatusCode, readBody(t, seedResp))
	}

	boardResp := mustJSONRequest(t, server, http.MethodGet, "/api/orchestration/board-get?workspace_id="+serverOrchestrationSmokeWorkspacePath+"&limit=10", "")
	if boardResp.StatusCode != http.StatusOK {
		t.Fatalf("board get status=%d body=%s", boardResp.StatusCode, readBody(t, boardResp))
	}
	boardBody := decodeJSONMap(t, boardResp)
	if got := countBoardCards(boardBody); got < 2 {
		t.Fatalf("board card count=%d want >= 2", got)
	}

	cardResp := mustJSONRequest(t, server, http.MethodGet, "/api/orchestration/board-card-get?workspace_id="+serverOrchestrationSmokeWorkspacePath+"&issue_id=server-issue-1", "")
	if cardResp.StatusCode != http.StatusOK {
		t.Fatalf("card get status=%d body=%s", cardResp.StatusCode, readBody(t, cardResp))
	}
	cardBody := decodeJSONMap(t, cardResp)
	data, _ := cardBody["data"].(map[string]any)
	card, _ := data["card"].(map[string]any)
	if got := strings.TrimSpace(card["issue_id"].(string)); got != "server-issue-1" {
		t.Fatalf("card.issue_id=%q want server-issue-1", got)
	}
}

func TestServerHandler_RoomBoardWorkflowSmoke(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	server := newSmokeTestServer(t)
	defer server.Close()

	createRoomResp := mustJSONRequest(t, server, http.MethodPost, "/api/rooms", `{
		"workspace_id":"`+serverRoomBoardSmokeWorkspacePath+`",
		"id":"room-board-smoke",
		"title":"Room Board Smoke",
		"description":"room plus board routed smoke",
		"members":[
			{"actor_id":"agent:lead","role":"lead"},
			{"actor_id":"agent:worker","role":"worker"}
		]
	}`)
	if createRoomResp.StatusCode != http.StatusCreated {
		t.Fatalf("create room status=%d body=%s", createRoomResp.StatusCode, readBody(t, createRoomResp))
	}
	activateServerRoomLoop(t, server.cfg, serverRoomBoardSmokeWorkspacePath, "room-board-smoke")

	seedResp := mustJSONRequest(t, server, http.MethodPost, "/api/orchestration/seed-cards", `{
		"request_id":"req-server-room-board-seed-001",
		"workspace_id":"`+serverRoomBoardSmokeWorkspacePath+`",
		"cards":[
			{"issue_id":"room-board-issue-1","issue_identifier":"ROOM-BOARD-1","title":"Close loop from room transcript to board"}
		]
	}`)
	if seedResp.StatusCode != http.StatusOK {
		t.Fatalf("seed cards status=%d body=%s", seedResp.StatusCode, readBody(t, seedResp))
	}

	for _, sender := range []string{"human:gui", "agent:worker", "agent:lead"} {
		body := "room-board smoke update from " + sender
		if sender == "agent:lead" {
			body = "ROOM-BOARD-DONE room-board-issue-1: coordination complete"
		}
		postResp := mustJSONRequest(t, server, http.MethodPost, "/api/rooms/room-board-smoke/messages", `{
			"workspace_id":"`+serverRoomBoardSmokeWorkspacePath+`",
			"sender":"`+sender+`",
			"task_id":"room-board-issue-1",
			"body":"`+body+`"
		}`)
		if postResp.StatusCode != http.StatusCreated {
			t.Fatalf("post room message sender=%s status=%d body=%s", sender, postResp.StatusCode, readBody(t, postResp))
		}
	}

	msgResp := mustJSONRequest(t, server, http.MethodGet, "/api/rooms/room-board-smoke/messages?workspace_id="+serverRoomBoardSmokeWorkspacePath+"&limit=10", "")
	if msgResp.StatusCode != http.StatusOK {
		t.Fatalf("room messages status=%d body=%s", msgResp.StatusCode, readBody(t, msgResp))
	}
	msgBody := decodeJSONMap(t, msgResp)
	rawMessages, ok := msgBody["messages"].([]any)
	if !ok || len(rawMessages) != 3 {
		t.Fatalf("room messages payload=%v want 3 messages", msgBody["messages"])
	}

	cardResp := mustJSONRequest(t, server, http.MethodGet, "/api/orchestration/board-card-get?workspace_id="+serverRoomBoardSmokeWorkspacePath+"&issue_id=room-board-issue-1", "")
	if cardResp.StatusCode != http.StatusOK {
		t.Fatalf("card get status=%d body=%s", cardResp.StatusCode, readBody(t, cardResp))
	}
	cardBody := decodeJSONMap(t, cardResp)
	data, _ := cardBody["data"].(map[string]any)
	card, _ := data["card"].(map[string]any)
	if got := strings.TrimSpace(card["tracker_state"].(string)); got != "Done" {
		t.Fatalf("tracker_state=%q want Done", got)
	}
}

func newSmokeTestServer(t *testing.T) *smokeTestServer {
	t.Helper()

	originalRelayHook := api.RoomSendLiveRelayHookForTests()
	api.SetRoomSendLiveRelayHookForTests(func(ctx context.Context, workspaceID, roomID, messageID string) ([]api.RoomLiveRelayResult, error) {
		return nil, nil
	})
	t.Cleanup(func() {
		api.SetRoomSendLiveRelayHookForTests(originalRelayHook)
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg := config.Config{
		Storage: config.StorageSettings{Root: t.TempDir()},
		Database: config.DatabaseSettings{
			Driver: "sqlite",
		},
	}
	s, err := NewServer(ctx, Options{DevCORS: true}, cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("new web server: %v", err)
	}
	return &smokeTestServer{
		Server: httptest.NewServer(s.Handler()),
		cfg:    cfg,
	}
}

func mustJSONRequest(t *testing.T, server *smokeTestServer, method, path, body string) *http.Response {
	t.Helper()

	client := server.Client()
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		var reqBody *bytes.Reader
		if body != "" {
			reqBody = bytes.NewReader([]byte(body))
		} else {
			reqBody = bytes.NewReader(nil)
		}
		req, err := http.NewRequest(method, server.URL+path, reqBody)
		if err != nil {
			t.Fatalf("new request %s %s: %v", method, path, err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err == nil {
			payload, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				time.Sleep(25 * time.Millisecond)
				continue
			}
			resp.Body = io.NopCloser(bytes.NewReader(payload))
			return resp
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("do request %s %s: %v", method, path, lastErr)
	return nil
}

func decodeJSONMap(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return body
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	var payload bytes.Buffer
	if _, err := payload.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return payload.String()
}

func countBoardCards(body map[string]any) int {
	data, _ := body["data"].(map[string]any)
	lanes, _ := data["lanes"].([]any)
	total := 0
	for _, laneValue := range lanes {
		lane, _ := laneValue.(map[string]any)
		cards, _ := lane["cards"].([]any)
		total += len(cards)
	}
	return total
}
