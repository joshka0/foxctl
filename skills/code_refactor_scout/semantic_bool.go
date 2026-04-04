package main

import (
	"go/ast"
	"go/token"
	"strings"
)

type semanticBoolSyntax struct {
	And string
	Or  string
	Not string
}

var (
	goSemanticBoolSyntax = semanticBoolSyntax{And: "&&", Or: "||", Not: "!"}
	tsSemanticBoolSyntax = semanticBoolSyntax{And: "&&", Or: "||", Not: "!"}
	pySemanticBoolSyntax = semanticBoolSyntax{And: "and", Or: "or", Not: "not "}
)

type semanticBoolAtom struct {
	Render        string
	NegatedRender string
	Pure          bool
}

type semanticBoolExpr struct {
	Kind  string
	Value bool
	Atom  *semanticBoolAtom
	Left  *semanticBoolExpr
	Right *semanticBoolExpr
	Inner *semanticBoolExpr
}

func semanticBoolLiteral(v bool) *semanticBoolExpr {
	return &semanticBoolExpr{Kind: "literal", Value: v}
}

func semanticBoolAtomExpr(render, negated string, pure bool) *semanticBoolExpr {
	render = strings.TrimSpace(render)
	negated = strings.TrimSpace(negated)
	if render == "" {
		return nil
	}
	return &semanticBoolExpr{
		Kind: "atom",
		Atom: &semanticBoolAtom{
			Render:        render,
			NegatedRender: negated,
			Pure:          pure,
		},
	}
}

func semanticBoolNot(inner *semanticBoolExpr) *semanticBoolExpr {
	if inner == nil {
		return nil
	}
	return &semanticBoolExpr{Kind: "not", Inner: inner}
}

func semanticBoolAnd(left, right *semanticBoolExpr) *semanticBoolExpr {
	if left == nil || right == nil {
		return nil
	}
	return &semanticBoolExpr{Kind: "and", Left: left, Right: right}
}

func semanticBoolOr(left, right *semanticBoolExpr) *semanticBoolExpr {
	if left == nil || right == nil {
		return nil
	}
	return &semanticBoolExpr{Kind: "or", Left: left, Right: right}
}

func simplifySemanticBoolExpr(expr *semanticBoolExpr) (*semanticBoolExpr, []string, bool) {
	if expr == nil {
		return nil, nil, false
	}
	switch expr.Kind {
	case "literal", "atom":
		return expr, nil, false
	case "not":
		inner, patterns, changed := simplifySemanticBoolExpr(expr.Inner)
		if inner == nil {
			return expr, patterns, changed
		}
		if inner.Kind == "literal" {
			return semanticBoolLiteral(!inner.Value), appendUniquePatternStrings(patterns, "literal_negation"), true
		}
		if inner.Kind == "not" && inner.Inner != nil {
			return inner.Inner, appendUniquePatternStrings(patterns, "double_negation"), true
		}
		if changed {
			return semanticBoolNot(inner), patterns, true
		}
		return expr, patterns, false
	case "and", "or":
		left, leftPatterns, leftChanged := simplifySemanticBoolExpr(expr.Left)
		right, rightPatterns, rightChanged := simplifySemanticBoolExpr(expr.Right)
		patterns := appendUniquePatternStrings(nil, leftPatterns...)
		patterns = appendUniquePatternStrings(patterns, rightPatterns...)
		changed := leftChanged || rightChanged
		op := expr.Kind
		if left != nil && left.Kind == "literal" {
			if op == "and" {
				if left.Value {
					return right, appendUniquePatternStrings(patterns, "left_true_and"), true
				}
				return semanticBoolLiteral(false), appendUniquePatternStrings(patterns, "left_false_and"), true
			}
			if left.Value {
				return semanticBoolLiteral(true), appendUniquePatternStrings(patterns, "left_true_or"), true
			}
			return right, appendUniquePatternStrings(patterns, "left_false_or"), true
		}
		if right != nil && right.Kind == "literal" {
			if op == "and" {
				if right.Value {
					return left, appendUniquePatternStrings(patterns, "right_true_and"), true
				}
				if semanticBoolExprPure(left) {
					return semanticBoolLiteral(false), appendUniquePatternStrings(patterns, "right_false_and"), true
				}
			}
			if !right.Value {
				return left, appendUniquePatternStrings(patterns, "right_false_or"), true
			}
			if semanticBoolExprPure(left) {
				return semanticBoolLiteral(true), appendUniquePatternStrings(patterns, "right_true_or"), true
			}
		}
		if changed {
			if op == "and" {
				return semanticBoolAnd(left, right), patterns, true
			}
			return semanticBoolOr(left, right), patterns, true
		}
		return expr, patterns, false
	default:
		return expr, nil, false
	}
}

func semanticBoolExprPure(expr *semanticBoolExpr) bool {
	if expr == nil {
		return false
	}
	switch expr.Kind {
	case "literal":
		return true
	case "atom":
		return expr.Atom != nil && expr.Atom.Pure
	case "not":
		return semanticBoolExprPure(expr.Inner)
	case "and", "or":
		return semanticBoolExprPure(expr.Left) && semanticBoolExprPure(expr.Right)
	default:
		return false
	}
}

func renderSemanticBoolExpr(expr *semanticBoolExpr, syntax semanticBoolSyntax) string {
	return renderSemanticBoolExprPrec(expr, syntax, 0)
}

func renderSemanticBoolExprPrec(expr *semanticBoolExpr, syntax semanticBoolSyntax, parentPrec int) string {
	if expr == nil {
		return ""
	}
	switch expr.Kind {
	case "literal":
		if expr.Value {
			return "true"
		}
		return "false"
	case "atom":
		if expr.Atom == nil {
			return ""
		}
		return expr.Atom.Render
	case "not":
		if expr.Inner == nil {
			return ""
		}
		if expr.Inner.Kind == "atom" && expr.Inner.Atom != nil && strings.TrimSpace(expr.Inner.Atom.NegatedRender) != "" {
			return expr.Inner.Atom.NegatedRender
		}
		inner := renderSemanticBoolExprPrec(expr.Inner, syntax, 3)
		if inner == "" {
			return ""
		}
		if semanticBoolPrecedence(expr.Inner.Kind) < 3 {
			inner = "(" + inner + ")"
		}
		return syntax.Not + inner
	case "and", "or":
		left := renderSemanticBoolExprPrec(expr.Left, syntax, semanticBoolPrecedence(expr.Kind))
		right := renderSemanticBoolExprPrec(expr.Right, syntax, semanticBoolPrecedence(expr.Kind))
		if left == "" || right == "" {
			return ""
		}
		op := syntax.And
		if expr.Kind == "or" {
			op = syntax.Or
		}
		out := left + " " + op + " " + right
		if semanticBoolPrecedence(expr.Kind) < parentPrec {
			return "(" + out + ")"
		}
		return out
	default:
		return ""
	}
}

func semanticBoolPrecedence(kind string) int {
	switch kind {
	case "or":
		return 1
	case "and":
		return 2
	case "not":
		return 3
	default:
		return 4
	}
}

func semanticSourceTokenCount(value string) int {
	replacer := strings.NewReplacer(
		"(", " ",
		")", " ",
		"{", " ",
		"}", " ",
		",", " ",
		";", " ",
		"!", " ! ",
		"&", " & ",
		"|", " | ",
		"=", " = ",
		"<", " < ",
		">", " > ",
	)
	return len(strings.Fields(replacer.Replace(value)))
}

func lowerGoSemanticBoolExpr(expr ast.Expr) *semanticBoolExpr {
	lowered, _, _ := lowerGoSemanticBoolExprDetailed(expr)
	return lowered
}

func lowerGoSemanticBoolExprDetailed(expr ast.Expr) (*semanticBoolExpr, []string, bool) {
	expr = goUnwrapParenExpr(expr)
	switch node := expr.(type) {
	case nil:
		return nil, nil, false
	case *ast.Ident:
		switch node.Name {
		case "true":
			return semanticBoolLiteral(true), nil, false
		case "false":
			return semanticBoolLiteral(false), nil, false
		default:
			render := renderGoNode(node)
			return semanticBoolAtomExpr(render, "!"+render, true), nil, false
		}
	case *ast.ParenExpr:
		return lowerGoSemanticBoolExprDetailed(node.X)
	case *ast.UnaryExpr:
		if node.Op != token.NOT {
			return goSemanticBoolAtom(node), nil, false
		}
		inner, patterns, changed := lowerGoSemanticBoolExprDetailed(node.X)
		return semanticBoolNot(inner), patterns, changed
	case *ast.BinaryExpr:
		switch node.Op {
		case token.LAND:
			left, leftPatterns, leftChanged := lowerGoSemanticBoolExprDetailed(node.X)
			right, rightPatterns, rightChanged := lowerGoSemanticBoolExprDetailed(node.Y)
			patterns := appendUniquePatternStrings(nil, leftPatterns...)
			patterns = appendUniquePatternStrings(patterns, rightPatterns...)
			return semanticBoolAnd(left, right), patterns, leftChanged || rightChanged
		case token.LOR:
			left, leftPatterns, leftChanged := lowerGoSemanticBoolExprDetailed(node.X)
			right, rightPatterns, rightChanged := lowerGoSemanticBoolExprDetailed(node.Y)
			patterns := appendUniquePatternStrings(nil, leftPatterns...)
			patterns = appendUniquePatternStrings(patterns, rightPatterns...)
			return semanticBoolOr(left, right), patterns, leftChanged || rightChanged
		case token.EQL, token.NEQ:
			if lit, ok := goBoolLiteralValue(node.Y); ok {
				base, patterns, changed := lowerGoSemanticBoolExprDetailed(node.X)
				if base == nil {
					return nil, patterns, changed
				}
				patterns = appendUniquePatternStrings(patterns, "boolean_literal_comparison")
				return lowerGoBoolLiteralComparisonFromBase(base, node.Op, lit), patterns, true
			}
			if lit, ok := goBoolLiteralValue(node.X); ok {
				base, patterns, changed := lowerGoSemanticBoolExprDetailed(node.Y)
				if base == nil {
					return nil, patterns, changed
				}
				patterns = appendUniquePatternStrings(patterns, "boolean_literal_comparison")
				return lowerGoBoolLiteralComparisonFromBase(base, node.Op, lit), patterns, true
			}
		}
		return goSemanticBoolAtom(node), nil, false
	default:
		return goSemanticBoolAtom(node), nil, false
	}
}

func lowerGoBoolLiteralComparisonFromBase(base *semanticBoolExpr, op token.Token, lit bool) *semanticBoolExpr {
	switch op {
	case token.EQL:
		if lit {
			return base
		}
		return semanticBoolNot(base)
	case token.NEQ:
		if lit {
			return semanticBoolNot(base)
		}
		return base
	default:
		return nil
	}
}

func goSemanticBoolAtom(expr ast.Expr) *semanticBoolExpr {
	render := strings.TrimSpace(renderGoNode(expr))
	if render == "" {
		return nil
	}
	return semanticBoolAtomExpr(render, goNegatedRender(expr, render), goExprPure(expr))
}

func goNegatedRender(expr ast.Expr, render string) string {
	expr = goUnwrapParenExpr(expr)
	if binary, ok := expr.(*ast.BinaryExpr); ok {
		if inverse, ok := goInverseComparisonToken(binary.Op); ok {
			return strings.TrimSpace(renderGoNode(&ast.BinaryExpr{X: binary.X, Op: inverse, Y: binary.Y}))
		}
	}
	switch expr.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
		return "!" + render
	default:
		return "!(" + render + ")"
	}
}

func goInverseComparisonToken(op token.Token) (token.Token, bool) {
	switch op {
	case token.EQL:
		return token.NEQ, true
	case token.NEQ:
		return token.EQL, true
	case token.GTR:
		return token.LEQ, true
	case token.GEQ:
		return token.LSS, true
	case token.LSS:
		return token.GEQ, true
	case token.LEQ:
		return token.GTR, true
	default:
		return token.ILLEGAL, false
	}
}

func goExprPure(expr ast.Expr) bool {
	expr = goUnwrapParenExpr(expr)
	switch node := expr.(type) {
	case nil:
		return false
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.SelectorExpr:
		return goExprPure(node.X)
	case *ast.IndexExpr:
		return goExprPure(node.X) && goExprPure(node.Index)
	case *ast.IndexListExpr:
		if !goExprPure(node.X) {
			return false
		}
		for _, index := range node.Indices {
			if !goExprPure(index) {
				return false
			}
		}
		return true
	case *ast.ParenExpr:
		return goExprPure(node.X)
	case *ast.UnaryExpr:
		return node.Op == token.NOT && goExprPure(node.X)
	case *ast.BinaryExpr:
		return goExprPure(node.X) && goExprPure(node.Y)
	case *ast.CompositeLit:
		for _, elt := range node.Elts {
			switch e := elt.(type) {
			case ast.Expr:
				if !goExprPure(e) {
					return false
				}
			case *ast.KeyValueExpr:
				if !goExprPure(e.Key) || !goExprPure(e.Value) {
					return false
				}
			default:
				return false
			}
		}
		return true
	default:
		return false
	}
}

func goUnwrapParenExpr(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func goBoolLiteralValue(expr ast.Expr) (bool, bool) {
	ident, ok := goUnwrapParenExpr(expr).(*ast.Ident)
	if !ok {
		return false, false
	}
	switch ident.Name {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func goSingleBlockBooleanReturnValue(block *ast.BlockStmt) (bool, bool) {
	if block == nil || len(block.List) != 1 {
		return false, false
	}
	return goBooleanReturnValue(block.List[0])
}

func goBooleanReturnValue(stmt ast.Stmt) (bool, bool) {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false, false
	}
	return goBoolLiteralValue(ret.Results[0])
}
