package main

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

func TestRunSummaryPresentationSignalsAndTargetFiltering(t *testing.T) {
	work := t.TempDir()
	source := `package sample

func hasValue(value *int) bool {
	if value == nil {
		return false
	}
	return true
}

func isPresent(value *int) bool {
	if value == nil {
		return false
	}
	return true
}
`
	if err := os.WriteFile(filepath.Join(work, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stdout := &bytes.Buffer{}
	rc, cleanup := skilltest.NewTestRunContext(t, stdout, &skilltest.TestContextOptions{Workspace: work})
	defer cleanup()

	if err := run(context.Background(), rc, input{
		Path:       "sample.go",
		Language:   "go",
		Target:     targetSmallComposableCode,
		View:       "summary",
		MaxResults: 1,
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := skilltest.AssertEnvelopeOK(t, stdout.Bytes(), command)
	data := extractMapData(t, env.Data)

	if data["focus"] != "all" {
		t.Fatalf("focus=%v want all", data["focus"])
	}
	if data["target"] != targetSmallComposableCode {
		t.Fatalf("target=%v want %s", data["target"], targetSmallComposableCode)
	}
	if data["view"] != "summary" {
		t.Fatalf("view=%v want summary", data["view"])
	}

	languageScope := extractMapData(t, data["language_scope"])
	if languageScope["language"] != "go" {
		t.Fatalf("language_scope.language=%v want go", languageScope["language"])
	}

	presentation := extractMapData(t, data["presentation"])
	if presentation["active_lane"] != "overview" {
		t.Fatalf("presentation.active_lane=%v want overview", presentation["active_lane"])
	}

	summary := extractMapData(t, data["summary"])
	if got := intFromAny(t, summary["finding_count"]); got < 2 {
		t.Fatalf("summary.finding_count=%d want at least 2", got)
	}
	if got := intFromAny(t, summary["returned_findings"]); got != 1 {
		t.Fatalf("summary.returned_findings=%d want 1", got)
	}
	if !boolFromAny(t, summary["limited_by_results"]) {
		t.Fatalf("summary.limited_by_results=%v want true", summary["limited_by_results"])
	}

	signals := extractMapData(t, data["signals"])
	if !boolFromAny(t, signals["targeted_review"]) {
		t.Fatalf("signals.targeted_review=%v want true", signals["targeted_review"])
	}
	if boolFromAny(t, signals["slop_focus"]) {
		t.Fatalf("signals.slop_focus=%v want false", signals["slop_focus"])
	}
	if boolFromAny(t, signals["dead_focus"]) {
		t.Fatalf("signals.dead_focus=%v want false", signals["dead_focus"])
	}

	findings, ok := data["findings"].([]any)
	if !ok {
		t.Fatalf("findings type=%T want []any", data["findings"])
	}
	if len(findings) != 1 {
		t.Fatalf("len(findings)=%d want 1", len(findings))
	}
	first := extractMapData(t, findings[0])
	targets, ok := first["targets"].([]any)
	if !ok || len(targets) == 0 {
		t.Fatalf("findings[0].targets=%#v want non-empty target annotations", first["targets"])
	}
	foundTarget := false
	for _, target := range targets {
		if target == targetSmallComposableCode {
			foundTarget = true
			break
		}
	}
	if !foundTarget {
		t.Fatalf("findings[0].targets=%#v missing %s", targets, targetSmallComposableCode)
	}
}

func TestRunSlopFocusFiltersNonSlopFindings(t *testing.T) {
	work := t.TempDir()
	source := `package sample

func isPresent(value *int) bool {
	if value == nil {
		return false
	}
	return true
}
`
	if err := os.WriteFile(filepath.Join(work, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stdout := &bytes.Buffer{}
	rc, cleanup := skilltest.NewTestRunContext(t, stdout, &skilltest.TestContextOptions{Workspace: work})
	defer cleanup()

	if err := run(context.Background(), rc, input{
		Path:     "sample.go",
		Language: "go",
		Focus:    "slop",
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := skilltest.AssertEnvelopeOK(t, stdout.Bytes(), command)
	data := extractMapData(t, env.Data)
	if data["focus"] != "slop" {
		t.Fatalf("focus=%v want slop", data["focus"])
	}

	signals := extractMapData(t, data["signals"])
	if !boolFromAny(t, signals["slop_focus"]) {
		t.Fatalf("signals.slop_focus=%v want true", signals["slop_focus"])
	}
	if boolFromAny(t, signals["targeted_review"]) {
		t.Fatalf("signals.targeted_review=%v want false", signals["targeted_review"])
	}

	summary := extractMapData(t, data["summary"])
	if got := intFromAny(t, summary["finding_count"]); got == 0 {
		t.Fatalf("summary.finding_count=%d want at least 1 slop finding", got)
	}
	if got := intFromAny(t, summary["returned_findings"]); got == 0 {
		t.Fatalf("summary.returned_findings=%d want at least 1 slop finding", got)
	}

	findings, ok := data["findings"].([]any)
	if !ok {
		t.Fatalf("findings type=%T want []any", data["findings"])
	}
	for _, raw := range findings {
		item := extractMapData(t, raw)
		if !focusAllowsFinding(finding{
			RuleID:   item["rule_id"].(string),
			Evidence: extractOptionalMap(item["evidence"]),
		}, "slop") {
			t.Fatalf("non-slop finding survived slop focus: %#v", item)
		}
	}
}

func extractMapData(t *testing.T, v any) map[string]any {
	t.Helper()
	data, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("value type=%T want map[string]any", v)
	}
	return data
}

func intFromAny(t *testing.T, v any) int {
	t.Helper()
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		t.Fatalf("value type=%T want number", v)
		return 0
	}
}

func boolFromAny(t *testing.T, v any) bool {
	t.Helper()
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("value type=%T want bool", v)
	}
	return b
}

func extractOptionalMap(v any) map[string]any {
	data, _ := v.(map[string]any)
	return data
}

func TestSplitTopLevel(t *testing.T) {
	got := splitTopLevel("a int, b map[string]int, c tuple[int, string], d func(x int, y string)")
	if len(got) != 4 {
		t.Fatalf("expected 4 parts, got %d: %#v", len(got), got)
	}
}

func TestGoSplitTopLevelNestedStructures(t *testing.T) {
	input := "a map[string][]int, b func(x func(y int, z []string), y map[string]int), c tuple[map[string]int, func(a, b int)], d"
	got := splitTopLevel(input)
	want := []string{
		"a map[string][]int",
		"b func(x func(y int, z []string), y map[string]int)",
		"c tuple[map[string]int, func(a, b int)]",
		"d",
	}
	if len(got) != len(want) {
		t.Fatalf("len(splitTopLevel)=%d want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitTopLevel[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestGoFuncSignatureReceiverAndNamedReturns(t *testing.T) {
	fn := mustParseGoFuncDeclByName(t, `package sample

import "context"

type Worker struct{}

func (w *Worker) Build(ctx context.Context, labels []string) (ok bool, err error) {
	return true, nil
}
`, "Build")

	got := goFuncSignature(fn)
	want := "func (w *Worker) Build(ctx context.Context, labels []string) (ok bool, err error)"
	if got != want {
		t.Fatalf("goFuncSignature=%q want %q", got, want)
	}
}

func TestGoStmtFingerprintStabilityWithBranchesAndCalls(t *testing.T) {
	fn := mustParseGoFuncDeclByName(t, `package sample

type Item struct{}

func evaluate(value *Item) bool {
	present := lookup(value)
	if present != nil {
		record(present)
		return check(present)
	}
	return fallback()
}
`, "evaluate")

	got := extractGoOrchestrationMetrics(fn)
	wantFingerprint := "assign(call_ident_1);if(bin(!=,ident,ident)){call_ident_1;return(call_ident_1)};return(call_ident_0)"
	if got.Fingerprint != wantFingerprint {
		t.Fatalf("fingerprint=%q want %q", got.Fingerprint, wantFingerprint)
	}
	if got.BranchCount != 1 {
		t.Fatalf("branch_count=%d want 1", got.BranchCount)
	}
	if got.CallSiteCount != 4 {
		t.Fatalf("call_site_count=%d want 4", got.CallSiteCount)
	}
}

func TestDetectGoBoolReturnWrapperIfElseForm(t *testing.T) {
	fn := mustParseGoFuncDeclByName(t, `package sample

type Item struct{}

func isPresent(value *Item) bool {
	if value == nil {
		return false
	} else {
		return true
	}
}
`, "isPresent")

	candidate := detectGoBoolReturnWrapper(fn.Body.List)
	if candidate == nil {
		t.Fatal("expected bool return wrapper candidate")
	}
	if candidate.Kind != "boolean_return_wrapper" {
		t.Fatalf("kind=%q want boolean_return_wrapper", candidate.Kind)
	}
	if candidate.SimplifiedForm != "return value != nil" {
		t.Fatalf("simplified_form=%q want return value != nil", candidate.SimplifiedForm)
	}
	if len(candidate.PatternIDs) == 0 || candidate.PatternIDs[0] != "inverted_boolean_return_wrapper" {
		t.Fatalf("pattern_ids=%#v want inverted wrapper first", candidate.PatternIDs)
	}
}

func TestDetectGoBoolReturnWrapperIfTailForm(t *testing.T) {
	fn := mustParseGoFuncDeclByName(t, `package sample

type Item struct{}

func isPresent(value *Item) bool {
	if value == nil {
		return false
	}
	return true
}
`, "isPresent")

	candidate := detectGoBoolReturnWrapper(fn.Body.List)
	if candidate == nil {
		t.Fatal("expected bool return wrapper candidate")
	}
	if candidate.Kind != "boolean_return_wrapper" {
		t.Fatalf("kind=%q want boolean_return_wrapper", candidate.Kind)
	}
	if candidate.SimplifiedForm != "return value != nil" {
		t.Fatalf("simplified_form=%q want return value != nil", candidate.SimplifiedForm)
	}
	if len(candidate.PatternIDs) == 0 || candidate.PatternIDs[0] != "inverted_boolean_return_wrapper" {
		t.Fatalf("pattern_ids=%#v want inverted wrapper first", candidate.PatternIDs)
	}
}

func mustParseGoFuncDeclByName(t *testing.T, src, name string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse go file: %v", err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		if fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("function %q not found", name)
	return nil
}

func TestParseSignatureMetricsTypeScript(t *testing.T) {
	metrics := parseSignatureMetrics("export function build(flag: boolean, opts: Map<string, number>, pair: [number, string]): [string, Error]", "typescript")
	if metrics.ParamCount != 3 {
		t.Fatalf("expected 3 params, got %d", metrics.ParamCount)
	}
	if metrics.BoolParamCount != 1 {
		t.Fatalf("expected 1 bool param, got %d", metrics.BoolParamCount)
	}
	if metrics.ReturnCount != 2 {
		t.Fatalf("expected 2 return values, got %d", metrics.ReturnCount)
	}
}

func TestAnalyzeGoFuncDeclFindings(t *testing.T) {
	src := `package sample

type Big interface {
	A()
	B()
	C()
	D()
	E()
	F()
	G()
	H()
}

func DoThing(a int, b int, c int, d bool, e string, f string) (int, string, error) {
	if a > 0 {
		if b > 0 {
			if c > 0 {
				if d {
					return 1, "ok", nil
				}
			}
		}
	}
	return 0, "", nil
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse go file: %v", err)
	}

	state := &scoutState{
		Thresholds: thresholdsFor("aggressive"),
	}

	var got []finding
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			got = append(got, analyzeGoFuncDecl(d, fset, "sample.go", state)...)
		case *ast.GenDecl:
			got = append(got, analyzeGoGenDecl(d, fset, "sample.go", state)...)
		}
	}

	wantRules := map[string]bool{
		"long_parameter_list": false,
		"boolean_parameter":   false,
		"wide_return_tuple":   false,
		"deep_nesting":        false,
		"wide_interface":      false,
	}
	for _, item := range got {
		if _, ok := wantRules[item.RuleID]; ok {
			wantRules[item.RuleID] = true
		}
	}
	for ruleID, matched := range wantRules {
		if !matched {
			t.Fatalf("expected finding %s, got %#v", ruleID, got)
		}
	}
}

func TestAnalyzeGoFuncDeclFindsDuplicateRecoveryBlock(t *testing.T) {
	src := `package sample

func recoverThing(a, b bool) error {
	if a {
		logFailure()
		return errRetry
	}
	if b {
		logFailure()
		return errRetry
	}
	return nil
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse go file: %v", err)
	}

	state := &scoutState{
		Thresholds: thresholdsFor("default"),
	}

	var got []finding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		got = append(got, analyzeGoFuncDecl(fn, fset, "sample.go", state)...)
	}

	for _, item := range got {
		if item.RuleID != "duplicate_recovery_block" {
			continue
		}
		if item.Symbol != "recoverThing" {
			t.Fatalf("symbol=%q want recoverThing", item.Symbol)
		}
		if gotLines, ok := item.Evidence["duplicate_span_lines"].([]int); !ok || len(gotLines) != 2 || gotLines[0] != 4 || gotLines[1] != 8 {
			t.Fatalf("duplicate_span_lines=%#v", item.Evidence["duplicate_span_lines"])
		}
		return
	}

	t.Fatalf("expected duplicate_recovery_block finding, got %#v", got)
}

func TestAnalyzeGoFuncDeclFindsSemanticSimplificationForBoolWrapper(t *testing.T) {
	src := `package sample

func isPresent(value *Item) bool {
	if value == nil {
		return false
	}
	return true
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse go file: %v", err)
	}

	state := &scoutState{Thresholds: thresholdsFor("default")}
	var got []finding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		got = append(got, analyzeGoFuncDecl(fn, fset, "sample.go", state)...)
	}

	for _, item := range got {
		if item.RuleID != "semantic_simplification_candidate" {
			continue
		}
		if item.Symbol != "isPresent" {
			t.Fatalf("symbol=%q want isPresent", item.Symbol)
		}
		if item.Evidence["simplified_form"] != "return value != nil" {
			t.Fatalf("simplified_form=%#v", item.Evidence["simplified_form"])
		}
		patterns, ok := item.Evidence["pattern_ids"].([]string)
		if !ok || len(patterns) == 0 {
			t.Fatalf("pattern_ids=%#v", item.Evidence["pattern_ids"])
		}
		if patterns[0] != "inverted_boolean_return_wrapper" {
			t.Fatalf("pattern_ids=%#v want inverted wrapper first", patterns)
		}
		return
	}

	t.Fatalf("expected semantic_simplification_candidate, got %#v", got)
}

func TestAnalyzeGoFuncDeclFindsSemanticSimplificationForBooleanLiteralComparison(t *testing.T) {
	src := `package sample

func shouldRun(flag bool) bool {
	return flag == true
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse go file: %v", err)
	}

	state := &scoutState{Thresholds: thresholdsFor("default")}
	var got []finding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		got = append(got, analyzeGoFuncDecl(fn, fset, "sample.go", state)...)
	}

	for _, item := range got {
		if item.RuleID != "semantic_simplification_candidate" {
			continue
		}
		if item.Evidence["simplified_form"] != "return flag" {
			t.Fatalf("simplified_form=%#v", item.Evidence["simplified_form"])
		}
		return
	}

	t.Fatalf("expected semantic_simplification_candidate, got %#v", got)
}

func TestAnalyzeGoFuncDeclSkipsUnsafeSemanticSimplification(t *testing.T) {
	src := `package sample

func keepCall() bool {
	return expensive() || true
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse go file: %v", err)
	}

	state := &scoutState{Thresholds: thresholdsFor("default")}
	var got []finding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		got = append(got, analyzeGoFuncDecl(fn, fset, "sample.go", state)...)
	}

	for _, item := range got {
		if item.RuleID == "semantic_simplification_candidate" {
			t.Fatalf("unexpected semantic simplification finding: %#v", item)
		}
	}
}

func TestFinalizeReceiverHotspots(t *testing.T) {
	state := &scoutState{
		Thresholds: thresholdsFor("default"),
		ReceiverMethods: map[string]receiverHotspot{
			"Worker": {Count: 10, File: "worker.go", Line: 12, Language: "go"},
		},
	}
	finalizeReceiverHotspots(state)
	if len(state.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(state.Findings))
	}
	if state.Findings[0].RuleID != "receiver_hotspot" {
		t.Fatalf("expected receiver_hotspot, got %s", state.Findings[0].RuleID)
	}
}

func TestResolveLanguageScopeRejectsMixedDirectoryAuto(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/main.go", []byte("package sample\n"), 0o644); err != nil {
		t.Fatalf("write go file: %v", err)
	}
	if err := os.WriteFile(dir+"/main.ts", []byte("export const x = 1;\n"), 0o644); err != nil {
		t.Fatalf("write ts file: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}

	_, err = resolveLanguageScope(dir, dir, info, input{Language: "auto"})
	if err == nil {
		t.Fatal("expected mixed-language auto scope to fail")
	}
}

func TestResolveLanguageScopeInfersSingleDirectoryLanguage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/main.go", []byte("package sample\n"), 0o644); err != nil {
		t.Fatalf("write go file: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}

	scope, err := resolveLanguageScope(dir, dir, info, input{Language: "auto"})
	if err != nil {
		t.Fatalf("resolve scope: %v", err)
	}
	if scope.Language != "go" {
		t.Fatalf("expected go, got %s", scope.Language)
	}
}

func TestBuildScoutStatusScopeUsesWorkspaceRelativePath(t *testing.T) {
	workspace := t.TempDir()
	searchPath := filepath.Join(workspace, "internal", "runtime", "actor")
	if err := os.MkdirAll(searchPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	info, err := os.Stat(searchPath)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}

	scope := buildScoutStatusScope(workspace, searchPath, info, languageScope{
		Mode:     "explicit",
		Language: "go",
		Detected: []string{"go"},
	}, false)

	if scope.Path != "internal/runtime/actor" {
		t.Fatalf("scope path=%q want internal/runtime/actor", scope.Path)
	}
	if scope.Absolute != searchPath {
		t.Fatalf("scope absolute=%q want %q", scope.Absolute, searchPath)
	}
	if !scope.IsDir {
		t.Fatal("expected scope IsDir=true")
	}
	if scope.IncludeTests {
		t.Fatal("expected include_tests=false")
	}
}

func TestDiversifyFindingsLimitsRuleFloodingAtHead(t *testing.T) {
	items := []finding{
		{RuleID: "god_file", File: "a.go", Score: 95},
		{RuleID: "god_file", File: "b.go", Score: 94},
		{RuleID: "god_file", File: "c.go", Score: 93},
		{RuleID: "god_file", File: "d.go", Score: 92},
		{RuleID: "high_cyclomatic_complexity", File: "e.go", Symbol: "E", Score: 91},
		{RuleID: "receiver_hotspot", File: "f.go", Symbol: "F", Score: 90},
	}

	got := diversifyFindings(items, 4, 2, 4, 2)
	head := got[:4]
	ruleCounts := map[string]int{}
	for _, item := range head {
		ruleCounts[item.RuleID]++
	}
	if ruleCounts["god_file"] > 2 {
		t.Fatalf("expected god_file to be capped in head, got %#v", head)
	}
}

func TestSynthesizeCompositeFindingsAddsFunctionHotspot(t *testing.T) {
	state := &scoutState{
		Findings: []finding{
			{
				RuleID:     "high_cyclomatic_complexity",
				File:       "a.go",
				Line:       10,
				Symbol:     "DoThing",
				Language:   "go",
				Score:      88,
				Confidence: "high",
				Signals:    []string{"go_ast", "cyclomatic_complexity"},
			},
			{
				RuleID:     "oversized_function",
				File:       "a.go",
				Line:       10,
				Symbol:     "DoThing",
				Language:   "go",
				Score:      80,
				Confidence: "high",
				Signals:    []string{"go_ast", "function_lines"},
			},
		},
	}

	hotspots := synthesizeCompositeFindings(state)
	if len(hotspots) != 1 {
		t.Fatalf("expected 1 hotspot symbol, got %d", len(hotspots))
	}

	found := false
	for _, item := range state.Findings {
		if item.RuleID == "function_hotspot" {
			found = true
			if item.Symbol != "DoThing" {
				t.Fatalf("expected DoThing hotspot, got %s", item.Symbol)
			}
			if item.Score <= 88 {
				t.Fatalf("expected hotspot score above constituent score, got %d", item.Score)
			}
		}
	}
	if !found {
		t.Fatal("expected function_hotspot finding")
	}
}

func TestApplyFocusSlopFiltersToSlopFindings(t *testing.T) {
	items := []finding{
		{RuleID: "duplicate_recovery_block", Symbol: "Recover", Score: 84},
		{RuleID: "duplicated_error_remap", Symbol: "Recover", Score: 83},
		{RuleID: "repeated_guard_ladder", Symbol: "Recover", Score: 82},
		{RuleID: "semantic_simplification_candidate", Symbol: "Recover", Score: 81},
		{RuleID: "high_cyclomatic_complexity", Symbol: "Complex", Score: 88},
		{
			RuleID: "function_hotspot",
			Symbol: "Recover",
			Evidence: map[string]any{
				"rules": []string{"semantic_simplification_candidate", "high_cyclomatic_complexity"},
			},
		},
	}

	got := applyFocus(items, "slop")
	if len(got) != 5 {
		t.Fatalf("len(got)=%d want 5 (%#v)", len(got), got)
	}
	for _, item := range got {
		if item.RuleID == "high_cyclomatic_complexity" {
			t.Fatalf("non-slop finding survived focus filter: %#v", item)
		}
	}
}

func TestApplyReviewTargetsAnnotatesSkillTargets(t *testing.T) {
	items := []finding{
		{RuleID: "function_hotspot", Category: "function", File: "a.go", Symbol: "DoThing", Score: 91},
		{RuleID: "semantic_simplification_candidate", Category: "function", File: "b.go", Symbol: "CleanBool", Score: 75},
		{RuleID: "unreachable_private_symbol", Category: "dead_code", File: "c.go", Symbol: "unused", Score: 88},
	}

	got := applyReviewTargets(items)
	hotspot := got[0]
	for _, want := range []string{targetSmallComposableCode, targetSemanticCommenting, targetImproveCodebaseArchitecture} {
		if !findingTargetsReview(hotspot, want) {
			t.Fatalf("function_hotspot missing target %q: %#v", want, hotspot.Targets)
		}
	}
	if !findingTargetsReview(got[1], targetSmallComposableCode) {
		t.Fatalf("semantic simplification should target small composable code: %#v", got[1].Targets)
	}
	if findingTargetsReview(got[1], targetImproveCodebaseArchitecture) {
		t.Fatalf("isolated semantic simplification should not target architecture: %#v", got[1].Targets)
	}
	if !findingTargetsReview(got[2], targetSmallComposableCode) {
		t.Fatalf("dead code should target small composable cleanup: %#v", got[2].Targets)
	}
}

func TestApplyTargetFiltersBySkillTarget(t *testing.T) {
	items := applyReviewTargets([]finding{
		{RuleID: "function_hotspot", Category: "function", File: "a.go", Symbol: "DoThing", Score: 91},
		{RuleID: "unreachable_private_symbol", Category: "dead_code", File: "b.go", Symbol: "unused", Score: 88},
	})

	got := applyTarget(items, targetImproveCodebaseArchitecture)
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1: %#v", len(got), got)
	}
	if got[0].RuleID != "function_hotspot" {
		t.Fatalf("filtered rule=%q want function_hotspot", got[0].RuleID)
	}
}

func TestShouldSuppressConstituentFindings(t *testing.T) {
	if shouldSuppressConstituentFindings("slop") {
		t.Fatal("slop focus should keep constituent findings visible")
	}
	if !shouldSuppressConstituentFindings("all") {
		t.Fatal("all focus should suppress constituent findings behind hotspots")
	}
}

func TestSuppressConstituentFindingsDropsChildFindingsForHotspot(t *testing.T) {
	state := &scoutState{
		Findings: []finding{
			{RuleID: "function_hotspot", File: "a.go", Symbol: "DoThing"},
			{RuleID: "high_cyclomatic_complexity", File: "a.go", Symbol: "DoThing"},
			{RuleID: "oversized_function", File: "a.go", Symbol: "DoThing"},
			{RuleID: "receiver_hotspot", File: "a.go", Symbol: "Worker"},
		},
	}

	suppressConstituentFindings(state, map[string]struct{}{"a.go::DoThing": {}})

	if len(state.Findings) != 2 {
		t.Fatalf("expected 2 remaining findings, got %d", len(state.Findings))
	}
	for _, item := range state.Findings {
		if item.Symbol == "DoThing" && item.RuleID != "function_hotspot" {
			t.Fatalf("unexpected constituent finding survived: %#v", item)
		}
	}
}

func TestQualifiesFunctionHotspotRequiresStrongerMix(t *testing.T) {
	if qualifiesFunctionHotspot(0, 0, 1) {
		t.Fatal("supportive-only mix should not qualify")
	}
	if qualifiesFunctionHotspot(1, 0, 0) {
		t.Fatal("single structural signal should not qualify")
	}
	if !qualifiesFunctionHotspot(2, 0, 0) {
		t.Fatal("two structural signals should qualify")
	}
	if !qualifiesFunctionHotspot(1, 1, 0) {
		t.Fatal("structural plus signature should qualify")
	}
}

func TestFinalizeObservationFindingsAddsDeterministicSignals(t *testing.T) {
	leftKey := observationKey("agent.go", "*Worker.Watch")
	rightKey := observationKey("agent.go", "*Worker.Ask")
	leftFingerprint := "assign(call_ident_1);if(bin(!=,ident,lit)){return(call_ident_1)};call_selector_2;return()"
	rightFingerprint := "assign(call_ident_1);if(bin(!=,ident,lit)){call_selector_1;return(call_ident_1)};call_selector_2;return()"
	state := &scoutState{
		Thresholds: thresholdsFor("default"),
		Symbols: map[string]*symbolObservation{
			leftKey: {
				File:                     "agent.go",
				Line:                     10,
				Symbol:                   "*Worker.Watch",
				Language:                 "go",
				Kind:                     symindex.KindMethod,
				Calls:                    []string{"prepare", "build", "dispatch", "wait", "persist", "notify", "close", "emit"},
				FanOut:                   8,
				ParamCount:               2,
				ReturnCount:              1,
				FunctionLines:            96,
				BranchCount:              2,
				CallSiteCount:            5,
				OrchestrationFingerprint: leftFingerprint,
				OrchestrationTokens:      tokenizeStructuralFingerprint(leftFingerprint),
			},
			rightKey: {
				File:                     "agent.go",
				Line:                     120,
				Symbol:                   "*Worker.Ask",
				Language:                 "go",
				Kind:                     symindex.KindMethod,
				Calls:                    []string{"prepare", "build", "dispatch", "wait", "persist", "notify", "close", "emit"},
				FanOut:                   8,
				ParamCount:               2,
				ReturnCount:              1,
				FunctionLines:            91,
				BranchCount:              2,
				CallSiteCount:            5,
				OrchestrationFingerprint: rightFingerprint,
				OrchestrationTokens:      tokenizeStructuralFingerprint(rightFingerprint),
			},
		},
		FileSymbols: map[string][]string{
			"agent.go": {leftKey, rightKey},
		},
	}

	finalizeObservationFindings(state)

	rules := map[string]int{}
	for _, item := range state.Findings {
		rules[item.RuleID]++
	}
	if rules["fan_out_dependency_spread"] != 2 {
		t.Fatalf("expected fan_out_dependency_spread for both symbols, got %#v", state.Findings)
	}
	if rules["duplicate_orchestration_fingerprint"] != 2 {
		t.Fatalf("expected duplicate_orchestration_fingerprint for both symbols, got %#v", state.Findings)
	}
	if rules["same_file_extraction_candidate"] != 2 {
		t.Fatalf("expected same_file_extraction_candidate for both symbols, got %#v", state.Findings)
	}
	if rules["structural_similarity_cluster"] != 1 {
		t.Fatalf("expected one structural_similarity_cluster finding, got %#v", state.Findings)
	}
	if rules["structural_similarity_module_cluster"] != 0 {
		t.Fatalf("expected no cross-file module cluster, got %#v", state.Findings)
	}
}

func TestOrchestrationSimilarityScoreAllowsNearMatches(t *testing.T) {
	leftFingerprint := "assign(call_ident_1);if(bin(!=,ident,lit)){return(call_ident_1)};call_selector_2;return()"
	rightFingerprint := "assign(call_ident_1);if(bin(!=,ident,lit)){call_selector_1;return(call_ident_1)};call_selector_2;return()"
	left := &symbolObservation{
		Language:                 "go",
		BranchCount:              2,
		CallSiteCount:            5,
		OrchestrationFingerprint: leftFingerprint,
		OrchestrationTokens:      tokenizeStructuralFingerprint(leftFingerprint),
	}
	right := &symbolObservation{
		Language:                 "go",
		BranchCount:              2,
		CallSiteCount:            5,
		OrchestrationFingerprint: rightFingerprint,
		OrchestrationTokens:      tokenizeStructuralFingerprint(rightFingerprint),
	}

	score := orchestrationSimilarityScore(left, right)
	if score < orchestrationSimilarityThreshold(thresholdsFor("default")) {
		t.Fatalf("expected near-match similarity to qualify, got %d", score)
	}
	details := orchestrationSimilarityDetailsFor(left, right)
	if details.WhySimilar == "" {
		t.Fatal("expected non-empty similarity explanation")
	}
	if len(details.SharedSubsequence) == 0 {
		t.Fatal("expected shared subsequence evidence")
	}
}

func TestSynthesizeCompositeFindingsUsesNewSignals(t *testing.T) {
	state := &scoutState{
		Findings: []finding{
			{
				RuleID:     "fan_out_dependency_spread",
				File:       "a.go",
				Line:       10,
				Symbol:     "DoThing",
				Language:   "go",
				Score:      82,
				Confidence: "high",
				Signals:    []string{"call_extraction", "fan_out"},
			},
			{
				RuleID:     "same_file_extraction_candidate",
				File:       "a.go",
				Line:       10,
				Symbol:     "DoThing",
				Language:   "go",
				Score:      78,
				Confidence: "high",
				Signals:    []string{"same_file_overlap"},
			},
		},
	}

	hotspots := synthesizeCompositeFindings(state)
	if len(hotspots) != 1 {
		t.Fatalf("expected hotspot from new signals, got %d", len(hotspots))
	}
}

func TestSortFindingsPrefersFunctionOverFileAtEqualScore(t *testing.T) {
	items := []finding{
		{RuleID: "god_file", Category: "file", File: "a.go", Score: 80},
		{RuleID: "fan_out_dependency_spread", Category: "function", File: "b.go", Symbol: "DoThing", Score: 80},
	}
	sortFindings(items)
	if items[0].Category != "function" {
		t.Fatalf("expected function finding first, got %#v", items)
	}
}

func TestSortFindingsPrefersClusterOverFileAtEqualScore(t *testing.T) {
	items := []finding{
		{RuleID: "god_file", Category: "file", File: "a.go", Score: 82},
		{RuleID: "structural_similarity_cluster", Category: "cluster", File: "b.go", Score: 82},
	}
	sortFindings(items)
	if items[0].Category != "cluster" {
		t.Fatalf("expected cluster finding first, got %#v", items)
	}
}

func TestSortFindingsPrefersModuleClusterOverFileClusterAtEqualScore(t *testing.T) {
	items := []finding{
		{RuleID: "structural_similarity_cluster", Category: "cluster", File: "a.go", Score: 84},
		{RuleID: "structural_similarity_module_cluster", Category: "cluster", File: "b.go", Score: 84},
	}
	sortFindings(items)
	if items[0].RuleID != "structural_similarity_module_cluster" {
		t.Fatalf("expected module cluster first, got %#v", items)
	}
}

func TestBuildScoutPresentationAggregatesRepeatedPatternFamilies(t *testing.T) {
	items := []finding{
		{
			RuleID:   "repeated_guard_ladder",
			Category: "function",
			File:     "reader/index.ts",
			Line:     10,
			Symbol:   "readerApi",
			Score:    84,
			Detail:   "readerApi repeats the same guard predicate 4 times.",
			Evidence: map[string]any{"guard_preview": "value !== null"},
		},
		{
			RuleID:   "repeated_guard_ladder",
			Category: "function",
			File:     "reader/index.ts",
			Line:     10,
			Symbol:   "readerApi",
			Score:    84,
			Detail:   "readerApi repeats the same guard predicate 4 times.",
			Evidence: map[string]any{"guard_preview": "value !== undefined"},
		},
		{
			RuleID:   "function_hotspot",
			Category: "function",
			File:     "reader/index.ts",
			Line:     10,
			Symbol:   "readerApi",
			Score:    94,
			Detail:   "readerApi triggers multiple refactoring signals.",
			Evidence: map[string]any{"rules": []string{"repeated_guard_ladder", "fan_out_dependency_spread"}},
		},
	}

	presentation := buildScoutPresentation(items, "grouped")
	if presentation.ActiveLane != "best_entrypoints" {
		t.Fatalf("active lane=%q want best_entrypoints", presentation.ActiveLane)
	}
	if len(presentation.Lanes.BestEntrypoints) != 1 {
		t.Fatalf("entrypoints=%#v want 1", presentation.Lanes.BestEntrypoints)
	}
	if presentation.Lanes.BestEntrypoints[0].RepresentativeRule != "function_hotspot" {
		t.Fatalf("representative rule=%q want function_hotspot", presentation.Lanes.BestEntrypoints[0].RepresentativeRule)
	}
	if len(presentation.Lanes.RepeatedPatternFamily) != 1 {
		t.Fatalf("pattern families=%#v want 1", presentation.Lanes.RepeatedPatternFamily)
	}
	if presentation.Lanes.RepeatedPatternFamily[0].FindingCount != 2 {
		t.Fatalf("pattern family count=%d want 2", presentation.Lanes.RepeatedPatternFamily[0].FindingCount)
	}
	if len(presentation.Lanes.RepeatedPatternFamily[0].Samples) != 2 {
		t.Fatalf("pattern family samples=%#v want 2", presentation.Lanes.RepeatedPatternFamily[0].Samples)
	}
}

func TestBuildScoutPresentationSummaryViewUsesOverviewLane(t *testing.T) {
	items := []finding{{RuleID: "god_file", Category: "file", File: "a.go", Score: 81, Detail: "large file"}}
	presentation := buildScoutPresentation(items, "summary")
	if presentation.ActiveLane != "overview" {
		t.Fatalf("active lane=%q want overview", presentation.ActiveLane)
	}
	if len(presentation.Overview.TopRuleFamilies) != 1 {
		t.Fatalf("top rule families=%#v want 1", presentation.Overview.TopRuleFamilies)
	}
}

func TestBuildScoutPresentationIncludesSkillTargetLanes(t *testing.T) {
	items := applyReviewTargets([]finding{
		{
			RuleID:   "function_hotspot",
			Category: "function",
			File:     "reader/index.ts",
			Line:     10,
			Symbol:   "readerApi",
			Score:    94,
			Detail:   "readerApi triggers multiple refactoring signals.",
		},
		{
			RuleID:   "unreachable_private_symbol",
			Category: "dead_code",
			File:     "reader/dead.ts",
			Line:     4,
			Symbol:   "unusedHelper",
			Score:    88,
			Detail:   "unusedHelper has no incoming references.",
		},
	})

	presentation := buildScoutPresentation(items, "grouped")
	if len(presentation.Lanes.SkillTargets[targetSmallComposableCode]) != 2 {
		t.Fatalf("small composable lane=%#v want 2 items", presentation.Lanes.SkillTargets[targetSmallComposableCode])
	}
	if len(presentation.Lanes.SkillTargets[targetSemanticCommenting]) != 1 {
		t.Fatalf("semantic commenting lane=%#v want 1 item", presentation.Lanes.SkillTargets[targetSemanticCommenting])
	}
	if presentation.Lanes.SkillTargets[targetImproveCodebaseArchitecture][0].RepresentativeRule != "function_hotspot" {
		t.Fatalf("architecture target lane=%#v want function_hotspot first", presentation.Lanes.SkillTargets[targetImproveCodebaseArchitecture])
	}
}

func TestBuildScoutPresentationIncludesDBAccessLane(t *testing.T) {
	items := []finding{
		{
			RuleID:   "preload_after_get_chain",
			Category: "function",
			File:     "praze/moderation/content_report_triage.ex",
			Line:     104,
			Symbol:   "resolve_source",
			Score:    74,
			Detail:   "resolve_source uses Repo.get/get_by and then immediately pipes into Repo.preload.",
			Evidence: map[string]any{
				"chain_samples": []string{"Repo.get(Offering, target_id) |> Repo.preload(:media_assets)"},
			},
		},
		{
			RuleID:   "transaction_script_hotspot",
			Category: "function",
			File:     "praze/bible.ex",
			Line:     238,
			Symbol:   "stream_version_verses",
			Score:    82,
			Detail:   "stream_version_verses runs a multi-step anonymous transaction body.",
			Evidence: map[string]any{
				"repo_calls":     []string{"Repo.stream", "Repo.rollback"},
				"script_preview": "Repo.transaction(fn -> ... end)",
			},
		},
	}

	presentation := buildScoutPresentation(items, "grouped")
	if len(presentation.Lanes.DBAccessPatterns) != 2 {
		t.Fatalf("db access lane=%#v want 2 items", presentation.Lanes.DBAccessPatterns)
	}
	if presentation.Lanes.DBAccessPatterns[0].RepresentativeRule != "transaction_script_hotspot" {
		t.Fatalf("top db lane rule=%q want transaction_script_hotspot", presentation.Lanes.DBAccessPatterns[0].RepresentativeRule)
	}
	if len(presentation.Lanes.DBAccessPatterns[0].Samples) == 0 {
		t.Fatalf("expected db lane samples, got %#v", presentation.Lanes.DBAccessPatterns[0])
	}
}

func TestSummarizeFilesDeterministicOrderingAndLimit(t *testing.T) {
	items := []finding{
		{RuleID: "receiver_hotspot", File: "b.go", Symbol: "B", Score: 90},
		{RuleID: "god_file", File: "a.go", Symbol: "A", Score: 90},
		{RuleID: "fan_out_dependency_spread", File: "a.go", Score: 90},
		{RuleID: "deep_nesting", File: "c.go", Symbol: "C", Score: 70},
	}

	got := summarizeFiles(items, 2)
	if len(got) != 2 {
		t.Fatalf("len(got)=%d want 2 (%#v)", len(got), got)
	}
	if got[0].File != "a.go" || got[1].File != "b.go" {
		t.Fatalf("unexpected summary order: %#v", got)
	}
	if got[0].TopSymbol != "A" {
		t.Fatalf("top symbol=%q want A", got[0].TopSymbol)
	}
	if len(got[0].DominantRules) != 2 || got[0].DominantRules[0] != "fan_out_dependency_spread" || got[0].DominantRules[1] != "god_file" {
		t.Fatalf("dominant rules=%#v want [fan_out_dependency_spread god_file]", got[0].DominantRules)
	}
}

func TestSummarizeSymbolsDeterministicOrderingAndLimit(t *testing.T) {
	items := []finding{
		{RuleID: "z_rule", File: "b.go", Symbol: "Zed", Line: 0, Score: 90},
		{RuleID: "a_rule", File: "b.go", Symbol: "Zed", Line: 22, Score: 85},
		{RuleID: "m_rule", File: "a.go", Symbol: "Alpha", Line: 5, Score: 90},
		{RuleID: "q_rule", File: "a.go", Symbol: "Alpha", Line: 5, Score: 70},
		{RuleID: "n_rule", File: "c.go", Symbol: "Other", Line: 1, Score: 60},
	}

	got := summarizeSymbols(items, 2)
	if len(got) != 2 {
		t.Fatalf("len(got)=%d want 2 (%#v)", len(got), got)
	}
	if got[0].File != "a.go" || got[0].Symbol != "Alpha" {
		t.Fatalf("first symbol summary=%#v want a.go/Alpha", got[0])
	}
	if got[1].File != "b.go" || got[1].Symbol != "Zed" {
		t.Fatalf("second symbol summary=%#v want b.go/Zed", got[1])
	}
	if got[1].Line != 22 {
		t.Fatalf("line=%d want 22", got[1].Line)
	}
	if len(got[1].RuleIDs) != 2 || got[1].RuleIDs[0] != "a_rule" || got[1].RuleIDs[1] != "z_rule" {
		t.Fatalf("rule ids=%#v want [a_rule z_rule]", got[1].RuleIDs)
	}
}

func TestBuildEntrypointLaneRepresentativePrefersHotspotAtEqualScore(t *testing.T) {
	items := []finding{
		{
			RuleID:   "high_cyclomatic_complexity",
			Category: "function",
			File:     "pkg/worker.go",
			Line:     10,
			Symbol:   "Run",
			Score:    90,
		},
		{
			RuleID:   "function_hotspot",
			Category: "function",
			File:     "pkg/worker.go",
			Line:     10,
			Symbol:   "Run",
			Score:    90,
		},
	}

	lane := buildEntrypointLane(items, 10)
	if len(lane) != 1 {
		t.Fatalf("lane=%#v want 1 item", lane)
	}
	if lane[0].RepresentativeRule != "function_hotspot" {
		t.Fatalf("representative rule=%q want function_hotspot", lane[0].RepresentativeRule)
	}
	if lane[0].FindingCount != 2 {
		t.Fatalf("finding_count=%d want 2", lane[0].FindingCount)
	}
}

func TestBuildSkillTargetLaneDeduplicatesAndAppliesLimit(t *testing.T) {
	items := []finding{
		{
			RuleID:        "function_hotspot",
			Category:      "function",
			File:          "pkg/a.go",
			Line:          10,
			Symbol:        "DoThing",
			Score:         95,
			Targets:       []string{targetSmallComposableCode},
			TargetReasons: []string{"alpha", "beta", "gamma", "delta"},
		},
		{
			RuleID:   "function_hotspot",
			Category: "function",
			File:     "pkg/a.go",
			Line:     12,
			Symbol:   "DoThing",
			Score:    80,
			Targets:  []string{targetSmallComposableCode},
		},
		{
			RuleID:   "unreachable_private_symbol",
			Category: "dead_code",
			File:     "pkg/b.go",
			Line:     4,
			Symbol:   "unused",
			Score:    88,
			Targets:  []string{targetSmallComposableCode},
		},
	}

	full := buildSkillTargetLane(items, targetSmallComposableCode, 10)
	if len(full) != 2 {
		t.Fatalf("lane=%#v want 2 deduplicated items", full)
	}
	if len(full[0].Samples) != 3 || full[0].Samples[0] != "alpha" || full[0].Samples[1] != "beta" || full[0].Samples[2] != "gamma" {
		t.Fatalf("samples=%#v want [alpha beta gamma]", full[0].Samples)
	}

	limited := buildSkillTargetLane(items, targetSmallComposableCode, 1)
	if len(limited) != 1 {
		t.Fatalf("limited lane=%#v want 1 item", limited)
	}
	if limited[0].RepresentativeRule != "function_hotspot" {
		t.Fatalf("limited top rule=%q want function_hotspot", limited[0].RepresentativeRule)
	}
}

func TestBuildDBAccessLaneCollapsesTransactionVariantsPerFunction(t *testing.T) {
	items := []finding{
		{
			RuleID:   "transaction_script_hotspot",
			Category: "function",
			File:     "praze/trust.ex",
			Line:     140,
			Symbol:   "request_export",
			Score:    90,
			Detail:   "request_export runs a multi-step anonymous transaction body.",
			Evidence: map[string]any{
				"script_preview": "Multi.new() |> Multi.insert(...) |> Repo.transaction()",
				"repo_calls":     []string{"Repo.transaction"},
			},
		},
		{
			RuleID:   "transaction_script_hotspot",
			Category: "function",
			File:     "praze/trust.ex",
			Line:     198,
			Symbol:   "request_export",
			Score:    88,
			Detail:   "request_export runs a second transaction-like pipeline.",
			Evidence: map[string]any{
				"script_preview": "Multi.new() |> Multi.update(...) |> Repo.transaction()",
				"repo_calls":     []string{"Repo.transaction"},
			},
		},
	}

	lane := buildDBAccessLane(items, 10)
	if len(lane) != 1 {
		t.Fatalf("lane=%#v want 1 item", lane)
	}
	if lane[0].FindingCount != 1 {
		t.Fatalf("finding_count=%d want 1 representative candidate", lane[0].FindingCount)
	}
	if len(lane[0].Samples) < 2 {
		t.Fatalf("samples=%#v want both transaction previews retained", lane[0].Samples)
	}
}

func TestClassifyStructuralClusterWorkflowAbstraction(t *testing.T) {
	profile := classifyStructuralCluster(structuralSimilarityCluster{
		AverageBranches:  3,
		AverageCallSites: 10,
		AverageFanOut:    6,
	})
	if profile.Kind != "workflow_abstraction" {
		t.Fatalf("expected workflow_abstraction, got %#v", profile)
	}
}

func TestClassifyStructuralClusterThinWrapperAPILayer(t *testing.T) {
	profile := classifyStructuralCluster(structuralSimilarityCluster{
		AdapterSurfaceScore: 78,
		AverageBranches:     1,
	})
	if profile.Kind != "thin_wrapper_api_layer" {
		t.Fatalf("expected thin_wrapper_api_layer, got %#v", profile)
	}
}

func TestClassifyStructuralClusterSharedOperationFamilyFallback(t *testing.T) {
	profile := classifyStructuralCluster(structuralSimilarityCluster{
		AverageBranches:  1,
		AverageCallSites: 6,
		AverageFanOut:    6,
	})
	if profile.Kind != "shared_operation_family" {
		t.Fatalf("expected shared_operation_family, got %#v", profile)
	}
}

func TestFinalizeStructuralSimilarityClusterFindingsAddsModuleCluster(t *testing.T) {
	leftKey := observationKey("pkg/one.go", "RunOne")
	rightKey := observationKey("pkg/two.go", "RunTwo")
	leftFingerprint := "assign(call_ident_1);if(bin(!=,ident,lit)){return(call_ident_1)};call_selector_2;return()"
	rightFingerprint := "assign(call_ident_1);if(bin(!=,ident,lit)){call_selector_1;return(call_ident_1)};call_selector_2;return()"
	state := &scoutState{
		Thresholds: thresholdsFor("default"),
		Symbols: map[string]*symbolObservation{
			leftKey: {
				File:                     "pkg/one.go",
				Line:                     10,
				Symbol:                   "RunOne",
				Language:                 "go",
				Kind:                     symindex.KindFunction,
				FanOut:                   6,
				BranchCount:              2,
				CallSiteCount:            9,
				FunctionLines:            80,
				OrchestrationFingerprint: leftFingerprint,
				OrchestrationTokens:      tokenizeStructuralFingerprint(leftFingerprint),
			},
			rightKey: {
				File:                     "pkg/two.go",
				Line:                     15,
				Symbol:                   "RunTwo",
				Language:                 "go",
				Kind:                     symindex.KindFunction,
				FanOut:                   6,
				BranchCount:              2,
				CallSiteCount:            9,
				FunctionLines:            78,
				OrchestrationFingerprint: rightFingerprint,
				OrchestrationTokens:      tokenizeStructuralFingerprint(rightFingerprint),
			},
		},
		FileSymbols: map[string][]string{
			"pkg/one.go": {leftKey},
			"pkg/two.go": {rightKey},
		},
	}
	peerMap := similarOrchestrationPeers(state)
	finalizeStructuralSimilarityClusterFindings(state, peerMap)

	found := false
	for _, item := range state.Findings {
		if item.RuleID != "structural_similarity_module_cluster" {
			continue
		}
		found = true
		if item.File != "pkg/one.go" {
			t.Fatalf("expected representative file pkg/one.go, got %s", item.File)
		}
		if item.Evidence["scope_path"] != "pkg" {
			t.Fatalf("expected scope_path pkg, got %#v", item.Evidence["scope_path"])
		}
		if item.Evidence["unique_file_count"] != 2 {
			t.Fatalf("expected unique_file_count 2, got %#v", item.Evidence["unique_file_count"])
		}
		if item.Evidence["seam_kind"] != "workflow_abstraction" {
			t.Fatalf("expected seam_kind workflow_abstraction, got %#v", item.Evidence["seam_kind"])
		}
	}
	if !found {
		t.Fatalf("expected structural_similarity_module_cluster, got %#v", state.Findings)
	}
}

func TestScoreAdapterSurfaceClusterRewardsUniformReceiverSurface(t *testing.T) {
	score := scoreAdapterSurfaceCluster(6, 4, 100, 1, 2, 2, 12, 0, 0)
	if score < 70 {
		t.Fatalf("expected adapter surface score >= 70, got %d", score)
	}
}

func TestScoreAdapterSurfaceClusterDoesNotRewardMixedWorkflowShape(t *testing.T) {
	score := scoreAdapterSurfaceCluster(3, 3, 33, 6, 8, 7, 70, 2, 1)
	if score >= 70 {
		t.Fatalf("expected mixed workflow shape to stay below wrapper threshold, got %d", score)
	}
}

func TestCallFamilySimilarityDetailsUsesSharedCalls(t *testing.T) {
	left := &symbolObservation{
		Language:    "typescript",
		FanOut:      5,
		SymbolLines: 18,
		ParamCount:  2,
		ReturnCount: 1,
		Calls:       []string{"client.get", "normalize", "mapRow", "sortBy", "toView"},
	}
	right := &symbolObservation{
		Language:    "typescript",
		FanOut:      4,
		SymbolLines: 16,
		ParamCount:  2,
		ReturnCount: 1,
		Calls:       []string{"client.get", "normalize", "mapRow", "toView"},
	}
	details := callFamilySimilarityDetailsFor(&scoutState{}, left, right)
	if details.Score < callFamilySimilarityThreshold(thresholdsFor("default")) {
		t.Fatalf("expected call family similarity above threshold, got %#v", details)
	}
	if len(details.SharedCalls) < 3 {
		t.Fatalf("expected shared calls evidence, got %#v", details)
	}
}

func TestSupportsObservedFunctionSignalsRecognizesTSConstArrow(t *testing.T) {
	content := []byte("export const View = (\n  props: Props,\n) => renderView(props)\n")
	sym := symindex.Symbol{
		Name:      "View",
		Language:  "typescript",
		Kind:      symindex.KindConstant,
		StartByte: 0,
		EndByte:   len(content),
		Signature: "export const View = (",
	}
	if !supportsObservedFunctionSignals(sym, "typescript", content) {
		t.Fatal("expected const arrow symbol to participate in observation")
	}
}

func TestFinalizeCallFamilyClusterFindingsAddsFileCluster(t *testing.T) {
	leftKey := observationKey("pkg/client.ts", "patchRoom")
	rightKey := observationKey("pkg/client.ts", "patchRoomMembers")
	state := &scoutState{
		Thresholds: thresholdsFor("default"),
		Symbols: map[string]*symbolObservation{
			leftKey: {
				File:        "pkg/client.ts",
				Line:        10,
				Symbol:      "patchRoom",
				Language:    "typescript",
				Kind:        symindex.KindFunction,
				FanOut:      5,
				SymbolLines: 18,
				ParamCount:  2,
				ReturnCount: 1,
				Calls:       []string{"client.patch", "buildPayload", "normalize", "toView", "cache.update"},
			},
			rightKey: {
				File:        "pkg/client.ts",
				Line:        40,
				Symbol:      "patchRoomMembers",
				Language:    "typescript",
				Kind:        symindex.KindFunction,
				FanOut:      5,
				SymbolLines: 19,
				ParamCount:  2,
				ReturnCount: 1,
				Calls:       []string{"client.patch", "buildPayload", "normalize", "toView", "cache.update"},
			},
		},
		FileSymbols: map[string][]string{
			"pkg/client.ts": {leftKey, rightKey},
		},
	}
	peerMap := similarCallFamilyPeers(state)
	finalizeCallFamilyClusterFindings(state, peerMap)

	found := false
	for _, item := range state.Findings {
		if item.RuleID != "call_family_cluster" {
			continue
		}
		found = true
		if item.Evidence["similarity_mode"] != "call_family" {
			t.Fatalf("expected call_family similarity_mode, got %#v", item.Evidence["similarity_mode"])
		}
		if item.Evidence["shared_calls"] == nil {
			t.Fatalf("expected shared_calls evidence, got %#v", item.Evidence)
		}
	}
	if !found {
		t.Fatalf("expected call_family_cluster, got %#v", state.Findings)
	}
}

func TestClassifyCallFamilyClusterElixirThinWrapper(t *testing.T) {
	profile := classifyCallFamilyCluster(callFamilyCluster{
		Members:             []*symbolObservation{{Language: "elixir"}},
		AdapterSurfaceScore: 80,
		AverageFanOut:       3,
		AverageSymbolLines:  13,
		StrongestDetail: callFamilySimilarityDetails{
			DistinctivenessScore: 30,
		},
	})
	if profile.Kind != "thin_wrapper_api_layer" {
		t.Fatalf("expected thin_wrapper_api_layer, got %#v", profile)
	}
}

func TestClassifyCallFamilyClusterElixirSharedOperation(t *testing.T) {
	profile := classifyCallFamilyCluster(callFamilyCluster{
		Members:             []*symbolObservation{{Language: "elixir"}},
		AdapterSurfaceScore: 80,
		AverageFanOut:       2,
		AverageSymbolLines:  16,
		StrongestDetail: callFamilySimilarityDetails{
			DistinctivenessScore: 47,
		},
	})
	if profile.Kind != "shared_operation_family" {
		t.Fatalf("expected shared_operation_family, got %#v", profile)
	}
}

func TestFinalizeCallFamilyClusterFindingsSuppressesWeakTopLevelPythonModuleSeam(t *testing.T) {
	leftKey := observationKey("scripts/a.py", "main")
	rightKey := observationKey("scripts/b.py", "main")
	state := &scoutState{
		Thresholds: thresholdsFor("default"),
		Symbols: map[string]*symbolObservation{
			leftKey: {
				File:        "scripts/a.py",
				Line:        10,
				Symbol:      "main",
				Language:    "python",
				Kind:        symindex.KindFunction,
				FanOut:      4,
				SymbolLines: 14,
				ParamCount:  1,
				ReturnCount: 0,
				Calls:       []string{"connect", "execute", "print", "close"},
			},
			rightKey: {
				File:        "scripts/b.py",
				Line:        12,
				Symbol:      "main",
				Language:    "python",
				Kind:        symindex.KindFunction,
				FanOut:      4,
				SymbolLines: 15,
				ParamCount:  1,
				ReturnCount: 0,
				Calls:       []string{"connect", "execute", "print", "close"},
			},
		},
		FileSymbols: map[string][]string{
			"scripts/a.py": {leftKey},
			"scripts/b.py": {rightKey},
		},
	}
	peerMap := similarCallFamilyPeers(state)
	finalizeCallFamilyClusterFindings(state, peerMap)
	for _, item := range state.Findings {
		if item.RuleID == "call_family_module_cluster" {
			t.Fatalf("expected weak top-level python module seam to be suppressed, got %#v", item)
		}
	}
}

func TestCallFamilySimilarityDetailsBoostsElixirNamespaceAndRarity(t *testing.T) {
	state := &scoutState{
		CallFrequency: map[string]map[string]int{
			"elixir": {
				"Enum":       12,
				"String":     10,
				"System":     8,
				"Praze.Repo": 1,
				"Moderation": 2,
			},
		},
	}
	left := &symbolObservation{
		File:        "apps/praze-api/lib/praze/moderation/content_report_triage.ex",
		Language:    "elixir",
		FanOut:      6,
		SymbolLines: 24,
		ParamCount:  2,
		ReturnCount: 1,
		Calls:       []string{"Enum", "String", "Praze.Repo", "Moderation"},
	}
	right := &symbolObservation{
		File:        "apps/praze-api/lib/praze/moderation/openrouter_audio_review.ex",
		Language:    "elixir",
		FanOut:      5,
		SymbolLines: 22,
		ParamCount:  2,
		ReturnCount: 1,
		Calls:       []string{"Enum", "String", "Praze.Repo", "Moderation"},
	}
	details := callFamilySimilarityDetailsFor(state, left, right)
	if details.NamespaceScore < 50 {
		t.Fatalf("expected strong elixir namespace score, got %#v", details)
	}
	if details.DistinctivenessScore <= 0 {
		t.Fatalf("expected positive distinctiveness score, got %#v", details)
	}
}

func TestAllowCallFamilyModuleClusterSuppressesWeakRootElixirCluster(t *testing.T) {
	cluster := callFamilyCluster{
		Members: []*symbolObservation{
			{Language: "elixir"},
			{Language: "elixir"},
		},
		UniqueFileCount:    5,
		AverageSimilarity:  82,
		AverageFanOut:      6,
		AverageSymbolLines: 20,
		StrongestDetail: callFamilySimilarityDetails{
			NamespaceScore:       0,
			DistinctivenessScore: 25,
		},
	}
	if allowCallFamilyModuleCluster(cluster) {
		t.Fatalf("expected weak root-level elixir cluster to be suppressed: %#v", cluster)
	}
}

func TestElixirModuleClusterScopeSuppressesRootLibFiles(t *testing.T) {
	scope := elixirModuleClusterScope("apps/praze-api/lib/praze/audio_storage.ex")
	if scope != "" {
		t.Fatalf("expected no module cluster scope for root lib file, got %q", scope)
	}
}

func TestElixirModuleClusterScopeKeepsNestedNamespace(t *testing.T) {
	scope := elixirModuleClusterScope("apps/praze-api/lib/praze/moderation/content_report_triage.ex")
	if scope != "apps/praze-api/lib/praze/moderation" {
		t.Fatalf("unexpected nested elixir scope: %q", scope)
	}
}

func TestSuppressRedundantClusterFindingsDropsContainedFileCluster(t *testing.T) {
	state := &scoutState{
		Findings: []finding{
			{
				RuleID:   "call_family_module_cluster",
				Score:    100,
				File:     "apps/praze-api/lib/praze_web/controllers/attention_controller.ex",
				Category: "cluster",
				Evidence: map[string]any{
					"similarity_mode": "call_family",
					"seam_kind":       "thin_wrapper_api_layer",
					"scope_path":      "apps/praze-api/lib/praze_web/controllers",
					"member_symbols": []string{
						"notifications_index",
						"mark_notification_read",
						"circle_items",
						"create_highlight",
					},
				},
			},
			{
				RuleID:   "call_family_cluster",
				Score:    93,
				File:     "apps/praze-api/lib/praze_web/controllers/reader_controller.ex",
				Category: "cluster",
				Evidence: map[string]any{
					"similarity_mode": "call_family",
					"seam_kind":       "thin_wrapper_api_layer",
					"scope_path":      "apps/praze-api/lib/praze_web/controllers/reader_controller.ex",
					"member_symbols": []string{
						"circle_items",
						"create_highlight",
					},
				},
			},
			{
				RuleID:   "call_family_cluster",
				Score:    87,
				File:     "apps/praze-api/lib/praze_web/controllers/attention_controller.ex",
				Category: "cluster",
				Evidence: map[string]any{
					"similarity_mode": "call_family",
					"seam_kind":       "shared_operation_family",
					"scope_path":      "apps/praze-api/lib/praze_web/controllers/attention_controller.ex",
					"member_symbols": []string{
						"notifications_index",
						"mark_notification_read",
					},
				},
			},
		},
	}
	suppressRedundantClusterFindings(state)
	if len(state.Findings) != 2 {
		t.Fatalf("expected one contained file cluster to be removed, got %#v", state.Findings)
	}
	for _, item := range state.Findings {
		if item.RuleID == "call_family_cluster" && item.File == "apps/praze-api/lib/praze_web/controllers/reader_controller.ex" {
			t.Fatalf("expected contained reader_controller cluster to be suppressed: %#v", state.Findings)
		}
	}
}

func TestObservationKeyNormalizesPointerReceiver(t *testing.T) {
	left := observationKey("a.go", "*Worker.Run")
	right := observationKey("a.go", "Worker.Run")
	if left != right {
		t.Fatalf("expected pointer receiver normalization, got %q and %q", left, right)
	}
}

func TestAgentGoHasStructurallySimilarRoutePair(t *testing.T) {
	content, err := os.ReadFile("../../cmd/foxctl/cmd/agent.go")
	if err != nil {
		t.Fatalf("read agent.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "agent.go", content, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse agent.go: %v", err)
	}

	state := &scoutState{
		Thresholds:  thresholdsFor("default"),
		Symbols:     make(map[string]*symbolObservation),
		FileSymbols: make(map[string][]string),
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		_ = analyzeGoFuncDecl(fn, fset, "cmd/foxctl/cmd/agent.go", state)
	}

	candidates := []string{
		"runAgentSpawnWithRoute",
		"runAgentWatch",
		"runAgentAskWithRoute",
		"runAgentKillWithRoute",
		"runAgentCmd",
	}
	bestScore := 0
	bestPair := ""
	for i := 0; i < len(candidates); i++ {
		left := state.Symbols[observationKey("cmd/foxctl/cmd/agent.go", candidates[i])]
		if left == nil {
			continue
		}
		for j := i + 1; j < len(candidates); j++ {
			right := state.Symbols[observationKey("cmd/foxctl/cmd/agent.go", candidates[j])]
			if right == nil {
				continue
			}
			score := orchestrationSimilarityScore(left, right)
			if score > bestScore {
				bestScore = score
				bestPair = left.Symbol + " <-> " + right.Symbol
			}
		}
	}
	if bestScore < orchestrationSimilarityThreshold(state.Thresholds) {
		t.Fatalf("expected at least one structurally similar pair in agent.go route functions, best=%q score=%d", bestPair, bestScore)
	}
}
