package repoquery

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
)

// Anchor is the repoquery graph-to-code projection shape.
type Anchor struct {
	Path       string  `json:"path"`
	SymbolID   string  `json:"symbol_id,omitempty"`
	SymbolName string  `json:"symbol_name,omitempty"`
	LineHint   int     `json:"line_hint,omitempty"`
	Score      float64 `json:"score,omitempty"`
	Source     string  `json:"source,omitempty"`
	Summary    string  `json:"summary,omitempty"`
}

type SearchOutput struct {
	Nodes   []repoindex.Node `json:"nodes"`
	Anchors []Anchor         `json:"anchors,omitempty"`
}

type ExpandOutput struct {
	Result  repoindex.ExpandResult `json:"result"`
	Anchors []Anchor               `json:"anchors,omitempty"`
}

type OpenOutput struct {
	Node   repoindex.Node `json:"node"`
	Anchor *Anchor        `json:"anchor,omitempty"`
}

type DAGOutput struct {
	Result   repoindex.DAGGrepResult `json:"result"`
	Anchors  []Anchor                `json:"anchors,omitempty"`
	Rendered map[string]string       `json:"rendered,omitempty"`
}

// Projector converts repo graph nodes into code-oriented anchors.
type Projector struct{}

// FromNodes projects graph nodes into anchors, skipping nodes without a code location.
func (Projector) FromNodes(nodes []repoindex.Node) []Anchor {
	out := make([]Anchor, 0, len(nodes))
	for _, n := range nodes {
		if a := (Projector{}).FromNodeValue(n); a != nil {
			out = append(out, *a)
		}
	}
	return out
}

// FromNode projects a single node into an anchor.
func (Projector) FromNode(node repoindex.Node) *Anchor {
	return (Projector{}).FromNodeValue(node)
}

// FromNodeValue is the non-method receiver form used internally.
func (Projector) FromNodeValue(node repoindex.Node) *Anchor {
	switch node.Kind {
	case repoindex.NodeSymbol:
		if strings.TrimSpace(node.File) == "" {
			return nil
		}
		return &Anchor{
			Path:       node.File,
			SymbolID:   symbolIDFromNode(node),
			SymbolName: strings.TrimSpace(node.Name),
			LineHint:   firstPositive(node.SpanStart, node.SpanEnd),
			Score:      1.0,
			Source:     "repo_index",
			Summary:    node.Summary,
		}
	case repoindex.NodeFile:
		if strings.TrimSpace(node.File) == "" {
			return nil
		}
		return &Anchor{
			Path:     node.File,
			LineHint: firstPositive(node.SpanStart, node.SpanEnd),
			Score:    1.0,
			Source:   "repo_index",
			Summary:  node.Summary,
		}
	case repoindex.NodeConcept:
		if strings.TrimSpace(node.File) == "" {
			return nil
		}
		return &Anchor{
			Path:       node.File,
			SymbolName: strings.TrimSpace(node.Name),
			LineHint:   firstPositive(node.SpanStart, node.SpanEnd),
			Score:      1.0,
			Source:     "repo_index",
			Summary:    node.Summary,
		}
	default:
		return nil
	}
}

func symbolIDFromNode(node repoindex.Node) string {
	if strings.TrimSpace(node.File) == "" || strings.TrimSpace(node.Name) == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", node.File, node.Name)
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

// AnchorFromNode projects a repo index node into a retrieval/codecontext-friendly anchor.
func AnchorFromNode(node repoindex.Node, score float64) (Anchor, bool) {
	path := strings.TrimSpace(node.File)
	if path == "" {
		return Anchor{}, false
	}

	anchor := Anchor{
		Path:       path,
		SymbolID:   symbolIDFromNode(node),
		SymbolName: strings.TrimSpace(node.Name),
		LineHint:   firstPositive(node.SpanStart, node.SpanEnd),
		Score:      normalizeAnchorScore(score),
		Source:     "repo_index",
		Summary:    node.Summary,
	}
	if node.Kind != repoindex.NodeSymbol {
		anchor.SymbolID = ""
	}
	return anchor, true
}

// ProjectAnchors converts node results into anchors and de-duplicates them.
func ProjectAnchors(nodes []repoindex.Node, scores map[string]float64) []Anchor {
	if len(nodes) == 0 {
		return nil
	}

	anchors := make([]Anchor, 0, len(nodes))
	for _, node := range nodes {
		anchor, ok := AnchorFromNode(node, scoreFromMap(scores, node.ID))
		if !ok {
			continue
		}
		anchors = append(anchors, anchor)
	}

	return dedupeAnchors(anchors)
}

func scoreFromMap(scores map[string]float64, id string) float64 {
	if len(scores) == 0 || strings.TrimSpace(id) == "" {
		return 0
	}
	return scores[id]
}

func normalizeAnchorScore(score float64) float64 {
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0
	}
	return score
}

func anchorKey(anchor Anchor) string {
	return strings.Join([]string{
		anchor.Path,
		anchor.SymbolID,
		strconv.Itoa(anchor.LineHint),
	}, "|")
}

func dedupeAnchors(anchors []Anchor) []Anchor {
	if len(anchors) <= 1 {
		return anchors
	}

	seen := make(map[string]Anchor, len(anchors))
	for _, anchor := range anchors {
		key := anchorKey(anchor)
		prev, ok := seen[key]
		if !ok || anchor.Score > prev.Score {
			seen[key] = anchor
		}
	}

	unique := make([]Anchor, 0, len(seen))
	for _, anchor := range seen {
		unique = append(unique, anchor)
	}

	sort.Slice(unique, func(i, j int) bool {
		if unique[i].Score == unique[j].Score {
			return anchorKey(unique[i]) < anchorKey(unique[j])
		}
		return unique[i].Score > unique[j].Score
	})
	return unique
}
