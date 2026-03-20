package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/spf13/cobra"
)

func newContextImportEvidenceCommand() *cobra.Command {
	var workspacePath string
	var vaultPath string
	var title string
	var text string
	var textFile string
	var transcriptFile string

	cmd := &cobra.Command{
		Use:   "import-evidence",
		Short: "Import external text or transcript evidence into the ACA inbox",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			sourceKind, sourceRef, content, err := contextplane.LoadEvidenceContent(text, textFile, transcriptFile)
			if err != nil {
				if err == io.EOF {
					return fmt.Errorf("one of --text, --text-file, or --transcript-file is required")
				}
				return err
			}
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			store := contextplane.NewWorkspaceStore(target)
			result, err := store.ImportEvidence(ctx, cfg.Paths.CAS, vaultPath, contextplane.EvidenceImportInput{
				Title:      title,
				SourceKind: sourceKind,
				SourceRef:  sourceRef,
				Content:    content,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/import_evidence", map[string]any{
				"workspace_path": target,
				"vault_path":     vaultPath,
				"result":         result,
			}, envelope.WithMeta(envelope.Meta{Source: "cli", CASDigest: result.Run.ArtifactDigest})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&title, "title", "", "Optional evidence note title")
	cmd.Flags().StringVar(&text, "text", "", "Inline source text")
	cmd.Flags().StringVar(&textFile, "text-file", "", "Path to a plain text source file")
	cmd.Flags().StringVar(&transcriptFile, "transcript-file", "", "Path to a transcript source file")
	return cmd
}
