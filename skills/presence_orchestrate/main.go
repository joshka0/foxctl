// Package main implements the presence/orchestrate skill.
// Coordinates presence skills to generate a complete presence bundle.
package main

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const command = "presence/orchestrate"

// Input defines the input parameters for presence/orchestrate.
type Input struct {
	Text               string `json:"text" validate:"required"`
	ConversationID     string `json:"conversation_id" validate:"required"`
	CharacterID        string `json:"character_id"`
	GenerateVoice      bool   `json:"generate_voice"`
	GenerateBackground bool   `json:"generate_background"`
	Scene              string `json:"scene"`
	Style              string `json:"style"`
	VoiceID            string `json:"voice_id"`
}

// Output defines the output for presence/orchestrate.
type Output struct {
	// Parsed emotion data
	Emotion       string   `json:"emotion"`
	Intensity     float64  `json:"intensity"`
	Confidence    float64  `json:"confidence"`
	DisplayText   string   `json:"display_text"`
	Markers       []string `json:"markers,omitempty"`
	DetectedEmoji []string `json:"detected_emoji,omitempty"`
	Method        string   `json:"method"`

	// Parameters for sub-skill calls (companion service runs these in parallel)
	BackgroundParams map[string]any `json:"background_params,omitempty"`
	CharacterParams  map[string]any `json:"character_params,omitempty"`
	VoiceParams      map[string]any `json:"voice_params,omitempty"`
}

// Emotion constants
const (
	EmotionNeutral  = "neutral"
	EmotionJoy      = "joy"
	EmotionSadness  = "sadness"
	EmotionAnger    = "anger"
	EmotionFear     = "fear"
	EmotionSurprise = "surprise"
	EmotionDisgust  = "disgust"
	EmotionPlayful  = "playful"
)

// emotionPriority defines deterministic ordering for emotion tie-breaking.
// Earlier in list = higher priority when counts are equal.
var emotionPriority = []string{
	EmotionJoy,
	EmotionSadness,
	EmotionAnger,
	EmotionFear,
	EmotionSurprise,
	EmotionDisgust,
	EmotionPlayful,
	EmotionNeutral,
}

// emojiToEmotion maps emoji to emotion categories.
var emojiToEmotion = map[string]string{
	// Joy/Happy
	"\U0001F60A": EmotionJoy, // 😊
	"\U0001F604": EmotionJoy, // 😄
	"\U0001F601": EmotionJoy, // 😁
	"\U0001F603": EmotionJoy, // 😃
	"\U0001F606": EmotionJoy, // 😆
	"\U0001F60D": EmotionJoy, // 😍
	"\U0001F970": EmotionJoy, // 🥰
	"\U0001F929": EmotionJoy, // 🤩
	"\U0001F973": EmotionJoy, // 🥳
	"\U0001F642": EmotionJoy, // 🙂

	// Sadness
	"\U0001F622": EmotionSadness, // 😢
	"\U0001F62D": EmotionSadness, // 😭
	"\U0001F625": EmotionSadness, // 😥
	"\U0001F61E": EmotionSadness, // 😞
	"\U0001F614": EmotionSadness, // 😔
	"\U0001F97A": EmotionSadness, // 🥺
	"\U0001F62A": EmotionSadness, // 😪

	// Anger
	"\U0001F620": EmotionAnger, // 😠
	"\U0001F621": EmotionAnger, // 😡
	"\U0001F624": EmotionAnger, // 😤
	"\U0001F92C": EmotionAnger, // 🤬
	"\U0001F47F": EmotionAnger, // 👿

	// Fear
	"\U0001F628": EmotionFear, // 😨
	"\U0001F630": EmotionFear, // 😰
	"\U0001F631": EmotionFear, // 😱
	"\U0001F627": EmotionFear, // 😧
	"\U0001F626": EmotionFear, // 😦

	// Surprise
	"\U0001F62E": EmotionSurprise, // 😮
	"\U0001F632": EmotionSurprise, // 😲
	"\U0001F62F": EmotionSurprise, // 😯
	"\U0001F92F": EmotionSurprise, // 🤯
	"\U0001F633": EmotionSurprise, // 😳

	// Disgust
	"\U0001F922": EmotionDisgust, // 🤢
	"\U0001F92E": EmotionDisgust, // 🤮
	"\U0001F637": EmotionDisgust, // 😷
	"\U0001F612": EmotionDisgust, // 😒

	// Playful
	"\U0001F61C": EmotionPlayful, // 😜
	"\U0001F61D": EmotionPlayful, // 😝
	"\U0001F61B": EmotionPlayful, // 😛
	"\U0001F60F": EmotionPlayful, // 😏
	"\U0001F92A": EmotionPlayful, // 🤪
	"\U0001F608": EmotionPlayful, // 😈
	"\U0001F609": EmotionPlayful, // 😉
}

// markerToEmotion maps common text markers to emotions.
var markerToEmotion = map[string]string{
	// Joy markers
	"laughs":   EmotionJoy,
	"giggles":  EmotionJoy,
	"smiles":   EmotionJoy,
	"grins":    EmotionJoy,
	"chuckles": EmotionJoy,
	"beams":    EmotionJoy,
	"happily":  EmotionJoy,

	// Sadness markers
	"sighs":    EmotionSadness,
	"sadly":    EmotionSadness,
	"tears":    EmotionSadness,
	"cries":    EmotionSadness,
	"sniffles": EmotionSadness,
	"weeps":    EmotionSadness,
	"sobs":     EmotionSadness,

	// Anger markers
	"growls":  EmotionAnger,
	"snaps":   EmotionAnger,
	"huffs":   EmotionAnger,
	"glares":  EmotionAnger,
	"angrily": EmotionAnger,
	"scowls":  EmotionAnger,

	// Fear markers
	"trembles":  EmotionFear,
	"shivers":   EmotionFear,
	"nervously": EmotionFear,
	"anxiously": EmotionFear,
	"fearfully": EmotionFear,
	"gasps":     EmotionFear,

	// Surprise markers
	"blinks":    EmotionSurprise,
	"startled":  EmotionSurprise,
	"shocked":   EmotionSurprise,
	"surprised": EmotionSurprise,

	// Playful markers
	"winks":        EmotionPlayful,
	"teases":       EmotionPlayful,
	"playfully":    EmotionPlayful,
	"mischievously": EmotionPlayful,
	"smirks":       EmotionPlayful,

	// Neutral/other markers
	"pause":   "",
	"pauses":  "",
	"softly":  "",
	"quietly": "",
	"gently":  "",
	"warmly":  "",
	"calmly":  "",
}

// markerRegex matches *marker* patterns
var markerRegex = regexp.MustCompile(`\*([a-zA-Z]+)\*`)

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if strings.TrimSpace(in.Text) == "" {
		return skillerr.Arg("text is required")
	}
	if in.ConversationID == "" {
		return skillerr.Arg("conversation_id is required")
	}

	// Default style
	if in.Style == "" {
		in.Style = "anime"
	}

	// Parse emotion from text
	out := Output{
		Emotion:    EmotionNeutral,
		Intensity:  0.5,
		Confidence: 0.5,
		Method:     "heuristic",
	}

	// Extract emoji
	detectedEmoji, emojiEmotion, emojiIntensity := extractEmoji(in.Text)
	out.DetectedEmoji = detectedEmoji

	// Extract markers
	markers, markerEmotion := extractMarkers(in.Text)
	out.Markers = markers

	// Determine final emotion (emoji takes precedence)
	if emojiEmotion != "" {
		out.Emotion = emojiEmotion
		out.Intensity = emojiIntensity
		out.Confidence = 0.9
		out.Method = "emoji_map"
	} else if markerEmotion != "" {
		out.Emotion = markerEmotion
		out.Intensity = 0.7
		out.Confidence = 0.7
		out.Method = "marker_analysis"
	}

	// Strip markers and emoji for display
	displayText := stripEmoji(in.Text, detectedEmoji)
	displayText = stripMarkers(displayText)
	displayText = strings.TrimSpace(displayText)
	displayText = regexp.MustCompile(`\s+`).ReplaceAllString(displayText, " ")
	out.DisplayText = displayText

	// Build parameters for sub-skills (companion service will call these in parallel)
	if in.GenerateBackground {
		out.BackgroundParams = map[string]any{
			"emotion":         out.Emotion,
			"scene":           in.Scene,
			"style":           in.Style,
			"conversation_id": in.ConversationID,
			"use_cache":       true,
		}
	}

	if in.CharacterID != "" {
		out.CharacterParams = map[string]any{
			"action":       "select",
			"character_id": in.CharacterID,
			"emotion":      out.Emotion,
			"intensity":    out.Intensity,
		}
	}

	if in.GenerateVoice {
		out.VoiceParams = map[string]any{
			"text":            displayText,
			"emotion":         out.Emotion,
			"intensity":       out.Intensity,
			"conversation_id": in.ConversationID,
			"use_cache":       true,
		}
		if in.VoiceID != "" {
			out.VoiceParams["voice_id"] = in.VoiceID
		}
	}

	return skillout.Emit(rc, command, out)
}

// extractEmoji finds emoji in text and returns the dominant emotion.
// Counts all occurrences of each emoji type (not just presence).
func extractEmoji(text string) ([]string, string, float64) {
	found := []string{} // Initialize as empty slice, not nil (nil serializes to null in JSON)
	emotionCounts := make(map[string]int)
	totalOccurrences := 0

	// Count occurrences of each known emoji
	for emoji, emotion := range emojiToEmotion {
		count := strings.Count(text, emoji)
		if count > 0 {
			found = append(found, emoji)
			emotionCounts[emotion] += count
			totalOccurrences += count
		}
	}

	if len(found) == 0 {
		return found, "", 0
	}

	// Sort found emoji for deterministic output
	sort.Strings(found)

	// Find dominant emotion using priority order for deterministic tie-breaking
	maxCount := 0
	dominant := ""
	for _, emotion := range emotionPriority {
		count := emotionCounts[emotion]
		if count > maxCount {
			maxCount = count
			dominant = emotion
		}
	}

	// Intensity based on total emoji occurrences (1=0.6, 2=0.8, 3+=1.0)
	intensity := 0.6
	if totalOccurrences == 2 {
		intensity = 0.8
	} else if totalOccurrences >= 3 {
		intensity = 1.0
	}

	return found, dominant, intensity
}

// extractMarkers finds *marker* patterns and returns the dominant emotion.
func extractMarkers(text string) ([]string, string) {
	matches := markerRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return []string{}, "" // Return empty slice, not nil (nil serializes to null in JSON)
	}

	var markers []string
	emotionCounts := make(map[string]int)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		marker := strings.ToLower(match[1])
		markers = append(markers, marker)

		if emotion, ok := markerToEmotion[marker]; ok && emotion != "" {
			emotionCounts[emotion]++
		}
	}

	// Find dominant emotion using priority order for deterministic tie-breaking
	maxCount := 0
	dominant := ""
	for _, emotion := range emotionPriority {
		count := emotionCounts[emotion]
		if count > maxCount {
			maxCount = count
			dominant = emotion
		}
	}

	return markers, dominant
}

// stripEmoji removes emoji from text.
func stripEmoji(text string, emoji []string) string {
	for _, e := range emoji {
		text = strings.ReplaceAll(text, e, "")
	}
	return text
}

// stripMarkers removes *marker* patterns from text.
func stripMarkers(text string) string {
	return markerRegex.ReplaceAllString(text, "")
}
