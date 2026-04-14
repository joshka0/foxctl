package contextbuilder_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/v2/core/run"
	"github.com/joshka0/foxctl/internal/v2/runtime/contextbuilder"
)

func TestContextBuilder_ResolveWholeTurnRef(t *testing.T) {
	t.Parallel()

	reader := &fakeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-ctx-1": {
				ID:            "turn-ctx-1",
				Prompt:        "summarize",
				CorrelationID: "trace-ctx-1",
				Iterations: []run.IterationRecord{
					{
						TurnID:         "turn-ctx-1",
						IterationIndex: 1,
						Message: run.MessageRef{
							ID:   "msg-iter-1",
							Role: "assistant",
							Text: "iteration output",
						},
						ToolCalls: []run.ToolCallRecord{
							{
								CallID: "tc-1-1",
								Name:   "fs_read",
								ResultRef: run.ArtifactRef{
									ID:   "artifact-tc-1-1",
									Kind: "tool_result",
									Text: "file contents",
								},
							},
						},
					},
				},
				FinalOutput: run.MessageRef{
					ID:   "msg-final",
					Role: "assistant",
					Text: "final answer",
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	bundle, err := builder.Build(context.Background(), contextbuilder.Request{
		Ref: "turn/turn-ctx-1",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if bundle.Kind != contextbuilder.RefWholeTurn {
		t.Fatalf("kind=%q want %q", bundle.Kind, contextbuilder.RefWholeTurn)
	}
	if bundle.TurnID != "turn-ctx-1" {
		t.Fatalf("turn_id=%q want turn-ctx-1", bundle.TurnID)
	}
	if bundle.Content == "" {
		t.Fatal("expected non-empty content")
	}
	if !strings.Contains(bundle.Content, "Iteration 1") {
		t.Fatalf("content missing iteration block: %q", bundle.Content)
	}
	if !strings.Contains(bundle.Content, "Final: final answer") {
		t.Fatalf("content missing final output: %q", bundle.Content)
	}
}

func TestContextBuilder_ResolveSliceRef(t *testing.T) {
	t.Parallel()

	reader := &fakeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-ctx-2": {
				ID: "turn-ctx-2",
				FinalOutput: run.MessageRef{
					ID:   "msg-final",
					Role: "assistant",
					Text: "abcdefghij",
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	bundle, err := builder.Build(context.Background(), contextbuilder.Request{
		Ref: "turn/turn-ctx-2#msg:msg-final:2-7",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if bundle.Kind != contextbuilder.RefSlice {
		t.Fatalf("kind=%q want %q", bundle.Kind, contextbuilder.RefSlice)
	}
	if bundle.Content != "cdefg" {
		t.Fatalf("slice=%q want cdefg", bundle.Content)
	}
}

func TestContextBuilder_ResolveEpisodeRef(t *testing.T) {
	t.Parallel()

	reader := &fakeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-ctx-ep-start": {ID: "turn-ctx-ep-start"},
			"turn-ctx-ep-end":   {ID: "turn-ctx-ep-end"},
		},
		episodes: map[string]run.EpisodeRecord{
			"ep-ctx-1": {
				ID:             "ep-ctx-1",
				SessionID:      "run-ctx-ep",
				EpisodeVersion: "v1",
				BoundaryKey:    "chunk:0001-0002",
				StartTurnID:    "turn-ctx-ep-start",
				EndTurnID:      "turn-ctx-ep-end",
				StartTurnIndex: 1,
				EndTurnIndex:   2,
				Topic:          "Migration stabilization",
				Summary:        "Captured migration retry and fix strategy.",
				SalienceScore:  0.73,
				IsLandmark:     true,
				AnchorRefs: []string{
					"turn/turn-ctx-ep-start",
					"turn/turn-ctx-ep-end",
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	bundle, err := builder.Build(context.Background(), contextbuilder.Request{
		Ref: "episode/ep-ctx-1",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if bundle.Kind != contextbuilder.RefEpisode {
		t.Fatalf("kind=%q want %q", bundle.Kind, contextbuilder.RefEpisode)
	}
	if bundle.TurnID != "turn-ctx-ep-start" {
		t.Fatalf("turn_id=%q want turn-ctx-ep-start", bundle.TurnID)
	}
	if !strings.Contains(bundle.Content, "Migration stabilization") {
		t.Fatalf("content missing topic: %q", bundle.Content)
	}
	if !strings.Contains(bundle.Content, "Landmark: true") {
		t.Fatalf("content missing landmark marker: %q", bundle.Content)
	}
	if got, _ := bundle.Meta["episode_id"].(string); got != "ep-ctx-1" {
		t.Fatalf("meta.episode_id=%q want ep-ctx-1", got)
	}
}

func TestContextBuilder_ResolveEpisodeRef_MissingEpisodeReader(t *testing.T) {
	t.Parallel()

	reader := &readOnlyTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-only": {ID: "turn-only"},
		},
	}

	builder := contextbuilder.New(reader)
	_, err := builder.Build(context.Background(), contextbuilder.Request{
		Ref: "episode/ep-missing-reader",
	})
	if !errors.Is(err, contextbuilder.ErrMissingEpisodeReader) {
		t.Fatalf("Build() error=%v want ErrMissingEpisodeReader", err)
	}
}

func TestContextBuilder_ResolveEpisodeRef_WithInjectedEpisodeReader(t *testing.T) {
	t.Parallel()

	builder := contextbuilder.New(&readOnlyTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-only": {ID: "turn-only"},
		},
	})
	builder.SetEpisodeReader(&fakeEpisodeReader{
		episodes: map[string]run.EpisodeRecord{
			"ep-injected": {
				ID:             "ep-injected",
				SessionID:      "run-ctx-ep",
				EpisodeVersion: "v1",
				BoundaryKey:    "chunk:0001-0001",
				StartTurnID:    "turn-only",
				EndTurnID:      "turn-only",
				StartTurnIndex: 1,
				EndTurnIndex:   1,
				Topic:          "Injected episode",
			},
		},
	})

	bundle, err := builder.Build(context.Background(), contextbuilder.Request{
		Ref: "episode/ep-injected",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if bundle.Kind != contextbuilder.RefEpisode {
		t.Fatalf("kind=%q want %q", bundle.Kind, contextbuilder.RefEpisode)
	}
	if got, _ := bundle.Meta["episode_id"].(string); got != "ep-injected" {
		t.Fatalf("meta.episode_id=%q want ep-injected", got)
	}
}

func TestContextBuilder_BuildTemporalDays(t *testing.T) {
	t.Parallel()

	reader := &fakeTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-a",
				SessionID: "run-ctx",
				CreatedAt: time.Date(2026, time.February, 17, 10, 0, 0, 0, time.UTC),
				Command:   "ask",
				Prompt:    "first prompt",
				FinalOutput: run.MessageRef{
					ID:   "msg-a",
					Role: "assistant",
					Text: "first result",
				},
				Iterations: []run.IterationRecord{{IterationIndex: 1}},
			},
			{
				ID:        "turn-b",
				SessionID: "run-ctx",
				CreatedAt: time.Date(2026, time.February, 17, 13, 0, 0, 0, time.UTC),
				Command:   "run",
				FinalOutput: run.MessageRef{
					ID:   "msg-b",
					Role: "assistant",
					Text: "second result",
				},
			},
			{
				ID:        "turn-c",
				SessionID: "run-ctx",
				CreatedAt: time.Date(2026, time.February, 18, 9, 0, 0, 0, time.UTC),
				Command:   "ask",
				FinalOutput: run.MessageRef{
					ID:   "msg-c",
					Role: "assistant",
					Text: "latest result",
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	bundle, err := builder.BuildTemporal(context.Background(), contextbuilder.TemporalRequest{
		SessionID: "run-ctx",
		View:      contextbuilder.ViewDays,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("BuildTemporal() error = %v", err)
	}
	if bundle.View != contextbuilder.ViewDays {
		t.Fatalf("view=%q want %q", bundle.View, contextbuilder.ViewDays)
	}
	if len(bundle.Buckets) != 2 {
		t.Fatalf("bucket count=%d want 2", len(bundle.Buckets))
	}
	if bundle.Buckets[0].Key != "2026-02-18" {
		t.Fatalf("bucket[0].key=%q want 2026-02-18", bundle.Buckets[0].Key)
	}
	if !contains(bundle.ExpandableDates, "2026-02-17") || !contains(bundle.ExpandableDates, "2026-02-18") {
		t.Fatalf("expandable_dates=%v want both days", bundle.ExpandableDates)
	}
	if !contains(bundle.ExpandableRefs, "hour:2026-02-18T09") {
		t.Fatalf("expandable_refs=%v missing hour ref", bundle.ExpandableRefs)
	}
	if !contains(bundle.ExpandableRefs, "turn/turn-c") {
		t.Fatalf("expandable_refs=%v missing turn ref", bundle.ExpandableRefs)
	}
	if !strings.Contains(bundle.Content, "Temporal view: days") {
		t.Fatalf("content missing header: %q", bundle.Content)
	}
}

func TestContextBuilder_BuildTemporalWeeks(t *testing.T) {
	t.Parallel()

	reader := &fakeTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-w1",
				SessionID: "run-week",
				CreatedAt: time.Date(2026, time.January, 30, 10, 0, 0, 0, time.UTC),
				Command:   "ask",
			},
			{
				ID:        "turn-w2",
				SessionID: "run-week",
				CreatedAt: time.Date(2026, time.February, 3, 9, 0, 0, 0, time.UTC),
				Command:   "run",
			},
		},
	}

	builder := contextbuilder.New(reader)
	bundle, err := builder.BuildTemporal(context.Background(), contextbuilder.TemporalRequest{
		SessionID: "run-week",
		View:      contextbuilder.ViewWeeks,
	})
	if err != nil {
		t.Fatalf("BuildTemporal() error = %v", err)
	}
	if len(bundle.Buckets) == 0 {
		t.Fatal("expected at least one week bucket")
	}
	if !strings.HasPrefix(bundle.Buckets[0].Key, "2026-W") {
		t.Fatalf("unexpected week bucket key %q", bundle.Buckets[0].Key)
	}
	if !containsPrefix(bundle.ExpandableRefs, "day:") {
		t.Fatalf("expandable_refs=%v missing day drill refs", bundle.ExpandableRefs)
	}
}

func TestContextBuilder_BuildTemporalRequiresTimelineReader(t *testing.T) {
	t.Parallel()

	reader := &readOnlyTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-only": {ID: "turn-only"},
		},
	}

	builder := contextbuilder.New(reader)
	_, err := builder.BuildTemporal(context.Background(), contextbuilder.TemporalRequest{
		SessionID: "run-only",
		View:      contextbuilder.ViewDays,
	})
	if !errors.Is(err, contextbuilder.ErrTemporalUnsupported) {
		t.Fatalf("BuildTemporal() error=%v want ErrTemporalUnsupported", err)
	}
}

type fakeTurnReader struct {
	turns        map[string]run.TurnRecord
	sessionTurns []run.TurnRecord
	episodes     map[string]run.EpisodeRecord
	sessionEps   []run.EpisodeRecord
}

func (f *fakeTurnReader) GetTurn(_ context.Context, turnID string) (run.TurnRecord, error) {
	if turn, ok := f.turns[turnID]; ok {
		return turn.Clone(), nil
	}
	for _, turn := range f.sessionTurns {
		if turn.ID == turnID {
			return turn.Clone(), nil
		}
	}
	return run.TurnRecord{}, run.ErrTurnNotFound
}

func (f *fakeTurnReader) ListTurns(_ context.Context, sessionID string, opts run.TurnListOptions) ([]run.TurnRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}

	filtered := make([]run.TurnRecord, 0, len(f.sessionTurns))
	for _, turn := range f.sessionTurns {
		if turn.SessionID != sessionID {
			continue
		}
		if !opts.Since.IsZero() && turn.CreatedAt.Before(opts.Since) {
			continue
		}
		if !opts.Until.IsZero() && turn.CreatedAt.After(opts.Until) {
			continue
		}
		filtered = append(filtered, turn.Clone())
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].ID < filtered[j].ID
		}
		if opts.Asc {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}
	return filtered, nil
}

func (f *fakeTurnReader) GetEpisode(_ context.Context, episodeID string) (run.EpisodeRecord, error) {
	if episode, ok := f.episodes[episodeID]; ok {
		return episode.Clone(), nil
	}
	for _, episode := range f.sessionEps {
		if episode.ID == episodeID {
			return episode.Clone(), nil
		}
	}
	return run.EpisodeRecord{}, run.ErrEpisodeNotFound
}

func (f *fakeTurnReader) ListEpisodes(_ context.Context, sessionID string, opts run.EpisodeListOptions) ([]run.EpisodeRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}

	matchesOpts := func(episode run.EpisodeRecord) bool {
		if opts.LandmarkOnly && !episode.IsLandmark {
			return false
		}
		if !opts.Since.IsZero() && episode.CreatedAt.Before(opts.Since) {
			return false
		}
		if !opts.Until.IsZero() && episode.CreatedAt.After(opts.Until) {
			return false
		}
		return true
	}

	seen := make(map[string]struct{}, len(f.sessionEps)+len(f.episodes))
	filtered := make([]run.EpisodeRecord, 0, len(f.sessionEps))
	for _, episode := range f.sessionEps {
		if episode.SessionID != sessionID {
			continue
		}
		if !matchesOpts(episode) {
			continue
		}
		if _, dup := seen[episode.ID]; dup {
			continue
		}
		seen[episode.ID] = struct{}{}
		filtered = append(filtered, episode.Clone())
	}
	for _, episode := range f.episodes {
		if episode.SessionID != sessionID {
			continue
		}
		if !matchesOpts(episode) {
			continue
		}
		if _, dup := seen[episode.ID]; dup {
			continue
		}
		seen[episode.ID] = struct{}{}
		filtered = append(filtered, episode.Clone())
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].ID < filtered[j].ID
		}
		if opts.Asc {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}
	return filtered, nil
}

type readOnlyTurnReader struct {
	turns map[string]run.TurnRecord
}

func (r *readOnlyTurnReader) GetTurn(_ context.Context, turnID string) (run.TurnRecord, error) {
	if turn, ok := r.turns[turnID]; ok {
		return turn.Clone(), nil
	}
	return run.TurnRecord{}, run.ErrTurnNotFound
}

type fakeEpisodeReader struct {
	episodes map[string]run.EpisodeRecord
}

func (f *fakeEpisodeReader) GetEpisode(_ context.Context, episodeID string) (run.EpisodeRecord, error) {
	if episode, ok := f.episodes[episodeID]; ok {
		return episode.Clone(), nil
	}
	return run.EpisodeRecord{}, run.ErrEpisodeNotFound
}

func (f *fakeEpisodeReader) ListEpisodes(_ context.Context, sessionID string, opts run.EpisodeListOptions) ([]run.EpisodeRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	out := make([]run.EpisodeRecord, 0, len(f.episodes))
	for _, episode := range f.episodes {
		if strings.TrimSpace(episode.SessionID) != sessionID {
			continue
		}
		if opts.LandmarkOnly && !episode.IsLandmark {
			continue
		}
		if !opts.Since.IsZero() && episode.CreatedAt.Before(opts.Since) {
			continue
		}
		if !opts.Until.IsZero() && episode.CreatedAt.After(opts.Until) {
			continue
		}
		out = append(out, episode.Clone())
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		if opts.Asc {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func containsPrefix(items []string, prefix string) bool {
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}
