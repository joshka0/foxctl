package contextplane

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/storage/vector"
)

const (
	defaultMemoryCollisionEntropy   = 0.35
	defaultMemoryCollisionThreshold = 0.70
	defaultMemoryCollisionLimit     = 10
	defaultMemoryCollisionStrategy  = "memory_structural_collision"
)

// MechanismQuery is the active problem shape used for memory collision planning.
// Callers provide literal and structural vectors; this package only scores and
// routes already-computed projections.
type MechanismQuery struct {
	ID               string                      `json:"id"`
	Domain           string                      `json:"domain"`
	Text             string                      `json:"text"`
	AbstractSchema   string                      `json:"abstract_schema,omitempty"`
	MechanismTags    []string                    `json:"mechanism_tags,omitempty"`
	LiteralVector    []float32                   `json:"literal_vector"`
	StructuralVector []float32                   `json:"structural_vector"`
	SourceRefs       []contextengine.EvidenceRef `json:"source_refs,omitempty"`
}

// MechanismMemory is a reviewed or draft memory with both literal and structural
// projections. OriginalDomain is the domain box the bisociative router tries to
// escape.
type MechanismMemory struct {
	ID               string                      `json:"id"`
	OriginalDomain   string                      `json:"original_domain"`
	Summary          string                      `json:"summary"`
	AbstractSchema   string                      `json:"abstract_schema"`
	MechanismTags    []string                    `json:"mechanism_tags,omitempty"`
	LiteralVector    []float32                   `json:"literal_vector"`
	StructuralVector []float32                   `json:"structural_vector"`
	SourceRefs       []contextengine.EvidenceRef `json:"source_refs,omitempty"`
}

// MemoryCollisionInput configures one deterministic collision-cell planning pass.
type MemoryCollisionInput struct {
	WorkspaceID       string            `json:"workspace_id"`
	Query             MechanismQuery    `json:"query"`
	Memories          []MechanismMemory `json:"memories"`
	Entropy           float64           `json:"entropy,omitempty"`
	Threshold         float64           `json:"threshold,omitempty"`
	Limit             int               `json:"limit,omitempty"`
	IncludeSameDomain bool              `json:"include_same_domain,omitempty"`
	Strategy          string            `json:"strategy,omitempty"`
}

// MemoryCollisionPlan is the pure output of one collision-cell planning pass.
type MemoryCollisionPlan struct {
	Cells   []MemoryCollisionCell `json:"cells"`
	Skipped int                   `json:"skipped"`
}

// MemoryCollisionCell is a candidate work unit for downstream collider skills
// or room tasks. It is intentionally a descriptor, not an orchestrator.
type MemoryCollisionCell struct {
	DedupeKey            string                      `json:"dedupe_key"`
	CollisionID          string                      `json:"collision_id"`
	TextID               string                      `json:"text_id"`
	SetID                string                      `json:"set_id"`
	Strategy             string                      `json:"strategy"`
	QueryDomain          string                      `json:"query_domain"`
	MemoryDomain         string                      `json:"memory_domain"`
	QueryText            string                      `json:"query_text"`
	MemoryID             string                      `json:"memory_id"`
	MemorySummary        string                      `json:"memory_summary,omitempty"`
	AbstractSchema       string                      `json:"abstract_schema"`
	MechanismTags        []string                    `json:"mechanism_tags,omitempty"`
	LiteralSimilarity    float64                     `json:"literal_similarity"`
	StructuralSimilarity float64                     `json:"structural_similarity"`
	CollisionScore       float64                     `json:"collision_score"`
	SourceRefs           []contextengine.EvidenceRef `json:"source_refs,omitempty"`
	Reason               string                      `json:"reason"`
}

// PlanMemoryCollisionCells scores cross-domain mechanism memories by structural
// similarity while penalizing literal sameness. It performs no embedding,
// storage, Obsidian, or collider orchestration side effects.
func PlanMemoryCollisionCells(input MemoryCollisionInput) MemoryCollisionPlan {
	entropy := normalizeMemoryCollisionEntropy(input.Entropy)
	threshold := input.Threshold
	if threshold <= 0 {
		threshold = defaultMemoryCollisionThreshold
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultMemoryCollisionLimit
	}
	strategy := strings.TrimSpace(input.Strategy)
	if strategy == "" {
		strategy = defaultMemoryCollisionStrategy
	}

	query := input.Query
	queryDomain := strings.TrimSpace(query.Domain)
	queryText := strings.TrimSpace(query.Text)
	textID := stableCollisionComponentID("text", firstNonEmpty(query.ID, queryText))

	var plan MemoryCollisionPlan
	if queryDomain == "" || len(query.LiteralVector) == 0 || len(query.StructuralVector) == 0 {
		plan.Skipped = len(input.Memories)
		return plan
	}

	seen := map[string]struct{}{}
	for _, memory := range input.Memories {
		cell, ok := planMemoryCollisionCell(input.WorkspaceID, query, memory, textID, strategy, entropy, threshold, input.IncludeSameDomain)
		if !ok {
			plan.Skipped++
			continue
		}
		if _, exists := seen[cell.DedupeKey]; exists {
			plan.Skipped++
			continue
		}
		seen[cell.DedupeKey] = struct{}{}
		plan.Cells = append(plan.Cells, cell)
	}

	sort.SliceStable(plan.Cells, func(i, j int) bool {
		left, right := plan.Cells[i], plan.Cells[j]
		if left.CollisionScore != right.CollisionScore {
			return left.CollisionScore > right.CollisionScore
		}
		if left.StructuralSimilarity != right.StructuralSimilarity {
			return left.StructuralSimilarity > right.StructuralSimilarity
		}
		return left.DedupeKey < right.DedupeKey
	})
	if len(plan.Cells) > limit {
		plan.Skipped += len(plan.Cells) - limit
		plan.Cells = plan.Cells[:limit]
	}
	return plan
}

func planMemoryCollisionCell(workspaceID string, query MechanismQuery, memory MechanismMemory, textID, strategy string, entropy, threshold float64, includeSameDomain bool) (MemoryCollisionCell, bool) {
	queryDomain := strings.TrimSpace(query.Domain)
	memoryDomain := strings.TrimSpace(memory.OriginalDomain)
	if memoryDomain == "" || strings.TrimSpace(memory.ID) == "" {
		return MemoryCollisionCell{}, false
	}
	if !includeSameDomain && strings.EqualFold(queryDomain, memoryDomain) {
		return MemoryCollisionCell{}, false
	}
	if len(query.LiteralVector) != len(memory.LiteralVector) || len(query.StructuralVector) != len(memory.StructuralVector) {
		return MemoryCollisionCell{}, false
	}

	literalSimilarity := vector.Cosine(query.LiteralVector, memory.LiteralVector)
	structuralSimilarity := vector.Cosine(query.StructuralVector, memory.StructuralVector)
	if !validCollisionScore(literalSimilarity) || !validCollisionScore(structuralSimilarity) {
		return MemoryCollisionCell{}, false
	}
	collisionScore := structuralSimilarity*(1+entropy) - literalSimilarity*entropy
	if !validCollisionScore(collisionScore) || collisionScore < threshold {
		return MemoryCollisionCell{}, false
	}

	dedupeKey := memoryCollisionDedupeKey(workspaceID, query, memory)
	setID := stableCollisionComponentID("set", firstNonEmpty(memory.ID, memory.AbstractSchema))
	return MemoryCollisionCell{
		DedupeKey:            dedupeKey,
		CollisionID:          "memory_collision:" + digestParts(workspaceID, dedupeKey)[:24],
		TextID:               textID,
		SetID:                setID,
		Strategy:             strategy,
		QueryDomain:          queryDomain,
		MemoryDomain:         memoryDomain,
		QueryText:            strings.TrimSpace(query.Text),
		MemoryID:             strings.TrimSpace(memory.ID),
		MemorySummary:        strings.TrimSpace(memory.Summary),
		AbstractSchema:       strings.TrimSpace(memory.AbstractSchema),
		MechanismTags:        normalizeMechanismTags(memory.MechanismTags),
		LiteralSimilarity:    roundCollisionScore(literalSimilarity),
		StructuralSimilarity: roundCollisionScore(structuralSimilarity),
		CollisionScore:       roundCollisionScore(collisionScore),
		SourceRefs:           compactEvidenceRefs(append(append([]contextengine.EvidenceRef(nil), query.SourceRefs...), memory.SourceRefs...)),
		Reason:               fmt.Sprintf("structural match across domains: %s -> %s", queryDomain, memoryDomain),
	}, true
}

func normalizeMemoryCollisionEntropy(value float64) float64 {
	switch {
	case value == 0:
		return defaultMemoryCollisionEntropy
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func memoryCollisionDedupeKey(workspaceID string, query MechanismQuery, memory MechanismMemory) string {
	return "memory_collision:" + digestParts(
		workspaceID,
		query.ID,
		query.Domain,
		query.Text,
		memory.ID,
		memory.OriginalDomain,
		memory.AbstractSchema,
	)
}

func stableCollisionComponentID(prefix, value string) string {
	return prefix + ":" + digestParts(prefix, value)[:16]
}

func digestParts(parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, strings.TrimSpace(part))
	}
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x00")))
	return hex.EncodeToString(sum[:])
}

func validCollisionScore(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func roundCollisionScore(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func compactEvidenceRefs(refs []contextengine.EvidenceRef) []contextengine.EvidenceRef {
	out := make([]contextengine.EvidenceRef, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		key := contextengine.FormatEvidenceRef(ref)
		if strings.TrimSpace(key) == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}
