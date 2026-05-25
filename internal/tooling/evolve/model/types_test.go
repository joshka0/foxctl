package model

import (
	"math"
	"testing"
	"time"
)

func TestNodeValidateRejectsNonFiniteScore(t *testing.T) {
	tests := []struct {
		name  string
		score float64
	}{
		{name: "nan", score: math.NaN()},
		{name: "positive infinity", score: math.Inf(1)},
		{name: "negative infinity", score: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := validTestNode()
			node.Score = &tt.score

			if err := node.Validate(); err == nil {
				t.Fatalf("expected non-finite node score %s to be rejected", tt.name)
			}
		})
	}
}

func TestAttemptValidateRejectsNonFiniteScore(t *testing.T) {
	tests := []struct {
		name  string
		score float64
	}{
		{name: "nan", score: math.NaN()},
		{name: "positive infinity", score: math.Inf(1)},
		{name: "negative infinity", score: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempt := validTestAttempt()
			attempt.Score = &tt.score

			if err := attempt.Validate(); err == nil {
				t.Fatalf("expected non-finite attempt score %s to be rejected", tt.name)
			}
		})
	}
}

func TestValidateAllowsNilAndFiniteScores(t *testing.T) {
	node := validTestNode()
	if err := node.Validate(); err != nil {
		t.Fatalf("nil node score rejected: %v", err)
	}
	nodeScore := 0.42
	node.Score = &nodeScore
	if err := node.Validate(); err != nil {
		t.Fatalf("finite node score rejected: %v", err)
	}

	attempt := validTestAttempt()
	if err := attempt.Validate(); err != nil {
		t.Fatalf("nil attempt score rejected: %v", err)
	}
	attemptScore := -2.5
	attempt.Score = &attemptScore
	if err := attempt.Validate(); err != nil {
		t.Fatalf("finite attempt score rejected: %v", err)
	}
}

func validTestNode() Node {
	now := time.Date(2026, 4, 16, 6, 45, 0, 0, time.UTC)
	return Node{
		ID:        "node-1",
		RunID:     "run-1",
		Status:    NodeStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func validTestAttempt() Attempt {
	now := time.Date(2026, 4, 16, 6, 45, 0, 0, time.UTC)
	return Attempt{
		ID:        "attempt-1",
		NodeID:    "node-1",
		AttemptNo: 1,
		Status:    AttemptStatusCompleted,
		StartedAt: now,
	}
}
