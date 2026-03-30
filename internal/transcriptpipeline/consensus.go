package transcriptpipeline

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/storage/transcriptcache"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

// ArtifactCacheReport describes one cached pipeline artifact.
type ArtifactCacheReport struct {
	ArtifactKind   string `json:"artifact_kind"`
	NormalizedHash string `json:"normalized_hash"`
	SourceHash     string `json:"source_hash"`
	DerivationMode string `json:"derivation_mode"`
	ModelID        string `json:"model_id"`
	CacheHit       bool   `json:"cache_hit"`
	SummaryPreview string `json:"summary_preview"`
}

// ConsensusClaim is one polished claim ready for scoring and consolidation.
type ConsensusClaim struct {
	Text                  string   `json:"text"`
	SupportSessions       []string `json:"support_sessions,omitempty"`
	SupportCount          int      `json:"support_count"`
	MainlineEvidenceScore float64  `json:"mainline_evidence_score"`
	PersistDurable        bool     `json:"persist_durable"`
}

// BuildGroupTopline produces one cached sidecar-derived prose topline.
func BuildGroupTopline(ctx context.Context, sidecars []SidecarPacket, cacheStore *transcriptcache.Store, opts WorkerConfig, mode, promptVersion string, timeoutSeconds int) (string, *ArtifactCacheReport, error) {
	if len(sidecars) == 0 {
		return "", nil, nil
	}
	parts := make([]string, 0, len(sidecars))
	for _, packet := range sidecars {
		text := strings.TrimSpace(packet.SummaryText)
		if text == "" {
			continue
		}
		label := firstNonEmpty(packet.AgentNickname, packet.AgentRole, packet.SessionID)
		parts = append(parts, "["+label+"] "+text)
	}
	if len(parts) == 0 {
		return "", nil, nil
	}

	normalized := strings.Join(parts, "\n\n")
	sourceHash := transcriptcache.DigestText(normalized)
	normalizedHash := transcriptcache.DigestText(normalized)
	modelID := resolveModelID(mode, opts)
	if promptVersion == "" {
		promptVersion = "group_topline_summary_v2"
	}

	if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, "group_topline", normalizedHash, promptVersion, modelID); err != nil {
		return "", nil, err
	} else if hit {
		return entry.Summary, &ArtifactCacheReport{
			ArtifactKind:   "group_topline",
			NormalizedHash: normalizedHash,
			SourceHash:     sourceHash,
			DerivationMode: entry.DerivationMode,
			ModelID:        entry.ModelID,
			CacheHit:       true,
			SummaryPreview: truncatePacketInline(entry.Summary, 140),
		}, nil
	}

	deterministic := summarizeGroupToplineDeterministic(parts)
	entry := transcriptcache.Entry{
		ArtifactKind:   "group_topline",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		PromptVersion:  promptVersion,
		SourcePreview:  truncatePacketInline(normalized, 120),
	}

	switch normalizeMode(mode) {
	case "deterministic":
		entry.DerivationMode = "deterministic"
		entry.ModelID = deterministicModelID()
		entry.Summary = deterministic
	default:
		result, err := RunLLMTask(ctx, workerConfigWithTimeout(opts, timeoutSeconds), Task{
			Stage:         StageDerive,
			InputKind:     "group_topline",
			PromptVersion: promptVersion,
			SystemPrompt:  "Synthesize one topline from sidecar subagent findings. Return exactly one complete sentence under 35 words. Focus on the main architectural or investigative takeaway that should guide the mainline conversation.",
			ArtifactText:  normalized,
			MaxTokens:     180,
		})
		if err != nil {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
			entry.Summary = deterministic
		} else {
			entry.DerivationMode = "lmstudio"
			entry.ModelID = result.ModelID
			entry.Summary = result.OutputText
		}
	}

	if err := cacheStore.Put(ctx, entry); err != nil {
		return "", nil, err
	}
	return entry.Summary, &ArtifactCacheReport{
		ArtifactKind:   "group_topline",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: entry.DerivationMode,
		ModelID:        entry.ModelID,
		CacheHit:       false,
		SummaryPreview: truncatePacketInline(entry.Summary, 140),
	}, nil
}

// BuildStructuredToplineClaims turns sidecar packets into cached durable claim candidates.
func BuildStructuredToplineClaims(ctx context.Context, sidecars []SidecarPacket, cacheStore *transcriptcache.Store, opts WorkerConfig, mode, promptVersion string, timeoutSeconds int) ([]ConsensusClaim, *ArtifactCacheReport, error) {
	if len(sidecars) == 0 {
		return nil, nil, nil
	}
	parts := make([]string, 0, len(sidecars))
	for _, packet := range sidecars {
		text := strings.TrimSpace(packet.SummaryText)
		if text == "" {
			continue
		}
		label := firstNonEmpty(packet.AgentNickname, packet.AgentRole, packet.SessionID)
		parts = append(parts, "["+label+"] "+text)
	}
	if len(parts) == 0 {
		return nil, nil, nil
	}

	normalized := strings.Join(parts, "\n\n")
	sourceHash := transcriptcache.DigestText(normalized)
	normalizedHash := transcriptcache.DigestText(normalized)
	modelID := resolveModelID(mode, opts)
	if promptVersion == "" {
		promptVersion = "group_topline_claims_v3"
	}

	if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, "group_topline_claims", normalizedHash, promptVersion, modelID); err != nil {
		return nil, nil, err
	} else if hit {
		claims := DecodeToplineClaims(entry.Summary)
		return claims, &ArtifactCacheReport{
			ArtifactKind:   "group_topline_claims",
			NormalizedHash: normalizedHash,
			SourceHash:     sourceHash,
			DerivationMode: entry.DerivationMode,
			ModelID:        entry.ModelID,
			CacheHit:       true,
			SummaryPreview: truncatePacketInline(entry.Summary, 140),
		}, nil
	}

	claimTexts := PolishConsensusClaimTexts(ExtractConsensusClaimTexts(normalized))
	derivationMode := "deterministic"
	finalModelID := deterministicModelID()
	if normalizeMode(mode) != "deterministic" {
		result, err := RunLLMTask(ctx, workerConfigWithTimeout(opts, timeoutSeconds), Task{
			Stage:         StagePolish,
			InputKind:     "group_topline_claims",
			PromptVersion: promptVersion,
			SystemPrompt:  "Extract up to 4 durable project-memory claims from these sidecar findings. Return only valid JSON of the form {\"claims\":[\"...\"]}. Prefer architecture decisions, memory-pipeline doctrine, and workflow rules. Avoid file inventory, storage trivia, and implementation chatter. Each claim must be a complete standalone sentence under 120 characters.",
			ArtifactText:  normalized,
			MaxTokens:     260,
		})
		if err == nil {
			if decoded := DecodeToplineClaimTexts(result.OutputText); len(decoded) > 0 {
				claimTexts = PolishConsensusClaimTexts(decoded)
				derivationMode = "lmstudio"
				finalModelID = result.ModelID
			}
		}
	}

	entry := transcriptcache.Entry{
		ArtifactKind:   "group_topline_claims",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: derivationMode,
		ModelID:        finalModelID,
		PromptVersion:  promptVersion,
		Summary:        EncodeToplineClaimTexts(claimTexts),
		SourcePreview:  truncatePacketInline(normalized, 120),
	}
	if err := cacheStore.Put(ctx, entry); err != nil {
		return nil, nil, err
	}

	claims := make([]ConsensusClaim, 0, len(claimTexts))
	for _, claim := range claimTexts {
		claims = append(claims, ConsensusClaim{Text: claim, SupportCount: 1})
	}
	return claims, &ArtifactCacheReport{
		ArtifactKind:   "group_topline_claims",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: derivationMode,
		ModelID:        finalModelID,
		CacheHit:       false,
		SummaryPreview: truncatePacketInline(entry.Summary, 140),
	}, nil
}

// DeriveConsensusClaims scores polished sidecar claims against full mainline evidence.
func DeriveConsensusClaims(sidecars []SidecarPacket, parsed sourceimport.ParsedSession, frames []companion.AnchoredInteractionFrame, derivations []companion.AnchoredMemoryDerivation, toplineClaims []ConsensusClaim) []ConsensusClaim {
	sidecarClaims := map[string]map[string]struct{}{}
	for _, packet := range sidecars {
		for _, claim := range PolishConsensusClaimTexts(ExtractConsensusClaimTexts(packet.SummaryText)) {
			key := strings.ToLower(strings.TrimSpace(claim))
			if key == "" {
				continue
			}
			if sidecarClaims[key] == nil {
				sidecarClaims[key] = make(map[string]struct{})
			}
			sidecarClaims[key][packet.SessionID] = struct{}{}
		}
	}

	mainlineCorpus := BuildMainlineEvidenceCorpus(parsed, frames, derivations)
	var out []ConsensusClaim
	for _, toplineClaim := range toplineClaims {
		key := strings.ToLower(strings.TrimSpace(toplineClaim.Text))
		support := sidecarClaims[key]
		if len(support) == 0 {
			support = make(map[string]struct{})
		}
		for _, packet := range sidecars {
			if claimSupportScore(toplineClaim.Text, packet.SummaryText) >= 0.35 {
				support[packet.SessionID] = struct{}{}
			}
		}
		if len(support) == 0 {
			continue
		}
		sessions := make([]string, 0, len(support))
		for sessionID := range support {
			sessions = append(sessions, sessionID)
		}
		sort.Strings(sessions)

		score := BestCorpusOverlap(toplineClaim.Text, mainlineCorpus)
		claim := ConsensusClaim{
			Text:                  toplineClaim.Text,
			SupportSessions:       sessions,
			SupportCount:          len(sessions),
			MainlineEvidenceScore: score,
			PersistDurable:        len(sessions) >= 2 && score >= 0.2,
		}
		out = append(out, claim)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PersistDurable != out[j].PersistDurable {
			return out[i].PersistDurable
		}
		if out[i].SupportCount != out[j].SupportCount {
			return out[i].SupportCount > out[j].SupportCount
		}
		return out[i].MainlineEvidenceScore > out[j].MainlineEvidenceScore
	})
	return out
}

func summarizeGroupToplineDeterministic(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return "Group topline: " + strings.Join(parts, " || ")
}

func ExtractConsensusClaimTexts(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	text = strings.TrimPrefix(text, "Group topline: ")
	segments := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == ';'
	})
	var claims []string
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		for _, part := range strings.Split(segment, "||") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if idx := strings.Index(part, "] "); idx >= 0 {
				part = strings.TrimSpace(part[idx+2:])
			}
			if idx := strings.Index(part, " - "); idx >= 0 && idx < 40 {
				part = strings.TrimSpace(part[idx+3:])
			}
			lower := strings.ToLower(part)
			if len(part) < 40 {
				continue
			}
			if strings.Contains(lower, "tool_result:") || strings.Contains(lower, "command: /bin/") {
				continue
			}
			if strings.HasPrefix(lower, "i’m ") || strings.HasPrefix(lower, "i'm ") {
				continue
			}
			part = truncatePacketInline(part, 220)
			claims = append(claims, part)
		}
	}
	return dedupeClaims(claims)
}

func PolishConsensusClaimTexts(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	var out []string
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if polished, ok := canonicalizeClaim(item); ok {
			out = append(out, polished)
			continue
		}
		if acceptableClaim(item) {
			out = append(out, truncatePacketInline(item, 140))
		}
	}
	return dedupeClaims(out)
}

func DecodeToplineClaimTexts(raw string) []string {
	var payload struct {
		Claims []string `json:"claims"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return nil
	}
	return dedupeClaims(payload.Claims)
}

func DecodeToplineClaims(raw string) []ConsensusClaim {
	var out []ConsensusClaim
	for _, claim := range DecodeToplineClaimTexts(raw) {
		out = append(out, ConsensusClaim{Text: claim, SupportCount: 1})
	}
	return out
}

func EncodeToplineClaimTexts(claims []string) string {
	payload := struct {
		Claims []string `json:"claims"`
	}{Claims: dedupeClaims(claims)}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return `{"claims":[]}`
	}
	return string(bytes)
}

func BuildMainlineEvidenceCorpus(parsed sourceimport.ParsedSession, frames []companion.AnchoredInteractionFrame, derivations []companion.AnchoredMemoryDerivation) []string {
	var corpus []string
	for _, turn := range parsed.Turns {
		if text := strings.TrimSpace(turn.Prompt); text != "" {
			corpus = append(corpus, text)
		}
		if text := strings.TrimSpace(turn.FinalOutput.Text); text != "" {
			corpus = append(corpus, text)
		}
	}
	for _, frame := range frames {
		corpus = append(corpus, frame.UserEvent.Content, frame.AssistantEvent.Content)
		if frame.FollowUpUser != nil {
			corpus = append(corpus, frame.FollowUpUser.Content)
		}
	}
	for _, derivation := range derivations {
		corpus = append(corpus, derivation.InteractionSummary)
		for _, candidate := range derivation.Candidates {
			if candidate.Scope == companion.CandidateScopeDurable {
				corpus = append(corpus, candidate.Text)
			}
		}
	}
	return corpus
}

func BestCorpusOverlap(claim string, corpus []string) float64 {
	best := 0.0
	claimTokens := tokenizeText(claim)
	for _, item := range corpus {
		score := overlapScore(claimTokens, tokenizeText(item))
		if score > best {
			best = score
		}
	}
	return best
}

func tokenizeText(text string) map[string]struct{} {
	text = strings.ToLower(strings.TrimSpace(text))
	tokens := strings.FieldsFunc(text, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	out := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if len(token) < 3 {
			continue
		}
		out[token] = struct{}{}
	}
	return out
}

func overlapScore(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for token := range a {
		if _, ok := b[token]; ok {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	union := len(a)
	for token := range b {
		if _, ok := a[token]; !ok {
			union++
		}
	}
	return float64(intersection) / float64(union)
}

func claimSupportScore(claim, evidence string) float64 {
	claimTokens := tokenizeText(claim)
	evidenceTokens := tokenizeText(evidence)
	if len(claimTokens) == 0 || len(evidenceTokens) == 0 {
		return 0
	}
	intersection := 0
	for token := range claimTokens {
		if _, ok := evidenceTokens[token]; ok {
			intersection++
		}
	}
	return float64(intersection) / float64(len(claimTokens))
}

func dedupeClaims(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, item := range in {
		key := strings.ToLower(strings.TrimSpace(item))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func canonicalizeClaim(text string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.Contains(lower, "second-pass") && (strings.Contains(lower, "hybrid") || strings.Contains(lower, "runtime") || strings.Contains(lower, "daemon")):
		return "Implement auto-memory as a second-pass consolidator over the existing hybrid companion runtime.", true
	case strings.Contains(lower, "named_memory") && strings.Contains(lower, "session_id"):
		return "Use named_memory as the durable sink for transcript-derived memories.", true
	case (strings.Contains(lower, "ordered turn pairs") || strings.Contains(lower, "interaction packet") || strings.Contains(lower, "candidate memories")) && strings.Contains(lower, "consolidation"):
		return "Derive transcript memories from ordered interaction packets before running consolidation.", true
	case strings.Contains(lower, "append-only") && (strings.Contains(lower, "proposal") || strings.Contains(lower, "proposal-driven")):
		return "Keep memory updates append-only and proposal-driven before consolidation applies them.", true
	}
	return "", false
}

func acceptableClaim(text string) bool {
	text = strings.TrimSpace(text)
	if len(text) < 60 || len(text) > 160 {
		return false
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "the existing ") || strings.HasPrefix(lower, "the main insight ") || strings.HasPrefix(lower, "what’s missing") || strings.HasPrefix(lower, "what's missing") || strings.HasPrefix(lower, "what is missing") {
		return false
	}
	if strings.Contains(lower, "tool_result:") || strings.Contains(lower, "/users/") || strings.Contains(lower, "command: /bin/") {
		return false
	}
	return true
}

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto":
		return "auto"
	case "deterministic":
		return "deterministic"
	case "lmstudio":
		return "lmstudio"
	default:
		return "auto"
	}
}

func deterministicModelID() string {
	return "deterministic-v1"
}

func resolveModelID(mode string, cfg WorkerConfig) string {
	if normalizeMode(mode) == "deterministic" {
		return deterministicModelID()
	}
	if strings.TrimSpace(cfg.Provider) == "" || strings.TrimSpace(cfg.Model) == "" {
		return deterministicModelID()
	}
	return strings.TrimSpace(cfg.Provider) + ":" + strings.TrimSpace(cfg.Model)
}

func workerConfigWithTimeout(cfg WorkerConfig, timeoutSeconds int) WorkerConfig {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 45
	}
	out := cfg
	out.Timeout = time.Duration(timeoutSeconds) * time.Second
	if out.MaxContextTokens <= 0 {
		out.MaxContextTokens = 100000
	}
	return out
}
