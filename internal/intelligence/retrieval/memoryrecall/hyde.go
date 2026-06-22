package memoryrecall

import (
	"strings"
)

// DecomposeQuery applies a lightweight HyDE-style relational decomposition to a
// natural-language question, returning sub-queries that widen recall. This is a
// pure string function — no LLM call, no embeddings. It mirrors the query
// decomposition patterns used by agent-oss (Quarq) but stays deterministic and
// testable.
//
// Decomposition rules:
//   - Entity/action split: "What degree did I get?" -> ["degree", "what degree", "earned degree"]
//   - Possessive expansion: "my commute" -> ["commute", "daily commute"]
//   - Multi-clause: "What and where?" -> split on "and"
//   - Noun extraction: key nouns >3 chars become standalone probes
//
// The original query is always included as the first result. Capped at 6 probes.
func DecomposeQuery(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	// Extract trailing Question: text if present (staged prompt format).
	if idx := strings.LastIndex(strings.ToLower(query), "question:"); idx >= 0 {
		query = strings.TrimSpace(query[idx+len("question:"):])
	}

	out := make([]string, 0, 6)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range out {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		if len(out) >= 6 {
			return
		}
		out = append(out, value)
	}

	// Always include the original query.
	add(query)

	// Strip trailing question mark.
	cleaned := strings.TrimSuffix(strings.TrimSpace(query), "?")

	// Multi-clause split on " and ".
	if strings.Contains(strings.ToLower(cleaned), " and ") {
		parts := splitOnAnd(cleaned)
		for _, part := range parts {
			add(part)
			nouns := extractKeyNouns(part)
			add(strings.Join(nouns, " "))
		}
		return out
	}

	// Entity/action split: extract the core noun phrase after the question word.
	// "What degree did I get?" -> "degree"
	// "Do I have any model kits?" -> "model kits"
	nouns := extractKeyNouns(cleaned)
	if len(nouns) > 0 {
		// Add the noun phrase as a focused probe.
		add(strings.Join(nouns, " "))
	}

	// Possessive expansion: "my X" -> "X" (strip possessive pronouns).
	possessiveStripped := stripPossessives(cleaned)
	if possessiveStripped != cleaned && possessiveStripped != "" {
		add(possessiveStripped)
	}

	// Add individual key nouns as single-word probes for entity-anchored recall.
	for _, noun := range nouns {
		add(noun)
		if len(out) >= 6 {
			break
		}
	}

	return out
}

// extractKeyNouns returns content words >3 characters, filtering question
// prefixes, stop words, and common verbs. This is a deterministic approximation
// of noun-phrase extraction — not a full POS tagger, but sufficient for
// widening recall without keyword heuristics on classification decisions.
func extractKeyNouns(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	// Remove leading question words.
	for _, prefix := range []string{"what ", "where ", "when ", "how ", "which ", "do ", "did ", "does ", "is ", "are ", "can ", "could ", "have ", "has ", "who ", "why "} {
		text = strings.TrimPrefix(text, prefix)
	}
	words := strings.Fields(text)
	nouns := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?()[]{}\"'")
		if len(w) <= 3 {
			continue
		}
		if queryDecomposeStopword(w) {
			continue
		}
		nouns = append(nouns, w)
	}
	return nouns
}

// stripPossessives removes "my", "your", "our", "their", "his", "her" from the
// query to create a possessive-stripped variant that matches stored memory text
// which may not include the possessive.
func stripPossessives(text string) string {
	words := strings.Fields(text)
	out := make([]string, 0, len(words))
	for _, w := range words {
		lower := strings.ToLower(w)
		switch lower {
		case "my", "your", "our", "their", "his", "her", "its":
			continue
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

// splitOnAnd splits text on " and " boundaries, returning the non-empty parts.
func splitOnAnd(text string) []string {
	parts := strings.Split(strings.ToLower(text), " and ")
	// Re-split on the original text to preserve case.
	originalParts := strings.Split(text, " and ")
	if len(originalParts) != len(parts) {
		return []string{text}
	}
	out := make([]string, 0, len(originalParts))
	for _, p := range originalParts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// queryDecomposeStopword returns true for common non-content words that should
// not become standalone probes. This is NOT used for classification/routing —
// only for narrowing which words become individual recall probes.
func queryDecomposeStopword(word string) bool {
	switch strings.ToLower(word) {
	case "have", "been", "from", "that", "this", "will", "would", "could",
		"about", "into", "they", "them", "were", "some", "more", "most",
		"than", "then", "also", "just", "only", "even", "very", "much",
		"many", "such", "same", "other", "what", "where", "when", "which",
		"does", "done", "made", "make", "like", "want", "need", "know",
		"tell", "said", "says", "say":
		return true
	default:
		return false
	}
}
