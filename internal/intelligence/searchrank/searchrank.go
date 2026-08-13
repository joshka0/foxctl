// Package searchrank re-exports the foxsearch searchrank module.
//
// The implementation moved to github.com/joshka0/foxsearch/searchrank as part
// of the foxsearch extraction; this shim preserves the foxctl import path for
// existing consumers. New code may import either path.
package searchrank

import "github.com/joshka0/foxsearch/searchrank"

type (
	// SourceID identifies a recall source in a fusion run.
	SourceID = searchrank.SourceID
	// FuseMode selects how per-source results are combined.
	FuseMode = searchrank.FuseMode
	// FuseOptions controls cross-source fusion.
	FuseOptions = searchrank.FuseOptions
	// SourceHit is one candidate from a single source.
	SourceHit[T any] = searchrank.SourceHit[T]
	// FusedHit is a fused candidate with per-source contributions.
	FusedHit[T any] = searchrank.FusedHit[T]
	// SourceContribution records one source's contribution to a fused hit.
	SourceContribution = searchrank.SourceContribution
	// Hit is a generic ranked hit used in fusion inputs.
	Hit = searchrank.Hit
)

const (
	// FuseModeRRF uses reciprocal-rank fusion.
	FuseModeRRF = searchrank.FuseModeRRF
	// FuseModeWeighted uses source-weighted score summation.
	FuseModeWeighted = searchrank.FuseModeWeighted
)

// Fuse combines per-source hits into a single ranked list.
func Fuse[T any](sourceHits map[SourceID][]SourceHit[T], opts FuseOptions) []FusedHit[T] {
	return searchrank.Fuse(sourceHits, opts)
}
