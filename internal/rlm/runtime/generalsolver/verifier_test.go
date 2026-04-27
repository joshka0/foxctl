package generalsolver

import (
	"testing"
)

func TestVerifierStackEmpty(t *testing.T) {
	stack := NewVerifierStack()
	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	artifact := WorkArtifact{Status: "solved", Answer: "42", Confidence: 0.9}
	verdict := stack.Verify(artifact, item)
	if !verdict.Accept {
		t.Error("expected accept with no checks")
	}
}

func TestVerifierStackAllPass(t *testing.T) {
	stack := NewVerifierStack()
	stack.AddCheck(VerifierTier1, SchemaCheck)
	stack.AddCheck(VerifierTier1, ConfidenceThresholdCheck(0.5))

	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	artifact := WorkArtifact{WorkItemID: "n1", Status: "solved", Answer: "42", Confidence: 0.9}
	verdict := stack.Verify(artifact, item)
	if !verdict.Accept {
		t.Errorf("expected accept, got reject: %+v", verdict)
	}
}

func TestVerifierStackSchemaFailure(t *testing.T) {
	stack := NewVerifierStack()
	stack.AddCheck(VerifierTier1, SchemaCheck)

	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	artifact := WorkArtifact{WorkItemID: "n1", Status: "solved", Answer: nil, Confidence: 0.9}
	verdict := stack.Verify(artifact, item)
	if verdict.Accept {
		t.Error("expected reject for nil answer with solved status")
	}
	if !verdict.Repairable {
		t.Error("expected repairable when schema check provides details")
	}
}

func TestVerifierStackConfidenceBelowThreshold(t *testing.T) {
	stack := NewVerifierStack()
	stack.AddCheck(VerifierTier1, ConfidenceThresholdCheck(0.8))

	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	artifact := WorkArtifact{WorkItemID: "n1", Status: "solved", Answer: "maybe", Confidence: 0.4}
	verdict := stack.Verify(artifact, item)
	if verdict.Accept {
		t.Error("expected reject for low confidence")
	}
}

func TestSchemaCheckValid(t *testing.T) {
	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	artifact := WorkArtifact{WorkItemID: "n1", Status: "solved", Answer: "42"}
	result := SchemaCheck(artifact, item)
	if !result.Pass {
		t.Errorf("expected pass, got: %v", result.Details)
	}
}

func TestSchemaCheckMismatchedID(t *testing.T) {
	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	artifact := WorkArtifact{WorkItemID: "n2", Status: "solved", Answer: "42"}
	result := SchemaCheck(artifact, item)
	if result.Pass {
		t.Error("expected fail for mismatched id")
	}
}

func TestSchemaCheckMissingAnswer(t *testing.T) {
	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	artifact := WorkArtifact{WorkItemID: "n1", Status: "solved", Answer: nil}
	result := SchemaCheck(artifact, item)
	if result.Pass {
		t.Error("expected fail for nil answer with solved status")
	}
}

func TestSchemaCheckPartialAllowed(t *testing.T) {
	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	artifact := WorkArtifact{WorkItemID: "n1", Status: "partial", Answer: nil}
	result := SchemaCheck(artifact, item)
	if !result.Pass {
		t.Error("expected pass for partial with nil answer (answer not required for partial)")
	}
}

func TestSchemaCheckEmptyStatus(t *testing.T) {
	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	artifact := WorkArtifact{WorkItemID: "n1", Status: "", Answer: "42"}
	result := SchemaCheck(artifact, item)
	if result.Pass {
		t.Error("expected fail for empty status")
	}
}

func TestConfidenceThresholdCheck(t *testing.T) {
	check := ConfidenceThresholdCheck(0.7)
	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}

	highConf := WorkArtifact{Confidence: 0.9}
	if result := check(highConf, item); !result.Pass {
		t.Error("expected pass for high confidence")
	}

	lowConf := WorkArtifact{Confidence: 0.3}
	if result := check(lowConf, item); result.Pass {
		t.Error("expected fail for low confidence")
	}
}

func TestMaxAttemptsCheck(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG, MaxAttempts: 2})
	check := MaxAttemptsCheck(state)

	item := state.Items["n1"]
	result := check(WorkArtifact{}, item)
	if !result.Pass {
		t.Error("expected pass when under max attempts")
	}

	// Simulate hitting max attempts by updating state directly
	item.Attempts = 2
	item.Status = StatusFailed
	state.Items["n1"] = item
	result = check(WorkArtifact{}, item)
	if result.Pass {
		t.Error("expected fail when at max attempts")
	}
}

func TestFormatCheck(t *testing.T) {
	check := FormatCheck("solution =")
	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}

	validArtifact := WorkArtifact{Answer: "solution = 42"}
	if result := check(validArtifact, item); !result.Pass {
		t.Error("expected pass for matching prefix")
	}

	invalidArtifact := WorkArtifact{Answer: "answer is 42"}
	if result := check(invalidArtifact, item); result.Pass {
		t.Error("expected fail for non-matching prefix")
	}

	nonStringArtifact := WorkArtifact{Answer: 42}
	if result := check(nonStringArtifact, item); result.Pass {
		t.Error("expected fail for non-string answer")
	}
}

func TestFormatCheckEmptyPrefix(t *testing.T) {
	check := FormatCheck("")
	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	result := check(WorkArtifact{Answer: "anything"}, item)
	if !result.Pass {
		t.Error("expected pass with empty prefix")
	}
}

func TestVerifierStackTierCounts(t *testing.T) {
	stack := NewVerifierStack()
	stack.AddCheck(VerifierTier1, SchemaCheck)
	stack.AddCheck(VerifierTier2, ConfidenceThresholdCheck(0.5))
	stack.AddCheck(VerifierTier3, ConfidenceThresholdCheck(0.9))

	t1, t2, t3 := stack.TierCounts()
	if t1 != 1 || t2 != 1 || t3 != 1 {
		t.Errorf("expected (1,1,1), got (%d,%d,%d)", t1, t2, t3)
	}
}

func TestVerifierFeedbackAccumulation(t *testing.T) {
	stack := NewVerifierStack()
	stack.AddCheck(VerifierTier1, ConfidenceThresholdCheck(0.8))
	stack.AddCheck(VerifierTier1, FormatCheck("solution ="))

	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	artifact := WorkArtifact{WorkItemID: "n1", Status: "solved", Answer: "maybe 42", Confidence: 0.6}
	verdict := stack.Verify(artifact, item)
	if verdict.Accept {
		t.Error("expected reject")
	}
	if len(verdict.Feedback) == 0 {
		t.Error("expected accumulated feedback")
	}
}
