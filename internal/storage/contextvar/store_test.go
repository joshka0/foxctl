package contextvar

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/quick"
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

func TestStore_KeyPrefixTreatsSQLWildcardsAsLiteralCharacters(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	tests := []struct {
		name   string
		prefix string
		hit    string
		miss   string
	}{
		{name: "percent", prefix: "literal%", hit: "literal%/hit", miss: "literalX/miss"},
		{name: "underscore", prefix: "literal_", hit: "literal_/hit", miss: "literalX/miss"},
		{name: "backslash", prefix: `literal\`, hit: `literal\hit`, miss: "literalXmiss"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			convID := "test-conv-prefix-" + tt.name
			for _, key := range []string{tt.hit, tt.miss} {
				if _, err := store.Put(ctx, PutParams{
					ConversationID: convID,
					Scope:          ScopeConversation,
					Key:            key,
					Value:          key,
					Source:         "test",
				}); err != nil {
					t.Fatalf("Put %q failed: %v", key, err)
				}
			}

			result, err := store.Query(ctx, QueryParams{
				ConversationID: convID,
				KeyPrefix:      tt.prefix,
			})
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}
			if len(result.Variables) != 1 || result.Variables[0].Key != tt.hit {
				t.Fatalf("KeyPrefix %q matched %v, want only %q", tt.prefix, variableKeys(result.Variables), tt.hit)
			}
		})
	}
}

func TestStorePropertyKeyPrefixEscapesSQLWildcards(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	caseID := 0
	wildcards := []byte{'%', '_', '\\'}

	property := func(rawStem, rawSuffix string, wildcardSeed uint8) bool {
		stem := "key" + safeContextKeyToken(rawStem)
		suffix := safeContextKeyToken(rawSuffix)
		wildcard := string([]byte{wildcards[int(wildcardSeed)%len(wildcards)]})
		convID := fmt.Sprintf("test-conv-prefix-property-%d", caseID)
		caseID++

		prefix := stem + wildcard
		hit := prefix + suffix
		miss := stem + "x" + suffix
		for _, key := range []string{hit, miss} {
			if _, err := store.Put(ctx, PutParams{
				ConversationID: convID,
				Scope:          ScopeConversation,
				Key:            key,
				Value:          key,
				Source:         "test",
			}); err != nil {
				t.Logf("Put %q failed: %v", key, err)
				return false
			}
		}

		result, err := store.Query(ctx, QueryParams{
			ConversationID: convID,
			KeyPrefix:      prefix,
		})
		if err != nil {
			t.Logf("Query prefix %q failed: %v", prefix, err)
			return false
		}
		if len(result.Variables) != 1 || result.Variables[0].Key != hit {
			t.Logf("KeyPrefix %q matched %v, want only %q", prefix, variableKeys(result.Variables), hit)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("KeyPrefix wildcard escaping property failed: %v", err)
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

func TestStore_PutRejectsNegativeTTL(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-negative-ttl"
	if _, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "negative_ttl",
		Value:          "should not persist",
		Source:         "test",
		TTL:            -time.Second,
	}); err == nil {
		t.Fatal("Put accepted negative TTL")
	}

	if _, err := store.GetByKey(ctx, convID, ScopeConversation, "negative_ttl"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByKey after rejected negative TTL error = %v, want ErrNotFound", err)
	}
}

func TestStore_PutZeroTTLDoesNotExpire(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-zero-ttl"
	v, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "zero_ttl",
		Value:          "persistent",
		Source:         "test",
		TTL:            0,
	})
	if err != nil {
		t.Fatalf("Put zero TTL failed: %v", err)
	}
	if v.ExpiresAt != nil {
		t.Fatalf("ExpiresAt=%v want nil for zero TTL", v.ExpiresAt)
	}
}

func TestStore_RejectsCorruptStoredValueJSON(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	convID := "test-conv-corrupt-json"
	v, err := store.Put(ctx, PutParams{
		ConversationID: convID,
		Scope:          ScopeConversation,
		Key:            "corrupt_value",
		Value:          map[string]string{"ok": "true"},
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE context_variables SET value_json = $1 WHERE id = $2
	`, "{", v.ID); err != nil {
		t.Fatalf("corrupt value_json: %v", err)
	}

	_, err = store.Get(ctx, v.ID)
	requireErrorContains(t, "Get", err, "value_json")
	_, err = store.GetByKey(ctx, convID, ScopeConversation, "corrupt_value")
	requireErrorContains(t, "GetByKey", err, "value_json")
	_, err = store.Query(ctx, QueryParams{ConversationID: convID, Key: "corrupt_value"})
	requireErrorContains(t, "Query", err, "value_json")
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

func variableKeys(vars []Variable) []string {
	keys := make([]string, len(vars))
	for i, v := range vars {
		keys[i] = v.Key
	}
	return keys
}

func safeContextKeyToken(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		}
		if b.Len() >= 16 {
			break
		}
	}
	if b.Len() == 0 {
		return "x"
	}
	return b.String()
}

func requireErrorContains(t *testing.T, operation string, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s error = nil, want error containing %q", operation, want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want it to contain %q", operation, err, want)
	}
}
