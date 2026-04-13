package taskhistory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/transcriptpipeline"
	tphistory "github.com/jkatigb/agentctl/internal/transcriptpipeline/history"
)

type fakeGitRunner struct {
	commits map[string][]GitCommit
}

func (f fakeGitRunner) FileHistory(_ context.Context, _ string, filePath string, _ int) ([]GitCommit, error) {
	return append([]GitCommit(nil), f.commits[filePath]...), nil
}

func TestCollectorCollectBuildsPack(t *testing.T) {
	ctx := context.Background()
	workspacePath := t.TempDir()
	storageRoot := t.TempDir()
	wsID := workspace.CanonicalID(workspacePath)

	wsStore := contextplane.NewWorkspaceStore(workspacePath)
	if _, err := wsStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID:  wsID,
		Objective:    "Compact Handoff Pattern",
		Phase:        "design",
		RelevantRefs: []string{"path:internal/contextplane/store.go"},
		UpdatedAt:    time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	if _, err := wsStore.SaveHandoff(contextplane.Handoff{
		TaskID:       "T-1",
		Phase:        "design",
		Outcome:      "partial",
		Summary:      "Collected compact handoff evidence.",
		FilesTouched: []string{"internal/contextplane/store.go"},
		EvidenceRefs: []string{"path:internal/contextplane/store.go"},
		CreatedAt:    time.Date(2026, 3, 14, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveHandoff: %v", err)
	}

	taskDB, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	defer taskDB.Close()
	task, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "T-1",
		WorkspaceID: wsID,
		Title:       "Compact Handoff Pattern",
		Description: "Trace the compact handoff implementation.",
		ScopePath:   "internal/contextplane/store.go",
		Status:      taskstore.StatusInProgress,
		SessionID:   "sess-1",
	})
	if err != nil {
		t.Fatalf("Add task: %v", err)
	}

	sessionDB, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open sessions: %v", err)
	}
	defer sessionDB.Close()
	if _, err := sessionDB.Save(ctx, storage.Session{
		ID:            "sess-1",
		WorkspaceID:   wsID,
		WorkspacePath: workspacePath,
		ProjectName:   "agentctl",
		Summary:       "Worked on the compact handoff flow.",
		Decisions:     []string{"Prefer a bounded handoff envelope."},
		Gotchas:       []string{"Do not inline large restore context."},
		KeyFiles:      []string{"internal/contextplane/store.go"},
		StartedAt:     time.Date(2026, 3, 14, 0, 30, 0, 0, time.UTC),
		EndedAt:       time.Date(2026, 3, 14, 1, 30, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Save session: %v", err)
	}

	repo, err := repoindex.Open(ctx, storageRoot, workspacePath)
	if err != nil {
		t.Fatalf("open repoindex: %v", err)
	}
	defer repo.Close()
	repoKey := repo.RepoKey()
	if err := repo.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:      repoindex.FileID(repoKey, "internal/contextplane", "internal/contextplane/store.go"),
			Kind:    repoindex.NodeFile,
			Pkg:     "internal/contextplane",
			File:    "internal/contextplane/store.go",
			Name:    "store.go",
			Summary: "Workspace ACA store implementation.",
		},
		{
			ID:      repoindex.NamespacedID(repoKey, "concept:compact-handoff-pattern"),
			Kind:    repoindex.NodeConcept,
			Pkg:     "internal/contextplane",
			File:    "internal/contextplane/store.go",
			Name:    "Compact Handoff Pattern",
			Summary: "Compact handoff pattern for ACA.",
		},
	}, nil); err != nil {
		t.Fatalf("ReplaceAll repoindex: %v", err)
	}

	vaultRoot := retrievalFixtureVaultRoot(t)
	index, err := obsidianindex.Open(ctx, storageRoot, vaultRoot)
	if err != nil {
		t.Fatalf("open obsidian index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(ctx, vaultRoot); err != nil {
		t.Fatalf("rebuild obsidian index: %v", err)
	}

	collector := Collector{
		WorkspaceStore: wsStore,
		TaskStore:      taskDB,
		SessionStore:   sessionDB,
		RepoStore:      repo,
		VaultIndex:     index,
		GitRunner: fakeGitRunner{
			commits: map[string][]GitCommit{
				"internal/contextplane/store.go": {{
					Hash:    "abc123",
					Date:    "2026-03-14",
					Subject: "refine compact handoff storage",
				}},
			},
		},
	}

	pack, err := collector.Collect(ctx, Options{
		WorkspacePath:  workspacePath,
		WorkspaceID:    wsID,
		TaskID:         task.ID,
		SessionLimit:   3,
		HandoffLimit:   5,
		FileLimit:      5,
		GitCommitLimit: 3,
		AnchorLimit:    5,
		NoteLimit:      3,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if pack.Task.ID != "T-1" {
		t.Fatalf("task id=%q", pack.Task.ID)
	}
	if len(pack.Handoffs) != 1 {
		t.Fatalf("handoffs=%d", len(pack.Handoffs))
	}
	if len(pack.FilesTouched) == 0 || pack.FilesTouched[0] != "internal/contextplane/store.go" {
		t.Fatalf("files=%v", pack.FilesTouched)
	}
	if len(pack.Sessions) == 0 || pack.Sessions[0].ID != "sess-1" {
		t.Fatalf("sessions=%v", pack.Sessions)
	}
	if len(pack.GitHistory) == 0 || pack.GitHistory[0].Path != "internal/contextplane/store.go" {
		t.Fatalf("git history=%v", pack.GitHistory)
	}
	if len(pack.RepoAnchors) == 0 {
		t.Fatalf("repo anchors empty")
	}
	if len(pack.DAGAnchors) == 0 {
		t.Fatalf("dag anchors empty")
	}
	if len(pack.ACANotes) == 0 {
		t.Fatalf("aca notes empty")
	}
	if pack.Summary == "" {
		t.Fatalf("summary empty")
	}
}

func TestCollectorCollectIncludesTranscriptHistory(t *testing.T) {
	ctx := context.Background()
	workspacePath := t.TempDir()
	storageRoot := t.TempDir()
	wsID := workspace.CanonicalID(workspacePath)

	wsStore := contextplane.NewWorkspaceStore(workspacePath)
	if _, err := wsStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: wsID,
		Objective:   "Use transcript continuity",
		Phase:       "implement",
		UpdatedAt:   time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	taskDB, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	defer taskDB.Close()
	task, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "T-history",
		WorkspaceID: wsID,
		Title:       "Use transcript continuity",
		ScopePath:   "internal/contextplane/taskhistory/taskhistory.go",
		Status:      taskstore.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("Add task: %v", err)
	}

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	answers := []tphistory.HistoryAnswer{
		{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Use transcript continuity as the agent handoff layer.", Confidence: 0.8},
		{QuestionID: tphistory.HistoryQuestionActiveDirections, Answer: "Inject history_pack.agent_brief into task continuity.", Confidence: 0.74},
		{QuestionID: tphistory.HistoryQuestionNextStep, Answer: "Use the transcript handoff in task-history-summary.", Confidence: 0.72},
	}
	records := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
		ConversationID: "conv-task-history",
		SessionIDs:     []string{"sess-history"},
	}, []transcriptpipeline.DecisionInsight{
		{
			Kind:                 transcriptpipeline.InsightKindDirection,
			Summary:              "Use named_memory as the durable sink for transcript-derived memories.",
			Status:               transcriptpipeline.InsightStatusAccepted,
			Confidence:           0.8,
			EvidenceFrameIndices: []int{3},
		},
	}, []transcriptpipeline.NotableInsight{
		{
			Kind:         transcriptpipeline.NotableInsightGotcha,
			Headline:     "We initially overfit on doctrine extraction.",
			WhyItMatters: "The next agent should avoid over-optimizing the doctrine lane.",
			StartFrame:   1,
			EndFrame:     2,
		},
		{
			Kind:         transcriptpipeline.NotableInsightSurprise,
			Headline:     "Consensus-backed hybrid runtime findings transferred well.",
			WhyItMatters: "The grouped path surfaced better support than expected.",
			StartFrame:   4,
			EndFrame:     4,
		},
		{
			Kind:         transcriptpipeline.NotableInsightEpisodic,
			Headline:     "We switched from transcript summaries to typed history records.",
			WhyItMatters: "That transition made the continuity surface much more usable.",
			StartFrame:   5,
			EndFrame:     5,
		},
	}, answers)
	if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, workspacePath, "sess-history", "sess-history", records, nil); err != nil {
		t.Fatalf("PersistHistoryRecords: %v", err)
	}
	unrelated := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
		ConversationID: "conv-other",
		SessionIDs:     []string{"sess-other"},
	}, nil, nil, []tphistory.HistoryAnswer{
		{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Close the release gate verdict for the mobile app.", Confidence: 0.8},
		{QuestionID: tphistory.HistoryQuestionNextStep, Answer: "Finish the release checklist.", Confidence: 0.72},
	})
	if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, workspacePath, "sess-other", "sess-other", unrelated, nil); err != nil {
		t.Fatalf("PersistHistoryRecords(unrelated): %v", err)
	}

	collector := Collector{
		WorkspaceStore: wsStore,
		TaskStore:      taskDB,
		MemoryStore:    memStore,
	}
	pack, err := collector.Collect(ctx, Options{
		WorkspacePath: workspacePath,
		WorkspaceID:   wsID,
		TaskID:        task.ID,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if pack.Transcript == nil {
		t.Fatal("expected transcript continuity in pack")
	}
	if !strings.Contains(pack.Transcript.AgentBrief, "Inject history_pack.agent_brief into task continuity.") {
		t.Fatalf("agent_brief=%q", pack.Transcript.AgentBrief)
	}
	if len(pack.Transcript.ContinueWith) == 0 || pack.Transcript.ContinueWith[0] != "Inject history_pack.agent_brief into task continuity." {
		t.Fatalf("continue_with=%v", pack.Transcript.ContinueWith)
	}
	if !strings.Contains(pack.Transcript.AgentBrief, "Watch out for: We initially overfit on doctrine extraction.") {
		t.Fatalf("agent_brief=%q missing watch-out", pack.Transcript.AgentBrief)
	}
	if !strings.Contains(pack.Transcript.AgentBrief, "Learned: Use named_memory as the durable sink for transcript-derived memories.") {
		t.Fatalf("agent_brief=%q missing learning", pack.Transcript.AgentBrief)
	}
	if strings.TrimSpace(pack.Transcript.RetrievedBrief) == "" {
		t.Fatalf("expected retrieved brief in %+v", pack.Transcript)
	}
	if len(pack.Transcript.RetrievedHighlights) == 0 {
		t.Fatalf("expected retrieved highlights in %+v", pack.Transcript)
	}
	if strings.Contains(pack.Transcript.AgentBrief, "Finish the release checklist.") {
		t.Fatalf("agent_brief=%q included unrelated transcript history", pack.Transcript.AgentBrief)
	}
	if len(pack.Transcript.WatchOutFor) == 0 || len(pack.Transcript.RecentLearnings) == 0 || len(pack.Transcript.RecentSurprises) == 0 {
		t.Fatalf("transcript support=%+v", pack.Transcript)
	}
	if !strings.Contains(pack.Summary, "transcript:") {
		t.Fatalf("summary=%q missing transcript continuity", pack.Summary)
	}
}

func TestCollectorCollectFallsBackToLatestTranscriptHistoryWithinScope(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mainRepo := filepath.Join(root, "praze")
	worktree := filepath.Join(root, "praze-v2-compare")
	storageRoot := t.TempDir()
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	content := []byte("gitdir: " + filepath.Join(mainRepo, ".git", "worktrees", "praze-v2-compare") + "\n")
	if err := os.WriteFile(gitFile, content, 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	wsID := workspace.CanonicalID(worktree)
	wsStore := contextplane.NewWorkspaceStore(worktree)
	if _, err := wsStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: wsID,
		Objective:   "Validate compare worktree continuity",
		Phase:       "implement",
		UpdatedAt:   time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	taskDB, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	defer taskDB.Close()
	task, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "T-fallback",
		WorkspaceID: wsID,
		Title:       "Edit unrelated backend file",
		ScopePath:   "apps/praze-api/lib/praze/circuit_breaker.ex",
		Status:      taskstore.StatusPending,
	})
	if err != nil {
		t.Fatalf("Add task: %v", err)
	}

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()
	records := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
		ConversationID: "conv-family-fallback",
		SessionIDs:     []string{"sess-family-fallback"},
	}, nil, nil, []tphistory.HistoryAnswer{
		{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Map the rebuilt frontend architecture to the target surfaces.", Confidence: 0.8},
		{QuestionID: tphistory.HistoryQuestionActiveDirections, Answer: "Compare the rebuilt surfaces against the target destinations.", Confidence: 0.74},
		{QuestionID: tphistory.HistoryQuestionOpenQuestions, Answer: "Do the rebuilt destinations preserve the intended user journey?", Confidence: 0.7},
		{QuestionID: tphistory.HistoryQuestionMisunderstandings, Answer: "We initially treated scaffolds as finished destinations.", Confidence: 0.82},
		{QuestionID: tphistory.HistoryQuestionGotchas, Answer: "Path-heavy guidance can bury the actual architectural gap.", Confidence: 0.78},
		{QuestionID: tphistory.HistoryQuestionRegressions, Answer: "Surface reviews can regress into implementation chatter.", Confidence: 0.72},
		{QuestionID: tphistory.HistoryQuestionRecurringMistakes, Answer: "Treating scaffolds as finished destinations.", Confidence: 0.7},
		{QuestionID: tphistory.HistoryQuestionSurprises, Answer: "The rebuilt data boundaries were better than expected.", Confidence: 0.8},
		{QuestionID: tphistory.HistoryQuestionEpisodicHistory, Answer: "We mapped the rebuilt frontend to the Paper targets.", Confidence: 0.66},
		{QuestionID: tphistory.HistoryQuestionNextStep, Answer: "Carry the structural gap forward into the next compare pass.", Confidence: 0.72},
		{QuestionID: tphistory.HistoryQuestionAcceptedLearnings, Answer: "Most surfaces are still first-pass renderers over rebuilt endpoints rather than polished destination experiences.", Confidence: 0.8},
	})
	if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, mainRepo, "sess-family-fallback", "sess-family-fallback", records, nil); err != nil {
		t.Fatalf("PersistHistoryRecords: %v", err)
	}

	collector := Collector{
		WorkspaceStore: wsStore,
		TaskStore:      taskDB,
		MemoryStore:    memStore,
	}
	pack, err := collector.Collect(ctx, Options{
		WorkspacePath:          worktree,
		WorkspaceID:            wsID,
		TaskID:                 task.ID,
		TranscriptHistoryScope: TranscriptHistoryScopeFamily,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if pack.Transcript == nil {
		t.Fatal("expected transcript continuity fallback in pack")
	}
	if !strings.Contains(pack.Transcript.AgentBrief, "Most surfaces are still first-pass renderers") {
		t.Fatalf("agent_brief=%q missing family fallback transcript history", pack.Transcript.AgentBrief)
	}
}

func TestCollectorCollectAggregatesRecurringMistakesAcrossOwners(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mainRepo := filepath.Join(root, "praze")
	worktree := filepath.Join(root, "praze-v2-compare")
	storageRoot := t.TempDir()
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	content := []byte("gitdir: " + filepath.Join(mainRepo, ".git", "worktrees", "praze-v2-compare") + "\n")
	if err := os.WriteFile(gitFile, content, 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	wsID := workspace.CanonicalID(worktree)
	wsStore := contextplane.NewWorkspaceStore(worktree)
	if _, err := wsStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: wsID,
		Objective:   "Avoid recurring transcript regressions",
		Phase:       "implement",
		UpdatedAt:   time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	taskDB, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	defer taskDB.Close()
	task, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "T-recurring",
		WorkspaceID: wsID,
		Title:       "Edit unrelated backend file",
		ScopePath:   "apps/praze-api/lib/praze/circuit_breaker.ex",
		Status:      taskstore.StatusPending,
	})
	if err != nil {
		t.Fatalf("Add task: %v", err)
	}

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()
	for _, owner := range []string{"sess-a", "sess-b"} {
		records := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
			ConversationID: "conv-" + owner,
			SessionIDs:     []string{owner},
		}, nil, nil, []tphistory.HistoryAnswer{
			{QuestionID: tphistory.HistoryQuestionRegressions, Answer: "Sandbox denied while checking git state.", Confidence: 0.72},
			{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Some task objective.", Confidence: 0.8},
		})
		if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, mainRepo, owner, owner, records, nil); err != nil {
			t.Fatalf("PersistHistoryRecords(%s): %v", owner, err)
		}
	}

	collector := Collector{
		WorkspaceStore: wsStore,
		TaskStore:      taskDB,
		MemoryStore:    memStore,
	}
	pack, err := collector.Collect(ctx, Options{
		WorkspacePath:          worktree,
		WorkspaceID:            wsID,
		TaskID:                 task.ID,
		TranscriptHistoryScope: TranscriptHistoryScopeFamily,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if pack.Transcript == nil {
		t.Fatal("expected transcript continuity in pack")
	}
	if len(pack.Transcript.RecurringMistakes) == 0 {
		t.Fatalf("expected recurring mistakes in %+v", pack.Transcript)
	}
	if !strings.Contains(strings.ToLower(strings.Join(pack.Transcript.RecurringMistakes, " | ")), "sandbox denied") {
		t.Fatalf("recurring mistakes=%v", pack.Transcript.RecurringMistakes)
	}
}

func TestCollectTranscriptRecurringLearningsAcrossOwners(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mainRepo := filepath.Join(root, "praze")
	worktree := filepath.Join(root, "praze-v2-compare")
	storageRoot := t.TempDir()
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	content := []byte("gitdir: " + filepath.Join(mainRepo, ".git", "worktrees", "praze-v2-compare") + "\n")
	if err := os.WriteFile(gitFile, content, 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	fixtures := map[string]string{
		"sess-a": "Non-presence hooks are real instead of client-only scaffolds. | Map target surfaces before polish.",
		"sess-b": "Non-presence hooks function as real backend components. | Guard unguarded components.",
	}
	for owner, learning := range fixtures {
		records := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
			ConversationID: "conv-" + owner,
			SessionIDs:     []string{owner},
		}, nil, nil, []tphistory.HistoryAnswer{
			{QuestionID: tphistory.HistoryQuestionAcceptedLearnings, Answer: learning, Confidence: 0.8},
			{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Some task objective.", Confidence: 0.8},
		})
		if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, mainRepo, owner, owner, records, nil); err != nil {
			t.Fatalf("PersistHistoryRecords(%s): %v", owner, err)
		}
	}

	collector := Collector{MemoryStore: memStore}
	got := collector.collectTranscriptRecurringLearnings(ctx, worktree, workspace.FamilyPath(worktree), TranscriptHistoryScopeFamily, 4, TranscriptHistoryDateRange{})
	if len(got) == 0 {
		t.Fatalf("expected recurring learnings, got %v", got)
	}
	if got[0] != "Non-presence hooks are real instead of client-only scaffolds." && got[0] != "Non-presence hooks function as real backend components." {
		t.Fatalf("recurring learnings=%v", got)
	}
	if containsString(got, "Map target surfaces before polish.") || containsString(got, "Guard unguarded components.") {
		t.Fatalf("recurring learnings=%v kept one-off learning", got)
	}
}

func TestSearchTranscriptHistoryAnswers_UsesRetrievalTextLexically(t *testing.T) {
	ctx := context.Background()
	workspacePath := t.TempDir()
	storageRoot := t.TempDir()

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	answers := []tphistory.HistoryAnswer{
		{
			QuestionID: tphistory.HistoryQuestionNextStep,
			Answer:     "Persist history records for retrieval.",
			Confidence: 0.72,
		},
	}
	records := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
		ConversationID: "conv-lexical",
		SessionIDs:     []string{"sess-lexical"},
	}, nil, nil, answers)
	if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, workspacePath, "sess-lexical", "sess-lexical", records, nil); err != nil {
		t.Fatalf("PersistHistoryRecords: %v", err)
	}

	collector := Collector{MemoryStore: memStore}
	got := collector.searchTranscriptHistoryAnswers(ctx, workspacePath, workspace.FamilyPath(workspacePath), "What should the next agent do?", TranscriptHistoryScopeAuto, 4)
	if len(got) != 1 {
		t.Fatalf("results=%d want 1", len(got))
	}
	if got[0].Type != "history_answer" {
		t.Fatalf("type=%q want history_answer", got[0].Type)
	}
}

func TestSearchTranscriptHistoryAnswers_ReturnsFullOwnerAnswerBundle(t *testing.T) {
	ctx := context.Background()
	workspacePath := t.TempDir()
	storageRoot := t.TempDir()

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	answers := []tphistory.HistoryAnswer{
		{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Map the rebuilt frontend architecture to the target surfaces.", Confidence: 0.8},
		{QuestionID: tphistory.HistoryQuestionActiveDirections, Answer: "Compare rebuilt surfaces to destination-quality targets.", Confidence: 0.74},
		{QuestionID: tphistory.HistoryQuestionOpenQuestions, Answer: "Which surfaces still feel scaffold-like?", Confidence: 0.7},
		{QuestionID: tphistory.HistoryQuestionMisunderstandings, Answer: "We initially treated scaffolds as finished destinations.", Confidence: 0.82},
		{QuestionID: tphistory.HistoryQuestionGotchas, Answer: "Path-heavy guidance can bury the actual architectural gap.", Confidence: 0.78},
		{QuestionID: tphistory.HistoryQuestionRegressions, Answer: "Surface reviews can regress into implementation chatter.", Confidence: 0.72},
		{QuestionID: tphistory.HistoryQuestionRecurringMistakes, Answer: "Treating scaffolds as finished destinations.", Confidence: 0.7},
		{QuestionID: tphistory.HistoryQuestionSurprises, Answer: "The rebuilt data boundaries were better than expected.", Confidence: 0.8},
		{QuestionID: tphistory.HistoryQuestionEpisodicHistory, Answer: "We mapped the rebuilt frontend to the Paper targets.", Confidence: 0.66},
		{QuestionID: tphistory.HistoryQuestionNextStep, Answer: "Carry the structural gap into the next compare pass.", Confidence: 0.72},
		{QuestionID: tphistory.HistoryQuestionAcceptedLearnings, Answer: "Most surfaces are still first-pass renderers over rebuilt endpoints rather than polished destination experiences.", Confidence: 0.8},
	}
	records := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
		ConversationID: "conv-owner-bundle",
		SessionIDs:     []string{"sess-owner-bundle"},
	}, nil, nil, answers)
	if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, workspacePath, "sess-owner-bundle", "sess-owner-bundle", records, nil); err != nil {
		t.Fatalf("PersistHistoryRecords: %v", err)
	}

	collector := Collector{MemoryStore: memStore}
	got := collector.searchTranscriptHistoryAnswers(ctx, workspacePath, workspace.FamilyPath(workspacePath), "architecture target surfaces", TranscriptHistoryScopeAuto, 4)
	if len(got) < len(answers) {
		t.Fatalf("results=%d want at least %d answers from owner bundle", len(got), len(answers))
	}
	foundLearning := false
	for _, entry := range got {
		answer, ok := historyAnswerFromMemoryEntry(entry)
		if !ok {
			continue
		}
		if answer.QuestionID == tphistory.HistoryQuestionAcceptedLearnings {
			foundLearning = true
			break
		}
	}
	if !foundLearning {
		t.Fatalf("results missing accepted learning answer: %+v", got)
	}
}

func TestSearchTranscriptHistoryAnswers_ScopeControlsWorkspaceVsFamily(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mainRepo := filepath.Join(root, "praze")
	worktree := filepath.Join(root, "praze-v2-compare")
	storageRoot := t.TempDir()
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	content := []byte("gitdir: " + filepath.Join(mainRepo, ".git", "worktrees", "praze-v2-compare") + "\n")
	if err := os.WriteFile(gitFile, content, 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	records := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
		ConversationID: "conv-family",
		SessionIDs:     []string{"sess-family"},
	}, nil, nil, []tphistory.HistoryAnswer{{
		QuestionID: tphistory.HistoryQuestionNextStep,
		Answer:     "Persist history records for retrieval.",
		Confidence: 0.72,
	}})
	if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, mainRepo, "sess-family", "sess-family", records, nil); err != nil {
		t.Fatalf("PersistHistoryRecords: %v", err)
	}

	collector := Collector{MemoryStore: memStore}
	familyPath := workspace.FamilyPath(worktree)

	workspaceScoped := collector.searchTranscriptHistoryAnswers(ctx, worktree, familyPath, "What should the next agent do?", TranscriptHistoryScopeWorkspace, 4)
	if len(workspaceScoped) != 0 {
		t.Fatalf("workspace scope results=%d want 0", len(workspaceScoped))
	}

	familyScoped := collector.searchTranscriptHistoryAnswers(ctx, worktree, familyPath, "What should the next agent do?", TranscriptHistoryScopeFamily, 4)
	if len(familyScoped) != 1 {
		t.Fatalf("family scope results=%d want 1", len(familyScoped))
	}

	autoScoped := collector.searchTranscriptHistoryAnswers(ctx, worktree, familyPath, "What should the next agent do?", TranscriptHistoryScopeAuto, 4)
	if len(autoScoped) != 1 {
		t.Fatalf("auto scope results=%d want 1", len(autoScoped))
	}
}

func TestDefaultTranscriptHistoryScope_FromEnv(t *testing.T) {
	t.Setenv("AGENTCTL_TRANSCRIPT_HISTORY_SCOPE", "family")
	if got := DefaultTranscriptHistoryScope(); got != TranscriptHistoryScopeFamily {
		t.Fatalf("DefaultTranscriptHistoryScope() = %q want %q", got, TranscriptHistoryScopeFamily)
	}

	t.Setenv("AGENTCTL_TRANSCRIPT_HISTORY_SCOPE", "invalid")
	if got := DefaultTranscriptHistoryScope(); got != TranscriptHistoryScopeAuto {
		t.Fatalf("DefaultTranscriptHistoryScope() invalid = %q want %q", got, TranscriptHistoryScopeAuto)
	}
}

func TestCollectorPrefersInRepoTaskWhenTaskIDOmitted(t *testing.T) {
	ctx := context.Background()
	workspacePath := t.TempDir()
	storageRoot := t.TempDir()
	wsID := workspace.CanonicalID(workspacePath)

	wsStore := contextplane.NewWorkspaceStore(workspacePath)
	if _, err := wsStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: wsID,
		Objective:   "Current repo work",
		Phase:       "implement",
		UpdatedAt:   time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	taskDB, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	defer taskDB.Close()

	if _, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "external-task",
		WorkspaceID: wsID,
		Title:       "Write /Users/joshka/.claude/plans/example.md",
		ScopePath:   "/Users/joshka/.claude/plans/example.md",
		Status:      taskstore.StatusInProgress,
		CreatedAt:   time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Add external task: %v", err)
	}
	if _, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "repo-task",
		WorkspaceID: wsID,
		Title:       "Refine ACA retrieval",
		ScopePath:   "internal/contextplane/retrieval.go",
		Status:      taskstore.StatusPending,
		CreatedAt:   time.Date(2026, 3, 14, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Add repo task: %v", err)
	}

	collector := Collector{
		WorkspaceStore: wsStore,
		TaskStore:      taskDB,
	}
	taskID, err := collector.selectTaskID(ctx, Options{
		WorkspacePath: workspacePath,
		WorkspaceID:   wsID,
	})
	if err != nil {
		t.Fatalf("selectTaskID: %v", err)
	}
	if taskID != "repo-task" {
		t.Fatalf("taskID=%q want repo-task", taskID)
	}
}

func TestCollectorCollectUsesTranscriptWorkerForRetrievedBrief(t *testing.T) {
	ctx := context.Background()
	workspacePath := t.TempDir()
	storageRoot := t.TempDir()
	wsID := workspace.CanonicalID(workspacePath)

	wsStore := contextplane.NewWorkspaceStore(workspacePath)
	if _, err := wsStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: wsID,
		Objective:   "Use transcript continuity",
		Phase:       "implement",
		UpdatedAt:   time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	taskDB, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	defer taskDB.Close()
	task, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "T-worker",
		WorkspaceID: wsID,
		Title:       "Use transcript continuity",
		ScopePath:   "internal/contextplane/taskhistory/taskhistory.go",
		Status:      taskstore.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("Add task: %v", err)
	}

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	records := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
		ConversationID: "conv-worker",
		SessionIDs:     []string{"sess-worker"},
	}, []transcriptpipeline.DecisionInsight{
		{
			Kind:                 transcriptpipeline.InsightKindDirection,
			Summary:              "Carry the retrieved transcript bundle into continuity synthesis.",
			Status:               transcriptpipeline.InsightStatusActive,
			Confidence:           0.78,
			SourceBasis:          "user",
			EvidenceFrameIndices: []int{2},
		},
	}, nil, []tphistory.HistoryAnswer{
		{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Use transcript continuity.", Label: "use transcript continuity", Confidence: 0.8},
	})
	if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, workspacePath, "sess-worker", "sess-worker", records, nil); err != nil {
		t.Fatalf("PersistHistoryRecords: %v", err)
	}

	collector := Collector{
		WorkspaceStore: wsStore,
		TaskStore:      taskDB,
		MemoryStore:    memStore,
		TranscriptWorker: &TranscriptSummaryWorker{
			Provider: "lmstudio",
			Model:    transcriptpipeline.DefaultDoctrineBridgeModel,
		},
		TranscriptRun: func(_ context.Context, _ TranscriptSummaryWorker, req TranscriptSummaryRequest) (TranscriptSummaryResponse, error) {
			if req.InputKind != "transcript_history_retrieved_bundle" {
				return TranscriptSummaryResponse{}, fmt.Errorf("unexpected input kind %q", req.InputKind)
			}
			return TranscriptSummaryResponse{
				ModelID:    "test:worker",
				OutputText: `{"continue_with":["carry the retrieved transcript bundle"],"recent_learnings":["use typed history records"],"brief":"Continue with: carry the retrieved transcript bundle\nLearned: use typed history records"}`,
			}, nil
		},
	}
	pack, err := collector.Collect(ctx, Options{
		WorkspacePath: workspacePath,
		WorkspaceID:   wsID,
		TaskID:        task.ID,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if pack.Transcript == nil {
		t.Fatal("expected transcript continuity in pack")
	}
	if !strings.Contains(pack.Transcript.RetrievedBrief, "carry the retrieved transcript bundle") {
		t.Fatalf("retrieved_brief=%q", pack.Transcript.RetrievedBrief)
	}
	if !strings.Contains(pack.Transcript.AgentBrief, "carry the retrieved transcript bundle") {
		t.Fatalf("agent_brief=%q", pack.Transcript.AgentBrief)
	}
	if !strings.Contains(pack.Transcript.AgentBrief, "Learned: use typed history records") {
		t.Fatalf("agent_brief=%q missing canonical learning line", pack.Transcript.AgentBrief)
	}
}

func TestCollectTranscriptFamilyOverview_AggregatesRecentOwners(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mainRepo := filepath.Join(root, "praze")
	worktree := filepath.Join(root, "praze-v2-compare")
	storageRoot := t.TempDir()
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	content := []byte("gitdir: " + filepath.Join(mainRepo, ".git", "worktrees", "praze-v2-compare") + "\n")
	if err := os.WriteFile(gitFile, content, 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	for _, fixture := range []struct {
		owner     string
		workspace string
		answers   []tphistory.HistoryAnswer
	}{
		{
			owner:     "sess-main",
			workspace: mainRepo,
			answers: []tphistory.HistoryAnswer{
				{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Compare branch changes with main and adjust code.", Label: "compare changes", Confidence: 0.8},
				{QuestionID: tphistory.HistoryQuestionRegressions, Answer: "exec_command sandbox denied", Confidence: 0.72},
			},
		},
		{
			owner:     "sess-compare",
			workspace: worktree,
			answers: []tphistory.HistoryAnswer{
				{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Complete backend integration tasks.", Label: "complete backend tasks", Confidence: 0.8},
				{QuestionID: tphistory.HistoryQuestionAcceptedLearnings, Answer: "non-presence hooks are real instead of client-only scaffolds.", Confidence: 0.8},
				{QuestionID: tphistory.HistoryQuestionNextStep, Answer: "finalize integration checks", Confidence: 0.72},
				{QuestionID: tphistory.HistoryQuestionSurprises, Answer: "split socket targets", Confidence: 0.8},
				{QuestionID: tphistory.HistoryQuestionRegressions, Answer: "exec_command sandbox denied", Confidence: 0.72},
			},
		},
	} {
		records := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
			ConversationID: "conv-" + fixture.owner,
			SessionIDs:     []string{fixture.owner},
		}, nil, nil, fixture.answers)
		if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, fixture.workspace, fixture.owner, fixture.owner, records, nil); err != nil {
			t.Fatalf("PersistHistoryRecords(%s): %v", fixture.owner, err)
		}
	}

	collector := Collector{MemoryStore: memStore}
	overview, err := collector.CollectTranscriptFamilyOverview(ctx, worktree, TranscriptHistoryScopeFamily, 4, "", TranscriptHistoryDateRange{})
	if err != nil {
		t.Fatalf("CollectTranscriptFamilyOverview: %v", err)
	}
	if overview == nil {
		t.Fatal("expected family overview")
		return
	}
	if len(overview.CurrentFocus) == 0 {
		t.Fatalf("overview=%+v missing current focus", overview)
	}
	if len(overview.TopLearnings) == 0 || overview.TopLearnings[0] != "non-presence hooks are real instead of client-only scaffolds." {
		t.Fatalf("overview=%+v missing learning", overview)
	}
	if len(overview.RecurringMistakes) == 0 || overview.RecurringMistakes[0] != "exec_command sandbox denied" {
		t.Fatalf("overview=%+v missing recurring mistake", overview)
	}
	if len(overview.SupportMetadata) == 0 {
		t.Fatalf("overview=%+v missing support metadata", overview)
	}
}

func TestCollectTranscriptFamilyOverview_FocusQuerySelectsMatchingLane(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mainRepo := filepath.Join(root, "agentctl")
	worktree := filepath.Join(root, "agentctl-compare")
	storageRoot := t.TempDir()
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	content := []byte("gitdir: " + filepath.Join(mainRepo, ".git", "worktrees", "agentctl-compare") + "\n")
	if err := os.WriteFile(gitFile, content, 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	fixtures := []struct {
		owner   string
		answers []tphistory.HistoryAnswer
	}{
		{
			owner: "sess-memory",
			answers: []tphistory.HistoryAnswer{
				{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Implement transcript-derived recursive memory consolidation.", Label: "recursive memory consolidation", Confidence: 0.8},
				{QuestionID: tphistory.HistoryQuestionAcceptedLearnings, Answer: "Use a second-pass consolidation layer over the existing hybrid companion runtime.", Confidence: 0.8},
				{QuestionID: tphistory.HistoryQuestionNextStep, Answer: "Wire grouped transcript consensus into durable consolidation.", Confidence: 0.72},
			},
		},
		{
			owner: "sess-aca",
			answers: []tphistory.HistoryAnswer{
				{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Install ACA hooks into workspace Claude settings.", Label: "aca hooks install", Confidence: 0.8},
				{QuestionID: tphistory.HistoryQuestionAcceptedLearnings, Answer: "Documentation alignment with official Obsidian CLI syntax matters.", Confidence: 0.8},
				{QuestionID: tphistory.HistoryQuestionNextStep, Answer: "Document the hook wiring and Obsidian authoring flow.", Confidence: 0.72},
			},
		},
	}
	for _, fixture := range fixtures {
		records := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
			ConversationID: "conv-" + fixture.owner,
			SessionIDs:     []string{fixture.owner},
		}, nil, nil, fixture.answers)
		if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, mainRepo, fixture.owner, fixture.owner, records, nil); err != nil {
			t.Fatalf("PersistHistoryRecords(%s): %v", fixture.owner, err)
		}
	}

	collector := Collector{MemoryStore: memStore}
	overview, err := collector.CollectTranscriptFamilyOverview(ctx, worktree, TranscriptHistoryScopeFamily, 4, "recursive memory consolidation transcript pipeline", TranscriptHistoryDateRange{})
	if err != nil {
		t.Fatalf("CollectTranscriptFamilyOverview: %v", err)
	}
	if overview == nil {
		t.Fatal("expected focused family overview")
		return
	}
	if overview.FocusQuery != "recursive memory consolidation transcript pipeline" {
		t.Fatalf("focus_query=%q", overview.FocusQuery)
	}
	if len(overview.CurrentFocus) == 0 || overview.CurrentFocus[0] != "recursive memory consolidation" {
		t.Fatalf("current_focus=%v", overview.CurrentFocus)
	}
	if containsString(overview.CurrentFocus, "aca hooks install") {
		t.Fatalf("current_focus=%v leaked unrelated lane", overview.CurrentFocus)
	}
}

func TestCollectTranscriptFamilyOverview_DateRangeFiltersOwners(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mainRepo := filepath.Join(root, "agentctl")
	worktree := filepath.Join(root, "agentctl-compare")
	storageRoot := t.TempDir()
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	content := []byte("gitdir: " + filepath.Join(mainRepo, ".git", "worktrees", "agentctl-compare") + "\n")
	if err := os.WriteFile(gitFile, content, 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	memStore, err := memory.Open(ctx, storageRoot, "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	newAnswers := []tphistory.HistoryAnswer{
		{QuestionID: tphistory.HistoryQuestionObjective, Answer: "Implement transcript-derived recursive memory consolidation.", Label: "recursive memory consolidation", Confidence: 0.8},
	}
	newRecords := transcriptpipeline.BuildHistoryRecords(tphistory.DefaultHistoryProfile(), tphistory.HistoryRecordContext{
		ConversationID: "conv-new",
		SessionIDs:     []string{"sess-new"},
	}, nil, nil, newAnswers)
	if _, err := transcriptpipeline.PersistHistoryRecords(ctx, memStore, mainRepo, "sess-new", "sess-new", newRecords, nil); err != nil {
		t.Fatalf("PersistHistoryRecords(new): %v", err)
	}

	now := time.Now().UTC()
	dateRange, err := ParseTranscriptHistoryDateRange(now.Add(-24*time.Hour).Format("2006-01-02"), now.Add(24*time.Hour).Format("2006-01-02"))
	if err != nil {
		t.Fatalf("ParseTranscriptHistoryDateRange: %v", err)
	}
	collector := Collector{MemoryStore: memStore}
	overview, err := collector.CollectTranscriptFamilyOverview(ctx, worktree, TranscriptHistoryScopeFamily, 4, "", dateRange)
	if err != nil {
		t.Fatalf("CollectTranscriptFamilyOverview: %v", err)
	}
	if overview == nil {
		t.Fatal("expected date-filtered overview")
		return
	}
	if overview.DateFrom != dateRange.DateFrom || overview.DateTo != dateRange.DateTo {
		t.Fatalf("date range not preserved in overview: %+v", overview)
	}
	if len(overview.CurrentFocus) == 0 || overview.CurrentFocus[0] != "recursive memory consolidation" {
		t.Fatalf("current_focus=%v", overview.CurrentFocus)
	}
	futureRange, err := ParseTranscriptHistoryDateRange(now.AddDate(1, 0, 0).Format("2006-01-02"), now.AddDate(1, 0, 31).Format("2006-01-02"))
	if err != nil {
		t.Fatalf("ParseTranscriptHistoryDateRange(future): %v", err)
	}
	overview, err = collector.CollectTranscriptFamilyOverview(ctx, worktree, TranscriptHistoryScopeFamily, 4, "", futureRange)
	if err != nil {
		t.Fatalf("CollectTranscriptFamilyOverview(future): %v", err)
	}
	if overview != nil {
		t.Fatalf("expected no overview for future-only range, got %+v", overview)
	}
}

func TestEnsureTranscriptBriefLine_ReplacesOverlappingLearningLine(t *testing.T) {
	base := "Objective: complete backend tasks\nLearned: non-presence hooks are real instead of client-only scaffolds."
	got := ensureTranscriptBriefLine(base, "Recent learnings: non-presence hooks are real instead of client-only scaffolds.")
	if strings.Contains(got, "Learned: non-presence hooks are real instead of client-only scaffolds.") {
		t.Fatalf("brief=%q kept older overlapping learning line", got)
	}
	if !strings.Contains(got, "Recent learnings: non-presence hooks are real instead of client-only scaffolds.") {
		t.Fatalf("brief=%q missing replacement learning line", got)
	}
}

func retrievalFixtureVaultRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "tools", "obsidian", "testdata", "vaults", "basic"))
}
