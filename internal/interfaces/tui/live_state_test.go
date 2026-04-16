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
