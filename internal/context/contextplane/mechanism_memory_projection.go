package contextplane

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/storage"
)

const (
	MechanismMemoryLiteralType    = "mechanism_memory_literal"
	MechanismMemoryStructuralType = "mechanism_memory_structural"

	mechanismMemoryNamePrefix = "mechanism-memory://"
)

// MechanismMemoryView identifies which embedding view a named-memory artifact
// represents.
type MechanismMemoryView string

const (
	MechanismMemoryViewLiteral    MechanismMemoryView = "literal"
	MechanismMemoryViewStructural MechanismMemoryView = "structural"
)

// MechanismProjection is the lossy structural projection produced by a
// compactor, plus the literal text needed to keep provenance anchored.
type MechanismProjection struct {
	ID             string
	WorkspaceID    string
	OriginalDomain string
	Summary        string
	LiteralText    string
	AbstractSchema string
	MechanismTags  []string
	SourceRefs     []contextengine.EvidenceRef
	Tags           []string
}

// MechanismMemoryArtifact is a named-memory write descriptor. Callers persist
// Result with SaveFromResult and embed EmbeddingText into the saved row.
type MechanismMemoryArtifact struct {
	Name          string              `json:"name"`
	Type          string              `json:"type"`
	Summary       string              `json:"summary"`
	Result        []byte              `json:"result"`
	EmbeddingText string              `json:"embedding_text"`
	View          MechanismMemoryView `json:"view"`
}

type mechanismMemoryPayload struct {
	ID             string              `json:"id"`
	WorkspaceID    string              `json:"workspace_id,omitempty"`
	OriginalDomain string              `json:"original_domain"`
	Summary        string              `json:"summary"`
	LiteralText    string              `json:"literal_text,omitempty"`
	AbstractSchema string              `json:"abstract_schema"`
	MechanismTags  []string            `json:"mechanism_tags,omitempty"`
	View           MechanismMemoryView `json:"view"`
	SourceRefs     []string            `json:"source_refs,omitempty"`
	Tags           []string            `json:"tags,omitempty"`
}

// PlanMechanismMemoryArtifacts creates the two named-memory artifacts required
// for dual-view retrieval. It is pure and does not write to memory storage.
func PlanMechanismMemoryArtifacts(projection MechanismProjection) ([]MechanismMemoryArtifact, error) {
	normalized, err := normalizeMechanismProjection(projection)
	if err != nil {
		return nil, err
	}
	literal, err := buildMechanismMemoryArtifact(normalized, MechanismMemoryViewLiteral)
	if err != nil {
		return nil, err
	}
	structural, err := buildMechanismMemoryArtifact(normalized, MechanismMemoryViewStructural)
	if err != nil {
		return nil, err
	}
	return []MechanismMemoryArtifact{literal, structural}, nil
}

// DecodeMechanismMemoryArtifact decodes one named-memory artifact produced by
// PlanMechanismMemoryArtifacts.
func DecodeMechanismMemoryArtifact(entry storage.NamedEntry) (MechanismProjection, MechanismMemoryView, bool) {
	view, ok := mechanismMemoryViewForType(entry.Type)
	if !ok || len(entry.Result) == 0 {
		return MechanismProjection{}, "", false
	}
	var payload mechanismMemoryPayload
	if err := json.Unmarshal(entry.Result, &payload); err != nil {
		return MechanismProjection{}, "", false
	}
	if payload.View != view {
		return MechanismProjection{}, "", false
	}
	projection := MechanismProjection{
		ID:             strings.TrimSpace(payload.ID),
		WorkspaceID:    strings.TrimSpace(payload.WorkspaceID),
		OriginalDomain: strings.TrimSpace(payload.OriginalDomain),
		Summary:        strings.TrimSpace(payload.Summary),
		LiteralText:    strings.TrimSpace(payload.LiteralText),
		AbstractSchema: strings.TrimSpace(payload.AbstractSchema),
		MechanismTags:  compactStrings(payload.MechanismTags),
		SourceRefs:     compactEvidenceRefs(StringsToEvidenceRefs(payload.SourceRefs)),
		Tags:           compactStrings(payload.Tags),
	}
	if len(projection.MechanismTags) == 0 {
		projection.MechanismTags = compactStrings(projection.Tags)
	}
	if projection.ID == "" || projection.OriginalDomain == "" || projection.AbstractSchema == "" {
		return MechanismProjection{}, "", false
	}
	if projection.Summary == "" {
		projection.Summary = strings.TrimSpace(entry.Summary)
	}
	if projection.LiteralText == "" {
		projection.LiteralText = projection.Summary
	}
	return projection, view, true
}

// MechanismMemoryFromArtifacts joins a literal artifact, a structural artifact,
// and their embeddings into the planner-ready MechanismMemory shape.
func MechanismMemoryFromArtifacts(literalEntry, structuralEntry storage.NamedEntry, literalVector, structuralVector []float32) (MechanismMemory, bool) {
	literal, literalView, ok := DecodeMechanismMemoryArtifact(literalEntry)
	if !ok || literalView != MechanismMemoryViewLiteral {
		return MechanismMemory{}, false
	}
	structural, structuralView, ok := DecodeMechanismMemoryArtifact(structuralEntry)
	if !ok || structuralView != MechanismMemoryViewStructural {
		return MechanismMemory{}, false
	}
	if literal.ID != structural.ID || len(literalVector) == 0 || len(structuralVector) == 0 {
		return MechanismMemory{}, false
	}
	return MechanismMemory{
		ID:               literal.ID,
		OriginalDomain:   firstNonEmpty(structural.OriginalDomain, literal.OriginalDomain),
		Summary:          firstNonEmpty(literal.Summary, structural.Summary),
		AbstractSchema:   firstNonEmpty(structural.AbstractSchema, literal.AbstractSchema),
		MechanismTags:    compactStrings(append(append([]string(nil), literal.MechanismTags...), structural.MechanismTags...)),
		LiteralVector:    append([]float32(nil), literalVector...),
		StructuralVector: append([]float32(nil), structuralVector...),
		SourceRefs:       compactEvidenceRefs(append(append([]contextengine.EvidenceRef(nil), literal.SourceRefs...), structural.SourceRefs...)),
	}, true
}

func normalizeMechanismProjection(projection MechanismProjection) (MechanismProjection, error) {
	projection.ID = strings.TrimSpace(projection.ID)
	projection.WorkspaceID = strings.TrimSpace(projection.WorkspaceID)
	projection.OriginalDomain = strings.TrimSpace(projection.OriginalDomain)
	projection.Summary = strings.TrimSpace(projection.Summary)
	projection.LiteralText = strings.TrimSpace(projection.LiteralText)
	projection.AbstractSchema = strings.TrimSpace(projection.AbstractSchema)
	projection.SourceRefs = compactEvidenceRefs(projection.SourceRefs)
	projection.MechanismTags = normalizeMechanismTags(projection.MechanismTags)
	projection.Tags = compactStrings(projection.Tags)
	if projection.ID == "" {
		return MechanismProjection{}, fmt.Errorf("mechanism projection: id is required")
	}
	if projection.OriginalDomain == "" {
		return MechanismProjection{}, fmt.Errorf("mechanism projection: original_domain is required")
	}
	if projection.Summary == "" {
		return MechanismProjection{}, fmt.Errorf("mechanism projection: summary is required")
	}
	if projection.LiteralText == "" {
		projection.LiteralText = projection.Summary
	}
	if projection.AbstractSchema == "" {
		return MechanismProjection{}, fmt.Errorf("mechanism projection: abstract_schema is required")
	}
	if len(projection.SourceRefs) == 0 {
		return MechanismProjection{}, fmt.Errorf("mechanism projection: source_refs are required")
	}
	return projection, nil
}

func buildMechanismMemoryArtifact(projection MechanismProjection, view MechanismMemoryView) (MechanismMemoryArtifact, error) {
	artifactType, ok := mechanismMemoryTypeForView(view)
	if !ok {
		return MechanismMemoryArtifact{}, fmt.Errorf("mechanism projection: unknown view %q", view)
	}
	payload := mechanismMemoryPayload{
		ID:             projection.ID,
		WorkspaceID:    projection.WorkspaceID,
		OriginalDomain: projection.OriginalDomain,
		Summary:        projection.Summary,
		LiteralText:    projection.LiteralText,
		AbstractSchema: projection.AbstractSchema,
		MechanismTags:  projection.MechanismTags,
		View:           view,
		SourceRefs:     EvidenceRefsToStrings(projection.SourceRefs),
		Tags:           projection.Tags,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return MechanismMemoryArtifact{}, fmt.Errorf("marshal mechanism projection: %w", err)
	}
	return MechanismMemoryArtifact{
		Name:          mechanismMemoryName(projection.ID, view),
		Type:          artifactType,
		Summary:       projection.Summary,
		Result:        body,
		EmbeddingText: mechanismMemoryEmbeddingText(projection, view),
		View:          view,
	}, nil
}

func mechanismMemoryEmbeddingText(projection MechanismProjection, view MechanismMemoryView) string {
	switch view {
	case MechanismMemoryViewStructural:
		return strings.TrimSpace("mechanism schema:\n" + projection.AbstractSchema)
	default:
		var b strings.Builder
		b.WriteString("domain: ")
		b.WriteString(projection.OriginalDomain)
		b.WriteString("\nsummary: ")
		b.WriteString(projection.Summary)
		b.WriteString("\nliteral memory:\n")
		b.WriteString(projection.LiteralText)
		return strings.TrimSpace(b.String())
	}
}

func mechanismMemoryName(id string, view MechanismMemoryView) string {
	slug := safeMemorySlug(id)
	if slug == "" {
		slug = "mechanism"
	}
	return mechanismMemoryNamePrefix + slug + "-" + digestParts(id)[:12] + "/" + string(view)
}

func mechanismMemoryTypeForView(view MechanismMemoryView) (string, bool) {
	switch view {
	case MechanismMemoryViewLiteral:
		return MechanismMemoryLiteralType, true
	case MechanismMemoryViewStructural:
		return MechanismMemoryStructuralType, true
	default:
		return "", false
	}
}

func mechanismMemoryViewForType(artifactType string) (MechanismMemoryView, bool) {
	switch strings.TrimSpace(artifactType) {
	case MechanismMemoryLiteralType:
		return MechanismMemoryViewLiteral, true
	case MechanismMemoryStructuralType:
		return MechanismMemoryViewStructural, true
	default:
		return "", false
	}
}
