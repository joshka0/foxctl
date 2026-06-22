package rlm

import (
	"context"
	"strings"
	"testing"
)

func TestExtractFallbackProbeQueriesDerivesNounsFromQuestion(t *testing.T) {
	t.Parallel()

	// Simple question: "Do I have any model kits?" -> probes should include "model kits"
	probes := extractFallbackProbeQueries("Question: Do I have any model kits?")
	if len(probes) == 0 {
		t.Fatalf("expected probes for model kits question, got none")
	}
	// The first probe should be the question text minus prefix
	found := false
	for _, p := range probes {
		if contains(probes, "model") || strings.Contains(p, "model") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a probe containing 'model', got %v", probes)
	}
}

func TestExtractFallbackProbeQueriesEmptyForNoQuestion(t *testing.T) {
	t.Parallel()

	probes := extractFallbackProbeQueries("")
	if probes != nil {
		t.Fatalf("expected nil probes for empty prompt, got %v", probes)
	}
}

func TestExtractFallbackProbeQueriesCappedAt3(t *testing.T) {
	t.Parallel()

	// Long question should cap at 3 probes
	probes := extractFallbackProbeQueries("What degree did I get and from where and when and why?")
	if len(probes) > 3 {
		t.Fatalf("expected at most 3 probes, got %d: %v", len(probes), probes)
	}
}

func TestSynthesisQueryIsEnumeration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query string
		want  bool
	}{
		{"How many items of clothing do I need to pick up?", true},
		{"How many days passed between visits?", true},
		{"What is the total number of meetings?", true},
		{"List all the projects I worked on", true},
		{"What degree did I graduate with?", false},
		{"Where did I redeem a coupon?", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := synthesisQueryIsEnumeration(tt.query); got != tt.want {
			t.Fatalf("synthesisQueryIsEnumeration(%q)=%v want %v", tt.query, got, tt.want)
		}
	}
}

func TestClassifySynthesisAnswerType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query string
		want  string
	}{
		{"How long ago did I visit MoMA?", "duration"},
		{"How many days between my MoMA and Met visits?", "duration"},
		{"How many days passed between my visit to MoMA and the Met?", "duration"},
		{"When did I graduate?", "temporal"},
		{"What date did I visit the museum?", "temporal"},
		{"What coupon did I redeem?", "fact"},
		{"What degree did I get?", "fact"},
		{"", "fact"},
	}
	for _, tt := range tests {
		if got := classifySynthesisAnswerType(tt.query); got != tt.want {
			t.Fatalf("classifySynthesisAnswerType(%q)=%q want %q", tt.query, got, tt.want)
		}
	}
}

func contains(slice []string, value string) bool {
	for _, s := range slice {
		if s == value {
			return true
		}
	}
	return false
}

func TestValidateRunRequestRequiresPrompt(t *testing.T) {
	t.Parallel()

	err := ValidateRunRequest(Task{}, Environment{})
	if err == nil || err.Error() != "rlm: prompt is required" {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateRunRequestRejectsWritableTools(t *testing.T) {
	t.Parallel()

	err := ValidateRunRequest(Task{Prompt: "inspect"}, Environment{
		Tools: []Tool{{Name: "write_file", ReadOnly: false}},
	})
	if err == nil || err.Error() != "rlm: first-version runtime only allows read-only tools" {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateRunRequestAllowsReadOnlyTools(t *testing.T) {
	t.Parallel()

	err := ValidateRunRequest(Task{Prompt: "inspect"}, Environment{
		Tools: []Tool{{Name: "retrieve_mixed", ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("ValidateRunRequest: %v", err)
	}
}

func TestRunFuncExecutes(t *testing.T) {
	t.Parallel()

	runner := RunFunc(func(_ context.Context, task Task, env Environment) (Result, error) {
		return Result{
			Answer:       task.Prompt,
			Iterations:   1,
			EvidenceRefs: []string{env.Tools[0].Name},
		}, nil
	})
	got, err := runner.Run(context.Background(), Task{Prompt: "inspect repo"}, Environment{
		Tools: []Tool{{Name: "retrieve_mixed", ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Answer != "inspect repo" {
		t.Fatalf("answer=%q", got.Answer)
	}
	if got.Iterations != 1 {
		t.Fatalf("iterations=%d", got.Iterations)
	}
	if len(got.EvidenceRefs) != 1 || got.EvidenceRefs[0] != "retrieve_mixed" {
		t.Fatalf("evidence=%v", got.EvidenceRefs)
	}
}
