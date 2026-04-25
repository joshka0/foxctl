package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	if !strings.Contains(prompts[1], "Verifier counterexample") || !strings.Contains(prompts[1], "unit_counterexample") {
		t.Fatalf("repair prompt missing verifier counterexample:\n%s", prompts[1])
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
