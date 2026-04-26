package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
)

func TestHelperFactoryToolsDraftsRunsAndReturnsAnswer(t *testing.T) {
	t.Parallel()

	source := `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": "solution = helper"}
}`
	server := helperFactoryTestServer(t, []string{mustHelperFactoryDraftJSON(t, source, nil)}, nil)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt:          "Return solution = helper.",
		Attempts:            2,
		ExtractSolutionLine: true,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = helper" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
}

func TestHelperFactoryAnswerRejectsExplicitOKFalse(t *testing.T) {
	t.Parallel()

	answer, ok := helperFactoryAnswer(map[string]any{
		"ok":     false,
		"answer": "no solution",
	}, false)
	if ok || answer != "" {
		t.Fatalf("helperFactoryAnswer() answer=%q ok=%v, want rejected", answer, ok)
	}
}

func TestHelperFactoryToolsDraftsPythonRunsAndReturnsAnswer(t *testing.T) {
	t.Parallel()

	source := `def solve(input):
    return {"ok": True, "answer": "solution = " + str(input["value"])}`
	var prompts []string
	server := helperFactoryTestServer(t, []string{mustHelperFactoryDraftJSON(t, source, map[string]any{"value": 42})}, &prompts)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt:          "Return solution = value.",
		Attempts:            1,
		ExtractSolutionLine: true,
		Language:            HelperLanguagePython,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = 42" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "short-lived Python helper") || !strings.Contains(prompts[0], "source_b64") {
		t.Fatalf("expected Python helper prompt, got %#v", prompts)
	}
}

func TestHelperFactoryToolsAcceptsSourceB64Draft(t *testing.T) {
	t.Parallel()

	source := `def solve(input):
    return {"ok": True, "answer": "solution = b64"}`
	server := helperFactoryTestServer(t, []string{mustHelperFactoryDraftB64JSON(t, source, nil)}, nil)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt:          "Return solution = b64.",
		Attempts:            1,
		ExtractSolutionLine: true,
		Language:            HelperLanguagePython,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = b64" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
}

func TestHelperFactoryToolsTreatsRawSourceB64AsSource(t *testing.T) {
	t.Parallel()

	source := `def solve(input):
    return {"ok": True, "answer": "solution = raw-source-b64"}`
	body, err := json.Marshal(map[string]any{
		"source_b64": source,
	})
	if err != nil {
		t.Fatalf("marshal draft: %v", err)
	}
	var draft helperFactoryDraft
	if err := decodeHelperFactoryDraft(string(body), &draft); err != nil {
		t.Fatalf("decode raw source_b64: %v", err)
	}
	if draft.Source != source {
		t.Fatalf("source = %q, want %q", draft.Source, source)
	}
}

func TestHelperFactoryToolsExtractsSourceB64FromMalformedDraft(t *testing.T) {
	t.Parallel()

	source := `def solve(input):
    return {"ok": True, "answer": "solution = extracted"}`
	malformed := `prefix {"source_b64":"` + base64.StdEncoding.EncodeToString([]byte(source))
	var draft helperFactoryDraft
	if err := decodeHelperFactoryDraft(malformed, &draft); err != nil {
		t.Fatalf("decode malformed source_b64: %v", err)
	}
	if draft.Source != source {
		t.Fatalf("source = %q, want %q", draft.Source, source)
	}
}

func TestHelperFactoryToolsRepairsMalformedDraftOutput(t *testing.T) {
	t.Parallel()

	malformed := "{\"source_b64\":\"def solve(input):\n    return {\"ok\": True, \"answer\": \"solution = broken\"}\""
	goodSource := `def solve(input):
    return {"ok": True, "answer": "solution = fixed"}`
	var prompts []string
	server := helperFactoryTestServer(t, []string{
		malformed,
		mustHelperFactoryDraftB64JSON(t, goodSource, nil),
	}, &prompts)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt:          "SECRET_TASK_CONTEXT_SHOULD_NOT_APPEAR. Return solution = fixed.",
		Attempts:            2,
		ExtractSolutionLine: true,
		Language:            HelperLanguagePython,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = fixed" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
	if len(prompts) != 2 {
		t.Fatalf("prompts=%d", len(prompts))
	}
	repairPrompt := prompts[1]
	for _, want := range []string{
		"Repair a failed helper source file",
		"Language: python",
		"Malformed draft/output to fix",
		"Interpret malformed draft text generously",
		"def solve(input):",
	} {
		if !strings.Contains(repairPrompt, want) {
			t.Fatalf("repair prompt missing %q:\n%s", want, repairPrompt)
		}
	}
	if strings.Contains(repairPrompt, "SECRET_TASK_CONTEXT_SHOULD_NOT_APPEAR") || strings.Contains(repairPrompt, "Task:") {
		t.Fatalf("repair prompt leaked task context:\n%s", repairPrompt)
	}
}

func TestHelperFactoryToolsRetriesValidationFeedback(t *testing.T) {
	t.Parallel()

	badSource := `func NotSolve(input map[string]any) map[string]any { return map[string]any{"answer":"bad"} }`
	goodSource := `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": "solution = repaired"}
}`
	var prompts []string
	server := helperFactoryTestServer(t, []string{
		mustHelperFactoryDraftJSON(t, badSource, nil),
		mustHelperFactoryDraftJSON(t, goodSource, nil),
	}, &prompts)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt:          "Return solution = repaired.",
		Attempts:            2,
		ExtractSolutionLine: true,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK       bool             `json:"ok"`
		Answer   string           `json:"answer"`
		Attempts []map[string]any `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = repaired" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
	if len(got.Attempts) != 2 {
		t.Fatalf("attempts=%d raw=%s", len(got.Attempts), raw)
	}
	if len(prompts) < 2 || !strings.Contains(prompts[1], "ephemeral Go skill source must define Solve") {
		t.Fatalf("second prompt did not include validation feedback: %#v", prompts)
	}
	if !strings.Contains(prompts[1], "Invalid source to repair") || !strings.Contains(prompts[1], "func NotSolve") {
		t.Fatalf("second prompt did not include previous source context: %#v", prompts[1])
	}
	if strings.Contains(prompts[1], "Return solution = repaired.") || strings.Contains(prompts[1], "Task:") {
		t.Fatalf("repair prompt should not include task context: %s", prompts[1])
	}
	if strings.Contains(prompts[0], "{ ... }") {
		t.Fatalf("draft prompt includes ellipsis placeholder likely to be copied: %s", prompts[0])
	}
}

func TestHelperFactoryToolsRejectsOversizedSourceAndRepairs(t *testing.T) {
	t.Parallel()

	longSource := `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": "` + strings.Repeat("x", 140) + `"}
}`
	goodSource := `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": "solution = compact"}
}`
	var prompts []string
	server := helperFactoryTestServer(t, []string{
		mustHelperFactoryDraftJSON(t, longSource, nil),
		mustHelperFactoryDraftJSON(t, goodSource, nil),
	}, &prompts)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 256,
		},
		TaskPrompt:          "Return solution = compact.",
		Attempts:            2,
		ExtractSolutionLine: true,
		MaxSourceChars:      120,
		MaxSourceLines:      6,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK            bool             `json:"ok"`
		Answer        string           `json:"answer"`
		Attempts      []map[string]any `json:"attempts"`
		CandidateBeam []map[string]any `json:"candidate_beam"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = compact" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
	if len(got.Attempts) != 2 {
		t.Fatalf("attempts=%d raw=%s", len(got.Attempts), raw)
	}
	if len(prompts) < 2 || !strings.Contains(prompts[0], "at most 6 non-empty lines and 120 characters") {
		t.Fatalf("draft prompt missing source budget: %#v", prompts)
	}
	if !strings.Contains(prompts[1], "max chars") {
		t.Fatalf("repair prompt missing budget failure: %s", prompts[1])
	}
	for _, want := range []string{"Repair policy", "minimal patch", "previous source exceeded the source budget"} {
		if !strings.Contains(prompts[1], want) {
			t.Fatalf("repair prompt missing %q:\n%s", want, prompts[1])
		}
	}
}

func TestHelperFactoryToolsPerCallMaxAttemptsOnlyReducesBudget(t *testing.T) {
	t.Parallel()

	badSource := `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": "` + strings.Repeat("x", 140) + `"}
}`
	goodSource := `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": "solution = compact"}
}`
	var prompts []string
	server := helperFactoryTestServer(t, []string{
		mustHelperFactoryDraftJSON(t, badSource, nil),
		mustHelperFactoryDraftJSON(t, goodSource, nil),
	}, &prompts)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 256,
		},
		TaskPrompt:          "Return solution = compact.",
		Attempts:            3,
		ExtractSolutionLine: true,
		MaxSourceChars:      120,
		MaxSourceLines:      6,
	}}
	args := json.RawMessage(`{"max_attempts":1}`)
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK       bool             `json:"ok"`
		Error    string           `json:"error"`
		Attempts []map[string]any `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if got.OK {
		t.Fatalf("expected capped helper to fail, raw=%s", raw)
	}
	if !strings.Contains(got.Error, "failed after 1 attempts") {
		t.Fatalf("unexpected error %q raw=%s", got.Error, raw)
	}
	if len(got.Attempts) != 1 || len(prompts) != 1 {
		t.Fatalf("attempts=%d prompts=%d raw=%s", len(got.Attempts), len(prompts), raw)
	}
}

func TestHelperFactoryToolsRepairsPythonSourceWithoutTaskContext(t *testing.T) {
	t.Parallel()

	badSource := `def solve(input):
    return`
	goodSource := `def solve(input):
    return {"ok": True, "answer": "solution = repaired"}`
	var prompts []string
	server := helperFactoryTestServer(t, []string{
		mustHelperFactoryDraftJSON(t, badSource, nil),
		mustHelperFactoryDraftJSON(t, goodSource, nil),
	}, &prompts)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt:          "SECRET_TASK_CONTEXT_SHOULD_NOT_APPEAR. Return solution = repaired.",
		Attempts:            2,
		ExtractSolutionLine: true,
		Language:            HelperLanguagePython,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = repaired" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
	if len(prompts) != 2 {
		t.Fatalf("prompts=%d", len(prompts))
	}
	repairPrompt := prompts[1]
	for _, want := range []string{
		"Repair a failed helper source file",
		"Language: python",
		"Invalid source to repair",
		"def solve(input):",
	} {
		if !strings.Contains(repairPrompt, want) {
			t.Fatalf("repair prompt missing %q:\n%s", want, repairPrompt)
		}
	}
	if strings.Contains(repairPrompt, "SECRET_TASK_CONTEXT_SHOULD_NOT_APPEAR") || strings.Contains(repairPrompt, "Task:") {
		t.Fatalf("repair prompt leaked task context:\n%s", repairPrompt)
	}
}

func TestHelperFactoryToolsRepairsVerifierCounterexample(t *testing.T) {
	t.Parallel()

	badSource := `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": "solution = bad"}
}`
	goodSource := `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": "solution = fixed"}
}`
	var prompts []string
	server := helperFactoryTestServer(t, []string{
		mustHelperFactoryDraftJSON(t, badSource, nil),
		mustHelperFactoryDraftJSON(t, goodSource, nil),
	}, &prompts)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt:          "Return solution = fixed.",
		Attempts:            2,
		ExtractSolutionLine: true,
		AnswerVerifier: func(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
			if answer == "solution = fixed" {
				return HelperVerifierDiagnostic{Pass: true, FailedAtStep: -1}, true
			}
			return HelperVerifierDiagnostic{
				Pass:         false,
				FailedAtStep: 0,
				FailureKind:  "unit_counterexample",
				Message:      "bad answer",
				RepairHint:   "return the fixed answer",
			}, true
		},
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK            bool             `json:"ok"`
		Answer        string           `json:"answer"`
		Attempts      []map[string]any `json:"attempts"`
		CandidateBeam []map[string]any `json:"candidate_beam"`
		RepairHarness map[string]any   `json:"repair_harness"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = fixed" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
	if len(got.Attempts) != 2 || got.Attempts[0]["stage"] != "verify" {
		t.Fatalf("attempts=%#v", got.Attempts)
	}
	if len(got.CandidateBeam) != 1 || got.CandidateBeam[0]["answer"] != "solution = bad" {
		t.Fatalf("candidate beam=%#v", got.CandidateBeam)
	}
	if got.RepairHarness["kind"] != CounterexampleRepairKind || got.RepairHarness["candidate_count"] != float64(1) {
		t.Fatalf("repair harness telemetry=%#v", got.RepairHarness)
	}
	if !strings.Contains(prompts[1], "Verifier counterexample") || !strings.Contains(prompts[1], "unit_counterexample") {
		t.Fatalf("repair prompt missing verifier counterexample:\n%s", prompts[1])
	}
}

func TestCounterexampleRepairHarnessRecordsStructuredDiagnostic(t *testing.T) {
	t.Parallel()

	harness := NewCounterexampleRepairHarness(2)
	feedback := harness.RecordVerifierFailure(3, "solution = candidate", map[string]any{
		"failure_kind": "node_mismatch",
		"failed_node":  "n7",
		"observed":     "candidate value",
		"expected":     "verified value",
		"repair_hint":  "repair the failed node dependency",
		"score":        float64(0.4),
	})
	counterexample, ok := feedback["counterexample"].(map[string]any)
	if !ok || counterexample["failed_node"] != "n7" || counterexample["observed"] != "candidate value" || counterexample["expected"] != "verified value" {
		t.Fatalf("feedback missing structured counterexample: %#v", feedback)
	}
	telemetry := harness.Telemetry()
	if telemetry["kind"] != CounterexampleRepairKind || telemetry["candidate_count"] != 1 {
		t.Fatalf("telemetry=%#v", telemetry)
	}
	latest, ok := telemetry["latest_counterexample"].(map[string]any)
	if !ok || latest["failed_node"] != "n7" {
		t.Fatalf("telemetry missing latest counterexample: %#v", telemetry)
	}
}

func TestHelperVerifierDiagnosticMapPromotesKnownExtraCounterexampleFields(t *testing.T) {
	t.Parallel()

	diagMap := helperVerifierDiagnosticMap(HelperVerifierDiagnostic{
		Pass:        false,
		FailureKind: "graph_check",
		Extra: map[string]any{
			"failed_step":    0,
			"failed_node":    "edge-2",
			"observed":       []any{float64(1), float64(2)},
			"expected":       []any{float64(1), float64(3)},
			"benchmark_case": "must not become repair contract",
		},
	})
	counterexample := helperFactoryCounterexamplePacket(diagMap)
	if counterexample["failed_step"] != 0 || counterexample["failed_node"] != "edge-2" {
		t.Fatalf("counterexample=%#v diag=%#v", counterexample, diagMap)
	}
	if _, exists := diagMap["benchmark_case"]; exists {
		t.Fatalf("unknown diagnostic extra leaked into structured repair fields: %#v", diagMap)
	}
	if _, exists := diagMap["extra"]; exists {
		t.Fatalf("nested extra leaked into diagnostic map: %#v", diagMap)
	}
}

func TestHelperFactoryVerifierFeedbackKeepsBestCandidate(t *testing.T) {
	t.Parallel()

	current := map[string]any{
		"attempt": float64(1),
		"answer":  "solution = bad",
		"diagnostic": map[string]any{
			"score": float64(0.2),
		},
	}
	next := map[string]any{
		"attempt": float64(2),
		"answer":  "solution = better",
		"diagnostic": map[string]any{
			"score":          float64(0.7),
			"failure_kind":   "goal_mismatch",
			"failed_at_step": float64(4),
			"repair_hint":    "repair from failed step",
		},
	}
	best := bestHelperFactoryVerifierCandidate(current, next)
	feedback := helperFactoryVerifierFeedbackMap(next["diagnostic"].(map[string]any), best, []map[string]any{current, next}, 3)
	candidate, ok := feedback["best_candidate"].(map[string]any)
	if !ok || candidate["answer"] != "solution = better" {
		t.Fatalf("feedback=%#v", feedback)
	}
	counterexample, ok := feedback["counterexample"].(map[string]any)
	if !ok || counterexample["failure_kind"] != "goal_mismatch" {
		t.Fatalf("feedback missing counterexample: %#v", feedback)
	}
	frontier, ok := feedback["candidate_frontier"].([]map[string]any)
	if !ok || len(frontier) != 2 || frontier[0]["answer"] != "solution = better" {
		t.Fatalf("feedback frontier=%#v", feedback["candidate_frontier"])
	}
	if feedback["search_policy"] == nil {
		t.Fatalf("feedback missing search policy: %#v", feedback)
	}
	summary := helperFactoryVerifierSummary(feedback)
	for _, want := range []string{"best_candidate", "solution = better", "search_policy", "counterexample", "candidate_frontier"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestHelperFactoryFinalizeFeedbackConvertsFailureOutputToCounterexample(t *testing.T) {
	t.Parallel()

	feedback := helperFactoryFinalizeFeedbackMap(map[string]any{
		"ok":            false,
		"status":        "blocked",
		"first_failure": "could not clear block 44",
		"repair_hint":   "move blockers to a buffer first",
	})
	counterexample, ok := feedback["counterexample"].(map[string]any)
	if !ok || counterexample["first_failure"] != "could not clear block 44" {
		t.Fatalf("feedback missing counterexample: %#v", feedback)
	}
	policy, ok := feedback["search_policy"].(map[string]any)
	if !ok || policy["kind"] != "self_reported_failure_repair" {
		t.Fatalf("feedback missing search policy: %#v", feedback)
	}
	summary := helperFactoryVerifierSummary(feedback)
	for _, want := range []string{"counterexample", "first_failure", "search_policy"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestHelperFactoryFinalizeFeedbackSynthesizesCounterexampleForBareOKFalse(t *testing.T) {
	t.Parallel()

	feedback := helperFactoryFinalizeFeedbackMap(map[string]any{"ok": false, "answer": nil})
	counterexample, ok := feedback["counterexample"].(map[string]any)
	if !ok || counterexample["failure_kind"] != "unqualified_helper_failure" {
		t.Fatalf("feedback missing synthesized counterexample: %#v", feedback)
	}
	if !strings.Contains(fmt.Sprint(counterexample["repair_hint"]), "first_failure") {
		t.Fatalf("counterexample missing repair hint: %#v", counterexample)
	}
}

func TestHelperFactoryVerifierSummaryCompactsLargeCandidates(t *testing.T) {
	t.Parallel()

	hugeAnswer := "solution = " + strings.Repeat("[1,2,0],", 5000)
	feedback := helperFactoryVerifierFeedbackMap(map[string]any{
		"answer": hugeAnswer,
		"diagnostic": map[string]any{
			"failure_kind":   "same_stack",
			"failed_at_step": float64(1200),
			"valid_prefix":   []any{[]any{float64(1), float64(0), float64(2)}},
			"state_before":   []any{[]any{float64(1), float64(2)}, []any{}, []any{float64(3)}},
			"message":        strings.Repeat("bad ", 1000),
		},
	}, map[string]any{
		"answer": hugeAnswer,
		"diagnostic": map[string]any{
			"score": float64(0.7),
		},
	}, nil, 3)
	summary := helperFactoryVerifierSummary(feedback)
	if len(summary) > 4200 {
		t.Fatalf("summary len=%d, want compact summary: %s", len(summary), summary)
	}
	for _, want := range []string{"same_stack", "failed_at_step", "json_hash", "best_candidate"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
	if strings.Count(summary, "[1,2,0]") > 5 {
		t.Fatalf("summary retained too much candidate body: %s", summary)
	}
}

func TestCompactHelperFactoryMapSummarizesStructuredSolution(t *testing.T) {
	t.Parallel()

	solution := make([]any, 0, 1000)
	for i := 0; i < 1000; i++ {
		solution = append(solution, map[string]any{
			"block": float64(i),
			"from":  float64(0),
			"to":    float64(1),
		})
	}
	summary := compactHelperFactoryMap(map[string]any{
		"ok":       true,
		"solution": solution,
	})
	if _, exists := summary["solution"]; exists {
		t.Fatalf("summary inlined structured solution: %#v", summary["solution"])
	}
	solutionSummary, ok := summary["solution_summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing solution summary: %#v", summary)
	}
	if solutionSummary["count"] != 1000 {
		t.Fatalf("solution count=%#v, want 1000", solutionSummary["count"])
	}
}

func TestHelperFactoryAnswerAcceptsNestedSolutionObject(t *testing.T) {
	t.Parallel()

	answer, ok := helperFactoryAnswer(map[string]any{
		"ok": true,
		"answer": map[string]any{
			"solution": []any{
				[]any{float64(1), float64(0), float64(2)},
				[]any{float64(3), float64(2), float64(1)},
			},
		},
	}, true)
	if !ok {
		t.Fatalf("expected nested solution to be accepted")
	}
	if answer != "solution = [[1,0,2],[3,2,1]]" {
		t.Fatalf("answer=%q", answer)
	}
}

func TestHelperFactoryAnswerAcceptsStructuredAnswerWhenSolutionLineRequired(t *testing.T) {
	t.Parallel()

	answer, ok := helperFactoryAnswer(map[string]any{
		"ok": true,
		"answer": []any{
			[]any{float64(1), float64(0), float64(2)},
		},
	}, true)
	if !ok {
		t.Fatalf("expected structured answer to be accepted")
	}
	if answer != "solution = [[1,0,2]]" {
		t.Fatalf("answer=%q", answer)
	}
}

func TestHelperFactoryAnswerCanonicalizesObjectMoveSolution(t *testing.T) {
	t.Parallel()

	answer, ok := helperFactoryAnswer(map[string]any{
		"ok": true,
		"solution": []any{
			map[string]any{"block": float64(2), "from": float64(1), "to": float64(0)},
			map[string]any{"move": float64(3), "from_stack": float64(0), "to_stack": float64(2)},
		},
	}, true)
	if !ok {
		t.Fatalf("expected object move solution to be accepted")
	}
	if answer != "solution = [[2,1,0],[3,0,2]]" {
		t.Fatalf("answer=%q", answer)
	}
}

func TestStackTransitionPlannerPresetSolvesAndVerifiesSample(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"initial_state": []any{[]any{float64(0)}, []any{float64(1), float64(2)}, []any{}},
		"goal_state":    []any{[]any{}, []any{float64(1)}, []any{float64(2), float64(0)}},
	}
	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		Attempts:            1,
		TaskPrompt:          "Solve the typed stack transition task.",
		ExtractSolutionLine: true,
		PresetName:          BraidScaffoldClassFiniteStateTransition + "/" + BraidScaffoldIDStackRelocationV1,
		PresetSource:        stackTransitionPlannerPresetSource(),
		PresetInput:         input,
		MaxSourceLines:      380,
		MaxSourceChars:      18000,
		AnswerVerifier:      stackMoveAnswerVerifier,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
		Preset string `json:"preset"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Preset != BraidScaffoldClassFiniteStateTransition+"/"+BraidScaffoldIDStackRelocationV1 {
		t.Fatalf("output ok=%v preset=%q raw=%s", got.OK, got.Preset, raw)
	}
	if ok, detail, applicable := verifyStackMoveCandidateFromInput(got.Answer, input); !applicable || !ok {
		t.Fatalf("preset answer failed verifier applicable=%v detail=%q answer=%s", applicable, detail, got.Answer)
	}
}

func TestGridResourcePathPresetSolvesAndVerifiesSample(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"task_type":      BraidScaffoldClassGraphSearch,
		"scaffold_class": BraidScaffoldClassGraphSearch,
		"scaffold_id":    BraidScaffoldIDResourcePathMinInitialV1,
		"grid_layout": []any{
			[]any{float64(0), float64(1), float64(-2)},
			[]any{float64(-5), float64(-1), float64(0)},
			[]any{float64(1), float64(-2), float64(-1)},
		},
	}
	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		Attempts:            1,
		TaskPrompt:          "Solve the typed graph search resource path task.",
		ExtractSolutionLine: true,
		PresetName:          BraidScaffoldClassGraphSearch + "/" + BraidScaffoldIDResourcePathMinInitialV1,
		PresetSource:        gridResourcePathPresetSource(),
		PresetInput:         input,
		MaxSourceLines:      180,
		MaxSourceChars:      9000,
		AnswerVerifier:      gridResourcePathAnswerVerifier,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
		Preset string `json:"preset"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Preset != BraidScaffoldClassGraphSearch+"/"+BraidScaffoldIDResourcePathMinInitialV1 {
		t.Fatalf("output ok=%v preset=%q raw=%s", got.OK, got.Preset, raw)
	}
	if got.Answer != "solution = 2" {
		t.Fatalf("answer=%q", got.Answer)
	}
	if diag, applicable := gridResourcePathAnswerVerifier(got.Answer, input); !applicable || !diag.Pass {
		t.Fatalf("preset answer failed verifier applicable=%v diag=%#v", applicable, diag)
	}
}

func TestExplicitShortestPathPresetSolvesAndVerifiesSample(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"task_type":      BraidScaffoldClassGraphSearch,
		"scaffold_class": BraidScaffoldClassGraphSearch,
		"scaffold_id":    BraidScaffoldIDExplicitShortestPathV1,
		"nodes":          []any{"A", "B", "C", "D", "E"},
		"edges":          []any{[]any{"A", "B"}, []any{"B", "D"}, []any{"A", "C"}, []any{"C", "D"}},
		"start_node":     "A",
		"goal_node":      "D",
		"objective":      "shortest_path_length",
	}
	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		Attempts:            1,
		TaskPrompt:          "Solve the typed graph search shortest-path task.",
		ExtractSolutionLine: true,
		PresetName:          BraidScaffoldClassGraphSearch + "/" + BraidScaffoldIDExplicitShortestPathV1,
		PresetSource:        explicitShortestPathPresetSource(),
		PresetInput:         input,
		MaxSourceLines:      220,
		MaxSourceChars:      11000,
		AnswerVerifier:      explicitShortestPathAnswerVerifier,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
		Preset string `json:"preset"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Preset != BraidScaffoldClassGraphSearch+"/"+BraidScaffoldIDExplicitShortestPathV1 {
		t.Fatalf("output ok=%v preset=%q raw=%s", got.OK, got.Preset, raw)
	}
	if got.Answer != "solution = 2" {
		t.Fatalf("answer=%q", got.Answer)
	}
	if diag, applicable := explicitShortestPathAnswerVerifier(got.Answer, input); !applicable || !diag.Pass {
		t.Fatalf("preset answer failed verifier applicable=%v diag=%#v", applicable, diag)
	}
}

func TestFiniteDomainConstraintPresetSolvesAndVerifiesSample(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"task_type":      BraidScaffoldClassConstraintSolver,
		"scaffold_class": BraidScaffoldClassConstraintSolver,
		"scaffold_id":    BraidScaffoldIDFiniteDomainV1,
		"variables": []any{
			map[string]any{"name": "x", "min": float64(0), "max": float64(5)},
			map[string]any{"name": "y", "min": float64(0), "max": float64(5)},
		},
		"known_values": map[string]any{"target": float64(5)},
		"constraints": []any{
			map[string]any{
				"name": "sum",
				"op":   "eq",
				"left": map[string]any{
					"op": "add",
					"args": []any{
						map[string]any{"var": "x"},
						map[string]any{"var": "y"},
					},
				},
				"right": map[string]any{"known": "target"},
			},
			map[string]any{
				"name":  "x_fixed",
				"op":    "eq",
				"left":  map[string]any{"var": "x"},
				"right": map[string]any{"const": float64(2)},
			},
		},
		"requested_outputs": []any{"x", "y"},
	}
	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		Attempts:            1,
		TaskPrompt:          "Solve the typed finite-domain constraint task.",
		ExtractSolutionLine: true,
		PresetName:          BraidScaffoldClassConstraintSolver + "/" + BraidScaffoldIDFiniteDomainV1,
		PresetSource:        finiteDomainConstraintPresetSource(),
		PresetInput:         input,
		MaxSourceLines:      340,
		MaxSourceChars:      18000,
		AnswerVerifier:      finiteDomainAnswerVerifier,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
		Preset string `json:"preset"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Preset != BraidScaffoldClassConstraintSolver+"/"+BraidScaffoldIDFiniteDomainV1 {
		t.Fatalf("output ok=%v preset=%q raw=%s", got.OK, got.Preset, raw)
	}
	if got.Answer != `solution = {"x":2,"y":3}` {
		t.Fatalf("answer=%q", got.Answer)
	}
	if diag, applicable := finiteDomainAnswerVerifier(got.Answer, input); !applicable || !diag.Pass {
		t.Fatalf("preset answer failed verifier applicable=%v diag=%#v", applicable, diag)
	}
}

func TestNumericDPPresetSolvesAndVerifiesMinRecurrenceSample(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"task_type":      BraidScaffoldClassNumericDP,
		"scaffold_class": BraidScaffoldClassNumericDP,
		"scaffold_id":    BraidScaffoldIDRecurrenceTableV1,
		"objective":      "min",
		"dp_dimensions":  []any{float64(5)},
		"target":         []any{float64(4)},
		"base_cases": []any{
			map[string]any{"index": []any{float64(0)}, "value": float64(0)},
		},
		"transitions": []any{
			map[string]any{"offset": []any{float64(-1)}, "weight": float64(2)},
			map[string]any{"offset": []any{float64(-2)}, "weight": float64(3)},
		},
	}
	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		Attempts:            1,
		TaskPrompt:          "Solve the typed numeric dynamic-programming task.",
		ExtractSolutionLine: true,
		PresetName:          BraidScaffoldClassNumericDP + "/" + BraidScaffoldIDRecurrenceTableV1,
		PresetSource:        numericDPTablePresetSource(),
		PresetInput:         input,
		MaxSourceLines:      300,
		MaxSourceChars:      14000,
		AnswerVerifier:      numericDPAnswerVerifier,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
		Preset string `json:"preset"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Preset != BraidScaffoldClassNumericDP+"/"+BraidScaffoldIDRecurrenceTableV1 {
		t.Fatalf("output ok=%v preset=%q raw=%s", got.OK, got.Preset, raw)
	}
	if got.Answer != "solution = 6" {
		t.Fatalf("answer=%q", got.Answer)
	}
	if diag, applicable := numericDPAnswerVerifier(got.Answer, input); !applicable || !diag.Pass {
		t.Fatalf("preset answer failed verifier applicable=%v diag=%#v", applicable, diag)
	}
}

func TestNumericDPVerifierRejectsObjectiveMismatch(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"task_type":      BraidScaffoldClassNumericDP,
		"scaffold_class": BraidScaffoldClassNumericDP,
		"scaffold_id":    BraidScaffoldIDRecurrenceTableV1,
		"objective":      "count",
		"dp_dimensions":  []any{float64(6)},
		"target":         []any{float64(5)},
		"base_cases": []any{
			map[string]any{"index": []any{float64(0)}, "value": float64(1)},
		},
		"transitions": []any{
			map[string]any{"offset": []any{float64(-1)}, "multiplier": float64(1)},
			map[string]any{"offset": []any{float64(-2)}, "multiplier": float64(1)},
		},
	}
	diag, applicable := numericDPAnswerVerifier("solution = 99", input)
	if !applicable || diag.Pass || diag.FailureKind != "objective_mismatch" || diag.ExpectedFinal != 8 {
		t.Fatalf("diagnostic=%#v applicable=%v", diag, applicable)
	}
	diag, applicable = numericDPAnswerVerifier("solution = 8", input)
	if !applicable || !diag.Pass {
		t.Fatalf("passing diagnostic=%#v applicable=%v", diag, applicable)
	}
}

func TestSequenceSimulationPresetSolvesAndVerifiesSample(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"task_type":      BraidScaffoldClassSequenceSimulation,
		"scaffold_class": BraidScaffoldClassSequenceSimulation,
		"scaffold_id":    BraidScaffoldIDJSONPatchSequenceV1,
		"sequence_model": BraidScaffoldIDJSONPatchSequenceV1,
		"initial_state": map[string]any{
			"count": float64(0),
			"log":   []any{},
		},
		"events": []any{
			map[string]any{"op": "inc", "path": []any{"count"}, "delta": float64(2)},
			map[string]any{"op": "append", "path": []any{"log"}, "value": "tick"},
		},
		"invariants": []any{
			map[string]any{"path": []any{"count"}, "min": float64(0)},
		},
		"goal_state": map[string]any{
			"count": float64(2),
			"log":   []any{"tick"},
		},
	}
	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		Attempts:            1,
		TaskPrompt:          "Solve the typed sequence simulation task.",
		ExtractSolutionLine: true,
		PresetName:          BraidScaffoldClassSequenceSimulation + "/" + BraidScaffoldIDJSONPatchSequenceV1,
		PresetSource:        jsonPatchSequenceSimulationPresetSource(),
		PresetInput:         input,
		MaxSourceLines:      360,
		MaxSourceChars:      18000,
		AnswerVerifier:      sequenceSimulationAnswerVerifier,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
		Preset string `json:"preset"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Preset != BraidScaffoldClassSequenceSimulation+"/"+BraidScaffoldIDJSONPatchSequenceV1 {
		t.Fatalf("output ok=%v preset=%q raw=%s", got.OK, got.Preset, raw)
	}
	if diag, applicable := sequenceSimulationAnswerVerifier(got.Answer, input); !applicable || !diag.Pass {
		t.Fatalf("preset answer failed verifier applicable=%v diag=%#v answer=%s", applicable, diag, got.Answer)
	}
}

func TestSequenceSimulationVerifierRejectsBadFinalState(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"scaffold_class": BraidScaffoldClassSequenceSimulation,
		"scaffold_id":    BraidScaffoldIDJSONPatchSequenceV1,
		"initial_state":  map[string]any{"count": float64(0)},
		"events": []any{
			map[string]any{"op": "inc", "path": []any{"count"}, "delta": float64(2)},
		},
	}
	diag, applicable := sequenceSimulationAnswerVerifier(`solution = {"count":1}`, input)
	if !applicable || diag.Pass || diag.FailureKind != "final_state_mismatch" {
		t.Fatalf("diag applicable=%v diag=%#v", applicable, diag)
	}
}

func TestSequenceSimulationVerifierRejectsInvariantFailure(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"scaffold_class": BraidScaffoldClassSequenceSimulation,
		"scaffold_id":    BraidScaffoldIDJSONPatchSequenceV1,
		"initial_state":  map[string]any{"count": float64(1)},
		"events": []any{
			map[string]any{"op": "inc", "path": []any{"count"}, "delta": float64(-2)},
		},
		"invariants": []any{
			map[string]any{"path": []any{"count"}, "min": float64(0)},
		},
	}
	diag, applicable := sequenceSimulationAnswerVerifier(`solution = {"count":-1}`, input)
	if !applicable || diag.Pass || diag.FailureKind != "invariant" || diag.FailedAtStep != 0 {
		t.Fatalf("diag applicable=%v diag=%#v", applicable, diag)
	}
}

func TestHelperFactoryToolsRedraftsIncompletePythonSourceWithTaskContext(t *testing.T) {
	t.Parallel()

	badSource := `def solve(input):\`
	goodSource := `def solve(input):
    return {"ok": True, "answer": "solution = redrafted"}`
	var prompts []string
	server := helperFactoryTestServer(t, []string{
		mustHelperFactoryDraftJSON(t, badSource, nil),
		mustHelperFactoryDraftJSON(t, goodSource, nil),
	}, &prompts)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt:          "TASK_CONTEXT_MUST_BE_AVAILABLE_FOR_REDRAFT. Return solution = redrafted.",
		Attempts:            2,
		ExtractSolutionLine: true,
		Language:            HelperLanguagePython,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = redrafted" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
	if len(prompts) != 2 {
		t.Fatalf("prompts=%d", len(prompts))
	}
	if !strings.Contains(prompts[1], "TASK_CONTEXT_MUST_BE_AVAILABLE_FOR_REDRAFT") || !strings.Contains(prompts[1], "Previous failed attempt feedback") {
		t.Fatalf("second prompt should redraft with task context and feedback:\n%s", prompts[1])
	}
	if strings.Contains(prompts[1], "Repair a failed helper source file") {
		t.Fatalf("incomplete source should not use source-only repair prompt:\n%s", prompts[1])
	}
}

func TestHelperFactoryToolsFallsBackWhenResponseFormatUnsupported(t *testing.T) {
	t.Parallel()

	source := `def solve(input):
    return {"ok": True, "answer": "solution = fallback"}`
	var sawStrict bool
	var sawPlain bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, ok := req["response_format"]; ok {
			sawStrict = true
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"json mode unsupported"}}`))
			return
		}
		sawPlain = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-helper-fallback",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": mustHelperFactoryDraftJSON(t, source, nil)},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 4},
		})
	}))
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt:          "Return solution = fallback.",
		Attempts:            1,
		ExtractSolutionLine: true,
		Language:            HelperLanguagePython,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = fallback" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
	if !sawStrict || !sawPlain {
		t.Fatalf("expected strict then plain fallback, saw strict=%v plain=%v", sawStrict, sawPlain)
	}
}

func TestHelperFactoryToolsAutoExecuteArgsUseCompactedInstanceInput(t *testing.T) {
	t.Parallel()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		TaskPrompt: `
Puzzle description:
Return the moves.

Puzzle instance:
Initial state: [[1, 2], [3]]
Goal state: [[1], [2, 3]]
Number of blocks: 3
Number of stacks: 2
Return your solution in the format: stack_moves
`,
	}}
	raw := tools.AutoExecuteArgs()
	var got struct {
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode args: %v raw=%s", err, raw)
	}
	for _, key := range []string{"initial_state", "goal_state", "number_of_blocks", "number_of_stacks"} {
		if _, ok := got.Input[key]; !ok {
			t.Fatalf("missing key %q in auto args: %#v", key, got.Input)
		}
	}
}

func TestHelperFactoryToolsMergesEmptyDraftInputWithStructuredDefault(t *testing.T) {
	t.Parallel()

	source := `def solve(input):
    return {"ok": True, "answer": "solution = " + str(len(input["initial_state"])) + "," + str(input["number_of_blocks"])}`
	server := helperFactoryTestServer(t, []string{mustHelperFactoryDraftJSON(t, source, map[string]any{})}, nil)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt: `
Puzzle description:
Move blocks between stacks.

Puzzle instance:
Initial state: [[1, 2], [3]]
Goal state: [[1], [2, 3]]
Number of blocks: 3
Number of stacks: 2
Format your solution as:
solution = ...
`,
		Attempts:            1,
		ExtractSolutionLine: true,
		Language:            HelperLanguagePython,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = 2,3" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
}

func TestHelperFactoryToolsUsesPresetSourceBeforeDrafting(t *testing.T) {
	t.Parallel()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		TaskPrompt:          "Return solution = preset.",
		Attempts:            1,
		ExtractSolutionLine: true,
		PresetName:          "test_preset",
		PresetSource: `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": "solution = preset"}
}`,
		PresetInput: map[string]any{"value": "ignored"},
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK       bool             `json:"ok"`
		Answer   string           `json:"answer"`
		Preset   string           `json:"preset"`
		Attempts []map[string]any `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = preset" || got.Preset != "test_preset" {
		t.Fatalf("output ok=%v answer=%q preset=%q raw=%s", got.OK, got.Answer, got.Preset, raw)
	}
	if strings.Contains(raw, "func Solve") {
		t.Fatalf("compact helper output leaked full source: %s", raw)
	}
	if !strings.Contains(raw, "source_hash") {
		t.Fatalf("compact helper output missing source hash: %s", raw)
	}
	if strings.Contains(raw, `"output":`) {
		t.Fatalf("compact helper output should use output_summary, got: %s", raw)
	}
}

func TestHelperFactoryToolsCompactsVisibleInstanceInput(t *testing.T) {
	t.Parallel()

	source := `func Solve(input map[string]any) map[string]any {
	packages, ok := input["packages"].([]any)
	if !ok {
		return map[string]any{"ok": false, "error": "missing packages"}
	}
	suppliers, ok := input["suppliers"].([]any)
	if !ok {
		return map[string]any{"ok": false, "error": "missing suppliers"}
	}
	return map[string]any{"ok": true, "answer": fmt.Sprintf("solution = %d", len(packages)+len(suppliers))}
}`
	var prompts []string
	server := helperFactoryTestServer(t, []string{mustHelperFactoryDraftJSON(t, source, nil)}, &prompts)
	defer server.Close()

	tools := &HelperFactoryTools{Config: HelperFactoryConfig{
		LLM: rlm.LLMConfig{
			Provider:  "openai_compat",
			BaseURL:   server.URL,
			AuthMode:  "none",
			Model:     "test-model",
			Timeout:   5 * time.Second,
			MaxTokens: 512,
		},
		TaskPrompt: strings.Repeat("benchmark boilerplate should be removed\n", 40) + `
Puzzle description:
Choose one supplier and minimize total waste. Return -1 if no supplier can fit all packages.
Return the result modulo 1000000007.

Example:
Packages: [999, 999, 999]
Suppliers: [[999]]

Puzzle instance:
Number of packages: 3
Number of suppliers: 2
Packages: [2, 3, 5]
Suppliers: [[4, 8], [2, 8]]

Find the minimum total wasted space (mod 1000000007).
Format your solution as:
solution = <integer>
`,
		Attempts:            1,
		ExtractSolutionLine: true,
	}}
	raw, err := tools.Execute(context.Background(), EphemeralHelperSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var got struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	if !got.OK || got.Answer != "solution = 5" {
		t.Fatalf("output ok=%v answer=%q raw=%s", got.OK, got.Answer, raw)
	}
	if len(prompts) != 1 {
		t.Fatalf("prompts=%d", len(prompts))
	}
	prompt := prompts[0]
	for _, want := range []string{
		"Compacted visible task",
		"packages: array len=3",
		"suppliers: array len=2",
		"Canonical structured input JSON:",
		`"packages":[2,3,5]`,
		"JSON arrays arrive as []any",
		"minimum total wasted space",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("compacted prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, forbidden := range []string{
		"benchmark boilerplate should be removed",
		"999, 999, 999",
		"[[4, 8], [2, 8]]",
	} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("compacted prompt still contains %q:\n%s", forbidden, prompt)
		}
	}
}

func helperFactoryTestServer(t *testing.T, contents []string, prompts *[]string) *httptest.Server {
	t.Helper()
	var calls int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if prompts != nil && len(req.Messages) > 0 {
			*prompts = append(*prompts, req.Messages[len(req.Messages)-1].Content)
		}
		if calls >= len(contents) {
			t.Fatalf("unexpected draft call %d", calls+1)
		}
		content := contents[calls]
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-helper-test",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 4},
		})
	}))
}

func mustHelperFactoryDraftJSON(t *testing.T, source string, input map[string]any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"source": source,
		"input":  input,
	})
	if err != nil {
		t.Fatalf("marshal draft: %v", err)
	}
	return string(body)
}

func mustHelperFactoryDraftB64JSON(t *testing.T, source string, input map[string]any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"source_b64": base64.StdEncoding.EncodeToString([]byte(source)),
		"input":      input,
	})
	if err != nil {
		t.Fatalf("marshal draft: %v", err)
	}
	return string(body)
}
