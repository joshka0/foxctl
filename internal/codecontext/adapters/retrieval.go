package adapters

import (
	"strings"

	"github.com/jkatigb/agentctl/internal/codecontext"
	retrievalv2 "github.com/jkatigb/agentctl/internal/retrieval/v2"
	"github.com/jkatigb/agentctl/internal/searchindex"
)

// SearchHitToCandidate converts a retrieval search hit into a codecontext candidate.
//
// The mapping is intentionally narrow:
// - Path comes from the hit document path, with fallback to the anchor path.
// - SymbolID is preserved when present.
// - LineHint uses anchor line metadata when available.
// - Priority mirrors the hit score, clamped to [0,1].
func SearchHitToCandidate(hit searchindex.SearchHit) (candidate codecontext.Candidate) {
	path := strings.TrimSpace(hit.Doc.Path)
	if path == "" {
		path = strings.TrimSpace(hit.Doc.Anchor.Path)
	}
	if path == "" {
		return codecontext.Candidate{}
	}

	candidate.Path = path
	candidate.SymbolID = strings.TrimSpace(hit.Doc.SymbolID)
	candidate.Priority = clampPriority(hit.Score)
	candidate.Summary = strings.TrimSpace(hit.Doc.Summary)

	if lineHint := bestLineFromAnchor(hit.Doc.Anchor); lineHint > 0 {
		candidate.LineHint = lineHint
	}
	candidate.Anchors = anchorsFromDocument(hit.Doc, candidate.Priority)

	return candidate
}

// SearchHitsToCandidates maps retrieval hits to ordered codecontext candidates.
//
// This is a minimal bridge used by codecontext callers that start from ranked hits
// and need a normalized candidate slice for collection.
func SearchHitsToCandidates(hits []searchindex.SearchHit) []codecontext.Candidate {
	out := make([]codecontext.Candidate, 0, len(hits))

	for _, hit := range hits {
		candidate := SearchHitToCandidate(hit)
		if candidate.Path == "" {
			continue
		}
		out = append(out, candidate)
	}

	return out
}

// GroupsToCandidates converts grouped retrieval-v2 file hits into codecontext candidates.
func GroupsToCandidates(groups []retrievalv2.Group) []codecontext.Candidate {
	out := make([]codecontext.Candidate, 0, len(groups))
	for _, group := range groups {
		path := strings.TrimSpace(group.Path)
		if path == "" {
			continue
		}
		candidate := codecontext.Candidate{
			Path:     path,
			Priority: clampPriority(group.Score),
			Summary:  strings.TrimSpace(group.Summary),
		}
		if len(group.Anchors) > 0 {
			candidate.SymbolID = strings.TrimSpace(group.Anchors[0].SymbolID)
			candidate.Anchors = make([]codecontext.Anchor, 0, len(group.Anchors))
			for _, anchor := range group.Anchors {
				candidate.Anchors = append(candidate.Anchors, codecontext.Anchor{
					Kind:       mapAnchorKind(anchor.Anchor.Type),
					SymbolID:   strings.TrimSpace(anchor.SymbolID),
					SymbolName: strings.TrimSpace(anchor.SymbolName),
					Line:       bestLineFromAnchor(anchor.Anchor),
					StartLine:  anchor.Anchor.StartLine,
					EndLine:    anchor.Anchor.EndLine,
					Score:      clampPriority(anchor.Score),
					Source:     string(anchor.Source),
					Reason:     "group_hit",
				})
			}
		}
		out = append(out, candidate)
	}
	return out
}

func clampPriority(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func bestLineFromAnchor(anchor searchindex.Anchor) int {
	if anchor.Line > 0 {
		return anchor.Line
	}
	if anchor.StartLine > 0 {
		return anchor.StartLine
	}
	if anchor.EndLine > 0 {
		return anchor.EndLine
	}
	return 0
}

func anchorsFromDocument(doc searchindex.Document, score float64) []codecontext.Anchor {
	var anchors []codecontext.Anchor

	if doc.SymbolID != "" || doc.Anchor.Type == searchindex.AnchorSymbol {
		anchors = append(anchors, codecontext.Anchor{
			Kind:       codecontext.AnchorSymbol,
			SymbolID:   strings.TrimSpace(doc.SymbolID),
			SymbolName: strings.TrimSpace(doc.SymbolName),
			StartLine:  doc.Anchor.StartLine,
			EndLine:    doc.Anchor.EndLine,
			Score:      score,
			Reason:     "search_hit",
			Source:     "searchindex",
		})
	}

	if line := bestLineFromAnchor(doc.Anchor); line > 0 {
		anchors = append(anchors, codecontext.Anchor{
			Kind:      codecontext.AnchorLine,
			Line:      line,
			StartLine: doc.Anchor.StartLine,
			EndLine:   doc.Anchor.EndLine,
			Score:     score,
			Reason:    "search_hit",
			Source:    "searchindex",
		})
	}

	if len(anchors) == 0 {
		anchors = append(anchors, codecontext.Anchor{
			Kind:   codecontext.AnchorFile,
			Score:  score,
			Reason: "search_hit",
			Source: "searchindex",
		})
	}

	return anchors
}

func mapAnchorKind(k searchindex.AnchorType) codecontext.AnchorKind {
	switch k {
	case searchindex.AnchorSymbol:
		return codecontext.AnchorSymbol
	case searchindex.AnchorLine:
		return codecontext.AnchorLine
	default:
		return codecontext.AnchorFile
	}
}
