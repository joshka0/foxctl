package sourceimport

import (
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/v2/adapters/turso/turns"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

const (
	defaultNarrativeVersion = "v1"
	defaultNarrativeClaims  = 6
)

// NarrativeBuildOptions controls deterministic narrative derivation.
type NarrativeBuildOptions struct {
	ArtifactVersion string
	MaxClaims       int
	Now             func() time.Time
}

// NarrativeBuildResult contains one session-scoped narrative output.
type NarrativeBuildResult struct {
	Narrative  run.NarrativeRecord
	HasResult  bool
	ClaimCount int
	Warnings   []string
}

// BuildNarrative derives one evidence-cited narrative artifact from parsed turns.
func BuildNarrative(parsed ParsedSession, artifacts []turns.Artifact, opts NarrativeBuildOptions) NarrativeBuildResult {
	if len(parsed.Turns) == 0 {
		return NarrativeBuildResult{}
	}

	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	version := strings.TrimSpace(opts.ArtifactVersion)
	if version == "" {
		version = defaultNarrativeVersion
	}

	maxClaims := opts.MaxClaims
	if maxClaims <= 0 {
		maxClaims = defaultNarrativeClaims
	}
	if maxClaims > 16 {
		maxClaims = 16
	}

	orderedTurns := make([]run.TurnRecord, 0, len(parsed.Turns))
	for _, turn := range parsed.Turns {
		orderedTurns = append(orderedTurns, turn.Clone())
	}
	sort.SliceStable(orderedTurns, func(i, j int) bool {
		if orderedTurns[i].TurnIndex == orderedTurns[j].TurnIndex {
			return orderedTurns[i].ID < orderedTurns[j].ID
		}
		return orderedTurns[i].TurnIndex < orderedTurns[j].TurnIndex
	})

	refsByTurn := make(map[string][]string, len(orderedTurns))
	for _, artifact := range artifacts {
		turnID := strings.TrimSpace(artifact.TurnID)
		ref := strings.TrimSpace(artifact.Ref)
		if turnID == "" || ref == "" {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(artifact.ArtifactType)) {
		case turns.ArtifactTypeAnnotation, turns.ArtifactTypeClassification, turns.ArtifactTypeLearning, turns.ArtifactTypeEmbedding:
			refsByTurn[turnID] = append(refsByTurn[turnID], ref)
		}
	}

	start := 0
	if len(orderedTurns) > maxClaims {
		start = len(orderedTurns) - maxClaims
	}
	window := orderedTurns[start:]
	if len(window) == 0 {
		return NarrativeBuildResult{}
	}
	latest := window[len(window)-1]

	claims := make([]run.NarrativeClaim, 0, len(window))
	for _, turn := range window {
		text := strings.TrimSpace(turn.FinalOutput.Text)
		if text == "" {
			text = strings.TrimSpace(turn.Prompt)
		}
		if text == "" {
			continue
		}

		anchorRefs := []string{"turn/" + strings.TrimSpace(turn.ID)}
		artifactRefs := uniqueSorted(refsByTurn[turn.ID])
		if len(artifactRefs) > 0 {
			if len(artifactRefs) > 2 {
				artifactRefs = artifactRefs[:2]
			}
			anchorRefs = append(anchorRefs, artifactRefs...)
		}
		claims = append(claims, run.NarrativeClaim{
			Text:       truncate(text, 180),
			AnchorRefs: uniqueSorted(anchorRefs),
		})
	}

	if len(claims) == 0 {
		return NarrativeBuildResult{}
	}

	summaryParts := make([]string, 0, 2)
	for _, claim := range claims {
		part := strings.TrimSpace(claim.Text)
		if part == "" {
			continue
		}
		summaryParts = append(summaryParts, truncate(part, 120))
		if len(summaryParts) >= 2 {
			break
		}
	}

	anchorRefs := make([]string, 0, len(claims)*2)
	for _, claim := range claims {
		anchorRefs = append(anchorRefs, claim.AnchorRefs...)
	}

	sessionID := ""
	for _, turn := range orderedTurns {
		if sid := strings.TrimSpace(turn.SessionID); sid != "" {
			sessionID = sid
			break
		}
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(parsed.SessionID)
	}

	updatedAt := latest.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = latest.CreatedAt.UTC()
	}
	if updatedAt.IsZero() {
		updatedAt = now().UTC()
	}

	narrative := run.NarrativeRecord{
		SessionID:       sessionID,
		ArtifactVersion: version,
		Summary:         strings.Join(summaryParts, " "),
		Claims:          claims,
		AnchorRefs:      uniqueSorted(anchorRefs),
		SourceTurnID:    strings.TrimSpace(latest.ID),
		SourceTurnIndex: latest.TurnIndex,
		SourceTurnCount: len(orderedTurns),
		UpdatedAt:       updatedAt,
	}
	return NarrativeBuildResult{
		Narrative:  narrative,
		HasResult:  true,
		ClaimCount: len(claims),
	}
}
