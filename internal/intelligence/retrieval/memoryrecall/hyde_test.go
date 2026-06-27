package memoryrecall

import (
	"strings"
	"testing"
)

func TestDecomposeQueryModelKits(t *testing.T) {
	t.Parallel()

	probes := DecomposeQuery("Do I have any model kits?")
	if len(probes) < 2 {
		t.Fatalf("expected at least 2 probes for model kits question, got %d: %v", len(probes), probes)
	}
	// Original query should be first.
	if probes[0] != "Do I have any model kits?" {
		t.Fatalf("first probe should be original query, got %q", probes[0])
	}
	// Should contain a probe with "model" as a key noun.
	foundModel := false
	for _, p := range probes {
		if strings.Contains(strings.ToLower(p), "model") {
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Fatalf("expected a probe containing 'model', got %v", probes)
	}
}

func TestDecomposeQueryDegreeQuestion(t *testing.T) {
	t.Parallel()

	probes := DecomposeQuery("What degree did I get?")
	if len(probes) < 2 {
		t.Fatalf("expected at least 2 probes, got %d: %v", len(probes), probes)
	}
	// Should contain "degree" as a focused probe.
	foundDegree := false
	for _, p := range probes {
		if strings.EqualFold(p, "degree") {
			foundDegree = true
			break
		}
	}
	if !foundDegree {
		t.Fatalf("expected 'degree' as a standalone probe, got %v", probes)
	}
}

func TestDecomposeQueryMultiClauseSplit(t *testing.T) {
	t.Parallel()

	probes := DecomposeQuery("What degree did I get and from where?")
	if len(probes) < 3 {
		t.Fatalf("multi-clause query should produce at least 3 probes, got %d: %v", len(probes), probes)
	}
}

func TestDecomposeQueryPossessiveStripped(t *testing.T) {
	t.Parallel()

	probes := DecomposeQuery("How long is my daily commute?")
	// Should have a variant without "my".
	foundStripped := false
	for _, p := range probes {
		if !strings.Contains(strings.ToLower(p), "my ") && strings.Contains(strings.ToLower(p), "commute") {
			foundStripped = true
			break
		}
	}
	if !foundStripped {
		t.Fatalf("expected a possessive-stripped probe containing 'commute', got %v", probes)
	}
}

func TestDecomposeQueryEmpty(t *testing.T) {
	t.Parallel()

	if probes := DecomposeQuery(""); probes != nil {
		t.Fatalf("expected nil for empty query, got %v", probes)
	}
}

func TestDecomposeQueryCapped(t *testing.T) {
	t.Parallel()

	// Very long query should still cap at 6 probes.
	probes := DecomposeQuery("What degree did I get and from where and when and why and how?")
	if len(probes) > 6 {
		t.Fatalf("expected at most 6 probes, got %d: %v", len(probes), probes)
	}
}

func TestDecomposeQueryStagedPromptFormat(t *testing.T) {
	t.Parallel()

	// Should extract trailing Question: text from staged prompt.
	probes := DecomposeQuery("Some context here.\n\nQuestion: Do I have any model kits?")
	foundModel := false
	for _, p := range probes {
		if strings.Contains(strings.ToLower(p), "model") {
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Fatalf("expected model probe from staged prompt, got %v", probes)
	}
}
