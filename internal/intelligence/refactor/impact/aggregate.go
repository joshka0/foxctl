package impact

import (
	"sort"
	"strings"
)

type candidatePatch struct {
	path         string
	symbol       string
	lineHint     int
	bucket       Bucket
	score        int
	source       Source
	reason       string
	summary      string
	relationship TargetRelationship
}

type aggregator struct {
	byPath map[string]*Candidate
}

func newAggregator() *aggregator {
	return &aggregator{byPath: make(map[string]*Candidate)}
}

func (a *aggregator) add(patch candidatePatch) {
	path := cleanPath(patch.path)
	if path == "" {
		return
	}
	candidate, ok := a.byPath[path]
	if !ok {
		candidate = &Candidate{Path: path, Rank: patch.bucket}
		a.byPath[path] = candidate
	}
	candidate.Score += patch.score
	candidate.Rank = strongerBucket(candidate.Rank, patch.bucket)
	candidate.Sources = appendSource(candidate.Sources, patch.source)
	candidate.Reasons = appendString(candidate.Reasons, patch.reason)
	if patch.symbol != "" {
		candidate.Symbols = appendString(candidate.Symbols, patch.symbol)
	}
	if patch.lineHint > 0 && !containsInt(candidate.LineHints, patch.lineHint) {
		candidate.LineHints = append(candidate.LineHints, patch.lineHint)
		sort.Ints(candidate.LineHints)
	}
	if patch.summary != "" && candidate.Summary == "" {
		candidate.Summary = patch.summary
	}
	if patch.relationship.TargetKey != "" || patch.relationship.Target != "" || patch.relationship.Section != "" {
		candidate.TargetRelationships = appendRelationship(candidate.TargetRelationships, patch.relationship)
	}
}

func (a *aggregator) groups(limit int) map[Bucket][]Candidate {
	candidates := make([]Candidate, 0, len(a.byPath))
	for _, candidate := range a.byPath {
		sort.Slice(candidate.Sources, func(i, j int) bool { return candidate.Sources[i] < candidate.Sources[j] })
		sort.Strings(candidate.Reasons)
		sort.Strings(candidate.Symbols)
		sort.Slice(candidate.TargetRelationships, func(i, j int) bool {
			left, right := candidate.TargetRelationships[i], candidate.TargetRelationships[j]
			if left.TargetKey != right.TargetKey {
				return left.TargetKey < right.TargetKey
			}
			if left.Section != right.Section {
				return left.Section < right.Section
			}
			return left.Target < right.Target
		})
		candidates = append(candidates, *candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Rank != candidates[j].Rank {
			return bucketOrder(candidates[i].Rank) < bucketOrder(candidates[j].Rank)
		}
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Path != candidates[j].Path {
			return candidates[i].Path < candidates[j].Path
		}
		return strings.Join(candidates[i].Symbols, ",") < strings.Join(candidates[j].Symbols, ",")
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}

	groups := map[Bucket][]Candidate{
		BucketMustUpdate:       {},
		BucketShouldInspect:    {},
		BucketLikelyDuplicate:  {},
		BucketContractBoundary: {},
		BucketTestsToRun:       {},
		BucketDocsToUpdate:     {},
		BucketContextOnly:      {},
	}
	for _, candidate := range candidates {
		groups[candidate.Rank] = append(groups[candidate.Rank], candidate)
	}
	return groups
}

func strongerBucket(left, right Bucket) Bucket {
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	if bucketOrder(right) < bucketOrder(left) {
		return right
	}
	return left
}

func bucketOrder(bucket Bucket) int {
	switch bucket {
	case BucketMustUpdate:
		return 0
	case BucketContractBoundary:
		return 1
	case BucketTestsToRun:
		return 2
	case BucketDocsToUpdate:
		return 3
	case BucketLikelyDuplicate:
		return 4
	case BucketShouldInspect:
		return 5
	default:
		return 6
	}
}

func appendSource(values []Source, value Source) []Source {
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

func appendSources(values []Source, incoming ...Source) []Source {
	for _, value := range incoming {
		values = appendSource(values, value)
	}
	return values
}

func appendString(values []string, value string) []string {
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

func appendRelationship(values []TargetRelationship, value TargetRelationship) []TargetRelationship {
	value.EdgeTypes = uniqueSorted(value.EdgeTypes)
	for _, existing := range values {
		if existing.TargetKey == value.TargetKey &&
			existing.Target == value.Target &&
			existing.Section == value.Section &&
			existing.Depth == value.Depth &&
			strings.Join(existing.EdgeTypes, ",") == strings.Join(value.EdgeTypes, ",") {
			return values
		}
	}
	return append(values, value)
}

func containsInt(values []int, value int) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = appendString(out, value)
	}
	sort.Strings(out)
	return out
}
