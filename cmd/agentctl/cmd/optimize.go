package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/spf13/cobra"
)

const (
	optimizeCommand         = "optimize"
	optimizePatternsCommand = "optimize.patterns"
	optimizeWeightsCommand  = "optimize.weights"
	optimizeAnalyzeCommand  = "optimize.analyze"
)

func newOptimizeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "optimize",
		Short: "Agent optimization and learning commands",
		Long: `Commands for optimizing agent performance using collected trajectory data.

Supports online pattern learning, offline batch optimization (bootstrap few-shot,
prompt optimization), and feedback-driven weight learning.`,
	}
	cmd.AddCommand(
		newOptimizePatternsCommand(),
		newOptimizeBootstrapCommand(),
		newOptimizeAnalyzeCommand(),
		newOptimizeWeightsCommand(),
		newOptimizeReflectCommand(),
		newOptimizeFeedbackCommand(),
	)
	return cmd
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

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				absWorkspace = workspace // Fallback to original
			}
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

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				absWorkspace = workspace // Fallback to original
			}
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

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				absWorkspace = workspace // Fallback to original
			}
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

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("resolve workspace: %v", err))
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

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return writeOptimizeError(out, optimizeAnalyzeCommand, fmt.Sprintf("resolve workspace: %v", err))
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

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return writeOptimizeError(out, optimizeWeightsCommand, fmt.Sprintf("resolve workspace: %v", err))
			}

			cfg, err := config.Load(ctx)
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

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return writeOptimizeError(out, optimizeWeightsCommand, fmt.Sprintf("resolve workspace: %v", err))
			}

			cfg, err := config.Load(ctx)
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

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("resolve workspace: %v", err))
			}

			cfg, err := config.Load(ctx)
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

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("resolve workspace: %v", err))
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

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return writeOptimizeError(out, optimizeCommand, fmt.Sprintf("resolve workspace: %v", err))
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

// Helper functions

func writeOptimizeError(w io.Writer, command, msg string) error {
	_ = protocol.WriteError(w, command, protocol.ErrorCodeEARG, msg, map[string]any{"hint": "check flags and configuration"}, protocol.WithSource("run")) //nolint:errcheck
	return fmt.Errorf("%s: %s", command, msg)
}

func init() {
	rootCmd.AddCommand(newOptimizeCommand())
}
