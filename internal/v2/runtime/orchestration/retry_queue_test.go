package orchestration

import (
	"fmt"
	"sort"
	"testing"
	"testing/quick"
	"time"
)

func TestRetryQueue_RejectsBlankIssueAndReplacesExistingAtCapacity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 6, 9, 0, 0, 0, time.UTC)
	queue := NewRetryQueue(1)

	if queue.Upsert(RetryEntry{Candidate: Candidate{IssueID: "   "}, DueAt: now}) {
		t.Fatal("blank issue id should be rejected")
	}
	if queue.Len() != 0 {
		t.Fatalf("queue len=%d want 0 after blank issue", queue.Len())
	}

	if ok := queue.Upsert(RetryEntry{
		Candidate: Candidate{IssueID: " issue-1 "},
		Attempt:   4,
		DueAt:     now.Add(time.Hour),
		LastError: "old failure",
	}); !ok {
		t.Fatal("initial retry should be accepted")
	}
	if ok := queue.Upsert(RetryEntry{
		Candidate: Candidate{IssueID: "issue-1"},
		Attempt:   0,
		DueAt:     now,
		LastError: "new failure",
	}); !ok {
		t.Fatal("existing retry should be replaceable even when queue is at capacity")
	}
	if queue.Upsert(RetryEntry{
		Candidate: Candidate{IssueID: "issue-2"},
		DueAt:     now,
	}) {
		t.Fatal("new issue should be rejected when queue is at capacity")
	}

	due := queue.PopDue(now, 10)
	if len(due) != 1 {
		t.Fatalf("due len=%d want 1", len(due))
	}
	if due[0].Candidate.IssueID != "issue-1" {
		t.Fatalf("issue id=%q want issue-1", due[0].Candidate.IssueID)
	}
	if due[0].LastError != "new failure" {
		t.Fatalf("last error=%q want new failure", due[0].LastError)
	}
	if due[0].Attempt != 1 {
		t.Fatalf("attempt=%d want normalized floor 1", due[0].Attempt)
	}
}

func TestRetryQueue_PopDueRemovesEntriesByTrimmedIssueID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 6, 9, 30, 0, 0, time.UTC)
	queue := NewRetryQueue(4)
	if ok := queue.Upsert(RetryEntry{
		Candidate: Candidate{IssueID: " issue-1 "},
		Attempt:   1,
		DueAt:     now,
	}); !ok {
		t.Fatal("spaced issue id should be accepted by trimmed queue key")
	}

	due := queue.PopDue(now, 1)
	if len(due) != 1 {
		t.Fatalf("due len=%d want 1", len(due))
	}
	if queue.Len() != 0 {
		t.Fatalf("queue len=%d want 0 after popping spaced issue id", queue.Len())
	}
	if again := queue.PopDue(now, 1); len(again) != 0 {
		t.Fatalf("spaced issue id was returned again: %+v", again)
	}
}

func TestRetryQueue_PopDueProperty(t *testing.T) {
	t.Parallel()

	property := func(raw []retryQueuePropertyCase, rawLimit uint8) bool {
		now := time.Date(2026, time.March, 6, 10, 0, 0, 0, time.UTC)
		queue := NewRetryQueue(len(raw) + 1)
		expectedByIssue := map[string]RetryEntry{}

		for _, item := range raw {
			entry := retryQueuePropertyEntry(now, item)
			if !queue.Upsert(entry) {
				return false
			}
			expectedByIssue[entry.Candidate.IssueID] = entry
			if expectedByIssue[entry.Candidate.IssueID].Attempt < 1 {
				normalized := expectedByIssue[entry.Candidate.IssueID]
				normalized.Attempt = 1
				expectedByIssue[entry.Candidate.IssueID] = normalized
			}
		}

		expectedDue := make([]RetryEntry, 0, len(expectedByIssue))
		for _, entry := range expectedByIssue {
			if entry.DueAt.IsZero() || !entry.DueAt.After(now) {
				expectedDue = append(expectedDue, entry)
			}
		}
		sortRetryEntries(expectedDue)

		limit := int(rawLimit%8) + 1
		wantPop := min(limit, len(expectedDue))

		got := queue.PopDue(now, limit)
		if len(got) != wantPop {
			return false
		}
		for i, entry := range got {
			if entry.Candidate.IssueID != expectedDue[i].Candidate.IssueID {
				return false
			}
			if !entry.DueAt.Equal(expectedDue[i].DueAt) {
				return false
			}
			if entry.Attempt != expectedDue[i].Attempt {
				return false
			}
		}
		if queue.Len() != len(expectedByIssue)-wantPop {
			return false
		}

		remainingDue := queue.PopDue(now, 0)
		if len(remainingDue) != len(expectedDue)-wantPop {
			return false
		}

		expectedFuture := len(expectedByIssue) - len(expectedDue)
		future := queue.PopDue(now.Add(5*time.Minute), 0)
		if len(future) != expectedFuture {
			return false
		}
		return queue.Len() == 0
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 128}); err != nil {
		t.Fatalf("retry queue property failed: %v", err)
	}
}

type retryQueuePropertyCase struct {
	ID      uint8
	Offset  int8
	Attempt uint8
}

func retryQueuePropertyEntry(now time.Time, item retryQueuePropertyCase) RetryEntry {
	dueAt := now.Add(time.Duration(item.Offset) * time.Second)
	if item.Offset == 0 && item.ID%5 == 0 {
		dueAt = time.Time{}
	}
	return RetryEntry{
		Candidate: Candidate{
			IssueID: fmt.Sprintf("issue-%03d", item.ID%32),
		},
		Attempt: int(item.Attempt % 4),
		DueAt:   dueAt,
	}
}

func sortRetryEntries(entries []RetryEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].DueAt.Equal(entries[j].DueAt) {
			return entries[i].Candidate.IssueID < entries[j].Candidate.IssueID
		}
		return entries[i].DueAt.Before(entries[j].DueAt)
	})
}
