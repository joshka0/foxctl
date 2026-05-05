package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/cmd/foxctl/cmd/memorycmd"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/runservice"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/spf13/cobra"
)

const curatorReportSkillName = "memory/curator_report"

type curatorReportFlags struct {
	Workspace                    string
	Limit                        int
	StaleAfterDays               int
	ArchiveAfterDays             int
	RevalidateAfterDays          int
	RevalidateEnvClaimsAfterDays int
	MinUsesBeforeUtilityJudgment int
	PersistReport                bool
	Apply                        bool
	DryRun                       bool
	ConfirmApply                 bool
	Timeout                      time.Duration
}

func newCuratorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "curator",
		Short: "Inspect and apply memory curator lifecycle reports",
	}
	cmd.AddCommand(
		newCuratorStatusCommand(),
		newCuratorRunCommand(),
		newCuratorReportCommand(),
	)
	return cmd
}

func newCuratorStatusCommand() *cobra.Command {
	var flags curatorReportFlags
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Generate a dry-run memory curator status report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags.DryRun = true
			flags.Apply = false
			return executeCuratorReportSkill(cmd, flags)
		},
	}
	bindCuratorReportFlags(cmd, &flags, false)
	return cmd
}

func newCuratorRunCommand() *cobra.Command {
	var flags curatorReportFlags
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the memory curator in dry-run or apply mode",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flags.Apply && flags.DryRun {
				return memorycmd.WriteArgError(cmd.OutOrStdout(), "foxctl.curator.run", "conflicting flags", "Use --apply or --dry-run, not both.")
			}
			if flags.Apply && !flags.ConfirmApply {
				return memorycmd.WriteArgError(cmd.OutOrStdout(), "foxctl.curator.run", "mode=apply requires --confirm-apply", "Run without --apply first, review the report, then rerun with --apply --confirm-apply.")
			}
			return executeCuratorReportSkill(cmd, flags)
		},
	}
	bindCuratorReportFlags(cmd, &flags, true)
	return cmd
}

func newCuratorReportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Inspect persisted memory curator reports",
	}
	cmd.AddCommand(newCuratorReportLatestCommand())
	return cmd
}

func newCuratorReportLatestCommand() *cobra.Command {
	var workspaceFlag string
	cmd := &cobra.Command{
		Use:   "latest",
		Short: "Show the latest persisted memory curator report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entries, _, err := store.ListFiltered(ctx, workspaceID, storage.MemoryListFilter{
						Types: []string{"curator_report"},
					}, 1, 0)
					if err != nil {
						return err
					}
					if len(entries) == 0 {
						return memorycmd.WriteNotFound(cmd.OutOrStdout(), "foxctl.curator.report.latest", "curator_report:*", workspaceID)
					}
					entry := entries[0]
					payload := map[string]any{}
					if len(entry.Result) > 0 {
						if err := json.Unmarshal(entry.Result, &payload); err != nil {
							return fmt.Errorf("decode curator report memory %q: %w", entry.Name, err)
						}
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.curator.report.latest", map[string]any{
						"name":       entry.Name,
						"type":       entry.Type,
						"workspace":  entry.Workspace,
						"summary":    entry.Summary,
						"created_at": entry.CreatedAt,
						"updated_at": entry.UpdatedAt,
						"artifact":   payload["artifact"],
						"payload":    payload,
					})
				})
			})
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	return cmd
}

func bindCuratorReportFlags(cmd *cobra.Command, flags *curatorReportFlags, includeModeFlags bool) {
	cmd.Flags().StringVar(&flags.Workspace, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().IntVar(&flags.Limit, "limit", 1000, "Maximum memory records to inspect")
	cmd.Flags().IntVar(&flags.StaleAfterDays, "stale-after-days", 0, "Days before unused active records are proposed stale")
	cmd.Flags().IntVar(&flags.ArchiveAfterDays, "archive-after-days", 0, "Days before stale records are proposed archived")
	cmd.Flags().IntVar(&flags.RevalidateAfterDays, "revalidate-after-days", 0, "Days before candidates/validated records need review")
	cmd.Flags().IntVar(&flags.RevalidateEnvClaimsAfterDays, "revalidate-env-claims-after-days", 0, "Days before environment-dependent claims need review")
	cmd.Flags().IntVar(&flags.MinUsesBeforeUtilityJudgment, "min-uses-before-utility-judgment", 0, "Minimum uses before success-rate demotion is considered")
	cmd.Flags().BoolVar(&flags.PersistReport, "persist-report", false, "Persist the curator report to CAS and named memory")
	cmd.Flags().DurationVar(&flags.Timeout, "timeout", runservice.DefaultTimeout, "Maximum execution time")
	if includeModeFlags {
		cmd.Flags().BoolVar(&flags.DryRun, "dry-run", false, "Run without mutating lifecycle state (default)")
		cmd.Flags().BoolVar(&flags.Apply, "apply", false, "Apply supported lifecycle proposals")
		cmd.Flags().BoolVar(&flags.ConfirmApply, "confirm-apply", false, "Required with --apply")
	}
}

func executeCuratorReportSkill(cmd *cobra.Command, flags curatorReportFlags) error {
	input, err := curatorReportInput(cmd, flags)
	if err != nil {
		return err
	}
	runFlags := runCommandFlags{
		Input:     string(input),
		Ephemeral: true,
		Workspace: flags.Workspace,
		Timeout:   flags.Timeout,
		NoCAS:     true,
	}
	return executeRunCommand(cmd, []string{curatorReportSkillName}, runFlags)
}

func curatorReportInput(cmd *cobra.Command, flags curatorReportFlags) ([]byte, error) {
	mode := "dry_run"
	if flags.Apply {
		mode = "apply"
	}
	workspaceFlagChanged := cmd.Flags().Changed("workspace")
	if workspaceFlagChanged {
		flags.Workspace = strings.TrimSpace(flags.Workspace)
	}
	payload := map[string]any{
		"mode":          mode,
		"confirm_apply": flags.ConfirmApply,
		"limit":         flags.Limit,
	}
	if flags.Workspace != "" || workspaceFlagChanged {
		payload["workspace"] = flags.Workspace
	}
	if flags.PersistReport {
		payload["persist_report"] = true
	}
	if flags.StaleAfterDays > 0 {
		payload["stale_after_days"] = flags.StaleAfterDays
	}
	if flags.ArchiveAfterDays > 0 {
		payload["archive_after_days"] = flags.ArchiveAfterDays
	}
	if flags.RevalidateAfterDays > 0 {
		payload["revalidate_after_days"] = flags.RevalidateAfterDays
	}
	if flags.RevalidateEnvClaimsAfterDays > 0 {
		payload["revalidate_env_claims_after_days"] = flags.RevalidateEnvClaimsAfterDays
	}
	if flags.MinUsesBeforeUtilityJudgment > 0 {
		payload["min_uses_before_utility_judgment"] = flags.MinUsesBeforeUtilityJudgment
	}
	return json.Marshal(payload)
}

func init() {
	rootCmd.AddCommand(newCuratorCommand())
}
