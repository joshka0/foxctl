//go:build cgo

package main

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	py "github.com/tree-sitter/tree-sitter-python/bindings/go"

	symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

func analyzePythonSemanticSimplifications(_ string, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	if len(symbols) == 0 {
		return nil
	}
	findings := make([]finding, 0, 4)
	for _, sym := range symbols {
		if !supportsObservedFunctionSignals(sym, lang, content) {
			continue
		}
		body, ok := extractObservedSymbolBytes(sym, content)
		if !ok {
			continue
		}
		candidate := detectPythonSemanticSimplification(body)
		if candidate == nil {
			continue
		}
		findings = append(findings, emitSemanticSimplificationFinding(candidate, relPath, sym.Name, sym.StartLine, lang))
	}
	return findings
}

func detectPythonSemanticSimplification(body []byte) *semanticSimplificationCandidate {
	tree, ok := parsePythonSemanticTree(body)
	if !ok {
		return nil
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return nil
	}
	if wrapper := detectPythonBoolReturnWrapper(root, body); wrapper != nil {
		return wrapper
	}
	expr, originalExpr, lowerPatterns, lowerChanged := pythonSingleReturnedBooleanExpr(root, body)
	if expr == nil {
		return nil
	}
	simplified, patterns, changed := simplifySemanticBoolExpr(expr)
	patterns = appendUniquePatternStrings(lowerPatterns, patterns...)
	if !changed && !lowerChanged {
		return nil
	}
	original := "return " + originalExpr
	simplifiedText := "return " + renderSemanticBoolExpr(simplified, pySemanticBoolSyntax)
	if original == simplifiedText {
		return nil
	}
	return &semanticSimplificationCandidate{
		Kind:               "boolean_expr_identity",
		PatternIDs:         appendUniquePatternStrings(nil, patterns...),
		OriginalForm:       original,
		SimplifiedForm:     simplifiedText,
		OriginalTokenCount: semanticSourceTokenCount(original),
		SimplifiedTokens:   semanticSourceTokenCount(simplifiedText),
	}
}

func parsePythonSemanticTree(content []byte) (*sitter.Tree, bool) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(py.Language())); err != nil {
		return nil, false
	}
	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil, false
	}
	return tree, true
}

func detectPythonBoolReturnWrapper(root *sitter.Node, content []byte) *semanticSimplificationCandidate {
	block := pythonPrimaryFunctionBlock(root)
	if block == nil {
		return nil
	}
	stmts := pythonNamedChildren(block)
	if len(stmts) == 0 {
		return nil
	}
	var (
		cond      *sitter.Node
		invert    bool
		patternID string
	)
	switch len(stmts) {
	case 1:
		ifNode := stmts[0]
		if ifNode.Kind() != "if_statement" {
			return nil
		}
		condition, consequence, elseClause := pythonIfParts(ifNode)
		bodyValue, bodyOK := pythonSingleBlockBooleanReturnValue(consequence)
		elseValue, elseOK := pythonElseBooleanReturnValue(elseClause)
		if condition == nil || !bodyOK || !elseOK || bodyValue == elseValue {
			return nil
		}
		cond = condition
		invert = !bodyValue && elseValue
	case 2:
		ifNode := stmts[0]
		if ifNode.Kind() != "if_statement" {
			return nil
		}
		condition, consequence, elseClause := pythonIfParts(ifNode)
		if elseClause != nil {
			return nil
		}
		bodyValue, bodyOK := pythonSingleBlockBooleanReturnValue(consequence)
		tailValue, tailOK := pythonBooleanReturnValue(stmts[1])
		if condition == nil || !bodyOK || !tailOK || bodyValue == tailValue {
			return nil
		}
		cond = condition
		invert = !bodyValue && tailValue
	default:
		return nil
	}
	expr, lowerPatterns, lowerChanged := lowerPythonSemanticBoolExprDetailed(cond, content)
	if expr == nil {
		return nil
	}
	patternID = "boolean_return_wrapper"
	if invert {
		patternID = "inverted_boolean_return_wrapper"
		expr = semanticBoolNot(expr)
	}
	patterns := appendUniquePatternStrings([]string{patternID}, lowerPatterns...)
	if simplified, extra, changed := simplifySemanticBoolExpr(expr); changed {
		expr = simplified
		patterns = append(patterns, extra...)
	} else if !lowerChanged && !invert {
		return nil
	}
	original := strings.Join(strings.Fields(strings.TrimSpace(pythonNodeText(block, content))), " ")
	simplifiedText := "return " + renderSemanticBoolExpr(expr, pySemanticBoolSyntax)
	if original == "" || original == simplifiedText {
		return nil
	}
	return &semanticSimplificationCandidate{
		Kind:               "boolean_return_wrapper",
		PatternIDs:         appendUniquePatternStrings(nil, patterns...),
		OriginalForm:       original,
		SimplifiedForm:     simplifiedText,
		OriginalTokenCount: semanticSourceTokenCount(original),
		SimplifiedTokens:   semanticSourceTokenCount(simplifiedText),
	}
}

func pythonSingleReturnedBooleanExpr(root *sitter.Node, content []byte) (*semanticBoolExpr, string, []string, bool) {
	block := pythonPrimaryFunctionBlock(root)
	if block == nil {
		return nil, "", nil, false
	}
	stmts := pythonNamedChildren(block)
	if len(stmts) != 1 || stmts[0].Kind() != "return_statement" {
		return nil, "", nil, false
	}
	value := pythonReturnValueNode(stmts[0])
	if value == nil {
		return nil, "", nil, false
	}
	expr, patterns, changed := lowerPythonSemanticBoolExprDetailed(value, content)
	original := strings.Join(strings.Fields(strings.TrimSpace(pythonNodeText(value, content))), " ")
	return expr, original, patterns, changed
}

func lowerPythonSemanticBoolExprDetailed(node *sitter.Node, content []byte) (*semanticBoolExpr, []string, bool) {
	node = pythonUnwrapParens(node)
	if node == nil {
		return nil, nil, false
	}
	switch node.Kind() {
	case "true":
		return semanticBoolLiteral(true), nil, false
	case "false":
		return semanticBoolLiteral(false), nil, false
	case "identifier":
		render := strings.Join(strings.Fields(strings.TrimSpace(pythonNodeText(node, content))), " ")
		return semanticBoolAtomExpr(render, "not "+render, true), nil, false
	case "not_operator":
		if node.NamedChildCount() == 0 {
			return nil, nil, false
		}
		inner, patterns, changed := lowerPythonSemanticBoolExprDetailed(node.NamedChild(0), content)
		return semanticBoolNot(inner), patterns, changed
	case "boolean_operator":
		left, right := pythonBinaryOperands(node)
		op := pythonBinaryOperator(node, left, right, content)
		leftExpr, leftPatterns, leftChanged := lowerPythonSemanticBoolExprDetailed(left, content)
		rightExpr, rightPatterns, rightChanged := lowerPythonSemanticBoolExprDetailed(right, content)
		patterns := appendUniquePatternStrings(nil, leftPatterns...)
		patterns = appendUniquePatternStrings(patterns, rightPatterns...)
		switch op {
		case "and":
			return semanticBoolAnd(leftExpr, rightExpr), patterns, leftChanged || rightChanged
		case "or":
			return semanticBoolOr(leftExpr, rightExpr), patterns, leftChanged || rightChanged
		default:
			return pythonSemanticBoolAtom(node, content), nil, false
		}
	case "comparison_operator":
		left, right := pythonBinaryOperands(node)
		op := pythonBinaryOperator(node, left, right, content)
		if lit, ok := pythonBoolLiteralValue(right); ok {
			base, patterns, changed := lowerPythonSemanticBoolExprDetailed(left, content)
			if base == nil {
				return nil, patterns, changed
			}
			patterns = appendUniquePatternStrings(patterns, "boolean_literal_comparison")
			return lowerPythonBoolLiteralComparisonFromBase(base, op, lit), patterns, true
		}
		if lit, ok := pythonBoolLiteralValue(left); ok {
			base, patterns, changed := lowerPythonSemanticBoolExprDetailed(right, content)
			if base == nil {
				return nil, patterns, changed
			}
			patterns = appendUniquePatternStrings(patterns, "boolean_literal_comparison")
			return lowerPythonBoolLiteralComparisonFromBase(base, op, lit), patterns, true
		}
		return pythonSemanticBoolAtom(node, content), nil, false
	case "parenthesized_expression":
		if node.NamedChildCount() == 1 {
			return lowerPythonSemanticBoolExprDetailed(node.NamedChild(0), content)
		}
	}
	return pythonSemanticBoolAtom(node, content), nil, false
}

func lowerPythonBoolLiteralComparisonFromBase(base *semanticBoolExpr, op string, lit bool) *semanticBoolExpr {
	switch op {
	case "==":
		if lit {
			return base
		}
		return semanticBoolNot(base)
	case "!=":
		if lit {
			return semanticBoolNot(base)
		}
		return base
	default:
		return nil
	}
}

func pythonSemanticBoolAtom(node *sitter.Node, content []byte) *semanticBoolExpr {
	render := strings.Join(strings.Fields(strings.TrimSpace(pythonNodeText(node, content))), " ")
	if render == "" {
		return nil
	}
	return semanticBoolAtomExpr(render, pythonNegatedRender(node, render, content), pythonExprPure(node, content))
}

func pythonNegatedRender(node *sitter.Node, render string, content []byte) string {
	node = pythonUnwrapParens(node)
	if node == nil {
		return ""
	}
	if node.Kind() == "comparison_operator" {
		left, right := pythonBinaryOperands(node)
		op := pythonBinaryOperator(node, left, right, content)
		if inverse := pythonInverseComparisonOperator(op); inverse != "" && left != nil && right != nil {
			return strings.Join(strings.Fields(strings.TrimSpace(pythonNodeText(left, content))), " ") + " " + inverse + " " + strings.Join(strings.Fields(strings.TrimSpace(pythonNodeText(right, content))), " ")
		}
	}
	return "not " + render
}

func pythonInverseComparisonOperator(op string) string {
	switch op {
	case "==":
		return "!="
	case "!=":
		return "=="
	case "is":
		return "is not"
	case "is not":
		return "is"
	case ">":
		return "<="
	case ">=":
		return "<"
	case "<":
		return ">="
	case "<=":
		return ">"
	default:
		return ""
	}
}

func pythonExprPure(node *sitter.Node, content []byte) bool {
	node = pythonUnwrapParens(node)
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "identifier", "true", "false", "none", "integer", "float", "string":
		return true
	case "attribute":
		if node.NamedChildCount() == 0 {
			return false
		}
		return pythonExprPure(node.NamedChild(0), content)
	case "subscript":
		if node.NamedChildCount() == 0 {
			return false
		}
		return pythonExprPure(node.NamedChild(0), content)
	case "not_operator":
		if node.NamedChildCount() == 0 {
			return false
		}
		return pythonExprPure(node.NamedChild(0), content)
	case "comparison_operator", "boolean_operator":
		left, right := pythonBinaryOperands(node)
		return pythonExprPure(left, content) && pythonExprPure(right, content)
	default:
		return false
	}
}

func pythonPrimaryFunctionBlock(root *sitter.Node) *sitter.Node {
	if root == nil {
		return nil
	}
	var found *sitter.Node
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil || found != nil {
			return
		}
		if node.Kind() == "function_definition" {
			cursor := node.Walk()
			for _, child := range node.NamedChildren(cursor) {
				if child.Kind() == "block" {
					c := child
					found = &c
					return
				}
			}
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
			if found != nil {
				return
			}
		}
	}
	walk(root)
	return found
}

func pythonNamedChildren(node *sitter.Node) []*sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	out := make([]*sitter.Node, 0, len(children))
	for i := range children {
		child := children[i]
		c := child
		out = append(out, &c)
	}
	return out
}

func pythonIfParts(node *sitter.Node) (condition, consequence, elseClause *sitter.Node) {
	if node == nil || node.Kind() != "if_statement" {
		return nil, nil, nil
	}
	children := pythonNamedChildren(node)
	if len(children) >= 1 {
		condition = children[0]
	}
	if len(children) >= 2 {
		consequence = children[1]
	}
	if len(children) >= 3 && children[2].Kind() == "else_clause" {
		elseClause = children[2]
	}
	return condition, consequence, elseClause
}

func pythonSingleBlockBooleanReturnValue(node *sitter.Node) (bool, bool) {
	if node == nil || node.Kind() != "block" {
		return false, false
	}
	stmts := pythonNamedChildren(node)
	if len(stmts) != 1 {
		return false, false
	}
	return pythonBooleanReturnValue(stmts[0])
}

func pythonElseBooleanReturnValue(node *sitter.Node) (bool, bool) {
	if node == nil || node.Kind() != "else_clause" {
		return false, false
	}
	children := pythonNamedChildren(node)
	if len(children) != 1 {
		return false, false
	}
	return pythonSingleBlockBooleanReturnValue(children[0])
}

func pythonBooleanReturnValue(node *sitter.Node) (bool, bool) {
	if node == nil || node.Kind() != "return_statement" {
		return false, false
	}
	return pythonBoolLiteralValue(pythonReturnValueNode(node))
}

func pythonReturnValueNode(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	if node.NamedChildCount() > 0 {
		return node.NamedChild(0)
	}
	return nil
}

func pythonBoolLiteralValue(node *sitter.Node) (bool, bool) {
	node = pythonUnwrapParens(node)
	if node == nil {
		return false, false
	}
	switch node.Kind() {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func pythonUnwrapParens(node *sitter.Node) *sitter.Node {
	for node != nil && node.Kind() == "parenthesized_expression" && node.NamedChildCount() > 0 {
		node = node.NamedChild(0)
	}
	return node
}

func pythonBinaryOperands(node *sitter.Node) (left, right *sitter.Node) {
	if node == nil {
		return nil, nil
	}
	children := pythonNamedChildren(node)
	if len(children) >= 1 {
		left = children[0]
	}
	if len(children) >= 2 {
		right = children[len(children)-1]
	}
	return left, right
}

func pythonBinaryOperator(node, left, right *sitter.Node, content []byte) string {
	if node == nil || left == nil || right == nil {
		return ""
	}
	start := int(left.EndByte())
	end := int(right.StartByte())
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > len(content) {
		end = len(content)
	}
	if start > len(content) {
		start = len(content)
	}
	return strings.Join(strings.Fields(strings.TrimSpace(string(content[start:end]))), " ")
}

func pythonNodeText(node *sitter.Node, content []byte) string {
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
	if end > len(content) {
		end = len(content)
	}
	if start > len(content) {
		start = len(content)
	}
	return string(content[start:end])
}
