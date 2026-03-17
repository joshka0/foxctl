package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/evals/correctioneval"
	"github.com/jkatigb/agentctl/internal/evals/retrievaleval"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/repoquery"
	"github.com/jkatigb/agentctl/internal/rlm"
	rlmenv "github.com/jkatigb/agentctl/internal/rlm/env"
	"github.com/jkatigb/agentctl/internal/storage"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/spf13/cobra"
)

func newEvalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run lightweight evaluation suites",
	}
	cmd.AddCommand(newEvalRetrievalCommand())
	cmd.AddCommand(newEvalCorrectionsCommand())
	return cmd
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
	var rebuildIndex bool
	var canonicalOnly bool
	var save bool
	var saveDir string

	cmd := &cobra.Command{
		Use:   "retrieval",
		Short: "Evaluate lexical, semantic, and blended retrieval against a curated query suite",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(suiteRef) == "" {
				return fmt.Errorf("--suite is required")
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
				if hasMode(selectedModes, "aca_control_only") {
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, q.Query, limit, contextplane.RetrievalOptions{
						IncludeTopOfMindResult:  true,
						IncludeLatestHandoff:    true,
						IncludeVaultHits:        false,
						UseRelevantRefBoost:     false,
						UseHandoffRefBoost:      false,
						UseCodeHints:            false,
						UseSemanticVaultSearch:  false,
						IncludeControlPlaneRefs: true,
					})
					qr.Modes["aca_control_only"] = retrievaleval.EvaluateMode("aca_control_only", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_vault_only") {
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, q.Query, limit, contextplane.RetrievalOptions{
						IncludeTopOfMindResult:  false,
						IncludeLatestHandoff:    false,
						IncludeVaultHits:        true,
						UseRelevantRefBoost:     false,
						UseHandoffRefBoost:      false,
						UseCodeHints:            false,
						UseSemanticVaultSearch:  true,
						IncludeControlPlaneRefs: false,
					})
					qr.Modes["aca_vault_only"] = retrievaleval.EvaluateMode("aca_vault_only", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_repo_hints") {
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, q.Query, limit, contextplane.RetrievalOptions{
						IncludeTopOfMindResult:  false,
						IncludeLatestHandoff:    false,
						IncludeVaultHits:        true,
						UseRelevantRefBoost:     false,
						UseHandoffRefBoost:      false,
						UseCodeHints:            true,
						UseSemanticVaultSearch:  true,
						IncludeControlPlaneRefs: false,
					})
					qr.Modes["aca_repo_hints"] = retrievaleval.EvaluateMode("aca_repo_hints", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_canonical_only") {
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, q.Query, limit, contextplane.RetrievalOptions{
						IncludeTopOfMindResult:  false,
						IncludeLatestHandoff:    false,
						IncludeVaultHits:        true,
						UseRelevantRefBoost:     false,
						UseHandoffRefBoost:      false,
						UseCodeHints:            true,
						UseSemanticVaultSearch:  true,
						AllowedTrusts:           []string{"canonical", "reviewed"},
						IncludeControlPlaneRefs: false,
					})
					qr.Modes["aca_canonical_only"] = retrievaleval.EvaluateMode("aca_canonical_only", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_package_fallback") {
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, q.Query, limit, contextplane.RetrievalOptions{
						IncludeTopOfMindResult:  false,
						IncludeLatestHandoff:    false,
						IncludeVaultHits:        true,
						UseRelevantRefBoost:     false,
						UseHandoffRefBoost:      false,
						UseCodeHints:            true,
						UseSemanticVaultSearch:  true,
						UsePackageNoteFallback:  true,
						AllowedTrusts:           []string{"canonical", "reviewed"},
						IncludeControlPlaneRefs: false,
					})
					qr.Modes["aca_package_fallback"] = retrievaleval.EvaluateMode("aca_package_fallback", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_query_typed") {
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, q.Query, limit, contextplane.RetrievalOptions{
						IncludeTopOfMindResult:  false,
						IncludeLatestHandoff:    false,
						IncludeVaultHits:        true,
						UseRelevantRefBoost:     false,
						UseHandoffRefBoost:      false,
						UseCodeHints:            true,
						UseSemanticVaultSearch:  true,
						AllowedTrusts:           []string{"canonical", "reviewed"},
						UseQueryTypeBias:        true,
						IncludeControlPlaneRefs: false,
					})
					qr.Modes["aca_query_typed"] = retrievaleval.EvaluateMode("aca_query_typed", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_default") {
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, q.Query, limit, acaDefaultEvalOptions())
					qr.Modes["aca_default"] = retrievaleval.EvaluateMode("aca_default", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_cochange") {
					opts := acaDefaultEvalOptions()
					opts.UseCoChangePrior = true
					opts.UseContinuityBundles = false
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, q.Query, limit, opts)
					qr.Modes["aca_cochange"] = retrievaleval.EvaluateMode("aca_cochange", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "aca_cochange_continuity") {
					opts := acaDefaultEvalOptions()
					opts.UseCoChangePrior = true
					opts.UseContinuityBundles = true
					paths, err := runACAEvalMode(ctx, workspaceStore, index, repo, semanticProvider, q.Query, limit, opts)
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
			data := map[string]any{
				"suite_path": suitePath,
				"result":     runResult,
			}
			markdown := retrievaleval.RenderMarkdown(runResult)
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
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("eval/retrieval", data, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&suiteRef, "suite", "", "Suite name (agentctl, praze) or explicit YAML path")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum hits per retrieval mode")
	cmd.Flags().StringVar(&format, "format", "markdown", "Output companion format: markdown or json")
	cmd.Flags().StringSliceVar(&modes, "mode", []string{"baseline", "lexical", "semantic", "blended"}, "Retrieval modes to evaluate (also available: skill_default, skill_context, skill_default_plus_context, aca_control_only, aca_vault_only, aca_repo_hints, aca_canonical_only, aca_package_fallback, aca_query_typed, aca_default, aca_cochange, aca_cochange_continuity, cochange_artifacts, repoindex_search, repoindex_dag, rlm_llm, rlm_llm_codeintel, rlm_llm_code_staged)")
	cmd.Flags().BoolVar(&rebuildIndex, "rebuild-index", true, "Rebuild the vault index before evaluation")
	cmd.Flags().BoolVar(&canonicalOnly, "canonical-only", true, "Exclude inbox draft hits from evaluation paths")
	cmd.Flags().BoolVar(&save, "save", false, "Save JSON and Markdown eval outputs under the workspace exports directory")
	cmd.Flags().StringVar(&saveDir, "save-dir", "", "Override save directory (default: <workspace>/.agentctl/exports/evals)")
	return cmd
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
	query string,
	limit int,
	opts contextplane.RetrievalOptions,
) ([]string, error) {
	if store == nil {
		return nil, fmt.Errorf("workspace store unavailable")
	}
	result, err := store.RetrieveWithOptions(ctx, index, repo, semanticProvider, query, limit, opts)
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

func acaDefaultEvalOptions() contextplane.RetrievalOptions {
	opts := contextplane.DefaultRetrievalOptions()
	opts.IncludeTopOfMindResult = true
	opts.IncludeLatestHandoff = true
	opts.IncludeVaultHits = true
	opts.UseRelevantRefBoost = true
	opts.UseHandoffRefBoost = true
	opts.UseCodeHints = true
	opts.UseSemanticVaultSearch = true
	opts.IncludeControlPlaneRefs = false
	return opts
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
