package memory

import (
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/storage/dbdriver"
)

const (
	memorySearchMaxTerms          = 12
	memorySearchMinCandidateLimit = 200
	memorySearchMaxCandidateLimit = 5000
)

func memorySearchTerms(query string) []string {
	terms := dbdriver.Tokenize(query)
	seen := make(map[string]struct{}, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" || isMemorySearchStopWord(term) {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
		if len(out) >= memorySearchMaxTerms {
			break
		}
	}
	return out
}

func isMemorySearchStopWord(term string) bool {
	switch term {
	case "a", "an", "and", "are", "as", "at", "be", "by", "for", "from",
		"has", "in", "is", "it", "of", "on", "or", "that", "the", "to",
		"was", "were", "will", "with":
		return true
	default:
		return false
	}
}

func memorySearchCandidateWindow(limit int) int {
	if limit <= 0 {
		limit = 20
	}
	window := limit * 50
	if window < memorySearchMinCandidateLimit {
		window = memorySearchMinCandidateLimit
	}
	if window > memorySearchMaxCandidateLimit {
		window = memorySearchMaxCandidateLimit
	}
	return window
}

func memorySearchableText(entry NamedEntry) string {
	summary := memorySearchTermText(entry.Summary)
	atomicText := memorySearchTermText(entry.AtomicText)
	parts := []string{
		entry.Name,
		entry.Name,
		entry.Summary,
		summary,
		entry.AtomicText,
		entry.AtomicText,
		atomicText,
		strings.Join(entry.Entities, " "),
		memorySearchTermText(strings.Join(entry.Entities, " ")),
		strings.Join(entry.Keywords, " "),
		strings.Join(entry.Keywords, " "),
		memorySearchTermText(strings.Join(entry.Keywords, " ")),
	}
	return strings.Join(parts, " ")
}

func memorySearchTermText(text string) string {
	return strings.NewReplacer(
		".", " ",
		"/", " ",
		"\\", " ",
		":", " ",
		"-", " ",
		"_", " ",
	).Replace(text)
}

func scoreMemoryLexicalEntries(entries []NamedEntry, query string, limit int) []ScoredEntry {
	queryTerms := memorySearchTerms(query)
	if len(entries) == 0 || len(queryTerms) == 0 {
		return nil
	}

	docStats := make([]dbdriver.DocumentStats, 0, len(entries))
	docFreqs := map[string]int{}
	totalTokens := 0
	for _, entry := range entries {
		text := memorySearchableText(entry)
		terms := dbdriver.Tokenize(text)
		totalTokens += len(terms)
		stats := dbdriver.DocumentStats{
			ID:        entry.ID,
			Length:    len(terms),
			TermFreqs: dbdriver.ComputeTermFrequency(text),
		}
		docStats = append(docStats, stats)

		uniqueTerms := map[string]struct{}{}
		for _, term := range terms {
			uniqueTerms[term] = struct{}{}
		}
		for term := range uniqueTerms {
			docFreqs[term]++
		}
	}

	if totalTokens == 0 {
		return nil
	}

	scorer := dbdriver.NewBM25Scorer(dbdriver.DefaultBM25Params(), dbdriver.CorpusStats{
		TotalDocs:    len(entries),
		AvgDocLength: float64(totalTokens) / float64(len(entries)),
		DocFreqs:     docFreqs,
	})

	rawScores := make([]float64, len(entries))
	for i, stats := range docStats {
		rawScores[i] = scorer.Score(queryTerms, stats)
	}
	nonZero := nonZeroScores(rawScores)
	scaler := dbdriver.NewMinMaxScaler(nonZero)

	scored := make([]ScoredEntry, 0, len(entries))
	for i, rawScore := range rawScores {
		if rawScore <= 0 {
			continue
		}
		score := 0.5
		if len(nonZero) > 1 {
			score = 0.5 + 0.5*scaler.Scale(rawScore)
		}
		scored = append(scored, ScoredEntry{
			Entry: entries[i],
			Score: score,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			if !scored[i].Entry.UpdatedAt.Equal(scored[j].Entry.UpdatedAt) {
				return scored[i].Entry.UpdatedAt.After(scored[j].Entry.UpdatedAt)
			}
			return scored[i].Entry.Name < scored[j].Entry.Name
		}
		return scored[i].Score > scored[j].Score
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

func nonZeroScores(scores []float64) []float64 {
	out := make([]float64, 0, len(scores))
	for _, score := range scores {
		if score > 0 {
			out = append(out, score)
		}
	}
	return out
}
