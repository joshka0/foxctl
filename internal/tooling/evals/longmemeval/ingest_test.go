package longmemeval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
)

func TestLoadCasesNormalizesScalarAnswers(t *testing.T) {
	path := t.TempDir() + "/cases.json"
	body := []byte(`[
		{"question_id":"string-answer","answer":"Business Administration","haystack_sessions":[]},
		{"question_id":"number-answer","answer":3,"haystack_sessions":[]},
		{"question_id":"bool-answer","answer":true,"haystack_sessions":[]},
		{"question_id":"null-answer","answer":null,"haystack_sessions":[]}
	]`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cases, err := LoadCases(path)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	got := []string{cases[0].Answer, cases[1].Answer, cases[2].Answer, cases[3].Answer}
	want := []string{"Business Administration", "3", "true", ""}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("answers=%v want %v", got, want)
	}
}

func TestBuildPlanKeepsAnswerAndEvidenceMetadataOutOfSemanticContent(t *testing.T) {
	cases := []Case{tinyCase()}

	plan, err := BuildPlan(cases, IngestOptions{
		WorkspaceID:    "ws-longmem",
		EmbeddingModel: "text-embedding-qwen3-embedding-8b",
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Records) != 1 {
		t.Fatalf("records=%d want 1", len(plan.Records))
	}
	if len(plan.Leakage) != 0 {
		t.Fatalf("unexpected leakage: %#v", plan.Leakage)
	}

	record := plan.Records[0]
	for field, text := range map[string]string{
		"summary":           record.Summary,
		"atomic_text":       record.AtomicText,
		"embedding_content": record.EmbeddingInput.Content,
		"entities":          strings.Join(record.Entities, " "),
		"keywords":          strings.Join(record.Keywords, " "),
		"name":              record.Name,
	} {
		assertNotContains(t, field, text, "Business Administration")
		assertNotContains(t, field, text, "answer_280352e9")
		assertNotContains(t, field, text, "e47becba")
		assertNotContains(t, field, text, "What degree did I graduate with?")
	}
	if !strings.Contains(strings.ToLower(record.AtomicText), "qwen") {
		t.Fatalf("atomic text=%q missing transcript content", record.AtomicText)
	}
}

func TestBuildPlanReportsAnswerLeakWhenAnswerNotInSourceTranscript(t *testing.T) {
	c := tinyCase()

	plan, err := BuildPlan([]Case{c}, IngestOptions{WorkspaceID: "ws-longmem"})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	record := plan.Records[0]
	record.Summary += " Expected answer: Business Administration"
	findings := CheckLeakage(c, record)
	if len(findings) == 0 {
		t.Fatalf("expected leakage finding")
	}
}

func TestBuildPlanAllowsAnswerTextWhenItComesFromSourceTranscript(t *testing.T) {
	c := tinyCase()
	c.HaystackSessions[0][0].Content = "I graduated with a Business Administration degree."

	plan, err := BuildPlan([]Case{c}, IngestOptions{WorkspaceID: "ws-longmem"})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Leakage) != 0 {
		t.Fatalf("natural transcript answer text should not be leakage: %#v", plan.Leakage)
	}
}

func TestBuildPlanKeepsLateSourceTurnsInAtomicText(t *testing.T) {
	c := tinyCase()
	c.HaystackSessions[0] = []Message{
		{Role: "user", Content: strings.Repeat("morning planning details ", 260)},
		{Role: "assistant", Content: "Those planning details are recorded."},
		{Role: "user", Content: "I graduated with a degree in Business Administration."},
	}

	plan, err := BuildPlan([]Case{c}, IngestOptions{WorkspaceID: "ws-longmem"})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Leakage) != 0 {
		t.Fatalf("natural transcript answer text should not be leakage: %#v", plan.Leakage)
	}
	// With per-turn-pair chunking, the late answer is in its own chunk.
	// Search all records for the answer text.
	found := false
	for _, record := range plan.Records {
		if strings.Contains(stripSessionMetadata(record.AtomicText), "Business Administration") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no chunk contains late source answer 'Business Administration'")
	}
	for _, record := range plan.Records {
		assertNotContains(t, "atomic text", record.AtomicText, "answer_280352e9")
		assertNotContains(t, "embedding content", record.EmbeddingInput.Content, "answer_280352e9")
	}
}

func TestCheckLeakageIgnoresLowInformationAnswerCollisionInSyntheticName(t *testing.T) {
	c := tinyCase()
	c.Answer = "3"

	rec := Record{
		CaseID:     c.QuestionID,
		SessionID:  "session-001",
		Name:       "longmem://abc123",
		Summary:    "safe summary",
		AtomicText: "safe atomic text",
	}
	if findings := CheckLeakage(c, rec); len(findings) != 0 {
		t.Fatalf("numeric answer collision in synthetic name should not be leakage: %#v", findings)
	}

	rec.Summary = "unsafe injected answer 3"
	findings := CheckLeakage(c, rec)
	if len(findings) == 0 {
		t.Fatalf("numeric answer in semantic content should still be leakage")
	}
	if findings[0].Field != "summary" || findings[0].Reason != "answer" {
		t.Fatalf("unexpected finding: %#v", findings[0])
	}
}

func TestApplyPlanRejectsLeakageBeforeSavingOrQueueing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	memory, err := memorystore.Open(ctx, root, "")
	if err != nil {
		t.Fatalf("memory open: %v", err)
	}
	t.Cleanup(func() { _ = memory.Close() })
	queue, err := embedding.OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("queue open: %v", err)
	}
	t.Cleanup(func() { _ = queue.Close() })

	plan, err := BuildPlan([]Case{tinyCase()}, IngestOptions{WorkspaceID: "ws-longmem"})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	plan.Leakage = []LeakageFinding{{CaseID: "e47becba", Field: "summary", Token: "Business Administration", Reason: "answer"}}

	result, err := ApplyPlan(ctx, memory, queue, plan)
	if !errors.Is(err, ErrLeakage) {
		t.Fatalf("ApplyPlan error=%v want ErrLeakage", err)
	}
	if result.Saved != 0 || result.Queued != 0 || result.Skipped != 0 {
		t.Fatalf("result=%#v want no writes", result)
	}
	if got, err := memory.Get(ctx, plan.Records[0].Name, "ws-longmem"); err == nil {
		t.Fatalf("unexpected saved memory: %#v", got)
	}
	if job, err := queue.ClaimNext(ctx); err != nil || job != nil {
		t.Fatalf("unexpected queued job=%#v err=%v", job, err)
	}
}

func TestApplyPlanIsIdempotentAndQueuesMemoryJobs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	memory, err := memorystore.Open(ctx, root, "")
	if err != nil {
		t.Fatalf("memory open: %v", err)
	}
	t.Cleanup(func() { _ = memory.Close() })
	queue, err := embedding.OpenStore(ctx, root)
	if err != nil {
		t.Fatalf("queue open: %v", err)
	}
	t.Cleanup(func() { _ = queue.Close() })

	plan, err := BuildPlan([]Case{tinyCase()}, IngestOptions{
		WorkspaceID:    "ws-longmem",
		EmbeddingModel: "text-embedding-qwen3-embedding-8b",
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	first, err := ApplyPlan(ctx, memory, queue, plan)
	if err != nil {
		t.Fatalf("ApplyPlan first: %v", err)
	}
	if first.Saved != 1 || first.Queued != 1 || first.Skipped != 0 {
		t.Fatalf("first result=%#v", first)
	}
	before, err := memory.Get(ctx, plan.Records[0].Name, "ws-longmem")
	if err != nil {
		t.Fatalf("memory get before second apply: %v", err)
	}
	second, err := ApplyPlan(ctx, memory, queue, plan)
	if err != nil {
		t.Fatalf("ApplyPlan second: %v", err)
	}
	if second.Saved != 0 || second.Queued != 0 || second.Skipped != 1 {
		t.Fatalf("second result=%#v", second)
	}
	after, err := memory.Get(ctx, plan.Records[0].Name, "ws-longmem")
	if err != nil {
		t.Fatalf("memory get after second apply: %v", err)
	}
	if before.ID != after.ID || !before.UpdatedAt.Equal(after.UpdatedAt) {
		t.Fatalf("second apply mutated entry: before=%#v after=%#v", before, after)
	}

	reembedPlan := plan
	reembedPlan.EmbeddingModel = "text-embedding-qwen3-embedding-8b-v2"
	third, err := ApplyPlan(ctx, memory, queue, reembedPlan)
	if err != nil {
		t.Fatalf("ApplyPlan third: %v", err)
	}
	if third.Saved != 0 || third.Queued != 1 || third.Skipped != 1 {
		t.Fatalf("third result=%#v", third)
	}
	reembedded, err := memory.Get(ctx, plan.Records[0].Name, "ws-longmem")
	if err != nil {
		t.Fatalf("memory get after third apply: %v", err)
	}
	if before.ID != reembedded.ID || !before.UpdatedAt.Equal(reembedded.UpdatedAt) {
		t.Fatalf("third apply mutated entry: before=%#v after=%#v", before, reembedded)
	}

	entry, err := memory.Get(ctx, plan.Records[0].Name, "ws-longmem")
	if err != nil {
		t.Fatalf("memory get: %v", err)
	}
	if !strings.Contains(strings.ToLower(entry.AtomicText), "qwen") {
		t.Fatalf("atomic text=%q missing transcript content", entry.AtomicText)
	}
	assertNotContains(t, "stored atomic text", entry.AtomicText, "Business Administration")
	assertNotContains(t, "stored result", string(entry.Result), "is_expected_evidence")
	assertNotContains(t, "stored result", string(entry.Result), "e47becba")
	assertNotContains(t, "stored result", string(entry.Result), "sharegpt_001")
	assertNotContains(t, "memory name", entry.Name, "e47becba")
	assertNotContains(t, "memory name", entry.Name, "sharegpt_001")

	for _, query := range []string{"e47becba", "answer_280352e9"} {
		hits, err := memory.Search(ctx, "ws-longmem", query, 10)
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		if len(hits) != 0 {
			t.Fatalf("Search(%q) returned leaked records: %#v", query, hits)
		}
	}

	jobsByModel := map[string]int{}
	for {
		job, err := queue.ClaimNext(ctx)
		if err != nil {
			t.Fatalf("ClaimNext: %v", err)
		}
		if job == nil {
			break
		}
		if job.Kind != "memory" || job.WorkspaceID != "ws-longmem" || job.MemoryName != plan.Records[0].Name {
			t.Fatalf("job=%#v", job)
		}
		jobsByModel[job.Model]++
		assertNotContains(t, "job content", job.Content, "Business Administration")
		assertNotContains(t, "job content", job.Content, "answer_280352e9")
	}
	if jobsByModel["text-embedding-qwen3-embedding-8b"] != 1 {
		t.Fatalf("jobs by model=%v missing original model job", jobsByModel)
	}
	if jobsByModel["text-embedding-qwen3-embedding-8b-v2"] != 1 {
		t.Fatalf("jobs by model=%v missing reembed model job", jobsByModel)
	}
}

func TestResultEnvelopeKeepsProvenanceOutsideSemanticFields(t *testing.T) {
	plan, err := BuildPlan([]Case{tinyCase()}, IngestOptions{WorkspaceID: "ws-longmem"})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(plan.Records[0].Result, &envelope); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	data := envelope["data"].(map[string]any)
	if len(data) != 0 {
		t.Fatalf("stored result data must not contain eval identifiers: %v", data)
	}
	if _, ok := data["answer"]; ok {
		t.Fatalf("result data must not persist expected answer: %v", data)
	}
}

func tinyCase() Case {
	return Case{
		QuestionID:         "e47becba",
		QuestionType:       "single-session-user",
		Question:           "What degree did I graduate with?",
		QuestionDate:       "2023/05/30 (Tue) 23:40",
		Answer:             "Business Administration",
		AnswerSessionIDs:   []string{"answer_280352e9"},
		HaystackDates:      []string{"2023/05/20 (Sat) 02:21"},
		HaystackSessionIDs: []string{"sharegpt_001"},
		HaystackSessions: [][]Message{{
			{Role: "user", Content: "Please remember that local Qwen embeddings power this memory recall check."},
			{Role: "assistant", Content: "I will use the Qwen embedder when the queue drains named memories."},
		}},
	}
}

func assertNotContains(t *testing.T, field, text, token string) {
	t.Helper()
	if strings.Contains(strings.ToLower(text), strings.ToLower(token)) {
		t.Fatalf("%s leaked %q in %q", field, token, text)
	}
}
