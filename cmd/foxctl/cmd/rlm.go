package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/rlm"
	rlmenv "github.com/joshka0/foxctl/internal/rlm/env"
	"github.com/joshka0/foxctl/internal/rlm/repl"
	rlmruntime "github.com/joshka0/foxctl/internal/rlm/runtime"
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
	var maxChildren int
	var maxConcurrent int
	var maxTotalNodes int
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
	var sandboxKind string
	var ephemeralSkills bool
	var extractSolution bool
	var optTraceOut string
	var trajectoryOut string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Bootstrap and execute the experimental read-only RLM runtime",
		RunE: func(cmd *cobra.Command, _ []string) error {
			inputPrompt := strings.TrimSpace(prompt)
			traceOutPath, err := resolveRLMTraceOutputPath(optTraceOut, trajectoryOut)
			if err != nil {
				return err
			}
			cfg, err := loadConfig(cmd.Context())
			if err != nil {
				return err
			}
			task := rlm.Task{
				Prompt:        inputPrompt,
				WorkspaceID:   "",
				WorkspaceRoot: resolveContextWorkspace(workspacePath),
				MaxDepth:      maxDepth,
				MaxIterations: maxIterations,
				MaxSubcalls:   maxSubcalls,
				MaxChildren:   maxChildren,
				MaxConcurrent: maxConcurrent,
				MaxTotalNodes: maxTotalNodes,
			}
			companionDB, companionClose, err := openRLMCompanionDB(cmd.Context(), cfg)
			if err != nil {
				return err
			}
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
			defer func() { _ = bootstrapper.Close() }()
			ceStore := bootstrapper.ContextEngineStore()
			taskStore := bootstrapper.TaskStore()
			spec, err := rlm.ResolveRunSpec(rlm.ResolveRunSpecInput{
				Prompt:               task.Prompt,
				RequestedRoute:       rlm.RouteProfile(routeProfile),
				RequestedPlanMode:    rlm.PlanMode(planMode),
				RequestedToolProfile: toolProfile,
				AvailableTools:       env.Tools,
			})
			if err != nil {
				return err
			}
			env.Tools = append([]rlm.Tool(nil), spec.ToolPolicy.Tools...)
			traceTask, traceEnv := applyRLMScoutRole(task, env)
			var runRecursive func(context.Context, rlm.Task, rlm.Environment) (rlm.Result, error)
			runRecursive = func(ctx context.Context, currentTask rlm.Task, currentEnv rlm.Environment) (rlm.Result, error) {
				currentTask, currentEnv = applyRLMScoutRole(currentTask, currentEnv)
				currentAdapter := rlmenv.NewReadOnlyAdapter(cfg, currentTask.WorkspaceRoot, strings.TrimSpace(vaultPath), companionDB, currentEnv)
				currentAdapter.SetSubcall(runRecursive)
				currentAdapter.SetContextEngineStore(ceStore)
				currentAdapter.SetTaskStore(taskStore)
				runner := chooseRLMRunner(executor, currentAdapter, currentTask, currentEnv, llmProvider, llmModel, llmBaseURL, llmAPIKey, llmTimeout, requireToolUse, routeProfile, planMode, toolProfile, sandboxKind, ephemeralSkills, extractSolution)
				return runner.Run(ctx, currentTask, currentEnv)
			}
			mode := normalizedRLMExecutor(executor)
			result, runErr := runRecursive(cmd.Context(), task, env)
			if traceOutPath != "" {
				traceRecord := buildRLMOptimizerTraceRecord(rlmOptimizerTraceInput{
					Executor:        strings.TrimSpace(executor),
					Mode:            mode,
					ToolProfile:     string(spec.ToolPolicy.Profile),
					RouteProfile:    string(spec.RouteProfile),
					PlanMode:        string(spec.PlanMode),
					SandboxKind:     strings.TrimSpace(sandboxKind),
					EphemeralSkills: ephemeralSkills,
					ExtractSolution: extractSolution,
					LLMProvider:     strings.TrimSpace(llmProvider),
					LLMModel:        strings.TrimSpace(llmModel),
					LLMBaseURL:      strings.TrimSpace(llmBaseURL),
					InputPrompt:     inputPrompt,
					Task:            traceTask,
					Environment:     traceEnv,
					Result:          result,
					RunErr:          runErr,
				})
				if err := appendRLMOptimizerTraceRecord(traceOutPath, traceRecord); err != nil {
					return err
				}
			}
			if runErr != nil {
				return runErr
			}
			responseData := map[string]any{
				"task":        task,
				"environment": env,
				"result":      result,
				"mode":        mode,
				"run_spec":    spec,
			}
			if traceOutPath != "" {
				responseData["optimizer_trace"] = map[string]any{
					"path":   traceOutPath,
					"format": "jsonl",
				}
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("rlm/run", responseData, envelope.WithMeta(envelope.Meta{Source: "cli", Workspace: task.WorkspaceRoot})))
		},
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt for the RLM runtime")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: current workspace)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Optional vault path for knowledge-plane bootstrap")
	cmd.Flags().StringVar(&executor, "executor", "inspect", "Executor mode: inspect, llm, or repl")
	cmd.Flags().StringVar(&llmProvider, "llm-provider", "", "LLM provider override (e.g. lmstudio, openai, openrouter)")
	cmd.Flags().StringVar(&llmModel, "llm-model", "", "LLM model override")
	cmd.Flags().StringVar(&llmBaseURL, "llm-base-url", "", "LLM base URL override")
	cmd.Flags().StringVar(&llmAPIKey, "llm-api-key", "", "LLM API key override")
	cmd.Flags().DurationVar(&llmTimeout, "llm-timeout", 0, "LLM timeout override")
	cmd.Flags().BoolVar(&requireToolUse, "require-tool-use", true, "Require the LLM executor to make at least one tool call before answering")
	cmd.Flags().StringVar(&toolProfile, "tool-profile", rlmenv.ToolProfileDefault, "Experimental RLM tool profile")
	cmd.Flags().StringVar(&routeProfile, "route-profile", string(rlm.RouteProfileAuto), "Experimental RLM route profile: auto, code_retrieval, memory_recall, mixed, or evidence_audit")
	cmd.Flags().StringVar(&planMode, "plan-mode", string(rlm.PlanModeFree), "Experimental planning mode: free, staged, or lambda-retrieval")
	cmd.Flags().StringVar(&sandboxKind, "sandbox", string(rlmruntime.SandboxKindPython), "Scratch REPL sandbox for --executor repl: python, smolvm, or yaegi")
	cmd.Flags().BoolVar(&ephemeralSkills, "ephemeral-skills", false, "Enable runtime-managed ephemeral helper solve support for --executor repl")
	cmd.Flags().BoolVar(&extractSolution, "extract-solution", false, "For --executor repl, return only the final solution = ... line when present")
	cmd.Flags().StringVar(&optTraceOut, "opt-trace-out", "", "Append one optimizer-ready JSONL trace record per run")
	cmd.Flags().StringVar(&trajectoryOut, "trajectory-out", "", "Alias for --opt-trace-out (append optimizer-ready JSONL trace records)")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 1, "Maximum recursion depth")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 8, "Maximum root iterations")
	cmd.Flags().IntVar(&maxSubcalls, "max-subcalls", 8, "Maximum subcalls")
	cmd.Flags().IntVar(&maxChildren, "max-children", 0, "Maximum child nodes for async recursive RLM runs (0 derives from --max-subcalls)")
	cmd.Flags().IntVar(&maxConcurrent, "max-concurrent", 0, "Maximum concurrently running child nodes for async recursive RLM runs (0 uses runtime default)")
	cmd.Flags().IntVar(&maxTotalNodes, "max-total-nodes", 0, "Maximum total async recursive RLM nodes (0 derives from --max-children)")
	_ = cmd.MarkFlagRequired("prompt")
	return cmd
}

func normalizedRLMExecutor(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "llm", "lmstudio":
		return "llm"
	case "repl", "rlm-repl":
		return "repl"
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
	toolProfile string,
	sandboxKind string,
	ephemeralSkills bool,
	extractSolution bool,
) rlm.Runner {
	switch strings.ToLower(strings.TrimSpace(executor)) {
	case "llm", "lmstudio":
		pm := rlm.NormalizePlanMode(planMode)
		if pm == rlm.PlanModeLambda {
			llmCfg := rlm.LLMConfig{
				Provider:    firstNonEmpty(llmProvider, os.Getenv("FOXCTL_RLM_LLM_PROVIDER"), "lmstudio"),
				Model:       firstNonEmpty(llmModel, os.Getenv("FOXCTL_RLM_LLM_MODEL"), os.Getenv("LMSTUDIO_MODEL")),
				BaseURL:     firstNonEmpty(llmBaseURL, os.Getenv("FOXCTL_RLM_LLM_BASE_URL"), os.Getenv("LMSTUDIO_BASE_URL")),
				APIKey:      firstNonEmpty(llmAPIKey, os.Getenv("FOXCTL_RLM_LLM_API_KEY"), os.Getenv("LMSTUDIO_API_KEY")),
				Timeout:     llmTimeout,
				ToolProfile: toolProfile,
			}
			return rlm.LambdaRunner{
				Tools: adapter,
				Config: rlm.LambdaConfig{
					LLM:                 llmCfg,
					EphemeralSkills:     ephemeralSkills,
					ExtractSolutionLine: extractSolution,
				},
			}
		}
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
				ToolProfile:    toolProfile,
			},
		}
	case "repl", "rlm-repl":
		timeout := llmTimeout
		if timeout <= 0 {
			timeout = 90 * time.Second
		}
		kind := rlmruntime.NormalizeSandboxKind(rlmruntime.SandboxKind(sandboxKind))
		recursionEnabled := task.MaxDepth > 0 && task.MaxSubcalls > 0
		helperFactory := &rlmruntime.HelperFactoryConfig{
			Attempts:            3,
			ExtractSolutionLine: extractSolution,
		}
		return &rlmruntime.REPLRunner{
			Config: rlmruntime.REPLRunnerConfig{
				LLM: rlm.LLMConfig{
					Provider:       firstNonEmpty(llmProvider, os.Getenv("FOXCTL_RLM_LLM_PROVIDER"), "lmstudio"),
					Model:          firstNonEmpty(llmModel, os.Getenv("FOXCTL_RLM_LLM_MODEL"), os.Getenv("LMSTUDIO_MODEL")),
					BaseURL:        firstNonEmpty(llmBaseURL, os.Getenv("FOXCTL_RLM_LLM_BASE_URL"), os.Getenv("LMSTUDIO_BASE_URL")),
					APIKey:         firstNonEmpty(llmAPIKey, os.Getenv("FOXCTL_RLM_LLM_API_KEY"), os.Getenv("LMSTUDIO_API_KEY")),
					Timeout:        timeout,
					MaxIterations:  task.MaxIterations,
					RequireToolUse: requireToolUse,
				},
				Budget: rlmruntime.BudgetConfig{
					MaxDepth:       task.MaxDepth,
					MaxIterations:  task.MaxIterations,
					MaxSubcalls:    task.MaxSubcalls,
					MaxChildren:    task.MaxChildren,
					MaxConcurrent:  task.MaxConcurrent,
					MaxTotalNodes:  task.MaxTotalNodes,
					MaxREPLCalls:   task.MaxIterations,
					MaxHelperCalls: task.MaxIterations,
					MaxDuration:    timeout,
				},
				Sandbox: rlmruntime.SandboxConfig{
					Kind: kind,
					SmolVMPython: repl.SmolVMPythonOptions{
						MachineName:         "foxctl-rlm-longcot-glibc-offline",
						Image:               "python:3.12-slim",
						GuestWorkDir:        "/workspace/foxctl-rlm-python",
						Network:             false,
						CreateOnInit:        false,
						StartOnInit:         true,
						StopOnClose:         false,
						AllowPackageInstall: true,
						AllowedPackages:     []string{"python-chess", "sympy", "networkx", "numpy", "rdkit", "rdkit-pypi", "requests"},
						PackageAliases: map[string]string{
							"chess":    "python-chess",
							"sympy":    "sympy",
							"networkx": "networkx",
							"numpy":    "numpy",
							"rdkit":    "rdkit-pypi",
							"requests": "requests",
						},
						PackageInstallTimeout: 180 * time.Second,
					},
				},
				SystemPrompt:    buildRLMREPLCLISystemPromptForPolicy(kind, true, recursionEnabled),
				AsyncRecursion:  true,
				RecursionPolicy: rlmruntime.RecursionPolicyOptional,
				RLMQueryFactory: newRLMREPLQueryFactory(adapter),
				InitialState: map[string]any{
					"official_prompt": task.Prompt,
				},
				EphemeralSkills:     false,
				HelperFactory:       helperFactory,
				ExtractSolutionLine: extractSolution,
			},
		}
	default:
		return rlm.InspectRunner{Tools: adapter}
	}
}

func buildRLMREPLCLISystemPromptForPolicy(kind rlmruntime.SandboxKind, helperEnabled bool, recursionEnabled bool) string {
	var b strings.Builder
	b.WriteString(rlmruntime.BuildREPLSystemPrompt())
	b.WriteString("\n\nRuntime protocol for this run:\n")
	b.WriteString("- Compute locally in the scratch REPL first.\n")
	if helperEnabled {
		b.WriteString("- Use ephemeral_helper_solve when a short-lived helper improves parsing, simulation, or verification.\n")
		b.WriteString("- Treat ephemeral_helper_solve as a helper shortcut only; it is not recursive execution.\n")
		b.WriteString("- If helper output returns ok=true, synthesize the final answer from that output.\n")
	}
	if recursionEnabled {
		b.WriteString("- For child decomposition, call rlm_query, then call rlm_wait({}) before finalizing.\n")
		b.WriteString("- Use rlm_result only to re-read one child result when needed.\n")
		b.WriteString("- Follow this order: compute locally, use helper when useful, query child, wait, then synthesize.\n")
	} else {
		b.WriteString("- Child recursion tools are not enabled in this run; solve directly with the scratch REPL and helper shortcut.\n")
	}
	if rlmruntime.NormalizeSandboxKind(kind) == rlmruntime.SandboxKindYaegi {
		b.WriteString("\nThe scratch REPL is Go, but helper and recursion tools are separate model tools, not Go functions inside go_repl.\n")
	}
	return b.String()
}

func newRLMREPLQueryFactory(adapter *rlmenv.ReadOnlyAdapter) func(parentTask rlm.Task, env rlm.Environment) rlmruntime.RLMQueryRunFunc {
	if adapter == nil {
		return nil
	}
	return func(_ rlm.Task, _ rlm.Environment) rlmruntime.RLMQueryRunFunc {
		return func(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
			args, err := json.Marshal(map[string]any{
				"prompt":           task.Prompt,
				"role":             task.Role,
				"repo_handles":     env.RepoHandles,
				"vault_handles":    env.VaultHandles,
				"scene_handles":    env.SceneHandles,
				"artifact_handles": env.ArtifactHandles,
				"max_depth":        task.MaxDepth,
				"max_iterations":   task.MaxIterations,
				"max_subcalls":     task.MaxSubcalls,
			})
			if err != nil {
				return rlm.Result{}, err
			}
			out, err := adapter.ExecuteInternal(ctx, "subcall", args)
			if err != nil {
				return rlm.Result{}, err
			}
			rawResult, ok := out["result"]
			if !ok {
				return rlm.Result{}, fmt.Errorf("subcall returned no result payload")
			}
			body, err := json.Marshal(rawResult)
			if err != nil {
				return rlm.Result{}, err
			}
			var result rlm.Result
			if err := json.Unmarshal(body, &result); err != nil {
				return rlm.Result{}, err
			}
			return result, nil
		}
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
