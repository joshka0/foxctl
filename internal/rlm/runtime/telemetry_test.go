package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/observability"
)

func TestObservabilityTelemetrySinkEmitsSanitizedWideEvent(t *testing.T) {
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})
	t.Cleanup(func() {
		observability.SetObsDirForTesting("")
		observability.SetSamplerForTesting(nil)
	})

	sink := ObservabilityTelemetrySink{
		SessionID:   "session-1",
		AgentID:     "agent-1",
		WorkspaceID: "workspace-1",
		Command:     "test.rlm",
	}
	sink.EmitRLMEvent(context.Background(), Event{
		Seq:  7,
		Type: EventTypeREPLCall,
		REPLCall: &REPLCallEvent{
			CallID: "call-1",
			Input:  "print(secret_value)",
		},
	})

	records := readWideEventRecords(t, obsDir)
	if len(records) != 1 {
		t.Fatalf("records=%d want 1", len(records))
	}
	record := records[0]
	if record.Operation != OpRLMREPLCall {
		t.Fatalf("operation=%q want %q", record.Operation, OpRLMREPLCall)
	}
	if record.Component != observability.ComponentAgent {
		t.Fatalf("component=%q want %q", record.Component, observability.ComponentAgent)
	}
	if record.Command != "test.rlm" {
		t.Fatalf("command=%q want test.rlm", record.Command)
	}
	if record.SessionID != "session-1" || record.AgentID != "agent-1" || record.WorkspaceID != "workspace-1" {
		t.Fatalf("identity fields not preserved: session=%q agent=%q workspace=%q", record.SessionID, record.AgentID, record.WorkspaceID)
	}
	if got := intFromData(record.Data, "input_chars"); got != len("print(secret_value)") {
		t.Fatalf("input_chars=%d want %d", got, len("print(secret_value)"))
	}
	if got, ok := record.Data["always_sample"].(bool); !ok || !got {
		t.Fatalf("always_sample=%v want true", record.Data["always_sample"])
	}
	if _, ok := record.Data["input"]; ok {
		t.Fatalf("raw REPL input leaked into telemetry data: %#v", record.Data)
	}
	if _, ok := record.Data["code"]; ok {
		t.Fatalf("raw code leaked into telemetry data: %#v", record.Data)
	}
}

func TestObservabilityTelemetrySinkIncludesParentToolCallFields(t *testing.T) {
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})
	t.Cleanup(func() {
		observability.SetObsDirForTesting("")
		observability.SetSamplerForTesting(nil)
	})

	sink := ObservabilityTelemetrySink{
		SessionID:   "session-parent",
		AgentID:     "agent-parent",
		WorkspaceID: "workspace-1",
		Command:     "test.rlm",
	}
	sink.EmitRLMEvent(context.Background(), Event{
		Seq:  9,
		Type: EventTypeParentLLMCall,
		ParentLLMCall: &ParentLLMCallEvent{
			CallID:           "parent-1",
			Model:            "test-model",
			PromptTokens:     123,
			CompletionTokens: 45,
			FinishReason:     "tool_calls",
			ToolCalls:        2,
			ToolNames:        []string{RLMQueryToolName, RLMWaitToolName},
		},
	})

	records := readWideEventRecords(t, obsDir)
	if len(records) != 1 {
		t.Fatalf("records=%d want 1", len(records))
	}
	record := records[0]
	if record.Operation != OpRLMParentLLMCall {
		t.Fatalf("operation=%q want %q", record.Operation, OpRLMParentLLMCall)
	}
	if got := intFromData(record.Data, "tool_calls"); got != 2 {
		t.Fatalf("tool_calls=%d want 2", got)
	}
	if got := record.Data["finish_reason"]; got != "tool_calls" {
		t.Fatalf("finish_reason=%v want tool_calls", got)
	}
	names, ok := record.Data["tool_names"].([]any)
	if !ok {
		t.Fatalf("tool_names type=%T data=%#v", record.Data["tool_names"], record.Data)
	}
	if len(names) != 2 || names[0] != RLMQueryToolName || names[1] != RLMWaitToolName {
		t.Fatalf("tool_names=%#v", names)
	}
	if _, ok := record.Data["prompt"]; ok {
		t.Fatalf("raw prompt leaked into telemetry data: %#v", record.Data)
	}
	if _, ok := record.Data["code"]; ok {
		t.Fatalf("raw code leaked into telemetry data: %#v", record.Data)
	}
}

func TestObservabilityTelemetrySinkIncludesSubcallIdentityFields(t *testing.T) {
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})
	t.Cleanup(func() {
		observability.SetObsDirForTesting("")
		observability.SetSamplerForTesting(nil)
	})

	sink := ObservabilityTelemetrySink{
		SessionID:   "session-2",
		AgentID:     "agent-root",
		WorkspaceID: "workspace-1",
		Command:     "test.rlm",
	}
	sink.EmitRLMEvent(context.Background(), Event{
		Seq:  8,
		Type: EventTypeSubcallStart,
		Subcall: &SubcallEvent{
			CallID:          "subcall-1",
			Name:            "rlm_query",
			Depth:           1,
			AgentID:         "agent-root/rlm-0001",
			ParentAgentID:   "agent-root",
			OutputNamespace: "runs/run-main/agents/agent-root/rlm-0001",
		},
	})

	records := readWideEventRecords(t, obsDir)
	if len(records) != 1 {
		t.Fatalf("records=%d want 1", len(records))
	}
	record := records[0]
	if got := stringFromData(record.Data, "agent_id"); got != "agent-root/rlm-0001" {
		t.Fatalf("subcall agent_id=%q", got)
	}
	if got := stringFromData(record.Data, "parent_agent_id"); got != "agent-root" {
		t.Fatalf("subcall parent_agent_id=%q", got)
	}
	if got := stringFromData(record.Data, "output_namespace"); got != "runs/run-main/agents/agent-root/rlm-0001" {
		t.Fatalf("subcall output_namespace=%q", got)
	}
}

func TestOperationForRLMEventNodeLifecycleMappings(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		eventType EventType
		want      string
	}{
		{eventType: EventTypeNodeQueued, want: OpRLMNodeQueued},
		{eventType: EventTypeNodeStarted, want: OpRLMNodeStarted},
		{eventType: EventTypeNodeWaitStarted, want: OpRLMNodeWaitStarted},
		{eventType: EventTypeNodeWaitCompleted, want: OpRLMNodeWaitCompleted},
		{eventType: EventTypeNodeCompleted, want: OpRLMNodeCompleted},
		{eventType: EventTypeNodeFailed, want: OpRLMNodeFailed},
		{eventType: EventTypeNodeCanceled, want: OpRLMNodeCanceled},
		{eventType: EventTypeContract, want: OpRLMContract},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(string(tc.eventType), func(t *testing.T) {
			t.Parallel()
			if got := operationForRLMEvent(tc.eventType); got != tc.want {
				t.Fatalf("operationForRLMEvent(%q) = %q, want %q", tc.eventType, got, tc.want)
			}
		})
	}
}

func TestObservabilityTelemetrySinkIncludesContractFields(t *testing.T) {
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})
	t.Cleanup(func() {
		observability.SetObsDirForTesting("")
		observability.SetSamplerForTesting(nil)
	})

	sink := ObservabilityTelemetrySink{
		SessionID:   "session-contract",
		AgentID:     "agent-root",
		WorkspaceID: "workspace-2",
		Command:     "test.rlm",
	}
	sink.EmitRLMEvent(context.Background(), Event{
		Seq:  11,
		Type: EventTypeContract,
		Contract: &ContractEvent{
			Boundary:            "tool_input",
			Phase:               "lambda_verify",
			Tool:                RLMQueryToolName,
			Status:              "repaired",
			IssueKind:           "numeric_string",
			IssuePath:           "$.max_iterations",
			RepairRule:          "parse_int_string",
			RevalidateOK:        true,
			Message:             "normalized",
			CandidateSolved:     2,
			CandidateRegistered: 1,
			ToolInputBytes:      64,
			RepairedInputBytes:  48,
		},
	})

	records := readWideEventRecords(t, obsDir)
	if len(records) != 1 {
		t.Fatalf("records=%d want 1", len(records))
	}
	record := records[0]
	if record.Operation != OpRLMContract {
		t.Fatalf("operation=%q want %q", record.Operation, OpRLMContract)
	}
	if got := stringFromData(record.Data, "boundary"); got != "tool_input" {
		t.Fatalf("boundary=%q", got)
	}
	if got := stringFromData(record.Data, "tool"); got != RLMQueryToolName {
		t.Fatalf("tool=%q", got)
	}
	if got := stringFromData(record.Data, "status"); got != "repaired" {
		t.Fatalf("status=%q", got)
	}
	if got := stringFromData(record.Data, "repair_rule"); got != "parse_int_string" {
		t.Fatalf("repair_rule=%q", got)
	}
	if got := intFromData(record.Data, "candidate_registered"); got != 1 {
		t.Fatalf("candidate_registered=%d", got)
	}
	if got := intFromData(record.Data, "tool_input_bytes"); got != 64 {
		t.Fatalf("tool_input_bytes=%d", got)
	}
	if _, ok := record.Data["input"]; ok {
		t.Fatalf("raw input leaked into contract telemetry: %#v", record.Data)
	}
	if _, ok := record.Data["args"]; ok {
		t.Fatalf("raw args leaked into contract telemetry: %#v", record.Data)
	}
}

func TestObservabilityTelemetrySinkIncludesNodeAndWaitFields(t *testing.T) {
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})
	t.Cleanup(func() {
		observability.SetObsDirForTesting("")
		observability.SetSamplerForTesting(nil)
	})

	sink := ObservabilityTelemetrySink{
		SessionID:   "session-3",
		AgentID:     "agent-root",
		WorkspaceID: "workspace-2",
		Command:     "test.rlm",
	}
	sink.EmitRLMEvent(context.Background(), Event{
		Seq:  9,
		Type: EventTypeNodeCompleted,
		Node: &NodeEvent{
			RunID:                   "run-1",
			NodeID:                  "node-4",
			ParentNodeID:            "node-1",
			Depth:                   2,
			Status:                  NodeStatusCompleted,
			OutputNamespace:         "runs/run-1/nodes/node-4",
			Message:                 "node completed",
			RequiredSubcalls:        1,
			RequiredSubcallAttempts: 2,
			RecursiveSubcallsUsed:   1,
		},
	})
	sink.EmitRLMEvent(context.Background(), Event{
		Seq:  10,
		Type: EventTypeNodeWaitCompleted,
		Wait: &WaitEvent{
			RunID:        "run-1",
			ParentNodeID: "node-1",
			ChildIDs:     []string{"node-2", "node-3"},
			Completed:    1,
			Failed:       1,
			Pending:      0,
			MinComplete:  1,
			TimeoutMS:    1200,
		},
	})

	records := readWideEventRecords(t, obsDir)
	if len(records) != 2 {
		t.Fatalf("records=%d want 2", len(records))
	}

	nodeRecord := records[0]
	if nodeRecord.Operation != OpRLMNodeCompleted {
		t.Fatalf("node operation=%q want %q", nodeRecord.Operation, OpRLMNodeCompleted)
	}
	if got := stringFromData(nodeRecord.Data, "run_id"); got != "run-1" {
		t.Fatalf("node run_id=%q", got)
	}
	if got := stringFromData(nodeRecord.Data, "node_id"); got != "node-4" {
		t.Fatalf("node node_id=%q", got)
	}
	if got := stringFromData(nodeRecord.Data, "parent_node_id"); got != "node-1" {
		t.Fatalf("node parent_node_id=%q", got)
	}
	if got := intFromData(nodeRecord.Data, "depth"); got != 2 {
		t.Fatalf("node depth=%d", got)
	}
	if got := stringFromData(nodeRecord.Data, "status"); got != string(NodeStatusCompleted) {
		t.Fatalf("node status=%q", got)
	}
	if got := stringFromData(nodeRecord.Data, "output_namespace"); got != "runs/run-1/nodes/node-4" {
		t.Fatalf("node output_namespace=%q", got)
	}
	if got := stringFromData(nodeRecord.Data, "message"); got != "node completed" {
		t.Fatalf("node message=%q", got)
	}
	if got := intFromData(nodeRecord.Data, "required_subcalls"); got != 1 {
		t.Fatalf("node required_subcalls=%d", got)
	}
	if got := intFromData(nodeRecord.Data, "required_subcall_attempts"); got != 2 {
		t.Fatalf("node required_subcall_attempts=%d", got)
	}
	if got := intFromData(nodeRecord.Data, "recursive_subcalls_used"); got != 1 {
		t.Fatalf("node recursive_subcalls_used=%d", got)
	}

	waitRecord := records[1]
	if waitRecord.Operation != OpRLMNodeWaitCompleted {
		t.Fatalf("wait operation=%q want %q", waitRecord.Operation, OpRLMNodeWaitCompleted)
	}
	if got := stringFromData(waitRecord.Data, "run_id"); got != "run-1" {
		t.Fatalf("wait run_id=%q", got)
	}
	if got := stringFromData(waitRecord.Data, "parent_node_id"); got != "node-1" {
		t.Fatalf("wait parent_node_id=%q", got)
	}
	if got := stringSliceFromData(waitRecord.Data, "child_ids"); !reflect.DeepEqual(got, []string{"node-2", "node-3"}) {
		t.Fatalf("wait child_ids=%v", got)
	}
	if got := intFromData(waitRecord.Data, "completed"); got != 1 {
		t.Fatalf("wait completed=%d", got)
	}
	if got := intFromData(waitRecord.Data, "failed"); got != 1 {
		t.Fatalf("wait failed=%d", got)
	}
	if got := intFromData(waitRecord.Data, "pending"); got != 0 {
		t.Fatalf("wait pending=%d", got)
	}
	if got := intFromData(waitRecord.Data, "min_complete"); got != 1 {
		t.Fatalf("wait min_complete=%d", got)
	}
	if got := intFromData(waitRecord.Data, "timeout_ms"); got != 1200 {
		t.Fatalf("wait timeout_ms=%d", got)
	}
}

func readWideEventRecords(t *testing.T, obsDir string) []observability.WideEvent {
	t.Helper()

	filePath := filepath.Join(obsDir, "events", observability.WideEventFileName+".ndjson")
	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("open wide events: %v", err)
	}
	defer f.Close()

	var records []observability.WideEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var record observability.WideEvent
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode wide event: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan wide events: %v", err)
	}
	return records
}

func intFromData(data map[string]any, key string) int {
	value, ok := data[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}

func stringFromData(data map[string]any, key string) string {
	value, ok := data[key]
	if !ok {
		return ""
	}
	typed, _ := value.(string)
	return typed
}

func stringSliceFromData(data map[string]any, key string) []string {
	value, ok := data[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
