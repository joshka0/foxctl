package v1bridge

import (
	"context"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
)

// LegacySpawnRequest is the transport-neutral request expected by a legacy spawner.
type LegacySpawnRequest struct {
	RequestID        string
	Role             string
	Prompt           string
	Name             string
	Slug             string
	MaxIterations    int
	MaxContextTokens int
	ExecMode         string
	MaxAutoTurns     int
	LLMProvider      string
	LLMModel         string
	LLMAPIKey        string
}

// LegacySpawnResult is the transport-neutral result returned by a legacy spawner.
type LegacySpawnResult struct {
	SessionID string
	ActorID   string
	AgentID   string
	Status    string
	Name      string
	NS        string
}

// LegacySpawner is the minimal spawn contract for v1 interop.
type LegacySpawner interface {
	Spawn(ctx context.Context, req LegacySpawnRequest) (LegacySpawnResult, error)
}

// SpawnBridge adapts v2 spawn requests to a legacy spawn backend.
type SpawnBridge struct {
	legacy LegacySpawner
}

// NewSpawnBridge creates a v1 spawn bridge.
func NewSpawnBridge(legacy LegacySpawner) *SpawnBridge {
	return &SpawnBridge{legacy: legacy}
}

// Spawn maps v2 request fields to legacy spawn and normalizes the response.
func (b *SpawnBridge) Spawn(ctx context.Context, req spawn.Request) (spawn.Response, error) {
	if b == nil || b.legacy == nil {
		return spawn.Response{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "legacy spawner is not configured",
			Fatal:   true,
		}
	}

	res, err := b.legacy.Spawn(ctx, LegacySpawnRequest{
		RequestID:        strings.TrimSpace(req.RequestID),
		Role:             strings.TrimSpace(req.Role),
		Prompt:           strings.TrimSpace(req.Prompt),
		MaxIterations:    req.MaxIterations,
		MaxContextTokens: req.MaxContextTokens,
		ExecMode:         strings.TrimSpace(req.ExecMode),
		MaxAutoTurns:     req.MaxAutoTurns,
	})
	if err != nil {
		return spawn.Response{}, &v2errors.V2Error{
			Kind:      v2errors.ErrDependency,
			Message:   "legacy spawn failed",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
		}
	}

	status := strings.TrimSpace(res.Status)
	if status == "" {
		status = "running"
	}
	return spawn.Response{
		RunID:     strings.TrimSpace(res.SessionID),
		AgentID:   strings.TrimSpace(res.AgentID),
		ActorID:   strings.TrimSpace(res.ActorID),
		RequestID: strings.TrimSpace(req.RequestID),
		Status:    status,
	}, nil
}
