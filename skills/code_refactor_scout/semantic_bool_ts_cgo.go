//go:build cgo

package main

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	symindex "github.com/jkatigb/agentctl/internal/indexing/symbol"
)

func analyzeTypeScriptSemanticSimplifications(path, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	if len(symbols) == 0 {
		return nil
	}
	grammar := tsGrammarForPath(path)
	if grammar == nil {
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
		candidate := detectTypeScriptSemanticSimplification(grammar, body)
		if candidate == nil {
			continue
		}
		findings = append(findings, emitSemanticSimplificationFinding(candidate, relPath, sym.Name, sym.StartLine, lang))
	}
	return findings
}

func detectTypeScriptSemanticSimplification(grammar *sitter.Language, body []byte) *semanticSimplificationCandidate {
	tree, ok := parseTSRecoveryTree(grammar, body)
	if !ok {
		return nil
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return nil
	}
	if wrapper := detectTypeScriptBoolReturnWrapper(root, body); wrapper != nil {
		return wrapper
	}
	expr, originalExpr, lowerPatterns, lowerChanged := tsSingleReturnedBooleanExpr(root, body)
	if expr == nil {
		return nil
	}
	simplified, patterns, changed := simplifySemanticBoolExpr(expr)
	patterns = appendUniquePatternStrings(lowerPatterns, patterns...)
	if !changed && !lowerChanged {
		return nil
	}
	original := "return " + originalExpr
	simplifiedText := "return " + renderSemanticBoolExpr(simplified, tsSemanticBoolSyntax)
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

func detectTypeScriptBoolReturnWrapper(root *sitter.Node, content []byte) *semanticSimplificationCandidate {
	block := tsPrimaryStatementBlock(root)
	if block == nil {
		return nil
	}
	stmts := tsNamedChildren(block)
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
		condition := ifNode.ChildByFieldName("condition")
		consequence := ifNode.ChildByFieldName("consequence")
		alternative := ifNode.ChildByFieldName("alternative")
		bodyValue, bodyOK := tsSingleBlockBooleanReturnValue(consequence)
		elseValue, elseOK := tsSingleBlockBooleanReturnValue(alternative)
		if condition == nil || !bodyOK || !elseOK || bodyValue == elseValue {
			return nil
		}
		cond = condition
		invert = !bodyValue && elseValue
	case 2:
		ifNode := stmts[0]
		if ifNode.Kind() != "if_statement" || ifNode.ChildByFieldName("alternative") != nil {
			return nil
		}
		condition := ifNode.ChildByFieldName("condition")
		consequence := ifNode.ChildByFieldName("consequence")
		bodyValue, bodyOK := tsSingleBlockBooleanReturnValue(consequence)
		tailValue, tailOK := tsBooleanReturnValue(stmts[1])
		if condition == nil || !bodyOK || !tailOK || bodyValue == tailValue {
			return nil
		}
		cond = condition
		invert = !bodyValue && tailValue
	default:
		return nil
	}
	if cond == nil {
		return nil
	}
	expr, lowerPatterns, lowerChanged := lowerTypeScriptSemanticBoolExprDetailed(cond, content)
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
	original := strings.TrimSpace(tsNodeText(block, content))
	original = strings.Join(strings.Fields(original), " ")
	simplifiedText := "return " + renderSemanticBoolExpr(expr, tsSemanticBoolSyntax)
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

func tsSingleReturnedBooleanExpr(root *sitter.Node, content []byte) (*semanticBoolExpr, string, []string, bool) {
	block := tsPrimaryStatementBlock(root)
	if block != nil {
		stmts := tsNamedChildren(block)
		if len(stmts) != 1 || stmts[0].Kind() != "return_statement" {
			return nil, "", nil, false
		}
		value := tsReturnValueNode(stmts[0])
		if value == nil {
			return nil, "", nil, false
		}
		expr, patterns, changed := lowerTypeScriptSemanticBoolExprDetailed(value, content)
		original := strings.Join(strings.Fields(strings.TrimSpace(tsNodeText(value, content))), " ")
		return expr, original, patterns, changed
	}
	expr := tsPrimaryArrowExpression(root)
	if expr == nil {
		return nil, "", nil, false
	}
	lowered, patterns, changed := lowerTypeScriptSemanticBoolExprDetailed(expr, content)
	original := strings.Join(strings.Fields(strings.TrimSpace(tsNodeText(expr, content))), " ")
	return lowered, original, patterns, changed
}

func lowerTypeScriptSemanticBoolExprDetailed(node *sitter.Node, content []byte) (*semanticBoolExpr, []string, bool) {
	node = tsUnwrapParens(node)
	if node == nil {
		return nil, nil, false
	}
	switch node.Kind() {
	case "true":
		return semanticBoolLiteral(true), nil, false
	case "false":
		return semanticBoolLiteral(false), nil, false
	case "unary_expression":
		operator := strings.TrimSpace(tsUnaryOperator(node, content))
		argument := node.ChildByFieldName("argument")
		if operator == "!" && argument != nil {
			inner, patterns, changed := lowerTypeScriptSemanticBoolExprDetailed(argument, content)
			return semanticBoolNot(inner), patterns, changed
		}
	case "binary_expression":
		left := node.ChildByFieldName("left")
		right := node.ChildByFieldName("right")
		operator := strings.TrimSpace(tsBinaryOperator(node, content))
		switch operator {
		case "&&":
			leftExpr, leftPatterns, leftChanged := lowerTypeScriptSemanticBoolExprDetailed(left, content)
			rightExpr, rightPatterns, rightChanged := lowerTypeScriptSemanticBoolExprDetailed(right, content)
			patterns := appendUniquePatternStrings(nil, leftPatterns...)
			patterns = appendUniquePatternStrings(patterns, rightPatterns...)
			return semanticBoolAnd(leftExpr, rightExpr), patterns, leftChanged || rightChanged
		case "||":
			leftExpr, leftPatterns, leftChanged := lowerTypeScriptSemanticBoolExprDetailed(left, content)
			rightExpr, rightPatterns, rightChanged := lowerTypeScriptSemanticBoolExprDetailed(right, content)
			patterns := appendUniquePatternStrings(nil, leftPatterns...)
			patterns = appendUniquePatternStrings(patterns, rightPatterns...)
			return semanticBoolOr(leftExpr, rightExpr), patterns, leftChanged || rightChanged
		case "==", "===", "!=", "!==":
			if lit, ok := tsBoolLiteralValue(right); ok {
				base, patterns, changed := lowerTypeScriptSemanticBoolExprDetailed(left, content)
				if base == nil {
					return nil, patterns, changed
				}
				patterns = appendUniquePatternStrings(patterns, "boolean_literal_comparison")
				return lowerTypeScriptBoolLiteralComparisonFromBase(base, operator, lit), patterns, true
			}
			if lit, ok := tsBoolLiteralValue(left); ok {
				base, patterns, changed := lowerTypeScriptSemanticBoolExprDetailed(right, content)
				if base == nil {
					return nil, patterns, changed
				}
				patterns = appendUniquePatternStrings(patterns, "boolean_literal_comparison")
				return lowerTypeScriptBoolLiteralComparisonFromBase(base, operator, lit), patterns, true
			}
		}
	}
	return tsSemanticBoolAtom(node, content), nil, false
}

func lowerTypeScriptBoolLiteralComparisonFromBase(base *semanticBoolExpr, operator string, lit bool) *semanticBoolExpr {
	switch operator {
	case "==", "===":
		if lit {
			return base
		}
		return semanticBoolNot(base)
	case "!=", "!==":
		if lit {
			return semanticBoolNot(base)
		}
		return base
	default:
		return nil
	}
}

func tsSemanticBoolAtom(node *sitter.Node, content []byte) *semanticBoolExpr {
	render := strings.Join(strings.Fields(strings.TrimSpace(tsNodeText(node, content))), " ")
	if render == "" {
		return nil
	}
	return semanticBoolAtomExpr(render, tsNegatedRender(node, render, content), tsExprPure(node, content))
}

func tsNegatedRender(node *sitter.Node, render string, content []byte) string {
	node = tsUnwrapParens(node)
	if node == nil {
		return ""
	}
	if node.Kind() == "binary_expression" {
		left := node.ChildByFieldName("left")
		right := node.ChildByFieldName("right")
		operator := strings.TrimSpace(tsBinaryOperator(node, content))
		if inverse := tsInverseComparisonOperator(operator); inverse != "" && left != nil && right != nil {
			return strings.Join(strings.Fields(strings.TrimSpace(tsNodeText(left, content))), " ") + " " + inverse + " " + strings.Join(strings.Fields(strings.TrimSpace(tsNodeText(right, content))), " ")
		}
	}
	switch node.Kind() {
	case "identifier", "member_expression", "subscript_expression", "optional_chain":
		return "!" + render
	default:
		return "!(" + render + ")"
	}
}

func tsInverseComparisonOperator(op string) string {
	switch op {
	case "==":
		return "!="
	case "===":
		return "!=="
	case "!=":
		return "=="
	case "!==":
		return "==="
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

func tsExprPure(node *sitter.Node, content []byte) bool {
	node = tsUnwrapParens(node)
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "identifier", "this", "property_identifier", "true", "false", "null", "undefined", "number", "string":
		return true
	case "member_expression", "subscript_expression":
		object := node.ChildByFieldName("object")
		if object == nil {
			object = node.NamedChild(0)
		}
		return tsExprPure(object, content)
	case "unary_expression":
		return strings.TrimSpace(tsUnaryOperator(node, content)) == "!" && tsExprPure(node.ChildByFieldName("argument"), content)
	case "binary_expression":
		return tsExprPure(node.ChildByFieldName("left"), content) && tsExprPure(node.ChildByFieldName("right"), content)
	default:
		return false
	}
}

func tsPrimaryStatementBlock(root *sitter.Node) *sitter.Node {
	if root == nil {
		return nil
	}
	if root.Kind() == "statement_block" {
		return root
	}
	var found *sitter.Node
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil || found != nil {
			return
		}
		if node.Kind() == "statement_block" {
			found = node
			return
		}
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		for i := range children {
			child := children[i]
			walk(&child)
			if found != nil {
				return
			}
		}
	}
	walk(root)
	return found
}

func tsPrimaryArrowExpression(root *sitter.Node) *sitter.Node {
	if root == nil {
		return nil
	}
	var found *sitter.Node
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil || found != nil {
			return
		}
		if node.Kind() == "arrow_function" {
			body := node.ChildByFieldName("body")
			if body != nil && body.Kind() != "statement_block" {
				found = body
				return
			}
		}
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		for i := range children {
			child := children[i]
			walk(&child)
			if found != nil {
				return
			}
		}
	}
	walk(root)
	return found
}

func tsNamedChildren(node *sitter.Node) []*sitter.Node {
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

func tsSingleBlockBooleanReturnValue(node *sitter.Node) (bool, bool) {
	if node == nil || node.Kind() != "statement_block" {
		return false, false
	}
	stmts := tsNamedChildren(node)
	if len(stmts) != 1 {
		return false, false
	}
	return tsBooleanReturnValue(stmts[0])
}

func tsBooleanReturnValue(node *sitter.Node) (bool, bool) {
	if node == nil || node.Kind() != "return_statement" {
		return false, false
	}
	return tsBoolLiteralValue(tsReturnValueNode(node))
}

func tsBoolLiteralValue(node *sitter.Node) (bool, bool) {
	node = tsUnwrapParens(node)
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

func tsUnwrapParens(node *sitter.Node) *sitter.Node {
	for node != nil && node.Kind() == "parenthesized_expression" && node.NamedChildCount() > 0 {
		node = node.NamedChild(0)
	}
	return node
}

func tsReturnValueNode(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	if value := node.ChildByFieldName("value"); value != nil {
		return value
	}
	if node.NamedChildCount() > 0 {
		return node.NamedChild(0)
	}
	return nil
}

func tsUnaryOperator(node *sitter.Node, content []byte) string {
	if node == nil || node.Kind() != "unary_expression" {
		return ""
	}
	text := strings.TrimSpace(tsNodeText(node, content))
	switch {
	case strings.HasPrefix(text, "!"):
		return "!"
	case strings.HasPrefix(text, "~"):
		return "~"
	case strings.HasPrefix(text, "+"):
		return "+"
	case strings.HasPrefix(text, "-"):
		return "-"
	default:
		return ""
	}
}
