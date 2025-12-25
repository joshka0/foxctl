package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/cmd/agentctl/cmd/sessionscmd"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/spf13/cobra"
)

func newSessionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage captured Claude Code conversation sessions",
		Long: `Manage captured Claude Code conversation sessions.

Sessions are captured from Claude Code's JSONL conversation files and stored
in agentctl's sessions.db. They can be summarized, searched, and used for
progressive memory retrieval.

Use the Stop hook (session-capture.sh) for automatic capture, or capture
manually with:
  agentctl run session/capture --input '{"workspace": "/path/to/project"}'`,
	}
	cmd.AddCommand(
		newSessionsListCommand(),
		newSessionsShowCommand(),
		newSessionsSearchCommand(),
		newSessionsStatsCommand(),
		newSessionsDeleteCommand(),
		newSessionsCaptureCommand(),
		newSessionsSummarizeCommand(),
		newSessionsImportCommand(),
	)
	return cmd
}

func newSessionsListCommand() *cobra.Command {
	var workspacePath string
	var projectName string
	var tags []string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List captured sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					opts := storage.SessionListOptions{
						WorkspacePath: workspacePath,
						ProjectName:   projectName,
						Tags:          tags,
						Limit:         limit,
					}
					sessionList, err := store.List(ctx, opts)
					if err != nil {
						return err
					}

					payload := struct {
						Sessions []sessionSummary `json:"sessions"`
						Count    int              `json:"count"`
					}{
						Count: len(sessionList),
					}

					for _, s := range sessionList {
						payload.Sessions = append(payload.Sessions, sessionSummary{
							ID:              s.ID,
							ProjectName:     s.ProjectName,
							GitBranch:       s.GitBranch,
							Summary:         truncateSummary(s.Summary, 100),
							MessageCount:    s.MessageCount,
							UserTurns:       s.UserTurns,
							ToolInvocations: s.ToolInvocations,
							TotalTokens:     s.TotalTokens,
							StartedAt:       s.StartedAt,
							Tags:            s.Tags,
						})
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.list", payload)
				})
			})
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Filter by workspace path")
	cmd.Flags().StringVar(&projectName, "project", "", "Filter by project name")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "Filter by tags (matches any)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum sessions to return")
	return cmd
}

func newSessionsShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show details of a captured session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					session, err := store.Get(ctx, sessionID)
					if err != nil {
						if errors.Is(err, sessions.ErrNotFound) {
							return sessionscmd.WriteNotFound(cmd.OutOrStdout(), "agentctl.sessions.show", sessionID)
						}
						return err
					}

					payload := sessionDetail{
						ID:              session.ID,
						WorkspacePath:   session.WorkspacePath,
						ProjectName:     session.ProjectName,
						GitBranch:       session.GitBranch,
						ClaudeVersion:   session.ClaudeVersion,
						StartedAt:       session.StartedAt,
						EndedAt:         session.EndedAt,
						Summary:         session.Summary,
						Accomplished:    session.Accomplished,
						Decisions:       session.Decisions,
						Gotchas:         session.Gotchas,
						Tags:            session.Tags,
						KeyFiles:        session.KeyFiles,
						ToolsPattern:    session.ToolsPattern,
						MessageCount:    session.MessageCount,
						UserTurns:       session.UserTurns,
						ToolInvocations: session.ToolInvocations,
						TotalTokens:     session.TotalTokens,
						RawJSONLPath:    session.RawJSONLPath,
						HasEmbedding:    len(session.Embedding) > 0,
						EmbeddingModel:  session.EmbeddingModel,
						CreatedAt:       session.CreatedAt,
						UpdatedAt:       session.UpdatedAt,
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.show", payload)
				})
			})
		},
	}
	return cmd
}

func newSessionsSearchCommand() *cobra.Command {
	var query string
	var limit int

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search sessions by text",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(query) == "" {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.search",
					"--query is required",
					"Provide a search query to find relevant sessions.")
			}
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					sessionList, err := store.Search(ctx, query, limit)
					if err != nil {
						return err
					}

					payload := struct {
						Sessions []sessionSummary `json:"sessions"`
						Query    string           `json:"query"`
						Count    int              `json:"count"`
					}{
						Query: query,
						Count: len(sessionList),
					}

					for _, s := range sessionList {
						payload.Sessions = append(payload.Sessions, sessionSummary{
							ID:              s.ID,
							ProjectName:     s.ProjectName,
							GitBranch:       s.GitBranch,
							Summary:         truncateSummary(s.Summary, 100),
							MessageCount:    s.MessageCount,
							UserTurns:       s.UserTurns,
							ToolInvocations: s.ToolInvocations,
							TotalTokens:     s.TotalTokens,
							StartedAt:       s.StartedAt,
							Tags:            s.Tags,
						})
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.search", payload)
				})
			})
		},
	}

	cmd.Flags().StringVar(&query, "query", "", "Search text")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum sessions to return")
	return cmd
}

func newSessionsStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show sessions store statistics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					stats, err := store.Stats(ctx)
					if err != nil {
						return err
					}

					payload := struct {
						TotalSessions int64  `json:"total_sessions"`
						DatabasePath  string `json:"database_path"`
					}{
						TotalSessions: stats.Count,
						DatabasePath:  stats.Path,
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.stats", payload)
				})
			})
		},
	}
	return cmd
}

func newSessionsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a captured session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					if err := store.Delete(ctx, sessionID); err != nil {
						if errors.Is(err, sessions.ErrNotFound) {
							return sessionscmd.WriteNotFound(cmd.OutOrStdout(), "agentctl.sessions.delete", sessionID)
						}
						return err
					}

					payload := struct {
						SessionID string `json:"session_id"`
						Deleted   bool   `json:"deleted"`
					}{
						SessionID: sessionID,
						Deleted:   true,
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.delete", payload)
				})
			})
		},
	}
	return cmd
}

func newSessionsCaptureCommand() *cobra.Command {
	var workspace string
	var sessionID string
	var force bool

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture a Claude Code session (delegates to session/capture skill)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{}
			if workspace != "" {
				payload["workspace"] = workspace
			}
			if sessionID != "" {
				payload["session_id"] = sessionID
			}
			if force {
				payload["force"] = true
			}
			return runSessionSkill(cmd, "session/capture", payload)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Project workspace path (defaults to cwd)")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Specific session ID to capture")
	cmd.Flags().BoolVar(&force, "force", false, "Re-capture even if session exists")
	return cmd
}

func newSessionsSummarizeCommand() *cobra.Command {
	var sessionID string
	var force bool

	cmd := &cobra.Command{
		Use:   "summarize",
		Short: "Generate summary for a captured session (requires CEREBRAS_API_KEY)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sessionID == "" {
				return fmt.Errorf("--session-id is required")
			}
			payload := map[string]any{
				"session_id": sessionID,
			}
			if force {
				payload["force"] = true
			}
			return runSessionSkill(cmd, "session/summarize", payload)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID to summarize (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Re-summarize even if already summarized")
	if err := cmd.MarkFlagRequired("session-id"); err != nil {
		panic(err)
	}
	return cmd
}

func newSessionsImportCommand() *cobra.Command {
	var dryRun bool
	var summarize bool
	var limit int
	var projectFilter string

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import uncaptured Claude Code sessions",
		Long: `Scan Claude Code's JSONL conversation files and import uncaptured sessions.

This command:
1. Scans ~/.claude/projects/ for JSONL session files
2. Compares with sessions.db to find uncaptured sessions
3. Captures each session (parses JSONL, extracts metadata)
4. Optionally summarizes with LLM and generates embeddings

Requires OPENROUTER_API_KEY or GEMINI_API_KEY for summarization and embeddings.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					// Find Claude projects directory
					claudeProjectsDir := sessionscmd.ClaudeProjectsDir()

					// Get captured session IDs
					captured := make(map[string]bool)
					allSessions, err := store.List(ctx, storage.SessionListOptions{Limit: 10000})
					if err != nil {
						return fmt.Errorf("list captured sessions: %w", err)
					}
					for _, s := range allSessions {
						captured[s.ID] = true
					}

					// Find uncaptured sessions
					uncaptured, err := sessionscmd.FindUncapturedSessions(claudeProjectsDir, captured, projectFilter)
					if err != nil {
						return fmt.Errorf("scan for sessions: %w", err)
					}

					if limit > 0 && len(uncaptured) > limit {
						uncaptured = uncaptured[:limit]
					}

					if dryRun {
						payload := struct {
							Uncaptured []sessionscmd.UncapturedSession `json:"uncaptured"`
							Count      int                             `json:"count"`
							DryRun     bool                            `json:"dry_run"`
						}{
							Uncaptured: uncaptured,
							Count:      len(uncaptured),
							DryRun:     true,
						}
						return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.import", payload)
					}

					// Import each session
					imported := []string{}
					failed := []string{}
					for _, u := range uncaptured {
						fmt.Fprintf(cmd.ErrOrStderr(), "Importing %s from %s...\n", u.SessionID, u.ProjectName)

						// Capture
						capturePayload := map[string]any{
							"workspace":  u.WorkspacePath,
							"session_id": u.SessionID,
						}
						if err := runSessionSkillQuiet(cmd, cfg, "session/capture", capturePayload); err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "  capture failed: %v\n", err)
							failed = append(failed, u.SessionID)
							continue
						}

						// Summarize if requested
						if summarize {
							fmt.Fprintf(cmd.ErrOrStderr(), "  summarizing...\n")
							summarizePayload := map[string]any{
								"session_id": u.SessionID,
							}
							if err := runSessionSkillQuiet(cmd, cfg, "session/summarize", summarizePayload); err != nil {
								fmt.Fprintf(cmd.ErrOrStderr(), "  summarize failed: %v\n", err)
								// Continue anyway, capture succeeded
							}
						}

						imported = append(imported, u.SessionID)
					}

					payload := struct {
						Imported []string `json:"imported"`
						Failed   []string `json:"failed,omitempty"`
						Count    int      `json:"count"`
					}{
						Imported: imported,
						Failed:   failed,
						Count:    len(imported),
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.import", payload)
				})
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be imported without importing")
	cmd.Flags().BoolVar(&summarize, "summarize", true, "Generate LLM summary and embeddings after capture")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum sessions to import (0 = unlimited)")
	cmd.Flags().StringVar(&projectFilter, "project", "", "Only import sessions from this project")
	return cmd
}

// runSessionSkillQuiet runs a session skill without output.
func runSessionSkillQuiet(cmd *cobra.Command, cfg config.Config, skillName string, payload map[string]any) error {
	_, err := findSkill(cfg, skillName)
	if err != nil {
		return fmt.Errorf("%s skill not found: %w", skillName, err)
	}

	runCmd := newRunCommand()
	runCmd.SetContext(cmd.Context())
	runCmd.SetOut(io.Discard) // Suppress output
	runCmd.SetErr(io.Discard)

	inputJSON := "{}"
	if len(payload) > 0 {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		inputJSON = string(b)
	}

	runCmd.SetArgs([]string{"--input", inputJSON, skillName})
	return runCmd.Execute()
}

// runSessionSkill runs a session skill via the run command.
func runSessionSkill(cmd *cobra.Command, skillName string, payload map[string]any) error {
	cfg, err := config.Load(cmd.Context())
	if err != nil {
		return err
	}
	_, err = findSkill(cfg, skillName)
	if err != nil {
		return fmt.Errorf("%s skill not found (run make skills-build): %w", skillName, err)
	}

	runCmd := newRunCommand()
	runCmd.SetContext(cmd.Context())
	runCmd.SetOut(cmd.OutOrStdout())
	runCmd.SetErr(cmd.ErrOrStderr())

	inputJSON := "{}"
	if len(payload) > 0 {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		inputJSON = string(b)
	}

	runCmd.SetArgs([]string{"--input", inputJSON, skillName})
	return runCmd.Execute()
}

// sessionSummary is a compact representation for list/search output.
type sessionSummary struct {
	ID              string    `json:"id"`
	ProjectName     string    `json:"project_name"`
	GitBranch       string    `json:"git_branch,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	MessageCount    int       `json:"message_count"`
	UserTurns       int       `json:"user_turns"`
	ToolInvocations int       `json:"tool_invocations"`
	TotalTokens     int       `json:"total_tokens"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
}

// sessionDetail is the full session representation for show output.
type sessionDetail struct {
	ID              string    `json:"id"`
	WorkspacePath   string    `json:"workspace_path"`
	ProjectName     string    `json:"project_name"`
	GitBranch       string    `json:"git_branch,omitempty"`
	ClaudeVersion   string    `json:"claude_version,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	EndedAt         time.Time `json:"ended_at,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	Accomplished    []string  `json:"accomplished,omitempty"`
	Decisions       []string  `json:"decisions,omitempty"`
	Gotchas         []string  `json:"gotchas,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
	KeyFiles        []string  `json:"key_files,omitempty"`
	ToolsPattern    string    `json:"tools_pattern,omitempty"`
	MessageCount    int       `json:"message_count"`
	UserTurns       int       `json:"user_turns"`
	ToolInvocations int       `json:"tool_invocations"`
	TotalTokens     int       `json:"total_tokens"`
	RawJSONLPath    string    `json:"raw_jsonl_path,omitempty"`
	HasEmbedding    bool      `json:"has_embedding"`
	EmbeddingModel  string    `json:"embedding_model,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func truncateSummary(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func init() {
	rootCmd.AddCommand(newSessionsCommand())
}
