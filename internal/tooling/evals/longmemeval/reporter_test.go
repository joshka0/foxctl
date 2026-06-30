package longmemeval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	if m.MRR < 0.49 || m.MRR > 0.51 {
		t.Fatalf("mrr=%v want ~0.5", m.MRR)
	}
	if m.MeanLatencyMS < 19 || m.MeanLatencyMS > 21 {
		t.Fatalf("latency=%v want ~20", m.MeanLatencyMS)
	}
}

func TestSummarizeEmptyCases(t *testing.T) {
	m := Summarize(nil)
	if m.CaseCount != 0 || m.FailureCount != 0 {
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
		DatasetPath: "longmem.json",
		ArtifactDir: "artifacts",
		Limit:       5,
		GeneratedAt: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
		Modes:       []string{"retrieval"},
		Cases:       []CaseResult{{CaseID: "case-1", HitAt5: true, ReciprocalRank: 1}},
		Metrics:     &Metrics{CaseCount: 1, HitAt5: 1, MRR: 1, MeanLatencyMS: 4},
	}
	md := RenderHeadToHeadMarkdown(result)
	for _, want := range []string{
		"| foxctl raw memory/query equivalent | run | hit@5 1.000",
		"| foxctl answer-mode | not run | unavailable | unavailable",
		"| HydraDB baseline | not run | unavailable | unavailable",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("RenderHeadToHeadMarkdown missing %q:\n%s", want, md)
		}
	}
}
