// Package main implements the code/refactor_scout skill.
package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/fsutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/hashutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/langutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/pathutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	symindex "github.com/jkatigb/agentctl/internal/indexing/symbol"
	refscope "github.com/jkatigb/agentctl/internal/intelligence/refactor/scope"
	refstatus "github.com/jkatigb/agentctl/internal/intelligence/refactor/status"
)

const command = "code/refactor_scout"

const (
	headDiversifyMinItems = 30
	headMaxPerRule        = 2
	headMaxPerFile        = 2
	headMaxPerSymbol      = 1
)

type input struct {
	Path         string `json:"path"`
	Language     string `json:"language" validate:"omitempty,oneof=auto go python javascript typescript elixir rust"`
	Focus        string `json:"focus" validate:"omitempty,oneof=all slop dead"`
	View         string `json:"view" validate:"omitempty,oneof=raw grouped entrypoints summary"`
	IncludeTests bool   `json:"include_tests"`
	MaxResults   int    `json:"max_results" validate:"gte=0"`
	MinScore     int    `json:"min_score" validate:"gte=0,lte=100"`
	RuleSet      string `json:"rule_set" validate:"omitempty,oneof=conservative default aggressive"`
}

type finding struct {
	RuleID            string         `json:"rule_id"`
	Category          string         `json:"category"`
	Severity          string         `json:"severity"`
	Score             int            `json:"score"`
	Title             string         `json:"title"`
	Detail            string         `json:"detail"`
	SuggestedRefactor string         `json:"suggested_refactor,omitempty"`
	File              string         `json:"file"`
	Line              int            `json:"line,omitempty"`
	Symbol            string         `json:"symbol,omitempty"`
	Language          string         `json:"language"`
	Confidence        string         `json:"confidence,omitempty"`
	Signals           []string       `json:"signals,omitempty"`
	Evidence          map[string]any `json:"evidence,omitempty"`
}

type thresholds struct {
	ParamCount          int
	ReturnCount         int
	SymbolLines         int
	FunctionLines       int
	Cyclomatic          int
	Nesting             int
	InterfaceMethods    int
	FileSymbols         int
	FileLines           int
	ReceiverMethods     int
	MinFileComplexity   int
	FanOutCalls         int
	DuplicateCallSites  int
	DuplicateBranches   int
	SameFileSharedCalls int
}

type signatureMetrics struct {
	ParamCount     int
	BoolParamCount int
	ReturnCount    int
}

type receiverHotspot struct {
	Count    int
	File     string
	Line     int
	Language string
}

type symbolObservation struct {
	File                     string
	Line                     int
	Symbol                   string
	Language                 string
	Kind                     symindex.Kind
	Signature                string
	ParamCount               int
	BoolParamCount           int
	ReturnCount              int
	SymbolLines              int
	Calls                    []string
	FanOut                   int
	FunctionLines            int
	Cyclomatic               int
	Nesting                  int
	BranchCount              int
	CallSiteCount            int
	OrchestrationFingerprint string
	OrchestrationTokens      []string
}

type scoutState struct {
	Workspace        string
	Thresholds       thresholds
	Registry         *symindex.ExtractorRegistry
	Findings         []finding
	ScannedFiles     int
	ScannedSymbols   int
	ScannedLanguages map[string]int
	ReceiverMethods  map[string]receiverHotspot
	Symbols          map[string]*symbolObservation
	FileSymbols      map[string][]string
	CallFrequency    map[string]map[string]int
}

type languageScope struct {
	Mode     string   `json:"mode"`
	Language string   `json:"language"`
	Detected []string `json:"detected,omitempty"`
}

type scoutPresentation struct {
	View       string                 `json:"view"`
	ActiveLane string                 `json:"active_lane,omitempty"`
	Overview   scoutPresentationMeta  `json:"overview"`
	Lanes      scoutPresentationLanes `json:"lanes"`
}

type scoutPresentationMeta struct {
	TopRuleFamilies []scoutRuleFamilySummary `json:"top_rule_families,omitempty"`
	TopFiles        []scoutFileSummary       `json:"top_files,omitempty"`
	TopSymbols      []scoutSymbolSummary     `json:"top_symbols,omitempty"`
	NoiseIndicators []scoutNoiseIndicator    `json:"noise_indicators,omitempty"`
}

type scoutRuleFamilySummary struct {
	RuleID   string  `json:"rule_id"`
	Count    int     `json:"count"`
	MaxScore int     `json:"max_score"`
	Share    float64 `json:"share"`
}

type scoutFileSummary struct {
	File          string   `json:"file"`
	Count         int      `json:"count"`
	MaxScore      int      `json:"max_score"`
	TopSymbol     string   `json:"top_symbol,omitempty"`
	DominantRules []string `json:"dominant_rules,omitempty"`
}

type scoutSymbolSummary struct {
	File     string   `json:"file"`
	Symbol   string   `json:"symbol"`
	Line     int      `json:"line,omitempty"`
	Count    int      `json:"count"`
	MaxScore int      `json:"max_score"`
	RuleIDs  []string `json:"rule_ids,omitempty"`
}

type scoutNoiseIndicator struct {
	Kind   string  `json:"kind"`
	Detail string  `json:"detail"`
	Count  int     `json:"count,omitempty"`
	Share  float64 `json:"share,omitempty"`
}

type scoutPresentationLanes struct {
	BestEntrypoints       []scoutLaneItem `json:"best_entrypoints,omitempty"`
	DBAccessPatterns      []scoutLaneItem `json:"db_access_patterns,omitempty"`
	RepeatedPatternFamily []scoutLaneItem `json:"repeated_pattern_families,omitempty"`
	ModuleSeams           []scoutLaneItem `json:"module_seams,omitempty"`
	Backlog               []scoutLaneItem `json:"backlog,omitempty"`
}

type scoutLaneItem struct {
	File               string   `json:"file,omitempty"`
	Symbol             string   `json:"symbol,omitempty"`
	Line               int      `json:"line,omitempty"`
	MaxScore           int      `json:"max_score"`
	FindingCount       int      `json:"finding_count"`
	RepresentativeRule string   `json:"representative_rule"`
	RuleIDs            []string `json:"rule_ids,omitempty"`
	Summary            string   `json:"summary"`
	Category           string   `json:"category,omitempty"`
	SeamKind           string   `json:"seam_kind,omitempty"`
	ScopePath          string   `json:"scope_path,omitempty"`
	Samples            []string `json:"samples,omitempty"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	if in.Language == "" {
		in.Language = "auto"
	}
	if in.Focus == "" {
		in.Focus = "all"
	}
	if in.View == "" {
		in.View = "grouped"
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 100
	}
	if in.MinScore <= 0 {
		in.MinScore = 50
	}
	if in.RuleSet == "" {
		in.RuleSet = "default"
	}

	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	info, err := os.Stat(searchPath)
	if err != nil {
		return skillerr.WrapIO("stat path", err)
	}

	scope, err := resolveLanguageScope(workspace, searchPath, info, in)
	if err != nil {
		return err
	}
	in.Language = scope.Language

	state := &scoutState{
		Workspace:        workspace,
		Thresholds:       thresholdsFor(in.RuleSet),
		Registry:         symindex.DefaultRegistry(),
		ScannedLanguages: make(map[string]int),
		ReceiverMethods:  make(map[string]receiverHotspot),
		Symbols:          make(map[string]*symbolObservation),
		FileSymbols:      make(map[string][]string),
		CallFrequency:    make(map[string]map[string]int),
	}

	if info.IsDir() {
		if err := analyzeDirectory(ctx, searchPath, in, state); err != nil {
			return err
		}
	} else {
		if err := analyzeFile(ctx, searchPath, workspace, in, state); err != nil {
			return err
		}
	}

	finalizeObservationFindings(state)
	finalizeReceiverHotspots(state)
	hotspotSymbols := synthesizeCompositeFindings(state)
	if shouldSuppressConstituentFindings(in.Focus) {
		suppressConstituentFindings(state, hotspotSymbols)
	}

	statusScope := buildScoutStatusScope(workspace, searchPath, info, scope, in.IncludeTests)
	indexStatus := refstatus.Evaluate(ctx, rc.Config.Storage.Root, statusScope)
	effectiveScope := indexStatus.Scope

	allFindings := append([]finding(nil), state.Findings...)
	deadCodeFindings, deadCodeErr := buildDeadCodeFindings(ctx, rc.Config.Storage.Root, effectiveScope, indexStatus, in.Focus)
	if deadCodeErr == nil {
		allFindings = append(allFindings, deadCodeFindings...)
	}
	allFindings = applyFocus(allFindings, in.Focus)

	filtered := make([]finding, 0, len(allFindings))
	for _, item := range allFindings {
		if item.Score >= in.MinScore {
			filtered = append(filtered, item)
		}
	}
	sortFindings(filtered)
	filtered = diversifyFindings(filtered, maxInt(headDiversifyMinItems, in.MaxResults*3), headMaxPerRule, headMaxPerFile, headMaxPerSymbol)

	evidence, evidenceErr := buildScoutEvidence(ctx, rc, in, effectiveScope, indexStatus, filtered)
	if evidenceErr == nil {
		filtered = evidence.Findings
	}
	filtered = applyConfidenceScores(filtered, indexStatus.Mode)
	sortFindings(filtered)

	totalFindings := len(filtered)
	summary := buildSummary(filtered)
	presentation := buildScoutPresentation(filtered, in.View)
	limitedByMaxResults := false
	if len(filtered) > in.MaxResults {
		filtered = filtered[:in.MaxResults]
		limitedByMaxResults = true
	}
	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, filtered, rc.MaxPreview, "code_refactor_scout", true)
	if err != nil {
		return err
	}

	data := map[string]any{
		"findings":       previewResult.Preview,
		"language_scope": scope,
		"index_mode":     string(indexStatus.Mode),
		"view":           in.View,
		"presentation":   presentation,
		"summary": map[string]any{
			"scanned_files":      state.ScannedFiles,
			"scanned_symbols":    state.ScannedSymbols,
			"languages":          state.ScannedLanguages,
			"finding_count":      totalFindings,
			"returned_findings":  len(filtered),
			"severity_counts":    summary,
			"limited_by_results": limitedByMaxResults,
		},
		"rule_set": in.RuleSet,
		"focus":    in.Focus,
		"signals": map[string]any{
			"symbol_signatures": true,
			"go_ast":            true,
			"call_extraction":   true,
			"slop_focus":        in.Focus == "slop",
			"dead_focus":        in.Focus == "dead",
			"repo_graph":        indexStatus.Mode == refstatus.ModeIndexBacked,
			"evidence_backed":   evidenceErr == nil,
		},
	}
	if deadCodeErr != nil {
		data["dead_code_error"] = deadCodeErr.Error()
	}
	if evidenceErr == nil {
		data["snapshot_id"] = evidence.SnapshotID
		data["snapshot_artifact"] = evidence.SnapshotArtifact
		if strings.TrimSpace(evidence.EvidenceArtifact) != "" {
			data["evidence_artifact"] = evidence.EvidenceArtifact
		}
	} else {
		data["evidence_error"] = evidenceErr.Error()
	}
	skillout.AddArtifact(data, previewResult.Artifact)

	return skillout.Emit(rc, command, data)
}

func resolveLanguageScope(workspace, searchPath string, info fs.FileInfo, in input) (languageScope, error) {
	resolved, err := refscope.ResolveResolvedPath(workspace, searchPath, info, in.Language, in.IncludeTests)
	if err != nil {
		if resolveErr, ok := err.(*refscope.ResolveError); ok {
			return languageScope{}, skillerr.Validation(resolveErr.Message, skillerr.WithHint(resolveErr.Hint))
		}
		return languageScope{}, skillerr.WrapIO("resolve language scope", err)
	}
	return languageScope{
		Mode:     resolved.Mode,
		Language: resolved.Language,
		Detected: resolved.Detected,
	}, nil
}

func buildScoutStatusScope(workspace, searchPath string, info fs.FileInfo, scope languageScope, includeTests bool) refscope.Scope {
	return refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         pathutil.RelativePath(searchPath, workspace),
		Absolute:     searchPath,
		Mode:         scope.Mode,
		Language:     scope.Language,
		Detected:     append([]string(nil), scope.Detected...),
		IsDir:        info != nil && info.IsDir(),
		IncludeTests: includeTests,
	}
}

func analyzeDirectory(ctx context.Context, dir string, in input, state *scoutState) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if fsutil.ShouldSkipHiddenOrCommon(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !in.IncludeTests && fsutil.IsTestFile(d.Name()) {
			return nil
		}
		if analyzeFile(ctx, path, state.Workspace, in, state) != nil {
			return nil
		}
		return nil
	})
}

func analyzeFile(ctx context.Context, path, workspace string, in input, state *scoutState) error {
	lang := langutil.DetectAllowedWithHint(in.Language, path, langutil.CommonCodeLanguages)
	if lang == "" {
		return nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return skillerr.WrapIO("read file", err)
	}
	if fsutil.IsBinaryContent(content) {
		return nil
	}

	relPath := pathutil.RelTo(workspace, path)
	state.ScannedFiles++
	state.ScannedLanguages[lang]++

	extractor := state.Registry.Get(lang)
	if extractor == nil {
		return nil
	}
	symbols, err := extractor.Extract(ctx, relPath, content)
	if err != nil {
		return nil
	}
	state.ScannedSymbols += len(symbols)
	observeSymbols(ctx, extractor, relPath, lang, content, symbols, state)

	applyFileFinding(relPath, lang, content, symbols, state)

	switch lang {
	case "go":
		return analyzeGoFile(path, relPath, content, state)
	case "typescript", "javascript":
		applySignatureFindings(relPath, lang, symbols, state)
		state.Findings = append(state.Findings, analyzeTypeScriptSemanticSimplifications(path, relPath, lang, content, symbols)...)
		state.Findings = append(state.Findings, analyzeTypeScriptDuplicateRecoveryBlocks(path, relPath, lang, content, symbols)...)
		state.Findings = append(state.Findings, analyzeTypeScriptDuplicatedErrorRemaps(path, relPath, lang, content, symbols)...)
		state.Findings = append(state.Findings, analyzeTypeScriptRepeatedGuardLadders(path, relPath, lang, content, symbols)...)
	case "python":
		applySignatureFindings(relPath, lang, symbols, state)
		state.Findings = append(state.Findings, analyzePythonSemanticSimplifications(path, relPath, lang, content, symbols)...)
	case "elixir":
		applySignatureFindings(relPath, lang, symbols, state)
		state.Findings = append(state.Findings, analyzeElixirSemanticSimplifications(path, relPath, lang, content, symbols)...)
		state.Findings = append(state.Findings, analyzeElixirPreloadAfterGetChains(path, relPath, lang, content, symbols)...)
		state.Findings = append(state.Findings, analyzeElixirPostTransactionPreloads(path, relPath, lang, content, symbols)...)
		state.Findings = append(state.Findings, analyzeElixirTransactionScriptHotspots(path, relPath, lang, content, symbols)...)
		state.Findings = append(state.Findings, analyzeElixirDuplicateRecoveryBlocks(path, relPath, lang, content, symbols)...)
		state.Findings = append(state.Findings, analyzeElixirDuplicatedErrorRemaps(path, relPath, lang, content, symbols)...)
		state.Findings = append(state.Findings, analyzeElixirRepeatedGuardLadders(path, relPath, lang, content, symbols)...)
	default:
		applySignatureFindings(relPath, lang, symbols, state)
	}

	return nil
}

func observeSymbols(ctx context.Context, extractor symindex.Extractor, relPath, lang string, content []byte, symbols []symindex.Symbol, state *scoutState) {
	if state == nil || extractor == nil {
		return
	}
	for _, sym := range symbols {
		if !supportsObservedFunctionSignals(sym, lang, content) || strings.TrimSpace(sym.Name) == "" {
			continue
		}
		obs := ensureSymbolObservation(state, relPath, sym.Name, lang, sym.Kind, sym.StartLine)
		obs.Signature = strings.TrimSpace(sym.Signature)
		obs.SymbolLines = spanLength(sym.StartLine, sym.EndLine)
		if metrics := parseSignatureMetrics(sym.Signature, lang); metrics.ParamCount > 0 || metrics.ReturnCount > 0 || metrics.BoolParamCount > 0 {
			obs.ParamCount = metrics.ParamCount
			obs.BoolParamCount = metrics.BoolParamCount
			obs.ReturnCount = metrics.ReturnCount
		}
		calls, err := extractor.ExtractCalls(ctx, sym, content)
		if err != nil {
			continue
		}
		obs.Calls = normalizeCallList(calls)
		obs.FanOut = len(obs.Calls)
		recordObservedCalls(state, lang, obs.Calls)
	}
}

func recordObservedCalls(state *scoutState, lang string, calls []string) {
	if state == nil || len(calls) == 0 {
		return
	}
	if state.CallFrequency == nil {
		state.CallFrequency = make(map[string]map[string]int)
	}
	freq := state.CallFrequency[lang]
	if freq == nil {
		freq = make(map[string]int)
		state.CallFrequency[lang] = freq
	}
	for _, call := range calls {
		call = strings.TrimSpace(call)
		if call == "" {
			continue
		}
		freq[call]++
	}
}

func applyFileFinding(relPath, lang string, content []byte, symbols []symindex.Symbol, state *scoutState) {
	lineCount := countLines(content)
	if lineCount < state.Thresholds.FileLines || len(symbols) < state.Thresholds.FileSymbols {
		return
	}
	score := scoreGodFile(len(symbols), lineCount, state.Thresholds)
	state.Findings = append(state.Findings, finding{
		RuleID:            "god_file",
		Category:          "file",
		Severity:          severityFor(score),
		Score:             score,
		Title:             "File concentrates too many top-level responsibilities",
		Detail:            fmt.Sprintf("%s spans %d lines and exposes %d top-level symbols, which makes cohesive refactoring harder.", relPath, lineCount, len(symbols)),
		SuggestedRefactor: "Split the file by responsibility boundary and move related symbols into smaller focused files or types.",
		File:              relPath,
		Language:          lang,
		Confidence:        "medium",
		Signals:           []string{"symbol_count", "file_lines"},
		Evidence: map[string]any{
			"file_lines":   lineCount,
			"symbol_count": len(symbols),
		},
	})
}

func applySignatureFindings(relPath, lang string, symbols []symindex.Symbol, state *scoutState) {
	for _, sym := range symbols {
		if strings.TrimSpace(sym.Signature) == "" {
			continue
		}
		metrics := parseSignatureMetrics(sym.Signature, lang)
		if metrics.ParamCount >= state.Thresholds.ParamCount {
			score := scoreLongParameterList(metrics.ParamCount, state.Thresholds)
			state.Findings = append(state.Findings, finding{
				RuleID:            "long_parameter_list",
				Category:          "signature",
				Severity:          severityFor(score),
				Score:             score,
				Title:             "Function signature carries too many parameters",
				Detail:            fmt.Sprintf("%s accepts %d parameters, which suggests the callsite is carrying too much setup state.", sym.Name, metrics.ParamCount),
				SuggestedRefactor: "Group related inputs into a config object or extract a narrower helper with a more focused contract.",
				File:              relPath,
				Line:              sym.StartLine,
				Symbol:            sym.Name,
				Language:          lang,
				Confidence:        "medium",
				Signals:           []string{"signature", "parameter_count"},
				Evidence: map[string]any{
					"signature":       skillout.TruncateSingleLine(sym.Signature, 160),
					"parameter_count": metrics.ParamCount,
				},
			})
		}
		if metrics.BoolParamCount > 0 {
			score := scoreBooleanParams(metrics.BoolParamCount)
			state.Findings = append(state.Findings, finding{
				RuleID:            "boolean_parameter",
				Category:          "signature",
				Severity:          severityFor(score),
				Score:             score,
				Title:             "Explicit boolean parameters suggest mode-switch behavior",
				Detail:            fmt.Sprintf("%s exposes %d explicitly typed boolean parameter(s), which often hides multiple execution modes behind one signature.", sym.Name, metrics.BoolParamCount),
				SuggestedRefactor: "Replace boolean mode switches with separate entrypoints, an enum-like option, or a small options type.",
				File:              relPath,
				Line:              sym.StartLine,
				Symbol:            sym.Name,
				Language:          lang,
				Confidence:        "medium",
				Signals:           []string{"signature", "typed_boolean_parameter"},
				Evidence: map[string]any{
					"signature":       skillout.TruncateSingleLine(sym.Signature, 160),
					"boolean_params":  metrics.BoolParamCount,
					"parameter_count": metrics.ParamCount,
				},
			})
		}
		if metrics.ReturnCount >= state.Thresholds.ReturnCount {
			score := scoreWideReturnTuple(metrics.ReturnCount, state.Thresholds)
			state.Findings = append(state.Findings, finding{
				RuleID:            "wide_return_tuple",
				Category:          "signature",
				Severity:          severityFor(score),
				Score:             score,
				Title:             "Function returns a wide tuple",
				Detail:            fmt.Sprintf("%s returns %d values, which increases caller-side unpacking and coordination.", sym.Name, metrics.ReturnCount),
				SuggestedRefactor: "Group related outputs into a result object or split value production from status reporting.",
				File:              relPath,
				Line:              sym.StartLine,
				Symbol:            sym.Name,
				Language:          lang,
				Confidence:        "medium",
				Signals:           []string{"signature", "return_count"},
				Evidence: map[string]any{
					"signature":    skillout.TruncateSingleLine(sym.Signature, 160),
					"return_count": metrics.ReturnCount,
				},
			})
		}
		symbolLines := spanLength(sym.StartLine, sym.EndLine)
		if symbolLines >= state.Thresholds.SymbolLines {
			score := scoreOversizedSymbol(symbolLines, state.Thresholds)
			state.Findings = append(state.Findings, finding{
				RuleID:            "oversized_symbol",
				Category:          "structure",
				Severity:          severityFor(score),
				Score:             score,
				Title:             "Symbol body is large enough to hide multiple concerns",
				Detail:            fmt.Sprintf("%s spans %d lines, which makes it a candidate for extraction even before adding call-graph signals.", sym.Name, symbolLines),
				SuggestedRefactor: "Split the symbol into smaller helpers or nested types around clear sub-responsibilities.",
				File:              relPath,
				Line:              sym.StartLine,
				Symbol:            sym.Name,
				Language:          lang,
				Confidence:        "medium",
				Signals:           []string{"symbol_span"},
				Evidence: map[string]any{
					"symbol_lines": symbolLines,
				},
			})
		}
	}
}

func analyzeGoFile(path, relPath string, content []byte, state *scoutState) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return skillerr.WrapParse("parse go file", err)
	}

	fileHighRiskFns := 0

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			findings := analyzeGoFuncDecl(d, fset, relPath, state)
			for _, item := range findings {
				if item.RuleID == "high_cyclomatic_complexity" || item.RuleID == "oversized_function" {
					fileHighRiskFns++
				}
			}
			state.Findings = append(state.Findings, findings...)
		case *ast.GenDecl:
			state.Findings = append(state.Findings, analyzeGoGenDecl(d, fset, relPath, state)...)
		}
	}

	if fileHighRiskFns >= state.Thresholds.MinFileComplexity {
		score := scoreComplexityCluster(fileHighRiskFns, state.Thresholds)
		state.Findings = append(state.Findings, finding{
			RuleID:            "complexity_cluster",
			Category:          "file",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "File clusters multiple hard-to-change functions",
			Detail:            fmt.Sprintf("%s contains %d high-risk Go functions, which usually means the file is mixing orchestration and low-level detail.", relPath, fileHighRiskFns),
			SuggestedRefactor: "Extract a smaller workflow layer and move lower-level branches into private helpers or separate files.",
			File:              relPath,
			Language:          "go",
			Confidence:        "high",
			Signals:           []string{"function_cluster"},
			Evidence: map[string]any{
				"high_risk_functions": fileHighRiskFns,
			},
		})
	}

	return nil
}

func analyzeGoFuncDecl(fn *ast.FuncDecl, fset *token.FileSet, relPath string, state *scoutState) []finding {
	if fn == nil || fn.Name == nil {
		return nil
	}

	name := fn.Name.Name
	receiver := receiverTypeName(fn)
	if receiver != "" {
		name = receiver + "." + name
		hotspot := state.ReceiverMethods[receiver]
		hotspot.Count++
		hotspot.File = relPath
		hotspot.Line = fset.Position(fn.Pos()).Line
		hotspot.Language = "go"
		state.ReceiverMethods[receiver] = hotspot
	}

	line := fset.Position(fn.Pos()).Line
	length := fset.Position(fn.End()).Line - line + 1
	paramCount, boolParamCount := goParamMetrics(fn)
	returnCount := goReturnCount(fn)
	cyclomatic := calculateGoCyclomaticComplexity(fn)
	nesting := calculateGoNestingDepth(fn)
	signature := goFuncSignature(fn)
	orchestration := extractGoOrchestrationMetrics(fn)

	kind := symindex.KindFunction
	if receiver != "" {
		kind = symindex.KindMethod
	}
	obs := ensureSymbolObservation(state, relPath, name, "go", kind, line)
	obs.Symbol = chooseDisplaySymbol(obs.Symbol, name)
	obs.Signature = signature
	obs.ParamCount = paramCount
	obs.BoolParamCount = boolParamCount
	obs.ReturnCount = returnCount
	obs.SymbolLines = length
	obs.FunctionLines = length
	obs.Cyclomatic = cyclomatic
	obs.Nesting = nesting
	obs.BranchCount = orchestration.BranchCount
	obs.CallSiteCount = orchestration.CallSiteCount
	obs.OrchestrationFingerprint = orchestration.Fingerprint
	obs.OrchestrationTokens = orchestration.Tokens

	findings := make([]finding, 0, 5)

	if paramCount >= state.Thresholds.ParamCount {
		score := scoreLongParameterList(paramCount, state.Thresholds)
		findings = append(findings, finding{
			RuleID:            "long_parameter_list",
			Category:          "signature",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function signature carries too many parameters",
			Detail:            fmt.Sprintf("%s accepts %d parameters, which usually indicates hidden coupling between callsite setup and business logic.", name, paramCount),
			SuggestedRefactor: "Group related inputs into a dedicated struct or extract a narrower helper that owns part of the setup.",
			File:              relPath,
			Line:              line,
			Symbol:            name,
			Language:          "go",
			Confidence:        "high",
			Signals:           []string{"go_ast", "parameter_count"},
			Evidence: map[string]any{
				"signature":       signature,
				"parameter_count": paramCount,
			},
		})
	}

	if boolParamCount > 0 {
		score := scoreBooleanParams(boolParamCount)
		findings = append(findings, finding{
			RuleID:            "boolean_parameter",
			Category:          "signature",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Boolean parameters hide multiple code paths",
			Detail:            fmt.Sprintf("%s takes %d boolean parameter(s), which often means the function is selecting between modes instead of exposing clear operations.", name, boolParamCount),
			SuggestedRefactor: "Split mode-specific behavior into separate functions or replace booleans with a typed option object.",
			File:              relPath,
			Line:              line,
			Symbol:            name,
			Language:          "go",
			Confidence:        "high",
			Signals:           []string{"go_ast", "typed_boolean_parameter"},
			Evidence: map[string]any{
				"signature":      signature,
				"boolean_params": boolParamCount,
			},
		})
	}

	if returnCount >= state.Thresholds.ReturnCount {
		score := scoreWideReturnTuple(returnCount, state.Thresholds)
		findings = append(findings, finding{
			RuleID:            "wide_return_tuple",
			Category:          "signature",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function returns a wide tuple",
			Detail:            fmt.Sprintf("%s returns %d values, which makes the caller coordinate too many parallel outputs.", name, returnCount),
			SuggestedRefactor: "Return a small result struct when outputs belong together, or split status reporting from value production.",
			File:              relPath,
			Line:              line,
			Symbol:            name,
			Language:          "go",
			Confidence:        "high",
			Signals:           []string{"go_ast", "return_count"},
			Evidence: map[string]any{
				"signature":    signature,
				"return_count": returnCount,
			},
		})
	}

	if length >= state.Thresholds.FunctionLines {
		score := scoreOversizedFunction(length, state.Thresholds)
		findings = append(findings, finding{
			RuleID:            "oversized_function",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function body is long enough to obscure responsibility boundaries",
			Detail:            fmt.Sprintf("%s is %d lines long, which raises the odds that orchestration and detail logic are mixed together.", name, length),
			SuggestedRefactor: "Extract named helpers around decision points, side effects, or repeated setup and keep the top-level function as orchestration only.",
			File:              relPath,
			Line:              line,
			Symbol:            name,
			Language:          "go",
			Confidence:        "high",
			Signals:           []string{"go_ast", "function_lines"},
			Evidence: map[string]any{
				"function_lines": length,
			},
		})
	}

	if cyclomatic >= state.Thresholds.Cyclomatic {
		score := scoreCyclomaticComplexity(cyclomatic, state.Thresholds)
		findings = append(findings, finding{
			RuleID:            "high_cyclomatic_complexity",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Cyclomatic complexity is high enough to merit decomposition",
			Detail:            fmt.Sprintf("%s has cyclomatic complexity %d, which suggests too many decision paths for a single unit.", name, cyclomatic),
			SuggestedRefactor: "Flatten conditionals with early returns and move independent decision branches behind dedicated helpers or strategy objects.",
			File:              relPath,
			Line:              line,
			Symbol:            name,
			Language:          "go",
			Confidence:        "high",
			Signals:           []string{"go_ast", "cyclomatic_complexity"},
			Evidence: map[string]any{
				"cyclomatic_complexity": cyclomatic,
			},
		})
	}

	if nesting >= state.Thresholds.Nesting {
		score := scoreDeepNesting(nesting, state.Thresholds)
		findings = append(findings, finding{
			RuleID:            "deep_nesting",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Nested control flow is making the function harder to reshape",
			Detail:            fmt.Sprintf("%s reaches nesting depth %d, which makes extraction and local reasoning more expensive.", name, nesting),
			SuggestedRefactor: "Invert guards, return early on edge cases, and isolate nested branches into helpers that express intent directly.",
			File:              relPath,
			Line:              line,
			Symbol:            name,
			Language:          "go",
			Confidence:        "high",
			Signals:           []string{"go_ast", "nesting_depth"},
			Evidence: map[string]any{
				"nesting_depth": nesting,
			},
		})
	}

	findings = append(findings, analyzeGoSemanticSimplification(fn, fset, relPath, name)...)
	findings = append(findings, analyzeDuplicateRecoveryBlocks(fn, fset, relPath, name)...)

	return findings
}

func analyzeGoGenDecl(decl *ast.GenDecl, fset *token.FileSet, relPath string, state *scoutState) []finding {
	if decl == nil {
		return nil
	}

	findings := make([]finding, 0, 2)
	for _, spec := range decl.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		if iface, ok := typeSpec.Type.(*ast.InterfaceType); ok {
			methodCount := 0
			if iface.Methods != nil {
				for _, method := range iface.Methods.List {
					if len(method.Names) > 0 {
						methodCount += len(method.Names)
						continue
					}
					methodCount++
				}
			}
			if methodCount >= state.Thresholds.InterfaceMethods {
				score := scoreWideInterface(methodCount, state.Thresholds)
				findings = append(findings, finding{
					RuleID:            "wide_interface",
					Category:          "type",
					Severity:          severityFor(score),
					Score:             score,
					Title:             "Interface surface is wide enough to suggest multiple roles",
					Detail:            fmt.Sprintf("Interface %s exposes %d methods, which is often a signal that callers depend on more than one responsibility.", typeSpec.Name.Name, methodCount),
					SuggestedRefactor: "Split the interface by usage role and let consumers depend on the smallest capability slice they need.",
					File:              relPath,
					Line:              fset.Position(typeSpec.Pos()).Line,
					Symbol:            typeSpec.Name.Name,
					Language:          "go",
					Confidence:        "high",
					Signals:           []string{"go_ast", "interface_method_count"},
					Evidence: map[string]any{
						"method_count": methodCount,
					},
				})
			}
		}
	}
	return findings
}

func finalizeReceiverHotspots(state *scoutState) {
	for receiver, hotspot := range state.ReceiverMethods {
		if hotspot.Count < state.Thresholds.ReceiverMethods {
			continue
		}
		score := scoreReceiverHotspot(hotspot.Count, state.Thresholds)
		state.Findings = append(state.Findings, finding{
			RuleID:            "receiver_hotspot",
			Category:          "type",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Receiver owns a large method surface",
			Detail:            fmt.Sprintf("Receiver %s owns %d methods in the scanned scope, which often means it is doing coordination plus domain work plus persistence or transport concerns.", receiver, hotspot.Count),
			SuggestedRefactor: "Split the receiver into narrower collaborators or extract role-specific helpers from the heaviest methods first.",
			File:              hotspot.File,
			Line:              hotspot.Line,
			Symbol:            receiver,
			Language:          hotspot.Language,
			Confidence:        "medium",
			Signals:           []string{"go_ast", "receiver_method_count"},
			Evidence: map[string]any{
				"method_count": hotspot.Count,
			},
		})
	}
}

func finalizeObservationFindings(state *scoutState) {
	if state == nil || len(state.Symbols) == 0 {
		return
	}
	finalizeFanOutFindings(state)
	peerMap := similarOrchestrationPeers(state)
	finalizeStructuralSimilarityClusterFindings(state, peerMap)
	finalizeDuplicateOrchestrationFindings(state, peerMap)
	callFamilyPeerMap := similarCallFamilyPeers(state)
	finalizeCallFamilyClusterFindings(state, callFamilyPeerMap)
	suppressRedundantClusterFindings(state)
	finalizeSameFileExtractionFindings(state)
}

func finalizeFanOutFindings(state *scoutState) {
	for _, obs := range state.Symbols {
		if obs == nil || obs.FanOut < state.Thresholds.FanOutCalls {
			continue
		}
		score := scoreFanOutDependencySpread(obs.FanOut, state.Thresholds)
		confidence := "medium"
		if obs.Language == "go" {
			confidence = "high"
		}
		state.Findings = append(state.Findings, finding{
			RuleID:            "fan_out_dependency_spread",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function touches too many collaborators",
			Detail:            fmt.Sprintf("%s reaches %d distinct downstream call target(s), which is a strong sign that orchestration and detail logic are coupled together.", obs.Symbol, obs.FanOut),
			SuggestedRefactor: "Move step-specific branches behind focused helpers or collaborators so the entrypoint coordinates fewer direct dependencies.",
			File:              obs.File,
			Line:              obs.Line,
			Symbol:            obs.Symbol,
			Language:          obs.Language,
			Confidence:        confidence,
			Signals:           []string{"call_extraction", "fan_out"},
			Evidence: map[string]any{
				"fan_out":      obs.FanOut,
				"call_targets": sampleStrings(obs.Calls, 8),
			},
		})
	}
}

type similarObservation struct {
	Observation *symbolObservation
	Similarity  int
	Details     orchestrationSimilarityDetails
}

type orchestrationSimilarityDetails struct {
	Score             int
	SequenceScore     int
	OverlapScore      int
	CallSiteScore     int
	BranchScore       int
	SharedSubsequence []string
	SharedTokens      []string
	WhySimilar        string
}

type structuralSimilarityCluster struct {
	ScopePath              string
	File                   string
	MemberKeys             []string
	Members                []*symbolObservation
	MemberFiles            []string
	UniqueFileCount        int
	EntryLine              int
	EdgeCount              int
	AverageSimilarity      int
	MaxSimilarity          int
	AverageBranches        int
	AverageCallSites       int
	AverageFanOut          int
	AverageSymbolLines     int
	BranchRange            int
	CallSiteRange          int
	FanOutRange            int
	SymbolLineRange        int
	ParamRange             int
	ReturnRange            int
	DominantContainer      string
	DominantContainerRatio int
	AdapterSurfaceScore    int
	StrongestPair          [2]string
	StrongestDetail        orchestrationSimilarityDetails
}

type structuralSeamProfile struct {
	Kind              string
	Title             string
	DetailTemplate    string
	SuggestedRefactor string
}

type callFamilySimilarityDetails struct {
	Score                int
	CallOverlapScore     int
	DistinctivenessScore int
	NamespaceScore       int
	SignatureShapeScore  int
	FanOutScore          int
	SpanScore            int
	SharedCalls          []string
	WhySimilar           string
}

type callFamilyPeer struct {
	Observation *symbolObservation
	Similarity  int
	Details     callFamilySimilarityDetails
}

type callFamilyCluster struct {
	ScopePath              string
	File                   string
	Members                []*symbolObservation
	MemberFiles            []string
	UniqueFileCount        int
	EntryLine              int
	EdgeCount              int
	AverageSimilarity      int
	MaxSimilarity          int
	AverageFanOut          int
	AverageSymbolLines     int
	AverageParamCount      int
	AverageReturnCount     int
	DominantContainer      string
	DominantContainerRatio int
	AdapterSurfaceScore    int
	StrongestPair          [2]string
	StrongestDetail        callFamilySimilarityDetails
}

func similarOrchestrationPeers(state *scoutState) map[string][]similarObservation {
	candidates := make([]*symbolObservation, 0, len(state.Symbols))
	for _, obs := range state.Symbols {
		if !qualifiesDuplicateOrchestrationCandidate(obs, state.Thresholds) {
			continue
		}
		candidates = append(candidates, obs)
	}
	peers := make(map[string][]similarObservation)
	threshold := orchestrationSimilarityThreshold(state.Thresholds)
	for i := 0; i < len(candidates); i++ {
		left := candidates[i]
		for j := i + 1; j < len(candidates); j++ {
			right := candidates[j]
			if !comparableOrchestrationPair(left, right) {
				continue
			}
			details := orchestrationSimilarityDetailsFor(left, right)
			if details.Score < threshold {
				continue
			}
			leftKey := observationKey(left.File, left.Symbol)
			rightKey := observationKey(right.File, right.Symbol)
			peers[leftKey] = append(peers[leftKey], similarObservation{Observation: right, Similarity: details.Score, Details: details})
			peers[rightKey] = append(peers[rightKey], similarObservation{Observation: left, Similarity: details.Score, Details: details})
		}
	}
	return peers
}

func similarCallFamilyPeers(state *scoutState) map[string][]callFamilyPeer {
	candidates := make([]*symbolObservation, 0, len(state.Symbols))
	for _, obs := range state.Symbols {
		if !qualifiesCallFamilyCandidate(obs) {
			continue
		}
		candidates = append(candidates, obs)
	}
	peers := make(map[string][]callFamilyPeer)
	threshold := callFamilySimilarityThreshold(state.Thresholds)
	for i := 0; i < len(candidates); i++ {
		left := candidates[i]
		for j := i + 1; j < len(candidates); j++ {
			right := candidates[j]
			if !comparableCallFamilyPair(left, right) {
				continue
			}
			details := callFamilySimilarityDetailsFor(state, left, right)
			if details.Score < threshold {
				continue
			}
			leftKey := observationKey(left.File, left.Symbol)
			rightKey := observationKey(right.File, right.Symbol)
			peers[leftKey] = append(peers[leftKey], callFamilyPeer{Observation: right, Similarity: details.Score, Details: details})
			peers[rightKey] = append(peers[rightKey], callFamilyPeer{Observation: left, Similarity: details.Score, Details: details})
		}
	}
	return peers
}

func qualifiesCallFamilyCandidate(obs *symbolObservation) bool {
	if obs == nil || obs.Language == "go" {
		return false
	}
	if obs.FanOut < 2 {
		return false
	}
	if obs.SymbolLines < 8 {
		return false
	}
	return true
}

func comparableCallFamilyPair(left, right *symbolObservation) bool {
	if left == nil || right == nil || left.Language != right.Language {
		return false
	}
	if absInt(left.FanOut-right.FanOut) > maxInt(3, maxInt(left.FanOut, right.FanOut)/2) {
		return false
	}
	if absInt(left.ParamCount-right.ParamCount) > 3 {
		return false
	}
	if absInt(left.ReturnCount-right.ReturnCount) > 2 {
		return false
	}
	return true
}

func callFamilySimilarityThreshold(th thresholds) int {
	switch {
	case th.FunctionLines <= 60:
		return 64
	case th.FunctionLines >= 100:
		return 72
	default:
		return 68
	}
}

func callFamilySimilarityDetailsFor(state *scoutState, left, right *symbolObservation) callFamilySimilarityDetails {
	if state == nil || left == nil || right == nil {
		return callFamilySimilarityDetails{}
	}
	sharedCalls := intersectStrings(left.Calls, right.Calls)
	if len(sharedCalls) == 0 {
		return callFamilySimilarityDetails{}
	}
	callOverlap := 0
	if maxCalls := maxInt(len(left.Calls), len(right.Calls)); maxCalls > 0 {
		callOverlap = (len(sharedCalls) * 100) / maxCalls
	}
	signatureShape := 100
	signatureShape -= minInt(30, absInt(left.ParamCount-right.ParamCount)*12)
	signatureShape -= minInt(20, absInt(left.ReturnCount-right.ReturnCount)*10)
	signatureShape -= minInt(10, absInt(left.BoolParamCount-right.BoolParamCount)*10)
	if signatureShape < 0 {
		signatureShape = 0
	}
	fanOutScore := boundedCountSimilarity(left.FanOut, right.FanOut)
	spanScore := boundedCountSimilarity(left.SymbolLines, right.SymbolLines)
	distinctiveness := sharedCallDistinctivenessScore(state, left.Language, sharedCalls)
	namespaceScore := 0
	if left.Language == "elixir" {
		namespaceScore = elixirNamespaceSimilarityScore(left.File, right.File)
	}
	score := clampScore((callOverlap*35 + signatureShape*20 + fanOutScore*15 + spanScore*10 + distinctiveness*20) / 100)
	if left.Language == "elixir" {
		score = clampScore((callOverlap*15 + signatureShape*10 + fanOutScore*10 + spanScore*10 + distinctiveness*25 + namespaceScore*30) / 100)
	}
	return callFamilySimilarityDetails{
		Score:                score,
		CallOverlapScore:     callOverlap,
		DistinctivenessScore: distinctiveness,
		NamespaceScore:       namespaceScore,
		SignatureShapeScore:  signatureShape,
		FanOutScore:          fanOutScore,
		SpanScore:            spanScore,
		SharedCalls:          sampleStrings(sharedCalls, 8),
		WhySimilar: fmt.Sprintf(
			"shared calls %s; call_overlap=%d distinctiveness=%d namespace=%d signature_shape=%d fan_out=%d span=%d",
			strings.Join(sampleStrings(sharedCalls, 6), ", "),
			callOverlap,
			distinctiveness,
			namespaceScore,
			signatureShape,
			fanOutScore,
			spanScore,
		),
	}
}

func sharedCallDistinctivenessScore(state *scoutState, lang string, sharedCalls []string) int {
	if state == nil || len(sharedCalls) == 0 {
		return 0
	}
	freq := state.CallFrequency[lang]
	if len(freq) == 0 {
		return 0
	}
	total := 0
	count := 0
	for _, call := range sharedCalls {
		call = strings.TrimSpace(call)
		if call == "" {
			continue
		}
		f := freq[call]
		if f <= 0 {
			f = 1
		}
		score := 100 - minInt(85, maxInt(0, f-1)*10)
		total += score
		count++
	}
	if count == 0 {
		return 0
	}
	return total / count
}

func elixirNamespaceSimilarityScore(leftFile, rightFile string) int {
	left := elixirNamespaceSegments(leftFile)
	right := elixirNamespaceSegments(rightFile)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	if len(left) > 0 {
		left = left[:len(left)-1]
	}
	if len(right) > 0 {
		right = right[:len(right)-1]
	}
	if len(left) > 0 {
		left = left[1:]
	}
	if len(right) > 0 {
		right = right[1:]
	}
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	common := 0
	for common < len(left) && common < len(right) && left[common] == right[common] {
		common++
	}
	if common == 0 {
		return 0
	}
	return (common * 100) / maxInt(len(left), len(right))
}

func elixirNamespaceSegments(file string) []string {
	parts := strings.Split(filepath.ToSlash(file), "/")
	libIdx := -1
	for i := 0; i < len(parts); i++ {
		if parts[i] == "lib" {
			libIdx = i
			break
		}
	}
	if libIdx < 0 || libIdx+1 >= len(parts) {
		return nil
	}
	ns := append([]string(nil), parts[libIdx+1:]...)
	if len(ns) == 0 {
		return nil
	}
	last := ns[len(ns)-1]
	ext := filepath.Ext(last)
	last = strings.TrimSuffix(last, ext)
	ns[len(ns)-1] = last
	return ns
}

func qualifiesDuplicateOrchestrationCandidate(obs *symbolObservation, th thresholds) bool {
	if obs == nil || obs.Language != "go" {
		return false
	}
	if strings.TrimSpace(obs.OrchestrationFingerprint) == "" || len(obs.OrchestrationTokens) == 0 {
		return false
	}
	if obs.CallSiteCount < th.DuplicateCallSites || obs.BranchCount < th.DuplicateBranches {
		return false
	}
	if obs.FunctionLines < maxInt(20, th.FunctionLines/2) {
		return false
	}
	return true
}

func comparableOrchestrationPair(left, right *symbolObservation) bool {
	if left == nil || right == nil {
		return false
	}
	leftLen := len(left.OrchestrationTokens)
	rightLen := len(right.OrchestrationTokens)
	if leftLen == 0 || rightLen == 0 {
		return false
	}
	maxLen := maxInt(leftLen, rightLen)
	if absInt(leftLen-rightLen) > maxInt(6, maxLen/2) {
		return false
	}
	maxCalls := maxInt(left.CallSiteCount, right.CallSiteCount)
	if absInt(left.CallSiteCount-right.CallSiteCount) > maxInt(2, maxCalls/2) {
		return false
	}
	maxBranches := maxInt(left.BranchCount, right.BranchCount)
	return absInt(left.BranchCount-right.BranchCount) <= maxInt(1, maxBranches/2)
}

func orchestrationSimilarityThreshold(th thresholds) int {
	switch {
	case th.FunctionLines <= 60:
		return 68
	case th.FunctionLines >= 100:
		return 78
	default:
		return 72
	}
}

func orchestrationSimilarityScore(left, right *symbolObservation) int {
	return orchestrationSimilarityDetailsFor(left, right).Score
}

func orchestrationSimilarityDetailsFor(left, right *symbolObservation) orchestrationSimilarityDetails {
	if left == nil || right == nil {
		return orchestrationSimilarityDetails{}
	}
	if len(left.OrchestrationTokens) == 0 || len(right.OrchestrationTokens) == 0 {
		return orchestrationSimilarityDetails{}
	}
	sharedSubsequence := longestCommonSubsequence(left.OrchestrationTokens, right.OrchestrationTokens)
	sharedTokens := sharedStructuralTokens(left.OrchestrationTokens, right.OrchestrationTokens, 8)
	seq := 0
	if maxLen := maxInt(len(left.OrchestrationTokens), len(right.OrchestrationTokens)); maxLen > 0 {
		seq = (len(sharedSubsequence) * 100) / maxLen
	}
	overlap := tokenOverlapSimilarity(left.OrchestrationTokens, right.OrchestrationTokens)
	callSite := boundedCountSimilarity(left.CallSiteCount, right.CallSiteCount)
	branch := boundedCountSimilarity(left.BranchCount, right.BranchCount)
	score := clampScore((seq*45 + overlap*25 + callSite*20 + branch*10) / 100)
	return orchestrationSimilarityDetails{
		Score:             score,
		SequenceScore:     seq,
		OverlapScore:      overlap,
		CallSiteScore:     callSite,
		BranchScore:       branch,
		SharedSubsequence: sharedSubsequence,
		SharedTokens:      sharedTokens,
		WhySimilar:        similaritySummary(sharedSubsequence, seq, overlap, callSite, branch),
	}
}

func tokenOverlapSimilarity(left, right []string) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	leftCounts := make(map[string]int, len(left))
	rightCounts := make(map[string]int, len(right))
	for _, item := range left {
		leftCounts[item]++
	}
	for _, item := range right {
		rightCounts[item]++
	}
	intersection := 0
	union := 0
	seen := make(map[string]struct{}, len(leftCounts)+len(rightCounts))
	for key, count := range leftCounts {
		seen[key] = struct{}{}
		other := rightCounts[key]
		intersection += minInt(count, other)
		union += maxInt(count, other)
	}
	for key, count := range rightCounts {
		if _, ok := seen[key]; ok {
			continue
		}
		union += count
	}
	if union == 0 {
		return 0
	}
	return (intersection * 100) / union
}

func longestCommonSubsequence(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	dp := make([][]int, len(left)+1)
	for i := range dp {
		dp[i] = make([]int, len(right)+1)
	}
	for i := 1; i <= len(left); i++ {
		for j := 1; j <= len(right); j++ {
			if left[i-1] == right[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
				continue
			}
			dp[i][j] = maxInt(dp[i-1][j], dp[i][j-1])
		}
	}
	out := make([]string, 0, dp[len(left)][len(right)])
	i := len(left)
	j := len(right)
	for i > 0 && j > 0 {
		if left[i-1] == right[j-1] {
			out = append(out, left[i-1])
			i--
			j--
			continue
		}
		if dp[i-1][j] >= dp[i][j-1] {
			i--
			continue
		}
		j--
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

func sharedStructuralTokens(left, right []string, limit int) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	rightSet := make(map[string]struct{}, len(right))
	for _, token := range right {
		rightSet[token] = struct{}{}
	}
	out := make([]string, 0, minInt(len(left), limit))
	seen := make(map[string]struct{}, len(left))
	for _, token := range left {
		if _, ok := rightSet[token]; !ok {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func similaritySummary(sharedSubsequence []string, seq, overlap, callSite, branch int) string {
	if len(sharedSubsequence) == 0 {
		return fmt.Sprintf("sequence=%d overlap=%d call_sites=%d branches=%d", seq, overlap, callSite, branch)
	}
	preview := sharedSubsequence
	if len(preview) > 6 {
		preview = preview[:6]
	}
	return fmt.Sprintf("shared sequence %s; sequence=%d overlap=%d call_sites=%d branches=%d",
		strings.Join(preview, " -> "), seq, overlap, callSite, branch)
}

func boundedCountSimilarity(left, right int) int {
	if left == 0 && right == 0 {
		return 100
	}
	maxCount := maxInt(left, right)
	if maxCount == 0 {
		return 0
	}
	return 100 - ((absInt(left-right) * 100) / maxCount)
}

func finalizeStructuralSimilarityClusterFindings(state *scoutState, peerMap map[string][]similarObservation) {
	if state == nil || len(peerMap) == 0 {
		return
	}
	for _, cluster := range topStructuralSimilarityClustersByFile(state, peerMap) {
		if len(cluster.Members) < 2 {
			continue
		}
		memberNames := make([]string, 0, len(cluster.Members))
		for _, member := range cluster.Members {
			memberNames = append(memberNames, member.Symbol)
		}
		profile := classifyStructuralCluster(cluster)
		score := scoreStructuralSimilarityCluster(len(cluster.Members), cluster.EdgeCount, cluster.MaxSimilarity, cluster.AverageSimilarity, cluster.UniqueFileCount, cluster.AverageBranches, cluster.AverageCallSites, cluster.AverageFanOut)
		state.Findings = append(state.Findings, finding{
			RuleID:            "structural_similarity_cluster",
			Category:          "cluster",
			Severity:          severityFor(score),
			Score:             score,
			Title:             profile.Title,
			Detail:            fmt.Sprintf(profile.DetailTemplate, cluster.File, len(cluster.Members)),
			SuggestedRefactor: profile.SuggestedRefactor,
			File:              cluster.File,
			Line:              cluster.EntryLine,
			Language:          "go",
			Confidence:        "high",
			Signals:           []string{"go_ast", "orchestration_similarity", "cluster"},
			Evidence: map[string]any{
				"seam_kind":                profile.Kind,
				"adapter_surface_score":    cluster.AdapterSurfaceScore,
				"scope_path":               cluster.ScopePath,
				"cluster_size":             len(cluster.Members),
				"edge_count":               cluster.EdgeCount,
				"average_similarity":       cluster.AverageSimilarity,
				"max_similarity":           cluster.MaxSimilarity,
				"average_branches":         cluster.AverageBranches,
				"average_call_sites":       cluster.AverageCallSites,
				"average_fan_out":          cluster.AverageFanOut,
				"average_symbol_lines":     cluster.AverageSymbolLines,
				"branch_range":             cluster.BranchRange,
				"call_site_range":          cluster.CallSiteRange,
				"fan_out_range":            cluster.FanOutRange,
				"symbol_line_range":        cluster.SymbolLineRange,
				"param_range":              cluster.ParamRange,
				"return_range":             cluster.ReturnRange,
				"dominant_container":       cluster.DominantContainer,
				"dominant_container_ratio": cluster.DominantContainerRatio,
				"member_symbols":           memberNames,
				"member_files":             cluster.MemberFiles,
				"strongest_pair":           []string{cluster.StrongestPair[0], cluster.StrongestPair[1]},
				"why_similar":              cluster.StrongestDetail.WhySimilar,
				"shared_subsequence":       sampleStrings(cluster.StrongestDetail.SharedSubsequence, 8),
				"shared_tokens":            cluster.StrongestDetail.SharedTokens,
				"similarity_breakdown": map[string]int{
					"sequence":   cluster.StrongestDetail.SequenceScore,
					"overlap":    cluster.StrongestDetail.OverlapScore,
					"call_sites": cluster.StrongestDetail.CallSiteScore,
					"branches":   cluster.StrongestDetail.BranchScore,
				},
			},
		})
	}
	for _, cluster := range topStructuralSimilarityClustersByDirectory(state, peerMap) {
		if len(cluster.Members) < 2 || cluster.UniqueFileCount < 2 {
			continue
		}
		memberNames := make([]string, 0, len(cluster.Members))
		for _, member := range cluster.Members {
			memberNames = append(memberNames, member.Symbol)
		}
		profile := classifyStructuralCluster(cluster)
		score := scoreStructuralSimilarityCluster(len(cluster.Members), cluster.EdgeCount, cluster.MaxSimilarity, cluster.AverageSimilarity, cluster.UniqueFileCount, cluster.AverageBranches, cluster.AverageCallSites, cluster.AverageFanOut)
		state.Findings = append(state.Findings, finding{
			RuleID:            "structural_similarity_module_cluster",
			Category:          "cluster",
			Severity:          severityFor(score),
			Score:             score,
			Title:             moduleClusterTitle(profile),
			Detail:            moduleClusterDetail(cluster, profile),
			SuggestedRefactor: profile.SuggestedRefactor,
			File:              cluster.File,
			Line:              cluster.EntryLine,
			Language:          "go",
			Confidence:        "high",
			Signals:           []string{"go_ast", "orchestration_similarity", "cluster", "module_scope"},
			Evidence: map[string]any{
				"seam_kind":                profile.Kind,
				"adapter_surface_score":    cluster.AdapterSurfaceScore,
				"scope_path":               cluster.ScopePath,
				"cluster_size":             len(cluster.Members),
				"unique_file_count":        cluster.UniqueFileCount,
				"edge_count":               cluster.EdgeCount,
				"average_similarity":       cluster.AverageSimilarity,
				"max_similarity":           cluster.MaxSimilarity,
				"average_branches":         cluster.AverageBranches,
				"average_call_sites":       cluster.AverageCallSites,
				"average_fan_out":          cluster.AverageFanOut,
				"average_symbol_lines":     cluster.AverageSymbolLines,
				"branch_range":             cluster.BranchRange,
				"call_site_range":          cluster.CallSiteRange,
				"fan_out_range":            cluster.FanOutRange,
				"symbol_line_range":        cluster.SymbolLineRange,
				"param_range":              cluster.ParamRange,
				"return_range":             cluster.ReturnRange,
				"dominant_container":       cluster.DominantContainer,
				"dominant_container_ratio": cluster.DominantContainerRatio,
				"member_symbols":           memberNames,
				"member_files":             cluster.MemberFiles,
				"strongest_pair":           []string{cluster.StrongestPair[0], cluster.StrongestPair[1]},
				"why_similar":              cluster.StrongestDetail.WhySimilar,
				"shared_subsequence":       sampleStrings(cluster.StrongestDetail.SharedSubsequence, 8),
				"shared_tokens":            cluster.StrongestDetail.SharedTokens,
				"similarity_breakdown": map[string]int{
					"sequence":   cluster.StrongestDetail.SequenceScore,
					"overlap":    cluster.StrongestDetail.OverlapScore,
					"call_sites": cluster.StrongestDetail.CallSiteScore,
					"branches":   cluster.StrongestDetail.BranchScore,
				},
			},
		})
	}
}

func finalizeCallFamilyClusterFindings(state *scoutState, peerMap map[string][]callFamilyPeer) {
	if state == nil || len(peerMap) == 0 {
		return
	}
	for _, cluster := range topCallFamilyClustersByFile(state, peerMap) {
		if len(cluster.Members) < 2 {
			continue
		}
		memberNames := make([]string, 0, len(cluster.Members))
		for _, member := range cluster.Members {
			memberNames = append(memberNames, member.Symbol)
		}
		profile := classifyCallFamilyCluster(cluster)
		score := scoreCallFamilyCluster(len(cluster.Members), cluster.EdgeCount, cluster.MaxSimilarity, cluster.AverageSimilarity, cluster.UniqueFileCount, cluster.AverageFanOut, cluster.AverageSymbolLines, cluster.AdapterSurfaceScore)
		state.Findings = append(state.Findings, finding{
			RuleID:            "call_family_cluster",
			Category:          "cluster",
			Severity:          severityFor(score),
			Score:             score,
			Title:             profile.Title,
			Detail:            fmt.Sprintf(profile.DetailTemplate, cluster.File, len(cluster.Members)),
			SuggestedRefactor: profile.SuggestedRefactor,
			File:              cluster.File,
			Line:              cluster.EntryLine,
			Language:          cluster.Members[0].Language,
			Confidence:        "medium",
			Signals:           []string{"call_extraction", "call_family_similarity", "cluster"},
			Evidence: map[string]any{
				"seam_kind":                profile.Kind,
				"similarity_mode":          "call_family",
				"adapter_surface_score":    cluster.AdapterSurfaceScore,
				"scope_path":               cluster.ScopePath,
				"cluster_size":             len(cluster.Members),
				"edge_count":               cluster.EdgeCount,
				"average_similarity":       cluster.AverageSimilarity,
				"max_similarity":           cluster.MaxSimilarity,
				"average_fan_out":          cluster.AverageFanOut,
				"average_symbol_lines":     cluster.AverageSymbolLines,
				"average_param_count":      cluster.AverageParamCount,
				"average_return_count":     cluster.AverageReturnCount,
				"dominant_container":       cluster.DominantContainer,
				"dominant_container_ratio": cluster.DominantContainerRatio,
				"member_symbols":           memberNames,
				"member_files":             cluster.MemberFiles,
				"strongest_pair":           []string{cluster.StrongestPair[0], cluster.StrongestPair[1]},
				"why_similar":              cluster.StrongestDetail.WhySimilar,
				"shared_calls":             cluster.StrongestDetail.SharedCalls,
				"similarity_breakdown": map[string]int{
					"call_overlap":    cluster.StrongestDetail.CallOverlapScore,
					"distinctiveness": cluster.StrongestDetail.DistinctivenessScore,
					"namespace":       cluster.StrongestDetail.NamespaceScore,
					"signature_shape": cluster.StrongestDetail.SignatureShapeScore,
					"fan_out":         cluster.StrongestDetail.FanOutScore,
					"span":            cluster.StrongestDetail.SpanScore,
				},
			},
		})
	}
	for _, cluster := range topCallFamilyClustersByDirectory(state, peerMap) {
		if len(cluster.Members) < 2 || cluster.UniqueFileCount < 2 {
			continue
		}
		if !allowCallFamilyModuleCluster(cluster) {
			continue
		}
		memberNames := make([]string, 0, len(cluster.Members))
		for _, member := range cluster.Members {
			memberNames = append(memberNames, member.Symbol)
		}
		profile := classifyCallFamilyCluster(cluster)
		score := scoreCallFamilyCluster(len(cluster.Members), cluster.EdgeCount, cluster.MaxSimilarity, cluster.AverageSimilarity, cluster.UniqueFileCount, cluster.AverageFanOut, cluster.AverageSymbolLines, cluster.AdapterSurfaceScore)
		state.Findings = append(state.Findings, finding{
			RuleID:            "call_family_module_cluster",
			Category:          "cluster",
			Severity:          severityFor(score),
			Score:             score,
			Title:             moduleClusterTitle(profile),
			Detail:            moduleCallFamilyClusterDetail(cluster, profile),
			SuggestedRefactor: profile.SuggestedRefactor,
			File:              cluster.File,
			Line:              cluster.EntryLine,
			Language:          cluster.Members[0].Language,
			Confidence:        "medium",
			Signals:           []string{"call_extraction", "call_family_similarity", "cluster", "module_scope"},
			Evidence: map[string]any{
				"seam_kind":                profile.Kind,
				"similarity_mode":          "call_family",
				"adapter_surface_score":    cluster.AdapterSurfaceScore,
				"scope_path":               cluster.ScopePath,
				"cluster_size":             len(cluster.Members),
				"unique_file_count":        cluster.UniqueFileCount,
				"edge_count":               cluster.EdgeCount,
				"average_similarity":       cluster.AverageSimilarity,
				"max_similarity":           cluster.MaxSimilarity,
				"average_fan_out":          cluster.AverageFanOut,
				"average_symbol_lines":     cluster.AverageSymbolLines,
				"average_param_count":      cluster.AverageParamCount,
				"average_return_count":     cluster.AverageReturnCount,
				"dominant_container":       cluster.DominantContainer,
				"dominant_container_ratio": cluster.DominantContainerRatio,
				"member_symbols":           memberNames,
				"member_files":             cluster.MemberFiles,
				"strongest_pair":           []string{cluster.StrongestPair[0], cluster.StrongestPair[1]},
				"why_similar":              cluster.StrongestDetail.WhySimilar,
				"shared_calls":             cluster.StrongestDetail.SharedCalls,
				"similarity_breakdown": map[string]int{
					"call_overlap":    cluster.StrongestDetail.CallOverlapScore,
					"distinctiveness": cluster.StrongestDetail.DistinctivenessScore,
					"namespace":       cluster.StrongestDetail.NamespaceScore,
					"signature_shape": cluster.StrongestDetail.SignatureShapeScore,
					"fan_out":         cluster.StrongestDetail.FanOutScore,
					"span":            cluster.StrongestDetail.SpanScore,
				},
			},
		})
	}
}

func allowCallFamilyModuleCluster(cluster callFamilyCluster) bool {
	if len(cluster.Members) == 0 {
		return false
	}
	lang := cluster.Members[0].Language
	if lang == "python" {
		if strings.TrimSpace(cluster.DominantContainer) != "" {
			return true
		}
		if cluster.UniqueFileCount >= 3 {
			return true
		}
		if cluster.AverageSimilarity >= 90 && cluster.AverageFanOut >= 6 && cluster.AverageSymbolLines >= 24 {
			return true
		}
		return false
	}
	if lang == "elixir" {
		if cluster.StrongestDetail.NamespaceScore >= 80 {
			return true
		}
		if cluster.StrongestDetail.DistinctivenessScore >= 70 && cluster.AverageSimilarity >= 78 {
			return true
		}
		return false
	}
	return true
}

func suppressRedundantClusterFindings(state *scoutState) {
	if state == nil || len(state.Findings) == 0 {
		return
	}
	moduleClusters := make([]finding, 0)
	for _, item := range state.Findings {
		if item.RuleID == "structural_similarity_module_cluster" || item.RuleID == "call_family_module_cluster" {
			moduleClusters = append(moduleClusters, item)
		}
	}
	if len(moduleClusters) == 0 {
		return
	}
	filtered := state.Findings[:0]
	for _, item := range state.Findings {
		if !isFileScopeCluster(item) {
			filtered = append(filtered, item)
			continue
		}
		if clusterCoveredByModuleCluster(item, moduleClusters) {
			continue
		}
		filtered = append(filtered, item)
	}
	state.Findings = filtered
}

func isFileScopeCluster(item finding) bool {
	return item.RuleID == "structural_similarity_cluster" || item.RuleID == "call_family_cluster"
}

func clusterCoveredByModuleCluster(item finding, moduleClusters []finding) bool {
	itemMode, _ := clusterModeAndSeam(item)
	itemScope, _ := item.Evidence["scope_path"].(string)
	itemMembers := evidenceStringSlice(item.Evidence["member_symbols"])
	if itemMode == "" || itemScope == "" || len(itemMembers) == 0 {
		return false
	}
	itemSeam, _ := item.Evidence["seam_kind"].(string)
	for _, module := range moduleClusters {
		moduleMode, moduleSeam := clusterModeAndSeam(module)
		if moduleMode != itemMode || moduleSeam != itemSeam {
			continue
		}
		moduleScope, _ := module.Evidence["scope_path"].(string)
		if moduleScope == "" || moduleScope == itemScope {
			continue
		}
		if module.File == "" || item.File == "" || !strings.HasPrefix(item.File, moduleScope+"/") {
			continue
		}
		moduleMembers := evidenceStringSlice(module.Evidence["member_symbols"])
		if len(moduleMembers) == 0 {
			continue
		}
		if !stringSliceSubset(itemMembers, moduleMembers) {
			continue
		}
		if module.Score < item.Score {
			continue
		}
		return true
	}
	return false
}

func clusterModeAndSeam(item finding) (string, string) {
	mode, _ := item.Evidence["similarity_mode"].(string)
	if mode == "" {
		switch item.RuleID {
		case "structural_similarity_cluster", "structural_similarity_module_cluster":
			mode = "structural"
		case "call_family_cluster", "call_family_module_cluster":
			mode = "call_family"
		}
	}
	seam, _ := item.Evidence["seam_kind"].(string)
	return mode, seam
}

func evidenceStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if strings, ok := value.([]string); ok {
			return append([]string(nil), strings...)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func stringSliceSubset(left, right []string) bool {
	if len(left) == 0 {
		return true
	}
	rightSet := make(map[string]struct{}, len(right))
	for _, item := range right {
		rightSet[item] = struct{}{}
	}
	for _, item := range left {
		if _, ok := rightSet[item]; !ok {
			return false
		}
	}
	return true
}

func topStructuralSimilarityClustersByFile(state *scoutState, peerMap map[string][]similarObservation) []structuralSimilarityCluster {
	bestByFile := make(map[string]structuralSimilarityCluster)
	visited := make(map[string]struct{})
	for file, keys := range state.FileSymbols {
		for _, key := range keys {
			if _, ok := visited[key]; ok {
				continue
			}
			localPeers := localSimilarPeers(peerMap[key], file)
			if len(localPeers) == 0 {
				visited[key] = struct{}{}
				continue
			}
			cluster := buildStructuralSimilarityCluster(state, peerMap, file, key, visited)
			if len(cluster.Members) < 2 {
				continue
			}
			current, ok := bestByFile[file]
			if !ok || compareStructuralClusters(cluster, current) > 0 {
				bestByFile[file] = cluster
			}
		}
	}
	out := make([]structuralSimilarityCluster, 0, len(bestByFile))
	for _, cluster := range bestByFile {
		out = append(out, cluster)
	}
	sort.Slice(out, func(i, j int) bool {
		leftScore := scoreStructuralSimilarityCluster(len(out[i].Members), out[i].EdgeCount, out[i].MaxSimilarity, out[i].AverageSimilarity, out[i].UniqueFileCount, out[i].AverageBranches, out[i].AverageCallSites, out[i].AverageFanOut)
		rightScore := scoreStructuralSimilarityCluster(len(out[j].Members), out[j].EdgeCount, out[j].MaxSimilarity, out[j].AverageSimilarity, out[j].UniqueFileCount, out[j].AverageBranches, out[j].AverageCallSites, out[j].AverageFanOut)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].EntryLine < out[j].EntryLine
	})
	return out
}

func topCallFamilyClustersByFile(state *scoutState, peerMap map[string][]callFamilyPeer) []callFamilyCluster {
	bestByFile := make(map[string]callFamilyCluster)
	visited := make(map[string]struct{})
	for file, keys := range state.FileSymbols {
		for _, key := range keys {
			if _, ok := visited[key]; ok {
				continue
			}
			localPeers := localCallFamilyPeers(peerMap[key], file)
			if len(localPeers) == 0 {
				visited[key] = struct{}{}
				continue
			}
			cluster := buildCallFamilyClusterInScope(state, peerMap, file, key, visited, func(obs *symbolObservation) bool {
				return obs != nil && obs.File == file
			}, func(peers []callFamilyPeer) []callFamilyPeer {
				return localCallFamilyPeers(peers, file)
			})
			if len(cluster.Members) < 2 {
				continue
			}
			current, ok := bestByFile[file]
			if !ok || compareCallFamilyClusters(cluster, current) > 0 {
				bestByFile[file] = cluster
			}
		}
	}
	out := make([]callFamilyCluster, 0, len(bestByFile))
	for _, cluster := range bestByFile {
		out = append(out, cluster)
	}
	sort.Slice(out, func(i, j int) bool {
		leftScore := scoreCallFamilyCluster(len(out[i].Members), out[i].EdgeCount, out[i].MaxSimilarity, out[i].AverageSimilarity, out[i].UniqueFileCount, out[i].AverageFanOut, out[i].AverageSymbolLines, out[i].AdapterSurfaceScore)
		rightScore := scoreCallFamilyCluster(len(out[j].Members), out[j].EdgeCount, out[j].MaxSimilarity, out[j].AverageSimilarity, out[j].UniqueFileCount, out[j].AverageFanOut, out[j].AverageSymbolLines, out[j].AdapterSurfaceScore)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].EntryLine < out[j].EntryLine
	})
	return out
}

func topCallFamilyClustersByDirectory(state *scoutState, peerMap map[string][]callFamilyPeer) []callFamilyCluster {
	bestByDir := make(map[string]callFamilyCluster)
	visited := make(map[string]struct{})
	for _, keys := range state.FileSymbols {
		for _, key := range keys {
			if _, ok := visited[key]; ok {
				continue
			}
			obs := state.Symbols[key]
			if obs == nil {
				continue
			}
			dir := callFamilyModuleScopeForObservation(obs)
			if strings.TrimSpace(dir) == "" {
				visited[key] = struct{}{}
				continue
			}
			localPeers := directoryCallFamilyPeers(peerMap[key], dir)
			if len(localPeers) == 0 {
				visited[key] = struct{}{}
				continue
			}
			cluster := buildCallFamilyClusterInScope(state, peerMap, dir, key, visited, func(obs *symbolObservation) bool {
				return obs != nil && callFamilyModuleScopeForObservation(obs) == dir
			}, func(peers []callFamilyPeer) []callFamilyPeer {
				return directoryCallFamilyPeers(peers, dir)
			})
			if len(cluster.Members) < 2 || cluster.UniqueFileCount < 2 {
				continue
			}
			current, ok := bestByDir[dir]
			if !ok || compareCallFamilyClusters(cluster, current) > 0 {
				bestByDir[dir] = cluster
			}
		}
	}
	out := make([]callFamilyCluster, 0, len(bestByDir))
	for _, cluster := range bestByDir {
		out = append(out, cluster)
	}
	sort.Slice(out, func(i, j int) bool {
		leftScore := scoreCallFamilyCluster(len(out[i].Members), out[i].EdgeCount, out[i].MaxSimilarity, out[i].AverageSimilarity, out[i].UniqueFileCount, out[i].AverageFanOut, out[i].AverageSymbolLines, out[i].AdapterSurfaceScore)
		rightScore := scoreCallFamilyCluster(len(out[j].Members), out[j].EdgeCount, out[j].MaxSimilarity, out[j].AverageSimilarity, out[j].UniqueFileCount, out[j].AverageFanOut, out[j].AverageSymbolLines, out[j].AdapterSurfaceScore)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if out[i].ScopePath != out[j].ScopePath {
			return out[i].ScopePath < out[j].ScopePath
		}
		return out[i].EntryLine < out[j].EntryLine
	})
	return out
}

func topStructuralSimilarityClustersByDirectory(state *scoutState, peerMap map[string][]similarObservation) []structuralSimilarityCluster {
	bestByDir := make(map[string]structuralSimilarityCluster)
	visited := make(map[string]struct{})
	for _, keys := range state.FileSymbols {
		for _, key := range keys {
			if _, ok := visited[key]; ok {
				continue
			}
			obs := state.Symbols[key]
			if obs == nil {
				continue
			}
			dir := moduleScopeFor(obs.File)
			localPeers := directorySimilarPeers(peerMap[key], dir)
			if len(localPeers) == 0 {
				visited[key] = struct{}{}
				continue
			}
			cluster := buildStructuralSimilarityDirectoryCluster(state, peerMap, dir, key, visited)
			if len(cluster.Members) < 2 || cluster.UniqueFileCount < 2 {
				continue
			}
			current, ok := bestByDir[dir]
			if !ok || compareStructuralClusters(cluster, current) > 0 {
				bestByDir[dir] = cluster
			}
		}
	}
	out := make([]structuralSimilarityCluster, 0, len(bestByDir))
	for _, cluster := range bestByDir {
		out = append(out, cluster)
	}
	sort.Slice(out, func(i, j int) bool {
		leftScore := scoreStructuralSimilarityCluster(len(out[i].Members), out[i].EdgeCount, out[i].MaxSimilarity, out[i].AverageSimilarity, out[i].UniqueFileCount, out[i].AverageBranches, out[i].AverageCallSites, out[i].AverageFanOut)
		rightScore := scoreStructuralSimilarityCluster(len(out[j].Members), out[j].EdgeCount, out[j].MaxSimilarity, out[j].AverageSimilarity, out[j].UniqueFileCount, out[j].AverageBranches, out[j].AverageCallSites, out[j].AverageFanOut)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if out[i].ScopePath != out[j].ScopePath {
			return out[i].ScopePath < out[j].ScopePath
		}
		return out[i].EntryLine < out[j].EntryLine
	})
	return out
}

func buildStructuralSimilarityCluster(state *scoutState, peerMap map[string][]similarObservation, file, startKey string, visited map[string]struct{}) structuralSimilarityCluster {
	return buildStructuralSimilarityClusterInScope(state, peerMap, file, startKey, visited, func(obs *symbolObservation) bool {
		return obs != nil && obs.File == file
	}, func(peers []similarObservation) []similarObservation {
		return localSimilarPeers(peers, file)
	})
}

func buildStructuralSimilarityDirectoryCluster(state *scoutState, peerMap map[string][]similarObservation, dir, startKey string, visited map[string]struct{}) structuralSimilarityCluster {
	return buildStructuralSimilarityClusterInScope(state, peerMap, dir, startKey, visited, func(obs *symbolObservation) bool {
		return obs != nil && moduleScopeFor(obs.File) == dir
	}, func(peers []similarObservation) []similarObservation {
		return directorySimilarPeers(peers, dir)
	})
}

func buildCallFamilyClusterInScope(
	state *scoutState,
	peerMap map[string][]callFamilyPeer,
	scopePath, startKey string,
	visited map[string]struct{},
	inScope func(*symbolObservation) bool,
	filterPeers func([]callFamilyPeer) []callFamilyPeer,
) callFamilyCluster {
	queue := []string{startKey}
	componentKeys := make([]string, 0, 4)
	componentSet := make(map[string]struct{}, 4)
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		if _, ok := visited[key]; ok {
			continue
		}
		visited[key] = struct{}{}
		obs := state.Symbols[key]
		if obs == nil || !inScope(obs) {
			continue
		}
		componentKeys = append(componentKeys, key)
		componentSet[key] = struct{}{}
		for _, peer := range filterPeers(peerMap[key]) {
			peerKey := observationKey(peer.Observation.File, peer.Observation.Symbol)
			if _, ok := visited[peerKey]; ok {
				continue
			}
			queue = append(queue, peerKey)
		}
	}
	if len(componentKeys) == 0 {
		return callFamilyCluster{}
	}
	sort.Strings(componentKeys)
	members := make([]*symbolObservation, 0, len(componentKeys))
	memberFilesSet := make(map[string]struct{}, len(componentKeys))
	entryLine := 0
	representativeFile := ""
	edgeCount := 0
	totalSimilarity := 0
	totalFanOut := 0
	totalSymbolLines := 0
	totalParamCount := 0
	totalReturnCount := 0
	minFanOut := 0
	maxFanOut := 0
	minSymbolLines := 0
	maxSymbolLines := 0
	minParams := 0
	maxParams := 0
	minReturns := 0
	maxReturns := 0
	containerCounts := make(map[string]int)
	maxSimilarity := 0
	strongestPair := [2]string{}
	strongestDetail := callFamilySimilarityDetails{}
	seenEdges := make(map[string]struct{})
	for _, key := range componentKeys {
		obs := state.Symbols[key]
		if obs == nil {
			continue
		}
		members = append(members, obs)
		memberFilesSet[obs.File] = struct{}{}
		totalFanOut += obs.FanOut
		totalSymbolLines += obs.SymbolLines
		totalParamCount += obs.ParamCount
		totalReturnCount += obs.ReturnCount
		minFanOut, maxFanOut = updateRange(minFanOut, maxFanOut, obs.FanOut, len(members) == 1)
		minSymbolLines, maxSymbolLines = updateRange(minSymbolLines, maxSymbolLines, obs.SymbolLines, len(members) == 1)
		minParams, maxParams = updateRange(minParams, maxParams, obs.ParamCount, len(members) == 1)
		minReturns, maxReturns = updateRange(minReturns, maxReturns, obs.ReturnCount, len(members) == 1)
		containerCounts[symbolContainer(obs.Symbol)]++
		if representativeFile == "" || obs.File < representativeFile || (obs.File == representativeFile && (entryLine == 0 || obs.Line < entryLine)) {
			representativeFile = obs.File
			entryLine = obs.Line
		}
		for _, peer := range filterPeers(peerMap[key]) {
			peerKey := observationKey(peer.Observation.File, peer.Observation.Symbol)
			if _, ok := componentSet[peerKey]; !ok {
				continue
			}
			edgeID := orderedPairKey(key, peerKey)
			if _, ok := seenEdges[edgeID]; ok {
				continue
			}
			seenEdges[edgeID] = struct{}{}
			edgeCount++
			totalSimilarity += peer.Similarity
			if peer.Similarity > maxSimilarity {
				maxSimilarity = peer.Similarity
				strongestPair = [2]string{obs.Symbol, peer.Observation.Symbol}
				strongestDetail = peer.Details
			}
		}
	}
	memberFiles := sortedKeys(memberFilesSet)
	avgSimilarity := 0
	if edgeCount > 0 {
		avgSimilarity = totalSimilarity / edgeCount
	}
	avgFanOut := 0
	avgSymbolLines := 0
	avgParamCount := 0
	avgReturnCount := 0
	if len(members) > 0 {
		avgFanOut = totalFanOut / len(members)
		avgSymbolLines = totalSymbolLines / len(members)
		avgParamCount = totalParamCount / len(members)
		avgReturnCount = totalReturnCount / len(members)
	}
	dominantContainer, dominantContainerRatio := dominantContainerStats(containerCounts, len(members))
	adapterSurfaceScore := scoreAdapterSurfaceCluster(len(members), len(memberFiles), dominantContainerRatio, 0, 0, maxFanOut-minFanOut, maxSymbolLines-minSymbolLines, maxParams-minParams, maxReturns-minReturns)
	return callFamilyCluster{
		ScopePath:              scopePath,
		File:                   representativeFile,
		Members:                members,
		MemberFiles:            memberFiles,
		UniqueFileCount:        len(memberFiles),
		EntryLine:              entryLine,
		EdgeCount:              edgeCount,
		AverageSimilarity:      avgSimilarity,
		MaxSimilarity:          maxSimilarity,
		AverageFanOut:          avgFanOut,
		AverageSymbolLines:     avgSymbolLines,
		AverageParamCount:      avgParamCount,
		AverageReturnCount:     avgReturnCount,
		DominantContainer:      dominantContainer,
		DominantContainerRatio: dominantContainerRatio,
		AdapterSurfaceScore:    adapterSurfaceScore,
		StrongestPair:          strongestPair,
		StrongestDetail:        strongestDetail,
	}
}

func buildStructuralSimilarityClusterInScope(
	state *scoutState,
	peerMap map[string][]similarObservation,
	scopePath, startKey string,
	visited map[string]struct{},
	inScope func(*symbolObservation) bool,
	filterPeers func([]similarObservation) []similarObservation,
) structuralSimilarityCluster {
	queue := []string{startKey}
	componentKeys := make([]string, 0, 4)
	componentSet := make(map[string]struct{}, 4)
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		if _, ok := visited[key]; ok {
			continue
		}
		visited[key] = struct{}{}
		obs := state.Symbols[key]
		if obs == nil || !inScope(obs) {
			continue
		}
		componentKeys = append(componentKeys, key)
		componentSet[key] = struct{}{}
		for _, peer := range filterPeers(peerMap[key]) {
			peerKey := observationKey(peer.Observation.File, peer.Observation.Symbol)
			if _, ok := visited[peerKey]; ok {
				continue
			}
			queue = append(queue, peerKey)
		}
	}
	if len(componentKeys) == 0 {
		return structuralSimilarityCluster{}
	}
	sort.Strings(componentKeys)
	members := make([]*symbolObservation, 0, len(componentKeys))
	memberFilesSet := make(map[string]struct{}, len(componentKeys))
	entryLine := 0
	representativeFile := ""
	edgeCount := 0
	totalSimilarity := 0
	totalBranches := 0
	totalCallSites := 0
	totalFanOut := 0
	totalSymbolLines := 0
	maxSimilarity := 0
	minBranches := 0
	maxBranches := 0
	minCallSites := 0
	maxCallSites := 0
	minFanOut := 0
	maxFanOut := 0
	minSymbolLines := 0
	maxSymbolLines := 0
	minParams := 0
	maxParams := 0
	minReturns := 0
	maxReturns := 0
	containerCounts := make(map[string]int)
	strongestPair := [2]string{}
	strongestDetail := orchestrationSimilarityDetails{}
	seenEdges := make(map[string]struct{})
	for _, key := range componentKeys {
		obs := state.Symbols[key]
		if obs == nil {
			continue
		}
		members = append(members, obs)
		memberFilesSet[obs.File] = struct{}{}
		totalBranches += obs.BranchCount
		totalCallSites += obs.CallSiteCount
		totalFanOut += obs.FanOut
		totalSymbolLines += obs.SymbolLines
		minBranches, maxBranches = updateRange(minBranches, maxBranches, obs.BranchCount, len(members) == 1)
		minCallSites, maxCallSites = updateRange(minCallSites, maxCallSites, obs.CallSiteCount, len(members) == 1)
		minFanOut, maxFanOut = updateRange(minFanOut, maxFanOut, obs.FanOut, len(members) == 1)
		minSymbolLines, maxSymbolLines = updateRange(minSymbolLines, maxSymbolLines, obs.SymbolLines, len(members) == 1)
		minParams, maxParams = updateRange(minParams, maxParams, obs.ParamCount, len(members) == 1)
		minReturns, maxReturns = updateRange(minReturns, maxReturns, obs.ReturnCount, len(members) == 1)
		containerCounts[symbolContainer(obs.Symbol)]++
		if representativeFile == "" || obs.File < representativeFile || (obs.File == representativeFile && (entryLine == 0 || obs.Line < entryLine)) {
			representativeFile = obs.File
			entryLine = obs.Line
		}
		for _, peer := range filterPeers(peerMap[key]) {
			peerKey := observationKey(peer.Observation.File, peer.Observation.Symbol)
			if _, ok := componentSet[peerKey]; !ok {
				continue
			}
			edgeID := orderedPairKey(key, peerKey)
			if _, ok := seenEdges[edgeID]; ok {
				continue
			}
			seenEdges[edgeID] = struct{}{}
			edgeCount++
			totalSimilarity += peer.Similarity
			if peer.Similarity > maxSimilarity {
				maxSimilarity = peer.Similarity
				strongestPair = [2]string{obs.Symbol, peer.Observation.Symbol}
				strongestDetail = peer.Details
			}
		}
	}
	memberFiles := sortedKeys(memberFilesSet)
	avgSimilarity := 0
	if edgeCount > 0 {
		avgSimilarity = totalSimilarity / edgeCount
	}
	avgBranches := 0
	avgCallSites := 0
	avgFanOut := 0
	avgSymbolLines := 0
	if len(members) > 0 {
		avgBranches = totalBranches / len(members)
		avgCallSites = totalCallSites / len(members)
		avgFanOut = totalFanOut / len(members)
		avgSymbolLines = totalSymbolLines / len(members)
	}
	dominantContainer, dominantContainerRatio := dominantContainerStats(containerCounts, len(members))
	branchRange := maxBranches - minBranches
	callSiteRange := maxCallSites - minCallSites
	fanOutRange := maxFanOut - minFanOut
	symbolLineRange := maxSymbolLines - minSymbolLines
	paramRange := maxParams - minParams
	returnRange := maxReturns - minReturns
	adapterSurfaceScore := scoreAdapterSurfaceCluster(len(members), len(memberFiles), dominantContainerRatio, branchRange, callSiteRange, fanOutRange, symbolLineRange, paramRange, returnRange)
	return structuralSimilarityCluster{
		ScopePath:              scopePath,
		File:                   representativeFile,
		MemberKeys:             componentKeys,
		Members:                members,
		MemberFiles:            memberFiles,
		UniqueFileCount:        len(memberFiles),
		EntryLine:              entryLine,
		EdgeCount:              edgeCount,
		AverageSimilarity:      avgSimilarity,
		MaxSimilarity:          maxSimilarity,
		AverageBranches:        avgBranches,
		AverageCallSites:       avgCallSites,
		AverageFanOut:          avgFanOut,
		AverageSymbolLines:     avgSymbolLines,
		BranchRange:            branchRange,
		CallSiteRange:          callSiteRange,
		FanOutRange:            fanOutRange,
		SymbolLineRange:        symbolLineRange,
		ParamRange:             paramRange,
		ReturnRange:            returnRange,
		DominantContainer:      dominantContainer,
		DominantContainerRatio: dominantContainerRatio,
		AdapterSurfaceScore:    adapterSurfaceScore,
		StrongestPair:          strongestPair,
		StrongestDetail:        strongestDetail,
	}
}

func updateRange(minValue, maxValue, value int, initialize bool) (int, int) {
	if initialize {
		return value, value
	}
	if value < minValue {
		minValue = value
	}
	if value > maxValue {
		maxValue = value
	}
	return minValue, maxValue
}

func symbolContainer(symbol string) string {
	symbol = canonicalSymbolName(symbol)
	idx := strings.LastIndex(symbol, ".")
	if idx < 0 {
		return ""
	}
	return symbol[:idx]
}

func dominantContainerStats(counts map[string]int, total int) (string, int) {
	if len(counts) == 0 || total <= 0 {
		return "", 0
	}
	bestName := ""
	bestCount := 0
	for name, count := range counts {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if count > bestCount || (count == bestCount && name < bestName) {
			bestName = name
			bestCount = count
		}
	}
	if bestCount == 0 {
		return "", 0
	}
	return bestName, (bestCount * 100) / total
}

func localSimilarPeers(peers []similarObservation, file string) []similarObservation {
	if len(peers) == 0 {
		return nil
	}
	out := make([]similarObservation, 0, len(peers))
	for _, peer := range peers {
		if peer.Observation == nil || peer.Observation.File != file {
			continue
		}
		out = append(out, peer)
	}
	return out
}

func directorySimilarPeers(peers []similarObservation, dir string) []similarObservation {
	if len(peers) == 0 {
		return nil
	}
	out := make([]similarObservation, 0, len(peers))
	for _, peer := range peers {
		if peer.Observation == nil || moduleScopeFor(peer.Observation.File) != dir {
			continue
		}
		out = append(out, peer)
	}
	return out
}

func localCallFamilyPeers(peers []callFamilyPeer, file string) []callFamilyPeer {
	if len(peers) == 0 {
		return nil
	}
	out := make([]callFamilyPeer, 0, len(peers))
	for _, peer := range peers {
		if peer.Observation == nil || peer.Observation.File != file {
			continue
		}
		out = append(out, peer)
	}
	return out
}

func directoryCallFamilyPeers(peers []callFamilyPeer, dir string) []callFamilyPeer {
	if len(peers) == 0 {
		return nil
	}
	out := make([]callFamilyPeer, 0, len(peers))
	for _, peer := range peers {
		if peer.Observation == nil || callFamilyModuleScopeForObservation(peer.Observation) != dir {
			continue
		}
		out = append(out, peer)
	}
	return out
}

func callFamilyModuleScopeForObservation(obs *symbolObservation) string {
	if obs == nil {
		return ""
	}
	if obs.Language == "elixir" {
		return elixirModuleClusterScope(obs.File)
	}
	return moduleScopeFor(obs.File)
}

func elixirModuleClusterScope(file string) string {
	parts := strings.Split(filepath.ToSlash(file), "/")
	libIdx := -1
	for i, part := range parts {
		if part == "lib" {
			libIdx = i
			break
		}
	}
	if libIdx < 0 {
		return ""
	}
	segmentsAfterLib := len(parts) - (libIdx + 1)
	if segmentsAfterLib < 3 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

func moduleScopeFor(file string) string {
	dir := filepath.Dir(file)
	if dir == "" {
		return "."
	}
	return dir
}

func orderedPairKey(left, right string) string {
	if left <= right {
		return left + "::" + right
	}
	return right + "::" + left
}

func compareStructuralClusters(left, right structuralSimilarityCluster) int {
	leftScore := scoreStructuralSimilarityCluster(len(left.Members), left.EdgeCount, left.MaxSimilarity, left.AverageSimilarity, left.UniqueFileCount, left.AverageBranches, left.AverageCallSites, left.AverageFanOut)
	rightScore := scoreStructuralSimilarityCluster(len(right.Members), right.EdgeCount, right.MaxSimilarity, right.AverageSimilarity, right.UniqueFileCount, right.AverageBranches, right.AverageCallSites, right.AverageFanOut)
	if leftScore != rightScore {
		return leftScore - rightScore
	}
	if left.UniqueFileCount != right.UniqueFileCount {
		return left.UniqueFileCount - right.UniqueFileCount
	}
	if len(left.Members) != len(right.Members) {
		return len(left.Members) - len(right.Members)
	}
	if left.MaxSimilarity != right.MaxSimilarity {
		return left.MaxSimilarity - right.MaxSimilarity
	}
	return right.EntryLine - left.EntryLine
}

func compareCallFamilyClusters(left, right callFamilyCluster) int {
	leftScore := scoreCallFamilyCluster(len(left.Members), left.EdgeCount, left.MaxSimilarity, left.AverageSimilarity, left.UniqueFileCount, left.AverageFanOut, left.AverageSymbolLines, left.AdapterSurfaceScore)
	rightScore := scoreCallFamilyCluster(len(right.Members), right.EdgeCount, right.MaxSimilarity, right.AverageSimilarity, right.UniqueFileCount, right.AverageFanOut, right.AverageSymbolLines, right.AdapterSurfaceScore)
	if leftScore != rightScore {
		return leftScore - rightScore
	}
	if left.UniqueFileCount != right.UniqueFileCount {
		return left.UniqueFileCount - right.UniqueFileCount
	}
	if len(left.Members) != len(right.Members) {
		return len(left.Members) - len(right.Members)
	}
	if left.MaxSimilarity != right.MaxSimilarity {
		return left.MaxSimilarity - right.MaxSimilarity
	}
	return right.EntryLine - left.EntryLine
}

func classifyStructuralCluster(cluster structuralSimilarityCluster) structuralSeamProfile {
	switch {
	case cluster.AdapterSurfaceScore >= 70 && (cluster.UniqueFileCount >= 3 || cluster.AverageBranches <= 6):
		return structuralSeamProfile{
			Kind:              "thin_wrapper_api_layer",
			Title:             "File contains a thin wrapper API seam",
			DetailTemplate:    "%s contains a cluster of %d structurally similar uniform wrappers, which suggests an adapter surface that wants consolidation instead of repeated per-endpoint glue.",
			SuggestedRefactor: "Collapse the repeated wrapper shape behind a shared helper, generic adapter, or registration path and keep only the minimal per-endpoint differences exposed.",
		}
	case cluster.AverageBranches >= 2 && cluster.AverageCallSites >= 8 && cluster.AverageFanOut >= 4:
		return structuralSeamProfile{
			Kind:              "workflow_abstraction",
			Title:             "File contains a workflow abstraction seam",
			DetailTemplate:    "%s contains a cluster of %d structurally similar branch-heavy functions, which suggests a shared workflow abstraction rather than repeated local orchestration.",
			SuggestedRefactor: "Extract the shared workflow contract and step sequence first, then keep each function focused on the small policy or adapter differences that remain.",
		}
	default:
		return structuralSeamProfile{
			Kind:              "shared_operation_family",
			Title:             "File contains a shared operation family seam",
			DetailTemplate:    "%s contains a cluster of %d structurally similar functions, which suggests a shared operation family with extraction boundaries stronger than isolated cleanup.",
			SuggestedRefactor: "Refactor this seam as a family: extract the common setup and result-shaping path first, then split the remaining operation-specific branches.",
		}
	}
}

func classifyCallFamilyCluster(cluster callFamilyCluster) structuralSeamProfile {
	switch {
	case cluster.AdapterSurfaceScore >= 70 && callFamilyLooksLikeThinWrapper(cluster):
		return structuralSeamProfile{
			Kind:              "thin_wrapper_api_layer",
			Title:             "File contains a thin wrapper API seam",
			DetailTemplate:    "%s contains a cluster of %d structurally similar call-family wrappers, which suggests an adapter surface that wants consolidation instead of repeated per-endpoint glue.",
			SuggestedRefactor: "Collapse the repeated wrapper shape behind a shared helper, client adapter, or registration path and keep only the minimal endpoint-specific pieces exposed.",
		}
	case cluster.AverageFanOut >= 5 && cluster.AverageSymbolLines >= 18:
		return structuralSeamProfile{
			Kind:              "shared_operation_family",
			Title:             "File contains a shared operation family seam",
			DetailTemplate:    "%s contains a cluster of %d functions with similar call families, which suggests a shared operation family with stronger extraction boundaries than isolated cleanup.",
			SuggestedRefactor: "Refactor this seam as a family: extract the common setup and response-shaping path first, then split the remaining operation-specific differences.",
		}
	default:
		return structuralSeamProfile{
			Kind:              "shared_operation_family",
			Title:             "File contains a shared operation family seam",
			DetailTemplate:    "%s contains a cluster of %d similar call-family functions, which suggests repeated operation scaffolding that should be consolidated.",
			SuggestedRefactor: "Extract the repeated call-family scaffolding into a helper or operation factory, then keep only the endpoint-specific details in each symbol.",
		}
	}
}

func callFamilyLooksLikeThinWrapper(cluster callFamilyCluster) bool {
	if len(cluster.Members) == 0 {
		return false
	}
	lang := cluster.Members[0].Language
	if lang != "elixir" {
		return true
	}
	return cluster.AverageFanOut <= 3 && cluster.AverageSymbolLines <= 14 && cluster.StrongestDetail.DistinctivenessScore <= 40
}

func scoreAdapterSurfaceCluster(clusterSize, uniqueFileCount, dominantContainerRatio, branchRange, callSiteRange, fanOutRange, symbolLineRange, paramRange, returnRange int) int {
	score := 0
	score += minInt(20, maxInt(0, clusterSize-2)*4)
	score += minInt(16, maxInt(0, uniqueFileCount-1)*4)
	score += minInt(28, maxInt(0, dominantContainerRatio-40)/2)
	switch paramRange {
	case 0:
		score += 10
	case 1:
		score += 5
	}
	switch returnRange {
	case 0:
		score += 10
	case 1:
		score += 5
	}
	switch {
	case branchRange <= 1:
		score += 8
	case branchRange <= 3:
		score += 4
	}
	switch {
	case callSiteRange <= 2:
		score += 8
	case callSiteRange <= 5:
		score += 4
	}
	switch {
	case fanOutRange <= 2:
		score += 8
	case fanOutRange <= 5:
		score += 4
	}
	switch {
	case symbolLineRange <= 20:
		score += 6
	case symbolLineRange <= 40:
		score += 3
	}
	return clampScore(score)
}

func moduleClusterTitle(profile structuralSeamProfile) string {
	switch profile.Kind {
	case "workflow_abstraction":
		return "Directory contains a cross-file workflow abstraction seam"
	case "thin_wrapper_api_layer":
		return "Directory contains a cross-file thin wrapper API seam"
	default:
		return "Directory contains a cross-file shared operation seam"
	}
}

func moduleClusterDetail(cluster structuralSimilarityCluster, profile structuralSeamProfile) string {
	switch profile.Kind {
	case "workflow_abstraction":
		return fmt.Sprintf("%s contains a cross-file cluster of %d structurally similar branch-heavy functions across %d files, which suggests a shared package-level workflow abstraction rather than repeated file-local orchestration.", cluster.ScopePath, len(cluster.Members), cluster.UniqueFileCount)
	case "thin_wrapper_api_layer":
		return fmt.Sprintf("%s contains a cross-file cluster of %d structurally similar low-branch wrappers across %d files, which suggests a thin package API layer that wants consolidation instead of duplicated per-file wrappers.", cluster.ScopePath, len(cluster.Members), cluster.UniqueFileCount)
	default:
		return fmt.Sprintf("%s contains a cross-file cluster of %d structurally similar functions across %d files, which reads as a shared operation family rather than isolated file-local cleanup.", cluster.ScopePath, len(cluster.Members), cluster.UniqueFileCount)
	}
}

func moduleCallFamilyClusterDetail(cluster callFamilyCluster, profile structuralSeamProfile) string {
	switch profile.Kind {
	case "thin_wrapper_api_layer":
		return fmt.Sprintf("%s contains a cross-file cluster of %d structurally similar call-family wrappers across %d files, which suggests a thin package API layer that wants consolidation instead of duplicated adapter glue.", cluster.ScopePath, len(cluster.Members), cluster.UniqueFileCount)
	default:
		return fmt.Sprintf("%s contains a cross-file cluster of %d functions with similar call families across %d files, which reads as a shared operation family rather than isolated file-local cleanup.", cluster.ScopePath, len(cluster.Members), cluster.UniqueFileCount)
	}
}

func finalizeDuplicateOrchestrationFindings(state *scoutState, peerMap map[string][]similarObservation) {
	for key, peers := range peerMap {
		obs := state.Symbols[key]
		if obs == nil || len(peers) == 0 {
			continue
		}
		sort.Slice(peers, func(i, j int) bool {
			if peers[i].Similarity != peers[j].Similarity {
				return peers[i].Similarity > peers[j].Similarity
			}
			return peers[i].Observation.Symbol < peers[j].Observation.Symbol
		})
		peerNames := make([]string, 0, len(peers))
		peerScores := make(map[string]int, len(peers))
		for _, peer := range peers {
			peerNames = append(peerNames, peer.Observation.Symbol)
			peerScores[peer.Observation.Symbol] = peer.Similarity
		}
		score := scoreDuplicateOrchestration(len(peers)+1, obs.BranchCount, obs.CallSiteCount, peers[0].Similarity)
		state.Findings = append(state.Findings, finding{
			RuleID:            "duplicate_orchestration_fingerprint",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function repeats a structurally similar orchestration skeleton",
			Detail:            fmt.Sprintf("%s is structurally similar to %d other Go function(s), which is a strong signal that orchestration steps want a shared helper or workflow type.", obs.Symbol, len(peers)),
			SuggestedRefactor: "Extract the shared orchestration sequence into a helper or small workflow object, then keep the current functions as thin entrypoints.",
			File:              obs.File,
			Line:              obs.Line,
			Symbol:            obs.Symbol,
			Language:          obs.Language,
			Confidence:        "high",
			Signals:           []string{"go_ast", "orchestration_similarity"},
			Evidence: map[string]any{
				"cluster_size":       len(peers) + 1,
				"branch_count":       obs.BranchCount,
				"call_sites":         obs.CallSiteCount,
				"top_similarity":     peers[0].Similarity,
				"why_similar":        peers[0].Details.WhySimilar,
				"shared_subsequence": sampleStrings(peers[0].Details.SharedSubsequence, 8),
				"shared_tokens":      peers[0].Details.SharedTokens,
				"similarity_breakdown": map[string]int{
					"sequence":   peers[0].Details.SequenceScore,
					"overlap":    peers[0].Details.OverlapScore,
					"call_sites": peers[0].Details.CallSiteScore,
					"branches":   peers[0].Details.BranchScore,
				},
				"peer_symbols":          sampleStrings(peerNames, 6),
				"peer_similarity":       peerScores,
				"orchestration_preview": truncateForEvidence(obs.OrchestrationFingerprint, 200),
			},
		})
	}
}

func finalizeSameFileExtractionFindings(state *scoutState) {
	type peerEvidence struct {
		peer               string
		sharedCalls        []string
		sameOrchestration  bool
		orchestrationWhy   string
		sharedSubsequence  []string
		sharedTokens       []string
		signatureAlignment bool
	}
	perSymbol := make(map[string][]peerEvidence)

	for _, keys := range state.FileSymbols {
		if len(keys) < 2 {
			continue
		}
		for i := 0; i < len(keys); i++ {
			left := state.Symbols[keys[i]]
			if left == nil {
				continue
			}
			for j := i + 1; j < len(keys); j++ {
				right := state.Symbols[keys[j]]
				if right == nil || left.Language != right.Language {
					continue
				}
				sharedCalls := intersectStrings(left.Calls, right.Calls)
				orchestrationSimilarity := 0
				if left.Language == "go" {
					details := orchestrationSimilarityDetailsFor(left, right)
					orchestrationSimilarity = details.Score
					if orchestrationSimilarity >= orchestrationSimilarityThreshold(state.Thresholds) {
						perSymbol[keys[i]] = append(perSymbol[keys[i]], peerEvidence{
							peer:               right.Symbol,
							sharedCalls:        sharedCalls,
							sameOrchestration:  true,
							orchestrationWhy:   details.WhySimilar,
							sharedSubsequence:  details.SharedSubsequence,
							sharedTokens:       details.SharedTokens,
							signatureAlignment: left.ParamCount == right.ParamCount && left.ReturnCount == right.ReturnCount,
						})
						perSymbol[keys[j]] = append(perSymbol[keys[j]], peerEvidence{
							peer:               left.Symbol,
							sharedCalls:        sharedCalls,
							sameOrchestration:  true,
							orchestrationWhy:   details.WhySimilar,
							sharedSubsequence:  details.SharedSubsequence,
							sharedTokens:       details.SharedTokens,
							signatureAlignment: left.ParamCount == right.ParamCount && left.ReturnCount == right.ReturnCount,
						})
						continue
					}
				}
				signatureAlignment := left.ParamCount == right.ParamCount && left.ReturnCount == right.ReturnCount
				minFanOut := minInt(left.FanOut, right.FanOut)
				substantialPair := maxInt(left.SymbolLines, right.SymbolLines) >= maxInt(18, state.Thresholds.FunctionLines/3)
				overlapOK := minFanOut > 0 && len(sharedCalls)*2 >= minFanOut
				qualifies := substantialPair && len(sharedCalls) >= state.Thresholds.SameFileSharedCalls && overlapOK && signatureAlignment
				if !qualifies {
					continue
				}
				perSymbol[keys[i]] = append(perSymbol[keys[i]], peerEvidence{
					peer:               right.Symbol,
					sharedCalls:        sharedCalls,
					sameOrchestration:  false,
					signatureAlignment: signatureAlignment,
				})
				perSymbol[keys[j]] = append(perSymbol[keys[j]], peerEvidence{
					peer:               left.Symbol,
					sharedCalls:        sharedCalls,
					sameOrchestration:  false,
					signatureAlignment: signatureAlignment,
				})
			}
		}
	}

	for key, peers := range perSymbol {
		obs := state.Symbols[key]
		if obs == nil || len(peers) == 0 {
			continue
		}
		sort.Slice(peers, func(i, j int) bool {
			if len(peers[i].sharedCalls) != len(peers[j].sharedCalls) {
				return len(peers[i].sharedCalls) > len(peers[j].sharedCalls)
			}
			return peers[i].peer < peers[j].peer
		})
		maxSharedCalls := 0
		sameOrchestrationPeers := 0
		peerNames := make([]string, 0, len(peers))
		sharedCallUnion := make(map[string]struct{})
		bestWhySimilar := ""
		bestSharedSubsequence := []string(nil)
		bestSharedTokens := []string(nil)
		for _, peer := range peers {
			peerNames = append(peerNames, peer.peer)
			if len(peer.sharedCalls) > maxSharedCalls {
				maxSharedCalls = len(peer.sharedCalls)
			}
			if peer.sameOrchestration {
				sameOrchestrationPeers++
				if bestWhySimilar == "" {
					bestWhySimilar = peer.orchestrationWhy
					bestSharedSubsequence = peer.sharedSubsequence
					bestSharedTokens = peer.sharedTokens
				}
			}
			for _, call := range peer.sharedCalls {
				sharedCallUnion[call] = struct{}{}
			}
		}
		score := scoreSameFileExtraction(len(peers), maxSharedCalls, sameOrchestrationPeers > 0)
		confidence := "medium"
		if sameOrchestrationPeers > 0 || obs.Language == "go" {
			confidence = "high"
		}
		state.Findings = append(state.Findings, finding{
			RuleID:            "same_file_extraction_candidate",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Sibling functions in the same file want a shared extraction",
			Detail:            fmt.Sprintf("%s overlaps with %d same-file sibling function(s), which suggests repeated setup or branch handling that should live behind a shared helper.", obs.Symbol, len(peers)),
			SuggestedRefactor: "Extract the shared setup or decision sequence into a helper, then keep each public entrypoint focused on what actually differs.",
			File:              obs.File,
			Line:              obs.Line,
			Symbol:            obs.Symbol,
			Language:          obs.Language,
			Confidence:        confidence,
			Signals:           []string{"call_extraction", "same_file_overlap"},
			Evidence: map[string]any{
				"peer_symbols":             sampleStrings(peerNames, 6),
				"shared_calls":             sampleStrings(sortedKeys(sharedCallUnion), 8),
				"peer_count":               len(peers),
				"max_shared_calls":         maxSharedCalls,
				"same_orchestration_peers": sameOrchestrationPeers,
				"why_similar":              bestWhySimilar,
				"shared_subsequence":       sampleStrings(bestSharedSubsequence, 8),
				"shared_tokens":            bestSharedTokens,
			},
		})
	}
}

func synthesizeCompositeFindings(state *scoutState) map[string]struct{} {
	if state == nil || len(state.Findings) == 0 {
		return nil
	}
	grouped := make(map[string][]finding)
	for _, item := range state.Findings {
		if !isFunctionConstituent(item) {
			continue
		}
		key := findingSymbolKey(item)
		if key == "" {
			continue
		}
		grouped[key] = append(grouped[key], item)
	}

	hotspots := make(map[string]struct{})
	for _, group := range grouped {
		if len(group) < 2 {
			continue
		}
		rules := make(map[string]finding)
		top := group[0]
		for _, item := range group {
			if item.Score > top.Score {
				top = item
			}
			if _, ok := rules[item.RuleID]; !ok {
				rules[item.RuleID] = item
			}
		}
		structuralCount, signatureCount, supportiveCount := hotspotRuleMix(rules)
		if !qualifiesFunctionHotspot(structuralCount, signatureCount, supportiveCount) {
			continue
		}

		ruleIDs := make([]string, 0, len(rules))
		signals := make(map[string]struct{})
		ruleScores := make(map[string]int)
		confidence := "medium"
		for ruleID, item := range rules {
			ruleIDs = append(ruleIDs, ruleID)
			ruleScores[ruleID] = item.Score
			for _, signal := range item.Signals {
				signals[signal] = struct{}{}
			}
			if item.Confidence == "high" {
				confidence = "high"
			}
		}
		sort.Strings(ruleIDs)
		score := clampScore(top.Score + 5*(len(ruleIDs)-1))
		hotspots[findingSymbolKey(top)] = struct{}{}
		state.Findings = append(state.Findings, finding{
			RuleID:            "function_hotspot",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function combines multiple refactoring signals",
			Detail:            fmt.Sprintf("%s triggers %d independent refactoring signals (%s), making it a stronger entrypoint than any single metric alone.", top.Symbol, len(ruleIDs), strings.Join(ruleIDs, ", ")),
			SuggestedRefactor: "Refactor this function as a unit: split orchestration from branch-heavy detail, then narrow the signature or outputs if they still read as overloaded.",
			File:              top.File,
			Line:              top.Line,
			Symbol:            top.Symbol,
			Language:          top.Language,
			Confidence:        confidence,
			Signals:           sortedKeys(signals),
			Evidence: map[string]any{
				"rules":       ruleIDs,
				"rule_scores": ruleScores,
				"mix": map[string]int{
					"structural": structuralCount,
					"signature":  signatureCount,
					"supportive": supportiveCount,
				},
			},
		})
	}
	return hotspots
}

func isFunctionConstituent(item finding) bool {
	switch item.RuleID {
	case "long_parameter_list", "boolean_parameter", "wide_return_tuple",
		"oversized_function", "high_cyclomatic_complexity", "deep_nesting", "oversized_symbol",
		"fan_out_dependency_spread", "preload_after_get_chain", "post_transaction_preload", "transaction_script_hotspot", "duplicate_orchestration_fingerprint", "duplicate_recovery_block", "duplicated_error_remap", "repeated_guard_ladder", "semantic_simplification_candidate", "same_file_extraction_candidate":
		return strings.TrimSpace(item.Symbol) != ""
	default:
		return false
	}
}

func suppressConstituentFindings(state *scoutState, hotspotSymbols map[string]struct{}) {
	if state == nil || len(state.Findings) == 0 || len(hotspotSymbols) == 0 {
		return
	}
	filtered := state.Findings[:0]
	for _, item := range state.Findings {
		if isFunctionConstituent(item) {
			if _, ok := hotspotSymbols[findingSymbolKey(item)]; ok {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	state.Findings = filtered
}

func shouldSuppressConstituentFindings(focus string) bool {
	return strings.TrimSpace(focus) != "slop"
}

func hotspotRuleMix(rules map[string]finding) (structural, signature, supportive int) {
	for ruleID := range rules {
		switch ruleID {
		case "high_cyclomatic_complexity", "oversized_function", "deep_nesting",
			"fan_out_dependency_spread", "preload_after_get_chain", "post_transaction_preload", "transaction_script_hotspot", "duplicate_orchestration_fingerprint", "duplicate_recovery_block", "duplicated_error_remap", "repeated_guard_ladder", "semantic_simplification_candidate":
			structural++
		case "long_parameter_list", "boolean_parameter", "wide_return_tuple":
			signature++
		case "oversized_symbol", "same_file_extraction_candidate":
			supportive++
		}
	}
	return structural, signature, supportive
}

func qualifiesFunctionHotspot(structural, signature, supportive int) bool {
	switch {
	case structural >= 2:
		return true
	case structural >= 1 && signature >= 1:
		return true
	case structural >= 1 && supportive >= 1:
		return true
	case signature >= 2:
		return true
	default:
		return false
	}
}

func diversifyFindings(items []finding, headLimit, maxPerRule, maxPerFile, maxPerSymbol int) []finding {
	if len(items) <= 1 || headLimit <= 0 {
		return items
	}
	headLimit = minInt(headLimit, len(items))
	used := make([]bool, len(items))
	ruleCounts := make(map[string]int)
	fileCounts := make(map[string]int)
	symbolCounts := make(map[string]int)
	out := make([]finding, 0, len(items))

	progress := true
	for len(out) < headLimit && progress {
		progress = false
		for i, item := range items {
			if used[i] {
				continue
			}
			if ruleCounts[item.RuleID] >= maxPerRule {
				continue
			}
			if fileCounts[item.File] >= maxPerFile {
				continue
			}
			key := findingSymbolKey(item)
			if key != "" && symbolCounts[key] >= maxPerSymbol {
				continue
			}
			used[i] = true
			out = append(out, item)
			ruleCounts[item.RuleID]++
			fileCounts[item.File]++
			if key != "" {
				symbolCounts[key]++
			}
			progress = true
			if len(out) >= headLimit {
				break
			}
		}
	}

	for i, item := range items {
		if !used[i] {
			out = append(out, item)
		}
	}
	return out
}

func findingSymbolKey(item finding) string {
	symbol := strings.TrimSpace(item.Symbol)
	if symbol == "" {
		return ""
	}
	return observationKey(item.File, symbol)
}

func parseSignatureMetrics(signature, lang string) signatureMetrics {
	sig := strings.TrimSpace(signature)
	start := strings.Index(sig, "(")
	if start < 0 {
		return signatureMetrics{}
	}
	end := findMatching(sig, start, '(', ')')
	if end < 0 {
		return signatureMetrics{}
	}

	params := splitTopLevel(sig[start+1 : end])
	metrics := signatureMetrics{ParamCount: countNonEmpty(params)}
	for _, param := range params {
		if isTypedBooleanParam(lang, param) {
			metrics.BoolParamCount++
		}
	}

	rest := strings.TrimSpace(sig[end+1:])
	switch lang {
	case "go":
		metrics.ReturnCount = parseGoReturnCount(rest)
	case "typescript", "javascript":
		metrics.ReturnCount = parseTSReturnCount(rest)
	case "python":
		metrics.ReturnCount = parsePythonReturnCount(rest)
	case "rust":
		metrics.ReturnCount = parseRustReturnCount(rest)
	}

	return metrics
}

func parseGoReturnCount(rest string) int {
	if rest == "" || rest == "{" {
		return 0
	}
	if idx := strings.Index(rest, "{"); idx >= 0 {
		rest = strings.TrimSpace(rest[:idx])
	}
	if rest == "" {
		return 0
	}
	if strings.HasPrefix(rest, "(") {
		end := findMatching(rest, 0, '(', ')')
		if end < 0 {
			return 0
		}
		return countNonEmpty(splitTopLevel(rest[1:end]))
	}
	return 1
}

func parseTSReturnCount(rest string) int {
	idx := strings.Index(rest, ":")
	if idx < 0 {
		return 0
	}
	ret := strings.TrimSpace(rest[idx+1:])
	if ret == "" {
		return 0
	}
	if strings.HasPrefix(ret, "[") {
		end := findMatching(ret, 0, '[', ']')
		if end >= 0 {
			return countNonEmpty(splitTopLevel(ret[1:end]))
		}
	}
	return 1
}

func parsePythonReturnCount(rest string) int {
	idx := strings.LastIndex(rest, "->")
	if idx < 0 {
		return 0
	}
	ret := strings.TrimSpace(rest[idx+2:])
	if ret == "" {
		return 0
	}
	lower := strings.ToLower(ret)
	if strings.HasPrefix(lower, "tuple[") {
		start := strings.Index(ret, "[")
		end := findMatching(ret, start, '[', ']')
		if start >= 0 && end > start {
			return countNonEmpty(splitTopLevel(ret[start+1 : end]))
		}
	}
	return 1
}

func parseRustReturnCount(rest string) int {
	idx := strings.LastIndex(rest, "->")
	if idx < 0 {
		return 0
	}
	ret := strings.TrimSpace(rest[idx+2:])
	if ret == "" {
		return 0
	}
	if idx := strings.Index(ret, "{"); idx >= 0 {
		ret = strings.TrimSpace(ret[:idx])
	}
	if ret == "" || ret == "!" {
		return 0
	}
	if strings.HasPrefix(ret, "(") {
		end := findMatching(ret, 0, '(', ')')
		if end >= 0 {
			return countNonEmpty(splitTopLevel(ret[1:end]))
		}
	}
	return 1
}

func isTypedBooleanParam(lang, param string) bool {
	value := strings.ToLower(strings.TrimSpace(param))
	if value == "" {
		return false
	}
	switch lang {
	case "go":
		return strings.HasSuffix(value, " bool")
	case "typescript", "javascript":
		return strings.Contains(value, ": boolean") || strings.HasSuffix(value, ":bool")
	case "python":
		return strings.Contains(value, ": bool")
	case "rust":
		return strings.Contains(value, ": bool")
	default:
		return false
	}
}

func splitTopLevel(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var parts []string
	start := 0
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	angleDepth := 0
	for i, r := range value {
		switch r {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '<':
			angleDepth++
		case '>':
			if angleDepth > 0 {
				angleDepth--
			}
		case ',':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 && angleDepth == 0 {
				parts = append(parts, strings.TrimSpace(value[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(value[start:]))
	return parts
}

func findMatching(value string, start int, open, close rune) int {
	if start < 0 || start >= len(value) {
		return -1
	}
	depth := 0
	for i, r := range value[start:] {
		switch r {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return start + i
			}
		}
	}
	return -1
}

func countNonEmpty(items []string) int {
	count := 0
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			count++
		}
	}
	return count
}

func sortFindings(items []finding) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		if categoryPriority(items[i]) != categoryPriority(items[j]) {
			return categoryPriority(items[i]) < categoryPriority(items[j])
		}
		if rulePriority(items[i].RuleID) != rulePriority(items[j].RuleID) {
			return rulePriority(items[i].RuleID) < rulePriority(items[j].RuleID)
		}
		if items[i].File != items[j].File {
			return items[i].File < items[j].File
		}
		if items[i].Line != items[j].Line {
			return items[i].Line < items[j].Line
		}
		return items[i].RuleID < items[j].RuleID
	})
}

func buildSummary(items []finding) map[string]int {
	out := map[string]int{
		"high":   0,
		"medium": 0,
		"low":    0,
	}
	for _, item := range items {
		out[item.Severity]++
	}
	return out
}

func buildScoutPresentation(items []finding, view string) scoutPresentation {
	presentation := scoutPresentation{
		View: view,
		Overview: scoutPresentationMeta{
			TopRuleFamilies: summarizeRuleFamilies(items, 5),
			TopFiles:        summarizeFiles(items, 5),
			TopSymbols:      summarizeSymbols(items, 5),
			NoiseIndicators: summarizeNoiseIndicators(items),
		},
		Lanes: scoutPresentationLanes{
			BestEntrypoints:       buildEntrypointLane(items, 8),
			DBAccessPatterns:      buildDBAccessLane(items, 8),
			RepeatedPatternFamily: buildRepeatedPatternLane(items, 8),
			ModuleSeams:           buildModuleSeamLane(items, 8),
			Backlog:               buildBacklogLane(items, 8),
		},
	}
	switch view {
	case "entrypoints":
		presentation.ActiveLane = "best_entrypoints"
	case "summary":
		presentation.ActiveLane = "overview"
	case "raw":
		presentation.ActiveLane = ""
	default:
		presentation.ActiveLane = "best_entrypoints"
	}
	return presentation
}

func summarizeRuleFamilies(items []finding, limit int) []scoutRuleFamilySummary {
	if len(items) == 0 {
		return nil
	}
	type acc struct {
		count    int
		maxScore int
	}
	index := make(map[string]acc)
	for _, item := range items {
		a := index[item.RuleID]
		a.count++
		if item.Score > a.maxScore {
			a.maxScore = item.Score
		}
		index[item.RuleID] = a
	}
	out := make([]scoutRuleFamilySummary, 0, len(index))
	total := len(items)
	for ruleID, a := range index {
		share := 0.0
		if total > 0 {
			share = float64(a.count) / float64(total)
		}
		out = append(out, scoutRuleFamilySummary{
			RuleID:   ruleID,
			Count:    a.count,
			MaxScore: a.maxScore,
			Share:    share,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].MaxScore != out[j].MaxScore {
			return out[i].MaxScore > out[j].MaxScore
		}
		return out[i].RuleID < out[j].RuleID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func summarizeFiles(items []finding, limit int) []scoutFileSummary {
	if len(items) == 0 {
		return nil
	}
	type acc struct {
		count     int
		maxScore  int
		topSymbol string
		rules     map[string]int
	}
	index := make(map[string]*acc)
	for _, item := range items {
		if strings.TrimSpace(item.File) == "" {
			continue
		}
		a := index[item.File]
		if a == nil {
			a = &acc{rules: make(map[string]int)}
			index[item.File] = a
		}
		a.count++
		a.rules[item.RuleID]++
		if item.Score > a.maxScore || (item.Score == a.maxScore && strings.TrimSpace(item.Symbol) != "" && a.topSymbol == "") {
			a.maxScore = item.Score
			a.topSymbol = item.Symbol
		}
	}
	out := make([]scoutFileSummary, 0, len(index))
	for file, a := range index {
		out = append(out, scoutFileSummary{
			File:          file,
			Count:         a.count,
			MaxScore:      a.maxScore,
			TopSymbol:     strings.TrimSpace(a.topSymbol),
			DominantRules: topCountKeys(a.rules, 3),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MaxScore != out[j].MaxScore {
			return out[i].MaxScore > out[j].MaxScore
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].File < out[j].File
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func summarizeSymbols(items []finding, limit int) []scoutSymbolSummary {
	type acc struct {
		file     string
		symbol   string
		line     int
		count    int
		maxScore int
		rules    map[string]struct{}
	}
	index := make(map[string]*acc)
	for _, item := range items {
		if strings.TrimSpace(item.Symbol) == "" {
			continue
		}
		key := findingSymbolKey(item)
		a := index[key]
		if a == nil {
			a = &acc{
				file:   item.File,
				symbol: item.Symbol,
				line:   item.Line,
				rules:  make(map[string]struct{}),
			}
			index[key] = a
		}
		a.count++
		if item.Score > a.maxScore {
			a.maxScore = item.Score
		}
		if a.line == 0 && item.Line > 0 {
			a.line = item.Line
		}
		a.rules[item.RuleID] = struct{}{}
	}
	out := make([]scoutSymbolSummary, 0, len(index))
	for _, a := range index {
		out = append(out, scoutSymbolSummary{
			File:     a.file,
			Symbol:   a.symbol,
			Line:     a.line,
			Count:    a.count,
			MaxScore: a.maxScore,
			RuleIDs:  sortedKeys(a.rules),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MaxScore != out[j].MaxScore {
			return out[i].MaxScore > out[j].MaxScore
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Symbol < out[j].Symbol
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func summarizeNoiseIndicators(items []finding) []scoutNoiseIndicator {
	if len(items) == 0 {
		return nil
	}
	ruleCounts := make(map[string]int)
	symbolRuleCounts := make(map[string]int)
	for _, item := range items {
		ruleCounts[item.RuleID]++
		if strings.TrimSpace(item.Symbol) != "" {
			symbolRuleCounts[findingSymbolKey(item)+"::"+item.RuleID]++
		}
	}
	out := make([]scoutNoiseIndicator, 0, 3)
	var dominantRule string
	dominantCount := 0
	for ruleID, count := range ruleCounts {
		if count > dominantCount || (count == dominantCount && ruleID < dominantRule) {
			dominantRule = ruleID
			dominantCount = count
		}
	}
	if dominantRule != "" {
		out = append(out, scoutNoiseIndicator{
			Kind:   "dominant_rule_share",
			Detail: dominantRule,
			Count:  dominantCount,
			Share:  float64(dominantCount) / float64(len(items)),
		})
	}
	duplicatedGroups := 0
	for _, count := range symbolRuleCounts {
		if count > 1 {
			duplicatedGroups++
		}
	}
	if duplicatedGroups > 0 {
		out = append(out, scoutNoiseIndicator{
			Kind:   "duplicate_symbol_rule_groups",
			Detail: "same symbol and rule emitted more than once",
			Count:  duplicatedGroups,
		})
	}
	clusterCount := 0
	for _, item := range items {
		if item.Category == "cluster" {
			clusterCount++
		}
	}
	if clusterCount > 0 {
		out = append(out, scoutNoiseIndicator{
			Kind:   "cluster_share",
			Detail: "cluster findings among returned results",
			Count:  clusterCount,
			Share:  float64(clusterCount) / float64(len(items)),
		})
	}
	return out
}

func buildEntrypointLane(items []finding, limit int) []scoutLaneItem {
	type group struct {
		rep   finding
		count int
		rules map[string]struct{}
	}
	index := make(map[string]*group)
	for _, item := range items {
		if strings.TrimSpace(item.Symbol) == "" {
			continue
		}
		if item.Category != "function" && item.RuleID != "function_hotspot" {
			continue
		}
		key := findingSymbolKey(item)
		g := index[key]
		if g == nil {
			g = &group{rep: item, rules: make(map[string]struct{})}
			index[key] = g
		}
		g.count++
		g.rules[item.RuleID] = struct{}{}
		if shouldReplaceRepresentative(g.rep, item) {
			g.rep = item
		}
	}
	out := make([]scoutLaneItem, 0, len(index))
	for _, g := range index {
		out = append(out, scoutLaneItem{
			File:               g.rep.File,
			Symbol:             g.rep.Symbol,
			Line:               g.rep.Line,
			MaxScore:           g.rep.Score,
			FindingCount:       g.count,
			RepresentativeRule: g.rep.RuleID,
			RuleIDs:            sortedKeys(g.rules),
			Summary:            scoutLaneSummary(g.rep),
			Category:           g.rep.Category,
			SeamKind:           evidenceString(g.rep.Evidence["seam_kind"]),
		})
	}
	sortLaneItems(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func buildRepeatedPatternLane(items []finding, limit int) []scoutLaneItem {
	type group struct {
		rep     finding
		count   int
		samples []string
	}
	index := make(map[string]*group)
	for _, item := range items {
		if !isPatternFamilyRule(item.RuleID) || strings.TrimSpace(item.Symbol) == "" {
			continue
		}
		key := findingSymbolKey(item) + "::" + item.RuleID
		g := index[key]
		if g == nil {
			g = &group{rep: item}
			index[key] = g
		}
		g.count++
		if shouldReplaceRepresentative(g.rep, item) {
			g.rep = item
		}
		g.samples = appendUniquePatternStrings(g.samples, patternEvidenceSamples(item)...)
	}
	out := make([]scoutLaneItem, 0, len(index))
	for _, g := range index {
		if g.count <= 1 && g.rep.RuleID != "semantic_simplification_candidate" {
			continue
		}
		summary := scoutLaneSummary(g.rep)
		if g.count > 1 {
			summary = fmt.Sprintf("%s emitted %d %s finding(s). %s", g.rep.Symbol, g.count, g.rep.RuleID, scoutLaneSummary(g.rep))
		}
		out = append(out, scoutLaneItem{
			File:               g.rep.File,
			Symbol:             g.rep.Symbol,
			Line:               g.rep.Line,
			MaxScore:           g.rep.Score,
			FindingCount:       g.count,
			RepresentativeRule: g.rep.RuleID,
			RuleIDs:            []string{g.rep.RuleID},
			Summary:            summary,
			Category:           g.rep.Category,
			Samples:            sampleStrings(g.samples, 4),
		})
	}
	sortLaneItems(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func buildDBAccessLane(items []finding, limit int) []scoutLaneItem {
	type group struct {
		rep        finding
		count      int
		rules      map[string]struct{}
		seenShapes map[string]struct{}
		samples    []string
	}
	index := make(map[string]*group)
	for _, item := range items {
		if !isDBAccessRule(item.RuleID) || strings.TrimSpace(item.Symbol) == "" {
			continue
		}
		key := findingSymbolKey(item)
		g := index[key]
		if g == nil {
			g = &group{rep: item, rules: make(map[string]struct{}), seenShapes: make(map[string]struct{})}
			index[key] = g
		}
		shapeKey := item.RuleID + "::" + fmt.Sprintf("%d", item.Line) + "::" + dbAccessShapeKey(item)
		if item.RuleID == "transaction_script_hotspot" {
			shapeKey = item.RuleID
		}
		if _, ok := g.seenShapes[shapeKey]; !ok {
			g.seenShapes[shapeKey] = struct{}{}
			g.count++
		}
		g.rules[item.RuleID] = struct{}{}
		g.samples = appendUniquePatternStrings(g.samples, patternEvidenceSamples(item)...)
		if shouldReplaceRepresentative(g.rep, item) {
			g.rep = item
		}
	}
	out := make([]scoutLaneItem, 0, len(index))
	for _, g := range index {
		displayCount := g.count
		if len(g.rules) == 1 {
			if _, ok := g.rules["transaction_script_hotspot"]; ok {
				displayCount = 1
			}
		}
		summary := scoutLaneSummary(g.rep)
		if displayCount > 1 {
			summary = fmt.Sprintf("%s carries %d DB access pattern finding(s). %s", g.rep.Symbol, displayCount, scoutLaneSummary(g.rep))
		}
		out = append(out, scoutLaneItem{
			File:               g.rep.File,
			Symbol:             g.rep.Symbol,
			Line:               g.rep.Line,
			MaxScore:           g.rep.Score,
			FindingCount:       displayCount,
			RepresentativeRule: g.rep.RuleID,
			RuleIDs:            sortedKeys(g.rules),
			Summary:            summary,
			Category:           g.rep.Category,
			SeamKind:           evidenceString(g.rep.Evidence["suggested_boundary_kind"]),
			Samples:            sampleStrings(g.samples, 4),
		})
	}
	sortLaneItems(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func dbAccessShapeKey(item finding) string {
	if item.Evidence == nil {
		return truncateForEvidence(item.Detail, 120)
	}
	if preview := evidenceString(item.Evidence["script_preview"]); preview != "" {
		return preview
	}
	if values := evidenceStrings(item.Evidence["chain_samples"]); len(values) > 0 {
		return strings.Join(values, "|")
	}
	return truncateForEvidence(item.Detail, 120)
}

func buildModuleSeamLane(items []finding, limit int) []scoutLaneItem {
	out := make([]scoutLaneItem, 0, len(items))
	seen := make(map[string]struct{})
	for _, item := range items {
		if item.Category != "cluster" && item.Category != "file" {
			continue
		}
		scopePath := evidenceString(item.Evidence["scope_path"])
		key := item.RuleID + "::" + item.File + "::" + scopePath + "::" + evidenceString(item.Evidence["seam_kind"])
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, scoutLaneItem{
			File:               item.File,
			Line:               item.Line,
			MaxScore:           item.Score,
			FindingCount:       1,
			RepresentativeRule: item.RuleID,
			RuleIDs:            []string{item.RuleID},
			Summary:            scoutLaneSummary(item),
			Category:           item.Category,
			SeamKind:           evidenceString(item.Evidence["seam_kind"]),
			ScopePath:          scopePath,
		})
	}
	sortLaneItems(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func buildBacklogLane(items []finding, limit int) []scoutLaneItem {
	coveredSymbols := make(map[string]struct{})
	for _, item := range buildEntrypointLane(items, 1000) {
		if strings.TrimSpace(item.Symbol) != "" {
			coveredSymbols[item.File+"\x00"+item.Symbol] = struct{}{}
		}
	}
	out := make([]scoutLaneItem, 0, len(items))
	seen := make(map[string]struct{})
	for _, item := range items {
		key := item.File + "::" + item.Symbol + "::" + item.RuleID + "::" + item.Category
		if _, ok := seen[key]; ok {
			continue
		}
		if strings.TrimSpace(item.Symbol) != "" {
			if _, ok := coveredSymbols[item.File+"\x00"+item.Symbol]; ok {
				continue
			}
		}
		seen[key] = struct{}{}
		out = append(out, scoutLaneItem{
			File:               item.File,
			Symbol:             item.Symbol,
			Line:               item.Line,
			MaxScore:           item.Score,
			FindingCount:       1,
			RepresentativeRule: item.RuleID,
			RuleIDs:            []string{item.RuleID},
			Summary:            scoutLaneSummary(item),
			Category:           item.Category,
			SeamKind:           evidenceString(item.Evidence["seam_kind"]),
			ScopePath:          evidenceString(item.Evidence["scope_path"]),
		})
	}
	sortLaneItems(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func shouldReplaceRepresentative(current, candidate finding) bool {
	if candidate.RuleID == "function_hotspot" && current.RuleID != "function_hotspot" {
		return true
	}
	if candidate.Score != current.Score {
		return candidate.Score > current.Score
	}
	return rulePriority(candidate.RuleID) < rulePriority(current.RuleID)
}

func isPatternFamilyRule(ruleID string) bool {
	switch ruleID {
	case "duplicate_recovery_block", "duplicated_error_remap", "repeated_guard_ladder", "semantic_simplification_candidate":
		return true
	default:
		return false
	}
}

func isDBAccessRule(ruleID string) bool {
	switch ruleID {
	case "preload_after_get_chain", "post_transaction_preload", "transaction_script_hotspot":
		return true
	default:
		return false
	}
}

func patternEvidenceSamples(item finding) []string {
	if item.Evidence == nil {
		return nil
	}
	var samples []string
	for _, key := range []string{"guard_preview", "simplified_form", "why_similar"} {
		if value := evidenceString(item.Evidence[key]); value != "" {
			samples = append(samples, truncateForEvidence(value, 120))
		}
	}
	for _, key := range []string{"script_preview"} {
		if value := evidenceString(item.Evidence[key]); value != "" {
			samples = append(samples, truncateForEvidence(value, 120))
		}
	}
	if values := evidenceStrings(item.Evidence["chain_samples"]); len(values) > 0 {
		samples = append(samples, sampleStrings(values, 2)...)
	}
	if values := evidenceStrings(item.Evidence["repo_calls"]); len(values) > 0 {
		samples = append(samples, sampleStrings(values, 3)...)
	}
	return samples
}

func scoutLaneSummary(item finding) string {
	detail := strings.TrimSpace(item.Detail)
	if detail == "" {
		detail = strings.TrimSpace(item.Title)
	}
	return truncateForEvidence(detail, 220)
}

func sortLaneItems(items []scoutLaneItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].MaxScore != items[j].MaxScore {
			return items[i].MaxScore > items[j].MaxScore
		}
		if items[i].FindingCount != items[j].FindingCount {
			return items[i].FindingCount > items[j].FindingCount
		}
		if items[i].File != items[j].File {
			return items[i].File < items[j].File
		}
		return items[i].Symbol < items[j].Symbol
	})
}

func topCountKeys(values map[string]int, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	type pair struct {
		key   string
		count int
	}
	pairs := make([]pair, 0, len(values))
	for key, count := range values {
		pairs = append(pairs, pair{key: key, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].key < pairs[j].key
	})
	if limit > 0 && len(pairs) > limit {
		pairs = pairs[:limit]
	}
	out := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, pair.key)
	}
	return out
}

func thresholdsFor(ruleSet string) thresholds {
	switch ruleSet {
	case "aggressive":
		return thresholds{
			ParamCount:          5,
			ReturnCount:         3,
			SymbolLines:         80,
			FunctionLines:       60,
			Cyclomatic:          10,
			Nesting:             4,
			InterfaceMethods:    7,
			FileSymbols:         10,
			FileLines:           240,
			ReceiverMethods:     8,
			MinFileComplexity:   2,
			FanOutCalls:         6,
			DuplicateCallSites:  4,
			DuplicateBranches:   1,
			SameFileSharedCalls: 2,
		}
	case "conservative":
		return thresholds{
			ParamCount:          7,
			ReturnCount:         4,
			SymbolLines:         120,
			FunctionLines:       100,
			Cyclomatic:          15,
			Nesting:             5,
			InterfaceMethods:    10,
			FileSymbols:         20,
			FileLines:           500,
			ReceiverMethods:     12,
			MinFileComplexity:   3,
			FanOutCalls:         10,
			DuplicateCallSites:  5,
			DuplicateBranches:   2,
			SameFileSharedCalls: 3,
		}
	default:
		return thresholds{
			ParamCount:          6,
			ReturnCount:         3,
			SymbolLines:         100,
			FunctionLines:       80,
			Cyclomatic:          12,
			Nesting:             4,
			InterfaceMethods:    8,
			FileSymbols:         14,
			FileLines:           300,
			ReceiverMethods:     10,
			MinFileComplexity:   2,
			FanOutCalls:         8,
			DuplicateCallSites:  4,
			DuplicateBranches:   1,
			SameFileSharedCalls: 2,
		}
	}
}

func clampScore(score int) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func scoreLongParameterList(paramCount int, th thresholds) int {
	extra := maxInt(0, paramCount-th.ParamCount)
	return clampScore(56 + minInt(18, extra*4))
}

func scoreBooleanParams(boolCount int) int {
	extra := maxInt(0, boolCount-1)
	return clampScore(62 + minInt(12, extra*4))
}

func scoreWideReturnTuple(returnCount int, th thresholds) int {
	extra := maxInt(0, returnCount-th.ReturnCount)
	return clampScore(57 + minInt(18, extra*5))
}

func scoreOversizedSymbol(symbolLines int, th thresholds) int {
	extra := maxInt(0, symbolLines-th.SymbolLines)
	return clampScore(50 + minInt(18, extra/10))
}

func scoreGodFile(symbolCount, lineCount int, th thresholds) int {
	symbolExtra := maxInt(0, symbolCount-th.FileSymbols)
	lineExtra := maxInt(0, lineCount-th.FileLines)
	return clampScore(58 + minInt(14, symbolExtra*2) + minInt(10, lineExtra/120))
}

func scoreOversizedFunction(length int, th thresholds) int {
	extra := maxInt(0, length-th.FunctionLines)
	return clampScore(60 + minInt(16, extra/12))
}

func scoreCyclomaticComplexity(cyclomatic int, th thresholds) int {
	extra := maxInt(0, cyclomatic-th.Cyclomatic)
	return clampScore(68 + minInt(24, extra*3))
}

func scoreDeepNesting(nesting int, th thresholds) int {
	extra := maxInt(0, nesting-th.Nesting)
	return clampScore(62 + minInt(18, extra*5))
}

func scoreWideInterface(methodCount int, th thresholds) int {
	extra := maxInt(0, methodCount-th.InterfaceMethods)
	return clampScore(60 + minInt(18, extra*4))
}

func scoreReceiverHotspot(methodCount int, th thresholds) int {
	extra := maxInt(0, methodCount-th.ReceiverMethods)
	return clampScore(61 + minInt(18, extra*3))
}

func scoreComplexityCluster(highRiskCount int, th thresholds) int {
	extra := maxInt(0, highRiskCount-th.MinFileComplexity)
	return clampScore(72 + minInt(14, extra))
}

func scoreFanOutDependencySpread(fanOut int, th thresholds) int {
	extra := maxInt(0, fanOut-th.FanOutCalls)
	return clampScore(67 + minInt(18, extra*3))
}

func scorePreloadAfterGetChain(chainCount int) int {
	score := 74 + minInt(10, maxInt(0, chainCount-1)*5)
	return clampScore(score)
}

func scorePostTransactionPreload(matchCount int) int {
	score := 73 + minInt(10, maxInt(0, matchCount-1)*5)
	return clampScore(score)
}

func scoreTransactionScriptHotspot(stmtCount, repoCalls, branchCount int) int {
	score := 76 +
		minInt(8, maxInt(0, stmtCount-3)*2) +
		minInt(8, maxInt(0, repoCalls-2)*3) +
		minInt(6, branchCount*3)
	return clampScore(score)
}

func scoreSemanticSimplification(patternCount, simplificationGain int, fullWrapper bool) int {
	score := 72 +
		minInt(10, maxInt(0, patternCount-1)*4) +
		minInt(12, maxInt(0, simplificationGain)*3)
	if fullWrapper {
		score += 6
	}
	return clampScore(score)
}

func scoreDuplicateOrchestration(clusterSize, branchCount, callSites, similarity int) int {
	return clampScore(70 + minInt(8, (clusterSize-2)*4) + minInt(8, maxInt(0, branchCount-1)*2) + minInt(6, maxInt(0, callSites-4)) + minInt(10, maxInt(0, similarity-70)/2))
}

func scoreStructuralSimilarityCluster(clusterSize, edgeCount, maxSimilarity, averageSimilarity, uniqueFileCount, averageBranches, averageCallSites, averageFanOut int) int {
	score := 68 +
		minInt(10, maxInt(0, clusterSize-2)*4) +
		minInt(8, maxInt(0, edgeCount-1)*2) +
		minInt(8, maxInt(0, uniqueFileCount-1)*3) +
		minInt(8, maxInt(0, maxSimilarity-70)/2) +
		minInt(6, maxInt(0, averageSimilarity-68)/4) +
		minInt(10, averageBranches*3) +
		minInt(8, maxInt(0, averageCallSites-2)*2) +
		minInt(8, maxInt(0, averageFanOut-2)*2)
	switch {
	case averageBranches == 0 && averageFanOut <= 3:
		score -= 14
	case averageBranches <= 1 && averageFanOut <= 3:
		score -= 8
	}
	return clampScore(score)
}

func scoreCallFamilyCluster(clusterSize, edgeCount, maxSimilarity, averageSimilarity, uniqueFileCount, averageFanOut, averageSymbolLines, adapterSurfaceScore int) int {
	score := 62 +
		minInt(8, maxInt(0, clusterSize-2)*3) +
		minInt(6, maxInt(0, edgeCount-1)*2) +
		minInt(8, maxInt(0, uniqueFileCount-1)*3) +
		minInt(8, maxInt(0, maxSimilarity-65)/2) +
		minInt(6, maxInt(0, averageSimilarity-62)/4) +
		minInt(8, maxInt(0, averageFanOut-2)*2) +
		minInt(6, maxInt(0, averageSymbolLines-12)/4)
	if adapterSurfaceScore >= 70 {
		score += 6
	}
	return clampScore(score)
}

func scoreSameFileExtraction(peerCount, maxSharedCalls int, sameOrchestration bool) int {
	score := 64 + minInt(6, maxInt(0, peerCount-1)*2) + minInt(10, maxInt(0, maxSharedCalls-2)*2)
	if sameOrchestration {
		score += 4
	}
	return clampScore(score)
}

func severityFor(score int) string {
	switch {
	case score >= 80:
		return "high"
	case score >= 60:
		return "medium"
	default:
		return "low"
	}
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func categoryPriority(item finding) int {
	switch item.Category {
	case "function":
		return 0
	case "cluster":
		return 1
	case "signature":
		return 2
	case "type":
		return 3
	case "file":
		return 4
	default:
		return 5
	}
}

func rulePriority(ruleID string) int {
	switch ruleID {
	case "function_hotspot":
		return 0
	case "unreachable_private_symbol":
		return 1
	case "test_only_helper":
		return 2
	case "stale_export_candidate":
		return 3
	case "orphan_file":
		return 4
	case "test_only_file":
		return 5
	case "stale_package_candidate":
		return 6
	case "test_only_package":
		return 7
	case "duplicated_error_remap":
		return 8
	case "preload_after_get_chain":
		return 9
	case "post_transaction_preload":
		return 10
	case "duplicate_recovery_block":
		return 11
	case "repeated_guard_ladder":
		return 12
	case "transaction_script_hotspot":
		return 13
	case "semantic_simplification_candidate":
		return 14
	case "duplicate_orchestration_fingerprint":
		return 15
	case "structural_similarity_module_cluster":
		return 8
	case "structural_similarity_cluster":
		return 9
	case "call_family_module_cluster":
		return 10
	case "call_family_cluster":
		return 11
	case "same_file_extraction_candidate":
		return 16
	case "fan_out_dependency_spread":
		return 17
	case "complexity_cluster":
		return 18
	case "receiver_hotspot":
		return 19
	case "god_file":
		return 20
	default:
		return 10
	}
}

func supportsFunctionSignals(kind symindex.Kind) bool {
	switch kind {
	case symindex.KindFunction, symindex.KindMethod:
		return true
	default:
		return false
	}
}

func supportsObservedFunctionSignals(sym symindex.Symbol, lang string, content []byte) bool {
	if supportsFunctionSignals(sym.Kind) {
		return true
	}
	switch lang {
	case "typescript", "javascript":
		switch sym.Kind {
		case symindex.KindConstant, symindex.KindVariable:
			return symbolLooksLikeTSCallable(sym, content)
		default:
			return false
		}
	default:
		return false
	}
}

func symbolLooksLikeTSCallable(sym symindex.Symbol, content []byte) bool {
	signature := strings.TrimSpace(sym.Signature)
	if strings.Contains(signature, "=>") || strings.Contains(signature, "function(") || strings.Contains(signature, "function ") {
		return true
	}
	body := extractObservedSymbolText(sym, content)
	if body == "" {
		return false
	}
	if len(body) > 320 {
		body = body[:320]
	}
	return strings.Contains(body, "=>") || strings.Contains(body, "function(") || strings.Contains(body, "function ")
}

func extractObservedSymbolText(sym symindex.Symbol, content []byte) string {
	start := sym.StartByte
	end := sym.EndByte
	if start < 0 || end > len(content) || start >= end {
		return ""
	}
	return string(content[start:end])
}

func ensureSymbolObservation(state *scoutState, file, symbol, language string, kind symindex.Kind, line int) *symbolObservation {
	if state.Symbols == nil {
		state.Symbols = make(map[string]*symbolObservation)
	}
	if state.FileSymbols == nil {
		state.FileSymbols = make(map[string][]string)
	}
	key := observationKey(file, symbol)
	if obs, ok := state.Symbols[key]; ok {
		if obs.Line == 0 {
			obs.Line = line
		}
		obs.Symbol = chooseDisplaySymbol(obs.Symbol, symbol)
		if obs.Language == "" {
			obs.Language = language
		}
		if obs.Kind == "" {
			obs.Kind = kind
		}
		return obs
	}
	obs := &symbolObservation{
		File:     file,
		Line:     line,
		Symbol:   symbol,
		Language: language,
		Kind:     kind,
	}
	state.Symbols[key] = obs
	state.FileSymbols[file] = append(state.FileSymbols[file], key)
	return obs
}

func observationKey(file, symbol string) string {
	return file + "::" + canonicalSymbolName(symbol)
}

func canonicalSymbolName(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	return strings.TrimPrefix(symbol, "*")
}

func chooseDisplaySymbol(current, candidate string) string {
	current = strings.TrimSpace(current)
	candidate = strings.TrimSpace(candidate)
	if current == "" {
		return candidate
	}
	if strings.HasPrefix(candidate, "*") && !strings.HasPrefix(current, "*") {
		return candidate
	}
	return current
}

func normalizeCallList(calls []string) []string {
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(calls))
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		call = strings.TrimSpace(call)
		if call == "" {
			continue
		}
		if _, ok := seen[call]; ok {
			continue
		}
		seen[call] = struct{}{}
		out = append(out, call)
	}
	return out
}

func sampleStrings(items []string, limit int) []string {
	if len(items) == 0 {
		return nil
	}
	if limit <= 0 || len(items) <= limit {
		out := append([]string(nil), items...)
		sort.Strings(out)
		return out
	}
	out := append([]string(nil), items[:limit]...)
	sort.Strings(out)
	return out
}

func intersectStrings(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	rightSet := make(map[string]struct{}, len(right))
	for _, item := range right {
		rightSet[item] = struct{}{}
	}
	var out []string
	for _, item := range left {
		if _, ok := rightSet[item]; ok {
			out = append(out, item)
		}
	}
	return normalizeCallList(out)
}

func truncateForEvidence(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	return strings.Count(string(content), "\n") + 1
}

func spanLength(startLine, endLine int) int {
	if startLine <= 0 || endLine < startLine {
		return 0
	}
	return endLine - startLine + 1
}

func receiverTypeName(fn *ast.FuncDecl) string {
	if fn == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	return exprToString(fn.Recv.List[0].Type)
}

func goParamMetrics(fn *ast.FuncDecl) (int, int) {
	if fn == nil || fn.Type.Params == nil {
		return 0, 0
	}
	params := 0
	bools := 0
	for _, field := range fn.Type.Params.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		params += count
		if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "bool" {
			bools += count
		}
	}
	return params, bools
}

func goReturnCount(fn *ast.FuncDecl) int {
	if fn == nil || fn.Type.Results == nil {
		return 0
	}
	count := 0
	for _, field := range fn.Type.Results.List {
		if len(field.Names) == 0 {
			count++
			continue
		}
		count += len(field.Names)
	}
	return count
}

func goFuncSignature(fn *ast.FuncDecl) string {
	if fn == nil || fn.Name == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("func ")
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		b.WriteString("(")
		if len(fn.Recv.List[0].Names) > 0 {
			b.WriteString(fn.Recv.List[0].Names[0].Name)
			b.WriteString(" ")
		}
		b.WriteString(exprToString(fn.Recv.List[0].Type))
		b.WriteString(") ")
	}
	b.WriteString(fn.Name.Name)
	b.WriteString("(")
	if fn.Type.Params != nil {
		params := make([]string, 0, len(fn.Type.Params.List))
		for _, field := range fn.Type.Params.List {
			typeText := exprToString(field.Type)
			if len(field.Names) == 0 {
				params = append(params, typeText)
				continue
			}
			for _, name := range field.Names {
				params = append(params, name.Name+" "+typeText)
			}
		}
		b.WriteString(strings.Join(params, ", "))
	}
	b.WriteString(")")
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		results := make([]string, 0, len(fn.Type.Results.List))
		for _, field := range fn.Type.Results.List {
			typeText := exprToString(field.Type)
			if len(field.Names) == 0 {
				results = append(results, typeText)
				continue
			}
			for _, name := range field.Names {
				results = append(results, name.Name+" "+typeText)
			}
		}
		if len(results) == 1 && len(fn.Type.Results.List[0].Names) == 0 {
			b.WriteString(" ")
			b.WriteString(results[0])
		} else {
			b.WriteString(" (")
			b.WriteString(strings.Join(results, ", "))
			b.WriteString(")")
		}
	}
	return b.String()
}

func exprToString(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.StarExpr:
		return "*" + exprToString(node.X)
	case *ast.SelectorExpr:
		return exprToString(node.X) + "." + node.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprToString(node.Elt)
	case *ast.MapType:
		return "map[" + exprToString(node.Key) + "]" + exprToString(node.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + exprToString(node.Elt)
	case *ast.IndexExpr:
		return exprToString(node.X) + "[" + exprToString(node.Index) + "]"
	case *ast.IndexListExpr:
		items := make([]string, 0, len(node.Indices))
		for _, item := range node.Indices {
			items = append(items, exprToString(item))
		}
		return exprToString(node.X) + "[" + strings.Join(items, ", ") + "]"
	case *ast.FuncType:
		return "func"
	default:
		return ""
	}
}

type goOrchestrationMetrics struct {
	Fingerprint   string
	Tokens        []string
	BranchCount   int
	CallSiteCount int
}

func extractGoOrchestrationMetrics(fn *ast.FuncDecl) goOrchestrationMetrics {
	if fn == nil || fn.Body == nil {
		return goOrchestrationMetrics{}
	}
	tokens := make([]string, 0, len(fn.Body.List))
	metrics := goOrchestrationMetrics{}
	for _, stmt := range fn.Body.List {
		token := goStmtFingerprint(stmt, &metrics)
		if token == "" {
			continue
		}
		tokens = append(tokens, token)
	}
	metrics.Fingerprint = strings.Join(tokens, ";")
	metrics.Tokens = tokenizeStructuralFingerprint(metrics.Fingerprint)
	return metrics
}

func tokenizeStructuralFingerprint(fingerprint string) []string {
	if strings.TrimSpace(fingerprint) == "" {
		return nil
	}
	replacer := strings.NewReplacer(
		"(", " ",
		")", " ",
		"{", " ",
		"}", " ",
		";", " ",
		",", " ",
		"|", " ",
		"[", " ",
		"]", " ",
	)
	return strings.Fields(replacer.Replace(fingerprint))
}

func goStmtFingerprint(stmt ast.Stmt, metrics *goOrchestrationMetrics) string {
	switch node := stmt.(type) {
	case *ast.AssignStmt:
		parts := make([]string, 0, len(node.Rhs))
		for _, expr := range node.Rhs {
			parts = append(parts, goExprFingerprint(expr, metrics))
		}
		return "assign(" + strings.Join(parts, ",") + ")"
	case *ast.ExprStmt:
		return goExprFingerprint(node.X, metrics)
	case *ast.ReturnStmt:
		parts := make([]string, 0, len(node.Results))
		for _, expr := range node.Results {
			parts = append(parts, goExprFingerprint(expr, metrics))
		}
		return "return(" + strings.Join(parts, ",") + ")"
	case *ast.IfStmt:
		if metrics != nil {
			metrics.BranchCount++
		}
		body := goBlockFingerprint(node.Body, metrics)
		elseShape := ""
		if node.Else != nil {
			elseShape = "|" + goStmtFingerprint(node.Else, metrics)
		}
		return "if(" + goExprFingerprint(node.Cond, metrics) + "){" + body + "}" + elseShape
	case *ast.ForStmt:
		if metrics != nil {
			metrics.BranchCount++
		}
		return "for{" + goBlockFingerprint(node.Body, metrics) + "}"
	case *ast.RangeStmt:
		if metrics != nil {
			metrics.BranchCount++
		}
		return "range(" + goExprFingerprint(node.X, metrics) + "){" + goBlockFingerprint(node.Body, metrics) + "}"
	case *ast.SwitchStmt:
		if metrics != nil {
			metrics.BranchCount++
		}
		return "switch{" + goCaseBlockFingerprint(node.Body, metrics) + "}"
	case *ast.TypeSwitchStmt:
		if metrics != nil {
			metrics.BranchCount++
		}
		return "typeswitch{" + goCaseBlockFingerprint(node.Body, metrics) + "}"
	case *ast.SelectStmt:
		if metrics != nil {
			metrics.BranchCount++
		}
		return "select{" + goCaseBlockFingerprint(node.Body, metrics) + "}"
	case *ast.DeferStmt:
		return "defer(" + goExprFingerprint(node.Call, metrics) + ")"
	case *ast.GoStmt:
		return "go(" + goExprFingerprint(node.Call, metrics) + ")"
	case *ast.DeclStmt:
		return "decl"
	case *ast.IncDecStmt:
		return "incdec"
	case *ast.BranchStmt:
		return strings.ToLower(node.Tok.String())
	case *ast.BlockStmt:
		return "block{" + goBlockFingerprint(node, metrics) + "}"
	case *ast.LabeledStmt:
		return "label{" + goStmtFingerprint(node.Stmt, metrics) + "}"
	case *ast.SendStmt:
		return "send(" + goExprFingerprint(node.Value, metrics) + ")"
	case *ast.EmptyStmt:
		return ""
	default:
		return fmt.Sprintf("%T", stmt)
	}
}

func goBlockFingerprint(block *ast.BlockStmt, metrics *goOrchestrationMetrics) string {
	if block == nil || len(block.List) == 0 {
		return ""
	}
	parts := make([]string, 0, len(block.List))
	for _, stmt := range block.List {
		token := goStmtFingerprint(stmt, metrics)
		if token == "" {
			continue
		}
		parts = append(parts, token)
	}
	return strings.Join(parts, ";")
}

func goCaseBlockFingerprint(block *ast.BlockStmt, metrics *goOrchestrationMetrics) string {
	if block == nil || len(block.List) == 0 {
		return ""
	}
	parts := make([]string, 0, len(block.List))
	for _, stmt := range block.List {
		switch clause := stmt.(type) {
		case *ast.CaseClause:
			body := make([]string, 0, len(clause.Body))
			for _, bodyStmt := range clause.Body {
				token := goStmtFingerprint(bodyStmt, metrics)
				if token == "" {
					continue
				}
				body = append(body, token)
			}
			parts = append(parts, "case{"+strings.Join(body, ";")+"}")
		case *ast.CommClause:
			body := make([]string, 0, len(clause.Body))
			for _, bodyStmt := range clause.Body {
				token := goStmtFingerprint(bodyStmt, metrics)
				if token == "" {
					continue
				}
				body = append(body, token)
			}
			parts = append(parts, "comm{"+strings.Join(body, ";")+"}")
		}
	}
	return strings.Join(parts, "|")
}

func goExprFingerprint(expr ast.Expr, metrics *goOrchestrationMetrics) string {
	switch node := expr.(type) {
	case *ast.CallExpr:
		if metrics != nil {
			metrics.CallSiteCount++
		}
		return goCallFingerprint(node)
	case *ast.BinaryExpr:
		return "bin(" + strings.ToLower(node.Op.String()) + "," + goExprFingerprint(node.X, metrics) + "," + goExprFingerprint(node.Y, metrics) + ")"
	case *ast.UnaryExpr:
		return "unary(" + strings.ToLower(node.Op.String()) + "," + goExprFingerprint(node.X, metrics) + ")"
	case *ast.SelectorExpr:
		return "selector"
	case *ast.IndexExpr, *ast.IndexListExpr:
		return "index"
	case *ast.CompositeLit:
		return "literal"
	case *ast.ParenExpr:
		return goExprFingerprint(node.X, metrics)
	case *ast.FuncLit:
		return "funclit{" + goBlockFingerprint(node.Body, metrics) + "}"
	case *ast.Ident:
		return "ident"
	case *ast.BasicLit:
		return "lit"
	case *ast.TypeAssertExpr:
		return "typeassert"
	case *ast.SliceExpr:
		return "slice"
	case *ast.KeyValueExpr:
		return "keyvalue"
	case nil:
		return ""
	default:
		return fmt.Sprintf("%T", expr)
	}
}

type semanticSimplificationCandidate struct {
	Kind               string
	PatternIDs         []string
	OriginalForm       string
	SimplifiedForm     string
	OriginalTokenCount int
	SimplifiedTokens   int
}

func analyzeGoSemanticSimplification(fn *ast.FuncDecl, fset *token.FileSet, relPath, name string) []finding {
	candidate := detectGoSemanticSimplification(fn)
	if candidate == nil {
		return nil
	}
	line := 0
	if fset != nil && fn != nil {
		line = fset.Position(fn.Pos()).Line
	}
	return []finding{emitSemanticSimplificationFinding(candidate, relPath, name, line, "go")}
}

func emitSemanticSimplificationFinding(candidate *semanticSimplificationCandidate, relPath, name string, line int, language string) finding {
	simplificationGain := maxInt(1, candidate.OriginalTokenCount-candidate.SimplifiedTokens)
	score := scoreSemanticSimplification(len(candidate.PatternIDs), simplificationGain, candidate.Kind == "boolean_return_wrapper")
	signals := []string{"semantic_simplification"}
	switch language {
	case "go":
		signals = append([]string{"go_ast"}, signals...)
	case "typescript", "javascript", "python", "elixir":
		signals = append([]string{"tree_sitter"}, signals...)
	}
	return finding{
		RuleID:            "semantic_simplification_candidate",
		Category:          "function",
		Severity:          severityFor(score),
		Score:             score,
		Title:             "Function reduces to a much simpler boolean predicate",
		Detail:            fmt.Sprintf("%s has a deterministic boolean simplification path. The current body collapses to `%s`, which means the extra wrapper logic is carrying noise more than behavior.", name, candidate.SimplifiedForm),
		SuggestedRefactor: "Collapse the boolean wrapper or redundant predicate logic into the simpler boolean form, then keep any remaining guard logic only when it adds real behavior.",
		File:              relPath,
		Line:              line,
		Symbol:            name,
		Language:          language,
		Confidence:        "high",
		Signals:           signals,
		Evidence: map[string]any{
			"simplification_kind":  candidate.Kind,
			"pattern_ids":          candidate.PatternIDs,
			"original_form":        skillout.TruncateSingleLine(candidate.OriginalForm, 220),
			"simplified_form":      candidate.SimplifiedForm,
			"original_token_count": candidate.OriginalTokenCount,
			"simplified_tokens":    candidate.SimplifiedTokens,
			"simplification_gain":  simplificationGain,
		},
	}
}

func detectGoSemanticSimplification(fn *ast.FuncDecl) *semanticSimplificationCandidate {
	if fn == nil || fn.Body == nil || len(fn.Body.List) == 0 {
		return nil
	}
	if wrapper := detectGoBoolReturnWrapper(fn.Body.List); wrapper != nil {
		return wrapper
	}
	if len(fn.Body.List) != 1 {
		return nil
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil
	}
	expr, lowerPatterns, lowerChanged := lowerGoSemanticBoolExprDetailed(ret.Results[0])
	if expr == nil {
		return nil
	}
	simplified, patterns, changed := simplifySemanticBoolExpr(expr)
	patterns = appendUniquePatternStrings(lowerPatterns, patterns...)
	if !changed && !lowerChanged {
		return nil
	}
	original := "return " + renderGoNode(ret.Results[0])
	simplifiedText := "return " + renderSemanticBoolExpr(simplified, goSemanticBoolSyntax)
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

func detectGoBoolReturnWrapper(stmts []ast.Stmt) *semanticSimplificationCandidate {
	if len(stmts) == 0 {
		return nil
	}
	var (
		cond      ast.Expr
		invert    bool
		patternID string
	)
	switch len(stmts) {
	case 1:
		ifStmt, ok := stmts[0].(*ast.IfStmt)
		if !ok || ifStmt.Else == nil {
			return nil
		}
		bodyValue, bodyOK := goSingleBlockBooleanReturnValue(ifStmt.Body)
		elseBlock, elseOK := ifStmt.Else.(*ast.BlockStmt)
		elseValue, elseReturnOK := goSingleBlockBooleanReturnValue(elseBlock)
		if !bodyOK || !elseOK || !elseReturnOK || bodyValue == elseValue {
			return nil
		}
		cond = ifStmt.Cond
		invert = !bodyValue && elseValue
		if invert {
			patternID = "inverted_boolean_return_wrapper"
		} else {
			patternID = "boolean_return_wrapper"
		}
	case 2:
		ifStmt, ok := stmts[0].(*ast.IfStmt)
		if !ok || ifStmt.Else != nil {
			return nil
		}
		bodyValue, bodyOK := goSingleBlockBooleanReturnValue(ifStmt.Body)
		tailValue, tailOK := goBooleanReturnValue(stmts[1])
		if !bodyOK || !tailOK || bodyValue == tailValue {
			return nil
		}
		cond = ifStmt.Cond
		invert = !bodyValue && tailValue
		if invert {
			patternID = "inverted_boolean_return_wrapper"
		} else {
			patternID = "boolean_return_wrapper"
		}
	default:
		return nil
	}
	if cond == nil {
		return nil
	}
	simplifiedExpr, lowerPatterns, lowerChanged := lowerGoSemanticBoolExprDetailed(cond)
	if simplifiedExpr == nil {
		return nil
	}
	patterns := appendUniquePatternStrings([]string{patternID}, lowerPatterns...)
	if invert {
		simplifiedExpr = semanticBoolNot(simplifiedExpr)
	}
	if expr, exprPatterns, changed := simplifySemanticBoolExpr(simplifiedExpr); changed {
		simplifiedExpr = expr
		patterns = append(patterns, exprPatterns...)
	} else if !lowerChanged && !invert {
		return nil
	}
	original := renderGoStmtList(stmts)
	simplifiedText := "return " + renderSemanticBoolExpr(simplifiedExpr, goSemanticBoolSyntax)
	if original == simplifiedText {
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

func renderGoStmtList(stmts []ast.Stmt) string {
	if len(stmts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(stmts))
	for _, stmt := range stmts {
		parts = append(parts, renderGoNode(stmt))
	}
	return strings.Join(parts, " ")
}

func renderGoNode(node any) string {
	if node == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, token.NewFileSet(), node); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

func appendUniquePatternStrings(base []string, values ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(values))
	out := make([]string, 0, len(base)+len(values))
	for _, item := range base {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

type duplicateRecoveryCandidate struct {
	Fingerprint      string
	StartLine        int
	Lines            []int
	StatementCount   int
	ControlTransfers int
}

func analyzeDuplicateRecoveryBlocks(fn *ast.FuncDecl, fset *token.FileSet, relPath, name string) []finding {
	groups := findDuplicateRecoveryBlocks(fn, fset)
	if len(groups) == 0 {
		return nil
	}
	findings := make([]finding, 0, len(groups))
	for _, group := range groups {
		score := scoreDuplicateRecoveryBlock(len(group.Lines), group.StatementCount)
		findings = append(findings, finding{
			RuleID:            "duplicate_recovery_block",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function repeats the same guarded recovery block",
			Detail:            fmt.Sprintf("%s repeats a normalized guarded block %d times, which is a strong signal that the recovery or fallback path wants one helper instead of copy-pasted branches.", name, len(group.Lines)),
			SuggestedRefactor: "Extract the repeated guarded recovery/remap path into a local helper or small policy function, then keep each branch focused on the condition that differs.",
			File:              relPath,
			Line:              group.Lines[0],
			Symbol:            name,
			Language:          "go",
			Confidence:        "high",
			Signals:           []string{"go_ast", "normalized_recovery_block"},
			Evidence: map[string]any{
				"normalized_block_hash": hashutil.ShortHash(group.Fingerprint),
				"duplicate_count":       len(group.Lines),
				"duplicate_span_lines":  group.Lines,
				"statement_count":       group.StatementCount,
				"control_transfers":     group.ControlTransfers,
			},
		})
	}
	return findings
}

func findDuplicateRecoveryBlocks(fn *ast.FuncDecl, fset *token.FileSet) []duplicateRecoveryCandidate {
	if fn == nil || fn.Body == nil || fset == nil {
		return nil
	}
	groups := make(map[string]*duplicateRecoveryCandidate)
	linesByFingerprint := make(map[string][]int)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt:
			recordDuplicateRecoveryCandidate(groups, linesByFingerprint, blockRecoveryCandidate(node.Body, fset))
			if block, ok := node.Else.(*ast.BlockStmt); ok {
				recordDuplicateRecoveryCandidate(groups, linesByFingerprint, blockRecoveryCandidate(block, fset))
			}
		}
		return true
	})
	out := make([]duplicateRecoveryCandidate, 0, len(groups))
	for _, candidate := range groups {
		if candidate == nil || len(candidate.Fingerprint) == 0 {
			continue
		}
		if candidate.ControlTransfers <= 0 || candidate.StatementCount < 2 {
			continue
		}
		if lines, ok := linesByFingerprint[candidate.Fingerprint]; ok && len(lines) >= 2 {
			item := *candidate
			item.Lines = append([]int(nil), lines...)
			item.StartLine = item.Lines[0]
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartLine != out[j].StartLine {
			return out[i].StartLine < out[j].StartLine
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	return out
}

func recordDuplicateRecoveryCandidate(groups map[string]*duplicateRecoveryCandidate, linesByFingerprint map[string][]int, candidate *duplicateRecoveryCandidate) {
	if groups == nil || candidate == nil || strings.TrimSpace(candidate.Fingerprint) == "" {
		return
	}
	lines := append(linesByFingerprint[candidate.Fingerprint], candidate.StartLine)
	sort.Ints(lines)
	linesByFingerprint[candidate.Fingerprint] = lines
	if existing, ok := groups[candidate.Fingerprint]; ok {
		existing.ControlTransfers = maxInt(existing.ControlTransfers, candidate.ControlTransfers)
		existing.StatementCount = maxInt(existing.StatementCount, candidate.StatementCount)
		return
	}
	copyCandidate := *candidate
	groups[candidate.Fingerprint] = &copyCandidate
}

func blockRecoveryCandidate(block *ast.BlockStmt, fset *token.FileSet) *duplicateRecoveryCandidate {
	if block == nil || len(block.List) < 2 {
		return nil
	}
	fingerprint := goBlockFingerprint(block, nil)
	if strings.TrimSpace(fingerprint) == "" {
		return nil
	}
	if len(tokenizeStructuralFingerprint(fingerprint)) < 3 {
		return nil
	}
	controlTransfers := blockControlTransfers(block)
	if controlTransfers == 0 {
		return nil
	}
	return &duplicateRecoveryCandidate{
		Fingerprint:      fingerprint,
		StartLine:        fset.Position(block.Pos()).Line,
		StatementCount:   len(block.List),
		ControlTransfers: controlTransfers,
	}
}

func blockControlTransfers(block *ast.BlockStmt) int {
	if block == nil {
		return 0
	}
	count := 0
	ast.Inspect(block, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.ReturnStmt, *ast.BranchStmt:
			count++
		}
		return true
	})
	return count
}

func scoreDuplicateRecoveryBlock(duplicateCount, stmtCount int) int {
	score := 74 + minInt(12, maxInt(0, duplicateCount-2)*6) + minInt(8, maxInt(0, stmtCount-2)*2)
	return clampScore(score)
}

func applyFocus(items []finding, focus string) []finding {
	if focus == "" || focus == "all" || len(items) == 0 {
		return items
	}
	filtered := make([]finding, 0, len(items))
	for _, item := range items {
		if focusAllowsFinding(item, focus) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func focusAllowsFinding(item finding, focus string) bool {
	switch focus {
	case "slop":
		return isSlopFinding(item)
	case "dead":
		return isDeadFinding(item)
	default:
		return true
	}
}

func isSlopFinding(item finding) bool {
	switch item.RuleID {
	case "duplicate_recovery_block", "duplicated_error_remap", "repeated_guard_ladder", "semantic_simplification_candidate", "preload_after_get_chain", "post_transaction_preload", "transaction_script_hotspot", "duplicate_orchestration_fingerprint", "same_file_extraction_candidate":
		return true
	case "function_hotspot":
		return compositeIncludesSlopRule(item)
	default:
		return false
	}
}

func isDeadFinding(item finding) bool {
	switch item.RuleID {
	case "unreachable_private_symbol", "test_only_helper", "stale_export_candidate", "orphan_file", "test_only_file", "stale_package_candidate", "test_only_package":
		return true
	default:
		return false
	}
}

func compositeIncludesSlopRule(item finding) bool {
	if item.Evidence == nil {
		return false
	}
	switch rules := item.Evidence["rules"].(type) {
	case []string:
		for _, ruleID := range rules {
			if isSlopFinding(finding{RuleID: ruleID}) {
				return true
			}
		}
	case []any:
		for _, value := range rules {
			if ruleID, ok := value.(string); ok && isSlopFinding(finding{RuleID: ruleID}) {
				return true
			}
		}
	}
	return false
}

func goCallFingerprint(call *ast.CallExpr) string {
	shape := "call"
	switch call.Fun.(type) {
	case *ast.Ident:
		shape = "call_ident"
	case *ast.SelectorExpr:
		shape = "call_selector"
	case *ast.FuncLit:
		shape = "call_funclit"
	}
	return fmt.Sprintf("%s_%d", shape, minInt(len(call.Args), 3))
}

func calculateGoCyclomaticComplexity(fn *ast.FuncDecl) int {
	if fn == nil || fn.Body == nil {
		return 1
	}
	complexity := 1
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt:
			complexity++
		case *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			if len(node.List) > 0 {
				complexity++
			}
		case *ast.CommClause:
			if node.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if node.Op == token.LAND || node.Op == token.LOR {
				complexity++
			}
		}
		return true
	})
	return complexity
}

func calculateGoNestingDepth(fn *ast.FuncDecl) int {
	if fn == nil || fn.Body == nil {
		return 0
	}
	maxDepth := 0
	var walk func(ast.Node, int)
	walk = func(n ast.Node, depth int) {
		if n == nil {
			return
		}
		if depth > maxDepth {
			maxDepth = depth
		}
		switch node := n.(type) {
		case *ast.BlockStmt:
			for _, stmt := range node.List {
				walk(stmt, depth)
			}
		case *ast.IfStmt:
			walk(node.Cond, depth)
			walk(node.Body, depth+1)
			if node.Else != nil {
				walk(node.Else, depth+1)
			}
		case *ast.ForStmt:
			walk(node.Init, depth)
			walk(node.Cond, depth)
			walk(node.Post, depth)
			walk(node.Body, depth+1)
		case *ast.RangeStmt:
			walk(node.X, depth)
			walk(node.Body, depth+1)
		case *ast.SwitchStmt:
			walk(node.Init, depth)
			walk(node.Tag, depth)
			for _, stmt := range node.Body.List {
				walk(stmt, depth+1)
			}
		case *ast.TypeSwitchStmt:
			walk(node.Init, depth)
			walk(node.Assign, depth)
			for _, stmt := range node.Body.List {
				walk(stmt, depth+1)
			}
		case *ast.SelectStmt:
			for _, stmt := range node.Body.List {
				walk(stmt, depth+1)
			}
		case *ast.CaseClause:
			for _, stmt := range node.Body {
				walk(stmt, depth)
			}
		case *ast.CommClause:
			for _, stmt := range node.Body {
				walk(stmt, depth)
			}
		}
	}
	walk(fn.Body, 0)
	return maxDepth
}
