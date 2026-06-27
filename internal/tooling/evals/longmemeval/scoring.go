package longmemeval

import (
	"strings"
)

// answerMatchScore returns a deterministic score for an answer against the
// expected answer. It rewards exact matches, answer-contains-expected,
// bidirectional substring (when the answer is a substantial fraction of the
// expected), numeric fact matches, and semantic key-fact overlap. It rejects
// answers that express insufficient evidence when the expected answer does not.
func answerMatchScore(answer, expected string) float64 {
	answer = normalizeAnswerText(answer)
	expected = normalizeAnswerText(expected)
	if answer == "" || expected == "" {
		return 0
	}
	if answerExpressesInsufficientEvidence(answer) && !answerExpressesInsufficientEvidence(expected) {
		return 0
	}
	if answer == expected || strings.Contains(answer, expected) {
		return 1
	}
	// Bidirectional contains: the expected answer may be longer and
	// contain the given answer as a substring. Only fire when the answer
	// is at least half the expected length (chars) to avoid matching a
	// single word to a long expected answer.
	if strings.Contains(expected, answer) && len(answer)*2 >= len(expected) {
		return 1
	}
	// Numeric fact match: when the expected answer leads with a number+unit
	// (e.g. "7 days", "3 items"), check if the answer contains that exact
	// number near the same unit. This catches long conversational answers
	// that state the correct value but in a verbose format.
	if score := numericFactMatchScore(answer, expected); score > 0 {
		return score
	}
	// Semantic key-fact overlap: for longer expected answers, check if
	// the answer covers the key facts/entities. This catches paraphrased
	// correct answers that strict substring matching misses.
	if score := keyFactOverlapScore(answer, expected); score > 0 {
		return score
	}
	return 0
}

// numericFactMatchScore extracts a leading "number + unit" pattern from the
// expected answer (e.g. "7 days", "3 items", "45 minutes") and checks if the
// answer contains that number followed by the same unit within a small window.
// Returns 1 on match, 0 otherwise. This handles short expected answers that
// contain the key fact at the start but also have additional context.
func numericFactMatchScore(answer, expected string) float64 {
	// Extract leading number from expected.
	expectedWords := strings.Fields(expected)
	if len(expectedWords) < 2 {
		return 0
	}
	num := strings.Trim(expectedWords[0], ".,;:!?()[]{}")
	unit := strings.Trim(expectedWords[1], ".,;:!?()[]{}")
	if !isNumeric(num) || len(unit) < 3 {
		return 0
	}
	// Check if answer contains "num unit" within 3 words of each other.
	answerWords := strings.Fields(answer)
	for i, w := range answerWords {
		w = strings.Trim(w, ".,;:!?()[]{}")
		if w == num && i+1 < len(answerWords) {
			next := strings.Trim(answerWords[i+1], ".,;:!?()[]{}")
			if strings.EqualFold(next, unit) || strings.EqualFold(strings.TrimSuffix(next, "s"), strings.TrimSuffix(unit, "s")) {
				return 1
			}
		}
		// Also handle markdown-formatted tokens (e.g. "7days" after ** strip).
		// Use word-boundary check: the number must be a standalone token, not
		// a substring of a larger number (e.g. "3" must not match "30days").
		stripped := strings.Trim(w, ".,;:!?()[]{}")
		if stripped == num && i+1 < len(answerWords) {
			next := strings.Trim(answerWords[i+1], ".,;:!?()[]{}")
			if strings.EqualFold(next, unit) || strings.EqualFold(strings.TrimSuffix(next, "s"), strings.TrimSuffix(unit, "s")) {
				return 1
			}
		}
	}
	return 0
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// keyFactOverlapScore checks whether the answer covers the key facts from
// the expected answer. It extracts significant phrases (>3 chars, excluding
// stopwords) from the expected answer and checks how many appear in the
// answer. Returns a score from 0 to 1 based on coverage ratio. Only fires
// when the expected answer has at least 3 significant phrases (short answers
// like "3" or "yes" use strict matching only).
func keyFactOverlapScore(answer, expected string) float64 {
	expectedPhrases := extractSignificantPhrases(expected)
	if len(expectedPhrases) < 3 {
		return 0
	}
	matched := 0
	for _, phrase := range expectedPhrases {
		if strings.Contains(answer, phrase) {
			matched++
		}
	}
	coverage := float64(matched) / float64(len(expectedPhrases))
	// Accept at 33% coverage with at least 2 matched phrases. Paraphrased
	// answers share key entities but rarely share connective phrasing. The
	// insufficiency guard filters refusals, and short expected answers use
	// strict matching. The minimum-2-match guard prevents a single shared
	// entity from triggering a false positive.
	if coverage >= 0.33 && matched >= 2 {
		return coverage
	}
	return 0
}

// extractSignificantPhrases returns multi-word and single-word phrases from
// text that are likely to carry factual content. Filters stopwords and short
// tokens. Used for semantic overlap scoring, not classification decisions.
func extractSignificantPhrases(text string) []string {
	words := strings.Fields(text)
	phrases := make([]string, 0, len(words))
	seen := make(map[string]struct{})
	addPhrase := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		phrases = append(phrases, p)
	}
	// Single significant words (>3 chars, not stop words).
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?()[]{}\"'")
		if len(w) <= 3 || answerScoreStopword(w) {
			continue
		}
		addPhrase(w)
	}
	// Bigrams: only when BOTH words are significant (>3 chars, not stopwords).
	// This filters connective bigrams like "been helpful" or "administration you".
	for i := 0; i < len(words)-1; i++ {
		w1 := strings.Trim(words[i], ".,;:!?()[]{}\"'")
		w2 := strings.Trim(words[i+1], ".,;:!?()[]{}\"'")
		if len(w1) <= 3 || len(w2) <= 3 {
			continue
		}
		if answerScoreStopword(w1) || answerScoreStopword(w2) {
			continue
		}
		addPhrase(w1 + " " + w2)
	}
	return phrases
}

// answerScoreStopword returns true for common words that should not count
// as significant phrases for overlap scoring. This is NOT used for routing
// or classification — only for narrowing which tokens count as factual
// content in the answer scorer.
func answerScoreStopword(word string) bool {
	switch strings.ToLower(word) {
	case "the", "that", "this", "they", "them", "their", "from", "have",
		"been", "would", "could", "should", "will", "with", "into",
		"some", "more", "most", "than", "then", "also", "just",
		"only", "even", "very", "much", "many", "such", "same",
		"other", "what", "where", "when", "which", "does", "were":
		return true
	default:
		return false
	}
}

func answerExpressesInsufficientEvidence(answer string) bool {
	for _, phrase := range []string{
		"cannot answer",
		"cannot determine",
		"cannot provide a verified answer",
		"cannot provide verified",
		"do not have enough",
		"does not contain",
		"evidence is insufficient",
		"insufficient evidence",
		"no record of",
		"no verified",
		"not enough evidence",
		"rejected all candidate",
		"verified evidence is insufficient",
	} {
		if strings.Contains(answer, phrase) {
			return true
		}
	}
	return false
}

func normalizeAnswerText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	// Strip markdown bold/italic markers that split tokens (e.g. "**7 days**"
	// would tokenize as ["**7", "days**"] instead of ["7", "days"]).
	value = strings.ReplaceAll(value, "**", "")
	value = strings.ReplaceAll(value, "__", "")
	value = strings.ReplaceAll(value, "*", "")
	value = strings.ReplaceAll(value, "_", "")
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}
