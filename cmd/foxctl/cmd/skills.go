package cmd

import "github.com/spf13/cobra"

func newSkillsCommand() *cobra.Command {
	var compact bool
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage local skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSkillsListCommand(cmd, compact)
		},
	}
	cmd.Flags().BoolVar(&compact, "compact", false, "Print compact agent-readable lines instead of a JSON envelope")
	cmd.AddCommand(
		newSkillsRunCommand(),
		newSkillsInstallCommand(),
		newSkillsListCommand(),
		newSkillsGetCommand(),
		newSkillsPathCommand(),
		newSkillsDoctorCommand(),
		newSkillsDescribeCommand(),
		newSkillsHelpCommand(),
		newSkillsSearchCommand(),
		newSkillsSyncCommand(),
		newSkillsUninstallCommand(),
		newSkillsUpgradeCommand(),
	)
	return cmd
}

func init() {
	rootCmd.AddCommand(newSkillsCommand())
}
