package cmd

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/spf13/cobra"
)

var profilesCoreAgent = []string{"core/v1", "agent/v1"}

func writeOK(cmd *cobra.Command, command string, data any, source string, profiles []string, mutate ...func(*envelope.Meta)) error {
	env := envelope.OK(command, data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = source
		if len(profiles) > 0 {
			m.Profiles = profiles
		}
		for _, fn := range mutate {
			if fn != nil {
				fn(m)
			}
		}
	}))

	if err := envelope.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}
	return nil
}
