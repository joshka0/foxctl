package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
		generation:           1,
		childOrdinalBy:       map[string]int{},
	}, nil
}

// Execute implements engine.ToolExecutor.
func (e *RLMToolsExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
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
	if err := rejectRLMToolFields(args, RLMQueryToolName, "parent_node_id", "node_id", "run_id", "child_ids", "required_subcalls"); err != nil {
		return "", err
	}
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
	Child                   int        `json:"child"`
	Status                  NodeStatus `json:"status"`
	Summary                 string     `json:"summary,omitempty"`
	SummaryChars            int        `json:"summary_chars,omitempty"`
	SummaryTruncated        bool       `json:"summary_truncated,omitempty"`
	SummaryCompactionMethod string     `json:"summary_compaction_method,omitempty"`
	ErrorCode               string     `json:"error_code,omitempty"`
	ErrorMessage            string     `json:"error_message,omitempty"`
	RequiredSubcalls        int        `json:"required_subcalls,omitempty"`
	RequiredSubcallAttempts int        `json:"required_subcall_attempts,omitempty"`
	RecursiveSubcallsUsed   int        `json:"recursive_subcalls_used,omitempty"`
}

type rlmWaitToolOutput struct {
	Completed []rlmNodeSummary `json:"completed"`
	Failed    []rlmNodeSummary `json:"failed"`
	Pending   []rlmNodeSummary `json:"pending"`
	Message   string           `json:"message,omitempty"`
}

func (e *RLMToolsExecutor) executeWait(ctx context.Context, args json.RawMessage) (string, error) {
	if err := rejectRLMToolFields(args, RLMWaitToolName, "parent_node_id", "node_id", "run_id", "child_ids", "required_subcalls"); err != nil {
		return "", err
	}
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
	Status                  NodeStatus `json:"status"`
	Summary                 string     `json:"summary,omitempty"`
	SummaryChars            int        `json:"summary_chars,omitempty"`
	SummaryTruncated        bool       `json:"summary_truncated,omitempty"`
	SummaryCompactionMethod string     `json:"summary_compaction_method,omitempty"`
	ErrorCode               string     `json:"error_code,omitempty"`
	ErrorMessage            string     `json:"error_message,omitempty"`
	StartedAt               time.Time  `json:"started_at,omitempty"`
	CompletedAt             time.Time  `json:"completed_at,omitempty"`
}

type rlmResultToolOutput struct {
	Child  int               `json:"child,omitempty"`
	Status NodeStatus        `json:"status"`
	Result *rlmResultSummary `json:"result,omitempty"`
}

func (e *RLMToolsExecutor) executeResult(ctx context.Context, args json.RawMessage) (string, error) {
	if err := rejectRLMToolFields(args, RLMResultToolName, "parent_node_id", "node_id", "run_id", "child_ids", "required_subcalls"); err != nil {
		return "", err
	}
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
		Status: node.Status,
		Result: summarizeNodeResult(node.Result, firstPositiveInt(input.MaxSummaryChars, e.summaryMaxChars)),
	})
}

func rejectRLMToolFields(args json.RawMessage, toolName string, disallowed ...string) error {
	if len(args) == 0 || string(args) == "null" {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(args, &raw); err != nil {
		return nil
	}
	for _, field := range disallowed {
		if _, ok := raw[field]; ok {
			return fmt.Errorf("%s field %q is runtime-owned and not accepted from model input", toolName, field)
		}
	}
	return nil
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
			Child:  childNumber(node.ID),
			Status: node.Status,
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
		}
		out = append(out, item)
	}
	return out
}

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

func summarizeNodeResult(result *NodeResult, maxSummaryChars int) *rlmResultSummary {
	if result == nil {
		return nil
	}
	summary, truncated := compactRLMSummaryText(result.Summary, maxSummaryChars)
	return &rlmResultSummary{
		Status:                  result.Status,
		Summary:                 summary,
		SummaryChars:            runeLen(summary),
		SummaryTruncated:        truncated,
		SummaryCompactionMethod: strings.TrimSpace(stringFromMapAny(result.Metadata, "summary_compaction_method")),
		ErrorCode:               strings.TrimSpace(result.ErrorCode),
		ErrorMessage:            strings.TrimSpace(result.ErrorMessage),
		StartedAt:               result.StartedAt,
		CompletedAt:             result.CompletedAt,
	}
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
