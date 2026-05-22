package contextplane

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

const memoryBlurTag = "blurred"

// MemoryBlurInput is the first-class forgetting contract. Callers provide
// already-typed structural signals; this function deliberately does not infer
// behavior from raw text.
type MemoryBlurInput struct {
	ID             string                      `json:"id"`
	WorkspaceID    string                      `json:"workspace_id,omitempty"`
	OriginalDomain string                      `json:"original_domain"`
	Summary        string                      `json:"summary"`
	LiteralText    string                      `json:"literal_text,omitempty"`
	Shape          MemoryStructuralShape       `json:"shape"`
	SourceRefs     []contextengine.EvidenceRef `json:"source_refs,omitempty"`
	MechanismTags  []string                    `json:"mechanism_tags,omitempty"`
	Tags           []string                    `json:"tags,omitempty"`
}

// MemoryStructuralShape is the domain-stripped mechanism retained after
// forgetting noisy details.
type MemoryStructuralShape struct {
	Schema      string            `json:"schema,omitempty"`
	Mechanism   string            `json:"mechanism,omitempty"`
	Actors      []string          `json:"actors,omitempty"`
	Operations  []string          `json:"operations,omitempty"`
	Flows       []string          `json:"flows,omitempty"`
	Constraints []string          `json:"constraints,omitempty"`
	Signals     []string          `json:"signals,omitempty"`
	Graph       *MemoryGraphShape `json:"graph,omitempty"`
}

// MemoryGraphShape captures repo graph topology without symbol names, file
// names, package names, or other literal domain identifiers.
type MemoryGraphShape struct {
	NodeKind    string         `json:"node_kind,omitempty"`
	Outgoing    map[string]int `json:"outgoing,omitempty"`
	Incoming    map[string]int `json:"incoming,omitempty"`
	SpanLines   int            `json:"span_lines,omitempty"`
	NeighborMix int            `json:"neighbor_mix,omitempty"`
}

// BlurMemoryProjection converts a literal memory plus typed structural signals
// into a mechanism projection. Literal text is kept for provenance, but the
// AbstractSchema is built only from Shape.
func BlurMemoryProjection(input MemoryBlurInput) (MechanismProjection, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.OriginalDomain = strings.TrimSpace(input.OriginalDomain)
	input.Summary = strings.TrimSpace(input.Summary)
	input.LiteralText = strings.TrimSpace(input.LiteralText)
	input.SourceRefs = compactEvidenceRefs(input.SourceRefs)
	input.MechanismTags = normalizeMechanismTags(input.MechanismTags)
	input.Tags = compactStrings(append(input.Tags, memoryBlurTag))
	if input.ID == "" {
		return MechanismProjection{}, fmt.Errorf("blur memory: id is required")
	}
	if input.OriginalDomain == "" {
		return MechanismProjection{}, fmt.Errorf("blur memory: original_domain is required")
	}
	if input.Summary == "" {
		return MechanismProjection{}, fmt.Errorf("blur memory: summary is required")
	}
	if input.LiteralText == "" {
		input.LiteralText = input.Summary
	}
	if len(input.SourceRefs) == 0 {
		return MechanismProjection{}, fmt.Errorf("blur memory: source_refs are required")
	}
	abstractSchema := BuildBlurredMechanismSchema(input.Shape)
	if abstractSchema == "" {
		return MechanismProjection{}, fmt.Errorf("blur memory: structural shape is required")
	}
	return MechanismProjection{
		ID:             input.ID,
		WorkspaceID:    input.WorkspaceID,
		OriginalDomain: input.OriginalDomain,
		Summary:        input.Summary,
		LiteralText:    input.LiteralText,
		AbstractSchema: abstractSchema,
		MechanismTags:  input.MechanismTags,
		SourceRefs:     input.SourceRefs,
		Tags:           input.Tags,
	}, nil
}

// BuildBlurredMechanismSchema renders structural shape fields in stable order.
func BuildBlurredMechanismSchema(shape MemoryStructuralShape) string {
	var b strings.Builder
	if schema := strings.TrimSpace(shape.Schema); schema != "" {
		b.WriteString("mechanism schema:\n")
		b.WriteString(schema)
		b.WriteString("\n")
	}
	writeBlurredLine(&b, "mechanism", shape.Mechanism)
	writeBlurredList(&b, "actors", shape.Actors)
	writeBlurredList(&b, "operations", shape.Operations)
	writeBlurredList(&b, "flows", shape.Flows)
	writeBlurredList(&b, "constraints", shape.Constraints)
	writeBlurredList(&b, "signals", shape.Signals)
	if shape.Graph != nil {
		writeGraphShape(&b, *shape.Graph)
	}
	return strings.TrimSpace(b.String())
}

// GraphShapeVector returns a fixed-width deterministic vector for structural
// matching. Counts are log-scaled so high-fanout symbols do not dominate purely
// by size.
func GraphShapeVector(shape MemoryGraphShape, edgeOrder []string) []float32 {
	edgeOrder = compactStringsInOrder(edgeOrder)
	if len(edgeOrder) == 0 {
		edgeOrder = defaultMemoryGraphEdgeOrder()
	}
	outTotal := sumGraphCounts(shape.Outgoing)
	inTotal := sumGraphCounts(shape.Incoming)
	total := outTotal + inTotal
	vector := make([]float32, 0, len(edgeOrder)*2+5)
	for _, edgeType := range edgeOrder {
		vector = append(vector, logScaledCount(shape.Outgoing[edgeType]))
	}
	for _, edgeType := range edgeOrder {
		vector = append(vector, logScaledCount(shape.Incoming[edgeType]))
	}
	vector = append(
		vector,
		logScaledCount(outTotal),
		logScaledCount(inTotal),
		logScaledCount(total),
		ratioFeature(outTotal-inTotal, max(total, 1)),
		logScaledCount(shape.NeighborMix),
	)
	return vector
}

func writeBlurredLine(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\n")
}

func writeBlurredList(b *strings.Builder, label string, values []string) {
	values = compactStrings(values)
	if len(values) == 0 {
		return
	}
	sort.Strings(values)
	b.WriteString(label)
	b.WriteString(":\n")
	for _, value := range values {
		b.WriteString("- ")
		b.WriteString(value)
		b.WriteString("\n")
	}
}

func writeGraphShape(b *strings.Builder, shape MemoryGraphShape) {
	b.WriteString("graph shape:\n")
	writeBlurredLine(b, "- node_kind", shape.NodeKind)
	writeGraphCounts(b, "- outgoing", shape.Outgoing)
	writeGraphCounts(b, "- incoming", shape.Incoming)
	if shape.SpanLines > 0 {
		writeBlurredLine(b, "- span_lines", fmt.Sprintf("%d", shape.SpanLines))
	}
	if shape.NeighborMix > 0 {
		writeBlurredLine(b, "- neighbor_mix", fmt.Sprintf("%d", shape.NeighborMix))
	}
}

func writeGraphCounts(b *strings.Builder, label string, values map[string]int) {
	keys := make([]string, 0, len(values))
	for key, count := range values {
		key = strings.TrimSpace(key)
		if key == "" || count <= 0 {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return
	}
	sort.Strings(keys)
	b.WriteString(label)
	b.WriteString(":\n")
	for _, key := range keys {
		b.WriteString("  - ")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(fmt.Sprintf("%d", values[key]))
		b.WriteString("\n")
	}
}

func sumGraphCounts(values map[string]int) int {
	var total int
	for _, count := range values {
		if count > 0 {
			total += count
		}
	}
	return total
}

func logScaledCount(value int) float32 {
	if value <= 0 {
		return 0
	}
	return float32(math.Log1p(float64(value)))
}

func ratioFeature(numerator, denominator int) float32 {
	if denominator <= 0 {
		return 0
	}
	return float32(numerator) / float32(denominator)
}

func defaultMemoryGraphEdgeOrder() []string {
	return []string{
		"CONTAINS",
		"IMPORTS",
		"USES_SYMBOL",
		"REFERS_TO",
		"CALLS",
		"IMPLEMENTS",
		"EMBEDS",
		"TESTS",
	}
}

func compactStringsInOrder(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
