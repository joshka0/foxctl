package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
	"github.com/spf13/cobra"
)

func newObsidianCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "obsidian",
		Short: "Interact with Obsidian vaults through the Phase 1 adapter",
	}
	cmd.AddCommand(
		newObsidianIndexCommand(),
		newObsidianGraphCommand(),
		newObsidianBridgeCommand(),
		newObsidianReadCommand(),
		newObsidianSearchCommand(),
		newObsidianRelatedCommand(),
		newObsidianCreateCommand(),
		newObsidianAppendCommand(),
		newObsidianCaptureSessionCommand(),
		newObsidianPromoteCommand(),
		newObsidianMergeReviewedDraftCommand(),
	)
	return cmd
}

func newObsidianReadCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var notePath string

	cmd := &cobra.Command{
		Use:   "read",
		Short: "Read a vault note",
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := obsidian.Read(cmd.Context(), obsidian.ReadOptions{
				VaultName: vaultName,
				VaultPath: vaultPath,
				NotePath:  notePath,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/read", map[string]any{
				"result": res,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&notePath, "path", "", "Note path")
	_ = cmd.MarkFlagRequired("path")
	return cmd
}

func newObsidianSearchCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var query string
	var scopePath string
	var limit int

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search a vault",
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := obsidian.Search(cmd.Context(), obsidian.SearchOptions{
				VaultName: vaultName,
				VaultPath: vaultPath,
				Query:     query,
				ScopePath: scopePath,
				Limit:     limit,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/search", map[string]any{
				"result": res,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&query, "query", "", "Search query")
	cmd.Flags().StringVar(&scopePath, "scope-path", "", "Vault subpath filter")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum result count")
	_ = cmd.MarkFlagRequired("query")
	return cmd
}

func newObsidianRelatedCommand() *cobra.Command {
	var vaultPath string
	var notePath string
	var limit int

	cmd := &cobra.Command{
		Use:   "related",
		Short: "List related notes by wikilinks, backlinks, and aliases",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			var (
				res any
				err error
			)
			cfg, cfgErr := loadConfig(cmd.Context())
			if cfgErr == nil {
				if idx, openErr := obsidianindex.Open(cmd.Context(), cfg.Storage.Root, vaultPath); openErr == nil {
					defer func() { _ = idx.Close() }()
					if stats, statsErr := idx.Stats(cmd.Context()); statsErr == nil && stats.Notes > 0 {
						res, err = idx.RelatedNotes(cmd.Context(), notePath, limit)
					}
				}
			}
			if res == nil && err == nil {
				res, err = obsidian.RelatedNotes(vaultPath, notePath, obsidian.LinkQueryOptions{
					Depth:         1,
					IncludeDirect: true,
					IncludeBack:   true,
					IncludeAlias:  true,
					Limit:         limit,
				})
			}
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/related", map[string]any{
				"vault_path": vaultPath,
				"path":       notePath,
				"results":    res,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&notePath, "path", "", "Note path")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum result count")
	_ = cmd.MarkFlagRequired("path")
	return cmd
}

func newObsidianCreateCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var notePath string
	var noteType string
	var project string
	var status string
	var trust string
	var body string

	cmd := &cobra.Command{
		Use:   "create-note",
		Short: "Create a draft note through the Obsidian adapter",
		RunE: func(cmd *cobra.Command, _ []string) error {
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			content := buildVaultDraftContent(filepath.Base(notePath), noteType, project, status, trust, body)
			if err := writer.CreateNote(cmd.Context(), notePath, content, true); err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/create_note", map[string]any{
				"vault_name": vaultName,
				"vault_path": vaultPath,
				"path":       notePath,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&notePath, "path", "", "Target note path")
	cmd.Flags().StringVar(&noteType, "type", "investigation", "Note type")
	cmd.Flags().StringVar(&project, "project", "", "Project")
	cmd.Flags().StringVar(&status, "status", "draft", "Status")
	cmd.Flags().StringVar(&trust, "trust", "raw", "Trust level")
	cmd.Flags().StringVar(&body, "body", "", "Markdown body")
	_ = cmd.MarkFlagRequired("path")
	return cmd
}

func newObsidianAppendCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var notePath string
	var heading string
	var content string

	cmd := &cobra.Command{
		Use:   "append-under-heading",
		Short: "Append content under a specific heading",
		RunE: func(cmd *cobra.Command, _ []string) error {
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			if err := writer.AppendUnderHeading(cmd.Context(), notePath, heading, content); err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/append_under_heading", map[string]any{
				"vault_name": vaultName,
				"vault_path": vaultPath,
				"path":       notePath,
				"heading":    heading,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&notePath, "path", "", "Target note path")
	cmd.Flags().StringVar(&heading, "heading", "", "Heading to append under")
	cmd.Flags().StringVar(&content, "content", "", "Markdown content")
	_ = cmd.MarkFlagRequired("path")
	_ = cmd.MarkFlagRequired("heading")
	_ = cmd.MarkFlagRequired("content")
	return cmd
}

func newObsidianCaptureSessionCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var slug string
	var content string

	cmd := &cobra.Command{
		Use:   "capture-session",
		Short: "Capture a session note into the sessions area",
		RunE: func(cmd *cobra.Command, _ []string) error {
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			path, err := writer.CaptureSession(cmd.Context(), slug, content)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/capture_session", map[string]any{
				"vault_name": vaultName,
				"vault_path": vaultPath,
				"path":       path,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&slug, "slug", "", "Session note slug")
	cmd.Flags().StringVar(&content, "content", "", "Markdown content")
	_ = cmd.MarkFlagRequired("content")
	return cmd
}

func newObsidianPromoteCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var slug string
	var content string

	cmd := &cobra.Command{
		Use:   "promote-evergreen",
		Short: "Create an inbox-first evergreen promotion draft",
		RunE: func(cmd *cobra.Command, _ []string) error {
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			path, err := writer.PromoteToEvergreen(cmd.Context(), slug, content)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/promote_to_evergreen", map[string]any{
				"vault_name": vaultName,
				"vault_path": vaultPath,
				"path":       path,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&slug, "slug", "", "Draft slug")
	cmd.Flags().StringVar(&content, "content", "", "Markdown content")
	_ = cmd.MarkFlagRequired("content")
	return cmd
}

func newObsidianMergeReviewedDraftCommand() *cobra.Command {
	var vaultName string
	var vaultPath string
	var draftPath string
	var targetPath string
	var heading string

	cmd := &cobra.Command{
		Use:   "merge-reviewed-draft",
		Short: "Merge a reviewed draft into a canonical vault note",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			if strings.TrimSpace(draftPath) == "" {
				return fmt.Errorf("--draft-path is required")
			}
			if strings.TrimSpace(targetPath) == "" {
				return fmt.Errorf("--target-path is required")
			}
			body, err := os.ReadFile(draftPath)
			if err != nil {
				return err
			}
			writer := obsidian.NewWriter("", vaultName, obsidian.DefaultPolicy())
			writer.VaultPath = vaultPath
			result, err := writer.MergeReviewedDraftContent(cmd.Context(), targetPath, heading, string(body), filepath.Base(draftPath))
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/merge_reviewed_draft", map[string]any{
				"vault_name":  vaultName,
				"vault_path":  vaultPath,
				"draft_path":  draftPath,
				"target_path": targetPath,
				"merge":       result,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&draftPath, "draft-path", "", "Local reviewed draft path")
	cmd.Flags().StringVar(&targetPath, "target-path", "", "Canonical target note path inside the vault")
	cmd.Flags().StringVar(&heading, "heading", "", "Bounded heading for appending into an existing note")
	return cmd
}

func buildVaultDraftContent(title, noteType, project, status, trust, body string) string {
	title = strings.TrimSuffix(filepath.Base(title), filepath.Ext(title))
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + yamlScalar(title) + "\n")
	if noteType != "" {
		b.WriteString("type: " + yamlScalar(noteType) + "\n")
	}
	if project != "" {
		b.WriteString("project: " + yamlScalar(project) + "\n")
	}
	if status != "" {
		b.WriteString("status: " + yamlScalar(status) + "\n")
	}
	if trust != "" {
		b.WriteString("trust: " + yamlScalar(trust) + "\n")
	}
	b.WriteString("---\n\n")
	b.WriteString("# " + title + "\n\n")
	b.WriteString(strings.TrimSpace(body))
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func yamlScalar(value string) string {
	return strconv.Quote(strings.TrimSpace(value))
}

func init() {
	rootCmd.AddCommand(newObsidianCommand())
}
