package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/cmd/foxctl/cmd/sessionscmd"
	"github.com/joshka0/foxctl/internal/context/transcriptpipeline"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/dreamer"
	"github.com/joshka0/foxctl/internal/storage/transcriptcache"
	"github.com/spf13/cobra"
)

type dreamCommandFlags struct {
	Workspace          string
	CodexHome          string
	ClaudeDir          string
	PiRoot             string
	HermesRoot         string
	VaultPath          string
	BatchSize          int
	Concurrency        int
	MaxAttempts        int
	RetryDelay         time.Duration
	ProcessingTimeout  time.Duration
	Interval           time.Duration
	Duration           time.Duration
	FrameLimit         int
	BlobSummaryMode    string
	BlobSummaryModel   string
	BlobSummaryTimeout time.Duration
	DryRun             bool
	WriteDreamNotes    bool
}

func newDreamCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dream",
		Short: "Run detached transcript dreaming into durable memory",
	}
	cmd.AddCommand(newDreamScanCommand(), newDreamRunOnceCommand(), newDreamWatchCommand())
	return cmd
}

func newDreamScanCommand() *cobra.Command {
	flags := defaultDreamCommandFlags()
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Preview transcript sources without consuming dream work",
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := previewDreamSources(cmd.Context(), flags)
			if err != nil {
				return err
			}
			return sessionscmd.WriteOK(cmd.OutOrStdout(), "foxctl.dream.scan", report)
		},
	}
	bindDreamScanFlags(cmd, &flags)
	return cmd
}

func newDreamRunOnceCommand() *cobra.Command {
	flags := defaultDreamCommandFlags()
	cmd := &cobra.Command{
		Use:   "run-once",
		Short: "Scan transcript sources and process one bounded dream batch",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDreamCommand(cmd, flags, false)
		},
	}
	bindDreamFlags(cmd, &flags, false)
	return cmd
}

func newDreamWatchCommand() *cobra.Command {
	flags := defaultDreamCommandFlags()
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Run detached transcript dreaming until canceled or duration expires",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDreamCommand(cmd, flags, true)
		},
	}
	bindDreamFlags(cmd, &flags, true)
	return cmd
}

func defaultDreamCommandFlags() dreamCommandFlags {
	return dreamCommandFlags{
		CodexHome:          "~/.codex",
		ClaudeDir:          "~/.claude/projects",
		BatchSize:          20,
		Concurrency:        1,
		MaxAttempts:        transcriptcache.DefaultSourceMaxAttempts,
		RetryDelay:         time.Hour,
		ProcessingTimeout:  4 * time.Hour,
		Interval:           10 * time.Minute,
		FrameLimit:         20,
		BlobSummaryMode:    "auto",
		BlobSummaryModel:   transcriptpipeline.DefaultWorkerModel,
		BlobSummaryTimeout: 45 * time.Second,
	}
}

func bindDreamScanFlags(cmd *cobra.Command, flags *dreamCommandFlags) {
	cmd.Flags().StringVar(&flags.Workspace, "workspace", "", "Workspace path hint")
	cmd.Flags().StringVar(&flags.CodexHome, "codex-home", flags.CodexHome, "Codex home directory")
	cmd.Flags().StringVar(&flags.ClaudeDir, "claude-dir", flags.ClaudeDir, "Claude projects/transcript root")
	cmd.Flags().StringVar(&flags.PiRoot, "pi-root", "", "Pi transcript root")
	cmd.Flags().StringVar(&flags.HermesRoot, "hermes-root", "", "Hermes transcript root")
}

func bindDreamFlags(cmd *cobra.Command, flags *dreamCommandFlags, includeWatch bool) {
	cmd.Flags().StringVar(&flags.Workspace, "workspace", "", "Workspace path hint")
	cmd.Flags().StringVar(&flags.CodexHome, "codex-home", flags.CodexHome, "Codex home directory")
	cmd.Flags().StringVar(&flags.ClaudeDir, "claude-dir", flags.ClaudeDir, "Claude projects/transcript root")
	cmd.Flags().StringVar(&flags.PiRoot, "pi-root", "", "Pi transcript root")
	cmd.Flags().StringVar(&flags.HermesRoot, "hermes-root", "", "Hermes transcript root")
	cmd.Flags().StringVar(&flags.VaultPath, "vault-path", "", "Obsidian vault path for dream notes")
	cmd.Flags().IntVar(&flags.BatchSize, "batch-size", flags.BatchSize, "Maximum queued transcript sources to process per pass")
	cmd.Flags().IntVar(&flags.Concurrency, "concurrency", flags.Concurrency, "Maximum concurrent transcript dream processors")
	cmd.Flags().IntVar(&flags.MaxAttempts, "max-attempts", flags.MaxAttempts, "Maximum processing attempts per source fingerprint")
	cmd.Flags().DurationVar(&flags.RetryDelay, "retry-delay", flags.RetryDelay, "Delay before retrying failed sources")
	cmd.Flags().DurationVar(&flags.ProcessingTimeout, "processing-timeout", flags.ProcessingTimeout, "Age after which in-flight processing is considered stale and retryable")
	cmd.Flags().IntVar(&flags.FrameLimit, "frame-limit", flags.FrameLimit, "Maximum anchored frames to derive per transcript")
	cmd.Flags().StringVar(&flags.BlobSummaryMode, "blob-summary-mode", flags.BlobSummaryMode, "Reference blob summary mode: auto, deterministic, or lmstudio")
	cmd.Flags().StringVar(&flags.BlobSummaryModel, "blob-summary-model", flags.BlobSummaryModel, "Model to use for one-shot reference blob summaries")
	cmd.Flags().DurationVar(&flags.BlobSummaryTimeout, "blob-summary-timeout", flags.BlobSummaryTimeout, "Timeout for one-shot reference blob summaries")
	cmd.Flags().BoolVar(&flags.DryRun, "dry-run", false, "Preview transcript source discovery without deriving memory or consuming ledger work")
	cmd.Flags().BoolVar(&flags.WriteDreamNotes, "write-dream-notes", false, "Write Obsidian transcript dream notes for persisted history")
	if includeWatch {
		cmd.Flags().DurationVar(&flags.Interval, "interval", flags.Interval, "Watch interval between dream passes")
		cmd.Flags().DurationVar(&flags.Duration, "duration", 0, "Optional maximum watch duration")
	}
}

func runDreamCommand(cmd *cobra.Command, flags dreamCommandFlags, watch bool) error {
	return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
		if flags.DryRun {
			report, err := previewDreamSources(ctx, flags)
			if err != nil {
				return err
			}
			command := "foxctl.dream.run_once"
			if watch {
				command = "foxctl.dream.watch"
			}
			return sessionscmd.WriteOK(cmd.OutOrStdout(), command, report)
		}
		var onError func(error)
		if watch {
			onError = func(err error) {
				fmt.Fprintf(cmd.ErrOrStderr(), "dream watch pass failed: %v\n", err)
			}
		}
		worker, closeFn, err := buildDreamWorker(ctx, cfg, flags, onError)
		if err != nil {
			return err
		}
		defer func() { _ = closeFn() }()

		if !watch {
			report, err := worker.RunOnce(ctx)
			if err != nil {
				return err
			}
			return sessionscmd.WriteOK(cmd.OutOrStdout(), "foxctl.dream.run_once", report)
		}
		runCtx := ctx
		cancel := func() {}
		if flags.Duration > 0 {
			runCtx, cancel = context.WithTimeout(ctx, flags.Duration)
		}
		defer cancel()
		err = worker.Run(runCtx)
		if err != nil && runCtx.Err() == nil {
			return err
		}
		return sessionscmd.WriteOK(cmd.OutOrStdout(), "foxctl.dream.watch", map[string]any{
			"status": "stopped",
			"reason": runCtx.Err(),
		})
	})
}

func buildDreamWorker(ctx context.Context, cfg config.Config, flags dreamCommandFlags, onError func(error)) (*dreamer.Worker, func() error, error) {
	cacheStore, _, err := transcriptcache.OpenShared(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, nil, err
	}
	closeFns := []func() error{cacheStore.Close}
	scanner := dreamer.SourceScanner{Roots: dreamSourceRoots(flags)}
	ledger := dreamer.SourceLedger{
		Store:             cacheStore,
		MaxAttempts:       flags.MaxAttempts,
		FailureDelay:      flags.RetryDelay,
		ProcessingTimeout: flags.ProcessingTimeout,
	}

	runtime := transcriptpipeline.NewLocalModelRuntime(strings.TrimSpace(flags.BlobSummaryMode), strings.TrimSpace(flags.BlobSummaryModel), cfg.LLM.ResolveBaseURL("lmstudio"), flags.BlobSummaryTimeout)
	var noteWriter dreamer.DreamNoteWriter
	if flags.WriteDreamNotes {
		vaultPath := strings.TrimSpace(flags.VaultPath)
		if vaultPath != "" {
			noteWriter = dreamer.NewObsidianDreamNoteWriter(expandHomePath(vaultPath))
		}
	}
	processor, closeProcessor, err := dreamer.NewSingleInsightProcessorFromConfig(ctx, cfg, dreamer.SingleInsightProcessorConfig{
		Runtime:     runtime,
		ActorID:     "actor:system:dreamer",
		FrameLimit:  flags.FrameLimit,
		Embed:       dreamer.EmbedFunc(buildHistoryRecordEmbedder(cfg)),
		NoteWriter:  noteWriter,
		StorageRoot: cfg.Storage.Root,
		CASPath:     cfg.Paths.CAS,
	})
	if err != nil {
		_ = closeDreamResources(closeFns)
		return nil, nil, err
	}
	closeFns = append(closeFns, closeProcessor)
	worker, err := dreamer.NewWorker(dreamer.Config{
		Interval:    flags.Interval,
		BatchSize:   flags.BatchSize,
		Concurrency: flags.Concurrency,
		OnError:     onError,
	}, scanner, ledger, processor)
	if err != nil {
		_ = closeDreamResources(closeFns)
		return nil, nil, err
	}
	return worker, func() error { return closeDreamResources(closeFns) }, nil
}

func dreamSourceRoots(flags dreamCommandFlags) []transcriptpipeline.DreamSourceRoot {
	workspace := strings.TrimSpace(flags.Workspace)
	roots := []transcriptpipeline.DreamSourceRoot{
		{Provider: transcriptpipeline.DreamSourceProviderCodex, RootPath: expandHomePath(flags.CodexHome), WorkspaceHint: workspace},
		{Provider: transcriptpipeline.DreamSourceProviderClaude, RootPath: expandHomePath(flags.ClaudeDir), WorkspaceHint: workspace},
	}
	if strings.TrimSpace(flags.PiRoot) != "" {
		roots = append(roots, transcriptpipeline.DreamSourceRoot{Provider: transcriptpipeline.DreamSourceProviderPi, RootPath: expandHomePath(flags.PiRoot), WorkspaceHint: workspace})
	}
	if strings.TrimSpace(flags.HermesRoot) != "" {
		roots = append(roots, transcriptpipeline.DreamSourceRoot{Provider: transcriptpipeline.DreamSourceProviderHermes, RootPath: expandHomePath(flags.HermesRoot), WorkspaceHint: workspace})
	}
	return roots
}

func previewDreamSources(ctx context.Context, flags dreamCommandFlags) (dreamer.Report, error) {
	scanner := dreamer.SourceScanner{Roots: dreamSourceRoots(flags)}
	sources, err := scanner.Scan(ctx)
	if err != nil {
		return dreamer.Report{}, err
	}
	report := dreamer.Report{Discovered: len(sources)}
	for _, source := range sources {
		if source.Stable {
			report.Queued++
		} else {
			report.Skipped++
		}
	}
	return report, nil
}

func closeDreamResources(closeFns []func() error) error {
	var first error
	for i := len(closeFns) - 1; i >= 0; i-- {
		if closeFns[i] == nil {
			continue
		}
		if err := closeFns[i](); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func init() {
	rootCmd.AddCommand(newDreamCommand())
}
