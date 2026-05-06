// Package main implements the presence/parse skill.
// Parses text for emoji and emotion markers to extract structured emotion data.
package main

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

// Input defines the input parameters for presence/parse with text and emotion filtering.
type Input struct {
	Text            string   `json:"text" validate:"required"`
	StripMarkers    bool     `json:"strip_markers"`
	AllowedEmotions []string `json:"allowed_emotions"`
}

// Output defines the output for presence/parse with emotion analysis results.
type Output struct {
	Emotion       string   `json:"emotion"`
	Intensity     float64  `json:"intensity"`
	Confidence    float64  `json:"confidence"`
	StrippedText  string   `json:"stripped_text"`
	DetectedEmoji []string `json:"detected_emoji"`
	Markers       []string `json:"markers"`
	Method        string   `json:"method"`
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
	"winks":         EmotionPlayful,
	"teases":        EmotionPlayful,
	"playfully":     EmotionPlayful,
	"mischievously": EmotionPlayful,
	"smirks":        EmotionPlayful,

	// Neutral/other markers (don't affect emotion but are extracted)
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

// main is the skill entry point for presence/parse.
func main() {
	skillmain.Main("presence/parse", run)
}

// run orchestrates emotion detection from emoji and text markers with configurable filtering.
//
// Index:
//   Purpose: Parse text for emoji and emotion markers to extract structured emotion data with confidence scores
//   Keywords: presence/parse, emotion_detection, emoji_parsing, marker_analysis, sentiment_analysis
//   Related: extractEmoji, extractMarkers, stripEmoji, stripMarkers
//   Flow: build allowed emotions → extract emoji → extract markers → determine dominant emotion → apply filters → strip markers
//   Resources: emoji and marker mapping tables
//   Events: emotion parsing events
//   OutputFields: emotion, intensity, confidence, stripped_text, detected_emoji, markers, method
// [[domain:emotion-parsing]]
// [[invariant:emoji-precedence-over-markers]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	out := Output{
		Emotion:       EmotionNeutral,
		Intensity:     0.5,
		Confidence:    0.5,
		StrippedText:  in.Text,
		DetectedEmoji: []string{},
		Markers:       []string{},
		Method:        "heuristic",
	}

	// Build allowed emotions set if specified
	allowedSet := make(map[string]bool)
	if len(in.AllowedEmotions) > 0 {
		for _, e := range in.AllowedEmotions {
			allowedSet[strings.ToLower(e)] = true
		}
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

	// Apply emotion filter if specified
	if len(allowedSet) > 0 && !allowedSet[out.Emotion] {
		// Find closest allowed emotion using deterministic order
		// First check if neutral is allowed
		if allowedSet[EmotionNeutral] {
			out.Emotion = EmotionNeutral
		} else {
			// Pick first allowed emotion from caller's order (preserves input order)
			for _, e := range in.AllowedEmotions {
				if allowedSet[strings.ToLower(e)] {
					out.Emotion = strings.ToLower(e)
					break
				}
			}
		}
		out.Confidence *= 0.5 // Lower confidence when constrained
	}

	// Strip markers if requested
	if in.StripMarkers {
		out.StrippedText = stripEmoji(in.Text, detectedEmoji)
		out.StrippedText = stripMarkers(out.StrippedText)
		out.StrippedText = strings.TrimSpace(out.StrippedText)
		// Clean up multiple spaces
		out.StrippedText = regexp.MustCompile(`\s+`).ReplaceAllString(out.StrippedText, " ")
	}

	return skillout.Emit(rc, "presence/parse", map[string]any{
		"emotion":        out.Emotion,
		"intensity":      out.Intensity,
		"confidence":     out.Confidence,
		"stripped_text":  out.StrippedText,
		"detected_emoji": out.DetectedEmoji,
		"markers":        out.Markers,
		"method":         out.Method,
	})
}

// extractEmoji finds emoji in text and returns the dominant emotion with intensity scoring.
func extractEmoji(text string) ([]string, string, float64) {
	var found []string
	emotionCounts := make(map[string]int)

	// Check each known emoji (iteration order doesn't matter, we sort found later)
	for emoji, emotion := range emojiToEmotion {
		if strings.Contains(text, emoji) {
			found = append(found, emoji)
			emotionCounts[emotion]++
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

	// Intensity based on emoji count (1=0.6, 2=0.8, 3+=1.0)
	intensity := 0.6
	if len(found) == 2 {
		intensity = 0.8
	} else if len(found) >= 3 {
		intensity = 1.0
	}

	return found, dominant, intensity
}

// extractMarkers finds *marker* patterns and returns the dominant emotion.
func extractMarkers(text string) ([]string, string) {
	matches := markerRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return []string{}, "" // Return empty slice for consistent JSON output
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

// stripEmoji removes emoji from text to clean content for further processing.
func stripEmoji(text string, emoji []string) string {
	for _, e := range emoji {
		text = strings.ReplaceAll(text, e, "")
	}
	return text
}

// stripMarkers removes *marker* patterns from text for content cleaning.
func stripMarkers(text string) string {
	return markerRegex.ReplaceAllString(text, "")
}
