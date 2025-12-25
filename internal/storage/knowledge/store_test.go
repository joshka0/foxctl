package knowledge_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/knowledge"
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

	// Use the actual agentctl directory as workspace
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
