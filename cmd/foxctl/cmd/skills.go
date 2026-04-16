package cmd

import "github.com/spf13/cobra"

func newSkillsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage local skills",
	}
	cmd.AddCommand(
		newSkillsRunCommand(),
		newSkillsInstallCommand(),
		newSkillsListCommand(),
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
