package rlm

import "math"

// TaskType classifies the query shape for lambda composition strategy selection.
type TaskType string

const (
	TaskTypeCodeLocate     TaskType = "code_locate"
	TaskTypeCodeUnderstand TaskType = "code_understand"
	TaskTypeMemoryRecall   TaskType = "memory_recall"
	TaskTypeEvidenceAudit  TaskType = "evidence_audit"
	TaskTypeGeneral        TaskType = "general"
)

// NormalizeTaskType returns the canonical task type or TaskTypeGeneral.
func NormalizeTaskType(value string) TaskType {
	switch TaskType(value) {
	case TaskTypeCodeLocate, TaskTypeCodeUnderstand, TaskTypeMemoryRecall,
		TaskTypeEvidenceAudit, TaskTypeGeneral:
		return TaskType(value)
	default:
		return TaskTypeGeneral
	}
}

// ComposeOp defines how to merge partial results from subproblems.
type ComposeOp string

const (
	// ComposeUnion merges unique paths from all partials (deterministic, no LLM).
	ComposeUnion ComposeOp = "union"
	// ComposeRerank re-ranks merged candidates (1 LLM call).
	ComposeRerank ComposeOp = "rerank"
	// ComposeSynthesize merges partial answers into one (1 LLM call).
	ComposeSynthesize ComposeOp = "synthesize"
	// ComposeIntersection keeps what multiple sources agree on (deterministic, no LLM).
	ComposeIntersection ComposeOp = "intersection"
	// ComposeChronological sorts by recency (deterministic, no LLM).
	ComposeChronological ComposeOp = "chronological"
)

// compositionTable maps task type to composition operator.
var compositionTable = map[TaskType]ComposeOp{
	TaskTypeCodeLocate:     ComposeRerank,
	TaskTypeCodeUnderstand: ComposeSynthesize,
	TaskTypeMemoryRecall:   ComposeChronological,
	TaskTypeEvidenceAudit:  ComposeIntersection,
	TaskTypeGeneral:        ComposeSynthesize,
}

// searchToolByTaskType maps task type to the primary search tool for leaf execution.
var searchToolByTaskType = map[TaskType]string{
	TaskTypeCodeLocate:     "retrieve_code",
	TaskTypeCodeUnderstand: "retrieve_code",
	TaskTypeMemoryRecall:   "retrieve_memory",
	TaskTypeEvidenceAudit:  "retrieve_mixed",
	TaskTypeGeneral:        "retrieve_mixed",
}

// ComposeOpForTask returns the composition operator for a task type.
func ComposeOpForTask(tt TaskType) ComposeOp {
	if op, ok := compositionTable[tt]; ok {
		return op
	}
	return ComposeSynthesize
}

// SearchToolForTask returns the primary search tool name for a task type.
func SearchToolForTask(tt TaskType) string {
	if tool, ok := searchToolByTaskType[tt]; ok {
		return tool
	}
	return "retrieve_mixed"
}

// LambdaConfig holds tuning parameters for the lambda runner.
type LambdaConfig struct {
	// ContextBudget is the max items per leaf call (analogous to context window).
	// Default: 10.
	ContextBudget int

	// MaxK is the maximum branching factor cap.
	// Default: 8.
	MaxK int

	// CSearch is the relative cost of one search tool call.
	// Default: 1.0.
	CSearch float64

	// CInspect is the relative cost of one inspect/LLM call.
	// Default: 3.0.
	CInspect float64

	// PerPhaseAccuracy is the estimated accuracy of one phase (0-1).
	// Default: 0.6.
	PerPhaseAccuracy float64

	// AccuracyTarget is the minimum acceptable composite accuracy (0-1).
	// Default: 0.5.
	AccuracyTarget float64

	// LLM is the configuration for the LLM calls (classify + leaf + compose).
	LLM LLMConfig

	// EphemeralSkills enables a lambda branch that synthesizes and runs a
	// short-lived Go Solve helper instead of using retrieval tools.
	EphemeralSkills bool

	// ExtractSolutionLine finalizes from solution = ... helper output when present.
	ExtractSolutionLine bool

	// EphemeralSkillAttempts caps repair attempts for synthesized helpers.
	// Default: 3.
	EphemeralSkillAttempts int
}

// Defaults fills zero fields with sensible defaults.
func (c LambdaConfig) Defaults() LambdaConfig {
	if c.ContextBudget <= 0 {
		c.ContextBudget = 10
	}
	if c.MaxK <= 0 {
		c.MaxK = 8
	}
	if c.CSearch <= 0 {
		c.CSearch = 1.0
	}
	if c.CInspect <= 0 {
		c.CInspect = 3.0
	}
	if c.PerPhaseAccuracy <= 0 {
		c.PerPhaseAccuracy = 0.6
	}
	if c.AccuracyTarget <= 0 {
		c.AccuracyTarget = 0.5
	}
	if c.EphemeralSkillAttempts <= 0 {
		c.EphemeralSkillAttempts = 3
	}
	return c
}

// LambdaPlan holds the analytically computed decomposition plan.
type LambdaPlan struct {
	TaskType     TaskType  `json:"task_type"`
	ComposeOp    ComposeOp `json:"compose_op"`
	KStar        int       `json:"k_star"`
	TauStar      int       `json:"tau_star"`
	Depth        int       `json:"depth"`
	CostEstimate float64   `json:"cost_estimate"`
	N            int       `json:"n"`
}

// PlanLambda computes the optimal decomposition analytically.
//
// k* = ceil(sqrt(n * c_search / c_inspect))
// depth = ceil(log_{k*}(n / tau*))
//
// Accuracy constraint: perPhaseAccuracy^depth >= accuracyTarget.
// If unsatisfied, increase k* to reduce depth.
func PlanLambda(taskType TaskType, n int, cfg LambdaConfig) LambdaPlan {
	cfg = cfg.Defaults()
	composeOp := ComposeOpForTask(taskType)
	K := cfg.ContextBudget

	if n <= K {
		return LambdaPlan{
			TaskType:     taskType,
			ComposeOp:    composeOp,
			KStar:        1,
			TauStar:      n,
			Depth:        0,
			CostEstimate: float64(n)*cfg.CSearch + cfg.CInspect,
			N:            n,
		}
	}

	// k* = ceil(sqrt(n * c_search / c_inspect))
	kStar := int(math.Ceil(math.Sqrt(float64(n) * cfg.CSearch / cfg.CInspect)))
	kStar = clampInt(kStar, 2, cfg.MaxK)

	// depth = ceil(log_{k*}(n / K))
	depth := maxInt(1, int(math.Ceil(math.Log(float64(n)/float64(K))/math.Log(float64(kStar)))))

	// Accuracy constraint: increase k* until perPhaseAccuracy^depth >= accuracyTarget.
	maxK := minInt(cfg.MaxK, maxInt(2, n/K))
	for math.Pow(cfg.PerPhaseAccuracy, float64(depth)) < cfg.AccuracyTarget && kStar < maxK {
		kStar++
		depth = maxInt(1, int(math.Ceil(math.Log(float64(n)/float64(K))/math.Log(float64(kStar)))))
	}

	tauStar := minInt(K, maxInt(1, n/kStar))

	costEstimate := math.Pow(float64(kStar), float64(depth))*float64(tauStar)*cfg.CSearch +
		float64(depth)*cfg.CInspect*float64(kStar) +
		cfg.CSearch

	return LambdaPlan{
		TaskType:     taskType,
		ComposeOp:    composeOp,
		KStar:        kStar,
		TauStar:      tauStar,
		Depth:        depth,
		CostEstimate: costEstimate,
		N:            n,
	}
}

func capLambdaPlanForTask(plan LambdaPlan, task Task, cfg LambdaConfig) (LambdaPlan, map[string]any) {
	cfg = cfg.Defaults()
	caps := map[string]any{}
	kCapped := false
	if task.MaxSubcalls > 0 && plan.KStar > task.MaxSubcalls {
		plan.KStar = maxInt(1, task.MaxSubcalls)
		caps["lambda_k_star_capped_by_task"] = true
		kCapped = true
	}
	if task.MaxDepth > 0 && plan.Depth > task.MaxDepth {
		plan.Depth = task.MaxDepth
		caps["lambda_depth_capped_by_task"] = true
	}
	if plan.KStar <= 1 || plan.Depth <= 0 {
		plan.KStar = 1
		plan.Depth = 0
		if plan.N > 0 {
			plan.TauStar = minInt(cfg.ContextBudget, maxInt(1, plan.N))
		}
		return plan, caps
	}
	if kCapped && plan.N > 0 {
		plan.TauStar = minInt(cfg.ContextBudget, maxInt(1, ceilDivInt(plan.N, plan.KStar)))
	}
	return plan, caps
}

func ceilDivInt(n, d int) int {
	if d <= 0 {
		return n
	}
	if n <= 0 {
		return 0
	}
	return (n + d - 1) / d
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
