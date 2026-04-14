package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
	"github.com/spf13/cobra"
)

func newObsidianBridgeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bridge",
		Short: "Reconcile repo docs with vault notes through inbox-first bridge drafts",
	}
	cmd.AddCommand(newObsidianBridgeReconcileCommand(), newObsidianBridgeReportCommand(), newObsidianBridgeApplyCommand(), newObsidianBridgeApplyBatchCommand(), newObsidianBridgeTidyCommand())
	return cmd
}

func newObsidianBridgeReconcileCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var workspacePath string
	var docsRoot string
	var project string
	var folder string
	var maxMatches int
	var includeDocPrefixes []string
	var excludeDocPrefixes []string
	var rebuildIndex bool

	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Draft repo docs <-> vault bridge notes and backlink suggestions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			var searchProvider obsidian.DocsBridgeSearchProvider
			cfg, err := loadConfig(ctx)
			if err == nil {
				if idx, openErr := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath); openErr == nil {
					defer func() { _ = idx.Close() }()
					if rebuildIndex {
						_, _ = idx.Rebuild(ctx, vaultPath)
					}
					if stats, statsErr := idx.Stats(ctx); statsErr == nil && stats.Notes > 0 {
						searchProvider = &bridgeIndexSearchProvider{
							index:            idx,
							semanticProvider: openObsidianSemanticProvider(cfg),
						}
					}
				}
			}
			result, err := obsidian.ReconcileDocsBridge(ctx, writer, obsidian.DocsBridgeReconcileOptions{
				Project:            project,
				WorkspaceRoot:      target,
				DocsRoot:           docsRoot,
				Folder:             folder,
				MaxMatches:         maxMatches,
				IncludeDocPrefixes: includeDocPrefixes,
				ExcludeDocPrefixes: excludeDocPrefixes,
				SearchProvider:     searchProvider,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/bridge_reconcile", map[string]any{
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
	cmd.Flags().StringVar(&docsRoot, "docs-root", "", "Repo docs root (default: <workspace>/docs)")
	cmd.Flags().StringVar(&project, "project", "", "Project name override")
	cmd.Flags().StringVar(&folder, "folder", "", "Vault folder override (default: inbox/drafted-from-foxctl/docs-bridge/<project>)")
	cmd.Flags().IntVar(&maxMatches, "max-matches", 5, "Maximum suggested canonical vault notes per repo doc")
	cmd.Flags().StringSliceVar(&includeDocPrefixes, "include-doc-prefix", nil, "Only reconcile repo docs whose relative path starts with these prefixes (repeatable)")
	cmd.Flags().StringSliceVar(&excludeDocPrefixes, "exclude-doc-prefix", nil, "Exclude repo docs whose relative path starts with these prefixes (repeatable); defaults to docs/archive/")
	cmd.Flags().BoolVar(&rebuildIndex, "rebuild-index", false, "Rebuild the vault index before reconciling")
	return cmd
}

func newObsidianBridgeReportCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var workspacePath string
	var project string
	var folder string
	var includeDocPrefixes []string
	var excludeDocPrefixes []string

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Report bridge draft review/apply state without opening each note",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			result, err := obsidian.ReportDocsBridgeDrafts(cmd.Context(), writer, obsidian.DocsBridgeReportOptions{
				Project:            project,
				WorkspaceRoot:      target,
				Folder:             folder,
				IncludeDocPrefixes: includeDocPrefixes,
				ExcludeDocPrefixes: excludeDocPrefixes,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/bridge_report", map[string]any{
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
	cmd.Flags().StringVar(&folder, "folder", "", "Bridge draft folder inside the vault (default: inbox/drafted-from-foxctl/docs-bridge/<project>)")
	cmd.Flags().StringSliceVar(&includeDocPrefixes, "include-doc-prefix", nil, "Only report drafts whose repo doc path starts with these prefixes (repeatable)")
	cmd.Flags().StringSliceVar(&excludeDocPrefixes, "exclude-doc-prefix", nil, "Exclude drafts whose repo doc path starts with these prefixes (repeatable)")
	return cmd
}

func newObsidianBridgeApplyCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var workspacePath string
	var project string
	var draftPath string
	var docPath string
	var maxLinks int

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply reviewed bridge draft frontmatter patches to repo docs and canonical vault notes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			result, err := obsidian.ApplyDocsBridgeDraft(cmd.Context(), writer, obsidian.DocsBridgeApplyOptions{
				Project:       project,
				WorkspaceRoot: target,
				DraftPath:     draftPath,
				DocPath:       docPath,
				MaxLinks:      maxLinks,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/bridge_apply", map[string]any{
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
	cmd.Flags().StringVar(&draftPath, "draft-path", "", "Reviewed bridge draft note path inside the vault")
	cmd.Flags().StringVar(&docPath, "doc-path", "", "Repo doc path; if set without --draft-path, resolves the default bridge draft path")
	cmd.Flags().IntVar(&maxLinks, "max-links", 0, "Maximum suggested vault refs to apply from the draft (default: all)")
	return cmd
}

func newObsidianBridgeApplyBatchCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var workspacePath string
	var project string
	var folder string
	var requireStatus string
	var requireTrust string
	var includeDocPrefixes []string
	var excludeDocPrefixes []string
	var maxLinks int
	var maxDrafts int

	cmd := &cobra.Command{
		Use:   "apply-batch",
		Short: "Apply reviewed bridge draft frontmatter patches in bulk",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			result, err := obsidian.ApplyDocsBridgeDrafts(cmd.Context(), writer, obsidian.DocsBridgeBatchApplyOptions{
				Project:            project,
				WorkspaceRoot:      target,
				Folder:             folder,
				RequireStatus:      requireStatus,
				RequireTrust:       requireTrust,
				IncludeDocPrefixes: includeDocPrefixes,
				ExcludeDocPrefixes: excludeDocPrefixes,
				MaxLinks:           maxLinks,
				MaxDrafts:          maxDrafts,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/bridge_apply_batch", map[string]any{
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
	cmd.Flags().StringVar(&folder, "folder", "", "Bridge draft folder inside the vault (default: inbox/drafted-from-foxctl/docs-bridge/<project>)")
	cmd.Flags().StringVar(&requireStatus, "require-status", "reviewed", "Only apply bridge drafts whose frontmatter status matches this value")
	cmd.Flags().StringVar(&requireTrust, "require-trust", "", "Optional trust filter for bridge drafts")
	cmd.Flags().StringSliceVar(&includeDocPrefixes, "include-doc-prefix", nil, "Only apply drafts whose repo doc path starts with these prefixes (repeatable)")
	cmd.Flags().StringSliceVar(&excludeDocPrefixes, "exclude-doc-prefix", nil, "Exclude drafts whose repo doc path starts with these prefixes (repeatable)")
	cmd.Flags().IntVar(&maxLinks, "max-links", 0, "Maximum suggested vault refs to apply per draft (default: all)")
	cmd.Flags().IntVar(&maxDrafts, "max-drafts", 0, "Maximum number of reviewed drafts to apply (default: all matches)")
	return cmd
}

func newObsidianBridgeTidyCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var workspacePath string
	var project string
	var folder string
	var archiveFolder string
	var includeDocPrefixes []string
	var excludeDocPrefixes []string
	var maxDrafts int

	cmd := &cobra.Command{
		Use:   "tidy",
		Short: "Archive fully applied bridge drafts out of the inbox",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			result, err := obsidian.TidyDocsBridgeDrafts(cmd.Context(), writer, obsidian.DocsBridgeTidyOptions{
				Project:            project,
				WorkspaceRoot:      target,
				Folder:             folder,
				ArchiveFolder:      archiveFolder,
				IncludeDocPrefixes: includeDocPrefixes,
				ExcludeDocPrefixes: excludeDocPrefixes,
				MaxDrafts:          maxDrafts,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/bridge_tidy", map[string]any{
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
	cmd.Flags().StringVar(&folder, "folder", "", "Bridge draft folder inside the vault (default: inbox/drafted-from-foxctl/docs-bridge/<project>)")
	cmd.Flags().StringVar(&archiveFolder, "archive-folder", "", "Archive folder inside the vault (default: ops/docs-bridge-applied/<project>)")
	cmd.Flags().StringSliceVar(&includeDocPrefixes, "include-doc-prefix", nil, "Only tidy drafts whose repo doc path starts with these prefixes (repeatable)")
	cmd.Flags().StringSliceVar(&excludeDocPrefixes, "exclude-doc-prefix", nil, "Exclude drafts whose repo doc path starts with these prefixes (repeatable)")
	cmd.Flags().IntVar(&maxDrafts, "max-drafts", 0, "Maximum number of fully applied drafts to archive (default: all)")
	return cmd
}

type bridgeIndexSearchProvider struct {
	index            obsidianindex.Store
	semanticProvider semantic.EmbeddingProvider
}

func (p *bridgeIndexSearchProvider) SearchBridgeCandidates(ctx context.Context, query string, limit int) ([]obsidian.DocsBridgeSearchHit, error) {
	if p == nil || p.index == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	byPath := map[string]obsidian.DocsBridgeSearchHit{}
	lexicalHits, err := p.index.SearchNotes(ctx, query, limit*3)
	if err != nil {
		return nil, err
	}
	maxLexical := 0
	for _, hit := range lexicalHits {
		if hit.Score > maxLexical {
			maxLexical = hit.Score
		}
	}
	for _, hit := range lexicalHits {
		if !isCanonicalBridgePath(hit.Path) {
			continue
		}
		score := hit.Score
		if maxLexical > 0 {
			score = int((float64(hit.Score) / float64(maxLexical)) * 50.0)
		}
		byPath[hit.Path] = obsidian.DocsBridgeSearchHit{
			Path:  hit.Path,
			Title: hit.Title,
			Score: score,
		}
	}
	if p.semanticProvider != nil {
		semanticHits, err := p.index.SearchNotesSemantic(ctx, query, p.semanticProvider, limit*3)
		if err == nil {
			maxSemantic := 0
			for _, hit := range semanticHits {
				if hit.Score > maxSemantic {
					maxSemantic = hit.Score
				}
			}
			for _, hit := range semanticHits {
				if !isCanonicalBridgePath(hit.Path) {
					continue
				}
				boost := hit.Score
				if maxSemantic > 0 {
					boost = int((float64(hit.Score) / float64(maxSemantic)) * 50.0)
				}
				entry := byPath[hit.Path]
				entry.Path = hit.Path
				entry.Title = hit.Title
				entry.Score += boost
				byPath[hit.Path] = entry
			}
		}
	}

	out := make([]obsidian.DocsBridgeSearchHit, 0, len(byPath))
	for _, hit := range byPath {
		out = append(out, hit)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func isCanonicalBridgePath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "notes/") || strings.HasPrefix(path, "00-home/") || strings.HasPrefix(path, "atlas/")
}
