package contextengine

import (
	"errors"
	"testing"
)

func TestErrors(t *testing.T) {
	t.Parallel()

	t.Run("ErrInvalidClaimStatus", func(t *testing.T) {
		if ErrInvalidClaimStatus == nil {
			t.Error("ErrInvalidClaimStatus should not be nil")
		}
		if ErrInvalidClaimStatus.Error() != "contextengine: invalid claim status" {
			t.Errorf("unexpected error message: %s", ErrInvalidClaimStatus.Error())
		}
	})

	t.Run("ErrInvalidStalenessStatus", func(t *testing.T) {
		if ErrInvalidStalenessStatus == nil {
			t.Error("ErrInvalidStalenessStatus should not be nil")
		}
		if ErrInvalidStalenessStatus.Error() != "contextengine: invalid staleness status" {
			t.Errorf("unexpected error message: %s", ErrInvalidStalenessStatus.Error())
		}
	})

	t.Run("ErrInvalidTransition", func(t *testing.T) {
		if ErrInvalidTransition == nil {
			t.Error("ErrInvalidTransition should not be nil")
		}
		if ErrInvalidTransition.Error() != "contextengine: invalid transition" {
			t.Errorf("unexpected error message: %s", ErrInvalidTransition.Error())
		}
	})

	t.Run("ClaimTransitionError_Unwrap", func(t *testing.T) {
		transErr := ClaimTransitionError{From: ClaimStatusCandidate, To: ClaimStatusSuperseded}
		if !errors.Is(transErr, ErrInvalidTransition) {
			t.Error("ClaimTransitionError should unwrap to ErrInvalidTransition")
		}
	})
}

func TestClaimTransitionError_Error(t *testing.T) {
	t.Parallel()
	err := ClaimTransitionError{
		From:   ClaimStatusCandidate,
		To:     ClaimStatusSuperseded,
		Reason: "test reason",
	}
	expected := "claim: invalid transition candidate -> superseded"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestStalenessTransitionError_Error(t *testing.T) {
	t.Parallel()
	err := StalenessTransitionError{
		From: StalenessStatusFresh,
		To:   StalenessStatusSuperseded,
	}
	expected := "staleness: invalid transition fresh -> superseded"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}
