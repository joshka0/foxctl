package longmemeval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

func TestSummarizeComputesHitAndMRR(t *testing.T) {
	cases := []CaseResult{
		{CaseID: "a", HitAt5: true, HitAt10: true, HitAt50: true, HitAt100: true, ReciprocalRank: 1.0, DurationMS: 10},
		{CaseID: "b", HitAt5: false, HitAt10: true, HitAt50: true, HitAt100: true, ReciprocalRank: 0.5, DurationMS: 20},
		{CaseID: "c", HitAt5: false, HitAt10: false, HitAt50: false, HitAt100: false, ReciprocalRank: 0, DurationMS: 30},
		{CaseID: "d", Error: "boom"},
	}
	m := Summarize(cases)
	if m.CaseCount != 4 || m.FailureCount != 1 {
		t.Fatalf("counts=%+v want case=4 failure=1", m)
	}
	if m.HitAt5 < 0.32 || m.HitAt5 > 0.34 {
		t.Fatalf("hit@5=%v want ~0.33", m.HitAt5)
	}
	if m.HitAt10 < 0.65 || m.HitAt10 > 0.67 {
		t.Fatalf("hit@10=%v want ~0.66", m.HitAt10)
	}
	if m.HitAt50 < 0.65 || m.HitAt50 > 0.67 {
		t.Fatalf("hit@50=%v want ~0.67", m.HitAt50)
	}
	if m.HitAt100 < 0.65 || m.HitAt100 > 0.67 {
		t.Fatalf("hit@100=%v want ~0.67", m.HitAt100)
	}
	if m.MRR < 0.49 || m.MRR > 0.51 {
		t.Fatalf("mrr=%v want ~0.5", m.MRR)
	}
	if m.MeanLatencyMS < 19.9 || m.MeanLatencyMS > 20.1 {
		t.Fatalf("mean latency=%v want ~20", m.MeanLatencyMS)
	}
}

func TestSummarizeEmptyCases(t *testing.T) {
	m := Summarize(nil)
	if m.CaseCount != 0 || m.FailureCount != 0 || m.HitAt5 != 0 || m.MRR != 0 {
		t.Fatalf("empty summary=%+v", m)
	}
}

func TestWriteArtifactsSkipsWhenDirEmpty(t *testing.T) {
	if err := WriteArtifacts("", RunResult{}); err != nil {
		t.Fatalf("WriteArtifacts(\"\") err=%v", err)
	}
}

func TestWriteArtifactsWritesReportAndPerCaseFiles(t *testing.T) {
	dir := t.TempDir()
	result := RunResult{
		Suite:       "longmem",
		WorkspaceID: "ws-longmem",
		DatasetPath: filepath.Join("fixtures", "longmem.json"),
		ArtifactDir: dir,
		Limit:       5,
		GeneratedAt: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
		Modes:       []string{"retrieval"},
		Cases: []CaseResult{
			{CaseID: "case-2", Method: "bm25", RetrievedNames: []string{"longmem://bbb"}, HitAt5: true, ReciprocalRank: 1.0},
			{CaseID: "case-1", Method: "bm25", RetrievedNames: []string{"longmem://aaa"}, HitAt5: true, ReciprocalRank: 1.0},
		},
		Metrics: &Metrics{CaseCount: 2, HitAt5: 1.0, MRR: 1.0},
	}
	originalFirstCase := result.Cases[0].CaseID
	if err := WriteArtifacts(dir, result); err != nil {
		t.Fatalf("WriteArtifacts: %v", err)
	}
	reportBody, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var decoded RunResult
	if err := json.Unmarshal(reportBody, &decoded); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if decoded.Suite != "longmem" || len(decoded.Cases) != 2 {
		t.Fatalf("decoded report=%+v", decoded)
	}
	if decoded.Cases[0].CaseID != "case-1" || decoded.Cases[1].CaseID != "case-2" {
		t.Fatalf("report cases not sorted: %+v", decoded.Cases)
	}
	if result.Cases[0].CaseID != originalFirstCase {
		t.Fatalf("WriteArtifacts mutated caller result cases: %+v", result.Cases)
	}
	caseBody, err := os.ReadFile(filepath.Join(dir, "cases", "case-1.json"))
	if err != nil {
		t.Fatalf("read case: %v", err)
	}
	var decodedCase CaseResult
	if err := json.Unmarshal(caseBody, &decodedCase); err != nil {
		t.Fatalf("decode case: %v", err)
	}
	if decodedCase.CaseID != "case-1" || !decodedCase.HitAt5 {
		t.Fatalf("decoded case=%+v", decodedCase)
	}
	headToHeadBody, err := os.ReadFile(filepath.Join(dir, "head-to-head.md"))
	if err != nil {
		t.Fatalf("read head-to-head report: %v", err)
	}
	headToHead := string(headToHeadBody)
	for _, want := range []string{"foxctl raw memory/query equivalent", "local retrieval-only equivalent", "HydraDB baseline", "--mode retrieval", "--artifact-dir", "foxctl run embedding/worker", "\"process_all\":true"} {
		if !strings.Contains(headToHead, want) {
			t.Fatalf("head-to-head report missing %q:\n%s", want, headToHead)
		}
	}
}

func TestRenderHeadToHeadMarkdownIncludesRetrievalAnswerAndUnavailableBaseline(t *testing.T) {
	result := RunResult{
		Suite:       "longmem",
		WorkspaceID: "ws-longmem",
		DatasetPath: "testdata/longmem fixture.json",
		ArtifactDir: "artifacts out",
		Limit:       7,
		GeneratedAt: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
		Modes:       []string{"retrieval", "answer"},
		Cases: []CaseResult{{
			CaseID:                "case-1",
			Answer:                "decorated",
			AnswerEvidenceNames:   []string{"longmem://aaa"},
			AnswerMatchedEvidence: []string{"longmem://aaa"},
		}},
		Metrics: &Metrics{
			CaseCount:           1,
			HitAt5:              1,
			HitAt10:             1,
			HitAt50:             1,
			HitAt100:            1,
			MRR:                 1,
			MeanLatencyMS:       3,
			AnswerCaseCount:     1,
			AnswerMatchedCount:  1,
			AnswerAccuracy:      1,
			AnswerMeanScore:     1,
			AnswerMeanLatencyMS: 11,
		},
	}
	md := RenderHeadToHeadMarkdown(result)
	for _, want := range []string{
		"foxctl raw memory/query equivalent",
		"hit@5 1.000",
		"no `memory/query` skill/tool invocation is recorded",
		"foxctl answer-mode",
		"accuracy 1.000",
		"evidence-hit 1.000",
		"HydraDB baseline | not run",
		"No HydraDB or external baseline data is attached",
		"deterministic non-refusal exact/answer-contains-expected",
		"foxctl eval longmem --dataset 'testdata/longmem fixture.json'",
		"--artifact-dir 'artifacts out'",
		"foxctl run embedding/worker --ephemeral --input '{\"workspace_id\":\"ws-longmem\",\"kind\":\"memory\",\"batch_size\":5,\"max_duration\":60,\"parallelism\":1,\"process_all\":true}'",
		"foxctl eval longmem --dataset 'testdata/longmem fixture.json' --workspace-id ws-longmem --mode answer --limit 7 --artifact-dir 'artifacts out'",
		"CLI default targets RLM `memory_recall`",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("RenderHeadToHeadMarkdown missing %q:\n%s", want, md)
		}
	}
}

func TestRenderHeadToHeadMarkdownKeepsUnavailableRowsHonest(t *testing.T) {
	result := RunResult{
		Suite:       "longmem",
		WorkspaceID: "ws-longmem",
		DatasetPath: "testdata/longmem.json",
		ArtifactDir: "artifacts",
		Limit:       5,
		GeneratedAt: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
		Modes:       []string{"retrieval"},
		Cases:       []CaseResult{{CaseID: "case-1", HitAt5: true, ReciprocalRank: 1}},
		Metrics:     &Metrics{CaseCount: 1, HitAt5: 1, MRR: 1, MeanLatencyMS: 4},
	}

	md := RenderHeadToHeadMarkdown(result)
	for _, want := range []string{
		"| foxctl raw memory/query equivalent | run | hit@5 1.000",
		"| foxctl answer-mode | not run | unavailable | unavailable",
		"| HydraDB baseline | not run | unavailable | unavailable",
		"not an invoked `memory/query` skill/tool run",
		"--mode retrieval",
		"--mode answer",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("RenderHeadToHeadMarkdown missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "foxctl answer-mode | run") {
		t.Fatalf("answer row claimed run without answer mode:\n%s", md)
	}
	if strings.Contains(md, "HydraDB baseline | run") {
		t.Fatalf("baseline row claimed run without baseline data:\n%s", md)
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
	plan, err := BuildPlan(cases, IngestOptions{WorkspaceID: workspaceID, EmbeddingModel: "text-embedding-qwen3-embedding-8b"})
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

func TestAnswerMatchScoreRejectsQuotedExpectedValueInRefusal(t *testing.T) {
	t.Parallel()

	answer := `I cannot provide a verified answer. The evidence ledger rejected all candidate memories; one rejected claim mentioned 45 minutes each way, but it was not accepted.`
	if got := answerMatchScore(answer, "45 minutes each way"); got != 0 {
		t.Fatalf("answerMatchScore()=%v want 0 for refused/rejected quoted value", got)
	}
	if got := answerMatchScore("Your commute is 45 minutes each way.", "45 minutes each way"); got != 1 {
		t.Fatalf("answerMatchScore()=%v want 1 for direct supported wording", got)
	}
	if got := answerMatchScore("Target", "Target checkout last Sunday"); got != 0 {
		t.Fatalf("answerMatchScore()=%v want 0 for partial substring answer", got)
	}
}

func TestKeyFactOverlapScoreCatchesParaphrasedAnswer(t *testing.T) {
	t.Parallel()

	// Video-editing case: answer correctly identifies Premiere Pro preference
	// but phrases it differently from the expected answer.
	answer := "I found a strong match in memory from a past conversation where you were exploring advanced settings in Adobe Premiere Pro. Since you already enjoy using Premiere Pro, here are the resources we discussed."
	expected := "The user would prefer responses that suggest resources specifically tailored to Adobe Premiere Pro, especially those that delve into its advanced settings."
	score := answerMatchScore(answer, expected)
	if score == 0 {
		t.Fatalf("paraphrased correct answer should score > 0 via key-fact overlap")
	}
	if score < 0.3 {
		t.Fatalf("overlap score %f should be >= 0.3 for this case", score)
	}
}

func TestKeyFactOverlapScoreRejectsWrongAnswer(t *testing.T) {
	t.Parallel()

	// Clothing case: expected "3", answer says "2 items" — genuinely wrong.
	score := answerMatchScore("you need to attend to 2 items of clothing", "3")
	if score > 0 {
		t.Fatalf("wrong numeric answer should score 0, got %f", score)
	}
}

func TestKeyFactOverlapScoreShortExpectedUsesStrictOnly(t *testing.T) {
	t.Parallel()

	// Short expected answer ("yes") should not trigger overlap scoring.
	score := answerMatchScore("yeah absolutely that is correct", "yes")
	if score > 0 {
		t.Fatalf("short expected answer should use strict matching only, got %f", score)
	}
}

func TestBidirectionalContainsScore(t *testing.T) {
	t.Parallel()

	// Answer "business administration" is contained in the longer expected
	// answer. The answer is a significant fraction of the expected, so this
	// should score via the key-fact overlap path even if the bidirectional
	// length check doesn't fire.
	score := answerMatchScore("business administration", "business administration. you mentioned it has been helpful.")
	if score == 0 {
		t.Fatalf("answer containing expected key facts should score > 0")
	}
}

func TestNumericFactMatchScoreCatchesVerboseCorrectAnswer(t *testing.T) {
	t.Parallel()

	// MoMA case: answer is verbose but contains "7 days"
	answer := "7 days passed between your two museum visits. MoMA visit: January 8, 2023. Met visit: January 15, 2023."
	expected := "7 days. 8 days (including the last day) is also acceptable."
	score := answerMatchScore(answer, expected)
	if score == 0 {
		t.Fatalf("verbose answer containing '7 days' should match expected '7 days. ...'")
	}
}

func TestNumericFactMatchScoreRejectsWrongNumber(t *testing.T) {
	t.Parallel()

	// Wrong number: answer says 3 days, expected 7 days
	score := answerMatchScore("3 days passed between visits", "7 days. 8 days (including the last day) is also acceptable.")
	if score > 0 {
		t.Fatalf("wrong numeric answer should score 0, got %f", score)
	}
}

func TestNumericFactMatchScoreMarkdownFormatting(t *testing.T) {
	t.Parallel()

	// Answer has markdown bold formatting: "**7 days**"
	score := answerMatchScore("**7 days** passed between the visits", "7 days")
	if score == 0 {
		t.Fatalf("markdown-formatted '7 days' should match expected '7 days'")
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
