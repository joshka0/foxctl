package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/runtime/engine"
)

func TestIsRetryableREPLLLMError(t *testing.T) {
	t.Parallel()

	if !isRetryableREPLLLMError(errors.New("read response: local error: tls: bad record MAC")) {
		t.Fatal("tls bad record MAC should be retryable")
	}
	if isRetryableREPLLLMError(context.DeadlineExceeded) {
		t.Fatal("context deadline exceeded should not be retried")
	}
	if isRetryableREPLLLMError(errors.New("invalid tool arguments")) {
		t.Fatal("semantic/runtime errors should not be retried")
	}
}

func TestREPLRunnerUsesPythonREPLTool(t *testing.T) {
	var calls int
	var sawTool bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if tools, ok := req["tools"].([]any); ok && len(tools) == 1 {
			tool := tools[0].(map[string]any)
			fn := tool["function"].(map[string]any)
			if fn["name"] == PythonREPLToolName {
				sawTool = true
			}
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_1",
							"type":"function",
							"function":{"name":"python_repl","arguments":"{\"code\":\"len(prompt)\"}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":4}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-2",
				"choices":[{
					"message":{"role":"assistant","content":"solution = [1, 2, 3]"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":20,"completion_tokens":6}
			}`))
		}
	}))
	defer server.Close()

	sandbox := &fakeSandbox{}
	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 5,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{MaxREPLCalls: 3, MaxIterations: 5},
		},
		SandboxFactory: func() rlm.Sandbox { return sandbox },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Solve this.",
		RunID:         "run-main",
		AgentID:       "agent-root",
		MaxIterations: 5,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !sawTool {
		t.Fatalf("request did not include %s tool", PythonREPLToolName)
	}
	if result.Answer != "solution = [1, 2, 3]" {
		t.Fatalf("answer = %q", result.Answer)
	}
	if len(sandbox.execs) != 1 || sandbox.execs[0] != "len(prompt)" {
		t.Fatalf("sandbox execs = %#v", sandbox.execs)
	}
	if result.Subcalls != 0 {
		t.Fatalf("subcalls = %d, want 0", result.Subcalls)
	}
	if result.Metadata["repl_calls"] != 1 {
		t.Fatalf("repl_calls metadata = %#v", result.Metadata["repl_calls"])
	}
	if got := result.Metadata["parent_total_tokens"]; got != 40 {
		t.Fatalf("parent_total_tokens = %#v, want 40", got)
	}
	if got := result.Metadata["run_id"]; got != "run-main" {
		t.Fatalf("run_id = %#v, want run-main", got)
	}
	if got := result.Metadata["agent_id"]; got != "agent-root" {
		t.Fatalf("agent_id = %#v, want agent-root", got)
	}
	if got := result.Metadata["output_namespace"]; got != "runs/run-main/agents/agent-root" {
		t.Fatalf("output_namespace = %#v", got)
	}
	events, ok := result.Metadata["trajectory_events"].([]Event)
	if !ok || len(events) == 0 {
		t.Fatalf("trajectory_events missing or wrong type: %#v", result.Metadata["trajectory_events"])
	}
}

func TestREPLRunnerRepairsToolArgumentError(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_bad",
							"type":"function",
							"function":{"name":"python_repl","arguments":"{\"code\":\"unterminated"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":64}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-2",
				"choices":[{
					"message":{"role":"assistant","content":"intermediate"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":20,"completion_tokens":3}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-repair",
				"choices":[{
					"message":{"role":"assistant","content":"status: partial\nanswer: solution = [1, 2, 3]\nchecks: recovered from invalid tool args"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":30,"completion_tokens":12}
			}`))
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 1,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget:                     BudgetConfig{MaxREPLCalls: 3, MaxIterations: 3},
			ToolErrorRepairMaxAttempts: 1,
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Solve this.",
		MaxIterations: 1,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(result.Answer, "solution = [1, 2, 3]") {
		t.Fatalf("answer=%q", result.Answer)
	}
	if calls != 3 {
		t.Fatalf("calls=%d want 3", calls)
	}
}

func TestREPLRunnerRejectsCompletionTokenOverrun(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_1",
							"type":"function",
							"function":{"name":"python_repl","arguments":"{\"code\":\"len(prompt)\"}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":4}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-2",
				"choices":[{
					"message":{"role":"assistant","content":""},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":20,"completion_tokens":512}
			}`))
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 5,
				MaxTokens:     64,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{MaxREPLCalls: 3, MaxIterations: 5},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	_, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Solve this.",
		MaxIterations: 5,
	}, rlm.Environment{})
	if err == nil {
		t.Fatalf("Run returned nil error")
	}
	if !strings.Contains(err.Error(), "completion exceeded configured max tokens") {
		t.Fatalf("error = %q", err)
	}
}

func TestValidateREPLAttemptOutputAllowsCompactVisibleAnswerAfterToolCallOverrun(t *testing.T) {
	output := engine.EngineOutput{
		AssistantText: "solution = [1, 2, 3]",
		Iterations: []engine.IterationUsage{
			{
				CompletionTokens: 512,
				FinishReason:     "tool_calls",
			},
			{
				CompletionTokens: 8,
				FinishReason:     "stop",
			},
		},
	}
	if err := validateREPLAttemptOutput(output, nil, 64); err != nil {
		t.Fatalf("validateREPLAttemptOutput returned error: %v", err)
	}
}

func TestValidateREPLAttemptOutputAllowsCompactFinalizeTokenOverrun(t *testing.T) {
	output := engine.EngineOutput{
		AssistantText: "solution = [1, 2, 3]",
		Iterations: []engine.IterationUsage{{
			CompletionTokens: 512,
			FinishReason:     "max_iterations_finalize",
		}},
	}
	if err := validateREPLAttemptOutput(output, nil, 64); err != nil {
		t.Fatalf("validateREPLAttemptOutput returned error: %v", err)
	}
}

func TestREPLRunnerCapsREPLToolResultPayload(t *testing.T) {
	var toolPayloads []map[string]any
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		toolPayloads = append(toolPayloads, extractToolPayloadsFromMessages(req["messages"])...)

		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_1",
							"type":"function",
							"function":{"name":"python_repl","arguments":"{\"code\":\"print(prompt)\"}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":4}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-2",
				"choices":[{
					"message":{"role":"assistant","content":"solution = 42"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":20,"completion_tokens":6}
			}`))
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 5,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget:                 BudgetConfig{MaxREPLCalls: 3, MaxIterations: 5},
			REPLToolResultMaxChars: 80,
		},
		SandboxFactory: func() rlm.Sandbox {
			return &fakeSandbox{output: strings.Repeat("x", 240)}
		},
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Solve this.",
		MaxIterations: 5,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "solution = 42" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if len(toolPayloads) == 0 {
		t.Fatal("expected tool payload in second model request")
	}
	payload := toolPayloads[len(toolPayloads)-1]
	output, _ := payload["output"].(string)
	if len(output) > 80 {
		t.Fatalf("tool output chars=%d want <= 80: %q", len(output), output)
	}
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata missing/wrong type: %#v", payload["metadata"])
	}
	if metadata["output_truncated"] != true {
		t.Fatalf("output_truncated=%#v", metadata["output_truncated"])
	}
	if got := int(metadata["output_original_chars"].(float64)); got != 240 {
		t.Fatalf("output_original_chars=%d want 240", got)
	}
	if got := int(metadata["output_max_chars"].(float64)); got != 80 {
		t.Fatalf("output_max_chars=%d want 80", got)
	}
}

func TestREPLBudgetedIterationCountSkipsFinalizeIterations(t *testing.T) {
	got := replBudgetedIterationCount([]engine.IterationUsage{
		{FinishReason: "tool_calls"},
		{FinishReason: "stop"},
		{FinishReason: "max_iterations_finalize"},
		{FinishReason: "empty_response_finalize"},
	})
	if got != 2 {
		t.Fatalf("replBudgetedIterationCount = %d, want 2", got)
	}
}

func TestQwenNoThinkREPLSystemPrompt(t *testing.T) {
	got := qwenNoThinkREPLSystemPrompt("Use tools carefully.")
	for _, want := range []string{
		"/no_think",
		"Qwen runtime profile: non-thinking mode.",
		"Keep non-tool text short.",
		"valid JSON arguments",
		"Use tools carefully.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q:\n%s", want, got)
		}
	}
	if strings.Count(qwenNoThinkREPLSystemPrompt(got), "/no_think") != 1 {
		t.Fatalf("prompt duplicated /no_think:\n%s", qwenNoThinkREPLSystemPrompt(got))
	}
}

func TestREPLRunnerSanitizesReasoningChannelOutput(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_1",
							"type":"function",
							"function":{"name":"python_repl","arguments":"{\"code\":\"len(prompt)\"}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":4}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-2",
				"choices":[{
					"message":{"role":"assistant","content":"<|channel>thought\n<channel|>solution = 42"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":20,"completion_tokens":6}
			}`))
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 3,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{MaxREPLCalls: 3, MaxIterations: 3},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{Prompt: "Solve", MaxIterations: 3}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "solution = 42" {
		t.Fatalf("answer=%q", result.Answer)
	}
	meta, ok := result.Metadata["output_sanitization"].(map[string]any)
	if !ok {
		t.Fatalf("missing output_sanitization: %#v", result.Metadata)
	}
	if meta["changed"] != true {
		t.Fatalf("changed=%v", meta["changed"])
	}
}

func TestREPLToolExecutorIncludesExtraTools(t *testing.T) {
	exec := &replToolExecutor{
		extraToolExecutor: fakeREPLExtraToolExecutor{},
		replToolName:      PythonREPLToolName,
		sandboxKind:       SandboxKindPython,
	}
	defs := exec.List()
	var found bool
	for _, def := range defs {
		if def.Name == "extra_test_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("extra tool was not listed: %#v", defs)
	}
	raw, err := exec.Execute(t.Context(), "extra_test_tool", json.RawMessage(`{"value":"ok"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if raw != `{"value":"ok"}` {
		t.Fatalf("raw=%s", raw)
	}
}

func TestMultiToolExecutorCombinesTools(t *testing.T) {
	t.Parallel()

	exec := MultiToolExecutor{
		fakeREPLExtraToolExecutor{},
		&HelperFactoryTools{},
	}
	defs := exec.List()
	var sawExtra, sawHelper bool
	for _, def := range defs {
		switch def.Name {
		case "extra_test_tool":
			sawExtra = true
		case EphemeralHelperSolveToolName:
			sawHelper = true
		}
	}
	if !sawExtra || !sawHelper {
		t.Fatalf("combined tool defs missing expected tools: %#v", defs)
	}
}

func TestREPLRunnerExtraToolExecutorPrefersHelperFactory(t *testing.T) {
	t.Parallel()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			EphemeralSkills: true,
			HelperFactory:   &HelperFactoryConfig{},
		},
	}
	exec := runner.extraToolExecutor(rlm.Task{Prompt: "solve"})
	if exec == nil {
		t.Fatal("extraToolExecutor=nil")
	}
	defs := exec.List()
	var sawHelper bool
	for _, def := range defs {
		switch def.Name {
		case EphemeralHelperSolveToolName:
			sawHelper = true
		}
	}
	if !sawHelper {
		t.Fatalf("missing helper tool in extra surface: %#v", defs)
	}
}

type fakeREPLExtraToolExecutor struct{}

func (fakeREPLExtraToolExecutor) List() []engine.ToolDef {
	return []engine.ToolDef{{
		Name:        "extra_test_tool",
		Description: "test",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`),
	}}
}

func (fakeREPLExtraToolExecutor) Execute(_ context.Context, name string, args json.RawMessage) (string, error) {
	if name != "extra_test_tool" {
		return "", fmt.Errorf("unexpected tool %q", name)
	}
	var input struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", err
	}
	return fmt.Sprintf(`{"value":%q}`, input.Value), nil
}

func TestREPLRunnerUsesGoREPLToolForYaegiSandbox(t *testing.T) {
	var sawTool bool
	var sawGoPrompt bool
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if tools, ok := req["tools"].([]any); ok && len(tools) == 1 {
			tool := tools[0].(map[string]any)
			fn := tool["function"].(map[string]any)
			if fn["name"] == GoREPLToolName {
				sawTool = true
			}
		}
		if messages, ok := req["messages"].([]any); ok && len(messages) > 0 {
			first := messages[0].(map[string]any)
			if strings.Contains(first["content"].(string), "persistent Go REPL") &&
				strings.Contains(first["content"].(string), GoREPLToolName) {
				sawGoPrompt = true
			}
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_1",
							"type":"function",
							"function":{"name":"go_repl","arguments":"{\"code\":\"len(prompt)\"}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":4}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-2",
				"choices":[{
					"message":{"role":"assistant","content":"solution = []"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":20,"completion_tokens":6}
			}`))
		}
	}))
	defer server.Close()

	sandbox := &fakeSandbox{}
	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 5,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget:  BudgetConfig{MaxREPLCalls: 3, MaxIterations: 5},
			Sandbox: SandboxConfig{Kind: SandboxKindYaegi},
		},
		SandboxFactory: func() rlm.Sandbox { return sandbox },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Solve this.",
		MaxIterations: 5,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !sawTool {
		t.Fatalf("request did not include %s tool", GoREPLToolName)
	}
	if !sawGoPrompt {
		t.Fatalf("system prompt did not describe Go REPL")
	}
	if len(sandbox.execs) != 1 || sandbox.execs[0] != "len(prompt)" {
		t.Fatalf("sandbox execs = %#v", sandbox.execs)
	}
	if result.Metadata["sandbox_kind"] != string(SandboxKindYaegi) {
		t.Fatalf("sandbox_kind metadata = %#v", result.Metadata["sandbox_kind"])
	}
	if result.Metadata["repl_tool_name"] != GoREPLToolName {
		t.Fatalf("repl_tool_name metadata = %#v", result.Metadata["repl_tool_name"])
	}
	if result.Metadata["repl_calls"] != 1 {
		t.Fatalf("repl_calls metadata = %#v", result.Metadata["repl_calls"])
	}
}

func TestREPLRunnerAddsRLMQueryToolWhenEnabled(t *testing.T) {
	var sawPython bool
	var sawRLMQuery bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		tools, _ := req["tools"].([]any)
		for _, raw := range tools {
			tool, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			function, ok := tool["function"].(map[string]any)
			if !ok {
				continue
			}
			switch function["name"] {
			case PythonREPLToolName:
				sawPython = true
			case RLMQueryToolName:
				sawRLMQuery = true
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-1",
			"choices":[{
				"message":{"role":"assistant","content":"answer"},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":3,"completion_tokens":2}
		}`))
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 3,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{
				MaxDepth:      1,
				MaxSubcalls:   1,
				MaxIterations: 3,
			},
			RLMQueryFactory: func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc {
				return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
					return rlm.Result{Answer: "child"}, nil
				}
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	_, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Solve this.",
		MaxDepth:      1,
		MaxSubcalls:   1,
		MaxIterations: 3,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !sawPython {
		t.Fatalf("request did not include %s tool", PythonREPLToolName)
	}
	if !sawRLMQuery {
		t.Fatalf("request did not include %s tool", RLMQueryToolName)
	}
}

func TestREPLRunnerDisablesRecursionSurfaceWhenPolicyDisabled(t *testing.T) {
	var sawPython bool
	var sawRLMQuery bool
	var sawRLMWait bool
	var sawRLMResult bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		tools, _ := req["tools"].([]any)
		for _, raw := range tools {
			tool, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			function, ok := tool["function"].(map[string]any)
			if !ok {
				continue
			}
			switch function["name"] {
			case PythonREPLToolName:
				sawPython = true
			case RLMQueryToolName:
				sawRLMQuery = true
			case RLMWaitToolName:
				sawRLMWait = true
			case RLMResultToolName:
				sawRLMResult = true
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-1",
			"choices":[{
				"message":{"role":"assistant","content":"answer"},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":3,"completion_tokens":2}
		}`))
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 3,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{
				MaxDepth:      1,
				MaxSubcalls:   1,
				MaxIterations: 3,
			},
			RecursionPolicy: RecursionPolicyDisabled,
			AsyncRecursion:  true,
			RLMQueryFactory: func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc {
				return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
					return rlm.Result{Answer: "child"}, nil
				}
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	_, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Solve this.",
		MaxDepth:      1,
		MaxSubcalls:   1,
		MaxIterations: 3,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !sawPython {
		t.Fatalf("request did not include %s tool", PythonREPLToolName)
	}
	if sawRLMQuery || sawRLMWait || sawRLMResult {
		t.Fatalf("recursion tools should be disabled query=%v wait=%v result=%v", sawRLMQuery, sawRLMWait, sawRLMResult)
	}
}

func TestREPLRunnerAsyncRecursionUsesSchedulerTools(t *testing.T) {
	var sawPython bool
	var sawRLMQuery bool
	var sawRLMWait bool
	var sawRLMResult bool
	var toolPayloads []map[string]any
	var calls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 0 {
			tools, _ := req["tools"].([]any)
			for _, raw := range tools {
				tool, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				function, ok := tool["function"].(map[string]any)
				if !ok {
					continue
				}
				switch function["name"] {
				case PythonREPLToolName:
					sawPython = true
				case RLMQueryToolName:
					sawRLMQuery = true
				case RLMWaitToolName:
					sawRLMWait = true
				case RLMResultToolName:
					sawRLMResult = true
				}
			}
		}
		toolPayloads = append(toolPayloads, extractToolPayloadsFromMessages(req["messages"])...)

		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_1",
							"type":"function",
							"function":{"name":"rlm_query","arguments":"{\"prompt\":\"child task\",\"max_iterations\":2}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":4}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-2",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_2",
							"type":"function",
							"function":{"name":"rlm_wait","arguments":"{\"min_complete\":1,\"timeout_ms\":200}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":20,"completion_tokens":3}
			}`))
		case 3:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-3",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_3",
							"type":"function",
							"function":{"name":"rlm_result","arguments":"{\"child\":1}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":30,"completion_tokens":2}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-4",
				"choices":[{
					"message":{"role":"assistant","content":"async final answer"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":40,"completion_tokens":5}
			}`))
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 6,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{
				MaxDepth:       2,
				MaxSubcalls:    2,
				MaxIterations:  6,
				MaxChildTokens: 100,
			},
			AsyncRecursion: true,
			AsyncScheduler: SchedulerConfig{MaxWorkers: 1},
			RLMQueryFactory: func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc {
				return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
					return rlm.Result{
						Answer:     "child-answer",
						Iterations: 2,
						Subcalls:   0,
						Metadata: map[string]any{
							"parent_input_tokens":  9,
							"parent_output_tokens": 4,
							"parent_total_tokens":  13,
						},
					}, nil
				}
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Solve this.",
		RunID:         "run-main",
		AgentID:       "agent-root",
		MaxDepth:      2,
		MaxSubcalls:   2,
		MaxIterations: 6,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !sawPython || !sawRLMQuery || !sawRLMWait || !sawRLMResult {
		t.Fatalf("missing tools python=%v rlm_query=%v rlm_wait=%v rlm_result=%v", sawPython, sawRLMQuery, sawRLMWait, sawRLMResult)
	}

	var sawHandle bool
	var sawWaitCompleted bool
	for _, payload := range toolPayloads {
		if payload["child"] == float64(1) && payload["status"] == string(NodeStatusQueued) {
			sawHandle = true
		}
		completed, ok := payload["completed"].([]any)
		if !ok || len(completed) == 0 {
			continue
		}
		first, ok := completed[0].(map[string]any)
		if !ok {
			continue
		}
		if first["child"] == float64(1) && first["status"] == string(NodeStatusCompleted) {
			sawWaitCompleted = true
		}
	}
	if !sawHandle {
		t.Fatalf("did not observe rlm_query handle payload in tool messages: %#v", toolPayloads)
	}
	if !sawWaitCompleted {
		t.Fatalf("did not observe completed child in rlm_wait payloads: %#v", toolPayloads)
	}

	if result.Answer != "async final answer" {
		t.Fatalf("answer = %q, want async final answer", result.Answer)
	}
	if result.Subcalls != 1 {
		t.Fatalf("subcalls = %d, want 1", result.Subcalls)
	}
	if got := result.Metadata["runner"]; got != "rlm_repl_with_async_recursion" {
		t.Fatalf("runner metadata = %#v, want rlm_repl_with_async_recursion", got)
	}
	if got := result.Metadata["recursion_policy"]; got != string(RecursionPolicyOptional) {
		t.Fatalf("recursion_policy = %#v, want optional", got)
	}
	if got := result.Metadata["recursive_subcalls_used"]; got != 1 {
		t.Fatalf("recursive_subcalls_used = %#v, want 1", got)
	}
	if got := result.Metadata["child_total_tokens"]; got != 13 {
		t.Fatalf("child_total_tokens = %#v, want 13", got)
	}
	if got := result.Metadata["async_recursion"]; got != true {
		t.Fatalf("async_recursion metadata = %#v, want true", got)
	}
	trace, ok := result.Metadata["recursive_trace"].(*RecursiveTrace)
	if !ok || trace == nil {
		t.Fatalf("recursive_trace missing or wrong type: %#v", result.Metadata["recursive_trace"])
	}
	if trace.RunID != "run-main" || trace.RootNodeID != replRootNodeID {
		t.Fatalf("recursive trace identity = %+v", trace)
	}
	if len(trace.Children) != 1 {
		t.Fatalf("recursive trace children = %d, want 1: %+v", len(trace.Children), trace)
	}
	if trace.Children[0].NodeID != "root.1" || trace.Children[0].Status != NodeStatusCompleted {
		t.Fatalf("recursive trace first child = %+v", trace.Children[0])
	}
}

func TestREPLRunnerRequiresAtLeastOneRecursiveQueryWhenPolicyRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-1",
			"choices":[{
				"message":{"role":"assistant","content":"final without recursion"},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":3,"completion_tokens":2}
		}`))
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 2,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{
				MaxDepth:      1,
				MaxSubcalls:   1,
				MaxIterations: 2,
			},
			RecursionPolicy: RecursionPolicyRequired,
			AsyncRecursion:  true,
			RLMQueryFactory: func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc {
				return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
					return rlm.Result{Answer: "child"}, nil
				}
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	_, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Solve this.",
		MaxDepth:      1,
		MaxSubcalls:   1,
		MaxIterations: 2,
	}, rlm.Environment{})
	if err == nil {
		t.Fatal("expected required recursion error")
	}
	if !strings.Contains(err.Error(), "recursion_policy=required") {
		t.Fatalf("error = %v", err)
	}
}

func TestREPLRunnerStagedPhasesConstrainToolSurface(t *testing.T) {
	var requestTools [][]string
	var toolPayloads []map[string]any
	var calls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requestTools = append(requestTools, requestToolNames(req["tools"]))
		toolPayloads = append(toolPayloads, extractToolPayloadsFromMessages(req["messages"])...)

		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-inspect-1",
				"choices":[{
					"message":{"role":"assistant","tool_calls":[{
						"id":"call_inspect",
						"type":"function",
						"function":{"name":"python_repl","arguments":"{}"}
					}]},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":4}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-inspect-2",
				"choices":[{
					"message":{"role":"assistant","content":"inspection complete"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":11,"completion_tokens":2}
			}`))
		case 3:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-query-1",
				"choices":[{
					"message":{"role":"assistant","tool_calls":[{
						"id":"call_query",
						"type":"function",
						"function":{"name":"rlm_query","arguments":"{}"}
					}]},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":3}
			}`))
		case 4:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-query-2",
				"choices":[{
					"message":{"role":"assistant","content":"critic submitted"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":13,"completion_tokens":2}
			}`))
		case 5:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-wait-1",
				"choices":[{
					"message":{"role":"assistant","tool_calls":[{
						"id":"call_wait",
						"type":"function",
						"function":{"name":"rlm_wait","arguments":"{}"}
					}]},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":14,"completion_tokens":3}
			}`))
		case 6:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-wait-2",
				"choices":[{
					"message":{"role":"assistant","content":"critic received"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":15,"completion_tokens":2}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final",
				"choices":[{
					"message":{"role":"assistant","content":"solution = ok"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":16,"completion_tokens":2}
			}`))
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:       "openai_compat",
				BaseURL:        server.URL,
				AuthMode:       "none",
				Model:          "test-model",
				MaxIterations:  5,
				MaxTokens:      256,
				Timeout:        5 * time.Second,
				RequireToolUse: true,
			},
			Budget: BudgetConfig{
				MaxDepth:       2,
				MaxSubcalls:    1,
				MaxIterations:  8,
				MaxREPLCalls:   2,
				MaxChildTokens: 100,
			},
			AsyncRecursion:                   true,
			AsyncScheduler:                   SchedulerConfig{MaxWorkers: 1},
			DefaultREPLCode:                  "print(official_prompt)",
			DefaultRLMQueryPrompt:            "Default child critic prompt.",
			ChildSummaryMaxChars:             10,
			ChildSummaryRewriteOverLimit:     true,
			ChildSummaryRewriteMaxIterations: 2,
			Phases: []REPLRunnerPhase{
				{Name: "inspect", Tools: []string{PythonREPLToolName}, RequiredTools: []string{PythonREPLToolName}, MaxIterations: 1},
				{Name: "critic_query", Tools: []string{RLMQueryToolName}, RequiredTools: []string{RLMQueryToolName}, MaxIterations: 1},
				{Name: "critic_wait", Tools: []string{RLMWaitToolName, RLMResultToolName}, RequiredTools: []string{RLMWaitToolName}, MaxIterations: 1},
				{Name: "final", Final: true, MaxIterations: 1},
			},
			RLMQueryFactory: func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc {
				return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
					if strings.Contains(task.AgentID, "/summary") {
						return rlm.Result{
							Answer: "dense",
							Metadata: map[string]any{
								"parent_input_tokens":  2,
								"parent_output_tokens": 3,
								"parent_total_tokens":  5,
							},
						}, nil
					}
					if !strings.Contains(task.Prompt, "Default child critic prompt.") {
						t.Fatalf("child prompt = %q", task.Prompt)
					}
					return rlm.Result{
						Answer: "child verdict too long",
						Metadata: map[string]any{
							"parent_input_tokens":  3,
							"parent_output_tokens": 4,
							"parent_total_tokens":  7,
						},
					}, nil
				}
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Review this answer.",
		RunID:         "run-staged",
		AgentID:       "agent-root",
		MaxDepth:      2,
		MaxSubcalls:   1,
		MaxIterations: 8,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "solution = ok" {
		t.Fatalf("answer = %q", result.Answer)
	}
	if result.Subcalls != 1 {
		t.Fatalf("subcalls = %d, want 1", result.Subcalls)
	}
	wantSurfaces := [][]string{
		{PythonREPLToolName},
		nil,
		{RLMQueryToolName},
		nil,
		{RLMWaitToolName, RLMResultToolName},
		nil,
		nil,
	}
	if len(requestTools) != len(wantSurfaces) {
		t.Fatalf("request tool surfaces = %v", requestTools)
	}
	for i, want := range wantSurfaces {
		if fmt.Sprint(requestTools[i]) != fmt.Sprint(want) {
			t.Fatalf("request %d tools=%v want %v", i+1, requestTools[i], want)
		}
	}
	if got := result.Metadata["staged_phases"]; fmt.Sprint(got) != "[inspect critic_query critic_wait final]" {
		t.Fatalf("staged_phases=%#v", got)
	}
	var sawDenseSummary bool
	for _, payload := range toolPayloads {
		completed, _ := payload["completed"].([]any)
		for _, item := range completed {
			node, _ := item.(map[string]any)
			if node["summary"] == "dense" && node["summary_compaction_method"] == "rewrite" {
				sawDenseSummary = true
			}
		}
	}
	if !sawDenseSummary {
		t.Fatalf("did not observe rewritten dense summary in wait payloads: %#v", toolPayloads)
	}
}

func TestREPLRunnerStagedBraidGraphPhaseRepairsAndCarriesState(t *testing.T) {
	t.Parallel()

	var calls int
	var finalPrompt string
	var sawGraphResponseFormat bool
	var repairPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 1 {
			if rf, ok := req["response_format"].(map[string]any); ok && rf["type"] == "json_object" {
				sawGraphResponseFormat = true
			}
		}
		if calls == 2 {
			messages, _ := req["messages"].([]any)
			if len(messages) > 0 {
				last, _ := messages[len(messages)-1].(map[string]any)
				repairPrompt, _ = last["content"].(string)
			}
		}
		if calls == 3 {
			messages, _ := req["messages"].([]any)
			if len(messages) > 0 {
				last, _ := messages[len(messages)-1].(map[string]any)
				finalPrompt, _ = last["content"].(string)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-braid-invalid",
				"choices":[{
					"message":{"role":"assistant","content":"{invalid-json"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":9,"completion_tokens":4}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-braid-repair",
				"choices":[{
					"message":{"role":"assistant","content":"{\"version\":1,\"nodes\":[{\"id\":\"n1\",\"kind\":\"extract\",\"question\":\"extract context\"},{\"id\":\"n2\",\"kind\":\"cycle_solve\",\"question\":\"solve fixed point\",\"depends_on\":[\"n1\"],\"input_schema\":{\"cycle_clusters\":[[\"node_2\",\"node_5\"]]}},{\"id\":\"n3\",\"kind\":\"solve\",\"question\":\"solve requested values\",\"depends_on\":[\"n1\",\"n2\"]},{\"id\":\"n4\",\"kind\":\"verify\",\"question\":\"verify candidate\",\"depends_on\":[\"n2\",\"n3\"]},{\"id\":\"n5\",\"kind\":\"reduce\",\"depends_on\":[\"n3\",\"n4\"]}],\"final_node\":\"n5\"}"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":12}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final",
				"choices":[{
					"message":{"role":"assistant","content":"solution = repaired"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":11,"completion_tokens":3}
			}`))
		}
	}))
	defer server.Close()

	var childPrompts []string
	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 4,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{
				MaxDepth:      1,
				MaxSubcalls:   5,
				MaxIterations: 4,
			},
			Phases: []REPLRunnerPhase{
				{
					Name:                  "plan",
					OutputKind:            REPLPhaseOutputKindBraidGraph,
					BraidGraphPolicy:      BraidGraphPolicyLongCoTController,
					ResponseFormat:        json.RawMessage(`{"type":"json_object"}`),
					AutoExecuteGraphNodes: true,
					MaxIterations:         1,
				},
				{Name: "final", Final: true, MaxIterations: 1},
			},
			RLMQueryFactory: func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc {
				return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
					childPrompts = append(childPrompts, task.Prompt)
					switch {
					case strings.Contains(task.Prompt, "(cycle_solve)"):
						return rlm.Result{Answer: `status: solved answer: cycle_json: {"pass":true,"candidates":{"x":1},"checks":[{"name":"fixed_point","ok":true,"observed":1,"expected":1}]} checks: pass=true`}, nil
					default:
						return rlm.Result{Answer: "status: solved answer: child ok checks: pass=true"}, nil
					}
				}
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Use braid mode.",
		MaxDepth:      1,
		MaxSubcalls:   5,
		MaxIterations: 4,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "solution = repaired" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if len(childPrompts) != 5 {
		t.Fatalf("child prompts=%d want 5", len(childPrompts))
	}
	if !strings.Contains(childPrompts[0], "BRAID node n1 (extract)") {
		t.Fatalf("unexpected child prompt: %q", childPrompts[0])
	}
	if !strings.Contains(childPrompts[0], "Official root task:") || !strings.Contains(childPrompts[0], "Use braid mode.") {
		t.Fatalf("child prompt missing root task: %q", childPrompts[0])
	}
	if !strings.Contains(childPrompts[1], "BRAID node n2 (cycle_solve)") || !strings.Contains(childPrompts[1], "Dependency summaries:") {
		t.Fatalf("dependent child prompt missing dependency summaries: %q", childPrompts[1])
	}
	if !strings.Contains(finalPrompt, "Current braid graph:") || !strings.Contains(finalPrompt, "final_node: n5") {
		t.Fatalf("final phase prompt missing braid summary:\n%s", finalPrompt)
	}
	if !sawGraphResponseFormat {
		t.Fatal("graph phase request did not include json_object response_format")
	}
	if !strings.Contains(repairPrompt, "cycle_solve is optional") || !strings.Contains(repairPrompt, "question under 220 characters") {
		t.Fatalf("repair prompt missing compact optional cycle_solve schema guidance:\n%s", repairPrompt)
	}
	if !strings.Contains(repairPrompt, "include extract, at least one solve-like node") || !strings.Contains(repairPrompt, "shorten invalid fields instead of deleting required node kinds") {
		t.Fatalf("repair prompt missing LongCoT controller policy guidance:\n%s", repairPrompt)
	}
	if !strings.Contains(repairPrompt, "BlocksWorld-style tasks") || !strings.Contains(repairPrompt, "Do not split state-transition") {
		t.Fatalf("repair prompt missing state-transition guidance:\n%s", repairPrompt)
	}
	if !strings.Contains(repairPrompt, "checks original constraints by substituting candidate values") {
		t.Fatalf("repair prompt missing verify-node original-constraints guidance:\n%s", repairPrompt)
	}
	if !strings.Contains(repairPrompt, "Do not leave a multi-target solve with only target_nodes") || !strings.Contains(repairPrompt, "input_schema.solve_targets") {
		t.Fatalf("repair prompt missing multi-target solve contract guidance:\n%s", repairPrompt)
	}
	if calls != 3 {
		t.Fatalf("calls=%d want 3", calls)
	}
}

func TestREPLRunnerStagedBraidAutoExecuteSchedulesInitialNodes(t *testing.T) {
	t.Parallel()

	var calls int
	var requestTools [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requestTools = append(requestTools, requestToolNames(req["tools"]))
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-braid",
				"choices":[{
					"message":{"role":"assistant","content":"{\"version\":1,\"nodes\":[{\"id\":\"n1\",\"kind\":\"extract\",\"question\":\"node 1\"},{\"id\":\"n2\",\"kind\":\"solve\",\"question\":\"node 2\"},{\"id\":\"n3\",\"kind\":\"reduce\",\"depends_on\":[\"n1\",\"n2\"]}],\"final_node\":\"n3\"}"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":8,"completion_tokens":12}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final",
				"choices":[{
					"message":{"role":"assistant","content":"solution = done"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":3}
			}`))
		}
	}))
	defer server.Close()

	var childPrompts []string
	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 4,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{
				MaxDepth:      1,
				MaxSubcalls:   3,
				MaxIterations: 4,
			},
			Phases: []REPLRunnerPhase{
				{
					Name:                  "plan",
					OutputKind:            REPLPhaseOutputKindBraidGraph,
					Tools:                 []string{RLMQueryToolName},
					AutoExecuteGraphNodes: true,
					MaxIterations:         1,
					MaxGraphNodes:         4,
				},
				{Name: "final", Final: true, MaxIterations: 1},
			},
			RLMQueryFactory: func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc {
				return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
					childPrompts = append(childPrompts, task.Prompt)
					return rlm.Result{Answer: "child ok"}, nil
				}
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Use braid mode.",
		MaxDepth:      1,
		MaxSubcalls:   3,
		MaxIterations: 4,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "solution = done" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if len(requestTools) < 1 || len(requestTools[0]) != 0 {
		t.Fatalf("braid phase should not advertise rlm_query tool surface: %#v", requestTools)
	}
	if len(childPrompts) != 3 {
		t.Fatalf("child prompts=%d want 3", len(childPrompts))
	}
	if !strings.Contains(childPrompts[0], "BRAID node n1 (extract)") {
		t.Fatalf("first child prompt=%q", childPrompts[0])
	}
	if !strings.Contains(childPrompts[0], "Official root task:") || !strings.Contains(childPrompts[0], "Use braid mode.") {
		t.Fatalf("first child prompt missing root task=%q", childPrompts[0])
	}
	if !strings.Contains(childPrompts[1], "BRAID node n2 (solve)") {
		t.Fatalf("second child prompt=%q", childPrompts[1])
	}
	if !strings.Contains(childPrompts[2], "BRAID node n3 (reduce)") || !strings.Contains(childPrompts[2], "Dependency summaries:") {
		t.Fatalf("third child prompt missing dependency summaries=%q", childPrompts[2])
	}
}

func TestExecutePhaseBraidGraphUsesPreferredHelperBeforeChild(t *testing.T) {
	t.Parallel()

	helper := &HelperFactoryTools{Config: HelperFactoryConfig{
		Language: HelperLanguageGo,
		PresetSource: `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": "solution = helper"}
}`,
	}}
	toolExec := &replToolExecutor{
		subcallsEnabled:   true,
		helperFactory:     helper,
		extraToolExecutor: helper,
		rlmQuery: func(context.Context, rlm.Task, rlm.Environment) (rlm.Result, error) {
			t.Fatal("rlm_query should not be called for helper-preferred node with valid helper")
			return rlm.Result{}, nil
		},
	}
	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{
				ID:           "n_solve",
				Kind:         "solve",
				Question:     "Compute the exact answer.",
				HelperPolicy: BraidNodeHelperPolicyPreferred,
			},
		},
		FinalNode: "n_solve",
	}
	var output engine.EngineOutput
	err := executePhaseBraidGraph(
		context.Background(),
		"graph_fanout",
		REPLRunnerPhase{AutoExecuteGraphNodes: true},
		toolExec,
		graph,
		"Return solution = ...",
		1,
		&output,
	)
	if err != nil {
		t.Fatalf("executePhaseBraidGraph() error = %v", err)
	}
	if len(output.ToolCalls) != 2 || output.ToolCalls[0].Name != EphemeralHelperSolveToolName {
		t.Fatalf("tool calls=%#v, want helper call + solver telemetry", output.ToolCalls)
	}
	var foundTelemetry bool
	for _, tc := range output.ToolCalls {
		if tc.Name == "solver_state_telemetry" {
			foundTelemetry = true
		}
	}
	if !foundTelemetry {
		t.Fatal("missing solver_state_telemetry tool call")
	}
	handoff := latestBraidFinalHandoff(output)
	if !strings.Contains(handoff, `"verified_answer": "solution = helper"`) {
		t.Fatalf("missing compact final handoff: %s", handoff)
	}
	prompt := buildREPLPhasePrompt("large original prompt should be omitted", REPLRunnerPhase{Name: "final", Final: true}, output, replRunnerRunState{braidGraph: graph})
	if !strings.Contains(prompt, "Verified braid final handoff") || !strings.Contains(prompt, "solution = helper") {
		t.Fatalf("final prompt missing handoff:\n%s", prompt)
	}
	if strings.Contains(prompt, "large original prompt should be omitted") || strings.Contains(prompt, "Prior phase tool outputs") {
		t.Fatalf("final prompt retained bulky prior context:\n%s", prompt)
	}
}

func TestVerifiedAnswerFromBraidFinalHandoff(t *testing.T) {
	t.Parallel()

	answer, ok := verifiedAnswerFromBraidFinalHandoff(`{
  "final_node": "n_reduce",
  "verified_answer": "solution = [[1,0,2]]",
  "required_output": "return exactly this answer line"
}`)
	if !ok {
		t.Fatal("verified answer not found")
	}
	if answer != "solution = [[1,0,2]]" {
		t.Fatalf("answer=%q", answer)
	}

	if answer, ok := verifiedAnswerFromBraidFinalHandoff(`{"verified_answer":"not a solution line"}`); ok {
		t.Fatalf("unexpected answer=%q", answer)
	}
}

func TestBraidGraphRewriteSummaryReportsNormalizationChanges(t *testing.T) {
	t.Parallel()

	before := BraidGraph{
		FinalNode: "n_solve",
		Nodes: []BraidNode{{
			ID:              "n_solve",
			Kind:            "solve",
			MaxSummaryChars: 25000,
		}},
	}
	after := NormalizeBraidGraphForPolicy(before, BraidGraphPolicyLongCoTController, 8)
	summary := braidGraphRewriteSummary(before, after)
	for _, want := range []string{"final_node:n_solve->", "nodes:1->", "node_ids:n_solve->"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("rewrite summary missing %q: %s", want, summary)
		}
	}
}

func TestBraidGraphRuntimeSplitRecordsGraphRewrite(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{{
			ID:            "n_solve",
			Kind:          "solve",
			Question:      "Solve typed symbolic trace.",
			HelperPolicy:  BraidNodeHelperPolicyPreferred,
			Archetype:     BraidScaffoldClassSymbolicTrace,
			ScaffoldClass: BraidScaffoldClassSymbolicTrace,
			ScaffoldID:    BraidScaffoldIDTypeInferenceV1,
			InputSchema:   map[string]any{"prompt": "trace bindings"},
		}},
		FinalNode: "n_solve",
	}
	recorder := NewRecorder()
	before := cloneBraidGraph(graph)
	applyBraidGraphSplits(&graph, &replToolExecutor{recorder: recorder}, "braid")
	recordBraidGraphRewriteIfChanged(recorder, REPLRunnerPhase{Name: "braid"}, "graph_runtime_split", before, graph)

	var saw bool
	for _, event := range recorder.Events() {
		if event.Braid == nil || event.Braid.Status != "graph_runtime_split" {
			continue
		}
		saw = true
		if !strings.Contains(event.Braid.Message, "nodes:1->") {
			t.Fatalf("rewrite message=%q, want node-count delta", event.Braid.Message)
		}
		if !strings.Contains(event.Braid.Message, "node_ids:n_solve->") {
			t.Fatalf("rewrite message=%q, want node id delta", event.Braid.Message)
		}
	}
	if !saw {
		t.Fatal("missing graph_runtime_split braid event")
	}
}

func TestRenderBraidFinalHandoffPreservesLongVerifiedAnswer(t *testing.T) {
	t.Parallel()

	moves := make([]string, 0, 180)
	for i := 0; i < 180; i++ {
		moves = append(moves, fmt.Sprintf("[%d,%d,%d]", i, i%3, (i+1)%3))
	}
	answer := "solution = [" + strings.Join(moves, ",") + "]"
	summary := "status: completed summary: status: solved answer: " + answer + " checks: reduce forwarded verified solve answer."

	handoff := renderBraidFinalHandoff(BraidGraph{FinalNode: "n_reduce"}, summary)
	got, ok := verifiedAnswerFromBraidFinalHandoff(handoff)
	if !ok {
		t.Fatalf("verified answer missing from handoff: %s", handoff)
	}
	if got != answer {
		t.Fatalf("verified answer was truncated or changed\ngot:  %q\nwant: %q", got, answer)
	}
	if !strings.Contains(handoff, "...[truncated]") {
		t.Fatalf("expected compact final_summary to be truncated: %s", handoff)
	}
}

func TestRenderBraidFinalHandoffExtractsNodeArtifactAnswer(t *testing.T) {
	t.Parallel()

	summary := `{"status":"solved","answer":"solution = [[1,0,2]]","checks":["verified"],"confidence":1}`
	handoff := renderBraidFinalHandoff(BraidGraph{FinalNode: "n_reduce"}, summary)
	got, ok := verifiedAnswerFromBraidFinalHandoff(handoff)
	if !ok {
		t.Fatalf("verified answer missing from handoff: %s", handoff)
	}
	if got != "solution = [[1,0,2]]" {
		t.Fatalf("verified answer=%q", got)
	}
}

func TestExecutePhaseBraidGraphCanDisableHelperFirstFallback(t *testing.T) {
	t.Parallel()

	helper := &HelperFactoryTools{Config: HelperFactoryConfig{
		Language: HelperLanguageGo,
		PresetSource: `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": false}
}`,
	}}
	toolExec := &replToolExecutor{
		subcallsEnabled:   true,
		helperFactory:     helper,
		extraToolExecutor: helper,
		rlmQuery: func(context.Context, rlm.Task, rlm.Environment) (rlm.Result, error) {
			t.Fatal("rlm_query should not be called when helper-first fallback is disabled")
			return rlm.Result{}, nil
		},
	}
	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{{
			ID:           "n_solve",
			Kind:         "solve",
			Question:     "Compute the exact answer.",
			HelperPolicy: BraidNodeHelperPolicyPreferred,
		}},
		FinalNode: "n_solve",
	}
	var output engine.EngineOutput
	err := executePhaseBraidGraph(
		context.Background(),
		"graph_fanout",
		REPLRunnerPhase{
			AutoExecuteGraphNodes:      true,
			DisableHelperFirstFallback: true,
		},
		toolExec,
		graph,
		"Return solution = ...",
		1,
		&output,
	)
	if err == nil {
		t.Fatal("executePhaseBraidGraph() succeeded despite failed required helper-first")
	}
	if !strings.Contains(err.Error(), "helper-first failed") {
		t.Fatalf("error=%v, want helper-first failure", err)
	}
	if len(output.ToolCalls) != 1 || output.ToolCalls[0].Name != EphemeralHelperSolveToolName {
		t.Fatalf("tool calls=%#v, want one helper call and no fallback child", output.ToolCalls)
	}
}

func TestREPLRunnerStagedBraidAutoExecuteRejectsBlockedFinalNode(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-braid",
				"choices":[{
					"message":{"role":"assistant","content":"{\"version\":1,\"nodes\":[{\"id\":\"n1\",\"kind\":\"extract\",\"question\":\"node 1\"},{\"id\":\"n2\",\"kind\":\"reduce\",\"depends_on\":[\"n1\"]}],\"final_node\":\"n2\"}"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":8,"completion_tokens":12}
			}`))
		default:
			t.Fatalf("unexpected final synthesis call after blocked braid node")
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 4,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{
				MaxDepth:      1,
				MaxSubcalls:   2,
				MaxIterations: 4,
			},
			Phases: []REPLRunnerPhase{
				{
					Name:                  "plan",
					OutputKind:            REPLPhaseOutputKindBraidGraph,
					AutoExecuteGraphNodes: true,
					MaxIterations:         1,
				},
				{Name: "final", Final: true, MaxIterations: 1},
			},
			RLMQueryFactory: func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc {
				return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
					if strings.Contains(task.Prompt, "BRAID node n2") {
						return rlm.Result{Answer: "status: blocked\nanswer:\nchecks: verification did not pass"}, nil
					}
					return rlm.Result{Answer: "status: solved\nanswer: extracted\nchecks: ok"}, nil
				}
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	_, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Use braid mode.",
		MaxDepth:      1,
		MaxSubcalls:   2,
		MaxIterations: 4,
	}, rlm.Environment{})
	if err == nil {
		t.Fatal("Run succeeded despite blocked final braid node")
	}
	if !strings.Contains(err.Error(), "braid node") || !strings.Contains(err.Error(), "did not complete") {
		t.Fatalf("Run error=%v, want blocked braid node failure", err)
	}
	if calls != 1 {
		t.Fatalf("model calls=%d want 1", calls)
	}
}

func TestREPLRunnerCompactChildSummaryCanNormalizeBeforeSubmit(t *testing.T) {
	t.Parallel()

	var sawTaskPrompt bool
	var sawRawAnswer bool
	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			ChildSummaryNormalizeBeforeSubmit: true,
			ChildSummaryRewriteMaxIterations:  1,
		},
	}
	toolExec := &replToolExecutor{
		rlmQuery: func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
			if strings.Contains(task.Prompt, "Child task:") && strings.Contains(task.Prompt, "Which values are needed?") {
				sawTaskPrompt = true
			}
			if strings.Contains(task.Prompt, "Child answer:") && strings.Contains(task.Prompt, "The answer is probably") {
				sawRawAnswer = true
			}
			if task.MaxDepth != 0 || task.MaxSubcalls != 0 {
				t.Fatalf("summary task budgets depth=%d subcalls=%d, want 0", task.MaxDepth, task.MaxSubcalls)
			}
			if task.MaxIterations != 1 {
				t.Fatalf("summary task max iterations=%d want 1", task.MaxIterations)
			}
			return rlm.Result{Answer: "status: ok\nanswer: solution = [1, 2]\nblockers: none"}, nil
		},
	}

	summary, truncated, meta := runner.compactChildSummary(
		context.Background(),
		toolExec,
		rlm.Task{Prompt: "Which values are needed?", RunID: "run-1", AgentID: "agent/root"},
		"The answer is probably solution = [1, 2], with intermediate derivation that the parent does not need.",
		1000,
	)
	if truncated {
		t.Fatal("summary should not be deterministically truncated")
	}
	if summary != "status: ok answer: solution = [1, 2] blockers: none" {
		t.Fatalf("summary = %q", summary)
	}
	if meta["summary_compaction_method"] != "rewrite" || meta["summary_rewrite_reason"] != "presubmit" {
		t.Fatalf("metadata = %#v", meta)
	}
	if !sawTaskPrompt || !sawRawAnswer {
		t.Fatalf("rewrite prompt missing context: sawTaskPrompt=%v sawRawAnswer=%v", sawTaskPrompt, sawRawAnswer)
	}
}

func TestREPLRunnerCompactChildSummaryRejectsInvalidStructuredRewrite(t *testing.T) {
	t.Parallel()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			ChildSummaryNormalizeBeforeSubmit: true,
			ChildSummaryRewriteMaxIterations:  1,
		},
	}
	toolExec := &replToolExecutor{
		rlmQuery: func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
			return rlm.Result{Answer: "status: blocked\nanswer:\nblockers: circular dependency prevents solving"}, nil
		},
	}
	raw := strings.Join([]string{
		"status: solved",
		"answer: solution = [1, 2]",
		"checks: compact verification",
	}, "\n")

	summary, truncated, meta := runner.compactChildSummary(
		context.Background(),
		toolExec,
		rlm.Task{Prompt: "Verify the answer", RunID: "run-1", AgentID: "agent/root"},
		raw,
		1000,
	)
	if truncated {
		t.Fatal("summary should not be truncated")
	}
	if summary != "status: solved answer: solution = [1, 2] checks: compact verification" {
		t.Fatalf("summary = %q", summary)
	}
	if meta["summary_rewrite_used"] == true {
		t.Fatalf("invalid rewrite should not be used: %#v", meta)
	}
	if !strings.Contains(fmt.Sprint(meta["summary_rewrite_error"]), "invalid child summary rewrite") {
		t.Fatalf("metadata = %#v, want invalid child summary rewrite", meta)
	}
}

func TestREPLRunnerStagedPhaseCanAutoExecuteSingleRequiredTool(t *testing.T) {
	t.Parallel()

	var calls int
	var firstRequest map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 1 {
			firstRequest = req
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final",
				"choices":[{
					"message":{"role":"assistant","content":"solution = auto"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":3}
			}`))
		default:
			t.Fatalf("unexpected model call %d", calls)
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:       "openai_compat",
				BaseURL:        server.URL,
				AuthMode:       "none",
				Model:          "test-model",
				MaxIterations:  3,
				MaxTokens:      256,
				Timeout:        5 * time.Second,
				RequireToolUse: true,
			},
			Budget:            BudgetConfig{MaxIterations: 3},
			ExtraToolExecutor: fakeREPLExtraToolExecutor{},
			Phases: []REPLRunnerPhase{
				{
					Name:                    "helper",
					Tools:                   []string{"extra_test_tool"},
					RequiredTools:           []string{"extra_test_tool"},
					MaxIterations:           1,
					AutoExecuteRequiredTool: true,
				},
				{Name: "final", Final: true, MaxIterations: 1},
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Use the helper.",
		RunID:         "run-auto",
		AgentID:       "agent-auto",
		MaxIterations: 3,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "solution = auto" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if got := fmt.Sprint(result.Metadata["tool_names"]); !strings.Contains(got, "extra_test_tool") {
		t.Fatalf("tool names=%s", got)
	}
	if _, ok := firstRequest["tool_choice"]; ok {
		t.Fatalf("auto-executed helper phase should not send tool_choice: %#v", firstRequest["tool_choice"])
	}
	if _, ok := firstRequest["tools"]; ok {
		t.Fatalf("auto-executed helper phase should not advertise tools: %#v", firstRequest["tools"])
	}
}

func TestREPLRunnerRepairsFinalMissingSolutionLine(t *testing.T) {
	t.Parallel()

	var calls int
	var repairPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 2 {
			messages, _ := req["messages"].([]any)
			if len(messages) > 0 {
				last, _ := messages[len(messages)-1].(map[string]any)
				repairPrompt, _ = last["content"].(string)
			}
			if _, ok := req["tools"]; ok {
				t.Fatalf("repair call should not advertise tools: %#v", req["tools"])
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final-invalid",
				"choices":[{
					"message":{"role":"assistant","content":"go_repl"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":3}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final-repair",
				"choices":[{
					"message":{"role":"assistant","content":"solution = repaired"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":14,"completion_tokens":4}
			}`))
		default:
			t.Fatalf("unexpected model call %d", calls)
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:       "openai_compat",
				BaseURL:        server.URL,
				AuthMode:       "none",
				Model:          "test-model",
				MaxIterations:  2,
				MaxTokens:      256,
				Timeout:        5 * time.Second,
				RequireToolUse: false,
			},
			Budget:                       BudgetConfig{MaxIterations: 2},
			ExtractSolutionLine:          true,
			FinalSolutionLineRequired:    true,
			FinalAnswerRepairMaxAttempts: 1,
			Phases: []REPLRunnerPhase{
				{Name: "final", Final: true, MaxIterations: 1},
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Return solution = repaired.",
		RunID:         "run-repair",
		AgentID:       "agent-repair",
		MaxIterations: 2,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "solution = repaired" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if !strings.Contains(repairPrompt, "Previous final response") || !strings.Contains(repairPrompt, "go_repl") {
		t.Fatalf("repair prompt missing invalid response: %q", repairPrompt)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
}

func TestREPLRunnerRepairsFinalLengthStop(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final-length",
				"choices":[{
					"message":{"role":"assistant","content":"long derivation without final format"},
					"finish_reason":"length"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":256}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final-repair",
				"choices":[{
					"message":{"role":"assistant","content":"solution = repaired"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":14,"completion_tokens":4}
			}`))
		default:
			t.Fatalf("unexpected model call %d", calls)
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 2,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget:                       BudgetConfig{MaxIterations: 2},
			ExtractSolutionLine:          true,
			FinalSolutionLineRequired:    true,
			FinalAnswerRepairMaxAttempts: 1,
			Phases: []REPLRunnerPhase{
				{Name: "final", Final: true, MaxIterations: 1},
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Return solution = repaired.",
		RunID:         "run-repair-length",
		AgentID:       "agent-repair-length",
		MaxIterations: 2,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "solution = repaired" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
}

func TestREPLRunnerRepairsChildSummaryPseudoToolCall(t *testing.T) {
	t.Parallel()

	var calls int
	var repairPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 2 {
			messages, _ := req["messages"].([]any)
			last, _ := messages[len(messages)-1].(map[string]any)
			repairPrompt, _ = last["content"].(string)
			if _, ok := req["tools"]; ok {
				t.Fatalf("structured repair should not advertise tools: %#v", req["tools"])
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-child-invalid",
				"choices":[{
					"message":{"role":"assistant","content":"python_repl(code=\"print(official_prompt)\")"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":8}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-child-repair",
				"choices":[{
					"message":{"role":"assistant","content":"status: blocked\nanswer:\nchecks: no sufficient information"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":14,"completion_tokens":12}
			}`))
		default:
			t.Fatalf("unexpected model call %d", calls)
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 2,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget:                       BudgetConfig{MaxIterations: 2},
			FinalAnswerRepairMaxAttempts: 1,
			Phases: []REPLRunnerPhase{{
				Name:            "child_final",
				Final:           true,
				FinalOutputKind: "child_summary",
				MaxIterations:   1,
			}},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Return compact child summary.",
		RunID:         "run-child-repair",
		AgentID:       "agent-child-repair",
		MaxIterations: 2,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(result.Answer, "status: blocked") {
		t.Fatalf("answer=%q", result.Answer)
	}
	if !strings.Contains(repairPrompt, "Structured final repair required") || !strings.Contains(repairPrompt, "python_repl") {
		t.Fatalf("repair prompt=%q", repairPrompt)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
}

func TestREPLRunnerRepairsOverlongChildSummaryAfterMaxTokens(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 2 {
			if _, ok := req["tools"]; ok {
				t.Fatalf("structured repair should not advertise tools: %#v", req["tools"])
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-child-overlong",
				"choices":[{
					"message":{"role":"assistant","content":"status: solved\nanswer: solution = [1, 2, 3]\nchecks: began a very long verification but did not finish compactly"},
					"finish_reason":"length"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":256}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-child-repair",
				"choices":[{
					"message":{"role":"assistant","content":"status: solved\nanswer: solution = [1, 2, 3]\nchecks: compact verification"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":14,"completion_tokens":12}
			}`))
		default:
			t.Fatalf("unexpected model call %d", calls)
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 2,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget:                       BudgetConfig{MaxIterations: 2},
			FinalAnswerRepairMaxAttempts: 1,
			Phases: []REPLRunnerPhase{{
				Name:            "child_final",
				Final:           true,
				FinalOutputKind: "child_summary",
				MaxIterations:   1,
			}},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Return compact child summary.",
		RunID:         "run-child-overlong",
		AgentID:       "agent-child-overlong",
		MaxIterations: 2,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "status: solved\nanswer: solution = [1, 2, 3]\nchecks: compact verification" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
}

func TestValidateChildSummaryRejectsRuntimeProtocolBlocker(t *testing.T) {
	t.Parallel()

	err := validateChildSummaryFinalText(strings.Join([]string{
		"status: blocked",
		"answer:",
		"checks: remaining depth is 0 and rlm_query is unavailable",
	}, "\n"))
	if err == nil {
		t.Fatal("validateChildSummaryFinalText() succeeded for runtime protocol blocker")
	}
	if !strings.Contains(err.Error(), "forbidden runtime protocol detail") {
		t.Fatalf("validateChildSummaryFinalText() err=%v, want forbidden runtime protocol detail", err)
	}
}

func TestValidateChildSummaryAcceptsNodeArtifact(t *testing.T) {
	t.Parallel()

	text := `{"status":"solved","answer":"solution = 42","checks":["computed deterministically"],"confidence":0.9}`
	if err := validateChildSummaryFinalText(text); err != nil {
		t.Fatalf("validateChildSummaryFinalText() structured artifact error = %v", err)
	}
}

func TestValidateChildSummaryRejectsNodeArtifactRuntimeProtocolBlocker(t *testing.T) {
	t.Parallel()

	text := `{"status":"blocked","answer":"","checks":["remaining depth is 0 and rlm_query is unavailable"],"confidence":0.2}`
	err := validateChildSummaryFinalText(text)
	if err == nil {
		t.Fatal("validateChildSummaryFinalText() succeeded for structured runtime protocol blocker")
	}
	if !strings.Contains(err.Error(), "forbidden runtime protocol detail") {
		t.Fatalf("validateChildSummaryFinalText() err=%v, want forbidden runtime protocol detail", err)
	}
}

func TestValidateChildSummaryRejectsCircularDependencyBlocker(t *testing.T) {
	t.Parallel()

	err := validateChildSummaryFinalText(strings.Join([]string{
		"status: blocked",
		"answer:",
		"checks: circular dependency among nodes 2-7 prevents unique solution",
	}, "\n"))
	if err == nil {
		t.Fatal("validateChildSummaryFinalText() succeeded for circular dependency blocker")
	}
	if !strings.Contains(err.Error(), "dependency-cycle constraint") {
		t.Fatalf("validateChildSummaryFinalText() err=%v, want dependency-cycle constraint", err)
	}
}

func TestValidateChildSummaryLengthUsesCharactersNotBytes(t *testing.T) {
	t.Parallel()

	text := strings.Join([]string{
		"status: solved",
		"answer: solution = ok",
		"checks: " + strings.Repeat("é", 1155),
	}, "\n")
	if chars := runeLen(strings.TrimSpace(text)); chars != 1200 {
		t.Fatalf("test summary chars=%d, want 1200", chars)
	}
	if bytes := len(strings.TrimSpace(text)); bytes <= 1200 {
		t.Fatalf("test summary bytes=%d, want >1200", bytes)
	}
	if err := validateChildSummaryFinalText(text); err != nil {
		t.Fatalf("validateChildSummaryFinalText() err=%v, want nil", err)
	}
}

func TestStructuredFinalRepairPromptWarnsAboutCircularDependencyBlockers(t *testing.T) {
	t.Parallel()

	prompt := buildStructuredFinalRepairPrompt(
		"child task",
		REPLRunnerPhase{Name: "child_final", FinalOutputKind: "child_summary"},
		engine.EngineOutput{},
		"status: blocked\nanswer:\nchecks: circular dependency",
		fmt.Errorf("invalid"),
		1,
		3,
	)
	for _, want := range []string{
		"Repair attempt: 1 of 3.",
		"Do not mark circular-looking dependencies as blocked",
		"will be rejected again",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("repair prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestREPLRunnerExecutesREPLCodePhaseWithoutProviderToolCall(t *testing.T) {
	t.Parallel()

	var calls int
	var advertisedTools []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 1 {
			advertisedTools = requestToolNames(req["tools"])
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-code",
				"choices":[{
					"message":{"role":"assistant","content":"print(6 * 7)"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":5}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final",
				"choices":[{
					"message":{"role":"assistant","content":"solution = 42"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":3}
			}`))
		default:
			t.Fatalf("unexpected model call %d", calls)
		}
	}))
	defer server.Close()

	sandbox := &fakeSandbox{output: "42"}
	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 2,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{MaxREPLCalls: 1, MaxIterations: 2},
			Phases: []REPLRunnerPhase{
				{
					Name:          "scratch",
					OutputKind:    REPLPhaseOutputKindREPLCode,
					Tools:         []string{PythonREPLToolName},
					MaxIterations: 1,
				},
				{
					Name:          "final",
					Final:         true,
					MaxIterations: 1,
				},
			},
			FinalSolutionLineRequired: true,
		},
		SandboxFactory: func() rlm.Sandbox { return sandbox },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Compute 6*7.",
		RunID:         "run-repl-code",
		AgentID:       "agent-repl-code",
		MaxIterations: 2,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(advertisedTools) != 0 {
		t.Fatalf("repl_code phase advertised provider tools: %v", advertisedTools)
	}
	if len(sandbox.execs) != 1 || sandbox.execs[0] != "print(6 * 7)" {
		t.Fatalf("sandbox execs=%v", sandbox.execs)
	}
	if result.Answer != "solution = 42" {
		t.Fatalf("answer=%q", result.Answer)
	}
}

func TestREPLRunnerRepairsREPLCodePhaseWithEmptyOutput(t *testing.T) {
	t.Parallel()

	var calls int
	var repairPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 2 {
			if messages, ok := req["messages"].([]any); ok && len(messages) > 0 {
				if msg, ok := messages[len(messages)-1].(map[string]any); ok {
					repairPrompt, _ = msg["content"].(string)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-code",
				"choices":[{
					"message":{"role":"assistant","content":"x = 6 * 7"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":5}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-code-repair",
				"choices":[{
					"message":{"role":"assistant","content":"print(6 * 7)"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":14,"completion_tokens":5}
			}`))
		case 3:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final",
				"choices":[{
					"message":{"role":"assistant","content":"solution = 42"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":3}
			}`))
		default:
			t.Fatalf("unexpected model call %d", calls)
		}
	}))
	defer server.Close()

	sandbox := &fakeSandbox{outputs: []string{"   ", "42"}}
	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 2,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{MaxREPLCalls: 2, MaxIterations: 4},
			Phases: []REPLRunnerPhase{
				{
					Name:              "scratch",
					OutputKind:        REPLPhaseOutputKindREPLCode,
					Tools:             []string{PythonREPLToolName},
					RequireToolOutput: true,
					MaxIterations:     1,
				},
				{
					Name:          "final",
					Final:         true,
					MaxIterations: 1,
				},
			},
			FinalSolutionLineRequired:  true,
			ToolErrorRepairMaxAttempts: 1,
		},
		SandboxFactory: func() rlm.Sandbox { return sandbox },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Compute 6*7.",
		RunID:         "run-repl-code-repair",
		AgentID:       "agent-repl-code-repair",
		MaxIterations: 4,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "solution = 42" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if got, want := sandbox.execs, []string{"x = 6 * 7", "print(6 * 7)"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sandbox execs=%v want %v", got, want)
	}
	for _, want := range []string{"REPL code repair required", "produced empty output", "print a non-empty compact result"} {
		if !strings.Contains(repairPrompt, want) {
			t.Fatalf("repair prompt missing %q:\n%s", want, repairPrompt)
		}
	}
}

func TestREPLRunnerCanContinueAfterREPLCodeRepairFailure(t *testing.T) {
	t.Parallel()

	var calls int
	var finalPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 3 {
			if messages, ok := req["messages"].([]any); ok && len(messages) > 0 {
				if msg, ok := messages[len(messages)-1].(map[string]any); ok {
					finalPrompt, _ = msg["content"].(string)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-code",
				"choices":[{
					"message":{"role":"assistant","content":"# no executable code"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":5}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-code-repair",
				"choices":[{
					"message":{"role":"assistant","content":"# still no executable code"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":14,"completion_tokens":5}
			}`))
		case 3:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final",
				"choices":[{
					"message":{"role":"assistant","content":"status: blocked\nanswer:\nchecks: scratch computation unavailable"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":8}
			}`))
		default:
			t.Fatalf("unexpected model call %d", calls)
		}
	}))
	defer server.Close()

	sandbox := &fakeSandbox{}
	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 3,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{MaxREPLCalls: 1, MaxIterations: 4},
			Phases: []REPLRunnerPhase{
				{
					Name:                    "scratch",
					OutputKind:              REPLPhaseOutputKindREPLCode,
					Tools:                   []string{PythonREPLToolName},
					RequireToolOutput:       true,
					ContinueOnREPLCodeError: true,
					MaxIterations:           1,
				},
				{
					Name:            "final",
					Final:           true,
					FinalOutputKind: "child_summary",
					MaxIterations:   1,
				},
			},
			ToolErrorRepairMaxAttempts: 1,
		},
		SandboxFactory: func() rlm.Sandbox { return sandbox },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Compute what you can.",
		RunID:         "run-repl-code-continue",
		AgentID:       "agent-repl-code-continue",
		MaxIterations: 4,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("model calls=%d want 3", calls)
	}
	if len(sandbox.execs) != 0 {
		t.Fatalf("sandbox execs=%v want none", sandbox.execs)
	}
	if !strings.Contains(result.Answer, "status: blocked") || !strings.Contains(result.Answer, "scratch computation unavailable") {
		t.Fatalf("answer=%q", result.Answer)
	}
	if !strings.Contains(finalPrompt, "repl_code_failure") || !strings.Contains(finalPrompt, "scratch computation unavailable") {
		t.Fatalf("final prompt missing repl_code_failure context:\n%s", finalPrompt)
	}
}

func TestREPLRunnerCanContinueAfterEmptyREPLCodeOutput(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-code-empty",
				"choices":[{
					"message":{"role":"assistant","content":""},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":0}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final",
				"choices":[{
					"message":{"role":"assistant","content":"status: blocked\nanswer:\nchecks: scratch computation unavailable"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":8}
			}`))
		case 3:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-final-repair",
				"choices":[{
					"message":{"role":"assistant","content":"status: blocked\nanswer:\nchecks: scratch computation unavailable"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":12,"completion_tokens":8}
			}`))
		default:
			t.Fatalf("unexpected model call %d", calls)
		}
	}))
	defer server.Close()

	sandbox := &fakeSandbox{}
	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 2,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{MaxREPLCalls: 1, MaxIterations: 3},
			Phases: []REPLRunnerPhase{
				{
					Name:                    "scratch",
					OutputKind:              REPLPhaseOutputKindREPLCode,
					Tools:                   []string{PythonREPLToolName},
					RequireToolOutput:       true,
					ContinueOnREPLCodeError: true,
					MaxIterations:           1,
				},
				{
					Name:            "final",
					Final:           true,
					FinalOutputKind: "child_summary",
					MaxIterations:   1,
				},
			},
		},
		SandboxFactory: func() rlm.Sandbox { return sandbox },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Compute what you can.",
		RunID:         "run-repl-code-empty",
		AgentID:       "agent-repl-code-empty",
		MaxIterations: 3,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("model calls=%d want 3", calls)
	}
	if !strings.Contains(result.Answer, "status: blocked") {
		t.Fatalf("answer=%q", result.Answer)
	}
}

func TestParseREPLCodePhaseTextExtractsSingleFence(t *testing.T) {
	t.Parallel()

	code, err := parseREPLCodePhaseText("```python\nprint(42)\n```")
	if err != nil {
		t.Fatalf("parseREPLCodePhaseText() error = %v", err)
	}
	if code != "print(42)" {
		t.Fatalf("code=%q want print(42)", code)
	}
}

func TestParseREPLCodePhaseTextRejectsCommentOnly(t *testing.T) {
	t.Parallel()

	_, err := parseREPLCodePhaseText("# derive node_0\n# node_0 = 5/2")
	if err == nil {
		t.Fatal("parseREPLCodePhaseText() succeeded for comment-only code")
	}
	if !strings.Contains(err.Error(), "executable non-comment") {
		t.Fatalf("parseREPLCodePhaseText() err=%v, want executable non-comment", err)
	}
}

func TestAutoExecuteREPLCodePhaseRequiresOutput(t *testing.T) {
	t.Parallel()

	output := engine.EngineOutput{AssistantText: "x = 1"}
	err := autoExecutePhaseREPLCode(context.Background(), "scratch", REPLRunnerPhase{
		OutputKind:        REPLPhaseOutputKindREPLCode,
		Tools:             []string{PythonREPLToolName},
		RequireToolOutput: true,
	}, &replToolExecutor{
		replToolName: PythonREPLToolName,
		sandbox:      &fakeSandbox{output: "   "},
		budget:       mustBudget(t, BudgetConfig{MaxREPLCalls: 1}),
		recorder:     NewRecorder(),
	}, &output)
	if err == nil {
		t.Fatal("autoExecutePhaseREPLCode() succeeded with empty REPL output")
	}
	if !strings.Contains(err.Error(), "produced empty output") {
		t.Fatalf("autoExecutePhaseREPLCode() err=%v, want produced empty output", err)
	}
}

func TestAutoExecuteREPLCodePhaseRequiresOutputRejectsExecutionError(t *testing.T) {
	t.Parallel()

	output := engine.EngineOutput{AssistantText: "import sympy\nprint(42)"}
	err := autoExecutePhaseREPLCode(context.Background(), "scratch", REPLRunnerPhase{
		OutputKind:        REPLPhaseOutputKindREPLCode,
		Tools:             []string{PythonREPLToolName},
		RequireToolOutput: true,
	}, &replToolExecutor{
		replToolName: PythonREPLToolName,
		sandbox:      &fakeSandbox{err: fmt.Errorf("No module named 'sympy'")},
		budget:       mustBudget(t, BudgetConfig{MaxREPLCalls: 1}),
		recorder:     NewRecorder(),
	}, &output)
	if err == nil {
		t.Fatal("autoExecutePhaseREPLCode() succeeded with REPL execution error")
	}
	if !strings.Contains(err.Error(), "sympy") {
		t.Fatalf("autoExecutePhaseREPLCode() err=%v, want sympy", err)
	}
}

func TestValidateREPLAttemptOutputForREPLCodeAllowsLengthStop(t *testing.T) {
	t.Parallel()

	err := validateREPLAttemptOutputForPhase(REPLRunnerPhase{OutputKind: REPLPhaseOutputKindREPLCode}, engine.EngineOutput{
		StopReason:    engine.StopReasonMaxTokens,
		AssistantText: "print(42)",
	}, nil, 8)
	if err != nil {
		t.Fatalf("validateREPLAttemptOutputForPhase() error = %v", err)
	}
}

func TestAutoExecuteREPLCodePhaseRequiresConfiguredCodeSubstrings(t *testing.T) {
	t.Parallel()

	output := engine.EngineOutput{AssistantText: "x = 1"}
	err := autoExecutePhaseREPLCode(context.Background(), "cycle", REPLRunnerPhase{
		OutputKind:                 REPLPhaseOutputKindREPLCode,
		Tools:                      []string{PythonREPLToolName},
		RequiredREPLCodeSubstrings: []string{"cycle_json", "print("},
	}, &replToolExecutor{
		replToolName: PythonREPLToolName,
		sandbox:      &fakeSandbox{output: "unused"},
		budget:       mustBudget(t, BudgetConfig{MaxREPLCalls: 1}),
		recorder:     NewRecorder(),
	}, &output)
	if err == nil || !strings.Contains(err.Error(), `missing required code substring "cycle_json"`) {
		t.Fatalf("autoExecutePhaseREPLCode() err=%v, want missing cycle_json", err)
	}

	output = engine.EngineOutput{AssistantText: `print("cycle_json: {}")`}
	err = autoExecutePhaseREPLCode(context.Background(), "cycle", REPLRunnerPhase{
		OutputKind:                 REPLPhaseOutputKindREPLCode,
		Tools:                      []string{PythonREPLToolName},
		RequiredREPLCodeSubstrings: []string{"cycle_json", "print("},
	}, &replToolExecutor{
		replToolName: PythonREPLToolName,
		sandbox:      &fakeSandbox{output: "cycle_json: {}"},
		budget:       mustBudget(t, BudgetConfig{MaxREPLCalls: 1}),
		recorder:     NewRecorder(),
	}, &output)
	if err != nil {
		t.Fatalf("autoExecutePhaseREPLCode() err=%v", err)
	}
}

func TestAutoExecuteREPLCodePhaseRejectsDisallowedImportsBeforeSandbox(t *testing.T) {
	t.Parallel()

	output := engine.EngineOutput{AssistantText: "import sympy\nprint(42)"}
	err := autoExecutePhaseREPLCode(context.Background(), "scratch", REPLRunnerPhase{
		OutputKind: REPLPhaseOutputKindREPLCode,
		Tools:      []string{PythonREPLToolName},
	}, &replToolExecutor{
		replToolName: PythonREPLToolName,
		sandbox:      &fakeSandbox{output: "should not run"},
		budget:       mustBudget(t, BudgetConfig{MaxREPLCalls: 1}),
		recorder:     NewRecorder(),
	}, &output)
	if err == nil || !strings.Contains(err.Error(), `disallowed third-party import "sympy"`) {
		t.Fatalf("autoExecutePhaseREPLCode() err=%v, want disallowed sympy", err)
	}
	if len(output.ToolCalls) != 0 {
		t.Fatalf("tool calls=%d want 0", len(output.ToolCalls))
	}
}

func TestAutoExecuteREPLCodePhaseRejectsVerboseCodeBeforeSandbox(t *testing.T) {
	t.Parallel()

	output := engine.EngineOutput{AssistantText: strings.Join([]string{
		"# derivation",
		"# more derivation",
		"x = 1",
		"print(x)",
	}, "\n")}
	err := autoExecutePhaseREPLCode(context.Background(), "scratch", REPLRunnerPhase{
		OutputKind:              REPLPhaseOutputKindREPLCode,
		Tools:                   []string{PythonREPLToolName},
		MaxREPLCodeLines:        3,
		MaxREPLCodeCommentLines: 1,
	}, &replToolExecutor{
		replToolName: PythonREPLToolName,
		sandbox:      &fakeSandbox{output: "should not run"},
		budget:       mustBudget(t, BudgetConfig{MaxREPLCalls: 1}),
		recorder:     NewRecorder(),
	}, &output)
	if err == nil || !strings.Contains(err.Error(), "too many code lines") {
		t.Fatalf("autoExecutePhaseREPLCode() err=%v, want too many code lines", err)
	}
	if len(output.ToolCalls) != 0 {
		t.Fatalf("tool calls=%d want 0", len(output.ToolCalls))
	}
}

func TestValidateREPLPhaseOutputValidatesCyclePacket(t *testing.T) {
	t.Parallel()

	phase := REPLRunnerPhase{Name: "packet", OutputKind: REPLPhaseOutputKindCyclePacket}
	valid := engine.EngineOutput{AssistantText: `{"unknowns":["x"],"known_values":{"a":1},"constraints":["x=a+1"],"candidate_bounds":{"x":[0,3]},"requested_outputs":["x"],"blockers":[]}`}
	if err := validateREPLPhaseOutput(phase, valid); err != nil {
		t.Fatalf("validateREPLPhaseOutput() error = %v", err)
	}

	invalid := engine.EngineOutput{AssistantText: `{"unknowns":["x"],"constraints":[]}`}
	err := validateREPLPhaseOutput(phase, invalid)
	if err == nil || !strings.Contains(err.Error(), "missing candidate_bounds") {
		t.Fatalf("validateREPLPhaseOutput() err=%v, want missing candidate_bounds", err)
	}
}

func TestShouldFilterInvalidCyclePacket(t *testing.T) {
	t.Parallel()

	phase := REPLRunnerPhase{
		OutputKind:           REPLPhaseOutputKindCyclePacket,
		FilterOverlongOutput: true,
	}
	if !shouldFilterInvalidCyclePacket(phase, engine.EngineOutput{AssistantText: "I explored the cycle"}, fmt.Errorf("parse JSON")) {
		t.Fatal("expected invalid non-empty cycle packet to be filtered")
	}
	if shouldFilterInvalidCyclePacket(phase, engine.EngineOutput{}, fmt.Errorf("parse JSON")) {
		t.Fatal("empty cycle packet should not be filtered")
	}
	if shouldFilterInvalidCyclePacket(REPLRunnerPhase{OutputKind: REPLPhaseOutputKindCyclePacket}, engine.EngineOutput{AssistantText: "I explored the cycle"}, fmt.Errorf("parse JSON")) {
		t.Fatal("cycle packet without filter enabled should not be filtered")
	}
}

func TestValidateREPLPhaseOutputValidatesCycleWitness(t *testing.T) {
	t.Parallel()

	phase := REPLRunnerPhase{Name: "witness", OutputKind: REPLPhaseOutputKindCycleWitness}
	valid := engine.EngineOutput{AssistantText: `{"version":1,"checker_kind":"bounded_search","variables":[{"name":"x","min":0,"max":3}],"constraints":[{"name":"target","op":"eq","left":{"var":"x"},"right":{"const":2}}]}`}
	if err := validateREPLPhaseOutput(phase, valid); err != nil {
		t.Fatalf("validateREPLPhaseOutput() error = %v", err)
	}

	invalid := engine.EngineOutput{AssistantText: `{"version":1,"checker_kind":"bounded_search","variables":[{"name":"x","min":0,"max":3}],"constraints":[{"name":"target","op":"eq","left":{"func":"eval","args":[{"var":"x"}]},"right":{"const":2}}]}`}
	err := validateREPLPhaseOutput(phase, invalid)
	if err == nil || !strings.Contains(err.Error(), `unsupported func "eval"`) {
		t.Fatalf("validateREPLPhaseOutput() err=%v, want unsupported func", err)
	}
}

func TestAutoCheckPhaseCycleWitnessAppendsCycleJSON(t *testing.T) {
	t.Parallel()

	output := engine.EngineOutput{AssistantText: `{"version":1,"checker_kind":"bounded_search","variables":[{"name":"x","min":0,"max":3}],"constraints":[{"name":"target","op":"eq","left":{"var":"x"},"right":{"const":2}}],"claims":{"answer":{"var":"x"}}}`}
	if err := autoCheckPhaseCycleWitness("witness", &output); err != nil {
		t.Fatalf("autoCheckPhaseCycleWitness() error = %v", err)
	}
	if len(output.ToolCalls) != 1 || output.ToolCalls[0].Name != "cycle_witness_check" {
		t.Fatalf("tool calls=%+v want cycle_witness_check", output.ToolCalls)
	}
	if len(output.ToolResults) != 1 || !strings.Contains(output.ToolResults[0].Content, `cycle_json:`) || !strings.Contains(output.ToolResults[0].Content, `"pass":true`) {
		t.Fatalf("tool results=%+v want pass=true cycle_json", output.ToolResults)
	}
}

func TestBuildREPLPhasePromptCanCarryPriorAssistantText(t *testing.T) {
	t.Parallel()

	prompt := buildREPLPhasePrompt("solve the node", REPLRunnerPhase{
		Name:                      "scratch",
		IncludePriorAssistantText: true,
	}, engine.EngineOutput{AssistantText: `{"unknowns":["x"],"constraints":["x=1"],"candidate_bounds":{"x":[1,1]}}`}, replRunnerRunState{})
	if !strings.Contains(prompt, "Prior phase assistant output:") || !strings.Contains(prompt, `"unknowns":["x"]`) {
		t.Fatalf("phase prompt missing prior assistant text:\n%s", prompt)
	}
}

func TestBuildREPLCodeFilterPromptPreservesExplorationContract(t *testing.T) {
	t.Parallel()

	prompt := buildREPLCodeFilterPrompt("solve", REPLRunnerPhase{
		Name:                       "cycle",
		RequiredREPLCodeSubstrings: []string{"cycle_json", "print("},
		MaxREPLCodeLines:           60,
		MaxREPLCodeCommentLines:    8,
		IncludePriorAssistantText:  true,
	}, engine.EngineOutput{AssistantText: `{"unknowns":["x"],"candidate_bounds":{"x":[0,10]},"constraints":["x=6"]}`}, "long exploration with candidates")
	for _, want := range []string{
		"REPL code filter required",
		"Treat it as exploration notes",
		"smallest executable witness program",
		"cycle_json, print(",
		"at most 60 non-empty lines",
		"long exploration with candidates",
		`"candidate_bounds":{"x":[0,10]}`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("filter prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildCyclePacketFilterPromptPreservesPacketContract(t *testing.T) {
	t.Parallel()

	prompt := buildCyclePacketFilterPrompt("solve", REPLRunnerPhase{Name: "packet"}, engine.EngineOutput{}, "verbose notes")
	for _, want := range []string{
		"Cycle packet filter required",
		"Return one compact raw JSON object only",
		"unknowns, known_values, constraints, candidate_bounds, requested_outputs, blockers",
		"verbose notes",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("packet filter prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildCycleWitnessRepairPromptRejectsCandidateMapContract(t *testing.T) {
	t.Parallel()

	prompt := buildCycleWitnessRepairPrompt(
		"solve",
		REPLRunnerPhase{Name: "witness", OutputKind: REPLPhaseOutputKindCycleWitness},
		engine.EngineOutput{AssistantText: `{"unknowns":["node_2"],"candidate_bounds":{"node_2":[0,2000]},"constraints":["node_2 = 1132"]}`},
		`{"node_2":1132}`,
		fmt.Errorf(`cycle witness: parse JSON: json: unknown field "node_2"`),
	)
	for _, want := range []string{
		"Cycle witness repair required",
		"bounded-search spec",
		"Do not include markdown, prose, code, cycle_json, or a direct candidate map",
		`"checker_kind":"bounded_search"`,
		`{"node_2":1132}`,
		"Do not return the candidate map directly",
		"product of all variable domain widths below 100000",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("witness repair prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestValidateREPLAttemptOutputForBraidGraphAllowsLengthStopForRepair(t *testing.T) {
	t.Parallel()

	err := validateREPLAttemptOutputForPhase(REPLRunnerPhase{OutputKind: REPLPhaseOutputKindBraidGraph}, engine.EngineOutput{
		StopReason:    engine.StopReasonMaxTokens,
		AssistantText: `{"version":1,"nodes":[`,
	}, nil, 8)
	if err != nil {
		t.Fatalf("validateREPLAttemptOutputForPhase() error = %v", err)
	}
}

func TestAutoExecutePhaseRequiredToolSupportsFanout(t *testing.T) {
	t.Parallel()

	toolExec := &replToolExecutor{
		replToolName:      PythonREPLToolName,
		extraToolExecutor: fakeREPLExtraToolExecutor{},
	}
	output := engine.EngineOutput{}
	err := autoExecutePhaseRequiredTool(context.Background(), "fanout", REPLRunnerPhase{
		RequiredTools:           []string{"extra_test_tool"},
		AutoExecuteRequiredTool: true,
		AutoExecuteToolCalls: []REPLRunnerPhaseAutoToolCall{
			{Tool: "extra_test_tool", Args: json.RawMessage(`{"value":"a"}`)},
			{Tool: "extra_test_tool", Args: json.RawMessage(`{"value":"b"}`)},
			{Tool: "extra_test_tool", Args: json.RawMessage(`{"value":"c"}`)},
		},
	}, toolExec, &output)
	if err != nil {
		t.Fatalf("auto execute error = %v", err)
	}
	if len(output.ToolCalls) != 3 || len(output.ToolResults) != 3 {
		t.Fatalf("tool calls=%d results=%d want 3/3", len(output.ToolCalls), len(output.ToolResults))
	}
	if got := toolCallNames(output.ToolCalls); fmt.Sprint(got) != "[extra_test_tool extra_test_tool extra_test_tool]" {
		t.Fatalf("tool names=%v", got)
	}
	if !strings.Contains(output.ToolResults[1].Content, `"b"`) {
		t.Fatalf("second result=%q", output.ToolResults[1].Content)
	}
}

func TestValidateREPLPhaseOutputCanRequireToolResultOK(t *testing.T) {
	t.Parallel()

	err := validateREPLPhaseOutput(REPLRunnerPhase{
		Name:                "helper",
		RequiredTools:       []string{"ephemeral_helper_solve"},
		RequireToolResultOK: true,
	}, engine.EngineOutput{
		ToolCalls: []engine.ToolCall{{
			ID:   "call_helper",
			Name: "ephemeral_helper_solve",
		}},
		ToolResults: []engine.ToolResult{{
			ToolCallID: "call_helper",
			Content:    `{"ok":false,"error":"compile failed"}`,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "ok=false") {
		t.Fatalf("err=%v", err)
	}
}

func TestREPLRunnerReturnsPartialResultOnRequiredToolResultFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-helper-skip",
			"choices":[{
				"message":{"role":"assistant","content":"I will answer directly."},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":10,"completion_tokens":4}
		}`))
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:       "openai_compat",
				BaseURL:        server.URL,
				AuthMode:       "none",
				Model:          "test-model",
				MaxIterations:  1,
				MaxTokens:      256,
				Timeout:        5 * time.Second,
				RequireToolUse: true,
			},
			Budget:            BudgetConfig{MaxIterations: 1},
			ExtraToolExecutor: fakeFailingHelperToolExecutor{},
			Phases: []REPLRunnerPhase{{
				Name:                    "helper",
				Tools:                   []string{EphemeralHelperSolveToolName},
				RequiredTools:           []string{EphemeralHelperSolveToolName},
				MaxIterations:           1,
				AutoExecuteRequiredTool: true,
				RequireToolResultOK:     true,
			}},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "Use the helper.",
		RunID:         "run-partial",
		AgentID:       "agent-partial",
		MaxIterations: 1,
	}, rlm.Environment{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := fmt.Sprint(result.Metadata["tool_names"]); !strings.Contains(got, EphemeralHelperSolveToolName) {
		t.Fatalf("tool names=%s metadata=%#v", got, result.Metadata)
	}
	if result.Metadata["error"] == "" {
		t.Fatalf("missing partial error metadata: %#v", result.Metadata)
	}
}

type fakeFailingHelperToolExecutor struct{}

func (fakeFailingHelperToolExecutor) List() []engine.ToolDef {
	return []engine.ToolDef{{
		Name:        EphemeralHelperSolveToolName,
		Description: "test helper",
		Parameters:  json.RawMessage(`{"type":"object","additionalProperties":false}`),
	}}
}

func (fakeFailingHelperToolExecutor) Execute(context.Context, string, json.RawMessage) (string, error) {
	return `{"ok":false,"error":"compile failed"}`, nil
}

func TestREPLRunnerAsyncRecursionFanoutAndGrandchildSummary(t *testing.T) {
	var toolPayloads []map[string]any
	var calls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		toolPayloads = append(toolPayloads, extractToolPayloadsFromMessages(req["messages"])...)

		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_1",
							"type":"function",
							"function":{"name":"rlm_query","arguments":"{\"prompt\":\"branch alpha\",\"max_iterations\":4}"}
						},{
							"id":"call_2",
							"type":"function",
							"function":{"name":"rlm_query","arguments":"{\"prompt\":\"branch beta\",\"max_iterations\":4}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":5}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-2",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_3",
							"type":"function",
							"function":{"name":"rlm_wait","arguments":"{\"timeout_ms\":1000}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":20,"completion_tokens":4}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-3",
				"choices":[{
					"message":{"role":"assistant","content":"summary: alpha uses gamma; beta is standalone"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":30,"completion_tokens":6}
			}`))
		}
	}))
	defer server.Close()

	var childTasks []rlm.Task
	var grandchildTasks []rlm.Task
	var taskMu sync.Mutex
	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 8,
				MaxTokens:     512,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{
				MaxDepth:       3,
				MaxSubcalls:    4,
				MaxIterations:  8,
				MaxChildTokens: 300,
			},
			AsyncRecursion: true,
			AsyncScheduler: SchedulerConfig{MaxWorkers: 2},
			RLMQueryFactory: func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc {
				return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
					taskMu.Lock()
					childTasks = append(childTasks, task)
					taskMu.Unlock()
					switch {
					case strings.Contains(task.Prompt, "branch alpha"):
						grandchild := rlm.Task{
							Prompt:        "grandchild gamma",
							RunID:         task.RunID,
							AgentID:       task.AgentID + "/manual-grandchild",
							ParentAgentID: task.AgentID,
							MaxDepth:      1,
							MaxIterations: 1,
						}
						taskMu.Lock()
						grandchildTasks = append(grandchildTasks, grandchild)
						taskMu.Unlock()
						return rlm.Result{
							Answer:     "alpha result with gamma result",
							Iterations: 2,
							Subcalls:   1,
							Metadata: map[string]any{
								"parent_input_tokens":  11,
								"parent_output_tokens": 7,
								"parent_total_tokens":  18,
							},
						}, nil
					case strings.Contains(task.Prompt, "branch beta"):
						return rlm.Result{
							Answer:     "beta result",
							Iterations: 1,
							Subcalls:   0,
							Metadata: map[string]any{
								"parent_input_tokens":  13,
								"parent_output_tokens": 5,
								"parent_total_tokens":  18,
							},
						}, nil
					default:
						return rlm.Result{Answer: "unknown child"}, nil
					}
				}
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "fan out and summarize",
		RunID:         "run-main",
		AgentID:       "agent-root",
		MaxDepth:      3,
		MaxSubcalls:   4,
		MaxIterations: 8,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "summary: alpha uses gamma; beta is standalone" {
		t.Fatalf("answer = %q", result.Answer)
	}
	if result.Subcalls != 2 {
		t.Fatalf("subcalls = %d, want 2 direct children", result.Subcalls)
	}
	if got := result.Metadata["child_total_tokens"]; got != 36 {
		t.Fatalf("child_total_tokens = %#v, want 36", got)
	}
	taskMu.Lock()
	childTasksSnapshot := append([]rlm.Task(nil), childTasks...)
	grandchildTasksSnapshot := append([]rlm.Task(nil), grandchildTasks...)
	taskMu.Unlock()
	if len(childTasksSnapshot) != 2 {
		t.Fatalf("child tasks = %d, want 2", len(childTasksSnapshot))
	}
	var alphaTask, betaTask *rlm.Task
	for i := range childTasksSnapshot {
		switch {
		case strings.Contains(childTasksSnapshot[i].Prompt, "branch alpha"):
			alphaTask = &childTasksSnapshot[i]
		case strings.Contains(childTasksSnapshot[i].Prompt, "branch beta"):
			betaTask = &childTasksSnapshot[i]
		}
	}
	if alphaTask == nil || betaTask == nil {
		t.Fatalf("child prompts = %q, %q", childTasksSnapshot[0].Prompt, childTasksSnapshot[1].Prompt)
	}
	if len(grandchildTasksSnapshot) != 1 {
		t.Fatalf("grandchild tasks = %d, want 1", len(grandchildTasksSnapshot))
	}
	if grandchildTasksSnapshot[0].Prompt != "grandchild gamma" {
		t.Fatalf("grandchild prompt = %q", grandchildTasksSnapshot[0].Prompt)
	}
	if grandchildTasksSnapshot[0].ParentAgentID != alphaTask.AgentID {
		t.Fatalf("grandchild parent = %q, want %q", grandchildTasksSnapshot[0].ParentAgentID, alphaTask.AgentID)
	}

	var sawFirstHandle, sawSecondHandle, sawBothCompleted bool
	for _, payload := range toolPayloads {
		if payload["child"] == float64(1) {
			sawFirstHandle = true
		}
		if payload["child"] == float64(2) {
			sawSecondHandle = true
		}
		completed, ok := payload["completed"].([]any)
		if !ok || len(completed) != 2 {
			continue
		}
		ids := map[string]bool{}
		for _, item := range completed {
			node, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if node["status"] == string(NodeStatusCompleted) {
				ids[fmt.Sprint(node["child"])] = true
			}
		}
		if ids["1"] && ids["2"] {
			sawBothCompleted = true
		}
	}
	if !sawFirstHandle || !sawSecondHandle {
		t.Fatalf("missing child handles first=%v second=%v payloads=%#v", sawFirstHandle, sawSecondHandle, toolPayloads)
	}
	if !sawBothCompleted {
		t.Fatalf("did not observe both children completed in wait payloads: %#v", toolPayloads)
	}
}

func TestREPLRunnerAsyncRecursionRetriesWhenParentFinalizesWithPendingChild(t *testing.T) {
	var calls int
	var sawCorrection bool
	var sawCorrectionToolSurface bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if messages, ok := req["messages"].([]any); ok && len(messages) > 0 {
			last := messages[len(messages)-1].(map[string]any)
			if strings.Contains(fmt.Sprint(last["content"]), "Runtime correction") &&
				strings.Contains(fmt.Sprint(last["content"]), "pending subcalls") {
				sawCorrection = true
			}
		}

		calls++
		if calls == 4 {
			toolNames := map[string]bool{}
			for _, raw := range req["tools"].([]any) {
				tool := raw.(map[string]any)
				fn := tool["function"].(map[string]any)
				toolNames[fmt.Sprint(fn["name"])] = true
			}
			sawCorrectionToolSurface = toolNames[RLMWaitToolName] &&
				toolNames[RLMResultToolName] &&
				!toolNames[RLMQueryToolName] &&
				!toolNames[PythonREPLToolName]
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_1",
							"type":"function",
							"function":{"name":"rlm_query","arguments":"{\"prompt\":\"slow child\",\"max_iterations\":1}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":4}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-2",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_2",
							"type":"function",
							"function":{"name":"rlm_wait","arguments":"{\"timeout_ms\":20}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":20,"completion_tokens":4}
			}`))
		case 3:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-3",
				"choices":[{
					"message":{"role":"assistant","content":"premature final"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":30,"completion_tokens":4}
			}`))
		case 4:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-4",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_3",
							"type":"function",
							"function":{"name":"rlm_wait","arguments":"{\"timeout_ms\":1000}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":40,"completion_tokens":4}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-5",
				"choices":[{
					"message":{"role":"assistant","content":"final after child"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":50,"completion_tokens":5}
			}`))
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 8,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{
				MaxDepth:       2,
				MaxSubcalls:    1,
				MaxIterations:  8,
				MaxChildTokens: 100,
			},
			AsyncRecursion: true,
			AsyncScheduler: SchedulerConfig{MaxWorkers: 1},
			RLMQueryFactory: func(parentTask rlm.Task, env rlm.Environment) RLMQueryRunFunc {
				return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
					time.Sleep(100 * time.Millisecond)
					return rlm.Result{
						Answer:     "slow child done",
						Iterations: 1,
						Metadata: map[string]any{
							"parent_input_tokens":  3,
							"parent_output_tokens": 2,
							"parent_total_tokens":  5,
						},
					}, nil
				}
			},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	result, err := runner.Run(context.Background(), rlm.Task{
		Prompt:        "submit a child and wait",
		RunID:         "run-main",
		AgentID:       "agent-root",
		MaxDepth:      2,
		MaxSubcalls:   1,
		MaxIterations: 8,
	}, rlm.Environment{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Answer != "final after child" {
		t.Fatalf("answer = %q", result.Answer)
	}
	if !sawCorrection {
		t.Fatal("model did not receive pending-subcall correction prompt")
	}
	if !sawCorrectionToolSurface {
		t.Fatal("pending correction did not narrow tools to rlm_wait/rlm_result")
	}
	if result.Metadata["parent_pending_retries"] != 1 {
		t.Fatalf("parent_pending_retries = %#v, want 1", result.Metadata["parent_pending_retries"])
	}
	if result.Metadata["parent_iteration_count"] != 5 {
		t.Fatalf("parent_iteration_count = %#v, want 5", result.Metadata["parent_iteration_count"])
	}
}

func TestREPLRunnerAsyncRecursionEnforcesRequiredSubcalls(t *testing.T) {
	t.Run("fails_when_child_flattens", func(t *testing.T) {
		recorder := NewRecorder()
		runner := &REPLRunner{}
		var attempts int
		backend := runner.newAsyncNodeBackend(&replToolExecutor{
			parentTask: rlm.Task{RunID: "run-main", AgentID: "agent-root", MaxDepth: 2, MaxSubcalls: 2},
			parentEnv:  rlm.Environment{},
			recorder:   recorder,
			rlmQuery: func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
				attempts++
				return rlm.Result{Answer: "flattened answer", Subcalls: 0}, nil
			},
		}, IdentityPlan{RunID: "run-main", AgentID: "agent-root"})

		_, err := backend.RunNode(context.Background(), Node{ID: "root.1", Depth: 1}, NodeInput{
			Prompt:           "must recurse",
			RequiredSubcalls: 1,
		})
		if err == nil {
			t.Fatal("expected required_subcalls error")
		}
		if !strings.Contains(err.Error(), "required_subcalls=1") {
			t.Fatalf("error = %v", err)
		}
		if attempts != requiredSubcallMaxAttempts {
			t.Fatalf("attempts = %d, want %d", attempts, requiredSubcallMaxAttempts)
		}
	})

	t.Run("retries_with_correction_and_recovers", func(t *testing.T) {
		runner := &REPLRunner{}
		var attempts int
		var retryPrompt string
		backend := runner.newAsyncNodeBackend(&replToolExecutor{
			parentTask: rlm.Task{RunID: "run-main", AgentID: "agent-root", MaxDepth: 2, MaxSubcalls: 2},
			parentEnv:  rlm.Environment{},
			rlmQuery: func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
				attempts++
				if attempts == 1 {
					return rlm.Result{Answer: "flattened answer", Subcalls: 0}, nil
				}
				retryPrompt = task.Prompt
				return rlm.Result{
					Answer:     "nested answer after retry",
					Subcalls:   1,
					Iterations: 2,
					Metadata: map[string]any{
						"recursive_subcalls_used": 1,
					},
				}, nil
			},
		}, IdentityPlan{RunID: "run-main", AgentID: "agent-root"})

		result, err := backend.RunNode(context.Background(), Node{ID: "root.1", Depth: 1}, NodeInput{
			Prompt:           "must recurse",
			RequiredSubcalls: 1,
		})
		if err != nil {
			t.Fatalf("RunNode returned error: %v", err)
		}
		if attempts != 2 {
			t.Fatalf("attempts = %d, want 2", attempts)
		}
		if !strings.Contains(retryPrompt, "Runtime correction") || !strings.Contains(retryPrompt, "required_subcalls=1") {
			t.Fatalf("retry prompt missing correction context:\n%s", retryPrompt)
		}
		if result.Answer != "nested answer after retry" {
			t.Fatalf("answer = %q", result.Answer)
		}
		if result.Metadata["required_subcall_attempts"] != 2 {
			t.Fatalf("required_subcall_attempts metadata = %#v", result.Metadata["required_subcall_attempts"])
		}
		if result.Metadata["recursive_subcalls_used"] != 1 {
			t.Fatalf("recursive_subcalls_used metadata = %#v", result.Metadata["recursive_subcalls_used"])
		}
	})

	t.Run("passes_when_child_reports_required_subcalls", func(t *testing.T) {
		runner := &REPLRunner{}
		backend := runner.newAsyncNodeBackend(&replToolExecutor{
			parentTask: rlm.Task{RunID: "run-main", AgentID: "agent-root", MaxDepth: 2, MaxSubcalls: 2},
			parentEnv:  rlm.Environment{},
			rlmQuery: func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
				return rlm.Result{
					Answer:     "nested answer",
					Subcalls:   1,
					Iterations: 2,
					Metadata: map[string]any{
						"recursive_subcalls_used": 1,
					},
				}, nil
			},
		}, IdentityPlan{RunID: "run-main", AgentID: "agent-root"})

		result, err := backend.RunNode(context.Background(), Node{ID: "root.1", Depth: 1}, NodeInput{
			Prompt:           "must recurse",
			RequiredSubcalls: 1,
		})
		if err != nil {
			t.Fatalf("RunNode returned error: %v", err)
		}
		if result.Answer != "nested answer" {
			t.Fatalf("answer = %q", result.Answer)
		}
		if result.Metadata["required_subcalls"] != 1 {
			t.Fatalf("required_subcalls metadata = %#v", result.Metadata["required_subcalls"])
		}
	})
}

func TestBuildChildRuntimePromptClarifiesLeafDepth(t *testing.T) {
	t.Parallel()

	prompt := buildChildRuntimePrompt("Parent: Move block A onto block B.", "Move block A onto block B.", 0, 0, 0)
	for _, want := range []string{
		"RLM child runtime context",
		"original parent task is included for grounding",
		"ignore that drift",
		"separate model tools",
		"not Python or Go functions",
		"Leaf-child mode",
		"solve directly with the child task",
		"Do not discuss recursion, depth, budget",
		"NodeArtifact JSON",
		"Original parent task for grounding",
		"Parent: Move block A onto block B.",
		"Move block A onto block B.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestREPLRunnerAsyncRecursionRejectsPendingSubmittedChildren(t *testing.T) {
	store := NewMemoryNodeStore()
	ctx := context.Background()
	if _, err := store.CreateRun(ctx, Run{ID: "run-main", RootNodeID: replRootNodeID, Status: NodeStatusQueued}); err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if _, err := store.CreateNode(ctx, Node{RunID: "run-main", ID: replRootNodeID, Status: NodeStatusQueued}); err != nil {
		t.Fatalf("CreateNode root returned error: %v", err)
	}
	if _, err := store.CreateNode(ctx, Node{
		RunID:        "run-main",
		ID:           "root.1",
		ParentNodeID: replRootNodeID,
		Depth:        1,
		Status:       NodeStatusRunning,
	}); err != nil {
		t.Fatalf("CreateNode child returned error: %v", err)
	}

	executor := &replToolExecutor{
		asyncNodeStore:  store,
		asyncRunID:      "run-main",
		asyncRootNodeID: replRootNodeID,
	}
	err := executor.unfinishedSubcallFailure(ctx)
	if err == nil {
		t.Fatal("expected pending subcall failure")
	}
	if !strings.Contains(err.Error(), "pending subcalls remain") || !strings.Contains(err.Error(), "root.1") {
		t.Fatalf("error = %v", err)
	}
}

func TestREPLRunnerAsyncRecursionRejectsStaleWaitBeforeFinalAnswer(t *testing.T) {
	store := NewMemoryNodeStore()
	ctx := context.Background()
	if _, err := store.CreateRun(ctx, Run{ID: "run-main", RootNodeID: replRootNodeID, Status: NodeStatusQueued}); err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if _, err := store.CreateNode(ctx, Node{RunID: "run-main", ID: replRootNodeID, Status: NodeStatusQueued}); err != nil {
		t.Fatalf("CreateNode root returned error: %v", err)
	}
	if _, err := store.CreateNode(ctx, Node{
		RunID:        "run-main",
		ID:           "root.1",
		ParentNodeID: replRootNodeID,
		Depth:        1,
		Status:       NodeStatusQueued,
	}); err != nil {
		t.Fatalf("CreateNode child returned error: %v", err)
	}
	if _, err := store.UpdateNodeStatus(ctx, "run-main", "root.1", NodeStatusRunning); err != nil {
		t.Fatalf("UpdateNodeStatus returned error: %v", err)
	}

	recorder := NewRecorder()
	recorder.RecordNodeWaitCompleted(WaitEvent{
		RunID:        "run-main",
		ParentNodeID: replRootNodeID,
		ChildIDs:     []string{"root.1"},
		Pending:      1,
		MinComplete:  1,
		TimeoutMS:    20,
	})
	if _, err := store.SetNodeResult(ctx, "run-main", "root.1", NodeResult{
		Status:  NodeStatusCompleted,
		Summary: "done",
	}); err != nil {
		t.Fatalf("SetNodeResult returned error: %v", err)
	}

	executor := &replToolExecutor{
		recorder:        recorder,
		asyncNodeStore:  store,
		asyncRunID:      "run-main",
		asyncRootNodeID: replRootNodeID,
		asyncRLMTools:   &RLMToolsExecutor{},
		subcallsEnabled: true,
		rlmQuery:        func(context.Context, rlm.Task, rlm.Environment) (rlm.Result, error) { return rlm.Result{}, nil },
	}
	err := executor.staleSubcallWaitFailure(ctx)
	if err == nil {
		t.Fatal("expected stale wait failure")
	}
	if !strings.Contains(err.Error(), "changed after the last rlm_wait") {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeMaxTokenChildSummaryWrapsPartialAnswer(t *testing.T) {
	t.Parallel()

	got := normalizeMaxTokenChildSummary("Looking at the task...\nanswer: solution = [1, 2, 3]\nmore scratch")
	for _, want := range []string{"status: partial", "answer: solution = [1, 2, 3]", "checks: max-token child output was salvaged"} {
		if !strings.Contains(got, want) {
			t.Fatalf("normalized summary missing %q:\n%s", want, got)
		}
	}
	if err := validateChildSummaryFinalText(got); err != nil {
		t.Fatalf("validateChildSummaryFinalText() error = %v", err)
	}
}

func TestIsREPLMaxTokenError(t *testing.T) {
	t.Parallel()

	if !isREPLMaxTokenError(fmt.Errorf("rlm repl runner: model hit max token stop before producing a valid final answer")) {
		t.Fatal("expected max-token error to be recognized")
	}
	if isREPLMaxTokenError(fmt.Errorf("rlm repl runner: tool failed")) {
		t.Fatal("non max-token error should not be recognized")
	}
}

func TestIsChildSummaryFinalRepairError(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("rlm repl runner: final output repair failed for child_summary: child summary final response must include status: solved|partial|blocked")
	if !isChildSummaryFinalRepairError(err) {
		t.Fatal("expected child summary final repair error to be recognized")
	}
	if isChildSummaryFinalRepairError(fmt.Errorf("rlm repl runner: python_repl failed")) {
		t.Fatal("non child-summary error should not be recognized")
	}
}

func TestREPLRunnerAsyncRecursionAllowsMultiWaveCompletedWaits(t *testing.T) {
	store := NewMemoryNodeStore()
	ctx := context.Background()
	if _, err := store.CreateRun(ctx, Run{ID: "run-main", RootNodeID: replRootNodeID, Status: NodeStatusQueued}); err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if _, err := store.CreateNode(ctx, Node{RunID: "run-main", ID: replRootNodeID, Status: NodeStatusQueued}); err != nil {
		t.Fatalf("CreateNode root returned error: %v", err)
	}
	for _, id := range []string{"root.1", "root.2", "root.3"} {
		if _, err := store.CreateNode(ctx, Node{
			RunID:        "run-main",
			ID:           id,
			ParentNodeID: replRootNodeID,
			Depth:        1,
			Status:       NodeStatusQueued,
		}); err != nil {
			t.Fatalf("CreateNode %s returned error: %v", id, err)
		}
		if _, err := store.UpdateNodeStatus(ctx, "run-main", id, NodeStatusRunning); err != nil {
			t.Fatalf("UpdateNodeStatus %s returned error: %v", id, err)
		}
		if _, err := store.SetNodeResult(ctx, "run-main", id, NodeResult{
			Status:  NodeStatusCompleted,
			Summary: "done",
		}); err != nil {
			t.Fatalf("SetNodeResult %s returned error: %v", id, err)
		}
	}

	recorder := NewRecorder()
	recorder.RecordNodeWaitCompleted(WaitEvent{
		RunID:        "run-main",
		ParentNodeID: replRootNodeID,
		ChildIDs:     []string{"root.1", "root.2"},
		Completed:    2,
		MinComplete:  2,
	})
	recorder.RecordNodeWaitCompleted(WaitEvent{
		RunID:        "run-main",
		ParentNodeID: replRootNodeID,
		ChildIDs:     []string{"root.3"},
		Completed:    1,
		MinComplete:  1,
	})

	executor := &replToolExecutor{
		recorder:        recorder,
		asyncNodeStore:  store,
		asyncRunID:      "run-main",
		asyncRootNodeID: replRootNodeID,
	}
	if err := executor.staleSubcallWaitFailure(ctx); err != nil {
		t.Fatalf("staleSubcallWaitFailure returned error: %v", err)
	}
}

func TestREPLRunnerRejectsFailedSubcallsWhenConfigured(t *testing.T) {
	store := NewMemoryNodeStore()
	ctx := context.Background()
	if _, err := store.CreateRun(ctx, Run{ID: "run-main", RootNodeID: replRootNodeID, Status: NodeStatusQueued}); err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if _, err := store.CreateNode(ctx, Node{RunID: "run-main", ID: replRootNodeID, Status: NodeStatusQueued}); err != nil {
		t.Fatalf("CreateNode root returned error: %v", err)
	}
	if _, err := store.CreateNode(ctx, Node{
		RunID:        "run-main",
		ID:           "root.1",
		ParentNodeID: replRootNodeID,
		Depth:        1,
		Status:       NodeStatusQueued,
	}); err != nil {
		t.Fatalf("CreateNode child returned error: %v", err)
	}
	if _, err := store.UpdateNodeStatus(ctx, "run-main", "root.1", NodeStatusRunning); err != nil {
		t.Fatalf("UpdateNodeStatus returned error: %v", err)
	}
	if _, err := store.SetNodeResult(ctx, "run-main", "root.1", NodeResult{
		Status:       NodeStatusFailed,
		Summary:      "failed summary",
		ErrorCode:    "backend_error",
		ErrorMessage: "child tool unavailable",
	}); err != nil {
		t.Fatalf("SetNodeResult returned error: %v", err)
	}

	executor := &replToolExecutor{
		asyncNodeStore: store,
		asyncRunID:     "run-main",
	}
	err := executor.failedSubcallFailure(ctx)
	if err == nil {
		t.Fatal("expected failed subcall error")
	}
	if !strings.Contains(err.Error(), "root.1") || !strings.Contains(err.Error(), "child tool unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestREPLRunnerPropagatesREPLBudgetFailure(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_1",
							"type":"function",
							"function":{"name":"python_repl","arguments":"{\"code\":\"1+1\"}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":4}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-2",
				"choices":[{
					"message":{
						"role":"assistant",
						"tool_calls":[{
							"id":"call_2",
							"type":"function",
							"function":{"name":"python_repl","arguments":"{\"code\":\"2+2\"}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":20,"completion_tokens":4}
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-3",
				"choices":[{
					"message":{"role":"assistant","content":"solution = []"},
					"finish_reason":"stop"
				}],
				"usage":{"prompt_tokens":30,"completion_tokens":4}
			}`))
		}
	}))
	defer server.Close()

	runner := &REPLRunner{
		Config: REPLRunnerConfig{
			LLM: rlm.LLMConfig{
				Provider:      "openai_compat",
				BaseURL:       server.URL,
				AuthMode:      "none",
				Model:         "test-model",
				MaxIterations: 2,
				MaxTokens:     256,
				Timeout:       5 * time.Second,
			},
			Budget: BudgetConfig{MaxREPLCalls: 1, MaxIterations: 2},
		},
		SandboxFactory: func() rlm.Sandbox { return &fakeSandbox{} },
	}

	_, err := runner.Run(context.Background(), rlm.Task{Prompt: "Solve.", MaxIterations: 2}, rlm.Environment{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "repl_calls budget exceeded") {
		t.Fatalf("error = %v", err)
	}
}

func TestREPLToolExecutorListNoSubcallsVsRecursive(t *testing.T) {
	noSubcalls := (&replToolExecutor{}).List()
	if len(noSubcalls) != 1 {
		t.Fatalf("len(noSubcalls) = %d, want 1", len(noSubcalls))
	}
	if noSubcalls[0].Name != PythonREPLToolName {
		t.Fatalf("noSubcalls[0].Name = %q, want %q", noSubcalls[0].Name, PythonREPLToolName)
	}

	withSubcalls := (&replToolExecutor{
		subcallsEnabled: true,
		rlmQuery: func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
			return rlm.Result{}, nil
		},
	}).List()
	if len(withSubcalls) != 2 {
		t.Fatalf("len(withSubcalls) = %d, want 2", len(withSubcalls))
	}
	if withSubcalls[0].Name != PythonREPLToolName {
		t.Fatalf("withSubcalls[0].Name = %q, want %q", withSubcalls[0].Name, PythonREPLToolName)
	}
	if withSubcalls[1].Name != RLMQueryToolName {
		t.Fatalf("withSubcalls[1].Name = %q, want %q", withSubcalls[1].Name, RLMQueryToolName)
	}
}

func TestREPLToolExecutorRLMQuerySuccess(t *testing.T) {
	budget, err := NewBudget(BudgetConfig{
		MaxDepth:       2,
		MaxSubcalls:    2,
		MaxChildTokens: 100,
	})
	if err != nil {
		t.Fatalf("NewBudget returned error: %v", err)
	}
	recorder := NewRecorder()

	var called int
	var seenTask rlm.Task
	executor := &replToolExecutor{
		budget:   budget,
		recorder: recorder,
		identity: IdentityPlan{
			RunID:           "run-main",
			AgentID:         "agent-root",
			OutputNamespace: "runs/run-main/agents/agent-root",
		},
		parentTask: rlm.Task{
			Prompt:        "parent",
			Role:          "researcher",
			RunID:         "run-main",
			AgentID:       "agent-root",
			WorkspaceID:   "ws",
			WorkspaceRoot: "/tmp/ws",
			MaxDepth:      2,
			MaxIterations: 7,
			MaxSubcalls:   2,
		},
		parentEnv:       rlm.Environment{RepoHandles: []string{"repo://example"}},
		subcallsEnabled: true,
		rlmQuery: func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
			called++
			seenTask = task
			return rlm.Result{
				Answer:     "child-answer",
				Iterations: 3,
				Subcalls:   1,
				Metadata: map[string]any{
					"parent_input_tokens":  11,
					"parent_output_tokens": 5,
					"parent_total_tokens":  16,
				},
			}, nil
		},
	}

	raw, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"child task","max_iterations":4}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if called != 1 {
		t.Fatalf("called = %d, want 1", called)
	}
	if !strings.Contains(seenTask.Prompt, "child task") {
		t.Fatalf("seenTask.Prompt = %q, want child task wrapper containing child task", seenTask.Prompt)
	}
	for _, want := range []string{"RLM child runtime context", "not Python or Go functions", "Child task:"} {
		if !strings.Contains(seenTask.Prompt, want) {
			t.Fatalf("seenTask.Prompt missing %q:\n%s", want, seenTask.Prompt)
		}
	}
	if seenTask.MaxDepth != 1 {
		t.Fatalf("seenTask.MaxDepth = %d, want 1", seenTask.MaxDepth)
	}
	if seenTask.MaxSubcalls != 1 {
		t.Fatalf("seenTask.MaxSubcalls = %d, want 1", seenTask.MaxSubcalls)
	}
	if seenTask.MaxIterations != 4 {
		t.Fatalf("seenTask.MaxIterations = %d, want 4", seenTask.MaxIterations)
	}
	if seenTask.RunID != "run-main" {
		t.Fatalf("seenTask.RunID = %q, want run-main", seenTask.RunID)
	}
	if seenTask.AgentID != "agent-root/rlm-0001" {
		t.Fatalf("seenTask.AgentID = %q, want agent-root/rlm-0001", seenTask.AgentID)
	}
	if seenTask.ParentAgentID != "agent-root" {
		t.Fatalf("seenTask.ParentAgentID = %q, want agent-root", seenTask.ParentAgentID)
	}
	if seenTask.OutputNamespace != "runs/run-main/agents/agent-root/rlm-0001" {
		t.Fatalf("seenTask.OutputNamespace = %q", seenTask.OutputNamespace)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode tool payload: %v", err)
	}
	if payload["answer"] != "child-answer" {
		t.Fatalf("payload.answer = %#v, want child-answer", payload["answer"])
	}
	if payload["iterations"] != float64(3) {
		t.Fatalf("payload.iterations = %#v, want 3", payload["iterations"])
	}
	if payload["subcalls"] != float64(1) {
		t.Fatalf("payload.subcalls = %#v, want 1", payload["subcalls"])
	}

	summary := executor.subcallSummary()
	if summary.Calls != 1 {
		t.Fatalf("summary.Calls = %d, want 1", summary.Calls)
	}
	if summary.InputTokens != 11 || summary.OutputTokens != 5 || summary.TotalTokens != 16 {
		t.Fatalf("summary tokens = %+v, want input=11 output=5 total=16", summary)
	}

	snapshot := budget.Snapshot()
	if snapshot.SubcallsUsed != 1 || snapshot.SubcallsActive != 0 {
		t.Fatalf("subcall snapshot = %+v, want used=1 active=0", snapshot)
	}
	if snapshot.ChildTokens != 16 {
		t.Fatalf("snapshot.ChildTokens = %d, want 16", snapshot.ChildTokens)
	}

	var starts int
	var ends int
	var subcallPayload SubcallEvent
	for _, event := range recorder.Events() {
		switch event.Type {
		case EventTypeSubcallStart:
			starts++
			if event.Subcall != nil {
				subcallPayload = *event.Subcall
			}
		case EventTypeSubcallEnd:
			ends++
		}
	}
	if starts != 1 || ends != 1 {
		t.Fatalf("subcall events = start:%d end:%d, want 1 each", starts, ends)
	}
	if subcallPayload.AgentID != "agent-root/rlm-0001" {
		t.Fatalf("subcall agent_id = %q, want agent-root/rlm-0001", subcallPayload.AgentID)
	}
	if subcallPayload.ParentAgentID != "agent-root" {
		t.Fatalf("subcall parent_agent_id = %q, want agent-root", subcallPayload.ParentAgentID)
	}
	if subcallPayload.OutputNamespace != "runs/run-main/agents/agent-root/rlm-0001" {
		t.Fatalf("subcall output_namespace = %q", subcallPayload.OutputNamespace)
	}
}

func TestREPLToolExecutorRLMQueryRejectsDepthAndBudget(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		budget, err := NewBudget(BudgetConfig{
			MaxDepth:    1,
			MaxSubcalls: 2,
		})
		if err != nil {
			t.Fatalf("NewBudget returned error: %v", err)
		}
		recorder := NewRecorder()
		calls := 0
		executor := &replToolExecutor{
			budget:          budget,
			recorder:        recorder,
			parentTask:      rlm.Task{Prompt: "parent", MaxDepth: 2, MaxSubcalls: 2},
			subcallsEnabled: true,
			currentDepth:    1,
			rlmQuery: func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
				calls++
				return rlm.Result{Answer: "child"}, nil
			},
		}

		_, err = executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"child task"}`))
		if err == nil {
			t.Fatal("expected depth error")
		}
		if !strings.Contains(err.Error(), "depth budget exceeded") {
			t.Fatalf("error = %v", err)
		}
		if calls != 0 {
			t.Fatalf("calls = %d, want 0", calls)
		}
		snapshot := budget.Snapshot()
		if snapshot.SubcallsUsed != 0 || snapshot.SubcallsActive != 0 {
			t.Fatalf("subcall snapshot = %+v, want zero usage", snapshot)
		}
		events := recorder.Events()
		if len(events) != 1 || events[0].Type != EventTypeBudget || events[0].Budget == nil || events[0].Budget.Limit != LimitDepth {
			t.Fatalf("events = %#v, want one depth budget event", events)
		}
	})

	t.Run("subcall_budget", func(t *testing.T) {
		budget, err := NewBudget(BudgetConfig{
			MaxDepth:    2,
			MaxSubcalls: 1,
		})
		if err != nil {
			t.Fatalf("NewBudget returned error: %v", err)
		}
		recorder := NewRecorder()
		calls := 0
		executor := &replToolExecutor{
			budget:          budget,
			recorder:        recorder,
			parentTask:      rlm.Task{Prompt: "parent", MaxDepth: 2, MaxSubcalls: 2},
			subcallsEnabled: true,
			rlmQuery: func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
				calls++
				return rlm.Result{Answer: "child"}, nil
			},
		}

		if _, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"first"}`)); err != nil {
			t.Fatalf("first Execute returned error: %v", err)
		}
		_, err = executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"second"}`))
		if err == nil {
			t.Fatal("expected subcall budget error")
		}
		if !strings.Contains(err.Error(), "subcalls budget exceeded") {
			t.Fatalf("error = %v", err)
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1", calls)
		}
		snapshot := budget.Snapshot()
		if snapshot.SubcallsUsed != 1 || snapshot.SubcallsActive != 0 {
			t.Fatalf("subcall snapshot = %+v, want used=1 active=0", snapshot)
		}

		events := recorder.Events()
		var hasSubcallBudget bool
		for _, event := range events {
			if event.Type == EventTypeBudget && event.Budget != nil && event.Budget.Limit == LimitSubcalls {
				hasSubcallBudget = true
				break
			}
		}
		if !hasSubcallBudget {
			t.Fatalf("expected subcall budget event, got %#v", events)
		}
	})
}

type fakeSandbox struct {
	output  string
	outputs []string
	err     error
	state   map[string]any
	execs   []string
}

func mustBudget(t *testing.T, cfg BudgetConfig) *Budget {
	t.Helper()
	budget, err := NewBudget(cfg)
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}
	return budget
}

func extractToolPayloadsFromMessages(raw any) []map[string]any {
	messages, ok := raw.([]any)
	if !ok {
		return nil
	}

	out := make([]map[string]any, 0, len(messages))
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if message["role"] != "tool" {
			continue
		}
		content, ok := message["content"].(string)
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(content), &payload); err != nil {
			continue
		}
		out = append(out, payload)
	}
	return out
}

func requestToolNames(raw any) []string {
	tools, ok := raw.([]any)
	if !ok || len(tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(tools))
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(fn["name"]))
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func (s *fakeSandbox) Init(ctx context.Context, state map[string]any) error {
	s.state = state
	return nil
}

func (s *fakeSandbox) Execute(ctx context.Context, code string) (rlm.ExecResult, error) {
	s.execs = append(s.execs, code)
	output := s.output
	if len(s.outputs) > 0 {
		output = s.outputs[0]
		s.outputs = s.outputs[1:]
	}
	if output == "" {
		output = "11"
	}
	if s.err != nil {
		return rlm.ExecResult{
			Output:     output,
			DurationMS: 7,
			ExecutedAt: time.Now().UTC(),
			Metadata: map[string]any{
				"ok":     false,
				"result": output,
			},
		}, s.err
	}
	return rlm.ExecResult{
		Output:     output,
		DurationMS: 7,
		ExecutedAt: time.Now().UTC(),
		Metadata: map[string]any{
			"ok":     true,
			"result": output,
		},
	}, nil
}

func (s *fakeSandbox) Snapshot(ctx context.Context) (map[string]any, error) {
	return map[string]any{"prompt": s.state["prompt"]}, nil
}

func (s *fakeSandbox) Close(ctx context.Context) error {
	return nil
}

func TestBuildREPLLLMConfigPreservesBearerPrefixSpace(t *testing.T) {
	t.Parallel()

	got := buildREPLLLMConfig(rlm.LLMConfig{
		Provider:   "openrouter",
		APIKey:     "sk-test",
		AuthMode:   "bearer",
		AuthPrefix: "Bearer ",
		Model:      "test-model",
	}, rlm.Task{})
	if got.AuthPrefix != "Bearer " {
		t.Fatalf("auth prefix=%q want trailing-space bearer prefix preserved", got.AuthPrefix)
	}
}

func TestNormalizeREPLPhaseResponseFormatDowngradesJSONObjectForLMStudio(t *testing.T) {
	t.Parallel()

	got := strings.TrimSpace(string(normalizeREPLPhaseResponseFormat("lmstudio", json.RawMessage(`{"type":"json_object"}`))))
	if got != `{"type":"text"}` {
		t.Fatalf("lmstudio response_format=%q want text", got)
	}

	got = strings.TrimSpace(string(normalizeREPLPhaseResponseFormat("openrouter", json.RawMessage(`{"type":"json_object"}`))))
	if got != `{"type":"json_object"}` {
		t.Fatalf("openrouter response_format=%q want json_object preserved", got)
	}
}
