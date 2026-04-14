package sourceimport

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/v2/adapters/libsql/turns"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

var (
	// ErrMissingEpisodeTurnTimeline indicates the compiler is missing turn timeline reads.
	ErrMissingEpisodeTurnTimeline = errors.New("sourceimport episode compiler: missing turn timeline reader")
	// ErrMissingEpisodeArtifactReader indicates the compiler is missing artifact reads.
	ErrMissingEpisodeArtifactReader = errors.New("sourceimport episode compiler: missing turn artifact reader")
	// ErrEpisodeTurnNotInTimeline indicates the target turn was not found in the loaded timeline window.
	ErrEpisodeTurnNotInTimeline = errors.New("sourceimport episode compiler: target turn not in loaded timeline")
	// ErrEpisodeTurnNotCovered indicates no built episode covered the target turn.
	ErrEpisodeTurnNotCovered = errors.New("sourceimport episode compiler: target turn not covered by built episodes")
)

const (
	defaultEpisodeTimelineLimit    = 4096
	defaultEpisodeTimelineMaxLimit = 1_000_000
)

// SessionEpisodeCompiler derives deterministic episodes from persisted turns/artifacts.
//
// It is designed for live runtime usage (turn.recorded events) and reuses the same
// BuildEpisodes logic used by source resynthesis for parity.
type SessionEpisodeCompiler struct {
	turnTimeline  run.TurnTimelineReader
	artifactStore TurnArtifactReader
	options       EpisodeBuildOptions
	listLimit     int
	maxListLimit  int
}

// NewSessionEpisodeCompiler creates a runtime episode compiler using persisted state.
func NewSessionEpisodeCompiler(
	turnTimeline run.TurnTimelineReader,
	artifactStore TurnArtifactReader,
	options EpisodeBuildOptions,
) *SessionEpisodeCompiler {
	return &SessionEpisodeCompiler{
		turnTimeline:  turnTimeline,
		artifactStore: artifactStore,
		options:       options,
		listLimit:     defaultEpisodeTimelineLimit,
		maxListLimit:  defaultEpisodeTimelineMaxLimit,
	}
}

// Compile derives the affected episode for the provided turn from persisted session state.
func (c *SessionEpisodeCompiler) Compile(ctx context.Context, turn run.TurnRecord) ([]run.EpisodeRecord, error) {
	if c == nil || c.turnTimeline == nil {
		return nil, ErrMissingEpisodeTurnTimeline
	}
	if c.artifactStore == nil {
		return nil, ErrMissingEpisodeArtifactReader
	}

	sessionID := strings.TrimSpace(turn.SessionID)
	if sessionID == "" {
		return nil, nil
	}

	timelineTurns, limit, err := c.loadTimeline(ctx, sessionID, turn)
	if err != nil {
		return nil, err
	}
	if len(timelineTurns) == 0 {
		return nil, nil
	}
	turnID := strings.TrimSpace(turn.ID)
	turnIndex := turn.TurnIndex
	if !timelineContainsTurn(timelineTurns, turnID, turnIndex) {
		return nil, fmt.Errorf("%w (session_id=%s turn_id=%s turn_index=%d limit=%d)",
			ErrEpisodeTurnNotInTimeline, sessionID, turnID, turnIndex, limit)
	}
	timelineTurns = focusEpisodeTimelineWindow(
		timelineTurns,
		turnID,
		turnIndex,
		effectiveEpisodeChunkSize(c.options.MaxTurnsPerEpisode),
	)
	sortEpisodeTimelineChronologically(timelineTurns)

	artifacts := make([]turns.Artifact, 0, len(timelineTurns)*3)
	for _, listedTurn := range timelineTurns {
		listedTurnID := strings.TrimSpace(listedTurn.ID)
		if listedTurnID == "" {
			continue
		}
		listedArtifacts, listErr := c.artifactStore.ListArtifacts(ctx, listedTurnID)
		if listErr != nil {
			return nil, fmt.Errorf("list artifacts for turn %s: %w", listedTurnID, listErr)
		}
		artifacts = append(artifacts, listedArtifacts...)
	}

	parsed := ParsedSession{
		Provider:  ProviderAuto,
		SessionID: sessionID,
		Turns:     cloneTurns(timelineTurns),
	}
	build := BuildEpisodes(parsed, artifacts, c.options)
	if len(build.Episodes) == 0 {
		return nil, nil
	}

	// Keep writes bounded to the episode currently affected by this turn.
	filtered := make([]run.EpisodeRecord, 0, 1)
	for _, episode := range build.Episodes {
		if turnIndex > 0 {
			if episode.StartTurnIndex <= turnIndex && turnIndex <= episode.EndTurnIndex {
				filtered = append(filtered, episode.Clone())
				break
			}
			continue
		}
		if turnID == "" {
			continue
		}
		if episodeContainsTurn(episode, turnID) {
			filtered = append(filtered, episode.Clone())
			break
		}
	}
	if len(filtered) > 0 {
		return filtered, nil
	}
	return nil, fmt.Errorf("%w (session_id=%s turn_id=%s turn_index=%d)",
		ErrEpisodeTurnNotCovered, sessionID, turnID, turnIndex)
}

func cloneTurns(turns []run.TurnRecord) []run.TurnRecord {
	if len(turns) == 0 {
		return nil
	}
	out := make([]run.TurnRecord, 0, len(turns))
	for _, turn := range turns {
		out = append(out, turn.Clone())
	}
	return out
}

func sortEpisodeTimelineChronologically(turns []run.TurnRecord) {
	if len(turns) < 2 {
		return
	}
	sort.SliceStable(turns, func(i, j int) bool {
		left := turns[i]
		right := turns[j]
		if left.TurnIndex > 0 && right.TurnIndex > 0 && left.TurnIndex != right.TurnIndex {
			return left.TurnIndex < right.TurnIndex
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return strings.TrimSpace(left.ID) < strings.TrimSpace(right.ID)
	})
}

func (c *SessionEpisodeCompiler) loadTimeline(
	ctx context.Context,
	sessionID string,
	turn run.TurnRecord,
) ([]run.TurnRecord, int, error) {
	limit := c.listLimit
	if limit <= 0 {
		limit = defaultEpisodeTimelineLimit
	}
	maxLimit := c.maxListLimit
	if maxLimit <= 0 {
		maxLimit = defaultEpisodeTimelineMaxLimit
	}
	if maxLimit < limit {
		maxLimit = limit
	}

	turnID := strings.TrimSpace(turn.ID)
	turnIndex := turn.TurnIndex
	currentLimit := limit

	// Runtime turn.recorded events are typically for the newest turn, so try recent-first.
	for {
		if err := ctx.Err(); err != nil {
			return nil, currentLimit, err
		}
		timelineTurns, err := c.turnTimeline.ListTurns(ctx, sessionID, run.TurnListOptions{
			Limit: currentLimit,
			Asc:   false,
		})
		if err != nil {
			return nil, currentLimit, fmt.Errorf("list turns for episode compile: %w", err)
		}
		if len(timelineTurns) == 0 {
			return nil, currentLimit, nil
		}
		if timelineContainsTurn(timelineTurns, turnID, turnIndex) {
			return timelineTurns, currentLimit, nil
		}
		if len(timelineTurns) < currentLimit || currentLimit >= maxLimit {
			break
		}
		nextLimit := currentLimit * 2
		if nextLimit > maxLimit {
			nextLimit = maxLimit
		}
		if nextLimit <= currentLimit {
			return timelineTurns, currentLimit, nil
		}
		currentLimit = nextLimit
	}

	// Fallback to oldest-first for non-recent targets (e.g., historical backfills).
	currentLimit = limit
	for {
		if err := ctx.Err(); err != nil {
			return nil, currentLimit, err
		}
		timelineTurns, err := c.turnTimeline.ListTurns(ctx, sessionID, run.TurnListOptions{
			Limit: currentLimit,
			Asc:   true,
		})
		if err != nil {
			return nil, currentLimit, fmt.Errorf("list turns for episode compile: %w", err)
		}
		if len(timelineTurns) == 0 {
			return nil, currentLimit, nil
		}
		if timelineContainsTurn(timelineTurns, turnID, turnIndex) {
			return timelineTurns, currentLimit, nil
		}
		if len(timelineTurns) < currentLimit || currentLimit >= maxLimit {
			return timelineTurns, currentLimit, nil
		}
		nextLimit := currentLimit * 2
		if nextLimit > maxLimit {
			nextLimit = maxLimit
		}
		if nextLimit <= currentLimit {
			return timelineTurns, currentLimit, nil
		}
		currentLimit = nextLimit
	}
}

func timelineContainsTurn(turns []run.TurnRecord, turnID string, turnIndex int) bool {
	for _, turn := range turns {
		if turnID != "" && strings.TrimSpace(turn.ID) == turnID {
			return true
		}
		if turnIndex > 0 && turn.TurnIndex == turnIndex {
			return true
		}
	}
	return false
}

func episodeContainsTurn(episode run.EpisodeRecord, turnID string) bool {
	if episode.StartTurnID == turnID || episode.EndTurnID == turnID {
		return true
	}
	wantRef := "turn/" + turnID
	for _, ref := range episode.AnchorRefs {
		if strings.TrimSpace(ref) == wantRef {
			return true
		}
	}
	return false
}

func effectiveEpisodeChunkSize(maxTurns int) int {
	if maxTurns <= 0 {
		return defaultEpisodeChunkSize
	}
	return maxTurns
}

func focusEpisodeTimelineWindow(turns []run.TurnRecord, turnID string, turnIndex, chunkSize int) []run.TurnRecord {
	if chunkSize <= 0 || len(turns) <= chunkSize {
		return turns
	}
	if turnIndex > 0 {
		chunkStart := ((turnIndex - 1) / chunkSize * chunkSize) + 1
		chunkEnd := chunkStart + chunkSize - 1
		filtered := make([]run.TurnRecord, 0, chunkSize)
		for _, turn := range turns {
			if turn.TurnIndex >= chunkStart && turn.TurnIndex <= chunkEnd {
				filtered = append(filtered, turn)
			}
		}
		if len(filtered) > 0 {
			return filtered
		}
	}

	targetPos := -1
	for i, turn := range turns {
		if turnID != "" && strings.TrimSpace(turn.ID) == turnID {
			targetPos = i
			break
		}
	}
	if targetPos < 0 {
		return turns
	}

	start := (targetPos / chunkSize) * chunkSize
	end := start + chunkSize
	if start < 0 {
		start = 0
	}
	if end > len(turns) {
		end = len(turns)
	}
	return turns[start:end]
}
