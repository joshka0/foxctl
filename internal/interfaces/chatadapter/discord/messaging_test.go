package discord

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/jkatigb/agentctl/internal/interfaces/chatadapter"
	"github.com/jkatigb/agentctl/internal/platform/config"
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
