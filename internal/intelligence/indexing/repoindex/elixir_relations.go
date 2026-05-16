package repoindex

import (
	"regexp"
	"sort"
	"strings"
)

type elixirFileRelation struct {
	TargetName string
	Type       EdgeType
	Weight     float64
}

func extractElixirFileRelations(content []byte) []elixirFileRelation {
	if relations, ok := extractElixirFileRelationsWithTreeSitter(content); ok {
		return relations
	}
	return uniqueElixirFileRelations(extractElixirFileRelationsRegex(string(content)))
}

func uniqueElixirFileRelations(relations []elixirFileRelation) []elixirFileRelation {
	if len(relations) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(relations))
	out := make([]elixirFileRelation, 0, len(relations))
	for _, relation := range relations {
		if relation.TargetName == "" || relation.Type == "" {
			continue
		}
		key := string(relation.Type) + "::" + relation.TargetName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, relation)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type == out[j].Type {
			return out[i].TargetName < out[j].TargetName
		}
		return out[i].Type < out[j].Type
	})
	return out
}

var (
	elixirDefimplRe   = regexp.MustCompile(`(?m)^\s*defimpl\s+([A-Z][A-Za-z0-9_.]*)\s*,\s*for:\s*([A-Z][A-Za-z0-9_.]*)\s+do`)
	elixirBehaviourRe = regexp.MustCompile(`(?m)^\s*@behaviour\s+([A-Z][A-Za-z0-9_.]*)`)
)

func extractElixirFileRelationsRegex(source string) []elixirFileRelation {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	relations := make([]elixirFileRelation, 0, 8)
	for _, match := range elixirDefimplRe.FindAllStringSubmatch(source, -1) {
		relations = append(
			relations,
			elixirFileRelation{TargetName: strings.TrimSpace(match[1]), Type: EdgeImplements, Weight: 0.95},
			elixirFileRelation{TargetName: strings.TrimSpace(match[2]), Type: EdgeUsesSymbol, Weight: 0.95},
		)
	}
	for _, match := range elixirBehaviourRe.FindAllStringSubmatch(source, -1) {
		relations = append(
			relations,
			elixirFileRelation{TargetName: strings.TrimSpace(match[1]), Type: EdgeImplements, Weight: 0.95},
		)
	}
	return relations
}
