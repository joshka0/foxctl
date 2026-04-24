package optdata

import (
	"fmt"
	"time"
)

// RecordType labels one optimization trajectory record schema.
type RecordType string

const (
	// RecordTypeTrajectoryV1 is the first stable schema for prompt optimization
	// trajectory records.
	RecordTypeTrajectoryV1 RecordType = "rlm_opt_trajectory_v1"
)

const (
	ComponentTaskPrompt            = "rlm.task.prompt"
	ComponentREPLSystemPrompt      = "rlm.repl.system_prompt"
	ComponentHelperSolveSystem     = "rlm.helper_solve.system_prompt"
	ComponentHelperSolveDraft      = "rlm.helper_solve.draft_prompt"
	ComponentRuntimeOutput         = "rlm.runtime.output"
	ComponentRuntimeError          = "rlm.runtime.error"
	ComponentOutputSanitization    = "rlm.runtime.output_sanitization"
	ComponentSolutionExtraction    = "rlm.runtime.solution_extraction"
	ComponentEphemeralFinalization = "rlm.runtime.ephemeral_finalization"
	ComponentHelperSolveRuntime    = "rlm.runtime.helper_solve"
	ComponentTrajectoryRuntime     = "rlm.runtime.trajectory"
)

// MetricGoal encodes how a metric should be interpreted by optimizers.
type MetricGoal string

const (
	MetricGoalMaximize MetricGoal = "maximize"
	MetricGoalMinimize MetricGoal = "minimize"
	MetricGoalTarget   MetricGoal = "target"
)

// PromptContextBlock is one structured context chunk included in the prompt.
type PromptContextBlock struct {
	Name    string `json:"name,omitempty"`
	Source  string `json:"source,omitempty"`
	Content string `json:"content,omitempty"`
}

// PromptToolDefinition captures one tool signature visible in the prompt.
type PromptToolDefinition struct {
	Name            string `json:"name,omitempty"`
	Description     string `json:"description,omitempty"`
	InputSchemaJSON string `json:"input_schema_json,omitempty"`
}

// PromptComponents stores the prompt fields that are useful for optimization.
type PromptComponents struct {
	Objective       string                 `json:"objective,omitempty"`
	System          string                 `json:"system,omitempty"`
	Developer       string                 `json:"developer,omitempty"`
	User            string                 `json:"user,omitempty"`
	OutputSchema    string                 `json:"output_schema,omitempty"`
	ContextBlocks   []PromptContextBlock   `json:"context_blocks,omitempty"`
	ToolDefinitions []PromptToolDefinition `json:"tool_definitions,omitempty"`
}

// ExecutionMetadata stores execution-level fields for one prompt attempt.
type ExecutionMetadata struct {
	Runtime          string   `json:"runtime,omitempty"`
	Mode             string   `json:"mode,omitempty"`
	Model            string   `json:"model,omitempty"`
	OutputText       string   `json:"output_text,omitempty"`
	RunID            string   `json:"run_id,omitempty"`
	NodeID           string   `json:"node_id,omitempty"`
	SessionID        string   `json:"session_id,omitempty"`
	AgentID          string   `json:"agent_id,omitempty"`
	Iteration        int      `json:"iteration,omitempty"`
	Attempt          int      `json:"attempt,omitempty"`
	Success          bool     `json:"success"`
	ErrorCode        string   `json:"error_code,omitempty"`
	ErrorMessage     string   `json:"error_message,omitempty"`
	PromptTokens     int      `json:"prompt_tokens,omitempty"`
	CompletionTokens int      `json:"completion_tokens,omitempty"`
	TotalTokens      int      `json:"total_tokens,omitempty"`
	DurationMS       int64    `json:"duration_ms,omitempty"`
	EvidenceRefs     []string `json:"evidence_refs,omitempty"`
	RetrievedPaths   []string `json:"retrieved_paths,omitempty"`
}

// MetricFeedback is one scored signal for downstream optimization.
type MetricFeedback struct {
	Name   string     `json:"name"`
	Value  float64    `json:"value"`
	Goal   MetricGoal `json:"goal,omitempty"`
	Weight float64    `json:"weight,omitempty"`
	Target *float64   `json:"target,omitempty"`
	Passed *bool      `json:"passed,omitempty"`
	Source string     `json:"source,omitempty"`
	Notes  string     `json:"notes,omitempty"`
}

// PromptFeedback is textual optimizer feedback tied to one prompt component.
type PromptFeedback struct {
	Component string            `json:"component,omitempty"`
	Stage     string            `json:"stage,omitempty"`
	Outcome   string            `json:"outcome,omitempty"`
	Message   string            `json:"message"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// TrajectoryRecord is one JSONL row used to train or tune prompt policies.
type TrajectoryRecord struct {
	RecordType     RecordType        `json:"record_type"`
	SchemaVersion  int               `json:"schema_version"`
	RecordID       string            `json:"record_id,omitempty"`
	ParentRecordID string            `json:"parent_record_id,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	Prompt         PromptComponents  `json:"prompt"`
	Execution      ExecutionMetadata `json:"execution"`
	Metrics        []MetricFeedback  `json:"metrics,omitempty"`
	Feedback       []PromptFeedback  `json:"feedback,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

// Validate ensures a trajectory record is structurally usable.
func (record TrajectoryRecord) Validate() error {
	if record.RecordType == "" {
		return fmt.Errorf("record type is required")
	}
	if record.SchemaVersion <= 0 {
		return fmt.Errorf("schema version must be > 0")
	}
	if record.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	for i := range record.Metrics {
		if record.Metrics[i].Name == "" {
			return fmt.Errorf("metric[%d] name is required", i)
		}
	}
	return nil
}

// BuildInput is the input payload for a RecordBuilder.
type BuildInput struct {
	RecordType     RecordType
	SchemaVersion  int
	RecordID       string
	ParentRecordID string
	CreatedAt      time.Time
	Prompt         PromptComponents
	Execution      ExecutionMetadata
	Metrics        []MetricFeedback
	Feedback       []PromptFeedback
	Labels         map[string]string
}

// BuilderOption configures a RecordBuilder.
type BuilderOption func(*RecordBuilder)

// RecordBuilder constructs normalized records with injected dependencies.
type RecordBuilder struct {
	now               func() time.Time
	defaultRecordType RecordType
	defaultVersion    int
}

// WithBuilderNow injects a deterministic clock for record creation.
func WithBuilderNow(now func() time.Time) BuilderOption {
	return func(builder *RecordBuilder) {
		if now != nil {
			builder.now = now
		}
	}
}

// WithBuilderDefaultRecordType sets the fallback record type.
func WithBuilderDefaultRecordType(recordType RecordType) BuilderOption {
	return func(builder *RecordBuilder) {
		if recordType != "" {
			builder.defaultRecordType = recordType
		}
	}
}

// WithBuilderSchemaVersion sets the fallback schema version.
func WithBuilderSchemaVersion(version int) BuilderOption {
	return func(builder *RecordBuilder) {
		if version > 0 {
			builder.defaultVersion = version
		}
	}
}

// NewRecordBuilder creates a record builder with injectable defaults.
func NewRecordBuilder(options ...BuilderOption) *RecordBuilder {
	builder := &RecordBuilder{
		now:               time.Now,
		defaultRecordType: RecordTypeTrajectoryV1,
		defaultVersion:    1,
	}
	for _, option := range options {
		option(builder)
	}
	return builder
}

// Build creates one normalized trajectory record from input.
func (builder *RecordBuilder) Build(input BuildInput) TrajectoryRecord {
	recordType := input.RecordType
	if recordType == "" {
		recordType = builder.defaultRecordType
	}
	version := input.SchemaVersion
	if version <= 0 {
		version = builder.defaultVersion
	}
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = builder.now()
	}

	return TrajectoryRecord{
		RecordType:     recordType,
		SchemaVersion:  version,
		RecordID:       input.RecordID,
		ParentRecordID: input.ParentRecordID,
		CreatedAt:      createdAt.UTC(),
		Prompt:         clonePromptComponents(input.Prompt),
		Execution:      cloneExecutionMetadata(input.Execution),
		Metrics:        cloneMetrics(input.Metrics),
		Feedback:       cloneFeedback(input.Feedback),
		Labels:         cloneLabels(input.Labels),
	}
}

func clonePromptComponents(prompt PromptComponents) PromptComponents {
	cloned := prompt
	if len(prompt.ContextBlocks) > 0 {
		cloned.ContextBlocks = append([]PromptContextBlock(nil), prompt.ContextBlocks...)
	}
	if len(prompt.ToolDefinitions) > 0 {
		cloned.ToolDefinitions = append([]PromptToolDefinition(nil), prompt.ToolDefinitions...)
	}
	return cloned
}

func cloneExecutionMetadata(execution ExecutionMetadata) ExecutionMetadata {
	cloned := execution
	if len(execution.EvidenceRefs) > 0 {
		cloned.EvidenceRefs = append([]string(nil), execution.EvidenceRefs...)
	}
	if len(execution.RetrievedPaths) > 0 {
		cloned.RetrievedPaths = append([]string(nil), execution.RetrievedPaths...)
	}
	return cloned
}

func cloneMetrics(metrics []MetricFeedback) []MetricFeedback {
	if len(metrics) == 0 {
		return nil
	}
	cloned := make([]MetricFeedback, len(metrics))
	for i, metric := range metrics {
		clonedMetric := metric
		if metric.Target != nil {
			target := *metric.Target
			clonedMetric.Target = &target
		}
		if metric.Passed != nil {
			passed := *metric.Passed
			clonedMetric.Passed = &passed
		}
		cloned[i] = clonedMetric
	}
	return cloned
}

func cloneFeedback(feedback []PromptFeedback) []PromptFeedback {
	if len(feedback) == 0 {
		return nil
	}
	cloned := make([]PromptFeedback, len(feedback))
	for i, item := range feedback {
		clonedItem := item
		clonedItem.Labels = cloneLabels(item.Labels)
		cloned[i] = clonedItem
	}
	return cloned
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func cloneRecord(record TrajectoryRecord) TrajectoryRecord {
	cloned := record
	cloned.Prompt = clonePromptComponents(record.Prompt)
	cloned.Execution = cloneExecutionMetadata(record.Execution)
	cloned.Metrics = cloneMetrics(record.Metrics)
	cloned.Feedback = cloneFeedback(record.Feedback)
	cloned.Labels = cloneLabels(record.Labels)
	return cloned
}
