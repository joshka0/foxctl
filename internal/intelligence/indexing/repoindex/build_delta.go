package repoindex

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	deltaIncrementalReason        = "file_delta_graph_patch"
	deltaNoIndexedFileStateReason = "no_indexed_file_state"
)

// BuildDelta is the incremental indexing entrypoint. It accepts a precomputed
// workspace delta so callers can report freshness and skip clean rebuilds
// without racing a second status calculation.
func (b *Builder) BuildDelta(ctx context.Context, opts BuildOptions, delta WorkspaceDelta) (BuildDeltaResult, error) {
	if delta.Empty() {
		if delta.Unchanged > 0 {
			return BuildDeltaResult{
				Delta:  delta,
				Mode:   DeltaBuildModeNoop,
				Reason: "no_file_delta",
			}, nil
		}
		return b.buildDeltaFullFallback(ctx, opts, delta, deltaNoIndexedFileStateReason)
	}
	states, err := b.store.ListFileStates(ctx)
	if err != nil {
		return BuildDeltaResult{Delta: delta}, fmt.Errorf("repoindex: list file state for delta build: %w", err)
	}
	if len(states) == 0 {
		return b.buildDeltaFullFallback(ctx, opts, delta, deltaNoIndexedFileStateReason)
	}

	start := time.Now()
	report := newBuildProgressReporter(opts.Progress, start)
	graph, err := b.buildGraph(ctx, opts, report)
	if err != nil {
		return BuildDeltaResult{
			Result: graph.Result,
			Delta:  delta,
			Mode:   DeltaBuildModeIncremental,
			Reason: deltaIncrementalReason,
		}, err
	}
	if graph.Opts.DryRun {
		report("done", "dry run complete", graph.Result)
		return BuildDeltaResult{
			Result: graph.Result,
			Delta:  delta,
			Mode:   DeltaBuildModeIncremental,
			Reason: deltaIncrementalReason,
		}, nil
	}

	snapshot := ResolveGitSnapshot(ctx, graph.Opts.RepoRoot)
	patch, err := b.buildDeltaGraphPatch(ctx, graph, delta, snapshot.HeadSHA)
	if err != nil {
		return BuildDeltaResult{Result: graph.Result, Delta: delta, Mode: DeltaBuildModeIncremental, Reason: deltaIncrementalReason}, err
	}
	report("persist", fmt.Sprintf("patching repoindex store paths=%d nodes=%d edges=%d", len(patch.RemoveFilePaths), len(patch.UpsertNodes), len(patch.UpsertEdges)), graph.Result)
	if err := b.store.replaceGraphPatch(ctx, patch); err != nil {
		return BuildDeltaResult{Result: graph.Result, Delta: delta, Mode: DeltaBuildModeIncremental, Reason: deltaIncrementalReason}, err
	}

	meta := IndexMetaFromGitSnapshot(IndexMeta{
		RepoRoot:      graph.Opts.RepoRoot,
		SchemaVersion: schemaVersion,
		IndexedAt:     time.Now().UTC(),
		Languages:     buildLanguages(graph.Opts),
	}, snapshot)
	if err := b.store.SetMeta(ctx, meta); err != nil {
		return BuildDeltaResult{Result: graph.Result, Delta: delta, Mode: DeltaBuildModeIncremental, Reason: deltaIncrementalReason}, err
	}
	report("done", "repoindex incremental build complete", graph.Result)
	return BuildDeltaResult{
		Result: graph.Result,
		Delta:  delta,
		Mode:   DeltaBuildModeIncremental,
		Reason: deltaIncrementalReason,
	}, nil
}

func (b *Builder) buildDeltaFullFallback(ctx context.Context, opts BuildOptions, delta WorkspaceDelta, reason string) (BuildDeltaResult, error) {
	if opts.Progress != nil {
		opts.Progress(BuildProgress{
			Phase:   "delta",
			Message: fmt.Sprintf("incremental request requires full rebuild fallback (%s)", reason),
		})
	}
	result, err := b.Build(ctx, opts)
	return BuildDeltaResult{
		Result:       result,
		Delta:        delta,
		Mode:         DeltaBuildModeFullFallback,
		Reason:       reason,
		FullFallback: true,
	}, err
}

func (b *Builder) buildDeltaGraphPatch(ctx context.Context, graph repoGraphBuild, delta WorkspaceDelta, headSHA string) (graphPatch, error) {
	changedPaths := deltaChangedPaths(delta)
	if len(changedPaths) == 0 {
		return graphPatch{}, nil
	}
	oldNodes, err := b.store.ListNodesByFiles(ctx, changedPaths)
	if err != nil {
		return graphPatch{}, fmt.Errorf("repoindex: list old nodes for delta patch: %w", err)
	}

	changedSet := stringSet(changedPaths)
	newNodesByID := make(map[string]Node, len(graph.Nodes))
	for _, node := range graph.Nodes {
		newNodesByID[node.ID] = node
	}

	removeNodeIDs := map[string]struct{}{repoNodeID(graph.Opts.RepoKey): {}}
	affectedPkgIDs := map[string]struct{}{}
	for _, node := range oldNodes {
		removeNodeIDs[node.ID] = struct{}{}
		if strings.TrimSpace(node.Pkg) != "" {
			affectedPkgIDs[PackageID(graph.Opts.RepoKey, node.Pkg)] = struct{}{}
		}
	}

	baseAffectedNewIDs := map[string]struct{}{repoNodeID(graph.Opts.RepoKey): {}}
	for _, node := range graph.Nodes {
		if node.File != "" && changedSet[filepath.ToSlash(node.File)] {
			baseAffectedNewIDs[node.ID] = struct{}{}
			if strings.TrimSpace(node.Pkg) != "" {
				affectedPkgIDs[PackageID(graph.Opts.RepoKey, node.Pkg)] = struct{}{}
			}
		}
	}
	for pkgID := range affectedPkgIDs {
		removeNodeIDs[pkgID] = struct{}{}
		if _, ok := newNodesByID[pkgID]; ok {
			baseAffectedNewIDs[pkgID] = struct{}{}
		}
	}

	upsertNodeIDs := copyStringSet(baseAffectedNewIDs)
	var upsertEdges []Edge
	for _, edge := range graph.Edges {
		_, srcAffected := baseAffectedNewIDs[edge.Src]
		_, dstAffected := baseAffectedNewIDs[edge.Dst]
		if !srcAffected && !dstAffected {
			continue
		}
		// Both endpoints must resolve to nodes in the freshly built graph.
		// Otherwise inserting this edge would reference a node that is neither
		// in the patch's upsert set nor guaranteed to exist in the store, which
		// trips the edges->nodes foreign key and aborts the entire delta build.
		// The unaffected endpoint is added to the upsert set so it is (re)written
		// before the edge, which also self-heals stale rows left by an older
		// index whose node IDs no longer match the current builder.
		if _, ok := newNodesByID[edge.Src]; !ok {
			continue
		}
		if _, ok := newNodesByID[edge.Dst]; !ok {
			continue
		}
		upsertEdges = append(upsertEdges, edge)
		upsertNodeIDs[edge.Src] = struct{}{}
		upsertNodeIDs[edge.Dst] = struct{}{}
	}

	upsertNodes := make([]Node, 0, len(upsertNodeIDs))
	for id := range upsertNodeIDs {
		if node, ok := newNodesByID[id]; ok {
			upsertNodes = append(upsertNodes, node)
		}
	}
	sort.Slice(upsertNodes, func(i, j int) bool {
		return upsertNodes[i].ID < upsertNodes[j].ID
	})

	states := filterFileStatesByPath(buildFileStates(graph.Opts.RepoRoot, graph.Nodes, headSHA), changedSet)
	locators := filterLocatorsByPath(graph.Locators, changedSet)
	return graphPatch{
		RemoveNodeIDs:       sortedStringSet(removeNodeIDs),
		UpsertNodes:         upsertNodes,
		UpsertEdges:         upsertEdges,
		RemoveFilePaths:     changedPaths,
		UpsertFileStates:    states,
		RemoveLocatorFiles:  changedPaths,
		UpsertLocatorValues: locators,
	}, nil
}

func deltaChangedPaths(delta WorkspaceDelta) []string {
	paths := make([]string, 0, len(delta.Added)+len(delta.Modified)+len(delta.Deleted)+len(delta.Untracked))
	paths = append(paths, delta.Added...)
	paths = append(paths, delta.Modified...)
	paths = append(paths, delta.Deleted...)
	paths = append(paths, delta.Untracked...)
	paths = sortedStringSet(stringSet(paths))
	return paths
}

func filterFileStatesByPath(states []FileState, paths map[string]bool) []FileState {
	out := make([]FileState, 0, len(states))
	for _, state := range states {
		if paths[filepath.ToSlash(strings.TrimSpace(state.Path))] {
			out = append(out, state)
		}
	}
	return out
}

func filterLocatorsByPath(locators []LocatorEntry, paths map[string]bool) []LocatorEntry {
	out := make([]LocatorEntry, 0, len(locators))
	for _, locator := range locators {
		if paths[filepath.ToSlash(strings.TrimSpace(locator.FilePath))] {
			out = append(out, locator)
		}
	}
	return out
}

func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = filepath.ToSlash(strings.TrimSpace(value))
		value = strings.TrimPrefix(value, "./")
		value = strings.Trim(value, "/")
		if value == "" {
			continue
		}
		out[value] = true
	}
	return out
}

func copyStringSet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for value := range in {
		out[value] = struct{}{}
	}
	return out
}

func sortedStringSet[T ~bool | struct{}](set map[string]T) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
