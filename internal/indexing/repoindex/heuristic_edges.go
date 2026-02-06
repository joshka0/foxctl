package repoindex

import (
	"sort"
	"strings"
)

// pendingNameEdge captures a name-based (heuristic) relationship discovered during indexing
// that must be resolved to concrete node IDs after all nodes are collected.
type pendingNameEdge struct {
	SrcID      string
	SrcPkg     string
	SrcFile    string
	TargetName string
	Type       EdgeType
	Weight     float64
}

type symbolNameIndex struct {
	byPkgFile map[string]map[string]map[string][]string
	byPkg     map[string]map[string][]string
	global    map[string][]string
}

func buildSymbolNameIndex(nodes map[string]Node) symbolNameIndex {
	idx := symbolNameIndex{
		byPkgFile: make(map[string]map[string]map[string][]string),
		byPkg:     make(map[string]map[string][]string),
		global:    make(map[string][]string),
	}
	for id, node := range nodes {
		if node.Kind != NodeSymbol || node.Name == "" {
			continue
		}
		if node.Pkg != "" && node.File != "" {
			if idx.byPkgFile[node.Pkg] == nil {
				idx.byPkgFile[node.Pkg] = make(map[string]map[string][]string)
			}
			if idx.byPkgFile[node.Pkg][node.File] == nil {
				idx.byPkgFile[node.Pkg][node.File] = make(map[string][]string)
			}
			idx.byPkgFile[node.Pkg][node.File][node.Name] = append(idx.byPkgFile[node.Pkg][node.File][node.Name], id)
		}
		if node.Pkg != "" {
			if idx.byPkg[node.Pkg] == nil {
				idx.byPkg[node.Pkg] = make(map[string][]string)
			}
			idx.byPkg[node.Pkg][node.Name] = append(idx.byPkg[node.Pkg][node.Name], id)
		}
		idx.global[node.Name] = append(idx.global[node.Name], id)
	}

	for _, byFile := range idx.byPkgFile {
		for _, byName := range byFile {
			for _, ids := range byName {
				sort.Strings(ids)
			}
		}
	}
	for _, byName := range idx.byPkg {
		for _, ids := range byName {
			sort.Strings(ids)
		}
	}
	for _, ids := range idx.global {
		sort.Strings(ids)
	}

	return idx
}

func resolveSymbolName(idx symbolNameIndex, pkg, file, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	if pkg != "" && file != "" {
		if byFile, ok := idx.byPkgFile[pkg]; ok {
			if byName, ok := byFile[file]; ok {
				if ids := byName[name]; len(ids) == 1 {
					return ids[0]
				}
			}
		}
	}
	if pkg != "" {
		if byName, ok := idx.byPkg[pkg]; ok {
			if ids := byName[name]; len(ids) == 1 {
				return ids[0]
			}
		}
	}
	if ids := idx.global[name]; len(ids) == 1 {
		return ids[0]
	}
	return ""
}

func resolveCandidateNames(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	out := []string{name}
	// Avoid accidentally resolving "MyApp.Foo" -> "Foo" when module names aren't indexed.
	// (Elixir modules, Go receiver-qualified methods, etc. tend to start uppercase.)
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' && strings.Contains(name, ".") {
		return out
	}
	if idx := strings.LastIndex(name, "."); idx >= 0 && idx+1 < len(name) {
		last := strings.TrimSpace(name[idx+1:])
		if last != "" && last != name {
			out = append(out, last)
		}
	}
	return out
}

func applyPendingNameEdges(nodes map[string]Node, edges map[string]Edge, pending []pendingNameEdge) {
	if len(pending) == 0 {
		return
	}
	idx := buildSymbolNameIndex(nodes)

	for _, item := range pending {
		if item.SrcID == "" || strings.TrimSpace(item.TargetName) == "" || item.Type == "" {
			continue
		}
		for _, candidate := range resolveCandidateNames(item.TargetName) {
			dstID := resolveSymbolName(idx, item.SrcPkg, item.SrcFile, candidate)
			if dstID == "" || dstID == item.SrcID {
				continue
			}
			weight := item.Weight
			if weight <= 0 {
				weight = 1.0
			}
			addEdge(edges, Edge{
				Src:    item.SrcID,
				Dst:    dstID,
				Type:   item.Type,
				Weight: weight,
			})
			break
		}
	}
}
