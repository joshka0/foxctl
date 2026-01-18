package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "session/restore", command)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	runSearch := true
	in := Input{
		Trigger:             "compact",
		Workspace:           "/path/to/workspace",
		SessionID:           "sess-123",
		ConversationSummary: "Working on test coverage",
		RunSemanticSearch:   &runSearch,
		MaxSearchResults:    5,
		CheckPending:        true,
	}

	assert.Equal(t, "compact", in.Trigger)
	assert.Equal(t, "/path/to/workspace", in.Workspace)
	assert.Equal(t, "sess-123", in.SessionID)
	assert.Equal(t, "Working on test coverage", in.ConversationSummary)
	assert.NotNil(t, in.RunSemanticSearch)
	assert.True(t, *in.RunSemanticSearch)
	assert.Equal(t, 5, in.MaxSearchResults)
	assert.True(t, in.CheckPending)
}

func TestInput_JSONSerialization(t *testing.T) {
	runSearch := false
	in := Input{
		Trigger:           "resume",
		Workspace:         "/ws",
		MaxSearchResults:  3,
		RunSemanticSearch: &runSearch,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Trigger, decoded.Trigger)
	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.MaxSearchResults, decoded.MaxSearchResults)
	assert.NotNil(t, decoded.RunSemanticSearch)
	assert.Equal(t, *in.RunSemanticSearch, *decoded.RunSemanticSearch)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.Trigger)
	assert.Empty(t, in.Workspace)
	assert.Empty(t, in.SessionID)
	assert.Empty(t, in.ConversationSummary)
	assert.Nil(t, in.RunSemanticSearch)
	assert.Zero(t, in.MaxSearchResults)
	assert.False(t, in.CheckPending)
}

func TestInput_TriggerValues(t *testing.T) {
	triggers := []string{"compact", "resume", "startup"}

	for _, trigger := range triggers {
		in := Input{Trigger: trigger}
		assert.Equal(t, trigger, in.Trigger)
	}
}

// Tests for SessionSnapshot structure

func TestSessionSnapshot_AllFields(t *testing.T) {
	now := time.Now().UTC()
	snap := SessionSnapshot{
		SnapshotID: "snap-123",
		SessionID:  "sess-456",
		Trigger:    "compact",
		Workspace:  "/workspace",
		Timestamp:  now,
		ActiveTask: &TaskInfo{ID: "task-1", Title: "Test task"},
		ActivePlan: &PlanInfo{FilePath: "/path/to/plan.md", Title: "Test Plan"},
		PendingTodos: []TaskInfo{
			{ID: "todo-1", Title: "Todo 1", Status: "pending"},
			{ID: "todo-2", Title: "Todo 2", Status: "in_progress"},
		},
		Decisions: []string{"decision1", "decision2"},
		Insights:  []string{"insight1"},
		Summary:   "Test session summary",
		Metadata:  map[string]string{"key": "value"},
	}

	assert.Equal(t, "snap-123", snap.SnapshotID)
	assert.Equal(t, "sess-456", snap.SessionID)
	assert.Equal(t, "compact", snap.Trigger)
	assert.Equal(t, "/workspace", snap.Workspace)
	assert.Equal(t, now, snap.Timestamp)
	assert.NotNil(t, snap.ActiveTask)
	assert.NotNil(t, snap.ActivePlan)
	assert.Len(t, snap.PendingTodos, 2)
	assert.Len(t, snap.Decisions, 2)
	assert.Len(t, snap.Insights, 1)
	assert.Equal(t, "Test session summary", snap.Summary)
	assert.Equal(t, "value", snap.Metadata["key"])
}

func TestSessionSnapshot_JSONSerialization(t *testing.T) {
	snap := SessionSnapshot{
		SnapshotID: "snap-test",
		Trigger:    "resume",
		Workspace:  "/ws",
		Timestamp:  time.Now().UTC().Truncate(time.Second),
	}

	data, err := json.Marshal(snap)
	assert.NoError(t, err)

	var decoded SessionSnapshot
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, snap.SnapshotID, decoded.SnapshotID)
	assert.Equal(t, snap.Trigger, decoded.Trigger)
	assert.Equal(t, snap.Workspace, decoded.Workspace)
}

func TestSessionSnapshot_EmptyFields(t *testing.T) {
	snap := SessionSnapshot{}

	assert.Empty(t, snap.SnapshotID)
	assert.Empty(t, snap.SessionID)
	assert.Nil(t, snap.ActiveTask)
	assert.Nil(t, snap.ActivePlan)
	assert.Nil(t, snap.PendingTodos)
}

// Tests for PlanInfo structure

func TestPlanInfo_AllFields(t *testing.T) {
	plan := PlanInfo{
		FilePath:    "/path/to/plan.md",
		FileName:    "plan.md",
		Title:       "Implementation Plan",
		ContentHash: "abc123",
		Sections:    []string{"Overview", "Tasks", "Timeline"},
		LinkedTasks: 5,
		ModTime:     "2026-01-15T10:00:00Z",
	}

	assert.Equal(t, "/path/to/plan.md", plan.FilePath)
	assert.Equal(t, "plan.md", plan.FileName)
	assert.Equal(t, "Implementation Plan", plan.Title)
	assert.Equal(t, "abc123", plan.ContentHash)
	assert.Len(t, plan.Sections, 3)
	assert.Equal(t, 5, plan.LinkedTasks)
	assert.Equal(t, "2026-01-15T10:00:00Z", plan.ModTime)
}

func TestPlanInfo_JSONSerialization(t *testing.T) {
	plan := PlanInfo{
		FilePath: "/plan.md",
		Title:    "Test Plan",
	}

	data, err := json.Marshal(plan)
	assert.NoError(t, err)

	var decoded PlanInfo
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, plan.FilePath, decoded.FilePath)
	assert.Equal(t, plan.Title, decoded.Title)
}

// Tests for TaskInfo structure

func TestTaskInfo_AllFields(t *testing.T) {
	task := TaskInfo{
		ID:          "task-123",
		Title:       "Implement feature X",
		Description: "Feature X needs to do Y and Z",
		Status:      "in_progress",
		Notes:       "Some notes here",
		Gotchas:     "Watch out for edge case A",
	}

	assert.Equal(t, "task-123", task.ID)
	assert.Equal(t, "Implement feature X", task.Title)
	assert.Equal(t, "Feature X needs to do Y and Z", task.Description)
	assert.Equal(t, "in_progress", task.Status)
	assert.Equal(t, "Some notes here", task.Notes)
	assert.Equal(t, "Watch out for edge case A", task.Gotchas)
}

func TestTaskInfo_StatusValues(t *testing.T) {
	statuses := []string{"pending", "in_progress", "completed", "blocked"}

	for _, status := range statuses {
		task := TaskInfo{Status: status}
		assert.Equal(t, status, task.Status)
	}
}

// Tests for HookOutput structure

func TestHookOutput_AllFields(t *testing.T) {
	hook := HookOutput{
		Decision: "approve",
		Reason:   "Restored session snapshot",
		Context:  "## Session Context\n...",
		Env:      map[string]string{"VAR1": "value1", "VAR2": "value2"},
	}

	assert.Equal(t, "approve", hook.Decision)
	assert.Equal(t, "Restored session snapshot", hook.Reason)
	assert.Contains(t, hook.Context, "Session Context")
	assert.Equal(t, "value1", hook.Env["VAR1"])
}

func TestHookOutput_DecisionValues(t *testing.T) {
	decisions := []string{"approve", "block", "none"}

	for _, decision := range decisions {
		hook := HookOutput{Decision: decision}
		assert.Equal(t, decision, hook.Decision)
	}
}

// Tests for SemanticSearchResult structure

func TestSemanticSearchResult_AllFields(t *testing.T) {
	result := SemanticSearchResult{
		Question: "How does authentication work?",
		Results:  []string{"├── internal/auth/handler.go:25", "├── internal/auth/middleware.go:10"},
	}

	assert.Equal(t, "How does authentication work?", result.Question)
	assert.Len(t, result.Results, 2)
	assert.Contains(t, result.Results[0], "auth/handler.go")
}

func TestSemanticSearchResult_EmptyResults(t *testing.T) {
	result := SemanticSearchResult{
		Question: "No results query",
		Results:  []string{},
	}

	assert.NotNil(t, result.Results)
	assert.Len(t, result.Results, 0)
}

// Tests for SimilarContextWindow structure

func TestSimilarContextWindow_AllFields(t *testing.T) {
	now := time.Now().UTC()
	window := SimilarContextWindow{
		WindowID:    "win-123",
		SessionID:   "sess-456",
		WindowIndex: 2,
		Summary:     "Working on auth implementation",
		Trigger:     "compact",
		Similarity:  0.85,
		StartedAt:   now,
	}

	assert.Equal(t, "win-123", window.WindowID)
	assert.Equal(t, "sess-456", window.SessionID)
	assert.Equal(t, 2, window.WindowIndex)
	assert.Equal(t, "Working on auth implementation", window.Summary)
	assert.Equal(t, "compact", window.Trigger)
	assert.Equal(t, 0.85, window.Similarity)
	assert.Equal(t, now, window.StartedAt)
}

// Tests for SimilarSession structure

func TestSimilarSession_AllFields(t *testing.T) {
	start := time.Now().UTC().Add(-1 * time.Hour)
	end := time.Now().UTC()
	session := SimilarSession{
		SessionID:    "sess-789",
		Summary:      "Session summary",
		Accomplished: "Completed tasks A and B",
		Similarity:   0.72,
		StartedAt:    start,
		EndedAt:      end,
	}

	assert.Equal(t, "sess-789", session.SessionID)
	assert.Equal(t, "Session summary", session.Summary)
	assert.Equal(t, "Completed tasks A and B", session.Accomplished)
	assert.Equal(t, 0.72, session.Similarity)
	assert.Equal(t, start, session.StartedAt)
	assert.Equal(t, end, session.EndedAt)
}

// Tests for MemoryResult structure

func TestMemoryResult_AllFields(t *testing.T) {
	mem := MemoryResult{
		Type:    "gotcha",
		Summary: "Don't forget to check nil pointers",
	}

	assert.Equal(t, "gotcha", mem.Type)
	assert.Equal(t, "Don't forget to check nil pointers", mem.Summary)
}

func TestMemoryResult_TypeValues(t *testing.T) {
	types := []string{"gotcha", "decision", "user_pref", "time_sink", "pattern"}

	for _, memType := range types {
		mem := MemoryResult{Type: memType}
		assert.Equal(t, memType, mem.Type)
	}
}

// Tests for AnchorInfo structure

func TestAnchorInfo_AllFields(t *testing.T) {
	anchor := AnchorInfo{
		AnchorID:        "anchor-123",
		MainPrompt:      "Implement comprehensive test coverage",
		CompactionCount: 3,
		Learnings:       []string{"learning1", "learning2"},
	}

	assert.Equal(t, "anchor-123", anchor.AnchorID)
	assert.Equal(t, "Implement comprehensive test coverage", anchor.MainPrompt)
	assert.Equal(t, 3, anchor.CompactionCount)
	assert.Len(t, anchor.Learnings, 2)
}

func TestAnchorInfo_EmptyLearnings(t *testing.T) {
	anchor := AnchorInfo{
		AnchorID:   "anchor-456",
		MainPrompt: "Test prompt",
	}

	assert.Nil(t, anchor.Learnings)
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	out := Output{
		HookOutput: HookOutput{
			Decision: "approve",
			Reason:   "test reason",
			Context:  "test context",
		},
		SnapshotID:    "snap-123",
		SnapshotAge:   "5m",
		ItemsRestored: 10,
		KeyQuestions:  []string{"question1", "question2"},
		SearchResults: []SemanticSearchResult{
			{Question: "q1", Results: []string{"result1"}},
		},
		RelevantMemories: []MemoryResult{
			{Type: "gotcha", Summary: "gotcha1"},
		},
		Anchor: &AnchorInfo{AnchorID: "anchor-1"},
	}

	assert.Equal(t, "approve", out.HookOutput.Decision)
	assert.Equal(t, "snap-123", out.SnapshotID)
	assert.Equal(t, "5m", out.SnapshotAge)
	assert.Equal(t, 10, out.ItemsRestored)
	assert.Len(t, out.KeyQuestions, 2)
	assert.Len(t, out.SearchResults, 1)
	assert.Len(t, out.RelevantMemories, 1)
	assert.NotNil(t, out.Anchor)
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := Output{
		HookOutput: HookOutput{
			Decision: "approve",
		},
		SnapshotID:    "snap-test",
		ItemsRestored: 5,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.HookOutput.Decision, decoded.HookOutput.Decision)
	assert.Equal(t, out.SnapshotID, decoded.SnapshotID)
	assert.Equal(t, out.ItemsRestored, decoded.ItemsRestored)
}

func TestOutput_EmptyFields(t *testing.T) {
	out := Output{}

	assert.Empty(t, out.HookOutput.Decision)
	assert.Empty(t, out.SnapshotID)
	assert.Zero(t, out.ItemsRestored)
	assert.Nil(t, out.KeyQuestions)
	assert.Nil(t, out.SearchResults)
	assert.Nil(t, out.Anchor)
}

// Tests for formatAge helper

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
	assert.Contains(t, ref, "agentctl todo")
	assert.Contains(t, ref, "agentctl ci")
	assert.Contains(t, ref, "agentctl memory")
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
		RelevantMemories: []MemoryResult{
			{Type: "gotcha", Summary: "memory1"},
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
	assert.Len(t, decoded.RelevantMemories, 1)
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
