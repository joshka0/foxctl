package env

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	skillobs "github.com/jkatigb/agentctl/internal/adapters/skillslib/obs"
	"github.com/jkatigb/agentctl/internal/context/contextplane"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
	memtokens "github.com/jkatigb/agentctl/internal/runtime/actor/memory"
	"github.com/jkatigb/agentctl/internal/runtime/engine"
	"github.com/jkatigb/agentctl/internal/runtime/observability"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
)

const (
	codeSearchTaskFileLocate        = "file_locate"
	codeSearchTaskExecutionTrace    = "execution_trace"
	codeSearchTaskSymbolInspect     = "symbol_inspect"
	codeSearchTaskChangeImpact      = "change_impact"
	codeSearchTaskRegistrationTrace = "registration_trace"
	codeSearchSemanticStageTimeout  = 12 * time.Second
)

type codeSearchEnsembleInput struct {
	Query          string                     `json:"query"`
	TaskType       string                     `json:"task_type"`
	CandidatePaths []string                   `json:"candidate_paths"`
	Planner        codeSearchLLMPlannerInput  `json:"llm_planner,omitempty"`
	Selector       codeSearchLLMSelectorInput `json:"llm_selector,omitempty"`
	Constraints    struct {
		ExcludePaths     []string `json:"exclude_paths"`
		IncludeHistory   bool     `json:"include_history"`
		IncludeACA       bool     `json:"include_aca"`
		RequireGrounding *bool    `json:"require_grounding"`
	} `json:"constraints"`
	Budget struct {
		MaxSteps      int  `json:"max_steps"`
		MaxCandidates int  `json:"max_candidates"`
		MaxFiles      int  `json:"max_files"`
		MaxSnippets   int  `json:"max_snippets"`
		MaxTokensOut  int  `json:"max_tokens_out"`
		AllowScouts   bool `json:"allow_scouts"`
	} `json:"budget"`
}

type codeSearchLLMSelectorInput struct {
	Enabled       bool   `json:"enabled"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	MaxCandidates int    `json:"max_candidates,omitempty"`
}

type codeSearchLLMPlannerInput struct {
	Enabled       bool   `json:"enabled"`
	EnableReplan  bool   `json:"enable_replan,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	MaxCandidates int    `json:"max_candidates,omitempty"`
}

type codeSearchEvidenceFile struct {
	Path         string   `json:"path"`
	Why          string   `json:"why,omitempty"`
	SupportScore float64  `json:"support_score,omitempty"`
	ConfirmedBy  []string `json:"confirmed_by,omitempty"`
}

type codeSearchEvidenceSymbol struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol,omitempty"`
	Line   int    `json:"line,omitempty"`
	Why    string `json:"why,omitempty"`
}

type codeSearchEvidenceSnippet struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type codeSearchCandidate struct {
	Path        string
	Why         string
	Sources     map[string]struct{}
	Support     float64
	LineHints   []int
	Symbols     []string
	RepoNodeIDs []string
}

type codeSearchLLMSelectorOutput struct {
	KeepPaths []string `json:"keep_paths"`
	Rationale string   `json:"rationale,omitempty"`
}

type codeSearchLLMPlannerOutput struct {
	SeedQueries     []string `json:"seed_queries"`
	PathBiases      []string `json:"path_biases"`
	EdgePriorities  []string `json:"edge_priorities"`
	MustCover       []string `json:"must_cover"`
	AmbiguityFamily string   `json:"ambiguity_family"`
	MaxWidenSteps   int      `json:"max_widen_steps"`
	Rationale       string   `json:"rationale"`
}

type codeSearchLLMReplannerOutput struct {
	AddSeedQueries      []string `json:"add_seed_queries"`
	AddPathBiases       []string `json:"add_path_biases"`
	RaiseEdgePriorities []string `json:"raise_edge_priorities"`
	MustCover           []string `json:"must_cover"`
	PreferPaths         []string `json:"prefer_paths"`
	DemotePaths         []string `json:"demote_paths"`
	AmbiguityFamily     string   `json:"ambiguity_family"`
	StopBroadening      bool     `json:"stop_broadening"`
	Rationale           string   `json:"rationale"`
}

type codeSearchRouteFamily string

const (
	codeSearchRouteCode            codeSearchRouteFamily = "code"
	codeSearchRouteInfraResource   codeSearchRouteFamily = "infra_resource"
	codeSearchRoutePackageOwner    codeSearchRouteFamily = "package_ownership"
	codeSearchRouteCochangeHistory codeSearchRouteFamily = "cochange_history"
)

type codeSearchCandidateTrace struct {
	Path          string   `json:"path"`
	Why           string   `json:"why,omitempty"`
	SupportScore  float64  `json:"support_score"`
	Sources       []string `json:"sources,omitempty"`
	LineHints     []int    `json:"line_hints,omitempty"`
	Symbols       []string `json:"symbols,omitempty"`
	EvidenceClass string   `json:"evidence_class,omitempty"`
	AnchorRole    string   `json:"anchor_role,omitempty"`
	RepoNodeCount int      `json:"repo_node_count,omitempty"`
	Selected      bool     `json:"selected,omitempty"`
	SelectedRank  int      `json:"selected_rank,omitempty"`
	PruneReason   string   `json:"prune_reason,omitempty"`
}

type codeSearchTraceAnchors struct {
	SourceQuery       string
	TargetQuery       string
	SourceExactProbes []string
	TargetExactProbes []string
	SourcePathProbes  []string
	TargetPathProbes  []string
}

func (a *ReadOnlyAdapter) codeSearchEnsemble(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	started := time.Now()
	var input codeSearchEnsembleInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	taskType, defaulted := normalizeCodeSearchTaskType(input.TaskType)
	if input.Budget.MaxCandidates <= 0 {
		input.Budget.MaxCandidates = 8
	}
	if input.Budget.MaxFiles <= 0 {
		input.Budget.MaxFiles = 4
	}
	if input.Budget.MaxSnippets <= 0 {
		input.Budget.MaxSnippets = 4
	}
	requireGrounding := true
	if input.Constraints.RequireGrounding != nil {
		requireGrounding = *input.Constraints.RequireGrounding
	}

	normalizedCandidates := normalizeCodeSearchPaths(input.CandidatePaths)
	excluded := normalizeCodeSearchPaths(input.Constraints.ExcludePaths)
	phraseIdentifierProbes := codeSearchPhraseIdentifierProbes(input.Query)
	traceAnchors := deriveExecutionTraceAnchors(input.Query, taskType)
	exactProbes := codeSearchTaskExactProbes(input.Query, taskType)
	pathProbes := codeSearchTaskPathProbes(input.Query, taskType, exactProbes)
	preferredSymbolProbes := codeSearchSymbolProbes(input.Query)
	metadata := map[string]any{
		"task_type_defaulted": defaulted,
		"lanes_used":          []string{},
		"grounded":            false,
		"scouts_used":         []string{},
		"history_requested":   input.Constraints.IncludeHistory,
		"aca_requested":       input.Constraints.IncludeACA,
	}

	candidates := map[string]*codeSearchCandidate{}
	gaps := make([]string, 0, 4)
	var routeFamily codeSearchRouteFamily
	var acaHits []contextplane.RetrievalHit
	plannerOut := codeSearchLLMPlannerOutput{}
	if input.Planner.Enabled {
		stageStart := time.Now()
		var plannerMeta map[string]any
		var plannerUsage *skillobs.TokenUsage
		var plannerErr error
		plannerOut, plannerMeta, plannerUsage, plannerErr = a.codeSearchLLMPlanner(ctx, input.Query, taskType, exactProbes, pathProbes, input.Planner)
		a.telemetry.record("llm_planner", time.Since(stageStart), plannerUsage)
		if plannerErr != nil {
			gaps = append(gaps, "llm planner failed: "+plannerErr.Error())
		} else {
			metadata["llm_planner"] = plannerMeta
			exactProbes = mergeCodeSearchProbeLists(exactProbes, plannerOut.SeedQueries)
			pathProbes = mergeCodeSearchProbeLists(pathProbes, plannerOut.PathBiases)
			if taskType == codeSearchTaskExecutionTrace {
				traceAnchors.SourceExactProbes = mergeCodeSearchProbeLists(traceAnchors.SourceExactProbes, plannerOut.SeedQueries)
				traceAnchors.TargetExactProbes = mergeCodeSearchProbeLists(traceAnchors.TargetExactProbes, plannerOut.SeedQueries)
				traceAnchors.SourcePathProbes = mergeCodeSearchProbeLists(traceAnchors.SourcePathProbes, plannerOut.PathBiases)
				traceAnchors.TargetPathProbes = mergeCodeSearchProbeLists(traceAnchors.TargetPathProbes, plannerOut.PathBiases)
			}
		}
	}
	phraseProbeSet := map[string]struct{}{}
	if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskChangeImpact {
		for _, probe := range phraseIdentifierProbes {
			probe = strings.TrimSpace(strings.ToLower(probe))
			if probe == "" {
				continue
			}
			phraseProbeSet[probe] = struct{}{}
		}
	}
	sourcePathProbeSet := stringSet(traceAnchors.SourcePathProbes)
	targetPathProbeSet := stringSet(traceAnchors.TargetPathProbes)
	sourceExactProbeSet := stringSet(traceAnchors.SourceExactProbes)
	targetExactProbeSet := stringSet(traceAnchors.TargetExactProbes)
	if traceAnchors.SourceQuery != "" {
		metadata["trace_source_query"] = traceAnchors.SourceQuery
	}
	if traceAnchors.TargetQuery != "" {
		metadata["trace_target_query"] = traceAnchors.TargetQuery
	}
	addLane := func(name string) {
		raw, _ := metadata["lanes_used"].([]string)
		for _, existing := range raw {
			if existing == name {
				return
			}
		}
		metadata["lanes_used"] = append(raw, name)
	}

	for _, candidate := range normalizedCandidates {
		if isExcludedCodeSearchPath(candidate, excluded) {
			continue
		}
		addCodeSearchCandidate(candidates, candidate, "caller supplied candidate", "caller", 1.0, 0, "", "")
	}

	for _, probe := range pathProbes {
		stageStart := time.Now()
		hits, err := codeSearchPathProbeSearch(a.workspaceRoot, probe, input.Budget.MaxCandidates)
		a.telemetry.record("path_probe", time.Since(stageStart), nil)
		if err != nil {
			gaps = append(gaps, "path probe search failed for "+probe+": "+err.Error())
			continue
		}
		if len(hits) == 0 {
			continue
		}
		addLane("path_probe")
		for _, hit := range hits {
			if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
				continue
			}
			why := "path probe: " + probe
			support := 0.92
			if _, ok := phraseProbeSet[strings.TrimSpace(strings.ToLower(probe))]; ok {
				support = 1.25
			}
			addCodeSearchCandidate(candidates, hit.Path, why, "path_probe", support, 0, "", "")
			if _, ok := sourcePathProbeSet[probe]; ok {
				addCodeSearchCandidate(candidates, hit.Path, "trace source anchor: "+probe, "trace_source_anchor", 0.45, 0, "", "")
			}
			if _, ok := targetPathProbeSet[probe]; ok {
				addCodeSearchCandidate(candidates, hit.Path, "trace target anchor: "+probe, "trace_target_anchor", 0.3, 0, "", "")
			}
		}
	}

	for _, probe := range exactProbes {
		stageStart := time.Now()
		hits, err := exactCodeProbeSearch(a.workspaceRoot, probe, input.Budget.MaxCandidates)
		a.telemetry.record("exact_probe", time.Since(stageStart), nil)
		if err != nil {
			gaps = append(gaps, "exact probe search failed for "+probe+": "+err.Error())
			continue
		}
		if len(hits) == 0 {
			continue
		}
		addLane("exact_probe")
		for _, hit := range hits {
			if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
				continue
			}
			why := "exact code probe: " + probe
			support := codeSearchFileWeight(hit.Path)
			if _, ok := phraseProbeSet[strings.TrimSpace(strings.ToLower(probe))]; ok && support < 1.05 {
				support = 1.05
			}
			addCodeSearchCandidate(candidates, hit.Path, why, "exact_probe", support, hit.Line, "", "")
			if _, ok := sourceExactProbeSet[probe]; ok && codeSearchPathTermScore(hit.Path, splitCodeSearchProbe(probe)) > 0 {
				addCodeSearchCandidate(candidates, hit.Path, "trace source anchor: "+probe, "trace_source_anchor", 0.55, hit.Line, "", "")
			}
			if _, ok := targetExactProbeSet[probe]; ok {
				addCodeSearchCandidate(candidates, hit.Path, "trace target anchor: "+probe, "trace_target_anchor", 0.35, hit.Line, "", "")
			}
		}
	}

	if taskType == codeSearchTaskFileLocate || taskType == codeSearchTaskSymbolInspect {
		semanticCtx, cancel := context.WithTimeout(ctx, codeSearchSemanticStageTimeout)
		defer cancel()
		if results, err := a.semanticSearchCode(semanticCtx, mustJSON(map[string]any{
			"query":   strings.TrimSpace(input.Query),
			"limit":   input.Budget.MaxCandidates,
			"profile": "code",
			"scope":   []string{"symbols"},
		})); err == nil {
			if bundleItems := semanticBundleMaps(results["candidate_bundles"]); len(bundleItems) > 0 {
				addLane("semantic_bundle")
				for _, item := range bundleItems {
					primaryPath := normalizeCodeSearchPath(stringValue(item["primary_path"]))
					if primaryPath == "" || isExcludedCodeSearchPath(primaryPath, excluded) {
						continue
					}
					why := firstNonEmpty(stringValue(item["match_reason"]), "semantic bundle primary")
					score := scoreValue(item["score"], 0.75)
					primaryAffinity := codeSearchBasenameProbeScore(primaryPath, pathProbes, exactProbes)
					if score < 0.72 {
						score = 0.72
					}
					if primaryAffinity > 0 && score < 0.82 {
						score = 0.82
					}
					addCodeSearchCandidate(candidates, primaryPath, why, "semantic_bundle_primary", score, 0, "", "")
					for _, related := range stringSliceValue(item["related_paths"]) {
						related = normalizeCodeSearchPath(related)
						if related == "" || isExcludedCodeSearchPath(related, excluded) {
							continue
						}
						relatedScore := score * 0.82
						if relatedScore < 0.58 {
							relatedScore = 0.58
						}
						if primaryAffinity > 0 && relatedScore < 0.72 {
							relatedScore = 0.72
						}
						addCodeSearchCandidate(candidates, related, "semantic bundle companion for "+primaryPath, "semantic_bundle_related", relatedScore, 0, "", "")
					}
				}
			}
			for _, item := range repoResultMaps(results["results"]) {
				pathValue := normalizeCodeSearchPath(stringValue(item["path"]))
				if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
					continue
				}
				why := firstNonEmpty(stringValue(item["summary"]), "semantic candidate")
				addCodeSearchCandidate(candidates, pathValue, why, "semantic_search_code", scoreValue(item["similarity"], 0.6), 0, stringValue(item["name"]), "")
			}
		} else {
			gaps = append(gaps, "semantic_search_code failed: "+err.Error())
		}
	}

	if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskChangeImpact {
		for _, probe := range exactProbes {
			stageStart := time.Now()
			hits, err := executionBridgeSearch(a.workspaceRoot, probe, input.Budget.MaxCandidates)
			a.telemetry.record("execution_bridge", time.Since(stageStart), nil)
			if err != nil {
				gaps = append(gaps, "execution bridge search failed for "+probe+": "+err.Error())
				continue
			}
			if len(hits) == 0 {
				continue
			}
			addLane("execution_bridge")
			for _, hit := range hits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				why := "execution bridge for " + probe
				addCodeSearchCandidate(candidates, hit.Path, why, "execution_bridge", 1.15, hit.Line, "", "")
			}
		}
	}

	var motifHits []contextplane.RepoMotifSearchHit
	motifPathScores := map[string]float64{}
	if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskFileLocate {
		stageStart := time.Now()
		var motifErr error
		motifHits, motifErr = a.searchRepoMotifs(ctx, input.Query, input.Budget.MaxCandidates)
		a.telemetry.record("repo_motif", time.Since(stageStart), nil)
		if motifErr != nil {
			gaps = append(gaps, "repo motif search failed: "+motifErr.Error())
		} else if len(motifHits) > 0 {
			motifPathScores = repoMotifPathScores(motifHits)
		}
	}

	store, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	querySvc := repoquery.NewQueryService(repoindex.NewQueryEngine(store))

	for _, probe := range codeSearchRepoProbes(input.Query) {
		stageStart := time.Now()
		searchOut, err := querySvc.SearchWithProjection(ctx, repoquery.SearchRequest{
			Query: probe,
			Limit: input.Budget.MaxCandidates,
		})
		a.telemetry.record("repo_index", time.Since(stageStart), nil)
		if err != nil {
			gaps = append(gaps, "repo search failed for "+probe+": "+err.Error())
			continue
		}
		if len(searchOut.Anchors) == 0 {
			continue
		}
		addLane("repo_index")
		for i, anchor := range searchOut.Anchors {
			pathValue := normalizeCodeSearchPath(anchor.Path)
			if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
				continue
			}
			nodeID := ""
			if i < len(searchOut.Nodes) {
				nodeID = strings.TrimSpace(searchOut.Nodes[i].ID)
			}
			why := firstNonEmpty(strings.TrimSpace(anchor.Summary), "repo-index anchor")
			addCodeSearchCandidate(candidates, pathValue, why, "search_repo", anchor.Score, anchor.LineHint, anchor.SymbolName, nodeID)
		}
	}
	if taskType == codeSearchTaskExecutionTrace {
		for _, side := range []struct {
			query       string
			exactProbes []string
			source      string
			label       string
		}{
			{query: traceAnchors.SourceQuery, exactProbes: traceAnchors.SourceExactProbes, source: "trace_source_repo", label: "trace source repo anchor"},
			{query: traceAnchors.TargetQuery, exactProbes: traceAnchors.TargetExactProbes, source: "trace_target_repo", label: "trace target repo anchor"},
		} {
			probes := codeSearchExecutionTraceSideRepoProbes(side.query, side.exactProbes)
			for _, probe := range probes {
				stageStart := time.Now()
				searchOut, err := querySvc.SearchWithProjection(ctx, repoquery.SearchRequest{
					Query: probe,
					Limit: input.Budget.MaxCandidates,
				})
				a.telemetry.record("repo_index", time.Since(stageStart), nil)
				if err != nil {
					gaps = append(gaps, "repo search failed for "+probe+": "+err.Error())
					continue
				}
				if len(searchOut.Anchors) == 0 {
					continue
				}
				addLane("trace_anchor_repo")
				for i, anchor := range searchOut.Anchors {
					pathValue := normalizeCodeSearchPath(anchor.Path)
					if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
						continue
					}
					nodeID := ""
					if i < len(searchOut.Nodes) {
						nodeID = strings.TrimSpace(searchOut.Nodes[i].ID)
					}
					why := firstNonEmpty(strings.TrimSpace(anchor.Summary), side.label)
					addCodeSearchCandidate(candidates, pathValue, why, "search_repo", anchor.Score*0.45, anchor.LineHint, anchor.SymbolName, nodeID)
					addCodeSearchCandidate(candidates, pathValue, side.label+": "+probe, side.source, 0.95, anchor.LineHint, anchor.SymbolName, nodeID)
				}
			}
		}
	}

	if len(motifHits) > 0 {
		if applyRepoMotifSupport(candidates, motifHits, taskType, excluded) > 0 {
			addLane("repo_motif")
		}
	}

	if taskType == codeSearchTaskExecutionTrace && len(candidates) > 0 {
		stageStart := time.Now()
		bridgeHits, bridgeQueries, bridgeGaps := a.codeSearchBridgeTupleAugment(ctx, querySvc, candidates, input.Query, input.Budget.MaxCandidates)
		a.telemetry.record("bridge_tuple", time.Since(stageStart), nil)
		gaps = append(gaps, bridgeGaps...)
		if len(bridgeQueries) > 0 {
			addLane("bridge_tuple")
			metadata["bridge_queries"] = bridgeQueries
			for _, hit := range bridgeHits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				addCodeSearchCandidate(candidates, hit.Path, "bridge tuple from "+hit.SourcePath, "bridge_tuple", 1.35, hit.Line, hit.Symbol, hit.NodeID)
			}
		}
	}

	if len(candidates) == 0 && taskType != codeSearchTaskExecutionTrace {
		semanticCtx, cancel := context.WithTimeout(ctx, codeSearchSemanticStageTimeout)
		defer cancel()
		if results, err := a.semanticSearchCode(semanticCtx, mustJSON(map[string]any{
			"query": strings.TrimSpace(input.Query),
			"limit": input.Budget.MaxCandidates,
			"scope": []string{"symbols", "codemaps"},
		})); err == nil {
			addLane("semantic_code")
			for _, item := range repoResultMaps(results["results"]) {
				pathValue := normalizeCodeSearchPath(stringValue(item["path"]))
				if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
					continue
				}
				why := firstNonEmpty(stringValue(item["summary"]), "semantic candidate")
				addCodeSearchCandidate(candidates, pathValue, why, "semantic_search_code", scoreValue(item["similarity"], 0.6), 0, stringValue(item["name"]), "")
			}
		} else {
			gaps = append(gaps, "semantic_search_code failed: "+err.Error())
		}
	}

	if taskType == codeSearchTaskRegistrationTrace {
		stageStart := time.Now()
		registrationHits, registrationGaps := codeSearchRegistrationAugment(a.workspaceRoot, candidates, input.Budget.MaxCandidates)
		a.telemetry.record("registration_trace", time.Since(stageStart), nil)
		gaps = append(gaps, registrationGaps...)
		if len(registrationHits) > 0 {
			addLane("registration_trace")
			for _, hit := range registrationHits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				addCodeSearchCandidate(candidates, hit.Path, "registration site for "+hit.Symbol, "registration_trace", 1.25, hit.Line, hit.Symbol, "")
			}
		}
	}

	routeFamily = inferCodeSearchRouteFamilyFromCandidates(taskType, input.Query, candidates)
	metadata["route_family"] = string(routeFamily)
	autoACA := taskType == codeSearchTaskFileLocate || taskType == codeSearchTaskChangeImpact
	metadata["aca_auto_enabled"] = autoACA
	if input.Constraints.IncludeACA || autoACA {
		stageStart := time.Now()
		var acaErr error
		acaHits, acaErr = a.searchACAGuidance(ctx, input.Query, routeFamily, minInt(input.Budget.MaxCandidates, 6))
		a.telemetry.record("aca_guidance", time.Since(stageStart), nil)
		if acaErr != nil {
			gaps = append(gaps, "aca guidance failed: "+acaErr.Error())
		} else if len(acaHits) > 0 {
			acaRouteFamily := inferCodeSearchRouteFamily(taskType, acaHits)
			if taskType == codeSearchTaskFileLocate && routeFamily != codeSearchRouteInfraResource {
				packageHits, packageErr := a.searchACAGuidance(ctx, input.Query, codeSearchRoutePackageOwner, minInt(input.Budget.MaxCandidates, 6))
				if packageErr != nil {
					gaps = append(gaps, "aca package guidance failed: "+packageErr.Error())
				} else if shouldUsePackageACAGuidance(routeFamily, input.Query, packageHits) {
					acaHits = packageHits
					acaRouteFamily = codeSearchRoutePackageOwner
				}
			}
			routeFamily = promoteCodeSearchRouteFamily(routeFamily, input.Query, taskType, acaHits, acaRouteFamily)
			metadata["route_family"] = string(routeFamily)
			metadata["aca_guidance"] = summarizeACAGuidanceHits(acaHits, 4)
		}
	}

	if len(acaHits) > 0 && len(candidates) > 0 {
		if applied := applyACAGuidanceSupport(input.Query, candidates, acaHits, routeFamily, taskType, excluded); applied > 0 {
			addLane("aca_guidance")
		}
	}

	if taskType == codeSearchTaskExecutionTrace {
		stageStart := time.Now()
		bridgeHits, bridgeAnchors, bridgeGaps := codeSearchExecutionTraceAugment(ctx, querySvc, candidates, input.Query, input.Budget.MaxCandidates)
		a.telemetry.record("execution_graph", time.Since(stageStart), nil)
		gaps = append(gaps, bridgeGaps...)
		if len(bridgeAnchors) > 0 {
			addLane("execution_graph")
			for _, hit := range bridgeHits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				addCodeSearchCandidate(candidates, hit.Path, "execution graph from "+hit.SourcePath, "execution_graph", 1.15, hit.Line, hit.Symbol, hit.NodeID)
				for _, edgeType := range hit.EdgeTypes {
					switch edgeType {
					case repoindex.EdgeImplements:
						addCodeSearchCandidate(candidates, hit.Path, "execution graph implements from "+hit.SourcePath, "execution_graph_implements", 0.95, hit.Line, hit.Symbol, hit.NodeID)
					case repoindex.EdgeUsesSymbol:
						addCodeSearchCandidate(candidates, hit.Path, "execution graph uses symbol from "+hit.SourcePath, "execution_graph_uses_symbol", 0.95, hit.Line, hit.Symbol, hit.NodeID)
					case repoindex.EdgeRefersTo:
						addCodeSearchCandidate(candidates, hit.Path, "execution graph refers-to from "+hit.SourcePath, "execution_graph_refers_to", 0.55, hit.Line, hit.Symbol, hit.NodeID)
					}
				}
			}
		}
	}

	if taskType == codeSearchTaskChangeImpact {
		stageStart := time.Now()
		impactHits, impactAnchors, impactGaps := codeSearchImpactAugment(ctx, querySvc, candidates, input.Budget.MaxCandidates)
		a.telemetry.record("impact_graph", time.Since(stageStart), nil)
		gaps = append(gaps, impactGaps...)
		if len(impactAnchors) > 0 {
			addLane("impact_graph")
			for _, hit := range impactHits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				addCodeSearchCandidate(candidates, hit.Path, "impact graph from "+hit.SourcePath, "impact_graph", 1.1, hit.Line, hit.Symbol, hit.NodeID)
			}
		}
	}
	if taskType == codeSearchTaskExecutionTrace {
		stageStart := time.Now()
		adjacentHits, adjacentGaps := codeSearchAdjacentImplementationAugment(a.workspaceRoot, candidates, input.Query, pathProbes, exactProbes, input.Budget.MaxCandidates)
		a.telemetry.record("adjacent_impl", time.Since(stageStart), nil)
		gaps = append(gaps, adjacentGaps...)
		if len(adjacentHits) > 0 {
			addLane("adjacent_impl")
			for _, hit := range adjacentHits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				addCodeSearchCandidate(candidates, hit.Path, "adjacent implementation near "+hit.SourcePath, "adjacent_impl", 1.05, hit.Line, "", "")
			}
		}
	}

	rankLimit := input.Budget.MaxCandidates
	if taskType == codeSearchTaskExecutionTrace {
		rankLimit = maxInt(input.Budget.MaxCandidates*3, 12)
	}
	replanOut := codeSearchLLMReplannerOutput{}
	replanEdgePrioritySet := map[string]struct{}{}
	replanMustCoverSet := map[string]struct{}{}
	preRanked := rankCodeSearchCandidatesWithProbes(candidates, input.Query, taskType, rankLimit, pathProbes, exactProbes)
	if taskType == codeSearchTaskExecutionTrace && input.Planner.Enabled && input.Planner.EnableReplan && shouldRunCodeSearchLLMReplanner(preRanked, input.Budget.MaxFiles, motifPathScores) {
		stageStart := time.Now()
		replanMeta, replanUsage, replanErr := map[string]any(nil), (*skillobs.TokenUsage)(nil), error(nil)
		replanOut, replanMeta, replanUsage, replanErr = a.codeSearchLLMReplanner(ctx, input.Query, taskType, preRanked, input.Budget.MaxFiles, motifPathScores, input.Planner, plannerOut)
		a.telemetry.record("llm_replanner", time.Since(stageStart), replanUsage)
		if replanErr != nil {
			gaps = append(gaps, "llm replanner failed: "+replanErr.Error())
		} else {
			metadata["llm_replanner"] = replanMeta
			replanEdgePrioritySet = stringSet(replanOut.RaiseEdgePriorities)
			replanMustCoverSet = stringSet(replanOut.MustCover)
			if !replanOut.StopBroadening {
				newExact := mergeCodeSearchProbeLists(nil, replanOut.AddSeedQueries)
				newPaths := mergeCodeSearchProbeLists(nil, replanOut.AddPathBiases)
				for _, probe := range newPaths {
					stageStart := time.Now()
					hits, err := codeSearchPathProbeSearch(a.workspaceRoot, probe, input.Budget.MaxCandidates)
					a.telemetry.record("replan_path_probe", time.Since(stageStart), nil)
					if err != nil {
						gaps = append(gaps, "replan path probe failed for "+probe+": "+err.Error())
						continue
					}
					for _, hit := range hits {
						if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
							continue
						}
						addCodeSearchCandidate(candidates, hit.Path, "replan path probe: "+probe, "replan_path_probe", 0.92, 0, "", "")
					}
				}
				for _, probe := range newExact {
					stageStart := time.Now()
					hits, err := exactCodeProbeSearch(a.workspaceRoot, probe, input.Budget.MaxCandidates)
					a.telemetry.record("replan_exact_probe", time.Since(stageStart), nil)
					if err != nil {
						gaps = append(gaps, "replan exact probe failed for "+probe+": "+err.Error())
						continue
					}
					for _, hit := range hits {
						if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
							continue
						}
						addCodeSearchCandidate(candidates, hit.Path, "replan exact probe: "+probe, "replan_exact_probe", codeSearchFileWeight(hit.Path), hit.Line, "", "")
					}
				}
				for _, probe := range newExact {
					stageStart := time.Now()
					searchOut, err := querySvc.SearchWithProjection(ctx, repoquery.SearchRequest{Query: probe, Limit: input.Budget.MaxCandidates})
					a.telemetry.record("replan_repo_index", time.Since(stageStart), nil)
					if err != nil {
						gaps = append(gaps, "replan repo search failed for "+probe+": "+err.Error())
						continue
					}
					for i, anchor := range searchOut.Anchors {
						pathValue := normalizeCodeSearchPath(anchor.Path)
						if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
							continue
						}
						nodeID := ""
						if i < len(searchOut.Nodes) {
							nodeID = strings.TrimSpace(searchOut.Nodes[i].ID)
						}
						addCodeSearchCandidate(candidates, pathValue, "replan repo-index anchor", "replan_repo_search", anchor.Score, anchor.LineHint, anchor.SymbolName, nodeID)
					}
				}
			}
		}
	}
	if taskType == codeSearchTaskExecutionTrace && input.Selector.Enabled {
		preRanked := rankCodeSearchCandidatesWithPlan(candidates, input.Query, taskType, rankLimit, pathProbes, exactProbes, replanEdgePrioritySet, replanMustCoverSet, replanOut.AmbiguityFamily)
		if shouldRunCodeSearchLLMSelector(preRanked, input.Budget.MaxFiles) {
			stageStart := time.Now()
			selectorOut, selectorMeta, selectorUsage, selectorErr := a.codeSearchLLMSelector(ctx, input.Query, taskType, preRanked, input.Selector, input.Budget.MaxFiles)
			a.telemetry.record("llm_selector", time.Since(stageStart), selectorUsage)
			if selectorErr != nil {
				gaps = append(gaps, "llm selector failed: "+selectorErr.Error())
			} else {
				metadata["llm_selector"] = selectorMeta
				for _, keepPath := range selectorOut.KeepPaths {
					keepPath = normalizeCodeSearchPath(keepPath)
					if keepPath == "" {
						continue
					}
					if candidate := candidates[keepPath]; candidate != nil {
						if candidate.Sources == nil {
							candidate.Sources = map[string]struct{}{}
						}
						candidate.Sources["llm_selector_keep"] = struct{}{}
					}
				}
			}
		}
	}
	ranked := rankCodeSearchCandidatesWithPlan(candidates, input.Query, taskType, rankLimit, pathProbes, exactProbes, replanEdgePrioritySet, replanMustCoverSet, replanOut.AmbiguityFamily)
	if len(ranked) == 0 {
		return map[string]any{
			"summary":            "",
			"task_type":          taskType,
			"answer_basis":       "none",
			"confidence":         0.0,
			"files":              []codeSearchEvidenceFile{},
			"symbols":            []codeSearchEvidenceSymbol{},
			"snippets":           []codeSearchEvidenceSnippet{},
			"call_paths":         []map[string]any{},
			"supporting_context": []any{},
			"gaps":               append(gaps, "no candidate files found"),
			"metadata":           metadata,
		}, nil
	}

	files := make([]codeSearchEvidenceFile, 0, input.Budget.MaxFiles)
	symbols := make([]codeSearchEvidenceSymbol, 0, input.Budget.MaxFiles)
	snippets := make([]codeSearchEvidenceSnippet, 0, input.Budget.MaxSnippets)
	callPaths := make([]map[string]any, 0, input.Budget.MaxFiles*2)
	groundedCount := 0
	loadedChars := 0
	selectedPaths := map[string]int{}
	groundingQueue := append([]*codeSearchCandidate(nil), prioritizedGroundingCandidates(ranked, input.Query, taskType)...)
	rankedByPath := make(map[string]*codeSearchCandidate, len(ranked))
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		rankedByPath[candidate.Path] = candidate
	}
	if taskType == codeSearchTaskExecutionTrace && len(groundingQueue) > 0 {
		groundingQueue = applyCodeSearchResolverHintsToQueue(groundingQueue, replanOut.PreferPaths, replanOut.DemotePaths, codeSearchSameRoleAmbiguities(ranked, input.Budget.MaxFiles, motifPathScores), input.Budget.MaxFiles)
	}

	for idx := 0; idx < len(groundingQueue); idx++ {
		if len(files) >= input.Budget.MaxFiles {
			break
		}
		candidate := groundingQueue[idx]
		filePath := candidate.Path
		if filePath == "" || isExcludedCodeSearchPath(filePath, excluded) {
			continue
		}

		lineHint := firstPositive(candidate.LineHints...)
		if taskType == codeSearchTaskSymbolInspect {
			lineHint = lastPositive(candidate.LineHints...)
		}
		startLine, endLine := codeSearchSliceBounds(lineHint, taskType)
		stageStart := time.Now()
		loaded, loadErr := a.loadFile(mustJSON(map[string]any{
			"path":       filePath,
			"start_line": startLine,
			"end_line":   endLine,
		}))
		a.telemetry.record("load_file", time.Since(stageStart), nil)
		if loadErr != nil {
			gaps = append(gaps, fmt.Sprintf("load_file failed for %s: %v", filePath, loadErr))
			continue
		}

		groundedCount++
		addLane("load_file")
		confirmedBy := sortedSourceKeys(candidate.Sources)
		confirmedBy = appendIfMissing(confirmedBy, "load_file")
		files = append(files, codeSearchEvidenceFile{
			Path:         filePath,
			Why:          candidate.Why,
			SupportScore: clampScore(candidate.Support),
			ConfirmedBy:  confirmedBy,
		})
		if _, ok := selectedPaths[filePath]; !ok {
			selectedPaths[filePath] = len(files)
		}

		content := strings.TrimSpace(stringValue(loaded["content"]))
		extractedSymbols := extractGroundedEvidenceSymbols(
			filePath,
			content,
			intValue(loaded["start_line"]),
			lineHint,
			preferredSymbolProbes,
			candidate.Symbols,
			taskType,
		)
		for _, symbol := range extractedSymbols {
			symbol.Why = candidate.Why
			symbols = append(symbols, symbol)
		}
		if len(symbols) > input.Budget.MaxFiles {
			symbols = symbols[:input.Budget.MaxFiles]
		}

		if len(snippets) < input.Budget.MaxSnippets {
			if content != "" {
				loadedChars += len(content)
				snippetStart := startLine
				snippetEnd := endLine
				if taskType == codeSearchTaskSymbolInspect {
					for _, symbol := range extractedSymbols {
						if symbol.Line <= 0 {
							continue
						}
						snippetStart, snippetEnd = codeSearchSliceBounds(symbol.Line, taskType)
						break
					}
				}
				snippets = append(snippets, codeSearchEvidenceSnippet{
					Path:      filePath,
					StartLine: snippetStart,
					EndLine:   snippetEnd,
					Reason:    candidate.Why,
				})
			}
		}

		if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskChangeImpact {
			neighborPaths := make([]string, 0, 8)
			if taskType == codeSearchTaskExecutionTrace {
				protocolImplPaths := codeSearchProtocolImplementationPaths(a.workspaceRoot, filePath, content, 2)
				for _, pathValue := range protocolImplPaths {
					groundingQueue = moveCandidateEarlier(groundingQueue, idx+1, pathValue)
				}
			}
			for _, seed := range uniqueStrings(candidate.RepoNodeIDs) {
				if strings.TrimSpace(seed) == "" {
					continue
				}
				directions := []repoindex.Direction{repoindex.DirOut}
				if taskType == codeSearchTaskChangeImpact {
					directions = []repoindex.Direction{repoindex.DirOut, repoindex.DirIn}
				}
				for _, direction := range directions {
					stageStart := time.Now()
					expanded, expandErr := querySvc.ExpandWithProjection(ctx, repoquery.ExpandRequest{
						Seeds:      []string{seed},
						EdgeTypes:  repoindex.EdgeSetStructural,
						Direction:  direction,
						Depth:      2,
						Budget:     input.Budget.MaxCandidates * 3,
						PerNodeCap: 20,
					})
					a.telemetry.record("repo_graph", time.Since(stageStart), nil)
					if expandErr != nil {
						gaps = append(gaps, fmt.Sprintf("expand_repo_graph failed for %s: %v", seed, expandErr))
						continue
					}
					addLane("repo_graph")
					for _, anchor := range expanded.Anchors {
						pathValue := normalizeCodeSearchPath(anchor.Path)
						if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
							continue
						}
						neighborPaths = append(neighborPaths, pathValue)
						callPaths = append(callPaths, map[string]any{
							"path":        pathValue,
							"symbol_name": strings.TrimSpace(anchor.SymbolName),
							"line_hint":   anchor.LineHint,
							"source":      "expand_repo_graph",
							"direction":   string(direction),
						})
					}
				}
				if len(callPaths) > input.Budget.MaxFiles*3 {
					callPaths = callPaths[:input.Budget.MaxFiles*3]
				}
				break
			}
			groundingQueue = promoteGroundedRepoGraphNeighbors(groundingQueue, idx+1, rankedByPath, selectedPaths, neighborPaths, input.Query, taskType)
		}
	}

	metadata["grounded"] = groundedCount > 0
	if requireGrounding && groundedCount == 0 {
		gaps = append(gaps, "no grounded files were loaded")
	}

	if len(files) == 0 && len(ranked) > 0 {
		for _, candidate := range ranked[:minInt(len(ranked), input.Budget.MaxFiles)] {
			files = append(files, codeSearchEvidenceFile{
				Path:         candidate.Path,
				Why:          candidate.Why,
				SupportScore: clampScore(candidate.Support * 0.7),
				ConfirmedBy:  sortedSourceKeys(candidate.Sources),
			})
			if _, ok := selectedPaths[candidate.Path]; !ok {
				selectedPaths[candidate.Path] = len(files)
			}
		}
	}

	metadata["candidate_trace"] = buildCodeSearchCandidateTrace(ranked, selectedPaths, input.Budget.MaxFiles, taskType)
	if taskType == codeSearchTaskFileLocate {
		metadata["file_locate_evidence_buckets"] = buildFileLocateEvidenceBuckets(files, rankedByPath)
	}

	answerBasis := buildCodeSearchAnswerBasis(metadata["lanes_used"].([]string))
	confidence := codeSearchConfidence(taskType, len(files), groundedCount, len(callPaths), requireGrounding)
	telemetry := a.telemetry.snapshot()
	evidencePayload := map[string]any{
		"summary":            buildCodeSearchSummary(taskType, files, callPaths, gaps),
		"task_type":          taskType,
		"answer_basis":       answerBasis,
		"confidence":         confidence,
		"files":              files,
		"symbols":            dedupeCodeSearchSymbols(symbols),
		"snippets":           snippets,
		"call_paths":         callPaths,
		"supporting_context": []any{},
		"gaps":               uniqueStrings(gaps),
	}
	if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskRegistrationTrace {
		buckets := buildExecutionTraceBuckets(ranked, selectedPaths)
		evidencePayload["direct_dispatch_files"] = buckets["direct_dispatch_files"]
		evidencePayload["exposure_files"] = buckets["exposure_files"]
		evidencePayload["structural_support_files"] = buckets["structural_support_files"]
		evidencePayload["registration_files"] = buckets["registration_files"]
	}
	emittedBytes, _ := json.Marshal(evidencePayload)
	loadedTokenEstimate := loadedChars / memtokens.CharsPerToken
	emittedTokenEstimate := len(emittedBytes) / memtokens.CharsPerToken
	telemetry["loaded_chars"] = loadedChars
	telemetry["loaded_token_estimate"] = loadedTokenEstimate
	telemetry["emitted_chars"] = len(emittedBytes)
	telemetry["emitted_token_estimate"] = emittedTokenEstimate
	telemetry["parent_input_token_savings_estimate"] = maxInt(0, loadedTokenEstimate-emittedTokenEstimate)
	if loadedTokenEstimate > 0 {
		telemetry["compaction_ratio"] = float64(emittedTokenEstimate) / float64(loadedTokenEstimate)
	}
	metadata["telemetry"] = telemetry
	_ = observability.EmitSync(ctx, observability.NewEvent("context.code_search_ensemble").
		WithComponent(observability.ComponentContextBuilder).
		WithCommand("code_search_ensemble").
		WithWorkspace(a.workspaceRoot).
		WithData("query_hash", observability.HashQuestion(input.Query)).
		WithData("task_type", taskType).
		WithData("answer_basis", answerBasis).
		WithData("grounded", groundedCount > 0).
		WithData("files_count", len(files)).
		WithData("snippets_count", len(snippets)).
		WithData("call_paths_count", len(callPaths)).
		WithDataMap(telemetry).
		Success(time.Since(started)))
	evidencePayload["metadata"] = metadata
	return evidencePayload, nil
}

func (a *ReadOnlyAdapter) searchRepoMotifs(ctx context.Context, query string, limit int) ([]contextplane.RepoMotifSearchHit, error) {
	if strings.TrimSpace(query) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	memStore, err := memorystore.OpenWithConfig(ctx, a.cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = memStore.Close() }()
	return contextplane.SearchRepoMotifArtifacts(ctx, a.workspaceRoot, query, limit, memStore, nil)
}

func (a *ReadOnlyAdapter) searchACAGuidance(ctx context.Context, query string, routeFamily codeSearchRouteFamily, limit int) ([]contextplane.RetrievalHit, error) {
	if strings.TrimSpace(query) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	vaultPath := firstNonEmpty(strings.TrimSpace(a.vaultPath),
		strings.TrimSpace(os.Getenv("AGENTCTL_RLM_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("AGENTCTL_ACA_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_VAULT_PATH")),
	)
	if vaultPath == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 6
	}
	index, err := obsidianindex.Open(ctx, a.cfg.Storage.Root, vaultPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = index.Close() }()
	if routeFamily == codeSearchRoutePackageOwner {
		return searchPackageACAGuidance(index, a.workspaceRoot, query, limit)
	}
	repoStore, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = repoStore.Close() }()
	memStore, err := memorystore.OpenWithConfig(ctx, a.cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = memStore.Close() }()
	workspaceStore := contextplane.NewWorkspaceStore(a.workspaceRoot)
	opts := workspaceStore.CurrentRetrievalOptions()
	opts.IncludeTopOfMindResult = false
	opts.IncludeLatestHandoff = false
	opts.IncludeVaultHits = true
	opts.UseRelevantRefBoost = false
	opts.UseHandoffRefBoost = false
	opts.IncludeControlPlaneRefs = false
	opts.UseSemanticVaultSearch = false
	if routeFamily == codeSearchRoutePackageOwner {
		opts.UseRepoMotifPrior = false
		opts.UseCodeHints = false
		opts.UseCoChangePrior = false
	}
	opts.AllowedTrusts = []string{"canonical"}
	result, err := workspaceStore.RetrieveWithOptionsAndMemory(ctx, index, repoStore, nil, memStore, query, limit, opts)
	if err != nil {
		return nil, err
	}
	return result.VaultHits, nil
}

func searchPackageACAGuidance(index obsidianindex.Store, workspaceRoot, query string, limit int) ([]contextplane.RetrievalHit, error) {
	if index == nil || strings.TrimSpace(query) == "" || strings.TrimSpace(workspaceRoot) == "" {
		return nil, nil
	}
	searchLimit := maxInt(limit*12, 24)
	hits, err := index.SearchNotes(context.Background(), strings.TrimSpace(query), searchLimit)
	if err != nil {
		return nil, err
	}
	return packageACAGuidanceHitsFromSearchHits(workspaceRoot, query, hits, limit), nil
}

func packageACAGuidanceHitsFromSearchHits(workspaceRoot, query string, hits []obsidianindex.SearchHit, limit int) []contextplane.RetrievalHit {
	if len(hits) == 0 || strings.TrimSpace(workspaceRoot) == "" {
		return nil
	}
	repoName := strings.ToLower(strings.TrimSpace(filepath.Base(workspaceRoot)))
	if repoName == "" {
		return nil
	}
	prefix := "notes/repo/" + repoName + "/packages/"
	type scoredHit struct {
		hit   obsidianindex.SearchHit
		score int
	}
	scored := make([]scoredHit, 0, len(hits))
	queryTerms := codeSearchPathTerms(query)
	for _, hit := range hits {
		pathValue := strings.ToLower(strings.TrimSpace(hit.Path))
		if !strings.HasPrefix(pathValue, prefix) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(hit.Trust), "canonical") {
			continue
		}
		score := maxInt(0, hit.Score)
		score += int(acaGuidanceTitleAffinity(hit.Title, query) * 120)
		anchorPaths := preferredPackageAnchorPaths(contextplane.RetrievalHit{
			PrimaryAnchorPath: hit.PrimaryAnchorPath,
			AnchorPaths:       hit.AnchorPaths,
			AnchorRoles:       cloneAnchorRoles(hit.AnchorRoles),
		})
		if len(anchorPaths) == 0 {
			anchorPaths = normalizeCodeSearchPaths(hit.AnchorPaths)
			if primary := normalizeCodeSearchPath(hit.PrimaryAnchorPath); primary != "" {
				anchorPaths = append([]string{primary}, anchorPaths...)
			}
		}
		score += int(anchorPathAffinityScore(anchorPaths, queryTerms) * 120)
		if primary := normalizeCodeSearchPath(hit.PrimaryAnchorPath); primary != "" {
			score += int(packageAnchorBasenameQueryAffinity(primary, queryTerms) * 60)
		}
		if repoPathCount := len(normalizeCodeSearchPaths(hit.RepoPaths)); repoPathCount > 0 {
			score += maxInt(0, 10-repoPathCount)
		}
		scored = append(scored, scoredHit{hit: hit, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return strings.TrimSpace(scored[i].hit.Path) < strings.TrimSpace(scored[j].hit.Path)
	})
	if limit <= 0 {
		limit = 1
	}
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]contextplane.RetrievalHit, 0, len(scored))
	for _, item := range scored {
		out = append(out, contextplane.RetrievalHit{
			Path:              strings.TrimSpace(item.hit.Path),
			Title:             strings.TrimSpace(item.hit.Title),
			Type:              strings.TrimSpace(item.hit.Type),
			Trust:             strings.TrimSpace(item.hit.Trust),
			Score:             item.score,
			Snippet:           strings.TrimSpace(item.hit.Snippet),
			PrimaryAnchorPath: strings.TrimSpace(item.hit.PrimaryAnchorPath),
			RepoPaths:         append([]string(nil), item.hit.RepoPaths...),
			AnchorPaths:       append([]string(nil), item.hit.AnchorPaths...),
			AnchorRoles:       cloneAnchorRoles(item.hit.AnchorRoles),
			Symbols:           append([]string(nil), item.hit.Symbols...),
		})
	}
	return out
}

func shouldUsePackageACAGuidance(routeFamily codeSearchRouteFamily, query string, hits []contextplane.RetrievalHit) bool {
	if len(hits) == 0 {
		return false
	}
	if routeFamily == codeSearchRoutePackageOwner {
		return true
	}
	return packageACAGuidanceSpecificityScore(query, hits[0]) >= 1.25
}

func promoteCodeSearchRouteFamily(current codeSearchRouteFamily, query, taskType string, hits []contextplane.RetrievalHit, inferred codeSearchRouteFamily) codeSearchRouteFamily {
	if current != codeSearchRouteCode || inferred == codeSearchRouteCode {
		return current
	}
	if inferred == codeSearchRoutePackageOwner && !shouldUsePackageACAGuidance(current, query, hits) {
		return current
	}
	return inferred
}

func applyACAGuidanceCandidates(workspaceRoot, query string, candidates map[string]*codeSearchCandidate, hits []contextplane.RetrievalHit, taskType string, excluded []string) int {
	if len(candidates) == 0 && len(hits) == 0 {
		return 0
	}
	applied := 0
	for _, hit := range hits {
		if acaGuidanceMatchCount(hit, taskType, query) < 2 {
			continue
		}
		repoPaths := limitedACARepoPaths(workspaceRoot, hit, 6)
		if len(repoPaths) == 0 {
			continue
		}
		support := codeSearchACAGuidanceSupport(taskType, hit)
		why := "aca guidance from " + strings.TrimSpace(hit.Path)
		for _, repoPath := range repoPaths {
			repoPath = normalizeCodeSearchPath(repoPath)
			if repoPath == "" || isExcludedCodeSearchPath(repoPath, excluded) {
				continue
			}
			symbolName := firstCodeLikeACASymbol(hit.Symbols)
			addCodeSearchCandidate(candidates, repoPath, why, "aca_guidance", support, 0, symbolName, "")
			applied++
		}
	}
	return applied
}

func applyACAGuidanceSupport(query string, candidates map[string]*codeSearchCandidate, hits []contextplane.RetrievalHit, routeFamily codeSearchRouteFamily, taskType string, excluded []string) int {
	if len(candidates) == 0 || len(hits) == 0 {
		return 0
	}
	exactSource, exactSupport, symbolSource, symbolSupport := acaGuidanceSupportWeights(routeFamily, taskType)
	if exactSupport <= 0 && symbolSupport <= 0 {
		return 0
	}
	hits = topACAGuidanceSupportHits(query, hits, routeFamily, taskType)
	if len(hits) == 0 {
		return 0
	}
	applied := 0
	minMatches := acaGuidanceMinimumMatchCount(routeFamily, taskType)
	queryTerms := codeSearchPathTerms(query)
	exactProbes := codeSearchExactProbes(query)
	pathProbes := codeSearchPathProbes(query)
	for _, hit := range hits {
		if acaGuidanceMatchCount(hit, taskType, query) < minMatches {
			continue
		}
		repoPaths := normalizeCodeSearchPaths(hit.RepoPaths)
		if routeFamily == codeSearchRoutePackageOwner {
			packageAnchors := preferredPackageAnchorPaths(hit)
			if len(packageAnchors) == 0 {
				packageAnchors = selectACAPackageAnchorPaths(repoPaths, candidates, hit.Symbols, queryTerms, pathProbes, exactProbes, 2)
			}
			primaryAnchor := normalizeCodeSearchPath(hit.PrimaryAnchorPath)
			for idx, repoPath := range packageAnchors {
				if isExcludedCodeSearchPath(repoPath, excluded) {
					continue
				}
				support := 1.1
				if idx == 0 {
					support = 1.3
				}
				if primaryAnchor != "" && repoPath == primaryAnchor {
					support = 1.35
				}
				symbolName := bestACASymbolForPath(repoPath, hit.Symbols)
				anchorRoleSource := "aca_route_package_secondary_anchor"
				if idx == 0 || (primaryAnchor != "" && repoPath == primaryAnchor) {
					anchorRoleSource = "aca_route_package_primary_anchor"
				}
				if candidates[repoPath] == nil {
					if !shouldIntroduceACAPackageAnchorCandidate(repoPath, primaryAnchor, candidates) {
						continue
					}
					addCodeSearchCandidate(candidates, repoPath, "aca package anchor from "+strings.TrimSpace(hit.Path), "aca_route_package_anchor", support, 0, symbolName, "")
					addCodeSearchCandidate(candidates, repoPath, "aca package anchor role from "+strings.TrimSpace(hit.Path), anchorRoleSource, 0, 0, "", "")
					applied++
					continue
				}
				candidate := candidates[repoPath]
				if candidate.Sources == nil {
					candidate.Sources = map[string]struct{}{}
				}
				if _, ok := candidate.Sources["aca_route_package_anchor"]; !ok {
					candidate.Sources["aca_route_package_anchor"] = struct{}{}
					applied++
				}
				candidate.Sources[anchorRoleSource] = struct{}{}
				candidate.Support += support
				if symbolName != "" {
					candidate.Symbols = append(candidate.Symbols, symbolName)
				}
			}
		}
		exactPaths := repoPaths
		primaryExactPath := ""
		if routeFamily == codeSearchRouteInfraResource {
			if resourcePaths := preferredInfraAnchorPaths(hit); len(resourcePaths) > 0 {
				exactPaths = resourcePaths
				primaryExactPath = resourcePaths[0]
			}
		}
		for _, repoPath := range exactPaths {
			if isExcludedCodeSearchPath(repoPath, excluded) {
				continue
			}
			candidate := candidates[repoPath]
			if candidate == nil {
				continue
			}
			if candidate.Sources == nil {
				candidate.Sources = map[string]struct{}{}
			}
			if _, ok := candidate.Sources[exactSource]; !ok {
				candidate.Sources[exactSource] = struct{}{}
				applied++
			}
			if routeFamily == codeSearchRouteInfraResource {
				if primaryExactPath != "" && repoPath == primaryExactPath {
					candidate.Sources["aca_route_infra_primary_anchor"] = struct{}{}
				} else if primaryExactPath != "" {
					candidate.Sources["aca_route_infra_secondary_anchor"] = struct{}{}
				}
			}
			candidate.Support += exactSupport
			if symbolName := bestACASymbolForPath(repoPath, hit.Symbols); symbolName != "" {
				candidate.Symbols = append(candidate.Symbols, symbolName)
			}
		}
		if symbolSupport <= 0 || len(hit.Symbols) == 0 {
			continue
		}
		for _, candidate := range candidates {
			if candidate == nil || candidate.Path == "" || isExcludedCodeSearchPath(candidate.Path, excluded) {
				continue
			}
			if !acaGuidanceSymbolOverlap(candidate, hit.Symbols) {
				continue
			}
			if candidate.Sources == nil {
				candidate.Sources = map[string]struct{}{}
			}
			if _, ok := candidate.Sources[symbolSource]; !ok {
				candidate.Sources[symbolSource] = struct{}{}
				applied++
			}
			candidate.Support += symbolSupport
		}
	}
	return applied
}

func acaGuidanceMinimumMatchCount(routeFamily codeSearchRouteFamily, taskType string) int {
	if routeFamily == codeSearchRoutePackageOwner {
		return 1
	}
	if taskType == codeSearchTaskExecutionTrace {
		return 2
	}
	return 2
}

func topACAGuidanceSupportHits(query string, hits []contextplane.RetrievalHit, routeFamily codeSearchRouteFamily, taskType string) []contextplane.RetrievalHit {
	if len(hits) == 0 {
		return nil
	}
	limit := 1
	switch routeFamily {
	case codeSearchRouteInfraResource, codeSearchRouteCochangeHistory:
		limit = 2
	}
	type scoredHit struct {
		hit   contextplane.RetrievalHit
		score int
	}
	scored := make([]scoredHit, 0, len(hits))
	for _, hit := range hits {
		score := acaGuidanceSupportHitScore(query, hit, routeFamily, taskType)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredHit{hit: hit, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return strings.TrimSpace(scored[i].hit.Path) < strings.TrimSpace(scored[j].hit.Path)
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]contextplane.RetrievalHit, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.hit)
	}
	return out
}

func acaGuidanceSupportHitScore(query string, hit contextplane.RetrievalHit, routeFamily codeSearchRouteFamily, taskType string) int {
	if routeFamily == codeSearchRoutePackageOwner {
		queryTerms := codeSearchPathTerms(query)
		score := maxInt(0, hit.Score)
		if strings.Contains(strings.ToLower(strings.TrimSpace(hit.Path)), "/packages/") {
			score += 30
		}
		score += int(acaGuidanceTitleAffinity(hit.Title, query) * 200)
		score += int(anchorPathAffinityScore(preferredPackageAnchorPaths(hit), queryTerms) * 160)
		if primary := normalizeCodeSearchPath(hit.PrimaryAnchorPath); primary != "" {
			score += int(packageAnchorBasenameQueryAffinity(primary, queryTerms) * 80)
		}
		return score
	}
	score := acaGuidanceMatchCount(hit, taskType, query) * 100
	if score == 0 {
		return 0
	}
	repoPathCount := len(normalizeCodeSearchPaths(hit.RepoPaths))
	switch routeFamily {
	case codeSearchRouteInfraResource:
		queryTerms := codeSearchPathTerms(query)
		if strings.Contains(strings.ToLower(strings.TrimSpace(hit.Path)), "/concepts/") {
			score += 40
		}
		if resourcePaths := preferredInfraAnchorPaths(hit); len(resourcePaths) > 0 {
			score += int(anchorPathAffinityScore(resourcePaths, queryTerms) * 180)
			repoPathCount = len(resourcePaths)
		}
		if repoPathCount > 0 {
			score += maxInt(0, 12-repoPathCount)
		}
	case codeSearchRouteCochangeHistory:
		pathValue := strings.ToLower(strings.TrimSpace(hit.Path))
		if strings.HasPrefix(pathValue, "notes/repo/") && !strings.Contains(pathValue, "/packages/") && !strings.Contains(pathValue, "/concepts/") {
			score += 30
		}
	default:
		if repoPathCount == 1 {
			score += 10
		}
	}
	score += maxInt(0, hit.Score)
	return score
}

func acaGuidanceTitleAffinity(title, query string) float64 {
	titleTerms := splitCodeSearchProbe(title)
	queryTerms := codeSearchPathTerms(query)
	if len(titleTerms) == 0 || len(queryTerms) == 0 {
		return 0
	}
	titleSet := map[string]struct{}{}
	for _, term := range titleTerms {
		if term == "" {
			continue
		}
		titleSet[term] = struct{}{}
	}
	score := 0.0
	for _, term := range queryTerms {
		if term == "" {
			continue
		}
		if _, ok := titleSet[term]; ok {
			score += 1.0
			continue
		}
		for titleTerm := range titleSet {
			switch {
			case strings.Contains(titleTerm, term), strings.Contains(term, titleTerm):
				score += 0.6
				goto nextTerm
			}
		}
	nextTerm:
	}
	if score == 0 {
		return 0
	}
	return clampScore(score / float64(len(queryTerms)))
}

func acaGuidanceSupportWeights(routeFamily codeSearchRouteFamily, taskType string) (exactSource string, exactSupport float64, symbolSource string, symbolSupport float64) {
	switch routeFamily {
	case codeSearchRouteInfraResource:
		return "aca_route_infra_exact", 1.35, "aca_route_infra_symbol", 0.35
	case codeSearchRoutePackageOwner:
		return "aca_route_package_exact", 0.95, "aca_route_package_symbol", 0.45
	case codeSearchRouteCochangeHistory:
		return "aca_route_cochange_exact", 0.85, "aca_route_cochange_symbol", 0.2
	default:
		if taskType == codeSearchTaskExecutionTrace {
			return "aca_route_code_exact", 0.0, "aca_route_code_symbol", 0.0
		}
		return "aca_route_code_exact", 0.55, "aca_route_code_symbol", 0.2
	}
}

func acaGuidanceSymbolOverlap(candidate *codeSearchCandidate, acaSymbols []string) bool {
	if candidate == nil || len(candidate.Symbols) == 0 || len(acaSymbols) == 0 {
		return false
	}
	for _, candidateSymbol := range candidate.Symbols {
		candidateSymbol = strings.TrimSpace(strings.ToLower(candidateSymbol))
		if candidateSymbol == "" {
			continue
		}
		for _, acaSymbol := range acaSymbols {
			acaSymbol = strings.TrimSpace(strings.ToLower(acaSymbol))
			if acaSymbol == "" {
				continue
			}
			switch {
			case candidateSymbol == acaSymbol:
				return true
			case strings.HasSuffix(candidateSymbol, "."+acaSymbol):
				return true
			case strings.HasSuffix(acaSymbol, "."+candidateSymbol):
				return true
			}
		}
	}
	return false
}

func limitedACARepoPaths(workspaceRoot string, hit contextplane.RetrievalHit, limit int) []string {
	paths := normalizeCodeSearchPaths(hit.RepoPaths)
	if strings.TrimSpace(workspaceRoot) != "" {
		filtered := make([]string, 0, len(paths))
		for _, repoPath := range paths {
			if acaRepoPathExists(workspaceRoot, repoPath) {
				filtered = append(filtered, repoPath)
			}
		}
		paths = filtered
	}
	if limit > 0 && len(paths) > limit {
		return paths[:limit]
	}
	return paths
}

func shouldIntroduceACAPackageAnchorCandidate(repoPath, primaryAnchor string, candidates map[string]*codeSearchCandidate) bool {
	repoPath = normalizeCodeSearchPath(repoPath)
	primaryAnchor = normalizeCodeSearchPath(primaryAnchor)
	if repoPath == "" {
		return false
	}
	if primaryAnchor != "" && repoPath == primaryAnchor {
		return true
	}
	repoDir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(repoPath)))
	if repoDir == "" || repoDir == "." {
		return false
	}
	for _, candidate := range candidates {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		candidatePath := normalizeCodeSearchPath(candidate.Path)
		if candidatePath == "" {
			continue
		}
		if strings.TrimSpace(filepath.ToSlash(filepath.Dir(candidatePath))) == repoDir {
			return true
		}
		if fileLocateModuleFamilyKey(candidate) == fileLocateModuleFamilyKey(&codeSearchCandidate{Path: repoPath}) {
			return true
		}
	}
	return false
}

func firstCodeLikeACASymbol(symbols []string) string {
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if isLikelyGroundedSymbolName(symbol) {
			return symbol
		}
	}
	return ""
}

func bestACASymbolForPath(repoPath string, symbols []string) string {
	repoPath = normalizeCodeSearchPath(repoPath)
	if repoPath == "" || len(symbols) == 0 {
		return firstCodeLikeACASymbol(symbols)
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(repoPath), filepath.Ext(repoPath)))
	best := ""
	bestScore := 0.0
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if !isLikelyGroundedSymbolName(symbol) {
			continue
		}
		parts := splitCodeSearchModuleParts(symbol)
		if len(parts) == 0 {
			continue
		}
		last := toSnakeCase(parts[len(parts)-1])
		score := 0.0
		switch {
		case last == base:
			score = 2.0
		case strings.Contains(base, last) || strings.Contains(last, base):
			score = 1.0
		default:
			continue
		}
		if score > bestScore {
			best = symbol
			bestScore = score
		}
	}
	if best != "" {
		return best
	}
	return firstCodeLikeACASymbol(symbols)
}

func codeSearchACAGuidanceSupport(taskType string, hit contextplane.RetrievalHit) float64 {
	base := 0.65
	switch taskType {
	case codeSearchTaskFileLocate, codeSearchTaskSymbolInspect:
		base = 0.82
	case codeSearchTaskChangeImpact, codeSearchTaskRegistrationTrace:
		base = 0.72
	case codeSearchTaskExecutionTrace:
		base = 0.62
	}
	lowerPath := strings.ToLower(strings.TrimSpace(hit.Path))
	if strings.Contains(lowerPath, "/concepts/") {
		base += 0.12
	}
	if len(hit.RepoPaths) == 1 {
		base += 0.06
	}
	if base > 0.98 {
		return 0.98
	}
	return base
}

func acaGuidanceMatchCount(hit contextplane.RetrievalHit, taskType, query string) int {
	queryTerms := codeSearchPathTerms(query)
	if len(queryTerms) == 0 {
		return 0
	}
	surface := strings.ToLower(strings.TrimSpace(hit.Title + " " + strings.Join(hit.RepoPaths, " ") + " " + strings.Join(hit.Symbols, " ")))
	matches := 0
	for _, term := range queryTerms {
		if len(term) < 3 {
			continue
		}
		if strings.Contains(surface, strings.ToLower(strings.TrimSpace(term))) {
			matches++
		}
	}
	if taskType == codeSearchTaskExecutionTrace && strings.Contains(strings.ToLower(strings.TrimSpace(hit.Path)), "/concepts/") {
		matches++
	}
	return matches
}

func summarizeACAGuidanceHits(hits []contextplane.RetrievalHit, limit int) []map[string]any {
	if len(hits) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(hits) {
		limit = len(hits)
	}
	out := make([]map[string]any, 0, limit)
	for _, hit := range hits[:limit] {
		repoPaths := normalizeCodeSearchPaths(hit.RepoPaths)
		if len(repoPaths) > 4 {
			repoPaths = repoPaths[:4]
		}
		out = append(out, map[string]any{
			"path":       strings.TrimSpace(hit.Path),
			"title":      strings.TrimSpace(hit.Title),
			"type":       strings.TrimSpace(hit.Type),
			"trust":      strings.TrimSpace(hit.Trust),
			"score":      hit.Score,
			"repo_paths": repoPaths,
		})
	}
	return out
}

func acaRepoPathExists(workspaceRoot, repoPath string) bool {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	repoPath = normalizeCodeSearchPath(repoPath)
	if workspaceRoot == "" || repoPath == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(workspaceRoot, filepath.FromSlash(repoPath)))
	return err == nil && !info.IsDir()
}

func inferCodeSearchRouteFamily(taskType string, hits []contextplane.RetrievalHit) codeSearchRouteFamily {
	if taskType == codeSearchTaskChangeImpact {
		for _, hit := range hits {
			pathValue := strings.ToLower(strings.TrimSpace(hit.Path))
			if strings.HasPrefix(pathValue, "notes/repo/") && !strings.Contains(pathValue, "/packages/") && !strings.Contains(pathValue, "/concepts/") {
				return codeSearchRouteCochangeHistory
			}
		}
	}
	for _, hit := range hits {
		if acaHitLooksPackageOwner(hit) {
			return codeSearchRoutePackageOwner
		}
	}
	for _, hit := range hits {
		if acaHitLooksInfraResource(hit) {
			return codeSearchRouteInfraResource
		}
	}
	for _, hit := range hits {
		pathValue := strings.ToLower(strings.TrimSpace(hit.Path))
		if strings.Contains(pathValue, "/packages/") {
			return codeSearchRoutePackageOwner
		}
	}
	return codeSearchRouteCode
}

func acaHitLooksPackageOwner(hit contextplane.RetrievalHit) bool {
	pathValue := strings.ToLower(strings.TrimSpace(hit.Path))
	if !strings.Contains(pathValue, "/packages/") {
		return false
	}
	return !acaHitRepoPathsLookInfra(hit.RepoPaths)
}

func inferCodeSearchRouteFamilyFromCandidates(taskType, query string, candidates map[string]*codeSearchCandidate) codeSearchRouteFamily {
	if taskType == codeSearchTaskChangeImpact {
		return codeSearchRouteCochangeHistory
	}
	if len(candidates) == 0 {
		return codeSearchRouteCode
	}
	if looksLikeCodeSearchPackageFamily(query, candidates) {
		return codeSearchRoutePackageOwner
	}
	infraScore := 0.0
	codeScore := 0.0
	for _, candidate := range candidates {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		weight := candidate.Support
		if weight < 0.25 {
			weight = 0.25
		}
		weight += float64(len(candidate.Sources)) * 0.1
		if codeSearchCandidateLooksInfraResource(candidate) {
			infraScore += weight
		} else {
			codeScore += weight
		}
	}
	if infraScore >= 1.0 && infraScore > codeScore {
		return codeSearchRouteInfraResource
	}
	return codeSearchRouteCode
}

func looksLikeCodeSearchPackageFamily(query string, candidates map[string]*codeSearchCandidate) bool {
	_, ok := dominantCodeSearchPackageFamily(query, candidates)
	return ok
}

func dominantCodeSearchPackageFamily(query string, candidates map[string]*codeSearchCandidate) (string, bool) {
	type familyStats struct {
		count      int
		supportSum float64
		repoHits   int
	}
	families := map[string]*familyStats{}
	for _, candidate := range candidates {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		if codeSearchCandidateLooksInfraResource(candidate) || !isLikelyTextCodeFile(candidate.Path) || isLikelyDocumentationPath(candidate.Path) {
			continue
		}
		family := codeSearchCandidateCodeFamily(candidate)
		if family == "" {
			continue
		}
		stats := families[family]
		if stats == nil {
			stats = &familyStats{}
			families[family] = stats
		}
		stats.count++
		weight := candidate.Support
		if weight < 0.25 {
			weight = 0.25
		}
		stats.supportSum += weight
		if candidateHasSource(candidate, "search_repo") {
			stats.repoHits++
		}
	}
	bestFamily := ""
	bestCount := 0
	bestSupport := 0.0
	bestRepoHits := 0
	for family, stats := range families {
		if stats == nil {
			continue
		}
		if bestFamily == "" || stats.count > bestCount || (stats.count == bestCount && stats.supportSum > bestSupport) {
			bestFamily = family
			bestCount = stats.count
			bestSupport = stats.supportSum
			bestRepoHits = stats.repoHits
		}
	}
	if bestFamily == "" || bestCount < 3 || bestSupport < 2.25 || bestRepoHits < 1 {
		return "", false
	}
	if codeSearchPathTermScore(bestFamily, codeSearchPathTerms(query)) < 0.25 {
		return "", false
	}
	return bestFamily, true
}

func selectACAPackageAnchorPaths(repoPaths []string, candidates map[string]*codeSearchCandidate, acaSymbols []string, queryTerms, pathProbes, exactProbes []string, limit int) []string {
	if len(repoPaths) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 1
	}
	symbolProbes := packageAnchorSymbolProbes(acaSymbols)
	type scoredPath struct {
		path  string
		score float64
	}
	scored := make([]scoredPath, 0, len(repoPaths))
	for _, repoPath := range normalizeCodeSearchPaths(repoPaths) {
		if !isLikelyTextCodeFile(repoPath) || isLikelyDocumentationPath(repoPath) {
			continue
		}
		score := codeSearchPathTermScore(repoPath, queryTerms) * 2.8
		score += packageAnchorBasenameQueryAffinity(repoPath, queryTerms) * 2.4
		score += packageAnchorExactBasenameTermScore(repoPath, queryTerms) * 1.6
		score += codeSearchBasenameProbeScore(repoPath, pathProbes, exactProbes) * 2.6
		score += packageAnchorPathSymbolAffinity(repoPath, symbolProbes) * 2.2
		if candidate := candidates[repoPath]; candidate != nil {
			score += minFloat(candidate.Support, 0.8)
			score += float64(len(candidate.Sources)) * 0.1
		}
		score += packageAnchorRoleScore(repoPath)
		score -= float64(strings.Count(normalizeCodeSearchPath(repoPath), "/")) * 0.04
		scored = append(scored, scoredPath{path: repoPath, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].path < scored[j].path
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]string, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.path)
	}
	return out
}

func packageAnchorBasenameQueryAffinity(pathValue string, queryTerms []string) float64 {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(pathValue), filepath.Ext(pathValue)))
	if base == "" || len(queryTerms) == 0 {
		return 0
	}
	matches := 0
	for _, term := range queryTerms {
		term = strings.TrimSpace(strings.ToLower(term))
		if term == "" {
			continue
		}
		if strings.Contains(base, term) {
			matches++
		}
	}
	if matches == 0 {
		return 0
	}
	return clampScore(float64(matches) / float64(len(queryTerms)))
}

func packageAnchorExactBasenameTermScore(pathValue string, queryTerms []string) float64 {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(pathValue), filepath.Ext(pathValue)))
	if base == "" || len(queryTerms) == 0 {
		return 0
	}
	for _, term := range queryTerms {
		term = strings.TrimSpace(strings.ToLower(term))
		if term == "" {
			continue
		}
		if base == term {
			return 1.0
		}
	}
	return 0
}

func anchorPathAffinityScore(paths, queryTerms []string) float64 {
	if len(paths) == 0 || len(queryTerms) == 0 {
		return 0
	}
	best := 0.0
	for _, pathValue := range paths {
		pathScore := packageAnchorBasenameQueryAffinity(pathValue, queryTerms)
		pathScore += packageAnchorExactBasenameTermScore(pathValue, queryTerms) * 0.8
		if pathScore > best {
			best = pathScore
		}
	}
	return clampScore(best)
}

func packageACAGuidanceSpecificityScore(query string, hit contextplane.RetrievalHit) float64 {
	if strings.TrimSpace(query) == "" {
		return 0
	}
	queryTerms := codeSearchPathTerms(query)
	anchorPaths := preferredPackageAnchorPaths(hit)
	if len(anchorPaths) == 0 {
		anchorPaths = normalizeCodeSearchPaths(hit.RepoPaths)
	}
	score := acaGuidanceTitleAffinity(hit.Title, query)*2.0 + anchorPathAffinityScore(anchorPaths, queryTerms)
	if primary := normalizeCodeSearchPath(hit.PrimaryAnchorPath); primary != "" {
		score += packageAnchorBasenameQueryAffinity(primary, queryTerms) * 0.5
	}
	return score
}

func primaryAndAnchorPaths(hit contextplane.RetrievalHit) []string {
	out := make([]string, 0, len(hit.AnchorPaths)+1)
	if primary := normalizeCodeSearchPath(hit.PrimaryAnchorPath); primary != "" {
		out = append(out, primary)
	}
	for _, pathValue := range normalizeCodeSearchPaths(hit.AnchorPaths) {
		if pathValue == "" {
			continue
		}
		if len(out) > 0 && out[0] == pathValue {
			continue
		}
		out = append(out, pathValue)
	}
	return out
}

func anchorRolePaths(hit contextplane.RetrievalHit, role string) []string {
	if len(hit.AnchorRoles) == 0 {
		return nil
	}
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" {
		return nil
	}
	return normalizeCodeSearchPaths(hit.AnchorRoles[role])
}

func preferredPackageAnchorPaths(hit contextplane.RetrievalHit) []string {
	out := make([]string, 0, 8)
	seen := map[string]struct{}{}
	addAll := func(paths []string) {
		for _, pathValue := range normalizeCodeSearchPaths(paths) {
			if pathValue == "" {
				continue
			}
			if _, ok := seen[pathValue]; ok {
				continue
			}
			seen[pathValue] = struct{}{}
			out = append(out, pathValue)
		}
	}
	addAll(anchorRolePaths(hit, "impl"))
	addAll(primaryAndAnchorPaths(hit))
	addAll(anchorRolePaths(hit, "support"))
	return out
}

func preferredInfraAnchorPaths(hit contextplane.RetrievalHit) []string {
	out := make([]string, 0, 8)
	seen := map[string]struct{}{}
	addAll := func(paths []string) {
		for _, pathValue := range normalizeCodeSearchPaths(paths) {
			if pathValue == "" {
				continue
			}
			if _, ok := seen[pathValue]; ok {
				continue
			}
			seen[pathValue] = struct{}{}
			out = append(out, pathValue)
		}
	}
	addAll(anchorRolePaths(hit, "resource"))
	addAll(primaryAndAnchorPaths(hit))
	return out
}

func isPreferredFileLocateAnchorCandidate(candidate *codeSearchCandidate) bool {
	if candidate == nil {
		return false
	}
	return candidateHasAnySource(candidate, "aca_route_package_anchor", "aca_route_infra_exact")
}

func cloneAnchorRoles(raw map[string][]string) map[string][]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string][]string, len(raw))
	for role, paths := range raw {
		if len(paths) == 0 {
			continue
		}
		out[role] = append([]string(nil), paths...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func packageAnchorSymbolProbes(symbols []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(symbols))
	add := func(value string) {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			continue
		}
		parts := splitCodeSearchModuleParts(symbol)
		if len(parts) == 0 {
			continue
		}
		add(toSnakeCase(parts[len(parts)-1]))
	}
	return out
}

func packageAnchorPathSymbolAffinity(pathValue string, symbolProbes []string) float64 {
	if len(symbolProbes) == 0 {
		return 0
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(pathValue), filepath.Ext(pathValue)))
	best := 0.0
	for _, probe := range symbolProbes {
		switch {
		case base == probe:
			if best < 1.0 {
				best = 1.0
			}
		case strings.Contains(base, probe), strings.Contains(probe, base):
			if best < 0.55 {
				best = 0.55
			}
		}
	}
	return best
}

func packageAnchorRoleScore(pathValue string) float64 {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(pathValue), filepath.Ext(pathValue)))
	switch {
	case strings.HasSuffix(base, "_exec"):
		return 0.45
	case base == "ets":
		return 0.4
	}
	switch base {
	case "main", "config", "store", "index", "indexer", "runtime", "router", "provider", "plugin", "registry":
		return 0.35
	default:
		return 0
	}
}

func codeSearchCandidateCodeFamily(candidate *codeSearchCandidate) string {
	if candidate == nil || candidate.Path == "" {
		return ""
	}
	dir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(candidate.Path)))
	if dir == "" || dir == "." {
		return ""
	}
	return dir
}

func codeSearchCandidateLooksInfraResource(candidate *codeSearchCandidate) bool {
	if candidate == nil {
		return false
	}
	pathValue := strings.ToLower(strings.TrimSpace(candidate.Path))
	switch strings.ToLower(filepath.Ext(pathValue)) {
	case ".yaml", ".yml", ".tf", ".tpl":
		return true
	}
	switch {
	case strings.HasPrefix(pathValue, "infra/"),
		strings.HasPrefix(pathValue, "platform/"),
		strings.HasPrefix(pathValue, "deploy/"),
		strings.Contains(pathValue, "/helm/"),
		strings.Contains(pathValue, "/templates/"):
		return true
	}
	for _, symbol := range candidate.Symbols {
		symbol = strings.TrimSpace(symbol)
		if strings.Count(symbol, "/") >= 1 {
			return true
		}
	}
	return false
}

func acaHitLooksInfraResource(hit contextplane.RetrievalHit) bool {
	pathValue := strings.ToLower(strings.TrimSpace(hit.Path))
	if strings.Contains(pathValue, "/concepts/") {
		return true
	}
	return acaHitRepoPathsLookInfra(hit.RepoPaths)
}

func acaHitRepoPathsLookInfra(repoPaths []string) bool {
	for _, repoPath := range repoPaths {
		repoPath = strings.ToLower(strings.TrimSpace(repoPath))
		switch {
		case strings.HasPrefix(repoPath, "infra/"),
			strings.HasPrefix(repoPath, "platform/"),
			strings.HasPrefix(repoPath, "deploy/"),
			strings.HasPrefix(repoPath, "charts/"),
			strings.HasSuffix(repoPath, ".yaml"),
			strings.HasSuffix(repoPath, ".yml"),
			strings.HasSuffix(repoPath, ".tf"),
			strings.HasSuffix(repoPath, ".sh"):
			return true
		}
	}
	return false
}

func normalizeCodeSearchTaskType(taskType string) (string, bool) {
	switch strings.TrimSpace(taskType) {
	case codeSearchTaskFileLocate:
		return codeSearchTaskFileLocate, false
	case codeSearchTaskExecutionTrace:
		return codeSearchTaskExecutionTrace, false
	case codeSearchTaskSymbolInspect:
		return codeSearchTaskSymbolInspect, false
	case codeSearchTaskChangeImpact:
		return codeSearchTaskChangeImpact, false
	case codeSearchTaskRegistrationTrace:
		return codeSearchTaskRegistrationTrace, false
	case "":
		return codeSearchTaskFileLocate, true
	default:
		return codeSearchTaskFileLocate, true
	}
}

func repoResultMaps(raw any) []map[string]any {
	items, ok := raw.([]map[string]any)
	if ok {
		return items
	}
	body, _ := json.Marshal(raw)
	var out []map[string]any
	_ = json.Unmarshal(body, &out)
	return out
}

func semanticBundleMaps(raw any) []map[string]any {
	items, ok := raw.([]map[string]any)
	if ok {
		return items
	}
	body, _ := json.Marshal(raw)
	var out []map[string]any
	_ = json.Unmarshal(body, &out)
	return out
}

var (
	codeLikeTokenPattern          = regexp.MustCompile(`[\pL\pN_./:-]+`)
	groundedTypeSymbolPattern     = regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	groundedFuncSymbolPattern     = regexp.MustCompile(`^\s*func\s*(?:\([^)]+\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	groundedVarConstSymbolPattern = regexp.MustCompile(`^\s*(?:var|const)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	bridgeCtorAssignPattern       = regexp.MustCompile(`(?m)([A-Za-z_][A-Za-z0-9_]*)\s*(?::=|=)\s*(?:[A-Za-z_][A-Za-z0-9_]*\.)?New([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	bridgeMethodLiteralPattern    = regexp.MustCompile(`(?m)([A-Za-z_][A-Za-z0-9_]*)\.(Execute)\s*\([^\n]*"([A-Za-z_][A-Za-z0-9_]*)"`)
	elixirProtocolPattern         = regexp.MustCompile(`(?m)^\s*defprotocol\s+([A-Za-z0-9_.]+)\s+do`)
)

func codeSearchExactProbes(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, token := range codeLikeTokenPattern.FindAllString(query, -1) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if isGenericCodeSearchPathWord(token) {
			continue
		}
		if !looksLikeCodeProbe(token) {
			continue
		}
		add(token)
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func codeSearchTaskExactProbes(query, taskType string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, probe := range codeSearchExactProbes(query) {
		add(probe)
	}
	if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskChangeImpact {
		for _, probe := range normalizedExactProbePrefixes(codeSearchExactProbes(query)) {
			add(probe)
			if len(out) >= 6 {
				return out
			}
		}
		normalizedExisting := normalizedExactProbePrefixes(out)
		for _, probe := range codeSearchPhraseIdentifierProbes(query) {
			joined := strings.Join(splitCodeSearchProbe(probe), "_")
			skip := false
			for _, existing := range normalizedExisting {
				if existing == "" || existing == joined {
					continue
				}
				if strings.HasPrefix(existing, joined+"_") {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			add(probe)
			if len(out) >= 6 {
				break
			}
		}
	}
	return out
}

func codeSearchRepoProbes(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	normalizedExact := normalizedExactProbePrefixes(codeSearchExactProbes(query))
	isStrictExactPrefix := func(value string) bool {
		parts := splitCodeSearchProbe(value)
		if len(parts) == 0 {
			return false
		}
		joined := strings.Join(parts, "_")
		for _, exact := range normalizedExact {
			if exact == "" || exact == joined {
				continue
			}
			if strings.HasPrefix(exact, joined+"_") {
				return true
			}
		}
		return false
	}
	add := func(value string) {
		value = normalizeRepoSearchProbe(value)
		if value == "" {
			return
		}
		if isStrictExactPrefix(value) {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(query)
	for _, probe := range codeSearchExactProbes(query) {
		add(probe)
		if strings.Contains(probe, "_") {
			add(strings.ReplaceAll(probe, "_", " "))
		}
		if idx := strings.LastIndex(probe, "_"); idx > 0 {
			add(probe[:idx])
		}
	}
	if len(out) == 0 {
		add(query)
	}
	return out
}

func codeSearchExecutionTraceSideRepoProbes(query string, exactProbes []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(value string) {
		value = normalizeRepoSearchProbe(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(query)
	for _, probe := range exactProbes {
		probe = strings.TrimSpace(probe)
		if probe == "" {
			continue
		}
		add(probe)
		if strings.Contains(probe, "_") {
			add(strings.ReplaceAll(probe, "_", " "))
		}
	}
	return out
}

func codeSearchPathProbes(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	normalizedExact := normalizedExactProbePrefixes(codeSearchExactProbes(query))
	isStrictExactPrefix := func(value string) bool {
		for _, exact := range normalizedExact {
			if exact == "" || exact == value {
				continue
			}
			if strings.HasPrefix(exact, value+"_") {
				return true
			}
		}
		return false
	}
	add := func(value string) {
		value = strings.TrimSpace(strings.ToLower(value))
		if len(value) < 3 {
			return
		}
		if isStrictExactPrefix(value) {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, probe := range codeSearchExactProbes(query) {
		add(probe)
		for _, derived := range derivedCodeSearchPathProbes(probe) {
			add(derived)
		}
		if strings.Contains(probe, "_") && !strings.ContainsAny(probe, "/.:") {
			parts := splitCodeSearchProbe(probe)
			if len(parts) >= 2 {
				add(parts[0] + "_" + parts[1])
			}
		}
	}
	for _, probe := range codeSearchRepoProbes(query) {
		parts := splitCodeSearchProbe(probe)
		if len(parts) >= 2 && len(parts) <= 4 {
			add(strings.Join(parts, "_"))
		}
		if len(parts) >= 3 {
			for i := 0; i+2 < len(parts); i++ {
				add(strings.Join(parts[i:i+3], "_"))
			}
		}
		if len(parts) >= 2 {
			for i := 0; i+1 < len(parts); i++ {
				add(strings.Join(parts[i:i+2], "_"))
			}
		}
	}
	return out
}

func codeSearchTaskPathProbes(query, taskType string, exactProbes []string) []string {
	base := codeSearchPathProbes(query)
	if taskType != codeSearchTaskExecutionTrace && taskType != codeSearchTaskChangeImpact {
		return base
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+4)
	for _, item := range base {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	normalizedExact := normalizedExactProbePrefixes(exactProbes)
	for _, probe := range codeSearchPhraseIdentifierProbes(query) {
		probe = strings.TrimSpace(strings.ToLower(probe))
		if probe == "" {
			continue
		}
		skip := false
		for _, exact := range normalizedExact {
			if exact == "" || exact == probe {
				continue
			}
			if strings.HasPrefix(exact, probe+"_") {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if _, ok := seen[probe]; ok {
			continue
		}
		seen[probe] = struct{}{}
		out = append(out, probe)
	}
	return out
}

func deriveExecutionTraceAnchors(query, taskType string) codeSearchTraceAnchors {
	if taskType != codeSearchTaskExecutionTrace {
		return codeSearchTraceAnchors{}
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return codeSearchTraceAnchors{}
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bconnect\s+(.+?)\s+\bto\b\s+(.+)$`),
		regexp.MustCompile(`(?i)\bfrom\s+(.+?)\s+\bto\b\s+(.+)$`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(query)
		if len(matches) != 3 {
			continue
		}
		sourceQuery := normalizeExecutionTraceAnchorQuery(matches[1])
		targetQuery := normalizeExecutionTraceAnchorQuery(matches[2])
		if sourceQuery == "" || targetQuery == "" {
			continue
		}
		sourceExact := codeSearchTaskExactProbes(sourceQuery, codeSearchTaskExecutionTrace)
		targetExact := codeSearchTaskExactProbes(targetQuery, codeSearchTaskExecutionTrace)
		return codeSearchTraceAnchors{
			SourceQuery:       sourceQuery,
			TargetQuery:       targetQuery,
			SourceExactProbes: sourceExact,
			TargetExactProbes: targetExact,
			SourcePathProbes:  codeSearchTaskPathProbes(sourceQuery, codeSearchTaskExecutionTrace, sourceExact),
			TargetPathProbes:  codeSearchTaskPathProbes(targetQuery, codeSearchTaskExecutionTrace, targetExact),
		}
	}
	return codeSearchTraceAnchors{}
}

func normalizeExecutionTraceAnchorQuery(raw string) string {
	tokens := codeLikeTokenPattern.FindAllString(raw, -1)
	if len(tokens) == 0 {
		return ""
	}
	for len(tokens) > 0 && isGenericCodeSearchPathWord(tokens[0]) {
		tokens = tokens[1:]
	}
	for len(tokens) > 0 && isGenericCodeSearchPathWord(tokens[len(tokens)-1]) {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) == 0 {
		return ""
	}
	return strings.Join(tokens, " ")
}

func stringSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(strings.ToLower(item))
		if item == "" {
			continue
		}
		out[item] = struct{}{}
	}
	return out
}

func normalizedExactProbePrefixes(probes []string) []string {
	out := make([]string, 0, len(probes))
	for _, probe := range probes {
		parts := splitCodeSearchProbe(probe)
		if len(parts) == 0 {
			continue
		}
		out = append(out, strings.Join(parts, "_"))
	}
	return out
}

func codeSearchPhraseIdentifierProbes(query string) []string {
	tokens := splitCodeSearchProbe(query)
	if len(tokens) < 2 {
		return nil
	}
	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if isGenericCodeSearchPathWord(token) {
			continue
		}
		filtered = append(filtered, token)
	}
	if len(filtered) < 2 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for width := 2; width <= 3; width++ {
		for i := 0; i+width <= len(filtered); i++ {
			parts := filtered[i : i+width]
			joined := strings.Join(parts, "_")
			if len(joined) < 5 {
				continue
			}
			if _, ok := seen[joined]; ok {
				continue
			}
			seen[joined] = struct{}{}
			out = append(out, joined)
			if len(out) >= 4 {
				return out
			}
		}
	}
	return out
}

func looksLikeCodeProbe(token string) bool {
	if strings.Contains(token, "_") || strings.Contains(token, "/") || strings.Contains(token, ".") || strings.Contains(token, ":") {
		return true
	}
	if isLikelyPascalCodeIdentifier(token) {
		return true
	}
	hasLower := false
	hasInternalUpper := false
	for i, r := range token {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			if i > 0 {
				hasInternalUpper = true
			}
		}
	}
	return hasLower && hasInternalUpper
}

func isLikelyPascalCodeIdentifier(token string) bool {
	token = strings.TrimSpace(token)
	if len(token) < 4 {
		return false
	}
	hasLower := false
	for i, r := range token {
		switch {
		case r >= 'A' && r <= 'Z':
			if i == 0 {
				continue
			}
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	first := rune(token[0])
	return first >= 'A' && first <= 'Z' && hasLower
}

func derivedCodeSearchPathProbes(probe string) []string {
	parts := splitCodeSearchModuleParts(probe)
	if len(parts) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) < 3 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(toSnakeCase(parts[len(parts)-1]))
	if len(parts) >= 2 {
		add(toSnakeCase(parts[len(parts)-2]) + "/" + toSnakeCase(parts[len(parts)-1]))
		add(toSnakeCase(parts[len(parts)-2]) + "_" + toSnakeCase(parts[len(parts)-1]))
	}
	return out
}

func splitCodeSearchModuleParts(value string) []string {
	fields := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return r == '.' || r == '/' || r == ':' || r == '\\'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		out = append(out, field)
	}
	return out
}

func toSnakeCase(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var out []rune
	var prev rune
	for i, r := range value {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && ((prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9')) {
				out = append(out, '_')
			}
			out = append(out, r+('a'-'A'))
		} else {
			out = append(out, unicodeLowerASCII(r))
		}
		prev = r
	}
	return strings.Trim(strings.ReplaceAll(string(out), "__", "_"), "_")
}

func unicodeLowerASCII(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

func normalizeRepoSearchProbe(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("?", " ", "!", " ", ",", " ", ";", " ", "(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ", "\"", " ", "'", " ", "-", " ")
	value = replacer.Replace(value)
	value = strings.Join(strings.Fields(value), " ")
	return strings.TrimSpace(value)
}

func addCodeSearchCandidate(candidates map[string]*codeSearchCandidate, pathValue, why, source string, support float64, lineHint int, symbolName, repoNodeID string) {
	pathValue = normalizeCodeSearchPath(pathValue)
	if pathValue == "" {
		return
	}
	item, ok := candidates[pathValue]
	if !ok {
		item = &codeSearchCandidate{
			Path:    pathValue,
			Why:     strings.TrimSpace(why),
			Sources: map[string]struct{}{},
		}
		candidates[pathValue] = item
	}
	if strings.TrimSpace(why) != "" && (item.Why == "" || len(why) > len(item.Why)) {
		item.Why = strings.TrimSpace(why)
	}
	if strings.TrimSpace(source) != "" {
		item.Sources[strings.TrimSpace(source)] = struct{}{}
	}
	item.Support += support
	if lineHint > 0 {
		item.LineHints = append(item.LineHints, lineHint)
	}
	if strings.TrimSpace(symbolName) != "" {
		item.Symbols = append(item.Symbols, strings.TrimSpace(symbolName))
	}
	if strings.TrimSpace(repoNodeID) != "" {
		item.RepoNodeIDs = append(item.RepoNodeIDs, strings.TrimSpace(repoNodeID))
	}
}

func rankCodeSearchCandidates(candidates map[string]*codeSearchCandidate, query string, taskType string, limit int) []*codeSearchCandidate {
	return rankCodeSearchCandidatesWithPlan(candidates, query, taskType, limit, codeSearchPathProbes(query), codeSearchExactProbes(query), nil, nil, "")
}

func rankCodeSearchCandidatesWithProbes(candidates map[string]*codeSearchCandidate, query string, taskType string, limit int, pathProbes, exactProbes []string) []*codeSearchCandidate {
	return rankCodeSearchCandidatesWithPlan(candidates, query, taskType, limit, pathProbes, exactProbes, nil, nil, "")
}

func rankCodeSearchCandidatesWithPlan(candidates map[string]*codeSearchCandidate, query string, taskType string, limit int, pathProbes, exactProbes []string, plannerEdgePrioritySet, plannerMustCoverSet map[string]struct{}, plannerAmbiguityFamily string) []*codeSearchCandidate {
	out := make([]*codeSearchCandidate, 0, len(candidates))
	pathTerms := codeSearchPathTerms(query)
	traceAnchors := deriveExecutionTraceAnchors(query, taskType)
	sourceTerms := codeSearchPathTerms(traceAnchors.SourceQuery)
	targetTerms := codeSearchPathTerms(traceAnchors.TargetQuery)
	preferredBundleDirs := map[string]struct{}{}
	hasPackageGuidance := false
	preferredAnchorDirs := map[string]struct{}{}
	primaryAnchorLeaderByBase := map[string]string{}
	for _, candidate := range candidates {
		if candidateHasAnySource(candidate, "aca_route_package_exact", "aca_route_package_anchor") {
			hasPackageGuidance = true
		}
		if candidateHasAnySource(candidate, "aca_route_package_primary_anchor", "aca_route_infra_primary_anchor") {
			dir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(candidate.Path)))
			if dir != "" && dir != "." {
				preferredAnchorDirs[dir] = struct{}{}
			}
			base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(candidate.Path), filepath.Ext(candidate.Path)))
			base = strings.TrimSuffix(base, "_test")
			if base != "" {
				existing := primaryAnchorLeaderByBase[base]
				if existing == "" {
					primaryAnchorLeaderByBase[base] = candidate.Path
				} else {
					existingCandidate := candidates[existing]
					switch {
					case existingCandidate == nil:
						primaryAnchorLeaderByBase[base] = candidate.Path
					case candidate.Support > existingCandidate.Support:
						primaryAnchorLeaderByBase[base] = candidate.Path
					case candidate.Support == existingCandidate.Support && candidate.Path < existingCandidate.Path:
						primaryAnchorLeaderByBase[base] = candidate.Path
					}
				}
			}
		}
	}
	for _, candidate := range candidates {
		if !candidateHasSource(candidate, "semantic_bundle_primary") {
			continue
		}
		if codeSearchBasenameProbeScore(candidate.Path, pathProbes, exactProbes) <= 0 {
			continue
		}
		dir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(candidate.Path)))
		if dir != "" && dir != "." {
			preferredBundleDirs[dir] = struct{}{}
		}
	}
	for _, candidate := range candidates {
		candidate.Support += float64(len(candidate.Sources)) * 0.15
		candidate.Support += codeSearchPathTermScore(candidate.Path, pathTerms) * 0.45
		symbolScore := codeSearchExactSymbolScore(candidate, exactProbes)
		subtypePenalty := codeSearchSubtypePenalty(candidate, exactProbes)
		switch taskType {
		case codeSearchTaskSymbolInspect:
			candidate.Support += symbolScore * 1.25
			candidate.Support -= subtypePenalty * 0.9
		case codeSearchTaskFileLocate:
			candidate.Support += symbolScore * 0.35
			candidate.Support -= subtypePenalty * 0.2
		case codeSearchTaskExecutionTrace, codeSearchTaskChangeImpact, codeSearchTaskRegistrationTrace:
			candidate.Support += symbolScore * 0.2
			candidate.Support -= subtypePenalty * 0.15
		default:
			candidate.Support += symbolScore * 0.4
			candidate.Support -= subtypePenalty * 0.25
		}
		basenameScore := 0.0
		if taskType != codeSearchTaskRegistrationTrace {
			basenameScore = codeSearchBasenameProbeScore(candidate.Path, pathProbes, exactProbes)
			candidate.Support += basenameScore
		}
		if (candidateHasSource(candidate, "semantic_bundle_primary") || candidateHasSource(candidate, "semantic_bundle_related")) && basenameScore > 0 {
			candidate.Support += basenameScore * 0.85
		}
		if taskType == codeSearchTaskFileLocate && candidateHasSource(candidate, "semantic_bundle_related") {
			dir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(candidate.Path)))
			if _, ok := preferredBundleDirs[dir]; ok && isLikelyTextCodeFile(candidate.Path) {
				candidate.Support += 0.95
			} else if len(preferredBundleDirs) > 0 {
				candidate.Support -= 0.85
			}
		}
		if taskType == codeSearchTaskFileLocate && candidateHasSource(candidate, "aca_route_package_anchor") {
			switch {
			case candidateHasSource(candidate, "aca_route_package_primary_anchor"):
				candidate.Support += 2.4
			case candidateHasSource(candidate, "aca_route_package_secondary_anchor"):
				candidate.Support += 0.4
			default:
				candidate.Support += 2.4
			}
		}
		if taskType == codeSearchTaskFileLocate {
			if candidateHasAnySource(candidate, "aca_route_package_primary_anchor", "aca_route_infra_primary_anchor") {
				candidate.Support += 1.1
				base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(candidate.Path), filepath.Ext(candidate.Path)))
				base = strings.TrimSuffix(base, "_test")
				if leaderPath := primaryAnchorLeaderByBase[base]; leaderPath != "" && leaderPath != candidate.Path {
					candidate.Support -= 1.75
				}
			}
			if candidateHasAnySource(candidate, "aca_route_package_secondary_anchor", "aca_route_infra_secondary_anchor") {
				dir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(candidate.Path)))
				if _, ok := preferredAnchorDirs[dir]; ok {
					candidate.Support -= 2.5
				}
				base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(candidate.Path), filepath.Ext(candidate.Path)))
				base = strings.TrimSuffix(base, "_test")
				if leaderPath := primaryAnchorLeaderByBase[base]; leaderPath != "" && leaderPath != candidate.Path {
					candidate.Support -= 1.75
				}
			}
		}
		if taskType == codeSearchTaskFileLocate && hasPackageGuidance {
			if candidateHasSource(candidate, "aca_route_package_secondary_anchor") {
				candidate.Support -= 0.6
			} else if candidateHasAnySource(candidate, "aca_route_package_exact", "aca_route_package_anchor") {
				candidate.Support += 1.2
			} else {
				candidate.Support -= 0.8
			}
		}
		if candidateHasSource(candidate, "exact_probe") {
			candidate.Support += 0.2
		}
		if len(exactProbes) > 0 && codeSearchPathTermScore(candidate.Path, pathTerms) == 0 && !candidateHasSource(candidate, "exact_probe") {
			candidate.Support -= 0.25
		}
		if taskType == codeSearchTaskRegistrationTrace {
			if candidateHasSource(candidate, "registration_trace") {
				candidate.Support += 2.0
			}
			if candidateHasSource(candidate, "path_probe") && !candidateHasSource(candidate, "registration_trace") {
				candidate.Support -= 0.25
			}
		}
		if (taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskChangeImpact) && candidateHasSource(candidate, "execution_bridge") {
			candidate.Support += 1.5
		}
		if taskType == codeSearchTaskExecutionTrace && candidateHasSource(candidate, "execution_graph") {
			candidate.Support += 1.0
		}
		if taskType == codeSearchTaskExecutionTrace {
			candidate.Support += codeSearchPlannerBoost(candidate, plannerEdgePrioritySet, plannerMustCoverSet, plannerAmbiguityFamily)
			if candidateHasSource(candidate, "llm_selector_keep") {
				candidate.Support += 1.1
			}
			if candidateHasSource(candidate, "execution_graph_implements") {
				candidate.Support += 1.2
			}
			if candidateHasSource(candidate, "execution_graph_uses_symbol") {
				candidate.Support += 0.9
			}
			if candidateHasSource(candidate, "execution_graph_refers_to") {
				candidate.Support += 0.35
			}
		}
		if taskType == codeSearchTaskExecutionTrace {
			sourceAffinity := executionTraceCandidateAffinity(candidate, sourceTerms, traceAnchors.SourcePathProbes, traceAnchors.SourceExactProbes)
			targetAffinity := executionTraceCandidateAffinity(candidate, targetTerms, traceAnchors.TargetPathProbes, traceAnchors.TargetExactProbes)
			if candidateHasAnySource(candidate, "trace_source_anchor", "trace_source_repo") {
				candidate.Support += 1.2 + (sourceAffinity * 0.9)
			} else if sourceAffinity > 0 && candidateHasSource(candidate, "search_repo") {
				candidate.Support += sourceAffinity * 0.35
			}
			if candidateHasAnySource(candidate, "trace_target_anchor", "trace_target_repo") {
				candidate.Support += 0.9 + (targetAffinity * 1.1)
			} else if targetAffinity > 0 && candidateHasAnySource(candidate, "execution_graph", "search_repo") {
				candidate.Support += targetAffinity * 0.8
			}
			if candidateHasSource(candidate, "trace_target_anchor") {
				candidate.Support += 0.25
			}
		}
		if taskType == codeSearchTaskExecutionTrace && candidateHasSource(candidate, "bridge_tuple") {
			candidate.Support += 1.4
		}
		if strings.HasSuffix(candidate.Path, "_test.go") {
			penalty := 0.6
			switch taskType {
			case codeSearchTaskExecutionTrace, codeSearchTaskChangeImpact, codeSearchTaskRegistrationTrace:
				penalty = 1.25
			}
			candidate.Support -= penalty
		}
		switch strings.ToLower(filepath.Ext(candidate.Path)) {
		case ".md", ".txt":
			candidate.Support -= 1.8
		}
		if isLikelyDocumentationPath(candidate.Path) {
			candidate.Support -= 0.8
		}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Support == out[j].Support {
			return out[i].Path < out[j].Path
		}
		return out[i].Support > out[j].Support
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func codeSearchBasenameProbeScore(pathValue string, pathProbes, exactProbes []string) float64 {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(filepath.ToSlash(strings.TrimSpace(pathValue))), filepath.Ext(pathValue)))
	if base == "" {
		return 0
	}
	best := 0.0
	for _, probe := range append(append([]string(nil), pathProbes...), exactProbes...) {
		probe = strings.ToLower(strings.TrimSpace(probe))
		if probe == "" {
			continue
		}
		switch {
		case base == probe:
			if best < 0.95 {
				best = 0.95
			}
		case strings.HasSuffix(base, probe):
			if best < 0.75 {
				best = 0.75
			}
		case strings.Contains(base, probe):
			if best < 0.45 {
				best = 0.45
			}
		}
	}
	return best
}

func candidateHasSource(candidate *codeSearchCandidate, source string) bool {
	if candidate == nil || candidate.Sources == nil {
		return false
	}
	_, ok := candidate.Sources[strings.TrimSpace(source)]
	return ok
}

func candidateHasAnySource(candidate *codeSearchCandidate, sources ...string) bool {
	for _, source := range sources {
		if candidateHasSource(candidate, source) {
			return true
		}
	}
	return false
}

func executionTraceCandidateAffinity(candidate *codeSearchCandidate, pathTerms []string, pathProbes, exactProbes []string) float64 {
	if candidate == nil {
		return 0
	}
	termScore := codeSearchPathTermScore(candidate.Path, pathTerms)
	baseScore := codeSearchBasenameProbeScore(candidate.Path, pathProbes, exactProbes)
	score := termScore
	if baseScore > score {
		score = baseScore
	}
	if score <= 0 {
		return 0
	}
	if isLikelyTextCodeFile(candidate.Path) {
		score += 0.1
	}
	return clampScore(score)
}

func codeSearchExactSymbolScore(candidate *codeSearchCandidate, exactProbes []string) float64 {
	if candidate == nil || len(candidate.Symbols) == 0 || len(exactProbes) == 0 {
		return 0
	}
	best := 0.0
	for _, symbol := range candidate.Symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			continue
		}
		symbolFold := strings.ToLower(symbol)
		for _, probe := range exactProbes {
			probe = strings.TrimSpace(probe)
			if probe == "" {
				continue
			}
			probeFold := strings.ToLower(probe)
			switch {
			case symbolFold == probeFold:
				if best < 1.2 {
					best = 1.2
				}
			case strings.HasSuffix(symbolFold, "."+probeFold):
				if best < 1.0 {
					best = 1.0
				}
			case strings.HasPrefix(symbolFold, probeFold+"."):
				if best < 0.35 {
					best = 0.35
				}
			}
		}
	}
	return best
}

func codeSearchSubtypePenalty(candidate *codeSearchCandidate, exactProbes []string) float64 {
	if candidate == nil || len(candidate.Symbols) == 0 || len(exactProbes) == 0 {
		return 0
	}
	best := 0.0
	for _, symbol := range candidate.Symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			continue
		}
		symbolFold := strings.ToLower(symbol)
		for _, probe := range exactProbes {
			probe = strings.TrimSpace(probe)
			if probe == "" {
				continue
			}
			probeFold := strings.ToLower(probe)
			if symbolFold != probeFold && (strings.HasPrefix(symbolFold, probeFold+".") || strings.Contains(symbolFold, "."+probeFold+".")) {
				if best < 1.0 {
					best = 1.0
				}
			}
		}
	}
	return best
}

func codeSearchPathTerms(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) < 3 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, probe := range codeSearchExactProbes(query) {
		for _, part := range splitCodeSearchProbe(probe) {
			add(part)
		}
		if strings.Contains(probe, "_") {
			parts := splitCodeSearchProbe(probe)
			if len(parts) >= 2 {
				add(parts[0] + "_" + parts[1])
			}
		}
	}
	for _, token := range splitCodeSearchProbe(query) {
		if isGenericCodeSearchPathWord(token) {
			continue
		}
		add(token)
	}
	return out
}

func isGenericCodeSearchPathWord(word string) bool {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "", "the", "where", "which", "what", "does", "with", "from", "into", "to", "in", "of", "for", "on", "at", "by", "via", "and", "or", "that", "this", "these", "those", "change", "changes", "changed", "file", "files", "path", "paths", "anchor", "anchors", "directly", "implemented", "defines", "defined", "connect", "connects", "used", "execution", "declared", "implementation", "manifest", "skill", "skills", "package", "packages":
		return true
	default:
		return false
	}
}

func splitCodeSearchProbe(probe string) []string {
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(probe)), func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= '0' && r <= '9':
			return false
		default:
			return true
		}
	})
	return fields
}

func codeSearchPathTermScore(pathValue string, terms []string) float64 {
	if len(terms) == 0 {
		return 0
	}
	pathValue = strings.ToLower(filepath.ToSlash(strings.TrimSpace(pathValue)))
	matches := 0
	for _, term := range terms {
		needle := strings.ToLower(strings.TrimSpace(term))
		if needle == "" {
			continue
		}
		if strings.Contains(pathValue, needle) {
			matches++
		}
	}
	return clampScore(float64(matches) / float64(len(terms)))
}

func normalizeCodeSearchPaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, item := range paths {
		item = normalizeCodeSearchPath(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func normalizeCodeSearchPath(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "./")
	return value
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func applyRepoMotifSupport(candidates map[string]*codeSearchCandidate, hits []contextplane.RepoMotifSearchHit, taskType string, excluded []string) int {
	if len(candidates) == 0 || len(hits) == 0 {
		return 0
	}
	applied := 0
	for _, hit := range hits {
		baseScore := 0.15 + minFloat(hit.Score, 1.0)*0.15
		if taskType == codeSearchTaskExecutionTrace {
			baseScore += 0.05
		}
		apply := func(pathValue, source string, score float64) {
			pathValue = normalizeCodeSearchPath(pathValue)
			if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
				return
			}
			candidate := candidates[pathValue]
			if candidate == nil {
				return
			}
			if candidate.Sources == nil {
				candidate.Sources = map[string]struct{}{}
			}
			if _, ok := candidate.Sources[source]; !ok {
				candidate.Sources[source] = struct{}{}
				applied++
			}
			candidate.Support += score
		}
		apply(hit.AnchorPath, "repo_motif_anchor_support", baseScore)
		for _, pathValue := range hit.RelatedPaths {
			apply(pathValue, "repo_motif_support", baseScore-0.05)
		}
	}
	return applied
}

func repoMotifPathScores(hits []contextplane.RepoMotifSearchHit) map[string]float64 {
	out := map[string]float64{}
	for _, hit := range hits {
		base := minFloat(hit.Score, 1.0)
		if pathValue := normalizeCodeSearchPath(hit.AnchorPath); pathValue != "" {
			out[pathValue] += base
		}
		for _, pathValue := range hit.RelatedPaths {
			pathValue = normalizeCodeSearchPath(pathValue)
			if pathValue == "" {
				continue
			}
			out[pathValue] += base * 0.85
		}
	}
	return out
}

func isExcludedCodeSearchPath(pathValue string, excluded []string) bool {
	pathValue = normalizeCodeSearchPath(pathValue)
	if pathValue == "" {
		return false
	}
	for _, pattern := range excluded {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if prefix != "" && strings.HasPrefix(pathValue, prefix+"/") {
				return true
			}
		}
		if ok, _ := path.Match(pattern, pathValue); ok {
			return true
		}
	}
	return false
}

func codeSearchSliceBounds(lineHint int, taskType string) (int, int) {
	if lineHint <= 0 {
		switch taskType {
		case codeSearchTaskSymbolInspect:
			return 1, 160
		default:
			return 1, 120
		}
	}
	start := lineHint - 20
	if start < 1 {
		start = 1
	}
	end := lineHint + 40
	return start, end
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func lastPositive(values ...int) int {
	for i := len(values) - 1; i >= 0; i-- {
		if values[i] > 0 {
			return values[i]
		}
	}
	return 0
}

func buildCodeSearchAnswerBasis(lanes []string) string {
	if len(lanes) == 0 {
		return "none"
	}
	return strings.Join(lanes, " + ")
}

func buildCodeSearchSummary(taskType string, files []codeSearchEvidenceFile, callPaths []map[string]any, gaps []string) string {
	if len(files) == 0 {
		if len(gaps) > 0 {
			return "No grounded code evidence was found."
		}
		return ""
	}
	switch taskType {
	case codeSearchTaskExecutionTrace:
		if len(callPaths) > 0 {
			return fmt.Sprintf("Grounded %d file(s) and %d structural call-path anchor(s) for the execution trace.", len(files), len(callPaths))
		}
		return fmt.Sprintf("Grounded %d file(s) for the execution trace.", len(files))
	case codeSearchTaskSymbolInspect:
		return fmt.Sprintf("Grounded %d file(s) for symbol inspection.", len(files))
	default:
		return fmt.Sprintf("Grounded %d candidate file(s) for the code-location query.", len(files))
	}
}

func codeSearchConfidence(taskType string, fileCount, groundedCount, callPathCount int, requireGrounding bool) float64 {
	score := 0.2
	score += float64(minInt(fileCount, 4)) * 0.12
	score += float64(minInt(groundedCount, 3)) * 0.18
	if taskType == codeSearchTaskExecutionTrace {
		score += float64(minInt(callPathCount, 4)) * 0.06
	}
	if requireGrounding && groundedCount == 0 {
		score -= 0.35
	}
	return clampScore(score)
}

func dedupeCodeSearchSymbols(in []codeSearchEvidenceSymbol) []codeSearchEvidenceSymbol {
	if len(in) <= 1 {
		return in
	}
	seen := map[string]struct{}{}
	out := make([]codeSearchEvidenceSymbol, 0, len(in))
	for _, item := range in {
		key := item.Path + "|" + item.Symbol + "|" + fmt.Sprintf("%d", item.Line)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func buildCodeSearchCandidateTrace(ranked []*codeSearchCandidate, selectedPaths map[string]int, maxFiles int, taskType string) []codeSearchCandidateTrace {
	if len(ranked) == 0 {
		return nil
	}
	inferredAnchorRoles := map[string]string{}
	if taskType == codeSearchTaskFileLocate {
		inferredAnchorRoles = inferFileLocateAnchorRoles(ranked, selectedPaths)
	}
	limit := len(ranked)
	if limit > 12 {
		limit = 12
	}
	out := make([]codeSearchCandidateTrace, 0, limit)
	for idx, candidate := range ranked[:limit] {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		trace := codeSearchCandidateTrace{
			Path:          candidate.Path,
			Why:           candidate.Why,
			SupportScore:  clampScore(candidate.Support),
			Sources:       sortedSourceKeys(candidate.Sources),
			LineHints:     uniquePositiveInts(candidate.LineHints),
			Symbols:       uniqueStrings(candidate.Symbols),
			RepoNodeCount: len(uniqueStrings(candidate.RepoNodeIDs)),
		}
		if taskType == codeSearchTaskFileLocate {
			trace.EvidenceClass = fileLocateEvidenceBucket(candidate)
			trace.AnchorRole = firstNonEmpty(fileLocateAnchorRole(candidate), inferredAnchorRoles[candidate.Path])
			if trace.AnchorRole == "primary_anchor" || trace.AnchorRole == "secondary_anchor" {
				trace.EvidenceClass = trace.AnchorRole
			}
		}
		if rank, ok := selectedPaths[candidate.Path]; ok {
			trace.Selected = true
			trace.SelectedRank = rank
		} else if idx >= maxFiles {
			trace.PruneReason = "file_budget"
		} else {
			trace.PruneReason = "not_grounded"
		}
		out = append(out, trace)
	}
	return out
}

func buildFileLocateEvidenceBuckets(files []codeSearchEvidenceFile, rankedByPath map[string]*codeSearchCandidate) map[string][]string {
	if len(files) == 0 {
		return nil
	}
	selectedPaths := make(map[string]int, len(files))
	for idx, file := range files {
		pathValue := normalizeCodeSearchPath(file.Path)
		if pathValue == "" {
			continue
		}
		selectedPaths[pathValue] = idx + 1
	}
	ranked := make([]*codeSearchCandidate, 0, len(rankedByPath))
	for _, file := range files {
		pathValue := normalizeCodeSearchPath(file.Path)
		if pathValue == "" {
			continue
		}
		if candidate := rankedByPath[pathValue]; candidate != nil {
			ranked = append(ranked, candidate)
		}
	}
	inferredAnchorRoles := inferFileLocateAnchorRoles(ranked, selectedPaths)
	buckets := map[string][]string{}
	for _, file := range files {
		pathValue := normalizeCodeSearchPath(file.Path)
		if pathValue == "" {
			continue
		}
		bucket := "other"
		if candidate := rankedByPath[pathValue]; candidate != nil {
			anchorRole := firstNonEmpty(fileLocateAnchorRole(candidate), inferredAnchorRoles[pathValue])
			if anchorRole == "primary_anchor" || anchorRole == "secondary_anchor" {
				bucket = anchorRole
			} else {
				bucket = fileLocateEvidenceBucket(candidate)
			}
		}
		buckets[bucket] = appendIfMissing(buckets[bucket], pathValue)
	}
	if len(buckets) == 0 {
		return nil
	}
	return buckets
}

func prioritizedGroundingCandidates(ranked []*codeSearchCandidate, query, taskType string) []*codeSearchCandidate {
	if len(ranked) == 0 {
		return ranked
	}
	switch taskType {
	case codeSearchTaskFileLocate:
		return prioritizedFileLocateGroundingCandidates(ranked, query)
	case codeSearchTaskExecutionTrace:
	default:
		return ranked
	}
	traceAnchors := deriveExecutionTraceAnchors(query, taskType)
	sourceTerms := codeSearchPathTerms(traceAnchors.SourceQuery)
	targetTerms := codeSearchPathTerms(traceAnchors.TargetQuery)
	out := make([]*codeSearchCandidate, 0, len(ranked))
	seen := map[string]struct{}{}
	var sourceRepresentative *codeSearchCandidate
	var targetRepresentative *codeSearchCandidate
	addBest := func(match func(*codeSearchCandidate) bool, scoreFn func(*codeSearchCandidate) float64) {
		var best *codeSearchCandidate
		bestScore := -1.0
		for _, candidate := range ranked {
			if candidate == nil || candidate.Path == "" {
				continue
			}
			if !match(candidate) {
				continue
			}
			if _, ok := seen[candidate.Path]; ok {
				continue
			}
			score := scoreFn(candidate)
			if best == nil || score > bestScore {
				best = candidate
				bestScore = score
			}
		}
		if best == nil {
			return
		}
		seen[best.Path] = struct{}{}
		out = append(out, best)
	}
	addBest(func(candidate *codeSearchCandidate) bool {
		return candidateHasAnySource(candidate, "trace_source_anchor", "trace_source_repo")
	}, func(candidate *codeSearchCandidate) float64 {
		sourceAffinity := executionTraceCandidateAffinity(candidate, sourceTerms, traceAnchors.SourcePathProbes, traceAnchors.SourceExactProbes)
		targetAffinity := executionTraceCandidateAffinity(candidate, targetTerms, traceAnchors.TargetPathProbes, traceAnchors.TargetExactProbes)
		score := executionTraceAnchorPreference(candidate) + (sourceAffinity * 2.5) - (targetAffinity * 0.75)
		if candidateHasSource(candidate, "execution_graph") {
			score += 0.85
		}
		if candidateHasAnySource(candidate, "trace_target_anchor", "trace_target_repo") {
			score -= 0.6
		}
		return score
	})
	if len(out) > 0 {
		sourceRepresentative = out[len(out)-1]
	}
	addBest(func(candidate *codeSearchCandidate) bool {
		return candidateHasAnySource(candidate, "trace_target_anchor", "trace_target_repo")
	}, func(candidate *codeSearchCandidate) float64 {
		sourceAffinity := executionTraceCandidateAffinity(candidate, sourceTerms, traceAnchors.SourcePathProbes, traceAnchors.SourceExactProbes)
		targetAffinity := executionTraceCandidateAffinity(candidate, targetTerms, traceAnchors.TargetPathProbes, traceAnchors.TargetExactProbes)
		score := executionTraceAnchorPreference(candidate) + (targetAffinity * 2.75) - (sourceAffinity * 0.75)
		if candidateHasSource(candidate, "execution_graph") {
			score += 0.85
		}
		if candidateHasAnySource(candidate, "trace_source_anchor", "trace_source_repo") {
			score -= 0.6
		}
		return score
	})
	if len(out) > 1 {
		targetRepresentative = out[len(out)-1]
	}
	addBest(func(candidate *codeSearchCandidate) bool {
		if !candidateHasAnySource(candidate, "trace_target_anchor", "trace_target_repo") {
			return false
		}
		if candidateHasAnySource(candidate, "trace_source_anchor", "trace_source_repo") {
			return false
		}
		return candidateHasAnySource(candidate, "path_probe", "exact_probe", "search_repo")
	}, func(candidate *codeSearchCandidate) float64 {
		targetAffinity := executionTraceCandidateAffinity(candidate, targetTerms, traceAnchors.TargetPathProbes, traceAnchors.TargetExactProbes)
		score := executionTraceAnchorPreference(candidate) + (targetAffinity * 2.4)
		if candidateHasSource(candidate, "path_probe") {
			score += 0.7
		}
		if candidateHasSource(candidate, "exact_probe") {
			score += 0.5
		}
		return score
	})
	addBest(func(candidate *codeSearchCandidate) bool {
		return candidateHasAnySource(candidate, "bridge_tuple", "execution_bridge")
	}, func(candidate *codeSearchCandidate) float64 {
		score := executionTraceAnchorPreference(candidate)
		if candidateHasSource(candidate, "bridge_tuple") {
			score += 2.0
		}
		if candidateHasSource(candidate, "execution_bridge") {
			score += 1.4
		}
		if candidateHasSource(candidate, "execution_graph") {
			score += 0.6
		}
		return score
	})
	addBest(func(candidate *codeSearchCandidate) bool {
		return executionTraceHubScore(candidate, sourceTerms, targetTerms, traceAnchors.SourcePathProbes, traceAnchors.SourceExactProbes, traceAnchors.TargetPathProbes, traceAnchors.TargetExactProbes) > 0
	}, func(candidate *codeSearchCandidate) float64 {
		return executionTraceHubScore(candidate, sourceTerms, targetTerms, traceAnchors.SourcePathProbes, traceAnchors.SourceExactProbes, traceAnchors.TargetPathProbes, traceAnchors.TargetExactProbes)
	})
	if sourceRepresentative != nil && targetRepresentative != nil {
		addBest(func(candidate *codeSearchCandidate) bool {
			if !candidateHasSource(candidate, "adjacent_impl") {
				return false
			}
			if candidateHasAnySource(candidate, "trace_source_anchor", "trace_source_repo") {
				return false
			}
			return candidateHasAnySource(candidate, "trace_target_anchor", "trace_target_repo", "execution_graph", "search_repo")
		}, func(candidate *codeSearchCandidate) float64 {
			score := executionTraceAnchorPreference(candidate)
			score += groundedRepoGraphNeighborScore(candidate, targetTerms, traceAnchors.TargetPathProbes, traceAnchors.TargetExactProbes)
			score += 1.5
			return score
		})
	}
	addBest(func(candidate *codeSearchCandidate) bool {
		return executionTraceCandidateAffinity(candidate, sourceTerms, traceAnchors.SourcePathProbes, traceAnchors.SourceExactProbes) > 0
	}, func(candidate *codeSearchCandidate) float64 {
		return executionTraceAnchorPreference(candidate) + (executionTraceCandidateAffinity(candidate, sourceTerms, traceAnchors.SourcePathProbes, traceAnchors.SourceExactProbes) * 2.0)
	})
	addBest(func(candidate *codeSearchCandidate) bool {
		return executionTraceCandidateAffinity(candidate, targetTerms, traceAnchors.TargetPathProbes, traceAnchors.TargetExactProbes) > 0
	}, func(candidate *codeSearchCandidate) float64 {
		return executionTraceAnchorPreference(candidate) + (executionTraceCandidateAffinity(candidate, targetTerms, traceAnchors.TargetPathProbes, traceAnchors.TargetExactProbes) * 2.5)
	})
	if clusterRoot := commonExecutionTraceClusterRoot(sourceRepresentative, targetRepresentative); clusterRoot != "" {
		addBest(func(candidate *codeSearchCandidate) bool {
			if candidate == nil || candidate.Path == "" {
				return false
			}
			return strings.HasPrefix(candidate.Path, clusterRoot+"/")
		}, func(candidate *codeSearchCandidate) float64 {
			return executionTraceClusterCompanionScore(candidate, clusterRoot)
		})
	}
	addFirstClass := func(class string) {
		for _, candidate := range ranked {
			if candidate == nil || candidate.Path == "" {
				continue
			}
			if _, ok := seen[candidate.Path]; ok {
				continue
			}
			if classifyExecutionTraceCandidate(candidate) != class {
				continue
			}
			seen[candidate.Path] = struct{}{}
			out = append(out, candidate)
			return
		}
	}
	addFirstClass("exposure")
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		if _, ok := seen[candidate.Path]; ok {
			continue
		}
		seen[candidate.Path] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func prioritizedFileLocateGroundingCandidates(ranked []*codeSearchCandidate, query string) []*codeSearchCandidate {
	if len(ranked) == 0 {
		return ranked
	}
	out := make([]*codeSearchCandidate, 0, len(ranked))
	seen := map[string]struct{}{}
	add := func(candidate *codeSearchCandidate) {
		if candidate == nil || candidate.Path == "" {
			return
		}
		if _, ok := seen[candidate.Path]; ok {
			return
		}
		seen[candidate.Path] = struct{}{}
		out = append(out, candidate)
	}

	var anchor *codeSearchCandidate
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		if !isPreferredFileLocateAnchorCandidate(candidate) {
			continue
		}
		if isLikelyDocumentationPath(candidate.Path) {
			continue
		}
		anchor = candidate
		add(candidate)
		break
	}

	if anchor == nil {
		for _, candidate := range ranked {
			if candidate == nil || candidate.Path == "" {
				continue
			}
			if isLikelyDocumentationPath(candidate.Path) {
				continue
			}
			anchor = candidate
			add(candidate)
			break
		}
	}
	if anchor == nil {
		return ranked
	}

	if companion := bestFileLocateCompanionCandidate(ranked, anchor, seen); companion != nil {
		add(companion)
	}
	addFileLocateDiverseComplements(ranked, anchor, query, seen, &out)
	addPreferredFileLocateRoots(ranked, anchor, seen, &out)
	for _, candidate := range ranked {
		add(candidate)
	}
	return out
}

func addFileLocateDiverseComplements(ranked []*codeSearchCandidate, anchor *codeSearchCandidate, query string, seen map[string]struct{}, out *[]*codeSearchCandidate) {
	if anchor == nil || len(ranked) == 0 {
		return
	}
	queryTerms := codeSearchPathTerms(query)
	exactProbes := codeSearchExactProbes(query)
	usedClasses := map[string]struct{}{}
	limit := 2
	for picks := 0; picks < limit; picks++ {
		var best *codeSearchCandidate
		bestScore := -1.0
		bestClass := ""
		for _, candidate := range ranked {
			if candidate == nil || candidate.Path == "" {
				continue
			}
			if _, ok := seen[candidate.Path]; ok {
				continue
			}
			if isPreferredFileLocateAnchorCandidate(anchor) && isPreferredFileLocateAnchorCandidate(candidate) {
				continue
			}
			class := fileLocateEvidenceClass(candidate)
			score := fileLocateComplementScore(anchor, candidate, queryTerms, exactProbes)
			if class != "" {
				if _, ok := usedClasses[class]; ok {
					score -= 0.75
				} else {
					score += 0.4
				}
			}
			if best == nil || score > bestScore || (score == bestScore && candidate.Path < best.Path) {
				best = candidate
				bestScore = score
				bestClass = class
			}
		}
		if best == nil {
			return
		}
		seen[best.Path] = struct{}{}
		*out = append(*out, best)
		if bestClass != "" {
			usedClasses[bestClass] = struct{}{}
		}
	}
}

func bestFileLocateCompanionCandidate(ranked []*codeSearchCandidate, anchor *codeSearchCandidate, seen map[string]struct{}) *codeSearchCandidate {
	if anchor == nil || anchor.Path == "" {
		return nil
	}
	if isPreferredFileLocateAnchorCandidate(anchor) {
		return nil
	}
	anchorDir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(anchor.Path)))
	if anchorDir == "" || anchorDir == "." {
		return nil
	}
	var best *codeSearchCandidate
	bestScore := -1.0
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		if _, ok := seen[candidate.Path]; ok {
			continue
		}
		if strings.TrimSpace(filepath.ToSlash(filepath.Dir(candidate.Path))) != anchorDir {
			continue
		}
		if !isLikelyDeclarativeCompanionPath(candidate.Path) {
			continue
		}
		score := candidate.Support
		if candidateHasSource(candidate, "path_probe") {
			score += 1.0
		}
		if candidateHasSource(candidate, "semantic_bundle_related") {
			score += 0.8
		}
		if candidateHasSource(candidate, "search_repo") {
			score += 0.25
		}
		if best == nil || score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func fileLocateEvidenceClass(candidate *codeSearchCandidate) string {
	if candidate == nil || candidate.Path == "" {
		return ""
	}
	switch {
	case isLikelyDeclarativeCompanionPath(candidate.Path):
		return "declarative"
	case candidateHasAnySource(candidate, "search_repo", "exact_probe", "path_probe"):
		return "repo"
	case candidateHasAnySource(candidate, "semantic_search_code", "semantic_bundle_primary", "semantic_bundle_related", "repo_motif_anchor_support"):
		return "semantic"
	default:
		return "other"
	}
}

func fileLocateAnchorRole(candidate *codeSearchCandidate) string {
	if candidate == nil {
		return ""
	}
	switch {
	case candidateHasAnySource(candidate, "aca_route_package_primary_anchor", "aca_route_infra_primary_anchor"):
		return "primary_anchor"
	case candidateHasAnySource(candidate, "aca_route_package_secondary_anchor", "aca_route_infra_secondary_anchor"):
		return "secondary_anchor"
	default:
		return ""
	}
}

func inferFileLocateAnchorRoles(ranked []*codeSearchCandidate, selectedPaths map[string]int) map[string]string {
	if len(ranked) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		if role := fileLocateAnchorRole(candidate); role != "" {
			out[candidate.Path] = role
		}
	}
	var primaryPath string
	primaryRank := 0
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		if _, ok := out[candidate.Path]; ok {
			continue
		}
		if !candidateHasSource(candidate, "aca_route_infra_exact") {
			continue
		}
		rank, ok := selectedPaths[candidate.Path]
		if !ok {
			continue
		}
		if primaryPath == "" || rank < primaryRank {
			primaryPath = candidate.Path
			primaryRank = rank
		}
	}
	if primaryPath == "" {
		for _, candidate := range ranked {
			if candidate == nil || candidate.Path == "" {
				continue
			}
			if _, ok := out[candidate.Path]; ok {
				continue
			}
			if candidateHasSource(candidate, "aca_route_infra_exact") {
				primaryPath = candidate.Path
				break
			}
		}
	}
	if primaryPath == "" {
		return out
	}
	out[primaryPath] = "primary_anchor"
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" || candidate.Path == primaryPath {
			continue
		}
		if _, ok := out[candidate.Path]; ok {
			continue
		}
		if candidateHasSource(candidate, "aca_route_infra_exact") {
			out[candidate.Path] = "secondary_anchor"
		}
	}
	return out
}

func fileLocateEvidenceBucket(candidate *codeSearchCandidate) string {
	switch fileLocateAnchorRole(candidate) {
	case "primary_anchor":
		return "primary_anchor"
	case "secondary_anchor":
		return "secondary_anchor"
	}
	switch fileLocateEvidenceClass(candidate) {
	case "repo":
		return "repo_evidence"
	case "declarative":
		return "declarative_companion"
	case "semantic":
		return "semantic_companion"
	default:
		return "other"
	}
}

func fileLocateComplementScore(anchor, candidate *codeSearchCandidate, queryTerms, exactProbes []string) float64 {
	if candidate == nil || candidate.Path == "" {
		return -1
	}
	score := candidate.Support
	score += codeSearchPathTermScore(candidate.Path, queryTerms) * 1.1
	if isLikelyDeclarativeCompanionPath(candidate.Path) {
		score += 0.55
	}
	if candidateHasSource(candidate, "search_repo") {
		score += 0.3
	}
	if candidateHasSource(candidate, "exact_probe") {
		score += 0.2
	}
	if candidateHasSource(candidate, "semantic_search_code") {
		score += 0.15
	}
	if anchor != nil && anchor.Path != "" {
		if strings.TrimSpace(filepath.ToSlash(filepath.Dir(anchor.Path))) == strings.TrimSpace(filepath.ToSlash(filepath.Dir(candidate.Path))) {
			score -= 0.5
		}
		if fileLocateModuleFamilyKey(anchor) == fileLocateModuleFamilyKey(candidate) {
			score -= 0.8
		}
	}
	score -= codeSearchSubtypePenalty(candidate, exactProbes) * 1.1
	return score
}

func addPreferredFileLocateRoots(ranked []*codeSearchCandidate, anchor *codeSearchCandidate, seen map[string]struct{}, out *[]*codeSearchCandidate) {
	preferSingleAnchor := isPreferredFileLocateAnchorCandidate(anchor)
	candidates := make([]*codeSearchCandidate, 0, len(ranked))
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		if _, ok := seen[candidate.Path]; ok {
			continue
		}
		if !isLikelyTextCodeFile(candidate.Path) || isLikelyDocumentationPath(candidate.Path) {
			continue
		}
		if preferSingleAnchor && isPreferredFileLocateAnchorCandidate(candidate) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		si := fileLocateRootPreferenceScore(candidates[i])
		sj := fileLocateRootPreferenceScore(candidates[j])
		di := strings.Count(normalizeCodeSearchPath(candidates[i].Path), "/")
		dj := strings.Count(normalizeCodeSearchPath(candidates[j].Path), "/")
		if di != dj {
			return di < dj
		}
		if si == sj {
			return candidates[i].Path < candidates[j].Path
		}
		return si > sj
	})
	limit := minInt(2, len(candidates))
	for _, candidate := range candidates[:limit] {
		if _, ok := seen[candidate.Path]; ok {
			continue
		}
		seen[candidate.Path] = struct{}{}
		*out = append(*out, candidate)
	}
}

func fileLocateModuleFamilyKey(candidate *codeSearchCandidate) string {
	if candidate == nil || candidate.Path == "" {
		return ""
	}
	pathValue := normalizeCodeSearchPath(candidate.Path)
	if pathValue == "" {
		return ""
	}
	dir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(pathValue)))
	base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(pathValue), filepath.Ext(pathValue)))
	if dir == "" || dir == "." || base == "" {
		return ""
	}
	base = strings.TrimSuffix(base, "_test")
	return dir + "::" + base
}

func fileLocateRootPreferenceScore(candidate *codeSearchCandidate) float64 {
	if candidate == nil || candidate.Path == "" {
		return -1
	}
	pathValue := normalizeCodeSearchPath(candidate.Path)
	score := candidate.Support
	score -= float64(strings.Count(pathValue, "/")) * 0.2
	if candidateHasSource(candidate, "semantic_search_code") {
		score += 0.4
	}
	if candidateHasSource(candidate, "search_repo") {
		score += 0.25
	}
	if candidateHasSource(candidate, "exact_probe") {
		score += 0.15
	}
	return score
}

func executionTraceHubScore(candidate *codeSearchCandidate, sourceTerms, targetTerms, sourcePathProbes, sourceExactProbes, targetPathProbes, targetExactProbes []string) float64 {
	if candidate == nil {
		return 0
	}
	sourceAffinity := executionTraceCandidateAffinity(candidate, sourceTerms, sourcePathProbes, sourceExactProbes)
	targetAffinity := executionTraceCandidateAffinity(candidate, targetTerms, targetPathProbes, targetExactProbes)
	hasSourceSide := sourceAffinity > 0 || candidateHasAnySource(candidate, "trace_source_anchor", "trace_source_repo")
	hasTargetSide := targetAffinity > 0 || candidateHasAnySource(candidate, "trace_target_anchor", "trace_target_repo")
	if !hasSourceSide || !hasTargetSide {
		return 0
	}
	score := executionTraceAnchorPreference(candidate) + sourceAffinity + targetAffinity
	if candidateHasSource(candidate, "execution_graph") {
		score += 1.4
	}
	if candidateHasSource(candidate, "search_repo") {
		score += 0.4
	}
	if candidateHasAnySource(candidate, "trace_source_anchor", "trace_source_repo") {
		score += 0.3
	}
	if candidateHasAnySource(candidate, "trace_target_anchor", "trace_target_repo") {
		score += 0.3
	}
	return score
}

func promoteGroundedRepoGraphNeighbors(queue []*codeSearchCandidate, insertAt int, rankedByPath map[string]*codeSearchCandidate, selectedPaths map[string]int, neighborPaths []string, query, taskType string) []*codeSearchCandidate {
	if len(queue) == 0 || len(neighborPaths) == 0 || insertAt >= len(queue) {
		return queue
	}
	pathTerms := codeSearchPathTerms(query)
	exactProbes := codeSearchTaskExactProbes(query, taskType)
	pathProbes := codeSearchTaskPathProbes(query, taskType, exactProbes)
	seen := map[string]struct{}{}
	candidates := make([]*codeSearchCandidate, 0, len(neighborPaths))
	for _, pathValue := range neighborPaths {
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" {
			continue
		}
		if _, ok := seen[pathValue]; ok {
			continue
		}
		seen[pathValue] = struct{}{}
		if _, ok := selectedPaths[pathValue]; ok {
			continue
		}
		candidate := rankedByPath[pathValue]
		if candidate == nil || candidate.Path == "" {
			continue
		}
		if !isLikelyTextCodeFile(candidate.Path) || isLikelyDocumentationPath(candidate.Path) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return queue
	}
	sort.Slice(candidates, func(i, j int) bool {
		si := groundedRepoGraphNeighborScore(candidates[i], pathTerms, pathProbes, exactProbes)
		sj := groundedRepoGraphNeighborScore(candidates[j], pathTerms, pathProbes, exactProbes)
		if si == sj {
			return candidates[i].Path < candidates[j].Path
		}
		return si > sj
	})
	limit := minInt(2, len(candidates))
	for _, candidate := range candidates[:limit] {
		queue = moveCandidateEarlier(queue, insertAt, candidate.Path)
		insertAt++
	}
	return queue
}

func groundedRepoGraphNeighborScore(candidate *codeSearchCandidate, pathTerms, pathProbes, exactProbes []string) float64 {
	if candidate == nil {
		return -1
	}
	score := candidate.Support
	score += codeSearchPathTermScore(candidate.Path, pathTerms) * 0.8
	score += codeSearchPathTermScore(filepath.Base(candidate.Path), pathTerms) * 0.8
	score += codeSearchBasenameProbeScore(candidate.Path, pathProbes, exactProbes) * 1.1
	score += codeSearchExactSymbolScore(candidate, exactProbes) * 0.8
	score -= codeSearchSubtypePenalty(candidate, exactProbes) * 0.5
	if candidateHasSource(candidate, "execution_graph") {
		score += 1.0
	}
	if candidateHasSource(candidate, "search_repo") {
		score += 0.35
	}
	score += codeSearchFileWeight(candidate.Path)
	if isLikelyDocumentationPath(candidate.Path) {
		score -= 2.0
	}
	return score
}

func moveCandidateEarlier(queue []*codeSearchCandidate, insertAt int, pathValue string) []*codeSearchCandidate {
	pathValue = normalizeCodeSearchPath(pathValue)
	if pathValue == "" || insertAt < 0 || insertAt >= len(queue) {
		return queue
	}
	idx := -1
	for i, candidate := range queue {
		if candidate == nil {
			continue
		}
		if normalizeCodeSearchPath(candidate.Path) == pathValue {
			idx = i
			break
		}
	}
	if idx < 0 || idx <= insertAt {
		return queue
	}
	candidate := queue[idx]
	copy(queue[insertAt+1:idx+1], queue[insertAt:idx])
	queue[insertAt] = candidate
	return queue
}

func applyCodeSearchResolverHintsToQueue(queue []*codeSearchCandidate, preferPaths, demotePaths []string, ambiguities []map[string]any, maxFiles int) []*codeSearchCandidate {
	if len(queue) == 0 {
		return queue
	}
	if maxFiles <= 0 {
		maxFiles = 4
	}
	alternateToSelected := map[string]string{}
	selectedSet := map[string]struct{}{}
	for _, item := range ambiguities {
		selectedPath := normalizeCodeSearchPath(stringValue(item["selected_path"]))
		alternatePath := normalizeCodeSearchPath(stringValue(item["alternate_path"]))
		if selectedPath == "" || alternatePath == "" {
			continue
		}
		alternateToSelected[alternatePath] = selectedPath
		selectedSet[selectedPath] = struct{}{}
	}
	insertAt := 0
	for _, pathValue := range preferPaths {
		if insertAt >= len(queue) {
			break
		}
		pathValue = normalizeCodeSearchPath(pathValue)
		if selectedPath, ok := alternateToSelected[pathValue]; ok {
			queue = moveCandidateEarlier(queue, insertAt, pathValue)
			queue = demoteCandidateWithinTopWindow(queue, selectedPath, maxFiles)
		} else {
			queue = moveCandidateEarlier(queue, insertAt, pathValue)
		}
		insertAt++
	}
	for _, pathValue := range demotePaths {
		pathValue = normalizeCodeSearchPath(pathValue)
		if pathValue == "" {
			continue
		}
		if _, ok := selectedSet[pathValue]; !ok {
			continue
		}
		queue = demoteCandidateWithinTopWindow(queue, pathValue, maxFiles)
	}
	return queue
}

func demoteCandidateWithinTopWindow(queue []*codeSearchCandidate, pathValue string, maxFiles int) []*codeSearchCandidate {
	pathValue = normalizeCodeSearchPath(pathValue)
	if pathValue == "" || len(queue) == 0 {
		return queue
	}
	if maxFiles <= 0 {
		maxFiles = 4
	}
	if maxFiles > len(queue) {
		maxFiles = len(queue)
	}
	idx := -1
	for i, candidate := range queue {
		if candidate == nil {
			continue
		}
		if normalizeCodeSearchPath(candidate.Path) == pathValue {
			idx = i
			break
		}
	}
	if idx < 0 || idx >= maxFiles {
		return queue
	}
	if idx == maxFiles-1 {
		return queue
	}
	candidate := queue[idx]
	copy(queue[idx:maxFiles-1], queue[idx+1:maxFiles])
	queue[maxFiles-1] = candidate
	return queue
}

func executionTraceAnchorPreference(candidate *codeSearchCandidate) float64 {
	if candidate == nil {
		return -1
	}
	score := 0.0
	if !candidateHasSource(candidate, "execution_bridge") {
		score += 2.0
	} else {
		score += 0.75
	}
	if candidateHasSource(candidate, "path_probe") {
		score += 1.0
	}
	if candidateHasSource(candidate, "search_repo") {
		score += 0.5
	}
	if strings.HasSuffix(strings.ToLower(candidate.Path), "_test.go") {
		score -= 2.0
	}
	switch strings.ToLower(filepath.Ext(candidate.Path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".ex", ".exs":
		score += 0.5
	case ".yaml", ".yml", ".json", ".toml":
		score -= 1.0
	}
	return score
}

func commonExecutionTraceClusterRoot(source, target *codeSearchCandidate) string {
	if source == nil || target == nil {
		return ""
	}
	sourceDir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(source.Path)))
	targetDir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(target.Path)))
	if sourceDir == "" || sourceDir == "." || targetDir == "" || targetDir == "." {
		return ""
	}
	sourceParts := strings.Split(sourceDir, "/")
	targetParts := strings.Split(targetDir, "/")
	shared := make([]string, 0, minInt(len(sourceParts), len(targetParts)))
	for i := 0; i < len(sourceParts) && i < len(targetParts); i++ {
		if sourceParts[i] != targetParts[i] {
			break
		}
		shared = append(shared, sourceParts[i])
	}
	if len(shared) < 2 {
		return ""
	}
	root := strings.Join(shared, "/")
	if root == "" || root == "." {
		return ""
	}
	if isGenericExecutionTraceClusterDir(filepath.Base(root)) {
		parent := strings.TrimSpace(filepath.ToSlash(filepath.Dir(root)))
		if parent != "" && parent != "." {
			root = parent
		}
	}
	return strings.TrimSpace(root)
}

func isGenericExecutionTraceClusterDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "src", "lib", "app", "apps", "cmd", "internal", "pkg":
		return true
	default:
		return false
	}
}

func executionTraceClusterCompanionScore(candidate *codeSearchCandidate, clusterRoot string) float64 {
	if candidate == nil || candidate.Path == "" || clusterRoot == "" {
		return -1
	}
	score := executionTraceAnchorPreference(candidate)
	if candidateHasSource(candidate, "execution_graph") {
		score += 0.9
	}
	if candidateHasSource(candidate, "search_repo") {
		score += 0.35
	}
	dir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(candidate.Path)))
	if dir == clusterRoot {
		score += 1.25
	}
	trimmed := strings.TrimPrefix(strings.TrimPrefix(candidate.Path, clusterRoot), "/")
	if trimmed != "" {
		score -= float64(strings.Count(trimmed, "/")) * 0.2
	}
	return score
}

func buildExecutionTraceBuckets(ranked []*codeSearchCandidate, selectedPaths map[string]int) map[string][]string {
	buckets := map[string][]string{
		"direct_dispatch_files":    {},
		"exposure_files":           {},
		"structural_support_files": {},
		"registration_files":       {},
	}
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		if _, ok := selectedPaths[candidate.Path]; !ok {
			continue
		}
		switch classifyExecutionTraceCandidate(candidate) {
		case "direct_dispatch":
			buckets["direct_dispatch_files"] = appendIfMissing(buckets["direct_dispatch_files"], candidate.Path)
		case "exposure":
			buckets["exposure_files"] = appendIfMissing(buckets["exposure_files"], candidate.Path)
		case "structural_support":
			buckets["structural_support_files"] = appendIfMissing(buckets["structural_support_files"], candidate.Path)
		case "registration":
			buckets["registration_files"] = appendIfMissing(buckets["registration_files"], candidate.Path)
		}
	}
	return buckets
}

func classifyExecutionTraceCandidate(candidate *codeSearchCandidate) string {
	if candidate == nil {
		return ""
	}
	switch {
	case candidateHasSource(candidate, "registration_trace"):
		return "registration"
	case candidateHasSource(candidate, "bridge_tuple"):
		return "direct_dispatch"
	case candidateHasSource(candidate, "path_probe"):
		return "direct_dispatch"
	case candidateHasSource(candidate, "execution_graph"):
		return "structural_support"
	case candidateHasSource(candidate, "execution_bridge"):
		return "exposure"
	default:
		return ""
	}
}

func uniquePositiveInts(in []int) []int {
	if len(in) == 0 {
		return nil
	}
	seen := map[int]struct{}{}
	out := make([]int, 0, len(in))
	for _, item := range in {
		if item <= 0 {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Ints(out)
	return out
}

type groundedSymbolHit struct {
	Name string
	Line int
}

func codeSearchSymbolProbes(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	for _, probe := range codeSearchExactProbes(query) {
		probe = strings.TrimSpace(probe)
		if !isLikelyGroundedSymbolName(probe) {
			continue
		}
		if _, ok := seen[probe]; ok {
			continue
		}
		seen[probe] = struct{}{}
		out = append(out, probe)
	}
	return out
}

func extractGroundedEvidenceSymbols(pathValue, content string, startLine, preferredLine int, preferred, fallback []string, taskType string) []codeSearchEvidenceSymbol {
	pathValue = normalizeCodeSearchPath(pathValue)
	content = strings.TrimSpace(content)
	if pathValue == "" || content == "" {
		return nil
	}
	decls := scanGroundedSymbolDeclarations(content, startLine)
	out := make([]codeSearchEvidenceSymbol, 0, 4)
	seen := map[string]struct{}{}
	add := func(name string, line int) {
		name = strings.TrimSpace(name)
		if !isLikelyGroundedSymbolName(name) {
			return
		}
		key := pathValue + "|" + name
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, codeSearchEvidenceSymbol{
			Path:   pathValue,
			Symbol: name,
			Line:   line,
		})
	}

	for _, probe := range preferred {
		if hit, ok := bestGroundedSymbolHit(decls, probe, preferredLine); ok {
			add(hit.Name, hit.Line)
			continue
		}
		if line := findGroundedSymbolLine(content, startLine, probe); line > 0 {
			add(probe, line)
		}
	}
	for _, item := range fallback {
		name := strings.TrimSpace(item)
		if !isLikelyGroundedSymbolName(name) {
			continue
		}
		if hit, ok := bestGroundedSymbolHit(decls, name, preferredLine); ok {
			add(hit.Name, hit.Line)
			continue
		}
		if line := findGroundedSymbolLine(content, startLine, name); line > 0 {
			add(name, line)
		}
	}
	if taskType == codeSearchTaskSymbolInspect && len(out) == 0 {
		for _, hit := range rankedGroundedSymbolHits(decls, preferredLine) {
			add(hit.Name, hit.Line)
			if len(out) >= 4 {
				break
			}
		}
	}
	return out
}

func scanGroundedSymbolDeclarations(content string, startLine int) []groundedSymbolHit {
	lines := strings.Split(content, "\n")
	hits := make([]groundedSymbolHit, 0, 8)
	for idx, line := range lines {
		lineNo := startLine + idx
		if lineNo <= 0 {
			lineNo = idx + 1
		}
		for _, pattern := range []*regexp.Regexp{groundedTypeSymbolPattern, groundedFuncSymbolPattern, groundedVarConstSymbolPattern} {
			matches := pattern.FindStringSubmatch(line)
			if len(matches) < 2 {
				continue
			}
			name := strings.TrimSpace(matches[1])
			if !isLikelyGroundedSymbolName(name) {
				continue
			}
			hits = append(hits, groundedSymbolHit{Name: name, Line: lineNo})
			break
		}
	}
	return dedupeGroundedSymbolHits(hits)
}

func dedupeGroundedSymbolHits(in []groundedSymbolHit) []groundedSymbolHit {
	seen := map[string]struct{}{}
	out := make([]groundedSymbolHit, 0, len(in))
	for _, item := range in {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		key := item.Name + "|" + fmt.Sprintf("%d", item.Line)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func bestGroundedSymbolHit(hits []groundedSymbolHit, want string, preferredLine int) (groundedSymbolHit, bool) {
	want = strings.TrimSpace(want)
	if want == "" {
		return groundedSymbolHit{}, false
	}
	candidates := make([]groundedSymbolHit, 0, len(hits))
	for _, hit := range hits {
		switch {
		case hit.Name == want:
			candidates = append(candidates, hit)
		case strings.HasSuffix(hit.Name, "."+want):
			candidates = append(candidates, hit)
		}
	}
	if len(candidates) == 0 {
		return groundedSymbolHit{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		return groundedSymbolDistance(candidates[i].Line, preferredLine) < groundedSymbolDistance(candidates[j].Line, preferredLine)
	})
	return candidates[0], true
}

func rankedGroundedSymbolHits(hits []groundedSymbolHit, preferredLine int) []groundedSymbolHit {
	out := append([]groundedSymbolHit(nil), hits...)
	sort.Slice(out, func(i, j int) bool {
		if groundedSymbolDistance(out[i].Line, preferredLine) == groundedSymbolDistance(out[j].Line, preferredLine) {
			return out[i].Line < out[j].Line
		}
		return groundedSymbolDistance(out[i].Line, preferredLine) < groundedSymbolDistance(out[j].Line, preferredLine)
	})
	return out
}

func groundedSymbolDistance(line, preferredLine int) int {
	if preferredLine <= 0 || line <= 0 {
		return 0
	}
	if line > preferredLine {
		return line - preferredLine
	}
	return preferredLine - line
}

func findGroundedSymbolLine(content string, startLine int, symbol string) int {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return 0
	}
	lines := strings.Split(content, "\n")
	for idx, line := range lines {
		if strings.Contains(line, symbol) {
			lineNo := startLine + idx
			if lineNo <= 0 {
				lineNo = idx + 1
			}
			return lineNo
		}
	}
	return 0
}

func isLikelyGroundedSymbolName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, " ") {
		return false
	}
	lower := strings.ToLower(name)
	for _, suffix := range []string{".go", ".ts", ".tsx", ".js", ".jsx", ".md", ".txt"} {
		if strings.HasSuffix(lower, suffix) {
			return false
		}
	}
	for i, r := range name {
		switch {
		case r == '_' || r == '.':
			continue
		case r >= 'a' && r <= 'z':
			continue
		case r >= 'A' && r <= 'Z':
			continue
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
			continue
		default:
			return false
		}
	}
	return true
}

func sortedSourceKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func appendIfMissing(in []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return in
	}
	for _, existing := range in {
		if existing == value {
			return in
		}
	}
	return append(in, value)
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func stringSliceValue(v any) []string {
	switch value := v.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s := stringValue(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func intValue(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case float32:
		return int(value)
	default:
		return 0
	}
}

type codeSearchExactHit struct {
	Path string
	Line int
}

type codeSearchPathHit struct {
	Path string
}

type codeSearchExecutionBridgeHit struct {
	Path string
	Line int
}

type codeSearchImpactHit struct {
	Path       string
	Line       int
	Symbol     string
	NodeID     string
	SourcePath string
	EdgeTypes  []repoindex.EdgeType
}

type codeSearchBridgeTuple struct {
	ReceiverType string
	Method       string
	Literal      string
	Query        string
}

func exactCodeProbeSearch(workspaceRoot, probe string, limit int) ([]codeSearchExactHit, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	probe = strings.TrimSpace(probe)
	if workspaceRoot == "" || probe == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	rawCap := limit * 32
	if rawCap < 32 {
		rawCap = 32
	}
	hits := make([]codeSearchExactHit, 0, limit)
	stop := fmt.Errorf("stop")
	err := filepath.WalkDir(workspaceRoot, func(current string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".idea", ".vscode", "tmp", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if len(hits) >= rawCap {
			return stop
		}
		if !isLikelyTextCodeFile(current) {
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
		index := strings.Index(string(body), probe)
		if index < 0 {
			return nil
		}
		rel, relErr := filepath.Rel(workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		hits = append(hits, codeSearchExactHit{
			Path: filepath.ToSlash(rel),
			Line: 1 + strings.Count(string(body[:index]), "\n"),
		})
		if len(hits) >= rawCap {
			return stop
		}
		return nil
	})
	if err != nil && err != stop {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		wi := codeSearchFileWeight(hits[i].Path)
		wj := codeSearchFileWeight(hits[j].Path)
		if wi == wj {
			return hits[i].Path < hits[j].Path
		}
		return wi > wj
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func codeSearchPathProbeSearch(workspaceRoot, probe string, limit int) ([]codeSearchPathHit, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	probe = strings.ToLower(strings.TrimSpace(probe))
	if workspaceRoot == "" || probe == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	rawCap := limit * 32
	if rawCap < 32 {
		rawCap = 32
	}
	hits := make([]codeSearchPathHit, 0, limit)
	stop := fmt.Errorf("stop")
	err := filepath.WalkDir(workspaceRoot, func(current string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".idea", ".vscode", "tmp", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if len(hits) >= rawCap {
			return stop
		}
		rel, relErr := filepath.Rel(workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		if !isLikelyTextCodeFile(rel) {
			return nil
		}
		normalized := strings.ToLower(filepath.ToSlash(rel))
		if !strings.Contains(normalized, probe) {
			return nil
		}
		hits = append(hits, codeSearchPathHit{Path: filepath.ToSlash(rel)})
		if len(hits) >= rawCap {
			return stop
		}
		return nil
	})
	if err != nil && err != stop {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		wi := codeSearchFileWeight(hits[i].Path)
		wj := codeSearchFileWeight(hits[j].Path)
		if wi == wj {
			return hits[i].Path < hits[j].Path
		}
		return wi > wj
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func executionBridgeSearch(workspaceRoot, probe string, limit int) ([]codeSearchExecutionBridgeHit, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	probe = strings.TrimSpace(probe)
	if workspaceRoot == "" || probe == "" || limit <= 0 {
		return nil, nil
	}
	patterns := []string{
		`case "` + probe + `"`,
		`Name: "` + probe + `"`,
		probe + `(`,
		`"` + probe + `"`,
	}
	rawCap := limit * 32
	if rawCap < 32 {
		rawCap = 32
	}
	hits := make([]codeSearchExecutionBridgeHit, 0, limit)
	stop := fmt.Errorf("stop")
	err := filepath.WalkDir(workspaceRoot, func(current string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".idea", ".vscode", "tmp", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(current) != ".go" {
			return nil
		}
		body, readErr := os.ReadFile(current)
		if readErr != nil {
			return nil
		}
		text := string(body)
		bestIndex := -1
		for _, pattern := range patterns {
			if idx := strings.Index(text, pattern); idx >= 0 && (bestIndex == -1 || idx < bestIndex) {
				bestIndex = idx
			}
		}
		if bestIndex < 0 {
			return nil
		}
		rel, relErr := filepath.Rel(workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		hits = append(hits, codeSearchExecutionBridgeHit{
			Path: filepath.ToSlash(rel),
			Line: 1 + strings.Count(text[:bestIndex], "\n"),
		})
		if len(hits) >= rawCap {
			return stop
		}
		return nil
	})
	if err != nil && err != stop {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		wi := codeSearchExecutionBridgeWeight(hits[i].Path)
		wj := codeSearchExecutionBridgeWeight(hits[j].Path)
		if wi == wj {
			return hits[i].Path < hits[j].Path
		}
		return wi > wj
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func codeSearchProtocolImplementationPaths(workspaceRoot, sourcePath, content string, limit int) []string {
	if strings.TrimSpace(workspaceRoot) == "" || strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(content) == "" || limit <= 0 {
		return nil
	}
	protocols := make([]string, 0, 2)
	seenProtocols := map[string]struct{}{}
	for _, match := range elixirProtocolPattern.FindAllStringSubmatch(content, -1) {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" {
			continue
		}
		if _, ok := seenProtocols[name]; ok {
			continue
		}
		seenProtocols[name] = struct{}{}
		protocols = append(protocols, name)
	}
	if len(protocols) == 0 {
		return nil
	}
	dir := filepath.Join(workspaceRoot, filepath.FromSlash(filepath.Dir(sourcePath)))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	sourcePath = normalizeCodeSearchPath(sourcePath)
	out := make([]string, 0, limit)
	seenPaths := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		rel := normalizeCodeSearchPath(filepath.ToSlash(filepath.Join(filepath.Dir(sourcePath), entry.Name())))
		if rel == "" || rel == sourcePath {
			continue
		}
		if !isLikelyTextCodeFile(rel) || isLikelyDocumentationPath(rel) {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			continue
		}
		text := string(body)
		for _, protocol := range protocols {
			if !strings.Contains(text, "defimpl "+protocol) {
				continue
			}
			if _, ok := seenPaths[rel]; ok {
				break
			}
			seenPaths[rel] = struct{}{}
			out = append(out, rel)
			break
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (a *ReadOnlyAdapter) codeSearchBridgeTupleAugment(ctx context.Context, querySvc *repoquery.QueryService, candidates map[string]*codeSearchCandidate, query string, limit int) ([]codeSearchImpactHit, []string, []string) {
	if querySvc == nil || limit <= 0 {
		return nil, nil, nil
	}
	ranked := rankCodeSearchCandidates(candidates, query, codeSearchTaskExecutionTrace, minInt(limit, 3))
	queries := make([]string, 0, 6)
	seenQueries := map[string]struct{}{}
	gaps := make([]string, 0, 4)
	addQuery := func(value string) {
		value = normalizeRepoSearchProbe(value)
		if value == "" {
			return
		}
		if _, ok := seenQueries[value]; ok {
			return
		}
		seenQueries[value] = struct{}{}
		queries = append(queries, value)
	}
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		stageStart := time.Now()
		loaded, err := a.loadFile(mustJSON(map[string]any{
			"path": candidate.Path,
		}))
		a.telemetry.record("bridge_seed_load", time.Since(stageStart), nil)
		if err != nil {
			gaps = append(gaps, fmt.Sprintf("bridge seed load failed for %s: %v", candidate.Path, err))
			continue
		}
		content := stringValue(loaded["content"])
		for _, tuple := range extractCodeSearchBridgeTuples(content) {
			addQuery(tuple.Query)
		}
	}
	if len(queries) == 0 {
		return nil, nil, uniqueStrings(gaps)
	}
	hits := make([]codeSearchImpactHit, 0, limit*2)
	for _, queryValue := range queries {
		stageStart := time.Now()
		searchOut, err := querySvc.SearchWithProjection(ctx, repoquery.SearchRequest{
			Query: queryValue,
			Limit: limit,
		})
		a.telemetry.record("bridge_tuple_search", time.Since(stageStart), nil)
		if err != nil {
			gaps = append(gaps, fmt.Sprintf("bridge tuple search failed for %s: %v", queryValue, err))
			continue
		}
		for i, anchor := range searchOut.Anchors {
			nodeID := ""
			if i < len(searchOut.Nodes) {
				nodeID = strings.TrimSpace(searchOut.Nodes[i].ID)
			}
			hits = append(hits, codeSearchImpactHit{
				Path:       normalizeCodeSearchPath(anchor.Path),
				Line:       anchor.LineHint,
				Symbol:     strings.TrimSpace(anchor.SymbolName),
				NodeID:     nodeID,
				SourcePath: queryValue,
			})
		}
	}
	return dedupeCodeSearchImpactHits(hits), queries, uniqueStrings(gaps)
}

func extractCodeSearchBridgeTuples(content string) []codeSearchBridgeTuple {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	varToType := map[string]string{}
	for _, match := range bridgeCtorAssignPattern.FindAllStringSubmatch(content, -1) {
		if len(match) < 3 {
			continue
		}
		varName := strings.TrimSpace(match[1])
		receiverType := strings.TrimSpace(match[2])
		if varName == "" || receiverType == "" {
			continue
		}
		varToType[varName] = receiverType
	}
	seen := map[string]struct{}{}
	out := make([]codeSearchBridgeTuple, 0, len(varToType))
	for _, match := range bridgeMethodLiteralPattern.FindAllStringSubmatch(content, -1) {
		if len(match) < 4 {
			continue
		}
		receiverVar := strings.TrimSpace(match[1])
		method := strings.TrimSpace(match[2])
		literal := strings.TrimSpace(match[3])
		receiverType := strings.TrimSpace(varToType[receiverVar])
		if receiverType == "" || method == "" || literal == "" {
			continue
		}
		query := receiverType + " " + method + " " + literal
		if _, ok := seen[query]; ok {
			continue
		}
		seen[query] = struct{}{}
		out = append(out, codeSearchBridgeTuple{
			ReceiverType: receiverType,
			Method:       method,
			Literal:      literal,
			Query:        query,
		})
	}
	return out
}

func codeSearchImpactAugment(ctx context.Context, querySvc *repoquery.QueryService, candidates map[string]*codeSearchCandidate, limit int) ([]codeSearchImpactHit, []repoquery.Anchor, []string) {
	if querySvc == nil || limit <= 0 {
		return nil, nil, nil
	}
	ranked := rankCodeSearchCandidates(candidates, "", codeSearchTaskFileLocate, minInt(limit, 4))
	hits := make([]codeSearchImpactHit, 0, limit*2)
	anchors := make([]repoquery.Anchor, 0, limit*2)
	gaps := make([]string, 0, 4)
	for _, candidate := range ranked {
		if candidate == nil {
			continue
		}
		for _, seed := range uniqueStrings(candidate.RepoNodeIDs) {
			if strings.TrimSpace(seed) == "" {
				continue
			}
			for _, direction := range []repoindex.Direction{repoindex.DirOut, repoindex.DirIn} {
				expanded, err := querySvc.ExpandWithProjection(ctx, repoquery.ExpandRequest{
					Seeds:      []string{seed},
					EdgeTypes:  repoindex.EdgeSetStructural,
					Direction:  direction,
					Depth:      2,
					Budget:     limit * 3,
					PerNodeCap: 20,
				})
				if err != nil {
					gaps = append(gaps, fmt.Sprintf("impact graph failed for %s: %v", seed, err))
					continue
				}
				for i, anchor := range expanded.Anchors {
					nodeID := ""
					if i < len(expanded.Result.Nodes) {
						nodeID = strings.TrimSpace(expanded.Result.Nodes[i].ID)
					}
					anchors = append(anchors, anchor)
					hits = append(hits, codeSearchImpactHit{
						Path:       normalizeCodeSearchPath(anchor.Path),
						Line:       anchor.LineHint,
						Symbol:     strings.TrimSpace(anchor.SymbolName),
						NodeID:     nodeID,
						SourcePath: candidate.Path,
					})
				}
			}
			break
		}
	}
	return dedupeCodeSearchImpactHits(hits), anchors, uniqueStrings(gaps)
}

func codeSearchExecutionTraceAugment(ctx context.Context, querySvc *repoquery.QueryService, candidates map[string]*codeSearchCandidate, query string, limit int) ([]codeSearchImpactHit, []repoquery.Anchor, []string) {
	if querySvc == nil || limit <= 0 {
		return nil, nil, nil
	}
	ranked := rankCodeSearchCandidates(candidates, query, codeSearchTaskExecutionTrace, minInt(limit, 4))
	hits := make([]codeSearchImpactHit, 0, limit*2)
	anchors := make([]repoquery.Anchor, 0, limit*2)
	gaps := make([]string, 0, 4)
	for _, candidate := range ranked {
		if candidate == nil {
			continue
		}
		for _, seed := range uniqueStrings(candidate.RepoNodeIDs) {
			if strings.TrimSpace(seed) == "" {
				continue
			}
			expanded, err := querySvc.ExpandWithProjection(ctx, repoquery.ExpandRequest{
				Seeds:      []string{seed},
				EdgeTypes:  repoindex.EdgeSetStructural,
				Direction:  repoindex.DirOut,
				Depth:      2,
				Budget:     limit * 3,
				PerNodeCap: 20,
			})
			if err != nil {
				gaps = append(gaps, fmt.Sprintf("execution graph failed for %s: %v", seed, err))
				continue
			}
			edgeTypesByNode := expandedEdgeTypesByNode(seed, expanded.Result.Edges, repoindex.DirOut)
			for _, node := range expanded.Result.Nodes {
				anchor, ok := repoquery.AnchorFromNode(node, 1)
				if !ok {
					continue
				}
				nodeID := strings.TrimSpace(node.ID)
				anchors = append(anchors, anchor)
				hits = append(hits, codeSearchImpactHit{
					Path:       normalizeCodeSearchPath(anchor.Path),
					Line:       anchor.LineHint,
					Symbol:     strings.TrimSpace(anchor.SymbolName),
					NodeID:     nodeID,
					SourcePath: candidate.Path,
					EdgeTypes:  edgeTypesByNode[nodeID],
				})
			}
			break
		}
	}
	return dedupeCodeSearchImpactHits(hits), anchors, uniqueStrings(gaps)
}

func codeSearchAdjacentImplementationAugment(workspaceRoot string, candidates map[string]*codeSearchCandidate, query string, pathProbes, exactProbes []string, limit int) ([]codeSearchImpactHit, []string) {
	if strings.TrimSpace(workspaceRoot) == "" || len(candidates) == 0 || limit <= 0 {
		return nil, nil
	}
	ranked := rankCodeSearchCandidatesWithProbes(candidates, query, codeSearchTaskExecutionTrace, minInt(limit, 6), pathProbes, exactProbes)
	pathTerms := codeSearchPathTerms(query)
	type seedDir struct {
		dir        string
		sourcePath string
	}
	seedDirs := make([]seedDir, 0, 8)
	seenSeedDirs := map[string]struct{}{}
	addSeedDir := func(dir, sourcePath string) {
		dir = strings.TrimSpace(filepath.ToSlash(dir))
		if dir == "" || dir == "." {
			return
		}
		if _, ok := seenSeedDirs[dir]; ok {
			return
		}
		seenSeedDirs[dir] = struct{}{}
		seedDirs = append(seedDirs, seedDir{dir: dir, sourcePath: sourcePath})
	}
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		if !isLikelyTextCodeFile(candidate.Path) || isLikelyDocumentationPath(candidate.Path) {
			continue
		}
		if !candidateHasAnySource(candidate, "execution_graph", "trace_source_repo", "trace_target_repo", "bridge_tuple", "execution_bridge") {
			continue
		}
		dir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(candidate.Path)))
		if dir == "" || dir == "." {
			continue
		}
		addSeedDir(dir, candidate.Path)
		baseDir := strings.TrimSpace(strings.TrimSuffix(filepath.Base(candidate.Path), filepath.Ext(candidate.Path)))
		if baseDir != "" {
			addSeedDir(filepath.ToSlash(filepath.Join(dir, baseDir)), candidate.Path)
		}
		if len(seedDirs) >= 6 {
			break
		}
	}
	if len(seedDirs) == 0 {
		return nil, nil
	}
	seenPaths := map[string]struct{}{}
	for pathValue := range candidates {
		seenPaths[normalizeCodeSearchPath(pathValue)] = struct{}{}
	}
	hits := make([]codeSearchImpactHit, 0, limit)
	for _, seed := range seedDirs {
		entries, err := os.ReadDir(filepath.Join(workspaceRoot, filepath.FromSlash(seed.dir)))
		if err != nil {
			continue
		}
		type scored struct {
			path  string
			score float64
			line  int
		}
		scoredHits := make([]scored, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			rel := normalizeCodeSearchPath(filepath.ToSlash(filepath.Join(seed.dir, entry.Name())))
			if rel == "" {
				continue
			}
			if _, ok := seenPaths[rel]; ok {
				continue
			}
			if !isLikelyTextCodeFile(rel) || isLikelyDocumentationPath(rel) {
				continue
			}
			score := codeSearchBasenameProbeScore(rel, pathProbes, exactProbes)*1.3 + codeSearchPathTermScore(filepath.Base(rel), pathTerms)
			if score <= 0.4 {
				continue
			}
			scoredHits = append(scoredHits, scored{path: rel, score: score, line: 1})
		}
		sort.Slice(scoredHits, func(i, j int) bool {
			if scoredHits[i].score == scoredHits[j].score {
				return scoredHits[i].path < scoredHits[j].path
			}
			return scoredHits[i].score > scoredHits[j].score
		})
		for _, item := range scoredHits {
			hits = append(hits, codeSearchImpactHit{
				Path:       item.path,
				Line:       item.line,
				SourcePath: seed.sourcePath,
			})
			seenPaths[item.path] = struct{}{}
			if len(hits) >= limit {
				return dedupeCodeSearchImpactHits(hits), nil
			}
		}
	}
	return dedupeCodeSearchImpactHits(hits), nil
}

func dedupeCodeSearchImpactHits(in []codeSearchImpactHit) []codeSearchImpactHit {
	if len(in) <= 1 {
		return in
	}
	seen := map[string]codeSearchImpactHit{}
	for _, item := range in {
		if item.Path == "" {
			continue
		}
		key := item.Path + "|" + item.Symbol
		if prev, ok := seen[key]; ok {
			prev.EdgeTypes = mergeCodeSearchEdgeTypes(prev.EdgeTypes, item.EdgeTypes)
			seen[key] = prev
			continue
		}
		seen[key] = item
	}
	out := make([]codeSearchImpactHit, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func expandedEdgeTypesByNode(seed string, edges []repoindex.Edge, direction repoindex.Direction) map[string][]repoindex.EdgeType {
	out := make(map[string][]repoindex.EdgeType)
	seen := make(map[string]map[repoindex.EdgeType]struct{})
	for _, edge := range edges {
		neighbor := codeSearchEdgeNeighbor(edge, seed, direction)
		if neighbor == "" {
			continue
		}
		if seen[neighbor] == nil {
			seen[neighbor] = make(map[repoindex.EdgeType]struct{})
		}
		seen[neighbor][edge.Type] = struct{}{}
	}
	for nodeID, types := range seen {
		list := make([]repoindex.EdgeType, 0, len(types))
		for edgeType := range types {
			list = append(list, edgeType)
		}
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
		out[nodeID] = list
	}
	return out
}

func codeSearchEdgeNeighbor(edge repoindex.Edge, current string, direction repoindex.Direction) string {
	switch direction {
	case repoindex.DirIn:
		if edge.Dst == current {
			return edge.Src
		}
	case repoindex.DirOut:
		if edge.Src == current {
			return edge.Dst
		}
	}
	return ""
}

func mergeCodeSearchEdgeTypes(existing, incoming []repoindex.EdgeType) []repoindex.EdgeType {
	if len(existing) == 0 {
		return append([]repoindex.EdgeType(nil), incoming...)
	}
	seen := make(map[repoindex.EdgeType]struct{}, len(existing)+len(incoming))
	out := make([]repoindex.EdgeType, 0, len(existing)+len(incoming))
	for _, edgeType := range existing {
		if _, ok := seen[edgeType]; ok {
			continue
		}
		seen[edgeType] = struct{}{}
		out = append(out, edgeType)
	}
	for _, edgeType := range incoming {
		if _, ok := seen[edgeType]; ok {
			continue
		}
		seen[edgeType] = struct{}{}
		out = append(out, edgeType)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (a *ReadOnlyAdapter) codeSearchLLMSelector(
	ctx context.Context,
	query, taskType string,
	ranked []*codeSearchCandidate,
	selector codeSearchLLMSelectorInput,
	maxFiles int,
) (codeSearchLLMSelectorOutput, map[string]any, *skillobs.TokenUsage, error) {
	var empty codeSearchLLMSelectorOutput
	if len(ranked) == 0 || !selector.Enabled {
		return empty, nil, nil, nil
	}

	provider := strings.TrimSpace(selector.Provider)
	if provider == "" {
		if strings.TrimSpace(a.cfg.LLM.OpenRouterAPIKey) != "" {
			provider = "openrouter"
		} else {
			provider = strings.TrimSpace(a.cfg.LLM.Provider)
		}
	}
	if provider == "" {
		return empty, nil, nil, fmt.Errorf("no llm provider configured")
	}
	apiKey := a.cfg.LLM.ResolveAPIKey(provider)
	if strings.TrimSpace(apiKey) == "" {
		return empty, nil, nil, fmt.Errorf("no api key configured for provider %q", provider)
	}
	model := strings.TrimSpace(selector.Model)
	if model == "" {
		if provider == "openrouter" {
			model = "openai/gpt-5.4-nano"
		} else {
			model = a.cfg.LLM.ResolveModel(provider)
		}
	}
	if model == "" {
		return empty, nil, nil, fmt.Errorf("no model configured for provider %q", provider)
	}

	maxCandidates := selector.MaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = 8
	}
	if maxCandidates > len(ranked) {
		maxCandidates = len(ranked)
	}
	candidates := make([]map[string]any, 0, maxCandidates)
	for _, candidate := range ranked[:maxCandidates] {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		edgeHints := make([]string, 0, 3)
		for _, source := range []string{"execution_graph_implements", "execution_graph_uses_symbol", "execution_graph_refers_to"} {
			if candidateHasSource(candidate, source) {
				edgeHints = append(edgeHints, source)
			}
		}
		candidates = append(candidates, map[string]any{
			"path":        candidate.Path,
			"why":         candidate.Why,
			"sources":     sortedSourceKeys(candidate.Sources),
			"symbols":     uniqueStrings(candidate.Symbols),
			"line_hints":  uniquePositiveInts(candidate.LineHints),
			"edge_hints":  edgeHints,
			"support":     candidate.Support,
			"repo_nodes":  len(uniqueStrings(candidate.RepoNodeIDs)),
			"code_weight": codeSearchFileWeight(candidate.Path),
		})
	}
	if len(candidates) == 0 {
		return empty, nil, nil, nil
	}

	reqPayload := map[string]any{
		"query":           strings.TrimSpace(query),
		"task_type":       taskType,
		"max_files":       maxFiles,
		"candidate_count": len(candidates),
		"candidates":      candidates,
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return empty, nil, nil, err
	}

	responseFormat := json.RawMessage(`{
		"type":"json_schema",
		"json_schema":{
			"name":"code_search_selector",
			"schema":{
				"type":"object",
				"additionalProperties":false,
				"properties":{
					"keep_paths":{"type":"array","items":{"type":"string"}},
					"rationale":{"type":"string"}
				},
				"required":["keep_paths","rationale"]
			}
		}
	}`)
	llmCfg := engine.DefaultLLMChatConfig()
	llmCfg.Provider = provider
	llmCfg.APIKey = apiKey
	llmCfg.BaseURL = a.cfg.LLM.ResolveBaseURL(provider)
	llmCfg.Model = model
	llmCfg.Timeout = 20 * time.Second
	llmCfg.MaxIterations = 1
	llmCfg.MaxTokens = 500
	llmCfg.Temperature = 0
	llmCfg.ResponseFormat = responseFormat

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return empty, nil, nil, err
	}
	systemPrompt := "You are a bounded code retrieval adjudicator. Choose up to max_files repo-relative paths that best complete the answer. Prefer direct source/target coverage, protocol implementation pairs, and typed graph edges like IMPLEMENTS and USES_SYMBOL. Avoid helper or documentation noise. Return only structured JSON."
	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(string(body))},
		MaxTokens:    llmCfg.MaxTokens,
		Temperature:  llmCfg.Temperature,
	})
	if err != nil {
		return empty, nil, nil, err
	}
	if output.StopReason == engine.StopReasonError {
		return empty, nil, nil, errors.New(strings.TrimSpace(output.Error))
	}
	usage := skillobs.CalculateTokenCost(llmCfg.Model, output.Tokens.InputTokens, output.Tokens.OutputTokens)
	var selectorOut codeSearchLLMSelectorOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(output.AssistantText)), &selectorOut); err != nil {
		return empty, nil, &usage, fmt.Errorf("parse selector output: %w", err)
	}
	selectorOut.KeepPaths = normalizeCodeSearchPaths(selectorOut.KeepPaths)
	if maxFiles > 0 && len(selectorOut.KeepPaths) > maxFiles {
		selectorOut.KeepPaths = selectorOut.KeepPaths[:maxFiles]
	}
	meta := map[string]any{
		"provider":   provider,
		"model":      model,
		"keep_paths": append([]string(nil), selectorOut.KeepPaths...),
		"rationale":  strings.TrimSpace(selectorOut.Rationale),
	}
	return selectorOut, meta, &usage, nil
}

func (a *ReadOnlyAdapter) codeSearchLLMPlanner(
	ctx context.Context,
	query, taskType string,
	exactProbes, pathProbes []string,
	planner codeSearchLLMPlannerInput,
) (codeSearchLLMPlannerOutput, map[string]any, *skillobs.TokenUsage, error) {
	var empty codeSearchLLMPlannerOutput
	if !planner.Enabled {
		return empty, nil, nil, nil
	}
	provider := strings.TrimSpace(planner.Provider)
	if provider == "" {
		if strings.TrimSpace(a.cfg.LLM.OpenRouterAPIKey) != "" {
			provider = "openrouter"
		} else {
			provider = strings.TrimSpace(a.cfg.LLM.Provider)
		}
	}
	if provider == "" {
		return empty, nil, nil, fmt.Errorf("no llm provider configured")
	}
	apiKey := a.cfg.LLM.ResolveAPIKey(provider)
	if strings.TrimSpace(apiKey) == "" {
		return empty, nil, nil, fmt.Errorf("no api key configured for provider %q", provider)
	}
	model := strings.TrimSpace(planner.Model)
	if model == "" {
		if provider == "openrouter" {
			model = "openai/gpt-5.4-nano"
		} else {
			model = a.cfg.LLM.ResolveModel(provider)
		}
	}
	if model == "" {
		return empty, nil, nil, fmt.Errorf("no model configured for provider %q", provider)
	}
	maxCandidates := planner.MaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = 8
	}
	if len(exactProbes) > maxCandidates {
		exactProbes = exactProbes[:maxCandidates]
	}
	if len(pathProbes) > maxCandidates {
		pathProbes = pathProbes[:maxCandidates]
	}

	reqPayload := map[string]any{
		"query":          strings.TrimSpace(query),
		"task_type":      taskType,
		"existing_exact": append([]string(nil), exactProbes...),
		"existing_paths": append([]string(nil), pathProbes...),
		"max_candidates": maxCandidates,
		"allowed_edge_types": []string{
			string(repoindex.EdgeCalls),
			string(repoindex.EdgeImports),
			string(repoindex.EdgeRefersTo),
			string(repoindex.EdgeImplements),
			string(repoindex.EdgeUsesSymbol),
		},
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return empty, nil, nil, err
	}
	responseFormat := json.RawMessage(`{
		"type":"json_schema",
		"json_schema":{
			"name":"code_search_planner",
			"schema":{
				"type":"object",
				"additionalProperties":false,
				"properties":{
					"seed_queries":{"type":"array","items":{"type":"string"}},
					"path_biases":{"type":"array","items":{"type":"string"}},
					"edge_priorities":{"type":"array","items":{"type":"string"}},
					"must_cover":{"type":"array","items":{"type":"string"}},
					"ambiguity_family":{"type":"string"},
					"max_widen_steps":{"type":"integer"},
					"rationale":{"type":"string"}
				},
				"required":["seed_queries","path_biases","edge_priorities","must_cover","ambiguity_family","max_widen_steps","rationale"]
			}
		}
	}`)
	llmCfg := engine.DefaultLLMChatConfig()
	llmCfg.Provider = provider
	llmCfg.APIKey = apiKey
	llmCfg.BaseURL = a.cfg.LLM.ResolveBaseURL(provider)
	llmCfg.Model = model
	llmCfg.Timeout = 20 * time.Second
	llmCfg.MaxIterations = 1
	llmCfg.MaxTokens = 400
	llmCfg.Temperature = 0
	llmCfg.ResponseFormat = responseFormat

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return empty, nil, nil, err
	}
	systemPrompt := "You are a bounded code retrieval planner. Given a repo question and current probe candidates, choose a few better seed queries, path biases, structural edge priorities, required coverage classes, and one ambiguity family. Do not return file paths. Use must_cover values only from source,target,implementation,bridge. Use ambiguity_family only from none,protocol_impl,root_vs_subtype,router_executor. Prefer terse repo terms and edge names only. Return only structured JSON."
	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(string(body))},
		MaxTokens:    llmCfg.MaxTokens,
		Temperature:  llmCfg.Temperature,
	})
	if err != nil {
		return empty, nil, nil, err
	}
	if output.StopReason == engine.StopReasonError {
		return empty, nil, nil, errors.New(strings.TrimSpace(output.Error))
	}
	usage := skillobs.CalculateTokenCost(llmCfg.Model, output.Tokens.InputTokens, output.Tokens.OutputTokens)
	var plannerOut codeSearchLLMPlannerOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(output.AssistantText)), &plannerOut); err != nil {
		return empty, nil, &usage, fmt.Errorf("parse planner output: %w", err)
	}
	plannerOut.SeedQueries = mergeCodeSearchProbeLists(nil, plannerOut.SeedQueries)
	plannerOut.PathBiases = mergeCodeSearchProbeLists(nil, plannerOut.PathBiases)
	plannerOut.EdgePriorities = mergeCodeSearchProbeLists(nil, plannerOut.EdgePriorities)
	plannerOut.MustCover = mergeCodeSearchProbeLists(nil, plannerOut.MustCover)
	plannerOut.AmbiguityFamily = strings.TrimSpace(strings.ToLower(plannerOut.AmbiguityFamily))
	if plannerOut.AmbiguityFamily == "" {
		plannerOut.AmbiguityFamily = "none"
	}
	if plannerOut.MaxWidenSteps < 0 {
		plannerOut.MaxWidenSteps = 0
	}
	meta := map[string]any{
		"provider":         provider,
		"model":            model,
		"seed_queries":     append([]string(nil), plannerOut.SeedQueries...),
		"path_biases":      append([]string(nil), plannerOut.PathBiases...),
		"edge_priorities":  append([]string(nil), plannerOut.EdgePriorities...),
		"must_cover":       append([]string(nil), plannerOut.MustCover...),
		"ambiguity_family": plannerOut.AmbiguityFamily,
		"max_widen_steps":  plannerOut.MaxWidenSteps,
		"rationale":        strings.TrimSpace(plannerOut.Rationale),
	}
	return plannerOut, meta, &usage, nil
}

func (a *ReadOnlyAdapter) codeSearchLLMReplanner(
	ctx context.Context,
	query, taskType string,
	ranked []*codeSearchCandidate,
	maxFiles int,
	motifPathScores map[string]float64,
	plannerCfg codeSearchLLMPlannerInput,
	initial codeSearchLLMPlannerOutput,
) (codeSearchLLMReplannerOutput, map[string]any, *skillobs.TokenUsage, error) {
	var empty codeSearchLLMReplannerOutput
	provider := strings.TrimSpace(plannerCfg.Provider)
	if provider == "" {
		if strings.TrimSpace(a.cfg.LLM.OpenRouterAPIKey) != "" {
			provider = "openrouter"
		} else {
			provider = strings.TrimSpace(a.cfg.LLM.Provider)
		}
	}
	if provider == "" {
		return empty, nil, nil, fmt.Errorf("no llm provider configured")
	}
	apiKey := a.cfg.LLM.ResolveAPIKey(provider)
	if strings.TrimSpace(apiKey) == "" {
		return empty, nil, nil, fmt.Errorf("no api key configured for provider %q", provider)
	}
	model := strings.TrimSpace(plannerCfg.Model)
	if model == "" {
		if provider == "openrouter" {
			model = "openai/gpt-5.4-nano"
		} else {
			model = a.cfg.LLM.ResolveModel(provider)
		}
	}
	if model == "" {
		return empty, nil, nil, fmt.Errorf("no model configured for provider %q", provider)
	}
	selectedWindow := minInt(len(ranked), maxInt(maxFiles, 1))
	feedbackWindow := minInt(len(ranked), maxInt(maxFiles*2, 8))
	selectedSummary := compactCodeSearchCandidateSummary(ranked[:selectedWindow])
	alternateSummary := compactCodeSearchCandidateSummary(ranked[selectedWindow:feedbackWindow])
	selectedCoverage := codeSearchCoverageSummary(ranked[:selectedWindow])
	feedbackCoverage := codeSearchCoverageSummary(ranked[:feedbackWindow])
	sameRoleAmbiguities := codeSearchSameRoleAmbiguities(ranked, maxFiles, motifPathScores)
	reqPayload := map[string]any{
		"query":        strings.TrimSpace(query),
		"task_type":    taskType,
		"max_files":    maxFiles,
		"initial_plan": initial,
		"selected": map[string]any{
			"candidates": selectedSummary,
			"coverage":   selectedCoverage,
		},
		"alternates": map[string]any{
			"candidates": alternateSummary,
			"coverage":   feedbackCoverage,
		},
		"missing_in_selected": map[string]bool{
			"source":         !selectedCoverage["source"] && feedbackCoverage["source"],
			"target":         !selectedCoverage["target"] && feedbackCoverage["target"],
			"implementation": !selectedCoverage["implementation"] && feedbackCoverage["implementation"],
			"bridge":         !selectedCoverage["bridge"] && feedbackCoverage["bridge"],
		},
		"source_histogram":      codeSearchSourceHistogram(ranked[:feedbackWindow]),
		"dominant_dirs":         codeSearchDominantDirs(ranked[:feedbackWindow], 4),
		"same_role_ambiguities": sameRoleAmbiguities,
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return empty, nil, nil, err
	}
	responseFormat := json.RawMessage(`{
		"type":"json_schema",
		"json_schema":{
			"name":"code_search_replanner",
			"schema":{
				"type":"object",
				"additionalProperties":false,
				"properties":{
					"add_seed_queries":{"type":"array","items":{"type":"string"}},
					"add_path_biases":{"type":"array","items":{"type":"string"}},
					"raise_edge_priorities":{"type":"array","items":{"type":"string"}},
					"must_cover":{"type":"array","items":{"type":"string"}},
					"prefer_paths":{"type":"array","items":{"type":"string"}},
					"demote_paths":{"type":"array","items":{"type":"string"}},
					"ambiguity_family":{"type":"string"},
					"stop_broadening":{"type":"boolean"},
					"rationale":{"type":"string"}
				},
				"required":["add_seed_queries","add_path_biases","raise_edge_priorities","must_cover","prefer_paths","demote_paths","ambiguity_family","stop_broadening","rationale"]
			}
		}
	}`)
	llmCfg := engine.DefaultLLMChatConfig()
	llmCfg.Provider = provider
	llmCfg.APIKey = apiKey
	llmCfg.BaseURL = a.cfg.LLM.ResolveBaseURL(provider)
	llmCfg.Model = model
	llmCfg.Timeout = 20 * time.Second
	llmCfg.MaxIterations = 1
	llmCfg.MaxTokens = 220
	llmCfg.Temperature = 0
	llmCfg.ResponseFormat = responseFormat
	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return empty, nil, nil, err
	}
	systemPrompt := "You are a bounded code retrieval replanner. You are given a compact first-wave retrieval state. Prefer stop_broadening=true unless the selected top files are missing a role that exists in alternates, or the same_role_ambiguities list shows a strong alternate within the same role/class. Use prefer_paths and demote_paths for narrow same-role substitutions inside the existing candidate window. Use must_cover only from source,target,implementation,bridge. Use ambiguity_family only from none,protocol_impl,root_vs_subtype,router_executor. If ambiguity can be resolved by prefer_paths, demote_paths, ambiguity_family, or must_cover alone, do not broaden. Add only a few terse seed queries or path biases when widening is actually needed. Return only structured JSON."
	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(string(body))},
		MaxTokens:    llmCfg.MaxTokens,
		Temperature:  llmCfg.Temperature,
	})
	if err != nil {
		return empty, nil, nil, err
	}
	if output.StopReason == engine.StopReasonError {
		return empty, nil, nil, errors.New(strings.TrimSpace(output.Error))
	}
	usage := skillobs.CalculateTokenCost(llmCfg.Model, output.Tokens.InputTokens, output.Tokens.OutputTokens)
	var replannerOut codeSearchLLMReplannerOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(output.AssistantText)), &replannerOut); err != nil {
		return empty, nil, &usage, fmt.Errorf("parse replanner output: %w", err)
	}
	replannerOut.AddSeedQueries = mergeCodeSearchProbeLists(nil, replannerOut.AddSeedQueries)
	replannerOut.AddPathBiases = mergeCodeSearchProbeLists(nil, replannerOut.AddPathBiases)
	replannerOut.RaiseEdgePriorities = mergeCodeSearchProbeLists(nil, replannerOut.RaiseEdgePriorities)
	replannerOut.MustCover = mergeCodeSearchProbeLists(nil, replannerOut.MustCover)
	replannerOut.PreferPaths = normalizeCodeSearchPaths(replannerOut.PreferPaths)
	replannerOut.DemotePaths = normalizeCodeSearchPaths(replannerOut.DemotePaths)
	replannerOut.AmbiguityFamily = strings.TrimSpace(strings.ToLower(replannerOut.AmbiguityFamily))
	if replannerOut.AmbiguityFamily == "" {
		replannerOut.AmbiguityFamily = "none"
	}
	meta := map[string]any{
		"provider":              provider,
		"model":                 model,
		"selected_coverage":     selectedCoverage,
		"feedback_coverage":     feedbackCoverage,
		"same_role_ambiguities": sameRoleAmbiguities,
		"add_seed_queries":      append([]string(nil), replannerOut.AddSeedQueries...),
		"add_path_biases":       append([]string(nil), replannerOut.AddPathBiases...),
		"raise_edge_priorities": append([]string(nil), replannerOut.RaiseEdgePriorities...),
		"must_cover":            append([]string(nil), replannerOut.MustCover...),
		"prefer_paths":          append([]string(nil), replannerOut.PreferPaths...),
		"demote_paths":          append([]string(nil), replannerOut.DemotePaths...),
		"ambiguity_family":      replannerOut.AmbiguityFamily,
		"stop_broadening":       replannerOut.StopBroadening,
		"rationale":             strings.TrimSpace(replannerOut.Rationale),
	}
	return replannerOut, meta, &usage, nil
}

func compactCodeSearchCandidateSummary(ranked []*codeSearchCandidate) []map[string]any {
	top := make([]map[string]any, 0, len(ranked))
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		sources := sortedSourceKeys(candidate.Sources)
		top = append(top, map[string]any{
			"path":              candidate.Path,
			"why":               candidate.Why,
			"class":             classifyExecutionTraceCandidate(candidate),
			"dir":               normalizeCodeSearchPath(filepath.Dir(candidate.Path)),
			"sources":           sources,
			"symbols":           uniqueStrings(candidate.Symbols),
			"line_hints":        uniquePositiveInts(candidate.LineHints),
			"support":           candidate.Support,
			"anchor_preference": executionTraceAnchorPreference(candidate),
		})
	}
	return top
}

func codeSearchCoverageSummary(ranked []*codeSearchCandidate) map[string]bool {
	summary := map[string]bool{
		"source":         false,
		"target":         false,
		"implementation": false,
		"bridge":         false,
	}
	for _, candidate := range ranked {
		if candidate == nil {
			continue
		}
		if candidateHasAnySource(candidate, "trace_source_anchor", "trace_source_repo") {
			summary["source"] = true
		}
		if candidateHasAnySource(candidate, "trace_target_anchor", "trace_target_repo") {
			summary["target"] = true
		}
		if candidateHasAnySource(candidate, "execution_graph_implements", "execution_graph_uses_symbol", "adjacent_impl") {
			summary["implementation"] = true
		}
		if candidateHasAnySource(candidate, "execution_bridge", "bridge_tuple", "execution_graph_refers_to") {
			summary["bridge"] = true
		}
	}
	return summary
}

func codeSearchSourceHistogram(ranked []*codeSearchCandidate) map[string]int {
	out := map[string]int{}
	for _, candidate := range ranked {
		if candidate == nil {
			continue
		}
		for source := range candidate.Sources {
			source = strings.TrimSpace(source)
			if source == "" {
				continue
			}
			out[source]++
		}
	}
	return out
}

func codeSearchDominantDirs(ranked []*codeSearchCandidate, limit int) []string {
	if limit <= 0 {
		limit = 3
	}
	type item struct {
		dir   string
		count int
	}
	counts := map[string]int{}
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		dir := normalizeCodeSearchPath(filepath.Dir(candidate.Path))
		if dir == "" || dir == "." {
			continue
		}
		counts[dir]++
	}
	items := make([]item, 0, len(counts))
	for dir, count := range counts {
		items = append(items, item{dir: dir, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].dir < items[j].dir
		}
		return items[i].count > items[j].count
	})
	out := make([]string, 0, minInt(limit, len(items)))
	for _, it := range items[:minInt(limit, len(items))] {
		out = append(out, it.dir)
	}
	return out
}

func codeSearchSameRoleAmbiguities(ranked []*codeSearchCandidate, maxFiles int, motifPathScores map[string]float64) []map[string]any {
	if len(ranked) == 0 {
		return nil
	}
	if maxFiles <= 0 {
		maxFiles = 4
	}
	selectedWindow := minInt(len(ranked), maxFiles)
	feedbackWindow := minInt(len(ranked), maxInt(maxFiles*2, 8))
	if feedbackWindow <= selectedWindow {
		return nil
	}
	alternates := ranked[selectedWindow:feedbackWindow]
	out := make([]map[string]any, 0, 4)
	seen := map[string]struct{}{}
	for _, selected := range ranked[:selectedWindow] {
		if selected == nil || selected.Path == "" {
			continue
		}
		class := classifyExecutionTraceCandidate(selected)
		if class == "" {
			continue
		}
		var best *codeSearchCandidate
		for _, alternate := range alternates {
			if alternate == nil || alternate.Path == "" || alternate.Path == selected.Path {
				continue
			}
			if classifyExecutionTraceCandidate(alternate) != class {
				continue
			}
			gap := selected.Support - alternate.Support
			if gap > 1.25 {
				continue
			}
			if best == nil || alternate.Support > best.Support {
				best = alternate
			}
		}
		if best == nil {
			continue
		}
		key := selected.Path + "->" + best.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, map[string]any{
			"class":                 class,
			"selected_path":         selected.Path,
			"alternate_path":        best.Path,
			"selected_support":      selected.Support,
			"alternate_support":     best.Support,
			"support_gap":           selected.Support - best.Support,
			"selected_dir":          normalizeCodeSearchPath(filepath.Dir(selected.Path)),
			"alternate_dir":         normalizeCodeSearchPath(filepath.Dir(best.Path)),
			"selected_preference":   executionTraceAnchorPreference(selected),
			"alternate_preference":  executionTraceAnchorPreference(best),
			"selected_motif_score":  motifPathScores[normalizeCodeSearchPath(selected.Path)],
			"alternate_motif_score": motifPathScores[normalizeCodeSearchPath(best.Path)],
		})
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func shouldRunCodeSearchLLMReplanner(ranked []*codeSearchCandidate, maxFiles int, motifPathScores map[string]float64) bool {
	if len(ranked) == 0 {
		return false
	}
	if maxFiles <= 0 {
		maxFiles = 4
	}
	selectedWindow := minInt(len(ranked), maxFiles)
	feedbackWindow := minInt(len(ranked), maxInt(maxFiles*2, 8))
	selectedCoverage := codeSearchCoverageSummary(ranked[:selectedWindow])
	feedbackCoverage := codeSearchCoverageSummary(ranked[:feedbackWindow])
	for _, role := range []string{"source", "target", "implementation", "bridge"} {
		if !selectedCoverage[role] && feedbackCoverage[role] {
			return true
		}
	}
	return len(codeSearchSameRoleAmbiguities(ranked, maxFiles, motifPathScores)) > 0
}

func mergeCodeSearchProbeLists(existing, incoming []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(existing)+len(incoming))
	add := func(value string) {
		value = normalizeRepoSearchProbe(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range existing {
		add(value)
	}
	for _, value := range incoming {
		add(value)
	}
	return out
}

func shouldRunCodeSearchLLMSelector(ranked []*codeSearchCandidate, maxFiles int) bool {
	if len(ranked) == 0 {
		return false
	}
	if maxFiles <= 0 {
		maxFiles = 4
	}
	if len(ranked) <= maxFiles {
		return false
	}
	structuralHits := 0
	for _, candidate := range ranked[:minInt(len(ranked), maxFiles*2)] {
		if candidate == nil {
			continue
		}
		if candidateHasAnySource(candidate, "execution_graph_implements", "execution_graph_uses_symbol", "trace_target_anchor", "trace_target_repo") {
			structuralHits++
		}
	}
	return structuralHits >= 2
}

func codeSearchPlannerBoost(candidate *codeSearchCandidate, edgePrioritySet, mustCoverSet map[string]struct{}, ambiguityFamily string) float64 {
	if candidate == nil {
		return 0
	}
	boost := 0.0
	if _, ok := edgePrioritySet["implements"]; ok && candidateHasSource(candidate, "execution_graph_implements") {
		boost += 1.0
	}
	if _, ok := edgePrioritySet["uses_symbol"]; ok && candidateHasSource(candidate, "execution_graph_uses_symbol") {
		boost += 0.8
	}
	if _, ok := edgePrioritySet["refers_to"]; ok && candidateHasSource(candidate, "execution_graph_refers_to") {
		boost += 0.3
	}
	if _, ok := edgePrioritySet["calls"]; ok && candidateHasSource(candidate, "execution_graph") {
		boost += 0.25
	}
	if _, ok := mustCoverSet["source"]; ok && candidateHasAnySource(candidate, "trace_source_anchor", "trace_source_repo") {
		boost += 0.8
	}
	if _, ok := mustCoverSet["target"]; ok && candidateHasAnySource(candidate, "trace_target_anchor", "trace_target_repo") {
		boost += 0.8
	}
	if _, ok := mustCoverSet["implementation"]; ok && candidateHasAnySource(candidate, "execution_graph_implements", "execution_graph_uses_symbol", "adjacent_impl") {
		boost += 1.0
	}
	if _, ok := mustCoverSet["bridge"]; ok && candidateHasAnySource(candidate, "bridge_tuple", "execution_bridge") {
		boost += 0.7
	}
	switch strings.TrimSpace(strings.ToLower(ambiguityFamily)) {
	case "protocol_impl":
		if candidateHasAnySource(candidate, "execution_graph_implements", "execution_graph_uses_symbol", "adjacent_impl") {
			boost += 1.15
		}
		if candidateHasAnySource(candidate, "trace_source_anchor", "trace_source_repo") {
			boost += 0.25
		}
	case "root_vs_subtype":
		depth := strings.Count(normalizeCodeSearchPath(candidate.Path), "/")
		boost -= float64(depth) * 0.08
	case "router_executor":
		lowerPath := strings.ToLower(candidate.Path)
		if strings.Contains(lowerPath, "router") || strings.Contains(lowerPath, "signal_router") {
			boost += 0.5
		}
		if strings.Contains(lowerPath, "executor") || strings.Contains(lowerPath, "exec") {
			boost += 0.7
		}
	}
	return boost
}

func codeSearchExecutionBridgeWeight(path string) float64 {
	path = strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	base := filepath.Base(path)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return 0.15
	case isLikelyExecutionBridgeBase(base):
		return 1.15
	case strings.HasPrefix(path, "cmd/"):
		return 1.0
	default:
		return codeSearchFileWeight(path)
	}
}

func isLikelyExecutionBridgeBase(base string) bool {
	base = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(base)), filepath.Ext(base))
	switch base {
	case "adapter", "bridge", "router", "dispatcher", "dispatch", "registry", "runtime", "handler", "executor", "execute", "wiring":
		return true
	default:
		return false
	}
}

type registrationTraceHit struct {
	Path   string
	Line   int
	Symbol string
}

var goFuncPattern = regexp.MustCompile(`(?m)^func\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

func codeSearchRegistrationAugment(workspaceRoot string, candidates map[string]*codeSearchCandidate, limit int) ([]registrationTraceHit, []string) {
	if limit <= 0 {
		limit = 8
	}
	ranked := rankCodeSearchCandidates(candidates, "", codeSearchTaskFileLocate, minInt(limit, 4))
	symbols := make([]string, 0, 8)
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		fullPath := filepath.Join(workspaceRoot, filepath.FromSlash(candidate.Path))
		body, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		matches := goFuncPattern.FindAllStringSubmatch(string(body), -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			name := strings.TrimSpace(match[1])
			if looksLikeCommandFactory(name) {
				symbols = append(symbols, name)
			}
		}
	}
	symbols = uniqueStrings(symbols)
	if len(symbols) == 0 {
		return nil, nil
	}
	hits := make([]registrationTraceHit, 0, limit)
	gaps := make([]string, 0, 2)
	for _, symbol := range symbols {
		found, err := registrationSiteSearch(workspaceRoot, symbol, limit-len(hits))
		if err != nil {
			gaps = append(gaps, "registration search failed for "+symbol+": "+err.Error())
			continue
		}
		hits = append(hits, found...)
		if len(hits) >= limit {
			break
		}
	}
	return dedupeRegistrationHits(hits), uniqueStrings(gaps)
}

func looksLikeCommandFactory(name string) bool {
	name = strings.TrimSpace(name)
	return strings.HasPrefix(name, "new") && strings.HasSuffix(name, "Command")
}

func registrationSiteSearch(workspaceRoot, symbol string, limit int) ([]registrationTraceHit, error) {
	if workspaceRoot == "" || symbol == "" || limit <= 0 {
		return nil, nil
	}
	hits := make([]registrationTraceHit, 0, limit)
	stop := fmt.Errorf("stop")
	err := filepath.WalkDir(workspaceRoot, func(current string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".idea", ".vscode", "tmp", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(current) != ".go" {
			return nil
		}
		body, readErr := os.ReadFile(current)
		if readErr != nil {
			return nil
		}
		text := string(body)
		index := strings.Index(text, "AddCommand("+symbol)
		if index < 0 {
			index = strings.Index(text, "AddCommand("+symbol+"()")
		}
		if index < 0 {
			index = strings.Index(text, symbol)
			if index < 0 || !strings.Contains(text, "AddCommand(") {
				return nil
			}
		}
		rel, relErr := filepath.Rel(workspaceRoot, current)
		if relErr != nil {
			return nil
		}
		hits = append(hits, registrationTraceHit{
			Path:   filepath.ToSlash(rel),
			Line:   1 + strings.Count(text[:index], "\n"),
			Symbol: symbol,
		})
		if len(hits) >= limit {
			return stop
		}
		return nil
	})
	if err != nil && err != stop {
		return nil, err
	}
	return hits, nil
}

func dedupeRegistrationHits(in []registrationTraceHit) []registrationTraceHit {
	if len(in) <= 1 {
		return in
	}
	seen := map[string]registrationTraceHit{}
	for _, hit := range in {
		key := hit.Path + "|" + hit.Symbol
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = hit
	}
	out := make([]registrationTraceHit, 0, len(seen))
	for _, hit := range seen {
		out = append(out, hit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func isLikelyTextCodeFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".yaml", ".yml", ".md", ".txt", ".sql", ".tf", ".sh", ".py", ".proto", ".ex", ".exs":
		return true
	default:
		return false
	}
}

func codeSearchFileWeight(path string) float64 {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".sql", ".tf", ".proto", ".ex", ".exs":
		return 0.98
	case ".json", ".yaml", ".yml", ".sh":
		return 0.8
	case ".md", ".txt":
		return 0.45
	default:
		return 0.6
	}
}

func isLikelyDocumentationPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	if path == "" {
		return false
	}
	base := filepath.Base(path)
	switch base {
	case "readme.md", "agents.md", "changelog.md", "contributing.md":
		return true
	}
	for _, segment := range strings.Split(path, "/") {
		switch segment {
		case "docs", "doc", "guides", "guide":
			return true
		}
	}
	return false
}

func isLikelyDeclarativeCompanionPath(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".yaml", ".yml", ".json", ".toml":
		return true
	default:
		return false
	}
}

func scoreValue(v any, fallback float64) float64 {
	switch value := v.(type) {
	case float64:
		return clampScore(value)
	case float32:
		return clampScore(float64(value))
	case int:
		return clampScore(float64(value))
	default:
		return clampScore(fallback)
	}
}

func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
