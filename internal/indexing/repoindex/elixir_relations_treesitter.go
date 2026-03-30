//go:build cgo

package repoindex

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	elixir "github.com/tree-sitter/tree-sitter-elixir/bindings/go"
)

func extractElixirFileRelationsWithTreeSitter(content []byte) ([]elixirFileRelation, bool) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(elixir.Language())); err != nil {
		return nil, false
	}
	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil, false
	}
	defer tree.Close()

	relations := make([]elixirFileRelation, 0, 16)
	emit := func(target string, typ EdgeType, weight float64) {
		target = strings.TrimSpace(target)
		if target == "" {
			return
		}
		relations = append(relations, elixirFileRelation{
			TargetName: target,
			Type:       typ,
			Weight:     weight,
		})
	}

	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Kind() {
		case "call":
			target := node.ChildByFieldName("target")
			if target != nil {
				targetName := strings.TrimSpace(repoTreeSitterNodeText(target, content))
				switch targetName {
				case "alias", "import", "require", "use":
					if args := elixirRepoCallArguments(node); args != nil {
						elixirRepoEmitAliases(args, content, func(name string) {
							emit(name, EdgeRefersTo, 0.85)
						})
					}
				case "defimpl":
					if args := elixirRepoCallArguments(node); args != nil {
						if protocol := elixirRepoFirstAlias(args, content); protocol != "" {
							emit(protocol, EdgeImplements, 0.95)
						}
						if implFor := elixirRepoKeywordAlias(args, "for", content); implFor != "" {
							emit(implFor, EdgeUsesSymbol, 0.95)
						}
					}
				default:
					if target.Kind() == "dot" {
						if left := target.ChildByFieldName("left"); left != nil && left.Kind() == "alias" {
							emit(strings.TrimSpace(repoTreeSitterNodeText(left, content)), EdgeRefersTo, 0.85)
						}
					}
				}
			}
		case "unary_operator":
			if operand := node.ChildByFieldName("operand"); operand != nil && operand.Kind() == "call" {
				if target := operand.ChildByFieldName("target"); target != nil && strings.TrimSpace(repoTreeSitterNodeText(target, content)) == "behaviour" {
					if args := elixirRepoCallArguments(operand); args != nil {
						elixirRepoEmitAliases(args, content, func(name string) {
							emit(name, EdgeImplements, 0.95)
						})
					}
				}
			}
		case "struct":
			elixirRepoEmitAliases(node, content, func(name string) {
				emit(name, EdgeUsesSymbol, 0.85)
			})
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	root := tree.RootNode()
	walk(root)

	return uniqueElixirFileRelations(relations), true
}

func repoTreeSitterNodeText(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	start := int(node.StartByte())
	end := int(node.EndByte())
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > len(source) {
		end = len(source)
	}
	return string(source[start:end])
}

func elixirRepoCallArguments(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	if args := node.ChildByFieldName("arguments"); args != nil {
		return args
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "arguments" {
			c := child
			return &c
		}
	}
	return nil
}

func elixirRepoEmitAliases(node *sitter.Node, source []byte, emit func(string)) {
	if node == nil {
		return
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		c := child
		switch c.Kind() {
		case "alias":
			emit(strings.TrimSpace(repoTreeSitterNodeText(&c, source)))
		case "dot":
			if left := c.ChildByFieldName("left"); left != nil && left.Kind() == "alias" {
				emit(strings.TrimSpace(repoTreeSitterNodeText(left, source)))
			}
		}
		elixirRepoEmitAliases(&c, source, emit)
	}
}

func elixirRepoFirstAlias(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "alias" {
			return strings.TrimSpace(repoTreeSitterNodeText(&child, source))
		}
	}
	return ""
}

func elixirRepoKeywordAlias(node *sitter.Node, key string, source []byte) string {
	if node == nil {
		return ""
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() != "keywords" {
			continue
		}
		pairCursor := child.Walk()
		for _, pair := range child.NamedChildren(pairCursor) {
			if pair.Kind() != "pair" {
				continue
			}
			k := pair.ChildByFieldName("key")
			if k == nil {
				continue
			}
			keyText := strings.TrimSpace(repoTreeSitterNodeText(k, source))
			keyText = strings.TrimSpace(strings.TrimSuffix(keyText, ":"))
			if keyText != key {
				continue
			}
			if value := pair.ChildByFieldName("value"); value != nil && value.Kind() == "alias" {
				return strings.TrimSpace(repoTreeSitterNodeText(value, source))
			}
		}
	}
	return ""
}
