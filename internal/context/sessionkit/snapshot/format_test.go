package snapshot

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

func TestFormatForRestoreIncludesContinuitySections(t *testing.T) {
	t.Parallel()

	longSummary := strings.Repeat("x", 151)
	snap := &Snapshot{
		ActiveTask: &TaskInfo{
			ID:          "task-123",
			Title:       "Harden restore formatter",
			Description: "Protect session continuity output.",
		},
		ActivePlan: &PlanInfo{
			Title:    "Snapshot test plan",
			FileName: "snapshot-plan.md",
			Sections: []string{"Risk", "Verification"},
		},
		PendingTodos: []TaskInfo{
			{Title: "write property test", Status: "in_progress"},
			{Title: "verify mutation", Status: "blocked"},
			{Title: "review unknown state", Status: "surprise"},
		},
		Decisions: []string{"Restore output stays explicit."},
		Insights:  []string{"Related session identifiers may be short."},
		Summary:   "Ready to resume from this snapshot.",
	}

	out := FormatForRestore(snap, []WindowMatch{{
		SessionID:  "abcdef123456",
		Summary:    longSummary,
		Similarity: 0.86,
	}}, "M internal/file.go\n?? notes.md")

	assertContains(t, out, "<session-restore>\n")
	assertContains(t, out, "### Files Modified\n```\nM internal/file.go\n?? notes.md\n```\n\n")
	assertContains(t, out, "- **[86% match]** `session:abcdef12` | "+strings.Repeat("x", 147)+"...")
	assertContains(t, out, "**Harden restore formatter** (ID: task-123)")
	assertContains(t, out, "Protect session continuity output.")
	assertContains(t, out, "**Snapshot test plan** (`snapshot-plan.md`)")
	assertContains(t, out, "  - Risk\n")
	assertContains(t, out, "- 🔄 write property test")
	assertContains(t, out, "- 🚫 verify mutation")
	assertContains(t, out, "- • review unknown state")
	assertContains(t, out, "- Restore output stays explicit.")
	assertContains(t, out, "- Related session identifiers may be short.")
	assertContains(t, out, "### Summary\nReady to resume from this snapshot.")
	assertWrappedRestoreContext(t, out)
}

func TestFormatForRestoreHandlesNilSnapshot(t *testing.T) {
	t.Parallel()

	out := FormatForRestore(nil, nil, "")

	assertWrappedRestoreContext(t, out)
	if strings.Contains(out, "### Active Task") {
		t.Fatalf("nil snapshot emitted active task section:\n%s", out)
	}
}

func TestFormatForRestoreAcceptsShortAndEmptyRelatedSessionIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessionID string
		want      string
	}{
		{name: "empty", sessionID: "", want: "`session:unknown`"},
		{name: "one char", sessionID: "a", want: "`session:a`"},
		{name: "exact limit", sessionID: "12345678", want: "`session:12345678`"},
		{name: "truncated", sessionID: "123456789", want: "`session:12345678`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := FormatForRestore(&Snapshot{}, []WindowMatch{{
				SessionID:  tt.sessionID,
				Summary:    "short summary",
				Similarity: 1,
			}}, "")

			assertContains(t, out, tt.want)
			assertWrappedRestoreContext(t, out)
		})
	}
}

func TestFormatForRestoreTruncatesWindowSummariesByRune(t *testing.T) {
	t.Parallel()

	summary := strings.Repeat("é", 151)

	out := FormatForRestore(&Snapshot{}, []WindowMatch{{
		SessionID:  "unicode-session",
		Summary:    summary,
		Similarity: 0.5,
	}}, "")

	if !utf8.ValidString(out) {
		t.Fatalf("restore output is not valid UTF-8:\n%q", out)
	}
	assertContains(t, out, strings.Repeat("é", 147)+"...")
}

func TestFormatForRestorePropertyRelatedWindowsStayWrappedAndUTF8(t *testing.T) {
	t.Parallel()

	property := func(sessionID string, summary string, similarity uint8) bool {
		out := FormatForRestore(&Snapshot{}, []WindowMatch{{
			SessionID:  sessionID,
			Summary:    summary,
			Similarity: float64(similarity) / 255,
		}}, "")

		if !strings.HasPrefix(out, "<session-restore>\n") {
			t.Logf("restore context missing opening wrapper: %q", out)
			return false
		}
		if !strings.HasSuffix(out, "</session-restore>\n") {
			t.Logf("restore context missing closing wrapper: %q", out)
			return false
		}
		if !strings.Contains(out, "### Related Past Sessions") {
			t.Logf("restore context missing related sessions section: %q", out)
			return false
		}
		if !utf8.ValidString(out) {
			t.Logf("restore context is not valid UTF-8: %q", out)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("related window restore property failed: %v", err)
	}
}

func TestFormatMinimalHandlesNilSnapshotAndSummarizesWork(t *testing.T) {
	t.Parallel()

	if got, want := FormatMinimal(nil), "<session-restore>\n</session-restore>\n"; got != want {
		t.Fatalf("FormatMinimal(nil) = %q, want %q", got, want)
	}

	out := FormatMinimal(&Snapshot{
		ActiveTask:   &TaskInfo{Title: "Finish snapshot hardening"},
		PendingTodos: []TaskInfo{{Title: "test"}, {Title: "lint"}},
	})

	assertContains(t, out, "**Active:** Finish snapshot hardening")
	assertContains(t, out, "**Pending:** 2 tasks")
	assertWrappedRestoreContext(t, out)
}

func assertContains(t *testing.T, haystack string, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q\noutput:\n%s", needle, haystack)
	}
}

func assertWrappedRestoreContext(t *testing.T, out string) {
	t.Helper()
	if !strings.HasPrefix(out, "<session-restore>\n") {
		t.Fatalf("restore context missing opening wrapper:\n%s", out)
	}
	if !strings.HasSuffix(out, "</session-restore>\n") {
		t.Fatalf("restore context missing closing wrapper:\n%s", out)
	}
}
