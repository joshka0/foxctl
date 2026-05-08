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
