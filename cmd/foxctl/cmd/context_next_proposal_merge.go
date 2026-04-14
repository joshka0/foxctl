package cmd

import (
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/spf13/cobra"
)

func newContextNextProposalMergeCommand() *cobra.Command {
	var workspacePath string
	var vaultPath string
	var limit int
	var claim bool

	cmd := &cobra.Command{
		Use:   "next-proposal-merge",
		Short: "Return the next prepared proposal-merge task and work packet",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			store := contextplane.NewWorkspaceStore(target)
			if strings.TrimSpace(vaultPath) != "" {
				cfg, err := loadConfig(ctx)
				if err != nil {
					return err
				}
				index, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
				if err != nil {
					return err
				}
				defer func() { _ = index.Close() }()
				health, err := index.Health(ctx)
				if err != nil {
					return err
				}
				if _, err := store.GenerateMaintenanceTasksWithHealth(ctx, limit, &health); err != nil {
					return fmt.Errorf("generate maintenance tasks: %w", err)
				}
			} else {
				if _, err := store.GenerateMaintenanceTasks(ctx, limit); err != nil {
					return fmt.Errorf("generate maintenance tasks: %w", err)
				}
			}
			var (
				task *contextplane.MaintenanceTask
				err  error
			)
			if claim {
				task, err = store.ClaimNextProposalMergeTask(ctx, limit)
			} else {
				task, err = store.NextProposalMergeTask(ctx, limit)
			}
			if err != nil {
				return err
			}
			found := task != nil
			var packet *contextplane.ProposalWorkPacket
			if task != nil {
				packet = task.WorkPacket
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/next_proposal_merge", map[string]any{
				"workspace_path": target,
				"vault_path":     strings.TrimSpace(vaultPath),
				"found":          found,
				"task":           task,
				"work_packet":    packet,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Optional vault path for health refresh before selection")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maintenance-task scan limit")
	cmd.Flags().BoolVar(&claim, "claim", false, "Claim the selected proposal-merge task so it is not re-offered")
	return cmd
}
