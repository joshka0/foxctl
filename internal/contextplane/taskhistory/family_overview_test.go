package taskhistory

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/transcriptpipeline"
)

func TestDeterministicTranscriptFamilyOverview_PrefersRecentOwners(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	overview := deterministicTranscriptFamilyOverview(
		"/tmp/praze-v2-compare",
		"/tmp/praze",
		TranscriptHistoryScopeFamily,
		TranscriptHistoryScopeFamily,
		[]transcriptOwnerSummary{
			{
				OwnerPrefix: "transcript-history:older:",
				UpdatedAt:   now.Add(-45 * 24 * time.Hour),
				Pack: &transcriptpipeline.HistoryPack{
					CurrentObjective:  "Compare branch changes with main and adjust code.",
					ObjectiveLabel:    "compare changes",
					ContinueWith:      []string{"guard the unguarded"},
					AcceptedLearnings: []string{"the reflections mismatch came from divergent empty-state handling."},
					RecentSurprises:   []string{"reflections mismatch"},
					RecentEpisode:     "Implemented the requested hardening changes and pushed them to PR #116.",
				},
			},
			{
				OwnerPrefix: "transcript-history:newer:",
				UpdatedAt:   now,
				Pack: &transcriptpipeline.HistoryPack{
					CurrentObjective:  "Complete backend integration tasks while updating the plan.",
					ObjectiveLabel:    "complete backend tasks",
					ContinueWith:      []string{"finalize integration checks"},
					AcceptedLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
					RecentSurprises:   []string{"split socket targets"},
					RecentEpisode:     "Canonical integration now supports split socket targets.",
				},
			},
		},
		nil,
		[]string{"exec_command sandbox denied"},
	)
	if overview == nil {
		t.Fatal("expected overview")
		return
	}
	if len(overview.CurrentFocus) == 0 || overview.CurrentFocus[0] != "complete backend tasks" {
		t.Fatalf("current_focus=%v", overview.CurrentFocus)
	}
	if len(overview.RecentChanges) == 0 || overview.RecentChanges[0] != "Canonical integration now supports split socket targets." {
		t.Fatalf("recent_changes=%v", overview.RecentChanges)
	}
	if len(overview.TopLearnings) == 0 || overview.TopLearnings[0] != "non-presence hooks are real instead of client-only scaffolds." {
		t.Fatalf("top_learnings=%v", overview.TopLearnings)
	}
	if len(overview.TopSurprises) == 0 || overview.TopSurprises[0] != "split socket targets" {
		t.Fatalf("top_surprises=%v", overview.TopSurprises)
	}
	if len(overview.NextWork) == 0 || overview.NextWork[0] != "finalize integration checks" {
		t.Fatalf("next_work=%v", overview.NextWork)
	}
	if len(overview.RecurringMistakes) == 0 || overview.RecurringMistakes[0] != "exec_command sandbox denied" {
		t.Fatalf("recurring_mistakes=%v", overview.RecurringMistakes)
	}
}

func TestRefineTranscriptFamilyOverview_PreservesDeterministicRecencyOrdering(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	summaries := []transcriptOwnerSummary{
		{
			OwnerPrefix: "transcript-history:newer:",
			UpdatedAt:   now,
			Pack: &transcriptpipeline.HistoryPack{
				CurrentObjective:  "Complete backend integration tasks while updating the plan.",
				ObjectiveLabel:    "complete backend tasks",
				ContinueWith:      []string{"finalize integration checks"},
				AcceptedLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
			},
		},
		{
			OwnerPrefix: "transcript-history:older:",
			UpdatedAt:   now.Add(-30 * 24 * time.Hour),
			Pack: &transcriptpipeline.HistoryPack{
				CurrentObjective:  "Compare branch changes with main and adjust code.",
				ObjectiveLabel:    "compare changes",
				ContinueWith:      []string{"guard the unguarded"},
				AcceptedLearnings: []string{"the reflections mismatch came from divergent empty-state handling."},
			},
		},
	}
	base := deterministicTranscriptFamilyOverview(
		"/tmp/praze-v2-compare",
		"/tmp/praze",
		TranscriptHistoryScopeFamily,
		TranscriptHistoryScopeFamily,
		summaries,
		nil,
		[]string{"exec_command sandbox denied"},
	)
	if base == nil {
		t.Fatal("expected base overview")
	}

	collector := Collector{
		TranscriptWorker: &transcriptpipeline.WorkerConfig{Provider: "lmstudio", Model: "test"},
		TranscriptRun: func(_ context.Context, _ transcriptpipeline.WorkerConfig, task transcriptpipeline.Task) (transcriptpipeline.Result, error) {
			switch task.InputKind {
			case "transcript_family_overview":
				return transcriptpipeline.Result{
					ModelID:    "test:model",
					OutputText: `{"current_focus":["compare changes"],"next_work":["guard the unguarded"],"top_learnings":["the reflections mismatch came from divergent empty-state handling."],"brief":"Current focus: compare changes\nNext work: guard the unguarded"}`,
				}, nil
			case "transcript_family_overview_lists":
				return transcriptpipeline.Result{}, fmt.Errorf("skip cleanup for this regression")
			default:
				return transcriptpipeline.Result{}, fmt.Errorf("unexpected input kind %q", task.InputKind)
			}
		},
	}
	refined := collector.refineTranscriptFamilyOverview(context.Background(), summaries, base)
	if refined == nil {
		t.Fatal("expected refined overview")
		return
	}
	if len(refined.CurrentFocus) == 0 || refined.CurrentFocus[0] != "complete backend tasks" {
		t.Fatalf("current_focus=%v", refined.CurrentFocus)
	}
	if len(refined.NextWork) == 0 || refined.NextWork[0] != "finalize integration checks" {
		t.Fatalf("next_work=%v", refined.NextWork)
	}
	if len(refined.TopLearnings) == 0 || refined.TopLearnings[0] != "non-presence hooks are real instead of client-only scaffolds." {
		t.Fatalf("top_learnings=%v", refined.TopLearnings)
	}
}

func TestRefineTranscriptFamilyOverview_ReplacesLowerPriorityDeterministicItems(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	summaries := []transcriptOwnerSummary{
		{
			OwnerPrefix: "transcript-history:newer:",
			UpdatedAt:   now,
			Pack: &transcriptpipeline.HistoryPack{
				CurrentObjective: "Complete backend integration tasks while updating the plan.",
				ObjectiveLabel:   "complete backend tasks",
				ContinueWith: []string{
					"map the current rebuilt frontend architecture to these Paper target surfaces",
					"make any adjustments to improve the code",
				},
				AcceptedLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
			},
		},
		{
			OwnerPrefix: "transcript-history:older:",
			UpdatedAt:   now.Add(-30 * 24 * time.Hour),
			Pack: &transcriptpipeline.HistoryPack{
				CurrentObjective: "Make our app production ready.",
				ObjectiveLabel:   "make our app production ready",
				ContinueWith:     []string{"guard the unguarded"},
			},
		},
	}
	base := deterministicTranscriptFamilyOverview(
		"/tmp/praze-v2-compare",
		"/tmp/praze",
		TranscriptHistoryScopeFamily,
		TranscriptHistoryScopeFamily,
		summaries,
		nil,
		nil,
	)
	if base == nil {
		t.Fatal("expected base overview")
	}

	collector := Collector{
		TranscriptWorker: &transcriptpipeline.WorkerConfig{Provider: "lmstudio", Model: "test"},
		TranscriptRun: func(_ context.Context, _ transcriptpipeline.WorkerConfig, task transcriptpipeline.Task) (transcriptpipeline.Result, error) {
			switch task.InputKind {
			case "transcript_family_overview":
				return transcriptpipeline.Result{
					ModelID:    "test:model",
					OutputText: `{"next_work":["Production readiness hardening"],"current_focus":["Complete backend tasks","Production readiness hardening"]}`,
				}, nil
			case "transcript_family_overview_lists":
				return transcriptpipeline.Result{}, fmt.Errorf("skip cleanup for this regression")
			default:
				return transcriptpipeline.Result{}, fmt.Errorf("unexpected input kind %q", task.InputKind)
			}
		},
	}
	refined := collector.refineTranscriptFamilyOverview(context.Background(), summaries, base)
	if refined == nil {
		t.Fatal("expected refined overview")
		return
	}
	if len(refined.NextWork) < 2 {
		t.Fatalf("next_work=%v", refined.NextWork)
	}
	if refined.NextWork[0] != "map the current rebuilt frontend architecture to these Paper target surfaces" {
		t.Fatalf("next_work=%v missing preserved deterministic anchor", refined.NextWork)
	}
	if refined.NextWork[1] != "Production readiness hardening" {
		t.Fatalf("next_work=%v missing cleaner llm replacement", refined.NextWork)
	}
	if containsString(refined.NextWork, "make any adjustments to improve the code") {
		t.Fatalf("next_work=%v kept lower-priority deterministic filler", refined.NextWork)
	}
}

func TestDeterministicTranscriptFamilyOverview_PrefersSupportedThemesOverOneOffs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	overview := deterministicTranscriptFamilyOverview(
		"/tmp/praze-v2-compare",
		"/tmp/praze",
		TranscriptHistoryScopeFamily,
		TranscriptHistoryScopeFamily,
		[]transcriptOwnerSummary{
			{
				OwnerPrefix: "transcript-history:newest:",
				UpdatedAt:   now,
				Pack: &transcriptpipeline.HistoryPack{
					ObjectiveLabel:    "complete backend tasks",
					AcceptedLearnings: []string{"instrument the feature-flag audit trail before rollout."},
				},
			},
			{
				OwnerPrefix: "transcript-history:recent-a:",
				UpdatedAt:   now.Add(-7 * 24 * time.Hour),
				Pack: &transcriptpipeline.HistoryPack{
					ObjectiveLabel:    "complete backend tasks",
					AcceptedLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
				},
			},
			{
				OwnerPrefix: "transcript-history:recent-b:",
				UpdatedAt:   now.Add(-10 * 24 * time.Hour),
				Pack: &transcriptpipeline.HistoryPack{
					ObjectiveLabel:    "map frontend architecture",
					AcceptedLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
				},
			},
		},
		nil,
		nil,
	)
	if overview == nil {
		t.Fatal("expected overview")
		return
	}
	if len(overview.TopLearnings) == 0 || overview.TopLearnings[0] != "non-presence hooks are real instead of client-only scaffolds." {
		t.Fatalf("top_learnings=%v", overview.TopLearnings)
	}
}

func TestDeterministicTranscriptFamilyOverview_CollectsRecurringLearningsAcrossOwners(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	overview := deterministicTranscriptFamilyOverview(
		"/tmp/praze-v2-compare",
		"/tmp/praze",
		TranscriptHistoryScopeFamily,
		TranscriptHistoryScopeFamily,
		[]transcriptOwnerSummary{
			{
				OwnerPrefix: "transcript-history:a:",
				UpdatedAt:   now,
				Pack: &transcriptpipeline.HistoryPack{
					ObjectiveLabel:    "complete backend tasks",
					AcceptedLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
				},
			},
			{
				OwnerPrefix: "transcript-history:b:",
				UpdatedAt:   now.Add(-5 * 24 * time.Hour),
				Pack: &transcriptpipeline.HistoryPack{
					ObjectiveLabel:    "production readiness hardening",
					AcceptedLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
				},
			},
		},
		[]string{"non-presence hooks are real instead of client-only scaffolds."},
		nil,
	)
	if overview == nil {
		t.Fatal("expected overview")
		return
	}
	if len(overview.RecurringLearnings) == 0 || overview.RecurringLearnings[0] != "non-presence hooks are real instead of client-only scaffolds." {
		t.Fatalf("recurring_learnings=%v", overview.RecurringLearnings)
	}
}

func TestBuildTranscriptFamilyOverviewArtifact_IncludesSupportHints(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	summaries := []transcriptOwnerSummary{
		{
			OwnerPrefix: "transcript-history:a:",
			UpdatedAt:   now,
			Pack: &transcriptpipeline.HistoryPack{
				ObjectiveLabel:    "complete backend tasks",
				AcceptedLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
				ContinueWith:      []string{"finalize integration checks"},
			},
		},
		{
			OwnerPrefix: "transcript-history:b:",
			UpdatedAt:   now.Add(-24 * time.Hour),
			Pack: &transcriptpipeline.HistoryPack{
				ObjectiveLabel:    "map frontend architecture",
				AcceptedLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
				ContinueWith:      []string{"finalize integration checks"},
			},
		},
	}
	artifact := buildTranscriptFamilyOverviewArtifact(summaries, &TranscriptFamilyOverview{
		RecurringMistakes: []string{"exec_command sandbox denied"},
	})
	if !strings.Contains(artifact, "deterministic_top_learnings: non-presence hooks are real instead of client-only scaffolds. [owners=2]") {
		t.Fatalf("artifact=%q missing learning support hint", artifact)
	}
	if !strings.Contains(artifact, "deterministic_next_work: finalize integration checks [owners=2]") {
		t.Fatalf("artifact=%q missing next_work support hint", artifact)
	}
}

func TestBuildTranscriptFamilySupportMetadata_IncludesOwnerCountAndRecency(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	summaries := []transcriptOwnerSummary{
		{
			OwnerPrefix: "transcript-history:a:",
			UpdatedAt:   now,
			Pack: &transcriptpipeline.HistoryPack{
				ObjectiveLabel:    "complete backend tasks",
				AcceptedLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
				ContinueWith:      []string{"finalize integration checks"},
			},
		},
		{
			OwnerPrefix: "transcript-history:b:",
			UpdatedAt:   now.Add(-48 * time.Hour),
			Pack: &transcriptpipeline.HistoryPack{
				ObjectiveLabel:    "production readiness hardening",
				AcceptedLearnings: []string{"non-presence hooks function as real backend components."},
				ContinueWith:      []string{"finalize integration checks"},
			},
		},
	}
	overview := &TranscriptFamilyOverview{
		CurrentFocus:       []string{"complete backend tasks"},
		RecurringLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
		NextWork:           []string{"finalize integration checks"},
	}
	meta := buildTranscriptFamilySupportMetadata(summaries, overview)
	if len(meta) == 0 {
		t.Fatal("expected support metadata")
	}
	foundRecurring := false
	for _, item := range meta {
		if item.Category != "recurring_learnings" {
			continue
		}
		foundRecurring = true
		if item.OwnerCount != 2 {
			t.Fatalf("item=%+v want owner_count=2", item)
		}
		if item.LatestAgeDays != 0 {
			t.Fatalf("item=%+v want latest_age_days=0", item)
		}
		if len(item.SourceOwners) != 2 {
			t.Fatalf("item=%+v want 2 source owners", item)
		}
	}
	if !foundRecurring {
		t.Fatalf("metadata=%+v missing recurring learning support", meta)
	}
}

func TestCleanupTranscriptFamilyOverview_CleansLowerSignalListTail(t *testing.T) {
	t.Parallel()

	base := &TranscriptFamilyOverview{
		WorkspacePath: "/tmp/praze-v2-compare",
		FamilyPath:    "/tmp/praze",
		SummaryMode:   "deterministic",
		CurrentFocus: []string{
			"complete backend tasks",
			"make any adjustments to improve the code",
			"make our app production ready",
		},
		RecentChanges: []string{
			"Anonymous guest sessions are now wired back into the canonical app.",
			"Implemented on the requested worktree and committed.",
		},
		NextWork: []string{
			"map the current rebuilt frontend architecture to these Paper target surfaces",
			"make any adjustments to improve the code",
		},
		TopLearnings: []string{"non-presence hooks are real instead of client-only scaffolds."},
	}
	collector := Collector{
		TranscriptWorker: &transcriptpipeline.WorkerConfig{Provider: "lmstudio", Model: "test"},
		TranscriptRun: func(_ context.Context, _ transcriptpipeline.WorkerConfig, task transcriptpipeline.Task) (transcriptpipeline.Result, error) {
			if task.InputKind != "transcript_family_overview_lists" {
				return transcriptpipeline.Result{}, fmt.Errorf("unexpected input kind %q", task.InputKind)
			}
			return transcriptpipeline.Result{
				ModelID:    "test:cleanup",
				OutputText: `{"current_focus":["complete backend tasks","production readiness hardening"],"recent_changes":["Anonymous guest sessions are now wired back into the canonical app."],"next_work":["map the current rebuilt frontend architecture to these Paper target surfaces","production readiness hardening"],"brief":"Current focus: complete backend tasks | production readiness hardening\nNext work: map the current rebuilt frontend architecture to these Paper target surfaces | production readiness hardening"}`,
			}, nil
		},
	}
	cleaned := collector.cleanupTranscriptFamilyOverview(context.Background(), base)
	if cleaned == nil {
		t.Fatal("expected cleaned overview")
		return
	}
	if cleaned.SummaryMode != "llm_cleanup" {
		t.Fatalf("summary_mode=%q", cleaned.SummaryMode)
	}
	if containsString(cleaned.CurrentFocus, "make any adjustments to improve the code") {
		t.Fatalf("current_focus=%v kept filler item", cleaned.CurrentFocus)
	}
	if containsString(cleaned.RecentChanges, "Implemented on the requested worktree and committed.") {
		t.Fatalf("recent_changes=%v kept generic progress chatter", cleaned.RecentChanges)
	}
	if containsString(cleaned.NextWork, "make any adjustments to improve the code") {
		t.Fatalf("next_work=%v kept filler next step", cleaned.NextWork)
	}
	if len(cleaned.CurrentFocus) < 2 || cleaned.CurrentFocus[1] != "production readiness hardening" {
		t.Fatalf("current_focus=%v missing cleaned replacement", cleaned.CurrentFocus)
	}
}

func TestCleanTranscriptFamilyOverviewLists_DropsFragmentaryItems(t *testing.T) {
	t.Parallel()

	overview := &TranscriptFamilyOverview{
		TopLearnings: []string{
			"non-presence hooks are real instead of client-only scaffolds.",
			"the reflections list was fetched from optiona…",
			"The “5 reflections but `No reflections…`” state was a mismatch between:",
			"most surfaces are still first-pass renderers over rebuilt endpoints rather than polished destination experiences.",
		},
		TopSurprises: []string{
			"The “5 reflections but `No reflections…`” state was a mismatch between:",
			"most surfaces are still first-pass renderers over rebuilt endpoints rather than polished destination experiences.",
		},
	}
	cleanTranscriptFamilyOverviewLists(overview)
	if containsString(overview.TopLearnings, "the reflections list was fetched from optiona…") {
		t.Fatalf("top_learnings=%v kept truncated item", overview.TopLearnings)
	}
	if containsString(overview.TopLearnings, "The “5 reflections but `No reflections…`” state was a mismatch between:") {
		t.Fatalf("top_learnings=%v kept dangling fragment", overview.TopLearnings)
	}
	if containsString(overview.TopSurprises, "The “5 reflections but `No reflections…`” state was a mismatch between:") {
		t.Fatalf("top_surprises=%v kept dangling fragment", overview.TopSurprises)
	}
	if len(overview.TopLearnings) == 0 || overview.TopLearnings[0] != "non-presence hooks are real instead of client-only scaffolds." {
		t.Fatalf("top_learnings=%v", overview.TopLearnings)
	}
}
