package codecontext

import "sort"

func scoreProposal(p snippetProposal) float64 {
	score := 0.60*p.fileScore + 0.25*p.anchorScore + 0.15*p.queryScore

	switch p.source {
	case string(AnchorSymbol):
		score += 0.10
	case string(AnchorLine):
		score += 0.05
	}

	if len(p.matched) >= 3 {
		score += 0.05
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

func finalizeProposals(in []snippetProposal, maxSnippets int) []Snippet {
	if len(in) == 0 {
		return nil
	}

	byRange := map[string]snippetProposal{}
	for _, p := range in {
		key := proposalKey(p)
		prev, ok := byRange[key]
		if !ok || p.finalScore > prev.finalScore {
			byRange[key] = p
		}
	}

	unique := make([]snippetProposal, 0, len(byRange))
	for _, p := range byRange {
		unique = append(unique, p)
	}

	sort.Slice(unique, func(i, j int) bool {
		if unique[i].finalScore != unique[j].finalScore {
			return unique[i].finalScore > unique[j].finalScore
		}
		if unique[i].File != unique[j].File {
			return unique[i].File < unique[j].File
		}
		return unique[i].StartLine < unique[j].StartLine
	})

	if maxSnippets > 0 && len(unique) > maxSnippets {
		unique = unique[:maxSnippets]
	}

	out := make([]Snippet, 0, len(unique))
	for _, p := range unique {
		p.Priority = p.finalScore
		out = append(out, p.Snippet)
	}
	return out
}

func dedupeLocalProposals(in []snippetProposal) []snippetProposal {
	byRange := map[string]snippetProposal{}
	for _, p := range in {
		key := proposalKey(p)
		prev, ok := byRange[key]
		if !ok || p.anchorScore+p.queryScore > prev.anchorScore+prev.queryScore {
			byRange[key] = p
		}
	}
	out := make([]snippetProposal, 0, len(byRange))
	for _, p := range byRange {
		out = append(out, p)
	}
	return out
}

func proposalKey(p snippetProposal) string {
	return p.File + ":" + itoa(p.StartLine) + ":" + itoa(p.EndLine) + ":" + p.SymbolID
}
