package generalsolver

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// SplitThresholdBytes is the serialized size above which a work item is
	// considered for splitting. Aligned with helperPreflightMaxInputChars.
	SplitThresholdBytes = 50000

	// SplitMaxSubItems caps the number of solve sub-items to prevent
	// combinatorial explosion on pathological inputs.
	SplitMaxSubItems = 16

	// Count-based structural thresholds: split regardless of serialized size
	// when arrays exceed these counts. These target the common case where the
	// helper receives hundreds of items that are individually small but
	// collectively overwhelm a single helper attempt.
	SplitMinBindings    = 8  // split when len(bindings) > 8
	SplitMinQueries     = 1  // split when len(queries) > 1
	SplitMinEvents      = 16 // split when len(events) > 16
	SplitMinConstraints = 8  // split when len(constraints) > 8

	// SplitMinSubItemBytes is the minimum payload size for a sub-item.
	// If a chunk would be smaller than this, it gets merged into the
	// previous chunk.
	SplitMinSubItemBytes = 2000
)

// SplitStrategy determines how a work item is decomposed.
type SplitStrategy string

const (
	// SplitStrategyQueryDecomposition splits a multi-query work item into
	// independent solve sub-items, one per query.
	SplitStrategyQueryDecomposition SplitStrategy = "query_decomposition"

	// SplitStrategyChunkDecomposition splits a large payload into fixed-size
	// chunks when no query boundaries are detectable.
	SplitStrategyChunkDecomposition SplitStrategy = "chunk_decomposition"

	// SplitStrategyNone means no splitting is needed.
	SplitStrategyNone SplitStrategy = "none"
)

// SplitPlan describes the result of analyzing a work item for splitting.
type SplitPlan struct {
	Strategy   SplitStrategy `json:"strategy"`
	ParentID   string        `json:"parent_id"`
	ParseID    string        `json:"parse_id,omitempty"`
	MergeID    string        `json:"merge_id,omitempty"`
	SolveIDs   []string      `json:"solve_ids,omitempty"`
	ChunkCount int           `json:"chunk_count,omitempty"`
	Reason     string        `json:"reason,omitempty"`
}

// AnalyzeForSplit determines whether a work item needs splitting based on
// its payload structure and size. Structural thresholds (count-based) are
// checked first — a 364-binding work item should split regardless of whether
// its serialized form happens to be under 50KB. Size thresholds apply as a
// fallback when no structural pattern is detected.
func AnalyzeForSplit(item WorkItem) SplitPlan {
	plan := SplitPlan{
		Strategy: SplitStrategyNone,
		ParentID: item.ID,
	}

	// Phase 1: structural (count-based) thresholds — these fire regardless
	// of serialized size. This is the primary split path.
	if structuralPlan := analyzeStructuralSplit(item); structuralPlan.Strategy != SplitStrategyNone {
		return structuralPlan
	}

	// Phase 2: size-based threshold — only if no structural pattern matched.
	payloadSize := estimatePayloadSize(item.Payload)
	if payloadSize < SplitThresholdBytes {
		return plan
	}

	// Fallback: fixed-size chunking.
	chunkCount := (payloadSize / SplitThresholdBytes) + 1
	if chunkCount > SplitMaxSubItems {
		chunkCount = SplitMaxSubItems
	}
	plan.Strategy = SplitStrategyChunkDecomposition
	plan.ChunkCount = chunkCount
	plan.Reason = fmt.Sprintf("payload %d bytes exceeds threshold, chunking into %d parts", payloadSize, chunkCount)
	return plan
}

// analyzeStructuralSplit checks count-based thresholds on known array fields.
func analyzeStructuralSplit(item WorkItem) SplitPlan {
	plan := SplitPlan{
		Strategy: SplitStrategyNone,
		ParentID: item.ID,
	}
	if item.Payload == nil {
		return plan
	}

	// Check "queries" — split when > 1.
	if arr, ok := extractArrayAny(item.Payload, "queries"); ok && len(arr) > SplitMinQueries {
		plan.Strategy = SplitStrategyQueryDecomposition
		plan.ChunkCount = min(len(arr), SplitMaxSubItems)
		plan.Reason = fmt.Sprintf("queries: %d items (threshold: %d)", len(arr), SplitMinQueries)
		return plan
	}
	// Check "sub_problems" / "subproblems" — split when > 1.
	if arr, ok := extractArrayAny(item.Payload, "sub_problems", "subproblems"); ok && len(arr) > SplitMinQueries {
		plan.Strategy = SplitStrategyQueryDecomposition
		plan.ChunkCount = min(len(arr), SplitMaxSubItems)
		plan.Reason = fmt.Sprintf("sub_problems: %d items (threshold: %d)", len(arr), SplitMinQueries)
		return plan
	}
	// Check "bindings" — split when above count threshold.
	if arr, ok := extractArrayAny(item.Payload, "bindings"); ok && len(arr) > SplitMinBindings {
		plan.Strategy = SplitStrategyQueryDecomposition
		plan.ChunkCount = min(len(arr), SplitMaxSubItems)
		plan.Reason = fmt.Sprintf("bindings: %d items (threshold: %d)", len(arr), SplitMinBindings)
		return plan
	}
	// Check "events" — split when above count threshold.
	if arr, ok := extractArrayAny(item.Payload, "events"); ok && len(arr) > SplitMinEvents {
		plan.Strategy = SplitStrategyQueryDecomposition
		plan.ChunkCount = min(len(arr), SplitMaxSubItems)
		plan.Reason = fmt.Sprintf("events: %d items (threshold: %d)", len(arr), SplitMinEvents)
		return plan
	}
	// Check "constraints" — split when above count threshold.
	if arr, ok := extractArrayAny(item.Payload, "constraints"); ok && len(arr) > SplitMinConstraints {
		plan.Strategy = SplitStrategyQueryDecomposition
		plan.ChunkCount = min(len(arr), SplitMaxSubItems)
		plan.Reason = fmt.Sprintf("constraints: %d items (threshold: %d)", len(arr), SplitMinConstraints)
		return plan
	}

	return plan
}

// ApplySplit decomposes a work item into sub-items based on a SplitPlan.
// The parent item is marked as blocked and replaced with:
//   - parse: extracts/normalizes the raw input (depends on parent's deps)
//   - solve₁...solveₙ: independent sub-problems (depend on parse)
//   - merge: combines sub-results (depends on all solve items)
//
// The parent item's original dependents are rewired to depend on merge.
// Returns the new sub-item IDs (parse, solves, merge).
func ApplySplit(state *SolverState, parentID string, plan SplitPlan) ([]string, error) {
	if state == nil {
		return nil, fmt.Errorf("generalsolver: state is nil")
	}
	parent, exists := state.Items[parentID]
	if !exists {
		return nil, fmt.Errorf("generalsolver: parent item %q not found", parentID)
	}
	if plan.Strategy == SplitStrategyNone {
		return nil, nil
	}

	// Generate sub-item IDs.
	parseID := parentID + "__parse"
	mergeID := parentID + "__merge"
	solveIDs := make([]string, plan.ChunkCount)
	for i := range solveIDs {
		solveIDs[i] = fmt.Sprintf("%s__solve_%02d", parentID, i)
	}

	// Create parse sub-item.
	if err := AddWorkItem(state, WorkItem{
		ID:        parseID,
		Goal:      "Parse and normalize raw input for sub-problem decomposition",
		Archetype: ArchetypeExplicitDAG,
		DependsOn: parent.DependsOn,
		Status:    StatusPending,
		Priority:  parent.Priority + 0.5,
		Risk:      0.1,
		Payload: map[string]any{
			"split_role":     "parse",
			"parent_id":      parentID,
			"strategy":       string(plan.Strategy),
			"chunk_count":    plan.ChunkCount,
			"parent_payload": parent.Payload,
		},
	}); err != nil {
		return nil, fmt.Errorf("generalsolver: split add parse: %w", err)
	}
	// Mark as ready if no deps.
	if len(parent.DependsOn) == 0 {
		item := state.Items[parseID]
		item.Status = StatusReady
		state.Items[parseID] = item
	}

	// Create solve sub-items.
	queries := ExtractQueryableChunks(parent.Payload)
	for i, solveID := range solveIDs {
		goal := fmt.Sprintf("Solve sub-problem %d/%d", i+1, plan.ChunkCount)
		payload := map[string]any{
			"split_role":  "solve",
			"parent_id":   parentID,
			"chunk_index": i,
			"parse_id":    parseID,
		}
		// Attach chunk-specific payload if we have query decomposition.
		if plan.Strategy == SplitStrategyQueryDecomposition && i < len(queries) {
			payload["chunk"] = queries[i]
		} else if plan.Strategy == SplitStrategyChunkDecomposition {
			payload["chunk_index"] = i
			payload["total_chunks"] = plan.ChunkCount
		}

		if err := AddWorkItem(state, WorkItem{
			ID:        solveID,
			Goal:      goal,
			Archetype: parent.Archetype,
			DependsOn: []string{parseID},
			Status:    StatusPending,
			Priority:  parent.Priority,
			Risk:      parent.Risk,
			Payload:   payload,
		}); err != nil {
			return nil, fmt.Errorf("generalsolver: split add solve %d: %w", i, err)
		}
	}

	// Create merge sub-item.
	if err := AddWorkItem(state, WorkItem{
		ID:        mergeID,
		Goal:      "Merge sub-problem results into final answer",
		Archetype: ArchetypeExplicitDAG,
		DependsOn: solveIDs,
		Status:    StatusPending,
		Priority:  parent.Priority - 0.1,
		Risk:      parent.Risk,
		Payload: map[string]any{
			"split_role":    "merge",
			"parent_id":     parentID,
			"parse_id":      parseID,
			"solve_ids":     solveIDs,
			"parent_goal":   parent.Goal,
			"parent_schema": parent.Payload["input_schema"],
		},
	}); err != nil {
		return nil, fmt.Errorf("generalsolver: split add merge: %w", err)
	}

	// Rewire dependents of parent to depend on merge instead.
	rewireDependents(state, parentID, mergeID)

	// Remove parent item.
	delete(state.Items, parentID)
	// Clean up reverse deps for parent.
	delete(state.ReverseDeps, parentID)

	allIDs := make([]string, 0, 2+len(solveIDs))
	allIDs = append(allIDs, parseID)
	allIDs = append(allIDs, solveIDs...)
	allIDs = append(allIDs, mergeID)
	return allIDs, nil
}

// rewireDependents updates all items that depend on oldParentID to depend on
// newParentID instead.
func rewireDependents(state *SolverState, oldParentID, newParentID string) {
	for id, item := range state.Items {
		changed := false
		for i, depID := range item.DependsOn {
			if depID == oldParentID {
				item.DependsOn[i] = newParentID
				changed = true
			}
		}
		if changed {
			state.Items[id] = item
			// Update reverse deps.
			delete(state.ReverseDeps, oldParentID)
			state.ReverseDeps[newParentID] = append(state.ReverseDeps[newParentID], id)
		}
	}
}

// ExtractQueryableChunks attempts to decompose a payload into independent
// query-able sub-problems. It looks for common patterns:
//   - "queries" array
//   - "sub_problems" / "subproblems" array
//   - "bindings" / "events" / "constraints" arrays (chunked)
func ExtractQueryableChunks(payload map[string]any) []map[string]any {
	if payload == nil {
		return nil
	}

	// Direct "queries" key.
	if queries, ok := extractArrayMaps(payload, "queries"); ok && len(queries) > 1 {
		return queries
	}
	// "sub_problems" or "subproblems".
	if queries, ok := extractArrayMaps(payload, "sub_problems", "subproblems"); ok && len(queries) > 1 {
		return queries
	}
	// "bindings" — chunk into groups.
	if bindings, ok := extractArrayAny(payload, "bindings"); ok && len(bindings) > 1 {
		return chunkArrayAny(bindings, "binding")
	}
	// "events" — chunk into groups.
	if events, ok := extractArrayAny(payload, "events"); ok && len(events) > 1 {
		return chunkArrayAny(events, "event")
	}
	// "constraints" — chunk into groups.
	if constraints, ok := extractArrayAny(payload, "constraints"); ok && len(constraints) > 1 {
		return chunkArrayAny(constraints, "constraint")
	}

	return nil
}

// extractArrayMaps extracts an array of maps from one of the given keys.
// Handles both []any and []map[string]any since Go's type system doesn't
// unify these.
func extractArrayMaps(payload map[string]any, keys ...string) ([]map[string]any, bool) {
	for _, key := range keys {
		val, ok := payload[key]
		if !ok {
			continue
		}
		// Try []map[string]any first (literal Go construction).
		if maps, ok := val.([]map[string]any); ok && len(maps) > 0 {
			return maps, true
		}
		// Try []any (JSON unmarshal path).
		arr, ok := val.([]any)
		if !ok {
			continue
		}
		maps := make([]map[string]any, 0, len(arr))
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				maps = append(maps, m)
			}
		}
		if len(maps) > 0 {
			return maps, true
		}
	}
	return nil, false
}

// extractArrayAny extracts a raw []any from one of the given keys.
// Handles both []any and []map[string]any.
func extractArrayAny(payload map[string]any, keys ...string) ([]any, bool) {
	for _, key := range keys {
		val, ok := payload[key]
		if !ok {
			continue
		}
		if arr, ok := val.([]any); ok {
			return arr, true
		}
		// Handle []map[string]any by wrapping into []any.
		if maps, ok := val.([]map[string]any); ok {
			out := make([]any, len(maps))
			for i, m := range maps {
				out[i] = m
			}
			return out, true
		}
	}
	return nil, false
}

// chunkArrayAny groups a flat array into chunks suitable for independent
// sub-items. Each chunk becomes a map with a "chunk_items" key.
func chunkArrayAny(items []any, label string) []map[string]any {
	// Estimate average item size from a sample to determine chunk count.
	avgItemBytes := estimateAverageItemSize(items)
	if avgItemBytes < 1 {
		avgItemBytes = 100
	}
	chunkSize := SplitThresholdBytes / avgItemBytes
	if chunkSize < 1 {
		chunkSize = 1
	}
	// Determine how many chunks we need.
	totalItems := len(items)
	neededChunks := (totalItems + chunkSize - 1) / chunkSize
	if neededChunks > SplitMaxSubItems {
		// Redistribute to fill SplitMaxSubItems chunks evenly.
		chunkSize = (totalItems + SplitMaxSubItems - 1) / SplitMaxSubItems
		neededChunks = SplitMaxSubItems
	}

	var chunks []map[string]any
	for i := 0; i < totalItems; i += chunkSize {
		end := i + chunkSize
		if end > totalItems {
			end = totalItems
		}
		chunks = append(chunks, map[string]any{
			"chunk_items": items[i:end],
			"chunk_label": label,
			"chunk_range": fmt.Sprintf("%d-%d", i, end-1),
		})
	}
	return chunks
}

// estimateAverageItemSize samples a few items to estimate average serialized
// byte size per item.
func estimateAverageItemSize(items []any) int {
	sampleCount := 3
	if len(items) < sampleCount {
		sampleCount = len(items)
	}
	if sampleCount == 0 {
		return 100
	}
	var totalSize int
	for i := 0; i < sampleCount; i++ {
		// Pick evenly-spaced samples.
		idx := (len(items) / (sampleCount + 1)) * (i + 1)
		if idx >= len(items) {
			idx = len(items) - 1
		}
		data, err := json.Marshal(items[idx])
		if err != nil {
			totalSize += 100
		} else {
			totalSize += len(data)
		}
	}
	return totalSize / sampleCount
}

// estimatePayloadSize returns the estimated serialized byte size of a payload.
func estimatePayloadSize(payload map[string]any) int {
	if payload == nil {
		return 0
	}
	data, err := json.Marshal(payload)
	if err != nil {
		// Fallback: rough string estimate.
		return len(fmt.Sprintf("%v", payload))
	}
	return len(data)
}

// SplitWorkItem is the high-level entry point. It analyzes the work item,
// and if splitting is warranted, applies the split and returns the sub-item IDs.
// Returns (nil, nil) if no split is needed.
func SplitWorkItem(state *SolverState, itemID string) ([]string, error) {
	if state == nil {
		return nil, fmt.Errorf("generalsolver: state is nil")
	}
	item, exists := state.Items[itemID]
	if !exists {
		return nil, fmt.Errorf("generalsolver: item %q not found", itemID)
	}
	plan := AnalyzeForSplit(item)
	if plan.Strategy == SplitStrategyNone {
		return nil, nil
	}
	return ApplySplit(state, itemID, plan)
}

// IsSplitSubItem returns true if a work item is a sub-item created by the
// splitter (has a "split_role" in its payload).
func IsSplitSubItem(item WorkItem) bool {
	role, ok := item.Payload["split_role"].(string)
	if !ok {
		return false
	}
	return role == "parse" || role == "solve" || role == "merge"
}

// SplitRole returns the split role of a work item ("parse", "solve", "merge")
// or empty string if it is not a split sub-item.
func SplitRole(item WorkItem) string {
	role, _ := item.Payload["split_role"].(string)
	return role
}

// ParentIDFromSplit returns the original parent item ID for a split sub-item,
// or empty string if not a sub-item.
func ParentIDFromSplit(item WorkItem) string {
	pid, _ := item.Payload["parent_id"].(string)
	return pid
}

// BuildSplitSummary constructs a combined summary from solve sub-item
// artifacts for the merge step.
func BuildSplitSummary(artifacts map[string]WorkArtifact, solveIDs []string) string {
	var parts []string
	for _, id := range solveIDs {
		art, ok := artifacts[id]
		if !ok {
			parts = append(parts, fmt.Sprintf("%s: [no artifact]", id))
			continue
		}
		answer := "n/a"
		if art.Answer != nil {
			if s, ok := art.Answer.(string); ok {
				answer = truncateString(s, 200)
			} else {
				answer = truncateString(fmt.Sprintf("%v", art.Answer), 200)
			}
		}
		parts = append(parts, fmt.Sprintf("%s: %s (confidence=%.2f)", id, answer, art.Confidence))
	}
	return strings.Join(parts, "\n")
}

// RenderSplitMergePayload builds the merge payload from collected artifacts.
func RenderSplitMergePayload(artifacts map[string]WorkArtifact, solveIDs []string, parentGoal string) map[string]any {
	subResults := make([]map[string]any, 0, len(solveIDs))
	for _, id := range solveIDs {
		art, ok := artifacts[id]
		if !ok {
			subResults = append(subResults, map[string]any{
				"id":     id,
				"status": "missing",
			})
			continue
		}
		entry := map[string]any{
			"id":         id,
			"status":     art.Status,
			"confidence": art.Confidence,
		}
		if art.Answer != nil {
			entry["answer"] = art.Answer
		}
		if art.Code != "" {
			entry["code"] = truncateString(art.Code, 500)
		}
		subResults = append(subResults, entry)
	}
	return map[string]any{
		"split_role":  "merge",
		"parent_goal": parentGoal,
		"sub_results": subResults,
		"solve_count": len(solveIDs),
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
