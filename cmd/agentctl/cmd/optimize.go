package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/runservice"
	storagents "github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/cache"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/jkatigb/agentctl/internal/verification"
	"github.com/spf13/cobra"
)

const (
	optimizeCommand         = "optimize"
	optimizeDatasetCommand  = "optimize.dataset"
	optimizePatternsCommand = "optimize.patterns"
	optimizePromptCommand   = "optimize.prompt"
	optimizePromptsCommand  = "optimize.prompts"
	optimizeWeightsCommand  = "optimize.weights"
	optimizeAnalyzeCommand  = "optimize.analyze"
)

func absWorkspacePath(workspace string) string {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return workspace
	}
	return abs
}

func absWorkspaceOrWriteError(out io.Writer, command, workspace string) (string, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return "", writeOptimizeError(out, command, fmt.Sprintf("resolve workspace: %v", err))
	}
	return abs, nil
}

func newOptimizeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "optimize",
		Short: "Agent optimization and learning commands",
		Long: `Commands for optimizing agent performance using collected trajectory data.

Supports online pattern learning, offline batch optimization (bootstrap few-shot,
prompt optimization), and feedback-driven weight learning.`,
	}
	cmd.AddCommand(
		newOptimizeDatasetCommand(),
		newOptimizePatternsCommand(),
		newOptimizeBootstrapCommand(),
		newOptimizePromptCommand(),
		newOptimizePromptsCommand(),
		newOptimizeAnalyzeCommand(),
		newOptimizeWeightsCommand(),
		newOptimizeReflectCommand(),
		newOptimizeFeedbackCommand(),
		newOptimizeSessionCommand(),
	)
	return cmd
}

// --- Dataset subcommand ---

func newOptimizeDatasetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dataset",
		Short: "Build optimizer-friendly datasets from captured sessions",
	}
	cmd.AddCommand(
		newOptimizeDatasetClaudeCommand(),
		newOptimizeDatasetExportCommand(),
		newOptimizeDatasetExportRankedCommand(),
	)
	return cmd
}

func newOptimizeDatasetExportCommand() *cobra.Command {
	var (
		workspace       string
		project         string
		source          string
		category        string
		sessionIDs      []string
		limit           int
		includeTools    bool
		includeFiles    bool
		includeFeedback bool
		outputFile      string
		toCAS           bool
		dryRun          bool
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export Claude/Codex transcripts into JSONL training material",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizeDatasetCommand, workspace)
			if err != nil {
				return err
			}

			sessionStore, err := sessions.OpenFromConfig(ctx, cfg)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("open sessions store: %v", err))
			}
			defer sessionStore.Close() //nolint:errcheck

			var memStore *memory.Store
			if includeFeedback {
				memStore, err = memory.OpenFromConfig(ctx, cfg)
				if err != nil {
					return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("open memory store: %v", err))
				}
				defer memStore.Close() //nolint:errcheck
			}

			examples, err := optimization.ExportTranscriptDataset(ctx, sessionStore, memStore, optimization.TranscriptDatasetRequest{
				SessionIDs:      append([]string(nil), sessionIDs...),
				WorkspacePath:   absWorkspace,
				Project:         strings.TrimSpace(project),
				Source:          strings.TrimSpace(source),
				Category:        strings.TrimSpace(category),
				IncludeTools:    includeTools,
				IncludeFiles:    includeFiles,
				IncludeFeedback: includeFeedback,
				Limit:           limit,
			})
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("export transcript dataset: %v", err))
			}

			plan, err := planTranscriptDatasetExport(cmd.CommandPath(), absWorkspace, strings.TrimSpace(project), strings.TrimSpace(source), optimization.NormalizeTranscriptCategory(category), includeTools, includeFiles, includeFeedback, outputFile, toCAS, examples)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("plan transcript dataset export: %v", err))
			}
			data, artifact, err := applyTranscriptDatasetExport(ctx, cfg.Paths.CAS, plan, dryRun)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, err.Error())
			}
			return protocol.WriteOK(out, optimizeDatasetCommand, data,
				protocol.WithSource(map[bool]string{true: "plan", false: "run"}[dryRun]),
				protocol.WithWorkspace(absWorkspace),
				protocol.WithCASDigest(artifact),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&project, "project", "", "Optional project name filter")
	cmd.Flags().StringVar(&source, "source", "all", "Session source filter: all, claude, or codex")
	cmd.Flags().StringVar(&category, "category", "all", "Transcript category filter: all, coder_impl, ops_infra, release_workflow, or continuation")
	cmd.Flags().StringSliceVar(&sessionIDs, "session-id", nil, "Specific session ID to export (repeatable)")
	cmd.Flags().IntVar(&limit, "limit", 1000, "Maximum number of transcript examples to export")
	cmd.Flags().BoolVar(&includeTools, "include-tools", true, "Include tool usage metadata")
	cmd.Flags().BoolVar(&includeFiles, "include-files", true, "Include file-touch metadata")
	cmd.Flags().BoolVar(&includeFeedback, "include-feedback", true, "Join session_feedback labels when available")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "Write JSONL dataset to a file")
	cmd.Flags().BoolVar(&toCAS, "to-cas", false, "Write JSONL dataset to CAS and return a digest")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview transcript dataset export without writing output")
	return cmd
}

func countDatasetSessions(examples []optimization.TranscriptTrainingExample) int {
	seen := map[string]struct{}{}
	for _, example := range examples {
		if strings.TrimSpace(example.Metadata.SessionID) == "" {
			continue
		}
		seen[example.Metadata.SessionID] = struct{}{}
	}
	return len(seen)
}

type transcriptDatasetExportPlan struct {
	Data       map[string]any
	Examples   []optimization.TranscriptTrainingExample
	OutputFile string
	ToCAS      bool
	Candidate  string
}

func planTranscriptDatasetExport(
	commandPath string,
	workspace string,
	project string,
	source string,
	category string,
	includeTools bool,
	includeFiles bool,
	includeFeedback bool,
	outputFile string,
	toCAS bool,
	examples []optimization.TranscriptTrainingExample,
) (transcriptDatasetExportPlan, error) {
	body, err := optimization.BuildTranscriptDatasetJSONL(examples)
	if err != nil {
		return transcriptDatasetExportPlan{}, err
	}
	sum := sha256.Sum256(body)
	candidate := "sha256:" + hex.EncodeToString(sum[:])
	data := map[string]any{
		"operation":                 "dataset.export",
		"workspace_id":              workspace,
		"project":                   project,
		"source":                    source,
		"category":                  category,
		"session_count":             countDatasetSessions(examples),
		"example_count":             len(examples),
		"include_tools":             includeTools,
		"include_files":             includeFiles,
		"include_feedback":          includeFeedback,
		"artifact_digest_candidate": candidate,
		"cli_command":               commandPath,
	}
	if strings.TrimSpace(outputFile) != "" {
		data["output_file"] = strings.TrimSpace(outputFile)
	}
	if toCAS {
		data["to_cas"] = true
	}
	if strings.TrimSpace(outputFile) == "" && !toCAS {
		data["examples"] = examples
	}
	return transcriptDatasetExportPlan{
		Data:       data,
		Examples:   examples,
		OutputFile: strings.TrimSpace(outputFile),
		ToCAS:      toCAS,
		Candidate:  candidate,
	}, nil
}

func applyTranscriptDatasetExport(ctx context.Context, casRoot string, plan transcriptDatasetExportPlan, dryRun bool) (map[string]any, string, error) {
	data := cloneMap(plan.Data)
	data["dry_run"] = dryRun
	if dryRun {
		if plan.OutputFile != "" {
			data["would_write_file"] = true
		}
		if plan.ToCAS {
			data["would_write_cas"] = true
		}
		return data, "", nil
	}
	if plan.OutputFile != "" {
		if err := optimization.SaveTranscriptDatasetFile(plan.OutputFile, plan.Examples); err != nil {
			return nil, "", fmt.Errorf("write dataset file: %v", err)
		}
	}
	if plan.ToCAS {
		artifact, err := persistTranscriptDatasetArtifact(ctx, casRoot, plan.Examples)
		if err != nil {
			return nil, "", fmt.Errorf("persist dataset artifact: %v", err)
		}
		data["artifact"] = artifact
		return data, artifact, nil
	}
	if plan.OutputFile == "" {
		data["examples"] = plan.Examples
	}
	return data, "", nil
}

// --- Patterns subcommand ---

func newOptimizePatternsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "patterns",
		Short: "Manage learned tool patterns",
	}
	cmd.AddCommand(
		newOptimizePatternsListCommand(),
		newOptimizePatternsClearCommand(),
		newOptimizePatternsHintsCommand(),
	)
	return cmd
}

func newOptimizePatternsListCommand() *cobra.Command {
	var (
		workspace string
		agentRole string
		limit     int
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List learned patterns for an agent role",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePatternsCommand, err.Error())
			}

			patternStore, err := optimization.OpenPatternStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePatternsCommand, fmt.Sprintf("open pattern store: %v", err))
			}
			defer patternStore.Close() //nolint:errcheck

			patterns, err := patternStore.List(ctx, agentRole, limit)
			if err != nil {
				return writeOptimizeError(out, optimizePatternsCommand, fmt.Sprintf("list patterns: %v", err))
			}

			data := map[string]any{
				"operation":   "list",
				"agent_role":  agentRole,
				"count":       len(patterns),
				"patterns":    patterns,
				"cli_command": cmd.CommandPath(),
			}

			absWorkspace := absWorkspacePath(workspace)
			return protocol.WriteOK(out, optimizePatternsCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role to filter by (required)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum patterns to return")
	if err := cmd.MarkFlagRequired("role"); err != nil {
		panic(err)
	}
	return cmd
}

func newOptimizePatternsClearCommand() *cobra.Command {
	var (
		workspace string
		agentRole string
		confirm   bool
	)

	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear learned patterns",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			if !confirm {
				return writeOptimizeError(out, optimizePatternsCommand, "use --confirm to clear patterns")
			}

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePatternsCommand, err.Error())
			}

			patternStore, err := optimization.OpenPatternStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePatternsCommand, fmt.Sprintf("open pattern store: %v", err))
			}
			defer patternStore.Close() //nolint:errcheck

			if err := patternStore.Clear(ctx, agentRole); err != nil {
				return writeOptimizeError(out, optimizePatternsCommand, fmt.Sprintf("clear patterns: %v", err))
			}

			data := map[string]any{
				"operation":   "clear",
				"agent_role":  agentRole,
				"cleared":     true,
				"cli_command": cmd.CommandPath(),
			}

			absWorkspace := absWorkspacePath(workspace)
			return protocol.WriteOK(out, optimizePatternsCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role to clear (empty = all roles)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm clearing patterns")
	return cmd
}

func newOptimizePatternsHintsCommand() *cobra.Command {
	var (
		workspace   string
		agentRole   string
		taskContext string
	)

	cmd := &cobra.Command{
		Use:   "hints",
		Short: "Get tool hints based on learned patterns for a task context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePatternsCommand, err.Error())
			}

			patternStore, err := optimization.OpenPatternStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePatternsCommand, fmt.Sprintf("open pattern store: %v", err))
			}
			defer patternStore.Close() //nolint:errcheck

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePatternsCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck

			collector := optimization.NewMCPPatternCollector(patternStore, trajStore)
			hints, err := collector.GetHints(ctx, agentRole, taskContext)
			if err != nil {
				return writeOptimizeError(out, optimizePatternsCommand, fmt.Sprintf("get hints: %v", err))
			}

			formatted := collector.FormatHintsForPrompt(hints)

			data := map[string]any{
				"operation":    "hints",
				"agent_role":   agentRole,
				"task_context": taskContext,
				"hints":        hints,
				"formatted":    formatted,
				"cli_command":  cmd.CommandPath(),
			}

			absWorkspace := absWorkspacePath(workspace)
			return protocol.WriteOK(out, optimizePatternsCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role (required)")
	cmd.Flags().StringVar(&taskContext, "context", "", "Task context/description to get hints for (required)")
	if err := cmd.MarkFlagRequired("role"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("context"); err != nil {
		panic(err)
	}
	return cmd
}

// --- Bootstrap subcommand ---

func newOptimizeBootstrapCommand() *cobra.Command {
	var (
		workspace       string
		agentRole       string
		maxExamples     int
		minSuccessRate  float64
		diversityWeight float64
		lookbackDays    int
	)

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Generate few-shot examples from successful trajectories",
		Long: `Generate few-shot examples for an agent role by analyzing successful
trajectories. Examples are selected based on success rate, diversity, and recency.

The generated examples can be used to enhance agent prompts with real-world
successful patterns.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			start := time.Now()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizeCommand, workspace)
			if err != nil {
				return err
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck

			patternStore, err := optimization.OpenPatternStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("open pattern store: %v", err))
			}
			defer patternStore.Close() //nolint:errcheck

			config := optimization.BootstrapConfig{
				MaxExamples:     maxExamples,
				MinSuccessRate:  minSuccessRate,
				DiversityWeight: diversityWeight,
				LookbackDays:    lookbackDays,
			}
			optimizer := optimization.NewBootstrapOptimizer(trajStore, patternStore, config)

			examples, err := optimizer.GenerateExamples(ctx, absWorkspace, agentRole)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("generate examples: %v", err))
			}

			formatted := optimizer.FormatExamplesForPrompt(examples)

			stats, err := optimizer.GetExampleStats(ctx, absWorkspace, agentRole)
			if err != nil {
				// Non-fatal, just log
				stats = &optimization.ExampleStats{}
			}

			data := map[string]any{
				"operation":     "bootstrap",
				"agent_role":    agentRole,
				"workspace_id":  absWorkspace,
				"examples":      examples,
				"example_count": len(examples),
				"formatted":     formatted,
				"stats":         stats,
				"config": map[string]any{
					"max_examples":     maxExamples,
					"min_success_rate": minSuccessRate,
					"diversity_weight": diversityWeight,
					"lookback_days":    lookbackDays,
				},
				"duration_ms": time.Since(start).Milliseconds(),
				"cli_command": cmd.CommandPath(),
			}

			return protocol.WriteOK(out, optimizeCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root (used as workspace_id)")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role to bootstrap (required)")
	cmd.Flags().IntVar(&maxExamples, "max-examples", 5, "Maximum examples to generate")
	cmd.Flags().Float64Var(&minSuccessRate, "min-success-rate", 0.8, "Minimum success rate for example trajectories")
	cmd.Flags().Float64Var(&diversityWeight, "diversity-weight", 0.5, "Weight for diversity vs recency (0-1)")
	cmd.Flags().IntVar(&lookbackDays, "lookback-days", 30, "Days to look back for trajectories")
	if err := cmd.MarkFlagRequired("role"); err != nil {
		panic(err)
	}
	return cmd
}

// --- Prompt subcommand ---

func newOptimizePromptCommand() *cobra.Command {
	var (
		workspace          string
		agentRole          string
		promptText         string
		promptFile         string
		targetProfile      string
		transcriptFile     string
		transcriptArtifact string
		preferenceFile     string
		preferenceArtifact string
		save               bool
		mode               string
		backend            string
		breadthCandidates  int
		depthIterations    int
		minImprovement     float64
		lookbackDays       int
	)

	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Optimize an agent prompt from trajectories and feedback",
		Long: `Optimize a prompt using stored trajectories, ratings, and reflection signals.

The GEPA mode is a local GEPA-style reflective optimizer built on agentctl's
trajectory store. It is intentionally offline and deterministic: no live model
calls are made during optimization unless you layer in a custom evaluator later.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizePromptCommand, workspace)
			if err != nil {
				return err
			}

			resolvedPrompt, err := resolveOptimizePromptInput(promptText, promptFile)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, err.Error())
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck

			patternStore, err := optimization.OpenPatternStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open pattern store: %v", err))
			}
			defer patternStore.Close() //nolint:errcheck

			optimizerCfg := buildPromptOptimizerConfig(cfg, mode, backend, targetProfile, breadthCandidates, depthIterations, minImprovement, lookbackDays)
			optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, optimizerCfg)

			transcriptExamples, err := loadTranscriptDatasetExamples(ctx, cfg.Paths.CAS, transcriptFile, transcriptArtifact)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("load transcript dataset: %v", err))
			}
			preferenceExamples, err := loadPreferenceDatasetExamples(ctx, cfg.Paths.CAS, preferenceFile, preferenceArtifact)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("load preference dataset: %v", err))
			}
			if len(transcriptExamples) > 0 {
				optimizer.SetTranscriptExamples(transcriptExamples)
			}
			if len(preferenceExamples) > 0 {
				optimizer.SetPreferenceExamples(preferenceExamples)
			}

			result, err := optimizer.OptimizeInstruction(ctx, absWorkspace, agentRole, resolvedPrompt)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("optimize prompt: %v", err))
			}

			data := map[string]any{
				"operation":        "prompt",
				"workspace_id":     absWorkspace,
				"agent_role":       agentRole,
				"mode":             mode,
				"backend":          promptOptimizerBackendLabel(mode, backend),
				"original_prompt":  result.OriginalPrompt,
				"original_score":   result.OriginalScore,
				"optimized_prompt": result.OptimizedPrompt,
				"optimized_score":  result.OptimizedScore,
				"improvement":      result.Improvement,
				"candidates":       result.Candidates,
				"duration_ms":      result.Duration.Milliseconds(),
				"config": map[string]any{
					"breadth_candidates": breadthCandidates,
					"depth_iterations":   depthIterations,
					"min_improvement":    minImprovement,
					"lookback_days":      lookbackDays,
				},
				"transcript_example_count": len(transcriptExamples),
				"preference_example_count": len(preferenceExamples),
				"cli_command":              cmd.CommandPath(),
			}

			if strings.EqualFold(strings.TrimSpace(mode), "gepa") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(mode)), "gepa-") {
				reflectionEngine := optimization.NewReflectionEngine(trajStore, patternStore, optimization.ReflectionConfig{
					MinEventsForReflection:        3,
					AnalyzeToolUsage:              true,
					LookbackDays:                  lookbackDays,
					MinTrajectoriesForImprovement: 2,
				})
				if summary, err := reflectionEngine.GenerateSummary(ctx, absWorkspace, agentRole); err == nil && summary != nil {
					data["reflection_summary"] = summary
				}
				if improvements, err := reflectionEngine.GenerateImprovements(ctx, absWorkspace, agentRole); err == nil && len(improvements) > 0 {
					data["top_improvements"] = improvements
				}
			}

			if save {
				variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
				if err != nil {
					return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open prompt variant store: %v", err))
				}
				defer variantStore.Close() //nolint:errcheck

				metadata := map[string]any{
					"config":      data["config"],
					"cli_command": cmd.CommandPath(),
				}
				if summary, ok := data["reflection_summary"]; ok {
					metadata["reflection_summary"] = summary
				}
				if improvements, ok := data["top_improvements"]; ok {
					metadata["top_improvements"] = improvements
				}

				savedVariant, err := variantStore.Save(ctx, optimization.PromptVariant{
					WorkspaceID:    absWorkspace,
					AgentRole:      agentRole,
					TargetProfile:  optimizerCfg.TargetProfile,
					Mode:           mode,
					OriginalPrompt: result.OriginalPrompt,
					Prompt:         result.OptimizedPrompt,
					OriginalScore:  result.OriginalScore,
					OptimizedScore: result.OptimizedScore,
					Improvement:    result.Improvement,
					CandidateCount: len(result.Candidates),
					Metadata:       metadata,
				})
				if err != nil {
					return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("save prompt variant: %v", err))
				}
				data["saved_variant"] = savedVariant
			}

			return protocol.WriteOK(out, optimizePromptCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}
	cmd.AddCommand(newOptimizePromptProposeCommand())
	cmd.AddCommand(newOptimizePromptCycleCommand())

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role to optimize (required)")
	cmd.Flags().StringVar(&promptText, "prompt", "", "Inline prompt text to optimize")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Path to a prompt file to optimize")
	cmd.Flags().StringVar(&targetProfile, "target-profile", "", "Optional target prompt profile (for example: local_lmstudio, jido_openrouter)")
	cmd.Flags().StringVar(&transcriptFile, "transcript-dataset-file", "", "Transcript dataset JSONL file to incorporate into GEPA mode")
	cmd.Flags().StringVar(&transcriptArtifact, "transcript-dataset-artifact", "", "CAS digest for a transcript dataset JSONL artifact")
	cmd.Flags().StringVar(&preferenceFile, "preference-dataset-file", "", "Ranked preference dataset JSONL file to incorporate into GEPA mode")
	cmd.Flags().StringVar(&preferenceArtifact, "preference-dataset-artifact", "", "CAS digest for a ranked preference dataset JSONL artifact")
	cmd.Flags().BoolVar(&save, "save", true, "Persist the optimized prompt variant")
	cmd.Flags().StringVar(&mode, "mode", "gepa", "Optimization mode: gepa, copro, mipro-light, mipro-medium, or mipro-heavy")
	cmd.Flags().StringVar(&backend, "backend", "auto", "Prompt optimization backend: auto, agentctl, or dspy-go (GEPA defaults to dspy-go)")
	cmd.Flags().IntVar(&breadthCandidates, "breadth-candidates", 5, "Number of breadth-phase candidates to evaluate")
	cmd.Flags().IntVar(&depthIterations, "depth-iterations", 3, "Number of depth-phase refinement iterations")
	cmd.Flags().Float64Var(&minImprovement, "min-improvement", 0.05, "Minimum score improvement required to accept a change")
	cmd.Flags().IntVar(&lookbackDays, "lookback-days", 30, "Days of trajectories and feedback to use")
	if err := cmd.MarkFlagRequired("role"); err != nil {
		panic(err)
	}
	return cmd
}

func newOptimizePromptProposeCommand() *cobra.Command {
	var (
		workspace          string
		agentRole          string
		promptText         string
		promptFile         string
		targetProfile      string
		transcriptFile     string
		transcriptArtifact string
		preferenceFile     string
		preferenceArtifact string
		mode               string
		backend            string
		count              int
		lookbackDays       int
		save               bool
	)

	cmd := &cobra.Command{
		Use:   "propose",
		Short: "Generate multiple prompt candidates for a role",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, err.Error())
			}
			absWorkspace, err := absWorkspaceOrWriteError(out, optimizePromptCommand, workspace)
			if err != nil {
				return err
			}

			optimizerCfg := buildPromptOptimizerConfig(cfg, mode, backend, targetProfile, count, 2, 0.01, lookbackDays)
			basePrompt, baseSource, err := resolvePromptBase(ctx, cfg, absWorkspace, agentRole, optimizerCfg.TargetProfile, promptText, promptFile)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, err.Error())
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck
			patternStore, err := optimization.OpenPatternStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open pattern store: %v", err))
			}
			defer patternStore.Close() //nolint:errcheck

			optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, optimizerCfg)

			transcriptExamples, err := loadTranscriptDatasetExamples(ctx, cfg.Paths.CAS, transcriptFile, transcriptArtifact)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("load transcript dataset: %v", err))
			}
			preferenceExamples, err := loadPreferenceDatasetExamples(ctx, cfg.Paths.CAS, preferenceFile, preferenceArtifact)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("load preference dataset: %v", err))
			}
			if len(transcriptExamples) > 0 {
				optimizer.SetTranscriptExamples(transcriptExamples)
			}
			if len(preferenceExamples) > 0 {
				optimizer.SetPreferenceExamples(preferenceExamples)
			}

			candidates, err := optimizer.ProposeCandidates(ctx, absWorkspace, agentRole, basePrompt, count)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("propose prompt candidates: %v", err))
			}

			data := map[string]any{
				"operation":                "prompt.propose",
				"workspace_id":             absWorkspace,
				"agent_role":               agentRole,
				"mode":                     mode,
				"backend":                  promptOptimizerBackendLabel(mode, backend),
				"base_prompt":              basePrompt,
				"base_prompt_source":       baseSource,
				"candidate_count":          len(candidates),
				"transcript_example_count": len(transcriptExamples),
				"preference_example_count": len(preferenceExamples),
				"candidates":               candidates,
				"cli_command":              cmd.CommandPath(),
			}

			if save {
				variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
				if err != nil {
					return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open prompt variant store: %v", err))
				}
				defer variantStore.Close() //nolint:errcheck

				saved := make([]optimization.PromptVariant, 0, len(candidates))
				for _, candidate := range candidates {
					variant, err := variantStore.Save(ctx, optimization.PromptVariant{
						WorkspaceID:    absWorkspace,
						AgentRole:      agentRole,
						TargetProfile:  optimizerCfg.TargetProfile,
						Mode:           mode,
						OriginalPrompt: basePrompt,
						Prompt:         candidate.Prompt,
						OriginalScore:  0,
						OptimizedScore: candidate.Score,
						Improvement:    candidate.Score,
						CandidateCount: len(candidate.Improvements),
						Metadata: map[string]any{
							"base_prompt_source":       baseSource,
							"transcript_example_count": len(transcriptExamples),
							"preference_example_count": len(preferenceExamples),
							"cli_command":              cmd.CommandPath(),
							"improvements":             candidate.Improvements,
							"generation":               candidate.Generation,
						},
					})
					if err != nil {
						return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("save prompt variant: %v", err))
					}
					saved = append(saved, variant)
				}
				data["saved_variants"] = saved
			}

			return protocol.WriteOK(out, optimizePromptCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role to propose prompts for (required)")
	cmd.Flags().StringVar(&promptText, "prompt", "", "Optional inline base prompt")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Optional base prompt file")
	cmd.Flags().StringVar(&targetProfile, "target-profile", "", "Optional target prompt profile (for example: local_lmstudio, jido_openrouter)")
	cmd.Flags().StringVar(&transcriptFile, "transcript-dataset-file", "", "Transcript dataset JSONL file to incorporate into GEPA mode")
	cmd.Flags().StringVar(&transcriptArtifact, "transcript-dataset-artifact", "", "CAS digest for a transcript dataset JSONL artifact")
	cmd.Flags().StringVar(&preferenceFile, "preference-dataset-file", "", "Ranked preference dataset JSONL file to incorporate into GEPA mode")
	cmd.Flags().StringVar(&preferenceArtifact, "preference-dataset-artifact", "", "CAS digest for a ranked preference dataset JSONL artifact")
	cmd.Flags().StringVar(&mode, "mode", "gepa", "Optimization mode")
	cmd.Flags().StringVar(&backend, "backend", "auto", "Prompt optimization backend: auto, agentctl, or dspy-go (GEPA defaults to dspy-go)")
	cmd.Flags().IntVar(&count, "count", 5, "Number of candidates to propose")
	cmd.Flags().IntVar(&lookbackDays, "lookback-days", 30, "Days of trajectories and feedback to use")
	cmd.Flags().BoolVar(&save, "save", true, "Persist proposed prompt variants")
	if err := cmd.MarkFlagRequired("role"); err != nil {
		panic(err)
	}
	return cmd
}

func newOptimizePromptCycleCommand() *cobra.Command {
	var (
		workspace          string
		agentRole          string
		promptText         string
		promptFile         string
		targetProfile      string
		transcriptFile     string
		transcriptArtifact string
		preferenceFile     string
		preferenceArtifact string
		mode               string
		backend            string
		count              int
		lookbackDays       int
		question           string
		contextText        string
		evalDatasetFile    string
		provider           string
		baseURL            string
		apiKey             string
		models             []string
		expectedSubstrings []string
		rejectSubstrings   []string
		timeout            time.Duration
		maxTokens          int
		temperature        float64
		passThreshold      float64
		minOutputChars     int
		maxOutputChars     int
		promote            bool
		promoteAgentRef    string
	)

	cmd := &cobra.Command{
		Use:   "cycle",
		Short: "Propose, compare, persist, and optionally promote prompt candidates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, err.Error())
			}
			absWorkspace, err := absWorkspaceOrWriteError(out, optimizePromptCommand, workspace)
			if err != nil {
				return err
			}

			derivedTargetProfile := targetProfile
			if strings.TrimSpace(derivedTargetProfile) == "" {
				primaryTarget, _, targetErr := resolvePromptComparisonTargets(cfg, provider, baseURL, apiKey, models)
				if targetErr == nil && len(primaryTarget.Models) > 0 {
					derivedTargetProfile = optimization.DerivePromptTargetProfile("", primaryTarget.Provider, primaryTarget.Models[0])
				}
			}
			optimizerCfg := buildPromptOptimizerConfig(cfg, mode, backend, derivedTargetProfile, count, 2, 0.01, lookbackDays)
			basePrompt, baseSource, err := resolvePromptBase(ctx, cfg, absWorkspace, agentRole, optimizerCfg.TargetProfile, promptText, promptFile)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, err.Error())
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck
			patternStore, err := optimization.OpenPatternStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open pattern store: %v", err))
			}
			defer patternStore.Close() //nolint:errcheck
			variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open prompt variant store: %v", err))
			}
			defer variantStore.Close() //nolint:errcheck

			optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, optimizerCfg)
			transcriptExamples, err := loadTranscriptDatasetExamples(ctx, cfg.Paths.CAS, transcriptFile, transcriptArtifact)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("load transcript dataset: %v", err))
			}
			preferenceExamples, err := loadPreferenceDatasetExamples(ctx, cfg.Paths.CAS, preferenceFile, preferenceArtifact)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("load preference dataset: %v", err))
			}
			if len(transcriptExamples) > 0 {
				optimizer.SetTranscriptExamples(transcriptExamples)
			}
			if len(preferenceExamples) > 0 {
				optimizer.SetPreferenceExamples(preferenceExamples)
			}

			candidates, err := optimizer.ProposeCandidates(ctx, absWorkspace, agentRole, basePrompt, count)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("propose prompt candidates: %v", err))
			}
			savedVariants := make([]optimization.PromptVariant, 0, len(candidates))
			for _, candidate := range candidates {
				variant, err := variantStore.Save(ctx, optimization.PromptVariant{
					WorkspaceID:    absWorkspace,
					AgentRole:      agentRole,
					TargetProfile:  optimizerCfg.TargetProfile,
					Mode:           mode,
					OriginalPrompt: basePrompt,
					Prompt:         candidate.Prompt,
					OriginalScore:  0,
					OptimizedScore: candidate.Score,
					Improvement:    candidate.Score,
					CandidateCount: len(candidate.Improvements),
					Metadata: map[string]any{
						"base_prompt_source":       baseSource,
						"transcript_example_count": len(transcriptExamples),
						"preference_example_count": len(preferenceExamples),
						"cli_command":              cmd.CommandPath(),
						"improvements":             candidate.Improvements,
						"generation":               candidate.Generation,
					},
				})
				if err != nil {
					return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("save prompt variant: %v", err))
				}
				savedVariants = append(savedVariants, variant)
			}

			primaryTarget, fallbackTarget, err := resolvePromptComparisonTargets(cfg, provider, baseURL, apiKey, models)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, err.Error())
			}
			evalCases, err := loadPromptEvalCases(evalDatasetFile)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, err.Error())
			}
			effectiveQuestion := effectivePromptComparisonQuestion(question, evalCases)

			comparisonResults, err := runPromptVariantComparisons(ctx, promptComparisonRequest{
				Variants:    savedVariants,
				Question:    effectiveQuestion,
				Context:     contextText,
				EvalCases:   evalCases,
				Provider:    primaryTarget.Provider,
				BaseURL:     primaryTarget.BaseURL,
				APIKey:      primaryTarget.APIKey,
				Models:      primaryTarget.Models,
				Fallback:    fallbackTarget,
				Timeout:     timeout,
				MaxTokens:   maxTokens,
				Temperature: temperature,
				Scoring: promptComparisonScoring{
					ExpectedSubstrings: append([]string(nil), expectedSubstrings...),
					RejectSubstrings:   append([]string(nil), rejectSubstrings...),
					MinOutputChars:     minOutputChars,
					MaxOutputChars:     maxOutputChars,
					PassThreshold:      passThreshold,
				},
			})
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("compare prompt variants: %v", err))
			}
			ranking := aggregatePromptVariantComparisons(comparisonResults)
			activeProvider, activeBaseURL, activeModels, failedOver := summarizePromptComparisonExecution(primaryTarget, fallbackTarget, comparisonResults)

			successCount := 0
			for _, result := range comparisonResults {
				if strings.TrimSpace(result.Error) == "" {
					successCount++
				}
			}

			report := map[string]any{
				"operation":          "prompt.cycle",
				"workspace_id":       absWorkspace,
				"agent_role":         agentRole,
				"mode":               mode,
				"backend":            promptOptimizerBackendLabel(mode, backend),
				"base_prompt":        basePrompt,
				"base_prompt_source": baseSource,
				"question":           effectiveQuestion,
				"context":            strings.TrimSpace(contextText),
				"eval_case_count":    len(evalCases),
				"eval_cases":         evalCases,
				"provider":           activeProvider,
				"base_url":           activeBaseURL,
				"primary_target": map[string]any{
					"provider": primaryTarget.Provider,
					"base_url": primaryTarget.BaseURL,
					"models":   primaryTarget.Models,
				},
				"failed_over":              failedOver,
				"model_count":              len(activeModels),
				"models":                   activeModels,
				"transcript_example_count": len(transcriptExamples),
				"preference_example_count": len(preferenceExamples),
				"proposed_variants":        savedVariants,
				"results":                  comparisonResults,
				"ranking":                  ranking,
				"success_count":            successCount,
				"failure_count":            len(comparisonResults) - successCount,
				"scoring": map[string]any{
					"expected_substrings": expectedSubstrings,
					"reject_substrings":   rejectSubstrings,
					"min_output_chars":    minOutputChars,
					"max_output_chars":    maxOutputChars,
					"pass_threshold":      passThreshold,
				},
				"cli_command": cmd.CommandPath(),
			}
			if fallbackTarget != nil {
				report["fallback_target"] = map[string]any{
					"provider": fallbackTarget.Provider,
					"base_url": fallbackTarget.BaseURL,
					"models":   fallbackTarget.Models,
				}
			}

			artifact, err := persistPromptComparisonArtifact(ctx, cfg.Paths.CAS, report)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("persist cycle artifact: %v", err))
			}
			runStore, err := optimization.OpenPromptComparisonRunStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open prompt comparison run store: %v", err))
			}
			defer runStore.Close() //nolint:errcheck
			savedRun, err := runStore.Save(ctx, optimization.PromptComparisonRun{
				WorkspaceID:    absWorkspace,
				ArtifactDigest: artifact,
				Provider:       activeProvider,
				BaseURL:        activeBaseURL,
				Question:       effectiveQuestion,
				Context:        strings.TrimSpace(contextText),
				ModelCount:     len(activeModels),
				VariantCount:   len(savedVariants),
				SuccessCount:   successCount,
				FailureCount:   len(comparisonResults) - successCount,
			})
			if err != nil {
				return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("save cycle run: %v", err))
			}

			data := map[string]any{
				"operation":          "prompt.cycle",
				"workspace_id":       absWorkspace,
				"agent_role":         agentRole,
				"backend":            promptOptimizerBackendLabel(mode, backend),
				"base_prompt_source": baseSource,
				"saved_variants":     savedVariants,
				"ranking":            ranking,
				"results":            comparisonResults,
				"saved_run":          savedRun,
				"artifact":           artifact,
				"cli_command":        cmd.CommandPath(),
			}

			if promote {
				if len(ranking) == 0 {
					return writeOptimizeError(out, optimizePromptCommand, "cannot promote without ranked comparison results")
				}
				topVariant, err := variantStore.Get(ctx, absWorkspace, ranking[0].VariantID)
				if err != nil {
					return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("load top variant: %v", err))
				}
				agentStore, err := storagents.Open(ctx, cfg.Storage.Root)
				if err != nil {
					return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("open agent store: %v", err))
				}
				defer agentStore.Close() //nolint:errcheck
				targetAgent, err := resolveAgentForPromptPromotion(ctx, agentStore, absWorkspace, agentRole, promoteAgentRef)
				if err != nil {
					return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("resolve promotion target: %v", err))
				}
				if err := agentStore.UpdatePrompt(ctx, targetAgent.ID, topVariant.Prompt); err != nil {
					return writeOptimizeError(out, optimizePromptCommand, fmt.Sprintf("promote top variant: %v", err))
				}
				data["promoted"] = map[string]any{
					"agent_id":    targetAgent.ID,
					"agent_role":  targetAgent.Role,
					"variant_id":  topVariant.ID,
					"mean_score":  ranking[0].MeanScore,
					"worst_score": ranking[0].WorstScore,
				}
			}

			return protocol.WriteOK(out, optimizePromptCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
				protocol.WithCASDigest(artifact),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role to optimize (required)")
	cmd.Flags().StringVar(&promptText, "prompt", "", "Optional inline base prompt")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Optional base prompt file")
	cmd.Flags().StringVar(&targetProfile, "target-profile", "", "Optional target prompt profile (for example: local_lmstudio, jido_openrouter)")
	cmd.Flags().StringVar(&transcriptFile, "transcript-dataset-file", "", "Transcript dataset JSONL file to incorporate into GEPA mode")
	cmd.Flags().StringVar(&transcriptArtifact, "transcript-dataset-artifact", "", "CAS digest for a transcript dataset JSONL artifact")
	cmd.Flags().StringVar(&preferenceFile, "preference-dataset-file", "", "Ranked preference dataset JSONL file to incorporate into GEPA mode")
	cmd.Flags().StringVar(&preferenceArtifact, "preference-dataset-artifact", "", "CAS digest for a ranked preference dataset JSONL artifact")
	cmd.Flags().StringVar(&mode, "mode", "gepa", "Optimization mode")
	cmd.Flags().StringVar(&backend, "backend", "auto", "Prompt optimization backend: auto, agentctl, or dspy-go (GEPA defaults to dspy-go)")
	cmd.Flags().IntVar(&count, "count", 5, "Number of candidates to propose")
	cmd.Flags().IntVar(&lookbackDays, "lookback-days", 30, "Days of trajectories and feedback to use")
	cmd.Flags().StringVar(&question, "question", "", "Question or task input to evaluate candidates with (required)")
	cmd.Flags().StringVar(&contextText, "context", "", "Optional extra context prepended to the user prompt")
	cmd.Flags().StringVar(&evalDatasetFile, "eval-dataset-file", "", "JSONL eval dataset file with question/context/target_response rows")
	cmd.Flags().StringVar(&provider, "provider", "", "Provider name for the comparison client (default: remote-best with local fallback)")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL override")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key override (defaults to lm-studio for LMStudio)")
	cmd.Flags().StringSliceVar(&models, "model", nil, "Model ID to compare against (repeatable; default: openai/gpt-5.4-nano with local fallback)")
	cmd.Flags().StringSliceVar(&expectedSubstrings, "expect-substring", nil, "Substring that should appear in a passing output (repeatable)")
	cmd.Flags().StringSliceVar(&rejectSubstrings, "reject-substring", nil, "Substring that should not appear in a passing output (repeatable)")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "Per-request timeout")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 512, "Maximum tokens per response")
	cmd.Flags().Float64Var(&temperature, "temperature", 0.1, "Sampling temperature")
	cmd.Flags().Float64Var(&passThreshold, "pass-threshold", 0.8, "Score threshold for counting a comparison as a pass")
	cmd.Flags().IntVar(&minOutputChars, "min-output-chars", 0, "Minimum output length required for a full score")
	cmd.Flags().IntVar(&maxOutputChars, "max-output-chars", 0, "Maximum output length allowed for a full score")
	cmd.Flags().BoolVar(&promote, "promote", false, "Promote the top-ranked variant into a live agent prompt")
	cmd.Flags().StringVar(&promoteAgentRef, "promote-agent", "", "Agent ID, slug, or name to promote into (defaults to latest matching role in workspace)")
	if err := cmd.MarkFlagRequired("role"); err != nil {
		panic(err)
	}
	return cmd
}

func resolveOptimizePromptInput(promptText, promptFile string) (string, error) {
	inline := strings.TrimSpace(promptText)
	filePath := strings.TrimSpace(promptFile)

	switch {
	case inline != "" && filePath != "":
		return "", fmt.Errorf("use either --prompt or --prompt-file, not both")
	case inline != "":
		return inline, nil
	case filePath == "":
		return "", fmt.Errorf("either --prompt or --prompt-file is required")
	}

	body, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read prompt file: %w", err)
	}
	resolved := strings.TrimSpace(string(body))
	if resolved == "" {
		return "", fmt.Errorf("prompt file is empty")
	}
	return resolved, nil
}

func loadTranscriptDatasetExamples(ctx context.Context, casRoot, filePath, artifact string) ([]optimization.TranscriptTrainingExample, error) {
	switch {
	case strings.TrimSpace(filePath) != "" && strings.TrimSpace(artifact) != "":
		return nil, fmt.Errorf("use either transcript dataset file or artifact, not both")
	case strings.TrimSpace(filePath) != "":
		f, err := os.Open(strings.TrimSpace(filePath))
		if err != nil {
			return nil, fmt.Errorf("open transcript dataset file: %w", err)
		}
		defer f.Close()
		return optimization.ParseTranscriptDatasetJSONL(f)
	case strings.TrimSpace(artifact) != "":
		store, err := cas.NewStore(casRoot)
		if err != nil {
			return nil, err
		}
		defer store.Close()
		rc, _, err := store.Get(ctx, strings.TrimSpace(artifact))
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return optimization.ParseTranscriptDatasetJSONL(rc)
	default:
		return nil, nil
	}
}

func loadPreferenceDatasetExamples(ctx context.Context, casRoot, filePath, artifact string) ([]optimization.PromptPreferenceExample, error) {
	switch {
	case strings.TrimSpace(filePath) != "" && strings.TrimSpace(artifact) != "":
		return nil, fmt.Errorf("use either preference dataset file or artifact, not both")
	case strings.TrimSpace(filePath) != "":
		f, err := os.Open(strings.TrimSpace(filePath))
		if err != nil {
			return nil, fmt.Errorf("open preference dataset file: %w", err)
		}
		defer f.Close()
		return optimization.ParsePromptPreferenceDatasetJSONL(f)
	case strings.TrimSpace(artifact) != "":
		store, err := cas.NewStore(casRoot)
		if err != nil {
			return nil, err
		}
		defer store.Close()
		rc, _, err := store.Get(ctx, strings.TrimSpace(artifact))
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return optimization.ParsePromptPreferenceDatasetJSONL(rc)
	default:
		return nil, nil
	}
}

func loadPromptEvalCases(filePath string) ([]promptEvalCase, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return nil, nil
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open eval dataset file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10<<20)
	cases := make([]promptEvalCase, 0, 32)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item promptEvalCase
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("decode eval dataset case: %w", err)
		}
		if strings.TrimSpace(item.Question) == "" {
			continue
		}
		cases = append(cases, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan eval dataset file: %w", err)
	}
	return cases, nil
}

func effectivePromptComparisonQuestion(question string, evalCases []promptEvalCase) string {
	question = strings.TrimSpace(question)
	if question != "" {
		return question
	}
	if len(evalCases) > 0 {
		return fmt.Sprintf("eval-dataset:%d-cases", len(evalCases))
	}
	return ""
}

func resolvePromptComparisonVariants(
	ctx context.Context,
	store optimization.PromptVariantStore,
	workspaceID string,
	ids []string,
	agentRole string,
	targetProfile string,
	limit int,
) ([]optimization.PromptVariant, error) {
	if len(ids) > 0 {
		var variants []optimization.PromptVariant
		for _, id := range ids {
			variant, err := store.Get(ctx, workspaceID, id)
			if err != nil {
				return nil, fmt.Errorf("get prompt variant %q: %w", id, err)
			}
			variants = append(variants, variant)
		}
		return variants, nil
	}
	if strings.TrimSpace(agentRole) == "" {
		return nil, fmt.Errorf("either --id or --role is required")
	}
	var variants []optimization.PromptVariant
	var err error
	if strings.TrimSpace(targetProfile) != "" {
		variants, err = store.ListByTargetProfile(ctx, workspaceID, agentRole, targetProfile, limit)
	} else {
		variants, err = store.List(ctx, workspaceID, agentRole, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list prompt variants for role %q: %w", agentRole, err)
	}
	if len(variants) == 0 {
		return nil, fmt.Errorf("no prompt variants found for role %q", agentRole)
	}
	return variants, nil
}

func samplePromptEvalCases(evalCases []promptEvalCase, sampleSize int, seed int64, round int) []promptEvalCase {
	if len(evalCases) == 0 {
		return nil
	}
	if sampleSize <= 0 || sampleSize >= len(evalCases) {
		out := append([]promptEvalCase(nil), evalCases...)
		return out
	}
	rng := rand.New(rand.NewSource(seed + int64(round)))
	indexes := rng.Perm(len(evalCases))[:sampleSize]
	sort.Ints(indexes)
	out := make([]promptEvalCase, 0, sampleSize)
	for _, idx := range indexes {
		out = append(out, evalCases[idx])
	}
	return out
}

func promptEvalCaseIDs(evalCases []promptEvalCase) []string {
	out := make([]string, 0, len(evalCases))
	for i, evalCase := range evalCases {
		id := strings.TrimSpace(evalCase.ID)
		if id == "" {
			id = fmt.Sprintf("case_%d", i+1)
		}
		out = append(out, id)
	}
	return out
}

func resolvePromptBase(
	ctx context.Context,
	cfg config.Config,
	workspacePath, agentRole, targetProfile, promptText, promptFile string,
) (string, string, error) {
	if strings.TrimSpace(promptText) != "" || strings.TrimSpace(promptFile) != "" {
		prompt, err := resolveOptimizePromptInput(promptText, promptFile)
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(promptText) != "" {
			return prompt, "inline", nil
		}
		return prompt, "file", nil
	}

	variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
	if err == nil {
		defer variantStore.Close() //nolint:errcheck
		if variant, resolveErr := variantStore.ResolveLatestCompatible(ctx, workspacePath, agentRole, targetProfile); resolveErr == nil && strings.TrimSpace(variant.Prompt) != "" {
			if strings.TrimSpace(variant.TargetProfile) != "" {
				return variant.Prompt, "latest_variant_target_profile", nil
			}
			return variant.Prompt, "latest_variant", nil
		}
	}

	agentStore, err := storagents.Open(ctx, cfg.Storage.Root)
	if err == nil {
		defer agentStore.Close() //nolint:errcheck
		if agentsList, listErr := agentStore.List(ctx, 200); listErr == nil {
			for _, candidate := range agentsList {
				if !strings.EqualFold(strings.TrimSpace(candidate.Role), strings.TrimSpace(agentRole)) {
					continue
				}
				if strings.TrimSpace(candidate.WorkspaceRoot) != "" && strings.TrimSpace(candidate.WorkspaceRoot) != strings.TrimSpace(workspacePath) {
					continue
				}
				if strings.TrimSpace(candidate.Prompt) == "" {
					continue
				}
				return strings.TrimSpace(candidate.Prompt), "latest_agent", nil
			}
		}
	}

	return "", "", fmt.Errorf("no base prompt found: provide --prompt/--prompt-file, or create a saved prompt variant for role %q", agentRole)
}

func resolveAgentForPromptPromotion(
	ctx context.Context,
	store storagents.Store,
	workspacePath, agentRole, explicitRef string,
) (agent.Agent, error) {
	if strings.TrimSpace(explicitRef) != "" {
		return store.Resolve(ctx, strings.TrimSpace(explicitRef))
	}
	agentsList, err := store.List(ctx, 200)
	if err != nil {
		return agent.Agent{}, err
	}
	for _, candidate := range agentsList {
		if !strings.EqualFold(strings.TrimSpace(candidate.Role), strings.TrimSpace(agentRole)) {
			continue
		}
		if strings.TrimSpace(candidate.WorkspaceRoot) != "" && strings.TrimSpace(candidate.WorkspaceRoot) != strings.TrimSpace(workspacePath) {
			continue
		}
		return candidate, nil
	}
	return agent.Agent{}, fmt.Errorf("no matching agent found for role %q in workspace %q", agentRole, workspacePath)
}

func buildPromptOptimizerConfig(cfg config.Config, mode, backend, targetProfile string, breadthCandidates, depthIterations int, minImprovement float64, lookbackDays int) optimization.PromptOptimizerConfig {
	result := optimization.PromptOptimizerConfig{
		Mode:              mode,
		Backend:           backend,
		BreadthCandidates: breadthCandidates,
		DepthIterations:   depthIterations,
		MinImprovement:    minImprovement,
		LookbackDays:      lookbackDays,
	}
	if strings.EqualFold(strings.TrimSpace(mode), "gepa") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(mode)), "gepa-") {
		primary, fallback, err := resolvePromptComparisonTargets(cfg, "", "", "", nil)
		if err == nil {
			if len(primary.Models) > 0 {
				result.PrimaryLLM = &optimization.PromptOptimizerLLMTarget{
					Provider: primary.Provider,
					BaseURL:  primary.BaseURL,
					APIKey:   primary.APIKey,
					Model:    primary.Models[0],
				}
				result.TargetProfile = optimization.NormalizePromptTargetProfile(firstNonEmpty(targetProfile, optimization.DerivePromptTargetProfile("", primary.Provider, primary.Models[0])))
			}
			if fallback != nil && len(fallback.Models) > 0 {
				result.FallbackLLM = &optimization.PromptOptimizerLLMTarget{
					Provider: fallback.Provider,
					BaseURL:  fallback.BaseURL,
					APIKey:   fallback.APIKey,
					Model:    fallback.Models[0],
				}
			}
		}
	}
	if result.TargetProfile == "" {
		result.TargetProfile = optimization.NormalizePromptTargetProfile(targetProfile)
	}
	return result
}

func promptOptimizerBackendLabel(mode, backend string) string {
	backend = strings.ToLower(strings.TrimSpace(backend))
	switch backend {
	case "", "auto":
		if strings.EqualFold(strings.TrimSpace(mode), "gepa") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(mode)), "gepa-") {
			return "dspy-go"
		}
		return "agentctl"
	case "dspy-go", "agentctl":
		return backend
	default:
		return "agentctl"
	}
}

func resolvePromptComparisonTargets(cfg config.Config, provider, baseURL, apiKey string, models []string) (promptComparisonTargetConfig, *promptComparisonTargetConfig, error) {
	explicit := strings.TrimSpace(provider) != "" || strings.TrimSpace(baseURL) != "" || strings.TrimSpace(apiKey) != "" || len(models) > 0
	if !explicit {
		primary, err := resolvePromptComparisonTarget(cfg, "openrouter", "", "", []string{"openai/gpt-5.4-nano"})
		if err != nil {
			return promptComparisonTargetConfig{}, nil, err
		}
		fallback, err := resolvePromptComparisonTarget(cfg, "lmstudio", "", "", []string{"liquid/lfm2.5-1.2b"})
		if err != nil {
			return promptComparisonTargetConfig{}, nil, err
		}
		return primary, &fallback, nil
	}
	primary, err := resolvePromptComparisonTarget(cfg, provider, baseURL, apiKey, models)
	if err != nil {
		return promptComparisonTargetConfig{}, nil, err
	}
	return primary, nil, nil
}

func resolvePromptComparisonTarget(cfg config.Config, provider, baseURL, apiKey string, models []string) (promptComparisonTargetConfig, error) {
	resolvedProvider := strings.TrimSpace(provider)
	if resolvedProvider == "" {
		resolvedProvider = "lmstudio"
	}
	resolvedBaseURL := strings.TrimSpace(baseURL)
	if resolvedBaseURL == "" {
		resolvedBaseURL = cfg.LLM.ResolveBaseURL(resolvedProvider)
	}
	resolvedAPIKey := strings.TrimSpace(apiKey)
	if resolvedAPIKey == "" {
		if strings.EqualFold(resolvedProvider, "lmstudio") {
			resolvedAPIKey = "lm-studio"
		} else {
			resolvedAPIKey = cfg.LLM.ResolveAPIKey(resolvedProvider)
		}
	}
	resolvedModels := append([]string(nil), models...)
	if len(resolvedModels) == 0 {
		if model := strings.TrimSpace(cfg.LLM.ResolveModel(resolvedProvider)); model != "" {
			resolvedModels = append(resolvedModels, model)
		}
	}
	if len(resolvedModels) == 0 {
		return promptComparisonTargetConfig{}, fmt.Errorf("at least one model is required")
	}
	return promptComparisonTargetConfig{
		Provider: resolvedProvider,
		BaseURL:  resolvedBaseURL,
		APIKey:   resolvedAPIKey,
		Models:   resolvedModels,
	}, nil
}

func newOptimizePromptsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "Inspect persisted optimized prompt variants",
	}
	cmd.AddCommand(
		newOptimizePromptsCompareCommand(),
		newOptimizePromptsCompareBatchCommand(),
		newOptimizePromptsComparisonsCommand(),
		newOptimizePromptsListCommand(),
		newOptimizePromptsShowCommand(),
	)
	return cmd
}

func newOptimizePromptsListCommand() *cobra.Command {
	var (
		workspace     string
		agentRole     string
		targetProfile string
		limit         int
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved optimized prompt variants",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizePromptsCommand, workspace)
			if err != nil {
				return err
			}

			variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("open prompt variant store: %v", err))
			}
			defer variantStore.Close() //nolint:errcheck

			var variants []optimization.PromptVariant
			if strings.TrimSpace(targetProfile) != "" {
				variants, err = variantStore.ListByTargetProfile(ctx, absWorkspace, agentRole, targetProfile, limit)
			} else {
				variants, err = variantStore.List(ctx, absWorkspace, agentRole, limit)
			}
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("list prompt variants: %v", err))
			}

			data := map[string]any{
				"operation":      "prompts.list",
				"workspace_id":   absWorkspace,
				"agent_role":     agentRole,
				"target_profile": optimization.NormalizePromptTargetProfile(targetProfile),
				"count":          len(variants),
				"variants":       variants,
				"cli_command":    cmd.CommandPath(),
			}
			return protocol.WriteOK(out, optimizePromptsCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&agentRole, "role", "", "Filter by agent role")
	cmd.Flags().StringVar(&targetProfile, "target-profile", "", "Filter by target prompt profile")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum variants to return")
	return cmd
}

func newOptimizePromptsShowCommand() *cobra.Command {
	var workspace string
	var id string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show one saved optimized prompt variant",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizePromptsCommand, workspace)
			if err != nil {
				return err
			}

			variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("open prompt variant store: %v", err))
			}
			defer variantStore.Close() //nolint:errcheck

			variant, err := variantStore.Get(ctx, absWorkspace, id)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("get prompt variant: %v", err))
			}

			data := map[string]any{
				"operation":    "prompts.show",
				"workspace_id": absWorkspace,
				"variant":      variant,
				"cli_command":  cmd.CommandPath(),
			}
			return protocol.WriteOK(out, optimizePromptsCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&id, "id", "", "Variant ID to fetch")
	if err := cmd.MarkFlagRequired("id"); err != nil {
		panic(err)
	}
	return cmd
}

type promptVariantComparison struct {
	VariantID  string  `json:"variant_id"`
	AgentRole  string  `json:"agent_role"`
	Mode       string  `json:"mode"`
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	EvalCaseID string  `json:"eval_case_id,omitempty"`
	Category   string  `json:"category,omitempty"`
	Output     string  `json:"output,omitempty"`
	Error      string  `json:"error,omitempty"`
	DurationMS int64   `json:"duration_ms"`
	ScoreDelta float64 `json:"score_delta"`
	Score      float64 `json:"score"`
	Passed     bool    `json:"passed"`
	FailedOver bool    `json:"failed_over,omitempty"`
}

type promptComparisonScoring struct {
	ExpectedSubstrings []string
	RejectSubstrings   []string
	MinOutputChars     int
	MaxOutputChars     int
	PassThreshold      float64
}

type promptVariantAggregate struct {
	VariantID       string  `json:"variant_id"`
	AgentRole       string  `json:"agent_role"`
	Mode            string  `json:"mode"`
	ComparisonCount int     `json:"comparison_count"`
	PassCount       int     `json:"pass_count"`
	ErrorCount      int     `json:"error_count"`
	MeanScore       float64 `json:"mean_score"`
	WorstScore      float64 `json:"worst_score"`
	BestScore       float64 `json:"best_score"`
	ScoreVariance   float64 `json:"score_variance"`
	AvgDurationMS   float64 `json:"avg_duration_ms"`
	ScoreDelta      float64 `json:"score_delta"`
}

type promptComparisonRequest struct {
	Variants    []optimization.PromptVariant
	Question    string
	Context     string
	EvalCases   []promptEvalCase
	Provider    string
	BaseURL     string
	APIKey      string
	Models      []string
	Fallback    *promptComparisonTargetConfig
	Timeout     time.Duration
	MaxTokens   int
	Temperature float64
	Scoring     promptComparisonScoring
}

type promptEvalCase struct {
	ID               string              `json:"id,omitempty"`
	Question         string              `json:"question"`
	Context          string              `json:"context,omitempty"`
	TargetResponse   string              `json:"target_response,omitempty"`
	ExpectedPaths    []string            `json:"expected_paths,omitempty"`
	ExpectedSymbols  []string            `json:"expected_symbols,omitempty"`
	ExpectedSnippets []promptEvalSnippet `json:"expected_snippets,omitempty"`
	RequiredFacts    []string            `json:"required_facts,omitempty"`
	ExcludedPaths    []string            `json:"excluded_paths,omitempty"`
	TaskType         string              `json:"task_type,omitempty"`
	RequireGrounding bool                `json:"requires_grounding,omitempty"`
	Category         string              `json:"category,omitempty"`
	SessionID        string              `json:"session_id,omitempty"`
}

type promptComparisonTargetConfig struct {
	Provider string
	BaseURL  string
	APIKey   string
	Models   []string
}

type promptComparisonExecution struct {
	Report         map[string]any
	Results        []promptVariantComparison
	Ranking        []promptVariantAggregate
	SavedRun       optimization.PromptComparisonRun
	Artifact       string
	ActiveProvider string
	ActiveBaseURL  string
	ActiveModels   []string
	FailedOver     bool
	SuccessCount   int
}

func newOptimizePromptsCompareCommand() *cobra.Command {
	var (
		workspace          string
		ids                []string
		question           string
		contextText        string
		evalDatasetFile    string
		provider           string
		baseURL            string
		apiKey             string
		models             []string
		expectedSubstrings []string
		rejectSubstrings   []string
		timeout            time.Duration
		maxTokens          int
		temperature        float64
		passThreshold      float64
		minOutputChars     int
		maxOutputChars     int
	)

	cmd := &cobra.Command{
		Use:   "compare",
		Short: "Compare saved prompt variants across multiple models",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizePromptsCommand, workspace)
			if err != nil {
				return err
			}

			variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("open prompt variant store: %v", err))
			}
			defer variantStore.Close() //nolint:errcheck

			variants, err := resolvePromptComparisonVariants(ctx, variantStore, absWorkspace, ids, "", "", 0)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}

			primaryTarget, fallbackTarget, err := resolvePromptComparisonTargets(cfg, provider, baseURL, apiKey, models)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}
			evalCases, err := loadPromptEvalCases(evalDatasetFile)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}
			effectiveQuestion := effectivePromptComparisonQuestion(question, evalCases)

			execution, err := executePromptComparisonRun(ctx, cfg, absWorkspace, promptComparisonRequest{
				Variants:    variants,
				Question:    effectiveQuestion,
				Context:     contextText,
				EvalCases:   evalCases,
				Provider:    primaryTarget.Provider,
				BaseURL:     primaryTarget.BaseURL,
				APIKey:      primaryTarget.APIKey,
				Models:      primaryTarget.Models,
				Fallback:    fallbackTarget,
				Timeout:     timeout,
				MaxTokens:   maxTokens,
				Temperature: temperature,
				Scoring: promptComparisonScoring{
					ExpectedSubstrings: append([]string(nil), expectedSubstrings...),
					RejectSubstrings:   append([]string(nil), rejectSubstrings...),
					MinOutputChars:     minOutputChars,
					MaxOutputChars:     maxOutputChars,
					PassThreshold:      passThreshold,
				},
			}, cmd.CommandPath())
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}
			return protocol.WriteOK(out, optimizePromptsCommand, execution.Report,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
				protocol.WithCASDigest(execution.Artifact),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringSliceVar(&ids, "id", nil, "Saved prompt variant ID (repeatable)")
	cmd.Flags().StringVar(&question, "question", "", "Question or task input to send to each model")
	cmd.Flags().StringVar(&contextText, "context", "", "Optional extra context prepended to the user prompt")
	cmd.Flags().StringVar(&provider, "provider", "", "Provider name for the comparison client (default: remote-best with local fallback)")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL override")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key override (defaults to lm-studio for LMStudio)")
	cmd.Flags().StringSliceVar(&models, "model", nil, "Model ID to compare against (repeatable; default: openai/gpt-5.4-nano with local fallback)")
	cmd.Flags().StringSliceVar(&expectedSubstrings, "expect-substring", nil, "Substring that should appear in a passing output (repeatable)")
	cmd.Flags().StringSliceVar(&rejectSubstrings, "reject-substring", nil, "Substring that should not appear in a passing output (repeatable)")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "Per-request timeout")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 512, "Maximum tokens per response")
	cmd.Flags().Float64Var(&temperature, "temperature", 0.1, "Sampling temperature")
	cmd.Flags().Float64Var(&passThreshold, "pass-threshold", 0.8, "Score threshold for counting a comparison as a pass")
	cmd.Flags().IntVar(&minOutputChars, "min-output-chars", 0, "Minimum output length required for a full score")
	cmd.Flags().IntVar(&maxOutputChars, "max-output-chars", 0, "Maximum output length allowed for a full score")
	if err := cmd.MarkFlagRequired("id"); err != nil {
		panic(err)
	}
	cmd.Flags().StringVar(&evalDatasetFile, "eval-dataset-file", "", "JSONL eval dataset file with question/context/target_response rows")
	return cmd
}

func newOptimizePromptsCompareBatchCommand() *cobra.Command {
	var (
		workspace          string
		ids                []string
		agentRole          string
		targetProfile      string
		variantLimit       int
		evalDatasetFile    string
		provider           string
		baseURL            string
		apiKey             string
		models             []string
		expectedSubstrings []string
		rejectSubstrings   []string
		timeout            time.Duration
		maxTokens          int
		temperature        float64
		passThreshold      float64
		minOutputChars     int
		maxOutputChars     int
		rounds             int
		sampleSize         int
		seed               int64
	)

	cmd := &cobra.Command{
		Use:   "compare-batch",
		Short: "Generate multiple persisted comparison runs from eval-case minibatches",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}
			absWorkspace, err := absWorkspaceOrWriteError(out, optimizePromptsCommand, workspace)
			if err != nil {
				return err
			}
			if rounds <= 0 {
				return writeOptimizeError(out, optimizePromptsCommand, "rounds must be greater than 0")
			}

			variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("open prompt variant store: %v", err))
			}
			defer variantStore.Close() //nolint:errcheck

			variants, err := resolvePromptComparisonVariants(ctx, variantStore, absWorkspace, ids, agentRole, targetProfile, variantLimit)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}

			evalCases, err := loadPromptEvalCases(evalDatasetFile)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}
			if len(evalCases) == 0 {
				return writeOptimizeError(out, optimizePromptsCommand, "eval-dataset-file must contain at least one case")
			}
			if sampleSize <= 0 || sampleSize > len(evalCases) {
				sampleSize = len(evalCases)
			}

			primaryTarget, fallbackTarget, err := resolvePromptComparisonTargets(cfg, provider, baseURL, apiKey, models)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}

			type promptBatchRoundSummary struct {
				Round          int      `json:"round"`
				RunID          string   `json:"run_id"`
				Artifact       string   `json:"artifact"`
				EvalCaseIDs    []string `json:"eval_case_ids"`
				TopVariantID   string   `json:"top_variant_id"`
				TopMeanScore   float64  `json:"top_mean_score"`
				ScoreSpread    float64  `json:"score_spread"`
				SuccessCount   int      `json:"success_count"`
				FailureCount   int      `json:"failure_count"`
				FailedOver     bool     `json:"failed_over"`
				ActiveProvider string   `json:"active_provider"`
				ActiveModels   []string `json:"active_models"`
			}
			roundSummaries := make([]promptBatchRoundSummary, 0, rounds)
			winnerCounts := map[string]int{}
			caseUsage := map[string]int{}

			for round := 0; round < rounds; round++ {
				sampledCases := samplePromptEvalCases(evalCases, sampleSize, seed, round)
				effectiveQuestion := effectivePromptComparisonQuestion("", sampledCases)
				execution, err := executePromptComparisonRun(ctx, cfg, absWorkspace, promptComparisonRequest{
					Variants:    variants,
					Question:    effectiveQuestion,
					EvalCases:   sampledCases,
					Provider:    primaryTarget.Provider,
					BaseURL:     primaryTarget.BaseURL,
					APIKey:      primaryTarget.APIKey,
					Models:      primaryTarget.Models,
					Fallback:    fallbackTarget,
					Timeout:     timeout,
					MaxTokens:   maxTokens,
					Temperature: temperature,
					Scoring: promptComparisonScoring{
						ExpectedSubstrings: append([]string(nil), expectedSubstrings...),
						RejectSubstrings:   append([]string(nil), rejectSubstrings...),
						MinOutputChars:     minOutputChars,
						MaxOutputChars:     maxOutputChars,
						PassThreshold:      passThreshold,
					},
				}, cmd.CommandPath())
				if err != nil {
					return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("batch round %d: %v", round+1, err))
				}

				evalCaseIDs := promptEvalCaseIDs(sampledCases)
				for _, id := range evalCaseIDs {
					caseUsage[id]++
				}
				topVariantID := ""
				topMeanScore := 0.0
				scoreSpread := 0.0
				if len(execution.Ranking) > 0 {
					topVariantID = execution.Ranking[0].VariantID
					topMeanScore = execution.Ranking[0].MeanScore
					winnerCounts[topVariantID]++
					if len(execution.Ranking) > 1 {
						scoreSpread = execution.Ranking[0].MeanScore - execution.Ranking[len(execution.Ranking)-1].MeanScore
					}
				}
				roundSummaries = append(roundSummaries, promptBatchRoundSummary{
					Round:          round + 1,
					RunID:          execution.SavedRun.ID,
					Artifact:       execution.Artifact,
					EvalCaseIDs:    evalCaseIDs,
					TopVariantID:   topVariantID,
					TopMeanScore:   topMeanScore,
					ScoreSpread:    scoreSpread,
					SuccessCount:   execution.SuccessCount,
					FailureCount:   len(execution.Results) - execution.SuccessCount,
					FailedOver:     execution.FailedOver,
					ActiveProvider: execution.ActiveProvider,
					ActiveModels:   append([]string(nil), execution.ActiveModels...),
				})
			}

			type promptBatchWinnerSummary struct {
				VariantID string `json:"variant_id"`
				Wins      int    `json:"wins"`
			}
			winners := make([]promptBatchWinnerSummary, 0, len(winnerCounts))
			for variantID, wins := range winnerCounts {
				winners = append(winners, promptBatchWinnerSummary{VariantID: variantID, Wins: wins})
			}
			sort.Slice(winners, func(i, j int) bool {
				if winners[i].Wins == winners[j].Wins {
					return winners[i].VariantID < winners[j].VariantID
				}
				return winners[i].Wins > winners[j].Wins
			})

			type promptBatchCaseUsage struct {
				ID    string `json:"id"`
				Count int    `json:"count"`
			}
			usage := make([]promptBatchCaseUsage, 0, len(caseUsage))
			for id, count := range caseUsage {
				usage = append(usage, promptBatchCaseUsage{ID: id, Count: count})
			}
			sort.Slice(usage, func(i, j int) bool {
				if usage[i].Count == usage[j].Count {
					return usage[i].ID < usage[j].ID
				}
				return usage[i].Count > usage[j].Count
			})

			data := map[string]any{
				"operation":            "prompts.compare-batch",
				"workspace_id":         absWorkspace,
				"agent_role":           strings.TrimSpace(agentRole),
				"target_profile":       optimization.NormalizePromptTargetProfile(targetProfile),
				"variant_count":        len(variants),
				"eval_case_pool_count": len(evalCases),
				"round_count":          rounds,
				"sample_size":          sampleSize,
				"seed":                 seed,
				"primary_target": map[string]any{
					"provider": primaryTarget.Provider,
					"base_url": primaryTarget.BaseURL,
					"models":   primaryTarget.Models,
				},
				"winner_counts": winners,
				"case_usage":    usage,
				"rounds":        roundSummaries,
				"cli_command":   cmd.CommandPath(),
			}
			if fallbackTarget != nil {
				data["fallback_target"] = map[string]any{
					"provider": fallbackTarget.Provider,
					"base_url": fallbackTarget.BaseURL,
					"models":   fallbackTarget.Models,
				}
			}
			return protocol.WriteOK(out, optimizePromptsCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringSliceVar(&ids, "id", nil, "Saved prompt variant ID (repeatable)")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role to pull the latest variants from when --id is omitted")
	cmd.Flags().StringVar(&targetProfile, "target-profile", "", "Optional target prompt profile when resolving latest variants by role")
	cmd.Flags().IntVar(&variantLimit, "variant-limit", 5, "Number of latest variants to use when --role is set")
	cmd.Flags().StringVar(&evalDatasetFile, "eval-dataset-file", "", "JSONL eval dataset file with question/context/target_response rows")
	cmd.Flags().StringVar(&provider, "provider", "", "Provider name for the comparison client")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL override")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key override")
	cmd.Flags().StringSliceVar(&models, "model", nil, "Model ID to compare against (repeatable)")
	cmd.Flags().StringSliceVar(&expectedSubstrings, "expect-substring", nil, "Substring that should appear in a passing output (repeatable)")
	cmd.Flags().StringSliceVar(&rejectSubstrings, "reject-substring", nil, "Substring that should not appear in a passing output (repeatable)")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "Per-request timeout")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 512, "Maximum tokens per response")
	cmd.Flags().Float64Var(&temperature, "temperature", 0.1, "Sampling temperature")
	cmd.Flags().Float64Var(&passThreshold, "pass-threshold", 0.8, "Score threshold for counting a comparison as a pass")
	cmd.Flags().IntVar(&minOutputChars, "min-output-chars", 0, "Minimum output length required for a full score")
	cmd.Flags().IntVar(&maxOutputChars, "max-output-chars", 0, "Maximum output length allowed for a full score")
	cmd.Flags().IntVar(&rounds, "rounds", 20, "Number of persisted comparison rounds to run")
	cmd.Flags().IntVar(&sampleSize, "sample-size", 4, "Number of eval cases to sample into each round")
	cmd.Flags().Int64Var(&seed, "seed", 42, "Deterministic sampling seed for eval-case minibatches")
	if err := cmd.MarkFlagRequired("eval-dataset-file"); err != nil {
		panic(err)
	}
	return cmd
}

func newOptimizePromptsComparisonsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comparisons",
		Short: "Inspect persisted prompt comparison runs",
	}
	cmd.AddCommand(
		newOptimizePromptsComparisonsListCommand(),
		newOptimizePromptsComparisonsShowCommand(),
	)
	return cmd
}

func executePromptComparisonRun(
	ctx context.Context,
	cfg config.Config,
	workspaceID string,
	req promptComparisonRequest,
	cliCommand string,
) (*promptComparisonExecution, error) {
	results, err := runPromptVariantComparisons(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("compare prompt variants: %w", err)
	}

	successCount := 0
	for _, result := range results {
		if strings.TrimSpace(result.Error) == "" {
			successCount++
		}
	}
	aggregates := aggregatePromptVariantComparisons(results)
	activeProvider, activeBaseURL, activeModels, failedOver := summarizePromptComparisonExecution(promptComparisonTargetConfig{
		Provider: req.Provider,
		BaseURL:  req.BaseURL,
		Models:   req.Models,
	}, req.Fallback, results)

	report := map[string]any{
		"operation":    "prompts.compare",
		"workspace_id": workspaceID,
		"provider":     activeProvider,
		"base_url":     activeBaseURL,
		"primary_target": map[string]any{
			"provider": req.Provider,
			"base_url": req.BaseURL,
			"models":   req.Models,
		},
		"question":        strings.TrimSpace(req.Question),
		"context":         strings.TrimSpace(req.Context),
		"eval_case_count": len(req.EvalCases),
		"eval_cases":      req.EvalCases,
		"model_count":     len(activeModels),
		"models":          activeModels,
		"failed_over":     failedOver,
		"variant_count":   len(req.Variants),
		"success_count":   successCount,
		"failure_count":   len(results) - successCount,
		"results":         results,
		"ranking":         aggregates,
		"scoring": map[string]any{
			"expected_substrings": req.Scoring.ExpectedSubstrings,
			"reject_substrings":   req.Scoring.RejectSubstrings,
			"min_output_chars":    req.Scoring.MinOutputChars,
			"max_output_chars":    req.Scoring.MaxOutputChars,
			"pass_threshold":      req.Scoring.PassThreshold,
		},
		"cli_command": cliCommand,
	}
	if req.Fallback != nil {
		report["fallback_target"] = map[string]any{
			"provider": req.Fallback.Provider,
			"base_url": req.Fallback.BaseURL,
			"models":   req.Fallback.Models,
		}
	}

	artifact, err := persistPromptComparisonArtifact(ctx, cfg.Paths.CAS, report)
	if err != nil {
		return nil, fmt.Errorf("persist comparison artifact: %w", err)
	}
	runStore, err := optimization.OpenPromptComparisonRunStore(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, fmt.Errorf("open prompt comparison run store: %w", err)
	}
	defer runStore.Close() //nolint:errcheck
	savedRun, err := runStore.Save(ctx, optimization.PromptComparisonRun{
		WorkspaceID:    workspaceID,
		ArtifactDigest: artifact,
		Provider:       activeProvider,
		BaseURL:        activeBaseURL,
		Question:       strings.TrimSpace(req.Question),
		Context:        strings.TrimSpace(req.Context),
		ModelCount:     len(activeModels),
		VariantCount:   len(req.Variants),
		SuccessCount:   successCount,
		FailureCount:   len(results) - successCount,
	})
	if err != nil {
		return nil, fmt.Errorf("save comparison run: %w", err)
	}
	report["artifact"] = artifact
	report["saved_run"] = savedRun

	return &promptComparisonExecution{
		Report:         report,
		Results:        results,
		Ranking:        aggregates,
		SavedRun:       savedRun,
		Artifact:       artifact,
		ActiveProvider: activeProvider,
		ActiveBaseURL:  activeBaseURL,
		ActiveModels:   activeModels,
		FailedOver:     failedOver,
		SuccessCount:   successCount,
	}, nil
}

func newOptimizePromptsComparisonsListCommand() *cobra.Command {
	var workspace string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved prompt comparison runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}
			absWorkspace, err := absWorkspaceOrWriteError(out, optimizePromptsCommand, workspace)
			if err != nil {
				return err
			}

			runStore, err := optimization.OpenPromptComparisonRunStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("open prompt comparison run store: %v", err))
			}
			defer runStore.Close() //nolint:errcheck

			runs, err := runStore.List(ctx, absWorkspace, limit)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("list comparison runs: %v", err))
			}

			data := map[string]any{
				"operation":    "prompts.comparisons.list",
				"workspace_id": absWorkspace,
				"count":        len(runs),
				"runs":         runs,
				"cli_command":  cmd.CommandPath(),
			}
			return protocol.WriteOK(out, optimizePromptsCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum runs to return")
	return cmd
}

func newOptimizePromptsComparisonsShowCommand() *cobra.Command {
	var workspace string
	var id string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show one saved prompt comparison run and report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, err.Error())
			}
			absWorkspace, err := absWorkspaceOrWriteError(out, optimizePromptsCommand, workspace)
			if err != nil {
				return err
			}

			runStore, err := optimization.OpenPromptComparisonRunStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("open prompt comparison run store: %v", err))
			}
			defer runStore.Close() //nolint:errcheck

			run, err := runStore.Get(ctx, absWorkspace, id)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("get comparison run: %v", err))
			}

			report, err := readPromptComparisonArtifact(ctx, cfg.Paths.CAS, run.ArtifactDigest)
			if err != nil {
				return writeOptimizeError(out, optimizePromptsCommand, fmt.Sprintf("read comparison artifact: %v", err))
			}

			data := map[string]any{
				"operation":    "prompts.comparisons.show",
				"workspace_id": absWorkspace,
				"run":          run,
				"report":       report,
				"cli_command":  cmd.CommandPath(),
			}
			return protocol.WriteOK(out, optimizePromptsCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
				protocol.WithCASDigest(run.ArtifactDigest),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&id, "id", "", "Comparison run ID to fetch")
	if err := cmd.MarkFlagRequired("id"); err != nil {
		panic(err)
	}
	return cmd
}

func runPromptVariantComparisons(ctx context.Context, req promptComparisonRequest) ([]promptVariantComparison, error) {
	if len(req.Variants) == 0 {
		return nil, fmt.Errorf("at least one prompt variant is required")
	}
	if len(req.Models) == 0 {
		return nil, fmt.Errorf("at least one model is required")
	}
	if strings.TrimSpace(req.Question) == "" && len(req.EvalCases) == 0 {
		return nil, fmt.Errorf("question is required")
	}
	if strings.TrimSpace(req.Provider) == "" {
		req.Provider = "lmstudio"
	}
	if strings.TrimSpace(req.APIKey) == "" && strings.EqualFold(req.Provider, "lmstudio") {
		req.APIKey = "lm-studio"
	}

	results := make([]promptVariantComparison, 0, len(req.Variants)*len(req.Models))
	evalCases := req.EvalCases
	if len(evalCases) == 0 {
		evalCases = []promptEvalCase{{
			Question: strings.TrimSpace(req.Question),
			Context:  strings.TrimSpace(req.Context),
		}}
	}
	for _, variant := range req.Variants {
		for _, evalCase := range evalCases {
			userPrompt := buildPromptComparisonUserPrompt(evalCase.Question, evalCase.Context)
			for _, model := range req.Models {
				activeProvider := req.Provider
				activeBaseURL := strings.TrimSpace(req.BaseURL)
				activeAPIKey := req.APIKey
				activeModel := strings.TrimSpace(model)
				start := time.Now()
				output, callErr := runPromptVariantComparisonChat(ctx, activeProvider, activeBaseURL, activeAPIKey, activeModel, variant.Prompt, userPrompt, req.Timeout, req.MaxTokens, req.Temperature)
				failedOver := false
				if callErr != nil && req.Fallback != nil {
					fallbackModel := ""
					if len(req.Fallback.Models) > 0 {
						fallbackModel = strings.TrimSpace(req.Fallback.Models[0])
					}
					if fallbackModel != "" {
						activeProvider = req.Fallback.Provider
						activeBaseURL = strings.TrimSpace(req.Fallback.BaseURL)
						activeAPIKey = req.Fallback.APIKey
						activeModel = fallbackModel
						failedOver = true
						output, callErr = runPromptVariantComparisonChat(ctx, activeProvider, activeBaseURL, activeAPIKey, activeModel, variant.Prompt, userPrompt, req.Timeout, req.MaxTokens, req.Temperature)
					}
				}

				result := promptVariantComparison{
					VariantID:  variant.ID,
					AgentRole:  variant.AgentRole,
					Mode:       variant.Mode,
					Provider:   activeProvider,
					Model:      activeModel,
					EvalCaseID: strings.TrimSpace(evalCase.ID),
					Category:   strings.TrimSpace(evalCase.Category),
					DurationMS: time.Since(start).Milliseconds(),
					ScoreDelta: variant.OptimizedScore - variant.OriginalScore,
					FailedOver: failedOver,
				}
				if callErr != nil {
					result.Error = callErr.Error()
				} else {
					result.Output = strings.TrimSpace(output)
				}
				result.Score = scorePromptComparisonAgainstCase(result, req.Scoring, evalCase)
				result.Passed = result.Score >= req.Scoring.PassThreshold
				results = append(results, result)
			}
		}
	}
	return results, nil
}

func runPromptVariantComparisonChat(ctx context.Context, provider, baseURL, apiKey, model, systemPrompt, userPrompt string, timeout time.Duration, maxTokens int, temperature float64) (string, error) {
	client, err := verification.NewOpenAIClient(verification.OpenAIConfig{
		Provider: provider,
		BaseURL:  baseURL,
		APIKey:   apiKey,
		Model:    model,
		Timeout:  timeout,
	})
	if err != nil {
		return "", err
	}
	return client.Chat(ctx, systemPrompt, userPrompt, verification.LLMCallOptions{
		MaxTokens:   maxTokens,
		Temperature: temperature,
	})
}

func summarizePromptComparisonExecution(primary promptComparisonTargetConfig, fallback *promptComparisonTargetConfig, results []promptVariantComparison) (string, string, []string, bool) {
	activeProvider := primary.Provider
	activeBaseURL := primary.BaseURL
	activeModels := append([]string(nil), primary.Models...)
	failedOver := false
	if len(results) == 0 {
		return activeProvider, activeBaseURL, activeModels, failedOver
	}
	modelSet := map[string]struct{}{}
	for _, result := range results {
		if strings.TrimSpace(result.Provider) != "" {
			activeProvider = result.Provider
			if result.Provider != primary.Provider {
				failedOver = true
				if fallback != nil {
					activeBaseURL = fallback.BaseURL
				}
			}
		}
		if strings.TrimSpace(result.Model) != "" {
			modelSet[result.Model] = struct{}{}
		}
		if result.FailedOver {
			failedOver = true
		}
	}
	if len(modelSet) > 0 {
		activeModels = activeModels[:0]
		for model := range modelSet {
			activeModels = append(activeModels, model)
		}
		sort.Strings(activeModels)
	}
	return activeProvider, activeBaseURL, activeModels, failedOver
}

func scorePromptComparisonAgainstCase(result promptVariantComparison, scoring promptComparisonScoring, evalCase promptEvalCase) float64 {
	if strings.TrimSpace(result.Error) != "" {
		return 0
	}

	output := strings.TrimSpace(result.Output)
	if output == "" {
		return 0
	}

	score := 1.0

	if len(scoring.ExpectedSubstrings) > 0 {
		matched := 0
		for _, expected := range scoring.ExpectedSubstrings {
			if strings.Contains(strings.ToLower(output), strings.ToLower(strings.TrimSpace(expected))) {
				matched++
			}
		}
		score *= float64(matched) / float64(len(scoring.ExpectedSubstrings))
	}

	if len(scoring.RejectSubstrings) > 0 {
		rejected := 0
		for _, forbidden := range scoring.RejectSubstrings {
			if strings.Contains(strings.ToLower(output), strings.ToLower(strings.TrimSpace(forbidden))) {
				rejected++
			}
		}
		if rejected > 0 {
			score *= 1 - (float64(rejected) / float64(len(scoring.RejectSubstrings)))
		}
	}

	outputLen := len([]rune(output))
	if scoring.MinOutputChars > 0 && outputLen < scoring.MinOutputChars {
		score *= 0.5
	}
	if scoring.MaxOutputChars > 0 && outputLen > scoring.MaxOutputChars {
		score *= 0.5
	}
	if strings.TrimSpace(evalCase.TargetResponse) != "" || strings.TrimSpace(evalCase.Question) != "" || strings.TrimSpace(evalCase.Context) != "" {
		score *= optimization.DefaultPromptJudge().Score(optimization.PromptJudgeInput{
			Question:       evalCase.Question,
			Context:        evalCase.Context,
			TargetResponse: evalCase.TargetResponse,
			Output:         output,
		})
	}
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func aggregatePromptVariantComparisons(results []promptVariantComparison) []promptVariantAggregate {
	if len(results) == 0 {
		return nil
	}

	type running struct {
		aggregate promptVariantAggregate
		scores    []float64
	}

	byVariant := map[string]*running{}
	for _, result := range results {
		item, ok := byVariant[result.VariantID]
		if !ok {
			item = &running{
				aggregate: promptVariantAggregate{
					VariantID:  result.VariantID,
					AgentRole:  result.AgentRole,
					Mode:       result.Mode,
					BestScore:  result.Score,
					WorstScore: result.Score,
					ScoreDelta: result.ScoreDelta,
				},
			}
			byVariant[result.VariantID] = item
		}

		item.aggregate.ComparisonCount++
		item.aggregate.AvgDurationMS += float64(result.DurationMS)
		if strings.TrimSpace(result.Error) != "" {
			item.aggregate.ErrorCount++
		}
		if result.Passed {
			item.aggregate.PassCount++
		}
		if result.Score > item.aggregate.BestScore {
			item.aggregate.BestScore = result.Score
		}
		if result.Score < item.aggregate.WorstScore {
			item.aggregate.WorstScore = result.Score
		}
		item.scores = append(item.scores, result.Score)
	}

	aggregates := make([]promptVariantAggregate, 0, len(byVariant))
	for _, item := range byVariant {
		total := 0.0
		for _, score := range item.scores {
			total += score
		}
		item.aggregate.MeanScore = total / float64(len(item.scores))
		item.aggregate.AvgDurationMS = item.aggregate.AvgDurationMS / float64(item.aggregate.ComparisonCount)

		var variance float64
		for _, score := range item.scores {
			diff := score - item.aggregate.MeanScore
			variance += diff * diff
		}
		item.aggregate.ScoreVariance = variance / float64(len(item.scores))
		aggregates = append(aggregates, item.aggregate)
	}

	sort.Slice(aggregates, func(i, j int) bool {
		if aggregates[i].MeanScore != aggregates[j].MeanScore {
			return aggregates[i].MeanScore > aggregates[j].MeanScore
		}
		if aggregates[i].WorstScore != aggregates[j].WorstScore {
			return aggregates[i].WorstScore > aggregates[j].WorstScore
		}
		if aggregates[i].ScoreVariance != aggregates[j].ScoreVariance {
			return aggregates[i].ScoreVariance < aggregates[j].ScoreVariance
		}
		return aggregates[i].VariantID < aggregates[j].VariantID
	})

	return aggregates
}

func buildPromptComparisonUserPrompt(question, contextText string) string {
	question = strings.TrimSpace(question)
	contextText = strings.TrimSpace(contextText)
	if contextText == "" {
		return question
	}
	return "Context:\n" + contextText + "\n\nQuestion:\n" + question
}

func persistPromptComparisonArtifact(ctx context.Context, casRoot string, report any) (string, error) {
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	obj, err := store.Put(ctx, strings.NewReader(string(append(body, '\n'))), "application/json", []string{"gepa", "prompt-comparison"})
	if err != nil {
		return "", err
	}
	return obj.Digest, nil
}

func readPromptComparisonArtifact(ctx context.Context, casRoot, digest string) (map[string]any, error) {
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	rc, _, err := store.Get(ctx, digest)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var report map[string]any
	if err := json.NewDecoder(rc).Decode(&report); err != nil {
		return nil, err
	}
	return report, nil
}

func persistTranscriptDatasetArtifact(ctx context.Context, casRoot string, examples []optimization.TranscriptTrainingExample) (string, error) {
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()
	body, err := optimization.BuildTranscriptDatasetJSONL(examples)
	if err != nil {
		return "", err
	}
	obj, err := store.Put(ctx, strings.NewReader(string(body)), "application/jsonl", []string{"gepa", "transcript-dataset"})
	if err != nil {
		return "", err
	}
	return obj.Digest, nil
}

// --- Analyze subcommand ---

func newOptimizeAnalyzeCommand() *cobra.Command {
	var (
		workspace string
		agentRole string
		since     string
		limit     int
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze trajectories and compute optimization statistics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			start := time.Now()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizeAnalyzeCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizeAnalyzeCommand, workspace)
			if err != nil {
				return err
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeAnalyzeCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck

			patternStore, err := optimization.OpenPatternStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeAnalyzeCommand, fmt.Sprintf("open pattern store: %v", err))
			}
			defer patternStore.Close() //nolint:errcheck

			// Parse since timestamp
			var sinceTS time.Time
			if since != "" {
				sinceTS, err = time.Parse(time.RFC3339, since)
				if err != nil {
					return writeOptimizeError(out, optimizeAnalyzeCommand, fmt.Sprintf("invalid since timestamp: %v", err))
				}
			} else {
				sinceTS = time.Now().AddDate(0, 0, -30) // Default: last 30 days
			}

			// Get trajectories for analysis
			filter := trajectory.ListFilter{
				WorkspaceID: absWorkspace,
				AgentRole:   agentRole,
				Since:       sinceTS,
				Limit:       limit,
			}

			trajectories, err := trajStore.ListTrajectories(ctx, filter)
			if err != nil {
				return writeOptimizeError(out, optimizeAnalyzeCommand, fmt.Sprintf("list trajectories: %v", err))
			}

			// Compute statistics
			stats := computeTrajectoryStats(trajectories)

			// Get top patterns
			topPatterns, err := patternStore.GetTopPatterns(ctx, agentRole, 10)
			if err != nil {
				topPatterns = nil // Non-fatal
			}

			// Get bootstrap stats
			bootstrapConfig := optimization.DefaultBootstrapConfig()
			optimizer := optimization.NewBootstrapOptimizer(trajStore, patternStore, bootstrapConfig)
			exampleStats, err := optimizer.GetExampleStats(ctx, absWorkspace, agentRole)
			if err != nil {
				exampleStats = nil // Continue without example stats if unavailable
			}

			data := map[string]any{
				"operation":        "analyze",
				"workspace_id":     absWorkspace,
				"agent_role":       agentRole,
				"trajectory_count": len(trajectories),
				"statistics":       stats,
				"top_patterns":     topPatterns,
				"example_stats":    exampleStats,
				"filter": map[string]any{
					"since": sinceTS.Format(time.RFC3339),
					"limit": limit,
				},
				"duration_ms": time.Since(start).Milliseconds(),
				"cli_command": cmd.CommandPath(),
			}

			return protocol.WriteOK(out, optimizeAnalyzeCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role to analyze (required)")
	cmd.Flags().StringVar(&since, "since", "", "Analyze trajectories since RFC3339 timestamp (default: 30 days ago)")
	cmd.Flags().IntVar(&limit, "limit", 1000, "Maximum trajectories to analyze")
	if err := cmd.MarkFlagRequired("role"); err != nil {
		panic(err)
	}
	return cmd
}

func computeTrajectoryStats(trajectories []trajectory.Trajectory) map[string]any {
	if len(trajectories) == 0 {
		return map[string]any{
			"total":        0,
			"success_rate": 0.0,
		}
	}

	var successCount, errorCount, abortedCount int
	var totalDuration int64
	toolCounts := map[string]int{}

	for _, t := range trajectories {
		switch t.Status {
		case trajectory.StatusOK:
			successCount++
		case trajectory.StatusError:
			errorCount++
		case trajectory.StatusAborted, trajectory.StatusPartial:
			abortedCount++
		}

		if t.Outcome != nil {
			totalDuration += t.Outcome.DurationMS
		}
	}

	total := len(trajectories)
	successRate := float64(successCount) / float64(total)
	avgDuration := totalDuration / int64(total)

	return map[string]any{
		"total":           total,
		"success_count":   successCount,
		"error_count":     errorCount,
		"aborted_count":   abortedCount,
		"success_rate":    successRate,
		"avg_duration_ms": avgDuration,
		"tool_usage":      toolCounts,
	}
}

// --- Weights subcommand ---

func newOptimizeWeightsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "weights",
		Short: "Manage learnable scorer weights",
	}
	cmd.AddCommand(
		newOptimizeWeightsShowCommand(),
		newOptimizeWeightsLearnCommand(),
	)
	return cmd
}

func newOptimizeWeightsShowCommand() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show current scorer weights",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizeWeightsCommand, workspace)
			if err != nil {
				return err
			}

			cfg, err := loadConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizeWeightsCommand, err.Error())
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeWeightsCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck

			weightStore := optimization.NewInMemoryWeightStore()
			// Try to load existing weights from store (would be persisted in real implementation)
			scorer := optimization.NewLearnableScorer(trajStore, weightStore, optimization.DefaultLearnerConfig())
			weights, err := scorer.GetCurrentWeights(ctx, absWorkspace)
			if err != nil {
				// Use defaults if no weights found
				weights = optimization.DefaultScorerWeights()
			}

			data := map[string]any{
				"operation": "show",
				"weights": map[string]any{
					"critical_path": weights.CriticalPath,
					"page_rank":     weights.PageRank,
					"admin_mail":    weights.AdminMail,
					"overseer_mail": weights.OverseerMail,
					"recency":       weights.Recency,
				},
				"cli_command": cmd.CommandPath(),
			}

			return protocol.WriteOK(out, optimizeWeightsCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	return cmd
}

func newOptimizeWeightsLearnCommand() *cobra.Command {
	var (
		workspace    string
		lookbackDays int
	)

	cmd := &cobra.Command{
		Use:   "learn",
		Short: "Learn optimal weights from outcome data",
		Long: `Analyze trajectory outcomes to learn optimal scorer weights.
Uses correlation analysis between current weights and success rates
to suggest improved weight distributions.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			start := time.Now()

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizeWeightsCommand, workspace)
			if err != nil {
				return err
			}

			cfg, err := loadConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizeWeightsCommand, err.Error())
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeWeightsCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck

			weightStore := optimization.NewInMemoryWeightStore()
			learnerConfig := optimization.LearnerConfig{
				LearningRate:    0.1,
				MinSamples:      10,
				MaxWeightChange: 0.05,
				LookbackDays:    lookbackDays,
			}
			scorer := optimization.NewLearnableScorer(trajStore, weightStore, learnerConfig)

			// Learn from outcomes
			update, err := scorer.LearnFromOutcomes(ctx, absWorkspace)
			if err != nil {
				return writeOptimizeError(out, optimizeWeightsCommand, fmt.Sprintf("learn weights: %v", err))
			}

			data := map[string]any{
				"operation":         "learn",
				"trajectories_used": update.SampleSize,
				"previous_weights": map[string]any{
					"critical_path": update.PreviousWeights.CriticalPath,
					"page_rank":     update.PreviousWeights.PageRank,
					"admin_mail":    update.PreviousWeights.AdminMail,
					"overseer_mail": update.PreviousWeights.OverseerMail,
					"recency":       update.PreviousWeights.Recency,
				},
				"new_weights": map[string]any{
					"critical_path": update.NewWeights.CriticalPath,
					"page_rank":     update.NewWeights.PageRank,
					"admin_mail":    update.NewWeights.AdminMail,
					"overseer_mail": update.NewWeights.OverseerMail,
					"recency":       update.NewWeights.Recency,
				},
				"reason":      update.Reason,
				"duration_ms": time.Since(start).Milliseconds(),
				"cli_command": cmd.CommandPath(),
			}

			return protocol.WriteOK(out, optimizeWeightsCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().IntVar(&lookbackDays, "lookback-days", 30, "Days to look back for trajectories")
	return cmd
}

// --- Reflect subcommand ---

func newOptimizeReflectCommand() *cobra.Command {
	var (
		workspace    string
		trajectoryID string
		agentRole    string
	)

	cmd := &cobra.Command{
		Use:   "reflect",
		Short: "Generate reflections on trajectory performance",
		Long: `Analyze trajectories and generate reflections on strengths,
weaknesses, and suggestions for improvement.

Can analyze a single trajectory by ID or batch analyze recent
trajectories for an agent role.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			start := time.Now()

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizeCommand, workspace)
			if err != nil {
				return err
			}

			cfg, err := loadConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, err.Error())
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck

			patternStore, err := optimization.OpenPatternStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("open pattern store: %v", err))
			}
			defer patternStore.Close() //nolint:errcheck

			engine := optimization.NewReflectionEngine(trajStore, patternStore, optimization.DefaultReflectionConfig())

			if trajectoryID != "" {
				// Single trajectory reflection
				reflection, err := engine.ReflectOnTrajectory(ctx, absWorkspace, trajectoryID)
				if err != nil {
					return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("reflect: %v", err))
				}

				data := map[string]any{
					"operation":     "reflect",
					"trajectory_id": trajectoryID,
					"reflection":    reflection,
					"duration_ms":   time.Since(start).Milliseconds(),
					"cli_command":   cmd.CommandPath(),
				}

				return protocol.WriteOK(out, optimizeCommand, data,
					protocol.WithSource("run"),
					protocol.WithWorkspace(absWorkspace),
				)
			}

			// Batch reflection for agent role
			if agentRole == "" {
				return writeOptimizeError(out, optimizeCommand, "either --traj-id or --role is required")
			}

			summary, err := engine.GenerateSummary(ctx, absWorkspace, agentRole)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("generate summary: %v", err))
			}

			data := map[string]any{
				"operation":   "reflect",
				"agent_role":  agentRole,
				"summary":     summary,
				"duration_ms": time.Since(start).Milliseconds(),
				"cli_command": cmd.CommandPath(),
			}

			return protocol.WriteOK(out, optimizeCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&trajectoryID, "traj-id", "", "Trajectory ID to reflect on")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role for batch reflection")
	return cmd
}

// --- Feedback subcommand ---

func newOptimizeFeedbackCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Manage human feedback on trajectories",
	}
	cmd.AddCommand(
		newOptimizeFeedbackAddCommand(),
		newOptimizeFeedbackStatsCommand(),
	)
	return cmd
}

func newOptimizeFeedbackAddCommand() *cobra.Command {
	var (
		workspace    string
		trajectoryID string
		rating       int
		comment      string
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add feedback on a trajectory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizeCommand, workspace)
			if err != nil {
				return err
			}

			if rating < 1 || rating > 5 {
				return writeOptimizeError(out, optimizeCommand, "rating must be between 1 and 5")
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck

			collector := optimization.NewFeedbackCollector(trajStore)

			feedback := optimization.HumanFeedback{
				TrajectoryID: trajectoryID,
				WorkspaceID:  absWorkspace,
				Rating:       rating,
				Feedback:     comment,
				Timestamp:    time.Now(),
			}

			if err := collector.RecordFeedback(ctx, feedback); err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("record feedback: %v", err))
			}

			data := map[string]any{
				"operation":     "feedback.add",
				"trajectory_id": trajectoryID,
				"rating":        rating,
				"comment":       comment,
				"recorded":      true,
				"cli_command":   cmd.CommandPath(),
			}

			return protocol.WriteOK(out, optimizeCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&trajectoryID, "traj-id", "", "Trajectory ID to rate (required)")
	cmd.Flags().IntVar(&rating, "rating", 0, "Rating 1-5 (required)")
	cmd.Flags().StringVar(&comment, "comment", "", "Optional feedback comment")
	if err := cmd.MarkFlagRequired("traj-id"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("rating"); err != nil {
		panic(err)
	}
	return cmd
}

func newOptimizeFeedbackStatsCommand() *cobra.Command {
	var (
		workspace string
		agentRole string
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show feedback statistics for an agent role",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizeCommand, workspace)
			if err != nil {
				return err
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer trajStore.Close() //nolint:errcheck

			collector := optimization.NewFeedbackCollector(trajStore)

			stats, err := collector.GetFeedbackStats(ctx, absWorkspace, agentRole)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("get feedback stats: %v", err))
			}

			data := map[string]any{
				"operation":   "feedback.stats",
				"agent_role":  agentRole,
				"stats":       stats,
				"cli_command": cmd.CommandPath(),
			}

			return protocol.WriteOK(out, optimizeCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&agentRole, "role", "", "Agent role to get stats for (required)")
	if err := cmd.MarkFlagRequired("role"); err != nil {
		panic(err)
	}
	return cmd
}

// --- Session subcommand ---

const optimizeSessionCommand = "optimize.session"

func newOptimizeSessionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Analyze session-level feedback",
	}
	cmd.AddCommand(newOptimizeSessionAnalyzeCommand())
	return cmd
}

func newOptimizeSessionAnalyzeCommand() *cobra.Command {
	var (
		workspace string
		since     string
		minRating int
		maxRating int
		outcome   string
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze session feedback patterns and generate recommendations",
		Long: `Analyze session feedback collected via the session/feedback skill
and generate optimization recommendations based on patterns.

Examples:
  # Analyze all feedback for current workspace
  agentctl optimize session analyze

  # Analyze feedback from the last week
  agentctl optimize session analyze --since 2024-01-01

  # Only analyze sessions with low ratings
  agentctl optimize session analyze --max-rating 2

  # Focus on failed sessions
  agentctl optimize session analyze --outcome failure`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(cmd.Context())
			if err != nil {
				return writeOptimizeError(out, optimizeSessionCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizeSessionCommand, workspace)
			if err != nil {
				return err
			}

			// Build input for optimize/feedback skill
			input := map[string]any{
				"workspace": absWorkspace,
			}
			if since != "" {
				input["since"] = since
			}
			if minRating > 0 {
				input["min_rating"] = minRating
			}
			if maxRating > 0 && maxRating < 5 {
				input["max_rating"] = maxRating
			}
			if outcome != "" {
				input["outcome"] = outcome
			}

			inputJSON, err := json.Marshal(input)
			if err != nil {
				return writeOptimizeError(out, optimizeSessionCommand, fmt.Sprintf("marshal input: %v", err))
			}

			// Find skill
			skillName := "optimize/feedback"
			handle, err := findSkill(cfg, skillName)
			if err != nil {
				return writeOptimizeError(out, optimizeSessionCommand, fmt.Sprintf("find skill: %v", err))
			}

			// Build run options
			opts := runservice.RunOptions{
				SkillName:     skillName,
				Input:         inputJSON,
				CacheMode:     cache.ModeOff, // Always run fresh
				Workspace:     absWorkspace,
				Timeout:       2 * time.Minute,
				CorrelationID: "",
				SessionID:     resolveSessionID(),
			}
			if err := opts.Validate(); err != nil {
				return writeOptimizeError(out, optimizeSessionCommand, err.Error())
			}

			// Execute skill
			ctx := cmd.Context()
			executor := runservice.NewExecutor(ctx, cfg, handle, out, cmd.ErrOrStderr(), opts)
			defer executor.Close()

			// Execute through job system
			job, isDuplicate, err := executor.PrepareJob(inputJSON)
			if err != nil {
				return writeOptimizeError(out, optimizeSessionCommand, fmt.Sprintf("prepare job: %v", err))
			}
			if isDuplicate {
				return executor.HandleDuplicate(job)
			}
			return executor.ExecuteSync(job)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Filter feedback by workspace")
	cmd.Flags().StringVar(&since, "since", "", "Only analyze feedback since this date (ISO 8601 or YYYY-MM-DD)")
	cmd.Flags().IntVar(&minRating, "min-rating", 0, "Minimum rating to include (default: 1)")
	cmd.Flags().IntVar(&maxRating, "max-rating", 0, "Maximum rating to include (default: 5)")
	cmd.Flags().StringVar(&outcome, "outcome", "", "Filter by outcome: success, partial, failure, abandoned")

	return cmd
}

// Helper functions

func writeOptimizeError(w io.Writer, command, msg string) error {
	_ = protocol.WriteError(w, command, protocol.ErrorCodeEARG, msg, map[string]any{"hint": "check flags and configuration"}, protocol.WithSource("run")) //nolint:errcheck
	return fmt.Errorf("%s: %s", command, msg)
}

func init() {
	rootCmd.AddCommand(newOptimizeCommand())
}
