package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/cmd/foxctl/cmd/memorycmd"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/obs"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/atomic"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/cache"
	memstore "github.com/joshka0/foxctl/internal/storage/memory"
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
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entries, err := store.List(ctx, workspaceID, limit)
					if err != nil {
						return err
					}
					payload := struct {
						Entries   []map[string]any `json:"entries"`
						Workspace string           `json:"workspace"`
					}{Workspace: workspaceID}
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
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.list", payload)
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
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entries, err := store.Search(ctx, workspaceID, query, limit)
					if err != nil {
						return err
					}
					payload := struct {
						Entries   []map[string]any `json:"entries"`
						Workspace string           `json:"workspace"`
					}{Workspace: workspaceID}
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
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.search", payload)
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
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)
				name := args[0]
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entry, err := store.Get(ctx, name, workspaceID)
					if err != nil {
						if errors.Is(err, memstore.ErrNotFound) {
							return memorycmd.WriteNotFound(cmd.OutOrStdout(), "foxctl.memory.get", name, workspaceID)
						}
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
	var atomicFlag bool
	cmd := &cobra.Command{
		Use:   "put",
		Short: "Store a JSON envelope as memory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceRoot := resolveWorkspace(cfg, workspaceFlag)
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)
				payload, err := readMemoryPayload(cmd, file, data)
				if err != nil {
					return err
				}
				if !json.Valid(payload) {
					return fmt.Errorf("payload must be valid JSON envelope")
				}
				payload = injectDefaultFileForGotchaTypes(payload, typ, workspaceRoot)
				if summary == "" {
					summary = summarizeResult(payload)
				}
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entry, err := store.SaveFromResult(ctx, name, typ, workspaceID, summary, payload)
					if err != nil {
						return err
					}

					// Atomic processing: transform summary into self-contained atomic fact
					var atomicText string
					var entities, keywords []string
					if atomicFlag && summary != "" {
						processor, procErr := atomic.NewProcessorWithConfig(cfg.LLM.AtomicAPIKey, cfg.LLM.AtomicEndpoint, cfg.LLM.AtomicModel)
						if procErr != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "atomic: processor init failed: %v (api_key_set=%v)\n",
								procErr, cfg.LLM.AtomicAPIKey != "")
						} else {
							fact, usage, factErr := processor.ProcessSingle(ctx, summary, atomic.ProcessContext{
								Workspace: workspaceID,
							})
							if factErr != nil {
								fmt.Fprintf(cmd.ErrOrStderr(), "atomic: processing failed: %v\n", factErr)
							} else {
								// Emit token usage via observability
								if usage != nil {
									event := observability.NewEvent("memory.atomic_processing").
										WithComponent(observability.ComponentCLI).
										WithData(obs.KeyLLMModel, usage.Model).
										WithData(obs.KeyLLMInputTokens, usage.InputTokens).
										WithData(obs.KeyLLMOutputTokens, usage.OutputTokens).
										WithData(obs.KeyLLMTotalTokens, usage.TotalTokens).
										WithData(obs.KeyLLMInputCostUSD, usage.InputCostUSD).
										WithData(obs.KeyLLMOutputCostUSD, usage.OutputCostUSD).
										WithData(obs.KeyLLMTotalCostUSD, usage.TotalCostUSD).
										WithData("memory_name", name)
									observability.Emit(ctx, event.Success(0))
								}
								// Update the memory with atomic fields - only set response fields on success
								if updateErr := store.UpdateAtomic(ctx, name, workspaceID, fact.Atomic, fact.Entities, fact.Keywords); updateErr != nil {
									fmt.Fprintf(cmd.ErrOrStderr(), "atomic: store update failed: %v\n", updateErr)
								} else {
									// Only populate response fields after successful persistence
									atomicText = fact.Atomic
									entities = fact.Entities
									keywords = fact.Keywords
								}
							}
						}
					}

					resp := struct {
						Name       string   `json:"name"`
						Type       string   `json:"type"`
						Workspace  string   `json:"workspace"`
						Summary    string   `json:"summary"`
						AtomicText string   `json:"atomic_text,omitempty"`
						Entities   []string `json:"entities,omitempty"`
						Keywords   []string `json:"keywords,omitempty"`
					}{
						Name:       entry.Name,
						Type:       entry.Type,
						Workspace:  entry.Workspace,
						Summary:    entry.Summary,
						AtomicText: atomicText,
						Entities:   entities,
						Keywords:   keywords,
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.put", resp)
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
	cmd.Flags().BoolVar(&atomicFlag, "atomic", false, "Enable atomic fact processing (SimpleMem-style)")
	return cmd
}

func injectDefaultFileForGotchaTypes(payload []byte, typ, workspace string) []byte {
	if typ != "gotcha" && typ != "decision" && typ != "pattern" && typ != "user_pref" && typ != "time_sink" {
		return payload
	}

	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return payload
	}

	if _, hasFile := data["file"]; hasFile {
		return payload
	}

	defaultFile := findDefaultGotchaFile(workspace)
	if defaultFile == "" {
		return payload
	}

	data["file"] = defaultFile
	modified, err := json.Marshal(data)
	if err != nil {
		return payload
	}
	return modified
}

func findDefaultGotchaFile(workspace string) string {
	workspaceClaude := filepath.Join(workspace, "CLAUDE.md")
	if _, err := os.Stat(workspaceClaude); err == nil {
		return "CLAUDE.md"
	}

	workspaceAgents := filepath.Join(workspace, "AGENTS.md")
	if _, err := os.Stat(workspaceAgents); err == nil {
		return "AGENTS.md"
	}

	homeClaude := filepath.Join(os.Getenv("HOME"), ".claude", "CLAUDE.md")
	if _, err := os.Stat(homeClaude); err == nil {
		return "CLAUDE.md"
	}

	return "CLAUDE.md"
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
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)
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
					entry, err := mem.SaveFromResult(ctx, name, typ, workspaceID, summary, result)
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
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.save", payload)
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
				return memorycmd.WriteArgError(cmd.OutOrStdout(), "foxctl.memory.update",
					"at least one of --summary or --type must be set",
					"Use --summary to update the summary or --type to change the memory type.")
			}
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)
				name := args[0]
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					var summaryPtr *string
					var typePtr *string
					if summary != "" {
						summaryPtr = &summary
					}
					if typ != "" {
						typePtr = &typ
					}
					entry, err := store.Update(ctx, name, workspaceID, summaryPtr, typePtr)
					if err != nil {
						if errors.Is(err, memstore.ErrNotFound) {
							return memorycmd.WriteNotFound(cmd.OutOrStdout(), "foxctl.memory.update", name, workspaceID)
						}
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
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.update", payload)
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
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a named memory entry",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)
				name := args[0]
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					// Check if entry exists (for both dry-run and actual delete)
					_, err := store.Get(ctx, name, workspaceID)
					if err != nil {
						if errors.Is(err, memstore.ErrNotFound) {
							return memorycmd.WriteNotFound(cmd.OutOrStdout(), "foxctl.memory.delete", name, workspaceID)
						}
						return err
					}

					if dryRun {
						payload := struct {
							Name      string `json:"name"`
							Workspace string `json:"workspace"`
							DryRun    bool   `json:"dry_run"`
							Message   string `json:"message"`
						}{
							Name:      name,
							Workspace: workspaceID,
							DryRun:    true,
							Message:   "Would delete memory entry",
						}
						return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.delete", payload)
					}

					if err := store.Delete(ctx, name, workspaceID); err != nil {
						return err
					}
					payload := struct {
						Name         string `json:"name"`
						Workspace    string `json:"workspace"`
						DeletedCount int    `json:"deleted_count"`
					}{
						Name:         name,
						Workspace:    workspaceID,
						DeletedCount: 1,
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.delete", payload)
				})
			})
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be deleted without making changes")
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
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)
				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					entries, err := store.Relevant(ctx, workspaceID, limit)
					if err != nil {
						return err
					}
					payload := struct {
						Entries   []map[string]any `json:"entries"`
						Workspace string           `json:"workspace"`
					}{Workspace: workspaceID}
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
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.relevant", payload)
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

func newMemoryMigrateWorkspaceCommand() *cobra.Command {
	var (
		workspaceFlag string
		from          []string
		to            string
		apply         bool
		dryRunFlag    bool
	)
	cmd := &cobra.Command{
		Use:   "migrate-workspace",
		Short: "Migrate named memory workspace IDs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceRoot := resolveWorkspace(cfg, workspaceFlag)
				workspaceID := strings.TrimSpace(to)
				if workspaceID == "" {
					workspaceID = workspace.ID(workspaceRoot)
				}
				if workspaceID == "" {
					return memorycmd.WriteArgError(cmd.OutOrStdout(), "foxctl.memory.migrate_workspace", "invalid target workspace", "Provide --to or a valid --workspace path.")
				}

				fromList := normalizeMigrationSources(from, workspaceRoot, workspaceID)
				if len(fromList) == 0 {
					return memorycmd.WriteArgError(cmd.OutOrStdout(), "foxctl.memory.migrate_workspace", "no source workspaces", "Provide --from or ensure the workspace path is available.")
				}

				return memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					memStore, ok := store.(*memstore.Store)
					if !ok {
						return fmt.Errorf("memory: migrate workspace requires local store")
					}

					if apply && dryRunFlag {
						return memorycmd.WriteArgError(cmd.OutOrStdout(), "foxctl.memory.migrate_workspace", "conflicting flags", "Use --apply or --dry-run, not both.")
					}
					dryRun := !apply
					if dryRunFlag {
						dryRun = true
					}
					summaries := make([]memstore.WorkspaceMigrationSummary, 0, len(fromList))
					for _, source := range fromList {
						summary, err := memStore.MigrateWorkspace(ctx, source, workspaceID, dryRun)
						if err != nil {
							return err
						}
						summaries = append(summaries, summary)
					}

					data := map[string]any{
						"workspace_root": workspaceRoot,
						"workspace_id":   workspaceID,
						"dry_run":        dryRun,
						"summaries":      summaries,
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.migrate_workspace", data)
				})
			})
		},
	}
	cmd.Flags().StringSliceVar(&from, "from", nil, "Source workspace IDs to migrate (repeatable)")
	cmd.Flags().StringVar(&to, "to", "", "Target workspace ID (defaults to repo ID)")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply changes")
	cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Force dry-run mode")
	return cmd
}

func normalizeMigrationSources(from []string, workspaceRoot, target string) []string {
	if len(from) == 0 {
		from = []string{"default", workspaceRoot}
	}
	seen := make(map[string]struct{}, len(from))
	result := make([]string, 0, len(from))
	for _, source := range from {
		source = strings.TrimSpace(source)
		if source == "" || source == target {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		result = append(result, source)
	}
	return result
}
