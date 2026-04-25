package cmd

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/rlm"
	rlmenv "github.com/joshka0/foxctl/internal/rlm/env"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/spf13/cobra"
)

func newRLMCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rlm",
		Short: "Experimental Recursive Language Model runtime tools",
	}
	cmd.AddCommand(newRLMRunCommand())
	return cmd
}

func newRLMRunCommand() *cobra.Command {
	var prompt string
	var workspacePath string
	var vaultPath string
	var maxDepth int
	var maxIterations int
	var maxSubcalls int
	var executor string
	var llmProvider string
	var llmModel string
	var llmBaseURL string
	var llmAPIKey string
	var llmTimeout time.Duration
	var requireToolUse bool
	var toolProfile string
	var routeProfile string
	var planMode string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Bootstrap and execute the experimental read-only RLM runtime",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(cmd.Context())
			if err != nil {
				return err
			}
			task := rlm.Task{
				Prompt:        strings.TrimSpace(prompt),
				WorkspaceID:   "",
				WorkspaceRoot: resolveContextWorkspace(workspacePath),
				MaxDepth:      maxDepth,
				MaxIterations: maxIterations,
				MaxSubcalls:   maxSubcalls,
			}
			companionDB, companionClose, _ := openRLMCompanionDB(cmd.Context(), cfg)
			if companionClose != nil {
				defer func() { _ = companionClose() }()
			}
			bootstrapper := rlmenv.NewBootstrapper(rlmenv.BootstrapConfig{
				AppConfig:   cfg,
				VaultPath:   strings.TrimSpace(vaultPath),
				CompanionDB: companionDB,
			})
			env, err := bootstrapper.Build(cmd.Context(), task)
			if err != nil {
				return err
			}
			env.Tools = rlmenv.FilterTools(env.Tools, toolProfile)
			var runRecursive func(context.Context, rlm.Task, rlm.Environment) (rlm.Result, error)
			runRecursive = func(ctx context.Context, currentTask rlm.Task, currentEnv rlm.Environment) (rlm.Result, error) {
				currentTask, currentEnv = applyRLMScoutRole(currentTask, currentEnv)
				currentAdapter := rlmenv.NewReadOnlyAdapter(cfg, currentTask.WorkspaceRoot, strings.TrimSpace(vaultPath), companionDB, currentEnv)
				currentAdapter.SetSubcall(runRecursive)
				runner := chooseRLMRunner(executor, currentAdapter, currentTask, currentEnv, llmProvider, llmModel, llmBaseURL, llmAPIKey, llmTimeout, requireToolUse, routeProfile, planMode)
				return runner.Run(ctx, currentTask, currentEnv)
			}
			result, err := runRecursive(cmd.Context(), task, env)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("rlm/run", map[string]any{
				"task":        task,
				"environment": env,
				"result":      result,
				"mode":        normalizedRLMExecutor(executor),
			}, envelope.WithMeta(envelope.Meta{Source: "cli", Workspace: task.WorkspaceRoot})))
		},
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt for the RLM runtime")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: current workspace)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Optional vault path for knowledge-plane bootstrap")
	cmd.Flags().StringVar(&executor, "executor", "inspect", "Executor mode: inspect or llm")
	cmd.Flags().StringVar(&llmProvider, "llm-provider", "", "LLM provider override (e.g. lmstudio, openai, openrouter)")
	cmd.Flags().StringVar(&llmModel, "llm-model", "", "LLM model override")
	cmd.Flags().StringVar(&llmBaseURL, "llm-base-url", "", "LLM base URL override")
	cmd.Flags().StringVar(&llmAPIKey, "llm-api-key", "", "LLM API key override")
	cmd.Flags().DurationVar(&llmTimeout, "llm-timeout", 0, "LLM timeout override")
	cmd.Flags().BoolVar(&requireToolUse, "require-tool-use", true, "Require the LLM executor to make at least one tool call before answering")
	cmd.Flags().StringVar(&toolProfile, "tool-profile", rlmenv.ToolProfileDefault, "Experimental RLM tool profile: default or code-intel")
	cmd.Flags().StringVar(&routeProfile, "route-profile", string(rlm.RouteProfileAuto), "Experimental RLM route profile: auto, code_retrieval, memory_recall, mixed, or evidence_audit")
	cmd.Flags().StringVar(&planMode, "plan-mode", string(rlm.PlanModeFree), "Experimental planning mode: free, guided, staged, or hard")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 1, "Maximum recursion depth")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 8, "Maximum root iterations")
	cmd.Flags().IntVar(&maxSubcalls, "max-subcalls", 8, "Maximum subcalls")
	_ = cmd.MarkFlagRequired("prompt")
	return cmd
}

func normalizedRLMExecutor(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "llm", "lmstudio":
		return "llm"
	default:
		return "inspect"
	}
}

func chooseRLMRunner(
	executor string,
	adapter *rlmenv.ReadOnlyAdapter,
	task rlm.Task,
	env rlm.Environment,
	llmProvider, llmModel, llmBaseURL, llmAPIKey string,
	llmTimeout time.Duration,
	requireToolUse bool,
	routeProfile string,
	planMode string,
) rlm.Runner {
	switch strings.ToLower(strings.TrimSpace(executor)) {
	case "llm", "lmstudio":
		pm := rlm.NormalizePlanMode(planMode)
		return rlm.LLMRunner{
			Tools: adapter,
			Config: rlm.LLMConfig{
				Provider:       firstNonEmpty(llmProvider, os.Getenv("FOXCTL_RLM_LLM_PROVIDER"), "lmstudio"),
				Model:          firstNonEmpty(llmModel, os.Getenv("FOXCTL_RLM_LLM_MODEL"), os.Getenv("LMSTUDIO_MODEL")),
				BaseURL:        firstNonEmpty(llmBaseURL, os.Getenv("FOXCTL_RLM_LLM_BASE_URL"), os.Getenv("LMSTUDIO_BASE_URL")),
				APIKey:         firstNonEmpty(llmAPIKey, os.Getenv("FOXCTL_RLM_LLM_API_KEY"), os.Getenv("LMSTUDIO_API_KEY")),
				Timeout:        llmTimeout,
				MaxIterations:  task.MaxIterations,
				RequireToolUse: requireToolUse,
				RouteProfile:   rlm.NormalizeRouteProfile(routeProfile),
				PlanMode:       pm,
			},
		}
	default:
		return rlm.InspectRunner{Tools: adapter}
	}
}

func applyRLMScoutRole(task rlm.Task, env rlm.Environment) (rlm.Task, rlm.Environment) {
	role := rlmenv.NormalizeScoutRole(task.Role)
	if role == "" {
		return task, env
	}
	task.Role = role
	task.Prompt = rlmenv.DecoratePromptForScoutRole(role, task.Prompt)
	env.Tools = rlmenv.FilterToolsForScoutRole(env.Tools, role)
	return task, env
}

func init() {
	rootCmd.AddCommand(newRLMCommand())
}

func openRLMCompanionDB(ctx context.Context, cfg config.Config) (*sql.DB, func() error, error) {
	root := strings.TrimSpace(cfg.Storage.Root)
	if root == "" {
		return nil, nil, nil
	}
	dbPath := filepath.Join(root, "companion.db")
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "COMPANION", filepath.Base(dbPath), companion.MigrateSchema)
	if err != nil {
		return nil, nil, err
	}
	return db, closeFn, nil
}
