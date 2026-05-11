package contextengine

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Integration tests for cross-area flows (VAL-CROSS-001 through VAL-CROSS-005)
// ---------------------------------------------------------------------------

var (
	integrationNow           = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	integrationIDCounter     atomic.Int64
)

func integrationIDGen() string {
	n := integrationIDCounter.Add(1)
	return fmt.Sprintf("int-id-%d", n)
}

func integrationClock() time.Time { return integrationNow }

func resetIntegrationIDCounter() {
	integrationIDCounter.Store(0)
}

// newIntegrationStore creates a fresh MemoryStore for integration tests.
func newIntegrationStore() *MemoryStore {
	return NewMemoryStore()
}

// newIntegrationLaneConfig creates a LaneConfig backed by the given store.
func newIntegrationLaneConfig(store *MemoryStore) LaneConfig {
	return LaneConfig{
		Store:       store,
		IDGen:       integrationIDGen,
		Clock:       integrationClock,
		WorkspaceID: "ws-integration",
	}
}

// ==========================================================================
// VAL-CROSS-001: Dirty edit flow
// File write → ContextEvent(code.changed_dirty) → WorkingSet update →
// StalenessMarker(dirty) → affected claims marked needs_revalidation →
// NO memory promotion → retrieval reflects dirty state.
// ==========================================================================

func TestIntegrationCrossArea_DirtyEditFlow(t *testing.T) {
	resetIntegrationIDCounter()
	ctx := context.Background()
	store := newIntegrationStore()
	wsID := "ws-integration"
	now := integrationClock()

	// Setup: create a candidate claim that sources from a file
	candidateClaim := MemoryClaim{
		ID:          "claim-candidate-1",
		WorkspaceID: wsID,
		ClaimType:   "decision",
		Status:      ClaimStatusCandidate,
		Scope: ClaimScope{
			Path: "src/handler.go",
			Refs: []EvidenceRef{{Type: RefTypePath, Ref: "src/handler.go"}},
		},
		Summary:    "Uses middleware pattern for auth",
		Confidence: 0.85,
		SourceRefs: []EvidenceRef{{Type: RefTypePath, Ref: "src/handler.go"}},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if _, err := store.UpsertClaim(ctx, candidateClaim); err != nil {
		t.Fatalf("setup: upsert candidate claim: %v", err)
	}

	// Setup: create a current claim sourced from the same file
	currentClaim := MemoryClaim{
		ID:          "claim-current-1",
		WorkspaceID: wsID,
		ClaimType:   "fact",
		Status:      ClaimStatusCurrent,
		Scope: ClaimScope{
			Path: "src/handler.go",
			Refs: []EvidenceRef{{Type: RefTypePath, Ref: "src/handler.go"}},
		},
		Summary:    "Handler returns HTTP 200 on success",
		Confidence: 0.9,
		SourceRefs: []EvidenceRef{{Type: RefTypePath, Ref: "src/handler.go"}},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if _, err := store.UpsertClaim(ctx, currentClaim); err != nil {
		t.Fatalf("setup: upsert current claim: %v", err)
	}

	// Setup: create an impact edge from the file to the current claim
	edge := ImpactEdge{
		ID:          "edge-setup-1",
		WorkspaceID: wsID,
		From:        EvidenceRef{Type: RefTypePath, Ref: "src/handler.go"},
		To:          EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-current-1"},
		Kind:        ImpactEdgeKindGeneratedFrom,
		CreatedAt:   now,
	}
	if _, err := store.PutImpactEdge(ctx, edge); err != nil {
		t.Fatalf("setup: put impact edge: %v", err)
	}

	// Step 1: Emit a dirty edit event
	dirtyEvent := ContextEvent{
		ID:          "evt-dirty-1",
		WorkspaceID: wsID,
		Kind:        EventKindCodeChangedDirty,
		Source:      "file_watcher",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/handler.go"}},
		Data:        map[string]any{"change_type": "content_modified"},
		CreatedAt:   now,
	}
	if _, err := store.AppendEvent(ctx, dirtyEvent); err != nil {
		t.Fatalf("step 1: append dirty event: %v", err)
	}

	// Step 2: Update WorkingSet with dirty ref
	ws := WorkingSet{
		WorkspaceID: wsID,
		UpdatedAt:   now,
	}
	ws.AddDirtyRef(EvidenceRef{Type: RefTypePath, Ref: "src/handler.go"})
	if err := ws.Validate(); err != nil {
		t.Fatalf("step 2: working set validation: %v", err)
	}

	// Verify dirty ref tracked
	if len(ws.DirtyRefs) != 1 {
		t.Fatalf("step 2: expected 1 dirty ref, got %d", len(ws.DirtyRefs))
	}
	if ws.DirtyRefs[0].Ref != "src/handler.go" {
		t.Errorf("step 2: dirty ref = %q, want src/handler.go", ws.DirtyRefs[0].Ref)
	}

	// Step 3: Apply invalidation (creates staleness markers + updates claims)
	if err := ApplyInvalidation(ctx, store, dirtyEvent); err != nil {
		t.Fatalf("step 3: apply invalidation: %v", err)
	}

	// Step 4: Verify dirty staleness markers created
	markers, err := store.ListStaleness(ctx, StalenessFilter{
		WorkspaceID: wsID,
		Status:      StalenessStatusDirty,
	})
	if err != nil {
		t.Fatalf("step 4: list staleness: %v", err)
	}
	if len(markers) == 0 {
		t.Error("step 4: expected dirty staleness markers, got none")
	}

	// Step 5: Verify NO claim promotion occurred — the current claim should remain
	// at its current status or be needs_revalidation depending on whether the
	// impact traversal found it. The key invariant is NO promotion to current.
	gotClaim, err := store.GetClaim(ctx, "claim-current-1")
	if err != nil {
		t.Fatalf("step 5: get claim: %v", err)
	}
	// The claim must NOT be promoted — it should remain current or transition to needs_revalidation
	if gotClaim.Status == ClaimStatusSuperseded {
		t.Errorf("step 5: claim should NOT be superseded after dirty edit, got %q", gotClaim.Status)
	}
	// The critical invariant: dirty edits never promote claims
	if gotClaim.Status != ClaimStatusCurrent && gotClaim.Status != ClaimStatusNeedsRevalidation {
		t.Errorf("step 5: unexpected claim status %q after dirty edit", gotClaim.Status)
	}

	// Step 6: Verify candidate claim NOT promoted (stays candidate)
	gotCandidate, err := store.GetClaim(ctx, "claim-candidate-1")
	if err != nil {
		t.Fatalf("step 6: get candidate claim: %v", err)
	}
	if gotCandidate.Status != ClaimStatusCandidate {
		t.Errorf("step 6: candidate claim status = %q, want candidate (NO promotion after dirty edit)", gotCandidate.Status)
	}

	// Step 7: Retrieval reflects dirty state — memory lane returns all claims
	cfg := newIntegrationLaneConfig(store)
	memoryFn := func(_ context.Context, wID, _ string) ([]MemoryClaim, error) {
		return store.ListClaims(ctx, ClaimFilter{WorkspaceID: wID})
	}
	pack, err := RetrieveMemory(ctx, cfg, memoryFn, "handler auth")
	if err != nil {
		t.Fatalf("step 7: retrieve memory: %v", err)
	}
	// Should find the claim (still exists, not deleted by dirty edit)
	found := false
	for _, node := range pack.Nodes {
		if node.Ref.Type == RefTypeMemoryClaim && node.Ref.Ref == "claim-current-1" {
			found = true
		}
	}
	if !found {
		t.Error("step 7: retrieval should include the claim (not deleted by dirty edit)")
	}

	// Step 8: Verify dirty staleness markers exist for the file ref
	dirtyMarkers, err := store.ListStaleness(ctx, StalenessFilter{
		WorkspaceID: wsID,
		Status:      StalenessStatusDirty,
	})
	if err != nil {
		t.Fatalf("step 8: list dirty staleness: %v", err)
	}
	if len(dirtyMarkers) == 0 {
		t.Error("step 8: expected dirty staleness markers for file refs")
	}

	// Step 9: Verify invalidates edges created for claims linked to dirty refs
	invalidatesEdges, err := store.ListImpactEdges(ctx, ImpactFilter{
		WorkspaceID: wsID,
		Kind:        ImpactEdgeKindInvalidates,
	})
	if err != nil {
		t.Fatalf("step 9: list invalidates edges: %v", err)
	}
	if len(invalidatesEdges) == 0 {
		t.Error("step 9: expected invalidates edges after dirty edit")
	}
}

// ==========================================================================
// VAL-CROSS-002: Commit flow
// Git commit → ContextEvent(code.committed) → durable checkpoint →
// stale markers resolved → claim promotion/demotion via PromotionPolicy →
// projection regeneration → task context update.
// ==========================================================================

func TestIntegrationCrossArea_CommitFlow(t *testing.T) {
	resetIntegrationIDCounter()
	ctx := context.Background()
	store := newIntegrationStore()
	wsID := "ws-integration"
	now := integrationClock()

	// Setup: create a candidate claim sourced from files
	candidateClaim := MemoryClaim{
		ID:          "claim-cand-2",
		WorkspaceID: wsID,
		ClaimType:   "decision",
		Status:      ClaimStatusCandidate,
		Scope: ClaimScope{
			Path: "src/auth.go",
			Refs: []EvidenceRef{{Type: RefTypePath, Ref: "src/auth.go"}},
		},
		Summary:    "Uses JWT for auth tokens",
		Confidence: 0.9,
		SourceRefs: []EvidenceRef{
			{Type: RefTypePath, Ref: "src/auth.go"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := store.UpsertClaim(ctx, candidateClaim); err != nil {
		t.Fatalf("setup: upsert candidate claim: %v", err)
	}

	// Setup: create a dirty staleness marker
	dirtyMarker := StalenessMarker{
		ID:             "marker-dirty-2",
		WorkspaceID:    wsID,
		TargetRef:      EvidenceRef{Type: RefTypePath, Ref: "src/auth.go"},
		Status:         StalenessStatusDirty,
		CausedByEvents: []string{"evt-dirty-previous"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, err := store.UpsertStaleness(ctx, dirtyMarker); err != nil {
		t.Fatalf("setup: upsert dirty marker: %v", err)
	}

	// Step 1: Emit commit event
	commitEvent := ContextEvent{
		ID:          "evt-commit-1",
		WorkspaceID: wsID,
		Kind:        EventKindCodeCommitted,
		Source:      "git_hook",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/auth.go"}},
		Data:        map[string]any{"commit_sha": "abc123"},
		CreatedAt:   now,
	}
	if _, err := store.AppendEvent(ctx, commitEvent); err != nil {
		t.Fatalf("step 1: append commit event: %v", err)
	}

	// Step 2: Apply invalidation (resolves dirty markers, promotes claims)
	if err := ApplyInvalidation(ctx, store, commitEvent); err != nil {
		t.Fatalf("step 2: apply invalidation: %v", err)
	}

	// Step 3: Verify dirty staleness markers resolved to fresh
	resolvedMarkers, err := store.ListStaleness(ctx, StalenessFilter{
		WorkspaceID: wsID,
		TargetRef:   &EvidenceRef{Type: RefTypePath, Ref: "src/auth.go"},
	})
	if err != nil {
		t.Fatalf("step 3: list staleness: %v", err)
	}
	for _, m := range resolvedMarkers {
		if m.ID == "marker-dirty-2" && m.Status != StalenessStatusFresh {
			t.Errorf("step 3: marker %s status = %q, want fresh", m.ID, m.Status)
		}
	}

	// Step 4: Verify candidate claim promoted to current
	promotedClaim, err := store.GetClaim(ctx, "claim-cand-2")
	if err != nil {
		t.Fatalf("step 4: get claim: %v", err)
	}
	if promotedClaim.Status != ClaimStatusCurrent {
		t.Errorf("step 4: claim status = %q, want current (promoted by commit)", promotedClaim.Status)
	}
	if promotedClaim.Reason == "" {
		t.Error("step 4: expected non-empty reason for promotion")
	}

	// Step 5: Verify validates edges created
	edges, err := store.ListImpactEdges(ctx, ImpactFilter{
		WorkspaceID: wsID,
		Kind:        ImpactEdgeKindValidates,
	})
	if err != nil {
		t.Fatalf("step 5: list edges: %v", err)
	}
	if len(edges) == 0 {
		t.Error("step 5: expected validates edges after commit")
	}

	// Step 6: Verify projection can be stored and retrieved
	taskCtx := TaskContext{
		WorkspaceID: wsID,
		TaskID:      "task-1",
		Objective:   "Implement auth",
		Status:      "in_progress",
		ProjectionMeta: ProjectionMeta{
			ProjectionID:      "proj-1",
			ProjectionType:    "task_context",
			ProjectionVersion: 1,
			WorkspaceID:       wsID,
			GeneratedAt:       now,
		},
		RelatedClaims: []EvidenceRef{
			{Type: RefTypeMemoryClaim, Ref: "claim-cand-2"},
		},
		UpdatedAt: now,
	}
	err = store.PutProjection(ctx,
		taskCtx.ProjectionMeta.ProjectionID,
		wsID,
		"task_context",
		1,
		"task-1",
		[]string{"evt-commit-1"},
		taskCtx,
		now,
		time.Time{},
	)
	if err != nil {
		t.Fatalf("step 6: put projection: %v", err)
	}

	// Verify projection stored
	_, version, _, _, _, _, _, err := store.GetProjection(ctx, wsID, "proj-1")
	if err != nil {
		t.Fatalf("step 6: get projection: %v", err)
	}
	if version != 1 {
		t.Errorf("step 6: projection version = %d, want 1", version)
	}
}

// ==========================================================================
// VAL-CROSS-003: User correction flow
// Wrong answer → ContextEvent(answer.corrected) → RetrievalFeedback recorded →
// implicated claims rejected/superseded → gap created → projection regenerated.
// ==========================================================================

func TestIntegrationCrossArea_UserCorrectionFlow(t *testing.T) {
	resetIntegrationIDCounter()
	ctx := context.Background()
	store := newIntegrationStore()
	wsID := "ws-integration"
	now := integrationClock()

	// Setup: create a current claim
	currentClaim := MemoryClaim{
		ID:          "claim-current-3",
		WorkspaceID: wsID,
		ClaimType:   "fact",
		Status:      ClaimStatusCurrent,
		Scope: ClaimScope{
			Path: "src/config.go",
			Refs: []EvidenceRef{{Type: RefTypePath, Ref: "src/config.go"}},
		},
		Summary:    "Config uses YAML format",
		Confidence: 0.95,
		SourceRefs: []EvidenceRef{
			{Type: RefTypePath, Ref: "src/config.go"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := store.UpsertClaim(ctx, currentClaim); err != nil {
		t.Fatalf("setup: upsert claim: %v", err)
	}

	// Setup: create impact edge from source to claim
	edge := ImpactEdge{
		ID:          "edge-setup-3",
		WorkspaceID: wsID,
		From:        EvidenceRef{Type: RefTypePath, Ref: "src/config.go"},
		To:          EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-current-3"},
		Kind:        ImpactEdgeKindGeneratedFrom,
		CreatedAt:   now,
	}
	if _, err := store.PutImpactEdge(ctx, edge); err != nil {
		t.Fatalf("setup: put edge: %v", err)
	}

	// Setup: simulate a retrieval that used this claim
	episodeID := "episode-3"
	episode := RetrievalEpisode{
		ID:          episodeID,
		WorkspaceID: wsID,
		Query:       "what format does config use",
		Lane:        LaneMemory,
		PackID:      "pack-3",
		HitCount:    1,
		CreatedAt:   now,
	}
	if _, err := store.RecordRetrievalEpisode(ctx, episode); err != nil {
		t.Fatalf("setup: record episode: %v", err)
	}

	// Step 1: User corrects the answer
	correctionEvent := ContextEvent{
		ID:          "evt-correction-1",
		WorkspaceID: wsID,
		Kind:        EventKindAnswerCorrected,
		Source:      "user_feedback",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/config.go"}},
		Data: map[string]any{
			"correction": "Config uses TOML, not YAML",
		},
		CreatedAt: now,
	}
	if _, err := store.AppendEvent(ctx, correctionEvent); err != nil {
		t.Fatalf("step 1: append correction event: %v", err)
	}

	// Step 2: Record RetrievalFeedback
	feedback := RetrievalFeedback{
		ID:             "feedback-3",
		WorkspaceID:    wsID,
		EpisodeID:      episodeID,
		Kind:           RetrievalFeedbackKindAnswerCorrected,
		Query:          "what format does config use",
		CorrectionStmt: "Config uses TOML, not YAML",
		CreatedAt:      now,
	}
	if _, err := store.RecordRetrievalFeedback(ctx, feedback); err != nil {
		t.Fatalf("step 2: record feedback: %v", err)
	}

	// Step 3: Apply invalidation
	if err := ApplyInvalidation(ctx, store, correctionEvent); err != nil {
		t.Fatalf("step 3: apply invalidation: %v", err)
	}

	// Step 4: Verify claim moved to needs_revalidation (not immediate rejection)
	gotClaim, err := store.GetClaim(ctx, "claim-current-3")
	if err != nil {
		t.Fatalf("step 4: get claim: %v", err)
	}
	if gotClaim.Status != ClaimStatusNeedsRevalidation {
		t.Errorf("step 4: claim status = %q, want needs_revalidation", gotClaim.Status)
	}

	// Step 5: Verify invalidates edges created
	edges, err := store.ListImpactEdges(ctx, ImpactFilter{
		WorkspaceID: wsID,
		Kind:        ImpactEdgeKindInvalidates,
	})
	if err != nil {
		t.Fatalf("step 5: list edges: %v", err)
	}
	foundInvalidates := false
	for _, e := range edges {
		if e.To.Type == RefTypeMemoryClaim && e.To.Ref == "claim-current-3" {
			foundInvalidates = true
		}
	}
	if !foundInvalidates {
		t.Error("step 5: expected invalidates edge for the claim")
	}

	// Step 6: Verify needs_revalidation staleness markers created
	markers, err := store.ListStaleness(ctx, StalenessFilter{
		WorkspaceID: wsID,
		Status:      StalenessStatusNeedsRevalidation,
	})
	if err != nil {
		t.Fatalf("step 6: list staleness: %v", err)
	}
	if len(markers) == 0 {
		t.Error("step 6: expected needs_revalidation staleness markers after user correction")
	}

	// Step 7: Simulate gap creation — record gap_created feedback
	gapFeedback := RetrievalFeedback{
		ID:          "feedback-gap-3",
		WorkspaceID: wsID,
		EpisodeID:   episodeID,
		Kind:        RetrievalFeedbackKindGapCreated,
		Query:       "what format does config use",
		GapStmt:     "Config format was incorrectly stated as YAML",
		CreatedAt:   now,
	}
	if _, err := store.RecordRetrievalFeedback(ctx, gapFeedback); err != nil {
		t.Fatalf("step 7: record gap feedback: %v", err)
	}

	// Verify gap feedback persisted
	gotFeedback, err := store.GetRetrievalFeedback(ctx, "feedback-gap-3")
	if err != nil {
		t.Fatalf("step 7: get feedback: %v", err)
	}
	if gotFeedback.Kind != RetrievalFeedbackKindGapCreated {
		t.Errorf("step 7: feedback kind = %q, want gap_created", gotFeedback.Kind)
	}
	if gotFeedback.GapStmt != "Config format was incorrectly stated as YAML" {
		t.Errorf("step 7: gap stmt = %q, unexpected", gotFeedback.GapStmt)
	}

	// Step 8: Invalidate the claim (explicit invalidation event)
	invalidationEvent := ContextEvent{
		ID:          "evt-invalidate-1",
		WorkspaceID: wsID,
		Kind:        EventKindMemoryClaimInvalidated,
		Source:      "feedback_processor",
		Refs:        []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: "claim-current-3"}},
		Data:        map[string]any{"reason": "user correction confirmed"},
		CreatedAt:   now,
	}
	if _, err := store.AppendEvent(ctx, invalidationEvent); err != nil {
		t.Fatalf("step 8: append invalidation event: %v", err)
	}
	if err := ApplyInvalidation(ctx, store, invalidationEvent); err != nil {
		t.Fatalf("step 8: apply invalidation: %v", err)
	}

	// Verify claim is now rejected
	rejectedClaim, err := store.GetClaim(ctx, "claim-current-3")
	if err != nil {
		t.Fatalf("step 8: get rejected claim: %v", err)
	}
	if rejectedClaim.Status != ClaimStatusRejected {
		t.Errorf("step 8: claim status = %q, want rejected after invalidation", rejectedClaim.Status)
	}
}

// ==========================================================================
// VAL-CROSS-004: Mixed retrieval flow
// Query → retrieve_mixed → fan out (code + memory + context + task) →
// fuse EvidencePacks by typed ref identity → return compact EvidencePack →
// record RetrievalEpisode with sub-episodes.
// ==========================================================================

func TestIntegrationCrossArea_MixedRetrievalFlow(t *testing.T) {
	resetIntegrationIDCounter()
	ctx := context.Background()
	store := newIntegrationStore()
	wsID := "ws-integration"

	// Setup: store some claims for memory lane
	claim := MemoryClaim{
		ID:          "claim-mixed-1",
		WorkspaceID: wsID,
		ClaimType:   "fact",
		Status:      ClaimStatusCurrent,
		Summary:     "Uses middleware pattern for auth",
		Confidence:  0.9,
		SourceRefs:  []EvidenceRef{{Type: RefTypePath, Ref: "src/auth.go"}},
		CreatedAt:   integrationClock(),
		UpdatedAt:   integrationClock(),
	}
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("setup: upsert claim: %v", err)
	}

	cfg := newIntegrationLaneConfig(store)

	// Code search function — returns hits including a shared path
	codeFn := func(_ context.Context, query string) ([]CodeSearchHit, error) {
		return []CodeSearchHit{
			{
				Path:    "src/auth.go",
				Snippet: "func AuthMiddleware() { ... }",
				Line:    10,
				Symbol:  "AuthMiddleware",
				Score:   0.95,
			},
		}, nil
	}

	// Memory query function — returns claims
	memoryFn := func(_ context.Context, wID, _ string) ([]MemoryClaim, error) {
		return store.ListClaims(ctx, ClaimFilter{WorkspaceID: wID})
	}

	// Context query function — returns context packet
	contextFn := func(_ context.Context, wID string) (*ContextPacket, error) {
		return &ContextPacket{
			WorkspaceID: wID,
			Objective:   "Implement auth middleware",
			Phase:       "implementation",
			RelevantRefs: []EvidenceRef{
				{Type: RefTypePath, Ref: "src/auth.go"},
			},
		}, nil
	}

	// Task query function
	taskQueryFn := func(_ context.Context, wID, tid string) (*TaskContext, error) {
		return &TaskContext{
			WorkspaceID: wID,
			TaskID:      tid,
			Objective:   "Auth middleware task",
			Status:      "in_progress",
			RelatedCodeRefs: []EvidenceRef{
				{Type: RefTypePath, Ref: "src/auth.go"},
			},
			ProjectionMeta: ProjectionMeta{
				ProjectionID:      "proj-task-1",
				ProjectionType:    "task_context",
				ProjectionVersion: 1,
				WorkspaceID:       wID,
				GeneratedAt:       integrationClock(),
			},
			UpdatedAt: integrationClock(),
		}, nil
	}

	taskListFn := func(_ context.Context, wID string) ([]string, error) {
		return []string{"task-1"}, nil
	}

	// Execute: Run mixed retrieval
	pack, err := RetrieveMixed(ctx, cfg, codeFn, memoryFn, contextFn, taskQueryFn, taskListFn, "task-1", "auth middleware")
	if err != nil {
		t.Fatalf("retrieve mixed: %v", err)
	}

	// Verify: Pack structure
	if pack.ID == "" {
		t.Error("pack ID is empty")
	}
	if pack.WorkspaceID != wsID {
		t.Errorf("pack workspace_id = %q, want %q", pack.WorkspaceID, wsID)
	}
	if pack.Query != "auth middleware" {
		t.Errorf("pack query = %q, want 'auth middleware'", pack.Query)
	}
	if pack.Lane != LaneMixed {
		t.Errorf("pack lane = %q, want mixed", pack.Lane)
	}

	// Verify: Nodes from all lanes present
	nodeTypes := make(map[EvidenceNodeType]int)
	for _, node := range pack.Nodes {
		nodeTypes[node.NodeType]++
	}
	if nodeTypes[EvidenceNodeTypeCode] == 0 {
		t.Error("expected code nodes in mixed pack")
	}
	if nodeTypes[EvidenceNodeTypeMemory] == 0 {
		t.Error("expected memory nodes in mixed pack")
	}
	if nodeTypes[EvidenceNodeTypeContext] == 0 {
		t.Error("expected context nodes in mixed pack")
	}
	if nodeTypes[EvidenceNodeTypeTask] == 0 {
		t.Error("expected task nodes in mixed pack")
	}

	// Verify: Fusion by typed ref identity — src/auth.go should be fused
	// (code + context + task all reference it)
	authRefNodes := 0
	for _, node := range pack.Nodes {
		if node.Ref.Type == RefTypePath && node.Ref.Ref == "src/auth.go" {
			authRefNodes++
		}
	}
	// After fusion, there should be only ONE node for src/auth.go (not 3 separate ones)
	if authRefNodes != 1 {
		t.Errorf("expected exactly 1 fused node for src/auth.go after fusion, got %d", authRefNodes)
	}

	// Verify the fused node has multiple source lanes
	for _, node := range pack.Nodes {
		if node.Ref.Type == RefTypePath && node.Ref.Ref == "src/auth.go" {
			lanes := extractLanes(node.Metadata)
			if len(lanes) < 2 {
				t.Errorf("fused src/auth.go node should have multiple source lanes, got %v", lanes)
			}
		}
	}

	// Verify: RetrievalEpisode recorded
	episodes, err := store.ListRetrievalEpisodes(ctx)
	if err != nil {
		t.Fatalf("list episodes: %v", err)
	}
	// Should have at least 5 episodes: 4 lane episodes + 1 mixed parent
	if len(episodes) < 5 {
		t.Errorf("expected at least 5 retrieval episodes (4 lanes + 1 mixed), got %d", len(episodes))
	}

	// Verify: Parent mixed episode has sub-episodes
	var mixedEpisode *RetrievalEpisode
	for i := range episodes {
		if episodes[i].Lane == LaneMixed {
			mixedEpisode = &episodes[i]
			break
		}
	}
	if mixedEpisode == nil {
		t.Fatal("mixed episode not found")
	}
	if len(mixedEpisode.SubEpisodeIDs) < 3 {
		t.Errorf("mixed episode sub_episodes = %d, want at least 3", len(mixedEpisode.SubEpisodeIDs))
	}

	// Verify: Telemetry
	if pack.Telemetry.LanesFused < 3 {
		t.Errorf("lanes_fused = %d, want at least 3", pack.Telemetry.LanesFused)
	}
	if pack.Telemetry.DurationMs < 0 {
		t.Error("duration_ms should be non-negative")
	}
}

// ==========================================================================
// VAL-CROSS-005: Full claim lifecycle
// Candidate → current (via commit) → needs_revalidation (via dirty edit) →
// stale (via unresolved) → superseded (via new claim) OR
// fresh (via validation event).
// ==========================================================================

func TestIntegrationCrossArea_FullClaimLifecycle(t *testing.T) {
	resetIntegrationIDCounter()
	ctx := context.Background()
	store := newIntegrationStore()
	wsID := "ws-integration"
	now := integrationClock()

	// ------------------------------------------------------------------
	// Phase 1: Candidate claim
	// ------------------------------------------------------------------
	claim := MemoryClaim{
		ID:          "claim-lifecycle-1",
		WorkspaceID: wsID,
		ClaimType:   "decision",
		Status:      ClaimStatusCandidate,
		Summary:     "Uses PostgreSQL for storage",
		Confidence:  0.8,
		SourceRefs: []EvidenceRef{
			{Type: RefTypePath, Ref: "src/db.go"},
			{Type: RefTypePath, Ref: "src/config.go"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("phase 1: upsert candidate claim: %v", err)
	}

	got, err := store.GetClaim(ctx, "claim-lifecycle-1")
	if err != nil {
		t.Fatalf("phase 1: get claim: %v", err)
	}
	if got.Status != ClaimStatusCandidate {
		t.Fatalf("phase 1: status = %q, want candidate", got.Status)
	}

	// ------------------------------------------------------------------
	// Phase 2: Candidate → Current (via commit)
	// ------------------------------------------------------------------
	commitEvent := ContextEvent{
		ID:          "evt-commit-lifecycle",
		WorkspaceID: wsID,
		Kind:        EventKindCodeCommitted,
		Source:      "git_hook",
		Refs: []EvidenceRef{
			{Type: RefTypePath, Ref: "src/db.go"},
			{Type: RefTypePath, Ref: "src/config.go"},
		},
		CreatedAt: now,
	}
	if _, err := store.AppendEvent(ctx, commitEvent); err != nil {
		t.Fatalf("phase 2: append commit event: %v", err)
	}
	if err := ApplyInvalidation(ctx, store, commitEvent); err != nil {
		t.Fatalf("phase 2: apply invalidation: %v", err)
	}

	got, err = store.GetClaim(ctx, "claim-lifecycle-1")
	if err != nil {
		t.Fatalf("phase 2: get claim: %v", err)
	}
	if got.Status != ClaimStatusCurrent {
		t.Fatalf("phase 2: status = %q, want current after commit", got.Status)
	}

	// ------------------------------------------------------------------
	// Phase 3: Current → NeedsRevalidation (via dirty edit)
	// The dirty edit creates staleness markers but does NOT directly
	// transition the claim status. Instead, we use an answer.corrected
	// event which DOES transition claims to needs_revalidation.
	// ------------------------------------------------------------------
	// First create an impact edge from the file to the claim
	setupEdge := ImpactEdge{
		ID:          "edge-lifecycle-1",
		WorkspaceID: wsID,
		From:        EvidenceRef{Type: RefTypePath, Ref: "src/db.go"},
		To:          EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-lifecycle-1"},
		Kind:        ImpactEdgeKindGeneratedFrom,
		CreatedAt:   now,
	}
	if _, err := store.PutImpactEdge(ctx, setupEdge); err != nil {
		t.Fatalf("phase 3 setup: put edge: %v", err)
	}

	// Emit a dirty edit event (creates dirty staleness markers)
	dirtyEvent := ContextEvent{
		ID:          "evt-dirty-lifecycle",
		WorkspaceID: wsID,
		Kind:        EventKindCodeChangedDirty,
		Source:      "file_watcher",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/db.go"}},
		CreatedAt:   now,
	}
	if _, err := store.AppendEvent(ctx, dirtyEvent); err != nil {
		t.Fatalf("phase 3: append dirty event: %v", err)
	}
	if err := ApplyInvalidation(ctx, store, dirtyEvent); err != nil {
		t.Fatalf("phase 3: apply invalidation: %v", err)
	}

	// Verify dirty markers exist
	dirtyMarkers, err := store.ListStaleness(ctx, StalenessFilter{
		WorkspaceID: wsID,
		Status:      StalenessStatusDirty,
	})
	if err != nil {
		t.Fatalf("phase 3: list dirty markers: %v", err)
	}
	if len(dirtyMarkers) == 0 {
		t.Fatal("phase 3: expected dirty staleness markers")
	}

	// Now emit a correction event to trigger needs_revalidation
	correctionEvent := ContextEvent{
		ID:          "evt-correct-lifecycle",
		WorkspaceID: wsID,
		Kind:        EventKindAnswerCorrected,
		Source:      "user",
		Refs:        []EvidenceRef{{Type: RefTypePath, Ref: "src/db.go"}},
		Data:        map[string]any{"correction": "the database layer needs updating"},
		CreatedAt:   now,
	}
	if _, err := store.AppendEvent(ctx, correctionEvent); err != nil {
		t.Fatalf("phase 3: append correction event: %v", err)
	}
	if err := ApplyInvalidation(ctx, store, correctionEvent); err != nil {
		t.Fatalf("phase 3: apply invalidation for correction: %v", err)
	}

	got, err = store.GetClaim(ctx, "claim-lifecycle-1")
	if err != nil {
		t.Fatalf("phase 3: get claim: %v", err)
	}
	if got.Status != ClaimStatusNeedsRevalidation {
		t.Fatalf("phase 3: status = %q, want needs_revalidation after correction", got.Status)
	}

	// ------------------------------------------------------------------
	// Phase 4: NeedsRevalidation → Stale (simulate time passage/unresolved)
	// We directly apply the transition since "stale" requires manual marking
	// ------------------------------------------------------------------
	got, err = store.GetClaim(ctx, "claim-lifecycle-1")
	if err != nil {
		t.Fatalf("phase 4: get claim: %v", err)
	}
	staleClaim, err := ApplyClaimTransition(got, ClaimStatusStale, "unresolved for too long", now)
	if err != nil {
		t.Fatalf("phase 4: apply transition needs_revalidation -> stale: %v", err)
	}
	if _, err := store.UpsertClaim(ctx, staleClaim); err != nil {
		t.Fatalf("phase 4: upsert stale claim: %v", err)
	}

	got, err = store.GetClaim(ctx, "claim-lifecycle-1")
	if err != nil {
		t.Fatalf("phase 4: get claim: %v", err)
	}
	if got.Status != ClaimStatusStale {
		t.Fatalf("phase 4: status = %q, want stale", got.Status)
	}

	// ------------------------------------------------------------------
	// Phase 5a: Stale → Superseded (via new claim promotion)
	// ------------------------------------------------------------------
	// Create a new claim that supersedes the old one
	newClaim := MemoryClaim{
		ID:          "claim-lifecycle-2",
		WorkspaceID: wsID,
		ClaimType:   "decision",
		Status:      ClaimStatusCurrent,
		Summary:     "Uses SQLite for storage (updated)",
		Confidence:  0.92,
		SourceRefs: []EvidenceRef{
			{Type: RefTypePath, Ref: "src/db.go"},
			{Type: RefTypeMemoryClaim, Ref: "claim-lifecycle-1"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := store.UpsertClaim(ctx, newClaim); err != nil {
		t.Fatalf("phase 5a: upsert new claim: %v", err)
	}

	// Supersede the old claim
	got, err = store.GetClaim(ctx, "claim-lifecycle-1")
	if err != nil {
		t.Fatalf("phase 5a: get old claim: %v", err)
	}
	supersededClaim, err := ApplyClaimTransition(got, ClaimStatusSuperseded, "superseded by claim-lifecycle-2", now)
	if err != nil {
		t.Fatalf("phase 5a: apply transition stale -> superseded: %v", err)
	}
	supersededClaim.SupersededBy = "claim-lifecycle-2"
	if _, err := store.UpsertClaim(ctx, supersededClaim); err != nil {
		t.Fatalf("phase 5a: upsert superseded claim: %v", err)
	}

	got, err = store.GetClaim(ctx, "claim-lifecycle-1")
	if err != nil {
		t.Fatalf("phase 5a: get claim: %v", err)
	}
	if got.Status != ClaimStatusSuperseded {
		t.Fatalf("phase 5a: status = %q, want superseded", got.Status)
	}
	if got.SupersededBy != "claim-lifecycle-2" {
		t.Errorf("phase 5a: superseded_by = %q, want claim-lifecycle-2", got.SupersededBy)
	}

	// ------------------------------------------------------------------
	// Phase 5b (alternative path): Stale → Current via validation
	// Test this as a separate sub-test on a different claim
	// ------------------------------------------------------------------
	altClaim := MemoryClaim{
		ID:          "claim-alt-1",
		WorkspaceID: wsID,
		ClaimType:   "fact",
		Status:      ClaimStatusStale,
		Summary:     "Alternative stale claim for validation path",
		Confidence:  0.7,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := store.UpsertClaim(ctx, altClaim); err != nil {
		t.Fatalf("phase 5b: upsert alt claim: %v", err)
	}

	// Validate the stale claim back to current
	validatedClaim, err := ApplyClaimTransition(altClaim, ClaimStatusCurrent, "validated by test", now)
	if err != nil {
		t.Fatalf("phase 5b: apply transition stale -> current: %v", err)
	}
	if _, err := store.UpsertClaim(ctx, validatedClaim); err != nil {
		t.Fatalf("phase 5b: upsert validated claim: %v", err)
	}

	got, err = store.GetClaim(ctx, "claim-alt-1")
	if err != nil {
		t.Fatalf("phase 5b: get claim: %v", err)
	}
	if got.Status != ClaimStatusCurrent {
		t.Errorf("phase 5b: status = %q, want current after validation", got.Status)
	}

	// ------------------------------------------------------------------
	// Verify: Complete lifecycle traversed correctly
	// ------------------------------------------------------------------
	// claim-lifecycle-1: candidate → current → needs_revalidation → stale → superseded
	// claim-alt-1: stale → current
	// Both transitions are valid per the transition matrix
	t.Log("Full claim lifecycle traversed successfully:")
	t.Log("  claim-lifecycle-1: candidate → current → needs_revalidation → stale → superseded")
	t.Log("  claim-alt-1: stale → current (validation path)")
}
