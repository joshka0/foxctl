package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/cmd/agentctl/cmd/memorycmd"
	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/spf13/cobra"
)

func newMemoryListCommand() *cobra.Command {
	var workspaceFlag string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List named memories",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				ws := resolveWorkspace(cfg, workspaceFlag)
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entries, err := store.List(ctx, ws, limit)
					if err != nil {
						return err
					}
					payload := struct {
						Entries   []map[string]any `json:"entries"`
						Workspace string           `json:"workspace"`
					}{Workspace: ws}
					for _, e := range entries {
						payload.Entries = append(payload.Entries, map[string]any{
							"name":         e.Name,
							"type":         e.Type,
							"workspace":    e.Workspace,
							"summary":      e.Summary,
							"created_at":   e.CreatedAt,
							"updated_at":   e.UpdatedAt,
							"access_count": e.AccessCount,
						})
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "agentctl.memory.list", payload)
				})
			})
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum entries to return")
	return cmd
}

func newMemorySearchCommand() *cobra.Command {
	var workspaceFlag string
	var query string
	var limit int
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search named memories",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("--query is required")
			}
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				ws := resolveWorkspace(cfg, workspaceFlag)
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entries, err := store.Search(ctx, ws, query, limit)
					if err != nil {
						return err
					}
					payload := struct {
						Entries   []map[string]any `json:"entries"`
						Workspace string           `json:"workspace"`
					}{Workspace: ws}
					for _, e := range entries {
						entry := e.Entry
						payload.Entries = append(payload.Entries, map[string]any{
							"name":       entry.Name,
							"type":       entry.Type,
							"workspace":  entry.Workspace,
							"summary":    entry.Summary,
							"updated_at": entry.UpdatedAt,
							"score":      e.Score,
						})
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "agentctl.memory.search", payload)
				})
			})
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().StringVar(&query, "query", "", "Search text")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum entries to return")
	return cmd
}

func newMemoryGetCommand() *cobra.Command {
	var workspaceFlag string
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Retrieve a named memory envelope",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				ws := resolveWorkspace(cfg, workspaceFlag)
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entry, err := store.Get(ctx, args[0], ws)
					if err != nil {
						return err
					}
					modified, err := cache.AnnotateMemory(entry.Result, envelope.MemoryRef{
						Name:      entry.Name,
						Type:      entry.Type,
						Workspace: entry.Workspace,
					})
					if err != nil {
						return err
					}
					return memorycmd.WriteEnvelope(cmd.OutOrStdout(), modified)
				})
			})
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	return cmd
}

func newMemoryPutCommand() *cobra.Command {
	var name string
	var typ string
	var workspaceFlag string
	var summary string
	var file string
	var data string
	cmd := &cobra.Command{
		Use:   "put",
		Short: "Store a JSON envelope as memory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				ws := resolveWorkspace(cfg, workspaceFlag)
				payload, err := readMemoryPayload(cmd, file, data)
				if err != nil {
					return err
				}
				if !json.Valid(payload) {
					return fmt.Errorf("payload must be valid JSON envelope")
				}
				if summary == "" {
					summary = summarizeResult(payload)
				}
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entry, err := store.SaveFromResult(ctx, name, typ, ws, summary, payload)
					if err != nil {
						return err
					}
					resp := struct {
						Name      string `json:"name"`
						Type      string `json:"type"`
						Workspace string `json:"workspace"`
						Summary   string `json:"summary"`
					}{
						Name:      entry.Name,
						Type:      entry.Type,
						Workspace: entry.Workspace,
						Summary:   entry.Summary,
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "agentctl.memory.put", resp)
				})
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Memory name")
	cmd.Flags().StringVar(&typ, "type", "result", "Memory type label")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().StringVar(&summary, "summary", "", "Summary metadata")
	cmd.Flags().StringVar(&file, "file", "", "Path to JSON envelope ('-' for stdin)")
	cmd.Flags().StringVar(&data, "data", "", "Inline JSON envelope")
	return cmd
}

func newMemorySaveCommand() *cobra.Command {
	var name string
	var typ string
	var workspaceFlag string
	var summary string
	cmd := &cobra.Command{
		Use:   "save <job-id>",
		Short: "Persist a job result as named memory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--as is required")
			}
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				ws := resolveWorkspace(cfg, workspaceFlag)
				jobStore, cleanup, err := openJobStore(ctx)
				if err != nil {
					return err
				}
				defer cleanup()
				result, err := jobStore.Result(ctx, args[0])
				if err != nil {
					return err
				}
				if summary == "" {
					summary = summarizeResult(result)
				}
				return memorycmd.WithMemoryStore(ctx, cfg, func(mem storage.MemoryStore) error {
					entry, err := mem.SaveFromResult(ctx, name, typ, ws, summary, result)
					if err != nil {
						return err
					}
					payload := struct {
						Name      string `json:"name"`
						Type      string `json:"type"`
						Workspace string `json:"workspace"`
						Summary   string `json:"summary"`
					}{
						Name:      entry.Name,
						Type:      entry.Type,
						Workspace: entry.Workspace,
						Summary:   entry.Summary,
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "agentctl.memory.save", payload)
				})
			})
		},
	}
	cmd.Flags().StringVar(&name, "as", "", "Name for the memory entry")
	cmd.Flags().StringVar(&typ, "type", "result", "Memory type label")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().StringVar(&summary, "summary", "", "Summary metadata")
	if err := cmd.MarkFlagRequired("as"); err != nil {
		panic(err)
	}
	return cmd
}

func newMemoryUpdateCommand() *cobra.Command {
	var workspaceFlag string
	var summary string
	var typ string
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update named memory metadata",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if summary == "" && typ == "" {
				return fmt.Errorf("at least one of --summary or --type must be set")
			}
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				ws := resolveWorkspace(cfg, workspaceFlag)
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					var summaryPtr *string
					var typePtr *string
					if summary != "" {
						summaryPtr = &summary
					}
					if typ != "" {
						typePtr = &typ
					}
					entry, err := store.Update(ctx, args[0], ws, summaryPtr, typePtr)
					if err != nil {
						return err
					}
					payload := struct {
						Name      string    `json:"name"`
						Type      string    `json:"type"`
						Workspace string    `json:"workspace"`
						Summary   string    `json:"summary"`
						UpdatedAt time.Time `json:"updated_at"`
					}{
						Name:      entry.Name,
						Type:      entry.Type,
						Workspace: entry.Workspace,
						Summary:   entry.Summary,
						UpdatedAt: entry.UpdatedAt,
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "agentctl.memory.update", payload)
				})
			})
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().StringVar(&summary, "summary", "", "New summary text")
	cmd.Flags().StringVar(&typ, "type", "", "New type label")
	return cmd
}

func newMemoryDeleteCommand() *cobra.Command {
	var workspaceFlag string
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a named memory entry",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				ws := resolveWorkspace(cfg, workspaceFlag)
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					if err := store.Delete(ctx, args[0], ws); err != nil {
						return err
					}
					payload := struct {
						Name      string `json:"name"`
						Workspace string `json:"workspace"`
					}{
						Name:      args[0],
						Workspace: ws,
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "agentctl.memory.delete", payload)
				})
			})
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	return cmd
}

func newMemoryRelevantCommand() *cobra.Command {
	var workspaceFlag string
	var limit int
	cmd := &cobra.Command{
		Use:   "relevant",
		Short: "Rank memories by recency and usage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				ws := resolveWorkspace(cfg, workspaceFlag)
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entries, err := store.Relevant(ctx, ws, limit)
					if err != nil {
						return err
					}
					payload := struct {
						Entries   []map[string]any `json:"entries"`
						Workspace string           `json:"workspace"`
					}{Workspace: ws}
					for _, e := range entries {
						payload.Entries = append(payload.Entries, map[string]any{
							"name":          e.Entry.Name,
							"type":          e.Entry.Type,
							"workspace":     e.Entry.Workspace,
							"summary":       e.Entry.Summary,
							"score":         e.Score,
							"access_count":  e.Entry.AccessCount,
							"last_accessed": e.Entry.LastAccess,
						})
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "agentctl.memory.relevant", payload)
				})
			})
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum entries to return")
	return cmd
}

func summarizeResult(result []byte) string {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err == nil {
		if env.Meta.Workspace != "" {
			return fmt.Sprintf("%s (%s)", env.Command, filepath.Base(env.Meta.Workspace))
		}
		return env.Command
	}
	return ""
}
