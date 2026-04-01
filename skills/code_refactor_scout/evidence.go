package main

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	refhot "github.com/jkatigb/agentctl/internal/refactor/hot"
	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
	refsnapshot "github.com/jkatigb/agentctl/internal/refactor/snapshot"
	refsnapshotstore "github.com/jkatigb/agentctl/internal/refactor/snapshotstore"
	refstatus "github.com/jkatigb/agentctl/internal/refactor/status"
	"github.com/jkatigb/agentctl/internal/repoquery"
)

const maxEvidenceHotspots = 5

type scoutEvidenceResult struct {
	SnapshotID       string
	SnapshotArtifact string
	EvidenceArtifact string
	Findings         []finding
}

type scoutHotspotEvidencePack struct {
	SnapshotID       string                    `json:"snapshot_id"`
	SnapshotArtifact string                    `json:"snapshot_artifact,omitempty"`
	IndexMode        string                    `json:"index_mode"`
	Reasons          []string                  `json:"reasons,omitempty"`
	Hotspots         []scoutHotspotEvidenceRow `json:"hotspots,omitempty"`
}

type scoutHotspotEvidenceRow struct {
	File              string   `json:"file"`
	Symbol            string   `json:"symbol"`
	RuleID            string   `json:"rule_id"`
	SeedNodeID        string   `json:"seed_node_id,omitempty"`
	SeedQuery         string   `json:"seed_query,omitempty"`
	ReverseDepCount   int      `json:"reverse_dep_count"`
	ForwardDepCount   int      `json:"forward_dep_count"`
	RecentChangeCount int      `json:"recent_change_count"`
	HotScore          float64  `json:"hot_score"`
	SuggestedReads    []string `json:"suggested_reads,omitempty"`
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

	hotIndex := buildScoutHotIndex(ctx, rc, scope, in.IncludeTests, now)

	pack := scoutHotspotEvidencePack{
		SnapshotID:       snapshotPayload.SnapshotID,
		SnapshotArtifact: snapshotArtifact.Digest,
		IndexMode:        string(status.Mode),
		Reasons:          append([]string(nil), status.Reasons...),
	}

	if status.Mode != refstatus.ModeIndexBacked {
		result.Findings = attachEvidenceToHotspots(ctx, result.Findings, nil, hotIndex, snapshotPayload.SnapshotID, snapshotArtifact.Digest, &pack)
		return persistScoutEvidencePack(ctx, rc, result, pack)
	}

	store, err := repoindex.Open(ctx, rc.Config.Storage.Root, scope.Workspace)
	if err != nil {
		result.Findings = attachEvidenceToHotspots(ctx, result.Findings, nil, hotIndex, snapshotPayload.SnapshotID, snapshotArtifact.Digest, &pack)
		return persistScoutEvidencePack(ctx, rc, result, pack)
	}
	defer store.Close()

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	result.Findings = attachEvidenceToHotspots(ctx, result.Findings, service, hotIndex, snapshotPayload.SnapshotID, snapshotArtifact.Digest, &pack)
	return persistScoutEvidencePack(ctx, rc, result, pack)
}

func persistScoutEvidencePack(ctx context.Context, rc *skillmain.RunContext, current scoutEvidenceResult, pack scoutHotspotEvidencePack) (scoutEvidenceResult, error) {
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

func attachEvidenceToHotspots(ctx context.Context, findings []finding, service *repoquery.QueryService, hotIndex map[string]refhot.FileHotspot, snapshotID, snapshotArtifact string, pack *scoutHotspotEvidencePack) []finding {
	out := append([]finding(nil), findings...)
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
		if pack == nil || len(pack.Hotspots) >= maxEvidenceHotspots {
			continue
		}
		row := scoutHotspotEvidenceRow{
			File:              out[i].File,
			Symbol:            out[i].Symbol,
			RuleID:            out[i].RuleID,
			RecentChangeCount: evidenceInt(out[i].Evidence["recent_change_count"]),
			HotScore:          evidenceFloat(out[i].Evidence["hot_score"]),
		}
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
		}
		pack.Hotspots = append(pack.Hotspots, row)
	}
	return out
}

func buildScoutHotIndex(ctx context.Context, rc *skillmain.RunContext, scope refscope.Scope, includeTests bool, now time.Time) map[string]refhot.FileHotspot {
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
		out := make(map[string]refhot.FileHotspot, len(result.Files))
		for _, file := range result.Files {
			out[file.Path] = file
		}
		return out
	}
	return nil
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
	names := findingSeedQueries(item.Symbol)
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
		if _, ok := nameSet[strings.TrimSpace(node.Name)]; ok {
			return node, true
		}
	}
	return repoindex.Node{}, false
}

func findingSeedQueries(symbol string) []string {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil
	}
	values := []string{symbol}
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
