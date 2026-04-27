package contextengine

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// ImpactGraph — read-only interface used by ComputeImpact
// ---------------------------------------------------------------------------

// ImpactGraph provides read-only access to the impact graph for traversal.
// Implementations wrap the Store interface but expose only the read operations
// needed for impact computation.
type ImpactGraph interface {
	// ForwardEdges returns edges originating from ref.
	ForwardEdges(ref EvidenceRef) []ImpactEdge
	// ReverseEdges returns edges pointing to ref.
	ReverseEdges(ref EvidenceRef) []ImpactEdge
	// ClaimsForRef returns claims whose SourceRefs contain the given ref.
	ClaimsForRef(ref EvidenceRef) []MemoryClaim
	// AllClaims returns all claims in the workspace.
	AllClaims() []MemoryClaim
}

// ComputeImpactOptions configures impact computation.
type ComputeImpactOptions struct {
	// MaxDepth limits graph traversal depth. Default is 10.
	MaxDepth int
	// Clock provides the current time. Default is time.Now.
	Clock func() time.Time
}

// ComputeImpactOption is a functional option for ComputeImpact.
type ComputeImpactOption func(*ComputeImpactOptions)

// WithMaxDepth sets the maximum traversal depth.
func WithMaxDepth(d int) ComputeImpactOption {
	return func(o *ComputeImpactOptions) { o.MaxDepth = d }
}

// ComputeImpact computes the impact edges and staleness markers resulting from
// an event, given the current impact graph state. It is a pure function:
// same inputs always produce the same outputs with no side effects.
//
// VAL-IMPL-001: Pure function with bounded traversal depth.
// VAL-IMPL-002: Traversal limited to max depth.
// VAL-IMPL-003: Empty graph returns empty slices.
func ComputeImpact(event ContextEvent, graph ImpactGraph, opts ...ComputeImpactOption) ([]ImpactEdge, []StalenessMarker, error) {
	options := &ComputeImpactOptions{
		MaxDepth: 10,
		Clock:    time.Now,
	}
	for _, o := range opts {
		o(options)
	}

	var edges []ImpactEdge
	var markers []StalenessMarker
	now := options.Clock().UTC().Truncate(time.Millisecond)

	switch event.Kind {
	case EventKindCodeChangedDirty:
		edges, markers = computeDirtyEditImpact(event, graph, now, options)

	case EventKindCodeCommitted:
		edges, markers = computeCommitImpact(event, graph, now, options)

	case EventKindCodeValidated:
		edges, markers = computeValidatedImpact(event, graph, now, options)

	case EventKindAnswerCorrected:
		edges, markers = computeCorrectionImpact(event, graph, now, options)

	case EventKindMemoryClaimPromoted:
		edges, markers = computePromotionImpact(event, graph, now, options)

	case EventKindMemoryClaimInvalidated:
		edges, markers = computeInvalidationImpact(event, graph, now, options)

	default:
		// Most events don't produce impact
		return nil, nil, nil
	}

	return edges, markers, nil
}

// computeDirtyEditImpact handles code.changed_dirty events.
// Creates dirty staleness markers and invalidates edges for affected claims.
func computeDirtyEditImpact(event ContextEvent, graph ImpactGraph, now time.Time, options *ComputeImpactOptions) ([]ImpactEdge, []StalenessMarker) {
	var edges []ImpactEdge
	var markers []StalenessMarker
	visited := make(map[string]bool)

	for _, ref := range event.Refs {
		// Create dirty staleness marker for the ref itself
		markerID := fmt.Sprintf("staleness-%s-%s", event.ID, FormatEvidenceRef(ref))
		markers = append(markers, StalenessMarker{
			ID:             markerID,
			WorkspaceID:    event.WorkspaceID,
			TargetRef:      ref,
			Status:         StalenessStatusDirty,
			CausedByEvents: []string{event.ID},
			CreatedAt:      now,
			UpdatedAt:      now,
		})

		// Traverse forward edges to find affected claims
		traverseImpact(event, ref, graph, now, options, 0, &edges, &markers, visited)
	}

	return edges, markers
}

// computeCommitImpact handles code.committed events.
// Creates validates edges for committed refs. Does NOT directly resolve markers
// (that's done by ApplyInvalidation which has store access).
func computeCommitImpact(event ContextEvent, graph ImpactGraph, now time.Time, options *ComputeImpactOptions) ([]ImpactEdge, []StalenessMarker) {
	var edges []ImpactEdge

	for _, ref := range event.Refs {
		edgeID := fmt.Sprintf("edge-%s-validates-%s", event.ID, FormatEvidenceRef(ref))
		edges = append(edges, ImpactEdge{
			ID:            edgeID,
			WorkspaceID:   event.WorkspaceID,
			From:          EvidenceRef{Type: RefTypeEvent, Ref: event.ID},
			To:            ref,
			Kind:          ImpactEdgeKindValidates,
			SourceEventID: event.ID,
			CreatedAt:     now,
		})
	}

	return edges, nil
}

// computeValidatedImpact handles code.validated events.
// Creates validates edges similar to commit.
func computeValidatedImpact(event ContextEvent, graph ImpactGraph, now time.Time, options *ComputeImpactOptions) ([]ImpactEdge, []StalenessMarker) {
	var edges []ImpactEdge

	for _, ref := range event.Refs {
		edgeID := fmt.Sprintf("edge-%s-validates-%s", event.ID, FormatEvidenceRef(ref))
		edges = append(edges, ImpactEdge{
			ID:            edgeID,
			WorkspaceID:   event.WorkspaceID,
			From:          EvidenceRef{Type: RefTypeEvent, Ref: event.ID},
			To:            ref,
			Kind:          ImpactEdgeKindValidates,
			SourceEventID: event.ID,
			CreatedAt:     now,
		})
	}

	return edges, nil
}

// computeCorrectionImpact handles answer.corrected events.
// Creates needs_revalidation markers for implicated claims.
func computeCorrectionImpact(event ContextEvent, graph ImpactGraph, now time.Time, options *ComputeImpactOptions) ([]ImpactEdge, []StalenessMarker) {
	var edges []ImpactEdge
	var markers []StalenessMarker

	for _, ref := range event.Refs {
		// Find claims generated from this ref
		claims := graph.ClaimsForRef(ref)
		for _, claim := range claims {
			if claim.Status == ClaimStatusCurrent || claim.Status == ClaimStatusCandidate {
				// Create invalidates edge
				edgeID := fmt.Sprintf("edge-%s-invalidates-%s", event.ID, claim.ID)
				edges = append(edges, ImpactEdge{
					ID:            edgeID,
					WorkspaceID:   event.WorkspaceID,
					From:          EvidenceRef{Type: RefTypeEvent, Ref: event.ID},
					To:            EvidenceRef{Type: RefTypeMemoryClaim, Ref: claim.ID},
					Kind:          ImpactEdgeKindInvalidates,
					SourceEventID: event.ID,
					CreatedAt:     now,
				})

				// Create needs_revalidation marker
				markerID := fmt.Sprintf("staleness-%s-claim-%s", event.ID, claim.ID)
				markers = append(markers, StalenessMarker{
					ID:             markerID,
					WorkspaceID:    event.WorkspaceID,
					TargetRef:      EvidenceRef{Type: RefTypeMemoryClaim, Ref: claim.ID},
					Status:         StalenessStatusNeedsRevalidation,
					CausedByEvents: []string{event.ID},
					CreatedAt:      now,
					UpdatedAt:      now,
				})
			}
		}
	}

	return edges, markers
}

// computePromotionImpact handles memory.claim_promoted events.
// Creates supersedes edges and superseded markers for old claims.
func computePromotionImpact(event ContextEvent, graph ImpactGraph, now time.Time, options *ComputeImpactOptions) ([]ImpactEdge, []StalenessMarker) {
	var edges []ImpactEdge
	var markers []StalenessMarker

	// The promoted claim is typically the first ref in the event.
	// Look for claims that the promoted claim supersedes.
	// A claim supersedes old claims if its SourceRefs point at existing claims.
	for _, ref := range event.Refs {
		if ref.Type != RefTypeMemoryClaim {
			continue
		}

		// Find all claims that have this claim's ref in their SourceRefs
		// OR find claims that the event data says it supersedes
		allClaims := graph.AllClaims()
		for _, claim := range allClaims {
			if claim.ID == ref.Ref {
				// This is the promoted claim itself — check its SourceRefs for old claims
				for _, srcRef := range claim.SourceRefs {
					if srcRef.Type == RefTypeMemoryClaim {
						// Find the old claim
						oldClaims := graph.AllClaims()
						for _, old := range oldClaims {
							if old.ID == srcRef.Ref && old.Status == ClaimStatusCurrent && old.ID != claim.ID {
								edgeID := fmt.Sprintf("edge-%s-supersedes-%s", event.ID, old.ID)
								edges = append(edges, ImpactEdge{
									ID:            edgeID,
									WorkspaceID:   event.WorkspaceID,
									From:          ref,
									To:            EvidenceRef{Type: RefTypeMemoryClaim, Ref: old.ID},
									Kind:          ImpactEdgeKindSupersedes,
									SourceEventID: event.ID,
									CreatedAt:     now,
								})

								markerID := fmt.Sprintf("staleness-%s-superseded-%s", event.ID, old.ID)
								markers = append(markers, StalenessMarker{
									ID:              markerID,
									WorkspaceID:     event.WorkspaceID,
									TargetRef:       EvidenceRef{Type: RefTypeMemoryClaim, Ref: old.ID},
									Status:          StalenessStatusSuperseded,
									CausedByEvents:  []string{event.ID},
									ResolvedByEvent: event.ID,
									CreatedAt:       now,
									UpdatedAt:       now,
								})
							}
						}
					}
				}
			}
		}
	}

	return edges, markers
}

// computeInvalidationImpact handles memory.claim_invalidated events.
// Creates invalidates edges for the affected claims.
func computeInvalidationImpact(event ContextEvent, graph ImpactGraph, now time.Time, options *ComputeImpactOptions) ([]ImpactEdge, []StalenessMarker) {
	var edges []ImpactEdge

	for _, ref := range event.Refs {
		if ref.Type == RefTypeMemoryClaim {
			edgeID := fmt.Sprintf("edge-%s-invalidates-%s", event.ID, ref.Ref)
			edges = append(edges, ImpactEdge{
				ID:            edgeID,
				WorkspaceID:   event.WorkspaceID,
				From:          EvidenceRef{Type: RefTypeEvent, Ref: event.ID},
				To:            ref,
				Kind:          ImpactEdgeKindInvalidates,
				SourceEventID: event.ID,
				CreatedAt:     now,
			})
		}
	}

	return edges, nil
}

// traverseImpact recursively traverses the impact graph from a ref,
// collecting edges and markers for affected claims.
func traverseImpact(
	event ContextEvent,
	ref EvidenceRef,
	graph ImpactGraph,
	now time.Time,
	options *ComputeImpactOptions,
	depth int,
	edges *[]ImpactEdge,
	markers *[]StalenessMarker,
	visited map[string]bool,
) {
	if depth >= options.MaxDepth {
		return
	}

	key := FormatEvidenceRef(ref)
	if visited[key] {
		return
	}
	visited[key] = true

	// Find claims sourced from this ref
	claims := graph.ClaimsForRef(ref)
	for _, claim := range claims {
		if claim.Status == ClaimStatusCurrent {
			edgeID := fmt.Sprintf("edge-%s-invalidates-claim-%s", event.ID, claim.ID)
			*edges = append(*edges, ImpactEdge{
				ID:            edgeID,
				WorkspaceID:   event.WorkspaceID,
				From:          EvidenceRef{Type: RefTypeEvent, Ref: event.ID},
				To:            EvidenceRef{Type: RefTypeMemoryClaim, Ref: claim.ID},
				Kind:          ImpactEdgeKindInvalidates,
				SourceEventID: event.ID,
				CreatedAt:     now,
			})
		}
	}

	// Traverse forward edges
	forwardEdges := graph.ForwardEdges(ref)
	for _, edge := range forwardEdges {
		traverseImpact(event, edge.To, graph, now, options, depth+1, edges, markers, visited)
	}
}

// ---------------------------------------------------------------------------
// ApplyInvalidation — applies computed impact to the store
// ---------------------------------------------------------------------------

// InvalidationStore defines the write operations needed by ApplyInvalidation.
// This is a subset of the full Store interface.
type InvalidationStore interface {
	PutImpactEdge(ctx context.Context, edge ImpactEdge) (ImpactEdge, error)
	UpsertStaleness(ctx context.Context, marker StalenessMarker) (StalenessMarker, error)
	UpsertClaim(ctx context.Context, claim MemoryClaim) (MemoryClaim, error)
	ListImpactEdges(ctx context.Context, filter ImpactFilter) ([]ImpactEdge, error)
	ListStaleness(ctx context.Context, filter StalenessFilter) ([]StalenessMarker, error)
	GetStaleness(ctx context.Context, id string) (StalenessMarker, error)
	GetClaim(ctx context.Context, id string) (MemoryClaim, error)
	ListClaims(ctx context.Context, filter ClaimFilter) ([]MemoryClaim, error)
	AppendEvent(ctx context.Context, event ContextEvent) (ContextEvent, error)
}

// ApplyInvalidation computes and persists impact edges and staleness markers
// for an event, then updates claims as needed.
//
// VAL-IMPL-004: Atomic — all changes are computed first, then applied.
// VAL-IMPL-005: Idempotent — repeated calls with same event produce same state.
// VAL-IMPL-006: Respects context cancellation.
func ApplyInvalidation(ctx context.Context, store InvalidationStore, event ContextEvent, opts ...ComputeImpactOption) error {
	// Check context cancellation upfront
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("apply invalidation: context canceled: %w", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	options := &ComputeImpactOptions{
		MaxDepth: 10,
		Clock:    func() time.Time { return now },
	}
	for _, o := range opts {
		o(options)
	}

	// Build impact graph from store
	graph := &storeImpactGraph{store: store, ctx: ctx, ws: event.WorkspaceID}

	// Compute impact (pure function)
	edges, markers, err := ComputeImpact(event, graph, opts...)
	if err != nil {
		return fmt.Errorf("apply invalidation: compute: %w", err)
	}

	// Persist edges (idempotent — store handles dedup)
	for _, edge := range edges {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("apply invalidation: context canceled: %w", err)
		}
		if _, err := store.PutImpactEdge(ctx, edge); err != nil {
			return fmt.Errorf("apply invalidation: put edge %s: %w", edge.ID, err)
		}
	}

	// Persist markers (idempotent — upsert)
	for _, marker := range markers {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("apply invalidation: context canceled: %w", err)
		}
		if _, err := store.UpsertStaleness(ctx, marker); err != nil {
			return fmt.Errorf("apply invalidation: upsert staleness %s: %w", marker.ID, err)
		}
	}

	// Update claims based on event kind
	switch event.Kind {
	case EventKindCodeChangedDirty:
		// Mark affected claims as needs_revalidation
		for _, marker := range markers {
			if marker.TargetRef.Type == RefTypeMemoryClaim {
				claim, err := store.GetClaim(ctx, marker.TargetRef.Ref)
				if err != nil {
					continue // claim may not exist
				}
				if claim.Status == ClaimStatusCurrent {
					updated, err := ApplyClaimTransition(claim, ClaimStatusNeedsRevalidation, "dirty edit: "+event.ID, now)
					if err == nil {
						if _, upsertErr := store.UpsertClaim(ctx, updated); upsertErr != nil {
							_ = upsertErr // best-effort
						}
					}
				}
			}
		}

	case EventKindCodeCommitted:
		// Resolve dirty markers for committed refs (two-step: dirty → needs_revalidation → fresh)
		for _, ref := range event.Refs {
			dirtyMarkers, _ := store.ListStaleness(ctx, StalenessFilter{
				WorkspaceID: event.WorkspaceID,
				TargetRef:   &ref,
				Status:      StalenessStatusDirty,
			})
			for _, m := range dirtyMarkers {
				// Step 1: dirty → needs_revalidation
				intermediate, err := ApplyStalenessTransition(m, StalenessStatusNeedsRevalidation, event.ID, now)
				if err != nil {
					continue
				}
				// Step 2: needs_revalidation → fresh
				resolved, err := ApplyStalenessTransition(intermediate, StalenessStatusFresh, event.ID, now)
				if err == nil {
					if _, upsertErr := store.UpsertStaleness(ctx, resolved); upsertErr != nil {
						_ = upsertErr // best-effort resolution
					}
				}
			}
		}
		// Promote candidate claims whose source refs match committed refs
		committedSet := make(map[string]bool)
		for _, ref := range event.Refs {
			committedSet[FormatEvidenceRef(ref)] = true
		}
		claims, _ := store.ListClaims(ctx, ClaimFilter{
			WorkspaceID: event.WorkspaceID,
			Status:      ClaimStatusCandidate,
		})
		for _, claim := range claims {
			allMatch := len(claim.SourceRefs) > 0
			for _, sr := range claim.SourceRefs {
				if !committedSet[FormatEvidenceRef(sr)] {
					allMatch = false
					break
				}
			}
			if allMatch {
				promoted, err := ApplyClaimTransition(claim, ClaimStatusCurrent, "commit: "+event.ID, now)
				if err == nil {
					if _, upsertErr := store.UpsertClaim(ctx, promoted); upsertErr != nil {
						_ = upsertErr // best-effort promotion
					}
				}
			}
		}
		// Also resolve needs_revalidation markers for committed refs
		for _, ref := range event.Refs {
			nrMarkers, _ := store.ListStaleness(ctx, StalenessFilter{
				WorkspaceID: event.WorkspaceID,
				TargetRef:   &ref,
				Status:      StalenessStatusNeedsRevalidation,
			})
			for _, m := range nrMarkers {
				resolved, err := ApplyStalenessTransition(m, StalenessStatusFresh, event.ID, now)
				if err == nil {
					if _, upsertErr := store.UpsertStaleness(ctx, resolved); upsertErr != nil {
						_ = upsertErr // best-effort
					}
				}
			}
		}

	case EventKindCodeValidated:
		// Resolve needs_revalidation markers for validated refs
		for _, ref := range event.Refs {
			nrMarkers, _ := store.ListStaleness(ctx, StalenessFilter{
				WorkspaceID: event.WorkspaceID,
				TargetRef:   &ref,
				Status:      StalenessStatusNeedsRevalidation,
			})
			for _, m := range nrMarkers {
				resolved, err := ApplyStalenessTransition(m, StalenessStatusFresh, event.ID, now)
				if err == nil {
					if _, upsertErr := store.UpsertStaleness(ctx, resolved); upsertErr != nil {
						_ = upsertErr // best-effort
					}
				}
			}
		}

	case EventKindAnswerCorrected:
		// Transition implicated claims to needs_revalidation (not immediate rejection)
		for _, marker := range markers {
			if marker.TargetRef.Type == RefTypeMemoryClaim {
				claim, err := store.GetClaim(ctx, marker.TargetRef.Ref)
				if err != nil {
					continue
				}
				if claim.Status == ClaimStatusCurrent || claim.Status == ClaimStatusCandidate {
					updated, err := ApplyClaimTransition(claim, ClaimStatusNeedsRevalidation, "user correction: "+event.ID, now)
					if err == nil {
						if _, upsertErr := store.UpsertClaim(ctx, updated); upsertErr != nil {
							_ = upsertErr // best-effort
						}
					}
				}
			}
		}

	case EventKindMemoryClaimPromoted:
		// Supersede old claims
		for _, marker := range markers {
			if marker.TargetRef.Type == RefTypeMemoryClaim {
				claim, err := store.GetClaim(ctx, marker.TargetRef.Ref)
				if err != nil {
					continue
				}
				if claim.Status == ClaimStatusCurrent {
					updated, err := ApplyClaimTransition(claim, ClaimStatusSuperseded, "superseded by promotion: "+event.ID, now)
					if err == nil {
						if _, upsertErr := store.UpsertClaim(ctx, updated); upsertErr != nil {
							_ = upsertErr // best-effort
						}
					}
				}
			}
		}

	case EventKindMemoryClaimInvalidated:
		// Reject implicated claims
		for _, ref := range event.Refs {
			if ref.Type == RefTypeMemoryClaim {
				claim, err := store.GetClaim(ctx, ref.Ref)
				if err != nil {
					continue
				}
				if claim.Status == ClaimStatusNeedsRevalidation || claim.Status == ClaimStatusCandidate {
					updated, err := ApplyClaimTransition(claim, ClaimStatusRejected, "invalidated: "+event.ID, now)
					if err == nil {
						if _, upsertErr := store.UpsertClaim(ctx, updated); upsertErr != nil {
							_ = upsertErr // best-effort
						}
					}
				}
			}
		}
	}

	return nil
}

// ResolveStaleness resolves a staleness marker by transitioning it to a resolved
// status (fresh or superseded) and recording the resolving event.
//
// VAL-IMPL-007: Transitions marker correctly.
// VAL-IMPL-008: Guards double-resolve.
// VAL-IMPL-009: Returns ENOTFOUND for missing marker.
func ResolveStaleness(ctx context.Context, store InvalidationStore, markerID string, resolvingEventID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("resolve staleness: context canceled: %w", err)
	}

	marker, err := store.GetStaleness(ctx, markerID)
	if err != nil {
		return fmt.Errorf("resolve staleness: marker %q not found: %w", markerID, err)
	}

	// Guard: already resolved (fresh or superseded are terminal resolved states)
	if marker.Status == StalenessStatusFresh || marker.Status == StalenessStatusSuperseded {
		return fmt.Errorf("resolve staleness: marker %q already resolved (status=%s)", markerID, marker.Status)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Determine target status based on current status.
	// Some statuses require multi-step transitions (e.g., dirty → needs_revalidation → fresh).
	var intermediateStatus StalenessStatus
	var targetStatus StalenessStatus
	switch marker.Status {
	case StalenessStatusDirty:
		// Two-step: dirty → needs_revalidation → fresh
		intermediateStatus = StalenessStatusNeedsRevalidation
		targetStatus = StalenessStatusFresh
	case StalenessStatusNeedsRevalidation:
		targetStatus = StalenessStatusFresh
	case StalenessStatusStale:
		targetStatus = StalenessStatusSuperseded
	case StalenessStatusUnknown:
		targetStatus = StalenessStatusFresh
	default:
		return fmt.Errorf("resolve staleness: cannot resolve marker with status %q", marker.Status)
	}

	// Apply intermediate transition if needed (e.g., dirty → needs_revalidation)
	if intermediateStatus != "" {
		intermediate, err := ApplyStalenessTransition(marker, intermediateStatus, resolvingEventID, now)
		if err != nil {
			return fmt.Errorf("resolve staleness: intermediate transition failed: %w", err)
		}
		marker = intermediate
	}

	resolved, err := ApplyStalenessTransition(marker, targetStatus, resolvingEventID, now)
	if err != nil {
		return fmt.Errorf("resolve staleness: transition failed: %w", err)
	}

	if _, err := store.UpsertStaleness(ctx, resolved); err != nil {
		return fmt.Errorf("resolve staleness: upsert failed: %w", err)
	}

	return nil
}

// storeImpactGraph adapts a Store to the ImpactGraph interface.
type storeImpactGraph struct {
	store InvalidationStore
	ctx   context.Context
	ws    string
}

func (g *storeImpactGraph) ForwardEdges(ref EvidenceRef) []ImpactEdge {
	edges, _ := g.store.ListImpactEdges(g.ctx, ImpactFilter{
		WorkspaceID: g.ws,
		FromRef:     &ref,
	})
	return edges
}

func (g *storeImpactGraph) ReverseEdges(ref EvidenceRef) []ImpactEdge {
	edges, _ := g.store.ListImpactEdges(g.ctx, ImpactFilter{
		WorkspaceID: g.ws,
		ToRef:       &ref,
	})
	return edges
}

func (g *storeImpactGraph) ClaimsForRef(ref EvidenceRef) []MemoryClaim {
	var result []MemoryClaim
	claims, _ := g.store.ListClaims(g.ctx, ClaimFilter{WorkspaceID: g.ws})
	for _, c := range claims {
		for _, sr := range c.SourceRefs {
			if sr.Equal(ref) {
				result = append(result, c)
				break
			}
		}
	}
	return result
}

func (g *storeImpactGraph) AllClaims() []MemoryClaim {
	claims, _ := g.store.ListClaims(g.ctx, ClaimFilter{WorkspaceID: g.ws})
	return claims
}
