package retrievalv2

import "github.com/jkatigb/agentctl/internal/searchrank"

// Fuse combines per-source results into cross-source ranked hits.
func Fuse(sourceHits map[SourceID][]SourceHit, opts FuseOptions) ([]FusedHit, []FusedHit) {
	fused := searchrank.Fuse(sourceHits, opts)
	return fused, fused
}
