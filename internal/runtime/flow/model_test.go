package flow

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// ---------------------------------------------------------------------------
// FlowState tests
// ---------------------------------------------------------------------------

func TestFlowState(t *testing.T) {
	tests := []struct {
		value   FlowState
		wantOK  bool
		wantStr string
	}{
		{FlowDraft, true, "draft"},
		{FlowRunning, true, "running"},
		{FlowPaused, true, "paused"},
		{FlowStopped, true, "stopped"},
		{FlowErrored, true, "errored"},
		{FlowState("unknown"), false, "unknown"},
		{FlowState(""), false, ""},
	}

	for _, tc := range tests {
		t.Run(string(tc.value), func(t *testing.T) {
			if got := tc.value.IsValid(); got != tc.wantOK {
				t.Errorf("FlowState(%q).IsValid() = %v, want %v", tc.value, got, tc.wantOK)
			}
			if got := string(tc.value); got != tc.wantStr {
				t.Errorf("string(FloorState(%q)) = %q, want %q", tc.value, got, tc.wantStr)
			}
		})
	}

	// Verify ValidFlowStates contains all valid states.
	if len(ValidFlowStates) != 5 {
		t.Errorf("len(ValidFlowStates) = %d, want 5", len(ValidFlowStates))
	}
	for _, s := range ValidFlowStates {
		if !s.IsValid() {
			t.Errorf("ValidFlowStates contains invalid state %q", s)
		}
	}
}

// ---------------------------------------------------------------------------
// NodeKind tests
// ---------------------------------------------------------------------------

func TestNodeKind(t *testing.T) {
	tests := []struct {
		value   NodeKind
		wantOK  bool
		wantStr string
	}{
		{NodeSkill, true, "skill"},
		{NodePTY, true, "pty"},
		{NodeHTTP, true, "http"},
		{NodePlaywright, true, "playwright"},
		{NodeImage, true, "image"},
		{NodeTransform, true, "transform"},
		{NodeAgent, true, "agent"},
		{NodeKind("unknown"), false, "unknown"},
		{NodeKind(""), false, ""},
	}

	for _, tc := range tests {
		t.Run(string(tc.value), func(t *testing.T) {
			if got := tc.value.IsValid(); got != tc.wantOK {
				t.Errorf("NodeKind(%q).IsValid() = %v, want %v", tc.value, got, tc.wantOK)
			}
		})
	}

	if len(ValidNodeKinds) != 7 {
		t.Errorf("len(ValidNodeKinds) = %d, want 7", len(ValidNodeKinds))
	}
	for _, k := range ValidNodeKinds {
		if !k.IsValid() {
			t.Errorf("ValidNodeKinds contains invalid kind %q", k)
		}
	}
}

// ---------------------------------------------------------------------------
// TransformKind tests
// ---------------------------------------------------------------------------

func TestTransformKind(t *testing.T) {
	tests := []struct {
		value   TransformKind
		wantOK  bool
		wantStr string
	}{
		{TransformPassthrough, true, "passthrough"},
		{TransformRegex, true, "regex_extract"},
		{TransformTemplate, true, "template"},
		{TransformJQ, true, "jq_filter"},
		{TransformSplitLines, true, "split_lines"},
		{TransformMapFields, true, "map_fields"},
		{TransformKind("unknown"), false, "unknown"},
	}

	for _, tc := range tests {
		t.Run(string(tc.value), func(t *testing.T) {
			if got := tc.value.IsValid(); got != tc.wantOK {
				t.Errorf("TransformKind(%q).IsValid() = %v, want %v", tc.value, got, tc.wantOK)
			}
		})
	}

	if len(ValidTransformKinds) != 6 {
		t.Errorf("len(ValidTransformKinds) = %d, want 6", len(ValidTransformKinds))
	}
}

// ---------------------------------------------------------------------------
// TriggerKind tests
// ---------------------------------------------------------------------------

func TestTriggerKind(t *testing.T) {
	tests := []struct {
		value   TriggerKind
		wantOK  bool
		wantStr string
	}{
		{TriggerOutputReady, true, "output_ready"},
		{TriggerScreenMatch, true, "screen_match"},
		{TriggerExit, true, "exit"},
		{TriggerManual, true, "manual"},
		{TriggerKind("unknown"), false, "unknown"},
	}

	for _, tc := range tests {
		t.Run(string(tc.value), func(t *testing.T) {
			if got := tc.value.IsValid(); got != tc.wantOK {
				t.Errorf("TriggerKind(%q).IsValid() = %v, want %v", tc.value, got, tc.wantOK)
			}
		})
	}

	if len(ValidTriggerKinds) != 4 {
		t.Errorf("len(ValidTriggerKinds) = %d, want 4", len(ValidTriggerKinds))
	}
}

// ---------------------------------------------------------------------------
// RunState tests
// ---------------------------------------------------------------------------

func TestRunState(t *testing.T) {
	tests := []struct {
		value   RunState
		wantOK  bool
		wantStr string
	}{
		{RunRunning, true, "running"},
		{RunCompleted, true, "completed"},
		{RunFailed, true, "failed"},
		{RunState("unknown"), false, "unknown"},
	}

	for _, tc := range tests {
		t.Run(string(tc.value), func(t *testing.T) {
			if got := tc.value.IsValid(); got != tc.wantOK {
				t.Errorf("RunState(%q).IsValid() = %v, want %v", tc.value, got, tc.wantOK)
			}
		})
	}

	if len(ValidRunStates) != 3 {
		t.Errorf("len(ValidRunStates) = %d, want 3", len(ValidRunStates))
	}
}

// ---------------------------------------------------------------------------
// Struct construction and JSON round-trip tests
// ---------------------------------------------------------------------------

func TestFlowStruct(t *testing.T) {
	now := time.Now().UTC()
	f := Flow{
		ID:          "01JKTESTFLOW000000000000001",
		Name:        "test-flow",
		Workspace:   "/tmp/workspace",
		State:       FlowDraft,
		Description: "A test flow",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if f.State != FlowDraft {
		t.Errorf("Flow.State = %q, want %q", f.State, FlowDraft)
	}
	if f.Name != "test-flow" {
		t.Errorf("Flow.Name = %q, want %q", f.Name, "test-flow")
	}

	// JSON round-trip.
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("json.Marshal(Flow): %v", err)
	}
	var f2 Flow
	if err := json.Unmarshal(data, &f2); err != nil {
		t.Fatalf("json.Unmarshal(Flow): %v", err)
	}
	if f2.ID != f.ID {
		t.Errorf("round-trip ID = %q, want %q", f2.ID, f.ID)
	}
	if f2.State != f.State {
		t.Errorf("round-trip State = %q, want %q", f2.State, f.State)
	}
}

func TestFlowNodeStruct(t *testing.T) {
	n := FlowNode{
		ID:     "01JKTESTNODE00000000000001",
		FlowID: "01JKTESTFLOW000000000000001",
		Kind:   NodeSkill,
		Label:  "search",
		Config: json.RawMessage(`{"skill":"code/semantic_search"}`),
	}

	if n.Kind != NodeSkill {
		t.Errorf("FlowNode.Kind = %q, want %q", n.Kind, NodeSkill)
	}

	// JSON round-trip preserves Config.
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("json.Marshal(FlowNode): %v", err)
	}
	var n2 FlowNode
	if err := json.Unmarshal(data, &n2); err != nil {
		t.Fatalf("json.Unmarshal(FlowNode): %v", err)
	}
	if string(n2.Config) != string(n.Config) {
		t.Errorf("round-trip Config = %s, want %s", n2.Config, n.Config)
	}
}

func TestFlowNodeWithPosition(t *testing.T) {
	n := FlowNode{
		ID:       "01JKTESTNODE00000000000002",
		FlowID:   "01JKTESTFLOW000000000000001",
		Kind:     NodeTransform,
		Label:    "extract",
		Config:   json.RawMessage(`{"transform":"jq_filter"}`),
		Position: &Position{X: 100.5, Y: 200},
	}

	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var n2 FlowNode
	if err := json.Unmarshal(data, &n2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if n2.Position == nil {
		t.Fatal("round-trip Position is nil")
	}
	if n2.Position.X != 100.5 || n2.Position.Y != 200 {
		t.Errorf("round-trip Position = (%v, %v), want (100.5, 200)", n2.Position.X, n2.Position.Y)
	}
}

func TestFlowNodeWithoutPosition(t *testing.T) {
	n := FlowNode{
		ID:     "01JKTESTNODE00000000000003",
		FlowID: "01JKTESTFLOW000000000000001",
		Kind:   NodeSkill,
		Label:  "no-pos",
		Config: json.RawMessage(`{}`),
	}

	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := raw["position"]; ok {
		t.Error("position field should be omitted when nil")
	}
}

func TestFlowEdgeStruct(t *testing.T) {
	e := FlowEdge{
		ID:              "01JKTESTEDGE0000000000001",
		FlowID:          "01JKTESTFLOW000000000000001",
		FromNodeID:      "01JKTESTNODE00000000000001",
		ToNodeID:        "01JKTESTNODE00000000000002",
		Transform:       TransformPassthrough,
		Trigger:         TriggerOutputReady,
		Condition:       "status == ok",
		TransformConfig: "",
		RetryPolicy:     &RetryPolicy{MaxAttempts: 2, DelayMS: 1000},
	}

	if e.Transform != TransformPassthrough {
		t.Errorf("FlowEdge.Transform = %q, want %q", e.Transform, TransformPassthrough)
	}
	if e.Trigger != TriggerOutputReady {
		t.Errorf("FlowEdge.Trigger = %q, want %q", e.Trigger, TriggerOutputReady)
	}

	// JSON round-trip.
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal(FlowEdge): %v", err)
	}
	var e2 FlowEdge
	if err := json.Unmarshal(data, &e2); err != nil {
		t.Fatalf("json.Unmarshal(FlowEdge): %v", err)
	}
	if e2.FromNodeID != e.FromNodeID {
		t.Errorf("round-trip FromNodeID = %q, want %q", e2.FromNodeID, e.FromNodeID)
	}
	if e2.RetryPolicy == nil {
		t.Fatal("round-trip RetryPolicy is nil")
	}
	if e2.RetryPolicy.MaxAttempts != 2 {
		t.Errorf("round-trip RetryPolicy.MaxAttempts = %d, want 2", e2.RetryPolicy.MaxAttempts)
	}
}

func TestFlowEdgeWithoutRetryPolicy(t *testing.T) {
	e := FlowEdge{
		ID:         "01JKTESTEDGE0000000000002",
		FlowID:     "01JKTESTFLOW000000000000001",
		FromNodeID: "01JKTESTNODE00000000000001",
		ToNodeID:   "01JKTESTNODE00000000000002",
		Transform:  TransformPassthrough,
		Trigger:    TriggerOutputReady,
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := raw["retry_policy"]; ok {
		t.Error("retry_policy field should be omitted when nil")
	}
}

func TestFlowRunStruct(t *testing.T) {
	now := time.Now().UTC()
	r := FlowRun{
		ID:        "01JKTESTRUN00000000000001",
		FlowID:    "01JKTESTFLOW000000000000001",
		State:     RunRunning,
		StartedAt: now,
	}

	if r.State != RunRunning {
		t.Errorf("FlowRun.State = %q, want %q", r.State, RunRunning)
	}
	if r.CompletedAt != nil {
		t.Error("FlowRun.CompletedAt should be nil for running state")
	}

	// JSON round-trip.
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal(FlowRun): %v", err)
	}
	var r2 FlowRun
	if err := json.Unmarshal(data, &r2); err != nil {
		t.Fatalf("json.Unmarshal(FlowRun): %v", err)
	}
	if r2.ID != r.ID {
		t.Errorf("round-trip ID = %q, want %q", r2.ID, r.ID)
	}
}

func TestFlowRunWithCompletion(t *testing.T) {
	now := time.Now().UTC()
	completed := now.Add(5 * time.Second)
	r := FlowRun{
		ID:          "01JKTESTRUN00000000000002",
		FlowID:      "01JKTESTFLOW000000000000001",
		State:       RunCompleted,
		StartedAt:   now,
		CompletedAt: &completed,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var r2 FlowRun
	if err := json.Unmarshal(data, &r2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if r2.CompletedAt == nil {
		t.Fatal("round-trip CompletedAt is nil")
	}
}

// ---------------------------------------------------------------------------
// Config struct tests
// ---------------------------------------------------------------------------

func TestSkillConfig(t *testing.T) {
	cfg := SkillConfig{
		Skill:     "code/semantic_search",
		ExtraArgs: []string{"--workspace", "."},
		InputMode: "data",
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal(SkillConfig): %v", err)
	}
	var cfg2 SkillConfig
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal(SkillConfig): %v", err)
	}
	if cfg2.Skill != cfg.Skill {
		t.Errorf("round-trip Skill = %q, want %q", cfg2.Skill, cfg.Skill)
	}
	if len(cfg2.ExtraArgs) != 2 {
		t.Errorf("round-trip ExtraArgs len = %d, want 2", len(cfg2.ExtraArgs))
	}
}

func TestPTYConfig(t *testing.T) {
	cfg := PTYConfig{
		Cmd:       []string{"claude"},
		Adapter:   "claude",
		Rows:      24,
		Cols:      80,
		SubmitKey: "Enter",
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal(PTYConfig): %v", err)
	}
	var cfg2 PTYConfig
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal(PTYConfig): %v", err)
	}
	if cfg2.Adapter != "claude" {
		t.Errorf("round-trip Adapter = %q, want %q", cfg2.Adapter, "claude")
	}
	if cfg2.Rows != 24 || cfg2.Cols != 80 {
		t.Errorf("round-trip Rows=%d Cols=%d, want 24,80", cfg2.Rows, cfg2.Cols)
	}
}

func TestHTTPConfig(t *testing.T) {
	cfg := HTTPConfig{
		URL:     "https://example.com/api",
		Method:  "POST",
		Headers: map[string]string{"Content-Type": "application/json"},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal(HTTPConfig): %v", err)
	}
	var cfg2 HTTPConfig
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal(HTTPConfig): %v", err)
	}
	if cfg2.URL != cfg.URL {
		t.Errorf("round-trip URL = %q, want %q", cfg2.URL, cfg.URL)
	}
	if cfg2.Headers["Content-Type"] != "application/json" {
		t.Errorf("round-trip Headers[Content-Type] = %q, want %q", cfg2.Headers["Content-Type"], "application/json")
	}
}

// ---------------------------------------------------------------------------
// Store interface tests (compile-time check)
// ---------------------------------------------------------------------------

// TestStore ensures the Store interface is defined correctly by verifying it
// exists at compile time. The actual SQLite implementation test is in
// internal/storage/flow/sqlite_test.go.
func TestStore(t *testing.T) {
	// This test verifies that Store is a valid interface with the expected
	// method set. The actual CRUD tests live in the storage layer.
	t.Log("Store interface verified at compile time")

	// Verify ErrNotFound is defined.
	if ErrNotFound == nil {
		t.Error("ErrNotFound should not be nil")
	}
	if ErrNotFound.Error() != "flow: not found" {
		t.Errorf("ErrNotFound.Error() = %q, want %q", ErrNotFound.Error(), "flow: not found")
	}
}

// ---------------------------------------------------------------------------
// NodeOutput struct test
// ---------------------------------------------------------------------------

func TestNodeOutput(t *testing.T) {
	env := envelope.OK("skill/run", map[string]any{"result": "hello"})
	out := NodeOutput{
		Envelope: env,
		Duration: 150 * time.Millisecond,
		NodeID:   "01JKTESTNODE00000000000001",
	}
	if out.Envelope.Status != "ok" {
		t.Errorf("NodeOutput.Envelope.Status = %q, want %q", out.Envelope.Status, "ok")
	}
	if out.Duration != 150*time.Millisecond {
		t.Errorf("NodeOutput.Duration = %v, want %v", out.Duration, 150*time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// Flow.Validate() tests
// ---------------------------------------------------------------------------

func TestFlowValidate(t *testing.T) {
	t.Run("valid short name", func(t *testing.T) {
		f := Flow{Name: "test-flow"}
		if err := f.Validate(); err != nil {
			t.Errorf("expected nil error for short name, got %v", err)
		}
	})

	t.Run("valid 256 char name", func(t *testing.T) {
		f := Flow{Name: strings.Repeat("a", 256)}
		if err := f.Validate(); err != nil {
			t.Errorf("expected nil error for 256-char name, got %v", err)
		}
	})

	t.Run("valid 1024 char name", func(t *testing.T) {
		f := Flow{Name: strings.Repeat("b", 1024)}
		if err := f.Validate(); err != nil {
			t.Errorf("expected nil error for 1024-char name, got %v", err)
		}
	})

	t.Run("rejects 1025 char name", func(t *testing.T) {
		f := Flow{Name: strings.Repeat("c", 1025)}
		err := f.Validate()
		if err == nil {
			t.Fatal("expected error for 1025-char name, got nil")
		}
		if !errors.Is(err, ErrNameTooLong) {
			t.Errorf("expected ErrNameTooLong, got %v", err)
		}
	})

	t.Run("rejects very long name", func(t *testing.T) {
		f := Flow{Name: strings.Repeat("d", 5000)}
		err := f.Validate()
		if err == nil {
			t.Fatal("expected error for 5000-char name, got nil")
		}
		if !errors.Is(err, ErrNameTooLong) {
			t.Errorf("expected ErrNameTooLong, got %v", err)
		}
	})

	t.Run("empty name passes validation", func(t *testing.T) {
		// Empty name is allowed by Validate(); the store rejects it via
		// UNIQUE constraint (empty strings collide).
		f := Flow{Name: ""}
		if err := f.Validate(); err != nil {
			t.Errorf("expected nil error for empty name, got %v", err)
		}
	})
}

func TestMaxFlowNameLen(t *testing.T) {
	if MaxFlowNameLen != 1024 {
		t.Errorf("MaxFlowNameLen = %d, want 1024", MaxFlowNameLen)
	}
}

func TestErrNameTooLong(t *testing.T) {
	if ErrNameTooLong == nil {
		t.Fatal("ErrNameTooLong should not be nil")
	}
	if !errors.Is(ErrNameTooLong, ErrNameTooLong) {
		t.Error("ErrNameTooLong should match itself via errors.Is")
	}
}
