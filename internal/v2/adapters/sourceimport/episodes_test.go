package sourceimport

import (
	"slices"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestBuildEpisodes_BoundaryKeyIncludesTurnIDs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 28, 9, 0, 0, 0, time.UTC)
	parsed := ParsedSession{
		SessionID: "run-ep-boundary",
		Turns: []run.TurnRecord{
			{
				ID:        "turn-a",
				SessionID: "run-ep-boundary",
				TurnIndex: 7,
				Prompt:    "first",
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        "turn-b",
				SessionID: "run-ep-boundary",
				TurnIndex: 7,
				Prompt:    "second",
				CreatedAt: now.Add(1 * time.Minute),
				UpdatedAt: now.Add(1 * time.Minute),
			},
		},
	}

	result := BuildEpisodes(parsed, nil, EpisodeBuildOptions{
		EpisodeVersion:     "v1",
		MaxTurnsPerEpisode: 2,
		Now:                func() time.Time { return now },
	})
	if len(result.Episodes) != 1 {
		t.Fatalf("episode count=%d want 1", len(result.Episodes))
	}
	got := result.Episodes[0].BoundaryKey
	want := "chunk:0007-0007:turn-a-turn-b"
	if got != want {
		t.Fatalf("boundary_key=%q want %q", got, want)
	}
}

func TestBuildEpisodes_DuplicateIndicesProduceDistinctBoundaryKeys(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 28, 10, 0, 0, 0, time.UTC)
	parsed := ParsedSession{
		SessionID: "run-ep-duplicate-index",
		Turns: []run.TurnRecord{
			{ID: "turn-1", SessionID: "run-ep-duplicate-index", TurnIndex: 1, Prompt: "a", CreatedAt: now, UpdatedAt: now},
			{ID: "turn-2", SessionID: "run-ep-duplicate-index", TurnIndex: 1, Prompt: "b", CreatedAt: now.Add(1 * time.Minute), UpdatedAt: now.Add(1 * time.Minute)},
			{ID: "turn-3", SessionID: "run-ep-duplicate-index", TurnIndex: 2, Prompt: "c", CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute)},
			{ID: "turn-4", SessionID: "run-ep-duplicate-index", TurnIndex: 2, Prompt: "d", CreatedAt: now.Add(3 * time.Minute), UpdatedAt: now.Add(3 * time.Minute)},
		},
	}

	result := BuildEpisodes(parsed, nil, EpisodeBuildOptions{
		EpisodeVersion:     "v1",
		MaxTurnsPerEpisode: 2,
		Now:                func() time.Time { return now },
	})
	if len(result.Episodes) != 2 {
		t.Fatalf("episode count=%d want 2", len(result.Episodes))
	}

	keys := []string{result.Episodes[0].BoundaryKey, result.Episodes[1].BoundaryKey}
	if keys[0] == keys[1] {
		t.Fatalf("boundary keys should differ, got %q and %q", keys[0], keys[1])
	}
	if !slices.Contains(keys, "chunk:0001-0001:turn-1-turn-2") {
		t.Fatalf("missing expected boundary key in %v", keys)
	}
	if !slices.Contains(keys, "chunk:0002-0002:turn-3-turn-4") {
		t.Fatalf("missing expected boundary key in %v", keys)
	}
}
