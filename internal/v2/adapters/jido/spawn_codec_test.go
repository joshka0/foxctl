package jido

import (
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
)

func TestSpawnRequestToSignalRequest(t *testing.T) {
	t.Parallel()

	req, err := SpawnRequestToSignalRequest("agent:overseer", spawn.Request{
		RequestID:     "req-1",
		Role:          "worker",
		Prompt:        "Investigate issue #42",
		ExecMode:      "autonomous",
		RunID:         "run-1",
		AgentID:       "agent:worker-1",
		ActorID:       "actor:worker-1",
		CorrelationID: "corr-1",
		CausationID:   "cause-1",
		Metadata: map[string]any{
			"issue_id": "ISSUE-42",
			"plugin_config": map[string]any{
				"binary": "/tmp/agentctl",
			},
		},
		MaxIterations:    3,
		MaxContextTokens: 4096,
		MaxAutoTurns:     2,
		ThinkInterval:    15,
	}, "/tests")
	if err != nil {
		t.Fatalf("SpawnRequestToSignalRequest() error = %v", err)
	}

	if req.AgentID != "agent:overseer" {
		t.Fatalf("agent_id=%q want agent:overseer", req.AgentID)
	}
	if req.Mode != SignalModeCall {
		t.Fatalf("mode=%q want %q", req.Mode, SignalModeCall)
	}
	if req.Signal.Type != DefaultSpawnChildSignal {
		t.Fatalf("signal.type=%q want %q", req.Signal.Type, DefaultSpawnChildSignal)
	}
	if req.Signal.Subject != "/agents/agent:overseer/children/agent:worker-1" {
		t.Fatalf("signal.subject=%q unexpected", req.Signal.Subject)
	}

	var data map[string]any
	if err := json.Unmarshal(req.Signal.Data, &data); err != nil {
		t.Fatalf("unmarshal signal data: %v", err)
	}

	if got := data["tag"]; got != "agent:worker-1" {
		t.Fatalf("signal.data.tag=%v want agent:worker-1", got)
	}
	if got := data["child_id"]; got != "agent:worker-1" {
		t.Fatalf("signal.data.child_id=%v want agent:worker-1", got)
	}
	if got := data["profile"]; got != "worker" {
		t.Fatalf("signal.data.profile=%v want worker", got)
	}

	initialState, _ := data["initial_state"].(map[string]any)
	if got := initialState["prompt"]; got != "Investigate issue #42" {
		t.Fatalf("signal.data.initial_state.prompt=%v want prompt", got)
	}
	if got := initialState["max_iterations"]; got != float64(3) {
		t.Fatalf("signal.data.initial_state.max_iterations=%v want 3", got)
	}
	if got := initialState["think_interval"]; got != float64(15) {
		t.Fatalf("signal.data.initial_state.think_interval=%v want 15", got)
	}

	metadata, _ := data["metadata"].(map[string]any)
	if got := metadata["request_id"]; got != "req-1" {
		t.Fatalf("signal.data.metadata.request_id=%v want req-1", got)
	}
	if got := metadata["issue_id"]; got != "ISSUE-42" {
		t.Fatalf("signal.data.metadata.issue_id=%v want ISSUE-42", got)
	}
	pluginConfig, _ := metadata["plugin_config"].(map[string]any)
	if got := pluginConfig["binary"]; got != "/tmp/agentctl" {
		t.Fatalf("signal.data.metadata.plugin_config.binary=%v want /tmp/agentctl", got)
	}
}

func TestSpawnRequestToSignalValidation(t *testing.T) {
	t.Parallel()

	_, err := SpawnRequestToSignal("", spawn.Request{Role: "worker"}, DefaultSignalSource)
	if err == nil {
		t.Fatal("expected parent agent validation error")
	}

	_, err = SpawnRequestToSignal("agent:overseer", spawn.Request{}, DefaultSignalSource)
	if err == nil {
		t.Fatal("expected role validation error")
	}
}
