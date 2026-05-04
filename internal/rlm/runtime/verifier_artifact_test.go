package runtime

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateVerifierArtifactAcceptsCandidateBoundArtifact(t *testing.T) {
	t.Parallel()

	candidates := map[string]VerifierCandidate{
		"child-2:sha256:abc": {
			CandidateID: "child-2:sha256:abc",
			Answer:      "solution = 42",
			AnswerHash:  "sha256:abc",
			Status:      "solved",
		},
	}
	artifact := VerifierArtifact{
		SchemaVersion: VerifierArtifactSchemaV1,
		AcceptedCandidate: VerifierAcceptedCandidate{
			CandidateID: "child-2:sha256:abc",
			Child:       2,
			Answer:      "solution = 42",
			AnswerHash:  "sha256:abc",
		},
		Checks: []VerifierCheck{
			{Name: "candidate_extracted", Pass: true, Evidence: map[string]any{"candidate_id": "child-2:sha256:abc"}},
			{Name: "format", Pass: true, Evidence: map[string]any{"expected": "solution =", "actual": "solution = 42"}},
			{Name: "constraint_replay_or_recompute", Pass: true, Evidence: map[string]any{"method": "recompute", "actual": "42", "expected": "42"}},
			{Name: "goal_or_requested_output", Pass: true, Evidence: map[string]any{"actual": "42", "expected": "42", "comparison": "equality"}},
		},
		Verified:    true,
		FinalAnswer: "solution = 42",
	}

	if err := ValidateVerifierArtifact(artifact, candidates); err != nil {
		t.Fatalf("ValidateVerifierArtifact() error = %v", err)
	}
}

func TestValidateVerifierArtifactRejectsUnknownCandidate(t *testing.T) {
	t.Parallel()

	artifact := minimalVerifierArtifact()
	artifact.AcceptedCandidate.CandidateID = "child-9:sha256:missing"

	err := ValidateVerifierArtifact(artifact, map[string]VerifierCandidate{
		"child-1:sha256:abc": {CandidateID: "child-1:sha256:abc", Answer: "solution = 42", AnswerHash: "sha256:abc", Status: "solved"},
	})
	if err == nil {
		t.Fatal("expected unknown candidate error")
	}
}

func TestValidateVerifierArtifactRejectsPartialCandidate(t *testing.T) {
	t.Parallel()

	artifact := minimalVerifierArtifact()
	err := ValidateVerifierArtifact(artifact, map[string]VerifierCandidate{
		"child-1:sha256:abc": {CandidateID: "child-1:sha256:abc", Answer: "solution = 42", AnswerHash: "sha256:abc", Status: "partial"},
	})
	if err == nil {
		t.Fatal("expected partial candidate error")
	}
}

func TestValidateVerifierArtifactRejectsMissingGoalEvidence(t *testing.T) {
	t.Parallel()

	artifact := minimalVerifierArtifact()
	for idx := range artifact.Checks {
		if artifact.Checks[idx].Name == "goal_or_requested_output" {
			artifact.Checks[idx].Evidence = map[string]any{"actual": "42"}
		}
	}
	err := ValidateVerifierArtifact(artifact, map[string]VerifierCandidate{
		"child-1:sha256:abc": {CandidateID: "child-1:sha256:abc", Answer: "solution = 42", AnswerHash: "sha256:abc", Status: "solved"},
	})
	if err == nil {
		t.Fatal("expected missing goal evidence error")
	}
}

func TestParseVerifierArtifactLine(t *testing.T) {
	t.Parallel()

	text := `stdout:
VERIFIER_ARTIFACT_JSON={"schema_version":"rlm.verifier.v1","accepted_candidate":{"candidate_id":"child-1:sha256:abc","answer":"solution = 42","answer_hash":"sha256:abc"},"checks":[],"verified":true,"final_answer":"solution = 42"}
`
	artifact, ok, err := ParseVerifierArtifactLine(text)
	if err != nil {
		t.Fatalf("ParseVerifierArtifactLine() error = %v", err)
	}
	if !ok {
		t.Fatal("expected artifact")
	}
	if artifact.AcceptedCandidate.CandidateID != "child-1:sha256:abc" {
		t.Fatalf("candidate_id=%q", artifact.AcceptedCandidate.CandidateID)
	}
}

func TestParseVerifierArtifactLineReturnsStructuredSchemaError(t *testing.T) {
	t.Parallel()

	_, ok, err := ParseVerifierArtifactLine(`VERIFIER_ARTIFACT_JSON={"schema_version":"rlm.verifier.v1","accepted_candidate":"child-1:sha256:abc","checks":["candidate_extracted"],"verified":true,"final_answer":"solution = 42"}`)
	if !ok {
		t.Fatal("expected artifact prefix")
	}
	var artifactErr *VerifierArtifactError
	if !errors.As(err, &artifactErr) {
		t.Fatalf("error=%T %v, want VerifierArtifactError", err, err)
	}
	if !strings.Contains(artifactErr.ExpectedSchema, `"accepted_candidate":{"candidate_id"`) {
		t.Fatalf("expected schema missing accepted_candidate object contract: %s", artifactErr.ExpectedSchema)
	}
	if !strings.Contains(artifactErr.RawExcerpt, `"accepted_candidate":"child-1`) {
		t.Fatalf("raw excerpt missing bad artifact: %s", artifactErr.RawExcerpt)
	}
}

func minimalVerifierArtifact() VerifierArtifact {
	return VerifierArtifact{
		SchemaVersion: VerifierArtifactSchemaV1,
		AcceptedCandidate: VerifierAcceptedCandidate{
			CandidateID: "child-1:sha256:abc",
			Child:       1,
			Answer:      "solution = 42",
			AnswerHash:  "sha256:abc",
		},
		Checks: []VerifierCheck{
			{Name: "candidate_extracted", Pass: true, Evidence: map[string]any{"candidate_id": "child-1:sha256:abc"}},
			{Name: "format", Pass: true, Evidence: map[string]any{"expected": "solution =", "actual": "solution = 42"}},
			{Name: "constraint_replay_or_recompute", Pass: true, Evidence: map[string]any{"method": "recompute", "actual": "42", "expected": "42"}},
			{Name: "goal_or_requested_output", Pass: true, Evidence: map[string]any{"actual": "42", "expected": "42", "comparison": "equality"}},
		},
		Verified:    true,
		FinalAnswer: "solution = 42",
	}
}
