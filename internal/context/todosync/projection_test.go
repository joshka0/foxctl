package todosync

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

func TestFormatContentTinyMaxLengthKeepsTaskIDAndBudget(t *testing.T) {
	t.Parallel()

	for _, maxLen := range []int{1, 2, 3} {
		t.Run(strings.Repeat("x", maxLen), func(t *testing.T) {
			got := FormatContent("abcdef", StatusPending, "task123", 0, ProjectionConfig{
				MaxContentLength: maxLen,
			})

			if taskID := ParseTaskID(got); taskID != "task123" {
				t.Fatalf("task id=%q want task123 in %q", taskID, got)
			}
			if visible := StripTaskID(got); utf8.RuneCountInString(visible) > maxLen {
				t.Fatalf("visible content length=%d want <= %d in %q", len(visible), maxLen, got)
			}
		})
	}
}

func TestFormatContentTruncatesUnicodeTitlesOnRuneBoundary(t *testing.T) {
	t.Parallel()

	got := FormatContent("éééé", StatusPending, "task123", 0, ProjectionConfig{
		MaxContentLength: 4,
	})
	visible := StripTaskID(got)

	if !utf8.ValidString(visible) {
		t.Fatalf("visible content is not valid UTF-8: %q", visible)
	}
	if utf8.RuneCountInString(visible) > 4 {
		t.Fatalf("visible rune length=%d want <= 4 in %q", utf8.RuneCountInString(visible), got)
	}
	if taskID := ParseTaskID(got); taskID != "task123" {
		t.Fatalf("task id=%q want task123 in %q", taskID, got)
	}
}

func TestFormatContentGeneratedMaxLengthKeepsTaskIDBudgetAndUTF8(t *testing.T) {
	t.Parallel()

	titleBudgetIsEnforced := func(raw string, rawMax uint8) bool {
		maxLen := int(rawMax%16) + 1
		title := "title-" + raw
		if !utf8.ValidString(title) {
			return true
		}

		got := FormatContent(title, StatusPending, "task123", 0, ProjectionConfig{
			MaxContentLength: maxLen,
		})

		visible := StripTaskID(got)
		return ParseTaskID(got) == "task123" &&
			utf8.ValidString(visible) &&
			utf8.RuneCountInString(visible) <= maxLen
	}

	if err := quick.Check(titleBudgetIsEnforced, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("generated content exceeded title budget or lost task id: %v", err)
	}
}
