//go:build cgo

package main

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	ts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/hashutil"
	symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

func analyzeTypeScriptDuplicateRecoveryBlocks(path, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	return analyzeTSSymbolGroups(path, relPath, lang, content, symbols, collectTSDuplicateRecoveryGroups, buildTypeScriptDuplicateRecoveryFindings)
}

func analyzeTypeScriptDuplicatedErrorRemaps(path, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	return analyzeTSSymbolGroups(path, relPath, lang, content, symbols, collectTSDuplicatedErrorRemapGroups, buildTypeScriptDuplicatedErrorRemapFindings)
}

func analyzeTypeScriptRepeatedGuardLadders(path, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	return analyzeTSSymbolGroups(path, relPath, lang, content, symbols, collectTSRepeatedGuardGroups, buildTypeScriptRepeatedGuardFindings)
}

func analyzeTSSymbolGroups[T any](path, relPath, lang string, content []byte, symbols []symindex.Symbol, collect func(*sitter.Node, []byte) []T, build func(symindex.Symbol, string, string, []T) []finding) []finding {
	if len(symbols) == 0 {
		return nil
	}
	grammar := tsGrammarForPath(path)
	if grammar == nil {
		return nil
	}

	findings := make([]finding, 0, 4)
	for _, sym := range symbols {
		groups, ok := collectTSSymbolCandidates(grammar, sym, lang, content, collect)
		if !ok {
			continue
		}
		findings = append(findings, build(sym, relPath, lang, groups)...)
	}
	return findings
}

func collectTSSymbolCandidates[T any](grammar *sitter.Language, sym symindex.Symbol, lang string, content []byte, collect func(*sitter.Node, []byte) []T) ([]T, bool) {
	if !supportsObservedFunctionSignals(sym, lang, content) {
		return nil, false
	}
	body, ok := extractObservedSymbolBytes(sym, content)
	if !ok {
		return nil, false
	}
	tree, ok := parseTSRecoveryTree(grammar, body)
	if !ok {
		return nil, false
	}
	groups := collect(tree.RootNode(), body)
	tree.Close()
	if len(groups) == 0 {
		return nil, false
	}
	return groups, true
}

func buildTypeScriptDuplicateRecoveryFindings(sym symindex.Symbol, relPath, lang string, groups []tsDuplicateRecoveryGroup) []finding {
	findings := make([]finding, 0, len(groups))
	for _, group := range groups {
		absLines := absoluteTSLines(sym, group.Lines)
		score := scoreDuplicateRecoveryBlock(len(absLines), group.StatementCount)
		findings = append(findings, finding{
			RuleID:            "duplicate_recovery_block",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function repeats the same guarded recovery block",
			Detail:            sym.Name + " repeats a normalized guarded block " + itoa(len(absLines)) + " times, which is a strong signal that the recovery or fallback path wants one helper instead of copy-pasted branches.",
			SuggestedRefactor: "Extract the repeated guarded recovery/remap path into a local helper or small policy function, then keep each branch focused on the condition that differs.",
			File:              relPath,
			Line:              absLines[0],
			Symbol:            sym.Name,
			Language:          lang,
			Confidence:        "high",
			Signals:           []string{"tree_sitter", "normalized_recovery_block"},
			Evidence: map[string]any{
				"normalized_block_hash": hashShort(group.Fingerprint),
				"duplicate_count":       len(absLines),
				"duplicate_span_lines":  absLines,
				"statement_count":       group.StatementCount,
				"control_transfers":     group.ControlTransfers,
			},
		})
	}
	return findings
}

func buildTypeScriptDuplicatedErrorRemapFindings(sym symindex.Symbol, relPath, lang string, groups []tsDuplicatedErrorRemapGroup) []finding {
	findings := make([]finding, 0, len(groups))
	for _, group := range groups {
		absLines := absoluteTSLines(sym, group.Lines)
		score := scoreDuplicatedErrorRemap(len(absLines))
		findings = append(findings, finding{
			RuleID:            "duplicated_error_remap",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function repeats the same guarded error remap",
			Detail:            sym.Name + " repeats the same guarded error remap " + itoa(len(absLines)) + " times inside catch handling, which suggests one shared error translation helper should own the mapping.",
			SuggestedRefactor: "Extract the repeated error remap into one helper that translates the external error shape to the domain error once, then call it from each catch path.",
			File:              relPath,
			Line:              absLines[0],
			Symbol:            sym.Name,
			Language:          lang,
			Confidence:        "high",
			Signals:           []string{"tree_sitter", "normalized_error_remap"},
			Evidence: map[string]any{
				"normalized_condition_hash": hashShort(group.ConditionFingerprint),
				"normalized_remap_hash":     hashShort(group.RemapFingerprint),
				"duplicate_count":           len(absLines),
				"duplicate_span_lines":      absLines,
			},
		})
	}
	return findings
}

func buildTypeScriptRepeatedGuardFindings(sym symindex.Symbol, relPath, lang string, groups []tsRepeatedGuardGroup) []finding {
	findings := make([]finding, 0, len(groups))
	for _, group := range groups {
		absLines := absoluteTSLines(sym, group.Lines)
		score := scoreRepeatedGuardLadder(len(absLines))
		findings = append(findings, finding{
			RuleID:            "repeated_guard_ladder",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function repeats the same guard predicate",
			Detail:            sym.Name + " repeats the same guard predicate " + itoa(len(absLines)) + " times, which suggests one policy helper or state transition should own the repeated gate instead of branching on it in multiple places.",
			SuggestedRefactor: "Extract the repeated guard or the guarded action into one helper so the policy check is expressed once and reused consistently.",
			File:              relPath,
			Line:              absLines[0],
			Symbol:            sym.Name,
			Language:          lang,
			Confidence:        "high",
			Signals:           []string{"tree_sitter", "normalized_guard_predicate"},
			Evidence: map[string]any{
				"normalized_guard_hash": hashShort(group.Fingerprint),
				"duplicate_count":       len(absLines),
				"duplicate_span_lines":  absLines,
				"guard_preview":         group.Preview,
			},
		})
	}
	return findings
}

func absoluteTSLines(sym symindex.Symbol, relativeLines []int) []int {
	absLines := make([]int, 0, len(relativeLines))
	for _, line := range relativeLines {
		absLines = append(absLines, sym.StartLine+line-1)
	}
	return absLines
}

func tsGrammarForPath(path string) *sitter.Language {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx", ".jsx":
		return sitter.NewLanguage(ts.LanguageTSX())
	case ".ts", ".mts", ".cts", ".js", ".mjs", ".cjs":
		return sitter.NewLanguage(ts.LanguageTypescript())
	default:
		return nil
	}
}

func parseTSRecoveryTree(grammar *sitter.Language, content []byte) (*sitter.Tree, bool) {
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

type tsDuplicateRecoveryGroup struct {
	Fingerprint      string
	Lines            []int
	StatementCount   int
	ControlTransfers int
}

type tsDuplicatedErrorRemapGroup struct {
	Fingerprint          string
	ConditionFingerprint string
	RemapFingerprint     string
	Lines                []int
}

type tsRepeatedGuardGroup struct {
	Fingerprint string
	Preview     string
	Lines       []int
}

type tsLineGroup[T any] struct {
	Fingerprint string
	Lines       []int
	Value       T
}

func newTSLineGroup[T any](fingerprint string, lines []int, value T) *tsLineGroup[T] {
	groupLines := append([]int(nil), lines...)
	sort.Ints(groupLines)
	return &tsLineGroup[T]{
		Fingerprint: fingerprint,
		Lines:       groupLines,
		Value:       value,
	}
}

func appendTSGroupLine[T any](group *tsLineGroup[T], line int) {
	if group == nil {
		return
	}
	group.Lines = append(group.Lines, line)
	sort.Ints(group.Lines)
}

func sortedTSLineGroups[T any](groups map[string]*tsLineGroup[T], applyLines func(*T, []int), lines func(*T) []int, fingerprint func(*T) string) []T {
	out := make([]T, 0, len(groups))
	for _, group := range groups {
		if group == nil || len(group.Lines) < 2 {
			continue
		}
		sort.Ints(group.Lines)
		value := group.Value
		applyLines(&value, append([]int(nil), group.Lines...))
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		leftLines := lines(&out[i])
		rightLines := lines(&out[j])
		if leftLines[0] != rightLines[0] {
			return leftLines[0] < rightLines[0]
		}
		return fingerprint(&out[i]) < fingerprint(&out[j])
	})
	return out
}

func mergeTSDuplicateRecoveryStats(existing *tsDuplicateRecoveryGroup, candidate tsDuplicateRecoveryGroup) {
	if candidate.StatementCount > existing.StatementCount {
		existing.StatementCount = candidate.StatementCount
	}
	if candidate.ControlTransfers > existing.ControlTransfers {
		existing.ControlTransfers = candidate.ControlTransfers
	}
}

func collectTSDuplicateRecoveryGroups(root *sitter.Node, content []byte) []tsDuplicateRecoveryGroup {
	if root == nil {
		return nil
	}
	groups := make(map[string]*tsLineGroup[tsDuplicateRecoveryGroup])
	walkTSDuplicateCandidates(root, content, func(block *sitter.Node) {
		candidate := tsRecoveryCandidate(block, content)
		if candidate == nil {
			return
		}
		if group, ok := groups[candidate.Fingerprint]; ok {
			appendTSGroupLine(group, candidate.Lines[0])
			mergeTSDuplicateRecoveryStats(&group.Value, *candidate)
			return
		}
		groups[candidate.Fingerprint] = newTSLineGroup(candidate.Fingerprint, candidate.Lines, *candidate)
	})
	return sortedTSLineGroups(
		groups,
		func(group *tsDuplicateRecoveryGroup, lines []int) { group.Lines = lines },
		func(group *tsDuplicateRecoveryGroup) []int { return group.Lines },
		func(group *tsDuplicateRecoveryGroup) string { return group.Fingerprint },
	)
}

func collectTSDuplicatedErrorRemapGroups(root *sitter.Node, content []byte) []tsDuplicatedErrorRemapGroup {
	if root == nil {
		return nil
	}
	groups := make(map[string]*tsLineGroup[tsDuplicatedErrorRemapGroup])
	walkTSCatchClauses(root, func(catchClause *sitter.Node) {
		candidates := tsErrorRemapCandidates(catchClause, content)
		for _, candidate := range candidates {
			if group, ok := groups[candidate.Fingerprint]; ok {
				appendTSGroupLine(group, candidate.Lines[0])
				continue
			}
			groups[candidate.Fingerprint] = newTSLineGroup(candidate.Fingerprint, candidate.Lines, candidate)
		}
	})
	return sortedTSLineGroups(
		groups,
		func(group *tsDuplicatedErrorRemapGroup, lines []int) { group.Lines = lines },
		func(group *tsDuplicatedErrorRemapGroup) []int { return group.Lines },
		func(group *tsDuplicatedErrorRemapGroup) string { return group.Fingerprint },
	)
}

func collectTSRepeatedGuardGroups(root *sitter.Node, content []byte) []tsRepeatedGuardGroup {
	if root == nil {
		return nil
	}
	groups := make(map[string]*tsLineGroup[tsRepeatedGuardGroup])
	walkTSIfStatements(root, func(ifNode *sitter.Node) {
		if ifNode == nil || ifNode.Kind() != "if_statement" {
			return
		}
		condition := ifNode.ChildByFieldName("condition")
		if condition == nil {
			return
		}
		for _, atom := range tsGuardAtoms(condition, content) {
			if group, ok := groups[atom.Fingerprint]; ok {
				appendTSGroupLine(group, atom.Line)
				continue
			}
			copyAtom := atom
			groups[atom.Fingerprint] = newTSLineGroup(atom.Fingerprint, []int{atom.Line}, tsRepeatedGuardGroup{
				Fingerprint: copyAtom.Fingerprint,
				Preview:     copyAtom.Preview,
				Lines:       []int{copyAtom.Line},
			})
		}
	})
	return sortedTSLineGroups(
		groups,
		func(group *tsRepeatedGuardGroup, lines []int) { group.Lines = lines },
		func(group *tsRepeatedGuardGroup) []int { return group.Lines },
		func(group *tsRepeatedGuardGroup) string { return group.Fingerprint },
	)
}

func walkTSDuplicateCandidates(node *sitter.Node, content []byte, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	switch node.Kind() {
	case "if_statement":
		if consequence := node.ChildByFieldName("consequence"); consequence != nil && consequence.Kind() == "statement_block" {
			visit(consequence)
		}
		if alternative := node.ChildByFieldName("alternative"); alternative != nil && alternative.Kind() == "statement_block" {
			visit(alternative)
		}
	case "catch_clause":
		if body := node.ChildByFieldName("body"); body != nil && body.Kind() == "statement_block" {
			visit(body)
		}
	}

	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		c := child
		walkTSDuplicateCandidates(&c, content, visit)
	}
}

func walkTSCatchClauses(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	if node.Kind() == "catch_clause" {
		visit(node)
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		c := child
		walkTSCatchClauses(&c, visit)
	}
}

func walkTSIfStatements(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	if node.Kind() == "if_statement" {
		visit(node)
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		c := child
		walkTSIfStatements(&c, visit)
	}
}

func tsRecoveryCandidate(block *sitter.Node, content []byte) *tsDuplicateRecoveryGroup {
	if block == nil || block.Kind() != "statement_block" {
		return nil
	}
	statementCount := int(block.NamedChildCount())
	if statementCount < 2 {
		return nil
	}
	fingerprint := tsNodeFingerprint(block, content)
	if strings.TrimSpace(fingerprint) == "" {
		return nil
	}
	if len(strings.Fields(strings.NewReplacer("(", " ", ")", " ", "{", " ", "}", " ", ";", " ", ",", " ").Replace(fingerprint))) < 3 {
		return nil
	}
	controlTransfers := tsControlTransfers(block)
	if controlTransfers == 0 {
		return nil
	}
	return &tsDuplicateRecoveryGroup{
		Fingerprint:      fingerprint,
		Lines:            []int{int(block.StartPosition().Row) + 1},
		StatementCount:   statementCount,
		ControlTransfers: controlTransfers,
	}
}

func tsErrorRemapCandidates(catchClause *sitter.Node, content []byte) []tsDuplicatedErrorRemapGroup {
	if catchClause == nil {
		return nil
	}
	body := catchClause.ChildByFieldName("body")
	if body == nil || body.Kind() != "statement_block" {
		return nil
	}
	cursor := body.Walk()
	children := body.NamedChildren(cursor)
	out := make([]tsDuplicatedErrorRemapGroup, 0, len(children))
	for _, child := range children {
		c := child
		candidate, ok := tsErrorRemapCandidate(&c, content)
		if ok {
			out = append(out, candidate)
		}
	}
	return out
}

func tsErrorRemapCandidate(node *sitter.Node, content []byte) (tsDuplicatedErrorRemapGroup, bool) {
	if node == nil || node.Kind() != "if_statement" {
		return tsDuplicatedErrorRemapGroup{}, false
	}
	if alt := node.ChildByFieldName("alternative"); alt != nil {
		return tsDuplicatedErrorRemapGroup{}, false
	}
	condition := node.ChildByFieldName("condition")
	consequence := node.ChildByFieldName("consequence")
	if condition == nil || consequence == nil || consequence.Kind() != "statement_block" {
		return tsDuplicatedErrorRemapGroup{}, false
	}
	if int(consequence.NamedChildCount()) != 1 {
		return tsDuplicatedErrorRemapGroup{}, false
	}
	action := consequence.NamedChild(0)
	if action == nil {
		return tsDuplicatedErrorRemapGroup{}, false
	}
	switch action.Kind() {
	case "throw_statement", "return_statement":
	default:
		return tsDuplicatedErrorRemapGroup{}, false
	}
	conditionFingerprint := tsNodeFingerprint(condition, content)
	if strings.TrimSpace(conditionFingerprint) == "" || !tsLooksLikeErrorCondition(conditionFingerprint) {
		return tsDuplicatedErrorRemapGroup{}, false
	}
	remapFingerprint := tsNodeFingerprint(action, content)
	if !tsLooksLikeErrorRemap(remapFingerprint) {
		return tsDuplicatedErrorRemapGroup{}, false
	}
	return tsDuplicatedErrorRemapGroup{
		Fingerprint:          conditionFingerprint + "=>" + remapFingerprint,
		ConditionFingerprint: conditionFingerprint,
		RemapFingerprint:     remapFingerprint,
		Lines:                []int{int(node.StartPosition().Row) + 1},
	}, true
}

func tsControlTransfers(node *sitter.Node) int {
	if node == nil {
		return 0
	}
	count := 0
	var walk func(*sitter.Node)
	walk = func(current *sitter.Node) {
		if current == nil {
			return
		}
		switch current.Kind() {
		case "return_statement", "throw_statement", "break_statement", "continue_statement":
			count++
		}
		cursor := current.Walk()
		for _, child := range current.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(node)
	return count
}

func tsLooksLikeErrorCondition(fingerprint string) bool {
	if strings.TrimSpace(fingerprint) == "" {
		return false
	}
	if !strings.Contains(fingerprint, "binary_expression") && !strings.Contains(fingerprint, "instanceof_expression") && !strings.Contains(fingerprint, "member(") {
		return false
	}
	return strings.Contains(fingerprint, "property") || strings.Contains(fingerprint, "member(") || strings.Contains(fingerprint, "instanceof_expression")
}

func tsLooksLikeErrorRemap(fingerprint string) bool {
	if strings.TrimSpace(fingerprint) == "" {
		return false
	}
	if !strings.HasPrefix(fingerprint, "throw(") && !strings.HasPrefix(fingerprint, "return(") {
		return false
	}
	return strings.Contains(fingerprint, "call(") || strings.Contains(fingerprint, "new(")
}

type tsGuardAtom struct {
	Fingerprint string
	Preview     string
	Line        int
}

func tsGuardAtoms(node *sitter.Node, content []byte) []tsGuardAtom {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "parenthesized_expression":
		if node.NamedChildCount() == 1 {
			return tsGuardAtoms(node.NamedChild(0), content)
		}
	case "binary_expression":
		op := tsBinaryOperator(node, content)
		left := node.ChildByFieldName("left")
		right := node.ChildByFieldName("right")
		switch op {
		case "&&", "||":
			out := tsGuardAtoms(left, content)
			out = append(out, tsGuardAtoms(right, content)...)
			return out
		case "==", "===", "!=", "!==":
			if atom, ok := tsBuildGuardAtom(left, right, op, content, int(node.StartPosition().Row)+1); ok {
				return []tsGuardAtom{atom}
			}
		}
	}
	return nil
}

func tsBuildGuardAtom(left, right *sitter.Node, op string, content []byte, line int) (tsGuardAtom, bool) {
	leftFP, leftPreview, leftOK := tsGuardOperandFingerprint(left, content)
	rightFP, rightPreview, rightOK := tsGuardOperandFingerprint(right, content)
	if !leftOK || !rightOK {
		return tsGuardAtom{}, false
	}
	if !tsGuardOperandLooksLikeTarget(left) || !tsGuardOperandLooksLikeSentinel(right) {
		if !tsGuardOperandLooksLikeTarget(right) || !tsGuardOperandLooksLikeSentinel(left) {
			return tsGuardAtom{}, false
		}
		leftFP, rightFP = rightFP, leftFP
		leftPreview, rightPreview = rightPreview, leftPreview
	}
	return tsGuardAtom{
		Fingerprint: leftFP + op + rightFP,
		Preview:     leftPreview + " " + op + " " + rightPreview,
		Line:        line,
	}, true
}

func tsGuardOperandFingerprint(node *sitter.Node, content []byte) (string, string, bool) {
	if node == nil {
		return "", "", false
	}
	switch node.Kind() {
	case "identifier":
		text := strings.TrimSpace(tsNodeText(node, content))
		if text == "" {
			return "", "", false
		}
		return "ident:" + text, text, true
	case "property_identifier":
		text := strings.TrimSpace(tsNodeText(node, content))
		if text == "" {
			return "", "", false
		}
		return "prop:" + text, text, true
	case "member_expression":
		text := tsMemberChain(node, content)
		if text == "" {
			return "", "", false
		}
		return "member:" + text, text, true
	case "string", "number", "true", "false", "null", "undefined":
		text := strings.TrimSpace(tsNodeText(node, content))
		if text == "" {
			return "", "", false
		}
		return "lit:" + text, text, true
	case "parenthesized_expression":
		if node.NamedChildCount() == 1 {
			return tsGuardOperandFingerprint(node.NamedChild(0), content)
		}
	}
	return "", "", false
}

func tsGuardOperandLooksLikeTarget(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "identifier", "member_expression":
		return true
	case "parenthesized_expression":
		if node.NamedChildCount() == 1 {
			return tsGuardOperandLooksLikeTarget(node.NamedChild(0))
		}
	}
	return false
}

func tsGuardOperandLooksLikeSentinel(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "string", "number", "true", "false", "null", "undefined", "identifier":
		return true
	case "parenthesized_expression":
		if node.NamedChildCount() == 1 {
			return tsGuardOperandLooksLikeSentinel(node.NamedChild(0))
		}
	}
	return false
}

func tsBinaryOperator(node *sitter.Node, content []byte) string {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil || right == nil {
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
	return strings.TrimSpace(string(content[start:end]))
}

func tsMemberChain(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier", "property_identifier":
		return strings.TrimSpace(tsNodeText(node, content))
	case "member_expression":
		object := node.ChildByFieldName("object")
		property := node.ChildByFieldName("property")
		left := tsMemberChain(object, content)
		right := tsMemberChain(property, content)
		switch {
		case left != "" && right != "":
			return left + "." + right
		case right != "":
			return right
		default:
			return left
		}
	}
	return ""
}

func tsNodeFingerprint(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "statement_block":
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		parts := make([]string, 0, len(children))
		for _, child := range children {
			c := child
			part := tsNodeFingerprint(&c, content)
			if part != "" {
				parts = append(parts, part)
			}
		}
		return "block{" + strings.Join(parts, ";") + "}"
	case "expression_statement":
		if node.NamedChildCount() == 0 {
			return "expr"
		}
		child := node.NamedChild(0)
		return "expr(" + tsNodeFingerprint(child, content) + ")"
	case "return_statement":
		return "return(" + tsNamedChildrenFingerprint(node, content) + ")"
	case "throw_statement":
		return "throw(" + tsNamedChildrenFingerprint(node, content) + ")"
	case "await_expression":
		return "await(" + tsNamedChildrenFingerprint(node, content) + ")"
	case "call_expression":
		return "call(" + tsCallLikeFingerprint(node, "function", content) + ")"
	case "new_expression":
		return "new(" + tsCallLikeFingerprint(node, "constructor", content) + ")"
	case "member_expression":
		return "member(" + tsMemberPropertyName(node, content) + ")"
	case "assignment_expression":
		return "assign(" + tsNamedChildrenFingerprint(node, content) + ")"
	case "augmented_assignment_expression":
		return "assign(" + tsNamedChildrenFingerprint(node, content) + ")"
	case "binary_expression":
		return "binary_expression(" + tsNamedChildrenFingerprint(node, content) + ")"
	case "instanceof_expression":
		return "instanceof_expression(" + tsNamedChildrenFingerprint(node, content) + ")"
	case "lexical_declaration", "variable_declaration":
		return "decl(" + tsNamedChildrenFingerprint(node, content) + ")"
	case "variable_declarator":
		if value := node.ChildByFieldName("value"); value != nil {
			return "var(" + tsNodeFingerprint(value, content) + ")"
		}
		return "var"
	case "try_statement":
		body := tsNodeFingerprint(node.ChildByFieldName("body"), content)
		handler := tsNodeFingerprint(node.ChildByFieldName("handler"), content)
		finalizer := tsNodeFingerprint(node.ChildByFieldName("finalizer"), content)
		return "try{" + body + "}catch{" + handler + "}finally{" + finalizer + "}"
	case "catch_clause":
		if body := node.ChildByFieldName("body"); body != nil {
			return "catch{" + tsNodeFingerprint(body, content) + "}"
		}
		return "catch"
	case "if_statement":
		cons := tsNodeFingerprint(node.ChildByFieldName("consequence"), content)
		alt := tsNodeFingerprint(node.ChildByFieldName("alternative"), content)
		return "if{" + cons + "}else{" + alt + "}"
	case "parenthesized_expression":
		return tsNamedChildrenFingerprint(node, content)
	case "identifier", "this":
		return "ident"
	case "property_identifier":
		return strings.TrimSpace(tsNodeText(node, content))
	case "string", "string_fragment", "template_string", "template_substitution", "number", "true", "false", "null", "undefined":
		return "lit"
	}

	if node.NamedChildCount() == 0 {
		return node.Kind()
	}
	return node.Kind() + "(" + tsNamedChildrenFingerprint(node, content) + ")"
}

func tsNamedChildrenFingerprint(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	parts := make([]string, 0, len(children))
	for _, child := range children {
		c := child
		part := tsNodeFingerprint(&c, content)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ",")
}

func tsCallLikeFingerprint(node *sitter.Node, field string, content []byte) string {
	if node == nil {
		return ""
	}
	target := node.ChildByFieldName(field)
	if target == nil {
		return "call"
	}
	switch target.Kind() {
	case "identifier", "property_identifier":
		return strings.TrimSpace(tsNodeText(target, content))
	case "member_expression":
		return tsMemberPropertyName(target, content)
	default:
		return target.Kind()
	}
}

func tsMemberPropertyName(node *sitter.Node, content []byte) string {
	if node == nil {
		return "member"
	}
	if property := node.ChildByFieldName("property"); property != nil {
		text := strings.TrimSpace(tsNodeText(property, content))
		if text != "" {
			return text
		}
	}
	return "member"
}

func tsNodeText(node *sitter.Node, content []byte) string {
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

func extractObservedSymbolBytes(sym symindex.Symbol, content []byte) ([]byte, bool) {
	if sym.StartByte < 0 || sym.EndByte > len(content) || sym.StartByte >= sym.EndByte {
		return nil, false
	}
	return content[sym.StartByte:sym.EndByte], true
}

func hashShort(value string) string {
	return hashutil.ShortHash(value)
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func scoreDuplicatedErrorRemap(duplicateCount int) int {
	score := 76 + minInt(14, maxInt(0, duplicateCount-2)*7)
	return clampScore(score)
}

func scoreRepeatedGuardLadder(duplicateCount int) int {
	score := 72 + minInt(12, maxInt(0, duplicateCount-2)*6)
	return clampScore(score)
}
