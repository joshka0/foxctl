package main

import (
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/transport/httpjson"
)

func TestShouldWarnWarmup(t *testing.T) {
	if !shouldWarnWarmup(httpjson.ReadinessResponse{Idle: true, IdleForMS: 10_000}, 10*time.Second) {
		t.Fatal("expected warning after full idle window")
	}
	if shouldWarnWarmup(httpjson.ReadinessResponse{Idle: true, IdleForMS: 9_999}, 10*time.Second) {
		t.Fatal("warning fired before full idle window")
	}
	if shouldWarnWarmup(httpjson.ReadinessResponse{Idle: false, IdleForMS: 10_000}, 10*time.Second) {
		t.Fatal("warning fired while session was not idle")
	}
}

func TestFormatWarmupWarning(t *testing.T) {
	got := formatWarmupWarning("codex", 10*time.Second)
	for _, want := range []string{"[codex]", "no new output", "10s"} {
		if !strings.Contains(got, want) {
			t.Fatalf("warning %q missing %q", got, want)
		}
	}
}

func TestTalkbackFlags(t *testing.T) {
	var rules []talkbackRule
	flags := talkbackFlags{rules: &rules}
	if err := flags.Set("codex=@room:"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	m := talkbackMap(rules)
	if m["codex"] != "@room:" {
		t.Fatalf("talkback map = %+v", m)
	}
	if err := flags.Set("bad"); err == nil {
		t.Fatal("expected malformed talkback flag to fail")
	}
}

func TestTalkbackText(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{name: "plain prefix", line: "@room: hello", want: "hello", ok: true},
		{name: "codex bullet", line: "• @room: hello", want: "hello", ok: true},
		{name: "droid symbol", line: "⛬  @room: hello", want: "hello", ok: true},
		{name: "mention later", line: "please say @room: hello", ok: false},
		{name: "empty message", line: "• @room:   ", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := talkbackText("@room:", tt.line)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("talkbackText() = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestTalkbackMessageRoutesExplicitRoom(t *testing.T) {
	const explicitRoom = "01KPN9VZ2WAMWT5NJBPFCVSY51"
	roomID, text, ok := talkbackMessage("default-room", "@room:", "• @room:"+explicitRoom+" hello")
	if !ok {
		t.Fatal("talkbackMessage rejected explicit room")
	}
	if roomID != explicitRoom || text != "hello" {
		t.Fatalf("talkbackMessage() = room %q text %q; want %q / hello", roomID, text, explicitRoom)
	}

	roomID, text, ok = talkbackMessage("default-room", "@room:", "• @room: hello")
	if !ok {
		t.Fatal("talkbackMessage rejected default room")
	}
	if roomID != "default-room" || text != "hello" {
		t.Fatalf("talkbackMessage default = room %q text %q", roomID, text)
	}
}
