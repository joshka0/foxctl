package adapters

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	rlmruntime "github.com/joshka0/foxctl/internal/rlm/runtime"
)

// ConvertNodeResult converts an RLM runtime NodeResult to a contextengine.EvidencePack.
func ConvertNodeResult(workspaceID, query string, src rlmruntime.NodeResult) contextengine.EvidencePack {
	nodes := make([]contextengine.EvidenceNode, 0)
	for i, finding := range src.Findings {
		nodes = append(nodes, ConvertFinding(workspaceID, i, finding))
	}
	for i, ref := range src.EvidenceRefs {
		nodes = append(nodes, contextengine.EvidenceNode{
			ID:          fmt.Sprintf("evref_%s_%d", workspaceID, i),
			WorkspaceID: workspaceID,
			NodeType:    contextengine.EvidenceNodeTypeRetrieval,
			Ref:         ConvertRLMEvidenceRef(ref),
			Statement:   ref.Title,
		})
	}

	status := "ok"
	if src.Status != "" {
		status = string(src.Status)
	}

	return contextengine.EvidencePack{
		ID:          fmt.Sprintf("rlm_%s_%d", workspaceID, src.StartedAt.UnixNano()),
		WorkspaceID: workspaceID,
		Query:       query,
		Lane:        contextengine.LaneMixed,
		Nodes:       nodes,
		Telemetry: contextengine.EvidenceTelemetry{
			DurationMs: src.CompletedAt.Sub(src.StartedAt).Milliseconds(),
		},
		Metadata: map[string]any{
			"status":        status,
			"summary":       src.Summary,
			"answer":        src.Answer,
			"error_code":    src.ErrorCode,
			"error_message": src.ErrorMessage,
		},
	}
}

// ConvertFinding converts an RLM Finding to a contextengine.EvidenceNode.
func ConvertFinding(workspaceID string, index int, src rlmruntime.Finding) contextengine.EvidenceNode {
	var ref contextengine.EvidenceRef
	if len(src.EvidenceRefs) > 0 {
		ref = ConvertRLMEvidenceRef(src.EvidenceRefs[0])
	}
	if ref.Type == "" {
		ref = contextengine.EvidenceRef{
			Type: contextengine.RefTypeRun,
			Ref:  fmt.Sprintf("finding_%d", index),
		}
	}
	return contextengine.EvidenceNode{
		ID:          fmt.Sprintf("finding_%s_%d", workspaceID, index),
		WorkspaceID: workspaceID,
		NodeType:    contextengine.EvidenceNodeTypeContext,
		Ref:         ref,
		Statement:   src.Summary,
		Metadata:    src.Metadata,
	}
}

// ConvertRLMEvidenceRef converts an RLM EvidenceRef to a contextengine EvidenceRef.
func ConvertRLMEvidenceRef(src rlmruntime.EvidenceRef) contextengine.EvidenceRef {
	rt := mapRLMRefKind(src.Kind)
	return contextengine.EvidenceRef{
		Type:    rt,
		Ref:     src.Ref,
		Title:   src.Title,
		Excerpt: src.Excerpt,
	}
}

// mapRLMRefKind maps RLM evidence ref kinds to canonical RefType.
// Uses explicit switch, not keyword heuristics.
func mapRLMRefKind(kind string) contextengine.RefType {
	switch kind {
	case "path", "file":
		return contextengine.RefTypePath
	case "symbol", "function", "method", "class":
		return contextengine.RefTypeSymbol
	case "task":
		return contextengine.RefTypeTask
	case "session":
		return contextengine.RefTypeSession
	case "note", "document", "artifact":
		return contextengine.RefTypeNote
	case "commit":
		return contextengine.RefTypeCommit
	case "event":
		return contextengine.RefTypeEvent
	case "run":
		return contextengine.RefTypeRun
	case "tool_call":
		return contextengine.RefTypeToolCall
	default:
		return contextengine.RefTypePath
	}
}

// ConvertRLMArtifactRef converts an RLM ArtifactRef to a contextengine EvidenceRef.
// ArtifactRef metadata (id, media_type) is preserved in the Title field as structured text.
func ConvertRLMArtifactRef(src rlmruntime.ArtifactRef) contextengine.EvidenceRef {
	title := src.Summary
	if src.MediaType != "" {
		title = fmt.Sprintf("%s [%s]", src.Summary, src.MediaType)
	}
	return contextengine.EvidenceRef{
		Type:  contextengine.RefTypeArtifact,
		Ref:   src.URI,
		Title: title,
	}
}
