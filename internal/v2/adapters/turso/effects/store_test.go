package effects

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestModelEffectSaveGetIdempotentConflict(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	record := modelRecord()
	saved, err := store.SaveModelEffect(ctx, record)
	if err != nil {
		t.Fatalf("SaveModelEffect() error = %v", err)
	}
	if string(saved.InputJSON) != `{"messages":["hi"]}` || string(saved.ResponseJSON) != `{"content":"hello"}` {
		t.Fatalf("saved json=%s/%s", saved.InputJSON, saved.ResponseJSON)
	}
	if saved.Status != run.ModelEffectSucceeded {
		t.Fatalf("saved status=%q want succeeded", saved.Status)
	}

	got, err := store.GetModelEffect(ctx, record.EffectKey)
	if err != nil {
		t.Fatalf("GetModelEffect() error = %v", err)
	}
	if !got.CreatedAt.Equal(saved.CreatedAt) || string(got.ResponseJSON) != string(saved.ResponseJSON) {
		t.Fatalf("got=%+v want saved=%+v", got, saved)
	}

	replayed, err := store.SaveModelEffect(ctx, record)
	if err != nil {
		t.Fatalf("SaveModelEffect() replay error = %v", err)
	}
	if !replayed.CreatedAt.Equal(saved.CreatedAt) {
		t.Fatalf("replay created_at=%s want existing %s", replayed.CreatedAt, saved.CreatedAt)
	}

	conflict := record
	conflict.ResponseJSON = []byte(`{"content":"different"}`)
	_, err = store.SaveModelEffect(ctx, conflict)
	if !errors.Is(err, run.ErrEffectConflict) {
		t.Fatalf("SaveModelEffect() conflict error=%v want ErrEffectConflict", err)
	}

	_, err = store.GetModelEffect(ctx, run.EffectKey{RunID: "run-001", RequestID: "req-001", TurnID: "turn-001", IterationIndex: 99})
	if !errors.Is(err, run.ErrEffectNotFound) {
		t.Fatalf("GetModelEffect() missing error=%v want ErrEffectNotFound", err)
	}
}

func TestModelEffectIntentCompleteAndReplay(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	record := modelRecord()
	intent, err := store.BeginModelEffect(ctx, record)
	if err != nil {
		t.Fatalf("BeginModelEffect() error = %v", err)
	}
	if intent.Status != run.ModelEffectIntent {
		t.Fatalf("intent status=%q want intent", intent.Status)
	}
	if intent.Status.IsTerminal() {
		t.Fatalf("intent status reported terminal")
	}
	if len(intent.ResponseJSON) != 0 || intent.ErrorMessage != "" {
		t.Fatalf("intent has terminal data: response=%s error=%q", intent.ResponseJSON, intent.ErrorMessage)
	}

	replayedIntent, err := store.BeginModelEffect(ctx, record)
	if err != nil {
		t.Fatalf("BeginModelEffect() replay error = %v", err)
	}
	if !replayedIntent.CreatedAt.Equal(intent.CreatedAt) || replayedIntent.Status != run.ModelEffectIntent {
		t.Fatalf("intent replay=%+v want existing=%+v", replayedIntent, intent)
	}

	inputConflict := record
	inputConflict.InputJSON = []byte(`{"messages":["other"]}`)
	_, err = store.BeginModelEffect(ctx, inputConflict)
	if !errors.Is(err, run.ErrEffectConflict) {
		t.Fatalf("BeginModelEffect() conflict error=%v want ErrEffectConflict", err)
	}

	terminal := record
	terminal.Status = run.ModelEffectSucceeded
	completed, err := store.CompleteModelEffect(ctx, terminal)
	if err != nil {
		t.Fatalf("CompleteModelEffect() error = %v", err)
	}
	if completed.Status != run.ModelEffectSucceeded || string(completed.ResponseJSON) != `{"content":"hello"}` {
		t.Fatalf("completed=%+v", completed)
	}

	replayedComplete, err := store.CompleteModelEffect(ctx, terminal)
	if err != nil {
		t.Fatalf("CompleteModelEffect() replay error = %v", err)
	}
	if !replayedComplete.UpdatedAt.Equal(completed.UpdatedAt) || string(replayedComplete.ResponseJSON) != string(completed.ResponseJSON) {
		t.Fatalf("terminal replay=%+v want existing=%+v", replayedComplete, completed)
	}

	began, err := store.BeginModelEffect(ctx, record)
	if err != nil {
		t.Fatalf("BeginModelEffect() after terminal error = %v", err)
	}
	if began.Status != run.ModelEffectSucceeded || string(began.ResponseJSON) != `{"content":"hello"}` {
		t.Fatalf("BeginModelEffect() after terminal=%+v want completed", began)
	}

	resultConflict := terminal
	resultConflict.ResponseJSON = []byte(`{"content":"different"}`)
	_, err = store.CompleteModelEffect(ctx, resultConflict)
	if !errors.Is(err, run.ErrEffectConflict) {
		t.Fatalf("CompleteModelEffect() result conflict error=%v want ErrEffectConflict", err)
	}
}

func TestToolEffectIntentGetAndNonTerminal(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	intent, err := store.BeginToolEffect(ctx, toolIntent())
	if err != nil {
		t.Fatalf("BeginToolEffect() error = %v", err)
	}
	if intent.Status != run.ToolEffectIntent {
		t.Fatalf("status=%q want intent", intent.Status)
	}
	if intent.Status.IsTerminal() {
		t.Fatalf("intent status reported terminal")
	}
	if len(intent.ResultJSON) != 0 || intent.ErrorMessage != "" {
		t.Fatalf("intent has terminal data: result=%s error=%q", intent.ResultJSON, intent.ErrorMessage)
	}

	got, err := store.GetToolEffect(ctx, intent.EffectKey)
	if err != nil {
		t.Fatalf("GetToolEffect() error = %v", err)
	}
	if got.Status.IsTerminal() {
		t.Fatalf("GetToolEffect() status=%q want non-terminal", got.Status)
	}

	replayed, err := store.BeginToolEffect(ctx, toolIntent())
	if err != nil {
		t.Fatalf("BeginToolEffect() replay error = %v", err)
	}
	if !replayed.CreatedAt.Equal(intent.CreatedAt) || replayed.Status != run.ToolEffectIntent {
		t.Fatalf("replay=%+v want existing=%+v", replayed, intent)
	}

	_, err = store.GetToolEffect(ctx, run.EffectKey{RunID: "run-001", RequestID: "req-001", TurnID: "turn-001", IterationIndex: 1, ToolCallID: "missing"})
	if !errors.Is(err, run.ErrEffectNotFound) {
		t.Fatalf("GetToolEffect() missing error=%v want ErrEffectNotFound", err)
	}
}

func TestToolEffectCompleteReplayAndTerminalBegin(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	if _, err := store.BeginToolEffect(ctx, toolIntent()); err != nil {
		t.Fatalf("BeginToolEffect() error = %v", err)
	}

	terminal := toolIntent()
	terminal.Status = run.ToolEffectSucceeded
	terminal.ResultJSON = []byte(`{"ok":true}`)
	completed, err := store.CompleteToolEffect(ctx, terminal)
	if err != nil {
		t.Fatalf("CompleteToolEffect() error = %v", err)
	}
	if completed.Status != run.ToolEffectSucceeded || string(completed.ResultJSON) != `{"ok":true}` {
		t.Fatalf("completed=%+v", completed)
	}

	replayed, err := store.CompleteToolEffect(ctx, terminal)
	if err != nil {
		t.Fatalf("CompleteToolEffect() replay error = %v", err)
	}
	if !replayed.UpdatedAt.Equal(completed.UpdatedAt) || string(replayed.ResultJSON) != string(completed.ResultJSON) {
		t.Fatalf("terminal replay=%+v want existing=%+v", replayed, completed)
	}

	began, err := store.BeginToolEffect(ctx, toolIntent())
	if err != nil {
		t.Fatalf("BeginToolEffect() after terminal error = %v", err)
	}
	if began.Status != run.ToolEffectSucceeded || string(began.ResultJSON) != `{"ok":true}` {
		t.Fatalf("BeginToolEffect() after terminal=%+v want completed", began)
	}
}

func TestToolEffectConflictingArgsAndResult(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	if _, err := store.BeginToolEffect(ctx, toolIntent()); err != nil {
		t.Fatalf("BeginToolEffect() error = %v", err)
	}

	argConflict := toolIntent()
	argConflict.ArgsJSON = []byte(`{"path":"other"}`)
	_, err := store.BeginToolEffect(ctx, argConflict)
	if !errors.Is(err, run.ErrEffectConflict) {
		t.Fatalf("BeginToolEffect() arg conflict error=%v want ErrEffectConflict", err)
	}

	terminal := toolIntent()
	terminal.Status = run.ToolEffectFailed
	terminal.ErrorMessage = "first failure"
	completed, err := store.CompleteToolEffect(ctx, terminal)
	if err != nil {
		t.Fatalf("CompleteToolEffect() error = %v", err)
	}
	if completed.Status != run.ToolEffectFailed || completed.ErrorMessage != "first failure" {
		t.Fatalf("completed=%+v", completed)
	}

	resultConflict := terminal
	resultConflict.ErrorMessage = "second failure"
	_, err = store.CompleteToolEffect(ctx, resultConflict)
	if !errors.Is(err, run.ErrEffectConflict) {
		t.Fatalf("CompleteToolEffect() result conflict error=%v want ErrEffectConflict", err)
	}

	resultConflict = terminal
	resultConflict.Status = run.ToolEffectSucceeded
	resultConflict.ResultJSON = []byte(`{"ok":true}`)
	resultConflict.ErrorMessage = ""
	_, err = store.CompleteToolEffect(ctx, resultConflict)
	if !errors.Is(err, run.ErrEffectConflict) {
		t.Fatalf("CompleteToolEffect() status conflict error=%v want ErrEffectConflict", err)
	}
}

func newTestStore(t *testing.T) (context.Context, *Store) {
	t.Helper()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "effects.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
	store := NewStore(db)
	store.SetNowForTest(func() time.Time { return now })
	return ctx, store
}

func modelRecord() run.ModelEffectRecord {
	return run.ModelEffectRecord{
		EffectKey: run.EffectKey{
			RunID:          "run-001",
			RequestID:      "req-001",
			TurnID:         "turn-001",
			IterationIndex: 1,
		},
		InputJSON:    []byte(`{"messages":["hi"]}`),
		ResponseJSON: []byte(`{"content":"hello"}`),
	}
}

func toolIntent() run.ToolEffectRecord {
	return run.ToolEffectRecord{
		EffectKey: run.EffectKey{
			RunID:          "run-001",
			RequestID:      "req-001",
			TurnID:         "turn-001",
			IterationIndex: 1,
			ToolCallID:     "call-001",
		},
		ToolName: "fs_read_file",
		ArgsJSON: []byte(`{"path":"README.md"}`),
	}
}
