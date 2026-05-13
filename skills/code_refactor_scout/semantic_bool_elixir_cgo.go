//go:build cgo

package main

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

var elixirSemanticBoolSyntax = semanticBoolSyntax{And: "and", Or: "or", Not: "not "}

func analyzeElixirSemanticSimplifications(_ string, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
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
		candidate := detectElixirSemanticSimplification(body)
		if candidate == nil {
			continue
		}
		findings = append(findings, emitSemanticSimplificationFinding(candidate, relPath, sym.Name, sym.StartLine, lang))
	}
	return findings
}

func detectElixirSemanticSimplification(body []byte) *semanticSimplificationCandidate {
	tree, ok := parseElixirSlopTree(body)
	if !ok {
		return nil
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return nil
	}
	if wrapper := detectElixirBoolReturnWrapper(root, body); wrapper != nil {
		return wrapper
	}
	expr, originalExpr, lowerPatterns, lowerChanged := elixirSingleBooleanResultExpr(root, body)
	if expr == nil {
		return nil
	}
	simplified, patterns, changed := simplifySemanticBoolExpr(expr)
	patterns = appendUniquePatternStrings(lowerPatterns, patterns...)
	if !changed && !lowerChanged {
		return nil
	}
	original := originalExpr
	simplifiedText := renderSemanticBoolExpr(simplified, elixirSemanticBoolSyntax)
	return buildSemanticSimplificationCandidate("boolean_expr_identity", patterns, original, simplifiedText)
}

func detectElixirBoolReturnWrapper(root *sitter.Node, content []byte) *semanticSimplificationCandidate {
	body := elixirPrimaryFunctionBody(root, content)
	if body == nil {
		return nil
	}
	children := elixirNamedChildren(body)
	if len(children) != 1 {
		return nil
	}
	ifCall := &children[0]
	if ifCall.Kind() != "call" || elixirCallTargetName(ifCall, content) != "if" {
		return nil
	}
	conditionNode := elixirPrimaryArgNode(elixirCallArgumentsLocal(ifCall))
	doBlock := elixirCallDoBlock(ifCall)
	if conditionNode == nil || doBlock == nil {
		return nil
	}
	thenExpr, elseExpr, ok := elixirIfDoBlockBooleans(doBlock, content)
	if !ok || thenExpr == elseExpr {
		return nil
	}
	expr, lowerPatterns, lowerChanged := lowerElixirSemanticBoolExprDetailed(conditionNode, content)
	if expr == nil {
		return nil
	}
	patternID := "boolean_return_wrapper"
	if !thenExpr && elseExpr {
		patternID = "inverted_boolean_return_wrapper"
	}
	expr, patterns, ok := finalizeSemanticBoolWrapperExpr(expr, patternID, lowerPatterns, lowerChanged, patternID == "inverted_boolean_return_wrapper")
	if !ok {
		return nil
	}
	original := strings.Join(strings.Fields(strings.TrimSpace(elixirNodeText(ifCall, content))), " ")
	simplifiedText := renderSemanticBoolExpr(expr, elixirSemanticBoolSyntax)
	return buildSemanticSimplificationCandidate("boolean_return_wrapper", patterns, original, simplifiedText)
}

func elixirSingleBooleanResultExpr(root *sitter.Node, content []byte) (*semanticBoolExpr, string, []string, bool) {
	body := elixirPrimaryFunctionBody(root, content)
	if body == nil {
		return nil, "", nil, false
	}
	children := elixirNamedChildren(body)
	if len(children) != 1 {
		return nil, "", nil, false
	}
	exprNode := &children[0]
	expr, patterns, changed := lowerElixirSemanticBoolExprDetailed(exprNode, content)
	original := strings.Join(strings.Fields(strings.TrimSpace(elixirNodeText(exprNode, content))), " ")
	return expr, original, patterns, changed
}

func lowerElixirSemanticBoolExprDetailed(node *sitter.Node, content []byte) (*semanticBoolExpr, []string, bool) {
	node = elixirUnwrapParens(node)
	if node == nil {
		return nil, nil, false
	}
	switch node.Kind() {
	case "boolean":
		if lit, ok := elixirBoolLiteralValue(node, content); ok {
			return semanticBoolLiteral(lit), nil, false
		}
	case "identifier":
		render := strings.Join(strings.Fields(strings.TrimSpace(elixirNodeText(node, content))), " ")
		return semanticBoolAtomExpr(render, "not "+render, true), nil, false
	case "unary_operator":
		if node.NamedChildCount() == 0 {
			return nil, nil, false
		}
		operand := node.NamedChild(0)
		op := strings.TrimSpace(elixirUnaryOperator(node, operand, content))
		if op == "not" || op == "!" {
			inner, patterns, changed := lowerElixirSemanticBoolExprDetailed(operand, content)
			return semanticBoolNot(inner), patterns, changed
		}
	case "binary_operator":
		children := elixirNamedChildren(node)
		if len(children) < 2 {
			return nil, nil, false
		}
		left := &children[0]
		right := &children[len(children)-1]
		op := strings.TrimSpace(elixirBinaryOperator(node, left, right, content))
		switch op {
		case "and", "&&":
			leftExpr, leftPatterns, leftChanged := lowerElixirSemanticBoolExprDetailed(left, content)
			rightExpr, rightPatterns, rightChanged := lowerElixirSemanticBoolExprDetailed(right, content)
			return semanticBoolBinaryFromChildren("and", leftExpr, leftPatterns, leftChanged, rightExpr, rightPatterns, rightChanged)
		case "or", "||":
			leftExpr, leftPatterns, leftChanged := lowerElixirSemanticBoolExprDetailed(left, content)
			rightExpr, rightPatterns, rightChanged := lowerElixirSemanticBoolExprDetailed(right, content)
			return semanticBoolBinaryFromChildren("or", leftExpr, leftPatterns, leftChanged, rightExpr, rightPatterns, rightChanged)
		case "==", "===", "!=", "!==":
			if lit, ok := elixirBoolLiteralValue(right, content); ok {
				base, patterns, changed := lowerElixirSemanticBoolExprDetailed(left, content)
				if base == nil {
					return nil, patterns, changed
				}
				patterns = appendUniquePatternStrings(patterns, "boolean_literal_comparison")
				return lowerElixirBoolLiteralComparisonFromBase(base, op, lit), patterns, true
			}
			if lit, ok := elixirBoolLiteralValue(left, content); ok {
				base, patterns, changed := lowerElixirSemanticBoolExprDetailed(right, content)
				if base == nil {
					return nil, patterns, changed
				}
				patterns = appendUniquePatternStrings(patterns, "boolean_literal_comparison")
				return lowerElixirBoolLiteralComparisonFromBase(base, op, lit), patterns, true
			}
		}
	}
	return elixirSemanticBoolAtom(node, content), nil, false
}

func lowerElixirBoolLiteralComparisonFromBase(base *semanticBoolExpr, op string, lit bool) *semanticBoolExpr {
	equality, ok := semanticBoolStringComparisonIsEquality(op)
	if !ok {
		return nil
	}
	return lowerSemanticBoolLiteralComparisonFromEquality(base, lit, equality)
}

func elixirSemanticBoolAtom(node *sitter.Node, content []byte) *semanticBoolExpr {
	render := strings.Join(strings.Fields(strings.TrimSpace(elixirNodeText(node, content))), " ")
	if render == "" {
		return nil
	}
	return semanticBoolAtomExpr(render, elixirNegatedRender(node, render, content), elixirExprPure(node, content))
}

func elixirNegatedRender(node *sitter.Node, render string, content []byte) string {
	node = elixirUnwrapParens(node)
	if node == nil {
		return ""
	}
	if node.Kind() == "binary_operator" {
		children := elixirNamedChildren(node)
		if len(children) >= 2 {
			left := &children[0]
			right := &children[len(children)-1]
			op := strings.TrimSpace(elixirBinaryOperator(node, left, right, content))
			if inverse := elixirInverseComparisonOperator(op); inverse != "" {
				return strings.Join(strings.Fields(strings.TrimSpace(elixirNodeText(left, content))), " ") + " " + inverse + " " + strings.Join(strings.Fields(strings.TrimSpace(elixirNodeText(right, content))), " ")
			}
		}
	}
	return "not " + render
}

func elixirInverseComparisonOperator(op string) string {
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

func elixirExprPure(node *sitter.Node, content []byte) bool {
	node = elixirUnwrapParens(node)
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "identifier", "boolean", "nil", "integer", "float", "string", "atom":
		return true
	case "unary_operator":
		if node.NamedChildCount() == 0 {
			return false
		}
		operand := node.NamedChild(0)
		op := strings.TrimSpace(elixirUnaryOperator(node, operand, content))
		return (op == "not" || op == "!") && elixirExprPure(operand, content)
	case "binary_operator":
		children := elixirNamedChildren(node)
		if len(children) < 2 {
			return false
		}
		left := &children[0]
		right := &children[len(children)-1]
		return elixirExprPure(left, content) && elixirExprPure(right, content)
	default:
		return false
	}
}

func elixirPrimaryFunctionBody(root *sitter.Node, content []byte) *sitter.Node {
	if root == nil {
		return nil
	}
	var found *sitter.Node
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil || found != nil {
			return
		}
		if node.Kind() == "call" {
			target := elixirCallTargetName(node, content)
			if target == "def" || target == "defp" {
				if body := elixirCallDoBlock(node); body != nil {
					found = body
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

func elixirPrimaryArgNode(args *sitter.Node) *sitter.Node {
	if args == nil {
		return nil
	}
	children := elixirNamedChildren(args)
	if len(children) == 0 {
		return nil
	}
	return &children[0]
}

func elixirIfDoBlockBooleans(doBlock *sitter.Node, content []byte) (bool, bool, bool) {
	if doBlock == nil {
		return false, false, false
	}
	children := elixirNamedChildren(doBlock)
	if len(children) < 2 {
		return false, false, false
	}
	thenValue, thenOK := elixirBoolLiteralValue(&children[0], content)
	if !thenOK {
		return false, false, false
	}
	for i := 1; i < len(children); i++ {
		if children[i].Kind() != "else_block" {
			continue
		}
		elseChildren := elixirNamedChildren(&children[i])
		if len(elseChildren) != 1 {
			return false, false, false
		}
		elseValue, elseOK := elixirBoolLiteralValue(&elseChildren[0], content)
		if !elseOK {
			return false, false, false
		}
		return thenValue, elseValue, true
	}
	return false, false, false
}

func elixirBoolLiteralValue(node *sitter.Node, content []byte) (bool, bool) {
	node = elixirUnwrapParens(node)
	if node == nil || node.Kind() != "boolean" {
		return false, false
	}
	switch strings.TrimSpace(elixirNodeText(node, content)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func elixirUnwrapParens(node *sitter.Node) *sitter.Node {
	for node != nil && node.Kind() == "parentheses_expression" && node.NamedChildCount() > 0 {
		node = node.NamedChild(0)
	}
	return node
}
