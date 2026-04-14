// Package main implements the hooks/knowledge_router skill.
// This skill surfaces relevant knowledge packs based on prompt content and file paths.
// It is advisory only (never blocks) and injects context hints when matches exceed threshold.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/hookutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/context/sessionkit"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/runtime/hooks/pathutil"
	"github.com/joshka0/foxctl/internal/storage/knowledge"
)

// RouterConfig holds configuration for the knowledge router.
// RouterConfig holds configuration for the knowledge router.
type RouterConfig struct {
	Threshold          float64 `json:"threshold"`
	MaxRecommendations int     `json:"max_recommendations"`
}

// DefaultConfig returns default router configuration.
func DefaultConfig() RouterConfig {
	return RouterConfig{
		Threshold:          0.5, // Lower threshold for rule-based matching
		MaxRecommendations: 3,
	}
}

// Match represents a knowledge item match with score.
// Match represents a knowledge item match with score.
type Match struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

// main is the skill entry point for hooks/knowledge_router.
func main() {
	skillmain.Main("hooks/knowledge_router", run)
}

// run orchestrates knowledge pack recommendation based on prompt content and file paths.
//
// Index:
// - Purpose: Surface relevant knowledge packs based on prompt content and file paths with threshold-based filtering
// - Flow: load config → open store → extract search context → find matches → filter by threshold → emit recommendations
// - SideEffects: knowledge store queries; context injection; recommendation scoring
// - FailureModes: store access failures, keyword extraction errors
// - Observability: emits recommendation counts, scores, and configuration info
// - Related: extractPrompt, findMatches, extractKeywords, scoreKeywordMatch
// - Keywords: hooks/knowledge_router, knowledge_recommendation, content_matching, path_matching, threshold_filtering
func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	paths := sessionkit.ResolvePaths(rc.Config)
	routerCfg := DefaultConfig()

	// Load custom config from environment if available
	if threshold := os.Getenv("AGENTCTL_KNOWLEDGE_THRESHOLD"); threshold != "" {
		var t float64
		if _, err := fmt.Sscanf(threshold, "%f", &t); err == nil && t > 0 && t <= 1 {
			routerCfg.Threshold = t
		}
	}

	// Open knowledge store
	store, err := knowledge.Open(ctx, paths.StorageRoot)
	if err != nil {
		// If store doesn't exist yet, emit none with no context
		output := hooks.NewNone()
		output.Reason = "knowledge store not initialized"
		return emitOutput(rc, output, nil, routerCfg)
	}
	defer store.Close()

	// Extract search context from input
	prompt := extractPrompt(in)
	filePath := pathutil.ExtractPath(in.ToolInput)

	// Find matching knowledge items
	matches := findMatches(ctx, store, prompt, filePath)

	// Filter by threshold and limit
	var recommendations []Match
	for _, m := range matches {
		if m.Score >= routerCfg.Threshold {
			recommendations = append(recommendations, m)
			if len(recommendations) >= routerCfg.MaxRecommendations {
				break
			}
		}
	}

	// Build output
	if len(recommendations) == 0 {
		output := hooks.NewNone()
		output.Reason = "no relevant knowledge packs found"
		return emitOutput(rc, output, nil, routerCfg)
	}

	// Build context hint
	var names []string
	for _, r := range recommendations {
		names = append(names, r.Name)
	}
	contextHint := fmt.Sprintf("Recommended knowledge packs: %s", strings.Join(names, ", "))

	output := hooks.Output{
		Decision: hooks.DecisionNone, // Advisory only, never block
		Context:  contextHint,
		Meta: map[string]any{
			"recommended": recommendations,
			"threshold":   routerCfg.Threshold,
		},
	}

	return emitOutput(rc, output, recommendations, routerCfg)
}

// extractPrompt extracts the prompt text from hook input.
// For UserPromptSubmit events, this would be in a "prompt" field.
// For PreToolUse events, we extract from tool_input.
func extractPrompt(in hooks.Input) string {
	if in.Prompt != "" {
		return in.Prompt
	}

	// For PreToolUse, extract meaningful text from tool input
	if len(in.ToolInput) > 0 {
		var input map[string]any
		if err := json.Unmarshal(in.ToolInput, &input); err == nil {
			// Concatenate string values for keyword matching
			var parts []string
			for _, v := range input {
				if s, ok := v.(string); ok {
					parts = append(parts, s)
				}
			}
			return strings.Join(parts, " ")
		}
	}

	return ""
}

// findMatches searches the knowledge store and scores matches.
func findMatches(ctx context.Context, store knowledge.Store, prompt, filePath string) []Match {
	var matches []Match
	seen := make(map[string]bool)

	// Extract keywords from prompt
	keywords := extractKeywords(prompt)

	// Match by keywords
	if len(keywords) > 0 {
		items, err := store.MatchByKeyword(ctx, keywords)
		if err == nil {
			for _, item := range items {
				if !seen[item.ID] {
					score := scoreKeywordMatch(keywords, item)
					matches = append(matches, Match{
						Name:        item.Name,
						Kind:        string(item.Kind),
						Description: item.Description,
						Score:       score,
					})
					seen[item.ID] = true
				}
			}
		}
	}

	// Match by file path
	if filePath != "" {
		items, err := store.MatchByPath(ctx, filePath)
		if err == nil {
			for _, item := range items {
				if !seen[item.ID] {
					matches = append(matches, Match{
						Name:        item.Name,
						Kind:        string(item.Kind),
						Description: item.Description,
						Score:       0.8, // High score for path matches
					})
					seen[item.ID] = true
				} else {
					// Boost score for items that match both keywords and path
					for i := range matches {
						if matches[i].Name == item.Name {
							matches[i].Score = minFloat(1.0, matches[i].Score+0.3)
						}
					}
				}
			}
		}
	}

	// Sort by score descending
	sortMatchesByScore(matches)

	return matches
}

var wordRe = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9_-]{2,}\b`)

// extractKeywords extracts meaningful keywords from text.
func extractKeywords(text string) []string {
	words := wordRe.FindAllString(strings.ToLower(text), -1)

	// Filter out common words
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "this": true, "that": true,
		"with": true, "from": true, "use": true, "when": true, "you": true,
		"are": true, "have": true, "has": true, "will": true, "can": true,
		"file": true, "path": true, "code": true, "new": true, "add": true,
	}

	var keywords []string
	seen := make(map[string]bool)
	for _, w := range words {
		if !stopWords[w] && !seen[w] && len(w) > 2 {
			keywords = append(keywords, w)
			seen[w] = true
		}
	}

	return keywords
}

// scoreKeywordMatch calculates a score based on keyword overlap.
func scoreKeywordMatch(keywords []string, item knowledge.Item) float64 {
	if len(keywords) == 0 {
		return 0
	}

	// Check how many keywords appear in the item name or description
	itemText := strings.ToLower(item.Name + " " + item.Description)
	matchCount := 0
	for _, kw := range keywords {
		if strings.Contains(itemText, kw) {
			matchCount++
		}
	}

	// Base score from trigger match (0.5) + bonus for description matches
	baseScore := 0.5
	bonus := float64(matchCount) / float64(len(keywords)) * 0.4

	return minFloat(1.0, baseScore+bonus)
}

// sortMatchesByScore sorts matches by score in descending order.
func sortMatchesByScore(matches []Match) {
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
}

// minFloat returns the smaller of two float64 values.
func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// emitOutput emits the hook output with optional extras.
func emitOutput(rc *skillmain.RunContext, output hooks.Output, recommendations []Match, cfg RouterConfig) error {
	var extras map[string]any
	if len(recommendations) > 0 {
		extras = map[string]any{
			"recommendations": recommendations,
			"config":          cfg,
		}
	}
	return hookutil.EmitOutput(rc, "hooks/knowledge_router", output, extras)
}
