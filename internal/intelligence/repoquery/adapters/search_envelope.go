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
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
)

type SemanticAnchorEnvelopeProvider struct {
	Store *repoindex.Store

	once sync.Once
	err  error

	symbolsByFileName map[string][]repoindex.Node
	symbolsByID       map[string][]repoindex.Node
	symbolsByScopedID map[string][]repoindex.Node
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

	anchors := make([]searchindex.SemanticEnvelopeAnchorMetadata, 0, len(projection.Edges))
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
		keywords = append(keywords, anchor.TargetDisplay, anchor.TargetID, anchor.TargetType, anchor.Relation)
		digestParts = append(digestParts, strings.Join([]string{
			anchor.TargetID,
			anchor.Relation,
			anchor.TargetType,
			anchor.ValidationStatus,
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
		Metadata: searchindex.SemanticEnvelopeProviderMetadata{
			OwnerNodeID:  owner.ID,
			Anchors:      anchors,
			WarningCount: len(projection.Warnings),
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
		p.symbolsByID = make(map[string][]repoindex.Node, len(symbols))
		p.symbolsByScopedID = make(map[string][]repoindex.Node, len(symbols))
		p.filesByPath = make(map[string]repoindex.Node, len(files))
		for _, node := range symbols {
			key := semanticEnvelopeSymbolKey(node.File, node.Name)
			p.symbolsByFileName[key] = append(p.symbolsByFileName[key], node)
			if id := strings.TrimSpace(node.ID); id != "" {
				p.symbolsByID[id] = append(p.symbolsByID[id], node)
			}
			if scoped := repoIndexNodeScopedSymbolID(node); scoped != "" {
				p.symbolsByScopedID[scoped] = append(p.symbolsByScopedID[scoped], node)
			}
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
		if node, ok := firstNodeBySortedID(p.symbolsByID[doc.SymbolID]); ok {
			return node, true
		}
		if node, ok := firstNodeBySortedID(p.symbolsByScopedID[doc.SymbolID]); ok {
			return node, true
		}
		candidates := p.symbolsByFileName[semanticEnvelopeSymbolKey(doc.Path, doc.SymbolName)]
		if len(candidates) == 0 && doc.SymbolName != doc.Title {
			candidates = p.symbolsByFileName[semanticEnvelopeSymbolKey(doc.Path, doc.Title)]
		}
		return firstNodeBySortedID(candidates)
	case searchindex.KindFile:
		node, ok := p.filesByPath[doc.Path]
		return node, ok
	default:
		return repoindex.Node{}, false
	}
}

func firstNodeBySortedID(candidates []repoindex.Node) (repoindex.Node, bool) {
	if len(candidates) == 0 {
		return repoindex.Node{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	return candidates[0], true
}

func semanticEnvelopeAnchorFromMeta(meta semanticanchors.SemanticAnchorEdgeMeta) searchindex.SemanticEnvelopeAnchorMetadata {
	return searchindex.SemanticEnvelopeAnchorMetadata{
		Relation:         string(meta.Relation),
		TargetID:         string(meta.TargetID),
		TargetDisplay:    meta.TargetDisplay,
		TargetType:       meta.TargetType,
		ValidationStatus: string(meta.ValidationStatus),
		Path:             meta.Path,
	}
}

func anchorEmbeddingText(anchor searchindex.SemanticEnvelopeAnchorMetadata) string {
	target := anchor.TargetDisplay
	if target == "" {
		target = anchor.TargetID
	}
	return fmt.Sprintf("%s %s %s %s", anchor.Relation, anchor.TargetType, target, anchor.ValidationStatus)
}

func semanticEnvelopeSymbolKey(path, name string) string {
	return strings.TrimSpace(path) + "\x00" + strings.TrimSpace(name)
}

func repoIndexNodeScopedSymbolID(node repoindex.Node) string {
	_, raw := repoindex.SplitNamespacedID(node.ID)
	prefix := "sym:" + node.Pkg + ":"
	if !strings.HasPrefix(raw, prefix) {
		return ""
	}
	return symbolutil.ScopedSymbolID(node.Pkg, strings.TrimPrefix(raw, prefix))
}
