package transcriptpipeline

import (
	"context"
	"testing"

	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
)

func TestPersistClassifiedClaims_PersistsDurableTypedClaims(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("memory.Open() error = %v", err)
	}
	defer store.Close()

	reports, err := PersistClassifiedClaims(ctx, store, sourceimport.ParsedSession{
		Provider:  sourceimport.ProviderCodex,
		SessionID: "sess-1",
	}, "conv-1", "/tmp/ws", nil, []ClassifiedClaim{
		{
			Text:                 "Use a classifier layer for durable labeling.",
			Kind:                 ClaimKindWorkflowRule,
			Durability:           ClaimDurabilityDurable,
			PromotionBlocker:     ClaimPromotionBlockerNone,
			Confidence:           0.82,
			SourceBasis:          "user_approved",
			GroupKeys:            []string{"pipeline/classification"},
			EvidenceFrameIndices: []int{3},
		},
		{
			Text:                 "The import shell is extracted from the CLI.",
			Kind:                 ClaimKindTechnical,
			Durability:           ClaimDurabilityDurable,
			PromotionBlocker:     ClaimPromotionBlockerImplementationStatus,
			Confidence:           0.90,
			SourceBasis:          "mixed",
			GroupKeys:            []string{"architecture/pipeline"},
			EvidenceFrameIndices: []int{2},
		},
		{
			Text:                 "Ask whether the rule should be durable.",
			Kind:                 ClaimKindOpenQuestion,
			Durability:           ClaimDurabilitySession,
			Confidence:           0.80,
			SourceBasis:          "user",
			GroupKeys:            []string{"pipeline/classification"},
			EvidenceFrameIndices: []int{4},
		},
	})
	if err != nil {
		t.Fatalf("PersistClassifiedClaims() error = %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports=%d want 1", len(reports))
	}
	if reports[0].Type != "learning" {
		t.Fatalf("memory_type=%q want learning", reports[0].Type)
	}
	if _, err := store.Get(ctx, reports[0].Name, "/tmp/ws"); err != nil {
		t.Fatalf("expected persisted classified claim: %v", err)
	}
}

func TestShouldPersistClassifiedClaim_UsesObjectiveAlignmentToPruneIrrelevantArchitecture(t *testing.T) {
	claim := ClassifiedClaim{
		Text:             "Append-only experiment logging provides reliable auto-memory.",
		Kind:             ClaimKindArchitecture,
		Durability:       ClaimDurabilityDurable,
		PromotionBlocker: ClaimPromotionBlockerNone,
		Confidence:       0.84,
		SourceBasis:      "mixed",
		ObjectiveRole:    ObjectiveRoleIrrelevant,
		ObjectiveAction:  ObjectiveMemoryActionPrune,
		ObjectiveScore:   0.81,
	}
	if shouldPersistClassifiedClaim(claim, &SessionObjective{Objective: "Build a robust auto-memory pipeline."}) {
		t.Fatal("expected irrelevant objective-aligned architecture claim to be pruned")
	}
	claim.ObjectiveRole = ObjectiveRoleSupport
	claim.ObjectiveAction = ObjectiveMemoryActionKeep
	if !shouldPersistClassifiedClaim(claim, &SessionObjective{Objective: "Build a robust auto-memory pipeline."}) {
		t.Fatal("expected supportive objective-aligned architecture claim to persist")
	}
	claim.ObjectiveRole = ObjectiveRoleBlock
	claim.ObjectiveAction = ObjectiveMemoryActionPrune
	if shouldPersistClassifiedClaim(claim, &SessionObjective{Objective: "Build a robust auto-memory pipeline."}) {
		t.Fatal("expected blocker-role architecture claim to be pruned")
	}
}

func TestReconcileMemoryPrefix_RemovesStaleEntries(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("memory.Open() error = %v", err)
	}
	defer store.Close()

	workspace := "/tmp/ws"
	prefix := TranscriptMemoryPrefix("codex:root")
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

	removed, err := ReconcileMemoryPrefix(ctx, store, workspace, prefix, []PersistedMemory{{
		Name:    prefix + "learning:keep",
		Type:    "learning",
		Summary: "keep",
	}})
	if err != nil {
		t.Fatalf("ReconcileMemoryPrefix() error = %v", err)
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
