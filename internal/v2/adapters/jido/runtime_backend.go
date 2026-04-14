package jido

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
)

var (
	_ coreworker.Spawner     = (*ChildSpawner)(nil)
	_ coreworker.StateReader = (*RuntimeAdapter)(nil)
)

// Worker reads one Jido-backed worker snapshot as a runtime-neutral worker record.
func (a *RuntimeAdapter) Worker(ctx context.Context, req coreworker.LookupRequest) (coreworker.Record, error) {
	if a == nil || a.client == nil {
		return coreworker.Record{}, fmt.Errorf("jido runtime adapter is not configured")
	}

	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(strings.TrimPrefix(req.WorkerID, "jido:"))
	}
	if agentID == "" {
		return coreworker.Record{}, fmt.Errorf("agent_id or worker_id is required")
	}

	resp, err := a.client.State(ctx, StateRequest{AgentID: agentID})
	if err != nil {
		return coreworker.Record{}, err
	}
	return workerRecordFromState(agentID, resp), nil
}

// Children reads Jido-backed children as runtime-neutral worker records.
func (a *RuntimeAdapter) Children(ctx context.Context, req coreworker.ChildrenRequest) ([]coreworker.Record, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("jido runtime adapter is not configured")
	}

	parentAgentID := strings.TrimSpace(req.ParentAgentID)
	if parentAgentID == "" {
		parentAgentID = strings.TrimSpace(strings.TrimPrefix(req.ParentWorkerID, "jido:"))
	}
	if parentAgentID == "" {
		return nil, fmt.Errorf("parent_agent_id or parent_worker_id is required")
	}

	resp, err := a.client.GetChildren(ctx, GetChildrenRequest{AgentID: parentAgentID})
	if err != nil {
		return nil, err
	}

	children := sortedRuntimeChildRefs(resp.Children)
	out := make([]coreworker.Record, 0, len(children))
	for _, child := range children {
		out = append(out, workerRecordFromChildRef(parentAgentID, child))
	}
	return out, nil
}

func workerRecordFromState(agentID string, resp StateResponse) coreworker.Record {
	record := coreworker.Record{
		WorkerID:         jidoWorkerID(agentID),
		BackendKind:      coreworker.BackendJido,
		BackendWorkerRef: strings.TrimSpace(agentID),
		AgentID:          strings.TrimSpace(agentID),
		Status:           coreworker.NormalizeStatus(resp.Status),
		RawState:         append(json.RawMessage(nil), resp.State...),
	}

	if len(resp.State) == 0 || string(resp.State) == "null" {
		return finalizeWorkerRecord(record)
	}

	var root map[string]any
	if err := json.Unmarshal(resp.State, &root); err != nil {
		return finalizeWorkerRecord(record)
	}

	target := mapAt(root, "foxctl")
	if len(target) == 0 {
		target = root
	}

	record.RunID = chooseNonEmpty(
		strings.TrimSpace(stringValue(target["run_id"])),
		strings.TrimSpace(stringValue(root["run_id"])),
	)
	record.SessionID = chooseNonEmpty(
		strings.TrimSpace(stringValue(target["session_id"])),
		strings.TrimSpace(stringValue(root["session_id"])),
	)
	record.WorkspaceID = chooseNonEmpty(
		strings.TrimSpace(stringValue(target["workspace_id"])),
		strings.TrimSpace(stringValue(root["workspace_id"])),
	)
	record.Role = chooseNonEmpty(
		strings.TrimSpace(stringValue(target["role"])),
		strings.TrimSpace(stringValue(root["role"])),
	)
	record.ParentAgentID = chooseNonEmpty(
		strings.TrimSpace(stringValue(target["parent_agent_id"])),
		strings.TrimSpace(stringValue(root["parent_agent_id"])),
	)
	record.StopReason = chooseNonEmpty(
		strings.TrimSpace(stringValue(target["stop_reason"])),
		strings.TrimSpace(stringValue(root["stop_reason"])),
	)
	record.PID = chooseNonEmpty(
		strings.TrimSpace(stringValue(target["pid"])),
		strings.TrimSpace(stringValue(root["pid"])),
	)

	status := chooseNonEmpty(
		normalizeCallbackStatus(stringValue(target["status"])),
		normalizeCallbackStatus(stringValue(root["status"])),
		string(record.Status),
	)
	record.Status = coreworker.NormalizeStatus(status)
	record.Metadata = cloneMap(mapAt(target, "metadata"))
	if len(record.Metadata) == 0 {
		record.Metadata = cloneMap(mapAt(root, "metadata"))
	}
	return finalizeWorkerRecord(record)
}

func workerRecordFromChildRef(parentAgentID string, child ChildRef) coreworker.Record {
	meta := cloneMap(child.Metadata)
	record := coreworker.Record{
		WorkerID:         jidoWorkerID(chooseNonEmpty(strings.TrimSpace(child.AgentID), strings.TrimSpace(child.Tag), strings.TrimSpace(child.PID))),
		BackendKind:      coreworker.BackendJido,
		BackendWorkerRef: chooseNonEmpty(strings.TrimSpace(child.PID), strings.TrimSpace(child.AgentID), strings.TrimSpace(child.Tag)),
		AgentID:          strings.TrimSpace(child.AgentID),
		ParentAgentID:    strings.TrimSpace(parentAgentID),
		ParentWorkerID:   jidoWorkerID(parentAgentID),
		RunID:            dispatchMetaString(meta, "run_id"),
		SessionID:        dispatchMetaString(meta, "session_id"),
		WorkspaceID:      dispatchMetaString(meta, "workspace_id"),
		Role:             chooseNonEmpty(dispatchMetaString(meta, "role"), dispatchMetaString(meta, "profile")),
		Status:           coreworker.StatusUnknown,
		Tag:              strings.TrimSpace(child.Tag),
		PID:              strings.TrimSpace(child.PID),
		Metadata:         meta,
	}
	return finalizeWorkerRecord(record)
}

func finalizeWorkerRecord(record coreworker.Record) coreworker.Record {
	if record.WorkerID == "" {
		record.WorkerID = jidoWorkerID(chooseNonEmpty(record.AgentID, record.BackendWorkerRef, record.Tag))
	}
	if record.Status == "" {
		record.Status = coreworker.StatusUnknown
	}
	return record
}

func jidoWorkerID(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "jido:unknown"
	}
	return "jido:" + ref
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(in))
	for _, key := range keys {
		out[key] = in[key]
	}
	return out
}

func sortedRuntimeChildRefs(children map[string]ChildRef) []ChildRef {
	if len(children) == 0 {
		return nil
	}
	keys := make([]string, 0, len(children))
	for key := range children {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ChildRef, 0, len(keys))
	for _, key := range keys {
		out = append(out, children[key])
	}
	return out
}
