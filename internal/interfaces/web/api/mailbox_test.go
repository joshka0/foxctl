package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
)

func TestMailboxListHandler_FiltersByStreamAndTask(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	store, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer store.Close()

	msgs := []agent.BoardMessage{
		{
			WorkspaceID: "ws1",
			TaskID:      "task-1",
			Stream:      "room:alpha",
			Sender:      "actor:agent:a",
			Recipient:   "actor:agent:coder",
			Subject:     "alpha-task-1",
			Body:        "match",
		},
		{
			WorkspaceID: "ws1",
			TaskID:      "task-2",
			Stream:      "room:alpha",
			Sender:      "actor:agent:b",
			Recipient:   "actor:agent:coder",
			Subject:     "alpha-task-2",
			Body:        "wrong-task",
		},
		{
			WorkspaceID: "ws1",
			TaskID:      "task-1",
			Stream:      "room:beta",
			Sender:      "actor:agent:c",
			Recipient:   "actor:agent:coder",
			Subject:     "beta-task-1",
			Body:        "wrong-stream",
		},
	}
	for i := range msgs {
		if err := store.SendMessage(context.Background(), &msgs[i]); err != nil {
			t.Fatalf("send message[%d]: %v", i, err)
		}
	}

	h := MailboxListHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/mailbox?workspace_id=ws1&all=true&stream=room:alpha&task_id=task-1&limit=10", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	body := decodeResponseBody(t, rr)
	rawMessages, ok := body["messages"].([]any)
	if !ok {
		t.Fatalf("messages type=%T want []any", body["messages"])
	}
	if len(rawMessages) != 1 {
		t.Fatalf("messages=%d want 1", len(rawMessages))
	}
	msg, ok := rawMessages[0].(map[string]any)
	if !ok {
		t.Fatalf("message type=%T want map[string]any", rawMessages[0])
	}
	if got := strings.TrimSpace(msg["stream"].(string)); got != "room:alpha" {
		t.Fatalf("stream=%q want room:alpha", got)
	}
	if got := strings.TrimSpace(msg["task_id"].(string)); got != "task-1" {
		t.Fatalf("task_id=%q want task-1", got)
	}
}

func TestMailboxListHandler_PatchMarksMessagesRead(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	store, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer store.Close()

	msg := &agent.BoardMessage{
		WorkspaceID: "ws1",
		Stream:      "room:alpha",
		Sender:      "actor:agent:a",
		Recipient:   "actor:agent:coder",
		Subject:     "needs-read",
		Body:        "hello",
	}
	if err := store.SendMessage(context.Background(), msg); err != nil {
		t.Fatalf("send message: %v", err)
	}

	h := MailboxListHandler(cfg, zerolog.Nop())
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/mailbox", strings.NewReader(`{
		"workspace_id":"ws1",
		"actor_id":"actor:agent:coder",
		"action":"read",
		"message_ids":["`+msg.ID+`"]
	}`))
	patchRR := httptest.NewRecorder()
	h.ServeHTTP(patchRR, patchReq)

	if patchRR.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patchRR.Code, patchRR.Body.String())
	}
	patchBody := decodeResponseBody(t, patchRR)
	if got := strings.TrimSpace(patchBody["action"].(string)); got != "read" {
		t.Fatalf("action=%q want read", got)
	}
	if got := int(patchBody["updated"].(float64)); got != 1 {
		t.Fatalf("updated=%d want 1", got)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/mailbox?workspace_id=ws1&actor_id=actor:agent:coder&only_unread=true&limit=10", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	getBody := decodeResponseBody(t, getRR)
	rawMessages, ok := getBody["messages"].([]any)
	if !ok {
		t.Fatalf("messages type=%T want []any", getBody["messages"])
	}
	if len(rawMessages) != 0 {
		t.Fatalf("unread messages=%d want 0", len(rawMessages))
	}
}
