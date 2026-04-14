package sourceimport

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/v2/adapters/libsql/turns"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestSessionEpisodeCompiler_Compile_ReturnsAffectedEpisode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
	turnsReader := &fakeTurnTimelineReader{
		turns: []run.TurnRecord{
			{ID: "turn-1", SessionID: "run-ep-live", TurnIndex: 1, Prompt: "initial setup", CreatedAt: now, UpdatedAt: now},
			{ID: "turn-2", SessionID: "run-ep-live", TurnIndex: 2, Prompt: "we decided to split runtime wiring", CreatedAt: now.Add(1 * time.Minute), UpdatedAt: now.Add(1 * time.Minute)},
		},
	}
	artifactReader := fakeTurnArtifactReader{
		artifactsByTurn: map[string][]turns.Artifact{
			"turn-1": {
				{
					TurnID:          "turn-1",
					ArtifactType:    turns.ArtifactTypeAnnotation,
					ArtifactVersion: "v1",
					Ref:             "artifact/turn-1/annotation/v1",
				},
			},
			"turn-2": {
				{
					TurnID:          "turn-2",
					ArtifactType:    turns.ArtifactTypeClassification,
					ArtifactVersion: "v1",
					Ref:             "artifact/turn-2/classification/v1",
				},
			},
		},
	}

	compiler := NewSessionEpisodeCompiler(
		turnsReader,
		artifactReader,
		EpisodeBuildOptions{
			EpisodeVersion:     "v1",
			MaxTurnsPerEpisode: 2,
			Now:                func() time.Time { return now },
		},
	)

	out, err := compiler.Compile(context.Background(), run.TurnRecord{
		ID:        "turn-2",
		SessionID: "run-ep-live",
		TurnIndex: 2,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("episode count=%d want 1", len(out))
	}
	if out[0].BoundaryKey != "chunk:0001-0002:turn-1-turn-2" {
		t.Fatalf("boundary_key=%q want chunk:0001-0002:turn-1-turn-2", out[0].BoundaryKey)
	}
	if out[0].StartTurnID != "turn-1" || out[0].EndTurnID != "turn-2" {
		t.Fatalf("turn span=%q..%q want turn-1..turn-2", out[0].StartTurnID, out[0].EndTurnID)
	}
}

func TestSessionEpisodeCompiler_Compile_MissingDependencies(t *testing.T) {
	t.Parallel()

	compiler := NewSessionEpisodeCompiler(nil, nil, EpisodeBuildOptions{})
	_, err := compiler.Compile(context.Background(), run.TurnRecord{SessionID: "run-x"})
	if !errors.Is(err, ErrMissingEpisodeTurnTimeline) {
		t.Fatalf("error=%v want ErrMissingEpisodeTurnTimeline", err)
	}

	compiler = NewSessionEpisodeCompiler(&fakeTurnTimelineReader{}, nil, EpisodeBuildOptions{})
	_, err = compiler.Compile(context.Background(), run.TurnRecord{SessionID: "run-x"})
	if !errors.Is(err, ErrMissingEpisodeArtifactReader) {
		t.Fatalf("error=%v want ErrMissingEpisodeArtifactReader", err)
	}
}

func TestSessionEpisodeCompiler_Compile_TargetTurnNotInTimeline(t *testing.T) {
	t.Parallel()

	compiler := NewSessionEpisodeCompiler(
		&fakeTurnTimelineReader{
			turns: []run.TurnRecord{
				{ID: "turn-1", SessionID: "run-ep-live", TurnIndex: 1},
			},
		},
		fakeTurnArtifactReader{},
		EpisodeBuildOptions{MaxTurnsPerEpisode: 2},
	)

	_, err := compiler.Compile(context.Background(), run.TurnRecord{
		ID:        "turn-missing",
		SessionID: "run-ep-live",
		TurnIndex: 99,
	})
	if !errors.Is(err, ErrEpisodeTurnNotInTimeline) {
		t.Fatalf("error=%v want ErrEpisodeTurnNotInTimeline", err)
	}
}

func TestSessionEpisodeCompiler_Compile_StopsOnCanceledContext(t *testing.T) {
	t.Parallel()

	turnsReader := &fakeTurnTimelineReader{
		turns: []run.TurnRecord{
			{ID: "turn-1", SessionID: "run-ep-live", TurnIndex: 1},
		},
	}
	compiler := NewSessionEpisodeCompiler(turnsReader, fakeTurnArtifactReader{}, EpisodeBuildOptions{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := compiler.Compile(ctx, run.TurnRecord{
		ID:        "turn-1",
		SessionID: "run-ep-live",
		TurnIndex: 1,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want context.Canceled", err)
	}
	if calls := turnsReader.calls.Load(); calls != 0 {
		t.Fatalf("timeline calls=%d want 0 when context canceled early", calls)
	}
}

func TestSessionEpisodeCompiler_Compile_ZeroIndexFallsBackToTurnRefs(t *testing.T) {
	t.Parallel()

	turnsReader := &fakeTurnTimelineReader{
		turns: []run.TurnRecord{
			{ID: "turn-1", SessionID: "run-ep-live", TurnIndex: 1, Prompt: "start"},
			{ID: "turn-2", SessionID: "run-ep-live", TurnIndex: 2, Prompt: "middle step"},
			{ID: "turn-3", SessionID: "run-ep-live", TurnIndex: 3, Prompt: "end"},
		},
	}
	artifactReader := fakeTurnArtifactReader{
		artifactsByTurn: map[string][]turns.Artifact{
			"turn-1": {{TurnID: "turn-1", ArtifactType: turns.ArtifactTypeAnnotation, ArtifactVersion: "v1", Ref: "artifact/turn-1/annotation/v1"}},
			"turn-2": {{TurnID: "turn-2", ArtifactType: turns.ArtifactTypeAnnotation, ArtifactVersion: "v1", Ref: "artifact/turn-2/annotation/v1"}},
			"turn-3": {{TurnID: "turn-3", ArtifactType: turns.ArtifactTypeAnnotation, ArtifactVersion: "v1", Ref: "artifact/turn-3/annotation/v1"}},
		},
	}
	compiler := NewSessionEpisodeCompiler(
		turnsReader,
		artifactReader,
		EpisodeBuildOptions{
			EpisodeVersion:     "v1",
			MaxTurnsPerEpisode: 3,
		},
	)

	out, err := compiler.Compile(context.Background(), run.TurnRecord{
		ID:        "turn-2",
		SessionID: "run-ep-live",
		TurnIndex: 0,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("episode count=%d want 1", len(out))
	}
	if out[0].StartTurnID != "turn-1" || out[0].EndTurnID != "turn-3" {
		t.Fatalf("turn span=%q..%q want turn-1..turn-3", out[0].StartTurnID, out[0].EndTurnID)
	}
}

func TestSessionEpisodeCompiler_Compile_ExpandsTimelineWindowUntilTargetIncluded(t *testing.T) {
	t.Parallel()

	turnsData := make([]run.TurnRecord, 0, 4500)
	for i := 1; i <= 4500; i++ {
		turnsData = append(turnsData, run.TurnRecord{
			ID:        fmt.Sprintf("turn-%04d", i),
			SessionID: "run-large",
			TurnIndex: i,
			Prompt:    "turn prompt",
		})
	}
	turnsReader := &fakeTurnTimelineReader{turns: turnsData}
	compiler := NewSessionEpisodeCompiler(turnsReader, &fakeTurnArtifactReader{}, EpisodeBuildOptions{
		MaxTurnsPerEpisode: 8,
	})

	out, err := compiler.Compile(context.Background(), run.TurnRecord{
		ID:        "turn-4500",
		SessionID: "run-large",
		TurnIndex: 4500,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected at least one episode")
	}
	if out[0].BoundaryKey != "chunk:4497-4500:turn-4497-turn-4500" {
		t.Fatalf("boundary_key=%q want chunk:4497-4500:turn-4497-turn-4500", out[0].BoundaryKey)
	}
	if calls := turnsReader.calls.Load(); calls < 1 {
		t.Fatalf("timeline calls=%d want >=1", calls)
	}
}

type fakeTurnTimelineReader struct {
	turns []run.TurnRecord
	err   error
	calls atomic.Int32
}

func (f *fakeTurnTimelineReader) ListTurns(_ context.Context, sessionID string, opts run.TurnListOptions) ([]run.TurnRecord, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	limit := opts.Limit
	if limit <= 0 || limit > len(f.turns) {
		limit = len(f.turns)
	}
	filtered := make([]run.TurnRecord, 0, len(f.turns))
	for _, turn := range f.turns {
		if sessionID != "" && turn.SessionID != sessionID {
			continue
		}
		filtered = append(filtered, turn.Clone())
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].TurnIndex == filtered[j].TurnIndex {
			return filtered[i].ID < filtered[j].ID
		}
		return filtered[i].TurnIndex < filtered[j].TurnIndex
	})
	if !opts.Asc {
		for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
			filtered[i], filtered[j] = filtered[j], filtered[i]
		}
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	out := make([]run.TurnRecord, 0, len(filtered))
	for _, turn := range filtered {
		out = append(out, turn.Clone())
	}
	return out, nil
}

type fakeTurnArtifactReader struct {
	artifactsByTurn map[string][]turns.Artifact
	err             error
}

func (f fakeTurnArtifactReader) ListArtifacts(_ context.Context, turnID string) ([]turns.Artifact, error) {
	if f.err != nil {
		return nil, f.err
	}
	items := f.artifactsByTurn[turnID]
	out := make([]turns.Artifact, 0, len(items))
	for _, artifact := range items {
		out = append(out, artifact.Clone())
	}
	return out, nil
}
