package cmd

import (
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSkillsDescribeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe <skill-name>",
		Short: "Show detailed information about a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			handle, err := findSkill(cfg, args[0])
			if err != nil {
				return err
			}
			m := handle.Manifest
			details := map[string]any{
				"name":         m.Metadata.Name,
				"version":      m.Metadata.Version,
				"description":  m.Metadata.Description,
				"tags":         m.Metadata.Tags,
				"distribution": m.Distribution.Type,
				"command":      m.Signature.Command,
				"parameters":   m.Signature.Parameters,
				"returns":      m.Signature.Returns,
				"capabilities": map[string]any{
					"network":     m.Capabilities.Network,
					"egressAllow": m.Capabilities.EgressAllow,
					"filesystem":  m.Capabilities.Filesystem,
					"pure":        m.Capabilities.Pure,
				},
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.skills.describe", details, protocol.WithSource("run"))
		},
	}
	return cmd
}

func newSkillsHelpCommand() *cobra.Command {
	var jsonFlag bool = true
	cmd := &cobra.Command{
		Use:   "help <skill-name>",
		Short: "Show AI-friendly JSON help for a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = jsonFlag
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			handle, err := findSkill(cfg, args[0])
			if err != nil {
				return err
			}
			m := handle.Manifest
			help := map[string]any{
				"skill":       m.Metadata.Name,
				"version":     m.Metadata.Version,
				"description": m.Metadata.Description,
				"command":     m.Signature.Command,
				"parameters":  m.Signature.Parameters,
				"returns":     m.Signature.Returns,
			}
			if m.Signature.Help != nil {
				help["help"] = m.Signature.Help
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.skills.help", help, protocol.WithSource("cli"))
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", true, "Output AI-friendly JSON help")
	return cmd
}
