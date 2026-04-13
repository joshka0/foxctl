package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/jkatigb/agentctl/internal/intelligence/verification"
)

// applyDefaults sets default values for input fields (mirrors run function).
func applyDefaults(in *input) {
	if in.MaxVerifiers <= 0 {
		in.MaxVerifiers = 6
	}
}

// parseInput is a test helper that parses JSON and applies defaults.
func parseInput(r io.Reader) (input, error) {
	return skilltest.ParseInputWithDefaults[input](r, applyDefaults)
}

func TestParseInputDefaults(t *testing.T) {
	in, err := parseInput(strings.NewReader(`{"question":"q"}`))
	if err != nil {
		t.Fatalf("parseInput error: %v", err)
	}
	if in.MaxVerifiers != 6 {
		t.Fatalf("expected MaxVerifiers=6, got %d", in.MaxVerifiers)
	}
}

func TestConvertResultDurations(t *testing.T) {
	resp := &verification.CoVeResponse{
		Question:         "q",
		BaselineResponse: "baseline",
		Claims: []verification.Claim{{
			ID:       "c1",
			Text:     "Paris is the capital of France",
			Category: "factual",
		}},
		Verification: verification.BatchVerificationResult{
			Results: map[string]verification.VerificationResult{
				"c1": {
					ClaimID:   "c1",
					Claim:     "Paris is the capital of France",
					Verdict:   verification.VerdictTrue,
					Evidence:  "general knowledge",
					RawOutput: "Source: general knowledge -> Verdict: True",
					Duration:  1200 * time.Millisecond,
				},
			},
			TotalClaims:    1,
			VerifiedCount:  1,
			TrueCount:      1,
			FalseCount:     0,
			UncertainCount: 0,
			ErrorCount:     0,
			TotalDuration:  1500 * time.Millisecond,
			Parallelism:    1,
		},
		FinalAnswer: "Paris.",
		Metrics: verification.CoVeMetrics{
			BaselineDuration:     10 * time.Millisecond,
			ExtractionDuration:   20 * time.Millisecond,
			VerificationDuration: 30 * time.Millisecond,
			RefinementDuration:   40 * time.Millisecond,
			TotalDuration:        1000 * time.Millisecond,
		},
	}

	out := convertResult(resp)
	if out.Metrics.TotalMS != 1000 {
		t.Fatalf("expected metrics total ms=1000, got %d", out.Metrics.TotalMS)
	}
	vr, ok := out.Verification.Results["c1"]
	if !ok {
		t.Fatalf("missing c1 result")
	}
	if vr.Verdict != "True" {
		t.Fatalf("expected verdict True, got %q", vr.Verdict)
	}
	if vr.DurationMS != 1200 {
		t.Fatalf("expected duration ms=1200, got %d", vr.DurationMS)
	}
}
