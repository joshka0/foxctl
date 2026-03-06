package jido

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
)

const DefaultSpawnChildSignal = "agentctl.child.spawn"

type childSpawnSignalData struct {
	RequestID    string         `json:"request_id,omitempty"`
	Tag          string         `json:"tag,omitempty"`
	ChildID      string         `json:"child_id,omitempty"`
	Profile      string         `json:"profile,omitempty"`
	InitialState map[string]any `json:"initial_state,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// SpawnRequestToSignal maps a canonical v2 spawn request into a child-spawn signal.
func SpawnRequestToSignal(parentAgentID string, req spawn.Request, source string) (Signal, error) {
	parentAgentID = strings.TrimSpace(parentAgentID)
	if parentAgentID == "" {
		return Signal{}, fmt.Errorf("parent_agent_id is required")
	}

	req.Role = strings.TrimSpace(req.Role)
	if req.Role == "" {
		return Signal{}, fmt.Errorf("role is required")
	}

	childID := strings.TrimSpace(req.AgentID)
	tag := chooseNonEmpty(childID, req.RequestID, req.Role)
	initialState := buildSpawnInitialState(req)
	metadata := buildSpawnMetadata(req, tag)

	raw, err := json.Marshal(childSpawnSignalData{
		RequestID:    strings.TrimSpace(req.RequestID),
		Tag:          tag,
		ChildID:      childID,
		Profile:      req.Role,
		InitialState: initialState,
		Metadata:     metadata,
	})
	if err != nil {
		return Signal{}, fmt.Errorf("marshal spawn child signal payload: %w", err)
	}

	src := strings.TrimSpace(source)
	if src == "" {
		src = DefaultSignalSource
	}

	signalID := chooseNonEmpty(req.RequestID, childID, tag)

	return Signal{
		ID:            signalID,
		Type:          DefaultSpawnChildSignal,
		Source:        src,
		Subject:       "/agents/" + parentAgentID + "/children/" + tag,
		CorrelationID: chooseNonEmpty(req.CorrelationID, req.RequestID, signalID),
		CausationID:   chooseNonEmpty(req.CausationID, req.RequestID),
		Data:          raw,
	}, nil
}

// SpawnRequestToSignalRequest wraps a spawn request in a runtime child-spawn request.
func SpawnRequestToSignalRequest(parentAgentID string, req spawn.Request, source string) (SignalRequest, error) {
	sig, err := SpawnRequestToSignal(parentAgentID, req, source)
	if err != nil {
		return SignalRequest{}, err
	}

	return SignalRequest{
		RequestID: strings.TrimSpace(req.RequestID),
		AgentID:   strings.TrimSpace(parentAgentID),
		Signal:    sig,
		Mode:      SignalModeCall,
	}, nil
}

func buildSpawnInitialState(req spawn.Request) map[string]any {
	state := map[string]any{}
	putNonEmpty(state, "prompt", req.Prompt)
	putNonEmpty(state, "exec_mode", req.ExecMode)
	putNonEmpty(state, "run_id", req.RunID)
	putNonEmpty(state, "actor_id", req.ActorID)
	putNonEmpty(state, "request_id", req.RequestID)
	putNonEmpty(state, "correlation_id", req.CorrelationID)
	putNonEmpty(state, "causation_id", req.CausationID)
	putPositive(state, "max_iterations", req.MaxIterations)
	putPositive(state, "max_context_tokens", req.MaxContextTokens)
	putPositive(state, "max_auto_turns", req.MaxAutoTurns)
	putPositive(state, "think_interval", req.ThinkInterval)
	return state
}

func buildSpawnMetadata(req spawn.Request, tag string) map[string]any {
	meta := make(map[string]any, len(req.Metadata)+5)
	for key, value := range req.Metadata {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		meta[key] = value
	}
	putNonEmpty(meta, "tag", tag)
	putNonEmpty(meta, "request_id", req.RequestID)
	putNonEmpty(meta, "run_id", req.RunID)
	putNonEmpty(meta, "actor_id", req.ActorID)
	putNonEmpty(meta, "profile", req.Role)
	return meta
}

func putNonEmpty(dst map[string]any, key, value string) {
	if dst == nil {
		return
	}
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		dst[key] = trimmed
	}
}

func putPositive(dst map[string]any, key string, value int) {
	if dst == nil {
		return
	}
	if value > 0 {
		dst[key] = value
	}
}
