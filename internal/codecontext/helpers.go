package codecontext

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/jkatigb/agentctl/internal/codecontext/files"
	"github.com/jkatigb/agentctl/internal/searchquery"
)

type lineBlock struct {
	start  int
	end    int
	center int
}

func normalizeCollectOpts(opts CollectOpts) CollectOpts {
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = DefaultMaxFiles
	}
	if opts.MaxSnippets <= 0 {
		opts.MaxSnippets = DefaultMaxSnippets
	}
	if opts.MaxBytesPerFile <= 0 {
		opts.MaxBytesPerFile = DefaultMaxBytesPerFile
	}
	if opts.ContextLines <= 0 {
		opts.ContextLines = DefaultContextLines
	}
	if opts.MaxAnchorsPerFile <= 0 {
		opts.MaxAnchorsPerFile = DefaultMaxAnchorsFile
	}
	if opts.Mode == "" {
		opts.Mode = ModeSnippets
	}
	if opts.SecretMode == "" {
		opts.SecretMode = "warn"
	}
	return opts
}

func parseSymbolName(symbolID string) string {
	if idx := strings.LastIndex(symbolID, "::"); idx != -1 {
		symbolKey := strings.TrimSpace(symbolID[idx+2:])
		if symbolKey != "" {
			if slash := strings.LastIndex(symbolKey, "/"); slash != -1 {
				return symbolKey[slash+1:]
			}
			return symbolKey
		}
	}
	idx := strings.LastIndex(symbolID, ":")
	if idx < 0 || idx >= len(symbolID)-1 {
		return ""
	}
	return strings.TrimSpace(symbolID[idx+1:])
}

func normalizeLegacyCandidate(c Candidate) Candidate {
	if c.Priority == 0 {
		c.Priority = 1.0
	}
	if c.SymbolID != "" && !hasSymbolAnchor(c.Anchors, c.SymbolID) {
		c.Anchors = append(c.Anchors, Anchor{
			Kind:       AnchorSymbol,
			SymbolID:   c.SymbolID,
			SymbolName: parseSymbolName(c.SymbolID),
			Score:      c.Priority,
			Reason:     "legacy_symbol_id",
		})
	}
	if c.LineHint > 0 && !hasLineAnchor(c.Anchors, c.LineHint) {
		c.Anchors = append(c.Anchors, Anchor{
			Kind:      AnchorLine,
			Line:      c.LineHint,
			StartLine: c.LineHint,
			EndLine:   c.LineHint,
			Score:     c.Priority,
			Reason:    "legacy_line_hint",
		})
	}
	if len(c.Anchors) == 0 {
		c.Anchors = append(c.Anchors, Anchor{
			Kind:   AnchorFile,
			Score:  c.Priority,
			Reason: "file_candidate",
		})
	}
	return c
}

func hasSymbolAnchor(anchors []Anchor, symbolID string) bool {
	for _, a := range anchors {
		if a.Kind == AnchorSymbol && a.SymbolID == symbolID {
			return true
		}
	}
	return false
}

func hasLineAnchor(anchors []Anchor, line int) bool {
	for _, a := range anchors {
		if a.Kind == AnchorLine && a.Line == line {
			return true
		}
	}
	return false
}

func dedupeAnchors(in []Anchor) []Anchor {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]Anchor, len(in))
	for _, a := range in {
		key := strings.Join([]string{
			string(a.Kind),
			a.SymbolID,
			a.SymbolName,
			itoa(a.Line),
			itoa(a.StartLine),
			itoa(a.EndLine),
		}, "|")
		prev, ok := seen[key]
		if !ok || a.Score > prev.Score {
			seen[key] = a
		}
	}
	out := make([]Anchor, 0, len(seen))
	for _, a := range seen {
		out = append(out, a)
	}
	return out
}

func classifyReadErr(path string, err error) FileError {
	var re *files.ReadError
	if errors.As(err, &re) {
		return FileError{Path: path, Code: re.Code, Message: re.Message}
	}
	return FileError{Path: path, Code: "EIO", Message: err.Error()}
}

func joinLines(lines []string, startZeroIdx, endZeroIdx int) string {
	if len(lines) == 0 {
		return ""
	}
	if startZeroIdx < 0 {
		startZeroIdx = 0
	}
	if endZeroIdx >= len(lines) {
		endZeroIdx = len(lines) - 1
	}
	if startZeroIdx > endZeroIdx {
		return ""
	}
	return strings.Join(lines[startZeroIdx:endZeroIdx+1], "\n")
}

func findMatchingLines(lines []string, plan searchquery.QueryPlan) []int {
	if len(lines) == 0 {
		return nil
	}
	var out []int
	for idx, line := range lines {
		score, _ := matchScore(line, plan)
		if score > 0 {
			out = append(out, idx+1)
		}
	}
	return out
}

func groupLinesIntoBlocks(matches []int, totalLines, context int) []lineBlock {
	if len(matches) == 0 {
		return nil
	}

	var out []lineBlock
	var cur *lineBlock

	for _, line := range matches {
		start := maxInt(1, line-context)
		end := minInt(totalLines, line+context)

		if cur == nil {
			cur = &lineBlock{start: start, end: end, center: line}
			continue
		}

		if start <= cur.end+1 {
			cur.end = maxInt(cur.end, end)
			continue
		}

		out = append(out, *cur)
		cur = &lineBlock{start: start, end: end, center: line}
	}

	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func trimAndDedupWarnings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func sortAnchorsByScore(anchors []Anchor) {
	sort.Slice(anchors, func(i, j int) bool {
		return anchors[i].Score > anchors[j].Score
	})
}

func sortCandidatesByPriority(candidates []Candidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority == candidates[j].Priority {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Priority > candidates[j].Priority
	})
}

func matchScore(text string, plan searchquery.QueryPlan) (float64, []string) {
	ms := searchquery.ScoreText(plan, text)
	if ms.Score == 0 {
		return 0, nil
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	matched := make([]string, 0)
	seen := map[string]bool{}

	for _, phrase := range plan.Phrases {
		needle := strings.ToLower(strings.TrimSpace(phrase.Text))
		if needle != "" && strings.Contains(lower, needle) && !seen[needle] {
			seen[needle] = true
			matched = append(matched, needle)
		}
	}
	for _, hint := range plan.PathHints {
		needle := strings.ToLower(strings.TrimSpace(hint.Path))
		if needle != "" && strings.Contains(lower, needle) && !seen[needle] {
			seen[needle] = true
			matched = append(matched, needle)
		}
	}
	for _, id := range plan.Identifiers {
		needle := strings.ToLower(strings.TrimSpace(id.Value))
		if needle != "" && strings.Contains(lower, needle) && !seen[needle] {
			seen[needle] = true
			matched = append(matched, needle)
		}
	}
	for _, term := range plan.Terms {
		needle := strings.ToLower(strings.TrimSpace(term))
		if needle != "" && strings.Contains(lower, needle) && !seen[needle] {
			seen[needle] = true
			matched = append(matched, needle)
		}
	}
	return ms.Score, matched
}
