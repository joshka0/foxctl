package main

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	refevidence "github.com/jkatigb/agentctl/internal/intelligence/refactor/evidence"
	refhot "github.com/jkatigb/agentctl/internal/intelligence/refactor/hot"
	refscope "github.com/jkatigb/agentctl/internal/intelligence/refactor/scope"
	refsnapshot "github.com/jkatigb/agentctl/internal/intelligence/refactor/snapshot"
	refsnapshotstore "github.com/jkatigb/agentctl/internal/intelligence/refactor/snapshotstore"
	refstatus "github.com/jkatigb/agentctl/internal/intelligence/refactor/status"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
)

const (
	maxEvidenceHotspots       = 5
	maxIndexedHotspotEvidence = 20
)

type scoutEvidenceResult struct {
	SnapshotID       string
	SnapshotArtifact string
	EvidenceArtifact string
	Findings         []finding
}

func buildScoutEvidence(ctx context.Context, rc *skillmain.RunContext, in input, scope refscope.Scope, status refstatus.Status, findings []finding) (scoutEvidenceResult, error) {
	now := scoutNow(rc)
	result := scoutEvidenceResult{
		Findings: append([]finding(nil), findings...),
	}

	snapshotPayload, err := refsnapshot.Builder{}.Build(ctx, refsnapshot.Input{
		SnapshotID:   refsnapshot.GenerateID(now),
		CreatedAt:    now,
		Scope:        scope,
		Status:       status,
		IncludeTests: in.IncludeTests,
	})
	if err != nil {
		return result, err
	}
	snapshotArtifact, err := skillmain.PersistJSON(ctx, rc, snapshotPayload, "refactor-scout-snapshot", scope.Language)
	if err != nil {
		return result, err
	}
	result.SnapshotID = snapshotPayload.SnapshotID
	result.SnapshotArtifact = snapshotArtifact.Digest

	metaStore, err := refsnapshotstore.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return result, err
	}
	defer metaStore.Close()
	if err := metaStore.Put(ctx, refsnapshotstore.Record{
		SnapshotID:     snapshotPayload.SnapshotID,
		Workspace:      snapshotPayload.Scope.Workspace,
		RepoRoot:       snapshotPayload.Scope.RepoRoot,
		Path:           snapshotPayload.Scope.Path,
		Language:       snapshotPayload.Scope.Language,
		IncludeTests:   in.IncludeTests,
		Mode:           string(snapshotPayload.Mode),
		GitHeadSHA:     snapshotPayload.Git.HeadSHA,
		IndexHeadSHA:   snapshotPayload.RepoIndex.HeadSHA,
		ArtifactDigest: snapshotArtifact.Digest,
		FileCount:      snapshotPayload.Summary.FileCount,
		SymbolCount:    snapshotPayload.Summary.SymbolCount,
		CreatedAt:      snapshotPayload.CreatedAt,
	}); err != nil {
		return result, err
	}

	hotIndex, symbolHotIndex, cochangeIndex := buildScoutHotIndexes(ctx, rc, scope, snapshotPayload, in.IncludeTests, now)

	pack := refevidence.HotspotPack{
		SnapshotID:       snapshotPayload.SnapshotID,
		SnapshotArtifact: snapshotArtifact.Digest,
		IndexMode:        string(status.Mode),
		Reasons:          append([]string(nil), status.Reasons...),
	}

	if status.Mode != refstatus.ModeIndexBacked {
		result.Findings = attachEvidenceToHotspots(ctx, result.Findings, nil, hotIndex, symbolHotIndex, cochangeIndex, snapshotPayload.SnapshotID, snapshotArtifact.Digest, &pack)
		result.Findings = rerankScoutFindings(result.Findings, status.Mode)
		applyOpportunityScoresToPack(&pack, result.Findings)
		return persistScoutEvidencePack(ctx, rc, result, pack)
	}

	store, err := repoindex.Open(ctx, rc.Config.Storage.Root, scope.Workspace)
	if err != nil {
		result.Findings = attachEvidenceToHotspots(ctx, result.Findings, nil, hotIndex, symbolHotIndex, cochangeIndex, snapshotPayload.SnapshotID, snapshotArtifact.Digest, &pack)
		result.Findings = rerankScoutFindings(result.Findings, status.Mode)
		applyOpportunityScoresToPack(&pack, result.Findings)
		return persistScoutEvidencePack(ctx, rc, result, pack)
	}
	defer store.Close()

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	result.Findings = attachEvidenceToHotspots(ctx, result.Findings, service, hotIndex, symbolHotIndex, cochangeIndex, snapshotPayload.SnapshotID, snapshotArtifact.Digest, &pack)
	result.Findings = rerankScoutFindings(result.Findings, status.Mode)
	applyOpportunityScoresToPack(&pack, result.Findings)
	return persistScoutEvidencePack(ctx, rc, result, pack)
}

func persistScoutEvidencePack(ctx context.Context, rc *skillmain.RunContext, current scoutEvidenceResult, pack refevidence.HotspotPack) (scoutEvidenceResult, error) {
	if len(pack.Hotspots) == 0 {
		return current, nil
	}
	artifact, err := skillmain.PersistJSON(ctx, rc, pack, "refactor-scout-evidence")
	if err != nil {
		return current, err
	}
	current.EvidenceArtifact = artifact.Digest
	for i := range current.Findings {
		if current.Findings[i].Evidence == nil {
			continue
		}
		if current.Findings[i].RuleID != "function_hotspot" {
			continue
		}
		current.Findings[i].Evidence["evidence_artifact"] = artifact.Digest
	}
	return current, nil
}

func attachEvidenceToHotspots(ctx context.Context, findings []finding, service *repoquery.QueryService, hotIndex map[string]refhot.FileHotspot, symbolHotIndex map[string]refhot.SymbolHotspot, cochangeIndex map[string][]refhot.CochangeNeighbor, snapshotID, snapshotArtifact string, pack *refevidence.HotspotPack) []finding {
	out := append([]finding(nil), findings...)
	graphEnriched := 0
	for i := range out {
		if out[i].RuleID != "function_hotspot" {
			continue
		}
		if out[i].Evidence == nil {
			out[i].Evidence = map[string]any{}
		}
		out[i].Evidence["scope_snapshot_id"] = snapshotID
		out[i].Evidence["scope_snapshot_artifact"] = snapshotArtifact
		if hot, ok := hotIndex[out[i].File]; ok {
			out[i].Evidence["recent_change_count"] = hot.TouchCount
			out[i].Evidence["hot_score"] = hot.Score
		}
		if symbolHot, ok := lookupFindingSymbolHotspot(symbolHotIndex, out[i]); ok {
			out[i].Evidence["symbol_recent_change_count"] = symbolHot.TouchCount
			out[i].Evidence["symbol_hot_score"] = symbolHot.Score
			out[i].Evidence["symbol_changed_line_count"] = symbolHot.ChangedLineCount
		}
		if cochange := lookupFindingCochange(cochangeIndex, out[i]); len(cochange) > 0 {
			out[i].Evidence["cochange_count"] = len(cochange)
			out[i].Evidence["cochange_strength"] = cochange[0].Score
			out[i].Evidence["cochange_paths"] = cochangeNeighborPaths(cochange)
		}
		if boundary := classifySuggestedBoundary(out[i]); boundary != "" {
			out[i].Evidence["suggested_boundary_kind"] = boundary
		}
		row := refevidence.HotspotRow{
			File:              out[i].File,
			Symbol:            out[i].Symbol,
			RuleID:            out[i].RuleID,
			RecentChangeCount: evidenceInt(out[i].Evidence["recent_change_count"]),
			HotScore:          evidenceFloat(out[i].Evidence["hot_score"]),
			SymbolTouchCount:  evidenceInt(out[i].Evidence["symbol_recent_change_count"]),
			SymbolHotScore:    evidenceFloat(out[i].Evidence["symbol_hot_score"]),
			SymbolChangedLine: evidenceInt(out[i].Evidence["symbol_changed_line_count"]),
			CochangeStrength:  evidenceFloat(out[i].Evidence["cochange_strength"]),
			CochangeCount:     evidenceInt(out[i].Evidence["cochange_count"]),
			CochangePaths:     evidenceStrings(out[i].Evidence["cochange_paths"]),
			SuggestedBoundary: evidenceString(out[i].Evidence["suggested_boundary_kind"]),
		}
		shouldAttachGraph := service != nil && graphEnriched < maxIndexedHotspotEvidence
		if shouldAttachGraph {
			if seed, seedQuery := resolveFindingSeedNode(ctx, service, out[i]); seed != nil {
				reverse, reverseAnchors := expandFindingNeighbors(ctx, service, seed.ID, repoindex.DirIn)
				forward, forwardAnchors := expandFindingNeighbors(ctx, service, seed.ID, repoindex.DirOut)
				suggestedReads := suggestedReadPaths(out[i].File, reverseAnchors, forwardAnchors)

				out[i].Evidence["seed_node_id"] = seed.ID
				out[i].Evidence["seed_query"] = seedQuery
				out[i].Evidence["reverse_dep_count"] = reverse
				out[i].Evidence["forward_dep_count"] = forward
				out[i].Evidence["suggested_reads"] = suggestedReads
				row.SeedNodeID = seed.ID
				row.SeedQuery = seedQuery
				row.ReverseDepCount = reverse
				row.ForwardDepCount = forward
				row.SuggestedReads = suggestedReads
				graphEnriched++
			}
		}
		row.OpportunityScore = evidenceInt(out[i].Evidence["opportunity_score"])
		if pack != nil && len(pack.Hotspots) < maxEvidenceHotspots {
			pack.Hotspots = append(pack.Hotspots, row)
		}
	}
	return out
}

func rerankScoutFindings(findings []finding, mode refstatus.Mode) []finding {
	out := append([]finding(nil), findings...)
	for i := range out {
		if out[i].RuleID != "function_hotspot" || out[i].Evidence == nil {
			continue
		}
		baseScore := out[i].Score
		reverseDepCount := evidenceInt(out[i].Evidence["reverse_dep_count"])
		forwardDepCount := evidenceInt(out[i].Evidence["forward_dep_count"])
		hotScore := evidenceFloat(out[i].Evidence["hot_score"])
		recentChangeCount := evidenceInt(out[i].Evidence["recent_change_count"])
		symbolHotScore := evidenceFloat(out[i].Evidence["symbol_hot_score"])
		symbolTouchCount := evidenceInt(out[i].Evidence["symbol_recent_change_count"])
		symbolChangedLines := evidenceInt(out[i].Evidence["symbol_changed_line_count"])
		cochangeStrength := evidenceFloat(out[i].Evidence["cochange_strength"])
		cochangeCount := evidenceInt(out[i].Evidence["cochange_count"])

		reverseBonus := 0
		forwardBonus := 0
		if mode == refstatus.ModeIndexBacked {
			reverseBonus = scoreReverseDependencyBonus(reverseDepCount)
			forwardBonus = scoreForwardDependencyBonus(forwardDepCount)
		}
		symbolHotBonus := scoreSymbolHotEvidenceBonus(symbolHotScore, symbolChangedLines)
		fileHotBonus := 0
		if symbolHotBonus == 0 {
			fileHotBonus = scoreHotEvidenceBonus(hotScore)
		}
		cochangeBonus := scoreCochangeBonus(cochangeStrength, cochangeCount)
		recentBonus := scoreRecentChangeCountBonus(maxEvidenceInt(recentChangeCount, symbolTouchCount))
		totalBonus := reverseBonus + forwardBonus + symbolHotBonus + fileHotBonus + cochangeBonus + recentBonus
		out[i].Score = clampScore(baseScore + totalBonus)
		out[i].Severity = severityFor(out[i].Score)
		out[i].Evidence["base_score"] = baseScore
		out[i].Evidence["opportunity_score"] = out[i].Score
		out[i].Evidence["opportunity_bonus"] = totalBonus
		out[i].Evidence["opportunity_factors"] = map[string]int{
			"reverse_deps": reverseBonus,
			"forward_deps": forwardBonus,
			"symbol_hot":   symbolHotBonus,
			"file_hot":     fileHotBonus,
			"cochange":     cochangeBonus,
			"recent":       recentBonus,
		}
		if mode == refstatus.ModeIndexBacked && totalBonus > 0 {
			out[i].Evidence["index_rerank_bonus"] = totalBonus
			out[i].Evidence["index_rerank_score"] = out[i].Score
			out[i].Evidence["index_rerank_factors"] = map[string]int{
				"reverse_deps": reverseBonus,
				"forward_deps": forwardBonus,
				"symbol_hot":   symbolHotBonus,
				"file_hot":     fileHotBonus,
				"cochange":     cochangeBonus,
				"recent":       recentBonus,
			}
		}
	}
	return out
}

func scoreReverseDependencyBonus(count int) int {
	switch {
	case count >= 12:
		return 8
	case count >= 6:
		return 6
	case count >= 3:
		return 4
	case count >= 1:
		return 2
	default:
		return 0
	}
}

func scoreForwardDependencyBonus(count int) int {
	switch {
	case count >= 20:
		return 4
	case count >= 10:
		return 3
	case count >= 4:
		return 2
	case count >= 1:
		return 1
	default:
		return 0
	}
}

func scoreHotEvidenceBonus(score float64) int {
	rounded := math.Round(score*100) / 100
	switch {
	case rounded >= 6:
		return 6
	case rounded >= 3:
		return 4
	case rounded >= 1.5:
		return 3
	case rounded >= 0.75:
		return 2
	case rounded > 0:
		return 1
	default:
		return 0
	}
}

func scoreSymbolHotEvidenceBonus(score float64, changedLines int) int {
	rounded := math.Round(score*100) / 100
	bonus := 0
	switch {
	case rounded >= 4:
		bonus = 6
	case rounded >= 2:
		bonus = 4
	case rounded > 0:
		bonus = 2
	}
	switch {
	case changedLines >= 20:
		bonus += 3
	case changedLines >= 8:
		bonus += 2
	case changedLines >= 1:
		bonus++
	}
	if bonus > 8 {
		return 8
	}
	return bonus
}

func scoreRecentChangeCountBonus(count int) int {
	switch {
	case count >= 10:
		return 2
	case count >= 5:
		return 1
	default:
		return 0
	}
}

func scoreCochangeBonus(strength float64, count int) int {
	bonus := 0
	rounded := math.Round(strength*100) / 100
	switch {
	case rounded >= 1.5:
		bonus = 3
	case rounded >= 0.75:
		bonus = 2
	case rounded > 0:
		bonus = 1
	}
	switch {
	case count >= 4:
		bonus += 2
	case count >= 2:
		bonus++
	}
	if bonus > 4 {
		return 4
	}
	return bonus
}

func applyOpportunityScoresToPack(pack *refevidence.HotspotPack, findings []finding) {
	if pack == nil || len(pack.Hotspots) == 0 || len(findings) == 0 {
		return
	}
	byKey := make(map[string]int, len(findings))
	for _, item := range findings {
		if item.RuleID != "function_hotspot" || item.Evidence == nil {
			continue
		}
		byKey[findingSymbolHotKey(item.File, item.Symbol)] = evidenceInt(item.Evidence["opportunity_score"])
	}
	for i := range pack.Hotspots {
		if score, ok := byKey[findingSymbolHotKey(pack.Hotspots[i].File, pack.Hotspots[i].Symbol)]; ok {
			pack.Hotspots[i].OpportunityScore = score
		}
	}
}

func buildScoutHotIndexes(ctx context.Context, rc *skillmain.RunContext, scope refscope.Scope, snapshot refsnapshot.Payload, includeTests bool, now time.Time) (map[string]refhot.FileHotspot, map[string]refhot.SymbolHotspot, map[string][]refhot.CochangeNeighbor) {
	for _, baseline := range []string{"HEAD~20", "HEAD~5", "HEAD"} {
		result, err := refhot.Build(ctx, rc.Config.Storage.Root, refhot.Options{
			Scope:        scope,
			IncludeTests: includeTests,
			Since:        baseline,
			MaxResults:   1000,
			HalfLifeDays: 90,
			Now:          now,
		})
		if err != nil {
			continue
		}
		fileIndex := make(map[string]refhot.FileHotspot, len(result.Files))
		for _, file := range result.Files {
			fileIndex[file.Path] = file
		}
		symbolHotspots, err := refhot.BuildSymbolHotspots(ctx, scope, baseline, snapshot, fileIndex, now)
		if err != nil {
			return fileIndex, nil, nil
		}
		symbolIndex := buildFindingSymbolHotIndex(symbolHotspots)
		cochangeIndex, err := refhot.BuildCochangeIndex(ctx, scope, includeTests, baseline, 90, now, 3)
		if err != nil {
			return fileIndex, symbolIndex, nil
		}
		return fileIndex, symbolIndex, cochangeIndex
	}
	return nil, nil, nil
}

func buildFindingSymbolHotIndex(items []refhot.SymbolHotspot) map[string]refhot.SymbolHotspot {
	if len(items) == 0 {
		return nil
	}
	index := make(map[string]refhot.SymbolHotspot, len(items)*2)
	for _, item := range items {
		for _, variant := range findingSeedVariants(item.Name) {
			key := findingSymbolHotKey(item.Path, variant)
			prev, ok := index[key]
			if !ok || item.Score > prev.Score || (item.Score == prev.Score && item.ChangedLineCount > prev.ChangedLineCount) {
				index[key] = item
			}
		}
	}
	return index
}

func lookupFindingSymbolHotspot(index map[string]refhot.SymbolHotspot, item finding) (refhot.SymbolHotspot, bool) {
	if len(index) == 0 {
		return refhot.SymbolHotspot{}, false
	}
	best := refhot.SymbolHotspot{}
	found := false
	for _, variant := range findingSeedVariants(item.Symbol) {
		key := findingSymbolHotKey(item.File, variant)
		hot, ok := index[key]
		if !ok {
			continue
		}
		if item.Line > 0 && hot.LineStart > 0 && hot.LineEnd > 0 {
			if item.Line < hot.LineStart || item.Line > hot.LineEnd {
				continue
			}
		}
		if !found || hot.Score > best.Score || (hot.Score == best.Score && hot.ChangedLineCount > best.ChangedLineCount) {
			best = hot
			found = true
		}
	}
	return best, found
}

func findingSymbolHotKey(path, symbol string) string {
	return strings.TrimSpace(path) + "\x00" + strings.TrimSpace(symbol)
}

func maxEvidenceInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func lookupFindingCochange(index map[string][]refhot.CochangeNeighbor, item finding) []refhot.CochangeNeighbor {
	if len(index) == 0 {
		return nil
	}
	return index[strings.TrimSpace(item.File)]
}

func cochangeNeighborPaths(items []refhot.CochangeNeighbor) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		out = append(out, path)
	}
	return out
}

func classifySuggestedBoundary(item finding) string {
	if item.RuleID != "function_hotspot" || item.Evidence == nil {
		return ""
	}
	rules := evidenceStrings(item.Evidence["rules"])
	if len(rules) == 0 {
		return ""
	}
	set := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		set[strings.TrimSpace(rule)] = struct{}{}
	}
	if hasAnyRule(set, "duplicate_recovery_block", "duplicated_error_remap", "repeated_guard_ladder") {
		if hasAnyRule(set, "duplicate_recovery_block", "duplicated_error_remap") {
			return "extract_error_normalizer"
		}
	}
	if hasAnyRule(set, "semantic_simplification_candidate") {
		return "simplify_boolean_surface"
	}
	if hasAnyRule(set, "preload_after_get_chain") {
		return "extract_repo_loader"
	}
	if hasAnyRule(set, "post_transaction_preload") {
		return "split_transaction_loader"
	}
	if hasAnyRule(set, "transaction_script_hotspot") {
		return "extract_transaction_script"
	}
	if hasAnyRule(set, "duplicate_orchestration_fingerprint") && hasAnyRule(set, "fan_out_dependency_spread", "same_file_extraction_candidate") {
		return "extract_workflow_step"
	}
	if hasAnyRule(set, "same_file_extraction_candidate") && hasAnyRule(set, "high_cyclomatic_complexity", "deep_nesting", "oversized_function") {
		return "extract_branch_core"
	}
	return ""
}

func hasAnyRule(set map[string]struct{}, names ...string) bool {
	for _, name := range names {
		if _, ok := set[strings.TrimSpace(name)]; ok {
			return true
		}
	}
	return false
}

func evidenceStrings(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func evidenceString(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func resolveFindingSeedNode(ctx context.Context, service *repoquery.QueryService, item finding) (*repoindex.Node, string) {
	if service == nil {
		return nil, ""
	}
	for _, query := range findingSeedQueries(item.Symbol) {
		result, err := service.SearchWithProjection(ctx, repoquery.SearchRequest{
			Query: query,
			Limit: 25,
		})
		if err != nil {
			continue
		}
		if node, ok := pickFindingSeedNode(result.Nodes, item); ok {
			return &node, query
		}
	}
	return nil, ""
}

func expandFindingNeighbors(ctx context.Context, service *repoquery.QueryService, seedID string, direction repoindex.Direction) (int, []repoquery.Anchor) {
	req, err := repoquery.NewExpandRequest([]string{seedID}, repoquery.EdgeTypeValues(repoindex.EdgeSetStructural), string(direction), 1, 30, 20)
	if err != nil {
		return 0, nil
	}
	result, err := service.ExpandWithProjection(ctx, req)
	if err != nil {
		return 0, nil
	}
	return countExpandedNeighbors(result.Result, seedID), result.Anchors
}

func countExpandedNeighbors(result repoindex.ExpandResult, seedID string) int {
	count := 0
	for _, node := range result.Nodes {
		if strings.TrimSpace(node.ID) == strings.TrimSpace(seedID) {
			continue
		}
		count++
	}
	return count
}

func pickFindingSeedNode(nodes []repoindex.Node, item finding) (repoindex.Node, bool) {
	targetFile := strings.TrimSpace(item.File)
	if targetFile == "" {
		return repoindex.Node{}, false
	}
	names := findingSeedVariants(item.Symbol)
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		nameSet[strings.TrimSpace(name)] = struct{}{}
	}
	for _, node := range nodes {
		if node.Kind != repoindex.NodeSymbol {
			continue
		}
		if strings.TrimSpace(node.File) != targetFile {
			continue
		}
		for _, candidate := range findingSeedVariants(node.Name) {
			if _, ok := nameSet[strings.TrimSpace(candidate)]; ok {
				return node, true
			}
		}
	}
	return repoindex.Node{}, false
}

func findingSeedQueries(symbol string) []string {
	return findingSeedVariants(symbol)
}

func findingSeedVariants(symbol string) []string {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil
	}
	values := []string{symbol}
	if trimmed := strings.TrimPrefix(symbol, "*"); strings.TrimSpace(trimmed) != "" && trimmed != symbol {
		values = append(values, trimmed)
	}
	if idx := strings.LastIndex(symbol, "."); idx >= 0 && idx+1 < len(symbol) {
		values = append(values, symbol[idx+1:])
	}
	if idx := strings.LastIndex(symbol, ")"); idx >= 0 && idx+2 <= len(symbol) {
		tail := strings.TrimPrefix(symbol[idx+1:], ".")
		if strings.TrimSpace(tail) != "" {
			values = append(values, tail)
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
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

func suggestedReadPaths(currentFile string, groups ...[]repoquery.Anchor) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for _, group := range groups {
		for _, anchor := range group {
			path := strings.TrimSpace(anchor.Path)
			if path == "" || path == strings.TrimSpace(currentFile) {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			out = append(out, path)
		}
	}
	sort.Strings(out)
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func evidenceInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func evidenceFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

func scoutNow(rc *skillmain.RunContext) time.Time {
	if rc != nil && rc.Now != nil {
		return rc.Now().UTC()
	}
	return time.Now().UTC()
}
