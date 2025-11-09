package cmd

import (
	"bytes"
	"os/exec"

	"github.com/spf13/cobra"
)

func newSkillsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage local skills",
	}
	cmd.AddCommand(newSkillsRunCommand())
	return cmd
}

func newSkillsRunCommand() *cobra.Command {
	var input string
	var inputFile string
	cmd := &cobra.Command{
		Use:   "run <skill-name>",
		Short: "Run a local skill binary with JSON input",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := loadSkillInput(cmd, input, inputFile)
			if err != nil {
				return err
			}
			bin, err := resolveSkillBinary(args[0])
			if err != nil {
				return err
			}

			c := exec.CommandContext(cmd.Context(), bin)
			c.Stdin = bytes.NewReader(data)
			c.Stdout = cmd.OutOrStdout()
			c.Stderr = cmd.ErrOrStderr()
			return c.Run()
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Inline JSON input (default: {})")
	cmd.Flags().StringVar(&inputFile, "input-file", "", "Path to JSON input file ('-' for stdin)")
	return cmd
}

func init() {
	rootCmd.AddCommand(newSkillsCommand())
}
