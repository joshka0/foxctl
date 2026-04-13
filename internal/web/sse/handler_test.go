package sse

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func readEvent(t *testing.T, body *bufio.Reader) map[string]any {
	t.Helper()
	for {
		line, err := body.ReadString('\n')
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

func TestHandler_GlobalClientReceivesScopedEvent(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	srv := httptest.NewServer(Handler(hub))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	connected := readEvent(t, reader)
	if got := strings.TrimSpace(connected["type"].(string)); got != "connected" {
		t.Fatalf("connected type=%q want connected", got)
	}

	hub.PublishTopic("room-event:ws1:alpha", "room.message", map[string]any{
		"room_id":    "alpha",
		"message_id": "msg-alpha",
	})

	event := readEvent(t, reader)
	if got := strings.TrimSpace(event["type"].(string)); got != "room.message" {
		t.Fatalf("event type=%q want room.message", got)
	}
}

func TestTopicHandler_ScopedClientReceivesGlobalEvent(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	srv := httptest.NewServer(TopicHandler(hub, "room-event:ws1:alpha"))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	connected := readEvent(t, reader)
	if got := strings.TrimSpace(connected["type"].(string)); got != "connected" {
		t.Fatalf("connected type=%q want connected", got)
	}

	hub.Publish("heartbeat", nil)

	event := readEvent(t, reader)
	if got := strings.TrimSpace(event["type"].(string)); got != "heartbeat" {
		t.Fatalf("event type=%q want heartbeat", got)
	}
}
