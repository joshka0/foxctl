package dbdriver

import (
	"math"
	"strings"
	"testing"
	"testing/quick"
)

func TestValidateSQLIdentifierAllowsOnlyPortableIdentifiers(t *testing.T) {
	t.Parallel()

	valid := []string{
		"documents",
		"_documents",
		"documents_2026",
		strings.Repeat("a", 64),
	}
	for _, identifier := range valid {
		if err := validateSQLIdentifier(identifier); err != nil {
			t.Fatalf("validateSQLIdentifier(%q) error = %v, want nil", identifier, err)
		}
	}

	invalid := []string{
		"",
		"1documents",
		"documents.archive",
		"documents archive",
		"documents; DROP TABLE documents",
		`"documents"`,
		"documents/*comment*/",
		strings.Repeat("a", 65),
	}
	for _, identifier := range invalid {
		if err := validateSQLIdentifier(identifier); err == nil {
			t.Fatalf("validateSQLIdentifier(%q) error = nil, want rejection", identifier)
		}
	}
}

func TestValidateSQLIdentifierPropertyRejectsInjectedSuffixes(t *testing.T) {
	t.Parallel()

	property := func(raw []byte, suffixSeed uint8) bool {
		identifier := generatedSQLIdentifier(raw)
		if err := validateSQLIdentifier(identifier); err != nil {
			t.Logf("valid identifier %q rejected: %v", identifier, err)
			return false
		}

		suffixes := []string{
			";DROP",
			".child",
			" child",
			"--comment",
			"/*comment*/",
			`"`,
			"'",
		}
		mutated := identifier + suffixes[int(suffixSeed)%len(suffixes)]
		if err := validateSQLIdentifier(mutated); err == nil {
			t.Logf("mutated identifier %q accepted", mutated)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func TestBM25ScoreUsesUniqueQueryTerms(t *testing.T) {
	t.Parallel()

	scorer := NewBM25Scorer(DefaultBM25Params(), CorpusStats{
		TotalDocs:    10,
		AvgDocLength: 4,
		DocFreqs: map[string]int{
			"fox": 2,
		},
	})
	doc := DocumentStats{
		ID:        "doc-1",
		Length:    4,
		TermFreqs: TermFrequency{"fox": 3},
	}

	once := scorer.Score([]string{"fox"}, doc)
	repeated := scorer.Score([]string{"fox", "fox", "fox"}, doc)
	if once != repeated {
		t.Fatalf("repeated query terms changed BM25 score: once=%v repeated=%v", once, repeated)
	}
	if once <= 0 || math.IsNaN(once) || math.IsInf(once, 0) {
		t.Fatalf("score = %v, want positive finite", once)
	}
}

func TestSearchResultsSortAndTopKProperty(t *testing.T) {
	t.Parallel()

	property := func(raw []int16, rawK uint8) bool {
		if len(raw) > 32 {
			raw = raw[:32]
		}
		results := make(SearchResults, len(raw))
		for i, score := range raw {
			results[i] = SearchResult{
				DocumentID: string(rune('a' + i%26)),
				Score:      float64(score) / 8,
			}
		}

		results.Sort()
		for i := 1; i < len(results); i++ {
			if results[i-1].Score < results[i].Score {
				t.Logf("results not sorted descending at %d: %v", i, results)
				return false
			}
		}

		k := int(rawK % 40)
		top := results.TopK(k)
		if len(top) != min(k, len(results)) {
			t.Logf("TopK(%d) len=%d want %d", k, len(top), min(k, len(results)))
			return false
		}
		for i := range top {
			if top[i].DocumentID != results[i].DocumentID || top[i].Score != results[i].Score {
				t.Logf("TopK(%d)[%d]=%+v want prefix %+v", k, i, top[i], results[i])
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func generatedSQLIdentifier(raw []byte) string {
	if len(raw) == 0 {
		return "_"
	}
	if len(raw) > 63 {
		raw = raw[:63]
	}
	var b strings.Builder
	b.Grow(len(raw) + 1)
	b.WriteByte('_')
	for _, value := range raw {
		switch value % 3 {
		case 0:
			b.WriteByte('a' + value%26)
		case 1:
			b.WriteByte('A' + value%26)
		default:
			b.WriteByte('0' + value%10)
		}
	}
	return b.String()
}
