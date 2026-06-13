package env

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextengine/adapters"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
	ctxengstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
	"github.com/joshka0/foxctl/internal/storage/vector"
)

// retrieveLaneInput is the shared input shape for the 5 retrieval composite tools.
type retrieveLaneInput struct {
	Query          string   `json:"query"`
	Limit          int      `json:"limit,omitempty"`
	TaskID         string   `json:"task_id,omitempty"`
	SessionID      string   `json:"session_id,omitempty"`
	MemoryStatuses []string `json:"memory_statuses,omitempty"`
}

// gatherContextInput is the input shape for gather_context.
type gatherContextInput struct {
	Query                string                              `json:"query"`
	Goal                 string                              `json:"goal,omitempty"`
	RequiredEvidence     []string                            `json:"required_evidence,omitempty"`
	CoverageRequirements []contextengine.CoverageRequirement `json:"coverage_requirements,omitempty"`
	Limit                int                                 `json:"limit,omitempty"`
	TaskID               string                              `json:"task_id,omitempty"`
	TaskType             string                              `json:"task_type,omitempty"`
	SourceProfiles       []string                            `json:"source_profiles,omitempty"`
	Languages            []string                            `json:"languages,omitempty"`
	PathPrefixes         []string                            `json:"path_prefixes,omitempty"`
	ExcludedPaths        []string                            `json:"excluded_paths,omitempty"`
	MemoryStatuses       []string                            `json:"memory_statuses,omitempty"`
	Lanes                []string                            `json:"lanes,omitempty"`
	MaxContextChars      int                                 `json:"max_context_chars,omitempty"`
	ResponseMode         string                              `json:"response_mode,omitempty"`
	GraphMode            string                              `json:"graph_mode,omitempty"`
}

type gatherContextOptions struct {
	MemoryCoverageRepair bool
}

type codeSearchRequestOptions struct {
	Languages        []string
	PathPrefixes     []string
	ExcludedPaths    []string
	RequiredEvidence []string
	IncludeTests     bool
}

// loadEvidenceRefInput is the input shape for load_evidence_ref.
type loadEvidenceRefInput struct {
	Ref       string `json:"ref"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

type aggregateEvidenceRefsInput struct {
	Query                string                              `json:"query"`
	Refs                 []string                            `json:"refs"`
	RequiredEvidence     []string                            `json:"required_evidence,omitempty"`
	CoverageRequirements []contextengine.CoverageRequirement `json:"coverage_requirements,omitempty"`
	MaxRefs              int                                 `json:"max_refs,omitempty"`
	MaxTextChars         int                                 `json:"max_text_chars,omitempty"`
	MaxTokensPerRef      int                                 `json:"max_tokens_per_ref,omitempty"`
}

type evidenceLedgerInput struct {
	Query                string                              `json:"query"`
	Refs                 []string                            `json:"refs"`
	RequiredEvidence     []string                            `json:"required_evidence,omitempty"`
	CoverageRequirements []contextengine.CoverageRequirement `json:"coverage_requirements,omitempty"`
	MaxRefs              int                                 `json:"max_refs,omitempty"`
	MaxTextChars         int                                 `json:"max_text_chars,omitempty"`
	MaxTokensPerRef      int                                 `json:"max_tokens_per_ref,omitempty"`
}

const (
	defaultLoadEvidenceRefMaxTokens           = 4096
	defaultGatherMemoryContextMaxContextChars = 24000
	defaultContextEvidenceDigestMaxClaims     = 6
	defaultContextEvidenceDigestMaxClaimChars = 360
	defaultAggregateEvidenceMaxRefs           = 12
	defaultAggregateEvidenceMaxTextChars      = 480
	defaultAggregateEvidenceLoadMaxTokens     = 1200
	defaultAggregateEvidenceScanMaxTokens     = 6000
)

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
	wsID := strings.TrimSpace(a.workspaceID)
	if wsID == "" && a.workspaceRoot != "" {
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
	return a.codeSearchFnForTaskWithRequired(limit, taskType, nil, codeSearchRequestOptions{}, sourceProfiles...)
}

func (a *ReadOnlyAdapter) codeSearchFnForTaskWithRequired(limit int, taskType string, requiredEvidence []string, options codeSearchRequestOptions, sourceProfiles ...contextengine.SourceProfile) contextengine.CodeSearchFunc {
	return func(ctx context.Context, query string) ([]contextengine.CodeSearchHit, error) {
		if strings.TrimSpace(a.workspaceRoot) == "" {
			return nil, nil
		}
		options = normalizeCodeSearchRequestOptions(options)
		normalizedTaskType, _ := normalizeCodeSearchTaskType(taskType)
		required := cleanStringList(requiredEvidence)
		options.IncludeTests = requiredEvidenceSuggestsTests(append([]string{query}, required...))
		providerTelemetry := newCodeSearchProviderTelemetry()
		liveBudget := newLocalProviderBudget(ctx, limit)
		liveHits, liveErr := timedRankedCodeSearchProvider(providerTelemetry, "live_overlay", func() ([]rankedCodeSearchHit, error) {
			return a.liveOverlayCodeSearchHits(ctx, query, required, limit, liveBudget)
		}, liveBudget)
		repoDocBudget := newLocalProviderBudget(ctx, limit)
		repoDocHits, repoDocErr := timedRankedCodeSearchProvider(providerTelemetry, "repo_docs", func() ([]rankedCodeSearchHit, error) {
			return a.repoDocsSearchHits(ctx, query, required, sourceProfiles, limit, options, repoDocBudget)
		}, repoDocBudget)
		repoHits, repoErr := timedCodeSearchProvider(providerTelemetry, "repo_index", func() ([]contextengine.CodeSearchHit, error) {
			return a.repoIndexCodeSearch(ctx, query, limit, options)
		})
		repoCoverageHits, repoCoverageErr := timedRankedCodeSearchProvider(providerTelemetry, "repo_index_coverage", func() ([]rankedCodeSearchHit, error) {
			return a.repoIndexCoverageSearchHits(ctx, required, limit, options)
		})
		var semanticFileHits []rankedCodeSearchHit
		var semanticFileErr error
		if codeSearchSemanticProviderEnabled(sourceProfiles) {
			semanticFileHits, semanticFileErr = timedRankedCodeSearchProvider(providerTelemetry, "semantic_file_index", func() ([]rankedCodeSearchHit, error) {
				return a.semanticFileIndexSearchHits(ctx, query, required, limit, options)
			})
		} else {
			recordCodeSearchProviderSkipped(providerTelemetry, "semantic_file_index", "semantic provider disabled")
		}
		coverageBudget := newLocalProviderBudget(ctx, limit)
		coverageHits, coverageErr := timedRankedCodeSearchProvider(providerTelemetry, "local_coverage", func() ([]rankedCodeSearchHit, error) {
			return a.localCoverageCodeSearchHits(ctx, query, required, limit, options, coverageBudget)
		}, coverageBudget)
		nonCodeBudget := newLocalProviderBudget(ctx, limit)
		nonCodeHits := timedRankedCodeSearchProviderNoError(providerTelemetry, "local_non_code_config_data", func() []rankedCodeSearchHit {
			return a.localNonCodeConfigDataSearchHits(ctx, query, required, sourceProfiles, limit, options, nonCodeBudget)
		}, nonCodeBudget)
		probeBudget := newLocalProviderBudget(ctx, limit)
		localHits, localErr := timedRankedCodeSearchProvider(providerTelemetry, "local_probe", func() ([]rankedCodeSearchHit, error) {
			return a.localCodeProbeSearch(ctx, query, normalizedTaskType, required, limit, options, probeBudget)
		}, probeBudget)
		buildTargetHits := timedRankedCodeSearchProviderNoError(providerTelemetry, "build_target", func() []rankedCodeSearchHit {
			return a.localBuildTargetCodeSearchHits(query)
		})

		cheapLimit := codeSearchCandidatePoolLimit(limit, normalizedTaskType, len(required), 0)
		cheapHits := mergeCodeSearchHitsWithOptions(cheapLimit, options, repoHits, liveHits, repoDocHits, repoCoverageHits, semanticFileHits, coverageHits, nonCodeHits, localHits, buildTargetHits)
		annotateCodeSearchHitCoverage(cheapHits, required)
		pathRepairHits := timedRankedCodeSearchProviderNoError(providerTelemetry, "local_required_path_repair", func() []rankedCodeSearchHit {
			return a.localRequiredPathRepairHits(required, cheapHits, limit)
		})
		definitionBudget := newLocalProviderBudget(ctx, limit)
		definitionRepairHits := timedRankedCodeSearchProviderNoError(providerTelemetry, "local_required_definition_repair", func() []rankedCodeSearchHit {
			return a.localRequiredDefinitionRepairHits(ctx, required, limit, options, definitionBudget)
		}, definitionBudget)
		testBudget := newLocalProviderBudget(ctx, limit)
		testCoverageHits := timedRankedCodeSearchProviderNoError(providerTelemetry, "local_test_coverage_repair", func() []rankedCodeSearchHit {
			return a.localTestCoverageRepairHits(ctx, query, required, limit, options, testBudget)
		}, testBudget)
		routeBudget := newLocalProviderBudget(ctx, limit)
		routeActionHits := timedRankedCodeSearchProviderNoError(providerTelemetry, "local_route_action_repair", func() []rankedCodeSearchHit {
			return a.localRouteActionRepairHits(ctx, query, required, limit, options, routeBudget)
		}, routeBudget)
		routeFamilyHits := timedRankedCodeSearchProviderNoError(providerTelemetry, "local_route_family_closure", func() []rankedCodeSearchHit {
			return a.localRouteFamilyClosureHits(cheapHits, limit, options)
		})
		cheapHits = mergeCodeSearchHitsWithOptions(cheapLimit, options, repoHits, liveHits, repoDocHits, repoCoverageHits, semanticFileHits, coverageHits, nonCodeHits, localHits, buildTargetHits, pathRepairHits, definitionRepairHits, testCoverageHits, routeActionHits, routeFamilyHits)
		annotateCodeSearchHitCoverage(cheapHits, required)
		needEnsemble := codeSearchNeedsExpensiveFallback(cheapHits, limit, normalizedTaskType, required)
		var ensembleHits []rankedCodeSearchHit
		var ensembleErr error
		if needEnsemble && codeSearchShouldSkipEarlyEnsemble(normalizedTaskType, required) {
			recordCodeSearchProviderSkipped(providerTelemetry, "code_search_ensemble", "deferred for local coverage repair")
		} else if needEnsemble {
			ensembleHits, ensembleErr = timedRankedCodeSearchProvider(providerTelemetry, "code_search_ensemble", func() ([]rankedCodeSearchHit, error) {
				return a.codeSearchEnsembleHits(ctx, query, normalizedTaskType, limit, required, sourceProfiles)
			})
		} else {
			recordCodeSearchProviderSkipped(providerTelemetry, "code_search_ensemble", "cheap providers satisfied coverage")
		}

		postEnsembleHits := mergeCodeSearchHitsWithOptions(maxInt(limit*6, limit), options, repoHits, liveHits, repoDocHits, repoCoverageHits, semanticFileHits, coverageHits, nonCodeHits, localHits, buildTargetHits, pathRepairHits, definitionRepairHits, testCoverageHits, routeActionHits, routeFamilyHits, ensembleHits)
		annotateCodeSearchHitCoverage(postEnsembleHits, required)
		needDeepFallback := codeSearchNeedsDeepFallback(postEnsembleHits, limit, normalizedTaskType, required)
		var lexicalHits []rankedCodeSearchHit
		var lexicalErr error
		if needDeepFallback {
			lexicalBudget := newLocalProviderBudget(ctx, limit)
			lexicalHits, lexicalErr = timedRankedCodeSearchProvider(providerTelemetry, "local_lexical", func() ([]rankedCodeSearchHit, error) {
				return a.localLexicalCodeSearch(ctx, query, limit, options, lexicalBudget)
			}, lexicalBudget)
		} else {
			recordCodeSearchProviderSkipped(providerTelemetry, "local_lexical", "candidate set satisfied coverage")
		}

		wideHits := mergeCodeSearchHitsWithOptions(maxInt(limit*6, limit), options, repoHits, liveHits, repoDocHits, repoCoverageHits, semanticFileHits, coverageHits, nonCodeHits, localHits, buildTargetHits, pathRepairHits, definitionRepairHits, testCoverageHits, routeActionHits, routeFamilyHits, ensembleHits, lexicalHits)
		annotateCodeSearchHitCoverage(wideHits, required)
		importClosureHits := timedRankedCodeSearchProviderNoError(providerTelemetry, "local_import_mount_closure", func() []rankedCodeSearchHit {
			return a.localImportMountClosureHits(ctx, wideHits, limit, options)
		})
		moduleEntryHits := timedRankedCodeSearchProviderNoError(providerTelemetry, "local_module_entrypoint_closure", func() []rankedCodeSearchHit {
			return a.localModuleEntrypointClosureHits(wideHits, limit, options)
		})
		wideHits = mergeCodeSearchHitsWithOptions(maxInt(limit*6, limit), options, repoHits, liveHits, repoDocHits, repoCoverageHits, semanticFileHits, coverageHits, nonCodeHits, localHits, buildTargetHits, pathRepairHits, definitionRepairHits, testCoverageHits, routeActionHits, routeFamilyHits, ensembleHits, lexicalHits, importClosureHits, moduleEntryHits)
		annotateCodeSearchHitCoverage(wideHits, required)
		var relatedHits []rankedCodeSearchHit
		if codeSearchShouldRunRelatedFallback(wideHits, limit, normalizedTaskType, required) {
			relatedBudget := newLocalProviderBudget(ctx, limit)
			relatedHits = timedRankedCodeSearchProviderNoError(providerTelemetry, "local_related", func() []rankedCodeSearchHit {
				return a.localRelatedCodeSearchHits(ctx, query, required, wideHits, limit, options, relatedBudget)
			}, relatedBudget)
		} else {
			recordCodeSearchProviderSkipped(providerTelemetry, "local_related", "candidate set satisfied coverage")
		}
		companionHits := timedRankedCodeSearchProviderNoError(providerTelemetry, "local_companion_closure", func() []rankedCodeSearchHit {
			return a.localCompanionClosureHits(query, wideHits, limit, options)
		})
		baseHits := mergeCodeSearchHitsWithOptions(limit, options, repoHits, liveHits, repoDocHits, repoCoverageHits, semanticFileHits, coverageHits, nonCodeHits, localHits, buildTargetHits, pathRepairHits, definitionRepairHits, testCoverageHits, routeActionHits, routeFamilyHits, ensembleHits, lexicalHits, importClosureHits, moduleEntryHits, relatedHits, companionHits)
		annotateCodeSearchHitCoverage(baseHits, required)
		var closureHits []rankedCodeSearchHit
		if isSubsystemMapTask(normalizedTaskType) && codeSearchNeedsSubsystemClosure(baseHits, limit, required) {
			closureBudget := newLocalProviderBudget(ctx, limit)
			closureHits = timedRankedCodeSearchProviderNoError(providerTelemetry, "subsystem_sibling_closure", func() []rankedCodeSearchHit {
				return a.localSubsystemSiblingClosureHits(ctx, query, normalizedTaskType, required, baseHits, limit, options, closureBudget)
			}, closureBudget)
		} else {
			recordCodeSearchProviderSkipped(providerTelemetry, "subsystem_sibling_closure", "not needed for selected candidates")
		}
		candidateLimit := codeSearchCandidatePoolLimit(limit, normalizedTaskType, len(required), len(closureHits))
		if hits := mergeCodeSearchHitsWithOptions(candidateLimit, options, repoHits, liveHits, repoDocHits, repoCoverageHits, semanticFileHits, coverageHits, nonCodeHits, localHits, buildTargetHits, pathRepairHits, definitionRepairHits, testCoverageHits, routeActionHits, routeFamilyHits, ensembleHits, lexicalHits, importClosureHits, moduleEntryHits, relatedHits, companionHits, closureHits); len(hits) > 0 {
			hits = appendMissingRankedCodeSearchHits(hits, candidateLimit, options, moduleEntryHits)
			annotateCodeSearchHitCoverage(hits, required)
			return attachCodeSearchProviderTelemetry(hits, providerTelemetry, candidateLimit), nil
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
		if repoCoverageErr != nil {
			errs = append(errs, "repo index coverage: "+repoCoverageErr.Error())
		}
		if semanticFileErr != nil {
			errs = append(errs, "semantic file index: "+semanticFileErr.Error())
		}
		if coverageErr != nil {
			errs = append(errs, "local coverage: "+coverageErr.Error())
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

type codeSearchProviderTelemetryItem struct {
	Name       string
	DurationMS int64
	HitCount   int
	Paths      []string
	Error      string
	Skipped    bool
	Budget     map[string]any
}

func newCodeSearchProviderTelemetry() *[]codeSearchProviderTelemetryItem {
	items := make([]codeSearchProviderTelemetryItem, 0, 12)
	return &items
}

func timedRankedCodeSearchProvider(items *[]codeSearchProviderTelemetryItem, name string, fn func() ([]rankedCodeSearchHit, error), budgets ...*localProviderBudget) ([]rankedCodeSearchHit, error) {
	start := time.Now()
	hits, err := fn()
	recordCodeSearchProviderTelemetry(items, name, time.Since(start), len(hits), pathsFromRankedCodeSearchHits(hits), err, budgets...)
	return hits, err
}

func timedCodeSearchProvider(items *[]codeSearchProviderTelemetryItem, name string, fn func() ([]contextengine.CodeSearchHit, error)) ([]contextengine.CodeSearchHit, error) {
	start := time.Now()
	hits, err := fn()
	recordCodeSearchProviderTelemetry(items, name, time.Since(start), len(hits), pathsFromCodeSearchHits(hits), err)
	return hits, err
}

func timedRankedCodeSearchProviderNoError(items *[]codeSearchProviderTelemetryItem, name string, fn func() []rankedCodeSearchHit, budgets ...*localProviderBudget) []rankedCodeSearchHit {
	start := time.Now()
	hits := fn()
	recordCodeSearchProviderTelemetry(items, name, time.Since(start), len(hits), pathsFromRankedCodeSearchHits(hits), nil, budgets...)
	return hits
}

func recordCodeSearchProviderTelemetry(items *[]codeSearchProviderTelemetryItem, name string, duration time.Duration, hitCount int, paths []string, err error, budgets ...*localProviderBudget) {
	if items == nil {
		return
	}
	item := codeSearchProviderTelemetryItem{
		Name:       strings.TrimSpace(name),
		DurationMS: duration.Milliseconds(),
		HitCount:   hitCount,
		Paths:      capCodeSearchTelemetryPaths(paths),
	}
	if err != nil {
		item.Error = err.Error()
	}
	for _, budget := range budgets {
		if budget != nil {
			item.Budget = budget.snapshot()
			break
		}
	}
	*items = append(*items, item)
}

func pathsFromRankedCodeSearchHits(hits []rankedCodeSearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = appendUniqueStringEnv(out, normalizeCodeSearchPath(hit.Hit.Path))
	}
	return out
}

func pathsFromCodeSearchHits(hits []contextengine.CodeSearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = appendUniqueStringEnv(out, normalizeCodeSearchPath(hit.Path))
	}
	return out
}

func capCodeSearchTelemetryPaths(paths []string) []string {
	paths = append([]string(nil), paths...)
	sort.Strings(paths)
	if len(paths) > 20 {
		return paths[:20]
	}
	return paths
}

func recordCodeSearchProviderSkipped(items *[]codeSearchProviderTelemetryItem, name string, reason string) {
	if items == nil {
		return
	}
	*items = append(*items, codeSearchProviderTelemetryItem{
		Name:    strings.TrimSpace(name),
		Error:   strings.TrimSpace(reason),
		Skipped: true,
	})
}

func attachCodeSearchProviderTelemetry(hits []contextengine.CodeSearchHit, items *[]codeSearchProviderTelemetryItem, candidateLimit int) []contextengine.CodeSearchHit {
	if len(hits) == 0 || items == nil || len(*items) == 0 {
		return hits
	}
	providers := make([]map[string]any, 0, len(*items))
	var totalDuration int64
	for _, item := range *items {
		entry := map[string]any{
			"name":        item.Name,
			"duration_ms": item.DurationMS,
			"hit_count":   item.HitCount,
		}
		if item.Error != "" {
			entry["error"] = item.Error
		}
		if len(item.Paths) > 0 {
			entry["paths"] = append([]string(nil), item.Paths...)
		}
		if item.Skipped {
			entry["skipped"] = true
		}
		if len(item.Budget) > 0 {
			entry["budget"] = item.Budget
			if capped, _ := item.Budget["capped"].(bool); capped {
				entry["capped"] = true
				if reason, _ := item.Budget["skip_reason"].(string); reason != "" {
					entry["skip_reason"] = reason
				}
			}
		}
		providers = append(providers, entry)
		totalDuration += item.DurationMS
	}
	if hits[0].Metadata == nil {
		hits[0].Metadata = map[string]any{}
	}
	hits[0].Metadata["code_search_provider_telemetry"] = map[string]any{
		"providers":         providers,
		"total_duration_ms": totalDuration,
		"merged_hit_count":  len(hits),
		"merged_paths":      pathsFromCodeSearchHits(hits),
		"candidate_limit":   candidateLimit,
	}
	return hits
}

func codeSearchCandidatePoolLimit(limit int, taskType string, requiredEvidenceCount int, closureCount int) int {
	if limit <= 0 {
		limit = 8
	}
	candidateLimit := limit
	if requiredEvidenceCount > 0 {
		candidateLimit = maxInt(candidateLimit, limit+requiredEvidenceCount*3)
	}
	if isSubsystemMapTask(taskType) {
		candidateLimit = maxInt(candidateLimit, maxInt(limit+closureCount, limit*2))
	}
	return minInt(candidateLimit, maxInt(limit, 32))
}

func codeSearchShouldSkipEarlyEnsemble(taskType string, requirements []string) bool {
	if !isSubsystemMapTask(taskType) {
		return false
	}
	for _, requirement := range cleanStringList(requirements) {
		if isPathShapedCoverageRequirement(requirement) {
			return true
		}
	}
	return false
}

func codeSearchNeedsExpensiveFallback(hits []contextengine.CodeSearchHit, limit int, taskType string, requirements []string) bool {
	if len(hits) == 0 {
		return true
	}
	requirements = cleanStringList(requirements)
	if len(requirements) > 0 && !codeSearchHitsCoverRequirements(hits, requirements) {
		return true
	}
	minHits := 2
	if isSubsystemMapTask(taskType) {
		minHits = minInt(maxInt(4, len(requirements)), maxInt(limit, 4))
	} else if strings.TrimSpace(taskType) == codeSearchTaskExecutionTrace || strings.TrimSpace(taskType) == codeSearchTaskRegistrationTrace {
		minHits = minInt(maxInt(3, len(requirements)/2), maxInt(limit, 3))
	}
	return len(hits) < minHits
}

func codeSearchNeedsDeepFallback(hits []contextengine.CodeSearchHit, limit int, taskType string, requirements []string) bool {
	if len(hits) == 0 {
		return true
	}
	requirements = cleanStringList(requirements)
	if len(requirements) > 0 && !codeSearchHitsCoverRequirements(hits, requirements) {
		return true
	}
	if strings.TrimSpace(taskType) == codeSearchTaskExecutionTrace && len(hits) < minInt(maxInt(limit/2, 3), limit) {
		return true
	}
	return false
}

func codeSearchShouldRunRelatedFallback(hits []contextengine.CodeSearchHit, limit int, taskType string, requirements []string) bool {
	if len(hits) == 0 {
		return false
	}
	if strings.TrimSpace(taskType) == codeSearchTaskExecutionTrace || strings.TrimSpace(taskType) == codeSearchTaskRegistrationTrace {
		return true
	}
	if len(hits) < minInt(maxInt(3, limit/2), maxInt(limit, 3)) {
		return true
	}
	if strings.TrimSpace(taskType) == codeSearchTaskChangeImpact && !codeSearchHitsCoverRequirements(hits, requirements) {
		return true
	}
	return false
}

func codeSearchNeedsSubsystemClosure(hits []contextengine.CodeSearchHit, limit int, requirements []string) bool {
	if len(hits) == 0 {
		return false
	}
	if len(hits) < minInt(maxInt(4, len(cleanStringList(requirements))), maxInt(limit, 4)) {
		return true
	}
	return len(cleanStringList(requirements)) > 0 && !codeSearchHitsCoverRequirements(hits, requirements)
}

func codeSearchHitsCoverRequirements(hits []contextengine.CodeSearchHit, requirements []string) bool {
	requirements = cleanStringList(requirements)
	if len(requirements) == 0 {
		return true
	}
	for _, requirement := range requirements {
		if !codeSearchHitsCoverRequirement(hits, requirement) {
			return false
		}
	}
	return true
}

func codeSearchHitsCoverRequirement(hits []contextengine.CodeSearchHit, requirement string) bool {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" {
		return true
	}
	if isPathShapedCoverageRequirement(requirement) {
		for _, hit := range hits {
			if codeSearchHitStrictlyCoversPathRequirement(hit, requirement) {
				return true
			}
		}
		return false
	}
	ids := codeSearchCoverageRequirementIDs("", "", requirement, []string{requirement})
	if len(ids) == 0 {
		return true
	}
	for _, hit := range hits {
		if stringSliceHas(codeSearchHitCoverageRequirementIDs(hit, []string{requirement}), ids[0]) {
			return true
		}
	}
	return false
}

func isPathShapedCoverageRequirement(requirement string) bool {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" {
		return false
	}
	return strings.ContainsAny(requirement, "/._-")
}

func codeSearchHitStrictlyCoversPathRequirement(hit contextengine.CodeSearchHit, requirement string) bool {
	needle := normalizedPathRequirementKey(requirement)
	if needle == "" {
		return false
	}
	values := []string{hit.Path, hit.Symbol}
	values = append(values, metadataStringSliceEnv(hit.Metadata, "matched_terms")...)
	for _, value := range values {
		if strings.Contains(normalizedPathRequirementKey(value), needle) {
			return true
		}
	}
	return false
}

func normalizedPathRequirementKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func codeSearchHitCoverageRequirementIDs(hit contextengine.CodeSearchHit, requirements []string) []string {
	out := metadataStringSliceEnv(hit.Metadata, "coverage_requirement_ids")
	out = appendUniqueStringsEnv(out, codeSearchCoverageRequirementIDs(hit.Path, hit.Symbol, hit.Snippet, requirements)...)
	if terms := metadataStringSliceEnv(hit.Metadata, "matched_terms"); len(terms) > 0 {
		out = appendUniqueStringsEnv(out, codeSearchCoverageRequirementIDs(hit.Path, hit.Symbol, strings.Join(terms, "\n"), requirements)...)
	}
	return out
}

func normalizeCodeSearchRequestOptions(options codeSearchRequestOptions) codeSearchRequestOptions {
	out := codeSearchRequestOptions{
		Languages:        make([]string, 0, len(options.Languages)),
		PathPrefixes:     make([]string, 0, len(options.PathPrefixes)),
		ExcludedPaths:    make([]string, 0, len(options.ExcludedPaths)+len(defaultCodeSearchExcludedPaths())),
		RequiredEvidence: cleanStringList(options.RequiredEvidence),
	}
	for _, language := range options.Languages {
		switch strings.TrimSpace(strings.ToLower(language)) {
		case "go", "golang":
			out.Languages = appendUniqueStringEnv(out.Languages, "go")
		case "typescript", "ts", "tsx":
			out.Languages = appendUniqueStringEnv(out.Languages, "typescript")
		case "javascript", "js", "jsx":
			out.Languages = appendUniqueStringEnv(out.Languages, "javascript")
		case "elixir", "ex", "exs":
			out.Languages = appendUniqueStringEnv(out.Languages, "elixir")
		case "csharp", "c#", "cs":
			out.Languages = appendUniqueStringEnv(out.Languages, "csharp")
		case "python", "py":
			out.Languages = appendUniqueStringEnv(out.Languages, "python")
		case "markdown", "md", "docs":
			out.Languages = appendUniqueStringEnv(out.Languages, "markdown")
		case "json":
			out.Languages = appendUniqueStringEnv(out.Languages, "json")
		case "yaml", "yml":
			out.Languages = appendUniqueStringEnv(out.Languages, "yaml")
		case "toml":
			out.Languages = appendUniqueStringEnv(out.Languages, "toml")
		}
	}
	for _, prefix := range options.PathPrefixes {
		prefix = normalizeCodeSearchPath(prefix)
		prefix = strings.Trim(prefix, "/")
		if prefix == "" || prefix == "." {
			continue
		}
		out.PathPrefixes = appendUniqueStringEnv(out.PathPrefixes, prefix)
	}
	for _, excluded := range defaultCodeSearchExcludedPaths() {
		out.ExcludedPaths = appendUniqueStringEnv(out.ExcludedPaths, excluded)
	}
	for _, excluded := range options.ExcludedPaths {
		excluded = normalizeCodeSearchPath(excluded)
		excluded = strings.Trim(excluded, "/")
		if excluded == "" || excluded == "." {
			continue
		}
		out.ExcludedPaths = appendUniqueStringEnv(out.ExcludedPaths, excluded)
	}
	return out
}

func defaultCodeSearchExcludedPaths() []string {
	return []string{
		".git",
		".next",
		".turbo",
		"_build",
		"artifacts",
		"coverage",
		"dist",
		"node_modules",
		"out",
		"repoindex",
		"vendor",
		"foxctl_web",
		"testdata",
		"fixtures",
	}
}

func codeSearchHitMatchesOptions(hit contextengine.CodeSearchHit, options codeSearchRequestOptions) bool {
	if codeSearchPathExcluded(hit.Path, options.ExcludedPaths) {
		return false
	}
	if !options.IncludeTests && isTestLikeCodeSearchPath(hit.Path) {
		return false
	}
	if isThirdPartyCodeSearchPath(hit.Path) && !codeSearchOptionsAllowThirdParty(options) {
		return false
	}
	if len(options.Languages) > 0 {
		language := strings.TrimSpace(hit.Language)
		if language == "" {
			language = languageFromPath(hit.Path)
		}
		if !stringSliceHas(options.Languages, language) {
			return false
		}
	}
	if len(options.PathPrefixes) > 0 {
		path := strings.Trim(normalizeCodeSearchPath(hit.Path), "/")
		matched := false
		for _, prefix := range options.PathPrefixes {
			prefix = strings.Trim(prefix, "/")
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func codeSearchPathExcluded(pathValue string, excludedPaths []string) bool {
	pathValue = strings.Trim(normalizeCodeSearchPath(pathValue), "/")
	if pathValue == "" || len(excludedPaths) == 0 {
		return false
	}
	for _, excluded := range excludedPaths {
		excluded = strings.Trim(normalizeCodeSearchPath(excluded), "/")
		if excluded == "" {
			continue
		}
		excluded = strings.TrimSuffix(excluded, "/")
		if pathValue == excluded || strings.HasPrefix(pathValue, excluded+"/") {
			return true
		}
		if !strings.Contains(excluded, "/") && pathHasSegment(pathValue, excluded) {
			return true
		}
		if ok, _ := filepath.Match(excluded, pathValue); ok {
			return true
		}
	}
	return false
}

func pathHasSegment(pathValue string, segment string) bool {
	segment = strings.Trim(strings.ToLower(segment), "/")
	if segment == "" {
		return false
	}
	for _, part := range strings.Split(strings.ToLower(strings.Trim(pathValue, "/")), "/") {
		if part == segment {
			return true
		}
	}
	return false
}

func codeSearchPathPrefixAllowed(pathValue string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	pathValue = strings.Trim(normalizeCodeSearchPath(pathValue), "/")
	for _, prefix := range prefixes {
		prefix = strings.Trim(normalizeCodeSearchPath(prefix), "/")
		if prefix == "" || pathValue == prefix || strings.HasPrefix(pathValue, prefix+"/") {
			return true
		}
	}
	return false
}

func codeSearchOptionsAllowThirdParty(options codeSearchRequestOptions) bool {
	for _, prefix := range options.PathPrefixes {
		if pathContainsThirdPartySegment(prefix) {
			return true
		}
	}
	return false
}

func isThirdPartyCodeSearchPath(pathValue string) bool {
	return pathContainsThirdPartySegment(normalizeCodeSearchPath(pathValue))
}

func pathContainsThirdPartySegment(pathValue string) bool {
	for _, part := range strings.Split(strings.Trim(pathValue, "/"), "/") {
		switch part {
		case "node_modules", "deps", "_build", "vendor", "Pods", ".gradle":
			return true
		}
	}
	return false
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
	addTerm := func(term string) {
		for _, variant := range subsystemClosureTermVariants(term) {
			if len(variant) < 4 || isGenericCodeSearchPathWord(variant) {
				continue
			}
			if _, ok := seen[variant]; ok {
				continue
			}
			seen[variant] = struct{}{}
			out = append(out, variant)
		}
	}
	for _, value := range values {
		parts := codeSearchCoverageParts(value)
		for _, part := range parts {
			addTerm(part)
		}
		for width := 2; width <= 3; width++ {
			for i := 0; i+width <= len(parts); i++ {
				addTerm(strings.Join(parts[i:i+width], ""))
			}
		}
	}
	sort.Strings(out)
	return out
}

func codeSearchCoverageParts(value string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(part string) {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			return
		}
		if _, ok := seen[part]; ok {
			return
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	for _, part := range splitCodeSearchProbe(value) {
		add(part)
	}
	for _, part := range splitCodeSearchProbePreserveCase(value) {
		for _, camel := range splitCamelCodeSearchTerm(part) {
			add(camel)
		}
	}
	return out
}

func splitCodeSearchProbePreserveCase(probe string) []string {
	return strings.FieldsFunc(strings.TrimSpace(probe), func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= 'A' && r <= 'Z':
			return false
		case r >= '0' && r <= '9':
			return false
		default:
			return true
		}
	})
}

func splitCamelCodeSearchTerm(term string) []string {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil
	}
	var out []string
	start := 0
	runes := []rune(term)
	for i := 1; i < len(runes); i++ {
		prev := runes[i-1]
		cur := runes[i]
		nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
		if (cur >= 'A' && cur <= 'Z' && ((prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') || nextLower)) ||
			(cur >= '0' && cur <= '9' && !(prev >= '0' && prev <= '9')) {
			if part := strings.TrimSpace(string(runes[start:i])); part != "" {
				out = append(out, part)
			}
			start = i
		}
	}
	if part := strings.TrimSpace(string(runes[start:])); part != "" {
		out = append(out, part)
	}
	if len(out) <= 1 {
		return []string{term}
	}
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

func codeSearchCoverageRequirementIDFallback(requirements []string) []string {
	out := make([]string, 0, len(requirements))
	for _, req := range cleanStringList(requirements) {
		if terms := codeSearchCoverageTerms(req); len(terms) > 0 {
			out = appendUniqueStringEnv(out, strings.Join(terms, "_"))
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

func (a *ReadOnlyAdapter) repoIndexCodeSearch(ctx context.Context, query string, limit int, options codeSearchRequestOptions) ([]contextengine.CodeSearchHit, error) {
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
	return a.repoIndexAnchorsToCodeHits(output.Anchors, options), nil
}

func (a *ReadOnlyAdapter) repoIndexCoverageSearchHits(ctx context.Context, requiredEvidence []string, limit int, options codeSearchRequestOptions) ([]rankedCodeSearchHit, error) {
	requirements := cleanStringList(requiredEvidence)
	if len(requirements) == 0 || strings.TrimSpace(a.cfg.Storage.Root) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	store, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	perRequirementLimit := 4
	if limit > 0 {
		perRequirementLimit = maxInt(2, minInt(6, limit/2))
	}
	var out []rankedCodeSearchHit
	var errs []string
	for idx, requirement := range requirements {
		query := strings.Join(codeSearchCoverageTerms(requirement), " ")
		if strings.TrimSpace(query) == "" {
			query = requirement
		}
		output, err := service.SearchWithProjection(ctx, repoquery.SearchRequest{
			Query: strings.TrimSpace(query),
			Limit: perRequirementLimit,
		})
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		hits := a.repoIndexAnchorsToCodeHits(output.Anchors, options)
		for _, hit := range hits {
			if hit.Metadata == nil {
				hit.Metadata = map[string]any{}
			}
			if strings.TrimSpace(hit.Symbol) != "" {
				hit.Metadata["candidate_role"] = "symbol_definition"
			}
			hit.Metadata["source"] = "repo_index_coverage"
			hit.Metadata["source_profile"] = "repo_code"
			hit.Metadata["evidence_class"] = "symbol_definition"
			hit.Metadata["coverage_requirement_ids"] = codeSearchCoverageRequirementIDs(hit.Path, hit.Symbol, hit.Snippet, []string{requirement})
			hit.Metadata["matched_terms"] = codeSearchCoverageTerms(requirement)
			hit.Metadata["path_family"] = filepath.ToSlash(filepath.Dir(hit.Path))
			hit.Sources = appendUniqueStringsEnv(hit.Sources, "repo_index_coverage")
			out = append(out, rankedCodeSearchHit{
				Priority: 92 - minInt(idx, 16),
				Hit:      hit,
			})
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return out, nil
}

func (a *ReadOnlyAdapter) repoIndexAnchorsToCodeHits(anchors []repoquery.Anchor, options codeSearchRequestOptions) []contextengine.CodeSearchHit {
	hits := make([]contextengine.CodeSearchHit, 0, len(anchors))
	for _, anchor := range anchors {
		path := strings.TrimSpace(anchor.Path)
		if path == "" || codeSearchPathExcluded(path, options.ExcludedPaths) {
			continue
		}
		statement := a.repoAnchorStatement(anchor)
		hits = append(hits, contextengine.CodeSearchHit{
			Path:     path,
			Snippet:  statement,
			Line:     anchor.LineHint,
			Symbol:   strings.TrimSpace(anchor.SymbolName),
			Score:    repoAnchorScore(anchor.Score),
			Language: languageFromPath(path),
			Metadata: map[string]any{
				"candidate_role":  "repo_index_anchor",
				"source":          "repo_index",
				"source_profile":  "repo_code",
				"evidence_class":  "index",
				"coverage_terms":  codeSearchCoverageTerms(path, anchor.SymbolName, statement),
				"path_family":     filepath.ToSlash(filepath.Dir(path)),
				"is_test":         isTestLikeCodeSearchPath(path),
				"repo_index_node": strings.TrimSpace(anchor.SymbolID),
			},
			Sources: []string{"repo_index"},
		})
	}
	return hits
}

func (a *ReadOnlyAdapter) semanticFileIndexSearchHits(ctx context.Context, query string, requiredEvidence []string, limit int, options codeSearchRequestOptions) ([]rankedCodeSearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	provider, err := semantic.NewProviderForScope(semantic.ScopeFileSummaries, a.cfg)
	if err != nil {
		return nil, err
	}
	var queryEmbedding []float32
	if queryProvider, ok := provider.(semantic.QueryEmbeddingProvider); ok {
		queryEmbedding, err = queryProvider.EmbedQuery(ctx, query)
	} else {
		queryEmbedding, err = provider.Embed(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	workspaceID := ws.ID(a.workspaceRoot)
	cacheEntries, err := a.semanticEmbeddingCacheEntries(ctx, workspaceID, len(queryEmbedding))
	if err != nil {
		return nil, err
	}
	scoredEntries := rankSemanticEmbeddingCacheEntries(cacheEntries, queryEmbedding, maxInt(limit*8, 64))

	candidates := map[string]rankedCodeSearchHit{}
	add := func(pathValue string, language string, score float64, snippet string, chunkInfo string) {
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" || codeSearchPathExcluded(pathValue, options.ExcludedPaths) || !codeSearchPathPrefixAllowed(pathValue, options.PathPrefixes) {
			return
		}
		if isThirdPartyCodeSearchPath(pathValue) && !codeSearchOptionsAllowThirdParty(options) {
			return
		}
		if language == "" {
			language = languageFromPath(pathValue)
		}
		hit := contextengine.CodeSearchHit{
			Path:     pathValue,
			Snippet:  strings.TrimSpace(snippet),
			Score:    clampScore(score),
			Language: language,
			Metadata: map[string]any{
				"candidate_role":           "semantic_file_candidate",
				"source":                   "semantic_file_index",
				"source_profile":           "repo_code",
				"evidence_class":           "semantic_file_embedding",
				"coverage_terms":           codeSearchCoverageTerms(pathValue, "", snippet),
				"coverage_requirement_ids": codeSearchCoverageRequirementIDs(pathValue, "", snippet, requiredEvidence),
				"path_family":              filepath.ToSlash(filepath.Dir(pathValue)),
				"is_test":                  isTestLikeCodeSearchPath(pathValue),
			},
			Sources: []string{"semantic_file_index"},
		}
		if chunkInfo != "" {
			hit.Metadata["chunk"] = chunkInfo
		}
		priority := 78
		if len(metadataStringSliceEnv(hit.Metadata, "coverage_requirement_ids")) > 0 {
			priority = 88
		}
		existing, ok := candidates[pathValue]
		if !ok || priority > existing.Priority || (priority == existing.Priority && hit.Score > existing.Hit.Score) {
			candidates[pathValue] = rankedCodeSearchHit{Priority: priority, Hit: hit}
		}
	}

	for _, scored := range scoredEntries {
		add(scored.Path, scored.Language, scored.Score, scored.Summary, scored.ChunkID)
	}

	out := make([]rankedCodeSearchHit, 0, len(candidates))
	for _, hit := range candidates {
		out = append(out, hit)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		if out[i].Hit.Score != out[j].Hit.Score {
			return out[i].Hit.Score > out[j].Hit.Score
		}
		wi := codeSearchFileWeight(out[i].Hit.Path)
		wj := codeSearchFileWeight(out[j].Hit.Path)
		if wi != wj {
			return wi > wj
		}
		return out[i].Hit.Path < out[j].Hit.Path
	})
	maxHits := maxInt(limit*4, 16)
	if len(out) > maxHits {
		out = out[:maxHits]
	}
	return out, nil
}

func codeSearchSemanticProviderEnabled(sourceProfiles []contextengine.SourceProfile) bool {
	if envValue := strings.TrimSpace(os.Getenv("FOXCTL_RLM_GATHER_SEMANTIC")); envValue != "" {
		switch strings.ToLower(envValue) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	}
	for _, profile := range sourceProfiles {
		if strings.EqualFold(string(profile), "semantic_code") || strings.EqualFold(string(profile), "semantic") {
			return true
		}
	}
	return false
}

const (
	semanticEmbeddingCachePageSize   = 5000
	semanticEmbeddingCacheMaxEntries = 50000
)

var semanticEmbeddingCacheStore = struct {
	sync.Mutex
	Entries map[string]*semanticEmbeddingCache
}{Entries: map[string]*semanticEmbeddingCache{}}

type semanticEmbeddingCache struct {
	WorkspaceID string
	Key         string
	Dimensions  int
	Entries     []semanticEmbeddingCacheEntry
	LoadedAt    time.Time
}

type semanticEmbeddingCacheEntry struct {
	Path      string
	Language  string
	Digest    string
	Summary   string
	ChunkID   string
	Embedding []float32
}

type scoredSemanticEmbeddingCacheEntry struct {
	Path     string
	Language string
	Summary  string
	ChunkID  string
	Score    float64
}

func (a *ReadOnlyAdapter) semanticEmbeddingCacheEntries(ctx context.Context, workspaceID string, dimensions int) ([]semanticEmbeddingCacheEntry, error) {
	if dimensions <= 0 {
		return nil, nil
	}
	cacheKey := semanticEmbeddingCacheKey(a.cfg, dimensions)
	globalKey := workspaceID + "|" + cacheKey
	semanticEmbeddingCacheStore.Lock()
	if cache := semanticEmbeddingCacheStore.Entries[globalKey]; cache != nil && cache.WorkspaceID == workspaceID && cache.Key == cacheKey && cache.Dimensions == dimensions {
		entries := append([]semanticEmbeddingCacheEntry(nil), cache.Entries...)
		semanticEmbeddingCacheStore.Unlock()
		return entries, nil
	}
	semanticEmbeddingCacheStore.Unlock()

	entries, err := a.loadSemanticEmbeddingCacheEntries(ctx, workspaceID, dimensions, false)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		if legacyEntries, legacyErr := a.loadSemanticEmbeddingCacheEntries(ctx, workspaceID, dimensions, true); legacyErr == nil && len(legacyEntries) > 0 {
			entries = legacyEntries
		}
	}

	cache := &semanticEmbeddingCache{
		WorkspaceID: workspaceID,
		Key:         cacheKey,
		Dimensions:  dimensions,
		Entries:     append([]semanticEmbeddingCacheEntry(nil), entries...),
		LoadedAt:    time.Now(),
	}
	semanticEmbeddingCacheStore.Lock()
	semanticEmbeddingCacheStore.Entries[globalKey] = cache
	semanticEmbeddingCacheStore.Unlock()

	return entries, nil
}

func semanticEmbeddingCacheKey(cfg config.Config, dimensions int) string {
	parts := []string{
		strings.TrimSpace(cfg.Embedding.Provider),
		strings.TrimSpace(cfg.Embedding.Model),
		strings.TrimSpace(cfg.Database.Driver),
		strings.TrimSpace(cfg.Database.Turso.URL),
		strings.TrimSpace(cfg.Storage.Root),
		strconv.Itoa(dimensions),
	}
	return strings.Join(parts, "|")
}

func (a *ReadOnlyAdapter) loadSemanticEmbeddingCacheEntries(ctx context.Context, workspaceID string, dimensions int, legacy bool) ([]semanticEmbeddingCacheEntry, error) {
	storeCfg := a.cfg
	if dimensions > 0 {
		storeCfg.Embedding.Dimensions = dimensions
		storeCfg.Database.Vector.Dimensions = dimensions
	}
	var memStore storage.MemoryStore
	var err error
	if legacy {
		storageDir := storeCfg.Storage.Root
		if storageDir == "" {
			storageDir = filepath.Join(storeCfg.Home, "storage")
		}
		casDir := storeCfg.Paths.CAS
		if casDir == "" {
			casDir = filepath.Join(storeCfg.Home, "cas")
		}
		memStore, err = memorystore.Open(ctx, storageDir, casDir)
	} else {
		memStore, err = memorystore.OpenWithConfig(ctx, storeCfg)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = memStore.Close() }()

	filter := storage.MemoryListFilter{Types: []string{semantic.FileEmbeddingType, semantic.FileEmbeddingChunkType}}
	out := make([]semanticEmbeddingCacheEntry, 0, 8192)
	for offset := 0; offset < semanticEmbeddingCacheMaxEntries; offset += semanticEmbeddingCachePageSize {
		entries, total, err := memStore.ListFiltered(ctx, workspaceID, filter, semanticEmbeddingCachePageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			cacheEntry, ok := semanticEmbeddingEntryFromNamedEntry(entry, dimensions)
			if ok {
				out = append(out, cacheEntry)
			}
		}
		if len(entries) == 0 || offset+len(entries) >= total {
			break
		}
	}
	return out, nil
}

func semanticEmbeddingEntryFromNamedEntry(entry storage.NamedEntry, dimensions int) (semanticEmbeddingCacheEntry, bool) {
	switch entry.Type {
	case semantic.FileEmbeddingType:
		result, err := semantic.UnmarshalFileResult(entry.Result)
		if err != nil || result == nil || len(result.Embedding) != dimensions {
			return semanticEmbeddingCacheEntry{}, false
		}
		return semanticEmbeddingCacheEntry{
			Path:      normalizeCodeSearchPath(result.Path),
			Language:  result.Language,
			Digest:    result.Digest,
			Summary:   strings.TrimSpace(entry.Summary),
			Embedding: result.Embedding,
		}, true
	case semantic.FileEmbeddingChunkType:
		result, err := semantic.UnmarshalChunkResult(entry.Result)
		if err != nil || result == nil || len(result.Embedding) != dimensions {
			return semanticEmbeddingCacheEntry{}, false
		}
		return semanticEmbeddingCacheEntry{
			Path:      normalizeCodeSearchPath(result.Path),
			Language:  result.Language,
			Digest:    result.Digest,
			Summary:   strings.TrimSpace(entry.Summary),
			ChunkID:   strings.TrimSpace(result.Chunk.ID),
			Embedding: result.Embedding,
		}, true
	default:
		return semanticEmbeddingCacheEntry{}, false
	}
}

func rankSemanticEmbeddingCacheEntries(entries []semanticEmbeddingCacheEntry, queryEmbedding []float32, limit int) []scoredSemanticEmbeddingCacheEntry {
	if len(entries) == 0 || len(queryEmbedding) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 64
	}
	byPath := map[string]scoredSemanticEmbeddingCacheEntry{}
	for _, entry := range entries {
		if entry.Path == "" || len(entry.Embedding) != len(queryEmbedding) {
			continue
		}
		score := clampScore((vector.Cosine(queryEmbedding, entry.Embedding) + 1) / 2)
		current, ok := byPath[entry.Path]
		if !ok || score > current.Score {
			byPath[entry.Path] = scoredSemanticEmbeddingCacheEntry{
				Path:     entry.Path,
				Language: entry.Language,
				Summary:  entry.Summary,
				ChunkID:  entry.ChunkID,
				Score:    score,
			}
		}
	}
	out := make([]scoredSemanticEmbeddingCacheEntry, 0, len(byPath))
	for _, entry := range byPath {
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		wi := codeSearchFileWeight(out[i].Path)
		wj := codeSearchFileWeight(out[j].Path)
		if wi != wj {
			return wi > wj
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (a *ReadOnlyAdapter) localCodeProbeSearch(ctx context.Context, query string, taskType string, requiredEvidence []string, limit int, options codeSearchRequestOptions, budget *localProviderBudget) ([]rankedCodeSearchHit, error) {
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
	pathHits, exactHits, scanErr := workspaceCodeProbeSearch(ctx, a.workspaceRoot, pathProbes, exactProbes, probeLimit, options.ExcludedPaths, budget)
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
		if pathValue == "" || codeSearchPathExcluded(pathValue, options.ExcludedPaths) {
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
		definitions := a.findLocalDefinitions(ctx, codeSearchSymbolProbes(query), budget)
		for _, symbol := range sortedStringKeys(definitions) {
			definition := definitions[symbol]
			if definition.Path == "" || codeSearchPathExcluded(definition.Path, options.ExcludedPaths) {
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
			if hit.Path == "" || codeSearchPathExcluded(hit.Path, options.ExcludedPaths) {
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

func (a *ReadOnlyAdapter) localLexicalCodeSearch(ctx context.Context, query string, limit int, options codeSearchRequestOptions, budget *localProviderBudget) ([]rankedCodeSearchHit, error) {
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
	hits, err := workspaceLexicalCodeSearch(ctx, a.workspaceRoot, terms, minInt(maxInt(limit*3, 16), 48), options.ExcludedPaths, budget)
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

func (a *ReadOnlyAdapter) localCoverageCodeSearchHits(ctx context.Context, query string, requiredEvidence []string, limit int, options codeSearchRequestOptions, budget *localProviderBudget) ([]rankedCodeSearchHit, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" || len(cleanStringList(requiredEvidence)) == 0 {
		return nil, nil
	}
	return a.localCoverageFileSearchHits(ctx, query, requiredEvidence, nil, limit, false, options, budget)
}

func (a *ReadOnlyAdapter) repoDocsSearchHits(ctx context.Context, query string, requiredEvidence []string, sourceProfiles []contextengine.SourceProfile, limit int, options codeSearchRequestOptions, budget *localProviderBudget) ([]rankedCodeSearchHit, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" || !sourceProfileListHas(sourceProfiles, contextengine.SourceProfileRepoDocs) {
		return nil, nil
	}
	return a.localCoverageFileSearchHits(ctx, query, requiredEvidence, sourceProfiles, limit, true, options, budget)
}

func (a *ReadOnlyAdapter) localNonCodeConfigDataSearchHits(ctx context.Context, query string, requiredEvidence []string, sourceProfiles []contextengine.SourceProfile, limit int, options codeSearchRequestOptions, budget *localProviderBudget) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	requirements := cleanStringList(requiredEvidence)
	terms := codeSearchCoverageTerms(append([]string{query}, requirements...)...)
	if len(terms) == 0 {
		return nil
	}
	candidates := map[string]*coverageFileCandidate{}
	_ = filepath.WalkDir(a.workspaceRoot, func(current string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := budget.beforeFile(ctx); err != nil {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if shouldSkipLocalCodeSearchDir(d.Name()) {
				return filepath.SkipDir
			}
			if current != a.workspaceRoot {
				rel, relErr := filepath.Rel(a.workspaceRoot, current)
				if relErr == nil {
					rel = normalizeCodeSearchPath(filepath.ToSlash(rel))
					if codeSearchPathExcluded(rel, options.ExcludedPaths) {
						return filepath.SkipDir
					}
				}
			}
			return nil
		}
		rel, relErr := filepath.Rel(a.workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		rel = normalizeCodeSearchPath(filepath.ToSlash(rel))
		if rel == "" || codeSearchPathExcluded(rel, options.ExcludedPaths) || !codeSearchPathPrefixAllowed(rel, options.PathPrefixes) || !isLikelyNonCodeConfigDataPath(rel) {
			return nil
		}
		profile := nonCodeConfigDataSourceProfile(rel)
		if !nonCodeConfigDataProfileAllowed(sourceProfiles, profile) {
			return nil
		}
		info, statErr := d.Info()
		if statErr == nil && info.Size() > 512_000 {
			return nil
		}
		if statErr == nil {
			if err := budget.recordFile(info.Size()); err != nil {
				return filepath.SkipAll
			}
		} else if err := budget.recordFile(0); err != nil {
			return filepath.SkipAll
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
		if err := budget.recordHit(); err != nil {
			return filepath.SkipAll
		}
		if line == 0 {
			line = 1
		}
		score := 0.62 + minFloat(0.24, pathScore*0.20) + minFloat(0.12, float64(len(matched))*0.03)
		if len(coverageIDs) > 0 {
			score += minFloat(0.18, float64(len(coverageIDs))*0.06)
		}
		if isTestDataSupportPath(rel) {
			score += 0.08
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
	if len(items) > maxHits {
		items = items[:maxHits]
	}
	out := make([]rankedCodeSearchHit, 0, len(items))
	for _, candidate := range items {
		profile := nonCodeConfigDataSourceProfile(candidate.path)
		role := nonCodeConfigDataCandidateRole(candidate.path)
		source := "local_non_code_config_data"
		reason := source
		if len(candidate.matched) > 0 {
			reason += ": " + strings.Join(candidate.matched, ", ")
		}
		priority := 94
		if profile == contextengine.SourceProfileRepoCode {
			priority = 42
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
					"evidence_class":           "non_code_config_data",
					"coverage_terms":           codeSearchCoverageTerms(candidate.path, strings.Join(candidate.matched, " "), candidate.excerpt),
					"coverage_requirement_ids": candidate.coverageIDs,
					"matched_terms":            candidate.matched,
					"path_family":              filepath.ToSlash(filepath.Dir(candidate.path)),
					"is_test":                  isTestDataSupportPath(candidate.path),
				},
				Sources: []string{source},
			},
		})
	}
	return out
}

func sourceProfileListHas(profiles []contextengine.SourceProfile, want contextengine.SourceProfile) bool {
	for _, profile := range profiles {
		if profile == want {
			return true
		}
	}
	return false
}

func nonCodeConfigDataProfileAllowed(profiles []contextengine.SourceProfile, profile contextengine.SourceProfile) bool {
	if len(profiles) == 0 {
		return profile == contextengine.SourceProfileRepoCode
	}
	return sourceProfileListHas(profiles, profile)
}

func nonCodeConfigDataSourceProfile(pathValue string) contextengine.SourceProfile {
	if isLikelyDocumentationPath(pathValue) {
		return contextengine.SourceProfileRepoDocs
	}
	return contextengine.SourceProfileRepoCode
}

func nonCodeConfigDataCandidateRole(pathValue string) string {
	if isTestDataSupportPath(pathValue) {
		return "test_data_support"
	}
	if isConfigSupportPath(pathValue) {
		return "config_support"
	}
	return "data_support"
}

func isLikelyNonCodeConfigDataPath(pathValue string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(pathValue))) {
	case ".json", ".yaml", ".yml", ".toml":
		return true
	default:
		return false
	}
}

func isConfigSupportPath(pathValue string) bool {
	pathValue = strings.ToLower(filepath.ToSlash(strings.TrimSpace(pathValue)))
	base := filepath.Base(pathValue)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	for _, token := range nonCodePathTokens(stem) {
		switch token {
		case "config", "configuration", "settings", "schema", "manifest":
			return true
		}
	}
	for _, segment := range strings.Split(strings.Trim(pathValue, "/"), "/") {
		for _, token := range nonCodePathTokens(segment) {
			switch token {
			case "config", "configs", "configuration", "settings":
				return true
			}
		}
	}
	return false
}

func isTestDataSupportPath(pathValue string) bool {
	pathValue = strings.ToLower(filepath.ToSlash(strings.TrimSpace(pathValue)))
	for _, segment := range strings.Split(strings.Trim(pathValue, "/"), "/") {
		tokens := nonCodePathTokenSet(segment)
		if _, ok := tokens["fixture"]; ok {
			return true
		}
		if _, ok := tokens["fixtures"]; ok {
			return true
		}
		_, hasTest := tokens["test"]
		if !hasTest {
			_, hasTest = tokens["tests"]
		}
		_, hasData := tokens["data"]
		if hasTest && hasData {
			return true
		}
	}
	return isTestLikeCodeSearchPath(pathValue)
}

func nonCodePathTokenSet(value string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, token := range nonCodePathTokens(value) {
		out[token] = struct{}{}
	}
	return out
}

func nonCodePathTokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= '0' && r <= '9':
			return false
		default:
			return true
		}
	})
}

type coverageFileCandidate struct {
	path        string
	line        int
	score       float64
	excerpt     string
	matched     []string
	coverageIDs []string
}

func (a *ReadOnlyAdapter) localCoverageFileSearchHits(ctx context.Context, query string, requiredEvidence []string, sourceProfiles []contextengine.SourceProfile, limit int, docsOnly bool, options codeSearchRequestOptions, budget *localProviderBudget) ([]rankedCodeSearchHit, error) {
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
			if current != a.workspaceRoot {
				rel, relErr := filepath.Rel(a.workspaceRoot, current)
				if relErr == nil && codeSearchPathExcluded(rel, options.ExcludedPaths) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, relErr := filepath.Rel(a.workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		rel = normalizeCodeSearchPath(filepath.ToSlash(rel))
		if rel == "" || codeSearchPathExcluded(rel, options.ExcludedPaths) || !isLikelyLocalProviderCodeFile(rel) {
			return nil
		}
		if err := budget.beforeFile(ctx); err != nil {
			return err
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
		if statErr == nil {
			if err := budget.recordFile(info.Size()); err != nil {
				return err
			}
		} else if err := budget.recordFile(0); err != nil {
			return err
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
			if err := budget.recordHit(); err != nil {
				return err
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
		if err := budget.recordHit(); err != nil {
			return err
		}
		return nil
	})
	if err = budget.cappedError(err); err != nil {
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
		items = selectCoverageFanoutItems(items, maxInt(1, minInt(len(requirements), maxInt(2, limit))))
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

type requiredPathRepairRequirement struct {
	raw string
	key string
}

type requiredPathRepairDir struct {
	path  string
	score float64
	count int
}

type requiredPathRepairCandidate struct {
	path        string
	line        int
	score       float64
	requirement string
	key         string
}

func (a *ReadOnlyAdapter) localRequiredPathRepairHits(requiredEvidence []string, seeds []contextengine.CodeSearchHit, limit int) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" || len(seeds) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	requirements := requiredPathRepairRequirements(requiredEvidence)
	if len(requirements) == 0 {
		return nil
	}
	dirScores := map[string]*requiredPathRepairDir{}
	addDir := func(dir string, score float64) {
		dir = strings.Trim(filepath.ToSlash(dir), "/.")
		if dir == "" {
			return
		}
		item := dirScores[dir]
		if item == nil {
			item = &requiredPathRepairDir{path: dir}
			dirScores[dir] = item
		}
		item.score += score
		item.count++
	}
	for _, seed := range seeds {
		pathValue := normalizeCodeSearchPath(seed.Path)
		if pathValue == "" || !isLikelyLocalProviderCodeFile(pathValue) {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(pathValue))
		seedScore := repoAnchorScore(seed.Score)
		addDir(dir, seedScore)
		if parent := filepath.ToSlash(filepath.Dir(dir)); parent != "." && parent != "" {
			addDir(parent, seedScore*0.55)
		}
	}
	if len(dirScores) == 0 {
		return nil
	}
	dirs := make([]*requiredPathRepairDir, 0, len(dirScores))
	for _, dir := range dirScores {
		dirs = append(dirs, dir)
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		if dirs[i].count != dirs[j].count {
			return dirs[i].count > dirs[j].count
		}
		if dirs[i].score != dirs[j].score {
			return dirs[i].score > dirs[j].score
		}
		return dirs[i].path < dirs[j].path
	})
	if len(dirs) > 10 {
		dirs = dirs[:10]
	}
	byRequirement := map[string]*requiredPathRepairCandidate{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(filepath.Join(a.workspaceRoot, filepath.FromSlash(dir.path)))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || shouldSkipLocalCodeSearchDir(entry.Name()) {
				continue
			}
			pathValue := normalizeCodeSearchPath(filepath.ToSlash(filepath.Join(dir.path, entry.Name())))
			if pathValue == "" || !isLikelyLocalProviderCodeFile(pathValue) || isLikelyDocumentationPath(pathValue) {
				continue
			}
			info, statErr := entry.Info()
			if statErr == nil && info.Size() > 1_000_000 {
				continue
			}
			for _, req := range requirements {
				if !requiredPathRepairPathMatches(pathValue, req.key) {
					continue
				}
				score := requiredPathRepairScore(pathValue, req.key, dir.score)
				current := byRequirement[req.key]
				if current != nil && requiredPathRepairCandidateBetter(current, score, pathValue) {
					continue
				}
				byRequirement[req.key] = &requiredPathRepairCandidate{
					path:        pathValue,
					line:        1,
					score:       score,
					requirement: req.raw,
					key:         req.key,
				}
			}
		}
	}
	candidates := make([]*requiredPathRepairCandidate, 0, len(byRequirement))
	for _, candidate := range byRequirement {
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].path < candidates[j].path
	})
	maxHits := maxInt(limit, len(requirements))
	if len(candidates) > maxHits {
		candidates = candidates[:maxHits]
	}
	out := make([]rankedCodeSearchHit, 0, len(candidates))
	for _, candidate := range candidates {
		excerpt := a.repoFileExcerpt(candidate.path, candidate.line, 0, 5)
		coverageIDs := codeSearchCoverageRequirementIDs(candidate.path, "", candidate.requirement, requiredEvidence)
		out = append(out, rankedCodeSearchHit{
			Priority: 85,
			Hit: contextengine.CodeSearchHit{
				Path:     candidate.path,
				Snippet:  codeProbeSnippet("required path repair: "+candidate.requirement, excerpt),
				Line:     candidate.line,
				Score:    clampScore(candidate.score),
				Language: languageFromPath(candidate.path),
				Metadata: map[string]any{
					"candidate_role":           "required_path_support",
					"source":                   "local_required_path_repair",
					"source_profile":           "repo_code",
					"evidence_class":           "path_coverage",
					"coverage_terms":           codeSearchCoverageTerms(candidate.path, candidate.requirement),
					"coverage_requirement_ids": coverageIDs,
					"matched_terms":            []string{candidate.requirement},
					"path_family":              filepath.ToSlash(filepath.Dir(candidate.path)),
					"is_test":                  isTestLikeCodeSearchPath(candidate.path),
				},
				Sources: []string{"local_required_path_repair"},
			},
		})
	}
	return out
}

func (a *ReadOnlyAdapter) localRequiredDefinitionRepairHits(ctx context.Context, requiredEvidence []string, limit int, options codeSearchRequestOptions, budget *localProviderBudget) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	symbols := requiredDefinitionRepairSymbols(requiredEvidence)
	if len(symbols) == 0 {
		return nil
	}
	definitions := a.findLocalDefinitions(ctx, symbols, budget)
	if len(definitions) == 0 {
		return nil
	}
	out := make([]rankedCodeSearchHit, 0, len(definitions))
	for _, symbol := range symbols {
		def := definitions[symbol]
		if def.Path == "" || codeSearchPathExcluded(def.Path, options.ExcludedPaths) {
			continue
		}
		excerpt := a.repoFileExcerpt(def.Path, def.Line, 3, 5)
		coverageIDs := codeSearchCoverageRequirementIDs(def.Path, symbol, symbol, requiredEvidence)
		out = append(out, rankedCodeSearchHit{
			Priority: 82,
			Hit: contextengine.CodeSearchHit{
				Path:     def.Path,
				Snippet:  codeProbeSnippet("required definition repair: "+symbol, excerpt),
				Line:     def.Line,
				Symbol:   symbol,
				Score:    0.94,
				Language: languageFromPath(def.Path),
				Metadata: map[string]any{
					"candidate_role":           "symbol_definition",
					"source":                   "local_required_definition_repair",
					"source_profile":           "repo_code",
					"evidence_class":           "definition",
					"coverage_terms":           codeSearchCoverageTerms(def.Path, symbol),
					"coverage_requirement_ids": coverageIDs,
					"matched_terms":            []string{symbol},
					"path_family":              filepath.ToSlash(filepath.Dir(def.Path)),
					"is_test":                  isTestLikeCodeSearchPath(def.Path),
				},
				Sources: []string{"local_required_definition_repair"},
			},
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Hit.Score != out[j].Hit.Score {
			return out[i].Hit.Score > out[j].Hit.Score
		}
		return out[i].Hit.Path < out[j].Hit.Path
	})
	if len(out) > maxInt(limit, len(symbols)) {
		out = out[:maxInt(limit, len(symbols))]
	}
	return out
}

func (a *ReadOnlyAdapter) localTestCoverageRepairHits(ctx context.Context, query string, requiredEvidence []string, limit int, options codeSearchRequestOptions, budget *localProviderBudget) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" || !requiredEvidenceSuggestsTests([]string{query}) {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	terms := testCoverageSubjectTerms(query, requiredEvidence)
	if len(terms) == 0 {
		return nil
	}
	type candidate struct {
		path         string
		line         int
		score        float64
		matchedTerms []string
	}
	candidates := map[string]*candidate{}
	err := filepath.WalkDir(a.workspaceRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if current != a.workspaceRoot && shouldSkipLocalCodeSearchDir(entry.Name()) {
				return filepath.SkipDir
			}
			if current != a.workspaceRoot {
				rel, relErr := filepath.Rel(a.workspaceRoot, current)
				if relErr == nil && codeSearchPathExcluded(rel, options.ExcludedPaths) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(a.workspaceRoot, current)
		if err != nil {
			return nil
		}
		pathValue := normalizeCodeSearchPath(rel)
		if pathValue == "" || codeSearchPathExcluded(pathValue, options.ExcludedPaths) || !isTestLikeCodeSearchPath(pathValue) || !isLikelyLocalProviderCodeFile(pathValue) || isThirdPartyCodeSearchPath(pathValue) {
			return nil
		}
		if err := budget.beforeFile(ctx); err != nil {
			return err
		}
		info, statErr := entry.Info()
		if statErr == nil && info.Size() > 1_000_000 {
			return nil
		}
		if statErr == nil {
			if err := budget.recordFile(info.Size()); err != nil {
				return err
			}
		} else if err := budget.recordFile(0); err != nil {
			return err
		}
		body, readErr := os.ReadFile(current)
		if readErr != nil {
			return nil
		}
		text := string(body)
		lowerText := strings.ToLower(text)
		matched := make([]string, 0, len(terms))
		score := 0.0
		pathKey := normalizedPathRequirementKey(pathValue)
		for _, term := range terms {
			termKey := normalizedPathRequirementKey(term)
			if termKey == "" {
				continue
			}
			switch {
			case strings.Contains(pathKey, termKey):
				matched = appendUniqueStringEnv(matched, term)
				score += 2.5
			case strings.Contains(lowerText, strings.ToLower(term)):
				matched = appendUniqueStringEnv(matched, term)
				score += 1
			}
		}
		if len(matched) == 0 {
			return nil
		}
		line := firstMatchingLineNumber(text, matched)
		candidates[pathValue] = &candidate{path: pathValue, line: line, score: score, matchedTerms: matched}
		if err := budget.recordHit(); err != nil {
			return err
		}
		return nil
	})
	err = budget.cappedError(err)
	if err != nil || len(candidates) == 0 {
		return nil
	}
	items := make([]*candidate, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, candidate)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].path < items[j].path
	})
	maxHits := maxInt(limit, 12)
	if len(items) > maxHits {
		items = items[:maxHits]
	}
	out := make([]rankedCodeSearchHit, 0, len(items))
	for _, item := range items {
		excerpt := a.repoFileExcerpt(item.path, item.line, 3, 6)
		out = append(out, rankedCodeSearchHit{
			Priority: 92,
			Hit: contextengine.CodeSearchHit{
				Path:     item.path,
				Snippet:  codeProbeSnippet("test coverage repair: "+strings.Join(item.matchedTerms, ", "), excerpt),
				Line:     item.line,
				Score:    clampScore(0.82 + minFloat(item.score, 4)*0.04),
				Language: languageFromPath(item.path),
				Metadata: map[string]any{
					"candidate_role":           "test_companion",
					"source":                   "local_test_coverage_repair",
					"source_profile":           "repo_code",
					"evidence_class":           "test_coverage",
					"coverage_terms":           codeSearchCoverageTerms(item.path, strings.Join(item.matchedTerms, " ")),
					"coverage_requirement_ids": codeSearchCoverageRequirementIDs(item.path, "", strings.Join(item.matchedTerms, " "), requiredEvidence),
					"matched_terms":            item.matchedTerms,
					"path_family":              filepath.ToSlash(filepath.Dir(item.path)),
					"is_test":                  true,
				},
				Sources: []string{"local_test_coverage_repair"},
			},
		})
	}
	return out
}

func (a *ReadOnlyAdapter) localRouteActionRepairHits(ctx context.Context, query string, requiredEvidence []string, limit int, options codeSearchRequestOptions, budget *localProviderBudget) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	terms := routeActionSubjectTerms(query, requiredEvidence)
	if len(terms) == 0 {
		return nil
	}
	type candidate struct {
		path         string
		line         int
		score        float64
		priority     int
		matchedTerms []string
		routeSignals []string
	}
	var candidates []*candidate
	err := filepath.WalkDir(a.workspaceRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if current != a.workspaceRoot && shouldSkipLocalCodeSearchDir(entry.Name()) {
				return filepath.SkipDir
			}
			if current != a.workspaceRoot {
				rel, relErr := filepath.Rel(a.workspaceRoot, current)
				if relErr == nil && codeSearchPathExcluded(rel, options.ExcludedPaths) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(a.workspaceRoot, current)
		if err != nil {
			return nil
		}
		pathValue := normalizeCodeSearchPath(rel)
		if pathValue == "" || codeSearchPathExcluded(pathValue, options.ExcludedPaths) || isTestLikeCodeSearchPath(pathValue) || !isLikelyLocalProviderCodeFile(pathValue) || isThirdPartyCodeSearchPath(pathValue) {
			return nil
		}
		if !isLikelyRouteActionPath(pathValue) {
			return nil
		}
		if err := budget.beforeFile(ctx); err != nil {
			return err
		}
		info, statErr := entry.Info()
		if statErr == nil && info.Size() > 1_000_000 {
			return nil
		}
		if statErr == nil {
			if err := budget.recordFile(info.Size()); err != nil {
				return err
			}
		} else if err := budget.recordFile(0); err != nil {
			return err
		}
		body, readErr := os.ReadFile(current)
		if readErr != nil {
			return nil
		}
		text := string(body)
		routeSignals := routeActionSignals(pathValue, text)
		if len(routeSignals) == 0 {
			return nil
		}
		matched := routeActionMatchedTerms(pathValue, text, terms)
		if len(matched) == 0 {
			return nil
		}
		line := firstMatchingLineNumber(text, append(matched, routeSignals...))
		pathMatched := routeActionPathMatchesTerms(pathValue, matched)
		score := 0.78 + minFloat(0.14, float64(len(routeSignals))*0.04) + minFloat(0.18, float64(len(matched))*0.05)
		if strings.Contains(strings.ToLower(filepath.ToSlash(pathValue)), "/actions/") {
			score += 0.08
		}
		priority := 88
		if pathMatched {
			priority = 90
			score += 0.08
		}
		candidates = append(candidates, &candidate{
			path:         pathValue,
			line:         line,
			score:        clampScore(score),
			priority:     priority,
			matchedTerms: matched,
			routeSignals: routeSignals,
		})
		if err := budget.recordHit(); err != nil {
			return err
		}
		return nil
	})
	err = budget.cappedError(err)
	if err != nil || len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].path < candidates[j].path
	})
	maxHits := maxInt(limit, 12)
	if len(candidates) > maxHits {
		candidates = candidates[:maxHits]
	}
	out := make([]rankedCodeSearchHit, 0, len(candidates))
	for _, candidate := range candidates {
		excerpt := a.repoFileExcerpt(candidate.path, candidate.line, 3, 8)
		reason := "route/action repair: " + strings.Join(candidate.matchedTerms, ", ")
		out = append(out, rankedCodeSearchHit{
			Priority: candidate.priority,
			Hit: contextengine.CodeSearchHit{
				Path:     candidate.path,
				Snippet:  codeProbeSnippet(reason, excerpt),
				Line:     candidate.line,
				Score:    candidate.score,
				Language: languageFromPath(candidate.path),
				Metadata: map[string]any{
					"candidate_role":           "direct_dispatch_file",
					"source":                   "local_route_action_repair",
					"source_profile":           "repo_code",
					"evidence_class":           "route_action",
					"coverage_terms":           codeSearchCoverageTerms(candidate.path, strings.Join(candidate.matchedTerms, " "), strings.Join(candidate.routeSignals, " ")),
					"coverage_requirement_ids": codeSearchCoverageRequirementIDs(candidate.path, "", reason, requiredEvidence),
					"matched_terms":            candidate.matchedTerms,
					"route_signals":            candidate.routeSignals,
					"path_family":              filepath.ToSlash(filepath.Dir(candidate.path)),
					"is_test":                  false,
				},
				Sources: []string{"local_route_action_repair"},
			},
		})
	}
	return out
}

func routeActionPathMatchesTerms(pathValue string, terms []string) bool {
	pathKey := normalizedPathRequirementKey(pathValue)
	for _, term := range terms {
		termKey := normalizedPathRequirementKey(term)
		if termKey != "" && strings.Contains(pathKey, termKey) {
			return true
		}
	}
	return false
}

func routeActionSubjectTerms(query string, requiredEvidence []string) []string {
	values := append([]string{query}, requiredEvidence...)
	seen := map[string]struct{}{}
	out := make([]string, 0, 10)
	for _, value := range values {
		for _, term := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			switch {
			case r >= 'a' && r <= 'z':
				return false
			case r >= '0' && r <= '9':
				return false
			default:
				return true
			}
		}) {
			if len(term) < 4 || isGenericCodeSearchPathWord(term) {
				continue
			}
			switch term {
			case "route", "routes", "router", "action", "actions", "backend", "file", "files", "return", "paths", "facts", "find", "test", "tests", "domain", "implementation":
				continue
			}
			if strings.HasSuffix(term, "s") && len(term) > 5 {
				term = strings.TrimSuffix(term, "s")
			}
			if _, ok := seen[term]; ok {
				continue
			}
			seen[term] = struct{}{}
			out = append(out, term)
		}
	}
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func isLikelyRouteActionPath(pathValue string) bool {
	pathValue = strings.ToLower(filepath.ToSlash(pathValue))
	if pathValue == "" {
		return false
	}
	switch filepath.Ext(pathValue) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".ex", ".exs", ".rb", ".cs":
	default:
		return false
	}
	for _, part := range strings.Split(pathValue, "/") {
		switch part {
		case "actions", "action", "routes", "route", "routers", "router", "controllers", "controller", "handlers", "handler", "endpoints", "endpoint", "api":
			return true
		}
	}
	base := strings.TrimSuffix(filepath.Base(pathValue), filepath.Ext(pathValue))
	switch base {
	case "routes", "router", "server", "app", "index", "main":
		return true
	default:
		return false
	}
}

func routeActionSignals(pathValue string, text string) []string {
	lower := strings.ToLower(text)
	signals := make([]string, 0, 8)
	add := func(signal string) {
		signals = appendUniqueStringEnv(signals, signal)
	}
	for _, signal := range []string{
		"new hono", ".route(", ".get(", ".post(", ".put(", ".patch(", ".delete(",
		"router.", "express.router", "@app.", "@router.", "fastapi(", "apirouter(",
		"scope ", "pipe_through", "plug ", "post \"", "get \"", "put \"", "patch \"", "delete \"",
		"func ", "http.handle", "handlefunc",
	} {
		if strings.Contains(lower, signal) {
			add(signal)
		}
	}
	if strings.Contains(lower, "routes") || strings.Contains(lower, "router") {
		add("route_symbol")
	}
	return signals
}

func routeActionMatchedTerms(pathValue string, text string, terms []string) []string {
	relevantText := routeActionRelevantText(text)
	pathKey := normalizedPathRequirementKey(pathValue)
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		termKey := normalizedPathRequirementKey(term)
		switch {
		case termKey != "" && strings.Contains(pathKey, termKey):
			out = appendUniqueStringEnv(out, term)
		case strings.Contains(relevantText, term):
			out = appendUniqueStringEnv(out, term)
		}
	}
	return out
}

func routeActionRelevantText(text string) string {
	lines := strings.Split(text, "\n")
	selected := make([]string, 0, 16)
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		if lower == "" {
			continue
		}
		for _, marker := range []string{
			"new hono", ".route(", ".get(", ".post(", ".put(", ".patch(", ".delete(",
			"router.", "express.router", "@app.", "@router.", "fastapi(", "apirouter(",
			"scope ", "pipe_through", "plug ", "post \"", "get \"", "put \"", "patch \"", "delete \"",
			"http.handle", "handlefunc",
		} {
			if strings.Contains(lower, marker) {
				selected = append(selected, lower)
				break
			}
		}
		if len(selected) >= 32 {
			break
		}
	}
	return strings.Join(selected, "\n")
}

func testCoverageSubjectTerms(query string, requiredEvidence []string) []string {
	values := append([]string{query}, requiredEvidence...)
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	for _, value := range values {
		for _, term := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			switch {
			case r >= 'a' && r <= 'z':
				return false
			case r >= '0' && r <= '9':
				return false
			default:
				return true
			}
		}) {
			if len(term) < 4 || isGenericCodeSearchPathWord(term) {
				continue
			}
			switch term {
			case "test", "tests", "testing", "spec", "specs", "fixture", "fixtures", "code", "domain", "return", "paths", "facts", "find":
				continue
			}
			if strings.HasSuffix(term, "s") && len(term) > 5 {
				term = strings.TrimSuffix(term, "s")
			}
			if _, ok := seen[term]; ok {
				continue
			}
			seen[term] = struct{}{}
			out = append(out, term)
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func firstMatchingLineNumber(text string, terms []string) int {
	lines := strings.Split(text, "\n")
	for idx, line := range lines {
		lower := strings.ToLower(line)
		for _, term := range terms {
			if strings.Contains(lower, strings.ToLower(term)) {
				return idx + 1
			}
		}
	}
	return 1
}

func requiredDefinitionRepairSymbols(requiredEvidence []string) []string {
	out := make([]string, 0, len(requiredEvidence))
	seen := map[string]struct{}{}
	for _, raw := range cleanStringList(requiredEvidence) {
		if isPathShapedCoverageRequirement(raw) || !isSymbolShapedCoverageRequirement(raw) {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func isSymbolShapedCoverageRequirement(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\r\n/._-") {
		return false
	}
	if len(value) < 4 || isGenericCodeSearchPathWord(strings.ToLower(value)) {
		return false
	}
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func requiredPathRepairRequirements(requiredEvidence []string) []requiredPathRepairRequirement {
	out := make([]requiredPathRepairRequirement, 0, len(requiredEvidence))
	seen := map[string]struct{}{}
	for _, raw := range cleanStringList(requiredEvidence) {
		if !isPathShapedCoverageRequirement(raw) {
			continue
		}
		key := normalizedPathRequirementKey(raw)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, requiredPathRepairRequirement{raw: raw, key: key})
	}
	return out
}

func requiredPathRepairPathMatches(pathValue string, key string) bool {
	pathKey := normalizedPathRequirementKey(pathValue)
	baseKey := normalizedPathRequirementKey(strings.TrimSuffix(filepath.Base(pathValue), filepath.Ext(pathValue)))
	return strings.Contains(pathKey, key) || strings.Contains(baseKey, key)
}

func requiredPathRepairScore(pathValue string, key string, dirScore float64) float64 {
	score := 0.76 + minFloat(0.12, dirScore*0.03)
	baseKey := normalizedPathRequirementKey(strings.TrimSuffix(filepath.Base(pathValue), filepath.Ext(pathValue)))
	switch {
	case baseKey == key:
		score += 0.16
	case strings.Contains(baseKey, key):
		score += 0.10
	}
	if isTestLikeCodeSearchPath(pathValue) {
		score -= 0.22
	}
	return score
}

func requiredPathRepairCandidateBetter(current *requiredPathRepairCandidate, nextScore float64, nextPath string) bool {
	if current.score != nextScore {
		return current.score > nextScore
	}
	if isTestLikeCodeSearchPath(current.path) != isTestLikeCodeSearchPath(nextPath) {
		return !isTestLikeCodeSearchPath(current.path)
	}
	return current.path < nextPath
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

func (a *ReadOnlyAdapter) liveOverlayCodeSearchHits(ctx context.Context, query string, requiredEvidence []string, limit int, budget *localProviderBudget) ([]rankedCodeSearchHit, error) {
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
		if item.path == "" || item.status == "deleted" || !isLikelyLocalProviderCodeFile(item.path) {
			continue
		}
		if err := budget.beforeFile(ctx); err != nil {
			break
		}
		abs := filepath.Join(a.workspaceRoot, filepath.FromSlash(item.path))
		info, statErr := os.Stat(abs)
		if statErr != nil || info.IsDir() || info.Size() > 1_000_000 {
			continue
		}
		if err := budget.recordFile(info.Size()); err != nil {
			break
		}
		body, readErr := os.ReadFile(abs)
		if readErr != nil {
			continue
		}
		text := string(body)
		line, matched := firstMatchingLine(text, terms)
		pathScore := codeSearchPathTermScore(item.path, terms)
		coverageIDs := codeSearchCoverageRequirementIDs(item.path, "", text, requiredEvidence)
		if len(coverageIDs) == 0 {
			coverageIDs = codeSearchCoverageRequirementIDs(item.path, "", strings.Join(append(append([]string{}, matched...), requiredEvidence...), "\n"), requiredEvidence)
		}
		if len(coverageIDs) == 0 && (pathScore > 0 || len(matched) > 0) {
			coverageIDs = codeSearchCoverageRequirementIDFallback(requiredEvidence)
		}
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
		if err := budget.recordHit(); err != nil {
			break
		}
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
	cmd := exec.CommandContext(ctx, "git", "-C", workspaceRoot, "status", "--porcelain", "--untracked-files=normal")
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
		if status == "untracked" && strings.HasSuffix(pathValue, "/") {
			out = append(out, expandUntrackedOverlayDir(ctx, workspaceRoot, strings.TrimSuffix(pathValue, "/"), 200)...)
			continue
		}
		out = append(out, liveOverlayPath{path: pathValue, status: status})
	}
	return out, nil
}

func expandUntrackedOverlayDir(ctx context.Context, workspaceRoot string, dir string, limit int) []liveOverlayPath {
	dir = normalizeCodeSearchPath(dir)
	if dir == "" || shouldSkipLocalCodeSearchDir(filepath.Base(dir)) || limit <= 0 {
		return nil
	}
	root := filepath.Join(workspaceRoot, filepath.FromSlash(dir))
	out := make([]liveOverlayPath, 0, minInt(limit, 16))
	_ = filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil || len(out) >= limit {
			return filepath.SkipAll
		}
		if entry.IsDir() {
			if shouldSkipLocalCodeSearchDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		pathValue := normalizeCodeSearchPath(filepath.ToSlash(rel))
		if pathValue == "" || isThirdPartyCodeSearchPath(pathValue) || !isLikelyLocalProviderCodeFile(pathValue) {
			return nil
		}
		out = append(out, liveOverlayPath{path: pathValue, status: "untracked"})
		return nil
	})
	return out
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

func workspaceLexicalCodeSearch(ctx context.Context, workspaceRoot string, terms []string, limit int, excludedPaths []string, budget *localProviderBudget) ([]localLexicalProbeHit, error) {
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
			if current != workspaceRoot {
				rel, relErr := filepath.Rel(workspaceRoot, current)
				if relErr == nil && codeSearchPathExcluded(rel, excludedPaths) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, relErr := filepath.Rel(workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if codeSearchPathExcluded(rel, excludedPaths) || !isLikelyLocalProviderCodeFile(rel) {
			return nil
		}
		if err := budget.beforeFile(ctx); err != nil {
			return err
		}
		info, statErr := d.Info()
		if statErr == nil && info.Size() > 1_000_000 {
			return nil
		}
		if statErr == nil {
			if err := budget.recordFile(info.Size()); err != nil {
				return err
			}
		} else if err := budget.recordFile(0); err != nil {
			return err
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
		if err := budget.recordHit(); err != nil {
			return err
		}
		return nil
	})
	if err = budget.cappedError(err); err != nil {
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
	case "node_modules", "deps", "_build", "tmp", "temp", "dist", "build", "out", "archive", "vendor", "cover", "coverage", "prompt-exports", "Pods":
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

func (a *ReadOnlyAdapter) localRelatedCodeSearchHits(ctx context.Context, query string, requiredEvidence []string, seeds []contextengine.CodeSearchHit, limit int, options codeSearchRequestOptions, budget *localProviderBudget) []rankedCodeSearchHit {
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
	allowTests := requiredEvidenceSuggestsTests(append([]string{query}, requiredEvidence...))

	addWithRole := func(pathValue string, priority int, line int, reason string, symbol string, role string) {
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" || codeSearchPathExcluded(pathValue, options.ExcludedPaths) {
			return
		}
		if _, err := os.Stat(filepath.Join(a.workspaceRoot, filepath.FromSlash(pathValue))); err != nil {
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
				Metadata: map[string]any{
					"candidate_role": role,
				},
			},
		}
	}
	add := func(pathValue string, priority int, line int, reason string, symbol string) {
		addWithRole(pathValue, priority, line, reason, symbol, "definition_support")
	}

	for idx, seed := range seeds {
		if idx >= maxInt(limit*4, 32) {
			break
		}
		seedPath := normalizeCodeSearchPath(seed.Path)
		if seedPath == "" || codeSearchPathExcluded(seedPath, options.ExcludedPaths) {
			continue
		}
		seedPaths[seedPath] = struct{}{}
		if counterpart := productionCounterpartPath(seedPath); counterpart != "" {
			addWithRole(counterpart, 84, 1, "related production file for "+seedPath, "", "production_companion")
		}
		if allowTests {
			for _, companion := range testCompanionPaths(seedPath) {
				addWithRole(companion, 88, 1, "test companion for "+seedPath, "", "test_companion")
			}
		}
		if commandSourceAffinity(queryLower, seedPath, seed.Snippet, exactProbes) {
			add(seedPath, 29, seed.Line, "command/build companion evidence", seed.Symbol)
		}
		if err := budget.beforeFile(ctx); err != nil {
			break
		}
		absSeedPath := filepath.Join(a.workspaceRoot, filepath.FromSlash(seedPath))
		if info, statErr := os.Stat(absSeedPath); statErr == nil {
			if info.Size() > 1_000_000 {
				continue
			}
			if err := budget.recordFile(info.Size()); err != nil {
				break
			}
		} else if err := budget.recordFile(0); err != nil {
			break
		}
		body, err := os.ReadFile(absSeedPath)
		if err != nil {
			continue
		}
		text := string(body)
		symbols := relatedExportedIdentifiers(text, seed.Snippet, exactProbes)
		for _, symbol := range symbols {
			definitionRequests = append(definitionRequests, definitionRequest{seedPath: seedPath, symbol: symbol})
			definitionSymbols = append(definitionSymbols, symbol)
		}
		for _, ref := range a.localReverseReferenceHits(ctx, seedPath, symbols, maxInt(2, limit/3), options.ExcludedPaths) {
			addWithRole(ref.path, 89, ref.line, ref.reason, ref.symbol, "import_reference")
		}
	}
	definitions := a.findLocalDefinitions(ctx, definitionSymbols, budget)
	for _, req := range definitionRequests {
		def := definitions[req.symbol]
		if def.Path == "" {
			continue
		}
		priority := 82
		if strings.HasPrefix(req.symbol, "Retrieve") {
			priority = 84
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
		if candidate.hit.Metadata == nil {
			candidate.hit.Metadata = map[string]any{}
		}
		if _, ok := candidate.hit.Metadata["candidate_role"]; !ok {
			candidate.hit.Metadata["candidate_role"] = "definition_support"
		}
		candidate.hit.Metadata["source"] = "local_related"
		candidate.hit.Metadata["source_profile"] = "repo_code"
		candidate.hit.Metadata["evidence_class"] = "definition_support"
		candidate.hit.Metadata["coverage_terms"] = codeSearchCoverageTerms(candidate.hit.Path, candidate.hit.Symbol, candidate.hit.Snippet)
		candidate.hit.Metadata["path_family"] = filepath.ToSlash(filepath.Dir(candidate.hit.Path))
		candidate.hit.Metadata["is_test"] = isTestLikeCodeSearchPath(candidate.hit.Path)
		candidate.hit.Sources = appendUniqueStringEnv(candidate.hit.Sources, "local_related")
		hits = append(hits, rankedCodeSearchHit{Hit: candidate.hit, Priority: candidate.priority})
	}
	return hits
}

func (a *ReadOnlyAdapter) localCompanionClosureHits(query string, seeds []contextengine.CodeSearchHit, limit int, options codeSearchRequestOptions) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" || len(seeds) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	allowTests := requiredEvidenceSuggestsTests([]string{query})
	type companionCandidate struct {
		path     string
		role     string
		reason   string
		priority int
		seedPath string
	}
	byPath := map[string]companionCandidate{}
	add := func(pathValue string, role string, reason string, priority int, seedPath string) {
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" || codeSearchPathExcluded(pathValue, options.ExcludedPaths) || !isLikelyLocalProviderCodeFile(pathValue) || isThirdPartyCodeSearchPath(pathValue) {
			return
		}
		if role == "test_companion" && !allowTests {
			return
		}
		if _, err := os.Stat(filepath.Join(a.workspaceRoot, filepath.FromSlash(pathValue))); err != nil {
			return
		}
		current, ok := byPath[pathValue]
		if ok && current.priority >= priority {
			return
		}
		byPath[pathValue] = companionCandidate{path: pathValue, role: role, reason: reason, priority: priority, seedPath: seedPath}
	}
	for idx, seed := range seeds {
		if idx >= maxInt(limit*4, 32) {
			break
		}
		seedPath := normalizeCodeSearchPath(seed.Path)
		if seedPath == "" || codeSearchPathExcluded(seedPath, options.ExcludedPaths) || !isLikelyLocalProviderCodeFile(seedPath) {
			continue
		}
		if counterpart := productionCounterpartPath(seedPath); counterpart != "" {
			add(counterpart, "production_companion", "production companion for "+seedPath, 87, seedPath)
		}
		for _, companion := range testCompanionPaths(seedPath) {
			add(companion, "test_companion", "test companion for "+seedPath, 91, seedPath)
		}
	}
	candidates := make([]companionCandidate, 0, len(byPath))
	for _, candidate := range byPath {
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].path < candidates[j].path
	})
	maxHits := maxInt(limit, 12)
	if len(candidates) > maxHits {
		candidates = candidates[:maxHits]
	}
	out := make([]rankedCodeSearchHit, 0, len(candidates))
	for _, candidate := range candidates {
		excerpt := a.repoFileExcerpt(candidate.path, 1, 0, 8)
		out = append(out, rankedCodeSearchHit{
			Priority: candidate.priority,
			Hit: contextengine.CodeSearchHit{
				Path:     candidate.path,
				Snippet:  codeProbeSnippet(candidate.reason, excerpt),
				Line:     1,
				Score:    0.97,
				Language: languageFromPath(candidate.path),
				Metadata: map[string]any{
					"candidate_role":           candidate.role,
					"source":                   "local_companion_closure",
					"source_profile":           "repo_code",
					"evidence_class":           "companion",
					"supporting_path":          candidate.seedPath,
					"coverage_terms":           codeSearchCoverageTerms(candidate.path, candidate.reason),
					"coverage_requirement_ids": codeSearchCoverageRequirementIDs(candidate.path, "", candidate.reason, []string{query}),
					"path_family":              filepath.ToSlash(filepath.Dir(candidate.path)),
					"is_test":                  isTestLikeCodeSearchPath(candidate.path),
				},
				Sources: []string{"local_companion_closure"},
			},
		})
	}
	return out
}

func (a *ReadOnlyAdapter) localRouteFamilyClosureHits(seeds []contextengine.CodeSearchHit, limit int, options codeSearchRequestOptions) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" || len(seeds) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	type candidate struct {
		path     string
		role     string
		reason   string
		priority int
		seedPath string
	}
	byPath := map[string]candidate{}
	add := func(pathValue, role, reason string, priority int, seedPath string) {
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" || codeSearchPathExcluded(pathValue, options.ExcludedPaths) || !isLikelyLocalProviderCodeFile(pathValue) || isThirdPartyCodeSearchPath(pathValue) {
			return
		}
		if !a.isRouteFamilyCodeSearchPath(pathValue) {
			return
		}
		if _, err := os.Stat(filepath.Join(a.workspaceRoot, filepath.FromSlash(pathValue))); err != nil {
			return
		}
		current, ok := byPath[pathValue]
		if ok && current.priority >= priority {
			return
		}
		byPath[pathValue] = candidate{path: pathValue, role: role, reason: reason, priority: priority, seedPath: seedPath}
	}
	for idx, seed := range seeds {
		if idx >= maxInt(limit*4, 32) {
			break
		}
		seedPath := normalizeCodeSearchPath(seed.Path)
		if seedPath == "" || codeSearchPathExcluded(seedPath, options.ExcludedPaths) || !a.isRouteFamilyCodeSearchPath(seedPath) {
			continue
		}
		for _, item := range routeFamilyCandidatePaths(seedPath) {
			add(item.path, item.role, item.reason, item.priority, seedPath)
		}
	}
	if len(byPath) == 0 {
		return nil
	}
	candidates := make([]candidate, 0, len(byPath))
	for _, candidate := range byPath {
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].path < candidates[j].path
	})
	maxHits := maxInt(limit, 12)
	if len(candidates) > maxHits {
		candidates = candidates[:maxHits]
	}
	out := make([]rankedCodeSearchHit, 0, len(candidates))
	for _, candidate := range candidates {
		excerpt := a.repoFileExcerpt(candidate.path, 1, 0, 6)
		out = append(out, rankedCodeSearchHit{
			Priority: candidate.priority,
			Hit: contextengine.CodeSearchHit{
				Path:     candidate.path,
				Snippet:  codeProbeSnippet(candidate.reason, excerpt),
				Line:     1,
				Score:    0.90,
				Language: languageFromPath(candidate.path),
				Metadata: map[string]any{
					"candidate_role":           candidate.role,
					"source":                   "local_route_family_closure",
					"source_profile":           "repo_code",
					"evidence_class":           "route_family",
					"supporting_path":          candidate.seedPath,
					"coverage_terms":           codeSearchCoverageTerms(candidate.path, candidate.reason, candidate.seedPath),
					"coverage_requirement_ids": codeSearchCoverageRequirementIDs(candidate.path, "", candidate.reason, []string{candidate.seedPath}),
					"path_family":              routeFamilyPathFamily(candidate.path),
					"file_kind":                "route_file",
					"is_test":                  isTestLikeCodeSearchPath(candidate.path),
				},
				Sources: []string{"local_route_family_closure"},
			},
		})
	}
	return out
}

type routeFamilyCandidatePath struct {
	path     string
	role     string
	reason   string
	priority int
}

func routeFamilyCandidatePaths(seedPath string) []routeFamilyCandidatePath {
	seedPath = normalizeCodeSearchPath(seedPath)
	parts := strings.Split(seedPath, "/")
	rootIdx := routeFamilyRootIndex(parts)
	if rootIdx < 0 || len(parts) <= rootIdx+1 {
		return nil
	}
	dir := filepath.ToSlash(filepath.Dir(seedPath))
	root := strings.Join(parts[:rootIdx+1], "/")
	rootName := parts[rootIdx]
	out := make([]routeFamilyCandidatePath, 0, 12)
	add := func(pathValue, role, reason string, priority int) {
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" || pathValue == seedPath {
			return
		}
		for _, existing := range out {
			if existing.path == pathValue {
				return
			}
		}
		out = append(out, routeFamilyCandidatePath{path: pathValue, role: role, reason: reason, priority: priority})
	}
	for current := dir; current != "." && current != ""; current = filepath.ToSlash(filepath.Dir(current)) {
		if !routeFamilyDirWithinRoot(current, root) {
			break
		}
		add(filepath.ToSlash(filepath.Join(current, "_layout.tsx")), "route_layout", "route layout for "+seedPath, 89)
		add(filepath.ToSlash(filepath.Join(current, "_layout.ts")), "route_layout", "route layout for "+seedPath, 88)
		add(filepath.ToSlash(filepath.Join(current, "layout.tsx")), "route_layout", "route layout for "+seedPath, 87)
		add(filepath.ToSlash(filepath.Join(current, "layout.ts")), "route_layout", "route layout for "+seedPath, 86)
		add(filepath.ToSlash(filepath.Join(current, "index.tsx")), "route_index", "route index for "+seedPath, 85)
		add(filepath.ToSlash(filepath.Join(current, "index.ts")), "route_index", "route index for "+seedPath, 84)
		if current == root && rootName == "pages" {
			add(filepath.ToSlash(filepath.Join(current, "_app.tsx")), "route_layout", "pages app wrapper for "+seedPath, 89)
			add(filepath.ToSlash(filepath.Join(current, "_app.ts")), "route_layout", "pages app wrapper for "+seedPath, 88)
			add(filepath.ToSlash(filepath.Join(current, "_document.tsx")), "route_layout", "pages document wrapper for "+seedPath, 83)
			add(filepath.ToSlash(filepath.Join(current, "_document.ts")), "route_layout", "pages document wrapper for "+seedPath, 82)
		}
		if current == root {
			break
		}
	}
	return out
}

func routeFamilyRootIndex(parts []string) int {
	for i, part := range parts {
		switch part {
		case "app", "pages":
			return i
		}
	}
	return -1
}

func routeFamilyDirWithinRoot(dir, root string) bool {
	dir = strings.Trim(filepath.ToSlash(dir), "/")
	root = strings.Trim(filepath.ToSlash(root), "/")
	return dir == root || strings.HasPrefix(dir, root+"/")
}

func routeFamilyPathFamily(path string) string {
	path = normalizeCodeSearchPath(path)
	parts := strings.Split(path, "/")
	rootIdx := routeFamilyRootIndex(parts)
	if rootIdx < 0 {
		return filepath.ToSlash(filepath.Dir(path))
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	root := strings.Join(parts[:rootIdx+1], "/")
	if !routeFamilyDirWithinRoot(dir, root) {
		return dir
	}
	return dir
}

func (a *ReadOnlyAdapter) isRouteFamilyCodeSearchPath(path string) bool {
	path = normalizeCodeSearchPath(path)
	if path == "" {
		return false
	}
	parts := strings.Split(path, "/")
	rootIdx := routeFamilyRootIndex(parts)
	if rootIdx < 0 || !a.routeFamilyRootHasMarker(parts[:rootIdx+1]) {
		return false
	}
	base := strings.ToLower(filepath.Base(path))
	switch filepath.Ext(base) {
	case ".ts", ".tsx", ".js", ".jsx":
	default:
		return false
	}
	return base == "_layout.tsx" || base == "_layout.ts" ||
		base == "layout.tsx" || base == "layout.ts" ||
		base == "index.tsx" || base == "index.ts" ||
		base == "page.tsx" || base == "page.ts" ||
		base == "_app.tsx" || base == "_app.ts" ||
		base == "_document.tsx" || base == "_document.ts" ||
		strings.Contains(path, "/[")
}

func (a *ReadOnlyAdapter) routeFamilyRootHasMarker(rootParts []string) bool {
	if strings.TrimSpace(a.workspaceRoot) == "" || len(rootParts) == 0 {
		return false
	}
	root := strings.Join(rootParts, "/")
	for _, marker := range []string{
		"_layout.tsx", "_layout.ts", "layout.tsx", "layout.ts",
		"index.tsx", "index.ts", "page.tsx", "page.ts",
		"_app.tsx", "_app.ts", "_document.tsx", "_document.ts",
	} {
		if _, err := os.Stat(filepath.Join(a.workspaceRoot, filepath.FromSlash(root), marker)); err == nil {
			return true
		}
	}
	return false
}

func (a *ReadOnlyAdapter) localImportMountClosureHits(ctx context.Context, seeds []contextengine.CodeSearchHit, limit int, options codeSearchRequestOptions) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" || len(seeds) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	type candidate struct {
		path     string
		line     int
		seedPath string
		needle   string
		score    float64
	}
	byPath := map[string]candidate{}
	add := func(item candidate) {
		item.path = normalizeCodeSearchPath(item.path)
		if item.path == "" || codeSearchPathExcluded(item.path, options.ExcludedPaths) || !isLikelyLocalProviderCodeFile(item.path) || isThirdPartyCodeSearchPath(item.path) {
			return
		}
		if !options.IncludeTests && isTestLikeCodeSearchPath(item.path) {
			return
		}
		if isLikelyGeneratedProtocolBindingPath(item.path) {
			return
		}
		current, ok := byPath[item.path]
		if ok && current.score >= item.score {
			return
		}
		byPath[item.path] = item
	}
	for idx, seed := range seeds {
		if idx >= maxInt(limit*3, 24) {
			break
		}
		seedPath := normalizeCodeSearchPath(seed.Path)
		if seedPath == "" || codeSearchPathExcluded(seedPath, options.ExcludedPaths) || !isLikelyLocalProviderCodeFile(seedPath) {
			continue
		}
		if isLikelyGeneratedProtocolBindingPath(seedPath) && !codeSearchHitHasCandidateRole(seed, "generated_protocol_binding") {
			continue
		}
		needles := reverseReferenceNeedles(seedPath, nil)
		if len(needles) == 0 {
			continue
		}
		args := []string{
			"-n", "--no-heading", "--color", "never", "--fixed-strings", "-i",
			"--glob", "!node_modules/**", "--glob", "!dist/**", "--glob", "!build/**",
			"--glob", "!_build/**", "--glob", "!deps/**", "--glob", "!vendor/**",
		}
		for _, needle := range needles {
			args = append(args, "-e", needle.lower)
		}
		args = append(args, a.workspaceRoot)
		output, err := exec.CommandContext(ctx, "rg", args...).Output()
		if err != nil && len(output) == 0 {
			continue
		}
		seenForSeed := map[string]struct{}{}
		for _, line := range strings.Split(string(output), "\n") {
			if len(seenForSeed) >= 4 {
				break
			}
			pathValue, lineNo, text, ok := parseRipgrepLine(a.workspaceRoot, line)
			if !ok {
				continue
			}
			pathValue = normalizeCodeSearchPath(pathValue)
			if pathValue == "" || pathValue == seedPath || codeSearchPathExcluded(pathValue, options.ExcludedPaths) || !isLikelyLocalProviderCodeFile(pathValue) || isThirdPartyCodeSearchPath(pathValue) {
				continue
			}
			if _, ok := seenForSeed[pathValue]; ok {
				continue
			}
			lowerText := strings.ToLower(text)
			matched := ""
			for _, needle := range needles {
				if strings.Contains(lowerText, needle.lower) {
					matched = needle.lower
					break
				}
			}
			if matched == "" {
				continue
			}
			seenForSeed[pathValue] = struct{}{}
			score := 0.95
			if strings.Contains(strings.ToLower(pathValue), "/test") || strings.Contains(strings.ToLower(pathValue), ".test.") {
				score -= 0.03
			}
			add(candidate{path: pathValue, line: lineNo, seedPath: seedPath, needle: matched, score: score})
		}
	}
	items := make([]candidate, 0, len(byPath))
	for _, item := range byPath {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].path < items[j].path
	})
	maxHits := maxInt(limit, 12)
	if len(items) > maxHits {
		items = items[:maxHits]
	}
	out := make([]rankedCodeSearchHit, 0, len(items))
	for _, item := range items {
		excerpt := a.repoFileExcerpt(item.path, item.line, 2, 5)
		reason := "import/mount reference to " + item.seedPath
		out = append(out, rankedCodeSearchHit{
			Priority: 86,
			Hit: contextengine.CodeSearchHit{
				Path:     item.path,
				Snippet:  codeProbeSnippet(reason, excerpt),
				Line:     item.line,
				Score:    item.score,
				Language: languageFromPath(item.path),
				Metadata: map[string]any{
					"candidate_role":           "import_reference",
					"source":                   "local_import_mount_closure",
					"source_profile":           "repo_code",
					"evidence_class":           "import_reference",
					"supporting_path":          item.seedPath,
					"coverage_terms":           codeSearchCoverageTerms(item.path, item.seedPath, item.needle),
					"coverage_requirement_ids": codeSearchCoverageRequirementIDs(item.path, "", reason, []string{item.seedPath}),
					"path_family":              filepath.ToSlash(filepath.Dir(item.path)),
					"is_test":                  isTestLikeCodeSearchPath(item.path),
				},
				Sources: []string{"local_import_mount_closure"},
			},
		})
	}
	return out
}

func (a *ReadOnlyAdapter) localModuleEntrypointClosureHits(seeds []contextengine.CodeSearchHit, limit int, options codeSearchRequestOptions) []rankedCodeSearchHit {
	if strings.TrimSpace(a.workspaceRoot) == "" || len(seeds) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	type candidate struct {
		path     string
		seedPath string
		score    float64
	}
	seedSet := map[string]struct{}{}
	byPath := map[string]candidate{}
	add := func(pathValue string, seedPath string, score float64) {
		pathValue = normalizeCodeSearchPath(pathValue)
		seedPath = normalizeCodeSearchPath(seedPath)
		if pathValue == "" || pathValue == seedPath || codeSearchPathExcluded(pathValue, options.ExcludedPaths) || !isLikelyModuleEntrypointPath(pathValue) || isThirdPartyCodeSearchPath(pathValue) {
			return
		}
		if !options.IncludeTests && isTestLikeCodeSearchPath(pathValue) {
			return
		}
		if isLikelyGeneratedProtocolBindingPath(pathValue) {
			return
		}
		if _, err := os.Stat(filepath.Join(a.workspaceRoot, filepath.FromSlash(pathValue))); err != nil {
			return
		}
		current, ok := byPath[pathValue]
		if ok && current.score >= score {
			return
		}
		byPath[pathValue] = candidate{path: pathValue, seedPath: seedPath, score: score}
	}
	for idx, seed := range seeds {
		if idx >= maxInt(limit*4, 32) {
			break
		}
		seedPath := normalizeCodeSearchPath(seed.Path)
		if seedPath == "" || codeSearchPathExcluded(seedPath, options.ExcludedPaths) || !isLikelyModuleEntrypointSeedPath(seedPath) || isThirdPartyCodeSearchPath(seedPath) {
			continue
		}
		if isLikelyGeneratedProtocolBindingPath(seedPath) && !codeSearchHitHasCandidateRole(seed, "generated_protocol_binding") {
			continue
		}
		seedSet[seedPath] = struct{}{}
	}
	for idx, seed := range seeds {
		if idx >= maxInt(limit*4, 32) {
			break
		}
		seedPath := normalizeCodeSearchPath(seed.Path)
		if seedPath == "" {
			continue
		}
		if isLikelyGeneratedProtocolBindingPath(seedPath) && !codeSearchHitHasCandidateRole(seed, "generated_protocol_binding") {
			continue
		}
		score := 0.92
		dirs := moduleEntrypointCandidateDirs(seedPath)
		for dirIdx, dir := range dirs {
			for _, entry := range moduleEntrypointNamesForPath(seedPath) {
				candidatePath := filepath.ToSlash(filepath.Join(dir, entry))
				add(candidatePath, seedPath, score-(float64(dirIdx)*0.03))
			}
		}
	}
	items := make([]candidate, 0, len(byPath))
	for _, item := range byPath {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].path < items[j].path
	})
	maxHits := maxInt(limit, 12)
	if len(items) > maxHits {
		items = items[:maxHits]
	}
	out := make([]rankedCodeSearchHit, 0, len(items))
	for _, item := range items {
		reason := "module entrypoint companion for " + item.seedPath
		out = append(out, rankedCodeSearchHit{
			Priority: 84,
			Hit: contextengine.CodeSearchHit{
				Path:     item.path,
				Snippet:  codeProbeSnippet(reason, a.repoFileExcerpt(item.path, 1, 0, 8)),
				Line:     1,
				Score:    item.score,
				Language: languageFromPath(item.path),
				Metadata: map[string]any{
					"candidate_role":           "module_entrypoint",
					"source":                   "local_module_entrypoint_closure",
					"source_profile":           "repo_code",
					"evidence_class":           "module_entrypoint",
					"supporting_path":          item.seedPath,
					"coverage_terms":           codeSearchCoverageTerms(item.path, item.seedPath, reason),
					"coverage_requirement_ids": codeSearchCoverageRequirementIDs(item.path, "", reason, append([]string{item.seedPath}, options.RequiredEvidence...)),
					"path_family":              filepath.ToSlash(filepath.Dir(item.path)),
					"is_test":                  isTestLikeCodeSearchPath(item.path),
				},
				Sources: []string{"local_module_entrypoint_closure"},
			},
		})
	}
	return out
}

func moduleEntrypointCandidateDirs(pathValue string) []string {
	dir := filepath.ToSlash(filepath.Dir(normalizeCodeSearchPath(pathValue)))
	if dir == "." || dir == "" {
		return nil
	}
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for len(out) < 4 && dir != "." && dir != "" {
		if _, ok := seen[dir]; !ok {
			seen[dir] = struct{}{}
			out = append(out, dir)
		}
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent == "." || parent == "" || parent == dir {
			break
		}
		dir = parent
	}
	return out
}

func moduleEntrypointNamesForPath(pathValue string) []string {
	switch strings.ToLower(filepath.Ext(pathValue)) {
	case ".rs":
		return []string{"lib.rs", "mod.rs", "main.rs"}
	case ".ts", ".tsx", ".js", ".jsx":
		return []string{
			"index.ts", "index.tsx", "index.js", "index.jsx",
			"router.ts", "router.tsx", "router.js", "router.jsx",
			"routes.ts", "routes.tsx", "routes.js", "routes.jsx",
			"main.ts", "main.tsx", "main.js", "main.jsx",
		}
	case ".py":
		return []string{"__init__.py", "main.py", "dependencies.py"}
	case ".go":
		return []string{"main.go"}
	case ".ex", ".exs":
		return []string{"application.ex", "router.ex", "endpoint.ex"}
	case ".cs":
		return []string{"Program.cs", "Startup.cs"}
	default:
		return nil
	}
}

func isLikelyModuleEntrypointSeedPath(pathValue string) bool {
	switch strings.ToLower(filepath.Ext(normalizeCodeSearchPath(pathValue))) {
	case ".rs", ".ts", ".tsx", ".js", ".jsx", ".py", ".go", ".ex", ".exs", ".cs":
		return true
	default:
		return false
	}
}

func isLikelyModuleEntrypointPath(pathValue string) bool {
	base := filepath.Base(normalizeCodeSearchPath(pathValue))
	for _, name := range []string{
		"lib.rs", "mod.rs", "main.rs",
		"index.ts", "index.tsx", "index.js", "index.jsx",
		"router.ts", "router.tsx", "router.js", "router.jsx",
		"routes.ts", "routes.tsx", "routes.js", "routes.jsx",
		"main.ts", "main.tsx", "main.js", "main.jsx",
		"__init__.py", "main.py", "dependencies.py", "main.go",
		"application.ex", "router.ex", "endpoint.ex",
		"Program.cs", "Startup.cs",
	} {
		if base == name {
			return true
		}
	}
	return false
}

func requiredEvidenceSuggestsTests(requiredEvidence []string) bool {
	for _, item := range cleanStringList(requiredEvidence) {
		for _, term := range strings.FieldsFunc(strings.ToLower(item), func(r rune) bool {
			switch {
			case r >= 'a' && r <= 'z':
				return false
			case r >= '0' && r <= '9':
				return false
			default:
				return true
			}
		}) {
			switch term {
			case "test", "tests", "testing", "spec", "specs", "fixture", "fixtures":
				return true
			}
		}
	}
	return false
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

func (a *ReadOnlyAdapter) localSubsystemSiblingClosureHits(ctx context.Context, query string, taskType string, requiredEvidence []string, seeds []contextengine.CodeSearchHit, limit int, options codeSearchRequestOptions, budget *localProviderBudget) []rankedCodeSearchHit {
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
		if pathValue == "" || codeSearchPathExcluded(pathValue, options.ExcludedPaths) || !isLikelyLocalProviderCodeFile(pathValue) {
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
		if budget.isCapped() {
			break
		}
		dir := dirCandidate.dir
		if codeSearchPathExcluded(dir, options.ExcludedPaths) {
			continue
		}
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
			if codeSearchPathExcluded(pathValue, options.ExcludedPaths) {
				continue
			}
			if _, ok := seedPathSet[pathValue]; ok {
				continue
			}
			if !isLikelyLocalProviderCodeFile(pathValue) {
				continue
			}
			if err := budget.beforeFile(ctx); err != nil {
				break
			}
			info, statErr := entry.Info()
			if statErr == nil && info.Size() > 1_000_000 {
				continue
			}
			if statErr == nil {
				if err := budget.recordFile(info.Size()); err != nil {
					break
				}
			} else if err := budget.recordFile(0); err != nil {
				break
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
			if err := budget.recordHit(); err != nil {
				break
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
		strings.HasPrefix(line, "def "),
		strings.HasPrefix(line, "public "), strings.HasPrefix(line, "private "),
		strings.HasPrefix(line, "protected "), strings.HasPrefix(line, "internal "):
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
		strings.HasSuffix(base, "test.cs"),
		strings.HasSuffix(base, "tests.cs"),
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
		case "test", "tests", "__tests__", "testdata", "fixtures":
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
	lower := strings.ToLower(pathValue)
	switch {
	case strings.HasSuffix(lower, "_test.go"):
		return strings.TrimSuffix(pathValue, "_test.go") + ".go"
	case strings.HasSuffix(lower, "tests.cs"):
		return pathValue[:len(pathValue)-len("Tests.cs")] + ".cs"
	case strings.HasSuffix(lower, "test.cs"):
		return pathValue[:len(pathValue)-len("Test.cs")] + ".cs"
	default:
		return ""
	}
}

func testCompanionPaths(pathValue string) []string {
	pathValue = normalizeCodeSearchPath(pathValue)
	if pathValue == "" || isTestLikeCodeSearchPath(pathValue) {
		return nil
	}
	ext := filepath.Ext(pathValue)
	stem := strings.TrimSuffix(pathValue, ext)
	var suffixes []string
	switch ext {
	case ".go":
		suffixes = []string{"_test.go"}
	case ".ts", ".tsx", ".js", ".jsx":
		suffixes = []string{".test" + ext, ".spec" + ext}
	case ".py":
		suffixes = []string{"_test.py"}
	case ".ex", ".exs":
		suffixes = []string{"_test.exs"}
	case ".cs":
		suffixes = []string{"Test.cs", "Tests.cs"}
	default:
		return nil
	}
	candidates := make([]string, 0, 8)
	for _, suffix := range suffixes {
		candidates = appendUniqueStringEnv(candidates, stem+suffix)
	}
	if ext == ".cs" {
		dir := filepath.ToSlash(filepath.Dir(pathValue))
		base := strings.TrimSuffix(filepath.Base(pathValue), ext)
		if dir != "." && dir != "" {
			for _, suffix := range suffixes {
				candidates = appendUniqueStringEnv(candidates, dir+"Tests/"+base+suffix)
			}
			parent := filepath.ToSlash(filepath.Dir(dir))
			dirBase := filepath.Base(dir)
			if parent == "." {
				parent = ""
			} else if parent != "" {
				parent += "/"
			}
			for _, suffix := range suffixes {
				candidates = appendUniqueStringEnv(candidates, parent+dirBase+"Tests/"+base+suffix)
			}
		}
	}
	parts := strings.Split(pathValue, "/")
	for i, part := range parts {
		if part != "src" && part != "lib" && part != "app" {
			continue
		}
		prefix := strings.Join(parts[:i], "/")
		if prefix != "" {
			prefix += "/"
		}
		rest := strings.Join(parts[i+1:], "/")
		if rest == "" {
			continue
		}
		restStem := strings.TrimSuffix(rest, ext)
		base := filepath.Base(restStem)
		dir := filepath.ToSlash(filepath.Dir(restStem))
		if dir == "." {
			dir = ""
		} else if dir != "" {
			dir += "/"
		}
		for _, testRoot := range []string{"test", "tests", "__tests__"} {
			for _, suffix := range suffixes {
				candidates = appendUniqueStringEnv(candidates, prefix+testRoot+"/"+dir+base+suffix)
			}
			if ext == ".py" {
				candidates = appendUniqueStringEnv(candidates, prefix+testRoot+"/"+dir+"test_"+base+".py")
			}
		}
	}
	return candidates
}

type reverseReferenceHit struct {
	path   string
	line   int
	symbol string
	reason string
}

func (a *ReadOnlyAdapter) localReverseReferenceHits(ctx context.Context, seedPath string, symbols []string, limit int, excludedPaths []string) []reverseReferenceHit {
	if strings.TrimSpace(a.workspaceRoot) == "" || seedPath == "" || limit <= 0 {
		return nil
	}
	needles := reverseReferenceNeedles(seedPath, symbols)
	if len(needles) == 0 {
		return nil
	}
	seedPath = normalizeCodeSearchPath(seedPath)
	var out []reverseReferenceHit
	args := []string{
		"-n", "--no-heading", "--color", "never", "--fixed-strings", "-i",
		"--glob", "!node_modules/**", "--glob", "!dist/**", "--glob", "!build/**",
		"--glob", "!_build/**", "--glob", "!deps/**", "--glob", "!vendor/**",
	}
	for _, needle := range needles {
		args = append(args, "-e", needle.lower)
	}
	args = append(args, a.workspaceRoot)
	output, err := exec.CommandContext(ctx, "rg", args...).Output()
	if err != nil && len(output) == 0 {
		return nil
	}
	needleByValue := map[string]reverseReferenceNeedle{}
	for _, needle := range needles {
		needleByValue[needle.lower] = needle
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(string(output), "\n") {
		if len(out) >= limit {
			break
		}
		pathValue, lineNo, text, ok := parseRipgrepLine(a.workspaceRoot, line)
		if !ok {
			continue
		}
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" || pathValue == seedPath || codeSearchPathExcluded(pathValue, excludedPaths) || !isLikelyLocalProviderCodeFile(pathValue) || isThirdPartyCodeSearchPath(pathValue) {
			continue
		}
		if _, ok := seen[pathValue]; ok {
			continue
		}
		lowerText := strings.ToLower(text)
		var matched reverseReferenceNeedle
		for _, needle := range needles {
			if strings.Contains(lowerText, needle.lower) {
				matched = needleByValue[needle.lower]
				break
			}
		}
		if matched.lower == "" {
			continue
		}
		seen[pathValue] = struct{}{}
		reason := "reverse reference to " + seedPath
		if matched.symbol != "" {
			reason += " via " + matched.symbol
		}
		out = append(out, reverseReferenceHit{path: pathValue, line: lineNo, symbol: matched.symbol, reason: reason})
	}
	return out
}

func parseRipgrepLine(workspaceRoot string, line string) (string, int, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", 0, "", false
	}
	parts := strings.SplitN(line, ":", 3)
	if len(parts) < 3 {
		return "", 0, "", false
	}
	lineNo := 0
	_, _ = fmt.Sscanf(parts[1], "%d", &lineNo)
	pathValue := parts[0]
	if filepath.IsAbs(pathValue) {
		if rel, err := filepath.Rel(workspaceRoot, pathValue); err == nil {
			pathValue = rel
		}
	}
	return pathValue, lineNo, parts[2], true
}

type reverseReferenceNeedle struct {
	lower  string
	symbol string
}

func reverseReferenceNeedles(seedPath string, symbols []string) []reverseReferenceNeedle {
	seedPath = normalizeCodeSearchPath(seedPath)
	seen := map[string]struct{}{}
	out := make([]reverseReferenceNeedle, 0, 12)
	add := func(value string, symbol string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) < 4 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, reverseReferenceNeedle{lower: value, symbol: symbol})
	}
	noExt := strings.TrimSuffix(seedPath, filepath.Ext(seedPath))
	add(noExt, "")
	for _, marker := range []string{"/src/", "/lib/", "/app/"} {
		if idx := strings.Index(noExt, marker); idx >= 0 {
			add(noExt[idx+1:], "")
		}
	}
	parts := strings.Split(noExt, "/")
	for width := 2; width <= 3; width++ {
		if len(parts) >= width {
			add(strings.Join(parts[len(parts)-width:], "/"), "")
		}
	}
	stem := ""
	if len(parts) > 0 {
		stem = parts[len(parts)-1]
	}
	if len(stem) >= 4 {
		switch strings.ToLower(filepath.Ext(seedPath)) {
		case ".ts", ".tsx", ".js", ".jsx", ".rs":
			add("./"+stem, "")
			add("../"+stem, "")
		case ".py":
			add("."+stem, "")
			add("import "+stem, "")
			dotted := strings.ReplaceAll(noExt, "/", ".")
			add(dotted, "")
			if len(parts) >= 2 {
				add(strings.ReplaceAll(strings.Join(parts[len(parts)-2:], "/"), "/", "."), "")
			}
		case ".ex", ".exs":
			add(stem, "")
		}
	}
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if len(symbol) < 4 || isGenericRelatedSymbol(symbol) {
			continue
		}
		add(symbol, symbol)
	}
	return out
}

func codeSearchHitHasCandidateRole(hit contextengine.CodeSearchHit, role string) bool {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" {
		return false
	}
	return strings.TrimSpace(strings.ToLower(metadataStringEnv(hit.Metadata, "candidate_role"))) == role
}

func isLikelyGeneratedProtocolBindingPath(pathValue string) bool {
	pathValue = strings.ToLower(normalizeCodeSearchPath(pathValue))
	if pathValue == "" {
		return false
	}
	base := filepath.Base(pathValue)
	switch {
	case strings.HasSuffix(base, ".pb.go"),
		strings.HasSuffix(base, ".pb.ts"),
		strings.HasSuffix(base, ".pb.tsx"),
		strings.HasSuffix(base, ".pb.js"),
		strings.HasSuffix(base, ".pb.jsx"),
		strings.HasSuffix(base, "_pb.ts"),
		strings.HasSuffix(base, "_pb.js"),
		strings.HasSuffix(base, "_pb2.py"),
		strings.HasSuffix(base, "_pb2_grpc.py"),
		strings.HasSuffix(base, ".grpc.go"),
		strings.HasSuffix(base, ".grpc.ts"),
		strings.HasSuffix(base, ".grpc.js"),
		strings.HasSuffix(base, "_grpc.pb.go"),
		strings.HasSuffix(base, "_grpc_pb.js"),
		strings.HasSuffix(base, ".pb.ex"),
		strings.HasSuffix(base, ".grpc.ex"):
		return true
	}
	if strings.Contains(pathValue, "/generated/") || strings.Contains(pathValue, "/gen/") {
		return strings.Contains(pathValue, "proto") ||
			strings.Contains(pathValue, "protobuf") ||
			strings.Contains(pathValue, "grpc") ||
			strings.Contains(pathValue, "/pb/") ||
			strings.Contains(base, ".pb.")
	}
	return false
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

type localDefinitionRef struct {
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

func (a *ReadOnlyAdapter) findLocalDefinitions(ctx context.Context, symbols []string, budget *localProviderBudget) map[string]localDefinitionRef {
	wanted := map[string]struct{}{}
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			continue
		}
		wanted[symbol] = struct{}{}
	}
	out := map[string]localDefinitionRef{}
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
		if !isLocalDefinitionSearchFile(rel) || isTestLikeCodeSearchPath(rel) {
			return nil
		}
		if err := budget.beforeFile(ctx); err != nil {
			return err
		}
		info, statErr := d.Info()
		if statErr == nil && info.Size() > 1_000_000 {
			return nil
		}
		if statErr == nil {
			if err := budget.recordFile(info.Size()); err != nil {
				return err
			}
		} else if err := budget.recordFile(0); err != nil {
			return err
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
			if line := findLocalDefinitionLine(rel, text, symbol); line > 0 {
				out[symbol] = localDefinitionRef{Path: rel, Line: line}
				if err := budget.recordHit(); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return out
}

func isLocalDefinitionSearchFile(pathValue string) bool {
	switch strings.ToLower(filepath.Ext(pathValue)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".ex", ".exs", ".cs":
		return true
	default:
		return false
	}
}

func isLikelyLocalProviderCodeFile(pathValue string) bool {
	switch strings.ToLower(filepath.Ext(pathValue)) {
	case ".rs":
		return true
	case ".cs":
		return true
	default:
		return isLikelyTextCodeFile(pathValue)
	}
}

func findLocalDefinitionLine(pathValue string, text string, symbol string) int {
	switch strings.ToLower(filepath.Ext(pathValue)) {
	case ".go":
		return findGoDefinitionLine(text, symbol)
	case ".ts", ".tsx", ".js", ".jsx":
		return findJavaScriptDefinitionLine(text, symbol)
	case ".py":
		return findPythonDefinitionLine(text, symbol)
	case ".ex", ".exs":
		return findElixirDefinitionLine(text, symbol)
	case ".cs":
		return findCSharpDefinitionLine(text, symbol)
	default:
		return 0
	}
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

func findJavaScriptDefinitionLine(text, symbol string) int {
	return firstLineMatchingDefinition(text, []string{
		"function " + symbol + "(",
		"async function " + symbol + "(",
		"export function " + symbol + "(",
		"export async function " + symbol + "(",
		"class " + symbol,
		"export class " + symbol,
		"interface " + symbol,
		"export interface " + symbol,
		"type " + symbol,
		"export type " + symbol,
		"const " + symbol,
		"let " + symbol,
		"var " + symbol,
		"export const " + symbol,
		"export let " + symbol,
		"export var " + symbol,
	})
}

func findPythonDefinitionLine(text, symbol string) int {
	return firstLineMatchingDefinition(text, []string{
		"def " + symbol + "(",
		"async def " + symbol + "(",
		"class " + symbol,
	})
}

func findElixirDefinitionLine(text, symbol string) int {
	return firstLineMatchingDefinition(text, []string{
		"def " + symbol + "(",
		"defp " + symbol + "(",
		"defmacro " + symbol + "(",
		"defmodule " + symbol,
		"defmodule " + strings.TrimPrefix(symbol, "Elixir."),
		"defmodule ",
	})
}

func findCSharpDefinitionLine(text, symbol string) int {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return 0
	}
	quoted := regexp.QuoteMeta(symbol)
	typePattern := regexp.MustCompile(`^(?:\[[^\]]+\]\s*)*(?:(?:public|private|protected|internal|static|sealed|abstract|partial|readonly|unsafe|new)\s+)*(?:class|interface|struct|enum|record(?:\s+(?:class|struct))?)\s+` + quoted + `\b`)
	memberPattern := regexp.MustCompile(`^(?:\[[^\]]+\]\s*)*(?:(?:public|private|protected|internal|static|virtual|override|abstract|sealed|partial|readonly|async|extern|unsafe|new|required)\s+)*(?:(?:[A-Za-z_][A-Za-z0-9_<>,\[\].?]*\s+)+)?` + quoted + `\s*(?:\(|\{|=>)`)
	lines := strings.Split(text, "\n")
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if isLikelyCSharpStatementLine(trimmed) {
			continue
		}
		if typePattern.MatchString(trimmed) || memberPattern.MatchString(trimmed) {
			return idx + 1
		}
	}
	return 0
}

func isLikelyCSharpStatementLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, prefix := range []string{"return ", "if ", "while ", "for ", "foreach ", "switch ", "case ", "await ", "throw ", "new "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func firstLineMatchingDefinition(text string, patterns []string) int {
	lines := strings.Split(text, "\n")
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, pattern := range patterns {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" {
				continue
			}
			if pattern == "defmodule " {
				continue
			}
			if strings.HasPrefix(trimmed, pattern) {
				return idx + 1
			}
		}
	}
	return 0
}

func workspaceCodeProbeSearch(ctx context.Context, workspaceRoot string, pathProbes, exactProbes []string, perProbeLimit int, excludedPaths []string, budget *localProviderBudget) ([]localPathProbeHit, []localExactProbeHit, error) {
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
			if current != workspaceRoot {
				rel, relErr := filepath.Rel(workspaceRoot, current)
				if relErr == nil && codeSearchPathExcluded(rel, excludedPaths) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, relErr := filepath.Rel(workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if codeSearchPathExcluded(rel, excludedPaths) || !isLikelyLocalProviderCodeFile(rel) {
			return nil
		}
		if err := budget.beforeFile(ctx); err != nil {
			return err
		}
		info, statErr := d.Info()
		if statErr == nil && info.Size() > 1_000_000 {
			return nil
		}
		if statErr == nil {
			if err := budget.recordFile(info.Size()); err != nil {
				return err
			}
		} else if err := budget.recordFile(0); err != nil {
			return err
		}
		lowerRel := strings.ToLower(rel)
		for _, probe := range normalizedPathProbes {
			if pathCounts[probe] >= perProbeLimit || !strings.Contains(lowerRel, probe) {
				continue
			}
			pathHits = append(pathHits, localPathProbeHit{Path: rel, Probe: probe})
			pathCounts[probe]++
			if err := budget.recordHit(); err != nil {
				return err
			}
		}
		if len(normalizedExactProbes) == 0 {
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
			if err := budget.recordHit(); err != nil {
				return err
			}
		}
		return nil
	})
	if err = budget.cappedError(err); err != nil {
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

type mergedCodeSearchHit struct {
	hit      contextengine.CodeSearchHit
	priority int
}

func mergeCodeSearchHitsWithOptions(limit int, options codeSearchRequestOptions, repoHits []contextengine.CodeSearchHit, rankedGroups ...[]rankedCodeSearchHit) []contextengine.CodeSearchHit {
	if limit <= 0 {
		limit = 8
	}
	byKey := map[string]mergedCodeSearchHit{}
	add := func(hit contextengine.CodeSearchHit, priority int) {
		hit.Path = normalizeCodeSearchPath(hit.Path)
		hit.Symbol = strings.TrimSpace(hit.Symbol)
		if hit.Path == "" && hit.Symbol == "" {
			return
		}
		if hit.Language == "" {
			hit.Language = languageFromPath(hit.Path)
		}
		hit = annotateCodeSearchHitRoleBuckets(hit)
		key := hit.Path + "|" + hit.Symbol
		existing, ok := byKey[key]
		if !ok {
			byKey[key] = mergedCodeSearchHit{hit: hit, priority: priority}
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
			byKey[key] = mergedCodeSearchHit{hit: hit, priority: priority}
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
	out := make([]mergedCodeSearchHit, 0, len(byKey))
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
	if len(options.RequiredEvidence) > 0 {
		for _, item := range coverageAdmissionHits(out, options, limit) {
			hits = append(hits, item.hit)
		}
	}
	added := map[string]struct{}{}
	for _, hit := range hits {
		added[codeSearchHitMergeKey(hit)] = struct{}{}
	}
	for _, item := range out {
		if !codeSearchHitMatchesOptions(item.hit, options) {
			continue
		}
		if _, ok := added[codeSearchHitMergeKey(item.hit)]; ok {
			continue
		}
		hits = append(hits, item.hit)
		added[codeSearchHitMergeKey(item.hit)] = struct{}{}
		if len(hits) >= limit {
			break
		}
	}
	return hits
}

func appendMissingRankedCodeSearchHits(hits []contextengine.CodeSearchHit, limit int, options codeSearchRequestOptions, rankedHits []rankedCodeSearchHit) []contextengine.CodeSearchHit {
	if limit <= 0 || len(rankedHits) == 0 {
		return hits
	}
	added := map[string]struct{}{}
	uniquePaths := map[string]struct{}{}
	for _, hit := range hits {
		added[codeSearchHitMergeKey(hit)] = struct{}{}
		if pathValue := normalizeCodeSearchPath(hit.Path); pathValue != "" {
			uniquePaths[pathValue] = struct{}{}
		}
	}
	if len(uniquePaths) >= limit {
		return hits
	}
	for _, ranked := range rankedHits {
		hit := ranked.Hit
		hit.Path = normalizeCodeSearchPath(hit.Path)
		if hit.Path == "" && strings.TrimSpace(hit.Symbol) == "" {
			continue
		}
		if !codeSearchHitMatchesOptions(hit, options) {
			continue
		}
		key := codeSearchHitMergeKey(hit)
		if _, ok := added[key]; ok {
			continue
		}
		hits = append(hits, annotateCodeSearchHitRoleBuckets(hit))
		added[key] = struct{}{}
		if hit.Path != "" {
			uniquePaths[hit.Path] = struct{}{}
		}
		if len(uniquePaths) >= limit {
			break
		}
	}
	return hits
}

func coverageAdmissionHits(out []mergedCodeSearchHit, options codeSearchRequestOptions, limit int) []mergedCodeSearchHit {
	if limit <= 0 || len(out) == 0 || len(options.RequiredEvidence) == 0 {
		return nil
	}
	maxReserved := minInt(limit, maxInt(1, len(cleanStringList(options.RequiredEvidence))))
	selected := make([]mergedCodeSearchHit, 0, maxReserved)
	selectedKeys := map[string]struct{}{}
	for _, requirement := range cleanStringList(options.RequiredEvidence) {
		if len(selected) >= maxReserved {
			break
		}
		var best mergedCodeSearchHit
		bestScore := -1.0
		for _, item := range out {
			if !codeSearchHitMatchesOptions(item.hit, options) {
				continue
			}
			key := codeSearchHitMergeKey(item.hit)
			if _, ok := selectedKeys[key]; ok {
				continue
			}
			score := coverageAdmissionScore(item, requirement)
			if score <= 0 {
				continue
			}
			if score > bestScore || (score == bestScore && coverageAdmissionLess(item, best)) {
				best = item
				bestScore = score
			}
		}
		if bestScore <= 0 {
			continue
		}
		selected = append(selected, best)
		selectedKeys[codeSearchHitMergeKey(best.hit)] = struct{}{}
	}
	return selected
}

func coverageAdmissionScore(item mergedCodeSearchHit, requirement string) float64 {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" {
		return 0
	}
	ids := codeSearchCoverageRequirementIDs("", "", requirement, []string{requirement})
	score := 0.0
	for _, id := range ids {
		if stringSliceHas(metadataStringSliceEnv(item.hit.Metadata, "coverage_requirement_ids"), id) {
			score += 10
		}
	}
	if directCoverageAnchorHit(item.hit, requirement) {
		score += 6
	}
	if score <= 0 {
		return 0
	}
	score += float64(item.priority) / 100
	score += item.hit.Score / 10
	if strings.TrimSpace(item.hit.Symbol) != "" {
		score += 1
	}
	role := strings.TrimSpace(strings.ToLower(metadataStringEnv(item.hit.Metadata, "candidate_role")))
	switch role {
	case "symbol_definition", "required_path_support":
		score += 1
	case "test_support", "test_companion":
		score -= 1
	}
	return score
}

func directCoverageAnchorHit(hit contextengine.CodeSearchHit, requirement string) bool {
	needle := compactCoverageAdmissionAnchor(requirement)
	if needle == "" {
		return false
	}
	anchors := []string{
		strings.TrimSuffix(filepath.Base(hit.Path), filepath.Ext(hit.Path)),
		hit.Symbol,
		metadataStringEnv(hit.Metadata, "symbol"),
	}
	for _, anchor := range anchors {
		if compactCoverageAdmissionAnchor(anchor) == needle {
			return true
		}
	}
	return false
}

func compactCoverageAdmissionAnchor(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func coverageAdmissionLess(left, right mergedCodeSearchHit) bool {
	if right.hit.Path == "" && right.hit.Symbol == "" {
		return true
	}
	if left.priority != right.priority {
		return left.priority > right.priority
	}
	if left.hit.Score != right.hit.Score {
		return left.hit.Score > right.hit.Score
	}
	return left.hit.Path < right.hit.Path
}

func codeSearchHitMergeKey(hit contextengine.CodeSearchHit) string {
	return normalizeCodeSearchPath(hit.Path) + "|" + strings.TrimSpace(hit.Symbol)
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
			case "matched_terms", "coverage_terms", "coverage_requirement_ids", "sources", "role_buckets":
				merged := metadataStringSliceEnv(base.Metadata, key)
				merged = appendUniqueStringsEnv(merged, metadataStringSliceEnv(extra.Metadata, key)...)
				if len(merged) > 0 {
					base.Metadata[key] = merged
				}
			case "candidate_role":
				base.Metadata[key] = preferredMergedCandidateRole(metadataStringEnv(base.Metadata, key), metadataStringEnv(extra.Metadata, key))
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

func preferredMergedCandidateRole(baseRole string, extraRole string) string {
	baseRole = strings.TrimSpace(baseRole)
	extraRole = strings.TrimSpace(extraRole)
	if baseRole == "" {
		return extraRole
	}
	if extraRole == "" {
		return baseRole
	}
	if candidateRoleSpecificity(extraRole) > candidateRoleSpecificity(baseRole) {
		return extraRole
	}
	return baseRole
}

func candidateRoleSpecificity(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "test_data_support", "config_support", "data_support", "documentation_anchor", "documentation_map", "route_layout", "route_index":
		return 5
	case "symbol_definition", "required_path_support", "direct_dispatch_file", "import_reference", "module_entrypoint":
		return 4
	case "production_companion", "test_companion", "definition_support":
		return 3
	case "primary_anchor", "repo_index_anchor":
		return 2
	case "structural_support", "semantic_file_candidate":
		return 1
	default:
		if role != "" {
			return 2
		}
		return 0
	}
}

func annotateCodeSearchHitRoleBuckets(hit contextengine.CodeSearchHit) contextengine.CodeSearchHit {
	buckets := codeSearchHitRoleBuckets(hit)
	if len(buckets) == 0 {
		return hit
	}
	if hit.Metadata == nil {
		hit.Metadata = map[string]any{}
	}
	hit.Metadata["role_buckets"] = appendUniqueStringsEnv(metadataStringSliceEnv(hit.Metadata, "role_buckets"), buckets...)
	return hit
}

func codeSearchHitRoleBuckets(hit contextengine.CodeSearchHit) []string {
	var buckets []string
	pathValue := strings.ToLower(filepath.ToSlash(strings.TrimSpace(hit.Path)))
	role := strings.ToLower(metadataStringEnv(hit.Metadata, "candidate_role"))
	source := strings.ToLower(metadataStringEnv(hit.Metadata, "source"))
	profile := strings.ToLower(metadataStringEnv(hit.Metadata, "source_profile"))
	evidenceClass := strings.ToLower(metadataStringEnv(hit.Metadata, "evidence_class"))

	if isTestLikeCodeSearchPath(pathValue) || strings.Contains(role, "test") || strings.Contains(evidenceClass, "test") {
		buckets = appendUniqueStringEnv(buckets, "test")
	}
	if isLikelyDocumentationPath(pathValue) || profile == "repo_docs" || strings.Contains(role, "documentation") {
		buckets = appendUniqueStringEnv(buckets, "docs")
	}
	if role == "import_reference" || source == "local_import_mount_closure" || isLikelyMountPath(pathValue) {
		buckets = appendUniqueStringEnv(buckets, "mount")
	}
	if role == "registration_file" || role == "tool_declaration" || role == "direct_dispatch_file" || evidenceClass == "route_action" || isLikelyRouteActionPath(pathValue) {
		buckets = appendUniqueStringEnv(buckets, "route_action")
	}
	if role == "symbol_definition" || role == "definition_support" || isLikelyDomainPath(pathValue) {
		buckets = appendUniqueStringEnv(buckets, "domain")
	}
	if isLikelyFrontendPath(pathValue) {
		buckets = appendUniqueStringEnv(buckets, "frontend")
	}
	return buckets
}

func metadataStringEnv(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func isLikelyMountPath(pathValue string) bool {
	base := filepath.Base(pathValue)
	switch base {
	case "main.go", "main.ts", "main.tsx", "main.js", "main.jsx", "index.go", "index.ts", "index.tsx", "index.js", "index.jsx", "app.ts", "app.tsx", "app.js", "app.jsx", "server.ts", "server.js", "program.cs", "startup.cs":
		return true
	}
	return strings.Contains(pathValue, "/router.") || strings.Contains(pathValue, "/routes.") || strings.Contains(pathValue, "/server.") || strings.Contains(pathValue, "/app.")
}

func isLikelyDomainPath(pathValue string) bool {
	parts := strings.Split(pathValue, "/")
	for _, part := range parts {
		switch part {
		case "domain", "domains", "model", "models", "service", "services", "schema", "schemas", "entities":
			return true
		}
	}
	return false
}

func isLikelyFrontendPath(pathValue string) bool {
	parts := strings.Split(pathValue, "/")
	for _, part := range parts {
		switch part {
		case "app", "components", "component", "pages", "screens", "views", "ui":
			return true
		}
	}
	return strings.HasSuffix(pathValue, ".tsx") || strings.HasSuffix(pathValue, ".jsx")
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
	case ".cs":
		return "csharp"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
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
	return a.memoryQueryFnForStatusesAndScope(limit, statuses, contextengine.MemoryQueryScope{})
}

func (a *ReadOnlyAdapter) memoryQueryFnForStatusesAndScope(limit int, statuses []contextengine.ClaimStatus, scope contextengine.MemoryQueryScope) contextengine.MemoryQueryFunc {
	return func(ctx context.Context, workspaceID, query string) ([]contextengine.MemoryClaim, error) {
		effectiveStatuses := contextengine.EffectiveMemoryQueryStatuses(statuses, scope)
		if a.ceStore == nil {
			return nil, nil
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
		appendClaims := func(found []contextengine.MemoryClaim) {
			for _, claim := range found {
				if _, ok := seen[claim.ID]; ok {
					continue
				}
				seen[claim.ID] = struct{}{}
				claims = append(claims, claim)
			}
		}
		for _, status := range effectiveStatuses {
			if strings.TrimSpace(scope.TaskID) != "" {
				found, err := a.ceStore.ListClaims(ctx, ctxengstore.ClaimFilter{
					WorkspaceID: workspaceID,
					Status:      status,
					TaskID:      strings.TrimSpace(scope.TaskID),
					Limit:       fetchLimit,
				})
				if err != nil {
					return nil, err
				}
				appendClaims(found)
			}
			if strings.TrimSpace(scope.SessionID) != "" {
				found, err := a.ceStore.ListClaims(ctx, ctxengstore.ClaimFilter{
					WorkspaceID: workspaceID,
					Status:      status,
					SessionID:   strings.TrimSpace(scope.SessionID),
					Limit:       fetchLimit,
				})
				if err != nil {
					return nil, err
				}
				appendClaims(found)
			}
			if !scope.HasScope() || status == contextengine.ClaimStatusCurrent {
				found, err := a.ceStore.ListClaims(ctx, ctxengstore.ClaimFilter{
					WorkspaceID: workspaceID,
					Status:      status,
					Limit:       fetchLimit,
				})
				if err != nil {
					return nil, err
				}
				appendClaims(found)
			}
		}
		q := strings.TrimSpace(query)
		if q == "" && !scope.HasScope() {
			if bounded := contextengine.LimitMemoryQueryClaims(claims, limit); len(bounded) > 0 {
				return bounded, nil
			}
			return nil, nil
		}
		filtered := make([]contextengine.MemoryClaim, 0, len(claims))
		for _, c := range claims {
			if contextengine.ClaimVisibleForMemoryQuery(c, q, scope) {
				filtered = append(filtered, c)
				if limit > 0 && len(filtered) >= limit {
					break
				}
			}
		}
		if len(filtered) == 0 {
			return nil, nil
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
		tc := a.taskContextFromTask(ctx, t, workspaceID)
		return &tc, nil
	}
}

func (a *ReadOnlyAdapter) taskContextFromTask(ctx context.Context, task tasks.Task, workspaceID string) contextengine.TaskContext {
	tc := adapters.ConvertTask(task)
	if strings.TrimSpace(tc.WorkspaceID) == "" {
		tc.WorkspaceID = strings.TrimSpace(workspaceID)
	}
	if strings.TrimSpace(tc.WorkspaceID) == "" {
		tc.WorkspaceID = a.laneConfig().WorkspaceID
	}
	tc.RelatedClaims = appendUniqueEvidenceRefsEnv(tc.RelatedClaims, a.taskClaimRefs(ctx, tc.WorkspaceID, tc.TaskID, 20)...)
	return tc
}

func (a *ReadOnlyAdapter) taskClaimRefs(ctx context.Context, workspaceID, taskID string, limit int) []contextengine.EvidenceRef {
	if a.ceStore == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(taskID) == "" {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	statuses := []contextengine.ClaimStatus{
		contextengine.ClaimStatusCandidate,
		contextengine.ClaimStatusCurrent,
		contextengine.ClaimStatusNeedsRevalidation,
	}
	refs := make([]contextengine.EvidenceRef, 0, limit)
	seen := map[string]struct{}{}
	for _, status := range statuses {
		claims, err := a.ceStore.ListClaims(ctx, ctxengstore.ClaimFilter{
			WorkspaceID: workspaceID,
			Status:      status,
			TaskID:      strings.TrimSpace(taskID),
			Limit:       limit,
		})
		if err != nil {
			continue
		}
		for _, claim := range claims {
			id := strings.TrimSpace(claim.ID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			refs = append(refs, contextengine.EvidenceRef{
				Type:        contextengine.RefTypeMemoryClaim,
				Ref:         id,
				WorkspaceID: workspaceID,
				Title:       claim.Summary,
			})
			if len(refs) >= limit {
				return refs
			}
		}
	}
	return refs
}

func appendUniqueEvidenceRefsEnv(base []contextengine.EvidenceRef, extra ...contextengine.EvidenceRef) []contextengine.EvidenceRef {
	out := append([]contextengine.EvidenceRef(nil), base...)
	seen := map[string]struct{}{}
	for _, ref := range out {
		key := contextengine.FormatEvidenceRef(ref)
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, ref := range extra {
		if err := contextengine.ValidateEvidenceRef(ref); err != nil {
			continue
		}
		key := contextengine.FormatEvidenceRef(ref)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
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
	vaultPath := firstNonEmpty(
		strings.TrimSpace(a.vaultPath),
		strings.TrimSpace(os.Getenv("FOXCTL_RLM_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("FOXCTL_CONTEXTWIKI_VAULT_PATH")),
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
		var memStoreForContextWiki storage.MemoryStore
		if memStore, err := memorystore.OpenWithConfig(ctx, a.cfg); err == nil {
			memStoreForContextWiki = memStore
			defer func() { _ = memStore.Close() }()
		}

		workspaceStore := contextplane.NewWorkspaceStore(a.workspaceRoot)
		opts := workspaceStore.CurrentRetrievalOptions()
		opts.IncludeTopOfMindResult = false
		opts.IncludeLatestHandoff = false
		opts.IncludeVaultHits = true
		opts.IncludeControlPlaneRefs = false
		result, err := workspaceStore.RetrieveWithOptionsAndMemory(ctx, index, repoStore, nil, memStoreForContextWiki, query, effectiveLimit, opts)
		if err != nil {
			hits, searchErr := index.SearchNotes(ctx, query, effectiveLimit)
			if searchErr != nil {
				return nil, err
			}
			pack := directContextPackFromObsidianHits(firstNonEmpty(ws.ID(a.workspaceRoot), a.workspaceRoot), query, hits)
			if len(pack.Nodes) == 0 {
				return nil, nil
			}
			pack.Metadata["source"] = "contextwiki_vault_search_fallback"
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
		pack.Metadata["source"] = "contextwiki_retrieval"
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
			ID:          fmt.Sprintf("contextwiki_vault_hit_%d_%s", i, strings.ReplaceAll(path, "/", "_")),
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
		ID:          "contextwiki_vault_search:" + workspaceID,
		WorkspaceID: workspaceID,
		Query:       strings.TrimSpace(query),
		Lane:        contextengine.LaneContext,
		Nodes:       nodes,
		Metadata: map[string]any{
			"source": "contextwiki_vault_search",
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
	query := strings.TrimSpace(input.Query)
	scope := contextengine.MemoryQueryScope{
		TaskID:    input.TaskID,
		SessionID: input.SessionID,
	}
	statuses := parseMemoryClaimStatuses(input.MemoryStatuses)
	pack, err := contextengine.RetrieveMemory(ctx, cfg, a.memoryQueryFnForStatusesAndScope(limit, statuses, scope), query)
	if err != nil {
		return nil, err
	}
	if !scope.HasScope() && contextengine.MemoryQueryAllowsNamedFallback(contextengine.EffectiveMemoryQueryStatuses(statuses, scope)) {
		namedPack, ok, err := a.retrieveNamedMemory(ctx, cfg, query, limit)
		if ok {
			if err != nil {
				if ctx.Err() != nil {
					return nil, err
				}
				ensurePackMetadataEnv(&pack)
				pack.Metadata["named_memory_error"] = err.Error()
			} else if len(pack.Nodes) == 0 && (len(namedPack.Nodes) > 0 || namedMemoryPackSuppressesFallback(namedPack)) {
				return packToMap(namedPack), nil
			} else {
				mergeNamedMemoryPackIntoContextPack(&pack, namedPack, limit)
			}
		}
	}
	return packToMap(pack), nil
}

func mergeNamedMemoryPackIntoContextPack(pack *contextengine.EvidencePack, namedPack contextengine.EvidencePack, limit int) {
	if pack == nil {
		return
	}
	ensurePackMetadataEnv(pack)
	if len(namedPack.Nodes) > 0 {
		remaining := len(namedPack.Nodes)
		if limit > 0 {
			remaining = limit - len(pack.Nodes)
		}
		if remaining > 0 {
			if remaining > len(namedPack.Nodes) {
				remaining = len(namedPack.Nodes)
			}
			pack.Nodes = append(pack.Nodes, namedPack.Nodes[:remaining]...)
		}
		pack.Metadata["named_memory_hits"] = len(namedPack.Nodes)
		pack.Metadata["named_memory_search_method"] = namedPack.Metadata["search_method"]
		pack.Metadata["named_memory_episode_id"] = namedPack.Metadata["episode_id"]
	}
	if suppressed := intFromAnyEnv(namedPack.Metadata["suppressed_by_lifecycle"]); suppressed > 0 {
		pack.Metadata["named_memory_suppressed_by_lifecycle"] = suppressed
	}
}

func ensurePackMetadataEnv(pack *contextengine.EvidencePack) {
	if pack.Metadata == nil {
		pack.Metadata = map[string]any{}
	}
}

func namedMemoryPackSuppressesFallback(pack contextengine.EvidencePack) bool {
	if len(pack.Nodes) > 0 || pack.Metadata == nil {
		return false
	}
	return intFromAnyEnv(pack.Metadata["suppressed_by_lifecycle"]) > 0
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
	return a.gatherContextWithInput(ctx, input)
}

func (a *ReadOnlyAdapter) gatherMemoryContext(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input gatherContextInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	input = applyGatherMemoryContextDefaults(input)
	if len(input.RequiredEvidence) == 0 && len(input.CoverageRequirements) == 0 {
		plan, err := buildContextQueryPlan(planContextQueryInput{
			Question: input.Query,
			Goal:     input.Goal,
			Lanes:    input.Lanes,
			Limit:    input.Limit,
		})
		if err != nil {
			return nil, err
		}
		input.RequiredEvidence = append([]string(nil), plan.RequiredEvidence...)
		input.CoverageRequirements = append([]contextengine.CoverageRequirement(nil), plan.CoverageRequirements...)
	}
	return a.gatherContextWithOptions(ctx, input, gatherContextOptions{MemoryCoverageRepair: true})
}

func applyGatherMemoryContextDefaults(input gatherContextInput) gatherContextInput {
	if strings.TrimSpace(input.Goal) == "" {
		input.Goal = "recall"
	}
	input.Lanes = []string{"memory"}
	input.SourceProfiles = []string{string(contextengine.SourceProfileMemory)}
	if strings.TrimSpace(input.ResponseMode) == "" {
		input.ResponseMode = "answer_surface"
	}
	if input.MaxContextChars <= 0 {
		input.MaxContextChars = defaultGatherMemoryContextMaxContextChars
	}
	return input
}

func (a *ReadOnlyAdapter) gatherTestContext(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input gatherContextInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	if len(input.SourceProfiles) == 0 {
		input.SourceProfiles = []string{"repo_code"}
	}
	input.RequiredEvidence = appendIfMissingStringEnv(input.RequiredEvidence, "tests")
	input.RequiredEvidence = appendIfMissingStringEnv(input.RequiredEvidence, "test companions")
	return a.gatherContextWithInput(ctx, input)
}

func (a *ReadOnlyAdapter) gatherDocsContext(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input gatherContextInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	input.SourceProfiles = []string{"repo_docs"}
	if strings.TrimSpace(input.TaskType) == "" {
		input.TaskType = "documentation_map"
	}
	if len(cleanStringList(input.PathPrefixes)) == 0 {
		input.PathPrefixes = appendGatherDocsDefaultPathPrefixes(input.PathPrefixes)
	}
	input.ExcludedPaths = appendGatherDocsDefaultExcludedPaths(input.ExcludedPaths)
	return a.gatherContextWithInput(ctx, input)
}

func (a *ReadOnlyAdapter) gatherContextWithInput(ctx context.Context, input gatherContextInput) (map[string]any, error) {
	return a.gatherContextWithOptions(ctx, input, gatherContextOptions{})
}

func (a *ReadOnlyAdapter) gatherContextWithOptions(ctx context.Context, input gatherContextInput, options gatherContextOptions) (map[string]any, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	cfg := a.laneConfig()
	sourceProfiles := contextengine.NormalizeSourceProfiles(input.SourceProfiles)
	req := contextengine.GatherContextRequest{
		Query:                strings.TrimSpace(input.Query),
		Goal:                 strings.TrimSpace(input.Goal),
		TaskID:               strings.TrimSpace(input.TaskID),
		TaskType:             strings.TrimSpace(input.TaskType),
		SourceProfiles:       sourceProfiles,
		RequiredEvidence:     cleanStringList(input.RequiredEvidence),
		CoverageRequirements: append([]contextengine.CoverageRequirement(nil), input.CoverageRequirements...),
		Limit:                limit,
		Lanes:                parseGatherLanes(input.Lanes),
		Budget: contextengine.ContextBudget{
			MaxSources:      limit,
			MaxContextChars: input.MaxContextChars,
		},
	}
	providerRequiredEvidence := providerCoverageEvidence(req.RequiredEvidence, req.CoverageRequirements)
	codeOptions := normalizeCodeSearchRequestOptions(codeSearchRequestOptions{
		Languages:        input.Languages,
		PathPrefixes:     input.PathPrefixes,
		ExcludedPaths:    input.ExcludedPaths,
		RequiredEvidence: providerRequiredEvidence,
	})
	memoryStatuses := parseMemoryClaimStatuses(input.MemoryStatuses)
	memoryScope := contextengine.MemoryQueryScope{
		TaskID: input.TaskID,
	}
	bundle, err := contextengine.GatherContext(ctx, cfg, contextengine.GatherContextDeps{
		CodeSearch:  a.codeSearchFnForTaskWithRequired(limit, input.TaskType, providerRequiredEvidence, codeOptions, sourceProfiles...),
		MemoryQuery: a.memoryQueryFnForStatusesAndScope(limit, memoryStatuses, memoryScope),
		MemoryPacks: a.namedMemoryGatherPacksFn(memoryStatuses, memoryScope, namedMemoryGatherOptions{
			CoverageRepair:       options.MemoryCoverageRepair,
			RequiredEvidence:     providerRequiredEvidence,
			CoverageRequirements: req.CoverageRequirements,
		}),
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
	bundle = a.withGatherContextTrustMetadata(ctx, bundle)
	responseMode := strings.TrimSpace(input.ResponseMode)
	if responseMode == "" {
		responseMode = "answer_surface"
	}
	graphMode := strings.TrimSpace(strings.ToLower(input.GraphMode))
	if graphMode != "" && graphMode != "none" && graphMode != "summary" {
		return nil, fmt.Errorf("unsupported gather_context graph_mode %q", input.GraphMode)
	}
	if strings.EqualFold(responseMode, "full") || strings.EqualFold(responseMode, "bundle") {
		return contextBundleToMap(bundle), nil
	}
	if strings.EqualFold(responseMode, "answer_surface") ||
		strings.EqualFold(responseMode, "compact") {
		bundle = a.certifySelectedPathLoadability(bundle)
		out := contextBundleAnswerSurfaceToMap(bundle)
		switch graphMode {
		case "", "none":
			return out, nil
		case "summary":
			if summary := a.contextGraphSummaryForAnswerSurface(ctx, bundle, input); summary != nil {
				out["context_graph"] = summary
				markContextAnswerGraphUsed(out, "summary")
			}
			return out, nil
		default:
			return nil, fmt.Errorf("unsupported gather_context graph_mode %q", input.GraphMode)
		}
	}
	return nil, fmt.Errorf("unsupported gather_context response_mode %q", input.ResponseMode)
}

func appendIfMissingStringEnv(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), value) {
			return values
		}
	}
	return append(values, value)
}

func appendGatherDocsDefaultPathPrefixes(values []string) []string {
	for _, value := range []string{
		"docs", "README.md", "AGENTS.md", "CLAUDE.md", "CONTEXT.md",
		"CHANGELOG.md", "CONTRIBUTING.md", "LICENSE", "NOTICE",
	} {
		values = appendIfMissingStringEnv(values, value)
	}
	return values
}

func appendGatherDocsDefaultExcludedPaths(values []string) []string {
	for _, value := range []string{
		"node_modules", "dist", "build", "out", "coverage", "vendor", "deps", "_build",
		".git", ".local", ".replit", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
	} {
		values = appendIfMissingStringEnv(values, value)
	}
	return values
}

func (a *ReadOnlyAdapter) withGatherContextTrustMetadata(ctx context.Context, bundle contextengine.ContextBundle) contextengine.ContextBundle {
	if bundle.Metadata == nil {
		bundle.Metadata = map[string]any{}
	}
	if _, ok := bundle.Metadata["repoindex_freshness"]; !ok {
		bundle.Metadata["repoindex_freshness"] = a.repoIndexFreshnessForTrust(ctx)
	}
	return bundle
}

func (a *ReadOnlyAdapter) repoIndexFreshnessForTrust(ctx context.Context) map[string]any {
	out := map[string]any{
		"level":     "unknown",
		"available": false,
	}
	if strings.TrimSpace(a.cfg.Storage.Root) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		out["reason"] = "repoindex_unavailable"
		return out
	}
	store, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		out["reason"] = "repoindex_open_failed"
		return out
	}
	defer store.Close()
	meta, err := store.GetMeta(ctx)
	if err != nil {
		out["reason"] = "repoindex_meta_unavailable"
		return out
	}
	current := repoindex.ResolveGitSnapshot(ctx, a.workspaceRoot)
	freshness := repoindex.CompareIndexFreshness(meta, current)
	out["available"] = true
	out["level"] = string(freshness.Level)
	out["index_head_sha"] = freshness.IndexHeadSHA
	out["current_head_sha"] = freshness.CurrentHeadSHA
	out["index_worktree_dirty"] = freshness.IndexDirty
	out["current_worktree_dirty"] = freshness.CurrentDirty
	out["index_dirty_status_hash"] = freshness.IndexDirtyHash
	out["current_dirty_status_hash"] = freshness.CurrentDirtyHash
	out["commits_ahead"] = freshness.CommitsAhead
	out["commits_behind"] = freshness.CommitsBehind
	out["reasons"] = append([]string(nil), freshness.Reasons...)
	return out
}

func (a *ReadOnlyAdapter) contextGraphSummaryForAnswerSurface(ctx context.Context, bundle contextengine.ContextBundle, input gatherContextInput) map[string]any {
	roots := answerSurfaceContextGraphRoots(bundle)
	if len(roots) == 0 {
		return unavailableContextGraphSummary(nil, "graph_summary_no_roots", "No selected path roots were available for graph expansion.")
	}
	req := contextengine.ContextGraphRequest{
		WorkspaceID:          a.workspaceRoot,
		Query:                bundle.Query,
		TaskType:             strings.TrimSpace(input.TaskType),
		SourceProfiles:       contextengine.NormalizeSourceProfiles(input.SourceProfiles),
		CoverageRequirements: append([]contextengine.CoverageRequirement(nil), input.CoverageRequirements...),
		RootPaths:            roots,
		Direction:            contextengine.ContextGraphDirectionBoth,
		IncludeTests:         false,
		IncludeAdjacent:      false,
		PathPrefixes:         cleanContextGraphStrings(input.PathPrefixes),
		ExcludedPaths:        cleanContextGraphStrings(input.ExcludedPaths),
		Budget: contextengine.ContextGraphBudget{
			MaxRoots:      4,
			MaxNodes:      12,
			MaxEdges:      16,
			MaxDepth:      1,
			PerNodeCap:    4,
			MaxLocalFiles: 40,
			MaxLocalBytes: 256 * 1024,
			MaxDurationMs: 250,
		},
	}
	report, err := a.expandContextGraphReport(ctx, req, time.Now())
	if err != nil {
		return unavailableContextGraphSummary(roots, "graph_summary_unavailable", err.Error())
	}
	return compactContextGraphSummary(report)
}

func answerSurfaceContextGraphRoots(bundle contextengine.ContextBundle) []string {
	roots := make([]string, 0, len(bundle.SelectedPaths))
	for _, selected := range bundle.SelectedPaths {
		if path := normalizeGraphPath(selected.Path); path != "" {
			roots = appendUniqueStringEnv(roots, path)
		}
	}
	for _, candidate := range bundle.AnswerCandidates {
		if !strings.EqualFold(strings.TrimSpace(candidate.Kind), "path") {
			continue
		}
		if path := normalizeGraphPath(candidate.Value); path != "" {
			roots = appendUniqueStringEnv(roots, path)
		}
	}
	if len(roots) > 4 {
		roots = roots[:4]
	}
	return roots
}

func compactContextGraphSummary(report contextengine.ContextGraphReport) map[string]any {
	nodes := append([]contextengine.ContextGraphNode(nil), report.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Role != nodes[j].Role {
			return nodes[i].Role > nodes[j].Role
		}
		if nodes[i].Path != nodes[j].Path {
			return nodes[i].Path < nodes[j].Path
		}
		return nodes[i].ID < nodes[j].ID
	})
	topNodes := make([]map[string]any, 0, minIntEnv(len(nodes), 8))
	for i, node := range nodes {
		if i >= 8 {
			break
		}
		topNodes = append(topNodes, map[string]any{
			"id":         node.ID,
			"path":       node.Path,
			"kind":       node.Kind,
			"role":       node.Role,
			"load_ref":   node.LoadRef,
			"confidence": node.Confidence,
		})
	}
	edges := append([]contextengine.ContextGraphEdge(nil), report.Edges...)
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].Type != edges[j].Type {
			return edges[i].Type < edges[j].Type
		}
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	topEdges := make([]map[string]any, 0, minIntEnv(len(edges), 8))
	for i, edge := range edges {
		if i >= 8 {
			break
		}
		topEdges = append(topEdges, map[string]any{
			"from":       edge.From,
			"to":         edge.To,
			"type":       edge.Type,
			"confidence": edge.Confidence,
		})
	}
	missing := make([]map[string]any, 0, minIntEnv(len(report.Missing), 6))
	for i, gap := range report.Missing {
		if i >= 6 {
			break
		}
		missing = append(missing, map[string]any{
			"id":       gap.ID,
			"kind":     gap.Kind,
			"severity": gap.Severity,
			"message":  gap.Message,
			"roots":    gap.Roots,
		})
	}
	roots := make([]map[string]any, 0, len(report.Roots))
	for _, root := range report.Roots {
		roots = append(roots, map[string]any{
			"id":         root.ID,
			"path":       root.Path,
			"load_ref":   root.LoadRef,
			"confidence": root.Confidence,
		})
	}
	return map[string]any{
		"roots":      roots,
		"top_nodes":  topNodes,
		"top_edges":  topEdges,
		"confidence": report.Confidence,
		"missing":    missing,
	}
}

func unavailableContextGraphSummary(roots []string, id string, message string) map[string]any {
	rootItems := make([]map[string]any, 0, len(roots))
	for _, root := range roots {
		rootItems = append(rootItems, map[string]any{
			"path":     root,
			"load_ref": contextengine.FormatEvidenceRef(contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: root}),
		})
	}
	return map[string]any{
		"roots":      rootItems,
		"top_nodes":  []map[string]any{},
		"top_edges":  []map[string]any{},
		"confidence": contextengine.ContextGraphConfidence{Overall: 0, Completeness: "missing", TrustedForProceed: false},
		"missing": []map[string]any{{
			"id":       id,
			"kind":     id,
			"severity": "warn",
			"message":  message,
			"roots":    roots,
		}},
	}
}

func minIntEnv(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func providerCoverageEvidence(required []string, requirements []contextengine.CoverageRequirement) []string {
	out := cleanStringList(required)
	for _, req := range requirements {
		out = appendUniqueStringEnv(out, req.ID)
		out = appendUniqueStringEnv(out, req.Label)
		out = appendUniqueStringsEnv(out, req.Terms...)
		if req.Kind != "" && req.Label != "" {
			out = appendUniqueStringEnv(out, req.Kind+" "+req.Label)
		}
	}
	return cleanStringList(out)
}

func (a *ReadOnlyAdapter) certifySelectedPathLoadability(bundle contextengine.ContextBundle) contextengine.ContextBundle {
	if bundle.Certificate == nil {
		return bundle
	}
	unloadable := make([]contextengine.EvidenceRef, 0)
	for _, selected := range bundle.SelectedPaths {
		ref := selectedPathLoadRef(selected)
		if ref == "" {
			continue
		}
		parsed, err := contextengine.ParseEvidenceRef(ref)
		if err != nil {
			unloadable = append(unloadable, contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: strings.TrimSpace(selected.Path), WorkspaceID: bundle.WorkspaceID})
			continue
		}
		if parsed.Type != contextengine.RefTypePath {
			continue
		}
		if !a.workspacePathLoadable(parsed.Ref) {
			if parsed.WorkspaceID == "" {
				parsed.WorkspaceID = bundle.WorkspaceID
			}
			unloadable = append(unloadable, parsed)
		}
	}
	cert := *bundle.Certificate
	if len(unloadable) == 0 {
		cert.Checks = append(cert.Checks, contextengine.ContextCheck{Name: "selected_refs_loadable", Status: "pass"})
		bundle.Certificate = &cert
		return bundle
	}
	cert.UnloadableRefs = append(cert.UnloadableRefs, unloadable...)
	for _, ref := range unloadable {
		cert.MissingEvidence = append(cert.MissingEvidence, contextengine.FormatEvidenceRef(ref))
	}
	cert.RequiredEvidenceOK = false
	cert.Status = contextengine.ContextCertificateStatusFailed
	cert.Checks = append(cert.Checks, contextengine.ContextCheck{
		Name:    "selected_refs_loadable",
		Status:  "fail",
		Message: fmt.Sprintf("%d selected path refs are not loadable", len(unloadable)),
	})
	bundle.Answerable = false
	bundle.Certificate = &cert
	return bundle
}

func (a *ReadOnlyAdapter) workspacePathLoadable(path string) bool {
	path = strings.TrimSpace(path)
	root := strings.TrimSpace(a.workspaceRoot)
	if path == "" || root == "" {
		return false
	}
	fullPath := path
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(root, filepath.FromSlash(path))
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	cleanPath, err := filepath.Abs(fullPath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return false
	}
	info, err := os.Stat(cleanPath)
	return err == nil && !info.IsDir()
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
		a.memoryQueryFnForStatusesAndScope(limit, parseMemoryClaimStatuses(input.MemoryStatuses), contextengine.MemoryQueryScope{
			TaskID:    input.TaskID,
			SessionID: input.SessionID,
		}),
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
		workspaceID := strings.TrimSpace(a.laneConfig().WorkspaceID)
		if workspaceID != "" && strings.TrimSpace(claim.WorkspaceID) != workspaceID {
			return map[string]any{"ref": refStr, "loaded": false, "error": "claim not found in workspace"}, nil
		}
		return boundLoadedEvidenceRef(refStr, map[string]any{
			"ref":    refStr,
			"loaded": true,
			"claim":  claim,
		}, input.MaxTokens), nil
	case contextengine.RefTypeNamedMemory:
		out, err := a.loadNamedMemoryEvidence(ctx, refStr, ref.Ref)
		return boundLoadedEvidenceRef(refStr, out, input.MaxTokens), err
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

func (a *ReadOnlyAdapter) aggregateEvidenceRefs(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input aggregateEvidenceRefsInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	input.Refs = uniqueNonEmptyStringsEnv(input.Refs)
	if len(input.Refs) == 0 {
		return nil, fmt.Errorf("refs is required")
	}
	if input.MaxRefs <= 0 {
		input.MaxRefs = defaultAggregateEvidenceMaxRefs
	}
	if input.MaxTextChars <= 0 {
		input.MaxTextChars = defaultAggregateEvidenceMaxTextChars
	}
	if input.MaxTokensPerRef <= 0 {
		input.MaxTokensPerRef = defaultAggregateEvidenceLoadMaxTokens
	}
	if len(input.RequiredEvidence) == 0 && len(input.CoverageRequirements) == 0 {
		plan, err := buildContextQueryPlan(planContextQueryInput{
			Question: input.Query,
			Goal:     "recall",
			Limit:    input.MaxRefs,
		})
		if err != nil {
			return nil, err
		}
		input.RequiredEvidence = append([]string(nil), plan.RequiredEvidence...)
		input.CoverageRequirements = append([]contextengine.CoverageRequirement(nil), plan.CoverageRequirements...)
	}
	refs := input.Refs
	if len(refs) > input.MaxRefs {
		refs = refs[:input.MaxRefs]
	}
	loaded := make([]aggregateLoadedEvidenceRef, 0, len(refs))
	for _, ref := range refs {
		out, err := a.loadEvidenceRef(ctx, mustJSON(map[string]any{
			"ref":        ref,
			"max_tokens": aggregateEvidenceScanMaxTokens(input.MaxTokensPerRef),
		}))
		item := aggregateLoadedEvidenceRef{
			Ref:     ref,
			Loaded:  boolFromAnyEnv(out["loaded"]),
			Error:   strings.TrimSpace(fmt.Sprint(out["error"])),
			Payload: out,
		}
		if err != nil {
			item.Error = err.Error()
		}
		item.Text = aggregateEvidencePayloadTextForQuery(out, input.Query, input.MaxTextChars)
		loaded = append(loaded, item)
	}
	return aggregateLoadedEvidenceRefs(input, loaded), nil
}

func (a *ReadOnlyAdapter) evidenceLedger(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input evidenceLedgerInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	input.Refs = uniqueNonEmptyStringsEnv(input.Refs)
	if len(input.Refs) == 0 {
		return nil, fmt.Errorf("refs is required")
	}
	if input.MaxRefs <= 0 {
		input.MaxRefs = defaultAggregateEvidenceMaxRefs
	}
	if input.MaxTextChars <= 0 {
		input.MaxTextChars = defaultAggregateEvidenceMaxTextChars
	}
	if input.MaxTokensPerRef <= 0 {
		input.MaxTokensPerRef = defaultAggregateEvidenceLoadMaxTokens
	}
	plan, err := buildContextQueryPlan(planContextQueryInput{
		Question: input.Query,
		Goal:     "recall",
		Limit:    input.MaxRefs,
	})
	if err != nil {
		return nil, err
	}
	if len(input.RequiredEvidence) == 0 && len(input.CoverageRequirements) == 0 {
		input.RequiredEvidence = append([]string(nil), plan.RequiredEvidence...)
		input.CoverageRequirements = append([]contextengine.CoverageRequirement(nil), plan.CoverageRequirements...)
	}
	refs := input.Refs
	if len(refs) > input.MaxRefs {
		refs = refs[:input.MaxRefs]
	}
	loaded := make([]aggregateLoadedEvidenceRef, 0, len(refs))
	for _, ref := range refs {
		out, err := a.loadEvidenceRef(ctx, mustJSON(map[string]any{
			"ref":        ref,
			"max_tokens": aggregateEvidenceScanMaxTokens(input.MaxTokensPerRef),
		}))
		item := aggregateLoadedEvidenceRef{
			Ref:     ref,
			Loaded:  boolFromAnyEnv(out["loaded"]),
			Error:   strings.TrimSpace(fmt.Sprint(out["error"])),
			Payload: out,
		}
		if err != nil {
			item.Error = err.Error()
		}
		item.Text = evidenceLedgerPayloadTextForQuery(out, input.Query, input.MaxTextChars)
		loaded = append(loaded, item)
	}
	return buildEvidenceLedger(input, loaded, plan), nil
}

type aggregateLoadedEvidenceRef struct {
	Ref     string
	Loaded  bool
	Error   string
	Text    string
	Payload map[string]any
}

type aggregateEvidenceSlot struct {
	ID       string
	Kind     string
	Label    string
	Terms    []string
	Required bool
}

type evidenceLedgerValues struct {
	Durations []string
	Dates     []string
	Locations []string
	Money     []string
	Numbers   []string
	Items     []string
}

func aggregateLoadedEvidenceRefs(input aggregateEvidenceRefsInput, loaded []aggregateLoadedEvidenceRef) map[string]any {
	slots := aggregateEvidenceSlots(input)
	requiredTerms := aggregateEvidenceRequiredTerms(input)
	refSummaries := make([]map[string]any, 0, len(loaded))
	claims := make([]map[string]any, 0, len(loaded))
	slotSupport := map[string][]map[string]any{}
	loadedCount := 0
	for _, item := range loaded {
		if item.Loaded {
			loadedCount++
		}
		refSummary := map[string]any{
			"ref":    item.Ref,
			"loaded": item.Loaded,
		}
		if item.Error != "" && item.Error != "<nil>" {
			refSummary["error"] = item.Error
		}
		refSummaries = append(refSummaries, refSummary)
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		matchedTerms := aggregateMatchedTerms(text, requiredTerms)
		matchedSlots := aggregateMatchedSlots(text, slots)
		support := "loaded"
		if len(matchedSlots) > 0 {
			support = "slot"
		} else if len(matchedTerms) > 0 {
			support = "term"
		}
		claim := map[string]any{
			"ref":           item.Ref,
			"text":          limitContextEvidenceDigestText(text, input.MaxTextChars),
			"support":       support,
			"matched_terms": matchedTerms,
			"matched_slots": matchedSlots,
		}
		claims = append(claims, claim)
		for _, slotID := range matchedSlots {
			slotSupport[slotID] = append(slotSupport[slotID], map[string]any{
				"ref":  item.Ref,
				"text": claim["text"],
			})
		}
	}
	slotMaps, missingSlots := aggregateEvidenceSlotMaps(slots, slotSupport)
	return map[string]any{
		"schema_version": "evidence_ref_aggregate/v1",
		"query":          input.Query,
		"ref_count":      len(input.Refs),
		"loaded_count":   loadedCount,
		"refs":           refSummaries,
		"claims":         claims,
		"slots":          slotMaps,
		"answer_outline": map[string]any{
			"supported_claims": aggregateClaimTextsBySupport(claims, "slot", "term"),
			"loaded_claims":    aggregateClaimTextsBySupport(claims, "loaded"),
			"missing_slots":    missingSlots,
			"guidance":         "Synthesize across supported_claims; use loaded_claims as partial context only, and do not claim missing slots as proven.",
		},
	}
}

func buildEvidenceLedger(input evidenceLedgerInput, loaded []aggregateLoadedEvidenceRef, plan contextQueryPlanOutput) map[string]any {
	aggInput := aggregateEvidenceRefsInput{
		Query:                input.Query,
		Refs:                 input.Refs,
		RequiredEvidence:     input.RequiredEvidence,
		CoverageRequirements: input.CoverageRequirements,
		MaxTextChars:         input.MaxTextChars,
	}
	answerType := strings.TrimSpace(plan.AnswerType)
	if answerType == "" {
		answerType = classifyContextQueryAnswerType(input.Query)
	}
	slots := aggregateEvidenceSlots(aggInput)
	requiredTerms := aggregateEvidenceRequiredTerms(aggInput)

	rows := make([]evidenceLedgerRow, 0, len(loaded))
	anyAccepted := false
	for _, item := range loaded {
		text := strings.TrimSpace(item.Text)
		r := evidenceLedgerRow{
			item:         item,
			text:         text,
			matchedTerms: aggregateMatchedTerms(text, requiredTerms),
			matchedSlots: evidenceLedgerMatchedSlots(text, slots),
			values:       extractEvidenceLedgerValues(text),
		}
		r.status, r.reason = classifyEvidenceLedgerRow(answerType, slots, len(requiredTerms), r.matchedTerms, r.matchedSlots, r.values, item)
		if r.status == "accept" {
			anyAccepted = true
		}
		rows = append(rows, r)
	}

	// Cross-ref evidence composition: when no single ref qualifies on its own
	// but the union of refs covers the required anchors and a ref directly
	// states the requested answer value, accept the answer-bearing ref(s) and
	// credit the sibling refs that supply the anchor coverage. This is what
	// recovers answers that depend on separated conversational context.
	composedConceptSupport := map[string][]map[string]any{}
	if !anyAccepted && evidenceLedgerRequiresDirectAnswer(answerType) {
		composeCrossRefEvidence(rows, slots, answerType, input.MaxTextChars, composedConceptSupport)
	}

	acceptedRows := []map[string]any{}
	rejectedRows := []map[string]any{}
	acceptedRefs := []string{}
	rejectedRefs := []string{}
	acceptedSlotSupport := map[string][]map[string]any{}
	supportedValues := []string{}
	supportedItems := []string{}
	for _, r := range rows {
		row := map[string]any{
			"ref":           r.item.Ref,
			"status":        r.status,
			"reason":        r.reason,
			"loaded":        r.item.Loaded,
			"text":          limitContextEvidenceDigestText(r.text, input.MaxTextChars),
			"matched_terms": r.matchedTerms,
			"matched_slots": r.matchedSlots,
			"answer_values": evidenceLedgerValuesMap(r.values),
		}
		if r.item.Error != "" && r.item.Error != "<nil>" {
			row["error"] = r.item.Error
		}
		if r.status == "accept" {
			acceptedRows = append(acceptedRows, row)
			acceptedRefs = appendUniqueStringEnv(acceptedRefs, r.item.Ref)
			for _, slotID := range r.matchedSlots {
				acceptedSlotSupport[slotID] = append(acceptedSlotSupport[slotID], map[string]any{
					"ref":  r.item.Ref,
					"text": row["text"],
				})
			}
			supportedValues = appendUniqueStringsEnv(supportedValues, evidenceLedgerAnswerValues(answerType, r.values)...)
			supportedItems = appendUniqueStringsEnv(supportedItems, r.values.Items...)
		} else {
			rejectedRows = append(rejectedRows, row)
			rejectedRefs = appendUniqueStringEnv(rejectedRefs, r.item.Ref)
		}
	}
	// Credit cross-ref anchor coverage so composed answers report covered slots
	// instead of spurious fallbacks. Dedupe by ref to avoid inflating support.
	for slotID, entries := range composedConceptSupport {
		for _, entry := range entries {
			if !evidenceLedgerSlotSupportHasRef(acceptedSlotSupport[slotID], entry["ref"]) {
				acceptedSlotSupport[slotID] = append(acceptedSlotSupport[slotID], entry)
			}
		}
	}
	slotMaps, missingSlots := evidenceLedgerSlotMaps(slots, acceptedSlotSupport, answerType, len(supportedValues) > 0)
	fallbackCalls := evidenceLedgerFallbackCalls(plan, missingSlots)
	needsFallback := len(missingSlots) > 0 || len(acceptedRows) == 0
	return map[string]any{
		"schema_version":        "evidence_ledger/v1",
		"query":                 input.Query,
		"answer_type":           answerType,
		"ref_count":             len(input.Refs),
		"loaded_count":          countLoadedEvidenceRefs(loaded),
		"ready":                 !needsFallback,
		"needs_fallback":        needsFallback,
		"accepted_refs":         acceptedRefs,
		"rejected_refs":         rejectedRefs,
		"accepted_rows":         acceptedRows,
		"rejected_rows":         rejectedRows,
		"slots":                 slotMaps,
		"fallback_queries":      evidenceLedgerFallbackQueries(fallbackCalls),
		"fallback_gather_calls": fallbackCalls,
		"answer_outline": map[string]any{
			"supported_values": supportedValues,
			"supported_items":  supportedItems,
			"missing_slots":    missingSlots,
			"guidance":         "Use only accepted_rows for final factual claims. Do not use rejected_rows as answers. If needs_fallback is true, run the fallback query that targets the missing slot before refusing or finalizing.",
		},
	}
}

// evidenceLedgerRow is the per-ref working state of the evidence ledger: the
// loaded ref plus its matched terms/slots, extracted values, and the
// classification verdict. It is mutated in place by cross-ref composition.
type evidenceLedgerRow struct {
	item         aggregateLoadedEvidenceRef
	text         string
	matchedTerms []string
	matchedSlots []string
	values       evidenceLedgerValues
	status       string
	reason       string
}

// composeCrossRefEvidence implements cross-ref evidence composition. It only
// runs when the per-ref pass accepted nothing and the answer type needs a
// direct value. When the union of all loaded refs covers the required anchor
// concepts (the same threshold the per-ref pass uses) and at least one ref
// directly states the requested answer value, it flips those answer-bearing
// rows to "accept" and returns, via conceptSupportOut, which sibling refs
// supply the anchor coverage so the assembled slot map reports covered.
//
// The union-coverage guard keeps this strictly more permissive only where it
// is safe: a single ref that already cleared the concept threshold would have
// been accepted by the per-ref pass, so composition can only fire when
// complementary refs combine to exceed any one ref. Distractor-only ref sets
// (no ref states the answer value, or the union still misses anchors) stay
// rejected, preserving the per-ref fallback behavior.
func composeCrossRefEvidence(rows []evidenceLedgerRow, slots []aggregateEvidenceSlot, answerType string, maxTextChars int, conceptSupportOut map[string][]map[string]any) {
	unionConcepts := []string{}
	conceptSupport := map[string][]map[string]any{}
	for i := range rows {
		r := &rows[i]
		if !r.item.Loaded {
			continue
		}
		for _, label := range evidenceLedgerConceptSlotMatches(slots, r.matchedSlots) {
			unionConcepts = appendUniqueStringEnv(unionConcepts, label)
		}
		for _, slotID := range r.matchedSlots {
			if !evidenceLedgerSlotIsConcept(slots, slotID) {
				continue
			}
			conceptSupport[slotID] = append(conceptSupport[slotID], map[string]any{
				"ref":  r.item.Ref,
				"text": limitContextEvidenceDigestText(r.text, maxTextChars),
			})
		}
	}
	if len(unionConcepts) < evidenceLedgerRequiredConceptThreshold(slots) {
		return
	}
	composed := false
	for i := range rows {
		r := &rows[i]
		if r.status == "accept" || !r.item.Loaded {
			continue
		}
		if evidenceLedgerHasAnswerValue(answerType, r.values) {
			r.status = "accept"
			r.reason = "answer value stated here; required anchors covered by related evidence across refs (cross-ref composition)"
			composed = true
		}
	}
	if !composed {
		return
	}
	for slotID, entries := range conceptSupport {
		conceptSupportOut[slotID] = entries
	}
}

// evidenceLedgerSlotIsConcept reports whether slotID names a concept (anchor)
// slot rather than the answer slot.
func evidenceLedgerSlotIsConcept(slots []aggregateEvidenceSlot, slotID string) bool {
	for _, slot := range slots {
		if slot.ID == slotID {
			return slot.Kind != "answer_slot"
		}
	}
	return false
}

// evidenceLedgerSlotSupportHasRef reports whether a slot's support list already
// credits the given ref, used to dedupe composed anchor support.
func evidenceLedgerSlotSupportHasRef(entries []map[string]any, ref any) bool {
	for _, entry := range entries {
		if entry["ref"] == ref {
			return true
		}
	}
	return false
}

func classifyEvidenceLedgerRow(answerType string, slots []aggregateEvidenceSlot, requiredTermCount int, matchedTerms, matchedSlots []string, values evidenceLedgerValues, item aggregateLoadedEvidenceRef) (string, string) {
	if !item.Loaded {
		return "reject", firstNonEmpty(item.Error, "ref did not load")
	}
	if strings.TrimSpace(item.Text) == "" {
		return "reject", "loaded ref has no usable evidence text"
	}
	directAnswer := evidenceLedgerHasAnswerValue(answerType, values)
	conceptMatches := evidenceLedgerConceptSlotMatches(slots, matchedSlots)
	threshold := evidenceLedgerRequiredTermThreshold(requiredTermCount)
	strongTermMatch := len(matchedTerms) >= threshold
	strongSlotMatch := len(conceptMatches) >= evidenceLedgerRequiredConceptThreshold(slots)
	if evidenceLedgerRequiresDirectAnswer(answerType) && !directAnswer {
		return "reject", "required anchors are present but the requested answer slot is not directly stated"
	}
	if evidenceLedgerRequiresDirectAnswer(answerType) {
		if strongSlotMatch {
			return "accept", "directly covers required evidence and exposes the requested answer slot"
		}
		return "reject", "answer value is present but too many required anchors are missing"
	}
	if (strongTermMatch || strongSlotMatch) && (directAnswer || answerType == "fact" || answerType == "comparison") {
		if directAnswer {
			return "accept", "directly covers required evidence and exposes the requested answer slot"
		}
		return "accept", "covers required evidence for a factual answer"
	}
	if !strongTermMatch && !strongSlotMatch {
		return "reject", "topical or adjacent evidence; required anchors are missing"
	}
	return "reject", "required anchors are present but the requested answer slot is not directly stated"
}

func evidenceLedgerMatchedSlots(text string, slots []aggregateEvidenceSlot) []string {
	out := []string{}
	normalized := strings.ToLower(text)
	for _, slot := range slots {
		if evidenceLedgerSlotMatches(normalized, slot) {
			out = appendUniqueStringEnv(out, slot.ID)
		}
	}
	return out
}

func evidenceLedgerSlotMatches(normalizedText string, slot aggregateEvidenceSlot) bool {
	if slot.Kind == "answer_slot" {
		return len(aggregateMatchedTerms(normalizedText, slot.Terms)) > 0
	}
	terms := splitContextEvidenceTerms(slot.Label)
	if len(terms) == 0 {
		terms = aggregateEvidenceTerms(slot.Terms)
	}
	required := []string{}
	for _, term := range terms {
		term = normalizeContextEvidenceTerm(term)
		if term == "" || contextQuestionStopwords[term] {
			continue
		}
		required = appendUniqueStringEnv(required, term)
	}
	if len(required) == 0 {
		return len(aggregateMatchedTerms(normalizedText, slot.Terms)) > 0
	}
	for _, term := range required {
		if !strings.Contains(normalizedText, term) {
			return false
		}
	}
	return true
}

func evidenceLedgerRequiredTermThreshold(requiredTermCount int) int {
	if requiredTermCount <= 1 {
		return 1
	}
	return 2
}

func evidenceLedgerRequiredConceptThreshold(slots []aggregateEvidenceSlot) int {
	requiredConcepts := []string{}
	for _, slot := range slots {
		if slot.Required && slot.Kind != "answer_slot" {
			label := normalizeContextEvidenceTerm(firstNonEmpty(slot.Label, strings.Join(slot.Terms, " ")))
			if label == "" {
				label = slot.ID
			}
			requiredConcepts = appendUniqueStringEnv(requiredConcepts, label)
		}
	}
	count := len(requiredConcepts)
	if count <= 1 {
		return 1
	}
	if count <= 3 {
		return 2
	}
	threshold := (count*3 + 4) / 5
	return maxInt(2, threshold)
}

func evidenceLedgerRequiresDirectAnswer(answerType string) bool {
	switch answerType {
	case "count", "list", "location", "duration", "temporal":
		return true
	default:
		return false
	}
}

func evidenceLedgerConceptSlotMatches(slots []aggregateEvidenceSlot, matchedSlots []string) []string {
	out := []string{}
	for _, id := range matchedSlots {
		for _, slot := range slots {
			if slot.ID == id && slot.Kind != "answer_slot" {
				label := normalizeContextEvidenceTerm(firstNonEmpty(slot.Label, strings.Join(slot.Terms, " ")))
				if label == "" {
					label = id
				}
				out = appendUniqueStringEnv(out, label)
			}
		}
	}
	return out
}

func evidenceLedgerHasAnswerValue(answerType string, values evidenceLedgerValues) bool {
	switch answerType {
	case "duration":
		return len(values.Durations) > 0
	case "temporal":
		return len(values.Dates) > 0 || len(values.Durations) > 0
	case "location":
		return len(values.Locations) > 0
	case "count":
		return len(values.Items) > 0 || len(values.Numbers) > 0
	case "list":
		return len(values.Items) > 0
	case "comparison":
		return len(values.Numbers) > 0 || len(values.Money) > 0 || len(values.Durations) > 0
	default:
		return true
	}
}

func evidenceLedgerAnswerValues(answerType string, values evidenceLedgerValues) []string {
	switch answerType {
	case "duration":
		return values.Durations
	case "temporal":
		return appendUniqueStringsEnv(append([]string{}, values.Dates...), values.Durations...)
	case "location":
		return values.Locations
	case "count":
		if len(values.Items) > 0 {
			return values.Items
		}
		return values.Numbers
	case "list":
		return values.Items
	case "comparison":
		out := appendUniqueStringsEnv(append([]string{}, values.Money...), values.Durations...)
		return appendUniqueStringsEnv(out, values.Numbers...)
	default:
		return []string{}
	}
}

func evidenceLedgerSlotMaps(slots []aggregateEvidenceSlot, support map[string][]map[string]any, answerType string, directAnswerCovered bool) ([]map[string]any, []string) {
	out := make([]map[string]any, 0, len(slots))
	missing := []string{}
	for _, slot := range slots {
		items := support[slot.ID]
		status := "missing"
		if len(items) > 0 || (slot.Kind == "answer_slot" && directAnswerCovered) {
			status = "covered"
		} else if slot.Required {
			missing = appendUniqueStringEnv(missing, firstNonEmpty(slot.Label, slot.ID))
		}
		mapped := map[string]any{
			"id":            slot.ID,
			"kind":          slot.Kind,
			"label":         slot.Label,
			"required":      slot.Required,
			"status":        status,
			"terms":         append([]string(nil), slot.Terms...),
			"support_count": len(items),
		}
		if len(items) > 0 {
			mapped["support"] = items
		}
		out = append(out, mapped)
	}
	return out, missing
}

func evidenceLedgerFallbackCalls(plan contextQueryPlanOutput, missingSlots []string) []map[string]any {
	out := make([]map[string]any, 0, len(plan.FallbackProbes))
	for _, probe := range plan.FallbackProbes {
		if strings.TrimSpace(probe.Query) == "" {
			continue
		}
		out = append(out, map[string]any{
			"query":                 probe.Query,
			"goal":                  probe.Goal,
			"required_evidence":     append([]string(nil), probe.RequiredEvidence...),
			"coverage_requirements": probe.CoverageRequirements,
			"limit":                 probe.Limit,
			"lanes":                 append([]string(nil), probe.Lanes...),
			"response_mode":         probe.ResponseMode,
			"missing_slots":         append([]string(nil), missingSlots...),
		})
	}
	return out
}

func evidenceLedgerFallbackQueries(calls []map[string]any) []string {
	out := []string{}
	for _, call := range calls {
		out = appendUniqueStringEnv(out, strings.TrimSpace(fmt.Sprint(call["query"])))
	}
	return out
}

func countLoadedEvidenceRefs(loaded []aggregateLoadedEvidenceRef) int {
	count := 0
	for _, item := range loaded {
		if item.Loaded {
			count++
		}
	}
	return count
}

func evidenceLedgerValuesMap(values evidenceLedgerValues) map[string]any {
	return map[string]any{
		"durations": values.Durations,
		"dates":     values.Dates,
		"locations": values.Locations,
		"money":     values.Money,
		"numbers":   values.Numbers,
		"items":     values.Items,
	}
}

func aggregateEvidenceSlots(input aggregateEvidenceRefsInput) []aggregateEvidenceSlot {
	out := make([]aggregateEvidenceSlot, 0, len(input.CoverageRequirements)+len(input.RequiredEvidence))
	seen := map[string]struct{}{}
	add := func(slot aggregateEvidenceSlot) {
		slot.ID = strings.TrimSpace(slot.ID)
		slot.Kind = strings.TrimSpace(slot.Kind)
		slot.Label = strings.TrimSpace(slot.Label)
		slot.Terms = aggregateEvidenceTerms(append(slot.Terms, slot.Label))
		if slot.ID == "" {
			slot.ID = stableAggregateEvidenceID(slot.Kind, slot.Label, slot.Terms)
		}
		if slot.Kind == "" {
			slot.Kind = "concept"
		}
		if slot.Label == "" && len(slot.Terms) > 0 {
			slot.Label = strings.Join(slot.Terms, " ")
		}
		if slot.ID == "" || len(slot.Terms) == 0 {
			return
		}
		if _, ok := seen[slot.ID]; ok {
			return
		}
		seen[slot.ID] = struct{}{}
		out = append(out, slot)
	}
	for _, req := range input.CoverageRequirements {
		add(aggregateEvidenceSlot{
			ID:       req.ID,
			Kind:     req.Kind,
			Label:    req.Label,
			Terms:    req.Terms,
			Required: req.Required,
		})
	}
	for _, evidence := range input.RequiredEvidence {
		add(aggregateEvidenceSlot{
			Kind:     "concept",
			Label:    evidence,
			Terms:    []string{evidence},
			Required: true,
		})
	}
	return out
}

func aggregateEvidenceRequiredTerms(input aggregateEvidenceRefsInput) []string {
	values := append([]string(nil), input.RequiredEvidence...)
	for _, req := range input.CoverageRequirements {
		values = append(values, req.Label)
		values = append(values, req.Terms...)
	}
	return aggregateEvidenceTerms(values)
}

func aggregateEvidenceTerms(values []string) []string {
	out := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out = appendUniqueStringEnv(out, value)
		for _, part := range regexp.MustCompile(`[^a-z0-9$./_-]+`).Split(value, -1) {
			part = strings.TrimSpace(part)
			if len(part) < 3 {
				continue
			}
			out = appendUniqueStringEnv(out, part)
		}
	}
	return out
}

func stableAggregateEvidenceID(kind, label string, terms []string) string {
	parts := aggregateEvidenceTerms([]string{kind, label, strings.Join(terms, " ")})
	if len(parts) == 0 {
		return ""
	}
	if len(parts) > 4 {
		parts = parts[:4]
	}
	return strings.Join(parts, "_")
}

func aggregateMatchedTerms(text string, terms []string) []string {
	normalized := strings.ToLower(text)
	out := []string{}
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if strings.Contains(normalized, term) {
			out = appendUniqueStringEnv(out, term)
		}
	}
	return out
}

func aggregateMatchedSlots(text string, slots []aggregateEvidenceSlot) []string {
	out := []string{}
	for _, slot := range slots {
		if len(aggregateMatchedTerms(text, slot.Terms)) > 0 {
			out = appendUniqueStringEnv(out, slot.ID)
		}
	}
	return out
}

func aggregateEvidenceSlotMaps(slots []aggregateEvidenceSlot, support map[string][]map[string]any) ([]map[string]any, []string) {
	out := make([]map[string]any, 0, len(slots))
	missing := []string{}
	for _, slot := range slots {
		items := support[slot.ID]
		status := "missing"
		if len(items) > 0 {
			status = "covered"
		} else if slot.Required {
			missing = appendUniqueStringEnv(missing, firstNonEmpty(slot.Label, slot.ID))
		}
		mapped := map[string]any{
			"id":            slot.ID,
			"kind":          slot.Kind,
			"label":         slot.Label,
			"required":      slot.Required,
			"status":        status,
			"terms":         append([]string(nil), slot.Terms...),
			"support_count": len(items),
		}
		if len(items) > 0 {
			mapped["support"] = items
		}
		out = append(out, mapped)
	}
	return out, missing
}

func aggregateClaimTextsBySupport(claims []map[string]any, supportValues ...string) []string {
	allowed := map[string]struct{}{}
	for _, value := range supportValues {
		allowed[value] = struct{}{}
	}
	out := []string{}
	for _, claim := range claims {
		support := strings.TrimSpace(fmt.Sprint(claim["support"]))
		if _, ok := allowed[support]; !ok {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(claim["text"]))
		if text != "" {
			out = append(out, text)
		}
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func aggregateEvidencePayloadTextForQuery(payload map[string]any, query string, maxChars int) string {
	parts := []string{}
	collectAggregateEvidenceStrings(payload, &parts, 0)
	return evidencePayloadTextForQuery(parts, query, maxChars)
}

func evidenceLedgerPayloadText(payload map[string]any, maxChars int) string {
	parts := []string{}
	collectEvidenceLedgerAssertionStrings(payload, &parts, 0)
	return evidencePayloadTextFromParts(parts, maxChars)
}

func evidenceLedgerPayloadTextForQuery(payload map[string]any, query string, maxChars int) string {
	parts := []string{}
	collectEvidenceLedgerAssertionStrings(payload, &parts, 0)
	return evidencePayloadTextForQuery(parts, query, maxChars)
}

func aggregateEvidenceScanMaxTokens(maxTokens int) int {
	if maxTokens < defaultAggregateEvidenceScanMaxTokens {
		return defaultAggregateEvidenceScanMaxTokens
	}
	return maxTokens
}

func evidencePayloadTextFromParts(parts []string, maxChars int) string {
	parts = uniqueNonEmptyStringsEnv(parts)
	if len(parts) > 12 {
		parts = parts[:12]
	}
	return limitContextEvidenceDigestText(strings.Join(parts, "\n"), maxChars)
}

type evidencePayloadSnippet struct {
	Text        string
	Score       int
	SourceIndex int
	Start       int
}

func evidencePayloadTextForQuery(parts []string, query string, maxChars int) string {
	parts = uniqueNonEmptyStringsEnv(parts)
	if strings.TrimSpace(query) == "" || len(parts) == 0 {
		return evidencePayloadTextFromParts(parts, maxChars)
	}
	terms := evidencePayloadQueryTerms(query)
	if len(terms) == 0 {
		return evidencePayloadTextFromParts(parts, maxChars)
	}
	answerType := classifyContextQueryAnswerType(query)
	windowChars := evidencePayloadSnippetWindowChars(maxChars)
	snippets := []evidencePayloadSnippet{}
	for sourceIndex, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		for _, start := range evidencePayloadSnippetPositions(part, lower, terms, answerType) {
			text := evidencePayloadSnippetAround(part, start, windowChars)
			score := evidencePayloadSnippetScore(text, terms, answerType)
			if score <= 0 {
				continue
			}
			snippets = append(snippets, evidencePayloadSnippet{
				Text:        text,
				Score:       score,
				SourceIndex: sourceIndex,
				Start:       start,
			})
		}
	}
	if len(snippets) == 0 {
		return evidencePayloadTextFromParts(parts, maxChars)
	}
	sort.SliceStable(snippets, func(i, j int) bool {
		if snippets[i].Score != snippets[j].Score {
			return snippets[i].Score > snippets[j].Score
		}
		if snippets[i].SourceIndex != snippets[j].SourceIndex {
			return snippets[i].SourceIndex < snippets[j].SourceIndex
		}
		return snippets[i].Start < snippets[j].Start
	})
	selected := []string{}
	seen := map[string]struct{}{}
	usedChars := 0
	for _, snippet := range snippets {
		text := strings.TrimSpace(snippet.Text)
		if text == "" {
			continue
		}
		key := evidencePayloadSnippetDedupeKey(text)
		if _, ok := seen[key]; ok {
			continue
		}
		extra := len(text)
		if len(selected) > 0 {
			extra++
		}
		if maxChars > 0 && usedChars+extra > maxChars {
			remaining := maxChars - usedChars
			if len(selected) > 0 {
				remaining--
			}
			if remaining < 96 {
				continue
			}
			text = limitContextEvidenceDigestText(text, remaining)
			extra = len(text)
			if len(selected) > 0 {
				extra++
			}
		}
		selected = append(selected, text)
		seen[key] = struct{}{}
		usedChars += extra
		if len(selected) >= 4 {
			break
		}
	}
	if len(selected) == 0 {
		return evidencePayloadTextFromParts(parts, maxChars)
	}
	return strings.Join(selected, "\n")
}

func evidencePayloadQueryTerms(query string) []string {
	out := []string{}
	baseTerms := splitContextEvidenceTerms(query)
	add := func(term string) {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			return
		}
		if !strings.HasPrefix(term, "$") && len(term) < 3 {
			return
		}
		if contextQuestionStopwords[term] {
			return
		}
		switch term {
		case "where", "when", "what", "which", "long", "duration", "time", "answer", "fact", "facts":
			return
		}
		out = appendUniqueStringEnv(out, term)
	}
	for i, term := range baseTerms {
		add(term)
		if i+1 < len(baseTerms) {
			add(term + " " + baseTerms[i+1])
		}
	}
	for _, term := range aggregateEvidenceTerms([]string{query}) {
		if strings.Contains(term, " ") && len(splitContextEvidenceTerms(term)) > 3 {
			continue
		}
		add(term)
	}
	return out
}

func evidencePayloadSnippetPositions(text, lower string, terms []string, answerType string) []int {
	positions := []int{}
	for _, term := range terms {
		start := 0
		count := 0
		for start < len(lower) && count < 16 {
			idx := strings.Index(lower[start:], term)
			if idx < 0 {
				break
			}
			absolute := start + idx
			positions = appendEvidencePayloadPosition(positions, absolute)
			start = absolute + maxInt(1, len(term))
			count++
		}
	}
	for _, pattern := range evidencePayloadAnswerPatterns(answerType, text) {
		for _, match := range pattern.FindAllStringIndex(text, 16) {
			if len(match) == 2 {
				positions = appendEvidencePayloadPosition(positions, match[0])
			}
		}
	}
	return positions
}

func evidencePayloadAnswerPatterns(answerType, query string) []*regexp.Regexp {
	switch answerType {
	case "duration":
		return []*regexp.Regexp{evidenceLedgerDurationPattern}
	case "temporal":
		return []*regexp.Regexp{evidenceLedgerDatePattern, evidenceLedgerDurationPattern}
	case "location":
		return []*regexp.Regexp{evidenceLedgerLocationPattern, evidenceLedgerMoneyPattern}
	case "count", "comparison":
		return []*regexp.Regexp{evidenceLedgerNumberPattern, evidenceLedgerMoneyPattern, evidenceLedgerScalePattern}
	default:
		if strings.Contains(query, "$") {
			return []*regexp.Regexp{evidenceLedgerMoneyPattern}
		}
		return nil
	}
}

func appendEvidencePayloadPosition(positions []int, position int) []int {
	if position < 0 {
		return positions
	}
	for _, existing := range positions {
		if existing == position {
			return positions
		}
	}
	return append(positions, position)
}

func evidencePayloadSnippetWindowChars(maxChars int) int {
	if maxChars <= 0 {
		return 360
	}
	if maxChars <= 180 {
		return maxChars
	}
	return minIntEnv(260, maxInt(140, (maxChars/2)-8))
}

func evidencePayloadSnippetAround(text string, position, maxChars int) string {
	text = strings.TrimSpace(text)
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	if position < 0 {
		position = 0
	}
	if position > len(text) {
		position = len(text)
	}
	start := position - maxChars/2
	if start < 0 {
		start = 0
	}
	end := start + maxChars
	if end > len(text) {
		end = len(text)
		start = maxInt(0, end-maxChars)
	}
	snippet := strings.TrimSpace(text[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(text) {
		snippet = strings.TrimSpace(snippet) + "..."
	}
	return snippet
}

func evidencePayloadSnippetScore(text string, terms []string, answerType string) int {
	lower := strings.ToLower(text)
	score := 0
	for _, term := range terms {
		if strings.Contains(lower, term) {
			score += 3
			if strings.Contains(term, " ") {
				score += 2
			}
		}
	}
	if evidenceLedgerDurationPattern.MatchString(text) {
		score += 4
		if answerType == "duration" || answerType == "temporal" {
			score += 8
		}
	}
	if evidenceLedgerDatePattern.MatchString(text) {
		score += 3
		if answerType == "temporal" {
			score += 7
		}
	}
	if evidenceLedgerMoneyPattern.MatchString(text) {
		score += 3
	}
	if evidenceLedgerLocationPattern.MatchString(text) {
		score += 3
		if answerType == "location" {
			score += 8
		}
	}
	if evidenceLedgerScalePattern.MatchString(text) {
		score += 5
	}
	return score
}

func evidencePayloadSnippetDedupeKey(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if len(text) > 96 {
		text = text[:96]
	}
	return text
}

func collectAggregateEvidenceStrings(value any, out *[]string, depth int) {
	if depth > 5 || len(*out) >= 32 {
		return
	}
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if aggregateEvidenceUsefulText(text) {
			*out = append(*out, text)
		}
	case []any:
		for _, item := range typed {
			collectAggregateEvidenceStrings(item, out, depth+1)
		}
	case []string:
		for _, item := range typed {
			collectAggregateEvidenceStrings(item, out, depth+1)
		}
	case map[string]any:
		for _, key := range []string{"atomic_text", "summary", "content", "statement", "text", "description", "notes", "title", "value", "entities", "keywords"} {
			if item, ok := typed[key]; ok {
				collectAggregateEvidenceStrings(item, out, depth+1)
			}
		}
		for _, key := range []string{"named_memory", "claim", "task_context", "task", "session", "event", "tool_call", "artifact", "trajectory", "result"} {
			if item, ok := typed[key]; ok {
				collectAggregateEvidenceStrings(item, out, depth+1)
			}
		}
	}
}

func collectEvidenceLedgerAssertionStrings(value any, out *[]string, depth int) {
	if depth > 5 || len(*out) >= 32 {
		return
	}
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if aggregateEvidenceUsefulText(text) {
			*out = append(*out, text)
		}
	case []any:
		for _, item := range typed {
			collectEvidenceLedgerAssertionStrings(item, out, depth+1)
		}
	case []string:
		for _, item := range typed {
			collectEvidenceLedgerAssertionStrings(item, out, depth+1)
		}
	case map[string]any:
		for _, key := range []string{"atomic_text", "content", "statement", "text", "notes", "value"} {
			if item, ok := typed[key]; ok {
				collectEvidenceLedgerAssertionStrings(item, out, depth+1)
			}
		}
		for _, key := range []string{"named_memory", "claim", "task_context", "task", "session", "event", "tool_call", "artifact", "trajectory", "result"} {
			if item, ok := typed[key]; ok {
				collectEvidenceLedgerAssertionStrings(item, out, depth+1)
			}
		}
	}
}

func aggregateEvidenceUsefulText(text string) bool {
	if len(text) < 3 {
		return false
	}
	lower := strings.ToLower(text)
	switch lower {
	case "true", "false", "<nil>":
		return false
	}
	if strings.HasPrefix(lower, "path:") || strings.HasPrefix(lower, "named_memory:") || strings.HasPrefix(lower, "memory_claim:") {
		return false
	}
	return true
}

var (
	evidenceLedgerDurationPattern = regexp.MustCompile(`(?i)\b\d+(?:\.\d+)?\s*(?:minutes?|mins?|hours?|hrs?|days?|weeks?|months?|years?)\b(?:\s+each\s+way)?`)
	evidenceLedgerDatePattern     = regexp.MustCompile(`(?i)\b(?:today|yesterday|tomorrow|last\s+(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday|week|month|year)|next\s+(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday|week|month|year)|(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday)|(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\s+\d{1,2}(?:,\s*\d{4})?|\d{4}-\d{2}-\d{2}|\d{1,2}/\d{1,2}/\d{2,4})\b`)
	evidenceLedgerMoneyPattern    = regexp.MustCompile(`(?i)(?:\$\s*\d+(?:\.\d+)?|\b\d+(?:\.\d+)?\s*(?:dollars?|usd)\b)`)
	evidenceLedgerNumberPattern   = regexp.MustCompile(`(?i)\b\d+(?:\.\d+)?\b`)
	evidenceLedgerScalePattern    = regexp.MustCompile(`(?i)\b\d+/\d+\s*(?:scale\s*)?(?:[A-Z0-9][A-Za-z0-9.'-]*(?:\s+[A-Z0-9][A-Za-z0-9.'-]*){0,5})`)
	evidenceLedgerLocationPattern = regexp.MustCompile(`(?i:\b(?:at|in|from|through|near|inside|outside|via|to|by|was|is))\s+([A-Z][A-Za-z0-9&'.-]*(?:\s+[A-Z][A-Za-z0-9&'.-]*){0,4})`)
)

func extractEvidenceLedgerValues(text string) evidenceLedgerValues {
	text = strings.TrimSpace(text)
	values := evidenceLedgerValues{
		Durations: uniqueNonEmptyStringsEnv(evidenceLedgerDurationPattern.FindAllString(text, -1)),
		Dates:     uniqueNonEmptyStringsEnv(evidenceLedgerDatePattern.FindAllString(text, -1)),
		Money:     uniqueNonEmptyStringsEnv(evidenceLedgerMoneyPattern.FindAllString(text, -1)),
		Numbers:   uniqueNonEmptyStringsEnv(evidenceLedgerNumberPattern.FindAllString(text, -1)),
	}
	values.Locations = extractEvidenceLedgerLocations(text)
	values.Items = extractEvidenceLedgerItems(text, values.Locations)
	return values
}

func extractEvidenceLedgerLocations(text string) []string {
	out := []string{}
	for _, match := range evidenceLedgerLocationPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		value := evidenceLedgerCleanLocationValue(match[1])
		if !evidenceLedgerCapitalizedValueUseful(value) {
			continue
		}
		out = appendUniqueStringEnv(out, value)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func evidenceLedgerCleanLocationValue(value string) string {
	value = strings.Trim(value, " \t\n\r.,;:!?()[]{}\"")
	words := strings.Fields(value)
	for len(words) > 0 && evidenceLedgerLocationNoiseWord(words[len(words)-1]) {
		words = words[:len(words)-1]
	}
	for len(words) > 0 && evidenceLedgerLocationNoiseWord(words[0]) {
		words = words[1:]
	}
	return strings.Join(words, " ")
}

func evidenceLedgerLocationNoiseWord(value string) bool {
	switch strings.ToLower(strings.Trim(value, " \t\n\r.,;:!?()[]{}\"")) {
	case "checkout", "counter", "store", "location", "place", "last", "next",
		"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
		"january", "february", "march", "april", "may", "june", "july", "august",
		"september", "october", "november", "december":
		return true
	default:
		return false
	}
}

func evidenceLedgerCapitalizedValueUseful(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 3 {
		return false
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "memory/") || strings.HasPrefix(lower, "longmem") {
		return false
	}
	first := strings.ToLower(strings.Fields(value)[0])
	switch first {
	case "i", "the", "that", "this", "user", "assistant", "summary", "result", "true", "false", "question", "answer", "evidence", "based", "use", "do", "if", "when", "where", "what", "how":
		return false
	default:
		return true
	}
}

func extractEvidenceLedgerItems(text string, capitalized []string) []string {
	out := []string{}
	for _, value := range evidenceLedgerScalePattern.FindAllString(text, -1) {
		value = strings.Trim(value, " \t\n\r.,;:!?()[]{}\"")
		out = appendUniqueStringEnv(out, value)
	}
	for _, value := range capitalized {
		if evidenceLedgerCapitalizedValueUseful(value) {
			out = appendUniqueStringEnv(out, value)
		}
	}
	if len(out) > 12 {
		return out[:12]
	}
	return out
}

func uniqueNonEmptyStringsEnv(values []string) []string {
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = appendUniqueStringEnv(out, value)
	}
	return out
}

func boolFromAnyEnv(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func (a *ReadOnlyAdapter) loadNamedMemoryEvidence(ctx context.Context, refStr, name string) (map[string]any, error) {
	if strings.TrimSpace(a.cfg.Storage.Root) == "" {
		return map[string]any{"ref": refStr, "loaded": false, "error": "memory store unavailable"}, nil
	}
	memStore, err := memorystore.OpenWithConfig(ctx, a.cfg)
	if err != nil {
		return map[string]any{"ref": refStr, "loaded": false, "error": err.Error()}, nil
	}
	defer func() { _ = memStore.Close() }()

	entry, err := memStore.Get(ctx, strings.TrimSpace(name), a.laneConfig().WorkspaceID)
	if err != nil {
		return map[string]any{"ref": refStr, "loaded": false, "error": err.Error()}, nil
	}
	body := map[string]any{
		"name":            entry.Name,
		"id":              entry.ID,
		"type":            entry.Type,
		"workspace":       entry.Workspace,
		"summary":         entry.Summary,
		"atomic_text":     entry.AtomicText,
		"entities":        entry.Entities,
		"keywords":        entry.Keywords,
		"lifecycle_state": entry.LifecycleState,
		"review_status":   entry.ReviewStatus,
	}
	if len(entry.Result) > 0 {
		var decoded any
		if err := json.Unmarshal(entry.Result, &decoded); err == nil {
			body["result"] = decoded
		} else {
			body["result"] = string(entry.Result)
		}
	}
	return map[string]any{
		"ref":          refStr,
		"loaded":       true,
		"named_memory": body,
	}, nil
}

func (a *ReadOnlyAdapter) loadTaskEvidence(ctx context.Context, refStr, taskID string) (map[string]any, error) {
	if a.taskStore == nil {
		return map[string]any{"ref": refStr, "loaded": false, "error": "task store unavailable"}, nil
	}
	task, err := a.taskStore.Get(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return map[string]any{"ref": refStr, "loaded": false, "error": err.Error()}, nil
	}
	taskContext := a.taskContextFromTask(ctx, task, task.WorkspaceID)
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
	graphRecommendation := contextAnswerGraphRecommendation(bundle, false, "none")
	trust := buildContextAnswerTrust(bundle, copyable)
	trust["graph"] = graphRecommendation
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
		"trust":             trust,
		"graph":             graphRecommendation,
		"answer_seed":       answerSeed,
		"categories":        bundle.Categories,
		"integration_edges": bundle.IntegrationEdges,
		"coverage_report":   bundle.CoverageReport,
		"evidence_digest":   buildContextEvidenceDigest(bundle),
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
			"checks":               bundle.Certificate.Checks,
			"required_evidence_ok": bundle.Certificate.RequiredEvidenceOK,
			"unsupported_facts":    bundle.Certificate.UnsupportedFacts,
			"stale_evidence_ids":   bundle.Certificate.StaleEvidenceIDs,
			"unloadable_refs":      bundle.Certificate.UnloadableRefs,
			"missing_evidence":     bundle.Certificate.MissingEvidence,
			"conflict_ids":         bundle.Certificate.ConflictIDs,
		}
	}
	return out
}

func buildContextAnswerTrust(bundle contextengine.ContextBundle, copyable bool) map[string]any {
	coverageMissing := []string{}
	coverageRequired := 0
	coverageCovered := 0
	if bundle.CoverageReport != nil {
		coverageMissing = append([]string(nil), bundle.CoverageReport.Missing...)
		for _, req := range bundle.CoverageReport.Requirements {
			if req.Required {
				coverageRequired++
			}
		}
		coveredReqs := map[string]struct{}{}
		for _, covered := range bundle.CoverageReport.Covered {
			if strings.TrimSpace(covered.RequirementID) != "" {
				coveredReqs[covered.RequirementID] = struct{}{}
			}
		}
		coverageCovered = len(coveredReqs)
	}
	certStatus := ""
	requiredEvidenceOK := false
	unloadableRefs := []string{}
	conflictIDs := []string{}
	staleEvidenceIDs := []string{}
	if bundle.Certificate != nil {
		certStatus = string(bundle.Certificate.Status)
		requiredEvidenceOK = bundle.Certificate.RequiredEvidenceOK
		for _, ref := range bundle.Certificate.UnloadableRefs {
			if formatted := contextengine.FormatEvidenceRef(ref); formatted != "" {
				unloadableRefs = append(unloadableRefs, formatted)
			}
		}
		conflictIDs = append([]string(nil), bundle.Certificate.ConflictIDs...)
		staleEvidenceIDs = append([]string(nil), bundle.Certificate.StaleEvidenceIDs...)
	}
	freshness := contextAnswerFreshness(bundle)
	liveOverlayUsed := contextAnswerProviderHit(bundle, "live_overlay")
	budgetExhausted := bundle.Telemetry.OmittedContextItems > 0
	selectedLoadable := len(unloadableRefs) == 0
	score := contextAnswerTrustScore(bundle, copyable, freshness, selectedLoadable, budgetExhausted, coverageMissing, conflictIDs, staleEvidenceIDs)
	confidence := contextAnswerConfidenceLabel(score, copyable, coverageMissing, certStatus)
	testsIncluded := contextAnswerSelectedPathHasTest(bundle.SelectedPaths)
	testsRequested := contextAnswerTestsRequested(bundle)
	nextAction := contextAnswerNextAction(copyable, bundle, confidence, testsIncluded, testsRequested, coverageMissing, selectedLoadable)
	return map[string]any{
		"confidence": map[string]any{
			"level":               confidence,
			"score":               score,
			"trusted_for_proceed": copyable,
		},
		"freshness": map[string]any{
			"repoindex":         freshness,
			"live_overlay_used": liveOverlayUsed,
		},
		"coverage": map[string]any{
			"required_ok":    requiredEvidenceOK && len(coverageMissing) == 0,
			"required_count": coverageRequired,
			"covered_count":  coverageCovered,
			"missing":        coverageMissing,
		},
		"scope": map[string]any{
			"component_roots":     contextAnswerComponentRoots(bundle.SelectedPaths),
			"path_families":       contextAnswerPathFamilies(bundle.SelectedPaths),
			"peripheral_roles":    contextAnswerPeripheralRoles(bundle.SelectedPaths),
			"source_profiles":     contextAnswerMetadataStrings(bundle.Metadata, "source_profiles"),
			"selected_path_count": len(bundle.SelectedPaths),
		},
		"tests": map[string]any{
			"included":  testsIncluded,
			"requested": testsRequested,
			"policy":    "omitted_by_default",
		},
		"graph": contextAnswerGraphRecommendation(bundle, false, "none"),
		"loadability": map[string]any{
			"selected_refs_loadable": selectedLoadable,
			"unloadable_refs":        unloadableRefs,
		},
		"budget": map[string]any{
			"exhausted":               budgetExhausted,
			"omitted_items":           bundle.Telemetry.OmittedContextItems,
			"raw_context_chars":       bundle.Telemetry.RawContextChars,
			"emitted_context_chars":   bundle.Telemetry.EmittedContextChars,
			"evidence_count":          len(bundle.Evidence),
			"selected_evidence_count": len(bundle.Facts),
		},
		"certificate": map[string]any{
			"status":               certStatus,
			"required_evidence_ok": requiredEvidenceOK,
			"stale_evidence_count": len(staleEvidenceIDs),
			"conflict_count":       len(conflictIDs),
		},
		"next_action": nextAction,
	}
}

func contextAnswerTrustScore(bundle contextengine.ContextBundle, copyable bool, freshness string, selectedLoadable bool, budgetExhausted bool, coverageMissing []string, conflictIDs []string, staleEvidenceIDs []string) float64 {
	score := 0.50
	if copyable {
		score += 0.28
	}
	if bundle.Answerable && bundle.Status == contextengine.ContextBundleStatusSufficient {
		score += 0.10
	}
	switch freshness {
	case "current":
		score += 0.08
	case "dirty":
		score += 0.03
	case "stale":
		score -= 0.12
	case "unknown":
		score -= 0.04
	}
	if selectedLoadable {
		score += 0.04
	} else {
		score -= 0.20
	}
	if len(coverageMissing) > 0 {
		score -= minFloat(0.22, float64(len(coverageMissing))*0.08)
	}
	if len(conflictIDs) > 0 {
		score -= 0.18
	}
	if len(staleEvidenceIDs) > 0 {
		score -= 0.08
	}
	if budgetExhausted {
		score -= 0.04
	}
	return clampScore(score)
}

func contextAnswerConfidenceLabel(score float64, copyable bool, coverageMissing []string, certStatus string) string {
	if !copyable || len(coverageMissing) > 0 || certStatus == string(contextengine.ContextCertificateStatusFailed) {
		if score >= 0.62 {
			return "medium"
		}
		return "low"
	}
	if score >= 0.82 {
		return "high"
	}
	if score >= 0.62 {
		return "medium"
	}
	return "low"
}

func contextAnswerFreshness(bundle contextengine.ContextBundle) string {
	if bundle.Metadata == nil {
		return "unknown"
	}
	freshness, _ := bundle.Metadata["repoindex_freshness"].(map[string]any)
	if freshness == nil {
		return "unknown"
	}
	level := strings.TrimSpace(fmt.Sprint(freshness["level"]))
	if level == "" {
		return "unknown"
	}
	return level
}

func contextAnswerProviderHit(bundle contextengine.ContextBundle, name string) bool {
	for _, group := range metadataAnySlice(bundle.Metadata, "code_search_provider_telemetry") {
		groupMap, _ := group.(map[string]any)
		for _, provider := range anySlice(groupMap["providers"]) {
			providerMap, _ := provider.(map[string]any)
			if strings.EqualFold(strings.TrimSpace(fmt.Sprint(providerMap["name"])), name) && intFromAnyEnv(providerMap["hit_count"]) > 0 {
				return true
			}
		}
	}
	return false
}

func contextAnswerSelectedPathHasTest(paths []contextengine.ContextSelectedPath) bool {
	for _, selected := range paths {
		if selectedPathMetadataString(selected.Metadata, "is_test") == "true" || strings.EqualFold(selectedPathMetadataString(selected.Metadata, "file_role"), "test") || isTestLikeCodeSearchPath(selected.Path) {
			return true
		}
	}
	return false
}

func contextAnswerTestsRequested(bundle contextengine.ContextBundle) bool {
	if strings.Contains(strings.ToLower(strings.TrimSpace(fmt.Sprint(bundle.Metadata["task_type"]))), "test") {
		return true
	}
	if bundle.CoverageReport != nil {
		for _, req := range bundle.CoverageReport.Requirements {
			if requiredEvidenceSuggestsTests([]string{req.ID, req.Kind, req.Label}) || requiredEvidenceSuggestsTests(req.Terms) {
				return true
			}
		}
	}
	return false
}

func contextAnswerComponentRoots(paths []contextengine.ContextSelectedPath) []string {
	out := []string{}
	for _, selected := range paths {
		out = appendUniqueStringEnv(out, selectedPathMetadataString(selected.Metadata, "component_root"))
	}
	return out
}

func contextAnswerPathFamilies(paths []contextengine.ContextSelectedPath) []string {
	out := []string{}
	for _, selected := range paths {
		out = appendUniqueStringEnv(out, selectedPathMetadataString(selected.Metadata, "path_family"))
	}
	if len(out) > 8 {
		return out[:8]
	}
	return out
}

func contextAnswerPeripheralRoles(paths []contextengine.ContextSelectedPath) []string {
	out := []string{}
	for _, selected := range paths {
		if selectedPathMetadataString(selected.Metadata, "is_test") == "true" || strings.EqualFold(selectedPathMetadataString(selected.Metadata, "file_role"), "test") || isTestLikeCodeSearchPath(selected.Path) {
			out = appendUniqueStringEnv(out, "test")
		}
		for _, key := range []string{"is_tooling", "is_generated", "is_hidden"} {
			if selectedPathMetadataString(selected.Metadata, key) == "true" {
				out = appendUniqueStringEnv(out, strings.TrimPrefix(key, "is_"))
			}
		}
		role := selectedPathMetadataString(selected.Metadata, "file_role")
		switch role {
		case "template", "generated", "tooling":
			out = appendUniqueStringEnv(out, role)
		}
	}
	return out
}

func contextAnswerMetadataStrings(metadata map[string]any, key string) []string {
	return metadataStringSliceEnv(metadata, key)
}

func contextAnswerGraphRecommendation(bundle contextengine.ContextBundle, used bool, mode string) map[string]any {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = "none"
	}
	recommended, required, reason := contextAnswerGraphSignals(bundle)
	rootRefs := answerSurfaceContextGraphRootRefs(bundle)
	nextTool := ""
	if !used && (recommended || required) && len(rootRefs) > 0 {
		nextTool = "expand_context_graph"
	}
	blockedBy := []string{}
	if !used && (recommended || required) && len(rootRefs) == 0 {
		blockedBy = append(blockedBy, "no_graph_roots")
	}
	return map[string]any{
		"mode":                  mode,
		"used":                  used,
		"recommended":           recommended,
		"graph_required":        required,
		"recommended_next_tool": recommendedNextToolValue(nextTool),
		"root_refs":             rootRefs,
		"blocked_by":            blockedBy,
		"reason":                reason,
	}
}

func recommendedNextToolValue(tool string) any {
	if strings.TrimSpace(tool) == "" {
		return nil
	}
	return tool
}

func contextAnswerGraphSignals(bundle contextengine.ContextBundle) (recommended bool, required bool, reason string) {
	taskType := strings.ToLower(strings.TrimSpace(fmt.Sprint(bundle.Metadata["task_type"])))
	switch taskType {
	case "execution_trace", "change_impact", "registration_trace", "integration_surface", "architecture_map", "subsystem_map":
		return true, true, "task_type:" + taskType
	}
	if len(bundle.IntegrationEdges) > 0 {
		return true, true, "integration_edges"
	}
	if contextAnswerContractMode(bundle) == "repo_subsystem_map" {
		return true, false, "repo_subsystem_map"
	}
	return false, false, "not_graph_sensitive"
}

func answerSurfaceContextGraphRootRefs(bundle contextengine.ContextBundle) []string {
	roots := answerSurfaceContextGraphRoots(bundle)
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if ref := contextengine.FormatEvidenceRef(contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: root}); ref != "" {
			out = append(out, ref)
		}
	}
	return out
}

func markContextAnswerGraphUsed(out map[string]any, mode string) {
	update := func(graph map[string]any) {
		if graph == nil {
			return
		}
		graph["mode"] = mode
		graph["used"] = true
		graph["recommended_next_tool"] = nil
		graph["blocked_by"] = []string{}
	}
	if graph, _ := out["graph"].(map[string]any); graph != nil {
		update(graph)
	}
	trust, _ := out["trust"].(map[string]any)
	if trust == nil {
		return
	}
	if graph, _ := trust["graph"].(map[string]any); graph != nil {
		update(graph)
	}
	if trust["next_action"] == "expand_context_graph" {
		if contract, _ := out["answer_contract"].(map[string]any); contract != nil && contract["copy_answer_seed"] == true {
			trust["next_action"] = "copy_answer_seed"
		} else if selected, _ := out["selected_paths"].([]contextengine.ContextSelectedPath); len(selected) > 0 {
			trust["next_action"] = "load_refs"
		}
	}
}

func contextAnswerGraphRecommended(bundle contextengine.ContextBundle) bool {
	recommended, _, _ := contextAnswerGraphSignals(bundle)
	return recommended
}

func contextAnswerNextAction(copyable bool, bundle contextengine.ContextBundle, confidence string, testsIncluded bool, testsRequested bool, coverageMissing []string, selectedLoadable bool) string {
	_, graphRequired, _ := contextAnswerGraphSignals(bundle)
	switch {
	case graphRequired:
		return "expand_context_graph"
	case copyable:
		return "copy_answer_seed"
	case !selectedLoadable:
		return "load_refs"
	case len(coverageMissing) > 0 && !testsIncluded && !testsRequested:
		return "broaden_search"
	case contextAnswerGraphRecommended(bundle) && confidence != "high":
		return "expand_context_graph"
	case len(bundle.SelectedPaths) > 0:
		return "load_refs"
	default:
		return "broad_explore"
	}
}

func metadataAnySlice(metadata map[string]any, key string) []any {
	if metadata == nil {
		return nil
	}
	return anySlice(metadata[key])
}

func anySlice(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	default:
		return nil
	}
}

func intFromAnyEnv(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		got, _ := typed.Int64()
		return int(got)
	default:
		return 0
	}
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
			symbols = appendUniqueStringEnv(symbols, strings.TrimSpace(candidate.Value))
		}
	}
	for _, fact := range bundle.Facts {
		if symbol := selectedPathMetadataString(fact.Metadata, "symbol"); symbol != "" {
			if path := selectedPathMetadataString(fact.Metadata, "path"); path != "" {
				symbol = path + "::" + symbol
			}
			symbols = appendUniqueStringEnv(symbols, symbol)
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

func buildContextEvidenceDigest(bundle contextengine.ContextBundle) map[string]any {
	claims := buildContextEvidenceDigestClaims(bundle, defaultContextEvidenceDigestMaxClaims)
	slots := buildContextEvidenceDigestSlots(bundle)
	return map[string]any{
		"schema_version": "context_evidence_digest/v1",
		"claims":         claims,
		"slots":          slots,
		"missing":        append([]contextengine.ContextGap(nil), bundle.Missing...),
		"conflicts":      append([]contextengine.ContextConflict(nil), bundle.Conflicts...),
		"load_refs":      contextEvidenceDigestLoadRefs(claims, slots, 12),
	}
}

func buildContextEvidenceDigestClaims(bundle contextengine.ContextBundle, limit int) []map[string]any {
	if limit <= 0 {
		limit = len(bundle.Facts)
	}
	out := make([]map[string]any, 0, minInt(limit, len(bundle.Facts)))
	for _, fact := range bundle.Facts {
		text := limitContextEvidenceDigestText(fact.Fact, defaultContextEvidenceDigestMaxClaimChars)
		if text == "" {
			continue
		}
		loadRefs := formattedEvidenceRefs(fact.Refs)
		item := map[string]any{
			"text":       text,
			"kind":       string(fact.Kind),
			"support":    string(fact.Status),
			"confidence": fact.Confidence,
			"load_refs":  loadRefs,
		}
		if coverageIDs := contextEvidenceDigestCoverageIDs(fact.Metadata); len(coverageIDs) > 0 {
			item["coverage_ids"] = coverageIDs
		}
		for _, key := range []string{"source", "source_profile", "candidate_role", "evidence_class", "fact_kind"} {
			if value := selectedPathMetadataString(fact.Metadata, key); value != "" {
				item[key] = value
			}
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func buildContextEvidenceDigestSlots(bundle contextengine.ContextBundle) []map[string]any {
	if bundle.CoverageReport == nil || len(bundle.CoverageReport.Requirements) == 0 {
		return []map[string]any{}
	}
	evidenceByID := map[string]contextengine.EvidenceNode{}
	for _, node := range bundle.Evidence {
		evidenceByID[node.ID] = node
	}
	coveredByRequirement := map[string][]contextengine.PathCoverage{}
	for _, covered := range bundle.CoverageReport.Covered {
		coveredByRequirement[covered.RequirementID] = append(coveredByRequirement[covered.RequirementID], covered)
	}
	missing := map[string]struct{}{}
	for _, id := range bundle.CoverageReport.Missing {
		missing[id] = struct{}{}
	}
	out := make([]map[string]any, 0, len(bundle.CoverageReport.Requirements))
	for _, req := range bundle.CoverageReport.Requirements {
		status := "missing"
		if _, ok := missing[req.ID]; !ok && len(coveredByRequirement[req.ID]) > 0 {
			status = "covered"
		}
		item := map[string]any{
			"id":       req.ID,
			"kind":     req.Kind,
			"label":    req.Label,
			"required": req.Required,
			"status":   status,
			"terms":    append([]string(nil), req.Terms...),
		}
		support := contextEvidenceDigestSlotSupport(coveredByRequirement[req.ID], evidenceByID)
		if len(support) > 0 {
			item["support"] = support
		}
		out = append(out, item)
	}
	return out
}

func contextEvidenceDigestSlotSupport(covered []contextengine.PathCoverage, evidenceByID map[string]contextengine.EvidenceNode) []map[string]any {
	out := make([]map[string]any, 0, len(covered))
	for _, item := range covered {
		support := map[string]any{
			"path":  item.Path,
			"score": item.Score,
		}
		loadRefs := []string{}
		for _, evidenceID := range item.EvidenceIDs {
			node, ok := evidenceByID[evidenceID]
			if !ok {
				continue
			}
			if ref := contextengine.FormatEvidenceRef(node.Ref); ref != "" {
				loadRefs = appendUniqueStringEnv(loadRefs, ref)
			}
		}
		if len(loadRefs) > 0 {
			support["load_refs"] = loadRefs
		}
		out = append(out, support)
	}
	return out
}

func contextEvidenceDigestCoverageIDs(metadata map[string]any) []string {
	out := metadataStringSliceEnv(metadata, "coverage_requirement_ids")
	if id := selectedPathMetadataString(metadata, "coverage_requirement_id"); id != "" {
		out = appendUniqueStringEnv(out, id)
	}
	return out
}

func contextEvidenceDigestLoadRefs(claims []map[string]any, slots []map[string]any, limit int) []string {
	out := []string{}
	addRefs := func(value any) {
		for _, ref := range anyStringSliceEnv(value) {
			out = appendUniqueStringEnv(out, ref)
			if limit > 0 && len(out) >= limit {
				return
			}
		}
	}
	for _, claim := range claims {
		addRefs(claim["load_refs"])
		if limit > 0 && len(out) >= limit {
			return out
		}
	}
	for _, slot := range slots {
		for _, support := range anyMapSliceEnv(slot["support"]) {
			addRefs(support["load_refs"])
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func formattedEvidenceRefs(refs []contextengine.EvidenceRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if formatted := contextengine.FormatEvidenceRef(ref); formatted != "" {
			out = appendUniqueStringEnv(out, formatted)
		}
	}
	return out
}

func limitContextEvidenceDigestText(value string, maxChars int) string {
	value = strings.TrimSpace(value)
	if maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxChars {
		return value
	}
	return strings.TrimSpace(string(runes[:maxChars])) + "..."
}

func anyStringSliceEnv(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func anyMapSliceEnv(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
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
