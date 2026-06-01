package history

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode"
	"unicode/utf8"
)

func TestBuildHistoryPackTruncatesOverviewByRune(t *testing.T) {
	t.Parallel()

	pack := BuildHistoryPack([]HistoryAnswer{{
		QuestionID: HistoryQuestionObjective,
		Answer:     strings.Repeat("é", 260),
		Confidence: 0.8,
	}})
	if pack == nil {
		t.Fatal("expected history pack")
	}
	if !utf8.ValidString(pack.Overview) {
		t.Fatalf("overview is invalid UTF-8: %q", pack.Overview)
	}
	if got := utf8.RuneCountInString(pack.Overview); got > 240 {
		t.Fatalf("overview rune count=%d want <= 240", got)
	}
	if pack.AgentBrief == "" || !utf8.ValidString(pack.AgentBrief) {
		t.Fatalf("agent brief should be non-empty valid UTF-8: %q", pack.AgentBrief)
	}
}

func TestTruncateInlinePropertyNormalizesAndBoundsGeneratedText(t *testing.T) {
	t.Parallel()

	property := func(input string, rawLimit uint8) bool {
		limit := int(rawLimit)
		got := truncateInline(input, limit)
		if !utf8.ValidString(got) {
			t.Logf("truncateInline(%q, %d) produced invalid UTF-8: %q", input, limit, got)
			return false
		}
		normalized := normalizeInlineForTest(input)
		if limit <= 0 {
			return got == normalized
		}
		if utf8.RuneCountInString(got) > limit {
			t.Logf("truncateInline(%q, %d) produced %d runes", input, limit, utf8.RuneCountInString(got))
			return false
		}
		if utf8.RuneCountInString(normalized) <= limit {
			return got == normalized
		}
		if limit > 1 && !strings.HasSuffix(got, "…") {
			t.Logf("truncated output missing ellipsis: %q", got)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("truncateInline property failed: %v", err)
	}
}

func normalizeInlineForTest(input string) string {
	fields := strings.FieldsFunc(strings.TrimSpace(input), unicode.IsSpace)
	return strings.Join(fields, " ")
}
