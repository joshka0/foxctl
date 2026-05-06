package adapters

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semanticanchors"
	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
)

type SemanticAnchorEnvelopeProvider struct {
	Store *repoindex.Store

	once sync.Once
	err  error

	symbolsByFileName map[string][]repoindex.Node
	filesByPath       map[string]repoindex.Node
}

var _ searchindex.CodeEnvelopeProvider = (*SemanticAnchorEnvelopeProvider)(nil)

func (p *SemanticAnchorEnvelopeProvider) BuildCodeEnvelope(ctx context.Context, req searchindex.CodeEnvelopeRequest) (searchindex.SemanticEnvelopeBits, error) {
	if p == nil || p.Store == nil {
		return searchindex.SemanticEnvelopeBits{}, nil
	}
	if err := p.load(ctx); err != nil {
		return searchindex.SemanticEnvelopeBits{}, err
	}
	owner, ok := p.ownerForDocument(req.Document)
	if !ok {
		return searchindex.SemanticEnvelopeBits{}, nil
	}
	edges, err := p.Store.GetOutgoingEdges(ctx, owner.ID, repoindex.EdgeSetSemanticAnchors, 32)
	if err != nil {
		return searchindex.SemanticEnvelopeBits{}, err
	}
	projection := repoindex.ProjectSemanticAnchorEdges(edges)
	if len(projection.Edges) == 0 {
		return searchindex.SemanticEnvelopeBits{}, nil
	}

	anchors := make([]semanticEnvelopeAnchor, 0, len(projection.Edges))
	sections := make([]searchindex.EnvelopeSection, 0, len(projection.Edges))
	var keywords []string
	var digestParts []string
	for _, edge := range projection.Edges {
		meta, present, err := repoindex.DecodeAndValidateSemanticAnchorEdge(edge)
		if err != nil || !present {
			continue
		}
		anchor := semanticEnvelopeAnchorFromMeta(meta)
		anchors = append(anchors, anchor)
		sections = append(sections, searchindex.EnvelopeSection{
			Name: "semantic_anchor",
			Text: anchorEmbeddingText(anchor),
		})
		keywords = append(keywords, anchor.TargetDisplay, string(anchor.TargetID), anchor.TargetType, string(anchor.Relation))
		digestParts = append(digestParts, strings.Join([]string{
			string(anchor.TargetID),
			string(anchor.Relation),
			anchor.TargetType,
			string(anchor.ValidationStatus),
		}, "|"))
	}
	if len(anchors) == 0 {
		return searchindex.SemanticEnvelopeBits{}, nil
	}
	sort.SliceStable(anchors, func(i, j int) bool {
		if anchors[i].TargetID != anchors[j].TargetID {
			return anchors[i].TargetID < anchors[j].TargetID
		}
		return anchors[i].Relation < anchors[j].Relation
	})
	return searchindex.SemanticEnvelopeBits{
		ProviderVersion: "repoindex-semantic-anchors-v1",
		TextSections:    sections,
		Keywords:        keywords,
		Metadata: map[string]any{
			"owner_node_id": owner.ID,
			"anchors":       anchors,
			"warning_count": len(projection.Warnings),
		},
		DigestParts: digestParts,
	}, nil
}

func (p *SemanticAnchorEnvelopeProvider) load(ctx context.Context) error {
	p.once.Do(func() {
		symbols, err := p.Store.ListNodesByKind(ctx, repoindex.NodeSymbol, 100000)
		if err != nil {
			p.err = err
			return
		}
		files, err := p.Store.ListNodesByKind(ctx, repoindex.NodeFile, 100000)
		if err != nil {
			p.err = err
			return
		}
		p.symbolsByFileName = make(map[string][]repoindex.Node, len(symbols))
		p.filesByPath = make(map[string]repoindex.Node, len(files))
		for _, node := range symbols {
			key := semanticEnvelopeSymbolKey(node.File, node.Name)
			p.symbolsByFileName[key] = append(p.symbolsByFileName[key], node)
		}
		for _, node := range files {
			p.filesByPath[node.File] = node
		}
	})
	return p.err
}

func (p *SemanticAnchorEnvelopeProvider) ownerForDocument(doc searchindex.Document) (repoindex.Node, bool) {
	switch doc.Kind {
	case searchindex.KindSymbol:
		candidates := p.symbolsByFileName[semanticEnvelopeSymbolKey(doc.Path, doc.SymbolName)]
		if len(candidates) == 0 && doc.SymbolName != doc.Title {
			candidates = p.symbolsByFileName[semanticEnvelopeSymbolKey(doc.Path, doc.Title)]
		}
		if len(candidates) == 0 {
			return repoindex.Node{}, false
		}
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
		return candidates[0], true
	case searchindex.KindFile:
		node, ok := p.filesByPath[doc.Path]
		return node, ok
	default:
		return repoindex.Node{}, false
	}
}

type semanticEnvelopeAnchor struct {
	Relation         semanticanchors.SemanticAnchorRelation `json:"relation"`
	TargetID         semanticanchors.AnchorTargetID         `json:"target_id"`
	TargetDisplay    string                                 `json:"target_display,omitempty"`
	TargetType       string                                 `json:"target_type,omitempty"`
	ValidationStatus semanticanchors.AnchorValidationStatus `json:"validation_status"`
	Path             string                                 `json:"path,omitempty"`
}

func semanticEnvelopeAnchorFromMeta(meta semanticanchors.SemanticAnchorEdgeMeta) semanticEnvelopeAnchor {
	return semanticEnvelopeAnchor{
		Relation:         meta.Relation,
		TargetID:         meta.TargetID,
		TargetDisplay:    meta.TargetDisplay,
		TargetType:       meta.TargetType,
		ValidationStatus: meta.ValidationStatus,
		Path:             meta.Path,
	}
}

func anchorEmbeddingText(anchor semanticEnvelopeAnchor) string {
	target := anchor.TargetDisplay
	if target == "" {
		target = string(anchor.TargetID)
	}
	return fmt.Sprintf("%s %s %s %s", anchor.Relation, anchor.TargetType, target, anchor.ValidationStatus)
}

func semanticEnvelopeSymbolKey(path, name string) string {
	return strings.TrimSpace(path) + "\x00" + strings.TrimSpace(name)
}
