package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/runtime/observability"
	"github.com/spf13/cobra"
)

type errorsSummary struct {
	ByComponent map[string]int `json:"by_component,omitempty"`
	ByErrorCode map[string]int `json:"by_error_code,omitempty"`
	ByOperation map[string]int `json:"by_operation,omitempty"`
}

func init() {
	rootCmd.AddCommand(newErrorsCommand())
}

func newErrorsCommand() *cobra.Command {
	var (
		limit          int
		since          string
		component      string
		operation      string
		workspaceFlag  string
		obsDir         string
		includeNonErrs bool
	)

	cmd := &cobra.Command{
		Use:   "errors",
		Short: "Show recent platform errors from observability events",
		Long: `Show recent platform errors from the observability event stream.

By default this only returns events with status=error, matching the GUI error view.
Use --all to include non-error events for the same query shape.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, ok := config.FromContext(cmd.Context())
			if !ok {
				return fmt.Errorf("configuration not loaded")
			}

			var sinceTime time.Time
			if strings.TrimSpace(since) != "" {
				d, err := time.ParseDuration(strings.TrimSpace(since))
				if err != nil {
					return fmt.Errorf("parse --since: %w", err)
				}
				sinceTime = time.Now().Add(-d)
			}

			workspaceID := ""
			workspaceRoot := ""
			var workspaceFilters []string
			if strings.TrimSpace(workspaceFlag) != "" {
				workspaceRoot = resolveWorkspace(cfg, workspaceFlag)
				if workspaceRoot != "" {
					if absRoot, absErr := filepath.Abs(workspaceRoot); absErr == nil {
						workspaceRoot = absRoot
					}
				}
				workspaceID = resolveWorkspaceID(cfg, workspaceFlag)
				workspaceFilters = appendWorkspaceFilters(workspaceFilters, workspaceID, workspaceRoot)
			}

			entries, err := observability.QueryEventRecords(cmd.Context(), observability.EventQueryOptions{
				ObsDir:          strings.TrimSpace(obsDir),
				Limit:           limit,
				Since:           sinceTime,
				Component:       strings.TrimSpace(component),
				OperationPrefix: strings.TrimSpace(operation),
				WorkspaceID:     workspaceID,
				WorkspaceIDs:    workspaceFilters,
				ErrorsOnly:      !includeNonErrs,
			})
			if err != nil {
				return err
			}

			payload := map[string]any{
				"entries": entries,
				"count":   len(entries),
				"summary": buildErrorsSummary(entries),
				"filters": map[string]any{
					"errors_only":    !includeNonErrs,
					"component":      strings.TrimSpace(component),
					"operation":      strings.TrimSpace(operation),
					"workspace":      workspaceID,
					"workspace_root": workspaceRoot,
					"since":          strings.TrimSpace(since),
					"obs_dir":        firstNonEmptyErrorValue(strings.TrimSpace(obsDir), observability.ResolveObsDir()),
				},
			}

			return writeOK(cmd, "agentctl.errors", payload, "local", profilesCoreAgent)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum entries to return")
	cmd.Flags().StringVar(&since, "since", "24h", "Only include events newer than this duration (for example 15m, 2h, 7d is not supported; use h/m/s)")
	cmd.Flags().StringVar(&component, "component", "", "Filter by component")
	cmd.Flags().StringVar(&operation, "operation", "", "Filter by operation prefix")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Filter by workspace path")
	cmd.Flags().StringVar(&obsDir, "obs-dir", "", "Override observability directory")
	cmd.Flags().BoolVar(&includeNonErrs, "all", false, "Include non-error events")
	return cmd
}

func buildErrorsSummary(entries []observability.EventRecord) errorsSummary {
	summary := errorsSummary{
		ByComponent: map[string]int{},
		ByErrorCode: map[string]int{},
		ByOperation: map[string]int{},
	}
	for _, entry := range entries {
		if entry.Component != "" {
			summary.ByComponent[entry.Component]++
		}
		if entry.ErrorCode != "" {
			summary.ByErrorCode[entry.ErrorCode]++
		}
		if entry.Operation != "" {
			summary.ByOperation[entry.Operation]++
		}
	}
	if len(summary.ByComponent) == 0 {
		summary.ByComponent = nil
	}
	if len(summary.ByErrorCode) == 0 {
		summary.ByErrorCode = nil
	}
	if len(summary.ByOperation) == 0 {
		summary.ByOperation = nil
	}
	return summary
}

func firstNonEmptyErrorValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func appendWorkspaceFilters(filters []string, values ...string) []string {
	seen := make(map[string]struct{}, len(filters)+len(values))
	for _, filter := range filters {
		filter = strings.TrimSpace(filter)
		if filter == "" {
			continue
		}
		seen[filter] = struct{}{}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		filters = append(filters, value)
		seen[value] = struct{}{}
	}
	return filters
}
