//go:build cgo

package symbol

import (
	"context"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	elixir "github.com/tree-sitter/tree-sitter-elixir/bindings/go"
)

func extractElixirSymbolsWithTreeSitter(_ context.Context, filePath string, content []byte) ([]Symbol, bool, error) {
	tree, ok := parseElixirTree(content)
	if !ok {
		return nil, false, nil
	}
	defer tree.Close()

	lines := strings.Split(string(content), "\n")
	root := tree.RootNode()
	symbols := make([]Symbol, 0, 16)
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if sym, ok := buildElixirTreeSitterSymbol(filePath, content, lines, node); ok {
			symbols = append(symbols, sym)
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(root)

	return symbols, true, nil
}

func extractElixirCallsWithTreeSitter(_ context.Context, symbol Symbol, content []byte) ([]string, bool, error) {
	body, ok := extractSymbolBodyBytes(symbol, content)
	if !ok {
		return nil, true, nil
	}
	tree, ok := parseElixirTree(body)
	if !ok {
		return nil, false, nil
	}
	defer tree.Close()

	seen := make(map[string]bool)
	out := make([]string, 0, 16)
	emit := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
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
				targetName := strings.TrimSpace(treeSitterNodeText(target, body))
				switch targetName {
				case "alias", "import", "require", "use":
					if args := elixirCallArguments(node); args != nil {
						elixirEmitModuleRefs(args, body, emit)
					}
				default:
					if target.Kind() == "identifier" {
						if isElixirLocalCallCandidate(targetName) {
							emit(targetName)
						}
					}
					if target.Kind() == "dot" {
						if left := target.ChildByFieldName("left"); left != nil && left.Kind() == "alias" {
							emit(strings.TrimSpace(treeSitterNodeText(left, body)))
						}
					}
				}
			}
		case "unary_operator":
			if operand := node.ChildByFieldName("operand"); operand != nil && operand.Kind() == "call" {
				if target := operand.ChildByFieldName("target"); target != nil {
					switch strings.TrimSpace(treeSitterNodeText(target, body)) {
					case "behaviour":
						if args := elixirCallArguments(operand); args != nil {
							elixirEmitModuleRefs(args, body, emit)
						}
					}
				}
			}
		case "struct":
			elixirEmitModuleRefs(node, body, emit)
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(tree.RootNode())

	if len(out) == 0 {
		return nil, true, nil
	}
	if len(out) > 50 {
		out = out[:50]
	}
	return out, true, nil
}

func parseElixirTree(content []byte) (*sitter.Tree, bool) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(elixir.Language())); err != nil {
		return nil, false
	}
	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil, false
	}
	return tree, true
}

func buildElixirTreeSitterSymbol(filePath string, content []byte, lines []string, node *sitter.Node) (Symbol, bool) {
	if node == nil {
		return Symbol{}, false
	}

	switch node.Kind() {
	case "call":
		target := node.ChildByFieldName("target")
		if target == nil {
			return Symbol{}, false
		}
		switch strings.TrimSpace(treeSitterNodeText(target, content)) {
		case "defmodule", "defprotocol", "defimpl":
			name := elixirModuleDeclName(node, content)
			return buildElixirTreeSymbol(filePath, content, lines, node, name, KindClass, false)
		case "def", "defp", "defmacro", "defmacrop":
			name := elixirCallDeclName(node, content)
			return buildElixirTreeSymbol(filePath, content, lines, node, name, KindFunction, false)
		default:
			return Symbol{}, false
		}
	case "unary_operator":
		operand := node.ChildByFieldName("operand")
		if operand == nil || operand.Kind() != "call" {
			return Symbol{}, false
		}
		target := operand.ChildByFieldName("target")
		if target == nil {
			return Symbol{}, false
		}
		switch strings.TrimSpace(treeSitterNodeText(target, content)) {
		case "type", "typep":
			name := elixirTypeDeclName(operand, content)
			return buildElixirTreeSymbol(filePath, content, lines, node, name, KindType, true)
		case "callback":
			name := elixirCallbackDeclName(operand, content)
			return buildElixirTreeSymbol(filePath, content, lines, node, name, KindInterface, false)
		default:
			return Symbol{}, false
		}
	default:
		return Symbol{}, false
	}
}

func buildElixirTreeSymbol(filePath string, content []byte, lines []string, node *sitter.Node, name string, kind Kind, typedoc bool) (Symbol, bool) {
	if node == nil || strings.TrimSpace(name) == "" {
		return Symbol{}, false
	}

	startLine := int(node.StartPosition().Row) + 1
	signature := treeSitterNodeSignature(node, content)
	lineOffsets := computeLineOffsets(lines)

	endLine := startLine
	if kind == KindClass || kind == KindFunction {
		line := ""
		if startLine-1 >= 0 && startLine-1 < len(lines) {
			line = lines[startLine-1]
		}
		if elixirBlockStart.MatchString(line) || strings.HasSuffix(strings.TrimSpace(line), "do") {
			endLine = findElixirBlockEnd(lines, startLine-1) + 1
		}
	}

	startByte := 0
	if startLine-1 >= 0 && startLine-1 < len(lineOffsets) {
		startByte = lineOffsets[startLine-1]
	}
	endByte := len(content)
	if endLine-1 >= 0 && endLine-1 < len(lineOffsets) {
		endByte = lineOffsets[endLine-1] + len(lines[endLine-1])
	}
	if startByte > len(content) {
		startByte = len(content)
	}
	if endByte > len(content) {
		endByte = len(content)
	}
	if endByte < startByte {
		endByte = startByte
	}
	body := content[startByte:endByte]

	doc := ""
	switch kind {
	case KindClass:
		doc = extractElixirModuleDoc(lines, startLine-1, endLine-1)
	case KindType:
		if typedoc {
			doc = findNearestElixirDoc(lines, startLine-1, "@typedoc")
		}
	default:
		doc = findNearestElixirDoc(lines, startLine-1, "@doc")
	}

	return Symbol{
		ID:            ID(filePath, name),
		FilePath:      filePath,
		Name:          name,
		Language:      "elixir",
		Kind:          kind,
		StartByte:     startByte,
		EndByte:       endByte,
		StartLine:     startLine,
		EndLine:       endLine,
		Signature:     signature,
		BodyDigest:    ComputeDigest(body),
		Documentation: strings.TrimSpace(doc),
	}, true
}

func elixirModuleDeclName(node *sitter.Node, content []byte) string {
	args := elixirCallArguments(node)
	if args == nil {
		return ""
	}
	cursor := args.Walk()
	for _, child := range args.NamedChildren(cursor) {
		if child.Kind() == "alias" {
			return strings.TrimSpace(treeSitterNodeText(&child, content))
		}
	}
	return ""
}

func elixirCallDeclName(node *sitter.Node, content []byte) string {
	args := elixirCallArguments(node)
	if args == nil {
		return ""
	}
	cursor := args.Walk()
	for _, child := range args.NamedChildren(cursor) {
		switch child.Kind() {
		case "call":
			if target := child.ChildByFieldName("target"); target != nil {
				return strings.TrimSpace(treeSitterNodeText(target, content))
			}
		case "binary_operator":
			if left := child.ChildByFieldName("left"); left != nil && left.Kind() == "call" {
				if target := left.ChildByFieldName("target"); target != nil {
					return strings.TrimSpace(treeSitterNodeText(target, content))
				}
			}
		case "identifier":
			return strings.TrimSpace(treeSitterNodeText(&child, content))
		}
	}
	return ""
}

func elixirTypeDeclName(node *sitter.Node, content []byte) string {
	args := elixirCallArguments(node)
	if args == nil {
		return ""
	}
	cursor := args.Walk()
	for _, child := range args.NamedChildren(cursor) {
		if child.Kind() == "binary_operator" {
			if left := child.ChildByFieldName("left"); left != nil {
				return strings.TrimSpace(treeSitterNodeText(left, content))
			}
		}
	}
	return ""
}

func elixirCallbackDeclName(node *sitter.Node, content []byte) string {
	args := elixirCallArguments(node)
	if args == nil {
		return ""
	}
	cursor := args.Walk()
	for _, child := range args.NamedChildren(cursor) {
		if child.Kind() == "binary_operator" {
			if left := child.ChildByFieldName("left"); left != nil {
				if left.Kind() == "call" {
					if target := left.ChildByFieldName("target"); target != nil {
						return strings.TrimSpace(treeSitterNodeText(target, content))
					}
				}
				return strings.TrimSpace(treeSitterNodeText(left, content))
			}
		}
	}
	return ""
}

func elixirEmitModuleRefs(node *sitter.Node, content []byte, emit func(string)) {
	if node == nil {
		return
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		c := child
		switch c.Kind() {
		case "alias":
			emit(strings.TrimSpace(treeSitterNodeText(&c, content)))
		case "struct":
			elixirEmitModuleRefs(&c, content, emit)
		case "dot":
			if left := c.ChildByFieldName("left"); left != nil && left.Kind() == "alias" {
				emit(strings.TrimSpace(treeSitterNodeText(left, content)))
			}
		}
		elixirEmitModuleRefs(&c, content, emit)
	}
}

func findNearestElixirDoc(lines []string, startIdx int, attr string) string {
	for i := startIdx - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, attr) {
			doc, _, ok := parseElixirDocAttribute(lines, i, attr)
			if ok {
				return doc
			}
			return ""
		}
		if strings.HasPrefix(trimmed, "@") {
			continue
		}
		break
	}
	return ""
}

func elixirCallArguments(node *sitter.Node) *sitter.Node {
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
