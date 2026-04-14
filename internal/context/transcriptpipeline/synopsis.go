package transcriptpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/jkatigb/agentctl/internal/context/companion"
	"github.com/jkatigb/agentctl/internal/storage/transcriptcache"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

const frameSynopsisSystemPromptV1 = `Summarize one anchored interaction frame into rolling narrative state.
Return only valid JSON:
{"line":"...","session_synopsis":"..."}

Rules:
- line must be exactly one standalone sentence about what changed in this frame.
- session_synopsis must be exactly one standalone sentence that summarizes the session arc so far, including this frame.
- Use recent_window and prior_session_synopsis as context only; do not repeat them verbatim unless necessary.
- Focus on decisions, corrections, preferences, blockers, or durable direction shifts.
- Avoid file inventories, patch narration, and implementation bookkeeping unless they are the main point.
- Keep each sentence under 28 words.`

// FrameSynopsis is the bounded narrative state carried across interaction frames.
type FrameSynopsis struct {
	FrameIndex             int                  `json:"frame_index"`
	RecentWindow           []string             `json:"recent_window,omitempty"`
	SessionSynopsis        string               `json:"session_synopsis,omitempty"`
	Line                   string               `json:"line,omitempty"`
	UpdatedSessionSynopsis string               `json:"updated_session_synopsis,omitempty"`
	Artifact               *ArtifactCacheReport `json:"artifact,omitempty"`
}

type frameSynopsisPayload struct {
	Line            string `json:"line"`
	SessionSynopsis string `json:"session_synopsis"`
}

// BuildFrameSynopses produces one bounded rolling synopsis state per derivation frame.
func BuildFrameSynopses(ctx context.Context, cacheStore *transcriptcache.Store, runtime LocalModelRuntime, parsed sourceimport.ParsedSession, objective SessionObjective, derivations []companion.AnchoredMemoryDerivation) ([]FrameSynopsis, error) {
	if len(derivations) == 0 {
		return nil, nil
	}

	windowSize := positiveIntOr(runtime.SynopsisWindowSize, DefaultSynopsisWindowSize)
	promptVersion := firstNonEmpty(strings.TrimSpace(runtime.FrameSynopsisPromptVersion), DefaultFrameSynopsisPromptVersion)
	modelID := resolveModelID(runtime.Mode, runtime.WorkerConfig())

	synopses := make([]FrameSynopsis, 0, len(derivations))
	recentLines := make([]string, 0, windowSize)
	sessionSynopsis := ""
	scaffold := objective.Scaffold()
	for _, derivation := range derivations {
		recentWindow := append([]string(nil), recentLines...)
		synopsis, err := buildFrameSynopsis(ctx, cacheStore, runtime, parsed, derivation, recentWindow, sessionSynopsis, scaffold, promptVersion, modelID)
		if err != nil {
			return nil, err
		}
		synopses = append(synopses, synopsis)
		if synopsis.Line != "" {
			recentLines = append(recentLines, synopsis.Line)
			if len(recentLines) > windowSize {
				recentLines = recentLines[len(recentLines)-windowSize:]
			}
		}
		sessionSynopsis = synopsis.UpdatedSessionSynopsis
	}
	return synopses, nil
}

func buildFrameSynopsis(ctx context.Context, cacheStore *transcriptcache.Store, runtime LocalModelRuntime, parsed sourceimport.ParsedSession, derivation companion.AnchoredMemoryDerivation, recentWindow []string, priorSession string, objective ObjectiveScaffold, promptVersion, modelID string) (FrameSynopsis, error) {
	artifactText := buildFrameSynopsisArtifactText(parsed, derivation, recentWindow, priorSession, objective)
	sourceHash := transcriptcache.DigestText(artifactText)
	normalizedHash := sourceHash

	if cacheStore != nil {
		if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, "frame_synopsis", normalizedHash, promptVersion, modelID); err != nil {
			return FrameSynopsis{}, err
		} else if hit {
			payload := decodeFrameSynopsisPayload(entry.Summary, derivation, priorSession, objective)
			return FrameSynopsis{
				FrameIndex:             derivation.FrameIndex,
				RecentWindow:           recentWindow,
				SessionSynopsis:        priorSession,
				Line:                   payload.Line,
				UpdatedSessionSynopsis: payload.SessionSynopsis,
				Artifact: &ArtifactCacheReport{
					ArtifactKind:   "frame_synopsis",
					NormalizedHash: normalizedHash,
					SourceHash:     sourceHash,
					DerivationMode: entry.DerivationMode,
					ModelID:        entry.ModelID,
					CacheHit:       true,
					SummaryPreview: truncatePacketInline(entry.Summary, 140),
				},
			}, nil
		}
	}

	payload := deterministicFrameSynopsisPayload(derivation, priorSession, objective)
	entry := transcriptcache.Entry{
		ArtifactKind:   "frame_synopsis",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		PromptVersion:  promptVersion,
		SourcePreview:  truncatePacketInline(artifactText, 120),
	}
	switch normalizeMode(runtime.Mode) {
	case "deterministic":
		entry.DerivationMode = "deterministic"
		entry.ModelID = deterministicModelID()
	default:
		result, err := RunLLMTask(ctx, runtime.WorkerConfig(), Task{
			Stage:         StageDerive,
			InputKind:     "frame_synopsis",
			PromptVersion: promptVersion,
			SystemPrompt:  frameSynopsisSystemPromptV1,
			ArtifactText:  artifactText,
			MaxTokens:     160,
		})
		if err != nil {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
		} else if decoded, ok := parseFrameSynopsisPayload(result.OutputText, derivation, priorSession, objective); ok {
			entry.DerivationMode = "lmstudio"
			entry.ModelID = result.ModelID
			payload = decoded
		} else {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
		}
	}
	entry.Summary = encodeFrameSynopsisPayload(payload)
	if cacheStore != nil {
		if err := cacheStore.Put(ctx, entry); err != nil {
			return FrameSynopsis{}, err
		}
	}

	return FrameSynopsis{
		FrameIndex:             derivation.FrameIndex,
		RecentWindow:           recentWindow,
		SessionSynopsis:        priorSession,
		Line:                   payload.Line,
		UpdatedSessionSynopsis: payload.SessionSynopsis,
		Artifact: &ArtifactCacheReport{
			ArtifactKind:   "frame_synopsis",
			NormalizedHash: normalizedHash,
			SourceHash:     sourceHash,
			DerivationMode: entry.DerivationMode,
			ModelID:        entry.ModelID,
			CacheHit:       false,
			SummaryPreview: truncatePacketInline(entry.Summary, 140),
		},
	}, nil
}

func buildFrameSynopsisArtifactText(parsed sourceimport.ParsedSession, derivation companion.AnchoredMemoryDerivation, recentWindow []string, priorSession string, objective ObjectiveScaffold) string {
	var b strings.Builder
	b.WriteString("provider: ")
	b.WriteString(string(parsed.Provider))
	b.WriteString("\nsession_id: ")
	b.WriteString(strings.TrimSpace(parsed.SessionID))
	b.WriteString("\nframe_index: ")
	b.WriteString(fmt.Sprintf("%d", derivation.FrameIndex))
	b.WriteString("\nresolution: ")
	b.WriteString(string(derivation.Resolution))
	b.WriteString("\nreaction: ")
	b.WriteString(string(derivation.Reaction.Outcome))
	b.WriteString("\n")
	if objective.Label != "" {
		b.WriteString("objective_label: ")
		b.WriteString(objective.Label)
		b.WriteString("\n")
	}
	if objective.Status != "" {
		b.WriteString("objective_status: ")
		b.WriteString(objective.Status)
		b.WriteString("\n")
	}
	if len(objective.Tags) > 0 {
		b.WriteString("objective_tags:\n")
		for _, item := range objective.Tags {
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}
	if priorSession != "" {
		b.WriteString("prior_session_synopsis: ")
		b.WriteString(priorSession)
		b.WriteString("\n")
	}
	if len(recentWindow) > 0 {
		b.WriteString("recent_window:\n")
		for _, line := range recentWindow {
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("interaction_summary: ")
	b.WriteString(strings.TrimSpace(derivation.InteractionSummary))
	b.WriteString("\n")
	for _, candidate := range derivation.Candidates {
		if strings.TrimSpace(candidate.Type) == "tool_output_digest" {
			continue
		}
		b.WriteString("- candidate type=")
		b.WriteString(candidate.Type)
		b.WriteString(" scope=")
		b.WriteString(string(candidate.Scope))
		b.WriteString(" confidence=")
		b.WriteString(fmt.Sprintf("%.2f", candidate.Confidence))
		b.WriteString(" text=")
		b.WriteString(strings.TrimSpace(candidate.Text))
		b.WriteString("\n")
	}
	return b.String()
}

func deterministicFrameSynopsisPayload(derivation companion.AnchoredMemoryDerivation, priorSession string, objective ObjectiveScaffold) frameSynopsisPayload {
	line := deterministicFrameSynopsisLine(derivation)
	session := deterministicSessionSynopsis(priorSession, line, objective)
	return frameSynopsisPayload{
		Line:            line,
		SessionSynopsis: session,
	}
}

func deterministicFrameSynopsisLine(derivation companion.AnchoredMemoryDerivation) string {
	text := ""
	for _, candidate := range derivation.Candidates {
		if strings.TrimSpace(candidate.Text) == "" || strings.TrimSpace(candidate.Type) == "tool_output_digest" {
			continue
		}
		text = strings.TrimSpace(candidate.Text)
		break
	}
	if text == "" {
		text = strings.TrimSpace(derivation.InteractionSummary)
	}
	prefix := "The conversation continued around"
	switch derivation.Resolution {
	case companion.InteractionResolutionResolved:
		prefix = "The interaction resolved around"
	case companion.InteractionResolutionCorrected:
		prefix = "The user corrected the direction around"
	case companion.InteractionResolutionUnresolved:
		prefix = "The issue remained unresolved around"
	}
	return normalizeSynopsisSentence(prefix+" "+text, 200)
}

func deterministicSessionSynopsis(priorSession, line string, objective ObjectiveScaffold) string {
	priorSession = strings.TrimSpace(priorSession)
	line = strings.TrimSpace(line)
	switch {
	case priorSession == "":
		if objective.Label != "" && line != "" {
			return normalizeSynopsisSentence(objective.Label+" Then "+lowercaseSynopsisLead(line), 240)
		}
		return line
	case line == "":
		return priorSession
	case strings.EqualFold(priorSession, line):
		return priorSession
	default:
		return normalizeSynopsisSentence(priorSession+" Then "+lowercaseSynopsisLead(line), 240)
	}
}

func parseFrameSynopsisPayload(raw string, derivation companion.AnchoredMemoryDerivation, priorSession string, objective ObjectiveScaffold) (frameSynopsisPayload, bool) {
	var payload frameSynopsisPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return frameSynopsisPayload{}, false
	}
	payload.Line = normalizeSynopsisSentence(payload.Line, 200)
	payload.SessionSynopsis = normalizeSynopsisSentence(payload.SessionSynopsis, 240)
	if payload.Line == "" {
		return frameSynopsisPayload{}, false
	}
	if payload.SessionSynopsis == "" {
		payload.SessionSynopsis = deterministicSessionSynopsis(priorSession, payload.Line, objective)
	}
	if payload.SessionSynopsis == "" {
		payload = deterministicFrameSynopsisPayload(derivation, priorSession, objective)
	}
	return payload, true
}

func decodeFrameSynopsisPayload(raw string, derivation companion.AnchoredMemoryDerivation, priorSession string, objective ObjectiveScaffold) frameSynopsisPayload {
	if payload, ok := parseFrameSynopsisPayload(raw, derivation, priorSession, objective); ok {
		return payload
	}
	return deterministicFrameSynopsisPayload(derivation, priorSession, objective)
}

func encodeFrameSynopsisPayload(payload frameSynopsisPayload) string {
	payload.Line = normalizeSynopsisSentence(payload.Line, 200)
	payload.SessionSynopsis = normalizeSynopsisSentence(payload.SessionSynopsis, 240)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"line":%q,"session_synopsis":%q}`, payload.Line, payload.SessionSynopsis)
	}
	return string(encoded)
}

func normalizeSynopsisSentence(text string, max int) string {
	text = truncatePacketInline(text, max)
	text = strings.Trim(text, "\"' ")
	if text == "" {
		return ""
	}
	if !strings.HasSuffix(text, ".") && !strings.HasSuffix(text, "!") && !strings.HasSuffix(text, "?") {
		text += "."
	}
	return text
}

func lowercaseSynopsisLead(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.TrimRight(text, ".!?")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}
