package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for constants

func readEvents(t *testing.T, dir string) []observability.Event {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "events", observability.EventFileName+".ndjson"))
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
	events := make([]observability.Event, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event observability.Event
		require.NoError(t, json.Unmarshal(line, &event))
		events = append(events, event)
	}
	return events
}

func TestEmitSessionRestoreTelemetry(t *testing.T) {
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})
	t.Cleanup(func() {
		observability.SetObsDirForTesting("")
		observability.SetSamplerForTesting(nil)
	})

	emitSessionRestoreTelemetry(context.Background(), nil, Input{
		Trigger:          "compact",
		Workspace:        "/workspace",
		MaxSearchResults: 3,
	}, Output{
		SnapshotID:    "snap-123",
		ItemsRestored: 2,
		KeyQuestions:  []string{"question"},
		SearchResults: []SemanticSearchResult{{Results: []string{"path-a", "path-b"}}},
		RelevantRecords: []memorycore.Record{
			{
				Kind: memorycore.KindDecision,
				Lifecycle: memorycore.LifecycleEnvelope{
					State: memorycore.LifecycleStateActive,
				},
			},
			{
				Kind: memorycore.KindSemanticFact,
				Lifecycle: memorycore.LifecycleEnvelope{
					State: memorycore.LifecycleStateStale,
				},
			},
		},
	}, "", 10*time.Millisecond)

	var found *observability.Event
	for _, event := range readEvents(t, obsDir) {
		if event.Operation == observability.OpMemorySessionRestore {
			found = &event
			break
		}
	}
	require.NotNil(t, found, "memory.session_restore event not emitted")
	require.Equal(t, observability.ComponentSkill, observability.EventDataString(found, observability.DataKeyComponent))
	require.Equal(t, command, found.Name)
	require.Equal(t, "/workspace", observability.EventDataString(found, observability.DataKeyWorkspaceID))
	require.Equal(t, observability.StatusOK, found.Status)
	require.Equal(t, "compact", found.Data["trigger"])
	require.Equal(t, float64(2), found.Data["relevant_memory_records"])
	require.Equal(t, float64(2), found.Data["semantic_result_items"])

	kindCounts := found.Data["record_kind_counts"].(map[string]any)
	require.Equal(t, float64(1), kindCounts["decision"])
	require.Equal(t, float64(1), kindCounts["semantic_fact"])

	lifecycleCounts := found.Data["record_lifecycle_counts"].(map[string]any)
	require.Equal(t, float64(1), lifecycleCounts["active"])
	require.Equal(t, float64(1), lifecycleCounts["stale"])
}

// Tests for Input structure

func TestInput_TriggerValues(t *testing.T) {
	triggers := []string{"compact", "resume", "startup"}

	for _, trigger := range triggers {
		in := Input{Trigger: trigger}
		assert.Equal(t, trigger, in.Trigger)
	}
}

// Tests for SessionSnapshot structure

func TestTaskInfo_StatusValues(t *testing.T) {
	statuses := []string{"pending", "in_progress", "completed", "blocked"}

	for _, status := range statuses {
		task := TaskInfo{Status: status}
		assert.Equal(t, status, task.Status)
	}
}

// Tests for HookOutput structure

func TestHookOutput_DecisionValues(t *testing.T) {
	decisions := []string{"approve", "block", "none"}

	for _, decision := range decisions {
		hook := HookOutput{Decision: decision}
		assert.Equal(t, decision, hook.Decision)
	}
}

// Tests for SemanticSearchResult structure

func TestSemanticSearchResult_EmptyResults(t *testing.T) {
	result := SemanticSearchResult{
		Question: "No results query",
		Results:  []string{},
	}

	assert.NotNil(t, result.Results)
	assert.Len(t, result.Results, 0)
}

// Tests for SimilarContextWindow structure

func TestAnchorInfo_EmptyLearnings(t *testing.T) {
	anchor := AnchorInfo{
		AnchorID:   "anchor-456",
		MainPrompt: "Test prompt",
	}

	assert.Nil(t, anchor.Learnings)
}

// Tests for Output structure

func TestFormatAge_JustNow(t *testing.T) {
	now := time.Now()
	result := formatAge(now)
	assert.Equal(t, "just now", result)
}

func TestFormatAge_Seconds(t *testing.T) {
	t30SecondsAgo := time.Now().Add(-30 * time.Second)
	result := formatAge(t30SecondsAgo)
	assert.Equal(t, "just now", result)
}

func TestFormatAge_Minutes(t *testing.T) {
	t5MinutesAgo := time.Now().Add(-5 * time.Minute)
	result := formatAge(t5MinutesAgo)
	assert.Equal(t, "5m", result)
}

func TestFormatAge_Hours(t *testing.T) {
	t3HoursAgo := time.Now().Add(-3 * time.Hour)
	result := formatAge(t3HoursAgo)
	assert.Equal(t, "3h", result)
}

func TestFormatAge_Days(t *testing.T) {
	t2DaysAgo := time.Now().Add(-48 * time.Hour)
	result := formatAge(t2DaysAgo)
	assert.Equal(t, "2d", result)
}

func TestFormatAge_OneMinute(t *testing.T) {
	t1MinuteAgo := time.Now().Add(-1*time.Minute - 1*time.Second)
	result := formatAge(t1MinuteAgo)
	assert.Equal(t, "1m", result)
}

func TestFormatAge_OneHour(t *testing.T) {
	t1HourAgo := time.Now().Add(-1*time.Hour - 1*time.Minute)
	result := formatAge(t1HourAgo)
	assert.Equal(t, "1h", result)
}

func TestFormatAge_OneDay(t *testing.T) {
	t1DayAgo := time.Now().Add(-25 * time.Hour)
	result := formatAge(t1DayAgo)
	assert.Equal(t, "1d", result)
}

// Tests for countItems helper

func TestCountItems_Empty(t *testing.T) {
	snap := SessionSnapshot{}
	count := countItems(snap)
	assert.Equal(t, 0, count)
}

func TestCountItems_WithActivePlan(t *testing.T) {
	snap := SessionSnapshot{
		ActivePlan: &PlanInfo{Title: "Test Plan"},
	}
	count := countItems(snap)
	assert.Equal(t, 1, count)
}

func TestCountItems_WithActiveTask(t *testing.T) {
	snap := SessionSnapshot{
		ActiveTask: &TaskInfo{Title: "Test Task"},
	}
	count := countItems(snap)
	assert.Equal(t, 1, count)
}

func TestCountItems_WithPendingTodos(t *testing.T) {
	snap := SessionSnapshot{
		PendingTodos: []TaskInfo{
			{Title: "Todo 1"},
			{Title: "Todo 2"},
			{Title: "Todo 3"},
		},
	}
	count := countItems(snap)
	assert.Equal(t, 3, count)
}

func TestCountItems_WithDecisions(t *testing.T) {
	snap := SessionSnapshot{
		Decisions: []string{"decision1", "decision2"},
	}
	count := countItems(snap)
	assert.Equal(t, 2, count)
}

func TestCountItems_WithInsights(t *testing.T) {
	snap := SessionSnapshot{
		Insights: []string{"insight1"},
	}
	count := countItems(snap)
	assert.Equal(t, 1, count)
}

func TestCountItems_AllTypes(t *testing.T) {
	snap := SessionSnapshot{
		ActivePlan:   &PlanInfo{Title: "Plan"},
		ActiveTask:   &TaskInfo{Title: "Task"},
		PendingTodos: []TaskInfo{{Title: "Todo1"}, {Title: "Todo2"}},
		Decisions:    []string{"d1", "d2", "d3"},
		Insights:     []string{"i1"},
	}
	count := countItems(snap)
	// 1 (plan) + 1 (task) + 2 (todos) + 3 (decisions) + 1 (insight) = 8
	assert.Equal(t, 8, count)
}

// Tests for buildContextQuery helper

func TestBuildContextQuery_Empty(t *testing.T) {
	snap := SessionSnapshot{}
	query := buildContextQuery(snap)
	assert.Empty(t, query)
}

func TestBuildContextQuery_WithActiveTask(t *testing.T) {
	snap := SessionSnapshot{
		ActiveTask: &TaskInfo{Title: "Implement auth"},
	}
	query := buildContextQuery(snap)
	assert.Equal(t, "Implement auth", query)
}

func TestBuildContextQuery_WithActivePlan(t *testing.T) {
	snap := SessionSnapshot{
		ActivePlan: &PlanInfo{Title: "Auth Plan"},
	}
	query := buildContextQuery(snap)
	assert.Equal(t, "Auth Plan", query)
}

func TestBuildContextQuery_WithBothTaskAndPlan(t *testing.T) {
	snap := SessionSnapshot{
		ActiveTask: &TaskInfo{Title: "Task Title"},
		ActivePlan: &PlanInfo{Title: "Plan Title"},
	}
	query := buildContextQuery(snap)
	assert.Equal(t, "Task Title Plan Title", query)
}

func TestBuildContextQuery_FallbackToSummary(t *testing.T) {
	snap := SessionSnapshot{
		Summary: "This is the session summary",
	}
	query := buildContextQuery(snap)
	assert.Equal(t, "This is the session summary", query)
}

func TestBuildContextQuery_SummaryTruncation(t *testing.T) {
	longSummary := ""
	for i := 0; i < 150; i++ {
		longSummary += "x"
	}
	snap := SessionSnapshot{
		Summary: longSummary,
	}
	query := buildContextQuery(snap)
	assert.Equal(t, 100, len(query))
}

func TestBuildContextQuery_TaskPreferredOverSummary(t *testing.T) {
	snap := SessionSnapshot{
		ActiveTask: &TaskInfo{Title: "Task"},
		Summary:    "Summary that should be ignored",
	}
	query := buildContextQuery(snap)
	assert.Equal(t, "Task", query)
	assert.NotContains(t, query, "Summary")
}

// Tests for buildContextSummaryFromSnapshot helper

func TestBuildContextSummaryFromSnapshot_Empty(t *testing.T) {
	snap := SessionSnapshot{}
	summary := buildContextSummaryFromSnapshot(snap)
	assert.Empty(t, summary)
}

func TestBuildContextSummaryFromSnapshot_WithActiveTask(t *testing.T) {
	snap := SessionSnapshot{
		ActiveTask: &TaskInfo{
			Title:       "Task Title",
			Description: "Task Description",
		},
	}
	summary := buildContextSummaryFromSnapshot(snap)
	assert.Contains(t, summary, "Task Title")
	assert.Contains(t, summary, "Task Description")
}

func TestBuildContextSummaryFromSnapshot_WithActivePlan(t *testing.T) {
	snap := SessionSnapshot{
		ActivePlan: &PlanInfo{Title: "Plan Title"},
	}
	summary := buildContextSummaryFromSnapshot(snap)
	assert.Equal(t, "Plan Title", summary)
}

func TestBuildContextSummaryFromSnapshot_WithPendingTodos(t *testing.T) {
	snap := SessionSnapshot{
		PendingTodos: []TaskInfo{
			{Title: "Todo 1"},
			{Title: "Todo 2"},
			{Title: "Todo 3"},
		},
	}
	summary := buildContextSummaryFromSnapshot(snap)
	assert.Contains(t, summary, "Todo 1")
	assert.Contains(t, summary, "Todo 2")
	assert.Contains(t, summary, "Todo 3")
}

func TestBuildContextSummaryFromSnapshot_LimitsFiveTotos(t *testing.T) {
	snap := SessionSnapshot{
		PendingTodos: []TaskInfo{
			{Title: "Todo 1"},
			{Title: "Todo 2"},
			{Title: "Todo 3"},
			{Title: "Todo 4"},
			{Title: "Todo 5"},
			{Title: "Todo 6"},
			{Title: "Todo 7"},
		},
	}
	summary := buildContextSummaryFromSnapshot(snap)
	assert.Contains(t, summary, "Todo 5")
	assert.NotContains(t, summary, "Todo 6")
	assert.NotContains(t, summary, "Todo 7")
}

func TestBuildContextSummaryFromSnapshot_FallbackToSummary(t *testing.T) {
	snap := SessionSnapshot{
		Summary: "This is the session summary",
	}
	summary := buildContextSummaryFromSnapshot(snap)
	assert.Equal(t, "This is the session summary", summary)
}

func TestBuildContextSummaryFromSnapshot_SummaryTruncation(t *testing.T) {
	longSummary := ""
	for i := 0; i < 300; i++ {
		longSummary += "x"
	}
	snap := SessionSnapshot{
		Summary: longSummary,
	}
	summary := buildContextSummaryFromSnapshot(snap)
	assert.Equal(t, 200, len(summary))
}

func TestBuildContextSummaryFromSnapshot_CombinesAllParts(t *testing.T) {
	snap := SessionSnapshot{
		ActiveTask: &TaskInfo{Title: "Active Task"},
		ActivePlan: &PlanInfo{Title: "Active Plan"},
		PendingTodos: []TaskInfo{
			{Title: "Todo 1"},
		},
	}
	summary := buildContextSummaryFromSnapshot(snap)
	assert.Contains(t, summary, "Active Task")
	assert.Contains(t, summary, "Active Plan")
	assert.Contains(t, summary, "Todo 1")
	assert.Contains(t, summary, ". ") // separator
}

// Tests for getSkillsReference helper

func TestGetSkillsReference_NotEmpty(t *testing.T) {
	ref := getSkillsReference()
	assert.NotEmpty(t, ref)
}

func TestGetSkillsReference_ContainsSkillSections(t *testing.T) {
	ref := getSkillsReference()
	assert.Contains(t, ref, "Files & Search")
	assert.Contains(t, ref, "Code Intelligence")
	assert.Contains(t, ref, "Testing & CI")
	assert.Contains(t, ref, "Tasks & Memory")
}

func TestGetSkillsReference_ContainsCommonSkills(t *testing.T) {
	ref := getSkillsReference()
	assert.Contains(t, ref, "code/context_ripgrep")
	assert.Contains(t, ref, "code/semantic_search")
	assert.Contains(t, ref, "test/run")
	assert.Contains(t, ref, "todo/manage")
}

func TestGetSkillsReference_ContainsCLIShortcuts(t *testing.T) {
	ref := getSkillsReference()
	assert.Contains(t, ref, "CLI Shortcuts")
	assert.Contains(t, ref, "foxctl todo")
	assert.Contains(t, ref, "foxctl ci")
	assert.Contains(t, ref, "foxctl memory")
}

// Edge case tests

func TestInput_RunSemanticSearchDefaults(t *testing.T) {
	in := Input{}

	// When RunSemanticSearch is nil, default to true
	runSearch := true
	if in.RunSemanticSearch != nil {
		runSearch = *in.RunSemanticSearch
	}
	assert.True(t, runSearch)
}

func TestInput_RunSemanticSearchExplicitFalse(t *testing.T) {
	runSearch := false
	in := Input{RunSemanticSearch: &runSearch}

	result := true
	if in.RunSemanticSearch != nil {
		result = *in.RunSemanticSearch
	}
	assert.False(t, result)
}

func TestSessionSnapshot_EmptySlices(t *testing.T) {
	snap := SessionSnapshot{
		PendingTodos: []TaskInfo{},
		Decisions:    []string{},
		Insights:     []string{},
	}

	assert.NotNil(t, snap.PendingTodos)
	assert.NotNil(t, snap.Decisions)
	assert.NotNil(t, snap.Insights)
	assert.Len(t, snap.PendingTodos, 0)
	assert.Len(t, snap.Decisions, 0)
	assert.Len(t, snap.Insights, 0)
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := Output{
		HookOutput: HookOutput{
			Decision: "approve",
			Reason:   "Restored from snapshot",
			Context:  "## Context\n...",
			Env:      map[string]string{"KEY": "value"},
		},
		SnapshotID:    "snap-full",
		SnapshotAge:   "10m",
		ItemsRestored: 5,
		KeyQuestions:  []string{"q1", "q2"},
		SearchResults: []SemanticSearchResult{
			{Question: "q1", Results: []string{"r1", "r2"}},
		},
		RelevantRecords: []memorycore.Record{
			{Kind: memorycore.KindSemanticFact, Summary: "memory1"},
		},
		Anchor: &AnchorInfo{
			AnchorID:        "anchor-full",
			MainPrompt:      "Test prompt",
			CompactionCount: 2,
			Learnings:       []string{"l1"},
		},
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.HookOutput.Decision, decoded.HookOutput.Decision)
	assert.Equal(t, out.HookOutput.Env["KEY"], decoded.HookOutput.Env["KEY"])
	assert.Equal(t, out.SnapshotID, decoded.SnapshotID)
	assert.Equal(t, out.ItemsRestored, decoded.ItemsRestored)
	assert.Equal(t, out.KeyQuestions, decoded.KeyQuestions)
	assert.Len(t, decoded.SearchResults, 1)
	assert.Len(t, decoded.RelevantRecords, 1)
	assert.NotNil(t, decoded.Anchor)
	assert.Equal(t, "anchor-full", decoded.Anchor.AnchorID)
}

func TestTaskInfo_EmptyGotchas(t *testing.T) {
	task := TaskInfo{
		ID:     "task-1",
		Title:  "Test task",
		Status: "pending",
	}

	assert.Empty(t, task.Gotchas)
	assert.Empty(t, task.Notes)
}

func TestPlanInfo_EmptySections(t *testing.T) {
	plan := PlanInfo{
		FilePath: "/path/to/plan.md",
		Title:    "Test Plan",
	}

	assert.Nil(t, plan.Sections)
	assert.Zero(t, plan.LinkedTasks)
}

func TestSimilarContextWindow_ZeroWindowIndex(t *testing.T) {
	window := SimilarContextWindow{
		WindowID:    "win-1",
		WindowIndex: 0, // First window in session
	}

	assert.Equal(t, 0, window.WindowIndex)
}

func TestSimilarSession_ZeroSimilarity(t *testing.T) {
	session := SimilarSession{
		SessionID:  "sess-1",
		Similarity: 0.0,
	}

	assert.Equal(t, 0.0, session.Similarity)
}

func TestFormatAge_EdgeCases(t *testing.T) {
	// Exactly 1 minute
	t1MinuteExact := time.Now().Add(-1 * time.Minute)
	result := formatAge(t1MinuteExact)
	assert.Equal(t, "1m", result)

	// Exactly 1 hour
	t1HourExact := time.Now().Add(-1 * time.Hour)
	result = formatAge(t1HourExact)
	assert.Equal(t, "1h", result)

	// Exactly 24 hours
	t24HoursExact := time.Now().Add(-24 * time.Hour)
	result = formatAge(t24HoursExact)
	assert.Equal(t, "1d", result)
}
