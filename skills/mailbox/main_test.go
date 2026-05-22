package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/teams"
)

func TestMailboxSend_TeamFanout(t *testing.T) {
	env := newMailboxTestEnv(t)

	env.upsertTeam(t, teams.Team{WorkspaceID: env.workspaceID, TeamID: "team:backend", Name: "Backend"})
	env.addMember(t, teams.TeamMember{WorkspaceID: env.workspaceID, TeamID: "team:backend", ActorID: "agent:1"})
	env.addMember(t, teams.TeamMember{WorkspaceID: env.workspaceID, TeamID: "team:backend", ActorID: "agent:2"})

	data := env.run(t, input{
		Operation:   "send",
		WorkspaceID: env.workspaceID,
		Send: &sendReq{
			Sender:    "overseer",
			Recipient: "team:backend",
			Subject:   "Hello",
			Body:      "Hi",
		},
	})

	delivered, ok := data["delivered_count"].(float64)
	if !ok {
		t.Fatalf("expected delivered_count number, got %T", data["delivered_count"])
	}
	if delivered != 2 {
		t.Fatalf("expected delivered_count=2, got %v", delivered)
	}

	ids := stringSlice(t, data["message_ids"])
	if len(ids) != 2 {
		t.Fatalf("expected 2 message_ids, got %d", len(ids))
	}
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	m1 := env.inboxMessages(t, "agent:1")
	if len(m1) != 1 {
		t.Fatalf("expected 1 message for agent:1, got %d", len(m1))
	}
	if m1[0].Subject != "Hello" {
		t.Fatalf("expected subject %q, got %q", "Hello", m1[0].Subject)
	}
	if m1[0].Recipient != "agent:1" {
		t.Fatalf("expected recipient %q, got %q", "agent:1", m1[0].Recipient)
	}
	if _, ok := idSet[m1[0].ID]; !ok {
		t.Fatalf("unexpected message id %q", m1[0].ID)
	}

	m2 := env.inboxMessages(t, "agent:2")
	if len(m2) != 1 {
		t.Fatalf("expected 1 message for agent:2, got %d", len(m2))
	}
	if m2[0].Subject != "Hello" {
		t.Fatalf("expected subject %q, got %q", "Hello", m2[0].Subject)
	}
	if m2[0].Recipient != "agent:2" {
		t.Fatalf("expected recipient %q, got %q", "agent:2", m2[0].Recipient)
	}
	if _, ok := idSet[m2[0].ID]; !ok {
		t.Fatalf("unexpected message id %q", m2[0].ID)
	}

	mTeam := env.inboxMessages(t, "team:backend")
	if len(mTeam) != 0 {
		t.Fatalf("expected no messages for team:backend inbox, got %d", len(mTeam))
	}
}

func TestMailboxSend_TeamNotFound(t *testing.T) {
	env := newMailboxTestEnv(t)

	err := env.expectError(t, input{
		Operation:   "send",
		WorkspaceID: env.workspaceID,
		Send: &sendReq{
			Sender:    "overseer",
			Recipient: "team:missing",
			Subject:   "Hello",
			Body:      "Hi",
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "team not found") {
		t.Fatalf("expected error to mention team not found, got %v", err)
	}
}

func TestMailboxSend_TeamHasNoMembers(t *testing.T) {
	env := newMailboxTestEnv(t)

	env.upsertTeam(t, teams.Team{WorkspaceID: env.workspaceID, TeamID: "team:backend", Name: "Backend"})

	err := env.expectError(t, input{
		Operation:   "send",
		WorkspaceID: env.workspaceID,
		Send: &sendReq{
			Sender:    "overseer",
			Recipient: "team:backend",
			Subject:   "Hello",
			Body:      "Hi",
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "team has no members") {
		t.Fatalf("expected error to mention no members, got %v", err)
	}
}

type mailboxTestEnv struct {
	ctx         context.Context
	workspaceID string
	rc          *skillmain.RunContext
}

func newMailboxTestEnv(t *testing.T) *mailboxTestEnv {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	rc := newTestRunnerContext(t, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	return &mailboxTestEnv{
		ctx:         ctx,
		workspaceID: "test-workspace",
		rc:          rc,
	}
}

func (env *mailboxTestEnv) run(t *testing.T, in input) map[string]any {
	t.Helper()
	buf := runSkill(env.ctx, t, env.rc, in)
	return decodeData(t, buf)
}

func (env *mailboxTestEnv) expectError(t *testing.T, in input) error {
	t.Helper()
	env.rc.Stdout = &bytes.Buffer{}
	return run(env.ctx, env.rc, in)
}

func (env *mailboxTestEnv) upsertTeam(t *testing.T, team teams.Team) {
	t.Helper()
	store, err := teams.Open(env.ctx, env.rc.Config.Storage.Root)
	if err != nil {
		t.Fatalf("open teams store: %v", err)
	}
	defer store.Close()

	if _, err := store.UpsertTeam(env.ctx, team); err != nil {
		t.Fatalf("upsert team: %v", err)
	}
}

func (env *mailboxTestEnv) addMember(t *testing.T, member teams.TeamMember) {
	t.Helper()
	store, err := teams.Open(env.ctx, env.rc.Config.Storage.Root)
	if err != nil {
		t.Fatalf("open teams store: %v", err)
	}
	defer store.Close()

	if err := store.AddMember(env.ctx, member); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func (env *mailboxTestEnv) inboxMessages(t *testing.T, actorID string) []agent.BoardMessage {
	t.Helper()
	store, err := blackboard.OpenBoardStore(env.ctx, env.rc.Config.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer store.Close()

	msgs, err := store.Inbox(env.ctx, agent.InboxFilter{
		WorkspaceID: env.workspaceID,
		ActorID:     actorID,
		OnlyUnread:  true,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	return msgs
}

func newTestRunnerContext(t *testing.T, tmp string) *skillmain.RunContext {
	t.Helper()
	cfg := config.Config{
		Home:           tmp,
		InlineOutputKB: config.DefaultInlineOutputKB,
		MaxCaptureKB:   config.DefaultMaxCaptureKB,
		Paths: config.Paths{
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
			Cache: filepath.Join(tmp, "cache"),
		},
		Storage: config.StorageSettings{
			Root: filepath.Join(tmp, "storage"),
		},
	}
	rc, err := skillmain.BuildRunContext(cfg, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func runSkill(ctx context.Context, t *testing.T, rc *skillmain.RunContext, in input) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	rc.Stdout = buf
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}
	return buf
}

func decodeData(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var out envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if out.Status != envelope.StatusOK {
		t.Fatalf("expected ok status, got %v", out.Status)
	}
	data, ok := out.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", out.Data)
	}
	return data
}

func stringSlice(t *testing.T, v any) []string {
	t.Helper()
	items, ok := v.([]any)
	if !ok {
		t.Fatalf("expected slice, got %T", v)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("expected string, got %T", item)
		}
		out = append(out, s)
	}
	return out
}
