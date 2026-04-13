package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/evals/correctioneval"
	"github.com/jkatigb/agentctl/internal/evals/retrievaleval"
	"github.com/jkatigb/agentctl/internal/evals/transcriptmemoryeval"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/rlm"
	rlmenv "github.com/jkatigb/agentctl/internal/rlm/env"
	"github.com/jkatigb/agentctl/internal/storage"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/transcriptpipeline"
	"github.com/spf13/cobra"
)

func newEvalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run lightweight evaluation suites",
	}
	cmd.AddCommand(newEvalRetrievalCommand())
	cmd.AddCommand(newEvalCorrectionsCommand())
	cmd.AddCommand(newEvalTranscriptMemoryCommand())
	cmd.AddCommand(newEvalAgentsCommand())
	cmd.AddCommand(newEvalCodeSearchEnsembleCommand())
	return cmd
}

func newEvalTranscriptMemoryCommand() *cobra.Command {
	var suiteRef string
	var format string
	var blobSummaryMode string
	var blobSummaryModel string
	var blobSummaryTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "transcript-memory",
		Short: "Evaluate transcript-memory claim quality against a fixed suite",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(suiteRef) == "" {
				return fmt.Errorf("--suite is required")
			}
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			suitePath, err := resolveTranscriptMemorySuitePath(suiteRef)
			if err != nil {
				return err
			}
			runtimeCfg := transcriptpipeline.NewLocalModelRuntime(strings.TrimSpace(blobSummaryMode), strings.TrimSpace(blobSummaryModel), cfg.LLM.ResolveBaseURL("lmstudio"), blobSummaryTimeout)
			runResult, markdown, err := runTranscriptMemoryEval(ctx, suitePath, runtimeCfg)
			if err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "", "markdown", "md":
			default:
				return fmt.Errorf("unsupported --format %q", format)
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("eval/transcript-memory", map[string]any{
				"markdown":   markdown,
				"result":     runResult,
				"suite_path": suitePath,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.AddCommand(newEvalTranscriptMemoryExperimentCommand())

	cmd.Flags().StringVar(&suiteRef, "suite", "", "Transcript-memory eval suite name or path")
	cmd.Flags().StringVar(&format, "format", "markdown", "Output format: markdown")
	cmd.Flags().StringVar(&blobSummaryMode, "blob-summary-mode", "auto", "Blob summary mode: auto, deterministic, or lmstudio")
	cmd.Flags().StringVar(&blobSummaryModel, "blob-summary-model", "nvidia/nemotron-3-nano-4b", "Model to use for LMStudio transcript-memory eval stages")
	cmd.Flags().DurationVar(&blobSummaryTimeout, "blob-summary-timeout", 45*time.Second, "Timeout for LMStudio transcript-memory eval stages")
	return cmd
}

func newEvalTranscriptMemoryExperimentCommand() *cobra.Command {
	var suiteRefs []string
	var blobSummaryMode string
	var blobSummaryModel string
	var blobSummaryTimeout time.Duration
	var classificationPromptVersions []string
	var claimReviewPromptVersions []string
	var includeBaseline bool
	var saveDir string
	var logFile string
	var label string
	var description string

	cmd := &cobra.Command{
		Use:   "experiment",
		Short: "Run transcript-memory eval suites and append an experiment record",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(suiteRefs) == 0 {
				return fmt.Errorf("--suite is required")
			}
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			baseRuntime := transcriptpipeline.NewLocalModelRuntime(strings.TrimSpace(blobSummaryMode), strings.TrimSpace(blobSummaryModel), cfg.LLM.ResolveBaseURL("lmstudio"), blobSummaryTimeout)
			runtimes := expandTranscriptMemoryVariants(baseRuntime, classificationPromptVersions, claimReviewPromptVersions, includeBaseline)

			resolvedSaveDir := strings.TrimSpace(saveDir)
			if resolvedSaveDir == "" {
				resolvedSaveDir = filepath.Join(resolveContextWorkspace(""), ".agentctl", "exports", "evals", "transcript-memory")
			}
			resolvedLogFile := strings.TrimSpace(logFile)
			if resolvedLogFile == "" {
				resolvedLogFile = filepath.Join(resolveContextWorkspace(""), ".agentctl", "runtime", "transcript_memory_experiments.ndjson")
			}

			type variantRun struct {
				Record  transcriptmemoryeval.ExperimentRecord `json:"record"`
				Results []transcriptmemoryeval.RunResult      `json:"results"`
			}
			variantRuns := make([]variantRun, 0, len(runtimes))
			for _, runtimeCfg := range runtimes {
				runResults := make([]transcriptmemoryeval.RunResult, 0, len(suiteRefs))
				artifacts := make([]transcriptmemoryeval.SavedArtifact, 0, len(suiteRefs))
				configID := transcriptMemoryConfigID(runtimeCfg)
				variantDir := filepath.Join(resolvedSaveDir, configID)
				for _, suiteRef := range suiteRefs {
					suitePath, err := resolveTranscriptMemorySuitePath(suiteRef)
					if err != nil {
						return err
					}
					runResult, markdown, err := runTranscriptMemoryEval(ctx, suitePath, runtimeCfg)
					if err != nil {
						return err
					}
					jsonPath, markdownPath, err := transcriptmemoryeval.SaveRunOutputs(variantDir, runResult.Suite, runResult, markdown)
					if err != nil {
						return err
					}
					runResults = append(runResults, runResult)
					artifacts = append(artifacts, transcriptmemoryeval.SavedArtifact{
						Suite:        runResult.Suite,
						JSONPath:     jsonPath,
						MarkdownPath: markdownPath,
					})
				}
				record := transcriptmemoryeval.BuildExperimentRecord(label, description, configID, runResults, artifacts)
				if err := transcriptmemoryeval.AppendExperimentRecord(resolvedLogFile, record); err != nil {
					return err
				}
				variantRuns = append(variantRuns, variantRun{Record: record, Results: runResults})
			}
			sort.SliceStable(variantRuns, func(i, j int) bool {
				return variantRuns[i].Record.Score > variantRuns[j].Record.Score
			})

			return envelope.Write(cmd.OutOrStdout(), envelope.OK("eval/transcript-memory-experiment", map[string]any{
				"records":  variantRuns,
				"best":     variantRuns[0],
				"log_file": resolvedLogFile,
				"save_dir": resolvedSaveDir,
				"variants": len(variantRuns),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringSliceVar(&suiteRefs, "suite", nil, "Transcript-memory eval suite name or path (repeatable)")
	cmd.Flags().StringVar(&blobSummaryMode, "blob-summary-mode", "auto", "Blob summary mode: auto, deterministic, or lmstudio")
	cmd.Flags().StringVar(&blobSummaryModel, "blob-summary-model", "nvidia/nemotron-3-nano-4b", "Model to use for LMStudio transcript-memory experiment stages")
	cmd.Flags().DurationVar(&blobSummaryTimeout, "blob-summary-timeout", 45*time.Second, "Timeout for LMStudio transcript-memory experiment stages")
	cmd.Flags().StringSliceVar(&classificationPromptVersions, "classification-prompt-version", nil, "Classification prompt version(s) to compare")
	cmd.Flags().StringSliceVar(&claimReviewPromptVersions, "claim-review-prompt-version", nil, "Claim review prompt version(s) to compare")
	cmd.Flags().BoolVar(&includeBaseline, "include-baseline", true, "Include the default runtime config as a baseline variant")
	cmd.Flags().StringVar(&saveDir, "save-dir", "", "Directory for saved eval artifacts")
	cmd.Flags().StringVar(&logFile, "log-file", "", "NDJSON experiment log file")
	cmd.Flags().StringVar(&label, "label", "", "Short label for this experiment run")
	cmd.Flags().StringVar(&description, "description", "", "Short description of what this run is testing")
	return cmd
}

func runTranscriptMemoryEval(ctx context.Context, suitePath string, runtimeCfg transcriptpipeline.LocalModelRuntime) (transcriptmemoryeval.RunResult, string, error) {
	suite, err := transcriptmemoryeval.LoadSuite(suitePath)
	if err != nil {
		return transcriptmemoryeval.RunResult{}, "", err
	}
	runResult, err := transcriptmemoryeval.RunSuite(ctx, suite, transcriptmemoryeval.RunOptions{Runtime: runtimeCfg})
	if err != nil {
		return transcriptmemoryeval.RunResult{}, "", err
	}
	return runResult, transcriptmemoryeval.RenderMarkdown(runResult), nil
}

func transcriptMemoryConfigID(runtimeCfg transcriptpipeline.LocalModelRuntime) string {
	raw := strings.Join([]string{
		runtimeCfg.Mode,
		runtimeCfg.Provider,
		runtimeCfg.Model,
		runtimeCfg.DoctrineBridgeModel,
		runtimeCfg.DoctrineDistillModel,
		runtimeCfg.ReferencePromptVersion,
		runtimeCfg.ToolOutputPromptVersion,
		runtimeCfg.ObjectivePromptVersion,
		runtimeCfg.DoctrineBridgePromptVersion,
		runtimeCfg.DoctrineDistillPromptVersion,
		runtimeCfg.ObjectiveAlignmentPromptVersion,
		runtimeCfg.GroupToplinePromptVersion,
		runtimeCfg.GroupClaimsPromptVersion,
		runtimeCfg.ClassificationPromptVersion,
		runtimeCfg.ClaimReviewPromptVersion,
		fmt.Sprintf("%d", runtimeCfg.MaxContextTokens),
	}, "|")
	return sanitizeEvalName(transcriptpipeline.BoundArtifactText(raw, 400))
}

func expandTranscriptMemoryVariants(base transcriptpipeline.LocalModelRuntime, classificationPromptVersions, claimReviewPromptVersions []string, includeBaseline bool) []transcriptpipeline.LocalModelRuntime {
	variants := make([]transcriptpipeline.LocalModelRuntime, 0, 4)
	seen := make(map[string]struct{})
	appendVariant := func(rt transcriptpipeline.LocalModelRuntime) {
		key := transcriptMemoryConfigID(rt)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		variants = append(variants, rt)
	}
	if includeBaseline {
		appendVariant(base)
	}
	maxLen := maxInt(1, len(classificationPromptVersions), len(claimReviewPromptVersions))
	for i := 0; i < maxLen; i++ {
		rt := base
		if len(classificationPromptVersions) > 0 {
			rt.ClassificationPromptVersion = classificationPromptVersions[minIntEval(i, len(classificationPromptVersions)-1)]
		}
		if len(claimReviewPromptVersions) > 0 {
			rt.ClaimReviewPromptVersion = claimReviewPromptVersions[minIntEval(i, len(claimReviewPromptVersions)-1)]
		}
		appendVariant(rt)
	}
	return variants
}

func maxInt(first int, rest ...int) int {
	best := first
	for _, item := range rest {
		if item > best {
			best = item
		}
	}
	return best
}

func minIntEval(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func newEvalCorrectionsCommand() *cobra.Command {
	var suiteRef string
	var workspacePath string
	var vaultPath string
	var format string

	cmd := &cobra.Command{
		Use:   "corrections",
		Short: "Evaluate correction inspectors against a curated suite of expected classifications and fix families",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(suiteRef) == "" {
				return fmt.Errorf("--suite is required")
			}
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			suitePath, err := resolveCorrectionSuitePath(suiteRef)
			if err != nil {
				return err
			}
			suite, err := correctioneval.LoadSuite(suitePath)
			if err != nil {
				return err
			}

			var index obsidianindex.Store
			var repo *repoindex.Store
			var semanticProvider semantic.EmbeddingProvider
			var workspaceStore *contextplane.WorkspaceStore

			for _, c := range suite.Cases {
				if c.Method == "aca_retrieve" && strings.TrimSpace(vaultPath) == "" {
					return fmt.Errorf("--vault-path is required for aca_retrieve correction cases")
				}
			}
			if strings.TrimSpace(vaultPath) != "" {
				index, err = obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
				if err != nil {
					return err
				}
				defer func() { _ = index.Close() }()
				repo, err = repoindex.Open(ctx, cfg.Storage.Root, target)
				if err != nil {
					return err
				}
				defer func() { _ = repo.Close() }()
				semanticProvider = openObsidianSemanticProvider(cfg)
				workspaceStore = contextplane.NewWorkspaceStore(target)
				if err := ensureTopOfMindForEval(ctx, cfg.Storage.Root, target, workspaceStore); err != nil {
					return err
				}
			} else {
				repo, err = repoindex.Open(ctx, cfg.Storage.Root, target)
				if err != nil {
					return err
				}
				defer func() { _ = repo.Close() }()
				workspaceStore = contextplane.NewWorkspaceStore(target)
			}

			results := make([]correctioneval.CaseResult, 0, len(suite.Cases))
			for _, c := range suite.Cases {
				result := correctioneval.CaseResult{
					ID:                     c.ID,
					Method:                 c.Method,
					Query:                  c.Query,
					Notes:                  c.Notes,
					ExpectedClassification: c.ExpectedClassification,
					ExpectedFixContains:    c.ExpectedFixContains,
				}
				var actualClass, actualFix string
				var evalErr error
				switch c.Method {
				case "aca_retrieve":
					res, err := workspaceStore.RetrieveWithOptions(ctx, index, repo, semanticProvider, c.Query, 5, workspaceStore.CurrentRetrievalOptions())
					if err != nil {
						evalErr = err
						break
					}
					inspection, err := workspaceStore.InspectRetrieval(ctx, index, vaultPath, c.Query, c.ExpectedAnyOf, res, workspaceStore.CurrentRetrievalOptions(), 5)
					if err != nil {
						evalErr = err
						break
					}
					actualClass = inspection.Classification
					actualFix = inspection.Proposal.Summary
				case "repoindex_search":
					report, err := buildRepoIndexSearchInspectionReport(ctx, cfg.Storage.Root, target, retrievaleval.Suite{Name: "single", Queries: []retrievaleval.Query{{ID: c.ID, Query: c.Query, ExpectedAnyOf: c.ExpectedAnyOf}}}, 5)
					if err != nil {
						evalErr = err
						break
					}
					if len(report.Inspections) > 0 {
						actualClass = report.Inspections[0].Classification
						actualFix = report.Inspections[0].RecommendedFix
					}
				case "repoindex_dag":
					report, err := buildRepoIndexDAGInspectionReport(ctx, cfg.Storage.Root, target, retrievaleval.Suite{Name: "single", Queries: []retrievaleval.Query{{ID: c.ID, Query: c.Query, ExpectedAnyOf: c.ExpectedAnyOf}}}, 5)
					if err != nil {
						evalErr = err
						break
					}
					if len(report.Inspections) > 0 {
						actualClass = report.Inspections[0].Classification
						actualFix = report.Inspections[0].RecommendedFix
					}
				case "semantic_search":
					report, err := buildSemanticSearchInspectionReport(ctx, target, vaultPath, c.Query, c.ExpectedAnyOf, 5)
					if err != nil {
						evalErr = err
						break
					}
					actualClass = report.Classification
					actualFix = report.RecommendedFix
				default:
					evalErr = fmt.Errorf("unsupported correction eval method %q", c.Method)
				}
				result.ActualClassification = actualClass
				result.ActualFix = actualFix
				result.ClassificationMatch = strings.TrimSpace(actualClass) == strings.TrimSpace(c.ExpectedClassification)
				if strings.TrimSpace(c.ExpectedFixContains) != "" {
					result.FixChecked = true
					result.FixMatch = strings.Contains(strings.ToLower(actualFix), strings.ToLower(strings.TrimSpace(c.ExpectedFixContains)))
				}
				if evalErr != nil {
					result.Error = evalErr.Error()
				}
				results = append(results, result)
			}

			runResult := correctioneval.RunResult{
				Suite:       suite.Name,
				Workspace:   target,
				VaultPath:   vaultPath,
				Cases:       results,
				Summaries:   correctioneval.Summarize(results),
				GeneratedAt: time.Now().UTC(),
			}

			var markdown string
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "", "markdown", "md":
				markdown = correctioneval.RenderMarkdown(runResult)
			default:
				return fmt.Errorf("unsupported --format %q", format)
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("eval/corrections", map[string]any{
				"markdown":   markdown,
				"result":     runResult,
				"suite_path": suitePath,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&suiteRef, "suite", "", "Correction eval suite name or path")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path (required for ACA correction cases)")
	cmd.Flags().StringVar(&format, "format", "markdown", "Output format: markdown")
	return cmd
}

func newEvalRetrievalCommand() *cobra.Command {
	var suiteRef string
	var workspacePath string
	var vaultPath string
	var limit int
	var format string
	var modes []string
	var policyFile string
	var failOnAlerts bool
	var rebuildIndex bool
	var canonicalOnly bool
	var save bool
	var saveDir string

	cmd := &cobra.Command{
		Use:   "retrieval",
		Short: "Evaluate lexical, semantic, and blended retrieval against a curated query suite",
		RunE: func(cmd *cobra.Command, _ []string) error {
			policy := retrievaleval.Policy{}
			if strings.TrimSpace(policyFile) != "" {
				loadedPolicy, err := retrievaleval.LoadPolicy(policyFile)
				if err != nil {
					return fmt.Errorf("load policy file: %w", err)
				}
				policy = loadedPolicy
			}
			if strings.TrimSpace(suiteRef) == "" {
				suiteRef = strings.TrimSpace(policy.Suite)
			}
			if !cmd.Flags().Changed("limit") && policy.Limit > 0 {
				limit = policy.Limit
			}
			if !cmd.Flags().Changed("format") && strings.TrimSpace(policy.Format) != "" {
				format = strings.TrimSpace(policy.Format)
			}
			if !cmd.Flags().Changed("mode") && len(policy.Modes) > 0 {
				modes = append([]string(nil), policy.Modes...)
			}
			if !cmd.Flags().Changed("fail-on-alerts") && policy.FailOnAlerts {
				failOnAlerts = true
			}
			if strings.TrimSpace(suiteRef) == "" {
				return fmt.Errorf("--suite is required (or provide it via --policy-file)")
			}
			selectedModes := normalizeEvalModes(modes)
			requiresVault := evalModesRequireVault(selectedModes)
			if requiresVault && strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			suitePath, err := resolveEvalSuitePath(suiteRef)
			if err != nil {
				return err
			}
			suite, err := retrievaleval.LoadSuite(suitePath)
			if err != nil {
				return err
			}
			var index obsidianindex.Store
			var repo *repoindex.Store
			var semanticProvider semantic.EmbeddingProvider
			var workspaceStore *contextplane.WorkspaceStore
			var acaMemStore storage.MemoryStore
			var cochangeMemStore storage.MemoryStore
			var cochangeProvider semantic.EmbeddingProvider
			if hasMode(selectedModes, "cochange_artifacts") {
				memStore, err := memorystore.OpenWithConfig(ctx, cfg)
				if err != nil {
					return err
				}
				cochangeMemStore = memStore
				defer cochangeMemStore.Close()
				cochangeProvider, _ = semantic.NewProviderForScope(
					semantic.ScopeMemory,
					cfg,
					semantic.WithVoyageKey(os.Getenv("VOYAGE_API_KEY")),
					semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
				)
				if _, err := contextplane.BuildCoChangeArtifacts(ctx, target, memStore, cochangeProvider, contextplane.DefaultCoChangeArtifactBuildOptions()); err != nil {
					return err
				}
			}
			if requiresVault {
				memStore, err := memorystore.OpenWithConfig(ctx, cfg)
				if err != nil {
					return err
				}
				acaMemStore = memStore
				defer acaMemStore.Close()
				index, err = obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
				if err != nil {
					return err
				}
				defer func() { _ = index.Close() }()
				if rebuildIndex {
					if _, err := index.Rebuild(ctx, vaultPath); err != nil {
						return err
					}
				}
				repo, err = repoindex.Open(ctx, cfg.Storage.Root, target)
				if err != nil {
					return err
				}
				defer func() { _ = repo.Close() }()
				semanticProvider = openObsidianSemanticProvider(cfg)
				workspaceStore = contextplane.NewWorkspaceStore(target)
				if err := ensureTopOfMindForEval(ctx, cfg.Storage.Root, target, workspaceStore); err != nil {
					return err
				}
			}
			results := make([]retrievaleval.QueryResult, 0, len(suite.Queries))
			for _, q := range suite.Queries {
				qr := retrievaleval.QueryResult{
					ID:    q.ID,
					Query: q.Query,
					Notes: q.Notes,
					Modes: map[string]retrievaleval.ModeResult{},
				}
				if hasMode(selectedModes, "baseline") {
					hits, err := index.SearchNotes(ctx, q.Query, limit)
					paths := extractSearchHitPaths(hits)
					paths = filterEvalPaths(paths, true)
					qr.Modes["baseline"] = retrievaleval.EvaluateMode("baseline", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "lexical") {
					hits, err := index.SearchNotes(ctx, q.Query, limit)
					paths := filterEvalPaths(extractSearchHitPaths(hits), canonicalOnly)
					qr.Modes["lexical"] = retrievaleval.EvaluateMode("lexical", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "semantic") {
					if semanticProvider == nil {
						qr.Modes["semantic"] = retrievaleval.EvaluateMode("semantic", nil, q.ExpectedAnyOf, 0, fmt.Errorf("semantic provider unavailable"))
					} else {
						hits, err := index.SearchNotesSemantic(ctx, q.Query, semanticProvider, limit)
						paths := filterEvalPaths(extractSearchHitPaths(hits), canonicalOnly)
						qr.Modes["semantic"] = retrievaleval.EvaluateMode("semantic", paths, q.ExpectedAnyOf, len(paths), err)
					}
				}
				if hasMode(selectedModes, "blended") {
					result, err := workspaceStore.Retrieve(ctx, index, repo, semanticProvider, q.Query, limit)
					paths := filterEvalPaths(extractRetrievalHitPaths(result.VaultHits), canonicalOnly)
					qr.Modes["blended"] = retrievaleval.EvaluateMode("blended", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "skill_default") {
					paths, err := runSemanticSearchEvalMode(ctx, target, vaultPath, q.Query, limit, []string{"symbols", "sessions", "memories", "tasks", "codemaps"})
					qr.Modes["skill_default"] = retrievaleval.EvaluateMode("skill_default", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "skill_context") {
					paths, err := runSemanticSearchEvalMode(ctx, target, vaultPath, q.Query, limit, []string{"context"})
					qr.Modes["skill_context"] = retrievaleval.EvaluateMode("skill_context", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "skill_default_plus_context") {
					paths, err := runSemanticSearchEvalMode(ctx, target, vaultPath, q.Query, limit, []string{"symbols", "sessions", "memories", "tasks", "codemaps", "context"})
					qr.Modes["skill_default_plus_context"] = retrievaleval.EvaluateMode("skill_default_plus_context", paths, q.ExpectedAnyOf, len(paths), err)
				}
				baseACAOpts := workspaceStore.CurrentRetrievalOptions()
				if hasMode(selectedModes, "aca_control_only") {
					opts := baseACAOpts
					opts.IncludeTopOfMindResult = true
					opts.IncludeLatestHandoff = true
					opts.IncludeVaultHits = false
					opts.UseRelevantRefBoost = false
					opts.UseHandoffRefBoost = false
					opts.UseCodeHints = false
					opts.UseSemanticVaultSearch = false
					opts.IncludeControlPlaneRefs = true
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, acaMemStore, q.Query, limit, opts)
					qr.Modes["aca_control_only"] = retrievaleval.EvaluateMode("aca_control_only", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_vault_only") {
					opts := baseACAOpts
					opts.IncludeTopOfMindResult = false
					opts.IncludeLatestHandoff = false
					opts.IncludeVaultHits = true
					opts.UseRelevantRefBoost = false
					opts.UseHandoffRefBoost = false
					opts.UseCodeHints = false
					opts.UseSemanticVaultSearch = true
					opts.IncludeControlPlaneRefs = false
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, acaMemStore, q.Query, limit, opts)
					qr.Modes["aca_vault_only"] = retrievaleval.EvaluateMode("aca_vault_only", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_repo_hints") {
					opts := baseACAOpts
					opts.IncludeTopOfMindResult = false
					opts.IncludeLatestHandoff = false
					opts.IncludeVaultHits = true
					opts.UseRelevantRefBoost = false
					opts.UseHandoffRefBoost = false
					opts.UseCodeHints = true
					opts.UseSemanticVaultSearch = true
					opts.IncludeControlPlaneRefs = false
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, acaMemStore, q.Query, limit, opts)
					qr.Modes["aca_repo_hints"] = retrievaleval.EvaluateMode("aca_repo_hints", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_canonical_only") {
					opts := baseACAOpts
					opts.IncludeTopOfMindResult = false
					opts.IncludeLatestHandoff = false
					opts.IncludeVaultHits = true
					opts.UseRelevantRefBoost = false
					opts.UseHandoffRefBoost = false
					opts.UseCodeHints = true
					opts.UseSemanticVaultSearch = true
					opts.AllowedTrusts = []string{"canonical", "reviewed"}
					opts.IncludeControlPlaneRefs = false
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, acaMemStore, q.Query, limit, opts)
					qr.Modes["aca_canonical_only"] = retrievaleval.EvaluateMode("aca_canonical_only", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_package_fallback") {
					opts := baseACAOpts
					opts.IncludeTopOfMindResult = false
					opts.IncludeLatestHandoff = false
					opts.IncludeVaultHits = true
					opts.UseRelevantRefBoost = false
					opts.UseHandoffRefBoost = false
					opts.UseCodeHints = true
					opts.UseSemanticVaultSearch = true
					opts.UsePackageNoteFallback = true
					opts.AllowedTrusts = []string{"canonical", "reviewed"}
					opts.IncludeControlPlaneRefs = false
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, acaMemStore, q.Query, limit, opts)
					qr.Modes["aca_package_fallback"] = retrievaleval.EvaluateMode("aca_package_fallback", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_query_typed") {
					opts := baseACAOpts
					opts.IncludeTopOfMindResult = false
					opts.IncludeLatestHandoff = false
					opts.IncludeVaultHits = true
					opts.UseRelevantRefBoost = false
					opts.UseHandoffRefBoost = false
					opts.UseCodeHints = true
					opts.UseSemanticVaultSearch = true
					opts.AllowedTrusts = []string{"canonical", "reviewed"}
					opts.UseQueryTypeBias = true
					opts.IncludeControlPlaneRefs = false
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, acaMemStore, q.Query, limit, opts)
					qr.Modes["aca_query_typed"] = retrievaleval.EvaluateMode("aca_query_typed", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_default") {
					opts := baseACAOpts
					opts.IncludeControlPlaneRefs = false
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, acaMemStore, q.Query, limit, opts)
					qr.Modes["aca_default"] = retrievaleval.EvaluateMode("aca_default", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_cochange") {
					opts := baseACAOpts
					opts.IncludeControlPlaneRefs = false
					opts.UseCoChangePrior = true
					opts.UseContinuityBundles = false
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, acaMemStore, q.Query, limit, opts)
					qr.Modes["aca_cochange"] = retrievaleval.EvaluateMode("aca_cochange", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_cochange_continuity") {
					opts := baseACAOpts
					opts.IncludeControlPlaneRefs = false
					opts.UseCoChangePrior = true
					opts.UseContinuityBundles = true
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, acaMemStore, q.Query, limit, opts)
					qr.Modes["aca_cochange_continuity"] = retrievaleval.EvaluateMode("aca_cochange_continuity", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "cochange_artifacts") {
					paths, err := runCoChangeArtifactEvalMode(ctx, target, q.Query, limit, cochangeMemStore, cochangeProvider)
					qr.Modes["cochange_artifacts"] = retrievaleval.EvaluateMode("cochange_artifacts", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "repoindex_search") {
					paths, err := runRepoIndexSearchEvalMode(ctx, cfg.Storage.Root, target, q.Query, limit)
					qr.Modes["repoindex_search"] = retrievaleval.EvaluateMode("repoindex_search", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "repoindex_dag") {
					paths, err := runRepoIndexDAGEvalMode(ctx, cfg.Storage.Root, target, q.Query, limit)
					qr.Modes["repoindex_dag"] = retrievaleval.EvaluateMode("repoindex_dag", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "rlm_llm") {
					paths, err := runRLMEvalMode(ctx, cfg, target, vaultPath, q.Query, limit, rlmenv.ToolProfileDefault)
					qr.Modes["rlm_llm"] = retrievaleval.EvaluateMode("rlm_llm", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "rlm_llm_codeintel") {
					paths, err := runRLMEvalMode(ctx, cfg, target, vaultPath, q.Query, limit, rlmenv.ToolProfileCodeIntel)
					qr.Modes["rlm_llm_codeintel"] = retrievaleval.EvaluateMode("rlm_llm_codeintel", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "rlm_llm_code_staged") {
					paths, err := runRLMStagedEvalMode(ctx, cfg, target, vaultPath, q.Query, limit)
					qr.Modes["rlm_llm_code_staged"] = retrievaleval.EvaluateMode("rlm_llm_code_staged", paths, q.ExpectedAnyOf, len(paths), err)
				}
				results = append(results, qr)
			}
			runResult := retrievaleval.RunResult{
				Suite:       suite.Name,
				Workspace:   target,
				VaultPath:   vaultPath,
				Limit:       limit,
				Queries:     results,
				Summaries:   retrievaleval.Summarize(results, selectedModes),
				GeneratedAt: time.Now().UTC(),
			}
			policy.Suite = suite.Name
			policy.Limit = limit
			policy.Format = format
			policy.Modes = append([]string(nil), selectedModes...)
			policy.FailOnAlerts = failOnAlerts
			alerts := retrievaleval.BuildAlerts(runResult.Summaries, policy)
			data := map[string]any{
				"suite_path":  suitePath,
				"policy_file": strings.TrimSpace(policyFile),
				"policy":      policy,
				"alerts":      alerts,
				"result":      runResult,
			}
			markdown := renderRetrievalEvalMarkdown(runResult, policy, alerts, strings.TrimSpace(policyFile))
			if strings.EqualFold(format, "markdown") {
				data["markdown"] = markdown
			}
			if save {
				jsonPath, markdownPath, err := saveEvalOutputs(target, saveDir, suite.Name, runResult, markdown)
				if err != nil {
					return err
				}
				data["saved"] = map[string]any{
					"json_path":     jsonPath,
					"markdown_path": markdownPath,
				}
			}
			if failOnAlerts && len(alerts) > 0 {
				messages := make([]string, 0, len(alerts))
				for _, alert := range alerts {
					messages = append(messages, alert.Message)
				}
				return fmt.Errorf("retrieval alerts: %s", strings.Join(messages, "; "))
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("eval/retrieval", data, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&suiteRef, "suite", "", "Suite name (agentctl, praze) or explicit YAML path")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum hits per retrieval mode")
	cmd.Flags().StringVar(&format, "format", "markdown", "Output companion format: markdown or json")
	cmd.Flags().StringSliceVar(&modes, "mode", []string{"baseline", "lexical", "semantic", "blended"}, "Retrieval modes to evaluate (also available: skill_default, skill_context, skill_default_plus_context, aca_control_only, aca_vault_only, aca_repo_hints, aca_canonical_only, aca_package_fallback, aca_query_typed, aca_default, aca_cochange, aca_cochange_continuity, cochange_artifacts, repoindex_search, repoindex_dag, rlm_llm, rlm_llm_codeintel, rlm_llm_code_staged)")
	cmd.Flags().StringVar(&policyFile, "policy-file", "", "Optional YAML file with retrieval suite defaults and metric thresholds")
	cmd.Flags().BoolVar(&failOnAlerts, "fail-on-alerts", false, "Exit with an error when any retrieval metric alert is present")
	cmd.Flags().BoolVar(&rebuildIndex, "rebuild-index", true, "Rebuild the vault index before evaluation")
	cmd.Flags().BoolVar(&canonicalOnly, "canonical-only", true, "Exclude inbox draft hits from evaluation paths")
	cmd.Flags().BoolVar(&save, "save", false, "Save JSON and Markdown eval outputs under the workspace exports directory")
	cmd.Flags().StringVar(&saveDir, "save-dir", "", "Override save directory (default: <workspace>/.agentctl/exports/evals)")
	return cmd
}

func renderRetrievalEvalMarkdown(result retrievaleval.RunResult, policy retrievaleval.Policy, alerts []retrievaleval.Alert, policyFile string) string {
	var b strings.Builder
	b.WriteString(retrievaleval.RenderMarkdown(result))
	if strings.TrimSpace(policyFile) != "" || len(policy.Thresholds) > 0 {
		b.WriteString("\n## Policy\n\n")
		if strings.TrimSpace(policyFile) != "" {
			b.WriteString(fmt.Sprintf("- Policy file: `%s`\n", strings.TrimSpace(policyFile)))
		}
		b.WriteString(fmt.Sprintf("- Fail on alerts: `%t`\n", policy.FailOnAlerts))
		if len(policy.Thresholds) > 0 {
			modes := make([]string, 0, len(policy.Thresholds))
			for mode := range policy.Thresholds {
				modes = append(modes, mode)
			}
			sort.Strings(modes)
			b.WriteString("- Thresholds:\n")
			for _, mode := range modes {
				threshold := policy.Thresholds[mode]
				parts := make([]string, 0, 3)
				if threshold.MinHitRateAt5 > 0 {
					parts = append(parts, fmt.Sprintf("hit@5>=%.2f", threshold.MinHitRateAt5))
				}
				if threshold.MinHitRateAt10 > 0 {
					parts = append(parts, fmt.Sprintf("hit@10>=%.2f", threshold.MinHitRateAt10))
				}
				if threshold.MinMeanReciprocalRank > 0 {
					parts = append(parts, fmt.Sprintf("MRR>=%.2f", threshold.MinMeanReciprocalRank))
				}
				b.WriteString(fmt.Sprintf("  - `%s`: %s\n", mode, strings.Join(parts, ", ")))
			}
		}
	}
	if len(alerts) > 0 {
		b.WriteString("\n## Alerts\n\n")
		for _, alert := range alerts {
			b.WriteString("- " + alert.Message + "\n")
		}
	}
	return b.String()
}

func resolveEvalSuitePath(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("suite reference required")
	}
	if filepath.IsAbs(ref) {
		return ref, nil
	}
	baseDir := filepath.Join("testdata", "evals", "retrieval")
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	if strings.Contains(ref, string(filepath.Separator)) || strings.Contains(ref, "/") || strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") {
		candidateAbs, err := filepath.Abs(filepath.Clean(ref))
		if err != nil {
			return "", err
		}
		if strings.Contains(ref, string(filepath.Separator)) || strings.Contains(ref, "/") {
			return candidateAbs, nil
		}
		rel, err := filepath.Rel(baseAbs, candidateAbs)
		if err != nil {
			return "", err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("suite path must stay under %s", baseDir)
		}
		return candidateAbs, nil
	}
	return filepath.Join(baseDir, ref+".yaml"), nil
}

func resolveCorrectionSuitePath(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("suite reference required")
	}
	if filepath.IsAbs(ref) {
		return ref, nil
	}
	baseDir := filepath.Join("testdata", "evals", "corrections")
	if strings.Contains(ref, string(filepath.Separator)) || strings.Contains(ref, "/") || strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") {
		return filepath.Abs(filepath.Clean(ref))
	}
	return filepath.Join(baseDir, ref+".yaml"), nil
}

func resolveTranscriptMemorySuitePath(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("suite reference required")
	}
	if filepath.IsAbs(ref) {
		return ref, nil
	}
	baseDir := filepath.Join("testdata", "evals", "transcriptmemory")
	if strings.Contains(ref, string(filepath.Separator)) || strings.Contains(ref, "/") || strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") {
		return filepath.Abs(filepath.Clean(ref))
	}
	return filepath.Join(baseDir, ref+".yaml"), nil
}

func normalizeEvalModes(modes []string) []string {
	if len(modes) == 0 {
		return []string{"baseline", "lexical", "semantic", "blended"}
	}
	out := make([]string, 0, len(modes))
	seen := map[string]struct{}{}
	for _, mode := range modes {
		mode = strings.ToLower(strings.TrimSpace(mode))
		if mode == "" {
			continue
		}
		if _, ok := seen[mode]; ok {
			continue
		}
		seen[mode] = struct{}{}
		out = append(out, mode)
	}
	return out
}

func evalModesRequireVault(modes []string) bool {
	for _, mode := range modes {
		switch mode {
		case "baseline", "lexical", "semantic", "blended",
			"aca_control_only", "aca_vault_only", "aca_repo_hints", "aca_canonical_only", "aca_package_fallback", "aca_query_typed",
			"aca_default", "aca_cochange", "aca_cochange_continuity":
			return true
		}
	}
	return false
}

type semanticSearchEvalOutput struct {
	Results []struct {
		Path string `json:"path"`
	} `json:"results"`
}

func runSemanticSearchEvalMode(ctx context.Context, workspacePath, vaultPath, query string, limit int, scope []string) ([]string, error) {
	input := map[string]any{
		"query":     query,
		"scope":     scope,
		"workspace": resolveContextWorkspace(workspacePath),
		"limit":     limit,
	}
	if slicesContain(scope, "context") && strings.TrimSpace(vaultPath) != "" {
		input["vault_path"] = strings.TrimSpace(vaultPath)
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	repoRoot, err := resolveAgentctlRepoRoot()
	if err != nil {
		return nil, err
	}
	result := executil.RunWithInput(ctx, repoRoot, "go", body, "run", "./skills/code_semantic_search")
	if result.Err != nil {
		return nil, fmt.Errorf("run current code/semantic_search source: %w", result.Err)
	}
	env, err := protocol.DecodeEnvelope(result.Stdout)
	if err != nil {
		return nil, err
	}
	if env.Status == envelope.StatusError {
		return nil, protocol.EnvelopeStatusErrorFromEnvelope(env)
	}
	var out semanticSearchEvalOutput
	if err := protocol.DecodeEnvelopeDataInto(env, &out); err != nil {
		return nil, err
	}
	return extractSemanticSearchResultPaths(out.Results), nil
}

func runRepoIndexSearchEvalMode(ctx context.Context, storageRoot, workspacePath, query string, limit int) ([]string, error) {
	store, err := repoindex.Open(ctx, storageRoot, resolveContextWorkspace(workspacePath))
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	result, err := service.SearchWithProjection(ctx, repoquery.SearchRequest{
		Query: strings.TrimSpace(query),
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return extractRepoAnchorPaths(result.Anchors), nil
}

func runRepoIndexDAGEvalMode(ctx context.Context, storageRoot, workspacePath, query string, limit int) ([]string, error) {
	store, err := repoindex.Open(ctx, storageRoot, resolveContextWorkspace(workspacePath))
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	result, err := service.DAGGrepWithProjection(ctx, repoquery.DAGGrepRequest{
		Query:          strings.TrimSpace(query),
		K:              3,
		EdgeTypes:      repoindex.EdgeSetStructural,
		Direction:      repoindex.DirOut,
		Depth:          2,
		Budget:         dagBudget(limit),
		PerNodeCap:     20,
		IncludeAnchors: true,
	})
	if err != nil {
		return nil, err
	}
	paths := extractRepoAnchorPaths(result.Anchors)
	if limit > 0 && len(paths) > limit {
		paths = paths[:limit]
	}
	return paths, nil
}

func runACAEvalMode(
	ctx context.Context,
	store *contextplane.WorkspaceStore,
	index obsidianindex.Store,
	repo *repoindex.Store,
	semanticProvider semantic.EmbeddingProvider,
	memStore storage.MemoryStore,
	query string,
	limit int,
	opts contextplane.RetrievalOptions,
) ([]string, error) {
	if store == nil {
		return nil, fmt.Errorf("workspace store unavailable")
	}
	result, err := store.RetrieveWithOptionsAndMemory(ctx, index, repo, semanticProvider, memStore, query, limit, opts)
	if err != nil {
		return nil, err
	}
	return extractACAResultPaths(result, limit, opts), nil
}

func runCoChangeArtifactEvalMode(ctx context.Context, workspacePath, query string, limit int, memStore storage.MemoryStore, provider semantic.EmbeddingProvider) ([]string, error) {
	if memStore == nil {
		return nil, fmt.Errorf("cochange memory store unavailable")
	}
	hits, err := contextplane.SearchCoChangeArtifacts(ctx, workspacePath, query, limit, memStore, provider)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		path := filepath.ToSlash(strings.TrimSpace(hit.AnchorPath))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out, nil
}

func runRLMEvalMode(ctx context.Context, cfg config.Config, workspacePath, vaultPath, query string, limit int, toolProfile string) ([]string, error) {
	task := rlm.Task{
		Prompt:        strings.TrimSpace(query) + "\n\nFind the exact repository files or canonical notes that best answer this query. Use repo-aware tools first, inspect concrete evidence, and cite only relative repo or vault paths.",
		WorkspaceRoot: resolveContextWorkspace(workspacePath),
		MaxDepth:      1,
		MaxIterations: 8,
		MaxSubcalls:   8,
	}

	companionDB, companionClose, err := openRLMCompanionDB(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if companionClose != nil {
		defer func() { _ = companionClose() }()
	}

	bootstrapper := rlmenv.NewBootstrapper(rlmenv.BootstrapConfig{
		AppConfig:   cfg,
		VaultPath:   strings.TrimSpace(vaultPath),
		CompanionDB: companionDB,
	})
	env, err := bootstrapper.Build(ctx, task)
	if err != nil {
		return nil, err
	}
	env.Tools = rlmenv.FilterTools(env.Tools, toolProfile)

	var runRecursive func(context.Context, rlm.Task, rlm.Environment) (rlm.Result, error)
	runRecursive = func(runCtx context.Context, currentTask rlm.Task, currentEnv rlm.Environment) (rlm.Result, error) {
		currentTask, currentEnv = applyRLMScoutRole(currentTask, currentEnv)
		currentAdapter := rlmenv.NewReadOnlyAdapter(cfg, currentTask.WorkspaceRoot, strings.TrimSpace(vaultPath), companionDB, currentEnv)
		currentAdapter.SetSubcall(runRecursive)
		runner := chooseRLMRunner("llm", currentAdapter, currentTask, currentEnv, "", "", "", "", 0, true, string(rlm.RouteProfileAuto), string(rlm.PlanModeFree))
		return runner.Run(runCtx, currentTask, currentEnv)
	}

	result, err := runRecursive(ctx, task, env)
	if err != nil {
		return nil, err
	}
	paths := append([]string(nil), result.RetrievedPaths...)
	if limit > 0 && len(paths) > limit {
		paths = paths[:limit]
	}
	return paths, nil
}

func runRLMStagedEvalMode(ctx context.Context, cfg config.Config, workspacePath, vaultPath, query string, limit int) ([]string, error) {
	task := rlm.Task{
		Prompt:        strings.TrimSpace(query) + "\n\nFind the exact repository files or canonical notes that best answer this query. Use repo-aware tools first, inspect concrete evidence, and cite only relative repo or vault paths.",
		WorkspaceRoot: resolveContextWorkspace(workspacePath),
		MaxDepth:      1,
		MaxIterations: 8,
		MaxSubcalls:   8,
	}

	companionDB, companionClose, err := openRLMCompanionDB(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if companionClose != nil {
		defer func() { _ = companionClose() }()
	}

	bootstrapper := rlmenv.NewBootstrapper(rlmenv.BootstrapConfig{
		AppConfig:   cfg,
		VaultPath:   strings.TrimSpace(vaultPath),
		CompanionDB: companionDB,
	})
	env, err := bootstrapper.Build(ctx, task)
	if err != nil {
		return nil, err
	}

	var runRecursive func(context.Context, rlm.Task, rlm.Environment) (rlm.Result, error)
	runRecursive = func(runCtx context.Context, currentTask rlm.Task, currentEnv rlm.Environment) (rlm.Result, error) {
		currentTask, currentEnv = applyRLMScoutRole(currentTask, currentEnv)
		currentAdapter := rlmenv.NewReadOnlyAdapter(cfg, currentTask.WorkspaceRoot, strings.TrimSpace(vaultPath), companionDB, currentEnv)
		currentAdapter.SetSubcall(runRecursive)
		runner := chooseRLMRunner("llm", currentAdapter, currentTask, currentEnv, "", "", "", "", 0, true, string(rlm.RouteProfileCodeRetrieval), string(rlm.PlanModeStaged))
		return runner.Run(runCtx, currentTask, currentEnv)
	}

	result, err := runRecursive(ctx, task, env)
	if err != nil {
		return nil, err
	}
	paths := append([]string(nil), result.RetrievedPaths...)
	if limit > 0 && len(paths) > limit {
		paths = paths[:limit]
	}
	return paths, nil
}

func resolveAgentctlRepoRoot() (string, error) {
	if _, file, _, ok := runtime.Caller(0); ok {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
		if _, err := os.Stat(filepath.Join(candidate, "skills", "code_semantic_search", "main.go")); err == nil {
			return candidate, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "skills", "code_semantic_search", "main.go")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("resolve agentctl repo root: could not locate skills/code_semantic_search/main.go")
}

func extractSemanticSearchResultPaths(results []struct {
	Path string `json:"path"`
},
) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(results))
	for _, result := range results {
		path := filepath.ToSlash(strings.TrimSpace(result.Path))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func extractRepoAnchorPaths(anchors []repoquery.Anchor) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		path := filepath.ToSlash(strings.TrimSpace(anchor.Path))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func extractACAResultPaths(result contextplane.RetrievalResult, limit int, opts contextplane.RetrievalOptions) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, limit)
	appendPath := func(path string) {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	appendRef := func(ref string) {
		ref = strings.TrimSpace(ref)
		switch {
		case strings.HasPrefix(ref, "path:"):
			appendPath(strings.TrimPrefix(ref, "path:"))
		case strings.HasPrefix(ref, "note:"):
			appendPath(strings.TrimPrefix(ref, "note:"))
		}
	}

	if opts.IncludeControlPlaneRefs {
		if result.TopOfMind != nil {
			for _, ref := range result.TopOfMind.RelevantRefs {
				appendRef(ref)
			}
		}
		if result.LatestHandoff != nil {
			for _, path := range result.LatestHandoff.Handoff.FilesTouched {
				appendPath(path)
			}
			for _, ref := range result.LatestHandoff.Handoff.EvidenceRefs {
				appendRef(ref)
			}
		}
	}
	if opts.IncludeVaultHits {
		for _, hit := range result.VaultHits {
			appendPath(hit.Path)
			for _, repoPath := range hit.RepoPaths {
				appendPath(repoPath)
			}
		}
	}
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func dagBudget(limit int) int {
	if limit <= 0 {
		return 40
	}
	if limit*8 < 40 {
		return 40
	}
	return limit * 8
}

func slicesContain(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func hasMode(modes []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, mode := range modes {
		if mode == target {
			return true
		}
	}
	return false
}

func ensureTopOfMindForEval(ctx context.Context, storageRoot, workspacePath string, store *contextplane.WorkspaceStore) error {
	if _, err := store.LoadTopOfMind(); err == nil {
		return nil
	}
	taskStore, err := tasks.Open(ctx, storageRoot)
	if err != nil {
		return err
	}
	defer func() { _ = taskStore.Close() }()
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		return err
	}
	defer func() { _ = sessionStore.Close() }()
	orienter := contextplane.NewOrienter(taskStore, sessionStore)
	top, err := orienter.Build(ctx, workspacePath)
	if err != nil {
		return err
	}
	_, err = store.SaveTopOfMind(top)
	return err
}

func extractSearchHitPaths(hits []obsidianindex.SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, filepath.ToSlash(hit.Path))
	}
	return out
}

func extractRetrievalHitPaths(hits []contextplane.RetrievalHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		if path := filepath.ToSlash(strings.TrimSpace(hit.Path)); path != "" {
			out = append(out, path)
		}
		for _, repoPath := range hit.RepoPaths {
			repoPath = filepath.ToSlash(strings.TrimSpace(repoPath))
			if repoPath != "" {
				out = append(out, repoPath)
			}
		}
	}
	return uniqueStrings(out)
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func filterEvalPaths(paths []string, canonicalOnly bool) []string {
	if !canonicalOnly {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		if strings.HasPrefix(path, "notes/") || strings.HasPrefix(path, "00-home/") || strings.HasPrefix(path, "atlas/") {
			out = append(out, path)
		}
	}
	return out
}

func saveEvalOutputs(workspacePath, saveDir, suite string, result retrievaleval.RunResult, markdown string) (string, string, error) {
	dir := strings.TrimSpace(saveDir)
	if dir == "" {
		dir = filepath.Join(resolveContextWorkspace(workspacePath), ".agentctl", "exports", "evals")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	stamp := result.GeneratedAt.UTC().Format("20060102T150405Z")
	base := sanitizeEvalName(suite)
	jsonPath := filepath.Join(dir, base+"-"+stamp+".json")
	markdownPath := filepath.Join(dir, base+"-"+stamp+".md")
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", "", err
	}
	body = append(body, '\n')
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(markdownPath, []byte(markdown), 0o644); err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

func sanitizeEvalName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "eval"
	}
	return out
}

func init() {
	rootCmd.AddCommand(newEvalCommand())
}
