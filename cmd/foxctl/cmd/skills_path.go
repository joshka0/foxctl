package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func newSkillsPathCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path [skill-name]",
		Short: "Print the configured skills root or a skill directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.MustFromContext(cmd.Context())
			if len(args) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), absolutePath(cfg.Paths.Skills))
				return err
			}
			handle, err := createSkillResolver(cfg).Resolve(args[0])
			if err != nil {
				return skillNotFoundError(args[0], err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), absolutePath(filepath.Dir(handle.ManifestPath)))
			return err
		},
	}
	return cmd
}
