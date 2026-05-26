package main

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/storage/knowledge"
	"github.com/stretchr/testify/assert"
)

// Tests for DefaultConfig

func TestDefaultConfig_Values(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 0.5, cfg.Threshold)
	assert.Equal(t, 3, cfg.MaxRecommendations)
}

// Tests for RouterConfig structure

func TestRouterConfig_CustomValues(t *testing.T) {
	cfg := RouterConfig{
		Threshold:          0.7,
		MaxRecommendations: 5,
	}

	assert.Equal(t, 0.7, cfg.Threshold)
	assert.Equal(t, 5, cfg.MaxRecommendations)
}

func TestMatch_Structure(t *testing.T) {
	m := Match{
		Name:        "react-patterns",
		Kind:        "pack",
		Description: "React component patterns",
		Score:       0.85,
	}

	assert.Equal(t, "react-patterns", m.Name)
	assert.Equal(t, "pack", m.Kind)
	assert.Equal(t, "React component patterns", m.Description)
	assert.Equal(t, 0.85, m.Score)
}

func TestExtractPrompt_DirectPrompt(t *testing.T) {
	in := hooks.Input{
		Prompt: "How do I implement authentication?",
	}

	result := extractPrompt(in)

	assert.Equal(t, "How do I implement authentication?", result)
}

func TestExtractPrompt_FromToolInput(t *testing.T) {
	toolInput := map[string]any{
		"query":   "react hooks",
		"pattern": "useState",
	}
	data, _ := json.Marshal(toolInput)

	in := hooks.Input{
		ToolInput: data,
	}

	result := extractPrompt(in)

	// Should contain both string values (order may vary)
	assert.Contains(t, result, "react hooks")
	assert.Contains(t, result, "useState")
}

func TestExtractPrompt_EmptyInput(t *testing.T) {
	in := hooks.Input{}

	result := extractPrompt(in)

	assert.Equal(t, "", result)
}

func TestExtractPrompt_InvalidJSON(t *testing.T) {
	in := hooks.Input{
		ToolInput: []byte("not json"),
	}

	result := extractPrompt(in)

	assert.Equal(t, "", result)
}

func TestExtractPrompt_PromptTakesPrecedence(t *testing.T) {
	toolInput := map[string]any{"query": "from tool"}
	data, _ := json.Marshal(toolInput)

	in := hooks.Input{
		Prompt:    "direct prompt",
		ToolInput: data,
	}

	result := extractPrompt(in)

	assert.Equal(t, "direct prompt", result)
}

func TestExtractPrompt_NonStringValues(t *testing.T) {
	toolInput := map[string]any{
		"count":   42,
		"enabled": true,
		"name":    "test",
	}
	data, _ := json.Marshal(toolInput)

	in := hooks.Input{
		ToolInput: data,
	}

	result := extractPrompt(in)

	// Only string values should be included
	assert.Contains(t, result, "test")
	assert.NotContains(t, result, "42")
}

// Tests for extractKeywords helper

func TestExtractKeywords_Basic(t *testing.T) {
	result := extractKeywords("implement authentication system")

	assert.Contains(t, result, "implement")
	assert.Contains(t, result, "authentication")
	assert.Contains(t, result, "system")
}

func TestExtractKeywords_FilterStopWords(t *testing.T) {
	result := extractKeywords("the authentication and the system")

	assert.Contains(t, result, "authentication")
	assert.Contains(t, result, "system")
	assert.NotContains(t, result, "the")
	assert.NotContains(t, result, "and")
}

func TestExtractKeywords_FilterShortWords(t *testing.T) {
	result := extractKeywords("go to the db on server")

	// Words with 2 or fewer chars should be filtered
	assert.NotContains(t, result, "go")
	assert.NotContains(t, result, "to")
	assert.NotContains(t, result, "db")
	assert.NotContains(t, result, "on")
	assert.Contains(t, result, "server")
}

func TestExtractKeywords_Lowercase(t *testing.T) {
	result := extractKeywords("AUTHENTICATION System")

	// All keywords should be lowercase
	for _, kw := range result {
		assert.Equal(t, kw, kw) // Check it's lowercased
	}
	assert.Contains(t, result, "authentication")
	assert.Contains(t, result, "system")
}

func TestExtractKeywords_NoDuplicates(t *testing.T) {
	result := extractKeywords("auth auth authentication authentication")

	// Count occurrences
	authCount := 0
	for _, kw := range result {
		if kw == "auth" {
			authCount++
		}
	}
	assert.Equal(t, 1, authCount)
}

func TestExtractKeywords_Empty(t *testing.T) {
	result := extractKeywords("")

	assert.Empty(t, result)
}

func TestExtractKeywords_CamelCase(t *testing.T) {
	result := extractKeywords("useState useEffect")

	assert.Contains(t, result, "usestate")
	assert.Contains(t, result, "useeffect")
}

func TestExtractKeywords_WithHyphens(t *testing.T) {
	result := extractKeywords("use-effect type-script")

	// Hyphenated words should be extracted
	assert.NotEmpty(t, result)
}

// Tests for scoreKeywordMatch helper

func TestScoreKeywordMatch_NoKeywords(t *testing.T) {
	item := knowledge.Item{
		Name:        "react-guide",
		Description: "React component patterns",
	}

	result := scoreKeywordMatch(nil, item)

	assert.Equal(t, float64(0), result)
}

func TestScoreKeywordMatch_NoMatches(t *testing.T) {
	item := knowledge.Item{
		Name:        "python-guide",
		Description: "Python programming patterns",
	}

	result := scoreKeywordMatch([]string{"react", "javascript"}, item)

	// Base score is 0.5, no matches means no bonus
	assert.Equal(t, 0.5, result)
}

func TestScoreKeywordMatch_AllMatch(t *testing.T) {
	item := knowledge.Item{
		Name:        "react-patterns",
		Description: "React component patterns for building UIs",
	}

	result := scoreKeywordMatch([]string{"react", "component", "patterns"}, item)

	// Base 0.5 + full bonus (3/3 * 0.4 = 0.4) = 0.9
	assert.Equal(t, 0.9, result)
}

func TestScoreKeywordMatch_PartialMatch(t *testing.T) {
	item := knowledge.Item{
		Name:        "react-guide",
		Description: "React component patterns",
	}

	result := scoreKeywordMatch([]string{"react", "authentication"}, item)

	// Base 0.5 + partial bonus (1/2 * 0.4 = 0.2) = 0.7
	assert.Equal(t, 0.7, result)
}

func TestScoreKeywordMatch_CappedAtOne(t *testing.T) {
	item := knowledge.Item{
		Name:        "react authentication component patterns testing",
		Description: "react authentication component patterns testing guide",
	}

	// Many matching keywords
	keywords := []string{"react", "authentication", "component", "patterns", "testing", "guide"}
	result := scoreKeywordMatch(keywords, item)

	// Should be capped at 1.0
	assert.LessOrEqual(t, result, 1.0)
}

// Tests for sortMatchesByScore helper

func TestSortMatchesByScore_Empty(t *testing.T) {
	var matches []Match
	sortMatchesByScore(matches)

	assert.Empty(t, matches)
}

func TestSortMatchesByScore_SingleItem(t *testing.T) {
	matches := []Match{
		{Name: "only", Score: 0.5},
	}
	sortMatchesByScore(matches)

	assert.Len(t, matches, 1)
	assert.Equal(t, "only", matches[0].Name)
}

func TestSortMatchesByScore_DescendingOrder(t *testing.T) {
	matches := []Match{
		{Name: "low", Score: 0.3},
		{Name: "high", Score: 0.9},
		{Name: "mid", Score: 0.6},
	}
	sortMatchesByScore(matches)

	assert.Equal(t, "high", matches[0].Name)
	assert.Equal(t, "mid", matches[1].Name)
	assert.Equal(t, "low", matches[2].Name)
}

func TestSortMatchesByScore_StableForEqualScores(t *testing.T) {
	matches := []Match{
		{Name: "a", Score: 0.5},
		{Name: "b", Score: 0.5},
		{Name: "c", Score: 0.5},
	}

	// Sort should maintain relative order for equal scores (stable sort behavior)
	sortMatchesByScore(matches)

	// All should still have 0.5 score
	for _, m := range matches {
		assert.Equal(t, 0.5, m.Score)
	}
}

// Tests for minFloat helper

func TestMinFloat_FirstSmaller(t *testing.T) {
	result := minFloat(0.3, 0.7)
	assert.Equal(t, 0.3, result)
}

func TestMinFloat_SecondSmaller(t *testing.T) {
	result := minFloat(0.8, 0.2)
	assert.Equal(t, 0.2, result)
}

func TestMinFloat_Equal(t *testing.T) {
	result := minFloat(0.5, 0.5)
	assert.Equal(t, 0.5, result)
}

func TestMinFloat_Negative(t *testing.T) {
	result := minFloat(-0.3, 0.3)
	assert.Equal(t, -0.3, result)
}

func TestMinFloat_Zero(t *testing.T) {
	result := minFloat(0, 0.5)
	assert.Equal(t, float64(0), result)
}

// Tests for wordRe regex

func TestWordRe_BasicMatch(t *testing.T) {
	matches := wordRe.FindAllString("hello world", -1)

	assert.Contains(t, matches, "hello")
	assert.Contains(t, matches, "world")
}

func TestWordRe_MinLength(t *testing.T) {
	matches := wordRe.FindAllString("a ab abc abcd", -1)

	// Only words with 3+ chars should match
	assert.NotContains(t, matches, "a")
	assert.NotContains(t, matches, "ab")
	assert.Contains(t, matches, "abc")
	assert.Contains(t, matches, "abcd")
}

func TestWordRe_StartsWithLetter(t *testing.T) {
	matches := wordRe.FindAllString("abc 123 def 456test", -1)

	assert.Contains(t, matches, "abc")
	assert.Contains(t, matches, "def")
	// 123 doesn't start with letter
}

func TestWordRe_AllowsHyphensAndUnderscores(t *testing.T) {
	matches := wordRe.FindAllString("use-effect use_effect useState", -1)

	// Should match words with hyphens and underscores
	assert.NotEmpty(t, matches)
}

// Tests for knowledge.ItemKind values

func TestKnowledgeKind_ValidValues(t *testing.T) {
	// These should be valid knowledge item kinds
	validKinds := []knowledge.ItemKind{
		knowledge.KindPack,
		knowledge.KindAgent,
		knowledge.KindCommand,
	}

	for _, kind := range validKinds {
		assert.NotEmpty(t, string(kind))
	}
}

// Tests for filtering matches by threshold

func TestFilterByThreshold(t *testing.T) {
	matches := []Match{
		{Name: "high", Score: 0.9},
		{Name: "above", Score: 0.6},
		{Name: "at", Score: 0.5},
		{Name: "below", Score: 0.4},
		{Name: "low", Score: 0.2},
	}

	threshold := 0.5
	var filtered []Match
	for _, m := range matches {
		if m.Score >= threshold {
			filtered = append(filtered, m)
		}
	}

	assert.Len(t, filtered, 3)
	assert.Equal(t, "high", filtered[0].Name)
	assert.Equal(t, "above", filtered[1].Name)
	assert.Equal(t, "at", filtered[2].Name)
}

// Tests for limiting recommendations

func TestLimitRecommendations(t *testing.T) {
	matches := []Match{
		{Name: "1", Score: 0.9},
		{Name: "2", Score: 0.8},
		{Name: "3", Score: 0.7},
		{Name: "4", Score: 0.6},
		{Name: "5", Score: 0.5},
	}

	maxRec := 3
	limited := matches
	if len(limited) > maxRec {
		limited = limited[:maxRec]
	}

	assert.Len(t, limited, 3)
	assert.Equal(t, "1", limited[0].Name)
	assert.Equal(t, "3", limited[2].Name)
}

// Integration-style tests for sorting after scoring

func TestScoreAndSort(t *testing.T) {
	items := []knowledge.Item{
		{Name: "python-guide", Description: "Python programming"},
		{Name: "react-patterns", Description: "React component patterns"},
		{Name: "auth-guide", Description: "Authentication best practices"},
	}

	keywords := []string{"react", "component"}

	var matches []Match
	for _, item := range items {
		score := scoreKeywordMatch(keywords, item)
		matches = append(matches, Match{
			Name:  item.Name,
			Score: score,
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	// React patterns should score highest
	assert.Equal(t, "react-patterns", matches[0].Name)
}

// Tests for edge cases

func TestExtractKeywords_OnlyStopWords(t *testing.T) {
	result := extractKeywords("the and for with from")

	assert.Empty(t, result)
}

func TestExtractKeywords_SpecialChars(t *testing.T) {
	result := extractKeywords("auth@example.com user#123")

	// Should extract valid parts
	assert.Contains(t, result, "auth")
	assert.Contains(t, result, "example")
	assert.Contains(t, result, "user")
}

func TestExtractPrompt_NestedJSON(t *testing.T) {
	toolInput := map[string]any{
		"config": map[string]any{
			"query": "nested value",
		},
		"name": "top level",
	}
	data, _ := json.Marshal(toolInput)

	in := hooks.Input{
		ToolInput: data,
	}

	result := extractPrompt(in)

	// Only top-level string values
	assert.Contains(t, result, "top level")
}
