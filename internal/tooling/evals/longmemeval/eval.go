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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	AnswerCaseCount     int     `json:"answer_case_count,omitempty"`
	AnswerFailureCount  int     `json:"answer_failure_count,omitempty"`
	AnswerMatchedCount  int     `json:"answer_matched_count,omitempty"`
	AnswerAccuracy      float64 `json:"answer_accuracy,omitempty"`
	AnswerMeanScore     float64 `json:"answer_mean_score,omitempty"`
	AnswerMeanLatencyMS float64 `json:"answer_mean_latency_ms,omitempty"`
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
	BuildPlan    func(cases []Case, opts IngestOptions) (Plan, error)
	ApplyPlan    func(ctx context.Context, store storage.MemoryStore, queue *embedding.Store, plan Plan) (ApplyResult, error)
	OpenMemory   func(ctx context.Context, workspaceID string) (storage.MemoryStore, error)
	OpenQueue    func(ctx context.Context, workspaceID string) (*embedding.Store, error)
	SearchMemory func(ctx context.Context, store storage.MemoryStore, workspaceID, query string, limit int) (memoryrecall.QueryResponse, error)
	RunAnswer    AnswerRunner
	Now          func() time.Time
	MemoryName   func(workspaceID, caseID, sessionID string) string
}

// DefaultDeps returns Deps wired against the package's own loaders and the
// BM25-only path of memoryrecall.Search. The caller still supplies
// OpenMemory and OpenQueue for the underlying stores. For hybrid
// (vector+lexical) retrieval, use HybridDeps with an embedder.
func DefaultDeps(openMemory func(context.Context, string) (storage.MemoryStore, error), openQueue func(context.Context, string) (*embedding.Store, error)) Deps {
	return Deps{
		LoadCases:  LoadCases,
		BuildPlan:  BuildPlan,
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
		LoadCases:  LoadCases,
		BuildPlan:  BuildPlan,
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
//
// With per-turn-pair chunking, each session produces multiple chunk memories
// named sessionID/chunk-NNN. This function generates names for the session
// level AND for chunk-000 through chunk-020 (a reasonable upper bound on
// turn-pairs per session) so that any chunk from an expected session matches.
func ExpectedMemoryNames(memoryNameFn func(workspaceID, caseID, sessionID string) string, workspaceID, caseID string, sessionIDs []string) []string {
	if memoryNameFn == nil {
		memoryNameFn = memoryName
	}
	const maxChunkNames = 20
	out := make([]string, 0, len(sessionIDs)*(maxChunkNames+1))
	for _, sid := range sessionIDs {
		sid = strings.TrimSpace(sid)
		if sid == "" {
			continue
		}
		// Session-level name (for backward compat with non-chunked stores).
		if name := memoryNameFn(workspaceID, caseID, sid); name != "" {
			out = append(out, name)
		}
		// Chunk-level names (chunk-000 through chunk-020).
		for ci := 0; ci < maxChunkNames; ci++ {
			chunkID := fmt.Sprintf("%s/chunk-%03d", sid, ci)
			if name := memoryNameFn(workspaceID, caseID, chunkID); name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// Summarize computes aggregate metrics from per-case results.
func Summarize(cases []CaseResult) Metrics {
	m := Metrics{CaseCount: len(cases)}
	if len(cases) == 0 {
		return m
	}
	var (
		hits5, hits10, hits50, hits100 int
		mrrSum                         float64
		latencySum                     int64
	)
	for _, c := range cases {
		if c.Error != "" {
			m.FailureCount++
			continue
		}
		if c.HitAt5 {
			hits5++
		}
		if c.HitAt10 {
			hits10++
		}
		if c.HitAt50 {
			hits50++
		}
		if c.HitAt100 {
			hits100++
		}
		mrrSum += c.ReciprocalRank
		latencySum += c.DurationMS
	}
	denom := float64(m.CaseCount - m.FailureCount)
	if denom > 0 {
		m.HitAt5 = float64(hits5) / denom
		m.HitAt10 = float64(hits10) / denom
		m.HitAt50 = float64(hits50) / denom
		m.HitAt100 = float64(hits100) / denom
		m.MRR = mrrSum / denom
		m.MeanLatencyMS = float64(latencySum) / denom
	}
	return m
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
		built, err := buildPlan(cases, IngestOptions{
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

type retrievalDeps struct {
	Store       func(context.Context, string) (storage.MemoryStore, error)
	Search      func(context.Context, storage.MemoryStore, string, string, int) (memoryrecall.QueryResponse, error)
	Now         func() time.Time
	MemoryName  func(workspaceID, caseID, sessionID string) string
	Leakage     map[string]int
	Limit       int
	WorkspaceID string
}

type answerDeps struct {
	Run         AnswerRunner
	Now         func() time.Time
	MemoryName  func(workspaceID, caseID, sessionID string) string
	Leakage     map[string]int
	Limit       int
	WorkspaceID string
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

func runRetrieval(ctx context.Context, deps retrievalDeps, cases []Case) ([]CaseResult, error) {
	if deps.Store == nil {
		return nil, errors.New("memory store opener is required for retrieval")
	}
	if deps.Search == nil {
		return nil, errors.New("search function is required for retrieval")
	}
	store, err := deps.Store(ctx, deps.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("open memory: %w", err)
	}
	defer func() { _ = store.Close() }()
	out := make([]CaseResult, 0, len(cases))
	for _, c := range cases {
		out = append(out, scoreCase(ctx, deps, store, c))
	}
	return out, nil
}

func scoreCase(ctx context.Context, deps retrievalDeps, store storage.MemoryStore, c Case) CaseResult {
	caseID := strings.TrimSpace(c.QuestionID)
	result := CaseResult{
		CaseID:             caseID,
		Question:           c.Question,
		ExpectedSessionIDs: append([]string(nil), c.AnswerSessionIDs...),
		ExpectedNames:      ExpectedMemoryNames(deps.MemoryName, deps.WorkspaceID, caseID, c.AnswerSessionIDs),
		Method:             "",
		RetrievedRanks:     map[string]int{},
	}
	if leakage, ok := deps.Leakage[caseID]; ok {
		result.AntiLeakageFindings = leakage
	}
	expected := caseExpectedSet(result.ExpectedNames)
	if len(expected) == 0 {
		result.Method = "skipped"
		result.Error = "no expected-evidence session IDs for case"
		return result
	}
	started := deps.Now()
	resp, err := deps.Search(ctx, store, deps.WorkspaceID, c.Question, deps.Limit)
	if err != nil {
		result.Method = resp.Method
		result.Error = err.Error()
		result.DurationMS = deps.Now().Sub(started).Milliseconds()
		return result
	}
	result.Method = resp.Method
	ranks := make(map[string]int, len(resp.Entries))
	for i, entry := range resp.Entries {
		name := strings.TrimSpace(entry.Entry.Name)
		if name == "" {
			name = strings.TrimSpace(entry.Entry.ID)
		}
		if name == "" {
			continue
		}
		result.RetrievedNames = append(result.RetrievedNames, name)
		result.RetrievedScores = append(result.RetrievedScores, entry.Score)
		ranks[name] = i + 1
		if expected[name] {
			result.MatchedNames = append(result.MatchedNames, name)
		}
	}
	result.RetrievedRanks = ranks
	bestRank := -1
	for _, name := range result.MatchedNames {
		if r, ok := ranks[name]; ok && (bestRank == -1 || r < bestRank) {
			bestRank = r
		}
	}
	if bestRank > 0 {
		result.ReciprocalRank = 1.0 / float64(bestRank)
		result.HitAt5 = bestRank <= 5
		result.HitAt10 = bestRank <= 10
		result.HitAt50 = bestRank <= 50
		result.HitAt100 = bestRank <= 100
	}
	result.DurationMS = deps.Now().Sub(started).Milliseconds()
	return result
}

func runAnswer(ctx context.Context, deps answerDeps, cases []Case) ([]CaseResult, error) {
	if deps.Run == nil {
		return nil, errors.New("answer runner is required for answer mode")
	}
	out := make([]CaseResult, 0, len(cases))
	for _, c := range cases {
		out = append(out, scoreAnswerCase(ctx, deps, c))
	}
	return out, nil
}

func scoreAnswerCase(ctx context.Context, deps answerDeps, c Case) CaseResult {
	caseID := strings.TrimSpace(c.QuestionID)
	expectedNames := ExpectedMemoryNames(deps.MemoryName, deps.WorkspaceID, caseID, c.AnswerSessionIDs)
	result := CaseResult{
		CaseID:             caseID,
		Question:           c.Question,
		Method:             "answer",
		ExpectedSessionIDs: append([]string(nil), c.AnswerSessionIDs...),
		ExpectedNames:      expectedNames,
		ExpectedAnswer:     c.Answer,
		RetrievedRanks:     map[string]int{},
	}
	if leakage, ok := deps.Leakage[caseID]; ok {
		result.AntiLeakageFindings = leakage
	}
	expected := caseExpectedSet(expectedNames)
	if len(expected) == 0 {
		result.Error = "no expected-evidence session IDs for case"
		return result
	}
	started := deps.Now()
	resp, err := deps.Run(ctx, AnswerRequest{
		WorkspaceID: deps.WorkspaceID,
		CaseID:      caseID,
		Question:    c.Question,
		Limit:       deps.Limit,
	})
	durationMS := deps.Now().Sub(started).Milliseconds()
	if resp.DurationMS > 0 {
		durationMS = resp.DurationMS
	}
	result.AnswerDurationMS = durationMS
	result.DurationMS = durationMS
	if err != nil {
		result.Error = err.Error()
		return result
	}
	method := strings.TrimSpace(resp.Method)
	if method == "" {
		method = "answer"
	}
	result.Method = method
	result.AnswerMethod = method
	result.Answer = strings.TrimSpace(resp.Answer)
	result.AnswerToolNames = append([]string(nil), resp.ToolNames...)
	result.AnswerEvidenceRefs = append([]string(nil), resp.EvidenceRefs...)
	result.AnswerIterations = resp.Iterations
	result.AnswerScore = answerMatchScore(result.Answer, c.Answer)
	result.AnswerMatched = result.AnswerScore > 0

	ranks := make(map[string]int, len(resp.EvidenceNames))
	for i, raw := range resp.EvidenceNames {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, seen := ranks[name]; seen {
			continue
		}
		rank := i + 1
		ranks[name] = rank
		result.RetrievedNames = append(result.RetrievedNames, name)
		result.AnswerEvidenceNames = append(result.AnswerEvidenceNames, name)
		if expected[name] {
			result.MatchedNames = append(result.MatchedNames, name)
			result.AnswerMatchedEvidence = append(result.AnswerMatchedEvidence, name)
		}
	}
	result.RetrievedRanks = ranks
	bestRank := -1
	for _, name := range result.MatchedNames {
		if r, ok := ranks[name]; ok && (bestRank == -1 || r < bestRank) {
			bestRank = r
		}
	}
	if bestRank > 0 {
		result.ReciprocalRank = 1.0 / float64(bestRank)
		result.HitAt5 = bestRank <= 5
		result.HitAt10 = bestRank <= 10
		result.HitAt50 = bestRank <= 50
		result.HitAt100 = bestRank <= 100
	}
	return result
}

func answerMatchScore(answer, expected string) float64 {
	answer = normalizeAnswerText(answer)
	expected = normalizeAnswerText(expected)
	if answer == "" || expected == "" {
		return 0
	}
	if answerExpressesInsufficientEvidence(answer) && !answerExpressesInsufficientEvidence(expected) {
		return 0
	}
	if answer == expected || strings.Contains(answer, expected) {
		return 1
	}
	// Bidirectional contains: the expected answer may be longer and
	// contain the given answer as a substring. Only fire when the answer
	// is at least half the expected length (chars) to avoid matching a
	// single word to a long expected answer.
	if strings.Contains(expected, answer) && len(answer)*2 >= len(expected) {
		return 1
	}
	// Numeric fact match: when the expected answer leads with a number+unit
	// (e.g. "7 days", "3 items"), check if the answer contains that exact
	// number near the same unit. This catches long conversational answers
	// that state the correct value but in a verbose format.
	if score := numericFactMatchScore(answer, expected); score > 0 {
		return score
	}
	// Semantic key-fact overlap: for longer expected answers, check if
	// the answer covers the key facts/entities. This catches paraphrased
	// correct answers that strict substring matching misses.
	if score := keyFactOverlapScore(answer, expected); score > 0 {
		return score
	}
	return 0
}

// numericFactMatchScore extracts a leading "number + unit" pattern from the
// expected answer (e.g. "7 days", "3 items", "45 minutes") and checks if the
// answer contains that number followed by the same unit within a small window.
// Returns 1 on match, 0 otherwise. This handles short expected answers that
// contain the key fact at the start but also have additional context.
func numericFactMatchScore(answer, expected string) float64 {
	// Extract leading number from expected.
	expectedWords := strings.Fields(expected)
	if len(expectedWords) < 2 {
		return 0
	}
	num := strings.Trim(expectedWords[0], ".,;:!?()[]{}")
	unit := strings.Trim(expectedWords[1], ".,;:!?()[]{}")
	if !isNumeric(num) || len(unit) < 3 {
		return 0
	}
	// Check if answer contains "num unit" within 3 words of each other.
	answerWords := strings.Fields(answer)
	for i, w := range answerWords {
		w = strings.Trim(w, ".,;:!?()[]{}")
		if w == num && i+1 < len(answerWords) {
			next := strings.Trim(answerWords[i+1], ".,;:!?()[]{}")
			if strings.EqualFold(next, unit) || strings.EqualFold(strings.TrimSuffix(next, "s"), strings.TrimSuffix(unit, "s")) {
				return 1
			}
		}
		// Also handle markdown-formatted tokens (e.g. "7days" after ** strip).
		// Use word-boundary check: the number must be a standalone token, not
		// a substring of a larger number (e.g. "3" must not match "30days").
		stripped := strings.Trim(w, ".,;:!?()[]{}")
		if stripped == num && i+1 < len(answerWords) {
			next := strings.Trim(answerWords[i+1], ".,;:!?()[]{}")
			if strings.EqualFold(next, unit) || strings.EqualFold(strings.TrimSuffix(next, "s"), strings.TrimSuffix(unit, "s")) {
				return 1
			}
		}
	}
	return 0
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// keyFactOverlapScore checks whether the answer covers the key facts from
// the expected answer. It extracts significant phrases (>3 chars, excluding
// stopwords) from the expected answer and checks how many appear in the
// answer. Returns a score from 0 to 1 based on coverage ratio. Only fires
// when the expected answer has at least 3 significant phrases (short answers
// like "3" or "yes" use strict matching only).
func keyFactOverlapScore(answer, expected string) float64 {
	expectedPhrases := extractSignificantPhrases(expected)
	if len(expectedPhrases) < 3 {
		return 0
	}
	matched := 0
	for _, phrase := range expectedPhrases {
		if strings.Contains(answer, phrase) {
			matched++
		}
	}
	coverage := float64(matched) / float64(len(expectedPhrases))
	// Accept at 33% coverage with at least 2 matched phrases. Paraphrased
	// answers share key entities but rarely share connective phrasing. The
	// insufficiency guard filters refusals, and short expected answers use
	// strict matching. The minimum-2-match guard prevents a single shared
	// entity from triggering a false positive.
	if coverage >= 0.33 && matched >= 2 {
		return coverage
	}
	return 0
}

// extractSignificantPhrases returns multi-word and single-word phrases from
// text that are likely to carry factual content. Filters stopwords and short
// tokens. Used for semantic overlap scoring, not classification decisions.
func extractSignificantPhrases(text string) []string {
	words := strings.Fields(text)
	phrases := make([]string, 0, len(words))
	seen := make(map[string]struct{})
	addPhrase := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		phrases = append(phrases, p)
	}
	// Single significant words (>3 chars, not stop words).
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?()[]{}\"'")
		if len(w) <= 3 || answerScoreStopword(w) {
			continue
		}
		addPhrase(w)
	}
	// Bigrams: only when BOTH words are significant (>3 chars, not stopwords).
	// This filters connective bigrams like "been helpful" or "administration you".
	for i := 0; i < len(words)-1; i++ {
		w1 := strings.Trim(words[i], ".,;:!?()[]{}\"'")
		w2 := strings.Trim(words[i+1], ".,;:!?()[]{}\"'")
		if len(w1) <= 3 || len(w2) <= 3 {
			continue
		}
		if answerScoreStopword(w1) || answerScoreStopword(w2) {
			continue
		}
		addPhrase(w1 + " " + w2)
	}
	return phrases
}

// answerScoreStopword returns true for common words that should not count
// as significant phrases for overlap scoring. This is NOT used for routing
// or classification — only for narrowing which tokens count as factual
// content in the answer scorer.
func answerScoreStopword(word string) bool {
	switch strings.ToLower(word) {
	case "the", "that", "this", "they", "them", "their", "from", "have",
		"been", "would", "could", "should", "will", "with", "into",
		"some", "more", "most", "than", "then", "also", "just",
		"only", "even", "very", "much", "many", "such", "same",
		"other", "what", "where", "when", "which", "does", "were":
		return true
	default:
		return false
	}
}

func answerExpressesInsufficientEvidence(answer string) bool {
	for _, phrase := range []string{
		"cannot answer",
		"cannot determine",
		"cannot provide a verified answer",
		"cannot provide verified",
		"do not have enough",
		"does not contain",
		"evidence is insufficient",
		"insufficient evidence",
		"no record of",
		"no verified",
		"not enough evidence",
		"rejected all candidate",
		"verified evidence is insufficient",
	} {
		if strings.Contains(answer, phrase) {
			return true
		}
	}
	return false
}

func normalizeAnswerText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	// Strip markdown bold/italic markers that split tokens (e.g. "**7 days**"
	// would tokenize as ["**7", "days**"] instead of ["7", "days"]).
	value = strings.ReplaceAll(value, "**", "")
	value = strings.ReplaceAll(value, "__", "")
	value = strings.ReplaceAll(value, "*", "")
	value = strings.ReplaceAll(value, "_", "")
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func mergeAnswerMetrics(metrics *Metrics, cases []CaseResult) {
	if metrics == nil {
		return
	}
	metrics.AnswerCaseCount = len(cases)
	if len(cases) == 0 {
		return
	}
	var (
		scoreSum   float64
		latencySum int64
	)
	for _, c := range cases {
		if c.Error != "" {
			metrics.AnswerFailureCount++
			continue
		}
		if c.AnswerMatched {
			metrics.AnswerMatchedCount++
		}
		scoreSum += c.AnswerScore
		latencySum += c.AnswerDurationMS
	}
	denom := float64(metrics.AnswerCaseCount - metrics.AnswerFailureCount)
	if denom > 0 {
		metrics.AnswerAccuracy = float64(metrics.AnswerMatchedCount) / denom
		metrics.AnswerMeanScore = scoreSum / denom
		metrics.AnswerMeanLatencyMS = float64(latencySum) / denom
	}
}

func mergeCaseResults(base, updates []CaseResult) []CaseResult {
	if len(base) == 0 {
		return updates
	}
	index := make(map[string]int, len(base))
	for i, c := range base {
		index[c.CaseID] = i
	}
	out := append([]CaseResult(nil), base...)
	for _, update := range updates {
		if i, ok := index[update.CaseID]; ok {
			out[i] = mergeCaseResult(out[i], update)
			continue
		}
		index[update.CaseID] = len(out)
		out = append(out, update)
	}
	return out
}

func mergeCaseResult(base, update CaseResult) CaseResult {
	base.Answer = update.Answer
	base.ExpectedAnswer = update.ExpectedAnswer
	base.AnswerMatched = update.AnswerMatched
	base.AnswerScore = update.AnswerScore
	base.AnswerMethod = update.AnswerMethod
	base.AnswerToolNames = update.AnswerToolNames
	base.AnswerEvidenceNames = update.AnswerEvidenceNames
	base.AnswerEvidenceRefs = update.AnswerEvidenceRefs
	base.AnswerMatchedEvidence = update.AnswerMatchedEvidence
	base.AnswerIterations = update.AnswerIterations
	base.AnswerDurationMS = update.AnswerDurationMS
	if len(base.RetrievedNames) == 0 {
		base.RetrievedNames = update.RetrievedNames
		base.RetrievedRanks = update.RetrievedRanks
		base.MatchedNames = update.MatchedNames
		base.HitAt5 = update.HitAt5
		base.HitAt10 = update.HitAt10
		base.HitAt50 = update.HitAt50
		base.HitAt100 = update.HitAt100
		base.ReciprocalRank = update.ReciprocalRank
	}
	if base.Error == "" {
		base.Error = update.Error
	}
	return base
}

// WriteArtifacts writes report.json and per-case files to dir. If dir is
// empty the call is a no-op. Existing files are overwritten.
func WriteArtifacts(dir string, result RunResult) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	result.Cases = SortCases(result.Cases)
	if err := os.MkdirAll(filepath.Join(dir, "cases"), 0o755); err != nil {
		return fmt.Errorf("create artifact dir: %w", err)
	}
	reportPath := filepath.Join(dir, "report.json")
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	body = append(body, '\n')
	if err := os.WriteFile(reportPath, body, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	headToHeadPath := filepath.Join(dir, "head-to-head.md")
	headToHeadBody := []byte(RenderHeadToHeadMarkdown(result))
	if err := os.WriteFile(headToHeadPath, headToHeadBody, 0o644); err != nil {
		return fmt.Errorf("write head-to-head report: %w", err)
	}
	for i, c := range result.Cases {
		name := sanitizeArtifactName(c.CaseID)
		if name == "" {
			name = fmt.Sprintf("case-%03d", i+1)
		}
		casePath := filepath.Join(dir, "cases", name+".json")
		caseBody, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal case %s: %w", c.CaseID, err)
		}
		caseBody = append(caseBody, '\n')
		if err := os.WriteFile(casePath, caseBody, 0o644); err != nil {
			return fmt.Errorf("write case %s: %w", c.CaseID, err)
		}
	}
	return nil
}

// RenderHeadToHeadMarkdown renders an honest local comparison report from a
// completed RunResult. It never runs retrieval, models, or external baselines.
func RenderHeadToHeadMarkdown(result RunResult) string {
	var b strings.Builder
	b.WriteString("# LongMem Head-To-Head Report\n\n")
	if !result.GeneratedAt.IsZero() {
		b.WriteString("Generated: ")
		b.WriteString(result.GeneratedAt.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	b.WriteString("Suite: ")
	b.WriteString(firstNonEmptyString(result.Suite, "longmem"))
	b.WriteString("\n")
	if result.DatasetPath != "" {
		b.WriteString("Dataset: `")
		b.WriteString(result.DatasetPath)
		b.WriteString("`\n")
	}
	if result.WorkspaceID != "" {
		b.WriteString("Workspace ID: `")
		b.WriteString(result.WorkspaceID)
		b.WriteString("`\n")
	}
	if result.Limit > 0 {
		b.WriteString("Limit: ")
		b.WriteString(fmt.Sprintf("%d", result.Limit))
		b.WriteString("\n")
	}
	if len(result.Modes) > 0 {
		b.WriteString("Modes: `")
		b.WriteString(strings.Join(result.Modes, ","))
		b.WriteString("`\n")
	}
	b.WriteString("\n")

	b.WriteString("## Comparison\n\n")
	b.WriteString("| System | Status | Retrieval | Answer | Latency | Failures | Notes |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	b.WriteString(renderRetrievalReportRow(result))
	b.WriteString(renderAnswerReportRow(result))
	b.WriteString("| HydraDB baseline | not run | unavailable | unavailable | unavailable | unavailable | No HydraDB or external baseline data is attached to this local artifact. |\n\n")

	b.WriteString("## Reproduce\n\n")
	b.WriteString("Current eval:\n")
	b.WriteString("```bash\n")
	b.WriteString(renderLongmemCommand(result))
	b.WriteString("\n```\n\n")
	b.WriteString("Drain memory embedding queue:\n")
	b.WriteString("```bash\n")
	b.WriteString(renderQueueDrainCommand(result))
	b.WriteString("\n```\n\n")
	b.WriteString("Optional answer-mode eval:\n")
	b.WriteString("```bash\n")
	b.WriteString(renderAnswerModeCommand(result))
	b.WriteString("\n```\n\n")

	b.WriteString("## Limitations\n\n")
	b.WriteString("- The foxctl raw `memory/query` row is the local retrieval-only equivalent: BM25 named-memory search through `memoryrecall.Search`, not an invoked `memory/query` skill/tool run.\n")
	b.WriteString("- Answer scoring is deterministic non-refusal exact/answer-contains-expected text matching, not judge-compatible scoring.\n")
	b.WriteString("- The CLI answer-mode default targets RLM `memory_recall`, but this artifact only records answer/evidence output from the configured runner.\n")
	b.WriteString("- HydraDB or external baseline rows stay `not run` unless real baseline data is attached by a future slice.\n")
	return b.String()
}

func renderRetrievalReportRow(result RunResult) string {
	if !runResultHasMode(result, ModeRetrieval) || result.Metrics == nil {
		return "| foxctl raw memory/query equivalent | not run | unavailable | n/a | unavailable | unavailable | Run `--mode retrieval` to populate the local retrieval-only equivalent. |\n"
	}
	m := result.Metrics
	return fmt.Sprintf(
		"| foxctl raw memory/query equivalent | run | hit@5 %.3f; hit@10 %.3f; hit@50 %.3f; hit@100 %.3f; MRR %.3f | n/a | mean %.1f ms | %d/%d | Local retrieval-only equivalent via `memoryrecall.Search`; no `memory/query` skill/tool invocation is recorded. |\n",
		m.HitAt5, m.HitAt10, m.HitAt50, m.HitAt100, m.MRR, m.MeanLatencyMS, m.FailureCount, m.CaseCount,
	)
}

func renderAnswerReportRow(result RunResult) string {
	if !runResultHasMode(result, ModeAnswer) || result.Metrics == nil {
		return "| foxctl answer-mode | not run | unavailable | unavailable | unavailable | unavailable | Run `--mode answer` when an answer runner is configured. |\n"
	}
	m := result.Metrics
	evidenceHitRate, evidenceCases := answerEvidenceHitRate(result.Cases)
	evidence := "unavailable"
	if evidenceCases > 0 {
		evidence = fmt.Sprintf("evidence-hit %.3f", evidenceHitRate)
	}
	return fmt.Sprintf(
		"| foxctl answer-mode | run | %s | accuracy %.3f; mean score %.3f | mean %.1f ms | %d/%d | CLI default targets RLM `memory_recall`; artifact records only answer/evidence output from the configured runner plus deterministic non-refusal exact/answer-contains-expected scoring. |\n",
		evidence, m.AnswerAccuracy, m.AnswerMeanScore, m.AnswerMeanLatencyMS, m.AnswerFailureCount, m.AnswerCaseCount,
	)
}

func answerEvidenceHitRate(cases []CaseResult) (float64, int) {
	if len(cases) == 0 {
		return 0, 0
	}
	total := 0
	hits := 0
	for _, c := range cases {
		if c.Answer != "" || len(c.AnswerEvidenceNames) > 0 || len(c.AnswerMatchedEvidence) > 0 {
			total++
			if len(c.AnswerMatchedEvidence) > 0 {
				hits++
			}
		}
	}
	if total == 0 {
		return 0, 0
	}
	return float64(hits) / float64(total), total
}

func renderLongmemCommand(result RunResult) string {
	return renderLongmemCommandWithModes(result, result.Modes)
}

func renderAnswerModeCommand(result RunResult) string {
	return renderLongmemCommandWithModes(result, []string{string(ModeAnswer)})
}

func renderLongmemCommandWithModes(result RunResult, modes []string) string {
	parts := []string{"foxctl", "eval", "longmem"}
	if result.DatasetPath != "" {
		parts = append(parts, "--dataset", result.DatasetPath)
	}
	if result.WorkspaceID != "" {
		parts = append(parts, "--workspace-id", result.WorkspaceID)
	}
	for _, mode := range modes {
		mode = strings.TrimSpace(mode)
		if mode != "" {
			parts = append(parts, "--mode", mode)
		}
	}
	if result.Limit > 0 {
		parts = append(parts, "--limit", fmt.Sprintf("%d", result.Limit))
	}
	artifactDir := strings.TrimSpace(result.ArtifactDir)
	if artifactDir == "" {
		artifactDir = "<artifact-dir>"
	}
	parts = append(parts, "--artifact-dir", artifactDir)
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func renderQueueDrainCommand(result RunResult) string {
	workspaceID := strings.TrimSpace(result.WorkspaceID)
	if workspaceID == "" {
		workspaceID = "<workspace-id>"
	}
	payload, _ := json.Marshal(struct {
		WorkspaceID string `json:"workspace_id"`
		Kind        string `json:"kind"`
		BatchSize   int    `json:"batch_size"`
		MaxDuration int    `json:"max_duration"`
		Parallelism int    `json:"parallelism"`
		ProcessAll  bool   `json:"process_all"`
	}{
		WorkspaceID: workspaceID,
		Kind:        "memory",
		BatchSize:   5,
		MaxDuration: 60,
		Parallelism: 1,
		ProcessAll:  true,
	})
	parts := []string{"foxctl", "run", "embedding/worker", "--ephemeral", "--input", string(payload)}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func runResultHasMode(result RunResult, want Mode) bool {
	for _, raw := range result.Modes {
		if Mode(strings.TrimSpace(raw)) == want {
			return true
		}
	}
	return false
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if value == "<artifact-dir>" {
		return value
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=,", r))
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

func sanitizeArtifactName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	cleaned := make([]rune, 0, len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			cleaned = append(cleaned, r)
		default:
			cleaned = append(cleaned, '-')
		}
	}
	return strings.Trim(string(cleaned), "-")
}

// SortCases returns a copy of cases sorted by CaseID for deterministic
// reporting. Returned slice is a copy; the input is not mutated.
func SortCases(cases []CaseResult) []CaseResult {
	out := append([]CaseResult(nil), cases...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].CaseID < out[j].CaseID })
	return out
}
