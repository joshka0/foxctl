// Package main implements the presence/background skill.
// Generates mood-based background images using Gemini image generation.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/platform/config"
)

const command = "presence/background"

// Input defines the input parameters for presence/background.
type Input struct {
	Emotion        string `json:"emotion"`
	Prompt         string `json:"prompt"`
	Scene          string `json:"scene"`
	Style          string `json:"style"`
	ConversationID string `json:"conversation_id"`
	UseCache       bool   `json:"use_cache"`
}

// Output defines the output for presence/background.
type Output struct {
	ImageDigest string `json:"image_digest"`
	Prompt      string `json:"prompt"`
	Cached      bool   `json:"cached"`
	Model       string `json:"model"`
	Emotion     string `json:"emotion"`
}

// emotionPrompts maps emotions to background generation prompts.
var emotionPrompts = map[string]string{
	"neutral":  "Calm ambient scene, soft lighting, muted colors, peaceful atmosphere",
	"joy":      "Bright warm scene, golden sunlight, vibrant colors, cheerful atmosphere, sparkles",
	"sadness":  "Moody scene, soft blue tones, gentle rain or mist, contemplative atmosphere",
	"anger":    "Intense scene, deep red and orange tones, dramatic storm clouds, dynamic lighting",
	"fear":     "Dark shadowy scene, cool blue-grey tones, mysterious fog, subtle tension",
	"surprise": "Dynamic scene, bright contrasts, energetic burst of colors, exciting atmosphere",
	"disgust":  "Murky scene, sickly green and brown tones, hazy atmosphere",
	"playful":  "Whimsical scene, pastel colors, fun patterns, lighthearted bubbly atmosphere",
}

// styleModifiers add style-specific instructions to prompts.
var styleModifiers = map[string]string{
	"anime":      "anime art style, cel-shaded, vibrant colors, clean lines",
	"realistic":  "photorealistic, highly detailed, natural lighting, 4K quality",
	"watercolor": "watercolor painting style, soft edges, flowing colors, artistic",
	"minimalist": "minimalist design, simple shapes, clean composition, subtle colors",
}

func main() {
	// Load .env before any API key access
	config.LoadDotEnv()
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Default and normalize values
	if in.Emotion == "" {
		in.Emotion = "neutral"
	} else {
		in.Emotion = strings.ToLower(in.Emotion)
	}
	if in.Style == "" {
		in.Style = "anime"
	}

	// Fail fast: Check CAS store availability before making expensive API calls
	if rc.CASStore == nil {
		return skillerr.Runtime("CAS store not available",
			skillerr.WithHint("CAS store is required for storing generated images"))
	}

	// Get API key from config
	apiKey := rc.Config.LLM.GeminiAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return skillerr.Auth("GEMINI_API_KEY not set",
			skillerr.WithHint("Set GEMINI_API_KEY in environment or ~/.foxctl/.env"))
	}

	// Build prompt
	prompt := buildPrompt(in)
	promptHash := hashPrompt(prompt)

	// Check cache if enabled (only when conversation_id is provided)
	if in.UseCache && in.ConversationID != "" {
		digest, found := checkBackgroundCache(ctx, rc, in.ConversationID, promptHash)
		if found {
			return skillout.Emit(rc, command, Output{
				ImageDigest: digest,
				Prompt:      prompt,
				Cached:      true,
				Model:       "cached",
				Emotion:     in.Emotion,
			})
		}
	}

	// Generate image using Gemini
	var imageData []byte
	var model string
	if err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var e error
		imageData, model, e = generateImageGemini(ctx, apiKey, prompt)
		return e
	}); err != nil {
		return skillerr.WrapRuntime("generate image", err)
	}

	tags := []string{
		"presence",
		"background",
		fmt.Sprintf("emotion:%s", in.Emotion),
		fmt.Sprintf("style:%s", in.Style),
	}
	if in.ConversationID != "" {
		tags = append(tags, fmt.Sprintf("conversation:%s", in.ConversationID))
	}

	obj, err := rc.CASStore.Put(ctx, bytes.NewReader(imageData), "image/png", tags)
	if err != nil {
		return skillerr.WrapIO("store image in CAS", err)
	}

	// Cache the result only when caching is enabled and conversation_id provided
	if in.UseCache && in.ConversationID != "" {
		cacheBackgroundResult(ctx, rc, in.ConversationID, promptHash, obj.Digest, in.Emotion, model)
	}

	return skillout.Emit(rc, command, Output{
		ImageDigest: obj.Digest,
		Prompt:      prompt,
		Cached:      false,
		Model:       model,
		Emotion:     in.Emotion,
	})
}

// buildPrompt constructs the image generation prompt.
func buildPrompt(in Input) string {
	var parts []string

	// Custom prompt takes precedence
	if in.Prompt != "" {
		parts = append(parts, in.Prompt)
	} else {
		// Use emotion template
		emotionPrompt, ok := emotionPrompts[strings.ToLower(in.Emotion)]
		if !ok {
			emotionPrompt = emotionPrompts["neutral"]
		}
		parts = append(parts, emotionPrompt)

		// Add scene context
		if in.Scene != "" {
			parts = append(parts, fmt.Sprintf("Scene: %s", in.Scene))
		}
	}

	// Add style modifier
	styleModifier, ok := styleModifiers[strings.ToLower(in.Style)]
	if ok {
		parts = append(parts, styleModifier)
	}

	// Add background-specific instructions
	parts = append(parts, "Background image only, no characters or text, suitable as chat background")

	return strings.Join(parts, ". ")
}

// hashPrompt creates a deterministic hash of the prompt for caching.
func hashPrompt(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(h[:16]) // Use first 16 bytes for shorter hash
}

// geminiImageRequest is the request body for Gemini image generation.
type geminiImageRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig,omitempty"`
	SafetySettings   []geminiSafetySetting  `json:"safetySettings,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiGenerationConfig struct {
	ResponseMimeType string `json:"responseMimeType,omitempty"`
}

type geminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type geminiImageResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text       string `json:"text,omitempty"`
				InlineData *struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"`
				} `json:"inlineData,omitempty"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// generateImageGemini generates an image using Gemini's image generation.
// Uses gemini-2.0-flash-exp with image output capability.
func generateImageGemini(ctx context.Context, apiKey, prompt string) ([]byte, string, error) {
	// Gemini 2.0 Flash Experimental with native image generation
	model := "gemini-2.0-flash-exp"
	baseURL := "https://generativelanguage.googleapis.com/v1beta"
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", baseURL, model, apiKey)

	reqBody := geminiImageRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: fmt.Sprintf("Generate an image: %s", prompt)},
				},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			ResponseMimeType: "image/png",
		},
		SafetySettings: []geminiSafetySetting{
			{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "BLOCK_MEDIUM_AND_ABOVE"},
			{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "BLOCK_NONE"},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Try to parse structured error before falling back to generic message
		var errResp struct {
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != nil && errResp.Error.Message != "" {
			return nil, "", fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, errResp.Error.Message)
		}
		// Generic error without exposing potentially sensitive response body
		return nil, "", fmt.Errorf("gemini API error (status %d): request failed", resp.StatusCode)
	}

	var geminiResp geminiImageResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return nil, "", fmt.Errorf("unmarshal response: %w", err)
	}

	if geminiResp.Error != nil {
		return nil, "", fmt.Errorf("gemini error: %s (code %d)", geminiResp.Error.Message, geminiResp.Error.Code)
	}

	// Extract image data from response
	for _, candidate := range geminiResp.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "image/") {
				imageData, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err != nil {
					return nil, "", fmt.Errorf("decode base64 image: %w", err)
				}
				return imageData, model, nil
			}
		}
	}

	return nil, "", fmt.Errorf("no image data in response")
}

// checkBackgroundCache looks up cached background by prompt hash.
func checkBackgroundCache(ctx context.Context, rc *skillmain.RunContext, conversationID, promptHash string) (string, bool) {
	// This would query the companion_generated_backgrounds table
	// For now, return not found - the companion service will handle caching
	return "", false
}

// cacheBackgroundResult stores the generated background in cache.
func cacheBackgroundResult(ctx context.Context, rc *skillmain.RunContext, conversationID, promptHash, digest, emotion, model string) {
	// This would insert into companion_generated_backgrounds table
	// For now, the companion service will handle caching through its own memory store
}
