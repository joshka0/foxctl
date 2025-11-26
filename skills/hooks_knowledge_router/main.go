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
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/hook"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/knowledge"
)

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
type Match struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("hooks/knowledge_router", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("hooks/knowledge_router", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in hook.Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("hooks/knowledge_router", "EARG", fmt.Errorf("decode input: %w", err))
	}

	if err := run(ctx, rc, cfg, in); err != nil {
		fail("hooks/knowledge_router", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, cfg config.Config, in hook.Input) error {
	routerCfg := DefaultConfig()

	// Load custom config from environment if available
	if threshold := os.Getenv("AGENTCTL_KNOWLEDGE_THRESHOLD"); threshold != "" {
		var t float64
		if _, err := fmt.Sscanf(threshold, "%f", &t); err == nil && t > 0 && t <= 1 {
			routerCfg.Threshold = t
		}
	}

	// Open knowledge store
	store, err := knowledge.Open(ctx, cfg.Storage.Root)
	if err != nil {
		// If store doesn't exist yet, emit none with no context
		output := hook.NewNone()
		output.Reason = "knowledge store not initialized"
		return emitOutput(rc, output, nil, routerCfg)
	}
	defer func() { _ = store.Close() }()

	// Extract search context from input
	prompt := extractPrompt(in)
	filePath := extractFilePath(in.ToolInput)

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
		output := hook.NewNone()
		output.Reason = "no relevant knowledge packs found"
		return emitOutput(rc, output, nil, routerCfg)
	}

	// Build context hint
	var names []string
	for _, r := range recommendations {
		names = append(names, r.Name)
	}
	contextHint := fmt.Sprintf("Recommended knowledge packs: %s", strings.Join(names, ", "))

	output := hook.Output{
		Decision: hook.DecisionNone, // Advisory only, never block
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
func extractPrompt(in hook.Input) string {
	// Try to get prompt from a dedicated field (for future UserPromptSubmit support)
	if len(in.ToolInput) > 0 {
		var input struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(in.ToolInput, &input); err == nil && input.Prompt != "" {
			return input.Prompt
		}
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

// extractFilePath extracts the file_path from tool input JSON.
func extractFilePath(toolInput json.RawMessage) string {
	if len(toolInput) == 0 {
		return ""
	}

	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return ""
	}
	return input.FilePath
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
							matches[i].Score = min(1.0, matches[i].Score+0.3)
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

	return min(1.0, baseScore+bonus)
}

// sortMatchesByScore sorts matches by score in descending order.
func sortMatchesByScore(matches []Match) {
	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].Score > matches[i].Score {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
}

func emitOutput(rc *runner.RunnerContext, output hook.Output, recommendations []Match, cfg RouterConfig) error {
	data := map[string]any{
		"hook_output": output,
	}
	if len(recommendations) > 0 {
		data["recommendations"] = recommendations
		data["config"] = cfg
	}
	return rc.Emit("hooks/knowledge_router", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit hook failure")
	os.Exit(1)
}
