package runtime

import (
	"errors"
	"strings"
	"testing"
	"testing/quick"
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

func TestValidateVerifierArtifactRejectsCandidateBindingMismatches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*VerifierArtifact)
		want   string
	}{
		{
			name: "answer_mismatch",
			mutate: func(artifact *VerifierArtifact) {
				artifact.AcceptedCandidate.Answer = "solution = 41"
			},
			want: "accepted_candidate.answer does not match",
		},
		{
			name: "answer_hash_mismatch",
			mutate: func(artifact *VerifierArtifact) {
				artifact.AcceptedCandidate.AnswerHash = "sha256:def"
			},
			want: "accepted_candidate.answer_hash does not match",
		},
		{
			name: "child_mismatch",
			mutate: func(artifact *VerifierArtifact) {
				artifact.AcceptedCandidate.Child = 2
			},
			want: "accepted_candidate.child does not match",
		},
		{
			name: "node_id_mismatch",
			mutate: func(artifact *VerifierArtifact) {
				artifact.AcceptedCandidate.NodeID = "root.2"
			},
			want: "accepted_candidate.node_id does not match",
		},
		{
			name: "final_answer_mismatch",
			mutate: func(artifact *VerifierArtifact) {
				artifact.FinalAnswer = "solution = 41"
			},
			want: "final_answer does not match",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			artifact := minimalVerifierArtifact()
			artifact.AcceptedCandidate.NodeID = "root.1"
			tc.mutate(&artifact)

			err := ValidateVerifierArtifact(artifact, map[string]VerifierCandidate{
				"child-1:sha256:abc": verifierCandidateForMinimalArtifact(),
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateVerifierArtifact() error=%v want %q", err, tc.want)
			}
		})
	}
}

func TestValidateVerifierArtifactRejectsFailedOrMissingRequiredChecks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*VerifierArtifact)
		want   string
	}{
		{
			name: "failed_check",
			mutate: func(artifact *VerifierArtifact) {
				for idx := range artifact.Checks {
					if artifact.Checks[idx].Name == "format" {
						artifact.Checks[idx].Pass = false
					}
				}
			},
			want: `check "format" failed`,
		},
		{
			name: "missing_constraint_check",
			mutate: func(artifact *VerifierArtifact) {
				artifact.Checks = removeVerifierCheck(artifact.Checks, "constraint_replay_or_recompute")
			},
			want: `missing required check "constraint_replay_or_recompute"`,
		},
		{
			name: "empty_check_name",
			mutate: func(artifact *VerifierArtifact) {
				artifact.Checks = append(artifact.Checks, VerifierCheck{Name: " ", Pass: true})
			},
			want: "check with empty name",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			artifact := minimalVerifierArtifact()
			tc.mutate(&artifact)

			err := ValidateVerifierArtifact(artifact, map[string]VerifierCandidate{
				"child-1:sha256:abc": verifierCandidateForMinimalArtifact(),
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateVerifierArtifact() error=%v want %q", err, tc.want)
			}
		})
	}
}

func TestValidateVerifierArtifactRejectsGeneratedUnknownCandidateIDs(t *testing.T) {
	t.Parallel()

	unknownCandidatesFailClosed := func(raw string) bool {
		artifact := minimalVerifierArtifact()
		artifact.AcceptedCandidate.CandidateID = "unknown:" + raw
		err := ValidateVerifierArtifact(artifact, map[string]VerifierCandidate{
			"child-1:sha256:abc": verifierCandidateForMinimalArtifact(),
		})
		return err != nil && strings.Contains(err.Error(), "accepted unknown candidate_id")
	}

	if err := quick.Check(unknownCandidatesFailClosed, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("generated unknown candidate ID was accepted: %v", err)
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

func verifierCandidateForMinimalArtifact() VerifierCandidate {
	return VerifierCandidate{
		CandidateID: "child-1:sha256:abc",
		Child:       1,
		NodeID:      "root.1",
		Answer:      "solution = 42",
		AnswerHash:  "sha256:abc",
		Status:      "solved",
	}
}

func removeVerifierCheck(checks []VerifierCheck, name string) []VerifierCheck {
	out := make([]VerifierCheck, 0, len(checks))
	for _, check := range checks {
		if check.Name != name {
			out = append(out, check)
		}
	}
	return out
}
