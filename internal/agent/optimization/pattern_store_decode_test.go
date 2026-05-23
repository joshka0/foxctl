package optimization

import (
	"context"
	"strings"
	"testing"
)

func TestPatternStoreDecodeRejectsCorruptFields(t *testing.T) {
	ctx := context.Background()

	t.Run("tool sequence json", func(t *testing.T) {
		store := openDecodePatternStore(t)
		defer store.Close()

		recordDecodePattern(t, ctx, store)
		mustExecPatternDecodeTest(t, store, `UPDATE patterns SET tool_sequence = ? WHERE agent_role = ?`, "{", "coder")

		_, err := store.List(ctx, "coder", 10)
		requirePatternDecodeError(t, err, "tool_sequence")
	})

	t.Run("last seen timestamp", func(t *testing.T) {
		store := openDecodePatternStore(t)
		defer store.Close()

		recordDecodePattern(t, ctx, store)
		mustExecPatternDecodeTest(t, store, `UPDATE patterns SET last_seen = ? WHERE agent_role = ?`, "not-a-time", "coder")

		_, err := store.List(ctx, "coder", 10)
		requirePatternDecodeError(t, err, "last_seen")
	})
}

func openDecodePatternStore(t *testing.T) PatternStore {
	t.Helper()
	store, err := OpenPatternStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open pattern store: %v", err)
	}
	return store
}

func recordDecodePattern(t *testing.T, ctx context.Context, store PatternStore) {
	t.Helper()
	if err := store.Record(ctx, Pattern{
		AgentRole:    "coder",
		Context:      "decode test",
		ToolSequence: []string{"read", "edit"},
		Outcome:      "success",
	}); err != nil {
		t.Fatalf("record pattern: %v", err)
	}
}

func mustExecPatternDecodeTest(t *testing.T, store PatternStore, query string, args ...any) {
	t.Helper()
	sqlStore := store.(*sqlPatternStore)
	if _, err := sqlStore.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec corrupt fixture: %v", err)
	}
}

func requirePatternDecodeError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected decode error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err.Error(), want)
	}
}
