package knowledge

import (
	"context"
	"strings"
	"testing"
	"testing/quick"
)

func TestDecodeRejectsCorruptKnowledgeKinds(t *testing.T) {
	ctx := context.Background()

	t.Run("item kind", func(t *testing.T) {
		store := openDecodeTestStore(t)
		defer store.Close()

		item, err := store.UpsertItem(ctx, Item{
			Name:       "corrupt-item-kind",
			Kind:       KindPack,
			SourcePath: "docs/knowledge/corrupt-item-kind",
		})
		if err != nil {
			t.Fatalf("UpsertItem: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE knowledge_items SET kind = ? WHERE id = ?`, "unknown-kind", item.ID)

		_, err = store.GetItem(ctx, item.ID)
		requireDecodeError(t, err, "kind")
		_, _, err = store.GetItemByName(ctx, item.Name)
		requireDecodeError(t, err, "kind")
		_, err = store.ListAllItems(ctx)
		requireDecodeError(t, err, "kind")
	})

	t.Run("trigger kind", func(t *testing.T) {
		store := openDecodeTestStore(t)
		defer store.Close()

		item, err := store.UpsertItem(ctx, Item{
			Name:       "corrupt-trigger-kind",
			Kind:       KindPack,
			SourcePath: "docs/knowledge/corrupt-trigger-kind",
		})
		if err != nil {
			t.Fatalf("UpsertItem: %v", err)
		}
		trigger, err := store.AddTrigger(ctx, Trigger{
			ItemID:      item.ID,
			TriggerKind: TriggerKeyword,
			Pattern:     "keyword",
		})
		if err != nil {
			t.Fatalf("AddTrigger: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE knowledge_triggers SET trigger_kind = ? WHERE id = ?`, "unknown-trigger-kind", trigger.ID)

		_, err = store.ListTriggers(ctx, item.ID)
		requireDecodeError(t, err, "trigger_kind")
		_, err = store.ListAllTriggers(ctx)
		requireDecodeError(t, err, "trigger_kind")
	})
}

func TestDecodeRejectsCorruptKnowledgePriority(t *testing.T) {
	ctx := context.Background()
	store := openDecodeTestStore(t)
	defer store.Close()

	item, err := store.UpsertItem(ctx, Item{
		Name:       "corrupt-priority",
		Kind:       KindPack,
		SourcePath: "docs/knowledge/corrupt-priority",
		Priority:   priorityHigh,
	})
	if err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}
	trigger, err := store.AddTrigger(ctx, Trigger{
		ItemID:      item.ID,
		TriggerKind: TriggerKeyword,
		Pattern:     "corrupt-priority",
	})
	if err != nil {
		t.Fatalf("AddTrigger keyword: %v", err)
	}
	if trigger.ID == "" {
		t.Fatalf("expected trigger ID")
	}
	if _, err := store.AddTrigger(ctx, Trigger{
		ItemID:      item.ID,
		TriggerKind: TriggerPath,
		Pattern:     "src/**",
	}); err != nil {
		t.Fatalf("AddTrigger path: %v", err)
	}

	mustExecDecodeTest(t, store, `UPDATE knowledge_items SET priority = ? WHERE id = ?`, "urgent", item.ID)

	_, err = store.GetItem(ctx, item.ID)
	requireDecodeError(t, err, "priority")
	_, _, err = store.GetItemByName(ctx, item.Name)
	requireDecodeError(t, err, "priority")
	_, err = store.ListItems(ctx, KindPack)
	requireDecodeError(t, err, "priority")
	_, err = store.ListAllItems(ctx)
	requireDecodeError(t, err, "priority")
	_, err = store.MatchByKeyword(ctx, []string{"corrupt-priority"})
	requireDecodeError(t, err, "priority")
	_, err = store.MatchByPath(ctx, "src/app.go")
	requireDecodeError(t, err, "priority")
}

func TestNormalizeItemPriorityProperty(t *testing.T) {
	rejectsGeneratedUnknownPriorities := func(raw string) bool {
		priority, err := normalizeItemPriority("unknown:" + raw)
		return err != nil && priority == "" && strings.Contains(err.Error(), "priority")
	}

	if err := quick.Check(rejectsGeneratedUnknownPriorities, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("unknown priority property failed: %v", err)
	}

	for raw, want := range map[string]string{
		"":           priorityMedium,
		" Critical ": priorityCritical,
		"HIGH":       priorityHigh,
		"medium":     priorityMedium,
		" low ":      priorityLow,
	} {
		got, err := normalizeItemPriority(raw)
		if err != nil {
			t.Fatalf("normalize priority %q: %v", raw, err)
		}
		if got != want {
			t.Fatalf("normalize priority %q = %q, want %q", raw, got, want)
		}
	}
}

func openDecodeTestStore(t *testing.T) Store {
	t.Helper()
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

func mustExecDecodeTest(t *testing.T, store Store, query string, args ...any) {
	t.Helper()
	sqlStore := store.(*sqlStore)
	if _, err := sqlStore.db.ExecContext(context.Background(), query, args...); err != nil {
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
