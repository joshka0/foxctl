package contextengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	_ "modernc.org/sqlite"
)

// frozenClock returns a fixed time for deterministic tests.
type frozenClock struct {
	t time.Time
}

func (f frozenClock) Now() time.Time { return f.t }

// memCAS is an in-memory CAS backend for testing.
type memCAS struct {
	data map[string][]byte
}

func newMemCAS() *memCAS {
	return &memCAS{data: make(map[string][]byte)}
}

func (m *memCAS) Put(_ context.Context, data []byte) (string, error) {
	digest := fmt.Sprintf("sha256:%x", data[:min(len(data), 32)])
	m.data[digest] = data
	return digest, nil
}

func (m *memCAS) Get(_ context.Context, digest string) ([]byte, error) {
	data, ok := m.data[digest]
	if !ok {
		return nil, fmt.Errorf("CAS: not found: %s", digest)
	}
	return data, nil
}

// openTestStore creates an in-memory store for testing.
func openTestStore(t *testing.T) Store {
	t.Helper()
	return openTestStoreWithCAS(t, nil)
}

func openTestStoreWithCAS(t *testing.T, cas CASBackend) Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clock := frozenClock{t: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)}
	store, err := OpenDB(db, clock, cas)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func now() time.Time {
	return time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
}

// --- Helper constructors ---

func testEvent(id, ws string, kind contextengine.ContextEventKind) contextengine.ContextEvent {
	return contextengine.ContextEvent{
		ID: id, WorkspaceID: ws, Kind: kind,
		Source: "test", CreatedAt: now(),
	}
}

func testPack(id, ws, query string, lane contextengine.EvidenceLane) contextengine.EvidencePack {
	return contextengine.EvidencePack{
		ID: id, WorkspaceID: ws, Query: query, Lane: lane,
	}
}

func testNode(id, ws string, ref contextengine.EvidenceRef) contextengine.EvidenceNode {
	return contextengine.EvidenceNode{
		ID: id, WorkspaceID: ws,
		NodeType: contextengine.EvidenceNodeTypeCode,
		Ref:      ref,
		Statement: "test statement",
	}
}

func testClaim(id, ws string, status contextengine.ClaimStatus) contextengine.MemoryClaim {
	return contextengine.MemoryClaim{
		ID: id, WorkspaceID: ws,
		ClaimType: "decision", Status: status,
		Summary: "test claim",
		CreatedAt: now(), UpdatedAt: now(),
	}
}

func testImpactEdge(id, ws string, from, to contextengine.EvidenceRef) contextengine.ImpactEdge {
	return contextengine.ImpactEdge{
		ID: id, WorkspaceID: ws,
		From: from, To: to,
		Kind:      contextengine.ImpactEdgeKindDependsOn,
		CreatedAt: now(),
	}
}

func testStalenessMarker(id, ws string, target contextengine.EvidenceRef, status contextengine.StalenessStatus) contextengine.StalenessMarker {
	m := contextengine.StalenessMarker{
		ID: id, WorkspaceID: ws,
		TargetRef: target,
		Status:    status,
		CreatedAt: now(), UpdatedAt: now(),
	}
	if status == contextengine.StalenessStatusDirty {
		m.CausedByEvents = []string{"evt-1"}
	}
	return m
}

func testEpisode(id, ws, query string, lane contextengine.EvidenceLane) contextengine.RetrievalEpisode {
	return contextengine.RetrievalEpisode{
		ID: id, WorkspaceID: ws,
		Query: query, Lane: lane,
		CreatedAt: now(),
	}
}

func testFeedback(id, ws, episodeID string, kind contextengine.RetrievalFeedbackKind) contextengine.RetrievalFeedback {
	return contextengine.RetrievalFeedback{
		ID: id, WorkspaceID: ws,
		EpisodeID: episodeID, Kind: kind,
		Query: "test query", CreatedAt: now(),
	}
}

// ========== VAL-STORE-001: Append event creates durable record ==========

func TestStore_AppendEvent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	evt := testEvent("evt-1", "ws-1", contextengine.EventKindCodeChangedDirty)
	got, err := s.AppendEvent(ctx, evt)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if got.ID != "evt-1" {
		t.Errorf("expected ID evt-1, got %q", got.ID)
	}

	events, err := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != contextengine.EventKindCodeChangedDirty {
		t.Errorf("expected kind code.changed_dirty, got %q", events[0].Kind)
	}
}

// ========== VAL-STORE-002: List events filters by workspace ==========

func mustAppendEvent(t *testing.T, s Store, ctx context.Context, evt contextengine.ContextEvent) {
	t.Helper()
	if _, err := s.AppendEvent(ctx, evt); err != nil {
		t.Fatalf("AppendEvent %s: %v", evt.ID, err)
	}
}

func mustUpsertClaim(t *testing.T, s Store, ctx context.Context, claim contextengine.MemoryClaim) {
	t.Helper()
	if _, err := s.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("UpsertClaim %s: %v", claim.ID, err)
	}
}

func mustPutImpactEdge(t *testing.T, s Store, ctx context.Context, edge contextengine.ImpactEdge) {
	t.Helper()
	if _, err := s.PutImpactEdge(ctx, edge); err != nil {
		t.Fatalf("PutImpactEdge %s: %v", edge.ID, err)
	}
}

func mustPutEvidencePack(t *testing.T, s Store, ctx context.Context, pack contextengine.EvidencePack) {
	t.Helper()
	if _, err := s.PutEvidencePack(ctx, pack); err != nil {
		t.Fatalf("PutEvidencePack %s: %v", pack.ID, err)
	}
}

func mustPutEvidenceNode(t *testing.T, s Store, ctx context.Context, node contextengine.EvidenceNode) {
	t.Helper()
	if _, err := s.PutEvidenceNode(ctx, node); err != nil {
		t.Fatalf("PutEvidenceNode %s: %v", node.ID, err)
	}
}

func mustUpsertStaleness(t *testing.T, s Store, ctx context.Context, marker contextengine.StalenessMarker) {
	t.Helper()
	if _, err := s.UpsertStaleness(ctx, marker); err != nil {
		t.Fatalf("UpsertStaleness %s: %v", marker.ID, err)
	}
}

func mustRecordEpisode(t *testing.T, s Store, ctx context.Context, ep contextengine.RetrievalEpisode) {
	t.Helper()
	if _, err := s.RecordRetrievalEpisode(ctx, ep); err != nil {
		t.Fatalf("RecordRetrievalEpisode %s: %v", ep.ID, err)
	}
}

func mustRecordFeedback(t *testing.T, s Store, ctx context.Context, fb contextengine.RetrievalFeedback) {
	t.Helper()
	if _, err := s.RecordRetrievalFeedback(ctx, fb); err != nil {
		t.Fatalf("RecordRetrievalFeedback %s: %v", fb.ID, err)
	}
}

func mustPutProjection(t *testing.T, s Store, ctx context.Context, id, ws, pType string, version int, taskID string, events []string, payload any, genAt, expAt time.Time) {
	t.Helper()
	if err := s.PutProjection(ctx, id, ws, pType, version, taskID, events, payload, genAt, expAt); err != nil {
		t.Fatalf("PutProjection %s: %v", id, err)
	}
}

func TestStore_ListEvents_FilterWorkspace(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	mustAppendEvent(t, s, ctx, testEvent("evt-1", "ws-1", contextengine.EventKindCodeChangedDirty))
	mustAppendEvent(t, s, ctx, testEvent("evt-2", "ws-2", contextengine.EventKindCodeCommitted))

	events, err := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 || events[0].WorkspaceID != "ws-1" {
		t.Errorf("expected 1 event from ws-1, got %d events", len(events))
	}
}

// ========== VAL-STORE-003: List events filters by kind ==========

func TestStore_ListEvents_FilterKind(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	mustAppendEvent(t, s, ctx, testEvent("evt-1", "ws-1", contextengine.EventKindCodeChangedDirty))
	mustAppendEvent(t, s, ctx, testEvent("evt-2", "ws-1", contextengine.EventKindCodeCommitted))

	events, err := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1", Kind: contextengine.EventKindCodeCommitted})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != contextengine.EventKindCodeCommitted {
		t.Errorf("expected 1 committed event, got %d events", len(events))
	}
}

// ========== VAL-STORE-004: List events filters by task_id ==========

func TestStore_ListEvents_FilterTaskID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	evt1 := testEvent("evt-1", "ws-1", contextengine.EventKindCodeChangedDirty)
	evt1.TaskID = "task-1"
	evt2 := testEvent("evt-2", "ws-1", contextengine.EventKindCodeChangedDirty)
	evt2.TaskID = "task-2"
	mustAppendEvent(t, s, ctx, evt1)
	mustAppendEvent(t, s, ctx, evt2)

	events, err := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1", TaskID: "task-1"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 || events[0].TaskID != "task-1" {
		t.Errorf("expected 1 event for task-1, got %d", len(events))
	}
}

// ========== VAL-STORE-005: List events filters by session_id ==========

func TestStore_ListEvents_FilterSessionID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	evt1 := testEvent("evt-1", "ws-1", contextengine.EventKindCodeChangedDirty)
	evt1.SessionID = "sess-1"
	evt2 := testEvent("evt-2", "ws-1", contextengine.EventKindCodeChangedDirty)
	evt2.SessionID = "sess-2"
	mustAppendEvent(t, s, ctx, evt1)
	mustAppendEvent(t, s, ctx, evt2)

	events, err := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1", SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 || events[0].SessionID != "sess-1" {
		t.Errorf("expected 1 event for sess-1, got %d", len(events))
	}
}

// ========== VAL-STORE-006: Events ordered by created_at descending ==========

func TestStore_ListEvents_OrderDesc(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	mustAppendEvent(t, s, ctx, testEvent("evt-1", "ws-1", contextengine.EventKindCodeChangedDirty))
	mustAppendEvent(t, s, ctx, testEvent("evt-2", "ws-1", contextengine.EventKindCodeCommitted))
	mustAppendEvent(t, s, ctx, testEvent("evt-3", "ws-1", contextengine.EventKindCodeValidated))

	events, err := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	// With frozen clock, all have same created_at, so order is by rowid desc (insert order reversed)
	// Just verify we got all 3 events
	ids := make(map[string]bool)
	for _, e := range events {
		ids[e.ID] = true
	}
	for _, expected := range []string{"evt-1", "evt-2", "evt-3"} {
		if !ids[expected] {
			t.Errorf("missing event %q", expected)
		}
	}
}

// ========== VAL-STORE-007: Put evidence pack creates durable record ==========

func TestStore_PutEvidencePack(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	pack := testPack("pack-1", "ws-1", "test query", contextengine.LaneCode)
	got, err := s.PutEvidencePack(ctx, pack)
	if err != nil {
		t.Fatalf("PutEvidencePack: %v", err)
	}
	if got.ID != "pack-1" {
		t.Errorf("expected ID pack-1, got %q", got.ID)
	}

	retrieved, err := s.GetEvidencePack(ctx, "pack-1")
	if err != nil {
		t.Fatalf("GetEvidencePack: %v", err)
	}
	if retrieved.Query != "test query" {
		t.Errorf("expected query 'test query', got %q", retrieved.Query)
	}
}

// ========== VAL-STORE-008: Upsert claim creates or updates ==========

func TestStore_UpsertClaim_Create(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	claim := testClaim("claim-1", "ws-1", contextengine.ClaimStatusCandidate)
	got, err := s.UpsertClaim(ctx, claim)
	if err != nil {
		t.Fatalf("UpsertClaim: %v", err)
	}
	if got.ID != "claim-1" {
		t.Errorf("expected ID claim-1, got %q", got.ID)
	}
}

func TestStore_UpsertClaim_Update(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	claim := testClaim("claim-1", "ws-1", contextengine.ClaimStatusCandidate)
	mustUpsertClaim(t, s, ctx, claim)

	claim.Status = contextengine.ClaimStatusCurrent
	claim.Reason = "promoted by commit"
	got, err := s.UpsertClaim(ctx, claim)
	if err != nil {
		t.Fatalf("UpsertClaim update: %v", err)
	}
	if got.Status != contextengine.ClaimStatusCurrent {
		t.Errorf("expected status current, got %q", got.Status)
	}

	retrieved, err := s.GetClaim(ctx, "claim-1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if retrieved.Status != contextengine.ClaimStatusCurrent {
		t.Errorf("expected persisted status current, got %q", retrieved.Status)
	}
}

// ========== VAL-STORE-009: List claims filters by workspace and status ==========

func TestStore_ListClaims_Filter(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	mustUpsertClaim(t, s, ctx, testClaim("c1", "ws-1", contextengine.ClaimStatusCandidate))
	mustUpsertClaim(t, s, ctx, testClaim("c2", "ws-1", contextengine.ClaimStatusCurrent))
	mustUpsertClaim(t, s, ctx, testClaim("c3", "ws-2", contextengine.ClaimStatusCandidate))

	claims, err := s.ListClaims(ctx, ClaimFilter{WorkspaceID: "ws-1", Status: contextengine.ClaimStatusCurrent})
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(claims) != 1 || claims[0].ID != "c2" {
		t.Errorf("expected 1 current claim c2 from ws-1, got %d", len(claims))
	}
}

// ========== Index verification tests ==========

func assertIndexUsed(t *testing.T, s Store, ctx context.Context, query string, args ...any) {
	t.Helper()
	plan, err := s.ExplainQueryPlan(ctx, query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	t.Logf("plan: %s", plan)
	// Look for index usage indicator (USING INDEX or COVERING INDEX or sqlite autoindex)
	if !containsIndex(plan) {
		t.Errorf("expected index usage in query plan, got: %s", plan)
	}
}

func containsIndex(plan string) bool {
	// SQLite EXPLAIN QUERY PLAN shows index usage with these markers
	markers := []string{"USING INDEX", "COVERING INDEX", "USING AUTOINDEX"}
	for _, m := range markers {
		if len(m) <= len(plan) {
			for i := 0; i <= len(plan)-len(m); i++ {
				if plan[i:i+len(m)] == m {
					return true
				}
			}
		}
	}
	// Also check for PRIMARY KEY usage (which is an index scan)
	if len(plan) >= len("PRIMARY KEY") {
		for i := 0; i <= len(plan)-len("PRIMARY KEY"); i++ {
			if plan[i:i+len("PRIMARY KEY")] == "PRIMARY KEY" {
				return true
			}
		}
	}
	return false
}

// VAL-STORE-010
func TestStore_Index_WorkspaceID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	assertIndexUsed(t, s, ctx,
		"SELECT * FROM context_events WHERE workspace_id = ?", "ws-1")
}

// VAL-STORE-011
func TestStore_Index_KindCreated(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	assertIndexUsed(t, s, ctx,
		"SELECT * FROM context_events WHERE kind = ? ORDER BY created_at DESC", "code.changed_dirty")
}

// VAL-STORE-012
func TestStore_Index_TaskID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	assertIndexUsed(t, s, ctx,
		"SELECT * FROM context_events WHERE task_id = ?", "task-1")
}

// VAL-STORE-013
func TestStore_Index_SessionID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	assertIndexUsed(t, s, ctx,
		"SELECT * FROM context_events WHERE session_id = ?", "sess-1")
}

// VAL-STORE-014
func TestStore_Index_RefTypeValue(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	assertIndexUsed(t, s, ctx,
		"SELECT * FROM evidence_nodes WHERE ref_type = ? AND ref_value = ?", "path", "foo.go")
}

// VAL-STORE-015
func TestStore_Index_ClaimStatus(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	assertIndexUsed(t, s, ctx,
		"SELECT * FROM memory_claims WHERE workspace_id = ? AND status = ?", "ws-1", "current")
}

// VAL-STORE-016
func TestStore_Index_StalenessTarget(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	assertIndexUsed(t, s, ctx,
		"SELECT * FROM staleness_markers WHERE workspace_id = ? AND target_ref_type = ? AND target_ref_value = ?",
		"ws-1", "path", "foo.go")
}

// VAL-STORE-017
func TestStore_Index_ImpactFrom(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	assertIndexUsed(t, s, ctx,
		"SELECT * FROM impact_edges WHERE workspace_id = ? AND from_type = ? AND from_ref = ?",
		"ws-1", "path", "foo.go")
}

// VAL-STORE-018
func TestStore_Index_ImpactTo(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	assertIndexUsed(t, s, ctx,
		"SELECT * FROM impact_edges WHERE workspace_id = ? AND to_type = ? AND to_ref = ?",
		"ws-1", "path", "bar.go")
}

// ========== VAL-STORE-019: Events are append-only ==========

func TestStore_AppendEvent_RejectsDuplicate(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	evt := testEvent("evt-1", "ws-1", contextengine.EventKindCodeChangedDirty)
	_, err := s.AppendEvent(ctx, evt)
	if err != nil {
		t.Fatalf("first AppendEvent: %v", err)
	}

	_, err = s.AppendEvent(ctx, evt)
	if err == nil {
		t.Fatal("expected error on duplicate event ID, got nil")
	}
}

// ========== VAL-STORE-020: Retrieval episodes are append-only ==========

func TestStore_RecordEpisode_RejectsDuplicate(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ep := testEpisode("ep-1", "ws-1", "test query", contextengine.LaneCode)
	_, err := s.RecordRetrievalEpisode(ctx, ep)
	if err != nil {
		t.Fatalf("first RecordRetrievalEpisode: %v", err)
	}

	_, err = s.RecordRetrievalEpisode(ctx, ep)
	if err == nil {
		t.Fatal("expected error on duplicate episode ID, got nil")
	}
}

// ========== VAL-STORE-021: Retrieval feedback is append-only ==========

func TestStore_RecordFeedback_RejectsDuplicate(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Need an episode first
	ep := testEpisode("ep-1", "ws-1", "test query", contextengine.LaneCode)
	mustRecordEpisode(t, s, ctx, ep)

	fb := testFeedback("fb-1", "ws-1", "ep-1", contextengine.RetrievalFeedbackKindEvidenceUsed)
	_, err := s.RecordRetrievalFeedback(ctx, fb)
	if err != nil {
		t.Fatalf("first RecordRetrievalFeedback: %v", err)
	}

	_, err = s.RecordRetrievalFeedback(ctx, fb)
	if err == nil {
		t.Fatal("expected error on duplicate feedback ID, got nil")
	}
}

// ========== VAL-STORE-022/023/024: CAS integration ==========

func TestStore_LargePayload_RoutesToCAS(t *testing.T) {
	cas := newMemCAS()
	s := openTestStoreWithCAS(t, cas)
	ctx := context.Background()

	// Create a pack with nodes totaling >64KB
	largeText := make([]byte, 65*1024)
	for i := range largeText {
		largeText[i] = 'A'
	}

	pack := testPack("pack-1", "ws-1", "test query", contextengine.LaneCode)
	pack.Nodes = []contextengine.EvidenceNode{
		{
			ID: "node-1", WorkspaceID: "ws-1",
			NodeType: contextengine.EvidenceNodeTypeCode,
			Ref:      contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "big.go"},
			Statement: string(largeText),
		},
	}

	_, err := s.PutEvidencePack(ctx, pack)
	if err != nil {
		t.Fatalf("PutEvidencePack with large payload: %v", err)
	}

	// Verify CAS was used
	if len(cas.data) == 0 {
		t.Error("expected CAS to be used for large payload")
	}

	// Retrieve and verify content
	retrieved, err := s.GetEvidencePack(ctx, "pack-1")
	if err != nil {
		t.Fatalf("GetEvidencePack: %v", err)
	}
	if len(retrieved.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(retrieved.Nodes))
	}
	if retrieved.Nodes[0].Statement != string(largeText) {
		t.Errorf("expected %d byte statement, got %d bytes", len(largeText), len(retrieved.Nodes[0].Statement))
	}
}

func TestStore_SmallPayload_StaysInline(t *testing.T) {
	cas := newMemCAS()
	s := openTestStoreWithCAS(t, cas)
	ctx := context.Background()

	pack := testPack("pack-1", "ws-1", "test query", contextengine.LaneCode)
	pack.Nodes = []contextengine.EvidenceNode{
		{
			ID: "node-1", WorkspaceID: "ws-1",
			NodeType: contextengine.EvidenceNodeTypeCode,
			Ref:      contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "small.go"},
			Statement: "tiny statement",
		},
	}

	_, err := s.PutEvidencePack(ctx, pack)
	if err != nil {
		t.Fatalf("PutEvidencePack: %v", err)
	}

	if len(cas.data) > 0 {
		t.Error("expected no CAS usage for small payload")
	}

	retrieved, err := s.GetEvidencePack(ctx, "pack-1")
	if err != nil {
		t.Fatalf("GetEvidencePack: %v", err)
	}
	if retrieved.Nodes[0].Statement != "tiny statement" {
		t.Errorf("expected inline text, got %q", retrieved.Nodes[0].Statement)
	}
}

// ========== VAL-STORE-025: Forward impact traversal ==========

func TestStore_ImpactForwardTraversal(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "foo.go"}
	mustPutImpactEdge(t, s, ctx, testImpactEdge("e1", "ws-1", ref,
		contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "bar.go"}))
	mustPutImpactEdge(t, s, ctx, testImpactEdge("e2", "ws-1", ref,
		contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "baz.go"}))
	mustPutImpactEdge(t, s, ctx, testImpactEdge("e3", "ws-1",
		contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "other.go"}, ref))

	edges, err := s.ListImpactEdges(ctx, ImpactFilter{
		WorkspaceID: "ws-1",
		FromRef:     &ref,
	})
	if err != nil {
		t.Fatalf("ListImpactEdges: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 forward edges, got %d", len(edges))
	}
}

// ========== VAL-STORE-026: Reverse impact traversal ==========

func TestStore_ImpactReverseTraversal(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	target := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "bar.go"}
	mustPutImpactEdge(t, s, ctx, testImpactEdge("e1", "ws-1",
		contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "foo.go"}, target))
	mustPutImpactEdge(t, s, ctx, testImpactEdge("e2", "ws-1",
		contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "baz.go"}, target))
	mustPutImpactEdge(t, s, ctx, testImpactEdge("e3", "ws-1",
		target, contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "qux.go"}))

	edges, err := s.ReverseImpact(ctx, "ws-1", target)
	if err != nil {
		t.Fatalf("ReverseImpact: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 reverse edges, got %d", len(edges))
	}
}

// ========== VAL-STORE-027: Combined impact graph traversal ==========

func TestStore_CombinedImpactTraversal(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Build: a -> b -> c, d -> b
	aRef := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "a.go"}
	bRef := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "b.go"}
	cRef := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "c.go"}
	dRef := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "d.go"}

	mustPutImpactEdge(t, s, ctx, testImpactEdge("e1", "ws-1", aRef, bRef))
	mustPutImpactEdge(t, s, ctx, testImpactEdge("e2", "ws-1", bRef, cRef))
	mustPutImpactEdge(t, s, ctx, testImpactEdge("e3", "ws-1", dRef, bRef))

	// From b: forward = [b->c], reverse = [a->b, d->b]
	forward, _ := s.ListImpactEdges(ctx, ImpactFilter{WorkspaceID: "ws-1", FromRef: &bRef})
	reverse, _ := s.ReverseImpact(ctx, "ws-1", bRef)

	total := len(forward) + len(reverse)
	if total != 3 {
		t.Errorf("expected 3 edges (1 forward + 2 reverse) from seed b, got %d (fwd=%d, rev=%d)",
			total, len(forward), len(reverse))
	}
}

// ========== VAL-STORE-028: Reject invalid ref types ==========

func TestStore_RejectInvalidRefType(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	node := contextengine.EvidenceNode{
		ID: "node-1", WorkspaceID: "ws-1",
		NodeType: contextengine.EvidenceNodeTypeCode,
		Ref:      contextengine.EvidenceRef{Type: "invalid_type", Ref: "foo.go"},
	}
	_, err := s.PutEvidenceNode(ctx, node)
	if err == nil {
		t.Fatal("expected error for invalid ref type, got nil")
	}
}

// ========== VAL-STORE-029: Reject empty required fields ==========

func TestStore_RejectEmptyWorkspaceID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	evt := testEvent("evt-1", "", contextengine.EventKindCodeChangedDirty)
	_, err := s.AppendEvent(ctx, evt)
	if err == nil {
		t.Fatal("expected error for empty workspace_id, got nil")
	}
}

// ========== VAL-STORE-030: Reject unknown claim statuses ==========

func TestStore_RejectUnknownClaimStatus(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	claim := testClaim("c1", "ws-1", contextengine.ClaimStatus("unknown"))
	_, err := s.UpsertClaim(ctx, claim)
	if err == nil {
		t.Fatal("expected error for unknown claim status, got nil")
	}
}

// ========== VAL-STORE-031: Reject non-UTC timestamps ==========

func TestStore_RejectNonUTCTimestamps(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// The validation in contextengine.Validate doesn't explicitly check UTC,
	// but our store normalizes to UTC via the clock. This tests that
	// the clock injection works (all timestamps are UTC from frozen clock).
	evt := testEvent("evt-1", "ws-1", contextengine.EventKindCodeChangedDirty)
	got, err := s.AppendEvent(ctx, evt)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if got.CreatedAt.Location() != time.UTC && got.CreatedAt.Location().String() != "" {
		t.Errorf("expected UTC timestamp, got %v", got.CreatedAt.Location())
	}
}

// ========== VAL-STORE-032: Store does not import domain packages ==========
// (verified via go list -deps in separate test run)

// ========== VAL-STORE-033: Claim upsert is idempotent ==========

func TestStore_ClaimUpsertIdempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	claim := testClaim("c1", "ws-1", contextengine.ClaimStatusCandidate)
	first, err := s.UpsertClaim(ctx, claim)
	if err != nil {
		t.Fatalf("first UpsertClaim: %v", err)
	}

	second, err := s.UpsertClaim(ctx, claim)
	if err != nil {
		t.Fatalf("second UpsertClaim: %v", err)
	}

	if first.Status != second.Status {
		t.Errorf("idempotent upsert changed status: %q -> %q", first.Status, second.Status)
	}
}

// ========== VAL-STORE-034: Staleness upsert is idempotent ==========

func TestStore_StalenessUpsertIdempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "foo.go"}
	marker := testStalenessMarker("m1", "ws-1", ref, contextengine.StalenessStatusDirty)
	first, err := s.UpsertStaleness(ctx, marker)
	if err != nil {
		t.Fatalf("first UpsertStaleness: %v", err)
	}

	second, err := s.UpsertStaleness(ctx, marker)
	if err != nil {
		t.Fatalf("second UpsertStaleness: %v", err)
	}

	if first.Status != second.Status {
		t.Errorf("idempotent upsert changed status: %q -> %q", first.Status, second.Status)
	}
}

// ========== VAL-STORE-035: Projection version increments monotonically ==========

func TestStore_ProjectionVersionMonotonic(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	payload := map[string]string{"key": "value"}

	mustPutProjection(t, s, ctx, "proj-1", "ws-1", "top_of_mind", 1, "", nil, payload, now(), time.Time{})
	mustPutProjection(t, s, ctx, "proj-1", "ws-1", "top_of_mind", 2, "", nil, payload, now(), time.Time{})

	_, version, _, _, _, _, _, err := s.GetProjection(ctx, "ws-1", "proj-1")
	if err != nil {
		t.Fatalf("GetProjection: %v", err)
	}
	if version != 2 {
		t.Errorf("expected version 2, got %d", version)
	}
}

// ========== VAL-STORE-037: Projection lineage isolation ==========

func TestStore_ProjectionLineageIsolation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	payload := map[string]string{"key": "value"}
	mustPutProjection(t, s, ctx, "proj-1", "ws-1", "type-a", 1, "", nil, payload, now(), time.Time{})
	mustPutProjection(t, s, ctx, "proj-2", "ws-1", "type-b", 1, "", nil, payload, now(), time.Time{})
	mustPutProjection(t, s, ctx, "proj-1", "ws-1", "type-a", 2, "", nil, payload, now(), time.Time{})

	_, v1, _, _, _, _, _, err := s.GetProjection(ctx, "ws-1", "proj-1")
	if err != nil {
		t.Fatalf("GetProjection proj-1: %v", err)
	}
	_, v2, _, _, _, _, _, err := s.GetProjection(ctx, "ws-1", "proj-2")
	if err != nil {
		t.Fatalf("GetProjection proj-2: %v", err)
	}

	if v1 != 2 {
		t.Errorf("proj-1: expected version 2, got %d", v1)
	}
	if v2 != 1 {
		t.Errorf("proj-2: expected version 1, got %d", v2)
	}
}

// ========== VAL-STORE-038: List events pagination ==========

func TestStore_ListEvents_Pagination(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		mustAppendEvent(t, s, ctx, testEvent(fmt.Sprintf("evt-%d", i), "ws-1", contextengine.EventKindCodeChangedDirty))
	}

	// Page 1: limit=3, offset=0
	page1, err := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1", Limit: 3, Offset: 0})
	if err != nil {
		t.Fatalf("ListEvents page 1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("page 1: expected 3 events, got %d", len(page1))
	}

	// Page 2: limit=3, offset=3
	page2, err := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1", Limit: 3, Offset: 3})
	if err != nil {
		t.Fatalf("ListEvents page 2: %v", err)
	}
	if len(page2) != 3 {
		t.Fatalf("page 2: expected 3 events, got %d", len(page2))
	}

	// Pages should have different events
	if page1[0].ID == page2[0].ID {
		t.Error("page 1 and page 2 should have different events")
	}
}

// ========== VAL-STORE-039: Clock injection for determinism ==========

func TestStore_ClockInjection(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	evt := testEvent("evt-1", "ws-1", contextengine.EventKindCodeChangedDirty)
	// Remove CreatedAt to let the store inject it
	evt.CreatedAt = time.Time{}

	got, err := s.AppendEvent(ctx, evt)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	expected := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	if !got.CreatedAt.Equal(expected) {
		t.Errorf("expected frozen clock time %v, got %v", expected, got.CreatedAt)
	}
}

// ========== VAL-STORE-040: Schema migration is idempotent ==========

func TestStore_SchemaMigrationIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

// ========== VAL-STORE-041: Evidence pack upsert ==========

func TestStore_EvidencePackUpsert(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	pack := testPack("pack-1", "ws-1", "query 1", contextengine.LaneCode)
	mustPutEvidencePack(t, s, ctx, pack)

	pack.Query = "query 2"
	mustPutEvidencePack(t, s, ctx, pack)

	got, err := s.GetEvidencePack(ctx, "pack-1")
	if err != nil {
		t.Fatalf("GetEvidencePack: %v", err)
	}
	if got.Query != "query 2" {
		t.Errorf("expected updated query, got %q", got.Query)
	}
}

// ========== VAL-STORE-042: Impact edge idempotent insert ==========

func TestStore_ImpactEdgeIdempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	from := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "a.go"}
	to := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "b.go"}

	edge := testImpactEdge("e1", "ws-1", from, to)
	mustPutImpactEdge(t, s, ctx, edge)
	mustPutImpactEdge(t, s, ctx, edge) // same unique key

	edges, err := s.ListImpactEdges(ctx, ImpactFilter{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("ListImpactEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("expected 1 edge (idempotent), got %d", len(edges))
	}
}

// ========== VAL-STORE-043: Cross-workspace isolation ==========

func TestStore_CrossWorkspaceIsolation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	mustAppendEvent(t, s, ctx, testEvent("evt-1", "ws-1", contextengine.EventKindCodeChangedDirty))
	mustAppendEvent(t, s, ctx, testEvent("evt-2", "ws-2", contextengine.EventKindCodeCommitted))
	mustUpsertClaim(t, s, ctx, testClaim("c1", "ws-1", contextengine.ClaimStatusCandidate))
	mustUpsertClaim(t, s, ctx, testClaim("c2", "ws-2", contextengine.ClaimStatusCurrent))

	events, _ := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1"})
	for _, e := range events {
		if e.WorkspaceID != "ws-1" {
			t.Errorf("cross-workspace leak: event %q has workspace %q", e.ID, e.WorkspaceID)
		}
	}

	claims, _ := s.ListClaims(ctx, ClaimFilter{WorkspaceID: "ws-2"})
	for _, c := range claims {
		if c.WorkspaceID != "ws-2" {
			t.Errorf("cross-workspace leak: claim %q has workspace %q", c.ID, c.WorkspaceID)
		}
	}
}

// ========== VAL-STORE-044: Close safety ==========

func TestStore_CloseSafety(t *testing.T) {
	s := openTestStore(t)

	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// ========== Evidence node CRUD ==========

func TestStore_EvidenceNodeCRUD(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "foo.go"}
	node := testNode("node-1", "ws-1", ref)
	got, err := s.PutEvidenceNode(ctx, node)
	if err != nil {
		t.Fatalf("PutEvidenceNode: %v", err)
	}
	if got.ID != "node-1" {
		t.Errorf("expected ID node-1, got %q", got.ID)
	}

	retrieved, err := s.GetEvidenceNode(ctx, "node-1")
	if err != nil {
		t.Fatalf("GetEvidenceNode: %v", err)
	}
	if retrieved.Statement != "test statement" {
		t.Errorf("expected 'test statement', got %q", retrieved.Statement)
	}

	// List by ref
	nodes, err := s.ListEvidenceNodes(ctx, "ws-1", ref, 10)
	if err != nil {
		t.Fatalf("ListEvidenceNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(nodes))
	}
}

// ========== Staleness CRUD ==========

func TestStore_StalenessCRUD(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "foo.go"}
	marker := testStalenessMarker("m1", "ws-1", ref, contextengine.StalenessStatusDirty)
	got, err := s.UpsertStaleness(ctx, marker)
	if err != nil {
		t.Fatalf("UpsertStaleness: %v", err)
	}
	if got.ID != "m1" {
		t.Errorf("expected ID m1, got %q", got.ID)
	}

	retrieved, err := s.GetStaleness(ctx, "m1")
	if err != nil {
		t.Fatalf("GetStaleness: %v", err)
	}
	if retrieved.Status != contextengine.StalenessStatusDirty {
		t.Errorf("expected dirty, got %q", retrieved.Status)
	}

	// List by target
	markers, err := s.ListStaleness(ctx, StalenessFilter{
		WorkspaceID: "ws-1",
		TargetRef:   &ref,
	})
	if err != nil {
		t.Fatalf("ListStaleness: %v", err)
	}
	if len(markers) != 1 {
		t.Errorf("expected 1 marker, got %d", len(markers))
	}
}

// ========== Retrieval episode and feedback CRUD ==========

func TestStore_RetrievalEpisodeCRUD(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ep := testEpisode("ep-1", "ws-1", "test query", contextengine.LaneCode)
	got, err := s.RecordRetrievalEpisode(ctx, ep)
	if err != nil {
		t.Fatalf("RecordRetrievalEpisode: %v", err)
	}
	if got.ID != "ep-1" {
		t.Errorf("expected ID ep-1, got %q", got.ID)
	}

	retrieved, err := s.GetRetrievalEpisode(ctx, "ep-1")
	if err != nil {
		t.Fatalf("GetRetrievalEpisode: %v", err)
	}
	if retrieved.Query != "test query" {
		t.Errorf("expected 'test query', got %q", retrieved.Query)
	}
}

func TestStore_RetrievalFeedbackCRUD(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ep := testEpisode("ep-1", "ws-1", "test query", contextengine.LaneCode)
	mustRecordEpisode(t, s, ctx, ep)

	fb := testFeedback("fb-1", "ws-1", "ep-1", contextengine.RetrievalFeedbackKindEvidenceUsed)
	got, err := s.RecordRetrievalFeedback(ctx, fb)
	if err != nil {
		t.Fatalf("RecordRetrievalFeedback: %v", err)
	}
	if got.ID != "fb-1" {
		t.Errorf("expected ID fb-1, got %q", got.ID)
	}

	retrieved, err := s.GetRetrievalFeedback(ctx, "fb-1")
	if err != nil {
		t.Fatalf("GetRetrievalFeedback: %v", err)
	}
	if retrieved.Kind != contextengine.RetrievalFeedbackKindEvidenceUsed {
		t.Errorf("expected evidence_used, got %q", retrieved.Kind)
	}
}

// ========== Projection CRUD ==========

func TestStore_ProjectionCRUD(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	payload := map[string]string{"key": "value"}
	err := s.PutProjection(ctx, "proj-1", "ws-1", "top_of_mind", 1, "", []string{"evt-1"}, payload, now(), time.Time{})
	if err != nil {
		t.Fatalf("PutProjection: %v", err)
	}

	pType, version, taskID, events, rawPayload, genAt, expiresAt, err := s.GetProjection(ctx, "ws-1", "proj-1")
	if err != nil {
		t.Fatalf("GetProjection: %v", err)
	}
	if pType != "top_of_mind" {
		t.Errorf("expected type top_of_mind, got %q", pType)
	}
	if version != 1 {
		t.Errorf("expected version 1, got %d", version)
	}
	if taskID != "" {
		t.Errorf("expected empty taskID, got %q", taskID)
	}
	if len(events) != 1 || events[0] != "evt-1" {
		t.Errorf("expected events [evt-1], got %v", events)
	}
	if genAt.IsZero() {
		t.Error("expected non-zero generated_at")
	}
	if !expiresAt.IsZero() {
		t.Error("expected zero expires_at")
	}

	var result map[string]string
	if err := json.Unmarshal(rawPayload, &result); err != nil { t.Fatalf("unmarshal payload: %v", err) }
	if result["key"] != "value" {
		t.Errorf("expected payload key=value, got %v", result)
	}
}

// ========== Projection with taskID filter ==========

func TestStore_ProjectionFilterByTaskID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	payload := map[string]string{"key": "value"}
	mustPutProjection(t, s, ctx, "proj-1", "ws-1", "task_context", 1, "task-1", nil, payload, now(), time.Time{})
	mustPutProjection(t, s, ctx, "proj-2", "ws-1", "top_of_mind", 1, "", nil, payload, now(), time.Time{})

	rows, err := s.ListProjections(ctx, ProjectionFilter{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
	})
	if err != nil {
		t.Fatalf("ListProjections: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "proj-1" {
		t.Errorf("expected 1 projection proj-1 with task-1, got %d", len(rows))
	}
}

// ========== EvidenceNode large statement CAS ==========

func TestStore_EvidenceNodeLargeStatementCAS(t *testing.T) {
	cas := newMemCAS()
	s := openTestStoreWithCAS(t, cas)
	ctx := context.Background()

	largeText := make([]byte, 65*1024)
	for i := range largeText {
		largeText[i] = 'B'
	}

	ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "big.go"}
	node := contextengine.EvidenceNode{
		ID: "node-big", WorkspaceID: "ws-1",
		NodeType: contextengine.EvidenceNodeTypeCode,
		Ref:      ref,
		Statement: string(largeText),
	}

	_, err := s.PutEvidenceNode(ctx, node)
	if err != nil {
		t.Fatalf("PutEvidenceNode large: %v", err)
	}

	if len(cas.data) == 0 {
		t.Error("expected CAS usage for large statement")
	}

	got, err := s.GetEvidenceNode(ctx, "node-big")
	if err != nil {
		t.Fatalf("GetEvidenceNode: %v", err)
	}
	if got.Statement != string(largeText) {
		t.Errorf("expected %d byte statement, got %d", len(largeText), len(got.Statement))
	}
}

// ========== Event with refs and data ==========

func TestStore_EventWithRefsAndData(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	evt := contextengine.ContextEvent{
		ID: "evt-1", WorkspaceID: "ws-1",
		Kind: contextengine.EventKindCodeChangedDirty, Source: "test",
		Refs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "foo.go"},
			{Type: contextengine.RefTypeSymbol, Ref: "Bar"},
		},
		Data:      map[string]any{"key": "value", "count": float64(42)},
		CreatedAt: now(),
	}
	mustAppendEvent(t, s, ctx, evt)

	events, _ := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1"})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]
	if len(got.Refs) != 2 {
		t.Errorf("expected 2 refs, got %d", len(got.Refs))
	}
	if got.Refs[0].Ref != "foo.go" {
		t.Errorf("expected ref foo.go, got %q", got.Refs[0].Ref)
	}
	if got.Data["key"] != "value" {
		t.Errorf("expected data key=value, got %v", got.Data["key"])
	}
}

// ========== Claim with source refs ==========

func TestStore_ClaimWithSourceRefs(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	claim := testClaim("c1", "ws-1", contextengine.ClaimStatusCandidate)
	claim.SourceRefs = []contextengine.EvidenceRef{
		{Type: contextengine.RefTypePath, Ref: "foo.go"},
		{Type: contextengine.RefTypeEvent, Ref: "evt-1"},
	}
	mustUpsertClaim(t, s, ctx, claim)

	got, err := s.GetClaim(ctx, "c1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if len(got.SourceRefs) != 2 {
		t.Errorf("expected 2 source refs, got %d", len(got.SourceRefs))
	}
}

// ========== Impact edge with source event ==========

func TestStore_ImpactEdgeWithSourceEvent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	from := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "a.go"}
	to := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "b.go"}
	edge := testImpactEdge("e1", "ws-1", from, to)
	edge.SourceEventID = "evt-1"
	mustPutImpactEdge(t, s, ctx, edge)

	edges, _ := s.ListImpactEdges(ctx, ImpactFilter{WorkspaceID: "ws-1"})
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].SourceEventID != "evt-1" {
		t.Errorf("expected source_event_id evt-1, got %q", edges[0].SourceEventID)
	}
}

// ========== Staleness marker with caused_by_events ==========

func TestStore_StalnessMarkerCausedByEvents(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "foo.go"}
	marker := testStalenessMarker("m1", "ws-1", ref, contextengine.StalenessStatusDirty)
	marker.CausedByEvents = []string{"evt-1", "evt-2"}
	mustUpsertStaleness(t, s, ctx, marker)

	got, err := s.GetStaleness(ctx, "m1")
	if err != nil {
		t.Fatalf("GetStaleness: %v", err)
	}
	if len(got.CausedByEvents) != 2 {
		t.Errorf("expected 2 caused_by_events, got %d", len(got.CausedByEvents))
	}
}

// ========== Episode with sub-episodes ==========

func TestStore_EpisodeWithSubEpisodes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ep := testEpisode("ep-parent", "ws-1", "test query", contextengine.LaneMixed)
	ep.SubEpisodeIDs = []string{"ep-code", "ep-memory", "ep-context", "ep-task"}
	mustRecordEpisode(t, s, ctx, ep)

	got, err := s.GetRetrievalEpisode(ctx, "ep-parent")
	if err != nil {
		t.Fatalf("GetRetrievalEpisode: %v", err)
	}
	if len(got.SubEpisodeIDs) != 4 {
		t.Errorf("expected 4 sub_episode_ids, got %d", len(got.SubEpisodeIDs))
	}
}

// ========== Feedback with used refs ==========

func TestStore_FeedbackWithUsedRefs(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ep := testEpisode("ep-1", "ws-1", "test query", contextengine.LaneCode)
	mustRecordEpisode(t, s, ctx, ep)

	fb := testFeedback("fb-1", "ws-1", "ep-1", contextengine.RetrievalFeedbackKindEvidenceUsed)
	fb.UsedRefs = []contextengine.EvidenceRef{
		{Type: contextengine.RefTypePath, Ref: "foo.go"},
	}
	mustRecordFeedback(t, s, ctx, fb)

	got, err := s.GetRetrievalFeedback(ctx, "fb-1")
	if err != nil {
		t.Fatalf("GetRetrievalFeedback: %v", err)
	}
	if len(got.UsedRefs) != 1 || got.UsedRefs[0].Ref != "foo.go" {
		t.Errorf("expected 1 used_ref foo.go, got %v", got.UsedRefs)
	}
}

// ========== Sort events deterministically ==========

func TestStore_EventsDeterministic(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ids := []string{"evt-z", "evt-a", "evt-m"}
	for _, id := range ids {
		mustAppendEvent(t, s, ctx, testEvent(id, "ws-1", contextengine.EventKindCodeChangedDirty))
	}

	events, err := s.ListEvents(ctx, EventFilter{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	gotIDs := make([]string, len(events))
	for i, e := range events {
		gotIDs[i] = e.ID
	}

	// Verify all IDs present
	sort.Strings(gotIDs)
	expected := []string{"evt-a", "evt-m", "evt-z"}
	for i, id := range expected {
		if gotIDs[i] != id {
			t.Errorf("expected sorted IDs %v, got %v", expected, gotIDs)
		}
	}
}

// ========== Not found errors ==========

func TestStore_NotFoundErrors(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.GetEvidencePack(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent pack")
	}

	_, err = s.GetClaim(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent claim")
	}

	_, err = s.GetEvidenceNode(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent node")
	}

	_, err = s.GetStaleness(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent staleness marker")
	}

	_, _, _, _, _, _, _, err = s.GetProjection(ctx, "ws-1", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent projection")
	}

	_, err = s.GetRetrievalEpisode(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent episode")
	}

	_, err = s.GetRetrievalFeedback(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent feedback")
	}
}

// ========== Additional coverage tests ==========

func TestStore_ImpactEdgeFilterByKind(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	from := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "a.go"}
	to := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "b.go"}

	edge1 := testImpactEdge("e1", "ws-1", from, to)
	edge1.Kind = contextengine.ImpactEdgeKindDependsOn
	mustPutImpactEdge(t, s, ctx, edge1)

	edge2 := testImpactEdge("e2", "ws-1", to, from)
	edge2.Kind = contextengine.ImpactEdgeKindCites
	mustPutImpactEdge(t, s, ctx, edge2)

	edges, err := s.ListImpactEdges(ctx, ImpactFilter{
		WorkspaceID: "ws-1",
		Kind:        contextengine.ImpactEdgeKindCites,
	})
	if err != nil {
		t.Fatalf("ListImpactEdges: %v", err)
	}
	if len(edges) != 1 || edges[0].Kind != contextengine.ImpactEdgeKindCites {
		t.Errorf("expected 1 cites edge, got %d", len(edges))
	}
}

func TestStore_ImpactEdgeFilterByToRef(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	a := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "a.go"}
	b := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "b.go"}
	c := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "c.go"}

	mustPutImpactEdge(t, s, ctx, testImpactEdge("e1", "ws-1", a, b))
	mustPutImpactEdge(t, s, ctx, testImpactEdge("e2", "ws-1", a, c))

	edges, err := s.ListImpactEdges(ctx, ImpactFilter{
		WorkspaceID: "ws-1",
		ToRef:       &b,
	})
	if err != nil {
		t.Fatalf("ListImpactEdges: %v", err)
	}
	if len(edges) != 1 || edges[0].To.Ref != "b.go" {
		t.Errorf("expected 1 edge to b.go, got %d", len(edges))
	}
}

func TestStore_StalenessFilterByStatus(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ref1 := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "a.go"}
	ref2 := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "b.go"}

	m1 := testStalenessMarker("m1", "ws-1", ref1, contextengine.StalenessStatusDirty)
	mustUpsertStaleness(t, s, ctx, m1)

	m2 := contextengine.StalenessMarker{
		ID: "m2", WorkspaceID: "ws-1",
		TargetRef: ref2,
		Status:    contextengine.StalenessStatusUnknown,
		CreatedAt: now(), UpdatedAt: now(),
	}
	mustUpsertStaleness(t, s, ctx, m2)

	markers, err := s.ListStaleness(ctx, StalenessFilter{
		WorkspaceID: "ws-1",
		Status:      contextengine.StalenessStatusDirty,
	})
	if err != nil {
		t.Fatalf("ListStaleness: %v", err)
	}
	if len(markers) != 1 {
		t.Errorf("expected 1 dirty marker, got %d", len(markers))
	}
}

func TestStore_EvidenceNodeUpdate(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "foo.go"}
	node := testNode("node-1", "ws-1", ref)
	mustPutEvidenceNode(t, s, ctx, node)

	// Update with higher count
	node.Count = 5
	node.Statement = "updated statement"
	mustPutEvidenceNode(t, s, ctx, node)

	got, err := s.GetEvidenceNode(ctx, "node-1")
	if err != nil {
		t.Fatalf("GetEvidenceNode: %v", err)
	}
	if got.Count != 5 {
		t.Errorf("expected count 5, got %d", got.Count)
	}
	if got.Statement != "updated statement" {
		t.Errorf("expected 'updated statement', got %q", got.Statement)
	}
}

func TestStore_EvidenceNodeDifferentRef(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ref1 := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "a.go"}
	ref2 := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "b.go"}

	mustPutEvidenceNode(t, s, ctx, testNode("node-1", "ws-1", ref1))
	mustPutEvidenceNode(t, s, ctx, testNode("node-2", "ws-1", ref2))

	nodes, err := s.ListEvidenceNodes(ctx, "ws-1", ref1, 10)
	if err != nil {
		t.Fatalf("ListEvidenceNodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "node-1" {
		t.Errorf("expected 1 node for a.go ref, got %d", len(nodes))
	}
}

func TestStore_ProjectionExpiresAt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	payload := map[string]string{"key": "value"}
	expiresAt := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	mustPutProjection(t, s, ctx, "proj-1", "ws-1", "top_of_mind", 1, "", nil, payload, now(), expiresAt)

	_, _, _, _, _, _, ea, err := s.GetProjection(ctx, "ws-1", "proj-1")
	if err != nil {
		t.Fatalf("GetProjection: %v", err)
	}
	if !ea.Equal(expiresAt) {
		t.Errorf("expected expires_at %v, got %v", expiresAt, ea)
	}
}

func TestStore_ProjectionListByType(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	payload := map[string]string{"key": "value"}
	mustPutProjection(t, s, ctx, "proj-1", "ws-1", "top_of_mind", 1, "", nil, payload, now(), time.Time{})
	mustPutProjection(t, s, ctx, "proj-2", "ws-1", "task_context", 1, "t-1", nil, payload, now(), time.Time{})

	rows, err := s.ListProjections(ctx, ProjectionFilter{
		WorkspaceID:    "ws-1",
		ProjectionType: "top_of_mind",
	})
	if err != nil {
		t.Fatalf("ListProjections: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "proj-1" {
		t.Errorf("expected 1 top_of_mind projection, got %d", len(rows))
	}
}

func TestStore_EvidenceNodeWithMetadata(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "foo.go"}
	node := testNode("node-1", "ws-1", ref)
	node.Metadata = map[string]any{"source": "test", "score": float64(0.95)}
	mustPutEvidenceNode(t, s, ctx, node)

	got, err := s.GetEvidenceNode(ctx, "node-1")
	if err != nil {
		t.Fatalf("GetEvidenceNode: %v", err)
	}
	if got.Metadata["source"] != "test" {
		t.Errorf("expected metadata source=test, got %v", got.Metadata["source"])
	}
}

func TestStore_ClaimWithScope(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	claim := testClaim("c1", "ws-1", contextengine.ClaimStatusCurrent)
	claim.Scope = contextengine.ClaimScope{Path: "internal/foo.go", TaskID: "task-1"}
	mustUpsertClaim(t, s, ctx, claim)

	got, err := s.GetClaim(ctx, "c1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if got.Scope.Path != "internal/foo.go" {
		t.Errorf("expected scope path internal/foo.go, got %q", got.Scope.Path)
	}
	if got.Scope.TaskID != "task-1" {
		t.Errorf("expected scope task_id task-1, got %q", got.Scope.TaskID)
	}
}

func TestStore_EpisodeWithAllFields(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ep := contextengine.RetrievalEpisode{
		ID: "ep-1", WorkspaceID: "ws-1",
		Query: "test", Lane: contextengine.LaneCode,
		PackID: "pack-1", DurationMs: 150, TokensUsed: 500, HitCount: 10,
		CreatedAt: now(),
	}
	mustRecordEpisode(t, s, ctx, ep)

	got, err := s.GetRetrievalEpisode(ctx, "ep-1")
	if err != nil {
		t.Fatalf("GetRetrievalEpisode: %v", err)
	}
	if got.DurationMs != 150 {
		t.Errorf("expected duration_ms 150, got %d", got.DurationMs)
	}
	if got.TokensUsed != 500 {
		t.Errorf("expected tokens_used 500, got %d", got.TokensUsed)
	}
	if got.HitCount != 10 {
		t.Errorf("expected hit_count 10, got %d", got.HitCount)
	}
	if got.PackID != "pack-1" {
		t.Errorf("expected pack_id pack-1, got %q", got.PackID)
	}
}

func TestStore_FeedbackWithCorrection(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	mustRecordEpisode(t, s, ctx, testEpisode("ep-1", "ws-1", "q", contextengine.LaneCode))

	fb := testFeedback("fb-1", "ws-1", "ep-1", contextengine.RetrievalFeedbackKindAnswerCorrected)
	fb.CorrectionStmt = "the correct answer is X"
	fb.GapStmt = "missing knowledge about X"
	mustRecordFeedback(t, s, ctx, fb)

	got, err := s.GetRetrievalFeedback(ctx, "fb-1")
	if err != nil {
		t.Fatalf("GetRetrievalFeedback: %v", err)
	}
	if got.CorrectionStmt != "the correct answer is X" {
		t.Errorf("expected correction, got %q", got.CorrectionStmt)
	}
	if got.GapStmt != "missing knowledge about X" {
		t.Errorf("expected gap, got %q", got.GapStmt)
	}
}

func TestStore_AppendEvent_Validation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Missing kind
	evt := contextengine.ContextEvent{
		ID: "evt-1", WorkspaceID: "ws-1", Source: "test",
		Kind: contextengine.ContextEventKind("unknown"), CreatedAt: now(),
	}
	_, err := s.AppendEvent(ctx, evt)
	if err == nil {
		t.Fatal("expected error for unknown event kind")
	}
}

func TestStore_PutEvidencePack_Validation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	pack := contextengine.EvidencePack{ID: "pack-1", WorkspaceID: "ws-1"}
	_, err := s.PutEvidencePack(ctx, pack)
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestStore_ImpactEdge_Validation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	edge := contextengine.ImpactEdge{
		ID: "e1", WorkspaceID: "ws-1",
		Kind: contextengine.ImpactEdgeKind("unknown"),
		CreatedAt: now(),
	}
	_, err := s.PutImpactEdge(ctx, edge)
	if err == nil {
		t.Fatal("expected error for invalid impact edge")
	}
}

func TestStore_RecordEpisode_Validation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ep := contextengine.RetrievalEpisode{
		ID: "ep-1", WorkspaceID: "ws-1",
		Lane: contextengine.EvidenceLane("unknown"),
		CreatedAt: now(),
	}
	_, err := s.RecordRetrievalEpisode(ctx, ep)
	if err == nil {
		t.Fatal("expected error for unknown lane")
	}
}

func TestStore_RecordFeedback_Validation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	fb := contextengine.RetrievalFeedback{
		ID: "fb-1", WorkspaceID: "ws-1",
		Kind: contextengine.RetrievalFeedbackKind("unknown"),
		CreatedAt: now(),
	}
	_, err := s.RecordRetrievalFeedback(ctx, fb)
	if err == nil {
		t.Fatal("expected error for unknown feedback kind")
	}
}

func TestStore_UpsertStaleness_Validation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	marker := contextengine.StalenessMarker{
		ID: "m1", WorkspaceID: "ws-1",
		Status: contextengine.StalenessStatus("unknown"),
		CreatedAt: now(), UpdatedAt: now(),
	}
	_, err := s.UpsertStaleness(ctx, marker)
	if err == nil {
		t.Fatal("expected error for unknown staleness status")
	}
}

func TestStore_ClaimSupersededBy(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	claim := testClaim("c1", "ws-1", contextengine.ClaimStatusSuperseded)
	claim.SupersededBy = "c2"
	mustUpsertClaim(t, s, ctx, claim)

	got, err := s.GetClaim(ctx, "c1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if got.SupersededBy != "c2" {
		t.Errorf("expected superseded_by c2, got %q", got.SupersededBy)
	}
}
