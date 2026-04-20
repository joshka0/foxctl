package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLoadInitialShellStateSkipsAPIWhenBaseURLEmpty(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer srv.Close()

	state, err := LoadInitialShellState(context.Background(), Options{
		APIBaseURL: "",
	})
	if err != nil {
		t.Fatalf("LoadInitialShellState error: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("unexpected API calls: got %d, want 0", calls.Load())
	}
	if len(state.Workers) == 0 {
		t.Fatal("Workers = 0, want default shell workers")
	}
}

func TestLoadInitialShellStateEnrichesWorkersFromAgentsDeterministically(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/agents" {
			t.Fatalf("path = %s, want /api/agents", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "7" {
			t.Fatalf("limit query = %q, want %q", got, "7")
		}

		_ = json.NewEncoder(w).Encode(ListAgentsResponse{
			Agents: []AgentRecord{
				{
					ID:            "b2",
					Name:          "zeta",
					State:         "running",
					Role:          "researcher",
					ExecMode:      "proactive",
					LLMProvider:   "lmstudio",
					LLMModel:      "gpt-5.3",
					WorkspaceRoot: "/ws/z",
				},
				{
					ID:    "a2",
					Name:  "same",
					State: "idle",
				},
				{
					ID:    "a1",
					Name:  "same",
					State: "running",
				},
				{
					ID:        "c3",
					Slug:      "alpha-slug",
					State:     "blocked",
					Role:      "coder",
					ExecMode:  "reactive",
					LLMModel:  "mini",
					Namespace: "ws-alpha",
				},
				{
					ID:    "d4",
					Name:  "Alpha",
					State: "waiting",
					Role:  "planner",
				},
			},
			Total: 5,
		})
	}))
	defer srv.Close()

	state, err := LoadInitialShellState(context.Background(), Options{
		APIBaseURL: srv.URL,
		AgentLimit: 7,
	})
	if err != nil {
		t.Fatalf("LoadInitialShellState error: %v", err)
	}

	if got, want := len(state.Workers), 5; got != want {
		t.Fatalf("len(Workers) = %d, want %d", got, want)
	}

	expected := []WorkerSummary{
		{Name: "Alpha", Status: "waiting", Task: "role=planner"},
		{Name: "alpha-slug", Status: "blocked", Task: "role=coder | mode=reactive | model=mini | workspace=ws-alpha"},
		{Name: "same", Status: "running", Task: "no runtime metadata"},
		{Name: "same", Status: "idle", Task: "no runtime metadata"},
		{Name: "zeta", Status: "running", Task: "role=researcher | mode=proactive | model=lmstudio/gpt-5.3 | workspace=/ws/z"},
	}

	for i := range expected {
		if state.Workers[i] != expected[i] {
			t.Fatalf("Workers[%d] = %#v, want %#v", i, state.Workers[i], expected[i])
		}
	}
}

func TestLoadInitialShellStatePropagatesAgentListFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := LoadInitialShellState(context.Background(), Options{
		APIBaseURL: srv.URL,
		AgentLimit: 3,
	})
	if err == nil {
		t.Fatal("LoadInitialShellState error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "--api-base-url") {
		t.Fatalf("error = %q, want actionable --api-base-url hint", err)
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error type = %T, want wrapped *HTTPStatusError", err)
	}
	if statusErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("StatusCode = %d, want %d", statusErr.StatusCode, http.StatusBadGateway)
	}
}

func TestLoadInitialShellStatePropagatesDecodeFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not-json"))
	}))
	defer srv.Close()

	_, err := LoadInitialShellState(context.Background(), Options{
		APIBaseURL: srv.URL,
	})
	if err == nil {
		t.Fatal("LoadInitialShellState error = nil, want decode failure")
	}
	if !strings.Contains(err.Error(), "decode response body") {
		t.Fatalf("error = %q, want decode context", err)
	}
}

func TestLoadInitialShellStateUsesDefaultLimitForNonPositiveValues(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "25" {
			t.Fatalf("limit query = %q, want %q", got, "25")
		}
		_ = json.NewEncoder(w).Encode(ListAgentsResponse{})
	}))
	defer srv.Close()

	_, err := LoadInitialShellState(context.Background(), Options{
		APIBaseURL: srv.URL,
		AgentLimit: 0,
	})
	if err != nil {
		t.Fatalf("LoadInitialShellState error: %v", err)
	}
}

func TestLoadInitialShellStateErrorsWhenConsoleSessionIDWithoutAPIBaseURL(t *testing.T) {
	t.Parallel()

	_, err := LoadInitialShellState(context.Background(), Options{
		ConsoleSessionID: "sess-7",
	})
	if err == nil {
		t.Fatal("LoadInitialShellState error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--api-base-url") {
		t.Fatalf("error = %q, want actionable --api-base-url hint", err)
	}
}

func TestLoadInitialShellStateDoesNotCallConsoleSessionByDefault(t *testing.T) {
	t.Parallel()

	var consoleCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode(ListAgentsResponse{
				Agents: []AgentRecord{
					{
						ID:    "a1",
						Name:  "Agent One",
						State: "running",
					},
				},
				Total: 1,
			})
		case strings.HasPrefix(r.URL.Path, "/api/console/sessions/"):
			consoleCalls.Add(1)
			http.Error(w, "unexpected console request", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected route: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	state, err := LoadInitialShellState(context.Background(), Options{
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("LoadInitialShellState error: %v", err)
	}
	if got := consoleCalls.Load(); got != 0 {
		t.Fatalf("console session calls = %d, want 0", got)
	}
	if got := len(state.Workers); got != 1 {
		t.Fatalf("len(Workers) = %d, want 1", got)
	}
}

func TestLoadInitialShellStateMapsConsoleSessionTranscriptWhenRequested(t *testing.T) {
	t.Parallel()

	var sessionCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode(ListAgentsResponse{})
		case r.Method == http.MethodGet && r.URL.Path == "/api/console/sessions/sess-99":
			sessionCalls.Add(1)
			_, _ = w.Write([]byte(`{
				"session": {"id":"sess-99","workspace":".","profile":"explorer"},
				"messages": [
					{"role":"user","content":"hello"},
					{"role":"assistant","content":"world"},
					{"role":"assistant","content":"","tool_call_id":"call-1"}
				],
				"inflight":"corr-live"
			}`))
		default:
			t.Fatalf("unexpected route: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	state, err := LoadInitialShellState(context.Background(), Options{
		APIBaseURL:       srv.URL,
		ConsoleSessionID: "sess-99",
	})
	if err != nil {
		t.Fatalf("LoadInitialShellState error: %v", err)
	}
	if got := sessionCalls.Load(); got != 1 {
		t.Fatalf("console session calls = %d, want 1", got)
	}

	expected := []TranscriptEntry{
		{Speaker: "user", Kind: "console", Text: "hello"},
		{Speaker: "assistant", Kind: "console", Text: "world"},
		{Speaker: "assistant", Kind: "tool", Text: "tool call: call-1"},
		{Speaker: "system", Kind: "inflight", Text: "in-flight correlation: corr-live"},
	}
	if got := len(state.Transcript); got != len(expected) {
		t.Fatalf("len(Transcript) = %d, want %d", got, len(expected))
	}
	for i := range expected {
		if state.Transcript[i] != expected[i] {
			t.Fatalf("Transcript[%d] = %#v, want %#v", i, state.Transcript[i], expected[i])
		}
	}
}

func TestLoadInitialShellStateMapsAgentCompanionWhenRequested(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode(ListAgentsResponse{
				Agents: []AgentRecord{{
					ID:          "agent-1",
					Name:        "Local Fox",
					Role:        "coder",
					State:       "running",
					LLMProvider: "lmstudio",
					LLMModel:    "local-model",
				}},
				Total: 1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents/agent-1":
			_ = json.NewEncoder(w).Encode(GetAgentResponse{
				Agent: AgentRecord{
					ID:          "agent-1",
					Name:        "Local Fox",
					Role:        "coder",
					State:       "running",
					LLMProvider: "lmstudio",
					LLMModel:    "local-model",
				},
			})
		default:
			t.Fatalf("unexpected route: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	state, err := LoadInitialShellState(context.Background(), Options{
		APIBaseURL: srv.URL,
		AgentID:    "agent-1",
	})
	if err != nil {
		t.Fatalf("LoadInitialShellState error: %v", err)
	}
	if state.Assistant.Name != "Local Fox" {
		t.Fatalf("Assistant.Name = %q, want Local Fox", state.Assistant.Name)
	}
	if state.Assistant.Provider != "lmstudio" {
		t.Fatalf("Assistant.Provider = %q, want lmstudio", state.Assistant.Provider)
	}
	if got := len(state.Transcript); got != 1 {
		t.Fatalf("len(Transcript) = %d, want 1", got)
	}
	if !strings.Contains(state.Transcript[0].Text, "Live foxctl companion attached: Local Fox") {
		t.Fatalf("Transcript[0].Text = %q, want agent attachment message", state.Transcript[0].Text)
	}
	if got := len(state.Memory); got != 3 {
		t.Fatalf("len(Memory) = %d, want 3 live usage cards", got)
	}
	if !strings.Contains(state.Memory[0].Summary, "Codex or Claude Code") {
		t.Fatalf("Memory[0].Summary = %q, want Codex/Claude usage guidance", state.Memory[0].Summary)
	}
	if state.Workers[0].Name != "Local Fox" || state.Workers[0].Status != "running" {
		t.Fatalf("Workers[0] = %#v, want attached Local Fox worker", state.Workers[0])
	}
}

func TestLoadInitialShellStateReturnsActionableErrorWhenConsoleLookupFails(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agents":
			_ = json.NewEncoder(w).Encode(ListAgentsResponse{})
		case "/api/console/sessions/missing":
			http.Error(w, "session not found", http.StatusNotFound)
		default:
			t.Fatalf("unexpected route: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	_, err := LoadInitialShellState(context.Background(), Options{
		APIBaseURL:       srv.URL,
		ConsoleSessionID: "missing",
	})
	if err == nil {
		t.Fatal("LoadInitialShellState error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "GET /api/console/sessions") {
		t.Fatalf("error = %q, want actionable session lookup hint", err)
	}
}
