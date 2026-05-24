package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	workspaceutil "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSearchIndexCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "searchindex",
		Short: "Inspect the persistent retrieval searchindex",
	}
	cmd.AddCommand(newSearchIndexStatsCommand())
	return cmd
}

func newSearchIndexStatsCommand() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show persistent searchindex document stats for a workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSearchIndexStats(cmd, workspace)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	return cmd
}

func runSearchIndexStats(cmd *cobra.Command, workspace string) error {
	start := time.Now()
	ctx := cmd.Context()

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return writeSearchIndexError(cmd, "EARG", fmt.Sprintf("resolve workspace: %v", err))
	}
	info, err := os.Stat(absWorkspace)
	if err != nil {
		code := "ENOTFOUND"
		if os.IsPermission(err) {
			code = "EIO"
		}
		return writeSearchIndexError(cmd, code, fmt.Sprintf("workspace %q: %v", absWorkspace, err))
	}
	if !info.IsDir() {
		return writeSearchIndexError(cmd, "EARG", fmt.Sprintf("workspace %q is not a directory", absWorkspace))
	}

	cfg, err := configForSearchIndex(ctx)
	if err != nil {
		return writeSearchIndexError(cmd, "ECONFIG", err.Error())
	}
	store, err := searchindex.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeSearchIndexError(cmd, "EIO", err.Error())
	}
	defer func() {
		_ = store.Close()
	}()

	workspaceID := workspaceutil.ID(absWorkspace)
	stats, err := store.WorkspaceStats(ctx, workspaceID)
	if err != nil {
		return writeSearchIndexError(cmd, "EIO", err.Error())
	}

	data := map[string]any{
		"workspace":      absWorkspace,
		"family_path":    workspaceutil.FamilyPath(absWorkspace),
		"workspace_id":   stats.WorkspaceID,
		"document_count": stats.DocumentCount,
		"embedded_count": stats.EmbeddedCount,
		"has_documents":  stats.DocumentCount > 0,
	}
	if stats.EmbeddingMetadata != nil {
		data["embedding"] = map[string]any{
			"model":      stats.EmbeddingMetadata.Model,
			"dimensions": stats.EmbeddingMetadata.Dimensions,
		}
	}

	env := protocol.OK(
		"searchindex.stats",
		data,
		protocol.WithSource("cli"),
		protocol.WithWorkspace(absWorkspace),
		protocol.WithDuration(time.Since(start).Milliseconds()),
	)
	return protocol.Write(cmd.OutOrStdout(), env)
}

func configForSearchIndex(ctx context.Context) (config.Config, error) {
	if cfg, ok := config.FromContext(ctx); ok {
		return cfg, nil
	}
	return loadConfig(ctx)
}

func writeSearchIndexError(cmd *cobra.Command, code, message string) error {
	env := protocol.Error("searchindex", protocol.ErrorCode(code), message, nil, protocol.WithSource("cli"))
	if err := protocol.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s: %s", code, message)
}

func init() {
	rootCmd.AddCommand(newSearchIndexCommand())
}
