package workflow

import (
	"testing"
)

func TestNewDAG_Simple(t *testing.T) {
	steps := []Step{
		{ID: "a", Skill: "test/a"},
		{ID: "b", Skill: "test/b", DependsOn: []string{"a"}},
		{ID: "c", Skill: "test/c", DependsOn: []string{"a"}},
		{ID: "d", Skill: "test/d", DependsOn: []string{"b", "c"}},
	}

	dag, err := NewDAG(steps)
	if err != nil {
		t.Fatalf("NewDAG failed: %v", err)
	}

	// Check topological order
	order := dag.Order()
	if len(order) != 4 {
		t.Errorf("expected 4 steps, got %d", len(order))
	}

	// 'a' must come first
	if order[0] != "a" {
		t.Errorf("expected 'a' first, got %s", order[0])
	}

	// 'd' must come last
	if order[3] != "d" {
		t.Errorf("expected 'd' last, got %s", order[3])
	}

	// 'b' and 'c' can be in any order, but must come before 'd'
	bIdx, cIdx, dIdx := sliceIndexOf(order, "b"), sliceIndexOf(order, "c"), sliceIndexOf(order, "d")
	if bIdx >= dIdx || cIdx >= dIdx {
		t.Errorf("b and c must come before d")
	}
}

func TestNewDAG_DetectsCycle(t *testing.T) {
	steps := []Step{
		{ID: "a", Skill: "test/a", DependsOn: []string{"c"}},
		{ID: "b", Skill: "test/b", DependsOn: []string{"a"}},
		{ID: "c", Skill: "test/c", DependsOn: []string{"b"}},
	}

	_, err := NewDAG(steps)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

func TestNewDAG_DuplicateID(t *testing.T) {
	steps := []Step{
		{ID: "a", Skill: "test/a"},
		{ID: "a", Skill: "test/b"},
	}

	_, err := NewDAG(steps)
	if err == nil {
		t.Fatal("expected duplicate ID error, got nil")
	}
}

func TestNewDAG_MissingDependency(t *testing.T) {
	steps := []Step{
		{ID: "a", Skill: "test/a", DependsOn: []string{"nonexistent"}},
	}

	_, err := NewDAG(steps)
	if err == nil {
		t.Fatal("expected missing dependency error, got nil")
	}
}

func TestDAG_Batches(t *testing.T) {
	steps := []Step{
		{ID: "a", Skill: "test/a"},
		{ID: "b", Skill: "test/b"},
		{ID: "c", Skill: "test/c", DependsOn: []string{"a", "b"}},
		{ID: "d", Skill: "test/d", DependsOn: []string{"c"}},
	}

	dag, err := NewDAG(steps)
	if err != nil {
		t.Fatalf("NewDAG failed: %v", err)
	}

	batches := dag.Batches()

	// Should have 3 batches:
	// 1. [a, b] - can run in parallel
	// 2. [c] - depends on a and b
	// 3. [d] - depends on c
	if len(batches) != 3 {
		t.Errorf("expected 3 batches, got %d", len(batches))
	}

	// First batch should have a and b
	if len(batches[0]) != 2 {
		t.Errorf("expected 2 steps in first batch, got %d", len(batches[0]))
	}
}

func TestDAG_Ready(t *testing.T) {
	steps := []Step{
		{ID: "a", Skill: "test/a"},
		{ID: "b", Skill: "test/b", DependsOn: []string{"a"}},
		{ID: "c", Skill: "test/c", DependsOn: []string{"a"}},
	}

	dag, err := NewDAG(steps)
	if err != nil {
		t.Fatalf("NewDAG failed: %v", err)
	}

	// Initially only 'a' is ready
	ready := dag.Ready(map[string]bool{})
	if len(ready) != 1 || ready[0] != "a" {
		t.Errorf("expected only 'a' ready, got %v", ready)
	}

	// After 'a' completes, 'b' and 'c' are ready
	ready = dag.Ready(map[string]bool{"a": true})
	if len(ready) != 2 {
		t.Errorf("expected 'b' and 'c' ready, got %v", ready)
	}
}

func TestDAG_InferDependencies(t *testing.T) {
	steps := []Step{
		{ID: "find", Skill: "fs/find", Input: map[string]any{"path": "."}},
		{ID: "analyze", Skill: "code/symbols", Input: map[string]any{
			"path": "{{.find.data.files}}",
		}},
	}

	dag, err := NewDAG(steps)
	if err != nil {
		t.Fatalf("NewDAG failed: %v", err)
	}

	// 'analyze' should depend on 'find' due to template reference
	deps := dag.Dependencies("analyze")
	if len(deps) != 1 || deps[0] != "find" {
		t.Errorf("expected 'analyze' to depend on 'find', got %v", deps)
	}
}

func TestParseTemplateRefs(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"{{.find.data.files}}", []string{"find"}},
		{"{{.step1.data}} and {{.step2.data}}", []string{"step1", "step2"}},
		{"{{.inputs.path}}", []string{}}, // inputs is a keyword
		{"no templates here", []string{}},
		{"{{.analyze.data.symbols}}", []string{"analyze"}},
	}

	for _, tc := range tests {
		refs := parseTemplateRefs(tc.input)
		if len(refs) != len(tc.expected) {
			t.Errorf("input %q: expected %v, got %v", tc.input, tc.expected, refs)
			continue
		}
		for i, ref := range refs {
			if ref != tc.expected[i] {
				t.Errorf("input %q: expected %v, got %v", tc.input, tc.expected, refs)
			}
		}
	}
}

func sliceIndexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}
