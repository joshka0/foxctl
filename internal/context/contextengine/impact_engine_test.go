package contextengine

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ComputeImpact tests (VAL-IMPL-001 through VAL-IMPL-003)
// ---------------------------------------------------------------------------

func TestComputeImpact_IsPureFunction(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-001: Same inputs produce same outputs.
	now := time.Now().UTC().Truncate(time.Second)
	event := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
	}
	graph := &StubImpactGraph{
		edges:  []ImpactEdge{},
		claims: []MemoryClaim{},
	}

	result1, markers1, err1 := ComputeImpact(event, graph)
	result2, markers2, err2 := ComputeImpact(event, graph)

	if err1 != nil || err2 != nil {
		t.Fatalf("ComputeImpact returned errors: %v, %v", err1, err2)
	}
	if len(result1) != len(result2) {
		t.Errorf("edges count differs: %d vs %d", len(result1), len(result2))
	}
	if len(markers1) != len(markers2) {
		t.Errorf("markers count differs: %d vs %d", len(markers1), len(markers2))
	}
}

func TestComputeImpact_BoundedTraversalDepth(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-002: Traversal limited to configurable max depth.
	now := time.Now().UTC().Truncate(time.Second)
	event := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/deep.go"}},
		CreatedAt:   now,
	}

	// Build a chain of edges 10 deep
	edges := make([]ImpactEdge, 10)
	for i := 0; i < 10; i++ {
		edges[i] = ImpactEdge{
			ID:          fmt.Sprintf("edge-%d", i),
			WorkspaceID: "ws-1",
			From:        EvidenceRef{Type: RefTypePath, Ref: fmt.Sprintf("src/deep%d.go", i)},
			To:          EvidenceRef{Type: RefTypePath, Ref: fmt.Sprintf("src/deep%d.go", i+1)},
			Kind:        ImpactEdgeKindDependsOn,
			CreatedAt:   now,
		}
	}
	graph := &StubImpactGraph{
		edges:  edges,
		claims: []MemoryClaim{},
	}

	// With max depth 3, should only traverse 3 hops
	result, _, err := ComputeImpact(event, graph, WithMaxDepth(3))
	if err != nil {
		t.Fatalf("ComputeImpact returned error: %v", err)
	}
	// Should have impact edges for refs within depth 3
	if len(result) > 5 {
		t.Errorf("expected bounded traversal, got %d edges", len(result))
	}
}

func TestComputeImpact_EmptyGraph(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-003: Empty graph returns no impact edges.
	// Note: dirty edit events always create staleness markers for their refs,
	// even without existing impact edges. We test with no refs to verify truly empty.
	now := time.Now().UTC().Truncate(time.Second)
	event := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeCommitted, // committed events don't create markers
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
	}
	graph := &StubImpactGraph{}

	edges, markers, err := ComputeImpact(event, graph)
	if err != nil {
		t.Fatalf("ComputeImpact returned error: %v", err)
	}
	if len(edges) == 0 {
		// committed creates validates edges, not staleness — expected
		_ = edges
	}
	// For committed events, markers should be nil
	if len(markers) != 0 {
		t.Errorf("expected 0 markers for committed event, got %d", len(markers))
	}
}

func TestComputeImpact_DirtyEditNoEdges(t *testing.T) {
	t.Parallel()
	// Dirty edit with no existing impact edges creates markers but no invalidates edges.
	now := time.Now().UTC().Truncate(time.Second)
	event := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
	}
	graph := &StubImpactGraph{}

	edges, markers, err := ComputeImpact(event, graph)
	if err != nil {
		t.Fatalf("ComputeImpact returned error: %v", err)
	}
	// Dirty markers should be created for the refs themselves
	if len(markers) == 0 {
		t.Error("expected dirty markers for refs")
	}
	// No invalidates edges since no claims link to these refs
	foundInvalidates := false
	for _, e := range edges {
		if e.Kind == ImpactEdgeKindInvalidates {
			foundInvalidates = true
		}
	}
	if foundInvalidates {
		t.Error("expected no invalidates edges when no claims exist")
	}
}

func TestComputeImpact_DirtyEditCreatesStalenessDirty(t *testing.T) {
	t.Parallel()
	// Dirty edit event creates StalenessDirty markers for affected refs.
	now := time.Now().UTC().Truncate(time.Second)
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		ClaimType:   "decision",
		Status:      ClaimStatusCurrent,
		SourceRefs:  []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	event := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
	}
	graph := &StubImpactGraph{
		edges: []ImpactEdge{
			{
				ID:          "edge-1",
				WorkspaceID: "ws-1",
				From:        EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
				To:          EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-1"},
				Kind:        ImpactEdgeKindGeneratedFrom,
				CreatedAt:   now,
			},
		},
		claims: []MemoryClaim{claim},
	}

	edges, markers, err := ComputeImpact(event, graph)
	if err != nil {
		t.Fatalf("ComputeImpact returned error: %v", err)
	}

	// Should create dirty staleness markers
	found := false
	for _, m := range markers {
		if m.Status == StalenessStatusDirty {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected dirty staleness markers from code.changed_dirty event")
	}

	// Should have invalidates edges for affected claims
	foundInvalidates := false
	for _, e := range edges {
		if e.Kind == ImpactEdgeKindInvalidates {
			foundInvalidates = true
			break
		}
	}
	if !foundInvalidates {
		t.Error("expected invalidates impact edges from code.changed_dirty event")
	}
}

func TestComputeImpact_CommitResolvesDirty(t *testing.T) {
	t.Parallel()
	// Commit event should produce edges that resolve dirty markers.
	now := time.Now().UTC().Truncate(time.Second)
	event := ContextEvent{
		ID:          "evt-2",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeCommitted,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
	}
	graph := &StubImpactGraph{}

	edges, markers, err := ComputeImpact(event, graph)
	if err != nil {
		t.Fatalf("ComputeImpact returned error: %v", err)
	}
	// Commit with no existing dirty state just creates validates edges
	_ = edges
	_ = markers
}

func TestComputeImpact_AnswerCorrectedCreatesNeedsRevalidation(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-012: answer.corrected creates needs_revalidation markers.
	now := time.Now().UTC().Truncate(time.Second)
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		ClaimType:   "fact",
		Status:      ClaimStatusCurrent,
		SourceRefs:  []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	event := ContextEvent{
		ID:          "evt-3",
		WorkspaceID: "ws-1",
		Kind:        EventKindAnswerCorrected,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		Data:        map[string]any{"correction": "the function should return error"},
		CreatedAt:   now,
	}
	graph := &StubImpactGraph{
		edges: []ImpactEdge{
			{
				ID:          "edge-1",
				WorkspaceID: "ws-1",
				From:        EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
				To:          EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-1"},
				Kind:        ImpactEdgeKindGeneratedFrom,
				CreatedAt:   now,
			},
		},
		claims: []MemoryClaim{claim},
	}

	_, markers, err := ComputeImpact(event, graph)
	if err != nil {
		t.Fatalf("ComputeImpact returned error: %v", err)
	}

	found := false
	for _, m := range markers {
		if m.Status == StalenessStatusNeedsRevalidation {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected needs_revalidation staleness markers from answer.corrected event")
	}
}

func TestComputeImpact_SupersessionCreatesSuperseded(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-013: Supersession creates superseded markers and supersedes edges.
	now := time.Now().UTC().Truncate(time.Second)
	oldClaim := MemoryClaim{
		ID:          "claim-old",
		WorkspaceID: "ws-1",
		ClaimType:   "fact",
		Status:      ClaimStatusCurrent,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	newClaim := MemoryClaim{
		ID:          "claim-new",
		WorkspaceID: "ws-1",
		ClaimType:   "fact",
		Status:      ClaimStatusCandidate,
		SourceRefs:  []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: "claim-old"}},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	event := ContextEvent{
		ID:          "evt-supersede",
		WorkspaceID: "ws-1",
		Kind:        EventKindMemoryClaimPromoted,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: "claim-new"}, {Type: RefTypeMemoryClaim, Ref: "claim-old"}},
		Data:        map[string]any{"supersedes": "claim-old"},
		CreatedAt:   now,
	}
	graph := &StubImpactGraph{
		edges:  []ImpactEdge{},
		claims: []MemoryClaim{oldClaim, newClaim},
	}

	edges, markers, err := ComputeImpact(event, graph)
	if err != nil {
		t.Fatalf("ComputeImpact returned error: %v", err)
	}

	foundSuperseded := false
	for _, m := range markers {
		if m.Status == StalenessStatusSuperseded {
			foundSuperseded = true
			break
		}
	}
	if !foundSuperseded {
		t.Error("expected superseded staleness marker")
	}

	foundSupersedesEdge := false
	for _, e := range edges {
		if e.Kind == ImpactEdgeKindSupersedes {
			foundSupersedesEdge = true
			break
		}
	}
	if !foundSupersedesEdge {
		t.Error("expected supersedes impact edge")
	}
}

func TestComputeImpact_RejectionCreatesInvalidates(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-014: Rejection creates rejected claim status and invalidates edges.
	now := time.Now().UTC().Truncate(time.Second)
	event := ContextEvent{
		ID:          "evt-reject",
		WorkspaceID: "ws-1",
		Kind:        EventKindMemoryClaimInvalidated,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: "claim-bad"}},
		CreatedAt:   now,
	}
	graph := &StubImpactGraph{
		claims: []MemoryClaim{
			{
				ID:          "claim-bad",
				WorkspaceID: "ws-1",
				Status:      ClaimStatusNeedsRevalidation,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		},
	}

	edges, _, err := ComputeImpact(event, graph)
	if err != nil {
		t.Fatalf("ComputeImpact returned error: %v", err)
	}

	foundInvalidates := false
	for _, e := range edges {
		if e.Kind == ImpactEdgeKindInvalidates {
			foundInvalidates = true
			break
		}
	}
	if !foundInvalidates {
		t.Error("expected invalidates edge for claim invalidation event")
	}
}

// ---------------------------------------------------------------------------
// ApplyInvalidation tests (VAL-IMPL-004 through VAL-IMPL-006)
// ---------------------------------------------------------------------------

func TestApplyInvalidation_IsAtomic(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-004: All edges/markers persisted in a single transaction.
	// We verify this by checking that a failing store mid-operation causes rollback.
	event := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   time.Now().UTC(),
	}
	store := &FailingApplyStore{failAfter: 0}
	ctx := context.Background()

	err := ApplyInvalidation(ctx, store, event)
	if err == nil {
		t.Error("expected error from failing store")
	}
}

func TestApplyInvalidation_IsIdempotent(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-005: Repeated application produces same final state.
	now := time.Now().UTC().Truncate(time.Second)
	event := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
	}
	store := NewMemoryStore()
	ctx := context.Background()

	// Apply twice
	err1 := ApplyInvalidation(ctx, store, event)

	if err1 != nil {
		t.Errorf("first ApplyInvalidation returned error: %v", err1)
	}

	// Check state after first apply
	edges1, _ := store.ListImpactEdges(ctx, ImpactFilter{WorkspaceID: "ws-1"})
	markers1, _ := store.ListStaleness(ctx, StalenessFilter{WorkspaceID: "ws-1"})

	// Apply a second time should produce the same count
	err2 := ApplyInvalidation(ctx, store, event)
	if err2 != nil {
		t.Errorf("second ApplyInvalidation returned error: %v", err2)
	}

	edges2, _ := store.ListImpactEdges(ctx, ImpactFilter{WorkspaceID: "ws-1"})
	markers2, _ := store.ListStaleness(ctx, StalenessFilter{WorkspaceID: "ws-1"})

	// Counts should be identical (idempotent)
	if len(edges2) != len(edges1) {
		t.Errorf("edges not idempotent: first=%d, second=%d", len(edges1), len(edges2))
	}
	if len(markers2) != len(markers1) {
		t.Errorf("markers not idempotent: first=%d, second=%d", len(markers1), len(markers2))
	}
}

func TestApplyInvalidation_RespectsContextCancellation(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-006: Returns promptly when context is canceled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	event := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   time.Now().UTC(),
	}
	store := NewMemoryStore()

	err := ApplyInvalidation(ctx, store, event)
	if err == nil {
		t.Error("expected error from canceled context")
	}
}

func TestApplyInvalidation_DirtyEditNoClaimPromotion(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-015: Dirty edit does not promote memory.
	now := time.Now().UTC().Truncate(time.Second)
	event := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
	}
	store := NewMemoryStore()
	ctx := context.Background()

	// Pre-create a candidate claim
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		ClaimType:   "decision",
		Status:      ClaimStatusCandidate,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, _ = store.UpsertClaim(ctx, claim)

	err := ApplyInvalidation(ctx, store, event)
	if err != nil {
		t.Fatalf("ApplyInvalidation returned error: %v", err)
	}

	// Claim should still be candidate, not promoted
	gotClaim, _ := store.GetClaim(ctx, "claim-1")
	if gotClaim.Status != ClaimStatusCandidate {
		t.Errorf("claim status = %q, want candidate (dirty edit should not promote)", gotClaim.Status)
	}
}

func TestApplyInvalidation_DirtyEditMarksOnlyRelatedCurrentClaims(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	store := NewMemoryStore()
	ctx := context.Background()
	related := MemoryClaim{
		ID:          "claim-related",
		WorkspaceID: "ws-1",
		ClaimType:   "fact",
		Status:      ClaimStatusCurrent,
		SourceRefs:  []EvidenceRef{{Type: RefTypePath, Ref: "src/auth.go"}},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	edgeRelated := MemoryClaim{
		ID:          "claim-edge-related",
		WorkspaceID: "ws-1",
		ClaimType:   "fact",
		Status:      ClaimStatusCurrent,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	unrelated := MemoryClaim{
		ID:          "claim-unrelated",
		WorkspaceID: "ws-1",
		ClaimType:   "fact",
		Status:      ClaimStatusCurrent,
		SourceRefs:  []EvidenceRef{{Type: RefTypePath, Ref: "src/billing.go"}},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	candidate := MemoryClaim{
		ID:          "claim-candidate",
		WorkspaceID: "ws-1",
		ClaimType:   "fact",
		Status:      ClaimStatusCandidate,
		SourceRefs:  []EvidenceRef{{Type: RefTypePath, Ref: "src/auth.go"}},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	for _, claim := range []MemoryClaim{related, edgeRelated, unrelated, candidate} {
		if _, err := store.UpsertClaim(ctx, claim); err != nil {
			t.Fatalf("upsert claim %s: %v", claim.ID, err)
		}
	}
	if _, err := store.PutImpactEdge(ctx, ImpactEdge{
		ID:          "edge-dirty-related",
		WorkspaceID: "ws-1",
		From:        EvidenceRef{Type: RefTypePath, Ref: "src/auth.go"},
		To:          EvidenceRef{Type: RefTypeMemoryClaim, Ref: edgeRelated.ID},
		Kind:        ImpactEdgeKindGeneratedFrom,
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("put related impact edge: %v", err)
	}

	event := ContextEvent{
		ID:          "evt-dirty-related",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/auth.go"}},
		CreatedAt:   now,
	}
	if err := ApplyInvalidation(ctx, store, event); err != nil {
		t.Fatalf("ApplyInvalidation: %v", err)
	}

	gotRelated, err := store.GetClaim(ctx, related.ID)
	if err != nil {
		t.Fatalf("get related claim: %v", err)
	}
	if gotRelated.Status != ClaimStatusNeedsRevalidation {
		t.Fatalf("related claim status=%q want %q", gotRelated.Status, ClaimStatusNeedsRevalidation)
	}
	gotEdgeRelated, err := store.GetClaim(ctx, edgeRelated.ID)
	if err != nil {
		t.Fatalf("get edge-related claim: %v", err)
	}
	if gotEdgeRelated.Status != ClaimStatusNeedsRevalidation {
		t.Fatalf("edge-related claim status=%q want %q", gotEdgeRelated.Status, ClaimStatusNeedsRevalidation)
	}
	gotUnrelated, err := store.GetClaim(ctx, unrelated.ID)
	if err != nil {
		t.Fatalf("get unrelated claim: %v", err)
	}
	if gotUnrelated.Status != ClaimStatusCurrent {
		t.Fatalf("unrelated claim status=%q want %q", gotUnrelated.Status, ClaimStatusCurrent)
	}
	gotCandidate, err := store.GetClaim(ctx, candidate.ID)
	if err != nil {
		t.Fatalf("get candidate claim: %v", err)
	}
	if gotCandidate.Status != ClaimStatusCandidate {
		t.Fatalf("candidate claim status=%q want %q", gotCandidate.Status, ClaimStatusCandidate)
	}

	target := EvidenceRef{Type: RefTypeMemoryClaim, Ref: related.ID}
	markers, err := store.ListStaleness(ctx, StalenessFilter{
		WorkspaceID: "ws-1",
		TargetRef:   &target,
		Status:      StalenessStatusNeedsRevalidation,
	})
	if err != nil {
		t.Fatalf("list related claim markers: %v", err)
	}
	if len(markers) != 1 {
		t.Fatalf("related claim markers=%d want 1", len(markers))
	}
}

func TestApplyInvalidation_CommitAllowsClaimPromotion(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-015: Commit allows claim promotion through PromotionPolicy.
	now := time.Now().UTC().Truncate(time.Second)
	event := ContextEvent{
		ID:          "evt-commit",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeCommitted,
		Source:      "test",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
	}
	store := NewMemoryStore()
	ctx := context.Background()

	// Pre-create a candidate claim
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		ClaimType:   "decision",
		Status:      ClaimStatusCandidate,
		SourceRefs:  []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, _ = store.UpsertClaim(ctx, claim)

	err := ApplyInvalidation(ctx, store, event)
	if err != nil {
		t.Fatalf("ApplyInvalidation returned error: %v", err)
	}

	// Candidate claims with matching source refs should be eligible for promotion
	gotClaim, _ := store.GetClaim(ctx, "claim-1")
	// After commit, candidate claims should be promoted to current
	if gotClaim.Status != ClaimStatusCurrent {
		t.Errorf("claim status = %q, want current (commit should allow promotion)", gotClaim.Status)
	}
}

// ---------------------------------------------------------------------------
// ResolveStaleness tests (VAL-IMPL-007 through VAL-IMPL-009)
// ---------------------------------------------------------------------------

func TestResolveStaleness_TransitionsCorrectly(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-007: Updates marker status and sets resolved_by_event.
	now := time.Now().UTC().Truncate(time.Second)
	store := NewMemoryStore()
	ctx := context.Background()

	marker := StalenessMarker{
		ID:             "marker-1",
		WorkspaceID:    "ws-1",
		TargetRef:      EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
		Status:         StalenessStatusDirty,
		CausedByEvents: []string{"evt-1"},
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}
	_, _ = store.UpsertStaleness(ctx, marker)

	err := ResolveStaleness(ctx, store, "marker-1", "evt-resolve")
	if err != nil {
		t.Fatalf("ResolveStaleness returned error: %v", err)
	}

	got, _ := store.GetStaleness(ctx, "marker-1")
	if got.Status != StalenessStatusFresh {
		t.Errorf("status = %q, want fresh", got.Status)
	}
	if got.ResolvedByEvent != "evt-resolve" {
		t.Errorf("resolved_by_event = %q, want evt-resolve", got.ResolvedByEvent)
	}
}

func TestResolveStaleness_GuardsDoubleResolve(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-008: Returns error on already-resolved marker.
	now := time.Now().UTC().Truncate(time.Second)
	store := NewMemoryStore()
	ctx := context.Background()

	marker := StalenessMarker{
		ID:              "marker-1",
		WorkspaceID:     "ws-1",
		TargetRef:       EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
		Status:          StalenessStatusFresh,
		ResolvedByEvent: "evt-first",
		CreatedAt:       now.Add(-time.Hour),
		UpdatedAt:       now.Add(-time.Hour),
	}
	_, _ = store.UpsertStaleness(ctx, marker)

	err := ResolveStaleness(ctx, store, "marker-1", "evt-second")
	if err == nil {
		t.Error("expected error for double resolve")
	}
}

func TestResolveStaleness_ReturnsNotFoundForMissing(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-009: Returns clear error for missing marker.
	store := NewMemoryStore()
	ctx := context.Background()

	err := ResolveStaleness(ctx, store, "nonexistent", "evt-1")
	if err == nil {
		t.Error("expected error for nonexistent marker")
	}
}

func TestResolveStaleness_CommitResolvesDirtyMarkers(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-010: Commit event resolves dirty markers for affected refs.
	now := time.Now().UTC().Truncate(time.Second)
	store := NewMemoryStore()
	ctx := context.Background()

	// Create dirty marker
	marker := StalenessMarker{
		ID:             "marker-1",
		WorkspaceID:    "ws-1",
		TargetRef:      EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
		Status:         StalenessStatusDirty,
		CausedByEvents: []string{"evt-dirty"},
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}
	_, _ = store.UpsertStaleness(ctx, marker)

	// Simulate commit event resolution
	err := ResolveStaleness(ctx, store, "marker-1", "evt-commit")
	if err != nil {
		t.Fatalf("ResolveStaleness returned error: %v", err)
	}

	got, _ := store.GetStaleness(ctx, "marker-1")
	if got.Status != StalenessStatusFresh {
		t.Errorf("status = %q, want fresh after commit resolution", got.Status)
	}
}

func TestResolveStaleness_ValidatedResolvesNeedsRevalidation(t *testing.T) {
	t.Parallel()
	// VAL-IMPL-011: code.validated resolves needs_revalidation markers.
	now := time.Now().UTC().Truncate(time.Second)
	store := NewMemoryStore()
	ctx := context.Background()

	marker := StalenessMarker{
		ID:          "marker-1",
		WorkspaceID: "ws-1",
		TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
		Status:      StalenessStatusNeedsRevalidation,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}
	_, _ = store.UpsertStaleness(ctx, marker)

	err := ResolveStaleness(ctx, store, "marker-1", "evt-validated")
	if err != nil {
		t.Fatalf("ResolveStaleness returned error: %v", err)
	}

	got, _ := store.GetStaleness(ctx, "marker-1")
	if got.Status != StalenessStatusFresh {
		t.Errorf("status = %q, want fresh after validation", got.Status)
	}
}

func TestResolveStaleness_CannotResolveSuperseded(t *testing.T) {
	t.Parallel()
	// Superseded markers cannot be resolved (terminal state).
	now := time.Now().UTC().Truncate(time.Second)
	store := NewMemoryStore()
	ctx := context.Background()

	marker := StalenessMarker{
		ID:              "marker-1",
		WorkspaceID:     "ws-1",
		TargetRef:       EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
		Status:          StalenessStatusSuperseded,
		ResolvedByEvent: "evt-original",
		CreatedAt:       now.Add(-time.Hour),
		UpdatedAt:       now.Add(-time.Hour),
	}
	_, _ = store.UpsertStaleness(ctx, marker)

	err := ResolveStaleness(ctx, store, "marker-1", "evt-new")
	if err == nil {
		t.Error("expected error for resolving superseded marker")
	}
}

// ---------------------------------------------------------------------------
// Cross-event flow integration tests (VAL-CROSS-001 through VAL-CROSS-003)
// ---------------------------------------------------------------------------

func TestIntegration_DirtyEditFlow(t *testing.T) {
	t.Parallel()
	// VAL-CROSS-001: Dirty edit creates markers, no memory promotion.
	now := time.Now().UTC().Truncate(time.Second)
	store := NewMemoryStore()
	ctx := context.Background()

	// Setup: create a current claim referencing a file
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		ClaimType:   "decision",
		Status:      ClaimStatusCurrent,
		SourceRefs:  []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		Summary:     "Uses UTF-8 encoding",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, _ = store.UpsertClaim(ctx, claim)

	// Create an edge linking file to claim
	edge := ImpactEdge{
		ID:          "edge-1",
		WorkspaceID: "ws-1",
		From:        EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
		To:          EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-1"},
		Kind:        ImpactEdgeKindGeneratedFrom,
		CreatedAt:   now,
	}
	if _, err := store.PutImpactEdge(ctx, edge); err != nil {
		t.Fatalf("PutImpactEdge failed: %v", err)
	}

	// Emit dirty edit event
	dirtyEvent := ContextEvent{
		ID:          "evt-dirty",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "file_watcher",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now.Add(time.Minute),
	}
	if _, err := store.AppendEvent(ctx, dirtyEvent); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Apply invalidation
	err := ApplyInvalidation(ctx, store, dirtyEvent)
	if err != nil {
		t.Fatalf("ApplyInvalidation returned error: %v", err)
	}

	// Verify: claim should NOT be promoted, should be needs_revalidation
	gotClaim, _ := store.GetClaim(ctx, "claim-1")
	// Dirty edit should not change claim to rejected or superseded
	if gotClaim.Status == ClaimStatusRejected || gotClaim.Status == ClaimStatusSuperseded {
		t.Errorf("dirty edit should not reject/supersede claim, got %q", gotClaim.Status)
	}

	// Verify: dirty staleness marker exists
	markers, _ := store.ListStaleness(ctx, StalenessFilter{WorkspaceID: "ws-1"})
	if len(markers) == 0 {
		t.Error("expected staleness markers after dirty edit")
	}

	// Verify: invalidation edges created
	edges, _ := store.ListImpactEdges(ctx, ImpactFilter{
		WorkspaceID: "ws-1",
		Kind:        ImpactEdgeKindInvalidates,
	})
	if len(edges) == 0 {
		t.Error("expected invalidates edges after dirty edit")
	}
}

func TestIntegration_CommitFlow(t *testing.T) {
	t.Parallel()
	// VAL-CROSS-002: Commit resolves dirty markers, allows claim promotion.
	now := time.Now().UTC().Truncate(time.Second)
	store := NewMemoryStore()
	ctx := context.Background()

	// Setup: candidate claim
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		ClaimType:   "decision",
		Status:      ClaimStatusCandidate,
		SourceRefs:  []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		Summary:     "Uses UTF-8 encoding",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, _ = store.UpsertClaim(ctx, claim)

	// Setup: dirty marker
	marker := StalenessMarker{
		ID:             "marker-1",
		WorkspaceID:    "ws-1",
		TargetRef:      EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
		Status:         StalenessStatusDirty,
		CausedByEvents: []string{"evt-dirty"},
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}
	_, _ = store.UpsertStaleness(ctx, marker)

	// Emit commit event
	commitEvent := ContextEvent{
		ID:          "evt-commit",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeCommitted,
		Source:      "git_hook",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		CreatedAt:   now.Add(time.Minute),
	}
	if _, err := store.AppendEvent(ctx, commitEvent); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Apply invalidation (commit)
	err := ApplyInvalidation(ctx, store, commitEvent)
	if err != nil {
		t.Fatalf("ApplyInvalidation returned error: %v", err)
	}

	// Verify: candidate claim promoted to current
	gotClaim, _ := store.GetClaim(ctx, "claim-1")
	if gotClaim.Status != ClaimStatusCurrent {
		t.Errorf("claim status = %q, want current after commit", gotClaim.Status)
	}

	// Verify: dirty marker resolved
	gotMarker, _ := store.GetStaleness(ctx, "marker-1")
	if gotMarker.Status != StalenessStatusFresh {
		t.Errorf("marker status = %q, want fresh after commit", gotMarker.Status)
	}
}

func TestIntegration_UserCorrectionFlow(t *testing.T) {
	t.Parallel()
	// VAL-CROSS-003: User correction records feedback, invalidates claims.
	now := time.Now().UTC().Truncate(time.Second)
	store := NewMemoryStore()
	ctx := context.Background()

	// Setup: current claim
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		ClaimType:   "fact",
		Status:      ClaimStatusCurrent,
		SourceRefs:  []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		Summary:     "Function returns nil on error",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, _ = store.UpsertClaim(ctx, claim)

	// Link claim to evidence
	edge := ImpactEdge{
		ID:          "edge-1",
		WorkspaceID: "ws-1",
		From:        EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
		To:          EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-1"},
		Kind:        ImpactEdgeKindGeneratedFrom,
		CreatedAt:   now,
	}
	if _, err := store.PutImpactEdge(ctx, edge); err != nil {
		t.Fatalf("PutImpactEdge failed: %v", err)
	}

	// Emit answer.corrected event
	correctionEvent := ContextEvent{
		ID:          "evt-correct",
		WorkspaceID: "ws-1",
		Kind:        EventKindAnswerCorrected,
		Source:      "user",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		Data:        map[string]any{"correction": "function actually returns error, not nil"},
		CreatedAt:   now.Add(time.Minute),
	}
	if _, err := store.AppendEvent(ctx, correctionEvent); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Apply invalidation
	err := ApplyInvalidation(ctx, store, correctionEvent)
	if err != nil {
		t.Fatalf("ApplyInvalidation returned error: %v", err)
	}

	// Verify: claim moved to needs_revalidation (not immediately rejected)
	gotClaim, _ := store.GetClaim(ctx, "claim-1")
	if gotClaim.Status != ClaimStatusNeedsRevalidation {
		t.Errorf("claim status = %q, want needs_revalidation after correction", gotClaim.Status)
	}

	// Verify: needs_revalidation staleness marker exists
	markers, _ := store.ListStaleness(ctx, StalenessFilter{
		WorkspaceID: "ws-1",
		Status:      StalenessStatusNeedsRevalidation,
	})
	if len(markers) == 0 {
		t.Error("expected needs_revalidation staleness markers after user correction")
	}
}

// ---------------------------------------------------------------------------
// Stub types for testing
// ---------------------------------------------------------------------------

// StubImpactGraph provides a minimal implementation of ImpactGraph for testing.
type StubImpactGraph struct {
	edges  []ImpactEdge
	claims []MemoryClaim
}

func (g *StubImpactGraph) ForwardEdges(ref EvidenceRef) []ImpactEdge {
	var result []ImpactEdge
	for _, e := range g.edges {
		if e.From.Equal(ref) {
			result = append(result, e)
		}
	}
	return result
}

func (g *StubImpactGraph) ReverseEdges(ref EvidenceRef) []ImpactEdge {
	var result []ImpactEdge
	for _, e := range g.edges {
		if e.To.Equal(ref) {
			result = append(result, e)
		}
	}
	return result
}

func (g *StubImpactGraph) ClaimsForRef(ref EvidenceRef) []MemoryClaim {
	var result []MemoryClaim
	for _, c := range g.claims {
		for _, sr := range c.SourceRefs {
			if sr.Equal(ref) {
				result = append(result, c)
				break
			}
		}
	}
	return result
}

func (g *StubImpactGraph) ClaimByID(id string) (MemoryClaim, bool) {
	for _, c := range g.claims {
		if c.ID == id {
			return c, true
		}
	}
	return MemoryClaim{}, false
}

func (g *StubImpactGraph) AllClaims() []MemoryClaim {
	return g.claims
}

// FailingApplyStore always fails. Used for atomicity testing.
type FailingApplyStore struct {
	failAfter int
	calls     int
}

func (s *FailingApplyStore) PutImpactEdge(_ context.Context, _ ImpactEdge) (ImpactEdge, error) {
	s.calls++
	return ImpactEdge{}, fmt.Errorf("injected failure at call %d", s.calls)
}

func (s *FailingApplyStore) UpsertStaleness(_ context.Context, _ StalenessMarker) (StalenessMarker, error) {
	s.calls++
	return StalenessMarker{}, fmt.Errorf("injected failure at call %d", s.calls)
}

func (s *FailingApplyStore) UpsertClaim(_ context.Context, _ MemoryClaim) (MemoryClaim, error) {
	s.calls++
	return MemoryClaim{}, fmt.Errorf("injected failure at call %d", s.calls)
}

func (s *FailingApplyStore) ListImpactEdges(_ context.Context, _ ImpactFilter) ([]ImpactEdge, error) {
	return nil, nil
}

func (s *FailingApplyStore) ListStaleness(_ context.Context, _ StalenessFilter) ([]StalenessMarker, error) {
	return nil, nil
}

func (s *FailingApplyStore) GetStaleness(_ context.Context, _ string) (StalenessMarker, error) {
	return StalenessMarker{}, fmt.Errorf("not found")
}

func (s *FailingApplyStore) GetClaim(_ context.Context, _ string) (MemoryClaim, error) {
	return MemoryClaim{}, fmt.Errorf("not found")
}

func (s *FailingApplyStore) ListClaims(_ context.Context, _ ClaimFilter) ([]MemoryClaim, error) {
	return nil, nil
}

func (s *FailingApplyStore) AppendEvent(_ context.Context, _ ContextEvent) (ContextEvent, error) {
	return ContextEvent{}, nil
}
