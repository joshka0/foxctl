package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/cmd/agentctl/cmd/memorycmd"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/config"
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
		newRefactorScoutCommand(),
		newRefactorAdvisorCommand(),
	)
	return cmd
}

func newRefactorScoutCommand() *cobra.Command {
	var (
		path         string
		workspace    string
		language     string
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
