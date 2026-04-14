package cmd

import (
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/spf13/cobra"
)

func newContextProposalsCommand() *cobra.Command {
	var workspacePath string
	var limit int

	cmd := &cobra.Command{
		Use:   "proposals",
		Short: "List recorded ACA memory proposals",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			items, err := store.ListMemoryProposals(cmd.Context(), limit)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/proposals", map[string]any{
				"workspace_path": target,
				"proposals":      items,
				"count":          len(items),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum proposals to list")
	return cmd
}

func newContextProposalCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "proposal <id>",
		Short: "Read or update one ACA memory proposal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			proposal, err := store.GetMemoryProposal(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			if proposal == nil {
				return fmt.Errorf("no proposal found for %s", strings.TrimSpace(args[0]))
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/proposal", map[string]any{
				"workspace_path": target,
				"proposal":       proposal,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.AddCommand(
		newContextProposalApplyCommand(),
		newContextProposalMergeCommand(),
		newContextProposalReleaseMergeCommand(),
		newContextProposalRejectCommand(),
	)
	return cmd
}

func newContextProposalApplyCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "apply <id>",
		Short: "Apply one low-risk ACA memory proposal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			proposal, result, packet, err := store.ApplyMemoryProposal(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/proposal_apply", map[string]any{
				"workspace_path": target,
				"proposal":       proposal,
				"result":         result,
				"work_packet":    packet,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newContextProposalMergeCommand() *cobra.Command {
	var workspacePath string
	var vaultName string
	var vaultPath string
	var draftPath string
	var targetPath string
	var heading string

	cmd := &cobra.Command{
		Use:   "merge <id>",
		Short: "Merge one evidence-backed ACA proposal into a canonical vault note",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			proposal, merge, packet, err := store.MergeMemoryProposal(cmd.Context(), vaultName, vaultPath, strings.TrimSpace(args[0]), draftPath, targetPath, heading)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/proposal_merge", map[string]any{
				"workspace_path": target,
				"vault_name":     vaultName,
				"vault_path":     vaultPath,
				"proposal":       proposal,
				"merge":          merge,
				"work_packet":    packet,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&draftPath, "draft-path", "", "Optional draft path override")
	cmd.Flags().StringVar(&targetPath, "target-path", "", "Optional canonical target note path override")
	cmd.Flags().StringVar(&heading, "heading", "", "Optional bounded review heading override")
	return cmd
}

func newContextProposalRejectCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject one ACA memory proposal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			proposal, err := store.RejectMemoryProposal(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/proposal_reject", map[string]any{
				"workspace_path": target,
				"proposal":       proposal,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newContextProposalReleaseMergeCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "release-merge <id>",
		Short: "Release a claimed proposal-merge packet back to the pool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			proposal, err := store.ReleaseProposalMergeClaim(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/proposal_release_merge", map[string]any{
				"workspace_path": target,
				"proposal":       proposal,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}
