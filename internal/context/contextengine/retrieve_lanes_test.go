package contextengine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Helpers ---

var (
	testNow       = time.Date(2025, 3, 15, 10, 0, 0, 0, time.UTC)
	testIDCounter int64
)

func testIDGen() string {
	n := atomic.AddInt64(&testIDCounter, 1)
	return fmt.Sprintf("test-id-%d", n)
}

func testClock() time.Time { return testNow }

func testLaneConfig() LaneConfig {
	return LaneConfig{
		Store:       NewMemoryStore(),
		IDGen:       testIDGen,
		Clock:       testClock,
		WorkspaceID: "ws-test",
	}
}

func resetTestIDCounter() {
	atomic.StoreInt64(&testIDCounter, 0)
}

// --- VAL-RETR-010: All lanes reject empty query ---

func TestRetrieveCode_EmptyQuery(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()
	_, err := RetrieveCode(context.Background(), cfg, nil, "")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	var eqe EmptyQueryError
	if !errors.As(err, &eqe) {
		t.Fatalf("expected EmptyQueryError, got %T: %v", err, err)
	}
	if eqe.Lane != LaneCode {
		t.Errorf("expected lane %q, got %q", LaneCode, eqe.Lane)
	}
	if !errors.Is(err, ErrEmptyQuery) {
		t.Error("expected ErrEmptyQuery in chain")
	}
}

func TestRetrieveMemory_EmptyQuery(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()
	_, err := RetrieveMemory(context.Background(), cfg, nil, "")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	var eqe EmptyQueryError
	if !errors.As(err, &eqe) {
		t.Fatalf("expected EmptyQueryError, got %T: %v", err, err)
	}
	if eqe.Lane != LaneMemory {
		t.Errorf("expected lane %q, got %q", LaneMemory, eqe.Lane)
	}
}

func TestRetrieveContext_EmptyQuery(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()
	_, err := RetrieveContext(context.Background(), cfg, nil, "")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	var eqe EmptyQueryError
	if !errors.As(err, &eqe) {
		t.Fatalf("expected EmptyQueryError, got %T: %v", err, err)
	}
}

func TestRetrieveTask_EmptyQuery(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()
	_, err := RetrieveTask(context.Background(), cfg, nil, nil, "task-1", "")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	var eqe EmptyQueryError
	if !errors.As(err, &eqe) {
		t.Fatalf("expected EmptyQueryError, got %T: %v", err, err)
	}
}

func TestRetrieveMixed_EmptyQuery(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()
	_, err := RetrieveMixed(context.Background(), cfg, nil, nil, nil, nil, nil, "task-1", "")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	var eqe EmptyQueryError
	if !errors.As(err, &eqe) {
		t.Fatalf("expected EmptyQueryError, got %T: %v", err, err)
	}
	if eqe.Lane != LaneMixed {
		t.Errorf("expected lane %q, got %q", LaneMixed, eqe.Lane)
	}
}

// Table-driven test for all lanes rejecting empty query.
func TestAllLanes_EmptyQuery(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	tests := []struct {
		name string
		lane EvidenceLane
		fn   func() (EvidencePack, error)
	}{
		{"code", LaneCode, func() (EvidencePack, error) {
			return RetrieveCode(context.Background(), cfg, func(_ context.Context, _ string) ([]CodeSearchHit, error) {
				return nil, nil
			}, "")
		}},
		{"memory", LaneMemory, func() (EvidencePack, error) {
			return RetrieveMemory(context.Background(), cfg, func(_ context.Context, _, _ string) ([]MemoryClaim, error) {
				return nil, nil
			}, "")
		}},
		{"context", LaneContext, func() (EvidencePack, error) {
			return RetrieveContext(context.Background(), cfg, func(_ context.Context, _ string) (*ContextPacket, error) {
				return nil, nil
			}, "")
		}},
		{"task", LaneTask, func() (EvidencePack, error) {
			return RetrieveTask(context.Background(), cfg, func(_ context.Context, _, _ string) (*TaskContext, error) {
				return nil, nil
			}, func(_ context.Context, _ string) ([]string, error) {
				return nil, nil
			}, "", "")
		}},
		{"mixed", LaneMixed, func() (EvidencePack, error) {
			return RetrieveMixed(context.Background(), cfg,
				func(_ context.Context, _ string) ([]CodeSearchHit, error) { return nil, nil },
				func(_ context.Context, _, _ string) ([]MemoryClaim, error) { return nil, nil },
				func(_ context.Context, _ string) (*ContextPacket, error) { return nil, nil },
				func(_ context.Context, _, _ string) (*TaskContext, error) { return nil, nil },
				func(_ context.Context, _ string) ([]string, error) { return nil, nil },
				"", "")
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.fn()
			if err == nil {
				t.Fatalf("expected error for empty query on lane %s", tc.name)
			}
			var eqe EmptyQueryError
			if !errors.As(err, &eqe) {
				t.Errorf("expected EmptyQueryError, got %T: %v", err, err)
			}
			if eqe.Lane != tc.lane {
				t.Errorf("expected lane %q, got %q", tc.lane, eqe.Lane)
			}
			if !errors.Is(err, ErrEmptyQuery) {
				t.Error("expected ErrEmptyQuery in chain")
			}
		})
	}
}

// --- VAL-RETR-001: retrieve_code returns EvidencePack ---

func TestRetrieveCode_ReturnsEvidencePack(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	searchFn := func(_ context.Context, query string) ([]CodeSearchHit, error) {
		return []CodeSearchHit{
			{Path: "internal/auth/handler.go", Snippet: "func HandleAuth() {", Line: 42, Symbol: "HandleAuth", Score: 0.95, Language: "go"},
			{Path: "internal/auth/middleware.go", Snippet: "func AuthMiddleware() {", Line: 15, Score: 0.8, Language: "go"},
		}, nil
	}

	pack, err := RetrieveCode(context.Background(), cfg, searchFn, "how does auth work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.Lane != LaneCode {
		t.Errorf("expected lane %q, got %q", LaneCode, pack.Lane)
	}
	if pack.Query != "how does auth work" {
		t.Errorf("expected query preserved, got %q", pack.Query)
	}
	if pack.WorkspaceID != "ws-test" {
		t.Errorf("expected workspace_id ws-test, got %q", pack.WorkspaceID)
	}
	if len(pack.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(pack.Nodes))
	}

	// First node should have symbol ref.
	if pack.Nodes[0].Ref.Type != RefTypeSymbol {
		t.Errorf("expected symbol ref, got %q", pack.Nodes[0].Ref.Type)
	}
	if pack.Nodes[0].Ref.Ref != "HandleAuth" {
		t.Errorf("expected HandleAuth ref, got %q", pack.Nodes[0].Ref.Ref)
	}
	if pack.Nodes[0].NodeType != EvidenceNodeTypeCode {
		t.Errorf("expected code node type, got %q", pack.Nodes[0].NodeType)
	}

	// Second node should have path ref (no symbol).
	if pack.Nodes[1].Ref.Type != RefTypePath {
		t.Errorf("expected path ref, got %q", pack.Nodes[1].Ref.Type)
	}

	// Pack should validate.
	if err := pack.Validate(); err != nil {
		t.Errorf("pack validation failed: %v", err)
	}
}

// --- VAL-RETR-011: Code lane wraps code_search_ensemble ---

func TestRetrieveCode_WrapsCodeSearchEnsemble(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	called := false
	searchFn := func(_ context.Context, query string) ([]CodeSearchHit, error) {
		called = true
		if query != "auth middleware" {
			t.Errorf("expected query passed through, got %q", query)
		}
		return []CodeSearchHit{{Path: "auth.go", Snippet: "code", Score: 0.9}}, nil
	}

	pack, err := RetrieveCode(context.Background(), cfg, searchFn, "auth middleware")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected search function to be called")
	}
	if pack.Lane != LaneCode {
		t.Errorf("expected lane code, got %q", pack.Lane)
	}
}

// --- VAL-RETR-002: retrieve_memory returns EvidencePack ---

func TestRetrieveMemory_ReturnsEvidencePack(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	queryFn := func(_ context.Context, workspaceID, _ string) ([]MemoryClaim, error) {
		return []MemoryClaim{
			{
				ID:          "claim-1",
				WorkspaceID: "ws-test",
				ClaimType:   "preference",
				Status:      ClaimStatusCurrent,
				Summary:     "User prefers dark mode",
				Confidence:  0.9,
			},
			{
				ID:          "claim-2",
				WorkspaceID: "ws-test",
				ClaimType:   "decision",
				Status:      ClaimStatusCandidate,
				Summary:     "Use JWT for auth",
				Confidence:  0.75,
			},
		}, nil
	}

	pack, err := RetrieveMemory(context.Background(), cfg, queryFn, "dark mode preference")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.Lane != LaneMemory {
		t.Errorf("expected lane %q, got %q", LaneMemory, pack.Lane)
	}
	if len(pack.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(pack.Nodes))
	}
	if pack.Nodes[0].Ref.Type != RefTypeMemoryClaim {
		t.Errorf("expected memory_claim ref, got %q", pack.Nodes[0].Ref.Type)
	}
	if pack.Nodes[0].NodeType != EvidenceNodeTypeMemory {
		t.Errorf("expected memory node type, got %q", pack.Nodes[0].NodeType)
	}
	if err := pack.Validate(); err != nil {
		t.Errorf("pack validation failed: %v", err)
	}
}

// --- VAL-RETR-003: retrieve_context returns EvidencePack ---

func TestRetrieveContext_ReturnsEvidencePack(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	queryFn := func(_ context.Context, workspaceID string) (*ContextPacket, error) {
		return &ContextPacket{
			WorkspaceID:     "ws-test",
			Objective:       "Implement auth system",
			Phase:           "implementation",
			HardConstraints: []string{"Must use JWT"},
			Blockers:        []string{"Missing token secret"},
			NextActions:     []string{"Generate secret key"},
			RecentDecisions: []RecentDecision{
				{ID: "dec-1", Text: "Use RS256 algorithm"},
			},
			RelevantRefs: []EvidenceRef{
				{Type: RefTypePath, Ref: "internal/auth/jwt.go"},
			},
		}, nil
	}

	pack, err := RetrieveContext(context.Background(), cfg, queryFn, "auth system context")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.Lane != LaneContext {
		t.Errorf("expected lane %q, got %q", LaneContext, pack.Lane)
	}
	if len(pack.Nodes) < 1 {
		t.Fatal("expected at least 1 node (context packet)")
	}

	// First node should be the context packet (TopOfMind).
	firstNode := pack.Nodes[0]
	if firstNode.NodeType != EvidenceNodeTypeContext {
		t.Errorf("expected context node type, got %q", firstNode.NodeType)
	}
	if firstNode.Statement != "Implement auth system" {
		t.Errorf("expected objective as statement, got %q", firstNode.Statement)
	}
	if firstNode.Grounding != GroundingLoaded {
		t.Errorf("expected loaded grounding, got %q", firstNode.Grounding)
	}

	if err := pack.Validate(); err != nil {
		t.Errorf("pack validation failed: %v", err)
	}
}

// --- VAL-RETR-013: Context lane includes TopOfMind node ---

func TestRetrieveContext_IncludesTopOfMindNode(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	queryFn := func(_ context.Context, _ string) (*ContextPacket, error) {
		return &ContextPacket{
			WorkspaceID: "ws-test",
			Objective:   "Build retrieval lanes",
			Phase:       "testing",
		}, nil
	}

	pack, err := RetrieveContext(context.Background(), cfg, queryFn, "context query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, node := range pack.Nodes {
		if node.Ref.Type == RefTypeNote && node.Ref.Ref == "top_of_mind:ws-test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected top_of_mind node in pack")
	}
}

// --- VAL-RETR-004: retrieve_task returns EvidencePack ---

func TestRetrieveTask_ReturnsEvidencePack(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	taskQueryFn := func(_ context.Context, _, _ string) (*TaskContext, error) {
		return &TaskContext{
			WorkspaceID: "ws-test",
			TaskID:      "task-1",
			Objective:   "Implement retrieval lanes",
			Status:      "in_progress",
			RelatedCodeRefs: []EvidenceRef{
				{Type: RefTypePath, Ref: "internal/context/contextengine/retrieve_task.go"},
			},
			ProjectionMeta: ProjectionMeta{
				ProjectionID:      "proj-1",
				ProjectionType:    "task_context",
				ProjectionVersion: 1,
				WorkspaceID:       "ws-test",
			},
		}, nil
	}

	taskListFn := func(_ context.Context, _ string) ([]string, error) {
		return []string{"task-1"}, nil
	}

	pack, err := RetrieveTask(context.Background(), cfg, taskQueryFn, taskListFn, "task-1", "task context")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.Lane != LaneTask {
		t.Errorf("expected lane %q, got %q", LaneTask, pack.Lane)
	}
	if len(pack.Nodes) < 1 {
		t.Fatal("expected at least 1 node")
	}
	// First node should have task ref.
	if pack.Nodes[0].Ref.Type != RefTypeTask {
		t.Errorf("expected task ref, got %q", pack.Nodes[0].Ref.Type)
	}
	if pack.Nodes[0].Ref.Ref != "task-1" {
		t.Errorf("expected task-1 ref, got %q", pack.Nodes[0].Ref.Ref)
	}
	if err := pack.Validate(); err != nil {
		t.Errorf("pack validation failed: %v", err)
	}
}

// --- VAL-RETR-014: Task lane includes TaskContext node ---

func TestRetrieveTask_IncludesTaskContextNode(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	taskQueryFn := func(_ context.Context, _, _ string) (*TaskContext, error) {
		return &TaskContext{
			WorkspaceID: "ws-test",
			TaskID:      "task-42",
			Objective:   "Build mixed lane fusion",
			Status:      "active",
			RelatedCodeRefs: []EvidenceRef{
				{Type: RefTypePath, Ref: "internal/context/contextengine/retrieve_mixed.go"},
			},
			ProjectionMeta: ProjectionMeta{
				ProjectionID:      "proj-1",
				ProjectionType:    "task_context",
				ProjectionVersion: 1,
				WorkspaceID:       "ws-test",
			},
		}, nil
	}

	taskListFn := func(_ context.Context, _ string) ([]string, error) {
		return nil, nil
	}

	pack, err := RetrieveTask(context.Background(), cfg, taskQueryFn, taskListFn, "task-42", "task context")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, node := range pack.Nodes {
		if node.Ref.Type == RefTypeTask && node.Ref.Ref == "task-42" {
			found = true
			if node.Statement != "Build mixed lane fusion" {
				t.Errorf("expected objective as statement, got %q", node.Statement)
			}
			break
		}
	}
	if !found {
		t.Error("expected task context node with task-42 ref")
	}
}

// --- VAL-RETR-005: retrieve_mixed fans out to all four lanes ---

func TestRetrieveMixed_FansOutToAllLanes(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	codeCalled := false
	memoryCalled := false
	contextCalled := false
	taskCalled := false

	codeFn := func(_ context.Context, _ string) ([]CodeSearchHit, error) {
		codeCalled = true
		return []CodeSearchHit{{Path: "auth.go", Snippet: "code", Score: 0.9}}, nil
	}
	memoryFn := func(_ context.Context, _, _ string) ([]MemoryClaim, error) {
		memoryCalled = true
		return []MemoryClaim{{ID: "claim-1", WorkspaceID: "ws-test", ClaimType: "preference", Status: ClaimStatusCurrent, Summary: "prefers dark mode"}}, nil
	}
	contextFn := func(_ context.Context, _ string) (*ContextPacket, error) {
		contextCalled = true
		return &ContextPacket{WorkspaceID: "ws-test", Objective: "test objective"}, nil
	}
	taskQueryFn := func(_ context.Context, _, _ string) (*TaskContext, error) {
		taskCalled = true
		return &TaskContext{
			WorkspaceID: "ws-test",
			TaskID:      "task-1",
			ProjectionMeta: ProjectionMeta{
				ProjectionID: "proj-1", ProjectionType: "task_context",
				ProjectionVersion: 1, WorkspaceID: "ws-test",
			},
		}, nil
	}
	taskListFn := func(_ context.Context, _ string) ([]string, error) {
		return []string{"task-1"}, nil
	}

	pack, err := RetrieveMixed(context.Background(), cfg, codeFn, memoryFn, contextFn, taskQueryFn, taskListFn, "task-1", "auth system")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !codeCalled {
		t.Error("expected code lane to be called")
	}
	if !memoryCalled {
		t.Error("expected memory lane to be called")
	}
	if !contextCalled {
		t.Error("expected context lane to be called")
	}
	if !taskCalled {
		t.Error("expected task lane to be called")
	}
	if pack.Lane != LaneMixed {
		t.Errorf("expected lane %q, got %q", LaneMixed, pack.Lane)
	}
	if len(pack.Nodes) == 0 {
		t.Error("expected non-empty nodes in mixed pack")
	}
}

// --- VAL-RETR-006: Fusion uses typed ref identity ---

func TestRetrieveMixed_FusesByRefIdentity(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	// Both code and task lanes return the same path ref.
	codeFn := func(_ context.Context, _ string) ([]CodeSearchHit, error) {
		return []CodeSearchHit{
			{Path: "internal/auth/handler.go", Snippet: "func HandleAuth()", Score: 0.9, Language: "go"},
		}, nil
	}
	memoryFn := func(_ context.Context, _, _ string) ([]MemoryClaim, error) {
		return nil, nil
	}
	contextFn := func(_ context.Context, _ string) (*ContextPacket, error) {
		return &ContextPacket{
			WorkspaceID:  "ws-test",
			Objective:    "test",
			RelevantRefs: []EvidenceRef{{Type: RefTypePath, Ref: "internal/auth/handler.go"}},
		}, nil
	}
	taskQueryFn := func(_ context.Context, _, _ string) (*TaskContext, error) {
		return &TaskContext{
			WorkspaceID:     "ws-test",
			TaskID:          "task-1",
			RelatedCodeRefs: []EvidenceRef{{Type: RefTypePath, Ref: "internal/auth/handler.go"}},
			ProjectionMeta: ProjectionMeta{
				ProjectionID: "proj-1", ProjectionType: "task_context",
				ProjectionVersion: 1, WorkspaceID: "ws-test",
			},
		}, nil
	}
	taskListFn := func(_ context.Context, _ string) ([]string, error) {
		return []string{"task-1"}, nil
	}

	pack, err := RetrieveMixed(context.Background(), cfg, codeFn, memoryFn, contextFn, taskQueryFn, taskListFn, "task-1", "auth handler")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The path "internal/auth/handler.go" should appear as a single fused node.
	pathNodeCount := 0
	for _, node := range pack.Nodes {
		if node.Ref.Type == RefTypePath && node.Ref.Ref == "internal/auth/handler.go" {
			pathNodeCount++
			// Check source_lanes metadata.
			lanes := extractLanes(node.Metadata)
			if len(lanes) < 2 {
				t.Errorf("expected at least 2 source lanes for fused node, got %v", lanes)
			}
		}
	}
	if pathNodeCount != 1 {
		t.Errorf("expected exactly 1 fused node for internal/auth/handler.go, got %d", pathNodeCount)
	}
}

// --- VAL-RETR-007: Fused nodes preserve source lanes ---

func TestRetrieveMixed_PreservesProvenance(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	codeFn := func(_ context.Context, _ string) ([]CodeSearchHit, error) {
		return []CodeSearchHit{{Path: "foo.go", Snippet: "code", Score: 0.9}}, nil
	}
	memoryFn := func(_ context.Context, _, _ string) ([]MemoryClaim, error) {
		return nil, nil
	}
	contextFn := func(_ context.Context, _ string) (*ContextPacket, error) {
		return &ContextPacket{
			WorkspaceID:  "ws-test",
			RelevantRefs: []EvidenceRef{{Type: RefTypePath, Ref: "foo.go"}},
		}, nil
	}
	taskQueryFn := func(_ context.Context, _, _ string) (*TaskContext, error) {
		return &TaskContext{
			WorkspaceID:     "ws-test",
			TaskID:          "task-1",
			RelatedCodeRefs: []EvidenceRef{{Type: RefTypePath, Ref: "foo.go"}},
			ProjectionMeta: ProjectionMeta{
				ProjectionID: "proj-1", ProjectionType: "task_context",
				ProjectionVersion: 1, WorkspaceID: "ws-test",
			},
		}, nil
	}
	taskListFn := func(_ context.Context, _ string) ([]string, error) {
		return []string{"task-1"}, nil
	}

	pack, err := RetrieveMixed(context.Background(), cfg, codeFn, memoryFn, contextFn, taskQueryFn, taskListFn, "task-1", "foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the fused node for foo.go and check provenance.
	for _, node := range pack.Nodes {
		if node.Ref.Ref == "foo.go" {
			lanes := extractLanes(node.Metadata)
			// Should have code, context, and task lanes.
			expectedLanes := map[string]bool{
				string(LaneCode):    false,
				string(LaneContext): false,
				string(LaneTask):    false,
			}
			for _, l := range lanes {
				if _, ok := expectedLanes[l]; ok {
					expectedLanes[l] = true
				}
			}
			for lane, found := range expectedLanes {
				if !found {
					t.Errorf("expected source lane %q in fused node metadata, lanes=%v", lane, lanes)
				}
			}
			return
		}
	}
	t.Error("expected to find fused node for foo.go")
}

// --- VAL-RETR-008: Each lane records RetrievalEpisode ---

func TestRetrieveCode_RecordsEpisode(t *testing.T) {
	resetTestIDCounter()
	store := NewMemoryStore()
	cfg := LaneConfig{
		Store:       store,
		IDGen:       testIDGen,
		Clock:       testClock,
		WorkspaceID: "ws-test",
	}

	searchFn := func(_ context.Context, _ string) ([]CodeSearchHit, error) {
		return []CodeSearchHit{{Path: "auth.go", Snippet: "code", Score: 0.9}}, nil
	}

	_, err := RetrieveCode(context.Background(), cfg, searchFn, "auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	episodes := listAllEpisodes(store)
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}
	ep := episodes[0]
	if ep.Lane != LaneCode {
		t.Errorf("expected lane code, got %q", ep.Lane)
	}
	if ep.Query != "auth" {
		t.Errorf("expected query auth, got %q", ep.Query)
	}
	if ep.WorkspaceID != "ws-test" {
		t.Errorf("expected workspace ws-test, got %q", ep.WorkspaceID)
	}
}

func TestRetrieveMemory_RecordsEpisode(t *testing.T) {
	resetTestIDCounter()
	store := NewMemoryStore()
	cfg := LaneConfig{
		Store:       store,
		IDGen:       testIDGen,
		Clock:       testClock,
		WorkspaceID: "ws-test",
	}

	queryFn := func(_ context.Context, _, _ string) ([]MemoryClaim, error) {
		return []MemoryClaim{{ID: "c1", WorkspaceID: "ws-test", ClaimType: "pref", Status: ClaimStatusCurrent}}, nil
	}

	_, err := RetrieveMemory(context.Background(), cfg, queryFn, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	episodes := listAllEpisodes(store)
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}
	if episodes[0].Lane != LaneMemory {
		t.Errorf("expected lane memory, got %q", episodes[0].Lane)
	}
}

func TestRetrieveContext_RecordsEpisode(t *testing.T) {
	resetTestIDCounter()
	store := NewMemoryStore()
	cfg := LaneConfig{
		Store:       store,
		IDGen:       testIDGen,
		Clock:       testClock,
		WorkspaceID: "ws-test",
	}

	queryFn := func(_ context.Context, _ string) (*ContextPacket, error) {
		return &ContextPacket{WorkspaceID: "ws-test", Objective: "test"}, nil
	}

	_, err := RetrieveContext(context.Background(), cfg, queryFn, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	episodes := listAllEpisodes(store)
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}
	if episodes[0].Lane != LaneContext {
		t.Errorf("expected lane context, got %q", episodes[0].Lane)
	}
}

func TestRetrieveTask_RecordsEpisode(t *testing.T) {
	resetTestIDCounter()
	store := NewMemoryStore()
	cfg := LaneConfig{
		Store:       store,
		IDGen:       testIDGen,
		Clock:       testClock,
		WorkspaceID: "ws-test",
	}

	taskQueryFn := func(_ context.Context, _, _ string) (*TaskContext, error) {
		return &TaskContext{
			WorkspaceID: "ws-test", TaskID: "task-1",
			ProjectionMeta: ProjectionMeta{
				ProjectionID: "proj-1", ProjectionType: "task_context",
				ProjectionVersion: 1, WorkspaceID: "ws-test",
			},
		}, nil
	}
	taskListFn := func(_ context.Context, _ string) ([]string, error) {
		return []string{"task-1"}, nil
	}

	_, err := RetrieveTask(context.Background(), cfg, taskQueryFn, taskListFn, "task-1", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	episodes := listAllEpisodes(store)
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}
	if episodes[0].Lane != LaneTask {
		t.Errorf("expected lane task, got %q", episodes[0].Lane)
	}
}

// --- VAL-RETR-009: Mixed lane records parent with sub-episodes ---

func TestRetrieveMixed_RecordsParentWithSubEpisodes(t *testing.T) {
	resetTestIDCounter()
	store := NewMemoryStore()
	cfg := LaneConfig{
		Store:       store,
		IDGen:       testIDGen,
		Clock:       testClock,
		WorkspaceID: "ws-test",
	}

	codeFn := func(_ context.Context, _ string) ([]CodeSearchHit, error) {
		return []CodeSearchHit{{Path: "a.go", Snippet: "x", Score: 0.9}}, nil
	}
	memoryFn := func(_ context.Context, _, _ string) ([]MemoryClaim, error) {
		return []MemoryClaim{{ID: "c1", WorkspaceID: "ws-test", ClaimType: "t", Status: ClaimStatusCurrent}}, nil
	}
	contextFn := func(_ context.Context, _ string) (*ContextPacket, error) {
		return &ContextPacket{WorkspaceID: "ws-test", Objective: "test"}, nil
	}
	taskQueryFn := func(_ context.Context, _, _ string) (*TaskContext, error) {
		return &TaskContext{
			WorkspaceID: "ws-test", TaskID: "task-1",
			ProjectionMeta: ProjectionMeta{
				ProjectionID: "proj-1", ProjectionType: "task_context",
				ProjectionVersion: 1, WorkspaceID: "ws-test",
			},
		}, nil
	}
	taskListFn := func(_ context.Context, _ string) ([]string, error) {
		return []string{"task-1"}, nil
	}

	_, err := RetrieveMixed(context.Background(), cfg, codeFn, memoryFn, contextFn, taskQueryFn, taskListFn, "task-1", "test query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 5 episodes: 4 sub-lane + 1 parent mixed.
	episodes := listAllEpisodes(store)
	if len(episodes) != 5 {
		t.Fatalf("expected 5 episodes (4 sub + 1 parent), got %d", len(episodes))
	}

	// Find the mixed parent episode.
	var parentEpisode *RetrievalEpisode
	for i := range episodes {
		if episodes[i].Lane == LaneMixed {
			parentEpisode = &episodes[i]
			break
		}
	}
	if parentEpisode == nil {
		t.Fatal("expected a mixed lane parent episode")
	}
	if len(parentEpisode.SubEpisodeIDs) != 4 {
		t.Errorf("expected 4 sub_episode_ids, got %d", len(parentEpisode.SubEpisodeIDs))
	}
}

// --- VAL-RETR-017: Context cancellation ---

func TestRetrieveCode_ContextCancellation(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	searchFn := func(ctx context.Context, _ string) ([]CodeSearchHit, error) {
		return nil, ctx.Err()
	}

	_, err := RetrieveCode(ctx, cfg, searchFn, "test")
	if err == nil {
		t.Error("expected error with canceled context")
	}
}

// --- VAL-RETR-018: Partial lane failure degrades gracefully ---

func TestRetrieveMixed_PartialFailureDegracesGracefully(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	codeFn := func(_ context.Context, _ string) ([]CodeSearchHit, error) {
		return []CodeSearchHit{{Path: "auth.go", Snippet: "code", Score: 0.9}}, nil
	}
	memoryFn := func(_ context.Context, _, _ string) ([]MemoryClaim, error) {
		return nil, errors.New("memory store unavailable")
	}
	contextFn := func(_ context.Context, _ string) (*ContextPacket, error) {
		return &ContextPacket{WorkspaceID: "ws-test", Objective: "test"}, nil
	}
	taskQueryFn := func(_ context.Context, _, _ string) (*TaskContext, error) {
		return &TaskContext{
			WorkspaceID: "ws-test", TaskID: "task-1",
			ProjectionMeta: ProjectionMeta{
				ProjectionID: "proj-1", ProjectionType: "task_context",
				ProjectionVersion: 1, WorkspaceID: "ws-test",
			},
		}, nil
	}
	taskListFn := func(_ context.Context, _ string) ([]string, error) {
		return []string{"task-1"}, nil
	}

	pack, err := RetrieveMixed(context.Background(), cfg, codeFn, memoryFn, contextFn, taskQueryFn, taskListFn, "task-1", "auth")
	// Should NOT return an error (partial results are valid).
	if err != nil {
		t.Fatalf("expected no error for partial failure, got: %v", err)
	}
	if pack.Lane != LaneMixed {
		t.Errorf("expected lane mixed, got %q", pack.Lane)
	}
	if len(pack.Nodes) == 0 {
		t.Error("expected partial results with non-zero nodes")
	}

	// Check that lane_errors metadata is present.
	laneErrors, ok := pack.Metadata["lane_errors"]
	if !ok {
		t.Error("expected lane_errors in metadata")
	} else {
		errList := laneErrors.([]string)
		if len(errList) == 0 {
			t.Error("expected at least one lane error")
		}
	}
}

// --- Memory lane forwards query string to MemoryQueryFunc ---

func TestRetrieveMemory_PassesQueryToFunc(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	all := []MemoryClaim{
		{ID: "c-dark", WorkspaceID: "ws-test", ClaimType: "preference", Status: ClaimStatusCurrent, Summary: "User prefers DARK mode"},
		{ID: "c-jwt", WorkspaceID: "ws-test", ClaimType: "decision", Status: ClaimStatusCurrent, Summary: "Use JWT for auth"},
		{ID: "c-sqlite", WorkspaceID: "ws-test", ClaimType: "decision", Status: ClaimStatusCurrent, Summary: "Use SQLite for storage"},
	}
	var seenQuery string
	queryFn := func(_ context.Context, _, query string) ([]MemoryClaim, error) {
		seenQuery = query
		q := strings.ToLower(query)
		if q == "" {
			return all, nil
		}
		out := make([]MemoryClaim, 0, len(all))
		for _, c := range all {
			if strings.Contains(strings.ToLower(c.Summary), q) {
				out = append(out, c)
			}
		}
		return out, nil
	}

	pack, err := RetrieveMemory(context.Background(), cfg, queryFn, "dark")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seenQuery != "dark" {
		t.Errorf("expected queryFn to receive query \"dark\", got %q", seenQuery)
	}
	if len(pack.Nodes) != 1 {
		t.Fatalf("expected 1 filtered node, got %d", len(pack.Nodes))
	}
	if pack.Nodes[0].Ref.Ref != "c-dark" {
		t.Errorf("expected c-dark to survive filter, got ref %q", pack.Nodes[0].Ref.Ref)
	}
}

// --- VAL-RETR-012: Memory lane uses direct store queries ---

func TestRetrieveMemory_DirectStoreQueries(t *testing.T) {
	resetTestIDCounter()
	cfg := testLaneConfig()

	queryCalled := false
	queryFn := func(_ context.Context, workspaceID, _ string) ([]MemoryClaim, error) {
		queryCalled = true
		if workspaceID != "ws-test" {
			t.Errorf("expected workspace ws-test, got %q", workspaceID)
		}
		return []MemoryClaim{
			{ID: "c1", WorkspaceID: "ws-test", ClaimType: "preference", Status: ClaimStatusCurrent, Summary: "Uses SQLite"},
		}, nil
	}

	pack, err := RetrieveMemory(context.Background(), cfg, queryFn, "sqlite storage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !queryCalled {
		t.Error("expected direct query function to be called")
	}
	if len(pack.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(pack.Nodes))
	}
}

// --- Fusion tests ---

func TestFuseNodes_DeduplicatesByRefIdentity(t *testing.T) {
	nodes := []EvidenceNode{
		{
			ID:          "n1",
			WorkspaceID: "ws",
			NodeType:    EvidenceNodeTypeCode,
			Ref:         EvidenceRef{Type: RefTypePath, Ref: "foo.go"},
			Statement:   "from code lane",
			Confidence:  0.9,
		},
		{
			ID:          "n2",
			WorkspaceID: "ws",
			NodeType:    EvidenceNodeTypeTask,
			Ref:         EvidenceRef{Type: RefTypePath, Ref: "foo.go"},
			Statement:   "from task lane",
			Confidence:  0.7,
		},
		{
			ID:          "n3",
			WorkspaceID: "ws",
			NodeType:    EvidenceNodeTypeMemory,
			Ref:         EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-1"},
			Statement:   "memory claim",
			Confidence:  0.8,
		},
	}

	fused := fuseNodes(nodes)
	if len(fused) != 2 {
		t.Fatalf("expected 2 fused nodes, got %d", len(fused))
	}

	// Find the foo.go node.
	var fooNode *EvidenceNode
	for i := range fused {
		if fused[i].Ref.Ref == "foo.go" {
			fooNode = &fused[i]
			break
		}
	}
	if fooNode == nil {
		t.Fatal("expected foo.go node")
	}
	lanes := extractLanes(fooNode.Metadata)
	if len(lanes) != 2 {
		t.Errorf("expected 2 source lanes, got %v", lanes)
	}
	// Confidence should be the max of both.
	if fooNode.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", fooNode.Confidence)
	}
}

// --- Helper ---

func listAllEpisodes(store *MemoryStore) []RetrievalEpisode {
	ctx := context.Background()
	episodes, _ := store.ListRetrievalEpisodes(ctx)
	return episodes
}
