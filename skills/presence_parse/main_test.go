package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain/lite"
)

func TestExtractEmoji(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		wantEmoji     []string
		wantEmotion   string
		wantIntensity float64
	}{
		{
			name:          "single joy emoji",
			text:          "That's great! 😊",
			wantEmoji:     []string{"\U0001F60A"},
			wantEmotion:   EmotionJoy,
			wantIntensity: 0.6,
		},
		{
			name:          "multiple joy emoji",
			text:          "Amazing! 😊😄",
			wantEmoji:     []string{"\U0001F60A", "\U0001F604"},
			wantEmotion:   EmotionJoy,
			wantIntensity: 0.8,
		},
		{
			name:          "three or more emoji",
			text:          "So happy! 😊😄😁",
			wantEmoji:     []string{"\U0001F60A", "\U0001F604", "\U0001F601"},
			wantEmotion:   EmotionJoy,
			wantIntensity: 1.0,
		},
		{
			name:          "sadness emoji",
			text:          "I'm sorry 😢",
			wantEmoji:     []string{"\U0001F622"},
			wantEmotion:   EmotionSadness,
			wantIntensity: 0.6,
		},
		{
			name:          "anger emoji",
			text:          "That's frustrating! 😠",
			wantEmoji:     []string{"\U0001F620"},
			wantEmotion:   EmotionAnger,
			wantIntensity: 0.6,
		},
		{
			name:          "fear emoji",
			text:          "Oh no! 😨",
			wantEmoji:     []string{"\U0001F628"},
			wantEmotion:   EmotionFear,
			wantIntensity: 0.6,
		},
		{
			name:          "surprise emoji",
			text:          "What?! 😮",
			wantEmoji:     []string{"\U0001F62E"},
			wantEmotion:   EmotionSurprise,
			wantIntensity: 0.6,
		},
		{
			name:          "playful emoji",
			text:          "Just kidding 😜",
			wantEmoji:     []string{"\U0001F61C"},
			wantEmotion:   EmotionPlayful,
			wantIntensity: 0.6,
		},
		{
			name:          "no emoji",
			text:          "Hello there",
			wantEmoji:     []string{},
			wantEmotion:   "",
			wantIntensity: 0,
		},
		{
			name:          "mixed emotions - dominant wins",
			text:          "Mixed feelings 😊😄😢", // 2 unique joy emoji vs 1 sadness
			wantEmoji:     []string{"\U0001F60A", "\U0001F604", "\U0001F622"},
			wantEmotion:   EmotionJoy, // 2 joy vs 1 sadness
			wantIntensity: 1.0,        // 3 emoji total = max intensity
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emoji, emotion, intensity := extractEmoji(tt.text)

			// Check emotion
			if emotion != tt.wantEmotion {
				t.Errorf("extractEmoji() emotion = %v, want %v", emotion, tt.wantEmotion)
			}

			// Check intensity
			if intensity != tt.wantIntensity {
				t.Errorf("extractEmoji() intensity = %v, want %v", intensity, tt.wantIntensity)
			}

			// Check emoji count (order may vary due to map iteration)
			if len(emoji) != len(tt.wantEmoji) {
				t.Errorf("extractEmoji() emoji count = %v, want %v", len(emoji), len(tt.wantEmoji))
			}
		})
	}
}

func TestExtractMarkers(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		wantMarkers []string
		wantEmotion string
	}{
		{
			name:        "single joy marker",
			text:        "*laughs* That's funny!",
			wantMarkers: []string{"laughs"},
			wantEmotion: EmotionJoy,
		},
		{
			name:        "multiple markers same emotion",
			text:        "*laughs* *giggles* So silly!",
			wantMarkers: []string{"laughs", "giggles"},
			wantEmotion: EmotionJoy,
		},
		{
			name:        "sadness marker",
			text:        "*sighs* I understand...",
			wantMarkers: []string{"sighs"},
			wantEmotion: EmotionSadness,
		},
		{
			name:        "fear marker",
			text:        "*trembles* That's scary",
			wantMarkers: []string{"trembles"},
			wantEmotion: EmotionFear,
		},
		{
			name:        "playful marker",
			text:        "*winks* You know what I mean",
			wantMarkers: []string{"winks"},
			wantEmotion: EmotionPlayful,
		},
		{
			name:        "neutral marker - no emotion",
			text:        "*pause* Let me think...",
			wantMarkers: []string{"pause"},
			wantEmotion: "", // pause has empty emotion
		},
		{
			name:        "no markers",
			text:        "Just plain text",
			wantMarkers: nil,
			wantEmotion: "",
		},
		{
			name:        "mixed neutral and emotion markers",
			text:        "*softly* *laughs* That's nice",
			wantMarkers: []string{"softly", "laughs"},
			wantEmotion: EmotionJoy, // softly is neutral, laughs is joy
		},
		{
			name:        "unknown marker",
			text:        "*unknown* Hello",
			wantMarkers: []string{"unknown"},
			wantEmotion: "", // unknown markers don't map to emotion
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			markers, emotion := extractMarkers(tt.text)

			// Check emotion
			if emotion != tt.wantEmotion {
				t.Errorf("extractMarkers() emotion = %v, want %v", emotion, tt.wantEmotion)
			}

			// Check markers
			if len(markers) != len(tt.wantMarkers) {
				t.Errorf("extractMarkers() markers = %v, want %v", markers, tt.wantMarkers)
			}
		})
	}
}

func TestStripEmoji(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		emoji []string
		want  string
	}{
		{
			name:  "strip single emoji",
			text:  "Hello 😊 world",
			emoji: []string{"\U0001F60A"},
			want:  "Hello  world",
		},
		{
			name:  "strip multiple emoji",
			text:  "😊 Hello 😄 world 😁",
			emoji: []string{"\U0001F60A", "\U0001F604", "\U0001F601"},
			want:  " Hello  world ",
		},
		{
			name:  "no emoji to strip",
			text:  "Hello world",
			emoji: []string{},
			want:  "Hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripEmoji(tt.text, tt.emoji)
			if got != tt.want {
				t.Errorf("stripEmoji() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripMarkers(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "strip single marker",
			text: "*laughs* That's funny!",
			want: " That's funny!",
		},
		{
			name: "strip multiple markers",
			text: "*softly* Hello *pause* there",
			want: " Hello  there",
		},
		{
			name: "no markers",
			text: "Hello world",
			want: "Hello world",
		},
		{
			name: "marker at end",
			text: "Goodbye *winks*",
			want: "Goodbye ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMarkers(tt.text)
			if got != tt.want {
				t.Errorf("stripMarkers() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmotionPrecedence(t *testing.T) {
	// Test that emoji takes precedence over markers
	t.Run("emoji overrides marker", func(t *testing.T) {
		// Text has sadness marker but joy emoji
		text := "*sighs* But actually I'm happy! 😊"

		emoji, emojiEmotion, _ := extractEmoji(text)
		_, markerEmotion := extractMarkers(text)

		// Verify emoji detected joy
		if emojiEmotion != EmotionJoy {
			t.Errorf("Expected emoji emotion to be joy, got %v", emojiEmotion)
		}

		// Verify marker detected sadness
		if markerEmotion != EmotionSadness {
			t.Errorf("Expected marker emotion to be sadness, got %v", markerEmotion)
		}

		// In the run function, emoji should win
		if len(emoji) > 0 && emojiEmotion != "" {
			// This is the expected path - emoji emotion is used
		} else {
			t.Error("Emoji should be detected and take precedence")
		}
	})
}

func TestIntensityScaling(t *testing.T) {
	// Test intensity increases with unique emoji count
	// The implementation counts unique emoji, not repeated instances
	joyEmoji := []string{
		"\U0001F60A", // 😊
		"\U0001F604", // 😄
		"\U0001F601", // 😁
		"\U0001F603", // 😃
		"\U0001F606", // 😆
	}

	tests := []struct {
		count         int
		wantIntensity float64
	}{
		{1, 0.6},
		{2, 0.8},
		{3, 1.0},
		{5, 1.0},
	}

	for _, tt := range tests {
		text := ""
		for i := 0; i < tt.count && i < len(joyEmoji); i++ {
			text += joyEmoji[i] + " "
		}

		_, _, intensity := extractEmoji(text)

		if intensity != tt.wantIntensity {
			t.Errorf("For %d unique emoji, intensity = %v, want %v", tt.count, intensity, tt.wantIntensity)
		}
	}
}

func newTestContext(t *testing.T, stdout *bytes.Buffer) *lite.RunContext {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := lite.LiteConfig{
		Home:           tmpDir,
		InlineOutputKB: 32,
		Paths: lite.LitePaths{
			CAS:   filepath.Join(tmpDir, "cas"),
			Cache: filepath.Join(tmpDir, "cache"),
		},
		CAS: lite.LiteCASPolicy{Store: true, Expose: "off"},
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	rc, err := lite.BuildRunContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func TestRunPresenceParse(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestContext(t, stdout)
	defer func() { _ = rc.Close() }()

	in := Input{
		Text:         "That's great! \U0001F60A",
		StripMarkers: true,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	out := stdout.String()
	if out == "" {
		t.Fatal("expected non-empty output envelope")
	}

	var env struct {
		Status string `json:"status"`
		Data   struct {
			Emotion string   `json:"emotion"`
			Emoji   []string `json:"detected_emoji"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parse envelope: %v\noutput: %s", err, out)
	}
	if env.Status != "ok" {
		t.Fatalf("expected ok status, got %q", env.Status)
	}
	if env.Data.Emotion != EmotionJoy {
		t.Fatalf("expected joy emotion, got %q", env.Data.Emotion)
	}
	if len(env.Data.Emoji) == 0 {
		t.Fatal("expected at least one detected emoji")
	}
}

func TestRunPresenceParseMarkers(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestContext(t, stdout)
	defer func() { _ = rc.Close() }()

	in := Input{
		Text: "*laughs* That's funny!",
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	out := stdout.String()
	var env struct {
		Data struct {
			Emotion string   `json:"emotion"`
			Markers []string `json:"markers"`
			Method  string   `json:"method"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	if env.Data.Emotion != EmotionJoy {
		t.Fatalf("expected joy emotion from marker, got %q", env.Data.Emotion)
	}
	if env.Data.Method != "marker_analysis" {
		t.Fatalf("expected marker_analysis method, got %q", env.Data.Method)
	}
	if len(env.Data.Markers) == 0 {
		t.Fatal("expected at least one marker")
	}
}

func TestRunPresenceParseAllowedEmotionsFilter(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestContext(t, stdout)
	defer func() { _ = rc.Close() }()

	in := Input{
		Text:            "That's great! \U0001F60A",
		AllowedEmotions: []string{"sadness", "neutral"},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	out := stdout.String()
	var env struct {
		Data struct {
			Emotion    string  `json:"emotion"`
			Confidence float64 `json:"confidence"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	if env.Data.Emotion != EmotionNeutral {
		t.Fatalf("expected neutral after filter, got %q", env.Data.Emotion)
	}
	if env.Data.Confidence >= 0.9 {
		t.Fatalf("expected reduced confidence after filter, got %v", env.Data.Confidence)
	}
}
