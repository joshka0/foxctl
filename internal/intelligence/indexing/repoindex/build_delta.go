package repoindex

import (
	"context"
	"fmt"
)

const (
	deltaFullFallbackReason       = "partial_graph_rebuild_not_supported_for_global_edges_yet"
	deltaNoIndexedFileStateReason = "no_indexed_file_state"
)

// BuildDelta is the incremental indexing entrypoint. It accepts a precomputed
// workspace delta so callers can report freshness and skip clean rebuilds
// without racing a second status calculation.
//
// Today, non-empty deltas intentionally use a full rebuild. The graph builder
// resolves cross-file edges and package/repo rollups globally, so replacing only
// changed files would leave stale edges unless the store can repair every
// affected source and target set.
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
	return b.buildDeltaFullFallback(ctx, opts, delta, deltaFullFallbackReason)
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
