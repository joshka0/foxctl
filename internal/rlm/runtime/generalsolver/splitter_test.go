package generalsolver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAnalyzeForSplit_BelowThreshold(t *testing.T) {
	item := WorkItem{
		ID:        "n1",
		Goal:      "small task",
		Archetype: ArchetypeExplicitDAG,
		DependsOn: []string{},
		Status:    StatusReady,
		Priority:  1.0,
		Payload: map[string]any{
			"query": "what is 2+2",
		},
	}
	plan := AnalyzeForSplit(item)
	if plan.Strategy != SplitStrategyNone {
		t.Fatalf("expected none, got %q", plan.Strategy)
	}
}

func TestAnalyzeForSplit_CountBased_BindingsUnderThreshold(t *testing.T) {
	// 8 bindings is AT threshold, should NOT split (need > 8).
	bindings := make([]any, SplitMinBindings)
	for i := range bindings {
		bindings[i] = map[string]any{"var": fmt.Sprintf("x%d", i)}
	}
	item := WorkItem{
		ID:        "n1",
		Goal:      "typecheck bindings",
		Archetype: ArchetypeSymbolicTrace,
		Payload:   map[string]any{"bindings": bindings},
	}
	plan := AnalyzeForSplit(item)
	if plan.Strategy != SplitStrategyNone {
		t.Fatalf("expected none for %d bindings (at threshold), got %q", SplitMinBindings, plan.Strategy)
	}
}

func TestAnalyzeForSplit_CountBased_BindingsOverThreshold(t *testing.T) {
	// 9 bindings exceeds threshold, should split regardless of size.
	bindings := make([]any, SplitMinBindings+1)
	for i := range bindings {
		bindings[i] = map[string]any{"var": fmt.Sprintf("x%d", i)}
	}
	item := WorkItem{
		ID:        "n1",
		Goal:      "typecheck bindings",
		Archetype: ArchetypeSymbolicTrace,
		Payload:   map[string]any{"bindings": bindings},
	}
	plan := AnalyzeForSplit(item)
	if plan.Strategy != SplitStrategyQueryDecomposition {
		t.Fatalf("expected query_decomposition for %d bindings, got %q", SplitMinBindings+1, plan.Strategy)
	}
	if !strings.Contains(plan.Reason, "bindings:") {
		t.Fatalf("reason should mention bindings: %q", plan.Reason)
	}
}

func TestAnalyzeForSplit_CountBased_QueriesOverThreshold(t *testing.T) {
	// 3 queries exceeds threshold (> 1), even if small.
	queries := []map[string]any{
		{"id": "q1", "prompt": "x"},
		{"id": "q2", "prompt": "y"},
		{"id": "q3", "prompt": "z"},
	}
	item := WorkItem{
		ID:        "n1",
		Goal:      "solve 3 queries",
		Archetype: ArchetypeExplicitDAG,
		Payload:   map[string]any{"queries": queries},
	}
	plan := AnalyzeForSplit(item)
	if plan.Strategy != SplitStrategyQueryDecomposition {
		t.Fatalf("expected query_decomposition for 3 queries, got %q", plan.Strategy)
	}
	if plan.ChunkCount != 3 {
		t.Fatalf("expected 3 chunks, got %d", plan.ChunkCount)
	}
}

func TestAnalyzeForSplit_MultiQuery(t *testing.T) {
	queries := make([]map[string]any, 20)
	for i := range queries {
		queries[i] = map[string]any{
			"id":     fmt.Sprintf("q%d", i),
			"prompt": string(make([]byte, 3000)), // 3KB each
		}
	}
	payload := map[string]any{
		"queries": queries,
	}
	item := WorkItem{
		ID:        "n1",
		Goal:      "solve 20 queries",
		Archetype: ArchetypeExplicitDAG,
		DependsOn: []string{},
		Status:    StatusReady,
		Priority:  1.0,
		Payload:   payload,
	}
	plan := AnalyzeForSplit(item)
	if plan.Strategy != SplitStrategyQueryDecomposition {
		t.Fatalf("expected query_decomposition, got %q (reason: %s)", plan.Strategy, plan.Reason)
	}
	// 20 queries, capped at SplitMaxSubItems.
	if plan.ChunkCount != SplitMaxSubItems {
		t.Fatalf("expected %d chunks (capped), got %d", SplitMaxSubItems, plan.ChunkCount)
	}
}

func TestAnalyzeForSplit_Bindings(t *testing.T) {
	// Each binding is ~200 bytes; need >500 to exceed 50KB threshold.
	bindings := make([]any, 600)
	for i := range bindings {
		bindings[i] = map[string]any{
			"var":   fmt.Sprintf("x%d", i),
			"type":  "int",
			"expr":  string(make([]byte, 80)),
		}
	}
	payload := map[string]any{
		"bindings": bindings,
	}
	item := WorkItem{
		ID:        "n1",
		Goal:      "typecheck 600 bindings",
		Archetype: ArchetypeSymbolicTrace,
		DependsOn: []string{},
		Status:    StatusReady,
		Priority:  1.0,
		Payload:   payload,
	}
	plan := AnalyzeForSplit(item)
	if plan.Strategy != SplitStrategyQueryDecomposition {
		t.Fatalf("expected query_decomposition (from bindings), got %q (reason: %s)", plan.Strategy, plan.Reason)
	}
	if plan.ChunkCount < 2 {
		t.Fatalf("expected multiple chunks from 600 bindings, got %d", plan.ChunkCount)
	}
}

func TestAnalyzeForSplit_LargePayloadChunking(t *testing.T) {
	// Large payload with no recognizable query structure.
	bigString := string(make([]byte, 60000))
	payload := map[string]any{
		"raw_data": bigString,
	}
	item := WorkItem{
		ID:        "n1",
		Goal:      "process big data",
		Archetype: ArchetypeMixed,
		DependsOn: []string{},
		Status:    StatusReady,
		Priority:  1.0,
		Payload:   payload,
	}
	plan := AnalyzeForSplit(item)
	if plan.Strategy != SplitStrategyChunkDecomposition {
		t.Fatalf("expected chunk_decomposition, got %q (reason: %s)", plan.Strategy, plan.Reason)
	}
}

func TestApplySplit_QueryDecomposition(t *testing.T) {
	state := NewSolverState()
	// Add a downstream item that depends on the parent.
	_ = AddWorkItem(state, WorkItem{
		ID:        "child",
		Goal:      "verify results",
		Archetype: ArchetypeCandidateVerify,
		DependsOn: []string{"parent"},
		Status:    StatusPending,
		Priority:  1.0,
	})
	// Add parent.
	queries := make([]map[string]any, 5)
	for i := range queries {
		queries[i] = map[string]any{
			"id":     fmt.Sprintf("q%d", i),
			"prompt": string(make([]byte, 12000)), // 12KB each
		}
	}
	_ = AddWorkItem(state, WorkItem{
		ID:        "parent",
		Goal:      "solve 5 queries",
		Archetype: ArchetypeExplicitDAG,
		DependsOn: []string{},
		Status:    StatusReady,
		Priority:  1.0,
		Payload:   map[string]any{"queries": queries},
	})

	subIDs, err := SplitWorkItem(state, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(subIDs) != 7 { // 1 parse + 5 solve + 1 merge
		t.Fatalf("expected 7 sub-items, got %d: %v", len(subIDs), subIDs)
	}

	// Verify sub-items exist.
	parseID := "parent__parse"
	mergeID := "parent__merge"
	if _, ok := state.Items[parseID]; !ok {
		t.Fatal("parse sub-item missing")
	}
	if _, ok := state.Items[mergeID]; !ok {
		t.Fatal("merge sub-item missing")
	}
	for i := 0; i < 5; i++ {
		solveID := fmt.Sprintf("parent__solve_%02d", i)
		if _, ok := state.Items[solveID]; !ok {
			t.Fatalf("solve sub-item %q missing", solveID)
		}
	}

	// Verify parent was removed.
	if _, ok := state.Items["parent"]; ok {
		t.Fatal("parent should be removed after split")
	}

	// Verify child now depends on merge instead of parent.
	child := state.Items["child"]
	found := false
	for _, dep := range child.DependsOn {
		if dep == mergeID {
			found = true
		}
	}
	if !found {
		t.Fatalf("child should depend on %s, got deps=%v", mergeID, child.DependsOn)
	}

	// Verify parse depends on nothing (parent had no deps).
	parse := state.Items[parseID]
	if len(parse.DependsOn) != 0 {
		t.Fatalf("parse should have no deps, got %v", parse.DependsOn)
	}
	if parse.Status != StatusReady {
		t.Fatalf("parse should be ready (no deps), got %q", parse.Status)
	}

	// Verify solve items depend on parse.
	for i := 0; i < 5; i++ {
		solveID := fmt.Sprintf("parent__solve_%02d", i)
		solve := state.Items[solveID]
		if len(solve.DependsOn) != 1 || solve.DependsOn[0] != parseID {
			t.Fatalf("solve %s should depend on %s, got %v", solveID, parseID, solve.DependsOn)
		}
	}

	// Verify merge depends on all solve items.
	merge := state.Items[mergeID]
	if len(merge.DependsOn) != 5 {
		t.Fatalf("merge should depend on 5 solve items, got %d", len(merge.DependsOn))
	}

	// Validate state is consistent.
	if err := ValidateSolverState(state); err != nil {
		t.Fatalf("state validation failed: %v", err)
	}
}

func TestApplySplit_WithParentDeps(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{
		ID:        "dep1",
		Goal:      "upstream",
		Archetype: ArchetypeExplicitDAG,
		DependsOn: []string{},
		Status:    StatusSolved,
		Priority:  2.0,
	})
	payload := map[string]any{
		"queries": []map[string]any{
			{"id": "q1", "data": string(make([]byte, 30000))},
			{"id": "q2", "data": string(make([]byte, 30000))},
		},
	}
	_ = AddWorkItem(state, WorkItem{
		ID:        "parent",
		Goal:      "solve 2 queries",
		Archetype: ArchetypeExplicitDAG,
		DependsOn: []string{"dep1"},
		Status:    StatusPending,
		Priority:  1.0,
		Payload:   payload,
	})

	subIDs, err := SplitWorkItem(state, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(subIDs) != 4 { // parse + 2 solve + merge
		t.Fatalf("expected 4 sub-items, got %d", len(subIDs))
	}

	// Parse should depend on dep1 (inherited from parent).
	parse := state.Items["parent__parse"]
	if len(parse.DependsOn) != 1 || parse.DependsOn[0] != "dep1" {
		t.Fatalf("parse should depend on dep1, got %v", parse.DependsOn)
	}

	// State should be valid.
	if err := ValidateSolverState(state); err != nil {
		t.Fatalf("state validation failed: %v", err)
	}
}

func TestApplySplit_RespectsMaxSubItems(t *testing.T) {
	state := NewSolverState()
	// 30 queries — should be capped at SplitMaxSubItems (16).
	queries := make([]map[string]any, 30)
	for i := range queries {
		queries[i] = map[string]any{
			"id":     fmt.Sprintf("q%d", i),
			"prompt": string(make([]byte, 5000)),
		}
	}
	_ = AddWorkItem(state, WorkItem{
		ID:        "parent",
		Goal:      "solve 30 queries",
		Archetype: ArchetypeExplicitDAG,
		DependsOn: []string{},
		Status:    StatusReady,
		Priority:  1.0,
		Payload:   map[string]any{"queries": queries},
	})

	subIDs, err := SplitWorkItem(state, "parent")
	if err != nil {
		t.Fatal(err)
	}
	expected := 1 + SplitMaxSubItems + 1 // parse + solves + merge
	if len(subIDs) != expected {
		t.Fatalf("expected %d sub-items (capped), got %d", expected, len(subIDs))
	}
}

func TestSplitWorkItem_NoSplitNeeded(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{
		ID:        "n1",
		Goal:      "small task",
		Archetype: ArchetypeExplicitDAG,
		DependsOn: []string{},
		Status:    StatusReady,
		Priority:  1.0,
		Payload:   map[string]any{"query": "what is 2+2"},
	})

	subIDs, err := SplitWorkItem(state, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if subIDs != nil {
		t.Fatalf("expected nil (no split), got %v", subIDs)
	}
	// Original item should still exist.
	if _, ok := state.Items["n1"]; !ok {
		t.Fatal("item should still exist when no split needed")
	}
}

func TestIsSplitSubItem(t *testing.T) {
	tests := []struct {
		name   string
		item   WorkItem
		expect bool
	}{
		{
			name: "parse sub-item",
			item: WorkItem{Payload: map[string]any{"split_role": "parse"}},
			expect: true,
		},
		{
			name: "solve sub-item",
			item: WorkItem{Payload: map[string]any{"split_role": "solve"}},
			expect: true,
		},
		{
			name: "merge sub-item",
			item: WorkItem{Payload: map[string]any{"split_role": "merge"}},
			expect: true,
		},
		{
			name: "regular item",
			item: WorkItem{Payload: map[string]any{"query": "x"}},
			expect: false,
		},
		{
			name: "nil payload",
			item: WorkItem{},
			expect: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSplitSubItem(tt.item); got != tt.expect {
				t.Fatalf("expected %v, got %v", tt.expect, got)
			}
		})
	}
}

func TestSplitRole(t *testing.T) {
	if role := SplitRole(WorkItem{Payload: map[string]any{"split_role": "parse"}}); role != "parse" {
		t.Fatalf("expected parse, got %q", role)
	}
	if role := SplitRole(WorkItem{Payload: map[string]any{"query": "x"}}); role != "" {
		t.Fatalf("expected empty, got %q", role)
	}
}

func TestParentIDFromSplit(t *testing.T) {
	if pid := ParentIDFromSplit(WorkItem{Payload: map[string]any{"parent_id": "n1"}}); pid != "n1" {
		t.Fatalf("expected n1, got %q", pid)
	}
	if pid := ParentIDFromSplit(WorkItem{Payload: map[string]any{"query": "x"}}); pid != "" {
		t.Fatalf("expected empty, got %q", pid)
	}
}

func TestBuildSplitSummary(t *testing.T) {
	artifacts := map[string]WorkArtifact{
		"s1": {WorkItemID: "s1", Status: ArtifactStatusSolved, Answer: "42", Confidence: 0.95},
		"s2": {WorkItemID: "s2", Status: ArtifactStatusSolved, Answer: "hello", Confidence: 0.8},
	}
	summary := BuildSplitSummary(artifacts, []string{"s1", "s2"})
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	// Should contain both answers.
	if !contains(summary, "42") || !contains(summary, "hello") {
		t.Fatalf("summary should contain both answers: %q", summary)
	}
	// Missing artifact should show [no artifact].
	summary2 := BuildSplitSummary(artifacts, []string{"s1", "s3"})
	if !contains(summary2, "[no artifact]") {
		t.Fatalf("summary should mention missing artifact: %q", summary2)
	}
}

func TestRenderSplitMergePayload(t *testing.T) {
	artifacts := map[string]WorkArtifact{
		"s1": {WorkItemID: "s1", Status: ArtifactStatusSolved, Answer: "42", Confidence: 0.95},
	}
	payload := RenderSplitMergePayload(artifacts, []string{"s1"}, "parent goal")
	if payload["split_role"] != "merge" {
		t.Fatalf("expected merge role, got %v", payload["split_role"])
	}
	if payload["parent_goal"] != "parent goal" {
		t.Fatalf("expected parent goal, got %v", payload["parent_goal"])
	}
	if payload["solve_count"] != 1 {
		t.Fatalf("expected solve_count=1, got %v", payload["solve_count"])
	}
}

func TestEstimatePayloadSize(t *testing.T) {
	// Nil payload.
	if size := estimatePayloadSize(nil); size != 0 {
		t.Fatalf("expected 0 for nil, got %d", size)
	}
	// Small payload.
	size := estimatePayloadSize(map[string]any{"key": "value"})
	if size <= 0 {
		t.Fatal("expected positive size")
	}
	// Verify consistency with json.Marshal.
	data, _ := json.Marshal(map[string]any{"key": "value"})
	if size != len(data) {
		t.Fatalf("expected %d, got %d", len(data), size)
	}
}

func TestChunkArrayAny_CapsAtMax(t *testing.T) {
	items := make([]any, 2000)
	for i := range items {
		items[i] = fmt.Sprintf("item_%d", i)
	}
	chunks := chunkArrayAny(items, "test")
	if len(chunks) > SplitMaxSubItems {
		t.Fatalf("expected at most %d chunks, got %d", SplitMaxSubItems, len(chunks))
	}
}

func TestExtractQueryableChunks_SubProblems(t *testing.T) {
	payload := map[string]any{
		"sub_problems": []map[string]any{
			{"id": "sp1", "data": "x"},
			{"id": "sp2", "data": "y"},
			{"id": "sp3", "data": "z"},
		},
	}
	chunks := ExtractQueryableChunks(payload)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
}

func TestApplySplit_SolveItemsInheritArchetype(t *testing.T) {
	state := NewSolverState()
	queries := make([]map[string]any, 3)
	for i := range queries {
		queries[i] = map[string]any{
			"id":     fmt.Sprintf("q%d", i),
			"prompt": string(make([]byte, 20000)),
		}
	}
	_ = AddWorkItem(state, WorkItem{
		ID:        "parent",
		Goal:      "trace 3 queries",
		Archetype: ArchetypeSymbolicTrace,
		DependsOn: []string{},
		Status:    StatusReady,
		Priority:  1.0,
		Payload:   map[string]any{"queries": queries},
	})

	_, err := SplitWorkItem(state, "parent")
	if err != nil {
		t.Fatal(err)
	}

	// Solve items should inherit the parent's archetype.
	for i := 0; i < 3; i++ {
		solveID := fmt.Sprintf("parent__solve_%02d", i)
		solve := state.Items[solveID]
		if solve.Archetype != ArchetypeSymbolicTrace {
			t.Fatalf("solve %s should inherit archetype %q, got %q", solveID, ArchetypeSymbolicTrace, solve.Archetype)
		}
	}
	// Parse and merge should be explicit_dag.
	if state.Items["parent__parse"].Archetype != ArchetypeExplicitDAG {
		t.Fatalf("parse should be explicit_dag, got %q", state.Items["parent__parse"].Archetype)
	}
	if state.Items["parent__merge"].Archetype != ArchetypeExplicitDAG {
		t.Fatalf("merge should be explicit_dag, got %q", state.Items["parent__merge"].Archetype)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
