package tools

import (
	"testing"
	"testing/quick"
)

func TestValidateSpawnDepthRejectsMalformedDepthState(t *testing.T) {
	tests := []struct {
		name                string
		parentDepth         int
		parentMaxDepth      int
		parentLocalMaxDepth int
	}{
		{
			name:                "negative parent depth",
			parentDepth:         -1,
			parentMaxDepth:      3,
			parentLocalMaxDepth: 3,
		},
		{
			name:                "zero global max depth",
			parentDepth:         0,
			parentMaxDepth:      0,
			parentLocalMaxDepth: 3,
		},
		{
			name:                "negative global max depth",
			parentDepth:         0,
			parentMaxDepth:      -1,
			parentLocalMaxDepth: 3,
		},
		{
			name:                "zero local max depth",
			parentDepth:         0,
			parentMaxDepth:      3,
			parentLocalMaxDepth: 0,
		},
		{
			name:                "negative local max depth",
			parentDepth:         0,
			parentMaxDepth:      3,
			parentLocalMaxDepth: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSpawnDepth(tt.parentDepth, tt.parentMaxDepth, tt.parentLocalMaxDepth); err == nil {
				t.Fatalf("ValidateSpawnDepth(%d, %d, %d) accepted malformed depth state",
					tt.parentDepth, tt.parentMaxDepth, tt.parentLocalMaxDepth)
			}
		})
	}
}

func TestValidateSpawnDepthPropertyAllowsOnlyStrictlyRemainingBudget(t *testing.T) {
	property := func(rawDepth, rawMax, rawLocal int) bool {
		parentDepth := boundedNonNegative(rawDepth, 8)
		parentMaxDepth := boundedNonNegative(rawMax, 8) + 1
		parentLocalMaxDepth := boundedNonNegative(rawLocal, 8) + 1

		gotAllowed := ValidateSpawnDepth(parentDepth, parentMaxDepth, parentLocalMaxDepth) == nil
		wantAllowed := parentDepth < parentMaxDepth && parentDepth < parentLocalMaxDepth

		return gotAllowed == wantAllowed
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("spawn depth budget property failed: %v", err)
	}
}

func TestComputeChildDepthLimitsPropertyNeverLoosensSubtreeLimit(t *testing.T) {
	property := func(rawDepth, rawMax, rawParentLocal, rawRequestedLocal int) bool {
		parentDepth := boundedNonNegative(rawDepth, 8)
		parentMaxDepth := boundedNonNegative(rawMax, 8) + 1
		parentLocalMaxDepth := boundedNonNegative(rawParentLocal, 8) + 1
		requestedLocalMaxDepth := boundedNonNegative(rawRequestedLocal, 10)

		childDepth, childMaxDepth, childLocalMaxDepth := ComputeChildDepthLimits(
			parentDepth,
			parentMaxDepth,
			parentLocalMaxDepth,
			requestedLocalMaxDepth,
		)

		if childDepth != parentDepth+1 || childMaxDepth != parentMaxDepth {
			return false
		}
		if childLocalMaxDepth > parentLocalMaxDepth {
			return false
		}
		if requestedLocalMaxDepth > 0 && requestedLocalMaxDepth < parentLocalMaxDepth {
			return childLocalMaxDepth == requestedLocalMaxDepth
		}
		return childLocalMaxDepth == parentLocalMaxDepth
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("child depth limit property failed: %v", err)
	}
}

func boundedNonNegative(n, max int) int {
	r := n % (max + 1)
	if r < 0 {
		return -r
	}
	return r
}
