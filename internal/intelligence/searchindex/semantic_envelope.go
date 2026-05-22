package searchindex

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embeddingtext"
)

const (
	metadataKeySemanticEnvelope  = "semantic_envelope"
	metadataKeyCoChangeNeighbors = "cochange_neighbors"
)

// CodeEnvelopeProvider supplies deterministic semantic context for code
// documents without coupling searchindex to repoindex, git, ContextWiki, or memorycore.
type CodeEnvelopeProvider interface {
	BuildCodeEnvelope(ctx context.Context, req CodeEnvelopeRequest) (SemanticEnvelopeBits, error)
}

type CodeEnvelopeRequest struct {
	Document Document

	// IncludeCoChangeNeighborsInEnvelope controls whether co-change neighbors
	// may enter embedding text. Metadata may still carry co-change neighbors.
	IncludeCoChangeNeighborsInEnvelope bool
}

type SemanticEnvelopeBits struct {
	ProviderVersion string
	TextSections    []EnvelopeSection
	Keywords        []string
	Metadata        map[string]any
	DigestParts     []string

	// CoChangeNeighbors are inspect-only metadata by default. They enter
	// embedding text only when IncludeCoChangeNeighborsInEnvelope is true.
	CoChangeNeighbors []string
}

type EnvelopeSection struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type SemanticEnvelopeMetadata struct {
	Digest               string            `json:"digest"`
	ProviderVersion      string            `json:"provider_version"`
	IncludeCoChangeText  bool              `json:"include_cochange_text"`
	CoChangeMetadataOnly bool              `json:"cochange_metadata_only"`
	TextSections         []EnvelopeSection `json:"text_sections,omitempty"`
	Metadata             map[string]any    `json:"metadata,omitempty"`
}

// [[protocol:semantic-envelope-retrieval-evidence]]
// [[doc:docs/plans/features/semantic-code-anchors.md#Remaining Work]]
// [[test:internal/intelligence/searchindex/build_code_test.go#TestBuildCodeDocumentsAppliesSemanticEnvelopeProvider]]
func applySemanticEnvelope(doc Document, bits SemanticEnvelopeBits, opts BuildCodeOptions) Document {
	bits = normalizeSemanticEnvelopeBits(bits)
	if isEmptySemanticEnvelope(bits) {
		return doc
	}
	doc.Keywords = append(doc.Keywords, bits.Keywords...)
	if doc.Metadata == nil {
		doc.Metadata = map[string]any{}
	}
	digest := buildSemanticEnvelopeDigest(bits, opts.IncludeCoChangeNeighborsInEnvelope)
	envelope := SemanticEnvelopeMetadata{
		Digest:               digest,
		ProviderVersion:      bits.ProviderVersion,
		IncludeCoChangeText:  opts.IncludeCoChangeNeighborsInEnvelope,
		CoChangeMetadataOnly: !opts.IncludeCoChangeNeighborsInEnvelope,
		TextSections:         bits.TextSections,
		Metadata:             bits.Metadata,
	}
	doc.Metadata[metadataKeySemanticEnvelope] = envelope
	if len(bits.CoChangeNeighbors) > 0 {
		doc.Metadata[metadataKeyCoChangeNeighbors] = bits.CoChangeNeighbors
	}
	doc.SearchText = enrichedSearchText(doc.SearchText, bits, opts.IncludeCoChangeNeighborsInEnvelope)
	return doc
}

func normalizeSemanticEnvelopeBits(bits SemanticEnvelopeBits) SemanticEnvelopeBits {
	bits.ProviderVersion = strings.TrimSpace(bits.ProviderVersion)
	bits.TextSections = normalizeEnvelopeSections(bits.TextSections)
	bits.Keywords = normalizeKeywords(bits.Keywords)
	bits.DigestParts = sortDedupTrimmed(bits.DigestParts)
	bits.CoChangeNeighbors = sortDedupTrimmed(bits.CoChangeNeighbors)
	if len(bits.Metadata) == 0 {
		bits.Metadata = nil
	}
	return bits
}

func normalizeEnvelopeSections(sections []EnvelopeSection) []EnvelopeSection {
	if len(sections) == 0 {
		return nil
	}
	out := make([]EnvelopeSection, 0, len(sections))
	for _, section := range sections {
		name := strings.TrimSpace(section.Name)
		text := strings.TrimSpace(section.Text)
		if name == "" || text == "" {
			continue
		}
		out = append(out, EnvelopeSection{Name: name, Text: text})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Text < out[j].Text
	})
	return out
}

func isEmptySemanticEnvelope(bits SemanticEnvelopeBits) bool {
	return bits.ProviderVersion == "" &&
		len(bits.TextSections) == 0 &&
		len(bits.Keywords) == 0 &&
		len(bits.Metadata) == 0 &&
		len(bits.DigestParts) == 0 &&
		len(bits.CoChangeNeighbors) == 0
}

func buildSemanticEnvelopeDigest(bits SemanticEnvelopeBits, includeCoChangeText bool) string {
	sections := make([]embeddingtext.SemanticEnvelopeDigestSection, 0, len(bits.TextSections))
	for _, section := range bits.TextSections {
		sections = append(sections, embeddingtext.SemanticEnvelopeDigestSection{Name: section.Name, Text: section.Text})
	}
	return embeddingtext.BuildSemanticEnvelopeContentDigest(embeddingtext.SemanticEnvelopeDigestInput{
		ProviderVersion:                bits.ProviderVersion,
		IncludeCoChangeNeighborsInText: includeCoChangeText,
		TextSections:                   sections,
		Keywords:                       bits.Keywords,
		DigestParts:                    bits.DigestParts,
		CoChangeNeighborPaths:          bits.CoChangeNeighbors,
	})
}

func enrichedSearchText(base string, bits SemanticEnvelopeBits, includeCoChangeText bool) string {
	parts := []string{base}
	for _, section := range bits.TextSections {
		parts = append(parts, section.Name, section.Text)
	}
	if len(bits.Keywords) > 0 {
		parts = append(parts, strings.Join(bits.Keywords, " "))
	}
	if includeCoChangeText && len(bits.CoChangeNeighbors) > 0 {
		parts = append(parts, "cochange", strings.Join(bits.CoChangeNeighbors, " "))
	}
	return encodeSearchText(parts...)
}

func envelopeTextSectionsFromMetadata(metadata map[string]any) []EnvelopeSection {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata[metadataKeySemanticEnvelope]
	if !ok {
		return nil
	}
	envelope, ok := semanticEnvelopeMetadataFromValue(raw)
	if !ok {
		return nil
	}
	return normalizeEnvelopeSections(envelope.TextSections)
}

func coChangeTextFromMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	envelope, ok := semanticEnvelopeMetadataFromValue(metadata[metadataKeySemanticEnvelope])
	if !ok || !envelope.IncludeCoChangeText {
		return ""
	}
	return strings.Join(stringsFromMetadataValue(metadata[metadataKeyCoChangeNeighbors]), ", ")
}

func semanticEnvelopeMetadataFromValue(raw any) (SemanticEnvelopeMetadata, bool) {
	switch value := raw.(type) {
	case SemanticEnvelopeMetadata:
		return value, true
	case *SemanticEnvelopeMetadata:
		if value == nil {
			return SemanticEnvelopeMetadata{}, false
		}
		return *value, true
	default:
		body, err := json.Marshal(raw)
		if err != nil {
			return SemanticEnvelopeMetadata{}, false
		}
		var envelope SemanticEnvelopeMetadata
		if err := json.Unmarshal(body, &envelope); err != nil {
			return SemanticEnvelopeMetadata{}, false
		}
		return envelope, true
	}
}

func stringsFromMetadataValue(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return sortDedupTrimmed(values)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			out = append(out, fmt.Sprint(value))
		}
		return sortDedupTrimmed(out)
	default:
		return nil
	}
}

func sortDedupTrimmed(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
