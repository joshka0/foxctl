package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/tools/obsidian"
	"github.com/spf13/cobra"
)

func newObsidianGraphCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Build inbox-first Obsidian graph-layer drafts from the repo index",
	}
	cmd.AddCommand(newObsidianGraphBuildCommand(), newObsidianGraphPromoteCommand())
	return cmd
}

func newObsidianGraphBuildCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var workspacePath string
	var project string
	var folder string
	var maxPackages int
	var maxFilesPerPackage int
	var maxSymbolsPerPackage int
	var includePrefixes []string
	var excludePrefixes []string

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Generate a repo graph draft bundle in the vault inbox",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			repo, err := repoindex.Open(ctx, cfg.Storage.Root, target)
			if err != nil {
				return err
			}
			defer func() { _ = repo.Close() }()
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			result, err := obsidian.BuildRepoGraphDrafts(ctx, writer, repo, obsidian.RepoGraphBuildOptions{
				Project:                project,
				WorkspaceRoot:          target,
				Folder:                 folder,
				MaxPackages:            maxPackages,
				MaxFilesPerPackage:     maxFilesPerPackage,
				MaxSymbolsPerPackage:   maxSymbolsPerPackage,
				IncludePackagePrefixes: includePrefixes,
				ExcludePackagePrefixes: excludePrefixes,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/graph_build", map[string]any{
				"workspace_path": target,
				"vault_name":     vaultName,
				"vault_path":     vaultPath,
				"result":         result,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&project, "project", "", "Project name override")
	cmd.Flags().StringVar(&folder, "folder", "", "Vault folder override (default: inbox/drafted-from-agentctl/repo-graph/<project>)")
	cmd.Flags().IntVar(&maxPackages, "max-packages", 6, "Maximum package notes to generate")
	cmd.Flags().IntVar(&maxFilesPerPackage, "max-files-per-package", 8, "Maximum files per package note")
	cmd.Flags().IntVar(&maxSymbolsPerPackage, "max-symbols-per-package", 12, "Maximum symbols per package note")
	cmd.Flags().StringSliceVar(&includePrefixes, "include-package-prefix", nil, "Only include package paths starting with these prefixes (repeatable)")
	cmd.Flags().StringSliceVar(&excludePrefixes, "exclude-package-prefix", nil, "Exclude package paths starting with these prefixes (repeatable)")
	return cmd
}

func newObsidianGraphPromoteCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var workspacePath string
	var project string
	var sourceFolder string
	var targetFolder string

	cmd := &cobra.Command{
		Use:   "promote",
		Short: "Review-merge a generated graph draft bundle into canonical repo notes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			if strings.TrimSpace(project) == "" {
				project = filepath.Base(target)
			}
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			if strings.TrimSpace(sourceFolder) == "" {
				sourceFolder = obsidian.DefaultRepoGraphDraftFolder(writer.Policy, project)
			}
			if strings.TrimSpace(targetFolder) == "" {
				targetFolder = obsidian.DefaultRepoGraphCanonicalFolder(project)
			}
			result, err := obsidian.PromoteRepoGraphDrafts(cmd.Context(), writer, sourceFolder, targetFolder)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/graph_promote", map[string]any{
				"vault_name": vaultName,
				"vault_path": vaultPath,
				"result":     result,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&project, "project", "", "Project name override")
	cmd.Flags().StringVar(&sourceFolder, "source-folder", "", "Draft bundle folder inside the vault (default: inbox/drafted-from-agentctl/repo-graph/<project>)")
	cmd.Flags().StringVar(&targetFolder, "target-folder", "", "Canonical target folder inside the vault (default: notes/repo/<project>)")
	return cmd
}
