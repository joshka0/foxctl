package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/runservice"
	"github.com/jkatigb/agentctl/internal/storage/cache"
	"github.com/spf13/cobra"
)

type runCommandFlags struct {
	Input           string
	InputFile       string
	Async           bool
	Dedupe          bool
	CacheMode       string
	Workspace       string
	RememberName    string
	RememberType    string
	RememberSummary string
}

func bindRunFlags(cmd *cobra.Command, flags *runCommandFlags) {
	cmd.Flags().StringVar(&flags.Input, "input", "", "Inline JSON input (default: {}). Special values: 'stdin' to extract data from envelope, 'sha256:<hex>' to read from CAS")
	cmd.Flags().StringVar(&flags.InputFile, "input-file", "", "Path to JSON input file ('-' for stdin)")
	cmd.Flags().BoolVar(&flags.Async, "async", false, "Submit job and return immediately")
	cmd.Flags().BoolVar(&flags.Dedupe, "dedupe", false, "Reuse existing job with same args_hash")
	cmd.Flags().StringVar(&flags.CacheMode, "cache", "", "Cache mode: auto|off|only (default from config)")
	cmd.Flags().StringVar(&flags.Workspace, "workspace", "", "Workspace override (default: auto-detect)")
	cmd.Flags().StringVar(&flags.RememberName, "remember", "", "Save successful result as named memory")
	cmd.Flags().StringVar(&flags.RememberType, "remember-type", "result", "Memory type for --remember")
	cmd.Flags().StringVar(&flags.RememberSummary, "remember-summary", "", "Summary to record for remembered result")
}

func buildRunOptions(cfg config.Config, skillName string, flags runCommandFlags, input []byte) (runservice.RunOptions, error) {
	ws := workspace.Normalize(flags.Workspace)
	if ws == "" && cfg.Memory.AutoLoadWorkspace {
		ws = workspace.Detect("")
	} else if ws == "" {
		if cwd, err := os.Getwd(); err == nil {
			ws = cwd
		}
	}

	cacheMode, err := parseCacheMode(flags.CacheMode, cfg.Cache.DefaultMode)
	if err != nil {
		return runservice.RunOptions{}, err
	}

	opts := runservice.RunOptions{
		SkillName:       skillName,
		Input:           input,
		Async:           flags.Async,
		Dedupe:          flags.Dedupe,
		CacheMode:       cacheMode,
		Workspace:       ws,
		RememberName:    flags.RememberName,
		RememberType:    flags.RememberType,
		RememberSummary: flags.RememberSummary,
	}
	if err := opts.Validate(); err != nil {
		return runservice.RunOptions{}, err
	}
	return opts, nil
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
