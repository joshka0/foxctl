package workflow

import (
	"fmt"
	"slices"
	"sort"
)

// DAG represents a directed acyclic graph of workflow steps.
type DAG struct {
	nodes   map[string]*Step
	edges   map[string][]string // step -> dependencies
	reverse map[string][]string // step -> dependents
	order   []string            // topological order
	batches [][]string          // parallel execution batches
}

// NewDAG builds a DAG from workflow steps.
func NewDAG(steps []Step) (*DAG, error) {
	d := &DAG{
		nodes:   make(map[string]*Step),
		edges:   make(map[string][]string),
		reverse: make(map[string][]string),
	}

	// Index all steps
	for i := range steps {
		step := &steps[i]
		if step.ID == "" {
			return nil, fmt.Errorf("step at index %d has no ID", i)
		}
		if _, exists := d.nodes[step.ID]; exists {
			return nil, fmt.Errorf("duplicate step ID: %s", step.ID)
		}
		d.nodes[step.ID] = step
		d.edges[step.ID] = nil
		d.reverse[step.ID] = nil
	}

	// Build edges from explicit dependencies
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			if _, exists := d.nodes[dep]; !exists {
				return nil, fmt.Errorf("step %s depends on unknown step: %s", step.ID, dep)
			}
			d.edges[step.ID] = append(d.edges[step.ID], dep)
			d.reverse[dep] = append(d.reverse[dep], step.ID)
		}
	}

	// Infer implicit dependencies from template references
	if err := d.inferDependencies(steps); err != nil {
		return nil, err
	}

	// Compute topological order
	order, err := d.topologicalSort()
	if err != nil {
		return nil, err
	}
	d.order = order

	// Compute parallel batches
	d.batches = d.computeBatches()

	return d, nil
}

// inferDependencies adds implicit edges based on template references.
func (d *DAG) inferDependencies(steps []Step) error {
	for _, step := range steps {
		refs := extractStepReferences(step)
		for _, ref := range refs {
			// Check if this is a step ID reference
			if _, exists := d.nodes[ref]; exists {
				// Add implicit dependency if not already explicit
				if !slices.Contains(d.edges[step.ID], ref) && ref != step.ID {
					d.edges[step.ID] = append(d.edges[step.ID], ref)
					d.reverse[ref] = append(d.reverse[ref], step.ID)
				}
			}
		}
	}
	return nil
}

// extractStepReferences finds step ID references in a step's templates.
func extractStepReferences(step Step) []string {
	var refs []string
	seen := make(map[string]bool)

	// Check input templates
	for _, v := range step.Input {
		for _, ref := range findTemplateRefs(v) {
			if !seen[ref] {
				refs = append(refs, ref)
				seen[ref] = true
			}
		}
	}

	// Check condition
	for _, ref := range findTemplateRefs(step.If) {
		if !seen[ref] {
			refs = append(refs, ref)
			seen[ref] = true
		}
	}

	// Check loop
	if step.Loop != nil {
		for _, ref := range findTemplateRefs(step.Loop.Over) {
			if !seen[ref] {
				refs = append(refs, ref)
				seen[ref] = true
			}
		}
	}

	return refs
}

// findTemplateRefs extracts step references from template expressions.
// It looks for patterns like {{.stepId.data...}} or {{.stepId...}}
func findTemplateRefs(v any) []string {
	switch val := v.(type) {
	case string:
		return parseTemplateRefs(val)
	case map[string]any:
		var refs []string
		for _, v := range val {
			refs = append(refs, findTemplateRefs(v)...)
		}
		return refs
	case []any:
		var refs []string
		for _, v := range val {
			refs = append(refs, findTemplateRefs(v)...)
		}
		return refs
	default:
		return nil
	}
}

// parseTemplateRefs extracts step ID references from a template string.
func parseTemplateRefs(s string) []string {
	var refs []string
	seen := make(map[string]bool)

	// Simple pattern matching for {{.stepId...}}
	// This is a simplified approach; a full template parser would be more robust
	i := 0
	for i < len(s) {
		// Find start of template
		start := indexOf(s[i:], "{{.")
		if start == -1 {
			break
		}
		start += i + 3 // Move past "{{."

		// Skip if it's a special variable
		if len(s) > start && (s[start] == 'i' || s[start] == 'I') {
			// Could be "inputs" - skip
			if hasPrefix(s[start:], "inputs") {
				i = start
				continue
			}
		}

		// Extract the identifier (first segment before . or }})
		end := start
		for end < len(s) && isIdentChar(s[end]) {
			end++
		}

		if end > start {
			ref := s[start:end]
			// Skip known keywords
			if ref != "inputs" && ref != "item" && ref != "index" && ref != "loop" {
				if !seen[ref] {
					refs = append(refs, ref)
					seen[ref] = true
				}
			}
		}

		i = end
	}

	return refs
}

// topologicalSort returns nodes in dependency order using Kahn's algorithm.
func (d *DAG) topologicalSort() ([]string, error) {
	// Count incoming edges
	inDegree := make(map[string]int)
	for id := range d.nodes {
		inDegree[id] = len(d.edges[id])
	}

	// Find nodes with no dependencies
	var queue []string
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Sort for deterministic order
	sort.Strings(queue)

	var order []string
	for len(queue) > 0 {
		// Pop first node
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)

		// Process dependents
		dependents := d.reverse[node]
		sort.Strings(dependents) // Deterministic order

		for _, dep := range dependents {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	// Check for cycles
	if len(order) != len(d.nodes) {
		// Find nodes involved in cycle
		var cycleNodes []string
		for id, degree := range inDegree {
			if degree > 0 {
				cycleNodes = append(cycleNodes, id)
			}
		}
		return nil, fmt.Errorf("workflow contains cycle involving steps: %v", cycleNodes)
	}

	return order, nil
}

// computeBatches groups steps that can run in parallel.
func (d *DAG) computeBatches() [][]string {
	if len(d.order) == 0 {
		return [][]string{}
	}

	// Track which batch each step is in
	levels := make(map[string]int)

	for _, id := range d.order {
		maxDepLevel := -1
		for _, dep := range d.edges[id] {
			if level, ok := levels[dep]; ok && level > maxDepLevel {
				maxDepLevel = level
			}
		}
		levels[id] = maxDepLevel + 1
	}

	// Group by level
	maxLevel := 0
	for _, level := range levels {
		if level > maxLevel {
			maxLevel = level
		}
	}

	batches := make([][]string, maxLevel+1)
	for id, level := range levels {
		batches[level] = append(batches[level], id)
	}

	// Sort within batches for deterministic order
	for i := range batches {
		sort.Strings(batches[i])
	}

	return batches
}

// Order returns the topological order of steps.
func (d *DAG) Order() []string {
	return d.order
}

// Batches returns groups of steps that can execute in parallel.
func (d *DAG) Batches() [][]string {
	return d.batches
}

// Step returns a step by ID.
func (d *DAG) Step(id string) *Step {
	return d.nodes[id]
}

// Dependencies returns the direct dependencies of a step.
func (d *DAG) Dependencies(id string) []string {
	return d.edges[id]
}

// Dependents returns steps that depend on the given step.
func (d *DAG) Dependents(id string) []string {
	return d.reverse[id]
}

// AllSteps returns all steps in the DAG.
func (d *DAG) AllSteps() []*Step {
	steps := make([]*Step, 0, len(d.nodes))
	for _, id := range d.order {
		steps = append(steps, d.nodes[id])
	}
	return steps
}

// Ready returns steps that are ready to execute given completed steps.
func (d *DAG) Ready(completed map[string]bool) []string {
	var ready []string
	for id := range d.nodes {
		if completed[id] {
			continue
		}

		// Check if all dependencies are completed
		allDepsComplete := true
		for _, dep := range d.edges[id] {
			if !completed[dep] {
				allDepsComplete = false
				break
			}
		}

		if allDepsComplete {
			ready = append(ready, id)
		}
	}

	sort.Strings(ready)
	return ready
}

// Helper functions

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_'
}
