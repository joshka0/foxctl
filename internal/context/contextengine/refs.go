// Package contextengine provides canonical domain types for the unified context engine.
// All types are pure (no IO imports) and validated at construction boundaries.
package contextengine

import (
	"fmt"
	"strings"
)

// RefType is the kind of entity an EvidenceRef points at.
type RefType string

const (
	RefTypePath         RefType = "path"
	RefTypeSymbol       RefType = "symbol"
	RefTypeTask         RefType = "task"
	RefTypeSession      RefType = "session"
	RefTypeMemoryClaim  RefType = "memory_claim"
	RefTypeNote         RefType = "note"
	RefTypeArtifact     RefType = "artifact"
	RefTypeTrajectory   RefType = "trajectory"
	RefTypeCommit       RefType = "commit"
	RefTypeEvent        RefType = "event"
	RefTypeRun          RefType = "run"
	RefTypeToolCall     RefType = "tool_call"
)

// IsValid reports whether r is a known RefType.
func (r RefType) IsValid() bool {
	switch r {
	case RefTypePath, RefTypeSymbol, RefTypeTask, RefTypeSession,
		RefTypeMemoryClaim, RefTypeNote, RefTypeArtifact, RefTypeTrajectory,
		RefTypeCommit, RefTypeEvent, RefTypeRun, RefTypeToolCall:
		return true
	default:
		return false
	}
}

// EvidenceRef points at source material used in reasoning or retrieval.
type EvidenceRef struct {
	// Type is the kind of entity being referenced.
	Type RefType `json:"type"`
	// Ref is the opaque reference value (format depends on Type).
	Ref string `json:"ref"`
	// WorkspaceID is the owning workspace, used for normalization.
	WorkspaceID string `json:"workspace_id,omitempty"`
	// Title is an optional human-readable label.
	Title string `json:"title,omitempty"`
	// Excerpt is an optional snippet.
	Excerpt string `json:"excerpt,omitempty"`
}

// ParseEvidenceRef parses a string of the form "type:value" into an EvidenceRef.
func ParseEvidenceRef(s string) (EvidenceRef, error) {
	if s == "" {
		return EvidenceRef{}, fmt.Errorf("cannot parse empty evidence ref")
	}
	colonIdx := strings.IndexByte(s, ':')
	if colonIdx <= 0 {
		return EvidenceRef{}, fmt.Errorf("missing type prefix in %q", s)
	}
	rawType := s[:colonIdx]
	ref := s[colonIdx+1:]
	if ref == "" {
		return EvidenceRef{}, fmt.Errorf("empty ref value in %q", s)
	}
	rt := RefType(rawType)
	if !rt.IsValid() {
		return EvidenceRef{}, fmt.Errorf("unknown ref type %q", rawType)
	}
	return EvidenceRef{Type: rt, Ref: ref}, nil
}

// FormatEvidenceRef formats an EvidenceRef as a "type:value" string.
func FormatEvidenceRef(ref EvidenceRef) string {
	if ref.Type == "" || ref.Ref == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", ref.Type, ref.Ref)
}

// NormalizeEvidenceRef fills in the WorkspaceID if missing.
func NormalizeEvidenceRef(ref EvidenceRef, workspaceID string) EvidenceRef {
	if ref.WorkspaceID == "" && workspaceID != "" {
		ref.WorkspaceID = workspaceID
	}
	return ref
}

// ValidateEvidenceRef checks that the ref has a valid type and non-empty ref value.
func ValidateEvidenceRef(ref EvidenceRef) error {
	if !ref.Type.IsValid() {
		return fmt.Errorf("invalid ref type: %q", ref.Type)
	}
	if ref.Ref == "" {
		return fmt.Errorf("empty ref value")
	}
	return nil
}

// Equal reports whether two EvidenceRefs have the same identity.
func (ref EvidenceRef) Equal(other EvidenceRef) bool {
	return ref.Type == other.Type && ref.Ref == other.Ref
}

// Validate implements the Validator interface by delegating to ValidateEvidenceRef.
func (ref EvidenceRef) Validate() error {
	return ValidateEvidenceRef(ref)
}
