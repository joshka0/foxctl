package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

const (
	// RLMWaitToolName waits on one parent node's direct children.
	RLMWaitToolName = "rlm_wait"
	// RLMResultToolName fetches one node/result snapshot from the store.
	RLMResultToolName = "rlm_result"
)

// RLMToolsConfig configures scheduler-backed RLM tool execution.
type RLMToolsConfig struct {
	Scheduler            *Scheduler
	Store                NodeStore
	RunID                string
	ParentNodeID         string // Default parent/current node ID.
	DefaultQueryPrompt   string
	SummaryMaxChars      int
	RequiredSubcallRules []RequiredSubcallRule
	Recorder             *Recorder
}

// RLMToolsExecutor exposes scheduler-backed recursive RLM tools.
type RLMToolsExecutor struct {
	scheduler            *Scheduler
	store                NodeStore
	runID                string
	parentNodeID         string
	defaultQueryPrompt   string
	summaryMaxChars      int
	requiredSubcallRules []RequiredSubcallRule
	recorder             *Recorder

	mu             sync.Mutex
	nextChild      int
	generation     int
	submitted      []trackedRLMChild
	childOrdinalBy map[string]int
}

type trackedRLMChild struct {
	handle     NodeHandle
	ordinal    int
	generation int
}

// RequiredSubcallRule is a runtime-owned recursion-shape requirement.
// Child is the 1-based rlm_query ordinal within one tool executor.
type RequiredSubcallRule struct {
	Child            int `json:"child"`
	RequiredSubcalls int `json:"required_subcalls"`
}

var _ engine.ToolExecutor = (*RLMToolsExecutor)(nil)

// NewRLMToolsExecutor builds a scheduler-backed RLM tool executor.
func NewRLMToolsExecutor(cfg RLMToolsConfig) (*RLMToolsExecutor, error) {
	if cfg.Scheduler == nil {
		return nil, fmt.Errorf("%w: scheduler is required", ErrInvalidSchedulerConfig)
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidSchedulerConfig)
	}
	runID := strings.TrimSpace(cfg.RunID)
	if runID == "" {
		return nil, fmt.Errorf("%w: run id is required", ErrInvalidSchedulerConfig)
	}
	parentNodeID := strings.TrimSpace(cfg.ParentNodeID)
	if parentNodeID == "" {
		return nil, fmt.Errorf("%w: parent node id is required", ErrInvalidSchedulerConfig)
	}
	return &RLMToolsExecutor{
		scheduler:            cfg.Scheduler,
		store:                cfg.Store,
		runID:                runID,
		parentNodeID:         parentNodeID,
		defaultQueryPrompt:   strings.TrimSpace(cfg.DefaultQueryPrompt),
		summaryMaxChars:      cfg.SummaryMaxChars,
		requiredSubcallRules: normalizeRequiredSubcallRules(cfg.RequiredSubcallRules),
		recorder:             cfg.Recorder,
		generation:           1,
		childOrdinalBy:       map[string]int{},
	}, nil
}

// Execute implements engine.ToolExecutor.
func (e *RLMToolsExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	repairedArgs, repair, err := repairRLMToolInput(name, args)
	e.recordToolInputContract(name, args, repairedArgs, repair, err)
	if err != nil {
		return "", err
	}
	args = repairedArgs
	switch name {
	case RLMQueryToolName:
		return e.executeQuery(ctx, args)
	case RLMWaitToolName:
		return e.executeWait(ctx, args)
	case RLMResultToolName:
		return e.executeResult(ctx, args)
	default:
		return "", fmt.Errorf("unknown RLM scheduler tool: %s", name)
	}
}

func (e *RLMToolsExecutor) recordToolInputContract(toolName string, original, repaired json.RawMessage, report rlmToolInputRepairReport, repairErr error) {
	if e == nil || e.recorder == nil || report.Status == "" || report.Status == "valid_as_is" {
		return
	}
	event := ContractEvent{
		Boundary:           "tool_input",
		Tool:               toolName,
		Status:             report.Status,
		IssueKind:          report.IssueKind,
		IssuePath:          report.IssuePath,
		RepairRule:         report.RepairRule,
		RevalidateOK:       report.RevalidateOK,
		Message:            report.Message,
		ToolInputBytes:     len(original),
		RepairedInputBytes: len(repaired),
	}
	if repairErr != nil {
		event.Message = repairErr.Error()
	}
	e.recorder.RecordContractEvent(event)
}

// List implements engine.ToolExecutor.
func (e *RLMToolsExecutor) List() []engine.ToolDef {
	return e.rlmSchedulerToolDefs()
}

func (e *RLMToolsExecutor) rlmSchedulerToolDefs() []engine.ToolDef {
	queryRequired := `"required":["prompt"],`
	if e != nil && strings.TrimSpace(e.defaultQueryPrompt) != "" {
		queryRequired = ""
	}
	return []engine.ToolDef{
		{
			Name:        RLMQueryToolName,
			Description: "Submit one async child solve. The runtime tracks the child automatically; call rlm_wait with no child IDs to collect results.",
			Parameters: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					"prompt":{"type":"string","description":"Child prompt to execute."},
					"max_iterations":{"type":"integer","minimum":1,"description":"Optional child iteration cap override."},
					"max_summary_chars":{"type":"integer","minimum":32,"description":"Optional compact summary character budget for this child result."},
					"metadata":{"type":"object","description":"Optional child metadata map.","additionalProperties":true}
				},
				%s
				"additionalProperties":false
			}`, queryRequired)),
		},
		{
			Name:        RLMWaitToolName,
			Description: "Wait for child solves submitted by this tool session and return compact child summaries. Do not pass node IDs.",
			Parameters: json.RawMessage(`{
				"type":"object",
				"properties":{
					"min_complete":{"type":"integer","minimum":0,"description":"Return once this many children are terminal."},
					"timeout_ms":{"type":"integer","minimum":0,"description":"Optional wait timeout in milliseconds."},
					"max_summary_chars":{"type":"integer","minimum":32,"description":"Optional compact summary character budget for each returned child summary."}
				},
				"additionalProperties":false
			}`),
		},
		{
			Name:        RLMResultToolName,
			Description: "Fetch a compact result summary for a child number from this tool session. Usually rlm_wait is enough.",
			Parameters: json.RawMessage(`{
				"type":"object",
				"properties":{
					"child":{"type":"integer","minimum":1,"description":"Child number returned by rlm_query. Defaults to the most recent child."},
					"max_summary_chars":{"type":"integer","minimum":32,"description":"Optional compact summary character budget for the returned child result."}
				},
				"additionalProperties":false
			}`),
		},
	}
}

type rlmQueryToolInput struct {
	Prompt          string         `json:"prompt"`
	MaxIterations   int            `json:"max_iterations,omitempty"`
	MaxSummaryChars int            `json:"max_summary_chars,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type rlmQueryToolOutput struct {
	Child   int        `json:"child"`
	Status  NodeStatus `json:"status"`
	Message string     `json:"message"`
}

func (e *RLMToolsExecutor) executeQuery(ctx context.Context, args json.RawMessage) (string, error) {
	var input rlmQueryToolInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("decode rlm_query args: %w", err)
	}

	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Prompt == "" {
		input.Prompt = e.defaultQueryPrompt
	}
	if input.Prompt == "" {
		return "", fmt.Errorf("rlm_query requires non-empty prompt")
	}

	parentNodeID := e.parentNodeID
	if parentNodeID == "" {
		return "", fmt.Errorf("rlm_query requires parent_node_id")
	}

	ordinal := e.reserveSubmittedOrdinal()
	requiredSubcalls := e.requiredSubcallsForChild(ordinal)

	handle, err := e.scheduler.Submit(ctx, parentNodeID, QueryRequest{
		Prompt:           input.Prompt,
		MaxIterations:    input.MaxIterations,
		SummaryMaxChars:  firstPositiveInt(input.MaxSummaryChars, e.summaryMaxChars),
		RequiredSubcalls: requiredSubcalls,
		Metadata:         cloneMapAny(input.Metadata),
	})
	if err != nil {
		return "", err
	}
	e.recordSubmitted(handle, ordinal)

	return marshalRLMToolOutput(rlmQueryToolOutput{
		Child:   ordinal,
		Status:  handle.Status,
		Message: "child query submitted; call rlm_wait to collect submitted child results",
	})
}

type rlmWaitToolInput struct {
	Children        []int `json:"children,omitempty"`
	MinComplete     int   `json:"min_complete,omitempty"`
	TimeoutMS       int64 `json:"timeout_ms,omitempty"`
	MaxSummaryChars int   `json:"max_summary_chars,omitempty"`
}

type rlmNodeSummary struct {
	Child                    int        `json:"child"`
	NodeID                   string     `json:"node_id,omitempty"`
	NodeStatus               NodeStatus `json:"node_status,omitempty"`
	Status                   NodeStatus `json:"status"`
	CandidateID              string     `json:"candidate_id,omitempty"`
	CandidateAnswer          string     `json:"candidate_answer,omitempty"`
	CandidateAnswerHash      string     `json:"candidate_answer_hash,omitempty"`
	CandidateStatus          string     `json:"candidate_status,omitempty"`
	CandidateRejectionReason string     `json:"candidate_rejection_reason,omitempty"`
	Summary                  string     `json:"summary,omitempty"`
	SummaryChars             int        `json:"summary_chars,omitempty"`
	SummaryTruncated         bool       `json:"summary_truncated,omitempty"`
	SummaryCompactionMethod  string     `json:"summary_compaction_method,omitempty"`
	ErrorCode                string     `json:"error_code,omitempty"`
	ErrorMessage             string     `json:"error_message,omitempty"`
	RequiredSubcalls         int        `json:"required_subcalls,omitempty"`
	RequiredSubcallAttempts  int        `json:"required_subcall_attempts,omitempty"`
	RecursiveSubcallsUsed    int        `json:"recursive_subcalls_used,omitempty"`
}

type rlmWaitToolOutput struct {
	Completed []rlmNodeSummary `json:"completed"`
	Failed    []rlmNodeSummary `json:"failed"`
	Pending   []rlmNodeSummary `json:"pending"`
	Message   string           `json:"message,omitempty"`
}

func (e *RLMToolsExecutor) executeWait(ctx context.Context, args json.RawMessage) (string, error) {
	var input rlmWaitToolInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("decode rlm_wait args: %w", err)
	}

	parentNodeID := e.parentNodeID
	if parentNodeID == "" {
		return "", fmt.Errorf("rlm_wait requires parent_node_id")
	}
	if input.TimeoutMS < 0 {
		return "", fmt.Errorf("rlm_wait timeout_ms must be >= 0")
	}
	childNodeIDs, generation := e.resolveWaitChildNodeIDs(input)
	minComplete := input.MinComplete
	if minComplete <= 0 && len(childNodeIDs) > 0 {
		minComplete = len(childNodeIDs)
	}

	waitResult, err := e.scheduler.Wait(ctx, parentNodeID, WaitRequest{
		ChildNodeIDs: childNodeIDs,
		MinComplete:  minComplete,
		Timeout:      time.Duration(input.TimeoutMS) * time.Millisecond,
	})
	if err != nil {
		return "", err
	}
	e.advanceGenerationIfComplete(generation, waitResult)

	return marshalRLMToolOutput(rlmWaitToolOutput{
		Completed: summarizeNodes(waitResult.Completed, e.childNumber, firstPositiveInt(input.MaxSummaryChars, e.summaryMaxChars)),
		Failed:    summarizeNodes(waitResult.Failed, e.childNumber, firstPositiveInt(input.MaxSummaryChars, e.summaryMaxChars)),
		Pending:   summarizeNodes(waitResult.Pending, e.childNumber, firstPositiveInt(input.MaxSummaryChars, e.summaryMaxChars)),
		Message:   waitMessage(waitResult),
	})
}

type rlmResultToolInput struct {
	Child           int `json:"child,omitempty"`
	MaxSummaryChars int `json:"max_summary_chars,omitempty"`
}

type rlmResultSummary struct {
	NodeID                   string     `json:"node_id,omitempty"`
	NodeStatus               NodeStatus `json:"node_status,omitempty"`
	Status                   NodeStatus `json:"status"`
	CandidateID              string     `json:"candidate_id,omitempty"`
	CandidateAnswer          string     `json:"candidate_answer,omitempty"`
	CandidateAnswerHash      string     `json:"candidate_answer_hash,omitempty"`
	CandidateStatus          string     `json:"candidate_status,omitempty"`
	CandidateRejectionReason string     `json:"candidate_rejection_reason,omitempty"`
	Summary                  string     `json:"summary,omitempty"`
	SummaryChars             int        `json:"summary_chars,omitempty"`
	SummaryTruncated         bool       `json:"summary_truncated,omitempty"`
	SummaryCompactionMethod  string     `json:"summary_compaction_method,omitempty"`
	ErrorCode                string     `json:"error_code,omitempty"`
	ErrorMessage             string     `json:"error_message,omitempty"`
	StartedAt                time.Time  `json:"started_at,omitempty"`
	CompletedAt              time.Time  `json:"completed_at,omitempty"`
}

type rlmResultToolOutput struct {
	Child  int               `json:"child,omitempty"`
	NodeID string            `json:"node_id,omitempty"`
	Status NodeStatus        `json:"status"`
	Result *rlmResultSummary `json:"result,omitempty"`
}

type rlmToolInputSpec struct {
	optionalFields     map[string]struct{}
	intFields          map[string]struct{}
	objectFields       map[string]struct{}
	intArrayFields     map[string]struct{}
	runtimeOwnedFields map[string]struct{}
}

type rlmToolInputRepairReport struct {
	Status       string
	IssueKind    string
	IssuePath    string
	RepairRule   string
	RevalidateOK bool
	Message      string
}

func repairRLMToolInput(toolName string, args json.RawMessage) (json.RawMessage, rlmToolInputRepairReport, error) {
	report := rlmToolInputRepairReport{Status: "valid_as_is", RevalidateOK: true}
	spec, ok := rlmToolInputSpecFor(toolName)
	if !ok {
		return args, report, nil
	}
	if len(args) == 0 || strings.TrimSpace(string(args)) == "" || strings.TrimSpace(string(args)) == "null" {
		return json.RawMessage(`{}`), rlmToolInputRepairReport{
			Status:       "repaired",
			IssueKind:    "empty_args",
			RepairRule:   "empty_args_to_object",
			RevalidateOK: true,
		}, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(args, &raw); err != nil {
		return args, report, nil
	}
	raw, unwrapped := unwrapRLMToolArgumentsObject(raw, spec)
	if unwrapped {
		report.markRepair("wrapper_object", "$.arguments", "unwrap_arguments_object")
	}
	for field := range raw {
		if _, runtimeOwned := spec.runtimeOwnedFields[field]; runtimeOwned {
			if jsonRawIsEmptyPlaceholder(raw[field]) {
				delete(raw, field)
				report.markRepair("runtime_owned_empty_placeholder", "$."+field, "drop_empty_runtime_owned_field")
				continue
			}
			report.Status = "invalid"
			report.IssueKind = "runtime_owned_field"
			report.IssuePath = "$." + field
			report.RevalidateOK = false
			report.Message = "runtime-owned field supplied by model"
			return nil, report, fmt.Errorf("%s field %q is runtime-owned and cannot be supplied by the model. %s", toolName, field, rlmToolInputHint(toolName))
		}
	}
	for field, value := range raw {
		_, optional := spec.optionalFields[field]
		if optional && jsonRawIsEmptyPlaceholder(value) {
			delete(raw, field)
			report.markRepair("optional_empty_placeholder", "$."+field, "drop_empty_optional_field")
			continue
		}
		if _, intField := spec.intFields[field]; intField {
			repaired, changed, err := repairRLMToolIntField(value)
			if err != nil {
				report.Status = "invalid"
				report.IssueKind = "invalid_integer"
				report.IssuePath = "$." + field
				report.RevalidateOK = false
				report.Message = err.Error()
				return nil, report, fmt.Errorf("%s field %q expects an integer: %w", toolName, field, err)
			}
			if changed {
				raw[field] = repaired
				report.markRepair("numeric_string", "$."+field, "parse_int_string")
			}
			continue
		}
		if _, objectField := spec.objectFields[field]; objectField {
			repaired, changed, err := repairRLMToolJSONObjectField(value)
			if err != nil {
				report.Status = "invalid"
				report.IssueKind = "invalid_object"
				report.IssuePath = "$." + field
				report.RevalidateOK = false
				report.Message = err.Error()
				return nil, report, fmt.Errorf("%s field %q expects an object: %w", toolName, field, err)
			}
			if changed {
				raw[field] = repaired
				report.markRepair("stringified_object", "$."+field, "parse_stringified_object")
			}
			continue
		}
		if _, arrayField := spec.intArrayFields[field]; arrayField {
			repaired, changed, err := repairRLMToolIntArrayField(value)
			if err != nil {
				report.Status = "invalid"
				report.IssueKind = "invalid_integer_array"
				report.IssuePath = "$." + field
				report.RevalidateOK = false
				report.Message = err.Error()
				return nil, report, fmt.Errorf("%s field %q expects an integer array: %w", toolName, field, err)
			}
			if changed {
				raw[field] = repaired
				report.markRepair("stringified_or_bare_array", "$."+field, "parse_or_wrap_int_array")
			}
			continue
		}
	}
	out, err := json.Marshal(raw)
	if err != nil {
		report.Status = "invalid"
		report.IssueKind = "marshal_repaired_args"
		report.RevalidateOK = false
		report.Message = err.Error()
		return nil, report, err
	}
	return out, report, nil
}

func (r *rlmToolInputRepairReport) markRepair(issueKind, issuePath, repairRule string) {
	if r == nil {
		return
	}
	if r.Status == "" || r.Status == "valid_as_is" {
		r.Status = "repaired"
		r.IssueKind = issueKind
		r.IssuePath = issuePath
		r.RepairRule = repairRule
		r.RevalidateOK = true
		return
	}
	if r.Status == "repaired" {
		r.IssueKind = "multiple"
		r.IssuePath = ""
		r.RepairRule = "multiple"
		r.RevalidateOK = true
	}
}

func rlmToolInputSpecFor(toolName string) (rlmToolInputSpec, bool) {
	runtimeOwned := stringSet("parent_node_id", "node_id", "run_id", "child_ids", "required_subcalls")
	switch toolName {
	case RLMQueryToolName:
		return rlmToolInputSpec{
			optionalFields:     stringSet("max_iterations", "max_summary_chars", "metadata"),
			intFields:          stringSet("max_iterations", "max_summary_chars"),
			objectFields:       stringSet("metadata"),
			runtimeOwnedFields: runtimeOwned,
		}, true
	case RLMWaitToolName:
		return rlmToolInputSpec{
			optionalFields:     stringSet("children", "min_complete", "timeout_ms", "max_summary_chars"),
			intFields:          stringSet("min_complete", "timeout_ms", "max_summary_chars"),
			intArrayFields:     stringSet("children"),
			runtimeOwnedFields: runtimeOwned,
		}, true
	case RLMResultToolName:
		return rlmToolInputSpec{
			optionalFields:     stringSet("child", "max_summary_chars"),
			intFields:          stringSet("child", "max_summary_chars"),
			runtimeOwnedFields: runtimeOwned,
		}, true
	default:
		return rlmToolInputSpec{}, false
	}
}

func stringSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func unwrapRLMToolArgumentsObject(raw map[string]json.RawMessage, spec rlmToolInputSpec) (map[string]json.RawMessage, bool) {
	if len(raw) != 1 {
		return raw, false
	}
	for _, wrapper := range []string{"arguments", "args"} {
		value, ok := raw[wrapper]
		if !ok {
			continue
		}
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(value, &nested); err != nil || len(nested) == 0 {
			return raw, false
		}
		for field := range nested {
			if _, ok := spec.optionalFields[field]; ok {
				return nested, true
			}
			if _, ok := spec.intFields[field]; ok {
				return nested, true
			}
			if _, ok := spec.objectFields[field]; ok {
				return nested, true
			}
			if _, ok := spec.intArrayFields[field]; ok {
				return nested, true
			}
			if _, ok := spec.runtimeOwnedFields[field]; ok {
				return nested, true
			}
			if field == "prompt" {
				return nested, true
			}
		}
	}
	return raw, false
}

func jsonRawIsEmptyPlaceholder(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	switch trimmed {
	case "", "null", `""`, "{}", "[]":
		return true
	default:
		return false
	}
}

func repairRLMToolIntField(raw json.RawMessage) (json.RawMessage, bool, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return raw, false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return raw, false, nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(strconv.FormatInt(value, 10)), true, nil
}

func repairRLMToolJSONObjectField(raw json.RawMessage) (json.RawMessage, bool, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return raw, false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return raw, false, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, false, err
	}
	repaired, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	return repaired, true, nil
}

func repairRLMToolIntArrayField(raw json.RawMessage) (json.RawMessage, bool, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return raw, false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return raw, false, nil
	}
	if strings.HasPrefix(text, "[") {
		var values []int
		if err := json.Unmarshal([]byte(text), &values); err != nil {
			return nil, false, err
		}
		repaired, err := json.Marshal(values)
		if err != nil {
			return nil, false, err
		}
		return repaired, true, nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage("[" + strconv.FormatInt(value, 10) + "]"), true, nil
}

func rlmToolInputHint(toolName string) string {
	switch toolName {
	case RLMWaitToolName:
		return "Call rlm_wait with {}. The runtime tracks submitted children automatically."
	case RLMResultToolName:
		return "Call rlm_result with {\"child\":N}, using a child number returned by this tool session."
	case RLMQueryToolName:
		return "Call rlm_query with a prompt and optional metadata only; the runtime assigns ownership fields."
	default:
		return "Remove runtime-owned fields and retry."
	}
}

func (e *RLMToolsExecutor) executeResult(ctx context.Context, args json.RawMessage) (string, error) {
	var input rlmResultToolInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("decode rlm_result args: %w", err)
	}

	nodeID := ""
	child := input.Child
	if child > 0 {
		nodeID = e.nodeIDForChild(child)
		if nodeID == "" {
			return "", fmt.Errorf("rlm_result child %d was not submitted by this tool session", child)
		}
	}
	if nodeID == "" {
		nodeID = e.mostRecentChildNodeID()
	}
	if nodeID == "" {
		return "", fmt.Errorf("rlm_result requires a submitted child")
	}
	runID := e.runID
	if runID == "" {
		return "", fmt.Errorf("rlm_result requires run_id")
	}

	node, err := e.store.GetNode(ctx, runID, nodeID)
	if err != nil {
		return "", err
	}

	return marshalRLMToolOutput(rlmResultToolOutput{
		Child:  e.childNumber(node.ID),
		NodeID: node.ID,
		Status: node.Status,
		Result: summarizeNodeResult(node.ID, node.Status, e.childNumber(node.ID), node.Result, firstPositiveInt(input.MaxSummaryChars, e.summaryMaxChars)),
	})
}

func (e *RLMToolsExecutor) reserveSubmittedOrdinal() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextChild++
	return e.nextChild
}

func (e *RLMToolsExecutor) recordSubmitted(handle NodeHandle, ordinal int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ordinal <= 0 {
		ordinal = e.nextChild + 1
	}
	generation := e.generation
	if generation <= 0 {
		generation = 1
		e.generation = generation
	}
	e.submitted = append(e.submitted, trackedRLMChild{
		handle:     handle,
		ordinal:    ordinal,
		generation: generation,
	})
	e.childOrdinalBy[handle.NodeID] = ordinal
	return ordinal
}

func (e *RLMToolsExecutor) requiredSubcallsForChild(child int) int {
	if child <= 0 || len(e.requiredSubcallRules) == 0 {
		return 0
	}
	for _, rule := range e.requiredSubcallRules {
		if rule.Child == child {
			return rule.RequiredSubcalls
		}
	}
	return 0
}

func (e *RLMToolsExecutor) resolveWaitChildNodeIDs(input rlmWaitToolInput) ([]string, int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(input.Children) > 0 {
		out := make([]string, 0, len(input.Children))
		for _, child := range input.Children {
			for _, tracked := range e.submitted {
				if tracked.ordinal == child {
					out = append(out, tracked.handle.NodeID)
					break
				}
			}
		}
		return out, 0
	}

	generation := e.generation
	if generation <= 0 {
		generation = 1
	}
	out := make([]string, 0)
	for _, tracked := range e.submitted {
		if tracked.generation == generation {
			out = append(out, tracked.handle.NodeID)
		}
	}
	return out, generation
}

func (e *RLMToolsExecutor) advanceGenerationIfComplete(generation int, result WaitResult) {
	if generation <= 0 || len(result.Pending) > 0 {
		return
	}
	if len(result.Completed)+len(result.Failed) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.generation == generation {
		e.generation++
	}
}

func (e *RLMToolsExecutor) childNumber(nodeID string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.childOrdinalBy[nodeID]
}

func (e *RLMToolsExecutor) nodeIDForChild(child int) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tracked := range e.submitted {
		if tracked.ordinal == child {
			return tracked.handle.NodeID
		}
	}
	return ""
}

func (e *RLMToolsExecutor) mostRecentChildNodeID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.submitted) == 0 {
		return ""
	}
	return e.submitted[len(e.submitted)-1].handle.NodeID
}

//nolint:unused // Kept for recursive tool routing variants.
func (e *RLMToolsExecutor) resolveParentNodeID(input string) string {
	parentNodeID := strings.TrimSpace(input)
	if parentNodeID != "" {
		return parentNodeID
	}
	return e.parentNodeID
}

func summarizeNodes(nodes []Node, childNumber func(string) int, maxSummaryChars int) []rlmNodeSummary {
	if len(nodes) == 0 {
		return []rlmNodeSummary{}
	}

	out := make([]rlmNodeSummary, 0, len(nodes))
	for _, node := range nodes {
		item := rlmNodeSummary{
			Child:      childNumber(node.ID),
			NodeID:     node.ID,
			NodeStatus: node.Status,
			Status:     node.Status,
		}
		if node.Result != nil {
			summary, truncated := compactRLMSummaryText(node.Result.Summary, maxSummaryChars)
			item.Summary = summary
			item.SummaryChars = runeLen(summary)
			item.SummaryTruncated = truncated
			item.SummaryCompactionMethod = strings.TrimSpace(stringFromMapAny(node.Result.Metadata, "summary_compaction_method"))
			item.ErrorCode = strings.TrimSpace(node.Result.ErrorCode)
			item.ErrorMessage = strings.TrimSpace(node.Result.ErrorMessage)
			item.RequiredSubcalls = intFromAny(node.Result.Metadata["required_subcalls"])
			item.RequiredSubcallAttempts = intFromAny(node.Result.Metadata["required_subcall_attempts"])
			item.RecursiveSubcallsUsed = intFromAny(node.Result.Metadata["recursive_subcalls_used"])
			if candidate := childCandidateRecord(item.Child, node.Result); candidate.CandidateID != "" {
				item.CandidateID = candidate.CandidateID
				item.CandidateAnswer = candidate.CandidateAnswer
				item.CandidateAnswerHash = candidate.CandidateAnswerHash
				item.CandidateStatus = candidate.CandidateStatus
			} else if candidate.CandidateStatus != "" {
				item.CandidateStatus = candidate.CandidateStatus
				item.CandidateRejectionReason = candidate.CandidateRejectionReason
			}
		}
		out = append(out, item)
	}
	return out
}

//nolint:unused // Kept for recursive tool input normalization variants.
func trimNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func waitMessage(result WaitResult) string {
	if len(result.Pending) > 0 {
		return "some child results are still pending; call rlm_wait again to continue waiting"
	}
	if len(result.Completed)+len(result.Failed) == 0 {
		return "no submitted child queries are pending in this tool session"
	}
	return "child results collected; synthesize the completed child summaries"
}

func normalizeRequiredSubcallRules(rules []RequiredSubcallRule) []RequiredSubcallRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]RequiredSubcallRule, 0, len(rules))
	seen := map[int]int{}
	for _, rule := range rules {
		if rule.Child <= 0 || rule.RequiredSubcalls <= 0 {
			continue
		}
		if existing, ok := seen[rule.Child]; ok {
			if rule.RequiredSubcalls > existing {
				seen[rule.Child] = rule.RequiredSubcalls
			}
			continue
		}
		seen[rule.Child] = rule.RequiredSubcalls
	}
	for child, required := range seen {
		out = append(out, RequiredSubcallRule{Child: child, RequiredSubcalls: required})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Child < out[j].Child
	})
	return out
}

func summarizeNodeResult(nodeID string, nodeStatus NodeStatus, child int, result *NodeResult, maxSummaryChars int) *rlmResultSummary {
	if result == nil {
		return nil
	}
	summary, truncated := compactRLMSummaryText(result.Summary, maxSummaryChars)
	candidate := childCandidateRecord(child, result)
	return &rlmResultSummary{
		NodeID:                   strings.TrimSpace(nodeID),
		NodeStatus:               nodeStatus,
		Status:                   result.Status,
		CandidateID:              candidate.CandidateID,
		CandidateAnswer:          candidate.CandidateAnswer,
		CandidateAnswerHash:      candidate.CandidateAnswerHash,
		CandidateStatus:          candidate.CandidateStatus,
		CandidateRejectionReason: candidate.CandidateRejectionReason,
		Summary:                  summary,
		SummaryChars:             runeLen(summary),
		SummaryTruncated:         truncated,
		SummaryCompactionMethod:  strings.TrimSpace(stringFromMapAny(result.Metadata, "summary_compaction_method")),
		ErrorCode:                strings.TrimSpace(result.ErrorCode),
		ErrorMessage:             strings.TrimSpace(result.ErrorMessage),
		StartedAt:                result.StartedAt,
		CompletedAt:              result.CompletedAt,
	}
}

type rlmCandidateRecord struct {
	CandidateID              string
	CandidateAnswer          string
	CandidateAnswerHash      string
	CandidateStatus          string
	CandidateRejectionReason string
}

func childCandidateRecord(child int, result *NodeResult) rlmCandidateRecord {
	if result == nil {
		return rlmCandidateRecord{}
	}
	answer, status := candidateAnswerAndStatus(result)
	answer = strings.TrimSpace(answer)
	if answer == "" {
		if status != "" && status != string(NodeStatusCompleted) {
			return rlmCandidateRecord{CandidateStatus: status}
		}
		return rlmCandidateRecord{}
	}
	concrete := classifyCandidateConcreteness(answer, status)
	if concrete.Status != "solved" {
		return rlmCandidateRecord{
			CandidateAnswer:          answer,
			CandidateStatus:          concrete.Status,
			CandidateRejectionReason: concrete.Reason,
		}
	}
	hash := shortSHA256(answer)
	prefix := "child"
	if child > 0 {
		prefix = fmt.Sprintf("child-%d", child)
	}
	return rlmCandidateRecord{
		CandidateID:         prefix + ":sha256:" + hash,
		CandidateAnswer:     answer,
		CandidateAnswerHash: "sha256:" + hash,
		CandidateStatus:     status,
	}
}

type candidateConcreteness struct {
	Status string
	Reason string
}

func classifyCandidateConcreteness(answer, status string) candidateConcreteness {
	answer = strings.TrimSpace(answer)
	status = strings.TrimSpace(status)
	if status == "" {
		status = "solved"
	}
	if answer == "" {
		return candidateConcreteness{Status: "malformed", Reason: "missing solution answer"}
	}
	if !strings.HasPrefix(answer, "solution =") {
		return candidateConcreteness{Status: "malformed", Reason: "missing solution prefix"}
	}
	if solutionLineIsBlocked(answer) {
		if strings.HasPrefix(strings.ToLower(solutionPayload(answer)), "partial") {
			return candidateConcreteness{Status: "partial", Reason: "solution line is partial"}
		}
		return candidateConcreteness{Status: "blocked", Reason: "solution line is blocked"}
	}
	payload := solutionPayload(answer)
	if payload == "" {
		return candidateConcreteness{Status: "malformed", Reason: "empty solution payload"}
	}
	if reason := placeholderSolutionReason(payload); reason != "" {
		return candidateConcreteness{Status: "placeholder", Reason: reason}
	}
	if status != "solved" && status != string(NodeStatusCompleted) {
		return candidateConcreteness{Status: status, Reason: "candidate status is not solved"}
	}
	return candidateConcreteness{Status: "solved"}
}

func solutionPayload(answer string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(answer), "solution ="))
}

func placeholderSolutionReason(payload string) string {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "empty solution payload"
	}
	if reason := placeholderStringReason(payload); reason != "" {
		return reason
	}
	if reason := placeholderUnresolvedLabelListReason(payload); reason != "" {
		return reason
	}
	var decoded any
	if err := json.Unmarshal([]byte(payload), &decoded); err == nil {
		if reason := placeholderJSONValueReason(decoded); reason != "" {
			return reason
		}
	}
	return ""
}

func placeholderJSONValueReason(value any) string {
	switch typed := value.(type) {
	case string:
		return placeholderStringReason(typed)
	case []any:
		allLabelRefs := len(typed) > 1
		for _, item := range typed {
			if reason := placeholderJSONValueReason(item); reason != "" {
				return reason
			}
			itemString, ok := item.(string)
			if !ok || !unresolvedLabelRef(itemString) {
				allLabelRefs = false
			}
		}
		if allLabelRefs {
			return "solution list contains only unresolved label references"
		}
	case map[string]any:
		for _, item := range typed {
			if reason := placeholderJSONValueReason(item); reason != "" {
				return reason
			}
		}
	}
	return ""
}

func placeholderUnresolvedLabelListReason(payload string) string {
	trimmed := strings.TrimSpace(payload)
	trimmed = strings.TrimSuffix(trimmed, ".")
	trimmed = strings.TrimSpace(trimmed)
	if len(trimmed) < 5 {
		return ""
	}
	open, close := trimmed[0], trimmed[len(trimmed)-1]
	if !((open == '[' && close == ']') || (open == '(' && close == ')')) {
		return ""
	}
	body := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	if body == "" || !strings.Contains(body, ",") {
		return ""
	}
	parts := strings.Split(body, ",")
	for _, part := range parts {
		item := strings.TrimSpace(part)
		item = strings.Trim(item, `"'`)
		if !unresolvedLabelRef(item) {
			return ""
		}
	}
	return "solution list contains only unresolved label references"
}

func unresolvedLabelRef(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return false
	}
	for _, sep := range []string{"_", "-"} {
		for _, prefix := range []string{"node", "q", "question", "target", "item", "answer", "value", "result"} {
			if strings.HasPrefix(value, prefix+sep) && suffixAllDigits(value[len(prefix)+len(sep):]) {
				return true
			}
		}
	}
	if strings.HasPrefix(value, "q") && suffixAllDigits(value[1:]) {
		return true
	}
	return false
}

func suffixAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func placeholderStringReason(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if placeholderAngleTemplate(lower) {
		return "solution contains unresolved angle-bracket template"
	}
	for _, marker := range []string{"checks:", "confidence:", "status:", "reason:"} {
		if strings.Contains(lower, marker) {
			return "solution contains metadata text"
		}
	}
	for _, phrase := range []string{
		"answer for ",
		"value for ",
		"result for ",
		"solution for ",
		"todo",
		"tbd",
		"placeholder",
		"fill in",
		"not computed",
		"cannot determine",
		"insufficient information",
		"not enough information",
	} {
		if strings.Contains(lower, phrase) {
			return "solution contains unresolved placeholder phrase"
		}
	}
	return ""
}

func placeholderAngleTemplate(lower string) bool {
	start := strings.Index(lower, "<")
	for start >= 0 {
		endRel := strings.Index(lower[start+1:], ">")
		if endRel < 0 {
			return false
		}
		inside := strings.TrimSpace(lower[start+1 : start+1+endRel])
		for _, word := range []string{"value", "answer", "result", "solution", "todo", "fill", "placeholder"} {
			if strings.Contains(inside, word) {
				return true
			}
		}
		next := start + 1 + endRel + 1
		if next >= len(lower) {
			return false
		}
		nextStart := strings.Index(lower[next:], "<")
		if nextStart < 0 {
			return false
		}
		start = next + nextStart
	}
	return false
}

func candidateAnswerAndStatus(result *NodeResult) (string, string) {
	status := strings.TrimSpace(string(result.Status))
	for _, text := range []string{result.Answer, result.Summary} {
		if artifact, ok := parseBraidNodeArtifact(strings.TrimSpace(text)); ok {
			if artifact.Status != "" {
				status = strings.TrimSpace(artifact.Status)
			}
			if answer := strings.TrimSpace(fmt.Sprint(artifact.Answer)); strings.HasPrefix(answer, "solution =") {
				return canonicalSolutionLine(answer), status
			}
		}
		if answer := firstSolutionLine(text); answer != "" {
			if solutionLineIsBlocked(answer) {
				return "", "blocked"
			}
			return answer, "solved"
		}
	}
	return "", status
}

func solutionLineIsBlocked(answer string) bool {
	value := strings.ToLower(solutionPayload(answer))
	return strings.HasPrefix(value, "blocked") || strings.HasPrefix(value, "partial")
}

func firstSolutionLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "solution =") {
			return canonicalSolutionLine(line)
		}
	}
	compact := strings.TrimSpace(text)
	idx := strings.Index(compact, "solution =")
	if idx < 0 {
		return ""
	}
	return canonicalSolutionLine(strings.TrimSpace(compact[idx:]))
}

func canonicalSolutionLine(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "solution =") {
		return line
	}
	payload := solutionPayload(line)
	if payload == "" {
		return line
	}
	switch payload[0] {
	case '[', '{', '(':
		if end := matchingPayloadCloseIndex(payload); end >= 0 {
			return "solution = " + strings.TrimSpace(payload[:end+1])
		}
	}
	return line
}

func matchingPayloadCloseIndex(payload string) int {
	if payload == "" {
		return -1
	}
	open := payload[0]
	close := byte(0)
	switch open {
	case '[':
		close = ']'
	case '{':
		close = '}'
	case '(':
		close = ')'
	default:
		return -1
	}
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(payload); i++ {
		ch := payload[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == open {
			depth++
			continue
		}
		if ch == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func shortSHA256(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return fmt.Sprintf("%x", sum[:])[:16]
}

func compactRLMSummaryText(text string, maxChars int) (string, bool) {
	compact := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if compact == "" || maxChars <= 0 {
		return compact, false
	}
	runes := []rune(compact)
	if len(runes) <= maxChars {
		return compact, false
	}
	if maxChars <= 3 {
		return string(runes[:maxChars]), true
	}
	return string(runes[:maxChars-3]) + "...", true
}

func runeLen(text string) int {
	return len([]rune(text))
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func marshalRLMToolOutput(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
