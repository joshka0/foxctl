package discord

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/jkatigb/agentctl/internal/chatadapter"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/consolews"
)

func TestIsChatChannel(t *testing.T) {
	a := &Adapter{
		cfg: config.DiscordSettings{
			ChatChannelIDs: []string{"chan1", "chan2"},
		},
	}

	if !a.isChatChannel("chan1") {
		t.Error("expected chan1 to be a chat channel")
	}
	if !a.isChatChannel("chan2") {
		t.Error("expected chan2 to be a chat channel")
	}
	if a.isChatChannel("chan3") {
		t.Error("expected chan3 to NOT be a chat channel")
	}
	if a.isChatChannel("") {
		t.Error("expected empty string to NOT be a chat channel")
	}
}

func TestIsBotMentioned(t *testing.T) {
	sess := &discordgo.Session{
		State: discordgo.NewState(),
	}
	sess.State.User = &discordgo.User{ID: "bot123"}
	a := &Adapter{session: sess}

	// Mentioned
	msg := &discordgo.Message{
		Mentions: []*discordgo.User{{ID: "bot123"}},
	}
	if !a.isBotMentioned(msg) {
		t.Error("expected bot to be mentioned")
	}

	// Not mentioned
	msg2 := &discordgo.Message{
		Mentions: []*discordgo.User{{ID: "other456"}},
	}
	if a.isBotMentioned(msg2) {
		t.Error("expected bot to NOT be mentioned")
	}

	// No mentions
	msg3 := &discordgo.Message{}
	if a.isBotMentioned(msg3) {
		t.Error("expected bot to NOT be mentioned with empty mentions")
	}
}

func TestCleanMention(t *testing.T) {
	sess := &discordgo.Session{
		State: discordgo.NewState(),
	}
	sess.State.User = &discordgo.User{ID: "bot123"}
	a := &Adapter{session: sess}

	tests := []struct {
		input    string
		expected string
	}{
		{"<@bot123> hello", "hello"},
		{"<@!bot123> hello", "hello"},
		{"hello <@bot123>", "hello"},
		{"hello", "hello"},
		{"<@other> hello", "<@other> hello"},
	}

	for _, tt := range tests {
		result := a.cleanMention(tt.input)
		if result != tt.expected {
			t.Errorf("cleanMention(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestHandleMessageCreate_IgnoresBots(t *testing.T) {
	called := atomic.Bool{}
	a := &Adapter{
		cfg: config.DiscordSettings{
			ChatChannelIDs: []string{"chan1"},
		},
		msgHandler: func(ctx context.Context, evt chatadapter.MessageEvent) error {
			called.Store(true)
			return nil
		},
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	defer a.cancel()

	// Bot message should be ignored
	a.handleMessageCreate(nil, &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Author:    &discordgo.User{ID: "bot1", Bot: true},
			ChannelID: "chan1",
			Content:   "hello",
		},
	})

	time.Sleep(50 * time.Millisecond)
	if called.Load() {
		t.Error("handler should not be called for bot messages")
	}
}

func TestGetOrCreateSession(t *testing.T) {
	ctx := context.Background()
	hub := consolews.NewHub(ctx)
	hub.SetRunnerFactory(func(s *consolews.Session) consolews.Runner { return nil })

	sb := NewSessionBridge(hub, nil, config.DiscordSettings{
		ChatProfile: "explorer",
	})

	// First call creates a session
	s1, err := sb.getOrCreateSession("chan1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1 == nil {
		t.Fatal("expected non-nil session")
	}

	// Second call reuses the same session
	s2, err := sb.getOrCreateSession("chan1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1.ID() != s2.ID() {
		t.Errorf("expected same session, got %s vs %s", s1.ID(), s2.ID())
	}

	// Different channel gets a different session
	s3, err := sb.getOrCreateSession("chan2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1.ID() == s3.ID() {
		t.Error("expected different session for different channel")
	}
}

func TestGetOrCreateSession_RecreatesIfRemoved(t *testing.T) {
	ctx := context.Background()
	hub := consolews.NewHub(ctx)
	hub.SetRunnerFactory(func(s *consolews.Session) consolews.Runner { return nil })

	sb := NewSessionBridge(hub, nil, config.DiscordSettings{
		ChatProfile: "explorer",
	})

	// Create session
	s1, _ := sb.getOrCreateSession("chan1")
	oldID := s1.ID()

	// Remove from hub (simulating eviction)
	hub.RemoveSession(oldID)

	// Next call should create a new session
	s2, err := sb.getOrCreateSession("chan1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s2.ID() == oldID {
		t.Error("expected new session after hub eviction")
	}
}

func TestTruncateForDiscord(t *testing.T) {
	// Short string - no truncation
	short := "hello"
	if truncateForDiscord(short) != short {
		t.Error("short string should not be truncated")
	}

	// Exactly 2000 chars - no truncation
	exact := make([]byte, 2000)
	for i := range exact {
		exact[i] = 'a'
	}
	if len(truncateForDiscord(string(exact))) != 2000 {
		t.Error("2000-char string should not be truncated")
	}

	// Over 2000 chars - truncated with "..."
	long := make([]byte, 2500)
	for i := range long {
		long[i] = 'b'
	}
	result := truncateForDiscord(string(long))
	if len(result) != 2000 {
		t.Errorf("expected truncated length 2000, got %d", len(result))
	}
	if result[1997:] != "..." {
		t.Error("expected trailing '...'")
	}
}

func TestTruncateForDiscordWithSuffix(t *testing.T) {
	// Short string - suffix appended.
	if got := truncateForDiscordWithSuffix("hello", "..."); got != "hello..." {
		t.Fatalf("unexpected value: %q", got)
	}

	// Over limit - truncate to fit suffix.
	long := strings.Repeat("x", 2500)
	got := truncateForDiscordWithSuffix(long, "...")
	if len([]rune(got)) != 2000 {
		t.Fatalf("expected 2000 runes, got %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected suffix, got %q", got[len(got)-3:])
	}
}

func TestIsPartial(t *testing.T) {
	if isPartial(nil) {
		t.Error("nil metadata should not be partial")
	}
	if isPartial(map[string]any{}) {
		t.Error("empty metadata should not be partial")
	}
	if isPartial(map[string]any{"partial": false}) {
		t.Error("partial=false should not be partial")
	}
	if !isPartial(map[string]any{"partial": true}) {
		t.Error("partial=true should be partial")
	}
	if isPartial(map[string]any{"partial": "true"}) {
		t.Error("partial=string should not be partial (must be bool)")
	}
}

func TestCollectAndUpdate_FinalReply(t *testing.T) {
	hub := consolews.NewHub(context.Background())
	sb := NewSessionBridge(hub, nil, config.DiscordSettings{})

	ctx := context.Background()
	ch := make(chan consolews.Payload, 8)

	var editedContent string
	evt := chatadapter.NewMessageEvent("test", chatadapter.UserRef{}, "chan1", "", "msg1",
		func(content string, embeds []chatadapter.Embed) (chatadapter.MessageRef, error) {
			return chatadapter.MessageRef{ChannelID: "chan1", MessageID: "reply1"}, nil
		},
		func(ref chatadapter.MessageRef, content string, embeds []chatadapter.Embed) error {
			editedContent = content
			return nil
		},
	)
	ref := chatadapter.MessageRef{ChannelID: "chan1", MessageID: "reply1"}

	// Send a final reply
	ch <- consolews.Payload{
		Type:    consolews.PayloadTypeReply,
		Content: "Hello from the LLM!",
	}

	err := sb.collectAndUpdate(ctx, ch, ref, evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editedContent != "Hello from the LLM!" {
		t.Errorf("expected 'Hello from the LLM!', got %q", editedContent)
	}
}

func TestCollectAndUpdate_PartialEdit_TruncatesToLimit(t *testing.T) {
	hub := consolews.NewHub(context.Background())
	sb := NewSessionBridge(hub, nil, config.DiscordSettings{})

	ctx := context.Background()
	ch := make(chan consolews.Payload, 8)

	var edits []string
	evt := chatadapter.NewMessageEvent("test", chatadapter.UserRef{}, "chan1", "", "msg1",
		func(content string, embeds []chatadapter.Embed) (chatadapter.MessageRef, error) {
			return chatadapter.MessageRef{ChannelID: "chan1", MessageID: "reply1"}, nil
		},
		func(ref chatadapter.MessageRef, content string, embeds []chatadapter.Embed) error {
			edits = append(edits, content)
			return nil
		},
	)
	ref := chatadapter.MessageRef{ChannelID: "chan1", MessageID: "reply1"}

	// Send a partial event large enough to force truncation, then a final reply.
	large := strings.Repeat("a", 2500)
	ch <- consolews.Payload{
		Type:     consolews.PayloadTypeEvent,
		Content:  large,
		Metadata: map[string]any{"partial": true},
	}
	ch <- consolews.Payload{
		Type:    consolews.PayloadTypeReply,
		Content: large,
	}

	err := sb.collectAndUpdate(ctx, ch, ref, evt)
	if err != nil {
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
