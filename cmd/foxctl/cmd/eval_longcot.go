package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	agentruntime "github.com/joshka0/foxctl/internal/agent/runtime"
	agenttypes "github.com/joshka0/foxctl/internal/agent/types"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/rlm"
	rlmenv "github.com/joshka0/foxctl/internal/rlm/env"
	"github.com/joshka0/foxctl/internal/rlm/repl"
	rlmruntime "github.com/joshka0/foxctl/internal/rlm/runtime"
	ctxengstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/joshka0/foxctl/internal/tooling/evals/longcotbridge"
	"github.com/joshka0/foxctl/internal/tooling/evals/longcoteval"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

const evalLongCoTCommand = "eval.longcot"

var longCoTConditionTemplates = map[longcoteval.ConditionID]longcoteval.Condition{
	longcoteval.ConditionBaselineNoToolsOfficial: {
		ID:                    longcoteval.ConditionBaselineNoToolsOfficial,
		Kind:                  longcoteval.ConditionKindBaseline,
		PromptTemplateVersion: "official_longcot_v1",
	},
	longcoteval.ConditionRLMNoToolsSingle: {
		ID:              longcoteval.ConditionRLMNoToolsSingle,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_repl_recursive",
		RLMPlanMode:     "repl_recursive",
		RLMToolProfile:  "longcot-repl-recursive",
		MaxDepth:        1,
		MaxIterations:   32,
		MaxSubcalls:     4,
	},
	longcoteval.ConditionRLMReplNoSubcalls: {
		ID:              longcoteval.ConditionRLMReplNoSubcalls,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_repl_no_subcalls",
		RLMPlanMode:     "repl_no_subcalls",
		RLMToolProfile:  "longcot-repl",
		MaxDepth:        1,
		MaxIterations:   32,
		MaxSubcalls:     0,
	},
	longcoteval.ConditionRLMReplRecursive: {
		ID:              longcoteval.ConditionRLMReplRecursive,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_repl_recursive",
		RLMPlanMode:     "repl_recursive",
		RLMToolProfile:  "longcot-repl-recursive",
		MaxDepth:        2,
		MaxIterations:   32,
		MaxSubcalls:     4,
	},
	longcoteval.ConditionRLMLambdaReplSingle: {
		ID:              longcoteval.ConditionRLMLambdaReplSingle,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_lambda_repl",
		RLMPlanMode:     "repl_lambda",
		RLMToolProfile:  "longcot-repl-recursive",
		MaxDepth:        2,
		MaxIterations:   32,
		MaxSubcalls:     4,
	},
	longcoteval.ConditionRLMLambdaAdaptiveSingle: {
		ID:              longcoteval.ConditionRLMLambdaAdaptiveSingle,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_lambda_adaptive",
		RLMPlanMode:     "repl_lambda_adaptive",
		RLMToolProfile:  "longcot-repl-recursive",
		MaxDepth:        2,
		MaxIterations:   24,
		MaxSubcalls:     4,
	},
	longcoteval.ConditionRLMLambdaThenBraidSingle: {
		ID:              longcoteval.ConditionRLMLambdaThenBraidSingle,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_lambda_then_braid",
		RLMPlanMode:     "repl_lambda_then_braid",
		RLMToolProfile:  "longcot-repl-recursive",
		MaxDepth:        2,
		MaxIterations:   32,
		MaxSubcalls:     16,
	},
	longcoteval.ConditionRLMBraidSingle: {
		ID:              longcoteval.ConditionRLMBraidSingle,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_repl_braid",
		RLMPlanMode:     "repl_braid",
		RLMToolProfile:  "longcot-repl-recursive",
		MaxDepth:        1,
		MaxIterations:   32,
		MaxSubcalls:     16,
	},
	longcoteval.ConditionRLMNoToolsStaged: {
		ID:              longcoteval.ConditionRLMNoToolsStaged,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_no_tools_staged",
		RLMPlanMode:     "staged",
		RLMToolProfile:  rlmenv.ToolProfileLongCoTNoModelTools,
		MaxDepth:        1,
		MaxIterations:   2,
		MaxSubcalls:     0,
	},
	longcoteval.ConditionRLMNoModelToolsSingle: {
		ID:              longcoteval.ConditionRLMNoModelToolsSingle,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_repl_no_subcalls",
		RLMPlanMode:     "repl_no_subcalls",
		RLMToolProfile:  "longcot-repl",
		MaxDepth:        1,
		MaxIterations:   32,
		MaxSubcalls:     0,
	},
	longcoteval.ConditionRLMNoModelToolsStaged: {
		ID:              longcoteval.ConditionRLMNoModelToolsStaged,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_no_model_tools_staged",
		RLMPlanMode:     "staged",
		RLMToolProfile:  rlmenv.ToolProfileLongCoTNoModelTools,
		MaxIterations:   2,
		MaxSubcalls:     0,
	},
}

type longCoTQuestionFilter struct {
	Split      string
	Domains    []string
	Difficulty string
	Limit      int
	Seed       int64
}

type longCoTQuestionRow struct {
	ID                    string                            `json:"id,omitempty"` // legacy fallback for local fixtures
	QuestionID            string                            `json:"question_id"`
	Split                 string                            `json:"split,omitempty"` // legacy fixture field
	Domain                string                            `json:"domain"`
	Difficulty            string                            `json:"difficulty"`
	Template              string                            `json:"template"`
	Prompt                string                            `json:"prompt"`
	Answer                json.RawMessage                   `json:"answer"`
	Canary                string                            `json:"canary"`
	Question              string                            `json:"question,omitempty"` // backward-compatible fallback
	AllowOptionalSubcalls bool                              `json:"allow_optional_subcalls,omitempty"`
	RLMReview             bool                              `json:"rlm_review,omitempty"`
	RLMReviewRecursive    bool                              `json:"rlm_review_recursive,omitempty"`
	RequiredSubcallRules  []longcoteval.RequiredSubcallRule `json:"required_subcall_rules,omitempty"`
}

type longCoTLiveTarget struct {
	Provider   string
	Model      string
	BaseURL    string
	APIKey     string
	AuthMode   string
	AuthHeader string
	AuthPrefix string
}

type longCoTHelperRuntime struct {
	Target    longCoTLiveTarget
	Attempts  int
	Timeout   time.Duration
	MaxTokens int
	Language  string
}

type longCoTLiveRunner string

const (
	longCoTLiveRunnerAgent          longCoTLiveRunner         = "agent-runtime"
	longCoTLiveRunnerRLM            longCoTLiveRunner         = "rlm-llm"
	longCoTLiveRunnerREPL           longCoTLiveRunner         = "rlm-repl"
	longCoTAttemptStatusUnsupported longcoteval.AttemptStatus = "unsupported"
)

var errLongCoTLiveConditionUnsupported = errors.New("longcot live condition unsupported")

func newEvalLongCoTCommand() *cobra.Command {
	var (
		dryRun                        bool
		datasetPath                   string
		split                         string
		domains                       []string
		difficulty                    string
		limit                         int
		seed                          int64
		conditionFlags                []string
		provider                      string
		model                         string
		baseURL                       string
		apiKey                        string
		helperProvider                string
		helperModel                   string
		helperBaseURL                 string
		helperAPIKey                  string
		helperAttempts                int
		helperTimeout                 time.Duration
		helperMaxTokens               int
		helperLanguage                string
		timeout                       time.Duration
		maxTokens                     int
		temperature                   float64
		maxIterations                 int
		agentRole                     string
		outputDir                     string
		save                          bool
		format                        string
		verify                        bool
		reviewMode                    string
		reviewIter                    int
		reviewRecursive               bool
		reviewMaxDepth                int
		reviewMaxSubcalls             int
		reviewCandidateMaxChars       int
		reviewChildSummaryMaxChars    int
		reviewChildSummaryRewrite     bool
		reviewChildSummaryRewriteIter int
		noThink                       bool
		longCoTRepo                   string
		longCoTPython                 string
		sandboxKind                   string
		ephemeralSkills               bool
		generalHelper                 bool
		requireEphemeralSkills        bool
		blocksworldHelper             bool
		finalFromVerifiedHandoff      bool
		noFallback                    bool
	)

	cmd := &cobra.Command{
		Use:   "longcot",
		Short: "Plan LongCoT evaluation runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			if err := validateLongCoTRunNumericFlags(limit, maxTokens, timeout, maxIterations, helperAttempts, helperTimeout, helperMaxTokens); err != nil {
				return writeOptimizeError(out, evalLongCoTCommand, err.Error())
			}
			normalizedHelperLanguage, err := normalizeLongCoTHelperLanguage(helperLanguage)
			if err != nil {
				return writeOptimizeError(out, evalLongCoTCommand, err.Error())
			}
			helperLanguage = normalizedHelperLanguage
			sandbox, err := normalizeLongCoTSandboxKind(sandboxKind)
			if err != nil {
				return writeOptimizeError(out, evalLongCoTCommand, err.Error())
			}
			ephemeralSkills, generalHelper = normalizeLongCoTHelperFlags(ephemeralSkills, generalHelper, requireEphemeralSkills)

			datasetPath = strings.TrimSpace(datasetPath)
			datasetLabel := ""
			datasetSource := "dataset"

			normalizedFormat, normalizedReviewMode, reviewCfg, err := normalizeLongCoTReviewInputs(
				format,
				reviewMode,
				reviewIter,
				reviewRecursive,
				reviewMaxDepth,
				reviewMaxSubcalls,
				reviewCandidateMaxChars,
				reviewChildSummaryMaxChars,
				reviewChildSummaryRewrite,
				reviewChildSummaryRewriteIter,
			)
			if err != nil {
				return writeOptimizeError(out, evalLongCoTCommand, err.Error())
			}
			format = normalizedFormat
			reviewMode = normalizedReviewMode

			filter, err := normalizeLongCoTQuestionFilter(split, domains, difficulty, limit, seed)
			if err != nil {
				return writeOptimizeError(out, evalLongCoTCommand, err.Error())
			}

			var questions []longcoteval.Question
			if datasetPath == "" && strings.TrimSpace(longCoTRepo) != "" {
				datasetLabel = "official-longcot://" + strings.TrimSpace(longCoTRepo)
				datasetSource = "official_python_loader"
				loader := longcotbridge.NewPythonQuestionLoader(longcotbridge.PythonLoaderConfig{
					RepoPath:  strings.TrimSpace(longCoTRepo),
					PythonBin: strings.TrimSpace(longCoTPython),
				})
				questions, err = loader.LoadQuestions(ctx, longcoteval.LoadRequest{
					Domains: filter.Domains,
				})
				if err != nil {
					return writeOptimizeError(out, evalLongCoTCommand, fmt.Sprintf("load official LongCoT questions: %v", err))
				}
				questions, err = applyLongCoTQuestionFilter(questions, filter)
				if err != nil {
					return writeOptimizeError(out, evalLongCoTCommand, fmt.Sprintf("filter official LongCoT questions: %v", err))
				}
				if len(questions) == 0 {
					return writeOptimizeError(out, evalLongCoTCommand, "official LongCoT loader produced zero questions after filters")
				}
			} else if dryRun && datasetPath == "" {
				if verify {
					return writeOptimizeError(out, evalLongCoTCommand, "--verify requires --dataset when --dry-run uses offline fixture data")
				}
				datasetLabel = "official-fixture://longcot-mini-dry-run"
				datasetSource = "offline_fixture"
				questions = longCoTDryRunOfficialFixtureQuestions()
				questions, err = applyLongCoTQuestionFilter(questions, filter)
				if err != nil {
					return writeOptimizeError(out, evalLongCoTCommand, fmt.Sprintf("filter offline fixture questions: %v", err))
				}
				if len(questions) == 0 {
					return writeOptimizeError(out, evalLongCoTCommand, "offline fixture produced zero questions after filters")
				}
			} else {
				if !dryRun && datasetPath == "" {
					return writeOptimizeError(out, evalLongCoTCommand, "--dataset/--longcot-dataset or --longcot-repo is required when --dry-run=false")
				}
				absDatasetPath, err := filepath.Abs(datasetPath)
				if err != nil {
					return writeOptimizeError(out, evalLongCoTCommand, fmt.Sprintf("resolve dataset path: %v", err))
				}
				datasetLabel = absDatasetPath
				questions, err = loadLongCoTQuestions(absDatasetPath, filter)
				if err != nil {
					return writeOptimizeError(out, evalLongCoTCommand, fmt.Sprintf("load longcot dataset: %v", err))
				}
				if len(questions) == 0 {
					return writeOptimizeError(out, evalLongCoTCommand, "dataset produced zero questions after filters")
				}
			}
			if noThink {
				questions = prefixLongCoTQuestionPrompts(questions, "/no_think\n")
			}

			runtime := longCoTConditionRuntime{
				MaxTokens:              maxTokens,
				Timeout:                timeout,
				Temperature:            temperature,
				Seed:                   seed,
				MaxIter:                maxIterations,
				SandboxKind:            sandbox,
				EphemeralSkills:        ephemeralSkills,
				GeneralHelper:          generalHelper,
				RequireEphemeralSkills: requireEphemeralSkills,
				BlocksWorldHelper:      blocksworldHelper,
			}
			conditions, err := resolveLongCoTConditions(conditionFlags, runtime)
			if err != nil {
				return writeOptimizeError(out, evalLongCoTCommand, err.Error())
			}

			workspaceRoot := resolveContextWorkspace("")
			activeProvider := strings.TrimSpace(provider)
			activeModel := strings.TrimSpace(model)
			activeBaseURL := strings.TrimSpace(baseURL)
			activeAPIKey := strings.TrimSpace(apiKey)
			activeHelperProvider := strings.TrimSpace(helperProvider)
			activeHelperModel := strings.TrimSpace(helperModel)
			activeHelperBaseURL := strings.TrimSpace(helperBaseURL)
			activeHelperAPIKey := strings.TrimSpace(helperAPIKey)
			runMode := "dry-run"

			var (
				target        longCoTLiveTarget
				helperTarget  longCoTLiveTarget
				helperRuntime longCoTHelperRuntime
				cfg           config.Config
				attempts      []longcoteval.Attempt
			)

			if !dryRun {
				runMode = "live"
				cfg, err = loadConfig(ctx, config.WithWorkspacePath(workspaceRoot))
				if err != nil {
					return writeOptimizeError(out, evalLongCoTCommand, fmt.Sprintf("load config: %v", err))
				}
				target, err = resolveLongCoTLiveTarget(cfg, activeProvider, activeModel, activeBaseURL, activeAPIKey)
				if err != nil {
					return writeOptimizeError(out, evalLongCoTCommand, err.Error())
				}
				activeProvider = target.Provider
				activeModel = target.Model
				activeBaseURL = target.BaseURL
				activeAPIKey = target.APIKey
				helperTarget = target
				if activeHelperProvider != "" || activeHelperModel != "" || activeHelperBaseURL != "" || activeHelperAPIKey != "" {
					helperProviderForResolve := firstNonEmpty(activeHelperProvider, activeProvider)
					helperModelForResolve := firstNonEmpty(activeHelperModel, activeModel)
					helperBaseURLForResolve := activeHelperBaseURL
					helperAPIKeyForResolve := activeHelperAPIKey
					if strings.EqualFold(helperProviderForResolve, activeProvider) {
						helperBaseURLForResolve = firstNonEmpty(helperBaseURLForResolve, activeBaseURL)
						helperAPIKeyForResolve = firstNonEmpty(helperAPIKeyForResolve, activeAPIKey)
					}
					helperTarget, err = resolveLongCoTLiveTarget(
						cfg,
						helperProviderForResolve,
						helperModelForResolve,
						helperBaseURLForResolve,
						helperAPIKeyForResolve,
					)
					if err != nil {
						return writeOptimizeError(out, evalLongCoTCommand, fmt.Sprintf("resolve helper target: %v", err))
					}
				}
				activeHelperProvider = helperTarget.Provider
				activeHelperModel = helperTarget.Model
				activeHelperBaseURL = helperTarget.BaseURL
				activeHelperAPIKey = helperTarget.APIKey
				helperRuntime = longCoTHelperRuntime{
					Target:    helperTarget,
					Attempts:  helperAttempts,
					Timeout:   helperTimeout,
					MaxTokens: firstPositiveInt(helperMaxTokens, longCoTDefaultHelperMaxTokens(maxTokens)),
					Language:  helperLanguage,
				}
			}

			runID := buildLongCoTRunID(runMode, datasetLabel, filter, conditions, questions, activeProvider, activeModel, activeBaseURL, maxTokens, temperature, timeout)
			if dryRun {
				attempts = planLongCoTDryRunAttempts(runID, questions, conditions, activeProvider, activeModel)
			} else {
				attempts, err = runLongCoTLiveAttempts(ctx, cfg, workspaceRoot, runID, questions, conditions, target, helperRuntime, strings.TrimSpace(agentRole), sandbox, reviewCfg, ephemeralSkills, generalHelper, requireEphemeralSkills, blocksworldHelper, finalFromVerifiedHandoff)
				if err != nil {
					return writeOptimizeError(out, evalLongCoTCommand, fmt.Sprintf("run live attempts: %v", err))
				}
			}
			var verifyResult *longcoteval.VerifyResult
			if verify {
				if datasetSource == "dataset" {
					verifiedAttempts, result := verifyLongCoTAttemptsAgainstDatasetAnswers(attempts, questions)
					attempts = verifiedAttempts
					verifyResult = &result
				} else {
					verifier := longcotbridge.NewPythonVerifier(longcotbridge.PythonVerifierConfig{
						RepoPath:   strings.TrimSpace(longCoTRepo),
						PythonBin:  strings.TrimSpace(longCoTPython),
						NoFallback: noFallback,
					})
					verifiedAttempts, result, err := verifyLongCoTAttempts(ctx, attempts, questions, verifier)
					if err != nil {
						return writeOptimizeError(out, evalLongCoTCommand, err.Error())
					}
					attempts = verifiedAttempts
					verifyResult = &result
				}
			}

			comparisons := longCoTComparisonsForConditions(conditions)
			resolvedOutputDir := resolvedLongCoTOutputDir(outputDir)

			runConfig := map[string]any{
				"effective_contract_version":         "longcot_eval_v1",
				"dry_run":                            dryRun,
				"mode":                               runMode,
				"dataset":                            datasetLabel,
				"dataset_source":                     datasetSource,
				"split":                              filter.Split,
				"domains":                            filter.Domains,
				"difficulty":                         filter.Difficulty,
				"limit":                              filter.Limit,
				"seed":                               filter.Seed,
				"conditions":                         conditions,
				"comparisons":                        comparisons,
				"provider":                           activeProvider,
				"model":                              activeModel,
				"base_url":                           activeBaseURL,
				"api_key_set":                        strings.TrimSpace(activeAPIKey) != "",
				"helper_provider":                    activeHelperProvider,
				"helper_model":                       activeHelperModel,
				"helper_base_url":                    activeHelperBaseURL,
				"helper_api_key_set":                 strings.TrimSpace(activeHelperAPIKey) != "",
				"helper_attempts":                    helperAttempts,
				"helper_timeout_ms":                  helperTimeout.Milliseconds(),
				"helper_max_tokens":                  helperMaxTokens,
				"helper_language":                    helperLanguage,
				"timeout_ms":                         timeout.Milliseconds(),
				"max_tokens":                         maxTokens,
				"max_iterations":                     maxIterations,
				"temperature":                        temperature,
				"agent_role":                         strings.TrimSpace(agentRole),
				"output_dir":                         resolvedOutputDir,
				"save":                               save,
				"format":                             format,
				"verify":                             verify,
				"rlm_review":                         reviewCfg.Mode,
				"rlm_review_iterations":              reviewCfg.Iterations,
				"rlm_review_recursive":               reviewCfg.Recursive,
				"rlm_review_max_depth":               reviewCfg.MaxDepth,
				"rlm_review_max_subcalls":            reviewCfg.MaxSubcalls,
				"rlm_review_candidate_max_chars":     reviewCfg.CandidateMaxChars,
				"rlm_review_child_summary_max_chars": reviewCfg.ChildSummaryMaxChars,
				"rlm_review_child_summary_rewrite":   reviewCfg.ChildSummaryRewrite,
				"rlm_review_child_summary_rewrite_iterations": reviewCfg.ChildSummaryRewriteIterations,
				"no_think":                        noThink,
				"sandbox":                         string(sandbox),
				"longcot_repo":                    strings.TrimSpace(longCoTRepo),
				"longcot_python":                  strings.TrimSpace(longCoTPython),
				"ephemeral_skills":                ephemeralSkills,
				"general_helper":                  generalHelper,
				"require_ephemeral_skills":        requireEphemeralSkills,
				"blocksworld_helper":              blocksworldHelper,
				"rlm_final_from_verified_handoff": finalFromVerifiedHandoff,
				"verify_no_fallback":              noFallback,
				"selected_question_ids":           extractLongCoTQuestionIDs(questions),
			}
			if verifyResult != nil {
				runConfig["verification"] = map[string]any{
					"verifier_name":    verifyResult.VerifierName,
					"verifier_version": verifyResult.VerifierVersion,
					"counts":           verifyResult.Counts,
				}
			}

			result := longcoteval.RunResult{
				RunID:       runID,
				GeneratedAt: time.Now().UTC(),
				Config:      runConfig,
				Questions:   questions,
				Attempts:    attempts,
			}
			result.Summary = longcoteval.Summarize(result.Attempts, comparisons)

			markdown := longcoteval.RenderMarkdown(result)

			if save {
				artifacts, err := saveLongCoTOutputs(resolvedOutputDir, result, markdown)
				if err != nil {
					return writeOptimizeError(out, evalLongCoTCommand, fmt.Sprintf("save outputs: %v", err))
				}
				result.Artifacts = artifacts
				result.Config["saved_artifacts"] = artifacts
			}

			return protocol.WriteOK(out, evalLongCoTCommand, map[string]any{
				"markdown": markdown,
				"result":   result,
				"config":   result.Config,
			}, protocol.WithSource("run"), protocol.WithWorkspace(resolveContextWorkspace("")))
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Plan the run without live model or verifier execution")
	cmd.Flags().StringVar(&datasetPath, "dataset", "", "Path to official-shaped LongCoT JSONL dataset (question_id/domain/difficulty/template/prompt/answer/canary)")
	cmd.Flags().StringVar(&datasetPath, "longcot-dataset", "", "Path to official-shaped LongCoT JSONL dataset")
	cmd.Flags().StringVar(&split, "split", "", "Optional split filter (exact match)")
	cmd.Flags().StringSliceVar(&domains, "domain", nil, "Optional domain filter (repeatable)")
	cmd.Flags().StringVar(&difficulty, "difficulty", "", "Optional difficulty selector: easy|medium|hard|longcot-mini|longcot")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum question count after filtering (0 = unlimited)")
	cmd.Flags().Int64Var(&seed, "seed", 0, "Deterministic shuffle seed for filtered questions")
	cmd.Flags().StringSliceVar(&conditionFlags, "condition", nil, "Condition ID(s) to run (repeatable, accepts comma-separated)")
	cmd.Flags().StringVar(&provider, "provider", "lmstudio", "Provider for live runs (default: lmstudio)")
	cmd.Flags().StringVar(&model, "model", "", "Model override for live runs")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL override for live runs")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key override for live runs")
	cmd.Flags().StringVar(&helperProvider, "helper-provider", "", "Provider override for --general-helper nested helper drafter (defaults to --provider)")
	cmd.Flags().StringVar(&helperModel, "helper-model", "", "Model override for --general-helper nested helper drafter (defaults to --model)")
	cmd.Flags().StringVar(&helperBaseURL, "helper-base-url", "", "OpenAI-compatible base URL override for --general-helper nested helper drafter (defaults to --base-url)")
	cmd.Flags().StringVar(&helperAPIKey, "helper-api-key", "", "API key override for --general-helper nested helper drafter (defaults to --api-key)")
	cmd.Flags().IntVar(&helperAttempts, "helper-attempts", 3, "Max draft/validate/run attempts for --general-helper nested helper drafter")
	cmd.Flags().DurationVar(&helperTimeout, "helper-timeout", 0, "Per-draft timeout for --general-helper nested helper drafter (0 uses --timeout)")
	cmd.Flags().IntVar(&helperMaxTokens, "helper-max-tokens", 0, "Max output tokens for --general-helper nested helper drafter (0 uses --max-tokens)")
	cmd.Flags().StringVar(&helperLanguage, "helper-language", rlmruntime.HelperLanguageGo, "Synthesized helper language for --general-helper: go or python (presets remain Go)")
	cmd.Flags().DurationVar(&timeout, "timeout", 90*time.Second, "Per-attempt timeout used in planned conditions")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 0, "Per-attempt max token cap used in planned conditions")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Per-attempt iteration cap override (0 uses condition defaults)")
	cmd.Flags().Float64Var(&temperature, "temperature", 0, "Per-attempt sampling temperature used in planned conditions")
	cmd.Flags().StringVar(&agentRole, "agent-role", string(agenttypes.RoleResearcher), "Agent role for baseline_no_tools_official_prompt live attempts")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Base output directory for saved artifacts")
	cmd.Flags().BoolVar(&save, "save", false, "Write run JSON and markdown report under output-dir")
	cmd.Flags().StringVar(&format, "format", "markdown", "Output format: markdown or json")
	cmd.Flags().BoolVar(&verify, "verify", false, "Run official LongCoT Python verification (run_eval.py) against generated responses")
	cmd.Flags().StringVar(&reviewMode, "rlm-review", "off", "Optional RLM answer review pass: off, auto, or always")
	cmd.Flags().IntVar(&reviewIter, "rlm-review-iterations", 3, "Max iterations for the optional RLM review pass")
	cmd.Flags().BoolVar(&reviewRecursive, "rlm-review-recursive", false, "Allow the optional RLM review pass to spawn bounded child critic subcalls")
	cmd.Flags().IntVar(&reviewMaxDepth, "rlm-review-max-depth", 2, "Max recursive depth for --rlm-review-recursive")
	cmd.Flags().IntVar(&reviewMaxSubcalls, "rlm-review-max-subcalls", 2, "Max child critic subcalls for --rlm-review-recursive")
	cmd.Flags().IntVar(&reviewCandidateMaxChars, "rlm-review-candidate-max-chars", 2000, "Max candidate-answer characters included in the RLM review prompt (0 = unlimited)")
	cmd.Flags().IntVar(&reviewChildSummaryMaxChars, "rlm-review-child-summary-max-chars", 900, "Max characters returned for child critic summaries in recursive review (0 = unlimited)")
	cmd.Flags().BoolVar(&reviewChildSummaryRewrite, "rlm-review-child-summary-rewrite", true, "Use a bounded child summarizer when recursive review child output exceeds the summary limit")
	cmd.Flags().IntVar(&reviewChildSummaryRewriteIter, "rlm-review-child-summary-rewrite-iterations", 2, "Max iterations for recursive review child summary rewrite")
	cmd.Flags().BoolVar(&noThink, "no-think", false, "Prefix official prompts with /no_think for local reasoning models that otherwise hide final content")
	cmd.Flags().StringVar(&longCoTRepo, "longcot-repo", "", "Path to local LongCoT repository checkout used for official verification")
	cmd.Flags().StringVar(&longCoTPython, "longcot-python", "", "Python executable for LongCoT verification (optional; defaults to uv/python3 discovery)")
	cmd.Flags().StringVar(&sandboxKind, "sandbox", string(rlmruntime.SandboxKindPython), "Scratch REPL sandbox for rlm_repl conditions: python, smolvm, or yaegi")
	cmd.Flags().BoolVar(&ephemeralSkills, "ephemeral-skills", false, "Expose ephemeral_helper_solve to rlm_repl conditions")
	cmd.Flags().BoolVar(&generalHelper, "general-helper", false, "Expose ephemeral_helper_solve, a runtime-managed short-lived Go helper factory for rlm_repl conditions")
	cmd.Flags().BoolVar(&requireEphemeralSkills, "require-ephemeral-skills", false, "Require rlm_repl conditions to call ephemeral_helper_solve before finalizing")
	cmd.Flags().BoolVar(&blocksworldHelper, "blocksworld-helper", true, "Expose the deterministic blocksworld_solve helper for BlocksWorld LongCoT questions")
	cmd.Flags().BoolVar(&finalFromVerifiedHandoff, "rlm-final-from-verified-handoff", false, "For braid RLM conditions, return a runtime-verified final handoff directly instead of asking the model to restate long final answers")
	cmd.Flags().BoolVar(&noFallback, "verify-no-fallback", false, "Disable LongCoT verifier fallback judges (--no-fallback)")

	return cmd
}

func validateLongCoTRunNumericFlags(
	limit int,
	maxTokens int,
	timeout time.Duration,
	maxIterations int,
	helperAttempts int,
	helperTimeout time.Duration,
	helperMaxTokens int,
) error {
	if err := validateLongCoTNonNegativeIntFlag("--limit", limit); err != nil {
		return err
	}
	if err := validateLongCoTNonNegativeIntFlag("--max-tokens", maxTokens); err != nil {
		return err
	}
	if err := validateLongCoTNonNegativeDurationFlag("--timeout", timeout); err != nil {
		return err
	}
	if err := validateLongCoTNonNegativeIntFlag("--max-iterations", maxIterations); err != nil {
		return err
	}
	if err := validateLongCoTNonNegativeIntFlag("--helper-attempts", helperAttempts); err != nil {
		return err
	}
	if err := validateLongCoTNonNegativeDurationFlag("--helper-timeout", helperTimeout); err != nil {
		return err
	}
	if err := validateLongCoTNonNegativeIntFlag("--helper-max-tokens", helperMaxTokens); err != nil {
		return err
	}
	return nil
}

func validateLongCoTNonNegativeIntFlag(flag string, value int) error {
	if value < 0 {
		return fmt.Errorf("%s must be >= 0", flag)
	}
	return nil
}

func validateLongCoTNonNegativeDurationFlag(flag string, value time.Duration) error {
	if value < 0 {
		return fmt.Errorf("%s must be >= 0", flag)
	}
	return nil
}

func normalizeLongCoTHelperLanguage(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", rlmruntime.HelperLanguageGo, rlmruntime.HelperLanguagePython:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported --helper-language %q (allowed: go, python)", value)
	}
}

func normalizeLongCoTSandboxKind(raw string) (rlmruntime.SandboxKind, error) {
	sandbox := rlmruntime.NormalizeSandboxKind(rlmruntime.SandboxKind(raw))
	if !rlmruntime.IsSupportedSandboxKind(sandbox) {
		return "", fmt.Errorf("unsupported --sandbox %q (allowed: python, smolvm, yaegi)", raw)
	}
	return sandbox, nil
}

func normalizeLongCoTHelperFlags(ephemeralSkills, generalHelper, requireEphemeralSkills bool) (bool, bool) {
	if requireEphemeralSkills {
		ephemeralSkills = true
		generalHelper = true
	}
	if generalHelper {
		ephemeralSkills = true
	}
	return ephemeralSkills, generalHelper
}

func normalizeLongCoTReviewInputs(
	rawFormat string,
	rawReviewMode string,
	reviewIter int,
	reviewRecursive bool,
	reviewMaxDepth int,
	reviewMaxSubcalls int,
	reviewCandidateMaxChars int,
	reviewChildSummaryMaxChars int,
	reviewChildSummaryRewrite bool,
	reviewChildSummaryRewriteIter int,
) (string, string, longCoTReviewConfig, error) {
	format, err := normalizeLongCoTFormat(rawFormat)
	if err != nil {
		return "", "", longCoTReviewConfig{}, err
	}
	reviewMode, err := normalizeLongCoTReviewMode(rawReviewMode)
	if err != nil {
		return "", "", longCoTReviewConfig{}, err
	}
	if reviewIter < 1 {
		return "", "", longCoTReviewConfig{}, fmt.Errorf("--rlm-review-iterations must be >= 1")
	}
	reviewCfg, err := newLongCoTReviewConfig(
		reviewMode,
		reviewIter,
		reviewRecursive,
		reviewMaxDepth,
		reviewMaxSubcalls,
		reviewCandidateMaxChars,
		reviewChildSummaryMaxChars,
		reviewChildSummaryRewrite,
		reviewChildSummaryRewriteIter,
	)
	if err != nil {
		return "", "", longCoTReviewConfig{}, err
	}
	return format, reviewMode, reviewCfg, nil
}

func normalizeLongCoTQuestionFilter(split string, domains []string, difficulty string, limit int, seed int64) (longCoTQuestionFilter, error) {
	filter := longCoTQuestionFilter{
		Split:      strings.TrimSpace(split),
		Domains:    normalizeDistinctLower(domains),
		Difficulty: strings.TrimSpace(difficulty),
		Limit:      limit,
		Seed:       seed,
	}
	if _, err := resolveLongCoTDifficultySet(filter.Difficulty); err != nil {
		return longCoTQuestionFilter{}, err
	}
	return filter, nil
}

func normalizeLongCoTFormat(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "markdown", "md":
		return "markdown", nil
	case "json":
		return "json", nil
	default:
		return "", fmt.Errorf("unsupported --format %q", raw)
	}
}

func normalizeLongCoTReviewMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "off", "false", "none", "never":
		return "off", nil
	case "auto":
		return "auto", nil
	case "always", "true", "on":
		return "always", nil
	default:
		return "", fmt.Errorf("unsupported --rlm-review %q (allowed: off, auto, always)", raw)
	}
}

type longCoTReviewConfig struct {
	Mode                          string
	Iterations                    int
	Recursive                     bool
	MaxDepth                      int
	MaxSubcalls                   int
	CandidateMaxChars             int
	ChildSummaryMaxChars          int
	ChildSummaryRewrite           bool
	ChildSummaryRewriteIterations int
}

func newLongCoTReviewConfig(
	mode string,
	iterations int,
	recursive bool,
	maxDepth, maxSubcalls int,
	candidateMaxChars int,
	childSummaryMaxChars int,
	childSummaryRewrite bool,
	childSummaryRewriteIterations int,
) (longCoTReviewConfig, error) {
	cfg := longCoTReviewConfig{
		Mode:                          mode,
		Iterations:                    iterations,
		Recursive:                     recursive,
		MaxDepth:                      maxDepth,
		MaxSubcalls:                   maxSubcalls,
		CandidateMaxChars:             candidateMaxChars,
		ChildSummaryMaxChars:          childSummaryMaxChars,
		ChildSummaryRewrite:           childSummaryRewrite,
		ChildSummaryRewriteIterations: childSummaryRewriteIterations,
	}
	if candidateMaxChars < 0 {
		return longCoTReviewConfig{}, fmt.Errorf("--rlm-review-candidate-max-chars must be >= 0")
	}
	if childSummaryMaxChars < 0 {
		return longCoTReviewConfig{}, fmt.Errorf("--rlm-review-child-summary-max-chars must be >= 0")
	}
	if childSummaryRewriteIterations < 1 {
		return longCoTReviewConfig{}, fmt.Errorf("--rlm-review-child-summary-rewrite-iterations must be >= 1")
	}
	if !recursive {
		return cfg, nil
	}
	if maxDepth < 2 {
		return longCoTReviewConfig{}, fmt.Errorf("--rlm-review-max-depth must be >= 2 when --rlm-review-recursive is set")
	}
	if maxSubcalls < 1 {
		return longCoTReviewConfig{}, fmt.Errorf("--rlm-review-max-subcalls must be >= 1 when --rlm-review-recursive is set")
	}
	return cfg, nil
}

func longCoTReviewConfigForQuestion(question longcoteval.Question, cfg longCoTReviewConfig) longCoTReviewConfig {
	if !question.RLMReviewRecursive {
		return cfg
	}
	cfg.Recursive = true
	if cfg.MaxDepth < 2 {
		cfg.MaxDepth = 2
	}
	if cfg.MaxSubcalls < 1 {
		cfg.MaxSubcalls = 1
	}
	return cfg
}

type longCoTConditionRuntime struct {
	MaxTokens              int
	Timeout                time.Duration
	Temperature            float64
	Seed                   int64
	MaxIter                int
	SandboxKind            rlmruntime.SandboxKind
	EphemeralSkills        bool
	GeneralHelper          bool
	RequireEphemeralSkills bool
	BlocksWorldHelper      bool
}

func resolveLongCoTConditions(values []string, runtime longCoTConditionRuntime) ([]longcoteval.Condition, error) {
	ids := parseLongCoTConditionIDs(values)
	if len(ids) == 0 {
		ids = []string{
			string(longcoteval.ConditionBaselineNoToolsOfficial),
			string(longcoteval.ConditionRLMNoToolsSingle),
		}
	}
	out := make([]longcoteval.Condition, 0, len(ids))
	for _, rawID := range ids {
		id := longcoteval.ConditionID(rawID)
		template, ok := longCoTConditionTemplates[id]
		if !ok {
			allowed := allowedLongCoTConditionIDs()
			return nil, fmt.Errorf("unknown --condition %q (allowed: %s)", rawID, strings.Join(allowed, ", "))
		}
		condition := template
		condition.TimeoutMS = runtime.Timeout.Milliseconds()
		condition.MaxTokens = runtime.MaxTokens
		condition.Temperature = runtime.Temperature
		condition.Seed = runtime.Seed
		if runtime.MaxIter > 0 {
			condition.MaxIterations = runtime.MaxIter
		}
		condition.AllowedTools = longCoTAllowedToolsForCondition(condition, runtime.SandboxKind, runtime.EphemeralSkills, runtime.GeneralHelper)
		out = append(out, condition)
	}
	return out, nil
}

func allowedLongCoTConditionIDs() []string {
	out := make([]string, 0, len(longCoTConditionTemplates))
	for id := range longCoTConditionTemplates {
		out = append(out, string(id))
	}
	sort.Strings(out)
	return out
}

func parseLongCoTConditionIDs(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
}

func longCoTAllowedToolsForCondition(condition longcoteval.Condition, sandbox rlmruntime.SandboxKind, ephemeralSkills bool, _ bool) []string {
	replToolName := rlmruntime.PythonREPLToolName
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replToolName = rlmruntime.GoREPLToolName
	}
	withEphemeral := func(tools []string) []string {
		if !ephemeralSkills {
			return tools
		}
		return []string{rlmruntime.EphemeralHelperSolveToolName}
	}
	switch condition.ID {
	case longcoteval.ConditionBaselineNoToolsOfficial, longcoteval.ConditionRLMNoToolsStaged:
		return nil
	case longcoteval.ConditionRLMReplNoSubcalls:
		return withEphemeral([]string{replToolName})
	case longcoteval.ConditionRLMNoModelToolsSingle:
		return []string{replToolName}
	case longcoteval.ConditionRLMLambdaReplSingle, longcoteval.ConditionRLMLambdaAdaptiveSingle, longcoteval.ConditionRLMLambdaThenBraidSingle:
		tools := []string{replToolName, rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName}
		if ephemeralSkills {
			tools = append(tools, rlmruntime.EphemeralHelperSolveToolName)
		}
		return tools
	case longcoteval.ConditionRLMReplRecursive, longcoteval.ConditionRLMNoToolsSingle, longcoteval.ConditionRLMBraidSingle:
		return withEphemeral([]string{replToolName, rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName})
	}
	return longCoTAllowedToolsForProfile(condition.RLMToolProfile)
}

func longCoTAllowedToolsForProfile(profile string) []string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", rlmenv.ToolProfileLongCoTNoModelTools:
		return nil
	default:
		return nil
	}
}

func longCoTEffectiveConditionForQuestion(question longcoteval.Question, condition longcoteval.Condition) longcoteval.Condition {
	effective := condition
	if condition.ID == longcoteval.ConditionRLMNoToolsSingle {
		return effective
	}
	if condition.ID != longcoteval.ConditionRLMReplRecursive {
		return effective
	}
	if longCoTQuestionEnablesRecursiveSubcalls(question) {
		return effective
	}
	effective.MaxSubcalls = 0
	effective.MaxDepth = 1
	effective.AllowedTools = longCoTReplOnlyTools(condition.AllowedTools)
	return effective
}

func longCoTQuestionEnablesRecursiveSubcalls(question longcoteval.Question) bool {
	return question.AllowOptionalSubcalls || len(question.RequiredSubcallRules) > 0
}

func longCoTReplOnlyTools(allowed []string) []string {
	for _, tool := range allowed {
		switch strings.TrimSpace(tool) {
		case rlmruntime.PythonREPLToolName:
			return []string{rlmruntime.PythonREPLToolName}
		case rlmruntime.GoREPLToolName:
			return []string{rlmruntime.GoREPLToolName}
		}
	}
	return []string{rlmruntime.PythonREPLToolName}
}

func longCoTRecursiveReviewTools(allowed []string, sandbox rlmruntime.SandboxKind) []string {
	replTool := longCoTReplOnlyTools(allowed)
	if len(replTool) == 0 {
		replTool = []string{rlmruntime.PythonREPLToolName}
	}
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replTool = []string{rlmruntime.GoREPLToolName}
	}
	return []string{replTool[0], rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName}
}

func longCoTLeakageOptionsForCondition(condition longcoteval.Condition) longcoteval.LeakageOptions {
	return longcoteval.LeakageOptions{
		SubcallsAllowed: longCoTConditionAllowsSubcalls(condition),
	}
}

func longCoTConditionAllowsSubcalls(condition longcoteval.Condition) bool {
	for _, raw := range condition.AllowedTools {
		switch strings.TrimSpace(raw) {
		case "subcall", rlmruntime.RLMQueryToolName:
			return true
		}
	}
	return false
}

func resolveLongCoTLiveTarget(cfg config.Config, provider, model, baseURL, apiKey string) (longCoTLiveTarget, error) {
	models := make([]string, 0, 1)
	if value := strings.TrimSpace(model); value != "" {
		models = append(models, value)
	}
	resolved, err := resolvePromptComparisonTarget(cfg, provider, baseURL, apiKey, models)
	if err != nil {
		return longCoTLiveTarget{}, fmt.Errorf("resolve live target: %w", err)
	}
	resolvedModel := ""
	if len(resolved.Models) > 0 {
		resolvedModel = strings.TrimSpace(resolved.Models[0])
	}
	if resolvedModel == "" {
		return longCoTLiveTarget{}, fmt.Errorf("resolve live target: model is required")
	}
	resolvedProvider := strings.TrimSpace(resolved.Provider)
	if resolvedProvider == "" {
		resolvedProvider = "lmstudio"
	}
	authMode, authHeader, authPrefix := longCoTTargetAuth(cfg, resolvedProvider, resolved.APIKey)
	return longCoTLiveTarget{
		Provider:   resolvedProvider,
		Model:      resolvedModel,
		BaseURL:    strings.TrimSpace(resolved.BaseURL),
		APIKey:     strings.TrimSpace(resolved.APIKey),
		AuthMode:   authMode,
		AuthHeader: authHeader,
		AuthPrefix: authPrefix,
	}, nil
}

func longCoTTargetAuth(cfg config.Config, provider, apiKey string) (string, string, string) {
	authMode := cfg.LLM.ResolveAuthMode(provider)
	authHeader := cfg.LLM.ResolveAuthHeader(provider)
	authPrefix := cfg.LLM.ResolveAuthPrefix(provider)
	if strings.EqualFold(strings.TrimSpace(authMode), "none") &&
		strings.TrimSpace(apiKey) != "" &&
		longCoTProviderDefaultsToBearer(provider) {
		authMode = "bearer"
		if strings.TrimSpace(authHeader) == "" {
			authHeader = "Authorization"
		}
		if authPrefix == "" {
			authPrefix = "Bearer "
		}
	}
	return authMode, authHeader, authPrefix
}

func longCoTProviderDefaultsToBearer(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openrouter", "openai", "anthropic", "groq", "cerebras", "gemini":
		return true
	default:
		return false
	}
}

func runLongCoTLiveAttempts(
	ctx context.Context,
	cfg config.Config,
	workspaceRoot, runID string,
	questions []longcoteval.Question,
	conditions []longcoteval.Condition,
	target longCoTLiveTarget,
	helperRuntime longCoTHelperRuntime,
	agentRole string,
	sandbox rlmruntime.SandboxKind,
	reviewCfg longCoTReviewConfig,
	ephemeralSkills bool,
	generalHelper bool,
	requireEphemeralSkills bool,
	blocksworldHelper bool,
	finalFromVerifiedHandoff bool,
) ([]longcoteval.Attempt, error) {
	companionDB, companionClose, _ := openRLMCompanionDB(ctx, cfg)
	if companionClose != nil {
		defer func() { _ = companionClose() }()
	}
	attempts := make([]longcoteval.Attempt, 0, len(questions)*len(conditions))
	for _, question := range questions {
		for _, condition := range conditions {
			effectiveCondition := longCoTEffectiveConditionForQuestion(question, condition)
			attempt := longcoteval.Attempt{
				RunID:         runID,
				PairID:        question.ID,
				AttemptID:     longCoTAttemptID(runID, question.ID, condition.ID),
				QuestionID:    question.ID,
				ConditionID:   condition.ID,
				ConditionKind: condition.Kind,
				Provider:      target.Provider,
				Model:         target.Model,
				Runner:        string(longCoTRunnerForCondition(condition)),
				LeakageFlags:  longcoteval.AssessLeakage(effectiveCondition.AllowedTools, longCoTLeakageOptionsForCondition(effectiveCondition)),
			}
			if condition.Kind == longcoteval.ConditionKindRLM {
				attempt.RLM = &longcoteval.RLMAttemptMeta{
					RouteProfile:  effectiveCondition.RLMRouteProfile,
					PlanMode:      effectiveCondition.RLMPlanMode,
					ToolProfile:   effectiveCondition.RLMToolProfile,
					MaxDepth:      longCoTConditionMaxDepth(effectiveCondition),
					MaxIterations: effectiveCondition.MaxIterations,
					MaxSubcalls:   effectiveCondition.MaxSubcalls,
				}
			}

			start := time.Now()
			outcome, runErr := runLongCoTLiveAttempt(ctx, cfg, workspaceRoot, companionDB, question, effectiveCondition, target, helperRuntime, agentRole, sandbox, attempt.AttemptID, ephemeralSkills, generalHelper, requireEphemeralSkills, blocksworldHelper, finalFromVerifiedHandoff)
			attempt.DurationMS = time.Since(start).Milliseconds()
			if outcome.DurationMS > 0 {
				attempt.DurationMS = outcome.DurationMS
			}
			if runErr != nil {
				if errors.Is(runErr, errLongCoTLiveConditionUnsupported) {
					attempt.Status = longCoTAttemptStatusUnsupported
				} else {
					attempt.Status = longcoteval.AttemptStatusError
				}
				attempt.Error = strings.TrimSpace(runErr.Error())
				attempt.ResponseText = strings.TrimSpace(outcome.ResponseText)
				attempt.Usage = outcome.Usage
				attempt.ToolEvents = outcome.ToolEvents
				attempt.SessionID = outcome.SessionID
				attempt.TrajectoryID = outcome.TrajectoryID
				if outcome.RLM != nil {
					attempt.RLM = outcome.RLM
				}
				longCoTAttachEffectiveContractMeta(&attempt, effectiveCondition)
				attempts = append(attempts, attempt)
				continue
			}

			attempt.Status = outcome.Status
			attempt.ResponseText = outcome.ResponseText
			attempt.Usage = outcome.Usage
			attempt.ToolEvents = outcome.ToolEvents
			attempt.SessionID = outcome.SessionID
			attempt.TrajectoryID = outcome.TrajectoryID
			attempt.Error = strings.TrimSpace(outcome.Error)
			if outcome.RLM != nil {
				attempt.RLM = outcome.RLM
			}
			longCoTAttachEffectiveContractMeta(&attempt, effectiveCondition)
			enforceLongCoTOutputSanitization(&attempt)
			effectiveReviewCfg := longCoTReviewConfigForQuestion(question, reviewCfg)
			if shouldReviewLongCoTAttempt(question, effectiveCondition, attempt.Status, effectiveReviewCfg.Mode) && strings.TrimSpace(attempt.ResponseText) != "" {
				reviewed, reviewErr := runLongCoTRLMReviewAttempt(ctx, workspaceRoot, question, effectiveCondition, target, sandbox, attempt.AttemptID+"-review", attempt.ResponseText, effectiveReviewCfg)
				if reviewErr != nil {
					if effectiveReviewCfg.Recursive {
						fallbackCfg := effectiveReviewCfg
						fallbackCfg.Recursive = false
						fallbackCfg.MaxDepth = 1
						fallbackCfg.MaxSubcalls = 0
						fallback, fallbackErr := runLongCoTRLMReviewAttempt(ctx, workspaceRoot, question, effectiveCondition, target, sandbox, attempt.AttemptID+"-review-fallback", attempt.ResponseText, fallbackCfg)
						if fallbackErr == nil {
							if fallback.RLM != nil {
								if fallback.RLM.Metadata == nil {
									fallback.RLM.Metadata = map[string]any{}
								}
								fallback.RLM.Metadata["review_fallback"] = "non_recursive"
								fallback.RLM.Metadata["review_recursive_error"] = strings.TrimSpace(reviewErr.Error())
								fallback.RLM.Metadata["review_recursive_requested"] = true
							}
							reviewed = fallback
							reviewErr = nil
						}
					}
				}
				if reviewErr != nil {
					attempt.Error = firstNonEmpty(attempt.Error, "rlm review failed: "+strings.TrimSpace(reviewErr.Error()))
				} else {
					applyLongCoTReviewOutcome(&attempt, reviewed)
				}
			}
			enforceLongCoTOutputSanitization(&attempt)
			if attempt.Status == "" {
				attempt.Status = longcoteval.AttemptStatusOK
			}
			if attempt.Status == longcoteval.AttemptStatusOK && strings.TrimSpace(attempt.ResponseText) == "" {
				attempt.Status = longcoteval.AttemptStatusError
				attempt.Error = firstNonEmpty(attempt.Error, "empty model response")
			}
			attempts = append(attempts, attempt)
		}
	}
	return attempts, nil
}

type longCoTLiveAttemptOutcome struct {
	Status       longcoteval.AttemptStatus
	ResponseText string
	Usage        longcoteval.Usage
	ToolEvents   []longcoteval.ToolEvent
	SessionID    string
	TrajectoryID string
	DurationMS   int64
	Error        string
	RLM          *longcoteval.RLMAttemptMeta
}

func longCoTEffectiveContractMetadata(condition longcoteval.Condition) map[string]any {
	toolSurface := "model_tools"
	if len(condition.AllowedTools) == 0 {
		toolSurface = "none"
	}
	conditionLabel := string(condition.ID)
	switch condition.ID {
	case longcoteval.ConditionRLMNoModelToolsSingle:
		conditionLabel = "rlm_no_model_tools_single"
	case longcoteval.ConditionRLMNoModelToolsStaged:
		conditionLabel = "rlm_no_model_tools_staged"
	}
	return map[string]any{
		"contract_version": "longcot_eval_v1",
		"condition_id":     string(condition.ID),
		"condition_label":  conditionLabel,
		"condition_kind":   string(condition.Kind),
		"route_profile":    strings.TrimSpace(condition.RLMRouteProfile),
		"plan_mode":        strings.TrimSpace(condition.RLMPlanMode),
		"tool_profile":     strings.TrimSpace(condition.RLMToolProfile),
		"tool_surface":     toolSurface,
		"allowed_tools":    append([]string(nil), condition.AllowedTools...),
		"max_depth":        longCoTConditionMaxDepth(condition),
		"max_iterations":   condition.MaxIterations,
		"max_subcalls":     condition.MaxSubcalls,
	}
}

func longCoTAttachEffectiveContractMeta(attempt *longcoteval.Attempt, condition longcoteval.Condition) {
	if attempt == nil || condition.Kind != longcoteval.ConditionKindRLM {
		return
	}
	if attempt.RLM == nil {
		attempt.RLM = &longcoteval.RLMAttemptMeta{
			RouteProfile:  condition.RLMRouteProfile,
			PlanMode:      condition.RLMPlanMode,
			ToolProfile:   condition.RLMToolProfile,
			MaxDepth:      longCoTConditionMaxDepth(condition),
			MaxIterations: condition.MaxIterations,
			MaxSubcalls:   condition.MaxSubcalls,
		}
	}
	if attempt.RLM.Metadata == nil {
		attempt.RLM.Metadata = map[string]any{}
	}
	attempt.RLM.Metadata["effective_contract"] = longCoTEffectiveContractMetadata(condition)
	if attempt.Status == longCoTAttemptStatusUnsupported {
		attempt.RLM.Metadata["unsupported_live_condition"] = true
	}
}

func shouldReviewLongCoTAttempt(question longcoteval.Question, condition longcoteval.Condition, status longcoteval.AttemptStatus, mode string) bool {
	if status != longcoteval.AttemptStatusOK || condition.Kind != longcoteval.ConditionKindRLM {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "off":
		return false
	case "always":
		return true
	case "auto":
		return question.RLMReview || question.RLMReviewRecursive || strings.EqualFold(strings.TrimSpace(question.Difficulty), "hard")
	default:
		return false
	}
}

func applyLongCoTReviewOutcome(attempt *longcoteval.Attempt, reviewed longCoTLiveAttemptOutcome) {
	if attempt == nil {
		return
	}
	var preReviewSanitization any
	if attempt.RLM != nil && attempt.RLM.Metadata != nil {
		if value, ok := attempt.RLM.Metadata["output_sanitization"]; ok {
			preReviewSanitization = value
			delete(attempt.RLM.Metadata, "output_sanitization")
		}
	}
	attempt.ResponseText = reviewed.ResponseText
	attempt.Usage = addLongCoTUsage(attempt.Usage, reviewed.Usage)
	attempt.ToolEvents = mergeLongCoTToolEvents(attempt.ToolEvents, reviewed.ToolEvents)
	if attempt.RLM == nil {
		attempt.RLM = reviewed.RLM
		return
	}
	if reviewed.RLM == nil {
		return
	}
	if attempt.RLM.Metadata == nil {
		attempt.RLM.Metadata = map[string]any{}
	}
	attempt.RLM.Metadata["review"] = reviewed.RLM.Metadata
	attempt.RLM.Metadata["review_response_text"] = reviewed.ResponseText
	if preReviewSanitization != nil {
		attempt.RLM.Metadata["pre_review_output_sanitization"] = preReviewSanitization
	}
}

type longCoTOutputSanitization struct {
	Changed   bool     `json:"changed"`
	Artifacts []string `json:"artifacts,omitempty"`
	RawText   string   `json:"raw_text,omitempty"`
}

func enforceLongCoTOutputSanitization(attempt *longcoteval.Attempt) longCoTOutputSanitization {
	if attempt == nil {
		return longCoTOutputSanitization{}
	}
	sanitized, info := sanitizeLongCoTResponseText(attempt.ResponseText)
	if !info.Changed {
		return info
	}
	attempt.ResponseText = sanitized
	if attempt.RLM == nil {
		attempt.RLM = &longcoteval.RLMAttemptMeta{}
	}
	if attempt.RLM.Metadata == nil {
		attempt.RLM.Metadata = map[string]any{}
	}
	attempt.RLM.Metadata["output_sanitization"] = map[string]any{
		"changed":   true,
		"artifacts": append([]string(nil), info.Artifacts...),
		"raw_text":  info.RawText,
	}
	return info
}

func sanitizeLongCoTResponseText(response string) (string, longCoTOutputSanitization) {
	sanitized, info := rlm.SanitizeOutputText(response)
	if !info.Changed {
		return "", longCoTOutputSanitization{}
	}
	return sanitized, longCoTOutputSanitization{
		Changed:   true,
		Artifacts: append([]string(nil), info.Artifacts...),
		RawText:   info.RawText,
	}
}

func runLongCoTLiveAttempt(
	ctx context.Context,
	cfg config.Config,
	workspaceRoot string,
	companionDB *sql.DB,
	question longcoteval.Question,
	condition longcoteval.Condition,
	target longCoTLiveTarget,
	helperRuntime longCoTHelperRuntime,
	agentRole string,
	sandbox rlmruntime.SandboxKind,
	attemptID string,
	ephemeralSkills bool,
	generalHelper bool,
	requireEphemeralSkills bool,
	blocksworldHelper bool,
	finalFromVerifiedHandoff bool,
) (longCoTLiveAttemptOutcome, error) {
	switch longCoTRunnerForCondition(condition) {
	case longCoTLiveRunnerAgent:
		return runLongCoTAgentBaselineAttempt(ctx, workspaceRoot, question, condition, target, agentRole)
	case longCoTLiveRunnerRLM:
		return runLongCoTRLMAttempt(ctx, cfg, workspaceRoot, companionDB, question, condition, target)
	case longCoTLiveRunnerREPL:
		if condition.ID == longcoteval.ConditionRLMLambdaThenBraidSingle {
			return runLongCoTHybridLambdaThenBraidAttempt(ctx, workspaceRoot, question, condition, target, helperRuntime, sandbox, attemptID, ephemeralSkills, generalHelper, requireEphemeralSkills, blocksworldHelper, finalFromVerifiedHandoff)
		}
		return runLongCoTREPLAttempt(ctx, workspaceRoot, question, condition, target, helperRuntime, sandbox, attemptID, ephemeralSkills, generalHelper, requireEphemeralSkills, blocksworldHelper, finalFromVerifiedHandoff)
	default:
		return longCoTLiveAttemptOutcome{}, fmt.Errorf("unsupported condition runner for %s", condition.ID)
	}
}

func longCoTRunnerForCondition(condition longcoteval.Condition) longCoTLiveRunner {
	if condition.ID == longcoteval.ConditionBaselineNoToolsOfficial || condition.Kind == longcoteval.ConditionKindBaseline {
		return longCoTLiveRunnerAgent
	}
	if condition.ID == longcoteval.ConditionRLMReplNoSubcalls ||
		condition.ID == longcoteval.ConditionRLMReplRecursive ||
		condition.ID == longcoteval.ConditionRLMLambdaReplSingle ||
		condition.ID == longcoteval.ConditionRLMLambdaAdaptiveSingle ||
		condition.ID == longcoteval.ConditionRLMLambdaThenBraidSingle ||
		condition.ID == longcoteval.ConditionRLMBraidSingle ||
		condition.ID == longcoteval.ConditionRLMNoToolsSingle ||
		condition.ID == longcoteval.ConditionRLMNoModelToolsSingle {
		return longCoTLiveRunnerREPL
	}
	return longCoTLiveRunnerRLM
}

func runLongCoTAgentBaselineAttempt(
	ctx context.Context,
	workspaceRoot string,
	question longcoteval.Question,
	condition longcoteval.Condition,
	target longCoTLiveTarget,
	agentRole string,
) (longCoTLiveAttemptOutcome, error) {
	effectiveTimeout := time.Duration(condition.TimeoutMS) * time.Millisecond
	if effectiveTimeout <= 0 {
		effectiveTimeout = 90 * time.Second
	}
	maxIterations := condition.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 1
	}

	rt := agentruntime.NewRuntime(agentruntime.Config{
		DefaultMaxIterations: maxIterations,
		DefaultTimeout:       effectiveTimeout,
		LLMProvider:          target.Provider,
		LLMModel:             target.Model,
		LLMAPIKey:            target.APIKey,
		LLMBaseURL:           target.BaseURL,
		LLMAuthMode:          target.AuthMode,
		LLMAuthHeader:        target.AuthHeader,
		LLMAuthPrefix:        target.AuthPrefix,
		WorkspaceRoot:        workspaceRoot,
	})
	runCtx, cancel := context.WithTimeout(ctx, effectiveTimeout)
	defer cancel()

	session, err := rt.Spawn(runCtx, agenttypes.AgentConfig{
		Role:          agenttypes.AgentRole(strings.TrimSpace(agentRole)),
		ActorID:       "actor:eval:longcot:" + ulid.Make().String(),
		WorkspaceID:   workspaceRoot,
		WorkspaceRoot: workspaceRoot,
		Prompt:        buildLongCoTOfficialBaselinePrompt(question.PromptText),
		SkillsAllow:   []string{"__longcot_no_tools__"},
		MaxIterations: maxIterations,
		MaxTokens:     condition.MaxTokens,
		Timeout:       effectiveTimeout,
		LLMProvider:   target.Provider,
		LLMModel:      target.Model,
		LLMAPIKey:     target.APIKey,
		LLMBaseURL:    target.BaseURL,
		LLMAuthMode:   target.AuthMode,
		LLMAuthHeader: target.AuthHeader,
		LLMAuthPrefix: target.AuthPrefix,
	})
	if err != nil {
		return longCoTLiveAttemptOutcome{}, err
	}
	<-session.Done()

	status := longCoTStatusFromAgentStatus(session.Status, session.Error)
	if strings.TrimSpace(session.Error) != "" {
		status = longcoteval.AttemptStatusError
	}

	toolNames := collectRuntimeToolNames(session.ToolCalls)
	durationMS := int64(0)
	if session.EndedAt != nil {
		durationMS = session.EndedAt.Sub(session.StartedAt).Milliseconds()
	}
	return longCoTLiveAttemptOutcome{
		Status:       status,
		ResponseText: strings.TrimSpace(session.Summary),
		DurationMS:   durationMS,
		SessionID:    session.ID,
		Usage: longcoteval.Usage{
			InputTokens:  session.InputTokens,
			OutputTokens: session.OutputTokens,
			TotalTokens:  session.TotalTokens,
		},
		ToolEvents: longCoTToolEventsFromNames(toolNames),
		Error:      strings.TrimSpace(session.Error),
	}, nil
}

func longCoTStatusFromAgentStatus(status agenttypes.AgentStatus, sessionErr string) longcoteval.AttemptStatus {
	switch status {
	case agenttypes.StatusOK:
		if strings.TrimSpace(sessionErr) != "" {
			return longcoteval.AttemptStatusError
		}
		return longcoteval.AttemptStatusOK
	case agenttypes.StatusCanceled:
		return longcoteval.AttemptStatusCanceled
	default:
		return longcoteval.AttemptStatusError
	}
}

func buildLongCoTOfficialBaselinePrompt(prompt string) string {
	// Official LongCoT prompts already carry answer formatting instructions.
	// Preserve prompt text exactly in the baseline condition.
	return strings.TrimSpace(prompt)
}

func runLongCoTRLMAttempt(
	ctx context.Context,
	cfg config.Config,
	workspaceRoot string,
	companionDB *sql.DB,
	question longcoteval.Question,
	condition longcoteval.Condition,
	target longCoTLiveTarget,
) (longCoTLiveAttemptOutcome, error) {
	routeProfile, planMode, err := longCoTRLMExecutionSettings(condition)
	if err != nil {
		if errors.Is(err, errLongCoTLiveConditionUnsupported) {
			return longCoTLiveAttemptOutcome{
				Status: longCoTAttemptStatusUnsupported,
				Error:  strings.TrimSpace(err.Error()),
				RLM: &longcoteval.RLMAttemptMeta{
					RouteProfile:  condition.RLMRouteProfile,
					PlanMode:      condition.RLMPlanMode,
					ToolProfile:   condition.RLMToolProfile,
					MaxDepth:      longCoTConditionMaxDepth(condition),
					MaxIterations: condition.MaxIterations,
					MaxSubcalls:   condition.MaxSubcalls,
					Metadata: map[string]any{
						"unsupported_live_condition": true,
						"effective_contract":         longCoTEffectiveContractMetadata(condition),
					},
				},
			}, nil
		}
		return longCoTLiveAttemptOutcome{}, err
	}
	task := rlm.Task{
		Prompt:        buildLongCoTRLMTaskPrompt(question.PromptText, condition),
		WorkspaceRoot: workspaceRoot,
		MaxDepth:      longCoTConditionMaxDepth(condition),
		MaxIterations: condition.MaxIterations,
		MaxSubcalls:   condition.MaxSubcalls,
	}
	env := longCoTSafeRLMEnvironment(condition)

	adapter := rlmenv.NewReadOnlyAdapter(cfg, workspaceRoot, "", companionDB, env)
	if root := strings.TrimSpace(cfg.Storage.Root); root != "" {
		if ceStore, err := ctxengstore.Open(ctx, root); err == nil {
			adapter.SetContextEngineStore(ceStore)
			defer func() { _ = ceStore.Close() }()
		}
		if taskStore, err := tasks.Open(ctx, root); err == nil {
			adapter.SetTaskStore(taskStore)
			defer func() { _ = taskStore.Close() }()
		}
	}
	timeout := time.Duration(condition.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := rlm.LLMRunner{
		Tools: adapter,
		Config: rlm.LLMConfig{
			Provider:      target.Provider,
			APIKey:        target.APIKey,
			BaseURL:       target.BaseURL,
			Model:         target.Model,
			Timeout:       timeout,
			MaxTokens:     condition.MaxTokens,
			Temperature:   condition.Temperature,
			MaxIterations: condition.MaxIterations,
			RouteProfile:  routeProfile,
			PlanMode:      planMode,
		},
	}.Run(runCtx, task, env)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return longCoTLiveAttemptOutcome{Status: longcoteval.AttemptStatusCanceled}, err
		}
		return longCoTLiveAttemptOutcome{}, err
	}
	toolNames := longCoTStringSliceFromAny(result.Metadata["tool_names"])
	usage := longCoTUsageFromRLMResult(result)
	return longCoTLiveAttemptOutcome{
		Status:       longcoteval.AttemptStatusOK,
		ResponseText: strings.TrimSpace(result.Answer),
		Usage:        usage,
		ToolEvents:   longCoTToolEventsFromNames(toolNames),
		RLM:          longCoTRLMMetaFromResult(condition, result),
	}, nil
}

func runLongCoTREPLAttempt(
	ctx context.Context,
	workspaceRoot string,
	question longcoteval.Question,
	condition longcoteval.Condition,
	target longCoTLiveTarget,
	helperRuntime longCoTHelperRuntime,
	sandbox rlmruntime.SandboxKind,
	attemptID string,
	ephemeralSkills bool,
	generalHelper bool,
	requireEphemeralSkills bool,
	blocksworldHelper bool,
	finalFromVerifiedHandoff bool,
) (longCoTLiveAttemptOutcome, error) {
	timeout := time.Duration(condition.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	maxIterations := condition.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 32
	}
	task := rlm.Task{
		Prompt:        buildLongCoTREPLTaskPromptForQuestion(question, condition, sandbox, ephemeralSkills, generalHelper, requireEphemeralSkills, blocksworldHelper),
		RunID:         strings.TrimSpace(attemptID),
		AgentID:       "eval/longcot/" + string(condition.ID),
		WorkspaceRoot: workspaceRoot,
		MaxDepth:      longCoTConditionMaxDepth(condition),
		MaxIterations: maxIterations,
		MaxSubcalls:   condition.MaxSubcalls,
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	runnerCfg := longCoTREPLRunnerConfig(question, condition, target, helperRuntime, timeout, maxIterations, workspaceRoot, sandbox, ephemeralSkills, generalHelper, requireEphemeralSkills, blocksworldHelper, finalFromVerifiedHandoff)
	longCoTApplyAttemptSandboxWorkDir(&runnerCfg, attemptID)
	result, err := (&rlmruntime.REPLRunner{Config: runnerCfg}).Run(runCtx, task, rlm.Environment{})
	if generalHelper {
		markLongCoTGeneralHelperResult(&result)
	}
	if err != nil {
		if blocksworldHelper && !generalHelper {
			if finalized, ok := longCoTBlocksWorldFinalResponse(question); ok {
				result = rlm.Result{
					Answer: finalized,
					Metadata: map[string]any{
						"blocksworld_runtime_finalized":    true,
						"blocksworld_final_response":       finalized,
						"blocksworld_fallback_after_error": err.Error(),
						"tool_names":                       []string{longCoTBlocksWorldSolveToolName},
					},
				}
				return longCoTLiveAttemptOutcome{
					Status:       longcoteval.AttemptStatusOK,
					ResponseText: finalized,
					ToolEvents:   longCoTToolEventsFromNames([]string{longCoTBlocksWorldSolveToolName}),
					DurationMS:   time.Since(start).Milliseconds(),
					RLM:          longCoTRLMMetaFromResult(condition, result),
				}, nil
			}
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			toolNames := longCoTStringSliceFromAny(result.Metadata["tool_names"])
			return longCoTLiveAttemptOutcome{
				Status:       longcoteval.AttemptStatusCanceled,
				ResponseText: strings.TrimSpace(result.Answer),
				Usage:        longCoTUsageFromRLMResult(result),
				ToolEvents:   longCoTToolEventsFromNames(toolNames),
				DurationMS:   time.Since(start).Milliseconds(),
				RLM:          longCoTRLMMetaFromResult(condition, result),
			}, err
		}
		toolNames := longCoTStringSliceFromAny(result.Metadata["tool_names"])
		return longCoTLiveAttemptOutcome{
			Status:       longcoteval.AttemptStatusError,
			ResponseText: strings.TrimSpace(result.Answer),
			Usage:        longCoTUsageFromRLMResult(result),
			ToolEvents:   longCoTToolEventsFromNames(toolNames),
			DurationMS:   time.Since(start).Milliseconds(),
			RLM:          longCoTRLMMetaFromResult(condition, result),
		}, err
	}
	if finalized, ok := longCoTBlocksWorldFinalResponse(question); blocksworldHelper && !generalHelper && ok {
		result.Answer = finalized
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		result.Metadata["blocksworld_runtime_finalized"] = true
		result.Metadata["blocksworld_final_response"] = finalized
		result.Metadata["tool_names"] = appendLongCoTStringUnique(longCoTStringSliceFromAny(result.Metadata["tool_names"]), longCoTBlocksWorldSolveToolName)
	}
	toolNames := longCoTStringSliceFromAny(result.Metadata["tool_names"])
	return longCoTLiveAttemptOutcome{
		Status:       longcoteval.AttemptStatusOK,
		ResponseText: strings.TrimSpace(result.Answer),
		Usage:        longCoTUsageFromRLMResult(result),
		ToolEvents:   longCoTToolEventsFromNames(toolNames),
		DurationMS:   time.Since(start).Milliseconds(),
		RLM:          longCoTRLMMetaFromResult(condition, result),
	}, nil
}

func runLongCoTHybridLambdaThenBraidAttempt(
	ctx context.Context,
	workspaceRoot string,
	question longcoteval.Question,
	condition longcoteval.Condition,
	target longCoTLiveTarget,
	helperRuntime longCoTHelperRuntime,
	sandbox rlmruntime.SandboxKind,
	attemptID string,
	ephemeralSkills bool,
	generalHelper bool,
	requireEphemeralSkills bool,
	blocksworldHelper bool,
	finalFromVerifiedHandoff bool,
) (longCoTLiveAttemptOutcome, error) {
	lambdaCondition := longCoTLambdaBranchCondition(condition)
	lambdaOutcome, lambdaErr := runLongCoTREPLAttempt(
		ctx,
		workspaceRoot,
		question,
		lambdaCondition,
		target,
		helperRuntime,
		sandbox,
		attemptID+"-lambda",
		ephemeralSkills,
		generalHelper,
		requireEphemeralSkills,
		blocksworldHelper,
		finalFromVerifiedHandoff,
	)
	if lambdaErr == nil && lambdaOutcome.Status == longcoteval.AttemptStatusOK && strings.TrimSpace(lambdaOutcome.ResponseText) != "" {
		longCoTMarkHybridOutcome(&lambdaOutcome, condition, map[string]any{
			"selected":           "lambda",
			"lambda_status":      string(lambdaOutcome.Status),
			"lambda_duration_ms": lambdaOutcome.DurationMS,
			"fallback_invoked":   false,
		})
		return lambdaOutcome, nil
	}

	fallbackReason := strings.TrimSpace(lambdaOutcome.Error)
	if fallbackReason == "" && lambdaErr != nil {
		fallbackReason = strings.TrimSpace(lambdaErr.Error())
	}
	if fallbackReason == "" {
		fallbackReason = fmt.Sprintf("lambda branch returned status=%s response_empty=%v", lambdaOutcome.Status, strings.TrimSpace(lambdaOutcome.ResponseText) == "")
	}

	braidCondition := longCoTBraidFallbackCondition(condition)
	braidOutcome, braidErr := runLongCoTREPLAttempt(
		ctx,
		workspaceRoot,
		question,
		braidCondition,
		target,
		helperRuntime,
		sandbox,
		attemptID+"-braid",
		ephemeralSkills,
		generalHelper,
		requireEphemeralSkills,
		blocksworldHelper,
		finalFromVerifiedHandoff,
	)
	braidOutcome.Usage = addLongCoTUsage(lambdaOutcome.Usage, braidOutcome.Usage)
	braidOutcome.ToolEvents = mergeLongCoTToolEvents(lambdaOutcome.ToolEvents, braidOutcome.ToolEvents)
	longCoTMarkHybridOutcome(&braidOutcome, condition, map[string]any{
		"selected":                  "braid",
		"fallback_invoked":          true,
		"fallback_reason":           fallbackReason,
		"lambda_status":             string(lambdaOutcome.Status),
		"lambda_error":              firstNonEmpty(lambdaOutcome.Error, errorString(lambdaErr)),
		"lambda_duration_ms":        lambdaOutcome.DurationMS,
		"lambda_response_available": strings.TrimSpace(lambdaOutcome.ResponseText) != "",
		"lambda_usage":              lambdaOutcome.Usage,
		"lambda_tool_names":         longCoTToolNamesFromOutcome(lambdaOutcome),
		"lambda_metadata":           longCoTRLMMetadataFromOutcome(lambdaOutcome),
		"braid_timeout_ms":          braidCondition.TimeoutMS,
		"braid_max_iterations":      braidCondition.MaxIterations,
		"braid_max_subcalls":        braidCondition.MaxSubcalls,
	})
	return braidOutcome, braidErr
}

func longCoTLambdaBranchCondition(condition longcoteval.Condition) longcoteval.Condition {
	branch := condition
	branch.ID = longcoteval.ConditionRLMLambdaAdaptiveSingle
	branch.RLMRouteProfile = "longcot_lambda_adaptive"
	branch.RLMPlanMode = "repl_lambda_adaptive"
	branch.RLMToolProfile = "longcot-repl-recursive"
	if branch.MaxDepth <= 0 || branch.MaxDepth > 2 {
		branch.MaxDepth = 2
	}
	if branch.MaxSubcalls <= 0 || branch.MaxSubcalls > 4 {
		branch.MaxSubcalls = 4
	}
	if branch.MaxIterations <= 0 || branch.MaxIterations > 24 {
		branch.MaxIterations = 24
	}
	return branch
}

func longCoTBraidFallbackCondition(condition longcoteval.Condition) longcoteval.Condition {
	branch := condition
	branch.ID = longcoteval.ConditionRLMBraidSingle
	branch.RLMRouteProfile = "longcot_repl_braid"
	branch.RLMPlanMode = "repl_braid"
	branch.RLMToolProfile = "longcot-repl-recursive"
	branch.MaxDepth = 1
	branch.MaxIterations = maxInt(branch.MaxIterations, 32)
	branch.MaxSubcalls = maxInt(branch.MaxSubcalls, 16)
	branch.TimeoutMS = longCoTBraidFallbackTimeoutMS(condition.TimeoutMS)
	return branch
}

func longCoTBraidFallbackTimeoutMS(lambdaTimeoutMS int64) int64 {
	if lambdaTimeoutMS <= 0 {
		lambdaTimeoutMS = (90 * time.Second).Milliseconds()
	}
	fallback := lambdaTimeoutMS * 2
	minimum := (720 * time.Second).Milliseconds()
	if fallback < minimum {
		fallback = minimum
	}
	return fallback
}

func longCoTMarkHybridOutcome(outcome *longCoTLiveAttemptOutcome, condition longcoteval.Condition, hybrid map[string]any) {
	if outcome == nil {
		return
	}
	if outcome.RLM == nil {
		outcome.RLM = &longcoteval.RLMAttemptMeta{}
	}
	outcome.RLM.RouteProfile = condition.RLMRouteProfile
	outcome.RLM.PlanMode = condition.RLMPlanMode
	outcome.RLM.ToolProfile = condition.RLMToolProfile
	outcome.RLM.MaxDepth = longCoTConditionMaxDepth(condition)
	outcome.RLM.MaxIterations = condition.MaxIterations
	outcome.RLM.MaxSubcalls = condition.MaxSubcalls
	if outcome.RLM.Metadata == nil {
		outcome.RLM.Metadata = map[string]any{}
	}
	outcome.RLM.Metadata["hybrid"] = hybrid
	outcome.RLM.Metadata["effective_contract"] = longCoTEffectiveContractMetadata(condition)
}

func longCoTRLMMetadataFromOutcome(outcome longCoTLiveAttemptOutcome) map[string]any {
	if outcome.RLM == nil || len(outcome.RLM.Metadata) == 0 {
		return nil
	}
	return outcome.RLM.Metadata
}

func longCoTToolNamesFromOutcome(outcome longCoTLiveAttemptOutcome) []string {
	if outcome.RLM != nil && outcome.RLM.Metadata != nil {
		if names := longCoTStringSliceFromAny(outcome.RLM.Metadata["tool_names"]); len(names) > 0 {
			return names
		}
	}
	out := make([]string, 0, len(outcome.ToolEvents))
	for _, event := range outcome.ToolEvents {
		if name := strings.TrimSpace(event.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func markLongCoTGeneralHelperResult(result *rlm.Result) {
	if result == nil {
		return
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["helper_scaffolded"] = true
	result.Metadata["leaderboard_comparable"] = false
	if helper, ok := result.Metadata["ephemeral_helper"].(map[string]any); ok {
		if preset, _ := helper["preset"].(string); strings.TrimSpace(preset) != "" {
			result.Metadata["helper_preset"] = strings.TrimSpace(preset)
		}
	}
}

func runLongCoTRLMReviewAttempt(
	ctx context.Context,
	workspaceRoot string,
	question longcoteval.Question,
	condition longcoteval.Condition,
	target longCoTLiveTarget,
	sandbox rlmruntime.SandboxKind,
	attemptID string,
	candidateAnswer string,
	reviewCfg longCoTReviewConfig,
) (longCoTLiveAttemptOutcome, error) {
	timeout := time.Duration(condition.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	maxIterations := reviewCfg.Iterations
	if maxIterations <= 0 {
		maxIterations = 3
	}
	candidateForReview, candidateCompaction := compactLongCoTReviewCandidate(candidateAnswer, reviewCfg.CandidateMaxChars)
	reviewCondition := condition
	reviewCondition.MaxDepth = reviewCfg.MaxDepth
	reviewCondition.MaxSubcalls = reviewCfg.MaxSubcalls
	reviewCondition.MaxIterations = maxIterations
	if reviewCfg.Recursive {
		reviewCondition.AllowedTools = longCoTRecursiveReviewTools(condition.AllowedTools, sandbox)
	} else {
		reviewCondition.AllowedTools = longCoTReplOnlyTools(condition.AllowedTools)
		reviewCondition.MaxDepth = 1
		reviewCondition.MaxSubcalls = 0
	}
	task := rlm.Task{
		Prompt:        buildLongCoTReviewPrompt(question.PromptText, candidateForReview, sandbox, reviewCfg),
		RunID:         strings.TrimSpace(attemptID),
		AgentID:       "eval/longcot/" + string(condition.ID) + "/review",
		WorkspaceRoot: workspaceRoot,
		MaxDepth:      reviewCondition.MaxDepth,
		MaxIterations: maxIterations,
		MaxSubcalls:   reviewCondition.MaxSubcalls,
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	runnerCfg := longCoTREPLRunnerConfig(question, reviewCondition, target, longCoTHelperRuntime{Target: target}, timeout, maxIterations, workspaceRoot, sandbox, false, false, false, false, false)
	runnerCfg.LLM.RequireToolUse = reviewCfg.Recursive
	if reviewCfg.Recursive {
		runnerCfg.Phases = longCoTRecursiveReviewPhases(sandbox)
		runnerCfg.DefaultREPLCode = longCoTDefaultReviewREPLCode(sandbox)
		runnerCfg.DefaultRLMQueryPrompt = buildLongCoTDefaultCriticPrompt(question.PromptText, candidateForReview)
		runnerCfg.ChildSummaryMaxChars = reviewCfg.ChildSummaryMaxChars
		runnerCfg.ChildSummaryRewriteOverLimit = reviewCfg.ChildSummaryRewrite
		runnerCfg.ChildSummaryRewriteMaxIterations = reviewCfg.ChildSummaryRewriteIterations
		runnerCfg.RejectFailedSubcalls = true
		runnerCfg.Budget.MaxIterations = maxIterations * len(runnerCfg.Phases)
	}
	if runnerCfg.InitialState == nil {
		runnerCfg.InitialState = map[string]any{}
	}
	runnerCfg.InitialState["candidate_answer"] = strings.TrimSpace(candidateForReview)
	result, err := (&rlmruntime.REPLRunner{Config: runnerCfg}).Run(runCtx, task, rlm.Environment{})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return longCoTLiveAttemptOutcome{Status: longcoteval.AttemptStatusCanceled, DurationMS: time.Since(start).Milliseconds()}, err
		}
		return longCoTLiveAttemptOutcome{}, err
	}
	toolNames := longCoTStringSliceFromAny(result.Metadata["tool_names"])
	rawAnswer := strings.TrimSpace(result.Answer)
	reviewedAnswer := cleanLongCoTReviewResponse(rawAnswer)
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["review_raw_response_text"] = rawAnswer
	if candidateCompaction.Changed {
		result.Metadata["review_candidate_compaction"] = map[string]any{
			"changed":       true,
			"raw_chars":     candidateCompaction.RawChars,
			"compact_chars": candidateCompaction.CompactChars,
			"max_chars":     candidateCompaction.MaxChars,
		}
	}
	result.Metadata["review_recursive_requested"] = reviewCfg.Recursive
	result.Metadata["review_recursive_used"] = longCoTIntFromAny(result.Metadata["recursive_subcalls_used"]) > 0
	return longCoTLiveAttemptOutcome{
		Status:       longcoteval.AttemptStatusOK,
		ResponseText: reviewedAnswer,
		Usage:        longCoTUsageFromRLMResult(result),
		ToolEvents:   longCoTToolEventsFromNames(toolNames),
		DurationMS:   time.Since(start).Milliseconds(),
		RLM:          longCoTRLMMetaFromResult(reviewCondition, result),
	}, nil
}

func buildLongCoTReviewPrompt(officialPrompt, candidateAnswer string, sandbox rlmruntime.SandboxKind, reviewCfg longCoTReviewConfig) string {
	replToolName := rlmruntime.PythonREPLToolName
	language := "Python"
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replToolName = rlmruntime.GoREPLToolName
		language = "Go"
	}
	var b strings.Builder
	b.WriteString("LongCoT RLM review pass.\n")
	fmt.Fprintf(&b, "Before returning the reviewed answer, call %s with a short %s snippet that inspects official_prompt and candidate_answer.\n", replToolName, language)
	if reviewCfg.Recursive {
		b.WriteString("Do not use answer keys, official verifiers, hidden datasets, files, network, or memory.\n")
	} else {
		b.WriteString("Do not use answer keys, official verifiers, hidden datasets, files, network, memory, or recursive child calls.\n")
	}
	b.WriteString("Review whether the candidate answer solves the official task and follows the requested answer format.\n")
	if reviewCfg.Recursive {
		b.WriteString("Recursive review contract: after the REPL inspection, you must call rlm_query before any final answer.\n")
		b.WriteString("Tool order must be: python_repl first, then all rlm_query child critic calls, then rlm_wait({}), then the final reviewed answer. Do not call rlm_wait before rlm_query, and do not call rlm_query after rlm_wait.\n")
		b.WriteString("Use rlm_query to ask independent child critics to check correctness and answer-format compliance, then call rlm_wait({}) before producing the final reviewed answer.\n")
		b.WriteString("Ask children for concise verdicts only. Do not ask them to access files, network, memory, answer keys, or official verifiers. A child critic should answer with verdict, issues, and suggested final answer.\n")
		fmt.Fprintf(&b, "Review recursion limits: max_depth=%d, max_subcalls=%d. Use at most two child critics unless the task clearly needs fewer.\n", reviewCfg.MaxDepth, reviewCfg.MaxSubcalls)
		b.WriteString("rlm_query, rlm_wait, and rlm_result are model tools, not REPL functions. Do not invent child IDs.\n")
	}
	b.WriteString("If the candidate is correct, output only the final answer in the requested format. If it is wrong, incomplete, verbose, or uses placeholders, output only a corrected final answer in the requested format.\n\n")
	b.WriteString("Official task:\n")
	b.WriteString(strings.TrimSpace(officialPrompt))
	b.WriteString("\n\nCandidate answer:\n")
	b.WriteString(strings.TrimSpace(candidateAnswer))
	return b.String()
}

type longCoTReviewCandidateCompaction struct {
	Changed      bool
	RawChars     int
	CompactChars int
	MaxChars     int
}

func compactLongCoTReviewCandidate(candidate string, maxChars int) (string, longCoTReviewCandidateCompaction) {
	candidate = strings.TrimSpace(candidate)
	info := longCoTReviewCandidateCompaction{
		RawChars: runeLenLocal(candidate),
		MaxChars: maxChars,
	}
	if candidate == "" || maxChars <= 0 || info.RawChars <= maxChars {
		info.CompactChars = info.RawChars
		return candidate, info
	}
	if maxChars < 64 {
		runes := []rune(candidate)
		out := string(runes[:maxChars])
		info.Changed = true
		info.CompactChars = runeLenLocal(out)
		return out, info
	}
	headBudget := maxChars / 2
	tailBudget := maxChars - headBudget - len("\n...[candidate truncated]...\n")
	if tailBudget < 16 {
		tailBudget = 16
		headBudget = maxChars - tailBudget - len("\n...[candidate truncated]...\n")
	}
	runes := []rune(candidate)
	out := string(runes[:headBudget]) + "\n...[candidate truncated]...\n" + string(runes[len(runes)-tailBudget:])
	info.Changed = true
	info.CompactChars = runeLenLocal(out)
	return out, info
}

func runeLenLocal(text string) int {
	return len([]rune(text))
}

func longCoTRecursiveReviewPhases(sandbox rlmruntime.SandboxKind) []rlmruntime.REPLRunnerPhase {
	replToolName := rlmruntime.PythonREPLToolName
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replToolName = rlmruntime.GoREPLToolName
	}
	return []rlmruntime.REPLRunnerPhase{
		{
			Name:                    "inspect",
			Prompt:                  "Inspect official_prompt and candidate_answer using the scratch REPL. Do not produce a final answer in this phase.",
			Tools:                   []string{replToolName},
			RequiredTools:           []string{replToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
		{
			Name: "critic_query",
			Prompt: strings.Join([]string{
				"Submit one child critic now.",
				"Call rlm_query exactly as a model tool with a non-empty prompt.",
				"The child prompt must ask for a concise verdict about whether candidate_answer solves official_prompt and follows the requested answer format.",
				"If you set max_summary_chars, use 900 or less.",
				"Do not answer directly in this phase.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMQueryToolName},
			RequiredTools:           []string{rlmruntime.RLMQueryToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
		{
			Name: "critic_wait",
			Prompt: strings.Join([]string{
				"Wait for the submitted child critic.",
				"Call rlm_wait with empty JSON arguments: {}.",
				"Do not call rlm_query in this phase. Do not produce the final answer yet.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName},
			RequiredTools:           []string{rlmruntime.RLMWaitToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
		{
			Name: "final",
			Prompt: strings.Join([]string{
				"Produce only the reviewed final answer in the requested answer format.",
				"Use the prior tool outputs and child critic summary. Do not include review prose.",
			}, "\n"),
			MaxIterations: 1,
			Final:         true,
		},
	}
}

func buildLongCoTDefaultCriticPrompt(officialPrompt, candidateAnswer string) string {
	var b strings.Builder
	b.WriteString("You are a bounded LongCoT review child critic.\n")
	b.WriteString("Check whether the candidate answer solves the official task and follows the requested answer format.\n")
	b.WriteString("Do not use files, network, memory, answer keys, or official verifiers.\n")
	b.WriteString("Return only: verdict, issues, suggested_final_answer.\n\n")
	b.WriteString("Official task:\n")
	b.WriteString(strings.TrimSpace(officialPrompt))
	b.WriteString("\n\nCandidate answer:\n")
	b.WriteString(strings.TrimSpace(candidateAnswer))
	return b.String()
}

func longCoTBraidSolvePhases(sandbox rlmruntime.SandboxKind) []rlmruntime.REPLRunnerPhase {
	replToolName := rlmruntime.PythonREPLToolName
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replToolName = rlmruntime.GoREPLToolName
	}
	phases := []rlmruntime.REPLRunnerPhase{
		{
			Name: "context",
			Prompt: strings.Join([]string{
				"Inspect official_prompt with the scratch REPL.",
				"Extract only compact facts needed for solving: requested answer format, requested final values, known values, dependencies, and blockers.",
				"Do not produce a final answer in this phase.",
			}, "\n"),
			Tools:                   []string{replToolName},
			RequiredTools:           []string{replToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
		{
			Name: "graph_plan",
			Prompt: strings.Join([]string{
				"Return JSON only. Build a bounded reasoning graph with keys: version, nodes, final_node.",
				"The word json is intentional: return one valid json object and nothing else.",
				"Set version to 1.",
				"Use 4 to 12 nodes in this controller shape: extract -> one primary solve wave, optionally alternate/repair solve waves, dependency clusters, or one cycle_solve wave when justified -> verify -> reduce.",
				"Each node must include id, kind, question, depends_on, expected_output, max_summary_chars, and helper_policy.",
				"Every solve, cycle_solve, and verify node must additionally include archetype, scaffold_class, scaffold_id, and input_schema. These scaffold fields tell the runtime how to hand off execution. Omitting them is a validation error.",
				"Allowed kind values are extract, solve, cycle_solve, verify, reduce.",
				"Allowed helper_policy values are auto, preferred, required, never. Use never for extract and reduce. Use preferred for solve-like and verify nodes that may need exact search, simulation, parsing, or constraint checking.",
				"Allowed scaffold pairs are strict: symbolic_trace/type_inference_v1, candidate_verify/property_check_v1, state_transition/state_replay_v1, finite_state_transition/stack_relocation_v1, explicit_dag/search_backtrack_v1, graph_search/resource_path_min_initial_v1, graph_search/explicit_shortest_path_v1, numeric_dp/recurrence_table_v1, sequence_simulation/json_patch_v1, constraint_solver/finite_domain_v1.",
				"Do not use generic_v1. If no specialized scaffold applies, use explicit_dag/search_backtrack_v1 with input_schema {\"source_ref\":\"official_prompt\",\"prompt\":\"use official_prompt plus extracted dependencies\"}.",
				"If the task gives explicit initial_state and goal_state arrays for stacks or other finite transitions, prefer finite_state_transition/stack_relocation_v1 or state_transition/state_replay_v1 over explicit_dag.",
				"Use state_transition/state_replay_v1 only for replaying an explicit action sequence such as UCI chess moves. For algebraic chains, placeholder dependency chains, or independent subproblem DAGs, use explicit_dag/search_backtrack_v1 or numeric_dp/recurrence_table_v1 instead.",
				"input_schema must be a JSON object whose keys describe the structured input for the scaffold. For example, a chess replay node might use {\"move_sequence\":\"UCI moves\",\"goal\":\"FEN output\"}. For explicit dependency graphs, target_nodes means requested final output ids, solve_targets means independent split work items, and cycle_clusters means strongly connected target groups, e.g. {\"target_nodes\":[\"node_4\",\"node_2\",\"node_7\"],\"cycle_clusters\":[[\"node_2\",\"node_5\",\"node_6\",\"node_7\"]],\"prompt\":\"original problem and extracted dependencies\"}. Do not put final requested outputs in solve_targets unless they are truly independent.",
				"Do not copy large literals from the official task into node questions or input_schema: arrays, tables, code blocks, formulas, move lists, or long prose must stay in official_prompt/extract summaries. Use source_ref fields such as \"official_prompt\", \"extract_summary\", or \"candidate_answer\" and short natural-language selectors instead. The runtime passes the official task and dependency summaries to children.",
				"The extract node has depends_on []. The primary solve node depends on extract and must produce a complete candidate answer or candidate construction. The verify node depends on the final candidate-producing solve-like node. The reduce node depends on the final candidate-producing solve-like node and verify and must be final_node.",
				"Create one solve-like node by default. Add a second solve-like node only when it is a real independent alternate candidate, a concrete repair of the first candidate, or a true dependency cluster; do not split a single state-transition plan into arbitrary prose segments.",
				"Use small max_summary_chars for facts and verifier verdicts. Use a large max_summary_chars, up to 12000, for any solve node whose exact candidate may be long and must be verified losslessly downstream.",
				"Only put solve-like nodes in the same wave when they are mathematically independent. If one solve-like node needs another solve-like node's output, include that node id in depends_on.",
				"cycle_solve is optional. Use it only when extracted facts contain a true mutually dependent numeric/logical cluster: circular references, fixed-point equations, recursive definitions, or flow constraints that can be checked by finite bounded search.",
				"For state-transition, planning, simulation, path construction, program tracing, or BlocksWorld-style stack puzzles, do not segment the plan by vague phases. Use one primary solve node to build an executable candidate action sequence or value assignment, then a verify node to simulate/substitute against the original constraints.",
				"If you do use cycle_solve, collapse that strongly connected constraint cluster into one cycle_solve node and declare the same cluster in input_schema.cycle_clusters. Keep the runtime graph acyclic; do not encode mutual dependency as depends_on edges between separate nodes.",
				"The extract node must only extract facts: placeholders, requested outputs, equations, and dependency constraints. It must not solve, verify, reduce, or declare blocked.",
				"Each question must name one leaf-solvable subproblem, dependency cluster, or verification target; children receive the official task text automatically.",
				"Leaf-solvable means the child can work directly from the official task plus dependency summaries; do not ask children to recurse, spawn agents, wait, or request more runtime depth.",
				"A cycle_solve question must ask the child to model unknowns, constraints, candidate bounds, and a bounded search/fixed-point/propagation check. It must not ask the child to wait for another cyclic node.",
				"The verify node must independently substitute the candidate answer into the original placeholders and constraints. It must not merely check consistency with a prior summary.",
				"Node questions and expected_output must not mention rlm_query, rlm_wait, rlm_result, subagents, recursion budget, or runtime depth.",
				"Do not include markdown, Mermaid, prose, code fences, or trailing text.",
				"Example explicit-DAG shape: {\"version\":1,\"nodes\":[{\"id\":\"n_extract\",\"kind\":\"extract\",\"question\":\"Extract requested_outputs, known_values, dependency_edges, placeholders, cycle_clusters, equations_or_checks, candidate_bounds, and blockers as facts only\",\"depends_on\":[],\"expected_output\":\"facts and dependency graph; no blocked verdict\",\"max_summary_chars\":1200,\"helper_policy\":\"never\"},{\"id\":\"n_solve_plan\",\"kind\":\"solve\",\"question\":\"Use official_prompt and n_extract facts to solve requested outputs and produce a complete candidate answer\",\"depends_on\":[\"n_extract\"],\"expected_output\":\"full candidate answer in requested format; no truncation\",\"max_summary_chars\":12000,\"helper_policy\":\"preferred\",\"archetype\":\"explicit_dag\",\"scaffold_class\":\"explicit_dag\",\"scaffold_id\":\"search_backtrack_v1\",\"input_schema\":{\"source_ref\":\"official_prompt\",\"dependency_ref\":\"n_extract\",\"target_nodes\":[\"node_4\",\"node_2\",\"node_7\"],\"cycle_clusters\":[[\"node_2\",\"node_5\",\"node_6\",\"node_7\"]],\"prompt\":\"use source_ref and dependency_ref; do not restate task literals\"}},{\"id\":\"n_verify\",\"kind\":\"verify\",\"question\":\"Substitute the full candidate into the original constraints; report first failed constraint or pass\",\"depends_on\":[\"n_solve_plan\"],\"expected_output\":\"pass true or first concrete failed constraint\",\"max_summary_chars\":1200,\"helper_policy\":\"preferred\",\"archetype\":\"candidate_verify\",\"scaffold_class\":\"candidate_verify\",\"scaffold_id\":\"property_check_v1\",\"input_schema\":{\"source_ref\":\"official_prompt\",\"candidates\":\"candidate answers\",\"predicates\":\"verification predicates\"}},{\"id\":\"n_reduce\",\"kind\":\"reduce\",\"question\":\"Return the final answer only if verification passed; otherwise return failed constraints\",\"depends_on\":[\"n_solve_plan\",\"n_verify\"],\"expected_output\":\"solution line or concrete failed constraints\",\"max_summary_chars\":300,\"helper_policy\":\"never\"}],\"final_node\":\"n_reduce\"}",
			}, "\n"),
			MaxIterations:           1,
			OutputKind:              rlmruntime.REPLPhaseOutputKindBraidGraph,
			ResponseFormat:          json.RawMessage(`{"type":"json_object"}`),
			MaxTokens:               longCoTBraidGraphPlanMaxTokens,
			MaxGraphNodes:           12,
			BraidGraphPolicy:        rlmruntime.BraidGraphPolicyLongCoTController,
			RequireScaffoldContract: true,
		},
		{
			Name: "graph_fanout",
			Prompt: strings.Join([]string{
				"Runtime phase: the runtime executes graph nodes from graph_plan in topological waves.",
				"Each child receives the official task text and dependency summaries from prior waves.",
				"Do not answer directly in this phase.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMQueryToolName},
			MaxTokens:               longCoTBraidGraphFanoutMaxTokens,
			MaxIterations:           1,
			AutoExecuteGraphNodes:   true,
			BraidRepairAttempts:     2,
			BraidGraphPolicy:        rlmruntime.BraidGraphPolicyLongCoTController,
			RequireScaffoldContract: true,
		},
		{
			Name: "final",
			Prompt: strings.Join([]string{
				"Final formatting phase only.",
				"Your response must be exactly one line in this format: solution = <value>.",
			}, "\n"),
			MaxTokens:     longCoTBraidFinalMaxTokens,
			MaxIterations: 1,
			Final:         true,
		},
	}
	return phases
}

const (
	longCoTBraidGraphPlanMaxTokens   = 4096
	longCoTBraidGraphFanoutMaxTokens = 4096
	longCoTBraidFinalMaxTokens       = 2048
	longCoTBraidChildMaxTokens       = 4096
	longCoTBraidChildVerifyTokens    = 2048
	longCoTBraidChildFilterTokens    = 2048
)

func longCoTRecursiveSolvePhases(sandbox rlmruntime.SandboxKind) []rlmruntime.REPLRunnerPhase {
	replToolName := rlmruntime.PythonREPLToolName
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replToolName = rlmruntime.GoREPLToolName
	}
	return []rlmruntime.REPLRunnerPhase{
		{
			Name: "context",
			Prompt: strings.Join([]string{
				"Inspect official_prompt with the scratch REPL.",
				"Extract only compact facts needed for solving: requested answer format, requested final values, known values, dependencies, and blockers.",
				"Do not produce a final answer in this phase.",
			}, "\n"),
			Tools:                   []string{replToolName},
			RequiredTools:           []string{replToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
		{
			Name: "fanout-1",
			Prompt: strings.Join([]string{
				"Submit multiple child solves now using the context from the REPL output.",
				"The runtime will fan out bounded rlm_query calls for compact value derivation and verification.",
				"Do not answer directly in this phase.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMQueryToolName},
			RequiredTools:           []string{rlmruntime.RLMQueryToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
			AutoExecuteToolCalls: longCoTFanoutQueryCalls([]string{
				"Return compact lines only: answer, values, blockers. Identify requested final values and dependencies only if needed. Do not write a graph or proof.",
				"Solve or verify the earliest root-value branch. Return compact lines only: answer, values, checks, blockers.",
				"Analyze the final requested values. Return compact lines only: answer if known, missing dependencies, blockers.",
			}),
		},
		{
			Name: "wait-1",
			Prompt: strings.Join([]string{
				"Wait for submitted child solves.",
				"Call rlm_wait with empty JSON arguments: {}.",
				"Do not call rlm_query or produce a final answer in this phase.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName},
			RequiredTools:           []string{rlmruntime.RLMWaitToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
		{
			Name: "integrate",
			Prompt: strings.Join([]string{
				"Use the scratch REPL to integrate the first child summary with the original context.",
				"Compute newly unlocked values or identify one remaining branch that still needs a child solve.",
				"Do not produce a final answer in this phase.",
			}, "\n"),
			Tools:                   []string{replToolName},
			RequiredTools:           []string{replToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
		{
			Name: "fanout-2",
			Prompt: strings.Join([]string{
				"Submit one refinement child solve using the integrated context.",
				"The runtime will call rlm_query for a bounded remaining branch or consistency check.",
				"Do not answer directly in this phase.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMQueryToolName},
			RequiredTools:           []string{rlmruntime.RLMQueryToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
			AutoExecuteToolCalls: longCoTFanoutQueryCalls([]string{
				"Use the integrated context and child summaries to solve one remaining branch or verify final answer components. Return compact lines only: answer, values, checks, blockers.",
			}),
		},
		{
			Name: "wait-2",
			Prompt: strings.Join([]string{
				"Wait for the latest submitted child solve.",
				"Call rlm_wait with empty JSON arguments: {}.",
				"Do not call rlm_query or produce a final answer in this phase.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName},
			RequiredTools:           []string{rlmruntime.RLMWaitToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
		{
			Name: "final",
			Prompt: strings.Join([]string{
				"Final formatting phase only. Do not solve from scratch and do not derive node values in prose.",
				"Use only the compact prior REPL work and child summaries. If they are incomplete, still output the best one-line answer candidate in the official format.",
				"Your entire response must be exactly one line beginning with `solution =`. Do not include scratch work, dependency graphs, proof prose, review prose, markdown, or tool names.",
			}, "\n"),
			MaxIterations: 1,
			Final:         true,
		},
	}
}

func longCoTDefaultContextREPLCode(sandbox rlmruntime.SandboxKind) string {
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		return `println("PROMPT_PACKET_JSON={\"status\":\"blocked\",\"reason\":\"compact context parser is only implemented for Python sandbox\"}")`
	}
	return longCoTPromptPacketREPLCode(sandbox)
}

func longCoTFanoutQueryCalls(prompts []string) []rlmruntime.REPLRunnerPhaseAutoToolCall {
	out := make([]rlmruntime.REPLRunnerPhaseAutoToolCall, 0, len(prompts))
	for _, prompt := range prompts {
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			continue
		}
		out = append(out, rlmruntime.REPLRunnerPhaseAutoToolCall{
			Tool: rlmruntime.RLMQueryToolName,
			Args: mustLongCoTAutoToolArgs(map[string]any{
				"prompt":         prompt,
				"max_iterations": 2,
			}),
		})
	}
	return out
}

func longCoTFanoutWorkerCount(maxSubcalls int) int {
	if maxSubcalls <= 0 {
		return 1
	}
	if maxSubcalls < 3 {
		return maxSubcalls
	}
	return 3
}

func longCoTLambdaReplSolvePhases(sandbox rlmruntime.SandboxKind) []rlmruntime.REPLRunnerPhase {
	replToolName := rlmruntime.PythonREPLToolName
	verifyCodeContract := []string{
		"Return raw verifier code only. The runtime will execute this text with the scratch REPL.",
		"Your first non-empty line must be executable code, not a comment.",
		"Use official_prompt and the prior child summaries in this phase prompt as inputs.",
		"Do not generate a new candidate from scratch. Extract one or more child candidate answers and verify them.",
		"The runtime injects `rlm_candidates`, keyed by runtime-issued child candidate_id values from rlm_wait/rlm_result output.",
		"For simple verifier code, read candidate answer strings from `rlm_candidate_answers[candidate_id]` or `candidate_answer(candidate_id)`; `rlm_candidates[candidate_id]` is metadata.",
		"Use only a candidate_id present in `rlm_candidates`; do not invent child IDs, node IDs, answer hashes, or artifact JSON.",
		"Use deterministic checks: replay state transitions, recompute arithmetic, type-check traces, parse candidate formats, or run independent consistency checks.",
		"Use only built-in language features and standard library packages. Do not import unavailable third-party packages.",
		"The runtime injects `check(name, pass_value, **evidence)` and `accept_candidate(candidate_id, checks=[...], final_answer=None)` helpers.",
		"Build checks with `check(...)`; include at least one `constraint_replay_or_recompute` check and one `goal_or_requested_output` check.",
		"Do not manually print VERIFIER_ARTIFACT_JSON. Call `accept_candidate(...)`; the runtime helper emits the artifact and binds the final answer to the registered child candidate.",
		"If every child candidate fails, raise AssertionError with the first failed check instead of printing a solution.",
		"Do not use shorthand string checks. Each check must be an object produced by `check(...)` with name/pass/evidence.",
		"Do not call python_repl, go_repl, rlm_query, rlm_wait, or rlm_result yourself.",
		"Do not include prose, markdown, or the final answer outside code.",
	}
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replToolName = rlmruntime.GoREPLToolName
		verifyCodeContract = append(verifyCodeContract,
			"Use Go REPL statements such as fmt.Println(\"solution = ...\").",
			"Do not include package declarations or import blocks.",
		)
	} else {
		verifyCodeContract = append(verifyCodeContract,
			"Use Python statements such as print('solution = ...').",
			"If a small local package is essential and import fails, verify with a standard-library fallback or raise AssertionError.",
		)
	}
	return []rlmruntime.REPLRunnerPhase{
		{
			Name: "lambda_fanout",
			Prompt: strings.Join([]string{
				"Submit recursive child solvers now.",
				"The parent is the verifier, not the primary solver. Children should independently solve the task or a candidate-producing decomposition.",
				"Do not answer directly in this phase.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMQueryToolName},
			RequiredTools:           []string{rlmruntime.RLMQueryToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
			AutoExecuteToolCalls: longCoTFanoutQueryCalls([]string{
				"Independent solver child A: solve the official task end-to-end if possible. Use scratch code for deterministic parsing/search/simulation/calculation. Return exactly one compact candidate line beginning solution = plus compact checks or blockers.",
				"Independent solver child B: solve the same official task using a different decomposition or algorithm when possible. Use scratch code for deterministic checks. Return exactly one compact candidate line beginning solution = plus compact checks or blockers.",
			}),
		},
		{
			Name: "lambda_wait",
			Prompt: strings.Join([]string{
				"Wait for submitted child solvers.",
				"Call rlm_wait with empty JSON arguments: {}.",
				"Do not call rlm_query and do not produce the final answer in this phase.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName},
			RequiredTools:           []string{rlmruntime.RLMWaitToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
		{
			Name: "lambda_verify",
			Prompt: strings.Join(append([]string{
				"Parent verification phase.",
				"The parent must verify child candidates; do not solve directly unless needed to check a child candidate.",
			}, verifyCodeContract...), "\n"),
			OutputKind:                      rlmruntime.REPLPhaseOutputKindREPLCode,
			Tools:                           []string{replToolName},
			RequiredTools:                   []string{replToolName},
			MaxTokens:                       900,
			MaxIterations:                   1,
			RequireToolResultOK:             true,
			RequireToolOutput:               true,
			RequireStructuredToolOutputOnly: true,
			InjectVerifierPrelude:           true,
			RequiredToolOutputSubstrings:    []string{"VERIFIER_ARTIFACT_JSON="},
			RequireVerifierArtifact:         true,
			MaxREPLCodeLines:                140,
			FilterOverlongREPLCode:          true,
			FilterREPLCodeMaxTokens:         1200,
			DisableREPLCodeRepair:           true,
		},
		{
			Name: "lambda_final",
			Prompt: strings.Join([]string{
				"Final answer phase.",
				"Use the child solver summaries and the lambda_verify REPL output.",
				"Do not call tools. Do not output code, markdown, scratch prose, or runtime/tool discussion.",
				"The verifier output already satisfied the runtime verifier artifact requirement.",
				"Return exactly one line beginning solution = with the requested final answer.",
			}, "\n"),
			MaxIterations:                 1,
			Final:                         true,
			ForwardVerifierArtifactAnswer: true,
			RuntimeOnlyFinal:              true,
		},
	}
}

func longCoTLambdaAdaptiveSolvePhases(sandbox rlmruntime.SandboxKind) []rlmruntime.REPLRunnerPhase {
	replToolName := rlmruntime.PythonREPLToolName
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replToolName = rlmruntime.GoREPLToolName
	}
	return []rlmruntime.REPLRunnerPhase{
		{
			Name: "solve_direct",
			Prompt: strings.Join([]string{
				"Direct solve phase.",
				"Solve the official task as a normal model without calling tools.",
				"Do not emit a BRAID graph, scratch code, or runtime discussion.",
				"If the answer is clear, answer in the official requested format.",
				"If you cannot solve directly, state the blocker briefly without inventing a partial solution.",
			}, "\n"),
			MaxIterations: 1,
		},
		{
			Name: "tool_assist",
			Prompt: strings.Join([]string{
				"Candidate verification and repair phase.",
				"If the prior phase produced a `solution = ...` answer, verify or repair that exact candidate with the scratch REPL before accepting it.",
				"If the prior phase did not produce an answer, use tools only when they clearly reduce risk: compact parsing, simulation, arithmetic, consistency checks, or bounded child delegation.",
				"If a small deterministic Python library is useful, request it through the python_repl packages field, for example packages:[\"python-chess\"] before importing chess. Do not run pip, subprocess, or shell commands from code.",
				"For long inputs, never paste or retype the official task data into code. Read and parse the REPL variables `official_prompt` or `prompt` so the computation uses the exact input.",
				"Do not emit a BRAID graph. Prefer a compact verifier/checker over a full generic solver.",
				"When you run a deterministic candidate check in the REPL, print one structured line: RLM_CHECK_JSON={\"pass\":true,\"reason\":\"...\"}. If the candidate fails, print RLM_CHECK_JSON={\"pass\":false,\"reason\":\"...\"} with the first concrete failure and do not output an accepted answer.",
				"If a tool-produced deterministic check accepts or repairs the answer, the REPL must also print one structured answer sentinel: RLM_ANSWER_JSON={\"answer\":\"solution = ...\",\"pass\":true,\"checks\":[...]}.",
				"The answer field must contain the official final answer line exactly as `solution = ...`. Include deterministic check summaries in checks when available.",
				"If blocked, briefly state the blocker and the runtime may escalate. Do not emit RLM_ANSWER_JSON unless the answer is accepted.",
			}, "\n"),
			Tools:                       []string{replToolName, rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName},
			RequiredTools:               []string{replToolName},
			RequireToolResultOK:         true,
			AutoVerifyPriorSolutionLine: true,
			IncludePriorAssistantText:   true,
			MaxIterations:               3,
		},
		{
			Name: "final",
			Prompt: strings.Join([]string{
				"Final formatting phase.",
				"Use the previous solve_direct structured answer sentinel.",
				"Do not call tools. Do not add markdown, scratch prose, or runtime discussion.",
				"Return the answer from exactly one prior RLM_ANSWER_JSON={\"answer\":\"solution = ...\",\"pass\":true,\"checks\":[...]} sentinel.",
				"Do not invent, rewrite, or repair an answer in this phase.",
			}, "\n"),
			MaxIterations:                       1,
			Final:                               true,
			BlockFinalOnFailedToolEvidence:      true,
			RuntimeOnlyFinal:                    true,
			ForwardStructuredToolAnswer:         true,
			ForwardExecutedStructuredToolAnswer: true,
			RequireStructuredToolAnswer:         true,
			ForwardPriorSolutionLine:            false,
		},
	}
}

func longCoTLambdaAdaptiveLongInputSolvePhases(sandbox rlmruntime.SandboxKind) []rlmruntime.REPLRunnerPhase {
	replToolName := rlmruntime.PythonREPLToolName
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replToolName = rlmruntime.GoREPLToolName
	}
	return []rlmruntime.REPLRunnerPhase{
		{
			Name: "prompt_packet",
			Prompt: strings.Join([]string{
				"Runtime parser phase.",
				"Use the scratch REPL to extract a compact structural packet from official_prompt.",
				"Do not solve the task and do not produce a final answer.",
				"The packet should include answer_format, section labels, compact section previews, counts, and likely exact-data fields.",
			}, "\n"),
			Tools:                   []string{replToolName},
			RequiredTools:           []string{replToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
			AutoExecuteToolCalls: []rlmruntime.REPLRunnerPhaseAutoToolCall{{
				Tool: replToolName,
				Args: mustLongCoTAutoToolArgs(map[string]any{
					"code": longCoTPromptPacketREPLCode(sandbox),
				}),
			}},
			RequireToolResultOK:          true,
			RequireToolOutput:            true,
			RequiredToolOutputSubstrings: []string{"PROMPT_PACKET_JSON="},
		},
		{
			Name: "long_fanout",
			Prompt: strings.Join([]string{
				"Submit bounded recursive children now using the prompt packet from the prior REPL output.",
				"The parent coordinates and verifies; children should have distinct roles: parser, solver, verifier.",
				"Do not answer directly in this phase.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMQueryToolName},
			RequiredTools:           []string{rlmruntime.RLMQueryToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
			AutoExecuteToolCalls: longCoTFanoutQueryCalls([]string{
				"Parser child: extract exact structured inputs from the official task, including answer format and compact refs to long lists/tables/code. Do not solve unless trivial. Return compact JSON-like facts and blockers.",
				"Solver child: use the parser facts and official task to produce one candidate answer. Use scratch code for deterministic calculation/simulation when useful. Return one compact candidate line beginning solution = plus checks or blockers.",
				"Verifier child: design and, where possible, execute deterministic checks for the requested output. Return check strategy, first failure if any, and solution = only if independently verified.",
			}),
		},
		{
			Name: "long_wait",
			Prompt: strings.Join([]string{
				"Wait for submitted child solvers.",
				"Call rlm_wait with empty JSON arguments: {}.",
				"Do not call rlm_query and do not produce the final answer in this phase.",
			}, "\n"),
			Tools:                   []string{rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName},
			RequiredTools:           []string{rlmruntime.RLMWaitToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
		{
			Name: "long_tool_verify",
			Prompt: strings.Join([]string{
				"Executable integration and verification phase.",
				"Return raw REPL code only. The runtime will execute it.",
				"The code must read the task from `official_prompt`; it is a Python string variable, not a file path. Do not call open(...) to read it.",
				"The runtime predefines accept(answer, checks=[...], reason=\"...\") and reject(reason). Use accept to emit the required RLM_CHECK_JSON and RLM_ANSWER_JSON sentinels; use reject for blockers.",
				"Do not paste move lists, programs, SMILES strings, tables, formulas, or other long task data as literals.",
				"When the prompt packet lists exact_data_sections, extract from that labeled section only. Do not regex the entire prompt if examples or format instructions contain similar tokens. Preserve full tokens, including suffixes such as promotion characters.",
				"Use prior child summaries as candidate sources, but verify or repair candidates with deterministic parsing, recomputation, simulation, type checking, or constraint checks.",
				longCoTCapabilityPromptLine(sandbox),
				"Prefer compact library-backed verification over hand-written domain engines. If a library can parse/simulate/typecheck/recompute the task exactly, use that library instead of implementing those semantics yourself.",
				"Keep verifier code compact: imports, small helper functions, computation, then accept/reject. Do not write a derivation in comments.",
				"Keep comments brief. If the solution needs a long derivation, put the derivation into executable checks instead of comments.",
				"Do not output tool-call XML, markdown, or prose; return only executable code.",
				"Call reject(\"first concrete failed check\") when blocked.",
				"When accepted, call accept(\"solution = ...\", checks=[...], reason=\"...\"). A bare print(\"solution = ...\") line is not enough for final forwarding.",
				"The answer must contain exactly one official final answer line beginning `solution =`.",
			}, "\n"),
			OutputKind:                      rlmruntime.REPLPhaseOutputKindREPLCode,
			Tools:                           []string{replToolName},
			RequiredTools:                   []string{replToolName},
			MaxTokens:                       4096,
			MaxIterations:                   1,
			RequireToolResultOK:             true,
			RequireToolOutput:               true,
			RequireStructuredToolOutputOnly: true,
			InjectVerifierPrelude:           true,
			AllowPartialPseudoToolCallCode:  true,
			AllowedREPLImports:              longCoTVerifierAllowedImports(sandbox),
			MaxREPLCodeLines:                140,
			FilterOverlongREPLCode:          true,
			FilterREPLCodeMaxTokens:         2048,
		},
		{
			Name: "final",
			Prompt: strings.Join([]string{
				"Final formatting phase.",
				"Return the answer from exactly one prior RLM_ANSWER_JSON={\"answer\":\"solution = ...\",\"pass\":true,\"checks\":[...]} sentinel.",
				"Do not invent, rewrite, or repair an answer in this phase.",
			}, "\n"),
			MaxIterations:                       1,
			Final:                               true,
			BlockFinalOnFailedToolEvidence:      true,
			RuntimeOnlyFinal:                    true,
			ForwardStructuredToolAnswer:         true,
			ForwardExecutedStructuredToolAnswer: true,
			RequireStructuredToolAnswer:         true,
			ForwardPriorSolutionLine:            false,
		},
	}
}

func longCoTVerifierAllowedImports(sandbox rlmruntime.SandboxKind) []string {
	if rlmruntime.NormalizeSandboxKind(sandbox) != rlmruntime.SandboxKindSmolVMPython {
		return nil
	}
	return []string{"chess", "rdkit", "sympy", "networkx", "numpy"}
}

func longCoTQuestionNeedsAdaptiveFanout(question longcoteval.Question) bool {
	prompt := strings.TrimSpace(question.PromptText)
	if len(prompt) >= 2000 {
		return true
	}
	if len(question.RequiredSubcallRules) > 0 {
		return true
	}
	return false
}

func longCoTPromptPacketREPLCode(sandbox rlmruntime.SandboxKind) string {
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		return `println("PROMPT_PACKET_JSON={\"status\":\"blocked\",\"reason\":\"prompt packet auto-parser is only implemented for Python sandbox\"}")`
	}
	capabilityJSON := longCoTPythonCapabilityJSONLiteral(sandbox)
	capabilitySpecsJSON := longCoTPythonCapabilitySpecsJSONLiteral(sandbox)
	return strings.Join([]string{
		"import importlib.util, json, re",
		"p = official_prompt",
		"runtime_capabilities = " + capabilityJSON,
		"capability_specs = " + capabilitySpecsJSON,
		"runtime_capabilities['python_modules'] = []",
		"for spec in capability_specs:",
		"    entry = dict(spec)",
		"    module = entry.get('import', '')",
		"    try:",
		"        available = importlib.util.find_spec(module) is not None if module else False",
		"    except Exception as exc:",
		"        available = False",
		"        entry['availability_error'] = repr(exc)",
		"    entry['available'] = bool(available)",
		"    runtime_capabilities['python_modules'].append(entry)",
		"lines = p.splitlines()",
		"sections = []",
		"for i, line in enumerate(lines):",
		"    s = line.strip()",
		"    if not s:",
		"        continue",
		"    if re.match(r'^(Subproblem|Question|Task|Input|Output|Chess Move Sequence|UCI format|Please provide|When you are done|Return your answer)', s, re.I):",
		"        sections.append({'line': i + 1, 'label': s[:120]})",
		"answer_format = ''",
		"m = re.search(r'(solution\\s*=\\s*<[^>]+>|solution\\s*=\\s*[^\\n\\.]+)', p, re.I)",
		"if m:",
		"    answer_format = m.group(1).strip()",
		"uci_moves = re.findall(r'\\b[a-h][1-8][a-h][1-8][nbrq]?\\b', p)",
		"smilesish = re.findall(r'(?<![A-Za-z0-9@+\\-\\[\\]\\(\\)=#/\\\\])([A-Za-z0-9@+\\-\\[\\]\\(\\)=#/\\\\]{8,})(?![A-Za-z0-9@+\\-\\[\\]\\(\\)=#/\\\\])', p)",
		"numbers = re.findall(r'(?<![A-Za-z0-9])[-+]?\\d+(?:\\.\\d+)?(?:\\^\\d+)?(?![A-Za-z0-9])', p)",
		"exact_data_sections = []",
		"for i, line in enumerate(lines):",
		"    if ':' not in line:",
		"        continue",
		"    label, value = line.split(':', 1)",
		"    label_clean = label.strip()",
		"    value = value.strip()",
		"    if not value:",
		"        continue",
		"    section = {'line': i + 1, 'label': label_clean[:120], 'extraction_rule': 'parse only this labeled value, not examples or instructions elsewhere in the prompt'}",
		"    section_uci = re.findall(r'\\b[a-h][1-8][a-h][1-8][nbrq]?\\b', value)",
		"    if section_uci:",
		"        section.update({'token_type': 'uci_moves', 'count': len(section_uci), 'head': section_uci[:5], 'tail': section_uci[-5:]})",
		"        exact_data_sections.append(section)",
		"        continue",
		"    section_numbers = re.findall(r'(?<![A-Za-z0-9])[-+]?\\d+(?:\\.\\d+)?(?:\\^\\d+)?(?![A-Za-z0-9])', value)",
		"    if section_numbers and len(section_numbers) >= 4:",
		"        section.update({'token_type': 'numbers', 'count': len(section_numbers), 'head': section_numbers[:8], 'tail': section_numbers[-8:]})",
		"        exact_data_sections.append(section)",
		"packet = {",
		"    'answer_format': answer_format,",
		"    'line_count': len(lines),",
		"    'char_count': len(p),",
		"    'sections': sections[:40],",
		"    'counts': {'uci_moves': len(uci_moves), 'smiles_like_tokens': len(smilesish), 'numbers': len(numbers)},",
		"    'samples': {'uci_moves_head': uci_moves[:5], 'uci_moves_tail': uci_moves[-5:], 'smiles_like_head': smilesish[:8], 'numbers_head': numbers[:12]},",
		"    'exact_data_sections': exact_data_sections[:12],",
		"    'runtime_capabilities': runtime_capabilities,",
		"    'exact_data_rule': 'Do not paste long literals into code. Parse exact data from official_prompt using these labels/counts.'",
		"}",
		"print('PROMPT_PACKET_JSON=' + json.dumps(packet, separators=(',', ':')))",
	}, "\n")
}

func longCoTPythonCapabilityJSONLiteral(sandbox rlmruntime.SandboxKind) string {
	body, err := json.Marshal(longCoTPythonCapabilities(sandbox))
	if err != nil {
		return "{}"
	}
	return string(body)
}

func longCoTPythonCapabilitySpecsJSONLiteral(sandbox rlmruntime.SandboxKind) string {
	body, err := json.Marshal(longCoTPythonCapabilitySpecs(sandbox))
	if err != nil {
		return "[]"
	}
	return string(body)
}

func longCoTPythonCapabilities(sandbox rlmruntime.SandboxKind) map[string]any {
	out := map[string]any{
		"official_prompt_binding": "string_variable",
		"official_prompt_rule":    "use official_prompt directly; do not open files or read /workspace/official_prompt",
	}
	if rlmruntime.NormalizeSandboxKind(sandbox) != rlmruntime.SandboxKindSmolVMPython {
		out["python_modules"] = []any{}
		out["package_install"] = "unavailable"
		return out
	}
	out["package_install"] = "runtime_controlled_allowlist"
	out["python_modules"] = longCoTPythonCapabilitySpecs(sandbox)
	return out
}

func longCoTPythonCapabilitySpecs(sandbox rlmruntime.SandboxKind) []map[string]any {
	if rlmruntime.NormalizeSandboxKind(sandbox) != rlmruntime.SandboxKindSmolVMPython {
		return nil
	}
	return []map[string]any{
		{
			"import":  "chess",
			"package": "python-chess",
			"uses":    []string{"legal move replay", "FEN generation", "board-state simulation"},
		},
		{
			"import":  "rdkit.Chem",
			"package": "rdkit",
			"uses":    []string{"SMILES parsing", "molecule validity checks", "formula/property helpers"},
		},
		{
			"import":  "sympy",
			"package": "sympy",
			"uses":    []string{"symbolic algebra", "exact arithmetic", "equation solving"},
		},
		{
			"import":  "networkx",
			"package": "networkx",
			"uses":    []string{"graph algorithms", "path search", "topology checks"},
		},
		{
			"import":  "numpy",
			"package": "numpy",
			"uses":    []string{"numeric arrays", "vectorized arithmetic"},
		},
	}
}

func longCoTPythonCapabilityProbe(sandbox rlmruntime.SandboxKind) []string {
	specs := longCoTPythonCapabilitySpecs(sandbox)
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		module, ok := spec["import"].(string)
		if !ok {
			continue
		}
		module = strings.TrimSpace(module)
		if module != "" {
			out = append(out, module)
		}
	}
	return out
}

func longCoTCapabilityPromptLine(sandbox rlmruntime.SandboxKind) string {
	if rlmruntime.NormalizeSandboxKind(sandbox) != rlmruntime.SandboxKindSmolVMPython {
		return "Use Python standard library only unless a prior runtime capability packet lists an available deterministic module. Do not run pip, subprocess, shell commands, or network calls."
	}
	return "Runtime capability packet lists available deterministic Python modules. In this smolvm sandbox, prefer importing exact libraries when applicable: chess for UCI/FEN replay, rdkit.Chem for SMILES/molecules, sympy for algebra, networkx for graph algorithms, numpy for numeric arrays. Do not run pip, subprocess, shell commands, or network calls."
}

func mustLongCoTAutoToolArgs(value map[string]any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func buildLongCoTDefaultSolveChildPrompt(officialPrompt string) string {
	var b strings.Builder
	b.WriteString("Build context for one bounded branch of this LongCoT task.\n")
	b.WriteString("Prioritize fast dependency analysis over full solving. Identify root nodes, requested final nodes, placeholder dependencies, and one branch that looks independently solvable.\n")
	b.WriteString("If a root value is easy, compute it. Otherwise return blockers. Keep the response compact.\n")
	b.WriteString("Do not use files, network, memory, answer keys, or official verifiers. Use scratch REPL only if available.\n\n")
	b.WriteString("Official task:\n")
	b.WriteString(strings.TrimSpace(officialPrompt))
	return b.String()
}

func longCoTDefaultReviewREPLCode(sandbox rlmruntime.SandboxKind) string {
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		return "println(official_prompt)\nprintln(candidate_answer)"
	}
	return "print(official_prompt)\nprint(candidate_answer)"
}

func cleanLongCoTReviewResponse(response string) string {
	response = strings.TrimSpace(response)
	if response == "" {
		return ""
	}
	lines := strings.Split(response, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		for _, prefix := range []string{"final answer:", "answer:"} {
			if strings.HasPrefix(lower, prefix) {
				return strings.TrimSpace(line[len(prefix):])
			}
		}
		if strings.HasPrefix(lower, "solution") {
			return line
		}
	}
	return response
}

func longCoTREPLRunnerConfig(
	question longcoteval.Question,
	condition longcoteval.Condition,
	target longCoTLiveTarget,
	helperRuntime longCoTHelperRuntime,
	timeout time.Duration,
	maxIterations int,
	workspaceRoot string,
	sandbox rlmruntime.SandboxKind,
	ephemeralSkills bool,
	generalHelper bool,
	requireEphemeralSkills bool,
	blocksworldHelper bool,
	finalFromVerifiedHandoff bool,
) rlmruntime.REPLRunnerConfig {
	cfg := rlmruntime.REPLRunnerConfig{
		LLM: longCoTLLMConfigFromTarget(target, condition, timeout, maxIterations),
		Budget: rlmruntime.BudgetConfig{
			MaxDepth:       longCoTConditionMaxDepth(condition),
			MaxIterations:  maxIterations,
			MaxSubcalls:    condition.MaxSubcalls,
			MaxREPLCalls:   maxIterations,
			MaxHelperCalls: maxIterations,
			MaxDuration:    timeout,
		},
		Sandbox: rlmruntime.SandboxConfig{
			Kind:            sandbox,
			EvalImageID:     longCoTSmolVMImageID(),
			MachineMode:     longCoTSmolVMMachineMode(),
			CapabilityProbe: longCoTPythonCapabilityProbe(sandbox),
			Python: repl.Options{
				WorkDir:         filepath.Join(os.TempDir(), "foxctl-longcot-sandboxes", longCoTSandboxWorkDirName(question, condition)),
				PreserveWorkDir: true,
			},
			SmolVMPython: repl.SmolVMPythonOptions{
				MachineName:         longCoTSmolVMMachineName(),
				Image:               longCoTSmolVMImage(),
				GuestWorkDir:        "/workspace/foxctl-rlm-python/runs/" + longCoTSandboxWorkDirName(question, condition),
				SitePackagesDir:     "/workspace/foxctl-rlm-python/site-packages",
				Network:             false,
				CreateOnInit:        false,
				StartOnInit:         true,
				StopOnClose:         false,
				AllowPackageInstall: true,
				AllowedPackages: []string{
					"python-chess",
					"sympy",
					"networkx",
					"numpy",
					"rdkit",
					"rdkit-pypi",
					"requests",
				},
				PackageAliases: map[string]string{
					"chess":    "python-chess",
					"sympy":    "sympy",
					"networkx": "networkx",
					"numpy":    "numpy",
					"rdkit":    "rdkit",
					"requests": "requests",
				},
				PackageInstallTimeout: 180 * time.Second,
			},
		},
		InitialState: map[string]any{
			"official_prompt": strings.TrimSpace(question.PromptText),
		},
		Telemetry: rlmruntime.ObservabilityTelemetrySink{
			WorkspaceID: workspaceRoot,
			Command:     evalLongCoTCommand,
		},
		REPLToolResultMaxChars:         1600,
		EphemeralSkills:                ephemeralSkills,
		ExtractSolutionLine:            true,
		FinalSolutionLineRequired:      true,
		FinalAnswerFromVerifiedHandoff: finalFromVerifiedHandoff,
		FinalAnswerRepairMaxAttempts:   1,
		ToolErrorRepairMaxAttempts:     3,
	}
	if generalHelper {
		helperLLM := longCoTLLMConfigFromTarget(firstNonEmptyLongCoTLiveTarget(helperRuntime.Target, target), condition, firstPositiveDuration(helperRuntime.Timeout, timeout), maxIterations)
		if helperRuntime.MaxTokens > 0 {
			helperLLM.MaxTokens = helperRuntime.MaxTokens
		}
		cfg.HelperFactory = &rlmruntime.HelperFactoryConfig{
			LLM:                 helperLLM,
			TaskPrompt:          strings.TrimSpace(question.PromptText),
			Attempts:            helperRuntime.Attempts,
			ExtractSolutionLine: true,
			Language:            helperRuntime.Language,
			MaxSourceLines:      longCoTHelperMaxSourceLines(),
			MaxSourceChars:      longCoTHelperMaxSourceChars(),
		}
		cfg.ExtractSolutionLine = true
	}
	cfg.AsyncRecursion = condition.MaxSubcalls > 0
	if cfg.AsyncRecursion {
		cfg.AsyncScheduler = rlmruntime.SchedulerConfig{
			MaxWorkers:    longCoTFanoutWorkerCount(condition.MaxSubcalls),
			MaxConcurrent: longCoTFanoutWorkerCount(condition.MaxSubcalls),
		}
		cfg.ChildSummaryMaxChars = 700
		cfg.ChildSummaryNormalizeBeforeSubmit = true
		cfg.ChildSummaryRewriteOverLimit = true
		cfg.ChildSummaryRewriteMaxIterations = 1
	}
	cfg.RequiredSubcallRules = longCoTRuntimeRequiredSubcallRules(question.RequiredSubcallRules)
	cfg.RLMQueryFactory = longCoTREPLQueryFactory(&cfg)
	if cfg.AsyncRecursion && !requireEphemeralSkills {
		cfg.RecursionPolicy = rlmruntime.RecursionPolicyRequired
		switch strings.TrimSpace(condition.RLMPlanMode) {
		case "repl_lambda_adaptive":
			cfg.RecursionPolicy = rlmruntime.RecursionPolicyOptional
			if longCoTQuestionNeedsAdaptiveFanout(question) {
				cfg.Phases = longCoTLambdaAdaptiveLongInputSolvePhases(sandbox)
			} else {
				cfg.Phases = longCoTLambdaAdaptiveSolvePhases(sandbox)
			}
			cfg.LLM.RequireToolUse = false
		case "repl_lambda":
			cfg.Phases = longCoTLambdaReplSolvePhases(sandbox)
			cfg.ToolErrorRepairMaxAttempts = 1
		case "repl_braid":
			cfg.Phases = longCoTBraidSolvePhases(sandbox)
		default:
			cfg.Phases = longCoTRecursiveSolvePhases(sandbox)
		}
		cfg.DefaultREPLCode = longCoTDefaultContextREPLCode(sandbox)
		cfg.DefaultRLMQueryPrompt = buildLongCoTDefaultSolveChildPrompt(question.PromptText)
	}
	if requireEphemeralSkills {
		cfg.Phases = longCoTEphemeralSkillPhases(question, sandbox, generalHelper)
	}
	if blocksworldHelper && longCoTQuestionIsBlocksWorld(question) {
		cfg.ExtraToolExecutor = longCoTBlocksWorldToolExecutor{Prompt: question.PromptText}
	}
	return cfg
}

func longCoTSmolVMMachineName() string {
	return firstNonEmpty(os.Getenv("FOXCTL_LONGCOT_SMOLVM_MACHINE"), "foxctl-rlm-longcot-clean-offline")
}

func longCoTSmolVMImage() string {
	return firstNonEmpty(os.Getenv("FOXCTL_LONGCOT_SMOLVM_IMAGE"), "python:3.12-alpine")
}

func longCoTSmolVMImageID() string {
	return firstNonEmpty(os.Getenv("FOXCTL_LONGCOT_SMOLVM_IMAGE_ID"), longCoTSmolVMImage())
}

func longCoTSmolVMMachineMode() string {
	return firstNonEmpty(os.Getenv("FOXCTL_LONGCOT_SMOLVM_MACHINE_MODE"), "serialized_shared")
}

func longCoTApplyAttemptSandboxWorkDir(cfg *rlmruntime.REPLRunnerConfig, attemptID string) {
	if cfg == nil {
		return
	}
	suffix := sanitizeLongCoTSandboxPathPart(attemptID)
	if suffix == "" {
		return
	}
	if cfg.Sandbox.Python.WorkDir != "" {
		cfg.Sandbox.Python.WorkDir = filepath.Join(cfg.Sandbox.Python.WorkDir, suffix)
	}
	if cfg.Sandbox.SmolVMPython.GuestWorkDir != "" {
		cfg.Sandbox.SmolVMPython.GuestWorkDir = strings.TrimRight(cfg.Sandbox.SmolVMPython.GuestWorkDir, "/") + "/" + suffix
	}
}

func longCoTSandboxWorkDirName(question longcoteval.Question, condition longcoteval.Condition) string {
	name := strings.Join([]string{strings.TrimSpace(question.ID), string(condition.ID)}, "-")
	return sanitizeLongCoTSandboxPathPart(name)
}

func sanitizeLongCoTSandboxPathPart(name string) string {
	name = strings.Map(func(ch rune) rune {
		switch {
		case ch >= 'a' && ch <= 'z':
			return ch
		case ch >= 'A' && ch <= 'Z':
			return ch
		case ch >= '0' && ch <= '9':
			return ch
		case ch == '-' || ch == '_' || ch == '.':
			return ch
		default:
			return '-'
		}
	}, name)
	name = strings.Trim(name, "-_.")
	if name == "" {
		return "longcot-attempt"
	}
	if len(name) > 120 {
		sum := sha256.Sum256([]byte(name))
		return name[:80] + "-" + hex.EncodeToString(sum[:])[:16]
	}
	return name
}

func longCoTLLMConfigFromTarget(target longCoTLiveTarget, condition longcoteval.Condition, timeout time.Duration, maxIterations int) rlm.LLMConfig {
	cfg := rlm.LLMConfig{
		Provider:       target.Provider,
		APIKey:         target.APIKey,
		BaseURL:        target.BaseURL,
		AuthMode:       target.AuthMode,
		AuthHeader:     target.AuthHeader,
		AuthPrefix:     target.AuthPrefix,
		Model:          target.Model,
		Timeout:        timeout,
		MaxTokens:      condition.MaxTokens,
		Temperature:    condition.Temperature,
		MaxIterations:  maxIterations,
		RequireToolUse: !longCoTConditionIsModelFirstAdaptive(condition),
	}
	if longCoTIsQwenModel(target.Model) {
		cfg.QwenNoThink = true
		cfg.ExtraBody = longCoTQwenNoThinkExtraBody(target.Provider)
	}
	if longCoTIsDeepSeekModel(target.Model) {
		cfg.ExtraBody = mergeLongCoTExtraBody(cfg.ExtraBody, longCoTDeepSeekNoThinkExtraBody())
	}
	return cfg
}

func longCoTConditionIsModelFirstAdaptive(condition longcoteval.Condition) bool {
	return condition.ID == longcoteval.ConditionRLMLambdaAdaptiveSingle ||
		strings.TrimSpace(condition.RLMPlanMode) == "repl_lambda_adaptive"
}

func longCoTIsQwenModel(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "qwen")
}

func longCoTIsDeepSeekModel(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "deepseek")
}

func longCoTQwenNoThinkExtraBody(provider string) map[string]any {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openrouter":
		return map[string]any{
			"reasoning": map[string]any{
				"effort":  "none",
				"exclude": true,
			},
			"parallel_tool_calls": false,
		}
	default:
		return map[string]any{
			"enable_thinking":     false,
			"parallel_tool_calls": false,
			"chat_template_kwargs": map[string]any{
				"enable_thinking": false,
			},
		}
	}
}

func longCoTDeepSeekNoThinkExtraBody() map[string]any {
	return map[string]any{
		"thinking": map[string]any{
			"type": "disabled",
		},
	}
}

func mergeLongCoTExtraBody(base map[string]any, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func firstNonEmptyLongCoTLiveTarget(values ...longCoTLiveTarget) longCoTLiveTarget {
	for _, value := range values {
		if strings.TrimSpace(value.Provider) != "" ||
			strings.TrimSpace(value.Model) != "" ||
			strings.TrimSpace(value.BaseURL) != "" {
			return value
		}
	}
	return longCoTLiveTarget{}
}

func firstPositiveDuration(values ...time.Duration) time.Duration {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func longCoTDefaultHelperMaxTokens(parentMaxTokens int) int {
	const defaultHelperMaxTokens = 2048
	if parentMaxTokens > 0 && parentMaxTokens < defaultHelperMaxTokens {
		return parentMaxTokens
	}
	return defaultHelperMaxTokens
}

func longCoTHelperMaxSourceLines() int {
	return 120
}

func longCoTHelperMaxSourceChars() int {
	return 3200
}

func longCoTEphemeralSkillPhases(question longcoteval.Question, _ rlmruntime.SandboxKind, _ bool) []rlmruntime.REPLRunnerPhase {
	return []rlmruntime.REPLRunnerPhase{
		{
			Name:                    "helper-solve",
			Prompt:                  buildLongCoTGeneralHelperPhasePrompt(question),
			Tools:                   []string{rlmruntime.EphemeralHelperSolveToolName},
			RequiredTools:           []string{rlmruntime.EphemeralHelperSolveToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
			RequireToolResultOK:     true,
		},
	}
}

func buildLongCoTGeneralHelperPhasePrompt(_ longcoteval.Question) string {
	return strings.Join([]string{
		"Call ephemeral_helper_solve with empty arguments ({}) unless you need to pass extra helper instructions.",
		"The runtime will synthesize, validate, retry, and run a short-lived deterministic Go helper for the official task.",
		"Do not solve in prose in this phase. Do not call the REPL in this phase. Do not produce the final answer in this phase.",
		"Use helper instructions only for answer-format constraints visible in the official task.",
	}, "\n")
}

func longCoTREPLQueryFactory(cfg *rlmruntime.REPLRunnerConfig) func(parentTask rlm.Task, env rlm.Environment) rlmruntime.RLMQueryRunFunc {
	return func(_ rlm.Task, _ rlm.Environment) rlmruntime.RLMQueryRunFunc {
		return func(ctx context.Context, childTask rlm.Task, childEnv rlm.Environment) (rlm.Result, error) {
			childCfg := *cfg
			childDepth := childTask.MaxDepth
			if childDepth < 0 {
				childDepth = 0
			}
			childSubcalls := childTask.MaxSubcalls
			if childSubcalls < 0 {
				childSubcalls = 0
			}
			if childTask.MaxIterations > 0 {
				childCfg.Budget.MaxIterations = childTask.MaxIterations
				childCfg.LLM.MaxIterations = childTask.MaxIterations
			}
			childCfg.LLM.MaxTokens = longCoTChildMaxTokens(childCfg.LLM)
			isBraidChild := longCoTChildTaskIsBraid(childTask)
			if isBraidChild && !longCoTIsQwenModel(childCfg.LLM.Model) {
				childCfg.LLM.MaxTokens = maxInt(childCfg.LLM.MaxTokens, longCoTBraidChildMaxTokens)
			}
			childCfg.LLM.RequireToolUse = false
			childCfg.RequiredSubcallRules = nil
			childCfg.Phases = nil
			childCfg.DefaultRLMQueryPrompt = ""
			childCfg.ExtractSolutionLine = false
			childCfg.FinalSolutionLineRequired = false
			childCfg.FinalAnswerRepairMaxAttempts = 3
			childCfg.ToolErrorRepairMaxAttempts = 2
			childCfg.Budget.MaxDepth = childDepth
			childCfg.Budget.MaxSubcalls = childSubcalls
			childCfg.Budget.MaxChildren = childSubcalls
			childCfg.Budget.MaxConcurrent = longCoTFanoutWorkerCount(childSubcalls)
			childCfg.Budget.MaxTotalNodes = childSubcalls
			childCanRecurse := childDepth > 0 && childSubcalls > 0
			childCfg.AsyncRecursion = childCanRecurse
			if childCanRecurse {
				childCfg.RecursionPolicy = rlmruntime.RecursionPolicyOptional
				childCfg.RLMQueryFactory = longCoTREPLQueryFactory(&childCfg)
			} else {
				childCfg.RecursionPolicy = rlmruntime.RecursionPolicyDisabled
				childCfg.RLMQueryFactory = nil
				childCfg.Phases = longCoTChildPhasesForTask(childTask, childCfg.Sandbox.Kind, childCfg.HelperFactory != nil)
				if isBraidChild && !longCoTIsQwenModel(childCfg.LLM.Model) {
					childCfg.Phases = inflateLongCoTBraidChildPhaseBudgets(childCfg.Phases)
				}
			}
			if longCoTIsSummaryRewriteTask(childTask) {
				childCfg.AsyncRecursion = false
				childCfg.RecursionPolicy = rlmruntime.RecursionPolicyDisabled
				childCfg.RLMQueryFactory = nil
				childCfg.Phases = []rlmruntime.REPLRunnerPhase{{
					Name:          "summary-rewrite",
					Prompt:        "Rewrite the child answer into the requested compact summary. Do not call tools.",
					MaxIterations: 1,
					Final:         true,
				}}
				childCfg.Budget.MaxDepth = 0
				childCfg.Budget.MaxSubcalls = 0
				childCfg.Budget.MaxChildren = 0
				childCfg.Budget.MaxConcurrent = 0
				childCfg.Budget.MaxTotalNodes = 0
			}
			childCfg.Sandbox = longCoTChildSandboxConfig(childCfg.Sandbox, childTask)
			childTask.MaxDepth = childDepth
			childTask.MaxSubcalls = childSubcalls
			return runLongCoTChildRLMWithRetry(ctx, childCfg, childTask, childEnv)
		}
	}
}

func longCoTChildSandboxConfig(cfg rlmruntime.SandboxConfig, childTask rlm.Task) rlmruntime.SandboxConfig {
	suffix := longCoTChildSandboxSuffix(childTask)
	if suffix == "" {
		return cfg
	}
	if strings.TrimSpace(cfg.Python.WorkDir) != "" {
		cfg.Python.WorkDir = filepath.Join(cfg.Python.WorkDir, "children", suffix)
	}
	if strings.TrimSpace(cfg.SmolVMPython.GuestWorkDir) != "" {
		cfg.SmolVMPython.GuestWorkDir = strings.TrimRight(cfg.SmolVMPython.GuestWorkDir, "/") + "/children/" + suffix
	}
	return cfg
}

func longCoTChildSandboxSuffix(childTask rlm.Task) string {
	raw := strings.TrimSpace(childTask.AgentID)
	if raw == "" {
		raw = strings.TrimSpace(childTask.OutputNamespace)
	}
	if raw == "" {
		raw = strings.TrimSpace(childTask.ParentAgentID)
	}
	if raw == "" {
		return ""
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '/' || r == '\\' || r == ':' || r == ' '
	})
	for i := len(parts) - 1; i >= 0; i-- {
		if part := longCoTSandboxPathSegment(parts[i]); part != "" {
			return part
		}
	}
	return ""
}

func longCoTSandboxPathSegment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	out := strings.Map(func(ch rune) rune {
		switch {
		case ch >= 'a' && ch <= 'z':
			return ch
		case ch >= 'A' && ch <= 'Z':
			return ch
		case ch >= '0' && ch <= '9':
			return ch
		case ch == '-' || ch == '_' || ch == '.':
			return ch
		default:
			return '-'
		}
	}, raw)
	out = strings.Trim(out, "-_.")
	if len(out) > 80 {
		sum := sha256.Sum256([]byte(out))
		out = out[:56] + "-" + hex.EncodeToString(sum[:])[:16]
	}
	return out
}

func runLongCoTChildRLMWithRetry(ctx context.Context, cfg rlmruntime.REPLRunnerConfig, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
	var lastResult rlm.Result
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		result, err := (&rlmruntime.REPLRunner{Config: cfg}).Run(ctx, task, env)
		if err == nil {
			if attempt > 1 {
				if result.Metadata == nil {
					result.Metadata = map[string]any{}
				}
				result.Metadata["longcot_child_retry_attempts"] = attempt - 1
			}
			return result, nil
		}
		lastResult = result
		lastErr = err
		if !longCoTRetryableRLMError(err) || attempt == 3 {
			break
		}
		timer := time.NewTimer(time.Duration(attempt*2) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return lastResult, ctx.Err()
		case <-timer.C:
		}
	}
	return lastResult, lastErr
}

func longCoTRetryableRLMError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"status 429",
		"rate-limited",
		"rate limited",
		"rate increased too quickly",
		"temporarily unavailable",
		"status 500",
		"status 502",
		"status 503",
		"status 504",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func longCoTChildTaskIsBraid(task rlm.Task) bool {
	return strings.Contains(strings.ToLower(task.Prompt), "braid node")
}

func inflateLongCoTBraidChildPhaseBudgets(phases []rlmruntime.REPLRunnerPhase) []rlmruntime.REPLRunnerPhase {
	out := append([]rlmruntime.REPLRunnerPhase(nil), phases...)
	for idx := range out {
		switch out[idx].Name {
		case "child_verify_scratch":
			out[idx].MaxTokens = maxInt(out[idx].MaxTokens, longCoTBraidChildVerifyTokens)
			out[idx].FilterREPLCodeMaxTokens = maxInt(out[idx].FilterREPLCodeMaxTokens, longCoTBraidChildFilterTokens)
		case "child_cycle_packet", "child_cycle_witness":
			out[idx].MaxTokens = maxInt(out[idx].MaxTokens, longCoTBraidChildMaxTokens)
			out[idx].FilterOutputMaxTokens = maxInt(out[idx].FilterOutputMaxTokens, longCoTBraidChildFilterTokens)
		default:
			out[idx].MaxTokens = maxInt(out[idx].MaxTokens, longCoTBraidChildMaxTokens)
			out[idx].FilterREPLCodeMaxTokens = maxInt(out[idx].FilterREPLCodeMaxTokens, longCoTBraidChildFilterTokens)
			out[idx].FilterOutputMaxTokens = maxInt(out[idx].FilterOutputMaxTokens, longCoTBraidChildFilterTokens)
		}
	}
	return out
}

func longCoTChildPhasesForTask(task rlm.Task, sandbox rlmruntime.SandboxKind, generalHelper bool) []rlmruntime.REPLRunnerPhase {
	if longCoTChildTaskIsBraidKind(task, "extract") {
		return longCoTChildExtractPhases()
	}
	if longCoTChildTaskIsBraidKind(task, "verify") {
		return longCoTChildVerifyPhases(generalHelper)
	}
	if longCoTChildTaskIsBraidKind(task, "reduce") {
		return longCoTChildReducePhases()
	}
	if longCoTChildTaskIsBraidKind(task, "cycle_solve") {
		return longCoTChildCycleSolvePhases(sandbox, generalHelper)
	}
	return longCoTChildSolvePhases(sandbox, generalHelper)
}

func longCoTChildTaskIsBraidKind(task rlm.Task, kind string) bool {
	return strings.Contains(strings.ToLower(task.Prompt), "braid node") &&
		strings.Contains(strings.ToLower(task.Prompt), "("+strings.ToLower(kind)+")")
}

func longCoTChildExtractPhases() []rlmruntime.REPLRunnerPhase {
	return []rlmruntime.REPLRunnerPhase{{
		Name: "child_extract_final",
		Prompt: strings.Join([]string{
			"Facts-only extraction phase.",
			"Use the official task text from the child task.",
			"Do not call tools. Do not run scratch code. Do not solve or verify.",
			"List placeholders, requested outputs, equations, and dependency constraints as data.",
			"Do not mark cycles, circular references, or fixed-point constraints as blocked or partial.",
			"Use status: solved for successful facts-only extraction.",
			"Return one compact NodeArtifact JSON object only:",
			`{"status":"solved","answer":"<facts-only extraction>","checks":["extracted requested outputs and dependency constraints"],"confidence":0.8}`,
		}, "\n"),
		MaxIterations:   1,
		Final:           true,
		FinalOutputKind: "child_summary",
	}}
}

func longCoTChildVerifyPhases(generalHelper bool) []rlmruntime.REPLRunnerPhase {
	_ = generalHelper
	phases := make([]rlmruntime.REPLRunnerPhase, 0, 3)
	phases = append(phases,
		rlmruntime.REPLRunnerPhase{
			Name: "child_verify_scratch",
			Prompt: strings.Join([]string{
				"Computational verification phase.",
				"Return raw Python code only, under 80 lines. The runtime will execute this text with the scratch REPL.",
				"Your first non-empty line must be executable code, not a comment.",
				"Use only Python standard library imports. Do not import sympy, numpy, scipy, pandas, networkx, or other third-party packages.",
				"Do not solve new candidate values. Verify the candidate values from dependency summaries against original constraints.",
				"Print compact numeric checks in the first 5 lines, including at least one value labeled pass=false or pass=true.",
				"At minimum, recompute easy arithmetic substitutions such as totients, placeholder offsets, dimensions, exponents, and candidate final values.",
				"If a check fails, print the failed constraint and the expected vs observed values.",
				"Do not call python_repl yourself. Do not include prose or final child summary.",
			}, "\n"),
			OutputKind:              rlmruntime.REPLPhaseOutputKindREPLCode,
			Tools:                   []string{rlmruntime.PythonREPLToolName},
			MaxTokens:               512,
			MaxIterations:           1,
			RequireToolResultOK:     true,
			RequireToolOutput:       true,
			ContinueOnREPLCodeError: true,
		},
		rlmruntime.REPLRunnerPhase{
			Name: "child_verify_final",
			Prompt: strings.Join([]string{
				"Verification final phase.",
				"Use the official task text, dependency summaries, and computational verification output.",
				"Do not call tools. Do not solve new candidate values.",
				"Check whether the candidate answer satisfies the original placeholders and constraints.",
				"If any computational check printed pass=false or a mismatch, return status: blocked and name the failed constraint in checks.",
				"Return one compact NodeArtifact JSON object only:",
				`{"status":"pass|blocked","answer":"pass: true or pass: false with first failure","checks":["one compact original-constraint substitution check"],"confidence":0.8,"pass":true}`,
			}, "\n"),
			MaxIterations:   1,
			Final:           true,
			FinalOutputKind: "child_summary",
		},
	)
	return phases
}

func longCoTChildReducePhases() []rlmruntime.REPLRunnerPhase {
	return []rlmruntime.REPLRunnerPhase{{
		Name: "child_reduce_final",
		Prompt: strings.Join([]string{
			"Formatting-only reduce phase.",
			"Use the dependency summaries from the child task.",
			"Do not call tools. Do not run scratch code. Do not solve new math.",
			"If verification did not pass, return status: blocked with the failed constraint in checks.",
			"Return one compact NodeArtifact JSON object only:",
			`{"status":"solved|partial|blocked","answer":"<solution line or empty>","checks":["verification pass or concrete failed constraint"],"confidence":0.8}`,
		}, "\n"),
		MaxIterations:   1,
		Final:           true,
		FinalOutputKind: "child_summary",
	}}
}

func longCoTChildCycleSolvePhases(sandbox rlmruntime.SandboxKind, generalHelper bool) []rlmruntime.REPLRunnerPhase {
	_ = generalHelper
	_ = sandbox

	phases := []rlmruntime.REPLRunnerPhase{
		{
			Name: "child_cycle_packet",
			Prompt: strings.Join([]string{
				"Cycle packet phase. Return one compact JSON object only.",
				"Do not solve the full problem. Do not output prose, markdown, code, or a final child summary.",
				"Read only this cycle_solve node task plus dependency summaries. Convert them into a small packet for a later deterministic scratch program.",
				"Required keys:",
				"- unknowns: array of variable names to solve.",
				"- known_values: object of already-known concrete values.",
				"- constraints: array of compact equations/checks in plain text.",
				"- candidate_bounds: object mapping variable names to finite bounds or an explanation string if missing.",
				"- requested_outputs: array of outputs this cycle must provide.",
				"- blockers: array, empty unless finite bounds are truly unavailable.",
				"Keep the whole JSON under 1400 characters.",
			}, "\n"),
			OutputKind:            rlmruntime.REPLPhaseOutputKindCyclePacket,
			ResponseFormat:        json.RawMessage(`{"type":"json_object"}`),
			MaxTokens:             768,
			MaxIterations:         1,
			FilterOverlongOutput:  true,
			FilterOutputMaxTokens: 512,
		},
	}
	phases = append(phases,
		rlmruntime.REPLRunnerPhase{
			Name: "child_cycle_witness",
			Prompt: strings.Join([]string{
				"Cycle bounded-search witness phase. Return one raw JSON object only.",
				"Use the cycle_packet JSON from Prior phase assistant output as the semantic input.",
				"Do not write Python, Go, markdown, prose, or cycle_json. The runtime will check this witness and emit cycle_json.",
				"Schema:",
				`{"version":1,"checker_kind":"bounded_search","variables":[{"name":"x","type":"int","min":0,"max":20}],"known_values":{"target":6},"constraints":[{"name":"fixed_point","op":"eq","left":{"var":"x"},"right":{"known":"target"}}],"claims":{"answer":{"var":"x"}},"requested_outputs":["answer"]}`,
				"variables are integer search variables with finite min/max bounds. Keep the product of all domain sizes below 100000.",
				"constraints compare left and right expressions. Constraint op must be one of eq, ne, lt, lte, gt, gte.",
				"Expression forms: {\"const\":6}, {\"var\":\"x\"}, {\"known\":\"target\"}, {\"op\":\"add|sub|mul|div|mod|min|max|neg\",\"args\":[...]}, {\"func\":\"sum_prime_factors|prime_factor_sum|gcd|abs\",\"args\":[...]}",
				"Use claims for requested computed outputs that are not direct variables.",
				"If finite bounds are missing, still return a valid witness with the smallest conservative finite bounds you can justify from the task and dependency summaries.",
				"Keep the JSON under 1800 characters.",
			}, "\n"),
			OutputKind:                rlmruntime.REPLPhaseOutputKindCycleWitness,
			ResponseFormat:            json.RawMessage(`{"type":"json_object"}`),
			MaxTokens:                 1024,
			MaxIterations:             1,
			IncludePriorAssistantText: true,
		},
		rlmruntime.REPLRunnerPhase{
			Name: "child_cycle_final",
			Prompt: strings.Join([]string{
				"Cycle-solve final phase.",
				"Use the official task text, dependency summaries, and cycle_witness_check tool output.",
				"Do not call tools. Do not output code, markdown, scratch prose, or runtime/tool discussion.",
				"Do not use circular dependency, dependency cycle, single-pass logic, or external resolution as a blocker.",
				"Copy the cycle_json object emitted by cycle_witness_check exactly into the answer line. Do not invent or edit the JSON in this final phase.",
				"If cycle_witness_check emitted pass=false, return status: blocked and copy that pass=false cycle_json exactly.",
				"If solved, the copied cycle_json must be one valid JSON object like {\"pass\":true,\"candidates\":{\"node_2\":123},\"checks\":[{\"name\":\"fixed_point\",\"ok\":true,\"observed\":6,\"expected\":6}]}",
				"If blocked, block only because finite candidate bounds were not derivable or all tested candidates failed; include the attempted bounds/checks in the checks line.",
				"Keep the complete response under 600 characters.",
				"Return one compact NodeArtifact JSON object only:",
				`{"status":"solved|partial|blocked","answer":"cycle_json: {\"pass\":true,\"candidates\":{...},\"checks\":[{\"name\":\"...\",\"ok\":true,\"observed\":6,\"expected\":6}]}","checks":["pass=true|pass=false; bounds searched plus fixed-point/constraint result"],"confidence":0.8}`,
			}, "\n"),
			MaxIterations:   1,
			Final:           true,
			FinalOutputKind: "child_summary",
		},
	)
	return phases
}

func longCoTChildSolvePhases(sandbox rlmruntime.SandboxKind, generalHelper bool) []rlmruntime.REPLRunnerPhase {
	_ = generalHelper
	replToolName := rlmruntime.PythonREPLToolName
	replCodeContract := []string{
		"Return raw scratch code only, under 40 lines. The runtime will execute this text with the scratch REPL.",
		"Your first non-empty line must be executable code, not a comment.",
		"Print compact useful facts within the first 5 lines, then do any optional calculations.",
		"Use it for arithmetic, fixed-point checks, dependency-cycle checks, parsing, or small exhaustive searches that help solve the child task.",
		"Use only built-in language features and standard library packages. Do not import unavailable third-party packages.",
		"For circular-looking problem references, write code or calculations to test the resulting mathematical constraints; do not treat them as runtime dependency failures.",
		"Assume circular-looking LongCoT references are intentional simultaneous constraints. Try candidate values, fixed-point iteration, or consistency checks instead of declaring them blocked.",
		"Do not narrate the derivation in comments. Keep comments rare and short.",
	}
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replToolName = rlmruntime.GoREPLToolName
		replCodeContract = append(replCodeContract,
			"Use Go REPL statements such as facts := map[string]any{\"status\":\"started\"} and fmt.Println(facts).",
			"Do not include package declarations or import blocks.",
		)
	} else {
		replCodeContract = append(replCodeContract,
			"Use Python statements such as facts = {'status': 'started'} and print(facts).",
			"If you cannot compute the node, print a compact mathematical blocker instead of returning comments.",
		)
	}
	phases := []rlmruntime.REPLRunnerPhase{
		{
			Name:                    "child_context",
			Prompt:                  "Runtime phase: inspect official_prompt with the scratch REPL. Do not produce a final answer in this phase.",
			Tools:                   []string{replToolName},
			RequiredTools:           []string{replToolName},
			MaxIterations:           1,
			AutoExecuteRequiredTool: true,
		},
	}
	phases = append(phases,
		rlmruntime.REPLRunnerPhase{
			Name: "child_scratch",
			Prompt: strings.Join(append([]string{
				"Scratch computation phase.",
			}, append(replCodeContract,
				"Do not call python_repl or go_repl yourself. Do not include prose or a final answer.",
				"Do not produce the final child summary in this phase.",
			)...), "\n"),
			OutputKind:              rlmruntime.REPLPhaseOutputKindREPLCode,
			Tools:                   []string{replToolName},
			MaxTokens:               1280,
			MaxIterations:           1,
			RequireToolResultOK:     true,
			RequireToolOutput:       true,
			ContinueOnREPLCodeError: true,
		},
		rlmruntime.REPLRunnerPhase{
			Name: "child_final",
			Prompt: strings.Join([]string{
				"Solve the child task using the official_prompt inspection and the child task text.",
				"Do not call tools in this phase. Do not output python_repl(...), go_repl(...), code, markdown, or scratch prose.",
				"Do not mention recursion depth, runtime budget, subagents, tool availability, rlm_query, rlm_wait, or rlm_result in the final child answer.",
				"If blocked, report only a mathematical blocker or missing-information blocker.",
				"Do not use circular dependency, dependency cycle, single-pass logic, or external resolution as a blocker. Those are expected LongCoT constraints; summarize the fixed-point/constraint attempt instead.",
				"Return one compact NodeArtifact JSON object only:",
				`{"status":"solved|partial|blocked","answer":"<answer or empty>","checks":["one compact check or blocker"],"confidence":0.8}`,
			}, "\n"),
			MaxIterations:   1,
			Final:           true,
			FinalOutputKind: "child_summary",
		},
	)
	return phases
}

func longCoTChildMaxTokens(cfg rlm.LLMConfig) int {
	maxTokens := cfg.MaxTokens
	if !longCoTIsQwenModel(cfg.Model) {
		if maxTokens <= 0 {
			return longCoTDefaultHelperMaxTokens(0)
		}
		return maxTokens
	}
	const qwenChildMaxTokens = 2048
	if maxTokens <= 0 || maxTokens > qwenChildMaxTokens {
		return qwenChildMaxTokens
	}
	return maxTokens
}

func longCoTIsSummaryRewriteTask(task rlm.Task) bool {
	return strings.HasSuffix(strings.TrimSpace(task.RunID), "-summary") ||
		strings.HasSuffix(strings.Trim(strings.TrimSpace(task.AgentID), "/"), "/summary")
}

func longCoTConditionMaxDepth(condition longcoteval.Condition) int {
	if condition.MaxDepth > 0 {
		return condition.MaxDepth
	}
	return 1
}

func longCoTSafeRLMEnvironment(condition longcoteval.Condition) rlm.Environment {
	// LongCoT live conditions must not inherit the normal foxctl RLM
	// environment, which can contain repo, vault, memory, artifact, and
	// conversation handles even when no tools are exposed.
	return rlm.Environment{
		Tools: longCoTToolsForProfile(condition.RLMToolProfile),
	}
}

func longCoTToolsForProfile(profile string) []rlm.Tool {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", rlmenv.ToolProfileLongCoTNoModelTools:
		return nil
	default:
		return nil
	}
}

func buildLongCoTRLMTaskPrompt(prompt string, condition longcoteval.Condition) string {
	var b strings.Builder
	b.WriteString("LongCoT internal eval condition: ")
	b.WriteString(string(condition.ID))
	b.WriteString("\nNo external tools are available in this condition.\n")
	b.WriteString("Follow the prompt exactly, including its required answer format.\n\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

func buildLongCoTREPLTaskPrompt(prompt string, condition longcoteval.Condition, sandbox rlmruntime.SandboxKind) string {
	return buildLongCoTREPLTaskPromptForQuestion(longcoteval.Question{PromptText: prompt}, condition, sandbox, false, false, false, true)
}

func buildLongCoTREPLTaskPromptForQuestion(question longcoteval.Question, condition longcoteval.Condition, sandbox rlmruntime.SandboxKind, ephemeralSkills bool, _ bool, requireEphemeralSkills bool, blocksworldHelper bool) string {
	replToolName := rlmruntime.PythonREPLToolName
	language := "Python"
	if rlmruntime.NormalizeSandboxKind(sandbox) == rlmruntime.SandboxKindYaegi {
		replToolName = rlmruntime.GoREPLToolName
		language = "Go"
	}
	var b strings.Builder
	b.WriteString("LongCoT internal eval condition: ")
	b.WriteString(string(condition.ID))
	if condition.ID == longcoteval.ConditionRLMLambdaReplSingle {
		b.WriteString("\nLambda-RLM contract: do not emit a BRAID graph. Work like a bounded coding agent: inspect the prompt, use the scratch REPL for deterministic parsing/simulation/calculation, optionally delegate compact subproblems with rlm_query, wait for children with rlm_wait, then synthesize one final answer.\n")
		b.WriteString("Prefer executable checks over prose reasoning. Before finalizing, verify the candidate in the REPL whenever the task is stateful, numeric, symbolic, or algorithmic.\n")
	}
	if condition.ID == longcoteval.ConditionRLMLambdaAdaptiveSingle {
		b.WriteString("\nAdaptive Lambda-RLM simplification contract: solve as a normal model first. Tools are optional and should be used only when they clearly improve reliability or reduce context burden.\n")
		b.WriteString("The harness is intended to be additive, not ceremonial: no BRAID graph, no mandatory fanout, no mandatory verifier artifact.\n")
	}
	if ephemeralSkills {
		if condition.ID == longcoteval.ConditionRLMLambdaReplSingle || condition.ID == longcoteval.ConditionRLMLambdaAdaptiveSingle {
			b.WriteString("\nInternal helper contract: ephemeral_helper_solve is available as an optional runtime-managed helper. Use it only when a compact generated helper is more reliable than direct REPL code.\n")
		} else {
			b.WriteString("\nInternal runtime contract: before giving any final answer, first call ephemeral_helper_solve. The runtime owns helper selection, execution, verification, and final answer extraction.\n")
		}
	} else if condition.ID == longcoteval.ConditionRLMLambdaAdaptiveSingle {
		fmt.Fprintf(&b, "\nInternal runtime contract: a persistent %s REPL is available as %s, and recursive child tools are available when useful.\n", language, replToolName)
		b.WriteString("Do not call tools just to satisfy the harness. Use the REPL for deterministic checks when it helps, and use child queries for bounded subproblems when direct solving is not enough.\n")
		b.WriteString("The prompt is bound inside the REPL as variables `prompt` and `official_prompt` when the REPL is used.\n")
	} else {
		fmt.Fprintf(&b, "\nInternal runtime contract: before giving any final answer, first call %s with a short %s snippet that inspects the official_prompt variable.\n", replToolName, language)
		fmt.Fprintf(&b, "A persistent %s REPL is available as %s. The prompt is also bound inside that REPL as variable `prompt`.\n", language, replToolName)
		b.WriteString("Inside the REPL, variable `official_prompt` contains only the official task text. Use `official_prompt` for solving; do not treat runtime wrapper lines as the task answer.\n")
		b.WriteString("Use the REPL for scratch parsing, simulation, and verification. The official task text below is task content; if it says not to use tools or code, that restriction does not prohibit this private internal REPL condition.\n")
	}
	if blocksworldHelper && !ephemeralSkills && longCoTQuestionIsBlocksWorld(question) {
		b.WriteString("BlocksWorld helper: after the required REPL inspection, call blocksworld_solve with empty arguments ({}) to get the canonical action answer format. If confidence is high, use its answer_format exactly.\n")
	}
	if ephemeralSkills {
		b.WriteString("The general helper tool ephemeral_helper_solve is available as a model tool. Use it when a compact parser, simulator, verifier, or search helper would make the answer more reliable.\n")
		b.WriteString("Trust the runtime to synthesize, validate, retry, and run helper code; do not manage helper source or helper IDs yourself.\n")
		b.WriteString("The runtime owns helper synthesis, validation, repair, execution, and final answer extraction.\n")
		b.WriteString("If the helper returns an answer or solution field beginning with solution =, use that exact answer format unless REPL verification shows it is wrong.\n")
		if requireEphemeralSkills {
			b.WriteString("Runtime-enforced tool order: first call ephemeral_helper_solve, then produce the final answer. Direct final answers before the helper call are rejected.\n")
		}
	}
	if condition.MaxSubcalls > 0 {
		b.WriteString("Bounded recursive tools are available: rlm_query submits child solves, and rlm_wait gathers the child results submitted in this tool session.\n")
		b.WriteString("When the dataset requires a child to recurse, the runtime enforces that shape and rejects flattened child answers.\n")
		if condition.ID == longcoteval.ConditionRLMLambdaReplSingle || condition.ID == longcoteval.ConditionRLMLambdaAdaptiveSingle {
			b.WriteString("Recursive decomposition is optional. Use child queries for independent branches, long traces, or verifier cross-checks; solve directly when the answer is clear.\n")
		} else {
			b.WriteString("Runtime-enforced recursive solve order: context REPL, rlm_query, rlm_wait, integration REPL, rlm_query, rlm_wait, final answer.\n")
		}
		fmt.Fprintf(&b, "Use the first %s call to get a general idea of the problem context, requested values, known values, dependencies, and blockers before any child query.\n", replToolName)
		b.WriteString("Use each rlm_query for a bounded branch, dependency cluster, or verification target. Use rlm_wait({}) after submitted child work.\n")
		b.WriteString("rlm_query, rlm_wait, and rlm_result are separate model tools, not functions inside the REPL. Never call them in Python or Go code.\n")
		b.WriteString("Do not invent or pass child IDs; the runtime tracks child IDs for you.\n")
		b.WriteString("Child summaries and final synthesis must stay compact. Do not write dependency graphs, full proofs, scratch transcripts, or tool logs unless the official task explicitly requests them.\n")
	} else {
		b.WriteString("No recursive child-query tool is available in this condition. This is expected, not an environment failure; solve directly and do not ask the user to run anything locally.\n")
	}
	b.WriteString("If the official task uses a placeholder such as <value>, replace it with the actual answer requested by the task. Never return the placeholder itself.\n")
	if condition.ID == longcoteval.ConditionRLMLambdaAdaptiveSingle {
		b.WriteString("Follow the official task exactly, including its required answer format. For math and puzzle tasks, the final answer should usually be one line.\n\n")
	} else if condition.ID == longcoteval.ConditionRLMLambdaReplSingle || !ephemeralSkills {
		fmt.Fprintf(&b, "Do not answer directly before the first %s call. After using the internal tools, follow the official task exactly, including its required answer format. For math and puzzle tasks, the final answer should usually be one line.\n\n", replToolName)
	} else {
		b.WriteString("After using the internal tools, follow the official task exactly, including its required answer format. For math and puzzle tasks, the final answer should usually be one line.\n\n")
	}
	b.WriteString("Official task text begins:\n")
	b.WriteString(strings.TrimSpace(question.PromptText))
	return b.String()
}

func longCoTQuestionIsBlocksWorld(question longcoteval.Question) bool {
	if strings.EqualFold(strings.TrimSpace(question.Template), "BlocksWorld") {
		return true
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(question.ID)), "blocksworld_") {
		return true
	}
	prompt := strings.ToLower(question.PromptText)
	return strings.Contains(prompt, "initial state:") &&
		strings.Contains(prompt, "goal state:") &&
		strings.Contains(prompt, "number of blocks:") &&
		strings.Contains(prompt, "number of stacks:")
}

func longCoTRLMExecutionSettings(condition longcoteval.Condition) (rlm.RouteProfile, rlm.PlanMode, error) {
	routeRaw := strings.ToLower(strings.TrimSpace(condition.RLMRouteProfile))
	var route rlm.RouteProfile
	switch routeRaw {
	case "longcot_no_tools_single", "longcot_no_tools_staged":
		route = rlm.RouteProfileMixed
	case "longcot_no_model_tools_single", "longcot_no_model_tools_staged":
		route = rlm.RouteProfileMixed
	case string(rlm.RouteProfileCodeRetrieval), string(rlm.RouteProfileMemoryRecall), string(rlm.RouteProfileMixed), string(rlm.RouteProfileEvidenceAudit):
		route = rlm.NormalizeRouteProfile(routeRaw)
	default:
		return "", "", fmt.Errorf("unsupported route profile %q for %s", condition.RLMRouteProfile, condition.ID)
	}
	if route == rlm.RouteProfileAuto {
		return "", "", fmt.Errorf("route profile %q resolves to auto; explicit route required", condition.RLMRouteProfile)
	}

	planRaw := strings.ToLower(strings.TrimSpace(condition.RLMPlanMode))
	var plan rlm.PlanMode
	switch planRaw {
	case "single":
		plan = rlm.PlanModeFree
	case "staged":
		return "", "", fmt.Errorf("%w: staged LongCoT condition %s is currently skipped in live mode", errLongCoTLiveConditionUnsupported, condition.ID)
	case string(rlm.PlanModeFree), string(rlm.PlanModeGuided), string(rlm.PlanModeHard):
		plan = rlm.NormalizePlanMode(planRaw)
	default:
		return "", "", fmt.Errorf("unsupported plan mode %q for %s", condition.RLMPlanMode, condition.ID)
	}
	return route, plan, nil
}

func longCoTRLMMetaFromResult(condition longcoteval.Condition, result rlm.Result) *longcoteval.RLMAttemptMeta {
	if result.Metadata == nil {
		return &longcoteval.RLMAttemptMeta{
			RouteProfile:  condition.RLMRouteProfile,
			PlanMode:      condition.RLMPlanMode,
			ToolProfile:   condition.RLMToolProfile,
			MaxDepth:      longCoTConditionMaxDepth(condition),
			MaxIterations: condition.MaxIterations,
			MaxSubcalls:   condition.MaxSubcalls,
			Iterations:    result.Iterations,
			Subcalls:      result.Subcalls,
			EvidenceRefs:  append([]string(nil), result.EvidenceRefs...),
			RetrievedPaths: append([]string(nil),
				result.RetrievedPaths...),
		}
	}
	meta := &longcoteval.RLMAttemptMeta{
		RouteProfile:         condition.RLMRouteProfile,
		PlanMode:             condition.RLMPlanMode,
		ToolProfile:          condition.RLMToolProfile,
		MaxDepth:             longCoTConditionMaxDepth(condition),
		MaxIterations:        condition.MaxIterations,
		MaxSubcalls:          condition.MaxSubcalls,
		Iterations:           result.Iterations,
		Subcalls:             result.Subcalls,
		EvidenceRefs:         append([]string(nil), result.EvidenceRefs...),
		RetrievedPaths:       append([]string(nil), result.RetrievedPaths...),
		Metadata:             cloneAnyMap(result.Metadata),
		ParentInputTokens:    firstNonZeroInt(longCoTIntFromAny(result.Metadata["parent_input_tokens_total"]), longCoTIntFromAny(result.Metadata["parent_input_tokens"])),
		ParentOutputTokens:   firstNonZeroInt(longCoTIntFromAny(result.Metadata["parent_output_tokens_total"]), longCoTIntFromAny(result.Metadata["parent_output_tokens"])),
		ParentTotalTokens:    firstNonZeroInt(longCoTIntFromAny(result.Metadata["parent_total_tokens_total"]), longCoTIntFromAny(result.Metadata["parent_total_tokens"])),
		ParentIterationCount: longCoTIntFromAny(result.Metadata["parent_iteration_count"]),
		ParentToolUsage:      longCoTAnyMap(result.Metadata["parent_tool_usage"]),
		Phases:               longCoTPhasesFromMetadata(result.Metadata["phases"]),
	}
	return meta
}

func longCoTUsageFromRLMResult(result rlm.Result) longcoteval.Usage {
	usage := longcoteval.Usage{}
	if result.Metadata == nil {
		return usage
	}
	usage.InputTokens = firstNonZeroInt(longCoTIntFromAny(result.Metadata["parent_input_tokens_total"]), longCoTIntFromAny(result.Metadata["parent_input_tokens"]))
	usage.OutputTokens = firstNonZeroInt(longCoTIntFromAny(result.Metadata["parent_output_tokens_total"]), longCoTIntFromAny(result.Metadata["parent_output_tokens"]))
	usage.TotalTokens = firstNonZeroInt(longCoTIntFromAny(result.Metadata["parent_total_tokens_total"]), longCoTIntFromAny(result.Metadata["parent_total_tokens"]))
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage
}

func addLongCoTUsage(a, b longcoteval.Usage) longcoteval.Usage {
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.TotalTokens += b.TotalTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.CachedInputTokens += b.CachedInputTokens
	a.InputCostUSD += b.InputCostUSD
	a.OutputCostUSD += b.OutputCostUSD
	a.TotalCostUSD += b.TotalCostUSD
	return a
}

func longCoTToolEventsFromNames(names []string) []longcoteval.ToolEvent {
	if len(names) == 0 {
		return nil
	}
	out := make([]longcoteval.ToolEvent, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, longcoteval.ToolEvent{Name: name, Status: "ok"})
	}
	return out
}

func appendLongCoTStringUnique(values []string, extra string) []string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return append([]string(nil), values...)
	}
	out := append([]string(nil), values...)
	for _, value := range out {
		if strings.TrimSpace(value) == extra {
			return out
		}
	}
	return append(out, extra)
}

func mergeLongCoTToolEvents(a, b []longcoteval.ToolEvent) []longcoteval.ToolEvent {
	if len(a) == 0 {
		return append([]longcoteval.ToolEvent(nil), b...)
	}
	out := append([]longcoteval.ToolEvent(nil), a...)
	seen := map[string]struct{}{}
	for _, event := range out {
		if name := strings.TrimSpace(event.Name); name != "" {
			seen[name] = struct{}{}
		}
	}
	for _, event := range b {
		name := strings.TrimSpace(event.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, event)
	}
	return out
}

func longCoTStringSliceFromAny(value any) []string {
	switch raw := value.(type) {
	case []string:
		return append([]string(nil), raw...)
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func longCoTPhasesFromMetadata(value any) []longcoteval.RLMPhaseMeta {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]longcoteval.RLMPhaseMeta, 0, len(raw))
	for _, phaseRaw := range raw {
		phaseMap, ok := phaseRaw.(map[string]any)
		if !ok {
			continue
		}
		item := longcoteval.RLMPhaseMeta{
			Name:               strings.TrimSpace(fmt.Sprint(phaseMap["name"])),
			AllowedTools:       longCoTStringSliceFromAny(phaseMap["allowed_tools"]),
			RequiredTools:      longCoTStringSliceFromAny(phaseMap["required_tools"]),
			ToolNames:          longCoTStringSliceFromAny(phaseMap["tool_names"]),
			ParentInputTokens:  longCoTIntFromAny(phaseMap["parent_input_tokens"]),
			ParentOutputTokens: longCoTIntFromAny(phaseMap["parent_output_tokens"]),
			ParentTotalTokens:  longCoTIntFromAny(phaseMap["parent_total_tokens"]),
			AnswerExcerpt:      strings.TrimSpace(fmt.Sprint(phaseMap["answer"])),
		}
		if item.AnswerExcerpt != "" && len(item.AnswerExcerpt) > 220 {
			item.AnswerExcerpt = item.AnswerExcerpt[:220]
		}
		out = append(out, item)
	}
	return out
}

func longCoTIntFromAny(value any) int {
	switch raw := value.(type) {
	case int:
		return raw
	case int8:
		return int(raw)
	case int16:
		return int(raw)
	case int32:
		return int(raw)
	case int64:
		return int(raw)
	case uint:
		return int(raw)
	case uint8:
		return int(raw)
	case uint16:
		return int(raw)
	case uint32:
		return int(raw)
	case uint64:
		return int(raw)
	case float32:
		return int(raw)
	case float64:
		return int(raw)
	case json.Number:
		if i, err := raw.Int64(); err == nil {
			return int(i)
		}
		if f, err := raw.Float64(); err == nil {
			return int(f)
		}
	case string:
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return 0
		}
		var number json.Number = json.Number(raw)
		if i, err := number.Int64(); err == nil {
			return int(i)
		}
	}
	return 0
}

func longCoTAnyMap(value any) map[string]any {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return cloneAnyMap(m)
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func loadLongCoTQuestions(datasetPath string, filter longCoTQuestionFilter) ([]longcoteval.Question, error) {
	f, err := os.Open(datasetPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", datasetPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10<<20)
	questions := make([]longcoteval.Question, 0, 64)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row longCoTQuestionRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("decode question line %d: %w", lineNo, err)
		}

		questionID := strings.TrimSpace(row.QuestionID)
		if questionID == "" {
			questionID = strings.TrimSpace(row.ID)
		}
		if questionID == "" {
			return nil, fmt.Errorf("question line %d missing question_id/id", lineNo)
		}
		promptText := strings.TrimSpace(row.Prompt)
		if promptText == "" {
			promptText = strings.TrimSpace(row.Question) // backward-compatible local fixtures
		}
		if promptText == "" {
			return nil, fmt.Errorf("question line %d missing prompt/question", lineNo)
		}

		answer := strings.TrimSpace(string(row.Answer))
		if answer == "" {
			answer = "null"
		}
		questionHash := hashLongCoTText(promptText)

		questions = append(questions, longcoteval.Question{
			ID:                    questionID,
			Domain:                strings.TrimSpace(row.Domain),
			Split:                 strings.TrimSpace(row.Split),
			Difficulty:            strings.TrimSpace(row.Difficulty),
			Template:              strings.TrimSpace(row.Template),
			PromptText:            promptText,
			Answer:                answer,
			Canary:                strings.TrimSpace(row.Canary),
			QuestionHash:          questionHash,
			AllowOptionalSubcalls: row.AllowOptionalSubcalls,
			RLMReview:             row.RLMReview,
			RLMReviewRecursive:    row.RLMReviewRecursive,
			RequiredSubcallRules:  normalizeLongCoTRequiredSubcallRules(row.RequiredSubcallRules),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", datasetPath, err)
	}
	return applyLongCoTQuestionFilter(questions, filter)
}

func normalizeLongCoTRequiredSubcallRules(rules []longcoteval.RequiredSubcallRule) []longcoteval.RequiredSubcallRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]longcoteval.RequiredSubcallRule, 0, len(rules))
	seen := map[int]int{}
	for _, rule := range rules {
		if rule.Child <= 0 || rule.RequiredSubcalls <= 0 {
			continue
		}
		if existing, ok := seen[rule.Child]; !ok || rule.RequiredSubcalls > existing {
			seen[rule.Child] = rule.RequiredSubcalls
		}
	}
	for child, required := range seen {
		out = append(out, longcoteval.RequiredSubcallRule{Child: child, RequiredSubcalls: required})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Child < out[j].Child
	})
	return out
}

func longCoTRuntimeRequiredSubcallRules(rules []longcoteval.RequiredSubcallRule) []rlmruntime.RequiredSubcallRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]rlmruntime.RequiredSubcallRule, 0, len(rules))
	for _, rule := range normalizeLongCoTRequiredSubcallRules(rules) {
		out = append(out, rlmruntime.RequiredSubcallRule{
			Child:            rule.Child,
			RequiredSubcalls: rule.RequiredSubcalls,
		})
	}
	return out
}

func prefixLongCoTQuestionPrompts(questions []longcoteval.Question, prefix string) []longcoteval.Question {
	prefix = strings.TrimRight(prefix, "\n") + "\n"
	out := append([]longcoteval.Question(nil), questions...)
	for i := range out {
		prompt := strings.TrimSpace(out[i].PromptText)
		if strings.HasPrefix(prompt, strings.TrimSpace(prefix)) {
			continue
		}
		out[i].PromptText = prefix + prompt
		out[i].QuestionHash = hashLongCoTText(out[i].PromptText)
	}
	return out
}

func normalizeDistinctLower(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
}

func hashLongCoTText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func longCoTDryRunOfficialFixtureQuestions() []longcoteval.Question {
	const prompt = `You are being tested on your capacity for extended reasoning.
Solve the problem and return your final answer as:
solution = <value>

Question:
A room has 3 red balls and 2 blue balls. Two balls are drawn without replacement.
What is the probability that both balls are red?`
	return []longcoteval.Question{
		{
			ID:           "fixture_math_easy_1",
			Domain:       "math",
			Difficulty:   "easy",
			Template:     "OfflineFixtureProbability",
			PromptText:   strings.TrimSpace(prompt),
			Answer:       `{"solution":"3/10"}`,
			Canary:       "offline-fixture-canary",
			QuestionHash: hashLongCoTText(prompt),
		},
	}
}

func applyLongCoTQuestionFilter(questions []longcoteval.Question, filter longCoTQuestionFilter) ([]longcoteval.Question, error) {
	domainSet := make(map[string]struct{}, len(filter.Domains))
	for _, domain := range filter.Domains {
		domain = strings.TrimSpace(strings.ToLower(domain))
		if domain == "" {
			continue
		}
		domainSet[domain] = struct{}{}
	}
	difficultySet, err := resolveLongCoTDifficultySet(filter.Difficulty)
	if err != nil {
		return nil, err
	}
	split := strings.ToLower(strings.TrimSpace(filter.Split))

	filtered := make([]longcoteval.Question, 0, len(questions))
	for _, question := range questions {
		if split != "" {
			effectiveSplit := strings.ToLower(strings.TrimSpace(question.Split))
			if effectiveSplit == "" {
				effectiveSplit = strings.ToLower(strings.TrimSpace(question.Difficulty))
			}
			if split != effectiveSplit {
				continue
			}
		}
		if len(domainSet) > 0 {
			if _, ok := domainSet[strings.ToLower(strings.TrimSpace(question.Domain))]; !ok {
				continue
			}
		}
		if len(difficultySet) > 0 {
			if _, ok := difficultySet[strings.ToLower(strings.TrimSpace(question.Difficulty))]; !ok {
				continue
			}
		}
		filtered = append(filtered, question)
	}
	if filter.Seed != 0 {
		rng := rand.New(rand.NewSource(filter.Seed))
		rng.Shuffle(len(filtered), func(i, j int) {
			filtered[i], filtered[j] = filtered[j], filtered[i]
		})
	}
	if filter.Limit > 0 && len(filtered) > filter.Limit {
		filtered = filtered[:filter.Limit]
	}
	return filtered, nil
}

func resolveLongCoTDifficultySet(raw string) (map[string]struct{}, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "":
		return nil, nil
	case "easy", "medium", "hard":
		return map[string]struct{}{value: {}}, nil
	case "longcot-mini":
		return map[string]struct{}{"easy": {}}, nil
	case "longcot":
		return map[string]struct{}{"medium": {}, "hard": {}}, nil
	default:
		return nil, fmt.Errorf("unsupported --difficulty %q (allowed: easy, medium, hard, longcot-mini, longcot)", raw)
	}
}

func buildLongCoTRunID(
	mode string,
	datasetPath string,
	filter longCoTQuestionFilter,
	conditions []longcoteval.Condition,
	questions []longcoteval.Question,
	provider, model, baseURL string,
	maxTokens int,
	temperature float64,
	timeout time.Duration,
) string {
	h := sha256.New()
	writeLongCoTHashKV(h, "mode", strings.TrimSpace(mode))
	writeLongCoTHashKV(h, "dataset", datasetPath)
	writeLongCoTHashKV(h, "split", filter.Split)
	writeLongCoTHashKV(h, "difficulty", filter.Difficulty)
	writeLongCoTHashKV(h, "limit", fmt.Sprintf("%d", filter.Limit))
	writeLongCoTHashKV(h, "seed", fmt.Sprintf("%d", filter.Seed))
	writeLongCoTHashKV(h, "provider", strings.TrimSpace(provider))
	writeLongCoTHashKV(h, "model", strings.TrimSpace(model))
	writeLongCoTHashKV(h, "base_url", strings.TrimSpace(baseURL))
	writeLongCoTHashKV(h, "max_tokens", fmt.Sprintf("%d", maxTokens))
	writeLongCoTHashKV(h, "temperature", fmt.Sprintf("%f", temperature))
	writeLongCoTHashKV(h, "timeout_ms", fmt.Sprintf("%d", timeout.Milliseconds()))

	domains := append([]string(nil), filter.Domains...)
	sort.Strings(domains)
	for _, domain := range domains {
		writeLongCoTHashKV(h, "domain", domain)
	}

	conditionIDs := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		conditionIDs = append(conditionIDs, string(condition.ID))
	}
	sort.Strings(conditionIDs)
	for _, conditionID := range conditionIDs {
		writeLongCoTHashKV(h, "condition", conditionID)
	}

	for _, question := range questions {
		writeLongCoTHashKV(h, "question", question.ID)
		writeLongCoTHashKV(h, "question_hash", question.QuestionHash)
	}

	sum := h.Sum(nil)
	prefix := "longcot-run-"
	if strings.TrimSpace(mode) != "" {
		prefix = "longcot-" + strings.TrimSpace(mode) + "-"
	}
	return prefix + hex.EncodeToString(sum[:8])
}

func writeLongCoTHashKV(h hash.Hash, key, value string) {
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{'\n'})
}

func planLongCoTDryRunAttempts(
	runID string,
	questions []longcoteval.Question,
	conditions []longcoteval.Condition,
	provider, model string,
) []longcoteval.Attempt {
	attempts := make([]longcoteval.Attempt, 0, len(questions)*len(conditions))
	for _, question := range questions {
		for _, condition := range conditions {
			effectiveCondition := longCoTEffectiveConditionForQuestion(question, condition)
			leakage := longcoteval.AssessLeakage(effectiveCondition.AllowedTools, longCoTLeakageOptionsForCondition(effectiveCondition))
			attempt := longcoteval.Attempt{
				RunID:         runID,
				PairID:        question.ID,
				AttemptID:     longCoTAttemptID(runID, question.ID, condition.ID),
				QuestionID:    question.ID,
				ConditionID:   condition.ID,
				ConditionKind: condition.Kind,
				Status:        longcoteval.AttemptStatusUnverified,
				Provider:      strings.TrimSpace(provider),
				Model:         strings.TrimSpace(model),
				Runner:        "dry-run",
				LeakageFlags:  leakage,
			}
			if condition.Kind == longcoteval.ConditionKindRLM {
				attempt.RLM = &longcoteval.RLMAttemptMeta{
					RouteProfile:  effectiveCondition.RLMRouteProfile,
					PlanMode:      effectiveCondition.RLMPlanMode,
					ToolProfile:   effectiveCondition.RLMToolProfile,
					MaxDepth:      longCoTConditionMaxDepth(effectiveCondition),
					MaxIterations: effectiveCondition.MaxIterations,
					MaxSubcalls:   effectiveCondition.MaxSubcalls,
				}
				longCoTAttachEffectiveContractMeta(&attempt, effectiveCondition)
			}
			attempts = append(attempts, attempt)
		}
	}
	return attempts
}

func longCoTAttemptID(runID, questionID string, conditionID longcoteval.ConditionID) string {
	sum := sha256.Sum256([]byte(runID + "\x00" + questionID + "\x00" + string(conditionID)))
	return "attempt-" + hex.EncodeToString(sum[:8])
}

func longCoTComparisonsForConditions(conditions []longcoteval.Condition) []longcoteval.Comparison {
	selected := map[longcoteval.ConditionID]struct{}{}
	for _, condition := range conditions {
		selected[condition.ID] = struct{}{}
	}
	candidates := []longcoteval.Comparison{
		{Baseline: longcoteval.ConditionBaselineNoToolsOfficial, Candidate: longcoteval.ConditionRLMNoToolsSingle},
		{Baseline: longcoteval.ConditionBaselineNoToolsOfficial, Candidate: longcoteval.ConditionRLMReplNoSubcalls},
		{Baseline: longcoteval.ConditionBaselineNoToolsOfficial, Candidate: longcoteval.ConditionRLMReplRecursive},
		{Baseline: longcoteval.ConditionBaselineNoToolsOfficial, Candidate: longcoteval.ConditionRLMLambdaReplSingle},
		{Baseline: longcoteval.ConditionBaselineNoToolsOfficial, Candidate: longcoteval.ConditionRLMLambdaAdaptiveSingle},
		{Baseline: longcoteval.ConditionRLMNoToolsSingle, Candidate: longcoteval.ConditionRLMReplNoSubcalls},
		{Baseline: longcoteval.ConditionRLMNoToolsSingle, Candidate: longcoteval.ConditionRLMReplRecursive},
		{Baseline: longcoteval.ConditionRLMNoToolsSingle, Candidate: longcoteval.ConditionRLMLambdaReplSingle},
		{Baseline: longcoteval.ConditionRLMNoToolsSingle, Candidate: longcoteval.ConditionRLMLambdaAdaptiveSingle},
		{Baseline: longcoteval.ConditionRLMReplNoSubcalls, Candidate: longcoteval.ConditionRLMReplRecursive},
		{Baseline: longcoteval.ConditionRLMReplRecursive, Candidate: longcoteval.ConditionRLMLambdaReplSingle},
		{Baseline: longcoteval.ConditionRLMLambdaReplSingle, Candidate: longcoteval.ConditionRLMLambdaAdaptiveSingle},
		{Baseline: longcoteval.ConditionRLMLambdaAdaptiveSingle, Candidate: longcoteval.ConditionRLMLambdaThenBraidSingle},
		{Baseline: longcoteval.ConditionRLMBraidSingle, Candidate: longcoteval.ConditionRLMLambdaThenBraidSingle},
		{Baseline: longcoteval.ConditionRLMNoToolsSingle, Candidate: longcoteval.ConditionRLMNoToolsStaged},
		{Baseline: longcoteval.ConditionRLMNoToolsStaged, Candidate: longcoteval.ConditionRLMNoModelToolsStaged},
	}
	out := make([]longcoteval.Comparison, 0, len(candidates))
	for _, candidate := range candidates {
		_, hasBaseline := selected[candidate.Baseline]
		_, hasCandidate := selected[candidate.Candidate]
		if hasBaseline && hasCandidate {
			out = append(out, candidate)
		}
	}
	return out
}

func extractLongCoTQuestionIDs(questions []longcoteval.Question) []string {
	out := make([]string, 0, len(questions))
	for _, question := range questions {
		out = append(out, question.ID)
	}
	return out
}

func resolvedLongCoTOutputDir(outputDir string) string {
	dir := strings.TrimSpace(outputDir)
	if dir == "" {
		dir = filepath.Join(resolveContextWorkspace(""), ".foxctl", "exports", "evals")
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

func verifyLongCoTAttempts(
	ctx context.Context,
	attempts []longcoteval.Attempt,
	questions []longcoteval.Question,
	verifier longcoteval.Verifier,
) ([]longcoteval.Attempt, longcoteval.VerifyResult, error) {
	tempDir, err := os.MkdirTemp("", "foxctl-longcot-verify-*")
	if err != nil {
		return nil, longcoteval.VerifyResult{}, fmt.Errorf("prepare LongCoT verification workspace: %w", err)
	}
	defer os.RemoveAll(tempDir)

	responsesPath := filepath.Join(tempDir, "responses.jsonl")
	outputPath := filepath.Join(tempDir, "results.json")
	if err := writeLongCoTOfficialResponsesJSONL(responsesPath, attempts, questions); err != nil {
		return nil, longcoteval.VerifyResult{}, fmt.Errorf("write official LongCoT responses JSONL: %w", err)
	}

	verifyResult, err := verifier.Verify(ctx, longcoteval.VerifyRequest{
		ResponsesPath: responsesPath,
		OutputPath:    outputPath,
	})
	if err != nil {
		return nil, longcoteval.VerifyResult{}, fmt.Errorf("official LongCoT verification failed: %w", err)
	}
	verifiedAttempts := applyLongCoTVerificationRows(attempts, verifyResult.Rows)
	return verifiedAttempts, verifyResult, nil
}

func verifyLongCoTAttemptsAgainstDatasetAnswers(
	attempts []longcoteval.Attempt,
	questions []longcoteval.Question,
) ([]longcoteval.Attempt, longcoteval.VerifyResult) {
	byID := make(map[string]longcoteval.Question, len(questions))
	for _, question := range questions {
		byID[question.ID] = question
	}
	rows := make([]longcoteval.VerifyRow, 0, len(attempts))
	counts := map[string]int{"total": len(attempts)}
	for _, attempt := range attempts {
		question, ok := byID[attempt.QuestionID]
		row := verifyLongCoTAttemptAgainstDatasetAnswer(attempt, question, ok)
		rows = append(rows, row)
		switch row.Status {
		case longcoteval.VerifierStatusCorrect:
			counts["correct"]++
		case longcoteval.VerifierStatusIncorrect:
			counts["incorrect"]++
		case longcoteval.VerifierStatusWrongFormatting:
			counts["wrong_formatting"]++
		default:
			counts["failed"]++
		}
	}
	return applyLongCoTVerificationRows(attempts, rows), longcoteval.VerifyResult{
		VerifierName:    "foxctl.dataset_answer",
		VerifierVersion: "v1",
		Counts:          counts,
		Rows:            rows,
	}
}

func verifyLongCoTAttemptAgainstDatasetAnswer(attempt longcoteval.Attempt, question longcoteval.Question, found bool) longcoteval.VerifyRow {
	row := longcoteval.VerifyRow{
		QuestionID: attempt.QuestionID,
		Status:     longcoteval.VerifierStatusFailed,
	}
	if !found {
		row.VerificationError = "missing_question"
		return row
	}
	expected, ok := longCoTExpectedSolutionValue(question.Answer)
	if !ok {
		row.VerificationError = "missing_expected_solution"
		return row
	}
	actual, ok := longCoTResponseSolutionValue(attempt.ResponseText)
	if !ok {
		row.Status = longcoteval.VerifierStatusWrongFormatting
		row.WrongFormatting = true
		row.VerificationError = "missing_solution_line"
		return row
	}
	row.NormalizedAnswer = actual
	if longCoTCanonicalSolutionValue(actual) == longCoTCanonicalSolutionValue(expected) {
		row.Status = longcoteval.VerifierStatusCorrect
		row.Correct = true
		return row
	}
	row.Status = longcoteval.VerifierStatusIncorrect
	return row
}

func longCoTExpectedSolutionValue(answer string) (string, bool) {
	answer = strings.TrimSpace(answer)
	if answer == "" || answer == "null" {
		return "", false
	}
	var payload any
	if err := json.Unmarshal([]byte(answer), &payload); err != nil {
		return answer, true
	}
	return longCoTSolutionValueFromAny(payload)
}

func longCoTSolutionValueFromAny(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"solution", "answer", "value"} {
			if raw, ok := typed[key]; ok {
				return longCoTSolutionValueFromAny(raw)
			}
		}
		return "", false
	case string:
		return strings.TrimSpace(typed), strings.TrimSpace(typed) != ""
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case bool:
		if typed {
			return "true", true
		}
		return "false", true
	case nil:
		return "", false
	default:
		return strings.TrimSpace(fmt.Sprint(typed)), strings.TrimSpace(fmt.Sprint(typed)) != ""
	}
}

func longCoTResponseSolutionValue(response string) (string, bool) {
	if answer, ok := rlm.ExtractSolutionLine(response); ok {
		return strings.TrimSpace(strings.TrimPrefix(answer, "solution =")), strings.TrimSpace(strings.TrimPrefix(answer, "solution =")) != ""
	}
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "solution=") {
		value := strings.TrimSpace(strings.TrimPrefix(response, "solution="))
		return value, value != ""
	}
	return "", false
}

func longCoTCanonicalSolutionValue(value string) string {
	value = strings.TrimSpace(value)
	var payload any
	if err := json.Unmarshal([]byte(value), &payload); err == nil {
		if solution, ok := longCoTSolutionValueFromAny(payload); ok {
			value = solution
		}
	}
	value = strings.TrimSpace(value)
	if number, ok := longCoTCanonicalNumber(value); ok {
		return number
	}
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func longCoTCanonicalNumber(value string) (string, bool) {
	number := json.Number(strings.TrimSpace(value))
	if number.String() == "" {
		return "", false
	}
	if i, err := number.Int64(); err == nil {
		return strconv.FormatInt(i, 10), true
	}
	if f, err := number.Float64(); err == nil {
		return strconv.FormatFloat(f, 'f', -1, 64), true
	}
	return "", false
}

func applyLongCoTVerificationRows(attempts []longcoteval.Attempt, rows []longcoteval.VerifyRow) []longcoteval.Attempt {
	if len(attempts) == 0 || len(rows) == 0 {
		return attempts
	}
	out := append([]longcoteval.Attempt(nil), attempts...)
	if len(rows) == len(out) {
		for i := range out {
			out[i] = mergeLongCoTVerifyRow(out[i], rows[i])
		}
		return out
	}
	byQuestion := map[string][]longcoteval.VerifyRow{}
	for _, row := range rows {
		byQuestion[row.QuestionID] = append(byQuestion[row.QuestionID], row)
	}
	for i := range out {
		rowsForQuestion := byQuestion[out[i].QuestionID]
		if len(rowsForQuestion) == 0 {
			continue
		}
		out[i] = mergeLongCoTVerifyRow(out[i], rowsForQuestion[0])
		byQuestion[out[i].QuestionID] = rowsForQuestion[1:]
	}
	return out
}

func mergeLongCoTVerifyRow(attempt longcoteval.Attempt, row longcoteval.VerifyRow) longcoteval.Attempt {
	status := strings.TrimSpace(row.Status)
	if status == "" {
		status = longcoteval.VerifierStatusFailed
	}
	attempt.VerifierStatus = status
	attempt.Correct = row.Correct
	attempt.Successful = row.Correct
	attempt.WrongFormatting = row.WrongFormatting
	attempt.VerificationError = strings.TrimSpace(row.VerificationError)
	attempt.NormalizedAnswer = strings.TrimSpace(row.NormalizedAnswer)
	return attempt
}

func saveLongCoTOutputs(outputDir string, result longcoteval.RunResult, markdown string) ([]longcoteval.SavedArtifact, error) {
	runDir := filepath.Join(outputDir, "longcot", sanitizeEvalName(result.RunID))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}

	jsonPath := filepath.Join(runDir, "result.json")
	markdownPath := filepath.Join(runDir, "report.md")
	responsesPath := filepath.Join(runDir, "responses.official.jsonl")
	artifacts := []longcoteval.SavedArtifact{
		{Kind: "result_json", Path: jsonPath},
		{Kind: "report_markdown", Path: markdownPath},
		{Kind: "responses_official_jsonl", Path: responsesPath},
	}
	result.Artifacts = artifacts

	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	body = append(body, '\n')
	if err := writeLongCoTAtomic(jsonPath, body, 0o644); err != nil {
		return nil, err
	}
	if err := writeLongCoTAtomic(markdownPath, []byte(markdown), 0o644); err != nil {
		return nil, err
	}
	if err := writeLongCoTOfficialResponsesJSONL(responsesPath, result.Attempts, result.Questions); err != nil {
		return nil, err
	}

	return artifacts, nil
}

func writeLongCoTOfficialResponsesJSONL(path string, attempts []longcoteval.Attempt, questions []longcoteval.Question) error {
	var b strings.Builder
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	questionByID := make(map[string]longcoteval.Question, len(questions))
	for _, question := range questions {
		questionByID[question.ID] = question
	}
	for _, attempt := range attempts {
		record := longcoteval.OfficialResponseForAttemptQuestion(attempt, questionByID[attempt.QuestionID])
		if err := encoder.Encode(record); err != nil {
			return fmt.Errorf("encode official response for %s/%s: %w", attempt.QuestionID, attempt.ConditionID, err)
		}
	}
	return writeLongCoTAtomic(path, []byte(b.String()), 0o644)
}

func writeLongCoTAtomic(path string, body []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
