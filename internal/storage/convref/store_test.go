package convref

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close() //nolint:errcheck
	})
	return db
}

func TestUpsert_CreatesThenUpdates(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewSQLiteStore(db, nil)
	if err := store.CreateTable(ctx); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	key := "teams:t1:c1"

	ref1 := Ref{
		ConversationKey:   key,
		Platform:          "teams",
		TenantID:          "t1",
		RawConversationID: "c1",
		ServiceURL:        "https://service-1.example",
		LastActivityID:    "a1",
		BotID:             "bot1",
		CreatedAtMS:       100,
		UpdatedAtMS:       100,
	}
	if err := store.Upsert(ctx, ref1); err != nil {
		t.Fatalf("Upsert(1): %v", err)
	}

	got1, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get(1): %v", err)
	}
	if got1 == nil {
		t.Fatalf("expected ref, got nil")
	}
	if got1.ServiceURL != ref1.ServiceURL || got1.LastActivityID != ref1.LastActivityID {
		t.Fatalf("unexpected ref after first upsert: %+v", *got1)
	}
	if got1.CreatedAtMS != 100 || got1.UpdatedAtMS != 100 {
		t.Fatalf("unexpected timestamps after first upsert: %+v", *got1)
	}

	ref2 := ref1
	ref2.ServiceURL = "https://service-2.example"
	ref2.LastActivityID = "a2"
	ref2.UpdatedAtMS = 200
	ref2.CreatedAtMS = 999 // should not overwrite existing created_at_ms on conflict update

	if err := store.Upsert(ctx, ref2); err != nil {
		t.Fatalf("Upsert(2): %v", err)
	}

	got2, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get(2): %v", err)
	}
	if got2 == nil {
		t.Fatalf("expected ref, got nil")
	}
	if got2.ServiceURL != ref2.ServiceURL {
		t.Fatalf("expected service_url %q, got %q", ref2.ServiceURL, got2.ServiceURL)
	}
	if got2.LastActivityID != ref2.LastActivityID {
		t.Fatalf("expected last_activity_id %q, got %q", ref2.LastActivityID, got2.LastActivityID)
	}
	if got2.CreatedAtMS != 100 {
		t.Fatalf("expected CreatedAtMS preserved (100), got %d", got2.CreatedAtMS)
	}
	if got2.UpdatedAtMS != 200 {
		t.Fatalf("expected UpdatedAtMS updated (200), got %d", got2.UpdatedAtMS)
	}
}

func TestGet_UnknownReturnsNil(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewSQLiteStore(db, nil)
	if err := store.CreateTable(ctx); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	got, err := store.Get(ctx, "missing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", *got)
	}
}

func TestDeleteStale_RemovesOnlyOldRefs(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewSQLiteStore(db, nil)
	if err := store.CreateTable(ctx); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	oldKey := "teams:t1:old"
	newKey := "teams:t1:new"

	if err := store.Upsert(ctx, Ref{
		ConversationKey:   oldKey,
		Platform:          "teams",
		TenantID:          "t1",
		RawConversationID: "old",
		CreatedAtMS:       1,
		UpdatedAtMS:       100,
	}); err != nil {
		t.Fatalf("Upsert(old): %v", err)
	}
	if err := store.Upsert(ctx, Ref{
		ConversationKey:   newKey,
		Platform:          "teams",
		TenantID:          "t1",
		RawConversationID: "new",
		CreatedAtMS:       2,
		UpdatedAtMS:       200,
	}); err != nil {
		t.Fatalf("Upsert(new): %v", err)
	}

	deleted, err := store.DeleteStale(ctx, 150)
	if err != nil {
		t.Fatalf("DeleteStale: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 row deleted, got %d", deleted)
	}

	gotOld, err := store.Get(ctx, oldKey)
	if err != nil {
		t.Fatalf("Get(old): %v", err)
	}
	if gotOld != nil {
		t.Fatalf("expected old ref deleted, got %+v", *gotOld)
	}

	gotNew, err := store.Get(ctx, newKey)
	if err != nil {
		t.Fatalf("Get(new): %v", err)
	}
	if gotNew == nil {
		t.Fatalf("expected new ref to remain, got nil")
	}
}

func TestDelete_RemovesSpecificRef(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewSQLiteStore(db, nil)
	if err := store.CreateTable(ctx); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	keyA := "teams:t1:a"
	keyB := "teams:t1:b"

	if err := store.Upsert(ctx, Ref{
		ConversationKey:   keyA,
		Platform:          "teams",
		TenantID:          "t1",
		RawConversationID: "a",
		CreatedAtMS:       1,
		UpdatedAtMS:       1,
	}); err != nil {
		t.Fatalf("Upsert(a): %v", err)
	}
	if err := store.Upsert(ctx, Ref{
		ConversationKey:   keyB,
		Platform:          "teams",
		TenantID:          "t1",
		RawConversationID: "b",
		CreatedAtMS:       1,
		UpdatedAtMS:       2,
	}); err != nil {
		t.Fatalf("Upsert(b): %v", err)
	}

	if err := store.Delete(ctx, keyA); err != nil {
		t.Fatalf("Delete(a): %v", err)
	}

	gotA, err := store.Get(ctx, keyA)
	if err != nil {
		t.Fatalf("Get(a): %v", err)
	}
	if gotA != nil {
		t.Fatalf("expected a deleted, got %+v", *gotA)
	}

	gotB, err := store.Get(ctx, keyB)
	if err != nil {
		t.Fatalf("Get(b): %v", err)
	}
	if gotB == nil {
		t.Fatalf("expected b to remain, got nil")
	}
}
