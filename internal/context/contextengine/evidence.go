package contextengine

import (
	"fmt"
	"time"
)

// EvidenceNodeType classifies the kind of evidence node.
type EvidenceNodeType string

const (
	EvidenceNodeTypeCode        EvidenceNodeType = "code"
	EvidenceNodeTypeMemory      EvidenceNodeType = "memory"
	EvidenceNodeTypeContext     EvidenceNodeType = "context"
	EvidenceNodeTypeTask        EvidenceNodeType = "task"
	EvidenceNodeTypeTrajectory  EvidenceNodeType = "trajectory"
	EvidenceNodeTypeObservation EvidenceNodeType = "observation"
	EvidenceNodeTypeTension     EvidenceNodeType = "tension"
	EvidenceNodeTypeRetrieval   EvidenceNodeType = "retrieval"
)

// IsValid reports whether n is a known EvidenceNodeType.
func (n EvidenceNodeType) IsValid() bool {
	switch n {
	case EvidenceNodeTypeCode, EvidenceNodeTypeMemory, EvidenceNodeTypeContext,
		EvidenceNodeTypeTask, EvidenceNodeTypeTrajectory, EvidenceNodeTypeObservation,
		EvidenceNodeTypeTension, EvidenceNodeTypeRetrieval:
		return true
	default:
		return false
	}
}

// Grounding describes how well a piece of evidence is grounded.
type Grounding string

const (
	GroundingLoaded    Grounding = "loaded"
	GroundingIndexed   Grounding = "indexed"
	GroundingSemantic  Grounding = "semantic"
	GroundingInferred  Grounding = "inferred"
	GroundingValidated Grounding = "validated"
)

// IsValid reports whether g is a known Grounding value.
func (g Grounding) IsValid() bool {
	switch g {
	case GroundingLoaded, GroundingIndexed, GroundingSemantic, GroundingInferred, GroundingValidated:
		return true
	default:
		return false
	}
}

// EvidenceNode is one piece of evidence with a ref and metadata.
type EvidenceNode struct {
	// ID is the unique node identifier.
	ID string `json:"id"`
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// NodeType classifies the evidence kind.
	NodeType EvidenceNodeType `json:"node_type"`
	// Ref points at the source material.
	Ref EvidenceRef `json:"ref"`
	// Statement is the extracted or derived content.
	Statement string `json:"statement,omitempty"`
	// Confidence is a 0-1 score.
	Confidence float64 `json:"confidence,omitempty"`
	// Grounding describes how well-grounded this evidence is.
	Grounding Grounding `json:"grounding,omitempty"`
	// Count is how many times this evidence has been seen.
	Count int `json:"count,omitempty"`
	// FirstSeen is when the evidence was first captured.
	FirstSeen time.Time `json:"first_seen,omitempty"`
	// LastSeen is when the evidence was last captured.
	LastSeen time.Time `json:"last_seen,omitempty"`
	// Metadata holds unstructured node metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Validate checks that the node has required fields.
func (n EvidenceNode) Validate() error {
	if n.ID == "" {
		return fmt.Errorf("evidence node: missing id")
	}
	if n.WorkspaceID == "" {
		return fmt.Errorf("evidence node: missing workspace_id")
	}
	if !n.NodeType.IsValid() {
		return fmt.Errorf("evidence node: unknown node_type %q", n.NodeType)
	}
	if err := ValidateEvidenceRef(n.Ref); err != nil {
		return fmt.Errorf("evidence node: %w", err)
	}
	if n.Grounding != "" && !n.Grounding.IsValid() {
		return fmt.Errorf("evidence node: unknown grounding %q", n.Grounding)
	}
	return nil
}

// EvidenceLane classifies which retrieval lane produced an EvidencePack.
type EvidenceLane string

const (
	LaneCode    EvidenceLane = "code"
	LaneMemory  EvidenceLane = "memory"
	LaneContext EvidenceLane = "context"
	LaneTask    EvidenceLane = "task"
	LaneMixed   EvidenceLane = "mixed"
)

// IsValid reports whether l is a known EvidenceLane.
func (l EvidenceLane) IsValid() bool {
	switch l {
	case LaneCode, LaneMemory, LaneContext, LaneTask, LaneMixed:
		return true
	default:
		return false
	}
}

// EvidenceTelemetry holds retrieval telemetry for an EvidencePack.
type EvidenceTelemetry struct {
	// DurationMs is how long the retrieval took in milliseconds.
	DurationMs int64 `json:"duration_ms,omitempty"`
	// TokensUsed is the approximate token count consumed.
	TokensUsed int `json:"tokens_used,omitempty"`
	// LanesFused is the number of lanes fused into this pack.
	LanesFused int `json:"lanes_fused,omitempty"`
}

// EvidencePack is a collection of evidence nodes returned by retrieval.
type EvidencePack struct {
	// ID is the unique pack identifier.
	ID string `json:"id"`
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// Query is the original retrieval query.
	Query string `json:"query"`
	// Lane is which retrieval lane produced this pack.
	Lane EvidenceLane `json:"lane"`
	// Nodes are the evidence items in this pack.
	Nodes []EvidenceNode `json:"nodes,omitempty"`
	// Telemetry is retrieval performance data.
	Telemetry EvidenceTelemetry `json:"telemetry,omitempty"`
	// Metadata holds unstructured pack metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Validate checks that the pack has required fields.
func (p EvidencePack) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("evidence pack: missing id")
	}
	if p.WorkspaceID == "" {
		return fmt.Errorf("evidence pack: missing workspace_id")
	}
	if p.Query == "" {
		return fmt.Errorf("evidence pack: missing query")
	}
	if !p.Lane.IsValid() {
		return fmt.Errorf("evidence pack: unknown lane %q", p.Lane)
	}
	for i, node := range p.Nodes {
		if err := node.Validate(); err != nil {
			return fmt.Errorf("evidence pack: node[%d]: %w", i, err)
		}
	}
	return nil
}
