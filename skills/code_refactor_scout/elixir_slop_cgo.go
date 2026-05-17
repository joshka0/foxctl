//go:build cgo

package main

import (
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	elixir "github.com/tree-sitter/tree-sitter-elixir/bindings/go"

	symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

func analyzeElixirDuplicateRecoveryBlocks(_ string, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	return analyzeElixirSymbolGroups(relPath, lang, content, symbols, collectElixirDuplicateRecoveryGroups, buildElixirDuplicateRecoveryFindings)
}

func analyzeElixirDuplicatedErrorRemaps(_ string, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	return analyzeElixirSymbolGroups(relPath, lang, content, symbols, collectElixirDuplicatedErrorRemapGroups, buildElixirDuplicatedErrorRemapFindings)
}

func analyzeElixirRepeatedGuardLadders(_ string, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	return analyzeElixirSymbolGroups(relPath, lang, content, symbols, collectElixirRepeatedGuardGroups, buildElixirRepeatedGuardFindings)
}

func analyzeElixirSymbolGroups[T any](relPath, lang string, content []byte, symbols []symindex.Symbol, collect func(root *sitter.Node, content []byte) []T, build func(symindex.Symbol, string, string, []T) []finding) []finding {
	if len(symbols) == 0 {
		return nil
	}

	findings := make([]finding, 0, 4)
	for _, sym := range symbols {
		groups, ok := collectElixirSymbolCandidates(sym, lang, content, collect)
		if !ok {
			continue
		}
		findings = append(findings, build(sym, relPath, lang, groups)...)
	}
	return findings
}

func buildElixirDuplicateRecoveryFindings(sym symindex.Symbol, relPath, lang string, groups []elixirDuplicateRecoveryGroup) []finding {
	findings := make([]finding, 0, len(groups))
	for _, group := range groups {
		absLines := absoluteElixirLines(sym, group.Lines)
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

func buildElixirDuplicatedErrorRemapFindings(sym symindex.Symbol, relPath, lang string, groups []elixirDuplicatedErrorRemapGroup) []finding {
	findings := make([]finding, 0, len(groups))
	for _, group := range groups {
		absLines := absoluteElixirLines(sym, group.Lines)
		score := scoreDuplicatedErrorRemap(len(absLines))
		findings = append(findings, finding{
			RuleID:            "duplicated_error_remap",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function repeats the same guarded error remap",
			Detail:            elixirErrorRemapDetail(sym.Name, group.GroupKind, len(absLines)),
			SuggestedRefactor: "Extract the repeated error remap into one helper that translates the upstream error shape to the domain error once, then call it from each matching branch.",
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
				"condition_count":           len(group.ConditionFingerprints),
				"group_kind":                group.GroupKind,
			},
		})
	}
	return findings
}

func buildElixirRepeatedGuardFindings(sym symindex.Symbol, relPath, lang string, groups []elixirRepeatedGuardGroup) []finding {
	findings := make([]finding, 0, len(groups))
	for _, group := range groups {
		absLines := absoluteElixirLines(sym, group.Lines)
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

func absoluteElixirLines(sym symindex.Symbol, relativeLines []int) []int {
	absLines := make([]int, 0, len(relativeLines))
	for _, line := range relativeLines {
		absLines = append(absLines, sym.StartLine+line-1)
	}
	return absLines
}

type elixirDuplicateRecoveryGroup struct {
	Fingerprint      string
	Lines            []int
	StatementCount   int
	ControlTransfers int
}

type elixirDuplicatedErrorRemapGroup struct {
	Fingerprint           string
	ConditionFingerprint  string
	ConditionFingerprints []string
	GroupKind             string
	RemapFingerprint      string
	Lines                 []int
}

type elixirRepeatedGuardGroup struct {
	Fingerprint string
	Preview     string
	Lines       []int
}

type elixirGuardAtom struct {
	Fingerprint string
	Preview     string
	Line        int
}

type elixirLineGroup[T any] struct {
	Fingerprint string
	Lines       []int
	Value       T
}

func parseElixirSlopTree(content []byte) (*sitter.Tree, bool) {
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

func newElixirLineGroup[T any](fingerprint string, lines []int, value T) *elixirLineGroup[T] {
	groupLines := append([]int(nil), lines...)
	sort.Ints(groupLines)
	return &elixirLineGroup[T]{
		Fingerprint: fingerprint,
		Lines:       groupLines,
		Value:       value,
	}
}

func appendElixirGroupLine[T any](group *elixirLineGroup[T], line int) {
	if group == nil {
		return
	}
	group.Lines = append(group.Lines, line)
	sort.Ints(group.Lines)
}

func sortedElixirLineGroups[T any](groups map[string]*elixirLineGroup[T], applyLines func(*T, []int), lines func(*T) []int, fingerprint func(*T) string) []T {
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

func mergeElixirRecoveryStats(existing *elixirDuplicateRecoveryGroup, candidate elixirDuplicateRecoveryGroup) {
	if candidate.StatementCount > existing.StatementCount {
		existing.StatementCount = candidate.StatementCount
	}
	if candidate.ControlTransfers > existing.ControlTransfers {
		existing.ControlTransfers = candidate.ControlTransfers
	}
}

func collectElixirDuplicateRecoveryGroups(root *sitter.Node, content []byte) []elixirDuplicateRecoveryGroup {
	if root == nil {
		return nil
	}
	groups := make(map[string]*elixirLineGroup[elixirDuplicateRecoveryGroup])
	walkElixirIfCalls(root, content, func(call *sitter.Node) {
		candidate := elixirRecoveryCandidate(call, content)
		if candidate == nil {
			return
		}
		if group, ok := groups[candidate.Fingerprint]; ok {
			appendElixirGroupLine(group, candidate.Lines[0])
			mergeElixirRecoveryStats(&group.Value, *candidate)
			return
		}
		groups[candidate.Fingerprint] = newElixirLineGroup(candidate.Fingerprint, candidate.Lines, *candidate)
	})

	return sortedElixirLineGroups(
		groups,
		func(group *elixirDuplicateRecoveryGroup, lines []int) { group.Lines = lines },
		func(group *elixirDuplicateRecoveryGroup) []int { return group.Lines },
		func(group *elixirDuplicateRecoveryGroup) string { return group.Fingerprint },
	)
}

func collectElixirRepeatedGuardGroups(root *sitter.Node, content []byte) []elixirRepeatedGuardGroup {
	if root == nil {
		return nil
	}
	groups := make(map[string]*elixirLineGroup[elixirRepeatedGuardGroup])
	walkElixirIfCalls(root, content, func(call *sitter.Node) {
		args := elixirCallArgumentsLocal(call)
		if args == nil {
			return
		}
		for _, atom := range elixirGuardAtoms(args, content) {
			if group, ok := groups[atom.Fingerprint]; ok {
				appendElixirGroupLine(group, atom.Line)
				return
			}
			groups[atom.Fingerprint] = newElixirLineGroup(atom.Fingerprint, []int{atom.Line}, elixirRepeatedGuardGroup{
				Fingerprint: atom.Fingerprint,
				Preview:     atom.Preview,
				Lines:       []int{atom.Line},
			})
		}
	})

	return sortedElixirLineGroups(
		groups,
		func(group *elixirRepeatedGuardGroup, lines []int) { group.Lines = lines },
		func(group *elixirRepeatedGuardGroup) []int { return group.Lines },
		func(group *elixirRepeatedGuardGroup) string { return group.Fingerprint },
	)
}

func collectElixirDuplicatedErrorRemapGroups(root *sitter.Node, content []byte) []elixirDuplicatedErrorRemapGroup {
	if root == nil {
		return nil
	}
	groups := make(map[string]*elixirDuplicatedErrorRemapGroup)
	walkElixirRescueClauses(root, func(clause *sitter.Node) {
		candidates := elixirErrorRemapCandidates(clause, content)
		for _, candidate := range candidates {
			if existing, ok := groups[candidate.Fingerprint]; ok {
				existing.Lines = append(existing.Lines, candidate.Lines[0])
				sort.Ints(existing.Lines)
				existing.ConditionFingerprints = appendUniqueStrings(existing.ConditionFingerprints, candidate.ConditionFingerprints...)
				continue
			}
			copyCandidate := candidate
			groups[candidate.Fingerprint] = &copyCandidate
		}
	})

	out := make([]elixirDuplicatedErrorRemapGroup, 0, len(groups))
	for _, group := range groups {
		if group == nil || len(group.Lines) < 2 {
			continue
		}
		sort.Ints(group.Lines)
		out = append(out, *group)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lines[0] != out[j].Lines[0] {
			return out[i].Lines[0] < out[j].Lines[0]
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	return out
}

func walkElixirIfCalls(node *sitter.Node, content []byte, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	if node.Kind() == "call" {
		switch elixirCallTargetName(node, content) {
		case "if", "unless":
			visit(node)
		}
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		c := child
		walkElixirIfCalls(&c, content, visit)
	}
}

func walkElixirRescueClauses(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	if node.Kind() == "stab_clause" {
		visit(node)
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		c := child
		walkElixirRescueClauses(&c, visit)
	}
}

func elixirRecoveryCandidate(call *sitter.Node, content []byte) *elixirDuplicateRecoveryGroup {
	if call == nil || call.Kind() != "call" {
		return nil
	}
	block := elixirCallDoBlock(call)
	if block == nil {
		return nil
	}
	parts := elixirRecoveryFingerprints(block, content)
	if len(parts) == 0 {
		return nil
	}
	fingerprint := strings.Join(parts, ";")
	if !elixirRecoveryFingerprintHasSignal(fingerprint) {
		return nil
	}
	controlTransfers := elixirControlTransfers(block, content)
	if controlTransfers == 0 {
		return nil
	}
	return &elixirDuplicateRecoveryGroup{
		Fingerprint:      fingerprint,
		Lines:            []int{int(call.StartPosition().Row) + 1},
		StatementCount:   len(parts),
		ControlTransfers: controlTransfers,
	}
}

func elixirRecoveryFingerprints(block *sitter.Node, content []byte) []string {
	statements := elixirNamedChildren(block)
	if len(statements) == 0 {
		return nil
	}
	parts := make([]string, 0, len(statements))
	for _, stmt := range statements {
		fp := elixirNodeFingerprint(&stmt, content)
		if strings.TrimSpace(fp) == "" {
			continue
		}
		parts = append(parts, fp)
	}
	return parts
}

func elixirRecoveryFingerprintHasSignal(fingerprint string) bool {
	if strings.TrimSpace(fingerprint) == "" {
		return false
	}
	tokens := strings.Fields(strings.NewReplacer(
		"(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ", ",", " ", ";", " ",
	).Replace(fingerprint))
	return len(tokens) >= 2
}

func elixirErrorRemapCandidates(clause *sitter.Node, content []byte) []elixirDuplicatedErrorRemapGroup {
	if clause == nil || clause.Kind() != "stab_clause" {
		return nil
	}
	left := clause.ChildByFieldName("left")
	if left == nil {
		left = clause.ChildByFieldName("arguments")
	}
	body := clause.ChildByFieldName("right")
	if body == nil || body.Kind() != "body" {
		return nil
	}
	children := elixirNamedChildren(body)
	out := make([]elixirDuplicatedErrorRemapGroup, 0, len(children)+1)
	if candidate, ok := elixirDirectClauseRemapCandidate(left, body, content); ok {
		out = append(out, candidate)
	}
	for _, child := range children {
		c := child
		candidate, ok := elixirErrorRemapCandidate(&c, content)
		if ok {
			out = append(out, candidate)
		}
	}
	return out
}

func elixirErrorRemapCandidate(node *sitter.Node, content []byte) (elixirDuplicatedErrorRemapGroup, bool) {
	if node == nil || node.Kind() != "call" {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	switch elixirCallTargetName(node, content) {
	case "if", "unless":
	default:
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	args := elixirCallArgumentsLocal(node)
	block := elixirCallDoBlock(node)
	if args == nil || block == nil {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	statements := elixirNamedChildren(block)
	if len(statements) != 1 {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	action := &statements[0]
	if !elixirLooksLikeErrorRemapAction(action, content) {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	conditionFingerprint := elixirNodeFingerprint(args, content)
	if strings.TrimSpace(conditionFingerprint) == "" {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	remapFingerprint := elixirNodeFingerprint(action, content)
	if strings.TrimSpace(remapFingerprint) == "" {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	return elixirDuplicatedErrorRemapGroup{
		Fingerprint:           "guarded:" + conditionFingerprint + "=>" + remapFingerprint,
		ConditionFingerprint:  conditionFingerprint,
		ConditionFingerprints: []string{conditionFingerprint},
		GroupKind:             "guarded",
		RemapFingerprint:      remapFingerprint,
		Lines:                 []int{int(node.StartPosition().Row) + 1},
	}, true
}

func elixirDirectClauseRemapCandidate(left, body *sitter.Node, content []byte) (elixirDuplicatedErrorRemapGroup, bool) {
	if left == nil || body == nil || body.Kind() != "body" {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	children := elixirNamedChildren(body)
	if len(children) != 1 {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	action := &children[0]
	if !elixirLooksLikeTupleRemapAction(action, content) {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	conditionFingerprint := elixirNodeFingerprint(left, content)
	remapFingerprint := elixirNodeFingerprint(action, content)
	if strings.TrimSpace(conditionFingerprint) == "" || strings.TrimSpace(remapFingerprint) == "" {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	if conditionFingerprint == remapFingerprint {
		return elixirDuplicatedErrorRemapGroup{}, false
	}
	return elixirDuplicatedErrorRemapGroup{
		Fingerprint:           "tuple_clause:" + remapFingerprint,
		ConditionFingerprint:  conditionFingerprint,
		ConditionFingerprints: []string{conditionFingerprint},
		GroupKind:             "tuple_clause",
		RemapFingerprint:      remapFingerprint,
		Lines:                 []int{int(left.StartPosition().Row) + 1},
	}, true
}

func elixirGuardAtoms(node *sitter.Node, content []byte) []elixirGuardAtom {
	if node == nil {
		return nil
	}
	if atoms, handled := elixirGuardContainerAtoms(node, content); handled {
		return atoms
	}
	if atoms, handled := elixirGuardBooleanAtoms(node, content); handled {
		return atoms
	}
	return elixirGuardLeafAtom(node, content)
}

func elixirGuardContainerAtoms(node *sitter.Node, content []byte) ([]elixirGuardAtom, bool) {
	switch node.Kind() {
	case "arguments", "body":
	default:
		return nil, false
	}
	children := elixirNamedChildren(node)
	atoms := make([]elixirGuardAtom, 0, len(children))
	for _, child := range children {
		c := child
		atoms = append(atoms, elixirGuardAtoms(&c, content)...)
	}
	return atoms, true
}

func elixirGuardBooleanAtoms(node *sitter.Node, content []byte) ([]elixirGuardAtom, bool) {
	if node == nil || node.Kind() != "binary_operator" {
		return nil, false
	}
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	switch elixirBinaryOperator(node, left, right, content) {
	case "and", "or", "&&", "||":
	default:
		return nil, false
	}
	atoms := elixirGuardAtoms(left, content)
	atoms = append(atoms, elixirGuardAtoms(right, content)...)
	return atoms, true
}

func elixirGuardLeafAtom(node *sitter.Node, content []byte) []elixirGuardAtom {
	fingerprint := elixirNodeFingerprint(node, content)
	if strings.TrimSpace(fingerprint) == "" {
		return nil
	}
	if !elixirInterestingGuardNode(node, fingerprint) {
		return nil
	}
	return []elixirGuardAtom{{
		Fingerprint: fingerprint,
		Preview:     previewElixirText(elixirNodeText(node, content)),
		Line:        int(node.StartPosition().Row) + 1,
	}}
}

func elixirInterestingGuardNode(node *sitter.Node, fingerprint string) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "identifier", "alias", "atom", "integer", "float", "string", "quoted_content", "charlist", "true", "false", "nil":
		return false
	}
	return strings.HasPrefix(fingerprint, "bin(") ||
		strings.HasPrefix(fingerprint, "call(") ||
		strings.HasPrefix(fingerprint, "unary(") ||
		strings.Contains(fingerprint, "dot(")
}

func elixirLooksLikeErrorRemapAction(node *sitter.Node, content []byte) bool {
	if node == nil {
		return false
	}
	if node.Kind() == "tuple" {
		return elixirTupleRemapTag(node, content) != ""
	}
	if node.Kind() != "call" {
		return false
	}
	switch elixirCallTargetName(node, content) {
	case "raise", "reraise", "throw", "exit":
		return true
	default:
		return false
	}
}

func elixirLooksLikeTupleRemapAction(node *sitter.Node, content []byte) bool {
	if node == nil || node.Kind() != "tuple" {
		return false
	}
	return elixirTupleRemapTag(node, content) != ""
}

func elixirTupleRemapTag(node *sitter.Node, content []byte) string {
	if node == nil || node.Kind() != "tuple" {
		return ""
	}
	children := elixirNamedChildren(node)
	if len(children) == 0 {
		return ""
	}
	first := &children[0]
	if first.Kind() != "atom" {
		return ""
	}
	switch tag := strings.TrimSpace(elixirNodeText(first, content)); tag {
	case ":error", ":reply", ":noreply", ":stop", ":halt":
		return tag
	default:
		return ""
	}
}

func appendUniqueStrings(items []string, values ...string) []string {
	if len(values) == 0 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	out := append([]string(nil), items...)
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		seen[item] = struct{}{}
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func elixirErrorRemapDetail(name, kind string, duplicateCount int) string {
	switch kind {
	case "tuple_clause":
		return name + " repeats the same tuple-style error remap " + itoa(duplicateCount) + " times across clause handling, which suggests one shared translation helper should own the mapping."
	default:
		return name + " repeats the same guarded error remap " + itoa(duplicateCount) + " times inside rescue handling, which suggests one shared error translation helper should own the mapping."
	}
}

func elixirNodeFingerprint(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	if fp, ok := elixirScalarFingerprint(node, content); ok {
		return fp
	}
	if fp, ok := elixirStructuredFingerprint(node, content); ok {
		return fp
	}
	return elixirFallbackFingerprint(node, content)
}

func elixirScalarFingerprint(node *sitter.Node, content []byte) (string, bool) {
	switch node.Kind() {
	case "identifier":
		return "var", true
	case "alias":
		return "alias(" + strings.TrimSpace(elixirNodeText(node, content)) + ")", true
	case "atom", "integer", "float", "charlist", "true", "false", "nil":
		return "lit(" + strings.TrimSpace(elixirNodeText(node, content)) + ")", true
	case "quoted_content":
		return "quoted(" + strings.TrimSpace(elixirNodeText(node, content)) + ")", true
	case "string":
		children := elixirNamedChildren(node)
		if len(children) == 0 {
			return "string(" + strings.TrimSpace(elixirNodeText(node, content)) + ")", true
		}
		return "string(" + strings.Join(elixirFingerprints(children, content), ",") + ")", true
	default:
		return "", false
	}
}

func elixirStructuredFingerprint(node *sitter.Node, content []byte) (string, bool) {
	switch node.Kind() {
	case "call":
		return elixirCallFingerprint(node, content), true
	case "dot":
		return elixirDotFingerprint(node, content), true
	case "binary_operator":
		left := node.ChildByFieldName("left")
		right := node.ChildByFieldName("right")
		return "bin(" + elixirBinaryOperator(node, left, right, content) + "," + elixirNodeFingerprint(left, content) + "," + elixirNodeFingerprint(right, content) + ")", true
	case "unary_operator":
		operand := node.ChildByFieldName("operand")
		return "unary(" + elixirUnaryOperator(node, operand, content) + "," + elixirNodeFingerprint(operand, content) + ")", true
	case "arguments", "do_block", "body", "tuple", "list", "map", "struct":
		return elixirNodeWithChildrenFingerprint(node, content), true
	default:
		return "", false
	}
}

func elixirCallFingerprint(node *sitter.Node, content []byte) string {
	target := node.ChildByFieldName("target")
	targetFP := elixirCallTargetFingerprint(target, content)
	argFPs := elixirFingerprints(elixirNamedChildren(elixirCallArgumentsLocal(node)), content)
	if len(argFPs) == 0 {
		return "call(" + targetFP + ")"
	}
	return "call(" + targetFP + "," + strings.Join(argFPs, ",") + ")"
}

func elixirNodeWithChildrenFingerprint(node *sitter.Node, content []byte) string {
	childFPs := elixirFingerprints(elixirNamedChildren(node), content)
	if len(childFPs) == 0 {
		return node.Kind()
	}
	return node.Kind() + "[" + strings.Join(childFPs, ",") + "]"
}

func elixirFallbackFingerprint(node *sitter.Node, content []byte) string {
	childFPs := elixirFingerprints(elixirNamedChildren(node), content)
	if len(childFPs) > 0 {
		return node.Kind() + "[" + strings.Join(childFPs, ",") + "]"
	}
	text := strings.TrimSpace(elixirNodeText(node, content))
	if text == "" {
		return node.Kind()
	}
	return node.Kind() + "(" + text + ")"
}

func elixirCallTargetFingerprint(target *sitter.Node, content []byte) string {
	if target == nil {
		return "call"
	}
	switch target.Kind() {
	case "identifier", "alias":
		return strings.TrimSpace(elixirNodeText(target, content))
	case "dot":
		return elixirDotFingerprint(target, content)
	default:
		return elixirNodeFingerprint(target, content)
	}
}

func elixirDotFingerprint(node *sitter.Node, content []byte) string {
	if node == nil {
		return "dot"
	}
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil || right == nil {
		children := elixirNamedChildren(node)
		if len(children) >= 2 {
			left = &children[0]
			right = &children[1]
		}
	}
	return "dot(" + elixirNodeFingerprint(left, content) + "," + elixirMemberName(right, content) + ")"
}

func elixirMemberName(node *sitter.Node, content []byte) string {
	if node == nil {
		return "member"
	}
	text := strings.TrimSpace(elixirNodeText(node, content))
	if text == "" {
		return node.Kind()
	}
	return text
}

func elixirCallTargetName(node *sitter.Node, content []byte) string {
	if node == nil || node.Kind() != "call" {
		return ""
	}
	target := node.ChildByFieldName("target")
	if target == nil {
		return ""
	}
	return strings.TrimSpace(elixirNodeText(target, content))
}

func elixirCallArgumentsLocal(node *sitter.Node) *sitter.Node {
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

func elixirCallDoBlock(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "do_block" {
			c := child
			return &c
		}
	}
	return nil
}

func elixirNamedChildren(node *sitter.Node) []sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	if len(children) == 0 {
		return nil
	}
	out := make([]sitter.Node, 0, len(children))
	out = append(out, children...)
	return out
}

func elixirFingerprints(nodes []sitter.Node, content []byte) []string {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		fp := elixirNodeFingerprint(&node, content)
		if strings.TrimSpace(fp) == "" {
			continue
		}
		out = append(out, fp)
	}
	return out
}

func elixirBinaryOperator(node, left, right *sitter.Node, content []byte) string {
	if node == nil || left == nil || right == nil {
		return "op"
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
	op := strings.TrimSpace(string(content[start:end]))
	if op == "" {
		return "op"
	}
	return op
}

func elixirUnaryOperator(node, operand *sitter.Node, content []byte) string {
	if node == nil || operand == nil {
		return "op"
	}
	start := int(node.StartByte())
	end := int(operand.StartByte())
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
	op := strings.TrimSpace(string(content[start:end]))
	if op == "" {
		return "op"
	}
	return op
}

func elixirControlTransfers(node *sitter.Node, content []byte) int {
	if node == nil {
		return 0
	}
	count := 0
	var walk func(*sitter.Node)
	walk = func(current *sitter.Node) {
		if current == nil {
			return
		}
		if current.Kind() == "call" {
			switch elixirCallTargetName(current, content) {
			case "raise", "reraise", "throw", "exit":
				count++
			}
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

func elixirNodeText(node *sitter.Node, content []byte) string {
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

func previewElixirText(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if len(text) <= 80 {
		return text
	}
	return text[:77] + "..."
}
