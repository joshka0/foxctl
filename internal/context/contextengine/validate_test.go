package contextengine

import (
	"testing"
	"time"
)

// testValidator is a simple validator for testing ValidateAll.
type testValidator struct {
	err error
}

func (v testValidator) Validate() error {
	return v.err
}

func TestValidateAll(t *testing.T) {
	t.Parallel()

	t.Run("nil_entities", func(t *testing.T) {
		if err := ValidateAll(); err != nil {
			t.Errorf("expected nil for empty, got %v", err)
		}
	})

	t.Run("non_validator_ignored", func(t *testing.T) {
		if err := ValidateAll("not a validator", 42, nil); err != nil {
			t.Errorf("expected nil for non-Validator types, got %v", err)
		}
	})

	t.Run("valid_validators", func(t *testing.T) {
		validators := []testValidator{
			{},
			{},
		}
		if err := ValidateAll(validators[0], validators[1]); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("first_error_stops", func(t *testing.T) {
		validators := []testValidator{
			{},
			{err: errTest1},
			{},
		}
		err := ValidateAll(validators[0], validators[1], validators[2])
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("error_wraps_index", func(t *testing.T) {
		validators := []testValidator{
			{},
			{err: errTest1},
		}
		err := ValidateAll(validators[0], validators[1])
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("with_evidence_ref_validator", func(t *testing.T) {
		validRef := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
		if err := ValidateAll(validRef); err != nil {
			t.Errorf("expected valid EvidenceRef, got %v", err)
		}
	})

	t.Run("with_invalid_evidence_ref", func(t *testing.T) {
		invalidRef := EvidenceRef{Type: "bad", Ref: "x"}
		if err := ValidateAll(invalidRef); err == nil {
			t.Error("expected error for invalid EvidenceRef")
		}
	})

	t.Run("with_memory_claim_validator", func(t *testing.T) {
		validClaim := MemoryClaim{
			ID:          "claim-1",
			WorkspaceID: "ws-1",
			Status:      ClaimStatusCurrent,
		}
		if err := ValidateAll(validClaim); err != nil {
			t.Errorf("expected valid MemoryClaim, got %v", err)
		}
	})

	t.Run("with_task_context_validator", func(t *testing.T) {
		validTC := TaskContext{
			WorkspaceID: "ws-1",
			TaskID:      "task-1",
			ProjectionMeta: ProjectionMeta{
				ProjectionID:      "proj-1",
				ProjectionType:    "task_context",
				ProjectionVersion: 1,
				WorkspaceID:       "ws-1",
				GeneratedAt:       time.Now(),
			},
			UpdatedAt: time.Now(),
		}
		if err := ValidateAll(validTC); err != nil {
			t.Errorf("expected valid TaskContext, got %v", err)
		}
	})

	t.Run("mixed_validator_and_non_validator", func(t *testing.T) {
		validators := []testValidator{{}}
		validRef := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
		if err := ValidateAll(validators[0], "string", validRef); err != nil {
			t.Errorf("expected valid mixed, got %v", err)
		}
	})
}

func TestValidatorInterface(t *testing.T) {
	t.Parallel()
	// Verify that all expected types implement Validator
	var _ Validator = (ProjectionMeta)(ProjectionMeta{})
	var _ Validator = (WorkingSet)(WorkingSet{})
	var _ Validator = (TaskContext)(TaskContext{})
	var _ Validator = (ContextPacket)(ContextPacket{})
	var _ Validator = (EvidenceNode)(EvidenceNode{})
	var _ Validator = (EvidencePack)(EvidencePack{})
	var _ Validator = (ContextEvent)(ContextEvent{})
	var _ Validator = (MemoryClaim)(MemoryClaim{})
	var _ Validator = (StalenessMarker)(StalenessMarker{})
	var _ Validator = (ImpactEdge)(ImpactEdge{})
	var _ Validator = (RetrievalEpisode)(RetrievalEpisode{})
	var _ Validator = (RetrievalFeedback)(RetrievalFeedback{})
}

var errTest1 = assertTestError("test error 1")

func assertTestError(msg string) error {
	return &testError{msg: msg}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
