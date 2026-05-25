package history

import (
	"context"
	"strings"
	"testing"
	"testing/quick"
	"unicode"
	"unicode/utf8"

	"github.com/joshka0/foxctl/internal/storage/memory"
)

func TestPersistHistoryRecordsSummariesAreRuneBoundedAndUTF8(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("memory.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	longSummary := strings.Repeat("é", 201)
	persisted, err := PersistHistoryRecords(ctx, store, "/tmp/history-records", "owner-1", "session-1", []HistoryRecord{{
		RecordID:       "record-unicode",
		Kind:           HistoryRecordKindInsight,
		Summary:        longSummary,
		RetrievalText:  "retrieval text",
		NormalizedHash: "sha256:" + strings.Repeat("a", 64),
	}}, nil)
	if err != nil {
		t.Fatalf("PersistHistoryRecords() error = %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted=%d want 1", len(persisted))
	}

	wantSummary := strings.Repeat("é", 199) + "…"
	if persisted[0].Summary != wantSummary {
		t.Fatalf("persisted summary=%q want %q", persisted[0].Summary, wantSummary)
	}
	if !utf8.ValidString(persisted[0].Summary) {
		t.Fatalf("persisted summary is invalid UTF-8: %q", persisted[0].Summary)
	}
	if got := utf8.RuneCountInString(persisted[0].Summary); got != 200 {
		t.Fatalf("persisted summary rune count=%d want 200", got)
	}

	entry, err := store.Get(ctx, persisted[0].Name, "/tmp/history-records")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if entry.Summary != wantSummary {
		t.Fatalf("entry summary=%q want %q", entry.Summary, wantSummary)
	}
}

func TestSummarizeRecordSummaryPropertyNormalizesAndBoundsGeneratedText(t *testing.T) {
	t.Parallel()

	property := func(input string) bool {
		got := summarizeRecordSummary(input)
		if !utf8.ValidString(got) {
			t.Logf("summary is invalid UTF-8: %q", got)
			return false
		}
		if utf8.RuneCountInString(got) > 200 {
			t.Logf("summary has %d runes, want <= 200: %q", utf8.RuneCountInString(got), got)
			return false
		}
		normalized := normalizeRecordSummaryForTest(input)
		if normalized == "" {
			return got == ""
		}
		if utf8.RuneCountInString(normalized) <= 200 {
			if got != normalized {
				t.Logf("summary=%q want normalized %q", got, normalized)
				return false
			}
			return true
		}
		if !strings.HasSuffix(got, "…") {
			t.Logf("truncated summary missing ellipsis: %q", got)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("summary normalization property failed: %v", err)
	}
}

func normalizeRecordSummaryForTest(input string) string {
	fields := strings.FieldsFunc(strings.TrimSpace(input), unicode.IsSpace)
	return strings.Join(fields, " ")
}
