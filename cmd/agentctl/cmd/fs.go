package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

func newFSCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fs",
		Short: "Filesystem-focused helpers (built on fs/* skills)",
	}
	cmd.AddCommand(newFSReadCommand())
	return cmd
}

func newFSReadCommand() *cobra.Command {
	var maxBytes int
	var workspaceFlag string
	var rememberName string
	var rememberType string
	var rememberSummary string
	var cacheMode string

	cmd := &cobra.Command{
		Use:   "read <path>",
		Short: "Read a file via the fs/read skill and emit an envelope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"path": args[0],
			}
			if maxBytes > 0 {
				payload["max_bytes"] = maxBytes
			}
			input, err := json.Marshal(payload)
			if err != nil {
				return err
			}

			runCmd := newRunCommand()
			runCmd.SetContext(cmd.Context())
			runCmd.SetOut(cmd.OutOrStdout())
			runCmd.SetErr(cmd.ErrOrStderr())

			var runArgs []string
			runArgs = append(runArgs, "--input", string(input))
			if workspaceFlag != "" {
				runArgs = append(runArgs, "--workspace", workspaceFlag)
			}
			if cacheMode != "" {
				runArgs = append(runArgs, "--cache", cacheMode)
			}
			if rememberName != "" {
				runArgs = append(runArgs, "--remember", rememberName)
				if rememberType != "" {
					runArgs = append(runArgs, "--remember-type", rememberType)
				}
				if rememberSummary != "" {
					runArgs = append(runArgs, "--remember-summary", rememberSummary)
				}
			}
			runArgs = append(runArgs, "fs/read")
			runCmd.SetArgs(runArgs)
			return runCmd.Execute()
		},
	}

	cmd.Flags().IntVar(&maxBytes, "max-bytes", 0, "Override preview byte limit (<= inline_output_kb)")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace override for caching/memory")
	cmd.Flags().StringVar(&rememberName, "remember", "", "Save successful result as named memory")
	cmd.Flags().StringVar(&rememberType, "remember-type", "result", "Memory type label for --remember")
	cmd.Flags().StringVar(&rememberSummary, "remember-summary", "", "Summary to record with remembered result")
	cmd.Flags().StringVar(&cacheMode, "cache", "", "Cache mode: auto|off|only (default from config)")
	return cmd
}

func init() {
	rootCmd.AddCommand(newFSCommand())
}
