package companion

import (
	"strings"
)

// EpisodeBoundarySignal represents a typed signal for episode boundary detection.
// This replaces keyword-based strings.Contains classification.
type EpisodeBoundarySignal string

const (
	// SignalNone means no boundary signal detected.
	SignalNone EpisodeBoundarySignal = ""
	// SignalAssumptionInvalidated means an assumption was invalidated (retraction).
	SignalAssumptionInvalidated EpisodeBoundarySignal = "assumption_invalidated"
	// SignalUserRedirect means the user changed topic significantly.
	SignalUserRedirect EpisodeBoundarySignal = "user_redirect"
)

// ExtractionPolicy decides whether an event should be processed for extraction
// and performs the actual extraction.
//
// This replaces the keyword heuristic functions (containsPreference, containsDecision,
// containsQuestion, containsDefinition, containsGoalChange, containsRetraction,
// hasMemoryWorthySignals, extractProfileClaims, extractDecisions, extractOpenQuestions,
// extractGoalChange, extractRetractions).
type ExtractionPolicy interface {
	// ShouldExtract reports whether the event should be processed for Tier 1 extraction.
	// Returns true and a list of extraction categories to apply.
	// tool_result events always return true (backward compatibility).
	ShouldExtract(event ConversationEvent) (bool, []string)

	// ExtractEntries extracts entries from the given text for the specified categories.
	// Categories come from ShouldExtract's return value.
	ExtractEntries(text string, categories []string) []ExtractedEntry
}

// ExtractionCategory constants for the categories returned by ExtractionPolicy.
const (
	ExtractionCategoryPreference = "preference"
	ExtractionCategoryDecision   = "decision"
	ExtractionCategoryQuestion   = "open_question"
	ExtractionCategoryDefinition = "definition"
	ExtractionCategoryGoalChange = "goal_change"
	ExtractionCategoryRetraction = "retraction"
)

// SignalExtractor detects typed episode boundary signals from event content.
// This replaces isRetractionSignal and isUserRedirectSignal keyword functions.
type SignalExtractor interface {
	// DetectBoundarySignal returns a typed signal for the given event content.
	// Returns SignalNone if no signal is detected.
	DetectBoundarySignal(content string) EpisodeBoundarySignal
}

// CompositeExtractionPolicy combines multiple extraction policies.
// An event passes extraction if ANY policy returns true.
// Categories are deduplicated from all policies that return true.
type CompositeExtractionPolicy struct {
	policies []ExtractionPolicy
}

// NewCompositeExtractionPolicy creates a policy that delegates to the given sub-policies.
func NewCompositeExtractionPolicy(policies ...ExtractionPolicy) *CompositeExtractionPolicy {
	return &CompositeExtractionPolicy{policies: policies}
}

// ShouldExtract returns true if any sub-policy returns true, with merged categories.
func (p *CompositeExtractionPolicy) ShouldExtract(event ConversationEvent) (bool, []string) {
	var allCategories []string
	seen := make(map[string]struct{})
	anyTrue := false

	for _, policy := range p.policies {
		ok, categories := policy.ShouldExtract(event)
		if ok {
			anyTrue = true
			for _, cat := range categories {
				if _, exists := seen[cat]; !exists {
					seen[cat] = struct{}{}
					allCategories = append(allCategories, cat)
				}
			}
		}
	}
	return anyTrue, allCategories
}

// ExtractEntries delegates to sub-policies and merges results.
func (p *CompositeExtractionPolicy) ExtractEntries(text string, categories []string) []ExtractedEntry {
	seen := make(map[string]struct{})
	var all []ExtractedEntry
	for _, policy := range p.policies {
		for _, entry := range policy.ExtractEntries(text, categories) {
			key := entry.EntryType + ":" + entry.RawText
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			all = append(all, entry)
		}
	}
	return all
}

// ToolResultBypassPolicy always returns true for tool_result events.
// This preserves the existing behavior where tool_result events always
// pass through extraction regardless of content.
type ToolResultBypassPolicy struct{}

// ShouldExtract returns true for tool_result events with no specific categories.
func (p ToolResultBypassPolicy) ShouldExtract(event ConversationEvent) (bool, []string) {
	if strings.ToLower(strings.TrimSpace(event.EventType)) == EventTypeToolResult {
		return true, nil
	}
	return false, nil
}

// ExtractEntries returns no entries (tool_result bypass has no extraction logic).
func (p ToolResultBypassPolicy) ExtractEntries(_ string, _ []string) []ExtractedEntry {
	return nil
}

// PatternExtractionPolicy uses explicit regex patterns to detect extractable content.
// Each pattern is associated with an extraction category.
// This replaces the keyword-based containsPreference, containsDecision, etc.
type PatternExtractionPolicy struct {
	detectors []patternDetector
}

type patternDetector struct {
	category   string
	indicators []string
}

// ShouldExtract returns true if any pattern matches, with matching categories.
func (p *PatternExtractionPolicy) ShouldExtract(event ConversationEvent) (bool, []string) {
	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	if eventType != EventTypeUserMessage && eventType != EventTypeAssistantMessage {
		return false, nil
	}

	text := strings.ToLower(event.Content)
	if text == "" {
		return false, nil
	}

	var matched []string
	for _, det := range p.detectors {
		if det.matches(text) {
			matched = append(matched, det.category)
		}
	}
	return len(matched) > 0, matched
}

func (d patternDetector) matches(text string) bool {
	for _, indicator := range d.indicators {
		if strings.Contains(text, indicator) {
			return true
		}
	}
	return false
}

// NewDefaultPatternExtractionPolicy creates the default pattern-based extraction policy.
// This uses explicit indicator patterns to detect extractable content categories.
func NewDefaultPatternExtractionPolicy() *PatternExtractionPolicy {
	return &PatternExtractionPolicy{
		detectors: []patternDetector{
			{
				category: ExtractionCategoryPreference,
				indicators: []string{
					"i prefer", "i'd prefer", "i always", "i never",
					"i don't like", "don't like", "i like", "i enjoy",
				},
			},
			{
				category: ExtractionCategoryDecision,
				indicators: []string{
					"let's go with", "decided to", "the plan is",
					"we will", "we're going to", "i've decided",
					"i decided", "let us", "let's do",
				},
			},
			{
				category: ExtractionCategoryQuestion,
				indicators: []string{
					"what about", "should we", "could we",
					"can we", "what if", "how about",
					"when do", "where should",
				},
			},
			{
				category: ExtractionCategoryDefinition,
				indicators: []string{
					" means ", " by ", " i mean ",
					" is when", "defined as", "is called",
				},
			},
			{
				category: ExtractionCategoryGoalChange,
				indicators: []string{
					"the goal is", "we need to", "let's focus",
					"from now on", "going forward", "new objective",
					"change the goal", "priority is",
				},
			},
			{
				category: ExtractionCategoryRetraction,
				indicators: []string{
					"actually no", "that was wrong", "forget that",
					"disregard", "scratch that", "nevermind",
					"i retract", "i take it back",
				},
			},
		},
	}
}

// ExtractEntries extracts entries from text for the specified categories.
// This replaces extractProfileClaims, extractDecisions, extractOpenQuestions,
// extractGoalChange, and extractRetractions.
func (p *PatternExtractionPolicy) ExtractEntries(text string, categories []string) []ExtractedEntry {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	catSet := make(map[string]struct{}, len(categories))
	for _, c := range categories {
		catSet[c] = struct{}{}
	}

	var entries []ExtractedEntry

	if _, ok := catSet[ExtractionCategoryPreference]; ok {
		entries = append(entries, extractByPatterns(text, []string{
			"i prefer", "i'd prefer", "i always", "i never",
			"i don't like", "i dislike", "i like",
		}, "preference", 0.86)...)
	}

	if _, ok := catSet[ExtractionCategoryDecision]; ok {
		entries = append(entries, extractByPatterns(text, []string{
			"let's decide", "let's go with", "we decided",
			"i decided", "we will", "i'll do", "i will do", "we should do",
		}, "decision", 0.8)...)
	}

	if _, ok := catSet[ExtractionCategoryQuestion]; ok {
		entries = append(entries, extractQuestionEntries(text)...)
	}

	if _, ok := catSet[ExtractionCategoryGoalChange]; ok {
		if entry := extractGoalEntry(text); entry != nil {
			entries = append(entries, *entry)
		}
	}

	if _, ok := catSet[ExtractionCategoryRetraction]; ok {
		entries = append(entries, extractRetractionEntries(text)...)
	}

	return entries
}

// extractQuestionEntries extracts question-type entries from text.
// Replaces the old extractOpenQuestions function.
func extractQuestionEntries(text string) []ExtractedEntry {
	var out []ExtractedEntry
	seen := map[string]struct{}{}

	for _, sentence := range splitSentences(text) {
		if strings.Contains(sentence, "?") {
			for _, question := range splitQuestionParts(sentence) {
				question = strings.TrimSpace(question)
				if question == "" {
					continue
				}
				if _, ok := seen[question]; ok {
					continue
				}
				seen[question] = struct{}{}
				out = append(out, ExtractedEntry{
					EntryType:  "open_question",
					RawText:    question,
					Value:      question,
					Confidence: 0.72,
				})
			}
		}
	}

	for _, extra := range extractByPatterns(text, []string{
		"what should", "should we", "could we", "how should",
		"why don't we", "can we", "what about",
	}, "open_question", 0.66) {
		if _, ok := seen[extra.Value]; ok {
			continue
		}
		seen[extra.Value] = struct{}{}
		out = append(out, extra)
	}

	return out
}

// extractGoalEntry extracts a goal change from text.
// Replaces the old extractGoalChange function.
func extractGoalEntry(text string) *ExtractedEntry {
	goal := firstMatchAfterPattern(text, []string{
		"the goal is", "our goal is", "let's focus on",
		"let's work toward", "goal:",
	})
	if goal == "" {
		return nil
	}
	return &ExtractedEntry{
		EntryType:  "goal",
		RawText:    goal,
		Value:      goal,
		Confidence: 0.9,
	}
}

// extractRetractionEntries extracts retraction entries from text.
// Replaces the old extractRetractions function.
func extractRetractionEntries(text string) []ExtractedEntry {
	if !containsRetractionPattern(text) {
		return nil
	}
	return []ExtractedEntry{{
		EntryType:  "retraction",
		RawText:    strings.TrimSpace(text),
		Value:      strings.TrimSpace(text),
		Confidence: 0.93,
	}}
}

// containsRetractionPattern checks for retraction indicator patterns.
// This is used by the extraction pipeline (not episode boundary detection).
func containsRetractionPattern(text string) bool {
	lower := strings.ToLower(text)
	for _, signal := range []string{"actually no", "that was wrong", "forget that", "disregard", "scratch that", "nevermind", "i retract", "i take it back"} {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

// TypedSignalExtractor implements SignalExtractor using typed pattern lists.
// This replaces isRetractionSignal and isUserRedirectSignal.
type TypedSignalExtractor struct {
	retractionIndicators []string
	redirectIndicators   []string
}

// NewDefaultTypedSignalExtractor creates the default signal extractor with
// explicit indicator patterns for retraction and redirect detection.
func NewDefaultTypedSignalExtractor() *TypedSignalExtractor {
	return &TypedSignalExtractor{
		retractionIndicators: []string{
			"actually no",
			"that was wrong",
			"that's wrong",
			"that's incorrect",
			"never mind",
			"i'm sorry",
			"i take that back",
			"assumption invalidated",
			"let's reset",
		},
		redirectIndicators: []string{
			"let's move on to",
			"forget that, let's",
			"let's switch to",
			"let's pivot to",
			"moving on to",
		},
	}
}

// DetectBoundarySignal returns the typed signal for the given content.
func (e *TypedSignalExtractor) DetectBoundarySignal(content string) EpisodeBoundarySignal {
	lower := strings.ToLower(content)
	for _, indicator := range e.retractionIndicators {
		if strings.Contains(lower, indicator) {
			return SignalAssumptionInvalidated
		}
	}
	for _, indicator := range e.redirectIndicators {
		if strings.Contains(lower, indicator) {
			return SignalUserRedirect
		}
	}
	return SignalNone
}
