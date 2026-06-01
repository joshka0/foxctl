package todosync

import (
	"fmt"
	"strings"
	"testing"
	"testing/quick"
)

func TestAppendTaskIDReplacesExistingTags(t *testing.T) {
	t.Parallel()

	got := AppendTaskID("Fix parser 〔T:old1〕 and cleanup 〔T:old2〕", "new123")
	if taskID := ParseTaskID(got); taskID != "new123" {
		t.Fatalf("ParseTaskID(%q)=%q want new123", got, taskID)
	}
	if count := strings.Count(got, "〔T:"); count != 1 {
		t.Fatalf("tag count=%d want 1 in %q", count, got)
	}
	if strings.Contains(got, "old1") || strings.Contains(got, "old2") {
		t.Fatalf("old task id leaked after replacement: %q", got)
	}
}

func TestTaskIDTagPropertyRoundTripsGeneratedAlphanumericIDs(t *testing.T) {
	t.Parallel()

	property := func(rawContent string, idSeed uint32) bool {
		taskID := generatedTaskID(idSeed)
		content := strings.ReplaceAll(rawContent, "〔T:", "")
		content = strings.ReplaceAll(content, "〕", "")
		if len(content) > 120 {
			content = content[:120]
		}

		tagged := AppendTaskID(content, taskID)
		if ParseTaskID(tagged) != taskID {
			t.Logf("ParseTaskID(%q) != %q", tagged, taskID)
			return false
		}
		if !HasTaskID(tagged) {
			t.Logf("HasTaskID(%q)=false", tagged)
			return false
		}
		stripped := StripTaskID(tagged)
		if HasTaskID(stripped) || ParseTaskID(stripped) != "" {
			t.Logf("StripTaskID(%q) left tag in %q", tagged, stripped)
			return false
		}
		replaced := AppendTaskID(tagged, taskID+"x")
		return ParseTaskID(replaced) == taskID+"x" && strings.Count(replaced, "〔T:") == 1
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func generatedTaskID(seed uint32) string {
	return fmt.Sprintf("task%08x", seed)
}
