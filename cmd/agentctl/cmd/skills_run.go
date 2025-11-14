package cmd

import (
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func newSkillsRunCommand() *cobra.Command {
	var input string
	var inputFile string
	var workspaceFlag string
	cmd := &cobra.Command{
		Use:   "run <skill-name>",
		Short: "Run a local skill binary with JSON input",
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
			runCtx := resolveWorkspaceContext(cmd.Context(), workspaceFlag)
			stdout, stderr, err := executeSkill(runCtx, handle.Manifest, handle.ArtifactPath, data)
			if len(stderr) > 0 {
				if _, werr := cmd.ErrOrStderr().Write(append(stderr, '\n')); werr != nil {
					return werr
				}
			}
			if err != nil {
				return err
			}
			return writeEnvelope(cmd.OutOrStdout(), stdout)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Inline JSON input (default: {})")
	cmd.Flags().StringVar(&inputFile, "input-file", "", "Path to JSON input file ('-' for stdin)")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace override (default: auto-detect)")
	return cmd
}
