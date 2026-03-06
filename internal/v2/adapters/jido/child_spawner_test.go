package jido

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
)

func TestChildSpawner_SpawnChild(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		spawnChildResp: SignalResponse{
			Status:    "spawned",
			MessageID: "spawn-1",
			Data: json.RawMessage(`{
				"child": {
					"tag": "agent:worker-1",
					"agent_id": "agent:worker-1",
					"pid": "#PID<0.1.0>"
				}
			}`),
		},
	}
	now := time.Date(2026, time.March, 6, 10, 30, 0, 0, time.UTC)

	spawner, err := NewChildSpawner(ChildSpawnerConfig{
		Client:       client,
		SignalSource: "/tests",
		Timeout:      12 * time.Second,
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}

	resp, err := spawner.SpawnChild(context.Background(), spawn.Request{
		RequestID:     "req-1",
		Role:          "worker",
		RunID:         "run-1",
		AgentID:       "agent:worker-1",
		ActorID:       "actor:worker-1",
		ParentAgentID: "agent:parent-1",
		Prompt:        "Investigate issue #42",
		Metadata: map[string]any{
			"issue_id": "ISSUE-42",
		},
	})
	if err != nil {
		t.Fatalf("SpawnChild() error = %v", err)
	}

	if client.spawnChildReq.AgentID != "agent:parent-1" {
		t.Fatalf("client.agent_id=%q want agent:parent-1", client.spawnChildReq.AgentID)
	}
	if client.spawnChildReq.Signal.Type != DefaultSpawnChildSignal {
		t.Fatalf("signal.type=%q want %q", client.spawnChildReq.Signal.Type, DefaultSpawnChildSignal)
	}
	if client.spawnChildReq.TimeoutMS != 12_000 {
		t.Fatalf("timeout_ms=%d want 12000", client.spawnChildReq.TimeoutMS)
	}

	if resp.RunID != "run-1" {
		t.Fatalf("run_id=%q want run-1", resp.RunID)
	}
	if resp.AgentID != "agent:worker-1" {
		t.Fatalf("agent_id=%q want agent:worker-1", resp.AgentID)
	}
	if resp.ActorID != "actor:worker-1" {
		t.Fatalf("actor_id=%q want actor:worker-1", resp.ActorID)
	}
	if resp.TurnID != "spawn-1" {
		t.Fatalf("turn_id=%q want spawn-1", resp.TurnID)
	}
	if resp.Status != "spawned" {
		t.Fatalf("status=%q want spawned", resp.Status)
	}
	if resp.CreatedAt != now {
		t.Fatalf("created_at=%s want %s", resp.CreatedAt, now)
	}
}

func TestChildSpawner_RequiresParentAgentID(t *testing.T) {
	t.Parallel()

	spawner, err := NewChildSpawner(ChildSpawnerConfig{Client: &fakeClient{}})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}

	_, err = spawner.SpawnChild(context.Background(), spawn.Request{Role: "worker"})
	if err == nil {
		t.Fatal("expected parent_agent_id validation error")
	}
}

func TestChildSpawner_SpawnChild_InvokesOnSpawnResult(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		spawnChildResp: SignalResponse{
			Status:    "spawned",
			MessageID: "spawn-2",
			Data: json.RawMessage(`{
				"child": {
					"tag": "agent:worker-2",
					"agent_id": "agent:worker-2",
					"pid": "#PID<0.2.0>"
				}
			}`),
		},
	}
	called := 0
	now := time.Date(2026, time.March, 6, 10, 45, 0, 0, time.UTC)

	spawner, err := NewChildSpawner(ChildSpawnerConfig{
		Client: client,
		Now:    func() time.Time { return now },
		OnSpawnResult: func(_ context.Context, req spawn.Request, resp spawn.Response, err error) error {
			called++
			if err != nil {
				t.Fatalf("unexpected callback error: %v", err)
			}
			if req.RequestID != "req-callback" {
				t.Fatalf("callback request_id=%q want req-callback", req.RequestID)
			}
			if resp.AgentID != "agent:worker-2" {
				t.Fatalf("callback agent_id=%q want agent:worker-2", resp.AgentID)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}

	_, err = spawner.SpawnChild(context.Background(), spawn.Request{
		RequestID:     "req-callback",
		Role:          "worker",
		RunID:         "run-callback",
		AgentID:       "agent:worker-2",
		ActorID:       "actor:worker-2",
		ParentAgentID: "agent:parent-2",
	})
	if err != nil {
		t.Fatalf("SpawnChild() error = %v", err)
	}
	if called != 1 {
		t.Fatalf("callback calls=%d want 1", called)
	}
}

func TestChildSpawner_SpawnChild_InvokesOnSpawnResultOnFailure(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		spawnChildErr: errors.New("runtime unavailable"),
	}
	called := 0

	spawner, err := NewChildSpawner(ChildSpawnerConfig{
		Client: client,
		OnSpawnResult: func(_ context.Context, req spawn.Request, resp spawn.Response, err error) error {
			called++
			if err == nil {
				t.Fatal("expected callback error")
			}
			if req.RequestID != "req-failure" {
				t.Fatalf("callback request_id=%q want req-failure", req.RequestID)
			}
			if resp.RunID != "" {
				t.Fatalf("callback run_id=%q want empty", resp.RunID)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}

	_, err = spawner.SpawnChild(context.Background(), spawn.Request{
		RequestID:     "req-failure",
		Role:          "worker",
		ParentAgentID: "agent:parent-3",
	})
	if err == nil {
		t.Fatal("expected SpawnChild() error")
	}
	if called != 1 {
		t.Fatalf("callback calls=%d want 1", called)
	}
}
