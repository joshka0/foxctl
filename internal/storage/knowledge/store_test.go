package knowledge_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/storage/knowledge"
)

func TestStore_ItemCRUD(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Create item
	item := knowledge.Item{
		Name:        "test-pack",
		Kind:        knowledge.KindPack,
		Description: "A test knowledge pack",
		SourcePath:  "docs/knowledge/test-pack",
	}

	created, err := store.UpsertItem(ctx, item)
	if err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}
	if created.ID == "" {
		t.Error("expected ID to be set")
	}
	if created.Name != "test-pack" {
		t.Errorf("expected name 'test-pack', got %q", created.Name)
	}

	// Get by ID
	got, err := store.GetItem(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got.Name != "test-pack" {
		t.Errorf("expected name 'test-pack', got %q", got.Name)
	}

	// Get by name
	gotByName, found, err := store.GetItemByName(ctx, "test-pack")
	if err != nil {
		t.Fatalf("GetItemByName: %v", err)
	}
	if !found {
		t.Error("expected to find item by name")
	}
	if gotByName.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, gotByName.ID)
	}

	// Update (upsert with same name)
	item.Description = "Updated description"
	updated, err := store.UpsertItem(ctx, item)
	if err != nil {
		t.Fatalf("UpsertItem (update): %v", err)
	}
	if updated.Description != "Updated description" {
		t.Errorf("expected updated description, got %q", updated.Description)
	}

	// List all
	all, err := store.ListAllItems(ctx)
	if err != nil {
		t.Fatalf("ListAllItems: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 item, got %d", len(all))
	}

	// List by kind
	packs, err := store.ListItems(ctx, knowledge.KindPack)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(packs) != 1 {
		t.Errorf("expected 1 pack, got %d", len(packs))
	}

	agents, err := store.ListItems(ctx, knowledge.KindAgent)
	if err != nil {
		t.Fatalf("ListItems (agents): %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}

	// Delete
	if err := store.DeleteItem(ctx, created.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	_, found, err = store.GetItemByName(ctx, "test-pack")
	if err != nil {
		t.Fatalf("GetItemByName after delete: %v", err)
	}
	if found {
		t.Error("expected item to be deleted")
	}
}

func TestStore_UpsertItemAcceptsKnownKinds(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	kinds := []knowledge.ItemKind{
		knowledge.KindPack,
		knowledge.KindAgent,
		knowledge.KindCommand,
	}

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			created, err := store.UpsertItem(ctx, knowledge.Item{
				Name:       "item-" + string(kind),
				Kind:       kind,
				SourcePath: "docs/knowledge/" + string(kind),
			})
			if err != nil {
				t.Fatalf("UpsertItem kind %q: %v", kind, err)
			}
			if created.Kind != kind {
				t.Fatalf("created kind = %q, want %q", created.Kind, kind)
			}
		})
	}
}

func TestStore_UpsertItemRejectsInvalidKind(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertItem(ctx, knowledge.Item{
		Name:       "invalid-kind",
		Kind:       knowledge.ItemKind("unknown-kind"),
		SourcePath: "docs/knowledge/invalid-kind",
	})
	if err == nil {
		t.Fatalf("expected invalid item kind to be rejected")
	}

	items, err := store.ListAllItems(ctx)
	if err != nil {
		t.Fatalf("ListAllItems: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("invalid item was persisted: %+v", items)
	}
}

func TestStore_UpsertItemDefaultsAndNormalizesPriority(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	defaulted, err := store.UpsertItem(ctx, knowledge.Item{
		Name:       "default-priority",
		Kind:       knowledge.KindPack,
		SourcePath: "docs/knowledge/default-priority",
	})
	if err != nil {
		t.Fatalf("UpsertItem default priority: %v", err)
	}
	if defaulted.Priority != "medium" {
		t.Fatalf("default priority=%q want medium", defaulted.Priority)
	}

	normalized, err := store.UpsertItem(ctx, knowledge.Item{
		Name:       "normalized-priority",
		Kind:       knowledge.KindPack,
		SourcePath: "docs/knowledge/normalized-priority",
		Priority:   " HIGH ",
	})
	if err != nil {
		t.Fatalf("UpsertItem normalized priority: %v", err)
	}
	if normalized.Priority != "high" {
		t.Fatalf("normalized priority=%q want high", normalized.Priority)
	}
}

func TestStore_UpsertItemRejectsInvalidPriority(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertItem(ctx, knowledge.Item{
		Name:       "invalid-priority",
		Kind:       knowledge.KindPack,
		SourcePath: "docs/knowledge/invalid-priority",
		Priority:   "urgent",
	})
	if err == nil {
		t.Fatalf("expected invalid item priority to be rejected")
	}

	items, err := store.ListAllItems(ctx)
	if err != nil {
		t.Fatalf("ListAllItems: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("invalid priority item was persisted: %+v", items)
	}
}

func TestStore_ListItemsRejectsInvalidKind(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if _, err := store.ListItems(ctx, knowledge.ItemKind("unknown-kind")); err == nil {
		t.Fatalf("expected invalid list item kind to be rejected")
	}
}

func TestStore_Triggers(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Create item first
	item, err := store.UpsertItem(ctx, knowledge.Item{
		Name:       "frontend-guidelines",
		Kind:       knowledge.KindPack,
		SourcePath: "docs/knowledge/frontend-guidelines",
	})
	if err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}

	// Add triggers
	triggers := []knowledge.Trigger{
		{ItemID: item.ID, TriggerKind: knowledge.TriggerKeyword, Pattern: "react"},
		{ItemID: item.ID, TriggerKind: knowledge.TriggerKeyword, Pattern: "component"},
		{ItemID: item.ID, TriggerKind: knowledge.TriggerPath, Pattern: "src/components/**"},
	}

	for _, trig := range triggers {
		if _, err := store.AddTrigger(ctx, trig); err != nil {
			t.Fatalf("AddTrigger: %v", err)
		}
	}

	// List triggers
	got, err := store.ListTriggers(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListTriggers: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 triggers, got %d", len(got))
	}

	// Match by keyword
	matches, err := store.MatchByKeyword(ctx, []string{"react", "vue"})
	if err != nil {
		t.Fatalf("MatchByKeyword: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}
	if len(matches) > 0 && matches[0].Name != "frontend-guidelines" {
		t.Errorf("expected 'frontend-guidelines', got %q", matches[0].Name)
	}

	// Delete triggers
	if err := store.DeleteTriggersForItem(ctx, item.ID); err != nil {
		t.Fatalf("DeleteTriggersForItem: %v", err)
	}

	got, err = store.ListTriggers(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListTriggers after delete: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 triggers after delete, got %d", len(got))
	}
}

func TestStore_AddTriggerAcceptsKnownKinds(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	item, err := store.UpsertItem(ctx, knowledge.Item{
		Name:       "trigger-kind-item",
		Kind:       knowledge.KindPack,
		SourcePath: "docs/knowledge/trigger-kind-item",
	})
	if err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}

	kinds := []knowledge.TriggerKind{
		knowledge.TriggerKeyword,
		knowledge.TriggerIntent,
		knowledge.TriggerPath,
		knowledge.TriggerContent,
	}

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			created, err := store.AddTrigger(ctx, knowledge.Trigger{
				ItemID:      item.ID,
				TriggerKind: kind,
				Pattern:     "pattern-" + string(kind),
			})
			if err != nil {
				t.Fatalf("AddTrigger kind %q: %v", kind, err)
			}
			if created.TriggerKind != kind {
				t.Fatalf("created trigger kind = %q, want %q", created.TriggerKind, kind)
			}
		})
	}
}

func TestStore_AddTriggerRejectsInvalidKind(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	item, err := store.UpsertItem(ctx, knowledge.Item{
		Name:       "invalid-trigger-kind-item",
		Kind:       knowledge.KindPack,
		SourcePath: "docs/knowledge/invalid-trigger-kind-item",
	})
	if err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}

	_, err = store.AddTrigger(ctx, knowledge.Trigger{
		ItemID:      item.ID,
		TriggerKind: knowledge.TriggerKind("unknown-trigger-kind"),
		Pattern:     "should-not-persist",
	})
	if err == nil {
		t.Fatalf("expected invalid trigger kind to be rejected")
	}

	triggers, err := store.ListTriggers(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListTriggers: %v", err)
	}
	if len(triggers) != 0 {
		t.Fatalf("invalid trigger was persisted: %+v", triggers)
	}
}

func TestStore_MatchesUseSemanticPriorityOrder(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	for _, item := range []struct {
		name     string
		priority string
	}{
		{name: "low-priority", priority: "low"},
		{name: "critical-priority", priority: "critical"},
		{name: "medium-priority", priority: "medium"},
		{name: "high-priority", priority: "high"},
	} {
		created, err := store.UpsertItem(ctx, knowledge.Item{
			Name:       item.name,
			Kind:       knowledge.KindPack,
			SourcePath: "docs/knowledge/" + item.name,
			Priority:   item.priority,
		})
		if err != nil {
			t.Fatalf("UpsertItem %s: %v", item.name, err)
		}
		if _, err := store.AddTrigger(ctx, knowledge.Trigger{
			ItemID:      created.ID,
			TriggerKind: knowledge.TriggerKeyword,
			Pattern:     "shared-keyword",
		}); err != nil {
			t.Fatalf("AddTrigger keyword %s: %v", item.name, err)
		}
		if _, err := store.AddTrigger(ctx, knowledge.Trigger{
			ItemID:      created.ID,
			TriggerKind: knowledge.TriggerPath,
			Pattern:     "src/**",
		}); err != nil {
			t.Fatalf("AddTrigger path %s: %v", item.name, err)
		}
	}

	want := []string{"critical-priority", "high-priority", "medium-priority", "low-priority"}
	keywordMatches, err := store.MatchByKeyword(ctx, []string{"shared-keyword"})
	if err != nil {
		t.Fatalf("MatchByKeyword: %v", err)
	}
	if got := itemNames(keywordMatches); !equalStrings(got, want) {
		t.Fatalf("keyword match order=%v want %v", got, want)
	}

	pathMatches, err := store.MatchByPath(ctx, "src/app/main.go")
	if err != nil {
		t.Fatalf("MatchByPath: %v", err)
	}
	if got := itemNames(pathMatches); !equalStrings(got, want) {
		t.Fatalf("path match order=%v want %v", got, want)
	}
}

func TestStore_Documents(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Create item first
	item, err := store.UpsertItem(ctx, knowledge.Item{
		Name:       "backend-guidelines",
		Kind:       knowledge.KindPack,
		SourcePath: "docs/knowledge/backend-guidelines",
	})
	if err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}

	// Add documents
	doc := knowledge.Document{
		ItemID:     item.ID,
		Title:      "SKILL",
		SourcePath: "docs/knowledge/backend-guidelines/SKILL.md",
		Body:       "# Backend Guidelines\n\nThis is the main doc.",
	}

	created, err := store.UpsertDocument(ctx, doc)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if created.ID == "" {
		t.Error("expected document ID to be set")
	}

	// List documents
	docs, err := store.ListDocuments(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) != 1 {
		t.Errorf("expected 1 document, got %d", len(docs))
	}
	if len(docs) > 0 && docs[0].Title != "SKILL" {
		t.Errorf("expected title 'SKILL', got %q", docs[0].Title)
	}

	// Delete documents
	if err := store.DeleteDocumentsForItem(ctx, item.ID); err != nil {
		t.Fatalf("DeleteDocumentsForItem: %v", err)
	}

	docs, err = store.ListDocuments(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListDocuments after delete: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("expected 0 documents after delete, got %d", len(docs))
	}
}

func TestSync_KnowledgePacks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Use the actual foxctl directory as workspace
	workspaceRoot := filepath.Join(tmpDir, "workspace")

	// Create a minimal knowledge pack structure
	setupTestKnowledgePack(t, workspaceRoot)

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	opts := knowledge.DefaultSyncOptions(workspaceRoot)
	result, err := knowledge.Sync(ctx, store, opts)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if result.PacksAdded != 1 {
		t.Errorf("expected 1 pack added, got %d", result.PacksAdded)
	}

	// Verify item was created
	items, err := store.ListItems(ctx, knowledge.KindPack)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 pack, got %d", len(items))
	}
	if len(items) > 0 && items[0].Name != "test-pack" {
		t.Errorf("expected name 'test-pack', got %q", items[0].Name)
	}
}

func setupTestKnowledgePack(t *testing.T, root string) {
	t.Helper()

	// Create docs/knowledge/test-pack/SKILL.md
	packDir := filepath.Join(root, "docs", "knowledge", "test-pack")
	if err := createDirAndFile(packDir, "SKILL.md", `---
description: A test knowledge pack for unit tests
---

# Test Pack

This is a test knowledge pack.
`); err != nil {
		t.Fatalf("setup test pack: %v", err)
	}
}

func createDirAndFile(dir, filename, content string) error {
	return createFile(filepath.Join(dir, filename), content)
}

func createFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func itemNames(items []knowledge.Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
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
