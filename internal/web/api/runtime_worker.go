package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/contextplane/taskhistory"
	"github.com/jkatigb/agentctl/internal/platform/config"
	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
	libsqlworkers "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/workers"
	coreworker "github.com/jkatigb/agentctl/internal/v2/core/worker"
)

func newJidoStateReader(client v2jido.Client) (coreworker.StateReader, error) {
	return v2jido.NewRuntimeAdapter(v2jido.RuntimeAdapterConfig{Client: client})
}

func decodeRuntimeWorkerState(ctx context.Context, cfg config.Config, log zerolog.Logger, agentID string, raw json.RawMessage, msg string) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var state any
	if err := json.Unmarshal(raw, &state); err != nil {
		log.Debug().Err(err).Str("agent_id", agentID).Msg(msg)
		return string(raw)
	}
	if stateMap, ok := state.(map[string]any); ok {
		state = taskhistory.RefreshJidoRuntimeState(ctx, cfg.Storage.Root, cfg.Paths.CAS, stateMap)
	}
	return state
}

func mergeRuntimeMetadata(base, overlay map[string]any) map[string]any {
	switch {
	case len(base) == 0 && len(overlay) == 0:
		return nil
	case len(base) == 0:
		return overlay
	case len(overlay) == 0:
		return base
	}
	out := make(map[string]any, len(base)+len(overlay))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range overlay {
		out[key] = value
	}
	return out
}

func loadOptionalRuntimeStateReader(ctx context.Context, cfg config.Config) (coreworker.StateReader, func() error, bool, error) {
	if strings.EqualFold(ResolveOrchestrationRuntimeBackend(), orchestrationRuntimeBackendGoruntimeAPI) {
		store, closeFn, err := libsqlworkers.Open(ctx, cfg.Storage.Root)
		if err != nil {
			return nil, nil, false, err
		}
		return store, closeFn, true, nil
	}

	client, available, err := loadOptionalJidoClient()
	if err != nil || !available {
		return nil, nil, available, err
	}
	reader, err := newJidoStateReader(client)
	if err != nil {
		return nil, nil, false, err
	}
	return reader, func() error { return nil }, true, nil
}

func workerChildRefs(children []coreworker.Record) map[string]v2jido.ChildRef {
	if len(children) == 0 {
		return nil
	}
	out := make(map[string]v2jido.ChildRef, len(children))
	for _, child := range children {
		key := strings.TrimSpace(child.AgentID)
		if key == "" {
			key = strings.TrimSpace(child.WorkerID)
		}
		if key == "" {
			continue
		}
		out[key] = v2jido.ChildRef{
			Tag:      strings.TrimSpace(child.Tag),
			AgentID:  strings.TrimSpace(child.AgentID),
			PID:      strings.TrimSpace(child.PID),
			Metadata: child.Metadata,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func runtimeRecentLogs(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil
	}
	agentctl, _ := state["agentctl"].(map[string]any)
	entries, _ := agentctl["recent_logs"].([]any)
	if len(entries) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		entryMap, _ := entry.(map[string]any)
		if entryMap == nil {
			continue
		}
		out = append(out, map[string]any{
			"stream": strings.TrimSpace(fmt.Sprint(entryMap["stream"])),
			"text":   strings.TrimSpace(fmt.Sprint(entryMap["text"])),
			"ts":     normalizeRuntimeLogTS(entryMap["ts"]),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeRuntimeLogTS(value any) string {
	trimmed := strings.TrimSpace(fmt.Sprint(value))
	if trimmed == "" || trimmed == "<nil>" {
		return ""
	}
	if ts, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return ts.UTC().Format(time.RFC3339Nano)
	}
	return trimmed
}
