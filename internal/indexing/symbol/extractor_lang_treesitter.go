//go:build cgo

package symbol

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	py "github.com/tree-sitter/tree-sitter-python/bindings/go"
	rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	ts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func extractTypeScriptSymbolsWithTreeSitter(_ context.Context, filePath string, content []byte) ([]Symbol, bool, error) {
	lang, grammar, ok := treeSitterTypeScriptLanguage(filePath)
	if !ok {
		return nil, false, nil
	}

	tree, ok := parseTreeSitterContent(grammar, content)
	if !ok {
		return nil, false, nil
	}
	defer tree.Close()

	lines := strings.Split(string(content), "\n")
	root := tree.RootNode()
	cursor := root.Walk()
	children := root.NamedChildren(cursor)
	symbols := make([]Symbol, 0, len(children))
	for i := range children {
		child := children[i]
		symbols = append(symbols, extractTopLevelTSSymbols(filePath, lang, content, lines, &child)...)
	}
	return symbols, true, nil
}

func extractTypeScriptCallsWithTreeSitter(_ context.Context, symbol Symbol, content []byte) ([]string, bool, error) {
	_, grammar, ok := treeSitterTypeScriptLanguage(symbol.FilePath)
	if !ok {
		return nil, false, nil
	}
	body, ok := extractSymbolBodyBytes(symbol, content)
	if !ok {
		return nil, true, nil
	}

	tree, ok := parseTreeSitterContent(grammar, body)
	if !ok {
		return nil, false, nil
	}
	defer tree.Close()

	calls := collectTreeSitterCallNames(tree.RootNode(), body, tsCallNameFromNode)
	return calls, true, nil
}

func extractTypeScriptRefsWithTreeSitter(_ context.Context, _ Symbol, _ []byte) ([]string, bool, error) {
	return nil, false, nil
}

func extractPythonSymbolsWithTreeSitter(_ context.Context, filePath string, content []byte) ([]Symbol, bool, error) {
	if strings.ToLower(filepath.Ext(filePath)) != ".py" {
		return nil, false, nil
	}
	grammar := sitter.NewLanguage(py.Language())
	tree, ok := parseTreeSitterContent(grammar, content)
	if !ok {
		return nil, false, nil
	}
	defer tree.Close()

	lines := strings.Split(string(content), "\n")
	root := tree.RootNode()
	cursor := root.Walk()
	children := root.NamedChildren(cursor)
	symbols := make([]Symbol, 0, len(children))
	for i := range children {
		child := children[i]
		switch child.Kind() {
		case "function_definition":
			if sym, ok := buildPythonTreeSitterSymbol(filePath, content, lines, &child, KindFunction); ok {
				symbols = append(symbols, sym)
			}
		case "class_definition":
			if sym, ok := buildPythonTreeSitterSymbol(filePath, content, lines, &child, KindClass); ok {
				symbols = append(symbols, sym)
			}
		}
	}
	return symbols, true, nil
}

func extractPythonCallsWithTreeSitter(_ context.Context, symbol Symbol, content []byte) ([]string, bool, error) {
	if strings.ToLower(filepath.Ext(symbol.FilePath)) != ".py" {
		return nil, false, nil
	}
	body, ok := extractSymbolBodyBytes(symbol, content)
	if !ok {
		return nil, true, nil
	}

	grammar := sitter.NewLanguage(py.Language())
	tree, ok := parseTreeSitterContent(grammar, body)
	if !ok {
		return nil, false, nil
	}
	defer tree.Close()

	calls := collectTreeSitterCallNames(tree.RootNode(), body, pythonCallNameFromNode)
	return calls, true, nil
}

func extractRustSymbolsWithTreeSitter(_ context.Context, filePath string, content []byte) ([]Symbol, bool, error) {
	if strings.ToLower(filepath.Ext(filePath)) != ".rs" {
		return nil, false, nil
	}
	grammar := sitter.NewLanguage(rust.Language())
	tree, ok := parseTreeSitterContent(grammar, content)
	if !ok {
		return nil, false, nil
	}
	defer tree.Close()

	lines := strings.Split(string(content), "\n")
	root := tree.RootNode()
	cursor := root.Walk()
	children := root.NamedChildren(cursor)
	symbols := make([]Symbol, 0, len(children))
	for i := range children {
		child := children[i]
		symbols = append(symbols, extractTopLevelRustSymbols(filePath, content, lines, &child)...)
	}
	return symbols, true, nil
}

func extractRustCallsWithTreeSitter(_ context.Context, symbol Symbol, content []byte) ([]string, bool, error) {
	if strings.ToLower(filepath.Ext(symbol.FilePath)) != ".rs" {
		return nil, false, nil
	}
	body, ok := extractSymbolBodyBytes(symbol, content)
	if !ok {
		return nil, true, nil
	}

	grammar := sitter.NewLanguage(rust.Language())
	tree, ok := parseTreeSitterContent(grammar, body)
	if !ok {
		return nil, false, nil
	}
	defer tree.Close()

	calls := collectRustTreeSitterCallNames(tree.RootNode(), body)
	return calls, true, nil
}

func parseTreeSitterContent(grammar *sitter.Language, content []byte) (*sitter.Tree, bool) {
	if grammar == nil {
		return nil, false
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(grammar); err != nil {
		return nil, false
	}
	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil, false
	}
	return tree, true
}

func treeSitterTypeScriptLanguage(filePath string) (string, *sitter.Language, bool) {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".ts", ".mts", ".cts", ".js", ".mjs", ".cjs":
		lang := "typescript"
		if strings.HasSuffix(strings.ToLower(filePath), ".js") || strings.HasSuffix(strings.ToLower(filePath), ".mjs") || strings.HasSuffix(strings.ToLower(filePath), ".cjs") {
			lang = "javascript"
		}
		return lang, sitter.NewLanguage(ts.LanguageTypescript()), true
	case ".tsx", ".jsx":
		lang := "typescript"
		if strings.HasSuffix(strings.ToLower(filePath), ".jsx") {
			lang = "javascript"
		}
		return lang, sitter.NewLanguage(ts.LanguageTSX()), true
	default:
		return "", nil, false
	}
}

func extractTopLevelTSSymbols(filePath, language string, content []byte, lines []string, node *sitter.Node) []Symbol {
	decl, container, defaultExport := unwrapTSDeclarationNode(node, content)
	if decl == nil || container == nil {
		return nil
	}

	switch decl.Kind() {
	case "function_declaration", "function_expression":
		name := treeSitterNodeName(decl, content)
		if name == "" && defaultExport {
			name = "default"
		}
		return singleTreeSitterSymbol(filePath, language, content, lines, container, name, KindFunction, extractTSLeadingDoc)
	case "class_declaration", "abstract_class_declaration":
		name := treeSitterNodeName(decl, content)
		if name == "" && defaultExport {
			name = "default"
		}
		return singleTreeSitterSymbol(filePath, language, content, lines, container, name, KindClass, extractTSLeadingDoc)
	case "interface_declaration":
		return singleTreeSitterSymbol(filePath, language, content, lines, container, treeSitterNodeName(decl, content), KindInterface, extractTSLeadingDoc)
	case "type_alias_declaration", "enum_declaration":
		return singleTreeSitterSymbol(filePath, language, content, lines, container, treeSitterNodeName(decl, content), KindType, extractTSLeadingDoc)
	case "lexical_declaration", "variable_declaration":
		return tsVariableSymbols(filePath, language, content, lines, decl, container, defaultExport)
	default:
		return nil
	}
}

func unwrapTSDeclarationNode(node *sitter.Node, content []byte) (decl *sitter.Node, container *sitter.Node, defaultExport bool) {
	if node == nil {
		return nil, nil, false
	}
	if node.Kind() != "export_statement" {
		return node, node, false
	}
	defaultExport = strings.Contains(treeSitterNodeText(node, content), "default")
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		c := child
		return &c, node, defaultExport
	}
	return nil, nil, defaultExport
}

func singleTreeSitterSymbol(
	filePath, language string,
	content []byte,
	lines []string,
	rangeNode *sitter.Node,
	name string,
	kind Kind,
	docExtractor func([]string, int) string,
) []Symbol {
	sym, ok := buildTreeSitterSymbol(filePath, language, content, lines, rangeNode, name, kind, docExtractor)
	if !ok {
		return nil
	}
	return []Symbol{sym}
}

func tsVariableSymbols(filePath, language string, content []byte, lines []string, declNode, container *sitter.Node, defaultExport bool) []Symbol {
	kind := KindVariable
	declText := strings.TrimSpace(treeSitterNodeText(declNode, content))
	if strings.HasPrefix(declText, "const ") {
		kind = KindConstant
	}

	cursor := declNode.Walk()
	children := declNode.NamedChildren(cursor)
	symbols := make([]Symbol, 0, len(children))
	for _, child := range children {
		if child.Kind() != "variable_declarator" {
			continue
		}
		c := child
		rangeNode := &c
		if len(children) == 1 && container != nil {
			rangeNode = container
		}
		name := treeSitterNodeName(&c, content)
		if name == "" && defaultExport {
			name = "default"
		}
		if sym, ok := buildTreeSitterSymbol(filePath, language, content, lines, rangeNode, name, kind, extractTSLeadingDoc); ok {
			symbols = append(symbols, sym)
		}
	}
	return symbols
}

func buildPythonTreeSitterSymbol(filePath string, content []byte, lines []string, node *sitter.Node, kind Kind) (Symbol, bool) {
	return buildTreeSitterSymbol(filePath, "python", content, lines, node, treeSitterNodeName(node, content), kind, extractPythonDocstring)
}

func buildTreeSitterSymbol(
	filePath, language string,
	content []byte,
	lines []string,
	node *sitter.Node,
	name string,
	kind Kind,
	docExtractor func([]string, int) string,
) (Symbol, bool) {
	if node == nil || strings.TrimSpace(name) == "" {
		return Symbol{}, false
	}

	startByte, endByte := treeSitterByteRange(node, len(content))
	body := content[startByte:endByte]
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1
	signature := treeSitterNodeSignature(node, content)
	doc := ""
	if docExtractor != nil {
		doc = docExtractor(lines, max(startLine-1, 0))
	}

	return Symbol{
		ID:            ID(filePath, name),
		FilePath:      filePath,
		Name:          name,
		Language:      language,
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

func treeSitterNodeName(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return strings.TrimSpace(treeSitterNodeText(nameNode, content))
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		switch child.Kind() {
		case "identifier", "type_identifier", "property_identifier":
			return strings.TrimSpace(treeSitterNodeText(&child, content))
		}
	}
	return ""
}

func treeSitterNodeSignature(node *sitter.Node, content []byte) string {
	text := strings.TrimSpace(treeSitterNodeText(node, content))
	if text == "" {
		return ""
	}
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		return strings.TrimSpace(text[:idx])
	}
	return text
}

func treeSitterNodeText(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	startByte, endByte := treeSitterByteRange(node, len(content))
	return string(content[startByte:endByte])
}

func treeSitterByteRange(node *sitter.Node, limit int) (int, int) {
	startByte := int(node.StartByte())
	endByte := int(node.EndByte())
	if startByte < 0 {
		startByte = 0
	}
	if endByte < startByte {
		endByte = startByte
	}
	if startByte > limit {
		startByte = limit
	}
	if endByte > limit {
		endByte = limit
	}
	return startByte, endByte
}

func extractSymbolBodyBytes(symbol Symbol, content []byte) ([]byte, bool) {
	if symbol.StartByte < 0 || symbol.EndByte > len(content) || symbol.StartByte >= symbol.EndByte {
		return nil, false
	}
	return content[symbol.StartByte:symbol.EndByte], true
}

func collectTreeSitterCallNames(root *sitter.Node, content []byte, mapper func(*sitter.Node, []byte) string) []string {
	if root == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, 16)
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Kind() {
		case "call_expression", "new_expression", "call":
			if name := strings.TrimSpace(mapper(node, content)); name != "" && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(root)
	if len(out) == 0 {
		return nil
	}
	if len(out) > 50 {
		return out[:50]
	}
	return out
}

func tsCallNameFromNode(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	if fn := node.ChildByFieldName("function"); fn != nil {
		return treeSitterCallableName(fn, content)
	}
	if ctor := node.ChildByFieldName("constructor"); ctor != nil {
		return treeSitterCallableName(ctor, content)
	}
	return ""
}

func pythonCallNameFromNode(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	if fn := node.ChildByFieldName("function"); fn != nil {
		return treeSitterCallableName(fn, content)
	}
	return ""
}

func extractTopLevelRustSymbols(filePath string, content []byte, lines []string, node *sitter.Node) []Symbol {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "function_item":
		return singleTreeSitterSymbol(filePath, "rust", content, lines, node, treeSitterNodeName(node, content), KindFunction, extractRustLeadingDoc)
	case "struct_item":
		return singleTreeSitterSymbol(filePath, "rust", content, lines, node, treeSitterNodeName(node, content), KindStruct, extractRustLeadingDoc)
	case "enum_item":
		return singleTreeSitterSymbol(filePath, "rust", content, lines, node, treeSitterNodeName(node, content), KindType, extractRustLeadingDoc)
	case "trait_item":
		return singleTreeSitterSymbol(filePath, "rust", content, lines, node, treeSitterNodeName(node, content), KindInterface, extractRustLeadingDoc)
	case "type_item":
		return singleTreeSitterSymbol(filePath, "rust", content, lines, node, treeSitterNodeName(node, content), KindType, extractRustLeadingDoc)
	case "const_item":
		return singleTreeSitterSymbol(filePath, "rust", content, lines, node, treeSitterNodeName(node, content), KindConstant, extractRustLeadingDoc)
	case "static_item":
		return singleTreeSitterSymbol(filePath, "rust", content, lines, node, treeSitterNodeName(node, content), KindVariable, extractRustLeadingDoc)
	case "impl_item":
		return rustImplSymbols(filePath, content, lines, node)
	default:
		return nil
	}
}

func rustImplSymbols(filePath string, content []byte, lines []string, node *sitter.Node) []Symbol {
	if node == nil {
		return nil
	}
	implType := rustImplTypeName(node, content)
	if implType == "" {
		return nil
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	symbols := make([]Symbol, 0, len(children))
	for _, child := range children {
		if child.Kind() != "function_item" {
			continue
		}
		c := child
		name := treeSitterNodeName(&c, content)
		if strings.TrimSpace(name) == "" {
			continue
		}
		symbolName := implType + "." + name
		if sym, ok := buildTreeSitterSymbol(filePath, "rust", content, lines, &c, symbolName, KindMethod, extractRustLeadingDoc); ok {
			symbols = append(symbols, sym)
		}
	}
	return symbols
}

var rustImplHeaderPattern = regexp.MustCompile(`^impl(?:<[^>]*>\s*)?(?:.+\s+for\s+)?(.+)$`)

func rustImplTypeName(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	header := strings.TrimSpace(treeSitterNodeSignature(node, content))
	if idx := strings.Index(header, "{"); idx >= 0 {
		header = strings.TrimSpace(header[:idx])
	}
	matches := rustImplHeaderPattern.FindStringSubmatch(header)
	if len(matches) != 2 {
		return ""
	}
	return rustLastIdentifier(matches[1])
}

func collectRustTreeSitterCallNames(root *sitter.Node, content []byte) []string {
	if root == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, 16)
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Kind() {
		case "call_expression":
			if fn := node.ChildByFieldName("function"); fn != nil {
				if name := strings.TrimSpace(treeSitterCallableName(fn, content)); name != "" && !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
			}
		case "method_call_expression":
			if name := rustMethodCallName(node, content); name != "" && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		case "macro_invocation":
			if macro := node.ChildByFieldName("macro"); macro != nil {
				if name := strings.TrimSpace(treeSitterCallableName(macro, content)); name != "" && !seen[name] {
					seen[name] = true
					out = append(out, name)
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
	if len(out) == 0 {
		return nil
	}
	if len(out) > 50 {
		return out[:50]
	}
	return out
}

func rustMethodCallName(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	if method := node.ChildByFieldName("method"); method != nil {
		return strings.TrimSpace(treeSitterNodeText(method, content))
	}
	if name := node.ChildByFieldName("name"); name != nil {
		return strings.TrimSpace(treeSitterNodeText(name, content))
	}
	for i := int(node.NamedChildCount()) - 1; i >= 0; i-- {
		child := node.NamedChild(uint(i))
		if child == nil {
			continue
		}
		c := *child
		if c.Kind() == "identifier" || c.Kind() == "field_identifier" {
			return strings.TrimSpace(treeSitterNodeText(&c, content))
		}
	}
	return ""
}

func treeSitterCallableName(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier", "type_identifier", "property_identifier":
		return strings.TrimSpace(treeSitterNodeText(node, content))
	case "member_expression", "attribute":
		if prop := node.ChildByFieldName("property"); prop != nil {
			return strings.TrimSpace(treeSitterNodeText(prop, content))
		}
		if attr := node.ChildByFieldName("attribute"); attr != nil {
			return strings.TrimSpace(treeSitterNodeText(attr, content))
		}
	}
	for i := int(node.NamedChildCount()) - 1; i >= 0; i-- {
		child := node.NamedChild(uint(i))
		if child == nil {
			continue
		}
		if name := treeSitterCallableName(child, content); name != "" {
			return name
		}
	}
	return ""
}
