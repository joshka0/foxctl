package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	memstore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/tooling/evals/longmemeval"
)

func TestSplitEvalModesAcceptsRepeatableAndComma(t *testing.T) {
	got := splitEvalModes([]string{"ingest,retrieval", " queue-status "})
	want := []string{"ingest", "retrieval", "queue-status"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitEvalModes=%v want %v", got, want)
	}
}

func TestResolveLongmemWorkspaceIDPrefersExplicit(t *testing.T) {
	got, err := resolveLongmemWorkspaceID(config.Config{}, "", "ws-explicit")
	if err != nil {
		t.Fatalf("resolveLongmemWorkspaceID: %v", err)
	}
	if got != "ws-explicit" {
		t.Fatalf("got=%q want ws-explicit", got)
	}
}

func TestResolveLongmemWorkspaceIDDerivesFromPath(t *testing.T) {
	got, err := resolveLongmemWorkspaceID(config.Config{}, "/tmp/some workspace", "")
	if err != nil {
		t.Fatalf("resolveLongmemWorkspaceID: %v", err)
	}
	if strings.TrimSpace(got) == "" || got == "/tmp/some workspace" {
		t.Fatalf("workspace ID=%q not derived from path", got)
	}
}

func TestResolveLongmemWorkspaceIDRejectsEmpty(t *testing.T) {
	if _, err := resolveLongmemWorkspaceID(config.Config{}, "", ""); err == nil {
		t.Fatalf("expected error for empty workspace")
	}
}

func TestEvalLongmemCommand_RequiresDatasetForRetrieval(t *testing.T) {
	root := t.TempDir()
	workspaceID := "ws-cli"
	openMemory, openQueue := newEvalLongmemTestStores(t, root, workspaceID)
	cmd := newEvalLongmemCommandWithDeps(longmemeval.Deps{
		Now:       func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
		LoadCases: longmemeval.LoadCases,
	}, openMemory, openQueue)
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"--workspace-id", workspaceID, "--mode", "retrieval"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "dataset is required") {
		t.Fatalf("err=%v want dataset required", err)
	}
}

func TestEvalLongmemCommand_RetrievalOnlyWritesReportAndCases(t *testing.T) {
	root := t.TempDir()
	workspaceID := "ws-cli"
	openMemory, openQueue := newEvalLongmemTestStores(t, root, workspaceID)
	cmd := newEvalLongmemCommandWithDeps(longmemeval.Deps{
		Now: func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
	}, openMemory, openQueue)

	datasetPath := filepath.Join(root, "dataset.json")
	ingestEvalLongmemFixture(t, datasetPath, workspaceID, openMemory, openQueue)

	artifactDir := filepath.Join(root, "artifacts")
	out := &strings.Builder{}
	cmd.SetOut(out)
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{
		"--dataset", datasetPath,
		"--workspace-id", workspaceID,
		"--mode", "retrieval",
		"--artifact-dir", artifactDir,
		"--limit", "5",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	reportPath := filepath.Join(artifactDir, "report.json")
	body, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report longmemeval.RunResult
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Suite != "longmem" {
		t.Fatalf("report suite=%v", report.Suite)
	}
	if report.Metrics == nil || report.Metrics.HitAt5 != 1.0 || report.Metrics.MRR != 1.0 {
		t.Fatalf("report metrics=%+v want hit@5=1 mrr=1", report.Metrics)
	}
	if len(report.Cases) != 1 || len(report.Cases[0].MatchedNames) != 1 {
		t.Fatalf("report cases=%+v want one matched evidence", report.Cases)
	}
	entries, err := os.ReadDir(filepath.Join(artifactDir, "cases"))
	if err != nil {
		t.Fatalf("read cases dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no per-case files written")
	}
	headToHead, err := os.ReadFile(filepath.Join(artifactDir, "head-to-head.md"))
	if err != nil {
		t.Fatalf("read head-to-head report: %v", err)
	}
	if !strings.Contains(string(headToHead), "foxctl raw memory/query equivalent") || !strings.Contains(string(headToHead), "--mode retrieval") {
		t.Fatalf("head-to-head report missing retrieval command/table: %s", string(headToHead))
	}
	if !strings.Contains(out.String(), `"status":"ok"`) {
		t.Fatalf("stdout missing status:ok; out=%q", out.String())
	}
	if !strings.Contains(out.String(), `"command":"eval/longmem"`) {
		t.Fatalf("stdout missing command: %q", out.String())
	}
}

func TestEvalLongmemCommand_AnswerModeWritesReportAndCases(t *testing.T) {
	root := t.TempDir()
	workspaceID := "ws-cli"
	openMemory, openQueue := newEvalLongmemTestStores(t, root, workspaceID)
	cmd := newEvalLongmemCommandWithDeps(longmemeval.Deps{
		Now: func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
		RunAnswer: func(_ context.Context, req longmemeval.AnswerRequest) (longmemeval.AnswerResult, error) {
			expectedNames := longmemeval.ExpectedMemoryNames(nil, workspaceID, "case-cli", []string{"sharegpt_001"})
			return longmemeval.AnswerResult{
				Answer:        "The answer is decorated.",
				Method:        "fake-answer",
				ToolNames:     []string{"retrieve_memory"},
				EvidenceNames: expectedNames,
				EvidenceRefs:  []string{"memory_claim:" + expectedNames[0]},
				Iterations:    1,
				DurationMS:    11,
			}, nil
		},
	}, openMemory, openQueue)

	datasetPath := filepath.Join(root, "dataset.json")
	ingestEvalLongmemFixture(t, datasetPath, workspaceID, openMemory, openQueue)

	artifactDir := filepath.Join(root, "answer-artifacts")
	out := &strings.Builder{}
	cmd.SetOut(out)
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{
		"--dataset", datasetPath,
		"--workspace-id", workspaceID,
		"--mode", "answer",
		"--artifact-dir", artifactDir,
		"--limit", "5",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(artifactDir, "report.json"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report longmemeval.RunResult
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Metrics == nil || report.Metrics.AnswerAccuracy != 1 || report.Metrics.AnswerMeanScore != 1 {
		t.Fatalf("answer metrics=%+v", report.Metrics)
	}
	if len(report.Cases) != 1 || !report.Cases[0].AnswerMatched || len(report.Cases[0].AnswerMatchedEvidence) != 1 {
		t.Fatalf("answer cases=%+v", report.Cases)
	}
	if report.Cases[0].ExpectedAnswer != "decorated" || report.Cases[0].AnswerMethod != "fake-answer" {
		t.Fatalf("answer artifact fields=%+v", report.Cases[0])
	}
	caseBody, err := os.ReadFile(filepath.Join(artifactDir, "cases", "case-cli.json"))
	if err != nil {
		t.Fatalf("read answer case: %v", err)
	}
	if !strings.Contains(string(caseBody), `"answer_matched": true`) {
		t.Fatalf("case artifact missing answer fields: %s", string(caseBody))
	}
	if !strings.Contains(out.String(), `"command":"eval/longmem"`) {
		t.Fatalf("stdout missing command: %q", out.String())
	}
}

func TestEvalLongmemCommand_QueueStatusModeSurfacesStats(t *testing.T) {
	root := t.TempDir()
	workspaceID := "ws-cli"
	openMemory, openQueue := newEvalLongmemTestStores(t, root, workspaceID)
	cmd := newEvalLongmemCommandWithDeps(longmemeval.Deps{
		Now: func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
	}, openMemory, openQueue)

	out := &strings.Builder{}
	cmd.SetOut(out)
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"--workspace-id", workspaceID, "--mode", "queue-status"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out.String()), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v; out=%q", err, out.String())
	}
	data, _ := envelope["data"].(map[string]any)
	if data == nil {
		t.Fatalf("envelope data missing: %q", out.String())
	}
	result, _ := data["result"].(map[string]any)
	if result == nil {
		t.Fatalf("envelope result missing: %q", out.String())
	}
	queue, _ := result["queue_status"].(map[string]any)
	if queue == nil {
		t.Fatalf("queue_status missing; result keys=%v", mapKeys(result))
	}
	if queue["kind"] != string(embedqueue.TaskKindMemory) {
		t.Fatalf("queue kind=%v", queue["kind"])
	}
}

func TestEvalLongmemCommand_RejectsUnknownMode(t *testing.T) {
	root := t.TempDir()
	openMemory, openQueue := newEvalLongmemTestStores(t, root, "ws")
	cmd := newEvalLongmemCommandWithDeps(longmemeval.Deps{
		Now: func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
	}, openMemory, openQueue)
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"--workspace-id", "ws", "--mode", "judge"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown mode") {
		t.Fatalf("err=%v want unknown mode error", err)
	}
}

func TestResolveLongmemAnswerRuntimeUsesStrategyDefaults(t *testing.T) {
	got, err := resolveLongmemAnswerRuntime(longmemAnswerRuntimeInput{
		Strategy: "gather-memory",
	})
	if err != nil {
		t.Fatalf("resolveLongmemAnswerRuntime: %v", err)
	}
	if got.Strategy != longmemAnswerStrategyGatherMemory {
		t.Fatalf("strategy=%q", got.Strategy)
	}
	if got.RouteProfile != "memory_recall" {
		t.Fatalf("route=%q want memory_recall", got.RouteProfile)
	}
	if got.ToolProfile != "gather-context" {
		t.Fatalf("tool profile=%q want gather-context", got.ToolProfile)
	}

	got, err = resolveLongmemAnswerRuntime(longmemAnswerRuntimeInput{
		Strategy:           "gather-memory",
		ToolProfile:        "memory-recall",
		ToolProfileChanged: true,
	})
	if err != nil {
		t.Fatalf("resolveLongmemAnswerRuntime override: %v", err)
	}
	if got.ToolProfile != "memory-recall" {
		t.Fatalf("tool profile override=%q", got.ToolProfile)
	}
}

func TestResolveLongmemAnswerRuntimeRejectsUnknownStrategy(t *testing.T) {
	if _, err := resolveLongmemAnswerRuntime(longmemAnswerRuntimeInput{Strategy: "dag-grep"}); err == nil || !strings.Contains(err.Error(), "unknown longmem answer strategy") {
		t.Fatalf("err=%v want unknown strategy", err)
	}
}

func TestLongmemAnswerPromptReflectsStrategy(t *testing.T) {
	prompt := longmemAnswerPrompt("Where is the answer?", longmemAnswerStrategyGatherMemory)
	if !strings.Contains(prompt, `gather_memory_context`) || strings.Contains(prompt, "Use retrieve_memory before answering") {
		t.Fatalf("gather-memory prompt not strategy-specific:\n%s", prompt)
	}
	if !strings.Contains(prompt, "narrower required_evidence") {
		t.Fatalf("gather-memory prompt missing repair guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "evidence_digest claims and slots") {
		t.Fatalf("gather-memory prompt missing digest guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "aggregate_evidence_refs") {
		t.Fatalf("gather-memory prompt missing aggregate guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "evidence_ledger") || !strings.Contains(prompt, "accepted_rows") {
		t.Fatalf("gather-memory prompt missing ledger guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "max_tokens around 1200") {
		t.Fatalf("gather-memory prompt missing bounded verification guidance:\n%s", prompt)
	}
	prompt = longmemAnswerPrompt("Where is the answer?", longmemAnswerStrategyRetrieveMemory)
	if !strings.Contains(prompt, "Use retrieve_memory before answering") {
		t.Fatalf("retrieve-memory prompt missing retrieve guidance:\n%s", prompt)
	}
}

func TestLongmemAnswerToolRecorderCapturesGatherContextMemoryRefs(t *testing.T) {
	recorder := &longmemAnswerToolRecorder{
		next: staticLongmemAnswerToolExecutor{payload: map[string]any{
			"answer_seed": map[string]any{
				"facts": []any{
					map[string]any{
						"fact":      "The answer is decorated.",
						"load_refs": []any{"memory_claim:longmem://expected"},
					},
					map[string]any{
						"fact":      "The raw digest should canonicalize.",
						"load_refs": []any{"memory_claim:75c3adc8500a435a9d5a0c2a"},
					},
					map[string]any{
						"fact":      "Named memory refs should count as answer evidence too.",
						"load_refs": []any{"named_memory:1b9d49518b3ef36c7acc22a3"},
					},
				},
			},
			"path_set": map[string]any{
				"must": []any{
					map[string]any{
						"ref": map[string]any{
							"type": "memory_claim",
							"ref":  "longmem://also-expected",
						},
					},
					map[string]any{
						"ref": "memory_claim:longmemeval-s-pilot18-20260602172317/891d49518b3ef36c7acc22a3",
					},
					map[string]any{
						"ref": map[string]any{
							"type": "named_memory",
							"ref":  "longmem://named-typed",
						},
					},
				},
			},
		}},
	}
	if _, err := recorder.Execute(context.Background(), "gather_context", nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	names := recorder.evidenceNames()
	for _, want := range []string{"longmem://expected", "longmem://also-expected", "longmem://75c3adc8500a435a9d5a0c2a", "longmem://891d49518b3ef36c7acc22a3", "longmem://1b9d49518b3ef36c7acc22a3", "longmem://named-typed"} {
		if !longmemTestStringSliceContains(names, want) {
			t.Fatalf("names=%v missing %q", names, want)
		}
	}
	refs := recorder.evidenceRefs()
	for _, want := range []string{"memory_claim:longmem://expected", "memory_claim:longmem://also-expected", "memory_claim:longmem://75c3adc8500a435a9d5a0c2a", "memory_claim:longmem://891d49518b3ef36c7acc22a3", "named_memory:longmem://1b9d49518b3ef36c7acc22a3", "named_memory:longmem://named-typed"} {
		if !longmemTestStringSliceContains(refs, want) {
			t.Fatalf("refs=%v missing %q", refs, want)
		}
	}
}

func TestLongmemAnswerToolRecorderIgnoresRejectedLedgerRefs(t *testing.T) {
	recorder := &longmemAnswerToolRecorder{
		next: staticLongmemAnswerToolExecutor{payload: map[string]any{
			"accepted_refs": []any{"memory_claim:longmem://accepted-ref"},
			"accepted_rows": []any{
				map[string]any{"ref": "named_memory:longmem://accepted-row"},
			},
			"rejected_refs": []any{"memory_claim:longmem://rejected-ref"},
			"rejected_rows": []any{
				map[string]any{"ref": "named_memory:longmem://rejected-row"},
			},
		}},
	}
	if _, err := recorder.Execute(context.Background(), "evidence_ledger", nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	names := recorder.evidenceNames()
	for _, want := range []string{"longmem://accepted-ref", "longmem://accepted-row"} {
		if !longmemTestStringSliceContains(names, want) {
			t.Fatalf("names=%v missing %q", names, want)
		}
	}
	for _, rejected := range []string{"longmem://rejected-ref", "longmem://rejected-row"} {
		if longmemTestStringSliceContains(names, rejected) {
			t.Fatalf("names=%v should not include rejected %q", names, rejected)
		}
	}
	refs := recorder.evidenceRefs()
	for _, rejected := range []string{"memory_claim:longmem://rejected-ref", "named_memory:longmem://rejected-row"} {
		if longmemTestStringSliceContains(refs, rejected) {
			t.Fatalf("refs=%v should not include rejected %q", refs, rejected)
		}
	}
}

type staticLongmemAnswerToolExecutor struct {
	payload map[string]any
}

func (s staticLongmemAnswerToolExecutor) Execute(context.Context, string, json.RawMessage) (map[string]any, error) {
	return s.payload, nil
}

func longmemTestStringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func newEvalLongmemTestStores(t *testing.T, root, workspaceID string) (func(context.Context, string) (storage.MemoryStore, error), func(context.Context, string) (*embedding.Store, error)) {
	t.Helper()
	ctx := context.Background()
	memPath := filepath.Join(root, "memory")
	if err := os.MkdirAll(memPath, 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	queuePath := filepath.Join(root, "queue")
	if err := os.MkdirAll(queuePath, 0o755); err != nil {
		t.Fatalf("mkdir queue: %v", err)
	}
	openMemory := func(_ context.Context, _ string) (storage.MemoryStore, error) {
		return memstore.Open(ctx, memPath, "")
	}
	openQueue := func(_ context.Context, _ string) (*embedding.Store, error) {
		return embedding.OpenStore(ctx, queuePath)
	}
	if m, err := openMemory(ctx, workspaceID); err != nil {
		t.Fatalf("pre-open memory: %v", err)
	} else {
		_ = m.Close()
	}
	if q, err := openQueue(ctx, workspaceID); err != nil {
		t.Fatalf("pre-open queue: %v", err)
	} else {
		_ = q.Close()
	}
	return openMemory, openQueue
}

func ingestEvalLongmemFixture(
	t *testing.T,
	datasetPath, workspaceID string,
	openMemory func(context.Context, string) (storage.MemoryStore, error),
	openQueue func(context.Context, string) (*embedding.Store, error),
) {
	t.Helper()
	ctx := context.Background()
	store, err := openMemory(ctx, workspaceID)
	if err != nil {
		t.Fatalf("memory open: %v", err)
	}
	defer func() { _ = store.Close() }()
	queue, err := openQueue(ctx, workspaceID)
	if err != nil {
		t.Fatalf("queue open: %v", err)
	}
	defer func() { _ = queue.Close() }()

	cases := []longmemeval.Case{{
		QuestionID:         "case-cli",
		Question:           "which session mentions decorated material",
		Answer:             "decorated",
		AnswerSessionIDs:   []string{"sharegpt_001"},
		HaystackSessionIDs: []string{"sharegpt_001"},
		HaystackSessions: [][]longmemeval.Message{{
			{Role: "user", Content: "decorated content lives here"},
		}},
	}}
	body, err := json.Marshal(cases)
	if err != nil {
		t.Fatalf("marshal cases: %v", err)
	}
	if err := os.WriteFile(datasetPath, body, 0o644); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	plan, err := longmemeval.BuildPlan(cases, longmemeval.IngestOptions{
		WorkspaceID:    workspaceID,
		EmbeddingModel: "text-embedding-qwen3-embedding-8b",
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if _, err := longmemeval.ApplyPlan(ctx, store, queue, plan); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
