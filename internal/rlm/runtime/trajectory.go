package runtime

import (
	"sync"
	"time"
)

// EventType identifies one trajectory event kind.
type EventType string

const (
	EventTypeParentLLMCall     EventType = "parent_llm_call"
	EventTypeREPLCall          EventType = "repl_call"
	EventTypeREPLResult        EventType = "repl_result"
	EventTypeSubcallStart      EventType = "subcall_start"
	EventTypeSubcallEnd        EventType = "subcall_end"
	EventTypeNodeQueued        EventType = "node_queued"
	EventTypeNodeStarted       EventType = "node_started"
	EventTypeNodeWaitStarted   EventType = "node_wait_started"
	EventTypeNodeWaitCompleted EventType = "node_wait_completed"
	EventTypeNodeCompleted     EventType = "node_completed"
	EventTypeNodeFailed        EventType = "node_failed"
	EventTypeNodeCanceled      EventType = "node_canceled"
	EventTypeBudget            EventType = "budget"
	EventTypeBraid             EventType = "braid"
	EventTypeFinalAnswer       EventType = "final_answer"
	EventTypeError             EventType = "error"
)

// Event is one typed trajectory record.
type Event struct {
	Seq  int64     `json:"seq"`
	At   time.Time `json:"at,omitempty"`
	Type EventType `json:"type"`

	ParentLLMCall *ParentLLMCallEvent `json:"parent_llm_call,omitempty"`
	REPLCall      *REPLCallEvent      `json:"repl_call,omitempty"`
	REPLResult    *REPLResultEvent    `json:"repl_result,omitempty"`
	Subcall       *SubcallEvent       `json:"subcall,omitempty"`
	Node          *NodeEvent          `json:"node,omitempty"`
	Wait          *WaitEvent          `json:"wait,omitempty"`
	Budget        *BudgetEvent        `json:"budget,omitempty"`
	Braid         *BraidEvent         `json:"braid,omitempty"`
	FinalAnswer   *FinalAnswerEvent   `json:"final_answer,omitempty"`
	RuntimeError  *RuntimeErrorEvent  `json:"error,omitempty"`
}

// ParentLLMCallEvent records one parent model invocation/result.
type ParentLLMCallEvent struct {
	CallID           string   `json:"call_id,omitempty"`
	Model            string   `json:"model,omitempty"`
	PromptTokens     int      `json:"prompt_tokens,omitempty"`
	CompletionTokens int      `json:"completion_tokens,omitempty"`
	FinishReason     string   `json:"finish_reason,omitempty"`
	ToolCalls        int      `json:"tool_calls,omitempty"`
	ToolNames        []string `json:"tool_names,omitempty"`
}

// REPLCallEvent records one REPL invocation.
type REPLCallEvent struct {
	CallID string `json:"call_id,omitempty"`
	Input  string `json:"input,omitempty"`
}

// REPLResultEvent records one REPL result.
type REPLResultEvent struct {
	CallID     string `json:"call_id,omitempty"`
	Output     string `json:"output,omitempty"`
	Success    bool   `json:"success"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// SubcallEvent records subcall boundaries.
type SubcallEvent struct {
	CallID          string `json:"call_id,omitempty"`
	Name            string `json:"name,omitempty"`
	Depth           int    `json:"depth,omitempty"`
	AgentID         string `json:"agent_id,omitempty"`
	ParentAgentID   string `json:"parent_agent_id,omitempty"`
	OutputNamespace string `json:"output_namespace,omitempty"`
}

// NodeEvent records lifecycle updates for one node.
type NodeEvent struct {
	RunID                   string     `json:"run_id,omitempty"`
	NodeID                  string     `json:"node_id,omitempty"`
	ParentNodeID            string     `json:"parent_node_id,omitempty"`
	Depth                   int        `json:"depth,omitempty"`
	Status                  NodeStatus `json:"status,omitempty"`
	OutputNamespace         string     `json:"output_namespace,omitempty"`
	Message                 string     `json:"message,omitempty"`
	RequiredSubcalls        int        `json:"required_subcalls,omitempty"`
	RequiredSubcallAttempts int        `json:"required_subcall_attempts,omitempty"`
	RecursiveSubcallsUsed   int        `json:"recursive_subcalls_used,omitempty"`
}

// WaitEvent records aggregate waiting state for one parent node.
type WaitEvent struct {
	RunID        string   `json:"run_id,omitempty"`
	ParentNodeID string   `json:"parent_node_id,omitempty"`
	ChildIDs     []string `json:"child_ids,omitempty"`
	Completed    int      `json:"completed,omitempty"`
	Failed       int      `json:"failed,omitempty"`
	Pending      int      `json:"pending,omitempty"`
	MinComplete  int      `json:"min_complete,omitempty"`
	TimeoutMS    int64    `json:"timeout_ms,omitempty"`
}

// BudgetEvent records budget-related progress or failures.
type BudgetEvent struct {
	Limit   BudgetLimit `json:"limit,omitempty"`
	Used    int         `json:"used,omitempty"`
	Max     int         `json:"max,omitempty"`
	Message string      `json:"message,omitempty"`
}

// BraidEvent records runtime-controlled BRAID graph execution progress.
type BraidEvent struct {
	Phase     string `json:"phase,omitempty"`
	Status    string `json:"status,omitempty"`
	Wave      int    `json:"wave,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	FinalNode string `json:"final_node,omitempty"`
	NodeCount int    `json:"node_count,omitempty"`
	Message   string `json:"message,omitempty"`
}

// FinalAnswerEvent records the terminal answer payload.
type FinalAnswerEvent struct {
	Text   string `json:"text,omitempty"`
	Tokens int    `json:"tokens,omitempty"`
}

// RuntimeErrorEvent records terminal or intermediate errors.
type RuntimeErrorEvent struct {
	Code             string   `json:"code,omitempty"`
	Message          string   `json:"message"`
	RawChars         int      `json:"raw_chars,omitempty"`
	SanitizedChars   int      `json:"sanitized_chars,omitempty"`
	RawExcerpt       string   `json:"raw_excerpt,omitempty"`
	SanitizedExcerpt string   `json:"sanitized_excerpt,omitempty"`
	Artifacts        []string `json:"artifacts,omitempty"`
}

// RecorderOption configures a Recorder.
type RecorderOption func(*Recorder)

// WithRecorderNow injects a deterministic clock for tests.
func WithRecorderNow(now func() time.Time) RecorderOption {
	return func(r *Recorder) {
		if now != nil {
			r.now = now
		}
	}
}

// WithRecorderHook registers a best-effort callback invoked for every stored
// event. The callback receives a defensive copy after sequencing/timestamping.
func WithRecorderHook(hook func(Event)) RecorderOption {
	return func(r *Recorder) {
		r.hook = hook
	}
}

// Recorder stores ordered trajectory events in memory.
type Recorder struct {
	mu      sync.Mutex
	now     func() time.Time
	nextSeq int64
	events  []Event
	hook    func(Event)
}

// NewRecorder creates a new in-memory event recorder.
func NewRecorder(options ...RecorderOption) *Recorder {
	r := &Recorder{
		now: time.Now,
	}
	for _, option := range options {
		option(r)
	}
	return r
}

// Record appends one event to the trajectory.
func (r *Recorder) Record(event Event) Event {
	r.mu.Lock()

	stored := cloneEvent(event)
	if stored.Seq <= 0 {
		r.nextSeq++
		stored.Seq = r.nextSeq
	} else if stored.Seq > r.nextSeq {
		r.nextSeq = stored.Seq
	}
	if stored.At.IsZero() {
		stored.At = r.now().UTC()
	}

	r.events = append(r.events, stored)
	out := cloneEvent(stored)
	hook := r.hook
	r.mu.Unlock()

	if hook != nil {
		hook(cloneEvent(out))
	}
	return out
}

// RecordParentLLMCall appends one parent-LLM event.
func (r *Recorder) RecordParentLLMCall(event ParentLLMCallEvent) Event {
	payload := event
	return r.Record(Event{
		Type:          EventTypeParentLLMCall,
		ParentLLMCall: &payload,
	})
}

// RecordREPLCall appends one REPL-call event.
func (r *Recorder) RecordREPLCall(event REPLCallEvent) Event {
	payload := event
	return r.Record(Event{
		Type:     EventTypeREPLCall,
		REPLCall: &payload,
	})
}

// RecordREPLResult appends one REPL-result event.
func (r *Recorder) RecordREPLResult(event REPLResultEvent) Event {
	payload := event
	return r.Record(Event{
		Type:       EventTypeREPLResult,
		REPLResult: &payload,
	})
}

// RecordSubcallStart appends one subcall-start event.
func (r *Recorder) RecordSubcallStart(event SubcallEvent) Event {
	payload := event
	return r.Record(Event{
		Type:    EventTypeSubcallStart,
		Subcall: &payload,
	})
}

// RecordSubcallEnd appends one subcall-end event.
func (r *Recorder) RecordSubcallEnd(event SubcallEvent) Event {
	payload := event
	return r.Record(Event{
		Type:    EventTypeSubcallEnd,
		Subcall: &payload,
	})
}

// RecordNodeQueued appends one node-queued event.
func (r *Recorder) RecordNodeQueued(event NodeEvent) Event {
	return r.recordNodeEvent(EventTypeNodeQueued, event)
}

// RecordNodeStarted appends one node-started event.
func (r *Recorder) RecordNodeStarted(event NodeEvent) Event {
	return r.recordNodeEvent(EventTypeNodeStarted, event)
}

// RecordNodeWaitStarted appends one node-wait-started event.
func (r *Recorder) RecordNodeWaitStarted(event WaitEvent) Event {
	return r.recordWaitEvent(EventTypeNodeWaitStarted, event)
}

// RecordNodeWaitCompleted appends one node-wait-completed event.
func (r *Recorder) RecordNodeWaitCompleted(event WaitEvent) Event {
	return r.recordWaitEvent(EventTypeNodeWaitCompleted, event)
}

// RecordNodeCompleted appends one node-completed event.
func (r *Recorder) RecordNodeCompleted(event NodeEvent) Event {
	return r.recordNodeEvent(EventTypeNodeCompleted, event)
}

// RecordNodeFailed appends one node-failed event.
func (r *Recorder) RecordNodeFailed(event NodeEvent) Event {
	return r.recordNodeEvent(EventTypeNodeFailed, event)
}

// RecordNodeCanceled appends one node-canceled event.
func (r *Recorder) RecordNodeCanceled(event NodeEvent) Event {
	return r.recordNodeEvent(EventTypeNodeCanceled, event)
}

// RecordBudgetEvent appends one budget event.
func (r *Recorder) RecordBudgetEvent(event BudgetEvent) Event {
	payload := event
	return r.Record(Event{
		Type:   EventTypeBudget,
		Budget: &payload,
	})
}

// RecordBraidEvent appends one BRAID execution event.
func (r *Recorder) RecordBraidEvent(event BraidEvent) Event {
	payload := event
	return r.Record(Event{
		Type:  EventTypeBraid,
		Braid: &payload,
	})
}

// RecordFinalAnswer appends one final-answer event.
func (r *Recorder) RecordFinalAnswer(event FinalAnswerEvent) Event {
	payload := event
	return r.Record(Event{
		Type:        EventTypeFinalAnswer,
		FinalAnswer: &payload,
	})
}

// RecordError appends one runtime-error event.
func (r *Recorder) RecordError(event RuntimeErrorEvent) Event {
	payload := event
	return r.Record(Event{
		Type:         EventTypeError,
		RuntimeError: &payload,
	})
}

func (r *Recorder) recordNodeEvent(eventType EventType, event NodeEvent) Event {
	payload := event
	return r.Record(Event{
		Type: eventType,
		Node: &payload,
	})
}

func (r *Recorder) recordWaitEvent(eventType EventType, event WaitEvent) Event {
	payload := event
	return r.Record(Event{
		Type: eventType,
		Wait: &payload,
	})
}

// Events returns a snapshot copy of all recorded events.
func (r *Recorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Event, len(r.events))
	for i, event := range r.events {
		out[i] = cloneEvent(event)
	}
	return out
}

// Reset clears all events and sequence state.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = nil
	r.nextSeq = 0
}

func cloneEvent(event Event) Event {
	cloned := event
	if event.ParentLLMCall != nil {
		parent := *event.ParentLLMCall
		if len(event.ParentLLMCall.ToolNames) > 0 {
			parent.ToolNames = append([]string(nil), event.ParentLLMCall.ToolNames...)
		}
		cloned.ParentLLMCall = &parent
	}
	if event.REPLCall != nil {
		replCall := *event.REPLCall
		cloned.REPLCall = &replCall
	}
	if event.REPLResult != nil {
		replResult := *event.REPLResult
		cloned.REPLResult = &replResult
	}
	if event.Subcall != nil {
		subcall := *event.Subcall
		cloned.Subcall = &subcall
	}
	if event.Node != nil {
		node := *event.Node
		cloned.Node = &node
	}
	if event.Wait != nil {
		wait := *event.Wait
		if len(event.Wait.ChildIDs) > 0 {
			wait.ChildIDs = append([]string(nil), event.Wait.ChildIDs...)
		}
		cloned.Wait = &wait
	}
	if event.Budget != nil {
		budget := *event.Budget
		cloned.Budget = &budget
	}
	if event.Braid != nil {
		braid := *event.Braid
		cloned.Braid = &braid
	}
	if event.FinalAnswer != nil {
		answer := *event.FinalAnswer
		cloned.FinalAnswer = &answer
	}
	if event.RuntimeError != nil {
		runtimeError := *event.RuntimeError
		cloned.RuntimeError = &runtimeError
	}
	return cloned
}
