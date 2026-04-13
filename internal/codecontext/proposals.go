package codecontext

import (
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/codecontext/expander"
	"github.com/jkatigb/agentctl/internal/codecontext/files"
)

const maxLinesPerSnippet = 80

type snippetProposal struct {
	Snippet

	fileScore   float64
	anchorScore float64
	queryScore  float64
	source      string
	matched     []string
	finalScore  float64
}

func proposeForFile(fc *files.FileContent, cand Candidate, plan QueryPlan, opts CollectOpts) []snippetProposal {
	var out []snippetProposal

	out = append(out, proposeAnchors(fc, cand, plan, opts)...)
	out = append(out, proposeQueryMatches(fc, cand, plan, opts)...)

	if len(out) == 0 {
		out = append(out, fallbackProposal(fc, cand, plan))
	}

	out = dedupeLocalProposals(out)

	for i := range out {
		out[i].finalScore = scoreProposal(out[i])
		out[i].Priority = out[i].finalScore
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].finalScore != out[j].finalScore {
			return out[i].finalScore > out[j].finalScore
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].StartLine < out[j].StartLine
	})

	return out
}

func proposeAnchors(fc *files.FileContent, cand Candidate, plan QueryPlan, opts CollectOpts) []snippetProposal {
	out := make([]snippetProposal, 0, len(cand.Anchors))

	for _, a := range cand.Anchors {
		start, end, symbolID, reason, ok := expandAnchor(fc, a, opts)
		if !ok {
			continue
		}

		text := joinLines(fc.Lines, start-1, end-1)
		qscore, matched := matchScore(text, plan)

		out = append(out, snippetProposal{
			Snippet: Snippet{
				File:      fc.Path,
				SymbolID:  symbolID,
				StartLine: start,
				EndLine:   end,
				Text:      text,
				Reason:    reason,
				Language:  fc.Language,
			},
			fileScore:   cand.Priority,
			anchorScore: maxFloat(a.Score, cand.Priority),
			queryScore:  qscore,
			source:      string(a.Kind),
			matched:     matched,
		})
	}

	return out
}

func proposeQueryMatches(fc *files.FileContent, cand Candidate, plan QueryPlan, opts CollectOpts) []snippetProposal {
	if strings.TrimSpace(plan.Raw) == "" {
		return nil
	}
	lines := findMatchingLines(fc.Lines, plan)
	if len(lines) == 0 {
		return nil
	}

	blocks := groupLinesIntoBlocks(lines, len(fc.Lines), opts.ContextLines)
	out := make([]snippetProposal, 0, len(blocks))

	for _, b := range blocks {
		start := b.start
		end := b.end

		ex := expander.GetOrGeneric(fc.Language)
		if ex != nil {
			if s, e, _, err := ex.FindBlock(fc, b.center); err == nil {
				if e-s+1 <= maxLinesPerSnippet {
					start, end = s, e
				}
			}
		}

		text := joinLines(fc.Lines, start-1, end-1)
		qscore, matched := matchScore(text, plan)

		out = append(out, snippetProposal{
			Snippet: Snippet{
				File:      fc.Path,
				StartLine: start,
				EndLine:   end,
				Text:      text,
				Reason:    "query_match",
				Language:  fc.Language,
			},
			fileScore:   cand.Priority,
			anchorScore: 0.0,
			queryScore:  qscore,
			source:      "query_match",
			matched:     matched,
		})
	}

	return out
}

func fallbackProposal(fc *files.FileContent, cand Candidate, plan QueryPlan) snippetProposal {
	end := minInt(fc.LineCount(), maxLinesPerSnippet)
	text := joinLines(fc.Lines, 0, end-1)

	qscore, matched := matchScore(text, plan)

	return snippetProposal{
		Snippet: Snippet{
			File:      fc.Path,
			StartLine: 1,
			EndLine:   end,
			Text:      text,
			Reason:    "file_start",
			Language:  fc.Language,
		},
		fileScore:   cand.Priority,
		anchorScore: 0.0,
		queryScore:  qscore,
		source:      "file_start",
		matched:     matched,
	}
}

func expandAnchor(fc *files.FileContent, a Anchor, opts CollectOpts) (start, end int, symbolID, reason string, ok bool) {
	switch a.Kind {
	case AnchorSymbol:
		symbolID = a.SymbolID
		name := firstNonEmpty(a.SymbolName, parseSymbolName(a.SymbolID))
		if name == "" {
			return 0, 0, "", "", false
		}
		ex := expander.GetOrGeneric(fc.Language)
		if ex == nil {
			return 0, 0, "", "", false
		}
		s, e, err := ex.ExpandToSymbol(fc, name)
		if err == nil {
			if e-s+1 > maxLinesPerSnippet {
				e = s + maxLinesPerSnippet - 1
			}
			return s, e, symbolID, "symbol_anchor", true
		}
		if a.StartLine > 0 {
			s = a.StartLine
			e = maxInt(a.EndLine, a.StartLine)
			if e-s+1 > maxLinesPerSnippet {
				e = s + maxLinesPerSnippet - 1
			}
			return s, e, symbolID, "symbol_anchor_fallback", true
		}
		return 0, 0, "", "", false

	case AnchorLine:
		line := a.Line
		if line <= 0 {
			line = maxInt(a.StartLine, 1)
		}
		ex := expander.GetOrGeneric(fc.Language)
		if ex != nil {
			if s, e, _, err := ex.FindBlock(fc, line); err == nil {
				if e-s+1 > maxLinesPerSnippet {
					e = s + maxLinesPerSnippet - 1
				}
				return s, e, "", "line_anchor", true
			}
		}
		s := maxInt(1, line-opts.ContextLines)
		e := minInt(fc.LineCount(), line+opts.ContextLines)
		return s, e, "", "line_context", true

	case AnchorFile:
		return 0, 0, "", "", false
	default:
		return 0, 0, "", "", false
	}
}
