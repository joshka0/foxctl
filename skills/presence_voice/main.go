// Package main implements the presence/voice skill.
// Generates speech audio using ElevenLabs TTS with emotion support.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

const command = "presence/voice"

// Default voice ID (Rachel - warm, expressive female voice)
const defaultVoiceID = "21m00Tcm4TlvDq8ikWAM"

// Input defines the input parameters for presence/voice.
type Input struct {
	Text           string  `json:"text" validate:"required,max=5000"`
	VoiceID        string  `json:"voice_id"`
	Emotion        string  `json:"emotion"`
	Intensity      float64 `json:"intensity"`
	ConversationID string  `json:"conversation_id"`
	UseCache       bool    `json:"use_cache"`
	Model          string  `json:"model"`
}

// Output defines the output for presence/voice.
type Output struct {
	AudioDigest string `json:"audio_digest"`
	DurationMS  int    `json:"duration_ms"`
	Cached      bool   `json:"cached"`
	Model       string `json:"model"`
	VoiceID     string `json:"voice_id"`
}

// VoiceSettings holds ElevenLabs voice modulation settings.
type VoiceSettings struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
	Style           float64 `json:"style"`
	UseSpeakerBoost bool    `json:"use_speaker_boost"`
}

// emotionVoiceSettings maps emotions to voice settings.
var emotionVoiceSettings = map[string]VoiceSettings{
	"neutral":  {Stability: 0.5, SimilarityBoost: 0.75, Style: 0.0, UseSpeakerBoost: true},
	"joy":      {Stability: 0.3, SimilarityBoost: 0.8, Style: 0.3, UseSpeakerBoost: true},
	"sadness":  {Stability: 0.7, SimilarityBoost: 0.6, Style: 0.2, UseSpeakerBoost: true},
	"anger":    {Stability: 0.2, SimilarityBoost: 0.9, Style: 0.5, UseSpeakerBoost: true},
	"fear":     {Stability: 0.6, SimilarityBoost: 0.7, Style: 0.1, UseSpeakerBoost: true},
	"surprise": {Stability: 0.3, SimilarityBoost: 0.8, Style: 0.4, UseSpeakerBoost: true},
	"disgust":  {Stability: 0.6, SimilarityBoost: 0.7, Style: 0.2, UseSpeakerBoost: true},
	"playful":  {Stability: 0.3, SimilarityBoost: 0.8, Style: 0.5, UseSpeakerBoost: true},
}

func main() {
	// Load .env before any API key access
	config.LoadDotEnv()
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate text
	if strings.TrimSpace(in.Text) == "" {
		return skillerr.Arg("text is required")
	}
	if len(in.Text) > 5000 {
		return skillerr.Arg("text exceeds 5000 character limit")
	}

	// Fail fast: Check CAS store availability before making expensive API calls
	if rc.CASStore == nil {
		return skillerr.Runtime("CAS store not available",
			skillerr.WithHint("CAS store is required for storing generated audio"))
	}

	// Get API key
	apiKey := rc.Config.LLM.ElevenLabsAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ELEVENLABS_API_KEY")
	}
	if apiKey == "" {
		return skillerr.Auth("ELEVENLABS_API_KEY not set",
			skillerr.WithHint("Set ELEVENLABS_API_KEY in environment or ~/.agentctl/.env"))
	}

	// Default and normalize values
	voiceID := in.VoiceID
	if voiceID == "" {
		voiceID = defaultVoiceID
	}
	model := in.Model
	if model == "" {
		model = "eleven_multilingual_v2"
	}
	if in.Intensity <= 0 {
		in.Intensity = 0.5
	}
	// Normalize emotion for consistent caching and voice settings lookup
	if in.Emotion != "" {
		in.Emotion = strings.ToLower(in.Emotion)
	}

	// Create cache key from text + voice + emotion
	textHash := hashText(in.Text, voiceID, in.Emotion, in.Intensity)

	// Check cache if enabled
	if in.UseCache && in.ConversationID != "" && rc.CASStore != nil {
		digest, durationMS, found := checkVoiceCache(ctx, rc, in.ConversationID, textHash, voiceID)
		if found {
			return skillout.Emit(rc, command, Output{
				AudioDigest: digest,
				DurationMS:  durationMS,
				Cached:      true,
				Model:       "cached",
				VoiceID:     voiceID,
			})
		}
	}

	// Get voice settings for emotion
	settings := getVoiceSettings(in.Emotion, in.Intensity)

	// Generate audio using ElevenLabs
	audioData, err := generateSpeech(ctx, apiKey, voiceID, model, in.Text, settings)
	if err != nil {
		return skillerr.WrapRuntime("generate speech", err)
	}

	// Estimate duration (rough: ~150 chars per second at normal speed)
	durationMS := len(in.Text) * 1000 / 150
	if durationMS < 500 {
		durationMS = 500
	}

	// Store in CAS (already validated at function start)
	tags := []string{
		"presence",
		"voice",
		fmt.Sprintf("voice_id:%s", voiceID),
	}
	if in.Emotion != "" {
		tags = append(tags, fmt.Sprintf("emotion:%s", in.Emotion))
	}
	if in.ConversationID != "" {
		tags = append(tags, fmt.Sprintf("conversation:%s", in.ConversationID))
	}

	obj, err := rc.CASStore.Put(ctx, bytes.NewReader(audioData), "audio/mpeg", tags)
	if err != nil {
		return skillerr.WrapIO("store audio in CAS", err)
	}

	// Cache the result only when caching is enabled and conversation_id provided
	if in.UseCache && in.ConversationID != "" {
		cacheVoiceResult(ctx, rc, in.ConversationID, textHash, voiceID, obj.Digest, model, durationMS)
	}

	return skillout.Emit(rc, command, Output{
		AudioDigest: obj.Digest,
		DurationMS:  durationMS,
		Cached:      false,
		Model:       model,
		VoiceID:     voiceID,
	})
}

// hashText creates a deterministic hash for caching.
func hashText(text, voiceID, emotion string, intensity float64) string {
	data := fmt.Sprintf("%s|%s|%s|%.2f", text, voiceID, emotion, intensity)
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:16])
}

// getVoiceSettings returns voice settings adjusted for emotion and intensity.
func getVoiceSettings(emotion string, intensity float64) VoiceSettings {
	base, ok := emotionVoiceSettings[strings.ToLower(emotion)]
	if !ok {
		base = emotionVoiceSettings["neutral"]
	}

	// Adjust settings based on intensity
	// Higher intensity = more dramatic expression
	if intensity > 0.5 {
		factor := (intensity - 0.5) * 2 // 0 to 1
		// Reduce stability for more variation
		base.Stability = base.Stability * (1 - factor*0.3)
		// Increase style for more expression
		base.Style = base.Style + factor*0.2
		if base.Style > 1.0 {
			base.Style = 1.0
		}
	} else if intensity < 0.5 {
		factor := (0.5 - intensity) * 2 // 0 to 1
		// Increase stability for calmer delivery
		base.Stability = base.Stability + factor*0.2
		if base.Stability > 1.0 {
			base.Stability = 1.0
		}
		// Reduce style
		base.Style = base.Style * (1 - factor*0.5)
	}

	return base
}

// elevenLabsRequest is the request body for ElevenLabs TTS.
type elevenLabsRequest struct {
	Text          string        `json:"text"`
	ModelID       string        `json:"model_id"`
	VoiceSettings VoiceSettings `json:"voice_settings"`
}

// generateSpeech calls ElevenLabs API to generate audio.
func generateSpeech(ctx context.Context, apiKey, voiceID, model, text string, settings VoiceSettings) ([]byte, error) {
	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", voiceID)

	reqBody := elevenLabsRequest{
		Text:          text,
		ModelID:       model,
		VoiceSettings: settings,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", apiKey)
	req.Header.Set("Accept", "audio/mpeg")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Try to parse error message (sanitized - don't expose raw response body)
		var errResp struct {
			Detail struct {
				Message string `json:"message"`
			} `json:"detail"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Detail.Message != "" {
			// Sanitize: only include structured error message, not raw body
			return nil, fmt.Errorf("elevenlabs API error (status %d): %s", resp.StatusCode, errResp.Detail.Message)
		}
		// Generic error without exposing potentially sensitive response body
		return nil, fmt.Errorf("elevenlabs API error (status %d): request failed", resp.StatusCode)
	}

	return body, nil
}

// checkVoiceCache looks up cached audio by text hash.
func checkVoiceCache(ctx context.Context, rc *skillmain.RunContext, conversationID, textHash, voiceID string) (string, int, bool) {
	// This would query the companion_generated_voices table
	// For now, return not found - the companion service will handle caching
	return "", 0, false
}

// cacheVoiceResult stores the generated audio in cache.
func cacheVoiceResult(ctx context.Context, rc *skillmain.RunContext, conversationID, textHash, voiceID, digest, model string, durationMS int) {
	// This would insert into companion_generated_voices table
	// For now, the companion service will handle caching through its own memory store
}
