package contextengine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// MemoryStore is an in-memory implementation of InvalidationStore for testing.
// It is NOT production-grade — it uses maps and a mutex for simplicity.
type MemoryStore struct {
	mu          sync.Mutex
	events      map[string]ContextEvent
	edges       map[string]ImpactEdge
	markers     map[string]StalenessMarker
	claims      map[string]MemoryClaim
	packs       map[string]EvidencePack
	nodes       map[string]EvidenceNode
	episodes    map[string]RetrievalEpisode
	feedback    map[string]RetrievalFeedback
	projections map[string]ProjectionRow
}

// NewMemoryStore creates a new empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		events:      make(map[string]ContextEvent),
		edges:       make(map[string]ImpactEdge),
		markers:     make(map[string]StalenessMarker),
		claims:      make(map[string]MemoryClaim),
		packs:       make(map[string]EvidencePack),
		nodes:       make(map[string]EvidenceNode),
		episodes:    make(map[string]RetrievalEpisode),
		feedback:    make(map[string]RetrievalFeedback),
		projections: make(map[string]ProjectionRow),
	}
}

func (s *MemoryStore) Close() error { return nil }

// Events
func (s *MemoryStore) AppendEvent(_ context.Context, event ContextEvent) (ContextEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.events[event.ID]; exists {
		return ContextEvent{}, fmt.Errorf("event %s already exists", event.ID)
	}
	s.events[event.ID] = event
	return event, nil
}

func (s *MemoryStore) ListEvents(_ context.Context, filter EventFilter) ([]ContextEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []ContextEvent
	for _, e := range s.events {
		if filter.WorkspaceID != "" && e.WorkspaceID != filter.WorkspaceID {
			continue
		}
		if filter.Kind != "" && e.Kind != filter.Kind {
			continue
		}
		if filter.TaskID != "" && e.TaskID != filter.TaskID {
			continue
		}
		if filter.SessionID != "" && e.SessionID != filter.SessionID {
			continue
		}
		result = append(result, e)
	}
	// Sort by CreatedAt desc (simple insertion sort for test data)
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].CreatedAt.Before(result[j].CreatedAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

// Evidence packs
func (s *MemoryStore) PutEvidencePack(_ context.Context, pack EvidencePack) (EvidencePack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packs[pack.ID] = pack
	return pack, nil
}

func (s *MemoryStore) GetEvidencePack(_ context.Context, id string) (EvidencePack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.packs[id]
	if !ok {
		return EvidencePack{}, fmt.Errorf("pack %s not found", id)
	}
	return p, nil
}

// Evidence nodes
func (s *MemoryStore) PutEvidenceNode(_ context.Context, node EvidenceNode) (EvidenceNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[node.ID] = node
	return node, nil
}

func (s *MemoryStore) GetEvidenceNode(_ context.Context, id string) (EvidenceNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[id]
	if !ok {
		return EvidenceNode{}, fmt.Errorf("node %s not found", id)
	}
	return n, nil
}

func (s *MemoryStore) ListEvidenceNodes(_ context.Context, workspaceID string, _ EvidenceRef, _ int) ([]EvidenceNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []EvidenceNode
	for _, n := range s.nodes {
		if n.WorkspaceID == workspaceID {
			result = append(result, n)
		}
	}
	return result, nil
}

// Claims
func (s *MemoryStore) UpsertClaim(_ context.Context, claim MemoryClaim) (MemoryClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims[claim.ID] = claim
	return claim, nil
}

func (s *MemoryStore) GetClaim(_ context.Context, id string) (MemoryClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.claims[id]
	if !ok {
		return MemoryClaim{}, fmt.Errorf("claim %s not found", id)
	}
	return c, nil
}

func (s *MemoryStore) ListClaims(_ context.Context, filter ClaimFilter) ([]MemoryClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []MemoryClaim
	for _, c := range s.claims {
		if filter.WorkspaceID != "" && c.WorkspaceID != filter.WorkspaceID {
			continue
		}
		if filter.Status != "" && c.Status != filter.Status {
			continue
		}
		result = append(result, c)
	}
	return result, nil
}

// Impact edges
func (s *MemoryStore) PutImpactEdge(_ context.Context, edge ImpactEdge) (ImpactEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Idempotent: check if edge already exists by (from, to, kind) tuple
	for _, existing := range s.edges {
		if existing.From.Equal(edge.From) && existing.To.Equal(edge.To) && existing.Kind == edge.Kind {
			return existing, nil
		}
	}
	s.edges[edge.ID] = edge
	return edge, nil
}

func (s *MemoryStore) ListImpactEdges(_ context.Context, filter ImpactFilter) ([]ImpactEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []ImpactEdge
	for _, e := range s.edges {
		if filter.WorkspaceID != "" && e.WorkspaceID != filter.WorkspaceID {
			continue
		}
		if filter.FromRef != nil && !e.From.Equal(*filter.FromRef) {
			continue
		}
		if filter.ToRef != nil && !e.To.Equal(*filter.ToRef) {
			continue
		}
		if filter.Kind != "" && e.Kind != filter.Kind {
			continue
		}
		result = append(result, e)
	}
	return result, nil
}

func (s *MemoryStore) ReverseImpact(_ context.Context, _ string, target EvidenceRef) ([]ImpactEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []ImpactEdge
	for _, e := range s.edges {
		if e.To.Equal(target) {
			result = append(result, e)
		}
	}
	return result, nil
}

// Staleness markers
func (s *MemoryStore) UpsertStaleness(_ context.Context, marker StalenessMarker) (StalenessMarker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markers[marker.ID] = marker
	return marker, nil
}

func (s *MemoryStore) GetStaleness(_ context.Context, id string) (StalenessMarker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.markers[id]
	if !ok {
		return StalenessMarker{}, fmt.Errorf("staleness marker %s not found", id)
	}
	return m, nil
}

func (s *MemoryStore) ListStaleness(_ context.Context, filter StalenessFilter) ([]StalenessMarker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []StalenessMarker
	for _, m := range s.markers {
		if filter.WorkspaceID != "" && m.WorkspaceID != filter.WorkspaceID {
			continue
		}
		if filter.Status != "" && m.Status != filter.Status {
			continue
		}
		if filter.TargetRef != nil && !m.TargetRef.Equal(*filter.TargetRef) {
			continue
		}
		result = append(result, m)
	}
	return result, nil
}

// Projections
func (s *MemoryStore) PutProjection(_ context.Context, id, workspaceID, projectionType string, version int, taskID string, generatedFromEvents []string, payload any, generatedAt, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var payloadBytes []byte
	switch p := payload.(type) {
	case []byte:
		payloadBytes = p
	case json.RawMessage:
		payloadBytes = p
	default:
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal projection payload: %w", err)
		}
	}
	s.projections[id] = ProjectionRow{
		ID:                  id,
		WorkspaceID:         workspaceID,
		ProjectionType:      projectionType,
		ProjectionVersion:   version,
		TaskID:              taskID,
		GeneratedFromEvents: generatedFromEvents,
		Payload:             payloadBytes,
		GeneratedAt:         generatedAt,
		ExpiresAt:           expiresAt,
	}
	return nil
}

func (s *MemoryStore) GetProjection(_ context.Context, _, id string) (string, int, string, []string, []byte, time.Time, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projections[id]
	if !ok {
		return "", 0, "", nil, nil, time.Time{}, time.Time{}, fmt.Errorf("projection %s not found", id)
	}
	return p.ProjectionType, p.ProjectionVersion, p.TaskID, p.GeneratedFromEvents, p.Payload, p.GeneratedAt, p.ExpiresAt, nil
}

func (s *MemoryStore) ListProjections(_ context.Context, filter ProjectionFilter) ([]ProjectionRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []ProjectionRow
	for _, p := range s.projections {
		if filter.WorkspaceID != "" && p.WorkspaceID != filter.WorkspaceID {
			continue
		}
		if filter.ProjectionType != "" && p.ProjectionType != filter.ProjectionType {
			continue
		}
		result = append(result, p)
	}
	return result, nil
}

// Retrieval episodes
func (s *MemoryStore) RecordRetrievalEpisode(_ context.Context, episode RetrievalEpisode) (RetrievalEpisode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.episodes[episode.ID]; exists {
		return RetrievalEpisode{}, fmt.Errorf("episode %s already exists", episode.ID)
	}
	s.episodes[episode.ID] = episode
	return episode, nil
}

func (s *MemoryStore) GetRetrievalEpisode(_ context.Context, id string) (RetrievalEpisode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.episodes[id]
	if !ok {
		return RetrievalEpisode{}, fmt.Errorf("episode %s not found", id)
	}
	return e, nil
}

// Retrieval feedback
func (s *MemoryStore) RecordRetrievalFeedback(_ context.Context, feedback RetrievalFeedback) (RetrievalFeedback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.feedback[feedback.ID]; exists {
		return RetrievalFeedback{}, fmt.Errorf("feedback %s already exists", feedback.ID)
	}
	s.feedback[feedback.ID] = feedback
	return feedback, nil
}

func (s *MemoryStore) GetRetrievalFeedback(_ context.Context, id string) (RetrievalFeedback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.feedback[id]
	if !ok {
		return RetrievalFeedback{}, fmt.Errorf("feedback %s not found", id)
	}
	return f, nil
}

func (s *MemoryStore) ExplainQueryPlan(_ context.Context, _ string, _ ...any) (string, error) {
	return "", nil
}
