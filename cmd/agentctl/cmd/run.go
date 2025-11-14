package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	errs "github.com/jkatigb/agentctl/internal/errors"
	memstore "github.com/jkatigb/agentctl/internal/memory"
	"github.com/jkatigb/agentctl/internal/workspace"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var input string
	var inputFile string
	var async bool
	var dedupe bool
	var cacheModeFlag string
	var workspaceFlag string
	var rememberName string
	var rememberType string
	var rememberSummary string
	cmd := &cobra.Command{
		Use:   "run <skill-name>",
		Short: "Run a skill and record the result as a job",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			data, err := loadSkillInput(cmd, input, inputFile)
			if err != nil {
				return err
			}
			handle, err := findSkill(cfg, args[0])
			if err != nil {
				return err
			}

			ws := workspace.Normalize(workspaceFlag)
			if ws == "" && cfg.Memory.AutoLoadWorkspace {
				ws = workspace.Detect("")
			} else if ws == "" {
				if cwd, err := os.Getwd(); err == nil {
					ws = cwd
				}
			}

			cacheMode, err := parseCacheMode(cacheModeFlag, cfg.Cache.DefaultMode)
			if err != nil {
				return err
			}

			// Build and validate options
			opts := RunOptions{
				SkillName:       args[0],
				Input:           data,
				Async:           async,
				Dedupe:          dedupe,
				CacheMode:       cacheMode,
				Workspace:       ws,
				RememberName:    rememberName,
				RememberType:    rememberType,
				RememberSummary: rememberSummary,
			}
			if err := opts.Validate(); err != nil {
				return err
			}

			executor := newRunExecutor(cmd.Context(), cfg, handle, cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
			defer executor.Close()

			if done, err := executor.tryServeCache(data); err != nil {
				return err
			} else if done {
				return nil
			}

			job, isDuplicate, err := executor.prepareJob(data)
			if err != nil {
				return err
			}

			if isDuplicate {
				return executor.handleDuplicate(job)
			}

			if async {
				return executor.submitAsync(job)
			}

			return executor.executeSync(job)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Inline JSON input (default: {})")
	cmd.Flags().StringVar(&inputFile, "input-file", "", "Path to JSON input file ('-' for stdin)")
	cmd.Flags().BoolVar(&async, "async", false, "Submit job and return immediately")
	cmd.Flags().BoolVar(&dedupe, "dedupe", false, "Reuse existing job with same args_hash")
	cmd.Flags().StringVar(&cacheModeFlag, "cache", "", "Cache mode: auto|off|only (default from config)")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace override (default: auto-detect)")
	cmd.Flags().StringVar(&rememberName, "remember", "", "Save successful result as named memory")
	cmd.Flags().StringVar(&rememberType, "remember-type", "result", "Memory type for --remember")
	cmd.Flags().StringVar(&rememberSummary, "remember-summary", "", "Summary to record for remembered result")
	return cmd
}

func init() {
	rootCmd.AddCommand(newRunCommand())
}

func parseCacheMode(flagValue, defaultValue string) (cache.Mode, error) {
	mode := strings.TrimSpace(flagValue)
	if mode == "" {
		mode = defaultValue
	}
	switch strings.ToLower(mode) {
	case "", "auto":
		return cache.ModeAuto, nil
	case "off":
		return cache.ModeOff, nil
	case "only":
		return cache.ModeOnly, nil
	default:
		return cache.ModeOff, fmt.Errorf("invalid cache mode %q (expected auto|off|only)", mode)
	}
}

func annotateRunMeta(result []byte, workspacePath, skillVersion string) []byte {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err != nil {
		return result
	}
	env.Meta.Source = "run"
	if workspacePath != "" {
		env.Meta.Workspace = workspacePath
	}
	if skillVersion != "" {
		env.Meta.SkillVer = skillVersion
	}
	data, err := json.Marshal(env)
	if err != nil {
		return result
	}
	return data
}

// RememberOptions contains parameters for saving execution results to memory.
type RememberOptions struct {
	Name      string
	Type      string
	Summary   string
	Workspace string
	Result    []byte
}

func rememberResult(ctx context.Context, cfg config.Config, opts RememberOptions) error {
	name := strings.TrimSpace(strings.TrimPrefix(opts.Name, "memory:"))
	if name == "" {
		return fmt.Errorf("memory name cannot be empty")
	}
	store, err := memstore.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		return err
	}
	defer func() {
		errs.Ignore(store.Close(), "close memory store after remember")
	}()
	summary := opts.Summary
	if summary == "" {
		summary = summarizeResult(opts.Result)
	}
	_, err = store.SaveResult(ctx, memstore.SaveOptions{
		Name:      name,
		Type:      opts.Type,
		Workspace: opts.Workspace,
		Summary:   summary,
		Result:    opts.Result,
	})
	return err
}
