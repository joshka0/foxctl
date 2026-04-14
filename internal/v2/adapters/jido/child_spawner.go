package jido

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/v2/core/spawn"
)

const defaultSpawnChildTimeout = 10 * time.Second

// ChildSpawnerConfig configures a Jido-backed child spawner.
type ChildSpawnerConfig struct {
	Client        Client
	SignalSource  string
	Timeout       time.Duration
	Now           func() time.Time
	OnSpawnResult func(ctx context.Context, req spawn.Request, resp spawn.Response, err error) error
}

// ChildSpawner maps canonical v2 spawn requests onto runtime.spawn_child calls.
type ChildSpawner struct {
	client        Client
	signalSource  string
	timeout       time.Duration
	now           func() time.Time
	onSpawnResult func(ctx context.Context, req spawn.Request, resp spawn.Response, err error) error
}

// NewChildSpawner builds a runtime-backed child spawner.
func NewChildSpawner(cfg ChildSpawnerConfig) (*ChildSpawner, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("jido child spawner requires client")
	}
	source := strings.TrimSpace(cfg.SignalSource)
	if source == "" {
		source = DefaultSignalSource
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultSpawnChildTimeout
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ChildSpawner{
		client:        cfg.Client,
		signalSource:  source,
		timeout:       timeout,
		now:           now,
		onSpawnResult: cfg.OnSpawnResult,
	}, nil
}

// SpawnChild performs one runtime.spawn_child request and returns the canonical v2 response.
func (s *ChildSpawner) SpawnChild(ctx context.Context, req spawn.Request) (spawn.Response, error) {
	if s == nil || s.client == nil {
		return spawn.Response{}, fmt.Errorf("jido child spawner is not configured")
	}

	parentAgentID := strings.TrimSpace(req.ParentAgentID)
	if parentAgentID == "" {
		return spawn.Response{}, fmt.Errorf("parent_agent_id is required")
	}

	signalReq, err := SpawnRequestToSignalRequest(parentAgentID, req, s.signalSource)
	if err != nil {
		return spawn.Response{}, err
	}
	if signalReq.TimeoutMS <= 0 {
		signalReq.TimeoutMS = s.timeout.Milliseconds()
	}

	resp, err := s.client.SpawnChild(ctx, signalReq)
	if err != nil {
		if s.onSpawnResult != nil {
			if hookErr := s.onSpawnResult(ctx, req, spawn.Response{}, err); hookErr != nil {
				return spawn.Response{}, hookErr
			}
		}
		return spawn.Response{}, err
	}

	child := decodeSpawnedChild(resp.Data)
	status := strings.TrimSpace(resp.Status)
	if status == "" {
		status = "spawned"
	}

	agentID := chooseNonEmpty(child.AgentID, req.AgentID)
	summary := strings.TrimSpace(child.Tag)
	if summary != "" {
		summary = "spawned child " + summary
	}

	out := spawn.Response{
		RunID:     strings.TrimSpace(req.RunID),
		AgentID:   agentID,
		ActorID:   strings.TrimSpace(req.ActorID),
		TurnID:    chooseNonEmpty(resp.MessageID, resp.SignalID),
		RequestID: strings.TrimSpace(req.RequestID),
		Status:    status,
		Summary:   summary,
		CreatedAt: s.now(),
	}
	if s.onSpawnResult != nil {
		if err := s.onSpawnResult(ctx, req, out, nil); err != nil {
			return spawn.Response{}, err
		}
	}
	return out, nil
}

type spawnChildEnvelope struct {
	Child ChildRef `json:"child"`
}

func decodeSpawnedChild(raw json.RawMessage) ChildRef {
	if len(raw) == 0 || string(raw) == "null" {
		return ChildRef{}
	}
	var payload spawnChildEnvelope
	if err := json.Unmarshal(raw, &payload); err == nil && strings.TrimSpace(payload.Child.AgentID) != "" {
		return payload.Child
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return ChildRef{}
	}
	childMap, _ := root["child"].(map[string]any)
	return ChildRef{
		Tag:     strings.TrimSpace(stringValue(childMap["tag"])),
		AgentID: strings.TrimSpace(stringValue(childMap["agent_id"])),
		PID:     strings.TrimSpace(stringValue(childMap["pid"])),
	}
}
