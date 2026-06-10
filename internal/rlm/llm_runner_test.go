package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

type fakeLLMToolExecutor struct{}

func (fakeLLMToolExecutor) Execute(_ context.Context, _ string, _ json.RawMessage) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

type evidenceLLMToolExecutor struct{}

func (evidenceLLMToolExecutor) Execute(_ context.Context, name string, args json.RawMessage) (map[string]any, error) {
	switch name {
	case "gather_context", "gather_memory_context":
		return map[string]any{
			"schema_version": "context_answer_surface/v2",
			"answer_seed": map[string]any{
				"paths": []any{"internal/context/contextengine/retrieve_task.go"},
			},
			"path_set": map[string]any{
				"must": []any{
					map[string]any{
						"path":     "internal/context/contextengine/retrieve_task.go",
						"load_ref": "path:internal/context/contextengine/retrieve_task.go",
					},
					map[string]any{
						"path":     "docs/memory.md",
						"load_ref": "memory_claim:claim-runner-1",
					},
				},
			},
		}, nil
	case "load_evidence_ref":
		if strings.Contains(string(args), "error-claim") {
			return nil, fmt.Errorf("load failed for memory_claim:error-claim")
		}
		return map[string]any{
			"load_ref": "memory_claim:claim-runner-2",
			"evidence": []any{
				map[string]any{"type": "path", "ref": "internal/rlm/types.go"},
			},
		}, nil
	case "evidence_ledger":
		return map[string]any{
			"schema_version": "evidence_ledger/v1",
			"accepted_refs":  []any{"memory_claim:claim-runner-2"},
			"accepted_rows": []any{
				map[string]any{"ref": "path:internal/rlm/types.go", "status": "accept", "text": "The runner should cite the accepted ledger row."},
			},
			"rejected_rows": []any{
				map[string]any{"ref": "memory_claim:near-miss", "status": "reject"},
			},
			"ready":          true,
			"needs_fallback": false,
		}, nil
	default:
		return map[string]any{"ok": true}, nil
	}
}

type querySensitiveEvidenceLedgerExecutor struct{}

func (querySensitiveEvidenceLedgerExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (map[string]any, error) {
	if name != "evidence_ledger" {
		return evidenceLLMToolExecutor{}.Execute(ctx, name, args)
	}
	var input map[string]any
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(fmt.Sprint(input["query"])), "Answer the LongMem") {
		return map[string]any{
			"schema_version":   "evidence_ledger/v1",
			"accepted_refs":    []any{},
			"accepted_rows":    []any{},
			"rejected_refs":    []any{"memory_claim:claim-runner-1"},
			"rejected_rows":    []any{map[string]any{"ref": "memory_claim:claim-runner-1", "status": "reject"}},
			"ready":            false,
			"needs_fallback":   true,
			"answer_outline":   map[string]any{"missing_slots": []any{"answer fact"}},
			"fallback_queries": []any{"recall prior memory claim"},
		}, nil
	}
	return evidenceLLMToolExecutor{}.Execute(ctx, name, args)
}

func contentResponse(id, content string) map[string]any {
	return map[string]any{
		"id": id,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	}
}

func toolCallResponse(id, callID, name, arguments string) map[string]any {
	return map[string]any{
		"id": id,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{
						{
							"id":   callID,
							"type": "function",
							"function": map[string]any{
								"name":      name,
								"arguments": arguments,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	}
}

func assertNoRequestTools(t *testing.T, request map[string]any) {
	t.Helper()
	value, exists := request["tools"]
	if !exists || value == nil {
		return
	}
	tools, ok := value.([]any)
	if !ok || len(tools) > 0 {
		t.Fatalf("final synthesis request tools=%T %v, want none", value, value)
	}
}

func TestLLMRunnerUsesOpenAICompatibleChatPath(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/chat/completions" {
			t.Fatalf("path=%q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "LLM inspection summary",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 4,
				"total_tokens":      14,
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 2,
			ToolProfile:   string(ToolProfileCodeDebug),
		},
	}

	result, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 2,
	}, Environment{
		Tools: []Tool{{
			Name:        "retrieve_code",
			Description: "code retrieval",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
			ReadOnly:    true,
		}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Answer != "LLM inspection summary" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if got := result.Metadata["provider"]; got != "lmstudio" {
		t.Fatalf("provider=%v", got)
	}
	tools, ok := captured["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools=%T %v", captured["tools"], captured["tools"])
	}
	firstTool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tool=%T", tools[0])
	}
	function, ok := firstTool["function"].(map[string]any)
	if !ok {
		t.Fatalf("function=%T", firstTool["function"])
	}
	parameters, ok := function["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters=%T", function["parameters"])
	}
	properties, ok := parameters["properties"].(map[string]any)
	if !ok || properties["query"] == nil {
		t.Fatalf("properties=%v", parameters["properties"])
	}
}

func TestLLMRunnerAttachesSurfacedToolEvidenceRefs(t *testing.T) {
	t.Parallel()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-tool",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{
								{
									"id":   "call-1",
									"type": "function",
									"function": map[string]any{
										"name":      "gather_context",
										"arguments": `{}`,
									},
								},
								{
									"id":   "call-2",
									"type": "function",
									"function": map[string]any{
										"name":      "load_evidence_ref",
										"arguments": `{"ref":"memory_claim:error-claim"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-final",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "The task context claim is relevant: memory_claim:claim-runner-1 and internal/context/contextengine/retrieve_task.go.",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: evidenceLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 2,
			ToolProfile:   string(ToolProfileDefault),
		},
	}

	result, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect memory claim",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 2,
	}, Environment{
		Tools: []Tool{
			{
				Name:        "gather_context",
				Description: "context gathering",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
				ReadOnly:    true,
			},
			{
				Name:        "load_evidence_ref",
				Description: "evidence loading",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"ref":{"type":"string"}}}`),
				ReadOnly:    true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantRefs := []string{
		"memory_claim:claim-runner-1",
		"path:internal/context/contextengine/retrieve_task.go",
	}
	if len(result.EvidenceRefs) != 0 {
		t.Fatalf("evidence refs=%v want none; surfaced tool refs are candidates only", result.EvidenceRefs)
	}
	if !reflect.DeepEqual(result.Metadata["tool_surfaced_evidence_refs"], wantRefs) {
		t.Fatalf("metadata tool_surfaced_evidence_refs=%v want %v", result.Metadata["tool_surfaced_evidence_refs"], wantRefs)
	}
	if !reflect.DeepEqual(result.Metadata["answer_used_evidence_refs"], wantRefs) {
		t.Fatalf("metadata answer_used_evidence_refs=%v want %v", result.Metadata["answer_used_evidence_refs"], wantRefs)
	}
}

func TestLLMRunnerStagedAggregatesSurfacedToolEvidenceRefs(t *testing.T) {
	t.Parallel()

	responses := []map[string]any{
		toolCallResponse("chatcmpl-recall-tool", "call-recall", "gather_memory_context", `{}`),
		contentResponse("chatcmpl-recall-final", "Recall phase complete."),
		toolCallResponse("chatcmpl-verify-tool", "call-verify", "evidence_ledger", `{"query":"recall prior memory claim","refs":["memory_claim:claim-runner-1"]}`),
		contentResponse("chatcmpl-verify-final", "Verification phase complete."),
		contentResponse("chatcmpl-synthesis", "Final answer cites memory_claim:claim-runner-2, internal/context/contextengine/retrieve_task.go, and bootstrap/memory_context.go."),
	}
	callCount := 0
	var capturedRequests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount >= len(responses) {
			t.Fatalf("unexpected llm call %d", callCount+1)
		}
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		capturedRequests = append(capturedRequests, requestBody)
		response := responses[callCount]
		callCount++
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: evidenceLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 3,
			RouteProfile:  RouteProfileMemoryRecall,
			PlanMode:      PlanModeStaged,
			ToolProfile:   string(ToolProfileMemoryRecall),
		},
	}

	result, err := runner.Run(context.Background(), Task{
		Prompt:        "recall prior memory claim",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 3,
	}, Environment{
		RepoHandles: []string{"path:bootstrap/memory_context.go"},
		Tools: []Tool{
			{Name: "gather_context", Description: "context gathering", ReadOnly: true},
			{Name: "gather_memory_context", Description: "memory context gathering", ReadOnly: true},
			{Name: "evidence_ledger", Description: "evidence ledger", ReadOnly: true},
			{Name: "load_evidence_ref", Description: "evidence loading", ReadOnly: true},
			{Name: "retrieve_memory", Description: "memory retrieval", ReadOnly: true},
			{Name: "retrieve_context", Description: "context retrieval", ReadOnly: true},
			{Name: "retrieve_task", Description: "task retrieval", ReadOnly: true},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantRefs := []string{
		"memory_claim:claim-runner-1",
		"memory_claim:claim-runner-2",
		"path:internal/context/contextengine/retrieve_task.go",
		"path:internal/rlm/types.go",
	}
	if !reflect.DeepEqual(result.Metadata["tool_surfaced_evidence_refs"], wantRefs) {
		t.Fatalf("metadata tool_surfaced_evidence_refs=%v want %v", result.Metadata["tool_surfaced_evidence_refs"], wantRefs)
	}
	if !reflect.DeepEqual(result.EvidenceRefs, []string{"path:bootstrap/memory_context.go"}) {
		t.Fatalf("evidence refs=%v want only bootstrap evidence", result.EvidenceRefs)
	}
	if !reflect.DeepEqual(result.Metadata["answer_used_evidence_refs"], []string{
		"memory_claim:claim-runner-2",
		"path:internal/context/contextengine/retrieve_task.go",
	}) {
		t.Fatalf("answer used refs=%v", result.Metadata["answer_used_evidence_refs"])
	}
	phases, ok := result.Metadata["phases"].([]map[string]any)
	if !ok || len(phases) != 2 {
		t.Fatalf("phases=%T %v", result.Metadata["phases"], result.Metadata["phases"])
	}
	if !reflect.DeepEqual(phases[0]["tool_surfaced_evidence_refs"], []string{
		"memory_claim:claim-runner-1",
		"path:internal/context/contextengine/retrieve_task.go",
	}) {
		t.Fatalf("recall phase surfaced refs=%v", phases[0]["tool_surfaced_evidence_refs"])
	}
	if !reflect.DeepEqual(phases[1]["tool_surfaced_evidence_refs"], []string{
		"memory_claim:claim-runner-2",
		"path:internal/rlm/types.go",
	}) {
		t.Fatalf("verification phase surfaced refs=%v", phases[1]["tool_surfaced_evidence_refs"])
	}
	if callCount != len(responses) {
		t.Fatalf("llm calls=%d want %d", callCount, len(responses))
	}
	finalRequestBody, err := json.Marshal(capturedRequests[len(capturedRequests)-1])
	if err != nil {
		t.Fatalf("marshal final request: %v", err)
	}
	if !strings.Contains(string(finalRequestBody), "Diagnostic surfaced evidence refs") ||
		!strings.Contains(string(finalRequestBody), "memory_claim:claim-runner-2") ||
		!strings.Contains(string(finalRequestBody), "Accepted ledger evidence") ||
		!strings.Contains(string(finalRequestBody), "The runner should cite the accepted ledger row.") {
		t.Fatalf("final synthesis request missing surfaced refs: %s", finalRequestBody)
	}
	assertNoRequestTools(t, capturedRequests[len(capturedRequests)-1])
}

func TestEvidenceLedgerQueryFromTaskPromptUsesTrailingQuestion(t *testing.T) {
	t.Parallel()

	prompt := `Answer the LongMem recall question using the available context tools.
Start with gather_memory_context and then build evidence_ledger.

Question: Where did I redeem a $5 coupon on coffee creamer?`
	got := evidenceLedgerQueryFromTaskPrompt(prompt)
	want := "Where did I redeem a $5 coupon on coffee creamer?"
	if got != want {
		t.Fatalf("evidenceLedgerQueryFromTaskPrompt()=%q want %q", got, want)
	}

	plain := "recall prior memory claim"
	if got := evidenceLedgerQueryFromTaskPrompt(plain); got != plain {
		t.Fatalf("plain prompt query=%q want %q", got, plain)
	}
}

func TestBuildSynthesisPromptBansUnacceptedSurfacedEvidence(t *testing.T) {
	t.Parallel()

	prompt := buildSynthesisPrompt(
		"How long is my daily commute?",
		nil,
		[]string{"named_memory:longmem://near-miss"},
		nil,
		[]string{"verification: Evidence ledger built. The ledger is not ready; do not answer from rejected refs without accepted rows."},
	)
	if !strings.Contains(prompt, "Diagnostic surfaced evidence refs") {
		t.Fatalf("prompt missing diagnostic surfaced refs label:\n%s", prompt)
	}
	if !strings.Contains(prompt, "No accepted ledger evidence was collected") {
		t.Fatalf("prompt missing no-accepted-ledger guard:\n%s", prompt)
	}
}

func TestLLMRunnerStagedRepairsEmptyLedgerWithCanonicalQuery(t *testing.T) {
	t.Parallel()

	responses := []map[string]any{
		toolCallResponse("chatcmpl-recall-tool", "call-recall", "gather_memory_context", `{}`),
		contentResponse("chatcmpl-recall-final", "Recall phase complete."),
		toolCallResponse("chatcmpl-verify-ledger", "call-verify-ledger", "evidence_ledger", `{"query":"Answer the LongMem recall question using tools. Question: recall prior memory claim","refs":["memory_claim:claim-runner-1"]}`),
		contentResponse("chatcmpl-verify-final", "Verification phase produced no accepted evidence."),
		contentResponse("chatcmpl-synthesis", "Final answer cites accepted ledger evidence."),
	}
	callCount := 0
	var capturedRequests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount >= len(responses) {
			t.Fatalf("unexpected llm call %d", callCount+1)
		}
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		capturedRequests = append(capturedRequests, requestBody)
		response := responses[callCount]
		callCount++
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: querySensitiveEvidenceLedgerExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 3,
			RouteProfile:  RouteProfileMemoryRecall,
			PlanMode:      PlanModeStaged,
			ToolProfile:   string(ToolProfileMemoryRecall),
		},
	}

	result, err := runner.Run(context.Background(), Task{
		Prompt: `Answer the LongMem recall question using the available context tools.

Question: recall prior memory claim`,
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 3,
	}, Environment{
		Tools: []Tool{
			{Name: "gather_context", Description: "context gathering", ReadOnly: true},
			{Name: "gather_memory_context", Description: "memory context gathering", ReadOnly: true},
			{Name: "evidence_ledger", Description: "evidence ledger", ReadOnly: true},
			{Name: "load_evidence_ref", Description: "evidence loading", ReadOnly: true},
			{Name: "retrieve_memory", Description: "memory retrieval", ReadOnly: true},
			{Name: "retrieve_context", Description: "context retrieval", ReadOnly: true},
			{Name: "retrieve_task", Description: "task retrieval", ReadOnly: true},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if callCount != len(responses) {
		t.Fatalf("llm calls=%d want %d; canonical ledger repair should not call the model", callCount, len(responses))
	}
	if !reflect.DeepEqual(result.Metadata["accepted_ledger_evidence"], []string{
		"path:internal/rlm/types.go: The runner should cite the accepted ledger row.",
	}) {
		t.Fatalf("accepted ledger evidence=%v", result.Metadata["accepted_ledger_evidence"])
	}
	finalRequestBody, err := json.Marshal(capturedRequests[len(capturedRequests)-1])
	if err != nil {
		t.Fatalf("marshal final request: %v", err)
	}
	if !strings.Contains(string(finalRequestBody), "Accepted ledger evidence") ||
		!strings.Contains(string(finalRequestBody), "The runner should cite the accepted ledger row.") {
		t.Fatalf("final synthesis request missing repaired ledger evidence: %s", finalRequestBody)
	}
}

func TestLLMRunnerStagedAutoBuildsEvidenceLedgerWhenVerificationMissesRequiredTool(t *testing.T) {
	t.Parallel()

	responses := []map[string]any{
		toolCallResponse("chatcmpl-recall-tool", "call-recall", "gather_memory_context", `{}`),
		contentResponse("chatcmpl-recall-final", "Recall phase complete."),
		toolCallResponse("chatcmpl-verify-wrong-tool", "call-verify-gather", "gather_memory_context", `{}`),
		contentResponse("chatcmpl-verify-final", "Verification gathered more context but skipped the ledger."),
		contentResponse("chatcmpl-synthesis", "Final answer cites path:internal/rlm/types.go."),
	}
	callCount := 0
	var capturedRequests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount >= len(responses) {
			t.Fatalf("unexpected llm call %d", callCount+1)
		}
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		capturedRequests = append(capturedRequests, requestBody)
		response := responses[callCount]
		callCount++
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: evidenceLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 3,
			RouteProfile:  RouteProfileMemoryRecall,
			PlanMode:      PlanModeStaged,
			ToolProfile:   string(ToolProfileMemoryRecall),
		},
	}

	result, err := runner.Run(context.Background(), Task{
		Prompt:        "recall prior memory claim",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 3,
	}, Environment{
		Tools: []Tool{
			{Name: "gather_context", Description: "context gathering", ReadOnly: true},
			{Name: "gather_memory_context", Description: "memory context gathering", ReadOnly: true},
			{Name: "evidence_ledger", Description: "evidence ledger", ReadOnly: true},
			{Name: "load_evidence_ref", Description: "evidence loading", ReadOnly: true},
			{Name: "retrieve_memory", Description: "memory retrieval", ReadOnly: true},
			{Name: "retrieve_context", Description: "context retrieval", ReadOnly: true},
			{Name: "retrieve_task", Description: "task retrieval", ReadOnly: true},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if callCount != len(responses) {
		t.Fatalf("llm calls=%d want %d; deterministic ledger fallback should not call the model", callCount, len(responses))
	}
	phases, ok := result.Metadata["phases"].([]map[string]any)
	if !ok || len(phases) != 2 {
		t.Fatalf("phases=%T %v", result.Metadata["phases"], result.Metadata["phases"])
	}
	if !reflect.DeepEqual(phases[1]["tool_names"], []string{"evidence_ledger"}) {
		t.Fatalf("verification phase tool_names=%v want deterministic evidence_ledger", phases[1]["tool_names"])
	}
	if !reflect.DeepEqual(result.Metadata["accepted_ledger_evidence"], []string{
		"path:internal/rlm/types.go: The runner should cite the accepted ledger row.",
	}) {
		t.Fatalf("accepted ledger evidence=%v", result.Metadata["accepted_ledger_evidence"])
	}
	finalRequestBody, err := json.Marshal(capturedRequests[len(capturedRequests)-1])
	if err != nil {
		t.Fatalf("marshal final request: %v", err)
	}
	if !strings.Contains(string(finalRequestBody), "Accepted ledger evidence") ||
		!strings.Contains(string(finalRequestBody), "The runner should cite the accepted ledger row.") {
		t.Fatalf("final synthesis request missing deterministic ledger evidence: %s", finalRequestBody)
	}
	assertNoRequestTools(t, capturedRequests[len(capturedRequests)-1])
}

func TestLLMRunnerSanitizesFinalAnswer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "<|channel>thought\ninternal notes<channel|>Final answer",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 4,
				"total_tokens":      14,
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:    "lmstudio",
			APIKey:      "lm-studio",
			BaseURL:     server.URL + "/v1",
			Model:       "test-model",
			Timeout:     5 * time.Second,
			ToolProfile: string(ToolProfileCodeDebug),
		},
	}

	result, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 1,
	}, Environment{
		Tools: []Tool{{Name: "retrieve_code", Description: "code retrieval", ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Answer != "Final answer" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if result.Metadata["output_sanitization"] == nil {
		t.Fatalf("missing output_sanitization metadata: %#v", result.Metadata)
	}
}

func TestLLMRunnerReturnsErrorOnModelFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 0,
				"total_tokens":      10,
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 2,
			ToolProfile:   string(ToolProfileCodeDebug),
		},
	}

	_, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 1,
	}, Environment{
		Tools: []Tool{{Name: "retrieve_code", Description: "code retrieval", ReadOnly: true}},
	})
	if err == nil {
		t.Fatal("expected error on empty assistant response")
	}
}

func TestLLMRunnerPreservesStopReasonBeforeAssistantResponse(t *testing.T) {
	t.Parallel()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-tool",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{
								{
									"id":   "call-1",
									"type": "function",
									"function": map[string]any{
										"name":      "retrieve_code",
										"arguments": `{}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-empty-finalize",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 1,
			ToolProfile:   string(ToolProfileCodeDebug),
		},
	}

	_, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 1,
	}, Environment{
		Tools: []Tool{{Name: "retrieve_code", Description: "code retrieval", ReadOnly: true}},
	})
	if err == nil {
		t.Fatal("expected max-iterations error")
	}
	want := "rlm llm runner: max_iterations before assistant response"
	if err.Error() != want {
		t.Fatalf("err=%v want %q", err, want)
	}
}

func TestLLMRunnerPreservesCancelledBeforeAssistantResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when context is already cancelled")
	}))
	defer server.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 2,
			ToolProfile:   string(ToolProfileCodeDebug),
		},
	}

	_, err := runner.Run(ctx, Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 2,
	}, Environment{
		Tools: []Tool{{Name: "retrieve_code", Description: "repo", ReadOnly: true}},
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	want := "rlm llm runner: cancelled before assistant response: context deadline exceeded"
	if err.Error() != want {
		t.Fatalf("err=%v want %q", err, want)
	}
}

func TestLLMRunnerRequireToolUseRejectsZeroToolCallAnswer(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Generic answer without tools",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 4,
				"total_tokens":      14,
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:       "lmstudio",
			APIKey:         "lm-studio",
			BaseURL:        server.URL + "/v1",
			Model:          "test-model",
			Timeout:        5 * time.Second,
			MaxIterations:  2,
			RequireToolUse: true,
			ToolProfile:    string(ToolProfileCodeDebug),
		},
	}

	_, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 2,
	}, Environment{
		Tools: []Tool{{
			Name:        "retrieve_code",
			Description: "code retrieval",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
			ReadOnly:    true,
		}},
	})
	if err == nil || err.Error() != "rlm llm runner: model answered without using tools" {
		t.Fatalf("err=%v", err)
	}
	if got := captured["tool_choice"]; got != "required" {
		t.Fatalf("tool_choice=%v want required", got)
	}
}

func TestLLMRunnerRequireToolUseRejectsEmptyToolSurface(t *testing.T) {
	t.Parallel()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			RequireToolUse: true,
		},
	}

	_, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 2,
	}, Environment{})
	if err == nil || err.Error() != "rlm llm runner: require-tool-use is enabled but no tools are available" {
		t.Fatalf("err=%v", err)
	}
}

func TestLLMSystemPromptUsesCompositeToolsOnlyWhenLegacyAbsent(t *testing.T) {
	t.Parallel()

	prompt := BuildLLMSystemPrompt(Environment{
		Tools: compositeOnlyTools(),
	}, Task{
		Prompt:        "find the retrieval prompt wiring",
		WorkspaceRoot: "/repo",
	})

	assertMentionsTools(t, prompt, []string{
		"plan_context_query",
		"gather_memory_context",
		"retrieve_code",
		"retrieve_memory",
		"retrieve_context",
		"retrieve_task",
		"gather_context",
		"retrieve_mixed",
		"load_evidence_ref",
		"aggregate_evidence_refs",
		"evidence_ledger",
	})
	for _, want := range []string{
		"Use gather_memory_context for explicit long-term memory recall",
		"evidence_digest, answer_seed, and path_set",
		"call plan_context_query before gather_context",
		"Read evidence_digest first",
		"Deterministic gather trust policy",
		"Do not spend extra tool/model turns re-ranking those paths",
		"Fall back to verification or broader retrieval for package-owner/package-anchor questions",
		"call expand_context_graph before claiming dependency or subsystem completeness",
		"Use aggregate_evidence_refs after gather surfaces",
		"Use evidence_ledger before final answers",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestStagedPromptsUseLoadEvidenceRefWhenLegacyAbsent(t *testing.T) {
	t.Parallel()

	base := BuildLLMSystemPrompt(Environment{
		Tools: compositeOnlyTools(),
	}, Task{Prompt: "inspect staged prompts", WorkspaceRoot: "/repo"})
	phase := Phase{
		Name:         "inspection",
		Objective:    "Aggregate or inspect the strongest candidates from discovery.",
		AllowedTools: []string{"evidence_ledger", "aggregate_evidence_refs", "load_evidence_ref", "retrieve_code"},
		RequireOneOf: []string{"evidence_ledger"},
	}
	systemPrompt := buildPhaseSystemPrompt(base, "inspect staged prompts", phase, []string{"internal/rlm/llm_runner.go"}, nil)
	userPrompt := buildPhasePrompt("inspect staged prompts", phase, []string{"internal/rlm/llm_runner.go"}, nil)
	combined := systemPrompt + "\n" + userPrompt

	assertMentionsTools(t, combined, []string{"retrieve_code", "load_evidence_ref", "aggregate_evidence_refs", "evidence_ledger"})
	if !strings.Contains(userPrompt, "must build an evidence_ledger") {
		t.Fatalf("staged inspection prompt did not require evidence ledger:\n%s", userPrompt)
	}
}

func compositeOnlyTools() []Tool {
	return []Tool{
		{Name: "plan_context_query", ReadOnly: true},
		{Name: "gather_memory_context", ReadOnly: true},
		{Name: "retrieve_code", ReadOnly: true},
		{Name: "retrieve_memory", ReadOnly: true},
		{Name: "retrieve_context", ReadOnly: true},
		{Name: "retrieve_task", ReadOnly: true},
		{Name: "gather_context", ReadOnly: true},
		{Name: "expand_context_graph", ReadOnly: true},
		{Name: "retrieve_mixed", ReadOnly: true},
		{Name: "load_evidence_ref", ReadOnly: true},
		{Name: "aggregate_evidence_refs", ReadOnly: true},
		{Name: "evidence_ledger", ReadOnly: true},
	}
}

func assertMentionsTools(t *testing.T, prompt string, tools []string) {
	t.Helper()
	for _, tool := range tools {
		if !strings.Contains(prompt, tool) {
			t.Fatalf("prompt missing tool %q:\n%s", tool, prompt)
		}
	}
}

func TestCollectRetrievedPaths(t *testing.T) {
	t.Parallel()

	results := []engine.ToolResult{
		{
			Content: `{"results":[{"path":"internal/auth/handler.go"},{"full_path":"/repo/internal/auth/middleware.go"},{"path":".foxctl/runtime/handoff.json"}]}`,
		},
		{
			Content: `{"path":"internal/auth/handler.go"}`,
		},
		{
			Content: `not-json`,
		},
	}

	got := collectRetrievedPaths(results, "/repo", "See internal/auth/service.go and web/src/AuthPanel.tsx for the final entrypoints.")
	want := []string{"internal/auth/handler.go", "internal/auth/middleware.go", "internal/auth/service.go", "web/src/AuthPanel.tsx"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectRetrievedPaths()=%v want %v", got, want)
	}
}

func TestCollectSurfacedToolEvidenceRefs(t *testing.T) {
	t.Parallel()

	calls := []engine.ToolCall{
		{ID: "good", Name: "gather_context"},
		{ID: "extra", Name: "load_evidence_ref"},
		{ID: "aggregate", Name: "aggregate_evidence_refs"},
		{ID: "ledger", Name: "evidence_ledger"},
		{ID: "error", Name: "gather_context"},
		{ID: "private", Name: "search_artifacts"},
		{ID: "invalid", Name: "gather_context"},
	}
	results := []engine.ToolResult{
		{
			ToolCallID: "good",
			Content: `{
				"path_set": {"must": [
					{"path": "internal/rlm/llm_runner.go", "load_ref": "path:internal/rlm/llm_runner.go"},
					{"path": "memory", "load_ref": "memory_claim:claim-1"}
				]},
				"evidence": [{"type": "session", "ref": "session-1"}]
			}`,
		},
		{
			ToolCallID: "extra",
			Content:    `{"load_refs":["path:internal/rlm/types.go","memory_claim:claim-1"]}`,
		},
		{
			ToolCallID: "aggregate",
			Content:    `{"refs":[{"ref":"named_memory:aggregate-1","loaded":true}],"claims":[{"ref":"memory_claim:aggregate-2","text":"supported"}]}`,
		},
		{
			ToolCallID: "ledger",
			Content:    `{"accepted_refs":["named_memory:ledger-0"],"accepted_rows":[{"ref":"named_memory:ledger-1"}],"rejected_rows":[{"ref":"memory_claim:ledger-rejected"}]}`,
		},
		{
			ToolCallID: "error",
			IsError:    true,
			Content:    `{"load_ref":"memory_claim:error-claim"}`,
		},
		{
			ToolCallID: "private",
			Content:    `{"load_ref":"memory_claim:private-claim","ref":"artifact:private"}`,
		},
		{
			ToolCallID: "invalid",
			Content:    `not-json`,
		},
	}

	got := collectSurfacedToolEvidenceRefs(calls, results)
	want := []string{
		"memory_claim:aggregate-2",
		"memory_claim:claim-1",
		"named_memory:aggregate-1",
		"named_memory:ledger-0",
		"named_memory:ledger-1",
		"path:internal/rlm/llm_runner.go",
		"path:internal/rlm/types.go",
		"session:session-1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectSurfacedToolEvidenceRefs()=%v want %v", got, want)
	}
}

func TestCollectAnswerUsedEvidenceRefsRequiresExplicitCitation(t *testing.T) {
	t.Parallel()

	got := collectAnswerUsedEvidenceRefs(
		"Use memory_claim:claim-1 plus internal/rlm/types.go and docs/memory.md; claim-2 is only mentioned as a bare id, and internal/rlm/unused.go.bak is not the cited file.",
		[]string{
			"path:internal/rlm/types.go",
			"memory_claim:claim-1",
			"memory_claim:claim-2",
			"path:internal/rlm/unused.go",
			"note:docs/memory.md",
		},
	)
	want := []string{
		"memory_claim:claim-1",
		"note:docs/memory.md",
		"path:internal/rlm/types.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectAnswerUsedEvidenceRefs()=%v want %v", got, want)
	}

	adjacent := collectAnswerUsedEvidenceRefs(
		"Use memory_claim:claim-10 and internal/rlm/unused.go.bak.",
		[]string{
			"memory_claim:claim-1",
			"memory_claim:claim-10",
			"path:internal/rlm/unused.go",
			"path:internal/rlm/unused.go.bak",
		},
	)
	wantAdjacent := []string{
		"memory_claim:claim-10",
		"path:internal/rlm/unused.go.bak",
	}
	if !reflect.DeepEqual(adjacent, wantAdjacent) {
		t.Fatalf("adjacent collectAnswerUsedEvidenceRefs()=%v want %v", adjacent, wantAdjacent)
	}
}

func TestCollectGatherContextSurfaceMetadata(t *testing.T) {
	t.Parallel()

	metadata := collectGatherContextSurfaceMetadata(
		[]engine.ToolCall{{ID: "call-1", Name: "gather_context"}},
		[]engine.ToolResult{{
			ToolCallID: "call-1",
			Content: `{
				"schema_version": "context_answer_surface/v2",
				"selected_paths": [{"path": "internal/rlm/env/tool_exec.go"}],
				"answer_seed": {"paths": ["internal/context/contextengine/context_gather.go"]},
				"path_set": {"must": [{"path": "internal/rlm/env/tools.go"}]},
				"certificate": {"status": "certified"}
			}`,
		}},
		"/workspace",
	)
	if !reflect.DeepEqual(metadata["gather_context_selected_paths"], []string{"internal/rlm/env/tool_exec.go"}) {
		t.Fatalf("selected paths=%v", metadata["gather_context_selected_paths"])
	}
	if !reflect.DeepEqual(metadata["gather_context_answer_seed_paths"], []string{"internal/context/contextengine/context_gather.go"}) {
		t.Fatalf("answer seed paths=%v", metadata["gather_context_answer_seed_paths"])
	}
	if !reflect.DeepEqual(metadata["gather_context_path_set_must"], []string{"internal/rlm/env/tools.go"}) {
		t.Fatalf("path set=%v", metadata["gather_context_path_set_must"])
	}
	if !reflect.DeepEqual(metadata["gather_context_certificate_statuses"], []string{"certified"}) {
		t.Fatalf("certificate statuses=%v", metadata["gather_context_certificate_statuses"])
	}
}

func TestSummarizeParentToolUsage(t *testing.T) {
	t.Parallel()

	got := summarizeParentToolUsage([]engine.IterationUsage{
		{
			Iteration:               1,
			PromptTokens:            100,
			CompletionTokens:        10,
			ToolCalls:               1,
			ToolNames:               []string{"retrieve_code"},
			ToolResultTokenEstimate: 42,
		},
		{
			Iteration:        2,
			PromptTokens:     145,
			CompletionTokens: 20,
		},
	}, "retrieve_code")

	if intFromAny(got["target_tool_invocations"]) != 1 {
		t.Fatalf("usage=%v", got)
	}
	if intFromAny(got["target_tool_prompt_delta_total"]) != 45 {
		t.Fatalf("usage=%v", got)
	}
	if intFromAny(got["target_tool_result_token_estimate_total"]) != 42 {
		t.Fatalf("usage=%v", got)
	}
}

func TestRerankCandidatePaths(t *testing.T) {
	t.Parallel()

	// After hard-cut: reranking uses path depth ordering, not keyword scoring.
	paths := rerankCandidatePaths("semantic indexing package", []string{
		"builder.go",
		"internal/intelligence/indexing/semantic/indexer.go",
		"internal/v2/core/events/repository.go",
	})
	if len(paths) == 0 {
		t.Fatal("rerank produced no paths")
	}
	// Deeper paths should come first (depth-based ordering).
	if pathDepth(paths[0]) < pathDepth(paths[len(paths)-1]) {
		t.Fatalf("rerank=%v should order deeper paths first", paths)
	}
}
