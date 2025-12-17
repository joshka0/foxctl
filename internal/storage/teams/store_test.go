package teams

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_UpsertAndGetTeam(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	team := Team{
		WorkspaceID:  "ws-1",
		TeamID:       "team:backend",
		Name:         "Backend",
		Description:  "Backend team",
		PrimaryEpics: []string{"epic:one"},
		Tags:         []string{"tag:a"},
	}

	got, err := store.UpsertTeam(ctx, team)
	if err != nil {
		t.Fatalf("UpsertTeam failed: %v", err)
	}
	if got.WorkspaceID != team.WorkspaceID {
		t.Fatalf("expected workspace_id %q, got %q", team.WorkspaceID, got.WorkspaceID)
	}
	if got.TeamID != team.TeamID {
		t.Fatalf("expected team_id %q, got %q", team.TeamID, got.TeamID)
	}
	if got.Name != team.Name {
		t.Fatalf("expected name %q, got %q", team.Name, got.Name)
	}
	if got.Description != team.Description {
		t.Fatalf("expected description %q, got %q", team.Description, got.Description)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}

	read, err := store.GetTeam(ctx, team.WorkspaceID, team.TeamID)
	if err != nil {
		t.Fatalf("GetTeam failed: %v", err)
	}
	if read.TeamID != team.TeamID {
		t.Fatalf("expected team_id %q, got %q", team.TeamID, read.TeamID)
	}
	if read.Name != team.Name {
		t.Fatalf("expected name %q, got %q", team.Name, read.Name)
	}
}

func TestStore_GetTeam_NotFound(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	_, err := store.GetTeam(ctx, "ws-1", "team:missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_UpsertTeam_PreservesCreatedAt(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	created := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	first, err := store.UpsertTeam(ctx, Team{
		WorkspaceID: "ws-1",
		TeamID:      "team:backend",
		Name:        "Backend",
		CreatedAt:   created,
	})
	if err != nil {
		t.Fatalf("UpsertTeam (first) failed: %v", err)
	}
	if !first.CreatedAt.Equal(created) {
		t.Fatalf("expected created_at %s, got %s", created, first.CreatedAt)
	}

	secondCreated := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	second, err := store.UpsertTeam(ctx, Team{
		WorkspaceID: "ws-1",
		TeamID:      "team:backend",
		Name:        "Backend v2",
		CreatedAt:   secondCreated,
	})
	if err != nil {
		t.Fatalf("UpsertTeam (second) failed: %v", err)
	}
	if second.Name != "Backend v2" {
		t.Fatalf("expected name %q, got %q", "Backend v2", second.Name)
	}
	if !second.CreatedAt.Equal(created) {
		t.Fatalf("expected created_at to remain %s, got %s", created, second.CreatedAt)
	}
}

func TestStore_ListTeams_WorkspaceScoped(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	_, err := store.UpsertTeam(ctx, Team{WorkspaceID: "ws-1", TeamID: "team:alpha", Name: "Alpha"})
	if err != nil {
		t.Fatalf("UpsertTeam failed: %v", err)
	}
	_, err = store.UpsertTeam(ctx, Team{WorkspaceID: "ws-1", TeamID: "team:beta", Name: "Beta"})
	if err != nil {
		t.Fatalf("UpsertTeam failed: %v", err)
	}
	_, err = store.UpsertTeam(ctx, Team{WorkspaceID: "ws-2", TeamID: "team:gamma", Name: "Gamma"})
	if err != nil {
		t.Fatalf("UpsertTeam failed: %v", err)
	}

	ws1, err := store.ListTeams(ctx, "ws-1", 10)
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}
	if len(ws1) != 2 {
		t.Fatalf("expected 2 teams, got %d", len(ws1))
	}
	if ws1[0].TeamID != "team:alpha" {
		t.Fatalf("expected first team_id %q, got %q", "team:alpha", ws1[0].TeamID)
	}
	if ws1[1].TeamID != "team:beta" {
		t.Fatalf("expected second team_id %q, got %q", "team:beta", ws1[1].TeamID)
	}

	ws2, err := store.ListTeams(ctx, "ws-2", 10)
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}
	if len(ws2) != 1 {
		t.Fatalf("expected 1 team, got %d", len(ws2))
	}
	if ws2[0].TeamID != "team:gamma" {
		t.Fatalf("expected team_id %q, got %q", "team:gamma", ws2[0].TeamID)
	}
}

func TestStore_AddListRemoveMembers(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	_, err := store.UpsertTeam(ctx, Team{WorkspaceID: "ws-1", TeamID: "team:backend", Name: "Backend"})
	if err != nil {
		t.Fatalf("UpsertTeam failed: %v", err)
	}

	if err := store.AddMember(ctx, TeamMember{WorkspaceID: "ws-1", TeamID: "team:backend", ActorID: "agent:1", Role: "member"}); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	members, err := store.ListMembers(ctx, "ws-1", "team:backend", 10)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if members[0].ActorID != "agent:1" {
		t.Fatalf("expected actor_id %q, got %q", "agent:1", members[0].ActorID)
	}
	if members[0].Role != "member" {
		t.Fatalf("expected role %q, got %q", "member", members[0].Role)
	}

	if err := store.AddMember(ctx, TeamMember{WorkspaceID: "ws-1", TeamID: "team:backend", ActorID: "agent:1", Role: "lead"}); err != nil {
		t.Fatalf("AddMember (update) failed: %v", err)
	}

	members, err = store.ListMembers(ctx, "ws-1", "team:backend", 10)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if members[0].Role != "lead" {
		t.Fatalf("expected updated role %q, got %q", "lead", members[0].Role)
	}

	if err := store.RemoveMember(ctx, "ws-1", "team:backend", "agent:1"); err != nil {
		t.Fatalf("RemoveMember failed: %v", err)
	}

	members, err = store.ListMembers(ctx, "ws-1", "team:backend", 10)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("expected 0 members, got %d", len(members))
	}
}

func TestStore_ListMembers_WorkspaceScoped(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	_, err := store.UpsertTeam(ctx, Team{WorkspaceID: "ws-1", TeamID: "team:backend", Name: "Backend"})
	if err != nil {
		t.Fatalf("UpsertTeam failed: %v", err)
	}
	_, err = store.UpsertTeam(ctx, Team{WorkspaceID: "ws-2", TeamID: "team:backend", Name: "Backend"})
	if err != nil {
		t.Fatalf("UpsertTeam failed: %v", err)
	}

	if err := store.AddMember(ctx, TeamMember{WorkspaceID: "ws-1", TeamID: "team:backend", ActorID: "agent:1"}); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}
	if err := store.AddMember(ctx, TeamMember{WorkspaceID: "ws-2", TeamID: "team:backend", ActorID: "agent:2"}); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	ws1, err := store.ListMembers(ctx, "ws-1", "team:backend", 10)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(ws1) != 1 {
		t.Fatalf("expected 1 member, got %d", len(ws1))
	}
	if ws1[0].ActorID != "agent:1" {
		t.Fatalf("expected actor_id %q, got %q", "agent:1", ws1[0].ActorID)
	}

	ws2, err := store.ListMembers(ctx, "ws-2", "team:backend", 10)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(ws2) != 1 {
		t.Fatalf("expected 1 member, got %d", len(ws2))
	}
	if ws2[0].ActorID != "agent:2" {
		t.Fatalf("expected actor_id %q, got %q", "agent:2", ws2[0].ActorID)
	}
}

func setupTestStore(t *testing.T) Store {
	t.Helper()
	root := filepath.Join(t.TempDir(), "storage")
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		// Test cleanup; error is not actionable.
		_ = store.Close() //nolint:errcheck
	})
	return store
}
