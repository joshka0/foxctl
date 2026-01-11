package hooks

import (
	"encoding/json"
	"testing"
)

func TestMerge_EmptyOutputs(t *testing.T) {
	result := Merge(nil)
	if result.Decision != DecisionNone {
		t.Errorf("expected none decision, got %s", result.Decision)
	}

	result = Merge([]Output{})
	if result.Decision != DecisionNone {
		t.Errorf("expected none decision, got %s", result.Decision)
	}
}

func TestMerge_SingleOutput(t *testing.T) {
	out := NewBlock("test reason")
	result := Merge([]Output{out})

	if result.Decision != DecisionBlock {
		t.Errorf("expected block decision, got %s", result.Decision)
	}
	if result.Reason != "test reason" {
		t.Errorf("expected reason 'test reason', got %s", result.Reason)
	}
}

func TestMerge_BlockWins(t *testing.T) {
	tests := []struct {
		name      string
		outputs   []Output
		wantBlock bool
	}{
		{
			name: "all approve",
			outputs: []Output{
				NewApprove("r1", nil),
				NewApprove("r2", nil),
			},
			wantBlock: false,
		},
		{
			name: "first blocks",
			outputs: []Output{
				NewBlock("blocked"),
				NewApprove("r2", nil),
			},
			wantBlock: true,
		},
		{
			name: "last blocks",
			outputs: []Output{
				NewApprove("r1", nil),
				NewBlock("blocked"),
			},
			wantBlock: true,
		},
		{
			name: "middle blocks",
			outputs: []Output{
				NewApprove("r1", nil),
				NewBlock("blocked"),
				NewApprove("r3", nil),
			},
			wantBlock: true,
		},
		{
			name: "none with approve",
			outputs: []Output{
				NewNone(),
				NewApprove("r1", nil),
			},
			wantBlock: false,
		},
		{
			name: "none with block",
			outputs: []Output{
				NewNone(),
				NewBlock("blocked"),
			},
			wantBlock: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Merge(tt.outputs)
			if result.Decision.IsBlocking() != tt.wantBlock {
				t.Errorf("expected block=%v, got decision=%s", tt.wantBlock, result.Decision)
			}
		})
	}
}

func TestMerge_LastWins_ToolInput(t *testing.T) {
	input1 := json.RawMessage(`{"key": "value1"}`)
	input2 := json.RawMessage(`{"key": "value2"}`)
	input3 := json.RawMessage(`{"key": "value3"}`)

	tests := []struct {
		name    string
		outputs []Output
		want    string
	}{
		{
			name: "single input",
			outputs: []Output{
				{Decision: DecisionApprove, UpdatedToolInput: input1},
			},
			want: `{"key": "value1"}`,
		},
		{
			name: "last wins",
			outputs: []Output{
				{Decision: DecisionApprove, UpdatedToolInput: input1},
				{Decision: DecisionApprove, UpdatedToolInput: input2},
			},
			want: `{"key": "value2"}`,
		},
		{
			name: "skip empty",
			outputs: []Output{
				{Decision: DecisionApprove, UpdatedToolInput: input1},
				{Decision: DecisionApprove},
				{Decision: DecisionApprove, UpdatedToolInput: input3},
			},
			want: `{"key": "value3"}`,
		},
		{
			name: "first only",
			outputs: []Output{
				{Decision: DecisionApprove, UpdatedToolInput: input1},
				{Decision: DecisionApprove},
			},
			want: `{"key": "value1"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Merge(tt.outputs)
			if string(result.UpdatedToolInput) != tt.want {
				t.Errorf("expected %s, got %s", tt.want, string(result.UpdatedToolInput))
			}
		})
	}
}

func TestMerge_LastWins_AssistantText(t *testing.T) {
	tests := []struct {
		name    string
		outputs []Output
		want    string
	}{
		{
			name: "single text",
			outputs: []Output{
				{Decision: DecisionApprove, UpdatedAssistantText: "text1"},
			},
			want: "text1",
		},
		{
			name: "last wins",
			outputs: []Output{
				{Decision: DecisionApprove, UpdatedAssistantText: "text1"},
				{Decision: DecisionApprove, UpdatedAssistantText: "text2"},
			},
			want: "text2",
		},
		{
			name: "skip empty",
			outputs: []Output{
				{Decision: DecisionApprove, UpdatedAssistantText: "text1"},
				{Decision: DecisionApprove},
				{Decision: DecisionApprove, UpdatedAssistantText: "text3"},
			},
			want: "text3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Merge(tt.outputs)
			if result.UpdatedAssistantText != tt.want {
				t.Errorf("expected %s, got %s", tt.want, result.UpdatedAssistantText)
			}
		})
	}
}

func TestMerge_ActionsAppended(t *testing.T) {
	a1 := RunSkillAction("skill1", nil)
	a2 := InjectContextAction("context1", 0)
	a3 := RunSkillAction("skill2", nil)

	tests := []struct {
		name        string
		outputs     []Output
		wantActions int
	}{
		{
			name: "single hook",
			outputs: []Output{
				{Decision: DecisionApprove, Actions: []Action{a1}},
			},
			wantActions: 1,
		},
		{
			name: "multiple hooks",
			outputs: []Output{
				{Decision: DecisionApprove, Actions: []Action{a1}},
				{Decision: DecisionApprove, Actions: []Action{a2, a3}},
			},
			wantActions: 3,
		},
		{
			name: "empty actions",
			outputs: []Output{
				{Decision: DecisionApprove, Actions: []Action{a1}},
				{Decision: DecisionApprove},
				{Decision: DecisionApprove, Actions: []Action{a2}},
			},
			wantActions: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Merge(tt.outputs)
			if len(result.Actions) != tt.wantActions {
				t.Errorf("expected %d actions, got %d", tt.wantActions, len(result.Actions))
			}
		})
	}
}

func TestMerge_ContextJoined(t *testing.T) {
	tests := []struct {
		name        string
		outputs     []Output
		wantActions int
		wantContext string
	}{
		{
			name: "single context",
			outputs: []Output{
				{Decision: DecisionApprove, Context: "ctx1"},
			},
			wantActions: 0,
			wantContext: "ctx1",
		},
		{
			name: "multiple contexts",
			outputs: []Output{
				{Decision: DecisionApprove, Context: "ctx1"},
				{Decision: DecisionApprove, Context: "ctx2"},
			},
			wantActions: 0,
			wantContext: "ctx1\n\nctx2",
		},
		{
			name: "context and actions",
			outputs: []Output{
				{Decision: DecisionApprove, Context: "ctx1", Actions: []Action{RunSkillAction("s1", nil)}},
			},
			wantActions: 1,
			wantContext: "ctx1",
		},
		{
			name: "empty context ignored",
			outputs: []Output{
				{Decision: DecisionApprove, Context: "ctx1"},
				{Decision: DecisionApprove, Context: ""},
			},
			wantActions: 0,
			wantContext: "ctx1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Merge(tt.outputs)
			if len(result.Actions) != tt.wantActions {
				t.Errorf("expected %d actions, got %d", tt.wantActions, len(result.Actions))
			}
			if result.Context != tt.wantContext {
				t.Errorf("expected context %q, got %q", tt.wantContext, result.Context)
			}
		})
	}
}

func TestMerge_ReasonForDecision(t *testing.T) {
	tests := []struct {
		name    string
		outputs []Output
		want    string
	}{
		{
			name: "first approve reason wins",
			outputs: []Output{
				{Decision: DecisionApprove, Reason: "r1"},
				{Decision: DecisionApprove, Reason: "r2"},
			},
			want: "r1",
		},
		{
			name: "block reason beats approve",
			outputs: []Output{
				{Decision: DecisionApprove, Reason: "ok"},
				{Decision: DecisionBlock, Reason: "blocked"},
				{Decision: DecisionBlock, Reason: "blocked-2"},
			},
			want: "blocked",
		},
		{
			name: "first block reason skips empty",
			outputs: []Output{
				{Decision: DecisionBlock, Reason: ""},
				{Decision: DecisionBlock, Reason: "blocked"},
			},
			want: "blocked",
		},
		{
			name: "none decision has empty reason",
			outputs: []Output{
				{Decision: DecisionNone},
				{Decision: DecisionNone},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Merge(tt.outputs)
			if result.Reason != tt.want {
				t.Errorf("expected %q, got %q", tt.want, result.Reason)
			}
		})
	}
}

func TestMerge_MetaShallowMerge(t *testing.T) {
	tests := []struct {
		name    string
		outputs []Output
		want    map[string]any
	}{
		{
			name: "single meta",
			outputs: []Output{
				{Decision: DecisionApprove, Meta: map[string]any{"k1": "v1"}},
			},
			want: map[string]any{"k1": "v1"},
		},
		{
			name: "merge different keys",
			outputs: []Output{
				{Decision: DecisionApprove, Meta: map[string]any{"k1": "v1"}},
				{Decision: DecisionApprove, Meta: map[string]any{"k2": "v2"}},
			},
			want: map[string]any{"k1": "v1", "k2": "v2"},
		},
		{
			name: "last wins same key",
			outputs: []Output{
				{Decision: DecisionApprove, Meta: map[string]any{"k1": "v1"}},
				{Decision: DecisionApprove, Meta: map[string]any{"k1": "v2"}},
			},
			want: map[string]any{"k1": "v2"},
		},
		{
			name: "nil meta",
			outputs: []Output{
				{Decision: DecisionApprove},
				{Decision: DecisionApprove},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Merge(tt.outputs)
			if tt.want == nil {
				if result.Meta != nil {
					t.Errorf("expected nil meta, got %v", result.Meta)
				}
				return
			}
			for k, v := range tt.want {
				if result.Meta[k] != v {
					t.Errorf("expected meta[%s]=%v, got %v", k, v, result.Meta[k])
				}
			}
		})
	}
}

func TestMergeWithDetails(t *testing.T) {
	outputs := []Output{
		{Decision: DecisionApprove, Reason: "r1", Actions: []Action{RunSkillAction("s1", nil)}},
		{Decision: DecisionBlock, Reason: "blocked"},
		{Decision: DecisionApprove, Reason: "r3", Actions: []Action{RunSkillAction("s2", nil), RunSkillAction("s3", nil)}},
	}

	result := MergeWithDetails(outputs)

	if result.BlockedBy != 1 {
		t.Errorf("expected blocked by index 1, got %d", result.BlockedBy)
	}

	if result.ActionCounts[0] != 1 || result.ActionCounts[1] != 0 || result.ActionCounts[2] != 2 {
		t.Errorf("unexpected action counts: %v", result.ActionCounts)
	}

	if len(result.ReasonSources) != 1 || result.ReasonSources[0] != 1 {
		t.Errorf("expected reason source [1], got %v", result.ReasonSources)
	}
}

func TestDeduplicateActions(t *testing.T) {
	a1 := RunSkillAction("skill1", nil)
	a2 := RunSkillAction("skill1", nil) // duplicate
	a3 := RunSkillAction("skill2", nil)
	a4 := InjectContextAction("ctx1", 0)
	a5 := InjectContextAction("ctx1", 0) // duplicate

	actions := []Action{a1, a2, a3, a4, a5}
	result := DeduplicateActions(actions)

	if len(result) != 3 {
		t.Errorf("expected 3 unique actions, got %d", len(result))
	}
}

func TestSortActionsByPriority(t *testing.T) {
	actions := []Action{
		InjectContextAction("low", 1),
		RunSkillAction("skill1", nil),
		InjectContextAction("high", 10),
		InjectContextAction("medium", 5),
		RunSkillAction("skill2", nil),
	}

	result := SortActionsByPriority(actions)

	// First 3 should be inject_context in priority order
	if result[0].Type != ActionInjectContext || result[0].Priority != 10 {
		t.Errorf("expected high priority first, got %v", result[0])
	}
	if result[1].Type != ActionInjectContext || result[1].Priority != 5 {
		t.Errorf("expected medium priority second, got %v", result[1])
	}
	if result[2].Type != ActionInjectContext || result[2].Priority != 1 {
		t.Errorf("expected low priority third, got %v", result[2])
	}
}

func TestOutput_Helpers(t *testing.T) {
	// Test builder pattern
	out := NewNone().
		WithContext("ctx").
		WithActions(RunSkillAction("s1", nil)).
		WithMeta(map[string]any{"k": "v"})

	if out.Decision != DecisionNone {
		t.Errorf("expected none decision")
	}
	if out.Context != "ctx" {
		t.Errorf("expected context 'ctx'")
	}
	if len(out.Actions) != 1 {
		t.Errorf("expected 1 action")
	}
	if out.Meta["k"] != "v" {
		t.Errorf("expected meta[k]=v")
	}
}

func TestEvent_IsValid(t *testing.T) {
	tests := []struct {
		event Event
		valid bool
	}{
		{EventSessionStart, true},
		{EventPreToolUse, true},
		{EventPostToolUse, true},
		{EventStopRequested, true},
		{Event("InvalidEvent"), false},
		{Event(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.event), func(t *testing.T) {
			if tt.event.IsValid() != tt.valid {
				t.Errorf("expected valid=%v for event %s", tt.valid, tt.event)
			}
		})
	}
}

func TestDecision_IsBlocking(t *testing.T) {
	tests := []struct {
		decision Decision
		blocking bool
	}{
		{DecisionNone, false},
		{DecisionApprove, false},
		{DecisionBlock, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.decision), func(t *testing.T) {
			if tt.decision.IsBlocking() != tt.blocking {
				t.Errorf("expected blocking=%v for decision %s", tt.blocking, tt.decision)
			}
		})
	}
}

func TestActionType_IsValid(t *testing.T) {
	tests := []struct {
		action ActionType
		valid  bool
	}{
		{ActionRunSkill, true},
		{ActionInjectContext, true},
		{ActionSendMailbox, true},
		{ActionBBPost, true},
		{ActionBBClaim, true},
		{ActionType("invalid"), false},
		{ActionType(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			if tt.action.IsValid() != tt.valid {
				t.Errorf("expected valid=%v for action %s", tt.valid, tt.action)
			}
		})
	}
}
