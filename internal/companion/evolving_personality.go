package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/storage/contextvar"
)

// PersonalityDimension represents an adjustable aspect of the companion's personality.
type PersonalityDimension struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Value       float64 `json:"value"`     // 0.0 to 1.0
	MinLabel    string  `json:"min_label"` // e.g., "formal"
	MaxLabel    string  `json:"max_label"` // e.g., "casual"
}

// DefaultPersonalityDimensions returns the default personality configuration.
func DefaultPersonalityDimensions() []PersonalityDimension {
	return []PersonalityDimension{
		{
			Name:        "formality",
			Description: "How formal vs casual the responses are",
			Value:       0.5,
			MinLabel:    "formal and professional",
			MaxLabel:    "casual and friendly",
		},
		{
			Name:        "verbosity",
			Description: "How detailed vs concise the responses are",
			Value:       0.5,
			MinLabel:    "brief and to-the-point",
			MaxLabel:    "detailed and thorough",
		},
		{
			Name:        "enthusiasm",
			Description: "Energy level in responses",
			Value:       0.6,
			MinLabel:    "calm and measured",
			MaxLabel:    "enthusiastic and energetic",
		},
		{
			Name:        "humor",
			Description: "Use of humor and playfulness",
			Value:       0.3,
			MinLabel:    "serious and straightforward",
			MaxLabel:    "playful and witty",
		},
		{
			Name:        "empathy",
			Description: "Emotional attunement and support",
			Value:       0.7,
			MinLabel:    "task-focused",
			MaxLabel:    "emotionally supportive",
		},
		{
			Name:        "proactivity",
			Description: "How much to offer suggestions and follow-ups",
			Value:       0.5,
			MinLabel:    "responsive only",
			MaxLabel:    "proactive with suggestions",
		},
	}
}

// PersonalityProfile holds the complete personality state.
type PersonalityProfile struct {
	Dimensions    []PersonalityDimension `json:"dimensions"`
	LearnedTraits []string               `json:"learned_traits"` // e.g., "prefers technical depth", "enjoys analogies"
	Interests     []string               `json:"interests"`      // Topics user enjoys
	Dislikes      []string               `json:"dislikes"`       // Things to avoid
	FeedbackLog   []PersonalityFeedback  `json:"feedback_log"`   // Recent feedback for learning
}

// PersonalityFeedback records user feedback for personality adjustment.
type PersonalityFeedback struct {
	Dimension string  `json:"dimension,omitempty"` // Which dimension to adjust
	Direction string  `json:"direction"`           // "increase", "decrease", or "note"
	Amount    float64 `json:"amount,omitempty"`    // How much to adjust (0.1 = subtle, 0.3 = significant)
	Note      string  `json:"note,omitempty"`      // Free-form feedback
	Reason    string  `json:"reason,omitempty"`    // Why this adjustment
}

// EvolvingPersonality manages dynamic personality adaptation.
type EvolvingPersonality struct {
	store          contextvar.Store
	conversationID string
}

// NewEvolvingPersonality creates a new evolving personality manager.
func NewEvolvingPersonality(store contextvar.Store, conversationID string) *EvolvingPersonality {
	return &EvolvingPersonality{
		store:          store,
		conversationID: conversationID,
	}
}

// GetProfile retrieves the current personality profile from context.
func (e *EvolvingPersonality) GetProfile(ctx context.Context) (*PersonalityProfile, error) {
	// Try to load from global scope (persists across conversations)
	v, err := e.store.GetByKey(ctx, e.conversationID, contextvar.ScopeGlobal, "personality/profile")
	if err != nil {
		if errors.Is(err, contextvar.ErrNotFound) {
			// Return default profile
			return &PersonalityProfile{
				Dimensions: DefaultPersonalityDimensions(),
			}, nil
		}
		return nil, err
	}

	var profile PersonalityProfile
	if err := json.Unmarshal(v.ValueJSON, &profile); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}

	// Ensure all dimensions exist (in case new ones were added)
	profile.Dimensions = mergeDimensions(profile.Dimensions, DefaultPersonalityDimensions())

	return &profile, nil
}

// SaveProfile persists the personality profile.
func (e *EvolvingPersonality) SaveProfile(ctx context.Context, profile *PersonalityProfile) error {
	_, err := e.store.Put(ctx, contextvar.PutParams{
		ConversationID: e.conversationID,
		Scope:          contextvar.ScopeGlobal,
		Key:            "personality/profile",
		Value:          profile,
		Source:         "evolving_personality",
		Upsert:         true,
	})
	return err
}

// ApplyFeedback adjusts the personality based on feedback.
func (e *EvolvingPersonality) ApplyFeedback(ctx context.Context, feedback PersonalityFeedback) error {
	profile, err := e.GetProfile(ctx)
	if err != nil {
		return err
	}

	// Apply dimension adjustment only for explicit increase/decrease directions.
	// "note" and other non-actionable directions should not modify dimension values.
	if feedback.Dimension != "" {
		for i := range profile.Dimensions {
			if profile.Dimensions[i].Name == feedback.Dimension {
				switch feedback.Direction {
				case "increase":
					delta := feedback.Amount
					if delta == 0 {
						delta = 0.1 // Default subtle adjustment
					}
					profile.Dimensions[i].Value = clamp(profile.Dimensions[i].Value+delta, 0, 1)
				case "decrease":
					delta := feedback.Amount
					if delta == 0 {
						delta = 0.1 // Default subtle adjustment
					}
					profile.Dimensions[i].Value = clamp(profile.Dimensions[i].Value-delta, 0, 1)
					// "note" and other directions: do not modify dimension value
				}
				break
			}
		}
	}

	// Add note to learned traits if significant
	if feedback.Note != "" {
		profile.LearnedTraits = appendUnique(profile.LearnedTraits, feedback.Note)
		// Keep only last 10 traits
		if len(profile.LearnedTraits) > 10 {
			profile.LearnedTraits = profile.LearnedTraits[len(profile.LearnedTraits)-10:]
		}
	}

	// Log feedback for analysis
	profile.FeedbackLog = append(profile.FeedbackLog, feedback)
	// Keep only last 20 feedback entries
	if len(profile.FeedbackLog) > 20 {
		profile.FeedbackLog = profile.FeedbackLog[len(profile.FeedbackLog)-20:]
	}

	return e.SaveProfile(ctx, profile)
}

// AddInterest adds a topic the user enjoys.
func (e *EvolvingPersonality) AddInterest(ctx context.Context, interest string) error {
	profile, err := e.GetProfile(ctx)
	if err != nil {
		return err
	}
	profile.Interests = appendUnique(profile.Interests, interest)
	return e.SaveProfile(ctx, profile)
}

// AddDislike adds something the user doesn't want.
func (e *EvolvingPersonality) AddDislike(ctx context.Context, dislike string) error {
	profile, err := e.GetProfile(ctx)
	if err != nil {
		return err
	}
	profile.Dislikes = appendUnique(profile.Dislikes, dislike)
	return e.SaveProfile(ctx, profile)
}

// BuildSystemPrompt generates a dynamic system prompt based on the profile.
func (e *EvolvingPersonality) BuildSystemPrompt(ctx context.Context, basePrompt string) (string, error) {
	profile, err := e.GetProfile(ctx)
	if err != nil {
		return basePrompt, err
	}

	var parts []string
	parts = append(parts, basePrompt)

	// Add personality style instructions
	parts = append(parts, "\n## Your Communication Style")
	parts = append(parts, "Based on learned preferences, adjust your responses:")

	for _, dim := range profile.Dimensions {
		style := describeStyle(dim)
		if style != "" {
			parts = append(parts, fmt.Sprintf("- %s", style))
		}
	}

	// Add learned traits
	if len(profile.LearnedTraits) > 0 {
		parts = append(parts, "\n## Learned Preferences")
		for _, trait := range profile.LearnedTraits {
			parts = append(parts, fmt.Sprintf("- %s", trait))
		}
	}

	// Add interests
	if len(profile.Interests) > 0 {
		parts = append(parts, fmt.Sprintf("\n## User Interests\nTopics they enjoy: %s", strings.Join(profile.Interests, ", ")))
	}

	// Add dislikes
	if len(profile.Dislikes) > 0 {
		parts = append(parts, fmt.Sprintf("\n## Things to Avoid\n%s", strings.Join(profile.Dislikes, ", ")))
	}

	return strings.Join(parts, "\n"), nil
}

// describeStyle generates a natural language description of a dimension's current value.
func describeStyle(dim PersonalityDimension) string {
	if dim.Value < 0.3 {
		return fmt.Sprintf("Be %s (%s)", dim.MinLabel, dim.Name)
	} else if dim.Value > 0.7 {
		return fmt.Sprintf("Be %s (%s)", dim.MaxLabel, dim.Name)
	}
	// Middle ground - no strong preference
	return ""
}

// mergeDimensions ensures all default dimensions exist in the profile.
func mergeDimensions(existing, defaults []PersonalityDimension) []PersonalityDimension {
	dimMap := make(map[string]PersonalityDimension)
	for _, d := range defaults {
		dimMap[d.Name] = d
	}
	for _, d := range existing {
		dimMap[d.Name] = d
	}
	result := make([]PersonalityDimension, 0, len(dimMap))
	for _, d := range dimMap {
		result = append(result, d)
	}
	return result
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func appendUnique(slice []string, item string) []string {
	for _, existing := range slice {
		if strings.EqualFold(existing, item) {
			return slice
		}
	}
	return append(slice, item)
}

// PersonalityAdjustmentToolDef returns the tool definition for personality adjustment.
// This allows the LLM to suggest personality changes based on conversation.
func PersonalityAdjustmentToolDef() map[string]interface{} {
	return map[string]interface{}{
		"name":        "rlm_personality_adjust",
		"description": "Adjust the companion's personality based on user feedback or observed preferences. Use this when the user expresses preferences about how you communicate, or when you notice patterns in what they respond well to.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"dimension": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"formality", "verbosity", "enthusiasm", "humor", "empathy", "proactivity"},
					"description": "Which personality dimension to adjust",
				},
				"direction": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"increase", "decrease", "note"},
					"description": "Direction of adjustment, or 'note' to record an observation",
				},
				"amount": map[string]interface{}{
					"type":        "number",
					"description": "Adjustment amount (0.1=subtle, 0.2=moderate, 0.3=significant). Default 0.1",
				},
				"note": map[string]interface{}{
					"type":        "string",
					"description": "Free-form note about user preference (e.g., 'prefers technical explanations', 'enjoys analogies')",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Why this adjustment is being made",
				},
			},
			"required": []string{"direction"},
		},
	}
}
