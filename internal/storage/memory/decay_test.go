package memory

import (
	"testing"
	"time"
)

func TestScoreWithDecay_RecentMemoriesGetSoftBoost(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()

	recent := ScoreWithDecay(0.6, now.Add(-30*time.Minute), now.Add(-48*time.Hour), now, 0, cfg)
	older := ScoreWithDecay(0.6, now.Add(-48*time.Hour), now.Add(-48*time.Hour), now, 0, cfg)

	if recent.DecayFactor <= older.DecayFactor {
		t.Fatalf("expected recent decay factor to be higher: recent=%f older=%f", recent.DecayFactor, older.DecayFactor)
	}
	if recent.DecayFactor < cfg.MinFactor || recent.DecayFactor > cfg.MaxFactor {
		t.Fatalf("expected recent decay factor in bounds [%f,%f], got %f", cfg.MinFactor, cfg.MaxFactor, recent.DecayFactor)
	}
}

func TestScoreWithDecay_CanBeDisabledForOptInRollout(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()
	cfg.Enabled = false

	score := ScoreWithDecay(0.72, now.Add(-365*24*time.Hour), now.Add(-365*24*time.Hour), now, 100, cfg)

	if score.DecayFactor != 1 {
		t.Fatalf("disabled decay factor=%f want 1", score.DecayFactor)
	}
	if !near(score.FinalScore, 0.72) {
		t.Fatalf("disabled final score=%f want base score", score.FinalScore)
	}
	if score.Bucket != "disabled" || score.TimestampSource != "disabled" {
		t.Fatalf("disabled metadata bucket=%q source=%q", score.Bucket, score.TimestampSource)
	}
}

func near(got, want float64) bool {
	const epsilon = 0.000001
	if got > want {
		return got-want < epsilon
	}
	return want-got < epsilon
}

func TestScoreWithDecay_IdleMemoriesAreDampenedButNotBelowMin(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()

	idle := ScoreWithDecay(0.8, now.Add(-365*24*time.Hour), now.Add(-365*24*time.Hour), now, 0, cfg)

	if idle.DecayFactor < cfg.MinFactor {
		t.Fatalf("expected idle decay factor >= min (%f), got %f", cfg.MinFactor, idle.DecayFactor)
	}
	if idle.Bucket != decayBucketIdle {
		t.Fatalf("expected idle bucket %q, got %q", decayBucketIdle, idle.Bucket)
	}
}

func TestScoreWithDecay_UsesUpdatedAtWhenLastAccessedIsZero(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()

	recentUpdated := ScoreWithDecay(0.5, time.Time{}, now.Add(-2*time.Hour), now, 0, cfg)
	staleUpdated := ScoreWithDecay(0.5, time.Time{}, now.Add(-20*24*time.Hour), now, 0, cfg)

	if recentUpdated.TimestampSource != "updated_at" {
		t.Fatalf("expected updated_at fallback source, got %q", recentUpdated.TimestampSource)
	}
	if recentUpdated.DecayFactor <= staleUpdated.DecayFactor {
		t.Fatalf("expected newer updated_at to score higher: recent=%f stale=%f", recentUpdated.DecayFactor, staleUpdated.DecayFactor)
	}
}

func TestScoreWithDecay_UsesInjectedNow(t *testing.T) {
	cfg := DefaultDecayConfig()
	lastAccessed := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	firstNow := lastAccessed.Add(2 * time.Hour)
	secondNow := lastAccessed.Add(20 * 24 * time.Hour)

	fresh := ScoreWithDecay(0.6, lastAccessed, lastAccessed, firstNow, 0, cfg)
	stale := ScoreWithDecay(0.6, lastAccessed, lastAccessed, secondNow, 0, cfg)

	if fresh.DecayFactor <= stale.DecayFactor {
		t.Fatalf("expected injected later now to reduce factor: fresh=%f stale=%f", fresh.DecayFactor, stale.DecayFactor)
	}
}

func TestScoreWithDecay_StrongOldMatchCanBeatWeakRecentMatch(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()

	strongOld := ScoreWithDecay(0.95, now.Add(-120*24*time.Hour), now.Add(-120*24*time.Hour), now, 0, cfg)
	weakRecent := ScoreWithDecay(0.1, now.Add(-1*time.Hour), now.Add(-1*time.Hour), now, 0, cfg)

	if strongOld.FinalScore <= weakRecent.FinalScore {
		t.Fatalf(
			"expected strong old match to beat weak recent match: old=%f recent=%f",
			strongOld.FinalScore,
			weakRecent.FinalScore,
		)
	}
}

func TestScoreWithDecay_ClampsPublicFinalScoreToUnitInterval(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()

	high := ScoreWithDecay(5.0, now, now, now, 1000, cfg)
	low := ScoreWithDecay(-1.0, now, now, now, 0, cfg)

	if high.FinalScore != 1 {
		t.Fatalf("expected high score to clamp to 1, got %f", high.FinalScore)
	}
	if low.FinalScore != 0 {
		t.Fatalf("expected low score to clamp to 0, got %f", low.FinalScore)
	}
	if high.DecayFactor > cfg.MaxFactor {
		t.Fatalf("expected decay factor <= max (%f), got %f", cfg.MaxFactor, high.DecayFactor)
	}
}

func TestRerankScoredEntriesWithDecay_ReturnsCopiedSliceAndPreservesInput(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()

	input := []ScoredEntry{
		{
			Entry: NamedEntry{
				ID:          "a",
				Name:        "alpha",
				UpdatedAt:   now.Add(-12 * time.Hour),
				LastAccess:  now.Add(-12 * time.Hour),
				AccessCount: 0,
			},
			Score: 0.65,
		},
		{
			Entry: NamedEntry{
				ID:          "b",
				Name:        "beta",
				UpdatedAt:   now.Add(-20 * time.Minute),
				LastAccess:  now.Add(-20 * time.Minute),
				AccessCount: 2,
			},
			Score: 0.45,
		},
	}

	originalScore := input[0].Score
	originalID := input[0].Entry.ID

	got := RerankScoredEntriesWithDecay(input, now, cfg, 0)
	if len(got) != len(input) {
		t.Fatalf("len=%d want %d", len(got), len(input))
	}
	if &got[0] == &input[0] {
		t.Fatalf("expected rerank to return a copied slice")
	}
	if input[0].Score != originalScore || input[0].Entry.ID != originalID {
		t.Fatalf("input mutated: score=%f id=%q", input[0].Score, input[0].Entry.ID)
	}
	if got[0].Score < 0 || got[0].Score > 1 || got[1].Score < 0 || got[1].Score > 1 {
		t.Fatalf("expected reranked scores to be clamped into [0,1], got %f and %f", got[0].Score, got[1].Score)
	}
}

func TestRerankScoredEntriesWithDecay_DisabledConfigPreservesBaseScoreRanking(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()
	cfg.Enabled = false

	input := []ScoredEntry{
		{Entry: NamedEntry{ID: "low", Name: "low", UpdatedAt: now.Add(-1 * time.Hour)}, Score: 1.2},
		{Entry: NamedEntry{ID: "high", Name: "high", UpdatedAt: now.Add(-2 * time.Hour)}, Score: 9.3},
		{Entry: NamedEntry{ID: "mid", Name: "mid", UpdatedAt: now.Add(-3 * time.Hour)}, Score: 2.1},
	}

	got := RerankScoredEntriesWithDecay(input, now, cfg, 0)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	if got[0].Entry.ID != "high" || got[1].Entry.ID != "mid" || got[2].Entry.ID != "low" {
		t.Fatalf("unexpected ranking order: [%s %s %s]", got[0].Entry.ID, got[1].Entry.ID, got[2].Entry.ID)
	}
	if got[0].Score != 1 || got[1].Score != 1 || got[2].Score != 1 {
		t.Fatalf("expected disabled rerank to clamp visible scores to 1, got [%f %f %f]", got[0].Score, got[1].Score, got[2].Score)
	}
}

func TestRerankScoredEntriesWithDecay_DeterministicTieBreakers(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()
	cfg.Enabled = false

	updatedSame := now.Add(-2 * time.Hour)
	input := []ScoredEntry{
		{Entry: NamedEntry{ID: "9", Name: "same", UpdatedAt: updatedSame}, Score: 0.7},
		{Entry: NamedEntry{ID: "1", Name: "same", UpdatedAt: updatedSame}, Score: 0.7},
		{Entry: NamedEntry{ID: "x", Name: "beta", UpdatedAt: now.Add(-1 * time.Hour)}, Score: 0.7},
		{Entry: NamedEntry{ID: "y", Name: "alpha", UpdatedAt: now.Add(-1 * time.Hour)}, Score: 0.7},
		{Entry: NamedEntry{ID: "newer", Name: "zzz", UpdatedAt: now.Add(-30 * time.Minute)}, Score: 0.7},
	}

	got := RerankScoredEntriesWithDecay(input, now, cfg, 0)
	if len(got) != len(input) {
		t.Fatalf("len=%d want %d", len(got), len(input))
	}

	want := []string{"newer", "y", "x", "1", "9"}
	for i := range want {
		if got[i].Entry.ID != want[i] {
			t.Fatalf("index=%d id=%q want=%q", i, got[i].Entry.ID, want[i])
		}
	}
}

func TestRerankScoredEntriesWithDecay_UsesLastAccessBeforeNameTieBreak(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()
	cfg.Enabled = false
	updatedSame := now.Add(-2 * time.Hour)

	input := []ScoredEntry{
		{Entry: NamedEntry{ID: "a", Name: "alpha", UpdatedAt: updatedSame, LastAccess: now.Add(-2 * time.Hour)}, Score: 0.7},
		{Entry: NamedEntry{ID: "z", Name: "zeta", UpdatedAt: updatedSame, LastAccess: now.Add(-1 * time.Hour)}, Score: 0.7},
	}

	got := RerankScoredEntriesWithDecay(input, now, cfg, 0)
	if got[0].Entry.ID != "z" || got[1].Entry.ID != "a" {
		t.Fatalf("last_access tie-break ignored: [%s %s]", got[0].Entry.ID, got[1].Entry.ID)
	}
}

func TestRerankScoredEntriesWithDecay_AppliesLimitAfterRerank(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()
	cfg.Enabled = false

	input := []ScoredEntry{
		{Entry: NamedEntry{ID: "c", Name: "c", UpdatedAt: now}, Score: 0.3},
		{Entry: NamedEntry{ID: "a", Name: "a", UpdatedAt: now}, Score: 0.9},
		{Entry: NamedEntry{ID: "b", Name: "b", UpdatedAt: now}, Score: 0.6},
	}

	got := RerankScoredEntriesWithDecay(input, now, cfg, 2)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0].Entry.ID != "a" || got[1].Entry.ID != "b" {
		t.Fatalf("unexpected limited order: [%s %s]", got[0].Entry.ID, got[1].Entry.ID)
	}
}

func TestRerankScoredEntriesWithDecayStatsReportsFactorsAndCandidateCounts(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	cfg := DefaultDecayConfig()
	input := []ScoredEntry{
		{Entry: NamedEntry{ID: "old", Name: "old", UpdatedAt: now.Add(-60 * 24 * time.Hour), LastAccess: now.Add(-60 * 24 * time.Hour)}, Score: 0.8},
		{Entry: NamedEntry{ID: "recent", Name: "recent", UpdatedAt: now.Add(-30 * time.Minute), LastAccess: now.Add(-30 * time.Minute), AccessCount: 2}, Score: 0.7},
		{Entry: NamedEntry{ID: "warm", Name: "warm", UpdatedAt: now.Add(-2 * 24 * time.Hour), LastAccess: now.Add(-2 * 24 * time.Hour)}, Score: 0.6},
	}

	got, stats := RerankScoredEntriesWithDecayStats(input, now, cfg, 2)

	if len(got) != 2 {
		t.Fatalf("len(got)=%d want 2", len(got))
	}
	if !stats.Enabled {
		t.Fatalf("stats.Enabled=false want true")
	}
	if stats.CandidatesBefore != 3 || stats.CandidatesAfter != 2 {
		t.Fatalf("candidate counts before=%d after=%d want 3/2", stats.CandidatesBefore, stats.CandidatesAfter)
	}
	if stats.FactorMin < cfg.MinFactor || stats.FactorMax > cfg.MaxFactor || stats.FactorMin >= stats.FactorMax {
		t.Fatalf("factor bounds min=%f max=%f want within [%f,%f] with spread", stats.FactorMin, stats.FactorMax, cfg.MinFactor, cfg.MaxFactor)
	}
	if stats.FactorAvg <= stats.FactorMin || stats.FactorAvg >= stats.FactorMax {
		t.Fatalf("factor avg=%f outside expected min/max (%f,%f)", stats.FactorAvg, stats.FactorMin, stats.FactorMax)
	}
}

func TestDecayCandidateLimitWidensWithBounds(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "unbounded preserved", limit: 0, want: 0},
		{name: "negative preserved", limit: -1, want: -1},
		{name: "floor", limit: 10, want: decayCandidateMin},
		{name: "triple", limit: 200, want: 600},
		{name: "cap", limit: 10000, want: decayCandidateMax},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecayCandidateLimit(tt.limit); got != tt.want {
				t.Fatalf("DecayCandidateLimit(%d)=%d want %d", tt.limit, got, tt.want)
			}
		})
	}
}
