//go:build sqlite_mattn
// +build sqlite_mattn

package actor

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestRegistryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}

func TestNewRegistryStore(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}
	if store == nil {
		t.Fatal("store is nil")
	}

	// Verify table was created
	var tableName string
	err = db.QueryRowContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type='table' AND name='actor_registry'
	`).Scan(&tableName)
	if err != nil {
		t.Fatalf("table not created: %v", err)
	}
	if tableName != "actor_registry" {
		t.Errorf("table name = %q, want actor_registry", tableName)
	}
}

func TestRegistryStore_RegisterActor(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	configJSON, err := MarshalConfig(Config{
		ID:        "actor-1",
		Namespace: "test-ns",
		Role:      "coder",
	})
	if err != nil {
		t.Fatalf("MarshalConfig() error = %v", err)
	}

	rec := ActorRecord{
		Namespace:  "test-ns",
		Role:       "coder",
		ConfigJSON: configJSON,
		Status:     ActorStatusRegistered,
	}

	err = store.RegisterActor(ctx, rec)
	if err != nil {
		t.Fatalf("RegisterActor() error = %v", err)
	}

	// Verify actor was stored
	got, err := store.GetActor(ctx, "test-ns")
	if err != nil {
		t.Fatalf("GetActor() error = %v", err)
	}
	if got.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want test-ns", got.Namespace)
	}
	if got.Role != "coder" {
		t.Errorf("Role = %q, want coder", got.Role)
	}
	if got.Status != ActorStatusRegistered {
		t.Errorf("Status = %q, want registered", got.Status)
	}
	if got.Config.ID != "actor-1" {
		t.Errorf("Config.ID = %q, want actor-1", got.Config.ID)
	}
}

func TestRegistryStore_RegisterActor_Upsert(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	// First registration
	rec := ActorRecord{
		Namespace:  "test-ns",
		Role:       "coder",
		ConfigJSON: `{"id":"v1"}`,
		Status:     ActorStatusRegistered,
	}
	if err := store.RegisterActor(ctx, rec); err != nil {
		t.Fatalf("first RegisterActor() error = %v", err)
	}

	// Second registration (update)
	rec.ConfigJSON = `{"id":"v2"}`
	rec.Status = ActorStatusRunning
	if err := store.RegisterActor(ctx, rec); err != nil {
		t.Fatalf("second RegisterActor() error = %v", err)
	}

	// Verify update
	got, err := store.GetActor(ctx, "test-ns")
	if err != nil {
		t.Fatalf("GetActor() error = %v", err)
	}
	if got.ConfigJSON != `{"id":"v2"}` {
		t.Errorf("ConfigJSON = %q, want {\"id\":\"v2\"}", got.ConfigJSON)
	}
	if got.Status != ActorStatusRunning {
		t.Errorf("Status = %q, want running", got.Status)
	}
}

func TestRegistryStore_UnregisterActor(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	// Register first
	rec := ActorRecord{
		Namespace:  "test-ns",
		Role:       "coder",
		ConfigJSON: `{}`,
		Status:     ActorStatusRegistered,
	}
	if err := store.RegisterActor(ctx, rec); err != nil {
		t.Fatalf("RegisterActor() error = %v", err)
	}

	// Unregister
	if err := store.UnregisterActor(ctx, "test-ns"); err != nil {
		t.Fatalf("UnregisterActor() error = %v", err)
	}

	// Verify removed
	_, err = store.GetActor(ctx, "test-ns")
	if !errors.Is(err, ErrActorNotFound) {
		t.Errorf("GetActor() error = %v, want ErrActorNotFound", err)
	}
}

func TestRegistryStore_UnregisterActor_NotFound(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	err = store.UnregisterActor(ctx, "nonexistent")
	if !errors.Is(err, ErrActorNotFound) {
		t.Errorf("UnregisterActor() error = %v, want ErrActorNotFound", err)
	}
}

func TestRegistryStore_GetActor_NotFound(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	_, err = store.GetActor(ctx, "nonexistent")
	if !errors.Is(err, ErrActorNotFound) {
		t.Errorf("GetActor() error = %v, want ErrActorNotFound", err)
	}
}

func TestRegistryStore_ListActors(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	// Register multiple actors
	actors := []ActorRecord{
		{Namespace: "ns-1", Role: "coder", ConfigJSON: `{"id":"1"}`, Status: ActorStatusRegistered},
		{Namespace: "ns-2", Role: "planner", ConfigJSON: `{"id":"2"}`, Status: ActorStatusRunning},
		{Namespace: "ns-3", Role: "reviewer", ConfigJSON: `{"id":"3"}`, Status: ActorStatusStopped},
	}

	for _, a := range actors {
		if err := store.RegisterActor(ctx, a); err != nil {
			t.Fatalf("RegisterActor(%s) error = %v", a.Namespace, err)
		}
		// Small delay to ensure different timestamps
		time.Sleep(time.Millisecond)
	}

	// List all
	got, err := store.ListActors(ctx)
	if err != nil {
		t.Fatalf("ListActors() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListActors() returned %d actors, want 3", len(got))
	}

	// Verify order (by created_at ASC)
	if got[0].Namespace != "ns-1" {
		t.Errorf("got[0].Namespace = %q, want ns-1", got[0].Namespace)
	}
	if got[1].Namespace != "ns-2" {
		t.Errorf("got[1].Namespace = %q, want ns-2", got[1].Namespace)
	}
	if got[2].Namespace != "ns-3" {
		t.Errorf("got[2].Namespace = %q, want ns-3", got[2].Namespace)
	}
}

func TestRegistryStore_ListActors_Empty(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	got, err := store.ListActors(ctx)
	if err != nil {
		t.Fatalf("ListActors() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListActors() returned %d actors, want 0", len(got))
	}
}

func TestRegistryStore_UpdateStatus(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	// Register first
	rec := ActorRecord{
		Namespace:  "test-ns",
		Role:       "coder",
		ConfigJSON: `{}`,
		Status:     ActorStatusRegistered,
	}
	if err := store.RegisterActor(ctx, rec); err != nil {
		t.Fatalf("RegisterActor() error = %v", err)
	}

	// Update status
	if err := store.UpdateStatus(ctx, "test-ns", ActorStatusRunning); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	// Verify
	got, err := store.GetActor(ctx, "test-ns")
	if err != nil {
		t.Fatalf("GetActor() error = %v", err)
	}
	if got.Status != ActorStatusRunning {
		t.Errorf("Status = %q, want running", got.Status)
	}
}

func TestRegistryStore_UpdateStatus_NotFound(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	err = store.UpdateStatus(ctx, "nonexistent", ActorStatusRunning)
	if !errors.Is(err, ErrActorNotFound) {
		t.Errorf("UpdateStatus() error = %v, want ErrActorNotFound", err)
	}
}

func TestRegistryStore_ListActorsByStatus(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	// Register actors with different statuses
	actors := []ActorRecord{
		{Namespace: "ns-1", Role: "coder", ConfigJSON: `{"id":"1"}`, Status: ActorStatusRunning},
		{Namespace: "ns-2", Role: "planner", ConfigJSON: `{"id":"2"}`, Status: ActorStatusRunning},
		{Namespace: "ns-3", Role: "reviewer", ConfigJSON: `{"id":"3"}`, Status: ActorStatusStopped},
	}

	for _, a := range actors {
		if err := store.RegisterActor(ctx, a); err != nil {
			t.Fatalf("RegisterActor(%s) error = %v", a.Namespace, err)
		}
	}

	// List running actors
	running, err := store.ListActorsByStatus(ctx, ActorStatusRunning)
	if err != nil {
		t.Fatalf("ListActorsByStatus(running) error = %v", err)
	}
	if len(running) != 2 {
		t.Errorf("ListActorsByStatus(running) returned %d, want 2", len(running))
	}

	// List stopped actors
	stopped, err := store.ListActorsByStatus(ctx, ActorStatusStopped)
	if err != nil {
		t.Fatalf("ListActorsByStatus(stopped) error = %v", err)
	}
	if len(stopped) != 1 {
		t.Errorf("ListActorsByStatus(stopped) returned %d, want 1", len(stopped))
	}
}

func TestMarshalConfig(t *testing.T) {
	cfg := Config{
		ID:        "actor-1",
		Namespace: "test-ns",
		Role:      "coder",
	}

	json, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig() error = %v", err)
	}
	if json == "" {
		t.Error("MarshalConfig() returned empty string")
	}

	// Should contain the fields
	if !contains(json, "actor-1") {
		t.Error("JSON should contain actor-1")
	}
	if !contains(json, "test-ns") {
		t.Error("JSON should contain test-ns")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestRegistryStore_Close(t *testing.T) {
	db := setupTestRegistryDB(t)
	defer db.Close()

	ctx := context.Background()
	store, err := NewRegistryStore(ctx, db)
	if err != nil {
		t.Fatalf("NewRegistryStore() error = %v", err)
	}

	// Close should be a no-op (doesn't own db)
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// DB should still be usable
	_, err = store.ListActors(ctx)
	if err != nil {
		t.Errorf("ListActors after Close() error = %v", err)
	}
}

func TestActorStatus_Values(t *testing.T) {
	// Verify status constants
	statuses := []ActorStatus{
		ActorStatusRegistered,
		ActorStatusRunning,
		ActorStatusStopped,
		ActorStatusError,
	}

	expected := []string{"registered", "running", "stopped", "error"}
	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("status[%d] = %q, want %q", i, s, expected[i])
		}
	}
}

func TestRegistryStore_Interface(t *testing.T) {
	// Verify SQLiteRegistryStore implements RegistryStore
	var _ RegistryStore = (*SQLiteRegistryStore)(nil)
}
