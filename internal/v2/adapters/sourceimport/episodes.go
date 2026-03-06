package sourceimport

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/adapters/libsql/turns"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

const (
	defaultEpisodeVersion   = "v2"
	defaultEpisodeChunkSize = 8
)

// EpisodeBuildOptions controls deterministic episode derivation.
type EpisodeBuildOptions struct {
	EpisodeVersion     string
	MaxTurnsPerEpisode int
	Now                func() time.Time
}

// EpisodeBuildResult contains derived episodes and non-fatal warnings.
type EpisodeBuildResult struct {
	Episodes []run.EpisodeRecord
	Warnings []string
}

// BuildEpisodes derives deterministic semantic episodes from parsed turns and
// synthesized artifacts.
func BuildEpisodes(parsed ParsedSession, artifacts []turns.Artifact, opts EpisodeBuildOptions) EpisodeBuildResult {
	if len(parsed.Turns) == 0 {
		return EpisodeBuildResult{}
	}

	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	version := strings.TrimSpace(opts.EpisodeVersion)
	if version == "" {
		version = defaultEpisodeVersion
	}

	maxTurns := opts.MaxTurnsPerEpisode
	if maxTurns <= 0 {
		maxTurns = defaultEpisodeChunkSize
	}
	if maxTurns > 64 {
		maxTurns = 64
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
		ref := strings.TrimSpace(artifact.Ref)
		turnID := strings.TrimSpace(artifact.TurnID)
		if ref == "" || turnID == "" {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(artifact.ArtifactType)) {
		case turns.ArtifactTypeAnnotation, turns.ArtifactTypeClassification, turns.ArtifactTypeLearning:
			refsByTurn[turnID] = append(refsByTurn[turnID], ref)
		}
	}

	sessionID := strings.TrimSpace(orderedTurns[0].SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(parsed.SessionID)
	}

	out := make([]run.EpisodeRecord, 0, (len(orderedTurns)/maxTurns)+1)
	for start := 0; start < len(orderedTurns); start += maxTurns {
		end := start + maxTurns
		if end > len(orderedTurns) {
			end = len(orderedTurns)
		}
		chunk := orderedTurns[start:end]
		if len(chunk) == 0 {
			continue
		}

		startTurn := chunk[0]
		endTurn := chunk[len(chunk)-1]
		toolCalls := 0
		errorTurns := 0
		anchorRefs := make([]string, 0, len(chunk)*3)
		for _, turn := range chunk {
			toolCalls += countToolCalls(turn)
			if turnHasError(turn) {
				errorTurns++
			}
			anchorRefs = append(anchorRefs, refsByTurn[turn.ID]...)
			anchorRefs = append(anchorRefs, fmt.Sprintf("turn/%s", strings.TrimSpace(turn.ID)))
		}

		topic := deriveEpisodeTopic(chunk)
		summary := fmt.Sprintf("%d turns, %d tool calls, %d error turns", len(chunk), toolCalls, errorTurns)
		if topic != "" {
			summary = fmt.Sprintf("%s — %s", topic, summary)
		}

		salience := deriveEpisodeSalience(toolCalls, errorTurns)
		isLandmark := errorTurns > 0 || hasDecisionCue(chunk)
		boundaryKey := buildEpisodeBoundaryKey(startTurn, endTurn)

		createdAt := startTurn.CreatedAt.UTC()
		if createdAt.IsZero() {
			createdAt = now().UTC()
		}
		updatedAt := endTurn.UpdatedAt.UTC()
		if updatedAt.IsZero() {
			updatedAt = now().UTC()
		}
		if updatedAt.Before(createdAt) {
			updatedAt = createdAt
		}

		out = append(out, run.EpisodeRecord{
			SessionID:      sessionID,
			EpisodeVersion: version,
			BoundaryKey:    boundaryKey,
			StartTurnID:    startTurn.ID,
			EndTurnID:      endTurn.ID,
			StartTurnIndex: startTurn.TurnIndex,
			EndTurnIndex:   endTurn.TurnIndex,
			Topic:          topic,
			Summary:        summary,
			SalienceScore:  salience,
			IsLandmark:     isLandmark,
			AnchorRefs:     uniqueSorted(anchorRefs),
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		})
	}

	return EpisodeBuildResult{
		Episodes: out,
	}
}

func deriveEpisodeTopic(chunk []run.TurnRecord) string {
	for _, turn := range chunk {
		if prompt := strings.TrimSpace(turn.Prompt); prompt != "" {
			return truncate(prompt, 100)
		}
	}
	for _, turn := range chunk {
		if final := strings.TrimSpace(turn.FinalOutput.Text); final != "" {
			return truncate(final, 100)
		}
	}
	return "session episode"
}

func deriveEpisodeSalience(toolCalls, errorTurns int) float64 {
	score := 0.10 + float64(errorTurns)*0.20 + float64(toolCalls)*0.01
	if score > 1 {
		return 1
	}
	if score < 0 {
		return 0
	}
	return score
}

func hasDecisionCue(chunk []run.TurnRecord) bool {
	for _, turn := range chunk {
		text := strings.ToLower(strings.TrimSpace(turn.Prompt + " " + turn.FinalOutput.Text))
		if strings.Contains(text, "we decided") ||
			strings.Contains(text, "decision") ||
			strings.Contains(text, "decided") {
			return true
		}
	}
	return false
}

func buildEpisodeBoundaryKey(startTurn, endTurn run.TurnRecord) string {
	startID := strings.TrimSpace(startTurn.ID)
	if startID == "" {
		startID = "unknown_start"
	}
	endID := strings.TrimSpace(endTurn.ID)
	if endID == "" {
		endID = "unknown_end"
	}
	return fmt.Sprintf("chunk:%04d-%04d:%s-%s", startTurn.TurnIndex, endTurn.TurnIndex, startID, endID)
}

func uniqueSorted(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
