package contextvar

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStore_PutAndGet(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-1"

	// Put a variable
	v, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "user_name",
		Value:          "Alice",
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if v.Key != "user_name" {
		t.Errorf("Expected key 'user_name', got %q", v.Key)
	}
	if v.Scope != ScopeConversation {
		t.Errorf("Expected scope 'conversation', got %q", v.Scope)
	}

	// Get by ID
	got, err := store.Get(ctx, v.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.Key != "user_name" {
		t.Errorf("Get: expected key 'user_name', got %q", got.Key)
	}

	// Get by key
	got2, err := store.GetByKey(ctx, convID, ScopeConversation, "user_name")
	if err != nil {
		t.Fatalf("GetByKey failed: %v", err)
	}

	if got2.ID != v.ID {
		t.Errorf("GetByKey: expected ID %q, got %q", v.ID, got2.ID)
	}
}

func TestStore_PutUpsert(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-upsert"

	// Initial put
	_, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "counter",
		Value:          1,
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("Initial put failed: %v", err)
	}

	// Upsert with new value
	v2, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "counter",
		Value:          2,
		Source:         "test",
		Upsert:         true,
	})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Verify value was updated
	got, err := store.GetByKey(ctx, convID, ScopeConversation, "counter")
	if err != nil {
		t.Fatalf("Get after upsert failed: %v", err)
	}

	if string(got.ValueJSON) != "2" {
		t.Errorf("Expected value '2', got %q", string(got.ValueJSON))
	}

	if got.ID != v2.ID {
		t.Errorf("IDs should match after upsert")
	}
}

func TestStore_PutConflict(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-conflict"

	// First put
	_, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "unique_key",
		Value:          "first",
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("First put failed: %v", err)
	}

	// Second put without upsert should conflict
	_, err = store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "unique_key",
		Value:          "second",
		Source:         "test",
		Upsert:         false,
	})
	if err == nil {
		t.Fatal("Expected conflict error, got nil")
	}
	if !errors.Is(err, ErrConflict) {
		t.Errorf("Expected ErrConflict, got: %v", err)
	}
}

func TestStore_Query(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-query"

	// Create several variables
	keys := []string{"pref/theme", "pref/language", "pref/timezone", "user_name"}
	for _, key := range keys {
		_, err := store.Put(ctx, PutParams{
			ConversationID: convID,
			Scope:          ScopeConversation,
			Key:            key,
			Value:          "value_" + key,
			Source:         "test",
		})
		if err != nil {
			t.Fatalf("Put %q failed: %v", key, err)
		}
	}

	// Query with pattern
	result, err := store.Query(ctx, QueryParams{
		ConversationID: convID,
		KeyPattern:     "pref/*",
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Variables) != 3 {
		t.Errorf("Expected 3 results for 'pref/*', got %d", len(result.Variables))
	}

	// Query with exact key
	result, err = store.Query(ctx, QueryParams{
		ConversationID: convID,
		Key:            "user_name",
	})
	if err != nil {
		t.Fatalf("Query exact key failed: %v", err)
	}

	if len(result.Variables) != 1 {
		t.Errorf("Expected 1 result for exact key, got %d", len(result.Variables))
	}
}

func TestStore_Scopes(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-scopes"

	// Create variables in different scopes
	scopes := []Scope{ScopeGlobal, ScopeConversation, ScopeTurn}
	for _, scope := range scopes {
		_, err := store.Put(ctx, PutParams{
			ConversationID: convID,
			Scope:          scope,
			Key:            "test_key",
			Value:          string(scope),
			Source:         "test",
		})
		if err != nil {
			t.Fatalf("Put scope %q failed: %v", scope, err)
		}
	}

	// Query all scopes
	result, err := store.Query(ctx, QueryParams{
		ConversationID: convID,
		Key:            "test_key",
	})
	if err != nil {
		t.Fatalf("Query all scopes failed: %v", err)
	}

	if len(result.Variables) != 3 {
		t.Errorf("Expected 3 results (one per scope), got %d", len(result.Variables))
	}

	// Query specific scope
	result, err = store.Query(ctx, QueryParams{
		ConversationID: convID,
		Key:            "test_key",
		Scope:          ScopeGlobal,
	})
	if err != nil {
		t.Fatalf("Query global scope failed: %v", err)
	}

	if len(result.Variables) != 1 {
		t.Errorf("Expected 1 result for global scope, got %d", len(result.Variables))
	}
}

func TestStore_Delete(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-delete"

	// Create a variable
	v, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "to_delete",
		Value:          "temp",
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Delete by ID
	err = store.Delete(ctx, v.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deleted
	_, err = store.Get(ctx, v.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Expected ErrNotFound after delete, got: %v", err)
	}
}

func TestStore_DeleteByKey(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-delete-key"

	// Create a variable
	_, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "to_delete_key",
		Value:          "temp",
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Delete by key
	err = store.DeleteByKey(ctx, convID, ScopeConversation, "to_delete_key")
	if err != nil {
		t.Fatalf("DeleteByKey failed: %v", err)
	}

	// Verify deleted
	_, err = store.GetByKey(ctx, convID, ScopeConversation, "to_delete_key")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Expected ErrNotFound after delete, got: %v", err)
	}
}

func TestStore_TTLExpiration(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-ttl"

	// Create a variable with very short TTL
	_, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "ephemeral",
		Value:          "will expire",
		Source:         "test",
		TTL:            1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Query should not return expired by default
	result, err := store.Query(ctx, QueryParams{
		ConversationID: convID,
		Key:            "ephemeral",
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Variables) != 0 {
		t.Errorf("Expected 0 results (expired), got %d", len(result.Variables))
	}

	// Query with IncludeExpired should return it
	result, err = store.Query(ctx, QueryParams{
		ConversationID: convID,
		Key:            "ephemeral",
		IncludeExpired: true,
	})
	if err != nil {
		t.Fatalf("Query with IncludeExpired failed: %v", err)
	}

	if len(result.Variables) != 1 {
		t.Errorf("Expected 1 result (with IncludeExpired), got %d", len(result.Variables))
	}
}

func TestStore_ListKeys(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-list"

	// Create several variables
	keys := []string{"alpha", "beta", "gamma"}
	for _, key := range keys {
		_, err := store.Put(ctx, PutParams{
			ConversationID: convID,
			Scope:          ScopeConversation,
			Key:            key,
			Value:          key,
			Source:         "test",
		})
		if err != nil {
			t.Fatalf("Put %q failed: %v", key, err)
		}
	}

	// List keys
	result, err := store.ListKeys(ctx, convID, "")
	if err != nil {
		t.Fatalf("ListKeys failed: %v", err)
	}

	if result.TotalCount != 3 {
		t.Errorf("Expected 3 keys, got %d", result.TotalCount)
	}

	// Verify keys are sorted
	if len(result.Keys) >= 3 {
		if result.Keys[0].Key != "alpha" || result.Keys[1].Key != "beta" || result.Keys[2].Key != "gamma" {
			t.Errorf("Keys not sorted alphabetically: %v", result.Keys)
		}
	}
}

func TestStore_Stats(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create some variables
	if _, err := store.Put(ctx, PutParams{
		ConversationID: "",
		Scope:          ScopeGlobal,
		Key:            "global_setting",
		Value:          "test",
		Source:         "test",
	}); err != nil {
		t.Fatalf("Put global_setting failed: %v", err)
	}

	if _, err := store.Put(ctx, PutParams{
		ConversationID: "conv-1",
		Scope:          ScopeConversation,
		Key:            "conv_var",
		Value:          "test",
		Source:         "test",
	}); err != nil {
		t.Fatalf("Put conv_var failed: %v", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.TotalVariables < 2 {
		t.Errorf("Expected at least 2 total variables, got %d", stats.TotalVariables)
	}

	if stats.GlobalVariables < 1 {
		t.Errorf("Expected at least 1 global variable, got %d", stats.GlobalVariables)
	}
}

func TestStore_IncrementAccess(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-access"

	// Create a variable
	v, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "access_test",
		Value:          "test",
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Initial access count should be 0
	got, err := store.Get(ctx, v.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.AccessCount != 0 {
		t.Errorf("Expected initial access count 0, got %d", got.AccessCount)
	}

	// Increment access
	err = store.IncrementAccess(ctx, v.ID)
	if err != nil {
		t.Fatalf("IncrementAccess failed: %v", err)
	}

	// Verify incremented
	got, err = store.Get(ctx, v.ID)
	if err != nil {
		t.Fatalf("Get after increment failed: %v", err)
	}
	if got.AccessCount != 1 {
		t.Errorf("Expected access count 1, got %d", got.AccessCount)
	}
}

func setupTestStore(t *testing.T) Store {
	t.Helper()

	dir := t.TempDir()
	store, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Failed to open test store: %v", err)
	}

	return store
}
