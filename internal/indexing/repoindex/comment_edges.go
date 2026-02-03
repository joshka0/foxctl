package repoindex

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	docparser "github.com/jkatigb/agentctl/internal/indexing/repoindex/parser"
)

const (
	maxKeywordEdges     = 24
	maxResourceEdges    = 16
	maxEventEdges       = 16
	maxOutputFieldEdges = 16
	maxRelatedEdges     = 16
	maxFlowEdges        = 16
	conceptEdgeWeight   = 0.6
	docEdgeWeight       = 0.75
)

func applyCommentEdges(nodes map[string]Node, edges map[string]Edge, repoKey string) {
	if len(nodes) == 0 {
		return
	}
	byPkg := make(map[string]map[string]string)
	global := make(map[string][]string)
	symbols := make([]Node, 0, len(nodes))
	for id, node := range nodes {
		if node.Kind != NodeSymbol {
			continue
		}
		if node.Name == "" {
			continue
		}
		if byPkg[node.Pkg] == nil {
			byPkg[node.Pkg] = make(map[string]string)
		}
		if _, ok := byPkg[node.Pkg][node.Name]; !ok {
			byPkg[node.Pkg][node.Name] = id
		}
		global[node.Name] = append(global[node.Name], id)
		symbols = append(symbols, node)
	}
	for _, ids := range global {
		sort.Strings(ids)
	}

	now := time.Now().UTC()
	for _, node := range symbols {
		if node.Kind != NodeSymbol || len(node.Meta) == 0 {
			continue
		}
		var meta docparser.DocIndex
		if err := json.Unmarshal(node.Meta, &meta); err != nil {
			continue
		}

		addConceptEdges(nodes, edges, repoKey, node, meta.Keywords, ConceptKeyword, EdgeHasKeyword, maxKeywordEdges, now)
		addConceptEdges(nodes, edges, repoKey, node, meta.Resources, ConceptResource, EdgeTouchesResource, maxResourceEdges, now)
		addConceptEdges(nodes, edges, repoKey, node, meta.Events, ConceptEvent, EdgeEmitsEvent, maxEventEdges, now)
		addConceptEdges(nodes, edges, repoKey, node, meta.OutputFields, ConceptField, EdgeHasOutputField, maxOutputFieldEdges, now)

		addDocEdges(edges, repoKey, node, meta.Related, EdgeDocRelated, maxRelatedEdges, byPkg, global)
		addDocEdges(edges, repoKey, node, meta.Flow, EdgeDocFlow, maxFlowEdges, byPkg, global)
	}
}

func addConceptEdges(nodes map[string]Node, edges map[string]Edge, repoKey string, src Node, items []string, prefix string, edgeType EdgeType, limit int, now time.Time) {
	items = capList(items, limit)
	for _, item := range items {
		if item == "" {
			continue
		}
		conceptID := NamespacedID(repoKey, prefix+item)
		addNode(nodes, Node{
			ID:        conceptID,
			Kind:      NodeConcept,
			Name:      item,
			UpdatedAt: now,
		})
		addEdge(edges, Edge{
			Src:    src.ID,
			Dst:    conceptID,
			Type:   edgeType,
			Weight: conceptEdgeWeight,
		})
	}
}

func addDocEdges(edges map[string]Edge, repoKey string, src Node, targets []string, edgeType EdgeType, limit int, byPkg map[string]map[string]string, global map[string][]string) {
	targets = capList(targets, limit)
	for _, target := range targets {
		resolved := resolveSymbolID(repoKey, src.Pkg, target, byPkg, global)
		if resolved == "" {
			continue
		}
		addEdge(edges, Edge{
			Src:    src.ID,
			Dst:    resolved,
			Type:   edgeType,
			Weight: docEdgeWeight,
		})
	}
}

func capList(items []string, limit int) []string {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func resolveSymbolID(repoKey, pkg, name string, byPkg map[string]map[string]string, global map[string][]string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.Contains(name, "::") {
		return name
	}
	if strings.HasPrefix(name, "sym:") || strings.HasPrefix(name, "file:") || strings.HasPrefix(name, "pkg:") || strings.HasPrefix(name, "repo:") {
		return NamespacedID(repoKey, name)
	}
	if strings.HasPrefix(name, ConceptKeyword) || strings.HasPrefix(name, ConceptField) || strings.HasPrefix(name, ConceptResource) || strings.HasPrefix(name, ConceptEvent) {
		return NamespacedID(repoKey, name)
	}
	if pkg != "" {
		if id, ok := byPkg[pkg][name]; ok {
			return id
		}
	}
	if ids, ok := global[name]; ok && len(ids) == 1 {
		return ids[0]
	}
	return ""
}
