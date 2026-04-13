//go:build cgo

package repoindex

import (
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	ts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func extractTSImportsWithTreeSitter(filePath string, source []byte) ([]string, bool) {
	if !isTSImportableFile(filePath) {
		return nil, false
	}
	grammar, ok := treeSitterTSLanguage(filePath)
	if !ok {
		return nil, false
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(grammar); err != nil {
		return nil, false
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, false
	}
	defer tree.Close()

	root := tree.RootNode()
	imports := make(map[string]struct{})
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Kind() {
		case "import_statement", "export_statement":
			if sourceNode := node.ChildByFieldName("source"); sourceNode != nil {
				addTSImport(imports, treeSitterStringLiteralValue(sourceNode, source))
			}
		case "call_expression":
			if fn := node.ChildByFieldName("function"); fn != nil && fn.Kind() == "import" {
				if args := node.ChildByFieldName("arguments"); args != nil {
					if value := firstStringLiteral(args, source); value != "" {
						addTSImport(imports, value)
					}
				}
			} else if fn != nil && strings.TrimSpace(treeSitterNodeText(fn, source)) == "require" {
				if args := node.ChildByFieldName("arguments"); args != nil {
					if value := firstStringLiteral(args, source); value != "" {
						addTSImport(imports, value)
					}
				}
			}
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(root)

	return flattenTSImports(imports), true
}

func extractTSImportBindingsWithTreeSitter(filePath string, source []byte) ([]tsImportBinding, bool) {
	if !isTSImportableFile(filePath) {
		return nil, false
	}
	grammar, ok := treeSitterTSLanguage(filePath)
	if !ok {
		return nil, false
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(grammar); err != nil {
		return nil, false
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, false
	}
	defer tree.Close()

	root := tree.RootNode()
	bindings := make([]tsImportBinding, 0)
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Kind() == "import_statement" {
			importPath := ""
			if sourceNode := node.ChildByFieldName("source"); sourceNode != nil {
				importPath = treeSitterStringLiteralValue(sourceNode, source)
			}
			if importPath != "" {
				bindings = append(bindings, treeSitterImportBindings(node, importPath, source)...)
			}
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(root)

	return uniqueTSImportBindings(bindings), true
}

func treeSitterTSLanguage(filePath string) (*sitter.Language, bool) {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".ts", ".js", ".mts", ".cts", ".mjs", ".cjs":
		return sitter.NewLanguage(ts.LanguageTypescript()), true
	case ".tsx", ".jsx":
		return sitter.NewLanguage(ts.LanguageTSX()), true
	default:
		return nil, false
	}
}

func treeSitterStringLiteralValue(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "string_fragment" {
			return strings.TrimSpace(treeSitterNodeText(&child, source))
		}
	}
	return strings.Trim(strings.TrimSpace(treeSitterNodeText(node, source)), `"'`)
}

func firstStringLiteral(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "string" {
			return treeSitterStringLiteralValue(&child, source)
		}
	}
	return ""
}

func treeSitterNodeText(node *sitter.Node, source []byte) string {
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

func treeSitterImportBindings(node *sitter.Node, importPath string, source []byte) []tsImportBinding {
	if node == nil || importPath == "" {
		return nil
	}
	clause := node.ChildByFieldName("import_clause")
	if clause == nil {
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			if child.Kind() == "import_clause" {
				c := child
				clause = &c
				break
			}
		}
		if clause == nil {
			return nil
		}
	}
	cursor := clause.Walk()
	children := clause.NamedChildren(cursor)
	out := make([]tsImportBinding, 0, len(children))
	for _, child := range children {
		switch child.Kind() {
		case "identifier":
			out = append(out, tsImportBinding{ImportPath: importPath, TargetName: "default"})
		case "named_imports":
			out = append(out, treeSitterNamedImportBindings(&child, importPath, source)...)
		}
	}
	return out
}

func treeSitterNamedImportBindings(node *sitter.Node, importPath string, source []byte) []tsImportBinding {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	out := make([]tsImportBinding, 0, len(children))
	for _, child := range children {
		if child.Kind() != "import_specifier" {
			continue
		}
		targetName := ""
		if nameNode := child.ChildByFieldName("name"); nameNode != nil {
			targetName = strings.TrimSpace(treeSitterNodeText(nameNode, source))
		}
		if targetName == "" {
			targetName = strings.TrimSpace(treeSitterNodeText(&child, source))
		}
		targetName = strings.TrimPrefix(targetName, "type ")
		targetName = strings.TrimSpace(targetName)
		if targetName == "" {
			continue
		}
		out = append(out, tsImportBinding{ImportPath: importPath, TargetName: targetName})
	}
	return out
}
