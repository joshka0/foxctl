package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/platform/config"
	workspaceutil "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/rlm"
	rlmenv "github.com/joshka0/foxctl/internal/rlm/env"
	"github.com/joshka0/foxctl/internal/storage"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/tooling/evals/longmemeval"
	"github.com/spf13/cobra"
)

func newEvalLongmemCommand() *cobra.Command {
	return newEvalLongmemCommandWithDeps(longmemeval.Deps{}, defaultEvalMemoryOpener(), defaultEvalQueueOpener())
}

func defaultEvalMemoryOpener() func(context.Context, string) (storage.MemoryStore, error) {
	return func(ctx context.Context, _ string) (storage.MemoryStore, error) {
		cfg, err := loadConfig(ctx)
		if err != nil {
			return nil, err
		}
		return openEvalMemoryStore(ctx, cfg)
	}
}

func defaultEvalQueueOpener() func(context.Context, string) (*embedding.Store, error) {
	return func(ctx context.Context, _ string) (*embedding.Store, error) {
		cfg, err := loadConfig(ctx)
		if err != nil {
			return nil, err
		}
		return openEvalQueueStore(ctx, cfg)
	}
}

func newEvalLongmemCommandWithDeps(deps longmemeval.Deps, openMemory func(context.Context, string) (storage.MemoryStore, error), openQueue func(context.Context, string) (*embedding.Store, error)) *cobra.Command {
	var (
		datasetPath    string
		workspaceID    string
		workspacePath  string
		modes          []string
		artifactDir    string
		limit          int
		embeddingModel string
		suiteName      string
		answerProvider string
		answerModel    string
		answerBaseURL  string
		answerAPIKey   string
		answerTimeout  time.Duration
		answerMaxIters int
		answerRoute    string
		answerPlanMode string
		answerTools    string
		answerStrategy string
	)

	cmd := &cobra.Command{
		Use:   "longmem",
		Short: "Run a bounded longmem eval: ingest, queue-status, retrieval, and answer scoring",
		Long: strings.TrimSpace(`Evaluate longmem-style retrieval against an ingested named-memory workspace.

The command covers four bounded modes:
  ingest        - BuildPlan+ApplyPlan against the configured workspace
  queue-status  - Embedding queue Stats for kind=memory (no worker drain)
  retrieval     - BM25 retrieval-only scoring (hit@5/10/50/100, MRR, latency)
  answer        - RLM answer-mode scoring via the memory_recall route

When --mode is omitted, the command runs ingest, queue-status, and retrieval.
Answer mode is explicit because it calls the configured RLM model.

The retrieval mode uses memoryrecall.Search with no query embedding, so it
never calls an embedder or an LLM. Answer mode calls the configured RLM model
and exposes only the memory-recall tool profile; package tests inject a fake
runner and do not call external services.

Anti-leakage is enforced at the build-plan layer by the longmemeval
package: the answer, evidence labels, and case IDs are never embedded
into the workspace memories.
`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			workspace, err := resolveLongmemWorkspaceID(cfg, workspacePath, workspaceID)
			if err != nil {
				return err
			}
			modeList, err := longmemeval.NormalizeModes(splitEvalModes(modes))
			if err != nil {
				return err
			}
			if limit <= 0 {
				limit = 10
			}
			resolvedDeps := deps
			if resolvedDeps.OpenMemory == nil {
				resolvedDeps.OpenMemory = openMemory
			}
			if resolvedDeps.OpenQueue == nil {
				resolvedDeps.OpenQueue = openQueue
			}
			if resolvedDeps.Now == nil {
				resolvedDeps.Now = time.Now
			}
			if resolvedDeps.RunAnswer == nil && longmemModesInclude(modeList, longmemeval.ModeAnswer) {
				answerRuntime, err := resolveLongmemAnswerRuntime(longmemAnswerRuntimeInput{
					Strategy:            answerStrategy,
					RouteProfile:        answerRoute,
					PlanMode:            answerPlanMode,
					ToolProfile:         answerTools,
					RouteProfileChanged: cmd.Flags().Changed("answer-route"),
					PlanModeChanged:     cmd.Flags().Changed("answer-plan-mode"),
					ToolProfileChanged:  cmd.Flags().Changed("answer-tool-profile"),
				})
				if err != nil {
					return err
				}
				workspaceRoot := strings.TrimSpace(workspacePath)
				if workspaceRoot == "" {
					workspaceRoot = strings.TrimSpace(cfg.Storage.Root)
				}
				if workspaceRoot == "" {
					workspaceRoot = workspace
				}
				resolvedDeps.RunAnswer = newLongmemRLMAnswerRunner(longmemRLMAnswerRunnerConfig{
					AppConfig:     cfg,
					WorkspaceRoot: workspaceRoot,
					Provider:      answerProvider,
					Model:         answerModel,
					BaseURL:       answerBaseURL,
					APIKey:        answerAPIKey,
					Timeout:       answerTimeout,
					MaxIterations: answerMaxIters,
					RouteProfile:  answerRuntime.RouteProfile,
					PlanMode:      answerRuntime.PlanMode,
					ToolProfile:   answerRuntime.ToolProfile,
					Strategy:      answerRuntime.Strategy,
				})
			}
			result, err := longmemeval.Run(ctx, longmemeval.EvalOptions{
				DatasetPath:    datasetPath,
				WorkspaceID:    workspace,
				Modes:          modeList,
				ArtifactDir:    artifactDir,
				Limit:          limit,
				EmbeddingModel: embeddingModel,
				SuiteName:      suiteName,
			}, resolvedDeps)
			if err != nil {
				return err
			}
			if err := longmemeval.WriteArtifacts(artifactDir, result); err != nil {
				return err
			}
			data := map[string]any{
				"suite":        result.Suite,
				"workspace_id": result.WorkspaceID,
				"dataset_path": result.DatasetPath,
				"modes":        result.Modes,
				"limit":        limit,
				"generated_at": result.GeneratedAt.Format(time.RFC3339Nano),
				"result":       result,
			}
			if strings.TrimSpace(artifactDir) != "" {
				reportPath := filepath.Join(artifactDir, "report.json")
				data["artifact_dir"] = artifactDir
				data["report_path"] = reportPath
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "eval/longmem", data, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().StringVar(&datasetPath, "dataset", "", "Path to the longmem dataset JSON (Case list)")
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Explicit canonical workspace ID (overrides --workspace)")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path used to derive the canonical ID")
	cmd.Flags().StringSliceVar(&modes, "mode", nil, "Eval modes: ingest, queue-status, queue-check, retrieval, answer (repeatable or comma-separated; default: ingest,queue-status,retrieval)")
	cmd.Flags().StringVar(&artifactDir, "artifact-dir", "", "Write report.json and per-case files under this directory")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum retrieved memories per case")
	cmd.Flags().StringVar(&embeddingModel, "embedding-model", "text-embedding-qwen3-embedding-8b", "Embedding model label recorded in queue jobs")
	cmd.Flags().StringVar(&suiteName, "suite", "longmem", "Suite label embedded in the report")
	cmd.Flags().StringVar(&answerProvider, "answer-provider", "", "Answer-mode LLM provider, e.g. lmstudio, openai_compat, or anthropic_compat (defaults to FOXCTL_RLM_LLM_PROVIDER or lmstudio)")
	cmd.Flags().StringVar(&answerModel, "answer-model", "", "Answer-mode LLM model (defaults to FOXCTL_RLM_LLM_MODEL or LMSTUDIO_MODEL)")
	cmd.Flags().StringVar(&answerBaseURL, "answer-base-url", "", "Answer-mode LLM base URL (defaults to FOXCTL_RLM_LLM_BASE_URL or LMSTUDIO_BASE_URL)")
	cmd.Flags().StringVar(&answerAPIKey, "answer-api-key", "", "Answer-mode LLM API key (defaults to FOXCTL_RLM_LLM_API_KEY or LMSTUDIO_API_KEY)")
	cmd.Flags().DurationVar(&answerTimeout, "answer-timeout", 90*time.Second, "Answer-mode timeout per case")
	cmd.Flags().IntVar(&answerMaxIters, "answer-max-iterations", 5, "Answer-mode max model/tool iterations per case")
	cmd.Flags().StringVar(&answerRoute, "answer-route", "", "Answer-mode RLM route profile (defaults by --answer-strategy)")
	cmd.Flags().StringVar(&answerPlanMode, "answer-plan-mode", "", "Answer-mode RLM plan mode (defaults by --answer-strategy)")
	cmd.Flags().StringVar(&answerTools, "answer-tool-profile", "", "Answer-mode RLM tool profile (defaults by --answer-strategy)")
	cmd.Flags().StringVar(&answerStrategy, "answer-strategy", string(longmemAnswerStrategyRetrieveMemory), "Answer strategy preset: retrieve-memory, gather-memory, gather-mixed, full-debug")
	return cmd
}

func splitEvalModes(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		for _, piece := range strings.Split(value, ",") {
			piece = strings.TrimSpace(piece)
			if piece != "" {
				out = append(out, piece)
			}
		}
	}
	return out
}

func resolveLongmemWorkspaceID(cfg config.Config, workspacePath, workspaceID string) (string, error) {
	if id := strings.TrimSpace(workspaceID); id != "" {
		return id, nil
	}
	if path := strings.TrimSpace(workspacePath); path != "" {
		return workspaceutil.CanonicalID(path), nil
	}
	if root := strings.TrimSpace(cfg.Storage.Root); root != "" {
		return workspaceutil.CanonicalID(root), nil
	}
	return "", fmt.Errorf("workspace_id (or --workspace path) is required")
}

func openEvalMemoryStore(ctx context.Context, cfg config.Config) (storage.MemoryStore, error) {
	return memorystore.OpenWithConfig(ctx, cfg)
}

func openEvalQueueStore(ctx context.Context, cfg config.Config) (*embedding.Store, error) {
	return embedding.OpenStoreFromConfig(ctx, cfg)
}

func longmemModesInclude(modes []longmemeval.Mode, want longmemeval.Mode) bool {
	for _, mode := range modes {
		if mode == want {
			return true
		}
	}
	return false
}

type longmemRLMAnswerRunnerConfig struct {
	AppConfig     config.Config
	WorkspaceRoot string
	Provider      string
	Model         string
	BaseURL       string
	APIKey        string
	Timeout       time.Duration
	MaxIterations int
	RouteProfile  string
	PlanMode      string
	ToolProfile   string
	Strategy      longmemAnswerStrategy
}

func newLongmemRLMAnswerRunner(in longmemRLMAnswerRunnerConfig) longmemeval.AnswerRunner {
	return func(ctx context.Context, req longmemeval.AnswerRequest) (longmemeval.AnswerResult, error) {
		strategy := in.Strategy
		if strategy == "" {
			strategy = longmemAnswerStrategyRetrieveMemory
		}
		timeout := in.Timeout
		if timeout <= 0 {
			timeout = 90 * time.Second
		}
		maxIterations := in.MaxIterations
		if maxIterations <= 0 {
			maxIterations = 5
		}
		toolProfile := strings.TrimSpace(in.ToolProfile)
		if toolProfile == "" {
			toolProfile = string(strategy.defaultToolProfile())
		}
		routeProfile := strings.TrimSpace(in.RouteProfile)
		if routeProfile == "" {
			routeProfile = string(strategy.defaultRouteProfile())
		}
		planMode := strings.TrimSpace(in.PlanMode)
		if planMode == "" {
			planMode = string(strategy.defaultPlanMode())
		}
		workspaceRoot := strings.TrimSpace(in.WorkspaceRoot)
		if workspaceRoot == "" {
			workspaceRoot = req.WorkspaceID
		}

		runCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		companionDB, companionClose, err := openRLMCompanionDB(runCtx, in.AppConfig)
		if err != nil {
			return longmemeval.AnswerResult{}, err
		}
		if companionClose != nil {
			defer func() { _ = companionClose() }()
		}

		prompt := longmemAnswerPrompt(req.Question, strategy)
		task := rlm.Task{
			Prompt:        prompt,
			WorkspaceRoot: workspaceRoot,
			WorkspaceID:   req.WorkspaceID,
			MaxIterations: maxIterations,
			Metadata: map[string]any{
				"longmem_case_id":         strings.TrimSpace(req.CaseID),
				"longmem_limit":           req.Limit,
				"longmem_answer_strategy": string(strategy),
			},
		}
		bootstrapper := rlmenv.NewBootstrapper(rlmenv.BootstrapConfig{
			AppConfig:   in.AppConfig,
			CompanionDB: companionDB,
		})
		env, err := bootstrapper.Build(runCtx, task)
		if err != nil {
			return longmemeval.AnswerResult{}, err
		}
		defer func() { _ = bootstrapper.Close() }()
		env.Tools = rlmenv.FilterTools(env.Tools, toolProfile)

		adapter := rlmenv.NewReadOnlyAdapter(in.AppConfig, workspaceRoot, "", companionDB, env)
		adapter.SetWorkspaceID(req.WorkspaceID)
		adapter.SetContextEngineStore(bootstrapper.ContextEngineStore())
		adapter.SetTaskStore(bootstrapper.TaskStore())
		recorder := &longmemAnswerToolRecorder{next: adapter}

		llmCfg := buildRLMCLIConfig(rlmCLIConfigInput{
			Task:        task,
			Provider:    in.Provider,
			Model:       in.Model,
			BaseURL:     in.BaseURL,
			APIKey:      in.APIKey,
			Timeout:     timeout,
			ToolProfile: toolProfile,
		})
		llmCfg.MaxIterations = maxIterations
		llmCfg.RequireToolUse = len(env.Tools) > 0
		llmCfg.RouteProfile = rlm.NormalizeRouteProfile(routeProfile)
		llmCfg.PlanMode = rlm.NormalizePlanMode(planMode)
		// Force near-deterministic sampling for eval reproducibility.
		// Temperature 0 is the Go zero value and would be skipped by the
		// runner's "not set" check, so use a tiny non-zero value.
		llmCfg.Temperature = 0.01
		runner := rlm.LLMRunner{
			Config: llmCfg,
			Tools:  recorder,
		}

		started := time.Now()
		rlmResult, err := runner.Run(runCtx, task, env)
		out := longmemeval.AnswerResult{
			Method:        "rlm-answer",
			DurationMS:    time.Since(started).Milliseconds(),
			EvidenceNames: recorder.evidenceNames(),
			EvidenceRefs:  recorder.evidenceRefs(),
		}
		if err != nil {
			return out, err
		}
		out.Answer = strings.TrimSpace(rlmResult.Answer)
		out.Iterations = rlmResult.Iterations
		out.ToolNames = evalStringsFromAnySlice(rlmResult.Metadata["tool_names"])
		if len(out.ToolNames) == 0 {
			out.ToolNames = collectRLMPhaseToolNames(rlmResult.Metadata)
		}
		out.EvidenceRefs = uniqueEvalStrings(append(out.EvidenceRefs, rlmResult.EvidenceRefs...))
		return out, nil
	}
}

type longmemAnswerStrategy string

const (
	longmemAnswerStrategyRetrieveMemory longmemAnswerStrategy = "retrieve-memory"
	longmemAnswerStrategyGatherMemory   longmemAnswerStrategy = "gather-memory"
	longmemAnswerStrategyGatherMixed    longmemAnswerStrategy = "gather-mixed"
	longmemAnswerStrategyFullDebug      longmemAnswerStrategy = "full-debug"
)

type longmemAnswerRuntimeInput struct {
	Strategy            string
	RouteProfile        string
	PlanMode            string
	ToolProfile         string
	RouteProfileChanged bool
	PlanModeChanged     bool
	ToolProfileChanged  bool
}

type longmemAnswerRuntime struct {
	Strategy     longmemAnswerStrategy
	RouteProfile string
	PlanMode     string
	ToolProfile  string
}

func resolveLongmemAnswerRuntime(in longmemAnswerRuntimeInput) (longmemAnswerRuntime, error) {
	strategy, err := normalizeLongmemAnswerStrategy(in.Strategy)
	if err != nil {
		return longmemAnswerRuntime{}, err
	}
	out := longmemAnswerRuntime{
		Strategy:     strategy,
		RouteProfile: string(strategy.defaultRouteProfile()),
		PlanMode:     string(strategy.defaultPlanMode()),
		ToolProfile:  string(strategy.defaultToolProfile()),
	}
	if in.RouteProfileChanged || strings.TrimSpace(in.RouteProfile) != "" {
		out.RouteProfile = strings.TrimSpace(in.RouteProfile)
	}
	if in.PlanModeChanged || strings.TrimSpace(in.PlanMode) != "" {
		out.PlanMode = strings.TrimSpace(in.PlanMode)
	}
	if in.ToolProfileChanged || strings.TrimSpace(in.ToolProfile) != "" {
		out.ToolProfile = strings.TrimSpace(in.ToolProfile)
	}
	if out.RouteProfile == "" {
		out.RouteProfile = string(strategy.defaultRouteProfile())
	}
	if out.PlanMode == "" {
		out.PlanMode = string(strategy.defaultPlanMode())
	}
	if out.ToolProfile == "" {
		out.ToolProfile = string(strategy.defaultToolProfile())
	}
	return out, nil
}

func normalizeLongmemAnswerStrategy(value string) (longmemAnswerStrategy, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(longmemAnswerStrategyRetrieveMemory), "memory", "retrieve_memory":
		return longmemAnswerStrategyRetrieveMemory, nil
	case string(longmemAnswerStrategyGatherMemory), "gather_memory":
		return longmemAnswerStrategyGatherMemory, nil
	case string(longmemAnswerStrategyGatherMixed), "gather_mixed":
		return longmemAnswerStrategyGatherMixed, nil
	case string(longmemAnswerStrategyFullDebug), "full_debug":
		return longmemAnswerStrategyFullDebug, nil
	default:
		return "", fmt.Errorf("unknown longmem answer strategy %q", value)
	}
}

func (s longmemAnswerStrategy) defaultRouteProfile() rlm.RouteProfile {
	switch s {
	case longmemAnswerStrategyGatherMixed, longmemAnswerStrategyFullDebug:
		return rlm.RouteProfileMixed
	default:
		return rlm.RouteProfileMemoryRecall
	}
}

func (s longmemAnswerStrategy) defaultPlanMode() rlm.PlanMode {
	return rlm.PlanModeFree
}

func (s longmemAnswerStrategy) defaultToolProfile() rlm.ToolProfile {
	switch s {
	case longmemAnswerStrategyGatherMemory:
		return rlm.ToolProfileGatherContext
	case longmemAnswerStrategyGatherMixed:
		return rlm.ToolProfileMemoryContext
	case longmemAnswerStrategyFullDebug:
		return rlm.ToolProfileFullDebug
	default:
		return rlm.ToolProfileMemoryRecall
	}
}

func longmemAnswerPrompt(question string, strategy longmemAnswerStrategy) string {
	question = strings.TrimSpace(question)
	switch strategy {
	case longmemAnswerStrategyGatherMemory:
		return strings.TrimSpace(`Answer the LongMem recall question using the available context tools.
Start with gather_memory_context using the question as the query. Read evidence_digest claims and slots before loading refs. Then call aggregate_evidence_refs on the smallest candidate ref set from evidence_digest.load_refs or path_set, followed by evidence_ledger on that same small ref set. Answer only from evidence_ledger accepted_rows. If evidence_ledger reports needs_fallback=true, use gather_memory_context once more with the best fallback query or narrower required_evidence from the missing answer slot, then rebuild the ledger. Load only refs the ledger leaves ambiguous, using max_tokens around 1200 for verification. Do not mention tool internals.

Question: ` + question)
	case longmemAnswerStrategyGatherMixed:
		return strings.TrimSpace(`Answer the LongMem recall question using the available memory and ContextWiki tools.
Start with plan_context_query using the question, lanes=["memory","context"], and goal="recall". Then call gather_context with the returned gather_context fields. Build evidence_ledger from candidate refs in answer_seed/path_set/evidence_digest before answering. Use retrieve_memory or retrieve_context only as follow-up diagnostics when the planned gather, evidence ledger, and one fallback probe are missing decisive evidence. Do not mention tool internals.

Question: ` + question)
	case longmemAnswerStrategyFullDebug:
		return strings.TrimSpace(`Answer the LongMem recall question using the available retrieval tools.
Start with plan_context_query using the question and goal="recall". Then call gather_context with the returned gather_context fields. Build evidence_ledger from candidate refs before final synthesis. Use retrieve_memory, retrieve_context, retrieve_mixed, or load_evidence_ref only when needed to resolve ambiguity after the planned gather and ledger. Do not use code retrieval unless the question explicitly asks about foxctl implementation. Do not mention tool internals.

Question: ` + question)
	default:
		return strings.TrimSpace(`Answer the LongMem recall question using the available memory-recall tools.
Use retrieve_memory before answering. Do not mention tool internals.

Question: ` + question)
	}
}

type longmemAnswerToolRecorder struct {
	next      rlm.ToolExecutor
	seenNames map[string]struct{}
	seenRefs  map[string]struct{}
	names     []string
	refs      []string
}

func (r *longmemAnswerToolRecorder) Execute(ctx context.Context, name string, args json.RawMessage) (map[string]any, error) {
	if r == nil || r.next == nil {
		return nil, fmt.Errorf("longmem answer tool recorder is not configured")
	}
	out, err := r.next.Execute(ctx, name, args)
	if err != nil {
		return out, err
	}
	r.recordMemoryEvidence(name, out)
	return out, nil
}

func (r *longmemAnswerToolRecorder) recordMemoryEvidence(toolName string, payload map[string]any) {
	if strings.TrimSpace(toolName) == "evidence_ledger" {
		r.recordAcceptedLedgerEvidence(payload)
		return
	}
	collectLongmemMemoryEvidence(payload, r.addName, r.addRef)
	for _, node := range longmemEvidenceNodes(payload["nodes"]) {
		refType := strings.TrimSpace(fmt.Sprint(node["ref_type"]))
		if refType != "" && !longmemMemoryEvidenceRefType(refType) {
			continue
		}
		refValue := strings.TrimSpace(fmt.Sprint(node["ref_value"]))
		if refValue != "" {
			r.addName(refValue)
		}
		ref := strings.TrimSpace(fmt.Sprint(node["ref"]))
		if ref != "" {
			r.addRef(ref)
		}
	}
}

func (r *longmemAnswerToolRecorder) recordAcceptedLedgerEvidence(payload map[string]any) {
	for _, ref := range longmemStringSlice(payload["accepted_refs"]) {
		recordLongmemMemoryRefString(ref, r.addName, r.addRef)
	}
	for _, row := range longmemMapSlice(payload["accepted_rows"]) {
		collectLongmemMemoryEvidence(row, r.addName, r.addRef)
	}
}

func collectLongmemMemoryEvidence(value any, addName func(string), addRef func(string)) {
	switch v := value.(type) {
	case string:
		recordLongmemMemoryRefString(v, addName, addRef)
	case []any:
		for _, item := range v {
			collectLongmemMemoryEvidence(item, addName, addRef)
		}
	case []map[string]any:
		for _, item := range v {
			collectLongmemMemoryEvidence(item, addName, addRef)
		}
	case map[string]any:
		refType := strings.TrimSpace(fmt.Sprint(firstMapValue(v, "ref_type", "type")))
		refValue := strings.TrimSpace(fmt.Sprint(firstMapValue(v, "ref_value", "ref")))
		if longmemMemoryEvidenceRefType(refType) && refValue != "" {
			addName(refValue)
			addRef(refType + ":" + refValue)
		}
		for _, child := range v {
			collectLongmemMemoryEvidence(child, addName, addRef)
		}
	}
}

func longmemStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" && value != "<nil>" {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func longmemMapSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func firstMapValue(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			return value
		}
	}
	return nil
}

func recordLongmemMemoryRefString(value string, addName func(string), addRef func(string)) {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"memory_claim:", "named_memory:"} {
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(value, prefix))
		if name == "" {
			return
		}
		name = normalizeLongmemMemoryName(name)
		addName(name)
		addRef(prefix + name)
		return
	}
}

func longmemMemoryEvidenceRefType(refType string) bool {
	switch strings.TrimSpace(refType) {
	case "memory_claim", "named_memory":
		return true
	default:
		return false
	}
}

func normalizeLongmemMemoryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "longmem://") {
		return name
	}
	if slash := strings.LastIndex(name, "/"); slash >= 0 && slash+1 < len(name) {
		name = name[slash+1:]
	}
	if looksLikeLongmemMemoryDigest(name) {
		return "longmem://" + name
	}
	return name
}

func looksLikeLongmemMemoryDigest(value string) bool {
	if len(value) != 24 {
		return false
	}
	for _, ch := range value {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return false
	}
	return true
}

func longmemEvidenceNodes(value any) []map[string]any {
	switch nodes := value.(type) {
	case []map[string]any:
		return nodes
	case []any:
		out := make([]map[string]any, 0, len(nodes))
		for _, node := range nodes {
			if m, ok := node.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func (r *longmemAnswerToolRecorder) addName(name string) {
	name = normalizeLongmemMemoryName(name)
	if name == "" {
		return
	}
	if r.seenNames == nil {
		r.seenNames = make(map[string]struct{})
	}
	if _, ok := r.seenNames[name]; ok {
		return
	}
	r.seenNames[name] = struct{}{}
	r.names = append(r.names, name)
}

func (r *longmemAnswerToolRecorder) addRef(ref string) {
	ref = normalizeLongmemMemoryEvidenceRef(ref)
	if ref == "" {
		return
	}
	if r.seenRefs == nil {
		r.seenRefs = make(map[string]struct{})
	}
	if _, ok := r.seenRefs[ref]; ok {
		return
	}
	r.seenRefs[ref] = struct{}{}
	r.refs = append(r.refs, ref)
}

func normalizeLongmemMemoryEvidenceRef(ref string) string {
	ref = strings.TrimSpace(ref)
	for _, prefix := range []string{"memory_claim:", "named_memory:"} {
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		name := normalizeLongmemMemoryName(strings.TrimPrefix(ref, prefix))
		if name == "" {
			return ""
		}
		return prefix + name
	}
	return ref
}

func (r *longmemAnswerToolRecorder) evidenceNames() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.names...)
}

func (r *longmemAnswerToolRecorder) evidenceRefs() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.refs...)
}

func uniqueEvalStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
