package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/cmd/agentctl/cmd/memorycmd"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	refchanges "github.com/jkatigb/agentctl/internal/refactor/changes"
	refdeps "github.com/jkatigb/agentctl/internal/refactor/deps"
	refevidence "github.com/jkatigb/agentctl/internal/refactor/evidence"
	refhot "github.com/jkatigb/agentctl/internal/refactor/hot"
	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
	refsnapshot "github.com/jkatigb/agentctl/internal/refactor/snapshot"
	refsnapshotstore "github.com/jkatigb/agentctl/internal/refactor/snapshotstore"
	refstatus "github.com/jkatigb/agentctl/internal/refactor/status"
	"github.com/jkatigb/agentctl/internal/repoquery"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newRefactorCommand())
}

func newRefactorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refactor",
		Short: "Single-language refactor hotspot analysis and shortlist advice",
	}
	cmd.AddCommand(
		newRefactorStatusCommand(),
		newRefactorSnapshotCommand(),
		newRefactorDepsCommand(),
		newRefactorChangesCommand(),
		newRefactorHotCommand(),
		newRefactorEvidenceCommand(),
		newRefactorScoutCommand(),
		newRefactorAdvisorCommand(),
	)
	return cmd
}

func newRefactorStatusCommand() *cobra.Command {
	var (
		path         string
		workspace    string
		language     string
		includeTests bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show refactor index/fallback status for a scoped path",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRefactorStatus(cmd, workspace, path, language, includeTests)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "File or directory to analyze")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&language, "language", "auto", "Single language to analyze (auto|go|python|javascript|typescript|elixir)")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include test files")
	return cmd
}

func newRefactorSnapshotCommand() *cobra.Command {
	var (
		path         string
		workspace    string
		language     string
		includeTests bool
	)

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Persist a deterministic refactor scope snapshot",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRefactorSnapshot(cmd, workspace, path, language, includeTests)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "File or directory to analyze")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&language, "language", "auto", "Single language to analyze (auto|go|python|javascript|typescript|elixir)")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include test files")
	return cmd
}

func newRefactorDepsCommand() *cobra.Command {
	var (
		path         string
		workspace    string
		language     string
		includeTests bool
		seeds        []string
		query        string
		seedLimit    int
		edgeSets     []string
		edgeTypes    []string
		direction    string
		depth        int
		budget       int
		perNodeCap   int
	)

	cmd := &cobra.Command{
		Use:   "deps",
		Short: "Expand forward or reverse repoindex dependencies for a scoped seed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRefactorDeps(cmd, workspace, path, language, includeTests, seeds, query, seedLimit, edgeSets, edgeTypes, direction, depth, budget, perNodeCap)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "File or directory scope used for seed resolution and reporting")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&language, "language", "auto", "Single language to analyze (auto|go|python|javascript|typescript|elixir)")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include test files")
	cmd.Flags().StringArrayVar(&seeds, "seed", nil, "Explicit repoindex node ID seed (repeatable)")
	cmd.Flags().StringVar(&query, "query", "", "Search query used to resolve seed nodes within the scoped path")
	cmd.Flags().IntVar(&seedLimit, "seed-limit", 5, "Maximum resolved seed nodes when using --query")
	cmd.Flags().StringArrayVar(&edgeSets, "edge-set", []string{"structural"}, "Named edge set to traverse (structural|doc|all)")
	cmd.Flags().StringArrayVar(&edgeTypes, "edge", nil, "Explicit edge types to traverse (repeatable)")
	cmd.Flags().StringVar(&direction, "direction", "out", "Traversal direction (out|in)")
	cmd.Flags().IntVar(&depth, "depth", 1, "Traversal depth")
	cmd.Flags().IntVar(&budget, "budget", 50, "Maximum nodes to retain in the traversal")
	cmd.Flags().IntVar(&perNodeCap, "per-node-cap", 50, "Maximum edges to follow per frontier node")
	return cmd
}

func newRefactorChangesCommand() *cobra.Command {
	var (
		path         string
		workspace    string
		language     string
		includeTests bool
		since        string
		maxFiles     int
		maxSymbols   int
	)

	cmd := &cobra.Command{
		Use:   "changes",
		Short: "Show changed files and symbols since a git ref or refactor snapshot",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRefactorChanges(cmd, workspace, path, language, includeTests, since, maxFiles, maxSymbols)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "File or directory scope used for the change comparison")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&language, "language", "auto", "Single language to analyze (auto|go|python|javascript|typescript|elixir)")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include test files")
	cmd.Flags().StringVar(&since, "since", "", "Git ref or refactor snapshot id (refsnap-...) to compare against")
	cmd.Flags().IntVar(&maxFiles, "max-files", 200, "Maximum changed files to return inline")
	cmd.Flags().IntVar(&maxSymbols, "max-symbols", 200, "Maximum changed symbols to return inline")
	return cmd
}

func newRefactorHotCommand() *cobra.Command {
	var (
		path         string
		workspace    string
		language     string
		includeTests bool
		since        string
		maxResults   int
		halfLifeDays int
	)

	cmd := &cobra.Command{
		Use:   "hot",
		Short: "Rank recently hot files in a scoped path from git churn",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRefactorHot(cmd, workspace, path, language, includeTests, since, maxResults, halfLifeDays)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "File or directory scope used for the hot ranking")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&language, "language", "auto", "Single language to analyze (auto|go|python|javascript|typescript|elixir)")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include test files")
	cmd.Flags().StringVar(&since, "since", "HEAD~20", "Git ref or refactor snapshot id (refsnap-...) used as the hot baseline")
	cmd.Flags().IntVar(&maxResults, "max-results", 20, "Maximum hot files to return")
	cmd.Flags().IntVar(&halfLifeDays, "half-life-days", 90, "Recency half-life in days for hot-file weighting")
	return cmd
}

func newRefactorEvidenceCommand() *cobra.Command {
	var (
		artifact   string
		snapshotID string
		full       bool
	)

	cmd := &cobra.Command{
		Use:   "evidence",
		Short: "Read a persisted refactor snapshot or scout evidence artifact",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRefactorEvidence(cmd, snapshotID, artifact, full)
		},
	}
	cmd.Flags().StringVar(&artifact, "artifact", "", "CAS digest for a refactor snapshot or scout evidence artifact")
	cmd.Flags().StringVar(&snapshotID, "snapshot-id", "", "Refactor snapshot id to inspect")
	cmd.Flags().BoolVar(&full, "full", false, "Include full snapshot file and symbol lists")
	return cmd
}

func newRefactorScoutCommand() *cobra.Command {
	var (
		path         string
		workspace    string
		language     string
		focus        string
		ruleSet      string
		minScore     int
		maxResults   int
		includeTests bool
	)

	cmd := &cobra.Command{
		Use:   "scout",
		Short: "Run the local structural refactor scout",
		RunE: func(cmd *cobra.Command, _ []string) error {
			input := map[string]any{
				"path":          path,
				"language":      language,
				"focus":         focus,
				"rule_set":      ruleSet,
				"min_score":     minScore,
				"max_results":   maxResults,
				"include_tests": includeTests,
			}
			return runRefactorSkill(cmd, workspace, "code/refactor_scout", input)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "File or directory to analyze")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace root override")
	cmd.Flags().StringVar(&language, "language", "", "Single language to analyze (required)")
	cmd.Flags().StringVar(&focus, "focus", "all", "Finding focus (all|slop)")
	cmd.Flags().StringVar(&ruleSet, "rule-set", "default", "Threshold profile (conservative|default|aggressive)")
	cmd.Flags().IntVar(&minScore, "min-score", 70, "Minimum finding score")
	cmd.Flags().IntVar(&maxResults, "max-results", 20, "Maximum findings to return")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include test files")
	_ = cmd.MarkFlagRequired("language")
	return cmd
}

func newRefactorAdvisorCommand() *cobra.Command {
	var (
		path          string
		workspace     string
		language      string
		focus         string
		ruleSet       string
		minScore      int
		maxFindings   int
		shortlistSize int
		provider      string
		model         string
		timeoutSec    int
		maxTokens     int
		temperature   float64
	)

	cmd := &cobra.Command{
		Use:   "advisor",
		Short: "Run the two-stage refactor advisor (local scout + remote shortlist ranking)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			input := map[string]any{
				"path":           path,
				"language":       language,
				"focus":          focus,
				"rule_set":       ruleSet,
				"min_score":      minScore,
				"max_findings":   maxFindings,
				"shortlist_size": shortlistSize,
				"provider":       provider,
				"model":          model,
				"timeout_sec":    timeoutSec,
				"max_tokens":     maxTokens,
				"temperature":    temperature,
			}
			return runRefactorSkill(cmd, workspace, "code/refactor_advisor", input)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "File or directory to analyze")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace root override")
	cmd.Flags().StringVar(&language, "language", "", "Single language to analyze (required)")
	cmd.Flags().StringVar(&focus, "focus", "all", "Finding focus (all|slop)")
	cmd.Flags().StringVar(&ruleSet, "rule-set", "default", "Threshold profile (conservative|default|aggressive)")
	cmd.Flags().IntVar(&minScore, "min-score", 70, "Minimum scout score")
	cmd.Flags().IntVar(&maxFindings, "max-findings", 8, "Maximum scout findings to send to the advisor")
	cmd.Flags().IntVar(&shortlistSize, "shortlist-size", 3, "Maximum prioritized recommendations to return")
	cmd.Flags().StringVar(&provider, "provider", "openrouter", "Second-stage provider")
	cmd.Flags().StringVar(&model, "model", "", "Second-stage model (default: provider-specific)")
	cmd.Flags().IntVar(&timeoutSec, "timeout-sec", 120, "Second-stage timeout in seconds")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 900, "Second-stage max output tokens")
	cmd.Flags().Float64Var(&temperature, "temperature", 0.1, "Second-stage sampling temperature")
	_ = cmd.MarkFlagRequired("language")
	return cmd
}

func runRefactorSkill(cmd *cobra.Command, workspace, skillName string, input map[string]any) error {
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal input: %w", err)
	}

	cfg := config.MustFromContext(cmd.Context())
	resolver := createSkillResolver(cfg)
	handle, err := resolver.Resolve(skillName)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	manifest, artifactPath, err := loadRefactorManifestAndArtifact(handle.ManifestPath, cwd)
	if err != nil {
		return err
	}

	runCtx := resolveWorkspaceContext(cmd.Context(), workspace)
	stdout, stderr, err := executeSkill(runCtx, manifest, artifactPath, inputBytes)
	if len(stderr) > 0 {
		if _, werr := cmd.ErrOrStderr().Write(append(stderr, '\n')); werr != nil {
			return werr
		}
	}
	if err != nil {
		return err
	}
	return memorycmd.WriteEnvelope(cmd.OutOrStdout(), stdout)
}

func runRefactorStatus(cmd *cobra.Command, workspace, path, language string, includeTests bool) error {
	ctx := cmd.Context()

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	scope, err := refscope.Resolve(refscope.Input{
		Workspace:    workspace,
		Path:         path,
		Language:     language,
		IncludeTests: includeTests,
	})
	if err != nil {
		return writeRefactorScopeError(cmd, "refactor.status", workspace, err)
	}

	status := refstatus.Evaluate(ctx, cfg.Storage.Root, scope)
	scope = status.Scope
	data := map[string]any{
		"scope":      status.Scope,
		"mode":       status.Mode,
		"reasons":    status.Reasons,
		"git":        status.Git,
		"repo_index": status.RepoIndex,
	}
	env := protocol.OK("refactor.status", data, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runRefactorSnapshot(cmd *cobra.Command, workspace, path, language string, includeTests bool) error {
	ctx := cmd.Context()

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	scope, err := refscope.Resolve(refscope.Input{
		Workspace:    workspace,
		Path:         path,
		Language:     language,
		IncludeTests: includeTests,
	})
	if err != nil {
		return writeRefactorScopeError(cmd, "refactor.snapshot", workspace, err)
	}

	status := refstatus.Evaluate(ctx, cfg.Storage.Root, scope)
	scope = status.Scope
	createdAt := time.Now().UTC()
	snapshotID := refsnapshot.GenerateID(createdAt)
	payload, err := refsnapshot.Builder{}.Build(ctx, refsnapshot.Input{
		SnapshotID:   snapshotID,
		CreatedAt:    createdAt,
		Scope:        scope,
		Status:       status,
		IncludeTests: includeTests,
	})
	if err != nil {
		if buildErr, ok := err.(*refsnapshot.BuildError); ok {
			env := protocol.Error("refactor.snapshot", protocol.ErrorCodeEARG, buildErr.Message, protocol.ErrorData{Hint: buildErr.Hint}, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace))
			if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
				return fmt.Errorf("write refactor snapshot error envelope: %w", writeErr)
			}
			return fmt.Errorf("build refactor snapshot: %w", err)
		}
		return fmt.Errorf("build refactor snapshot: %w", err)
	}

	artifact, err := persistRefactorSnapshotArtifact(ctx, cfg.Paths.CAS, payload)
	if err != nil {
		return fmt.Errorf("persist refactor snapshot artifact: %w", err)
	}

	metaStore, err := refsnapshotstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open refactor snapshot store: %w", err)
	}
	defer metaStore.Close()
	if err := metaStore.Put(ctx, refsnapshotstore.Record{
		SnapshotID:     payload.SnapshotID,
		Workspace:      payload.Scope.Workspace,
		RepoRoot:       payload.Scope.RepoRoot,
		Path:           payload.Scope.Path,
		Language:       payload.Scope.Language,
		IncludeTests:   includeTests,
		Mode:           string(payload.Mode),
		GitHeadSHA:     payload.Git.HeadSHA,
		IndexHeadSHA:   payload.RepoIndex.HeadSHA,
		ArtifactDigest: artifact,
		FileCount:      payload.Summary.FileCount,
		SymbolCount:    payload.Summary.SymbolCount,
		CreatedAt:      payload.CreatedAt,
	}); err != nil {
		return fmt.Errorf("persist refactor snapshot metadata: %w", err)
	}

	data := map[string]any{
		"snapshot_id": payload.SnapshotID,
		"mode":        payload.Mode,
		"scope":       payload.Scope,
		"summary":     payload.Summary,
		"artifact":    artifact,
		"created_at":  payload.CreatedAt,
	}
	env := protocol.OK("refactor.snapshot", data, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace), protocol.WithCASDigest(artifact))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runRefactorDeps(cmd *cobra.Command, workspace, path, language string, includeTests bool, seeds []string, query string, seedLimit int, edgeSets, edgeTypes []string, direction string, depth, budget, perNodeCap int) error {
	ctx := cmd.Context()

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	scope, err := refscope.Resolve(refscope.Input{
		Workspace:    workspace,
		Path:         path,
		Language:     language,
		IncludeTests: includeTests,
	})
	if err != nil {
		return writeRefactorScopeError(cmd, "refactor.deps", workspace, err)
	}

	status := refstatus.Evaluate(ctx, cfg.Storage.Root, scope)
	scope = status.Scope
	if !status.RepoIndex.Available {
		hint := fmt.Sprintf("Build the repo index first, for example: agentctl index repo build --workspace %s", scope.Workspace)
		env := protocol.Error("refactor.deps", protocol.ErrorCodeERuntime, "refactor deps requires an available repo index", protocol.ErrorData{Hint: hint}, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write refactor deps error envelope: %w", writeErr)
		}
		return fmt.Errorf("refactor deps requires repo index")
	}

	store, err := repoindex.Open(ctx, cfg.Storage.Root, scope.Workspace)
	if err != nil {
		return fmt.Errorf("open repoindex store: %w", err)
	}
	defer store.Close()

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	request, err := refdeps.BuildRequest(ctx, service, refdeps.Input{
		Scope:      scope,
		Status:     status,
		Seeds:      seeds,
		Query:      query,
		SeedLimit:  seedLimit,
		EdgeSets:   edgeSets,
		EdgeTypes:  edgeTypes,
		Direction:  direction,
		Depth:      depth,
		Budget:     budget,
		PerNodeCap: perNodeCap,
	})
	if err != nil {
		if buildErr, ok := err.(*refdeps.BuildError); ok {
			env := protocol.Error("refactor.deps", protocol.ErrorCodeEARG, buildErr.Message, protocol.ErrorData{Hint: buildErr.Hint}, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace))
			if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
				return fmt.Errorf("write refactor deps error envelope: %w", writeErr)
			}
			return fmt.Errorf("build refactor deps request: %w", err)
		}
		env := protocol.Error("refactor.deps", protocol.ErrorCodeEARG, err.Error(), protocol.ErrorData{Hint: "Use --direction in|out, --edge-set structural|doc|all, or explicit repoindex edge types with --edge."}, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write refactor deps error envelope: %w", writeErr)
		}
		return fmt.Errorf("build refactor deps request: %w", err)
	}

	result, err := service.ExpandWithProjection(ctx, request.Request)
	if err != nil {
		return fmt.Errorf("expand refactor deps graph: %w", err)
	}

	data := map[string]any{
		"scope":           scope,
		"index_mode":      string(request.IndexMode),
		"reasons":         request.Reasons,
		"seed_query":      request.SeedQuery,
		"seed_candidates": request.SeedCandidates,
		"seeds":           request.Request.Seeds,
		"edge_sets":       edgeSets,
		"edges":           repoquery.EdgeTypeValues(request.Request.EdgeTypes),
		"direction":       string(request.Request.Direction),
		"depth":           request.Request.Depth,
		"budget":          request.Request.Budget,
		"per_node_cap":    request.Request.PerNodeCap,
		"result":          result.Result,
		"anchors":         result.Anchors,
	}
	env := protocol.OK("refactor.deps", data, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runRefactorChanges(cmd *cobra.Command, workspace, path, language string, includeTests bool, since string, maxFiles, maxSymbols int) error {
	ctx := cmd.Context()

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	scope, err := refscope.Resolve(refscope.Input{
		Workspace:    workspace,
		Path:         path,
		Language:     language,
		IncludeTests: includeTests,
	})
	if err != nil {
		return writeRefactorScopeError(cmd, "refactor.changes", workspace, err)
	}

	status := refstatus.Evaluate(ctx, cfg.Storage.Root, scope)
	scope = status.Scope
	result, err := refchanges.Build(ctx, cfg.Storage.Root, cfg.Paths.CAS, time.Now().UTC(), refchanges.Options{
		Scope:        scope,
		IncludeTests: includeTests,
		Since:        since,
		MaxFiles:     maxFiles,
		MaxSymbols:   maxSymbols,
	})
	if err != nil {
		if buildErr, ok := err.(*refchanges.BuildError); ok {
			env := protocol.Error("refactor.changes", protocol.ErrorCodeEARG, buildErr.Message, protocol.ErrorData{Hint: buildErr.Hint}, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace))
			if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
				return fmt.Errorf("write refactor changes error envelope: %w", writeErr)
			}
			return fmt.Errorf("build refactor changes: %w", err)
		}
		return fmt.Errorf("build refactor changes: %w", err)
	}

	data := map[string]any{
		"scope":      scope,
		"index_mode": string(status.Mode),
		"reasons":    status.Reasons,
		"since":      result.Since,
		"summary":    result.Summary,
		"files":      result.Files,
		"symbols":    result.Symbols,
	}
	env := protocol.OK("refactor.changes", data, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runRefactorHot(cmd *cobra.Command, workspace, path, language string, includeTests bool, since string, maxResults, halfLifeDays int) error {
	ctx := cmd.Context()

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	scope, err := refscope.Resolve(refscope.Input{
		Workspace:    workspace,
		Path:         path,
		Language:     language,
		IncludeTests: includeTests,
	})
	if err != nil {
		return writeRefactorScopeError(cmd, "refactor.hot", workspace, err)
	}

	status := refstatus.Evaluate(ctx, cfg.Storage.Root, scope)
	scope = status.Scope
	result, err := refhot.Build(ctx, cfg.Storage.Root, refhot.Options{
		Scope:        scope,
		IncludeTests: includeTests,
		Since:        since,
		MaxResults:   maxResults,
		HalfLifeDays: halfLifeDays,
		Now:          time.Now().UTC(),
	})
	if err != nil {
		if buildErr, ok := err.(*refhot.BuildError); ok {
			env := protocol.Error("refactor.hot", protocol.ErrorCodeEARG, buildErr.Message, protocol.ErrorData{Hint: buildErr.Hint}, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace))
			if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
				return fmt.Errorf("write refactor hot error envelope: %w", writeErr)
			}
			return fmt.Errorf("build refactor hot: %w", err)
		}
		return fmt.Errorf("build refactor hot: %w", err)
	}

	data := map[string]any{
		"scope":      scope,
		"index_mode": string(status.Mode),
		"reasons":    status.Reasons,
		"since":      result.Since,
		"summary":    result.Summary,
		"files":      result.Files,
	}
	env := protocol.OK("refactor.hot", data, protocol.WithSource("cli"), protocol.WithWorkspace(scope.Workspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runRefactorEvidence(cmd *cobra.Command, snapshotID, artifact string, full bool) error {
	ctx := cmd.Context()

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	result, err := refevidence.Load(ctx, cfg.Storage.Root, cfg.Paths.CAS, refevidence.Options{
		SnapshotID: snapshotID,
		Artifact:   artifact,
	})
	if err != nil {
		if loadErr, ok := err.(*refevidence.LoadError); ok {
			code := protocol.ErrorCodeEARG
			if loadErr.Kind == refevidence.ErrorKindNotFound {
				code = protocol.ErrorCodeENotFound
			}
			env := protocol.Error("refactor.evidence", code, loadErr.Message, protocol.ErrorData{Hint: loadErr.Hint}, protocol.WithSource("cli"), protocol.WithWorkspace(refactorEvidenceWorkspace(result, ".")))
			if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
				return fmt.Errorf("write refactor evidence error envelope: %w", writeErr)
			}
			return fmt.Errorf("load refactor evidence: %w", err)
		}
		return fmt.Errorf("load refactor evidence: %w", err)
	}

	data := map[string]any{
		"kind":     result.Kind,
		"artifact": result.Artifact,
	}
	if result.SnapshotRecord != nil {
		data["snapshot_record"] = result.SnapshotRecord
	}
	switch result.Kind {
	case refevidence.ArtifactKindSnapshot:
		payload := result.Snapshot
		data["snapshot_id"] = payload.SnapshotID
		data["mode"] = payload.Mode
		data["scope"] = payload.Scope
		data["summary"] = payload.Summary
		data["git"] = payload.Git
		data["repo_index"] = payload.RepoIndex
		if full {
			data["files"] = payload.Files
			data["symbols"] = payload.Symbols
		}
	case refevidence.ArtifactKindHotspotPack:
		pack := result.HotspotPack
		data["snapshot_id"] = pack.SnapshotID
		data["snapshot_artifact"] = pack.SnapshotArtifact
		data["index_mode"] = pack.IndexMode
		data["reasons"] = pack.Reasons
		data["summary"] = map[string]any{
			"hotspot_count": len(pack.Hotspots),
		}
		data["hotspots"] = pack.Hotspots
	}
	env := protocol.OK("refactor.evidence", data, protocol.WithSource("cli"), protocol.WithWorkspace(refactorEvidenceWorkspace(result, ".")), protocol.WithCASDigest(result.Artifact))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func refactorWorkspaceValue(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		workspace = "."
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return workspace
	}
	return absWorkspace
}

func refactorEvidenceWorkspace(result refevidence.Result, fallback string) string {
	switch {
	case result.Snapshot != nil && strings.TrimSpace(result.Snapshot.Scope.Workspace) != "":
		return result.Snapshot.Scope.Workspace
	case result.SnapshotRecord != nil && strings.TrimSpace(result.SnapshotRecord.Workspace) != "":
		return result.SnapshotRecord.Workspace
	default:
		return refactorWorkspaceValue(fallback)
	}
}

func writeRefactorScopeError(cmd *cobra.Command, commandName, workspace string, err error) error {
	absWorkspace := refactorWorkspaceValue(workspace)
	message := "refactor scope resolution failed"
	data := protocol.ErrorData{}
	if resolveErr, ok := err.(*refscope.ResolveError); ok {
		message = resolveErr.Message
		data.Hint = resolveErr.Hint
	}
	env := protocol.Error(commandName, protocol.ErrorCodeEARG, message, data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
		return fmt.Errorf("write %s error envelope: %w", commandName, writeErr)
	}
	return fmt.Errorf("resolve refactor scope: %w", err)
}

func persistRefactorSnapshotArtifact(ctx context.Context, casRoot string, payload refsnapshot.Payload) (string, error) {
	casRoot = strings.TrimSpace(casRoot)
	if casRoot == "" {
		return "", fmt.Errorf("cas root is required")
	}
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	obj, err := store.Put(ctx, bytes.NewReader(append(body, '\n')), "application/json", []string{"refactor-snapshot", payload.Scope.Language})
	if err != nil {
		return "", err
	}
	return obj.Digest, nil
}

func loadRefactorManifestAndArtifact(manifestPath, cwd string) (skill.Manifest, string, error) {
	entryRoots := refactorEntryRoots(manifestPath, cwd)
	var firstErr error
	for _, root := range entryRoots {
		manifest, artifactPath, err := skill.LoadManifestAndArtifact(manifestPath, skill.ArtifactOptions{
			PreferCGO: buildinfo.IsCGO(),
			EntryRoot: root,
		})
		if err == nil {
			return manifest, artifactPath, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		if !errors.Is(err, skill.ErrArtifactsMissing) {
			return skill.Manifest{}, "", err
		}
	}
	if firstErr != nil {
		return skill.Manifest{}, "", firstErr
	}
	return skill.Manifest{}, "", fmt.Errorf("resolve skill artifact for %s", manifestPath)
}

func refactorEntryRoots(manifestPath, cwd string) []string {
	roots := make([]string, 0, 3)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		for _, existing := range roots {
			if existing == path {
				return
			}
		}
		roots = append(roots, path)
	}
	add(cwd)
	dir := filepath.Dir(manifestPath)
	if filepath.Base(filepath.Dir(dir)) == "skills" {
		add(filepath.Dir(filepath.Dir(dir)))
	}
	add(dir)
	return roots
}
