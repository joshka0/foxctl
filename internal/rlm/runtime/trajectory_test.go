package runtime

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRecorderEventOrdering(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.February, 3, 10, 11, 12, 0, time.UTC)
	tick := 0
	recorder := NewRecorder(WithRecorderNow(func() time.Time {
		at := base.Add(time.Duration(tick) * time.Second)
		tick++
		return at
	}))

	first := recorder.RecordParentLLMCall(ParentLLMCallEvent{
		CallID:       "parent-1",
		Model:        "test-model",
		PromptTokens: 50,
	})
	second := recorder.RecordREPLCall(REPLCallEvent{
		CallID: "repl-1",
		Input:  "x = 1",
	})
	third := recorder.RecordSubcallStart(SubcallEvent{
		CallID: "sub-1",
		Name:   "lookup",
		Depth:  1,
	})

	if first.Seq != 1 || second.Seq != 2 || third.Seq != 3 {
		t.Fatalf("unexpected sequence numbers: got %d, %d, %d", first.Seq, second.Seq, third.Seq)
	}

	events := recorder.Events()
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	if events[0].Type != EventTypeParentLLMCall {
		t.Fatalf("events[0].Type = %q, want %q", events[0].Type, EventTypeParentLLMCall)
	}
	if events[1].Type != EventTypeREPLCall {
		t.Fatalf("events[1].Type = %q, want %q", events[1].Type, EventTypeREPLCall)
	}
	if events[2].Type != EventTypeSubcallStart {
		t.Fatalf("events[2].Type = %q, want %q", events[2].Type, EventTypeSubcallStart)
	}
	if !events[1].At.After(events[0].At) || !events[2].At.After(events[1].At) {
		t.Fatalf("event timestamps are not strictly increasing: %+v", events)
	}
}

func TestRecorderHookReceivesStoredEventCopy(t *testing.T) {
	t.Parallel()

	var hooked []Event
	recorder := NewRecorder(WithRecorderHook(func(event Event) {
		hooked = append(hooked, event)
		if event.REPLCall != nil {
			event.REPLCall.Input = "mutated"
		}
	}))

	recorded := recorder.RecordREPLCall(REPLCallEvent{CallID: "repl-1", Input: "x = 1"})
	if len(hooked) != 1 {
		t.Fatalf("hooked events = %d, want 1", len(hooked))
	}
	if hooked[0].Seq != recorded.Seq {
		t.Fatalf("hook seq = %d, want %d", hooked[0].Seq, recorded.Seq)
	}
	events := recorder.Events()
	if events[0].REPLCall.Input != "x = 1" {
		t.Fatalf("stored event mutated through hook: %+v", events[0].REPLCall)
	}
}

func TestRecorderNodeAndWaitEventsAreDefensivelyCloned(t *testing.T) {
	t.Parallel()

	var hooked []Event
	recorder := NewRecorder(WithRecorderHook(func(event Event) {
		hooked = append(hooked, event)
		if event.Node != nil {
			event.Node.Message = "hook-mutated"
		}
		if event.Wait != nil && len(event.Wait.ChildIDs) > 0 {
			event.Wait.ChildIDs[0] = "hook-mutated"
		}
	}))

	nodeInput := NodeEvent{
		RunID:           "run-1",
		NodeID:          "node-1",
		ParentNodeID:    "root",
		Depth:           1,
		Status:          NodeStatusQueued,
		OutputNamespace: "runs/run-1/nodes/node-1",
		Message:         "queued",
	}
	waitInput := WaitEvent{
		RunID:        "run-1",
		ParentNodeID: "node-1",
		ChildIDs:     []string{"child-1", "child-2"},
		Completed:    1,
		Failed:       0,
		Pending:      1,
		MinComplete:  1,
		TimeoutMS:    2500,
	}

	recordedNode := recorder.RecordNodeQueued(nodeInput)
	recordedWait := recorder.RecordNodeWaitStarted(waitInput)

	nodeInput.Message = "caller-mutated"
	waitInput.ChildIDs[0] = "caller-mutated"
	recordedNode.Node.Message = "result-mutated"
	recordedWait.Wait.ChildIDs[0] = "result-mutated"

	if len(hooked) != 2 {
		t.Fatalf("hooked events = %d, want 2", len(hooked))
	}
	events := recorder.Events()
	if len(events) != 2 {
		t.Fatalf("recorded events = %d, want 2", len(events))
	}
	if events[0].Type != EventTypeNodeQueued {
		t.Fatalf("events[0].Type = %q, want %q", events[0].Type, EventTypeNodeQueued)
	}
	if got := events[0].Node.Message; got != "queued" {
		t.Fatalf("events[0].Node.Message = %q, want queued", got)
	}
	if events[1].Type != EventTypeNodeWaitStarted {
		t.Fatalf("events[1].Type = %q, want %q", events[1].Type, EventTypeNodeWaitStarted)
	}
	if got := events[1].Wait.ChildIDs[0]; got != "child-1" {
		t.Fatalf("events[1].Wait.ChildIDs[0] = %q, want child-1", got)
	}

	events[1].Wait.ChildIDs[1] = "snapshot-mutated"
	fresh := recorder.Events()
	if got := fresh[1].Wait.ChildIDs[1]; got != "child-2" {
		t.Fatalf("fresh wait snapshot mutated = %q, want child-2", got)
	}
}

func TestRecorderJSONMarshalStable(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)
	tick := 0
	recorder := NewRecorder(WithRecorderNow(func() time.Time {
		at := base.Add(time.Duration(tick) * time.Millisecond)
		tick++
		return at
	}))

	recorder.RecordParentLLMCall(ParentLLMCallEvent{
		CallID:           "parent-1",
		Model:            "model-x",
		PromptTokens:     31,
		CompletionTokens: 12,
	})
	recorder.RecordREPLCall(REPLCallEvent{
		CallID: "repl-1",
		Input:  "print(1)",
	})
	recorder.RecordREPLResult(REPLResultEvent{
		CallID:     "repl-1",
		Output:     "1",
		Success:    true,
		DurationMS: 5,
	})
	recorder.RecordSubcallStart(SubcallEvent{
		CallID: "sub-1",
		Name:   "search",
		Depth:  1,
	})
	recorder.RecordSubcallEnd(SubcallEvent{
		CallID: "sub-1",
		Name:   "search",
		Depth:  1,
	})
	recorder.RecordNodeQueued(NodeEvent{
		RunID:           "run-1",
		NodeID:          "node-1",
		ParentNodeID:    "root",
		Depth:           1,
		Status:          NodeStatusQueued,
		OutputNamespace: "runs/run-1/nodes/node-1",
		Message:         "queued",
	})
	recorder.RecordNodeWaitStarted(WaitEvent{
		RunID:        "run-1",
		ParentNodeID: "node-1",
		ChildIDs:     []string{"node-2", "node-3"},
		Completed:    1,
		Failed:       0,
		Pending:      1,
		MinComplete:  2,
		TimeoutMS:    1500,
	})
	recorder.RecordBudgetEvent(BudgetEvent{
		Limit:   LimitIterations,
		Used:    3,
		Max:     4,
		Message: "near limit",
	})
	recorder.RecordContractEvent(ContractEvent{
		Boundary:     "tool_input",
		Tool:         RLMQueryToolName,
		Status:       "repaired",
		IssueKind:    "numeric_string",
		RepairRule:   "parse_int_string",
		RevalidateOK: true,
	})
	recorder.RecordFinalAnswer(FinalAnswerEvent{
		Text:   "done",
		Tokens: 24,
	})
	recorder.RecordError(RuntimeErrorEvent{
		Code:    "test_error",
		Message: "synthetic failure",
	})

	first, err := json.Marshal(recorder.Events())
	if err != nil {
		t.Fatalf("json.Marshal(events) error = %v", err)
	}
	second, err := json.Marshal(recorder.Events())
	if err != nil {
		t.Fatalf("json.Marshal(events) second error = %v", err)
	}

	if string(first) != string(second) {
		t.Fatalf("event JSON not stable:\nfirst:  %s\nsecond: %s", first, second)
	}
}
