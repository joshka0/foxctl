package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/runtime/actor"
)

func TestWithActorRegistryStoreInitializesReusableStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	configJSON, err := actor.MarshalConfig(actor.Config{
		ID:        "actor:test",
		Namespace: "actor:test",
		Role:      "coder",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := withActorRegistryStore(ctx, storageRoot, func(regStore *actor.SQLiteRegistryStore) error {
		return regStore.RegisterActor(ctx, actor.ActorRecord{
			Namespace:  "actor:test",
			Role:       "coder",
			ConfigJSON: configJSON,
			Status:     actor.ActorStatusRegistered,
		})
	}); err != nil {
		t.Fatalf("register actor: %v", err)
	}

	if err := withActorRegistryStore(ctx, storageRoot, func(regStore *actor.SQLiteRegistryStore) error {
		record, err := regStore.GetActor(ctx, "actor:test")
		if err != nil {
			return err
		}
		if record.Namespace != "actor:test" || record.Role != "coder" || record.Status != actor.ActorStatusRegistered {
			return fmt.Errorf("record=%+v", record)
		}
		return nil
	}); err != nil {
		t.Fatalf("get actor: %v", err)
	}
}

func TestActorRegistryCommandSetupMessagePreservesEnvelopeText(t *testing.T) {
	t.Parallel()

	openErr := &actorRegistrySetupError{kind: actorRegistrySetupOpen, err: errors.New("open failed")}
	message, ok := actorRegistryCommandSetupMessage(openErr)
	if !ok {
		t.Fatal("expected open setup error to be recognized")
	}
	if message != "failed to open registry: open failed" {
		t.Fatalf("open message=%q", message)
	}

	createErr := &actorRegistrySetupError{kind: actorRegistrySetupCreate, err: errors.New("schema failed")}
	message, ok = actorRegistryCommandSetupMessage(createErr)
	if !ok {
		t.Fatal("expected create setup error to be recognized")
	}
	if message != "failed to create registry store: schema failed" {
		t.Fatalf("create message=%q", message)
	}

	if _, ok := actorRegistryCommandSetupMessage(errors.New("list failed")); ok {
		t.Fatal("operation errors should not be treated as setup errors")
	}
}

func TestActorRegistryRespawnSetupErrorPreservesExistingPrefixes(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("db failed")
	got := actorRegistryRespawnSetupError(&actorRegistrySetupError{kind: actorRegistrySetupOpen, err: sentinel})
	if got == nil || got.Error() != "open registry db: db failed" {
		t.Fatalf("open respawn error=%v", got)
	}
	if !errors.Is(got, sentinel) {
		t.Fatalf("open respawn error should wrap sentinel, got %v", got)
	}

	sentinel = errors.New("schema failed")
	got = actorRegistryRespawnSetupError(&actorRegistrySetupError{kind: actorRegistrySetupCreate, err: sentinel})
	if got == nil || got.Error() != "create registry store: schema failed" {
		t.Fatalf("create respawn error=%v", got)
	}
	if !errors.Is(got, sentinel) {
		t.Fatalf("create respawn error should wrap sentinel, got %v", got)
	}

	operationErr := fmt.Errorf("list running actors: %w", errors.New("query failed"))
	if got := actorRegistryRespawnSetupError(operationErr); got != operationErr {
		t.Fatalf("operation error changed: got %v want %v", got, operationErr)
	}
}

func TestWithActorSysMeta(t *testing.T) {
	t.Parallel()

	meta := envelope.Meta{}
	withActorSysMeta(&meta)
	if meta.Source != "actorsys" {
		t.Fatalf("source=%q want actorsys", meta.Source)
	}
	if len(meta.Profiles) != 2 || meta.Profiles[0] != "core/v1" || meta.Profiles[1] != "actor/v1" {
		t.Fatalf("profiles=%v", meta.Profiles)
	}
}
