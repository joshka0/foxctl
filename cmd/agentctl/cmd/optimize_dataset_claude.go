package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/context/sessionkit/claudejsonl"
	config "github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/providers/llmcompat"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/spf13/cobra"
)

func newOptimizeDatasetClaudeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude",
		Short: "Ingest direct Claude Code transcripts and export a cleaned dataset",
	}
	cmd.AddCommand(
		newOptimizeDatasetClaudeExportCommand(),
		newOptimizeDatasetClaudeRewriteCommand(),
		newOptimizeDatasetClaudeLeaderboardCommand(),
	)
	return cmd
}

func newOptimizeDatasetClaudeExportCommand() *cobra.Command {
	var (
		workspace       string
		claudeHome      string
		limit           int
		signalProfile   string
		datasetMode     string
		includeTools    bool
		includeFiles    bool
		includeFeedback bool
		outputFile      string
		toCAS           bool
		forceIngest     bool
		dryRun          bool
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Scan ~/.claude/projects for one workspace, ingest transcripts, and export JSONL",
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

			if limit < 0 {
				return writeOptimizeError(out, optimizeDatasetCommand, "invalid --limit: must be >= 0")
			}

			examples, ingested, scanned, err := exportClaudeProjectDataset(ctx, sessionStore, memStore, absWorkspace, strings.TrimSpace(claudeHome), forceIngest, dryRun, limit, includeTools, includeFiles, includeFeedback, signalProfile, datasetMode)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("export Claude transcripts: %v", err))
			}

			plan, err := planClaudeTranscriptExport(cmd.CommandPath(), absWorkspace, scanned, ingested, includeTools, includeFiles, includeFeedback, signalProfile, datasetMode, outputFile, toCAS, examples)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("plan Claude transcript export: %v", err))
			}
			data, artifact, err := applyClaudeTranscriptExport(ctx, cfg.Paths.CAS, plan, dryRun)
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
	cmd.Flags().StringVar(&claudeHome, "claude-home", "", "Optional Claude home override (defaults to ~/.claude)")
	cmd.Flags().IntVar(&limit, "limit", 1000, "Maximum examples to export after ingestion")
	cmd.Flags().StringVar(&signalProfile, "signal-profile", "general", "Signal profile: general, coder, or coder-strong")
	cmd.Flags().StringVar(&datasetMode, "dataset-mode", "standalone", "Dataset mode: standalone, continuation, or ops")
	cmd.Flags().BoolVar(&includeTools, "include-tools", true, "Include tool usage metadata")
	cmd.Flags().BoolVar(&includeFiles, "include-files", true, "Include file-touch metadata")
	cmd.Flags().BoolVar(&includeFeedback, "include-feedback", true, "Join session feedback when available")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "Write JSONL dataset to a file")
	cmd.Flags().BoolVar(&toCAS, "to-cas", false, "Write JSONL dataset to CAS and return a digest")
	cmd.Flags().BoolVar(&forceIngest, "force-ingest", false, "Re-ingest sessions even if already present in sessions.db")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview Claude dataset export without ingesting sessions or writing output")
	return cmd
}

type claudeTranscriptExportPlan struct {
	Data       map[string]any
	Examples   []optimization.TranscriptTrainingExample
	OutputFile string
	ToCAS      bool
	Candidate  string
}

func planClaudeTranscriptExport(
	commandPath string,
	workspace string,
	scanned int,
	ingested int,
	includeTools bool,
	includeFiles bool,
	includeFeedback bool,
	signalProfile string,
	datasetMode string,
	outputFile string,
	toCAS bool,
	examples []optimization.TranscriptTrainingExample,
) (claudeTranscriptExportPlan, error) {
	body, err := optimization.BuildTranscriptDatasetJSONL(examples)
	if err != nil {
		return claudeTranscriptExportPlan{}, err
	}
	sum := sha256.Sum256(body)
	candidate := "sha256:" + hex.EncodeToString(sum[:])
	data := map[string]any{
		"operation":                 "dataset.claude.export",
		"workspace_id":              workspace,
		"scanned_sessions":          scanned,
		"ingested_sessions":         ingested,
		"example_count":             len(examples),
		"signal_profile":            normalizeClaudeSignalProfile(signalProfile),
		"dataset_mode":              normalizeClaudeDatasetMode(datasetMode),
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
	return claudeTranscriptExportPlan{
		Data:       data,
		Examples:   examples,
		OutputFile: strings.TrimSpace(outputFile),
		ToCAS:      toCAS,
		Candidate:  candidate,
	}, nil
}

func applyClaudeTranscriptExport(ctx context.Context, casRoot string, plan claudeTranscriptExportPlan, dryRun bool) (map[string]any, string, error) {
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

type claudeRawPair struct {
	SessionID         string    `json:"session_id"`
	RawJSONLPath      string    `json:"raw_jsonl_path"`
	UserRequest       string    `json:"user_request"`
	AssistantResponse string    `json:"assistant_response"`
	ToolsUsed         []string  `json:"tools_used,omitempty"`
	Timestamp         time.Time `json:"timestamp,omitempty"`
}

type claudeRewriteContent struct {
	CleanUserRequest       string   `json:"clean_user_request"`
	CleanAssistantResponse string   `json:"clean_assistant_response"`
	Reason                 string   `json:"reason,omitempty"`
	Tags                   []string `json:"tags,omitempty"`
}

type claudeRewriteRecord struct {
	RecordType string                                 `json:"record_type"`
	Raw        claudeRawPair                          `json:"raw"`
	Clean      optimization.TranscriptTrainingExample `json:"clean"`
	Metadata   map[string]any                         `json:"metadata,omitempty"`
}

type claudeRewriteDebugRecord struct {
	RecordType string                `json:"record_type"`
	Status     string                `json:"status"`
	Raw        claudeRawPair         `json:"raw"`
	Attempted  *claudeRewriteContent `json:"attempted,omitempty"`
	Metadata   map[string]any        `json:"metadata,omitempty"`
}

type claudeRewriteFidelityConfig struct {
	Enabled                       bool    `json:"enabled"`
	MaxUserNovelRatio             float64 `json:"max_user_novel_ratio"`
	MaxAssistantNovelRatio        float64 `json:"max_assistant_novel_ratio"`
	AssistantLowOverlapNovelRatio float64 `json:"assistant_low_overlap_novel_ratio"`
	AssistantLowOverlapThreshold  float64 `json:"assistant_low_overlap_threshold"`
}

type claudeRewriteReasoningConfig struct {
	Effort  string `json:"effort,omitempty"`
	Exclude bool   `json:"exclude,omitempty"`
}

type claudeRewriteFidelityAnalysis struct {
	ContainsPlaceholder bool    `json:"contains_placeholder"`
	UserSame            float64 `json:"user_same"`
	UserCross           float64 `json:"user_cross"`
	AssistantSame       float64 `json:"assistant_same"`
	AssistantCross      float64 `json:"assistant_cross"`
	UserNovelRatio      float64 `json:"user_novel_ratio"`
	AssistantNovelRatio float64 `json:"assistant_novel_ratio"`
}

type claudeRewriteRunResult struct {
	ScannedPairs     int                        `json:"scanned_pairs"`
	CandidatePairs   int                        `json:"candidate_pairs"`
	KeptRecords      int                        `json:"kept_records"`
	FidelityRejected int                        `json:"fidelity_rejected"`
	DebugRejections  int                        `json:"debug_rejections"`
	FailedOver       bool                       `json:"failed_over"`
	PrimaryTarget    claudeRewriteTargetConfig  `json:"primary_target"`
	ActiveTarget     claudeRewriteTargetConfig  `json:"active_target"`
	FallbackTarget   *claudeRewriteTargetConfig `json:"fallback_target,omitempty"`
	Records          []claudeRewriteRecord      `json:"records,omitempty"`
	DebugRecords     []claudeRewriteDebugRecord `json:"debug_records,omitempty"`
}

func planClaudeRewrite(workspacePath, claudeHome string, limit int, signalProfile string) ([]claudeRawPair, int, error) {
	rawLimit := limit
	if rawLimit > 0 {
		rawLimit = max(limit*10, limit)
	}
	pairs, scanned, err := collectClaudeRawPairs(workspacePath, claudeHome, rawLimit)
	if err != nil {
		return nil, 0, err
	}
	candidates := filterClaudeRawPairsDeterministic(pairs, signalProfile)
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, scanned, nil
}

type claudeRewriteTargetConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"-"`
	Model    string `json:"model"`
}

type claudeLeaderboardTarget struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type claudeRewriteLeaderboardEntry struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	ElapsedMS        int64   `json:"elapsed_ms"`
	ScannedPairs     int     `json:"scanned_pairs"`
	CandidatePairs   int     `json:"candidate_pairs"`
	KeptRecords      int     `json:"kept_records"`
	FidelityRejected int     `json:"fidelity_rejected"`
	DebugRejections  int     `json:"debug_rejections"`
	KeepRate         float64 `json:"keep_rate"`
	OutputFile       string  `json:"output_file,omitempty"`
	DebugOutputFile  string  `json:"debug_output_file,omitempty"`
}

func defaultClaudeRewriteFidelityConfig() claudeRewriteFidelityConfig {
	return claudeRewriteFidelityConfig{
		Enabled:                       true,
		MaxUserNovelRatio:             0.7,
		MaxAssistantNovelRatio:        0.7,
		AssistantLowOverlapNovelRatio: 0.55,
		AssistantLowOverlapThreshold:  0.3,
	}
}

func newOptimizeDatasetClaudeRewriteCommand() *cobra.Command {
	var (
		workspace       string
		claudeHome      string
		limit           int
		signalProfile   string
		provider        string
		baseURL         string
		apiKey          string
		model           string
		outputFile      string
		debugOutputFile string
		toCAS           bool
		dryRun          bool
		fidelityConfig  = defaultClaudeRewriteFidelityConfig()
		reasoningConfig claudeRewriteReasoningConfig
	)

	cmd := &cobra.Command{
		Use:   "rewrite",
		Short: "Use one model to rewrite raw Claude transcript pairs into cleaner training rows",
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

			primaryTarget, fallbackTarget, err := resolveClaudeRewriteTargets(cfg, provider, baseURL, apiKey, model)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, err.Error())
			}
			var runResult claudeRewriteRunResult
			if dryRun {
				plannedCandidates, scannedPairs, planErr := planClaudeRewrite(absWorkspace, strings.TrimSpace(claudeHome), limit, signalProfile)
				if planErr != nil {
					return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("plan rewrite pipeline: %v", planErr))
				}
				runResult = claudeRewriteRunResult{
					PrimaryTarget:   primaryTarget,
					ActiveTarget:    primaryTarget,
					FallbackTarget:  fallbackTarget,
					ScannedPairs:    scannedPairs,
					CandidatePairs:  len(plannedCandidates),
					Records:         nil,
					DebugRecords:    nil,
					KeptRecords:     0,
					DebugRejections: 0,
				}
			} else {
				runResult, err = runClaudeRewrite(ctx, absWorkspace, strings.TrimSpace(claudeHome), limit, primaryTarget, fallbackTarget, fidelityConfig, reasoningConfig, signalProfile)
				if err != nil {
					return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("run rewrite pipeline: %v", err))
				}
			}

			data := map[string]any{
				"operation":         "dataset.claude.rewrite",
				"workspace_id":      absWorkspace,
				"provider":          runResult.ActiveTarget.Provider,
				"model":             runResult.ActiveTarget.Model,
				"primary_target":    runResult.PrimaryTarget,
				"active_target":     runResult.ActiveTarget,
				"failed_over":       runResult.FailedOver,
				"signal_profile":    normalizeClaudeSignalProfile(signalProfile),
				"scanned_pairs":     runResult.ScannedPairs,
				"candidate_pairs":   runResult.CandidatePairs,
				"kept_records":      runResult.KeptRecords,
				"fidelity_rejected": runResult.FidelityRejected,
				"debug_rejections":  runResult.DebugRejections,
				"fidelity":          fidelityConfig,
				"reasoning":         reasoningConfig,
				"cli_command":       cmd.CommandPath(),
			}
			if runResult.FallbackTarget != nil {
				data["fallback_target"] = runResult.FallbackTarget
			}

			data["dry_run"] = dryRun
			switch {
			case strings.TrimSpace(outputFile) != "":
				data["output_file"] = outputFile
				if !dryRun {
					if err := saveClaudeRewriteRecords(outputFile, runResult.Records); err != nil {
						return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("write rewritten dataset: %v", err))
					}
				} else {
					data["would_write_file"] = true
				}
			case toCAS:
				data["to_cas"] = true
				if !dryRun {
					artifact, err := persistClaudeRewriteRecords(ctx, cfg.Paths.CAS, runResult.Records)
					if err != nil {
						return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("persist rewritten dataset: %v", err))
					}
					data["artifact"] = artifact
					return protocol.WriteOK(out, optimizeDatasetCommand, data,
						protocol.WithSource("run"),
						protocol.WithWorkspace(absWorkspace),
						protocol.WithCASDigest(artifact),
					)
				}
				data["would_write_cas"] = true
			default:
				data["records"] = runResult.Records
			}
			if strings.TrimSpace(debugOutputFile) != "" {
				data["debug_output_file"] = debugOutputFile
				if !dryRun {
					if err := saveClaudeRewriteDebugRecords(debugOutputFile, runResult.DebugRecords); err != nil {
						return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("write debug dataset: %v", err))
					}
				} else {
					data["would_write_debug_file"] = true
				}
			}

			return protocol.WriteOK(out, optimizeDatasetCommand, data,
				protocol.WithSource(map[bool]string{true: "plan", false: "run"}[dryRun]),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&claudeHome, "claude-home", "", "Optional Claude home override (defaults to ~/.claude)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum raw Claude pairs to inspect")
	cmd.Flags().StringVar(&signalProfile, "signal-profile", "general", "Signal profile: general or coder")
	cmd.Flags().StringVar(&provider, "provider", "", "Provider name for the rewrite client (default: remote-best with local fallback)")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL override")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key override (defaults to lm-studio for LMStudio)")
	cmd.Flags().StringVar(&model, "model", "", "Model ID to use for rewriting (default: remote-best with local fallback)")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "Write rewritten dataset JSONL to a file")
	cmd.Flags().StringVar(&debugOutputFile, "debug-output-file", "", "Write rejected/debug rewrite attempts to a JSONL file")
	cmd.Flags().BoolVar(&toCAS, "to-cas", false, "Write rewritten dataset JSONL to CAS and return a digest")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview Claude rewrite output without writing files or CAS artifacts")
	cmd.Flags().StringVar(&reasoningConfig.Effort, "reasoning-effort", "", "Optional reasoning effort for providers that support explicit reasoning controls (for example OpenRouter)")
	cmd.Flags().BoolVar(&reasoningConfig.Exclude, "exclude-reasoning", false, "Exclude reasoning content from providers that support explicit reasoning controls")
	cmd.Flags().BoolVar(&fidelityConfig.Enabled, "fidelity-gate", true, "Enable post-rewrite fidelity validation")
	cmd.Flags().Float64Var(&fidelityConfig.MaxUserNovelRatio, "fidelity-max-user-novel-ratio", fidelityConfig.MaxUserNovelRatio, "Maximum allowed novel-token ratio for rewritten user requests")
	cmd.Flags().Float64Var(&fidelityConfig.MaxAssistantNovelRatio, "fidelity-max-assistant-novel-ratio", fidelityConfig.MaxAssistantNovelRatio, "Maximum allowed novel-token ratio for rewritten assistant responses")
	cmd.Flags().Float64Var(&fidelityConfig.AssistantLowOverlapNovelRatio, "fidelity-assistant-low-overlap-novel-ratio", fidelityConfig.AssistantLowOverlapNovelRatio, "Reject assistant rewrites when novelty exceeds this ratio and overlap stays low")
	cmd.Flags().Float64Var(&fidelityConfig.AssistantLowOverlapThreshold, "fidelity-assistant-low-overlap-threshold", fidelityConfig.AssistantLowOverlapThreshold, "Assistant overlap threshold paired with --fidelity-assistant-low-overlap-novel-ratio")
	return cmd
}

func newOptimizeDatasetClaudeLeaderboardCommand() *cobra.Command {
	var (
		workspace       string
		claudeHome      string
		limit           int
		signalProfile   string
		defaultProvider string
		baseURL         string
		apiKey          string
		models          []string
		targets         []string
		outputDir       string
		debugOutputDir  string
		reportFile      string
		dryRun          bool
		fidelityConfig  = defaultClaudeRewriteFidelityConfig()
		reasoningConfig claudeRewriteReasoningConfig
	)

	cmd := &cobra.Command{
		Use:   "leaderboard",
		Short: "Run the Claude rewrite pipeline across multiple models and summarize the results",
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

			resolvedTargets, err := resolveClaudeLeaderboardTargets(defaultProvider, models, targets)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, err.Error())
			}
			if len(resolvedTargets) == 0 {
				return writeOptimizeError(out, optimizeDatasetCommand, "at least one --model or --target is required")
			}
			if !dryRun && strings.TrimSpace(outputDir) != "" {
				if err := os.MkdirAll(outputDir, 0o755); err != nil {
					return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("create output dir: %v", err))
				}
			}
			if !dryRun && strings.TrimSpace(debugOutputDir) != "" {
				if err := os.MkdirAll(debugOutputDir, 0o755); err != nil {
					return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("create debug output dir: %v", err))
				}
			}

			entries := make([]claudeRewriteLeaderboardEntry, 0, len(resolvedTargets))
			for _, target := range resolvedTargets {
				if err := ctx.Err(); err != nil {
					return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("leaderboard canceled: %v", err))
				}
				resolvedBaseURL := strings.TrimSpace(baseURL)
				if resolvedBaseURL == "" {
					resolvedBaseURL = cfg.LLM.ResolveBaseURL(target.Provider)
				}
				resolvedAPIKey := strings.TrimSpace(apiKey)
				if resolvedAPIKey == "" {
					if strings.EqualFold(target.Provider, "lmstudio") {
						resolvedAPIKey = "lm-studio"
					} else {
						resolvedAPIKey = cfg.LLM.ResolveAPIKey(target.Provider)
					}
				}

				start := time.Now()
				runResult, err := runClaudeRewrite(ctx, absWorkspace, strings.TrimSpace(claudeHome), limit, claudeRewriteTargetConfig{
					Provider: target.Provider,
					BaseURL:  resolvedBaseURL,
					APIKey:   resolvedAPIKey,
					Model:    target.Model,
				}, nil, fidelityConfig, reasoningConfig, signalProfile)
				elapsed := time.Since(start).Milliseconds()
				if err != nil {
					return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("run rewrite pipeline for %s/%s: %v", target.Provider, target.Model, err))
				}

				entry := claudeRewriteLeaderboardEntry{
					Provider:         target.Provider,
					Model:            target.Model,
					ElapsedMS:        elapsed,
					ScannedPairs:     runResult.ScannedPairs,
					CandidatePairs:   runResult.CandidatePairs,
					KeptRecords:      runResult.KeptRecords,
					FidelityRejected: runResult.FidelityRejected,
					DebugRejections:  runResult.DebugRejections,
				}
				if runResult.CandidatePairs > 0 {
					entry.KeepRate = float64(runResult.KeptRecords) / float64(runResult.CandidatePairs)
				}
				if strings.TrimSpace(outputDir) != "" {
					entry.OutputFile = filepath.Join(outputDir, sanitizeClaudeLeaderboardName(target.Provider+"-"+target.Model)+".jsonl")
					if !dryRun {
						if err := saveClaudeRewriteRecords(entry.OutputFile, runResult.Records); err != nil {
							return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("write leaderboard output: %v", err))
						}
					}
				}
				if strings.TrimSpace(debugOutputDir) != "" {
					entry.DebugOutputFile = filepath.Join(debugOutputDir, sanitizeClaudeLeaderboardName(target.Provider+"-"+target.Model)+".debug.jsonl")
					if !dryRun {
						if err := saveClaudeRewriteDebugRecords(entry.DebugOutputFile, runResult.DebugRecords); err != nil {
							return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("write leaderboard debug output: %v", err))
						}
					}
				}
				entries = append(entries, entry)
			}

			sort.Slice(entries, func(i, j int) bool {
				if entries[i].KeepRate == entries[j].KeepRate {
					if entries[i].KeptRecords == entries[j].KeptRecords {
						return entries[i].ElapsedMS < entries[j].ElapsedMS
					}
					return entries[i].KeptRecords > entries[j].KeptRecords
				}
				return entries[i].KeepRate > entries[j].KeepRate
			})

			data := map[string]any{
				"operation":      "dataset.claude.leaderboard",
				"workspace_id":   absWorkspace,
				"limit":          limit,
				"entries":        entries,
				"signal_profile": normalizeClaudeSignalProfile(signalProfile),
				"fidelity":       fidelityConfig,
				"reasoning":      reasoningConfig,
				"dry_run":        dryRun,
				"cli_command":    cmd.CommandPath(),
			}
			if strings.TrimSpace(reportFile) != "" {
				data["report_file"] = reportFile
				if !dryRun {
					if err := saveClaudeRewriteLeaderboardReport(reportFile, data); err != nil {
						return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("write leaderboard report: %v", err))
					}
				} else {
					data["would_write_report"] = true
				}
			}
			if strings.TrimSpace(outputDir) != "" && dryRun {
				data["would_write_output_dir"] = outputDir
			}
			if strings.TrimSpace(debugOutputDir) != "" && dryRun {
				data["would_write_debug_output_dir"] = debugOutputDir
			}
			return protocol.WriteOK(out, optimizeDatasetCommand, data,
				protocol.WithSource(map[bool]string{true: "plan", false: "run"}[dryRun]),
				protocol.WithWorkspace(absWorkspace),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&claudeHome, "claude-home", "", "Optional Claude home override (defaults to ~/.claude)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum deterministic candidate pairs per model")
	cmd.Flags().StringVar(&signalProfile, "signal-profile", "general", "Signal profile: general or coder")
	cmd.Flags().StringVar(&defaultProvider, "provider", "lmstudio", "Default provider for --model entries")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL override")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key override")
	cmd.Flags().StringSliceVar(&models, "model", nil, "Model ID to benchmark using the default provider (repeatable)")
	cmd.Flags().StringSliceVar(&targets, "target", nil, "Explicit benchmark target in provider:model format (repeatable)")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Optional directory to write kept rewrites per model")
	cmd.Flags().StringVar(&debugOutputDir, "debug-output-dir", "", "Optional directory to write debug/rejected rewrites per model")
	cmd.Flags().StringVar(&reportFile, "report-file", "", "Optional path to write the leaderboard JSON report")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview leaderboard outputs without writing files")
	cmd.Flags().StringVar(&reasoningConfig.Effort, "reasoning-effort", "", "Optional reasoning effort for providers that support explicit reasoning controls")
	cmd.Flags().BoolVar(&reasoningConfig.Exclude, "exclude-reasoning", false, "Exclude reasoning content from providers that support explicit reasoning controls")
	cmd.Flags().BoolVar(&fidelityConfig.Enabled, "fidelity-gate", true, "Enable post-rewrite fidelity validation")
	cmd.Flags().Float64Var(&fidelityConfig.MaxUserNovelRatio, "fidelity-max-user-novel-ratio", fidelityConfig.MaxUserNovelRatio, "Maximum allowed novel-token ratio for rewritten user requests")
	cmd.Flags().Float64Var(&fidelityConfig.MaxAssistantNovelRatio, "fidelity-max-assistant-novel-ratio", fidelityConfig.MaxAssistantNovelRatio, "Maximum allowed novel-token ratio for rewritten assistant responses")
	cmd.Flags().Float64Var(&fidelityConfig.AssistantLowOverlapNovelRatio, "fidelity-assistant-low-overlap-novel-ratio", fidelityConfig.AssistantLowOverlapNovelRatio, "Reject assistant rewrites when novelty exceeds this ratio and overlap stays low")
	cmd.Flags().Float64Var(&fidelityConfig.AssistantLowOverlapThreshold, "fidelity-assistant-low-overlap-threshold", fidelityConfig.AssistantLowOverlapThreshold, "Assistant overlap threshold paired with --fidelity-assistant-low-overlap-novel-ratio")
	return cmd
}

func runClaudeRewrite(ctx context.Context, workspacePath, claudeHome string, limit int, primaryTarget claudeRewriteTargetConfig, fallbackTarget *claudeRewriteTargetConfig, fidelityConfig claudeRewriteFidelityConfig, reasoningConfig claudeRewriteReasoningConfig, signalProfile string) (claudeRewriteRunResult, error) {
	rawLimit := limit
	if rawLimit > 0 {
		rawLimit = max(limit*10, limit)
	}
	pairs, scanned, err := collectClaudeRawPairs(workspacePath, claudeHome, rawLimit)
	if err != nil {
		return claudeRewriteRunResult{}, err
	}
	candidates := filterClaudeRawPairsDeterministic(pairs, signalProfile)
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}

	records := make([]claudeRewriteRecord, 0, len(candidates))
	debugRecords := make([]claudeRewriteDebugRecord, 0, len(candidates))
	fidelityRejected := 0
	activeTarget := primaryTarget
	failedOver := false
	for _, pair := range candidates {
		if err := ctx.Err(); err != nil {
			return claudeRewriteRunResult{}, err
		}
		rewritten, rawModelOutput, err := rewriteClaudeRawPair(ctx, activeTarget.Provider, activeTarget.BaseURL, activeTarget.APIKey, activeTarget.Model, pair, reasoningConfig)
		if err != nil && !failedOver && fallbackTarget != nil && classifyClaudeRewriteFailure(err, rawModelOutput) == "request_error" {
			activeTarget = *fallbackTarget
			failedOver = true
			rewritten, rawModelOutput, err = rewriteClaudeRawPair(ctx, activeTarget.Provider, activeTarget.BaseURL, activeTarget.APIKey, activeTarget.Model, pair, reasoningConfig)
		}
		if err != nil {
			debugRecords = append(debugRecords, claudeRewriteDebugRecord{
				RecordType: "claude_rewrite_debug",
				Status:     classifyClaudeRewriteFailure(err, rawModelOutput),
				Raw:        pair,
				Metadata: map[string]any{
					"model":            activeTarget.Model,
					"provider":         activeTarget.Provider,
					"base_url":         activeTarget.BaseURL,
					"error":            err.Error(),
					"raw_model_output": rawModelOutput,
				},
			})
			continue
		}
		if strings.TrimSpace(rewritten.CleanUserRequest) == "" || strings.TrimSpace(rewritten.CleanAssistantResponse) == "" {
			debugRecords = append(debugRecords, claudeRewriteDebugRecord{
				RecordType: "claude_rewrite_debug",
				Status:     "empty_fields",
				Raw:        pair,
				Attempted:  &rewritten,
				Metadata: map[string]any{
					"model":            activeTarget.Model,
					"provider":         activeTarget.Provider,
					"base_url":         activeTarget.BaseURL,
					"raw_model_output": rawModelOutput,
				},
			})
			continue
		}
		fidelity := analyzeClaudeRewriteFidelity(pair, rewritten)
		if !isFaithfulClaudeRewriteWithConfig(pair, rewritten, fidelityConfig) {
			fidelityRejected++
			debugRecords = append(debugRecords, claudeRewriteDebugRecord{
				RecordType: "claude_rewrite_debug",
				Status:     "fidelity_rejected",
				Raw:        pair,
				Attempted:  &rewritten,
				Metadata: map[string]any{
					"model":            activeTarget.Model,
					"provider":         activeTarget.Provider,
					"base_url":         activeTarget.BaseURL,
					"raw_model_output": rawModelOutput,
					"fidelity":         fidelity,
				},
			})
			continue
		}
		record := claudeRewriteRecord{
			RecordType: "claude_rewrite",
			Raw:        pair,
			Clean: optimization.TranscriptTrainingExample{
				Input: optimization.TranscriptTrainingInput{
					UserRequest: strings.TrimSpace(rewritten.CleanUserRequest),
				},
				Output: optimization.TranscriptTrainingOutput{
					Response:  strings.TrimSpace(rewritten.CleanAssistantResponse),
					ToolsUsed: append([]string(nil), pair.ToolsUsed...),
				},
				Metadata: optimization.TranscriptTrainingMetadata{
					SessionID:    pair.SessionID,
					AgentType:    "claude",
					RawJSONLPath: pair.RawJSONLPath,
					Timestamp:    pair.Timestamp,
				},
			},
			Metadata: map[string]any{
				"model":            activeTarget.Model,
				"provider":         activeTarget.Provider,
				"base_url":         activeTarget.BaseURL,
				"reason":           strings.TrimSpace(rewritten.Reason),
				"tags":             append([]string(nil), rewritten.Tags...),
				"raw_model_output": rawModelOutput,
			},
		}
		records = append(records, record)
	}

	return claudeRewriteRunResult{
		ScannedPairs:     scanned,
		CandidatePairs:   len(candidates),
		KeptRecords:      len(records),
		FidelityRejected: fidelityRejected,
		DebugRejections:  len(debugRecords),
		FailedOver:       failedOver,
		PrimaryTarget:    primaryTarget,
		ActiveTarget:     activeTarget,
		FallbackTarget:   fallbackTarget,
		Records:          records,
		DebugRecords:     debugRecords,
	}, nil
}

func resolveClaudeRewriteTargets(cfg config.Config, provider, baseURL, apiKey, model string) (claudeRewriteTargetConfig, *claudeRewriteTargetConfig, error) {
	explicit := strings.TrimSpace(provider) != "" || strings.TrimSpace(baseURL) != "" || strings.TrimSpace(apiKey) != "" || strings.TrimSpace(model) != ""
	if !explicit {
		primary, err := resolveClaudeRewriteTarget(cfg, "openrouter", "", "", "openai/gpt-5.4-nano")
		if err != nil {
			return claudeRewriteTargetConfig{}, nil, err
		}
		fallback, err := resolveClaudeRewriteTarget(cfg, "lmstudio", "", "", "liquid/lfm2.5-1.2b")
		if err != nil {
			return claudeRewriteTargetConfig{}, nil, err
		}
		return primary, &fallback, nil
	}
	primary, err := resolveClaudeRewriteTarget(cfg, provider, baseURL, apiKey, model)
	if err != nil {
		return claudeRewriteTargetConfig{}, nil, err
	}
	return primary, nil, nil
}

func resolveClaudeRewriteTarget(cfg config.Config, provider, baseURL, apiKey, model string) (claudeRewriteTargetConfig, error) {
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
	resolvedModel := strings.TrimSpace(model)
	if resolvedModel == "" {
		resolvedModel = cfg.LLM.ResolveModel(resolvedProvider)
	}
	if strings.TrimSpace(resolvedModel) == "" {
		return claudeRewriteTargetConfig{}, fmt.Errorf("model is required")
	}
	return claudeRewriteTargetConfig{
		Provider: resolvedProvider,
		BaseURL:  resolvedBaseURL,
		APIKey:   resolvedAPIKey,
		Model:    resolvedModel,
	}, nil
}

func resolveClaudeLeaderboardTargets(defaultProvider string, models, targets []string) ([]claudeLeaderboardTarget, error) {
	out := make([]claudeLeaderboardTarget, 0, len(models)+len(targets))
	defaultProvider = strings.TrimSpace(defaultProvider)
	if defaultProvider == "" {
		defaultProvider = "lmstudio"
	}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		out = append(out, claudeLeaderboardTarget{Provider: defaultProvider, Model: model})
	}
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		parts := strings.SplitN(target, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid --target %q; expected provider:model", target)
		}
		out = append(out, claudeLeaderboardTarget{
			Provider: strings.TrimSpace(parts[0]),
			Model:    strings.TrimSpace(parts[1]),
		})
	}
	return out, nil
}

func sanitizeClaudeLeaderboardName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "model"
	}
	var builder strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	out := strings.Trim(builder.String(), "_")
	if out == "" {
		return "model"
	}
	return out
}

func saveClaudeRewriteLeaderboardReport(path string, data map[string]any) error {
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

type sessionFeedbackLabel struct {
	SessionID string    `json:"session_id,omitempty"`
	Rating    int       `json:"rating"`
	Outcome   string    `json:"outcome"`
	Notes     string    `json:"notes,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func exportClaudeProjectDataset(
	ctx context.Context,
	sessionStore storage.SessionStore,
	memStore storage.MemoryStore,
	workspacePath, claudeHome string,
	force bool,
	dryRun bool,
	limit int,
	includeTools, includeFiles, includeFeedback bool,
	signalProfile string,
	datasetMode string,
) ([]optimization.TranscriptTrainingExample, int, int, error) {
	projectDir := claudeProjectDirForHome(workspacePath, claudeHome)
	entries, readErr := os.ReadDir(projectDir)
	if readErr != nil {
		return nil, 0, 0, readErr
	}

	feedbackBySession := map[string]sessionFeedbackLabel{}
	if includeFeedback && memStore != nil {
		labels, err := loadClaudeFeedbackLabels(ctx, memStore, workspacePath, max(limit*10, 100))
		if err != nil {
			return nil, 0, 0, err
		}
		feedbackBySession = labels
	}

	type sessionFile struct {
		id      string
		path    string
		modTime time.Time
	}
	files := make([]sessionFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") || strings.HasPrefix(entry.Name(), "agent-") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, sessionFile{
			id:      strings.TrimSuffix(entry.Name(), ".jsonl"),
			path:    filepath.Join(projectDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })

	examples := make([]optimization.TranscriptTrainingExample, 0, limit)
	ingested, scanned := 0, 0
	for _, file := range files {
		if limit > 0 && len(examples) >= limit {
			break
		}
		scanned++

		reader, openErr := claudejsonl.OpenReader(file.path)
		if openErr != nil {
			continue
		}
		messages, readAllErr := reader.ReadAll()
		_ = reader.Close()
		if readAllErr != nil {
			continue
		}

		session := buildClaudeSessionFromMessages(file.id, file.path, workspacePath, messages)
		shouldIngest := !dryRun && (force || !claudeSessionExists(ctx, sessionStore, file.id))
		if force && !dryRun {
			_ = sessionStore.Delete(ctx, file.id)
		}
		if shouldIngest {
			if _, saveErr := sessionStore.Save(ctx, session); saveErr == nil {
				ingested++
				for _, turn := range buildClaudeTurns(session.ID, messages) {
					_, _ = sessionStore.SaveTurn(ctx, turn)
				}
			}
		}

		examples = append(examples, buildClaudeExamplesFromMessages(session, messages, feedbackBySession[file.id], includeTools, includeFiles, signalProfile, datasetMode)...)
	}
	if limit > 0 && len(examples) > limit {
		examples = examples[:limit]
	}
	return examples, ingested, scanned, nil
}

func claudeSessionExists(ctx context.Context, sessionStore storage.SessionStore, sessionID string) bool {
	if sessionStore == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	existing, err := sessionStore.Get(ctx, sessionID)
	return err == nil && strings.TrimSpace(existing.ID) != ""
}

func loadClaudeFeedbackLabels(ctx context.Context, memStore storage.MemoryStore, workspace string, limit int) (map[string]sessionFeedbackLabel, error) {
	entries, err := memStore.List(ctx, workspace, limit)
	if err != nil {
		return nil, err
	}
	out := map[string]sessionFeedbackLabel{}
	for _, entry := range entries {
		if entry.Type != "session_feedback" {
			continue
		}
		var label sessionFeedbackLabel
		if err := json.Unmarshal(entry.Result, &label); err != nil {
			continue
		}
		if strings.TrimSpace(label.SessionID) == "" {
			continue
		}
		current, ok := out[label.SessionID]
		if !ok || label.Timestamp.After(current.Timestamp) {
			out[label.SessionID] = label
		}
	}
	return out, nil
}

func claudeProjectDirForHome(workspacePath, claudeHome string) string {
	if strings.TrimSpace(claudeHome) == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(homeDir) == "" {
			homeDir = os.TempDir()
		}
		claudeHome = filepath.Join(homeDir, ".claude")
	}
	dashPath := strings.ReplaceAll(workspacePath, string(filepath.Separator), "-")
	projectDir := filepath.Join(claudeHome, "projects", dashPath)
	if _, err := os.Stat(projectDir); err == nil {
		return projectDir
	}
	hash := sha256.Sum256([]byte(workspacePath))
	workspaceHash := fmt.Sprintf("%x", hash)[:16]
	return filepath.Join(claudeHome, "projects", workspaceHash)
}

func buildClaudeSessionFromMessages(sessionID, rawPath, workspace string, messages []*claudejsonl.ReadMessage) storage.Session {
	session := storage.Session{
		ID:            sessionID,
		WorkspacePath: workspace,
		RawJSONLPath:  rawPath,
		AgentType:     "claude",
	}

	var minTime, maxTime time.Time
	toolSet := make(map[string]struct{})
	for _, rm := range messages {
		if rm == nil || rm.Message == nil {
			continue
		}
		msg := rm.Message
		session.MessageCount++
		if !rm.Timestamp.IsZero() {
			if minTime.IsZero() || rm.Timestamp.Before(minTime) {
				minTime = rm.Timestamp
			}
			if maxTime.IsZero() || rm.Timestamp.After(maxTime) {
				maxTime = rm.Timestamp
			}
		}
		switch claudejsonl.Classify(msg) {
		case claudejsonl.ChunkTypeUserRequest:
			session.UserTurns++
		case claudejsonl.ChunkTypeToolUse:
			session.ToolInvocations++
		case claudejsonl.ChunkTypeAssistantResponse:
			for _, tool := range claudejsonl.ExtractTools(msg) {
				toolSet[tool] = struct{}{}
				session.ToolInvocations++
			}
		}
		if msg.Message != nil {
			var nested struct {
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal(msg.Message, &nested) == nil && nested.Usage != nil {
				session.TotalTokens += nested.Usage.InputTokens + nested.Usage.OutputTokens
			}
		}
	}
	if !minTime.IsZero() {
		session.StartedAt = minTime
	}
	if !maxTime.IsZero() {
		session.EndedAt = maxTime
	}
	session.ProjectName = filepath.Base(workspace)
	if len(toolSet) > 0 {
		tools := make([]string, 0, len(toolSet))
		for tool := range toolSet {
			tools = append(tools, tool)
		}
		sort.Strings(tools)
		session.ToolsPattern = strings.Join(tools, ", ")
	}
	return session
}

func buildClaudeTurns(sessionID string, messages []*claudejsonl.ReadMessage) []storage.SessionTurn {
	importTime := deterministicClaudeTurnTimestamp(messages)
	turns := make([]storage.SessionTurn, 0, len(messages))
	turnIndex := 0
	for _, rm := range messages {
		if rm == nil || rm.Message == nil {
			continue
		}
		msg := rm.Message
		chunkType := claudejsonl.Classify(msg)
		if chunkType == claudejsonl.ChunkTypeOther || chunkType == claudejsonl.ChunkTypeCompactBoundary || chunkType == claudejsonl.ChunkTypeToolOutput {
			continue
		}

		role := "assistant"
		if chunkType == claudejsonl.ChunkTypeUserRequest {
			role = "user"
		}
		turnIndex++
		turn := storage.SessionTurn{
			ID:             fmt.Sprintf("%s-turn-%d", sessionID, turnIndex),
			SessionID:      sessionID,
			TurnIndex:      turnIndex,
			Role:           role,
			ContentPreview: claudejsonl.ExtractPreview(msg, 400),
			HasError:       chunkType == claudejsonl.ChunkTypeError,
			ErrorType:      claudejsonl.ExtractErrorType(msg),
			Timestamp:      rm.Timestamp,
			CreatedAt:      rm.Timestamp,
			ToolCalls:      buildClaudeToolCalls(msg),
		}
		if turn.Timestamp.IsZero() {
			turn.Timestamp = importTime
		}
		if turn.CreatedAt.IsZero() {
			turn.CreatedAt = turn.Timestamp
		}
		if len(turn.ToolCalls) == 0 && role == "assistant" && strings.TrimSpace(turn.ContentPreview) == "" {
			continue
		}
		turns = append(turns, turn)
	}
	return turns
}

func deterministicClaudeTurnTimestamp(messages []*claudejsonl.ReadMessage) time.Time {
	for _, rm := range messages {
		if rm != nil && !rm.Timestamp.IsZero() {
			return rm.Timestamp
		}
	}
	return time.Unix(0, 0).UTC()
}

func buildClaudeToolCalls(msg *claudejsonl.Message) []storage.ToolCall {
	tools := claudejsonl.ExtractTools(msg)
	if len(tools) == 0 {
		return nil
	}
	calls := make([]storage.ToolCall, 0, len(tools))
	for _, tool := range tools {
		calls = append(calls, storage.ToolCall{Name: tool})
	}
	return calls
}

func buildClaudeExamplesFromMessages(
	session storage.Session,
	messages []*claudejsonl.ReadMessage,
	feedback sessionFeedbackLabel,
	includeTools, includeFiles bool,
	signalProfile string,
	datasetMode string,
) []optimization.TranscriptTrainingExample {
	examples := make([]optimization.TranscriptTrainingExample, 0, 8)
	var currentUser string
	var currentTools []string
	var currentFiles []string
	turnIndex := 0
	var currentUserTS time.Time

	for _, rm := range messages {
		if rm == nil || rm.Message == nil {
			continue
		}
		msg := rm.Message
		switch claudejsonl.Classify(msg) {
		case claudejsonl.ChunkTypeUserRequest:
			currentUser = strings.TrimSpace(claudejsonl.ExtractPreview(msg, 400))
			currentTools = nil
			currentFiles = nil
			turnIndex++
			currentUserTS = rm.Timestamp
		case claudejsonl.ChunkTypeToolUse:
			currentTools = append(currentTools, claudejsonl.ExtractTools(msg)...)
			currentFiles = append(currentFiles, extractClaudeFiles(msg)...)
		case claudejsonl.ChunkTypeAssistantResponse:
			if strings.TrimSpace(currentUser) == "" {
				continue
			}
			response := strings.TrimSpace(claudejsonl.ExtractPreview(msg, 400))
			currentTools = append(currentTools, claudejsonl.ExtractTools(msg)...)
			currentFiles = append(currentFiles, extractClaudeFiles(msg)...)
			if response == "" {
				continue
			}
			if !shouldKeepClaudeTrainingPairForProfile(currentUser, response, signalProfile) {
				currentUser = ""
				currentTools = nil
				currentFiles = nil
				continue
			}
			exampleTS := feedback.Timestamp
			if exampleTS.IsZero() {
				if !rm.Timestamp.IsZero() {
					exampleTS = rm.Timestamp
				} else {
					exampleTS = currentUserTS
				}
			}
			example := optimization.TranscriptTrainingExample{
				Input: optimization.TranscriptTrainingInput{
					UserRequest: currentUser,
				},
				Output: optimization.TranscriptTrainingOutput{
					Response: response,
				},
				Metadata: optimization.TranscriptTrainingMetadata{
					SessionID:    session.ID,
					AgentType:    session.AgentType,
					ProjectName:  session.ProjectName,
					RawJSONLPath: session.RawJSONLPath,
					TurnIndex:    turnIndex,
					Prompt:       session.Prompt,
					PromptHash:   session.PromptHash,
					LLMProvider:  session.LLMProvider,
					LLMModel:     session.LLMModel,
					Rating:       feedback.Rating,
					Outcome:      feedback.Outcome,
					Notes:        feedback.Notes,
					Timestamp:    exampleTS,
				},
			}
			example.Metadata.Category = optimization.CategorizeTranscriptUserRequest(example.Input.UserRequest, example.Output.Response)
			if includeTools && len(currentTools) > 0 {
				example.Output.ToolsUsed = uniqueClaudeStrings(currentTools)
			}
			if includeFiles && len(currentFiles) > 0 {
				example.Input.Files = uniqueClaudeStrings(currentFiles)
			}
			if !shouldKeepClaudeExampleForMode(example, datasetMode) {
				currentUser = ""
				currentTools = nil
				currentFiles = nil
				continue
			}
			examples = append(examples, example)
			currentUser = ""
			currentTools = nil
			currentFiles = nil
		}
	}
	return examples
}

func extractClaudeFiles(msg *claudejsonl.Message) []string {
	anchors := claudejsonl.ExtractFilePaths(msg)
	if len(anchors) == 0 {
		return nil
	}
	files := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		if path := strings.TrimSpace(anchor.Meta.FilePath); path != "" {
			files = append(files, path)
		}
	}
	return files
}

func shouldKeepClaudeTrainingPair(userRequest, response string) bool {
	return shouldKeepClaudeTrainingPairForProfile(userRequest, response, "general")
}

func shouldKeepClaudeTrainingPairForProfile(userRequest, response, signalProfile string) bool {
	userRequest = strings.TrimSpace(userRequest)
	response = strings.TrimSpace(response)
	if userRequest == "" || response == "" {
		return false
	}

	lower := strings.ToLower(userRequest)
	assistantLower := strings.ToLower(response)

	prefixDrops := []string{
		"# mcp builder mode",
		"base directory for this skill:",
		"# plan build",
		"you are a builder agent",
		"you are chatting in the context of an agent session",
	}
	for _, prefix := range prefixDrops {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}

	containsDrops := []string{
		"mcp builder mode",
		"base directory for this skill:",
		"repo prompt context builder",
		"return a comprehensive overview of the v2 architecture",
		"prompt-ready continuity context",
		"this session is being continued from a previous conversation",
		"<task-notification>",
		"read the output file to retrieve the result",
		"background command",
	}
	for _, marker := range containsDrops {
		if strings.Contains(lower, marker) {
			return false
		}
	}

	if strings.HasPrefix(assistantLower, "i have rich context from the repoprompt context builder") {
		return false
	}

	if len([]rune(userRequest)) > 2000 {
		return false
	}

	switch normalizeClaudeSignalProfile(signalProfile) {
	case "coder":
		if isClaudeContinuationStub(lower) {
			return false
		}
		if isClaudeOperationalChatter(lower, assistantLower) {
			return false
		}
		if !hasClaudeCoderSignal(lower, assistantLower) {
			return false
		}
	case "coder-strong":
		if isClaudeContinuationStub(lower) {
			return false
		}
		if isClaudeOperationalChatter(lower, assistantLower) {
			return false
		}
		if !hasClaudeCoderStrongSignal(lower, assistantLower) {
			return false
		}
	}

	return true
}

func normalizeClaudeSignalProfile(profile string) string {
	profile = strings.ToLower(strings.TrimSpace(profile))
	switch profile {
	case "", "general":
		return "general"
	case "coder":
		return "coder"
	case "coder-strong":
		return "coder-strong"
	default:
		return "general"
	}
}

func normalizeClaudeDatasetMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "standalone":
		return "standalone"
	case "continuation":
		return "continuation"
	case "ops":
		return "ops"
	default:
		return "standalone"
	}
}

func isClaudeContinuationStub(userLower string) bool {
	stubs := []string{
		"continue",
		"loaded",
		"lets do 1",
		"let's do 1",
		"keep it simple",
		"lets test manually",
		"let's test manually",
	}
	for _, stub := range stubs {
		if userLower == stub {
			return true
		}
	}
	return false
}

func isClaudeOperationalChatter(userLower, assistantLower string) bool {
	markers := []string{
		"commit it",
		"create an mr",
		"check the job",
		"check mr",
		"lets merge",
		"let's merge",
		"merge request",
		"pull request",
		"gitlab.com",
		"github.com",
		"pipeline passed",
		"ci jobs",
		"coderabbit",
		"greptile",
		"retart the daemon",
		"restart the daemon",
		"daemon restarted",
	}
	for _, marker := range markers {
		if strings.Contains(userLower, marker) || strings.Contains(assistantLower, marker) {
			return true
		}
	}
	return false
}

func hasClaudeCoderSignal(userLower, assistantLower string) bool {
	markers := []string{
		"review",
		"debug",
		"error",
		"fix",
		"build",
		"test",
		"diff",
		"format",
		"lint",
		"embedding",
		"recall",
		"annotation",
		"retrieval",
		"context",
		"prompt",
		"tool",
		"worker",
		"queue",
		"sqlite",
		"session",
		"scout",
		"agent",
		"implementation",
		"architecture",
		"design",
		"schema",
		"api",
		"query",
		"memory",
	}
	for _, marker := range markers {
		if strings.Contains(userLower, marker) || strings.Contains(assistantLower, marker) {
			return true
		}
	}
	return false
}

func hasClaudeCoderStrongSignal(userLower, assistantLower string) bool {
	markers := []string{
		"review",
		"implementation",
		"architecture",
		"design",
		"schema",
		"api",
		"debug",
		"error",
		"build",
		"embedding",
		"annotation",
		"recall",
		"retrieval",
		"session_chunks",
		"chunk_granularity",
		"worker",
		"queue",
		"specialized",
		"longer term fix",
		"ux",
		"gui",
	}
	count := 0
	for _, marker := range markers {
		if strings.Contains(userLower, marker) || strings.Contains(assistantLower, marker) {
			count++
		}
	}
	return count >= 1
}

func shouldKeepClaudeExampleForMode(example optimization.TranscriptTrainingExample, datasetMode string) bool {
	mode := normalizeClaudeDatasetMode(datasetMode)
	category := optimization.NormalizeTranscriptCategory(example.Metadata.Category)
	switch mode {
	case "continuation":
		return category == "continuation"
	case "ops":
		return category == "ops_infra"
	default:
		if category == "continuation" || category == "release_workflow" {
			return false
		}
		return category == "coder_impl"
	}
}

func uniqueClaudeStrings(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
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
	sort.Strings(out)
	return out
}

func collectClaudeRawPairs(workspacePath, claudeHome string, limit int) ([]claudeRawPair, int, error) {
	projectDir := claudeProjectDirForHome(workspacePath, claudeHome)
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, 0, err
	}

	type sessionFile struct {
		id      string
		path    string
		modTime time.Time
	}
	files := make([]sessionFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") || strings.HasPrefix(entry.Name(), "agent-") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, sessionFile{
			id:      strings.TrimSuffix(entry.Name(), ".jsonl"),
			path:    filepath.Join(projectDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })

	pairs := make([]claudeRawPair, 0, limit)
	scanned := 0
	for _, file := range files {
		if limit > 0 && len(pairs) >= limit {
			break
		}
		reader, openErr := claudejsonl.OpenReader(file.path)
		if openErr != nil {
			continue
		}
		messages, readAllErr := reader.ReadAll()
		_ = reader.Close()
		if readAllErr != nil {
			continue
		}
		session := buildClaudeSessionFromMessages(file.id, file.path, workspacePath, messages)
		rawPairs := buildClaudeRawPairsFromMessages(session, messages)
		scanned += len(rawPairs)
		pairs = append(pairs, rawPairs...)
		if limit > 0 && len(pairs) >= limit {
			pairs = pairs[:limit]
			break
		}
	}
	return pairs, scanned, nil
}

func buildClaudeRawPairsFromMessages(session storage.Session, messages []*claudejsonl.ReadMessage) []claudeRawPair {
	pairs := make([]claudeRawPair, 0, 8)
	var currentUser string
	var currentTools []string
	var currentTS time.Time
	for _, rm := range messages {
		if rm == nil || rm.Message == nil {
			continue
		}
		msg := rm.Message
		switch claudejsonl.Classify(msg) {
		case claudejsonl.ChunkTypeUserRequest:
			currentUser = strings.TrimSpace(claudejsonl.ExtractPreview(msg, 4000))
			currentTools = nil
			currentTS = rm.Timestamp
		case claudejsonl.ChunkTypeToolUse:
			currentTools = append(currentTools, claudejsonl.ExtractTools(msg)...)
		case claudejsonl.ChunkTypeAssistantResponse:
			if strings.TrimSpace(currentUser) == "" {
				continue
			}
			response := strings.TrimSpace(claudejsonl.ExtractPreview(msg, 2000))
			currentTools = append(currentTools, claudejsonl.ExtractTools(msg)...)
			if response == "" {
				continue
			}
			ts := rm.Timestamp
			if ts.IsZero() {
				ts = currentTS
			}
			pairs = append(pairs, claudeRawPair{
				SessionID:         session.ID,
				RawJSONLPath:      session.RawJSONLPath,
				UserRequest:       currentUser,
				AssistantResponse: response,
				ToolsUsed:         uniqueClaudeStrings(currentTools),
				Timestamp:         ts,
			})
			currentUser = ""
			currentTools = nil
		}
	}
	return pairs
}

func filterClaudeRawPairsDeterministic(pairs []claudeRawPair, signalProfile string) []claudeRawPair {
	filtered := make([]claudeRawPair, 0, len(pairs))
	for _, pair := range pairs {
		if !shouldKeepClaudeTrainingPairForProfile(pair.UserRequest, pair.AssistantResponse, signalProfile) {
			continue
		}
		filtered = append(filtered, pair)
	}
	return filtered
}

func isFaithfulClaudeRewrite(pair claudeRawPair, rewritten claudeRewriteContent) bool {
	return isFaithfulClaudeRewriteWithConfig(pair, rewritten, defaultClaudeRewriteFidelityConfig())
}

func isFaithfulClaudeRewriteWithConfig(pair claudeRawPair, rewritten claudeRewriteContent, cfg claudeRewriteFidelityConfig) bool {
	if !cfg.Enabled {
		return true
	}
	fidelity := analyzeClaudeRewriteFidelity(pair, rewritten)
	if strings.TrimSpace(rewritten.CleanUserRequest) == "" || strings.TrimSpace(rewritten.CleanAssistantResponse) == "" {
		return false
	}
	if fidelity.ContainsPlaceholder {
		return false
	}
	if fidelity.UserSame == 0 || fidelity.AssistantSame == 0 {
		return false
	}
	if fidelity.UserSame < fidelity.UserCross {
		return false
	}
	if fidelity.AssistantSame < fidelity.AssistantCross {
		return false
	}
	if fidelity.UserNovelRatio > cfg.MaxUserNovelRatio {
		return false
	}
	if fidelity.AssistantNovelRatio > cfg.MaxAssistantNovelRatio {
		return false
	}
	if fidelity.AssistantNovelRatio > cfg.AssistantLowOverlapNovelRatio &&
		fidelity.AssistantSame < cfg.AssistantLowOverlapThreshold {
		return false
	}
	return true
}

func analyzeClaudeRewriteFidelity(pair claudeRawPair, rewritten claudeRewriteContent) claudeRewriteFidelityAnalysis {
	cleanUser := strings.TrimSpace(rewritten.CleanUserRequest)
	cleanAssistant := strings.TrimSpace(rewritten.CleanAssistantResponse)
	return claudeRewriteFidelityAnalysis{
		ContainsPlaceholder: containsClaudeRewritePlaceholder(cleanUser) || containsClaudeRewritePlaceholder(cleanAssistant),
		UserSame:            tokenOverlapRatio(cleanUser, pair.UserRequest),
		UserCross:           tokenOverlapRatio(cleanUser, pair.AssistantResponse),
		AssistantSame:       tokenOverlapRatio(cleanAssistant, pair.AssistantResponse),
		AssistantCross:      tokenOverlapRatio(cleanAssistant, pair.UserRequest),
		UserNovelRatio:      tokenNovelRatio(cleanUser, pair.UserRequest),
		AssistantNovelRatio: tokenNovelRatio(cleanAssistant, pair.AssistantResponse),
	}
}

func classifyClaudeRewriteFailure(err error, rawModelOutput string) string {
	if err == nil {
		return ""
	}
	if strings.TrimSpace(rawModelOutput) != "" {
		return "parse_error"
	}
	return "request_error"
}

func containsClaudeRewritePlaceholder(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return true
	}
	placeholders := []string{
		"...",
		"thinking process:",
		"return json only",
		"clean_user_request",
		"clean_assistant_response",
	}
	for _, marker := range placeholders {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func tokenOverlapRatio(candidate, original string) float64 {
	candidateSet := significantTokenSet(candidate)
	originalSet := significantTokenSet(original)
	if len(candidateSet) == 0 || len(originalSet) == 0 {
		return 0
	}
	shared := 0
	for token := range candidateSet {
		if _, ok := originalSet[token]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(candidateSet))
}

func tokenNovelRatio(candidate, original string) float64 {
	candidateSet := significantTokenSet(candidate)
	originalSet := significantTokenSet(original)
	if len(candidateSet) == 0 {
		return 1
	}
	novel := 0
	for token := range candidateSet {
		if _, ok := originalSet[token]; !ok {
			novel++
		}
	}
	return float64(novel) / float64(len(candidateSet))
}

func significantTokenSet(text string) map[string]struct{} {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-')
	})
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		token := strings.Trim(field, "_-")
		if !isSignificantClaudeToken(token) {
			continue
		}
		set[token] = struct{}{}
	}
	return set
}

func isSignificantClaudeToken(token string) bool {
	if token == "" {
		return false
	}
	stopwords := map[string]struct{}{
		"the": {}, "and": {}, "that": {}, "this": {}, "with": {}, "then": {}, "they": {}, "them": {},
		"from": {}, "into": {}, "just": {}, "have": {}, "what": {}, "when": {}, "were": {}, "your": {},
		"will": {}, "once": {}, "only": {}, "same": {}, "here": {}, "lets": {}, "let": {}, "can": {},
		"you": {}, "for": {}, "are": {}, "was": {}, "its": {}, "it": {}, "ill": {}, "now": {}, "due": {},
	}
	if _, ok := stopwords[token]; ok {
		return false
	}
	if len(token) >= 3 {
		return true
	}
	for _, r := range token {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func rewriteClaudeRawPair(ctx context.Context, provider, baseURL, apiKey, model string, pair claudeRawPair, reasoning claudeRewriteReasoningConfig) (claudeRewriteContent, string, error) {
	systemPrompt := `You clean already-approved AI coding assistant transcript pairs into training data.
Return JSON only with this exact schema:
{"clean_user_request":"...","clean_assistant_response":"...","reason":"...","tags":["..."]}

Rules:
- the pair is already approved for training use; do not reject it
- preserve technical meaning
- remove only residual orchestration noise that survived deterministic filtering
- keep short direct questions as questions
- keep concise assistant answers concise
- do not invent facts

Examples:
1. user: "Was there m3 and m4?"
assistant: "Yes — M3 is episodes, M4 is narrative/self model."

2. user: "Implement the following plan..."
assistant: "I’ll start by wiring the store changes..."
`

	userPrompt := fmt.Sprintf("Approved user request:\n%s\n\nApproved assistant response:\n%s\n\nTools used:\n%s\n",
		strings.TrimSpace(pair.UserRequest),
		strings.TrimSpace(pair.AssistantResponse),
		strings.Join(pair.ToolsUsed, ", "),
	)
	systemPrompt = llmcompat.ApplySystemPromptDefaults(model, systemPrompt)
	raw, err := openAICompatRawChat(ctx, provider, baseURL, apiKey, model, systemPrompt, userPrompt, 900, 0, reasoning)
	if err != nil {
		return claudeRewriteContent{}, "", err
	}
	rewritten, err := parseClaudeRewriteContent(raw)
	return rewritten, strings.TrimSpace(raw), err
}

func parseClaudeRewriteContent(raw string) (claudeRewriteContent, error) {
	raw = strings.TrimSpace(raw)
	for _, candidate := range extractJSONObjectCandidates(raw) {
		var rewritten claudeRewriteContent
		if err := json.Unmarshal([]byte(candidate), &rewritten); err != nil {
			continue
		}
		rewritten.CleanUserRequest = strings.TrimSpace(rewritten.CleanUserRequest)
		rewritten.CleanAssistantResponse = strings.TrimSpace(rewritten.CleanAssistantResponse)
		if rewritten.CleanUserRequest == "" || rewritten.CleanAssistantResponse == "" {
			continue
		}
		return rewritten, nil
	}
	return claudeRewriteContent{}, fmt.Errorf("rewrite response did not contain a valid content JSON object")
}

func extractJSONObjectCandidates(raw string) []string {
	candidates := []string{}
	for start := 0; start < len(raw); start++ {
		if raw[start] != '{' {
			continue
		}
		depth := 0
		inString := false
		escaped := false
		for end := start; end < len(raw); end++ {
			ch := raw[end]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if ch == '\\' {
					escaped = true
					continue
				}
				if ch == '"' {
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					candidates = append(candidates, raw[start:end+1])
					end = len(raw)
				}
			}
		}
	}
	return candidates
}

type rawOpenAICompatResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func openAICompatRawChat(ctx context.Context, provider, baseURL, apiKey, model, systemPrompt, userPrompt string, maxTokens int, temperature float64, reasoning claudeRewriteReasoningConfig) (string, error) {
	if strings.TrimSpace(apiKey) == "" && strings.EqualFold(provider, "lmstudio") {
		apiKey = "lm-studio"
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "http://localhost:1234/v1"
	}
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": temperature,
		"max_tokens":  maxTokens,
	}
	if strings.EqualFold(strings.TrimSpace(provider), "openrouter") && (strings.TrimSpace(reasoning.Effort) != "" || reasoning.Exclude) {
		reasoningBody := map[string]any{}
		if strings.TrimSpace(reasoning.Effort) != "" {
			reasoningBody["effort"] = strings.TrimSpace(reasoning.Effort)
		}
		if reasoning.Exclude {
			reasoningBody["exclude"] = true
		}
		body["reasoning"] = reasoningBody
	}
	llmcompat.ApplyOpenAICompatibleRequestDefaults(model, body)
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("rewrite LLM error (status %d): %s", resp.StatusCode, string(respBody))
	}
	var parsed rawOpenAICompatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return "", fmt.Errorf("%s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("rewrite LLM returned no choices")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content != "" {
		return content, nil
	}
	return strings.TrimSpace(parsed.Choices[0].Message.ReasoningContent), nil
}

func saveClaudeRewriteRecords(path string, records []claudeRewriteRecord) error {
	var builder strings.Builder
	enc := json.NewEncoder(&builder)
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func saveClaudeRewriteDebugRecords(path string, records []claudeRewriteDebugRecord) error {
	var builder strings.Builder
	enc := json.NewEncoder(&builder)
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func persistClaudeRewriteRecords(ctx context.Context, casRoot string, records []claudeRewriteRecord) (string, error) {
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()
	var builder strings.Builder
	enc := json.NewEncoder(&builder)
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			return "", err
		}
	}
	obj, err := store.Put(ctx, strings.NewReader(builder.String()), "application/jsonl", []string{"gepa", "claude-rewrite"})
	if err != nil {
		return "", err
	}
	return obj.Digest, nil
}
