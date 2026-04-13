package chatadapter

import (
	"context"
	"strings"
	"testing"

	consolepkg "github.com/jkatigb/agentctl/internal/console"
	"github.com/jkatigb/agentctl/internal/web/consolews"
)

func TestGetOrCreateSession_ReusesAndRecreates(t *testing.T) {
	ctx := context.Background()
	hub := consolews.NewHub(ctx)

	sb := NewSessionBridge(hub, nil, SessionBridgeConfig{
		PlatformName:  "test",
		MaxMessageLen: 2000,
		ChatProfile:   "explorer",
	}, nil)

	// First call creates a session.
	s1, err := sb.GetOrCreateSession(ctx, "chan1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1 == nil {
		t.Fatal("expected non-nil session")
	}

	// Second call reuses the same session.
	s2, err := sb.GetOrCreateSession(ctx, "chan1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1.ID() != s2.ID() {
		t.Fatalf("expected same session, got %s vs %s", s1.ID(), s2.ID())
	}

	// Different channel gets a different session.
	s3, err := sb.GetOrCreateSession(ctx, "chan2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s3.ID() == s1.ID() {
		t.Fatalf("expected different session for different channel")
	}

	// If removed from hub, next call recreates a new session.
	oldID := s1.ID()
	hub.RemoveSession(oldID)

	s4, err := sb.GetOrCreateSession(ctx, "chan1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s4.ID() == oldID {
		t.Fatalf("expected new session after hub eviction")
	}
}

func TestCollectAndUpdate_FinalReply(t *testing.T) {
	hub := consolews.NewHub(context.Background())
	sb := NewSessionBridge(hub, nil, SessionBridgeConfig{MaxMessageLen: 2000}, nil)

	ctx := context.Background()
	ch := make(chan consolepkg.Event, 8)

	var editedContent string
	evt := NewMessageEvent("test", UserRef{}, "chan1", "", "msg1",
		func(content string, embeds []Embed) (MessageRef, error) {
			return MessageRef{ChannelID: "chan1", MessageID: "reply1"}, nil
		},
		func(ref MessageRef, content string, embeds []Embed) error {
			editedContent = content
			return nil
		},
	)
	ref := MessageRef{ChannelID: "chan1", MessageID: "reply1"}

	ch <- consolepkg.Event{
		Type:    consolepkg.EventTypeReply,
		Content: "Hello from the LLM!",
	}

	if err := sb.collectAndUpdate(ctx, ch, ref, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editedContent != "Hello from the LLM!" {
		t.Fatalf("expected final edit, got %q", editedContent)
	}
}

func TestCollectAndUpdate_PartialEdit_TruncatesToLimit(t *testing.T) {
	hub := consolews.NewHub(context.Background())
	sb := NewSessionBridge(hub, nil, SessionBridgeConfig{
		MaxMessageLen:  2000,
		EditIntervalMS: 1,
	}, nil)

	ctx := context.Background()
	ch := make(chan consolepkg.Event, 8)

	var edits []string
	evt := NewMessageEvent("test", UserRef{}, "chan1", "", "msg1",
		func(content string, embeds []Embed) (MessageRef, error) {
			return MessageRef{ChannelID: "chan1", MessageID: "reply1"}, nil
		},
		func(ref MessageRef, content string, embeds []Embed) error {
			edits = append(edits, content)
			return nil
		},
	)
	ref := MessageRef{ChannelID: "chan1", MessageID: "reply1"}

	large := strings.Repeat("a", 2500)
	ch <- consolepkg.Event{
		Type:     consolepkg.EventTypeEvent,
		Content:  large,
		Metadata: map[string]any{"partial": true},
	}
	ch <- consolepkg.Event{
		Type:    consolepkg.EventTypeReply,
		Content: large,
	}

	if err := sb.collectAndUpdate(ctx, ch, ref, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(edits) < 2 {
		t.Fatalf("expected at least 2 edits (partial + final), got %d", len(edits))
	}
	for i, e := range edits {
		if len([]rune(e)) > 2000 {
			t.Fatalf("edit[%d] exceeds 2000 runes: %d", i, len([]rune(e)))
		}
	}
}

func TestIsPartial(t *testing.T) {
	if IsPartial(nil) {
		t.Fatal("nil metadata should not be partial")
	}
	if IsPartial(map[string]any{}) {
		t.Fatal("empty metadata should not be partial")
	}
	if IsPartial(map[string]any{"partial": false}) {
		t.Fatal("partial=false should not be partial")
	}
	if !IsPartial(map[string]any{"partial": true}) {
		t.Fatal("partial=true should be partial")
	}
	if IsPartial(map[string]any{"partial": "true"}) {
		t.Fatal("partial=string should not be partial")
	}
}
