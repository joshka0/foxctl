package contextbuffer

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
	"time"
)

func TestDrainReturnsPriorityOrderAndConsumesOnlyDrainedEntries(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, item := range []struct {
		source   string
		text     string
		priority int
	}{
		{source: "low", text: "low priority", priority: 3},
		{source: "high", text: "high priority", priority: 1},
		{source: "normal", text: "normal priority", priority: 2},
	} {
		_, err := store.Enqueue(ctx, EnqueueParams{
			WorkspaceID: "workspace",
			SessionID:   "session",
			Source:      item.source,
			Text:        item.text,
			Priority:    item.priority,
			TTL:         time.Hour,
		})
		if err != nil {
			t.Fatalf("enqueue %s: %v", item.source, err)
		}
	}

	result, err := store.Drain(ctx, DrainParams{
		WorkspaceID: "workspace",
		SessionID:   "session",
		Limit:       2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := entryTexts(result.Entries); !equalStrings(got, []string{"high priority", "normal priority"}) {
		t.Fatalf("drained texts=%v want priority order high, normal", got)
	}
	if result.TotalPending != 1 {
		t.Fatalf("total pending=%d want 1 after draining two entries", result.TotalPending)
	}

	count, err := store.Count(ctx, "workspace", "session")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count=%d want only the undrained low-priority entry pending", count)
	}

	peeked, err := store.Peek(ctx, DrainParams{
		WorkspaceID: "workspace",
		SessionID:   "session",
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := entryTexts(peeked.Entries); !equalStrings(got, []string{"low priority"}) {
		t.Fatalf("peeked texts=%v want remaining low-priority entry", got)
	}

	countAfterPeek, err := store.Count(ctx, "workspace", "session")
	if err != nil {
		t.Fatal(err)
	}
	if countAfterPeek != 1 {
		t.Fatalf("count after peek=%d want peek to leave entry pending", countAfterPeek)
	}

	final, err := store.Drain(ctx, DrainParams{
		WorkspaceID: "workspace",
		SessionID:   "session",
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := entryTexts(final.Entries); !equalStrings(got, []string{"low priority"}) {
		t.Fatalf("final drain texts=%v want remaining low-priority entry", got)
	}
	if final.TotalPending != 0 {
		t.Fatalf("final total pending=%d want 0", final.TotalPending)
	}
}

func TestReadsRejectCorruptPersistedPriority(t *testing.T) {
	ctx := context.Background()

	for _, priority := range []int{0, 4, -1} {
		t.Run("priority", func(t *testing.T) {
			store, err := Open(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()

			entry, err := store.Enqueue(ctx, EnqueueParams{
				WorkspaceID: "workspace",
				SessionID:   "session",
				Source:      "hook",
				Text:        "buffered context",
				Priority:    2,
				TTL:         time.Hour,
			})
			if err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			if _, err := store.DB().ExecContext(ctx, `UPDATE context_entries SET priority = $1 WHERE id = $2`, priority, entry.ID); err != nil {
				t.Fatalf("corrupt priority: %v", err)
			}

			if _, err := store.Peek(ctx, DrainParams{WorkspaceID: "workspace", SessionID: "session"}); !contextBufferReadErrorContains(err, "priority") {
				t.Fatalf("Peek() error=%v, want priority corruption", err)
			}
			if _, err := store.Drain(ctx, DrainParams{WorkspaceID: "workspace", SessionID: "session"}); !contextBufferReadErrorContains(err, "priority") {
				t.Fatalf("Drain() error=%v, want priority corruption", err)
			}
		})
	}
}

func TestReadsRejectCorruptPersistedMetadata(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		value string
	}{
		{name: "malformed", value: "{"},
		{name: "null", value: "null"},
		{name: "array", value: `["hook"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := Open(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()

			entry, err := store.Enqueue(ctx, EnqueueParams{
				WorkspaceID: "workspace",
				SessionID:   "session",
				Source:      "hook",
				Text:        "buffered context",
				Priority:    2,
				TTL:         time.Hour,
				Metadata:    map[string]any{"origin": "test"},
			})
			if err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			if _, err := store.DB().ExecContext(ctx, `UPDATE context_entries SET metadata = $1 WHERE id = $2`, tt.value, entry.ID); err != nil {
				t.Fatalf("corrupt metadata: %v", err)
			}

			if _, err := store.Peek(ctx, DrainParams{WorkspaceID: "workspace", SessionID: "session"}); !contextBufferReadErrorContains(err, "metadata") {
				t.Fatalf("Peek() error=%v, want metadata corruption", err)
			}
			if _, err := store.Drain(ctx, DrainParams{WorkspaceID: "workspace", SessionID: "session"}); !contextBufferReadErrorContains(err, "metadata") {
				t.Fatalf("Drain() error=%v, want metadata corruption", err)
			}
		})
	}
}

func TestDecodeEntryMetadataJSONProperty(t *testing.T) {
	roundTripsObjectMetadata := func(input map[string]string) bool {
		if input == nil {
			input = map[string]string{}
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			return false
		}
		got, err := decodeEntryMetadataJSON(string(encoded))
		if err != nil || len(got) != len(input) {
			return false
		}

		want := make(map[string]any, len(input))
		for key, value := range input {
			want[key] = value
		}
		return reflect.DeepEqual(got, want)
	}

	if err := quick.Check(roundTripsObjectMetadata, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("metadata object round-trip property failed: %v", err)
	}
}

func entryTexts(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Text)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contextBufferReadErrorContains(err error, want string) bool {
	return err != nil && strings.Contains(err.Error(), want)
}
