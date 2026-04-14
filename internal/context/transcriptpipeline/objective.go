package transcriptpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/storage/transcriptcache"
	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
)

const sessionObjectiveSystemPromptV1 = `Identify the overarching session objective from a transcript.
Return only valid JSON:
{"objective":"...","status":"active|completed|pivoted","confidence":0.0,"objective_history":["..."],"evidence":["..."]}

Rules:
- objective must be exactly one standalone sentence describing the dominant user goal.
- Prefer the slow-moving goal that explains multiple turns, not the latest local step.
- status should be active unless the transcript clearly shows completion or a pivot to a new dominant goal.
- objective_history should only include prior dominant goals if the user clearly redirected the session.
- evidence should be 1-3 short quoted or paraphrased cues from the transcript.
- Keep fields concise and avoid file inventories, implementation bookkeeping, or meta-evaluation commentary.`

const sessionObjectiveSystemPromptV2 = `Identify the overarching session objective from a transcript.
Return only valid JSON:
{"objective":"...","label":"...","status":"active|completed|pivoted","confidence":0.0,"tags":["..."],"objective_history":["..."],"evidence":["..."]}

Rules:
- objective must be exactly one standalone sentence describing the dominant user goal.
- label must be a shorter goal label under 10 words that can be fanned out to downstream stages.
- Prefer the slow-moving goal that explains multiple turns, not the latest local step.
- status should be active unless the transcript clearly shows completion or a pivot to a new dominant goal.
- tags should be 2-4 short semantic labels for the goal, not implementation details.
- objective_history should only include prior dominant goals if the user clearly redirected the session.
- evidence should be 1-3 short quoted or paraphrased cues from the transcript.
- Keep fields concise and avoid file inventories, implementation bookkeeping, or meta-evaluation commentary.`

const sessionObjectiveCandidateSelectPromptV1 = `Choose the dominant slow-moving session objective from candidate user asks.
Return only valid JSON:
{"objective":"...","label":"...","status":"active|completed|pivoted","confidence":0.0,"evidence":["..."]}

Rules:
- Prefer the candidate that best explains the actual work the session is carrying forward.
- Prefer concrete work requests over acknowledgements, speculation, or meta discussion.
- If a candidate is framed as rejected, negated, or something the user explicitly does not want to do, do not select that rejected proposal as the objective.
- When a candidate contrasts one idea against another (for example, "not X, instead continue with Y"), choose the surviving direction, not the rejected idea.
- Prefer repeated concrete continuation work over one-off tooling or coordination ideas that the session discards.
- Treat inventory, exploration, "use X as reference", commit, or docs-update steps as supporting tactics rather than the main objective when they clearly support a broader implementation task.
- If a candidate says to use some prior system/version as a reference, rewrite the objective as the concrete implementation target that reference supports.
- Each candidate includes both the user ask and the assistant result for that turn; prefer the objective that best explains the work that actually happened across turns.
- objective must be exactly one standalone sentence.
- label must be a shorter goal label under 10 words.
- Keep evidence concise and grounded in the selected candidate text.`

// ObjectiveScaffold is the compact downstream-safe form of a session objective.
type ObjectiveScaffold struct {
	Label  string   `json:"label,omitempty"`
	Status string   `json:"status,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

// SessionObjective is the slow-moving goal state for a session or grouped mainline.
type SessionObjective struct {
	Objective        string               `json:"objective"`
	Label            string               `json:"label,omitempty"`
	Status           string               `json:"status,omitempty"`
	Confidence       float64              `json:"confidence,omitempty"`
	Tags             []string             `json:"tags,omitempty"`
	ObjectiveHistory []string             `json:"objective_history,omitempty"`
	Evidence         []string             `json:"evidence,omitempty"`
	Artifact         *ArtifactCacheReport `json:"artifact,omitempty"`
}

type sessionObjectivePayload struct {
	Objective        string   `json:"objective"`
	Label            string   `json:"label"`
	Status           string   `json:"status"`
	Confidence       float64  `json:"confidence"`
	Tags             []string `json:"tags"`
	ObjectiveHistory []string `json:"objective_history"`
	Evidence         []string `json:"evidence"`
}

type objectivePromptCandidate struct {
	text  string
	reply string
	score int
	turn  int
}

// BuildSessionObjective extracts one cached overarching objective for a transcript run.
func BuildSessionObjective(ctx context.Context, cacheStore *transcriptcache.Store, runtime LocalModelRuntime, parsed sourceimport.ParsedSession) (SessionObjective, error) {
	artifactText := buildSessionObjectiveArtifactText(parsed)
	if strings.TrimSpace(artifactText) == "" {
		return SessionObjective{}, nil
	}
	candidates := deterministicObjectiveCandidates(parsed)

	promptVersion := firstNonEmpty(strings.TrimSpace(runtime.ObjectivePromptVersion), DefaultObjectivePromptVersion)
	modelID := resolveModelID(runtime.Mode, runtime.WorkerConfig())
	sourceHash := transcriptcache.DigestText(artifactText)
	normalizedHash := sourceHash

	if cacheStore != nil {
		if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, "session_objective", normalizedHash, promptVersion, modelID); err != nil {
			return SessionObjective{}, err
		} else if hit {
			payload := decodeSessionObjectivePayload(entry.Summary, parsed)
			return sessionObjectiveFromPayload(payload, &ArtifactCacheReport{
				ArtifactKind:   "session_objective",
				NormalizedHash: normalizedHash,
				SourceHash:     sourceHash,
				DerivationMode: entry.DerivationMode,
				ModelID:        entry.ModelID,
				CacheHit:       true,
				SummaryPreview: truncatePacketInline(entry.Summary, 140),
			}), nil
		}
	}

	payload := deterministicSessionObjectivePayloadFromCandidates(candidates)
	entry := transcriptcache.Entry{
		ArtifactKind:   "session_objective",
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
			InputKind:     "session_objective",
			PromptVersion: promptVersion,
			SystemPrompt:  sessionObjectiveSystemPromptForVersion(promptVersion),
			ArtifactText:  artifactText,
			MaxTokens:     260,
		})
		if err != nil {
			if selected, ok := selectSessionObjectivePayload(ctx, runtime, candidates); ok {
				entry.DerivationMode = "lmstudio_selector_fallback"
				entry.ModelID = selected.ModelID
				payload = selected.Payload
			} else {
				entry.DerivationMode = "deterministic_fallback"
				entry.ModelID = deterministicModelID()
			}
		} else if decoded, ok := parseSessionObjectivePayload(result.OutputText, parsed); ok {
			entry.DerivationMode = "lmstudio"
			entry.ModelID = result.ModelID
			payload = decoded
		} else {
			if selected, ok := selectSessionObjectivePayload(ctx, runtime, candidates); ok {
				entry.DerivationMode = "lmstudio_selector_fallback"
				entry.ModelID = selected.ModelID
				payload = selected.Payload
			} else {
				entry.DerivationMode = "deterministic_fallback"
				entry.ModelID = deterministicModelID()
			}
		}
	}
	entry.Summary = encodeSessionObjectivePayload(payload)
	if cacheStore != nil {
		if err := cacheStore.Put(ctx, entry); err != nil {
			return SessionObjective{}, err
		}
	}

	return sessionObjectiveFromPayload(payload, &ArtifactCacheReport{
		ArtifactKind:   "session_objective",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: entry.DerivationMode,
		ModelID:        entry.ModelID,
		CacheHit:       false,
		SummaryPreview: truncatePacketInline(entry.Summary, 140),
	}), nil
}

func buildSessionObjectiveArtifactText(parsed sourceimport.ParsedSession) string {
	var b strings.Builder
	b.WriteString("provider: ")
	b.WriteString(string(parsed.Provider))
	b.WriteString("\nsession_id: ")
	b.WriteString(strings.TrimSpace(parsed.SessionID))
	if strings.TrimSpace(parsed.WorkspacePath) != "" {
		b.WriteString("\nworkspace_path: ")
		b.WriteString(strings.TrimSpace(parsed.WorkspacePath))
	}
	b.WriteString("\n\ntranscript:\n")
	for i, turn := range parsed.Turns {
		prompt := strings.TrimSpace(turn.Prompt)
		output := strings.TrimSpace(turn.FinalOutput.Text)
		if prompt == "" && output == "" {
			continue
		}
		b.WriteString("turn ")
		b.WriteString(fmt.Sprintf("%d", i))
		b.WriteString("\n")
		if prompt != "" {
			b.WriteString("user: ")
			b.WriteString(truncatePacketInline(prompt, 600))
			b.WriteString("\n")
		}
		if output != "" {
			b.WriteString("assistant: ")
			b.WriteString(truncatePacketInline(output, 600))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func deterministicSessionObjectivePayload(parsed sourceimport.ParsedSession) sessionObjectivePayload {
	return deterministicSessionObjectivePayloadFromCandidates(deterministicObjectiveCandidates(parsed))
}

func deterministicSessionObjectivePayloadFromCandidates(candidates []objectivePromptCandidate) sessionObjectivePayload {
	objective := ""
	evidence := make([]string, 0, 2)
	if len(candidates) > 0 {
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].score != candidates[j].score {
				return candidates[i].score > candidates[j].score
			}
			if len(strings.Fields(candidates[i].text)) != len(strings.Fields(candidates[j].text)) {
				return len(strings.Fields(candidates[i].text)) > len(strings.Fields(candidates[j].text))
			}
			return candidates[i].turn > candidates[j].turn
		})
		objective = candidates[0].text
		for _, item := range candidates {
			evidence = appendUniqueString(evidence, truncatePacketInline(item.text, 120), 2)
			if len(evidence) >= 2 {
				break
			}
		}
	}
	if objective == "" {
		objective = "Continue the current session work and resolve the user's active objective."
	}
	return sessionObjectivePayload{
		Objective:  objective,
		Label:      normalizeObjectiveLabel("", objective),
		Status:     "active",
		Confidence: 0.56,
		Evidence:   evidence,
	}
}

func deterministicObjectiveCandidates(parsed sourceimport.ParsedSession) []objectivePromptCandidate {
	candidates := make([]objectivePromptCandidate, 0, len(parsed.Turns))
	for idx, turn := range parsed.Turns {
		prompt := normalizeObjectiveText(turn.Prompt, 220)
		if prompt == "" {
			continue
		}
		candidates = append(candidates, objectivePromptCandidate{
			text:  prompt,
			reply: normalizeObjectiveText(turn.FinalOutput.Text, 160),
			score: scoreObjectivePrompt(prompt, idx),
			turn:  idx,
		})
	}
	return candidates
}

func parseSessionObjectivePayload(raw string, parsed sourceimport.ParsedSession) (sessionObjectivePayload, bool) {
	var payload sessionObjectivePayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return sessionObjectivePayload{}, false
	}
	payload.Objective = normalizeObjectiveText(payload.Objective, 220)
	payload.Label = normalizeObjectiveLabel(payload.Label, payload.Objective)
	payload.Status = normalizeObjectiveStatus(payload.Status)
	payload.Tags = normalizeTagList(payload.Tags)
	payload.ObjectiveHistory = normalizeObjectiveList(payload.ObjectiveHistory, 4, 180)
	payload.Evidence = normalizeObjectiveList(payload.Evidence, 3, 120)
	if payload.Objective == "" {
		return sessionObjectivePayload{}, false
	}
	if payload.Label == "" {
		payload.Label = normalizeObjectiveLabel(payload.Objective, payload.Objective)
	}
	if payload.Status == "" {
		payload.Status = "active"
	}
	if payload.Confidence <= 0 {
		payload.Confidence = 0.5
	}
	if payload.Confidence > 1 {
		payload.Confidence = 1
	}
	return payload, true
}

func decodeSessionObjectivePayload(raw string, parsed sourceimport.ParsedSession) sessionObjectivePayload {
	if payload, ok := parseSessionObjectivePayload(raw, parsed); ok {
		return payload
	}
	return deterministicSessionObjectivePayload(parsed)
}

func encodeSessionObjectivePayload(payload sessionObjectivePayload) string {
	payload.Objective = normalizeObjectiveText(payload.Objective, 220)
	payload.Label = normalizeObjectiveLabel(payload.Label, payload.Objective)
	payload.Status = normalizeObjectiveStatus(payload.Status)
	payload.Tags = normalizeTagList(payload.Tags)
	payload.ObjectiveHistory = normalizeObjectiveList(payload.ObjectiveHistory, 4, 180)
	payload.Evidence = normalizeObjectiveList(payload.Evidence, 3, 120)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"objective":%q,"label":%q,"status":%q,"confidence":%.2f}`, payload.Objective, payload.Label, payload.Status, payload.Confidence)
	}
	return string(encoded)
}

func sessionObjectiveFromPayload(payload sessionObjectivePayload, artifact *ArtifactCacheReport) SessionObjective {
	return SessionObjective{
		Objective:        normalizeObjectiveText(payload.Objective, 220),
		Label:            normalizeObjectiveLabel(payload.Label, payload.Objective),
		Status:           normalizeObjectiveStatus(payload.Status),
		Confidence:       payload.Confidence,
		Tags:             normalizeTagList(payload.Tags),
		ObjectiveHistory: payload.ObjectiveHistory,
		Evidence:         payload.Evidence,
		Artifact:         artifact,
	}
}

// Scaffold returns the compact objective form used by downstream frame stages.
func (o SessionObjective) Scaffold() ObjectiveScaffold {
	return ObjectiveScaffold{
		Label:  normalizeObjectiveLabel(o.Label, o.Objective),
		Status: normalizeObjectiveStatus(o.Status),
		Tags:   normalizeTagList(o.Tags),
	}
}

func normalizeObjectiveStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "completed":
		return "completed"
	case "pivoted":
		return "pivoted"
	default:
		return "active"
	}
}

func normalizeObjectiveList(in []string, maxItems, maxLen int) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		item = normalizeObjectiveText(item, maxLen)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
		if maxItems > 0 && len(out) >= maxItems {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeObjectiveLabel(label, fallback string) string {
	label = compactObjectiveLabelText(label)
	label = strings.Trim(label, "\"' ")
	if label != "" {
		return trimObjectiveLabelWords(label, 10)
	}
	fallback = compactObjectiveLabelText(fallback)
	fallback = strings.Trim(fallback, "\"' ")
	return trimObjectiveLabelWords(fallback, 10)
}

func compactObjectiveLabelText(text string) string {
	text = summarizeInsightText(strings.TrimSpace(text))
	text = compactSummaryText(text, 220)
	text = stripObjectiveLeadPhrase(text)
	text = stripObjectiveFillers(text)
	text = preferCompactObjectiveClause(text, 10)
	text = trimObjectiveTrailingPhrase(text)
	return strings.TrimSpace(truncatePacketInline(text, 120))
}

func stripObjectiveLeadPhrase(text string) string {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	for _, prefix := range []string{
		"can we ",
		"could we ",
		"can you ",
		"could you ",
		"let's ",
		"lets ",
		"please ",
		"help me ",
		"i want to ",
		"we need to ",
		"we should ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(text[len(prefix):])
		}
	}
	return text
}

func stripObjectiveFillers(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	replacements := []struct {
		old string
		new string
	}{
		{" as well ", " "},
		{" try to ", " "},
		{"all the possible ", ""},
	}
	for _, item := range replacements {
		text = strings.ReplaceAll(text, item.old, item.new)
	}
	return strings.Join(strings.Fields(text), " ")
}

func preferCompactObjectiveClause(text string, limit int) string {
	text = strings.TrimSpace(strings.TrimRight(text, ".!?"))
	if text == "" {
		return ""
	}
	if len(strings.Fields(text)) <= limit {
		return text
	}
	parts := strings.Split(text, " and ")
	if len(parts) < 2 {
		return text
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	last = trimObjectiveTrailingPhrase(last)
	if words := len(strings.Fields(last)); words >= 3 && words <= limit {
		return last
	}
	return text
}

func trimObjectiveTrailingPhrase(text string) string {
	text = strings.TrimSpace(strings.TrimRight(text, ".!?"))
	lower := strings.ToLower(text)
	for _, suffix := range []string{" for that", " for this", " for it", " then"} {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimSpace(text[:len(text)-len(suffix)])
		}
	}
	return text
}

func normalizeObjectiveText(text string, max int) string {
	text = summarizeInsightText(text)
	text = compactSummaryText(text, max)
	return normalizeSynopsisSentence(text, max)
}

func scoreObjectivePrompt(prompt string, turnIdx int) int {
	wordCount := len(strings.Fields(strings.TrimSpace(prompt)))
	if wordCount > 32 {
		wordCount = 32
	}
	score := wordCount * 10
	if turnIdx > 0 {
		score += minInt(turnIdx, 12)
	}
	return score
}

type selectedObjectivePayload struct {
	Payload sessionObjectivePayload
	ModelID string
}

func selectSessionObjectivePayload(ctx context.Context, runtime LocalModelRuntime, candidates []objectivePromptCandidate) (selectedObjectivePayload, bool) {
	if len(candidates) == 0 || normalizeMode(runtime.Mode) == "deterministic" {
		return selectedObjectivePayload{}, false
	}
	var b strings.Builder
	b.WriteString("candidate_user_asks:\n")
	for _, item := range candidates {
		b.WriteString("- turn ")
		b.WriteString(fmt.Sprintf("%d", item.turn))
		b.WriteString("\n  user: ")
		b.WriteString(item.text)
		if strings.TrimSpace(item.reply) != "" {
			b.WriteString("\n  assistant: ")
			b.WriteString(item.reply)
		}
		b.WriteString("\n")
	}
	result, ok := RunLLMTaskWithFallbackModel(
		ctx,
		objectiveSelectorWorkerConfig(runtime),
		runtime.WorkerConfig(),
		Task{
			Stage:         StageClassify,
			InputKind:     "session_objective_candidates",
			PromptVersion: "session_objective_select_v1",
			SystemPrompt:  sessionObjectiveCandidateSelectPromptV1,
			ArtifactText:  b.String(),
			MaxTokens:     180,
		},
		func(result Result) bool {
			_, ok := parseSessionObjectivePayload(result.OutputText, sourceimport.ParsedSession{})
			return ok
		},
		nil,
	)
	if !ok {
		return selectedObjectivePayload{}, false
	}
	payload, ok := parseSessionObjectivePayload(result.OutputText, sourceimport.ParsedSession{})
	if !ok {
		return selectedObjectivePayload{}, false
	}
	return selectedObjectivePayload{Payload: payload, ModelID: result.ModelID}, true
}

func objectiveSelectorWorkerConfig(runtime LocalModelRuntime) WorkerConfig {
	cfg := runtime.WorkerConfig()
	cfg.Model = firstNonEmpty(strings.TrimSpace(runtime.DoctrineBridgeModel), DefaultDoctrineBridgeModel)
	return cfg
}

func trimObjectiveLabelWords(label string, limit int) string {
	label = strings.TrimSpace(strings.TrimRight(label, ".!?"))
	if label == "" || limit <= 0 {
		return label
	}
	words := strings.Fields(label)
	if len(words) <= limit {
		return label
	}
	return strings.Join(words[:limit], " ")
}

func sessionObjectiveSystemPromptForVersion(version string) string {
	switch strings.TrimSpace(version) {
	case "", "session_objective_v1":
		return sessionObjectiveSystemPromptV1
	case "session_objective_v2", "session_objective_v3", "session_objective_v4", DefaultObjectivePromptVersion:
		return sessionObjectiveSystemPromptV2
	default:
		return sessionObjectiveSystemPromptV1
	}
}
