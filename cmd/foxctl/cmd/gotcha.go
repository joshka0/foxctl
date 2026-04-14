package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/cmd/foxctl/cmd/memorycmd"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/spf13/cobra"
)

func newGotchaCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gotcha",
		Short: "Manage gotchas (lessons learned, pitfalls, decisions)",
		Long: `Gotchas are memories that capture lessons learned, decisions made, and patterns discovered.
They are automatically associated with CLAUDE.md or AGENTS.md for easy discovery.`,
	}
	cmd.AddCommand(
		newGotchaAddCommand(),
		newGotchaListCommand(),
	)
	return cmd
}

func newGotchaAddCommand() *cobra.Command {
	var name string
	var typ string
	var file string
	var detail string
	var workspaceFlag string

	cmd := &cobra.Command{
		Use:   "add <summary>",
		Short: "Add a new gotcha",
		Long: `Add a gotcha with a summary. Gotchas are automatically associated with CLAUDE.md or AGENTS.md.

Types:
  gotcha   - Lesson learned, pitfall to avoid (default)
  decision - Architecture or design decision with rationale  
  pattern  - Code pattern to follow or avoid

Examples:
  foxctl gotcha add "Skill binaries must be named 'bin' not custom names"
  foxctl gotcha add --type decision "Use SQLite for local storage"
  foxctl gotcha add --name "gotcha-cas-api" --detail "Put() requires 4 args" "CAS Store API gotcha"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary := args[0]
			if summary == "" {
				return fmt.Errorf("summary is required")
			}

			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceRoot := resolveWorkspace(cfg, workspaceFlag)
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)

				if name == "" {
					name = generateGotchaName(typ, summary)
				}

				payload := buildGotchaPayload(summary, detail, file, workspaceRoot)
				payloadBytes, err := json.Marshal(payload)
				if err != nil {
					return fmt.Errorf("marshal payload: %w", err)
				}

				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entry, err := store.SaveFromResult(ctx, name, typ, workspaceID, summary, payloadBytes)
					if err != nil {
						return err
					}
					resp := struct {
						Name      string `json:"name"`
						Type      string `json:"type"`
						File      string `json:"file"`
						Workspace string `json:"workspace"`
						Summary   string `json:"summary"`
					}{
						Name:      entry.Name,
						Type:      entry.Type,
						File:      payload["file"].(string),
						Workspace: entry.Workspace,
						Summary:   entry.Summary,
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.gotcha.add", resp)
				})
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Custom name (default: auto-generated)")
	cmd.Flags().StringVar(&typ, "type", "gotcha", "Type: gotcha, decision, pattern, user_pref, time_sink")
	cmd.Flags().StringVar(&file, "file", "", "Associated file (default: CLAUDE.md or AGENTS.md)")
	cmd.Flags().StringVar(&detail, "detail", "", "Additional detail/context")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path")

	return cmd
}

func newGotchaListCommand() *cobra.Command {
	var typ string
	var file string
	var limit int
	var workspaceFlag string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List gotchas",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)

				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entries, err := store.List(ctx, workspaceID, limit*3)
					if err != nil {
						return err
					}

					types := parseTypes(typ)
					filtered := []map[string]any{}
					for _, e := range entries {
						if !matchesTypes(e.Type, types) {
							continue
						}
						if file != "" && !isAssociatedWithFile(e, file) {
							continue
						}
						if len(filtered) >= limit {
							break
						}
						filtered = append(filtered, map[string]any{
							"name":       e.Name,
							"type":       e.Type,
							"summary":    e.Summary,
							"created_at": e.CreatedAt,
						})
					}

					payload := struct {
						Entries   []map[string]any `json:"entries"`
						Workspace string           `json:"workspace"`
						Count     int              `json:"count"`
					}{
						Entries:   filtered,
						Workspace: workspaceID,
						Count:     len(filtered),
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.gotcha.list", payload)
				})
			})
		},
	}

	cmd.Flags().StringVar(&typ, "type", "gotcha,decision,pattern,user_pref,time_sink", "Filter by type(s)")
	cmd.Flags().StringVar(&file, "file", "", "Filter by associated file")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum entries")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path")

	return cmd
}

func generateGotchaName(typ, summary string) string {
	words := strings.Fields(strings.ToLower(summary))
	if len(words) > 4 {
		words = words[:4]
	}
	slug := strings.Join(words, "-")
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, slug)
	if len(slug) > 30 {
		slug = slug[:30]
	}
	return fmt.Sprintf("%s-%s", typ, slug)
}

func buildGotchaPayload(summary, detail, file, workspace string) map[string]any {
	if file == "" {
		file = findDefaultGotchaFile(workspace)
	}

	payload := map[string]any{
		"summary": summary,
		"file":    file,
	}
	if detail != "" {
		payload["detail"] = detail
	}
	return payload
}

func parseTypes(typ string) []string {
	var types []string
	for _, t := range strings.Split(typ, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			types = append(types, t)
		}
	}
	return types
}

func matchesTypes(entryType string, types []string) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if entryType == t {
			return true
		}
	}
	return false
}

func isAssociatedWithFile(entry storage.NamedEntry, file string) bool {
	file = strings.ToLower(file)
	if strings.Contains(strings.ToLower(entry.Name), file) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Summary), file) {
		return true
	}
	if entry.Result != nil {
		var data map[string]any
		if json.Unmarshal(entry.Result, &data) == nil {
			if f, ok := data["file"].(string); ok {
				if strings.Contains(strings.ToLower(f), file) {
					return true
				}
			}
		}
	}
	return false
}

func init() {
	rootCmd.AddCommand(newGotchaCommand())
}
