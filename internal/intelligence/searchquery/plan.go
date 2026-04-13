package searchquery

import (
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Default minimum query term length for lexical matching.
const defaultMinTermLength = 3

// QueryPlan contains the normalized structure for a user query.
type QueryPlan struct {
	// Raw is the original unmodified query text.
	Raw string

	// Terms are de-duplicated lexical tokens suitable for broad text matching.
	Terms []string

	// Identifiers are extracted non-phrase identifiers such as function names,
	// symbols, or qualified identifiers.
	Identifiers []Identifier

	// Phrases are quoted groups of text used for exact matching.
	Phrases []Phrase

	// PathHints provide file/path-oriented constraints from the query.
	PathHints []PathHint
}

// IdentifierKind describes a likely identifier style.
type IdentifierKind string

const (
	// IdentifierKindSymbol is a general symbol/function-like identifier.
	IdentifierKindSymbol IdentifierKind = "symbol"
	// IdentifierKindQualified is a dotted identifier such as "pkg.Func".
	IdentifierKindQualified IdentifierKind = "qualified"
	// IdentifierKindSnake is a snake_case identifier.
	IdentifierKindSnake IdentifierKind = "snake"
	// IdentifierKindCamel is a CamelCase identifier.
	IdentifierKindCamel IdentifierKind = "camel"
	// IdentifierKindPath is a path-like identifier with slash separators.
	IdentifierKindPath IdentifierKind = "path"
)

// Identifier is an extracted query identifier token.
type Identifier struct {
	Value string
	Kind  IdentifierKind
}

// Phrase is an extracted quoted term sequence.
type Phrase struct {
	Text string
}

// PathHint is a file/path-oriented query hint.
type PathHint struct {
	Path string
}

// MatchScore contains scoring details for a query against text.
type MatchScore struct {
	Score             float64
	TermMatches       int
	IdentifierMatches int
	PhraseMatches     int
	PathHintMatches   int
	MaxPossible       float64
	Actual            float64
}

// ParseQuery parses raw text into a QueryPlan.
func ParseQuery(raw string) QueryPlan {
	p := QueryPlan{Raw: strings.TrimSpace(raw)}
	if p.Raw == "" {
		return p
	}

	tokens := parseTokens(raw)

	seenTerms := map[string]struct{}{}
	seenIdentifiers := map[string]struct{}{}
	seenPathHints := map[string]struct{}{}
	seenPhrases := map[string]struct{}{}

	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t.quoted {
			phrase := normalizeTerm(t.value)
			if phrase == "" {
				continue
			}
			if _, ok := seenPhrases[phrase]; !ok {
				seenPhrases[phrase] = struct{}{}
				p.Phrases = append(p.Phrases, Phrase{Text: phrase})
			}
			continue
		}

		if prefix, hintValue, ok := splitPathHintPrefix(strings.ToLower(t.value)); ok {
			pathValue := hintValue
			if pathValue == "" {
				if i+1 < len(tokens) {
					pathValue = strings.Trim(tokens[i+1].value, "\"'")
					i++
				}
			}
			normalized := normalizePathHint(prefix, pathValue)
			if normalized == "" {
				continue
			}
			if _, ok := seenPathHints[normalized]; !ok {
				seenPathHints[normalized] = struct{}{}
				p.PathHints = append(p.PathHints, PathHint{Path: normalized})
			}
			continue
		}

		parts := splitLexicalTokens(t.value)
		if len(parts) == 0 {
			continue
		}

		for _, part := range parts {
			candidate := normalizeTerm(part)
			if candidate == "" || len(candidate) < defaultMinTermLength {
				continue
			}
			if isStopWord(candidate) {
				continue
			}

			if id, ok := ParseIdentifier(part); ok {
				key := string(id.Kind) + "::" + id.Value
				if _, exists := seenIdentifiers[key]; !exists {
					seenIdentifiers[key] = struct{}{}
					p.Identifiers = append(p.Identifiers, id)
				}
			}

			if _, exists := seenTerms[candidate]; !exists {
				seenTerms[candidate] = struct{}{}
				p.Terms = append(p.Terms, candidate)
			}
		}
	}

	return p
}

// ExtractTerms returns normalized lexical terms from a raw query.
func ExtractTerms(raw string) []string {
	return ParseQuery(raw).Terms
}

// ExtractIdentifiers returns identifier tokens from a raw query.
func ExtractIdentifiers(raw string) []Identifier {
	return ParseQuery(raw).Identifiers
}

// ExtractPhrases returns quoted phrases from a raw query.
func ExtractPhrases(raw string) []Phrase {
	return ParseQuery(raw).Phrases
}

// ExtractPathHints returns path hints from a raw query.
func ExtractPathHints(raw string) []PathHint {
	return ParseQuery(raw).PathHints
}

// ComposeLexicalQuery generates a stable lexical query string from a parsed plan.
func ComposeLexicalQuery(plan QueryPlan) string {
	parts := make([]string, 0)
	seen := map[string]struct{}{}

	appendUnique := func(v string) {
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		parts = append(parts, v)
	}

	for _, hint := range plan.PathHints {
		if hint.Path == "" {
			continue
		}
		appendUnique("path:" + quoteIfNeeded(hint.Path))
	}

	for _, phrase := range plan.Phrases {
		appendUnique(quoteIfNeeded(phrase.Text))
	}

	for _, id := range plan.Identifiers {
		appendUnique(id.Value)
	}

	for _, term := range plan.Terms {
		appendUnique(term)
	}

	if len(parts) == 0 {
		if strings.TrimSpace(plan.Raw) != "" {
			return strings.TrimSpace(plan.Raw)
		}
		return ""
	}
	return strings.Join(parts, " ")
}

// ScoreText computes a lightweight, deterministic relevance score in [0,1].
func ScoreText(plan QueryPlan, text string) MatchScore {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return MatchScore{}
	}

	score := MatchScore{}

	const (
		termWeight       = 1.0
		identifierWeight = 1.25
		phraseWeight     = 2.5
		pathHintWeight   = 3.0
	)

	for _, phrase := range plan.Phrases {
		score.MaxPossible += phraseWeight
		if phrase.Text == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(phrase.Text)) {
			score.Actual += phraseWeight
			score.PhraseMatches++
		}
	}

	for _, hint := range plan.PathHints {
		normalized := strings.ToLower(path.Clean(hint.Path))
		score.MaxPossible += pathHintWeight
		if normalized == "." || normalized == "/" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(normalized)) {
			score.Actual += pathHintWeight
			score.PathHintMatches++
		}
	}

	for _, id := range plan.Identifiers {
		score.MaxPossible += identifierWeight
		if containsWord(lower, id.Value) {
			score.Actual += identifierWeight
			score.IdentifierMatches++
		}
	}

	for _, term := range plan.Terms {
		score.MaxPossible += termWeight
		if containsWord(lower, term) {
			score.Actual += termWeight
			score.TermMatches++
		}
	}

	if score.MaxPossible == 0 {
		return score
	}
	score.Score = score.Actual / score.MaxPossible
	return score
}

// ParseIdentifier extracts a normalized identifier and its style.
func ParseIdentifier(raw string) (Identifier, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Identifier{}, false
	}

	if strings.ContainsAny(s, `\"'`) {
		s = strings.Trim(s, `"'`)
	}
	if s == "" {
		return Identifier{}, false
	}

	kind := identifierKind(s)
	if kind == "" {
		return Identifier{}, false
	}

	normal := normalizeIdentifier(s)
	if normal == "" {
		return Identifier{}, false
	}
	return Identifier{Value: normal, Kind: kind}, true
}

func splitLexicalTokens(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	// Keep alphanumerics and underscores and dots; split punctuation elsewhere.
	return strings.FieldsFunc(raw, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.')
	})
}

func normalizeTerm(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.Trim(raw, "\t\n\r .,:;!?()[]{}<>\"'")
	return strings.TrimSpace(raw)
}

type token struct {
	value  string
	quoted bool
}

func parseTokens(query string) []token {
	if query == "" {
		return nil
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	runes := []rune(query)
	result := make([]token, 0)
	for i := 0; i < len(runes); {
		if unicode.IsSpace(runes[i]) {
			i++
			continue
		}

		if runes[i] == '"' || runes[i] == '\'' {
			quote := runes[i]
			i++
			start := i
			for i < len(runes) && runes[i] != quote {
				i++
			}
			value := string(runes[start:i])
			if i < len(runes) {
				i++
			}
			result = append(result, token{value: value, quoted: true})
			continue
		}

		start := i
		for i < len(runes) && !unicode.IsSpace(runes[i]) {
			i++
		}
		result = append(result, token{value: string(runes[start:i]), quoted: false})
	}

	return result
}

func splitPathHintPrefix(token string) (string, string, bool) {
	prefixes := []string{"path:", "file:", "in:", "dir:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(token, prefix) {
			return prefix, strings.TrimSpace(strings.TrimPrefix(token, prefix)), true
		}
	}
	return "", "", false
}

func normalizePathHint(prefix, raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") && len(raw) >= 2 {
		raw = raw[1 : len(raw)-1]
	}
	if strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") && len(raw) >= 2 {
		raw = raw[1 : len(raw)-1]
	}
	if raw == "" || prefix == "" {
		return ""
	}

	clean := path.Clean(strings.ReplaceAll(raw, "\\", "/"))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." {
		return ""
	}
	return clean
}

func isStopWord(token string) bool {
	switch token {
	case
		"a", "an", "and", "are", "as", "at", "be", "by", "can", "do", "does", "for", "from", "had", "has", "have", "how", "in", "is", "it", "of", "on", "or", "that", "the", "this", "to", "was", "what", "when", "where", "which", "who", "with", "will":
		return true
	default:
		return false
	}
}

func normalizeIdentifier(raw string) string {
	s := normalizeTerm(raw)
	if s == "" {
		return ""
	}
	if unicode.IsDigit(rune(s[0])) {
		return ""
	}
	if !hasAlphaOrDigit(s) {
		return ""
	}
	return s
}

func hasAlphaOrDigit(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func identifierKind(raw string) IdentifierKind {
	if strings.Contains(raw, "/") {
		return IdentifierKindPath
	}
	if strings.Contains(raw, ".") {
		return IdentifierKindQualified
	}
	if strings.Contains(raw, "_") {
		return IdentifierKindSnake
	}
	if hasUpperLowerMix(raw) {
		return IdentifierKindCamel
	}
	if strings.Contains(raw, "-") {
		return IdentifierKindSymbol
	}
	return IdentifierKindSymbol
}

func hasUpperLowerMix(s string) bool {
	hasUpper := false
	hasLower := false
	for _, r := range s {
		if unicode.IsUpper(r) {
			hasUpper = true
		} else if unicode.IsLower(r) {
			hasLower = true
		}
	}
	return hasUpper && hasLower
}

func quoteIfNeeded(value string) string {
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, " \t\n\r") {
		return strconv.Quote(value)
	}
	return value
}

func containsWord(haystack string, needle string) bool {
	n := strings.ToLower(strings.TrimSpace(needle))
	if n == "" {
		return false
	}
	searchFrom := 0
	for {
		relativeIdx := strings.Index(haystack[searchFrom:], n)
		if relativeIdx < 0 {
			return false
		}
		idx := searchFrom + relativeIdx
		right := idx + len(n)
		leftOk := true
		if idx > 0 {
			leftRune, _ := utf8.DecodeLastRuneInString(haystack[:idx])
			leftOk = !isWordChar(leftRune)
		}
		rightOk := true
		if right < len(haystack) {
			rightRune, _ := utf8.DecodeRuneInString(haystack[right:])
			rightOk = !isWordChar(rightRune)
		}
		if leftOk && rightOk {
			return true
		}
		_, width := utf8.DecodeRuneInString(haystack[idx:])
		if width <= 0 {
			width = 1
		}
		searchFrom = idx + width
	}
}

func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// StableTerms returns query terms in a sorted copy for deterministic callers.
func (p QueryPlan) StableTerms() []string {
	terms := append([]string{}, p.Terms...)
	sort.Strings(terms)
	return terms
}

// StableIdentifiers returns identifiers sorted by (kind, value) for deterministic callers.
func (p QueryPlan) StableIdentifiers() []Identifier {
	ids := append([]Identifier{}, p.Identifiers...)
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].Kind == ids[j].Kind {
			return ids[i].Value < ids[j].Value
		}
		return ids[i].Kind < ids[j].Kind
	})
	return ids
}

// StablePathHints returns path hints sorted for deterministic callers.
func (p QueryPlan) StablePathHints() []PathHint {
	hints := append([]PathHint{}, p.PathHints...)
	sort.Slice(hints, func(i, j int) bool { return hints[i].Path < hints[j].Path })
	return hints
}

// StablePhrases returns phrases sorted for deterministic callers.
func (p QueryPlan) StablePhrases() []Phrase {
	phrases := append([]Phrase{}, p.Phrases...)
	sort.Slice(phrases, func(i, j int) bool { return phrases[i].Text < phrases[j].Text })
	return phrases
}
