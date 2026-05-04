package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

const VerifierArtifactSchemaV1 = "rlm.verifier.v1"

type VerifierArtifact struct {
	SchemaVersion     string                    `json:"schema_version"`
	AcceptedCandidate VerifierAcceptedCandidate `json:"accepted_candidate"`
	Checks            []VerifierCheck           `json:"checks"`
	Verified          bool                      `json:"verified"`
	FinalAnswer       string                    `json:"final_answer"`
}

type VerifierAcceptedCandidate struct {
	CandidateID string `json:"candidate_id"`
	Child       int    `json:"child,omitempty"`
	NodeID      string `json:"node_id,omitempty"`
	Answer      string `json:"answer"`
	AnswerHash  string `json:"answer_hash"`
}

type VerifierCheck struct {
	Name     string         `json:"name"`
	Pass     bool           `json:"pass"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

type VerifierCandidate struct {
	CandidateID string
	Child       int
	NodeID      string
	Answer      string
	AnswerHash  string
	Status      string
}

type VerifierArtifactError struct {
	Stage          string
	Message        string
	ExpectedSchema string
	RawExcerpt     string
	CandidateIDs   []string
}

func (e *VerifierArtifactError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("verifier artifact ")
	b.WriteString(strings.TrimSpace(e.Stage))
	b.WriteString(": ")
	b.WriteString(strings.TrimSpace(e.Message))
	if strings.TrimSpace(e.ExpectedSchema) != "" {
		b.WriteString("; expected_schema=")
		b.WriteString(strings.TrimSpace(e.ExpectedSchema))
	}
	if len(e.CandidateIDs) > 0 {
		b.WriteString("; candidate_ids=")
		b.WriteString(strings.Join(e.CandidateIDs, ","))
	}
	if strings.TrimSpace(e.RawExcerpt) != "" {
		b.WriteString("; raw_excerpt=")
		b.WriteString(strings.TrimSpace(e.RawExcerpt))
	}
	return b.String()
}

func ParseVerifierArtifactLine(text string) (VerifierArtifact, bool, error) {
	trimmed := strings.TrimSpace(text)
	const prefix = "VERIFIER_ARTIFACT_JSON="
	idx := strings.Index(trimmed, prefix)
	if idx < 0 {
		return VerifierArtifact{}, false, nil
	}
	raw := strings.TrimSpace(trimmed[idx+len(prefix):])
	if newline := strings.IndexByte(raw, '\n'); newline >= 0 {
		raw = strings.TrimSpace(raw[:newline])
	}
	var artifact VerifierArtifact
	if err := json.Unmarshal([]byte(raw), &artifact); err != nil {
		return VerifierArtifact{}, true, &VerifierArtifactError{
			Stage:          "parse",
			Message:        err.Error(),
			ExpectedSchema: verifierArtifactExpectedSchemaText(),
			RawExcerpt:     compactVerifierArtifactErrorText(raw, 800),
		}
	}
	return artifact, true, nil
}

func ValidateVerifierArtifact(artifact VerifierArtifact, candidates map[string]VerifierCandidate) error {
	if strings.TrimSpace(artifact.SchemaVersion) != VerifierArtifactSchemaV1 {
		return fmt.Errorf("verifier artifact schema_version=%q want %q", artifact.SchemaVersion, VerifierArtifactSchemaV1)
	}
	if !artifact.Verified {
		return fmt.Errorf("verifier artifact verified=false")
	}
	candidateID := strings.TrimSpace(artifact.AcceptedCandidate.CandidateID)
	if candidateID == "" {
		return fmt.Errorf("verifier artifact missing accepted_candidate.candidate_id")
	}
	candidate, ok := candidates[candidateID]
	if !ok {
		return fmt.Errorf("verifier artifact accepted unknown candidate_id %q", candidateID)
	}
	if status := strings.TrimSpace(candidate.Status); status != "" && status != "solved" {
		return fmt.Errorf("verifier artifact accepted candidate %q with status %q", candidateID, status)
	}
	if strings.TrimSpace(artifact.AcceptedCandidate.Answer) != strings.TrimSpace(candidate.Answer) {
		return fmt.Errorf("verifier artifact accepted_candidate.answer does not match registered candidate")
	}
	if strings.TrimSpace(artifact.AcceptedCandidate.AnswerHash) != strings.TrimSpace(candidate.AnswerHash) {
		return fmt.Errorf("verifier artifact accepted_candidate.answer_hash does not match registered candidate")
	}
	if candidate.Child > 0 && artifact.AcceptedCandidate.Child > 0 && artifact.AcceptedCandidate.Child != candidate.Child {
		return fmt.Errorf("verifier artifact accepted_candidate.child does not match registered candidate")
	}
	if strings.TrimSpace(candidate.NodeID) != "" && strings.TrimSpace(artifact.AcceptedCandidate.NodeID) != "" &&
		strings.TrimSpace(artifact.AcceptedCandidate.NodeID) != strings.TrimSpace(candidate.NodeID) {
		return fmt.Errorf("verifier artifact accepted_candidate.node_id does not match registered candidate")
	}
	if strings.TrimSpace(artifact.FinalAnswer) != strings.TrimSpace(candidate.Answer) {
		return fmt.Errorf("verifier artifact final_answer does not match registered candidate answer")
	}
	checks := map[string]VerifierCheck{}
	for _, check := range artifact.Checks {
		name := strings.TrimSpace(check.Name)
		if name == "" {
			return fmt.Errorf("verifier artifact has check with empty name")
		}
		if !check.Pass {
			return fmt.Errorf("verifier artifact check %q failed", name)
		}
		checks[name] = check
	}
	for _, required := range []string{
		"candidate_extracted",
		"format",
		"constraint_replay_or_recompute",
		"goal_or_requested_output",
	} {
		check, ok := checks[required]
		if !ok {
			return fmt.Errorf("verifier artifact missing required check %q", required)
		}
		if required == "goal_or_requested_output" {
			actual := strings.TrimSpace(fmt.Sprint(check.Evidence["actual"]))
			expected := strings.TrimSpace(fmt.Sprint(check.Evidence["expected"]))
			if actual == "" || actual == "<nil>" || expected == "" || expected == "<nil>" {
				return fmt.Errorf("verifier artifact goal_or_requested_output check missing actual/expected evidence")
			}
		}
	}
	return nil
}

func verifierArtifactExpectedSchemaText() string {
	return `{"schema_version":"rlm.verifier.v1","accepted_candidate":{"candidate_id":"child-N:sha256:...","child":N,"node_id":"root.N","answer":"solution = ...","answer_hash":"sha256:..."},"checks":[{"name":"candidate_extracted","pass":true,"evidence":{"candidate_id":"child-N:sha256:..."}},{"name":"format","pass":true,"evidence":{"expected":"solution = ...","actual":"solution = ..."}},{"name":"constraint_replay_or_recompute","pass":true,"evidence":{"method":"replay|recompute|simulate|typecheck|grounding","actual":"...","expected":"..."}},{"name":"goal_or_requested_output","pass":true,"evidence":{"actual":"...","expected":"...","comparison":"equality|satisfies|supported_by_refs"}}],"verified":true,"final_answer":"solution = ..."}`
}

func compactVerifierArtifactErrorText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	if limit <= 16 {
		return string(runes[:limit])
	}
	return string(runes[:limit-15]) + " ...[truncated]"
}
