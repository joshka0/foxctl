package agents

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

func TestAgentStore(t *testing.T) {
	ctx := context.Background()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Open store
	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Test Create
	t.Run("Create", func(t *testing.T) {
		a := agent.Agent{
			ID:              "test-agent-001",
			ParentID:        "",
			Namespace:       "org/test",
			WorkspaceRoot:   "/sandbox/repo",
			WorkspaceSource: "sandbox",
			Role:            "test-role",
			Prompt:          "You are a test agent",
			SkillsAllow:     []string{"test/skill"},
			Policy: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Network:  "none",
			},
			ShareBB:         "scoped",
			State:           agent.StateStarting,
			CreatedAt:       time.Now().UTC(),
			HeartbeatAt:     time.Now().UTC(),
			LLMProvider:     "openai_compat",
			LLMModel:        "demo-model",
			LLMBaseURL:      "https://demo.example.com/v1",
			LLMAuthMode:     "header",
			LLMAuthHeader:   "X-Demo-Key",
			LLMAuthPrefix:   "Token ",
			SandboxProvider: "opensandbox",
			SandboxID:       "sbx-123",
			RepoURL:         "https://github.com/example/repo.git",
			RepoRef:         "main",
			TerminalBinding: agent.TerminalBinding{
				Backend:             "tmux",
				Session:             "collab",
				PaneID:              "%7",
				ParticipantID:       "agent-a",
				ParentParticipantID: "parent-a",
				ParentAgentID:       "agent:parent-1",
				RoomAccess:          "none",
			},
		}

		if err := store.Create(ctx, a); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	})

	// Test Get
	t.Run("Get", func(t *testing.T) {
		a, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if a.ID != "test-agent-001" {
			t.Errorf("expected ID test-agent-001, got %s", a.ID)
		}
		if a.Role != "test-role" {
			t.Errorf("expected role test-role, got %s", a.Role)
		}
		if a.WorkspaceRoot != "/sandbox/repo" {
			t.Errorf("expected workspace_root round-trip, got %q", a.WorkspaceRoot)
		}
		if a.WorkspaceSource != "sandbox" {
			t.Errorf("expected workspace_source round-trip, got %q", a.WorkspaceSource)
		}
		if a.LLMBaseURL != "https://demo.example.com/v1" {
			t.Errorf("expected llm_base_url round-trip, got %q", a.LLMBaseURL)
		}
		if a.LLMAuthMode != "header" {
			t.Errorf("expected llm_auth_mode round-trip, got %q", a.LLMAuthMode)
		}
		if a.LLMAuthHeader != "X-Demo-Key" {
			t.Errorf("expected llm_auth_header round-trip, got %q", a.LLMAuthHeader)
		}
		if a.LLMAuthPrefix != "Token " {
			t.Errorf("expected llm_auth_prefix round-trip, got %q", a.LLMAuthPrefix)
		}
		if a.MemoryScope != agent.MemoryScopeAgent {
			t.Errorf("expected default memory scope %q, got %q", agent.MemoryScopeAgent, a.MemoryScope)
		}
		if a.MemoryRetention != agent.MemoryRetentionDurable {
			t.Errorf("expected default memory retention %q, got %q", agent.MemoryRetentionDurable, a.MemoryRetention)
		}
		if a.ExecutionLayer != agent.ExecutionLayerClassic {
			t.Errorf("expected default execution layer %q, got %q", agent.ExecutionLayerClassic, a.ExecutionLayer)
		}
		if a.SandboxProvider != "opensandbox" {
			t.Errorf("expected sandbox_provider round-trip, got %q", a.SandboxProvider)
		}
		if a.SandboxID != "sbx-123" {
			t.Errorf("expected sandbox_id round-trip, got %q", a.SandboxID)
		}
		if a.RepoURL != "https://github.com/example/repo.git" {
			t.Errorf("expected repo_url round-trip, got %q", a.RepoURL)
		}
		if a.RepoRef != "main" {
			t.Errorf("expected repo_ref round-trip, got %q", a.RepoRef)
		}
		if a.TerminalBinding.ParticipantID != "agent-a" {
			t.Errorf("expected terminal binding participant round-trip, got %q", a.TerminalBinding.ParticipantID)
		}
		if a.TerminalBinding.RoomAccess != "none" {
			t.Errorf("expected terminal binding room access round-trip, got %q", a.TerminalBinding.RoomAccess)
		}
	})

	// Test GetByNamespace
	t.Run("GetByNamespace", func(t *testing.T) {
		a, err := store.GetByNamespace(ctx, "org/test")
		if err != nil {
			t.Fatalf("failed to get agent by namespace: %v", err)
		}

		if a.ID != "test-agent-001" {
			t.Errorf("expected ID test-agent-001, got %s", a.ID)
		}
	})

	// Test UpdateState
	t.Run("UpdateState", func(t *testing.T) {
		if err := store.UpdateState(ctx, "test-agent-001", agent.StateRunning); err != nil {
			t.Fatalf("failed to update state: %v", err)
		}

		a, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if a.State != agent.StateRunning {
			t.Errorf("expected state running, got %s", a.State)
		}
	})

	// Test UpdatePrompt
	t.Run("UpdatePrompt", func(t *testing.T) {
		if err := store.UpdatePrompt(ctx, "test-agent-001", "You are an optimized test agent"); err != nil {
			t.Fatalf("failed to update prompt: %v", err)
		}

		a, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}
		if a.Prompt != "You are an optimized test agent" {
			t.Errorf("expected updated prompt, got %q", a.Prompt)
		}
	})

	// Test UpdateMemoryScope
	t.Run("UpdateMemoryScope", func(t *testing.T) {
		if err := store.UpdateMemoryScope(ctx, "test-agent-001", agent.MemoryScopeSession); err != nil {
			t.Fatalf("failed to update memory scope: %v", err)
		}

		a, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if a.MemoryScope != agent.MemoryScopeSession {
			t.Errorf("expected memory scope session, got %q", a.MemoryScope)
		}
	})

	// Test UpdateMemoryRetention
	t.Run("UpdateMemoryRetention", func(t *testing.T) {
		if err := store.UpdateMemoryRetention(ctx, "test-agent-001", agent.MemoryRetentionEphemeral); err != nil {
			t.Fatalf("failed to update memory retention: %v", err)
		}

		a, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if a.MemoryRetention != agent.MemoryRetentionEphemeral {
			t.Errorf("expected memory retention ephemeral, got %q", a.MemoryRetention)
		}
	})

	// Test UpdateTerminalBinding
	t.Run("UpdateTerminalBinding", func(t *testing.T) {
		binding := agent.TerminalBinding{
			Backend:       "zellij",
			Session:       "alpha",
			PaneID:        "researcher-a1b2",
			ParticipantID: "researcher-a1b2",
			RoomAccess:    "none",
		}
		if err := store.UpdateTerminalBinding(ctx, "test-agent-001", binding); err != nil {
			t.Fatalf("failed to update terminal binding: %v", err)
		}
		a, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}
		if a.TerminalBinding.Backend != "zellij" {
			t.Fatalf("expected updated terminal binding backend, got %q", a.TerminalBinding.Backend)
		}
		if a.TerminalBinding.PaneID != "researcher-a1b2" {
			t.Fatalf("expected updated terminal binding pane id, got %q", a.TerminalBinding.PaneID)
		}
	})

	// Test UpdateHeartbeat
	t.Run("UpdateHeartbeat", func(t *testing.T) {
		time.Sleep(100 * time.Millisecond)

		oldHeartbeat, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if err := store.UpdateHeartbeat(ctx, "test-agent-001"); err != nil {
			t.Fatalf("failed to update heartbeat: %v", err)
		}

		a, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if !a.HeartbeatAt.After(oldHeartbeat.HeartbeatAt) {
			t.Errorf("heartbeat should be updated")
		}
	})

	// Test List
	t.Run("List", func(t *testing.T) {
		agents, err := store.List(ctx, 10)
		if err != nil {
			t.Fatalf("failed to list agents: %v", err)
		}

		if len(agents) != 1 {
			t.Errorf("expected 1 agent, got %d", len(agents))
		}
	})

	// Test Delete
	t.Run("Delete", func(t *testing.T) {
		if err := store.Delete(ctx, "test-agent-001"); err != nil {
			t.Fatalf("failed to delete agent: %v", err)
		}

		_, err := store.Get(ctx, "test-agent-001")
		if err == nil {
			t.Errorf("expected error getting deleted agent")
		}
	})
}

func TestAgentStore_PersistsExecutionLayer(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	record := agent.Agent{
		ID:             "agent-jido-1",
		Namespace:      "agent-jido-1",
		Role:           "overseer",
		Prompt:         "test",
		SkillsAllow:    []string{},
		Policy:         agent.Policy{},
		ShareBB:        "scoped",
		State:          agent.StateRunning,
		CreatedAt:      time.Now().UTC(),
		HeartbeatAt:    time.Now().UTC(),
		ExecutionLayer: agent.ExecutionLayerJido,
	}

	if err := store.Create(ctx, record); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	got, err := store.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if got.ExecutionLayer != agent.ExecutionLayerJido {
		t.Fatalf("execution layer = %q, want %q", got.ExecutionLayer, agent.ExecutionLayerJido)
	}
}

func TestAgentStoreRejectsInvalidStateWrites(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	invalid := agent.State("paused")
	record := agent.Agent{
		ID:          "agent-invalid-state",
		Namespace:   "org/invalid-state",
		Role:        "coder",
		Prompt:      "test",
		SkillsAllow: []string{},
		Policy:      agent.Policy{},
		ShareBB:     "scoped",
		State:       invalid,
		CreatedAt:   time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC),
	}

	if err := store.Create(ctx, record); !errors.Is(err, agent.ErrInvalidState) {
		t.Fatalf("Create() error=%v, want ErrInvalidState", err)
	}

	record.State = agent.StateStarting
	if err := store.Create(ctx, record); err != nil {
		t.Fatalf("create valid agent: %v", err)
	}
	if err := store.UpdateState(ctx, record.ID, invalid); !errors.Is(err, agent.ErrInvalidState) {
		t.Fatalf("UpdateState() error=%v, want ErrInvalidState", err)
	}

	got, err := store.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("get after rejected update: %v", err)
	}
	if got.State != agent.StateStarting {
		t.Fatalf("state=%q want unchanged %q", got.State, agent.StateStarting)
	}
}

func TestAgentStoreNormalizesSkillsAllow(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	record := agent.Agent{
		ID:          "agent-normalized-skills",
		Namespace:   "org/normalized-skills",
		Role:        "coder",
		Prompt:      "test",
		SkillsAllow: []string{" test/skill ", "", "test/skill", "other/skill"},
		Policy:      agent.Policy{},
		ShareBB:     "scoped",
		State:       agent.StateStarting,
		CreatedAt:   time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC),
	}
	if err := store.Create(ctx, record); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := []string{"test/skill", "other/skill"}
	if !reflect.DeepEqual(got.SkillsAllow, want) {
		t.Fatalf("SkillsAllow=%v want %v", got.SkillsAllow, want)
	}
}

func TestNormalizeSkillsAllowProperty(t *testing.T) {
	prop := func(input []string) bool {
		got := normalizeSkillsAllow(input)
		if !reflect.DeepEqual(got, normalizeSkillsAllow(got)) {
			return false
		}
		seen := make(map[string]struct{}, len(got))
		for _, value := range got {
			if value == "" || strings.TrimSpace(value) != value {
				return false
			}
			if _, ok := seen[value]; ok {
				return false
			}
			seen[value] = struct{}{}
		}
		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("normalize skills allow property failed: %v", err)
	}
}

func TestAgentStoreReadsRejectCorruptPersistedState(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	storeIface, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer storeIface.Close()
	store := storeIface.(*sqlStore)

	record := agent.Agent{
		ID:          "agent-corrupt-state",
		ParentID:    "parent-1",
		Namespace:   "org/corrupt-state",
		Name:        "Corrupt State",
		Slug:        "corrupt-state",
		Role:        "coder",
		Prompt:      "test",
		SkillsAllow: []string{},
		Policy:      agent.Policy{},
		ShareBB:     "scoped",
		State:       agent.StateRunning,
		CreatedAt:   time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC),
	}
	if err := store.Create(ctx, record); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Fatalf("disable check constraints: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
		UPDATE agents SET state = $1 WHERE id = $2
	`, "paused", record.ID); err != nil {
		t.Fatalf("corrupt state: %v", err)
	}

	if _, err := store.Get(ctx, record.ID); !agentReadErrorIsInvalidState(err) {
		t.Fatalf("Get() error=%v, want invalid state error", err)
	}
	if _, err := store.GetByNamespace(ctx, record.Namespace); !agentReadErrorIsInvalidState(err) {
		t.Fatalf("GetByNamespace() error=%v, want invalid state error", err)
	}
	if _, err := store.GetBySlug(ctx, record.Slug); !agentReadErrorIsInvalidState(err) {
		t.Fatalf("GetBySlug() error=%v, want invalid state error", err)
	}
	if _, err := store.Resolve(ctx, record.Slug); !agentReadErrorIsInvalidState(err) {
		t.Fatalf("Resolve() error=%v, want invalid state error", err)
	}
	if _, err := store.List(ctx, 10); !agentReadErrorIsInvalidState(err) {
		t.Fatalf("List() error=%v, want invalid state error", err)
	}
	if _, err := store.ListByParent(ctx, record.ParentID, 10); !agentReadErrorIsInvalidState(err) {
		t.Fatalf("ListByParent() error=%v, want invalid state error", err)
	}
}

func TestAgentStoreReadsRejectCorruptPersistedSkillsAllow(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		raw  string
	}{
		{name: "malformed json", raw: `["test/skill"`},
		{name: "null json", raw: `null`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			storeIface, err := Open(ctx, tmpDir)
			if err != nil {
				t.Fatalf("failed to open store: %v", err)
			}
			defer storeIface.Close()
			store := storeIface.(*sqlStore)

			record := agent.Agent{
				ID:          "agent-corrupt-skills",
				ParentID:    "parent-1",
				Namespace:   "org/corrupt-skills",
				Name:        "Corrupt Skills",
				Slug:        "corrupt-skills",
				Role:        "coder",
				Prompt:      "test",
				SkillsAllow: []string{"test/skill"},
				Policy:      agent.Policy{},
				ShareBB:     "scoped",
				State:       agent.StateRunning,
				CreatedAt:   time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC),
			}
			if err := store.Create(ctx, record); err != nil {
				t.Fatalf("create agent: %v", err)
			}
			if _, err := store.db.ExecContext(ctx, `
				UPDATE agents SET skills_allow = $1 WHERE id = $2
			`, tc.raw, record.ID); err != nil {
				t.Fatalf("corrupt skills_allow: %v", err)
			}

			if _, err := store.Get(ctx, record.ID); !agentReadErrorNamesColumn(err, "skills_allow") {
				t.Fatalf("Get() error=%v, want it to name corrupt skills_allow", err)
			}
			if _, err := store.GetByNamespace(ctx, record.Namespace); !agentReadErrorNamesColumn(err, "skills_allow") {
				t.Fatalf("GetByNamespace() error=%v, want it to name corrupt skills_allow", err)
			}
			if _, err := store.GetBySlug(ctx, record.Slug); !agentReadErrorNamesColumn(err, "skills_allow") {
				t.Fatalf("GetBySlug() error=%v, want it to name corrupt skills_allow", err)
			}
			if _, err := store.Resolve(ctx, record.Slug); !agentReadErrorNamesColumn(err, "skills_allow") {
				t.Fatalf("Resolve() error=%v, want it to name corrupt skills_allow", err)
			}
			if _, err := store.List(ctx, 10); !agentReadErrorNamesColumn(err, "skills_allow") {
				t.Fatalf("List() error=%v, want it to name corrupt skills_allow", err)
			}
			if _, err := store.ListByParent(ctx, record.ParentID, 10); !agentReadErrorNamesColumn(err, "skills_allow") {
				t.Fatalf("ListByParent() error=%v, want it to name corrupt skills_allow", err)
			}
		})
	}
}

func TestAgentStoreRejectsInvalidPolicyWritesAndReads(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	storeIface, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer storeIface.Close()
	store := storeIface.(*sqlStore)

	record := agent.Agent{
		ID:          "agent-invalid-policy",
		ParentID:    "parent-1",
		Namespace:   "org/invalid-policy",
		Name:        "Invalid Policy",
		Slug:        "invalid-policy",
		Role:        "coder",
		Prompt:      "test",
		SkillsAllow: []string{},
		Policy:      agent.Policy{Network: "none", EgressAllow: []string{"api.example.com"}},
		ShareBB:     "scoped",
		State:       agent.StateRunning,
		CreatedAt:   time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC),
	}

	for _, tc := range []struct {
		name   string
		policy agent.Policy
	}{
		{name: "egress allow with network none", policy: agent.Policy{Network: "none", EgressAllow: []string{"api.example.com"}}},
		{name: "malformed timeout", policy: agent.Policy{Timeout: "tomorrow"}},
		{name: "zero timeout", policy: agent.Policy{Timeout: "0s"}},
		{name: "negative timeout", policy: agent.Policy{Timeout: "-1s"}},
	} {
		t.Run("write "+tc.name, func(t *testing.T) {
			record.Policy = tc.policy
			if err := store.Create(ctx, record); !agentReadErrorNamesColumn(err, "policy") {
				t.Fatalf("Create() error=%v, want invalid policy error", err)
			}
		})
	}

	record.Policy = agent.Policy{}
	if err := store.Create(ctx, record); err != nil {
		t.Fatalf("create valid agent: %v", err)
	}
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "negative cpu", raw: `{"cpu":-1}`},
		{name: "malformed timeout", raw: `{"timeout":"tomorrow"}`},
		{name: "zero timeout", raw: `{"timeout":"0s"}`},
		{name: "negative timeout", raw: `{"timeout":"-1s"}`},
	} {
		t.Run("read "+tc.name, func(t *testing.T) {
			if _, err := store.db.ExecContext(ctx, `
				UPDATE agents SET policy = $1 WHERE id = $2
			`, tc.raw, record.ID); err != nil {
				t.Fatalf("corrupt policy: %v", err)
			}

			assertAgentPolicyReadRejected(t, ctx, store, record)
		})
	}
}

func assertAgentPolicyReadRejected(t *testing.T, ctx context.Context, store *sqlStore, record agent.Agent) {
	t.Helper()

	if _, err := store.Get(ctx, record.ID); !agentReadErrorNamesColumn(err, "policy") {
		t.Fatalf("Get() error=%v, want invalid policy error", err)
	}
	if _, err := store.GetByNamespace(ctx, record.Namespace); !agentReadErrorNamesColumn(err, "policy") {
		t.Fatalf("GetByNamespace() error=%v, want invalid policy error", err)
	}
	if _, err := store.GetBySlug(ctx, record.Slug); !agentReadErrorNamesColumn(err, "policy") {
		t.Fatalf("GetBySlug() error=%v, want invalid policy error", err)
	}
	if _, err := store.Resolve(ctx, record.Slug); !agentReadErrorNamesColumn(err, "policy") {
		t.Fatalf("Resolve() error=%v, want invalid policy error", err)
	}
	if _, err := store.List(ctx, 10); !agentReadErrorNamesColumn(err, "policy") {
		t.Fatalf("List() error=%v, want invalid policy error", err)
	}
	if _, err := store.ListByParent(ctx, record.ParentID, 10); !agentReadErrorNamesColumn(err, "policy") {
		t.Fatalf("ListByParent() error=%v, want invalid policy error", err)
	}
}

func TestAgentStore_ReadsRejectCorruptPersistedTerminalBinding(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	storeIface, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer storeIface.Close()
	store := storeIface.(*sqlStore)

	record := agent.Agent{
		ID:          "agent-corrupt-terminal-binding",
		ParentID:    "parent-1",
		Namespace:   "org/corrupt-terminal-binding",
		Name:        "Corrupt Terminal Binding",
		Slug:        "corrupt-terminal-binding",
		Role:        "coder",
		Prompt:      "test",
		SkillsAllow: []string{},
		Policy:      agent.Policy{},
		ShareBB:     "scoped",
		State:       agent.StateRunning,
		CreatedAt:   time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC),
		TerminalBinding: agent.TerminalBinding{
			Backend:       "tmux",
			Session:       "collab",
			PaneID:        "%7",
			ParticipantID: "agent-corrupt",
			RoomAccess:    "none",
		},
	}
	if err := store.Create(ctx, record); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
		UPDATE agents SET terminal_binding = $1 WHERE id = $2
	`, `{"backend":`, record.ID); err != nil {
		t.Fatalf("corrupt terminal_binding: %v", err)
	}

	if _, err := store.Get(ctx, record.ID); !agentReadErrorNamesColumn(err, "terminal_binding") {
		t.Fatalf("Get() error=%v, want it to name corrupt terminal_binding", err)
	}
	if _, err := store.GetByNamespace(ctx, record.Namespace); !agentReadErrorNamesColumn(err, "terminal_binding") {
		t.Fatalf("GetByNamespace() error=%v, want it to name corrupt terminal_binding", err)
	}
	if _, err := store.GetBySlug(ctx, record.Slug); !agentReadErrorNamesColumn(err, "terminal_binding") {
		t.Fatalf("GetBySlug() error=%v, want it to name corrupt terminal_binding", err)
	}
	if _, err := store.Resolve(ctx, record.Slug); !agentReadErrorNamesColumn(err, "terminal_binding") {
		t.Fatalf("Resolve() error=%v, want it to name corrupt terminal_binding", err)
	}
	if _, err := store.List(ctx, 10); !agentReadErrorNamesColumn(err, "terminal_binding") {
		t.Fatalf("List() error=%v, want it to name corrupt terminal_binding", err)
	}
	if _, err := store.ListByParent(ctx, record.ParentID, 10); !agentReadErrorNamesColumn(err, "terminal_binding") {
		t.Fatalf("ListByParent() error=%v, want it to name corrupt terminal_binding", err)
	}
}

func agentReadErrorNamesColumn(err error, column string) bool {
	return err != nil && strings.Contains(err.Error(), column)
}

func agentReadErrorIsInvalidState(err error) bool {
	return errors.Is(err, agent.ErrInvalidState) && strings.Contains(err.Error(), "state")
}

func TestAgentStore_AllowsDuplicateNamespaces(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	first := agent.Agent{
		ID:          "agent-1",
		Namespace:   "org/shared",
		Role:        "researcher",
		Prompt:      "first",
		SkillsAllow: []string{},
		Policy:      agent.Policy{},
		ShareBB:     "scoped",
		State:       agent.StateStarting,
		CreatedAt:   time.Date(2026, time.March, 6, 10, 0, 0, 0, time.UTC),
	}
	second := agent.Agent{
		ID:          "agent-2",
		Namespace:   "org/shared",
		Role:        "coder",
		Prompt:      "second",
		SkillsAllow: []string{},
		Policy:      agent.Policy{},
		ShareBB:     "scoped",
		State:       agent.StateStarting,
		CreatedAt:   time.Date(2026, time.March, 6, 10, 1, 0, 0, time.UTC),
	}

	if err := store.Create(ctx, first); err != nil {
		t.Fatalf("failed to create first agent: %v", err)
	}
	if err := store.Create(ctx, second); err != nil {
		t.Fatalf("failed to create second agent in same namespace: %v", err)
	}

	agentsList, err := store.List(ctx, 10)
	if err != nil {
		t.Fatalf("failed to list agents: %v", err)
	}
	if len(agentsList) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agentsList))
	}

	got, err := store.GetByNamespace(ctx, "org/shared")
	if err != nil {
		t.Fatalf("failed to get by namespace: %v", err)
	}
	if got.ID != "agent-2" {
		t.Fatalf("expected latest agent-2 for namespace, got %s", got.ID)
	}
}

func TestAgentStore_MigratesLegacyUniqueNamespaceSchema(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, tmpDir+"/agents.db", nil)
	if err != nil {
		t.Fatalf("open raw sqlite db: %v", err)
	}
	defer func() { _ = closeFn() }()

	legacyDDL := `
CREATE TABLE agents (
	id           TEXT PRIMARY KEY,
	parent_id    TEXT,
	ns           TEXT UNIQUE NOT NULL,
	name         TEXT,
	slug         TEXT UNIQUE,
	role         TEXT,
	prompt       TEXT,
	skills_allow TEXT NOT NULL,
	policy       TEXT NOT NULL,
	share_bb     TEXT NOT NULL,
	state        TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	heartbeat_at TEXT,
	llm_provider TEXT,
	llm_model    TEXT,
	llm_api_key  TEXT,
	exec_mode    TEXT,
	max_iterations INTEGER,
	max_auto_turns INTEGER,
	think_interval INTEGER,
	deleted_at   TEXT,
	conversation_id TEXT,
	memory_scope TEXT
);`
	if _, err := db.ExecContext(ctx, legacyDDL); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}

	insertAgent := func(id, prompt, createdAt string) {
		t.Helper()
		_, err := db.ExecContext(
			ctx, `
			INSERT INTO agents (id, ns, role, prompt, skills_allow, policy, share_bb, state, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, "org/shared", "coder", prompt, "[]", "{}", "scoped", string(agent.StateStarting), createdAt,
		)
		if err != nil {
			t.Fatalf("insert legacy agent %s: %v", id, err)
		}
	}
	insertAgent("agent-1", "first", "2026-03-06T10:00:00Z")

	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}

	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()

	if err := store.Create(ctx, agent.Agent{
		ID:          "agent-2",
		Namespace:   "org/shared",
		Role:        "reviewer",
		Prompt:      "second",
		SkillsAllow: []string{},
		Policy:      agent.Policy{},
		ShareBB:     "scoped",
		State:       agent.StateStarting,
		CreatedAt:   time.Date(2026, time.March, 6, 10, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create second agent after migration: %v", err)
	}

	got, err := store.GetByNamespace(ctx, "org/shared")
	if err != nil {
		t.Fatalf("get by namespace after migration: %v", err)
	}
	if got.ID != "agent-2" {
		t.Fatalf("expected latest agent-2 after migration, got %s", got.ID)
	}
}
