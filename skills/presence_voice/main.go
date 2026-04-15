// Package main implements the presence/voice skill.
// Generates speech audio using ElevenLabs or Pocket TTS with optional LLM rewrite.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/platform/config"
)

const command = "presence/voice"

// Default voice ID (Rachel - warm, expressive female voice)
const defaultVoiceID = "21m00Tcm4TlvDq8ikWAM"

const (
	providerElevenLabs     = "elevenlabs"
	providerPocketTTS      = "pocket"
	defaultElevenLabsModel = "eleven_multilingual_v2"
	defaultPocketBaseURL   = "http://127.0.0.1:18765"
	defaultRewriteModel    = "minimax/minimax-m2-her"
	defaultRewriteMaxChars = 280
	defaultRewriteBaseURL  = "https://openrouter.ai/api/v1"
)

var whitespaceRE = regexp.MustCompile(`\s+`)

// Input defines the input parameters for presence/voice.
type Input struct {
	Text            string  `json:"text" validate:"required,max=5000"`
	VoiceID         string  `json:"voice_id"`
	Emotion         string  `json:"emotion"`
	Intensity       float64 `json:"intensity"`
	ConversationID  string  `json:"conversation_id"`
	UseCache        bool    `json:"use_cache"`
	Model           string  `json:"model"`
	Provider        string  `json:"provider"`
	PocketBaseURL   string  `json:"pocket_base_url"`
	RewriteForTTS   bool    `json:"rewrite_for_tts"`
	RewriteModel    string  `json:"rewrite_model"`
	RewriteMaxChars int     `json:"rewrite_max_chars"`
	RewritePrompt   string  `json:"rewrite_prompt"`
	RewriteBaseURL  string  `json:"rewrite_base_url"`
}

// Output defines the output for presence/voice.
type Output struct {
	AudioDigest    string `json:"audio_digest"`
	DurationMS     int    `json:"duration_ms"`
	Cached         bool   `json:"cached"`
	Model          string `json:"model"`
	VoiceID        string `json:"voice_id"`
	Provider       string `json:"provider"`
	RewriteApplied bool   `json:"rewrite_applied"`
	RewriteModel   string `json:"rewrite_model,omitempty"`
	RewriteError   string `json:"rewrite_error,omitempty"`
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
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return skillerr.Arg("text is required")
	}
	if len(text) > 5000 {
		return skillerr.Arg("text exceeds 5000 character limit")
	}

	// Fail fast: Check CAS store availability before making expensive API calls
	if rc.CASStore == nil {
		return skillerr.Runtime("CAS store not available",
			skillerr.WithHint("CAS store is required for storing generated audio"))
	}

	provider, err := resolveProvider(in.Provider)
	if err != nil {
		return skillerr.Arg(err.Error())
	}

	// Default and normalize values
	voiceID := in.VoiceID
	if provider == providerElevenLabs && voiceID == "" {
		voiceID = defaultVoiceID
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		if provider == providerElevenLabs {
			model = defaultElevenLabsModel
		} else {
			model = "pocket-tts"
		}
	}
	if in.Intensity <= 0 {
		in.Intensity = 0.5
	}
	// Normalize emotion for consistent caching and voice settings lookup
	if in.Emotion != "" {
		in.Emotion = strings.ToLower(in.Emotion)
	}

	rewriteApplied := false
	rewriteModelUsed := ""
	rewriteError := ""
	finalText := text

	if in.RewriteForTTS {
		rewriteModel := strings.TrimSpace(in.RewriteModel)
		if rewriteModel == "" {
			rewriteModel = defaultRewriteModel
		}
		rewriteMaxChars := in.RewriteMaxChars
		if rewriteMaxChars <= 0 {
			rewriteMaxChars = defaultRewriteMaxChars
		}

		rewriteBaseURL := resolveRewriteBaseURL(in.RewriteBaseURL)
		rewritten, rewriteErr := rewriteTextForTTS(ctx, rc, finalText, rewriteModel, rewriteMaxChars, in.RewritePrompt, rewriteBaseURL)
		if rewriteErr != nil {
			rewriteError = rewriteErr.Error()
			rc.Logger.Warn().Err(rewriteErr).Msg("presence/voice rewrite_for_tts failed; using original text")
		} else {
			finalText = rewritten
			rewriteApplied = true
			rewriteModelUsed = rewriteModel
		}
	}

	// Create cache key from provider + text + voice + emotion
	textHash := hashText(finalText, provider, voiceID, in.Emotion, in.Intensity, model)

	// Check cache if enabled
	if in.UseCache && in.ConversationID != "" && rc.CASStore != nil {
		digest, durationMS, found := checkVoiceCache(ctx, rc, in.ConversationID, textHash, provider, voiceID)
		if found {
			return skillout.Emit(rc, command, Output{
				AudioDigest:    digest,
				DurationMS:     durationMS,
				Cached:         true,
				Model:          "cached",
				VoiceID:        voiceID,
				Provider:       provider,
				RewriteApplied: rewriteApplied,
				RewriteModel:   rewriteModelUsed,
				RewriteError:   rewriteError,
			})
		}
	}

	audioKind := "audio/mpeg"
	var audioData []byte
	if err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		switch provider {
		case providerElevenLabs:
			apiKey := rc.Config.LLM.ElevenLabsAPIKey
			if apiKey == "" {
				apiKey = os.Getenv("ELEVENLABS_API_KEY")
			}
			if apiKey == "" {
				return skillerr.Auth("ELEVENLABS_API_KEY not set",
					skillerr.WithHint("Set ELEVENLABS_API_KEY in environment or ~/.foxctl/.env"))
			}

			settings := getVoiceSettings(in.Emotion, in.Intensity)
			var e error
			audioData, e = generateSpeechElevenLabs(ctx, apiKey, voiceID, model, finalText, settings)
			audioKind = "audio/mpeg"
			return e
		case providerPocketTTS:
			baseURL := resolvePocketBaseURL(in.PocketBaseURL)
			var e error
			audioData, e = generateSpeechPocket(ctx, baseURL, voiceID, finalText)
			audioKind = "audio/wav"
			return e
		default:
			return fmt.Errorf("unsupported provider: %s", provider)
		}
	}); err != nil {
		return skillerr.WrapRuntime("generate speech", err)
	}

	// Estimate duration (rough: ~150 chars per second at normal speed)
	durationMS := len(finalText) * 1000 / 150
	if durationMS < 500 {
		durationMS = 500
	}

	// Store in CAS (already validated at function start)
	tags := []string{
		"presence",
		"voice",
		fmt.Sprintf("provider:%s", provider),
	}
	if voiceID != "" {
		tags = append(tags, fmt.Sprintf("voice_id:%s", voiceID))
	}
	if in.Emotion != "" {
		tags = append(tags, fmt.Sprintf("emotion:%s", in.Emotion))
	}
	if in.ConversationID != "" {
		tags = append(tags, fmt.Sprintf("conversation:%s", in.ConversationID))
	}
	if rewriteApplied && rewriteModelUsed != "" {
		tags = append(tags, fmt.Sprintf("rewrite_model:%s", rewriteModelUsed))
	}

	obj, err := rc.CASStore.Put(ctx, bytes.NewReader(audioData), audioKind, tags)
	if err != nil {
		return skillerr.WrapIO("store audio in CAS", err)
	}

	// Cache the result only when caching is enabled and conversation_id provided
	if in.UseCache && in.ConversationID != "" {
		cacheVoiceResult(ctx, rc, in.ConversationID, textHash, provider, voiceID, obj.Digest, model, durationMS)
	}

	return skillout.Emit(rc, command, Output{
		AudioDigest:    obj.Digest,
		DurationMS:     durationMS,
		Cached:         false,
		Model:          model,
		VoiceID:        voiceID,
		Provider:       provider,
		RewriteApplied: rewriteApplied,
		RewriteModel:   rewriteModelUsed,
		RewriteError:   rewriteError,
	})
}

// hashText creates a deterministic hash for caching.
func hashText(text, provider, voiceID, emotion string, intensity float64, model string) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%.2f|%s", text, provider, voiceID, emotion, intensity, model)
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

// generateSpeechElevenLabs calls ElevenLabs API to generate audio.
func generateSpeechElevenLabs(ctx context.Context, apiKey, voiceID, model, text string, settings VoiceSettings) ([]byte, error) {
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

// generateSpeechPocket calls a local Pocket TTS server and returns WAV bytes.
func generateSpeechPocket(ctx context.Context, baseURL, voiceID, text string) ([]byte, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultPocketBaseURL
	}
	endpoint := baseURL + "/tts"

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("text", text); err != nil {
		return nil, fmt.Errorf("write text field: %w", err)
	}
	if strings.TrimSpace(voiceID) != "" {
		if err := writer.WriteField("voice_url", strings.TrimSpace(voiceID)); err != nil {
			return nil, fmt.Errorf("write voice_url field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "audio/wav")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Detail any `json:"detail"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Detail != nil {
			return nil, fmt.Errorf("pocket-tts API error (status %d): %v", resp.StatusCode, errResp.Detail)
		}
		return nil, fmt.Errorf("pocket-tts API error (status %d): request failed", resp.StatusCode)
	}

	return respBody, nil
}

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Temperature float64             `json:"temperature,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// rewriteTextForTTS rewrites verbose text into short conversational speech.
func rewriteTextForTTS(ctx context.Context, rc *skillmain.RunContext, text, model string, maxChars int, rewritePrompt, rewriteBaseURL string) (string, error) {
	rewriteBaseURL = strings.TrimSpace(rewriteBaseURL)
	if rewriteBaseURL == "" {
		rewriteBaseURL = defaultRewriteBaseURL
	}
	rewriteBaseURL = strings.TrimRight(rewriteBaseURL, "/")
	endpoint := rewriteBaseURL + "/chat/completions"

	apiKey := resolveRewriteAPIKey(rc, rewriteBaseURL)

	reqBody := openAIChatRequest{
		Model: model,
		Messages: []openAIChatMessage{
			{
				Role:    "system",
				Content: buildRewriteSystemPrompt(maxChars, rewritePrompt),
			},
			{
				Role:    "user",
				Content: text,
			},
		},
		Temperature: 0.4,
		MaxTokens:   220,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal rewrite request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create rewrite request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if strings.Contains(rewriteBaseURL, "openrouter.ai") {
		req.Header.Set("HTTP-Referer", "https://foxctl.local")
		req.Header.Set("X-Title", "foxctl-presence-voice")
	}

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("rewrite API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read rewrite response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp openAIChatResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != nil && errResp.Error.Message != "" {
			return "", fmt.Errorf("rewrite API error (status %d): %s", resp.StatusCode, errResp.Error.Message)
		}
		return "", fmt.Errorf("rewrite API error (status %d): request failed", resp.StatusCode)
	}

	var chatResp openAIChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("parse rewrite response: %w", err)
	}
	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return "", fmt.Errorf("rewrite API error: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("rewrite API returned no choices")
	}

	rewritten := normalizeRewriteText(chatResp.Choices[0].Message.Content, maxChars)
	if rewritten == "" {
		return "", fmt.Errorf("rewrite output was empty")
	}
	return rewritten, nil
}

func buildRewriteSystemPrompt(maxChars int, rewritePrompt string) string {
	base := fmt.Sprintf(
		"Rewrite assistant text for spoken TTS.\n"+
			"Goal: a short, human-sounding spoken summary.\n"+
			"Rules:\n"+
			"- Keep it concise (TL;DR style), usually 1-2 short sentences.\n"+
			"- If the source contains a question, prioritize the question and keep surrounding context minimal.\n"+
			"- Sound natural and warm; light conversational connectors like 'so' are okay, but do not overdo filler.\n"+
			"- Preserve intent and emotional tone.\n"+
			"- Return plain text only: no markdown, no bullet points, no stage directions, no emojis, no surrounding quotes.\n"+
			"- Hard limit: %d characters.",
		maxChars,
	)
	extra := strings.TrimSpace(rewritePrompt)
	if extra == "" {
		return base
	}
	return base + "\nAdditional style requirements:\n" + extra
}

func resolveRewriteBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = strings.TrimSpace(os.Getenv("FOXCTL_REWRITE_BASE_URL"))
	}
	if base == "" {
		base = strings.TrimSpace(os.Getenv("LMSTUDIO_BASE_URL"))
	}
	if base == "" {
		base = defaultRewriteBaseURL
	}
	return strings.TrimRight(base, "/")
}

func resolveRewriteAPIKey(rc *skillmain.RunContext, rewriteBaseURL string) string {
	rewriteBaseURL = strings.TrimSpace(rewriteBaseURL)
	if strings.Contains(rewriteBaseURL, "openrouter.ai") {
		key := rc.Config.LLM.ResolveAPIKey("openrouter")
		if key == "" {
			key = os.Getenv("OPENROUTER_API_KEY")
		}
		return key
	}
	if key := os.Getenv("FOXCTL_REWRITE_API_KEY"); key != "" {
		return key
	}
	// LM Studio and many local OpenAI-compatible servers do not require auth.
	return ""
}

func normalizeRewriteText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, `"'`)
	text = whitespaceRE.ReplaceAllString(text, " ")
	return clampToMaxChars(text, maxChars)
}

func clampToMaxChars(text string, maxChars int) string {
	if maxChars <= 0 {
		return strings.TrimSpace(text)
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxChars {
		return string(runes)
	}

	segment := runes[:maxChars]
	cut := len(segment)
	for i := len(segment) - 1; i >= 0; i-- {
		switch segment[i] {
		case '.', '!', '?':
			cut = i + 1
			i = -1
		case ' ', ',', ';', ':':
			if cut == len(segment) {
				cut = i
			}
		}
	}
	if cut <= 0 {
		cut = len(segment)
	}
	return strings.TrimSpace(string(segment[:cut]))
}

func resolveProvider(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", providerElevenLabs, "eleven", "eleven-labs":
		return providerElevenLabs, nil
	case providerPocketTTS, "pocket-tts", "pocket_tts":
		return providerPocketTTS, nil
	default:
		return "", fmt.Errorf("provider must be one of: elevenlabs, pocket")
	}
}

func resolvePocketBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = strings.TrimSpace(os.Getenv("POCKET_TTS_BASE_URL"))
	}
	if base == "" {
		base = strings.TrimSpace(os.Getenv("POCKET_TTS_URL"))
	}
	if base == "" {
		base = defaultPocketBaseURL
	}
	return strings.TrimRight(base, "/")
}

// checkVoiceCache looks up cached audio by text hash.
func checkVoiceCache(ctx context.Context, rc *skillmain.RunContext, conversationID, textHash, provider, voiceID string) (string, int, bool) {
	// This would query the companion_generated_voices table
	// For now, return not found - the companion service will handle caching
	return "", 0, false
}

// cacheVoiceResult stores the generated audio in cache.
func cacheVoiceResult(ctx context.Context, rc *skillmain.RunContext, conversationID, textHash, provider, voiceID, digest, model string, durationMS int) {
	// This would insert into companion_generated_voices table
	// For now, the companion service will handle caching through its own memory store
}
