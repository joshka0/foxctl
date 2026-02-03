package retrieval

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/jkatigb/agentctl/internal/indexing/symbol"
)

// searchSymbolIndex searches the symbol index for relevant code symbols.
// This adapts the gatherSymbolHits logic from internal/agent/tools/code_tools.go.
func (g *Generator) searchSymbolIndex(ctx context.Context, workspaceID, question string, limit int) ([]Candidate, error) {
	if g.store == nil {
		return nil, nil
	}

	terms := buildSearchTerms(question)
	if len(terms) == 0 {
		return nil, nil
	}

	// Track seen symbols to avoid duplicates
	seen := make(map[string]bool)
	var hits []symbolHit

	// Query limit multiplier to oversample for scoring
	queryLimit := limit * 10
	if queryLimit < 50 {
		queryLimit = 50
	}

	for _, term := range terms {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		results, err := g.store.Search(ctx, workspaceID, term, queryLimit)
		if err != nil {
			g.logger.Debug().Err(err).Str("term", term).Msg("search term failed")
			continue
		}

		for _, scored := range results {
			entry := scored.Entry
			// Only process symbol entries
			if entry.Type != symbol.SymbolType {
				continue
			}

			// Deduplicate by entry name
			key := entry.Workspace + "|" + entry.Name
			if seen[key] {
				continue
			}
			seen[key] = true

			// Parse the symbol result
			res, err := symbol.UnmarshalResult(entry.Result)
			if err != nil {
				g.logger.Debug().Err(err).Str("name", entry.Name).Msg("failed to unmarshal symbol")
				continue
			}

			hits = append(hits, symbolHit{
				result: res,
				bm25:   scored.Score,
			})
		}
	}

	if len(hits) == 0 {
		return nil, nil
	}

	// Score and rank hits
	qTokens := tokenize(question)
	for i := range hits {
		hits[i].score = scoreSymbol(hits[i].result.Symbol, qTokens)
	}

	// Sort by score descending
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		// Stable tie-breaking by file path then symbol ID
		if hits[i].result.Symbol.FilePath != hits[j].result.Symbol.FilePath {
			return hits[i].result.Symbol.FilePath < hits[j].result.Symbol.FilePath
		}
		return hits[i].result.Symbol.ID < hits[j].result.Symbol.ID
	})

	// Limit and convert to candidates
	if len(hits) > limit {
		hits = hits[:limit]
	}

	// Find max score for normalization
	maxScore := 0.0
	for _, h := range hits {
		if h.score > maxScore {
			maxScore = h.score
		}
	}
	if maxScore == 0 {
		maxScore = 1.0
	}

	candidates := make([]Candidate, 0, len(hits))
	for _, hit := range hits {
		sym := hit.result.Symbol
		candidates = append(candidates, Candidate{
			Path:     sym.FilePath,
			SymbolID: sym.ID,
			Name:     sym.Name,
			Kind:     string(sym.Kind),
			Score:    hit.score / maxScore, // Normalize to 0-1
			RawScore: hit.score,
			Source:   SourceSymbol,
			Line:     sym.StartLine,
		})
	}

	return candidates, nil
}

// symbolHit holds a symbol result with its scores.
type symbolHit struct {
	result *symbol.Result
	bm25   float64
	score  float64
}

// buildSearchTerms extracts search terms from a question.
func buildSearchTerms(question string) []string {
	tokens := tokenize(question)
	if len(tokens) == 0 {
		return []string{question}
	}

	// Deduplicate and limit
	seen := make(map[string]bool)
	var terms []string
	for _, tok := range tokens {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		terms = append(terms, tok)
		if len(terms) >= 6 {
			break
		}
	}
	return terms
}

// tokenize splits text into searchable tokens.
func tokenize(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil
	}

	// Stop words to filter out
	stopWords := map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
		"be": true, "by": true, "can": true, "do": true, "does": true, "for": true,
		"from": true, "has": true, "have": true, "how": true, "i": true, "in": true,
		"is": true, "it": true, "of": true, "on": true, "or": true, "that": true,
		"the": true, "this": true, "to": true, "was": true, "what": true, "when": true,
		"where": true, "which": true, "who": true, "why": true, "will": true, "with": true,
	}

	var tokens []string
	var b strings.Builder

	flush := func() {
		if b.Len() == 0 {
			return
		}
		tok := b.String()
		b.Reset()
		// Skip short tokens and stop words
		if len(tok) < 3 || stopWords[tok] {
			return
		}
		tokens = append(tokens, tok)
	}

	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	return tokens
}

// scoreSymbol scores a symbol against query tokens.
func scoreSymbol(sym symbol.Symbol, tokens []string) float64 {
	name := strings.ToLower(sym.Name)
	sig := strings.ToLower(sym.Signature)
	doc := strings.ToLower(sym.Documentation)
	file := strings.ToLower(sym.FilePath)

	score := 0.0
	for _, tok := range tokens {
		if tok == "" {
			continue
		}

		// Name matches (highest weight)
		if name == tok {
			score += 5.0
		} else if strings.Contains(name, tok) {
			score += 2.0
		}

		// Signature matches
		if sig != "" && strings.Contains(sig, tok) {
			score += 1.0
		}

		// Documentation matches
		if doc != "" && strings.Contains(doc, tok) {
			score += 0.5
		}

		// File path matches
		if file != "" && strings.Contains(file, tok) {
			score += 0.3
		}
	}

	// Bonus for certain symbol kinds
	switch sym.Kind {
	case symbol.KindFunction, symbol.KindMethod:
		score *= 1.2 // Boost functions/methods
	case symbol.KindClass, symbol.KindStruct, symbol.KindInterface:
		score *= 1.1 // Slight boost for types
	}

	if score == 0 {
		score = 0.1 // Minimum score for any match
	}

	return score
}
