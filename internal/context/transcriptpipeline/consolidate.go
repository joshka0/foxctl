package transcriptpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/transcriptcache"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

// PersistedMemory is one transcript-derived durable memory write result.
type PersistedMemory struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	CandidateType string `json:"candidate_type"`
	FrameIndex    int    `json:"frame_index"`
	SourceEventID int64  `json:"source_event_id"`
	Summary       string `json:"summary"`
}

// PersistClassifiedClaims writes consolidated typed claims to named memory.
func PersistClassifiedClaims(ctx context.Context, store storage.MemoryStore, parsed sourceimport.ParsedSession, conversationID, workspace string, objective *SessionObjective, claims []ClassifiedClaim) ([]PersistedMemory, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil, fmt.Errorf("transcriptpipeline: persist classified claims: workspace is required")
	}

	reports := make([]PersistedMemory, 0, len(claims))
	for _, claim := range claims {
		if !shouldPersistClassifiedClaim(claim, objective) {
			continue
		}
		memoryType, ok := transcriptMemoryTypeFromClaimKind(claim.Kind)
		if !ok {
			continue
		}
		summary := truncatePacketInline(strings.TrimSpace(claim.Text), 240)
		if summary == "" {
			continue
		}
		name := TranscriptMemoryName(parsed.SessionID, memoryType, claimIdentityText(claim))
		result, err := json.Marshal(map[string]any{
			"source":                    "sessions/classified-claims",
			"provider":                  parsed.Provider,
			"session_id":                parsed.SessionID,
			"conversation_id":           conversationID,
			"source_path":               parsed.SourcePath,
			"claim_kind":                claim.Kind,
			"claim_durability":          claim.Durability,
			"confidence":                claim.Confidence,
			"source_basis":              claim.SourceBasis,
			"tags":                      claim.Tags,
			"group_keys":                claim.GroupKeys,
			"evidence_frame_indices":    claim.EvidenceFrameIndices,
			"objective_role":            claim.ObjectiveRole,
			"objective_alignment_score": claim.ObjectiveScore,
			"objective_explanation":     claim.ObjectiveExplanation,
		})
		if err != nil {
			return nil, fmt.Errorf("transcriptpipeline: persist classified claim marshal: %w", err)
		}
		if err := SaveMemoryWithRetry(ctx, store, storage.NamedEntry{
			Name:      name,
			Type:      memoryType,
			Workspace: workspace,
			Summary:   summary,
			Result:    result,
			SessionID: parsed.SessionID,
		}); err != nil {
			return nil, fmt.Errorf("transcriptpipeline: persist classified claim save: %w", err)
		}
		frameIndex := 0
		if len(claim.EvidenceFrameIndices) > 0 {
			frameIndex = claim.EvidenceFrameIndices[0]
		}
		reports = append(reports, PersistedMemory{
			Name:          name,
			Type:          memoryType,
			CandidateType: "classified_claim:" + string(claim.Kind),
			FrameIndex:    frameIndex,
			Summary:       summary,
		})
	}
	return reports, nil
}

// PersistDurableTranscriptMemories writes durable transcript-derived candidates to named memory.
func PersistDurableTranscriptMemories(ctx context.Context, store storage.MemoryStore, parsed sourceimport.ParsedSession, conversationID, workspace string, derivations []companion.AnchoredMemoryDerivation) ([]PersistedMemory, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil, fmt.Errorf("transcriptpipeline: persist transcript memory: workspace is required")
	}

	reports := make([]PersistedMemory, 0)
	for _, derivation := range derivations {
		for _, candidate := range derivation.Candidates {
			if candidate.Scope != companion.CandidateScopeDurable {
				continue
			}
			if !shouldPersistTranscriptCandidate(candidate) {
				continue
			}
			memoryType, ok := transcriptMemoryType(candidate.Type)
			if !ok {
				continue
			}

			summary := truncatePacketInline(strings.TrimSpace(candidate.Text), 240)
			if summary == "" {
				continue
			}
			name := TranscriptMemoryName(parsed.SessionID, memoryType, candidate.Text)
			result, err := json.Marshal(map[string]any{
				"source":              "sessions/derive-memory",
				"provider":            parsed.Provider,
				"session_id":          parsed.SessionID,
				"conversation_id":     conversationID,
				"source_path":         parsed.SourcePath,
				"frame_index":         derivation.FrameIndex,
				"candidate_type":      candidate.Type,
				"memory_type":         memoryType,
				"scope":               candidate.Scope,
				"confidence":          candidate.Confidence,
				"rationale":           candidate.Rationale,
				"source_event_id":     candidate.SourceEventID,
				"source_label":        candidate.Source,
				"interaction_summary": derivation.InteractionSummary,
			})
			if err != nil {
				return nil, fmt.Errorf("transcriptpipeline: persist transcript memory marshal: %w", err)
			}

			if err := SaveMemoryWithRetry(ctx, store, storage.NamedEntry{
				Name:      name,
				Type:      memoryType,
				Workspace: workspace,
				Summary:   summary,
				Result:    result,
				SessionID: parsed.SessionID,
			}); err != nil {
				return nil, fmt.Errorf("transcriptpipeline: persist transcript memory save: %w", err)
			}
			reports = append(reports, PersistedMemory{
				Name:          name,
				Type:          memoryType,
				CandidateType: candidate.Type,
				FrameIndex:    derivation.FrameIndex,
				SourceEventID: candidate.SourceEventID,
				Summary:       summary,
			})
		}
	}
	return reports, nil
}

// PersistConsensusClaims writes durable group-level claims to named memory.
func PersistConsensusClaims(ctx context.Context, store storage.MemoryStore, parsed sourceimport.ParsedSession, conversationID, workspace string, claims []ConsensusClaim) ([]PersistedMemory, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil, fmt.Errorf("transcriptpipeline: persist consensus claims: workspace is required")
	}
	var reports []PersistedMemory
	for idx, claim := range claims {
		if !claim.PersistDurable {
			continue
		}
		summary := truncatePacketInline(strings.TrimSpace(claim.Text), 240)
		if summary == "" {
			continue
		}
		name := TranscriptMemoryName(parsed.SessionID, "learning", claim.Text)
		result, err := json.Marshal(map[string]any{
			"source":                  "sessions/derive-memory-group",
			"provider":                parsed.Provider,
			"session_id":              parsed.SessionID,
			"conversation_id":         conversationID,
			"source_path":             parsed.SourcePath,
			"candidate_type":          "group_topline_claim",
			"memory_type":             "learning",
			"support_sessions":        claim.SupportSessions,
			"support_count":           claim.SupportCount,
			"mainline_evidence_score": claim.MainlineEvidenceScore,
		})
		if err != nil {
			return nil, fmt.Errorf("transcriptpipeline: persist consensus claim marshal: %w", err)
		}
		if err := SaveMemoryWithRetry(ctx, store, storage.NamedEntry{
			Name:      name,
			Type:      "learning",
			Workspace: workspace,
			Summary:   summary,
			Result:    result,
			SessionID: parsed.SessionID,
		}); err != nil {
			return nil, fmt.Errorf("transcriptpipeline: persist consensus claim save: %w", err)
		}
		reports = append(reports, PersistedMemory{
			Name:          name,
			Type:          "learning",
			CandidateType: "group_topline_claim",
			FrameIndex:    idx,
			Summary:       summary,
		})
	}
	return reports, nil
}

// ReconcileMemoryPrefix removes stale transcript-derived memories that are not in keep.
func ReconcileMemoryPrefix(ctx context.Context, store storage.MemoryStore, workspace, prefix string, keep []PersistedMemory) ([]string, error) {
	workspace = strings.TrimSpace(workspace)
	prefix = strings.TrimSpace(prefix)
	if workspace == "" || prefix == "" {
		return nil, nil
	}

	keepSet := make(map[string]struct{}, len(keep))
	for _, item := range keep {
		keepSet[item.Name] = struct{}{}
	}

	var removed []string
	offset := 0
	for {
		entries, total, err := store.ListFiltered(ctx, workspace, storage.MemoryListFilter{Types: []string{"preference", "decision", "learning"}}, 200, offset)
		if err != nil {
			return nil, fmt.Errorf("transcriptpipeline: reconcile transcript memories list: %w", err)
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name, prefix) {
				continue
			}
			if _, ok := keepSet[entry.Name]; ok {
				continue
			}
			if err := store.Delete(ctx, entry.Name, workspace); err != nil {
				return nil, fmt.Errorf("transcriptpipeline: reconcile transcript memories delete %s: %w", entry.Name, err)
			}
			removed = append(removed, entry.Name)
		}
		offset += len(entries)
		if offset >= total || len(entries) == 0 {
			break
		}
	}
	sort.Strings(removed)
	return removed, nil
}

// TranscriptMemoryPrefix returns the stable prefix used for transcript-derived memories.
func TranscriptMemoryPrefix(sessionID string) string {
	return fmt.Sprintf("transcript:%s:", sessionID)
}

// TranscriptMemoryName returns a stable hashed name for a transcript-derived memory.
func TranscriptMemoryName(sessionID, memoryType, text string) string {
	digest := transcriptcache.DigestText(text)
	return TranscriptMemoryPrefix(sessionID) + fmt.Sprintf("%s:%s", memoryType, strings.TrimPrefix(digest, "sha256:"))
}

// SaveMemoryWithRetry retries transient SQLite busy errors during memory writes.
func SaveMemoryWithRetry(ctx context.Context, store storage.MemoryStore, entry storage.NamedEntry) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := store.Save(ctx, entry); err != nil {
			lastErr = err
			if !isSQLiteBusyError(err) {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
			}
			continue
		}
		return nil
	}
	return lastErr
}

func transcriptMemoryType(candidateType string) (string, bool) {
	switch strings.TrimSpace(candidateType) {
	case companion.EntryTypePreference:
		return "preference", true
	case companion.EntryTypeDecision:
		return "decision", true
	case companion.EntryTypeTechnicalContext:
		return "learning", true
	default:
		return "", false
	}
}

func transcriptMemoryTypeFromClaimKind(kind ClaimKind) (string, bool) {
	switch kind {
	case ClaimKindPreference:
		return "preference", true
	case ClaimKindDecision:
		return "decision", true
	case ClaimKindWorkflowRule, ClaimKindArchitecture:
		return "learning", true
	default:
		return "", false
	}
}

func shouldPersistTranscriptCandidate(candidate companion.AnchoredMemoryCandidate) bool {
	if candidate.Confidence < 0.72 {
		return false
	}
	switch strings.TrimSpace(candidate.Type) {
	case companion.EntryTypePreference, companion.EntryTypeDecision:
		return true
	case companion.EntryTypeTechnicalContext:
		return candidate.Source == "assistant_guidance" || candidate.Source == "assistant"
	default:
		return false
	}
}

func shouldPersistClassifiedClaim(claim ClassifiedClaim, objective *SessionObjective) bool {
	if claim.Durability != ClaimDurabilityDurable {
		return false
	}
	if normalizeClaimPromotionBlocker(claim.PromotionBlocker) != ClaimPromotionBlockerNone {
		return false
	}
	if claim.Confidence < 0.72 {
		return false
	}
	switch normalizeClassifiedSourceBasis(claim.SourceBasis) {
	case "user", "user_approved", "mixed":
	default:
		return false
	}
	role := normalizeObjectiveRole(claim.ObjectiveRole)
	action := normalizeObjectiveMemoryAction(claim.ObjectiveAction)
	score := claim.ObjectiveScore
	switch claim.Kind {
	case ClaimKindPreference, ClaimKindDecision, ClaimKindWorkflowRule, ClaimKindArchitecture:
		if objective != nil && strings.TrimSpace(objective.Objective) != "" {
			if action == ObjectiveMemoryActionPrune {
				return false
			}
			switch claim.Kind {
			case ClaimKindWorkflowRule, ClaimKindArchitecture:
				if role == ObjectiveRoleBlock {
					return false
				}
			}
			switch claim.Kind {
			case ClaimKindWorkflowRule, ClaimKindArchitecture:
				if role == ObjectiveRoleIrrelevant && score >= 0.58 {
					return false
				}
			}
			if role == ObjectiveRoleRedirect {
				return score >= 0.55
			}
			if role == ObjectiveRoleSupport {
				return score >= 0.55
			}
		}
		return true
	default:
		return false
	}
}

func claimIdentityText(claim ClassifiedClaim) string {
	if len(claim.GroupKeys) > 0 {
		return string(claim.Kind) + "|" + claim.GroupKeys[0]
	}
	return string(claim.Kind) + "|" + claim.Text
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "sqlite_busy")
}
