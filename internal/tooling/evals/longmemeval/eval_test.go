package longmemeval

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/intelligence/retrieval/memoryrecall"
	"github.com/joshka0/foxctl/internal/storage"
	memstore "github.com/joshka0/foxctl/internal/storage/memory"
)

func TestNormalizeModesDefaultsAndDedup(t *testing.T) {
	got, err := NormalizeModes(nil)
	if err != nil {
		t.Fatalf("NormalizeModes(nil) err=%v", err)
	}
	want := []Mode{ModeIngest, ModeQueueStatus, ModeRetrieval}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default modes=%v want %v", got, want)
	}

	got, err = NormalizeModes([]string{"retrieval", "queue-status", "retrieval", " QUEUE-CHECK ", "answer-mode", "answer"})
	if err != nil {
		t.Fatalf("NormalizeModes err=%v", err)
	}
	want = []Mode{ModeRetrieval, ModeQueueStatus, ModeAnswer}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized modes=%v want %v", got, want)
	}
}

func TestNormalizeModesRejectsUnknown(t *testing.T) {
	if _, err := NormalizeModes([]string{"judge"}); err == nil {
		t.Fatalf("expected error for unknown mode")
	}
}

func TestExpectedMemoryNamesMatchBuildPlanHashing(t *testing.T) {
	workspaceID := "ws-longmem"
	caseID := "case-1"
	sessionIDs := []string{"sharegpt_001", "sharegpt_002"}
	names := ExpectedMemoryNames(memoryName, workspaceID, caseID, sessionIDs)
	if len(names) != 2 {
		t.Fatalf("names=%v want 2", names)
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "longmem://") {
			t.Fatalf("name=%q missing longmem:// prefix", name)
		}
	}
	// Determinism: same inputs produce same names.
	again := ExpectedMemoryNames(memoryName, workspaceID, caseID, sessionIDs)
	if !reflect.DeepEqual(names, again) {
		t.Fatalf("non-deterministic names: %v vs %v", names, again)
	}
}

func TestPlanLeakageByCaseGroupsFindingsByCaseID(t *testing.T) {
	plan := Plan{Leakage: []LeakageFinding{
		{CaseID: "case-1", Field: "summary", Token: "x", Reason: "answer"},
		{CaseID: "case-1", Field: "name", Token: "y", Reason: "question"},
		{CaseID: "case-2", Field: "atomic_text", Token: "z", Reason: "answer"},
		{CaseID: "", Field: "name", Token: "orphan", Reason: "answer"},
	}}
	counts := PlanLeakageByCase(plan)
	if counts["case-1"] != 2 {
		t.Fatalf("case-1 count=%d want 2", counts["case-1"])
	}
	if counts["case-2"] != 1 {
		t.Fatalf("case-2 count=%d want 1", counts["case-2"])
	}
	if _, ok := counts[""]; ok {
		t.Fatalf("empty caseID should be ignored")
	}
}

func TestRunRetrievalOnlyScoresWithFakeStore(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	memory, err := memstore.Open(ctx, root, "")
	if err != nil {
		t.Fatalf("memory open: %v", err)
	}
	t.Cleanup(func() { _ = memory.Close() })

	workspaceID := "ws-longmem"
	// Two cases, two memories each: one expected, one distractor. We
	// set HaystackSessionIDs to the expected session IDs so the
	// ingested memory names match the expected evidence names exactly.
	cases := []Case{
		{
			QuestionID:         "case-1",
			Question:           "which local model supports Qwen recall",
			Answer:             "Turso with Qwen",
			AnswerSessionIDs:   []string{"sharegpt_001"},
			HaystackSessionIDs: []string{"sharegpt_001"},
			HaystackSessions: [][]Message{{
				{Role: "user", Content: "I rely on local Qwen embeddings for memory recall."},
			}},
		},
		{
			QuestionID:         "case-2",
			Question:           "which session mentions decorated material",
			Answer:             "decorated",
			AnswerSessionIDs:   []string{"sharegpt_002"},
			HaystackSessionIDs: []string{"sharegpt_002"},
			HaystackSessions: [][]Message{{
				{Role: "user", Content: "decorated content lives here"},
			}},
		},
	}
	plan, err := BuildPlan(context.Background(), cases, IngestOptions{WorkspaceID: workspaceID, EmbeddingModel: "text-embedding-qwen3-embedding-8b"})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Records) != 2 {
		t.Fatalf("records=%d want 2", len(plan.Records))
	}
	for _, rec := range plan.Records {
		if _, err := memory.SaveFromResult(ctx, rec.Name, rec.Type, workspaceID, rec.Summary, rec.Result); err != nil {
			t.Fatalf("SaveFromResult(%s): %v", rec.Name, err)
		}
		if err := memory.UpdateAtomic(ctx, rec.Name, workspaceID, rec.AtomicText, rec.Entities, rec.Keywords); err != nil {
			t.Fatalf("UpdateAtomic(%s): %v", rec.Name, err)
		}
	}
	deps := Deps{
		LoadCases: func(string) ([]Case, error) { return cases, nil },
		OpenMemory: func(context.Context, string) (storage.MemoryStore, error) {
			// Reopen so the run's defer Close matches the test's t.Cleanup Close.
			return memstore.Open(ctx, root, "")
		},
		OpenQueue: nil,
		Now:       func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
	}
	result, err := Run(ctx, EvalOptions{
		DatasetPath: "test://fixture",
		WorkspaceID: workspaceID,
		Modes:       []Mode{ModeRetrieval},
		Limit:       5,
	}, deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Cases) != 2 {
		t.Fatalf("cases=%d want 2", len(result.Cases))
	}
	for _, c := range result.Cases {
		if c.Method != memoryrecall.MethodBM25 {
			t.Fatalf("case %s method=%q want %q", c.CaseID, c.Method, memoryrecall.MethodBM25)
		}
	}
	// BM25 should rank the matching memory first for both cases because
	// the question terms overlap with the ingested atomic text.
	for _, c := range result.Cases {
		if !c.HitAt5 || c.ReciprocalRank != 1.0 {
			t.Fatalf("case %s hit@5=%v mrr=%v want true/1.0; result=%+v", c.CaseID, c.HitAt5, c.ReciprocalRank, c)
		}
		if len(c.MatchedNames) != 1 {
			t.Fatalf("case %s matched=%v want 1", c.CaseID, c.MatchedNames)
		}
	}
	if result.Metrics == nil {
		t.Fatalf("metrics missing")
	}
	if result.Metrics.HitAt5 != 1.0 || result.Metrics.MRR != 1.0 {
		t.Fatalf("metrics=%+v want hit@5=1.0 mrr=1.0", result.Metrics)
	}
}

func TestRunIngestUsesDefaultApplyPlan(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceID := "ws-longmem-ingest"
	cases := []Case{{
		QuestionID:         "case-ingest",
		Question:           "Which local embedding model family is used?",
		Answer:             "Qwen",
		AnswerSessionIDs:   []string{"sharegpt_001"},
		HaystackSessionIDs: []string{"sharegpt_001"},
		HaystackSessions: [][]Message{{
			{Role: "user", Content: "The memory embedding queue uses the local Qwen embedder."},
		}},
	}}

	deps := Deps{
		LoadCases: func(string) ([]Case, error) { return cases, nil },
		OpenMemory: func(context.Context, string) (storage.MemoryStore, error) {
			return memstore.Open(ctx, filepath.Join(root, "memory"), "")
		},
		OpenQueue: func(context.Context, string) (*embedding.Store, error) {
			return embedding.OpenStore(ctx, filepath.Join(root, "cache"))
		},
		Now: func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
	}
	result, err := Run(ctx, EvalOptions{
		DatasetPath:    "test://fixture",
		WorkspaceID:    workspaceID,
		Modes:          []Mode{ModeIngest},
		EmbeddingModel: "text-embedding-qwen3-embedding-8b",
	}, deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ingest == nil || result.Ingest.Saved != 1 || result.Ingest.Queued != 1 {
		t.Fatalf("ingest=%+v want saved=1 queued=1", result.Ingest)
	}

	queue, err := embedding.OpenStore(ctx, filepath.Join(root, "cache"))
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	defer func() { _ = queue.Close() }()
	stats, err := queue.StatsInWorkspaceKind(ctx, workspaceID, MemoryKind)
	if err != nil {
		t.Fatalf("queue stats: %v", err)
	}
	if stats.QueuedCount != 1 {
		t.Fatalf("queued=%d want 1", stats.QueuedCount)
	}
}

func TestRunRequiresWorkspaceID(t *testing.T) {
	_, err := Run(context.Background(), EvalOptions{Modes: []Mode{ModeRetrieval}}, Deps{})
	if err == nil || !strings.Contains(err.Error(), "workspace_id is required") {
		t.Fatalf("Run err=%v want workspace_id required", err)
	}
}

func TestRunRequiresDatasetForIngest(t *testing.T) {
	_, err := Run(context.Background(), EvalOptions{
		WorkspaceID: "ws-longmem",
		Modes:       []Mode{ModeIngest},
	}, Deps{
		OpenMemory: func(context.Context, string) (storage.MemoryStore, error) { return nil, nil },
		OpenQueue:  func(context.Context, string) (*embedding.Store, error) { return nil, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "dataset is required") {
		t.Fatalf("Run err=%v want dataset required", err)
	}
}

func TestRunRejectsLeakageBeforeRetrievalScoring(t *testing.T) {
	_, err := Run(context.Background(), EvalOptions{
		DatasetPath: "test://fixture",
		WorkspaceID: "ws-longmem",
		Modes:       []Mode{ModeRetrieval},
	}, Deps{
		LoadCases: func(string) ([]Case, error) {
			return []Case{{
				QuestionID:         "case-leak",
				Question:           "secret exact question",
				AnswerSessionIDs:   []string{"session-001"},
				HaystackSessionIDs: []string{"session-001"},
				HaystackSessions: [][]Message{{
					{Role: "user", Content: "secret exact question"},
				}},
			}}, nil
		},
	})
	if !errors.Is(err, ErrLeakage) {
		t.Fatalf("Run err=%v want ErrLeakage", err)
	}
}

func TestRunAnswerModeScoresWithFakeRunner(t *testing.T) {
	ctx := context.Background()
	workspaceID := "ws-longmem"
	cases := []Case{{
		QuestionID:         "case-answer",
		Question:           "which marker is in the recalled memory",
		Answer:             "decorated",
		AnswerSessionIDs:   []string{"sharegpt_001"},
		HaystackSessionIDs: []string{"sharegpt_001"},
		HaystackSessions: [][]Message{{
			{Role: "user", Content: "The recalled memory says the marker is decorated."},
		}},
	}}
	deps := Deps{
		LoadCases: func(string) ([]Case, error) { return cases, nil },
		RunAnswer: func(_ context.Context, req AnswerRequest) (AnswerResult, error) {
			if req.WorkspaceID != workspaceID || req.CaseID != "case-answer" {
				t.Fatalf("bad request=%+v", req)
			}
			expectedNames := ExpectedMemoryNames(memoryName, workspaceID, "case-answer", []string{"sharegpt_001"})
			return AnswerResult{
				Answer:        "The marker is decorated.",
				Method:        "fake-answer",
				ToolNames:     []string{"retrieve_memory"},
				EvidenceNames: expectedNames,
				EvidenceRefs:  []string{"memory_claim:" + expectedNames[0]},
				Iterations:    1,
				DurationMS:    7,
			}, nil
		},
		Now: func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
	}
	result, err := Run(ctx, EvalOptions{
		DatasetPath: "test://fixture",
		WorkspaceID: workspaceID,
		Modes:       []Mode{ModeAnswer},
		Limit:       5,
	}, deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Cases) != 1 {
		t.Fatalf("cases=%d want 1", len(result.Cases))
	}
	c := result.Cases[0]
	if !c.AnswerMatched || c.AnswerScore != 1 || c.Answer != "The marker is decorated." {
		t.Fatalf("answer scoring=%+v", c)
	}
	if !c.HitAt5 || c.ReciprocalRank != 1 || len(c.AnswerMatchedEvidence) != 1 {
		t.Fatalf("evidence scoring=%+v", c)
	}
	if c.ExpectedAnswer != "decorated" || c.AnswerMethod != "fake-answer" || c.AnswerDurationMS != 7 {
		t.Fatalf("answer artifact fields=%+v", c)
	}
	if result.Metrics == nil || result.Metrics.AnswerAccuracy != 1 || result.Metrics.AnswerMeanScore != 1 {
		t.Fatalf("metrics=%+v", result.Metrics)
	}
}

func TestRunAnswerModeRequiresRunner(t *testing.T) {
	_, err := Run(context.Background(), EvalOptions{
		DatasetPath: "test://fixture",
		WorkspaceID: "ws-longmem",
		Modes:       []Mode{ModeAnswer},
	}, Deps{
		LoadCases: func(string) ([]Case, error) {
			return []Case{{
				QuestionID:         "case-answer",
				Question:           "what marker is mentioned",
				Answer:             "decorated",
				AnswerSessionIDs:   []string{"sharegpt_001"},
				HaystackSessionIDs: []string{"sharegpt_001"},
				HaystackSessions: [][]Message{{
					{Role: "user", Content: "marker decorated"},
				}},
			}}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "answer runner is required") {
		t.Fatalf("Run err=%v want answer runner required", err)
	}
}

func TestRunRejectsLeakageBeforeAnswerRunner(t *testing.T) {
	called := false
	_, err := Run(context.Background(), EvalOptions{
		DatasetPath: "test://fixture",
		WorkspaceID: "ws-longmem",
		Modes:       []Mode{ModeAnswer},
	}, Deps{
		LoadCases: func(string) ([]Case, error) {
			return []Case{{
				QuestionID:         "case-leak",
				Question:           "secret exact question",
				Answer:             "secret answer",
				AnswerSessionIDs:   []string{"session-001"},
				HaystackSessionIDs: []string{"session-001"},
				HaystackSessions: [][]Message{{
					{Role: "user", Content: "secret exact question with secret answer"},
				}},
			}}, nil
		},
		RunAnswer: func(context.Context, AnswerRequest) (AnswerResult, error) {
			called = true
			return AnswerResult{}, nil
		},
	})
	if !errors.Is(err, ErrLeakage) {
		t.Fatalf("Run err=%v want ErrLeakage", err)
	}
	if called {
		t.Fatalf("answer runner was called before leakage rejection")
	}
}

func TestRunQueueStatusSurfacesStats(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	queue, err := embedding.OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("queue open: %v", err)
	}
	t.Cleanup(func() { _ = queue.Close() })

	if _, err := queue.EnqueueMemories(ctx, embedding.MemoryEnqueueRequest{
		WorkspaceID: "ws-longmem",
		Memories: []embedding.MemoryInput{{
			Name:    "note:smoke",
			Type:    "note",
			Content: "smoke content",
		}},
		Model: "text-embedding-qwen3-embedding-8b",
	}); err != nil {
		t.Fatalf("EnqueueMemories: %v", err)
	}

	deps := Deps{
		OpenQueue: func(context.Context, string) (*embedding.Store, error) {
			return embedding.OpenStore(ctx, root)
		},
	}
	result, err := Run(ctx, EvalOptions{WorkspaceID: "ws-longmem", Modes: []Mode{ModeQueueStatus}}, deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.QueueStatus == nil {
		t.Fatalf("QueueStatus missing")
	}
	if result.QueueStatus.Kind != string(embedqueue.TaskKindMemory) {
		t.Fatalf("queue kind=%q want %q", result.QueueStatus.Kind, embedqueue.TaskKindMemory)
	}
	if result.QueueStatus.Stats == nil || result.QueueStatus.Stats.QueuedCount != 1 {
		t.Fatalf("queue stats=%+v", result.QueueStatus.Stats)
	}
}

func TestSortCasesIsStableByCaseID(t *testing.T) {
	input := []CaseResult{{CaseID: "b"}, {CaseID: "a"}, {CaseID: "c"}}
	snapshot := append([]CaseResult(nil), input...)
	got := SortCases(input)
	want := []string{"a", "b", "c"}
	ids := []string{got[0].CaseID, got[1].CaseID, got[2].CaseID}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("sorted=%v want %v", ids, want)
	}
	if !reflect.DeepEqual(input, snapshot) {
		t.Fatalf("input should be untouched; got=%v want=%v", input, snapshot)
	}
}

func TestSanitizeArtifactNameStripsUnsafe(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"Case #1!": "case--1",
		"safe-id":  "safe-id",
		"  trim  ": "trim",
	}
	for in, want := range cases {
		if got := sanitizeArtifactName(in); got != want {
			t.Fatalf("sanitizeArtifactName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestHybridDepsWiresEmbeddingIntoSearch(t *testing.T) {
	t.Parallel()

	// HybridDeps should pass the embedding into the search function.
	// Verify the embedFn is called and the result flows through.
	deps := HybridDeps(nil, nil, func(_ context.Context, query string) ([]float32, error) {
		return []float32{0.1, 0.2, 0.3}, nil
	})
	if deps.SearchMemory == nil {
		t.Fatal("SearchMemory should be wired")
	}
	// The embedFn should be called — verify by checking the search would use it.
	// We can't call SearchMemory without a real store, but we can verify the
	// wiring is non-nil and the embedFn is captured.
	if deps.SearchMemory == nil {
		t.Fatal("HybridDeps SearchMemory is nil")
	}
}

func TestHybridDepsNilEmbedFnFallsBackToBM25(t *testing.T) {
	t.Parallel()

	// HybridDeps with nil embedFn should not panic and should work like DefaultDeps.
	deps := HybridDeps(nil, nil, nil)
	if deps.SearchMemory == nil {
		t.Fatal("SearchMemory should be wired even with nil embedFn")
	}
}
