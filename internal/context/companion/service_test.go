package companion

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// TestStripThinkTags verifies <think> blocks are removed from model output.
func TestStripThinkTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no tags", "Hello world", "Hello world"},
		{"empty string", "", ""},
		{"only think block", "<think>reasoning</think>", ""},
		{"think then response", "<think>reasoning here</think>Hello!", "Hello!"},
		{"think then response with newline", "<think>reasoning</think>\nHello!", "Hello!"},
		{"unclosed think tag", "<think>partial reasoning", ""},
		{"text before unclosed think", "prefix <think>partial", "prefix"},
		{"text before and after think", "before <think>middle</think> after", "before  after"},
		{"multiple think blocks", "<think>one</think>Hi <think>two</think>there", "Hi there"},
		{"nested angle brackets in think", "<think>if a < b then c > d</think>Result", "Result"},
		{"think tag in middle", "Hello <think>reasoning</think>World", "Hello World"},
		{"no opening think tag", "reasoning here</think>Hello!", "Hello!"},
		{"no opening think tag with newline", "I should respond.\n</think>\nHello!", "Hello!"},
		{"no opening think tag multiline", "Let me think about this.\nThe user wants X.\n</think>Here is my response.", "Here is my response."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripThinkTags(tt.input)
			if got != tt.want {
				t.Errorf("stripThinkTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestShouldRetryGroundedTurn(t *testing.T) {
	tests := []struct {
		name            string
		enforceGrounded bool
		output          engine.EngineOutput
		responseText    string
		contextQueries  int
		want            bool
	}{
		{
			name:            "disabled policy never retries",
			enforceGrounded: false,
			output:          engine.EngineOutput{StopReason: engine.StopReasonError},
			responseText:    "",
			contextQueries:  0,
			want:            false,
		},
		{
			name:            "error retries",
			enforceGrounded: true,
			output:          engine.EngineOutput{StopReason: engine.StopReasonError},
			responseText:    "some text",
			contextQueries:  0,
			want:            true,
		},
		{
			name:            "empty response retries",
			enforceGrounded: true,
			output:          engine.EngineOutput{StopReason: engine.StopReasonEndTurn},
			responseText:    "",
			contextQueries:  1,
			want:            true,
		},
		{
			name:            "no tools and no context query retries",
			enforceGrounded: true,
			output:          engine.EngineOutput{StopReason: engine.StopReasonEndTurn},
			responseText:    "generic answer",
			contextQueries:  0,
			want:            true,
		},
		{
			name:            "tool-backed answer does not retry",
			enforceGrounded: true,
			output:          engine.EngineOutput{StopReason: engine.StopReasonEndTurn, ToolCalls: []engine.ToolCall{{Name: "context_search"}}},
			responseText:    "grounded answer",
			contextQueries:  1,
			want:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRetryGroundedTurn(tt.enforceGrounded, tt.output, tt.responseText, tt.contextQueries)
			if got != tt.want {
				t.Fatalf("shouldRetryGroundedTurn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldRecoverContextToolLeak(t *testing.T) {
	tests := []struct {
		name             string
		responseText     string
		rawAssistantText string
		calls            []engine.ToolCall
		want             bool
	}{
		{
			name:         "no context tools",
			responseText: "normal answer",
			calls:        []engine.ToolCall{{Name: "context_search"}},
			want:         false,
		},
		{
			name:         "raw tool call syntax leaks",
			responseText: `[rlm_context_list(),rlm_context_query(key="tech:owner")]<|tool_call_end|>`,
			calls:        nil,
			want:         true,
		},
		{
			name:         "context mutation json leaks",
			responseText: "{\"key\":\"tech:codename\",\"value\":\"amber-river-19\",\"scope\":\"global\"}",
			calls:        []engine.ToolCall{{Name: "rlm_context_put"}},
			want:         true,
		},
		{
			name:             "raw assistant marker leaks",
			responseText:     "placeholder",
			rawAssistantText: `[rlm_context_query(key="tech:owner")]<|tool_call_end|>`,
			calls:            nil,
			want:             true,
		},
		{
			name:         "natural answer with context tool calls is allowed",
			responseText: "{\"owner\":\"Mina\",\"codename\":\"amber-river-19\"}",
			calls:        []engine.ToolCall{{Name: "rlm_context_query"}},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRecoverContextToolLeak(tt.responseText, tt.rawAssistantText, tt.calls)
			if got != tt.want {
				t.Fatalf("shouldRecoverContextToolLeak() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLooksLikeToolCallMarkup(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "openai compatible marker",
			text: `<|tool_call>call:repo_index_dag_grep{query: "repo index"}<tool_call|>`,
			want: true,
		},
		{
			name: "xml marker",
			text: "<tool_call>{}</tool_call>",
			want: true,
		},
		{
			name: "normal answer",
			text: "The repo index DAG grep returned one seed and two edges.",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeToolCallMarkup(tt.text)
			if got != tt.want {
				t.Fatalf("looksLikeToolCallMarkup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMissingExplicitlyRequestedTools(t *testing.T) {
	toolDefs := []engine.ToolDef{
		{Name: "repo_index_search"},
		{Name: "repo_index_dag_grep"},
	}
	calls := []engine.ToolCall{{Name: "repo_index_search"}}
	got := missingExplicitlyRequestedTools(
		`Use repo_index_dag_grep with query "repo index dag grep" once.`,
		calls,
		toolDefs,
	)
	if len(got) != 1 || got[0] != "repo_index_dag_grep" {
		t.Fatalf("missingExplicitlyRequestedTools() = %#v", got)
	}
}

func TestExtractRequestedQuery(t *testing.T) {
	got := extractRequestedQuery(`Use repo_index_dag_grep with query "repo index dag grep" once.`)
	if got != "repo index dag grep" {
		t.Fatalf("extractRequestedQuery() = %q", got)
	}
}

func TestBuildStructuredConversationState(t *testing.T) {
	frame := conversationContextFrame{
		HasHistory:        true,
		HistoryRecap:      "- user: Can you help me plan our Japan trip?\nMost recent user ask: Can you help me plan our Japan trip?",
		ContinuationQuery: "Current user ask: do you see our previous conversation?\nPrevious user ask: Can you help me plan our Japan trip?",
		Turns: []ConversationTurn{
			{Role: "user", Content: "Can you help me plan our Japan trip?"},
			{Role: "assistant", Content: "Yes, we were comparing Tokyo and Kyoto hotels."},
		},
	}

	got := buildStructuredConversationState(frame)
	if !strings.Contains(got, `"ongoing_conversation": true`) {
		t.Fatalf("structured state=%q missing ongoing flag", got)
	}
	if !strings.Contains(got, `"last_user_ask": "Can you help me plan our Japan trip?"`) {
		t.Fatalf("structured state=%q missing last user ask", got)
	}
	if !strings.Contains(got, `"last_assistant_reply": "Yes, we were comparing Tokyo and Kyoto hotels."`) {
		t.Fatalf("structured state=%q missing last assistant reply", got)
	}
	if !strings.Contains(got, `"continuation_query": "Current user ask: do you see our previous conversation?\nPrevious user ask: Can you help me plan our Japan trip?"`) {
		t.Fatalf("structured state=%q missing continuation query", got)
	}
}

func TestParseContinuityControllerPlan(t *testing.T) {
	raw := `{"source":"visible_history","visible_summary":"We were discussing Japan travel planning.","memory_query":"Japan travel May hotels"}`
	plan, ok := parseContinuityControllerPlan(raw)
	if !ok {
		t.Fatal("expected continuity controller plan to parse")
	}
	if plan.Source != "visible_history" {
		t.Fatalf("source=%q want visible_history", plan.Source)
	}
	if plan.VisibleSummary != "We were discussing Japan travel planning." {
		t.Fatalf("visible_summary=%q", plan.VisibleSummary)
	}
	if plan.MemoryQuery != "Japan travel May hotels" {
		t.Fatalf("memory_query=%q", plan.MemoryQuery)
	}
}

func TestResolveCompanionSubcallRole(t *testing.T) {
	tests := []struct {
		name string
		role string
		want string
	}{
		{name: "default", role: "", want: DefaultSubcallWorkerRole},
		{name: "trimmed explicit role", role: "  researcher  ", want: "researcher"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveCompanionSubcallRole(tt.role); got != tt.want {
				t.Fatalf("resolveCompanionSubcallRole(%q)=%q want %q", tt.role, got, tt.want)
			}
		})
	}
}

func TestContinuityLayerHits(t *testing.T) {
	frame := conversationContextFrame{
		HasHistory:     true,
		HistoryRecap:   "recent recap",
		ArtifactRefs:   []string{"sha256:abc"},
		WorkspaceState: "# Top Of Mind\nobjective: ship harness\n\n# Task Continuity\nTask continuity summary",
	}
	meta := memoryPromptMetadata{
		HasLayeredContext:  true,
		HasTopOfMind:       true,
		HasTaskContinuity:  true,
		SessionRecallCount: 1,
	}
	got := continuityLayerHits(frame, meta, 1, false)
	want := []string{"L0", "L1", "L2", "L3", "L4", "L5"}
	if len(got) != len(want) {
		t.Fatalf("layer hits=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("layer hits=%v want %v", got, want)
		}
	}
}

func TestExplainInvisibleResponse(t *testing.T) {
	tests := []struct {
		name  string
		input engine.EngineOutput
		raw   string
		need  []string
	}{
		{
			name: "reasoning only with tool error",
			input: engine.EngineOutput{
				StopReason: engine.StopReasonMaxIterations,
				ToolCalls:  []engine.ToolCall{{ID: "call-1", Name: "repo_index_search"}},
				ToolResults: []engine.ToolResult{{
					ToolCallID: "call-1",
					Content:    "timeout while indexing",
					IsError:    true,
				}},
			},
			raw:  "<think>reasoning</think>",
			need: []string{"hidden reasoning", "repo_index_search failed", "iteration budget"},
		},
		{
			name: "no assistant text",
			input: engine.EngineOutput{
				StopReason: engine.StopReasonError,
				Error:      "upstream failed",
			},
			raw:  "",
			need: []string{"upstream failed", "no assistant text", "engine stopped with an error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := explainInvisibleResponse(tt.input, tt.raw, false)
			for _, part := range tt.need {
				if !strings.Contains(got, part) {
					t.Fatalf("explainInvisibleResponse()=%q missing %q", got, part)
				}
			}
		})
	}
}

func TestRequestedOutputFormat(t *testing.T) {
	tests := []struct {
		name        string
		question    string
		wantMode    requestedOutputFormatMode
		wantSnippet string
	}{
		{
			name:        "compact json",
			question:    "What is the current codename? Reply as compact JSON.",
			wantMode:    requestedOutputFormatCompactJSON,
			wantSnippet: "compact JSON object",
		},
		{
			name:        "reply only with token",
			question:    "Reply only with updated-codename",
			wantMode:    requestedOutputFormatOnlyValue,
			wantSnippet: "only the requested value or token",
		},
		{
			name:     "no special format",
			question: "Explain the current state.",
			wantMode: requestedOutputFormatNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotInstruction, gotMode := requestedOutputFormat(tt.question)
			if gotMode != tt.wantMode {
				t.Fatalf("requestedOutputFormat() mode = %v, want %v", gotMode, tt.wantMode)
			}
			if tt.wantSnippet != "" && !strings.Contains(gotInstruction, tt.wantSnippet) {
				t.Fatalf("requestedOutputFormat() instruction = %q, want snippet %q", gotInstruction, tt.wantSnippet)
			}
		})
	}
}

func TestBuildContinuationRecallQuery_ExpandsLowInformationFollowUp(t *testing.T) {
	turns := []ConversationTurn{
		{Role: "user", Content: "Can you help me plan our Japan travel for May?"},
		{Role: "assistant", Content: "Yes, we narrowed it to Tokyo and Kyoto and compared hotels near the train stations."},
	}

	got := buildContinuationRecallQuery("do you see our previous conversation?", turns)
	if !strings.Contains(got, "Current user ask: do you see our previous conversation?") {
		t.Fatalf("continuation query=%q missing current ask", got)
	}
	if !strings.Contains(got, "Previous user ask: Can you help me plan our Japan travel for May?") {
		t.Fatalf("continuation query=%q missing previous user context", got)
	}
	if !strings.Contains(got, "Previous assistant reply: Yes, we narrowed it to Tokyo and Kyoto") {
		t.Fatalf("continuation query=%q missing previous assistant context", got)
	}
}

func TestBuildContinuationRecallQuery_PreservesSpecificQuestion(t *testing.T) {
	turns := []ConversationTurn{
		{Role: "user", Content: "We were comparing LMStudio and OpenRouter latency yesterday."},
		{Role: "assistant", Content: "We found LMStudio was unreachable from the inspector path."},
	}

	input := "Why is LMStudio unreachable from the inspector panel even though the model is loaded locally?"
	got := buildContinuationRecallQuery(input, turns)
	if got != input {
		t.Fatalf("continuation query=%q want original specific question %q", got, input)
	}
}

func TestRequestedResponseKeys(t *testing.T) {
	got := requestedResponseKeys(ChatRequest{
		ResponseKeys: []string{"owner", "codename", "deploy_window", "rollback_color"},
	})
	want := []string{"owner", "codename", "deploy_window", "rollback_color"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("requestedResponseKeys() = %v, want %v", got, want)
	}
}

func TestRequestedResponseKeys_FallsBackToSchema(t *testing.T) {
	got := requestedResponseKeys(ChatRequest{
		ResponseSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"owner":{"type":"string"},
				"codename":{"type":"string"},
				"deploy_window":{"type":"string"},
				"rollback_color":{"type":"string"}
			}
		}`),
	})
	want := []string{"codename", "deploy_window", "owner", "rollback_color"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("requestedResponseKeys(schema) = %v, want %v", got, want)
	}
}

func TestApplyRequestedOutputFormat(t *testing.T) {
	tests := []struct {
		name string
		text string
		mode requestedOutputFormatMode
		want string
	}{
		{
			name: "compact json object",
			text: "Here you go:\n{\"owner\":\"Mina\",\"codename\":\"amber-river-19\"}",
			mode: requestedOutputFormatCompactJSON,
			want: "{\"owner\":\"Mina\",\"codename\":\"amber-river-19\"}",
		},
		{
			name: "only value first line",
			text: "amber-river-19\nThis is the latest codename.",
			mode: requestedOutputFormatOnlyValue,
			want: "amber-river-19",
		},
		{
			name: "only value trims quotes",
			text: "\"updated-codename\"",
			mode: requestedOutputFormatOnlyValue,
			want: "updated-codename",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyRequestedOutputFormat(tt.text, tt.mode)
			if tt.mode == requestedOutputFormatCompactJSON {
				var gotJSON any
				var wantJSON any
				if err := json.Unmarshal([]byte(got), &gotJSON); err != nil {
					t.Fatalf("unmarshal got json: %v", err)
				}
				if err := json.Unmarshal([]byte(tt.want), &wantJSON); err != nil {
					t.Fatalf("unmarshal want json: %v", err)
				}
				gotBytes, _ := json.Marshal(gotJSON)
				wantBytes, _ := json.Marshal(wantJSON)
				if string(gotBytes) != string(wantBytes) {
					t.Fatalf("applyRequestedOutputFormat() json = %s, want %s", gotBytes, wantBytes)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("applyRequestedOutputFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}
