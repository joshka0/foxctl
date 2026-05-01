package env

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextengine/adapters"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
	ctxengstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

// retrieveLaneInput is the shared input shape for the 5 retrieval composite tools.
type retrieveLaneInput struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit,omitempty"`
	TaskID string `json:"task_id,omitempty"`
}

// gatherContextInput is the input shape for gather_context.
type gatherContextInput struct {
	Query            string   `json:"query"`
	Goal             string   `json:"goal,omitempty"`
	RequiredEvidence []string `json:"required_evidence,omitempty"`
	Limit            int      `json:"limit,omitempty"`
	TaskID           string   `json:"task_id,omitempty"`
	TaskType         string   `json:"task_type,omitempty"`
	SourceProfiles   []string `json:"source_profiles,omitempty"`
	MemoryStatuses   []string `json:"memory_statuses,omitempty"`
	Lanes            []string `json:"lanes,omitempty"`
	MaxContextChars  int      `json:"max_context_chars,omitempty"`
	ResponseMode     string   `json:"response_mode,omitempty"`
}

// loadEvidenceRefInput is the input shape for load_evidence_ref.
type loadEvidenceRefInput struct {
	Ref       string `json:"ref"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

const defaultLoadEvidenceRefMaxTokens = 4096

// laneRetrievalStore is a no-op fallback when the SQLite store is unavailable.
// Episodes are recorded best-effort; a missing store must never break retrieval.
type laneRetrievalStore struct{}

func (laneRetrievalStore) RecordRetrievalEpisode(_ context.Context, ep contextengine.RetrievalEpisode) (contextengine.RetrievalEpisode, error) {
	return ep, nil
}

func (laneRetrievalStore) PutEvidencePack(_ context.Context, pack contextengine.EvidencePack) (contextengine.EvidencePack, error) {
	return pack, nil
}

func (a *ReadOnlyAdapter) laneConfig() contextengine.LaneConfig {
	var store contextengine.RetrievalStore = laneRetrievalStore{}
	if a.ceStore != nil {
		store = a.ceStore
	}
	wsID := ""
	if a.workspaceRoot != "" {
		wsID = ws.ID(a.workspaceRoot)
	}
	return contextengine.LaneConfig{
		Store:       store,
		IDGen:       newIDGen(),
		Clock:       func() time.Time { return time.Now().UTC() },
		WorkspaceID: wsID,
	}
}

func newIDGen() contextengine.IDGen {
	return func() string {
		var buf [12]byte
		_, _ = rand.Read(buf[:])
		return "ce-" + hex.EncodeToString(buf[:])
	}
}

// codeSearchFn returns a CodeSearchFunc backed by the existing semantic_search_code skill.
func (a *ReadOnlyAdapter) codeSearchFn(limit int) contextengine.CodeSearchFunc {
	return a.codeSearchFnForTask(limit, "")
}

func (a *ReadOnlyAdapter) codeSearchFnForTask(limit int, taskType string, sourceProfiles ...contextengine.SourceProfile) contextengine.CodeSearchFunc {
	return a.codeSearchFnForTaskWithRequired(limit, taskType, nil, sourceProfiles...)
}

func (a *ReadOnlyAdapter) codeSearchFnForTaskWithRequired(limit int, taskType string, requiredEvidence []string, sourceProfiles ...contextengine.SourceProfile) contextengine.CodeSearchFunc {
	return func(ctx context.Context, query string) ([]contextengine.CodeSearchHit, error) {
		if strings.TrimSpace(a.workspaceRoot) == "" {
			return nil, nil
		}
		ensembleHits, ensembleErr := a.codeSearchEnsembleHits(ctx, query, taskType, limit, requiredEvidence, sourceProfiles)
		liveHits, liveErr := a.liveOverlayCodeSearchHits(ctx, query, requiredEvidence, limit)
		repoDocHits, repoDocErr := a.repoDocsSearchHits(query, requiredEvidence, sourceProfiles, limit)
		repoHits, repoErr := a.repoIndexCodeSearch(ctx, query, limit)
		localHits, localErr := a.localCodeProbeSearch(query, taskType, requiredEvidence, limit)
		lexicalHits, lexicalErr := a.localLexicalCodeSearch(query, limit)
		buildTargetHits := a.localBuildTargetCodeSearchHits(query)
		wideHits := mergeCodeSearchHits(maxInt(limit*6, limit), repoHits, liveHits, repoDocHits, ensembleHits, localHits, buildTargetHits, lexicalHits)
		relatedHits := a.localRelatedCodeSearchHits(query, wideHits, limit)
		baseHits := mergeCodeSearchHits(limit, repoHits, liveHits, repoDocHits, ensembleHits, localHits, buildTargetHits, relatedHits, lexicalHits)
		closureHits := a.localSubsystemSiblingClosureHits(query, taskType, requiredEvidence, baseHits, limit)
		candidateLimit := codeSearchCandidatePoolLimit(limit, taskType, len(closureHits))
		if hits := mergeCodeSearchHits(candidateLimit, repoHits, liveHits, repoDocHits, ensembleHits, localHits, buildTargetHits, relatedHits, closureHits, lexicalHits); len(hits) > 0 {
			annotateCodeSearchHitCoverage(hits, requiredEvidence)
			return hits, nil
		}
		errs := make([]string, 0, 4)
		if ensembleErr != nil {
			errs = append(errs, "code search ensemble: "+ensembleErr.Error())
		}
		if liveErr != nil {
			errs = append(errs, "live overlay: "+liveErr.Error())
		}
		if repoDocErr != nil {
			errs = append(errs, "repo docs: "+repoDocErr.Error())
		}
		if repoErr != nil {
			errs = append(errs, "repo index: "+repoErr.Error())
		}
		if localErr != nil {
			errs = append(errs, "local probes: "+localErr.Error())
		}
		if lexicalErr != nil {
			errs = append(errs, "local lexical: "+lexicalErr.Error())
		}
		if len(errs) > 0 {
			return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
		}
		return nil, nil
	}
}

func codeSearchCandidatePoolLimit(limit int, taskType string, closureCount int) int {
	if limit <= 0 {
		limit = 8
	}
	if !isSubsystemMapTask(taskType) {
		return limit
	}
	candidateLimit := maxInt(limit+closureCount, limit*2)
	return minInt(candidateLimit, maxInt(limit, 32))
}

func (a *ReadOnlyAdapter) codeSearchEnsembleHits(ctx context.Context, query, taskType string, limit int, requiredEvidence []string, sourceProfiles []contextengine.SourceProfile) ([]rankedCodeSearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	normalizedTaskType, _ := normalizeCodeSearchTaskType(taskType)
	ensembleFiles := maxInt(limit, 4)
	ensembleCandidates := maxInt(limit*4, 16)
	if normalizedTaskType == codeSearchTaskExecutionTrace && len(requiredEvidence) == 0 && ensembleFiles > 4 {
		ensembleFiles = 4
		ensembleCandidates = 8
	}
	args := mustJSON(map[string]any{
		"query":     query,
		"task_type": normalizedTaskType,
		"constraints": map[string]any{
			"require_grounding": true,
		},
		"budget": map[string]any{
			"max_candidates": ensembleCandidates,
			"max_files":      ensembleFiles,
			"max_snippets":   ensembleFiles,
			"max_steps":      4,
		},
	})
	out, err := a.codeSearchEnsemble(ctx, args)
	if err != nil {
		return nil, err
	}

	files := decodeCodeSearchEvidenceFiles(out["files"])
	if len(files) == 0 {
		return nil, nil
	}
	snippetsByPath := codeSearchEvidenceSnippetReasons(out["snippets"])
	symbolsByPath := codeSearchEvidenceSymbolsByPath(out["symbols"])
	directDispatch := codeSearchPathSet(stringSliceValue(out["direct_dispatch_files"]))
	exposure := codeSearchPathSet(stringSliceValue(out["exposure_files"]))
	structural := codeSearchPathSet(stringSliceValue(out["structural_support_files"]))
	registration := codeSearchPathSet(stringSliceValue(out["registration_files"]))

	hits := make([]rankedCodeSearchHit, 0, len(files))
	for idx, file := range files {
		path := normalizeCodeSearchPath(file.Path)
		if path == "" {
			continue
		}
		role := codeSearchEnsembleCandidateRole(path, directDispatch, exposure, structural, registration)
		snippet := strings.TrimSpace(file.Why)
		if extra := strings.TrimSpace(snippetsByPath[path]); extra != "" {
			if snippet != "" {
				snippet += "\n"
			}
			snippet += extra
		}
		if snippet == "" {
			snippet = path
		}
		symbol := symbolsByPath[path]
		isSymbolDefinition := stringSliceHas(file.ConfirmedBy, "symbol_definition")
		if !isSymbolDefinition {
			symbol = codeSearchEvidenceSymbol{}
		}
		if symbol.Symbol != "" && !strings.Contains(snippet, symbol.Symbol) {
			if snippet != "" {
				snippet += "\n"
			}
			snippet += "symbol: " + symbol.Symbol
		}
		score := clampScore(file.SupportScore)
		if score == 0 {
			score = scoreValue(out["confidence"], 0.82)
		}
		if isSymbolDefinition {
			role = "symbol_definition"
		}
		hits = append(hits, rankedCodeSearchHit{
			Priority: 82 - minInt(idx, 20),
			Hit: contextengine.CodeSearchHit{
				Path:     path,
				Snippet:  snippet,
				Line:     symbol.Line,
				Symbol:   symbol.Symbol,
				Score:    score,
				Language: languageFromPath(path),
				Metadata: map[string]any{
					"candidate_role":           role,
					"source":                   "code_search_ensemble",
					"source_profile":           "repo_code",
					"evidence_class":           codeSearchEvidenceClassForRole(role),
					"task_type":                normalizedTaskType,
					"source_profiles":          contextengineSourceProfilesToStrings(sourceProfiles),
					"coverage_terms":           codeSearchCoverageTerms(path, symbol.Symbol, snippet),
					"coverage_requirement_ids": codeSearchCoverageRequirementIDs(path, symbol.Symbol, snippet, requiredEvidence),
					"path_family":              filepath.ToSlash(filepath.Dir(path)),
					"is_test":                  isTestLikeCodeSearchPath(path),
				},
				Sources: append([]string(nil), file.ConfirmedBy...),
			},
		})
	}
	return hits, nil
}

func contextengineSourceProfilesToStrings(profiles []contextengine.SourceProfile) []string {
	out := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if profile.IsValid() {
			out = append(out, string(profile))
		}
	}
	return out
}

func codeSearchCoverageTerms(values ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 12)
	for _, value := range values {
		for _, part := range splitCodeSearchProbe(value) {
			for _, term := range subsystemClosureTermVariants(part) {
				if len(term) < 4 || isGenericCodeSearchPathWord(term) {
					continue
				}
				if _, ok := seen[term]; ok {
					continue
				}
				seen[term] = struct{}{}
				out = append(out, term)
			}
		}
	}
	sort.Strings(out)
	return out
}

func codeSearchCoverageRequirementIDs(pathValue string, symbol string, snippet string, requirements []string) []string {
	if len(requirements) == 0 {
		return nil
	}
	terms := codeSearchCoverageTermSet(pathValue, symbol, snippet)
	out := make([]string, 0, len(requirements))
	for _, req := range requirements {
		reqTerms := codeSearchCoverageTerms(req)
		if codeSearchTermsCoverRequirement(terms, reqTerms) {
			out = appendUniqueStringEnv(out, strings.Join(reqTerms, "_"))
		}
	}
	return out
}

func codeSearchCoverageTermSet(values ...string) map[string]struct{} {
	terms := map[string]struct{}{}
	for _, term := range codeSearchCoverageTerms(values...) {
		terms[term] = struct{}{}
	}
	return terms
}

func codeSearchTermsCoverRequirement(terms map[string]struct{}, reqTerms []string) bool {
	if len(reqTerms) == 0 {
		return false
	}
	covered := 0
	for _, term := range reqTerms {
		if _, ok := terms[term]; ok {
			covered++
		}
	}
	if len(reqTerms) <= 2 {
		return covered == len(reqTerms)
	}
	return covered >= len(reqTerms)-1
}

func annotateCodeSearchHitCoverage(hits []contextengine.CodeSearchHit, requirements []string) {
	for i := range hits {
		hit := &hits[i]
		if hit.Metadata == nil {
			hit.Metadata = map[string]any{}
		}
		coverageTerms := codeSearchCoverageTerms(hit.Path, hit.Symbol, hit.Snippet, strings.Join(metadataStringSliceEnv(hit.Metadata, "matched_terms"), " "))
		if len(coverageTerms) > 0 {
			hit.Metadata["coverage_terms"] = coverageTerms
		}
		if ids := codeSearchCoverageRequirementIDs(hit.Path, hit.Symbol, hit.Snippet, requirements); len(ids) > 0 {
			hit.Metadata["coverage_requirement_ids"] = ids
		}
		if family := filepath.ToSlash(filepath.Dir(hit.Path)); family != "." && family != "" {
			hit.Metadata["path_family"] = family
		}
		hit.Metadata["is_test"] = isTestLikeCodeSearchPath(hit.Path)
	}
}

func metadataStringSliceEnv(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	value := metadata[key]
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = appendUniqueStringEnv(out, fmt.Sprint(item))
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	default:
		return nil
	}
}

func decodeCodeSearchEvidenceFiles(raw any) []codeSearchEvidenceFile {
	switch value := raw.(type) {
	case []codeSearchEvidenceFile:
		return append([]codeSearchEvidenceFile(nil), value...)
	case []any:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var files []codeSearchEvidenceFile
		if err := json.Unmarshal(encoded, &files); err != nil {
			return nil
		}
		return files
	default:
		return nil
	}
}

func decodeCodeSearchEvidenceSymbols(raw any) []codeSearchEvidenceSymbol {
	switch value := raw.(type) {
	case []codeSearchEvidenceSymbol:
		return append([]codeSearchEvidenceSymbol(nil), value...)
	case []any:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var symbols []codeSearchEvidenceSymbol
		if err := json.Unmarshal(encoded, &symbols); err != nil {
			return nil
		}
		return symbols
	default:
		return nil
	}
}

func codeSearchEvidenceSymbolsByPath(raw any) map[string]codeSearchEvidenceSymbol {
	out := map[string]codeSearchEvidenceSymbol{}
	for _, symbol := range decodeCodeSearchEvidenceSymbols(raw) {
		path := normalizeCodeSearchPath(symbol.Path)
		if path == "" {
			continue
		}
		current := out[path]
		if current.Symbol == "" || (current.Line == 0 && symbol.Line > 0) {
			out[path] = symbol
		}
	}
	return out
}

func codeSearchEvidenceClassForRole(role string) string {
	switch role {
	case "symbol_definition":
		return "symbol_definition"
	case "registration_file":
		return "registration"
	case "direct_dispatch_file":
		return "dispatch"
	case "structural_support":
		return "structural_support"
	default:
		return "structural"
	}
}

func stringSliceHas(items []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func codeSearchEvidenceSnippetReasons(raw any) map[string]string {
	out := map[string]string{}
	switch value := raw.(type) {
	case []codeSearchEvidenceSnippet:
		for _, snippet := range value {
			path := normalizeCodeSearchPath(snippet.Path)
			if path == "" {
				continue
			}
			out[path] = codeSearchEvidenceSnippetReason(snippet)
		}
	case []any:
		encoded, err := json.Marshal(value)
		if err != nil {
			return out
		}
		var snippets []codeSearchEvidenceSnippet
		if err := json.Unmarshal(encoded, &snippets); err != nil {
			return out
		}
		for _, snippet := range snippets {
			path := normalizeCodeSearchPath(snippet.Path)
			if path == "" {
				continue
			}
			out[path] = codeSearchEvidenceSnippetReason(snippet)
		}
	}
	return out
}

func codeSearchEvidenceSnippetReason(snippet codeSearchEvidenceSnippet) string {
	parts := make([]string, 0, 2)
	if snippet.StartLine > 0 {
		if snippet.EndLine > 0 && snippet.EndLine != snippet.StartLine {
			parts = append(parts, fmt.Sprintf("lines: %d-%d", snippet.StartLine, snippet.EndLine))
		} else {
			parts = append(parts, fmt.Sprintf("line: %d", snippet.StartLine))
		}
	}
	if reason := strings.TrimSpace(snippet.Reason); reason != "" {
		parts = append(parts, "reason: "+reason)
	}
	return strings.Join(parts, "\n")
}

func codeSearchPathSet(paths []string) map[string]struct{} {
	out := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = normalizeCodeSearchPath(path)
		if path == "" {
			continue
		}
		out[path] = struct{}{}
	}
	return out
}

func codeSearchEnsembleCandidateRole(path string, directDispatch, exposure, structural, registration map[string]struct{}) string {
	if _, ok := directDispatch[path]; ok {
		return "direct_dispatch_file"
	}
	if _, ok := registration[path]; ok {
		return "registration_file"
	}
	if _, ok := structural[path]; ok {
		return "structural_support"
	}
	if _, ok := exposure[path]; ok {
		return "primary_anchor"
	}
	return "primary_anchor"
}

type rankedCodeSearchHit struct {
	Hit      contextengine.CodeSearchHit
	Priority int
}

type localPathProbeHit struct {
	Path  string
	Probe string
}

type localExactProbeHit struct {
	Path  string
	Probe string
	Line  int
}

type localLexicalProbeHit struct {
	Path         string
	Line         int
	Score        float64
	Snippet      string
	MatchedTerms []string
}

func (a *ReadOnlyAdapter) repoIndexCodeSearch(ctx context.Context, query string, limit int) ([]contextengine.CodeSearchHit, error) {
	if strings.TrimSpace(a.cfg.Storage.Root) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	store, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	output, err := repoquery.NewQueryService(repoindex.NewQueryEngine(store)).SearchWithProjection(ctx, repoquery.SearchRequest{
		Query: strings.TrimSpace(query),
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	hits := make([]contextengine.CodeSearchHit, 0, len(output.Anchors))
	for _, anchor := range output.Anchors {
		path := strings.TrimSpace(anchor.Path)
		if path == "" {
			continue
		}
		hits = append(hits, contextengine.CodeSearchHit{
			Path:     path,
			Snippet:  a.repoAnchorStatement(anchor),
			Line:     anchor.LineHint,
			Score:    repoAnchorScore(anchor.Score),
			Language: languageFromPath(path),
		})
	}
	return hits, nil
}

func (a *ReadOnlyAdapter) semanticCodeSearchHits(ctx context.Context, query string, limit int) ([]rankedCodeSearchHit, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	args := mustJSON(map[string]any{"query": query, "limit": maxInt(limit*2, 12)})
	out, err := a.semanticSearchCode(ctx, args)
	if err != nil {
		return nil, err
	}
	results, _ := out["results"].([]map[string]any)
	hits := make([]rankedCodeSearchHit, 0, len(results))
	for _, r := range results {
		hit := contextengine.CodeSearchHit{
			Path:     normalizeCodeSearchPath(stringValue(r["path"])),
			Snippet:  stringValue(r["summary"]),
			Symbol:   stringValue(r["name"]),
			Language: languageFromPath(stringValue(r["path"])),
		}
		if sim, ok := r["similarity"].(float64); ok {
			hit.Score = sim
		}
		if hit.Path == "" && hit.Symbol == "" {
			continue
		}
		hits = append(hits, rankedCodeSearchHit{Hit: hit, Priority: 24})
	}
	for _, bundle := range decodeSemanticCandidateBundles(out["candidate_bundles"]) {
		if bundle.PrimaryPath != "" {
			hits = append(hits, rankedCodeSearchHit{
				Priority: 23,
				Hit: contextengine.CodeSearchHit{
					Path:     bundle.PrimaryPath,
					Snippet:  semanticBundleSnippet(bundle),
					Score:    bundle.Score,
					Language: languageFromPath(bundle.PrimaryPath),
				},
			})
		}
		for _, path := range bundle.RelatedPaths {
			path = normalizeCodeSearchPath(path)
			if path == "" {
				continue
			}
			hits = append(hits, rankedCodeSearchHit{
				Priority: 21,
				Hit: contextengine.CodeSearchHit{
					Path:     path,
					Snippet:  semanticBundleSnippet(bundle),
					Score:    bundle.Score * 0.95,
					Language: languageFromPath(path),
				},
			})
		}
	}
	return hits, nil
}

type semanticCandidateBundleHit struct {
	PrimaryPath  string
	RelatedPaths []string
	Symbols      []string
	Score        float64
	MatchReason  string
}

func decodeSemanticCandidateBundles(raw any) []semanticCandidateBundleHit {
	items, ok := raw.([]map[string]any)
	if !ok {
		return nil
	}
	out := make([]semanticCandidateBundleHit, 0, len(items))
	for _, item := range items {
		out = append(out, semanticCandidateBundleHit{
			PrimaryPath:  normalizeCodeSearchPath(stringValue(item["primary_path"])),
			RelatedPaths: decodeStringSliceAny(item["related_paths"]),
			Symbols:      decodeStringSliceAny(item["symbols"]),
			Score:        floatValue(item["score"]),
			MatchReason:  stringValue(item["match_reason"]),
		})
	}
	return out
}

func decodeStringSliceAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func semanticBundleSnippet(bundle semanticCandidateBundleHit) string {
	parts := make([]string, 0, 2)
	if bundle.MatchReason != "" {
		parts = append(parts, "semantic bundle: "+bundle.MatchReason)
	}
	if len(bundle.Symbols) > 0 {
		parts = append(parts, "symbols: "+strings.Join(bundle.Symbols, ", "))
	}
	return strings.Join(parts, "\n")
}

func (a *ReadOnlyAdapter) localCodeProbeSearch(query string, taskType string, requiredEvidence []string, limit int) ([]rankedCodeSearchHit, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	normalizedTaskType, _ := normalizeCodeSearchTaskType(taskType)
	exactProbes := codeSearchTaskExactProbes(query, normalizedTaskType)
	pathProbes := codeSearchTaskPathProbes(query, normalizedTaskType, exactProbes)
	probeLimit := minInt(maxInt(limit*8, 32), 96)
	pathHits, exactHits, scanErr := workspaceCodeProbeSearch(a.workspaceRoot, pathProbes, exactProbes, probeLimit)
	candidates := map[string]*codeSearchCandidate{}
	snippets := map[string]string{}
	lineHints := map[string]int{}
	symbols := map[string]string{}
	priorities := map[string]int{}
	matchedTerms := map[string][]string{}
	var errs []string
	if scanErr != nil {
		errs = append(errs, scanErr.Error())
	}
	remember := func(pathValue string, priority int, line int, symbol string, snippet string, terms ...string) {
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" {
			return
		}
		for _, term := range terms {
			if term = strings.TrimSpace(term); term != "" {
				matchedTerms[pathValue] = appendUniqueStringEnv(matchedTerms[pathValue], term)
			}
		}
		prevPriority := priorities[pathValue]
		if prevPriority < priority {
			priorities[pathValue] = priority
		}
		if lineHints[pathValue] == 0 && line > 0 {
			lineHints[pathValue] = line
		}
		if symbols[pathValue] == "" && strings.TrimSpace(symbol) != "" {
			symbols[pathValue] = strings.TrimSpace(symbol)
		}
		if strings.TrimSpace(snippet) != "" {
			snippet = strings.TrimSpace(snippet)
			switch {
			case snippets[pathValue] == "":
				snippets[pathValue] = snippet
			case priority > prevPriority:
				snippets[pathValue] = snippet + "\n" + snippets[pathValue]
			case priority == prevPriority && !strings.Contains(snippets[pathValue], snippet):
				snippets[pathValue] = snippets[pathValue] + "\n" + snippet
			}
		}
	}
	for _, hit := range pathHits {
		if hit.Path == "" {
			continue
		}
		addCodeSearchCandidate(candidates, hit.Path, "path probe: "+hit.Probe, "path_probe", 0.92, 0, "", "")
		remember(hit.Path, 20, 0, "", "path probe: "+hit.Probe, hit.Probe)
	}
	for _, hit := range exactHits {
		if hit.Path == "" {
			continue
		}
		support := codeSearchFileWeight(hit.Path) + lexicalCodeTermWeight(hit.Probe)
		addCodeSearchCandidate(candidates, hit.Path, "exact code probe: "+hit.Probe, "exact_probe", support, hit.Line, "", "")
		remember(hit.Path, 30, hit.Line, "", codeProbeSnippet("exact code probe: "+hit.Probe, a.repoFileExcerpt(hit.Path, hit.Line, 3, 5)), hit.Probe)
	}
	if normalizedTaskType == codeSearchTaskSymbolInspect {
		definitions := a.findGoDefinitions(codeSearchSymbolProbes(query))
		for _, symbol := range sortedStringKeys(definitions) {
			definition := definitions[symbol]
			if definition.Path == "" {
				continue
			}
			addCodeSearchCandidate(candidates, definition.Path, "symbol definition: "+symbol, "symbol_definition", 2.8, definition.Line, symbol, "")
			remember(definition.Path, 95, definition.Line, symbol, codeProbeSnippet("symbol definition: "+symbol, a.repoFileExcerpt(definition.Path, definition.Line, 3, 5)), symbol)
		}
	}
	if normalizedTaskType == codeSearchTaskRegistrationTrace {
		registrationHits, registrationGaps := codeSearchRegistrationAugment(a.workspaceRoot, candidates, limit)
		errs = append(errs, registrationGaps...)
		for _, hit := range registrationHits {
			if hit.Path == "" {
				continue
			}
			addCodeSearchCandidate(candidates, hit.Path, "registration site for "+hit.Symbol, "registration_trace", 1.25, hit.Line, hit.Symbol, "")
			remember(hit.Path, 40, hit.Line, hit.Symbol, codeProbeSnippet("registration site for "+hit.Symbol, a.repoFileExcerpt(hit.Path, hit.Line, 3, 5)), hit.Symbol)
		}
	}
	ranked := rankCodeSearchCandidatesWithProbes(candidates, query, normalizedTaskType, maxInt(limit*3, limit), pathProbes, exactProbes)
	out := make([]rankedCodeSearchHit, 0, len(ranked))
	for i, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		score := 1 - (float64(i) * 0.01)
		if score < 0.5 {
			score = 0.5
		}
		line := firstCodeSearchLine(candidate.LineHints)
		if line == 0 {
			line = lineHints[candidate.Path]
		}
		symbol := firstCodeSearchSymbol(candidate.Symbols)
		if symbol == "" {
			symbol = symbols[candidate.Path]
		}
		snippet := snippets[candidate.Path]
		if snippet == "" {
			snippet = candidate.Why
		}
		if terms := matchedTerms[candidate.Path]; len(terms) > 0 {
			snippet = appendCodeSearchMatchedTerms(snippet, terms)
		}
		metadata := localCodeProbeMetadata(candidate, normalizedTaskType)
		if terms := matchedTerms[candidate.Path]; len(terms) > 0 {
			metadata["matched_terms"] = append([]string(nil), terms...)
			metadata["coverage_terms"] = codeSearchCoverageTerms(strings.Join(terms, " "))
			if ids := codeSearchCoverageRequirementIDs(candidate.Path, symbol, strings.Join(terms, "\n"), requiredEvidence); len(ids) > 0 {
				metadata["coverage_requirement_ids"] = ids
			}
		}
		out = append(out, rankedCodeSearchHit{
			Priority: priorities[candidate.Path],
			Hit: contextengine.CodeSearchHit{
				Path:     candidate.Path,
				Snippet:  snippet,
				Line:     line,
				Symbol:   symbol,
				Score:    score,
				Language: languageFromPath(candidate.Path),
				Metadata: metadata,
			},
		})
	}
	if len(out) > 0 || len(errs) == 0 {
		return out, nil
	}
	return nil, fmt.Errorf("%s", strings.Join(uniqueStrings(errs), "; "))
}

func localCodeProbeMetadata(candidate *codeSearchCandidate, taskType string) map[string]any {
	role := ""
	evidenceClass := "structural"
	switch {
	case candidateHasSource(candidate, "symbol_definition"):
		role = "symbol_definition"
		evidenceClass = "symbol_definition"
	case candidateHasSource(candidate, "registration_trace"):
		role = "registration_file"
		evidenceClass = "registration"
	case candidateHasSource(candidate, "exact_probe"):
		evidenceClass = "direct"
	case candidateHasSource(candidate, "path_probe"):
		evidenceClass = "path"
	}
	metadata := map[string]any{
		"source":         "local_code_probe",
		"source_profile": "repo_code",
		"evidence_class": evidenceClass,
		"task_type":      taskType,
		"sources":        sortedSourceKeys(candidate.Sources),
	}
	if role != "" {
		metadata["candidate_role"] = role
	}
	return metadata
}

func appendCodeSearchMatchedTerms(snippet string, terms []string) string {
	terms = cleanStringList(terms)
	if len(terms) == 0 {
		return strings.TrimSpace(snippet)
	}
	line := "matched terms: " + strings.Join(terms, ", ")
	snippet = strings.TrimSpace(snippet)
	if snippet == "" {
		return line
	}
	if strings.Contains(snippet, line) {
		return snippet
	}
	return snippet + "\n" + line
}

func (a *ReadOnlyAdapter) localLexicalCodeSearch(query string, limit int) ([]rankedCodeSearchHit, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	terms := codeSearchLexicalTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	hits, err := workspaceLexicalCodeSearch(a.workspaceRoot, terms, minInt(maxInt(limit*3, 16), 48))
	if err != nil {
		return nil, err
	}
	out := make([]rankedCodeSearchHit, 0, len(hits))
	for _, hit := range hits {
		statement := strings.TrimSpace(hit.Snippet)
		if statement == "" {
			statement = "lexical code evidence"
		}
		if len(hit.MatchedTerms) > 0 {
			statement = "lexical code evidence: " + strings.Join(hit.MatchedTerms, ", ") + "\nexcerpt: " + statement
		}
		out = append(out, rankedCodeSearchHit{
			Priority: 27,
			Hit: contextengine.CodeSearchHit{
				Path:     hit.Path,
				Snippet:  statement,
				Line:     hit.Line,
				Score:    hit.Score,
				Language: languageFromPath(hit.Path),
			},
		})
	}
	return out, nil
}

func (a *ReadOnlyAdapter) localCoverageCodeSearchHits(query string, requiredEvidence []string, limit int) ([]rankedCodeSearchHit, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" || len(cleanStringList(requiredEvidence)) == 0 {
		return nil, nil
	}
	return a.localCoverageFileSearchHits(query, requiredEvidence, nil, limit, false)
}

func (a *ReadOnlyAdapter) repoDocsSearchHits(query string, requiredEvidence []string, sourceProfiles []contextengine.SourceProfile, limit int) ([]rankedCodeSearchHit, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" || !sourceProfileListHas(sourceProfiles, contextengine.SourceProfileRepoDocs) {
		return nil, nil
	}
	return a.localCoverageFileSearchHits(query, requiredEvidence, sourceProfiles, limit, true)
}

func sourceProfileListHas(profiles []contextengine.SourceProfile, want contextengine.SourceProfile) bool {
	for _, profile := range profiles {
		if profile == want {
			return true
		}
	}
	return false
}

type coverageFileCandidate struct {
	path        string
	line        int
	score       float64
	excerpt     string
	matched     []string
	coverageIDs []string
}

func (a *ReadOnlyAdapter) localCoverageFileSearchHits(query string, requiredEvidence []string, sourceProfiles []contextengine.SourceProfile, limit int, docsOnly bool) ([]rankedCodeSearchHit, error) {
	if limit <= 0 {
		limit = 8
	}
	requirements := cleanStringList(requiredEvidence)
	terms := codeSearchCoverageTerms(append([]string{query}, requirements...)...)
	if len(terms) == 0 {
		return nil, nil
	}
	candidates := map[string]*coverageFileCandidate{}
	err := filepath.WalkDir(a.workspaceRoot, func(current string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipLocalCodeSearchDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(a.workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		rel = normalizeCodeSearchPath(filepath.ToSlash(rel))
		if rel == "" || !isLikelyTextCodeFile(rel) {
			return nil
		}
		isDoc := isLikelyDocumentationPath(rel)
		if docsOnly && !isDoc {
			return nil
		}
		if !docsOnly && isDoc {
			return nil
		}
		info, statErr := d.Info()
		if statErr == nil && info.Size() > 1_000_000 {
			return nil
		}
		if !docsOnly {
			pathScore := codeSearchPathTermScore(rel, terms)
			coverageIDs := codeSearchCoverageRequirementIDs(rel, "", "", requirements)
			if pathScore == 0 && len(coverageIDs) == 0 {
				return nil
			}
			matched := matchingTermsInText(rel, terms)
			candidates[rel] = &coverageFileCandidate{
				path:        rel,
				line:        1,
				score:       clampScore(0.60 + minFloat(0.25, pathScore*0.22) + minFloat(0.18, float64(len(coverageIDs))*0.06)),
				excerpt:     a.repoFileExcerpt(rel, 1, 0, 5),
				matched:     matched,
				coverageIDs: append([]string(nil), coverageIDs...),
			}
			return nil
		}
		body, readErr := os.ReadFile(current)
		if readErr != nil {
			return nil
		}
		text := string(body)
		line, matched := firstMatchingLine(text, terms)
		pathScore := codeSearchPathTermScore(rel, terms)
		coverageIDs := codeSearchCoverageRequirementIDs(rel, "", text, requirements)
		if pathScore == 0 && len(matched) == 0 && len(coverageIDs) == 0 {
			return nil
		}
		if line == 0 {
			line = 1
		}
		score := 0.58 + minFloat(0.20, pathScore*0.18) + minFloat(0.12, float64(len(matched))*0.03)
		if len(coverageIDs) > 0 {
			score += minFloat(0.18, float64(len(coverageIDs))*0.06)
		}
		if docsOnly {
			score += 0.12
			switch {
			case isRepoDocsRootMapPath(rel):
				score += 0.55
			case isRepoDocsMapPath(rel):
				score += 0.18
			}
		}
		candidates[rel] = &coverageFileCandidate{
			path:        rel,
			line:        line,
			score:       clampScore(score),
			excerpt:     a.repoFileExcerpt(rel, line, 2, 5),
			matched:     append([]string(nil), matched...),
			coverageIDs: append([]string(nil), coverageIDs...),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	items := make([]*coverageFileCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, candidate)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if len(items[i].coverageIDs) != len(items[j].coverageIDs) {
			return len(items[i].coverageIDs) > len(items[j].coverageIDs)
		}
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].path < items[j].path
	})
	maxHits := maxInt(limit*2, limit)
	if docsOnly {
		maxHits = maxInt(limit, 8)
	} else {
		items = selectCoverageFanoutItems(items, maxInt(1, minInt(len(requirements), maxInt(2, limit/2))))
	}
	if len(items) > maxHits {
		items = items[:maxHits]
	}
	out := make([]rankedCodeSearchHit, 0, len(items))
	for _, candidate := range items {
		role := "primary_anchor"
		if isTestLikeCodeSearchPath(candidate.path) {
			role = "test_support"
		}
		source := "coverage_path_fanout"
		profile := contextengine.SourceProfileRepoCode
		priority := 26
		if docsOnly {
			role = "documentation_anchor"
			if isRepoDocsRootMapPath(candidate.path) || isRepoDocsMapPath(candidate.path) {
				role = "documentation_map"
			}
			source = "repo_docs_fanout"
			profile = contextengine.SourceProfileRepoDocs
			priority = 86
		}
		reason := source
		if len(candidate.matched) > 0 {
			reason += ": " + strings.Join(candidate.matched, ", ")
		}
		out = append(out, rankedCodeSearchHit{
			Priority: priority,
			Hit: contextengine.CodeSearchHit{
				Path:     candidate.path,
				Snippet:  codeProbeSnippet(reason, candidate.excerpt),
				Line:     candidate.line,
				Score:    candidate.score,
				Language: languageFromPath(candidate.path),
				Metadata: map[string]any{
					"candidate_role":           role,
					"source":                   source,
					"source_profile":           string(profile),
					"source_profiles":          contextengineSourceProfilesToStrings(sourceProfiles),
					"evidence_class":           "coverage",
					"coverage_terms":           codeSearchCoverageTerms(candidate.path, strings.Join(candidate.matched, " "), candidate.excerpt),
					"coverage_requirement_ids": candidate.coverageIDs,
					"matched_terms":            candidate.matched,
					"path_family":              filepath.ToSlash(filepath.Dir(candidate.path)),
					"is_test":                  isTestLikeCodeSearchPath(candidate.path),
				},
				Sources: []string{source},
			},
		})
	}
	return out, nil
}

func selectCoverageFanoutItems(items []*coverageFileCandidate, maxHits int) []*coverageFileCandidate {
	if maxHits <= 0 || len(items) <= maxHits {
		return items
	}
	selected := make([]*coverageFileCandidate, 0, maxHits)
	seenPath := map[string]struct{}{}
	seenCoverage := map[string]struct{}{}
	add := func(item *coverageFileCandidate) bool {
		if _, ok := seenPath[item.path]; ok {
			return false
		}
		seenPath[item.path] = struct{}{}
		selected = append(selected, item)
		return len(selected) >= maxHits
	}
	for _, item := range items {
		addedForCoverage := false
		for _, id := range item.coverageIDs {
			if _, ok := seenCoverage[id]; ok {
				continue
			}
			seenCoverage[id] = struct{}{}
			addedForCoverage = true
		}
		if addedForCoverage && add(item) {
			return selected
		}
	}
	for _, item := range items {
		if add(item) {
			return selected
		}
	}
	return selected
}

func isRepoDocsMapPath(pathValue string) bool {
	pathValue = strings.ToLower(filepath.ToSlash(strings.TrimSpace(pathValue)))
	if pathValue == "" {
		return false
	}
	base := filepath.Base(pathValue)
	if base != "readme.md" {
		return false
	}
	dir := strings.Trim(filepath.Dir(pathValue), "/.")
	return dir == "docs" || strings.HasPrefix(dir, "docs/")
}

func isRepoDocsRootMapPath(pathValue string) bool {
	pathValue = strings.ToLower(filepath.ToSlash(strings.TrimSpace(pathValue)))
	return pathValue == "docs/readme.md"
}

func matchingTermsInText(value string, terms []string) []string {
	value = strings.ToLower(value)
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if strings.Contains(value, term) {
			out = appendUniqueStringEnv(out, term)
		}
	}
	return out
}

func (a *ReadOnlyAdapter) liveOverlayCodeSearchHits(ctx context.Context, query string, requiredEvidence []string, limit int) ([]rankedCodeSearchHit, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	paths, err := gitWorkingTreeOverlayPaths(ctx, a.workspaceRoot)
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	terms := codeSearchLiveOverlayTerms(query, requiredEvidence)
	if len(terms) == 0 && len(requiredEvidence) == 0 {
		return nil, nil
	}
	out := make([]rankedCodeSearchHit, 0, minInt(len(paths), maxInt(limit, 8)))
	for _, item := range paths {
		if item.path == "" || item.status == "deleted" || !isLikelyTextCodeFile(item.path) {
			continue
		}
		abs := filepath.Join(a.workspaceRoot, filepath.FromSlash(item.path))
		info, statErr := os.Stat(abs)
		if statErr != nil || info.IsDir() || info.Size() > 1_000_000 {
			continue
		}
		body, readErr := os.ReadFile(abs)
		if readErr != nil {
			continue
		}
		text := string(body)
		line, matched := firstMatchingLine(text, terms)
		pathScore := codeSearchPathTermScore(item.path, terms)
		coverageIDs := codeSearchCoverageRequirementIDs(item.path, "", text, requiredEvidence)
		if pathScore == 0 && len(matched) == 0 && len(coverageIDs) == 0 {
			continue
		}
		if line == 0 {
			line = 1
		}
		excerpt := a.repoFileExcerpt(item.path, line, 2, 4)
		if excerpt == "" {
			excerpt = strings.TrimSpace(firstNRunes(text, 400))
		}
		role := "working_tree"
		if isTestLikeCodeSearchPath(item.path) {
			role = "test_support"
		}
		score := 0.72 + minFloat(0.22, pathScore*0.2)
		if len(coverageIDs) > 0 {
			score += 0.08
		}
		out = append(out, rankedCodeSearchHit{
			Priority: 76,
			Hit: contextengine.CodeSearchHit{
				Path:     item.path,
				Snippet:  codeProbeSnippet("live working-tree overlay: "+item.status, excerpt),
				Line:     line,
				Score:    clampScore(score),
				Language: languageFromPath(item.path),
				Metadata: map[string]any{
					"candidate_role":           role,
					"source":                   "live_overlay",
					"source_profile":           "repo_code",
					"evidence_class":           "working_tree",
					"freshness":                "working_tree",
					"git_status":               item.status,
					"coverage_terms":           codeSearchCoverageTerms(item.path, strings.Join(matched, " "), excerpt),
					"coverage_requirement_ids": coverageIDs,
					"matched_terms":            matched,
					"path_family":              filepath.ToSlash(filepath.Dir(item.path)),
					"is_test":                  isTestLikeCodeSearchPath(item.path),
				},
				Sources: []string{"live_overlay"},
			},
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Hit.Score != out[j].Hit.Score {
			return out[i].Hit.Score > out[j].Hit.Score
		}
		return out[i].Hit.Path < out[j].Hit.Path
	})
	if len(out) > maxInt(limit*2, limit) {
		out = out[:maxInt(limit*2, limit)]
	}
	return out, nil
}

type liveOverlayPath struct {
	path   string
	status string
}

func gitWorkingTreeOverlayPaths(ctx context.Context, workspaceRoot string) ([]liveOverlayPath, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", workspaceRoot, "status", "--porcelain", "--untracked-files=all")
	output, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	lines := strings.Split(string(output), "\n")
	out := make([]liveOverlayPath, 0, len(lines))
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		code := strings.TrimSpace(line[:2])
		pathValue := strings.TrimSpace(line[3:])
		if idx := strings.Index(pathValue, " -> "); idx >= 0 {
			pathValue = strings.TrimSpace(pathValue[idx+4:])
		}
		pathValue = strings.Trim(pathValue, `"`)
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" {
			continue
		}
		status := "modified"
		switch {
		case strings.Contains(code, "?"):
			status = "untracked"
		case strings.Contains(code, "A"):
			status = "added"
		case strings.Contains(code, "D"):
			status = "deleted"
		case strings.Contains(code, "R"):
			status = "renamed"
		}
		out = append(out, liveOverlayPath{path: pathValue, status: status})
	}
	return out, nil
}

func codeSearchLiveOverlayTerms(query string, requiredEvidence []string) []string {
	values := []string{query}
	values = append(values, requiredEvidence...)
	seen := map[string]struct{}{}
	out := make([]string, 0, 24)
	for _, term := range codeSearchCoverageTerms(values...) {
		if len(term) < 4 || isGenericCodeSearchPathWord(term) {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

func firstMatchingLine(text string, terms []string) (int, []string) {
	if len(terms) == 0 {
		return 0, nil
	}
	lines := strings.Split(text, "\n")
	for idx, line := range lines {
		lower := strings.ToLower(line)
		matched := make([]string, 0, 4)
		for _, term := range terms {
			if strings.Contains(lower, strings.ToLower(term)) {
				matched = appendUniqueStringEnv(matched, term)
			}
		}
		if len(matched) > 0 {
			return idx + 1, matched
		}
	}
	return 0, nil
}

func firstNRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

func codeSearchLexicalTerms(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 16)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) < 3 || isGenericCodeSearchPathWord(value) {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	tokens := splitCodeSearchProbe(query)
	for _, token := range tokens {
		add(token)
	}
	for width := 2; width <= 3; width++ {
		for i := 0; i+width <= len(tokens); i++ {
			parts := make([]string, 0, width)
			for _, token := range tokens[i : i+width] {
				if len(token) < 3 || isGenericCodeSearchPathWord(token) {
					continue
				}
				parts = append(parts, token)
			}
			if len(parts) < 2 {
				continue
			}
			add(strings.Join(parts, "_"))
		}
	}
	return out
}

func workspaceLexicalCodeSearch(workspaceRoot string, terms []string, limit int) ([]localLexicalProbeHit, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" || len(terms) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 16
	}
	hits := make([]localLexicalProbeHit, 0)
	err := filepath.WalkDir(workspaceRoot, func(current string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipLocalCodeSearchDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !isLikelyTextCodeFile(rel) {
			return nil
		}
		info, statErr := d.Info()
		if statErr == nil && info.Size() > 1_000_000 {
			return nil
		}
		body, readErr := os.ReadFile(current)
		if readErr != nil {
			return nil
		}
		text := string(body)
		lowerText := strings.ToLower(text)
		lowerPath := strings.ToLower(rel)
		score := codeSearchFileWeight(rel) * 0.15
		matched := make([]string, 0, len(terms))
		for _, term := range terms {
			if term == "" {
				continue
			}
			termWeight := lexicalCodeTermWeight(term)
			pathMatched := strings.Contains(lowerPath, term)
			count := strings.Count(lowerText, term)
			if !pathMatched && count == 0 {
				continue
			}
			matched = append(matched, term)
			if pathMatched {
				score += 3.0 * termWeight
				if strings.Contains(strings.ToLower(filepath.Base(rel)), term) {
					score += 1.5 * termWeight
				}
			}
			if count > 0 {
				score += float64(minInt(count, 3)) * 0.8 * termWeight
			}
		}
		minMatches := 2
		if len(terms) >= 8 {
			minMatches = 3
		}
		if len(matched) < minMatches {
			return nil
		}
		score += float64(len(matched)) * 0.6
		line, lineScore := bestLexicalMatchLine(text, terms)
		score += lineScore * 0.8
		if line <= 0 {
			line = 1
		}
		hits = append(hits, localLexicalProbeHit{
			Path:         rel,
			Line:         line,
			Score:        normalizeLexicalCodeScore(score),
			Snippet:      sliceLines(text, maxInt(1, line-4), line+8),
			MatchedTerms: matched,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		wi := codeSearchFileWeight(hits[i].Path)
		wj := codeSearchFileWeight(hits[j].Path)
		if wi != wj {
			return wi > wj
		}
		return hits[i].Path < hits[j].Path
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func lexicalCodeTermWeight(term string) float64 {
	term = strings.TrimSpace(term)
	switch {
	case len(term) >= 14:
		return 2.0
	case len(term) >= 10:
		return 1.6
	case strings.Contains(term, "_"):
		return 1.4
	case len(term) >= 7:
		return 1.2
	default:
		return 1.0
	}
}

func shouldSkipLocalCodeSearchDir(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "tmp", "dist", "build", "archive", "vendor", "prompt-exports":
		return true
	default:
		return false
	}
}

func bestLexicalMatchLine(text string, terms []string) (int, float64) {
	lines := strings.Split(text, "\n")
	bestLine := 1
	bestScore := 0.0
	for i, line := range lines {
		lower := strings.ToLower(line)
		lineScore := 0.0
		for _, term := range terms {
			if term != "" && strings.Contains(lower, term) {
				lineScore++
			}
		}
		if lineScore > bestScore {
			bestScore = lineScore
			bestLine = i + 1
		}
	}
	return bestLine, bestScore
}

func normalizeLexicalCodeScore(score float64) float64 {
	if score <= 0 {
		return 0.5
	}
	normalized := 0.45 + (score / 30.0)
	if normalized > 0.99 {
		return 0.99
	}
	if normalized < 0.5 {
		return 0.5
	}
	return normalized
}

func (a *ReadOnlyAdapter) localBuildTargetCodeSearchHits(query string) []rankedCodeSearchHit {
	queryLower := strings.ToLower(query)
	if !strings.Contains(queryLower, "build") && !strings.Contains(queryLower, "target") && !strings.Contains(queryLower, "command") && !strings.Contains(queryLower, "cgo") {
		return nil
	}
	makefilePath := filepath.Join(a.workspaceRoot, "Makefile")
	body, err := os.ReadFile(makefilePath)
	if err != nil {
		return nil
	}
	text := string(body)
	lowerText := strings.ToLower(text)
	exactProbes := codeSearchTaskExactProbes(query, "")
	for _, probe := range exactProbes {
		probe = strings.ToLower(strings.TrimSpace(probe))
		if probe == "" {
			continue
		}
		idx := strings.Index(lowerText, probe)
		if idx < 0 {
			continue
		}
		line := 1 + strings.Count(text[:idx], "\n")
		return []rankedCodeSearchHit{{
			Priority: 31,
			Hit: contextengine.CodeSearchHit{
				Path:     "Makefile",
				Snippet:  codeProbeSnippet("build target evidence: "+probe, sliceLines(text, maxInt(1, line-2), line+5)),
				Line:     line,
				Score:    0.99,
				Language: "make",
			},
		}}
	}
	return nil
}

var qualifiedRelatedIdentifierPattern = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.([A-Z][A-Za-z0-9_]{3,})\b`)

func (a *ReadOnlyAdapter) localRelatedCodeSearchHits(query string, seeds []contextengine.CodeSearchHit, limit int) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" || len(seeds) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	type relatedCandidate struct {
		hit      contextengine.CodeSearchHit
		priority int
	}
	candidates := map[string]relatedCandidate{}
	seedPaths := map[string]struct{}{}
	type definitionRequest struct {
		seedPath string
		symbol   string
	}
	definitionRequests := make([]definitionRequest, 0)
	definitionSymbols := make([]string, 0)
	queryLower := strings.ToLower(query)
	exactProbes := codeSearchTaskExactProbes(query, "")

	add := func(pathValue string, priority int, line int, reason string, symbol string) {
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" {
			return
		}
		key := pathValue + "|" + strings.TrimSpace(symbol)
		existing, ok := candidates[key]
		if ok && existing.priority > priority {
			return
		}
		snippet := strings.TrimSpace(reason)
		if excerpt := a.repoFileExcerpt(pathValue, line, 3, 5); excerpt != "" {
			if snippet != "" {
				snippet += "\n"
			}
			snippet += "excerpt: " + excerpt
		}
		score := 0.88 + float64(priority-24)*0.02
		if score > 0.99 {
			score = 0.99
		}
		candidates[key] = relatedCandidate{
			priority: priority,
			hit: contextengine.CodeSearchHit{
				Path:     pathValue,
				Snippet:  snippet,
				Line:     line,
				Symbol:   strings.TrimSpace(symbol),
				Score:    score,
				Language: languageFromPath(pathValue),
			},
		}
	}

	for _, seed := range seeds {
		seedPath := normalizeCodeSearchPath(seed.Path)
		if seedPath == "" {
			continue
		}
		seedPaths[seedPath] = struct{}{}
		if counterpart := productionCounterpartPath(seedPath); counterpart != "" {
			add(counterpart, 28, 1, "related production file for "+seedPath, "")
		}
		if commandSourceAffinity(queryLower, seedPath, seed.Snippet, exactProbes) {
			add(seedPath, 29, seed.Line, "command/build companion evidence", seed.Symbol)
		}
		body, err := os.ReadFile(filepath.Join(a.workspaceRoot, filepath.FromSlash(seedPath)))
		if err != nil {
			continue
		}
		text := string(body)
		for _, symbol := range relatedExportedIdentifiers(text, seed.Snippet, exactProbes) {
			definitionRequests = append(definitionRequests, definitionRequest{seedPath: seedPath, symbol: symbol})
			definitionSymbols = append(definitionSymbols, symbol)
		}
	}
	definitions := a.findGoDefinitions(definitionSymbols)
	for _, req := range definitionRequests {
		def := definitions[req.symbol]
		if def.Path == "" {
			continue
		}
		priority := 29
		if strings.HasPrefix(req.symbol, "Retrieve") {
			priority = 30
		}
		add(def.Path, priority, def.Line, "related definition for "+req.symbol+" referenced by "+req.seedPath, req.symbol)
	}

	out := make([]relatedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seedPaths[normalizeCodeSearchPath(candidate.hit.Path)]; ok && !commandSourceAffinity(queryLower, candidate.hit.Path, candidate.hit.Snippet, exactProbes) {
			continue
		}
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].priority != out[j].priority {
			return out[i].priority > out[j].priority
		}
		if out[i].hit.Score != out[j].hit.Score {
			return out[i].hit.Score > out[j].hit.Score
		}
		return out[i].hit.Path < out[j].hit.Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	hits := make([]rankedCodeSearchHit, 0, len(out))
	for _, candidate := range out {
		hits = append(hits, rankedCodeSearchHit{Hit: candidate.hit, Priority: candidate.priority})
	}
	return hits
}

type subsystemSiblingCandidate struct {
	path         string
	line         int
	score        float64
	matchedTerms []string
	excerpt      string
	seedPaths    []string
}

type subsystemSiblingDirCandidate struct {
	dir   string
	count int
	score float64
}

func (a *ReadOnlyAdapter) localSubsystemSiblingClosureHits(query string, taskType string, requiredEvidence []string, seeds []contextengine.CodeSearchHit, limit int) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" || len(seeds) == 0 || !isSubsystemMapTask(taskType) {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	terms := subsystemClosureTerms(query, requiredEvidence)
	if len(terms) == 0 {
		return nil
	}
	seedPathSet := map[string]struct{}{}
	dirStats := map[string]*subsystemSiblingDirCandidate{}
	for _, seed := range seeds {
		pathValue := normalizeCodeSearchPath(seed.Path)
		if pathValue == "" || !isLikelyTextCodeFile(pathValue) {
			continue
		}
		seedPathSet[pathValue] = struct{}{}
		dir := filepath.ToSlash(filepath.Dir(pathValue))
		if dir == "" || dir == "." {
			continue
		}
		item := dirStats[dir]
		if item == nil {
			item = &subsystemSiblingDirCandidate{dir: dir}
			dirStats[dir] = item
		}
		item.count++
		item.score += repoAnchorScore(seed.Score)
	}
	if len(dirStats) == 0 {
		return nil
	}
	dirCandidates := make([]*subsystemSiblingDirCandidate, 0, len(dirStats))
	for _, item := range dirStats {
		dirCandidates = append(dirCandidates, item)
	}
	sort.SliceStable(dirCandidates, func(i, j int) bool {
		if dirCandidates[i].count != dirCandidates[j].count {
			return dirCandidates[i].count > dirCandidates[j].count
		}
		if dirCandidates[i].score != dirCandidates[j].score {
			return dirCandidates[i].score > dirCandidates[j].score
		}
		return dirCandidates[i].dir < dirCandidates[j].dir
	})
	maxDirs := minInt(maxInt(limit, 4), 10)
	if len(dirCandidates) > maxDirs {
		dirCandidates = dirCandidates[:maxDirs]
	}
	candidatesByPath := map[string]*subsystemSiblingCandidate{}
	for _, dirCandidate := range dirCandidates {
		dir := dirCandidate.dir
		entries, err := os.ReadDir(filepath.Join(a.workspaceRoot, filepath.FromSlash(dir)))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || shouldSkipLocalCodeSearchDir(entry.Name()) {
				continue
			}
			pathValue := normalizeCodeSearchPath(filepath.ToSlash(filepath.Join(dir, entry.Name())))
			if pathValue == "" {
				continue
			}
			if _, ok := seedPathSet[pathValue]; ok {
				continue
			}
			if !isLikelyTextCodeFile(pathValue) {
				continue
			}
			info, statErr := entry.Info()
			if statErr == nil && info.Size() > 1_000_000 {
				continue
			}
			body, readErr := os.ReadFile(filepath.Join(a.workspaceRoot, filepath.FromSlash(pathValue)))
			if readErr != nil {
				continue
			}
			line, score, matches := scoreSubsystemSiblingFile(pathValue, string(body), terms)
			if score <= 0 {
				continue
			}
			if isTestLikeCodeSearchPath(pathValue) {
				score *= 0.35
			}
			item := candidatesByPath[pathValue]
			if item == nil {
				item = &subsystemSiblingCandidate{path: pathValue}
				candidatesByPath[pathValue] = item
			}
			item.score += score
			item.line = firstPositiveIntEnv(item.line, line)
			item.matchedTerms = appendUniqueStringsEnv(item.matchedTerms, matches...)
			item.seedPaths = appendUniqueStringsEnv(item.seedPaths, seedPathsInDir(seeds, dir)...)
			if item.excerpt == "" {
				item.excerpt = a.repoFileExcerpt(pathValue, line, 3, 5)
			}
		}
	}
	candidates := make([]*subsystemSiblingCandidate, 0, len(candidatesByPath))
	for _, candidate := range candidatesByPath {
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		wi := codeSearchFileWeight(candidates[i].path)
		wj := codeSearchFileWeight(candidates[j].path)
		if wi != wj {
			return wi > wj
		}
		return candidates[i].path < candidates[j].path
	})
	maxHits := minInt(maxInt(1, limit/3), 4)
	candidates = selectSubsystemSiblingCandidates(candidates, dirCandidates, maxHits)
	out := make([]rankedCodeSearchHit, 0, len(candidates))
	for _, candidate := range candidates {
		reason := "subsystem sibling closure"
		if len(candidate.matchedTerms) > 0 {
			reason += ": " + strings.Join(candidate.matchedTerms, ", ")
		}
		snippet := codeProbeSnippet(reason, candidate.excerpt)
		out = append(out, rankedCodeSearchHit{
			Priority: 31,
			Hit: contextengine.CodeSearchHit{
				Path:     candidate.path,
				Snippet:  snippet,
				Line:     candidate.line,
				Score:    clampScore(0.80 + minFloat(candidate.score, 4.0)*0.04),
				Language: languageFromPath(candidate.path),
				Metadata: map[string]any{
					"candidate_role":   "structural_support",
					"source":           "local_subsystem_sibling_closure",
					"source_profile":   "repo_code",
					"evidence_class":   "structural",
					"task_type":        normalizeSubsystemMapTaskType(taskType),
					"matched_terms":    append([]string(nil), candidate.matchedTerms...),
					"coverage_terms":   append([]string(nil), candidate.matchedTerms...),
					"path_family":      filepath.ToSlash(filepath.Dir(candidate.path)),
					"is_test":          isTestLikeCodeSearchPath(candidate.path),
					"supporting_paths": append([]string(nil), candidate.seedPaths...),
				},
				Sources: []string{"local_subsystem_sibling_closure"},
			},
		})
	}
	return out
}

func selectSubsystemSiblingCandidates(candidates []*subsystemSiblingCandidate, dirs []*subsystemSiblingDirCandidate, maxHits int) []*subsystemSiblingCandidate {
	if maxHits <= 0 || len(candidates) == 0 {
		return nil
	}
	if len(candidates) > maxHits {
		out := make([]*subsystemSiblingCandidate, 0, maxHits)
		selected := map[string]struct{}{}
		for _, dir := range dirs {
			if len(out) >= maxHits {
				break
			}
			for _, candidate := range candidates {
				if filepath.ToSlash(filepath.Dir(candidate.path)) != dir.dir {
					continue
				}
				if _, ok := selected[candidate.path]; ok {
					continue
				}
				selected[candidate.path] = struct{}{}
				out = append(out, candidate)
				break
			}
		}
		for _, candidate := range candidates {
			if len(out) >= maxHits {
				break
			}
			if _, ok := selected[candidate.path]; ok {
				continue
			}
			selected[candidate.path] = struct{}{}
			out = append(out, candidate)
		}
		return out
	}
	return append([]*subsystemSiblingCandidate(nil), candidates...)
}

func isSubsystemMapTask(taskType string) bool {
	switch strings.TrimSpace(taskType) {
	case "architecture_map", "subsystem_map", "integration_surface":
		return true
	default:
		return false
	}
}

func normalizeSubsystemMapTaskType(taskType string) string {
	if isSubsystemMapTask(taskType) {
		return strings.TrimSpace(taskType)
	}
	return ""
}

func subsystemClosureTerms(query string, requiredEvidence []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 16)
	add := func(value string) {
		for _, part := range splitCodeSearchProbe(value) {
			for _, term := range subsystemClosureTermVariants(part) {
				if len(term) < 4 || isGenericCodeSearchPathWord(term) {
					continue
				}
				if _, ok := seen[term]; ok {
					continue
				}
				seen[term] = struct{}{}
				out = append(out, term)
			}
		}
	}
	for _, item := range requiredEvidence {
		add(item)
	}
	if len(out) < 4 {
		add(query)
	}
	return out
}

func subsystemClosureTermVariants(term string) []string {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return nil
	}
	out := []string{term}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == term {
			return
		}
		for _, existing := range out {
			if existing == value {
				return
			}
		}
		out = append(out, value)
	}
	switch {
	case strings.HasSuffix(term, "ification") && len(term) > len("ification")+2:
		add(strings.TrimSuffix(term, "ification") + "ify")
	case strings.HasSuffix(term, "ication") && len(term) > len("ication")+2:
		add(strings.TrimSuffix(term, "ication") + "ify")
	case strings.HasSuffix(term, "ation") && len(term) > len("ation")+2:
		add(strings.TrimSuffix(term, "ation") + "ate")
	}
	if strings.HasSuffix(term, "ies") && len(term) > 4 {
		add(strings.TrimSuffix(term, "ies") + "y")
	}
	if strings.HasSuffix(term, "s") && len(term) > 5 {
		add(strings.TrimSuffix(term, "s"))
	}
	return out
}

func scoreSubsystemSiblingFile(pathValue string, body string, terms []string) (int, float64, []string) {
	lowerPath := strings.ToLower(pathValue)
	lines := strings.Split(body, "\n")
	bestLine := 1
	bestScore := 0.0
	matches := make([]string, 0)
	for _, term := range terms {
		if term == "" {
			continue
		}
		termScore := 0.0
		if strings.Contains(lowerPath, term) {
			termScore += 2.0
		}
		for i, line := range lines {
			lineLower := strings.ToLower(line)
			if !strings.Contains(lineLower, term) {
				continue
			}
			score := 1.0
			if looksLikeDeclarationLine(line, term) {
				score += 1.5
			}
			if score > termScore {
				termScore = score
				bestLine = i + 1
			}
		}
		if termScore > 0 {
			bestScore += termScore
			matches = appendUniqueStringEnv(matches, term)
		}
	}
	return bestLine, bestScore, matches
}

func looksLikeDeclarationLine(line string, term string) bool {
	line = strings.TrimSpace(line)
	term = strings.ToLower(strings.TrimSpace(term))
	if line == "" || term == "" {
		return false
	}
	lower := strings.ToLower(line)
	if !strings.Contains(lower, term) {
		return false
	}
	switch {
	case strings.HasPrefix(line, "func "), strings.HasPrefix(line, "type "),
		strings.HasPrefix(line, "const "), strings.HasPrefix(line, "var "),
		strings.HasPrefix(line, "class "), strings.HasPrefix(line, "export "),
		strings.HasPrefix(line, "def "):
		return true
	default:
		return false
	}
}

func isTestLikeCodeSearchPath(pathValue string) bool {
	pathValue = strings.ToLower(filepath.ToSlash(strings.TrimSpace(pathValue)))
	if pathValue == "" {
		return false
	}
	base := filepath.Base(pathValue)
	switch {
	case strings.HasSuffix(base, "_test.go"),
		strings.HasSuffix(base, ".test.ts"),
		strings.HasSuffix(base, ".test.tsx"),
		strings.HasSuffix(base, ".test.js"),
		strings.HasSuffix(base, ".test.jsx"),
		strings.HasSuffix(base, ".spec.ts"),
		strings.HasSuffix(base, ".spec.tsx"),
		strings.HasSuffix(base, ".spec.js"),
		strings.HasSuffix(base, ".spec.jsx"):
		return true
	}
	parts := strings.Split(pathValue, "/")
	for _, part := range parts {
		switch part {
		case "test", "tests", "__tests__":
			return true
		}
	}
	return false
}

func seedPathsInDir(seeds []contextengine.CodeSearchHit, dir string) []string {
	out := make([]string, 0)
	for _, seed := range seeds {
		pathValue := normalizeCodeSearchPath(seed.Path)
		if pathValue == "" {
			continue
		}
		if filepath.ToSlash(filepath.Dir(pathValue)) == dir {
			out = appendUniqueStringEnv(out, pathValue)
		}
	}
	return out
}

func appendUniqueStringsEnv(values []string, additions ...string) []string {
	for _, value := range additions {
		values = appendUniqueStringEnv(values, value)
	}
	return values
}

func appendUniqueStringEnv(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func firstPositiveIntEnv(current int, next int) int {
	if current > 0 {
		return current
	}
	if next > 0 {
		return next
	}
	return 0
}

func productionCounterpartPath(pathValue string) string {
	pathValue = normalizeCodeSearchPath(pathValue)
	if !strings.HasSuffix(pathValue, "_test.go") {
		return ""
	}
	return strings.TrimSuffix(pathValue, "_test.go") + ".go"
}

func commandSourceAffinity(queryLower, pathValue, snippet string, exactProbes []string) bool {
	pathValue = normalizeCodeSearchPath(pathValue)
	if pathValue == "" {
		return false
	}
	isCommandQuery := strings.Contains(queryLower, "command") || strings.Contains(queryLower, "cli") || strings.Contains(queryLower, "build target") || strings.Contains(queryLower, "target")
	isCommandPath := strings.HasPrefix(pathValue, "cmd/") || strings.EqualFold(filepath.Base(pathValue), "Makefile")
	if !isCommandQuery || !isCommandPath {
		return false
	}
	haystack := strings.ToLower(pathValue + "\n" + snippet)
	for _, probe := range exactProbes {
		probe = strings.ToLower(strings.TrimSpace(probe))
		if probe != "" && strings.Contains(haystack, probe) {
			return true
		}
	}
	return false
}

func relatedExportedIdentifiers(fileText string, snippet string, exactProbes []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 16)
	add := func(symbol string) {
		symbol = strings.TrimSpace(symbol)
		if len(symbol) < 4 || isGenericRelatedSymbol(symbol) {
			return
		}
		if _, ok := seen[symbol]; ok {
			return
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	for _, probe := range exactProbes {
		if isLikelyPascalCodeIdentifier(probe) {
			add(probe)
		}
	}
	for _, match := range qualifiedRelatedIdentifierPattern.FindAllStringSubmatch(snippet, -1) {
		if len(match) == 2 {
			add(match[1])
		}
	}
	for _, match := range qualifiedRelatedIdentifierPattern.FindAllStringSubmatch(fileText, -1) {
		if len(match) == 2 {
			add(match[1])
			if len(out) >= 32 {
				break
			}
		}
	}
	return out
}

func isGenericRelatedSymbol(symbol string) bool {
	switch symbol {
	case "Error", "Context", "String", "Reader", "Writer", "Request", "Response", "Config", "Options", "Result", "Results":
		return true
	default:
		return false
	}
}

type goDefinitionRef struct {
	Path string
	Line int
}

func sortedStringKeys[V any](in map[string]V) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (a *ReadOnlyAdapter) findGoDefinitions(symbols []string) map[string]goDefinitionRef {
	wanted := map[string]struct{}{}
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			continue
		}
		wanted[symbol] = struct{}{}
	}
	out := map[string]goDefinitionRef{}
	if len(wanted) == 0 {
		return out
	}
	_ = filepath.WalkDir(a.workspaceRoot, func(current string, d os.DirEntry, err error) error {
		if err != nil || len(out) >= len(wanted) {
			return nil
		}
		if d.IsDir() {
			if shouldSkipLocalCodeSearchDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(a.workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if filepath.Ext(rel) != ".go" || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(current)
		if readErr != nil {
			return nil
		}
		text := string(body)
		for symbol := range wanted {
			if _, exists := out[symbol]; exists {
				continue
			}
			if line := findGoDefinitionLine(text, symbol); line > 0 {
				out[symbol] = goDefinitionRef{Path: rel, Line: line}
			}
		}
		return nil
	})
	return out
}

func findGoDefinitionLine(text, symbol string) int {
	defPatterns := []string{
		"func " + symbol + "(",
		"func (",
		"type " + symbol + " ",
		"var " + symbol,
		"const " + symbol,
	}
	for _, pattern := range defPatterns {
		idx := strings.Index(text, pattern)
		if idx < 0 {
			continue
		}
		if pattern == "func (" && !strings.Contains(text[idx:minInt(len(text), idx+180)], ") "+symbol+"(") {
			continue
		}
		return 1 + strings.Count(text[:idx], "\n")
	}
	return 0
}

func workspaceCodeProbeSearch(workspaceRoot string, pathProbes, exactProbes []string, perProbeLimit int) ([]localPathProbeHit, []localExactProbeHit, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" || (len(pathProbes) == 0 && len(exactProbes) == 0) {
		return nil, nil, nil
	}
	if perProbeLimit <= 0 {
		perProbeLimit = 32
	}
	normalizedPathProbes := make([]string, 0, len(pathProbes))
	pathSeen := map[string]struct{}{}
	for _, probe := range pathProbes {
		probe = strings.ToLower(strings.TrimSpace(probe))
		if probe == "" {
			continue
		}
		if _, ok := pathSeen[probe]; ok {
			continue
		}
		pathSeen[probe] = struct{}{}
		normalizedPathProbes = append(normalizedPathProbes, probe)
	}
	normalizedExactProbes := make([]string, 0, len(exactProbes))
	exactSeen := map[string]struct{}{}
	for _, probe := range exactProbes {
		probe = strings.TrimSpace(probe)
		if probe == "" {
			continue
		}
		if _, ok := exactSeen[probe]; ok {
			continue
		}
		exactSeen[probe] = struct{}{}
		normalizedExactProbes = append(normalizedExactProbes, probe)
	}
	pathCounts := map[string]int{}
	exactCounts := map[string]int{}
	pathHits := make([]localPathProbeHit, 0)
	exactHits := make([]localExactProbeHit, 0)
	err := filepath.WalkDir(workspaceRoot, func(current string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipLocalCodeSearchDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !isLikelyTextCodeFile(rel) {
			return nil
		}
		lowerRel := strings.ToLower(rel)
		for _, probe := range normalizedPathProbes {
			if pathCounts[probe] >= perProbeLimit || !strings.Contains(lowerRel, probe) {
				continue
			}
			pathHits = append(pathHits, localPathProbeHit{Path: rel, Probe: probe})
			pathCounts[probe]++
		}
		if len(normalizedExactProbes) == 0 {
			return nil
		}
		info, statErr := d.Info()
		if statErr == nil && info.Size() > 1_000_000 {
			return nil
		}
		body, readErr := os.ReadFile(current)
		if readErr != nil {
			return nil
		}
		text := string(body)
		for _, probe := range normalizedExactProbes {
			if exactCounts[probe] >= perProbeLimit {
				continue
			}
			index := strings.Index(text, probe)
			if index < 0 {
				continue
			}
			exactHits = append(exactHits, localExactProbeHit{
				Path:  rel,
				Probe: probe,
				Line:  1 + strings.Count(text[:index], "\n"),
			})
			exactCounts[probe]++
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(pathHits, func(i, j int) bool {
		if pathHits[i].Probe != pathHits[j].Probe {
			return pathHits[i].Probe < pathHits[j].Probe
		}
		wi := codeSearchFileWeight(pathHits[i].Path)
		wj := codeSearchFileWeight(pathHits[j].Path)
		if wi != wj {
			return wi > wj
		}
		return pathHits[i].Path < pathHits[j].Path
	})
	sort.SliceStable(exactHits, func(i, j int) bool {
		if exactHits[i].Probe != exactHits[j].Probe {
			return exactHits[i].Probe < exactHits[j].Probe
		}
		wi := codeSearchFileWeight(exactHits[i].Path)
		wj := codeSearchFileWeight(exactHits[j].Path)
		if wi != wj {
			return wi > wj
		}
		return exactHits[i].Path < exactHits[j].Path
	})
	return pathHits, exactHits, nil
}

func firstCodeSearchLine(lines []int) int {
	for _, line := range lines {
		if line > 0 {
			return line
		}
	}
	return 0
}

func firstCodeSearchSymbol(symbols []string) string {
	for _, symbol := range symbols {
		if strings.TrimSpace(symbol) != "" {
			return strings.TrimSpace(symbol)
		}
	}
	return ""
}

func codeProbeSnippet(reason string, excerpt string) string {
	reason = strings.TrimSpace(reason)
	excerpt = strings.TrimSpace(excerpt)
	switch {
	case reason == "":
		return excerpt
	case excerpt == "":
		return reason
	default:
		return reason + "\nexcerpt: " + excerpt
	}
}

func mergeCodeSearchHits(limit int, repoHits []contextengine.CodeSearchHit, rankedGroups ...[]rankedCodeSearchHit) []contextengine.CodeSearchHit {
	if limit <= 0 {
		limit = 8
	}
	type mergedHit struct {
		hit      contextengine.CodeSearchHit
		priority int
	}
	byKey := map[string]mergedHit{}
	add := func(hit contextengine.CodeSearchHit, priority int) {
		hit.Path = normalizeCodeSearchPath(hit.Path)
		hit.Symbol = strings.TrimSpace(hit.Symbol)
		if hit.Path == "" && hit.Symbol == "" {
			return
		}
		if hit.Language == "" {
			hit.Language = languageFromPath(hit.Path)
		}
		key := hit.Path + "|" + hit.Symbol
		existing, ok := byKey[key]
		if !ok {
			byKey[key] = mergedHit{hit: hit, priority: priority}
			return
		}
		if priority > existing.priority || (priority == existing.priority && hit.Score > existing.hit.Score) {
			if strings.TrimSpace(hit.Snippet) == "" {
				hit.Snippet = existing.hit.Snippet
			} else if strings.Contains(existing.hit.Snippet, "excerpt:") && !strings.Contains(hit.Snippet, "excerpt:") {
				hit.Snippet = strings.TrimSpace(hit.Snippet) + "\n" + strings.TrimSpace(existing.hit.Snippet)
			}
			hit = supplementCodeSearchHit(hit, existing.hit)
			if hit.Line == 0 {
				hit.Line = existing.hit.Line
			}
			if len(hit.Metadata) == 0 {
				hit.Metadata = existing.hit.Metadata
			}
			if len(hit.Sources) == 0 {
				hit.Sources = existing.hit.Sources
			}
			byKey[key] = mergedHit{hit: hit, priority: priority}
			return
		}
		existing.hit = supplementCodeSearchHit(existing.hit, hit)
		if existing.hit.Snippet == "" && hit.Snippet != "" {
			existing.hit.Snippet = hit.Snippet
		}
		if existing.hit.Line == 0 && hit.Line > 0 {
			existing.hit.Line = hit.Line
		}
		if len(existing.hit.Metadata) == 0 && len(hit.Metadata) > 0 {
			existing.hit.Metadata = hit.Metadata
		}
		if len(existing.hit.Sources) == 0 && len(hit.Sources) > 0 {
			existing.hit.Sources = hit.Sources
		}
		byKey[key] = existing
	}
	for _, hit := range repoHits {
		add(hit, 10)
	}
	for _, group := range rankedGroups {
		for _, hit := range group {
			add(hit.Hit, hit.Priority)
		}
	}
	out := make([]mergedHit, 0, len(byKey))
	for _, hit := range byKey {
		out = append(out, hit)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].priority != out[j].priority {
			return out[i].priority > out[j].priority
		}
		if out[i].hit.Score != out[j].hit.Score {
			return out[i].hit.Score > out[j].hit.Score
		}
		wi := codeSearchFileWeight(out[i].hit.Path)
		wj := codeSearchFileWeight(out[j].hit.Path)
		if wi != wj {
			return wi > wj
		}
		return out[i].hit.Path < out[j].hit.Path
	})
	hits := make([]contextengine.CodeSearchHit, 0, minInt(limit, len(out)))
	for _, item := range out {
		hits = append(hits, item.hit)
		if len(hits) >= limit {
			break
		}
	}
	return hits
}

func supplementCodeSearchHit(base contextengine.CodeSearchHit, extra contextengine.CodeSearchHit) contextengine.CodeSearchHit {
	if strings.TrimSpace(extra.Snippet) != "" && !strings.Contains(base.Snippet, strings.TrimSpace(extra.Snippet)) {
		switch {
		case strings.TrimSpace(base.Snippet) == "":
			base.Snippet = strings.TrimSpace(extra.Snippet)
		case strings.Contains(extra.Snippet, "matched terms:") || strings.Contains(extra.Snippet, "exact code probe:") || strings.Contains(extra.Snippet, "symbol definition:"):
			base.Snippet = strings.TrimSpace(base.Snippet) + "\n" + strings.TrimSpace(extra.Snippet)
		}
	}
	if base.Metadata == nil && len(extra.Metadata) > 0 {
		base.Metadata = map[string]any{}
	}
	if len(extra.Metadata) > 0 {
		for key, value := range extra.Metadata {
			switch key {
			case "matched_terms", "coverage_terms", "coverage_requirement_ids", "sources":
				merged := metadataStringSliceEnv(base.Metadata, key)
				merged = appendUniqueStringsEnv(merged, metadataStringSliceEnv(extra.Metadata, key)...)
				if len(merged) > 0 {
					base.Metadata[key] = merged
				}
			default:
				if _, ok := base.Metadata[key]; !ok {
					base.Metadata[key] = value
				}
			}
		}
	}
	base.Sources = appendUniqueStringsEnv(base.Sources, extra.Sources...)
	return base
}

func (a *ReadOnlyAdapter) repoAnchorStatement(anchor repoquery.Anchor) string {
	parts := make([]string, 0, 4)
	if anchor.SymbolName != "" {
		parts = append(parts, "symbol: "+anchor.SymbolName)
	}
	if anchor.Summary != "" {
		parts = append(parts, "summary: "+anchor.Summary)
	}
	if excerpt := a.repoAnchorExcerpt(anchor); excerpt != "" {
		parts = append(parts, "excerpt: "+excerpt)
	}
	if len(parts) == 0 {
		return anchor.Path
	}
	return strings.Join(parts, "\n")
}

func (a *ReadOnlyAdapter) repoAnchorExcerpt(anchor repoquery.Anchor) string {
	if anchor.LineHint <= 0 || strings.TrimSpace(anchor.Path) == "" {
		return ""
	}
	return a.repoFileExcerpt(anchor.Path, anchor.LineHint, 3, 5)
}

func (a *ReadOnlyAdapter) repoFileExcerpt(path string, line, before, after int) string {
	if line <= 0 || strings.TrimSpace(path) == "" {
		return ""
	}
	body, err := os.ReadFile(filepath.Join(a.workspaceRoot, filepath.FromSlash(path)))
	if err != nil {
		return ""
	}
	start := line - before
	if start < 1 {
		start = 1
	}
	end := line + after
	return strings.TrimSpace(sliceLines(string(body), start, end))
}

func repoAnchorScore(score float64) float64 {
	if score <= 0 {
		return 0.75
	}
	if score > 1 {
		return 1
	}
	return score
}

func languageFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".ex", ".exs":
		return "elixir"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}

// memoryQueryFn returns a MemoryQueryFunc backed by the contextengine claim store.
// If the SQLite store is unavailable, returns no claims.
func (a *ReadOnlyAdapter) memoryQueryFn(limit int) contextengine.MemoryQueryFunc {
	return a.memoryQueryFnForStatuses(limit, nil)
}

func (a *ReadOnlyAdapter) memoryQueryFnForStatuses(limit int, statuses []contextengine.ClaimStatus) contextengine.MemoryQueryFunc {
	return func(ctx context.Context, workspaceID, query string) ([]contextengine.MemoryClaim, error) {
		if a.ceStore == nil {
			return nil, nil
		}
		if len(statuses) == 0 {
			statuses = []contextengine.ClaimStatus{contextengine.ClaimStatusCurrent}
		}
		// Fetch with no SQL-side query filtering; ClaimFilter does not yet
		// support substring search. We fetch up to a generous cap then
		// filter in memory by case-insensitive substring match on the
		// claim's textual fields.
		fetchLimit := limit
		if query != "" && fetchLimit > 0 {
			// When filtering, broaden the fetch so the post-filter result
			// can still satisfy the requested limit.
			fetchLimit = limit * 10
			if fetchLimit > 1000 {
				fetchLimit = 1000
			}
		}
		claims := make([]contextengine.MemoryClaim, 0, fetchLimit)
		seen := map[string]struct{}{}
		for _, status := range statuses {
			found, err := a.ceStore.ListClaims(ctx, ctxengstore.ClaimFilter{
				WorkspaceID: workspaceID,
				Status:      status,
				Limit:       fetchLimit,
			})
			if err != nil {
				return nil, err
			}
			for _, claim := range found {
				if _, ok := seen[claim.ID]; ok {
					continue
				}
				seen[claim.ID] = struct{}{}
				claims = append(claims, claim)
			}
		}
		q := strings.TrimSpace(strings.ToLower(query))
		if q == "" {
			if limit > 0 && len(claims) > limit {
				return claims[:limit], nil
			}
			return claims, nil
		}
		filtered := make([]contextengine.MemoryClaim, 0, len(claims))
		for _, c := range claims {
			if claimMatchesQuery(c, q) {
				filtered = append(filtered, c)
				if limit > 0 && len(filtered) >= limit {
					break
				}
			}
		}
		return filtered, nil
	}
}

func parseMemoryClaimStatuses(raw []string) []contextengine.ClaimStatus {
	out := make([]contextengine.ClaimStatus, 0, len(raw))
	seen := map[contextengine.ClaimStatus]struct{}{}
	for _, item := range raw {
		status := contextengine.ClaimStatus(strings.TrimSpace(item))
		if !status.IsValid() {
			continue
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		out = append(out, status)
	}
	return out
}

func cleanStringList(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, item := range raw {
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

// claimMatchesQuery reports whether any of the claim's searchable text
// fields contain the lowercased substring q. Caller must lowercase q.
func claimMatchesQuery(c contextengine.MemoryClaim, q string) bool {
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(c.Summary), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.ClaimType), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.Reason), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.Scope.Path), q) {
		return true
	}
	return false
}

// contextQueryFn returns a ContextQueryFunc that synthesizes a ContextPacket
// from the environment's TopOfMind projection.
func (a *ReadOnlyAdapter) contextQueryFn() contextengine.ContextQueryFunc {
	return func(_ context.Context, workspaceID string) (*contextengine.ContextPacket, error) {
		top := a.environment.TopOfMind
		handoff := a.environment.LatestHandoff
		if top == nil && handoff == nil {
			return nil, nil
		}
		packet := contextengine.ContextPacket{WorkspaceID: workspaceID}
		if top != nil {
			packet = contextPacketFromTopOfMindMap(top, workspaceID)
		}
		applyLatestHandoffToContextPacket(&packet, handoff)
		return &packet, nil
	}
}

func contextPacketFromTopOfMindMap(top map[string]any, workspaceID string) contextengine.ContextPacket {
	var typed contextplane.TopOfMind
	if body, err := json.Marshal(top); err == nil {
		_ = json.Unmarshal(body, &typed)
	}
	if typed.WorkspaceID == "" {
		typed.WorkspaceID = workspaceID
	}
	if typed.Objective != "" || typed.Phase != "" || len(typed.RelevantRefs) > 0 {
		packet := adapters.ConvertTopOfMind(typed)
		if packet.WorkspaceID == "" {
			packet.WorkspaceID = workspaceID
		}
		return packet
	}
	return contextengine.ContextPacket{
		WorkspaceID: workspaceID,
		Objective:   stringValue(top["objective"]),
		Phase:       stringValue(top["phase"]),
		Metadata:    map[string]any{"source": "top_of_mind"},
	}
}

func applyLatestHandoffToContextPacket(packet *contextengine.ContextPacket, handoff map[string]any) {
	if packet == nil || handoff == nil {
		return
	}
	summary := strings.TrimSpace(stringValue(handoff["summary"]))
	if summary != "" {
		packet.RecentDecisions = append(packet.RecentDecisions, contextengine.RecentDecision{
			ID:   "latest_handoff",
			Text: "Latest handoff: " + summary,
			Ref:  stringValue(handoff["ref"]),
		})
	}
	packet.RelevantRefs = append(packet.RelevantRefs, evidenceRefsFromAny(handoff["evidence_refs"])...)
	packet.RelevantRefs = append(packet.RelevantRefs, evidenceRefsFromAny(handoff["file_refs"])...)
	if packet.Metadata == nil {
		packet.Metadata = map[string]any{}
	}
	packet.Metadata["latest_handoff"] = handoff
}

func evidenceRefsFromAny(value any) []contextengine.EvidenceRef {
	switch typed := value.(type) {
	case []contextengine.EvidenceRef:
		return append([]contextengine.EvidenceRef(nil), typed...)
	case []any:
		out := make([]contextengine.EvidenceRef, 0, len(typed))
		for _, item := range typed {
			switch ref := item.(type) {
			case string:
				if parsed, err := contextengine.ParseEvidenceRef(ref); err == nil {
					out = append(out, parsed)
				}
			case map[string]any:
				body, err := json.Marshal(ref)
				if err != nil {
					continue
				}
				var parsed contextengine.EvidenceRef
				if err := json.Unmarshal(body, &parsed); err == nil && contextengine.ValidateEvidenceRef(parsed) == nil {
					out = append(out, parsed)
				}
			}
		}
		return out
	case []string:
		out := make([]contextengine.EvidenceRef, 0, len(typed))
		for _, item := range typed {
			if parsed, err := contextengine.ParseEvidenceRef(item); err == nil {
				out = append(out, parsed)
			}
		}
		return out
	default:
		return nil
	}
}

// taskQueryFn returns a TaskQueryFunc backed by the tasks SQLite store. It
// fetches a task by ID and projects it into a TaskContext via the existing
// adapters.ConvertTask helper. If the task store is unavailable, the task is
// missing, or it does not match the workspace, returns nil with no error so
// the lane records an empty pack rather than a failure.
func (a *ReadOnlyAdapter) taskQueryFn() contextengine.TaskQueryFunc {
	return func(ctx context.Context, workspaceID, taskID string) (*contextengine.TaskContext, error) {
		if a.taskStore == nil || strings.TrimSpace(taskID) == "" {
			return nil, nil
		}
		t, err := a.taskStore.Get(ctx, taskID)
		if err != nil {
			return nil, nil
		}
		if workspaceID != "" && t.WorkspaceID != "" && t.WorkspaceID != workspaceID {
			return nil, nil
		}
		tc := adapters.ConvertTask(t)
		return &tc, nil
	}
}

// taskListFn returns a TaskListFunc backed by the tasks SQLite store. It lists
// active and recently-completed tasks for the workspace, then applies a plain
// case-insensitive substring filter against title/description/scope/notes
// using the supplied query. The query is captured at the call site
// (retrieveTask / retrieveMixed) since TaskListFunc itself does not accept a
// query parameter.
func (a *ReadOnlyAdapter) taskListFn(query string) contextengine.TaskListFunc {
	return func(ctx context.Context, workspaceID string) ([]string, error) {
		if a.taskStore == nil {
			return nil, nil
		}
		opts := tasks.ListOptions{
			Statuses: []string{
				tasks.StatusPending,
				tasks.StatusInProgress,
				tasks.StatusReadyForReview,
				tasks.StatusBlocked,
				tasks.StatusCompleted,
			},
			Limit: 100,
		}
		all, err := a.taskStore.ListWithOptions(ctx, workspaceID, opts)
		if err != nil {
			return nil, err
		}
		needle := strings.ToLower(strings.TrimSpace(query))
		out := make([]string, 0, len(all))
		for _, t := range all {
			if !taskMatchesQuery(t, needle) {
				continue
			}
			out = append(out, t.ID)
		}
		return out, nil
	}
}

func (a *ReadOnlyAdapter) sessionRecallFn(limit int) contextengine.SessionRecallFunc {
	if strings.TrimSpace(a.cfg.Storage.Root) == "" {
		return nil
	}
	return func(ctx context.Context, workspaceID, query string, requestedLimit int) ([]contextengine.SessionRecallHit, error) {
		store, err := sessions.Open(ctx, a.cfg.Storage.Root)
		if err != nil {
			return nil, err
		}
		defer func() { _ = store.Close() }()
		effectiveLimit := requestedLimit
		if effectiveLimit <= 0 {
			effectiveLimit = limit
		}
		if effectiveLimit <= 0 {
			effectiveLimit = 5
		}
		provider := companion.SessionStoreRecallProvider{
			Store:     store,
			Workspace: firstNonEmpty(workspaceID, ws.ID(a.workspaceRoot), a.workspaceRoot),
		}
		matches, err := provider.RecallSessions(ctx, companion.SessionRecallRequest{
			Query:           query,
			Workspace:       firstNonEmpty(workspaceID, ws.ID(a.workspaceRoot), a.workspaceRoot),
			Limit:           effectiveLimit,
			MinSimilarity:   0.25,
			IncludeTimeline: true,
		})
		if err != nil {
			return nil, err
		}
		hits := make([]contextengine.SessionRecallHit, 0, len(matches))
		for _, match := range matches {
			metadata := map[string]any{
				"project_name": match.ProjectName,
				"git_branch":   match.GitBranch,
			}
			if len(match.TimelineSummaryLines) > 0 {
				metadata["timeline_summary"] = match.TimelineSummaryLines
			}
			if len(match.TimelineFiles) > 0 {
				metadata["timeline_files"] = match.TimelineFiles
			}
			hits = append(hits, contextengine.SessionRecallHit{
				SessionID:   match.SessionID,
				Summary:     match.Summary,
				Score:       match.Similarity,
				Decisions:   match.Decisions,
				Gotchas:     match.Gotchas,
				KeyFiles:    match.KeyFiles,
				StartedAt:   match.StartedAt,
				Source:      "session_store_recall",
				CanVerify:   true,
				SpanLocator: "session:" + match.SessionID,
				Metadata:    metadata,
			})
		}
		return hits, nil
	}
}

func (a *ReadOnlyAdapter) contextPacksFn(limit int) contextengine.ContextPackFunc {
	if strings.TrimSpace(a.workspaceRoot) == "" || strings.TrimSpace(a.cfg.Storage.Root) == "" {
		return nil
	}
	vaultPath := firstNonEmpty(strings.TrimSpace(a.vaultPath),
		strings.TrimSpace(os.Getenv("FOXCTL_RLM_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("FOXCTL_ACA_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("FOXCTL_OBSIDIAN_VAULT_PATH")),
	)
	if vaultPath == "" {
		return nil
	}
	return func(ctx context.Context, _ string, query string, requestedLimit int) ([]contextengine.EvidencePack, error) {
		effectiveLimit := requestedLimit
		if effectiveLimit <= 0 {
			effectiveLimit = limit
		}
		if effectiveLimit <= 0 {
			effectiveLimit = 5
		}
		index, err := obsidianindex.Open(ctx, a.cfg.Storage.Root, vaultPath)
		if err != nil {
			return nil, err
		}
		defer func() { _ = index.Close() }()

		var repoStore *repoindex.Store
		if repo, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot); err == nil {
			repoStore = repo
			defer func() { _ = repoStore.Close() }()
		}
		var memStoreForACA storage.MemoryStore
		if memStore, err := memorystore.OpenWithConfig(ctx, a.cfg); err == nil {
			memStoreForACA = memStore
			defer func() { _ = memStore.Close() }()
		}

		workspaceStore := contextplane.NewWorkspaceStore(a.workspaceRoot)
		opts := workspaceStore.CurrentRetrievalOptions()
		opts.IncludeTopOfMindResult = false
		opts.IncludeLatestHandoff = false
		opts.IncludeVaultHits = true
		opts.IncludeControlPlaneRefs = false
		result, err := workspaceStore.RetrieveWithOptionsAndMemory(ctx, index, repoStore, nil, memStoreForACA, query, effectiveLimit, opts)
		if err != nil {
			hits, searchErr := index.SearchNotes(ctx, query, effectiveLimit)
			if searchErr != nil {
				return nil, err
			}
			pack := directContextPackFromObsidianHits(firstNonEmpty(ws.ID(a.workspaceRoot), a.workspaceRoot), query, hits)
			if len(pack.Nodes) == 0 {
				return nil, nil
			}
			pack.Metadata["source"] = "aca_vault_search_fallback"
			pack.Metadata["fallback_error"] = err.Error()
			return []contextengine.EvidencePack{pack}, nil
		}
		pack := result.ToEvidencePack()
		if len(pack.Nodes) == 0 {
			return nil, nil
		}
		if pack.Metadata == nil {
			pack.Metadata = map[string]any{}
		}
		pack.Metadata["source"] = "aca_retrieval"
		return []contextengine.EvidencePack{pack}, nil
	}
}

func directContextPackFromObsidianHits(workspaceID, query string, hits []obsidianindex.SearchHit) contextengine.EvidencePack {
	workspaceID = strings.TrimSpace(workspaceID)
	nodes := make([]contextengine.EvidenceNode, 0, len(hits))
	for i, hit := range hits {
		path := strings.TrimSpace(hit.Path)
		if path == "" {
			continue
		}
		statement := strings.TrimSpace(hit.Snippet)
		if statement == "" {
			statement = strings.TrimSpace(hit.Title)
		}
		if statement == "" {
			statement = path
		}
		nodes = append(nodes, contextengine.EvidenceNode{
			ID:          fmt.Sprintf("aca_vault_hit_%d_%s", i, strings.ReplaceAll(path, "/", "_")),
			WorkspaceID: workspaceID,
			NodeType:    contextengine.EvidenceNodeTypeRetrieval,
			Ref: contextengine.EvidenceRef{
				Type:        contextengine.RefTypePath,
				Ref:         path,
				WorkspaceID: workspaceID,
				Title:       hit.Title,
				Excerpt:     hit.Snippet,
			},
			Statement:  statement,
			Confidence: float64(hit.Score) / 1000.0,
			Grounding:  contextengine.GroundingIndexed,
			Metadata: map[string]any{
				"source":  "obsidian_index",
				"title":   hit.Title,
				"type":    hit.Type,
				"project": hit.Project,
				"status":  hit.Status,
				"trust":   hit.Trust,
				"score":   hit.Score,
				"path":    path,
			},
		})
	}
	return contextengine.EvidencePack{
		ID:          "aca_vault_search:" + workspaceID,
		WorkspaceID: workspaceID,
		Query:       strings.TrimSpace(query),
		Lane:        contextengine.LaneContext,
		Nodes:       nodes,
		Metadata: map[string]any{
			"source": "aca_vault_search",
		},
	}
}

// taskMatchesQuery reports whether any of the task's searchable text fields
// contain the lowercased substring needle. An empty needle matches every
// task. Caller must pre-lowercase needle.
func taskMatchesQuery(t tasks.Task, needle string) bool {
	if needle == "" {
		return true
	}
	fields := []string{t.Title, t.Description, t.AtomicDescription, t.ScopePath, t.Notes, t.PlanSection}
	for _, f := range fields {
		if f == "" {
			continue
		}
		if strings.Contains(strings.ToLower(f), needle) {
			return true
		}
	}
	return false
}

// stalenessLookupFn returns a certifier hook backed by the contextengine store.
func (a *ReadOnlyAdapter) stalenessLookupFn() contextengine.StalenessLookupFunc {
	if a.ceStore == nil {
		return nil
	}
	return func(ctx context.Context, workspaceID string, refs []contextengine.EvidenceRef) ([]contextengine.StalenessMarker, error) {
		markers := make([]contextengine.StalenessMarker, 0, len(refs))
		for _, ref := range refs {
			ref := ref
			if !shouldLookupStalenessRef(ref) {
				continue
			}
			found, err := a.ceStore.ListStaleness(ctx, ctxengstore.StalenessFilter{
				WorkspaceID: workspaceID,
				TargetRef:   &ref,
				Limit:       1,
			})
			if err != nil {
				return nil, err
			}
			markers = append(markers, found...)
		}
		return markers, nil
	}
}

func shouldLookupStalenessRef(ref contextengine.EvidenceRef) bool {
	switch ref.Type {
	case contextengine.RefTypeMemoryClaim, contextengine.RefTypeNote, contextengine.RefTypeTask,
		contextengine.RefTypeSession, contextengine.RefTypeArtifact, contextengine.RefTypeTrajectory,
		contextengine.RefTypeCommit, contextengine.RefTypeEvent, contextengine.RefTypeRun,
		contextengine.RefTypeToolCall:
		return true
	default:
		return false
	}
}

func (a *ReadOnlyAdapter) retrieveCode(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input retrieveLaneInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	cfg := a.laneConfig()
	pack, err := contextengine.RetrieveCode(ctx, cfg, a.codeSearchFn(limit), strings.TrimSpace(input.Query))
	if err != nil {
		return nil, err
	}
	return packToMap(pack), nil
}

func (a *ReadOnlyAdapter) retrieveMemory(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input retrieveLaneInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	cfg := a.laneConfig()
	pack, err := contextengine.RetrieveMemory(ctx, cfg, a.memoryQueryFn(limit), strings.TrimSpace(input.Query))
	if err != nil {
		return nil, err
	}
	return packToMap(pack), nil
}

func (a *ReadOnlyAdapter) retrieveContext(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input retrieveLaneInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	cfg := a.laneConfig()
	pack, err := contextengine.RetrieveContext(ctx, cfg, a.contextQueryFn(), strings.TrimSpace(input.Query))
	if err != nil {
		return nil, err
	}
	return packToMap(pack), nil
}

func (a *ReadOnlyAdapter) retrieveTask(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input retrieveLaneInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	cfg := a.laneConfig()
	query := strings.TrimSpace(input.Query)
	pack, err := contextengine.RetrieveTask(ctx, cfg, a.taskQueryFn(), a.taskListFn(query), strings.TrimSpace(input.TaskID), query)
	if err != nil {
		return nil, err
	}
	return packToMap(pack), nil
}

func (a *ReadOnlyAdapter) gatherContext(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input gatherContextInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	cfg := a.laneConfig()
	sourceProfiles := contextengine.NormalizeSourceProfiles(input.SourceProfiles)
	req := contextengine.GatherContextRequest{
		Query:            strings.TrimSpace(input.Query),
		Goal:             strings.TrimSpace(input.Goal),
		TaskID:           strings.TrimSpace(input.TaskID),
		TaskType:         strings.TrimSpace(input.TaskType),
		SourceProfiles:   sourceProfiles,
		RequiredEvidence: cleanStringList(input.RequiredEvidence),
		Limit:            limit,
		Lanes:            parseGatherLanes(input.Lanes),
		Budget: contextengine.ContextBudget{
			MaxSources:      limit,
			MaxContextChars: input.MaxContextChars,
		},
	}
	bundle, err := contextengine.GatherContext(ctx, cfg, contextengine.GatherContextDeps{
		CodeSearch:    a.codeSearchFnForTaskWithRequired(limit, input.TaskType, req.RequiredEvidence, sourceProfiles...),
		MemoryQuery:   a.memoryQueryFnForStatuses(limit, parseMemoryClaimStatuses(input.MemoryStatuses)),
		ContextQuery:  a.contextQueryFn(),
		ContextPacks:  a.contextPacksFn(limit),
		TaskQuery:     a.taskQueryFn(),
		TaskList:      a.taskListFn(req.Query),
		SessionRecall: a.sessionRecallFn(limit),
		Staleness:     a.stalenessLookupFn(),
	}, req)
	if err != nil {
		return nil, err
	}
	responseMode := strings.TrimSpace(input.ResponseMode)
	if strings.EqualFold(responseMode, "answer_surface") ||
		strings.EqualFold(responseMode, "compact") {
		return contextBundleAnswerSurfaceToMap(bundle), nil
	}
	return contextBundleToMap(bundle), nil
}

func (a *ReadOnlyAdapter) retrieveMixed(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input retrieveLaneInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	cfg := a.laneConfig()
	query := strings.TrimSpace(input.Query)
	pack, err := contextengine.RetrieveMixed(
		ctx, cfg,
		a.codeSearchFn(limit),
		a.memoryQueryFn(limit),
		a.contextQueryFn(),
		a.taskQueryFn(),
		a.taskListFn(query),
		strings.TrimSpace(input.TaskID),
		query,
	)
	if err != nil {
		return nil, err
	}
	return packToMap(pack), nil
}

// loadEvidenceRef resolves a typed EvidenceRef and returns its bounded body.
func (a *ReadOnlyAdapter) loadEvidenceRef(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input loadEvidenceRefInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	refStr := strings.TrimSpace(input.Ref)
	if refStr == "" {
		return nil, fmt.Errorf("ref is required")
	}
	if input.MaxTokens <= 0 {
		input.MaxTokens = defaultLoadEvidenceRefMaxTokens
	}
	ref, err := contextengine.ParseEvidenceRef(refStr)
	if err != nil {
		return map[string]any{
			"ref":    refStr,
			"error":  err.Error(),
			"loaded": false,
		}, nil
	}
	switch ref.Type {
	case contextengine.RefTypePath:
		out, err := a.loadFile(mustJSON(map[string]any{"path": ref.Ref}))
		return boundLoadedEvidenceRef(refStr, out, input.MaxTokens), err
	case contextengine.RefTypeSymbol:
		out, err := a.loadSymbolEvidence(ctx, refStr, ref.Ref)
		return boundLoadedEvidenceRef(refStr, out, input.MaxTokens), err
	case contextengine.RefTypeTask:
		out, err := a.loadTaskEvidence(ctx, refStr, ref.Ref)
		return boundLoadedEvidenceRef(refStr, out, input.MaxTokens), err
	case contextengine.RefTypeSession:
		out, err := a.loadSessionEvidence(ctx, refStr, ref.Ref)
		return boundLoadedEvidenceRef(refStr, out, input.MaxTokens), err
	case contextengine.RefTypeNote:
		out, err := a.readNote(mustJSON(map[string]any{"path": ref.Ref}))
		return boundLoadedEvidenceRef(refStr, out, input.MaxTokens), err
	case contextengine.RefTypeArtifact, contextengine.RefTypeTrajectory:
		out, err := a.loadArtifact(ctx, mustJSON(map[string]any{"handle": refStr}))
		return boundLoadedEvidenceRef(refStr, out, input.MaxTokens), err
	case contextengine.RefTypeMemoryClaim:
		if a.ceStore == nil {
			return map[string]any{"ref": refStr, "loaded": false, "error": "contextengine store unavailable"}, nil
		}
		claim, err := a.ceStore.GetClaim(ctx, ref.Ref)
		if err != nil {
			return map[string]any{"ref": refStr, "loaded": false, "error": err.Error()}, nil
		}
		return boundLoadedEvidenceRef(refStr, map[string]any{
			"ref":    refStr,
			"loaded": true,
			"claim":  claim,
		}, input.MaxTokens), nil
	case contextengine.RefTypeEvent:
		out, err := a.loadEventEvidence(ctx, refStr, ref.Ref)
		return boundLoadedEvidenceRef(refStr, out, input.MaxTokens), err
	case contextengine.RefTypeRun:
		out, err := a.loadTrajectory(ctx, ref.Ref)
		return boundLoadedEvidenceRef(refStr, markLoadedEvidenceRef(refStr, "run", out), input.MaxTokens), err
	case contextengine.RefTypeToolCall:
		out, err := a.loadToolCallEvidence(ctx, refStr, ref.Ref)
		return boundLoadedEvidenceRef(refStr, out, input.MaxTokens), err
	case contextengine.RefTypeCommit:
		return map[string]any{
			"ref":    refStr,
			"loaded": false,
			"error":  "commit refs are recognized but no git object loader is configured in the read-only adapter",
		}, nil
	default:
		return map[string]any{
			"ref":    refStr,
			"loaded": false,
			"error":  "unsupported ref type for load_evidence_ref",
		}, nil
	}
}

func (a *ReadOnlyAdapter) loadTaskEvidence(ctx context.Context, refStr, taskID string) (map[string]any, error) {
	if a.taskStore == nil {
		return map[string]any{"ref": refStr, "loaded": false, "error": "task store unavailable"}, nil
	}
	task, err := a.taskStore.Get(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return map[string]any{"ref": refStr, "loaded": false, "error": err.Error()}, nil
	}
	taskContext := adapters.ConvertTask(task)
	return map[string]any{
		"ref":          refStr,
		"loaded":       true,
		"task":         task,
		"task_context": taskContext,
	}, nil
}

func (a *ReadOnlyAdapter) loadSessionEvidence(ctx context.Context, refStr, sessionID string) (map[string]any, error) {
	if strings.TrimSpace(a.cfg.Storage.Root) == "" {
		return map[string]any{"ref": refStr, "loaded": false, "error": "storage root unavailable"}, nil
	}
	store, err := sessions.Open(ctx, a.cfg.Storage.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	session, err := store.Get(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return map[string]any{"ref": refStr, "loaded": false, "error": err.Error()}, nil
	}
	turns, _ := store.GetTurns(ctx, session.ID, sessions.TurnListOptions{Limit: 20})
	chunks, _ := store.GetChunks(ctx, session.ID, 5)
	return map[string]any{
		"ref":     refStr,
		"loaded":  true,
		"session": session,
		"turns":   turns,
		"chunks":  chunks,
	}, nil
}

func (a *ReadOnlyAdapter) loadSymbolEvidence(ctx context.Context, refStr, symbol string) (map[string]any, error) {
	if strings.TrimSpace(a.cfg.Storage.Root) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return map[string]any{"ref": refStr, "loaded": false, "error": "repo index unavailable"}, nil
	}
	store, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	output, err := repoquery.NewQueryService(repoindex.NewQueryEngine(store)).SearchWithProjection(ctx, repoquery.SearchRequest{
		Query: strings.TrimSpace(symbol),
		Limit: 12,
	})
	if err != nil {
		return nil, err
	}
	anchors := make([]map[string]any, 0, len(output.Anchors))
	for _, anchor := range output.Anchors {
		anchors = append(anchors, map[string]any{
			"path":        anchor.Path,
			"symbol_name": anchor.SymbolName,
			"line_hint":   anchor.LineHint,
			"score":       anchor.Score,
			"summary":     anchor.Summary,
			"excerpt":     a.repoAnchorExcerpt(anchor),
		})
	}
	return map[string]any{
		"ref":     refStr,
		"loaded":  len(anchors) > 0,
		"symbol":  symbol,
		"anchors": anchors,
	}, nil
}

func (a *ReadOnlyAdapter) loadEventEvidence(ctx context.Context, refStr, eventRef string) (map[string]any, error) {
	if strings.Contains(eventRef, ":") {
		if out, err := a.loadTrajectoryEvent(ctx, eventRef); err == nil {
			if event := out["event"]; event != nil {
				return markLoadedEvidenceRef(refStr, "event", out), nil
			}
		}
	}
	if a.ceStore != nil {
		workspaceID := ""
		if strings.TrimSpace(a.workspaceRoot) != "" {
			workspaceID = ws.ID(a.workspaceRoot)
		}
		events, err := a.ceStore.ListEvents(ctx, ctxengstore.EventFilter{WorkspaceID: workspaceID, Limit: 500})
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			if strings.TrimSpace(event.ID) == strings.TrimSpace(eventRef) {
				return map[string]any{"ref": refStr, "loaded": true, "event": event}, nil
			}
		}
	}
	return map[string]any{"ref": refStr, "loaded": false, "error": "event not found"}, nil
}

func (a *ReadOnlyAdapter) loadToolCallEvidence(ctx context.Context, refStr, toolCallRef string) (map[string]any, error) {
	if strings.TrimSpace(a.cfg.Storage.Root) == "" {
		return map[string]any{"ref": refStr, "loaded": false, "error": "storage root unavailable"}, nil
	}
	store, err := trajectory.Open(ctx, a.cfg.Storage.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	workspaceID := ""
	if strings.TrimSpace(a.workspaceRoot) != "" {
		workspaceID = ws.ID(a.workspaceRoot)
	}
	var events []trajectory.Event
	if strings.Contains(toolCallRef, ":") {
		parts := strings.SplitN(toolCallRef, ":", 2)
		events, err = store.ListEvents(ctx, trajectory.EventFilter{
			TrajectoryID: strings.TrimSpace(parts[0]),
			Kind:         trajectory.EventKindToolCall,
			Limit:        200,
		})
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			if strings.TrimSpace(event.ID) == strings.TrimSpace(parts[1]) {
				return map[string]any{"ref": refStr, "loaded": true, "tool_call": event}, nil
			}
		}
	} else if workspaceID != "" {
		trajs, err := store.ListTrajectories(ctx, trajectory.ListFilter{WorkspaceID: workspaceID, Limit: 200})
		if err != nil {
			return nil, err
		}
		for _, traj := range trajs {
			events, err = store.ListEvents(ctx, trajectory.EventFilter{
				TrajectoryID: traj.ID,
				Kind:         trajectory.EventKindToolCall,
				Limit:        200,
			})
			if err != nil {
				continue
			}
			for _, event := range events {
				if strings.TrimSpace(event.ID) == strings.TrimSpace(toolCallRef) {
					return map[string]any{"ref": refStr, "loaded": true, "tool_call": event, "trajectory_id": traj.ID}, nil
				}
			}
		}
	}
	return map[string]any{"ref": refStr, "loaded": false, "error": "tool call not found"}, nil
}

func markLoadedEvidenceRef(refStr, key string, out map[string]any) map[string]any {
	if out == nil {
		out = map[string]any{}
	}
	out["ref"] = refStr
	if _, ok := out["loaded"]; !ok {
		out["loaded"] = out[key] != nil
	}
	return out
}

func boundLoadedEvidenceRef(refStr string, out map[string]any, maxTokens int) map[string]any {
	if out == nil {
		out = map[string]any{}
	}
	generic := jsonGenericMap(out)
	if _, ok := generic["ref"]; !ok {
		generic["ref"] = refStr
	}
	if _, ok := generic["loaded"]; !ok {
		generic["loaded"] = true
	}
	maxChars := maxTokens * 4
	if maxChars <= 0 {
		maxChars = defaultLoadEvidenceRefMaxTokens * 4
		maxTokens = defaultLoadEvidenceRefMaxTokens
	}
	body, err := json.Marshal(generic)
	if err != nil || len(body) <= maxChars {
		return generic
	}
	generic["truncated"] = true
	generic["max_tokens"] = maxTokens
	truncateMapStrings(generic, maxInt(128, maxChars/8))
	body, err = json.Marshal(generic)
	if err == nil && len(body) <= maxChars {
		return generic
	}
	return compactLoadedEvidenceRef(refStr, generic, len(body), maxTokens)
}

func jsonGenericMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	body, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]any, len(in))
		for key, value := range in {
			out[key] = value
		}
		return out
	}
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil || generic == nil {
		return map[string]any{}
	}
	return generic
}

func compactLoadedEvidenceRef(refStr string, generic map[string]any, omittedBytes int, maxTokens int) map[string]any {
	out := map[string]any{
		"ref":           refStr,
		"truncated":     true,
		"max_tokens":    maxTokens,
		"omitted_bytes": omittedBytes,
	}
	for _, key := range []string{"loaded", "type", "error"} {
		if value, ok := generic[key]; ok {
			out[key] = value
		}
	}
	return out
}

func truncateMapStrings(value any, maxChars int) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if text, ok := item.(string); ok && len(text) > maxChars {
				typed[key] = text[:maxChars]
				continue
			}
			truncateMapStrings(item, maxChars)
		}
	case []any:
		for _, item := range typed {
			truncateMapStrings(item, maxChars)
		}
	}
}

// packToMap renders an EvidencePack as a JSON-friendly map.
func packToMap(pack contextengine.EvidencePack) map[string]any {
	nodes := make([]map[string]any, 0, len(pack.Nodes))
	for _, node := range pack.Nodes {
		nodes = append(nodes, map[string]any{
			"id":         node.ID,
			"node_type":  string(node.NodeType),
			"ref":        contextengine.FormatEvidenceRef(node.Ref),
			"ref_type":   string(node.Ref.Type),
			"ref_value":  node.Ref.Ref,
			"statement":  node.Statement,
			"confidence": node.Confidence,
			"grounding":  string(node.Grounding),
			"metadata":   node.Metadata,
		})
	}
	return map[string]any{
		"id":           pack.ID,
		"workspace_id": pack.WorkspaceID,
		"query":        pack.Query,
		"lane":         string(pack.Lane),
		"nodes":        nodes,
		"telemetry": map[string]any{
			"duration_ms": pack.Telemetry.DurationMs,
			"lanes_fused": pack.Telemetry.LanesFused,
		},
		"metadata": pack.Metadata,
	}
}

func parseGatherLanes(input []string) []contextengine.EvidenceLane {
	if len(input) == 0 {
		return nil
	}
	lanes := make([]contextengine.EvidenceLane, 0, len(input))
	for _, raw := range input {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "code":
			lanes = append(lanes, contextengine.LaneCode)
		case "memory":
			lanes = append(lanes, contextengine.LaneMemory)
		case "context":
			lanes = append(lanes, contextengine.LaneContext)
		case "task":
			lanes = append(lanes, contextengine.LaneTask)
		case "mixed":
			lanes = append(lanes, contextengine.LaneMixed)
		}
	}
	return lanes
}

func contextBundleToMap(bundle contextengine.ContextBundle) map[string]any {
	body, _ := json.Marshal(bundle)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return out
}

func contextBundleAnswerSurfaceToMap(bundle contextengine.ContextBundle) map[string]any {
	answerSeed := buildContextAnswerSeed(bundle)
	pathSet := buildContextAnswerPathSet(bundle)
	copyable := contextBundleAnswerSurfaceCopyable(bundle)
	out := map[string]any{
		"schema_version":    "context_answer_surface/v2",
		"id":                bundle.ID,
		"workspace_id":      bundle.WorkspaceID,
		"query":             bundle.Query,
		"goal":              bundle.Goal,
		"status":            bundle.Status,
		"answerable":        bundle.Answerable,
		"summary":           bundle.Summary,
		"answer_contract":   map[string]any{"mode": contextAnswerContractMode(bundle), "copy_answer_seed": copyable, "drop_only_if_loaded_ref_disproves": true},
		"answer_seed":       answerSeed,
		"categories":        bundle.Categories,
		"integration_edges": bundle.IntegrationEdges,
		"coverage_report":   bundle.CoverageReport,
		"path_set":          pathSet,
		"facts_to_copy":     buildContextFactsToCopy(bundle),
		"load_queue":        buildContextLoadQueue(bundle),
		"selected_paths":    bundle.SelectedPaths,
		"answer_candidates": bundle.AnswerCandidates,
		"facts":             bundle.Facts,
		"missing":           bundle.Missing,
		"conflicts":         bundle.Conflicts,
		"source_coverage":   bundle.SourceCoverage,
		"telemetry": map[string]any{
			"raw_context_chars":     bundle.Telemetry.RawContextChars,
			"emitted_context_chars": bundle.Telemetry.EmittedContextChars,
			"omitted_context_items": bundle.Telemetry.OmittedContextItems,
		},
	}
	if bundle.Certificate != nil {
		out["certificate"] = map[string]any{
			"id":                   bundle.Certificate.ID,
			"status":               bundle.Certificate.Status,
			"required_evidence_ok": bundle.Certificate.RequiredEvidenceOK,
			"unsupported_facts":    bundle.Certificate.UnsupportedFacts,
			"stale_evidence_ids":   bundle.Certificate.StaleEvidenceIDs,
			"missing_evidence":     bundle.Certificate.MissingEvidence,
			"conflict_ids":         bundle.Certificate.ConflictIDs,
		}
	}
	return out
}

func contextBundleAnswerSurfaceCopyable(bundle contextengine.ContextBundle) bool {
	if !bundle.Answerable || bundle.Certificate == nil {
		return false
	}
	if bundle.Certificate.Status != contextengine.ContextCertificateStatusCertified || !bundle.Certificate.RequiredEvidenceOK {
		return false
	}
	if len(bundle.Missing) > 0 || len(bundle.Conflicts) > 0 {
		return false
	}
	if bundle.CoverageReport != nil && len(bundle.CoverageReport.Missing) > 0 {
		return false
	}
	return len(bundle.SelectedPaths) > 0 || len(bundle.AnswerCandidates) > 0 || len(bundle.Facts) > 0
}

func contextAnswerContractMode(bundle contextengine.ContextBundle) string {
	if len(bundle.Categories) > 0 || len(bundle.IntegrationEdges) > 0 {
		return "repo_subsystem_map"
	}
	return "repo_grounded_file_set"
}

func buildContextAnswerSeed(bundle contextengine.ContextBundle) map[string]any {
	paths := make([]string, 0, len(bundle.SelectedPaths))
	for _, selected := range bundle.SelectedPaths {
		if path := strings.TrimSpace(selected.Path); path != "" {
			paths = append(paths, path)
		}
	}
	facts := make([]string, 0, len(bundle.Facts))
	for _, fact := range bundle.Facts {
		if text := strings.TrimSpace(fact.Fact); text != "" {
			facts = append(facts, text)
		}
	}
	symbols := make([]string, 0)
	for _, candidate := range bundle.AnswerCandidates {
		if strings.EqualFold(strings.TrimSpace(candidate.Kind), "symbol") && strings.TrimSpace(candidate.Value) != "" {
			symbols = append(symbols, strings.TrimSpace(candidate.Value))
		}
	}
	return map[string]any{
		"summary":    bundle.Summary,
		"paths":      paths,
		"symbols":    symbols,
		"facts":      facts,
		"categories": buildContextAnswerSeedCategories(bundle),
	}
}

func buildContextAnswerSeedCategories(bundle contextengine.ContextBundle) []map[string]any {
	out := make([]map[string]any, 0, len(bundle.Categories))
	for _, category := range bundle.Categories {
		out = append(out, map[string]any{
			"name":    category.Name,
			"role":    category.Role,
			"paths":   append([]string(nil), category.Paths...),
			"signals": append([]string(nil), category.Signals...),
		})
	}
	return out
}

func buildContextAnswerPathSet(bundle contextengine.ContextBundle) map[string]any {
	must := make([]map[string]any, 0, len(bundle.SelectedPaths))
	for _, selected := range bundle.SelectedPaths {
		item := map[string]any{
			"path":         selected.Path,
			"rank":         selected.Rank,
			"confidence":   selected.Confidence,
			"reason":       selected.Reason,
			"evidence_ids": append([]string(nil), selected.EvidenceIDs...),
			"refs":         selected.Refs,
			"load_ref":     selectedPathLoadRef(selected),
		}
		if role := selectedPathMetadataString(selected.Metadata, "candidate_role"); role != "" {
			item["role"] = role
		}
		if profile := selectedPathMetadataString(selected.Metadata, "source_profile"); profile != "" {
			item["source_profile"] = profile
		}
		if kind := selectedPathMetadataString(selected.Metadata, "file_kind"); kind != "" {
			item["file_kind"] = kind
		}
		if count := len(selected.EvidenceIDs); count > 0 {
			item["support_count"] = count
		}
		must = append(must, item)
	}
	return map[string]any{
		"must":       must,
		"supporting": []map[string]any{},
		"maybe":      []map[string]any{},
	}
}

func buildContextFactsToCopy(bundle contextengine.ContextBundle) []map[string]any {
	out := make([]map[string]any, 0, len(bundle.Facts))
	for _, fact := range bundle.Facts {
		if strings.TrimSpace(fact.Fact) == "" {
			continue
		}
		loadRefs := make([]string, 0, len(fact.Refs))
		for _, ref := range fact.Refs {
			if formatted := contextengine.FormatEvidenceRef(ref); formatted != "" {
				loadRefs = append(loadRefs, formatted)
			}
		}
		out = append(out, map[string]any{
			"fact":         fact.Fact,
			"refs":         fact.Refs,
			"load_refs":    loadRefs,
			"evidence_ids": append([]string(nil), fact.EvidenceIDs...),
			"confidence":   fact.Confidence,
			"status":       fact.Status,
		})
	}
	return out
}

func buildContextLoadQueue(bundle contextengine.ContextBundle) []map[string]any {
	out := make([]map[string]any, 0, len(bundle.SelectedPaths))
	seen := map[string]struct{}{}
	for _, selected := range bundle.SelectedPaths {
		ref := selectedPathLoadRef(selected)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, map[string]any{
			"ref":    ref,
			"path":   selected.Path,
			"reason": selected.Reason,
		})
	}
	return out
}

func selectedPathLoadRef(selected contextengine.ContextSelectedPath) string {
	for _, ref := range selected.Refs {
		if ref.Type == contextengine.RefTypePath && strings.TrimSpace(ref.Ref) != "" {
			return contextengine.FormatEvidenceRef(ref)
		}
	}
	for _, ref := range selected.Refs {
		if formatted := contextengine.FormatEvidenceRef(ref); formatted != "" {
			return formatted
		}
	}
	if strings.TrimSpace(selected.Path) != "" {
		return contextengine.FormatEvidenceRef(contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: strings.TrimSpace(selected.Path)})
	}
	return ""
}

func selectedPathMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
