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
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
	refsnapshot "github.com/jkatigb/agentctl/internal/refactor/snapshot"
	refsnapshotstore "github.com/jkatigb/agentctl/internal/refactor/snapshotstore"
	refstatus "github.com/jkatigb/agentctl/internal/refactor/status"
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
