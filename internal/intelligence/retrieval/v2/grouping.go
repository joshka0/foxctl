package retrievalv2

import (
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
)

// GroupResults groups fused hits into document-oriented groups.
func GroupResults(hits []FusedHit, opts GroupOptions) []Group {
	if !opts.Enabled || len(hits) == 0 {
		return nil
	}

	if opts.MaxGroups <= 0 {
		opts.MaxGroups = 25
	}
	if opts.MaxMembers <= 0 {
		opts.MaxMembers = 5
	}

	groups := map[string]*Group{}

	for _, hit := range hits {
		key := strings.TrimSpace(hit.Document.GroupKey)
		if key == "" {
			key = strings.TrimSpace(hit.Document.Path)
		}
		if key == "" {
			continue
		}

		group, ok := groups[key]
		if !ok {
			group = &Group{Key: key, Path: hit.Document.Path, Kind: string(hit.Document.Kind), Score: 0}
			group.HitCount = 0
			groups[key] = group
		}

		group.HitCount++
		if group.Score < hit.Score {
			group.Score = hit.Score
		}
		if group.Summary == "" && strings.TrimSpace(hit.Document.Summary) != "" {
			group.Summary = hit.Document.Summary
		}
		group.Hits = append(group.Hits, hit)
		for _, src := range hit.Sources {
			group.Sources = appendUniqueSource(group.Sources, src)
		}
		group.Anchors = appendGroupAnchors(group.Anchors, hit)
	}

	groupSlice := make([]Group, 0, len(groups))
	for _, group := range groups {
		sort.SliceStable(group.Hits, func(i, j int) bool {
			if group.Hits[i].Score == group.Hits[j].Score {
				return len(group.Hits[i].Sources) > len(group.Hits[j].Sources)
			}
			return group.Hits[i].Score > group.Hits[j].Score
		})

		if opts.MaxMembers > 0 && len(group.Hits) > opts.MaxMembers {
			group.Hits = group.Hits[:opts.MaxMembers]
		}
		sort.SliceStable(group.Anchors, func(i, j int) bool {
			if group.Anchors[i].Score == group.Anchors[j].Score {
				if group.Anchors[i].Anchor.Path == group.Anchors[j].Anchor.Path {
					return group.Anchors[i].Anchor.StartLine < group.Anchors[j].Anchor.StartLine
				}
				return group.Anchors[i].Anchor.Path < group.Anchors[j].Anchor.Path
			}
			return group.Anchors[i].Score > group.Anchors[j].Score
		})
		sort.SliceStable(group.Sources, func(i, j int) bool {
			return group.Sources[i] < group.Sources[j]
		})

		groupSlice = append(groupSlice, *group)
	}

	sort.SliceStable(groupSlice, func(i, j int) bool {
		if groupSlice[i].Score == groupSlice[j].Score {
			return groupSlice[i].Key < groupSlice[j].Key
		}
		return groupSlice[i].Score > groupSlice[j].Score
	})

	if len(groupSlice) > opts.MaxGroups {
		groupSlice = groupSlice[:opts.MaxGroups]
	}

	return groupSlice
}

func appendUniqueSource(dst []SourceID, source SourceID) []SourceID {
	for _, existing := range dst {
		if existing == source {
			return dst
		}
	}
	return append(dst, source)
}

func appendGroupAnchors(dst []AnchorHit, hit FusedHit) []AnchorHit {
	doc := hit.Document
	anchor := doc.Anchor
	if anchor.Path == "" {
		anchor.Path = doc.Path
	}
	if anchor.Path == "" {
		return dst
	}

	source := SourceLexical
	if len(hit.Sources) > 0 {
		source = hit.Sources[0]
	}

	next := AnchorHit{
		Anchor:     anchor,
		Score:      hit.Score,
		Source:     source,
		SymbolID:   doc.SymbolID,
		SymbolName: doc.SymbolName,
	}

	for i, existing := range dst {
		if sameAnchor(existing.Anchor, next.Anchor) && existing.SymbolID == next.SymbolID {
			if next.Score > existing.Score {
				dst[i] = next
			}
			return dst
		}
	}
	return append(dst, next)
}

func sameAnchor(a, b searchindex.Anchor) bool {
	return a.Type == b.Type &&
		a.Path == b.Path &&
		a.Line == b.Line &&
		a.StartLine == b.StartLine &&
		a.EndLine == b.EndLine &&
		a.StartByte == b.StartByte &&
		a.EndByte == b.EndByte
}
