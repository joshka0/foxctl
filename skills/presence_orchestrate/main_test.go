package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain/lite"
)

func newTestRunContext(t *testing.T, stdout *bytes.Buffer) *lite.RunContext {
	t.Helper()
	state := t.TempDir()
	cfg := lite.LiteConfig{
		Home:           state,
		InlineOutputKB: 32,
		Paths: lite.LitePaths{
			CAS:   filepath.Join(state, "cas"),
			Cache: filepath.Join(state, "cache"),
		},
		CAS: lite.LiteCASPolicy{Store: true, Expose: "off"},
	}
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	rc, err := lite.BuildRunContext(cfg, stdout)
	if err != nil {
		t.Fatalf("build run context: %v", err)
	}
	return rc
}

func decodeEnvelope(t *testing.T, raw []byte) Output {
	t.Helper()
	var env struct {
		Status string `json:"status"`
		Data   Output `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, raw)
	}
	if env.Status != "ok" {
		t.Fatalf("status = %q, want ok", env.Status)
	}
	return env.Data
}

func TestRunRejectsRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		want string
	}{
		{name: "empty text", in: Input{Text: "   ", ConversationID: "conv1"}, want: "text is required"},
		{name: "missing conversation", in: Input{Text: "hello"}, want: "conversation_id is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			rc := newTestRunContext(t, stdout)
			defer func() { _ = rc.Close() }()

			err := run(context.Background(), rc, tt.in)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestRunDetectsEmotionAndCleansDisplayText(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		wantEmotion string
		wantMethod  string
	}{
		{name: "neutral", text: "Hello there.", wantEmotion: EmotionNeutral, wantMethod: "heuristic"},
		{name: "marker", text: "*sighs* I suppose that's that.", wantEmotion: EmotionSadness, wantMethod: "marker_analysis"},
		{name: "emoji overrides marker", text: "*sighs* but actually 😊", wantEmotion: EmotionJoy, wantMethod: "emoji_map"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			rc := newTestRunContext(t, stdout)
			defer func() { _ = rc.Close() }()

			err := run(context.Background(), rc, Input{
				Text:           tt.text,
				ConversationID: "conv1",
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}

			out := decodeEnvelope(t, stdout.Bytes())
			if out.Emotion != tt.wantEmotion {
				t.Fatalf("emotion = %q, want %q", out.Emotion, tt.wantEmotion)
			}
			if out.Method != tt.wantMethod {
				t.Fatalf("method = %q, want %q", out.Method, tt.wantMethod)
			}
			if strings.Contains(out.DisplayText, "*sighs*") || strings.Contains(out.DisplayText, "😊") {
				t.Fatalf("display_text = %q, want marker and emoji stripped", out.DisplayText)
			}
		})
	}
}

func TestRunBuildsRequestedSubSkillParams(t *testing.T) {
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	err := run(context.Background(), rc, Input{
		Text:               "😊 *giggles* Wonderful!",
		ConversationID:     "conv-full",
		CharacterID:        "hero",
		GenerateBackground: true,
		GenerateVoice:      true,
		Scene:              "sunset",
		VoiceID:            "voice-abc",
		TTSProvider:        "elevenlabs",
		RewriteForTTS:      true,
		RewriteModel:       "gpt-4o-mini",
		RewriteMaxChars:    200,
		RewritePrompt:      "simplify",
		RewriteBaseURL:     "http://llm:8000",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	out := decodeEnvelope(t, stdout.Bytes())
	if out.BackgroundParams["style"] != "anime" {
		t.Fatalf("default background style = %v, want anime", out.BackgroundParams["style"])
	}
	if out.BackgroundParams["scene"] != "sunset" {
		t.Fatalf("background scene = %v, want sunset", out.BackgroundParams["scene"])
	}
	if out.CharacterParams["character_id"] != "hero" {
		t.Fatalf("character_id = %v, want hero", out.CharacterParams["character_id"])
	}
	if out.CharacterParams["emotion"] != EmotionJoy {
		t.Fatalf("character emotion = %v, want joy", out.CharacterParams["emotion"])
	}
	if out.VoiceParams["text"] != out.DisplayText {
		t.Fatalf("voice text = %v, want display text %q", out.VoiceParams["text"], out.DisplayText)
	}
	if out.VoiceParams["voice_id"] != "voice-abc" {
		t.Fatalf("voice_id = %v, want voice-abc", out.VoiceParams["voice_id"])
	}
	if out.VoiceParams["provider"] != "elevenlabs" {
		t.Fatalf("provider = %v, want elevenlabs", out.VoiceParams["provider"])
	}
	if out.VoiceParams["rewrite_for_tts"] != true {
		t.Fatalf("rewrite_for_tts = %v, want true", out.VoiceParams["rewrite_for_tts"])
	}
	if out.VoiceParams["rewrite_model"] != "gpt-4o-mini" {
		t.Fatalf("rewrite_model = %v, want gpt-4o-mini", out.VoiceParams["rewrite_model"])
	}
	if out.VoiceParams["rewrite_max_chars"] != float64(200) {
		t.Fatalf("rewrite_max_chars = %v, want 200", out.VoiceParams["rewrite_max_chars"])
	}
	if out.VoiceParams["rewrite_prompt"] != "simplify" {
		t.Fatalf("rewrite_prompt = %v, want simplify", out.VoiceParams["rewrite_prompt"])
	}
	if out.VoiceParams["rewrite_base_url"] != "http://llm:8000" {
		t.Fatalf("rewrite_base_url = %v, want http://llm:8000", out.VoiceParams["rewrite_base_url"])
	}
}
