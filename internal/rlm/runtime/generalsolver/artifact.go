package generalsolver

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ArtifactStatusSolved   = "solved"
	ArtifactStatusPartial  = "partial"
	ArtifactStatusBlocked  = "blocked"
	ArtifactStatusFailed   = "failed"
)

var validArtifactStatuses = map[string]bool{
	ArtifactStatusSolved:  true,
	ArtifactStatusPartial: true,
	ArtifactStatusBlocked: true,
	ArtifactStatusFailed:  true,
}

// ParseWorkArtifact parses a WorkArtifact from raw JSON bytes.
func ParseWorkArtifact(data []byte) (WorkArtifact, error) {
	if len(data) == 0 {
		return WorkArtifact{}, fmt.Errorf("generalsolver: empty artifact data")
	}
	var artifact WorkArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return WorkArtifact{}, fmt.Errorf("generalsolver: parse artifact JSON: %w", err)
	}
	return artifact, nil
}

// ParseWorkArtifactFromText extracts a WorkArtifact from model text by finding
// the first JSON object in the text.
func ParseWorkArtifactFromText(text string) (WorkArtifact, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return WorkArtifact{}, fmt.Errorf("generalsolver: empty artifact text")
	}
	// If the whole text is JSON, parse directly.
	if strings.HasPrefix(text, "{") {
		artifact, err := ParseWorkArtifact([]byte(text))
		if err == nil {
			return artifact, nil
		}
	}
	// Extract the first JSON object from the text.
	start := strings.Index(text, "{")
	if start < 0 {
		return WorkArtifact{}, fmt.Errorf("generalsolver: no JSON object found in artifact text")
	}
	depth := 0
	end := -1
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
				goto found
			}
		}
	}
found:
	if end < 0 {
		return WorkArtifact{}, fmt.Errorf("generalsolver: incomplete JSON object in artifact text")
	}
	return ParseWorkArtifact([]byte(text[start:end]))
}

// ValidateWorkArtifact checks the structural validity of a WorkArtifact.
func ValidateWorkArtifact(artifact WorkArtifact) error {
	if strings.TrimSpace(artifact.WorkItemID) == "" {
		return fmt.Errorf("generalsolver: artifact work_item_id is required")
	}
	if !validArtifactStatuses[artifact.Status] {
		return fmt.Errorf("generalsolver: artifact status %q is not valid (expected solved|partial|blocked|failed)", artifact.Status)
	}
	if artifact.Status == ArtifactStatusSolved && artifact.Answer == nil {
		return fmt.Errorf("generalsolver: artifact status is solved but answer is nil")
	}
	if artifact.Status == ArtifactStatusSolved && artifact.Confidence <= 0 {
		return fmt.Errorf("generalsolver: solved artifact confidence must be positive, got %.2f", artifact.Confidence)
	}
	return nil
}

// NormalizeCounterexamples ensures each counterexample is a non-nil map.
func NormalizeCounterexamples(artifact *WorkArtifact) {
	if artifact == nil {
		return
	}
	normalized := make([]map[string]any, 0, len(artifact.Counterexamples))
	for _, ce := range artifact.Counterexamples {
		if ce == nil {
			ce = make(map[string]any)
		}
		normalized = append(normalized, ce)
	}
	artifact.Counterexamples = normalized
}
