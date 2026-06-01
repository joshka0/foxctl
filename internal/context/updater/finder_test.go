package updater

import (
	"context"
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

func TestFinderFindContextAcceptsShortSourceIDs(t *testing.T) {
	t.Parallel()

	finder := NewFinder(
		nil,
		staticSessionSearcher{results: []SessionResult{{
			SessionID: "abc",
			Content:   "short session id should not panic",
			Type:      "learning",
			Score:     0.8,
		}}},
		staticCodemapSearcher{results: []CodemapResult{{
			ID:      "xy",
			Query:   "short ids",
			Summary: "short codemap id should not panic",
			Score:   0.9,
		}}},
		DefaultFinderConfig(),
	)

	candidates, err := finder.FindContext(context.Background(), &AnalysisResult{
		SearchQueries: []string{"short ids"},
	}, "current-session", "workspace")
	if err != nil {
		t.Fatalf("FindContext() error = %v", err)
	}

	assertCandidateSource(t, candidates, "session:abc")
	assertCandidateSource(t, candidates, "codemap:xy")
}

func TestFinderFindContextPropertyGeneratedIDsStayBoundedAndUTF8(t *testing.T) {
	t.Parallel()

	property := func(sessionID, codemapID, content, summary string, sessionScoreRaw, codemapScoreRaw uint8) bool {
		sessionScore := float32(sessionScoreRaw)/255*0.5 + 0.5
		codemapScore := float32(codemapScoreRaw)/255*0.5 + 0.5
		finder := NewFinder(
			nil,
			staticSessionSearcher{results: []SessionResult{{
				SessionID: sessionID,
				Content:   content,
				Type:      "decision",
				Score:     sessionScore,
			}}},
			staticCodemapSearcher{results: []CodemapResult{{
				ID:      codemapID,
				Query:   "query",
				Summary: summary,
				Score:   codemapScore,
			}}},
			DefaultFinderConfig(),
		)

		candidates, err := finder.FindContext(context.Background(), &AnalysisResult{
			SearchQueries: []string{"query"},
		}, "current-session", "workspace")
		if err != nil {
			t.Logf("FindContext() error = %v", err)
			return false
		}
		if len(candidates) != 2 {
			t.Logf("candidate count = %d, want 2: %#v", len(candidates), candidates)
			return false
		}
		for _, candidate := range candidates {
			if !utf8.ValidString(candidate.Source) || !utf8.ValidString(candidate.Content) {
				t.Logf("candidate has invalid UTF-8: %#v", candidate)
				return false
			}
			if strings.HasPrefix(candidate.Source, "session:") {
				if utf8.RuneCountInString(strings.TrimPrefix(candidate.Source, "session:")) > sourceIDLimit {
					t.Logf("session source not bounded: %q", candidate.Source)
					return false
				}
			}
			if strings.HasPrefix(candidate.Source, "codemap:") {
				if utf8.RuneCountInString(strings.TrimPrefix(candidate.Source, "codemap:")) > sourceIDLimit {
					t.Logf("codemap source not bounded: %q", candidate.Source)
					return false
				}
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("generated finder IDs property failed: %v", err)
	}
}

func TestFinderSkipsCurrentSessionAndDeduplicatesHighestScore(t *testing.T) {
	t.Parallel()

	finder := NewFinder(
		staticMemorySearcher{results: []MemoryResult{
			{ID: "mem-1", Type: "decision", Summary: "lower score", Score: 0.6},
			{ID: "mem-1", Type: "decision", Summary: "higher score", Score: 0.9},
		}},
		staticSessionSearcher{results: []SessionResult{{
			SessionID: "current-session",
			Content:   "current session should be filtered",
			Type:      "learning",
			Score:     1,
		}}},
		nil,
		DefaultFinderConfig(),
	)

	candidates, err := finder.FindContext(context.Background(), &AnalysisResult{
		SearchQueries: []string{"dedupe"},
	}, "current-session", "workspace")
	if err != nil {
		t.Fatalf("FindContext() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(candidates), candidates)
	}
	if candidates[0].ID != "mem-1" || candidates[0].Score != 0.9 {
		t.Fatalf("kept candidate = %#v, want highest scoring mem-1", candidates[0])
	}
}

func TestMatchesActiveFilesIgnoresEmptyNamesAndPathSeparators(t *testing.T) {
	t.Parallel()

	if matchesActiveFiles("this content mentions nothing", []string{"", "   ", "/"}) {
		t.Fatal("empty active file names must not match every content string")
	}
	if !matchesActiveFiles("updated config_test.go for validation", []string{`internal\context\updater\config_test.go`}) {
		t.Fatal("Windows-style active file path should match by basename")
	}
}

func TestTruncatePreservesUTF8AndRuneLimit(t *testing.T) {
	t.Parallel()

	got := truncate(strings.Repeat("é", 201), 200)
	want := strings.Repeat("é", 197) + "..."
	if got != want {
		t.Fatalf("truncate() = %q, want %q", got, want)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncate() produced invalid UTF-8: %q", got)
	}
}

func TestTruncatePropertyKeepsOutputUTF8AndWithinLimit(t *testing.T) {
	t.Parallel()

	property := func(input string, rawLimit uint8) bool {
		limit := int(rawLimit)
		got := truncate(input, limit)
		if !utf8.ValidString(got) {
			t.Logf("truncate(%q, %d) produced invalid UTF-8 %q", input, limit, got)
			return false
		}
		if utf8.RuneCountInString(got) > limit {
			t.Logf("truncate(%q, %d) produced %d runes", input, limit, utf8.RuneCountInString(got))
			return false
		}
		if utf8.RuneCountInString(input) <= limit && got != input {
			t.Logf("truncate(%q, %d) changed unbounded input to %q", input, limit, got)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("truncate property failed: %v", err)
	}
}

type staticMemorySearcher struct {
	results []MemoryResult
}

func (s staticMemorySearcher) SearchByQuery(context.Context, string, string, int) ([]MemoryResult, error) {
	return s.results, nil
}

type staticSessionSearcher struct {
	results []SessionResult
}

func (s staticSessionSearcher) SearchSessions(context.Context, string, int) ([]SessionResult, error) {
	return s.results, nil
}

type staticCodemapSearcher struct {
	results []CodemapResult
}

func (s staticCodemapSearcher) SearchCodemaps(context.Context, string, int) ([]CodemapResult, error) {
	return s.results, nil
}

func assertCandidateSource(t *testing.T, candidates []ContextCandidate, source string) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.Source == source {
			return
		}
	}
	t.Fatalf("candidate source %q not found in %#v", source, candidates)
}
