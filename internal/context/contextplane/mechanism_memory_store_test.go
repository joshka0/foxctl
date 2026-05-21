package contextplane

import (
	"context"
	"fmt"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
)

func TestSearchMechanismMemoryCollisionsUsesPersistedDualVectors(t *testing.T) {
	ctx := context.Background()
	store, err := memorystore.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer func() { _ = store.Close() }()

	workspaceID := "ws-mechanism-store"
	query := testMechanismProjection(workspaceID, "query-traffic", "Urban Logistics", "Emergency vehicles need local intersection clearance without a central controller.", "decentralized local priority clearing")
	immunology := testMechanismProjection(workspaceID, "memory-immunology", "Immunology", "White cells coordinate local response without central approval.", "decentralized local threat response")
	immunology.MechanismTags = []string{"localized_response", "distributed_clearance"}
	traffic := testMechanismProjection(workspaceID, "memory-traffic", "Urban Logistics", "Traffic lights coordinate emergency lanes.", "same-domain route clearance")
	weak := testMechanismProjection(workspaceID, "memory-weak", "Mycology", "A weak unrelated mechanism.", "central scheduling")

	provider := newExactMechanismEmbeddingProvider(t, []mechanismTestProjectionVectors{
		{projection: query, vectors: mechanismTestVectors{literal: []float32{1, 0, 0}, structural: []float32{1, 0, 0}}},
		{projection: immunology, vectors: mechanismTestVectors{literal: []float32{0, 1, 0}, structural: []float32{1, 0, 0}}},
		{projection: traffic, vectors: mechanismTestVectors{literal: []float32{1, 0, 0}, structural: []float32{1, 0, 0}}},
		{projection: weak, vectors: mechanismTestVectors{literal: []float32{0, 1, 0}, structural: []float32{0, 1, 0}}},
	})

	for _, projection := range []MechanismProjection{immunology, traffic, weak} {
		report, err := PersistMechanismMemoryProjection(ctx, store, provider, projection)
		if err != nil {
			t.Fatalf("persist %s: %v", projection.ID, err)
		}
		if report.Stored != 2 || report.Embedded != 2 {
			t.Fatalf("persist report for %s = %#v, want two stored and embedded artifacts", projection.ID, report)
		}
	}

	result, err := SearchMechanismMemoryCollisions(ctx, store, provider, query, MechanismMemoryCollisionSearchOptions{
		Entropy:   0.4,
		Threshold: 0.70,
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("search mechanism collisions: %v", err)
	}
	if result.StructuralCandidates != 2 {
		t.Fatalf("StructuralCandidates=%d want immunology and same-domain traffic candidates", result.StructuralCandidates)
	}
	if len(result.Plan.Cells) != 1 {
		t.Fatalf("cells=%d skipped=%d result=%#v", len(result.Plan.Cells), result.Plan.Skipped, result)
	}
	cell := result.Plan.Cells[0]
	if cell.MemoryID != "memory-immunology" {
		t.Fatalf("top memory=%q want memory-immunology; cells=%#v", cell.MemoryID, result.Plan.Cells)
	}
	if cell.MemoryDomain != "Immunology" || cell.QueryDomain != "Urban Logistics" {
		t.Fatalf("domains not preserved: %#v", cell)
	}
	if cell.StructuralSimilarity != 1 || cell.LiteralSimilarity >= 0.2 {
		t.Fatalf("dual-vector scoring not applied: structural=%v literal=%v", cell.StructuralSimilarity, cell.LiteralSimilarity)
	}
	if !containsString(cell.MechanismTags, "localized_response") || !containsString(cell.MechanismTags, "distributed_clearance") {
		t.Fatalf("mechanism tags not surfaced on collision cell: %#v", cell.MechanismTags)
	}
}

type mechanismTestVectors struct {
	literal    []float32
	structural []float32
}

type mechanismTestProjectionVectors struct {
	projection MechanismProjection
	vectors    mechanismTestVectors
}

type exactMechanismEmbeddingProvider struct {
	vectors map[string][]float32
	dims    int
}

func newExactMechanismEmbeddingProvider(t *testing.T, projections []mechanismTestProjectionVectors) exactMechanismEmbeddingProvider {
	t.Helper()
	vectors := map[string][]float32{}
	dims := 0
	for _, item := range projections {
		artifacts, err := PlanMechanismMemoryArtifacts(item.projection)
		if err != nil {
			t.Fatalf("plan artifacts for %s: %v", item.projection.ID, err)
		}
		for _, artifact := range artifacts {
			var vec []float32
			switch artifact.View {
			case MechanismMemoryViewLiteral:
				vec = item.vectors.literal
			case MechanismMemoryViewStructural:
				vec = item.vectors.structural
			default:
				t.Fatalf("unexpected artifact view %q", artifact.View)
			}
			if dims == 0 {
				dims = len(vec)
			}
			vectors[artifact.EmbeddingText] = append([]float32(nil), vec...)
		}
	}
	return exactMechanismEmbeddingProvider{vectors: vectors, dims: dims}
}

func (p exactMechanismEmbeddingProvider) Embed(_ context.Context, text string) ([]float32, error) {
	vec, ok := p.vectors[text]
	if !ok {
		return nil, fmt.Errorf("missing test embedding for %q", text)
	}
	return append([]float32(nil), vec...), nil
}

func (p exactMechanismEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec, err := p.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, vec)
	}
	return out, nil
}

func (p exactMechanismEmbeddingProvider) Model() string {
	return "exact-mechanism-test"
}

func (p exactMechanismEmbeddingProvider) Dimensions() int {
	return p.dims
}

func testMechanismProjection(workspaceID, id, domain, summary, schema string) MechanismProjection {
	return MechanismProjection{
		ID:             id,
		WorkspaceID:    workspaceID,
		OriginalDomain: domain,
		Summary:        summary,
		LiteralText:    summary,
		AbstractSchema: schema,
		MechanismTags:  []string{"structural_match"},
		SourceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeMemoryClaim, Ref: "claim-" + id},
		},
	}
}
