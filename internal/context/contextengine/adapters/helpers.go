package adapters

import (
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

// ParseStringRefs converts a slice of "type:value" strings into EvidenceRefs.
// It returns the first parse error encountered.
func ParseStringRefs(refs []string) ([]contextengine.EvidenceRef, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	result := make([]contextengine.EvidenceRef, 0, len(refs))
	for _, s := range refs {
		ref, err := contextengine.ParseEvidenceRef(s)
		if err != nil {
			return nil, fmt.Errorf("parse ref %q: %w", s, err)
		}
		result = append(result, ref)
	}
	return result, nil
}

// FormatStringRefs converts EvidenceRefs to "type:value" strings.
func FormatStringRefs(refs []contextengine.EvidenceRef) []string {
	if len(refs) == 0 {
		return nil
	}
	result := make([]string, 0, len(refs))
	for _, ref := range refs {
		s := contextengine.FormatEvidenceRef(ref)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

// mustTime parses a time string or returns a zero time.
func mustTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// InferRefType attempts to determine the EvidenceRef Type from a raw string
// that lacks a type prefix. It uses structural heuristics (file extension,
// known prefix patterns) — NOT keyword matching.
func InferRefType(raw string) contextengine.RefType {
	if raw == "" {
		return ""
	}
	// Check for known prefixes that indicate type.
	// This is structural parsing, not keyword classification.
	colonIdx := -1
	for i, ch := range raw {
		if ch == ':' {
			colonIdx = i
			break
		}
	}
	if colonIdx > 0 {
		prefix := contextengine.RefType(raw[:colonIdx])
		if prefix.IsValid() {
			return prefix
		}
	}
	// Fall back to path type for strings that look like file paths.
	return contextengine.RefTypePath
}

// ParseOrInferRef parses a string as "type:value" or infers the type
// and returns a best-effort EvidenceRef.
func ParseOrInferRef(s string) contextengine.EvidenceRef {
	if s == "" {
		return contextengine.EvidenceRef{}
	}
	ref, err := contextengine.ParseEvidenceRef(s)
	if err == nil {
		return ref
	}
	// No valid type prefix — infer.
	rt := InferRefType(s)
	if rt != "" {
		return contextengine.EvidenceRef{Type: rt, Ref: s}
	}
	return contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: s}
}

// ParseOrInferRefs converts mixed string refs using ParseOrInferRef.
func ParseOrInferRefs(refs []string) []contextengine.EvidenceRef {
	if len(refs) == 0 {
		return nil
	}
	result := make([]contextengine.EvidenceRef, 0, len(refs))
	for _, s := range refs {
		ref := ParseOrInferRef(s)
		if ref.Type != "" && ref.Ref != "" {
			result = append(result, ref)
		}
	}
	return result
}
