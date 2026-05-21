package contextplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/storage"
)

type MechanismMemoryPersistedArtifact struct {
	Name          string              `json:"name"`
	Type          string              `json:"type"`
	View          MechanismMemoryView `json:"view"`
	Summary       string              `json:"summary,omitempty"`
	EmbeddingDims int                 `json:"embedding_dims,omitempty"`
}

type MechanismMemoryPersistReport struct {
	WorkspaceID string                             `json:"workspace_id"`
	Projection  string                             `json:"projection"`
	Stored      int                                `json:"stored"`
	Embedded    int                                `json:"embedded"`
	Artifacts   []MechanismMemoryPersistedArtifact `json:"artifacts"`
}

type MechanismMemoryCollisionSearchOptions struct {
	WorkspaceID       string  `json:"workspace_id,omitempty"`
	CandidateLimit    int     `json:"candidate_limit,omitempty"`
	Entropy           float64 `json:"entropy,omitempty"`
	Threshold         float64 `json:"threshold,omitempty"`
	Limit             int     `json:"limit,omitempty"`
	IncludeSameDomain bool    `json:"include_same_domain,omitempty"`
	Strategy          string  `json:"strategy,omitempty"`
}

type MechanismMemoryCollisionSearchResult struct {
	WorkspaceID          string              `json:"workspace_id"`
	Query                MechanismQuery      `json:"query"`
	Plan                 MemoryCollisionPlan `json:"plan"`
	StructuralCandidates int                 `json:"structural_candidates"`
	MemoriesLoaded       int                 `json:"memories_loaded"`
	SkippedPairs         int                 `json:"skipped_pairs"`
}

// PersistMechanismMemoryProjection writes the literal and structural views of a
// blurred mechanism into named memory. Embeddings are optional but required for
// later structural collision search.
func PersistMechanismMemoryProjection(ctx context.Context, memStore storage.MemoryStore, provider semantic.EmbeddingProvider, projection MechanismProjection) (MechanismMemoryPersistReport, error) {
	if memStore == nil {
		return MechanismMemoryPersistReport{}, fmt.Errorf("mechanism memory: memory store required")
	}
	projection.WorkspaceID = strings.TrimSpace(projection.WorkspaceID)
	if projection.WorkspaceID == "" {
		return MechanismMemoryPersistReport{}, fmt.Errorf("mechanism memory: workspace_id required")
	}
	artifacts, err := PlanMechanismMemoryArtifacts(projection)
	if err != nil {
		return MechanismMemoryPersistReport{}, err
	}

	report := MechanismMemoryPersistReport{
		WorkspaceID: projection.WorkspaceID,
		Projection:  strings.TrimSpace(projection.ID),
		Artifacts:   make([]MechanismMemoryPersistedArtifact, 0, len(artifacts)),
	}
	for _, artifact := range artifacts {
		saved, err := memStore.SaveFromResult(ctx, artifact.Name, artifact.Type, projection.WorkspaceID, artifact.Summary, artifact.Result)
		if err != nil {
			return report, fmt.Errorf("save mechanism memory %s: %w", artifact.Name, err)
		}
		persisted := MechanismMemoryPersistedArtifact{
			Name:    saved.Name,
			Type:    saved.Type,
			View:    artifact.View,
			Summary: saved.Summary,
		}
		report.Stored++
		if provider != nil {
			vec, err := provider.Embed(ctx, artifact.EmbeddingText)
			if err != nil {
				return report, fmt.Errorf("embed mechanism memory %s: %w", artifact.Name, err)
			}
			if err := memStore.UpdateEmbedding(ctx, artifact.Name, projection.WorkspaceID, vec); err != nil {
				return report, fmt.Errorf("store mechanism memory embedding %s: %w", artifact.Name, err)
			}
			persisted.EmbeddingDims = len(vec)
			report.Embedded++
		}
		report.Artifacts = append(report.Artifacts, persisted)
	}
	return report, nil
}

// SearchMechanismMemoryCollisions embeds a query projection, retrieves nearby
// structural mechanism memories, rehydrates their paired literal vectors, and
// delegates scoring to the pure collision planner.
func SearchMechanismMemoryCollisions(ctx context.Context, memStore storage.MemoryStore, provider semantic.EmbeddingProvider, queryProjection MechanismProjection, opts MechanismMemoryCollisionSearchOptions) (MechanismMemoryCollisionSearchResult, error) {
	if memStore == nil {
		return MechanismMemoryCollisionSearchResult{}, fmt.Errorf("mechanism memory: memory store required")
	}
	if provider == nil {
		return MechanismMemoryCollisionSearchResult{}, fmt.Errorf("mechanism memory: embedding provider required")
	}
	queryProjection.WorkspaceID = firstNonEmpty(strings.TrimSpace(opts.WorkspaceID), strings.TrimSpace(queryProjection.WorkspaceID))
	if queryProjection.WorkspaceID == "" {
		return MechanismMemoryCollisionSearchResult{}, fmt.Errorf("mechanism memory: workspace_id required")
	}

	query, err := mechanismQueryFromProjection(ctx, provider, queryProjection)
	if err != nil {
		return MechanismMemoryCollisionSearchResult{}, err
	}
	candidateLimit := opts.CandidateLimit
	if candidateLimit <= 0 {
		planLimit := opts.Limit
		if planLimit <= 0 {
			planLimit = defaultMemoryCollisionLimit
		}
		candidateLimit = max(planLimit*5, 50)
	}

	structuralHits, err := memStore.SearchSimilarByType(ctx, queryProjection.WorkspaceID, MechanismMemoryStructuralType, query.StructuralVector, candidateLimit)
	if err != nil {
		return MechanismMemoryCollisionSearchResult{}, fmt.Errorf("search structural mechanism memories: %w", err)
	}
	memories, skippedPairs := mechanismMemoriesFromStructuralHits(ctx, memStore, queryProjection.WorkspaceID, queryProjection.ID, structuralHits)
	plan := PlanMemoryCollisionCells(MemoryCollisionInput{
		WorkspaceID:       queryProjection.WorkspaceID,
		Query:             query,
		Memories:          memories,
		Entropy:           opts.Entropy,
		Threshold:         opts.Threshold,
		Limit:             opts.Limit,
		IncludeSameDomain: opts.IncludeSameDomain,
		Strategy:          opts.Strategy,
	})

	return MechanismMemoryCollisionSearchResult{
		WorkspaceID:          queryProjection.WorkspaceID,
		Query:                query,
		Plan:                 plan,
		StructuralCandidates: len(structuralHits),
		MemoriesLoaded:       len(memories),
		SkippedPairs:         skippedPairs,
	}, nil
}

func mechanismQueryFromProjection(ctx context.Context, provider semantic.EmbeddingProvider, projection MechanismProjection) (MechanismQuery, error) {
	artifacts, err := PlanMechanismMemoryArtifacts(projection)
	if err != nil {
		return MechanismQuery{}, err
	}
	var literalVector []float32
	var structuralVector []float32
	for _, artifact := range artifacts {
		vec, err := embedMechanismMemoryQueryText(ctx, provider, artifact.EmbeddingText)
		if err != nil {
			return MechanismQuery{}, fmt.Errorf("embed mechanism query %s: %w", artifact.View, err)
		}
		switch artifact.View {
		case MechanismMemoryViewLiteral:
			literalVector = vec
		case MechanismMemoryViewStructural:
			structuralVector = vec
		}
	}
	return MechanismQuery{
		ID:               strings.TrimSpace(projection.ID),
		Domain:           strings.TrimSpace(projection.OriginalDomain),
		Text:             strings.TrimSpace(projection.Summary),
		AbstractSchema:   strings.TrimSpace(projection.AbstractSchema),
		MechanismTags:    normalizeMechanismTags(projection.MechanismTags),
		LiteralVector:    literalVector,
		StructuralVector: structuralVector,
		SourceRefs:       compactEvidenceRefs(projection.SourceRefs),
	}, nil
}

func embedMechanismMemoryQueryText(ctx context.Context, provider semantic.EmbeddingProvider, text string) ([]float32, error) {
	if qp, ok := provider.(semantic.QueryEmbeddingProvider); ok {
		vec, err := qp.EmbedQuery(ctx, text)
		if err == nil && len(vec) > 0 {
			return vec, nil
		}
	}
	return provider.Embed(ctx, text)
}

func mechanismMemoriesFromStructuralHits(ctx context.Context, memStore storage.MemoryStore, workspaceID, queryProjectionID string, hits []storage.ScoredEntry) ([]MechanismMemory, int) {
	memories := make([]MechanismMemory, 0, len(hits))
	seen := map[string]struct{}{}
	skipped := 0
	for _, hit := range hits {
		projection, view, ok := DecodeMechanismMemoryArtifact(hit.Entry)
		if !ok || view != MechanismMemoryViewStructural {
			skipped++
			continue
		}
		if strings.TrimSpace(projection.ID) == "" || strings.TrimSpace(projection.ID) == strings.TrimSpace(queryProjectionID) {
			skipped++
			continue
		}
		if _, exists := seen[projection.ID]; exists {
			skipped++
			continue
		}

		literalName := mechanismMemoryName(projection.ID, MechanismMemoryViewLiteral)
		literalEntry, err := memStore.Get(ctx, literalName, workspaceID)
		if err != nil {
			skipped++
			continue
		}
		literalVector, err := memStore.GetEmbedding(ctx, literalEntry.Name, workspaceID)
		if err != nil || len(literalVector) == 0 {
			skipped++
			continue
		}
		structuralVector, err := memStore.GetEmbedding(ctx, hit.Entry.Name, workspaceID)
		if err != nil || len(structuralVector) == 0 {
			skipped++
			continue
		}
		memory, ok := MechanismMemoryFromArtifacts(literalEntry, hit.Entry, literalVector, structuralVector)
		if !ok {
			skipped++
			continue
		}
		seen[memory.ID] = struct{}{}
		memories = append(memories, memory)
	}
	return memories, skipped
}
