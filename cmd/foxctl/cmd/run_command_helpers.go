package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/runservice"
	"github.com/joshka0/foxctl/internal/storage/cache"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

type runCommandFlags struct {
	Input           string
	InputFile       string
	Async           bool
	Dedupe          bool
	Ephemeral       bool
	Daemon          bool
	Examples        bool
	CacheMode       string
	Workspace       string
	WorkspaceSet    bool
	RememberName    string
	RememberType    string
	RememberSummary string
	Timeout         time.Duration
	Format          string // Output format: json, table, compact, or jq expression
	JQ              string // jq expression to apply to output
	NoCAS           bool   // Disable CAS truncation - return full output inline
}

func bindRunFlags(cmd *cobra.Command, flags *runCommandFlags) {
	cmd.Flags().StringVar(&flags.Input, "input", "", "Inline JSON input (default: {}). Special values: 'stdin' to extract data from envelope, 'sha256:<hex>' to read from CAS")
	cmd.Flags().StringVar(&flags.InputFile, "input-file", "", "Path to JSON input file ('-' for stdin)")
	cmd.Flags().BoolVar(&flags.Async, "async", false, "Submit job and return immediately")
	cmd.Flags().BoolVar(&flags.Dedupe, "dedupe", false, "Reuse existing job with same args_hash")
	cmd.Flags().BoolVar(&flags.Ephemeral, "ephemeral", false, "Skip job persistence for faster execution (for hooks)")
	cmd.Flags().BoolVar(&flags.Daemon, "daemon", false, "Execute via daemon for faster hook execution")
	cmd.Flags().BoolVar(&flags.Examples, "examples", false, "Show example usage (optionally for a specific skill)")
	cmd.Flags().StringVar(&flags.CacheMode, "cache", "", "Cache mode (disabled; must be off)")
	cmd.Flags().StringVar(&flags.Workspace, "workspace", "", "Workspace override (default: auto-detect)")
	cmd.Flags().StringVar(&flags.RememberName, "remember", "", "Save successful result as named memory")
	cmd.Flags().StringVar(&flags.RememberType, "remember-type", "result", "Memory type for --remember")
	cmd.Flags().StringVar(&flags.RememberSummary, "remember-summary", "", "Summary to record for remembered result")
	cmd.Flags().DurationVar(&flags.Timeout, "timeout", runservice.DefaultTimeout, "Maximum execution time (e.g., 30s, 2m, 5m)")
	cmd.Flags().StringVarP(&flags.Format, "format", "f", "", "Output format: json (default), table, compact")
	cmd.Flags().StringVar(&flags.JQ, "jq", "", "jq expression to filter/transform JSON output (e.g., '.data.tasks[]')")
	cmd.Flags().BoolVar(&flags.NoCAS, "no-cas", true, "Disable CAS truncation - return full output inline (may be large)")
}

func buildRunOptions(cfg config.Config, skillName string, flags runCommandFlags, input []byte) (runservice.RunOptions, error) {
	correlationID := ""
	cliCommand := ""
	if len(input) > 0 {
		var m map[string]any
		if err := json.Unmarshal(input, &m); err == nil {
			if s, ok := m["correlation_id"].(string); ok {
				correlationID = strings.TrimSpace(s)
			}
			if s, ok := m["cli_command"].(string); ok {
				cliCommand = strings.TrimSpace(s)
			}
		}
	}
	if correlationID == "" {
		correlationID = ulid.Make().String()
	}

	ws := workspace.Normalize(flags.Workspace)
	// If the user explicitly provided --workspace (even as empty string), do not auto-detect.
	if !flags.WorkspaceSet {
		if ws == "" && cfg.Memory.AutoLoadWorkspace {
			ws = workspace.Detect("")
		} else if ws == "" {
			if cwd, err := os.Getwd(); err == nil {
				ws = cwd
			}
		}
	}

	cacheMode, err := parseCacheMode(flags.CacheMode, cfg.Cache.DefaultMode)
	if err != nil {
		return runservice.RunOptions{}, err
	}

	opts := runservice.RunOptions{
		SkillName:       skillName,
		CLICommand:      cliCommand,
		CorrelationID:   correlationID,
		Input:           input,
		Async:           flags.Async,
		Dedupe:          flags.Dedupe,
		Ephemeral:       flags.Ephemeral,
		CacheMode:       cacheMode,
		Workspace:       ws,
		RememberName:    flags.RememberName,
		RememberType:    flags.RememberType,
		RememberSummary: flags.RememberSummary,
		Timeout:         flags.Timeout,
		SessionID:       resolveSessionID(),
		NoCAS:           flags.NoCAS,
	}
	if err := opts.Validate(); err != nil {
		return runservice.RunOptions{}, err
	}
	return opts, nil
}

// resolveSessionID returns the session ID from environment variables.
// Priority: AGENTCTL_SESSION_ID > CLAUDE_SESSION_ID > OPENCODE_SESSION_ID >
// CURSOR_SESSION_ID > TERM_SESSION_ID. Returns empty string if none set.
func resolveSessionID() string {
	for _, key := range []string{
		"AGENTCTL_SESSION_ID",
		"CLAUDE_SESSION_ID",
		"OPENCODE_SESSION_ID",
		"CURSOR_SESSION_ID",
		"TERM_SESSION_ID",
	} {
		if sid := os.Getenv(key); sid != "" {
			return sid
		}
	}
	return ""
}

func parseCacheMode(flagValue, defaultValue string) (cache.Mode, error) {
	mode := strings.TrimSpace(flagValue)
	_ = defaultValue
	if mode == "" {
		return cache.ModeOff, nil
	}
	if strings.EqualFold(mode, string(cache.ModeOff)) {
		return cache.ModeOff, nil
	}
	return cache.ModeOff, fmt.Errorf("cache is disabled (expected --cache=off)")
}
