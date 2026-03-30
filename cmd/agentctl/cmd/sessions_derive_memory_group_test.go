package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/transcriptpipeline"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

func TestGroupTranscriptSourceBundles_UsesParentLineage(t *testing.T) {
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	bundles := []transcriptpipeline.SourceBundle{
		{
			Meta: transcriptpipeline.SourceMeta{
				Provider:      sourceimport.ProviderCodex,
				SessionID:     "root",
				SourcePath:    "/tmp/root.jsonl",
				WorkspacePath: "/tmp/ws",
				StartedAt:     now,
			},
			Parsed: sourceimport.ParsedSession{Provider: sourceimport.ProviderCodex, SessionID: "root"},
		},
		{
			Meta: transcriptpipeline.SourceMeta{
				Provider:        sourceimport.ProviderCodex,
				SessionID:       "child",
				ParentSessionID: "root",
				SourcePath:      "/tmp/child.jsonl",
				WorkspacePath:   "/tmp/ws",
				StartedAt:       now.Add(time.Minute),
			},
			Parsed: sourceimport.ParsedSession{Provider: sourceimport.ProviderCodex, SessionID: "child"},
		},
		{
			Meta: transcriptpipeline.SourceMeta{
				Provider:      sourceimport.ProviderCodex,
				SessionID:     "parallel",
				SourcePath:    "/tmp/parallel.jsonl",
				WorkspacePath: "/tmp/ws",
				StartedAt:     now.Add(2 * time.Minute),
			},
			Parsed: sourceimport.ParsedSession{Provider: sourceimport.ProviderCodex, SessionID: "parallel"},
		},
	}

	groups := transcriptpipeline.GroupSourceBundles(bundles)
	if len(groups) != 2 {
		t.Fatalf("groups=%d want 2", len(groups))
	}

	foundRootGroup := false
	foundParallel := false
	for _, group := range groups {
		switch group.GroupID {
		case "codex:root":
			foundRootGroup = true
			if len(group.Bundles) != 2 {
				t.Fatalf("root group bundles=%d want 2", len(group.Bundles))
			}
			if len(group.MainlineBundles()) != 2 {
				t.Fatalf("root group mainline=%d want 2", len(group.MainlineBundles()))
			}
		case "codex:parallel":
			foundParallel = true
			if len(group.Bundles) != 1 {
				t.Fatalf("parallel group bundles=%d want 1", len(group.Bundles))
			}
		}
	}
	if !foundRootGroup || !foundParallel {
		t.Fatalf("unexpected groups: %#v", groups)
	}
}

func TestGroupTranscriptSourceBundles_SeparatesSubagentsFromMainline(t *testing.T) {
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	bundles := []transcriptpipeline.SourceBundle{
		{
			Meta: transcriptpipeline.SourceMeta{
				Provider:      sourceimport.ProviderCodex,
				SessionID:     "root",
				SourcePath:    "/tmp/root.jsonl",
				WorkspacePath: "/tmp/ws",
				StartedAt:     now,
			},
			Parsed: sourceimport.ParsedSession{Provider: sourceimport.ProviderCodex, SessionID: "root"},
		},
		{
			Meta: transcriptpipeline.SourceMeta{
				Provider:        sourceimport.ProviderCodex,
				SessionID:       "child-subagent",
				ParentSessionID: "root",
				SourcePath:      "/tmp/subagent.jsonl",
				WorkspacePath:   "/tmp/ws",
				IsSubagent:      true,
				AgentRole:       "explorer",
				StartedAt:       now.Add(time.Minute),
			},
			Parsed: sourceimport.ParsedSession{Provider: sourceimport.ProviderCodex, SessionID: "child-subagent"},
		},
	}

	groups := transcriptpipeline.GroupSourceBundles(bundles)
	if len(groups) != 1 {
		t.Fatalf("groups=%d want 1", len(groups))
	}
	group := groups[0]
	if got := transcriptpipeline.SessionIDsForBundles(group.MainlineBundles()); len(got) != 1 || got[0] != "root" {
		t.Fatalf("mainline=%v want [root]", got)
	}
	if got := transcriptpipeline.SessionIDsForBundles(group.SidecarBundles()); len(got) != 1 || got[0] != "child-subagent" {
		t.Fatalf("sidecars=%v want [child-subagent]", got)
	}
}

func TestReconcileTranscriptMemoryPrefix_RemovesStaleEntries(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("memory.Open() error = %v", err)
	}
	defer store.Close()

	workspace := "/tmp/ws"
	prefix := transcriptpipeline.TranscriptMemoryPrefix("codex:root")
	for _, name := range []string{
		prefix + "learning:keep",
		prefix + "learning:drop",
		"transcript:other:learning:stay",
	} {
		if _, err := store.Save(ctx, storage.NamedEntry{
			Name:      name,
			Type:      "learning",
			Workspace: workspace,
			Summary:   name,
			Result:    []byte(`{}`),
		}); err != nil {
			t.Fatalf("Save(%q) error = %v", name, err)
		}
	}

	removed, err := transcriptpipeline.ReconcileMemoryPrefix(ctx, store, workspace, prefix, []transcriptpipeline.PersistedMemory{{
		Name:    prefix + "learning:keep",
		Type:    "learning",
		Summary: "keep",
	}})
	if err != nil {
		t.Fatalf("reconcileTranscriptMemoryPrefix() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != prefix+"learning:drop" {
		t.Fatalf("removed=%v want [%q]", removed, prefix+"learning:drop")
	}
	if _, err := store.Get(ctx, prefix+"learning:drop", workspace); err == nil {
		t.Fatal("expected dropped entry to be deleted")
	}
	if _, err := store.Get(ctx, prefix+"learning:keep", workspace); err != nil {
		t.Fatalf("expected keep entry to remain: %v", err)
	}
	if _, err := store.Get(ctx, "transcript:other:learning:stay", workspace); err != nil {
		t.Fatalf("expected unrelated entry to remain: %v", err)
	}
}

func TestPolishConsensusClaimTexts_CanonicalizesArchitectureClaims(t *testing.T) {
	in := []string{
		"The existing hybrid pipeline already gives you event sourcing, typed hard state, assumptions, episodes, and a maintenance daemon. What’s missing for your recursive memory idea is a second-pass consolidation layer over the hybrid runtime.",
		"`named_memory` is keyed by `(name, workspace)`, supports `session_id`, and already has optional embedding fields.",
	}

	got := transcriptpipeline.PolishConsensusClaimTexts(in)
	if len(got) != 2 {
		t.Fatalf("claims=%v want 2 items", got)
	}
	if got[0] != "Implement auto-memory as a second-pass consolidator over the existing hybrid companion runtime." {
		t.Fatalf("claim[0]=%q", got[0])
	}
	if got[1] != "Use named_memory as the durable sink for transcript-derived memories." {
		t.Fatalf("claim[1]=%q", got[1])
	}
}

func TestPolishConsensusClaimTexts_DropsMetaProcessChatter(t *testing.T) {
	in := []string{
		"The main insight is that the companion runtime already provides a",
		"tool_result: exec_command: Command: /bin/zsh -lc ...",
		"I'm going to inspect the extraction primitives next.",
	}

	got := transcriptpipeline.PolishConsensusClaimTexts(in)
	if len(got) != 0 {
		t.Fatalf("expected no durable claims, got %v", got)
	}
}
