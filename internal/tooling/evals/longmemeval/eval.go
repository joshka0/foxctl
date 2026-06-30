// Package longmemeval evaluates longmem-style retrieval against an ingested
// named-memory workspace. It owns the bounded slice that covers dataset
// ingest, embedding-queue status, retrieval-only scoring, and answer-mode
// scoring through an injected AnswerRunner.
//
// Retrieval mode uses BM25 via memoryrecall.Search and never calls an
// embedder. Answer mode is wired through Deps.RunAnswer; the CLI supplies
// an RLM-backed runner in production and tests inject a fake. Anti-leakage
// is enforced at the build-plan layer by the existing BuildPlan/
// CheckLeakage helpers in this package.
package longmemeval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/intelligence/retrieval/memoryrecall"
	"github.com/joshka0/foxctl/internal/storage"
)

// Mode is a single bounded eval mode. The slice supports ingest, queue
// status, retrieval-only scoring, and answer-mode scoring through an
// injected AnswerRunner.
type Mode string

const (
	// ModeIngest runs BuildPlan+ApplyPlan against the configured workspace.
	ModeIngest Mode = "ingest"
	// ModeQueueStatus reports the embedding-queue Stats for kind=memory.
	// The alias ModeQueueCheck is accepted as a synonym.
	ModeQueueStatus Mode = "queue-status"
	// ModeQueueCheck is the legacy alias for ModeQueueStatus.
	ModeQueueCheck Mode = "queue-check"
	// ModeRetrieval runs BM25 retrieval via memoryrecall.Search and scores
	// hit@K + MRR per case.
	ModeRetrieval Mode = "retrieval"
	// ModeAnswer runs the injected AnswerRunner and scores normalized
	// non-refusal exact/answer-contains-expected match plus evidence-name hit. The alias
	// ModeAnswerMode is accepted as a synonym.
	ModeAnswer Mode = "answer"
	// ModeAnswerMode is the legacy alias for ModeAnswer.
	ModeAnswerMode Mode = "answer-mode"
)

// DefaultModes is the order Run uses when EvalOptions.Modes is empty.
var DefaultModes = []Mode{ModeIngest, ModeQueueStatus, ModeRetrieval}

// MemoryKind is the embedqueue task kind used for longmem memory jobs.
const MemoryKind = embedqueue.TaskKindMemory

// EvalOptions configures a single eval run.
type EvalOptions struct {
	// DatasetPath points at the longmem dataset JSON file (Case list).
	DatasetPath string
	// WorkspaceID is the canonical workspace identifier. Required for
	// ingest, retrieval, and queue-status modes.
	WorkspaceID string
	// Modes is the list of bounded modes to run. Empty defaults to all
	// supported modes in the order ingest, queue-status, retrieval.
	Modes []Mode
	// ArtifactDir optionally writes a report.json and per-case files.
	ArtifactDir string
	// Limit bounds the number of retrieved results per case.
	Limit int
	// EmbeddingModel records the model label embedded in queue jobs.
	EmbeddingModel string
	// SuiteName labels the run in the report (defaults to "longmem").
	SuiteName string
	// PurgeBeforeIngest deletes all longmem:// prefixed memories in the
	// workspace before ingesting. Use this when re-running eval with
	// different enrichment (atomization, turn digests, etc.) to avoid
	// stale records being skipped by dedup.
	PurgeBeforeIngest bool
}

// AnswerRequest is the package-level seam for answer-mode execution.
// Expected answers and expected evidence IDs intentionally stay in the scorer
// so injected/custom runners cannot overfit the eval request.
type AnswerRequest struct {
	WorkspaceID string
	CaseID      string
	Question    string
	Limit       int
}

// AnswerResult is the model/runtime output used by deterministic scoring.
type AnswerResult struct {
	Answer        string
	Method        string
	ToolNames     []string
	EvidenceNames []string
	EvidenceRefs  []string
	Iterations    int
	DurationMS    int64
}

// AnswerRunner executes one answer-mode case.
type AnswerRunner func(ctx context.Context, req AnswerRequest) (AnswerResult, error)

// CaseResult captures per-case retrieval evidence and metrics.
// Answer-mode fields are populated only when the run includes ModeAnswer.
type CaseResult struct {
	CaseID              string         `json:"case_id"`
	Question            string         `json:"question"`
	Method              string         `json:"method"`
	RetrievedNames      []string       `json:"retrieved_memory_names"`
	RetrievedScores     []float64      `json:"retrieved_scores"`
	RetrievedRanks      map[string]int `json:"retrieved_ranks"`
	ExpectedNames       []string       `json:"expected_evidence_memory_names"`
	ExpectedSessionIDs  []string       `json:"expected_evidence_session_ids"`
	MatchedNames        []string       `json:"matched_evidence_memory_names"`
	HitAt5              bool           `json:"hit_at_5"`
	HitAt10             bool           `json:"hit_at_10"`
	HitAt50             bool           `json:"hit_at_50"`
	HitAt100            bool           `json:"hit_at_100"`
	ReciprocalRank      float64        `json:"reciprocal_rank"`
	DurationMS          int64          `json:"duration_ms"`
	AntiLeakageFindings int            `json:"anti_leakage_findings"`
	Error               string         `json:"error,omitempty"`

	// Answer-mode fields (populated only when ModeAnswer runs).
	Answer                string   `json:"answer,omitempty"`
	ExpectedAnswer        string   `json:"expected_answer,omitempty"`
	AnswerMatched         bool     `json:"answer_matched,omitempty"`
	AnswerScore           float64  `json:"answer_score,omitempty"`
	AnswerMethod          string   `json:"answer_method,omitempty"`
	AnswerToolNames       []string `json:"answer_tool_names,omitempty"`
	AnswerEvidenceNames   []string `json:"answer_evidence_memory_names,omitempty"`
	AnswerEvidenceRefs    []string `json:"answer_evidence_refs,omitempty"`
	AnswerMatchedEvidence []string `json:"answer_matched_evidence_memory_names,omitempty"`
	AnswerIterations      int      `json:"answer_iterations,omitempty"`
	AnswerDurationMS      int64    `json:"answer_duration_ms,omitempty"`
	// JudgeAnswerScore is the raw LLM judge verdict (1.0=YES, 0.0=NO). It is
	// independent of the deterministic AnswerScore and is populated only when
	// an LLM judge is configured.
	AnswerJudgeScore  float64 `json:"answer_judge_score,omitempty"`
	AnswerJudgeReason string  `json:"answer_judge_reason,omitempty"`
}

// QueueStatus reports the memory embedding queue state for one workspace.
type QueueStatus struct {
	WorkspaceID string                `json:"workspace_id"`
	Kind        string                `json:"kind"`
	Stats       *embedding.QueueStats `json:"stats"`
	Note        string                `json:"note,omitempty"`
}

// IngestOutcome reports the BuildPlan/ApplyPlan summary for one ingest pass.
type IngestOutcome struct {
	Saved   int `json:"saved"`
	Queued  int `json:"queued"`
	Skipped int `json:"skipped"`
}

// Metrics summarises retrieval-only scoring across all cases. Answer-mode
// aggregates are populated only when ModeAnswer runs.
type Metrics struct {
	CaseCount     int     `json:"case_count"`
	FailureCount  int     `json:"failure_count"`
	HitAt5        float64 `json:"hit_at_5"`
	HitAt10       float64 `json:"hit_at_10"`
	HitAt50       float64 `json:"hit_at_50"`
	HitAt100      float64 `json:"hit_at_100"`
	MRR           float64 `json:"mrr"`
	MeanLatencyMS float64 `json:"mean_latency_ms"`
	// Answer-mode aggregates (only populated when answer mode runs).
	AnswerCaseCount      int     `json:"answer_case_count,omitempty"`
	AnswerFailureCount   int     `json:"answer_failure_count,omitempty"`
	AnswerMatchedCount   int     `json:"answer_matched_count,omitempty"`
	AnswerAccuracy       float64 `json:"answer_accuracy,omitempty"`
	AnswerMeanScore      float64 `json:"answer_mean_score,omitempty"`
	AnswerMeanLatencyMS  float64 `json:"answer_mean_latency_ms,omitempty"`
	AnswerJudgeCaseCount int     `json:"answer_judge_case_count,omitempty"`
	AnswerJudgeMatched   int     `json:"answer_judge_matched_count,omitempty"`
	AnswerJudgeAccuracy  float64 `json:"answer_judge_accuracy,omitempty"`
}

// RunResult is the top-level report emitted by Run.
type RunResult struct {
	Suite       string         `json:"suite"`
	WorkspaceID string         `json:"workspace_id"`
	DatasetPath string         `json:"dataset_path"`
	ArtifactDir string         `json:"artifact_dir,omitempty"`
	Limit       int            `json:"limit,omitempty"`
	GeneratedAt time.Time      `json:"generated_at"`
	Modes       []string       `json:"modes"`
	Cases       []CaseResult   `json:"cases,omitempty"`
	QueueStatus *QueueStatus   `json:"queue_status,omitempty"`
	Ingest      *IngestOutcome `json:"ingest,omitempty"`
	Metrics     *Metrics       `json:"metrics,omitempty"`
}

// Deps is the seam between the package logic and IO. Tests inject fakes;
// the CLI supplies real loaders.
type Deps struct {
	LoadCases    func(path string) ([]Case, error)
	BuildPlan    func(ctx context.Context, cases []Case, opts IngestOptions) (Plan, error)
	ApplyPlan    func(ctx context.Context, store storage.MemoryStore, queue *embedding.Store, plan Plan) (ApplyResult, error)
	OpenMemory   func(ctx context.Context, workspaceID string) (storage.MemoryStore, error)
	OpenQueue    func(ctx context.Context, workspaceID string) (*embedding.Store, error)
	SearchMemory func(ctx context.Context, store storage.MemoryStore, workspaceID, query string, limit int) (memoryrecall.QueryResponse, error)
	RunAnswer    AnswerRunner
	Now          func() time.Time
	MemoryName   func(workspaceID, caseID, sessionID string) string
	// PurgeBeforeIngest deletes existing longmem:// memories before
	// re-ingesting. Prevents stale records being skipped by dedup.
	PurgeBeforeIngest bool
	// JudgeAnswer, when set, is called as a secondary LLM binary judge.
	// It receives the model answer, expected answer, and original question.
	// It should return 1.0 (YES/semantically equivalent), 0.0 (NO), an
	// optional reason string, and an error if the judge could not produce a
	// verdict.
	JudgeAnswer func(ctx context.Context, question, answer, expected string) (score float64, reason string, err error)
}

// DefaultDeps returns Deps wired against the package's own loaders and the
// BM25-only path of memoryrecall.Search. The caller still supplies
// OpenMemory and OpenQueue for the underlying stores. For hybrid
// (vector+lexical) retrieval, use HybridDeps with an embedder.
func DefaultDeps(openMemory func(context.Context, string) (storage.MemoryStore, error), openQueue func(context.Context, string) (*embedding.Store, error)) Deps {
	return Deps{
		LoadCases: LoadCases,
		BuildPlan: func(ctx context.Context, cases []Case, opts IngestOptions) (Plan, error) {
			return BuildPlan(ctx, cases, opts)
		},
		ApplyPlan:  ApplyPlan,
		OpenMemory: openMemory,
		OpenQueue:  openQueue,
		SearchMemory: func(ctx context.Context, store storage.MemoryStore, workspaceID, query string, limit int) (memoryrecall.QueryResponse, error) {
			return memoryrecall.Search(ctx, store, memoryrecall.QueryRequest{
				Workspace: workspaceID,
				Query:     query,
				Limit:     limit,
			})
		},
		Now:        time.Now,
		MemoryName: memoryName,
	}
}

// HybridDeps returns Deps wired with a hybrid (vector+BM25) search path. The
// embedFn is called per query to produce a vector embedding; when it fails or
// returns empty, the search silently falls back to BM25-only. Use this in
// production eval runs to exercise the full retrieval pipeline.
func HybridDeps(
	openMemory func(context.Context, string) (storage.MemoryStore, error),
	openQueue func(context.Context, string) (*embedding.Store, error),
	embedFn func(ctx context.Context, query string) ([]float32, error),
) Deps {
	return Deps{
		LoadCases: LoadCases,
		BuildPlan: func(ctx context.Context, cases []Case, opts IngestOptions) (Plan, error) {
			return BuildPlan(ctx, cases, opts)
		},
		ApplyPlan:  ApplyPlan,
		OpenMemory: openMemory,
		OpenQueue:  openQueue,
		SearchMemory: func(ctx context.Context, store storage.MemoryStore, workspaceID, query string, limit int) (memoryrecall.QueryResponse, error) {
			var embedding []float32
			var embedErr error
			if embedFn != nil {
				embedding, embedErr = embedFn(ctx, query)
			}
			return memoryrecall.Search(ctx, store, memoryrecall.QueryRequest{
				Workspace:      workspaceID,
				Query:          query,
				QueryEmbedding: embedding,
				EmbeddingError: embedErr,
				Limit:          limit,
			})
		},
		Now:        time.Now,
		MemoryName: memoryName,
	}
}

// PlanLeakageByCase groups a BuildPlan's leakage findings by case ID. The
// eval uses this to attach anti-leakage counts to per-case artifacts.
func PlanLeakageByCase(plan Plan) map[string]int {
	counts := make(map[string]int)
	for _, finding := range plan.Leakage {
		caseID := strings.TrimSpace(finding.CaseID)
		if caseID == "" {
			continue
		}
		counts[caseID]++
	}
	return counts
}

// ExpectedMemoryNames returns the predicted memory names for a case's
// expected-evidence session IDs. The names are deterministic: they use the
// same hashing the ingest plan uses, so the eval can match retrieved
// results back to expected evidence without storing extra provenance.
func ExpectedMemoryNames(memoryNameFn func(workspaceID, caseID, sessionID string) string, workspaceID, caseID string, sessionIDs []string) []string {
	if memoryNameFn == nil {
		memoryNameFn = memoryName
	}
	out := make([]string, 0, len(sessionIDs))
	for _, sid := range sessionIDs {
		sid = strings.TrimSpace(sid)
		if sid == "" {
			continue
		}
		name := memoryNameFn(workspaceID, caseID, sid)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// NormalizeModes returns a deduplicated, lower-cased, order-preserving
// slice of valid modes. Unknown values produce an error. Empty input
// returns the default mode order.
func NormalizeModes(raw []string) ([]Mode, error) {
	if len(raw) == 0 {
		return append([]Mode(nil), DefaultModes...), nil
	}
	seen := make(map[Mode]struct{}, len(raw))
	out := make([]Mode, 0, len(raw))
	for _, value := range raw {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		mode := Mode(value)
		switch mode {
		case ModeIngest, ModeQueueStatus, ModeQueueCheck, ModeRetrieval, ModeAnswer, ModeAnswerMode:
		default:
			return nil, fmt.Errorf("unknown mode %q (supported: ingest, queue-status, queue-check, retrieval, answer, answer-mode)", value)
		}
		if mode == ModeQueueCheck {
			mode = ModeQueueStatus
		}
		if mode == ModeAnswerMode {
			mode = ModeAnswer
		}
		if _, ok := seen[mode]; ok {
			continue
		}
		seen[mode] = struct{}{}
		out = append(out, mode)
	}
	if len(out) == 0 {
		return append([]Mode(nil), DefaultModes...), nil
	}
	return out, nil
}

// Run executes the requested eval modes and returns a populated RunResult.
// The Deps seam lets callers substitute test doubles; missing fields fall
// back to package-level helpers via DefaultDeps.
func Run(ctx context.Context, opts EvalOptions, deps Deps) (RunResult, error) {
	workspaceID := strings.TrimSpace(opts.WorkspaceID)
	if workspaceID == "" {
		return RunResult{}, errors.New("workspace_id is required")
	}
	modes, err := NormalizeModes(toStringSlice(opts.Modes))
	if err != nil {
		return RunResult{}, err
	}
	suite := strings.TrimSpace(opts.SuiteName)
	if suite == "" {
		suite = "longmem"
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	result := RunResult{
		Suite:       suite,
		WorkspaceID: workspaceID,
		DatasetPath: strings.TrimSpace(opts.DatasetPath),
		ArtifactDir: strings.TrimSpace(opts.ArtifactDir),
		Limit:       limit,
		GeneratedAt: now().UTC(),
		Modes:       modeStrings(modes),
	}
	loadCases := deps.LoadCases
	if loadCases == nil {
		loadCases = LoadCases
	}
	buildPlan := deps.BuildPlan
	if buildPlan == nil {
		buildPlan = BuildPlan
	}
	applyPlan := deps.ApplyPlan
	if applyPlan == nil {
		applyPlan = ApplyPlan
	}
	memoryNameFn := deps.MemoryName
	if memoryNameFn == nil {
		memoryNameFn = memoryName
	}
	searchFn := deps.SearchMemory
	if searchFn == nil {
		searchFn = func(ctx context.Context, store storage.MemoryStore, workspaceID, query string, limit int) (memoryrecall.QueryResponse, error) {
			return memoryrecall.Search(ctx, store, memoryrecall.QueryRequest{Workspace: workspaceID, Query: query, Limit: limit})
		}
	}
	ingestDeps := deps
	ingestDeps.PurgeBeforeIngest = opts.PurgeBeforeIngest
	ingestDeps.ApplyPlan = applyPlan

	var (
		cases   []Case
		plan    Plan
		leakage map[string]int
	)
	needsDataset := false
	for _, mode := range modes {
		if mode == ModeIngest || mode == ModeRetrieval || mode == ModeAnswer {
			needsDataset = true
			break
		}
	}
	if needsDataset {
		if strings.TrimSpace(opts.DatasetPath) == "" {
			return result, errors.New("dataset is required for ingest, retrieval, and answer modes")
		}
		loaded, err := loadCases(opts.DatasetPath)
		if err != nil {
			return result, fmt.Errorf("load dataset: %w", err)
		}
		cases = loaded
		built, err := buildPlan(ctx, cases, IngestOptions{
			WorkspaceID:    workspaceID,
			EmbeddingModel: strings.TrimSpace(opts.EmbeddingModel),
		})
		if err != nil {
			return result, fmt.Errorf("build plan: %w", err)
		}
		plan = built
		leakage = PlanLeakageByCase(plan)
		if len(plan.Leakage) > 0 {
			return result, fmt.Errorf("%w: %d finding(s)", ErrLeakage, len(plan.Leakage))
		}
	}

	for _, mode := range modes {
		switch mode {
		case ModeIngest:
			outcome, err := runIngest(ctx, ingestDeps, plan)
			if err != nil {
				return result, err
			}
			result.Ingest = &outcome
		case ModeQueueStatus:
			status, err := runQueueStatus(ctx, deps, workspaceID)
			if err != nil {
				return result, err
			}
			result.QueueStatus = &status
		case ModeRetrieval:
			casesOut, err := runRetrieval(ctx, retrievalDeps{
				Store:       deps.OpenMemory,
				Search:      searchFn,
				Now:         now,
				MemoryName:  memoryNameFn,
				Leakage:     leakage,
				Limit:       limit,
				WorkspaceID: workspaceID,
			}, cases)
			if err != nil {
				return result, err
			}
			result.Cases = casesOut
			metrics := Summarize(casesOut)
			result.Metrics = &metrics
		case ModeAnswer:
			casesOut, err := runAnswer(ctx, answerDeps{
				Run:         deps.RunAnswer,
				JudgeAnswer: deps.JudgeAnswer,
				Now:         now,
				MemoryName:  memoryNameFn,
				Leakage:     leakage,
				Limit:       limit,
				WorkspaceID: workspaceID,
			}, cases)
			if err != nil {
				return result, err
			}
			result.Cases = mergeCaseResults(result.Cases, casesOut)
			metrics := Summarize(result.Cases)
			mergeAnswerMetrics(&metrics, casesOut)
			result.Metrics = &metrics
		}
	}
	return result, nil
}

func runIngest(ctx context.Context, deps Deps, plan Plan) (IngestOutcome, error) {
	if deps.OpenMemory == nil {
		return IngestOutcome{}, errors.New("memory store opener is required for ingest")
	}
	if deps.OpenQueue == nil {
		return IngestOutcome{}, errors.New("embedding queue opener is required for ingest")
	}
	if len(plan.Records) == 0 {
		return IngestOutcome{}, nil
	}
	workspaceID := plan.WorkspaceID
	store, err := deps.OpenMemory(ctx, workspaceID)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("open memory: %w", err)
	}
	defer func() { _ = store.Close() }()
	// Purge existing longmem:// memories before re-ingesting to avoid
	// stale records being skipped by dedup.
	if deps.PurgeBeforeIngest {
		if deleted, err := store.DeleteByNamePrefix(ctx, workspaceID, "longmem://"); err == nil {
			_ = deleted
		}
	}
	queue, err := deps.OpenQueue(ctx, workspaceID)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("open queue: %w", err)
	}
	defer func() { _ = queue.Close() }()
	applyResult, err := deps.ApplyPlan(ctx, store, queue, plan)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("apply plan: %w", err)
	}
	return IngestOutcome(applyResult), nil
}

func runQueueStatus(ctx context.Context, deps Deps, workspaceID string) (QueueStatus, error) {
	if deps.OpenQueue == nil {
		return QueueStatus{}, errors.New("embedding queue opener is required for queue-status")
	}
	queue, err := deps.OpenQueue(ctx, workspaceID)
	if err != nil {
		return QueueStatus{}, fmt.Errorf("open queue: %w", err)
	}
	defer func() { _ = queue.Close() }()
	stats, err := queue.StatsInWorkspaceKind(ctx, workspaceID, MemoryKind)
	if err != nil {
		return QueueStatus{}, fmt.Errorf("queue stats: %w", err)
	}
	return QueueStatus{
		WorkspaceID: workspaceID,
		Kind:        string(MemoryKind),
		Stats:       stats,
		Note:        "status only; the eval does not drain the queue. Run `foxctl agent run` or the embedding/worker skill to drain.",
	}, nil
}

func modeStrings(modes []Mode) []string {
	out := make([]string, 0, len(modes))
	for _, m := range modes {
		out = append(out, string(m))
	}
	return out
}

func toStringSlice(modes []Mode) []string {
	out := make([]string, 0, len(modes))
	for _, m := range modes {
		out = append(out, string(m))
	}
	return out
}

func caseExpectedSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			set[v] = true
		}
	}
	return set
}
