package adapters

import (
	"fmt"

	"github.com/jkatigb/agentctl/internal/intelligence/codecontext"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
	"github.com/jkatigb/agentctl/internal/searchindex"
)

// ToSearchHits converts repoquery anchors into searchindex-style hits for retrieval fusion.
func ToSearchHits(anchors []repoquery.Anchor) []searchindex.SearchHit {
	out := make([]searchindex.SearchHit, 0, len(anchors))
	for _, a := range anchors {
		id := dedupID(a)
		if id == "" || a.Path == "" {
			continue
		}
		doc := searchindex.Document{
			ID:          id,
			WorkspaceID: "",
			Path:        a.Path,
			GroupKey:    a.Path,
			SymbolID:    a.SymbolID,
			SymbolName:  a.SymbolName,
			Title:       firstNonEmpty(a.SymbolName, a.Path),
			Summary:     a.Summary,
			Anchor: searchindex.Anchor{
				Type:      anchorType(a),
				Path:      a.Path,
				Line:      a.LineHint,
				StartLine: a.LineHint,
				EndLine:   a.LineHint,
			},
		}
		out = append(out, searchindex.SearchHit{Doc: doc, Score: a.Score})
	}
	return out
}

// ToCodeContextCandidates converts repoquery anchors into codecontext candidates.
func ToCodeContextCandidates(anchors []repoquery.Anchor) []codecontext.Candidate {
	out := make([]codecontext.Candidate, 0, len(anchors))
	for _, a := range anchors {
		if a.Path == "" {
			continue
		}
		candidate := codecontext.Candidate{
			Path:     a.Path,
			SymbolID: a.SymbolID,
			LineHint: a.LineHint,
			Priority: a.Score,
			Summary:  a.Summary,
			Anchors: []codecontext.Anchor{
				{
					Kind:       codeAnchorKind(a),
					SymbolID:   a.SymbolID,
					SymbolName: a.SymbolName,
					Line:       a.LineHint,
					StartLine:  a.LineHint,
					EndLine:    a.LineHint,
					Score:      a.Score,
					Source:     a.Source,
					Reason:     "repoquery",
				},
			},
		}
		out = append(out, candidate)
	}
	return out
}

func dedupID(a repoquery.Anchor) string {
	if a.SymbolID != "" {
		return "repoquery:sym:" + a.SymbolID
	}
	if a.Path != "" {
		return fmt.Sprintf("repoquery:file:%s#L%d", a.Path, a.LineHint)
	}
	return ""
}

func anchorType(a repoquery.Anchor) searchindex.AnchorType {
	if a.SymbolID != "" {
		return searchindex.AnchorSymbol
	}
	return searchindex.AnchorLine
}

func codeAnchorKind(a repoquery.Anchor) codecontext.AnchorKind {
	if a.SymbolID != "" {
		return codecontext.AnchorSymbol
	}
	if a.LineHint > 0 {
		return codecontext.AnchorLine
	}
	return codecontext.AnchorFile
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
