package console

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
	"time"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	store, err := NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new console store: %v", err)
	}
	return store
}

func TestReadsRejectCorruptPersistedTimestamps(t *testing.T) {
	ctx := context.Background()

	for _, column := range []string{"created_at", "last_attached_at"} {
		t.Run(column, func(t *testing.T) {
			store := newTestStore(t)
			session := ConsoleSession{
				ConsoleID:      "console-" + column,
				ActorID:        "actor-1",
				SessionID:      "session-1",
				Workspace:      "/workspace",
				CreatedAt:      time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC),
				LastAttachedAt: time.Date(2026, time.May, 25, 12, 1, 0, 0, time.UTC),
			}
			if err := store.Create(ctx, session); err != nil {
				t.Fatalf("create session: %v", err)
			}

			if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE console_sessions SET %s = ? WHERE console_id = ?
			`, column), "not-a-timestamp", session.ConsoleID); err != nil {
				t.Fatalf("corrupt %s: %v", column, err)
			}

			if _, err := store.Get(ctx, session.ConsoleID); !consoleReadErrorNamesColumn(err, column) {
				t.Fatalf("Get() error=%v, want it to name corrupt %s", err, column)
			}
			if _, err := store.GetByActor(ctx, session.ActorID); !consoleReadErrorNamesColumn(err, column) {
				t.Fatalf("GetByActor() error=%v, want it to name corrupt %s", err, column)
			}
			if _, err := store.GetBySession(ctx, session.SessionID); !consoleReadErrorNamesColumn(err, column) {
				t.Fatalf("GetBySession() error=%v, want it to name corrupt %s", err, column)
			}
			if _, err := store.List(ctx, session.Workspace, 10); !consoleReadErrorNamesColumn(err, column) {
				t.Fatalf("List() error=%v, want it to name corrupt %s", err, column)
			}
		})
	}
}

func TestReadsRejectCorruptPersistedMetadata(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "malformed", value: "{"},
		{name: "null", value: "null"},
		{name: "array", value: `["terminal"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := newTestStore(t)
			session := ConsoleSession{
				ConsoleID:      "console-corrupt-meta-" + tt.name,
				ActorID:        "actor-1",
				SessionID:      "session-1",
				Workspace:      "/workspace",
				CreatedAt:      time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC),
				LastAttachedAt: time.Date(2026, time.May, 25, 12, 1, 0, 0, time.UTC),
				Meta:           map[string]any{"origin": "terminal"},
			}
			if err := store.Create(ctx, session); err != nil {
				t.Fatalf("create session: %v", err)
			}

			if _, err := store.db.ExecContext(ctx, `
				UPDATE console_sessions SET meta = ? WHERE console_id = ?
			`, tt.value, session.ConsoleID); err != nil {
				t.Fatalf("corrupt meta: %v", err)
			}

			if _, err := store.Get(ctx, session.ConsoleID); !consoleReadErrorNamesColumn(err, "meta") {
				t.Fatalf("Get() error=%v, want it to name corrupt meta", err)
			}
			if _, err := store.GetByActor(ctx, session.ActorID); !consoleReadErrorNamesColumn(err, "meta") {
				t.Fatalf("GetByActor() error=%v, want it to name corrupt meta", err)
			}
			if _, err := store.GetBySession(ctx, session.SessionID); !consoleReadErrorNamesColumn(err, "meta") {
				t.Fatalf("GetBySession() error=%v, want it to name corrupt meta", err)
			}
			if _, err := store.List(ctx, session.Workspace, 10); !consoleReadErrorNamesColumn(err, "meta") {
				t.Fatalf("List() error=%v, want it to name corrupt meta", err)
			}
		})
	}
}

func TestDecodeSessionMetaJSONProperty(t *testing.T) {
	roundTripsObjectMetadata := func(input map[string]string) bool {
		if input == nil {
			input = map[string]string{}
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			return false
		}
		got, err := decodeSessionMetaJSON(string(encoded))
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

func consoleReadErrorNamesColumn(err error, column string) bool {
	return err != nil && strings.Contains(err.Error(), column)
}
