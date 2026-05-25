package workflow

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
	"testing/quick"
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

func TestDAGPropertyTopologicalOrderAndBatchesRespectDependencies(t *testing.T) {
	t.Parallel()

	property := func(raw []uint8) bool {
		steps := generatedAcyclicWorkflowSteps(raw)
		dag, err := NewDAG(steps)
		if err != nil {
			t.Logf("NewDAG(%+v) error = %v", steps, err)
			return false
		}

		order := dag.Order()
		if len(order) != len(steps) {
			t.Logf("order length = %d, want %d: %v", len(order), len(steps), order)
			return false
		}
		positions := positionsByID(order)
		batchByID := batchIndexByID(dag.Batches())

		for _, step := range steps {
			stepPos, ok := positions[step.ID]
			if !ok {
				t.Logf("step %q missing from order %v", step.ID, order)
				return false
			}
			if dag.Step(step.ID) == nil {
				t.Logf("Step(%q) = nil", step.ID)
				return false
			}
			for _, dep := range dag.Dependencies(step.ID) {
				depPos, ok := positions[dep]
				if !ok {
					t.Logf("dependency %q missing from order %v", dep, order)
					return false
				}
				if depPos >= stepPos {
					t.Logf("dependency order violated: dep %q at %d, step %q at %d, order %v", dep, depPos, step.ID, stepPos, order)
					return false
				}
				if batchByID[dep] >= batchByID[step.ID] {
					t.Logf("dependency batch violated: dep %q batch %d, step %q batch %d, batches %v", dep, batchByID[dep], step.ID, batchByID[step.ID], dag.Batches())
					return false
				}
			}
		}

		completed := map[string]bool{}
		for _, id := range order {
			ready := dag.Ready(completed)
			if !slices.Contains(ready, id) {
				t.Logf("step %q not ready after completed=%v; ready=%v order=%v", id, completed, ready, order)
				return false
			}
			completed[id] = true
		}
		return len(dag.Ready(completed)) == 0
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestDAGReadyReturnsSortedSteps(t *testing.T) {
	t.Parallel()

	steps := []Step{
		{ID: "z", Skill: "test/z"},
		{ID: "a", Skill: "test/a"},
		{ID: "m", Skill: "test/m"},
	}
	dag, err := NewDAG(steps)
	if err != nil {
		t.Fatalf("NewDAG failed: %v", err)
	}

	got := dag.Ready(nil)
	want := []string{"a", "m", "z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Ready(nil) = %v, want %v", got, want)
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

func generatedAcyclicWorkflowSteps(raw []uint8) []Step {
	count := len(raw)%8 + 1
	steps := make([]Step, count)
	for i := range steps {
		steps[i] = Step{
			ID:    fmt.Sprintf("step%d", i),
			Skill: "test/noop",
		}
	}
	for i := 1; i < count; i++ {
		bits := raw[(i-1)%len(raw)]
		for dep := 0; dep < i; dep++ {
			if bits&(1<<uint(dep%8)) != 0 {
				steps[i].DependsOn = append(steps[i].DependsOn, steps[dep].ID)
			}
		}
	}
	return steps
}

func positionsByID(ids []string) map[string]int {
	positions := make(map[string]int, len(ids))
	for i, id := range ids {
		positions[id] = i
	}
	return positions
}

func batchIndexByID(batches [][]string) map[string]int {
	index := make(map[string]int)
	for i, batch := range batches {
		for _, id := range batch {
			index[id] = i
		}
	}
	return index
}

func sliceIndexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}
