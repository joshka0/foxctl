package sessions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSessionDecodeRejectsCorruptFields(t *testing.T) {
	ctx := context.Background()

	t.Run("session json", func(t *testing.T) {
		store := openDecodeStore(t)
		defer store.Close()

		saveDecodeSession(t, ctx, store)
		mustExecDecodeTest(t, store, `UPDATE sessions SET accomplished = ? WHERE id = ?`, "{", "sess-decode")

		_, err := store.Get(ctx, "sess-decode")
		requireDecodeError(t, err, "accomplished")
	})

	t.Run("session timestamp", func(t *testing.T) {
		store := openDecodeStore(t)
		defer store.Close()

		saveDecodeSession(t, ctx, store)
		mustExecDecodeTest(t, store, `UPDATE sessions SET started_at = ? WHERE id = ?`, "not-a-time", "sess-decode")

		_, err := store.Get(ctx, "sess-decode")
		requireDecodeError(t, err, "started_at")
	})

	t.Run("session status", func(t *testing.T) {
		store := openDecodeStore(t)
		defer store.Close()

		saveDecodeSession(t, ctx, store)
		mustExecDecodeTest(t, store, `UPDATE sessions SET status = ? WHERE id = ?`, "paused", "sess-decode")

		_, err := store.Get(ctx, "sess-decode")
		if !errors.Is(err, ErrInvalidStatus) {
			t.Fatalf("Get() error = %v, want ErrInvalidStatus", err)
		}
		requireDecodeError(t, err, "paused")
	})

	t.Run("turn json", func(t *testing.T) {
		store := openDecodeStore(t)
		defer store.Close()

		saveDecodeSession(t, ctx, store)
		if _, err := store.SaveTurn(ctx, SessionTurn{
			ID:        "turn-decode",
			SessionID: "sess-decode",
			TurnIndex: 0,
			Role:      "assistant",
			ToolCalls: []ToolCall{{Name: "read", Success: true}},
			Timestamp: time.Now().UTC(),
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("save turn: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE session_turns SET tool_calls = ? WHERE id = ?`, "{", "turn-decode")

		_, err := store.GetTurns(ctx, "sess-decode", TurnListOptions{})
		requireDecodeError(t, err, "tool_calls")
	})

	t.Run("chunk json", func(t *testing.T) {
		store := openDecodeStore(t)
		defer store.Close()

		saveDecodeSession(t, ctx, store)
		if _, err := store.SaveChunk(ctx, SessionChunk{
			ID:             "chunk-decode",
			SessionID:      "sess-decode",
			ChunkIndex:     0,
			ChunkType:      "assistant_response",
			ContentHash:    "hash",
			ContentPreview: "preview",
			ToolsUsed:      []string{"read"},
		}); err != nil {
			t.Fatalf("save chunk: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE session_chunks SET tools_used = ? WHERE id = ?`, "{", "chunk-decode")

		_, err := store.GetChunks(ctx, "sess-decode", 0)
		requireDecodeError(t, err, "tools_used")
	})

	t.Run("context window timestamp", func(t *testing.T) {
		store := openDecodeStore(t)
		defer store.Close()

		saveDecodeSession(t, ctx, store)
		now := time.Now().UTC()
		if _, err := store.SaveContextWindow(ctx, ContextWindow{
			ID:          "window-decode",
			SessionID:   "sess-decode",
			WindowIndex: 0,
			StartedAt:   now,
			EndedAt:     now,
			Trigger:     "manual",
			CreatedAt:   now,
		}); err != nil {
			t.Fatalf("save context window: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE session_context_windows SET started_at = ? WHERE id = ?`, "not-a-time", "window-decode")

		_, err := store.GetContextWindows(ctx, "sess-decode")
		requireDecodeError(t, err, "started_at")
	})
}

func TestSessionDecodePreservesEmptyOptionalFields(t *testing.T) {
	ctx := context.Background()
	store := openDecodeStore(t)
	defer store.Close()

	saveDecodeSession(t, ctx, store)
	mustExecDecodeTest(t, store, `UPDATE sessions SET accomplished = '', ended_at = '' WHERE id = ?`, "sess-decode")

	got, err := store.Get(ctx, "sess-decode")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(got.Accomplished) != 0 {
		t.Fatalf("Accomplished len = %d, want 0", len(got.Accomplished))
	}
	if !got.EndedAt.IsZero() {
		t.Fatalf("EndedAt = %v, want zero", got.EndedAt)
	}
}

func openDecodeStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}

func saveDecodeSession(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	_, err := store.Save(ctx, Session{
		ID:            "sess-decode",
		WorkspacePath: "/workspace/decode",
		ProjectName:   "decode",
		Summary:       "decode",
		Accomplished:  []string{"done"},
		Status:        StatusOK,
	})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}
}

func mustExecDecodeTest(t *testing.T, store *Store, query string, args ...any) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec corrupt fixture: %v", err)
	}
}

func requireDecodeError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected decode error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err.Error(), want)
	}
}
